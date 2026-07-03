-- name: UpsertManifest :exec
-- Store a manifest by digest. Re-pushing identical content is a no-op, so an
-- image can be tagged repeatedly without duplicating the payload. subject and
-- artifact_type are denormalized from the payload for the referrers API.
INSERT INTO oci_manifests (id, project_id, repository, digest, media_type, payload, size, subject, artifact_type, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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

-- name: ListAllRepositories :many
-- Every repository across all projects, with tag and manifest counts, for the
-- registry browser's project-grouped index. A repository exists once it has at
-- least one manifest.
SELECT
    p.key                    AS project_key,
    p.name                   AS project_name,
    m.repository             AS repository,
    COUNT(DISTINCT m.digest) AS manifest_count,
    (SELECT COUNT(*) FROM oci_tags t
       WHERE t.project_id = m.project_id AND t.repository = m.repository) AS tag_count,
    MAX(m.created_at)        AS updated_at
FROM oci_manifests m
JOIN projects p ON p.id = m.project_id
GROUP BY m.project_id, m.repository
ORDER BY p.key ASC, m.repository ASC;

-- name: ListManifestPayloads :many
-- Every manifest's raw bytes, for garbage collection to mark the config and
-- layer blobs each one references. Blobs are a global CAS, so this spans all
-- projects.
SELECT payload FROM oci_manifests;

-- name: ListManifestSizing :many
-- Every manifest's project, repository, digest, stored size, and payload, so the
-- browser can compute per-repository storage: the sum of the distinct blobs each
-- repository references. Blobs are a shared CAS, so a layer used by two
-- repositories is counted once per repository (logical size), not physically.
SELECT
    p.key        AS project_key,
    m.repository AS repository,
    m.digest     AS digest,
    m.size       AS size,
    m.payload    AS payload
FROM oci_manifests m
JOIN projects p ON p.id = m.project_id;

-- name: ListReferrers :many
-- Manifests whose subject is the given digest (a subject's signatures, SBOMs,
-- and other attestations) for the referrers API. Newest first.
SELECT digest, media_type, size, artifact_type, payload
FROM oci_manifests
WHERE project_id = ? AND repository = ? AND subject = ?
ORDER BY created_at DESC;

-- name: CountRepositories :one
-- Distinct repositories across all projects, for the dashboard summary.
SELECT COUNT(*) FROM (SELECT DISTINCT project_id, repository FROM oci_manifests) AS repos;

-- name: CountTags :one
SELECT COUNT(*) FROM oci_tags;

-- name: ListTagsWithManifest :many
-- Tags in a repository joined to the manifest each points at, so the browser can
-- show media type and size without a second round-trip. Newest push first.
SELECT
    t.tag        AS tag,
    t.digest     AS digest,
    t.updated_at AS updated_at,
    m.media_type AS media_type,
    m.size       AS size,
    m.payload    AS payload
FROM oci_tags t
JOIN oci_manifests m
  ON m.project_id = t.project_id AND m.repository = t.repository AND m.digest = t.digest
WHERE t.project_id = ? AND t.repository = ?
ORDER BY t.updated_at DESC, t.tag ASC;
