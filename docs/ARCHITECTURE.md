# Architecture

## Guiding constraints

1. **Single binary, five-minute start.** SQLite + local disk with zero config; Postgres + S3 as opt-in. No required Redis, no sidecar services. This constraint is the product — protect it in every design decision.
2. **Modular monolith.** One process, strict internal package boundaries. Format support is added behind a small adapter interface, never by touching the core.
3. **Registry is the foundation, catalog is a metadata layer.** The registry must be protocol-perfect (clients are unforgiving). The catalog is our own API/UI — we control both ends, so it can evolve fast.
4. **Integrate, don't host, for git.** Git repos are catalog entities synced from external providers.
5. **Single-org.** Projects/teams/RBAC inside one organization. No tenant isolation, billing, or per-tenant domains — but every table is scoped by `project_id` so a hosted offering remains possible later.

## System overview

```
                 ┌────────────────────────────────────────────────────┐
                 │                  platbor (one Go binary)           │
                 │                                                    │
 docker/helm ───▶│ /v2/*            ┌──────────┐   ┌──────────────┐   │
 npm ───────────▶│ /npm/*      ────▶│  format   │──▶│  core        │   │
 nuget ─────────▶│ /nuget/*         │  adapters │   │  blobstore   │──▶ fs / S3
 curl ──────────▶│ /generic/*       └──────────┘   │  metadata DB │──▶ sqlite / pg
                 │                        │         │  auth/RBAC   │   │
 browser ───────▶│ /api/v1/* ─┐           ▼         └──────────────┘   │
                 │ /  (SPA)   ├──▶ ┌──────────┐   ┌──────────────┐    │
                 │            │    │ catalog   │◀──│ sync workers │◀──── GitHub /
                 │            │    │ graph     │   │ scan (Trivy) │      Gitea /
                 │            │    └──────────┘   └──────────────┘      GitLab APIs
                 └────────────────────────────────────────────────────┘
```

## Module layout

```
platbor/
├── cmd/platbor/            # main: config load, wiring, serve
├── internal/
│   ├── core/
│   │   ├── blob/           # content-addressable store; drivers: fs, s3
│   │   ├── db/             # migrations, query layer (sqlc)
│   │   ├── auth/           # users, sessions, API tokens, robot accounts,
│   │   │                   #   OCI bearer-token issuer, RBAC checks
│   │   └── config/         # file + env config, validated at boot
│   ├── registry/
│   │   ├── adapter.go      # the format-adapter contract (see below)
│   │   ├── oci/            # OCI Distribution Spec v1.1 (push/pull/list/delete)
│   │   ├── npm/            # npm registry protocol
│   │   ├── nuget/          # NuGet v3 JSON API
│   │   ├── generic/        # raw versioned files over HTTP
│   │   └── proxy/          # pull-through cache decorator for any adapter
│   ├── catalog/
│   │   ├── model/          # entities + edges (the graph)
│   │   ├── gitprovider/    # github/, gitea/, gitlab/ metadata sync
│   │   └── linker/         # matches artifacts ↔ components (labels, provenance)
│   ├── scan/               # Trivy integration, scan queue, policy gates
│   ├── jobs/               # in-process job runner (GC, sync, scan) — no Redis
│   └── httpapi/            # /api/v1 REST for the UI, middleware, SPA serving
├── web/                    # React app (see DESIGN-SYSTEM.md); dist embedded
│   └── src/{app,components,features,lib}
└── docs/
```

Dependency rule: `httpapi → {registry, catalog, scan} → core`. Nothing imports upward; adapters never import each other.

## Key design decisions

### Blob storage: content-addressable, shared, refcounted

All binary content — image layers, package tarballs, chart archives — lives in one CAS keyed by `sha256`. Format adapters store only metadata plus digest references.

- Drivers: `fs` (default, `{data-dir}/blobs/sha256/ab/abcdef...`) and `s3` (any S3-compatible).
- Uploads are session-based and resumable (the OCI spec requires it; others benefit).
- Deletion is mark-and-sweep GC over the `blob_refs` table, run by the job runner. Never delete inline — shared blobs make inline deletion a correctness trap.

### Database: SQLite first, Postgres for scale

- SQLite via a pure-Go driver (`modernc.org/sqlite`) keeps builds CGO-free and cross-compilation trivial.
- Schema managed by versioned migrations; queries via `sqlc` (typed, no ORM magic, SQL stays reviewable).
- Every query goes through `internal/core/db`; both engines run in CI.

### Format adapter contract

Each format implements a narrow interface and mounts its own routes:

```go
type Adapter interface {
    // Key returns the format identifier: "oci", "npm", "nuget", "generic".
    Key() string
    // Mount registers the format's protocol routes on r. The adapter
    // receives repo-scoped auth and blob/metadata services via deps.
    Mount(r chi.Router, deps Deps)
}
```

`Deps` provides the blob store, a metadata service scoped to the repository, and the auth checker. The **proxy** repository type wraps any adapter: on cache miss it fetches from the configured upstream (Docker Hub, registry.npmjs.org, api.nuget.org), stores blobs + metadata, then serves locally. Virtual/group repositories (one URL aggregating local + proxy) are deferred to Phase 4 but the URL scheme reserves space for them.

### Repository URL scheme

The **project is the registry**: each format's artifacts are named directly
under a project, with no intermediate "repository" container. The artifact's own
identifier — an OCI repository name, an npm package name, a generic file path —
follows the project. (An earlier draft nested a redundant `<repo>` segment under
the project; it was dropped because the artifact already carries its own name.)

```
/v2/<project>/<name>/...          OCI (the OCI repository name, possibly slashed, under the project)
/npm/<project>/<pkg>              npm (project is the registry; packages by name, incl. @scope/name)
/nuget/<project>/v3/...           NuGet (project is the feed)
/generic/<project>/<path>         generic (files by path under the project)
/api/v1/...                       UI + automation API
```

### Auth

- **Humans:** session cookies (login form), OIDC later.
- **Machines:** per-user API tokens and project-scoped **robot accounts**. Each protocol speaks its native dialect against the same token store: OCI bearer-token flow (we issue our own JWTs), npm bearer tokens, NuGet API-key header, HTTP basic for generic.
- **RBAC:** roles per project — `admin`, `maintainer` (push), `reader` (pull) — plus instance admins. Checks live in `core/auth`, called by adapters through `Deps`; adapters never implement authorization themselves.

### Catalog data model

Entities (nodes) and relations (edges) — stored relationally, exposed as a graph:

```
Team ──owns──▶ Component ──source──▶ GitRepo (external, synced)
                  │
                  ├──builds──▶ Artifact (image/package version in the registry)
                  ├──exposes─▶ API (OpenAPI/proto spec, Phase 5)
                  └──depends-on──▶ Component
Artifact ──affected-by──▶ Vulnerability (from scans)
```

- Components are declared in a `platbor.yaml` at the repo root (Backstage-style, but minimal) *or* created in the UI; the git sync workers discover and refresh them.
- The **linker** connects registry artifacts to components via OCI annotations / package metadata (`org.opencontainers.image.source`, npm `repository` field) with manual override in the UI.
- This graph powers the headline query: *"CVE-2026-1234 → 4 images → 3 repos → 2 teams."*

### Jobs: in-process, persistent queue

GC, git sync, and scans run on a job runner backed by a DB table (visibility timeout, retries). No external queue. Long-running scans get a bounded worker pool.

### Frontend delivery

The Vite build output is embedded with `go:embed` and served by the binary; `/api/v1` and format routes take precedence, everything else falls through to the SPA. In development, Vite proxies to the Go server.

### Observability

`log/slog` structured logging, Prometheus metrics at `/metrics`, health at `/healthz` + `/readyz`. Audit log (who pushed/pulled/deleted what) is a first-class table from Phase 1 — retrofitting audit is miserable.

## Non-goals (v1)

- Git hosting, CI execution, deployment tracking
- Replication between instances
- Multi-tenancy (but keep `project_id` scoping discipline)
- Plugin system — new formats land as PRs against the adapter interface, not runtime plugins
