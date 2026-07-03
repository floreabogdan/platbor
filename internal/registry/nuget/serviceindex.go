package nuget

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// serviceIndex advertises the feed's V3 resources. A client fetches this first
// to discover where to publish, restore, read metadata, and search.
func (h *handler) serviceIndex(w http.ResponseWriter, r *http.Request) {
	base := baseURL(r, chi.URLParam(r, "project"), chi.URLParam(r, "repo"))

	index := map[string]any{
		"version": "3.0.0",
		"resources": []map[string]string{
			{"@id": base + "/v3/package", "@type": "PackagePublish/2.0.0"},
			{"@id": base + "/v3-flatcontainer/", "@type": "PackageBaseAddress/3.0.0"},
			{"@id": base + "/v3/registrations/", "@type": "RegistrationsBaseUrl/3.6.0"},
			{"@id": base + "/v3/registrations/", "@type": "RegistrationsBaseUrl/Versioned"},
			{"@id": base + "/v3/search", "@type": "SearchQueryService/3.5.0"},
			{"@id": base + "/v3/search", "@type": "SearchQueryService"},
		},
	}
	writeJSON(w, http.StatusOK, index)
}
