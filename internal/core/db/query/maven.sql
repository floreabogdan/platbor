-- name: UpsertMavenFile :exec
-- Store a file at a path in a repository, replacing any existing file there
-- (Maven paths are mutable: SNAPSHOT redeploys and metadata updates overwrite).
INSERT INTO maven_files (id, repository_id, path, group_id, artifact_id, version, filename, is_metadata, blob_digest, size, sha1, md5, upstream_url, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (repository_id, path)
DO UPDATE SET
    group_id    = excluded.group_id,
    artifact_id = excluded.artifact_id,
    version     = excluded.version,
    filename    = excluded.filename,
    is_metadata = excluded.is_metadata,
    blob_digest = excluded.blob_digest,
    size        = excluded.size,
    sha1        = excluded.sha1,
    md5         = excluded.md5,
    upstream_url = excluded.upstream_url,
    updated_at  = excluded.updated_at;

-- name: GetMavenFile :one
SELECT * FROM maven_files
WHERE repository_id = ? AND path = ?;

-- name: DeleteMavenFile :execrows
DELETE FROM maven_files
WHERE repository_id = ? AND path = ?;

-- name: ListMavenBlobDigests :many
-- Distinct blob digests every cached/uploaded Maven file references, for garbage
-- collection to mark them alongside other formats. Empty (uncached) rows excluded.
SELECT DISTINCT blob_digest FROM maven_files WHERE blob_digest != '';

-- name: ListMavenFilesForArtifact :many
-- Every file of one artifact (group + artifact), for its detail page.
SELECT path, group_id, artifact_id, version, filename, is_metadata, size, sha1
FROM maven_files
WHERE repository_id = ? AND group_id = ? AND artifact_id = ?
ORDER BY version DESC, filename ASC;

-- name: ListMavenFilesForRetention :many
-- Every non-metadata file in a repository, grouped by artifact and newest first,
-- for keep-last-N-versions pruning. Metadata files are excluded from counting.
SELECT group_id AS group_id, artifact_id AS artifact_id, version AS version, path AS path, created_at AS created_at
FROM maven_files
WHERE repository_id = ? AND is_metadata = 0 AND version != ''
ORDER BY group_id ASC, artifact_id ASC, created_at DESC;

-- name: ListAllMavenArtifacts :many
-- Every Maven artifact (group:artifact) across all repositories, with its version
-- count, total cached size, and a proxy flag, for the browser's project-grouped
-- index. Metadata files count toward size but not toward the version count.
SELECT
    p.key         AS project_key,
    p.name        AS project_name,
    r.key         AS repo_key,
    f.group_id    AS group_id,
    f.artifact_id AS artifact_id,
    COUNT(DISTINCT CASE WHEN f.is_metadata = 0 AND f.version != '' THEN f.version END) AS version_count,
    CAST(COALESCE(SUM(f.size), 0) AS INTEGER) AS size_bytes,
    CAST(r.mode = 'proxy' AS INTEGER) AS is_proxy,
    CAST(MAX(f.updated_at) AS TEXT) AS updated_at
FROM maven_files f
JOIN repositories r ON r.id = f.repository_id
JOIN projects p ON p.id = r.project_id
WHERE f.group_id != '' AND f.artifact_id != ''
GROUP BY r.id, f.group_id, f.artifact_id
ORDER BY p.key ASC, r.key ASC, f.group_id ASC, f.artifact_id ASC;
