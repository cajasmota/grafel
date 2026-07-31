// Package atomicfile writes files atomically: into a UNIQUE temp file in the
// destination's own directory, then rename over the destination.
//
// # Why this package exists (#6018)
//
// The idiom this replaces was written by hand across ~48 sites:
//
//	tmp := path + ".tmp"
//	os.WriteFile(tmp, b, 0o644)
//	os.Rename(tmp, path)
//
// The temp name there is DETERMINISTIC PER DESTINATION, so it is shared by
// every writer aiming at that destination. Two concurrent writers both O_TRUNC
// the same temp inode and interleave their bytes into it; the destination can
// then be left holding a torn mix of two payloads, and whoever renames second
// fails with ENOENT because the first already moved the file away. The damaging
// half is not the lost write — it is the garbled one, which the next reader
// treats as truth. Where the rename error is discarded (several best-effort
// caches do that deliberately) the collision is entirely invisible at runtime.
//
// That bug was independently diagnosed and point-fixed three times before this
// package existed: internal/statusfile (review #5734),
// internal/daemon/watchreg, and internal/links (#5978). This is the shared
// remedy so there is no fourth.
//
// # Mode semantics — deliberately NOT umask-masked
//
// os.WriteFile passes perm through open(2), so the process umask narrows the
// resulting mode. os.CreateTemp always creates the file 0600 and the Chmod
// below then sets EXACTLY the requested perm, so the umask does not apply.
// Under a restrictive umask (077) a converted destination therefore widens
// from 0600 to 0644.
//
// This is a deliberate, one-time decision (#5978, generalised in #6018) and is
// not to be re-litigated per call site. The alternative — reading the umask to
// mask perm by hand — requires syscall.Umask, which is process-global, has no
// read-only form, and is racy against any concurrent file creation in the
// process. The destinations here are per-user state under the grafel home, and
// a mode that does not depend on which process (daemon, CLI, or child) happened
// to write the file is easier to reason about than one that does. Callers that
// need a genuinely private file must pass 0o600 explicitly rather than relying
// on a umask to narrow 0o644 for them.
//
// The Chmod is load-bearing and easy to delete without breaking anything
// visible — deleting it left the whole of #5978's package green. It is pinned
// by TestWriteFile_AppliesPerm; keep that test honest.
//
// # Other ways this differs from os.WriteFile
//
// All of these follow from rename-over-destination, so the hand-written
// temp+rename code this replaced behaved the same way. They are listed because
// they differ from the os.WriteFile the call sites LOOK like they still use.
//
//   - The destination directory must already exist. WriteFile does not
//     MkdirAll; os.CreateTemp fails on a missing directory exactly as
//     os.WriteFile would. Call sites keep their own MkdirAll.
//   - A SYMLINK at path is REPLACED by a regular file, not written through to
//     its target. os.WriteFile follows the link.
//   - A READ-ONLY (0444) destination is REPLACED. os.WriteFile fails with
//     EACCES. What governs is write permission on the DIRECTORY, not on the
//     existing file.
//
// # Orphaned temp files after a crash
//
// A deterministic path+".tmp" was self-healing across crashes: the next write
// simply truncated the orphan. Unique names give that up — a process killed
// between CreateTemp and Rename leaves a distinct `.<name>.tmp-<random>`
// behind, and nothing sweeps `*.tmp-*` in any of the destination directories.
//
// Accepted deliberately: the leak is bounded by crash frequency, the files are
// small, and the alternative is the torn-write bug. #5978 made the same trade
// silently; it is written down here so the next reader does not have to
// rediscover it. A sweeper belongs with whatever GCs these directories, not in
// the write path.
package atomicfile

import (
	"os"
	"path/filepath"
)

// WriteFile writes b to path atomically and sets path's mode to perm.
//
// The temp file is created by os.CreateTemp in path's OWN directory, so the
// rename stays within one filesystem and is therefore atomic. It is removed on
// every error path.
//
// WriteFile does NOT create the destination directory: os.CreateTemp fails on a
// missing directory exactly as os.WriteFile would have. Callers that need the
// directory keep their existing os.MkdirAll.
//
// perm is applied verbatim and is NOT masked by the process umask — see the
// package doc.
func WriteFile(path string, b []byte, perm os.FileMode) (err error) {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()
	if _, err = f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	// os.CreateTemp always uses 0600; set the caller's intended mode.
	if err = os.Chmod(tmp, perm); err != nil {
		return err
	}
	// Not os.Rename: on Windows that alone cannot replace a read-only
	// destination and loses races against other handles. See rename.go.
	err = renameAtomic(tmp, path)
	return err
}
