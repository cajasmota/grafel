# React convention

Required reading: `_graph-searchability.md`.

Applies to React web apps regardless of build tool. If the repo uses Vite specifically, also read `vite.md`. If it is React Native, use `react-native.md` instead — this file does not apply.

## Public surface

For a React app, "public" means user-reachable, not exported-from-package. Order:

1. **Routes** — every entry in the router (`react-router`, `tanstack-router`, Next.js `app/`/`pages/`, etc.). Each route is a public surface element.
2. **Pages / top-level views** — what each route renders.
3. **Server actions / API client functions** — where the client talks to the backend.
4. **Public components** — components exported from a barrel `index.ts` that other repos consume. Most apps have none; component libraries have many.
5. **Context providers** — runtime singletons that affect the whole tree (`AuthProvider`, `QueryClientProvider`, etc.).

Internal helpers (hooks under `hooks/_internal/`, anything with `// @internal` JSDoc, anything in `__tests__/`) are not public surface.

## Module shape

Modules in a React app are usually feature folders, not framework folders. Typical layout:

```
src/
  features/<feature>/
    components/
    hooks/
    api/
    types.ts
    index.ts        # barrel
  shared/
    components/
    hooks/
    api-client.ts
  routes/           # or app/ for Next.js
```

An `grafel_orient (view=clusters)` cluster usually maps to one feature folder. When two features share a hook in `shared/hooks/`, that hook shows up as central in both communities — describe it in `shared`'s module page, not in either feature.

## Entry points (Pass 3)

- The framework root: `main.tsx`, `index.tsx`, or Next's `app/layout.tsx`.
- The router root.
- The top-level provider stack (often `App.tsx`).
- The build config (`vite.config.ts`, `next.config.js`) — entry from the bundler's perspective.

## Dynamic edges (Pass 4)

- **Context consumption** — a component using `useContext(SomeContext)` is silently coupled to whichever provider wraps it. Encode in `flows.md`:
  ```markdown
  ## How `useAuth` connects to `AuthProvider`
  ```
- **Imperative refs / portals** — `createPortal` and `forwardRef` create runtime parent/child links not visible in JSX nesting.
- **Lazy routes** — `React.lazy(() => import('./X'))` is a dynamic import; the static graph sees the import edge but Vite/webpack splits it at runtime. Mention this in `reference/deployment.md`.
- **Feature flags** — `if (flags.foo)` branches are runtime forks. List each flag in `cross-cutting/observability.md`.
- **Client-side cache** — TanStack Query, SWR, or RTK Query queries are keyed by string; the same key in two places couples them. Encode key→consumer in `flows.md`.

## Deployment signals (Pass 5)

Check, in order:

1. Static-host config: `netlify.toml`, `vercel.json`, `_redirects`.
2. CDN/edge: CloudFront distributions referenced in IaC, Vercel/Netlify env config.
3. CI: build command, output directory, env-var injection.
4. SSR boundary if Next.js: which routes are SSR vs SSG vs ISR.

## Manifest files (Pass 5 — `dependencies.md`)

`package.json` is the truth; `pnpm-lock.yaml` / `package-lock.json` / `yarn.lock` give exact pins. Distinguish `dependencies` from `devDependencies` from `peerDependencies` in the rendered table.

## Cross-cutting pitfalls

- **Hydration mismatches** — server-rendered HTML differs from first client render. Always document where SSR is used; note pitfalls in `cross-cutting/errors.md`.
- **Environment variables** — only `VITE_*` (Vite) or `NEXT_PUBLIC_*` (Next.js) prefixes are exposed to the client bundle. Shipping a non-prefixed var is a silent failure.
- **CORS / cookies** — auth state lives in cookies the client cannot inspect; document the auth contract in `cross-cutting/auth.md` even if the client just passes through.

## Cross-repo signals

A React repo connects out via:

- HTTP to a backend (the most common; document the backend repo's slug in `overview.md`).
- WebSocket / SSE — note both ends.
- Shared component package — if the repo consumes a sibling component library, that's a static import edge.

For HTTP edges, the path is the most reliable join key. When `grafel_cross_links(action=list)` proposes `<this repo>.api/foo` → `<backend repo>.routes.foo`, accept if the path matches; the user's intent in question 10 of Pass 0 should already have predicted these. Note that `FETCHES` edges (HTTP consumer → endpoint) are now emitted by the indexer for fetch/axios calls — they appear in `grafel_subgraph` results and should be cited as first-class connections in module docs.
