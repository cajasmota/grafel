package daemon

import (
	"log/slog"
	"os"
	"time"

	"github.com/cajasmota/grafel/internal/progress"
	"github.com/cajasmota/grafel/internal/registry"
)

// Split-mode progress bridge — READ side (ADR-0024 / epic #5729).
//
// In split mode serve and engine are separate OS processes, each with its own
// in-memory progress.Broker. The engine's rebuild tees its per-repo indexer
// publisher AND the group link tracker into a per-group NDJSON sidecar under
// GRAFEL_HOME/progress (see cmd/grafel/daemon.go's WithPublisher /
// NewTracker wiring, gated on SplitModeEnabled). Serve, which owns the Broker
// the dashboard/wizard SSE subscribes to, has no way to see those events —
// they were published into the engine's Broker, in another process.
//
// sidecarTailer closes that gap: started ONLY in the serve plane, it polls the
// per-group sidecars, tails each with a progress.SidecarReader, and republishes
// every reconstructed progress.Event into serve's Broker. The dashboard SSE
// then renders live per-repo / per-module rows exactly as it does in monolith
// mode — with NO handler change (the same /api/index-progress and the
// dashboard web-onboard Rebuild path both read serve's Broker).
//
// It MUST NOT run in the engine plane: the engine is the WRITER; tailing its
// own writes would pointlessly re-publish into a Broker no SSE client reads.
// The lifecycle mirrors startStatusWriter (statuswriter.go): a start func that
// launches one goroutine and returns a stop func that joins it on shutdown.

const (
	// defaultSidecarTailPoll is how often the tailer rescans the known groups
	// and tails each group's sidecar for newly-appended lines. Kept tight
	// (~poll cadence of the SSE heartbeat) so the dashboard feels live without
	// busy-spinning.
	defaultSidecarTailPoll = 120 * time.Millisecond

	// defaultSidecarPruneEvery bounds how often PruneTerminalSidecars runs so a
	// long-lived serve process does not accumulate terminated group files.
	defaultSidecarPruneEvery = 5 * time.Minute

	// defaultSidecarPruneMaxAge is the age past which a TERMINATED (done/error)
	// group sidecar is eligible for deletion. A live or fresh file is always
	// retained (see progress.PruneTerminalSidecars).
	defaultSidecarPruneMaxAge = 6 * time.Hour
)

// groupTailState is the tailer's per-group cursor: the reader, the byte offset
// consumed so far, and whether the group has reached a group-scoped terminal.
type groupTailState struct {
	reader   *progress.SidecarReader
	offset   int64
	terminal bool
}

// sidecarTailer tails every known group's progress sidecar and republishes the
// events into a serve-side progress.Publisher (the Broker feeding the dashboard
// SSE). Single-goroutine: all state (states map, lastPrune) is touched only by
// tick(), which run() calls serially, so no internal locking is needed.
type sidecarTailer struct {
	pub    progress.Publisher
	logger *slog.Logger

	interval    time.Duration
	pruneEvery  time.Duration
	pruneMaxAge time.Duration

	// groupsFn returns the group slugs to tail. Defaults to the registered
	// fleet groups; injectable in tests. Re-evaluated every tick so a group
	// registered mid-session (its sidecar appearing under GRAFEL_HOME/progress)
	// is picked up by the next rescan.
	groupsFn func() []string

	states    map[string]*groupTailState
	lastPrune time.Time

	stop chan struct{}
	done chan struct{}
}

// newSidecarTailer constructs a tailer that republishes into pub every interval.
// logger may be nil (best-effort read failures are then swallowed).
func newSidecarTailer(pub progress.Publisher, interval time.Duration, logger *slog.Logger) *sidecarTailer {
	if interval <= 0 {
		interval = defaultSidecarTailPoll
	}
	return &sidecarTailer{
		pub:         pub,
		logger:      logger,
		interval:    interval,
		pruneEvery:  defaultSidecarPruneEvery,
		pruneMaxAge: defaultSidecarPruneMaxAge,
		groupsFn:    registryGroupSlugs,
		states:      make(map[string]*groupTailState),
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
	}
}

// registryGroupSlugs returns the slug for every registered fleet group. The
// slug is the group Name — identical to the args.Group the engine's
// SidecarWriter is keyed on — so progress.SidecarReader(name) derives the same
// hashed on-disk path the writer appended to.
func registryGroupSlugs() []string {
	groups, err := registry.Groups()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		out = append(out, g.Name)
	}
	return out
}

// isGroupTerminal reports whether e is the GROUP-scoped terminal (the link
// tracker's Done/Fail, emitted with RepoSlug == GroupSlug). Per-repo terminals
// (RepoSlug != GroupSlug) do not stop tailing — other repos and the group link
// pass are still to come.
func isGroupTerminal(e progress.Event) bool {
	return e.RepoSlug == e.GroupSlug &&
		(e.Phase == progress.PhaseDone || e.Phase == progress.PhaseError)
}

// seedIfAlreadyTerminal peeks at st's sidecar from the start and, if it
// already contains a group-scoped terminal (isGroupTerminal), advances st's
// offset past that content and marks st.terminal — WITHOUT publishing any of
// the events read. Returns true when it seeded (the caller should skip
// further processing of this group for the current tick); false when the
// sidecar is not yet terminal, in which case st is left untouched so the
// caller falls through to the normal read-and-publish path (a run that was
// already in progress when this tailer first saw the group is still live and
// its current state should be surfaced immediately).
//
// Only called on first sight of a group (st freshly constructed, offset 0);
// see #5937.
func (t *sidecarTailer) seedIfAlreadyTerminal(st *groupTailState) bool {
	events, newOffset, err := st.reader.ReadFrom(0)
	if err != nil {
		if t.logger != nil {
			t.logger.Warn("progress tailer: read failed", "group", st.reader.Path(), "err", err)
		}
		return false
	}
	terminal := false
	for _, e := range events {
		if isGroupTerminal(e) {
			terminal = true
			break
		}
	}
	if !terminal {
		return false
	}
	st.offset = newOffset
	st.terminal = true
	return true
}

// shrank reports whether the on-disk sidecar is now smaller than the offset we
// consumed to — the signal that a NEW run truncated it (NewSidecarWriter /
// Reset) or it was compacted. Used to decide whether to RESUME a group we
// previously saw terminate.
func (t *sidecarTailer) shrank(st *groupTailState) bool {
	fi, err := os.Stat(st.reader.Path())
	if err != nil {
		return false
	}
	return fi.Size() < st.offset
}

// tick performs one poll pass: for each known group, tail newly-appended lines
// and republish them. Idempotent and safe to call repeatedly.
func (t *sidecarTailer) tick() {
	for _, slug := range t.groupsFn() {
		st := t.states[slug]
		if st == nil {
			r, err := progress.NewSidecarReader(slug)
			if err != nil {
				if t.logger != nil {
					t.logger.Warn("progress tailer: reader init failed", "group", slug, "err", err)
				}
				continue
			}
			st = &groupTailState{reader: r}
			t.states[slug] = st

			// #5937 — first-sight terminal seeding. This is the FIRST time this
			// tailer instance has looked at slug (e.g. serve just started). If
			// the sidecar's tail is already a group-scoped terminal, those
			// events belong to a run that ended before serve (and this tailer)
			// existed — republishing them would poison the broker's retained
			// terminal (internal/progress.Broker.terminal) and close every SSE
			// subscriber before any live event for the CURRENT run arrives
			// (#5937). Seed the cursor/terminal flag WITHOUT publishing; normal
			// live tailing (including the offset-reset/shrink-resume path
			// below) still applies to everything that happens afterward.
			if t.seedIfAlreadyTerminal(st) {
				continue
			}
		}

		// Once a group reached its group-scoped terminal we stop republishing —
		// there is nothing new until a fresh run. But a fresh run REUSES the same
		// file (truncate), so keep watching for a shrink and resume when one
		// appears (the offset-reset/replay path below then re-seeds from 0).
		if st.terminal {
			if !t.shrank(st) {
				continue
			}
			st.terminal = false
		}

		events, newOffset, err := st.reader.ReadFrom(st.offset)
		if err != nil {
			if t.logger != nil {
				t.logger.Warn("progress tailer: read failed", "group", slug, "err", err)
			}
			continue
		}
		// OFFSET-RESET HANDLING: when ReadFrom returns newOffset < st.offset the
		// file shrank (new run / compaction) and `events` is a full replay from
		// 0. We must NOT skip them — the dashboard fold is idempotent, so
		// republishing re-derives the current run's state. Saving newOffset
		// (smaller) re-seeds the cursor onto the new run.
		st.offset = newOffset
		for _, e := range events {
			t.pub.Publish(e)
			if isGroupTerminal(e) {
				st.terminal = true
			}
		}
	}
	t.maybePrune()
}

// maybePrune runs PruneTerminalSidecars at most once per pruneEvery so
// terminated group files do not accumulate over a long serve lifetime.
func (t *sidecarTailer) maybePrune() {
	if t.pruneEvery <= 0 {
		return
	}
	if time.Since(t.lastPrune) < t.pruneEvery {
		return
	}
	t.lastPrune = time.Now()
	if _, err := progress.PruneTerminalSidecars(t.pruneMaxAge); err != nil && t.logger != nil {
		t.logger.Warn("progress tailer: prune failed", "err", err)
	}
}

// run is the single tailer goroutine: tail once immediately (so a reconnecting
// dashboard sees current state promptly), then on every tick until stopped.
func (t *sidecarTailer) run() {
	defer close(t.done)
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()
	t.tick()
	for {
		select {
		case <-t.stop:
			return
		case <-ticker.C:
			t.tick()
		}
	}
}

// shutdown stops the tailer goroutine and joins it. Call exactly once.
func (t *sidecarTailer) shutdown() {
	close(t.stop)
	<-t.done
}

// startSidecarTailer launches the serve-side progress-sidecar tailer and
// returns a stop func that joins the goroutine on shutdown. Mirrors
// startStatusWriter's lifecycle contract. Started ONLY in the serve plane (see
// run() in server.go) — never in the engine, which is the writer.
func startSidecarTailer(pub progress.Publisher, interval time.Duration, logger *slog.Logger) (stop func()) {
	t := newSidecarTailer(pub, interval, logger)
	go t.run()
	return t.shutdown
}
