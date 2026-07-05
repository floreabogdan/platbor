-- Project webhooks. A webhook subscribes a project to its mutation events, which
-- are sourced from the audit log rather than emitted by each adapter -- every
-- mutation already writes an audit entry transactionally, so the audit log is the
-- event stream, and webhooks need no changes to any format adapter.
--
-- events is a comma-separated list of action prefixes to match (e.g.
-- "oci.,generic."), or "*" for all. secret signs deliveries (HMAC-SHA256).
CREATE TABLE webhooks (
    id         TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    url        TEXT NOT NULL,
    secret     TEXT NOT NULL,
    events     TEXT NOT NULL DEFAULT '*',
    active     INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX idx_webhooks_project ON webhooks (project_id);

-- The dispatcher's single-row cursor: the last audit entry it has delivered, so
-- it resumes across restarts and never re-delivers. Seeded to the newest audit
-- entry on first run so historical activity is not replayed to new webhooks.
CREATE TABLE webhook_cursor (
    id              INTEGER PRIMARY KEY CHECK (id = 1),
    last_created_at TEXT NOT NULL DEFAULT '',
    last_id         TEXT NOT NULL DEFAULT ''
);

INSERT INTO webhook_cursor (id, last_created_at, last_id) VALUES (1, '', '');
