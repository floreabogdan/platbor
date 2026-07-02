# Coding Standards

These rules are enforced in review and, wherever possible, by tooling. When a rule and readability conflict, readability wins — and the rule should be revisited.

## Principles

**KISS first.** The simplest design that solves today's problem correctly. No speculative abstraction, no config options nobody asked for, no "we might need it later" (YAGNI). Platbor's core promise is simplicity — the codebase must embody it.

**DRY, with judgment.** Extract duplication when the copies must change together (a *knowledge* duplication), not merely when lines look alike. Two format adapters with similar-looking handlers are fine; two places computing a blob path are not. Wrong abstraction is more expensive than duplication — wait for the third occurrence if unsure (rule of three).

**SOLID, pragmatically applied:**
- *Single responsibility:* a package/module has one reason to change. `registry/npm` changes when the npm protocol changes, never when storage does.
- *Open/closed:* new formats extend via the `Adapter` interface; adding npm must not touch `core/`.
- *Liskov:* every blob driver and every adapter is fully substitutable — the conformance/contract tests (see Testing) enforce this mechanically.
- *Interface segregation:* small consumer-defined interfaces. `Deps` gives an adapter exactly what it needs, not the whole world.
- *Dependency inversion:* core defines interfaces (`blob.Store`, `auth.Checker`); drivers implement them. Constructors receive dependencies explicitly — no globals, no service locators, no `init()` side effects.

**Clean code basics, both languages:**
- Names say *what*, comments say *why*. A comment explaining what the next line does is a smell; a comment recording a constraint ("OCI spec requires 202 here, not 201") is gold.
- Functions do one thing, at one level of abstraction. Prefer < 50 lines; hard-think anything longer.
- No magic values — named constants with the spec reference where applicable.
- Guard clauses over nesting; return early.
- Boy-scout rule: leave touched code slightly cleaner, but keep refactors in separate commits from behavior changes.

## Go

Baseline: [Effective Go](https://go.dev/doc/effective_go) and the [Google Go Style Guide](https://google.github.io/styleguide/go/). Where they conflict with this doc, this doc wins.

- **Formatting/linting:** `gofumpt` + `golangci-lint` (errcheck, govet, staticcheck, revive, gosec, ineffassign, misspell). CI fails on any finding; no `//nolint` without a justification comment.
- **Errors:** wrap with context — `fmt.Errorf("resolving manifest %s: %w", digest, err)`. Sentinel errors (`var ErrNotFound = errors.New(...)`) in the package that owns the concept; check with `errors.Is/As`. Never ignore an error silently; never `panic` in library code.
- **Context:** `context.Context` is the first parameter of anything that does I/O, always propagated, never stored in structs.
- **Concurrency:** every goroutine has a defined owner and shutdown path. No naked `go func()` — use the job runner or an `errgroup` with context cancellation. Channels for ownership transfer, mutexes for state; keep critical sections tiny.
- **Interfaces:** defined by the consumer, next to the consumer, as small as possible. Accept interfaces, return concrete types.
- **Package hygiene:** no `util`/`common`/`helpers` packages — name packages after what they provide. `internal/` for everything not deliberately public. No circular workarounds via interfaces-in-a-shared-package; fix the dependency direction instead.
- **Logging:** `log/slog` only, structured key-value pairs (`slog.String("repo", name)`), no `fmt.Println`. Levels: `Debug` for protocol traces, `Info` for state changes, `Warn` for degraded-but-working, `Error` for failures needing attention.
- **HTTP handlers:** thin — decode, validate, call a service, encode. Business logic lives in services, never in handlers. Every handler path sets correct status codes per the relevant spec.
- **SQL:** through sqlc only; no string-built queries, ever. Migrations are append-only and reversible.

## TypeScript / React

Baseline: strict TypeScript, function components, hooks.

- **Tooling:** `tsc --noEmit` in CI, ESLint (typescript-eslint strict + react-hooks) + Prettier. Zero warnings policy.
- **Types:** `strict: true`, `noUncheckedIndexedAccess: true`. No `any` (use `unknown` and narrow); no non-null assertions (`!`) outside tests; no `as` casts except at validated I/O boundaries. API types live in `src/lib/types.ts`, generated or hand-mirrored from the Go API — one source of truth per shape.
- **Structure:** feature folders (`src/features/<name>/`) own their pages, hooks, and components; `src/components/` holds only genuinely shared UI primitives; `src/lib/` holds the API client and utilities. A feature never imports from another feature — shared things get promoted.
- **Components:** small and presentational where possible; data fetching in hooks (`useProjects()`), not inline in JSX. Derive state, don't duplicate it — compute from props/query data instead of mirroring into `useState`. Keys are stable IDs, never array indexes.
- **State:** server state via the API-client hooks (cache, refetch), UI state via local `useState`/`useReducer`. No global state library until a concrete need proves itself (KISS).
- **Styling:** Tailwind utility classes per [DESIGN-SYSTEM.md](DESIGN-SYSTEM.md) — tokens only, no hex values or arbitrary sizes sprinkled in JSX. Repeated visual patterns get promoted to a component in `src/components/ui.tsx`, not copy-pasted class strings (DRY applies to class lists too).
- **Accessibility:** interactive elements are `<button>`/`<a>`, not clickable divs; every input has a label; modals trap focus and close on Escape.

## Testing

Test pyramid, enforced pragmatically:

1. **Unit tests** (Go: table-driven, stdlib `testing`; TS: Vitest + Testing Library) for logic with branching. Test behavior through public APIs, not implementation details. Target ~80% on `internal/`, but a meaningful test beats a coverage point.
2. **Contract tests** — the Liskov enforcers. One shared suite runs against every `blob.Store` driver (fs, s3+minio) and every DB engine (SQLite, Postgres). Format adapters get protocol-level tests using real client behavior recorded from docker/npm/nuget CLIs.
3. **Conformance/E2E** — OCI distribution-spec conformance suite in CI; Playwright for the golden UI paths (login → create project → push → browse).

Rules: tests are deterministic (no sleeps — use channels/fakes for time), independent (any order), and fast (unit suite < 30s). A bug fix lands with the test that would have caught it.

## Git & process

- **Conventional Commits** (`feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `chore:`), imperative mood, body explains *why*. Scope by module: `feat(oci): support chunked uploads`.
- Trunk-based: short-lived branches off `main`, PRs small enough to review in one sitting (< ~400 lines of diff as a soft cap). `main` is always releasable.
- CI gates on every PR: lint, typecheck, unit + contract tests, build. Conformance/E2E on `main` and release branches.
- Behavior changes and refactors in separate commits; generated code (sqlc) in its own commit.

## API conventions (`/api/v1`)

- REST-ish JSON: plural nouns (`/api/v1/projects/{key}/repos`), standard verbs, kebab-case paths, `camelCase` JSON fields.
- Errors: RFC 7807 problem+json (`type`, `title`, `status`, `detail`) — one error shape everywhere.
- Pagination: cursor-based (`?cursor=&limit=`) from day one; offset pagination is a trap at registry scale.
- Timestamps: RFC 3339 UTC. IDs: opaque strings externally, whatever is efficient internally.
- Format-protocol endpoints (`/v2`, `/npm`, ...) follow *their* specs exactly, byte-for-byte where clients are picky — spec compliance beats internal consistency there.
