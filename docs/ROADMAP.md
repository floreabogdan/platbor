# Roadmap

Each phase ships something demoable. "Done" means: works via the real client (docker/npm/nuget CLI), covered by tests, visible in the UI, documented.

## Phase 0 — Skeleton (the walking spine)

Goal: one binary that boots, authenticates, and serves the UI shell.

- [x] Repo scaffolding: Go module, `cmd/platbor`, config loading (file + env), CI (lint, test, build)
- [x] SQLite + migrations + sqlc wiring; Postgres behind the same interface (SQLite done; Postgres seam present, driver not yet implemented)
- [x] Blob store: `fs` driver, upload sessions, sha256 verification (content-addressable, resumable, dedup; served to a consumer in Phase 1, GC later)
- [~] Auth: local users, sessions, instance admin bootstrap, API tokens, and **project roles/RBAC** are done. Roles are per project — `reader` (pull), `maintainer` (+push), `admin` (+configure); instance admins bypass. The check lives in `core/auth` (`Authorize`) and is enforced by every format adapter (read vs write from the HTTP method) and by the project-management API; the project creator is enrolled as its admin, and members are managed via `/api/v1/projects/{project}/members` and a Members panel in the UI. The OCI bearer-token flow is also done (opt-in via `PLATBOR_OCI_BEARER`): `/v2` then challenges clients toward a `/v2/token` endpoint that mints a short-lived, stateless HMAC-signed token carrying the caller's identity, which `/v2` accepts as `Bearer` while still authorizing every request against project roles; HTTP Basic (password or PAT) remains the default and always works, so the OCI conformance suite is unaffected. Verified end-to-end against the real token handshake.
- [x] Audit log: every mutation is recorded transactionally with the authenticated actor — the audit write commits in the *same* transaction as the change it describes (projects, proxies, personal access tokens, OCI manifest/tag deletes, and GC), so the record and the change land together or not at all. This deliberately replaces the originally-planned generic HTTP middleware: a request-level interceptor only sees coarse method+path+status *after* the handler runs and cannot guarantee the record commits atomically with the change, whereas the domain services own both writes. Request-level observability (method, path, status, duration, request id) is separately covered by the slog request logger.
- [x] React app shell: login, sidebar layout, projects list (per DESIGN-SYSTEM.md) wired to the API, embedded via `go:embed`
- [x] `docker run` quickstart works end to end — a multi-stage `Dockerfile` builds the SPA and a static (CGO-off) binary onto a distroless non-root base; `docker run -p 8080:8080 -v platbor-data:/data platbor` boots a zero-config instance that answers `/healthz` and the `/v2/` registry challenge. See the README quickstart.

**Demo:** log in, create a project, create an API token.

## Phase 1 — OCI registry

Goal: a real container registry. This is the hardest protocol and the anchor feature.

- [x] OCI Distribution Spec v1.1: blob + manifest + tag + referrers API (resumable/chunked + monolithic upload, pull, existence, digest verification, manifest push/pull by tag or digest with blob-reference validation, tag listing + deletion, and the referrers API with artifactType filtering, all under `/v2`) — proven by the conformance suite below
- [x] Auth: HTTP Basic challenge on `/v2` (password or PAT) — enough for docker/helm/oras and the full conformance suite. The token-based **bearer** flow shipped with RBAC (scoped tokens only mean something once project roles exist): opt-in via `PLATBOR_OCI_BEARER`, `/v2` challenges toward a `/v2/token` endpoint that issues a short-lived stateless token carrying the caller's identity, authorized live against project roles. Basic stays the default so the conformance suite is unaffected. See the Phase 0 auth entry.
- [x] Tag listing + deletion (paginated `tags/list`, manifest/tag delete) and per-repo storage accounting (logical size = the distinct blobs + manifests a repository holds, shown in the registry browser)
- [x] Pull-through proxy repo type (Docker Hub, ghcr.io) with cache — a project can mirror an upstream OCI registry (`internal/registry/proxy` upstream client with the anonymous/basic bearer-token handshake). On a cache miss the OCI adapter fetches the manifest or blob, digest-verifies it, stores it locally, then serves it; blobs and manifests-by-digest are cached permanently, a tag is refreshed from upstream and falls back to the cached copy when offline. Proxy projects are read-only (push denied). Created via `POST /api/v1/projects` with an `upstream` block and a proxy toggle in the UI. (Upstream credentials are stored as given — encrypting them at rest is a later hardening step.)
- [x] Mark-and-sweep GC for unreferenced blobs — `blob.Sweep` (generic, with a grace window) + `oci.Collector` (marks config/layer blobs across all manifests); admin-triggered via `POST /api/v1/registry/gc` (dry-run supported) and the Settings page
- [x] UI: repo browser — repositories grouped by project, tags, manifest detail (config + layers with sizes, multi-arch index platforms), copy-paste pull commands (`/api/v1/registry` read API + `/registry` pages). **Helm charts** ride the OCI registry (Helm 3.8+ pushes them as OCI artifacts) — no new adapter; a chart is recognized by its `application/vnd.cncf.helm.config.v1+json` config type, labelled "Helm chart" in the browser, and given a `helm pull` snippet. Verified with a real `helm push`/`helm pull` byte-identical round-trip.
- [x] Conformance: the `opencontainers/distribution-spec` conformance suite runs in CI (pull, push, content-discovery, content-management) and passes — 754 checks green, sha256 **and** sha512 digests, referrers, subjects, and non-distributable layers included. Blob deletion is intentionally out of scope (GC-only). Verifying docker/podman/helm/oras against a live instance is still worthwhile but the suite is the spec's own client contract.

**Demo:** `docker push` / `docker pull` / `helm push`; pull `library/alpine` through the proxy while offline from Docker Hub.

## Phase 2 — Package formats

Goal: npm, NuGet, generic — each with local + proxy repos.

- [x] npm: publish, install, dist-tags, scoped packages, `npm login` token flow, and a pull-through proxy of registry.npmjs.org — the project is the registry (`/npm/<project>/<pkg>`), verified against the real `npm` CLI (v10) including `npm install` through the proxy against live npmjs. Tarballs share the content-addressable blob store (GC generalized to mark them across formats); the proxy fetches the packument fresh (falling back to cache when the upstream is offline) and caches immutable tarballs lazily on first pull.
- [x] NuGet: V3 service index, `dotnet nuget push`, flat-container restore, registration metadata (with dependency groups), search, and a pull-through proxy of api.nuget.org — the repo is the feed (`/nuget/<project>/<repo>/v3/index.json`). The proxy discovers the upstream's resource URLs from its service index, rewrites registration/flat-container URLs back to us (so downloads flow through the cache), inlines externally-paged registration items, and caches immutable `.nupkg` blobs on first pull. Verified against the real `dotnet` CLI (9.0): push + `dotnet add package` + `dotnet restore --no-cache` with Platbor as the only source, resolving a transitive dependency (Serilog.Sinks.Console → Serilog) through the proxy and running.
- [x] Generic: PUT/GET/HEAD/DELETE versioned files at `/generic/<project>/<path>`, with sha256/sha1/md5 checksums (served as `X-Checksum-*` headers and `<path>.sha256`-style checksum siblings). Bytes stream into the shared blob store (GC-marked across formats); auth is HTTP Basic or bearer; paths are overwrite-on-PUT and proxy projects are read-only.
- [x] Retention policies: keep-last-N and untagged cleanup via a shared `registry.Pruner` engine (per-project config; each format prunes its own artifacts — OCI tags + untagged manifests, npm and NuGet versions). Admin-triggered run (`POST /api/v1/registry/retention`, dry-run supported) mirroring GC; pruning deletes metadata and audits it, blobs reclaimed by a later GC sweep.
- [x] UI: package browser per format — one unified Registry browser lists every format (OCI repositories, npm, NuGet and PyPI packages, generic files) in a single project-grouped table with a per-row format icon and a format filter dropdown. npm and NuGet packages have detail pages (versions, and copy-paste install snippets: scope-aware `npm install`, and `dotnet add package` + source config); generic files are display-only rows. npm and NuGet detail pages render the package README/description via a dependency-free, XSS-safe Markdown component (safe subset → React elements, scheme-allowlisted links, no dangerouslySetInnerHTML).

**Demo:** `npm install` and `dotnet add package` against Platbor as the only configured registry. ✅ Both verified against the real `npm` (v10) and `dotnet` (9.0) CLIs. Pull-through proxies of registry.npmjs.org and api.nuget.org both done and verified live; generic-file UI download links done. **Phase 2 closed** — the one remaining polish (package README rendering) is tracked into Phase 3's UI work.

## Phase 3 — Catalog

Goal: the "at a glance" layer — this is where Platbor stops being "another registry."

- [ ] Catalog entities: components, teams, ownership; `platbor.yaml` descriptor
- [ ] Git providers: GitHub + Gitea/Forgejo sync (repos, READMEs, releases, languages); GitLab next
- [ ] Linker: artifact ↔ component matching via OCI annotations / package repository fields + manual override
- [ ] UI: component page (repo info, owned artifacts, versions, activity), team page, global search across everything
- [ ] Dashboard: the flagship "everything at a glance" screen

**Demo:** search a component name → see its repo, owner, latest image, latest npm package, on one page.

## Phase 4 — Trust & operations

Goal: the reasons enterprises deploy Harbor.

- [ ] Trivy scanning: scan-on-push + scheduled rescans; CVE data in the graph
- [ ] Policy gates: block pull of images with criticals (per project, opt-in)
- [ ] The killer query in the UI: CVE → artifacts → components → teams
- [ ] Cosign signature and SBOM (attestation) display via the referrers API
- [ ] Webhooks (push/scan/delete events), quotas per project
- [ ] Virtual/group repositories (one URL over local + proxy)
- [~] S3 blob driver, Postgres load testing — **S3 driver done**: an S3-compatible blob backend (AWS S3, MinIO, R2) behind the same `blob.Store` contract the fs driver passes, selected via `blob.driver: s3` (or `PLATBOR_BLOB_DRIVER=s3` + `PLATBOR_S3_*`). In-progress uploads stage locally and flush to the bucket on commit (multipart for large blobs); committed blobs live only in object storage. Verified against a real MinIO: the full contract suite passes, and a live instance round-trips generic files (incl. a 40 MiB multipart blob), an OCI `/v2` layer upload, and GC/Walk — all with nothing on local disk. This is the unlock for large artifacts (container images today, ML models next). Remaining: driver hardening (retries/backoff, server-side dedup) and Postgres load testing.

## Phase 5 — Building blocks

Goal: the Backstage-killer features, informed by real usage.

- [ ] Software templates: "new service" scaffolding that creates git repo + registry repos + catalog entry
- [ ] API catalog: OpenAPI/proto specs as versioned, browsable artifacts
- [ ] TechDocs-style rendering of docs from synced repos
- [ ] OIDC login
- [~] More formats by demand — **PyPI done** (PEP 503 simple index for `pip`, legacy upload for `twine`, pull-through proxy of pypi.org; verified against real `twine`/`pip` incl. installing `six` through the proxy). **Maven done** (plain-HTTP repository layout: `mvn deploy` PUTs the pom/jar/checksums/maven-metadata.xml verbatim, `GET` resolves them, and a pull-through proxy mirrors repo1.maven.org — immutable artifacts cached, maven-metadata.xml streamed fresh; SNAPSHOT timestamped builds and version-level metadata handled. Verified against the real `mvn` (3.9.9): release deploy + `dependency:get` from a clean repo, a `-SNAPSHOT` deploy + resolve, and pulling `commons-lang3` through the Central proxy byte-identical). **Go modules done** (the GOPROXY protocol as a pull-through mirror of proxy.golang.org: immutable per-version files — `@v/<v>.info`, `.mod`, `.zip` — cached lazily into the blob store, mutable `@v/list` and `@latest` streamed fresh; module-path case-escaping decoded for the browser. Proxy-only: Go modules come from version control, there is no upload API, so a local Go repo has nothing to serve. Verified against the real `go` (1.26): the toolchain resolved `rsc.io/quote` + its transitive deps from Platbor-served bytes and ran the program, and Platbor's proxied zip is byte-identical to proxy.golang.org's). **Cargo done** (the sparse registry protocol: config.json with auth-required, per-crate sharded index files, `cargo publish` (the length-prefixed binary body), .crate downloads, and yank/unyank; a pull-through proxy of index.crates.io discovers the upstream download host from its config.json and caches each .crate on first download. Verified against the real `cargo` (1.96): publish, then `cargo check` resolved + downloaded + compiled the crate from a clean cache, `cargo yank`/`--undo` flipped the index flag, and pulling `cfg-if` through the crates.io proxy is byte-identical to static.crates.io). **RubyGems done** (the compact-index protocol: /versions, /info/<gem>, /names, .gem downloads, and `gem push` (raw .gem POST) with yank; the gemspec is parsed out of the pushed .gem to build the compact-index info line, and the /versions md5 is kept consistent with /info. A pull-through proxy of rubygems.org proxies the index and caches each .gem on first download. Verified against the real toolchain (Ruby 3.3): `gem push`, then `bundle install` resolved + downloaded + installed a gem from Platbor and it loads at runtime, and pulling `rake` through the rubygems.org proxy is byte-identical). **Terraform modules done** — scope: **modules only** (the provider registry protocol needs GPG signing-key plumbing and is out of scope; there is no proxy mode, as the public registry resolves modules from version control rather than a cacheable archive endpoint). The module registry protocol is served: instance-global service discovery at `/.well-known/terraform.json`, `/v1/modules/<namespace>/<name>/<provider>/versions`, and `/download` (a 204 whose `X-Terraform-Get` points at our archive endpoint). The namespace maps to a Platbor project. Terraform has no standard module upload API, so a module archive is uploaded via a Platbor endpoint (`PUT /terraform/upload/<project>/<repo>/<name>/<provider>/<version>`). Every protocol endpoint is verified against the spec (discovery, versions, the 204 + `X-Terraform-Get` handshake, byte-identical archive) via the real HTTP contract and unit tests; a live `terraform init` is blocked only by the environment (terraform requires an HTTPS registry host with a dotted name and a trusted cert), not by the adapter.

**All planned formats are now implemented: OCI/Helm, npm, NuGet, PyPI, Maven, Go, Cargo, RubyGems, Terraform (modules), and generic.**

## Sequencing rationale

OCI first because it's the highest-value and hardest — if the OCI implementation is excellent, credibility follows, and Helm/ORAS/SBOMs ride on it for free. The catalog lands only in Phase 3, *after* there are artifacts worth cataloging, but its schema (`project_id` scoping, audit, ownership) is designed in Phase 0 so nothing needs retrofitting. Proxy/cache ships inside Phase 1–2 per format rather than as a later feature — retrofitting caching into adapters is exactly the trap the architecture is built to avoid.
