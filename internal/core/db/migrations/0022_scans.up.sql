-- Vulnerability scans. A scan matches an artifact's SBOM component inventory
-- against a vulnerability database (OSV) and records the findings, so the result
-- is queryable both ways: an artifact's vulnerabilities, and a vulnerability's
-- affected artifacts (the "CVE -> artifacts" query). Scanning is SBOM-driven and
-- needs no external scanner binary -- it stays within the single-binary promise.
--
-- One scan is kept per (repo_id, image, digest): a rescan replaces the previous
-- result, so the finding set always reflects the latest run.
CREATE TABLE scans (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    repo_id         TEXT NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    image           TEXT NOT NULL,
    digest          TEXT NOT NULL,
    source_digest   TEXT NOT NULL,           -- the SBOM referrer that was scanned
    component_count INTEGER NOT NULL DEFAULT 0,
    critical        INTEGER NOT NULL DEFAULT 0,
    high            INTEGER NOT NULL DEFAULT 0,
    medium          INTEGER NOT NULL DEFAULT 0,
    low             INTEGER NOT NULL DEFAULT 0,
    unknown         INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT NOT NULL
);

CREATE INDEX idx_scans_artifact ON scans (repo_id, image, digest);
CREATE INDEX idx_scans_project ON scans (project_id);

-- One row per (vulnerability, component) a scan turned up. severity is one of
-- critical|high|medium|low|unknown; severity_rank orders them (4=critical..0=unknown)
-- so the rollup can pick the worst without parsing the label in SQL.
CREATE TABLE scan_findings (
    id            TEXT PRIMARY KEY,
    scan_id       TEXT NOT NULL REFERENCES scans (id) ON DELETE CASCADE,
    project_id    TEXT NOT NULL,
    vuln_id       TEXT NOT NULL,
    package       TEXT NOT NULL,
    version       TEXT NOT NULL,
    ecosystem     TEXT NOT NULL DEFAULT '',
    severity      TEXT NOT NULL DEFAULT 'unknown',
    severity_rank INTEGER NOT NULL DEFAULT 0,
    summary       TEXT NOT NULL DEFAULT '',
    fixed_version TEXT NOT NULL DEFAULT '',
    reference_url TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_findings_scan ON scan_findings (scan_id);
CREATE INDEX idx_findings_vuln ON scan_findings (vuln_id);
