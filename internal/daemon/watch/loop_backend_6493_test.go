package watch

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ---------------------------------------------------------------------------
// #6493 — the loop goroutine must never make a blocking call into the backend.
//
// fsnotify's Windows backend is a single-goroutine actor: Add and Remove are
// requests posted to the very goroutine that delivers events
// (`w.input <- in; return <-in.reply`, backend_windows.go:141-146, :162-167),
// and that same goroutine is the only sender on Events, via a sendEvent that
// blocks until grafel receives (:69-91).
//
// Watcher.loop is the ONLY consumer of Events. handleEvent used to call
// subscribeDirRecursive -> fsAdd synchronously, ON the loop goroutine. So the
// instant the backend had a second event to deliver while loop was inside Add,
// the two wedged against each other permanently: the backend cannot service
// the Add until someone receives the event, and the only receiver is inside
// the Add. Every later caller — RemoveRepo -> unwatchAll — then blocks forever
// posting into a dead actor, which is where the symptom always surfaced.
//
// kqueue and inotify do Add/Remove as a direct syscall on the calling
// goroutine, so the cycle is unreachable off Windows with a real backend. The
// fake below models the Windows actor semantics exactly, which makes the
// property — "loop keeps draining while a subscription is in flight" — a
// deterministic assertion on any host.
// ---------------------------------------------------------------------------

// errActorClosed stands in for fsnotify.ErrClosed: what a real backend returns
// from Add/Remove once it has been closed. The fake returns it rather than
// parking forever so a REGRESSION of this bug is a clean FAIL instead of a
// wedged test binary.
var errActorClosed = errors.New("windowsActor: closed")

type actorReq struct {
	op    string
	path  string
	reply chan error
}

// windowsActorBackend is a stand-in for fsnotify's Windows backend, faithful in
// the one dimension this bug lives in: ONE goroutine both delivers events
// (blocking send) and services Add/Remove (channel round-trip). Anything that
// takes the receiver away from the Events channel therefore stops Add and
// Remove from ever completing.
type windowsActorBackend struct {
	ev     chan fsnotify.Event
	errs   chan error
	reqs   chan actorReq
	outbox chan fsnotify.Event // events the test hands the actor to deliver

	delivered chan fsnotify.Event // one send per event whose blocking send RETURNED
	stop      chan struct{}
	stopOnce  sync.Once
	done      chan struct{}

	mu    sync.Mutex
	calls []string
}

func (b *windowsActorBackend) record(op, path string) {
	b.mu.Lock()
	b.calls = append(b.calls, op+" "+path)
	b.mu.Unlock()
}

func (b *windowsActorBackend) callsSnapshot() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.calls...)
}

// request is the Add/Remove seam: post to the actor, wait for its reply. This
// is the shape that makes the backend un-callable from a goroutine the actor
// needs.
func (b *windowsActorBackend) request(op, path string) error {
	r := actorReq{op: op, path: path, reply: make(chan error, 1)}
	select {
	case b.reqs <- r:
	case <-b.stop:
		return errActorClosed
	}
	select {
	case err := <-r.reply:
		return err
	case <-b.stop:
		return errActorClosed
	}
}

// run is the actor. Event delivery has strict priority over servicing
// requests, which is what makes the pre-fix deadlock deterministic rather than
// a scheduling coin-flip: with an event already queued, the actor is inside
// its blocking send by the time the loop goroutine posts an Add.
func (b *windowsActorBackend) run() {
	defer close(b.done)
	for {
		select {
		case <-b.stop:
			return
		default:
		}
		select {
		case e := <-b.outbox:
			select {
			case b.ev <- e:
			case <-b.stop:
				return
			}
			select {
			case b.delivered <- e:
			case <-b.stop:
				return
			}
			continue
		default:
		}
		select {
		case <-b.stop:
			return
		case e := <-b.outbox:
			select {
			case b.ev <- e:
			case <-b.stop:
				return
			}
			select {
			case b.delivered <- e:
			case <-b.stop:
				return
			}
		case r := <-b.reqs:
			b.record(r.op, r.path)
			r.reply <- nil
		}
	}
}

func (b *windowsActorBackend) shutdown() {
	b.stopOnce.Do(func() { close(b.stop) })
	<-b.done
}

// windowsActorWatcher builds a Watcher whose loop drains the actor's Events and
// whose Add/Remove seams post to that same actor.
//
// The REAL backend is still constructed by NewWatcherConfig and still gets a
// discard sink for its own channels — withholding events from the loop is not
// the same as leaving the backend unread, and #6380 is the bill for conflating
// them (see withheldEventsWatcher).
func windowsActorWatcher(t *testing.T, cfg Config) (*Watcher, *windowsActorBackend) {
	t.Helper()
	b := &windowsActorBackend{
		ev:        make(chan fsnotify.Event),
		errs:      make(chan error),
		reqs:      make(chan actorReq),
		outbox:    make(chan fsnotify.Event, 8),
		delivered: make(chan fsnotify.Event, 8),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	go b.run()

	cfg.testEvents, cfg.testErrors = b.ev, b.errs
	// The automatic sweep would call the same seams on its own schedule and
	// race the sequence this test drives by hand.
	cfg.reconcileInterval = -1
	cfg.disableQuarantine = true
	w := newBudgetedWatcherCfgNoCleanup(t, cfg)

	w.mu.Lock()
	fw := w.fs
	w.fsAdd = func(p string) error { return b.request("Add", p) }
	w.fsRemove = func(p string) error { return b.request("Remove", p) }
	w.mu.Unlock()
	drainedReal := discardBackendChannels(fw.Events, fw.Errors)

	realClose := w.closeBackend
	w.closeBackend = func() error {
		err := realClose()
		select {
		case <-drainedReal:
		case <-time.After(5 * time.Second):
			t.Errorf("the backend discard sink outlived Close by 5s")
		}
		// Retire the actor BEFORE closing the channels it sends on: a real
		// backend closes its Events only from inside its own I/O goroutine, so
		// closing them out from under a live actor is a harness bug, not a
		// property of the code under test.
		b.shutdown()
		close(b.ev)
		close(b.errs)
		return err
	}
	t.Cleanup(func() {
		w.Stop()
		b.shutdown()
	})
	return w, b
}

// TestLoopKeepsDrainingWhileSubscribingANewDirectory is the #6493 regression.
//
// Two events are queued on the actor before either is delivered. The first is
// a new directory, which drives handleEvent into the subscription path; the
// second exists only so the actor is parked in a blocking send while that
// subscription is being made. Pre-fix, the loop goroutine performed the Add
// itself and the two wedged: event 2 was never delivered and the test fails on
// the timeout below. Post-fix, the subscription is handed to a goroutine that
// is not the drainer, so event 2 lands and the Add then completes.
func TestLoopKeepsDrainingWhileSubscribingANewDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	w, b := windowsActorWatcher(t, Config{FDBudget: 1_000_000})

	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("premise broken: AddRepo failed, so no directory is watched and "+
			"handleEvent would never reach the subscription path: %v", err)
	}
	// AddRepo already proves the actor services requests when the loop is free.
	if got := b.callsSnapshot(); len(got) == 0 {
		t.Fatalf("premise broken: AddRepo made no backend call (%v)", got)
	}

	newDir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "b.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}

	// Queued together, before either is delivered: the actor sends the first,
	// then immediately parks in the blocking send of the second.
	b.outbox <- fsnotify.Event{Name: newDir, Op: fsnotify.Create}
	b.outbox <- fsnotify.Event{Name: filepath.Join(root, "z.go"), Op: fsnotify.Create}

	deadline := time.After(10 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case <-b.delivered:
		case <-deadline:
			t.Fatalf("only %d of 2 events reached the loop goroutine within 10s: the "+
				"loop stopped draining the backend's Events while a subscription was "+
				"in flight, which is the #6493 deadlock — the backend cannot answer "+
				"the Add until the loop receives, and the loop cannot receive until "+
				"the Add is answered. Backend calls seen: %v", i, b.callsSnapshot())
		}
	}

	// Non-vacuity: draining is only the half of it. The subscription the event
	// asked for must still actually happen, or a "fix" that simply dropped the
	// work would pass the drain assertion above.
	subscribed := make(chan struct{})
	go func() {
		defer close(subscribed)
		for {
			for _, c := range b.callsSnapshot() {
				if c == "Add "+newDir {
					return
				}
			}
			select {
			case <-time.After(10 * time.Millisecond):
			case <-b.stop:
				return
			}
		}
	}()
	select {
	case <-subscribed:
	case <-time.After(10 * time.Second):
		t.Fatalf("the new directory %s was never Add()ed to the backend within 10s; "+
			"backend calls seen: %v", newDir, b.callsSnapshot())
	}
}

// subscribesPending reports whether any subtree is queued for, or in the middle
// of, subscription. subInFlight is marked in enqueueSubscribe — before the
// hand-off — and cleared when the walk returns, so it covers both.
//
// Ledger assertions need it because #6493 moved subscription charging off the
// event goroutine: "the ledger stopped moving" no longer implies "everything
// that will be charged has been".
func subscribesPending(w *Watcher) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.subInFlight) > 0
}

// awaitSubscribed blocks until dir has been published into dirToRepo by the
// subscribe owner. Since #6493 a directory discovered by handleEvent is
// subscribed on a DIFFERENT goroutine, so a test that drives handleEvent
// directly and then reads the watcher's maps has to wait for that goroutine —
// the subscription is no longer complete when handleEvent returns.
func awaitSubscribed(t *testing.T, w *Watcher, dir string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		w.mu.Lock()
		_, watched := w.dirToRepo[dir]
		_, pending := w.subInFlight[dir]
		w.mu.Unlock()
		if watched && !pending {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the subscribe owner did not publish %s within 10s "+
				"(watched=%v, still in flight=%v)", dir, watched, pending)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// gateAdds wraps the watcher's Add seam so a test can park the subscribe owner
// inside a call and drive the queue deterministically. Returns the channel
// reporting each entered path and the gate that releases them.
func gateAdds(w *Watcher) (entered chan string, gate chan struct{}) {
	entered = make(chan string, 64)
	gate = make(chan struct{})
	w.mu.Lock()
	inner := w.fsAdd
	w.fsAdd = func(p string) error {
		entered <- p
		<-gate
		return inner(p)
	}
	w.mu.Unlock()
	return entered, gate
}

// waitDelivered blocks until n more events have been handed to the loop
// goroutine, i.e. until that many of the actor's blocking sends have returned.
func waitDelivered(t *testing.T, b *windowsActorBackend, n int) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for i := 0; i < n; i++ {
		select {
		case <-b.delivered:
		case <-deadline:
			t.Fatalf("only %d of %d events reached the loop goroutine within 10s — "+
				"the loop stopped draining the backend (#6493)", i, n)
		}
	}
}

// TestFullSubscribeQueueDropsTheSubtreeAndKeepsTheLoopDraining pins the price
// of the bound. The queue MUST be bounded — an unbounded one is just a slower
// leak — so overflow has to cost something, and what it costs is the
// subscription, never the drain. A hand-off that made the send blocking to
// avoid the loss would reinstate #6493 exactly.
func TestFullSubscribeQueueDropsTheSubtreeAndKeepsTheLoopDraining(t *testing.T) {
	old := subscribeQueueCap
	subscribeQueueCap = 1
	t.Cleanup(func() { subscribeQueueCap = old })

	root := t.TempDir()
	w, b := windowsActorWatcher(t, Config{FDBudget: 1_000_000})
	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("premise broken: AddRepo: %v", err)
	}
	dirs := make([]string, 3)
	for i := range dirs {
		dirs[i] = filepath.Join(root, "d"+string(rune('0'+i)))
		if err := os.MkdirAll(dirs[i], 0o755); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	entered, gate := gateAdds(w)
	defer close(gate)

	// First directory: park the owner inside its Add, so the single queue slot
	// is empty and the arithmetic below is exact rather than a race.
	b.outbox <- fsnotify.Event{Name: dirs[0], Op: fsnotify.Create}
	waitDelivered(t, b, 1)
	select {
	case p := <-entered:
		if p != dirs[0] {
			t.Fatalf("premise broken: owner entered Add(%s), want Add(%s)", p, dirs[0])
		}
	case <-time.After(10 * time.Second):
		t.Fatal("premise broken: the subscribe owner never entered Add for the first " +
			"directory, so the queue slot was never the thing under test")
	}

	// The second fills the single slot, the third has nowhere to go. The fourth
	// event is not a directory: it is there to prove the loop kept draining
	// across the overflow rather than blocking on the full queue.
	b.outbox <- fsnotify.Event{Name: dirs[1], Op: fsnotify.Create}
	b.outbox <- fsnotify.Event{Name: dirs[2], Op: fsnotify.Create}
	b.outbox <- fsnotify.Event{Name: filepath.Join(root, "z.go"), Op: fsnotify.Create}
	waitDelivered(t, b, 3)

	if got := atomic.LoadUint64(&w.droppedSubscribes); got != 1 {
		t.Fatalf("droppedSubscribes = %d, want 1: with a 1-slot queue, an owner "+
			"parked in Add and three directories offered, exactly the third has "+
			"nowhere to go", got)
	}
	// Read through the exported surface too. A dropped subtree is permanent —
	// nothing rediscovers it — so a counter only the package can see is a silent
	// failure in production, and this is what stops the export being dropped.
	if _, _, _, _, got := w.ExtendedStats(); got != 1 {
		t.Fatalf("ExtendedStats reported %d dropped subscribes, want 1: the counter "+
			"is not reaching /diagnostics, so the lost subtree is invisible to an "+
			"operator", got)
	}
}

// TestSubscribeQueuedBeforeRemoveRepoDoesNotResurrectTheWatch pins the ordering
// hazard the hand-off creates. A subscription discovered before RemoveRepo now
// runs AFTER it, so it could re-Add a directory RemoveRepo has just unwatched —
// a descriptor grafel would then never release. RemoveRepo clears dirToRepo
// under w.mu before it touches the backend, and subscribeDirRecursive resolves
// the owning repo on entry, so the deferred work becomes a no-op.
func TestSubscribeQueuedBeforeRemoveRepoDoesNotResurrectTheWatch(t *testing.T) {
	root := t.TempDir()
	w, b := windowsActorWatcher(t, Config{FDBudget: 1_000_000})
	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("premise broken: AddRepo: %v", err)
	}
	held := filepath.Join(root, "held")
	deferredDir := filepath.Join(root, "deferred")
	for _, d := range []string{held, deferredDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	entered, gate := gateAdds(w)

	b.outbox <- fsnotify.Event{Name: held, Op: fsnotify.Create}
	waitDelivered(t, b, 1)
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("premise broken: the owner never entered Add for the first directory")
	}
	// Queued behind the parked owner.
	b.outbox <- fsnotify.Event{Name: deferredDir, Op: fsnotify.Create}
	waitDelivered(t, b, 1)

	w.RemoveRepo(root)
	close(gate)

	// The owner is now free to process the queued directory. It must decline.
	timeout := time.After(2 * time.Second)
	for {
		select {
		case p := <-entered:
			if p == deferredDir {
				t.Fatalf("the subscribe owner re-Add()ed %s after RemoveRepo unwatched "+
					"its repo — the descriptor that Add opens would never be released, "+
					"because nothing in the watcher claims the directory any more", p)
			}
		case <-timeout:
			return
		}
	}
}

// TestStopRetiresTheSubscribeOwnerWithQueuedWorkOutstanding pins the shutdown
// half. Work still queued when Stop runs must be abandoned, not completed and
// not waited on: a queue Close had to drain would put an arbitrary amount of
// directory walking in front of every daemon shutdown.
func TestStopRetiresTheSubscribeOwnerWithQueuedWorkOutstanding(t *testing.T) {
	// Shortened so the ONE bounded wait the parked owner costs is small enough
	// for the assertion below to distinguish it from a second wait on the
	// queue. watcherStopTimeout is a var for exactly this.
	oldTimeout := watcherStopTimeout
	watcherStopTimeout = 2 * time.Second
	t.Cleanup(func() { watcherStopTimeout = oldTimeout })

	root := t.TempDir()
	w, b := windowsActorWatcher(t, Config{FDBudget: 1_000_000})
	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("premise broken: AddRepo: %v", err)
	}
	entered, gate := gateAdds(w)
	defer close(gate)

	// Park the owner in one Add so nothing behind it can be serviced, then
	// queue more. Everything after the first is outstanding when Stop runs.
	parked := filepath.Join(root, "parked")
	if err := os.MkdirAll(parked, 0o755); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	w.subCh <- parked
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("premise broken: the owner never entered Add, so nothing was outstanding")
	}
	var queued []string
	for i := 0; i < 5; i++ {
		d := filepath.Join(root, "q"+string(rune('0'+i)))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("fixture: %v", err)
		}
		queued = append(queued, d)
		w.subCh <- d
	}
	before := len(b.callsSnapshot())

	done := make(chan struct{})
	start := time.Now()
	go func() { defer close(done); w.Stop() }()
	select {
	case <-done:
	case <-time.After(3 * watcherStopTimeout):
		t.Fatalf("Stop did not return with %d subscriptions still queued", len(queued))
	}
	// One bounded wait for the parked owner is the design; waiting on the
	// QUEUE as well would be a second one, and that is what this catches.
	if elapsed := time.Since(start); elapsed > 2*watcherStopTimeout {
		t.Fatalf("Stop took %s with %d subscriptions queued: it waited on the queue "+
			"rather than abandoning it", elapsed, len(queued))
	}
	for _, c := range b.callsSnapshot()[before:] {
		for _, d := range queued {
			if c == "Add "+d {
				t.Fatalf("Stop completed a queued subscription (%s) instead of "+
					"abandoning it", c)
			}
		}
	}
}

// steppedAdds is gateAdds with one release token per call instead of a single
// broadcast close, so a test can let one walk finish and then catch the NEXT
// one while it is still inside the backend.
func steppedAdds(w *Watcher) (entered chan string, release chan struct{}) {
	entered = make(chan string, 64)
	release = make(chan struct{})
	w.mu.Lock()
	inner := w.fsAdd
	w.fsAdd = func(p string) error {
		entered <- p
		<-release
		return inner(p)
	}
	w.mu.Unlock()
	return entered, release
}

// TestDuplicateEnqueueKeepsTheDirectoryMarkedUntilEveryWalkIsDone pins the
// refcount, and a plain set is what it kills.
//
// A directory can be handed to the owner twice — a delete-then-recreate during
// a checkout reports Create twice, which is the pattern
// fdbudget_6293_test.go already models — and the two walks then run one after
// the other. With subInFlight keyed by root and cleared unconditionally, the
// FIRST walk's exit clears the mark while the SECOND has not run yet.
//
// Two things break at that moment, and neither is cosmetic. chargeEventOpen
// stops recording fdEventCharged markers for that subtree, which reopens the
// exact Create-then-listing double charge settleListing exists to close; and
// subscribesPending — which drainLedger now believes — reports "settled" with a
// subscription still outstanding.
func TestDuplicateEnqueueKeepsTheDirectoryMarkedUntilEveryWalkIsDone(t *testing.T) {
	root := t.TempDir()
	w, _ := windowsActorWatcher(t, Config{FDBudget: 1_000_000})
	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("premise broken: AddRepo: %v", err)
	}
	dir := filepath.Join(root, "d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	entered, release := steppedAdds(w)

	// Two reports of the same directory, exactly as two Creates for it would
	// produce.
	w.enqueueSubscribe(dir)
	w.enqueueSubscribe(dir)

	// Walk #1 in, then out.
	select {
	case p := <-entered:
		if p != dir {
			t.Fatalf("premise broken: first walk entered Add(%s), want Add(%s)", p, dir)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("premise broken: the owner never started the first walk")
	}
	release <- struct{}{}

	// Walk #2 in. The directory is still being subscribed, so it must still be
	// marked.
	select {
	case p := <-entered:
		if p != dir {
			t.Fatalf("premise broken: second walk entered Add(%s), want Add(%s)", p, dir)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("premise broken: the second queued subscription never ran, so the " +
			"duplicate this test is about never happened")
	}
	if !subscribesPending(w) {
		t.Error("subscribesPending() is false while a subscription walk is still " +
			"running: drainLedger reads this to decide the ledger has settled, and " +
			"would take its reading with charges still to come")
	}
	w.mu.Lock()
	_, marked := w.subInFlight[dir]
	w.mu.Unlock()
	if !marked {
		t.Fatalf("subInFlight lost %s while a subscription walk over it was still "+
			"running: the first walk's exit cleared a mark the second still needs, so "+
			"chargeEventOpen records no fdEventCharged marker for this subtree and the "+
			"Create-then-listing double charge is open again", dir)
	}
	release <- struct{}{}
}

// releaseForever keeps a steppedAdds gate open for the rest of the test, so a
// walk left mid-flight by an assertion cannot hold Stop to its timeout.
func releaseForever(t *testing.T, release chan struct{}) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case release <- struct{}{}:
			}
		}
	}()
	t.Cleanup(func() { close(done) })
}

// TestOnlyEntriesInsideTheWalkedSubtreeCountAsAlreadyCharged pins the ancestry
// walk in underSubscribeInFlightLocked. Answering "is ANY subscription in
// flight" instead of "is one in flight over THIS path" marks entries the walk
// will never list, and settleListing then deducts a charge from a listing that
// was entitled to make it.
//
// The positive half is not decoration: without it a predicate that always
// returned false would pass the negative half and pin nothing.
func TestOnlyEntriesInsideTheWalkedSubtreeCountAsAlreadyCharged(t *testing.T) {
	root := t.TempDir()
	w, _ := windowsActorWatcher(t, Config{FDBudget: 1_000_000})
	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("premise broken: AddRepo: %v", err)
	}
	inside := filepath.Join(root, "walked")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	entered, release := steppedAdds(w)
	releaseForever(t, release)

	// Park a walk over `inside`, and only over `inside`.
	w.enqueueSubscribe(inside)
	select {
	case p := <-entered:
		if p != inside {
			t.Fatalf("premise broken: parked in Add(%s), want Add(%s)", p, inside)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("premise broken: no walk started, so nothing was in flight")
	}

	// One entry inside the walked subtree, one outside it but under the same
	// repo. Both are charged; only the first is the walk's to reclaim.
	within := filepath.Join(inside, "a.go")
	beyond := filepath.Join(root, "b.go")
	for _, p := range []string{within, beyond} {
		if err := os.WriteFile(p, []byte("package p\n"), 0o644); err != nil {
			t.Fatalf("fixture: %v", err)
		}
		w.handleEvent(fsnotify.Event{Name: p, Op: fsnotify.Create})
	}

	w.mu.Lock()
	_, gotWithin := w.fdEventCharged[within]
	_, gotBeyond := w.fdEventCharged[beyond]
	w.mu.Unlock()
	if !gotWithin {
		t.Fatalf("premise broken: %s is inside the subtree being walked and was not "+
			"recorded as already charged, so this test would pass vacuously", within)
	}
	if gotBeyond {
		t.Fatalf("%s was recorded as already charged while the walk in flight covers "+
			"only %s: no listing of %s will ever reclaim that marker, and the listing "+
			"that does charge this entry will have its charge deducted for a "+
			"descriptor nobody else paid for", beyond, inside, inside)
	}
}

// TestASubdirectoryCreatedDuringAWalkIsNotCountedAsAnEntryOfItsParent pins the
// exclusion of directories from fdEventCharged.
//
// dirEntries is the count of CHARGEABLE, NON-DIRECTORY entries a directory
// holds — the quantity reconcileDir compares against a fresh listing to decide
// whether fsnotify silently stopped watching part of it (#6304). Recording a
// subdirectory as an already-charged entry inflates that count, and an inflated
// count makes `want <= have` true forever: the directory is then never
// repaired, however short it really is.
func TestASubdirectoryCreatedDuringAWalkIsNotCountedAsAnEntryOfItsParent(t *testing.T) {
	root := t.TempDir()
	w, _ := windowsActorWatcher(t, Config{FDBudget: 1_000_000})
	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("premise broken: AddRepo: %v", err)
	}
	parent := filepath.Join(root, "parent")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(parent, "only.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	entered, release := steppedAdds(w)

	w.enqueueSubscribe(parent)
	select {
	case p := <-entered:
		if p != parent {
			t.Fatalf("premise broken: parked in Add(%s), want Add(%s)", p, parent)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("premise broken: no walk started")
	}

	// A subdirectory appears while the parent's walk is parked in Add, and its
	// Create is processed before the listing runs.
	sub := filepath.Join(parent, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	w.handleEvent(fsnotify.Event{Name: sub, Op: fsnotify.Create})

	// Let the parent's Add return; the listing, settleListing and the dirEntries
	// publication all follow before the walk descends into sub and parks again.
	release <- struct{}{}
	select {
	case p := <-entered:
		if p != sub {
			t.Fatalf("premise broken: expected the walk to descend into %s, got %s", sub, p)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("premise broken: the walk never descended, so dirEntries for the " +
			"parent may not have been published yet")
	}
	releaseForever(t, release)

	w.mu.Lock()
	have := w.dirEntries[parent]
	w.mu.Unlock()
	if have != 1 {
		t.Fatalf("dirEntries[%s] = %d, want 1: the directory holds exactly one "+
			"chargeable entry (only.go); counting the subdirectory created during "+
			"the walk as one makes reconcileDir believe the directory is fuller "+
			"than it is, and a short directory is then never repaired", parent, have)
	}
}
