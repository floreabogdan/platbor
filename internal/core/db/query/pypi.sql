-- name: UpsertPypiPackage :one
-- Ensure the package row for (repository, normalized name) exists, returning its
-- id. A new file of an existing package just bumps updated_at.
INSERT INTO pypi_packages (id, repository_id, name, name_original, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (repository_id, name)
DO UPDATE SET updated_at = excluded.updated_at, name_original = excluded.name_original
RETURNING id;

-- name: PypiFileExists :one
-- Whether a distribution filename already exists in the repository (uploads are
-- immutable: re-uploading a filename is rejected).
SELECT COUNT(*) FROM pypi_files f
JOIN pypi_packages p ON p.id = f.package_id
WHERE p.repository_id = ? AND f.filename = ?;

-- name: InsertPypiFile :exec
-- Record a distribution file. On conflict (a proxy re-listing the same file, or a
-- download filling a cached row) update metadata and the blob only when a real
-- new digest is supplied, so a fresh simple-index read never clears a cached blob.
INSERT INTO pypi_files (id, package_id, version, filename, blob_digest, size, sha256, requires_python, upstream_url, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (package_id, filename)
DO UPDATE SET
    version = excluded.version,
    size = CASE WHEN excluded.blob_digest != '' THEN excluded.size ELSE pypi_files.size END,
    sha256 = excluded.sha256,
    requires_python = excluded.requires_python,
    upstream_url = excluded.upstream_url,
    blob_digest = CASE WHEN excluded.blob_digest != '' THEN excluded.blob_digest ELSE pypi_files.blob_digest END;

-- name: SetPypiFileBlob :exec
-- Fill a proxied file's cached blob after downloading it from the upstream.
UPDATE pypi_files SET blob_digest = ?, size = ?
WHERE package_id = ? AND filename = ?;

-- name: ListPypiFiles :many
-- Every distribution file of a package (by normalized name), for the simple index.
SELECT f.filename, f.version, f.sha256, f.requires_python, f.blob_digest, f.size, f.upstream_url
FROM pypi_files f
JOIN pypi_packages p ON p.id = f.package_id
WHERE p.repository_id = ? AND p.name = ?
ORDER BY f.filename ASC;

-- name: GetPypiFile :one
-- Resolve a distribution filename to its content for download.
SELECT f.package_id, f.blob_digest, f.size, f.sha256, f.upstream_url
FROM pypi_files f
JOIN pypi_packages p ON p.id = f.package_id
WHERE p.repository_id = ? AND f.filename = ?;

-- name: ListPypiPackageNames :many
-- Normalized names of every package in a repository, for the root simple index.
SELECT name FROM pypi_packages WHERE repository_id = ? ORDER BY name ASC;

-- name: ListAllPypiPackages :many
-- Every PyPI package across all repositories, with file count, total cached size,
-- and a proxy flag, for the browser's project-grouped index.
SELECT
    p.key         AS project_key,
    p.name        AS project_name,
    r.key         AS repo_key,
    pkg.name_original AS package_name,
    COUNT(f.id)   AS file_count,
    CAST(COALESCE(SUM(f.size), 0) AS INTEGER) AS size_bytes,
    CAST(r.mode = 'proxy' AS INTEGER) AS is_proxy,
    pkg.updated_at AS updated_at
FROM pypi_packages pkg
JOIN repositories r ON r.id = pkg.repository_id
JOIN projects p ON p.id = r.project_id
LEFT JOIN pypi_files f ON f.package_id = pkg.id
GROUP BY pkg.id
ORDER BY p.key ASC, r.key ASC, pkg.name ASC;

-- name: ListPypiFilesForRetention :many
-- Every file in a repository, grouped by package and newest first, for
-- keep-last-N-versions pruning.
SELECT pkg.name AS name, f.version AS version, f.filename AS filename, f.created_at AS created_at
FROM pypi_files f
JOIN pypi_packages pkg ON pkg.id = f.package_id
WHERE pkg.repository_id = ?
ORDER BY pkg.name ASC, f.created_at DESC;

-- name: DeletePypiFile :execrows
DELETE FROM pypi_files
WHERE package_id = (SELECT id FROM pypi_packages WHERE repository_id = ? AND name = ?)
  AND filename = ?;

-- name: ListPypiBlobDigests :many
-- Distinct blob digests every cached/uploaded file references, for garbage
-- collection to mark them alongside other formats. Empty (uncached) rows are excluded.
SELECT DISTINCT blob_digest FROM pypi_files WHERE blob_digest != '';
