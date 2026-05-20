package dashboard

import "embed"

// staticFS holds the production SPA bundle. The contents of
// internal/dashboard/dist/ are populated by `make dashboard-build`
// (cd dashboard && npm ci && npm run build, then copied here).
// The embed directive is stable; only the directory contents change.
//
// For development use `archigraph dashboard serve --port 47274` and run
// `cd dashboard && npm run dev` — the Vite proxy forwards /api/* to port 47274.
//
//go:embed dist
var staticFS embed.FS
