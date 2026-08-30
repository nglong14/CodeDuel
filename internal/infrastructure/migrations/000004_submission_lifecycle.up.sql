BEGIN;

ALTER TABLE submissions
    ADD COLUMN request_id UUID,
    ADD COLUMN status TEXT NOT NULL DEFAULT 'pending',
    ADD COLUMN failure_kind TEXT,
    ADD COLUMN attempts INT NOT NULL DEFAULT 0,
    ADD COLUMN attempt_token UUID,
    ADD COLUMN lease_until TIMESTAMPTZ,
    ADD COLUMN last_enqueued_at TIMESTAMPTZ,
    ADD COLUMN finished_at TIMESTAMPTZ;

ALTER TABLE submissions DROP CONSTRAINT submissions_result_check;
ALTER TABLE submissions ALTER COLUMN result DROP NOT NULL;
ALTER TABLE submissions ALTER COLUMN result DROP DEFAULT;

UPDATE submissions
SET request_id = id,
    status = CASE WHEN result = 'pending' THEN 'pending' ELSE 'completed' END,
    result = NULLIF(result, 'pending'),
    finished_at = CASE WHEN result = 'pending' THEN NULL ELSE created_at END;

ALTER TABLE submissions
    ALTER COLUMN request_id SET NOT NULL,
    ADD CONSTRAINT submissions_status_check
        CHECK (status IN ('pending', 'running', 'completed')),
    ADD CONSTRAINT submissions_result_terminal_check
        CHECK (result IS NULL OR result IN ('pass', 'fail', 'error', 'timeout', 'failed')),
    ADD CONSTRAINT submissions_failure_kind_check
        CHECK (failure_kind IS NULL OR failure_kind IN (
            'wrong_answer', 'compile_error', 'runtime_error', 'output_limit', 'infrastructure_error'
        )),
    ADD CONSTRAINT submissions_attempts_check CHECK (attempts >= 0),
    ADD CONSTRAINT submissions_tests_passed_check CHECK (tests_passed >= 0),
    ADD CONSTRAINT submissions_lifecycle_check CHECK (
        (status IN ('pending', 'running') AND result IS NULL AND finished_at IS NULL)
        OR
        (status = 'completed' AND result IS NOT NULL AND finished_at IS NOT NULL)
    ),
    ADD CONSTRAINT submissions_player_request_id_key UNIQUE (player_id, request_id),
    ADD CONSTRAINT submissions_match_player_fkey
        FOREIGN KEY (match_id, player_id) REFERENCES match_players (match_id, user_id);

UPDATE matches m
SET winner_id = NULL
WHERE winner_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1
      FROM match_players mp
      WHERE mp.match_id = m.id AND mp.user_id = m.winner_id
  );

ALTER TABLE matches
    ADD CONSTRAINT matches_winner_player_fkey
        FOREIGN KEY (id, winner_id) REFERENCES match_players (match_id, user_id);

CREATE INDEX idx_submissions_pending_dispatch
    ON submissions (last_enqueued_at, created_at)
    WHERE status = 'pending';

COMMIT;
