package cli

// status_daemon_detail.go -- the daemon-section renderer for `grafel status`,
// split out of runStatus so the print gates are unit-testable without dialing a
// real daemon (#6014). runStatus itself needs a live client.Dial, which put
// every one of these conditionals out of reach of a test -- and an unreachable
// print gate is precisely how #6014's engine-sourced rebuild count came to be
// computed correctly and then never shown to anyone.

import (
	"fmt"
	"io"
	"strings"

	"github.com/cajasmota/grafel/internal/daemon/proto"
)

// printDaemonDetail renders the per-subsystem detail lines of the daemon
// section: watcher, scheduler, annotation pass, stage gate, rebuild counters,
// memory/admission, indexed repos and the recent-event tail. Every line is
// individually gated on its subsystem being present or busy, so an idle daemon
// prints only the header lines runStatus emits directly.
func printDaemonDetail(w io.Writer, st proto.StatusReply) {
	if st.WatcherRepos > 0 || st.WatcherEvents > 0 {
		fmt.Fprintf(w, "  watcher: repos=%d dirs=%d events=%d dropped=%d",
			st.WatcherRepos, st.WatcherDirs, st.WatcherEvents, st.WatcherDropped)
		// PH2a (#2096): show pause/resume slot counts when available.
		if st.WatcherActiveSlots > 0 || st.WatcherPausedSlots > 0 {
			fmt.Fprintf(w, " slots_active=%d slots_paused=%d",
				st.WatcherActiveSlots, st.WatcherPausedSlots)
		}
		fmt.Fprintln(w)
	}
	// #6180: a repo the watcher refused for want of file descriptors is not
	// being watched at all. Printed on its own line and NOT gated on the
	// condition above — the worst case is exactly the one where WatcherRepos
	// is zero because every repo was refused.
	if st.WatcherUnwatched > 0 {
		fmt.Fprintf(w, "  watcher: %d repo(s) NOT WATCHED — file-descriptor budget full (%d/%d used); edits there will not re-index\n",
			st.WatcherUnwatched, st.WatcherFDUsed, st.WatcherFDLimit)
	}
	if st.QueueLen > 0 || len(st.IndexInFlight) > 0 ||
		len(st.PendingAlgo) > 0 || len(st.PendingLinks) > 0 {
		fmt.Fprintf(w, "  scheduler: queue=%d in_flight=%d pending_algo=%d pending_links=%d\n",
			st.QueueLen, len(st.IndexInFlight), len(st.PendingAlgo), len(st.PendingLinks))
	}
	printAnnotationStatus(w, st)
	// #5954 heavy write-stage gate. Printed whenever the gate is doing
	// ANYTHING (holding, deferring, or being barged) so an operator — or
	// a peak-RSS measurement run — can confirm from outside the process
	// that it fired, instead of inferring it from RSS shape. Silent when
	// the gate is idle, which is the overwhelmingly common case.
	if st.StageGateHolder != "" || len(st.StageGateDeferred) > 0 || len(st.StageGateBarging) > 0 ||
		st.StageGateForfeits > 0 {
		fmt.Fprint(w, "  stage_gate:")
		if len(st.StageGateBarging) > 0 {
			fmt.Fprintf(w, " barging=%s", strings.Join(st.StageGateBarging, ","))
		}
		if st.StageGateHolder != "" {
			fmt.Fprintf(w, " holder=%s", st.StageGateHolder)
			// LIVE, unlike FORFEITS below: this holder blew the 4h
			// hold-max and the gate is inside its forfeit grace right
			// now. It is the state an operator staring at a stalled
			// daemon is trying to identify, so it rides on the holder.
			if st.StageGateForfeitedHolder {
				fmt.Fprint(w, "(FORFEITED,awaiting-cancel)")
			}
		}
		if len(st.StageGateDeferred) > 0 {
			fmt.Fprintf(w, " deferred=%s", strings.Join(st.StageGateDeferred, ","))
		}
		// Sticky: a forfeit is a failure, so it stays on the line for
		// the life of the daemon rather than vanishing with the holder.
		if st.StageGateForfeits > 0 {
			fmt.Fprintf(w, " FORFEITS=%d", st.StageGateForfeits)
		}
		fmt.Fprintln(w)
	}
	// #6014: the rebuild counters are printed OUTSIDE the RSSBudgetMB block
	// below. RSSBudgetMB is populated only from an in-process scheduler
	// (Service.Status' `if s.scheduler != nil` branch), and in SPLIT MODE -- the
	// default since ADR-0024 -- serve has no scheduler, so it is 0 and that
	// whole block never prints. Nesting this line there made the engine-sourced
	// rebuild count unreachable on the one surface an operator actually reads:
	// the number crossed the process boundary correctly and was then discarded
	// by the print gate. RebuildConcurrencyCap is set unconditionally by
	// Service.Status, so gating on it alone is both sufficient and mode-neutral.
	if st.RebuildConcurrencyCap > 0 {
		fmt.Fprintf(w, "  rebuild: in_flight=%d / cap=%d\n",
			st.RebuildInFlight, st.RebuildConcurrencyCap)
	}
	if st.RSSBudgetMB > 0 {
		// Two separate lines: daemon idle RSS (informational) vs.
		// admission budget (delta-based predicted in-flight sum).
		// These are intentionally distinct — idle RSS can exceed the
		// budget without blocking jobs, because jobs are only blocked
		// when sum(predicted_in_flight) + new_job_pred > budget.
		// #3648: report the honest process footprint (resident set
		// size), not the old MemStats.Sys mislabel. On macOS RSS
		// under-counts swapped/compressed pages — note that, plus the
		// Go-heap breakdown, so the number is interpretable.
		fmt.Fprintf(w, "  mem: footprint=%dMB heap_inuse=%dMB heap_released=%dMB go_sys=%dMB\n",
			st.RSSUsedMB,
			st.HeapInuseBytes/(1024*1024),
			st.HeapReleasedBytes/(1024*1024),
			st.SysBytes/(1024*1024))
		if st.FootprintLabel != "" {
			fmt.Fprintf(w, "       (footprint = %s)\n", st.FootprintLabel)
		}
		admHeadroom := st.RSSBudgetMB - st.AdmissionUsedMB
		if admHeadroom < 0 {
			admHeadroom = 0
		}
		fmt.Fprintf(w, "  admission: queued=%d admitted=%d predicted=%dMB / budget=%dMB (headroom=%dMB)\n",
			len(st.BlockedJobs), len(st.InFlightJobs),
			st.AdmissionUsedMB, st.RSSBudgetMB, admHeadroom)
		if len(st.InFlightJobs) > 0 {
			for _, j := range st.InFlightJobs {
				fmt.Fprintf(w, "    admitted: %s (predicted=%dMB)\n", j.Path, j.PredictedMB)
			}
		}
		if len(st.BlockedJobs) > 0 {
			for _, p := range st.BlockedJobs {
				fmt.Fprintf(w, "    queued:   %s\n", p)
			}
		}
	}
	if len(st.IndexedRepos) > 0 {
		fmt.Fprintln(w, "  indexed repos:")
		for _, r := range st.IndexedRepos {
			last := r.LastIndex
			if last == "" {
				last = "(never)"
			}
			fmt.Fprintf(w, "    %s  last_index=%s  indexes=%d  algos=%d",
				r.Path, last, r.IndexCount, r.AlgoCount)
			if r.LastErr != "" {
				fmt.Fprintf(w, "  err=%s", r.LastErr)
			}
			// #5727/#5729-W1: show the exact indexed commit + whether
			// the on-disk graph still matches HEAD.
			if r.IndexedCommitShort != "" {
				fmt.Fprintf(w, "  commit=%s at_head=%v", r.IndexedCommitShort, r.AtHead)
			}
			// #6107: the measured peak reached the wire but was never
			// rendered, so the number that governs this repo's admission was
			// invisible. That matters more now there is no TTL: a wrong entry
			// has no automatic way to clear, and an operator cannot use the
			// manual escape hatch (delete the entry) without first being able
			// to see it. Always print the src alongside — the peak survives
			// runs that measured nothing, so without it a value carried over
			// from an older full index reads as a fresh one.
			if r.LastPeakMB > 0 {
				src := r.LastPeakSrc
				if src == "" {
					src = "unknown"
				}
				fmt.Fprintf(w, "  last_peak=%dMB(%s)", r.LastPeakMB, src)
			}
			fmt.Fprintln(w)
		}
	}
	if n := len(st.RecentLog); n > 0 {
		start := n - 5
		if start < 0 {
			start = 0
		}
		fmt.Fprintln(w, "  recent events:")
		for _, e := range st.RecentLog[start:] {
			line := fmt.Sprintf("    %s  %s", e.Time, e.Kind)
			if e.Repo != "" {
				line += "  " + e.Repo
			}
			if e.Msg != "" {
				line += "  " + e.Msg
			}
			fmt.Fprintln(w, line)
		}
	}
}
