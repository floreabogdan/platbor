# Design System

Modern SaaS aesthetic: warm paper canvas, dark ink sidebar, white cards with soft depth, teal accent, technical mono micro-labels. These tokens are **canonical** — do not introduce colors, fonts, shadows, or radii outside this document. New patterns get added here first, then used.

## Typography

| Role | Font | Notes |
|------|------|-------|
| Sans (default) | **Manrope** | `font-feature-settings: 'cv11', 'ss01'` on `body` |
| Mono | **JetBrains Mono** | digests, versions, commands, micro-labels |

- Page title: `text-2xl font-bold tracking-tight text-slate-900`
- Page subtitle: `mt-1 text-sm text-slate-500`
- Section micro-label (sidebar groups, kickers): `font-mono text-[10px] uppercase tracking-[0.18em] text-slate-500`
- Body text: `text-slate-800`; secondary: `text-slate-500`

## Tailwind config (exact)

```js
// tailwind.config.js — theme.extend
fontFamily: {
  sans: ['Manrope', 'ui-sans-serif', 'system-ui', 'sans-serif'],
  mono: ['"JetBrains Mono"', 'ui-monospace', 'SFMono-Regular', 'monospace'],
},
colors: {
  ink: {
    900: '#0b1220',   // sidebar / darkest surface
    800: '#101a2e',
    700: '#17233c',
  },
  canvas: '#f5f4f0',  // app background (warm paper)
},
boxShadow: {
  card: '0 1px 2px rgba(15, 23, 42, 0.04), 0 8px 24px -12px rgba(15, 23, 42, 0.12)',
},
```

Everything else uses stock Tailwind `slate` (neutrals), `teal` (accent), and the status palette below.

## Base CSS (exact)

```css
body {
  @apply bg-canvas text-slate-800 antialiased;
  font-feature-settings: 'cv11', 'ss01';
}

/* Subtle paper texture / depth for the app canvas. */
.app-canvas {
  background-color: #f5f4f0;
  background-image:
    radial-gradient(at 100% 0%, rgba(13, 148, 136, 0.06) 0px, transparent 50%),
    radial-gradient(at 0% 100%, rgba(15, 23, 42, 0.04) 0px, transparent 45%);
}

/* Thin scrollbars for the technical aesthetic. */
* { scrollbar-width: thin; scrollbar-color: #cbd5e1 transparent; }
*::-webkit-scrollbar { width: 8px; height: 8px; }
*::-webkit-scrollbar-thumb { background: #cbd5e1; border-radius: 9999px; }

@keyframes rise {
  from { opacity: 0; transform: translateY(8px); }
  to   { opacity: 1; transform: translateY(0); }
}
.animate-rise { animation: rise 0.5s cubic-bezier(0.22, 1, 0.36, 1) both; }
```

## Color roles

| Role | Token |
|------|-------|
| App background | `bg-canvas` (`.app-canvas` for the main pane texture) |
| Dark surfaces (sidebar) | `bg-ink-900`; hover `bg-white/5`; active `bg-white/10 ring-1 ring-white/10` |
| Cards / raised surfaces | `bg-white border-slate-200/80 shadow-card` |
| Accent / brand | `teal` — logo tile `bg-gradient-to-br from-teal-400 to-teal-600` with `shadow-lg shadow-teal-500/20`; primary actions teal-600/700 |
| Text on dark | primary `text-white`, secondary `text-slate-400`, muted `text-slate-500` |

### Status palette

Pattern for pills/badges: `bg-{c}-50 text-{c}-700 ring-1 ring-inset ring-{c}-600/20` + a 1.5px dot `bg-{c}-500`.

| Status | Color | Dot animation |
|--------|-------|---------------|
| success / completed / online | `emerald` | — |
| warning / partial / pending | `amber` | pending pulses |
| running / info | `sky` | pulse |
| failed / critical | `red` | — |
| queued / offline / neutral | `slate` (`bg-slate-100 text-slate-600`) | — |

Entity-type badges follow the same recipe with a per-type hue (e.g. emerald / indigo / amber) and a matching chip variant `bg-{c}-500/10 text-{c}-600`.

## Component recipes

**Card**
```tsx
<div className="rounded-2xl border border-slate-200/80 bg-white shadow-card">
```
Radius language: cards `rounded-2xl`, controls/nav items `rounded-lg`, small icon buttons `rounded-md`, pills/avatars `rounded-full`.

**StatusPill**
```tsx
<span className="inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ring-1 ring-inset {pill}">
  <span className="h-1.5 w-1.5 rounded-full {dot}" />
  {label}
</span>
```

**PageHeader** — `mb-6 flex items-end justify-between gap-4`; title + optional subtitle left, actions right.

**Modal** — rendered via portal to `document.body` (never inside transformed ancestors): overlay `fixed inset-0 z-50 bg-ink-900/50 backdrop-blur-sm p-6`, content in a Card, backdrop click and Escape close.

**App shell**
- Sidebar: `w-64 bg-ink-900 text-slate-300`, flex column — logo block, grouped nav, user block pinned bottom (`border-t border-white/5`).
- Nav item: `flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition-all`; active `bg-white/10 text-white shadow-sm ring-1 ring-white/10`, idle `text-slate-400 hover:bg-white/5 hover:text-white`. Group labels use the mono micro-label style.
- Main pane: `.app-canvas flex-1 overflow-y-auto`, content wrapper `px-8 py-7`.
- Icons: 18px stroke icons (`h-[18px] w-[18px]`, strokeWidth ≈ 2.2), inline SVG components — no icon-font dependency.

**Motion** — entrance only: `animate-rise` on page content/cards. No decorative animation beyond status-dot pulses; `transition-all` on interactive elements.

## Registry-specific conventions

- Digests, tags, versions, and install commands render in `font-mono text-xs` on `bg-slate-100 rounded-md px-1.5 py-0.5` (inline) or inside a copyable command block on `bg-ink-900 text-slate-200 rounded-lg` with a copy button.
- Format identity (OCI / npm / NuGet / generic) uses the entity-type badge recipe — assign each format a fixed hue and keep it consistent everywhere (list rows, detail pages, search results).
- Empty states: centered in a Card, icon + one sentence + primary action; never a bare "no data" string.
