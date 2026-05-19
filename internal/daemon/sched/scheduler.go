// Package sched is the daemon's reactive scheduler (Phase B+). The
// watcher hands off settled-repo notifications to Enqueue; the
// scheduler serialises per-repo indexes, runs them on a small worker
// pool, then schedules:
//
//   - a debounced cross-repo link recompute per group (10s),
//   - a debounced graph-algorithm pass per repo (30s),
//
// both of which are cancelled and rescheduled if new write activity
// arrives during the window. The link recompute and algorithm pass
// run via caller-supplied callbacks so the scheduler stays free of
// extractor + graph package dependencies.
//
// Concurrency cap (post-#644): the scheduler now applies RSS-budget
// admission control on top of the worker pool. Before a queued job
// is dispatched to a worker, the scheduler checks that
//
//	sum(predicted RSS of currently-running jobs) + predicted RSS of
//	the new job <= BudgetMB
//
// If the budget would be exceeded the job stays in the pending queue
// and is retried as soon as a running job completes. This prevents
// the post-#639 concurrent-3-repo peak (672MB) from blowing past the
// 500MB target on the real-fixture benchmark.
package sched

import (
	"context"
	"log"
	"os"
	"sort"
	"sync"
	"time"
)

// IndexFn re-indexes a single repo. The scheduler invokes it on a
// worker goroutine; concurrent calls for distinct repos may run in
// parallel up to the worker-pool size, but each repo path is
// serialised against itself.
type IndexFn func(ctx context.Context, repoPath string) error

// LinksFn re-runs the cross-repo link passes for a group.
type LinksFn func(ctx context.Context, group string) error

// AlgoFn runs the graph-algorithm pass for a repo (community detection,
// PageRank, articulation points). It is scheduled after a successful
// index settles and is cancelled+rescheduled on any further write.
type AlgoFn func(ctx context.Context, repoPath string) error

// GroupsForRepoFn returns the group names a repo participates in.
// Provided by the caller so the scheduler does not import the registry.
type GroupsForRepoFn func(repoPath string) []string

// PredictFn returns a predicted peak RSS contribution (MB) for indexing
// repoPath. Used by admission control. nil disables prediction (every
// job is admitted regardless of budget).
type PredictFn func(repoPath string) int64

// Config wires the scheduler. All function fields are required; nil
// causes Enqueue to short-circuit with a logged warning.
type Config struct {
	Workers       int           // worker pool size; defaults to 2
	LinkDebounce  time.Duration // group settling window; defaults to 10s
	AlgoDebounce  time.Duration // per-repo algo delay; defaults to 30s
	Index         IndexFn
	Links         LinksFn
	Algorithms    AlgoFn
	GroupsForRepo GroupsForRepoFn
	Logger        *log.Logger

	// BudgetMB caps the total predicted RSS of concurrently running
	// index jobs (megabytes). 0 disables admission control entirely
	// (legacy behaviour). Default for production wiring: 500.
	BudgetMB int64

	// Predict returns a per-repo RSS prediction. If nil, every job is
	// assumed to cost 1MB (admission control still serialises but is
	// effectively disabled unless many workers are configured).
	Predict PredictFn

	// History, when non-nil, overrides Predict for repos that have a
	// recorded peak. The scheduler also writes each completed job's
	// observed RSS into History.
	History *RSSHistory
}

// Scheduler is constructed once per daemon. It owns:
//   - a bounded job channel (per-repo dedup happens before enqueue),
//   - a worker pool,
//   - per-group link debounce timers,
//   - per-repo algorithm debounce timers,
//   - an RSS-budget ledger that gates dispatch.
type Scheduler struct {
	cfg    Config
	logger *log.Logger
	enq    chan string   // public enqueue input → dedup → pending queue
	jobs   chan jobToken // admitted jobs handed to workers
	wake   chan struct{} // poked when a worker frees budget
	stop   chan struct{}
	wg     sync.WaitGroup

	mu           sync.Mutex
	inflight     map[string]int64 // repo → predicted MB charged against the ledger
	pendingIndex map[string]bool  // repos already enqueued but not yet running
	pendingQ     []string         // ordered admission queue
	queueLen     int              // pending + admitted-but-not-yet-running
	usedMB       int64            // sum of inflight MB
	linkTimers   map[string]*time.Timer
	linkPending  map[string]bool
	algoTimers   map[string]*time.Timer
	algoPending  map[string]bool
	algoCancel   map[string]context.CancelFunc
	indexedRepos map[string]repoStats
	recentLog    []LogEntry
}

// jobToken couples a repo path with the predicted MB that admission
// control reserved for it. The worker decrements usedMB by this exact
// amount on completion, so partial-credit history updates don't drift.
type jobToken struct {
	repoPath    string
	predictedMB int64
}

// repoStats records what we know about each successful index pass.
type repoStats struct {
	LastIndex   time.Time
	LastAlgo    time.Time
	IndexCount  int64
	AlgoCount   int64
	LastErr     string
	LastPeakMB  int64 // observed peak (history) — 0 if predictor-only
	PredictedMB int64 // last predicted MB charged for this repo
}

// LogEntry is a single structured event captured for /status. Kept in
// memory only; the daemon's regular log file remains authoritative.
type LogEntry struct {
	Time time.Time `json:"time"`
	Kind string    `json:"kind"`
	Repo string    `json:"repo,omitempty"`
	Msg  string    `json:"msg"`
}

const maxRecentLog = 32

// New constructs a scheduler. Start must be called before Enqueue.
func New(cfg Config) *Scheduler {
	if cfg.Workers <= 0 {
		cfg.Workers = 2
	}
	if cfg.LinkDebounce <= 0 {
		cfg.LinkDebounce = 10 * time.Second
	}
	if cfg.AlgoDebounce <= 0 {
		cfg.AlgoDebounce = 30 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = log.New(os.Stderr, "sched: ", log.LstdFlags)
	}
	return &Scheduler{
		cfg:          cfg,
		logger:       cfg.Logger,
		enq:          make(chan string, 64),
		jobs:         make(chan jobToken, cfg.Workers),
		wake:         make(chan struct{}, 1),
		stop:         make(chan struct{}),
		inflight:     map[string]int64{},
		pendingIndex: map[string]bool{},
		linkTimers:   map[string]*time.Timer{},
		linkPending:  map[string]bool{},
		algoTimers:   map[string]*time.Timer{},
		algoPending:  map[string]bool{},
		algoCancel:   map[string]context.CancelFunc{},
		indexedRepos: map[string]repoStats{},
	}
}

// Start spins up the dedup goroutine + admission loop + worker pool.
// Stop reverses it.
func (s *Scheduler) Start() {
	s.wg.Add(1)
	go s.dedupLoop()
	s.wg.Add(1)
	go s.admitLoop()
	for i := 0; i < s.cfg.Workers; i++ {
		s.wg.Add(1)
		go s.workerLoop()
	}
}

// Stop closes the channels and waits for in-flight work to drain.
func (s *Scheduler) Stop() {
	close(s.stop)
	s.wg.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.linkTimers {
		t.Stop()
	}
	for _, t := range s.algoTimers {
		t.Stop()
	}
	for _, c := range s.algoCancel {
		c()
	}
}

// Enqueue requests a (debounced+deduped) reindex of repoPath. Safe to
// call from arbitrary goroutines.
func (s *Scheduler) Enqueue(repoPath string) {
	select {
	case s.enq <- repoPath:
	case <-s.stop:
	}
}

// dedupLoop forwards from enq into the pending admission queue,
// suppressing duplicates for repos already pending or running. This is
// also where we cancel any scheduled algorithm pass — any new write
// activity in the repo invalidates the pending algo schedule.
func (s *Scheduler) dedupLoop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.stop:
			return
		case p := <-s.enq:
			s.mu.Lock()
			s.cancelAlgoLocked(p)
			if _, running := s.inflight[p]; running {
				s.mu.Unlock()
				continue
			}
			if s.pendingIndex[p] {
				s.mu.Unlock()
				continue
			}
			s.pendingIndex[p] = true
			s.pendingQ = append(s.pendingQ, p)
			s.queueLen++
			s.mu.Unlock()
			s.poke()
		}
	}
}

// poke nudges the admission loop without blocking — the wake channel
// has capacity 1, so multiple poke()s coalesce into one wake-up.
func (s *Scheduler) poke() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// admitLoop dispatches queued jobs to workers, gated by the RSS
// budget. It wakes on (a) new enqueue, (b) job completion, (c) a 1s
// safety tick (paranoid retry in case a poke ever gets lost).
func (s *Scheduler) admitLoop() {
	defer s.wg.Done()
	tick := time.NewTicker(1 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-s.wake:
		case <-tick.C:
		}
		s.tryAdmit()
	}
}

// tryAdmit walks the pending queue head-first and dispatches every job
// whose predicted MB fits the remaining budget. Jobs that don't fit
// stay in place; head-of-line blocking is intentional so the very
// largest repo cannot starve forever behind a stream of small ones.
//
// Edge case: if a single job's prediction exceeds the entire budget,
// we admit it anyway as long as nothing else is running — otherwise it
// would never run. The log records this as `admit_oversize`.
func (s *Scheduler) tryAdmit() {
	s.mu.Lock()
	for len(s.pendingQ) > 0 {
		repo := s.pendingQ[0]
		predicted := s.predictedFor(repo)
		// Admission rule.
		if s.cfg.BudgetMB > 0 {
			if s.usedMB+predicted > s.cfg.BudgetMB {
				// Allow a single oversize job through ONLY when the
				// ledger is empty — otherwise nothing would ever
				// release the budget to make room.
				if len(s.inflight) == 0 && predicted > s.cfg.BudgetMB {
					s.logEventLocked("admit_oversize", repo, "predicted MB exceeds budget; running solo")
				} else {
					s.logEventLocked("admit_defer", repo,
						"predicted="+formatMB(predicted)+" used="+formatMB(s.usedMB)+
							" budget="+formatMB(s.cfg.BudgetMB))
					s.mu.Unlock()
					return
				}
			}
		}
		// Pop and dispatch.
		s.pendingQ = s.pendingQ[1:]
		s.inflight[repo] = predicted
		s.usedMB += predicted
		s.logEventLocked("admit_ok", repo,
			"predicted=" + formatMB(predicted) + " used=" + formatMB(s.usedMB))
		tok := jobToken{repoPath: repo, predictedMB: predicted}
		s.mu.Unlock()
		// Block on jobs channel — workers are sized to drain this
		// without deadlock because admission already ensures we are
		// within the worker pool.
		select {
		case s.jobs <- tok:
		case <-s.stop:
			return
		}
		s.mu.Lock()
	}
	s.mu.Unlock()
}

// predictedFor returns the predicted MB for a repo, preferring history
// over the cheap source-size predictor. Always returns at least 1.
func (s *Scheduler) predictedFor(repoPath string) int64 {
	if s.cfg.History != nil {
		if mb := s.cfg.History.Predict(repoPath); mb > 0 {
			return mb
		}
	}
	if s.cfg.Predict != nil {
		if mb := s.cfg.Predict(repoPath); mb > 0 {
			return mb
		}
	}
	return 1
}

// workerLoop pulls admitted jobs and runs them under a per-repo
// serialisation lock. Concurrency is bounded both by the worker pool
// AND by RSS-budget admission.
func (s *Scheduler) workerLoop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.stop:
			return
		case tok, ok := <-s.jobs:
			if !ok {
				return
			}
			s.runIndex(tok)
		}
	}
}

// runIndex executes IndexFn, then releases the reserved budget,
// records observed RSS into history, and schedules downstream link +
// algo passes.
func (s *Scheduler) runIndex(tok jobToken) {
	repoPath := tok.repoPath

	s.mu.Lock()
	s.pendingIndex[repoPath] = false
	s.queueLen--
	s.mu.Unlock()

	t0 := time.Now()
	s.logEvent("index_start", repoPath, "predicted="+formatMB(tok.predictedMB))

	// Spawn RSS sampler so we capture the peak the daemon hit during
	// this job. Records into history on completion.
	sampleStop := make(chan struct{})
	var sampleWG sync.WaitGroup
	var observedPeakMB int64
	sampleWG.Add(1)
	go func() {
		defer sampleWG.Done()
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		baseline := currentProcessRSSMB()
		for {
			select {
			case <-sampleStop:
				return
			case <-t.C:
				now := currentProcessRSSMB()
				delta := now - baseline
				if delta > observedPeakMB {
					observedPeakMB = delta
				}
			}
		}
	}()

	var err error
	if s.cfg.Index != nil {
		err = s.cfg.Index(context.Background(), repoPath)
	}

	close(sampleStop)
	sampleWG.Wait()

	s.mu.Lock()
	stats := s.indexedRepos[repoPath]
	stats.LastIndex = time.Now()
	stats.IndexCount++
	stats.PredictedMB = tok.predictedMB
	if observedPeakMB > 0 {
		stats.LastPeakMB = observedPeakMB
	}
	if err != nil {
		stats.LastErr = err.Error()
	} else {
		stats.LastErr = ""
	}
	s.indexedRepos[repoPath] = stats
	delete(s.inflight, repoPath)
	s.usedMB -= tok.predictedMB
	if s.usedMB < 0 {
		s.usedMB = 0
	}
	s.mu.Unlock()

	// History persistence happens outside the lock (its own mutex +
	// file IO). Only record when the job succeeded; failed runs may
	// have aborted before peak allocation.
	if err == nil && observedPeakMB > 0 && s.cfg.History != nil {
		s.cfg.History.Record(repoPath, observedPeakMB)
	}

	// Wake admission — capacity has freed.
	s.poke()

	if err != nil {
		s.logEvent("index_err", repoPath, err.Error())
		s.logger.Printf("sched: index %s failed: %v (took %s)", repoPath, err, time.Since(t0))
		return
	}
	s.logEvent("index_ok", repoPath,
		time.Since(t0).Truncate(time.Millisecond).String()+" peak="+formatMB(observedPeakMB))

	// Schedule downstream passes.
	s.scheduleAlgo(repoPath)
	if s.cfg.GroupsForRepo != nil {
		for _, g := range s.cfg.GroupsForRepo(repoPath) {
			s.scheduleLinks(g)
		}
	}
}

// scheduleLinks (re)arms the per-group link debounce timer. The 10s
// window is meant to coalesce bursts where multiple repos in a group
// re-index back-to-back.
func (s *Scheduler) scheduleLinks(group string) {
	if s.cfg.Links == nil {
		return
	}
	s.mu.Lock()
	if t, ok := s.linkTimers[group]; ok {
		t.Stop()
	}
	s.linkPending[group] = true
	s.linkTimers[group] = time.AfterFunc(s.cfg.LinkDebounce, func() {
		s.mu.Lock()
		s.linkPending[group] = false
		delete(s.linkTimers, group)
		s.mu.Unlock()
		s.runLinks(group)
	})
	s.mu.Unlock()
}

func (s *Scheduler) runLinks(group string) {
	s.logEvent("links_start", "", group)
	t0 := time.Now()
	err := s.cfg.Links(context.Background(), group)
	if err != nil {
		s.logEvent("links_err", "", group+": "+err.Error())
		s.logger.Printf("sched: links %s failed: %v", group, err)
		return
	}
	s.logEvent("links_ok", "", group+" "+time.Since(t0).Truncate(time.Millisecond).String())
}

// scheduleAlgo (re)arms the per-repo algorithm pass timer. Any pending
// pass is cancelled first; a new pass starts the 30s window over.
func (s *Scheduler) scheduleAlgo(repoPath string) {
	if s.cfg.Algorithms == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelAlgoLocked(repoPath)
	s.algoPending[repoPath] = true
	s.algoTimers[repoPath] = time.AfterFunc(s.cfg.AlgoDebounce, func() {
		s.mu.Lock()
		s.algoPending[repoPath] = false
		delete(s.algoTimers, repoPath)
		ctx, cancel := context.WithCancel(context.Background())
		s.algoCancel[repoPath] = cancel
		s.mu.Unlock()

		s.runAlgo(ctx, repoPath)

		s.mu.Lock()
		delete(s.algoCancel, repoPath)
		s.mu.Unlock()
	})
}

// cancelAlgoLocked stops any pending timer or cancels an in-flight
// algorithm pass for the given repo. MUST be called with s.mu held.
func (s *Scheduler) cancelAlgoLocked(repoPath string) {
	if t, ok := s.algoTimers[repoPath]; ok {
		t.Stop()
		delete(s.algoTimers, repoPath)
		s.algoPending[repoPath] = false
	}
	if c, ok := s.algoCancel[repoPath]; ok {
		c()
		delete(s.algoCancel, repoPath)
	}
}

func (s *Scheduler) runAlgo(ctx context.Context, repoPath string) {
	s.logEvent("algo_start", repoPath, "")
	t0 := time.Now()
	err := s.cfg.Algorithms(ctx, repoPath)
	if err != nil {
		if ctx.Err() != nil {
			s.logEvent("algo_cancelled", repoPath, "")
			return
		}
		s.logEvent("algo_err", repoPath, err.Error())
		s.logger.Printf("sched: algo %s failed: %v", repoPath, err)
		return
	}
	s.mu.Lock()
	stats := s.indexedRepos[repoPath]
	stats.LastAlgo = time.Now()
	stats.AlgoCount++
	s.indexedRepos[repoPath] = stats
	s.mu.Unlock()
	s.logEvent("algo_ok", repoPath, time.Since(t0).Truncate(time.Millisecond).String())
}

// Snapshot reports current scheduler state for the Status RPC.
type Snapshot struct {
	QueueLen     int
	InFlight     []InFlightJob
	PendingAlgo  []string
	PendingLinks []string
	IndexedRepos []RepoSnapshot
	RecentLog    []LogEntry

	// Budget telemetry (added with admission control).
	BudgetMB   int64
	UsedMB     int64
	BlockedJobs []string
}

// InFlightJob is one currently-running index, with its reserved MB.
type InFlightJob struct {
	Path        string `json:"path"`
	PredictedMB int64  `json:"predicted_mb"`
}

// RepoSnapshot is one repo's slice of Snapshot.
type RepoSnapshot struct {
	Path        string    `json:"path"`
	LastIndex   time.Time `json:"last_index"`
	LastAlgo    time.Time `json:"last_algo"`
	IndexCount  int64     `json:"index_count"`
	AlgoCount   int64     `json:"algo_count"`
	LastErr     string    `json:"last_err,omitempty"`
	LastPeakMB  int64     `json:"last_peak_mb,omitempty"`
	PredictedMB int64     `json:"predicted_mb,omitempty"`
}

// Snapshot returns a defensive copy of the scheduler's user-visible
// state. Safe to call from the RPC handler.
func (s *Scheduler) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Snapshot{
		QueueLen: s.queueLen,
		BudgetMB: s.cfg.BudgetMB,
		UsedMB:   s.usedMB,
	}
	for p, mb := range s.inflight {
		out.InFlight = append(out.InFlight, InFlightJob{Path: p, PredictedMB: mb})
	}
	// Deterministic ordering — helps both /status output and tests.
	sort.Slice(out.InFlight, func(i, j int) bool { return out.InFlight[i].Path < out.InFlight[j].Path })
	for p := range s.algoPending {
		if s.algoPending[p] {
			out.PendingAlgo = append(out.PendingAlgo, p)
		}
	}
	sort.Strings(out.PendingAlgo)
	for g := range s.linkPending {
		if s.linkPending[g] {
			out.PendingLinks = append(out.PendingLinks, g)
		}
	}
	sort.Strings(out.PendingLinks)
	out.BlockedJobs = append(out.BlockedJobs, s.pendingQ...)
	for p, st := range s.indexedRepos {
		out.IndexedRepos = append(out.IndexedRepos, RepoSnapshot{
			Path: p, LastIndex: st.LastIndex, LastAlgo: st.LastAlgo,
			IndexCount: st.IndexCount, AlgoCount: st.AlgoCount,
			LastErr: st.LastErr, LastPeakMB: st.LastPeakMB, PredictedMB: st.PredictedMB,
		})
	}
	if n := len(s.recentLog); n > 0 {
		out.RecentLog = append(out.RecentLog, s.recentLog...)
	}
	return out
}

// MarkIndexed lets the daemon record a non-watcher-driven index (e.g.
// an explicit `archigraph index` RPC) so Status reflects reality.
func (s *Scheduler) MarkIndexed(repoPath string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stats := s.indexedRepos[repoPath]
	stats.LastIndex = time.Now()
	stats.IndexCount++
	if err != nil {
		stats.LastErr = err.Error()
	} else {
		stats.LastErr = ""
	}
	s.indexedRepos[repoPath] = stats
}

// logEvent appends to the in-memory recent-log buffer (capped at
// maxRecentLog). The daemon log file remains the authoritative store.
func (s *Scheduler) logEvent(kind, repo, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logEventLocked(kind, repo, msg)
}

// logEventLocked is the s.mu-held form used inside hot paths that
// already hold the scheduler lock.
func (s *Scheduler) logEventLocked(kind, repo, msg string) {
	s.recentLog = append(s.recentLog, LogEntry{Time: time.Now(), Kind: kind, Repo: repo, Msg: msg})
	if len(s.recentLog) > maxRecentLog {
		s.recentLog = s.recentLog[len(s.recentLog)-maxRecentLog:]
	}
}

// formatMB is a tiny helper so the recent-log strings stay short.
func formatMB(mb int64) string {
	// Avoid pulling fmt into hot paths.
	if mb <= 0 {
		return "0MB"
	}
	return itoa(mb) + "MB"
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
