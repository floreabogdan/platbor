-- Projects are the root scoping entity: every domain table references a project.
-- Timestamps are RFC 3339 UTC strings (API convention); SQLite has no native
-- datetime and this keeps the wire format and storage identical.
CREATE TABLE projects (
    id          TEXT PRIMARY KEY,
    key         TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

-- Audit log for every mutation. project_id is nullable so instance-level events
-- (e.g. admin bootstrap) can be recorded; ON DELETE SET NULL preserves history
-- if a project is removed. metadata is a JSON object.
CREATE TABLE audit_log (
    id          TEXT PRIMARY KEY,
    project_id  TEXT REFERENCES projects (id) ON DELETE SET NULL,
    actor       TEXT NOT NULL,
    action      TEXT NOT NULL,
    target_type TEXT NOT NULL DEFAULT '',
    target_id   TEXT NOT NULL DEFAULT '',
    metadata    TEXT NOT NULL DEFAULT '{}',
    created_at  TEXT NOT NULL
);

CREATE INDEX idx_audit_log_project_created ON audit_log (project_id, created_at);
