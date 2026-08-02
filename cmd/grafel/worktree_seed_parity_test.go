package main

// Parity gate for #5964 worktree graph seeding.
//
// Seeding is only worth having if the graph it produces is the graph a full
// index would have produced. Counts alone cannot show that — a bug that loses
// N edges and invents N others passes any count assertion, and #6085 was
// itself a count bug. So this compares SEMANTIC DIGESTS: the full sets of
// entity and relationship identities, diffed both ways.
//
// It also cannot compare bytes. Per #6083 a whole-file cmp on graph.fb is
// invalid: one length change cascades through every FlatBuffers offset, so two
// runs of the same binary can differ by tens of megabytes while being
// semantically identical.
//
// The fixture is deliberately tiny (a couple of dozen small Go files) and runs
// in-process. Repeatedly indexing a large corpus to test this is what killed
// two earlier attempts on #6085.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/graph"
)

const parityRepoTag = "fixture-svc"

// fixtureFileCount is small on purpose — big enough that the delta is a small
// fraction of the graph, small enough that a run costs a second or two.
const fixtureFileCount = 24

func seedGitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

// writeSeedFixtureRepo lays down a small multi-file Go service and commits it on
// the given branch.
func writeSeedFixtureRepo(t *testing.T, dir, branch string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "svc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixture\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < fixtureFileCount; i++ {
		src := fmt.Sprintf(`package svc

// Service%[1]d is a fixture type.
type Service%[1]d struct {
	Name string
}

// Handle%[1]d does the work for service %[1]d.
func (s *Service%[1]d) Handle%[1]d(in string) string {
	return s.helper%[1]d(in)
}

func (s *Service%[1]d) helper%[1]d(in string) string {
	return in + s.Name
}

// Dispatch%[1]d calls into the previous service, giving the graph cross-file
// CALLS edges so the parity check exercises edge carry-forward, not just
// entity carry-forward.
func Dispatch%[1]d(in string) string {
	prev := &Service%[2]d{Name: "p"}
	return prev.Handle%[2]d(in)
}
`, i, (i+fixtureFileCount-1)%fixtureFileCount)
		if err := os.WriteFile(filepath.Join(dir, "svc", fmt.Sprintf("svc%02d.go", i)), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	seedGitRun(t, dir, "init", "-q", "-b", branch)
	seedGitRun(t, dir, "add", "-A")
	seedGitRun(t, dir, "commit", "-q", "-m", "fixture")
}

// addFixtureWorktree creates a REAL linked git worktree of parentPath on a new
// branch. Using two independent `git init` repos instead would be a fixture
// that cannot fail the way production does: the parent's indexed commit would
// be unreachable from the child, so the incremental pass's commit-range diff
// reports "head-advance-unconfirmed" and falls back before the seed is ever
// exercised. A worktree shares the parent's object store, which is the whole
// premise of #5954.
func addFixtureWorktree(t *testing.T, parentPath, childPath, branch string) {
	t.Helper()
	seedGitRun(t, parentPath, "worktree", "add", "-q", "-b", branch, childPath)
}

// applyChildDelta makes the child tree differ from the parent in the three
// ways an agent's worktree actually differs: a committed change, an
// uncommitted working-tree edit, and an untracked new file. The last two are
// exactly what a `git diff <parentRef>..<childRef>` would MISS, and they are
// the most common thing an agent asks about — its own in-progress work.
func applyChildDelta(t *testing.T, dir string) {
	t.Helper()
	// (a) committed change
	committed := filepath.Join(dir, "svc", "svc00.go")
	body, err := os.ReadFile(committed)
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, []byte("\n// CommittedOnChild is new on the child branch.\nfunc CommittedOnChild(in string) string { return in }\n")...)
	if err := os.WriteFile(committed, body, 0o644); err != nil {
		t.Fatal(err)
	}
	seedGitRun(t, dir, "add", "-A")
	seedGitRun(t, dir, "commit", "-q", "-m", "child commit")

	// (b) uncommitted working-tree edit
	dirty := filepath.Join(dir, "svc", "svc01.go")
	dbody, err := os.ReadFile(dirty)
	if err != nil {
		t.Fatal(err)
	}
	dbody = append(dbody, []byte("\n// UncommittedOnChild is an in-progress edit.\nfunc UncommittedOnChild(in string) string { return in }\n")...)
	if err := os.WriteFile(dirty, dbody, 0o644); err != nil {
		t.Fatal(err)
	}

	// (c) untracked new file
	untracked := `package svc

// UntrackedHelper lives in a file git has never seen.
func UntrackedHelper(in string) string { return in + "!" }
`
	if err := os.WriteFile(filepath.Join(dir, "svc", "untracked.go"), []byte(untracked), 0o644); err != nil {
		t.Fatal(err)
	}
}

// semanticDigest reduces a graph to the two identity sets that matter. Entity
// rows carry id AND (kind, name, source_file) so a bug that keeps the id but
// moves the entity, or keeps the shape but changes the id, both show up.
type semanticDigest struct {
	entities map[string]bool
	rels     map[string]bool
}

func digestOf(t *testing.T, stateDir string) semanticDigest {
	t.Helper()
	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil || doc == nil {
		t.Fatalf("load graph from %s: %v", stateDir, err)
	}
	d := semanticDigest{
		entities: make(map[string]bool, len(doc.Entities)),
		rels:     make(map[string]bool, len(doc.Relationships)),
	}
	for i := range doc.Entities {
		e := &doc.Entities[i]
		d.entities[strings.Join([]string{e.ID, e.Kind, e.Name, filepath.ToSlash(e.SourceFile)}, "|")] = true
	}
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		d.rels[strings.Join([]string{r.FromID, r.ToID, r.Kind}, "|")] = true
	}
	return d
}

// seedDiffSets returns up to `limit` members of a\b and b\a, sorted, plus the full
// counts — so a failure names WHICH rows were lost or invented rather than
// only how many.
func seedDiffSets(a, b map[string]bool, limit int) (onlyA, onlyB []string, nA, nB int) {
	for k := range a {
		if !b[k] {
			nA++
			if len(onlyA) < limit {
				onlyA = append(onlyA, k)
			}
		}
	}
	for k := range b {
		if !a[k] {
			nB++
			if len(onlyB) < limit {
				onlyB = append(onlyB, k)
			}
		}
	}
	sort.Strings(onlyA)
	sort.Strings(onlyB)
	return
}

func assertParity(t *testing.T, want, got semanticDigest, label string) {
	t.Helper()
	lost, invented, nLost, nInv := seedDiffSets(want.entities, got.entities, 8)
	if nLost != 0 || nInv != 0 {
		t.Errorf("%s: entity parity broken — %d lost, %d invented\n  lost: %v\n  invented: %v",
			label, nLost, nInv, lost, invented)
	}
	lost, invented, nLost, nInv = seedDiffSets(want.rels, got.rels, 8)
	if nLost != 0 || nInv != 0 {
		t.Errorf("%s: relationship parity broken — %d lost, %d invented\n  lost: %v\n  invented: %v",
			label, nLost, nInv, lost, invented)
	}
}

// TestWorktreeSeedParity_SeededGraphMatchesAFullIndex is the correctness gate.
// It indexes the SAME child tree twice — once from scratch, once seeded from
// the parent ref's graph plus an incremental pass over the delta — and asserts
// the two graphs are semantically identical.
func TestWorktreeSeedParity_SeededGraphMatchesAFullIndex(t *testing.T) {
	if testing.Short() {
		t.Skip("indexes a fixture repo twice; skipped under -short")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemonroot"))

	// --- parent, on a ref that is deliberately NOT "main" ---
	parentPath := filepath.Join(root, "parent")
	const parentRef = "release/2026-07"
	writeSeedFixtureRepo(t, parentPath, parentRef)
	parentSD := daemon.StateDirForRepo(parentPath)
	if err := Index(parentPath, "", parityRepoTag, []string{"graph-algo"}, false, false,
		WithIncremental(parentSD)); err != nil {
		t.Fatalf("index parent: %v", err)
	}
	if desc, _ := graph.CurrentGraphDescriptor(parentSD); desc.Kind == graph.GraphAbsent {
		t.Fatalf("parent state dir %s has no graph after indexing", parentSD)
	}

	// --- child worktree: same tree, plus a committed / uncommitted /
	//     untracked delta ---
	childPath := filepath.Join(root, "wt-agent-a")
	const childRef = "feat/agent-a"
	addFixtureWorktree(t, parentPath, childPath, childRef)
	applyChildDelta(t, childPath)
	childSD := daemon.StateDirForRepo(childPath)

	// --- PASS 1: from scratch, no seed. Pinned to the parent's tag, exactly
	//     as daemonSchedulerIndex does via the repo-tag pin. ---
	if err := Index(childPath, "", parityRepoTag, []string{"graph-algo"}, false, false); err != nil {
		t.Fatalf("scratch index child: %v", err)
	}
	scratch := digestOf(t, childSD)
	if len(scratch.entities) == 0 {
		t.Fatal("scratch index produced no entities — the fixture is inert")
	}

	// --- PASS 2: wipe, seed from the parent ref, index the delta only. ---
	if err := os.RemoveAll(childSD); err != nil {
		t.Fatal(err)
	}
	out := daemon.SeedWorktreeGraph(daemon.SeedRequest{
		ParentPath: parentPath,
		ParentRef:  parentRef,
		ChildPath:  childPath,
		ChildRef:   childRef,
		RepoTag:    parityRepoTag,
	})
	if !out.Seeded {
		t.Fatalf("seed failed: %s — %s", out.Reason, out.Detail)
	}
	stamp, reason, err := daemon.VerifySeededGraph(childSD)
	if err != nil || reason != "" {
		t.Fatalf("seed verification: reason=%q err=%v", reason, err)
	}
	if stamp.RepoTag != parityRepoTag {
		t.Fatalf("stamp RepoTag=%q want %q", stamp.RepoTag, parityRepoTag)
	}
	if err := Index(childPath, "", stamp.RepoTag, []string{"graph-algo"}, false, false,
		WithIncremental(childSD)); err != nil {
		t.Fatalf("seeded incremental index child: %v", err)
	}
	seeded := digestOf(t, childSD)

	assertParity(t, scratch, seeded, "seeded-vs-scratch")

	// The delta must actually be in the graph: if the seeded pass silently
	// skipped the child's own work, parity against a broken scratch index
	// could still pass. Assert the three delta shapes by name.
	for _, name := range []string{"CommittedOnChild", "UncommittedOnChild", "UntrackedHelper"} {
		if !digestHasName(seeded, name) {
			t.Errorf("seeded graph is missing %q — an agent would be served stale results for its own work", name)
		}
		if !digestHasName(scratch, name) {
			t.Errorf("scratch graph is missing %q — the fixture never exercised this delta shape", name)
		}
	}
}

// TestWorktreeSeedConsumedByTheDaemonPath covers the path the daemon actually
// takes for a seeded worktree: the scheduler calls extractors.TryIncremental
// before it ever calls Index. The parity gate above exercises
// Index()+WithIncremental (Path B); this asserts the SHIPPING path (Path A)
// consumes the same seed, indexes only the delta, and carries the child's own
// work into the graph.
//
// It deliberately does NOT assert full parity against a scratch index: Path A
// is an in-place patch that re-runs a reduced pass set, so it is not expected
// to be byte- or set-identical to a full pipeline run. What it must prove is
// that the seed is consumable and that the delta is not lost.
func TestWorktreeSeedConsumedByTheDaemonPath(t *testing.T) {
	if testing.Short() {
		t.Skip("indexes a fixture repo; skipped under -short")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemonroot"))

	parentPath := filepath.Join(root, "parent")
	const parentRef = "release/2026-07"
	writeSeedFixtureRepo(t, parentPath, parentRef)
	parentSD := daemon.StateDirForRepo(parentPath)
	if err := Index(parentPath, "", parityRepoTag, []string{"graph-algo"}, false, false,
		WithIncremental(parentSD)); err != nil {
		t.Fatalf("index parent: %v", err)
	}

	childPath := filepath.Join(root, "wt-agent-b")
	const childRef = "feat/agent-b"
	addFixtureWorktree(t, parentPath, childPath, childRef)
	applyChildDelta(t, childPath)
	childSD := daemon.StateDirForRepo(childPath)

	out := daemon.SeedWorktreeGraph(daemon.SeedRequest{
		ParentPath: parentPath,
		ParentRef:  parentRef,
		ChildPath:  childPath,
		ChildRef:   childRef,
		RepoTag:    parityRepoTag,
	})
	if !out.Seeded {
		t.Fatalf("seed failed: %s — %s", out.Reason, out.Detail)
	}
	if _, reason, err := daemon.VerifySeededGraph(childSD); err != nil || reason != "" {
		t.Fatalf("verification: reason=%q err=%v", reason, err)
	}

	res := extractors.TryIncremental(context.Background(), childPath, childSD, nil, nil)
	if !res.Done {
		t.Fatalf("TryIncremental fell back (%q) on a freshly seeded state dir — the seed is not consumable by the shipping path", res.FallbackReason)
	}
	// O(delta), not O(corpus): only the child's own changed files were
	// re-extracted, out of a fixture of fixtureFileCount+1 source files.
	if res.ChangedFiles == 0 || res.ChangedFiles > 6 {
		t.Errorf("ChangedFiles=%d — expected the small delta, not the whole corpus of %d files", res.ChangedFiles, fixtureFileCount+1)
	}
	t.Logf("seeded daemon-path pass re-extracted %d of %d source files", res.ChangedFiles, fixtureFileCount+1)
	got := digestOf(t, childSD)
	for _, name := range []string{"CommittedOnChild", "UncommittedOnChild", "UntrackedHelper"} {
		if !digestHasName(got, name) {
			t.Errorf("graph is missing %q after the seeded daemon-path pass", name)
		}
	}
}

// TestWorktreeSeedParity_PathAOnASeededBaseMatchesPathAOnASelfBuiltBase is the
// parity gate for the path users actually take.
//
// A naive "Path A seeded vs full scratch index" compare would be noisy: Path A
// is an in-place patch that re-runs a reduced pass set, so its output is not
// expected to equal a full pipeline run. That noise is cancelled by running
// Path A on BOTH sides and varying only the thing under test — where the base
// graph came from:
//
//	X: worktree indexed from scratch at parent content, then the delta applied,
//	   then Path A  -> base graph built by this worktree itself
//	Y: worktree SEEDED from the parent, same delta, then Path A
//	   -> base graph byte-copied from the parent
//
// Both worktrees hold identical content and are pinned to the same repo tag, so
// their entity ids coincide. Any difference in the resulting sets is
// attributable to seeding — including a merge that drops carry-forward edges,
// which is what the entity-only assertions elsewhere cannot see.
func TestWorktreeSeedParity_PathAOnASeededBaseMatchesPathAOnASelfBuiltBase(t *testing.T) {
	if testing.Short() {
		t.Skip("indexes fixture repos; skipped under -short")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemonroot"))

	parentPath := filepath.Join(root, "parent")
	const parentRef = "release/2026-07"
	writeSeedFixtureRepo(t, parentPath, parentRef)
	parentSD := daemon.StateDirForRepo(parentPath)
	if err := Index(parentPath, "", parityRepoTag, []string{"graph-algo"}, false, false,
		WithIncremental(parentSD)); err != nil {
		t.Fatalf("index parent: %v", err)
	}

	// --- X: self-built base ---
	xPath := filepath.Join(root, "wt-selfbuilt")
	addFixtureWorktree(t, parentPath, xPath, "feat/selfbuilt")
	xSD := daemon.StateDirForRepo(xPath)
	if err := Index(xPath, "", parityRepoTag, []string{"graph-algo"}, false, false,
		WithIncremental(xSD)); err != nil {
		t.Fatalf("index worktree X at parent content: %v", err)
	}
	applyChildDelta(t, xPath)
	resX := extractors.TryIncremental(context.Background(), xPath, xSD, nil, nil)
	if !resX.Done {
		t.Fatalf("Path A fell back on the self-built base (%q) — the control arm did not run", resX.FallbackReason)
	}
	digX := digestOf(t, xSD)

	// --- Y: seeded base ---
	yPath := filepath.Join(root, "wt-seeded")
	addFixtureWorktree(t, parentPath, yPath, "feat/seeded")
	applyChildDelta(t, yPath)
	ySD := daemon.StateDirForRepo(yPath)
	out := daemon.SeedWorktreeGraph(daemon.SeedRequest{
		ParentPath: parentPath, ParentRef: parentRef,
		ChildPath: yPath, ChildRef: "feat/seeded", RepoTag: parityRepoTag,
	})
	if !out.Seeded {
		t.Fatalf("seed failed: %s — %s", out.Reason, out.Detail)
	}
	if _, reason, err := daemon.VerifySeededGraph(ySD); err != nil || reason != "" {
		t.Fatalf("verify: reason=%q err=%v", reason, err)
	}
	resY := extractors.TryIncremental(context.Background(), yPath, ySD, nil, nil)
	if !resY.Done {
		t.Fatalf("Path A fell back on the seeded base (%q)", resY.FallbackReason)
	}
	digY := digestOf(t, ySD)

	if len(digX.entities) == 0 || len(digX.rels) == 0 {
		t.Fatal("control arm produced an empty graph — the fixture proves nothing")
	}
	t.Logf("Path A control: %d entities / %d rels (changed=%d); seeded: %d entities / %d rels (changed=%d)",
		len(digX.entities), len(digX.rels), resX.ChangedFiles,
		len(digY.entities), len(digY.rels), resY.ChangedFiles)

	assertParity(t, digX, digY, "pathA-seeded-vs-pathA-selfbuilt")
}

func digestHasName(d semanticDigest, name string) bool {
	for k := range d.entities {
		parts := strings.Split(k, "|")
		if len(parts) >= 3 && parts[2] == name {
			return true
		}
	}
	return false
}
