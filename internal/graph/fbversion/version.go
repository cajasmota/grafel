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
//
// Version 5 (#6033) — DATA-REPAIR bump, not a schema change. The v4 payload
// format is unchanged and still readable; the bump exists solely to force a
// clean reindex of graphs whose CONTENT is corrupt. Every incremental pass
// duplicated the entire surviving relationship set, so a v4 graph.fb can hold
// 2/4/8/16 copies of every edge (and stale unbound stub edges alongside their
// rewired copies). The #6033 fix stops the growth but cannot repair it —
// survivors are carried forward verbatim and no dedupe is possible, because
// RelationshipID is hash(from,to,kind) and the extractors deliberately emit
// distinct same-ID edges (golang/extractor.go:1421 `mvKey := mvTarget+"?mv"`),
// so deduping would diverge from a full rebuild. Rejecting v4 makes
// load.go's min-version gate fire, which the daemon turns into an automatic
// full reindex (internal/daemon/stale_reindex.go) — every existing user is
// repaired on upgrade with no manual `grafel index` and no `grafel reset`.
//
// Version 6 (#6236) — added the `end_line` slot to the Entity table. The
// binary path had only source_line, so every entity loaded from graph.fb came
// back with EndLine 0 even though graph.json has always carried the value.
// internal/coverage/attribute.go reads a zero EndLine as "no usable span" and
// answers with whole-file coverage, so a v5 graph.fb makes an incremental
// index disagree with a full one about the same entity. Rejecting v5 forces
// the clean reindex that repopulates spans.
const Version = 6
