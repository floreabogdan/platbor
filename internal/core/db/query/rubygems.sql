-- name: UpsertGem :one
-- Ensure the gem row for (repository, name) exists, returning its id.
INSERT INTO gem_gems (id, repository_id, name, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (repository_id, name)
DO UPDATE SET updated_at = excluded.updated_at
RETURNING id;

-- name: GemVersionExists :one
SELECT COUNT(*) FROM gem_versions v
JOIN gem_gems g ON g.id = v.gem_id
WHERE g.repository_id = ? AND g.name = ? AND v.full_name = ?;

-- name: InsertGemVersion :exec
-- Record a gem version. On conflict (a proxy re-listing, or a download filling a
-- cached row) update metadata and only overwrite the blob when a real digest is
-- supplied, so a fresh index read never clears a cached blob.
INSERT INTO gem_versions (id, gem_id, version, platform, number, full_name, info_deps, info_reqs, sha256, blob_digest, size, yanked, upstream_url, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (gem_id, full_name)
DO UPDATE SET
    version      = excluded.version,
    platform     = excluded.platform,
    number       = excluded.number,
    info_deps    = excluded.info_deps,
    info_reqs    = excluded.info_reqs,
    sha256       = excluded.sha256,
    size         = CASE WHEN excluded.blob_digest != '' THEN excluded.size ELSE gem_versions.size END,
    yanked       = excluded.yanked,
    upstream_url = excluded.upstream_url,
    blob_digest  = CASE WHEN excluded.blob_digest != '' THEN excluded.blob_digest ELSE gem_versions.blob_digest END;

-- name: SetGemVersionBlob :exec
-- Fill a proxied version's cached blob after download. full_name is unique within
-- a repository (name is unique per repo, and full_name is name-number), so the
-- repository plus full_name identify the row without the gem name.
UPDATE gem_versions SET blob_digest = ?, size = ?
WHERE full_name = ? AND gem_id IN (SELECT id FROM gem_gems WHERE repository_id = ?);

-- name: SetGemYanked :execrows
UPDATE gem_versions SET yanked = ?
WHERE gem_id = (SELECT id FROM gem_gems WHERE repository_id = ? AND name = ?)
  AND number = ?;

-- name: ListGemInfoVersions :many
-- Every version of a gem (by name) for the /info/<gem> file, oldest first.
SELECT v.number, v.info_deps, v.info_reqs, v.yanked
FROM gem_versions v
JOIN gem_gems g ON g.id = v.gem_id
WHERE g.repository_id = ? AND g.name = ?
ORDER BY v.created_at ASC;

-- name: GetGemFile :one
-- Resolve a .gem by its full name (name-number) for download.
SELECT v.gem_id, v.blob_digest, v.size, v.sha256, v.upstream_url
FROM gem_versions v
JOIN gem_gems g ON g.id = v.gem_id
WHERE g.repository_id = ? AND v.full_name = ?;

-- name: ListGemNames :many
SELECT name FROM gem_gems WHERE repository_id = ? ORDER BY name ASC;

-- name: ListGemVersionsForIndex :many
-- Every non-yanked version across all gems in a repository, for the /versions
-- compact-index file. Grouped by gem name.
SELECT g.name AS name, v.number AS number, v.info_deps AS info_deps, v.info_reqs AS info_reqs
FROM gem_versions v
JOIN gem_gems g ON g.id = v.gem_id
WHERE g.repository_id = ? AND v.yanked = 0
ORDER BY g.name ASC, v.created_at ASC;

-- name: ListGemBlobDigests :many
SELECT DISTINCT blob_digest FROM gem_versions WHERE blob_digest != '';

-- name: ListGemVersionsForRetention :many
SELECT g.name AS name, v.number AS number, v.full_name AS full_name, v.created_at AS created_at
FROM gem_versions v
JOIN gem_gems g ON g.id = v.gem_id
WHERE g.repository_id = ?
ORDER BY g.name ASC, v.created_at DESC;

-- name: DeleteGemVersion :execrows
DELETE FROM gem_versions
WHERE gem_id = (SELECT id FROM gem_gems WHERE repository_id = ? AND name = ?)
  AND full_name = ?;

-- name: ListGemVersionsForGem :many
-- Every version of a gem for its detail page.
SELECT v.number, v.version, v.platform, v.size, v.yanked, v.sha256
FROM gem_versions v
JOIN gem_gems g ON g.id = v.gem_id
WHERE g.repository_id = ? AND g.name = ?
ORDER BY v.created_at DESC;

-- name: ListAllGems :many
-- Every gem across all repositories, with version count, total cached size, and a
-- proxy flag, for the browser's project-grouped index.
SELECT
    p.key         AS project_key,
    p.name        AS project_name,
    r.key         AS repo_key,
    g.name        AS gem_name,
    COUNT(v.id)   AS version_count,
    CAST(COALESCE(SUM(v.size), 0) AS INTEGER) AS size_bytes,
    CAST(r.mode = 'proxy' AS INTEGER) AS is_proxy,
    g.updated_at  AS updated_at
FROM gem_gems g
JOIN repositories r ON r.id = g.repository_id
JOIN projects p ON p.id = r.project_id
LEFT JOIN gem_versions v ON v.gem_id = g.id
GROUP BY g.id
ORDER BY p.key ASC, r.key ASC, g.name ASC;
