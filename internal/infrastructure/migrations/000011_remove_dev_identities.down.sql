-- Restore the Alice and Bob development identities so rolling back reproduces the
-- pre-000011 state. Columns satisfy the normalized email and display-name
-- constraints added in 000006. ON CONFLICT keeps the rollback idempotent when the
-- development seed has already recreated them.
INSERT INTO users (id, email, display_name)
VALUES
    ('11111111-1111-1111-1111-111111111111', 'alice@codeduel.dev', 'alice'),
    ('22222222-2222-2222-2222-222222222222', 'bob@codeduel.dev', 'bob')
ON CONFLICT (id) DO NOTHING;
