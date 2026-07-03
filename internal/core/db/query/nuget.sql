-- name: UpsertNugetPackage :one
-- Ensure the package row for (project, id_lower) exists, returning its id.
-- Pushing a new version of an existing package bumps updated_at.
INSERT INTO nuget_packages (id, project_id, id_lower, id_original, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (project_id, id_lower)
DO UPDATE SET updated_at = excluded.updated_at
RETURNING id;

-- name: GetNugetPackage :one
SELECT * FROM nuget_packages
WHERE project_id = ? AND id_lower = ?;

-- name: NugetVersionExists :one
-- Whether a specific version is already pushed. NuGet forbids overwriting a
-- published version, so the handler rejects a re-push before inserting.
SELECT COUNT(*) FROM nuget_versions v
JOIN nuget_packages p ON p.id = v.package_id
WHERE p.project_id = ? AND p.id_lower = ? AND v.version_lower = ?;

-- name: InsertNugetVersion :exec
INSERT INTO nuget_versions (id, package_id, version, version_lower, nupkg_digest, nupkg_size, nuspec, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (package_id, version_lower) DO NOTHING;

-- name: ListNugetVersions :many
-- Every version of a package, oldest first, with the nuspec and nupkg pointer,
-- for the flat-container index, registration, and downloads.
SELECT v.version, v.version_lower, v.nupkg_digest, v.nupkg_size, v.nuspec, v.created_at
FROM nuget_versions v
JOIN nuget_packages p ON p.id = v.package_id
WHERE p.project_id = ? AND p.id_lower = ?
ORDER BY v.created_at ASC;

-- name: GetNugetNupkg :one
-- The blob digest and size for one version's .nupkg, to serve it.
SELECT v.nupkg_digest, v.nupkg_size
FROM nuget_versions v
JOIN nuget_packages p ON p.id = v.package_id
WHERE p.project_id = ? AND p.id_lower = ? AND v.version_lower = ?;

-- name: ListNugetVersionsForRetention :many
-- Every version in a project, grouped by package and newest first, for
-- keep-last-N pruning.
SELECT p.id_lower AS id_lower, v.version_lower AS version_lower, v.created_at AS created_at
FROM nuget_versions v
JOIN nuget_packages p ON p.id = v.package_id
WHERE p.project_id = ?
ORDER BY p.id_lower ASC, v.created_at DESC;

-- name: DeleteNugetVersion :execrows
DELETE FROM nuget_versions
WHERE package_id = (SELECT id FROM nuget_packages WHERE project_id = ? AND id_lower = ?)
  AND version_lower = ?;

-- name: ListNugetBlobDigests :many
-- Distinct nupkg digests every version references, for garbage collection to
-- mark them alongside the other formats (shared CAS).
SELECT DISTINCT nupkg_digest FROM nuget_versions;

-- name: SearchNugetPackages :many
-- Packages in a project whose id contains the (lowercased) query, newest first,
-- for the search resource. An empty query matches all.
SELECT
    pkg.id_original AS id_original,
    pkg.id_lower    AS id_lower,
    COUNT(v.id)     AS version_count,
    pkg.updated_at  AS updated_at
FROM nuget_packages pkg
LEFT JOIN nuget_versions v ON v.package_id = pkg.id
WHERE pkg.project_id = ? AND pkg.id_lower LIKE ?
GROUP BY pkg.id
ORDER BY pkg.updated_at DESC
LIMIT ?;

-- name: ListAllNugetPackages :many
-- Every NuGet package across all projects, with version count, total nupkg size,
-- and a proxy flag, for the registry browser's package index.
SELECT
    p.key         AS project_key,
    p.name        AS project_name,
    pkg.id_original AS package_id,
    COUNT(v.id)   AS version_count,
    CAST(COALESCE(SUM(v.nupkg_size), 0) AS INTEGER) AS size_bytes,
    CAST(rp.project_id IS NOT NULL AS INTEGER) AS is_proxy,
    pkg.updated_at AS updated_at
FROM nuget_packages pkg
JOIN projects p ON p.id = pkg.project_id
LEFT JOIN registry_proxies rp ON rp.project_id = pkg.project_id
LEFT JOIN nuget_versions v ON v.package_id = pkg.id
GROUP BY pkg.id
ORDER BY p.key ASC, pkg.id_lower ASC;
