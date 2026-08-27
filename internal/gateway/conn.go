package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/nglong14/CodeDuel/internal/proto"
	"github.com/nglong14/CodeDuel/internal/redisx"
	"github.com/nglong14/CodeDuel/internal/submission"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 256 << 10
	sendBufSize    = 16
)

// conn wraps a WebSocket so reader, heartbeat, and pub/sub never write concurrently.
type conn struct {
	ctx              context.Context
	userID           uuid.UUID
	connectionID     uuid.UUID
	presenceKey      string
	route            string
	ws               *websocket.Conn
	send             chan []byte
	closed           chan struct{}
	writeMu          sync.Mutex
	closeOnce        sync.Once
	cleanupOnce      sync.Once
	registry         *Registry
	registered       bool
	logger           *slog.Logger
	enqueue          func(context.Context, redisx.QueueMember) error
	acceptSubmission func(context.Context, submission.Request) (uuid.UUID, error)
	refreshPresence  func(context.Context) error
	onClose          func()
}

func newConn(userID uuid.UUID, ws *websocket.Conn, registry *Registry) *conn {
	connectionID := uuid.New()
	return &conn{
		ctx:          context.Background(),
		userID:       userID,
		connectionID: connectionID,
		presenceKey:  redisx.PresenceKey(userID, connectionID),
		route:        redisx.UserChannel(userID),
		ws:           ws,
		send:         make(chan []byte, sendBufSize),
		closed:       make(chan struct{}),
		registry:     registry,
		logger:       slog.Default(),
	}
}

func (c *conn) serve() {
	go c.writePump()
	c.readPump()
	c.cleanup()
}

func (c *conn) cleanup() {
	c.cleanupOnce.Do(func() {
		defer c.registry.Done(c)
		c.registry.Remove(c)
		if c.onClose != nil {
			c.onClose()
		}
		c.close()
	})
}

func (c *conn) close() {
	c.closeOnce.Do(func() {
		close(c.closed)
		if c.ws != nil {
			_ = c.ws.Close()
		}
	})
}

func (c *conn) Send(msg []byte) {
	select {
	case c.send <- msg:
	case <-c.closed:
	default:
		c.close()
	}
}

func (c *conn) write(messageType int, data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.ws.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
		return err
	}
	return c.ws.WriteMessage(messageType, data)
}

func (c *conn) readPump() {
	c.ws.SetReadLimit(maxMessageSize)
	_ = c.ws.SetReadDeadline(time.Now().Add(pongWait))
	c.ws.SetPongHandler(func(string) error {
		if err := c.ws.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
			return err
		}
		if c.refreshPresence != nil {
			if err := c.refreshPresence(c.ctx); err != nil {
				c.logger.Warn("refresh presence failed", "user_id", c.userID, "err", err)
			}
		}
		return nil
	})

	for {
		_, raw, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		resp, err := c.handleInbound(raw)
		if err != nil {
			return
		}
		if len(resp) > 0 {
			c.Send(resp)
		}
	}
}

func (c *conn) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case msg := <-c.send:
			if err := c.write(websocket.TextMessage, msg); err != nil {
				c.close()
				return
			}
		case <-ticker.C:
			if err := c.write(websocket.PingMessage, nil); err != nil {
				c.close()
				return
			}
		case <-c.closed:
			_ = c.write(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return
		}
	}
}

type inboundIntent struct {
	typ        string
	submission proto.SubmitCodeData
}

func decodeInbound(raw []byte) (inboundIntent, string) {
	env, err := proto.Decode(raw)
	if err != nil {
		return inboundIntent{}, "invalid message"
	}

	switch env.Type {
	case proto.TypeJoinQueue:
		if err := decodeEmptyData(env.Data); err != nil {
			return inboundIntent{}, "invalid join_queue"
		}
		return inboundIntent{typ: proto.TypeJoinQueue}, ""
	case proto.TypeSubmitCode:
		data, err := proto.DecodeSubmitCodeData(env.Data)
		if err != nil {
			return inboundIntent{}, "invalid submit_code"
		}
		return inboundIntent{typ: proto.TypeSubmitCode, submission: data}, ""
	default:
		return inboundIntent{}, "unknown message type"
	}
}

func (c *conn) handleInbound(raw []byte) ([]byte, error) {
	intent, protocolErr := decodeInbound(raw)
	if protocolErr != "" {
		return encodeError(protocolErr)
	}

	switch intent.typ {
	case proto.TypeJoinQueue:
		if c.enqueue == nil {
			return encodeError("unable to join queue")
		}
		member := redisx.QueueMember{
			UserID:      c.userID,
			PresenceKey: c.presenceKey,
			Route:       c.route,
			Rating:      0,
		}
		if err := c.enqueue(c.ctx, member); err != nil {
			c.logger.Warn("enqueue failed", "user_id", c.userID, "err", err)
			return encodeError("unable to join queue")
		}
		return nil, nil
	case proto.TypeSubmitCode:
		if c.acceptSubmission == nil {
			return encodeError("unable to accept submission")
		}
		matchID, _ := uuid.Parse(intent.submission.MatchID)
		requestID, _ := uuid.Parse(intent.submission.RequestID)
		submissionID, err := c.acceptSubmission(c.ctx, submission.Request{
			PlayerID:  c.userID,
			MatchID:   matchID,
			RequestID: requestID,
			Language:  intent.submission.Language,
			Code:      intent.submission.Code,
		})
		if err != nil {
			c.logger.Warn("accept submission failed", "user_id", c.userID, "err", err)
			return encodeSubmissionError(err)
		}
		return proto.Encode(proto.TypeJudging, proto.JudgingData{
			SubmissionID: submissionID.String(),
		})
	default:
		return encodeError("unknown message type")
	}
}

func decodeEmptyData(raw json.RawMessage) error {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&proto.JoinQueueData{}); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing data")
	}
	return nil
}

func encodeError(message string) ([]byte, error) {
	return proto.Encode(proto.TypeError, proto.ErrorData{Message: message})
}

func encodeSubmissionError(err error) ([]byte, error) {
	switch {
	case errors.Is(err, submission.ErrInvalidRequest):
		return encodeError("invalid request")
	case errors.Is(err, submission.ErrNotMatchPlayer):
		return encodeError("not a match player")
	case errors.Is(err, submission.ErrDeadlinePassed):
		return encodeError("deadline passed")
	case errors.Is(err, submission.ErrMatchNotFound), errors.Is(err, submission.ErrMatchNotActive):
		return encodeError("match not active")
	case errors.Is(err, submission.ErrIdempotencyConflict):
		return encodeError("idempotency conflict")
	default:
		return encodeError("unable to accept submission")
	}
}
