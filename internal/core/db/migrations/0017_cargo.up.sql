-- Cargo crates and their versions, scoped by repository. Cargo uses the sparse
-- registry protocol: a config.json at the registry root, per-crate index files
-- (newline-delimited JSON, one line per version) at a sharded path, and .crate
-- downloads. Publishing is a binary PUT to /api/v1/crates/new.
--
-- Each version stores its precomputed index line (the JSON cargo reads to resolve
-- the dependency graph) plus the .crate blob in the shared content-addressable
-- store. A proxy repository mirrors an upstream sparse index (index.crates.io):
-- the index is fetched fresh, versions are recorded for the browser and for lazy
-- caching, and each .crate is cached on first download.
CREATE TABLE cargo_crates (
    id            TEXT PRIMARY KEY,
    repository_id TEXT NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    name          TEXT NOT NULL,       -- original crate name
    name_lower    TEXT NOT NULL,       -- lowercased, for the sharded index path
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    UNIQUE (repository_id, name_lower)
);

CREATE INDEX idx_cargo_crates_repo ON cargo_crates (repository_id);

CREATE TABLE cargo_versions (
    id          TEXT PRIMARY KEY,
    crate_id    TEXT NOT NULL REFERENCES cargo_crates (id) ON DELETE CASCADE,
    version     TEXT NOT NULL,
    index_line  TEXT NOT NULL DEFAULT '', -- the JSON index entry cargo consumes
    cksum       TEXT NOT NULL DEFAULT '', -- sha256 hex of the .crate
    blob_digest TEXT NOT NULL DEFAULT '', -- empty until cached (proxy) or set on publish
    size        INTEGER NOT NULL DEFAULT 0,
    yanked      INTEGER NOT NULL DEFAULT 0,
    upstream_url TEXT NOT NULL DEFAULT '', -- proxy: where to fetch the .crate on a miss
    created_at  TEXT NOT NULL,
    UNIQUE (crate_id, version)
);

CREATE INDEX idx_cargo_versions_crate ON cargo_versions (crate_id);
