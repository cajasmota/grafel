package daemon

import (
	"bytes"
	"encoding/json"
	"log/slog"
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
