-- Identity layer. Users and sessions are instance-level, not project-scoped:
-- a user exists before any project and belongs to the whole instance; project
-- membership (a later slice) is the project-scoped join on top of this.
CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    email         TEXT NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL,
    is_admin      INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

-- Sessions store only a hash of the cookie value, so a database read cannot
-- reconstruct a live session token.
CREATE TABLE sessions (
    id         TEXT PRIMARY KEY,
    token_hash TEXT NOT NULL UNIQUE,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);

CREATE INDEX idx_sessions_user ON sessions (user_id);
