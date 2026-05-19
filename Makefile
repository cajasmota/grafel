.PHONY: build test lint fmt vet clean fbgen fb-bench

GO ?= go
BINARY := archigraph
LDFLAGS := -s -w \
  -X github.com/cajasmota/archigraph/internal/version.Version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev) \
  -X github.com/cajasmota/archigraph/internal/version.Commit=$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown) \
  -X github.com/cajasmota/archigraph/internal/version.Date=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)

build:
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

# Run the ADR-0016 microbenchmarks against the configured fixture.
# Override the fixture with: ARCHIGRAPH_BENCH_FIXTURE=/path/to/graph.json
fb-bench:
	$(GO) test ./internal/graph/ -bench=. -benchmem -run=^$$ -count=3 -benchtime=2s
