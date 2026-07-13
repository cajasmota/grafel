// Package repolock serializes indexing of a single repo across the two
// independent code paths that both write the same
// <store>/<repo>/refs/<ref>/graph.fb:
//
//   - the FOREGROUND group-rebuild path in cmd/grafel (daemonRebuildFuncCore →
//     Index), which calls the extractor directly and never touches the
//     scheduler, and
//   - the BACKGROUND scheduler in internal/daemon/sched, whose per-repo
//     in-flight guard only protects the scheduler against itself.
//
// Before this package existed, a wizard-triggered foreground rebuild and a
// scheduler-enqueued reindex of the SAME repo could run concurrently, both
// rewriting graph.fb under each other. The rebuild's post-index cross-repo link
// pass then never converged against a file being rewritten beneath it, so the
// rebuild call never returned and its request was never acked — runaway
// re-indexing that only stopped on a manual daemon kill.
//
// The claim is keyed on the repo path (filepath.Clean-normalised). Both callers
// derive that path from the SAME registry group config, so the strings match.
//
// Priority: the foreground rebuild wins. While a foreground claim is intended or
// held for a repo, the scheduler's TryClaimBackground fails (it yields and
// retries later) instead of racing. A foreground claim still BLOCKS on a
// background index that is already running for the repo — it must not corrupt a
// write already in progress.
//
// Boundary: this is an in-process (in-memory) lock. It serialises the daemon's
// own foreground-rebuild and scheduler paths only. A directly-invoked
// `grafel index` CLI runs in a SEPARATE OS process and is NOT covered by this
// registry — that pre-existing hazard (two processes writing one store) is out
// of scope here and would need a filesystem lock to close.
package repolock

import (
	"path/filepath"
	"sync"
)

// Registry tracks per-repo index claims. The zero value is not usable; call
// New. A process-wide DefaultRegistry is provided for the daemon, since the
// foreground rebuild (cmd/grafel) and the scheduler (internal/daemon/sched) do
// not share any object reference through which a non-global registry could be
// threaded.
type Registry struct {
	mu   sync.Mutex
	cond *sync.Cond
	st   map[string]*keyState
}

type keyState struct {
	held   bool // an index (foreground OR background) is currently running
	fgWant int  // foreground claimants that intend to hold or are holding
}

// New returns an empty Registry.
func New() *Registry {
	r := &Registry{st: map[string]*keyState{}}
	r.cond = sync.NewCond(&r.mu)
	return r
}

// DefaultRegistry is the process-wide registry used by the live daemon.
var DefaultRegistry = New()

func (r *Registry) stateOf(key string) *keyState {
	ks := r.st[key]
	if ks == nil {
		ks = &keyState{}
		r.st[key] = ks
	}
	return ks
}

// gcLocked drops a keyState once it carries no state, so the map does not grow
// unboundedly across the process lifetime. Caller must hold r.mu.
func (r *Registry) gcLocked(key string, ks *keyState) {
	if !ks.held && ks.fgWant == 0 {
		delete(r.st, key)
	}
}

// ClaimForeground acquires the key for a foreground (priority) index. It first
// records foreground intent — so any concurrent TryClaimBackground immediately
// yields — then blocks until no index is running for the key, marks it held,
// and returns an idempotent release.
//
// The returned release MUST be called from the goroutine that actually runs the
// index (not from a timeout/orphan supervisor), so the claim is released on the
// index's real completion. A rebuild whose outer per-repo timeout fires but
// whose index goroutine keeps running still holds the claim until that
// goroutine finishes — which is correct: the file is still being written. The
// release is idempotent, so wiring it as a defer is safe even alongside panic
// recovery.
func (r *Registry) ClaimForeground(key string) (release func()) {
	key = filepath.Clean(key)
	r.mu.Lock()
	ks := r.stateOf(key)
	ks.fgWant++
	for ks.held {
		r.cond.Wait()
	}
	ks.held = true
	r.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			ks.held = false
			ks.fgWant--
			r.gcLocked(key, ks)
			r.cond.Broadcast()
			r.mu.Unlock()
		})
	}
}

// TryClaimBackground attempts to acquire the key for a background scheduler
// index. It FAILS (ok=false) without blocking when either an index is already
// running for the key OR a foreground claim is intended/held for it (yield to
// the rebuild). On success it marks the key held and returns an idempotent
// release.
func (r *Registry) TryClaimBackground(key string) (release func(), ok bool) {
	key = filepath.Clean(key)
	r.mu.Lock()
	ks := r.stateOf(key)
	if ks.held || ks.fgWant > 0 {
		r.gcLocked(key, ks)
		r.mu.Unlock()
		return nil, false
	}
	ks.held = true
	r.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			ks.held = false
			r.gcLocked(key, ks)
			r.cond.Broadcast()
			r.mu.Unlock()
		})
	}, true
}
