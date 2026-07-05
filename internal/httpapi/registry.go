package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/core/project"
	"github.com/platbor/platbor/internal/core/repository"
	"github.com/platbor/platbor/internal/registry/cargo"
	"github.com/platbor/platbor/internal/registry/generic"
	"github.com/platbor/platbor/internal/registry/goproxy"
	"github.com/platbor/platbor/internal/registry/maven"
	"github.com/platbor/platbor/internal/registry/npm"
	"github.com/platbor/platbor/internal/registry/nuget"
	"github.com/platbor/platbor/internal/registry/oci"
	"github.com/platbor/platbor/internal/registry/pypi"
	"github.com/platbor/platbor/internal/registry/rubygems"
	"github.com/platbor/platbor/internal/registry/terraform"
	"github.com/platbor/platbor/internal/scan"
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
	nugets    *nuget.Browser
	generics  *generic.Browser
	pypis     *pypi.Browser
	mavens    *maven.Browser
	gomods    *goproxy.Browser
	crates    *cargo.Browser
	gems      *rubygems.Browser
	modules   *terraform.Browser
	manager   *oci.Manager
	collector *oci.Collector
	retention *RetentionService
	repos     *repository.Service
	projects  *project.Service
	blobs     blob.Store
	scanner   *scan.Scanner
	scans     *scan.Service
	// scanEnabled gates the vulnerability-scan endpoints; when off, scanning is
	// disabled instance-wide (the OSV lookup is the only network call scanning
	// makes, so operators offline by policy can turn it off).
	scanEnabled bool
	log         *slog.Logger
}

func (h registryHandler) mount(r chi.Router) {
	// Global indexes across every project (grouped by project in the UI).
	r.Get("/repositories", h.listRepositories)  // OCI repositories
	r.Get("/packages", h.listPackages)          // npm packages
	r.Get("/nuget-packages", h.listNugets)      // NuGet packages
	r.Get("/pypi-packages", h.listPypis)        // PyPI packages
	r.Get("/maven-artifacts", h.listMavens)     // Maven artifacts
	r.Get("/go-modules", h.listGoModules)       // Go modules
	r.Get("/cargo-crates", h.listCrates)        // Cargo crates
	r.Get("/rubygems", h.listGems)              // RubyGems gems
	r.Get("/terraform-modules", h.listModules)  // Terraform modules
	r.Get("/generic-files", h.listGenericFiles) // generic files
	// Garbage collection is instance-wide and destructive: admins only.
	r.With(requireAdmin).Post("/gc", h.runGC) // ?dryRun=true|false
	// Retention: an admin-triggered run across all policied repositories. The
	// per-repository policy config lives on the repository (PUT under projects).
	r.With(requireAdmin).Post("/retention", h.runRetention) // ?dryRun=true|false
	// Everything below is scoped to one project and repository (?repo=<repoKey>).
	r.Route("/{project}", func(r chi.Router) {
		r.Get("/tags", h.listTags)               // ?repo=<repo>&image=<image>
		r.Get("/manifests", h.getManifest)       // ?repo=<repo>&image=<image>&reference=<tag|digest>
		r.Delete("/manifests", h.deleteManifest) // ?repo=<repo>&image=<image>&reference=<tag|digest>
		r.Get("/referrers", h.listReferrers)     // ?repo=<repo>&image=<image>&subject=<digest>
		r.Get("/sbom", h.getSBOM)                // ?repo=<repo>&image=<image>&digest=<sbom-referrer-digest>
		r.Get("/scan", h.getScan)                // ?repo=<repo>&image=<image>&digest=<manifest-digest>
		r.Post("/scan", h.runScan)               // ?repo=<repo>&image=<image>&digest=<manifest-digest>
		r.Get("/package", h.getPackage)          // ?repo=<repo>&name=<pkg> (npm detail)
		r.Get("/nuget-package", h.getNuget)      // ?repo=<repo>&id=<id> (NuGet detail)
		r.Get("/pypi-package", h.getPypi)        // ?repo=<repo>&name=<pkg> (PyPI detail)
		r.Get("/maven-artifact", h.getMaven)     // ?repo=<repo>&group=<g>&artifact=<a> (Maven detail)
		r.Get("/go-module", h.getGoModule)       // ?repo=<repo>&module=<m> (Go detail)
		r.Get("/cargo-crate", h.getCrate)        // ?repo=<repo>&name=<crate> (Cargo detail)
		r.Get("/rubygem", h.getGem)              // ?repo=<repo>&name=<gem> (RubyGems detail)
		r.Get("/terraform-module", h.getModule)  // ?repo=<repo>&name=<n>&provider=<p> (Terraform detail)
	})
}

// --- responses (camelCase, RFC 3339 timestamps) ---

type repositoryResponse struct {
	ProjectKey    string    `json:"projectKey"`
	ProjectName   string    `json:"projectName"`
	RepoKey       string    `json:"repoKey"`
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
			RepoKey:       repo.RepoKey,
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
	RepoKey      string    `json:"repoKey"`
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
			RepoKey:      p.RepoKey,
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
	Readme   string                   `json:"readme,omitempty"`
}

// getPackage returns one npm package's versions and dist-tags for the detail
// page. The project comes from the path; the package name is a query param.
func (h registryHandler) getPackage(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "project")
	repoKey := r.URL.Query().Get("repo")
	name := r.URL.Query().Get("name")
	if repoKey == "" || name == "" {
		writeProblem(w, http.StatusBadRequest, "Missing parameter", "repo and name are required")
		return
	}

	detail, err := h.packages.Package(r.Context(), projectKey, repoKey, name)
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
		Readme:   detail.Readme,
	})
}

// --- NuGet ---

type nugetResponse struct {
	ProjectKey   string    `json:"projectKey"`
	ProjectName  string    `json:"projectName"`
	RepoKey      string    `json:"repoKey"`
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	VersionCount int       `json:"versionCount"`
	SizeBytes    int64     `json:"sizeBytes"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type listNugetsResponse struct {
	Packages []nugetResponse `json:"packages"`
}

// listNugets returns every NuGet package across all projects.
func (h registryHandler) listNugets(w http.ResponseWriter, r *http.Request) {
	pkgs, err := h.nugets.Packages(r.Context())
	if err != nil {
		h.log.Error("listing nuget packages", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	items := make([]nugetResponse, 0, len(pkgs))
	for _, p := range pkgs {
		items = append(items, nugetResponse{
			ProjectKey:   p.ProjectKey,
			ProjectName:  p.ProjectName,
			RepoKey:      p.RepoKey,
			ID:           p.ID,
			Kind:         repoKind(p.IsProxy),
			VersionCount: p.VersionCount,
			SizeBytes:    p.SizeBytes,
			UpdatedAt:    p.UpdatedAt,
		})
	}
	writeJSON(w, h.log, http.StatusOK, listNugetsResponse{Packages: items})
}

type nugetVersionResponse struct {
	Version     string    `json:"version"`
	SizeBytes   int64     `json:"sizeBytes"`
	PublishedAt time.Time `json:"publishedAt"`
}

type nugetDetailResponse struct {
	ID       string                 `json:"id"`
	Versions []nugetVersionResponse `json:"versions"`
	Readme   string                 `json:"readme,omitempty"`
}

// getNuget returns one NuGet package's versions for the detail page.
func (h registryHandler) getNuget(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "project")
	repoKey := r.URL.Query().Get("repo")
	pkgID := r.URL.Query().Get("id")
	if repoKey == "" || pkgID == "" {
		writeProblem(w, http.StatusBadRequest, "Missing parameter", "repo and id are required")
		return
	}
	detail, err := h.nugets.Package(r.Context(), projectKey, repoKey, pkgID)
	if err != nil {
		if errors.Is(err, nuget.ErrPackageNotFound) {
			writeProblem(w, http.StatusNotFound, "Package not found", "no such package in that project")
			return
		}
		h.log.Error("getting nuget package", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	versions := make([]nugetVersionResponse, 0, len(detail.Versions))
	for _, v := range detail.Versions {
		versions = append(versions, nugetVersionResponse{Version: v.Version, SizeBytes: v.SizeBytes, PublishedAt: v.PublishedAt})
	}
	writeJSON(w, h.log, http.StatusOK, nugetDetailResponse{ID: detail.ID, Versions: versions, Readme: detail.Description})
}

// --- PyPI ---

type pypiResponse struct {
	ProjectKey  string    `json:"projectKey"`
	ProjectName string    `json:"projectName"`
	RepoKey     string    `json:"repoKey"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	FileCount   int       `json:"fileCount"`
	SizeBytes   int64     `json:"sizeBytes"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type listPypisResponse struct {
	Packages []pypiResponse `json:"packages"`
}

// listPypis returns every PyPI package across all projects.
func (h registryHandler) listPypis(w http.ResponseWriter, r *http.Request) {
	pkgs, err := h.pypis.Packages(r.Context())
	if err != nil {
		h.log.Error("listing pypi packages", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	items := make([]pypiResponse, 0, len(pkgs))
	for _, p := range pkgs {
		items = append(items, pypiResponse{
			ProjectKey:  p.ProjectKey,
			ProjectName: p.ProjectName,
			RepoKey:     p.RepoKey,
			Name:        p.Name,
			Kind:        repoKind(p.IsProxy),
			FileCount:   p.FileCount,
			SizeBytes:   p.SizeBytes,
			UpdatedAt:   p.UpdatedAt,
		})
	}
	writeJSON(w, h.log, http.StatusOK, listPypisResponse{Packages: items})
}

type pypiFileResponse struct {
	Filename       string `json:"filename"`
	Version        string `json:"version"`
	SizeBytes      int64  `json:"sizeBytes"`
	SHA256         string `json:"sha256"`
	RequiresPython string `json:"requiresPython,omitempty"`
}

type pypiDetailResponse struct {
	Name  string             `json:"name"`
	Files []pypiFileResponse `json:"files"`
}

// getPypi returns one PyPI package's distribution files for the detail page.
func (h registryHandler) getPypi(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "project")
	repoKey := r.URL.Query().Get("repo")
	name := r.URL.Query().Get("name")
	if repoKey == "" || name == "" {
		writeProblem(w, http.StatusBadRequest, "Missing parameter", "repo and name are required")
		return
	}
	detail, err := h.pypis.Package(r.Context(), projectKey, repoKey, name)
	if err != nil {
		if errors.Is(err, pypi.ErrPackageNotFound) {
			writeProblem(w, http.StatusNotFound, "Package not found", "no such package in that project")
			return
		}
		h.log.Error("getting pypi package", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	files := make([]pypiFileResponse, 0, len(detail.Files))
	for _, f := range detail.Files {
		files = append(files, pypiFileResponse{
			Filename: f.Filename, Version: f.Version, SizeBytes: f.SizeBytes,
			SHA256: f.SHA256, RequiresPython: f.RequiresPython,
		})
	}
	writeJSON(w, h.log, http.StatusOK, pypiDetailResponse{Name: detail.Name, Files: files})
}

// --- Maven ---

type mavenResponse struct {
	ProjectKey   string    `json:"projectKey"`
	ProjectName  string    `json:"projectName"`
	RepoKey      string    `json:"repoKey"`
	GroupID      string    `json:"groupId"`
	ArtifactID   string    `json:"artifactId"`
	Kind         string    `json:"kind"`
	VersionCount int       `json:"versionCount"`
	SizeBytes    int64     `json:"sizeBytes"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type listMavensResponse struct {
	Artifacts []mavenResponse `json:"artifacts"`
}

// listMavens returns every Maven artifact across all projects.
func (h registryHandler) listMavens(w http.ResponseWriter, r *http.Request) {
	arts, err := h.mavens.Artifacts(r.Context())
	if err != nil {
		h.log.Error("listing maven artifacts", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	items := make([]mavenResponse, 0, len(arts))
	for _, a := range arts {
		items = append(items, mavenResponse{
			ProjectKey:   a.ProjectKey,
			ProjectName:  a.ProjectName,
			RepoKey:      a.RepoKey,
			GroupID:      a.GroupID,
			ArtifactID:   a.ArtifactID,
			Kind:         repoKind(a.IsProxy),
			VersionCount: a.VersionCount,
			SizeBytes:    a.SizeBytes,
			UpdatedAt:    a.UpdatedAt,
		})
	}
	writeJSON(w, h.log, http.StatusOK, listMavensResponse{Artifacts: items})
}

type mavenFileResponse struct {
	Path       string `json:"path"`
	Version    string `json:"version"`
	Filename   string `json:"filename"`
	IsMetadata bool   `json:"isMetadata"`
	SizeBytes  int64  `json:"sizeBytes"`
	SHA1       string `json:"sha1,omitempty"`
}

type mavenDetailResponse struct {
	GroupID    string              `json:"groupId"`
	ArtifactID string              `json:"artifactId"`
	Files      []mavenFileResponse `json:"files"`
}

// getMaven returns one Maven artifact's files for the detail page.
func (h registryHandler) getMaven(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "project")
	repoKey := r.URL.Query().Get("repo")
	group := r.URL.Query().Get("group")
	artifact := r.URL.Query().Get("artifact")
	if repoKey == "" || group == "" || artifact == "" {
		writeProblem(w, http.StatusBadRequest, "Missing parameter", "repo, group and artifact are required")
		return
	}
	detail, err := h.mavens.Artifact(r.Context(), projectKey, repoKey, group, artifact)
	if err != nil {
		if errors.Is(err, maven.ErrArtifactNotFound) {
			writeProblem(w, http.StatusNotFound, "Artifact not found", "no such artifact in that project")
			return
		}
		h.log.Error("getting maven artifact", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	files := make([]mavenFileResponse, 0, len(detail.Files))
	for _, f := range detail.Files {
		files = append(files, mavenFileResponse{
			Path: f.Path, Version: f.Version, Filename: f.Filename,
			IsMetadata: f.IsMetadata, SizeBytes: f.SizeBytes, SHA1: f.SHA1,
		})
	}
	writeJSON(w, h.log, http.StatusOK, mavenDetailResponse{GroupID: detail.GroupID, ArtifactID: detail.ArtifactID, Files: files})
}

// --- Go modules ---

type goModuleResponse struct {
	ProjectKey   string    `json:"projectKey"`
	ProjectName  string    `json:"projectName"`
	RepoKey      string    `json:"repoKey"`
	Module       string    `json:"module"`
	Kind         string    `json:"kind"`
	VersionCount int       `json:"versionCount"`
	SizeBytes    int64     `json:"sizeBytes"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type listGoModulesResponse struct {
	Modules []goModuleResponse `json:"modules"`
}

// listGoModules returns every cached Go module across all projects.
func (h registryHandler) listGoModules(w http.ResponseWriter, r *http.Request) {
	mods, err := h.gomods.Modules(r.Context())
	if err != nil {
		h.log.Error("listing go modules", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	items := make([]goModuleResponse, 0, len(mods))
	for _, m := range mods {
		items = append(items, goModuleResponse{
			ProjectKey:   m.ProjectKey,
			ProjectName:  m.ProjectName,
			RepoKey:      m.RepoKey,
			Module:       m.Module,
			Kind:         repoKind(m.IsProxy),
			VersionCount: m.VersionCount,
			SizeBytes:    m.SizeBytes,
			UpdatedAt:    m.UpdatedAt,
		})
	}
	writeJSON(w, h.log, http.StatusOK, listGoModulesResponse{Modules: items})
}

type goVersionResponse struct {
	Version   string `json:"version"`
	SizeBytes int64  `json:"sizeBytes"`
	HasZip    bool   `json:"hasZip"`
}

type goModuleDetailResponse struct {
	Module   string              `json:"module"`
	Versions []goVersionResponse `json:"versions"`
}

// getGoModule returns one Go module's cached versions for the detail page.
func (h registryHandler) getGoModule(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "project")
	repoKey := r.URL.Query().Get("repo")
	module := r.URL.Query().Get("module")
	if repoKey == "" || module == "" {
		writeProblem(w, http.StatusBadRequest, "Missing parameter", "repo and module are required")
		return
	}
	detail, err := h.gomods.Module(r.Context(), projectKey, repoKey, module)
	if err != nil {
		if errors.Is(err, goproxy.ErrModuleNotFound) {
			writeProblem(w, http.StatusNotFound, "Module not found", "no such module in that project")
			return
		}
		h.log.Error("getting go module", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	versions := make([]goVersionResponse, 0, len(detail.Versions))
	for _, v := range detail.Versions {
		versions = append(versions, goVersionResponse{Version: v.Version, SizeBytes: v.SizeBytes, HasZip: v.HasZip})
	}
	writeJSON(w, h.log, http.StatusOK, goModuleDetailResponse{Module: detail.Module, Versions: versions})
}

// --- Cargo crates ---

type cargoCrateResponse struct {
	ProjectKey   string    `json:"projectKey"`
	ProjectName  string    `json:"projectName"`
	RepoKey      string    `json:"repoKey"`
	Name         string    `json:"name"`
	Kind         string    `json:"kind"`
	VersionCount int       `json:"versionCount"`
	SizeBytes    int64     `json:"sizeBytes"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type listCratesResponse struct {
	Crates []cargoCrateResponse `json:"crates"`
}

// listCrates returns every Cargo crate across all projects.
func (h registryHandler) listCrates(w http.ResponseWriter, r *http.Request) {
	crates, err := h.crates.Crates(r.Context())
	if err != nil {
		h.log.Error("listing cargo crates", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	items := make([]cargoCrateResponse, 0, len(crates))
	for _, c := range crates {
		items = append(items, cargoCrateResponse{
			ProjectKey:   c.ProjectKey,
			ProjectName:  c.ProjectName,
			RepoKey:      c.RepoKey,
			Name:         c.Name,
			Kind:         repoKind(c.IsProxy),
			VersionCount: c.VersionCount,
			SizeBytes:    c.SizeBytes,
			UpdatedAt:    c.UpdatedAt,
		})
	}
	writeJSON(w, h.log, http.StatusOK, listCratesResponse{Crates: items})
}

type cargoVersionResponse struct {
	Version   string `json:"version"`
	SizeBytes int64  `json:"sizeBytes"`
	Yanked    bool   `json:"yanked"`
	Cksum     string `json:"cksum,omitempty"`
}

type cargoCrateDetailResponse struct {
	Name     string                 `json:"name"`
	Versions []cargoVersionResponse `json:"versions"`
}

// getCrate returns one Cargo crate's versions for the detail page.
func (h registryHandler) getCrate(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "project")
	repoKey := r.URL.Query().Get("repo")
	name := r.URL.Query().Get("name")
	if repoKey == "" || name == "" {
		writeProblem(w, http.StatusBadRequest, "Missing parameter", "repo and name are required")
		return
	}
	detail, err := h.crates.Crate(r.Context(), projectKey, repoKey, name)
	if err != nil {
		if errors.Is(err, cargo.ErrCrateNotFound) {
			writeProblem(w, http.StatusNotFound, "Crate not found", "no such crate in that project")
			return
		}
		h.log.Error("getting cargo crate", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	versions := make([]cargoVersionResponse, 0, len(detail.Versions))
	for _, v := range detail.Versions {
		versions = append(versions, cargoVersionResponse{Version: v.Version, SizeBytes: v.SizeBytes, Yanked: v.Yanked, Cksum: v.Cksum})
	}
	writeJSON(w, h.log, http.StatusOK, cargoCrateDetailResponse{Name: detail.Name, Versions: versions})
}

// --- RubyGems ---

type gemResponse struct {
	ProjectKey   string    `json:"projectKey"`
	ProjectName  string    `json:"projectName"`
	RepoKey      string    `json:"repoKey"`
	Name         string    `json:"name"`
	Kind         string    `json:"kind"`
	VersionCount int       `json:"versionCount"`
	SizeBytes    int64     `json:"sizeBytes"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type listGemsResponse struct {
	Gems []gemResponse `json:"gems"`
}

// listGems returns every RubyGems gem across all projects.
func (h registryHandler) listGems(w http.ResponseWriter, r *http.Request) {
	gems, err := h.gems.Gems(r.Context())
	if err != nil {
		h.log.Error("listing gems", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	items := make([]gemResponse, 0, len(gems))
	for _, g := range gems {
		items = append(items, gemResponse{
			ProjectKey:   g.ProjectKey,
			ProjectName:  g.ProjectName,
			RepoKey:      g.RepoKey,
			Name:         g.Name,
			Kind:         repoKind(g.IsProxy),
			VersionCount: g.VersionCount,
			SizeBytes:    g.SizeBytes,
			UpdatedAt:    g.UpdatedAt,
		})
	}
	writeJSON(w, h.log, http.StatusOK, listGemsResponse{Gems: items})
}

type gemVersionResponse struct {
	Number    string `json:"number"`
	Version   string `json:"version"`
	Platform  string `json:"platform"`
	SizeBytes int64  `json:"sizeBytes"`
	Yanked    bool   `json:"yanked"`
	SHA256    string `json:"sha256,omitempty"`
}

type gemDetailResponse struct {
	Name     string               `json:"name"`
	Versions []gemVersionResponse `json:"versions"`
}

// getGem returns one gem's versions for the detail page.
func (h registryHandler) getGem(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "project")
	repoKey := r.URL.Query().Get("repo")
	name := r.URL.Query().Get("name")
	if repoKey == "" || name == "" {
		writeProblem(w, http.StatusBadRequest, "Missing parameter", "repo and name are required")
		return
	}
	detail, err := h.gems.Gem(r.Context(), projectKey, repoKey, name)
	if err != nil {
		if errors.Is(err, rubygems.ErrGemNotFoundBrowse) {
			writeProblem(w, http.StatusNotFound, "Gem not found", "no such gem in that project")
			return
		}
		h.log.Error("getting gem", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	versions := make([]gemVersionResponse, 0, len(detail.Versions))
	for _, v := range detail.Versions {
		versions = append(versions, gemVersionResponse{
			Number: v.Number, Version: v.Version, Platform: v.Platform, SizeBytes: v.SizeBytes, Yanked: v.Yanked, SHA256: v.SHA256,
		})
	}
	writeJSON(w, h.log, http.StatusOK, gemDetailResponse{Name: detail.Name, Versions: versions})
}

// --- Terraform modules ---

type terraformModuleResponse struct {
	ProjectKey   string    `json:"projectKey"`
	ProjectName  string    `json:"projectName"`
	RepoKey      string    `json:"repoKey"`
	Name         string    `json:"name"`
	Provider     string    `json:"provider"`
	Kind         string    `json:"kind"`
	VersionCount int       `json:"versionCount"`
	SizeBytes    int64     `json:"sizeBytes"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type listModulesResponse struct {
	Modules []terraformModuleResponse `json:"modules"`
}

// listModules returns every Terraform module across all projects.
func (h registryHandler) listModules(w http.ResponseWriter, r *http.Request) {
	mods, err := h.modules.Modules(r.Context())
	if err != nil {
		h.log.Error("listing terraform modules", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	items := make([]terraformModuleResponse, 0, len(mods))
	for _, m := range mods {
		items = append(items, terraformModuleResponse{
			ProjectKey:   m.ProjectKey,
			ProjectName:  m.ProjectName,
			RepoKey:      m.RepoKey,
			Name:         m.Name,
			Provider:     m.Provider,
			Kind:         repoKind(m.IsProxy),
			VersionCount: m.VersionCount,
			SizeBytes:    m.SizeBytes,
			UpdatedAt:    m.UpdatedAt,
		})
	}
	writeJSON(w, h.log, http.StatusOK, listModulesResponse{Modules: items})
}

type terraformVersionResponse struct {
	Version   string `json:"version"`
	SizeBytes int64  `json:"sizeBytes"`
}

type terraformModuleDetailResponse struct {
	Name     string                     `json:"name"`
	Provider string                     `json:"provider"`
	Versions []terraformVersionResponse `json:"versions"`
}

// getModule returns one Terraform module's versions for the detail page.
func (h registryHandler) getModule(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "project")
	repoKey := r.URL.Query().Get("repo")
	name := r.URL.Query().Get("name")
	provider := r.URL.Query().Get("provider")
	if repoKey == "" || name == "" || provider == "" {
		writeProblem(w, http.StatusBadRequest, "Missing parameter", "repo, name and provider are required")
		return
	}
	detail, err := h.modules.Module(r.Context(), projectKey, repoKey, name, provider)
	if err != nil {
		if errors.Is(err, terraform.ErrModuleNotFoundBrowse) {
			writeProblem(w, http.StatusNotFound, "Module not found", "no such module in that project")
			return
		}
		h.log.Error("getting terraform module", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	versions := make([]terraformVersionResponse, 0, len(detail.Versions))
	for _, v := range detail.Versions {
		versions = append(versions, terraformVersionResponse{Version: v.Version, SizeBytes: v.SizeBytes})
	}
	writeJSON(w, h.log, http.StatusOK, terraformModuleDetailResponse{Name: detail.Name, Provider: detail.Provider, Versions: versions})
}

// --- generic ---

type genericFileResponse struct {
	ProjectKey  string    `json:"projectKey"`
	ProjectName string    `json:"projectName"`
	RepoKey     string    `json:"repoKey"`
	Path        string    `json:"path"`
	Kind        string    `json:"kind"`
	SizeBytes   int64     `json:"sizeBytes"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type listGenericFilesResponse struct {
	Files []genericFileResponse `json:"files"`
}

// listGenericFiles returns every generic file across all projects.
func (h registryHandler) listGenericFiles(w http.ResponseWriter, r *http.Request) {
	files, err := h.generics.Files(r.Context())
	if err != nil {
		h.log.Error("listing generic files", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	items := make([]genericFileResponse, 0, len(files))
	for _, f := range files {
		items = append(items, genericFileResponse{
			ProjectKey:  f.ProjectKey,
			ProjectName: f.ProjectName,
			RepoKey:     f.RepoKey,
			Path:        f.Path,
			Kind:        repoKind(f.IsProxy),
			SizeBytes:   f.SizeBytes,
			UpdatedAt:   f.UpdatedAt,
		})
	}
	writeJSON(w, h.log, http.StatusOK, listGenericFilesResponse{Files: items})
}

func (h registryHandler) listTags(w http.ResponseWriter, r *http.Request) {
	repo, image, ok := h.resolveScope(w, r)
	if !ok {
		return
	}
	tags, err := h.browser.Tags(r.Context(), repo.ID, image)
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
	writeJSON(w, h.log, http.StatusOK, listTagsResponse{Repository: image, Tags: items})
}

func (h registryHandler) getManifest(w http.ResponseWriter, r *http.Request) {
	repo, image, ok := h.resolveScope(w, r)
	if !ok {
		return
	}
	reference := r.URL.Query().Get("reference")
	if reference == "" {
		writeProblem(w, http.StatusBadRequest, "Missing reference", "the reference query parameter is required")
		return
	}

	view, err := h.browser.Manifest(r.Context(), repo.ID, image, reference)
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
	repo, image, ok := h.resolveScope(w, r)
	if !ok {
		return
	}
	subject := r.URL.Query().Get("subject")
	if subject == "" {
		writeProblem(w, http.StatusBadRequest, "Missing subject", "the subject query parameter is required")
		return
	}

	refs, err := h.browser.Referrers(r.Context(), repo.ID, image, subject)
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
	repo, image, ok := h.resolveScope(w, r)
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
		err = h.manager.DeleteManifest(r.Context(), repo.ID, repo.ProjectID, image, reference, actor)
	} else {
		err = h.manager.DeleteTag(r.Context(), repo.ID, repo.ProjectID, image, reference, actor)
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

// resolveScope resolves the {project} path param plus the ?repo (typed
// repository key) and ?image (OCI image name) query params to an OCI repository
// and the image within it. It writes the error response itself on any problem.
func (h registryHandler) resolveScope(w http.ResponseWriter, r *http.Request) (repository.Repository, string, bool) {
	projectKey := chi.URLParam(r, "project")
	repoKey := r.URL.Query().Get("repo")
	image := r.URL.Query().Get("image")
	if repoKey == "" || image == "" {
		writeProblem(w, http.StatusBadRequest, "Missing parameter", "the repo and image query parameters are required")
		return repository.Repository{}, "", false
	}
	proj, err := h.projects.GetByKey(r.Context(), projectKey)
	if err != nil {
		if errors.Is(err, project.ErrNotFound) {
			writeProblem(w, http.StatusNotFound, "Project not found", "no project with key "+projectKey)
			return repository.Repository{}, "", false
		}
		h.log.Error("resolving project", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return repository.Repository{}, "", false
	}
	repo, err := h.repos.GetForFormat(r.Context(), proj.ID, repoKey, repository.FormatOCI)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) || errors.Is(err, repository.ErrFormatMismatch) {
			writeProblem(w, http.StatusNotFound, "Repository not found", "no OCI repository "+repoKey+" in "+projectKey)
			return repository.Repository{}, "", false
		}
		h.log.Error("resolving repository", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return repository.Repository{}, "", false
	}
	return repo, image, true
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
