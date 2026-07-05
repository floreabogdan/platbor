-- Per-project storage quota. quota_bytes caps a project's logical storage (the
-- summed size of the artifacts across its repositories); 0 means unlimited (the
-- default, so existing projects and zero-config stay unbounded). Enforcement is
-- at the shared write path: once a project is at or over its quota, further
-- writes are rejected until space is freed.
ALTER TABLE projects ADD COLUMN quota_bytes INTEGER NOT NULL DEFAULT 0;
