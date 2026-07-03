-- PyPI packages and their distribution files, scoped by repository. A PyPI
-- "package" is a project (name-normalized per PEP 503); each upload is a file (an
-- sdist or a wheel), several of which may share a version. Files reference the
-- shared content-addressable blob store by digest, like every other format.
--
-- A proxy repository caches upstream files lazily: on a simple-index read the
-- file rows are recorded with their upstream_url and hash but an empty
-- blob_digest, then filled on first download. blob_digest stays empty until the
-- content is actually cached, so GC only ever marks real, present blobs.
CREATE TABLE pypi_packages (
    id            TEXT PRIMARY KEY,
    repository_id TEXT NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    name          TEXT NOT NULL,           -- PEP 503 normalized (lowercase, [-_.]+ -> -)
    name_original TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    UNIQUE (repository_id, name)
);

CREATE INDEX idx_pypi_packages_repo ON pypi_packages (repository_id);

CREATE TABLE pypi_files (
    id              TEXT PRIMARY KEY,
    package_id      TEXT NOT NULL REFERENCES pypi_packages (id) ON DELETE CASCADE,
    version         TEXT NOT NULL,
    filename        TEXT NOT NULL,
    blob_digest     TEXT NOT NULL DEFAULT '', -- empty until cached (proxy) or on local upload set immediately
    size            INTEGER NOT NULL DEFAULT 0,
    sha256          TEXT NOT NULL DEFAULT '',
    requires_python TEXT NOT NULL DEFAULT '',
    upstream_url    TEXT NOT NULL DEFAULT '', -- proxy: where to fetch the content on a cache miss
    created_at      TEXT NOT NULL,
    UNIQUE (package_id, filename)
);

CREATE INDEX idx_pypi_files_package ON pypi_files (package_id);
