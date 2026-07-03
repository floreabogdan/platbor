-- name: UpsertTerraformModule :one
-- Ensure the module row for (repository, name, provider) exists, returning its id.
INSERT INTO terraform_modules (id, repository_id, name, provider, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (repository_id, name, provider)
DO UPDATE SET updated_at = excluded.updated_at
RETURNING id;

-- name: TerraformVersionExists :one
SELECT COUNT(*) FROM terraform_versions v
JOIN terraform_modules m ON m.id = v.module_id
WHERE m.repository_id = ? AND m.name = ? AND m.provider = ? AND v.version = ?;

-- name: InsertTerraformVersion :exec
INSERT INTO terraform_versions (id, module_id, version, blob_digest, size, created_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: ResolveTerraformModule :one
-- Resolve (project, name, provider) to a module and its repository, for the
-- registry read protocol (the namespace in a module address is the project key).
SELECT m.id AS module_id, m.repository_id AS repository_id
FROM terraform_modules m
JOIN repositories r ON r.id = m.repository_id
WHERE r.project_id = ? AND r.format = 'terraform' AND m.name = ? AND m.provider = ?
ORDER BY m.updated_at DESC
LIMIT 1;

-- name: ListTerraformVersions :many
-- Every version of a module, newest first, for the /versions endpoint.
SELECT version FROM terraform_versions
WHERE module_id = ?
ORDER BY created_at DESC;

-- name: GetTerraformVersion :one
-- Resolve a module version to its archive blob for download.
SELECT blob_digest, size FROM terraform_versions
WHERE module_id = ? AND version = ?;

-- name: ListTerraformBlobDigests :many
SELECT DISTINCT blob_digest FROM terraform_versions WHERE blob_digest != '';

-- name: ListTerraformVersionsForRetention :many
SELECT m.name AS name, m.provider AS provider, v.version AS version, v.created_at AS created_at
FROM terraform_versions v
JOIN terraform_modules m ON m.id = v.module_id
WHERE m.repository_id = ?
ORDER BY m.name ASC, m.provider ASC, v.created_at DESC;

-- name: DeleteTerraformVersion :execrows
DELETE FROM terraform_versions
WHERE module_id = (SELECT id FROM terraform_modules WHERE repository_id = ? AND name = ? AND provider = ?)
  AND version = ?;

-- name: ListTerraformVersionsForModule :many
-- Every version of a module for its detail page.
SELECT v.version, v.size
FROM terraform_versions v
JOIN terraform_modules m ON m.id = v.module_id
WHERE m.repository_id = ? AND m.name = ? AND m.provider = ?
ORDER BY v.created_at DESC;

-- name: ListAllTerraformModules :many
-- Every Terraform module across all repositories, with version count, total size,
-- and a proxy flag (always local), for the browser's project-grouped index.
SELECT
    p.key         AS project_key,
    p.name        AS project_name,
    r.key         AS repo_key,
    m.name        AS module_name,
    m.provider    AS provider,
    COUNT(v.id)   AS version_count,
    CAST(COALESCE(SUM(v.size), 0) AS INTEGER) AS size_bytes,
    CAST(r.mode = 'proxy' AS INTEGER) AS is_proxy,
    m.updated_at  AS updated_at
FROM terraform_modules m
JOIN repositories r ON r.id = m.repository_id
JOIN projects p ON p.id = r.project_id
LEFT JOIN terraform_versions v ON v.module_id = m.id
GROUP BY m.id
ORDER BY p.key ASC, r.key ASC, m.name ASC, m.provider ASC;
