-- Go modules cached from an upstream GOPROXY (proxy.golang.org), scoped by
-- repository. The Go module proxy protocol is read-only over HTTP: a client
-- fetches <module>/@v/<version>.info, .mod, and .zip (all immutable per version),
-- plus <module>/@v/list and <module>/@latest (mutable). Platbor supports Go in
-- proxy mode only -- modules originate from version control, there is no upload
-- API, so a local Go repository has nothing to serve.
--
-- Immutable per-version files are cached lazily into the shared blob store on
-- first fetch and recorded here; list and @latest are always fetched fresh and
-- never stored. The module path is stored decoded (canonical, mixed-case) for the
-- browser, while path is the escaped request path used as the cache key.
CREATE TABLE go_files (
    id            TEXT PRIMARY KEY,
    repository_id TEXT NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    module        TEXT NOT NULL,            -- decoded module path, e.g. github.com/Azure/go-autorest
    version       TEXT NOT NULL,            -- e.g. v1.2.3
    kind          TEXT NOT NULL,            -- info | mod | zip
    path          TEXT NOT NULL,            -- escaped cache key, e.g. github.com/!azure/.../@v/v1.2.3.zip
    blob_digest   TEXT NOT NULL DEFAULT '',
    size          INTEGER NOT NULL DEFAULT 0,
    upstream_url  TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    UNIQUE (repository_id, path)
);

CREATE INDEX idx_go_files_repo ON go_files (repository_id);
CREATE INDEX idx_go_files_module ON go_files (repository_id, module);
