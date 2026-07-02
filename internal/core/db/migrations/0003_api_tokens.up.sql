-- Personal access tokens for machine/CLI/CI authentication. Like sessions,
-- only a hash of the token is stored; the prefix (first chars of the raw
-- token) is kept in cleartext so the owner can recognize a token in a list
-- without it being usable.
CREATE TABLE api_tokens (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    prefix     TEXT NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT
);

CREATE INDEX idx_api_tokens_user ON api_tokens (user_id);
