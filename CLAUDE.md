# Platbor — project instructions

Single-binary open-source developer platform: artifact registry (OCI, npm, NuGet, generic) + software catalog. "Between Harbor and Backstage."

Read before working:
- `docs/ARCHITECTURE.md` — module layout, adapter contract, data model, design decisions. Respect the dependency rule: `httpapi → {registry, catalog, scan} → core`.
- `docs/CODING-STANDARDS.md` — Go + TypeScript standards, testing pyramid, git conventions. Enforced, not aspirational.
- `docs/DESIGN-SYSTEM.md` — canonical UI tokens (Manrope, ink/canvas/teal palette). Never introduce colors/fonts/shadows outside it.
- `docs/ROADMAP.md` — current phase and acceptance criteria; check boxes off as work lands.

Hard constraints:
- Single binary, zero-config start (SQLite + local disk). Never add a required external service.
- New formats go behind the `Adapter` interface in `internal/registry/` — never touch `core/` for a format.
- Format protocol endpoints follow their specs exactly; verify against real clients (docker/npm/nuget CLI).
- Every DB table is scoped by `project_id`; audit log entries for all mutations.
- Conventional Commits, scoped by module (e.g. `feat(oci): ...`).
