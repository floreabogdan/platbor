-- name: CreateProject :one
INSERT INTO projects (id, key, name, description, allow_auto_create, quota_bytes, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetProjectByKey :one
SELECT * FROM projects WHERE key = ?;

-- name: GetProjectByID :one
SELECT * FROM projects WHERE id = ?;

-- name: SetProjectAutoCreate :exec
UPDATE projects SET allow_auto_create = ?, updated_at = ? WHERE key = ?;

-- name: SetProjectQuota :exec
UPDATE projects SET quota_bytes = ?, updated_at = ? WHERE key = ?;

-- name: SetProjectVerificationKey :exec
UPDATE projects SET verification_key = ?, updated_at = ? WHERE key = ?;

-- Keyset pagination on the unique `key` column. The first page passes the empty
-- string, which sorts before any valid key, so a single query serves both the
-- first page and subsequent pages.
-- name: ListProjects :many
SELECT * FROM projects
WHERE key > ?
ORDER BY key ASC
LIMIT ?;

-- name: CountProjects :one
SELECT COUNT(*) FROM projects;
