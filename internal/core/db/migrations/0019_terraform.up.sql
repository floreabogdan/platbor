-- Terraform modules and their versions, scoped by repository. Platbor implements
-- the Terraform module registry protocol (modules only -- providers need GPG
-- signing-key plumbing and are out of scope). A module is addressed as
-- <host>/<namespace>/<name>/<provider>; the namespace maps to a Platbor project,
-- so terraform's instance-global service discovery (/.well-known/terraform.json)
-- can coexist with multi-tenant projects.
--
-- Terraform has no standard module upload API (the public registry is VCS-backed),
-- so Platbor accepts a module archive via its own upload endpoint and serves it
-- through the registry protocol. Archives live in the shared content-addressable
-- blob store. There is no proxy mode: the public registry resolves modules from
-- git, which is out of scope for a pull-through cache.
CREATE TABLE terraform_modules (
    id            TEXT PRIMARY KEY,
    repository_id TEXT NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    provider      TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    UNIQUE (repository_id, name, provider)
);

CREATE INDEX idx_terraform_modules_repo ON terraform_modules (repository_id);

CREATE TABLE terraform_versions (
    id          TEXT PRIMARY KEY,
    module_id   TEXT NOT NULL REFERENCES terraform_modules (id) ON DELETE CASCADE,
    version     TEXT NOT NULL,
    blob_digest TEXT NOT NULL DEFAULT '',
    size        INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL,
    UNIQUE (module_id, version)
);

CREATE INDEX idx_terraform_versions_module ON terraform_versions (module_id);
