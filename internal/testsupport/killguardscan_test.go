package testsupport_test

// killguardscan_test.go — the detector's own table, including PLANTED
// VIOLATIONS THAT MUST FIRE, and the cases that must NOT.
//
// A scan-and-assert-absence guard has five independent ways to be a silent
// no-op: it reads nothing, it reads the wrong files, it reads the right files
// but not their content, it detects nothing, or it detects and fails to act.
// "It returns zero findings on a clean tree" distinguishes none of them. Every
// case below therefore states which way the detector must go, and the
// violations are the load-bearing half.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/testsupport"
)

func scanKills(t *testing.T, src string) []testsupport.DirectKill {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return testsupport.FindDirectKills(fset, f, "x.go")
}

const procImport = `import "github.com/cajasmota/grafel/internal/process"`

func TestFindDirectKills(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		wantHits int
		why      string
	}{
		{
			name: "PLANTED VIOLATION — the #7280 shape: Kill wired into a struct literal",
			src: `package p
` + procImport + `
func start() {
	reap(deps{kill: process.Kill})
}`,
			wantHits: 1,
			why: "verbatim both #7280 sites. A detector keyed on CallExpr finds NEITHER — " +
				"this is the shape the guard exists for, not a proxy for it",
		},
		{
			name: "PLANTED VIOLATION — a direct call",
			src: `package p
` + procImport + `
func f(pid int) error { return process.Kill(pid) }`,
			wantHits: 1,
		},
		{
			name: "PLANTED VIOLATION — a package-level var has no enclosing function and is still a site",
			src: `package p
` + procImport + `
var killProc = process.Kill`,
			wantHits: 1,
			why:      "this is exactly the shape #7268 chose at the two doctor sites; it must be graded too",
		},
		{
			name: "PLANTED VIOLATION — an import ALIAS does not hide it",
			src: `package p
import proc "github.com/cajasmota/grafel/internal/process"
func f(pid int) error { return proc.Kill(pid) }`,
			wantHits: 1,
			why:      "a scanner keyed on the literal text \"process.Kill\" is defeated by one rename",
		},
		{
			name: "PLANTED VIOLATION — a dot-import makes Kill a bare identifier",
			src: `package p
import . "github.com/cajasmota/grafel/internal/process"
func f(pid int) error { return Kill(pid) }`,
			wantHits: 1,
		},
		{
			name: "PLANTED VIOLATION — assigning the function value, not calling it",
			src: `package p
` + procImport + `
func f() { k := process.Kill; _ = k }`,
			wantHits: 1,
			why:      "the hazard is the VALUE reaching a call site later, not the call here",
		},
		{
			name: "KillGuarded is the fix and must be silent",
			src: `package p
` + procImport + `
func start() { reap(deps{kill: process.KillGuarded}) }`,
			wantHits: 0,
			why:      "routing is what silences the guard — this is the whole feedback loop",
		},
		{
			name: "os.Process.Kill on a child the caller spawned is not this package's Kill",
			src: `package p
import "os/exec"
func f(cmd *exec.Cmd) { _ = cmd.Process.Kill() }`,
			wantHits: 0,
			why: "the import is resolved, not the identifier text; spawnLiveChild's cleanup does " +
				"exactly this and must never be reported",
		},
		{
			name: "a file that does not import internal/process is not inspected",
			src: `package p
type process struct{}
func (process) Kill(int) error { return nil }
func f(process process) { _ = process.Kill(1) }`,
			wantHits: 0,
		},
		{
			name: "a blank import cannot reference anything",
			src: `package p
import _ "github.com/cajasmota/grafel/internal/process"
type t struct{ process struct{ Kill func(int) error } }
func f(v t) { _ = v.process.Kill }`,
			wantHits: 0,
		},
		{
			name: "the marker above the reference excuses it, with the reason wrapping",
			src: `package p
` + procImport + `
func sigtermPID(pid int) error {
	// The pids reaching here are children the test spawned itself.
	//
	//killguard:direct guarding this breaks a legitimate test that kills its own
	// child, and the reason wraps over several lines the way a real
	// justification does.
	return process.Kill(pid)
}`,
			wantHits: 0,
			why:      "absfixturescan's first cut matched only the marker line and the one below it",
		},
		{
			name: "the marker trailing the reference's own line excuses it",
			src: `package p
` + procImport + `
func f(pid int) error { return process.Kill(pid) } //killguard:direct deliberate`,
			wantHits: 0,
		},
		{
			name: "PLANTED VIOLATION — the marker excuses ONE line, not the function",
			src: `package p
` + procImport + `
func f(pid int) error {
	//killguard:direct only this one
	_ = process.Kill
	return process.Kill(pid)
}`,
			wantHits: 1,
			why: "a function- or file-scoped opt-out would retire every LATER line added beside " +
				"the one that earned it — which is precisely why this is a marker and not an allowlist",
		},
		{
			name: "PLANTED VIOLATION — a second site in the same file is its own finding",
			src: `package p
` + procImport + `
func a() { reap(deps{kill: process.Kill}) }
func b() { reap(deps{kill: process.Kill}) }`,
			wantHits: 2,
			why:      "#7280 is two sites in two files; a file-granular report would collapse them",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scanKills(t, tc.src)
			if len(got) != tc.wantHits {
				t.Fatalf("got %d findings, want %d (%s)\nfindings: %v", len(got), tc.wantHits, tc.why, got)
			}
		})
	}
}

// TestFindDirectKillsReportsWhereAndWhat pins that a finding is actionable. A
// guard that fires without naming the file, the line, the function and the fix
// costs more than it saves.
func TestFindDirectKillsReportsWhereAndWhat(t *testing.T) {
	got := scanKills(t, `package p
`+procImport+`
func (s *engineSupervisor) start() {
	reapStaleEngine(reapStaleEngineDeps{kill: process.Kill})
}`)
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1: %v", len(got), got)
	}
	if got[0].Key() != "x.go:engineSupervisor.start" {
		t.Errorf("Key() = %q, want %q", got[0].Key(), "x.go:engineSupervisor.start")
	}
	if got[0].Line == 0 {
		t.Error("finding has no line number")
	}
	s := got[0].String()
	for _, want := range []string{"x.go", "engineSupervisor.start", "process.Kill", "KillGuarded", testsupport.KillGuardMarker} {
		if !strings.Contains(s, want) {
			t.Errorf("finding %q does not name %q", s, want)
		}
	}
}

// TestFindDirectKillsNeedsParsedComments pins a trap that would make the
// marker silently inert: go/parser drops comments unless ParseComments is set,
// so a caller that omits it gets an exemption that never applies.
//
// This is asserted rather than documented because the repo-wide sweep is the
// caller, and the sibling sweep it was modelled on (#6478's) parses with
// SkipObjectResolution ALONE. Copying that call would have been the natural
// mistake, and the failure direction is loud rather than silent — but it would
// have been diagnosed as "the marker does not work" rather than as a parse flag.
func TestFindDirectKillsNeedsParsedComments(t *testing.T) {
	const src = `package p
` + procImport + `
func f(pid int) error {
	//killguard:direct deliberate
	return process.Kill(pid)
}`
	scan := func(mode parser.Mode) int {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "x.go", src, mode)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		return len(testsupport.FindDirectKills(fset, f, "x.go"))
	}
	if n := scan(parser.ParseComments | parser.SkipObjectResolution); n != 0 {
		t.Errorf("with ParseComments the marker must exempt the line; got %d findings", n)
	}
	if n := scan(parser.SkipObjectResolution); n != 1 {
		t.Errorf("without ParseComments the marker CANNOT be seen, so the site must still be "+
			"reported; got %d findings — if this is now 0 the scanner has stopped detecting", n)
	}
}

// TestFindDirectKillsHandlesNilBody pins that a declaration with no body (an
// assembly or cgo stub) does not panic the scanner.
func TestFindDirectKillsHandlesNilBody(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", `package p
`+procImport+`
func stub(pid int) error`, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var _ *ast.File = f
	if got := testsupport.FindDirectKills(fset, f, "x.go"); len(got) != 0 {
		t.Fatalf("got %v, want none", got)
	}
}
