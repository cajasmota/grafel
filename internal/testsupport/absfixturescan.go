package testsupport

// absfixturescan.go — the structural guard that makes a FIFTH unrouted
// fixture table impossible to add silently (#7268).
//
// # Why this exists
//
// daemon.IsCanonicalBinaryPath opens with filepath.IsAbs, whose answer is
// GOOS-dependent: "/usr/local/bin/grafel" is absolute on unix and NOT absolute
// on windows, which has no volume in it. A fixture table of bare unix literals
// therefore does not grade the gate on windows — its forbidden rows pass
// because everything looked relative, and its REQUIRED rows invert and go red.
// AbsFixture exists to route fixtures past that.
//
// Four tables had the defect. Three independent adversarial reviews and seven
// rounds enumerated them BY HAND and found three; the fourth
// (TestStaleProcessClassification) was missed by every one of them and found by
// the windows CI leg in twenty minutes. The lesson recorded on #7268 is not
// "review harder" — a hand-derived list of sites is unreliable here, and the
// audit has to be derived from code shape. This is that audit, kept as a test
// so it runs on every change rather than on every remembering.
//
// # What it checks, and at what granularity
//
// The unit is a FUNCTION, not a file — a file can hold one routed table and one
// unrouted, and three of these files do. For each test function that reaches
// the identity gate, it asks: does this function contain a unix-absolute
// FIXTURE literal while performing no routing at all?
//
// Per-function is the right granularity because of how these tables are
// written: they store bare literals and route them once in the loop body
// (`exe = testsupport.AbsFixture(tc.exe)`). Once a table routes THAT way, every
// row it gains later is routed automatically and cannot be forgotten.
//
// So "does this function route?" is asked precisely, and the distinction is
// load-bearing: routing a NON-LITERAL (`AbsFixture(tc.exe)`) covers the whole
// table, while routing a LITERAL (`selfExe := AbsFixture("/usr/local/bin/grafel")`)
// covers only that one literal. An earlier cut of this scanner accepted either
// as proof the function was safe — and then failed to notice when the loop-body
// routing was deleted from a table whose selfExe was still routed, because the
// function still "routed something". That mutant was ALIVE; this is why the two
// cases are now separated.
//
// # Deliberate exemptions
//
// /tmp-PREFIXED literals are exempt and must stay unrouted. The production test
// they exercise is strings.HasPrefix(exe, "/tmp/"), a byte comparison, so
// grafting a volume on moves the fixture off the boundary and turns a boundary
// row into an unrelated row that still passes. That includes the siblings
// (/tmpfoo, /tmpdir, /tmp-agent) whose entire purpose is to sit next to the
// boundary. Such rows are ungradable on windows and are SKIPPED there by their
// tables, not routed.
//
// # Known blind spots, stated rather than discovered later
//
//   - It reasons lexically. A table that routed through a helper this scanner
//     does not recognise as a router would read as unrouted (false positive,
//     loud) — and one that called AbsFixture on something irrelevant would read
//     as routed (false negative, silent). The second is the dangerous one; it
//     is bounded by the fact that a function doing any routing at all is a
//     function whose author knew about the problem.
//   - It says nothing about whether a routed row is CORRECT, only that routing
//     happens. The count floors in each table cover the rest.
//   - Prose in a `name:`/`why:` field that begins with "/" is excluded by key.
//     A positional struct field holding such prose would be treated as a
//     fixture; the diagnostic says so, and the fix is to key the field.
//
// # The opt-out
//
// A literal marked with the comment `//absfixture:unrouted <reason>`, on its own
// line or the line above, is exempt. One case genuinely needs it:
// TestFindCanonicalDaemon_AbsolutenessIsPlatformSpecific asserts that a
// VOLUME-LESS path is rejected on windows and accepted on unix, so routing its
// fixture would destroy the thing it tests.
//
// It is a marker rather than an allowlist inside this file on purpose: an
// allowlist drifts silently and is read by nobody, while a marker sits on the
// line it excuses, forces a reason to be written next to it, and shows up in
// the diff that adds it.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// gateFuncs are the functions whose behaviour depends on filepath.IsAbs. A test
// function that calls one is a function whose absolute-looking fixtures matter.
var gateFuncs = map[string]bool{
	"IsCanonicalBinaryPath":      true,
	"isStaleProc":                true,
	"isStaleDiagnosticsProc":     true,
	"scanGrafelProcs":            true,
	"runDoctorStaleDaemons":      true,
	"handleDiagnosticsKillStale": true,
	"findCanonicalDaemon":        true,
	"staleFor":                   true,
}

// absFixtureRouters are the spellings that count as routing a fixture.
var absFixtureRouters = map[string]bool{
	"AbsFixture":    true,
	"absFixture":    true,
	"absFixtureFor": true,
}

// proseKeys are struct fields that hold human text, not paths.
var proseKeys = map[string]bool{
	"name": true, "Name": true, "why": true, "Why": true, "msg": true, "Message": true,
}

// UnroutedFixture is one gate-reaching function that holds unix-absolute
// fixture literals and routes none of them.
type UnroutedFixture struct {
	File     string
	Func     string
	Line     int
	Literals []string
}

func (u UnroutedFixture) String() string {
	return fmt.Sprintf("%s:%d %s holds unrouted unix-absolute fixtures %v",
		u.File, u.Line, u.Func, u.Literals)
}

// ExemptMarker opts one literal out of the scan. See "The opt-out".
const ExemptMarker = "//absfixture:unrouted"

// ScanUnroutedFixtures reports every gate-reaching test function in dir whose
// unix-absolute fixture literals are not routed through AbsFixture.
func ScanUnroutedFixtures(dir string) ([]UnroutedFixture, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []UnroutedFixture
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			return nil, fmt.Errorf("parse %s: %w", path, perr)
		}
		// A comment GROUP carrying the marker excuses the line right after the
		// group, and its own lines (for a marker trailing the literal). The
		// group rather than the single comment line, because the reason the
		// marker demands is prose and wraps — an earlier cut matched only the
		// marker line and the one below it, and so ignored the very exemption
		// it had just been given.
		exemptLines := map[int]bool{}
		for _, cg := range f.Comments {
			marked := false
			for _, c := range cg.List {
				if strings.HasPrefix(c.Text, ExemptMarker) {
					marked = true
				}
			}
			if !marked {
				continue
			}
			start := fset.Position(cg.Pos()).Line
			end := fset.Position(cg.End()).Line
			for ln := start; ln <= end+1; ln++ {
				exemptLines[ln] = true
			}
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil || !callsGate(fd) || routesTable(fd) {
				continue
			}
			lits := fixtureLiterals(fd, fset, exemptLines)
			if len(lits) == 0 {
				continue
			}
			sort.Strings(lits)
			out = append(out, UnroutedFixture{
				File: e.Name(), Func: fd.Name.Name,
				Line: fset.Position(fd.Pos()).Line, Literals: lits,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}

func callsGate(fd *ast.FuncDecl) bool { return hasCallTo(fd, gateFuncs) }

// routesTable reports whether fd routes a NON-LITERAL expression through
// AbsFixture — `AbsFixture(tc.exe)`, `AbsFixture(proc.Exe)`, `AbsFixture(d)`.
//
// Only that shape covers a whole table, because it runs per row: a row added
// later is routed without anyone remembering to. Routing a literal covers that
// literal and nothing else, and is handled by the per-literal check instead.
func routesTable(fd *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fd, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok || !isRouterCall(ce) || len(ce.Args) == 0 {
			return true
		}
		if _, isLit := ce.Args[0].(*ast.BasicLit); !isLit {
			found = true
		}
		return true
	})
	return found
}

func isRouterCall(ce *ast.CallExpr) bool {
	switch fn := ce.Fun.(type) {
	case *ast.Ident:
		return absFixtureRouters[fn.Name]
	case *ast.SelectorExpr:
		return absFixtureRouters[fn.Sel.Name]
	}
	return false
}

func hasCallTo(n ast.Node, names map[string]bool) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		ce, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := ce.Fun.(type) {
		case *ast.Ident:
			if names[fn.Name] {
				found = true
			}
		case *ast.SelectorExpr:
			if names[fn.Sel.Name] {
				found = true
			}
		}
		return true
	})
	return found
}

// fixtureLiterals returns the unix-absolute strings in fd that sit in a FIXTURE
// position — excluding /tmp-prefixed boundary fixtures, prose fields, and
// arguments to calls that take URLs rather than exec paths.
func fixtureLiterals(fd *ast.FuncDecl, fset *token.FileSet, exemptLines map[int]bool) []string {
	skip := map[token.Pos]bool{}
	ast.Inspect(fd, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.KeyValueExpr:
			if id, ok := x.Key.(*ast.Ident); ok && proseKeys[id.Name] {
				markStrings(x.Value, skip)
			}
		case *ast.CallExpr:
			// httptest.NewRequest(method, url, body) and friends take URL
			// paths, which are not exec paths.
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok && strings.HasPrefix(sel.Sel.Name, "New") {
				for _, a := range x.Args {
					markStrings(a, skip)
				}
			}
		case *ast.BinaryExpr:
			// Separator concatenation: d + "/" + b.
			markSeparator(x, skip)
		}
		return true
	})

	// Literals lexically inside a router call are routed, individually.
	ast.Inspect(fd, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok || !isRouterCall(ce) {
			return true
		}
		for _, a := range ce.Args {
			markStrings(a, skip)
		}
		return true
	})

	seen := map[string]bool{}
	var out []string
	ast.Inspect(fd, func(n ast.Node) bool {
		bl, ok := n.(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING || skip[bl.Pos()] {
			return true
		}
		v, err := strconv.Unquote(bl.Value)
		if err != nil || !strings.HasPrefix(v, "/") {
			return true
		}
		if strings.HasPrefix(v, "/tmp") || v == "/" {
			return true // boundary fixture, or a separator
		}
		if exemptLines[fset.Position(bl.Pos()).Line] {
			return true // explicitly excused; see ExemptMarker
		}
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
		return true
	})
	return out
}

func markStrings(e ast.Expr, skip map[token.Pos]bool) {
	ast.Inspect(e, func(n ast.Node) bool {
		if bl, ok := n.(*ast.BasicLit); ok && bl.Kind == token.STRING {
			skip[bl.Pos()] = true
		}
		return true
	})
}

func markSeparator(x *ast.BinaryExpr, skip map[token.Pos]bool) {
	for _, side := range []ast.Expr{x.X, x.Y} {
		if bl, ok := side.(*ast.BasicLit); ok && bl.Kind == token.STRING {
			if v, err := strconv.Unquote(bl.Value); err == nil && v == "/" {
				skip[bl.Pos()] = true
			}
		}
	}
}
