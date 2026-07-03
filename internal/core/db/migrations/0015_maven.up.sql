-- Maven artifacts stored as path-addressed files, scoped by repository. A Maven
-- repository is a tree of files at their standard layout paths
-- (group/path/artifact/version/artifact-version.ext), plus maven-metadata.xml
-- files that list versions. Platbor stores each PUT file at its path and serves
-- it back; there is no server-side metadata generation, so `mvn deploy` uploads
-- the pom, jar, checksums, and maven-metadata.xml exactly as a plain HTTP repo
-- expects. Files reference the shared content-addressable blob store by digest.
--
-- The Maven coordinates (group_id, artifact_id, version) are parsed from the path
-- for the browser only; an unparseable path still stores fine with empty
-- coordinates. is_metadata marks maven-metadata.xml and its checksums, which are
-- mutable and (for a proxy) never cached as a permanent blob.
--
-- A proxy repository caches immutable artifact files lazily: on a GET miss the
-- file is fetched from the upstream, stored, and its row recorded. Metadata files
-- are always re-fetched fresh from the upstream (they change as versions publish).
CREATE TABLE maven_files (
    id            TEXT PRIMARY KEY,
    repository_id TEXT NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    path          TEXT NOT NULL,            -- full repo path, e.g. com/example/demo/1.0.0/demo-1.0.0.jar
    group_id      TEXT NOT NULL DEFAULT '', -- parsed for the browser, e.g. com.example
    artifact_id   TEXT NOT NULL DEFAULT '', -- parsed, e.g. demo
    version       TEXT NOT NULL DEFAULT '', -- parsed, e.g. 1.0.0 (empty for artifact-level metadata)
    filename      TEXT NOT NULL DEFAULT '', -- last path segment
    is_metadata   INTEGER NOT NULL DEFAULT 0,
    blob_digest   TEXT NOT NULL DEFAULT '',
    size          INTEGER NOT NULL DEFAULT 0,
    sha1          TEXT NOT NULL DEFAULT '',
    md5           TEXT NOT NULL DEFAULT '',
    upstream_url  TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    UNIQUE (repository_id, path)
);

CREATE INDEX idx_maven_files_repo ON maven_files (repository_id);
CREATE INDEX idx_maven_files_ga ON maven_files (repository_id, group_id, artifact_id);
