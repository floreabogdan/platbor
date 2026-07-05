-- name: CreateRepository :one
INSERT INTO repositories (
    id, project_id, key, name, format, mode,
    upstream_url, upstream_username, upstream_password,
    keep_last, delete_untagged, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetRepository :one
SELECT * FROM repositories
WHERE project_id = ? AND key = ?;

-- name: GetRepositoryByID :one
SELECT * FROM repositories WHERE id = ?;

-- name: ListRepositoriesByProject :many
SELECT * FROM repositories
WHERE project_id = ?
ORDER BY key ASC;

-- name: ListAllRepositoryRows :many
-- Every repository across all projects, with its project key, for the browser
-- and instance-wide operations (retention, listing).
SELECT r.*, p.key AS project_key, p.name AS project_name
FROM repositories r
JOIN projects p ON p.id = r.project_id
ORDER BY p.key ASC, r.key ASC;

-- name: ListRepositoriesWithPolicy :many
-- Repositories that have an effective retention policy, for a retention run.
SELECT * FROM repositories
WHERE keep_last > 0 OR delete_untagged = 1;

-- name: UpdateRepository :one
UPDATE repositories
SET name = ?, upstream_url = ?, upstream_username = ?, upstream_password = ?,
    keep_last = ?, delete_untagged = ?, updated_at = ?
WHERE id = ?
RETURNING *;

-- name: DeleteRepository :execrows
DELETE FROM repositories
WHERE project_id = ? AND key = ?;

-- name: AddVirtualMember :exec
INSERT INTO virtual_repo_members (virtual_repo_id, member_repo_id, position)
VALUES (?, ?, ?);

-- name: ListVirtualMembers :many
-- Member repositories of a virtual repository, in configured order.
SELECT r.* FROM virtual_repo_members m
JOIN repositories r ON r.id = m.member_repo_id
WHERE m.virtual_repo_id = ?
ORDER BY m.position ASC;

-- name: DeleteVirtualMembers :exec
DELETE FROM virtual_repo_members WHERE virtual_repo_id = ?;
