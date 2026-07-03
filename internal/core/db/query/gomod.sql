-- name: UpsertGoFile :exec
-- Record a cached, immutable Go module file (info, mod, or zip) at its escaped
-- request path, filling the blob on first fetch.
INSERT INTO go_files (id, repository_id, module, version, kind, path, blob_digest, size, upstream_url, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (repository_id, path)
DO UPDATE SET
    module      = excluded.module,
    version     = excluded.version,
    kind        = excluded.kind,
    blob_digest = excluded.blob_digest,
    size        = excluded.size,
    upstream_url = excluded.upstream_url,
    updated_at  = excluded.updated_at;

-- name: GetGoFile :one
SELECT * FROM go_files
WHERE repository_id = ? AND path = ?;

-- name: DeleteGoFile :execrows
DELETE FROM go_files
WHERE repository_id = ? AND path = ?;

-- name: ListGoBlobDigests :many
-- Distinct blob digests every cached Go file references, for garbage collection.
SELECT DISTINCT blob_digest FROM go_files WHERE blob_digest != '';

-- name: ListGoFilesForModule :many
-- Every cached file of one module, for its detail page.
SELECT module, version, kind, size, created_at
FROM go_files
WHERE repository_id = ? AND module = ?
ORDER BY version DESC, kind ASC;

-- name: ListGoFilesForRetention :many
-- Every cached file in a repository, grouped by module and newest first, for
-- keep-last-N-versions pruning.
SELECT module AS module, version AS version, path AS path, created_at AS created_at
FROM go_files
WHERE repository_id = ?
ORDER BY module ASC, created_at DESC;

-- name: ListAllGoModules :many
-- Every cached Go module across all repositories, with version count, total
-- cached size, and a proxy flag, for the browser's project-grouped index.
SELECT
    p.key         AS project_key,
    p.name        AS project_name,
    r.key         AS repo_key,
    f.module      AS module,
    COUNT(DISTINCT f.version) AS version_count,
    CAST(COALESCE(SUM(f.size), 0) AS INTEGER) AS size_bytes,
    CAST(r.mode = 'proxy' AS INTEGER) AS is_proxy,
    CAST(MAX(f.updated_at) AS TEXT) AS updated_at
FROM go_files f
JOIN repositories r ON r.id = f.repository_id
JOIN projects p ON p.id = r.project_id
GROUP BY r.id, f.module
ORDER BY p.key ASC, r.key ASC, f.module ASC;
