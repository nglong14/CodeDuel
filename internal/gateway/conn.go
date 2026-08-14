package gateway

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/nglong14/CodeDuel/internal/proto"
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
	userID    uuid.UUID
	ws        *websocket.Conn
	send      chan []byte
	closed    chan struct{}
	writeMu   sync.Mutex
	closeOnce sync.Once
	registry  *Registry
	onClose   func()
}

func newConn(userID uuid.UUID, ws *websocket.Conn, registry *Registry) *conn {
	return &conn{
		userID:   userID,
		ws:       ws,
		send:     make(chan []byte, sendBufSize),
		closed:   make(chan struct{}),
		registry: registry,
	}
}

func (c *conn) serve() {
	go c.writePump()
	c.readPump()
	c.cleanup()
}

func (c *conn) cleanup() {
	c.registry.Remove(c)
	if c.onClose != nil {
		c.onClose()
	}
	c.close()
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
		return c.ws.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, raw, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		resp, err := handleInbound(raw)
		if err != nil {
			return
		}
		c.Send(resp)
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

func handleInbound(raw []byte) ([]byte, error) {
	env, err := proto.Decode(raw)
	if err != nil {
		return encodeError("invalid message")
	}

	switch env.Type {
	case proto.TypeJoinQueue:
		if err := env.DecodeData(&proto.JoinQueueData{}); err != nil {
			return encodeError("invalid join_queue")
		}
		return proto.Encode(proto.TypeJoinQueue, nil)
	case proto.TypeSubmitCode:
		var data proto.SubmitCodeData
		if err := env.DecodeData(&data); err != nil {
			return encodeError("invalid submit_code")
		}
		if data.Language == "" || data.Code == "" {
			return encodeError("invalid submit_code")
		}
		return proto.Encode(proto.TypeJudging, proto.JudgingData{
			SubmissionID: uuid.New().String(),
		})
	default:
		return encodeError("unknown message type")
	}
}

func encodeError(message string) ([]byte, error) {
	return proto.Encode(proto.TypeError, proto.ErrorData{Message: message})
}
