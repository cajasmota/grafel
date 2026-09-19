package testsupport_test

// absfixturescan_test.go — the detector's own table, including PLANTED
// VIOLATIONS THAT MUST FIRE.
//
// A scan-and-assert-absence guard has several independent ways to be a silent
// no-op: read no files, read the wrong files, parse nothing, detect nothing, or
// report nothing. "It returns zero findings on a clean tree" distinguishes none
// of them. Every case below therefore states which way the detector must go,
// and the violations are the load-bearing half.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/testsupport"
)

func scanSrc(t *testing.T, src string) []testsupport.UnroutedFixture {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x_test.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := testsupport.ScanUnroutedFixtures(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	return got
}

func TestScanUnroutedFixtures_Detects(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		wantHits int
		why      string
	}{
		{
			name: "PLANTED VIOLATION — the #7281 shape: a table that routes nothing",
			src: `package p
func TestX(t *testing.T) {
	p := staleProcess{Exe: "/usr/local/lib/grafel/daemon/grafel"}
	_ = isStaleProc(p, "/usr/local/bin/grafel")
}`,
			wantHits: 1,
			why:      "this is verbatim the shape the windows leg caught; if it does not fire, the guard is decorative",
		},
		{
			name: "routed table is clean",
			src: `package p
func TestX(t *testing.T) {
	p := staleProcess{Exe: testsupport.AbsFixture("/usr/local/lib/grafel/daemon/grafel")}
	_ = isStaleProc(p, testsupport.AbsFixture("/usr/local/bin/grafel"))
}`,
			wantHits: 0,
		},
		{
			name: "routed in the LOOP, literals bare — the dominant real shape",
			src: `package p
func TestX(t *testing.T) {
	cases := []struct{ exe string }{{"/opt/grafel/daemon/bin/grafel"}}
	for _, tc := range cases {
		_ = isStaleProc(staleProcess{Exe: testsupport.AbsFixture(tc.exe)}, "/x/grafel")
	}
}`,
			wantHits: 0,
			why:      "three of the four real tables store bare literals and route once in the loop",
		},
		{
			name: "/tmp-prefixed fixtures are exempt and must NOT be reported",
			src: `package p
func TestX(t *testing.T) {
	_ = isStaleProc(staleProcess{Exe: "/tmp/agent-worktree/grafel"}, "/tmp/x")
}`,
			wantHits: 0,
			why:      "routing them would move them off the /tmp boundary they exist to test",
		},
		{
			name: "/tmp SIBLINGS are exempt too",
			src: `package p
func TestX(t *testing.T) {
	_ = isStaleProc(staleProcess{Exe: "/tmpfoo/grafel"}, "/tmpdir/grafel")
}`,
			wantHits: 0,
		},
		{
			name: "a function that never reaches the gate is not this guard's business",
			src: `package p
func TestX(t *testing.T) {
	_ = filepath.Join("/usr/local/lib/grafel/daemon/grafel")
}`,
			wantHits: 0,
		},
		{
			name: "prose in a keyed name/why field is not a fixture",
			src: `package p
func TestX(t *testing.T) {
	c := struct{ name, why string }{name: "/usr/local is where it lives", why: "/opt reasons"}
	_ = c
	_ = isStaleProc(staleProcess{}, "x")
}`,
			wantHits: 0,
		},
		{
			name: "a URL argument is not an exec path",
			src: `package p
func TestX(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/diagnostics/kill-stale", nil)
	_ = req
	_ = handleDiagnosticsKillStale(nil, nil)
}`,
			wantHits: 0,
		},
		{
			name: "a bare separator in d + \"/\" + b is not a fixture",
			src: `package p
func TestX(t *testing.T) {
	for _, d := range dirs {
		_ = isStaleProc(staleProcess{Exe: d + "/" + b}, "x")
	}
}`,
			wantHits: 0,
		},
		{
			name: "the opt-out marker excuses a literal, with its reason wrapping over lines",
			src: `package p
func TestX(t *testing.T) {
	// Absolute on unix only.
	//
	//absfixture:unrouted this fixture must stay volume-less because the test
	// asserts windows rejects it, and the reason wraps onto several lines the
	// way a real justification does.
	const unixOnlyAbs = "/usr/local/bin/grafel"
	_ = isStaleProc(staleProcess{Exe: unixOnlyAbs}, "x")
}`,
			wantHits: 0,
			why:      "an earlier cut matched only the marker line and the one below, ignoring the exemption",
		},
		{
			name: "the marker trailing the literal's own line also excuses it",
			src: `package p
func TestX(t *testing.T) {
	const a = "/usr/local/bin/grafel" //absfixture:unrouted deliberate
	_ = isStaleProc(staleProcess{Exe: a}, "x")
}`,
			wantHits: 0,
		},
		{
			name: "PLANTED VIOLATION — the marker excuses ONE literal, not the function",
			src: `package p
func TestX(t *testing.T) {
	//absfixture:unrouted only this one
	const a = "/usr/local/bin/grafel"
	const b = "/opt/grafel/daemon/bin/grafel"
	_ = isStaleProc(staleProcess{Exe: a}, b)
}`,
			wantHits: 1,
			why:      "a blanket opt-out would retire the whole table the moment one row needed excusing",
		},
		{
			name: "PLANTED VIOLATION — routing selfExe does NOT cover the table's own rows",
			src: `package p
func TestX(t *testing.T) {
	selfExe := testsupport.AbsFixture("/usr/local/bin/grafel")
	cases := []struct{ proc staleProcess }{
		{staleProcess{Exe: "/usr/local/lib/grafel/daemon/grafel"}},
	}
	for _, tc := range cases {
		_ = isStaleProc(tc.proc, selfExe)
	}
}`,
			wantHits: 1,
			why:      "this is exactly the mutant that was ALIVE: routing a LITERAL covers that literal, not the rows",
		},
		{
			name: "PLANTED VIOLATION — a second unrouted function in the same file is its own site",
			src: `package p
func TestRouted(t *testing.T) {
	_ = isStaleProc(staleProcess{Exe: testsupport.AbsFixture("/opt/grafel/bin/grafel")}, "x")
}
func TestUnrouted(t *testing.T) {
	_ = isStaleProc(staleProcess{Exe: "/opt/grafel/bin/grafel"}, "x")
}`,
			wantHits: 1,
			why:      "the unit is a FUNCTION, not a file — three real files hold one routed and one unrouted table",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scanSrc(t, tc.src)
			if len(got) != tc.wantHits {
				t.Fatalf("got %d findings, want %d (%s)\nfindings: %v", len(got), tc.wantHits, tc.why, got)
			}
		})
	}
}

// TestScanUnroutedFixtures_ReportsWhereAndWhat pins that a finding is
// actionable. A guard that fires without naming the function and the literals
// costs more than it saves.
func TestScanUnroutedFixtures_ReportsWhereAndWhat(t *testing.T) {
	got := scanSrc(t, `package p
func TestTheBadOne(t *testing.T) {
	_ = isStaleProc(staleProcess{Exe: "/usr/local/lib/grafel/daemon/grafel"}, "x")
}`)
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1", len(got))
	}
	s := got[0].String()
	for _, want := range []string{"TestTheBadOne", "/usr/local/lib/grafel/daemon/grafel", "x_test.go"} {
		if !strings.Contains(s, want) {
			t.Errorf("finding %q does not name %q", s, want)
		}
	}
	if got[0].Line == 0 {
		t.Error("finding has no line number")
	}
}

// TestScanUnroutedFixtures_ReadsOnlyTestFiles pins the file selector: a scan
// that read non-test files would report production code, and one that read
// nothing would report clean forever.
func TestScanUnroutedFixtures_ReadsOnlyTestFiles(t *testing.T) {
	dir := t.TempDir()
	bad := `package p
func TestX(t *testing.T) {
	_ = isStaleProc(staleProcess{Exe: "/usr/local/lib/grafel/daemon/grafel"}, "x")
}`
	if err := os.WriteFile(filepath.Join(dir, "notatest.go"), []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := testsupport.ScanUnroutedFixtures(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("scanned a non-_test.go file: %v", got)
	}

	// Control: the identical content in a _test.go IS reported, so the zero
	// above is the file selector talking and not a dead scanner.
	if err := os.WriteFile(filepath.Join(dir, "y_test.go"), []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = testsupport.ScanUnroutedFixtures(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("control failed: got %d findings for a _test.go with the same content, want 1", len(got))
	}
}

// TestScanUnroutedFixtures_SurfacesParseErrors pins that a file it cannot parse
// is an ERROR, not a silent skip — the difference between a guard and a guard
// that stopped working.
func TestScanUnroutedFixtures_SurfacesParseErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken_test.go"), []byte("package p\nfunc ("), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := testsupport.ScanUnroutedFixtures(dir); err == nil {
		t.Fatal("an unparseable _test.go was skipped silently")
	}
}
