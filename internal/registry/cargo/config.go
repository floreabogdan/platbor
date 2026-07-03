package cargo

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// config serves config.json, the sparse registry's root document. dl is our
// download endpoint and api is our base; auth-required makes cargo send the token
// on every request (index and download included), which Platbor's per-project
// RBAC requires.
//
// config.json is static (derived from the URL) and cargo fetches it before the
// first publish, so it deliberately does not require the repository to exist yet
// — only that the project exists and the caller may read it. The repository is
// auto-created on the first publish (when the project allows it).
func (h *handler) config(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	projectID, _, err := h.store.resolveProject(r.Context(), project)
	if err != nil {
		if errors.Is(err, errProjectNotFound) {
			writeError(w, http.StatusNotFound, "project not found: "+project)
			return
		}
		h.internalError(w, "resolving project", err)
		return
	}
	if !h.authorize(w, r, projectID, false) {
		return
	}
	base := baseURL(r, chi.URLParam(r, "project"), chi.URLParam(r, "repo"))
	doc := map[string]any{
		"dl":            base + "/api/v1/crates",
		"api":           base,
		"auth-required": true,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(doc)
}

// --- index entry construction (local publish) ---

// publishMeta is the JSON metadata part of a `cargo publish` request body.
type publishMeta struct {
	Name     string              `json:"name"`
	Vers     string              `json:"vers"`
	Deps     []publishDep        `json:"deps"`
	Features map[string][]string `json:"features"`
	Links    *string             `json:"links"`
}

type publishDep struct {
	Name               string   `json:"name"`
	VersionReq         string   `json:"version_req"`
	Features           []string `json:"features"`
	Optional           bool     `json:"optional"`
	DefaultFeatures    bool     `json:"default_features"`
	Target             *string  `json:"target"`
	Kind               string   `json:"kind"`
	Registry           *string  `json:"registry"`
	ExplicitNameInToml string   `json:"explicit_name_in_toml"`
}

// indexEntry is one line of the sparse index (the JSON cargo reads to resolve the
// dependency graph).
type indexEntry struct {
	Name     string              `json:"name"`
	Vers     string              `json:"vers"`
	Deps     []indexDep          `json:"deps"`
	Cksum    string              `json:"cksum"`
	Features map[string][]string `json:"features"`
	Yanked   bool                `json:"yanked"`
	Links    *string             `json:"links,omitempty"`
}

type indexDep struct {
	Name            string   `json:"name"`
	Req             string   `json:"req"`
	Features        []string `json:"features"`
	Optional        bool     `json:"optional"`
	DefaultFeatures bool     `json:"default_features"`
	Target          *string  `json:"target"`
	Kind            string   `json:"kind"`
	Registry        *string  `json:"registry,omitempty"`
	Package         *string  `json:"package,omitempty"`
}

// buildIndexLine turns publish metadata plus the .crate checksum into the single
// JSON index line cargo consumes.
func buildIndexLine(meta publishMeta, cksum string) (string, error) {
	entry := indexEntry{
		Name:     meta.Name,
		Vers:     meta.Vers,
		Deps:     make([]indexDep, 0, len(meta.Deps)),
		Cksum:    cksum,
		Features: meta.Features,
		Yanked:   false,
		Links:    meta.Links,
	}
	if entry.Features == nil {
		entry.Features = map[string][]string{}
	}
	for _, d := range meta.Deps {
		id := indexDep{
			Name:            d.Name,
			Req:             d.VersionReq,
			Features:        d.Features,
			Optional:        d.Optional,
			DefaultFeatures: d.DefaultFeatures,
			Target:          d.Target,
			Kind:            d.Kind,
			Registry:        d.Registry,
		}
		if id.Features == nil {
			id.Features = []string{}
		}
		// When the dependency is renamed in Cargo.toml, the index carries the
		// rename as `name` and the real crate as `package`.
		if d.ExplicitNameInToml != "" {
			pkg := d.Name
			id.Name = d.ExplicitNameInToml
			id.Package = &pkg
		}
		entry.Deps = append(entry.Deps, id)
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// setLineYanked rewrites the "yanked" field of a stored index line, used when a
// version is yanked/unyanked so the served index reflects it.
func setLineYanked(line string, yanked bool) string {
	var entry map[string]any
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return line
	}
	entry["yanked"] = yanked
	if b, err := json.Marshal(entry); err == nil {
		return string(b)
	}
	return line
}
