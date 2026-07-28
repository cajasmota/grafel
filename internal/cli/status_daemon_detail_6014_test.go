package cli

// status_daemon_detail_6014_test.go — #6014, the print gate.
//
// The engine-sourced rebuild count reached StatusReply.RebuildInFlight
// correctly and was then thrown away by the renderer: the `rebuild:` line lived
// inside `if st.RSSBudgetMB > 0`, and RSSBudgetMB is populated only from an
// IN-PROCESS scheduler (Service.Status' `if s.scheduler != nil` branch). In
// split mode — the default since ADR-0024 — serve has no scheduler, so
// RSSBudgetMB is 0 and the whole block, rebuild line included, never printed.
// `grafel status` showed no `rebuild:` line at all.
//
// That is the same shape as the bug being fixed: a number that is right on the
// wire and absent on the screen. So the gate is pinned here, in the one
// configuration that matters — no scheduler, no RSS budget.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon/proto"
)

// TestPrintDaemonDetail_RebuildLinePrintsWithNoRSSBudget is the regression: a
// split-mode serve reply (scheduler-derived fields all zero) must still show
// the rebuild counters. Re-nesting the line under RSSBudgetMB kills this test.
func TestPrintDaemonDetail_RebuildLinePrintsWithNoRSSBudget(t *testing.T) {
	var buf bytes.Buffer
	printDaemonDetail(&buf, proto.StatusReply{
		// Split-mode serve: no scheduler, so every scheduler-sourced field is
		// zero — RSSBudgetMB above all.
		RSSBudgetMB:           0,
		RebuildInFlight:       1,
		RebuildConcurrencyCap: 2,
	})
	out := buf.String()

	if !strings.Contains(out, "rebuild: in_flight=1 / cap=2") {
		t.Errorf("`rebuild:` line missing from `grafel status` output with RSSBudgetMB=0.\n"+
			"This is the DEFAULT deployment mode: serve has no scheduler, so a rebuild line gated on the "+
			"RSS-budget block is unreachable and the engine-sourced count is computed, transmitted, and "+
			"then discarded before anyone sees it.\ngot:\n%s", out)
	}
	// Guard the hoist rather than the copy: the RSS/admission block must still
	// be suppressed when there is no budget to report.
	if strings.Contains(out, "admission:") {
		t.Errorf("admission block printed with RSSBudgetMB=0; the rebuild line should have been hoisted "+
			"OUT of that block, not the block un-gated.\ngot:\n%s", out)
	}
}

// TestPrintDaemonDetail_RebuildLineStillPrintsWithRSSBudget: the monolith path
// (scheduler attached, budget reported) must be unchanged by the hoist.
func TestPrintDaemonDetail_RebuildLineStillPrintsWithRSSBudget(t *testing.T) {
	var buf bytes.Buffer
	printDaemonDetail(&buf, proto.StatusReply{
		RSSBudgetMB:           4096,
		RebuildInFlight:       3,
		RebuildConcurrencyCap: 4,
	})
	out := buf.String()
	if !strings.Contains(out, "rebuild: in_flight=3 / cap=4") {
		t.Errorf("`rebuild:` line missing in monolith mode (RSSBudgetMB > 0).\ngot:\n%s", out)
	}
	if !strings.Contains(out, "admission:") {
		t.Errorf("admission block missing with RSSBudgetMB > 0 — the hoist changed monolith output.\ngot:\n%s", out)
	}
}

// TestPrintDaemonDetail_NoRebuildLineWithoutCap: RebuildConcurrencyCap is set
// unconditionally by Service.Status, so a zero means the reply did not come
// from a daemon that reports it (an old daemon, or a zero-valued fixture).
// Printing `cap=0` there would be noise, not information.
func TestPrintDaemonDetail_NoRebuildLineWithoutCap(t *testing.T) {
	var buf bytes.Buffer
	printDaemonDetail(&buf, proto.StatusReply{})
	if out := buf.String(); strings.Contains(out, "rebuild:") {
		t.Errorf("`rebuild:` line printed with RebuildConcurrencyCap=0.\ngot:\n%s", out)
	}
}
