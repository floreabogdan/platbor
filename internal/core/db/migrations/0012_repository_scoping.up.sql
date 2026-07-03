-- R2: artifacts move from project scope to repository scope. Every artifact now
-- lives in a typed repository (see 0011), so its metadata is keyed by
-- repository_id rather than project_id. Proxy and retention configuration move
-- onto the repository too, so their standalone tables are dropped.
--
-- This is a pre-release reset: the artifact tables are recreated empty (blobs
-- remain in the content-addressable store and are reclaimed by GC). Projects,
-- users, tokens, and audit history are untouched. Repositories auto-create on
-- first push, so re-pushing repopulates everything with zero config.

-- Config now lives on repositories.
DROP TABLE retention_policies;
DROP TABLE registry_proxies;

-- Per-project governance: when 1 (default) a push to an unknown repo path
-- auto-creates a local repo of that format (zero-config); when 0, pushes must
-- target a pre-created repository and an unknown repo is rejected.
ALTER TABLE projects ADD COLUMN allow_auto_create INTEGER NOT NULL DEFAULT 1;

-- --- OCI ---
DROP TABLE oci_tags;
DROP TABLE oci_manifests;

CREATE TABLE oci_manifests (
    id            TEXT PRIMARY KEY,
    repository_id TEXT NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    repository    TEXT NOT NULL,           -- the OCI image name within the repo
    digest        TEXT NOT NULL,
    media_type    TEXT NOT NULL,
    payload       BLOB NOT NULL,
    size          INTEGER NOT NULL,
    subject       TEXT NOT NULL DEFAULT '',
    artifact_type TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL,
    UNIQUE (repository_id, repository, digest)
);
CREATE INDEX idx_oci_manifests_repo ON oci_manifests (repository_id, repository);
CREATE INDEX idx_oci_manifests_subject ON oci_manifests (repository_id, repository, subject);

CREATE TABLE oci_tags (
    repository_id TEXT NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    repository    TEXT NOT NULL,
    tag           TEXT NOT NULL,
    digest        TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    PRIMARY KEY (repository_id, repository, tag)
);
CREATE INDEX idx_oci_tags_digest ON oci_tags (repository_id, repository, digest);

-- --- npm ---
DROP TABLE npm_dist_tags;
DROP TABLE npm_versions;
DROP TABLE npm_packages;

CREATE TABLE npm_packages (
    id            TEXT PRIMARY KEY,
    repository_id TEXT NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    UNIQUE (repository_id, name)
);
CREATE TABLE npm_versions (
    id             TEXT PRIMARY KEY,
    package_id     TEXT NOT NULL REFERENCES npm_packages (id) ON DELETE CASCADE,
    version        TEXT NOT NULL,
    manifest       BLOB NOT NULL,
    tarball_digest TEXT NOT NULL,
    tarball_size   INTEGER NOT NULL,
    shasum         TEXT NOT NULL,
    integrity      TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL,
    UNIQUE (package_id, version)
);
CREATE INDEX idx_npm_versions_package ON npm_versions (package_id);
CREATE TABLE npm_dist_tags (
    package_id TEXT NOT NULL REFERENCES npm_packages (id) ON DELETE CASCADE,
    tag        TEXT NOT NULL,
    version    TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (package_id, tag)
);

-- --- NuGet ---
DROP TABLE nuget_versions;
DROP TABLE nuget_packages;

CREATE TABLE nuget_packages (
    id            TEXT PRIMARY KEY,
    repository_id TEXT NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    id_lower      TEXT NOT NULL,
    id_original   TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    UNIQUE (repository_id, id_lower)
);
CREATE TABLE nuget_versions (
    id            TEXT PRIMARY KEY,
    package_id    TEXT NOT NULL REFERENCES nuget_packages (id) ON DELETE CASCADE,
    version       TEXT NOT NULL,
    version_lower TEXT NOT NULL,
    nupkg_digest  TEXT NOT NULL,
    nupkg_size    INTEGER NOT NULL,
    nuspec        BLOB NOT NULL,
    created_at    TEXT NOT NULL,
    UNIQUE (package_id, version_lower)
);
CREATE INDEX idx_nuget_versions_package ON nuget_versions (package_id);

-- --- generic ---
DROP TABLE generic_files;

CREATE TABLE generic_files (
    id            TEXT PRIMARY KEY,
    repository_id TEXT NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    path          TEXT NOT NULL,
    blob_digest   TEXT NOT NULL,
    size          INTEGER NOT NULL,
    sha256        TEXT NOT NULL,
    sha1          TEXT NOT NULL,
    md5           TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    UNIQUE (repository_id, path)
);
CREATE INDEX idx_generic_files_repo ON generic_files (repository_id);
