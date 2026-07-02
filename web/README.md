# Platbor web

React 18 + TypeScript + Vite + Tailwind SPA. Built output is embedded into the
Go binary via `go:embed` (see `web/embed.go`).

## Develop

```sh
npm install
npm run dev        # Vite dev server on :5173, proxies /api etc. to the Go server on :8080
```

Run the Go server (`go run ./cmd/platbor`) alongside `npm run dev`.

## Build (for embedding)

```sh
npm run build      # typecheck + Vite build → web/dist
```

Then build the binary: `go build ./cmd/platbor`. The committed
`web/dist/index.html` is a placeholder so the Go build succeeds before the SPA
is built; `npm run build` overwrites it and emits hashed assets under
`web/dist/assets/` (git-ignored). Do not commit the built `index.html` over the
placeholder.

## Quality gates

```sh
npm run typecheck  # tsc --noEmit
npm run lint       # eslint, zero warnings
npm run test       # vitest
```

Design tokens are canonical — see `../docs/DESIGN-SYSTEM.md`. Never introduce
colors, fonts, shadows, or radii outside that document.
