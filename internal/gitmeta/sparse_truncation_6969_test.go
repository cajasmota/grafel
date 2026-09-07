// Truncation of the sparse-checkout pattern file (#6969).
//
// safeio.ReadFile caps a read and returns the PREFIX with a nil error. For
// every other file this package reads that is loud — a 64 KiB prefix of HEAD
// is not "ref: ..." and every parser rejects it. The sparse pattern file is
// the one LIST-shaped reader, and a prefix of a list is a valid-looking
// shorter list: the repo stayed sparse with fewer includes and files left the
// index with no error, no SkipEntry and no missing-entity signal.
//
// It was also the only input class landing on the EXCLUSION side. #6967
// settled the other two deliberately toward inclusion (absent → not sparse,
// unreadable → not sparse); oversized went the other way.
//
// WHAT THESE TESTS OBSERVE. Not a counter and not "IsSparse is false" alone —
// a NAMED FILE that the truncated pattern set would have dropped from the
// index and that is now indexed. Two cut geometries are separate cases,
// because the two hazards in the comment on parseSparsePatternFile are
// different failures:
//
//   - a cut on a LINE BOUNDARY silently deletes every pattern after it;
//   - a cut MID-LINE additionally leaves a pattern git never wrote, which
//     matches something WIDER than intended rather than nothing.
//
// The pattern file here is git's own bytes with padding APPENDED. The config
// is still written by real git, so #6964's rule ("never hand-write a config
// value") holds where it is load-bearing — an over-cap pattern file is a shape
// no git will ever produce, so it has to be constructed.
//
// Measured on macOS/APFS. Every case reports the git it ran on.
package gitmeta_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/gitmeta"
)

// sparseCapBytes6969 is an INDEPENDENT LITERAL of maxSparsePatternBytes, not a
// reference to it. A test that reads the production constant cannot detect the
// constant being changed, and the whole point of the exactly-at-the-cap case
// below is that the cap is a byte position the code must get right.
// TestSparseCapConstantMatchesTheTestsLiteral (internal package) fails if the
// two ever diverge, so a deliberate cap change updates this on purpose instead
// of silently making these fixtures measure nothing.
const sparseCapBytes6969 = 8 << 20

// trNamed is a path that `git sparse-checkout set src` EXCLUDES and that a
// whole-file read of the padded fixture would INCLUDE (the padding is followed
// by a `/services/` pattern that only survives an untruncated read).
const trNamed = "services/orders/h.go"

// trInsideCone is inside the cone git actually set. Case B's corrupted pattern
// (`!/src`) excludes it, so it is the path that names hazard 2.
const trInsideCone = "src/a.go"

// trSparseFile returns the path of the pattern file git wrote.
func trSparseFile(t *testing.T, dir string) string {
	t.Helper()
	gitDir := strings.TrimSpace(rgRun(t, dir, "rev-parse", "--git-dir"))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(dir, gitDir)
	}
	p := filepath.Join(gitDir, "info", "sparse-checkout")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("fixture premise broken: git wrote no %s: %v", p, err)
	}
	return p
}

// trSparseFixture builds a repo, makes it sparse with real git, and asserts
// the PREMISE the whole file rests on: trNamed is excluded and trInsideCone is
// included right now. Without that, every assertion below is satisfied by a
// fixture that was never sparse in the first place.
func trSparseFixture(t *testing.T) (dir string, patternFile string, gitWrote []byte) {
	t.Helper()
	dir = rgFixture(t)
	si := probeAfterSparseSet(t, dir, "src")

	if gitmeta.IsPathIncluded(si, trNamed) {
		t.Fatalf("fixture premise broken: %s is INCLUDED by the cone git just set (patterns=%v) — nothing below can observe it being dropped", trNamed, si.Patterns)
	}
	if !gitmeta.IsPathIncluded(si, trInsideCone) {
		t.Fatalf("fixture premise broken: %s is EXCLUDED by the cone git just set (patterns=%v)", trInsideCone, si.Patterns)
	}

	patternFile = trSparseFile(t, dir)
	b, err := os.ReadFile(patternFile)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(b)) >= sparseCapBytes6969 {
		t.Fatalf("fixture premise broken: git's own pattern file is already %d bytes, at or over the %d cap", len(b), sparseCapBytes6969)
	}
	t.Logf("%s | git wrote %d bytes of patterns: %q", rgVersion(t, dir), len(b), string(b))
	return dir, patternFile, b
}

// trPadTo appends comment lines to base until the content is EXACTLY n bytes.
// Comments are used for the filler so that the padding itself contributes no
// pattern: everything these tests observe comes from the real patterns and
// from the tail line, never from the filler.
func trPadTo(t *testing.T, base []byte, n int) []byte {
	t.Helper()
	if len(base) > n {
		t.Fatalf("cannot pad %d bytes down to %d", len(base), n)
	}
	const line = "# pad-6969 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"
	out := make([]byte, 0, n)
	out = append(out, base...)
	for len(out)+len(line) <= n {
		out = append(out, line...)
	}
	// One short comment closes the remainder exactly. "#" plus filler plus the
	// newline needs at least 2 bytes; the loop above leaves fewer than
	// len(line) so the remainder is small, and a 1-byte remainder is closed
	// with a bare newline instead.
	rem := n - len(out)
	switch {
	case rem == 0:
	case rem == 1:
		out = append(out, '\n')
	default:
		out = append(out, '#')
		for len(out) < n-1 {
			out = append(out, 'b')
		}
		out = append(out, '\n')
	}
	if len(out) != n {
		t.Fatalf("padding produced %d bytes, want %d", len(out), n)
	}
	return out
}

// TestSparse_OversizedPatternFile_LineBoundaryCut_IndexesInFull is hazard 1:
// the cut falls on a line boundary, so the truncated read is a syntactically
// perfect but SHORTER pattern list. Nothing about it looks wrong, and the
// patterns past the cut — here `/services/`, which is what puts trNamed back
// in the index — are simply gone.
func TestSparse_OversizedPatternFile_LineBoundaryCut_IndexesInFull(t *testing.T) {
	dir, patternFile, gitWrote := trSparseFixture(t)

	// Exactly cap bytes of git's patterns + comment filler, then the pattern
	// that only an untruncated read ever sees.
	content := trPadTo(t, gitWrote, sparseCapBytes6969)
	content = append(content, "/services/\n"...)
	if err := os.WriteFile(patternFile, content, 0o644); err != nil {
		t.Fatal(err)
	}

	// The cut is on a line boundary: byte sparseCapBytes6969-1 is the newline
	// that ends the last filler line. Asserted, because "line boundary" is the
	// axis that separates this case from the mid-line one and a fixture whose
	// label outruns its content grades neither.
	if got := content[sparseCapBytes6969-1]; got != '\n' {
		t.Fatalf("fixture is not the line-boundary geometry: byte %d is %q, want a newline", sparseCapBytes6969-1, got)
	}

	si := gitmeta.ProbeRepo(dir)
	if si.IsSparse {
		t.Fatalf("an oversized pattern file left the repo SPARSE with a truncated pattern set: patterns=%v (%d of them) — every pattern past byte %d was dropped and the index silently shrank (#6969)",
			si.Patterns, len(si.Patterns), sparseCapBytes6969)
	}
	if !gitmeta.IsPathIncluded(si, trNamed) {
		t.Fatalf("%s is still EXCLUDED from the index after an oversized pattern file (patterns=%v). This is the artefact the fix is about: a named file that silently left the index (#6969). %s",
			trNamed, si.Patterns, rgVersion(t, dir))
	}
	if !gitmeta.IsPathIncluded(si, trInsideCone) {
		t.Fatalf("%s is EXCLUDED after an oversized pattern file (patterns=%v)", trInsideCone, si.Patterns)
	}
	if len(si.Patterns) != 0 {
		t.Fatalf("a rejected pattern file must yield NO patterns, got %v", si.Patterns)
	}
}

// TestSparse_OversizedPatternFile_MidLineCut_DropsTheCorruptedPattern is
// hazard 2, and it is a DIFFERENT failure from the case above rather than a
// variant of it. The cut lands inside `!/src/sub/` and leaves `!/src` — not a
// pattern that fails to match, a pattern that matches MORE than the line git
// would ever have written. The file is rejected wholesale, so that fragment
// never reaches the matcher and trInsideCone stays in the index.
func TestSparse_OversizedPatternFile_MidLineCut_DropsTheCorruptedPattern(t *testing.T) {
	dir, patternFile, gitWrote := trSparseFixture(t)

	const tail = "!/src/sub/\n"
	const keep = len("!/src") // the fragment that survives the cut

	content := trPadTo(t, gitWrote, sparseCapBytes6969-keep)
	content = append(content, tail...)
	if err := os.WriteFile(patternFile, content, 0o644); err != nil {
		t.Fatal(err)
	}

	// The geometry, asserted rather than asserted-by-label: the cut is inside
	// the tail line, and the surviving fragment is exactly the corrupted
	// pattern this case is named for.
	if got := string(content[sparseCapBytes6969-keep : sparseCapBytes6969]); got != "!/src" {
		t.Fatalf("fixture is not the mid-line geometry: the truncated tail is %q, want %q", got, "!/src")
	}
	if got := content[sparseCapBytes6969-1]; got == '\n' {
		t.Fatalf("fixture is not a MID-LINE cut: byte %d is a newline, which is the other case's geometry", sparseCapBytes6969-1)
	}

	si := gitmeta.ProbeRepo(dir)
	if si.IsSparse {
		t.Fatalf("a mid-line-truncated pattern file left the repo SPARSE: patterns=%v (#6969)", si.Patterns)
	}
	for _, p := range si.Patterns {
		if p == "!/src" {
			t.Fatalf("the corrupted fragment %q reached the pattern set: patterns=%v", p, si.Patterns)
		}
	}
	if !gitmeta.IsPathIncluded(si, trInsideCone) {
		t.Fatalf("%s is EXCLUDED from the index by the corrupted fragment %q left behind by a mid-line cut (patterns=%v). %s",
			trInsideCone, "!/src", si.Patterns, rgVersion(t, dir))
	}
	if !gitmeta.IsPathIncluded(si, trNamed) {
		t.Fatalf("%s is still EXCLUDED after a mid-line-truncated pattern file (patterns=%v)", trNamed, si.Patterns)
	}
}

// TestSparse_PatternFileExactlyAtTheCapStaysSparse is the counter-case that
// makes the truncation signal load-bearing rather than a size threshold.
//
// A file whose size is EXACTLY the cap is complete: every byte of it was read.
// Inferring truncation from len(b) == maxBytes — the obvious implementation —
// would reject it, turn a perfectly good sparse repo into a full index, and
// look identical to the correct code on every other input. So this asserts the
// EXCLUSION direction survives at the boundary: trNamed must still be out.
func TestSparse_PatternFileExactlyAtTheCapStaysSparse(t *testing.T) {
	dir, patternFile, gitWrote := trSparseFixture(t)

	content := trPadTo(t, gitWrote, sparseCapBytes6969)
	if err := os.WriteFile(patternFile, content, 0o644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(patternFile)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != sparseCapBytes6969 {
		t.Fatalf("fixture premise broken: pattern file is %d bytes, want exactly %d", fi.Size(), sparseCapBytes6969)
	}

	si := gitmeta.ProbeRepo(dir)
	if !si.IsSparse {
		t.Fatalf("a pattern file of exactly %d bytes is COMPLETE, but the repo was reported not sparse — truncation is being inferred from the length instead of observed (#6969). %s",
			sparseCapBytes6969, rgVersion(t, dir))
	}
	if gitmeta.IsPathIncluded(si, trNamed) {
		t.Fatalf("%s is INCLUDED for a complete at-the-cap pattern file (patterns=%v): the sparse restriction was lost", trNamed, si.Patterns)
	}
	if !gitmeta.IsPathIncluded(si, trInsideCone) {
		t.Fatalf("%s is EXCLUDED for a complete at-the-cap pattern file (patterns=%v)", trInsideCone, si.Patterns)
	}
}
