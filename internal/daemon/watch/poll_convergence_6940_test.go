package watch

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/walk"
	"github.com/cajasmota/grafel/internal/gitmeta"
	"github.com/cajasmota/grafel/internal/indexer/diff"
)

// #6940 — a path git REPORTS and the walker REFUSES.
//
// WalkRepo's file branch has four gates; walk.IndexableEntryType (which the
// poller shares) reproduces one of them. A path that clears the entry-type
// gate and is then refused by the extension filter, the file-level ignore
// layer or the sparse filter used to enter the candidate set, get stamped by
// nothing, and read as new on EVERY cycle — one full reindex enqueued per repo
// per interval, forever.
//
// Every test here asserts CONVERGENCE — a bounded number of cycles after which
// the path stops being reported — because first-cycle detection is what hid
// the two earlier instances of this same defect class.
//
// The opposite direction is asserted just as hard (#6902): an assertion that
// the loop stopped cannot see a change that is now MISSED, and the mirror
// defect (a path the indexer WILL stamp being refused by the poller) is the
// blocker #6935 had to fix. See the three ...IsNeverDeclined /
// ...WithoutACompletedIndexPass / ...ForgivenWhenTheIndexerChangesItsMind
// cases below.

// cpDeclineCount reports how many paths the poller currently refuses for repo.
// It reads the tracker, not a log line: the decline set is the mechanism.
func cpDeclineCount(t *testing.T, p *ChangePoller, repo string) int {
	t.Helper()
	abs, err := filepath.Abs(repo)
	if err != nil {
		t.Fatal(err)
	}
	tr := p.trackerFor(abs)
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return len(tr.refused)
}

// --- gate 1: the indexed-extension filter ---------------------------------

// An untracked `photo.png`: git reports it, walk.IndexableEntryType accepts it
// (it is a regular file), and shouldSkipFileByExt refuses it. It can never be
// stamped, so a candidate set that keeps offering it never converges.
func TestChangePoller_ExtensionFilteredPathConverges(t *testing.T) {
	repo, state := cpNewRepo(t)
	cpIndexPass(t, repo, state)
	p, submits := cpNewTestPoller(t, repo, state)

	cpWriteFile(t, repo, "photo.png", "\x89PNG not really\n")

	// Premise 1: git reports it, and keeps reporting it — it is untracked, so
	// nothing short of committing or ignoring it will make git stop.
	if out := cpGitRun(t, repo, "status", "--porcelain", "-unormal"); !strings.Contains(out, "photo.png") {
		t.Fatalf("fixture premise broken: git does not report photo.png: %q", out)
	}
	// Premise 2: the walker refuses it — this is the gate under test, and if
	// the extension list ever grew to cover .png the test would be vacuous.
	files, _, err := walk.WalkRepo(repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cpContains(files, "photo.png") {
		t.Fatalf("fixture premise broken: the walker INDEXES photo.png, so no gate is under test: %v", files)
	}

	// Cycle 1 reports it. That is correct and must stay correct: on the first
	// cycle the poller genuinely cannot tell a refused path from a new one.
	if got := cpCycle(t, p, repo); !cpContains(got, "photo.png") {
		t.Fatalf("first cycle did not report the newly discovered path: %v", got)
	}
	if n := len(*submits); n != 1 {
		t.Fatalf("expected exactly one submission for the discovery, got %d", n)
	}

	// The index pass runs and declines it: the manifest cannot hold it.
	cpIndexPass(t, repo, state)
	if keys := cpManifestKeys(t, state); cpContains(keys, "photo.png") {
		t.Fatalf("fixture premise broken: the index pass stamped photo.png: %v", keys)
	}
	// git still reports it. This is exactly the input the old candidate set
	// took unconditionally.
	if out := cpGitRun(t, repo, "status", "--porcelain", "-unormal"); !strings.Contains(out, "photo.png") {
		t.Fatalf("fixture premise broken: git stopped reporting photo.png, so the loop is untested: %q", out)
	}

	before := len(*submits)
	if n := cpConverges(t, p, repo, 6); n != 0 {
		t.Fatalf("photo.png was still reported for %d cycles after the index pass declined it", n)
	}
	if after := len(*submits); after != before {
		t.Fatalf("poller enqueued %d more reindexes after convergence", after-before)
	}
	if n := cpDeclineCount(t, p, repo); n != 1 {
		t.Fatalf("expected exactly one recorded decline, got %d", n)
	}
}

// --- gate 3: the file-level ignore layer (#6931/#6933) ---------------------

// A TRACKED but gitignored file. Ignore rules do not apply to tracked files, so
// git reports every edit to it forever, while WalkRepo's igStack.MatchFile
// refuses it — the second driven shape in #6940, and the one #6933 widened
// into existence. This repo has two such files today (#6937).
func TestChangePoller_TrackedButGitignoredPathConverges(t *testing.T) {
	repo, state := cpNewRepo(t)
	cpWriteFile(t, repo, ".gitignore", "gen.go\n")
	cpWriteFile(t, repo, "gen.go", "package a\n\nfunc Gen() {}\n")
	cpGitRun(t, repo, "add", "-A")
	cpGitRun(t, repo, "add", "-f", "gen.go")
	cpGitRun(t, repo, "commit", "-q", "-m", "tracked-but-ignored")
	cpIndexPass(t, repo, state)

	// Premise: the ignore layer refuses a file git tracks.
	if keys := cpManifestKeys(t, state); cpContains(keys, "gen.go") {
		t.Fatalf("fixture premise broken: gen.go IS indexed, so the ignore gate is not under test: %v", keys)
	}

	p, submits := cpNewTestPoller(t, repo, state)
	cpWriteFile(t, repo, "gen.go", "package a\n\nfunc Gen() { _ = 1 }\n")
	if out := cpGitRun(t, repo, "status", "--porcelain", "-unormal"); !strings.Contains(out, "gen.go") {
		t.Fatalf("fixture premise broken: git does not report the tracked-but-ignored edit: %q", out)
	}

	if got := cpCycle(t, p, repo); !cpContains(got, "gen.go") {
		t.Fatalf("first cycle did not report gen.go: %v", got)
	}
	cpIndexPass(t, repo, state)
	if keys := cpManifestKeys(t, state); cpContains(keys, "gen.go") {
		t.Fatalf("fixture premise broken: the index pass stamped gen.go: %v", keys)
	}
	if out := cpGitRun(t, repo, "status", "--porcelain", "-unormal"); !strings.Contains(out, "gen.go") {
		t.Fatalf("fixture premise broken: git stopped reporting gen.go: %q", out)
	}

	before := len(*submits)
	if n := cpConverges(t, p, repo, 6); n != 0 {
		t.Fatalf("gen.go was still reported for %d cycles after the index pass declined it", n)
	}
	if after := len(*submits); after != before {
		t.Fatalf("poller enqueued %d more reindexes after convergence", after-before)
	}
}

// --- gate 4: the sparse-checkout filter ------------------------------------

// cpIndexPassSparse is cpIndexPass with the sparse probe the real incremental
// indexer performs, so the sparse gate is actually applied.
func cpIndexPassSparse(t *testing.T, repo, state string) {
	t.Helper()
	si := gitmeta.ProbeRepo(repo)
	files, _, err := walk.WalkRepo(repo, &walk.Options{Sparse: &si})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	m := diff.LoadManifest(state)
	changed, _ := diff.Filter(repo, files, m)
	diff.UpdateManifestScoped(repo, changed, files, m)
	if err := diff.SaveManifestAtCommit(state, m, "", ""); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
}

// An UNTRACKED file outside the sparse cone. Sparse checkout does not remove
// untracked files, so the path is on disk and git reports its directory — but
// WalkRepo's sparse filter refuses it by repo-relative path, and the poller's
// subtree walk (rooted below the repo root, with no sparse state) hands it over
// as a candidate.
func TestChangePoller_SparseFilteredPathConverges(t *testing.T) {
	repo, state := cpNewRepo(t)
	cpWriteFile(t, repo, "src/keep.go", "package src\n")
	cpGitRun(t, repo, "add", "-A")
	cpGitRun(t, repo, "commit", "-q", "-m", "src")
	// --no-cone deliberately: git's CONE patterns are `/*` + `!/*/` + `/src/`,
	// and gitmeta.IsPathIncluded treats a bare `*` as match-all, so a cone
	// checkout makes its own filter include everything. That is a real gap in
	// the sparse implementation and it is not #6940; a non-cone pattern set is
	// the shape grafel's filter actually applies, so it is the one that puts
	// the SPARSE GATE under test here rather than a no-op.
	if out, err := cpGitTry(repo, "sparse-checkout", "set", "--no-cone", "src"); err != nil {
		t.Skipf("git sparse-checkout unavailable here: %v\n%s", err, out)
	}
	// git >= 2.32 writes core.sparseCheckout into the WORKTREE-scoped config
	// (.git/config.worktree, via extensions.worktreeConfig), and
	// gitmeta.ProbeRepo reads `config --local` only — so the probe does not see
	// a sparse checkout this git just created. That gap is real and is not
	// #6940; mirroring the flag into --local here keeps the SPARSE GATE, which
	// is what this test is about, reachable rather than silently untested.
	cpGitRun(t, repo, "config", "--local", "core.sparseCheckout", "true")
	cpGitRun(t, repo, "config", "--local", "core.sparseCheckoutCone", "true")
	if si := gitmeta.ProbeRepo(repo); !si.IsSparse {
		// NOT a skip (#6961 review): the two config lines above make this
		// outcome deterministic, and CI runs without -v, so a skip here would
		// delete the only sparse leg in silence — removing just those two
		// lines turned the whole test into "SKIP ... PASS, exit 0".
		t.Fatal("the probe does not see the sparse checkout this fixture just created — the sparse gate is UNTESTED, not unreachable")
	}

	cpWriteFile(t, repo, "outside/gen.go", "package outside\n")
	cpIndexPassSparse(t, repo, state)
	keys := cpManifestKeys(t, state)
	if len(keys) == 0 {
		t.Fatal("fixture premise broken: nothing indexed at all")
	}
	if cpContains(keys, "outside/gen.go") {
		t.Fatalf("fixture premise broken: the sparse gate did not refuse outside/gen.go: %v", keys)
	}

	p, _ := cpNewTestPoller(t, repo, state)
	got := cpCycle(t, p, repo)
	if !cpContains(got, "outside/gen.go") {
		// Not a skip: if the untracked subtree stops reaching the candidate
		// set, the sparse loop is not "unreachable here", it is UNTESTED, and
		// a skip would hide that for as long as it lasted.
		t.Fatalf("the poller did not surface outside/gen.go as a candidate (%v) — the sparse case is untested", got)
	}
	cpIndexPassSparse(t, repo, state)
	if n := cpConverges(t, p, repo, 6); n != 0 {
		t.Fatalf("outside/gen.go was still reported for %d cycles after the sparse gate declined it", n)
	}
}

// cpGitTry runs git and returns its output and error rather than failing the
// test — for optional fixture steps (sparse-checkout) that may be unsupported.
func cpGitTry(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// --- the other direction (#6902): what must NOT be declined ----------------

// A genuinely new, indexable file must never be declined. It is discovered on
// cycle 1 exactly like photo.png is, and the ONLY thing that separates them is
// what the index pass then does with it. If the tracker keyed on "reported and
// not yet stamped" alone, this file would be dropped and every later edit to it
// silently missed — the mirror of the loop the rest of this file closes.
func TestChangePoller_NewIndexableFileIsNeverDeclined(t *testing.T) {
	repo, state := cpNewRepo(t)
	cpIndexPass(t, repo, state)
	p, _ := cpNewTestPoller(t, repo, state)

	cpWriteFile(t, repo, "gamma.go", "package a\n\nfunc Gamma() {}\n")
	if got := cpCycle(t, p, repo); !cpContains(got, "gamma.go") {
		t.Fatalf("cycle 1 did not report the new file: %v", got)
	}
	cpIndexPass(t, repo, state)
	if keys := cpManifestKeys(t, state); !cpContains(keys, "gamma.go") {
		t.Fatalf("fixture premise broken: the index pass did not stamp gamma.go: %v", keys)
	}
	// Quiet once the manifest agrees...
	if n := cpConverges(t, p, repo, 6); n != 0 {
		t.Fatalf("an indexed file kept being reported for %d cycles", n)
	}
	if n := cpDeclineCount(t, p, repo); n != 0 {
		t.Fatalf("an indexed file was recorded as declined (%d entries) — later edits to it would be invisible", n)
	}
	// ...and STILL detected when it changes. This is the assertion a
	// convergence test cannot make for itself.
	cpWriteFile(t, repo, "gamma.go", "package a\n\nfunc Gamma() { _ = 2 }\n")
	if got := cpCycle(t, p, repo); !cpContains(got, "gamma.go") {
		t.Fatalf("an edit to an indexed file was MISSED: %v", got)
	}
}

// A decline requires proof that an index pass actually ran: m.IndexedAt is
// stamped by every manifest save, so it distinguishes "the indexer looked and
// said no" from "our request has not been serviced yet". Without that premise
// the poller would drop a perfectly indexable file whose reindex was merely
// debounced or circuit-broken, and never look at it again.
func TestChangePoller_NoDeclineWithoutACompletedIndexPass(t *testing.T) {
	repo, state := cpNewRepo(t)
	cpIndexPass(t, repo, state)
	p, submits := cpNewTestPoller(t, repo, state)

	cpWriteFile(t, repo, "delta.go", "package a\n\nfunc Delta() {}\n")
	for i := 1; i <= 3; i++ {
		got := cpCycle(t, p, repo)
		if !cpContains(got, "delta.go") {
			t.Fatalf("cycle %d stopped reporting delta.go although NO index pass ever ran: %v", i, got)
		}
	}
	if n := len(*submits); n != 3 {
		t.Fatalf("expected a submission on each of 3 cycles (level-triggered), got %d", n)
	}
	if n := cpDeclineCount(t, p, repo); n != 0 {
		t.Fatalf("a path was declined with no completed index pass to justify it (%d entries)", n)
	}
}

// A decline is an observation about one past pass, not a life sentence. Drop
// the ignore rule and the indexer changes its mind; the poller must follow it
// back. The chain is real, not stipulated: .gitignore is itself a manifest key,
// so editing it is detected by the manifest sweep, the reindex re-walks the
// repo, and gen.go is stamped.
func TestChangePoller_DeclineIsForgivenWhenTheIndexerChangesItsMind(t *testing.T) {
	repo, state := cpNewRepo(t)
	cpWriteFile(t, repo, ".gitignore", "gen.go\n")
	cpWriteFile(t, repo, "gen.go", "package a\n\nfunc Gen() {}\n")
	cpGitRun(t, repo, "add", "-A")
	cpGitRun(t, repo, "add", "-f", "gen.go")
	cpGitRun(t, repo, "commit", "-q", "-m", "tracked-but-ignored")
	cpIndexPass(t, repo, state)
	if keys := cpManifestKeys(t, state); !cpContains(keys, ".gitignore") {
		t.Fatalf("fixture premise broken: .gitignore is not itself indexed, so the forgiveness chain is untested: %v", keys)
	}

	p, _ := cpNewTestPoller(t, repo, state)
	cpWriteFile(t, repo, "gen.go", "package a\n\nfunc Gen() { _ = 1 }\n")
	cpCycle(t, p, repo)
	cpIndexPass(t, repo, state)
	if n := cpConverges(t, p, repo, 6); n != 0 {
		t.Fatalf("gen.go did not converge (%d cycles) — the premise of this test is the decline", n)
	}
	if n := cpDeclineCount(t, p, repo); n != 1 {
		t.Fatalf("expected gen.go to be declined, got %d declines", n)
	}

	// Now un-ignore it.
	cpWriteFile(t, repo, ".gitignore", "# nothing ignored any more\n")
	got := cpCycle(t, p, repo)
	if !cpContains(got, ".gitignore") {
		t.Fatalf("the .gitignore edit itself was not detected: %v", got)
	}
	cpIndexPass(t, repo, state)
	if keys := cpManifestKeys(t, state); !cpContains(keys, "gen.go") {
		t.Fatalf("fixture premise broken: the re-walk did not stamp the un-ignored gen.go: %v", keys)
	}
	// The decline must be forgiven, and a later edit detected again.
	cpCycle(t, p, repo)
	if n := cpDeclineCount(t, p, repo); n != 0 {
		t.Fatalf("gen.go is still declined after the indexer stamped it (%d entries)", n)
	}
	cpWriteFile(t, repo, "gen.go", "package a\n\nfunc Gen() { _ = 3 }\n")
	if got := cpCycle(t, p, repo); !cpContains(got, "gen.go") {
		t.Fatalf("an edit to the now-indexed gen.go was MISSED: %v", got)
	}
}

// A decline is per repo. Two repos polled by one poller must not share a
// refusal, or a path refused in one silences the same relative path in the
// other — a missed change with no visible cause.
func TestChangePoller_DeclinesAreScopedToOneRepo(t *testing.T) {
	repoA, stateA := cpNewRepo(t)
	repoB, stateB := cpNewRepo(t)
	cpIndexPass(t, repoA, stateA)
	cpIndexPass(t, repoB, stateB)

	var submits []string
	p := NewChangePoller(ChangePollerConfig{
		StateDir: func(rp string) string {
			if rp == repoA {
				return stateA
			}
			return stateB
		},
		DisableWarmUp: true,
	}, func(rp string, bulk bool) { submits = append(submits, rp) }, nil)
	for _, r := range []string{repoA, repoB} {
		if err := p.AddRepo(r); err != nil {
			t.Fatalf("AddRepo: %v", err)
		}
	}
	t.Cleanup(func() { p.Stop() })

	// Same relative path in both: refused in A (a .png), indexable in B.
	cpWriteFile(t, repoA, "shared.png", "binary-ish\n")
	cpWriteFile(t, repoB, "shared.go", "package a\n\nfunc S() {}\n")
	p.PollOnce()
	cpIndexPass(t, repoA, stateA)
	cpIndexPass(t, repoB, stateB)
	p.PollOnce()

	if n := cpDeclineCount(t, p, repoA); n != 1 {
		t.Fatalf("repo A should hold exactly the one decline, got %d", n)
	}
	if n := cpDeclineCount(t, p, repoB); n != 0 {
		t.Fatalf("repo B holds %d declines it never earned", n)
	}
	cpWriteFile(t, repoB, "shared.go", "package a\n\nfunc S() { _ = 1 }\n")
	res := p.PollOnce()
	got := res[repoB]
	sort.Strings(got)
	if !cpContains(got, "shared.go") {
		t.Fatalf("repo B's edit was missed: %v", got)
	}
	if r := res[repoA]; len(r) != 0 {
		t.Fatalf("repo A re-fired after convergence: %v", r)
	}
}

// RemoveRepo must drop the tracker with the repo — otherwise a re-registered
// repo starts life refusing paths that were decided in an earlier era.
func TestChangePoller_RemoveRepoDropsTheDeclineSet(t *testing.T) {
	repo, state := cpNewRepo(t)
	cpIndexPass(t, repo, state)
	p, _ := cpNewTestPoller(t, repo, state)
	cpWriteFile(t, repo, "photo.png", "not a png\n")
	cpCycle(t, p, repo)
	cpIndexPass(t, repo, state)
	cpCycle(t, p, repo)
	if n := cpDeclineCount(t, p, repo); n != 1 {
		t.Fatalf("expected one decline before RemoveRepo, got %d", n)
	}
	p.RemoveRepo(repo)
	if n := cpDeclineCount(t, p, repo); n != 0 {
		t.Fatalf("RemoveRepo left %d declines behind", n)
	}
	// The refusal is gone but the file is not: re-registering the repo must
	// re-learn the decline from scratch, one cycle at a time.
	if _, err := os.Stat(filepath.Join(repo, "photo.png")); err != nil {
		t.Fatalf("fixture disappeared: %v", err)
	}
	if err := p.AddRepo(repo); err != nil {
		t.Fatalf("re-AddRepo: %v", err)
	}
	if got := cpCycle(t, p, repo); !cpContains(got, "photo.png") {
		t.Fatalf("a re-registered repo inherited a stale refusal: %v", got)
	}
}

// --- the boundary condition (2) rests on -------------------------------------

// MP-1, from the #6961 review: `!m.IndexedAt.After(submittedAt)` is what makes
// condition (2) PROOF that an index pass ran rather than a coincidence, and
// nothing observed the strictness — relaxing it to `Before` (i.e. ">=") left
// the whole package green. Two independent nanosecond clock reads do not
// collide in practice, so this is not a live defect; it is an ungraded claim,
// and a "simplification" of the comparison would ship green.
//
// Both halves are asserted, because an equality case that declines nothing is
// also what a tracker that declines NOTHING AT ALL looks like: the positive
// control drives the very same tracker one nanosecond further and requires the
// decline to appear.
func TestDeclineTracker_EqualTimestampIsNotProofOfACompletedPass(t *testing.T) {
	tr := &declineTracker{
		pending: make(map[string]time.Time),
		refused: make(map[string]struct{}),
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	at := time.Now().UTC()
	tr.noteSubmitted([]string{"photo.png"}, at)

	// EXACTLY equal: a manifest stamped at the same instant we submitted is not
	// evidence that a pass has since completed.
	tr.reconcile(&diff.Manifest{
		IndexedAt: at,
		Files:     map[string]diff.FileEntry{"alpha.go": {}},
	}, logger, "/repo")
	if tr.declined("photo.png") {
		t.Fatal("a manifest stamped at the SAME instant as the submission was taken as proof a pass completed")
	}

	// Positive control: one nanosecond later is proof, and must decline — else
	// the assertion above passes for the wrong reason.
	tr.reconcile(&diff.Manifest{
		IndexedAt: at.Add(time.Nanosecond),
		Files:     map[string]diff.FileEntry{"alpha.go": {}},
	}, logger, "/repo")
	if !tr.declined("photo.png") {
		t.Fatal("a manifest stamped strictly AFTER the submission did not decline the unstamped path — the equality assertion above is vacuous")
	}
}

// --- condition (1): only DISCOVERED paths, only after the request went out ---

// M1b, from the #6961 review. The tracker must record only paths that arrived
// through DISCOVERY, never a manifest key that half 1 swept. Recording manifest
// keys is a silent missed change, and it is reachable in three ordinary steps:
//
//  1. delete a manifest key — the poller reports it (correctly),
//  2. the pass prunes it and advances IndexedAt, so under the mutant the path
//     is now refused,
//  3. RECREATE it. It is no longer a manifest key, so it can only arrive
//     through discovery — and discovery skips refused paths. The recreated
//     file is then never indexed until some unrelated change triggers a
//     stamping pass, or the daemon restarts.
//
// The shipped code is right because `discovered` excludes anything already in
// `cand`; this is what fails when that stops being true.
func TestChangePoller_RecreatedManifestKeyIsNeverRefused(t *testing.T) {
	repo, state := cpNewRepo(t)
	cpIndexPass(t, repo, state)
	p, _ := cpNewTestPoller(t, repo, state)

	if err := os.Remove(filepath.Join(repo, "alpha.go")); err != nil {
		t.Fatal(err)
	}
	if got := cpCycle(t, p, repo); !cpContains(got, "alpha.go") {
		t.Fatalf("the deletion of a manifest key was not reported: %v", got)
	}
	cpIndexPass(t, repo, state)
	if keys := cpManifestKeys(t, state); cpContains(keys, "alpha.go") {
		t.Fatalf("fixture premise broken: the pass did not prune the deleted key: %v", keys)
	}
	if n := cpConverges(t, p, repo, 6); n != 0 {
		t.Fatalf("the pruned deletion still reported for %d cycles", n)
	}
	// Nothing about a deletion is a REFUSAL: the indexer did not decline the
	// path, it agreed the path is gone.
	if n := cpDeclineCount(t, p, repo); n != 0 {
		t.Fatalf("a swept manifest key was recorded as declined (%d entries) — its recreation would be invisible", n)
	}

	cpWriteFile(t, repo, "alpha.go", "package a\n\nfunc Alpha() { _ = 7 }\n")
	if got := cpCycle(t, p, repo); !cpContains(got, "alpha.go") {
		t.Fatalf("the RECREATED file was never reported (%v) — it would stay unindexed until an unrelated change or a daemon restart", got)
	}
}

// M7, from the same review: pending is recorded only AFTER the sink call, so a
// cycle that submits nothing records nothing.
//
// The guard is currently UNREACHABLE, and this test says why by pinning the
// premise rather than asserting the guard directly: every discovered path is by
// construction absent from the manifest, and diff.isChanged returns true
// unconditionally for a path the manifest lacks (diff.go, "new file"), so
// discovered is always a SUBSET of changed and a cycle with a non-empty
// discovered set always submits. If that ever stops holding — a Filter that can
// drop a discovered path — this assertion fails first, and that is exactly the
// moment the guard starts carrying weight.
func TestChangePoller_EveryDiscoveredPathIsSubmitted(t *testing.T) {
	repo, state := cpNewRepo(t)
	cpIndexPass(t, repo, state)
	p, submits := cpNewTestPoller(t, repo, state)

	// A quiescent cycle submits nothing and must record nothing.
	if got := cpCycle(t, p, repo); len(got) != 0 {
		t.Fatalf("fixture premise broken: the repo is not quiescent: %v", got)
	}
	abs, err := filepath.Abs(repo)
	if err != nil {
		t.Fatal(err)
	}
	tr := p.trackerFor(abs)
	tr.mu.Lock()
	pend := len(tr.pending)
	tr.mu.Unlock()
	if pend != 0 {
		t.Fatalf("a cycle that submitted nothing recorded %d pending path(s)", pend)
	}
	if len(*submits) != 0 {
		t.Fatalf("a quiescent cycle submitted %d reindex requests", len(*submits))
	}

	// The premise: discovered is a subset of changed, so "discovered but not
	// submitted" cannot arise. Driven with both discovery shapes at once — an
	// untracked file and an untracked subtree — plus a modified manifest key.
	cpWriteFile(t, repo, "new.go", "package a\n\nfunc New() {}\n")
	cpWriteFile(t, repo, "sub/deep.go", "package sub\n")
	cpWriteFile(t, repo, "beta.go", "package a\n\nfunc Beta() { _ = 1 }\n")
	changed, discovered := p.pollRepo(abs)
	if len(discovered) == 0 {
		t.Fatal("fixture premise broken: nothing was discovered, so the subset claim is vacuous")
	}
	for _, d := range discovered {
		if !cpContains(changed, d) {
			t.Fatalf("discovered path %q is NOT in the changed set %v — a cycle can now discover a path it does not submit, and the pending guard is load-bearing", d, changed)
		}
	}
}

// The cap's DIRECTION is the claim: past it the poller over-fires rather than
// dropping a path. Both maps are capped, and both caps are asserted here —
// pending because it drains only on a completed pass, refused because it is
// what suppresses reporting. Driven against the tracker directly: 4096 is far
// too many paths to route through a git fixture.
func TestDeclineTracker_CapOverflowOverFires(t *testing.T) {
	tr := &declineTracker{
		pending: make(map[string]time.Time),
		refused: make(map[string]struct{}),
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	at := time.Now().UTC()

	paths := make([]string, 0, maxDeclinedPaths+10)
	for i := 0; i < maxDeclinedPaths+10; i++ {
		paths = append(paths, "junk/"+strconv.Itoa(i)+".png")
	}
	tr.noteSubmitted(paths, at)
	tr.mu.Lock()
	pend := len(tr.pending)
	tr.mu.Unlock()
	if pend > maxDeclinedPaths {
		t.Fatalf("pending grew to %d, past the cap of %d — it drains only on a completed pass, so this is the unbounded case", pend, maxDeclinedPaths)
	}

	tr.reconcile(&diff.Manifest{
		IndexedAt: at.Add(time.Second),
		Files:     map[string]diff.FileEntry{"alpha.go": {}},
	}, logger, "/repo")

	tr.mu.Lock()
	ref := len(tr.refused)
	tr.mu.Unlock()
	if ref == 0 {
		t.Fatal("nothing was refused at all — the cap assertions here would pass for the wrong reason")
	}
	if ref > maxDeclinedPaths {
		t.Fatalf("refused grew to %d, past the cap of %d", ref, maxDeclinedPaths)
	}
	// The overflow must still be REPORTED. A cap that silently suppressed the
	// paths it could not record would be the mirror defect at scale.
	var suppressed, overflowing int
	for _, rel := range paths {
		if tr.declined(rel) {
			suppressed++
		} else {
			overflowing++
		}
	}
	if suppressed != maxDeclinedPaths {
		t.Fatalf("expected exactly %d suppressed paths, got %d", maxDeclinedPaths, suppressed)
	}
	if overflowing != 10 {
		t.Fatalf("expected the 10 over-cap paths to keep re-reporting (over-firing), got %d", overflowing)
	}

	// The two caps must be graded SEPARATELY, or they mask each other: with
	// pending capped, a promotion loop that ignored its own cap would still
	// never see more than maxDeclinedPaths candidates in one pass. pending is
	// drained now, so this second round reaches the promotion cap directly.
	extra := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		extra = append(extra, "second-round/"+strconv.Itoa(i)+".png")
	}
	tr.noteSubmitted(extra, at)
	tr.reconcile(&diff.Manifest{
		IndexedAt: at.Add(2 * time.Second),
		Files:     map[string]diff.FileEntry{"alpha.go": {}},
	}, logger, "/repo")
	tr.mu.Lock()
	ref2 := len(tr.refused)
	tr.mu.Unlock()
	if ref2 != maxDeclinedPaths {
		t.Fatalf("refused is %d after a second round, cap is %d — the promotion cap is not enforced", ref2, maxDeclinedPaths)
	}
	for _, rel := range extra {
		if tr.declined(rel) {
			t.Fatalf("%q was suppressed past the cap — over the cap the poller must OVER-fire, not drop a path", rel)
		}
	}
}
