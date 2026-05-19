// This go.mod isolates the go-chi-mini quality fixture from the parent
// archigraph module so `go vet ./...` and `go build ./...` at the repo root
// do not try to resolve the fixture's intentionally-vendored example imports
// (github.com/go-chi/chi/v5, example.com/demo/*). The fixture is consumed
// by the quality benchmark as source files on disk — it is never compiled
// or executed as a real Go program.

module example.com/go-chi-mini-fixture

go 1.25.5
