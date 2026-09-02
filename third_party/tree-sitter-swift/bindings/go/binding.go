// Package tree_sitter_swift is an inert placeholder. It is NOT a Swift grammar
// and deliberately exports nothing; anything that tries to use it will fail to
// compile, which is the truthful outcome. Its only job is to give an import
// path a home so that `go mod tidy` can finish loading the package graph.
//
// Why the path needs a home at all:
//
// github.com/alex-pinkus/tree-sitter-swift — the Swift grammar grafel actually
// uses — carries a test-only import of
// github.com/tree-sitter/tree-sitter-swift/bindings/go. That package has never
// existed. github.com/tree-sitter/tree-sitter-swift is archived and its final
// commit (2022) predates Go bindings entirely, so no version of it will ever
// provide the package. alex-pinkus corrected the import on `main` in d64a733
// (2025-06-16), but grafel has to pin the `with-generated-files` branch —
// `main` ships no src/parser.c — and that branch last synced the file in
// 2025-02, so no pin available to us carries the fix.
//
// `go mod tidy` loads test imports, so it aborted on the unresolvable package
// and go.mod / go.sum stopped being machine-checkable (issue #6732). A
// `replace` in the root go.mod points that import path here.
//
// This module intentionally has no requirements. Forwarding Language() to the
// real grammar was considered and rejected: it would pin alex-pinkus a second
// time, in a file nothing builds, that would silently drift out of step with
// the root go.mod on every grammar bump.
//
// Neither of those properties — no requirements, nothing exported — is observed
// by `go mod tidy`, which is satisfied by this package merely existing. They are
// enforced instead by TestSwiftShimModuleHasNoRequirements and
// TestSwiftShimExportsNothing in internal/treesitter (this is a nested module,
// so `go test ./...` from the repo root never reaches it).
//
// KNOWN LEAK. `go install` ignores the main module's `replace` directives, so
// `go install github.com/cajasmota/grafel/cmd/grafel@<version>` would try to
// resolve the placeholder require at v0.0.0-00010101000000-000000000000 and
// fail. No documented route uses that form — docs/install.md and
// docs/quickstart.md both `go install ./cmd/grafel` from a clone, and
// release.yml builds from the checkout, which is why the v0.3.1 platform
// binaries were unaffected — but anyone who reaches for the `@version` form
// will hit it, with no hint as to why.
//
// Nothing in grafel imports this package, and nothing should. If it ever
// becomes unnecessary — an upstream sync that fixes the test import — the
// removal is four edits, not two: delete this file and go.mod, then drop BOTH
// the `replace` and the `// indirect` require for
// github.com/tree-sitter/tree-sitter-swift from the root go.mod.
package tree_sitter_swift
