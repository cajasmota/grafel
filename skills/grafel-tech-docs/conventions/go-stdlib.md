# Go (standard library) convention

Required reading: `_graph-searchability.md`.

Applies to Go services that use `net/http` directly (with or without a thin router like `chi` or `gorilla/mux`). For frameworks with heavier conventions, write a derived convention via the `extend-convention` skill.

## Public surface

1. **HTTP handlers** — every `http.HandleFunc` registration and every `r.HandleFunc` / `r.Get` / etc. call on a router.
2. **Exported package-level identifiers** — anything in a non-`internal/` package that starts with a capital letter.
3. **CLI entry points** — `main` packages under `cmd/<name>/`.
4. **gRPC services** — any registered `*Server` on `grpc.Server`.
5. **Background goroutines launched at startup** — workers, schedulers, watchers.

`internal/` packages are by Go's own visibility rules not public; document them under modules but do not surface them in `reference/api.md`.

## Module shape

```
cmd/<name>/main.go        # one binary per cmd subdir
internal/
  <package>/
    <name>.go
    <name>_test.go
pkg/                      # optional; library code intended for external consumption
```

Communities typically map to one `internal/<package>`. A `cmd/<name>/main.go` is its own community of one — document it as part of the module of whichever package owns most of its imports.

## Entry points (Pass 3)

- Every `cmd/*/main.go`.
- Build artifacts: `go.mod` defines the module path; the `Makefile` or `goreleaser.yaml` defines what gets built.
- Configuration: env-var parsing (`os.Getenv`, `kelseyhightower/envconfig`, `caarlos0/env`).

## Dynamic edges (Pass 4)

- **Middleware chains** — a router wraps handlers in middleware in declaration order. Document the chain.
- **Goroutines + channels** — a goroutine producing into a channel and another consuming is a runtime pipeline. Encode the pipeline in `flows.md` with both ends in backticks.
- **Context-passed values** — `ctx.Value(myKey)` is a runtime contract; the producer and consumer are in different files with no static link. Document each context-key.
- **Interface satisfaction** — Go's structural typing means any struct that has the right methods satisfies an interface. The graph captures this for known interfaces, but cross-package satisfaction is fragile. When the convention's `dynamic_edges` matter (e.g., `io.Reader` implementations), name both ends.

## Deployment signals (Pass 5)

- `Dockerfile` (often multi-stage with a scratch/distroless final image).
- `goreleaser.yaml` / `Makefile` build targets.
- `go.mod` `go` directive — minimum runtime version.
- For services: a `Procfile`, a Kubernetes manifest, or systemd unit.

## Manifest files

`go.mod` and `go.sum`. List direct deps from `go.mod`'s `require` block excluding indirects.

## Cross-cutting pitfalls

- **Panic propagation** — a panic in a goroutine kills the process unless recovered. List every place `recover()` is used in `cross-cutting/errors.md`.
- **`init()` order** — files in a package run their `init()` functions before `main`; cross-package `init()` order depends on import order. Flag any `init()` that does I/O or sets globals.
- **Build tags** — files compiled only on certain OS/arch (e.g., `//go:build linux`) diverge between dev (mac) and prod (linux). Note divergent files explicitly.

## Cross-repo signals

Outbound HTTP via `net/http`; outbound gRPC via generated client code (the proto file is the join key); message bus via the cloud SDK. When `grafel_cross_links(action=list)` proposes an edge keyed on a proto package or on an HTTP path, accept with high confidence.
