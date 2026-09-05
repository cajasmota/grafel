// Package repowalk holds the one exclusion list every repository-rooted source
// walk in this module shares.
//
// Several guards walk this repository's own tree and parse its Go source: the
// #6018 deterministic-temp-name guard, the two ~/.grafel home-path sweeps, and
// the entity/relationship kind scanners. Each carried its own hand-written copy
// of the same `switch d.Name()` exclusion list, and #6842 fixed exactly one of
// them by hand — leaving `internal/atomicfile` walking into `.claude` for
// another release (#6846).
//
// Deliberately NOT everything that looks like this walk uses SkippedDir. Two
// guards keep a hand-written copy on purpose, because their copy is an
// INDEPENDENT replica used to cross-check a scanner that also does the walk:
//
//   - internal/relkinds/undeclared_kinds_sweep_guard_6757_test.go countSourceFiles
//   - internal/types/producer_entity_kinds_6776_test.go literalOnlyEntityKinds
//
// Both compare their own file count against relkinds.Scan / entkinds.Scan. If
// they shared this predicate with the scanner they check, a single change here
// would move both sides of the comparison and the equality would still hold —
// a floor that asks the scanner how many files the scanner read is not a floor.
//
// internal/types/producer_kinds_test.go walkGoFiles is also out: it is rooted
// at internal/ rather than at the repository root, and skips `fixtures` — a
// name this list does not carry.
package repowalk

// SkippedDir reports whether a directory with this base name is foreign to a
// walk over this repository's own source, and must not be descended into.
//
// The names, and why each is here:
//
//   - .git      — object store, not source.
//   - .claude   — holds full worktree checkouts of THIS repository. Walking it
//     parses (and reports offences under) other branches' copies of the
//     production tree, under paths no allow-list can ever name, and any
//     mid-edit parse error in an unrelated in-flight branch fails a guard
//     that has nothing to do with it. CI never has worktrees there, so this
//     only ever breaks development checkouts (#6842, #6846).
//   - node_modules, vendor — third-party source, not ours.
//   - testdata — deliberate fixtures, including deliberately invalid ones.
//   - dist, build — build output.
//
// SkippedDir matches the WHOLE base name. It is deliberately not a prefix,
// suffix or substring test: `.claude-backup`, `claudex` and `vendored` are
// ordinary directories and a guard that skipped them would report a clean tree
// it never looked at.
func SkippedDir(name string) bool {
	switch name {
	case ".git", ".claude", "node_modules", "vendor", "testdata", "dist", "build":
		return true
	}
	return false
}
