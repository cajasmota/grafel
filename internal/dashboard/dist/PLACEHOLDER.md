# SPA bundle placeholder

This directory holds the embedded production build of the webui-v2 React SPA.
On a fresh checkout it contains only this file so that `//go:embed dist` in
`internal/dashboard/static.go` has at least one file to embed (otherwise
`go build ./...` fails on a clean tree).

Run `make dashboard-build` from the repo root to populate the real bundle:

    cd webui-v2 && npm ci && npx vite build
    rm -rf internal/dashboard/dist
    cp -r webui-v2/dist internal/dashboard/dist

After that, this file is gone and the daemon serves the real SPA at
http://127.0.0.1:47274.

`server.go::routes()` calls `fs.Sub(staticFS, "dist")` and silently no-ops
if the bundle is missing, so a build without `make dashboard-build` still
runs — it just has no static UI to serve (API still works).
