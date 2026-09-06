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
// COST. Nothing here stats the common case: d.Type() is the type byte readdir
// already returned, so a regular file, a FIFO, a device and a socket are all
// decided for free. os.Stat is called for symlinks ONLY. (An earlier version
// of this comment claimed the gate was placed after the extension filter to
// amortise a per-file stat; there is no per-file stat to amortise, and the
// placement is justified by which SKIP REASON a path is reported under, not by
// cost — see walker.go.)
//
// It never opens the entry, so it cannot itself block. That leaves a TOCTOU
// residual — a regular file swapped for a FIFO between this check and the
// extractor's read — which this gate cannot close by construction. The read
// side closes it: internal/safeio opens with O_NONBLOCK and re-checks the type
// with fstat on the descriptor.
func irregularSkipRule(absPath string, d fs.DirEntry) (string, bool) {
	return irregularSkipRuleMode(absPath, d.Type())
}

// irregularSkipRuleMode is irregularSkipRule's body, taking the LSTAT-SHAPED
// type bits directly instead of an fs.DirEntry.
//
// Split out so a caller that holds a PATH rather than a walk entry can ask the
// walker the same question and get the same answer — see IndexableEntryType.
// The two must not be able to drift: the poller in internal/daemon/watch
// admits a git-reported path to its candidate set only if the walker would
// index it, and any divergence is a file one half indexes and the other half
// never notices changing (#6932 review, MA-1).
func irregularSkipRuleMode(absPath string, mode fs.FileMode) (string, bool) {
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
	if mode.IsDir() {
		// A symlink to a DIRECTORY lands here: WalkDir does not follow it, so
		// d.IsDir() was false and the entry reached the file branch. Skipping
		// it is right — the old behaviour handed a directory to an extractor,
		// which failed the read downstream — but it gets a rule OUTSIDE the
		// irregular: namespace on purpose. The always-on warning is worded
		// "reading one can block forever", and that is simply untrue of a
		// directory; symlink farms are common, and an unsuppressable line
		// telling a user their symlinked package directory is a hang hazard
		// would be a false alarm at exactly the scale that trains people to
		// ignore the warning (#6416 review).
		return "symlink-to-directory", true
	}
	return "irregular:" + irregularKind(mode), true
}

// IndexableEntryType reports whether the entry at absPath is one WalkRepo would
// hand to an extractor, deciding from the path alone.
//
// It is the walker's own entry-type gate, reached from outside the walk: it
// Lstats the path to recover the type bits filepath.WalkDir would have handed
// irregularSkipRule, then applies that identical rule — including the os.Stat
// resolution of a symlink, so a link to a regular file is indexable and a
// dangling link, a link to a directory, and a FIFO are not.
//
// WHY IT IS SHARED RATHER THAN REIMPLEMENTED (#6932 review, MA-1). The polling
// change-detector needs exactly this predicate to decide whether a path git
// reported can ever acquire a manifest stamp. Its first version asked
// os.Lstat().Mode().IsRegular() instead, which answers about the LINK rather
// than its target, so a newly created symlink-to-source in a tracked directory
// was refused as a candidate while WalkRepo indexed it with no skip entry: a
// file the index contains and the poller can never see change. Two predicates
// answering "would grafel index this?" is one predicate too many, and the
// agreement is asserted over an enumerated entry-type space rather than
// assumed — see TestExistsOnDisk_AgreesWithWalker.
//
// A path that does not exist, or that cannot be Lstat-ed at all, is not
// indexable. It never opens the entry, so — like the gate it delegates to — it
// cannot itself block on a FIFO.
func IndexableEntryType(absPath string) bool {
	fi, err := os.Lstat(absPath)
	if err != nil {
		return false
	}
	_, skip := irregularSkipRuleMode(absPath, fi.Mode().Type())
	return !skip
}

// irregularKind names the entry type for the skip report. The names are
// human-facing: the point of reporting the skip at all is that a reader can
// tell WHY a file vanished from the index, and "named-pipe" says that where a
// raw mode bitmask does not.
func irregularKind(mode fs.FileMode) string {
	switch {
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
