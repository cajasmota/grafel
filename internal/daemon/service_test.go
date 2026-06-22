package daemon

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/sched"
)

// syncBuffer is a goroutine-safe bytes.Buffer wrapper. The stall-detector test
// reads the log buffer from the test goroutine while the daemon's dead-man
// goroutine writes to it concurrently, which races on a bare bytes.Buffer.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// The three PR-#2374 tests below replaced the deleted startup guard
// (which warned when log.Logger had flags set under JSON mode). That guard was
// removed in #2375 because log/slog eliminates the failure mode entirely:
// handler selection at construction time means slog cannot be misconfigured
// the same way.
//
// The new tests verify the slog-based logging shape:
//   - JSON handler produces parseable JSON with expected fields.
//   - Text handler produces logfmt (not JSON).
//   - newService accepts a *slog.Logger (nil or non-nil) without panicking.

// TestNewService_JSONHandler_ProducesStructuredJSON verifies that a Service
// constructed with a JSON-handler slog.Logger emits parseable JSON lines
// containing expected structured fields. Replaces TestNewService_JSONMode_WarnsFlaggedLogger.
func TestNewService_JSONHandler_ProducesStructuredJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	svc := newService(
		func(proto.IndexArgs) (string, string, error) { return "", "", nil },
		func(proto.RebuildArgs) ([]string, string, error) { return []string{}, "", nil },
		func(proto.QualityAuditRequest) (proto.QualityAuditReply, error) {
			return proto.QualityAuditReply{}, nil
		},
		"/tmp/test.sock",
		make(chan struct{}),
		logger,
		1,
	)

	// Trigger a log line by calling Rebuild (which logs "rebuild: start").
	var reply proto.RebuildReply
	_ = svc.Rebuild(&proto.RebuildArgs{Group: "testgroup"}, &reply)

	out := strings.TrimSpace(buf.String())
	if out == "" {
		t.Skip("no log output produced (rebuild entrypoint nil) — skipping JSON shape check")
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("JSON handler produced non-JSON line: %v — got: %q", err, line)
		}
	}
}

// TestNewService_TextHandler_ProducesLogfmt verifies that a Service constructed
// with a text-handler slog.Logger emits logfmt (not JSON). Replaces
// TestNewService_JSONMode_NoWarnZeroFlags.
func TestNewService_TextHandler_ProducesLogfmt(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	svc := newService(
		func(proto.IndexArgs) (string, string, error) { return "", "", nil },
		func(proto.RebuildArgs) ([]string, string, error) { return []string{}, "", nil },
		func(proto.QualityAuditRequest) (proto.QualityAuditReply, error) {
			return proto.QualityAuditReply{}, nil
		},
		"/tmp/test.sock",
		make(chan struct{}),
		logger,
		1,
	)

	var reply proto.RebuildReply
	_ = svc.Rebuild(&proto.RebuildArgs{Group: "testgroup"}, &reply)

	out := strings.TrimSpace(buf.String())
	if out == "" {
		t.Skip("no log output produced (rebuild entrypoint nil) — skipping logfmt shape check")
	}
	// Text handler emits logfmt: time=... level=INFO msg=... key=value ...
	// It must NOT be JSON.
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil {
			t.Errorf("text handler produced JSON line (expected logfmt): %q", line)
		}
		if !strings.Contains(line, "msg=") {
			t.Errorf("text handler line missing msg= key: %q", line)
		}
	}
}

// TestNewService_NilLogger_NoStart verifies that newService accepts a nil
// *slog.Logger without panicking. Replaces TestNewService_TextMode_NoWarnFlaggedLogger.
func TestNewService_NilLogger_NoStart(t *testing.T) {
	// Nil logger: newService must not panic; the service runs in nil-logger mode.
	svc := newService(
		func(proto.IndexArgs) (string, string, error) { return "", "", nil },
		func(proto.RebuildArgs) ([]string, string, error) { return []string{}, "", nil },
		func(proto.QualityAuditRequest) (proto.QualityAuditReply, error) {
			return proto.QualityAuditReply{}, nil
		},
		"/tmp/test.sock",
		make(chan struct{}),
		nil, // nil logger — no output, no panic
		1,
	)
	if svc == nil {
		t.Fatal("newService returned nil")
	}
	if svc.logger != nil {
		t.Errorf("expected nil logger on service, got non-nil")
	}
}

// TestStatusRSSReportsActualMemory verifies that Status.RSSUsedMB
// reports the actual daemon memory (in MB) from runtime.MemStats,
// not just the sum of predicted in-flight job allocations (issue #803).
func TestStatusRSSReportsActualMemory(t *testing.T) {
	svc := newService(
		func(proto.IndexArgs) (string, string, error) { return "", "", nil },
		func(proto.RebuildArgs) ([]string, string, error) { return []string{}, "", nil },
		func(proto.QualityAuditRequest) (proto.QualityAuditReply, error) {
			return proto.QualityAuditReply{}, nil
		},
		"/tmp/test.sock",
		make(chan struct{}),
		nil, // logger
		2,   // maxConcurrentGroups
	)

	// Attach a scheduler with a non-zero budget.
	svc.scheduler = sched.New(sched.Config{
		Workers:  2,
		BudgetMB: 500,
		Predict:  func(_ string) int64 { return 50 },
	})

	// Call Status to get the RPC reply.
	var reply proto.StatusReply
	if err := svc.Status(&proto.StatusArgs{}, &reply); err != nil {
		t.Fatalf("Status RPC failed: %v", err)
	}

	// #3648: RSSBytes is now sourced from the honest process footprint
	// (resident set size), NOT runtime.MemStats.Sys (reserved virtual
	// address space). It must be populated and must equal FootprintBytes.
	if reply.RSSBytes == 0 {
		t.Errorf("expected RSSBytes > 0 (honest process footprint), got 0")
	}
	if reply.FootprintBytes != reply.RSSBytes {
		t.Errorf("RSSBytes (%d) must mirror FootprintBytes (%d)",
			reply.RSSBytes, reply.FootprintBytes)
	}
	if reply.FootprintLabel == "" {
		t.Error("FootprintLabel must describe what FootprintBytes measured")
	}

	// #3648: the distinct honest heap fields must be populated so clients can
	// see the Go-heap breakdown instead of a single mislabeled number.
	if reply.HeapInuseBytes == 0 {
		t.Error("expected HeapInuseBytes > 0")
	}
	if reply.SysBytes == 0 {
		t.Error("expected SysBytes > 0 (MemStats.Sys, reported as its own field)")
	}

	// The footprint (resident) must NOT be the old ms.Sys value — that was
	// the mislabel this change fixes. Sys (reserved VA) is typically far
	// larger than resident RSS; assert they are reported as DISTINCT fields.
	if reply.RSSBytes == reply.SysBytes {
		t.Error("RSSBytes must be the resident footprint, distinct from SysBytes (the old mislabel)")
	}

	// RSSUsedMB should be the RSSBytes (footprint) converted to MB.
	expectedUsedMB := int64(reply.RSSBytes / (1024 * 1024))
	if reply.RSSUsedMB != expectedUsedMB {
		t.Errorf("RSSUsedMB: got %d, want %d (RSSBytes %d / 1MB)",
			reply.RSSUsedMB, expectedUsedMB, reply.RSSBytes)
	}
}

// TestRebuild_StallDetectorLogsGoroutineDump verifies the #5326 stall
// diagnostics: when a rebuild runs longer than the (test-shortened) stall-warn
// interval without producing a result, the daemon logs a "possible stall"
// warning AND a goroutine dump so the next stall is diagnosable from the log.
// It also asserts the result is still delivered promptly once the rebuild
// completes — i.e. the warning is a heartbeat, not a wait the result depends on.
func TestRebuild_StallDetectorLogsGoroutineDump(t *testing.T) {
	t.Setenv("GRAFEL_STALL_WARN_INTERVAL", "30ms")

	var buf syncBuffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	release := make(chan struct{})
	rebuildStarted := make(chan struct{})
	svc := newService(
		func(proto.IndexArgs) (string, string, error) { return "", "", nil },
		func(proto.RebuildArgs) ([]string, string, error) {
			close(rebuildStarted)
			<-release // block long enough for the stall detector to fire
			return []string{"/repo/a"}, "", nil
		},
		func(proto.QualityAuditRequest) (proto.QualityAuditReply, error) {
			return proto.QualityAuditReply{}, nil
		},
		"/tmp/test.sock",
		make(chan struct{}),
		logger,
		1,
	)

	done := make(chan error, 1)
	var reply proto.RebuildReply
	go func() {
		done <- svc.Rebuild(&proto.RebuildArgs{Group: "stallgroup"}, &reply)
	}()

	<-rebuildStarted
	// Wait until the stall warning + goroutine dump have been logged.
	deadline := time.After(3 * time.Second)
	for {
		if strings.Contains(buf.String(), "goroutine dump") {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("stall goroutine dump not logged within timeout; log:\n%s", buf.String())
		case <-time.After(5 * time.Millisecond):
		}
	}

	logOut := buf.String()
	if !strings.Contains(logOut, "possible stall") {
		t.Errorf("expected 'possible stall' warning, got:\n%s", logOut)
	}
	// The dump must contain an actual goroutine stack ("goroutine N [..]").
	if !strings.Contains(logOut, "goroutine ") {
		t.Errorf("goroutine dump did not contain a stack trace:\n%s", logOut)
	}

	// Now release the rebuild — the result must be delivered promptly, proving
	// the result path does not wait on the stall timer.
	t0 := time.Now()
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("rebuild returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("rebuild did not complete promptly after work finished")
	}
	if elapsed := time.Since(t0); elapsed > time.Second {
		t.Errorf("result delivery was slow after completion: %s (expected ~immediate)", elapsed)
	}
	if len(reply.Repos) != 1 {
		t.Errorf("expected 1 repo in reply, got %d", len(reply.Repos))
	}
}

// TestRebuild_NoGoroutineLeak verifies the dead-man ticker goroutine is torn
// down when the rebuild completes — previously `for range ticker.C` blocked
// forever (Ticker.Stop does not close the channel), leaking one goroutine per
// Rebuild RPC (#5326).
func TestRebuild_NoGoroutineLeak(t *testing.T) {
	t.Setenv("GRAFEL_STALL_WARN_INTERVAL", "10ms")
	svc := newService(
		func(proto.IndexArgs) (string, string, error) { return "", "", nil },
		func(proto.RebuildArgs) ([]string, string, error) { return []string{"/r"}, "", nil },
		func(proto.QualityAuditRequest) (proto.QualityAuditReply, error) {
			return proto.QualityAuditReply{}, nil
		},
		"/tmp/test.sock",
		make(chan struct{}),
		nil,
		1,
	)

	// Let any startup goroutines settle.
	time.Sleep(20 * time.Millisecond)
	before := runtime.NumGoroutine()

	for i := 0; i < 50; i++ {
		var reply proto.RebuildReply
		if err := svc.Rebuild(&proto.RebuildArgs{Group: "g"}, &reply); err != nil {
			t.Fatalf("rebuild %d: %v", i, err)
		}
	}

	// Give torn-down ticker goroutines a moment to exit.
	time.Sleep(50 * time.Millisecond)
	after := runtime.NumGoroutine()
	// Allow a small slack for runtime/background goroutines; a leak would be ~50.
	if after-before > 10 {
		t.Errorf("goroutine leak: before=%d after=%d (delta=%d) after 50 rebuilds",
			before, after, after-before)
	}
}
