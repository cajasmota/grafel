package pathboundary

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// #6548 requirement 3 — grafel must not traverse into a home directory that is
// not the current user's. Owner decision 2026-09-02 overrode the package doc's
// former position that "another user's tree a user explicitly pointed grafel
// at" must keep climbing.
//
// Grading note: this is a REFUSAL, not an emission, so recall cannot see it and
// a boundary that refuses EVERYTHING looks identical from the graph to one that
// refuses correctly. Every positive case below is therefore paired with an
// under-firing control asserting a legitimate climb inside the user's OWN home
// still works, and the tests observe WHAT WAS VISITED (i.e. read), not what was
// produced.
//
// Every fixture is a TempDir with a synthetic /Users-like layout, pointed at by
// OverrideHomeReferences. No test here reads, enumerates or stats anything
// under a real home directory.

// usersLayout builds <tmp>/Users/{names...}, makes the FIRST name the current
// user's home, and returns the homes by name. The override is torn down when
// the test ends.
func usersLayout(t *testing.T, names ...string) map[string]string {
	t.Helper()
	container := mk(t, filepath.Join(t.TempDir(), "Users"))
	homes := make(map[string]string, len(names))
	for _, n := range names {
		homes[n] = mk(t, filepath.Join(container, n))
	}
	t.Cleanup(OverrideHomeReferences(homes[names[0]]))
	return homes
}

// TestClimb_RefusesAnotherUsersHome is the killing test for the Climb site: a
// climb that starts inside a directory belonging to another user must visit
// NOTHING — not the start path, not the other home, not the container above it.
func TestClimb_RefusesAnotherUsersHome(t *testing.T) {
	homes := usersLayout(t, "alice", "bob")
	start := mk(t, filepath.Join(homes["bob"], "proj", "pkg"))

	seen := visited(t, start, homes["alice"])
	if len(seen) != 0 {
		t.Fatalf("Climb READ another user's home (site: internal/pathboundary/pathboundary.go Climb): started at %q under %q while the current user's home is %q; visited %v",
			start, homes["bob"], homes["alice"], seen)
	}

	// The refusal is observable through the return value too, not only the log:
	// a marker inside the other user's tree must be unreachable.
	marker := mk(t, filepath.Join(homes["bob"], "proj"))
	if ClimbWithHome(start, homes["alice"], func(dir string) bool { return samePath(dir, marker) }) {
		t.Fatalf("Climb resolved a marker inside another user's home %q", marker)
	}
}

// TestClimb_OwnHomeStillClimbs is the UNDER-FIRING CONTROL. A boundary that
// refuses unconditionally passes the test above and fails this one.
func TestClimb_OwnHomeStillClimbs(t *testing.T) {
	homes := usersLayout(t, "alice", "bob")
	proj := mk(t, filepath.Join(homes["alice"], "proj"))
	start := mk(t, filepath.Join(proj, "pkg", "deep"))

	seen := visited(t, start, homes["alice"])
	want := []string{start, filepath.Join(proj, "pkg"), proj, homes["alice"]}
	if len(seen) != len(want) {
		t.Fatalf("legitimate climb inside the user's OWN home was cut short: visited %v, want %v", seen, want)
	}
	for i := range want {
		if !samePath(seen[i], want[i]) {
			t.Fatalf("legitimate climb inside the user's OWN home visited %v, want %v", seen, want)
		}
	}
	if !ClimbWithHome(start, homes["alice"], func(dir string) bool { return samePath(dir, proj) }) {
		t.Fatalf("Climb failed to resolve a marker at %q inside the user's own home", proj)
	}
}

// TestClimb_SiblingHomeIsNotAPrefixMatch pins the permissive direction: a
// string-prefix comparison would read /Users/alice2 as being inside
// /Users/alice and wave the climb through. #6579 was the adjacent
// case-sensitivity instance of exactly this class of bug.
func TestClimb_SiblingHomeIsNotAPrefixMatch(t *testing.T) {
	homes := usersLayout(t, "alice", "alice2")

	// alice is the current user; alice2 is a different, longer-named user.
	start := mk(t, filepath.Join(homes["alice2"], "proj"))
	if seen := visited(t, start, homes["alice"]); len(seen) != 0 {
		t.Fatalf("prefix comparison: %q was treated as inside %q; visited %v", start, homes["alice"], seen)
	}

	// And the reverse: the current user has the LONGER name, so a
	// HasPrefix(home, dir) spelling of the same mistake is caught too.
	longer := usersLayout(t, "alice2", "alice")
	start2 := mk(t, filepath.Join(longer["alice"], "proj"))
	if seen := visited(t, start2, longer["alice2"]); len(seen) != 0 {
		t.Fatalf("prefix comparison (reverse): %q was treated as inside %q; visited %v", start2, longer["alice2"], seen)
	}
}

// TestClimb_SymlinkedPathIntoAnotherUsersHome is the D1 regression pin. The
// class tests a symlink-RESOLVED path against the reference set, so holding
// only the LITERAL spelling of each home and container made the whole boundary
// silently inert whenever the fixture's own path crossed a symlink. On macOS
// that is every t.TempDir (/var → /private/var); in production it is
// /System/Volumes/Data/Users/... and any NFS automount.
//
// A boundary that is inert is indistinguishable from one that is absent, and
// this is the shape that hides it.
func TestClimb_SymlinkedPathIntoAnotherUsersHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privilege on Windows")
	}
	homes := usersLayout(t, "alice", "bob")
	start := mk(t, filepath.Join(homes["bob"], "proj", "pkg"))

	// Reach the same directory through a symlink that does not name bob.
	link := filepath.Join(t.TempDir(), "checkout")
	if err := os.Symlink(homes["bob"], link); err != nil {
		t.Skipf("symlink unsupported here: %v", err)
	}
	linked := filepath.Join(link, "proj", "pkg")

	for _, s := range []string{start, linked} {
		if seen := visited(t, s, homes["alice"]); len(seen) != 0 {
			t.Fatalf("climb through %q entered another user's home %q; visited %v", s, homes["bob"], seen)
		}
	}
}

// TestClimb_RealUIDHomeIsOursEvenWhenHOMEDisagrees constructs the case the
// owner's item 1 is about: the real uid says one thing and $HOME says another.
// A boundary that trusts $HOME classifies the REAL user's own home as somebody
// else's — and, far worse, treats whatever $HOME names as ours (see the
// laundering test below).
func TestClimb_RealUIDHomeIsOursEvenWhenHOMEDisagrees(t *testing.T) {
	homes := usersLayout(t, "alice", "bob")
	t.Setenv("HOME", homes["bob"])
	t.Setenv("USERPROFILE", homes["bob"])
	// Pinned alongside HOME per #6735: nothing here reads GRAFEL_HOME, but an
	// ambient one must not dangle against a redirected HOME.
	t.Setenv("GRAFEL_HOME", mk(t, filepath.Join(t.TempDir(), ".grafel")))

	proj := mk(t, filepath.Join(homes["alice"], "proj"))
	start := mk(t, filepath.Join(proj, "pkg"))

	// Home boundary disabled (climbHome == ""), so the only thing that can
	// classify alice's tree is the real-uid reference.
	seen := visited(t, start, "")
	if len(seen) == 0 {
		t.Fatalf("the REAL uid's own home %q was refused: the boundary trusted $HOME (%q) instead of os/user", homes["alice"], homes["bob"])
	}
	if !samePath(seen[0], start) {
		t.Fatalf("climb from %q visited %v", start, seen)
	}
}

// TestClimb_HOMECannotLaunderAnotherUsersHome is the D4 regression pin, and
// the direction that matters. An earlier revision took the UNION of the
// real-uid home and $HOME, reasoning that a hint could only ever widen what
// counts as ours. That widening IS the bypass: HOME=/Users/bob with the real
// uid alice made bob's entire home ours, and the climb walked out to the
// filesystem root. An environment variable is weaker evidence than an explicit
// path, and the owner's ruling already rejects the explicit path as
// authorisation.
func TestClimb_HOMECannotLaunderAnotherUsersHome(t *testing.T) {
	homes := usersLayout(t, "alice", "bob")
	t.Setenv("HOME", homes["bob"])
	t.Setenv("USERPROFILE", homes["bob"])
	// Pinned alongside HOME per #6735: nothing here reads GRAFEL_HOME, but an
	// ambient one must not dangle against a redirected HOME.
	t.Setenv("GRAFEL_HOME", mk(t, filepath.Join(t.TempDir(), ".grafel")))

	// Exactly the exposure shape review measured: $HOME set to the home ROOT
	// (what a container entrypoint or a HOME-preserving sudo sets).
	start := mk(t, filepath.Join(homes["bob"], "proj", "pkg"))
	for _, climbHome := range []string{"", homes["bob"]} {
		if seen := visited(t, start, climbHome); len(seen) != 0 {
			t.Fatalf("$HOME=%q laundered another user's home into ours (climbHome %q): visited %v", homes["bob"], climbHome, seen)
		}
	}
}

// TestOtherHome_SystemAndServiceHomesDoNotCreateContainers is the D3
// regression pin. Deriving a container from any home's PARENT looked simpler
// and broke real setups: macOS root is homed at /var/root, so the parent is
// /var — which made /var/folders (the macOS TMPDIR), /var/log and
// /var/tmp/checkout all "other users' homes" and made `sudo grafel index`
// refuse every climb. A Linux service account at /var/lib/postgresql does the
// same to /var/lib/jenkins/workspace.
//
// Guarding only "a filesystem root is never a container" caught just the Linux
// spelling /root, one level deep.
func TestOtherHome_SystemAndServiceHomesDoNotCreateContainers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX system-home layouts")
	}
	for _, home := range []string{"/root", "/var/root", "/var/lib/postgresql", "/usr/local/nobody"} {
		t.Run(home, func(t *testing.T) {
			t.Cleanup(OverrideHomeReferences(home))
			start := mk(t, filepath.Join(t.TempDir(), "srv", "checkout", "pkg"))
			if s := visited(t, start, ""); len(s) == 0 {
				t.Fatalf("a home of %q made %q a home container: climb from %q visited nothing",
					home, filepath.Dir(home), start)
			}
		})
	}
}

// TestOtherHome_NotAHomeNamesAreExempt is D2. These are children of a home
// container that are not user homes: refusing them buys ZERO privacy — nobody's
// private files live there — while breaking real, common setups (a shared
// checkout in /Users/Shared, Homebrew's prefix under /home/linuxbrew).
func TestOtherHome_NotAHomeNamesAreExempt(t *testing.T) {
	container := mk(t, filepath.Join(t.TempDir(), "Users"))
	home := mk(t, filepath.Join(container, "alice"))
	t.Cleanup(OverrideHomeReferences(home))

	var exempt []string
	switch runtime.GOOS {
	case "darwin":
		exempt = []string{"Shared", "shared", "Guest", ".localized"}
	case "windows":
		exempt = []string{"Public", "Default", "Default User", "All Users"}
	default:
		exempt = []string{"linuxbrew", "lost+found", "Public"}
	}
	for _, name := range exempt {
		t.Run(name, func(t *testing.T) {
			start := mk(t, filepath.Join(container, name, "repos", "proj"))
			if s := visited(t, start, home); len(s) == 0 {
				t.Fatalf("%q is not a user home and must not be refused: climb from %q visited nothing", name, start)
			}
		})
	}

	// Over-firing control: the exemption is by NAME, not a hole for anything
	// that merely sits beside it.
	if s := visited(t, mk(t, filepath.Join(container, "Sharedish", "proj")), home); len(s) != 0 {
		t.Fatalf("the not-a-home exemption leaked to a real home name; visited %v", s)
	}
}

// TestOtherHome_UsersContainerHoldsOnlyPlausibleNames — the container itself
// must be plausible. <tmp>/data/alice must not make <tmp>/data a container.
func TestOtherHome_UsersContainerHoldsOnlyPlausibleNames(t *testing.T) {
	root := t.TempDir()
	home := mk(t, filepath.Join(root, "data", "alice"))
	t.Cleanup(OverrideHomeReferences(home))

	start := mk(t, filepath.Join(root, "data", "shared-ci", "proj"))
	if s := visited(t, start, home); len(s) == 0 {
		t.Fatalf("%q is not a home container, yet the climb from %q was refused", filepath.Join(root, "data"), start)
	}
}

// TestClimb_RefusesUnderAnotherUsersProtectedTree — composition with the
// media/TCC class split. Another user's ~/Documents is refused AT the deepest
// level, by the other-home rule, without any dependence on classTCC's
// outermost-only rule.
func TestClimb_RefusesUnderAnotherUsersProtectedTree(t *testing.T) {
	homes := usersLayout(t, "alice", "bob")
	start := mk(t, filepath.Join(homes["bob"], "Documents", "proj", "pkg"))

	if s := visited(t, start, homes["alice"]); len(s) != 0 {
		t.Fatalf("climb inside another user's Documents visited %v", s)
	}
}
