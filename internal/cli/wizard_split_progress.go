package cli

// wizard_split_progress.go — split-mode completion detection for the wizard
// index (#5751 follow-up / split-mode progress UX).
//
// THE BUG THIS FIXES: in split mode (SplitModeEnabled(), the default), the
// daemon's Service.Rebuild ENQUEUES the rebuild onto the engine queue and
// RETURNS IMMEDIATELY (fire-and-forget). The old wizard keyed completion on
// that async RPC return, so the TUI jumped 0→100% "Done" in <1s while the
// engine indexed for ~20min in the background, and no per-module rows rendered.
//
// THE FIX (this file): in split mode the wizard still enqueues the rebuild, but
// it KEEPS forwarding SSE per-module progress events to the TUI while it polls
// a LEVEL-TRIGGERED completion signal off the status plane (statusfile). The
// RPC return no longer marks completion. Engine-liveness (the engine-liveness
// heartbeat) is a failure backstop and an overall timeout bounds the wait, so a
// dead/wedged engine surfaces a real error instead of a fake "Done".
//
// Monolith mode is UNCHANGED: there Service.Rebuild is synchronous (the RPC
// return IS completion) and streamIndexWithSummary still uses
// forwardBrokerToChannel with the RPC's own stats.

import (
	"context"
	"fmt"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/client"
	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/progress"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/statusfile"
)

// runSplitIndex is the production split-mode driver invoked by
// streamIndexWithSummary. It builds the status-plane probe for the group and an
// enqueue closure that fires the async Rebuild RPC on its own goroutine (so a
// slow serve never stalls the completion poll), then delegates to
// runSplitIndexCore.
func runSplitIndex(
	ctx context.Context,
	cancel context.CancelFunc,
	c *client.Client,
	group, token string,
	sseCh <-chan sseEvent,
	evCh chan<- progress.Event,
) rebuildOutcome {
	probe, err := newStatusPlaneProbe(group)
	if err != nil {
		return rebuildOutcome{err: err}
	}
	trigger := func() {
		go func() {
			// Fire-and-forget: in split mode this enqueues onto the engine
			// queue and returns immediately. Its reply carries no stats, so we
			// ignore it — completion + stats come from the status-plane poll.
			_, _ = c.Rebuild(proto.RebuildArgs{Group: group, ProgressToken: token, Interactive: true})
		}()
	}
	return runSplitIndexCore(ctx, cancel, trigger, sseCh, evCh, probe, realSplitClock{}, defaultSplitPoll())
}

// splitClock is the injectable time seam so completion-poll tests advance
// virtual time instead of sleeping for real intervals.
type splitClock interface {
	Now() time.Time
	Sleep(d time.Duration)
}

// realSplitClock is the production clock.
type realSplitClock struct{}

func (realSplitClock) Now() time.Time        { return time.Now() }
func (realSplitClock) Sleep(d time.Duration) { time.Sleep(d) }

// splitSnapshot is one level-triggered reading of a group's split-mode index
// state, sourced from the status plane.
type splitSnapshot struct {
	// Indexed is true when EVERY repo in the group is freshly indexed
	// (status file present, not indexing, entities>0). Level-triggered: it
	// stays true once reached and is robust to dropped SSE events.
	Indexed bool
	// EngineAlive mirrors the engine-liveness heartbeat freshness. Used as a
	// FAILURE backstop: if the engine was alive and then goes stale before the
	// group finishes, the wait fails instead of hanging.
	EngineAlive bool
	// Entities / Rels are the summed status-plane counts across the group's
	// repos — the source of the final summary stats (the async split RPC reply
	// carries none).
	Entities int64
	Rels     int64
	// ElapsedSec is wall time since the poll started.
	ElapsedSec float64
}

// splitProbe polls the status plane for a group's completion snapshot. The
// production implementation reads statusfiles; tests inject a fake.
type splitProbe interface {
	Snapshot() (splitSnapshot, error)
}

// splitPollConfig tunes the completion poll.
type splitPollConfig struct {
	interval time.Duration // between polls
	timeout  time.Duration // overall bound; exceeding it is a failure
}

// defaultSplitPoll returns the production poll cadence: a 500ms interval and a
// 45-minute overall timeout (comfortably above the observed ~20min large-repo
// index, but bounded so a wedged engine never hangs the wizard forever).
func defaultSplitPoll() splitPollConfig {
	return splitPollConfig{interval: 500 * time.Millisecond, timeout: 45 * time.Minute}
}

// awaitSplitCompletion polls probe until the group is freshly indexed (success),
// the engine-liveness heartbeat goes stale AFTER having been alive (failure
// backstop), or the overall timeout elapses (failure). It NEVER returns success
// on the RPC-enqueue instant — completion is decided solely by the level-
// triggered status-plane signal.
func awaitSplitCompletion(probe splitProbe, clk splitClock, cfg splitPollConfig) (splitSnapshot, error) {
	start := clk.Now()
	sawAlive := false
	for {
		snap, err := probe.Snapshot()
		if err == nil {
			if snap.Indexed {
				return snap, nil
			}
			if snap.EngineAlive {
				sawAlive = true
			} else if sawAlive {
				// The engine was alive and is now stale without the group
				// having finished — a real failure, not a fake "Done".
				return splitSnapshot{}, fmt.Errorf("index engine stopped responding before the group finished indexing")
			}
		}
		if clk.Now().Sub(start) >= cfg.timeout {
			return splitSnapshot{}, fmt.Errorf("timed out after %s waiting for group to finish indexing", cfg.timeout)
		}
		clk.Sleep(cfg.interval)
	}
}

// forwardSSEUntilCancel forwards parsed progress.Events from the broker SSE
// stream onto evCh until ctx is cancelled or the stream closes. It runs
// CONCURRENTLY with the completion poll so the per-module bars render live
// throughout the whole index (the bars come from SSE, completion from the poll).
func forwardSSEUntilCancel(ctx context.Context, sseCh <-chan sseEvent, evCh chan<- progress.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sseCh:
			if !ok {
				return
			}
			if ev.name != "progress" || ev.data == "" {
				continue
			}
			var e progress.Event
			if jsonUnmarshalEvent(ev.data, &e) {
				select {
				case evCh <- e:
				default:
				}
			}
		}
	}
}

// runSplitIndexCore drives the split-mode index: it enqueues the async rebuild,
// forwards SSE events to the TUI CONCURRENTLY, and polls the status-plane probe
// for real completion. The returned rebuildOutcome carries the status-sourced
// stats on success, or a real error on engine-death/timeout — NEVER a fake Done.
func runSplitIndexCore(
	ctx context.Context,
	cancel context.CancelFunc,
	triggerRebuild func(),
	sseCh <-chan sseEvent,
	evCh chan<- progress.Event,
	probe splitProbe,
	clk splitClock,
	cfg splitPollConfig,
) rebuildOutcome {
	// 1. Enqueue the async rebuild. In split mode this returns almost
	//    immediately (fire-and-forget) — we deliberately do NOT key completion
	//    on it. The production closure spawns its own goroutine for the RPC, so
	//    this call is non-blocking; completion is decided by the poll below.
	triggerRebuild()

	// 2. Forward SSE per-module events to the TUI CONCURRENTLY so the bars
	//    render live for the whole index. Cancelled once completion is decided.
	go forwardSSEUntilCancel(ctx, sseCh, evCh)

	// 3. Poll the status plane for REAL, level-triggered completion.
	snap, err := awaitSplitCompletion(probe, clk, cfg)
	cancel() // stop the SSE forward goroutine
	if err != nil {
		return rebuildOutcome{err: err}
	}
	return rebuildOutcome{
		entities: snap.Entities,
		rels:     snap.Rels,
		elapsed:  snap.ElapsedSec,
	}
}

// statusReader reads one repo's status-plane sidecar (mirrors daemon.RepoStatusFile).
type statusReader func(repoPath string) (*statusfile.File, bool)

// livenessReader reads the engine-liveness heartbeat (mirrors daemon.EngineLivenessStatus).
type livenessReader func(root string) (f *statusfile.File, fresh bool)

// statusPlaneProbe is the production splitProbe: it reads each group repo's
// status-plane sidecar (graph_fb_mtime / indexing) and the engine-liveness
// heartbeat. It captures a per-repo graph_fb_mtime baseline at construction so
// completion is "graph.fb was (re)written past the pre-enqueue mtime".
type statusPlaneProbe struct {
	repoPaths    []string
	root         string
	baseline     map[string]int64 // per-repo graph_fb_mtime captured BEFORE enqueue
	readStatus   statusReader
	readLiveness livenessReader
	started      time.Time
}

// newStatusPlaneProbe builds a status-plane probe for group by loading its
// registry config to enumerate repo paths and resolving the daemon layout root
// for the engine-liveness sidecar.
func newStatusPlaneProbe(group string) (*statusPlaneProbe, error) {
	cfgPath, err := registry.ConfigPathFor(group)
	if err != nil {
		return nil, err
	}
	cfg, err := registry.LoadGroupConfig(cfgPath)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(cfg.Repos))
	for _, r := range cfg.Repos {
		paths = append(paths, r.Path)
	}
	root := ""
	if layout, lErr := daemon.DefaultLayout(); lErr == nil {
		root = layout.Root
	}
	return newStatusPlaneProbeWith(paths, root, daemon.RepoStatusFile, daemon.EngineLivenessStatus), nil
}

// newStatusPlaneProbeWith builds a probe with injected status/liveness readers
// (production wires the real daemon funcs; tests inject fakes). It captures the
// per-repo graph_fb_mtime BASELINE right now — BEFORE the rebuild is enqueued —
// so completion can be detected as graph.fb being (re)written past this point.
// For a fresh group the repos have no status file yet, so their baseline is 0.
func newStatusPlaneProbeWith(paths []string, root string, rs statusReader, lr livenessReader) *statusPlaneProbe {
	p := &statusPlaneProbe{
		repoPaths:    paths,
		root:         root,
		baseline:     make(map[string]int64, len(paths)),
		readStatus:   rs,
		readLiveness: lr,
		started:      time.Now(),
	}
	for _, rp := range paths {
		if f, ok := rs(rp); ok && f != nil {
			p.baseline[rp] = f.GraphFBMtime
		}
	}
	return p
}

// Snapshot reads the status plane once. Indexed is true only when every group
// repo has a status file that is not indexing and whose graph_fb_mtime has
// ADVANCED past the pre-enqueue baseline (graph.fb was (re)written == index
// completed). Entities/Relationships are NOT part of the completion predicate
// because the wizard/rebuild code path does not write the graph-stats sidecar,
// so Entities can stay 0 on a successful index (tracked as a separate bug); we
// still surface whatever counts the status plane does have.
func (p *statusPlaneProbe) Snapshot() (splitSnapshot, error) {
	var ent, rels int64
	allIndexed := len(p.repoPaths) > 0
	for _, rp := range p.repoPaths {
		f, ok := p.readStatus(rp)
		if !ok || f == nil || f.Indexing || f.GraphFBMtime <= p.baseline[rp] {
			allIndexed = false
		}
		if ok && f != nil {
			ent += f.Entities
			rels += f.Relationships
		}
	}
	_, alive := p.readLiveness(p.root)
	return splitSnapshot{
		Indexed:     allIndexed,
		EngineAlive: alive,
		Entities:    ent,
		Rels:        rels,
		ElapsedSec:  time.Since(p.started).Seconds(),
	}, nil
}
