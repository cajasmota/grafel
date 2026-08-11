package testsupport

// homescan_test.go — the guard's own guard.
//
// A scan is vacuously green the moment the detector stops binding, so the
// detector is exercised against synthetic source with known answers rather than
// trusted because the tree is currently clean. Moved here from
// internal/install/home_isolation_guard_6171_test.go with #6288, along with the
// code it tests; the rows below the #6171 ones are the widening.
//
// The MISS rows assert what the detector does NOT see (want: 0). The FALSE
// POSITIVE rows assert what it sees and should not (want: 1 on isolated code).
// Neither group is an aspiration — both are the stated edge of its reach,
// confirmed by running it. If a change fixes one, flip its want and delete the
// matching sentence in homescan.go's doc; do not delete the row.
//
// Keep the two groups apart. #6290 found a false positive filed under the MISS
// header with want: 1, which reads as a contradiction and hides a real
// wrong-answer behind a heading that promises the opposite.

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/homeguard"
	"testing"
)

func TestFindUnisolatedHomeTests(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		// ── #6171's original rows, unchanged in meaning ──────────────────
		{
			name: "HOME redirected with nothing else pinned",
			src: `package p
import "testing"
func TestX(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home+"/.config")
}`,
			want: 1,
		},
		{
			name: "IsolateHome is isolated even alongside a HOME redirect",
			src: `package p
import "testing"
func TestX(t *testing.T) {
	home := testsupport.IsolateHome(t)
	t.Setenv("HOME", home)
}`,
			want: 0,
		},
		{
			name: "GRAFEL_DAEMON_ROOT alone does NOT count (see #6134)",
			src: `package p
import "testing"
func TestX(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRAFEL_DAEMON_ROOT", home+"/.grafel")
}`,
			want: 1,
		},
		{
			name: "a non-test helper that redirects HOME is caught too",
			src: `package p
import "testing"
func newEnv(t *testing.T) string {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("GRAFEL_HOME", home+"/.grafel")
	return home
}
func newBadEnv(t *testing.T) string {
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}`,
			want: 1,
		},
		{
			name: "os.Setenv is caught, not just t.Setenv",
			src: `package p
import "os"
func setup() { os.Setenv("HOME", "/tmp/x") }`,
			want: 1,
		},
		{
			name: "a function that touches neither is clean",
			src: `package p
import "testing"
func TestX(t *testing.T) { _ = t.TempDir() }`,
			want: 0,
		},
		{
			name: "two offenders in one file are both named",
			src: `package p
import "testing"
func TestA(t *testing.T) { t.Setenv("HOME", "/tmp/a") }
func TestB(t *testing.T) { t.Setenv("HOME", "/tmp/b") }`,
			want: 2,
		},

		// ── #6288's widening ─────────────────────────────────────────────
		{
			// The #6288 shape exactly: internal/mcp's newDocgenServer. Under
			// #6171's rule (GRAFEL_HOME only) this was a candidate; under the
			// widened rule it still is, and the report now also names the
			// Windows half.
			name: "HOME plus nothing: both pins missing",
			src: `package p
import "testing"
func TestX(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
}`,
			want: 1,
		},
		{
			// This row is the widening. Under #6171's rule this was CLEAN,
			// because GRAFEL_HOME is pinned. On Windows os.UserHomeDir() reads
			// %USERPROFILE% and ignores $HOME, so every path in the tree that
			// resolves through os.UserHomeDir() rather than registry.HomeDir()
			// — persona_telemetry.go, activity_log.go, mcpreg.SettingsPath —
			// still lands in the real profile.
			name: "GRAFEL_HOME pinned but USERPROFILE is not: caught since #6288",
			src: `package p
import "testing"
func TestX(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRAFEL_HOME", home+"/.grafel")
}`,
			want: 1,
		},
		{
			name: "USERPROFILE pinned but GRAFEL_HOME is not: still caught (#6171)",
			src: `package p
import "testing"
func TestX(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}`,
			want: 1,
		},
		{
			name: "both pinned is clean",
			src: `package p
import "testing"
func TestX(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("GRAFEL_HOME", home+"/.grafel")
}`,
			want: 0,
		},

		// ── CONFIRMED MISSES (the detector does NOT see these) ───────────
		{
			name: "MISS: the env-var name behind an identifier, not a literal",
			src: `package p
import "testing"
const homeEnv = "HOME"
func TestX(t *testing.T) { t.Setenv(homeEnv, "/tmp/x") }`,
			want: 0,
		},

		// ── KNOWN FALSE POSITIVES (the detector DOES see these, wrongly) ──
		{
			// #6290 item 4: this was filed under "CONFIRMED MISSES" with
			// want: 1, which contradicts that header — a MISS is want: 0. The
			// composed usage below is fully isolated; the detector reports
			// setHome because it reasons one function at a time and cannot see
			// the pins in the caller. It is a FALSE POSITIVE, and the cost of it
			// is a contributor being told to pin something already pinned.
			// Closing it needs intra-package call-graph resolution, which is the
			// same work the blind spot needs and is not attempted here.
			name: "FALSE POSITIVE: HOME set in one function, the pins in its caller",
			src: `package p
import "testing"
func setHome(t *testing.T) { t.Setenv("HOME", "/tmp/x") }
func TestX(t *testing.T) {
	t.Setenv("GRAFEL_HOME", "/tmp/x/.grafel")
	t.Setenv("USERPROFILE", "/tmp/x")
	setHome(t)
}`,
			want: 1,
		},
		{
			// THE important miss, not a curiosity: this is what "someone
			// deletes an IsolateHome call" actually looks like, and it is the
			// likeliest future regression. The detector is keyed on the
			// presence of a HOME redirect, so a function that isolates nothing
			// at all is invisible to it.
			name: "MISS: IsolateHome deleted with no replacement Setenv",
			src: `package p
import "testing"
func TestX(t *testing.T) {
	home := t.TempDir()
	_ = home
}`,
			want: 0,
		},
		{
			name: "MISS: a var-bound top-level closure is not a FuncDecl",
			src: `package p
import "testing"
var f = func(t *testing.T) { t.Setenv("HOME", "/tmp/x") }`,
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "x_test.go", tc.src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := len(FindUnisolatedHomeTests(fset, f, "x_test.go")); got != tc.want {
				t.Fatalf("detector found %d offender(s), want %d", got, tc.want)
			}
		})
	}
}

// TestFindUnisolatedHomeTests_NamesWhatIsMissing pins the report content, not
// just the count. A reader who is told only "not isolated" has to rediscover
// which of the two reasons applies, and the Windows one is the non-obvious half.
func TestFindUnisolatedHomeTests_NamesWhatIsMissing(t *testing.T) {
	src := `package p
import "testing"
func TestX(t *testing.T) {
	t.Setenv("HOME", "/tmp/x")
	t.Setenv("GRAFEL_HOME", "/tmp/x/.grafel")
}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x_test.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := FindUnisolatedHomeTests(fset, f, "x_test.go")
	if len(got) != 1 {
		t.Fatalf("found %d offenders, want 1", len(got))
	}
	if len(got[0].Missing) != 1 || got[0].Missing[0] != envUserProfile {
		t.Fatalf("Missing = %v, want exactly [%s]", got[0].Missing, envUserProfile)
	}
	if !strings.Contains(got[0].String(), envUserProfile) || !strings.Contains(got[0].String(), "TestX") {
		t.Fatalf("String() = %q, want it to name the function and the missing var", got[0].String())
	}
}

// TestScanUnisolatedHomeTests_IsNotVacuous: an empty or unreadable directory
// must be reported as a failure, not as a clean bill of health. A scan that
// reads nothing is the exact vacuous-gate shape this whole mechanism exists to
// prevent.
func TestScanUnisolatedHomeTests_IsNotVacuous(t *testing.T) {
	var sb strings.Builder
	if n := ReportUnisolatedHomeTests(&sb, t.TempDir(), "probe/"); n == 0 {
		t.Fatal("a directory with no _test.go files reported clean; it proves nothing and must say so")
	}
	if !strings.Contains(sb.String(), "not binding") {
		t.Errorf("report did not explain the vacuous scan: %q", sb.String())
	}

	sb.Reset()
	if n := ReportUnisolatedHomeTests(&sb, "/no/such/dir/anywhere", "probe/"); n == 0 {
		t.Fatal("an unreadable directory reported clean")
	}
	if !strings.Contains(sb.String(), "failed to scan") {
		t.Errorf("report did not explain the scan error: %q", sb.String())
	}
}

// TestReportUnisolatedHomeTests_NamesTheRemediation: the report is read by
// whoever broke the build, once, under time pressure. It has to carry the fix.
func TestReportUnisolatedHomeTests_NamesTheRemediation(t *testing.T) {
	dir := t.TempDir()
	writeTempTestFile(t, dir, "offender_test.go", `package p
import "testing"
func TestX(t *testing.T) { t.Setenv("HOME", "/tmp/x") }`)

	var sb strings.Builder
	if n := ReportUnisolatedHomeTests(&sb, dir, "probe/"); n != 1 {
		t.Fatalf("reported %d offenders, want 1", n)
	}
	for _, want := range []string{"IsolateHome", "USERPROFILE", "GRAFEL_HOME", "TestX", "#6288"} {
		if !strings.Contains(sb.String(), want) {
			t.Errorf("report is missing %q:\n%s", want, sb.String())
		}
	}
}

// writeTempTestFile writes a synthetic _test.go file for the scanner to read.
// It writes into t.TempDir(), never into a package directory.
func writeTempTestFile(t *testing.T, dir, name, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// TestAssertUnderHome_UsesTheSharedContainmentDecision is #6290 BLOCKER 1's
// second half. AssertUnderHome held a byte-for-byte copy of the construction
// homeguard was carrying, so it inherited the same two fail-open defects — and
// #6288 then used it as the assertion that docgen's promote cannot escape the
// sandbox, which is precisely the job that construction was too weak for.
//
// The property asserted here is delegation, driven through AssertUnderHome
// ITSELF rather than through homeguard.Escapes — an earlier draft called Escapes
// directly, which tests homeguard and says nothing about whether AssertUnderHome
// still carries its own copy.
//
// The discriminating shape is a ROOT home, chosen because it works on every
// platform: the old local copy compared against home+sep, which for a root home
// is "//" or "C:\\", so it answered "escapes" for every path underneath. The
// delegation answers "contained". No failure harness is needed — AssertUnderHome
// reports by t.Fatalf, so the assertion here is simply that it does not.
func TestAssertUnderHome_RootHomeContainsItsChildren(t *testing.T) {
	root := string(filepath.Separator)
	if vol := filepath.VolumeName(mustAbsForTest(t, root)); vol != "" {
		root = vol + string(filepath.Separator)
	}
	// The real user home is never the root on any machine this runs on, so the
	// second (refuse-the-real-home) check cannot mask the first.
	if homeguard.Escapes(realUserHome, root) && realUserHome == root {
		t.Skip("the real user home IS the filesystem root here; this shape cannot discriminate")
	}
	t.Setenv(envHome, root)
	t.Setenv(envUserProfile, root)

	AssertUnderHome(t, filepath.Join(root, "foo", "bar"))
}

// TestAssertUnderHome_AcceptsTheIsolatedHome is the ordinary path, kept so the
// delegation cannot regress into something that rejects everything.
func TestAssertUnderHome_AcceptsTheIsolatedHome(t *testing.T) {
	home := IsolateHome(t)
	AssertUnderHome(t, filepath.Join(home, ".grafel", "docs", "g"))
}

func mustAbsForTest(t *testing.T, p string) string {
	t.Helper()
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatalf("abs(%q): %v", p, err)
	}
	return a
}
