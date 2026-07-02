# Roadmap

Each phase ships something demoable. "Done" means: works via the real client (docker/npm/nuget CLI), covered by tests, visible in the UI, documented.

## Phase 0 — Skeleton (the walking spine)

Goal: one binary that boots, authenticates, and serves the UI shell.

- [x] Repo scaffolding: Go module, `cmd/platbor`, config loading (file + env), CI (lint, test, build)
- [x] SQLite + migrations + sqlc wiring; Postgres behind the same interface (SQLite done; Postgres seam present, driver not yet implemented)
- [x] Blob store: `fs` driver, upload sessions, sha256 verification (content-addressable, resumable, dedup; served to a consumer in Phase 1, GC later)
- [~] Auth: local users, sessions, instance admin bootstrap, and API tokens (bearer auth) done; project roles/RBAC pending
- [~] Audit log table + middleware (table + transactional writes with the authenticated actor done; generic middleware pending)
- [x] React app shell: login, sidebar layout, projects list (per DESIGN-SYSTEM.md) wired to the API, embedded via `go:embed`
- [ ] `docker run` quickstart works end to end

**Demo:** log in, create a project, create an API token.

## Phase 1 — OCI registry

Goal: a real container registry. This is the hardest protocol and the anchor feature.

- [~] OCI Distribution Spec v1.1: blob API done (resumable/chunked + monolithic upload, pull, existence, digest verification under `/v2`); manifests, tags, and referrers next
- [~] Auth: HTTP Basic challenge on `/v2` done (password or PAT); bearer-token JWT flow later
- [ ] Tag listing, deletion, per-repo storage accounting
- [ ] Pull-through proxy repo type (Docker Hub, ghcr.io) with cache
- [ ] Mark-and-sweep GC job for unreferenced blobs
- [ ] UI: repo browser — tags, manifests, layer sizes, copy-paste pull commands
- [ ] Conformance: pass the `opencontainers/distribution-spec` conformance suite in CI; verify docker, podman, helm, oras clients

**Demo:** `docker push` / `docker pull` / `helm push`; pull `library/alpine` through the proxy while offline from Docker Hub.

## Phase 2 — Package formats

Goal: npm, NuGet, generic — each with local + proxy repos.

- [ ] npm: publish, install, dist-tags, scoped packages, `npm login` token flow; proxy of registry.npmjs.org
- [ ] NuGet: v3 service index, push, search, package metadata; proxy of api.nuget.org
- [ ] Generic: PUT/GET versioned files, checksums
- [ ] Retention policies: keep-last-N, untagged cleanup (shared policy engine, per-repo config)
- [ ] UI: package browser per format — versions, READMEs, install snippets

**Demo:** `npm install` and `dotnet add package` against Platbor as the only configured registry.

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
- [ ] S3 blob driver hardening, Postgres load testing

## Phase 5 — Building blocks

Goal: the Backstage-killer features, informed by real usage.

- [ ] Software templates: "new service" scaffolding that creates git repo + registry repos + catalog entry
- [ ] API catalog: OpenAPI/proto specs as versioned, browsable artifacts
- [ ] TechDocs-style rendering of docs from synced repos
- [ ] OIDC login
- [ ] More formats by demand: Maven, PyPI, Go modules, Cargo, Terraform

## Sequencing rationale

OCI first because it's the highest-value and hardest — if the OCI implementation is excellent, credibility follows, and Helm/ORAS/SBOMs ride on it for free. The catalog lands only in Phase 3, *after* there are artifacts worth cataloging, but its schema (`project_id` scoping, audit, ownership) is designed in Phase 0 so nothing needs retrofitting. Proxy/cache ships inside Phase 1–2 per format rather than as a later feature — retrofitting caching into adapters is exactly the trap the architecture is built to avoid.
