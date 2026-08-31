package proto

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	TypeJoinQueue  = "join_queue"
	TypeSubmitCode = "submit_code"

	TypeMatchStart = "match_start"
	TypeJudging    = "judging"
	TypeResult     = "result"
	TypeMatchEnd   = "match_end"
	TypeError      = "error"

	VerdictPass    = "pass"
	VerdictFail    = "fail"
	VerdictError   = "error"
	VerdictTimeout = "timeout"
	VerdictFailed  = "failed"
	OutcomeWin     = "win"
	OutcomeLoss    = "loss"
	OutcomeDraw    = "draw"

	MaxSubmissionCodeBytes = 64 << 10
)

var eventIDNamespace = uuid.MustParse("ed186aa4-1a2e-5df6-8cc8-16de9f6d82e0")

// StableEventID is a deterministic UUID for a publishable event so retries can
// reuse the same identifier. The name format is kept for Phase 4 result IDs.
func StableEventID(kind string, scope, recipient uuid.UUID) uuid.UUID {
	name := fmt.Sprintf("codeduel:result:%s:%s:%s", kind, scope, recipient)
	return uuid.NewSHA1(eventIDNamespace, []byte(name))
}

type Envelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

type JoinQueueData struct{}

type SubmitCodeData struct {
	MatchID   string `json:"match_id"`
	RequestID string `json:"request_id"`
	Language  string `json:"language"`
	Code      string `json:"code"`
}

type MatchStartData struct {
	MatchID   string    `json:"match_id"`
	ProblemID string    `json:"problem_id"`
	Deadline  time.Time `json:"deadline"`
}

type JudgingData struct {
	SubmissionID string `json:"submission_id"`
}

type ResultData struct {
	EventID      string `json:"event_id"`
	SubmissionID string `json:"submission_id"`
	MatchID      string `json:"match_id"`
	PlayerID     string `json:"player_id"`
	Verdict      string `json:"verdict"`
	TestsPassed  int    `json:"tests_passed"`
	TotalTests   int    `json:"total_tests"`
	WinnerID     string `json:"winner_id,omitempty"`
	Outcome      string `json:"outcome,omitempty"`
}

type MatchEndData struct {
	EventID     string `json:"event_id"`
	MatchID     string `json:"match_id"`
	WinnerID    string `json:"winner_id,omitempty"`
	Outcome     string `json:"outcome"`
	TestsPassed int    `json:"tests_passed"`
	TotalTests  int    `json:"total_tests"`
}

type ErrorData struct {
	Message string `json:"message"`
}

func Encode(typ string, payload any) ([]byte, error) {
	if typ == "" {
		return nil, fmt.Errorf("encode: missing type")
	}
	env := Envelope{Type: typ}
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode %s payload: %w", typ, err)
		}
		env.Data = data
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("encode envelope: %w", err)
	}
	return raw, nil
}

func Decode(raw []byte) (Envelope, error) {
	var env Envelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&env); err != nil {
		return Envelope{}, fmt.Errorf("decode envelope: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Envelope{}, fmt.Errorf("decode envelope: unexpected trailing data")
	}
	if env.Type == "" {
		return Envelope{}, fmt.Errorf("decode envelope: missing type")
	}
	return env, nil
}

func (e Envelope) DecodeData(v any) error {
	if len(e.Data) == 0 || string(e.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(e.Data, v); err != nil {
		return fmt.Errorf("decode %s data: %w", e.Type, err)
	}
	return nil
}

// DecodeSubmitCodeData strictly decodes and validates an inbound submission.
func DecodeSubmitCodeData(raw json.RawMessage) (SubmitCodeData, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return SubmitCodeData{}, errors.New("missing submit_code data")
	}
	if !utf8.Valid(raw) {
		return SubmitCodeData{}, errors.New("submit_code data is not valid UTF-8")
	}

	var data SubmitCodeData
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&data); err != nil {
		return SubmitCodeData{}, fmt.Errorf("decode submit_code data: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return SubmitCodeData{}, errors.New("decode submit_code data: unexpected trailing data")
	}
	if err := data.Validate(); err != nil {
		return SubmitCodeData{}, err
	}
	return data, nil
}

func (d SubmitCodeData) Validate() error {
	if err := validateUUID("match_id", d.MatchID); err != nil {
		return err
	}
	if err := validateUUID("request_id", d.RequestID); err != nil {
		return err
	}
	switch d.Language {
	case "python", "cpp", "java":
	default:
		return errors.New("invalid submit_code language")
	}
	if !utf8.ValidString(d.Code) {
		return errors.New("submit_code code is not valid UTF-8")
	}
	if strings.IndexByte(d.Code, 0) >= 0 {
		return errors.New("submit_code code contains NUL")
	}
	if strings.TrimSpace(d.Code) == "" {
		return errors.New("submit_code code is empty")
	}
	if len(d.Code) > MaxSubmissionCodeBytes {
		return errors.New("submit_code code exceeds size limit")
	}
	return nil
}

func validateUUID(name, raw string) error {
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return fmt.Errorf("invalid submit_code %s", name)
	}
	return nil
}
