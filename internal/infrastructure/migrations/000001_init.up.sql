CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT UNIQUE,
    password_hash TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE problems (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title      TEXT NOT NULL,
    statement  TEXT NOT NULL,
    test_cases JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE matches (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    problem_id UUID NOT NULL REFERENCES problems(id),
    status     TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'finished')),
    winner_id  UUID REFERENCES users(id),
    deadline   TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_matches_active_deadline ON matches (deadline) WHERE status = 'active';

CREATE TABLE match_players (
    match_id UUID NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
    user_id  UUID NOT NULL REFERENCES users(id),
    slot     SMALLINT NOT NULL CHECK (slot > 0),
    PRIMARY KEY (match_id, user_id),
    UNIQUE (match_id, slot)
);

CREATE TABLE submissions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    match_id     UUID NOT NULL REFERENCES matches(id),
    player_id    UUID NOT NULL REFERENCES users(id),
    code         TEXT NOT NULL,
    language     TEXT NOT NULL CHECK (language IN ('python', 'cpp', 'java')),
    result       TEXT NOT NULL DEFAULT 'pending'
                 CHECK (result IN ('pending', 'pass', 'fail', 'error', 'timeout', 'failed')),
    tests_passed INT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_submissions_match ON submissions (match_id);
