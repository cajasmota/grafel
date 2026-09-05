package links

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
			g.Entities[i].Properties.Set("reachable", "false")
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
