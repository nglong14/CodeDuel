package reaper

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/nglong14/CodeDuel/internal/proto"
	"github.com/nglong14/CodeDuel/internal/redisx"
)

const (
	eventKindInfrastructureFailed = "infrastructure-failed"
	eventKindMatchFinalized       = "match-finalized"
)

type publishedEvent struct {
	RecipientID uuid.UUID
	Payload     []byte
}

func buildFailedResultEvent(submissionID, matchID, playerID uuid.UUID, totalTests int) (publishedEvent, error) {
	if submissionID == uuid.Nil || matchID == uuid.Nil || playerID == uuid.Nil || totalTests < 0 {
		return publishedEvent{}, errors.New("build failed result event: invalid arguments")
	}
	payload, err := proto.Encode(proto.TypeResult, proto.ResultData{
		EventID:      proto.StableEventID(eventKindInfrastructureFailed, submissionID, playerID).String(),
		SubmissionID: submissionID.String(),
		MatchID:      matchID.String(),
		PlayerID:     playerID.String(),
		Verdict:      proto.VerdictFailed,
		TestsPassed:  0,
		TotalTests:   totalTests,
	})
	if err != nil {
		return publishedEvent{}, fmt.Errorf("build failed result event: %w", err)
	}
	return publishedEvent{RecipientID: playerID, Payload: payload}, nil
}

func buildMatchEndEvents(
	matchID uuid.UUID,
	players [2]uuid.UUID,
	scores [2]int,
	totalTests int,
	winner uuid.NullUUID,
) ([]publishedEvent, error) {
	if matchID == uuid.Nil || totalTests < 0 ||
		players[0] == uuid.Nil || players[1] == uuid.Nil || players[0] == players[1] ||
		scores[0] < 0 || scores[1] < 0 {
		return nil, errors.New("build match end events: invalid arguments")
	}
	if winner.Valid && winner.UUID != players[0] && winner.UUID != players[1] {
		return nil, errors.New("build match end events: winner is not a match player")
	}

	events := make([]publishedEvent, 0, len(players))
	for index, recipientID := range players {
		data := proto.MatchEndData{
			EventID:     proto.StableEventID(eventKindMatchFinalized, matchID, recipientID).String(),
			MatchID:     matchID.String(),
			TestsPassed: scores[index],
			TotalTests:  totalTests,
		}
		if winner.Valid {
			data.WinnerID = winner.UUID.String()
			if recipientID == winner.UUID {
				data.Outcome = proto.OutcomeWin
			} else {
				data.Outcome = proto.OutcomeLoss
			}
		} else {
			data.Outcome = proto.OutcomeDraw
		}
		payload, err := proto.Encode(proto.TypeMatchEnd, data)
		if err != nil {
			return nil, fmt.Errorf("build match end event for %s: %w", recipientID, err)
		}
		events = append(events, publishedEvent{RecipientID: recipientID, Payload: payload})
	}
	return events, nil
}

func (s *service) publishEvents(ctx context.Context, events []publishedEvent) error {
	if len(events) == 0 {
		return nil
	}
	var publishErrors []error
	for _, event := range events {
		if err := s.publish(ctx, redisx.UserChannel(event.RecipientID), event.Payload); err != nil {
			publishErrors = append(publishErrors, fmt.Errorf("publish to %s: %w", event.RecipientID, err))
		}
	}
	return errors.Join(publishErrors...)
}
