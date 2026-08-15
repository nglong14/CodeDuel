package proto

import (
	"encoding/json"
	"fmt"
	"time"
)

const (
	TypeJoinQueue  = "join_queue"
	TypeSubmitCode = "submit_code"

	TypeMatchStart = "match_start"
	TypeJudging    = "judging"
	TypeResult     = "result"
	TypeError      = "error"
)

type Envelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

type JoinQueueData struct{}

type SubmitCodeData struct {
	Language string `json:"language"`
	Code     string `json:"code"`
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
	MatchID     string `json:"match_id"`
	WinnerID    string `json:"winner_id,omitempty"`
	TestsPassed int    `json:"tests_passed"`
	Outcome     string `json:"outcome"`
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
	if err := json.Unmarshal(raw, &env); err != nil {
		return Envelope{}, fmt.Errorf("decode envelope: %w", err)
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
