-- name: InsertAuditEntry :one
INSERT INTO audit_log (id, project_id, actor, action, target_type, target_id, metadata, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListAuditByProject :many
SELECT * FROM audit_log
WHERE project_id = ?
ORDER BY created_at DESC, id DESC
LIMIT ?;

-- name: ListRecentActivity :many
-- Instance-wide recent mutations for the dashboard feed, joined to the project
-- they touched (project_id is nullable for instance-level events).
SELECT
    a.actor,
    a.action,
    a.target_type,
    a.target_id,
    a.metadata,
    a.created_at,
    p.key  AS project_key,
    p.name AS project_name
FROM audit_log a
LEFT JOIN projects p ON p.id = a.project_id
ORDER BY a.created_at DESC, a.id DESC
LIMIT ?;
