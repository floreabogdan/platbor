-- RubyGems gems and their versions, scoped by repository. RubyGems uses the
-- "compact index" protocol: /versions lists every gem and its versions,
-- /info/<gem> gives one line per version (deps + requirements + checksum), /names
-- lists gem names, and .gem files download from /gems/<full-name>.gem. Pushing is
-- a POST of the raw .gem to /api/v1/gems.
--
-- Each version stores the precomputed pieces of its compact-index info line
-- (number, deps, requirements) plus the .gem blob in the shared content-
-- addressable store. A proxy repository mirrors an upstream (rubygems.org): the
-- index is fetched fresh, versions are recorded for the browser and for lazy
-- caching, and each .gem is cached on first download.
CREATE TABLE gem_gems (
    id            TEXT PRIMARY KEY,
    repository_id TEXT NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    UNIQUE (repository_id, name)
);

CREATE INDEX idx_gem_gems_repo ON gem_gems (repository_id);

CREATE TABLE gem_versions (
    id          TEXT PRIMARY KEY,
    gem_id      TEXT NOT NULL REFERENCES gem_gems (id) ON DELETE CASCADE,
    version     TEXT NOT NULL,            -- e.g. 1.2.3
    platform    TEXT NOT NULL DEFAULT 'ruby',
    number      TEXT NOT NULL,            -- version, or version-platform for non-ruby; the compact-index token
    full_name   TEXT NOT NULL,            -- name-number; the .gem filename base
    info_deps   TEXT NOT NULL DEFAULT '', -- comma-joined dep:req for the info line
    info_reqs   TEXT NOT NULL DEFAULT '', -- checksum:...,ruby:...,rubygems:... for the info line
    sha256      TEXT NOT NULL DEFAULT '',
    blob_digest TEXT NOT NULL DEFAULT '', -- empty until cached (proxy) or set on push
    size        INTEGER NOT NULL DEFAULT 0,
    yanked      INTEGER NOT NULL DEFAULT 0,
    upstream_url TEXT NOT NULL DEFAULT '', -- proxy: where to fetch the .gem on a miss
    created_at  TEXT NOT NULL,
    UNIQUE (gem_id, full_name)
);

CREATE INDEX idx_gem_versions_gem ON gem_versions (gem_id);
