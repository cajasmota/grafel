package walk

import (
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// irregularSkipRule reports whether a walked NON-DIRECTORY entry is something
// the indexer must not hand to an extractor, and the SkipEntry rule to record
// when it is.
//
// This exists because WalkRepo branched only on d.IsDir(): everything else was
// assumed to be a readable file. The comfortable version of that assumption —
// "a non-regular file simply fails the read and declares nothing" — is true of
// a directory and FALSE of a FIFO, which does not fail the read. It WAITS, in
// open(2), until a writer appears; nothing bounds that wait, so an indexing
// worker that picks up a FIFO named `Hang.vb` never finishes. No timeout, no
// error, no log line (#6416). A character device is the second shape of the
// same problem: /dev/zero opens fine and then never reaches EOF, so the read
// runs until memory does.
//
// The fix is therefore a FILE-TYPE check and not a timeout. A timeout would
// make the hang intermittent rather than absent, and would fire on a
// slow-but-legitimate read on a loaded machine or a network mount.
//
// SYMLINKS ARE RESOLVED WITH os.Stat, NOT os.Lstat, and the choice is
// load-bearing in both directions. filepath.WalkDir does not follow symlinks,
// so d.Type() is Lstat-shaped and answers ModeSymlink for a link to a regular
// file and a link to a FIFO alike. Deciding on that bit alone means either
// keeping the hang (accept every symlink) or losing real coverage (reject every
// symlink) — the indexer does mint a file entity for a symlink-to-file today,
// so blanket rejection would delete legitimate files from the index rather than
// being merely conservative. os.Stat follows the link and answers about the
// TARGET, and — unlike os.Open — stat(2) never blocks on a FIFO, so asking the
// question cannot reproduce the hang it is asked to prevent. A dangling or
// looping symlink returns an error in microseconds (ELOOP is ~29µs) and is
// skipped as unresolvable, which needs no special case of its own.
//
// It never opens the entry, so it cannot itself block.
func irregularSkipRule(absPath string, d fs.DirEntry) (string, bool) {
	mode := d.Type()
	if mode&os.ModeSymlink != 0 {
		fi, err := os.Stat(absPath)
		if err != nil {
			return "irregular:unresolvable", true
		}
		mode = fi.Mode()
	}
	if mode.IsRegular() {
		return "", false
	}
	return "irregular:" + irregularKind(mode), true
}

// irregularKind names the entry type for the skip report. The names are
// human-facing: the point of reporting the skip at all is that a reader can
// tell WHY a file vanished from the index, and "named-pipe" says that where a
// raw mode bitmask does not.
func irregularKind(mode fs.FileMode) string {
	switch {
	case mode.IsDir():
		return "directory"
	case mode&os.ModeNamedPipe != 0:
		return "named-pipe"
	case mode&os.ModeDevice != 0:
		return "device"
	case mode&os.ModeSocket != 0:
		return "socket"
	default:
		// Doors on Solaris/illumos, and anything a future GOOS reports as
		// ModeIrregular. Unreadable-or-worse by the same argument.
		return "other"
	}
}

// IrregularSkipReport renders the one-line warning an index run prints when
// the entry-type gate dropped files, or "" when it dropped none.
//
// It is separate from PrintSkipped on purpose. PrintSkipped is opt-in
// (`--print-skipped`) and carries the routine skips — vendor/, node_modules/,
// every gitignored tree — where one more line is noise. A non-regular file
// with an INDEXED extension is not routine: it is a source file the user can
// see in their tree that produced no entities, and #6338 is the standing
// evidence that a silent omission of that shape costs more to diagnose later
// than a line costs to print now. So this line is always on.
//
// The report is capped and aggregated for the same reason the unsupported
// report is: a listing long enough to scroll past reports nothing.
func IrregularSkipReport(skipped []SkipEntry) string {
	const maxNamed = 3
	var names []string
	total := 0
	for _, s := range skipped {
		if !strings.HasPrefix(s.Rule, "irregular:") {
			continue
		}
		total++
		if len(names) < maxNamed {
			names = append(names, fmt.Sprintf("%s (%s)", s.AbsPath, strings.TrimPrefix(s.Rule, "irregular:")))
		}
	}
	if total == 0 {
		return ""
	}
	msg := fmt.Sprintf("grafel: skipped %d non-regular file(s) — not indexed because reading one can block forever (#6416): %s",
		total, strings.Join(names, ", "))
	if total > len(names) {
		msg += fmt.Sprintf(", and %d more", total-len(names))
	}
	return msg
}
