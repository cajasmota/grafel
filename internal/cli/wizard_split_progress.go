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
// COMPLETION SIGNAL: the engine finishing OUR group rebuild — observed as the
// KindRebuild request we enqueued (identified by our ProgressToken) being
// drained+acked, i.e. daemon.RebuildRequestPending going false. This is
// authoritative and race-free (the enqueue RPC returns only after the request
// is on disk, and we poll for ITS disappearance) and, crucially, it fires even
// when a repo fails / is empty / is skipped — where a "every repo's graph.fb
// advanced" predicate would hang until the overall timeout. graph_fb_mtime is
// then used only to CLASSIFY the per-repo result once the rebuild is done.
//
// While polling we KEEP forwarding SSE per-module progress events so the bars
// render live. Engine-liveness is a failure backstop (never-alive fast-fail +
// alive→stale), and the overall timeout is a last-resort bound.
//
// Monolith mode is UNCHANGED: there Service.Rebuild is synchronous (the RPC
// return IS completion) and streamIndexWithSummary still uses
// forwardBrokerToChannel with the RPC's own stats.

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/client"
	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/progress"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/statusfile"
)

// runSplitIndex is the production split-mode driver invoked by
// streamIndexWithSummary. It builds the status-plane probe for the group (which
// captures the per-repo graph_fb_mtime baseline BEFORE the enqueue) and a
// synchronous enqueue closure, then delegates to runSplitIndexCore.
// onQueryable is invoked AT MOST ONCE, mid-poll, the first time the group
// becomes graph-queryable (AllAdvanced) while the background enhancement pass
// is still running (RequestPending still true). It carries the classified
// queryable-time stats. nil is fine — it simply means "no interim checkpoint
// wanted" (the caller only cares about the terminal outcome).
type onQueryableFunc func(splitResult)

func runSplitIndex(
	ctx context.Context,
	cancel context.CancelFunc,
	c *client.Client,
	group, token string,
	sseCh <-chan sseEvent,
	evCh chan<- progress.Event,
	onQueryable onQueryableFunc,
) rebuildOutcome {
	probe, err := newStatusPlaneProbe(group, token)
	if err != nil {
		return rebuildOutcome{err: err}
	}
	// Synchronous enqueue: in split mode Service.Rebuild writes the KindRebuild
	// request to disk and returns immediately, so this is fast. Returning only
	// after the RPC completes (a) race-free gates the request-ack poll below
	// (the request exists before we look for its disappearance) and (b) fixes
	// the goroutine-leak (N1) — nothing outlives c.Close on an early return.
	trigger := func() error {
		_, rErr := c.Rebuild(proto.RebuildArgs{Group: group, ProgressToken: token, Interactive: true})
		return rErr
	}
	return runSplitIndexCore(ctx, cancel, trigger, sseCh, evCh, probe, realSplitClock{}, defaultSplitPoll(), onQueryable)
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

// splitPoll is one reading of the completion loop's inputs: whether our rebuild
// request is still queued, and whether the engine is live.
type splitPoll struct {
	// RequestPending is true while our KindRebuild request is still on the
	// engine queue. false == the engine drained+acked it == our group rebuild
	// finished (success OR partial); the poll then stops and classifies.
	RequestPending bool
	// EngineAlive mirrors the engine-liveness heartbeat freshness (backstop).
	EngineAlive bool
	// AllAdvanced is true IFF every group repo is already graph-queryable — its
	// status shows !Indexing and a graph_fb_mtime advanced past the pre-enqueue
	// baseline. This is the ~6-min "graph queryable" completion signal: the engine
	// has written every repo's graph.fb (and, via the engine-side pre-linksFn
	// flush, stamped fresh status) but is still running the in-process linksFn tail
	// in the BACKGROUND. It lets the wizard complete SUCCESS early without waiting
	// for the full rebuild ack. It only ever goes true on a genuine mtime advance
	// (the flush writes fresh status under the repolock before the ack), so a stale
	// read keeps it false — no false-success. When any repo fails/empties it never
	// becomes true, so completion falls through to the ack backstop (RequestPending).
	AllAdvanced bool
}

// splitResult is the classified outcome, produced once the rebuild is done.
type splitResult struct {
	// Failed names the repos that did NOT produce a fresh graph (with a reason).
	// Empty == every repo indexed OK.
	Failed []string
	// Entities / Rels are summed across the repos that DID index (status plane;
	// may be 0 on the wizard path where the graph-stats sidecar isn't written).
	Entities int64
	Rels     int64
	// Repos carries the PER-REPO classified result (slug, entities, rels,
	// advanced-vs-failed) for every repo in the group config, one entry each —
	// the same repos summed into Entities/Rels above, individually broken out.
	// Threaded to the wiztui model (via rebuildOutcome.repoStats →
	// IndexOutcome.RepoStats) so a repo whose progress SSE events never
	// arrived still gets its real final count and Done/Error state on the
	// completion screen instead of staying blank/queued (#seed-rows dropped-
	// row fix — the live bug where a 3-repo group's per-repo rows summed to
	// less than the reported aggregate total).
	Repos []splitRepoResult
}

// splitRepoResult is one repo's classified final result (see
// splitResult.Repos). Slug matches registry.Repo.Slug / the progress.Event
// RepoSlug the same repo's SSE ticks carry, so it overlays the correct wiztui
// row rather than creating a duplicate.
type splitRepoResult struct {
	Slug     string
	Entities int64
	Rels     int64
	Failed   bool
	Reason   string // non-empty only when Failed
}

// splitProbe is the seam the completion loop polls. Poll is called repeatedly;
// Classify is called exactly once, after Poll reports RequestPending==false.
type splitProbe interface {
	Poll() (splitPoll, error)
	Classify() (splitResult, error)
}

// splitPollConfig tunes the completion poll.
type splitPollConfig struct {
	interval      time.Duration // between polls
	startupWindow time.Duration // fast-fail if the engine is never live within this (S1)
	timeout       time.Duration // last-resort overall bound
}

// defaultSplitPoll returns the production poll cadence: a 500ms interval, a 30s
// never-alive fast-fail window, and a 45-minute last-resort timeout (well above
// the observed ~20min large-repo index, but bounded so a genuinely wedged
// engine never hangs the wizard forever). With the request-ack signal the
// timeout should essentially never be hit.
func defaultSplitPoll() splitPollConfig {
	return splitPollConfig{
		interval:      500 * time.Millisecond,
		startupWindow: 30 * time.Second,
		timeout:       45 * time.Minute,
	}
}

// awaitSplitCompletion polls probe until our group rebuild is drained+acked
// (RequestPending==false → classify + return), or a failure fires: the engine
// is never seen live within startupWindow (S1 fast-fail), the engine was live
// and then went stale (backstop), or the overall timeout elapses (last resort).
// It NEVER returns success on the enqueue instant — completion is the request
// ack, not the RPC return.
//
// QUERYABLE CHECKPOINT: when every repo becomes graph-queryable (AllAdvanced)
// while the request is STILL pending (the background linksFn tail hasn't
// acked yet), the loop does NOT return early anymore — it invokes onQueryable
// (at most once, with the classified queryable-time stats) and keeps polling
// for the real ack, so the TUI can offer "queryable, enhancing in the
// background" instead of collapsing straight to Done. If AllAdvanced and
// !RequestPending land in the SAME poll (the rebuild was already fully done),
// the ack check runs first, so no interim fires — straight to the terminal
// return, matching the pre-existing fast/failure-path behavior.
func awaitSplitCompletion(probe splitProbe, clk splitClock, cfg splitPollConfig, onQueryable onQueryableFunc) (splitResult, error) {
	start := clk.Now()
	sawAlive := false
	queryableFired := false
	for {
		p, err := probe.Poll()
		if err == nil {
			if !p.RequestPending {
				// The engine drained+acked our rebuild request — real completion,
				// checked FIRST so an AllAdvanced-and-acked poll never fires an
				// interim (nothing left to wait for).
				return probe.Classify()
			}
			if p.AllAdvanced && !queryableFired {
				queryableFired = true
				if onQueryable != nil {
					if res, cErr := probe.Classify(); cErr == nil {
						onQueryable(res)
					}
				}
			}
			if p.EngineAlive {
				sawAlive = true
			} else if sawAlive {
				return splitResult{}, fmt.Errorf("index engine stopped responding before the group rebuild finished")
			}
		}
		elapsed := clk.Now().Sub(start)
		if !sawAlive && cfg.startupWindow > 0 && elapsed >= cfg.startupWindow {
			// S1: the engine never came alive — fail fast, don't wait the full timeout.
			return splitResult{}, fmt.Errorf("index engine never became live within %s; is the daemon/engine running?", cfg.startupWindow)
		}
		if elapsed >= cfg.timeout {
			return splitResult{}, fmt.Errorf("timed out after %s waiting for the group rebuild to finish", cfg.timeout)
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

// runSplitIndexCore drives the split-mode index: it forwards SSE events to the
// TUI CONCURRENTLY, enqueues the async rebuild (synchronously, so the ack poll
// is race-free), and polls until the engine finishes OUR rebuild, then
// classifies. The returned rebuildOutcome carries the status-sourced stats on a
// clean success, or a PROMPT error naming the repo(s) that did not index — and
// on engine-death/timeout — NEVER a fake Done and NEVER a 45m hang on a partial
// failure.
func runSplitIndexCore(
	ctx context.Context,
	cancel context.CancelFunc,
	triggerRebuild func() error,
	sseCh <-chan sseEvent,
	evCh chan<- progress.Event,
	probe splitProbe,
	clk splitClock,
	cfg splitPollConfig,
	onQueryable onQueryableFunc,
) rebuildOutcome {
	// 1. Start forwarding SSE per-module events CONCURRENTLY so the bars render
	//    live from the first moment (even during the enqueue).
	go forwardSSEUntilCancel(ctx, sseCh, evCh)

	// 2. Enqueue the rebuild synchronously. The RPC returns only once the
	//    request is on disk, which race-free gates the ack poll below.
	if err := triggerRebuild(); err != nil {
		cancel()
		return rebuildOutcome{err: fmt.Errorf("enqueue rebuild: %w", err)}
	}

	// 3. Poll for real completion (the request ack), then classify. onQueryable
	//    may fire once, mid-poll, on the AllAdvanced-while-pending checkpoint.
	res, err := awaitSplitCompletion(probe, clk, cfg, onQueryable)
	cancel() // stop the SSE forward goroutine
	if err != nil {
		return rebuildOutcome{err: err}
	}
	if len(res.Failed) > 0 {
		// Partial failure: the engine finished, but some repos produced no
		// graph. Surface a PROMPT, clear terminal error naming them — never a
		// hang, never a silent fake success.
		return rebuildOutcome{err: fmt.Errorf("index did not complete for %d repo(s): %s", len(res.Failed), strings.Join(res.Failed, "; "))}
	}
	return rebuildOutcome{
		entities:  res.Entities,
		rels:      res.Rels,
		repoStats: res.Repos,
	}
}

// statusReader reads one repo's status-plane sidecar (mirrors daemon.RepoStatusFile).
type statusReader func(repoPath string) (*statusfile.File, bool)

// livenessReader reads the engine-liveness heartbeat (mirrors daemon.EngineLivenessStatus).
type livenessReader func(root string) (f *statusfile.File, fresh bool)

// pendingReader reports whether our rebuild request is still queued (mirrors
// daemon.RebuildRequestPending bound to this group + token).
type pendingReader func() (bool, error)

// statusPlaneProbe is the production splitProbe. Poll consults the request-ack
// queue (completion) + engine-liveness (backstop); Classify inspects each group
// repo's status file vs the pre-enqueue graph_fb_mtime baseline. Entities are
// NOT part of the completion/classification gate (the wizard/rebuild path does
// not write the graph-stats sidecar, so a successfully-indexed repo can report
// entities:0) — graph_fb_mtime advancing is the "graph was written" signal.
type statusPlaneProbe struct {
	repoPaths    []string
	root         string
	baseline     map[string]int64 // per-repo graph_fb_mtime captured BEFORE enqueue
	readStatus   statusReader
	readLiveness livenessReader
	pending      pendingReader
}

// newStatusPlaneProbe builds a status-plane probe for group by loading its
// registry config to enumerate repo paths and resolving the daemon layout root
// for the engine-liveness sidecar. token scopes the request-ack check to OUR
// specific rebuild.
func newStatusPlaneProbe(group, token string) (*statusPlaneProbe, error) {
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
	pending := func() (bool, error) { return daemon.RebuildRequestPending(group, token) }
	return newStatusPlaneProbeWith(paths, root, daemon.RepoStatusFile, daemon.EngineLivenessStatus, pending), nil
}

// newStatusPlaneProbeWith builds a probe with injected readers (production wires
// the real daemon funcs; tests inject fakes). It captures the per-repo
// graph_fb_mtime BASELINE right now — BEFORE the rebuild is enqueued — so a
// completed index is detected as graph.fb being (re)written past this point. For
// a fresh group the repos have no status file yet, so their baseline is 0.
func newStatusPlaneProbeWith(paths []string, root string, rs statusReader, lr livenessReader, pending pendingReader) *statusPlaneProbe {
	p := &statusPlaneProbe{
		repoPaths:    paths,
		root:         root,
		baseline:     make(map[string]int64, len(paths)),
		readStatus:   rs,
		readLiveness: lr,
		pending:      pending,
	}
	for _, rp := range paths {
		if f, ok := rs(rp); ok && f != nil {
			p.baseline[rp] = f.GraphFBMtime
		}
	}
	return p
}

// repoAdvanced reports whether a repo's status file shows it is graph-queryable:
// the file exists, it is not mid-index, and its graph_fb_mtime advanced past the
// pre-enqueue baseline (i.e. graph.fb was (re)written by THIS rebuild). Shared by
// Poll (the AllAdvanced early-completion predicate) and Classify (per-repo result)
// so both use one definition of "this repo produced a fresh graph".
func repoAdvanced(f *statusfile.File, baseline int64) bool {
	return f != nil && !f.Indexing && f.GraphFBMtime > baseline
}

// Poll reads the completion signal (our request still queued?), engine liveness,
// and the early "graph queryable" signal (AllAdvanced) in one shot. AllAdvanced
// is true only when EVERY group repo already reports a fresh graph past its
// baseline — the engine has written all graph.fb + flushed fresh status under the
// repolock (before the ack) while the background linksFn tail still runs.
func (p *statusPlaneProbe) Poll() (splitPoll, error) {
	pend, err := p.pending()
	if err != nil {
		return splitPoll{}, err
	}
	_, alive := p.readLiveness(p.root)
	allAdvanced := len(p.repoPaths) > 0
	for _, rp := range p.repoPaths {
		f, ok := p.readStatus(rp)
		if !ok || !repoAdvanced(f, p.baseline[rp]) {
			allAdvanced = false
			break
		}
	}
	return splitPoll{RequestPending: pend, EngineAlive: alive, AllAdvanced: allAdvanced}, nil
}

// Classify inspects each group repo AFTER the rebuild finished. A repo counts as
// indexed-OK when its status file exists, is not indexing, and its
// graph_fb_mtime advanced past the pre-enqueue baseline; otherwise it is listed
// as failed (with LastErr if the status plane recorded one, else a generic
// "produced no graph"). Stats are summed only over the OK repos.
func (p *statusPlaneProbe) Classify() (splitResult, error) {
	var res splitResult
	for _, rp := range p.repoPaths {
		slug := filepath.Base(rp)
		f, ok := p.readStatus(rp)
		if ok && repoAdvanced(f, p.baseline[rp]) {
			res.Entities += f.Entities
			res.Rels += f.Relationships
			res.Repos = append(res.Repos, splitRepoResult{Slug: slug, Entities: f.Entities, Rels: f.Relationships})
			continue
		}
		reason := "produced no graph"
		if ok && f != nil {
			switch {
			case f.LastErr != "":
				reason = f.LastErr
			case f.Indexing:
				reason = "still indexing when the rebuild acked"
			}
		}
		res.Failed = append(res.Failed, fmt.Sprintf("%s (%s)", slug, reason))
		res.Repos = append(res.Repos, splitRepoResult{Slug: slug, Failed: true, Reason: reason})
	}
	return res, nil
}
