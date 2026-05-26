package daemon

import (
	"bytes"
	"log"
	"runtime"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/daemon/proto"
	"github.com/cajasmota/archigraph/internal/daemon/sched"
)

// TestNewService_JSONMode_WarnsFlaggedLogger verifies that newService emits a
// warning to stderr when ARCHIGRAPH_DAEMON_LOG_JSON=1 is active and the
// supplied log.Logger has non-zero flags (issue #2353). A flagged logger
// prepends a timestamp prefix, making JSON output invalid.
func TestNewService_JSONMode_WarnsFlaggedLogger(t *testing.T) {
	t.Setenv(EnvDaemonLogJSON, "1")

	var buf bytes.Buffer
	// log.LstdFlags = Ldate | Ltime — the default flags that break JSON output.
	logger := log.New(&buf, "", log.LstdFlags)

	newService(
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

	out := buf.String()
	if !strings.Contains(out, "WARN:") {
		t.Errorf("expected WARN in log output when logger has flags under JSON mode, got: %q", out)
	}
	if !strings.Contains(out, EnvDaemonLogJSON) {
		t.Errorf("expected env var name %q in warning, got: %q", EnvDaemonLogJSON, out)
	}
	if !strings.Contains(out, "flags=") {
		t.Errorf("expected flags= value in warning, got: %q", out)
	}
}

// TestNewService_JSONMode_NoWarnZeroFlags verifies that newService does NOT
// emit a warning when JSON mode is active and the logger has flags=0 (the
// correct configuration for JSON-lines output).
func TestNewService_JSONMode_NoWarnZeroFlags(t *testing.T) {
	t.Setenv(EnvDaemonLogJSON, "1")

	var buf bytes.Buffer
	logger := log.New(&buf, "", 0) // flags=0: safe for JSON output

	newService(
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

	out := buf.String()
	if strings.Contains(out, "WARN:") {
		t.Errorf("unexpected WARN in log output when logger has flags=0 under JSON mode, got: %q", out)
	}
}

// TestNewService_TextMode_NoWarnFlaggedLogger verifies that the flagged-logger
// warning is NOT emitted when JSON mode is disabled, even if the logger has
// non-zero flags (flags only corrupt output in JSON-lines mode).
func TestNewService_TextMode_NoWarnFlaggedLogger(t *testing.T) {
	t.Setenv(EnvDaemonLogJSON, "") // JSON mode off

	var buf bytes.Buffer
	logger := log.New(&buf, "", log.LstdFlags)

	newService(
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

	out := buf.String()
	if strings.Contains(out, "WARN:") {
		t.Errorf("unexpected WARN in log output in text mode with flagged logger, got: %q", out)
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
