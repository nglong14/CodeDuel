package redisx

import (
	"testing"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestDecodeJudgeJob(t *testing.T) {
	submissionID := uuid.New()
	tests := []struct {
		name   string
		values map[string]any
		valid  bool
	}{
		{
			name: "valid",
			values: map[string]any{
				"schema_version": "1",
				"submission_id":  submissionID.String(),
			},
			valid: true,
		},
		{
			name: "unknown field",
			values: map[string]any{
				"schema_version": "1",
				"submission_id":  submissionID.String(),
				"extra":          "value",
			},
		},
		{
			name: "invalid version",
			values: map[string]any{
				"schema_version": "2",
				"submission_id":  submissionID.String(),
			},
		},
		{
			name: "invalid submission ID",
			values: map[string]any{
				"schema_version": "1",
				"submission_id":  "not-a-uuid",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := decodeJudgeJob(redis.XMessage{ID: "1-0", Values: tt.values})
			if tt.valid {
				if job.DecodeErr != nil || job.SubmissionID != submissionID {
					t.Fatalf("decoded job = %#v", job)
				}
				return
			}
			if job.DecodeErr == nil {
				t.Fatalf("decoded job = %#v, want error", job)
			}
			if job.EntryID != "1-0" {
				t.Fatalf("entry ID = %q, want 1-0", job.EntryID)
			}
		})
	}
}
