# archigraph WebUI v2 — Architecture

This is the **clean-room rebuild** of the archigraph dashboard. It lives
beside the legacy `dashboard/` app and **never imports from it** — both UIs
stay alive until v2 reaches parity.

Stack (matches the legacy app's versions, per `docs/stack-guide.md`):
React 18 + TypeScript 5.7 + Vite 6 + Tailwind v4 + Radix + Zustand 5 +
React Router 6 + TanStack Query + sonner + lucide-react.

## The Lego layering

Strict one-directional dependency flow. A layer may only import from the
layers above it. This is the contract — keep it clean.

```
tokens.css            ← SOURCE OF TRUTH (color/type/spacing/radii/motion)
   │  (bridged into Tailwind via @theme in src/styles/app.css)
   ▼
primitives            src/components/ui/*      Button, Input/SearchInput, Badge,
   │                                           Card, Pill, Kbd, Tooltip, Popover,
   │                                           Dialog/Drawer, Tabs
   ▼
composed components   src/components/chrome/*  NavRail, TopBar (built from primitives)
   │                  src/components/showcase/*
   ▼
screen layouts        src/layouts/*            AppShell (chrome + <Outlet/>)
   │                  src/routes/*             one file per screen
   ▼
data hooks            src/hooks/*              useThemeSync, (screen data hooks)
   │                  src/store/*              Zustand global UI state
   ▼
typed API client      src/lib/api.ts           the ONLY place that talks to the daemon
                      src/data/types.ts        the domain model
```

### Hard rules

1. **Tokens, never hardcoded values.** Components consume tokens via
   Tailwind utilities (`bg-surface`, `text-text-2`, `rounded-md`,
   `font-mono`) or `var(--token)`. No hex colors, no magic px in component
   files. The token bridge is in `src/styles/app.css` (`@theme`).
2. **Theme/palette/density are CSS-attribute switches** on `<html>`
   (`data-theme` / `data-palette` / `data-density`). Only
   `src/hooks/use-theme-sync.ts` mutates them. No flash on load — the
   persisted values are applied by an inline script in `index.html`.
3. **One network door.** Every fetch goes through `src/lib/api.ts`. Screens
   never call `fetch` directly; they use TanStack Query hooks that call
   `api.*`.
4. **Primitives are dumb and reusable.** No business logic in
   `components/ui`. They take props, render tokens, forward refs.
5. **Pair color with shape/icon/label — never color-only** (handoff rule).
   See `Badge` (always carries a label), and the cross-repo dashed-edge
   treatment when Topology/Flows land.
6. **Do not touch `dashboard/`.** No imports across the two apps.

## How to add a screen

1. Read `docs/screens/<screen>.md` (the contract) + its prototype.
2. Create `src/routes/<screen>.tsx` with a default-exported component.
   Start from `PlaceholderScreen`, then build the real surface using
   primitives from `@/components/ui` and composed chrome from the shell.
3. Register it in `src/routes/router.tsx` as a child of the `/g/:groupId`
   `AppShell` route, with `handle: { surfaceLabel: "…" }` (drives the
   breadcrumb).
4. Per-screen state: extend `src/store/use-app-store.ts` (UI state) or add
   a data hook in `src/hooks/` that calls `api.*` via TanStack Query.

## How to add a primitive / component

1. New primitive → `src/components/ui/<name>.tsx`. Style with token
   utilities only. Forward refs. Export it from `src/components/ui/index.ts`.
2. Composed component (built from 2+ primitives) → `src/components/chrome/`
   (shell) or a feature folder. It may import primitives; primitives must
   never import composed components.
3. Add it to the showcase at `src/components/showcase/primitives-showcase.tsx`
   (route `/showcase`) so the catalog stays complete and the design bar
   stays visible.

## Routes

| Path | Screen | Chrome |
|---|---|---|
| `/` | Landing (group selector) | none |
| `/showcase` | Component library catalog | none |
| `/g/:groupId/graph` | Graph (default) | AppShell |
| `/g/:groupId/flows` | Flows | AppShell |
| `/g/:groupId/topology` | Topology | AppShell |
| `/g/:groupId/paths` | Paths | AppShell |
| `/g/:groupId/docs` | Docs | AppShell |
| `/g/:groupId/settings` | Group settings | AppShell |
| `/g/:groupId/pending` | Pending | AppShell |
| `/g/:groupId/operations` | Operations | AppShell |
| `*` | NotFound / error shell | none |

## Dev

```bash
npm install
npm run dev      # http://localhost:47280  (isolated; NOT the live daemon :47274)
npm run build    # tsc -b && vite build
npm run preview  # http://localhost:47281
```
