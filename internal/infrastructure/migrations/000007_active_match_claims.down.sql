BEGIN;

DROP TRIGGER IF EXISTS matches_sync_active_claims ON matches;
DROP FUNCTION IF EXISTS sync_active_match_claims();
DROP TRIGGER IF EXISTS match_players_claim_active ON match_players;
DROP FUNCTION IF EXISTS claim_active_match_player();

DROP INDEX IF EXISTS idx_submissions_match_player_created;
DROP INDEX IF EXISTS idx_match_players_user_match;
DROP INDEX IF EXISTS idx_active_match_players_match;
DROP TABLE IF EXISTS active_match_players;

COMMIT;
