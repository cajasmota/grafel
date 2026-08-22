// Package pathboundary is the single boundary predicate for grafel's
// ancestor climbs — the "walk up from a directory looking for a marker" loops
// that resolve a group, a repo or a git root.
//
// # Why it exists (#6548)
//
// Every one of those climbs used to have exactly one stop condition: finding
// what it was looking for. When the marker was absent — a cwd outside any
// registered repo, a directory with no .git anywhere above it — the loop ran
// until filepath.Dir stopped changing, i.e. all the way to the filesystem root,
// reading $HOME, /Users and / on the way. On macOS that is what asked a user to
// approve access to files managed by iCloud Drive; on Linux and Windows the same
// climb walks other users' homes and system directories, silently.
//
// The fix is deliberately one predicate rather than three patched loops: the
// next climber someone adds inherits the boundary by using Climb instead of
// hand-rolling `for { ...; parent := filepath.Dir(cur); if parent == cur ... }`.
//
// # The boundary
//
// A climb stops at the FIRST of:
//
//  1. the visit function reporting success (unchanged behaviour),
//  2. the user's home directory, when the start path is inside it — $HOME is
//     visited, and the climb stops there. A repo under $HOME has no marker
//     above $HOME worth finding, and everything above it is either another
//     user's tree or the system,
//  3. the filesystem root (or a Windows drive root), as before, and
//  4. MaxAncestorDepth levels, a backstop so a symlink cycle or an unusual mount
//     can never produce an unbounded climb.
//
// Two things it deliberately does NOT do:
//
//   - It does not stop early at a directory that merely LOOKS like a home
//     (a child of /Users, /home, C:\Users). A checkout under another root —
//     /opt, /srv, a CI workspace, another user's tree a user explicitly pointed
//     grafel at — must keep climbing, or the repo silently stops resolving its
//     group. Only the ACTUAL resolved home is a boundary.
//   - It does not constrain an explicitly user-supplied path. The rule bounds
//     INFERRED traversal; pointing grafel at a repo inside ~/Documents stays a
//     legitimate instruction.
//
// When the home directory cannot be determined (os.UserHomeDir fails because
// neither $HOME nor %USERPROFILE% is set — a bare cron/launchd/container
// environment) the home boundary is simply absent; the root stop and the depth
// cap still apply. That is a fail-open on ONE of four stop conditions, chosen
// over guessing a home path that may belong to somebody else — and it is not
// silent: the first climb in such a process logs one line saying the home
// boundary is inactive and that only the root stop and the depth cap apply.
package pathboundary

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// MaxAncestorDepth caps how many levels any single climb may ascend, counting
// the start directory itself. Real paths on every supported platform are far
// shallower than this (a deeply nested monorepo module sits around 10-15); the
// cap exists purely so a symlink cycle, a recursive mount or a pathological
// path can never make a climb unbounded.
const MaxAncestorDepth = 64

// UserHome returns the user's home directory, cleaned, or "" when it cannot be
// determined. It never guesses.
func UserHome() string {
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		return ""
	}
	return filepath.Clean(h)
}

// noHomeOnce makes the degraded-boundary diagnostic fire once per process. A
// per-climb line would print on every MCP call that resolves a cwd.
var noHomeOnce = new(sync.Once)

// logHome is the diagnostic sink, swappable in tests. It writes to stderr:
// grafel's MCP transport owns stdout, and a line there would corrupt the
// protocol.
var logHome = func(msg string) { log.Print(msg) }

// warnHomeBoundaryInactive announces, exactly once per process, that the home
// boundary is not in force. A guard that degrades silently reads as "protected"
// forever after, which is the failure mode this package exists to end — so the
// one case where the boundary is absent says so out loud.
func warnHomeBoundaryInactive() {
	noHomeOnce.Do(func() {
		logHome("grafel: pathboundary: home directory could not be determined " +
			"(os.UserHomeDir failed: neither $HOME nor %USERPROFILE% is set); the $HOME " +
			"boundary on ancestor climbs is INACTIVE for this process. Climbs remain " +
			"bounded by the filesystem root and a depth cap of " +
			strconv.Itoa(MaxAncestorDepth) + " levels. Set $HOME to restore the home boundary.")
	})
}

// Climb visits dir and then each of its ancestors, nearest first, stopping at
// the boundary described in the package doc. visit reports success by returning
// true, which ends the climb; Climb returns whether any visit succeeded.
//
// This is the only ancestor-walk primitive grafel's climbers should use.
func Climb(dir string, visit func(dir string) bool) bool {
	home := UserHome()
	if home == "" {
		warnHomeBoundaryInactive()
	}
	return ClimbWithHome(dir, home, visit)
}

// ClimbWithHome is Climb with the home boundary supplied explicitly. Production
// code calls Climb; this exists so a test can inject a TempDir home instead of
// reading — or worse, planting fixtures in — the developer's real one.
//
// An empty home disables the home boundary (root stop and depth cap remain).
func ClimbWithHome(dir, home string, visit func(dir string) bool) bool {
	if dir == "" || visit == nil {
		return false
	}
	cur := filepath.Clean(dir)
	if home != "" {
		home = filepath.Clean(home)
	}
	// The home boundary applies only to a climb that STARTS inside the home
	// directory. A start path elsewhere on disk keeps its full climb.
	bounded := home != "" && Inside(cur, home)

	for depth := 0; depth < MaxAncestorDepth; depth++ {
		if visit(cur) {
			return true
		}
		if bounded && samePath(cur, home) {
			// $HOME itself was visited; nothing above it is ours to read.
			return false
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			// Filesystem root (or a Windows drive/UNC root).
			return false
		}
		cur = parent
	}
	return false
}

// Inside reports whether path is home itself or lives underneath it. It is the
// containment half of the boundary, exported because a caller that is not
// climbing (a one-level parent scan, say) still needs the same answer.
func Inside(path, home string) bool {
	if path == "" || home == "" {
		return false
	}
	if containedIn(filepath.Clean(path), filepath.Clean(home)) {
		return true
	}
	// Retry through resolved symlinks: on macOS /var is a link to /private/var
	// and a home reached through a link would otherwise compare unequal. Only a
	// successful resolution of BOTH sides is meaningful.
	rp, err1 := filepath.EvalSymlinks(path)
	rh, err2 := filepath.EvalSymlinks(home)
	if err1 != nil || err2 != nil {
		return false
	}
	return containedIn(filepath.Clean(rp), filepath.Clean(rh))
}

// containedIn does the string-level containment on already-cleaned paths.
func containedIn(path, home string) bool {
	if eq(path, home) {
		return true
	}
	prefix := home
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	if caseInsensitiveFS() {
		return strings.HasPrefix(strings.ToLower(path), strings.ToLower(prefix))
	}
	return strings.HasPrefix(path, prefix)
}

// samePath compares two cleaned paths with the filesystem's case semantics.
func samePath(a, b string) bool { return eq(filepath.Clean(a), filepath.Clean(b)) }

func eq(a, b string) bool {
	if caseInsensitiveFS() {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func caseInsensitiveFS() bool {
	return runtime.GOOS == "darwin" || runtime.GOOS == "windows"
}
