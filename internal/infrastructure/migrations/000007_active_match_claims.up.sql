BEGIN;

CREATE TABLE active_match_players (
    user_id  UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    match_id UUID NOT NULL,
    CONSTRAINT active_match_players_membership_fkey
        FOREIGN KEY (match_id, user_id)
        REFERENCES match_players(match_id, user_id)
        ON DELETE CASCADE
);

CREATE INDEX idx_active_match_players_match
    ON active_match_players (match_id);

CREATE INDEX idx_match_players_user_match
    ON match_players (user_id, match_id);

CREATE INDEX idx_submissions_match_player_created
    ON submissions (match_id, player_id, created_at, id);

CREATE FUNCTION claim_active_match_player() RETURNS trigger AS $$
DECLARE
    match_status TEXT;
BEGIN
    SELECT status INTO STRICT match_status
    FROM matches
    WHERE id = NEW.match_id
    FOR SHARE;

    IF match_status = 'active' THEN
        INSERT INTO active_match_players (user_id, match_id)
        VALUES (NEW.user_id, NEW.match_id);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER match_players_claim_active
AFTER INSERT ON match_players
FOR EACH ROW EXECUTE FUNCTION claim_active_match_player();

CREATE FUNCTION sync_active_match_claims() RETURNS trigger AS $$
BEGIN
    IF NEW.status = 'finished' THEN
        DELETE FROM active_match_players WHERE match_id = NEW.id;
    ELSIF OLD.status = 'finished' AND NEW.status = 'active' THEN
        INSERT INTO active_match_players (user_id, match_id)
        SELECT user_id, match_id
        FROM match_players
        WHERE match_id = NEW.id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER matches_sync_active_claims
AFTER UPDATE OF status ON matches
FOR EACH ROW
WHEN (OLD.status IS DISTINCT FROM NEW.status)
EXECUTE FUNCTION sync_active_match_claims();

INSERT INTO active_match_players (user_id, match_id)
SELECT mp.user_id, mp.match_id
FROM match_players mp
JOIN matches m ON m.id = mp.match_id
WHERE m.status = 'active';

COMMIT;
