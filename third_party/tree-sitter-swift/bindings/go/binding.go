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
// Nothing in grafel imports this package, and nothing should. If it ever
// becomes unnecessary — an upstream sync that fixes the test import — delete
// this directory and the matching `replace` in the root go.mod together.
package tree_sitter_swift
