package dashboard

// store_graphjson_staleness_6015_test.go — #6015.
//
// repoGraphBytes read graph.json UNCONDITIONALLY and only fell back to the
// FlatBuffers graph when the JSON was absent. So any repo that ever produced a
// graph.json — via `--export-json`, or simply by predating the ADR-0016 layout
// — pinned the dashboard's RepoGraph / GroupGraph endpoints to that snapshot
// FOREVER, across every subsequent reindex, with nothing on the surface to say
// it was stale. Its sibling sidecar readers (graph/descriptions, graph/flows)
// already stat-compare before use; store.go was the odd one out.
//
// The fix is a stat gate, and these tests pin all three of its edges:
//
//   - STALE: graph.fb newer than graph.json ⇒ serve the FB-derived graph.
//   - FRESH: graph.json newer ⇒ serve its bytes VERBATIM. This is the
//     performance half and it is not optional: on the reference corpus (427k
//     entities / 1.85M relationships) regenerating from FB on every request
//     would be a far worse regression than the staleness it cures.
//   - TIE: equal mtimes ⇒ treat the JSON as stale. graph.fb is committed by an
//     atomic rename (internal/atomicfile), which carries the temp file's mtime,
//     and filesystem timestamp granularity is not guaranteed to be finer than
//     the write. An `After` comparison — never `!Before` — is what stops a
//     same-instant rebuild from being mistaken for "the JSON is still current".

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	fb "github.com/cajasmota/grafel/internal/graph/fbgraph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// staleJSONRepo is the marker baked into the graph.json fixture. It can never
// appear in an FB-derived result, so its presence/absence in the served bytes
// identifies WHICH source answered — the only thing these tests care about.
const staleJSONRepo = "stale-json-snapshot"

// freshFBRepo is the marker baked into the graph.fb fixture.
const freshFBRepo = "fresh-fb-graph"

// newGraphJSONFixtureRepo returns a repo path whose state dir exists and is
// isolated to this test, resolved through the SAME daemon.StateDirForRepo
// derivation production uses.
func newGraphJSONFixtureRepo(t *testing.T) (repoPath, stateDir string) {
	t.Helper()
	t.Setenv(daemon.EnvRoot, t.TempDir())
	repoPath = t.TempDir()
	stateDir = daemon.StateDirForRepo(repoPath)
	if stateDir == "" {
		t.Fatal("StateDirForRepo returned empty")
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	return repoPath, stateDir
}

// writeGraphJSONFixture writes a graph.json carrying staleJSONRepo, stamped
// with mtime.
//
// The bytes are deliberately INDENTED. A plain json.Marshal fixture would be
// byte-identical to what an unmarshal + re-marshal round trip produces, so the
// "served verbatim" assertions could not tell the raw-read fast path from a
// regeneration and would silently stop testing anything. Indentation is the
// cheapest property no re-marshal in this code path reproduces.
func writeGraphJSONFixture(t *testing.T, repoPath string, mtime time.Time) {
	t.Helper()
	p := daemon.GraphPathForRepo(repoPath)
	b, err := json.MarshalIndent(&graph.Document{Repo: staleJSONRepo}, "", "  ")
	if err != nil {
		t.Fatalf("marshal graph.json fixture: %v", err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatalf("chtimes graph.json: %v", err)
	}
}

// writeFlatGraphFBFixture writes a legacy flat graph.fb carrying freshFBRepo,
// stamped with mtime. Flat (not gen-pointer) on purpose: #6015 is live TODAY on
// flat graphs and does not need segments to manifest.
func writeFlatGraphFBFixture(t *testing.T, stateDir string, mtime time.Time) {
	t.Helper()
	p := filepath.Join(stateDir, "graph.fb")
	doc := &graph.Document{Repo: freshFBRepo, Entities: []graph.Entity{
		{ID: "aa1", QualifiedName: "p.A", Kind: "function", Name: "A"},
	}}
	if err := fbwriter.WriteAtomic(p, doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatalf("chtimes graph.fb: %v", err)
	}
}

// servedRepo unmarshals whatever repoGraphBytes returned far enough to read the
// `repo` field, which identifies the source.
func servedRepo(t *testing.T, b []byte) string {
	t.Helper()
	var doc struct {
		Repo string `json:"repo"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("unmarshal served graph: %v", err)
	}
	return doc.Repo
}

// TestRepoGraphBytes_StaleGraphJSONIgnored is the bug itself: N reindexes have
// written a newer graph.fb and the dashboard is still handing out the old JSON.
func TestRepoGraphBytes_StaleGraphJSONIgnored(t *testing.T) {
	repoPath, stateDir := newGraphJSONFixtureRepo(t)
	base := time.Now().Add(-time.Hour)
	writeGraphJSONFixture(t, repoPath, base)
	writeFlatGraphFBFixture(t, stateDir, base.Add(30*time.Minute))

	b, err := repoGraphBytes(repoPath)
	if err != nil {
		t.Fatalf("repoGraphBytes: %v", err)
	}
	if got := servedRepo(t, b); got != freshFBRepo {
		t.Errorf("served repo = %q, want %q: graph.fb is 30 minutes newer than graph.json, so the JSON "+
			"snapshot is a stale artifact of an earlier index and must not be served", got, freshFBRepo)
	}
}

// TestRepoGraphBytes_FreshGraphJSONServedVerbatim is the performance half. A
// gate that ignored graph.json unconditionally would "fix" staleness by
// re-marshalling a 427k-entity graph on every dashboard request. Byte-identity
// with the on-disk file is the assertion, because it proves NO regeneration
// happened — a re-marshal could never reproduce this fixture's content.
func TestRepoGraphBytes_FreshGraphJSONServedVerbatim(t *testing.T) {
	repoPath, stateDir := newGraphJSONFixtureRepo(t)
	base := time.Now().Add(-time.Hour)
	writeFlatGraphFBFixture(t, stateDir, base)
	writeGraphJSONFixture(t, repoPath, base.Add(30*time.Minute))

	b, err := repoGraphBytes(repoPath)
	if err != nil {
		t.Fatalf("repoGraphBytes: %v", err)
	}
	want, err := os.ReadFile(daemon.GraphPathForRepo(repoPath))
	if err != nil {
		t.Fatalf("read graph.json: %v", err)
	}
	if string(b) != string(want) {
		t.Errorf("served bytes are not the on-disk graph.json (served repo=%q): a graph.json NEWER than "+
			"graph.fb must be served as-is, or every request re-marshals the whole graph",
			servedRepo(t, b))
	}
}

// TestRepoGraphBytes_EqualMtimeTreatedAsStale: graph.fb lands by atomic rename,
// which preserves the temp file's mtime, and nothing guarantees the filesystem
// resolves the two writes to different timestamps. A `!Before` comparison would
// call this JSON current and serve a snapshot from a graph generation that has
// already been replaced. Ties must resolve to "regenerate".
func TestRepoGraphBytes_EqualMtimeTreatedAsStale(t *testing.T) {
	repoPath, stateDir := newGraphJSONFixtureRepo(t)
	same := time.Now().Add(-time.Hour).Truncate(time.Second)
	writeGraphJSONFixture(t, repoPath, same)
	writeFlatGraphFBFixture(t, stateDir, same)

	ji, err := os.Stat(daemon.GraphPathForRepo(repoPath))
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(stateDir, "graph.fb"))
	if err != nil {
		t.Fatal(err)
	}
	if !ji.ModTime().Equal(fi.ModTime()) {
		t.Fatalf("precondition: fixture mtimes differ (%v vs %v); this test is meaningless without a tie",
			ji.ModTime(), fi.ModTime())
	}

	b, err := repoGraphBytes(repoPath)
	if err != nil {
		t.Fatalf("repoGraphBytes: %v", err)
	}
	if got := servedRepo(t, b); got != freshFBRepo {
		t.Errorf("served repo = %q, want %q: on an mtime TIE the JSON must lose. The staleness check "+
			"cannot be fooled into serving a superseded snapshot by an equal-mtime write", got, freshFBRepo)
	}
}

// TestRepoGraphBytes_NoFBGraphServesJSON: a legacy repo with only a graph.json
// and no FB graph at all must keep working. The gate answers "is the JSON older
// than the graph?", and with no graph there is nothing for it to be older than.
//
// The assertion is byte-identity, not merely "the right content". Content alone
// cannot distinguish the two routes here: graph.LoadGraphFromDir ALSO falls back
// to graph.json when no FB graph exists, so a GraphAbsent branch that wrongly
// returned "not usable" would still produce the same graph — just via an
// unmarshal + re-marshal round trip on every single request. Byte-identity is
// what pins the fast path.
func TestRepoGraphBytes_NoFBGraphServesJSON(t *testing.T) {
	repoPath, _ := newGraphJSONFixtureRepo(t)
	writeGraphJSONFixture(t, repoPath, time.Now().Add(-time.Hour))

	b, err := repoGraphBytes(repoPath)
	if err != nil {
		t.Fatalf("repoGraphBytes: %v", err)
	}
	if got := servedRepo(t, b); got != staleJSONRepo {
		t.Fatalf("served repo = %q, want %q: with no FB graph on disk the JSON is the only graph there is",
			got, staleJSONRepo)
	}
	want, err := os.ReadFile(daemon.GraphPathForRepo(repoPath))
	if err != nil {
		t.Fatalf("read graph.json: %v", err)
	}
	if string(b) != string(want) {
		t.Errorf("served bytes are not the on-disk graph.json: with no graph to be stale against, the " +
			"JSON must take the raw-read fast path, not a re-marshal on every request")
	}
}

// TestRepoGraphBytes_SegmentSetKeepsJSONFastPathWhenManifestOlder pins the
// LAYOUT ASYMMETRY that graphJSONUsable's doc comment describes, so the comment
// is a checked claim rather than prose that can quietly go stale.
//
// cmd/grafel/index.go stamps graph.json and the FB artifact with one identical
// mtime. For a single-file repo the FB artifact IS the .fb file, so the tie
// resolves to "regenerate" and the raw-read fast path is disabled (that is
// TestRepoGraphBytes_EqualMtimeTreatedAsStale, and it is a real cost). For a
// SEGMENT SET the stamped artifact is the gen DIRECTORY while this gate reads
// manifest.json — written earlier, before the pointer flip — so the JSON is
// strictly newer and the fast path survives. Same indexer, two behaviours.
func TestRepoGraphBytes_SegmentSetKeepsJSONFastPathWhenManifestOlder(t *testing.T) {
	repoPath, stateDir := newGraphJSONFixtureRepo(t)
	base := time.Now().Add(-time.Hour)
	// Manifest older than graph.json: the shape the real writer produces, since
	// index.go stamps the gen dir and not the manifest inside it.
	writeDashboardSegmentSetFixture(t, stateDir, 5, base)
	writeGraphJSONFixture(t, repoPath, base.Add(30*time.Minute))

	b, err := repoGraphBytes(repoPath)
	if err != nil {
		t.Fatalf("repoGraphBytes: %v", err)
	}
	want, err := os.ReadFile(daemon.GraphPathForRepo(repoPath))
	if err != nil {
		t.Fatalf("read graph.json: %v", err)
	}
	if string(b) != string(want) {
		t.Errorf("segment-set repo with a manifest OLDER than graph.json did not take the raw-read fast "+
			"path (served repo=%q). The documented asymmetry between the single-file and segment-set "+
			"layouts no longer holds — re-check graphJSONUsable's comment against the code",
			servedRepo(t, b))
	}
}

// TestRepoGraphBytes_CorruptSegmentSetErrorsRatherThanServingStaleJSON: when a
// graph demonstrably exists but cannot be read, the honest answer is the error.
// Falling back to graph.json here would resurrect the exact failure mode #6015
// is about — a stale snapshot served in place of a real problem, with the
// dashboard showing a plausible topology and no way for anyone to tell.
func TestRepoGraphBytes_CorruptSegmentSetErrorsRatherThanServingStaleJSON(t *testing.T) {
	repoPath, stateDir := newGraphJSONFixtureRepo(t)
	writeGraphJSONFixture(t, repoPath, time.Now().Add(-time.Hour))

	genDir := filepath.Join(stateDir, graph.GenDirName(9))
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(genDir, graph.ManifestFileName), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := graph.WriteCurrentPointerRaw(stateDir, graph.GenDirName(9)); err != nil {
		t.Fatal(err)
	}

	b, err := repoGraphBytes(repoPath)
	if err == nil {
		t.Fatalf("repoGraphBytes returned bytes (repo=%q) for a repo whose active segment set is corrupt; "+
			"want an error, not a stale graph.json standing in for it", servedRepo(t, b))
	}
}

// writeOldFormatGraphFB builds a valid graph.fb and patches its on-disk version
// scalar down, fabricating a graph written by an older grafel build. Mirrors
// internal/graph/reindex_required_test.go's helper of the same name.
func writeOldFormatGraphFB(t *testing.T, stateDir string, oldVersion int, mtime time.Time) {
	t.Helper()
	doc := &graph.Document{Repo: freshFBRepo, Entities: []graph.Entity{
		{ID: "ent0000000000000a", Name: "foo", Kind: "function", SourceFile: "a.go"},
	}}
	buf, err := fbwriter.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !fb.GetRootAsGraph(buf, 0).MutateVersion(int32(oldVersion)) {
		t.Fatal("MutateVersion returned false — slot missing?")
	}
	p := filepath.Join(stateDir, "graph.fb")
	if err := os.WriteFile(p, buf, 0o644); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatalf("chtimes graph.fb: %v", err)
	}
}

// TestRepoGraphBytes_VersionSkewSurfacesReindexRequired covers the case the
// no-fallback rule made worse before it made better.
//
// After a grafel upgrade and before a reindex, a repo's graph.fb is
// version-incompatible. Previously the dashboard silently served graph.json —
// an older-but-honest graph, which is precisely the silently-stale behaviour
// #6015 removes. Now it refuses. But format skew is transient-by-reindex, not
// corruption, so refusing with a bare loader error puts an empty panel in front
// of the user with no indication that one command fixes it. The error must name
// the remedy.
func TestRepoGraphBytes_VersionSkewSurfacesReindexRequired(t *testing.T) {
	repoPath, stateDir := newGraphJSONFixtureRepo(t)
	base := time.Now().Add(-time.Hour)
	writeGraphJSONFixture(t, repoPath, base)
	// Newer than the JSON, so the staleness gate correctly refuses the snapshot
	// and the load is reached — and then fails on version skew.
	writeOldFormatGraphFB(t, stateDir, 1, base.Add(30*time.Minute))

	b, err := repoGraphBytes(repoPath)
	if err == nil {
		t.Fatalf("repoGraphBytes returned bytes (repo=%q) for a version-incompatible graph.fb; the stale "+
			"graph.json must not stand in for it", servedRepo(t, b))
	}
	if !strings.Contains(err.Error(), "reindex required") {
		t.Errorf("error does not tell the user what to do: %v\n"+
			"want it to carry the shared FormatVersionError wording ('reindex required'), so the panel "+
			"names the one action that fixes this instead of showing an opaque failure", err)
	}
}

// TestRepoGraphBytes_UnrelatedLoadErrorPassedThrough: only version skew gets the
// remedy wording. Dressing up every load failure as "reindex required" would
// send users to reindex a repo whose problem a reindex will not fix.
func TestRepoGraphBytes_UnrelatedLoadErrorPassedThrough(t *testing.T) {
	repoPath, _ := newGraphJSONFixtureRepo(t)
	// No graph.json and no graph at all: LoadGraphFromDir's "neither graph.fb
	// nor graph.json found" must reach the caller unchanged.
	_, err := repoGraphBytes(repoPath)
	if err == nil {
		t.Fatal("expected an error for a repo with no graph at all")
	}
	if strings.Contains(err.Error(), "reindex required") {
		t.Errorf("a non-version-skew load failure was rendered as a reindex prompt: %v", err)
	}
}

// TestGraphSourceModTime_PrefersManifestOverGenDir pins WHICH file dates a
// segment set. manifest.json is its atomic commit point; the gen dir's own
// mtime moves whenever anything inside it is touched and is only a fallback.
// Reading the directory instead would date the graph by an unrelated write.
func TestGraphSourceModTime_PrefersManifestOverGenDir(t *testing.T) {
	stateDir := t.TempDir()
	manifestMod := time.Now().Add(-3 * time.Hour).Truncate(time.Second)
	writeDashboardSegmentSetFixture(t, stateDir, 3, manifestMod)

	desc, err := graph.CurrentGraphDescriptor(stateDir)
	if err != nil {
		t.Fatalf("CurrentGraphDescriptor: %v", err)
	}
	if desc.Kind != graph.GraphSegmentSet {
		t.Fatalf("precondition: descriptor kind = %v, want GraphSegmentSet", desc.Kind)
	}
	genInfo, err := os.Stat(desc.GenDir)
	if err != nil {
		t.Fatal(err)
	}
	if genInfo.ModTime().Equal(manifestMod) {
		t.Skip("gen dir and manifest happen to share an mtime; nothing to distinguish")
	}

	got, ok := graphSourceModTime(desc)
	if !ok {
		t.Fatal("graphSourceModTime: ok=false for a well-formed segment set")
	}
	if !got.Equal(manifestMod) {
		t.Errorf("graphSourceModTime = %v, want the MANIFEST mtime %v (gen dir is %v): a segment set is "+
			"dated by its commit point, not by whatever last touched its directory",
			got, manifestMod, genInfo.ModTime())
	}
}

// TestGraphSourceModTime_NotOKWhenUndatable: "no graph" and "a graph I cannot
// stat" both report ok=false, which repoGraphBytes' gate treats as "do not
// serve the JSON". The single-file-with-missing-path shape is reachable only as
// a TOCTOU race in production (CurrentGraphDescriptor stats before returning),
// so it is pinned here at the helper rather than end-to-end.
func TestGraphSourceModTime_NotOKWhenUndatable(t *testing.T) {
	if _, ok := graphSourceModTime(graph.GraphDescriptor{Kind: graph.GraphAbsent}); ok {
		t.Error("GraphAbsent: ok=true, want false — there is no graph to date")
	}
	missing := graph.GraphDescriptor{Kind: graph.GraphSingleFile, Path: filepath.Join(t.TempDir(), "gone.fb")}
	if _, ok := graphSourceModTime(missing); ok {
		t.Error("GraphSingleFile with a vanished path: ok=true, want false")
	}
	emptySeg := graph.GraphDescriptor{Kind: graph.GraphSegmentSet, GenDir: filepath.Join(t.TempDir(), "gone")}
	if _, ok := graphSourceModTime(emptySeg); ok {
		t.Error("GraphSegmentSet with a vanished gen dir: ok=true, want false")
	}
}

// TestRepoGraphBytes_StaleAgainstSegmentSet: the same gate must hold when the
// active graph is a SEGMENT SET (graph.<gen>/ + manifest.json) rather than a
// flat .fb. A gate hardcoded to stat graph.fb would find nothing, conclude
// "no graph", and serve the stale JSON — the exact silent-stale failure again,
// via the layout that #5915 makes the default.
func TestRepoGraphBytes_StaleAgainstSegmentSet(t *testing.T) {
	repoPath, stateDir := newGraphJSONFixtureRepo(t)
	base := time.Now().Add(-time.Hour)
	writeGraphJSONFixture(t, repoPath, base)
	writeDashboardSegmentSetFixture(t, stateDir, 7, base.Add(30*time.Minute))

	b, err := repoGraphBytes(repoPath)
	if err != nil {
		t.Fatalf("repoGraphBytes: %v", err)
	}
	if got := servedRepo(t, b); got == staleJSONRepo {
		t.Errorf("served the stale graph.json against a NEWER segment set: the freshness source must be " +
			"the active graph descriptor, not a hardcoded graph.fb path")
	}
}
