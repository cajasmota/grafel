package watch

import (
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Watch reconciliation (#6304).
//
// THE DEFECT. fsnotify v1.10.1's kqueue backend mimics inotify by taking a
// descriptor on every ENTRY of a directory grafel Add()s, and by re-listing that
// directory on each NOTE_WRITE to pick up entries that appeared since
// (backend_kqueue.go dirChange). That re-listing gives up silently. On an
// f.Info() error, and on an EACCES/EPERM/ENOENT from sendCreateIfNew, dirChange
// `return nil`s in the middle of the listing; readEvents then discards even the
// errors it DOES return. The entries after the failure point are therefore never
// watched and never reported: no Create, no Errors entry, no log line.
//
// One unreadable file — mode 0000, a dangling symlink, an entry unlinked between
// the readdir and the lstat — is enough, and because it sorts wherever it sorts,
// it takes the whole remainder of the directory with it. A macOS CI run observed
// a deficit of exactly two directories' file complements.
//
// WHY IT IS PERMANENT. kqueue does not markSeen the abandoned entries, so a
// LATER dirChange for the same directory would pick them up — but only if a
// later NOTE_WRITE arrives. The last write of a burst leaves no successor, and
// nothing else in fsnotify or in grafel ever re-lists a directory it has already
// subscribed. The entries stay unwatched for the life of the daemon.
//
// WHAT IT COSTS. An unwatched file produces no event when it is edited: kqueue
// reports content changes on the FILE's descriptor, and a directory's NOTE_WRITE
// does not fire for a write that leaves the directory's own contents unchanged.
// So edits under those files silently stop triggering a re-index — the #6269
// failure shape, reached by a different route. The ledger reading is the visible
// symptom, not a second defect: descriptors that were never opened were also
// never charged, so `used` is an accurate account of a wrongly SMALL watch set.
// Watching the entries is what makes the ledger right.
//
// THE REPAIR. Compare a directory's listing against Watcher.dirEntries, the
// count grafel has charged for it. If the listing holds more chargeable entries
// than that, the difference is entries grafel is not watching — and each entry
// is Add()ed individually.
//
// Per ENTRY, not per directory, and that is not a detail. The obvious repair is
// to re-subscribe the directory itself, fs.Remove followed by fs.Add: Add alone
// cannot work, because addWatch re-lists only when the directory's stored
// dirFlags do not already carry NOTE_WRITE (backend_kqueue.go:418-420) and a
// directory grafel Add()ed always does. That repair is WRONG, and measurably so:
// watches.remove drops the directory from the `seen` set (:145-160), and `seen`
// is the parent's record that this entry is not new. The parent's next NOTE_WRITE
// therefore resurrects the directory as a Create (:657) — grafel charges its
// descriptor and re-walks it with subscribeDirRecursive, charging its whole
// listing a second time. Running the #6268 interleaved-fill test with that
// version of the repair reads 673 against a want of 660: exactly one directory
// plus its twelve files, charged twice.
//
// Add()ing an entry that fsnotify is already watching costs no descriptor —
// addWatch short-circuits on alreadyWatching (:358) and only re-registers the
// kevent — so Add()ing all of them opens exactly the ones that were missing.
// Peak descriptor use is never raised above what a correct listing would have
// held, which is the property this package exists to protect.
//
// An entry Add()ed this way is marked byUser (:290), and watchesInDir excludes
// byUser paths (:75-84) — so a later fs.Remove of the containing directory would
// NOT close it. Every path this repair Adds is therefore recorded in
// Watcher.dirUserEntries and removed explicitly on the teardown paths. A
// descriptor leak in the package whose job is bounded descriptor use would be a
// poor trade for a repair.
//
// WHY A SWEEP AND NOT AN EVENT TRIGGER. The obvious cheaper design — mark a
// directory dirty when an event arrives under it, and reconcile only those — is
// unsound for THIS defect, because the abandonment can happen on the first entry
// of the listing, in which case the directory produces no event at all. That is
// the shape the CI failure had: twelve files created, twelve missing, zero
// events. A signal that only fires when the directory is already reporting
// cannot see a directory that has stopped reporting.
//
// WHY THE DEFICIT IS CONFIRMED BEFORE IT IS ACTED ON. A directory whose Create
// events are merely still in the channel is short by exactly the same test as
// one whose listing was abandoned. Acting on the first sighting made the sweep
// fire 375 times in one 12-second run of the #6268 interleaved-fill test — work
// that is pure churn, since the events were about to arrive on their own. An
// abandonment is permanent, so waiting one sweep to confirm costs the repair
// that matters nothing at all.
//
// WHY THE REPAIR RUNS ON THE LOOP GOROUTINE. The scan is cheap and side-effect
// free, so it stays on the sweep goroutine; the repair is handed to the loop
// over Watcher.repairCh. See the field for why. Empirically: with the repair on
// the sweep goroutine the same interleaved-fill test misreads the ledger about
// one run in eight, in both directions.
//
// WHY IT DOES NOT FIGHT THE QUARANTINE. A quarantined directory is skipped
// outright. A directory that churns pathologically is one the tracker is
// already dropping events for; re-listing it on a timer would be exactly the
// thrash quarantine.go exists to stop.

// defaultReconcileInterval is how often a batch of subscribed directories is
// swept. A repair-of-last-resort cadence: nothing in normal operation depends on
// it, and the only thing a shorter interval buys is a faster repair of a defect
// that would otherwise be permanent. Switchable off entirely with
// GRAFEL_WATCH_RECONCILE_SEC.
const defaultReconcileInterval = 2 * time.Second

// dirDeficitCap bounds Watcher.dirDeficit — see recordDeficitLocked.
const dirDeficitCap = 8192

// reconcileEnv is the operator kill-switch, matching GRAFEL_QUARANTINE_SWEEP_SEC
// on the directly comparable loop: seconds, and 0 or negative disables the sweep
// entirely. It exists so a site that hits a pathological interaction can turn the
// repair off without a rebuild; the default is ON, because the defect it repairs
// is silent, permanent and user-facing.
const reconcileEnv = "GRAFEL_WATCH_RECONCILE_SEC"

// minReconcileInterval is a FLOOR, not a nicety, and it is the reason the sweep
// can claim to be ledger-neutral at all.
//
// The confirm-before-repair gate is what keeps a repair from racing the event
// stream: a directory whose Creates are still in the channel looks short until
// they drain, and waiting a whole sweep interval is how the sweep tells that
// apart from a listing fsnotify abandoned. That only works while the interval is
// longer than the queue takes to drain. Driven at 50 ms against a live burst the
// gate stops gating — every "deficit" it confirms is queue lag — and the ledger
// reading drifts by a descriptor over a few hundred events. Measured, on this
// tree, with this code.
//
// So the interval has a floor. An operator may lengthen it or switch the sweep
// off entirely (reconcileEnv), and neither of those can reach the regime where
// the confirmation is decorative.
const minReconcileInterval = time.Second

// reconcileBatch bounds a single pass. The sweep's whole cost when nothing is
// wrong is one os.ReadDir per directory, so the pass is bounded rather than the
// cycle: a watcher holding 40 000 directories spends the same per-second budget
// as one holding 40, and simply takes longer to come round again.
const reconcileBatch = 256

func (c *Config) reconcile() time.Duration {
	iv := c.reconcileInterval
	if iv == 0 {
		iv = defaultReconcileInterval
	}
	if v := os.Getenv(reconcileEnv); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			if n <= 0 {
				return -1 // disabled
			}
			iv = time.Duration(n) * time.Second
		}
	}
	if iv < 0 {
		return iv // explicitly disabled by the caller
	}
	if iv < minReconcileInterval {
		return minReconcileInterval
	}
	return iv
}

// reconcileLoop sweeps subscribed directories for listings fsnotify abandoned.
// A negative interval disables it.
func (w *Watcher) reconcileLoop() {
	defer w.loopWG.Done()
	iv := w.cfg.reconcile()
	if iv < 0 {
		w.logger.Info("watcher: reconcile sweep disabled", "override_env", reconcileEnv)
		return
	}
	ticker := time.NewTicker(iv)
	defer ticker.Stop()
	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.reconcileOnce()
		}
	}
}

// reconcileOnce sweeps up to reconcileBatch subscribed directories, refilling
// the cycle queue when it runs dry. Reported so a test can assert a whole cycle
// completed rather than sleeping for one.
func (w *Watcher) reconcileOnce() (repaired int) {
	if w.stopping() {
		return 0
	}
	cost := w.fdb.model()
	if cost.perEntry() <= 0 {
		// A per-watch backend (inotify) takes no descriptor on a directory's
		// entries and reports their changes through the directory's own watch,
		// so there is no per-entry listing to abandon and nothing to reconcile.
		// The same discriminator prune() and chargeEventOpen use.
		return 0
	}

	batch := w.nextReconcileBatch()
	acted := 0
	for _, dir := range batch {
		if w.stopping() {
			return repaired
		}
		if acted >= maxRepairEntriesPerPass {
			// Scanning is cheap and would finish the batch; ACTING is what puts
			// work on the loop goroutine, so acting is what is bounded. The
			// directories left over keep their confirmed deficit and are acted
			// on next pass.
			break
		}
		out := w.scanDir(dir, cost)
		if out.repaired {
			repaired++
		}
		acted += out.entriesActed
	}
	return repaired
}

// nextReconcileBatch pops up to reconcileBatch directories off the current
// cycle, starting a new cycle when the previous one is exhausted.
func (w *Watcher) nextReconcileBatch() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.reconcileQueue) == 0 {
		if len(w.dirToRepo) == 0 {
			return nil
		}
		w.reconcileQueue = make([]string, 0, len(w.dirToRepo))
		for d := range w.dirToRepo {
			w.reconcileQueue = append(w.reconcileQueue, d)
		}
	}
	n := reconcileBatch
	if n > len(w.reconcileQueue) {
		n = len(w.reconcileQueue)
	}
	batch := w.reconcileQueue[:n:n]
	w.reconcileQueue = w.reconcileQueue[n:]
	return batch
}

// scanDir looks for a directory short of watched entries and, on a CONFIRMED
// deficit, hands the repair to the loop goroutine. It reports whether entries
// were actually recovered.
//
// This is the half that runs on the SWEEP goroutine, and it is deliberately the
// half that does the reading: one os.ReadDir, two short critical sections, no
// ledger mutation and no fsnotify call. The entry list it builds travels with
// the request, so the loop goroutine never performs a directory read.
func (w *Watcher) scanDir(dir string, cost fdCostModel) scanOutcome {
	w.mu.Lock()
	repo, watched := w.dirToRepo[dir]
	have := w.dirEntries[dir]
	_, confirmed := w.dirDeficit[dir]
	if !watched {
		// Unsubscribed between the cycle snapshot and now. Drop the deficit
		// record with it: nothing else visits this key again, and a map that
		// only ever grows is a leak in a long-lived daemon (#6304).
		delete(w.dirDeficit, dir)
	}
	w.mu.Unlock()
	if !watched {
		return scanOutcome{}
	}
	// A quarantined directory is one the tracker has already decided is trash
	// churning faster than it is worth indexing. Re-listing it on a timer is the
	// thrash the tracker exists to stop, and its events are dropped anyway.
	//
	// The "_" is the tracker's calling convention, shared with subscribeDirRecursive:
	// IsQuarantined asks about the directory CONTAINING the path it is given, so a
	// question about `dir` is asked as a question about a notional child. For
	// dir == repo it resolves to "." and relDir refuses it — the repo root is
	// never quarantinable by design (quarantine.go relDir), which is the answer
	// this call wants anyway.
	if w.quarantine.IsQuarantined(repo, filepath.Join(dir, "_")) {
		return scanOutcome{}
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return scanOutcome{}
	}
	entries := make([]string, 0, len(ents))
	for _, e := range ents {
		// Subdirectories are excluded: their descriptor is charged where the
		// subdirectory itself is handled — perDir when grafel subscribes it, or
		// by prune when grafel skips it — never as an entry of the parent, and
		// Add()ing one would subscribe a subtree this sweep has no mandate over.
		if !e.IsDir() {
			entries = append(entries, filepath.Join(dir, e.Name()))
		}
	}
	if len(entries) <= have {
		// Fully covered, or holding entries whose Remove events have not been
		// drained yet. Never repaired downwards from here: an over-count is the
		// direction that REFUSES a subscription, which is the direction this
		// package prefers to err in; an under-count is the one that walks toward
		// EMFILE.
		w.mu.Lock()
		delete(w.dirDeficit, dir)
		w.mu.Unlock()
		return scanOutcome{}
	}
	if len(entries) > maxRepairEntries {
		// Refused, loudly, rather than repaired in part. A partial repair cannot
		// be accounted: the charge below is derived from "every entry of this
		// directory was attempted", and half an attempt makes that count a
		// fiction. The deficit record is kept, so raising the ceiling — or the
		// directory shrinking — makes the next sweep act on it.
		w.logger.Warn("watcher: directory too large to re-list — its unwatched entries stay unwatched",
			"repo", repo, "dir", dir, "entries", len(entries), "max", maxRepairEntries)
		w.mu.Lock()
		w.recordDeficitLocked(dir)
		w.mu.Unlock()
		return scanOutcome{}
	}
	if !confirmed {
		// First sighting. A directory whose Creates are still in the channel is
		// indistinguishable from one whose listing was abandoned, and only the
		// latter is still short on the next visit. An abandonment is permanent,
		// so waiting one sweep to confirm costs the repair that matters nothing
		// — and it is what keeps the sweep from firing hundreds of times during
		// an ordinary checkout burst.
		w.mu.Lock()
		w.recordDeficitLocked(dir)
		w.mu.Unlock()
		return scanOutcome{}
	}

	res := w.repairDir(dir, cost, entries)
	if res.settled {
		// Everything the listing named is now watched, so the directory is
		// settled whether or not anything had to be opened. The record is kept
		// in every other case on purpose — entries that could not be opened, and
		// a repair the budget refused. Those entries hold no descriptor,
		// dirEntries was not raised to cover them, and the directory must stay
		// eligible rather than be written off as repaired. A permanently
		// unreadable entry therefore costs one re-attempt per cycle, which is
		// the price of not going permanently blind on the directory holding it.
		w.mu.Lock()
		delete(w.dirDeficit, dir)
		w.mu.Unlock()
	}
	return scanOutcome{repaired: res.charged > 0, entriesActed: len(entries)}
}

// maxRepairEntriesPerPass bounds the work one pass may put on the LOOP
// goroutine, counted in the unit that work is actually done in — one fs.Add per
// entry — rather than in directories, which says nothing about size. At 4096
// register syscalls it is a few milliseconds of the drain per pass, against a
// subscribeDirRecursive that the event path already runs inline with no bound at
// all. Scanning is bounded separately by reconcileBatch; this bounds acting.
//
// A var, not a const, for the same reason watcherStopTimeout is: a test that
// wants to observe the bound should not have to build a tree big enough to hit
// the production one.
var maxRepairEntriesPerPass = 4096

// maxRepairEntries bounds the fs.Add calls one repair may make. Directories this
// wide are refused rather than partially repaired — see scanDir.
var maxRepairEntries = 4096

// repairResult reports what a repair did, which is more than "did anything
// change". A repair that left entries unreadable, or that the budget refused,
// has NOT settled the directory, and scanDir has to know the difference: those
// are exactly the cases where the deficit record must survive so the sweep comes
// back (#6304).
type repairResult struct {
	charged int  // entries newly watched and charged
	failed  int  // entries that could not be watched at all
	settled bool // every entry of the directory is now watched
}

// scanOutcome is what one directory's scan cost and achieved, so the pass can
// bound itself.
type scanOutcome struct {
	repaired     bool
	entriesActed int
}

// repairDir re-watches the entries of a directory whose listing fsnotify
// abandoned, and charges the ledger for the descriptors that opens.
//
// NEVER CALLED FROM THE LOOP GOROUTINE. It runs on the sweep goroutine, and that
// is a hard requirement rather than a preference: it calls fsAdd once per entry,
// and on Windows an Add only completes while the loop goroutine is draining
// Events — see the note on Watcher.fsAdd. Running it on the loop deadlocks
// against itself, the same shape as #6287 reached from the repair path, and it
// is what wedged the Windows job for a full 15-minute test timeout.
//
// Running off that goroutine means a Create CAN be processed between the tally
// read and the charge. That is handled by refusing to charge at all when it
// happens: the tally is re-checked under the lock and a repair that finds it
// moved abandons itself. A skipped repair is retried by the next sweep; a charge
// computed against a stale tally is a ledger drift nothing corrects.
func (w *Watcher) repairDir(dir string, cost fdCostModel, entries []string) repairResult {
	w.mu.Lock()
	repo, watched := w.dirToRepo[dir]
	have := w.dirEntries[dir]
	add := w.fsAdd
	w.mu.Unlock()
	if !watched {
		return repairResult{}
	}
	// Re-checked here and not only in scanDir: the two are separate steps and
	// the tracker can quarantine a directory in between.
	if w.quarantine.IsQuarantined(repo, filepath.Join(dir, "_")) {
		return repairResult{}
	}
	if len(entries) <= have {
		return repairResult{settled: true}
	}

	// Elective descriptors, unlike the event path's, which record opens that
	// have already happened. So this one asks the budget first and declines the
	// repair if it cannot afford it — a watcher at its ceiling stays partially
	// blind rather than pushing the process toward EMFILE.
	// The reservation is ATTRIBUTED as it is taken, in one critical section, and
	// every later adjustment moves both numbers together. The ledger and the
	// per-repo tally are one fact recorded twice (#6306), and any window where
	// they disagree is a window in which assertLedgerMatchesPerRepo is entitled
	// to fail — as it did, by exactly one descriptor, once in twenty runs, when
	// this reserved outside the lock and attributed at the end. It costs an
	// over-attribution of `est` for the length of the Add loop, which is the
	// direction that refuses subscriptions rather than the one that walks toward
	// EMFILE. fdBudget takes only its own mutex and never calls back, so nesting
	// is deadlock-free; this is the same nesting chargeEventOpen already does.
	est := (len(entries) - have) * cost.perEntry()
	w.mu.Lock()
	if w.dirToRepo[dir] != repo {
		// Already gone; nothing to reserve for.
		w.mu.Unlock()
		return repairResult{}
	}
	if !w.fdb.reserve(est) {
		w.mu.Unlock()
		used, limit := w.fdb.snapshot()
		w.logger.Warn("watcher: directory left partially unwatched — no budget to re-list it",
			"repo", repo, "dir", dir, "missing_entries", len(entries)-have,
			"fd_used", used, "fd_limit", limit, "override_env", fdBudgetEnv)
		return repairResult{}
	}
	w.fdReserved[repo] += est
	w.mu.Unlock()

	// unreserve hands back the whole reservation, both numbers together, for the
	// paths that abandon the repair.
	unreserve := func() {
		w.mu.Lock()
		// Only while the directory is still this repo's. If RemoveRepo or the
		// refusal unwind ran in between, it released `reserved + fdReserved[abs]`
		// — which INCLUDED this reservation — and dropped the key. Handing it
		// back again would take a descriptor off the ledger that the tally no
		// longer holds, and the clamp that stops fdReserved going negative makes
		// the two move by different amounts: the ledger ends one below the sum of
		// the per-repo tallies, which is #6268's failure mode (B) and what
		// assertLedgerMatchesPerRepo caught once in twenty-five runs.
		if w.dirToRepo[dir] == repo {
			if w.fdReserved[repo] -= est; w.fdReserved[repo] < 0 {
				w.fdReserved[repo] = 0
			}
			w.fdb.release(est)
		}
		w.mu.Unlock()
	}

	// Every entry is Add()ed, not just the ones believed missing: which of them
	// fsnotify already holds is not knowable from here — the entries it opens
	// itself are internal watches, absent from WatchList — and Add()ing one it
	// holds costs no descriptor, because addWatch short-circuits on
	// alreadyWatching before it reaches unix.Open (backend_kqueue.go:358-401).
	//
	// That is what makes len(added) an ABSOLUTE count rather than a delta: after
	// this loop, the entries fsnotify watches under dir are exactly the ones it
	// accepted. Deriving the charge from the LISTING instead would charge a
	// descriptor for every entry the process cannot open — and an unwatched path
	// emits no Remove, so nothing would ever release it, while dirEntries raised
	// to the listing count would leave `want <= have` true forever and the
	// directory never revisited. An EACCES entry is the trigger condition for
	// #6304; charging from the listing gets the first directory it meets in the
	// wild exactly wrong.
	added := make([]string, 0, len(entries))
	failed := 0
	for _, p := range entries {
		if w.stopping() {
			// Bail rather than keep Add()ing into a backend that is being torn
			// down. fsnotify's Windows AddWith checks isClosed and only THEN
			// posts to the reader's input channel, so an Add that starts after
			// the reader has gone has nothing to answer it — and this goroutine
			// is counted in loopWG, so Stop would wait out its whole timeout.
			unreserve()
			return repairResult{failed: failed}
		}
		if err := add(p); err != nil {
			// An entry that vanished between the listing and here, or one the
			// process cannot open. watchDirectoryFiles tolerates exactly these
			// and carries on (backend_kqueue.go:603-609); so does this.
			failed++
			continue
		}
		added = append(added, p)
	}

	w.mu.Lock()
	if w.dirToRepo[dir] != repo {
		// RemoveRepo (or a backend restart) dropped this directory while the
		// Adds above were in flight, and its own teardown could not have known
		// about watches that did not exist yet. Undo them here.
		w.mu.Unlock()
		unreserve()
		for _, p := range added {
			_ = w.fsRemove(p)
		}
		return repairResult{failed: failed}
	}
	// `have` is still current: this runs on the loop goroutine, so no Create can
	// have been processed since it was read. Guarded rather than assumed — if
	// this is ever moved off that goroutine the repair skips instead of charging
	// against a stale tally, which is a missed repair the next sweep retries
	// rather than a ledger drift nothing corrects. Free where it belongs: the
	// branch is unreachable while the placement is correct.
	if now := w.dirEntries[dir]; now != have {
		w.mu.Unlock()
		unreserve()
		w.logger.Warn("watcher: repair abandoned — the directory's tally moved underneath it",
			"repo", repo, "dir", dir, "was", have, "now", now)
		return repairResult{failed: failed}
	}
	charged := (len(added) - have) * cost.perEntry()
	if charged < 0 {
		charged = 0
	}
	if charged > 0 {
		// Recorded so RemoveRepo, the refusal unwind and Stop can close what only
		// this function knows it opened — fs.Remove of the DIRECTORY will not,
		// because fsnotify leaves user-added paths alone when it unwatches a
		// directory's contents (backend_kqueue.go:75-84, :325-333).
		userEntries := w.dirUserEntries[dir]
		if userEntries == nil {
			userEntries = make(map[string]struct{}, len(added))
			w.dirUserEntries[dir] = userEntries
		}
		for _, p := range added {
			userEntries[p] = struct{}{}
		}
		w.dirEntries[dir] = have + charged/cost.perEntry()
		// Every entry of dir has just been accounted for, so no released-dir
		// marker under it can still stand for a live descriptor (#6293) — the
		// same invariant both subscribe paths restore after their own listing.
		w.forgetReleasedEntriesLocked(dir)
		// An entry this function opened was never markSeen'd by fsnotify, so the
		// directory's next dirChange WILL report it as a Create
		// (backend_kqueue.go:657) even though the descriptor already exists; the
		// marker is what stops that report being charged a second time. Markers
		// for entries fsnotify already held are never consumed — those entries
		// are seen, and seen entries never produce a Create — which is the same
		// residue subscribeDirRecursive's post-Add listing leaves and which
		// fdPreChargedCap exists to bound. A repair that opened nothing
		// contributes none of it, which is why this is inside `charged > 0`.
		if len(w.fdPreCharged)+len(added) > fdPreChargedCap {
			w.fdPreCharged = make(map[string]struct{}, len(added))
		}
		for _, p := range added {
			w.fdPreCharged[p] = struct{}{}
		}
	}
	// Settle the reservation against what was actually opened — inside the same
	// critical section that holds the attribution, so the pair is never observed
	// disagreeing.
	if charged > est {
		// Already open: the descriptors exist whether or not they fit, exactly
		// as on the event path.
		w.fdReserved[repo] += charged - est
		w.fdb.charge(charged - est)
	} else if charged < est {
		if w.fdReserved[repo] -= est - charged; w.fdReserved[repo] < 0 {
			w.fdReserved[repo] = 0
		}
		w.fdb.release(est - charged)
	}
	w.mu.Unlock()

	if charged > 0 || failed > 0 {
		w.logger.Info("watcher: re-watched entries of a directory whose listing fsnotify had abandoned",
			"repo", repo, "dir", dir,
			"entries_recovered", charged/cost.perEntry(),
			"entries_unreadable", failed)
	}
	return repairResult{charged: charged / cost.perEntry(), failed: failed, settled: failed == 0}
}
