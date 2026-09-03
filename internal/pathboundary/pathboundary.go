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
//     can never produce an unbounded climb, and
//  5. a PROTECTED directory — one protectedpath refuses — which is not visited,
//     and nothing above it is either. See "The protected boundary" below.
//
// # The protected boundary (#6548 arm 3)
//
// #6549 built the protected-path table and #6550 built the bounded loop, and
// for two arms they never met: the climb was bounded by $HOME, the root and a
// depth cap, yet it still READ ~/Documents or ~/Library/Mobile Documents when
// the ascent passed through one. Climb now consults the table, and refusing
// means STOP, not skip: a protected directory is a boundary of the same kind as
// $HOME, not a hole in the middle of the climb. Skipping it and continuing
// would put grafel above ~/Documents on the strength of a path it was just told
// not to read.
//
// The granularity of the refusal is per CLASS, because protectedpath's two
// classes carry different policies and a single uniform rule gets one of them
// wrong:
//
//   - classMedia (~/Library — which contains Mobile Documents, i.e. iCloud
//     Drive — plus ~/Music, ~/Movies, ~/Photos, ~/Pictures and any
//     *.photoslibrary-style bundle) is refused AT OR UNDER. protectedpath
//     refuses this class even for an explicitly registered repo root (#5296:
//     descending pops the Music/Photos prompt), so there is no exemption to
//     preserve and the climb visits nothing inside it.
//   - classTCC (~/Documents, ~/Desktop, ~/Downloads, ~/Public) is refused at
//     the OUTERMOST directory of the tree — ~/Documents itself, not
//     ~/Documents/proj.
//
// The class split is load-bearing in both directions. Applying the outermost
// rule uniformly disarms the nested entry that matters most: ~/Library is
// protected, so ~/Library/Mobile Documents is not outermost, and a climb
// starting inside iCloud Drive would visit the iCloud directories and stop only
// at ~/Library. It also silently extends the TCC exemption to media bundles,
// which protectedpath explicitly refuses to grant. Conversely, applying the
// at-or-under rule uniformly would make a climb from ~/Documents/proj/pkg stop
// at its first step and silently break a repo the user deliberately keeps
// there — every ancestor of such a path is itself "at or under ~/Documents".
// The outermost-only rule for classTCC is what keeps the explicit-path
// exemption (below) real: that repo resolves its own .git and .grafel markers
// through its own ancestors, and the climb stops the moment it would step out
// of the repo's tree into the TCC-gated folder itself.
//
// What this boundary is NOT known to do: prove that the reported iCloud
// consent dialog is gone. #6548's own site enumeration identifies
// canonicalizePath's os.ReadDir of every ancestor as the prompt-causing
// operation, and the climbs routed through here perform lstat-class calls
// (filepath.EvalSymlinks, os.Stat of a named marker), which do not generally
// trip the file-provider dialog. This boundary hardens inferred traversal and
// removes the reads that had no business happening; whether it is what the
// reporting user saw is unproven.
//
// # Another user's home (#6548 requirement 3)
//
// A climb also stops at, and never enters, a home directory that is not the
// current user's — /Users/<other>, /home/<other>, C:\Users\<other>. This
// package used to document the opposite ("another user's tree a user
// explicitly pointed grafel at must keep climbing"); the owner ruled that
// position wrong on 2026-09-02, because an explicit path is a claim about
// INTENT, not about PERMISSION, and reading another user's files is not made
// harmless by the user having typed the path. See otherhome.go for the rule,
// what "the current user's home" means without trusting $HOME, and how the
// class composes with classMedia and classTCC.
//
// The remaining exemption is narrower than it was: the rule still does not
// constrain an explicitly user-supplied path INSIDE THE CURRENT USER'S OWN
// home. Pointing grafel at a repo in ~/Documents stays a legitimate
// instruction; pointing it at one in another user's home no longer resolves
// that repo's ancestors.
//
// A checkout under a non-home root — /opt, /srv, a CI workspace — is
// unaffected and keeps its full climb: only an actual home, the current user's
// or somebody else's, is a boundary.
//
// When the home directory cannot be determined (os.UserHomeDir fails because
// neither $HOME nor %USERPROFILE% is set — a bare cron/launchd/container
// environment) the home boundary is simply absent; the root stop and the depth
// cap still apply. That is a fail-open on ONE of five stop conditions, chosen
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

	"github.com/cajasmota/grafel/internal/protectedpath"
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

	homeReal := resolveOrSelf(home)
	refs := resolveHomeReferences(home)

	curClass := classifyDir(cur, home, homeReal, refs)
	for depth := 0; depth < MaxAncestorDepth; depth++ {
		parent := filepath.Dir(cur)
		atRoot := parent == cur
		var parentClass dirClass
		if !atRoot {
			parentClass = classifyDir(parent, home, homeReal, refs)
		}
		// classOtherHome: refused at or under — another user's home is not
		// ours to read, whatever path the caller supplied (#6548 req 3).
		// classMedia: refused at or under, with no exemption to preserve.
		// classTCC: refused at the outermost directory of the tree only — a
		// level whose own parent is TCC-protected is inside a tree the caller
		// was pointed at, and is visited normally.
		if curClass.otherHome || curClass.media || (curClass.tcc && !parentClass.tcc) {
			return false
		}
		if visit(cur) {
			return true
		}
		if bounded && samePath(cur, home) {
			// $HOME itself was visited; nothing above it is ours to read.
			return false
		}
		if atRoot {
			// Filesystem root (or a Windows drive/UNC root).
			return false
		}
		cur, curClass = parent, parentClass
	}
	return false
}

// dirClass is one directory's verdict from protectedpath, split by the class
// distinction the refusal rule needs. Both false means "not protected".
type dirClass struct {
	// media — protectedpath refuses this even for an explicitly registered
	// repo root (#5296). Refused at or under.
	media bool
	// tcc — protected against INFERRED traversal, but a legitimate place to
	// keep a repo. Refused at the outermost directory of the tree only.
	tcc bool
	// otherHome — at or under a home directory that is not the current
	// user's (#6548 req 3). Refused at or under, and checked FIRST: an
	// explicit path is a claim about intent, not about permission.
	otherHome bool
}

// classifyDir asks protectedpath — grafel's single authority on what must not
// be read — which class, if any, dir falls into. IsWalkProtectedIn is the media
// question and IsTraversalProtectedIn the full union, so a path in the union
// but not in media is exactly classTCC.
//
// # Cost
//
// This is not free, and the cost is worth stating plainly rather than burying:
// a climb calls it once per level, and each call runs one filepath.EvalSymlinks
// on dir (an lstat chain) plus, when the gate passes, protectedpath's own
// resolution of dir and of every denylist entry it compares against. The old
// unbounded loops did no filesystem work of their own at all.
//
// The gate in front keeps that from landing on every level of every climb, but
// it does NOT make an unprotected path free: resolving dir happens first,
// because a symlink outside $HOME can resolve into a protected tree and a
// lexical check would miss it — which is the case protectedpath resolves
// symlinks for in the first place. What the gate does buy is that the
// per-denylist-entry comparison (nine more resolutions on darwin) is skipped
// unless dir or its resolved form is inside $HOME or names a media-library
// bundle.
//
// One lstat chain per level, on climbs that are ≤ MaxAncestorDepth levels and
// typically around ten, is judged an acceptable price: both routed callers
// already ran an EvalSymlinks per level themselves, and every other climber
// already ran an os.Stat per level inside its own visit function. The
// alternative — reading a TCC-gated directory — is the defect.
func classifyDir(dir, home, homeReal string, refs homeReferences) dirClass {
	if dir == "" {
		return dirClass{}
	}
	resolved := resolveOrSelf(dir)
	// Another user's home is checked first and on BOTH spellings: a symlink
	// outside every home that resolves into one is the same read. This costs
	// no filesystem work of its own — resolved is already in hand and
	// underOtherUserHome is purely lexical.
	if underOtherUserHome(dir, refs) || underOtherUserHome(resolved, refs) {
		return dirClass{otherHome: true}
	}
	if !mayBeProtected(dir, resolved, home, homeReal) {
		return dirClass{}
	}
	if media, _ := protectedpath.IsWalkProtectedIn(dir, home, runtime.GOOS); media {
		return dirClass{media: true}
	}
	if traversal, _ := protectedpath.IsTraversalProtectedIn(dir, home, runtime.GOOS); traversal {
		return dirClass{tcc: true}
	}
	return dirClass{}
}

// mayBeProtected is the cheap gate described in classifyDir's Cost section:
// only a path at or under $HOME, or one naming a media-library bundle, can be
// protected at all. Both the literal and the symlink-resolved spelling are
// tested, so a link outside $HOME that resolves into a protected tree is not
// waved through.
func mayBeProtected(dir, resolved, home, homeReal string) bool {
	if protectedpath.IsMediaLibraryBundle(filepath.Base(dir)) ||
		protectedpath.IsMediaLibraryBundle(filepath.Base(resolved)) {
		return true
	}
	if home == "" {
		return false
	}
	return containedIn(filepath.Clean(dir), filepath.Clean(home)) ||
		containedIn(filepath.Clean(resolved), filepath.Clean(homeReal))
}

// resolveOrSelf returns path with symlinks resolved, or path unchanged when it
// cannot be resolved (a broken link, a race, a path that does not exist yet).
func resolveOrSelf(path string) string {
	if path == "" {
		return path
	}
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(r)
	}
	return path
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
