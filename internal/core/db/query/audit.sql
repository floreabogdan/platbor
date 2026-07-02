-- name: InsertAuditEntry :one
INSERT INTO audit_log (id, project_id, actor, action, target_type, target_id, metadata, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListAuditByProject :many
SELECT * FROM audit_log
WHERE project_id = ?
ORDER BY created_at DESC, id DESC
LIMIT ?;
