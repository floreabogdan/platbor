package pypi

import (
	"html"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/repository"
)

// simpleRoot serves the root of the simple index (PEP 503): a list of every
// project in the repository, each linking to its own index page.
func (h *handler) simpleRoot(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r, false)
	if !ok {
		return
	}
	if repo.Mode == repository.ModeProxy {
		// The root listing of an upstream is huge and unnecessary for installs;
		// point the operator at per-project pages instead.
		writeHTML(w, "<!DOCTYPE html><html><body><!-- proxy: request /simple/<project>/ --></body></html>")
		return
	}
	names, err := h.store.packageNames(r.Context(), repo.ID)
	if err != nil {
		h.internalError(w, "listing packages", err)
		return
	}
	var b strings.Builder
	b.WriteString("<!DOCTYPE html><html><head><meta name=\"pypi:repository-version\" content=\"1.0\"></head><body>\n")
	for _, name := range names {
		b.WriteString("<a href=\"" + html.EscapeString(name) + "/\">" + html.EscapeString(name) + "</a>\n")
	}
	b.WriteString("</body></html>\n")
	writeHTML(w, b.String())
}

// simpleProject serves a project's simple index page: an anchor per distribution
// file, its download URL carrying a #sha256 fragment (which pip verifies) and a
// data-requires-python attribute.
func (h *handler) simpleProject(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r, false)
	if !ok {
		return
	}
	rest := strings.Trim(chi.URLParam(r, "*"), "/")
	if rest == "" || strings.Contains(rest, "/") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	name := normalizeName(rest)

	if repo.Mode == repository.ModeProxy {
		h.proxySimple(w, r, upstreamOf(repo), repo.ID, name)
		return
	}

	files, err := h.store.listFiles(r.Context(), repo.ID, name)
	if err != nil {
		h.internalError(w, "listing files", err)
		return
	}
	if len(files) == 0 {
		writeError(w, http.StatusNotFound, "package not found")
		return
	}
	writeHTML(w, renderSimplePage(name, files, baseURL(r, chi.URLParam(r, "project"), chi.URLParam(r, "repo"))))
}

// renderSimplePage builds the PEP 503 HTML for a project's files. base is this
// repository's absolute URL; each file resolves to <base>/files/<filename>.
func renderSimplePage(name string, files []file, base string) string {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html><html><head><meta name=\"pypi:repository-version\" content=\"1.0\">")
	b.WriteString("<title>Links for " + html.EscapeString(name) + "</title></head><body>\n")
	b.WriteString("<h1>Links for " + html.EscapeString(name) + "</h1>\n")
	for _, f := range files {
		// Distribution filenames are constrained to URL-path-safe characters
		// ([A-Za-z0-9._+-]), so no percent-encoding is needed here.
		href := base + "/files/" + f.Filename
		if f.SHA256 != "" {
			href += "#sha256=" + f.SHA256
		}
		attr := ""
		if f.RequiresPython != "" {
			attr = " data-requires-python=\"" + html.EscapeString(f.RequiresPython) + "\""
		}
		b.WriteString("<a href=\"" + html.EscapeString(href) + "\"" + attr + ">" + html.EscapeString(f.Filename) + "</a><br/>\n")
	}
	b.WriteString("</body></html>\n")
	return b.String()
}

func writeHTML(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(body))
}
