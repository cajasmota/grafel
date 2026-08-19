// Package extractors — incremental_phasetrace.go adds per-phase wall-clock
// instrumentation to TryIncremental (issue #6199).
//
// # Why this exists
//
// The incremental path re-parses only the changed files, but almost everything
// around that parse is O(repo) or O(graph) and runs on every pass — including
// the zero-change no-op. #6199 lists those phases; this file makes each one a
// number instead of an inference. Before it, the only persisted timing was
// GraphStatsSidecar.ExtractMS, which despite its name is time.Since(t0) for the
// WHOLE pass, and Result.Duration, which every caller discards.
//
// # Cost when disabled
//
// A phaseTrace is a plain struct on the stack of TryIncremental; each span is
// two time.Now() calls and an append. There is no allocation per pass beyond a
// slice of ~20 samples and no locking on the fast path — the heap sampler
// goroutine (the only concurrent part) starts ONLY when GRAFEL_PHASE_TRACE is
// set. Leaving the SPANS compiled in unconditionally is what makes the trace
// usable on a live daemon without a rebuild; the env var controls every
// externally visible effect.
//
// # Output — ALL of it is gated (deliberate)
//
// Nothing here changes what an ungated daemon logs. That is a decision, not an
// omission, and it is worth stating because the first draft of this file wrote
// the summary line unconditionally:
//
// The single most frequent incremental pass on a live daemon is the zero-change
// no-op — the scheduler polls a quiet repo, nothing changed, the pass returns
// Done. That path logs NOTHING today: read tryIncremental's `totalChanged == 0`
// branch, the only Printf in it belongs to the absent-graph guard. An
// unconditional summary line would therefore not be "one more line on an
// already chatty path"; it would put a brand-new line into a place that is
// currently silent, once per poll, per repo, for every user — as a side effect
// of landing a MEASUREMENT harness. Measurement tooling does not get to change
// default output. So:
//
//   - GRAFEL_PHASE_TRACE unset (the default): no log line, no JSONL, no heap
//     sampler. The spans still run; see the cost note above.
//   - GRAFEL_PHASE_TRACE=1|true|yes|on: the `incremental: phases ...` summary
//     line on the pass's logger, space-separated name=ms pairs, so a daemon log
//     is greppable. No file is written.
//   - GRAFEL_PHASE_TRACE=<path>: the summary line AND one JSON object per pass
//     appended to <path> (JSONL). This is the aggregation surface the #6199
//     measurement harness reads.
package extractors

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// phaseSample is one timed phase of a single TryIncremental pass.
type phaseSample struct {
	Name string  `json:"name"`
	MS   float64 `json:"ms"`
}

// phaseTrace accumulates per-phase durations for one TryIncremental pass.
//
// The zero value is NOT usable; construct with newPhaseTrace so t0 is set.
type phaseTrace struct {
	t0      time.Time
	samples []phaseSample

	// heap sampling (only active when tracing to a file)
	heapStop   chan struct{}
	heapDone   chan struct{}
	heapPeak   atomic.Uint64 // max HeapAlloc observed during the pass
	sysPeak    atomic.Uint64 // max Sys observed during the pass
	traceePath string        // JSONL sink; empty when tracing is off OR boolean-enabled
	enabled    bool          // GRAFEL_PHASE_TRACE set to anything non-empty

	// Counters the pass fills in as it learns them. They are reported even when
	// the pass falls back part-way, which is the point: a fallback's walk and
	// hash sweep are already paid by the time it decides to fall back.
	walkedFiles  int
	changedFiles int
	entities     int
	rels         int
}

// newPhaseTrace starts a trace. When GRAFEL_PHASE_TRACE is set it also starts a
// 5 ms heap sampler for the duration of the pass; when the value is a path
// rather than a boolean token, the JSONL sidecar is written too.
func newPhaseTrace(t0 time.Time) *phaseTrace {
	tr := &phaseTrace{t0: t0, samples: make([]phaseSample, 0, 24)}
	raw := os.Getenv("GRAFEL_PHASE_TRACE")
	tr.enabled = raw != ""
	if !tr.enabled {
		return tr
	}
	// A boolean-ish value means "log the summary line, write no file". Anything
	// else is taken as the JSONL path. Without this, asking for the cheap
	// greppable line would force the caller to invent a throwaway file.
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		tr.traceePath = ""
	default:
		tr.traceePath = raw
	}
	tr.startHeapSampler()
	return tr
}

func (t *phaseTrace) startHeapSampler() {
	t.heapStop = make(chan struct{})
	t.heapDone = make(chan struct{})
	go func() {
		defer close(t.heapDone)
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		var ms runtime.MemStats
		for {
			select {
			case <-t.heapStop:
				return
			case <-tick.C:
				runtime.ReadMemStats(&ms)
				for {
					cur := t.heapPeak.Load()
					if ms.HeapAlloc <= cur || t.heapPeak.CompareAndSwap(cur, ms.HeapAlloc) {
						break
					}
				}
				for {
					cur := t.sysPeak.Load()
					if ms.Sys <= cur || t.sysPeak.CompareAndSwap(cur, ms.Sys) {
						break
					}
				}
			}
		}
	}()
}

func (t *phaseTrace) stopHeapSampler() {
	if t.heapStop == nil {
		return
	}
	close(t.heapStop)
	<-t.heapDone
	t.heapStop = nil
}

// span starts a named phase and returns the closure that ends it. Usage:
//
//	end := tr.span("graph-load")
//	doc, err := graph.LoadGraphFromDir(stateDir)
//	end()
//
// Spans may be nested or repeated; repeated names are summed by the reporter,
// not overwritten, so a per-file phase inside a loop still totals correctly.
func (t *phaseTrace) span(name string) func() {
	if t == nil {
		return func() {}
	}
	start := time.Now()
	return func() {
		t.samples = append(t.samples, phaseSample{Name: name, MS: float64(time.Since(start).Microseconds()) / 1000.0})
	}
}

// add records a phase whose duration was measured by the caller.
func (t *phaseTrace) add(name string, d time.Duration) {
	if t == nil {
		return
	}
	t.samples = append(t.samples, phaseSample{Name: name, MS: float64(d.Microseconds()) / 1000.0})
}

// totals folds repeated span names into one entry each, preserving first-seen
// order so the report reads in pipeline order.
func (t *phaseTrace) totals() []phaseSample {
	idx := map[string]int{}
	var out []phaseSample
	for _, s := range t.samples {
		if i, ok := idx[s.Name]; ok {
			out[i].MS += s.MS
			continue
		}
		idx[s.Name] = len(out)
		out = append(out, s)
	}
	return out
}

// phaseRecord is the JSONL shape written to GRAFEL_PHASE_TRACE.
type phaseRecord struct {
	TS             string             `json:"ts"`
	Label          string             `json:"label,omitempty"` // GRAFEL_PHASE_TRACE_LABEL
	Repo           string             `json:"repo"`
	Done           bool               `json:"done"`
	FallbackReason string             `json:"fallback_reason,omitempty"`
	ChangedFiles   int                `json:"changed_files"`
	WalkedFiles    int                `json:"walked_files"`
	Entities       int                `json:"entities"`
	Relationships  int                `json:"relationships"`
	TotalMS        float64            `json:"total_ms"`
	AccountedMS    float64            `json:"accounted_ms"`
	HeapPeakMB     float64            `json:"heap_peak_mb"`
	SysPeakMB      float64            `json:"sys_peak_mb"`
	Phases         map[string]float64 `json:"phases"`
	Order          []string           `json:"order"`
}

// emit writes the log line and, when a path was configured, the JSONL record.
// It is called exactly once per TryIncremental return, including every fallback
// return — TestPhaseTrace_EveryReturnIsTraced proves that structurally.
//
// Both outputs are gated on GRAFEL_PHASE_TRACE; see the "Output" section at the
// top of this file for why the summary line is NOT unconditional.
func (t *phaseTrace) emit(printf func(string, ...any), repo string, done bool, fallbackReason string, changedFiles, walkedFiles, entities, rels int) {
	if t == nil || !t.enabled {
		return
	}
	t.stopHeapSampler()
	total := time.Since(t.t0)
	folded := t.totals()

	var b strings.Builder
	var accounted float64
	phases := make(map[string]float64, len(folded))
	order := make([]string, 0, len(folded))
	for _, s := range folded {
		fmt.Fprintf(&b, " %s=%.1f", s.Name, s.MS)
		accounted += s.MS
		phases[s.Name] = s.MS
		order = append(order, s.Name)
	}
	outcome := "done"
	if !done {
		outcome = "fallback:" + fallbackReason
	}
	if printf != nil {
		printf("incremental: phases outcome=%s changed=%d walked=%d entities=%d rels=%d total=%.1f accounted=%.1f%s",
			outcome, changedFiles, walkedFiles, entities, rels,
			float64(total.Microseconds())/1000.0, accounted, b.String())
	}

	if t.traceePath == "" {
		return
	}
	rec := phaseRecord{
		TS:             time.Now().UTC().Format(time.RFC3339Nano),
		Label:          os.Getenv("GRAFEL_PHASE_TRACE_LABEL"),
		Repo:           repo,
		Done:           done,
		FallbackReason: fallbackReason,
		ChangedFiles:   changedFiles,
		WalkedFiles:    walkedFiles,
		Entities:       entities,
		Relationships:  rels,
		TotalMS:        float64(total.Microseconds()) / 1000.0,
		AccountedMS:    accounted,
		HeapPeakMB:     float64(t.heapPeak.Load()) / (1024 * 1024),
		SysPeakMB:      float64(t.sysPeak.Load()) / (1024 * 1024),
		Phases:         phases,
		Order:          order,
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return
	}
	phaseTraceWriteMu.Lock()
	defer phaseTraceWriteMu.Unlock()
	f, ferr := os.OpenFile(t.traceePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if ferr != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

// phaseTraceWriteMu serialises appends when several repos are reindexed
// concurrently by the daemon scheduler.
var phaseTraceWriteMu sync.Mutex

// sortedPhaseNames is a test/diagnostic helper.
func sortedPhaseNames(p map[string]float64) []string {
	out := make([]string, 0, len(p))
	for k := range p {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
