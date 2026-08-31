CREATE INDEX idx_submissions_expired_leases
    ON submissions (lease_until)
    WHERE status = 'running';

CREATE INDEX idx_submissions_match_open
    ON submissions (match_id)
    WHERE status IN ('pending', 'running');
