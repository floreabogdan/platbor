-- name: UpsertGenericFile :exec
-- Store a file at a path in a repository, replacing any existing file there
-- (generic paths are mutable: a re-upload overwrites).
INSERT INTO generic_files (id, repository_id, path, blob_digest, size, sha256, sha1, md5, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (repository_id, path)
DO UPDATE SET
    blob_digest = excluded.blob_digest,
    size        = excluded.size,
    sha256      = excluded.sha256,
    sha1        = excluded.sha1,
    md5         = excluded.md5,
    updated_at  = excluded.updated_at;

-- name: GetGenericFile :one
SELECT * FROM generic_files
WHERE repository_id = ? AND path = ?;

-- name: DeleteGenericFile :execrows
DELETE FROM generic_files
WHERE repository_id = ? AND path = ?;

-- name: ListGenericBlobDigests :many
-- Distinct blob digests every generic file references, for garbage collection to
-- mark them alongside OCI manifests and npm tarballs (shared CAS across formats).
SELECT DISTINCT blob_digest FROM generic_files;

-- name: ListAllGenericFiles :many
-- Every generic file across all repositories, joined to its repository and
-- project, for the registry browser's generic index. is_proxy is 1 for a proxy
-- repository.
SELECT
    p.key        AS project_key,
    p.name       AS project_name,
    r.key        AS repo_key,
    f.path       AS path,
    f.size       AS size,
    CAST(r.mode = 'proxy' AS INTEGER) AS is_proxy,
    f.updated_at AS updated_at
FROM generic_files f
JOIN repositories r ON r.id = f.repository_id
JOIN projects p ON p.id = r.project_id
ORDER BY p.key ASC, r.key ASC, f.path ASC;
