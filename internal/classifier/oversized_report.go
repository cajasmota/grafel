package classifier

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// oversized_report.go — the user-visible surface for a file dropped by the
// size gate (#6985).
//
// WHY IT EXISTS. classifyWithSizeInner turns any file over maxIndexableBytes
// into Skip=true / SkipReason="too_large", and that file then mints no entity
// and appears nowhere in graph.json. Before this report the only trace a user
// could read after an index was the aggregate `skipped=N` in the run summary:
// no reason, no filename. The reason was not thrown away entirely —
// classifier.go attaches classifier.skip_reason as an OTel span attribute on
// both Classify and ClassifyWithSize — but that requires tracing to be
// enabled and is not something a user reads after an index.
//
// WHY NOT THE UnsupportedTally. That tally aggregates unsupported-LANGUAGE
// skips by extension, and its Observe deliberately ignores oversized files
// (unsupported.go). Widening it would change what the unsupported-extension
// report counts, which is a different report about a different fact. This is a
// separate surface for a separate disposition.
//
// WHY HERE rather than at the extraction entry points. Three call sites
// classify with a size (cmd/grafel/index.go, internal/daemon/extract's
// coordinator and subproc). Reporting at each of them is the shape that let
// eleven copies of one read diverge in #6823; classifyWithSizeInner is the
// single place "too_large" is minted, so guarding it there is by construction.

// oversized* back the always-on report below. Shape taken from
// internal/gitmeta's reportGitMetaSkip / reportGitMetaTruncation: a dedup map
// plus a cap, so the output stays bounded.
var (
	oversizedMu   sync.Mutex
	oversizedSeen map[string]bool
	oversizedOut  io.Writer = os.Stderr
)

// maxOversizedReports caps the report the way maxGitMetaSkipReports caps its
// own: a warning long enough to scroll past reports nothing.
//
// The cap is load-bearing here rather than a backstop. The population is
// exactly a repo full of vendored bundles — of the ten oversized files with a
// recognised language across grafel's 60 corpora, eight are compiled/vendored
// JS and nine of the ten are in a single repo — so a line per file is the
// output shape this report has to avoid. A bounded list plus the existing
// `skipped=N` aggregate is what a reader can act on.
const maxOversizedReports = 8

// setOversizedReportOutput redirects the report for tests and returns a
// restore func. Test-only helper.
func setOversizedReportOutput(w io.Writer) func() {
	oversizedMu.Lock()
	prev := oversizedOut
	oversizedOut = w
	oversizedSeen = nil
	oversizedMu.Unlock()
	return func() {
		oversizedMu.Lock()
		oversizedOut = prev
		oversizedSeen = nil
		oversizedMu.Unlock()
	}
}

// reportOversizedSkip names a file that was dropped for exceeding the size
// gate, and says what the consequence is.
//
// It reports and returns; it does not change the ClassifyResult, so the
// `skipped=N` aggregate still counts a named file exactly once, as a skip. The
// dedup map is keyed on the file path, so two distinct oversized files are two
// lines and the same file classified twice is one.
func reportOversizedSkip(filePath string, sizeBytes int64) {
	oversizedMu.Lock()
	if oversizedSeen == nil {
		oversizedSeen = map[string]bool{}
	}
	if oversizedSeen[filePath] || len(oversizedSeen) >= maxOversizedReports {
		oversizedMu.Unlock()
		return
	}
	oversizedSeen[filePath] = true
	last := len(oversizedSeen) == maxOversizedReports
	w := oversizedOut
	oversizedMu.Unlock()

	fmt.Fprintf(w, "grafel: %s is %d bytes, over the %d-byte limit, and was NOT indexed — it mints no entity and appears nowhere in the graph (#6985)\n", filePath, sizeBytes, maxIndexableBytes)
	if last {
		fmt.Fprintf(w, "grafel: further oversized-file skips suppressed after %d\n", maxOversizedReports)
	}
}
