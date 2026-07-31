.PHONY: build dashboard-build verify-dashboard test test-perf lint fmt vet clean fbgen fb-bench mcp-audit coverage coverage-validate

GO ?= go
NPM ?= npm
BINARY := grafel
# osusergo: force pure-Go os/user (read /etc/passwd + env) instead of the cgo
# path that hangs the daemon at startup under macOS launchd (OpenDirectory
# blocking open(); #5222). Harmless on Linux/Windows; coexists with CGO_ENABLED=1
# (tree-sitter). Applied to every build so released binaries can't regress.
TAGS := osusergo
LDFLAGS := -s -w \
  -X github.com/cajasmota/grafel/internal/version.Version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev) \
  -X github.com/cajasmota/grafel/internal/version.Commit=$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown) \
  -X github.com/cajasmota/grafel/internal/version.Date=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# dashboard-build: compile the webui-v2 React SPA and copy the output into
# internal/dashboard/dist/ so the Go embed directive picks it up. webui-v2 is
# the one and only dashboard — it is served EMBEDDED by the daemon at
# http://127.0.0.1:47274, so `grafel install` ships the full UI with no
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
	$(GO) build -tags '$(TAGS)' -ldflags='$(LDFLAGS)' -o $(BINARY) ./cmd/grafel

# build-go-only: skip the npm step (for CI environments that have already
# pre-built the SPA or for fast Go-only iteration when the SPA is unchanged).
build-go-only:
	$(GO) build -tags '$(TAGS)' -ldflags='$(LDFLAGS)' -o $(BINARY) ./cmd/grafel

test:
	$(GO) test -race -count=1 ./...

# test-perf runs the performance/scaling tests that are excluded from the
# default suite (and therefore from the release gate) by the `perf` build tag.
# See CONTRIBUTING.md, "Performance tests". Run on a QUIET machine — every
# assertion behind this tag is a wall-clock or heap measurement, and a
# contended host produces meaningless numbers in both directions.
#
# No -race: the detector's instrumentation invalidates the timings.
test-perf:
	$(GO) test -tags perf -count=1 -timeout 30m ./...

lint: lint-localematch
	$(GO) vet ./...

# lint-localematch guards against the locale-bug class (#5317): branching on a
# localized OS command output / error string (e.g. strings.Contains(out,
# "cannot find")) breaks on non-English locales. Use exit codes / typed errors /
# structured output instead. Justified race fallbacks use // nolint:localematch.
.PHONY: lint-localematch
lint-localematch:
	$(GO) run ./cmd/lint-localematch

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

clean:
	rm -f $(BINARY)

# Regenerate the Go bindings for the v2 binary graph schema
# (internal/graph/schema/graph.fbs → internal/graph/fbgraph/*.go).
# Requires `flatc` (brew install flatbuffers). The generated package
# name is rewritten from `grafel` → `fbgraph` to avoid clashing
# with the module name. See ADR-0016.
fbgen:
	@command -v flatc >/dev/null || { echo "flatc not found: brew install flatbuffers"; exit 1; }
	rm -f internal/graph/fbgraph/*.go
	flatc --go -o internal/graph/fbgraph internal/graph/schema/graph.fbs
	@if [ -d internal/graph/fbgraph/grafel ]; then \
	  mv internal/graph/fbgraph/grafel/*.go internal/graph/fbgraph/ && \
	  rmdir internal/graph/fbgraph/grafel; \
	fi
	@sed -i.bak 's/^package grafel$$/package fbgraph/' internal/graph/fbgraph/*.go && \
	  rm -f internal/graph/fbgraph/*.bak

# mcp-audit: measure handshake token budget and validate tool descriptions.
# Exit 1 when handshake exceeds AUDIT_CEILING (default 3500) or any description
# exceeds 80 chars.  Run with -json for machine-readable output.
# Override ceiling: AUDIT_CEILING=3200 make mcp-audit
# Show delta against baseline: AUDIT_BASELINE=3326 make mcp-audit
mcp-audit:
	$(GO) run ./cmd/mcp-audit

# Run the ADR-0016 microbenchmarks against the configured fixture.
# Override the fixture with: GRAFEL_BENCH_FIXTURE=/path/to/graph.json
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
