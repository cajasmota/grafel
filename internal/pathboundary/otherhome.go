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
// # What "the current user's home" means: the real uid, and NOTHING else
//
// The reference is the REAL uid, and it is the only reference. os/user.Current
// resolves the running process's uid through the platform password database
// (getpwuid on darwin/linux, the token's profile directory on Windows) and
// never consults the environment.
//
// $HOME is deliberately NOT consulted here. An earlier revision took the union
// of the real-uid home and $HOME on the grounds that the hint could only widen
// what counts as ours; review showed that widening IS the bypass. With
// HOME=/Users/bob and the real uid alice, bob's entire home became "ours" and
// a climb from bob/proj/pkg walked twelve directories out to the filesystem
// root. An environment variable is strictly WEAKER evidence than an explicit
// path, and the owner's ruling already rejects the explicit path as
// authorisation — so a security boundary any env var can widen is not one.
// The exposure sat exactly at the home root, which is precisely what a
// container entrypoint or a HOME-preserving sudo sets.
//
// $HOME keeps its OTHER job unchanged: ClimbWithHome's home parameter still
// decides where the climb stops when it starts inside the user's own home
// (#6550). That is a stop condition, not an authorisation, so widening it can
// only ever read LESS. Note that the home parameter is likewise not consulted
// by this class: a caller could otherwise launder a foreign home through it.
// Today ClimbWithHome's only production caller is Climb, so nothing exercises
// that path, but the class does not rely on that staying true.
//
// When the password database has no entry for the uid (a cgo-less static
// binary in a container with no /etc/passwd row) the real-uid home is empty
// and only the platform-conventional containers below remain in force. That is
// a narrowing of this boundary, never a widening of it.
//
// # What counts as another user's home
//
// A directory D is another user's home when D's PARENT is a home CONTAINER, D
// is not the current user's home, and D's name is not on the platform's
// not-a-home list. The comparison is whole-component and uses the filesystem's
// case semantics — never a string prefix, so /home/alice2 is not read as being
// inside /home/alice (#6579 was the adjacent case-sensitivity instance of the
// same class of bug).
//
// Both the literal and the symlink-RESOLVED spelling of every home and every
// container are held, because classifyDir tests a resolved path against this
// set. Holding only literals made the whole class silently inert whenever the
// home's path crossed a symlink — on macOS that is every t.TempDir (/var →
// /private/var), and in production /System/Volumes/Data/Users/... and any NFS
// automount. Inside() already resolved both sides; this now matches it.
//
// # Which directories are home containers
//
// A container must be PLAUSIBLE, not merely the parent of a home: its own name
// is "Users" or "home" (case-folded), or it is one of the platform
// conventions. Deriving a container from any home's parent looked simpler and
// was wrong in a way that broke real setups:
//
//   - macOS root's home is /var/root, so the parent is /var — which made
//     /var/folders (the macOS TMPDIR), /var/log and /var/tmp all "other users'
//     homes" and made `sudo grafel index` refuse every climb.
//   - a Linux service account homed at /var/lib/postgresql yields /var/lib,
//     which refuses /var/lib/jenkins/workspace/proj.
//
// Guarding "a filesystem root is never a container" was not enough: it only
// caught the Linux spelling /root, one level deep. The plausibility rule is
// about the container's identity rather than a hardcoded home path, so both
// system-home shapes above fall out of it, and the root case with them.
//
// # How it composes with the media/TCC class split (v0.3.1)
//
// It is a third class refused AT OR UNDER, like classMedia and unlike
// classTCC's outermost-only rule — and it is evaluated FIRST. At-or-under is
// what makes it compose: the nesting bug review caught once (~/Library/Mobile
// Documents is not "outermost", so a climb starting inside iCloud Drive
// visited both iCloud directories) is an artefact of the outermost rule, and
// this class never uses it.

// homeReferences is one process's answer to "whose home is whose", computed
// once per climb rather than once per level. Every entry is held in both its
// literal and its symlink-resolved spelling.
type homeReferences struct {
	// current holds every spelling of the current user's home.
	current []string
	// containers holds directories that hold user homes as direct children.
	containers []string
}

// realUIDHome is the single reference, as a swappable func so a test can
// construct a synthetic /Users-like layout — and so the case where the real
// uid and $HOME DISAGREE can be built and asserted on.
var (
	homeRefsMu  sync.RWMutex
	realUIDHome = func() string {
		u, err := user.Current()
		if err != nil || u == nil || u.HomeDir == "" {
			return ""
		}
		return filepath.Clean(u.HomeDir)
	}
)

// OverrideHomeReferences replaces the current user's home and returns a restore
// func. It is a TEST SEAM, exported because the killing tests for this boundary
// must drive REAL climbers in OTHER packages (gitmeta.HasGitDirInTree,
// mcp.groupFromCWD) against a synthetic layout in a TempDir — unit-testing the
// predicate in this package does not pin the call site (#6533). It is also the
// only way a test can point the boundary somewhere safe, now that $HOME is not
// consulted. Production code must never call it.
func OverrideHomeReferences(realUID string) (restore func()) {
	homeRefsMu.Lock()
	prev := realUIDHome
	realUIDHome = func() string { return cleanOrEmpty(realUID) }
	homeRefsMu.Unlock()
	return func() {
		homeRefsMu.Lock()
		realUIDHome = prev
		homeRefsMu.Unlock()
	}
}

func cleanOrEmpty(p string) string {
	if p == "" {
		return ""
	}
	return filepath.Clean(p)
}

// resolveHomeReferences builds the reference set described above.
func resolveHomeReferences() homeReferences {
	homeRefsMu.RLock()
	realFn := realUIDHome
	homeRefsMu.RUnlock()

	var refs homeReferences
	appendUnique := func(list *[]string, p string) {
		p = cleanOrEmpty(p)
		if p == "" {
			return
		}
		for _, existing := range *list {
			if samePath(existing, p) {
				return
			}
		}
		*list = append(*list, p)
	}
	// bothSpellings adds the literal path and, when it differs, the
	// symlink-resolved one. classifyDir tests a resolved path against this
	// set, so holding only literals makes the class inert behind any symlink.
	bothSpellings := func(list *[]string, p string) {
		appendUnique(list, p)
		appendUnique(list, resolveOrSelf(cleanOrEmpty(p)))
	}

	home := cleanOrEmpty(realFn())
	if home != "" {
		bothSpellings(&refs.current, home)
		if parent := filepath.Dir(home); isPlausibleHomeContainer(parent) {
			bothSpellings(&refs.containers, parent)
		}
	}
	for _, c := range platformHomeContainers() {
		bothSpellings(&refs.containers, c)
	}
	return refs
}

// isPlausibleHomeContainer reports whether c is the kind of directory that
// holds user homes as direct children. See "Which directories are home
// containers" above for why the parent of a home is not enough on its own.
func isPlausibleHomeContainer(c string) bool {
	c = cleanOrEmpty(c)
	// A filesystem root is never a home container.
	if c == "" || filepath.Dir(c) == c {
		return false
	}
	switch base := filepath.Base(c); {
	case eqFold(base, "Users"), eqFold(base, "home"):
		return true
	}
	for _, p := range platformHomeContainers() {
		if samePath(p, c) {
			return true
		}
	}
	return false
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

// notAHomeNames lists children of a home container that are NOT user homes.
// Refusing them buys zero privacy — nobody's private files live there — while
// breaking real, common setups. Matching is case-folded because darwin's
// filesystem is (#6579 is the adjacent instance of getting that wrong).
//
//   - darwin: /Users/Shared is root-owned and group-writable and is the
//     conventional place for a shared checkout; Guest and .localized are not
//     homes at all.
//   - windows: C:\Users\Public is the direct analogue and is used by real
//     tooling; Default is the profile template, and "Default User" and
//     "All Users" are junctions to it and to ProgramData.
//   - linux: /home/linuxbrew/.linuxbrew holds Homebrew's prefix, taps and
//     repos on Linux — refusing it breaks them outright; lost+found is
//     filesystem bookkeeping.
func notAHomeNames() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"Shared", "Guest", ".localized"}
	case "windows":
		return []string{"Public", "Default", "Default User", "All Users"}
	default:
		return []string{"linuxbrew", "lost+found", "Public"}
	}
}

func isNotAHomeName(base string) bool {
	for _, n := range notAHomeNames() {
		if eqFold(base, n) {
			return true
		}
	}
	return false
}

// eqFold is a case-insensitive comparison used for the NAME heuristics above
// (container names, not-a-home names) on every platform. Unlike eq — which
// follows the filesystem's case semantics because it decides path IDENTITY —
// these are conventional names, and a case-sensitive filesystem that spells
// one differently is far likelier than a user genuinely named "Public".
func eqFold(a, b string) bool { return len(a) == len(b) && eqFoldASCII(a, b) }

func eqFoldASCII(a, b string) bool {
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
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
	if parent == dir || isNotAHomeName(filepath.Base(dir)) {
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
