package judge

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/nglong14/CodeDuel/internal/proto"
)

const (
	resultKindSubmission = "submission-result"
	resultKindWinner     = "winner-result"
)

type resultEvent struct {
	RecipientID uuid.UUID
	Payload     []byte
}

func terminalResultFromOutcome(outcome ExecutionOutcome, totalTests int) (terminalResult, error) {
	result := terminalResult{TestsPassed: outcome.TestsPassed}
	switch outcome.Kind {
	case OutcomePass:
		result.Verdict = proto.VerdictPass
	case OutcomeWrongAnswer:
		result.Verdict = proto.VerdictFail
		result.FailureKind = "wrong_answer"
	case OutcomeCompileError:
		result.Verdict = proto.VerdictError
		result.FailureKind = "compile_error"
	case OutcomeRuntimeError:
		result.Verdict = proto.VerdictError
		result.FailureKind = "runtime_error"
	case OutcomeOutputLimit:
		result.Verdict = proto.VerdictError
		result.FailureKind = "output_limit"
	case OutcomeTimeout:
		result.Verdict = proto.VerdictTimeout
	default:
		return terminalResult{}, fmt.Errorf("map execution outcome: unknown kind %q", outcome.Kind)
	}
	if err := validateTerminalResult(result, totalTests); err != nil {
		return terminalResult{}, fmt.Errorf("map execution outcome: %w", err)
	}
	return result, nil
}

func buildResultEvents(completed completedSubmission) ([]resultEvent, error) {
	if completed.SubmissionID == uuid.Nil || completed.MatchID == uuid.Nil || completed.PlayerID == uuid.Nil ||
		completed.TotalTests <= 0 || completed.TestsPassed < 0 || completed.TestsPassed > completed.TotalTests {
		return nil, errors.New("build result events: invalid completed submission")
	}
	if completed.Players[0] == uuid.Nil || completed.Players[1] == uuid.Nil ||
		completed.Players[0] == completed.Players[1] ||
		(completed.PlayerID != completed.Players[0] && completed.PlayerID != completed.Players[1]) {
		return nil, errors.New("build result events: invalid match players")
	}

	recipients := []uuid.UUID{completed.PlayerID}
	kind := resultKindSubmission
	if completed.WinnerID != uuid.Nil {
		if completed.WinnerID != completed.Players[0] && completed.WinnerID != completed.Players[1] {
			return nil, errors.New("build result events: winner is not a match player")
		}
		recipients = completed.Players[:]
		kind = resultKindWinner
	}

	events := make([]resultEvent, 0, len(recipients))
	for _, recipientID := range recipients {
		data := proto.ResultData{
			EventID:      stableResultEventID(kind, completed.SubmissionID, recipientID).String(),
			SubmissionID: completed.SubmissionID.String(),
			MatchID:      completed.MatchID.String(),
			PlayerID:     completed.PlayerID.String(),
			Verdict:      completed.Verdict,
			TestsPassed:  completed.TestsPassed,
			TotalTests:   completed.TotalTests,
		}
		if completed.WinnerID != uuid.Nil {
			data.WinnerID = completed.WinnerID.String()
			if recipientID == completed.WinnerID {
				data.Outcome = proto.OutcomeWin
			} else {
				data.Outcome = proto.OutcomeLoss
			}
		}
		payload, err := proto.Encode(proto.TypeResult, data)
		if err != nil {
			return nil, fmt.Errorf("build result event for %s: %w", recipientID, err)
		}
		events = append(events, resultEvent{RecipientID: recipientID, Payload: payload})
	}
	return events, nil
}

func stableResultEventID(kind string, submissionID, recipientID uuid.UUID) uuid.UUID {
	return proto.StableEventID(kind, submissionID, recipientID)
}
