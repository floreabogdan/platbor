-- Project membership is the unit of RBAC: it grants a user a role within one
-- project. Instance admins (users.is_admin) bypass these checks entirely with
-- full access; every other user's access to a project is exactly what their role
-- here permits, and a user with no row for a project has no access to it:
--   reader     - pull/read artifacts
--   maintainer - reader + push/write artifacts
--   admin      - maintainer + configure the project (repositories, members)
-- The project's creator is enrolled as an admin so a non-instance-admin who
-- creates a project can manage it.
CREATE TABLE project_members (
    project_id TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('reader', 'maintainer', 'admin')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (project_id, user_id)
);

CREATE INDEX idx_project_members_user ON project_members (user_id);
