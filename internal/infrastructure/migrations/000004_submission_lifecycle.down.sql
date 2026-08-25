BEGIN;

DROP INDEX IF EXISTS idx_submissions_pending_dispatch;

ALTER TABLE matches DROP CONSTRAINT IF EXISTS matches_winner_player_fkey;

ALTER TABLE submissions
    DROP CONSTRAINT IF EXISTS submissions_match_player_fkey,
    DROP CONSTRAINT IF EXISTS submissions_player_request_id_key,
    DROP CONSTRAINT IF EXISTS submissions_lifecycle_check,
    DROP CONSTRAINT IF EXISTS submissions_tests_passed_check,
    DROP CONSTRAINT IF EXISTS submissions_attempts_check,
    DROP CONSTRAINT IF EXISTS submissions_failure_kind_check,
    DROP CONSTRAINT IF EXISTS submissions_result_terminal_check,
    DROP CONSTRAINT IF EXISTS submissions_status_check;

UPDATE submissions
SET result = COALESCE(result, 'pending');

ALTER TABLE submissions
    ALTER COLUMN result SET NOT NULL,
    ALTER COLUMN result SET DEFAULT 'pending',
    ADD CONSTRAINT submissions_result_check
        CHECK (result IN ('pending', 'pass', 'fail', 'error', 'timeout', 'failed')),
    DROP COLUMN finished_at,
    DROP COLUMN last_enqueued_at,
    DROP COLUMN lease_until,
    DROP COLUMN attempt_token,
    DROP COLUMN attempts,
    DROP COLUMN failure_kind,
    DROP COLUMN status,
    DROP COLUMN request_id;

COMMIT;
