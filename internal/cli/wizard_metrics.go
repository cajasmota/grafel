package cli

// wizard_metrics.go — the wizard index screen's live CPU/RAM readout (wizard
// CPU/RAM readout). Motivation: a large-monorepo rebuild has a multi-minute
// enrichment phase AFTER the core index where the overall progress bar sits
// near 100% and looks stuck; a live "CPU 412% · 2.3 GB" readout next to the
// bar reassures the user the engine is still doing real work.
//
// Split mode is the DEFAULT (ADR-0024): the ENGINE child process does the
// indexing, not the wizard's own (serve-side) process, so the CPU/RAM to show
// is the engine's — surfaced via the SAME status-plane engine-liveness
// sidecar the split-mode completion probe already reads (see
// wizard_split_progress.go's livenessReader / daemon.EngineLivenessStatus).
// Monolith mode (split disabled) writes the identical sidecar from the one
// daemon process that both serves and indexes, so this reads unchanged in
// either mode — no branching required here.

import (
	"github.com/cajasmota/grafel/internal/cli/wiztui"
	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/statusfile"
)

// engineMetricsReader mirrors daemon.EngineLivenessStatus — injectable so
// engineMetricsFuncWith is testable without a live engine process.
type engineMetricsReader func(root string) (f *statusfile.File, fresh bool)

// engineMetricsFuncWith builds a wiztui.MetricsFunc bound to root using the
// injected reader. Never errors: a missing or stale sidecar (engine not
// started yet, monolith mode with no split, an old engine binary predating
// RSSMB/CPUPct) yields the zero wiztui.Metrics, which wiztui's index view
// renders as "no readout" rather than a misleading 0%/0.0 GB — see
// indexView.metricSuffix's doc.
func engineMetricsFuncWith(root string, read engineMetricsReader) wiztui.MetricsFunc {
	return func() wiztui.Metrics {
		f, fresh := read(root)
		if !fresh || f == nil {
			return wiztui.Metrics{}
		}
		return wiztui.Metrics{RSSMB: f.RSSMB, CPUPct: f.CPUPct}
	}
}

// wizardMetricsFunc is the production MetricsFunc wired into the wizard's
// index screen. Resolves the daemon layout root ONCE (cheap, no I/O beyond
// resolving GRAFEL_HOME) and reads daemon.EngineLivenessStatus — the exact
// same on-disk sidecar the split-mode completion probe polls — on every tick.
// Returns nil (readout disabled outright) only when the layout itself cannot
// be resolved, a hard environment failure distinct from "engine not running
// yet" (which EngineLivenessStatus already reports as fresh=false, degrading
// to an empty readout rather than disabling the poll).
func wizardMetricsFunc() wiztui.MetricsFunc {
	layout, err := daemon.DefaultLayout()
	if err != nil {
		return nil
	}
	return engineMetricsFuncWith(layout.Root, daemon.EngineLivenessStatus)
}
