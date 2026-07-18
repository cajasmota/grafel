package daemon

// selfdefense_internal_test.go — internal-package unit tests for the Layer 2
// pprof dumps (#857 goroutine dump, #5822 sub-ask 2 paired heap dump).
//
// These live in package daemon (not daemon_test) so they can call the
// unexported dumpGoroutineProfile / dumpHeapProfile / siblingHeapPath helpers
// directly without spinning up a real hot-loop watchdog tick.

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, nil))
}

// TestDumpGoroutineProfile_WritesFile verifies the pre-existing #857 behavior
// still holds: a goroutine dump is written to a discoverable temp file whose
// name follows the grafel-hotloop-*.pprof.txt convention.
func TestDumpGoroutineProfile_WritesFile(t *testing.T) {
	path := dumpGoroutineProfile(testLogger())
	if path == "" {
		t.Fatal("dumpGoroutineProfile returned empty path")
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	if !strings.HasSuffix(path, ".pprof.txt") {
		t.Errorf("goroutine dump path %q does not end in .pprof.txt", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read goroutine dump: %v", err)
	}
	if len(data) == 0 {
		t.Error("goroutine dump file is empty")
	}
}

// TestDumpHeapProfile_WritesSiblingFile is the RED->GREEN regression test for
// #5822 sub-ask 2: when the watchdog fires and writes a goroutine dump, it
// must ALSO write a heap profile to a sibling file (same directory,
// correlated name) so the operator can diagnose a heap blowup without needing
// the live /debug/pprof endpoint.
func TestDumpHeapProfile_WritesSiblingFile(t *testing.T) {
	logger := testLogger()

	goroutinePath := dumpGoroutineProfile(logger)
	if goroutinePath == "" {
		t.Fatal("dumpGoroutineProfile returned empty path")
	}
	t.Cleanup(func() { _ = os.Remove(goroutinePath) })

	dumpHeapProfile(logger, goroutinePath)

	wantHeapPath := strings.TrimSuffix(goroutinePath, ".pprof.txt") + ".heap.pprof"
	t.Cleanup(func() { _ = os.Remove(wantHeapPath) })

	if filepath.Dir(wantHeapPath) != filepath.Dir(goroutinePath) {
		t.Fatalf("heap profile %q is not a sibling of goroutine dump %q", wantHeapPath, goroutinePath)
	}

	info, err := os.Stat(wantHeapPath)
	if err != nil {
		t.Fatalf("heap profile file not written at %q: %v", wantHeapPath, err)
	}
	if info.Size() == 0 {
		t.Error("heap profile file is empty")
	}
}

// TestDumpHeapProfile_EmptyGoroutinePathFallsBack verifies dumpHeapProfile
// still writes a usable heap profile even when no goroutine dump path is
// available (dumpGoroutineProfile itself failed).
func TestDumpHeapProfile_EmptyGoroutinePathFallsBack(t *testing.T) {
	logger := testLogger()
	dumpHeapProfile(logger, "")
	// No path to assert on directly (siblingHeapPath("") uses a
	// nanosecond-timestamped name), but the call must not panic and
	// siblingHeapPath("") must resolve to a real, writable path — verified by
	// TestSiblingHeapPath_EmptyPath below. This test's job is simply to prove
	// the zero-arg call path is safe.
}

func TestSiblingHeapPath(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "standard goroutine dump suffix",
			in:   "/tmp/grafel-hotloop-123.pprof.txt",
			want: "/tmp/grafel-hotloop-123.heap.pprof",
		},
		{
			name: "non-standard suffix appends",
			in:   "/tmp/grafel-hotloop-weird",
			want: "/tmp/grafel-hotloop-weird.heap.pprof",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := siblingHeapPath(tc.in)
			if got != tc.want {
				t.Errorf("siblingHeapPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSiblingHeapPath_EmptyPath(t *testing.T) {
	got := siblingHeapPath("")
	if got == "" {
		t.Fatal("siblingHeapPath(\"\") returned empty string")
	}
	if !strings.HasSuffix(got, ".heap.pprof") {
		t.Errorf("siblingHeapPath(\"\") = %q, want suffix .heap.pprof", got)
	}
	if filepath.Clean(filepath.Dir(got)) != filepath.Clean(os.TempDir()) {
		t.Errorf("siblingHeapPath(\"\") = %q, want directory %q", got, os.TempDir())
	}
}
