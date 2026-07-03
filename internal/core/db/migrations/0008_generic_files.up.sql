-- Generic artifacts: arbitrary versioned files stored under an opaque path
-- within a project (/generic/<project>/<path>). The project IS the generic
-- repository -- files live directly under it by path, with no intermediate
-- repository level. Unlike OCI manifests and npm versions, a generic file has
-- no format semantics -- it is just bytes at a path -- so overwriting a path
-- replaces it (PUT is idempotent on the path). The bytes live in the shared
-- content-addressable blob store; this table holds the path -> blob mapping
-- plus the checksums clients verify downloads against (sha256 doubles as the
-- blob key's hex).
CREATE TABLE generic_files (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    path        TEXT NOT NULL,          -- file path within the project, e.g. tools/tool-1.0.0.tgz
    blob_digest TEXT NOT NULL,          -- sha256:<hex> into the blob store
    size        INTEGER NOT NULL,
    sha256      TEXT NOT NULL,          -- hex
    sha1        TEXT NOT NULL,          -- hex
    md5         TEXT NOT NULL,          -- hex
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    UNIQUE (project_id, path)
);

CREATE INDEX idx_generic_files_project ON generic_files (project_id);
