package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/project"
	"github.com/platbor/platbor/internal/registry/npm"
	"github.com/platbor/platbor/internal/registry/oci"
)

// gcGracePeriod spares blobs written within this window of a sweep: they may be
// freshly uploaded layers whose manifest has not been pushed yet.
const gcGracePeriod = time.Hour

// registryHandler serves the /api/v1/registry endpoints that back the UI
// repository browser: a browsable view of what was pushed over /v2, plus
// session-authenticated deletion and admin garbage collection.
type registryHandler struct {
	browser   *oci.Browser
	packages  *npm.Browser
	manager   *oci.Manager
	collector *oci.Collector
	projects  *project.Service
	log       *slog.Logger
}

func (h registryHandler) mount(r chi.Router) {
	// Global indexes across every project (grouped by project in the UI).
	r.Get("/repositories", h.listRepositories) // OCI repositories
	r.Get("/packages", h.listPackages)         // npm packages
	// Garbage collection is instance-wide and destructive: admins only.
	r.With(requireAdmin).Post("/gc", h.runGC) // ?dryRun=true|false
	// Everything below is scoped to one project.
	r.Route("/{project}", func(r chi.Router) {
		r.Get("/tags", h.listTags)               // ?repository=<repo>
		r.Get("/manifests", h.getManifest)       // ?repository=<repo>&reference=<tag|digest>
		r.Delete("/manifests", h.deleteManifest) // ?repository=<repo>&reference=<tag|digest>
		r.Get("/referrers", h.listReferrers)     // ?repository=<repo>&subject=<digest>
		r.Get("/package", h.getPackage)          // ?repository=<repo>&name=<pkg> (npm detail)
	})
}

// --- responses (camelCase, RFC 3339 timestamps) ---

type repositoryResponse struct {
	ProjectKey    string    `json:"projectKey"`
	ProjectName   string    `json:"projectName"`
	Repository    string    `json:"repository"`
	Kind          string    `json:"kind"` // "local" or "proxy"
	TagCount      int       `json:"tagCount"`
	ManifestCount int       `json:"manifestCount"`
	SizeBytes     int64     `json:"sizeBytes"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type listRepositoriesResponse struct {
	Repositories []repositoryResponse `json:"repositories"`
}

type tagResponse struct {
	Tag       string    `json:"tag"`
	Digest    string    `json:"digest"`
	MediaType string    `json:"mediaType"`
	Kind      string    `json:"kind"`
	Size      int64     `json:"size"`
	Count     int       `json:"count"`
	PushedAt  time.Time `json:"pushedAt"`
}

type listTagsResponse struct {
	Repository string        `json:"repository"`
	Tags       []tagResponse `json:"tags"`
}

type layerResponse struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

type indexEntryResponse struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	Platform  string `json:"platform,omitempty"`
}

type manifestResponse struct {
	Digest    string               `json:"digest"`
	MediaType string               `json:"mediaType"`
	Kind      string               `json:"kind"`
	TotalSize int64                `json:"totalSize"`
	Config    *layerResponse       `json:"config,omitempty"`
	Layers    []layerResponse      `json:"layers"`
	Manifests []indexEntryResponse `json:"manifests"`
}

type referrerResponse struct {
	Digest       string            `json:"digest"`
	MediaType    string            `json:"mediaType"`
	Size         int64             `json:"size"`
	ArtifactType string            `json:"artifactType,omitempty"`
	Annotations  map[string]string `json:"annotations,omitempty"`
}

type listReferrersResponse struct {
	Referrers []referrerResponse `json:"referrers"`
}

// --- handlers ---

func (h registryHandler) listRepositories(w http.ResponseWriter, r *http.Request) {
	repos, err := h.browser.Repositories(r.Context())
	if err != nil {
		h.log.Error("listing repositories", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	items := make([]repositoryResponse, 0, len(repos))
	for _, repo := range repos {
		items = append(items, repositoryResponse{
			ProjectKey:    repo.ProjectKey,
			ProjectName:   repo.ProjectName,
			Repository:    repo.Repository,
			Kind:          repoKind(repo.IsProxy),
			TagCount:      repo.TagCount,
			ManifestCount: repo.ManifestCount,
			SizeBytes:     repo.SizeBytes,
			UpdatedAt:     repo.UpdatedAt,
		})
	}
	writeJSON(w, h.log, http.StatusOK, listRepositoriesResponse{Repositories: items})
}

// repoKind maps the proxy flag to the wire kind the projects API also uses.
func repoKind(isProxy bool) string {
	if isProxy {
		return "proxy"
	}
	return "local"
}

type packageResponse struct {
	ProjectKey   string    `json:"projectKey"`
	ProjectName  string    `json:"projectName"`
	Name         string    `json:"name"`
	Kind         string    `json:"kind"` // "local" or "proxy"
	VersionCount int       `json:"versionCount"`
	SizeBytes    int64     `json:"sizeBytes"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type listPackagesResponse struct {
	Packages []packageResponse `json:"packages"`
}

// listPackages returns every npm package across all projects, grouped by project
// in the UI (mirrors listRepositories for the OCI format).
func (h registryHandler) listPackages(w http.ResponseWriter, r *http.Request) {
	pkgs, err := h.packages.Packages(r.Context())
	if err != nil {
		h.log.Error("listing packages", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	items := make([]packageResponse, 0, len(pkgs))
	for _, p := range pkgs {
		items = append(items, packageResponse{
			ProjectKey:   p.ProjectKey,
			ProjectName:  p.ProjectName,
			Name:         p.Name,
			Kind:         repoKind(p.IsProxy),
			VersionCount: p.VersionCount,
			SizeBytes:    p.SizeBytes,
			UpdatedAt:    p.UpdatedAt,
		})
	}
	writeJSON(w, h.log, http.StatusOK, listPackagesResponse{Packages: items})
}

type packageVersionResponse struct {
	Version     string    `json:"version"`
	SizeBytes   int64     `json:"sizeBytes"`
	Shasum      string    `json:"shasum"`
	Integrity   string    `json:"integrity"`
	PublishedAt time.Time `json:"publishedAt"`
}

type packageDetailResponse struct {
	Name     string                   `json:"name"`
	DistTags map[string]string        `json:"distTags"`
	Versions []packageVersionResponse `json:"versions"`
}

// getPackage returns one npm package's versions and dist-tags for the detail
// page. The project comes from the path; the package name is a query param.
func (h registryHandler) getPackage(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "project")
	name := r.URL.Query().Get("name")
	if name == "" {
		writeProblem(w, http.StatusBadRequest, "Missing parameter", "name is required")
		return
	}

	detail, err := h.packages.Package(r.Context(), projectKey, name)
	if err != nil {
		if errors.Is(err, npm.ErrPackageNotFound) {
			writeProblem(w, http.StatusNotFound, "Package not found", "no such package in that project")
			return
		}
		h.log.Error("getting package", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}

	versions := make([]packageVersionResponse, 0, len(detail.Versions))
	for _, v := range detail.Versions {
		versions = append(versions, packageVersionResponse{
			Version:     v.Version,
			SizeBytes:   v.SizeBytes,
			Shasum:      v.Shasum,
			Integrity:   v.Integrity,
			PublishedAt: v.PublishedAt,
		})
	}
	writeJSON(w, h.log, http.StatusOK, packageDetailResponse{
		Name:     detail.Name,
		DistTags: detail.DistTags,
		Versions: versions,
	})
}

func (h registryHandler) listTags(w http.ResponseWriter, r *http.Request) {
	proj, repo, ok := h.resolveScope(w, r)
	if !ok {
		return
	}
	tags, err := h.browser.Tags(r.Context(), proj.ID, repo)
	if err != nil {
		h.log.Error("listing tags", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	items := make([]tagResponse, 0, len(tags))
	for _, t := range tags {
		items = append(items, tagResponse{
			Tag:       t.Tag,
			Digest:    t.Digest,
			MediaType: t.MediaType,
			Kind:      t.Kind,
			Size:      t.Size,
			Count:     t.Count,
			PushedAt:  t.PushedAt,
		})
	}
	writeJSON(w, h.log, http.StatusOK, listTagsResponse{Repository: repo, Tags: items})
}

func (h registryHandler) getManifest(w http.ResponseWriter, r *http.Request) {
	proj, repo, ok := h.resolveScope(w, r)
	if !ok {
		return
	}
	reference := r.URL.Query().Get("reference")
	if reference == "" {
		writeProblem(w, http.StatusBadRequest, "Missing reference", "the reference query parameter is required")
		return
	}

	view, err := h.browser.Manifest(r.Context(), proj.ID, repo, reference)
	if err != nil {
		if errors.Is(err, oci.ErrManifestNotFound) {
			writeProblem(w, http.StatusNotFound, "Manifest not found", "no manifest for that reference")
			return
		}
		h.log.Error("getting manifest", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	writeJSON(w, h.log, http.StatusOK, toManifestResponse(view))
}

type gcResponse struct {
	DryRun         bool  `json:"dryRun"`
	Scanned        int   `json:"scanned"`
	Deleted        int   `json:"deleted"`
	ReclaimedBytes int64 `json:"reclaimedBytes"`
	Kept           int   `json:"kept"`
}

// runGC marks blobs referenced by any manifest and sweeps the rest (older than
// the grace window). ?dryRun=true reports what would be removed without deleting.
func (h registryHandler) runGC(w http.ResponseWriter, r *http.Request) {
	dryRun := r.URL.Query().Get("dryRun") == "true"

	report, err := h.collector.Collect(r.Context(), actorFrom(r), gcGracePeriod, dryRun, time.Now().UTC())
	if err != nil {
		h.log.Error("garbage collection", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	writeJSON(w, h.log, http.StatusOK, gcResponse{
		DryRun:         dryRun,
		Scanned:        report.Scanned,
		Deleted:        report.Deleted,
		ReclaimedBytes: report.ReclaimedBytes,
		Kept:           report.Kept,
	})
}

// listReferrers returns the artifacts (signatures, SBOMs, attestations) whose
// subject is the given manifest digest.
func (h registryHandler) listReferrers(w http.ResponseWriter, r *http.Request) {
	proj, repo, ok := h.resolveScope(w, r)
	if !ok {
		return
	}
	subject := r.URL.Query().Get("subject")
	if subject == "" {
		writeProblem(w, http.StatusBadRequest, "Missing subject", "the subject query parameter is required")
		return
	}

	refs, err := h.browser.Referrers(r.Context(), proj.ID, repo, subject)
	if err != nil {
		h.log.Error("listing referrers", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	items := make([]referrerResponse, 0, len(refs))
	for _, ref := range refs {
		items = append(items, referrerResponse{
			Digest:       ref.Digest,
			MediaType:    ref.MediaType,
			Size:         ref.Size,
			ArtifactType: ref.ArtifactType,
			Annotations:  ref.Annotations,
		})
	}
	writeJSON(w, h.log, http.StatusOK, listReferrersResponse{Referrers: items})
}

// deleteManifest removes a tag (reference is a tag) or a whole manifest and all
// its tags (reference is a digest). Deletion is audited by the store as the
// authenticated user.
func (h registryHandler) deleteManifest(w http.ResponseWriter, r *http.Request) {
	proj, repo, ok := h.resolveScope(w, r)
	if !ok {
		return
	}
	reference := r.URL.Query().Get("reference")
	if reference == "" {
		writeProblem(w, http.StatusBadRequest, "Missing reference", "the reference query parameter is required")
		return
	}

	actor := actorFrom(r)
	var err error
	if strings.Contains(reference, ":") {
		// A digest reference deletes the manifest and every tag pointing at it.
		err = h.manager.DeleteManifest(r.Context(), proj.ID, repo, reference, actor)
	} else {
		err = h.manager.DeleteTag(r.Context(), proj.ID, repo, reference, actor)
	}
	if err != nil {
		if errors.Is(err, oci.ErrManifestNotFound) {
			writeProblem(w, http.StatusNotFound, "Not found", "no tag or manifest for that reference")
			return
		}
		h.log.Error("deleting manifest", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resolveScope validates the {project} path param and the required repository
// query param, returning the resolved project and repository. It writes the
// error response itself when either is missing.
func (h registryHandler) resolveScope(w http.ResponseWriter, r *http.Request) (project.Project, string, bool) {
	key := chi.URLParam(r, "project")
	repo := r.URL.Query().Get("repository")
	if repo == "" {
		writeProblem(w, http.StatusBadRequest, "Missing repository", "the repository query parameter is required")
		return project.Project{}, "", false
	}

	proj, err := h.projects.GetByKey(r.Context(), key)
	if err != nil {
		if errors.Is(err, project.ErrNotFound) {
			writeProblem(w, http.StatusNotFound, "Project not found", "no project with key "+key)
			return project.Project{}, "", false
		}
		h.log.Error("resolving project", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return project.Project{}, "", false
	}
	return proj, repo, true
}

func toManifestResponse(v oci.ManifestView) manifestResponse {
	resp := manifestResponse{
		Digest:    v.Digest,
		MediaType: v.MediaType,
		Kind:      v.Kind,
		TotalSize: v.TotalSize,
		Layers:    make([]layerResponse, 0, len(v.Layers)),
		Manifests: make([]indexEntryResponse, 0, len(v.Manifests)),
	}
	if v.Config != nil {
		resp.Config = &layerResponse{MediaType: v.Config.MediaType, Digest: v.Config.Digest, Size: v.Config.Size}
	}
	for _, l := range v.Layers {
		resp.Layers = append(resp.Layers, layerResponse{MediaType: l.MediaType, Digest: l.Digest, Size: l.Size})
	}
	for _, m := range v.Manifests {
		resp.Manifests = append(resp.Manifests, indexEntryResponse{MediaType: m.MediaType, Digest: m.Digest, Size: m.Size, Platform: m.Platform})
	}
	return resp
}
