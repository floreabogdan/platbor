-- Repositories are the typed, configured artifact containers inside a project.
-- A project is the tenant boundary; a repository is where artifacts of ONE
-- format actually live, created and configured before anything is pushed. Each
-- repository has a format (oci, npm, nuget, generic), a mode (local, or a
-- pull-through proxy of an upstream), and its own retention policy. Artifacts
-- are addressed as /<format>/<project>/<repo>/...
--
-- Proxy and retention configuration live here, per repository (they were briefly
-- per-project). A project can hold many repositories of different formats.
CREATE TABLE repositories (
    id                TEXT PRIMARY KEY,
    project_id        TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    key               TEXT NOT NULL,          -- url-safe, unique within the project
    name              TEXT NOT NULL,
    format            TEXT NOT NULL,          -- oci | npm | nuget | generic
    mode              TEXT NOT NULL,          -- local | proxy
    upstream_url      TEXT NOT NULL DEFAULT '', -- proxy: the mirrored registry
    upstream_username TEXT NOT NULL DEFAULT '',
    upstream_password TEXT NOT NULL DEFAULT '',
    keep_last         INTEGER NOT NULL DEFAULT 0, -- retention: newest N kept; 0 = unlimited
    delete_untagged   INTEGER NOT NULL DEFAULT 0, -- retention: sweep untagged OCI manifests
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL,
    UNIQUE (project_id, key)
);

CREATE INDEX idx_repositories_project ON repositories (project_id);
