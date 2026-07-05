# Platbor

**The single-binary internal developer platform: artifact registry + software catalog.**

Platbor sits between Harbor and Backstage. It stores your artifacts (container images, npm and NuGet packages, Helm charts, generic binaries) *and* understands what they are: which repo they were built from, which team owns them, which components use them, and which vulnerabilities they carry — everything at a glance.

> Working title. The name is the directory; rename freely before going public.

## Why

The open-source gap is real:

| Tool | Has | Missing | Pain |
|------|-----|---------|------|
| Harbor | Registry, scanning | Catalog, non-OCI formats | Multi-container deployment zoo |
| Backstage | Catalog, templates | Any storage | It's a framework — you need a TS team to run it |
| Nexus/Artifactory OSS | Many formats | Catalog, graph | Aging, shrinking free tiers |
| Gitea/Forgejo | Git + basic packages | Catalog, graph, scanning | Packages are an afterthought |

Platbor's pitch: `docker run platbor` and in five minutes you have a working registry with a catalog UI. One Go binary, SQLite + local disk by default, Postgres + S3 when you grow.

## What it is (and is not)

**Is:**
- An OCI-compliant container registry (images, Helm charts, arbitrary OCI artifacts) with pull-through proxy/cache of upstream registries
- A package registry: npm, NuGet, generic — more formats over time
- A software catalog: components, owners, APIs, linked to external git repos (GitHub, Gitea/Forgejo, GitLab) and to the artifacts they build
- Vulnerability scanning via embedded Trivy
- Single-org with projects, teams, and RBAC

**Is not:**
- A git host — we integrate with your git provider, we don't replace it
- A CI system — we receive artifacts and webhooks from yours
- A SaaS — self-hosted, open source (Apache 2.0), no phone-home

## Quickstart

One container, no dependencies. Build the image and run it — SQLite and the blob
store live in a mounted volume:

```sh
docker build -t platbor .        # or: make image
docker run --rm -p 8080:8080 -v platbor-data:/data platbor
```

Then open http://localhost:8080. On first run Platbor creates the instance admin
and prints a generated password to the logs — grab it, or set your own:

```sh
docker run --rm -p 8080:8080 -v platbor-data:/data \
  -e PLATBOR_ADMIN_PASSWORD=change-me platbor
```

Point Docker at it and push an image:

```sh
docker login localhost:8080 -u admin          # password from the logs
docker tag alpine:latest localhost:8080/library/alpine:latest
docker push localhost:8080/library/alpine:latest
```

Runtime configuration is via `PLATBOR_*` environment variables (`PLATBOR_ADDR`,
`PLATBOR_DATA_DIR`, `PLATBOR_ADMIN_PASSWORD`, …) or a mounted YAML config — see
[docs/CONFIGURATION.md](docs/CONFIGURATION.md) for the full reference and a
[sample config](configs/platbor.example.yaml). Without Docker, `make build`
produces the same self-contained binary.

> A published image (`docker run ghcr.io/platbor/platbor`) lands with the first
> tagged release; until then, build from source as above.

## Documentation

| Doc | Contents |
|-----|----------|
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | System design, data model, module layout, key decisions |
| [docs/CONFIGURATION.md](docs/CONFIGURATION.md) | Every config key (YAML + env), health probes, examples |
| [docs/ROADMAP.md](docs/ROADMAP.md) | Phased delivery plan with acceptance criteria |
| [docs/CODING-STANDARDS.md](docs/CODING-STANDARDS.md) | Go, TypeScript/React, testing, git conventions |
| [docs/DESIGN-SYSTEM.md](docs/DESIGN-SYSTEM.md) | UI design tokens and component recipes |

## Stack at a glance

- **Backend:** Go (single binary), chi router, `log/slog`, SQLite (pure-Go driver) or Postgres, local filesystem or S3 blob storage
- **Frontend:** React 18 + TypeScript + Vite + Tailwind, embedded into the binary via `go:embed`
- **Scanning:** Trivy as a library
- **License:** Apache 2.0
