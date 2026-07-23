package memtrace

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestStart_InertWhenEnvUnset is the load-bearing test: with
// GRAFEL_MEMTRACE_DIR unset, Start must not start a goroutine or create any
// file. We assert this by checking Start returns nil and that no files
// exist in a scratch dir we point elsewhere (nothing to create there since
// the dir env itself is unset).
func TestMemtraceSampler_InertWhenEnvUnset(t *testing.T) {
	t.Setenv(DirEnv, "")
	os.Unsetenv(DirEnv)

	s := Start("child", nil, nil)
	if s != nil {
		t.Fatalf("Start() = %+v, want nil when %s is unset", s, DirEnv)
	}

	// Extra assurance: a Stop on the nil result must not panic.
	s.Stop()
}

// TestStart_WritesNDJSON verifies the sampler writes well-formed NDJSON with
// every expected field once GRAFEL_MEMTRACE_DIR is set.
func TestMemtraceSampler_WritesNDJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(DirEnv, dir)
	t.Setenv(IntervalEnv, "10ms")

	s := Start("child", func() string { return "extracting_ast" }, nil)
	if s == nil {
		t.Fatal("Start() = nil, want a live sampler when GRAFEL_MEMTRACE_DIR is set")
	}
	// Let a handful of ticks fire.
	time.Sleep(60 * time.Millisecond)
	s.Stop()

	path := filepath.Join(s.Dir(), "child.ndjson")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lines := 0
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line %d: not valid JSON: %v (%q)", lines, err, line)
		}
		if rec.TS == 0 {
			t.Errorf("line %d: ts is zero", lines)
		}
		if rec.Phase != "extracting_ast" {
			t.Errorf("line %d: phase = %q, want extracting_ast", lines, rec.Phase)
		}
		if rec.Role != "child" {
			t.Errorf("line %d: role = %q, want child", lines, rec.Role)
		}
		if rec.HeapSys == 0 {
			t.Errorf("line %d: heap_sys is zero, want a real memstat", lines)
		}
		lines++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if lines == 0 {
		t.Fatal("no NDJSON lines written")
	}
}

// TestStart_NonWritableDirDisablesSilently verifies that when the run
// directory cannot be created (a regular file sits where a directory is
// expected), Start disables itself and returns nil rather than erroring or
// panicking.
func TestMemtraceSampler_NonWritableDirDisablesSilently(t *testing.T) {
	base := t.TempDir()
	// Create a FILE at the path memtrace would need to MkdirAll as a
	// directory, so MkdirAll fails deterministically on every OS.
	blocker := filepath.Join(base, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	dirEnv := filepath.Join(blocker, "sub") // parent "blocker" is a file, not a dir
	t.Setenv(DirEnv, dirEnv)

	var loggedCount int32
	logf := func(format string, args ...any) {
		atomic.AddInt32(&loggedCount, 1)
	}

	s := Start("child", nil, logf)
	if s != nil {
		t.Fatalf("Start() = %+v, want nil when the run dir cannot be created", s)
	}
	if atomic.LoadInt32(&loggedCount) == 0 {
		t.Error("expected at least one best-effort log line on setup failure")
	}
	// Must not panic.
	s.Stop()
}

// TestPhaseFollowsTracker feeds a sequence of phases through a phaseFn (as
// the real caller wires progress.Tracker.CurrentPhase) and asserts the
// recorded trace observes them in order, with no parallel phase state.
func TestMemtraceSampler_PhaseFollowsTracker(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(DirEnv, dir)
	t.Setenv(IntervalEnv, "5ms")

	phases := []string{"scanning", "extracting_ast", "resolving_refs", "writing_graph"}
	var idx int32

	s := Start("child", func() string {
		i := atomic.LoadInt32(&idx)
		if int(i) >= len(phases) {
			i = int32(len(phases) - 1)
		}
		return phases[i]
	}, nil)
	if s == nil {
		t.Fatal("Start() = nil, want a live sampler")
	}

	for i := 1; i < len(phases); i++ {
		time.Sleep(20 * time.Millisecond)
		atomic.StoreInt32(&idx, int32(i))
	}
	time.Sleep(20 * time.Millisecond)
	s.Stop()

	path := filepath.Join(s.Dir(), "child.ndjson")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	var seen []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("bad line: %v", err)
		}
		if len(seen) == 0 || seen[len(seen)-1] != rec.Phase {
			seen = append(seen, rec.Phase)
		}
	}

	if len(seen) == 0 {
		t.Fatal("no phases observed")
	}
	// The observed distinct-phase sequence must be a subsequence of the fed
	// phases, in order, with no phase out of place (this is what would catch
	// drift between the tracker's phase and the trace).
	pi := 0
	for _, ph := range seen {
		found := false
		for pi < len(phases) {
			if phases[pi] == ph {
				found = true
				pi++
				break
			}
			pi++
		}
		if !found {
			t.Errorf("observed phase %q not found in expected order %v (already consumed up to %d)", ph, phases, pi)
		}
	}
}

// TestMemtraceSampler_PanicInSampleRecovered is the panic-containment test:
// it substitutes sampleFn with a panicking implementation and asserts (a)
// the process/test does not crash, (b) the sampler goroutine terminates
// (doneCh closes) rather than spinning or crashing the process, and (c) the
// best-effort logger is invoked at most once — mirroring the "must NEVER
// affect the index" contract for a caller that would otherwise be sitting
// on the other side of this goroutine doing real indexing work.
func TestMemtraceSampler_PanicInSampleRecovered(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(DirEnv, dir)
	t.Setenv(IntervalEnv, "5ms")

	orig := sampleFn
	sampleFn = func(s *Sampler) { panic("injected test panic: simulated sampler fault") }
	defer func() { sampleFn = orig }()

	var logCount int32
	logf := func(format string, args ...any) {
		atomic.AddInt32(&logCount, 1)
	}

	s := Start("child", nil, logf)
	if s == nil {
		t.Fatal("Start() = nil, want a live sampler")
	}

	// (b) the goroutine must terminate — recover() + return, not a crash and
	// not an infinite re-panic/retry spin.
	select {
	case <-s.doneCh:
		// recovered and exited cleanly
	case <-time.After(2 * time.Second):
		t.Fatal("sampler goroutine did not terminate after a panic in sampleFn — recover() missing or broken")
	}

	// Stop must be safe to call even though the goroutine already exited on
	// its own (closed stopCh/doneCh should not block or panic).
	s.Stop()

	// (a) reaching this line at all — in the same test process, un-recovered
	// from any runtime crash — is itself the proof the panic never escaped
	// the goroutine. (c) exactly one best-effort log line, per the
	// log-at-most-once contract.
	if got := atomic.LoadInt32(&logCount); got != 1 {
		t.Errorf("logf called %d times, want exactly 1 (log-once on panic recovery)", got)
	}
}
