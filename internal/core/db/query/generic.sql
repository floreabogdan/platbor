-- name: UpsertGenericFile :exec
-- Store a file at a path, replacing any existing file there (generic paths are
-- mutable: a re-upload overwrites).
INSERT INTO generic_files (id, project_id, path, blob_digest, size, sha256, sha1, md5, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (project_id, path)
DO UPDATE SET
    blob_digest = excluded.blob_digest,
    size        = excluded.size,
    sha256      = excluded.sha256,
    sha1        = excluded.sha1,
    md5         = excluded.md5,
    updated_at  = excluded.updated_at;

-- name: GetGenericFile :one
SELECT * FROM generic_files
WHERE project_id = ? AND path = ?;

-- name: DeleteGenericFile :execrows
DELETE FROM generic_files
WHERE project_id = ? AND path = ?;

-- name: ListGenericBlobDigests :many
-- Distinct blob digests every generic file references, for garbage collection to
-- mark them alongside OCI manifests and npm tarballs (shared CAS across formats).
SELECT DISTINCT blob_digest FROM generic_files;

-- name: ListAllGenericFiles :many
-- Every generic file across all projects, with a proxy flag, for the registry
-- browser's generic index. is_proxy is 1 when the file's project is a mirror.
SELECT
    p.key        AS project_key,
    p.name       AS project_name,
    f.path       AS path,
    f.size       AS size,
    CAST(rp.project_id IS NOT NULL AS INTEGER) AS is_proxy,
    f.updated_at AS updated_at
FROM generic_files f
JOIN projects p ON p.id = f.project_id
LEFT JOIN registry_proxies rp ON rp.project_id = f.project_id
ORDER BY p.key ASC, f.path ASC;
