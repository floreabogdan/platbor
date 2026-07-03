package rubygems

import (
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/repository"
)

// versions serves the compact-index /versions file: a header, then one line per
// gem listing its versions and the md5 of its /info file. Yanked versions are
// excluded (consistent with /info). For a proxy the upstream file is served
// fresh.
func (h *handler) versions(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r, false)
	if !ok {
		return
	}
	if repo.Mode == repository.ModeProxy {
		h.proxyText(w, r, repo, "/versions")
		return
	}
	rows, err := h.store.versionsForIndex(r.Context(), repo.ID)
	if err != nil {
		h.internalError(w, "listing versions", err)
		return
	}

	// Group by gem in the order returned (already name-sorted, oldest-first).
	order := []string{}
	byGem := map[string][]infoLine{}
	for _, v := range rows {
		if _, seen := byGem[v.Name]; !seen {
			order = append(order, v.Name)
		}
		byGem[v.Name] = append(byGem[v.Name], infoLine{Number: v.Number, Deps: v.Deps, Reqs: v.Reqs})
	}

	var b strings.Builder
	b.WriteString("created_at: 1990-01-01T00:00:00Z\n---\n")
	for _, name := range order {
		lines := byGem[name]
		nums := make([]string, 0, len(lines))
		for _, l := range lines {
			nums = append(nums, l.Number)
		}
		info := buildInfoFile(lines)
		sum := md5.Sum([]byte(info))
		b.WriteString(name + " " + strings.Join(nums, ",") + " " + hex.EncodeToString(sum[:]) + "\n")
	}
	writeText(w, b.String())
}

// info serves the compact-index /info/<gem> file: one line per non-yanked
// version. For a proxy the upstream file is served fresh (and versions recorded).
func (h *handler) info(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r, false)
	if !ok {
		return
	}
	name := chi.URLParam(r, "gem")
	if name == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if repo.Mode == repository.ModeProxy {
		h.proxyInfo(w, r, repo, name)
		return
	}
	rows, err := h.store.infoVersions(r.Context(), repo.ID, name)
	if err != nil {
		h.internalError(w, "listing info", err)
		return
	}
	lines := make([]infoLine, 0, len(rows))
	for _, v := range rows {
		if v.Yanked {
			continue
		}
		lines = append(lines, infoLine{Number: v.Number, Deps: v.Deps, Reqs: v.Reqs})
	}
	if len(lines) == 0 {
		writeError(w, http.StatusNotFound, "gem not found")
		return
	}
	writeText(w, buildInfoFile(lines))
}

// names serves the compact-index /names file: every gem name, one per line.
func (h *handler) names(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r, false)
	if !ok {
		return
	}
	if repo.Mode == repository.ModeProxy {
		h.proxyText(w, r, repo, "/names")
		return
	}
	names, err := h.store.names(r.Context(), repo.ID)
	if err != nil {
		h.internalError(w, "listing names", err)
		return
	}
	var b strings.Builder
	b.WriteString("---\n")
	for _, n := range names {
		b.WriteString(n + "\n")
	}
	writeText(w, b.String())
}

// infoLine is one version's data in an /info file.
type infoLine struct {
	Number string
	Deps   string
	Reqs   string
}

// buildInfoFile renders the exact bytes of an /info/<gem> file: a "---" header
// then "<number> <deps>|<reqs>" per version. The md5 of this output is what
// /versions advertises, so both paths must build it identically.
func buildInfoFile(lines []infoLine) string {
	var b strings.Builder
	b.WriteString("---\n")
	for _, l := range lines {
		b.WriteString(l.Number + " " + l.Deps + "|" + l.Reqs + "\n")
	}
	return b.String()
}

func writeText(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

// splitComma splits a comma-separated list, trimming spaces and dropping empties.
func splitComma(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
