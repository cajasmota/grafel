package links

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/cajasmota/grafel/internal/safeio"
	"github.com/cajasmota/grafel/internal/types"
)

// reachabilityFixture builds the one-repo fixture both directions of the
// #6839 test share: an http_endpoint_definition seed that CALLS a handler,
// a CLI main sniffed out of the source file, and an orphan that nothing
// reaches. Only the on-disk state of src/handler.go differs between the two
// directions.
func reachabilityFixture(repo, root string) repoGraph {
	return repoGraph{
		Repo:     repo,
		FileRoot: root,
		Entities: []entityNode{
			{ID: "ep1", Name: "GET /x", Kind: "http_endpoint_definition", SourceFile: "src/handler.go"},
			{ID: "handlerFn", Name: "HandleX", Kind: "SCOPE.Function", SourceFile: "src/handler.go"},
			{ID: "mainFn", Name: "main", Kind: "SCOPE.Function", SourceFile: "src/handler.go"},
			{ID: "orphanFn", Name: "orphan", Kind: "SCOPE.Function", SourceFile: "src/handler.go"},
		},
		Edges: []edgeRef{
			{FromID: "ep1", ToID: "handlerFn", Kind: "CALLS"},
		},
	}
}

const reachabilityFixtureSrc = `package h

func main() {}

func HandleX() {}

func orphan() {}
`

// plantUnreadableSource makes <root>/src/handler.go unreadable BY
// CONSTRUCTION: it creates a DIRECTORY at the path the pass will read.
// safeio.Open stats before opening and refuses any non-regular file with
// ErrNotRegular, so readSourceFile fails without a FIFO, a socket, or any
// dependence on file permissions (which do not stop root). Everything stays
// inside t.TempDir().
func plantUnreadableSource(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "src", "handler.go"), 0o755); err != nil {
		t.Fatalf("plant unreadable source: %v", err)
	}
}

func readReachabilityDoc(t *testing.T, links string) reachabilityDocument {
	t.Helper()
	buf, err := os.ReadFile(strings.TrimSuffix(links, ".json") + "-reachability.json")
	if err != nil {
		t.Fatalf("sidecar: %v", err)
	}
	var doc reachabilityDocument
	if err := json.Unmarshal(buf, &doc); err != nil {
		t.Fatalf("unmarshal sidecar: %v", err)
	}
	return doc
}

func propOfKey(g repoGraph, id, key string) string {
	for _, e := range g.Entities {
		if e.ID == id {
			return e.Properties.Get(key)
		}
	}
	return "<missing entity>"
}

func propOf(g repoGraph, id string) string {
	for _, e := range g.Entities {
		if e.ID == id {
			return e.Properties.Get("reachable")
		}
	}
	return "<missing entity>"
}

// TestReachabilityUnreadableEntryPointFileDoesNotClaimDead is the #6839
// regression pin. When an entry-point-bearing source file cannot be read,
// the pass loses that file's sniffed seeds — so it cannot know what those
// seeds would have reached. It must therefore NOT stamp reachable="false",
// which is a positive false claim consumed by grafel_dead_code as "this
// code is dead".
//
// The assertion is on the EMITTED ARTEFACT — the property written onto the
// entity and the sidecar row — not on a counter the pass keeps about itself.
func TestReachabilityUnreadableEntryPointFileDoesNotClaimDead(t *testing.T) {
	root := t.TempDir()
	plantUnreadableSource(t, root)

	g := reachabilityFixture("repo-a", root)
	// A stale stamp from an earlier, non-degraded run must not survive
	// either: the pass now knows it cannot substantiate it.
	for i := range g.Entities {
		if g.Entities[i].ID == "orphanFn" {
			g.Entities[i].Properties = types.Props{}
			g.Entities[i].Properties.Set("reachable", "true")
			g.Entities[i].Properties.Set("reachable_via", "sniff:cli_main:main")
		}
	}
	graphs := []repoGraph{g}

	paths := Paths{Links: filepath.Join(t.TempDir(), "g-links.json")}
	res, err := runReachabilityPass("g", graphs, paths)
	if err != nil {
		t.Fatalf("runReachabilityPass: %v", err)
	}

	// 1. The false claim is gone from the in-memory entity.
	if got := propOf(graphs[0], "orphanFn"); got != "" {
		t.Errorf("orphanFn: reachability was NOT computed (entry-point file unreadable), "+
			"so reachable must be unstamped; got %q", got)
	}
	// 2. Proven-reachable entities are still stamped — a lost seed can only
	//    ADD reachability, never remove it, so "true" stays sound.
	if got := propOfKey(graphs[0], "orphanFn", "reachable_via"); got != "" {
		t.Errorf("orphanFn: a stale reachable_via must not outlive the verdict it "+
			"justified; got %q", got)
	}
	if got := propOf(graphs[0], "ep1"); got != "true" {
		t.Errorf("ep1 (graph-encoded seed) should still be reachable=true, got %q", got)
	}
	// 3. The sidecar — what grafel_dead_code actually reads — must carry no
	//    unreachable row for the degraded repo.
	doc := readReachabilityDoc(t, paths.Links)
	for _, e := range doc.Entries {
		if e.Repo == "repo-a" && !e.Reachable {
			t.Errorf("sidecar still reports %s (%s) as unreachable in a repo whose "+
				"reachability could not be computed", e.Name, e.EntityID)
		}
	}
	if doc.Unreachable != 0 {
		t.Errorf("doc.unreachable: want 0 (nothing was established dead), got %d", doc.Unreachable)
	}
	// 4. And it must be REPORTED, not silently dropped.
	if res.UnreadableSourceFiles != 1 {
		t.Errorf("PassResult.UnreadableSourceFiles: want 1, got %d", res.UnreadableSourceFiles)
	}
	if len(doc.DegradedRepos) != 1 || doc.DegradedRepos[0] != "repo-a" {
		t.Errorf("doc.degraded_repos: want [repo-a], got %v", doc.DegradedRepos)
	}
	if doc.UnreadableEntryPointFiles != 1 {
		t.Errorf("doc.unreadable_entry_point_files: want 1, got %d", doc.UnreadableEntryPointFiles)
	}
	// The counter has to reach the operator-facing stats sidecar under the
	// name it is documented with, not just live on the struct.
	statsPath := filepath.Join(t.TempDir(), "g-link-pass-stats.json")
	if err := writeLinkPassStats(statsPath, &RunResult{Group: "g", Results: []PassResult{res}}); err != nil {
		t.Fatalf("writeLinkPassStats: %v", err)
	}
	statsBuf, err := os.ReadFile(statsPath)
	if err != nil {
		t.Fatalf("stats sidecar: %v", err)
	}
	var stats struct {
		Passes []struct {
			Pass                  string `json:"pass"`
			UnreadableSourceFiles int    `json:"unreadable_source_files"`
		} `json:"passes"`
	}
	if err := json.Unmarshal(statsBuf, &stats); err != nil {
		t.Fatalf("unmarshal stats sidecar: %v", err)
	}
	if len(stats.Passes) != 1 || stats.Passes[0].UnreadableSourceFiles != 1 {
		t.Errorf("link-pass-stats unreadable_source_files: want 1 for the reachability pass, got %+v",
			stats.Passes)
	}
	if doc.Unknown == 0 {
		t.Errorf("doc.unknown: want >0 (entities whose reachability is undetermined), got 0")
	}
}

// TestReachabilityReadableEntryPointFileStillClaimsDead is the other
// direction. A fix that suppresses the stamp unconditionally passes the test
// above and destroys the feature; only this case catches it.
func TestReachabilityReadableEntryPointFileStillClaimsDead(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/handler.go", reachabilityFixtureSrc)

	graphs := []repoGraph{reachabilityFixture("repo-a", root)}
	paths := Paths{Links: filepath.Join(t.TempDir(), "g-links.json")}
	res, err := runReachabilityPass("g", graphs, paths)
	if err != nil {
		t.Fatalf("runReachabilityPass: %v", err)
	}

	if got := propOf(graphs[0], "orphanFn"); got != "false" {
		t.Errorf("orphanFn: the file WAS readable, so genuinely-unreached code must "+
			"still be reachable=false; got %q", got)
	}
	if got := propOf(graphs[0], "mainFn"); got != "true" {
		t.Errorf("mainFn (sniffed CLI entry) should be reachable=true, got %q", got)
	}
	if res.UnreadableSourceFiles != 0 {
		t.Errorf("PassResult.UnreadableSourceFiles: want 0, got %d", res.UnreadableSourceFiles)
	}

	doc := readReachabilityDoc(t, paths.Links)
	if len(doc.DegradedRepos) != 0 {
		t.Errorf("doc.degraded_repos: want empty, got %v", doc.DegradedRepos)
	}
	found := false
	for _, e := range doc.Entries {
		if e.Name == "orphan" && !e.Reachable {
			found = true
		}
	}
	if !found {
		t.Errorf("sidecar must still carry the orphan as unreachable when the source read succeeded")
	}
}

// TestReachabilityMissingSourceFileStillClaimsDead pins the other half of
// the predicate. A file that is NOT THERE is a known fact, not a failed
// read — nothing about it was hidden from the pass — and it is the common
// case (a graph indexed from a source tree that has since moved has every
// file in this arm). Degrading on fs.ErrNotExist would suppress the pass's
// entire dead-code output on that input.
func TestReachabilityMissingSourceFileStillClaimsDead(t *testing.T) {
	root := t.TempDir() // empty: <root>/src/handler.go does not exist

	graphs := []repoGraph{reachabilityFixture("repo-a", root)}
	paths := Paths{Links: filepath.Join(t.TempDir(), "g-links.json")}
	res, err := runReachabilityPass("g", graphs, paths)
	if err != nil {
		t.Fatalf("runReachabilityPass: %v", err)
	}
	if got := propOf(graphs[0], "orphanFn"); got != "false" {
		t.Errorf("orphanFn: an ABSENT source file is not a failed read; dead-code "+
			"marking must stay on. want %q, got %q", "false", got)
	}
	if res.UnreadableSourceFiles != 0 {
		t.Errorf("PassResult.UnreadableSourceFiles: want 0 for an absent file, got %d",
			res.UnreadableSourceFiles)
	}
	doc := readReachabilityDoc(t, paths.Links)
	if len(doc.DegradedRepos) != 0 {
		t.Errorf("doc.degraded_repos: want empty for an absent file, got %v", doc.DegradedRepos)
	}
	if doc.Unreachable == 0 {
		t.Errorf("doc.unreachable: want >0, got 0")
	}
}

// TestReachabilityDegradationIsScopedToItsOwnRepo pins the SCOPE of the
// suppression. The BFS adjacency is built per repo graph, so a lost seed in
// repo-a cannot affect repo-b — suppressing group-wide would hide real dead
// code in every other repo of the group.
func TestReachabilityDegradationIsScopedToItsOwnRepo(t *testing.T) {
	badRoot := t.TempDir()
	plantUnreadableSource(t, badRoot)
	goodRoot := t.TempDir()
	writeFile(t, goodRoot, "src/handler.go", reachabilityFixtureSrc)

	graphs := []repoGraph{
		reachabilityFixture("repo-bad", badRoot),
		reachabilityFixture("repo-good", goodRoot),
	}
	paths := Paths{Links: filepath.Join(t.TempDir(), "g-links.json")}
	if _, err := runReachabilityPass("g", graphs, paths); err != nil {
		t.Fatalf("runReachabilityPass: %v", err)
	}

	if got := propOf(graphs[0], "orphanFn"); got != "" {
		t.Errorf("repo-bad orphanFn: want unstamped, got %q", got)
	}
	if got := propOf(graphs[1], "orphanFn"); got != "false" {
		t.Errorf("repo-good orphanFn: an unreadable file in ANOTHER repo must not "+
			"suppress this repo's dead-code finding; want %q, got %q", "false", got)
	}

	doc := readReachabilityDoc(t, paths.Links)
	badRows, goodRows := 0, 0
	for _, e := range doc.Entries {
		if e.Reachable {
			continue
		}
		switch e.Repo {
		case "repo-bad":
			badRows++
		case "repo-good":
			goodRows++
		}
	}
	if badRows != 0 {
		t.Errorf("sidecar: want 0 unreachable rows for repo-bad, got %d", badRows)
	}
	if goodRows == 0 {
		t.Errorf("sidecar: want unreachable rows for repo-good, got 0")
	}
}

// TestReachabilitySelfReferentialSymlinkDegrades adds a THIRD real error
// class, produced on a real filesystem inside t.TempDir() and independent of
// both the process's uid (unlike chmod) and safeio's stat-first refusal
// (unlike the directory fixture): a self-referential symlink, which the
// kernel fails with ELOOP.
func TestReachabilitySelfReferentialSymlinkDegrades(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	p := filepath.Join(root, "src", "handler.go")
	if err := os.Symlink(p, p); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}
	// Prove the premise the test rests on: this path really does fail to
	// read, and with neither of the classes the other tests inject.
	if _, err := readSourceFile(p, maxSourceFileBytes); err == nil {
		t.Fatalf("premise failed: self-referential symlink read succeeded")
	} else if errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("premise failed: want a read FAILURE, got fs.ErrNotExist: %v", err)
	}

	graphs := []repoGraph{reachabilityFixture("repo-a", root)}
	paths := Paths{Links: filepath.Join(t.TempDir(), "g-links.json")}
	res, err := runReachabilityPass("g", graphs, paths)
	if err != nil {
		t.Fatalf("runReachabilityPass: %v", err)
	}
	if got := propOf(graphs[0], "orphanFn"); got != "" {
		t.Errorf("orphanFn: an ELOOP read is a failure, not an answer; want unstamped, got %q", got)
	}
	if res.UnreadableSourceFiles != 1 {
		t.Errorf("PassResult.UnreadableSourceFiles: want 1, got %d", res.UnreadableSourceFiles)
	}
}

// TestReachabilityDegradesOnEveryReadFailureClass grades the predicate
// across the error CLASSES, not just the one a temp directory can produce.
// safeio.ErrWouldBlock is reachable only from safeio's process-global slot
// saturation and fs.ErrPermission is unreachable when the suite runs as
// root, so both are injected through the pass's read seam — while the pass
// itself, and the assertion on the stamped property, stay real.
//
// Without this table, widening the fs.ErrNotExist exemption to "|| ErrPermission"
// or "|| ErrWouldBlock" — the exact widening #6839 exists to forbid, and the
// one safeio's package doc singles out — stays green.
func TestReachabilityDegradesOnEveryReadFailureClass(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		degrades bool
	}{
		{"not_regular", safeio.ErrNotRegular, true},
		{"would_block", safeio.ErrWouldBlock, true},
		{"permission", fs.ErrPermission, true},
		{"wrapped_permission", fmt.Errorf("open x: %w", fs.ErrPermission), true},
		{"eloop", syscall.ELOOP, true},
		{"generic_io", errors.New("input/output error"), true},
		// The one exemption, and the only one: an absent file under a root
		// that exists is an answer, not a failure.
		{"not_exist", fs.ErrNotExist, false},
		{"wrapped_not_exist", fmt.Errorf("open x: %w", fs.ErrNotExist), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, root, "src/handler.go", reachabilityFixtureSrc)

			injected := tc.err
			readSourceFileForReachability = func(string, int64) ([]byte, error) {
				return nil, injected
			}
			t.Cleanup(func() { readSourceFileForReachability = readSourceFile })

			graphs := []repoGraph{reachabilityFixture("repo-a", root)}
			paths := Paths{Links: filepath.Join(t.TempDir(), "g-links.json")}
			res, err := runReachabilityPass("g", graphs, paths)
			if err != nil {
				t.Fatalf("runReachabilityPass: %v", err)
			}

			got := propOf(graphs[0], "orphanFn")
			doc := readReachabilityDoc(t, paths.Links)
			if tc.degrades {
				if got != "" {
					t.Errorf("%v: reachability could not be computed, so orphanFn must be "+
						"unstamped; got %q", injected, got)
				}
				if res.UnreadableSourceFiles == 0 || len(doc.DegradedRepos) != 1 {
					t.Errorf("%v: want the skip REPORTED; got unreadable=%d degraded=%v",
						injected, res.UnreadableSourceFiles, doc.DegradedRepos)
				}
				return
			}
			if got != "false" {
				t.Errorf("%v: an absent file is an answer, not a failure — orphanFn must "+
					"still be reachable=false; got %q", injected, got)
			}
			if res.UnreadableSourceFiles != 0 || len(doc.DegradedRepos) != 0 {
				t.Errorf("%v: must not be reported as a read failure; got unreadable=%d degraded=%v",
					injected, res.UnreadableSourceFiles, doc.DegradedRepos)
			}
		})
	}
}

// TestReachabilityMissingSourceRootDegrades closes the case the
// fs.ErrNotExist exemption would otherwise swallow whole: when the repo's
// source ROOT is gone, every file under it reports ErrNotExist while the
// code itself exists — exempting file by file would stamp the entire repo
// dead, which is #6839's original harm.
func TestReachabilityMissingSourceRootDegrades(t *testing.T) {
	root := filepath.Join(t.TempDir(), "moved-away")

	graphs := []repoGraph{reachabilityFixture("repo-a", root)}
	paths := Paths{Links: filepath.Join(t.TempDir(), "g-links.json")}
	res, err := runReachabilityPass("g", graphs, paths)
	if err != nil {
		t.Fatalf("runReachabilityPass: %v", err)
	}
	if got := propOf(graphs[0], "orphanFn"); got != "" {
		t.Errorf("orphanFn: the source root is missing, so NOTHING about this repo's "+
			"reachability was computed; want unstamped, got %q", got)
	}
	sidecar := readReachabilityDoc(t, paths.Links)
	if res.UnreadableSourceFiles != 1 || len(sidecar.DegradedRepos) != 1 {
		t.Errorf("want the missing root reported once; got unreadable=%d degraded=%v",
			res.UnreadableSourceFiles, sidecar.DegradedRepos)
	}
}
