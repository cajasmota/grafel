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
// OverrideHomeReferences, and no test here reads or enumerates the CONTENTS of
// a real home directory. Two qualifications, because an unqualified claim here
// would be prose asserting more than the tests do:
//
//   - On Windows t.TempDir() lives inside the running user's home
//     (C:\Users\<user>\AppData\Local\Temp\...), so a climb out of a
//     fixture necessarily lstats real ancestors. That is also why every
//     fixture passes RealUIDHomeForTest() alongside its synthetic home — see
//     that function, and #6787.
//   - TestOtherHome_NoHomeReferenceStandsDown classifies a path under the
//     PLATFORM container (/Users, /home, C:\Users) because a synthetic
//     container cannot exercise the stand-down. The name it uses belongs to no
//     user and is never created; the climb stops at the first level.

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
	t.Cleanup(OverrideHomeReferences(homes[names[0]], RealUIDHomeForTest()))
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
			t.Cleanup(OverrideHomeReferences(home, RealUIDHomeForTest()))
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
	t.Cleanup(OverrideHomeReferences(home, RealUIDHomeForTest()))

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
	t.Cleanup(OverrideHomeReferences(home, RealUIDHomeForTest()))

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

// TestClimb_OwnHomeUnderASecondSpellingIsStillOurs is the #6787 regression
// pin, and the reason Windows CI went red across five packages.
//
// AXIS VARIED: the SPELLING of the path used to reach the current user's own
// home — its canonical name, versus a second name for the SAME directory that
// sits in the same home container and does not match the reference set
// textually. HELD CONSTANT: the layout, the reference set, the foreign home,
// and the climb's home bound.
//
// On Windows the second spelling is the 8.3 short name the filesystem itself
// hands out: %TEMP% is C:\Users\RUNNER~1\AppData\Local\Temp\... while
// os/user reports C:\Users\runneradmin, so the literal spelling of the user's
// OWN home read as another user's and the at-or-under rule then refused
// everything beneath it. That mechanism needs Windows to observe, and this
// test does NOT reproduce it — a symlink is not a short name. What it does
// reproduce is the CLASS: one directory, two names, only one of them in the
// reference set. The fix is the same for both, because classifyDir now decides
// on the EvalSymlinks-resolved spelling, and on Windows EvalSymlinks folds the
// short name to the long one (toNorm/normBase, path/filepath).
//
// OVER-FIRING CONTROL is in the same fixture: a genuinely foreign home,
// reached by its own canonical name, must STILL be refused. Widening what
// counts as "ours" is precisely the direction that can silently disable this
// boundary, so the widening and the thing it must not touch are asserted
// together.
func TestClimb_OwnHomeUnderASecondSpellingIsStillOurs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privilege on Windows; the native second spelling there is the 8.3 short name")
	}
	homes := usersLayout(t, "alice", "bob")
	container := filepath.Dir(homes["alice"])

	// A second name for alice's home, sitting in the same container, whose
	// text matches neither spelling held in the reference set. Structurally
	// indistinguishable from a sibling user's home until it is resolved.
	alias := filepath.Join(container, "alice-second-spelling")
	if err := os.Symlink(homes["alice"], alias); err != nil {
		t.Skipf("symlink unsupported here: %v", err)
	}
	proj := mk(t, filepath.Join(homes["alice"], "proj"))
	start := filepath.Join(alias, "proj")

	seen := visited(t, start, "")
	if len(seen) == 0 {
		t.Fatalf("the current user's OWN home %q, reached as %q, was classified as another user's: the climb from %q visited nothing. A path can name one directory in more than one way (a symlink here; a Windows 8.3 short name in #6787) and the identity comparison must resolve before it compares.",
			homes["alice"], alias, start)
	}
	// And it must actually reach alice's own tree, not merely visit the start.
	// The climb reports the LITERAL path it walked, so the marker is matched
	// after resolving — the point of the test is that the two spell the same
	// directory.
	if !ClimbWithHome(start, "", func(dir string) bool { return samePath(resolveOrSelf(dir), resolveOrSelf(proj)) }) {
		t.Fatalf("climb from %q never reached %q inside the current user's own home", start, proj)
	}

	// OVER-FIRING CONTROL: bob is a different user, named canonically. The
	// widening above must not have reached him.
	if s := visited(t, mk(t, filepath.Join(homes["bob"], "proj")), ""); len(s) != 0 {
		t.Fatalf("resolving the identity comparison disabled the boundary: a climb inside another user's home %q visited %v", homes["bob"], s)
	}
	// ... nor to a foreign home reached through a symlink of its own, which
	// is the D1 direction: resolving must fold an alias ONTO its target, not
	// wave the alias through.
	bobAlias := filepath.Join(container, "not-obviously-bob")
	if err := os.Symlink(homes["bob"], bobAlias); err == nil {
		if s := visited(t, filepath.Join(bobAlias, "proj"), ""); len(s) != 0 {
			t.Fatalf("a foreign home reached as %q was let through; visited %v", bobAlias, s)
		}
	}
}

// TestOtherHome_NoHomeReferenceStandsDown pins the fail-direction decision
// documented in otherhome.go under "When the current user's home cannot be
// determined at all". Before #6787 the package doc asserted that an
// unresolvable home is "a narrowing of this boundary, never a widening of it"
// and NO test observed that claim — the dominant defect class in this repo.
// The claim was true and useless: the narrowing is TOTAL, because with no
// left-hand side for "not MY home" every home under a platform container,
// including the user's own, is somebody else's.
//
// AXIS VARIED: whether a reference for the current user's home exists at all.
// HELD CONSTANT: the path under test, the platform's conventional home
// container, and the climb bound.
//
// The path is deliberately under the PLATFORM container (/Users, /home,
// C:\Users) rather than a synthetic one: with no reference, the synthetic
// container is not derived either, so a TempDir layout would pass this test
// without the class standing down at all — vacuously. Nothing is created and
// nothing is enumerated; the name does not exist and the climb stops at the
// first level.
func TestOtherHome_NoHomeReferenceStandsDown(t *testing.T) {
	containers := platformHomeContainers()
	if len(containers) == 0 {
		t.Skip("no conventional home container on this platform")
	}
	// A name no user has. Never created, never read.
	stranger := filepath.Join(containers[0], "grafel-6787-no-such-user", "proj")

	reached := func() bool {
		return ClimbWithHome(stranger, "", func(dir string) bool { return samePath(dir, stranger) })
	}

	// WITH a reference (some other home entirely): stranger is a child of a
	// conventional container and is not ours, so it is refused. This is the
	// control that keeps the assertion below from being vacuous.
	restore := OverrideHomeReferences(filepath.Join(containers[0], "grafel-6787-me"))
	if reached() {
		restore()
		t.Fatalf("with a home reference, %q under the conventional container %q was NOT refused — the stand-down assertion below would prove nothing", stranger, containers[0])
	}
	restore()

	// WITHOUT any reference: the class has no evidence and must stand down
	// rather than refuse the user their own machine.
	t.Cleanup(OverrideHomeReferences())
	if !reached() {
		t.Fatalf("with NO home reference the other-user-home class refused %q. It cannot tell the user's own home from anyone else's here, and refusing means grafel declines to index the user's own repositories (#6787). See otherhome.go, \"When the current user's home cannot be determined at all\".", stranger)
	}
}

// TestOtherHome_EveryReferenceInTheSetIsHonoured pins the property the #6787
// Windows fix rests on: the reference set is a SET, and every member of it
// contributes — its own identity AND the container it sits in. A fix that
// honoured only the first entry would look correct in every fixture that
// passes one home and fail exactly where the fix is needed.
//
// WHAT THIS DOES NOT REPRODUCE, stated plainly: the Windows topology itself.
// There, t.TempDir() is C:\Users\<user>\AppData\Local\Temp\..., so the
// process's real home ENCLOSES the fixture and is a child of the
// platform-conventional container C:\Users — which is why a fixture that
// referenced only its synthetic home turned the real home into "another
// user's" and refused everything beneath the temp root. That shape needs the
// container to be the platform's own absolute path, so it cannot be built in a
// TempDir on darwin or linux. It is asserted here through the property, not
// the topology.
//
// AXIS VARIED: how many homes the reference set holds (one, versus the same
// one plus a second in a DIFFERENT container). HELD CONSTANT: both layouts,
// the start path, and the climb bound.
func TestOtherHome_EveryReferenceInTheSetIsHonoured(t *testing.T) {
	root := t.TempDir()
	first := mk(t, filepath.Join(root, "a", "Users", "alice"))
	second := mk(t, filepath.Join(root, "b", "Users", "me"))
	// A sibling of the SECOND home, in the second home's container.
	stranger := mk(t, filepath.Join(root, "b", "Users", "bob", "proj"))

	// CONTROL — with only the first home referenced, the second container is
	// never derived, so bob is not recognised as anybody's home and the climb
	// runs. Without this the assertion below could pass on a boundary that
	// refuses nothing.
	restore := OverrideHomeReferences(first)
	if s := visited(t, stranger, ""); len(s) == 0 {
		restore()
		t.Fatalf("with only %q referenced, %q sits in an underived container and the climb should run; visited nothing", first, stranger)
	}
	restore()

	// With BOTH referenced, the second reference contributes its container and
	// bob becomes another user's home. A fix that kept only homes[0] leaves
	// this climb running.
	t.Cleanup(OverrideHomeReferences(first, second))
	if s := visited(t, stranger, ""); len(s) != 0 {
		t.Fatalf("the second reference %q was dropped: its container %q was never derived, so another user's home %q was read; visited %v",
			second, filepath.Dir(second), filepath.Dir(stranger), s)
	}

	// UNDER-FIRING CONTROL: honouring the second reference must not refuse it.
	own := mk(t, filepath.Join(second, "proj", "pkg"))
	if s := visited(t, own, ""); len(s) == 0 {
		t.Fatalf("the current user's own home %q was refused once a second reference was held; climb from %q visited nothing", second, own)
	}
}
