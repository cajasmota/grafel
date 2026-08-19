package extractors

import (
	"testing"
	"time"
)

// TestPhaseTrace_EnvValueParsing pins how GRAFEL_PHASE_TRACE is interpreted,
// for all three of its meanings, and it exists because two of them were wrong.
//
//  1. FALSEY values were treated as PATHS. GRAFEL_PHASE_TRACE=0 gave
//     enabled=true and traceePath="0" — an operator disabling tracing the
//     obvious way turned it ON and started appending JSONL to a file literally
//     named "0" in the daemon's working directory. Same for false/off/no and
//     any casing of them. Only "1" was covered by a test.
//  2. BOOLEAN mode started the 5 ms heap sampler and threw its output away.
//     heapPeak/sysPeak are written only into the JSONL record, which boolean
//     mode never writes, so GRAFEL_PHASE_TRACE=1 paid a stop-the-world
//     runtime.ReadMemStats every 5 ms for numbers nothing reads. The struct
//     field comment already claimed the sampler was "only active when tracing
//     to a file"; this test is what makes that true.
func TestPhaseTrace_EnvValueParsing(t *testing.T) {
	cases := []struct {
		raw         string
		wantEnabled bool
		wantPath    string
	}{
		// Disabled. Whitespace is trimmed and case is ignored, so a value that
		// LOOKS like a disable is a disable, never a filename.
		{"", false, ""},
		{" ", false, ""},
		{"0", false, ""},
		{"false", false, ""},
		{"FALSE", false, ""},
		{"off", false, ""},
		{"OFF", false, ""},
		{"no", false, ""},
		{" no ", false, ""},

		// Boolean-enabled: summary line only, no file, and no heap sampler.
		{"1", true, ""},
		{"true", true, ""},
		{"TRUE", true, ""},
		{"yes", true, ""},
		{"on", true, ""},
		{" on ", true, ""},

		// Anything else is a path. Note "0.jsonl" is a path, not a disable:
		// only the exact falsey tokens disable.
		{"/tmp/phases.jsonl", true, "/tmp/phases.jsonl"},
		{"0.jsonl", true, "0.jsonl"},
		{"onward.jsonl", true, "onward.jsonl"},
	}

	for _, tc := range cases {
		t.Run("value="+tc.raw, func(t *testing.T) {
			t.Setenv("GRAFEL_PHASE_TRACE", tc.raw)
			tr := newPhaseTrace(time.Now())
			defer tr.stopHeapSampler()

			if tr.enabled != tc.wantEnabled {
				t.Fatalf("GRAFEL_PHASE_TRACE=%q: enabled=%v, want %v", tc.raw, tr.enabled, tc.wantEnabled)
			}
			if tr.traceePath != tc.wantPath {
				t.Fatalf("GRAFEL_PHASE_TRACE=%q: traceePath=%q, want %q "+
					"(a non-path value must never become a filename in the daemon's cwd)",
					tc.raw, tr.traceePath, tc.wantPath)
			}
			// The heap sampler runs only when there is a JSONL sink to put its
			// numbers in; every other mode must not pay ReadMemStats at all.
			if got, want := tr.heapStop != nil, tc.wantPath != ""; got != want {
				t.Fatalf("GRAFEL_PHASE_TRACE=%q: heap sampler running=%v, want %v "+
					"(it is only useful when the JSONL record it feeds is written)",
					tc.raw, got, want)
			}
		})
	}
}

// phaseNames6199 is the phase set a real zero-change pass opens, in order.
// Kept in step with incremental.go by TestPhaseTrace_EverySpanClosesOnEveryPath
// only loosely — it is a benchmark input, not an assertion.
var phaseNames6199 = []string{
	"manifest-load", "tree-walk", "git-diff-filter", "head-advance",
	"ast-hash-gate", "graph-materialise", "prune-scans", "extract-changed-files",
	"prune-scans", "scoped-resolve", "prior-resolution-replay", "module-stamp",
	"flow-recompute", "module-aggregate", "lib-boundary", "structural-coupling",
	"coverage-bfs", "migration-prune", "canonical-sort", "graph-remarshal-write",
	"sidecar-write", "manifest-apply-stamps", "manifest-save",
}

// BenchmarkPhaseTrace_DisabledPerPass measures what the trace costs a pass that
// is NOT being traced — the only cost every user pays. The file comment quotes
// its numbers, so they are measured here rather than asserted from memory.
func BenchmarkPhaseTrace_DisabledPerPass(b *testing.B) {
	b.Setenv("GRAFEL_PHASE_TRACE", "")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		tr := newPhaseTrace(time.Now())
		for _, name := range phaseNames6199 {
			end := tr.span(name)
			end()
		}
		tr.emit(nil, "repo", true, "", 0, 0, 0, 0)
	}
}
