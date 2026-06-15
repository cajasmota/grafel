# Vite convention

Required reading: `_graph-searchability.md`.

Applies to repos whose primary build tool is Vite. Layer this on top of `react.md` (or whichever framework convention applies); this file only adds Vite-specific guidance.

## Public surface (additions)

- **Vite plugins** are part of the build contract. Each plugin in `vite.config.ts` should be named in `reference/dependencies.md` and, if it transforms code, in `reference/scripts.md`.
- **Environment-variable prefixes** — only `VITE_*` reaches the client bundle. Document this rule in `reference/config.md`; mismatch is a common cause of "works on dev, blank on prod".

## Entry points

- `vite.config.ts` (or `.js`, `.mts`) — build entry.
- `index.html` — the literal HTML entry; Vite scans it for `<script type="module">`.
- The script that `index.html` loads (usually `src/main.tsx`).

## Dynamic edges

- **Glob imports** (`import.meta.glob`) — these create runtime imports the static graph cannot fully resolve. Encode the directory pattern → consumer in `flows.md`.
- **Aliases** in `vite.config.ts` `resolve.alias` — `@/foo` maps to a real path; the graph already understands this if the indexer has been pointed at the config, but documents should spell out the alias rules in `reference/config.md`.
- **Environment-mode files** — `.env`, `.env.production`, `.env.development`. List the precedence rules.

## Deployment signals

- `vite build` output directory (default `dist/`). Document the deploy target consumes that directory.
- Static-host config (`netlify.toml`, `vercel.json`, `_redirects`) — usually has SPA fallback rules that matter for the router.

## Manifest files

`package.json` only; Vite doesn't have its own manifest.

## Cross-cutting pitfalls

- **`define`** in `vite.config.ts` injects compile-time globals. They are not env vars; they are inlined. Document each global in `reference/config.md`.
- **HMR boundaries** — only modules that opt in via `import.meta.hot` survive HMR cleanly. Document any custom HMR boundaries.

## Cross-repo signals

Vite itself doesn't introduce new cross-repo edges; the framework convention layered on top owns those.
