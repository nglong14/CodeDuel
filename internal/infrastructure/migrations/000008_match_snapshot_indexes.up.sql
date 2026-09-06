CREATE INDEX idx_submissions_match_player_best
    ON submissions (match_id, player_id, tests_passed DESC)
    WHERE status = 'completed';
