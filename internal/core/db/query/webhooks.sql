-- name: CreateWebhook :one
INSERT INTO webhooks (id, project_id, url, secret, events, active, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, 1, ?, ?)
RETURNING *;

-- name: ListWebhooksByProject :many
SELECT * FROM webhooks WHERE project_id = ? ORDER BY created_at ASC;

-- name: GetWebhook :one
SELECT * FROM webhooks WHERE id = ? AND project_id = ?;

-- name: DeleteWebhook :execrows
DELETE FROM webhooks WHERE id = ? AND project_id = ?;

-- name: ListActiveWebhooksForProject :many
-- Active webhooks for a project, for the dispatcher to match against an event.
SELECT id, url, secret, events FROM webhooks WHERE project_id = ? AND active = 1;

-- name: ListAuditSince :many
-- Audit entries after the dispatcher's cursor, oldest first, with the project key
-- for the delivery payload. Keyset on (created_at, id): created_at is RFC3339Nano
-- (fixed width, so it orders lexically) and id breaks ties.
SELECT a.id, a.project_id, p.key AS project_key, a.actor, a.action, a.target_type, a.target_id, a.metadata, a.created_at
FROM audit_log a
JOIN projects p ON p.id = a.project_id
WHERE a.project_id IS NOT NULL
  AND (a.created_at > sqlc.arg(cursor_created_at)
       OR (a.created_at = sqlc.arg(cursor_created_at) AND a.id > sqlc.arg(cursor_id)))
ORDER BY a.created_at ASC, a.id ASC
LIMIT sqlc.arg(row_limit);

-- name: MaxAuditCursor :one
-- The newest audit entry, to seed the dispatcher cursor on first run.
SELECT created_at, id FROM audit_log ORDER BY created_at DESC, id DESC LIMIT 1;

-- name: GetWebhookCursor :one
SELECT last_created_at, last_id FROM webhook_cursor WHERE id = 1;

-- name: SetWebhookCursor :exec
UPDATE webhook_cursor SET last_created_at = ?, last_id = ? WHERE id = 1;
