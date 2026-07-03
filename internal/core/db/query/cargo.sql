-- name: UpsertCargoCrate :one
-- Ensure the crate row for (repository, lowercased name) exists, returning its id.
INSERT INTO cargo_crates (id, repository_id, name, name_lower, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (repository_id, name_lower)
DO UPDATE SET updated_at = excluded.updated_at, name = excluded.name
RETURNING id;

-- name: CargoVersionExists :one
SELECT COUNT(*) FROM cargo_versions v
JOIN cargo_crates c ON c.id = v.crate_id
WHERE c.repository_id = ? AND c.name_lower = ? AND v.version = ?;

-- name: InsertCargoVersion :exec
-- Record a crate version. On conflict (a proxy re-listing, or a download filling
-- a cached row) update metadata and only overwrite the blob when a real digest is
-- supplied, so a fresh index read never clears a cached blob.
INSERT INTO cargo_versions (id, crate_id, version, index_line, cksum, blob_digest, size, yanked, upstream_url, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (crate_id, version)
DO UPDATE SET
    index_line   = excluded.index_line,
    cksum        = excluded.cksum,
    size         = CASE WHEN excluded.blob_digest != '' THEN excluded.size ELSE cargo_versions.size END,
    yanked       = excluded.yanked,
    upstream_url = excluded.upstream_url,
    blob_digest  = CASE WHEN excluded.blob_digest != '' THEN excluded.blob_digest ELSE cargo_versions.blob_digest END;

-- name: SetCargoVersionBlob :exec
-- Fill a proxied version's cached blob after downloading the .crate.
UPDATE cargo_versions SET blob_digest = ?, size = ?
WHERE crate_id = (SELECT id FROM cargo_crates WHERE repository_id = ? AND name_lower = ?)
  AND version = ?;

-- name: SetCargoYanked :execrows
UPDATE cargo_versions SET yanked = ?
WHERE crate_id = (SELECT id FROM cargo_crates WHERE repository_id = ? AND name_lower = ?)
  AND version = ?;

-- name: ListCargoIndexLines :many
-- Every version's index line for a crate (by lowercased name), newest last, for
-- the sparse index file. yanked is returned so the served line reflects the
-- current flag without rewriting the stored line on every yank/unyank.
SELECT v.index_line, v.version, v.yanked
FROM cargo_versions v
JOIN cargo_crates c ON c.id = v.crate_id
WHERE c.repository_id = ? AND c.name_lower = ?
ORDER BY v.created_at ASC;

-- name: GetCargoVersion :one
-- Resolve a crate version to its content for download (by lowercased name).
SELECT v.crate_id, v.blob_digest, v.size, v.cksum, v.upstream_url
FROM cargo_versions v
JOIN cargo_crates c ON c.id = v.crate_id
WHERE c.repository_id = ? AND c.name_lower = ? AND v.version = ?;

-- name: ListCargoBlobDigests :many
-- Distinct blob digests every cached/published .crate references, for GC.
SELECT DISTINCT blob_digest FROM cargo_versions WHERE blob_digest != '';

-- name: ListCargoVersionsForRetention :many
-- Every version in a repository, grouped by crate and newest first, for pruning.
SELECT c.name_lower AS name_lower, v.version AS version, v.created_at AS created_at
FROM cargo_versions v
JOIN cargo_crates c ON c.id = v.crate_id
WHERE c.repository_id = ?
ORDER BY c.name_lower ASC, v.created_at DESC;

-- name: DeleteCargoVersion :execrows
DELETE FROM cargo_versions
WHERE crate_id = (SELECT id FROM cargo_crates WHERE repository_id = ? AND name_lower = ?)
  AND version = ?;

-- name: ListCargoVersionsForCrate :many
-- Every version of a crate for its detail page.
SELECT v.version, v.size, v.yanked, v.cksum
FROM cargo_versions v
JOIN cargo_crates c ON c.id = v.crate_id
WHERE c.repository_id = ? AND c.name_lower = ?
ORDER BY v.created_at DESC;

-- name: ListAllCargoCrates :many
-- Every Cargo crate across all repositories, with version count, total cached
-- size, and a proxy flag, for the browser's project-grouped index.
SELECT
    p.key         AS project_key,
    p.name        AS project_name,
    r.key         AS repo_key,
    c.name        AS crate_name,
    COUNT(v.id)   AS version_count,
    CAST(COALESCE(SUM(v.size), 0) AS INTEGER) AS size_bytes,
    CAST(r.mode = 'proxy' AS INTEGER) AS is_proxy,
    c.updated_at  AS updated_at
FROM cargo_crates c
JOIN repositories r ON r.id = c.repository_id
JOIN projects p ON p.id = r.project_id
LEFT JOIN cargo_versions v ON v.crate_id = c.id
GROUP BY c.id
ORDER BY p.key ASC, r.key ASC, c.name_lower ASC;
