-- name: UpsertProjectMember :exec
-- Grant a user a role in a project, or change their existing role.
INSERT INTO project_members (project_id, user_id, role, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (project_id, user_id)
DO UPDATE SET role = excluded.role, updated_at = excluded.updated_at;

-- name: GetProjectMemberRole :one
-- The user's role in a project; no row means no membership (no access).
SELECT role FROM project_members
WHERE project_id = ? AND user_id = ?;

-- name: ListProjectMembers :many
-- Every member of a project with their account, ordered by username.
SELECT m.user_id, u.username, u.email, m.role, m.created_at, m.updated_at
FROM project_members m
JOIN users u ON u.id = m.user_id
WHERE m.project_id = ?
ORDER BY u.username ASC;

-- name: DeleteProjectMember :execrows
DELETE FROM project_members
WHERE project_id = ? AND user_id = ?;
