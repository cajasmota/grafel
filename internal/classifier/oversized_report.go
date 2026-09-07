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
// WHY HERE rather than at the consuming call sites. Within this package
// "too_large" is minted in exactly one place, classifyWithSizeInner, while
// FOUR call sites consume it: cmd/grafel/index.go, internal/daemon/extract's
// coordinator and subproc, and internal/extractors/incremental.go:1003 on the
// daemon's watcher-driven incremental path. (internal/secrets declares its own
// unrelated SkipTooLarge constant; that is a different subsystem and not this
// mint point.) Reporting at each consumer is the shape that let eleven copies
// of one read diverge in #6823; guarding the mint point is by construction.
//
// WHY THE STATE IS PER-CLASSIFIER. It was process-global in the first cut of
// this file, and that made the report go permanently and globally silent after
// the first eight oversized files: the second index in the same process — a
// different repo included — printed nothing at all, not even the suppression
// line, so a user who fixed nothing and re-ran saw a clean report. internal/
// gitmeta can hold process-global state because its inputs are read once per
// process; this code re-enters per index AND per daemon incremental reindex.
// Every construction site (cmd/grafel/index.go:808,
// internal/daemon/extract/coordinator.go:662, subproc.go:105,
// internal/extractors/incremental.go:886) calls classifier.New per run, so
// hanging the state off the Classifier makes the lifetime per-index with no
// reset call for a caller to forget.

// maxOversizedReports caps the NAMED lines, the way maxGitMetaSkipReports caps
// internal/gitmeta's: a warning long enough to scroll past reports nothing.
//
// The cap is load-bearing here rather than a backstop. The population is
// exactly a repo full of vendored bundles — of the ten oversized files with a
// recognised language across grafel's 60 corpora, eight are compiled/vendored
// JS and nine of the ten are in a single repo — so a line per file is the
// output shape this report has to avoid.
const maxOversizedReports = 8

// oversizedReporter is one index's worth of oversized-skip reporting state.
//
// The mutex is not decoration: ClassifyWithSize runs inside the indexer's
// worker pool (cmd/grafel/index.go), so an unguarded map write here is a
// concurrent map write — a Go runtime fatal, not a warning.
type oversizedReporter struct {
	mu sync.Mutex
	// seen holds the paths already NAMED, and is capped at
	// maxOversizedReports so its memory is O(cap) rather than O(oversized
	// files in the repo).
	seen map[string]bool
	// suppressed counts the skips that were not named because the cap was
	// already full. It is what lets the end-of-index line say how much was
	// cut: "8 of 40" and "8 of 9" are indistinguishable without it.
	suppressed int
	// out is nil in production and is os.Stderr then; tests redirect it.
	out io.Writer
}

// writer resolves the destination. Callers hold r.mu.
func (r *oversizedReporter) writer() io.Writer {
	if r.out == nil {
		return os.Stderr
	}
	return r.out
}

// setOversizedReportOutput redirects one classifier's report. Test-only
// helper: production never sets it, so the zero value writes to stderr.
func setOversizedReportOutput(c *Classifier, w io.Writer) {
	c.oversized.mu.Lock()
	c.oversized.out = w
	c.oversized.mu.Unlock()
}

// report names a file that was dropped for exceeding the size gate, and says
// what the consequence is.
//
// It reports and returns; it does not change the ClassifyResult, so the
// `skipped=N` aggregate still counts a named file exactly once, as a skip. The
// dedup is keyed on the file path, so two distinct oversized files are two
// lines and the same file classified twice is one.
func (r *oversizedReporter) report(filePath string, sizeBytes int64) {
	r.mu.Lock()
	if r.seen == nil {
		r.seen = map[string]bool{}
	}
	if r.seen[filePath] {
		r.mu.Unlock()
		return
	}
	if len(r.seen) >= maxOversizedReports {
		// Past the cap the path is not remembered, so a repeat of an unnamed
		// file counts twice. On the index path each file is classified once,
		// and the alternative is an unbounded map keyed by a population this
		// report exists because it can be large.
		r.suppressed++
		r.mu.Unlock()
		return
	}
	r.seen[filePath] = true
	last := len(r.seen) == maxOversizedReports
	w := r.writer()
	r.mu.Unlock()

	fmt.Fprintf(w, "grafel: %s is %d bytes, over the %d-byte limit, and was NOT indexed — it mints no entity and appears nowhere in the graph (#6985)\n", filePath, sizeBytes, maxIndexableBytes)
	if last {
		fmt.Fprintf(w, "grafel: further oversized-file skips are not named; the total is reported at the end of the index\n")
	}
}

// summary closes the report with the count the cap hid.
//
// Nothing is printed when nothing was suppressed: the named lines already say
// everything in that case, and "0 further" is a zero row.
func (r *oversizedReporter) summary() {
	r.mu.Lock()
	hidden := r.suppressed
	named := len(r.seen)
	w := r.writer()
	r.mu.Unlock()

	if hidden == 0 {
		return
	}
	fmt.Fprintf(w, "grafel: %d further oversized-file skips were not named (%d named, %d over the %d-byte limit in total) (#6985)\n",
		hidden, named, named+hidden, maxIndexableBytes)
}

// ReportOversizedSummary prints the end-of-index line for the oversized files
// the cap did not name, and prints nothing when the cap was never reached.
//
// It is exported and called from ONE place — cmd/grafel/index.go, immediately
// before the `processed=… skipped=…` summary — because that is the surface a
// user reads after an index. The daemon's extract coordinator, subproc and
// incremental paths deliberately do not call it: they print no run summary at
// all, so a trailing count there would land nowhere. Their named lines are
// unaffected.
func (c *Classifier) ReportOversizedSummary() {
	c.oversized.summary()
}
