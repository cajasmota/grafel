package testsupport

// killguardscan.go — the structural guard that makes a THIRD unguarded
// process.Kill call site impossible to add silently (#7280).
//
// # Why this exists
//
// #7268 added process.KillGuarded — Kill, refusing to signal anything from
// inside a test binary — and wired it at the two `--kill-stale` sites, on the
// premise that "a seam only protects the tests that remember to install it, so
// the protection must belong to the call site". That work enumerated the
// repository's paths to process.Kill BY HAND. The enumeration missed two sites
// (internal/daemon/supervise.go and internal/daemon/service/service.go), which
// is what #7280 is.
//
// #7268's own lasting lesson is the same one, recorded because it cost seven
// rounds and four independent reviews: a fourth unrouted fixture table survived
// every human pass and was found by the windows CI leg in twenty minutes. The
// conclusion written down there is not "review harder" — it is that a
// hand-derived list of sites is unreliable in this repository and the audit has
// to be derived from code shape.
//
// So the durable half of #7280 is not the two one-line swaps. It is this: an
// AST scan that OBSERVES the boundary, so the third site cannot be added by
// somebody who never read either issue.
//
// # What it reports
//
// Every reference to the internal/process package's `Kill` — call or not — in a
// Go file, resolved through that file's own import spec so an alias
// (`import proc ".../internal/process"` → `proc.Kill`) is caught and an
// unrelated `cmd.Process.Kill()` on an *os.Process is not.
//
// Non-call references are the population that matters, because both #7280 sites
// are exactly that shape: `kill: process.Kill` inside a struct literal. A
// detector keyed on CallExpr would have found neither. That distinction is the
// direct analogue of absfixturescan's "routing a literal is not routing a
// table": check the shape you actually care about, not the proxy for it.
//
// # Scope, and why it is not an allowlist
//
// Callers decide which files to feed in; the repo-wide guard
// (internal/process/direct_kill_sweep_guard_7280_test.go) feeds it every
// NON-TEST .go file under internal/ and cmd/.
//
// Test files are out of scope on purpose rather than by omission. A test that
// names process.Kill deliberately is legitimate and already exists: the #7268
// identity assertions do `reflect.ValueOf(process.Kill).Pointer()` precisely to
// prove Kill and KillGuarded are different functions, and a guard that fired on
// those would be deleted within a week. What #7280 is about is PRODUCTION code
// wiring the unguarded Kill where a future test can reach it.
//
// Package internal/process itself is out of scope by CONSTRUCTION, not by a
// name check: inside that package Kill is a bare identifier, never a qualified
// selector, so there is nothing for this scanner to match. Stated because it
// looks like a missing exclusion and is not one. The cost is that it is blind
// to a new in-package direct use, which is the one place such a use is correct.
//
// # The opt-out is a MARKER, never a file allowlist
//
// A reference marked with the comment `//killguard:direct <reason>`, on its own
// line, in the comment group above, or trailing the line itself, is exempt.
//
// A file allowlist inside this scanner was the obvious alternative and is the
// wrong one: it silently exempts every LATER line added to that file, it drifts
// with every rename, and it is read by nobody. absfixturescan had the same
// choice and records the same answer. A marker sits on the line it excuses,
// forces a reason to be written next to it, and shows up in the diff that adds
// it.
//
// All three of those are OBSERVED, which they were not at first. Line-scoping
// was: moving the marker two lines away into a doc comment fires the sweep. The
// diff-visibility is a property of writing it in the source at all. But "forces
// a reason" was decorative — a bare `//killguard:direct` was honoured, so the
// marker was really a line-scoped allowlist and this paragraph asserted
// something no test could kill. It is enforced now: a marker with no reason
// after it does NOT exempt, and the finding says so rather than telling the
// author to mark a line they already marked. See killGuardMarkedLines.
//
// Exactly one site needs it today: internal/daemon/reaper.go's sigtermPID. It
// is reached by tests, and the PIDs it receives are children the test spawned
// itself (pidfile_test.go's spawnLiveChild — `exec.Command("sleep","30")`).
// Verified by panic probe, not assumed: TestReaper_sweepWatchers_LiveDaemonPIDFromPidfile
// reaches it via watchreg.Sweep with a spawnLiveChild pid. Guarding it would
// break a test that legitimately kills its own child.
//
// # Known blind spots, stated rather than discovered later
//
//   - It reasons lexically over one file's imports. A file that imports
//     internal/process AND declares a local variable named `process` would have
//     that variable's `.Kill` reported. That is a FALSE POSITIVE — the loud
//     direction — and no such shadowing exists in this tree.
//   - It cannot follow indirection. `k := process.Kill` is caught (the
//     reference is still a selector), but a Kill reached through a function
//     value returned by another package is not. Bounded by the fact that both
//     real sites, and every plausible next one, name the symbol directly.
//   - It says nothing about whether KillGuarded is the value actually WIRED at
//     a site — only that the unguarded Kill is not named there. Wiring is
//     graded separately, by function identity, in each consuming package.

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"
)

// ProcessImportPath is the package whose Kill must not be referenced directly.
const ProcessImportPath = "github.com/cajasmota/grafel/internal/process"

// KillGuardMarker opts one reference out of the scan. See "The opt-out".
const KillGuardMarker = "//killguard:direct"

// DirectKill is one reference to the unguarded process.Kill.
type DirectKill struct {
	File string // slash-separated path, relative to the scan root
	Line int
	Fn   string // enclosing function name, or "" at package level
	Expr string // the reference as written, e.g. "process.Kill" or "proc.Kill"

	// BareMarker is true when the site DOES carry a KillGuardMarker but no
	// reason after it, so the marker was not honoured. Without it the author
	// reads "mark the line" on a line they already marked, which is the worst
	// possible diagnostic: it reads as a broken guard rather than as the rule
	// the guard is enforcing.
	BareMarker bool
}

// Key is the ledger/diagnostic key: file + enclosing function. Not line-based —
// a line number churns on every edit above it.
func (d DirectKill) Key() string { return d.File + ":" + d.Fn }

func (d DirectKill) String() string {
	if d.BareMarker {
		return fmt.Sprintf("%s:%d %s references %s directly and carries a BARE %s with no reason "+
			"after it, so the marker was not honoured. The marker must be followed by the reason "+
			"it excuses — that forced justification is the whole of its advantage over an "+
			"allowlist. Write it, or, if there is no such reason, use process.KillGuarded",
			d.File, d.Line, d.Fn, d.Expr, KillGuardMarker)
	}
	return fmt.Sprintf("%s:%d %s references %s directly — use process.KillGuarded, "+
		"or mark the line %s <reason> if this site must signal a process the caller spawned",
		d.File, d.Line, d.Fn, d.Expr, KillGuardMarker)
}

// FindDirectKills reports every reference to internal/process.Kill in f.
//
// rel is the path reported in findings. A file that does not import
// internal/process is reported clean without any further inspection, which is
// what makes an unrelated `cmd.Process.Kill()` invisible.
func FindDirectKills(fset *token.FileSet, f *ast.File, rel string) []DirectKill {
	pkgIdent, dotImported, ok := processPkgIdent(f)
	if !ok {
		return nil
	}
	exempt, bare := killGuardMarkedLines(fset, f)

	var out []DirectKill
	seen := map[int]bool{}
	report := func(pos token.Pos, expr, fn string) {
		p := fset.Position(pos)
		if exempt[p.Line] || seen[p.Line] {
			return
		}
		seen[p.Line] = true
		out = append(out, DirectKill{
			File: rel, Line: p.Line, Fn: fn, Expr: expr, BareMarker: bare[p.Line],
		})
	}

	inspect := func(fn string, n ast.Node) {
		ast.Inspect(n, func(x ast.Node) bool {
			switch e := x.(type) {
			case *ast.SelectorExpr:
				id, isIdent := e.X.(*ast.Ident)
				if isIdent && pkgIdent != "" && id.Name == pkgIdent && e.Sel.Name == "Kill" {
					report(e.Pos(), id.Name+".Kill", fn)
				}
			case *ast.Ident:
				// A dot-import puts Kill in file scope as a bare identifier.
				if dotImported && e.Name == "Kill" {
					report(e.Pos(), "Kill", fn)
				}
			}
			return true
		})
	}

	for _, d := range f.Decls {
		switch decl := d.(type) {
		case *ast.FuncDecl:
			if decl.Body == nil {
				continue
			}
			name := decl.Name.Name
			if decl.Recv != nil && len(decl.Recv.List) > 0 {
				name = killGuardReceiverName(decl.Recv.List[0].Type) + "." + name
			}
			inspect(name, decl.Body)
		case *ast.GenDecl:
			// A package-level `var kill = process.Kill` is a wiring decision
			// exactly like the two inside functions, and has no enclosing
			// function to name.
			inspect("", decl)
		}
	}
	return out
}

// processPkgIdent returns the identifier f binds internal/process to. The
// second result is true for a dot-import, whose references are bare idents.
func processPkgIdent(f *ast.File) (name string, dotImported bool, found bool) {
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || p != ProcessImportPath {
			continue
		}
		if imp.Name == nil {
			return "process", false, true
		}
		switch imp.Name.Name {
		case ".":
			return "", true, true
		case "_":
			// Imported for side effects only; nothing can be referenced.
			return "", false, false
		default:
			return imp.Name.Name, false, true
		}
	}
	return "", false, false
}

// killGuardMarkedLines returns the lines a KillGuardMarker comment covers,
// split by whether the marker carried a REASON: exempt lines were excused, bare
// lines carry a marker that was NOT honoured.
//
// A comment GROUP carrying the marker covers its own lines (for a marker
// trailing the reference) and the line after the group (for a marker above it).
// The group rather than the single marker line, because the reason the marker
// demands is prose and wraps — absfixturescan's first cut matched only the
// marker line and the one below, and so ignored the very exemption it had just
// been given.
//
// # Why a reason is REQUIRED and not merely requested
//
// This file justifies choosing a marker over a file allowlist on three grounds:
// the marker is line-scoped, it forces a reason to be written next to it, and
// it shows up in the diff that adds it. Grounds one and three were observed
// from the start. Ground two was NOT: a bare `//killguard:direct` was honoured,
// which made the marker a line-scoped allowlist and the stated rationale a
// claim no test could kill. That is this repository's dominant defect class,
// and it was found by mutating the marker rather than by reading the prose.
//
// So the rule is enforced here: no reason, no exemption.
//
// # What counts as the reason, across all three positions the scanner accepts
//
// The reason is the text AFTER the marker, within the marker's own comment
// group — either on the marker's own line, or on a continuation line below it
// in the same group. One rule, applied identically whether the marker sits
// above the reference or trails it; a trailing marker simply has no
// continuation lines available, so its reason must be on its own line.
//
// Text BEFORE the marker does not count. `// some prose` on the line above is
// the author explaining the code, not justifying the exemption, and accepting
// it would let any marker dropped under an existing comment pass.
//
// Block comments are not a path: HasPrefix requires the comment to open with
// `//`, and a `/*...*/` never does. So a block comment can neither mark a line
// nor supply a reason, which is stated rather than left to be discovered.
func killGuardMarkedLines(fset *token.FileSet, f *ast.File) (exempt, bare map[int]bool) {
	exempt, bare = map[int]bool{}, map[int]bool{}
	for _, cg := range f.Comments {
		idx := -1
		for i, c := range cg.List {
			if strings.HasPrefix(c.Text, KillGuardMarker) {
				idx = i
				break
			}
		}
		if idx < 0 {
			continue
		}
		dst := bare
		if killGuardReasonGiven(cg.List[idx:]) {
			dst = exempt
		}
		start := fset.Position(cg.Pos()).Line
		end := fset.Position(cg.End()).Line
		for ln := start; ln <= end+1; ln++ {
			dst[ln] = true
		}
	}
	return exempt, bare
}

// killGuardReasonGiven reports whether any non-empty text follows the marker in
// rest, which is the marker's comment followed by the rest of its group.
func killGuardReasonGiven(rest []*ast.Comment) bool {
	if len(rest) == 0 {
		return false
	}
	if strings.TrimSpace(strings.TrimPrefix(rest[0].Text, KillGuardMarker)) != "" {
		return true
	}
	for _, c := range rest[1:] {
		if strings.TrimSpace(strings.TrimPrefix(c.Text, "//")) != "" {
			return true
		}
	}
	return false
}

func killGuardReceiverName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return killGuardReceiverName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return killGuardReceiverName(t.X)
	}
	return "?"
}
