-- name: UpsertNpmPackage :one
-- Ensure the package row for (repository, name) exists, returning its id.
-- Publishing a new version of an existing package just bumps updated_at.
INSERT INTO npm_packages (id, repository_id, name, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (repository_id, name)
DO UPDATE SET updated_at = excluded.updated_at
RETURNING id;

-- name: GetNpmPackage :one
SELECT * FROM npm_packages
WHERE repository_id = ? AND name = ?;

-- name: NpmVersionExists :one
-- Whether a specific version is already published. npm forbids overwriting a
-- published version, so the handler rejects a re-publish before inserting.
SELECT COUNT(*) FROM npm_versions v
JOIN npm_packages p ON p.id = v.package_id
WHERE p.repository_id = ? AND p.name = ? AND v.version = ?;

-- name: InsertNpmVersion :exec
-- Store a published version. First-writer-wins: a duplicate (already rejected at
-- the handler) is a no-op rather than a transaction error.
INSERT INTO npm_versions (id, package_id, version, manifest, tarball_digest, tarball_size, shasum, integrity, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (package_id, version) DO NOTHING;

-- name: ListNpmVersions :many
-- Every published version of a package, oldest first, with the verbatim version
-- metadata and its tarball's digests, so the packument can be rebuilt.
SELECT v.version, v.manifest, v.tarball_digest, v.tarball_size, v.shasum, v.integrity, v.created_at
FROM npm_versions v
JOIN npm_packages p ON p.id = v.package_id
WHERE p.repository_id = ? AND p.name = ?
ORDER BY v.created_at ASC;

-- name: GetNpmTarball :one
-- The blob digest and size for one package version's tarball, to serve it.
SELECT v.tarball_digest, v.tarball_size
FROM npm_versions v
JOIN npm_packages p ON p.id = v.package_id
WHERE p.repository_id = ? AND p.name = ? AND v.version = ?;

-- name: ListNpmDistTags :many
SELECT t.tag, t.version
FROM npm_dist_tags t
JOIN npm_packages p ON p.id = t.package_id
WHERE p.repository_id = ? AND p.name = ?;

-- name: UpsertNpmDistTag :exec
INSERT INTO npm_dist_tags (package_id, tag, version, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT (package_id, tag)
DO UPDATE SET version = excluded.version, updated_at = excluded.updated_at;

-- name: DeleteNpmDistTag :execrows
DELETE FROM npm_dist_tags
WHERE package_id = ? AND tag = ?;

-- name: ListNpmVersionsForRetention :many
-- Every version in a repository, grouped by package and newest first, for
-- keep-last-N pruning.
SELECT p.name AS name, v.version AS version, v.created_at AS created_at
FROM npm_versions v
JOIN npm_packages p ON p.id = v.package_id
WHERE p.repository_id = ?
ORDER BY p.name ASC, v.created_at DESC;

-- name: DeleteNpmVersion :execrows
DELETE FROM npm_versions
WHERE package_id = (SELECT id FROM npm_packages WHERE repository_id = ? AND name = ?)
  AND version = ?;

-- name: ListNpmTarballDigests :many
-- Distinct tarball digests every npm version references, for garbage collection
-- to mark them alongside OCI blobs. Blobs are a shared CAS spanning all formats.
SELECT DISTINCT tarball_digest FROM npm_versions;

-- name: ListAllNpmPackages :many
-- Every npm package across all repositories, joined to its repository and
-- project, with version count, total tarball size, and a proxy flag.
SELECT
    p.key        AS project_key,
    p.name       AS project_name,
    r.key        AS repo_key,
    pkg.name     AS package_name,
    COUNT(v.id)  AS version_count,
    CAST(COALESCE(SUM(v.tarball_size), 0) AS INTEGER) AS size_bytes,
    CAST(r.mode = 'proxy' AS INTEGER) AS is_proxy,
    pkg.updated_at AS updated_at
FROM npm_packages pkg
JOIN repositories r ON r.id = pkg.repository_id
JOIN projects p ON p.id = r.project_id
LEFT JOIN npm_versions v ON v.package_id = pkg.id
GROUP BY pkg.id
ORDER BY p.key ASC, r.key ASC, pkg.name ASC;
