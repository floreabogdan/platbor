-- name: DeleteScansForArtifact :exec
-- Remove any prior scan of this artifact so a rescan replaces it (findings
-- cascade). Keeps at most one scan per (repo_id, image, digest).
DELETE FROM scans WHERE repo_id = ? AND image = ? AND digest = ?;

-- name: CreateScan :one
INSERT INTO scans (
    id, project_id, repo_id, image, digest, source_digest,
    component_count, critical, high, medium, low, unknown, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: CreateFinding :exec
INSERT INTO scan_findings (
    id, scan_id, project_id, vuln_id, package, version,
    ecosystem, severity, severity_rank, summary, fixed_version, reference_url
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetLatestScan :one
-- The most recent scan of an artifact (there is normally one; ORDER BY guards
-- against a race leaving two).
SELECT * FROM scans
WHERE repo_id = ? AND image = ? AND digest = ?
ORDER BY created_at DESC
LIMIT 1;

-- name: ListFindingsForScan :many
SELECT * FROM scan_findings
WHERE scan_id = ?
ORDER BY severity_rank DESC, vuln_id ASC, package ASC;

-- name: ListVulnerabilities :many
-- One row per distinct vulnerability across all stored scans, with the number of
-- affected artifacts. severity and summary are properties of the vulnerability so
-- they are constant within a vuln_id; grouping by them yields one row per vuln.
SELECT vuln_id, severity, severity_rank, summary,
       COUNT(DISTINCT scan_id) AS artifact_count
FROM scan_findings
GROUP BY vuln_id, severity, severity_rank, summary
ORDER BY severity_rank DESC, artifact_count DESC, vuln_id ASC;

-- name: ListArtifactsByVuln :many
-- The artifacts affected by one vulnerability: the "CVE -> artifacts" query. Joins
-- through scans to the repository and project for human-readable keys.
SELECT p.key AS project_key, p.name AS project_name, r.key AS repo_key,
       s.image, s.digest, f.package, f.version, f.severity, f.severity_rank,
       f.fixed_version, s.created_at
FROM scan_findings f
JOIN scans s ON s.id = f.scan_id
JOIN repositories r ON r.id = s.repo_id
JOIN projects p ON p.id = s.project_id
WHERE f.vuln_id = ?
ORDER BY s.created_at DESC, s.image ASC;
