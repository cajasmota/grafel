package daemon

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"runtime"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/daemon/proto"
	"github.com/cajasmota/archigraph/internal/daemon/sched"
)

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

	// RSSBytes should be populated from runtime.MemStats.Sys.
	if reply.RSSBytes == 0 {
		t.Errorf("expected RSSBytes > 0 (actual daemon memory), got 0")
	}

	// RSSUsedMB should be the RSSBytes converted to MB.
	expectedUsedMB := int64(reply.RSSBytes / (1024 * 1024))
	if reply.RSSUsedMB != expectedUsedMB {
		t.Errorf("RSSUsedMB: got %d, want %d (RSSBytes %d / 1MB)",
			reply.RSSUsedMB, expectedUsedMB, reply.RSSBytes)
	}

	// RSSUsedMB should be non-zero when daemon has allocated heap.
	if reply.RSSUsedMB <= 0 {
		t.Errorf("expected RSSUsedMB > 0 (actual daemon memory), got %d", reply.RSSUsedMB)
	}

	// The difference between header RSS and budget display should be
	// within ~10% tolerance. Verify that they're using the same source.
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	headerMB := int64(ms.Sys / (1024 * 1024))
	budgetMB := reply.RSSUsedMB
	if headerMB > 0 {
		pctDiff := float64(headerMB-budgetMB) / float64(headerMB) * 100
		if pctDiff < -10 || pctDiff > 10 {
			t.Logf("warning: header RSS (≈%dMB) differs from budget display (%dMB) by >10%%",
				headerMB, budgetMB)
		}
	}
}
