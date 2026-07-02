-- name: UpsertManifest :exec
-- Store a manifest by digest. Re-pushing identical content is a no-op, so an
-- image can be tagged repeatedly without duplicating the payload.
INSERT INTO oci_manifests (id, project_id, repository, digest, media_type, payload, size, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (project_id, repository, digest) DO NOTHING;

-- name: GetManifest :one
SELECT * FROM oci_manifests
WHERE project_id = ? AND repository = ? AND digest = ?;

-- name: ManifestExists :one
SELECT COUNT(*) FROM oci_manifests
WHERE project_id = ? AND repository = ? AND digest = ?;

-- name: DeleteManifest :execrows
DELETE FROM oci_manifests
WHERE project_id = ? AND repository = ? AND digest = ?;

-- name: UpsertTag :exec
INSERT INTO oci_tags (project_id, repository, tag, digest, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (project_id, repository, tag)
DO UPDATE SET digest = excluded.digest, updated_at = excluded.updated_at;

-- name: GetTag :one
SELECT * FROM oci_tags
WHERE project_id = ? AND repository = ? AND tag = ?;

-- Keyset pagination on `tag`: the empty string sorts before any tag, so the
-- first page and subsequent pages share one query.
-- name: ListTags :many
SELECT tag FROM oci_tags
WHERE project_id = ? AND repository = ? AND tag > ?
ORDER BY tag ASC
LIMIT ?;

-- name: DeleteTag :execrows
DELETE FROM oci_tags
WHERE project_id = ? AND repository = ? AND tag = ?;

-- name: DeleteTagsForDigest :exec
DELETE FROM oci_tags
WHERE project_id = ? AND repository = ? AND digest = ?;
