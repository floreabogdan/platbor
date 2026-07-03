package nuget

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// registration serves the RegistrationsBaseUrl resource: package metadata the
// client uses to resolve versions and dependencies during restore. Platbor emits
// a single inline registration page per package (no external catalog pages).
func (h *handler) registration(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.resolveProject(w, r)
	if !ok {
		return
	}
	project := chi.URLParam(r, "project")
	rest := strings.Trim(chi.URLParam(r, "*"), "/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[1] != "index.json" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	idLower := strings.ToLower(parts[0])

	versions, err := h.store.versions(r.Context(), projectID, idLower)
	if err != nil {
		if errors.Is(err, ErrPackageNotFound) {
			writeError(w, http.StatusNotFound, "package not found")
			return
		}
		h.internalError(w, "listing versions", err)
		return
	}

	base := baseURL(r, project)
	regIndexID := base + "/v3/registrations/" + idLower + "/index.json"
	flatBase := base + "/v3-flatcontainer/" + idLower + "/"

	leaves := make([]map[string]any, 0, len(versions))
	for _, v := range versions {
		spec, _ := parseNuspec(v.Nuspec)
		content := flatBase + v.VersionLower + "/" + idLower + "." + v.VersionLower + ".nupkg"
		catalog := map[string]any{
			"@id":              regIndexID + "#" + v.Version,
			"@type":            "PackageDetails",
			"id":               spec.Metadata.ID,
			"version":          v.Version,
			"description":      spec.Metadata.Description,
			"authors":          spec.Metadata.Authors,
			"dependencyGroups": dependencyGroups(spec, base),
			"listed":           true,
			"packageContent":   content,
		}
		leaves = append(leaves, map[string]any{
			"@id":            regIndexID + "#" + v.Version,
			"@type":          "Package",
			"catalogEntry":   catalog,
			"packageContent": content,
			"registration":   regIndexID,
		})
	}

	page := map[string]any{
		"@id":   regIndexID + "#page",
		"@type": "catalog:CatalogPage",
		"count": len(leaves),
		"lower": versions[0].Version,
		"upper": versions[len(versions)-1].Version,
		"items": leaves,
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"@id":   regIndexID,
		"@type": []string{"catalog:CatalogRoot", "PackageRegistration", "catalog:Permalink"},
		"count": 1,
		"items": []map[string]any{page},
	})
}

// dependencyGroups converts a nuspec's dependencies into the registration shape
// the client expects (grouped by target framework).
func dependencyGroups(spec nuspec, base string) []map[string]any {
	toDeps := func(deps []nuspecDependency) []map[string]any {
		out := make([]map[string]any, 0, len(deps))
		for _, d := range deps {
			out = append(out, map[string]any{
				"id":           d.ID,
				"range":        d.Version,
				"registration": base + "/v3/registrations/" + strings.ToLower(d.ID) + "/index.json",
			})
		}
		return out
	}

	var groups []map[string]any
	for _, g := range spec.Metadata.Dependencies.Groups {
		group := map[string]any{}
		if g.TargetFramework != "" {
			group["targetFramework"] = g.TargetFramework
		}
		if deps := toDeps(g.Dependencies); len(deps) > 0 {
			group["dependencies"] = deps
		}
		groups = append(groups, group)
	}
	if deps := toDeps(spec.Metadata.Dependencies.Flat); len(deps) > 0 {
		groups = append(groups, map[string]any{"dependencies": deps})
	}
	return groups
}
