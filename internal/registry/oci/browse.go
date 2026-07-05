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
	KindChart = "chart" // a Helm chart (OCI artifact with the Helm config media type)
)

// helmConfigMediaType marks an OCI manifest as a Helm chart. Helm 3.8+ pushes
// charts as OCI artifacts with this config type, so the registry stores them like
// any image; we recognize it only to label them and show `helm pull` in the UI.
const helmConfigMediaType = "application/vnd.cncf.helm.config.v1+json"

// RepositorySummary is one OCI image in the browser's project-grouped index.
type RepositorySummary struct {
	ProjectKey    string
	ProjectName   string
	RepoKey       string // the typed repository the image lives in
	Repository    string // the OCI image name within the repo
	TagCount      int
	ManifestCount int
	SizeBytes     int64 // logical size: distinct blobs + manifests this image holds
	IsProxy       bool  // the repository is a pull-through mirror
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
	// Annotations carries the layer descriptor's annotations. Cosign stores a
	// signature (and, for keyless, the signing certificate) here, so verification
	// reads them off the signature manifest's layer.
	Annotations map[string]string
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

// Stats is a coarse count of registry contents, for the dashboard summary.
type Stats struct {
	Repositories int
	Tags         int
}

// Stats returns instance-wide repository and tag counts.
func (b *Browser) Stats(ctx context.Context) (Stats, error) {
	repos, err := b.q.CountRepositories(ctx)
	if err != nil {
		return Stats{}, fmt.Errorf("counting repositories: %w", err)
	}
	tags, err := b.q.CountTags(ctx)
	if err != nil {
		return Stats{}, fmt.Errorf("counting tags: %w", err)
	}
	return Stats{Repositories: int(repos), Tags: int(tags)}, nil
}

// Repositories returns every repository across all projects, ordered by project
// then repository, for the browser's grouped index.
func (b *Browser) Repositories(ctx context.Context) ([]RepositorySummary, error) {
	rows, err := b.q.ListAllRepositories(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing repositories: %w", err)
	}
	sizes, err := b.repositorySizes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]RepositorySummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, RepositorySummary{
			ProjectKey:    r.ProjectKey,
			ProjectName:   r.ProjectName,
			RepoKey:       r.RepoKey,
			Repository:    r.Repository,
			TagCount:      int(r.TagCount),
			ManifestCount: int(r.ManifestCount),
			SizeBytes:     sizes[repoKey(r.ProjectKey, r.RepoKey, r.Repository)],
			IsProxy:       r.IsProxy != 0,
			UpdatedAt:     parseTime(asString(r.UpdatedAt)),
		})
	}
	return out, nil
}

// repoKey joins a project key, repository key, and image into a map key. The NUL
// separator cannot appear in any of them, so distinct triples never collide.
func repoKey(projectKey, repo, image string) string {
	return projectKey + "\x00" + repo + "\x00" + image
}

// ProjectSize computes a project's OCI logical storage: the summed size of the
// distinct blobs (config + layers) and manifests each of its images holds,
// deduplicated per repository (matching the per-repo sizes the browser shows).
// It is the OCI contribution to per-project quota accounting.
func (b *Browser) ProjectSize(ctx context.Context, projectID string) (int64, error) {
	rows, err := b.q.ListManifestSizingForProject(ctx, projectID)
	if err != nil {
		return 0, fmt.Errorf("sizing project manifests: %w", err)
	}
	seen := make(map[string]map[string]struct{}) // repo -> counted digests
	var total int64
	add := func(repo, digest string, size int64) {
		set, ok := seen[repo]
		if !ok {
			set = make(map[string]struct{})
			seen[repo] = set
		}
		if _, dup := set[digest]; dup {
			return
		}
		set[digest] = struct{}{}
		total += size
	}
	for _, r := range rows {
		repo := r.RepoKey + "/" + r.Repository
		add(repo, r.Digest, r.Size) // the manifest document's own bytes
		var doc manifestDoc
		if err := json.Unmarshal(r.Payload, &doc); err != nil {
			continue
		}
		if doc.Config != nil && doc.Config.Digest != "" {
			add(repo, doc.Config.Digest, doc.Config.Size)
		}
		for _, l := range doc.Layers {
			if len(l.URLs) > 0 {
				continue // foreign layer: referenced by URL, not stored here
			}
			add(repo, l.Digest, l.Size)
		}
	}
	return total, nil
}

// repositorySizes computes each repository's logical storage: the summed size of
// the distinct blobs (config + layers) and manifests it holds. Blobs are a
// shared CAS, so a layer used by two repositories counts once per repository,
// not once globally — this is the size attributable to the repository, which is
// what "how big is this repo" means to a user, and the natural unit for future
// per-project quotas. Foreign (non-distributable) layers carry external URLs and
// are not stored, so they are excluded.
func (b *Browser) repositorySizes(ctx context.Context) (map[string]int64, error) {
	rows, err := b.q.ListManifestSizing(ctx)
	if err != nil {
		return nil, fmt.Errorf("sizing repositories: %w", err)
	}
	// Per repository, track each counted digest once (manifests and blobs share
	// the digest space) so nothing is double-counted within the repository.
	seen := make(map[string]map[string]struct{})
	sizes := make(map[string]int64)
	add := func(key, digest string, size int64) {
		set, ok := seen[key]
		if !ok {
			set = make(map[string]struct{})
			seen[key] = set
		}
		if _, dup := set[digest]; dup {
			return
		}
		set[digest] = struct{}{}
		sizes[key] += size
	}

	for _, r := range rows {
		key := repoKey(r.ProjectKey, r.RepoKey, r.Repository)
		add(key, r.Digest, r.Size) // the manifest document's own bytes

		var doc manifestDoc
		if err := json.Unmarshal(r.Payload, &doc); err != nil {
			continue // a stored manifest is well-formed; skip if somehow not
		}
		if doc.Config != nil && doc.Config.Digest != "" {
			add(key, doc.Config.Digest, doc.Config.Size)
		}
		for _, l := range doc.Layers {
			if len(l.URLs) > 0 {
				continue // foreign layer: referenced by URL, not stored here
			}
			add(key, l.Digest, l.Size)
		}
		// An index's children are themselves manifests in the same repository;
		// they appear as their own rows and are counted there, so nothing to add.
	}
	return sizes, nil
}

// Tags returns a repository's tags with the media type and size of the manifest
// each points at, newest push first.
func (b *Browser) Tags(ctx context.Context, repositoryID, image string) ([]TagSummary, error) {
	rows, err := b.q.ListTagsWithManifest(ctx, db.ListTagsWithManifestParams{RepositoryID: repositoryID, Repository: image})
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

// Referrer is a manifest that refers to a subject — a signature, SBOM, or other
// attestation attached to an image.
type Referrer struct {
	Digest       string
	MediaType    string
	Size         int64
	ArtifactType string
	Annotations  map[string]string
}

// Referrers returns the manifests whose subject is the given digest, newest
// first, for the browser's manifest detail view.
func (b *Browser) Referrers(ctx context.Context, repositoryID, image, subject string) ([]Referrer, error) {
	rows, err := b.q.ListReferrers(ctx, db.ListReferrersParams{RepositoryID: repositoryID, Repository: image, Subject: subject})
	if err != nil {
		return nil, fmt.Errorf("listing referrers: %w", err)
	}
	out := make([]Referrer, 0, len(rows))
	for _, r := range rows {
		out = append(out, Referrer{
			Digest:       r.Digest,
			MediaType:    r.MediaType,
			Size:         r.Size,
			ArtifactType: r.ArtifactType,
			Annotations:  annotationsFromPayload(r.Payload),
		})
	}
	return out, nil
}

// Manifest returns the detail of a manifest referenced by tag or digest.
func (b *Browser) Manifest(ctx context.Context, repositoryID, image, ref string) (ManifestView, error) {
	digest := ref
	if !isDigestRef(ref) {
		row, err := b.q.GetTag(ctx, db.GetTagParams{RepositoryID: repositoryID, Repository: image, Tag: ref})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ManifestView{}, ErrManifestNotFound
			}
			return ManifestView{}, fmt.Errorf("resolving tag: %w", err)
		}
		digest = row.Digest
	}

	m, err := b.q.GetManifest(ctx, db.GetManifestParams{RepositoryID: repositoryID, Repository: image, Digest: digest})
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
		if doc.Config.MediaType == helmConfigMediaType {
			view.Kind = KindChart
		}
	}
	for _, l := range doc.Layers {
		view.Layers = append(view.Layers, LayerRef{MediaType: l.MediaType, Digest: l.Digest, Size: l.Size, Annotations: l.Annotations})
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
	kind = KindImage
	if doc.Config != nil {
		size += doc.Config.Size
		if doc.Config.MediaType == helmConfigMediaType {
			kind = KindChart
		}
	}
	for _, l := range doc.Layers {
		size += l.Size
	}
	return kind, size, len(doc.Layers)
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
