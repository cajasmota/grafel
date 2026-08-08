package atomicfile

// writethrough.go — the symlink-following, mode-preserving counterpart to
// WriteFile, for the destinations grafel does NOT own.
//
// # Why a second function rather than a flag on WriteFile
//
// WriteFile's contract says a symlink at the destination is replaced and perm is
// applied verbatim, and says both are deliberate and "not to be re-litigated per
// call site" (see the package doc). That is the right contract for a file under
// the grafel home, where grafel decides the mode and nothing else writes there.
// It is precisely the wrong one for a user-owned dotfile, and #6240 showed what
// happens when the wrong one is used: a ~/.claude.json symlinked into a dotfiles
// repo detaches on the first install, and a 0600 file holding an OAuth token
// comes back 0644 with no diff to notice.
//
// It cannot be WriteFile with a bool, because resolving the destination can
// FAIL — an unresolvable symlink chain must stop the write — and WriteFile has
// no way to report that before it starts. So: a separate function, WriteFile's
// contract untouched, and a test (TestWriteThrough_IsTheInverseOfWriteFile) that
// states the contrast in one place so a caller choosing between them does not
// have to infer it from two package docs.
//
// # What is shared and what is not
//
// The genuinely subtle, drift-prone parts live here: the hop budget and its
// kernel rationale, the error on an unresolvable chain, and Stat-not-Lstat for
// the destination's mode. The MODE POLICY stays at each call site, because it is
// not the same everywhere and pretending otherwise is how a hook script ends up
// non-executable: a credential-bearing config grafel invents wants 0600, a
// project settings.json wants 0644, and a grafel-generated script wants a FIXED
// 0755 with no preservation at all. That last one uses ResolveWriteTarget +
// WriteFile directly rather than WriteThrough, which makes "the mode here is
// grafel's to state" visible at the call site instead of hidden in an argument.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrSymlinkChain classifies a destination whose symlink chain cannot be
// resolved: a cycle, or a chain longer than any kernel would follow. It exists
// so a caller can tell "the user's path is broken" from "the write failed".
var ErrSymlinkChain = errors.New("unresolvable symlink chain")

// MaxSymlinkHops bounds ResolveWriteTarget's walk.
//
// 40 matches Linux's MAXSYMLINKS, deliberately the most permissive of the kernel
// limits: anything the OS itself would follow must resolve here too. 32
// (macOS's limit) would mean a 33-39 link chain that resolves fine for the
// kernel runs out of budget here — and a budget-exhausted answer is an error, so
// that user's install would fail for no reason. macOS stops at 32 and answers
// ELOOP first, which is why a local probe cannot see the difference; it is a
// Linux-only regression on a repo whose CI runs three OSes.
const MaxSymlinkHops = 40

// ResolveWriteTarget follows a symlink chain at path and returns the real file a
// write must land on.
//
// Why it exists: temp-file + rename replaces the LINK INODE, so a config
// symlinked into a dotfiles repo silently detaches from that repo on the first
// write — the user keeps editing the repo copy while the tool reads a regular
// file that no longer tracks it, and nothing reports the divergence (#6240,
// #6246). Resolving first also puts the temp file in the TARGET's directory, so
// the rename stays intra-filesystem and therefore atomic.
//
// A plain path, an absent path, a dangling symlink (the final target is returned
// even though it does not exist yet, which is what a create must use), and an
// unreadable link all resolve to a usable answer.
//
// An UNRESOLVABLE chain is an ERROR wrapping ErrSymlinkChain, and the returned
// path is empty. Returning the last hop reached looks harmless and is not: that
// path is ITSELF a symlink, so a caller renames over it and flattens one of the
// user's links into a regular file while reporting success. That is the exact
// damage this function exists to prevent, and it was live in the first cut of
// #6240's fix.
func ResolveWriteTarget(path string) (string, error) {
	cur := path
	for i := 0; i < MaxSymlinkHops; i++ {
		fi, err := os.Lstat(cur)
		if err != nil || fi.Mode()&os.ModeSymlink == 0 {
			return cur, nil
		}
		dest, err := os.Readlink(cur)
		if err != nil {
			return cur, nil
		}
		if !filepath.IsAbs(dest) {
			dest = filepath.Join(filepath.Dir(cur), dest)
		}
		cur = filepath.Clean(dest)
	}
	return "", fmt.Errorf("%s: %w: still unresolved after %d hops "+
		"(a cycle, or longer than any kernel will follow)",
		filepath.Base(path), ErrSymlinkChain, MaxSymlinkHops)
}

// ExistingPerm returns the mode a write to target must reproduce: the mode
// target already has, or ifAbsent when it does not exist.
//
// os.Stat, not os.Lstat, is deliberate. Callers pass an already-resolved path,
// but if one ever passes a link, Lstat reports the LINK's mode — 0777 on
// Linux — which is not the mode any content is stored at. Preserving that would
// be a worse version of the widening bug this whole exercise is about.
func ExistingPerm(target string, ifAbsent os.FileMode) os.FileMode {
	if fi, err := os.Stat(target); err == nil {
		return fi.Mode().Perm()
	}
	return ifAbsent
}

// WriteThrough writes b to path atomically, following any symlink at path
// through to its target and reproducing that target's existing mode. ifAbsent is
// the mode used only when there is nothing at the destination yet — pick it
// deliberately per call site; there is no safe default.
//
// The inverse of WriteFile on both axes. Use WriteFile for a file under the
// grafel home whose mode grafel decides; use this for a destination a user owns.
//
// Like WriteFile it does NOT create the destination directory.
func WriteThrough(path string, b []byte, ifAbsent os.FileMode) error {
	target, err := ResolveWriteTarget(path)
	if err != nil {
		return err
	}
	return WriteFile(target, b, ExistingPerm(target, ifAbsent))
}
