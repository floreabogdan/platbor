package scan

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/id"
)

// ErrNoScan means no scan has been stored for an artifact yet.
var ErrNoScan = errors.New("no scan for artifact")

// Scan is a stored scan summary.
type Scan struct {
	ID             string
	ProjectID      string
	RepoID         string
	Image          string
	Digest         string
	SourceDigest   string
	ComponentCount int
	Counts         map[string]int // severity -> count
	CreatedAt      time.Time
}

// Vulnerability is a rollup row for the vulnerabilities index.
type Vulnerability struct {
	VulnID        string
	Severity      string
	SeverityRank  int
	Summary       string
	ArtifactCount int
}

// AffectedArtifact is one artifact affected by a vulnerability (the reverse query).
type AffectedArtifact struct {
	ProjectKey   string
	ProjectName  string
	RepoKey      string
	Image        string
	Digest       string
	Package      string
	Version      string
	Severity     string
	SeverityRank int
	FixedVersion string
	ScannedAt    time.Time
}

// Service persists scans and answers both directions of the scan graph. It writes
// through the audit log so a scan is recorded like any other mutation.
type Service struct {
	db  *sql.DB
	q   *db.Queries
	now func() time.Time
}

// NewService wires the scan store to an open database.
func NewService(sqlDB *sql.DB) *Service {
	return &Service{db: sqlDB, q: db.New(sqlDB), now: func() time.Time { return time.Now().UTC() }}
}

// Save replaces any prior scan of the artifact with a fresh result and its
// findings, in one transaction, auditing the run. Returns the stored summary.
func (s *Service) Save(ctx context.Context, projectID, repoID, image, digest, sourceDigest string, res Result, actor string) (Scan, error) {
	ts := s.now().Format(time.RFC3339Nano)
	scanID := id.New("scan")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Scan{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := s.q.WithTx(tx)

	if err := qtx.DeleteScansForArtifact(ctx, db.DeleteScansForArtifactParams{RepoID: repoID, Image: image, Digest: digest}); err != nil {
		return Scan{}, fmt.Errorf("clearing prior scan: %w", err)
	}

	row, err := qtx.CreateScan(ctx, db.CreateScanParams{
		ID:             scanID,
		ProjectID:      projectID,
		RepoID:         repoID,
		Image:          image,
		Digest:         digest,
		SourceDigest:   sourceDigest,
		ComponentCount: int64(res.ComponentCount),
		Critical:       int64(res.Counts[SeverityCritical]),
		High:           int64(res.Counts[SeverityHigh]),
		Medium:         int64(res.Counts[SeverityMedium]),
		Low:            int64(res.Counts[SeverityLow]),
		Unknown:        int64(res.Counts[SeverityUnknown]),
		CreatedAt:      ts,
	})
	if err != nil {
		return Scan{}, fmt.Errorf("creating scan: %w", err)
	}

	for _, f := range res.Findings {
		if err := qtx.CreateFinding(ctx, db.CreateFindingParams{
			ID:           id.New("finding"),
			ScanID:       scanID,
			ProjectID:    projectID,
			VulnID:       f.VulnID,
			Package:      f.Package,
			Version:      f.Version,
			Ecosystem:    f.Ecosystem,
			Severity:     f.Severity,
			SeverityRank: int64(f.SeverityRank),
			Summary:      f.Summary,
			FixedVersion: f.FixedVersion,
			ReferenceUrl: f.ReferenceURL,
		}); err != nil {
			return Scan{}, fmt.Errorf("creating finding: %w", err)
		}
	}

	if _, err := qtx.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		ID:         id.New("audit"),
		ProjectID:  sql.NullString{String: projectID, Valid: true},
		Actor:      auditActor(actor),
		Action:     "scan.run",
		TargetType: "artifact",
		TargetID:   digest,
		Metadata:   fmt.Sprintf(`{"image":%q,"findings":%d}`, image, len(res.Findings)),
		CreatedAt:  ts,
	}); err != nil {
		return Scan{}, fmt.Errorf("writing audit entry: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Scan{}, fmt.Errorf("commit: %w", err)
	}
	return scanFromRow(row), nil
}

// Latest returns the stored scan of an artifact and its findings, or ErrNoScan.
func (s *Service) Latest(ctx context.Context, repoID, image, digest string) (Scan, []Finding, error) {
	row, err := s.q.GetLatestScan(ctx, db.GetLatestScanParams{RepoID: repoID, Image: image, Digest: digest})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Scan{}, nil, ErrNoScan
		}
		return Scan{}, nil, fmt.Errorf("getting scan: %w", err)
	}
	rows, err := s.q.ListFindingsForScan(ctx, row.ID)
	if err != nil {
		return Scan{}, nil, fmt.Errorf("listing findings: %w", err)
	}
	findings := make([]Finding, 0, len(rows))
	for _, f := range rows {
		findings = append(findings, Finding{
			VulnID:       f.VulnID,
			Package:      f.Package,
			Version:      f.Version,
			Ecosystem:    f.Ecosystem,
			Severity:     f.Severity,
			SeverityRank: int(f.SeverityRank),
			Summary:      f.Summary,
			FixedVersion: f.FixedVersion,
			ReferenceURL: f.ReferenceUrl,
		})
	}
	return scanFromRow(row), findings, nil
}

// Vulnerabilities returns the distinct vulnerabilities across every stored scan,
// worst severity first, with the number of affected artifacts.
func (s *Service) Vulnerabilities(ctx context.Context) ([]Vulnerability, error) {
	rows, err := s.q.ListVulnerabilities(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing vulnerabilities: %w", err)
	}
	out := make([]Vulnerability, 0, len(rows))
	for _, r := range rows {
		out = append(out, Vulnerability{
			VulnID:        r.VulnID,
			Severity:      r.Severity,
			SeverityRank:  int(r.SeverityRank),
			Summary:       r.Summary,
			ArtifactCount: int(r.ArtifactCount),
		})
	}
	return out, nil
}

// AffectedBy returns the artifacts a vulnerability affects (the "CVE -> artifacts"
// query).
func (s *Service) AffectedBy(ctx context.Context, vulnID string) ([]AffectedArtifact, error) {
	rows, err := s.q.ListArtifactsByVuln(ctx, vulnID)
	if err != nil {
		return nil, fmt.Errorf("listing affected artifacts: %w", err)
	}
	out := make([]AffectedArtifact, 0, len(rows))
	for _, r := range rows {
		scanned, _ := time.Parse(time.RFC3339Nano, r.CreatedAt)
		out = append(out, AffectedArtifact{
			ProjectKey:   r.ProjectKey,
			ProjectName:  r.ProjectName,
			RepoKey:      r.RepoKey,
			Image:        r.Image,
			Digest:       r.Digest,
			Package:      r.Package,
			Version:      r.Version,
			Severity:     r.Severity,
			SeverityRank: int(r.SeverityRank),
			FixedVersion: r.FixedVersion,
			ScannedAt:    scanned,
		})
	}
	return out, nil
}

func scanFromRow(row db.Scan) Scan {
	created, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
	return Scan{
		ID:             row.ID,
		ProjectID:      row.ProjectID,
		RepoID:         row.RepoID,
		Image:          row.Image,
		Digest:         row.Digest,
		SourceDigest:   row.SourceDigest,
		ComponentCount: int(row.ComponentCount),
		Counts: map[string]int{
			SeverityCritical: int(row.Critical),
			SeverityHigh:     int(row.High),
			SeverityMedium:   int(row.Medium),
			SeverityLow:      int(row.Low),
			SeverityUnknown:  int(row.Unknown),
		},
		CreatedAt: created,
	}
}

func auditActor(actor string) string {
	if actor == "" {
		return "system"
	}
	return actor
}
