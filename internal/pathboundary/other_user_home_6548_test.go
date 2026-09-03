package pathboundary

import (
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
// Every fixture is a TempDir with a synthetic /Users-like layout. No test here
// reads, enumerates or stats anything under a real home directory.

// usersLayout builds <tmp>/Users/{alice,bob,alice2} and returns the container.
func usersLayout(t *testing.T, names ...string) (container string, homes map[string]string) {
	t.Helper()
	container = mk(t, filepath.Join(t.TempDir(), "Users"))
	homes = make(map[string]string, len(names))
	for _, n := range names {
		homes[n] = mk(t, filepath.Join(container, n))
	}
	return container, homes
}

// TestClimb_RefusesAnotherUsersHome is the killing test for the Climb site: a
// climb that starts inside a directory belonging to another user must visit
// NOTHING — not the start path, not the other home, not the container above it.
func TestClimb_RefusesAnotherUsersHome(t *testing.T) {
	container, homes := usersLayout(t, "alice", "bob")
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
	_ = container
}

// TestClimb_OwnHomeStillClimbs is the UNDER-FIRING CONTROL. A boundary that
// refuses unconditionally passes the test above and fails this one.
func TestClimb_OwnHomeStillClimbs(t *testing.T) {
	_, homes := usersLayout(t, "alice", "bob")
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
	_, homes := usersLayout(t, "alice", "alice2")

	// alice is the current user; alice2 is a different, longer-named user.
	start := mk(t, filepath.Join(homes["alice2"], "proj"))
	if seen := visited(t, start, homes["alice"]); len(seen) != 0 {
		t.Fatalf("prefix comparison: %q was treated as inside %q; visited %v", start, homes["alice"], seen)
	}

	// And the reverse: the current user has the LONGER name, so a
	// HasPrefix(home, dir) spelling of the same mistake is caught too.
	start2 := mk(t, filepath.Join(homes["alice"], "proj"))
	if seen := visited(t, start2, homes["alice2"]); len(seen) != 0 {
		t.Fatalf("prefix comparison (reverse): %q was treated as inside %q; visited %v", start2, homes["alice2"], seen)
	}
}

// TestClimb_RealUIDHomeIsOursEvenWhenHOMEDisagrees constructs the case the
// owner's item 1 is about: the real uid says one thing and $HOME says another.
// A boundary that trusts $HOME alone classifies the REAL user's own home as
// somebody else's and refuses a perfectly legitimate climb.
func TestClimb_RealUIDHomeIsOursEvenWhenHOMEDisagrees(t *testing.T) {
	_, homes := usersLayout(t, "alice", "bob")

	// Real uid home is alice. $HOME is empty — the bare cron/launchd case.
	restore := OverrideHomeReferences(homes["alice"], "")
	defer restore()

	proj := mk(t, filepath.Join(homes["alice"], "proj"))
	start := mk(t, filepath.Join(proj, "pkg"))

	// Home boundary disabled (climbHome == ""), so the only thing that can
	// classify alice's tree is the real-uid reference.
	seen := visited(t, start, "")
	if len(seen) == 0 {
		t.Fatalf("the REAL uid's own home %q was refused: the boundary trusted $HOME (empty) instead of os/user", homes["alice"])
	}
	if !samePath(seen[0], start) {
		t.Fatalf("climb from %q visited %v", start, seen)
	}

	// The container /Users was learned from the real-uid home, so bob's tree
	// is refused in the same process with the same (empty) $HOME.
	bobStart := mk(t, filepath.Join(homes["bob"], "proj"))
	if s := visited(t, bobStart, ""); len(s) != 0 {
		t.Fatalf("another user's home %q was climbed with $HOME unset; visited %v", homes["bob"], s)
	}
}

// TestClimb_EnvHomeHintOnlyWidens documents the union's direction: $HOME can
// add a home, never remove one. A test process whose $HOME is a TempDir must
// keep climbing there, and the real uid's home stays ours regardless.
func TestClimb_EnvHomeHintOnlyWidens(t *testing.T) {
	_, homes := usersLayout(t, "alice", "bob")
	restore := OverrideHomeReferences(homes["alice"], homes["bob"])
	defer restore()

	for name, h := range homes {
		start := mk(t, filepath.Join(h, "proj"))
		if s := visited(t, start, ""); len(s) == 0 {
			t.Fatalf("%s's home %q is named by one of the two references and must be climbable; visited nothing", name, h)
		}
	}
}

// TestOtherHome_FilesystemRootIsNeverAContainer — a process whose home is
// /root (a container image running as root) must not classify /usr, /opt and
// /tmp as other users' homes. This is the fail-catastrophic direction of the
// container derivation.
func TestOtherHome_FilesystemRootIsNeverAContainer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX root-home layout")
	}
	restore := OverrideHomeReferences("/root", "")
	defer restore()

	start := mk(t, filepath.Join(t.TempDir(), "srv", "checkout", "pkg"))
	if s := visited(t, start, ""); len(s) == 0 {
		t.Fatalf("a home of /root made the filesystem root a home container: climb from %q visited nothing", start)
	}
}

// TestClimb_RefusesUnderAnotherUsersProtectedTree — composition with the
// media/TCC class split. Another user's ~/Documents is refused AT the deepest
// level, by the other-home rule, without any dependence on classTCC's
// outermost-only rule (which is scoped to the CURRENT user's home).
func TestClimb_RefusesUnderAnotherUsersProtectedTree(t *testing.T) {
	_, homes := usersLayout(t, "alice", "bob")
	start := mk(t, filepath.Join(homes["bob"], "Documents", "proj", "pkg"))

	if s := visited(t, start, homes["alice"]); len(s) != 0 {
		t.Fatalf("climb inside another user's Documents visited %v", s)
	}
}
