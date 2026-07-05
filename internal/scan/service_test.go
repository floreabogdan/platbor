package scan

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/platbor/platbor/internal/core/config"
	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/project"
	"github.com/platbor/platbor/internal/core/repository"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	ctx := context.Background()
	sqlDB, err := db.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Migrate(ctx, sqlDB, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return sqlDB
}

func seedRepo(t *testing.T, sqlDB *sql.DB) (projectID, repoID string) {
	t.Helper()
	ctx := context.Background()
	proj, err := project.NewService(sqlDB).Create(ctx, project.CreateInput{Key: "p", Name: "P", Actor: "admin"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	repo, err := repository.NewService(sqlDB).Create(ctx, repository.CreateInput{
		ProjectID: proj.ID, Key: "images", Name: "images", Format: repository.FormatOCI, Mode: repository.ModeLocal, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}
	return proj.ID, repo.ID
}

func sampleResult() Result {
	findings := []Finding{
		{VulnID: "CVE-2020-1234", Package: "left-pad", Version: "1.0.0", Ecosystem: "npm", Severity: SeverityCritical, SeverityRank: severityRank(SeverityCritical), Summary: "bad", FixedVersion: "1.3.0", ReferenceURL: "https://x"},
		{VulnID: "CVE-2019-9999", Package: "old-lib", Version: "0.1.0", Ecosystem: "npm", Severity: SeverityMedium, SeverityRank: severityRank(SeverityMedium), Summary: "meh", FixedVersion: "0.2.0"},
	}
	return Result{
		ComponentCount: 5,
		Findings:       findings,
		Counts:         map[string]int{SeverityCritical: 1, SeverityMedium: 1},
	}
}

func TestServiceSaveAndLatest(t *testing.T) {
	ctx := context.Background()
	sqlDB := testDB(t)
	projectID, repoID := seedRepo(t, sqlDB)

	svc := NewService(sqlDB)
	if _, _, err := svc.Latest(ctx, repoID, "app", "sha256:aaa"); !errors.Is(err, ErrNoScan) {
		t.Fatalf("Latest before scan = %v, want ErrNoScan", err)
	}

	if _, err := svc.Save(ctx, projectID, repoID, "app", "sha256:aaa", "sha256:sbom", sampleResult(), "tester"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	summary, findings, err := svc.Latest(ctx, repoID, "app", "sha256:aaa")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if summary.ComponentCount != 5 {
		t.Errorf("ComponentCount = %d, want 5", summary.ComponentCount)
	}
	if summary.Counts[SeverityCritical] != 1 || summary.Counts[SeverityMedium] != 1 {
		t.Errorf("counts = %+v", summary.Counts)
	}
	if len(findings) != 2 {
		t.Fatalf("findings = %d, want 2", len(findings))
	}
	// Ordered worst-severity first.
	if findings[0].Severity != SeverityCritical {
		t.Errorf("first finding severity = %q, want critical", findings[0].Severity)
	}
}

func TestServiceRescanReplaces(t *testing.T) {
	ctx := context.Background()
	sqlDB := testDB(t)
	projectID, repoID := seedRepo(t, sqlDB)
	svc := NewService(sqlDB)

	if _, err := svc.Save(ctx, projectID, repoID, "app", "sha256:aaa", "sha256:sbom", sampleResult(), "tester"); err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	// A rescan finding nothing should leave no findings for the artifact.
	empty := Result{ComponentCount: 5, Findings: []Finding{}, Counts: map[string]int{}}
	if _, err := svc.Save(ctx, projectID, repoID, "app", "sha256:aaa", "sha256:sbom", empty, "tester"); err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	_, findings, err := svc.Latest(ctx, repoID, "app", "sha256:aaa")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("after rescan findings = %d, want 0 (replaced)", len(findings))
	}
	// And the reverse query no longer lists the old CVE.
	affected, err := svc.AffectedBy(ctx, "CVE-2020-1234")
	if err != nil {
		t.Fatalf("AffectedBy: %v", err)
	}
	if len(affected) != 0 {
		t.Errorf("stale CVE still affects %d artifacts", len(affected))
	}
}

func TestServiceVulnerabilitiesAndReverseQuery(t *testing.T) {
	ctx := context.Background()
	sqlDB := testDB(t)
	projectID, repoID := seedRepo(t, sqlDB)
	svc := NewService(sqlDB)

	// Two artifacts, both hit by CVE-2020-1234.
	if _, err := svc.Save(ctx, projectID, repoID, "app-a", "sha256:a", "sha256:sa", sampleResult(), "tester"); err != nil {
		t.Fatalf("Save a: %v", err)
	}
	if _, err := svc.Save(ctx, projectID, repoID, "app-b", "sha256:b", "sha256:sb", sampleResult(), "tester"); err != nil {
		t.Fatalf("Save b: %v", err)
	}

	vulns, err := svc.Vulnerabilities(ctx)
	if err != nil {
		t.Fatalf("Vulnerabilities: %v", err)
	}
	if len(vulns) != 2 {
		t.Fatalf("distinct vulns = %d, want 2", len(vulns))
	}
	// Worst severity first.
	if vulns[0].VulnID != "CVE-2020-1234" || vulns[0].Severity != SeverityCritical {
		t.Errorf("top vuln = %+v, want CVE-2020-1234/critical", vulns[0])
	}
	if vulns[0].ArtifactCount != 2 {
		t.Errorf("CVE-2020-1234 affects %d, want 2", vulns[0].ArtifactCount)
	}

	affected, err := svc.AffectedBy(ctx, "CVE-2020-1234")
	if err != nil {
		t.Fatalf("AffectedBy: %v", err)
	}
	if len(affected) != 2 {
		t.Fatalf("affected = %d, want 2", len(affected))
	}
	if affected[0].ProjectKey != "p" || affected[0].RepoKey != "images" {
		t.Errorf("affected[0] keys = %s/%s", affected[0].ProjectKey, affected[0].RepoKey)
	}
	if affected[0].FixedVersion != "1.3.0" {
		t.Errorf("affected[0] fixed = %q, want 1.3.0", affected[0].FixedVersion)
	}
}
