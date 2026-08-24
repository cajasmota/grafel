package pathboundary

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// #6548 — the boundary predicate itself. Every fixture is a TempDir with an
// INJECTED home (ClimbWithHome); no test here reads or writes the real home.

func mk(t *testing.T, p string) string {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
	return p
}

// visited records the climb so a test can assert exactly where it stopped.
func visited(t *testing.T, start, home string) []string {
	t.Helper()
	var seen []string
	ClimbWithHome(start, home, func(dir string) bool {
		seen = append(seen, dir)
		return false
	})
	return seen
}

func TestClimb_StopsAtHomeWhenStartIsInside(t *testing.T) {
	root := t.TempDir()
	home := mk(t, filepath.Join(root, "home", "u"))
	start := mk(t, filepath.Join(home, "a", "b"))

	seen := visited(t, start, home)
	want := []string{start, filepath.Join(home, "a"), home}
	if len(seen) != len(want) {
		t.Fatalf("climb did not stop at $HOME: visited %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("climb step %d: got %q, want %q (full: %v)", i, seen[i], want[i], seen)
		}
	}
	// The decisive assertion: nothing above $HOME was ever touched.
	for _, d := range seen {
		if !Inside(d, home) {
			t.Fatalf("climb visited %q, which is outside $HOME %q", d, home)
		}
	}
}

func TestClimb_HomeItselfIsVisited(t *testing.T) {
	root := t.TempDir()
	home := mk(t, filepath.Join(root, "home", "u"))
	start := mk(t, filepath.Join(home, "repo"))

	found := ClimbWithHome(start, home, func(dir string) bool { return dir == home })
	if !found {
		t.Fatalf("$HOME must be VISITED before the climb stops (a marker at $HOME is legitimate); visited %v", visited(t, start, home))
	}
}

func TestClimb_StartOutsideHomeReachesRoot(t *testing.T) {
	root := t.TempDir()
	home := mk(t, filepath.Join(root, "home", "u"))
	// A start path that is NOT under home: it must climb all the way up.
	start := mk(t, filepath.Join(root, "opt", "src", "repo"))

	seen := visited(t, start, home)
	last := seen[len(seen)-1]
	if last != filepath.Dir(last) {
		t.Fatalf("a climb starting outside $HOME must reach the filesystem root; stopped at %q (visited %v)", last, seen)
	}
	for _, d := range seen {
		if samePath(d, home) {
			t.Fatalf("climb outside $HOME must not visit the home dir itself: %v", seen)
		}
	}
}

// TestClimb_HomeLookingAncestorIsNotABoundary is the permissive-direction
// guard on the predicate itself: only the ACTUAL home stops a climb. A
// boundary that fired on any ancestor that merely looks like a home container
// (a child of /Users, /home, C:\Users) would silently stop a repo under another
// root from ever resolving its group — the marker above it is never reached.
func TestClimb_HomeLookingAncestorIsNotABoundary(t *testing.T) {
	root := t.TempDir()
	home := mk(t, filepath.Join(root, "Users", "me"))
	// Not our home, but a directory whose parent is named "Users".
	start := mk(t, filepath.Join(root, "Users", "someone", "src", "deep"))

	seen := visited(t, start, home)
	last := seen[len(seen)-1]
	if last != filepath.Dir(last) {
		t.Fatalf("a home-LOOKING ancestor must not stop the climb: stopped at %q, want the filesystem root (visited %v)", last, seen)
	}
	if len(seen) < 5 {
		t.Fatalf("climb cut short at a home-looking ancestor: visited %v", seen)
	}
}

func TestClimb_EmptyHomeStillReachesRootAndNoPanic(t *testing.T) {
	root := t.TempDir()
	start := mk(t, filepath.Join(root, "a", "b"))

	seen := visited(t, start, "")
	if len(seen) == 0 {
		t.Fatal("an undeterminable home must not disable the climb entirely")
	}
	last := seen[len(seen)-1]
	if last != filepath.Dir(last) {
		t.Fatalf("with no home boundary the climb still stops at the root; stopped at %q", last)
	}
}

func TestClimb_DepthBackstop(t *testing.T) {
	// A synthetic path deeper than the cap; no filesystem involved.
	deep := string(filepath.Separator) + strings.TrimSuffix(strings.Repeat("d"+string(filepath.Separator), MaxAncestorDepth*3), string(filepath.Separator))
	if runtime.GOOS == "windows" {
		deep = "C:" + deep
	}

	n := 0
	ClimbWithHome(deep, "", func(string) bool {
		n++
		return false
	})
	if n != MaxAncestorDepth {
		t.Fatalf("depth backstop: visited %d levels, want exactly %d", n, MaxAncestorDepth)
	}
}

func TestClimb_SuccessStopsImmediately(t *testing.T) {
	root := t.TempDir()
	start := mk(t, filepath.Join(root, "a", "b", "c"))
	n := 0
	ok := ClimbWithHome(start, "", func(dir string) bool {
		n++
		return dir == filepath.Join(root, "a", "b")
	})
	if !ok || n != 2 {
		t.Fatalf("climb should stop on the first success: ok=%v visits=%d, want ok=true visits=2", ok, n)
	}
}

func TestClimb_EmptyDirAndNilVisit(t *testing.T) {
	if ClimbWithHome("", "/home/u", func(string) bool { return true }) {
		t.Fatal("empty start must not climb")
	}
	if ClimbWithHome("/home/u/x", "/home/u", nil) {
		t.Fatal("nil visit must not climb")
	}
}

func TestInside(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "u")
	cases := []struct {
		path string
		want bool
	}{
		{home, true},
		{filepath.Join(home, "x"), true},
		{filepath.Join(home, "x", "y"), true},
		{filepath.Join(string(filepath.Separator), "home", "other"), false},
		{filepath.Join(string(filepath.Separator), "home"), false},
		// A sibling whose name merely PREFIXES the home path is not inside it.
		{filepath.Join(string(filepath.Separator), "home", "u2"), false},
		{"", false},
	}
	for _, c := range cases {
		if got := Inside(c.path, home); got != c.want {
			t.Errorf("Inside(%q, %q) = %v, want %v", c.path, home, got, c.want)
		}
	}
	if Inside(home, "") {
		t.Error("an empty home contains nothing")
	}
}

// TestClimb_NoHomeWarnsOncePerProcess — the boundary is allowed to be absent
// when os.UserHomeDir fails (a bare cron/launchd/container environment: fail-open
// keeps group resolution working, and guessing a home path could name somebody
// else's). It is NOT allowed to be absent silently: a guard that degrades with
// nothing saying so reads as "protected" forever after. One line, once per
// process — a per-climb line would print on every MCP call that resolves a cwd.
func TestClimb_NoHomeWarnsOncePerProcess(t *testing.T) {
	var lines []string
	origLog, origOnce := logHome, noHomeOnce
	logHome = func(msg string) { lines = append(lines, msg) }
	noHomeOnce = new(sync.Once)
	t.Cleanup(func() { logHome, noHomeOnce = origLog, origOnce })

	// Make os.UserHomeDir fail on every platform: it reads $HOME on unix and
	// %USERPROFILE% on Windows, and errors when the relevant one is empty.
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("home", "") // plan9
	if UserHome() != "" {
		t.Skip("this platform still resolves a home with the env cleared")
	}

	root := t.TempDir()
	start := mk(t, filepath.Join(root, "a", "b"))

	// Three climbs; the process must be told exactly once.
	for i := 0; i < 3; i++ {
		Climb(start, func(string) bool { return false })
	}

	if len(lines) != 1 {
		t.Fatalf("degraded home boundary must be announced exactly once per process: got %d lines: %v", len(lines), lines)
	}
	for _, want := range []string{"home", "INACTIVE", "root", "depth"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("diagnostic should name %q (that the home boundary is inactive and only the root stop + depth cap apply); got: %s", want, lines[0])
		}
	}
}

// TestClimb_HomeResolvableStaysQuiet — the diagnostic must not fire on the
// ordinary path, or it becomes noise on every MCP call.
func TestClimb_HomeResolvableStaysQuiet(t *testing.T) {
	var lines []string
	origLog, origOnce := logHome, noHomeOnce
	logHome = func(msg string) { lines = append(lines, msg) }
	noHomeOnce = new(sync.Once)
	t.Cleanup(func() { logHome, noHomeOnce = origLog, origOnce })

	root := t.TempDir()
	home := mk(t, filepath.Join(root, "home", "u"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	Climb(mk(t, filepath.Join(home, "a")), func(string) bool { return false })
	if len(lines) != 0 {
		t.Fatalf("a resolvable home must produce no diagnostic; got %v", lines)
	}
}

// TestClimb_StartingExactlyAtHomeStopsThere — the boundary must bound a climb
// that BEGINS at $HOME, not only one that begins below it. Running any grafel
// command with the working directory set to the user's home is ordinary use;
// if the start path being the boundary itself exempted the climb, that one case
// would ascend from $HOME through /Users (or /home) to /, reading at every
// level — precisely the traversal #6548 exists to stop.
func TestClimb_StartingExactlyAtHomeStopsThere(t *testing.T) {
	root := t.TempDir()
	home := mk(t, filepath.Join(root, "home", "u"))

	// No marker anywhere: the climb ends only at a stop condition.
	seen := visited(t, home, home)

	if len(seen) != 1 || seen[0] != home {
		t.Fatalf("a climb starting AT $HOME must visit $HOME and stop: visited %v, want exactly [%s]", seen, home)
	}
	for _, d := range seen {
		if !samePath(d, home) {
			t.Fatalf("a climb starting AT $HOME ascended to %q; nothing above $HOME %q is ours to read (visited %v)", d, home, seen)
		}
	}
}

// TestClimb_StartingAtHomeVisitsHomeItself — the permissive-direction half of
// the test above: bounding the climb must not skip the start directory. A
// marker sitting in $HOME has to be found when $HOME is where you started.
func TestClimb_StartingAtHomeVisitsHomeItself(t *testing.T) {
	root := t.TempDir()
	home := mk(t, filepath.Join(root, "home", "u"))

	if !ClimbWithHome(home, home, func(dir string) bool { return samePath(dir, home) }) {
		t.Fatal("a climb starting AT $HOME must still visit $HOME before stopping")
	}
}
