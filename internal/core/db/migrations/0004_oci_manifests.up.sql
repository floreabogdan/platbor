-- OCI image manifests and their tags, scoped to a project. A manifest is stored
-- by its content digest, and its exact bytes are kept verbatim (`payload`) so a
-- GET returns them byte-for-byte — the digest must stay stable, so we never
-- re-serialize. Layer and config blobs live in the content-addressable blob
-- store; only the manifest document itself is held here.
--
-- Tags are the mutable, human-facing names that point at a manifest digest
-- within a repository. Retagging just repoints the row.
CREATE TABLE oci_manifests (
    id         TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    repository TEXT NOT NULL,            -- OCI name minus the leading project component
    digest     TEXT NOT NULL,           -- sha256:<hex> of payload
    media_type TEXT NOT NULL,
    payload    BLOB NOT NULL,           -- exact manifest bytes, served verbatim
    size       INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (project_id, repository, digest)
);

CREATE INDEX idx_oci_manifests_repo ON oci_manifests (project_id, repository);

CREATE TABLE oci_tags (
    project_id TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    repository TEXT NOT NULL,
    tag        TEXT NOT NULL,
    digest     TEXT NOT NULL,           -- points at an oci_manifests.digest in the same repo
    updated_at TEXT NOT NULL,
    PRIMARY KEY (project_id, repository, tag)
);

CREATE INDEX idx_oci_tags_digest ON oci_tags (project_id, repository, digest);
