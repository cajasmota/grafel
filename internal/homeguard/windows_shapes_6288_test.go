package homeguard

// windows_shapes_6288_test.go — the Windows half of the containment decision,
// exercised on EVERY host.
//
// #6288: three TestEscapes rows failed on windows-latest and every one of them
// failed OPEN — Escapes said "does not escape", i.e. "this write is allowed",
// for a path that was the home itself. A guard that answers "allowed" for the
// home directory is not a guard.
//
// The three reported rows were the ROOTLESS-PATH bug (see
// TestContains_AbsolutionIsSymmetric): the fixture builds "\home\dev", which is
// rooted-but-volumeless on Windows, and the old Escapes ran filepath.Abs on
// `resolved` but NOT on `home`. Abs stamped the current volume onto one side
// only — "C:\home\dev" was compared against "\home\dev" and missed. That is a
// defect in the GUARD, not in the fixture: an asymmetric normalisation means
// the two arguments are not being compared in the same coordinate system, and
// no caller can be expected to know that only one of its arguments gets
// absolutised.
//
// Reading that code also surfaced a second, independent fail-open that no test
// covered and that the CI run happened not to hit: the comparison was
// byte-exact, and Windows paths are case-insensitive. See
// TestContains_CaseFoldsOnWindowsSemantics.
//
// None of this reproduces on the host that has to review it, so the decision is
// split into `contains`, which takes the platform's separator and case rule as
// ARGUMENTS. Windows semantics are therefore asserted from macOS and Linux by
// passing the Windows rule explicitly, rather than by hoping CI notices.

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	winSep  = '\\'
	unixSep = '/'
)

// TestContains_AbsolutionIsSymmetric is the direct #6288 regression. It states
// the property in the form the caller depends on: Escapes(x, x) is true for any
// non-empty x, whatever shape x has. The old code satisfied it only when
// filepath.Abs was a no-op on x, which on Windows excludes every volumeless
// rooted path.
func TestEscapes_AHomeIsAlwaysInsideItself(t *testing.T) {
	for _, home := range []string{
		filepath.Join(string(filepath.Separator), "home", "dev"),
		"relative-home",
		filepath.Join(".", "dot", "relative"),
	} {
		if !Escapes(home, home) {
			t.Errorf("Escapes(%q, %q) = false — the home is not inside itself, so the guard "+
				"fails OPEN for a write straight into it", home, home)
		}
		child := filepath.Join(home, ".cursor", "mcp.json")
		if !Escapes(child, home) {
			t.Errorf("Escapes(%q, %q) = false — a dotfile in the home reads as outside it", child, home)
		}
	}
}

// TestContains_WindowsShapes drives the pure decision with Windows-shaped
// inputs from any host. Each row is a string pair that a Windows process could
// genuinely produce for (write target, real user home).
func TestContains_WindowsShapes(t *testing.T) {
	cases := []struct {
		name string
		abs  string
		home string
		want bool
	}{
		{"identical", `C:\Users\dev`, `C:\Users\dev`, true},
		{"nested", `C:\Users\dev\.cursor\mcp.json`, `C:\Users\dev`, true},

		// The drive letter's case is not preserved by anything. os.UserHomeDir
		// returns %USERPROFILE% verbatim; a resolved write target may have come
		// from a different source with a different letter case, and on Windows
		// they name the SAME directory.
		{"drive letter case differs", `c:\Users\dev\.claude.json`, `C:\Users\dev`, true},
		{"whole path case differs", `C:\USERS\DEV\.claude.json`, `c:\users\dev`, true},

		// Prefix-not-parent must stay false with folding on, or the fix trades
		// one fail-open for a fail-closed.
		{"sibling sharing a name prefix", `C:\Users\dev-sandbox\x.json`, `C:\Users\dev`, false},
		{"sibling differing only in case, still a sibling", `C:\Users\DEVELOPER\x`, `C:\Users\dev`, false},
		{"another volume entirely", `D:\Users\dev\x`, `C:\Users\dev`, false},
		{"unrelated", `C:\Windows\Temp\x`, `C:\Users\dev`, false},

		// A UNC home is a real deployment (roaming profiles). Case folding must
		// reach the server and share components too.
		{"UNC identical", `\\srv\home\dev`, `\\srv\home\dev`, true},
		{"UNC nested, case differs", `\\SRV\Home\dev\.claude.json`, `\\srv\home\dev`, true},
		{"UNC different share", `\\srv\other\dev\x`, `\\srv\home\dev`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := contains(tc.abs, tc.home, winSep, true); got != tc.want {
				t.Fatalf("contains(%q, %q, windows) = %v, want %v", tc.abs, tc.home, got, tc.want)
			}
		})
	}
}

// TestContains_RootHomes is #6290 BLOCKER 1: a home that is a filesystem or
// drive ROOT already ends in a separator, so appending another one produced
// "//" or "C:\\" and NOTHING under the root ever matched. The guard answered
// "does not escape" — allowed — for every path on the volume.
//
// This was the same class of defect as the byte-exact comparison, in the same
// function, introduced by the commit that fixed the byte-exact comparison. It
// was reachable with HOME=/ or a drive-root %USERPROFILE%, and narrow
// reachability is not a defence in a fail-closed guard.
func TestContains_RootHomes(t *testing.T) {
	cases := []struct {
		name string
		abs  string
		home string
		sep  byte
		fold bool
		want bool
	}{
		{"windows drive root contains everything on the volume", `C:\Users\dev\.claude.json`, `C:\`, winSep, true, true},
		{"windows drive root is itself", `C:\`, `C:\`, winSep, true, true},
		{"windows drive root, other volume", `D:\Users\dev`, `C:\`, winSep, true, false},
		{"windows drive root, case differs", `c:\users\dev`, `C:\`, winSep, true, true},
		{"unix root contains everything", "/foo/bar", "/", unixSep, false, true},
		{"unix root is itself", "/", "/", unixSep, false, true},
		{"unix root and a relative path", "foo/bar", "/", unixSep, false, false},
		// A UNC share root does NOT end in a separator after Clean, so it must
		// keep going through the ordinary branch and must not over-match a
		// sibling share.
		{"UNC share root", `\\srv\share\x`, `\\srv\share`, winSep, true, true},
		{"UNC share root, sibling share", `\\srv\shareholder\x`, `\\srv\share`, winSep, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := contains(tc.abs, tc.home, tc.sep, tc.fold); got != tc.want {
				t.Fatalf("contains(%q, %q) = %v, want %v", tc.abs, tc.home, got, tc.want)
			}
		})
	}
}

// TestEscapes_RootHome is BLOCKER 1 at the exported boundary, on the host's own
// root — the shape a developer running with HOME=/ would actually hit.
func TestEscapes_RootHome(t *testing.T) {
	root := string(filepath.Separator)
	if vol := filepath.VolumeName(mustAbs(t, root)); vol != "" {
		root = vol + string(filepath.Separator)
	}
	p := filepath.Join(root, "foo", "bar")
	if !Escapes(p, root) {
		t.Fatalf("Escapes(%q, %q) = false — nothing on the volume is inside the root home, "+
			"so the guard fails OPEN for every write", p, root)
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatalf("abs(%q): %v", p, err)
	}
	return a
}

// TestFoldForCompare_MatchesNTFS is #6290 item 6. NTFS compares path names by
// simple UPPERCASE mapping ($UpCase), and Go's strings.ToLower disagrees with it
// in both directions:
//
//   - "dırk" (dotless i) vs "DIRK" — ToLower-unequal, NTFS-EQUAL. Folding down
//     therefore answers "different directory" for two names Windows treats as
//     one, which in this guard means FAIL-OPEN.
//   - "İrk" (dotted capital I) vs "irk", and U+212A KELVIN SIGN vs "K" —
//     ToLower-equal, NTFS-different, so folding down over-matches.
//
// strings.ToUpper agrees with NTFS on all three. Measured, not assumed.
func TestFoldForCompare_MatchesNTFS(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"dırk", "DIRK", true}, // NTFS: same name
		{"İrk", "irk", false},  // NTFS: different names
		{"\u212A", "K", false}, // KELVIN SIGN is not ASCII K to $UpCase
		{`C:\Users\DEV`, `c:\users\dev`, true},
		{"plain", "PLAIN", true},
	}
	for _, tc := range cases {
		if got := foldForCompare(tc.a) == foldForCompare(tc.b); got != tc.want {
			t.Errorf("foldForCompare(%q) == foldForCompare(%q) is %v, want %v "+
				"(NTFS compares via $UpCase, i.e. simple uppercase)", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestContains_CaseFoldsOnWindowsSemantics states the case rule as a property
// and pins that it is NOT applied under unix semantics — where `/home/Dev` and
// `/home/dev` are two different directories and folding them together would be
// a fail-CLOSED bug of exactly the same kind.
func TestContains_CaseFoldsOnWindowsSemantics(t *testing.T) {
	if !contains(`C:\Users\DEV\x`, `c:\users\dev`, winSep, true) {
		t.Error("windows semantics: a case-different path did not compare equal, so the guard " +
			"fails OPEN for a write into the real home spelled differently")
	}
	if contains("/home/DEV/x", "/home/dev", unixSep, false) {
		t.Error("unix semantics: two genuinely different directories compared equal, so the " +
			"guard fails CLOSED and panics correctly-isolated tests")
	}
}

// TestPathsAreCaseInsensitive_TracksTheHost pins the wiring, not the decision.
// contains is only correct if Escapes hands it the host's real rule; a mutant
// that hardcodes `false` passes every table above and reintroduces the whole
// Windows fail-open.
func TestPathsAreCaseInsensitive_TracksTheHost(t *testing.T) {
	want := runtime.GOOS == "windows" || runtime.GOOS == "darwin"
	if got := pathsAreCaseInsensitive; got != want {
		t.Fatalf("pathsAreCaseInsensitive = %v on %s, want %v", got, runtime.GOOS, want)
	}
}

// TestEscapes_UsesTheHostSeparatorAndCaseRule is the integration edge: Escapes
// must delegate to contains rather than re-deriving the comparison. Asserted by
// a shape only the delegation can satisfy — on a case-insensitive host the
// upper-cased home must still be caught, and on a case-sensitive one it must
// not be.
//
// #6290 item 8: this is a ONE-PLATFORM assertion, said plainly so nobody reads
// it as more. On Linux pathsAreCaseInsensitive is false and BOTH sides of the
// comparison are false, so the row is vacuous there and a mutant hardcoding
// fold=false survives the entire Ubuntu job. The darwin and windows legs of the
// matrix are what make this binding, and
// TestPathsAreCaseInsensitive_TracksTheHost is what fails on Linux if the wiring
// is cut.
func TestEscapes_UsesTheHostCaseRule(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	target := filepath.Join(home, "x.json")
	upper := strings.ToUpper(target)
	if got, want := Escapes(upper, home), pathsAreCaseInsensitive; got != want {
		t.Fatalf("Escapes(%q, %q) = %v, want %v (host case-insensitive = %v)",
			upper, home, got, want, pathsAreCaseInsensitive)
	}
}

// TestContains_ExtendedLengthPrefix documents a shape the guard normalises
// away. `\\?\C:\Users\dev` and `C:\Users\dev` are the same directory; Go's
// filepath.Clean deliberately preserves the `\\?\` prefix, so without stripping
// it the two never compare equal and the guard fails open.
func TestStripExtendedLengthPrefix(t *testing.T) {
	cases := []struct{ in, want string }{
		{`\\?\C:\Users\dev`, `C:\Users\dev`},
		{`\\?\c:\`, `c:\`},
		{`C:\Users\dev`, `C:\Users\dev`},
		{`\\?\UNC\srv\share\x`, `\\?\UNC\srv\share\x`}, // left alone on purpose; see the doc
		{`\\srv\share\x`, `\\srv\share\x`},
		{"/home/dev", "/home/dev"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := stripExtendedLengthPrefix(tc.in); got != tc.want {
			t.Errorf("stripExtendedLengthPrefix(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestContains_KnownMiss_ShortNames is a MISS row, in the sense
// internal/install's #6171 detector uses the word: it asserts what this guard
// does NOT catch, so the limit is a recorded decision rather than something a
// future reader discovers by being wrong about it.
//
// The #6288 CI log shows `C:\Users\RUNNER~1\AppData\...` and
// `C:\Users\runneradmin` in the same run, naming the same directory. Escapes
// compares strings and cannot see through an 8.3 alias, so a write reached
// through the short form is not recognised as being inside the home.
//
// This is deliberately NOT fixed here. Resolving short names (GetLongPathName)
// would make the guard start matching t.TempDir() on Windows — which lives
// under %USERPROFILE%\AppData\Local\Temp and is returned in short form — and
// this package reproduces mcpreg's semantics EXACTLY, including its lack of a
// temp-root exemption (see the package doc). Closing this miss therefore means
// deciding the temp-root question first, or every correctly-isolated Windows
// test in mcpreg and dashboard starts panicking. That is a separate change.
func TestContains_KnownMiss_ShortNames(t *testing.T) {
	if contains(`C:\Users\RUNNER~1\.claude.json`, `C:\Users\runneradmin`, winSep, true) {
		t.Fatal("the 8.3 short-name miss is now caught — good, but flip this row and read the " +
			"temp-root exemption note in the package doc before trusting it on Windows")
	}
}
