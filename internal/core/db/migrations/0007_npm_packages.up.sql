-- npm packages, their versions, and dist-tags, scoped to a project and a
-- repository (the <repo> segment of /npm/<project>/<repo>). One npm registry
-- endpoint is a (project, repository) pair; packages live inside it by name.
--
-- A package is the named unit clients install (e.g. "lodash" or the scoped
-- "@acme/widgets"). Each published version carries the exact version metadata
-- object npm sent (stored verbatim as JSON so the packument round-trips) plus a
-- pointer to its tarball in the shared content-addressable blob store. The
-- tarball is keyed there by sha256; npm's own dist.shasum (sha1) and
-- dist.integrity (sha512 SRI) are recomputed at publish and stored so the
-- packument reports authoritative values.
CREATE TABLE npm_packages (
    id         TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    repository TEXT NOT NULL,             -- the <repo> path segment
    name       TEXT NOT NULL,             -- package name, incl. @scope/ prefix
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (project_id, repository, name)
);

CREATE TABLE npm_versions (
    id             TEXT PRIMARY KEY,
    package_id     TEXT NOT NULL REFERENCES npm_packages (id) ON DELETE CASCADE,
    version        TEXT NOT NULL,         -- semver, e.g. 1.2.3
    manifest       BLOB NOT NULL,         -- the version metadata object (JSON), served verbatim
    tarball_digest TEXT NOT NULL,         -- sha256:<hex> into the blob store
    tarball_size   INTEGER NOT NULL,
    shasum         TEXT NOT NULL,         -- sha1 hex (npm dist.shasum)
    integrity      TEXT NOT NULL DEFAULT '', -- sha512 SRI (npm dist.integrity)
    created_at     TEXT NOT NULL,
    UNIQUE (package_id, version)
);

CREATE INDEX idx_npm_versions_package ON npm_versions (package_id);

-- dist-tags are the mutable, human-facing labels ("latest", "next", ...) that
-- point at a version within a package. Publishing updates "latest"; `npm
-- dist-tag` manages the rest.
CREATE TABLE npm_dist_tags (
    package_id TEXT NOT NULL REFERENCES npm_packages (id) ON DELETE CASCADE,
    tag        TEXT NOT NULL,
    version    TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (package_id, tag)
);
