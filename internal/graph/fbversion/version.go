// Package fbversion holds the single source of truth for the on-disk
// FlatBuffers format version written by internal/graph/fbwriter and
// read (with a minimum-version gate) by internal/graph/load.go.
//
// This is intentionally a leaf package with no imports so that both
// internal/graph (load.go) and internal/graph/fbwriter can import it
// without creating an import cycle.
package fbversion

// Version is the on-disk FB format version that fbwriter writes and
// load expects. Bump together with any schema change in graph.fbs that
// breaks readers. Both internal/graph and internal/graph/fbwriter
// import this package — drift is now compile-time impossible.
// Version 4 (#4881) — added the `signature` slot to the Entity table so the
// binary graph.fb path persists entity signatures (it never did before, which
// dropped SCOPE.Schema field TYPES from the dashboard shape API). A v3 graph.fb
// has no signatures, so the min-version gate rejects it and forces a clean
// reindex that repopulates signatures.
const Version = 4
