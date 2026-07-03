package httpapi

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/repository"
	"github.com/platbor/platbor/internal/registry"
)

// RetentionService applies each repository's retention policy through the pruner
// for that repository's format. It is instance-wide and admin-driven, mirroring
// GC: pruning removes version/tag metadata, and the blobs those held are
// reclaimed by a later garbage-collection sweep.
type RetentionService struct {
	q       *db.Queries
	pruners map[repository.Format]registry.Pruner
}

// NewRetentionService wires retention to the database and the per-format pruners.
func NewRetentionService(sqlDB *sql.DB, pruners map[repository.Format]registry.Pruner) *RetentionService {
	return &RetentionService{q: db.New(sqlDB), pruners: pruners}
}

// RepoResult is what a run pruned for one repository.
type RepoResult struct {
	ProjectKey string `json:"projectKey"`
	RepoKey    string `json:"repoKey"`
	Deleted    int    `json:"deleted"`
}

// RunReport summarizes a retention run across all policied repositories.
type RunReport struct {
	DryRun       bool         `json:"dryRun"`
	Deleted      int          `json:"deleted"`
	Repositories []RepoResult `json:"repositories"`
}

// Run applies every policied repository's policy through its format's pruner.
func (s *RetentionService) Run(ctx context.Context, dryRun bool, actor string) (RunReport, error) {
	repos, err := s.q.ListRepositoriesWithPolicy(ctx)
	if err != nil {
		return RunReport{}, fmt.Errorf("listing policied repositories: %w", err)
	}
	report := RunReport{DryRun: dryRun}
	for _, r := range repos {
		pruner, ok := s.pruners[repository.Format(r.Format)]
		if !ok {
			continue
		}
		n, err := pruner.Prune(ctx, r.ID, int(r.KeepLast), r.DeleteUntagged != 0, dryRun, actor)
		if err != nil {
			return report, fmt.Errorf("pruning repository %s: %w", r.Key, err)
		}
		if n > 0 {
			report.Repositories = append(report.Repositories, RepoResult{RepoKey: r.Key, Deleted: n})
			report.Deleted += n
		}
	}
	return report, nil
}

// runRetention applies retention across all policied repositories (admin only).
func (h registryHandler) runRetention(w http.ResponseWriter, r *http.Request) {
	dryRun := r.URL.Query().Get("dryRun") == "true"
	report, err := h.retention.Run(r.Context(), dryRun, actorFrom(r))
	if err != nil {
		h.log.Error("running retention", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	writeJSON(w, h.log, http.StatusOK, report)
}
