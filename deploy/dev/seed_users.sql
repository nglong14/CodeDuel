-- Development-only fixtures. NOT part of the embedded production migration sequence.
--
-- These are the fixed Alice and Bob identities that duelcli and Postman use to mint
-- local JWTs. They are password-less lookup accounts and cannot log in through the
-- HTTP auth flow. Apply with `make seed-dev` after `make migrate`.
--
-- Never load this file against a production database.
INSERT INTO users (id, email, display_name)
VALUES
    ('11111111-1111-1111-1111-111111111111', 'alice@codeduel.dev', 'alice'),
    ('22222222-2222-2222-2222-222222222222', 'bob@codeduel.dev', 'bob')
ON CONFLICT (id) DO NOTHING;
