-- Retention policies prune old artifacts to bound storage. One optional policy
-- per project: keep_last caps how many of the newest versions/tags each artifact
-- keeps (0 = keep everything), and delete_untagged removes OCI manifests that no
-- tag points at. Pruning deletes metadata rows and their tags; the underlying
-- blobs are reclaimed later by garbage collection (never inline -- shared blobs
-- make inline deletion a correctness trap). A project with no row here has no
-- retention and keeps all versions.
CREATE TABLE retention_policies (
    project_id      TEXT PRIMARY KEY REFERENCES projects (id) ON DELETE CASCADE,
    keep_last       INTEGER NOT NULL DEFAULT 0,  -- newest N kept per artifact; 0 = unlimited
    delete_untagged INTEGER NOT NULL DEFAULT 0,  -- 1 = sweep OCI manifests with no tag
    updated_at      TEXT NOT NULL
);
