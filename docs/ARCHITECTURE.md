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
                 │            │    │ graph     │   │ scan (OSV)   │──────▶ osv.dev
                 │            │    └──────────┘   └──────────────┘      Gitea/GitLab
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
│   │   ├── pypi/           # Python package index (PEP 503 simple + twine upload)
│   │   ├── maven/          # Maven repository (plain-HTTP layout; mvn deploy/resolve)
│   │   ├── goproxy/        # Go module proxy (GOPROXY protocol; proxy-only)
│   │   ├── cargo/          # Cargo sparse registry (publish + index + proxy)
│   │   ├── rubygems/       # RubyGems compact index (gem push + bundler + proxy)
│   │   ├── terraform/      # Terraform module registry (modules only; upload + protocol)
│   │   └── proxy/          # pull-through cache decorator for any adapter
│   ├── catalog/
│   │   ├── model/          # entities + edges (the graph)
│   │   ├── gitprovider/    # github/, gitea/, gitlab/ metadata sync
│   │   └── linker/         # matches artifacts ↔ components (labels, provenance)
│   ├── scan/               # SBOM-driven vulnerability scanning (OSV), scan store
│   ├── sign/               # cosign signature verification (key + keyless), stdlib-only
│   ├── jobs/               # in-process job runner (GC, sync, scan) — no Redis
│   └── httpapi/            # /api/v1 REST for the UI, middleware, SPA serving
├── web/                    # React app (see DESIGN-SYSTEM.md); dist embedded
│   └── src/{app,components,features,lib}
└── docs/
```

Dependency rule: `httpapi → {registry, catalog, scan, sign} → core`. Nothing imports upward; adapters never import each other.

## Key design decisions

### Blob storage: content-addressable, shared, refcounted

All binary content — image layers, package tarballs, chart archives — lives in one CAS keyed by `sha256`. Format adapters store only metadata plus digest references.

- Drivers: `fs` (default, `{data-dir}/blobs/sha256/ab/abcdef...`) and `s3` (any S3-compatible — AWS S3, MinIO, R2; keys `<prefix>/blobs/<algo>/<hex>`). Both share the same `blob.Store` contract test, so they are fully substitutable.
- Uploads are session-based and resumable (the OCI spec requires it; others benefit). Both drivers stage an in-progress upload as a local temp file under `{data-dir}/uploads` and differ only in the commit step: `fs` renames it into the CAS tree, `s3` flushes it to the bucket (multipart for large blobs). Staging is node-local, so a resumable upload must be resumed on the node that began it; committed blobs are shared. Object storage is the unlock for large artifacts — container images and, ahead, ML models.
- Deletion is mark-and-sweep GC: the collector unions every format's blob referencers, then sweeps the unreferenced remainder (with a grace window sparing freshly-uploaded blobs). Never delete inline — shared blobs make inline deletion a correctness trap. GC and retention (keep-last-N pruning) run on demand via the admin API, and optionally on a schedule (`maintenance.gcInterval` / `maintenance.retentionInterval`, both off by default; enable on a single instance in a fleet). Abandoned resumable-upload staging files are reclaimed by a background sweeper (older than 24h).

**Operational probes.** `/healthz` is liveness (the process can serve; no dependency checks, so a transient DB blip never restart-loops it). `/readyz` is readiness: it probes the metadata DB and the blob store and returns 503 with a per-check breakdown when either is unreachable, so an orchestrator stops routing to an instance that cannot serve.

**Webhooks.** A project can subscribe HTTP endpoints to its mutation events. Events are not emitted by each adapter — every mutation already writes an audit entry transactionally, so the **audit log is the event stream**, and webhooks need zero changes to any format adapter. A background dispatcher tails the audit log from a persisted cursor (seeded to the newest entry at first boot so history is not replayed), matches each entry against active webhooks (by action prefix), and POSTs a JSON payload signed with the webhook's secret (`X-Platbor-Signature: sha256=<hmac>`). Delivery is best-effort and at-most-once: a failing endpoint is logged and the cursor still advances, so one bad subscriber never stalls the stream.

**Per-project quotas.** `quota_bytes` caps a project's logical storage (0 = unlimited). Enforcement lives at the one write chokepoint every adapter shares — `repository.ResolveOrCreate` — surfaced as the validation error adapters already map to a 4xx, so no adapter changed. Usage is one UNION query over the flat-sized formats plus OCI's manifest-payload size, injected into the repository service so core need not know each format.

**Signature verification (`internal/sign`).** cosign signatures ride the referrers API. Verification is *real* — the signature bytes are checked against a public key over the exact signed payload (the "simple signing" blob), not merely reported as present — and it depends only on the Go standard library, so it adds no service. Two trust models: **keyless** signatures carry a Fulcio X.509 certificate, so the signature is verified against the certificate's key and the signer identity (SAN) and OIDC issuer are read from it; **key-based** signatures are verified against a per-project verification public key (`projects.verification_key`, a public key, set in project settings). The payload's `docker-manifest-digest` is checked against the image so a signature cannot be replayed onto another artifact. Attestations are summarized by decoding their in-toto/DSSE predicate type. The package is stdlib-only and never imports a format adapter; httpapi fetches the signature layer's payload and annotations (via the OCI browser, which now surfaces layer annotations) and hands them in. Chaining a keyless certificate to a Fulcio trust root and proving Rekor transparency-log inclusion is a further step beyond verifying the signature against the certificate.

**Vulnerability scanning (`internal/scan`).** Scanning is *SBOM-driven*, which is what lets it stay inside the single-binary promise: instead of bundling a scanner binary and a multi-gigabyte vulnerability database, a scan reads an artifact's existing SBOM referrer, extracts each component's package URL (purl), and matches those against the **OSV** database (https://osv.dev) over HTTPS. OSV is contacted only when a scan is triggered, so it is a lookup and never a required service — the instance boots and serves fully offline, and `scan.enabled: false` turns even the lookup off. The package sits beside `registry` in the dependency graph (`httpapi → {registry, catalog, scan} → core`): it depends only on core and never imports a format adapter — the caller (httpapi) fetches the SBOM components (reusing the referrers/SBOM path) and hands them in as plain structs. Severity is the CVSS v3 base score computed in-process (bucketed critical/high/medium/low), with a named-severity fallback for advisories that ship no vector. Findings are stored per artifact keyed by `(repo, image, digest)` — a rescan replaces the prior result — so the data reads both ways: an artifact's vulnerabilities (the manifest page) and a vulnerability's affected artifacts (the instance-wide Vulnerabilities page, the "CVE → artifacts" query). The reverse query is the first half of the headline "CVE → artifacts → components → teams"; the remaining hops join through the Phase 3 catalog. This is deliberately *not* container-OS package scanning (that needs image-filesystem inspection); it scans what the SBOM declares.

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

`Deps` provides the blob store, a metadata service scoped to the repository, and the auth checker. The **proxy** repository type wraps any adapter: on cache miss it fetches from the configured upstream (Docker Hub, registry.npmjs.org, api.nuget.org), stores blobs + metadata, then serves locally. A **virtual** (group) repository (`mode: virtual`, OCI) aggregates an ordered list of member repositories behind one URL: a read resolves against the members in order and returns the first hit (a proxy member fetches from its upstream on the way), while `tags/list` and the referrers API return the union across members; writes are rejected, since a virtual repository is a read-only view (you push to a member). This rides the existing single-repository read path unchanged — the virtual case only iterates members and reuses `loadManifest` / the proxy blob fetch per member — so the conformance-critical OCI logic is untouched.

### Projects, repositories, and the URL scheme

A **project** is the tenant boundary (RBAC, grouping). Inside it live **typed
repositories** — the configured containers that actually hold artifacts of one
format. A repository has a `format` (oci, npm, nuget, generic), a `mode` (local,
a pull-through `proxy` of an upstream, or — for OCI — a `virtual` aggregate of
member repositories), and its own retention policy. You can
create and configure a repository *before* pushing, or — when the project's
`allowAutoCreate` is on (the default) — a push to a new repo path auto-creates a
local repository of that format. Pushing a second format into an existing
repository is rejected; a project with `allowAutoCreate` off requires every
repository to be created explicitly.

Artifacts are addressed as `/<format>/<project>/<repo>/<artifact>`:

```
/v2/<project>/<repo>/<image>/...      OCI (image name may contain slashes)
/npm/<project>/<repo>/<pkg>           npm (incl. @scope/name)
/nuget/<project>/<repo>/v3/...        NuGet (the repo is the feed)
/generic/<project>/<repo>/<path>      generic (a "bucket" of files)
/pypi/<project>/<repo>/simple/...     PyPI (simple index; twine posts to the repo root)
/maven/<project>/<repo>/<path>        Maven (plain-HTTP layout; PUT to deploy, GET to resolve)
/go/<project>/<repo>/<module>/@v/...  Go module proxy (GOPROXY protocol; proxy-only)
/cargo/<project>/<repo>/...           Cargo sparse registry (config.json, index, publish, download)
/rubygems/<project>/<repo>/...        RubyGems compact index (versions, info, names, push, download)
/.well-known/terraform.json           Terraform service discovery (instance-global)
/terraform/v1/modules/<ns>/...        Terraform module registry (namespace = project; upload + protocol)
/api/v1/...                           UI + automation API
```

(An earlier draft made the project itself the registry with no `<repo>` segment;
it was reintroduced as a first-class, typed, configured entity once repositories
gained per-repo format/mode/retention — the redundant *empty* level became a
meaningful one.)

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
