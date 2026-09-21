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

		// wantBare is the DIAGNOSIS each finding must carry, asserted for every
		// row rather than only where a test remembers to look.
		//
		// It exists because these rows graded their COUNT and not their REASON.
		// A row named "a BARE marker ... does not excuse it" asserted only that
		// the site was reported — which it is either way, marked or not — so
		// suppressing DirectKill.BareMarker for TRAILING markers alone was
		// ALIVE: two of the three marker positions the scanner accepts had
		// their diagnosis ungraded, and the only test that looked at the flag
		// exercised the third. Under that mutant an author writing a bare
		// trailing marker is told to "mark the line" on a line already carrying
		// the marker, which is the misdirection round 3 added the flag to
		// prevent.
		//
		// A field on the table rather than more rows in the diagnosis test, so
		// the assertion matches the row's NAME for every row present and
		// future, instead of closing the two shapes named in review and leaving
		// the next one to be found the same way. The zero value is the common
		// case and asserts the useful negative: a site with no honoured-marker
		// story must NOT claim one.
		wantBare bool
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
			name: "PLANTED VIOLATION — a BLOCK comment cannot carry the marker",
			src: `package p
` + procImport + `
func f(pid int) error {
	/*killguard:direct a block comment is not a path*/
	return process.Kill(pid)
}`,
			wantHits: 1,
			why: "killguardscan.go states this as a property of HasPrefix rather than as a decision; " +
				"a row makes the claim observed instead of asserted, and pins that widening the " +
				"marker match to block comments is a deliberate change",
		},
		{
			name: "PLANTED VIOLATION — a BARE marker above the reference does not excuse it",
			src: `package p
` + procImport + `
func f(pid int) error {
	//killguard:direct
	return process.Kill(pid)
}`,
			wantHits: 1,
			wantBare: true,
			why: "the marker's whole advantage over a file allowlist is the FORCED justification. " +
				"Honouring a bare one makes it a line-scoped allowlist and makes this package's " +
				"own design section a claim no test can kill — the repo's dominant defect class",
		},
		{
			name: "PLANTED VIOLATION — a BARE marker TRAILING the reference does not excuse it either",
			src: `package p
` + procImport + `
func f(pid int) error { return process.Kill(pid) } //killguard:direct`,
			wantHits: 1,
			wantBare: true,
			why: "the scanner accepts the marker in three positions, so the rule has to behave " +
				"identically in all three; a trailing marker simply has no continuation line to " +
				"put the reason on",
		},
		{
			name: "PLANTED VIOLATION — whitespace after the marker is not a reason",
			src: `package p
` + procImport + `
func f(pid int) error {
	//killguard:direct   ` + `
	return process.Kill(pid)
}`,
			wantHits: 1,
			wantBare: true,
			why: "the cheapest way to defeat a naive `len(rest) > 0` check, and gofmt would strip it " +
				"from the file leaving a bare marker that had once passed review",
		},
		{
			name: "the reason may WRAP onto a continuation line with the marker line otherwise bare",
			src: `package p
` + procImport + `
func f(pid int) error {
	//killguard:direct
	// this site kills a child the caller spawned itself.
	return process.Kill(pid)
}`,
			wantHits: 0,
			why: "a rule demanding the reason on the marker's OWN line would reject ordinary " +
				"wrapping, which is how reaper.go's real justification is written",
		},
		{
			name: "PLANTED VIOLATION — a comment line BELOW a TRAILING marker is not its reason",
			src: `package p
` + procImport + `
func f(pid int) error { return process.Kill(pid) } //killguard:direct
// the reason wraps onto this line`,
			wantHits: 1,
			wantBare: true,
			why: "THE LOAD-BEARING INPUT IS go/parser, NOT THIS PACKAGE. killguardscan.go says a " +
				"trailing marker \"has no continuation lines available\", and that is true only " +
				"because go/parser cuts a comment group that begins on the same line as a " +
				"preceding token with a zero-line lookahead: the trailing marker and the `//` " +
				"line beneath it land in SEPARATE CommentGroups, so cg.List[idx:] holds the " +
				"marker alone. Upstream behaviour this repo does not control — which makes it " +
				"worth MORE pinning than a property of our own HasPrefix, not less. Letting the " +
				"reason continue into the next group when it starts at end+1 is a plausible " +
				"\"let the reason wrap\" change, it is the accommodation this code already makes " +
				"WITHIN a group, and it flips this row from 1 hit to 0 — silently restoring the " +
				"bare-marker hole for every marker trailing a line that happens to be followed " +
				"by a comment",
		},
		{
			name: "PLANTED VIOLATION — prose ABOVE the marker is not the reason",
			src: `package p
` + procImport + `
func f(pid int) error {
	// This function terminates a process.
	//killguard:direct
	return process.Kill(pid)
}`,
			wantHits: 1,
			wantBare: true,
			why: "text before the marker explains the CODE, not the exemption. Counting it would " +
				"let a bare marker dropped under any existing comment pass, which is most of them",
		},
		{
			name: "PLANTED VIOLATION — an empty continuation comment is not a reason",
			src: `package p
` + procImport + `
func f(pid int) error {
	//killguard:direct
	//
	return process.Kill(pid)
}`,
			wantHits: 1,
			wantBare: true,
			why: "`//` on its own is the blank line of a comment group; a check counting COMMENTS " +
				"rather than TEXT would accept it",
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
			// Every finding, which today is EQUIVALENT to checking got[0] and
			// is disclosed as such: no row yields two findings whose diagnosis
			// could differ, so narrowing this loop to got[0] is ALIVE. It is a
			// scalar field, so a row cannot even encode disagreement.
			//
			// Kept because it costs nothing and becomes load-bearing the moment
			// a multi-finding row is added — but the PROPERTY it gestures at,
			// that each site is diagnosed on its own line rather than per file,
			// is graded for real by TestFindDirectKillsDiagnosesEachSiteIndependently
			// below, which this loop cannot express.
			for i, g := range got {
				if g.BareMarker != tc.wantBare {
					t.Errorf("finding %d BareMarker = %v, want %v (%s)\n  %s",
						i, g.BareMarker, tc.wantBare, tc.why, g)
				}
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

// TestFindDirectKillsDiagnosesEachSiteIndependently pins that the BareMarker
// diagnosis is keyed to the SITE'S OWN LINE, not to the file.
//
// The table above cannot express this: wantBare is one bool per row, so a
// fixture holding one marked and one unmarked site has no way to say that the
// findings must differ. That gap is not hypothetical — it is why the table's
// "every finding" loop is equivalent to checking got[0], stated there rather
// than papered over.
//
// The hazard it closes is real rather than invented: the flag is carried in a
// map keyed by LINE, and an implementation that keyed it per FILE — or that
// spread one group's verdict across the whole file — would pass every row in
// the table, because no row mixes the two diagnoses.
func TestFindDirectKillsDiagnosesEachSiteIndependently(t *testing.T) {
	got := scanKills(t, `package p
`+procImport+`
func unmarked() { reap(deps{kill: process.Kill}) }
func bareMarked(pid int) error {
	//killguard:direct
	return process.Kill(pid)
}`)
	if len(got) != 2 {
		t.Fatalf("got %d findings, want 2 (one unmarked site, one bare-marked): %v", len(got), got)
	}
	byFn := map[string]testsupport.DirectKill{}
	for _, g := range got {
		byFn[g.Fn] = g
	}
	if g, ok := byFn["unmarked"]; !ok {
		t.Errorf("no finding for the unmarked site; got %v", got)
	} else if g.BareMarker {
		t.Errorf("the UNMARKED site is diagnosed as carrying a bare marker — the flag is "+
			"leaking across sites, so an author who marks one line gets the wrong message on "+
			"another: %s", g)
	}
	if g, ok := byFn["bareMarked"]; !ok {
		t.Errorf("no finding for the bare-marked site; got %v", got)
	} else if !g.BareMarker {
		t.Errorf("the BARE-MARKED site is not diagnosed as such even though an unmarked site "+
			"sits in the same file: %s", g)
	}
}

// TestFindDirectKillsBareMarkerDiagnosis pins that a bare marker is reported
// DIFFERENTLY from an unmarked site.
//
// Without this the author of a bare marker is told to "mark the line" on a line
// they already marked, which reads as a broken guard rather than as the rule
// being enforced — and the likeliest response to that is to delete the guard.
// An enforcement whose diagnostic misdirects is worse than no enforcement.
func TestFindDirectKillsBareMarkerDiagnosis(t *testing.T) {
	bare := scanKills(t, `package p
`+procImport+`
func f(pid int) error {
	//killguard:direct
	return process.Kill(pid)
}`)
	if len(bare) != 1 {
		t.Fatalf("got %d findings for a bare marker, want 1: %v", len(bare), bare)
	}
	if !bare[0].BareMarker {
		t.Error("BareMarker is false for a site whose marker carries no reason")
	}
	if msg := bare[0].String(); !strings.Contains(msg, "BARE") || !strings.Contains(msg, "reason") {
		t.Errorf("bare-marker finding %q does not say the marker was not honoured, and why", msg)
	}

	// Control: an UNMARKED site must NOT claim a bare marker, or the flag says
	// nothing and the two diagnostics collapse back into one.
	plain := scanKills(t, `package p
`+procImport+`
func f(pid int) error { return process.Kill(pid) }`)
	if len(plain) != 1 {
		t.Fatalf("got %d findings for an unmarked site, want 1: %v", len(plain), plain)
	}
	if plain[0].BareMarker {
		t.Error("BareMarker is true for a site carrying no marker at all")
	}
	if msg := plain[0].String(); strings.Contains(msg, "BARE") {
		t.Errorf("unmarked finding %q is reported as a bare marker", msg)
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
