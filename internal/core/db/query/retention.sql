-- name: UpsertRetentionPolicy :exec
INSERT INTO retention_policies (project_id, keep_last, delete_untagged, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT (project_id)
DO UPDATE SET keep_last = excluded.keep_last, delete_untagged = excluded.delete_untagged, updated_at = excluded.updated_at;

-- name: GetRetentionPolicy :one
SELECT * FROM retention_policies WHERE project_id = ?;

-- name: ListRetentionPolicies :many
-- Every project with an effective policy (keeps some, or sweeps untagged), with
-- its key, for a retention run.
SELECT rp.project_id, rp.keep_last, rp.delete_untagged, p.key AS project_key
FROM retention_policies rp
JOIN projects p ON p.id = rp.project_id
WHERE rp.keep_last > 0 OR rp.delete_untagged = 1;
