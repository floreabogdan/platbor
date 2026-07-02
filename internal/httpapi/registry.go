package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/project"
	"github.com/platbor/platbor/internal/registry/oci"
)

// registryHandler serves the /api/v1/registry endpoints that back the UI
// repository browser: a browsable view of what was pushed over /v2, plus
// session-authenticated deletion.
type registryHandler struct {
	browser  *oci.Browser
	manager  *oci.Manager
	projects *project.Service
	log      *slog.Logger
}

func (h registryHandler) mount(r chi.Router) {
	// Global index across every project (grouped by project in the UI).
	r.Get("/repositories", h.listRepositories)
	// Everything below is scoped to one project.
	r.Route("/{project}", func(r chi.Router) {
		r.Get("/tags", h.listTags)               // ?repository=<repo>
		r.Get("/manifests", h.getManifest)       // ?repository=<repo>&reference=<tag|digest>
		r.Delete("/manifests", h.deleteManifest) // ?repository=<repo>&reference=<tag|digest>
		r.Get("/referrers", h.listReferrers)     // ?repository=<repo>&subject=<digest>
	})
}

// --- responses (camelCase, RFC 3339 timestamps) ---

type repositoryResponse struct {
	ProjectKey    string    `json:"projectKey"`
	ProjectName   string    `json:"projectName"`
	Repository    string    `json:"repository"`
	TagCount      int       `json:"tagCount"`
	ManifestCount int       `json:"manifestCount"`
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
			TagCount:      repo.TagCount,
			ManifestCount: repo.ManifestCount,
			UpdatedAt:     repo.UpdatedAt,
		})
	}
	writeJSON(w, h.log, http.StatusOK, listRepositoriesResponse{Repositories: items})
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
