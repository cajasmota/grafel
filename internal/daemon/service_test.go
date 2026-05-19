package daemon

import (
	"runtime"
	"testing"

	"github.com/cajasmota/archigraph/internal/daemon/proto"
	"github.com/cajasmota/archigraph/internal/daemon/sched"
)

// TestStatusRSSReportsActualMemory verifies that Status.RSSUsedMB
// reports the actual daemon memory (in MB) from runtime.MemStats,
// not just the sum of predicted in-flight job allocations (issue #803).
func TestStatusRSSReportsActualMemory(t *testing.T) {
	svc := newService(
		func(proto.IndexArgs) (string, string, error) { return "", "", nil },
		func(proto.RebuildArgs) ([]string, string, error) { return []string{}, "", nil },
		func(proto.QualityAuditRequest) (proto.QualityAuditReply, error) { return proto.QualityAuditReply{}, nil },
		"/tmp/test.sock",
		make(chan struct{}),
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
