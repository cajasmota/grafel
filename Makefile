.PHONY: build dashboard-build verify-dashboard test lint fmt vet clean fbgen fb-bench mcp-audit coverage coverage-validate

GO ?= go
NPM ?= npm
BINARY := archigraph
LDFLAGS := -s -w \
  -X github.com/cajasmota/archigraph/internal/version.Version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev) \
  -X github.com/cajasmota/archigraph/internal/version.Commit=$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown) \
  -X github.com/cajasmota/archigraph/internal/version.Date=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# dashboard-build: compile the webui-v2 React SPA and copy the output into
# internal/dashboard/dist/ so the Go embed directive picks it up. webui-v2 is
# the one and only dashboard — it is served EMBEDDED by the daemon at
# http://127.0.0.1:47274, so `archigraph install` ships the full UI with no
# separate dev server. (Run `cd webui-v2 && npm run dev` only for dev iteration.)
# Must run before `go build` or the embed will fail (no dist/ dir).
# We use `vite build` directly instead of `npm run build` (which calls
# `tsc -b && vite build`) to skip the TypeScript emit step — Vite has
# its own transpilation pass. Type-checking is done separately via
# `npm run lint` (tsc --noEmit).
dashboard-build:
	cd webui-v2 && $(NPM) ci && npx vite build
	rm -rf internal/dashboard/dist
	cp -r webui-v2/dist internal/dashboard/dist
	@$(MAKE) verify-dashboard

# verify-dashboard (#4468): fail loudly if the embedded bundle
# (internal/dashboard/dist) is STALE relative to the freshly built SPA
# (webui-v2/dist). A `vite build && go build` that skips the `cp` above
# silently re-embeds the OLD dist — the daemon then serves a months-old UI
# while reporting the new commit. Run this in CI and post-deploy. It passes
# (with a notice) when webui-v2/dist is absent (Go-only / pre-built-CI flows).
verify-dashboard:
	$(GO) run ./cmd/verify-dashboard -root .

# build: full binary including embedded SPA. Depends on dashboard-build
# so `make build` always produces a self-contained binary. dashboard-build
# now also runs verify-dashboard, so a stale embed fails the build.
build: dashboard-build
	$(GO) build -ldflags='$(LDFLAGS)' -o $(BINARY) ./cmd/archigraph

# build-go-only: skip the npm step (for CI environments that have already
# pre-built the SPA or for fast Go-only iteration when the SPA is unchanged).
build-go-only:
	$(GO) build -ldflags='$(LDFLAGS)' -o $(BINARY) ./cmd/archigraph

test:
	$(GO) test -race -count=1 ./...

lint:
	@echo "lint: (no linter configured yet — run 'go vet ./...' for now)"

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

clean:
	rm -f $(BINARY)

# Regenerate the Go bindings for the v2 binary graph schema
# (internal/graph/schema/graph.fbs → internal/graph/fbgraph/*.go).
# Requires `flatc` (brew install flatbuffers). The generated package
# name is rewritten from `archigraph` → `fbgraph` to avoid clashing
# with the module name. See ADR-0016.
fbgen:
	@command -v flatc >/dev/null || { echo "flatc not found: brew install flatbuffers"; exit 1; }
	rm -f internal/graph/fbgraph/*.go
	flatc --go -o internal/graph/fbgraph internal/graph/schema/graph.fbs
	@if [ -d internal/graph/fbgraph/archigraph ]; then \
	  mv internal/graph/fbgraph/archigraph/*.go internal/graph/fbgraph/ && \
	  rmdir internal/graph/fbgraph/archigraph; \
	fi
	@sed -i.bak 's/^package archigraph$$/package fbgraph/' internal/graph/fbgraph/*.go && \
	  rm -f internal/graph/fbgraph/*.bak

# mcp-audit: measure handshake token budget and validate tool descriptions.
# Exit 1 when handshake exceeds AUDIT_CEILING (default 3500) or any description
# exceeds 80 chars.  Run with -json for machine-readable output.
# Override ceiling: AUDIT_CEILING=3200 make mcp-audit
# Show delta against baseline: AUDIT_BASELINE=3326 make mcp-audit
mcp-audit:
	$(GO) run ./cmd/mcp-audit

# Run the ADR-0016 microbenchmarks against the configured fixture.
# Override the fixture with: ARCHIGRAPH_BENCH_FIXTURE=/path/to/graph.json
fb-bench:
	$(GO) test ./internal/graph/ -bench=. -benchmem -run=^$$ -count=3 -benchtime=2s

# coverage: regenerate docs/coverage/*.md from docs/coverage/registry.json.
# CI runs `validate` then `gen` then asserts `git diff --exit-code
# docs/coverage/` is clean. See .github/workflows/coverage-docs.yml.
coverage:
	@$(GO) run ./tools/coverage gen

# coverage-validate: schema + cite-path-exists + duplicate-id + stale checks
# on docs/coverage/registry.json. Exit 0 with warnings only; non-zero on errors.
coverage-validate:
	@$(GO) run ./tools/coverage validate
