package watch

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/walk"
	"github.com/cajasmota/grafel/internal/indexer/diff"
)

// --- fixture helpers -------------------------------------------------------

func cpGitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return string(out)
}

// cpNewRepo builds a git repo with two committed source files and returns
// (repoPath, stateDir). The state dir is EMPTY — call cpIndexPass to seed it.
func cpNewRepo(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	cpGitRun(t, repo, "init", "-q", "-b", "main")
	cpWriteFile(t, repo, "alpha.go", "package a\n\nfunc Alpha() {}\n")
	cpWriteFile(t, repo, "beta.go", "package a\n\nfunc Beta() {}\n")
	cpGitRun(t, repo, "add", "-A")
	cpGitRun(t, repo, "commit", "-q", "-m", "init")
	return repo, state
}

func cpWriteFile(t *testing.T, repo, rel, content string) {
	t.Helper()
	abs := filepath.Join(repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// cpIndexPass mirrors what the real incremental indexer does to the manifest
// (internal/extractors/incremental.go:640-643): walk, Filter, re-stamp only the
// changed set, reconcile membership against the full walked set, save.
func cpIndexPass(t *testing.T, repo, state string) []string {
	t.Helper()
	files, _, err := walk.WalkRepo(repo, nil)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	m := diff.LoadManifest(state)
	changed, _ := diff.Filter(repo, files, m)
	diff.UpdateManifestScoped(repo, changed, files, m)
	if err := diff.SaveManifestAtCommit(state, m, "", ""); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	sort.Strings(changed)
	return changed
}

// cpManifestFiles reads the on-disk file-index.json and returns its Files map.
// It deliberately reads the RAW FILE rather than trusting an in-memory
// Manifest: the #6932 spike's bug (a manifest wiped by reconcileMembership)
// was invisible to a HIT/MISS matrix and visible only on disk.
func cpManifestFiles(t *testing.T, state string) map[string]diff.FileEntry {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(state, "file-index.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m struct {
		Files map[string]diff.FileEntry `json:"files"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	return m.Files
}

func cpManifestKeys(t *testing.T, state string) []string {
	t.Helper()
	var out []string
	for k := range cpManifestFiles(t, state) {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// cpNewTestPoller returns a poller wired to state, plus a pointer to the
// recorded sink submissions.
func cpNewTestPoller(t *testing.T, repo, state string) (*ChangePoller, *[]string) {
	t.Helper()
	var submits []string
	p := NewChangePoller(ChangePollerConfig{
		StateDir: func(string) string { return state },
	}, func(rp string, bulk bool) {
		submits = append(submits, rp)
	}, nil)
	if err := p.AddRepo(repo); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	t.Cleanup(func() { p.Stop() })
	return p, &submits
}

// cpCycle runs one poll cpCycle and returns the changed set for repo.
func cpCycle(t *testing.T, p *ChangePoller, repo string) []string {
	t.Helper()
	res := p.PollOnce()
	got := res[repo]
	sort.Strings(got)
	return got
}

func cpContains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// --- the manifest-population assertion ------------------------------------

// TestChangePoller_LeavesManifestPopulationIntact is the assertion #6932 asks
// for by name. A HIT/MISS matrix cannot see a manifest wipe; this reads
// file-index.json off disk before and after a run of detections and requires
// the key set AND every entry to be identical.
//
// The poller is a DETECTOR: it submits an index request through the
// EventSink(repo, bulk) contract the fsnotify watcher already satisfies, and
// it never writes the manifest itself. That is what this pins.
func TestChangePoller_LeavesManifestPopulationIntact(t *testing.T) {
	repo, state := cpNewRepo(t)
	cpWriteFile(t, repo, "sub/gamma.go", "package sub\n")
	cpIndexPass(t, repo, state)

	before := cpManifestFiles(t, state)
	if len(before) < 3 {
		t.Fatalf("fixture: manifest should hold >=3 files, got %d: %v", len(before), cpManifestKeys(t, state))
	}

	p, submits := cpNewTestPoller(t, repo, state)

	// Drive several detections without any index pass in between.
	cpWriteFile(t, repo, "alpha.go", "package a\n\nfunc Alpha() { println(1) }\n")
	_ = cpCycle(t, p, repo)
	cpWriteFile(t, repo, "untracked.go", "package a\n")
	_ = cpCycle(t, p, repo)
	cpWriteFile(t, repo, "alpha.go", "package a\n\nfunc Alpha() { println(2) }\n")
	_ = cpCycle(t, p, repo)

	if len(*submits) != 3 {
		t.Fatalf("expected 3 sink submissions, got %d", len(*submits))
	}

	after := cpManifestFiles(t, state)
	if len(after) != len(before) {
		t.Fatalf("manifest POPULATION changed: %d -> %d\nbefore=%v\nafter=%v",
			len(before), len(after), cpManifestKeys(t, state), cpManifestKeys(t, state))
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("manifest CONTENT changed.\nbefore=%#v\nafter=%#v", before, after)
	}
}

// --- the four driven cases -------------------------------------------------

// Case 1 — an untracked file edited a SECOND time. Kills git-status-only
// polling: `git status` reports `?? untracked.go` identically both times, so
// the report alone carries no "changed since last poll" information. The
// change decision comes from the manifest stamp.
func TestChangePoller_UntrackedFileEditedTwice(t *testing.T) {
	repo, state := cpNewRepo(t)
	cpIndexPass(t, repo, state)
	p, _ := cpNewTestPoller(t, repo, state)

	cpWriteFile(t, repo, "untracked.go", "package a\n// v1\n")
	if got := cpCycle(t, p, repo); !cpContains(got, "untracked.go") {
		t.Fatalf("first edit not detected: %v", got)
	}
	cpIndexPass(t, repo, state)
	if got := cpCycle(t, p, repo); len(got) != 0 {
		t.Fatalf("after index pass the repo must be quiescent, got %v", got)
	}

	// Second edit — git's report is byte-identical to the first.
	porcelain := cpGitRun(t, repo, "status", "--porcelain", "-unormal")
	cpWriteFile(t, repo, "untracked.go", "package a\n// v2\n")
	porcelain2 := cpGitRun(t, repo, "status", "--porcelain", "-unormal")
	if porcelain != porcelain2 {
		t.Fatalf("fixture premise broken: git's report differed between the two edits\n1=%q\n2=%q", porcelain, porcelain2)
	}

	if got := cpCycle(t, p, repo); !cpContains(got, "untracked.go") {
		t.Fatalf("SECOND edit of an untracked file not detected: %v", got)
	}
}

// Case 2 — edit, index, then revert to byte-identical HEAD content. Kills
// hybrid A (`git status -uall` for candidates): after the revert git reports
// the tree CLEAN, so a git-derived candidate set is empty and the file is
// never re-examined, while the manifest still holds the edited SHA — a
// permanent, silent divergence. The manifest-key sweep is what converges it.
func TestChangePoller_EditThenRevertConverges(t *testing.T) {
	repo, state := cpNewRepo(t)
	cpIndexPass(t, repo, state)
	p, _ := cpNewTestPoller(t, repo, state)

	orig, err := os.ReadFile(filepath.Join(repo, "alpha.go"))
	if err != nil {
		t.Fatal(err)
	}
	cpWriteFile(t, repo, "alpha.go", "package a\n\nfunc Alpha() { println(1) }\n")
	if got := cpCycle(t, p, repo); !cpContains(got, "alpha.go") {
		t.Fatalf("edit not detected: %v", got)
	}
	// The index pass runs on the EDITED bytes; the manifest now records them.
	cpIndexPass(t, repo, state)

	// Revert to the exact HEAD bytes.
	if err := os.WriteFile(filepath.Join(repo, "alpha.go"), orig, 0o644); err != nil {
		t.Fatal(err)
	}
	// Premise: git now considers the working tree clean, so a git-only
	// candidate set is empty.
	if out := strings.TrimSpace(cpGitRun(t, repo, "status", "--porcelain", "-unormal")); out != "" {
		t.Fatalf("fixture premise broken: git still reports the revert: %q", out)
	}
	if got := cpCycle(t, p, repo); !cpContains(got, "alpha.go") {
		t.Fatalf("edit-then-revert NOT detected — this is the hybrid-A permanent corruption: %v", got)
	}
}

// Case 3 — the same tracked file modified twice, with an index pass between.
func TestChangePoller_TrackedFileModifiedTwice(t *testing.T) {
	repo, state := cpNewRepo(t)
	cpIndexPass(t, repo, state)
	p, _ := cpNewTestPoller(t, repo, state)

	cpWriteFile(t, repo, "beta.go", "package a\n\nfunc Beta() { println(1) }\n")
	if got := cpCycle(t, p, repo); !cpContains(got, "beta.go") {
		t.Fatalf("first modification not detected: %v", got)
	}
	cpIndexPass(t, repo, state)

	cpWriteFile(t, repo, "beta.go", "package a\n\nfunc Beta() { println(2) }\n")
	if got := cpCycle(t, p, repo); !cpContains(got, "beta.go") {
		t.Fatalf("SECOND modification not detected: %v", got)
	}
}

// Case 4 — a same-SIZE content edit. The only stat-level signal is mtime.
//
// ASSUMPTION, stated per #6932: the filesystem's mtime granularity is finer
// than the interval between the index pass and the edit. On APFS/ext4
// (nanosecond stamps) this holds. On a coarse-granularity filesystem (HFS+ at
// 1 s, some network mounts, some container overlay configurations) a same-size
// edit landing inside the same mtime tick is invisible to diff.Filter's fast
// path and WOULD be missed — by the poller AND by every other grafel path that
// uses diff.Filter, including the fsnotify one. This is a property of
// diff.isChanged, not of the poller; the poller inherits it exactly.
func TestChangePoller_SameSizeEdit(t *testing.T) {
	repo, state := cpNewRepo(t)
	cpIndexPass(t, repo, state)
	p, _ := cpNewTestPoller(t, repo, state)

	before, err := os.Stat(filepath.Join(repo, "alpha.go"))
	if err != nil {
		t.Fatal(err)
	}
	// Same byte length, different content: "Alpha" -> "Alphb".
	cpWriteFile(t, repo, "alpha.go", "package a\n\nfunc Alphb() {}\n")
	after, err := os.Stat(filepath.Join(repo, "alpha.go"))
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() != after.Size() {
		t.Fatalf("fixture premise broken: size changed %d -> %d", before.Size(), after.Size())
	}
	if before.ModTime().UnixNano() == after.ModTime().UnixNano() {
		t.Skip("filesystem mtime granularity too coarse to distinguish this edit — see the doc comment")
	}
	if got := cpCycle(t, p, repo); !cpContains(got, "alpha.go") {
		t.Fatalf("same-size edit not detected: %v", got)
	}
}

// --- discovery half --------------------------------------------------------

// An untracked SUBTREE is collapsed by `git status -unormal` to a single
// `dir/` line. The poller must expand it via walk.WalkRepo rooted at the
// subtree, or every file inside a new package is invisible.
func TestChangePoller_UntrackedSubtreeIsWalked(t *testing.T) {
	repo, state := cpNewRepo(t)
	cpIndexPass(t, repo, state)
	p, _ := cpNewTestPoller(t, repo, state)

	cpWriteFile(t, repo, "newpkg/one.go", "package newpkg\n")
	cpWriteFile(t, repo, "newpkg/deep/two.go", "package deep\n")

	// Premise: git collapses the subtree to one line.
	out := strings.TrimSpace(cpGitRun(t, repo, "status", "--porcelain", "-unormal"))
	if out != "?? newpkg/" {
		t.Fatalf("fixture premise broken: expected a single collapsed line, got %q", out)
	}

	got := cpCycle(t, p, repo)
	for _, want := range []string{"newpkg/one.go", "newpkg/deep/two.go"} {
		if !cpContains(got, want) {
			t.Fatalf("collapsed untracked subtree not expanded: missing %q in %v", want, got)
		}
	}
}

// walk.WalkRepo rooted BELOW the repo root was flagged in #6932 as unverified.
// The property that actually matters is not "which skip layers fire" in the
// abstract — it is AGREEMENT: the poller's subtree expansion must produce
// exactly the paths the indexer's own full-repo walk produces under that
// subtree. Any path the poller adds that the indexer will never stamp is a file
// that reads as new on every single cycle, i.e. a permanent reindex storm (the
// #5665 defect shape).
//
// This pins agreement rather than a hand-picked skip list, so it stays true
// when the walker's layers change (e.g. when #6931 makes .gitignore apply to
// FILES and not just directories — today it does not, and both sides are
// equally affected, which is the point).
func TestChangePoller_SubtreeWalkAgreesWithFullWalk(t *testing.T) {
	repo, state := cpNewRepo(t)
	cpIndexPass(t, repo, state)
	_, _ = cpNewTestPoller(t, repo, state)

	cpWriteFile(t, repo, "newpkg/keep.go", "package newpkg\n")
	cpWriteFile(t, repo, "newpkg/deep/two.go", "package deep\n")
	cpWriteFile(t, repo, "newpkg/node_modules/junk.go", "package junk\n")
	cpWriteFile(t, repo, "newpkg/.gitignore", "ignored.go\n")
	cpWriteFile(t, repo, "newpkg/ignored.go", "package newpkg\n")

	// What the indexer's own full-repo walk yields under newpkg/.
	full, _, err := walk.WalkRepo(repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	var wantUnderSubtree []string
	for _, f := range full {
		if strings.HasPrefix(f, "newpkg/") {
			wantUnderSubtree = append(wantUnderSubtree, f)
		}
	}
	sort.Strings(wantUnderSubtree)
	if len(wantUnderSubtree) == 0 {
		t.Fatal("fixture premise broken: the full walk sees nothing under newpkg/")
	}
	if cpContains(wantUnderSubtree, "newpkg/node_modules/junk.go") {
		t.Fatal("fixture premise broken: the full walk did not skip node_modules")
	}

	got := walkUntrackedSubtree(repo, "newpkg/")
	sort.Strings(got)
	if !reflect.DeepEqual(got, wantUnderSubtree) {
		t.Fatalf("subtree expansion disagrees with the full walk\n subtree=%v\n    full=%v", got, wantUnderSubtree)
	}
}

// The consequence of that agreement, observed end-to-end: once the untracked
// subtree has been indexed the poller goes quiet. A subtree expansion that
// over-included even one path the indexer never stamps would re-fire forever.
func TestChangePoller_ConvergesAfterUntrackedSubtreeIndexed(t *testing.T) {
	repo, state := cpNewRepo(t)
	cpIndexPass(t, repo, state)
	p, submits := cpNewTestPoller(t, repo, state)

	cpWriteFile(t, repo, "newpkg/keep.go", "package newpkg\n")
	cpWriteFile(t, repo, "newpkg/deep/two.go", "package deep\n")
	cpWriteFile(t, repo, "newpkg/node_modules/junk.go", "package junk\n")
	cpWriteFile(t, repo, "newpkg/.gitignore", "ignored.go\n")
	cpWriteFile(t, repo, "newpkg/ignored.go", "package newpkg\n")

	if got := cpCycle(t, p, repo); len(got) == 0 {
		t.Fatal("new untracked subtree not detected")
	}
	cpIndexPass(t, repo, state)
	if got := cpCycle(t, p, repo); len(got) != 0 {
		t.Fatalf("poller did NOT converge after the subtree was indexed — it would re-fire forever: %v", got)
	}
	if n := len(*submits); n != 1 {
		t.Fatalf("expected exactly 1 submission, got %d", n)
	}
}

// --- the reverse direction: no over-firing ---------------------------------

func TestChangePoller_QuiescentRepoDoesNotSubmit(t *testing.T) {
	repo, state := cpNewRepo(t)
	cpIndexPass(t, repo, state)
	p, submits := cpNewTestPoller(t, repo, state)

	for i := 0; i < 3; i++ {
		if got := cpCycle(t, p, repo); len(got) != 0 {
			t.Fatalf("cpCycle %d on a quiescent repo reported %v", i, got)
		}
	}
	if len(*submits) != 0 {
		t.Fatalf("quiescent repo submitted %d index requests", len(*submits))
	}
}

// A repo with NO manifest has no baseline to diff against; every file would
// read as new and the poller would ask for a reindex on every single cpCycle.
// It stands down instead and leaves initial indexing to the scheduler.
func TestChangePoller_NoManifestNoSubmit(t *testing.T) {
	repo, state := cpNewRepo(t)
	p, submits := cpNewTestPoller(t, repo, state)

	// The tree must be DIRTY, or the guard is never reached: with no manifest
	// the key-set half contributes nothing, so only git's report can supply a
	// candidate. Without the guard these two are enqueued on every cycle.
	cpWriteFile(t, repo, "untracked.go", "package a\n")
	cpWriteFile(t, repo, "alpha.go", "package a\n// edited\n")
	if out := strings.TrimSpace(cpGitRun(t, repo, "status", "--porcelain", "-unormal")); out == "" {
		t.Fatal("fixture premise broken: git reports a clean tree, so the guard is untested")
	}

	if got := cpCycle(t, p, repo); len(got) != 0 {
		t.Fatalf("un-indexed repo reported %v", got)
	}
	if len(*submits) != 0 {
		t.Fatalf("un-indexed repo submitted %d index requests", len(*submits))
	}
}

// Filter's cross-file basename invalidation must survive the candidate-set
// construction: editing alpha.go must also pull sub/alpha.py into the changed
// set, exactly as the full-walk path does.
func TestChangePoller_PreservesCrossFileInvalidation(t *testing.T) {
	repo, state := cpNewRepo(t)
	cpWriteFile(t, repo, "sub/alpha.py", "x = 1\n")
	cpGitRun(t, repo, "add", "-A")
	cpGitRun(t, repo, "commit", "-q", "-m", "add py")
	cpIndexPass(t, repo, state)
	p, _ := cpNewTestPoller(t, repo, state)

	cpWriteFile(t, repo, "alpha.go", "package a\n\nfunc Alpha() { println(1) }\n")
	got := cpCycle(t, p, repo)
	if !cpContains(got, "sub/alpha.py") {
		t.Fatalf("cross-file basename invalidation lost: %v", got)
	}
}

// --- warm-up (arm D) -------------------------------------------------------

func TestChangePoller_WarmUpSetsUntrackedCache(t *testing.T) {
	repo, state := cpNewRepo(t)
	cpIndexPass(t, repo, state)
	cpNewTestPoller(t, repo, state)

	out := strings.TrimSpace(cpGitRun(t, repo, "config", "--local", "core.untrackedCache"))
	if out != "true" {
		t.Fatalf("AddRepo did not enable core.untrackedCache (arm D): got %q", out)
	}
}

// --- the loop --------------------------------------------------------------

func TestChangePoller_LoopSubmitsOnInterval(t *testing.T) {
	repo, state := cpNewRepo(t)
	cpIndexPass(t, repo, state)

	got := make(chan string, 8)
	p := NewChangePoller(ChangePollerConfig{
		Interval: 10 * time.Millisecond,
		StateDir: func(string) string { return state },
	}, func(rp string, bulk bool) {
		select {
		case got <- rp:
		default:
		}
	}, nil)
	if err := p.AddRepo(repo); err != nil {
		t.Fatal(err)
	}
	cpWriteFile(t, repo, "alpha.go", "package a\n\nfunc Alpha() { println(1) }\n")
	p.Start()
	defer p.Stop()

	select {
	case r := <-got:
		if r != repo {
			t.Fatalf("sink got %q want %q", r, repo)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("poll loop never submitted")
	}
}

// bulk=true asks the sink for a repo-level reindex instead of a file-level
// diff, exactly as the fsnotify watcher's bulk trigger does. The boundary is
// "at or above the threshold", which is what the fsnotify path means by it too.
func TestChangePoller_BulkThresholdBoundary(t *testing.T) {
	run := func(t *testing.T, nFiles, threshold int) bool {
		t.Helper()
		repo, state := cpNewRepo(t)
		cpIndexPass(t, repo, state)
		var bulks []bool
		p := NewChangePoller(ChangePollerConfig{
			BulkThreshold: threshold,
			StateDir:      func(string) string { return state },
			DisableWarmUp: true,
		}, func(_ string, bulk bool) { bulks = append(bulks, bulk) }, nil)
		if err := p.AddRepo(repo); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < nFiles; i++ {
			cpWriteFile(t, repo, "new"+string(rune('a'+i))+".go", "package a\n")
		}
		got := cpCycle(t, p, repo)
		if len(got) != nFiles {
			t.Fatalf("fixture premise broken: %d changed files, want %d: %v", len(got), nFiles, got)
		}
		if len(bulks) != 1 {
			t.Fatalf("expected one submission, got %d", len(bulks))
		}
		return bulks[0]
	}
	if run(t, 2, 3) {
		t.Fatal("2 changed files under a threshold of 3 must not be bulk")
	}
	if !run(t, 3, 3) {
		t.Fatal("3 changed files at a threshold of 3 must be bulk")
	}
}

// DisableWarmUp is the control for the warm-up test: it must actually suppress
// the git config write, or TestChangePoller_WarmUpSetsUntrackedCache proves
// nothing about AddRepo doing it.
func TestChangePoller_DisableWarmUpSuppressesIt(t *testing.T) {
	repo, state := cpNewRepo(t)
	cpIndexPass(t, repo, state)
	p := NewChangePoller(ChangePollerConfig{
		StateDir:      func(string) string { return state },
		DisableWarmUp: true,
	}, func(string, bool) {}, nil)
	if err := p.AddRepo(repo); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "config", "--local", "core.untrackedCache")
	cmd.Dir = repo
	out, _ := cmd.CombinedOutput()
	if strings.TrimSpace(string(out)) == "true" {
		t.Fatal("DisableWarmUp did not suppress the warm-up — the warm-up test is vacuous")
	}
}

// Porcelain v1 QUOTES and C-escapes any path with a space or a non-ASCII byte
// unless -z is used. A mis-unquoted path is a file that is silently never
// re-indexed, so the -z choice in gitStatusDiscovery is load-bearing.
func TestChangePoller_DiscoversPathsNeedingQuoting(t *testing.T) {
	repo, state := cpNewRepo(t)
	cpIndexPass(t, repo, state)
	p, _ := cpNewTestPoller(t, repo, state)

	cpWriteFile(t, repo, "has space.go", "package a\n")
	cpWriteFile(t, repo, "ünïcode.go", "package a\n")

	// Premise: without -z git would quote both of these.
	quoted := cpGitRun(t, repo, "status", "--porcelain", "-unormal")
	if !strings.Contains(quoted, `"`) {
		t.Fatalf("fixture premise broken: git did not quote anything: %q", quoted)
	}

	got := cpCycle(t, p, repo)
	for _, want := range []string{"has space.go", "ünïcode.go"} {
		if !cpContains(got, want) {
			t.Fatalf("path needing quoting was not discovered: missing %q in %v", want, got)
		}
	}
}
