package pathboundary

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"sync"
)

// # The other-user-home boundary (#6548 requirement 3)
//
// grafel must not traverse into a home directory that is not the current
// user's. The package doc above used to say the opposite — that "another
// user's tree a user explicitly pointed grafel at" must keep climbing — and
// the owner ruled that position wrong on 2026-09-02: an explicit path is a
// claim about INTENT, not about PERMISSION, and the harm this issue exists to
// prevent (reading another user's files) is not undone by the user having
// typed the path.
//
// # What "the current user's home" means
//
// $HOME is a hint, not the reference. It is settable by anyone who can set an
// environment variable, and this repo's own test isolation resets it
// constantly. The reference is the REAL uid: os/user.Current resolves the
// running process's uid through the platform password database (getpwuid on
// darwin/linux, the token's profile directory on Windows) and never consults
// $HOME.
//
// The rule chosen is a UNION, and the direction matters: a directory is "ours"
// if EITHER the real-uid home or the $HOME hint names it. So the hint can only
// ever WIDEN what counts as ours; it can never narrow it. Setting $HOME cannot
// make the real uid's own home look foreign (which would break every climb in
// a test process), and the real-uid home is always ours whatever $HOME says.
//
// # What counts as another user's home
//
// A directory D is another user's home when D's PARENT is a home CONTAINER and
// D is not one of the current user's homes. The comparison is whole-component
// and uses the filesystem's case semantics — never a string prefix, so
// /home/alice2 is not read as being inside /home/alice (#6579 was the adjacent
// case-sensitivity instance of the same class of bug).
//
// Containers are the parent of each current-user home plus the platform
// conventions: /Users on darwin, /home and /var/home on other unixes,
// %SystemDrive%\Users on Windows. A filesystem root is NEVER a container: a
// process whose home is /root (a container image running as root) would
// otherwise classify /usr, /opt and /tmp as other users' homes and refuse
// every climb on the machine.
//
// # How it composes with the media/TCC class split (v0.3.1)
//
// It is a third class refused AT OR UNDER, like classMedia and unlike
// classTCC's outermost-only rule — and it is evaluated FIRST. At-or-under is
// what makes it compose: the nesting bug review caught once (~/Library/Mobile
// Documents is not "outermost", so a climb starting inside iCloud Drive
// visited both iCloud directories) is an artefact of the outermost rule, and
// this class never uses it. There is also no overlap to reconcile: every
// classMedia and classTCC member is under the CURRENT user's home by
// construction, which is exactly the set this class excludes.

// homeReferences is one process's answer to "whose home is whose", computed
// once per climb rather than once per level.
type homeReferences struct {
	// current holds every directory that counts as the current user's home.
	current []string
	// containers holds directories that hold user homes as direct children.
	containers []string
}

// realUIDHome and envHome are the two references, as swappable funcs so a test
// can construct the case where they DISAGREE — the whole point of "$HOME is a
// hint". Production wiring: os/user (real uid) and os.UserHomeDir ($HOME /
// %USERPROFILE%).
var (
	homeRefsMu  sync.RWMutex
	realUIDHome = func() string {
		u, err := user.Current()
		if err != nil || u == nil || u.HomeDir == "" {
			return ""
		}
		return filepath.Clean(u.HomeDir)
	}
	envHome = func() string { return UserHome() }
)

// OverrideHomeReferences replaces the current-user home references and returns
// a restore func. It is a TEST SEAM, exported because the killing test for
// this boundary must drive a REAL climber in ANOTHER package
// (gitmeta.HasGitDirInTree) against a synthetic /Users-like layout in a
// TempDir — unit-testing the predicate in this package does not pin the call
// site (#6533). Production code must never call it.
func OverrideHomeReferences(realUID, env string) (restore func()) {
	homeRefsMu.Lock()
	prevReal, prevEnv := realUIDHome, envHome
	realUIDHome = func() string { return cleanOrEmpty(realUID) }
	envHome = func() string { return cleanOrEmpty(env) }
	homeRefsMu.Unlock()
	return func() {
		homeRefsMu.Lock()
		realUIDHome, envHome = prevReal, prevEnv
		homeRefsMu.Unlock()
	}
}

func cleanOrEmpty(p string) string {
	if p == "" {
		return ""
	}
	return filepath.Clean(p)
}

// resolveHomeReferences builds the union described above. climbHome is the
// home the caller injected into ClimbWithHome; it counts as ours too, so a
// test (or a caller with a better answer than the environment) never has its
// own fixture home classified as somebody else's.
func resolveHomeReferences(climbHome string) homeReferences {
	homeRefsMu.RLock()
	realFn, envFn := realUIDHome, envHome
	homeRefsMu.RUnlock()

	var refs homeReferences
	add := func(h string) {
		h = cleanOrEmpty(h)
		if h == "" {
			return
		}
		for _, existing := range refs.current {
			if samePath(existing, h) {
				return
			}
		}
		refs.current = append(refs.current, h)
	}
	add(realFn())
	add(envFn())
	add(climbHome)

	addContainer := func(c string) {
		c = cleanOrEmpty(c)
		// A filesystem root is never a home container — see the doc above.
		if c == "" || filepath.Dir(c) == c {
			return
		}
		for _, existing := range refs.containers {
			if samePath(existing, c) {
				return
			}
		}
		refs.containers = append(refs.containers, c)
	}
	for _, h := range refs.current {
		addContainer(filepath.Dir(h))
	}
	for _, c := range platformHomeContainers() {
		addContainer(c)
	}
	return refs
}

// platformHomeContainers lists the conventional home containers per platform.
// macOS is merely the platform that ASKS; on Linux and Windows the same
// traversal succeeds silently, which is worse — so all three are named.
func platformHomeContainers() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"/Users"}
	case "windows":
		drive := os.Getenv("SystemDrive")
		if drive == "" {
			drive = "C:"
		}
		return []string{drive + `\Users`}
	default:
		return []string{"/home", "/var/home"}
	}
}

// isOtherUserHome reports whether dir IS another user's home directory —
// a whole-component comparison against the container set, never a prefix.
func isOtherUserHome(dir string, refs homeReferences) bool {
	if dir == "" || len(refs.containers) == 0 {
		return false
	}
	dir = filepath.Clean(dir)
	for _, h := range refs.current {
		if samePath(dir, h) {
			return false
		}
	}
	parent := filepath.Dir(dir)
	if parent == dir {
		return false
	}
	for _, c := range refs.containers {
		if samePath(parent, c) {
			return true
		}
	}
	return false
}

// underOtherUserHome is the AT-OR-UNDER form: dir itself, or any ancestor of
// it, is another user's home. It is purely lexical — no filesystem calls at
// all — so adding it costs a climb nothing beyond string work.
func underOtherUserHome(dir string, refs homeReferences) bool {
	if dir == "" || len(refs.containers) == 0 {
		return false
	}
	cur := filepath.Clean(dir)
	for depth := 0; depth < MaxAncestorDepth; depth++ {
		if isOtherUserHome(cur, refs) {
			return true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return false
		}
		cur = parent
	}
	return false
}
