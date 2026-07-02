package oci

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/platbor/platbor/internal/core/db"
)

// Browser is the read side of the OCI registry: it answers the UI's browse
// queries (repositories, tags, manifest detail) over the same project-scoped
// tables the protocol writes to. It never mutates.
type Browser struct {
	q *db.Queries
}

// NewBrowser wires a read-only registry browser to the metadata store.
func NewBrowser(sqlDB *sql.DB) *Browser { return &Browser{q: db.New(sqlDB)} }

// Kinds a manifest can be, as shown in the UI.
const (
	KindImage = "image"
	KindIndex = "index"
)

// RepositorySummary is one repository in the browser's project-grouped index.
type RepositorySummary struct {
	ProjectKey    string
	ProjectName   string
	Repository    string
	TagCount      int
	ManifestCount int
	UpdatedAt     time.Time
}

// TagSummary is a tag joined to the manifest it points at, for the tag table.
type TagSummary struct {
	Tag       string
	Digest    string
	MediaType string
	Kind      string // KindImage or KindIndex
	Size      int64  // total layer size for an image; 0 for an index
	Count     int    // layer count for an image; platform count for an index
	PushedAt  time.Time
}

// LayerRef is a config or layer blob referenced by an image manifest.
type LayerRef struct {
	MediaType string
	Digest    string
	Size      int64
}

// IndexEntry is a child manifest referenced by an image index (multi-arch).
type IndexEntry struct {
	MediaType string
	Digest    string
	Size      int64
	Platform  string // "os/arch", empty if the index omits it
}

// ManifestView is the full detail of a single manifest.
type ManifestView struct {
	Digest    string
	MediaType string
	Kind      string
	TotalSize int64
	Config    *LayerRef    // nil for an index
	Layers    []LayerRef   // image layers, newest concept last (as authored)
	Manifests []IndexEntry // index children
}

// Repositories returns every repository across all projects, ordered by project
// then repository, for the browser's grouped index.
func (b *Browser) Repositories(ctx context.Context) ([]RepositorySummary, error) {
	rows, err := b.q.ListAllRepositories(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing repositories: %w", err)
	}
	out := make([]RepositorySummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, RepositorySummary{
			ProjectKey:    r.ProjectKey,
			ProjectName:   r.ProjectName,
			Repository:    r.Repository,
			TagCount:      int(r.TagCount),
			ManifestCount: int(r.ManifestCount),
			UpdatedAt:     parseTime(asString(r.UpdatedAt)),
		})
	}
	return out, nil
}

// Tags returns a repository's tags with the media type and size of the manifest
// each points at, newest push first.
func (b *Browser) Tags(ctx context.Context, projectID, repo string) ([]TagSummary, error) {
	rows, err := b.q.ListTagsWithManifest(ctx, db.ListTagsWithManifestParams{ProjectID: projectID, Repository: repo})
	if err != nil {
		return nil, fmt.Errorf("listing tags: %w", err)
	}
	out := make([]TagSummary, 0, len(rows))
	for _, r := range rows {
		kind, size, count := summarize(r.MediaType, r.Payload)
		out = append(out, TagSummary{
			Tag:       r.Tag,
			Digest:    r.Digest,
			MediaType: r.MediaType,
			Kind:      kind,
			Size:      size,
			Count:     count,
			PushedAt:  parseTime(r.UpdatedAt),
		})
	}
	return out, nil
}

// Manifest returns the detail of a manifest referenced by tag or digest.
func (b *Browser) Manifest(ctx context.Context, projectID, repo, ref string) (ManifestView, error) {
	digest := ref
	if !isDigestRef(ref) {
		row, err := b.q.GetTag(ctx, db.GetTagParams{ProjectID: projectID, Repository: repo, Tag: ref})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ManifestView{}, ErrManifestNotFound
			}
			return ManifestView{}, fmt.Errorf("resolving tag: %w", err)
		}
		digest = row.Digest
	}

	m, err := b.q.GetManifest(ctx, db.GetManifestParams{ProjectID: projectID, Repository: repo, Digest: digest})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ManifestView{}, ErrManifestNotFound
		}
		return ManifestView{}, fmt.Errorf("getting manifest: %w", err)
	}

	return buildManifestView(m.Digest, m.MediaType, m.Payload), nil
}

// buildManifestView parses a stored manifest payload into the detail view.
func buildManifestView(digest, mediaType string, payload []byte) ManifestView {
	var doc manifestDoc
	_ = json.Unmarshal(payload, &doc) // best effort; a stored manifest is well-formed

	view := ManifestView{Digest: digest, MediaType: mediaType, Kind: KindImage}
	if imageIndexTypes[mediaType] {
		view.Kind = KindIndex
		for _, d := range doc.Manifests {
			view.Manifests = append(view.Manifests, IndexEntry{
				MediaType: d.MediaType,
				Digest:    d.Digest,
				Size:      d.Size,
				Platform:  d.platformString(),
			})
		}
		return view
	}

	if doc.Config != nil {
		view.Config = &LayerRef{MediaType: doc.Config.MediaType, Digest: doc.Config.Digest, Size: doc.Config.Size}
		view.TotalSize += doc.Config.Size
	}
	for _, l := range doc.Layers {
		view.Layers = append(view.Layers, LayerRef{MediaType: l.MediaType, Digest: l.Digest, Size: l.Size})
		view.TotalSize += l.Size
	}
	return view
}

// summarize derives the kind, total size, and layer/platform count for a tag row
// without building the full view.
func summarize(mediaType string, payload []byte) (kind string, size int64, count int) {
	var doc manifestDoc
	_ = json.Unmarshal(payload, &doc)
	if imageIndexTypes[mediaType] {
		return KindIndex, 0, len(doc.Manifests)
	}
	if doc.Config != nil {
		size += doc.Config.Size
	}
	for _, l := range doc.Layers {
		size += l.Size
	}
	return KindImage, size, len(doc.Layers)
}

// parseTime parses an RFC 3339 timestamp, returning the zero time on failure so
// a stray value never breaks a listing.
func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// asString coerces a scanned value (sqlc types SQLite MAX() as interface{}) to a
// string.
func asString(v interface{}) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	default:
		return ""
	}
}
