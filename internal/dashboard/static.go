package dashboard

import "embed"

// staticFS holds the production SPA bundle (webui-v2). The contents of
// internal/dashboard/dist/ are populated by `make dashboard-build`
// (cd webui-v2 && npm ci && npx vite build, then copied here).
// The embed directive is stable; only the directory contents change.
// The daemon serves this bundle EMBEDDED at http://127.0.0.1:47274 with
// SPA-fallback (see spaHandler in server.go) so deep links + reloads work.
//
// For dev iteration run `cd webui-v2 && npm run dev` (:47280) — the Vite
// proxy forwards /api/* to the daemon at :47274.
//
//go:embed dist
var staticFS embed.FS
