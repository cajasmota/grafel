package process_test

// direct_kill_sweep_guard_7280_test.go — the repo-wide, BINDING guard on the
// #7268/#7280 "a production kill path names the unguarded process.Kill" class.
//
// # Why this file exists, and why the two one-line swaps were not the deliverable
//
// #7268 added process.KillGuarded and wired it at the two `grafel doctor
// --kill-stale` sites, having enumerated the repository's paths to process.Kill
// BY HAND. The enumeration missed two — internal/daemon/supervise.go and
// internal/daemon/service/service.go — which is what #7280 is.
//
// That is the same failure mode #7268 itself recorded, at a cost of seven
// rounds and four independent adversarial reviews: a fourth unrouted fixture
// table survived every human pass and was found by the windows CI leg in twenty
// minutes. The conclusion written there is not "review harder" — a hand-derived
// list of sites is unreliable in this repository, and the audit has to be
// derived from code shape. This is that audit, kept as a test so it runs on
// every change rather than on every remembering.
//
// So the durable half of #7280 is not the swaps; it is this. A third site
// cannot now be added by somebody who never read either issue.
//
// # What it observes
//
// testsupport.FindDirectKills reports every reference to internal/process.Kill
// — call or not — in a non-test .go file under internal/ or cmd/, resolved
// through that file's own import spec so an alias is caught and an unrelated
// cmd.Process.Kill() on an *os.Process is not.
//
// NON-CALL references are the population that matters: both #7280 sites were
// `kill: process.Kill` inside a struct literal, and a detector keyed on
// CallExpr would have found neither. Switching a site to process.KillGuarded
// removes it from the scan, which is the entire feedback loop.
//
// # The expected set is EMPTY, and there is no ledger
//
// Deliberately unlike the #6478 blocking-open sweep, which carries 34 ledgered
// call sites. This class has exactly one legitimate site in the whole tree, and
// it is excused where it lives — by a `//killguard:direct <reason>` marker on
// the line, not by an allowlist in this file.
//
// That choice is the one #7268's review forced on absfixturescan and it is the
// same choice here: a file allowlist silently exempts every LATER line added to
// that file, drifts with every rename, and is read by nobody. A marker sits on
// the line it excuses, forces a reason next to it, and appears in the diff that
// adds it. An author who trips this sweep therefore cannot silence it by
// editing this test.
//
// # The one marked site, verified rather than assumed
//
// internal/daemon/reaper.go's sigtermPID. It is reached by tests, and the pids
// it receives are children the test spawned itself: a panic probe at that line
// fired in TestReaper_sweepWatchers_LiveDaemonPIDFromPidfile, reaching it
// through watchreg.Sweep with a pid from pidfile_test.go's spawnLiveChild
// (`exec.Command("sleep","30")`). Guarding it would break a legitimate test
// rather than protect anything. Recorded so a future sweep does not "complete
// the coverage" and regress it.
//
// # Scope, stated rather than discovered later
//
// TEST files are out of scope on purpose. A test naming process.Kill
// deliberately is legitimate and already exists: the #7268 identity assertions
// do reflect.ValueOf(process.Kill).Pointer() precisely to prove Kill and
// KillGuarded are different functions. #7280 is about PRODUCTION code wiring
// the unguarded Kill where a future test can reach it.
//
// Package internal/process itself is out of scope by CONSTRUCTION rather than
// by a name check: inside it Kill is a bare identifier, never a qualified
// selector, so there is nothing to match. The cost is blindness to a new
// in-package direct use — the one place such a use is correct.
//
// # This guard's own guard
//
// TestDirectKillSweepCanFail plants unguarded sites in a synthetic tree and
// requires the scan to name them, so "this lint can go red" is an assertion
// rather than a hope. TestDirectKillSweepIsNotVacuous pins that the repo walk
// actually reaches source AND that it reaches the files the known sites live
// in, because a walk that reads nothing — or that reads everything except
// internal/daemon — reports nothing and looks green.
//
// # The guard's ALPHABET is one symbol, and that is a limit on the claim
//
// This sweep observes references to internal/process.Kill. It is NOT a general
// "no test binary may signal a stranger" lint: a future site reaching for
// syscall.Kill, (*os.Process).Signal, (*exec.Cmd).Process.Kill or a platform
// taskkill shell-out is outside its alphabet and would pass unseen.
//
// So "a third site cannot be added silently" is true of the process.Kill
// class, not of the kill class. Stated because the narrower claim is the one
// that was measured: a sweep of the other kill primitives in this tree found
// no live #7280-class site outside process.Kill —
// dashboard/handlers_system.go:127,155 signal os.Getpid() (self, and no test
// drives those handlers), supervise_unix.go:72 and sched/nice_unix.go:69 take
// a process the supervisor spawned, and install/watchers/loader_windows.go:59
// kills its own child. The wording is over-broad; it hides no hole today.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/repowalk"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// TestNoDirectProcessKills is the binding assertion: no non-test file under
// internal/ or cmd/ names process.Kill without a marker excusing the line.
func TestNoDirectProcessKills(t *testing.T) {
	found := scanDirectKills(t, repoRootFor7280(t))
	if len(found) == 0 {
		return
	}
	var lines []string
	for _, f := range found {
		lines = append(lines, "  "+f.String())
	}
	t.Fatalf("%d production reference(s) to the UNGUARDED process.Kill:\n%s\n\n"+
		"process.KillGuarded (#7268) is Kill that panics under `go test` instead of signalling, so "+
		"the protection belongs to the call site rather than to each test's discipline. Wire that "+
		"instead. If this site must signal a process the caller spawned itself — reaper.go's "+
		"sigtermPID is the only such site in the tree — mark the line `%s <reason>`. Do NOT add an "+
		"allowlist to this test: an allowlist would silently exempt every later line in the same file.",
		len(found), strings.Join(lines, "\n"), testsupport.KillGuardMarker)
}

// TestDirectKillSweepCanFail plants offenders in a synthetic tree and requires
// the sweep to name them. Without it, "the repo is clean" and "the scan is
// dead" are indistinguishable.
//
// It plants FOUR shapes on purpose. The struct-literal one is the #7280 shape
// the hand enumeration missed, and it is the one a CallExpr-keyed detector
// would silently skip; the aliased import defeats a text grep; the marked site
// must NOT be reported, so the marker is proved to work here rather than only
// where reaper.go relies on it; and the KillGuarded site must not be reported,
// so the feedback loop is proved to close.
func TestDirectKillSweepCanFail(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "internal", "planted")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	write := func(name, src string) {
		if err := os.WriteFile(filepath.Join(pkg, name), []byte(src), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("offender_literal.go", `package planted
import "`+testsupport.ProcessImportPath+`"
func start() { reap(deps{kill: process.Kill}) }`)
	write("offender_alias.go", `package planted
import proc "`+testsupport.ProcessImportPath+`"
func sweep(pid int) error { return proc.Kill(pid) }`)
	write("marked.go", `package planted
import "`+testsupport.ProcessImportPath+`"
func sigterm(pid int) error {
	//killguard:direct kills a child this caller spawned
	return process.Kill(pid)
}`)
	write("marked_noreason.go", `package planted
import "`+testsupport.ProcessImportPath+`"
func bareMarked(pid int) error {
	//killguard:direct
	return process.Kill(pid)
}`)
	write("guarded.go", `package planted
import "`+testsupport.ProcessImportPath+`"
func guarded() { reap(deps{kill: process.KillGuarded}) }`)
	// A _test.go offender must be invisible: the #7268 identity assertions name
	// process.Kill deliberately and must never trip this sweep.
	write("offender_test.go", `package planted
import "`+testsupport.ProcessImportPath+`"
func TestX() { _ = process.Kill }`)

	got := scanDirectKills(t, root)
	var keys []string
	for _, g := range got {
		keys = append(keys, g.Key())
	}
	sort.Strings(keys)
	want := []string{
		"internal/planted/marked_noreason.go:bareMarked",
		"internal/planted/offender_alias.go:sweep",
		"internal/planted/offender_literal.go:start",
	}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("planted sweep reported %v, want exactly %v.\n"+
			"A missing offender_literal row means the detector cannot see the #7280 struct-literal "+
			"shape, which is the whole point. A missing offender_alias row means one rename defeats "+
			"it. A missing marked_noreason.go row means a BARE marker is still honoured, so the "+
			"marker is a line-scoped allowlist and this file's \"forces a reason\" rationale is "+
			"decorative. An extra marked.go row means the opt-out marker does not work. An extra guarded.go "+
			"row means the fix does not silence the guard. An extra offender_test.go row means the "+
			"sweep reads test files and will fire on #7268's own identity assertions.", keys, want)
	}
	// The diagnostic must name the fix, not just the offence.
	for _, g := range got {
		if !strings.Contains(g.String(), "KillGuarded") {
			t.Errorf("finding %q does not name the fix", g.String())
		}
	}
}

// TestDirectKillSweepIsNotVacuous pins that the repo walk reaches real source,
// and specifically that it reaches THE FILES THE KNOWN SITES LIVE IN.
//
// A walk rooted at the wrong directory, or one whose exclusion list swallowed
// the trees those sites live in, reports a clean tree it never looked at.
func TestDirectKillSweepIsNotVacuous(t *testing.T) {
	root := repoRootFor7280(t)

	// One walk, its delivery recorded. Everything below asserts against what
	// the WALKER actually handed over, which is the distinction the first cut
	// of this test got wrong — see "reads the wrong files" below.
	visited := map[string]bool{}
	walkNonTestGoFilesFor7280(t, root, func(rel string, _ *token.FileSet, _ *ast.File) {
		visited[rel] = true
	})

	if n := len(visited); n < 500 {
		t.Fatalf("repo walk parsed %d non-test .go files under internal/ and cmd/; the sweep is "+
			"not binding the repository", n)
	}

	// READS THE WRONG FILES — vacuity way #2, which the count floor above
	// CANNOT see, and this is measured rather than argued.
	//
	// Adding one base name to the walker's prune — `|| d.Name() == "daemon"` —
	// hides reaper.go, supervise.go AND service/service.go, i.e. the entire
	// known population, in a single token. Measured on this tree rather than
	// estimated: the walk delivers 2112 non-test files, 127 of them under
	// internal/daemon, so the blinded walk still delivers 1985 — four times
	// the floor above, while the sweep goes green having looked at none of the
	// sites it exists for. That mutant was ALIVE against the first cut of this
	// file, and TestNoDirectProcessKills still PASSES under it today; the loop
	// below is the only thing that catches it. Compound it with the literal
	// #7280 regression (`kill: reapStaleKill` reverted to `kill: process.Kill`)
	// and the whole suite went green — the sweep is the SOLE grader of that
	// direction, so blinding the walk retires the one thing grading the
	// regression this issue exists to prevent. That compound is RED now, and
	// only because of the loop below.
	//
	// The hazard is not contrived. repowalk.SkippedDir is deliberately shared,
	// and internal/repowalk's own doc records a MEASURED instance of exactly
	// this: widening that list left 7 production Go files unread by both sides
	// of a guard. Three sibling guards (entkinds, relkinds, types) keep an
	// independent replica walk for that reason. This guard has one walk, so
	// the cross-check is this: name the files that must arrive.
	//
	// ALL THREE, not just the marked one, and that is a measurement rather
	// than a preference. reaper.go alone closes the `daemon` case above, but a
	// narrower prune — `|| d.Name() == "service"` — hides only
	// service/service.go: scored both ways, that prune is RED against the list
	// below and GREEN against a list holding reaper.go alone. The two WIRING
	// sites are the ones M3/M6 protect and this sweep is their only grader, so
	// each has to be named. The cost is a literal path list that must be updated by
	// hand if a site moves; that is a deliberate two-place edit, visible in
	// the diff, and it is the same trade the sibling guards make for their
	// independent floors.
	for _, want := range []string{
		"internal/daemon/reaper.go",          // the one MARKED site
		"internal/daemon/supervise.go",       // wiring site 2 (#7280)
		"internal/daemon/service/service.go", // wiring site 1 (#7280)
	} {
		if !visited[want] {
			t.Fatalf("the repo walk never delivered %s — the marked site and both #7280 wiring "+
				"sites live under internal/daemon, so a prune that swallows it leaves "+
				"TestNoDirectProcessKills green having inspected none of them. It walked %d "+
				"files, which is why the count floor above says nothing about this", want, len(visited))
		}
	}

	// EVERY DECLARED TREE, not just the one the sites live in. The list above
	// pins the internal/daemon subtree, so `|| d.Name() == "cmd"` was ALIVE:
	// cmd/ is 56 non-test files, far below any sane floor, and nothing
	// observed that the walk reached it at all.
	//
	// It hides no hole today — cmd/ contains no kill primitive of any kind, so
	// this closes a shape rather than a defect. It is closed anyway because
	// enumeration is this guard's entire purpose, and "the walk reaches every
	// tree it claims to scan" is the same property the list above pins one
	// level down.
	//
	// Asserted by PREFIX rather than by naming a file under cmd/. A path list
	// there would be pure churn: there is no site to pin, so any file named
	// would be an arbitrary hostage to the next rename. The prefix asks the
	// only question that has an answer — did this tree arrive at all. It is
	// deliberately weaker than the list above, and weaker in the right place:
	// where the population lives, the exact paths are named.
	//
	// THE "internal/" ENTRY IS A PROVABLY EQUIVALENT MUTANT, disclosed rather
	// than left for the next reviewer to find. Deleting it is ALIVE, and the
	// reason is algebra rather than a gap: the loop above already requires
	// "internal/daemon/reaper.go" to be in visited, every string with prefix
	// "internal/daemon/" has prefix "internal/", and that loop runs FIRST, so
	// its t.Fatalf pre-empts. No input can fail the "internal/" check while the
	// path list passes.
	//
	// It is kept anyway, and the standing "an ALIVE mutant means add a test,
	// not delete a line" rule is why the alternative was rejected: the only
	// test that could kill it is one that deletes the stronger path list, which
	// is strictly worse. Kept so this loop states the scope it claims — every
	// declared tree — instead of silently covering one of the two and relying
	// on a reader to notice the other is pinned twenty lines up.
	for _, top := range []string{"internal/", "cmd/"} {
		reached := false
		for rel := range visited {
			if strings.HasPrefix(rel, top) {
				reached = true
				break
			}
		}
		if !reached {
			t.Fatalf("the repo walk delivered nothing under %s, a tree this sweep declares it "+
				"scans. It walked %d files, so the count floor cannot see this either", top, len(visited))
		}
	}

	// The remaining check grades the DETECTOR, not the walker: that removing
	// the marker from reaper.go's source makes the site visible again, so the
	// live clean verdict is the marker talking and not a dead scan. It reads
	// the file directly rather than through the walker ON PURPOSE — the walker
	// hands back a parsed *ast.File and this needs source text to strip — and
	// therefore proves nothing about delivery. Delivery is the loop above.
	src, err := os.ReadFile(filepath.Join(root, "internal", "daemon", "reaper.go"))
	if err != nil {
		t.Fatalf("read reaper.go: %v", err)
	}
	if !strings.Contains(string(src), testsupport.KillGuardMarker) {
		t.Fatalf("internal/daemon/reaper.go no longer carries a %s marker. Either sigtermPID was "+
			"changed (in which case delete this assertion deliberately) or the marker was dropped "+
			"and TestNoDirectProcessKills should now be RED — if it is green, the sweep is not "+
			"reading this file", testsupport.KillGuardMarker)
	}
	stripped := strings.ReplaceAll(string(src), testsupport.KillGuardMarker, "// marker removed")
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, "reaper.go", stripped, parser.ParseComments|parser.SkipObjectResolution)
	if perr != nil {
		t.Fatalf("parse stripped reaper.go: %v", perr)
	}
	if got := testsupport.FindDirectKills(fset, f, "internal/daemon/reaper.go"); len(got) == 0 {
		t.Fatal("with its marker removed, internal/daemon/reaper.go reports NO direct kill — the " +
			"detector is not seeing the site the live sweep claims to be excusing, so the clean " +
			"verdict above means nothing")
	}
}

// scanDirectKills walks root's internal/ and cmd/ trees and returns every
// unmarked reference to internal/process.Kill in a non-test file.
func scanDirectKills(t *testing.T, root string) []testsupport.DirectKill {
	t.Helper()
	var out []testsupport.DirectKill
	walkNonTestGoFilesFor7280(t, root, func(rel string, fset *token.FileSet, f *ast.File) {
		out = append(out, testsupport.FindDirectKills(fset, f, rel)...)
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// walkNonTestGoFilesFor7280 visits every non-test .go file under root/internal
// and root/cmd. A directory that does not exist under root is skipped, so the
// same walker serves the repository and the synthetic tree.
//
// ParseComments is REQUIRED and not decoration: go/parser drops comments
// without it, and the //killguard:direct marker is a comment. The sibling #6478
// sweep this is modelled on parses with SkipObjectResolution alone, so copying
// that call is the natural mistake — pinned in
// internal/testsupport/killguardscan_test.go by
// TestFindDirectKillsNeedsParsedComments.
func walkNonTestGoFilesFor7280(t *testing.T, root string, visit func(rel string, fset *token.FileSet, f *ast.File)) {
	t.Helper()
	for _, top := range []string{"internal", "cmd"} {
		dir := filepath.Join(root, top)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				// The exclusion list is shared (#6846): `.claude` in
				// particular holds full worktree checkouts of THIS repository,
				// so walking it reports offences under other branches' copies.
				if repowalk.SkippedDir(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
			if perr != nil {
				t.Fatalf("parse %s: %v", rel, perr)
			}
			visit(filepath.ToSlash(rel), fset, f)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}

// repoRootFor7280 locates the repository root from this package's own directory
// rather than from the working directory, which `go test` sets per-package.
func repoRootFor7280(t *testing.T) string {
	t.Helper()
	dir, err := testsupport.PackageDirOfCaller(0)
	if err != nil {
		t.Fatalf("locate package dir: %v", err)
	}
	root := filepath.Dir(filepath.Dir(dir)) // internal/process -> internal -> repo
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("resolved repo root %q has no go.mod: %v", root, err)
	}
	return root
}
