-- A pull-through proxy turns a project into a read-only mirror of an upstream
-- OCI registry: on a cache miss Platbor fetches the manifest or blob from the
-- upstream, stores it locally, then serves it. One row per proxy project; a
-- project with no row here is an ordinary local project.
--
-- Credentials are optional (public upstreams such as Docker Hub and ghcr.io need
-- none for public content). They are stored as given for now; encrypting them at
-- rest is a later hardening step noted in the roadmap.
CREATE TABLE registry_proxies (
    project_id   TEXT PRIMARY KEY REFERENCES projects (id) ON DELETE CASCADE,
    upstream_url TEXT NOT NULL,            -- e.g. https://registry-1.docker.io
    username     TEXT NOT NULL DEFAULT '', -- optional basic creds for the upstream token endpoint
    password     TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);
