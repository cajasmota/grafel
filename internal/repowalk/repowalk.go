// Package repowalk holds the one exclusion list every repository-rooted source
// walk in this module shares.
//
// Several guards walk this repository's own tree and parse its Go source: the
// #6018 deterministic-temp-name guard, the two ~/.grafel home-path sweeps, the
// #6478 blocking-open sweep, the #6199 phase-trace proof, and the
// entity/relationship kind scanners. Each carried its own hand-written copy of
// the same `switch d.Name()` exclusion list.
//
// ELEVEN copies at 0ce393bc3, two of them production code, drifted into THREE
// different sets — and #6842 fixed exactly one by hand. #6846 was filed for
// `internal/atomicfile`; enumerating the literal rather than trusting the
// issue's file list turned up a second live omission in
// internal/extractors/incremental_phasetrace_6199_test.go, which is rooted at
// the module root and whose "exactly one call site" assertion therefore failed
// in every development checkout holding an agent worktree — no parse error
// required. Seven copies share this list now; the four that do not are named
// below, each with the reason recorded at the walk itself.
//
// Deliberately NOT everything that looks like this walk uses SkippedDir. Three
// guards keep a hand-written copy on purpose, because their copy is an
// INDEPENDENT replica used to cross-check a scanner that also does the walk:
//
//   - internal/entkinds/rule_declared_kinds_sweep_guard_6744_test.go countSourceFiles
//   - internal/relkinds/undeclared_kinds_sweep_guard_6757_test.go countSourceFiles
//   - internal/types/producer_entity_kinds_6776_test.go literalOnlyEntityKinds
//
// Both compare their own file count against relkinds.Scan / entkinds.Scan. If
// they shared this predicate with the scanner they check, a single change here
// would move both sides of the comparison and the equality would still hold —
// a floor that asks the scanner how many files the scanner read is not a floor.
//
// That is a measurement, not an argument. Widening SkippedDir with
// "security","atomicfile","registry" and running relkinds.TestRepoSweepIsNotVacuous
// both ways:
//
//	as shipped (independent countSourceFiles)  FAIL — "scan parsed 2079
//	                                           non-test .go files; an independent
//	                                           walk finds 2086"
//	countSourceFiles switched to SkippedDir    PASS — while 7 real production
//	                                           Go files went unread by BOTH sides
//
// The equality is backstopped by that test's absolute wantGo/wantYAML floors,
// which independently catch LARGE widenings, so the failure mode needs a
// deliberately small one. The backstop is coarse; the independence is what
// makes the small case visible at all.
//
// The fourth exception, internal/types/producer_kinds_test.go walkGoFiles, is
// out for a different reason: it is rooted at internal/ rather than at the
// repository root, and skips `fixtures` — a name this list does not carry.
//
// The test beside this file is the LOWER BOUND the whole set was missing: it
// fails if this list ever grows to cover a directory a guard must descend into
// to reach real source. It deliberately prunes with its own literal replica
// rather than with SkippedDir, for the same both-sides-of-the-equality reason.
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
//
// It takes a BASE name, not a path: SkippedDir("a/b/.claude") is false. Every
// call site passes fs.DirEntry.Name() from a filepath.WalkDir callback, which
// is always a base name, so this is a latent trap rather than a live one — but
// a caller that hands it a path gets a silent false and walks in.
func SkippedDir(name string) bool {
	switch name {
	case ".git", ".claude", "node_modules", "vendor", "testdata", "dist", "build":
		return true
	}
	return false
}
