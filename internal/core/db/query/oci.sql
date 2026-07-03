-- name: UpsertManifest :exec
-- Store a manifest by digest. Re-pushing identical content is a no-op, so an
-- image can be tagged repeatedly without duplicating the payload. subject and
-- artifact_type are denormalized from the payload for the referrers API.
INSERT INTO oci_manifests (id, repository_id, repository, digest, media_type, payload, size, subject, artifact_type, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (repository_id, repository, digest) DO NOTHING;

-- name: GetManifest :one
SELECT * FROM oci_manifests
WHERE repository_id = ? AND repository = ? AND digest = ?;

-- name: ManifestExists :one
SELECT COUNT(*) FROM oci_manifests
WHERE repository_id = ? AND repository = ? AND digest = ?;

-- name: DeleteManifest :execrows
DELETE FROM oci_manifests
WHERE repository_id = ? AND repository = ? AND digest = ?;

-- name: UpsertTag :exec
INSERT INTO oci_tags (repository_id, repository, tag, digest, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (repository_id, repository, tag)
DO UPDATE SET digest = excluded.digest, updated_at = excluded.updated_at;

-- name: GetTag :one
SELECT * FROM oci_tags
WHERE repository_id = ? AND repository = ? AND tag = ?;

-- Keyset pagination on `tag`: the empty string sorts before any tag, so the
-- first page and subsequent pages share one query.
-- name: ListTags :many
SELECT tag FROM oci_tags
WHERE repository_id = ? AND repository = ? AND tag > ?
ORDER BY tag ASC
LIMIT ?;

-- name: DeleteTag :execrows
DELETE FROM oci_tags
WHERE repository_id = ? AND repository = ? AND tag = ?;

-- name: DeleteTagsForDigest :exec
DELETE FROM oci_tags
WHERE repository_id = ? AND repository = ? AND digest = ?;

-- name: ListAllRepositories :many
-- Every OCI image across all repositories, joined to its typed repository and
-- project, with tag and manifest counts, for the browser's index. is_proxy is 1
-- for a proxy repository.
SELECT
    p.key                    AS project_key,
    p.name                   AS project_name,
    r.key                    AS repo_key,
    m.repository             AS repository,
    COUNT(DISTINCT m.digest) AS manifest_count,
    (SELECT COUNT(*) FROM oci_tags t
       WHERE t.repository_id = m.repository_id AND t.repository = m.repository) AS tag_count,
    CAST(r.mode = 'proxy' AS INTEGER) AS is_proxy,
    MAX(m.created_at)        AS updated_at
FROM oci_manifests m
JOIN repositories r ON r.id = m.repository_id
JOIN projects p ON p.id = r.project_id
GROUP BY m.repository_id, m.repository
ORDER BY p.key ASC, r.key ASC, m.repository ASC;

-- name: ListManifestPayloads :many
-- Every manifest's raw bytes, for garbage collection to mark the config and
-- layer blobs each one references. Blobs are a global CAS, so this spans all
-- repositories.
SELECT payload FROM oci_manifests;

-- name: ListManifestSizing :many
-- Every manifest's project, repo, image, digest, stored size, and payload, so
-- the browser can compute per-image storage: the sum of the distinct blobs each
-- image references. Blobs are a shared CAS, so a layer used by two images is
-- counted once per image (logical size), not physically.
SELECT
    p.key        AS project_key,
    r.key        AS repo_key,
    m.repository AS repository,
    m.digest     AS digest,
    m.size       AS size,
    m.payload    AS payload
FROM oci_manifests m
JOIN repositories r ON r.id = m.repository_id
JOIN projects p ON p.id = r.project_id;

-- name: ListReferrers :many
-- Manifests whose subject is the given digest (a subject's signatures, SBOMs,
-- and other attestations) for the referrers API. Newest first.
SELECT digest, media_type, size, artifact_type, payload
FROM oci_manifests
WHERE repository_id = ? AND repository = ? AND subject = ?
ORDER BY created_at DESC;

-- name: CountRepositories :one
-- Distinct images across all repositories, for the dashboard summary.
SELECT COUNT(*) FROM (SELECT DISTINCT repository_id, repository FROM oci_manifests) AS repos;

-- name: CountTags :one
SELECT COUNT(*) FROM oci_tags;

-- name: ListTagsForRetention :many
-- Every tag in a repository, grouped by image and newest first, so a
-- keep-last-N policy can keep the newest tags and delete the rest.
SELECT repository, tag, updated_at FROM oci_tags
WHERE repository_id = ?
ORDER BY repository ASC, updated_at DESC, tag DESC;

-- name: ListUntaggedManifests :many
-- Manifests in a repository that no tag points at, for the delete-untagged policy.
SELECT m.repository AS repository, m.digest AS digest FROM oci_manifests m
WHERE m.repository_id = ?
  AND NOT EXISTS (
    SELECT 1 FROM oci_tags t
    WHERE t.repository_id = m.repository_id AND t.repository = m.repository AND t.digest = m.digest
  );

-- name: ListTagsWithManifest :many
-- Tags in an image joined to the manifest each points at, so the browser can
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
  ON m.repository_id = t.repository_id AND m.repository = t.repository AND m.digest = t.digest
WHERE t.repository_id = ? AND t.repository = ?
ORDER BY t.updated_at DESC, t.tag ASC;
