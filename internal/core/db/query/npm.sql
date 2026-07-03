-- name: UpsertNpmPackage :one
-- Ensure the package row for (project, repository, name) exists, returning its
-- id. Publishing a new version of an existing package just bumps updated_at.
INSERT INTO npm_packages (id, project_id, repository, name, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (project_id, repository, name)
DO UPDATE SET updated_at = excluded.updated_at
RETURNING id;

-- name: GetNpmPackage :one
SELECT * FROM npm_packages
WHERE project_id = ? AND repository = ? AND name = ?;

-- name: NpmVersionExists :one
-- Whether a specific version is already published. npm forbids overwriting a
-- published version, so the handler rejects a re-publish before inserting.
SELECT COUNT(*) FROM npm_versions v
JOIN npm_packages p ON p.id = v.package_id
WHERE p.project_id = ? AND p.repository = ? AND p.name = ? AND v.version = ?;

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
WHERE p.project_id = ? AND p.repository = ? AND p.name = ?
ORDER BY v.created_at ASC;

-- name: GetNpmTarball :one
-- The blob digest and size for one package version's tarball, to serve it.
SELECT v.tarball_digest, v.tarball_size
FROM npm_versions v
JOIN npm_packages p ON p.id = v.package_id
WHERE p.project_id = ? AND p.repository = ? AND p.name = ? AND v.version = ?;

-- name: ListNpmDistTags :many
SELECT t.tag, t.version
FROM npm_dist_tags t
JOIN npm_packages p ON p.id = t.package_id
WHERE p.project_id = ? AND p.repository = ? AND p.name = ?;

-- name: UpsertNpmDistTag :exec
INSERT INTO npm_dist_tags (package_id, tag, version, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT (package_id, tag)
DO UPDATE SET version = excluded.version, updated_at = excluded.updated_at;

-- name: DeleteNpmDistTag :execrows
DELETE FROM npm_dist_tags
WHERE package_id = ? AND tag = ?;

-- name: ListNpmTarballDigests :many
-- Distinct tarball digests every npm version references, for garbage collection
-- to mark them alongside OCI blobs. Blobs are a shared CAS spanning all formats.
SELECT DISTINCT tarball_digest FROM npm_versions;

-- name: ListAllNpmPackages :many
-- Every npm package across all projects, with version count, total tarball size,
-- and a proxy flag, for the registry browser's package index. is_proxy is 1 when
-- the package's project is a pull-through mirror.
SELECT
    p.key        AS project_key,
    p.name       AS project_name,
    pkg.repository AS repository,
    pkg.name     AS package_name,
    COUNT(v.id)  AS version_count,
    CAST(COALESCE(SUM(v.tarball_size), 0) AS INTEGER) AS size_bytes,
    CAST(rp.project_id IS NOT NULL AS INTEGER) AS is_proxy,
    pkg.updated_at AS updated_at
FROM npm_packages pkg
JOIN projects p ON p.id = pkg.project_id
LEFT JOIN registry_proxies rp ON rp.project_id = pkg.project_id
LEFT JOIN npm_versions v ON v.package_id = pkg.id
GROUP BY pkg.id
ORDER BY p.key ASC, pkg.name ASC;
