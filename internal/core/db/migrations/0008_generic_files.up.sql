-- Generic artifacts: arbitrary versioned files stored under an opaque path
-- within a project repository (/generic/<project>/<repo>/<path>). Unlike OCI
-- manifests and npm versions, a generic file has no format semantics -- it is
-- just bytes at a path -- so overwriting a path replaces it (PUT is idempotent
-- on the path). The bytes live in the shared content-addressable blob store;
-- this table holds the path -> blob mapping plus the checksums clients verify
-- downloads against (sha256 doubles as the blob key's hex).
CREATE TABLE generic_files (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    repository  TEXT NOT NULL,
    path        TEXT NOT NULL,          -- file path within the repo, e.g. tool/1.0.0/tool.tgz
    blob_digest TEXT NOT NULL,          -- sha256:<hex> into the blob store
    size        INTEGER NOT NULL,
    sha256      TEXT NOT NULL,          -- hex
    sha1        TEXT NOT NULL,          -- hex
    md5         TEXT NOT NULL,          -- hex
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    UNIQUE (project_id, repository, path)
);

CREATE INDEX idx_generic_files_repo ON generic_files (project_id, repository);
