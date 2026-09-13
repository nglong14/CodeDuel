-- Remove the fixed Alice and Bob development identities seeded by 000003 so they
-- never exist in a production database. Local fixtures now live in the
-- development-only seed at deploy/dev/seed_users.sql (see `make seed-dev`).
--
-- A fresh database has no rows referencing these UUIDs, so the DELETE succeeds and
-- leaves no development identities behind. An existing development database that has
-- played matches will still reference them through matches.winner_id,
-- match_players.user_id, or submissions.player_id (all ON DELETE NO ACTION); in that
-- case this migration fails with an actionable error instead of a raw foreign-key
-- violation. active_match_players cascades on user delete and needs no check.
BEGIN;

DO $migration$
DECLARE
    dev_ids UUID[] := ARRAY[
        '11111111-1111-1111-1111-111111111111'::uuid,
        '22222222-2222-2222-2222-222222222222'::uuid
    ];
    referencing_rows INT;
BEGIN
    SELECT
        (SELECT count(*) FROM match_players WHERE user_id = ANY(dev_ids))
      + (SELECT count(*) FROM submissions WHERE player_id = ANY(dev_ids))
      + (SELECT count(*) FROM matches WHERE winner_id = ANY(dev_ids))
    INTO referencing_rows;

    IF referencing_rows > 0 THEN
        RAISE EXCEPTION
            '000011_remove_dev_identities cannot delete the Alice/Bob fixtures: % dependent match, player, or submission rows still reference them',
            referencing_rows
            USING HINT = 'Reset the disposable development database (make reset), or delete the referencing matches and submissions before migrating.';
    END IF;

    DELETE FROM users WHERE id = ANY(dev_ids);
END
$migration$;

COMMIT;
