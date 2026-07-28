package cli

// status_graphload_test.go — `grafel status` must not report a corrupt graph as
// healthy (#6013).
//
// The bug: ComputeStatusSummary wrapped its LoadGraphFromDir call in an
// anonymous func with `defer func(){ if r := recover(); r != nil {} }()` and an
// empty body ("Silently ignore panics during graph loading"), and additionally
// dropped the returned error on the floor (`if err == nil && doc != nil`). A
// truncated/garbage graph.fb therefore produced a plausible, SMALLER-than-
// reality repo line — Files/IndexedRef/IndexedSHA blank, endpoint/flow counts
// missing — with no warning at all, indistinguishable from a healthy repo.
//
// The fix records the failure on RepoStatus.GraphLoadError (the same
// string-valued "load failed" representation internal/mcp uses for
// LoadedRepo.loadErr / the dashboard's LoadError, see #6010) and renders it.
// A repo that simply has NO graph yet is NOT a failure — that stays the
// existing "0 entities … indexed (never)" shape.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/registry"
)

// statusFixtureRepo registers a repo path under the sandbox home and returns
// both the registry entry and its resolved state dir.
func statusFixtureRepo(t *testing.T, home, slug string) (registry.Repo, string) {
	t.Helper()
	path := filepath.Join(home, "repos", slug)
	makeRepo(t, path)
	stateDir := daemon.StateDirForRepo(path)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	return registry.Repo{Slug: slug, Path: path}, stateDir
}

// writeHealthyGraph writes a real, readable graph.fb into stateDir.
func writeHealthyGraph(t *testing.T, stateDir string) {
	t.Helper()
	doc := &graph.Document{Repo: "ok", Entities: []graph.Entity{
		{ID: "a1", QualifiedName: "p.A", Kind: "function", Name: "A"},
		{ID: "a2", QualifiedName: "p.B", Kind: "http_endpoint_definition", Name: "B"},
	}}
	if err := fbwriter.WriteAtomic(filepath.Join(stateDir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
}

// writeTruncatedGraph writes a REAL graph.fb and then chops it in half. The
// flatbuffers accessors PANIC on this input ("slice bounds out of range"),
// which is exactly the swallowed-panic path #6013 is about.
func writeTruncatedGraph(t *testing.T, stateDir string) {
	t.Helper()
	writeHealthyGraph(t, stateDir)
	p := filepath.Join(stateDir, "graph.fb")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read graph.fb: %v", err)
	}
	if err := os.WriteFile(p, b[:len(b)/2], 0o644); err != nil {
		t.Fatalf("truncate graph.fb: %v", err)
	}
}

// writeUnreadableGraph writes a graph.fb that fails to load via a returned
// ERROR rather than a panic (all-zero bytes read as format version 2, below
// the required version). This is the second half of the swallowed-failure
// path: the old code's `if err == nil && doc != nil` dropped it just as
// silently as the recover() dropped the panic.
func writeUnreadableGraph(t *testing.T, stateDir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(stateDir, "graph.fb"), make([]byte, 512), 0o644); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
}

// TestStatus_TruncatedGraphReportedAsFailed: a graph.fb whose reader PANICS
// must be reported as a load failure, never as a healthy (smaller) repo.
func TestStatus_TruncatedGraphReportedAsFailed(t *testing.T) {
	home := withSandboxHome(t)
	repo, stateDir := statusFixtureRepo(t, home, "broken")
	writeTruncatedGraph(t, stateDir)

	s := ComputeStatusSummary("g", []registry.Repo{repo})
	rs := s.RepoStats["broken"]
	if rs == nil {
		t.Fatal("repo missing from summary")
	}
	if rs.GraphLoadError == "" {
		t.Fatal("a graph.fb that panics the reader must set GraphLoadError (#6013)")
	}

	var buf bytes.Buffer
	PrintStatusSummary(&buf, s)
	out := buf.String()
	if !strings.Contains(out, "graph FAILED to load") {
		t.Fatalf("status output must mark the repo as failed, got:\n%s", out)
	}
	if !strings.Contains(out, "broken") {
		t.Fatalf("status output must name the repo, got:\n%s", out)
	}
}

// TestStatus_UnreadableGraphReportedAsFailed: the returned-error half of the
// same defect.
func TestStatus_UnreadableGraphReportedAsFailed(t *testing.T) {
	home := withSandboxHome(t)
	repo, stateDir := statusFixtureRepo(t, home, "unreadable")
	writeUnreadableGraph(t, stateDir)

	s := ComputeStatusSummary("g", []registry.Repo{repo})
	rs := s.RepoStats["unreadable"]
	if rs == nil {
		t.Fatal("repo missing from summary")
	}
	if rs.GraphLoadError == "" {
		t.Fatal("a graph.fb that fails to load with an error must set GraphLoadError (#6013)")
	}

	var buf bytes.Buffer
	PrintStatusSummary(&buf, s)
	if !strings.Contains(buf.String(), "graph FAILED to load") {
		t.Fatalf("status output must mark the repo as failed, got:\n%s", buf.String())
	}
}

// TestStatus_NoGraphYetIsNotAFailure: a never-indexed repo is a DISTINCT state
// from a failed load. It must keep the plain "indexed (never)" shape and must
// NOT be flagged as failed.
func TestStatus_NoGraphYetIsNotAFailure(t *testing.T) {
	home := withSandboxHome(t)
	repo, _ := statusFixtureRepo(t, home, "fresh")

	s := ComputeStatusSummary("g", []registry.Repo{repo})
	rs := s.RepoStats["fresh"]
	if rs == nil {
		t.Fatal("repo missing from summary")
	}
	if rs.GraphLoadError != "" {
		t.Fatalf("a never-indexed repo is not a load failure, got GraphLoadError=%q", rs.GraphLoadError)
	}

	var buf bytes.Buffer
	PrintStatusSummary(&buf, s)
	out := buf.String()
	if strings.Contains(out, "FAILED to load") {
		t.Fatalf("a never-indexed repo must not be marked failed, got:\n%s", out)
	}
	if !strings.Contains(out, "indexed (never)") {
		t.Fatalf("a never-indexed repo keeps its existing shape, got:\n%s", out)
	}
}

// TestStatus_HealthyGraphIsNotAFailure: the guard must not fire on a good
// graph — the third state.
func TestStatus_HealthyGraphIsNotAFailure(t *testing.T) {
	home := withSandboxHome(t)
	repo, stateDir := statusFixtureRepo(t, home, "healthy")
	writeHealthyGraph(t, stateDir)

	s := ComputeStatusSummary("g", []registry.Repo{repo})
	rs := s.RepoStats["healthy"]
	if rs == nil {
		t.Fatal("repo missing from summary")
	}
	if rs.GraphLoadError != "" {
		t.Fatalf("healthy graph must not be flagged, got GraphLoadError=%q", rs.GraphLoadError)
	}
	if s.HTTPEndpoints != 1 {
		t.Fatalf("healthy graph must still be fully counted, HTTPEndpoints=%d", s.HTTPEndpoints)
	}

	var buf bytes.Buffer
	PrintStatusSummary(&buf, s)
	if strings.Contains(buf.String(), "FAILED to load") {
		t.Fatalf("healthy repo must not be marked failed, got:\n%s", buf.String())
	}
}

// writeCorruptSegmentSet points `current` at a segment-set gen dir whose
// manifest.json is garbage. CurrentGraphDescriptor surfaces that as
// "graph: resolve segment-set <ABSOLUTE PATH>: …" — the failure mode that
// carries a filesystem path into the error string.
func writeCorruptSegmentSet(t *testing.T, stateDir string) {
	t.Helper()
	genDir := filepath.Join(stateDir, graph.GenDirName(3))
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatalf("mkdir gen dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(genDir, "manifest.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := graph.WriteCurrentPointerRaw(stateDir, graph.GenDirName(3)); err != nil {
		t.Fatalf("write current: %v", err)
	}
}

// TestStatus_GraphLoadErrorCarriesNoFilesystemPaths: GraphLoadError is printed
// to the terminal and may be pasted into an issue. Repository layouts are
// sensitive here, so a load error must be reduced to basenames rather than
// relying on which underlying error happens to fire.
func TestStatus_GraphLoadErrorCarriesNoFilesystemPaths(t *testing.T) {
	home := withSandboxHome(t)
	repo, stateDir := statusFixtureRepo(t, home, "segbroken")
	writeCorruptSegmentSet(t, stateDir)

	s := ComputeStatusSummary("g", []registry.Repo{repo})
	rs := s.RepoStats["segbroken"]
	if rs == nil || rs.GraphLoadError == "" {
		t.Fatalf("a corrupt segment-set manifest must be reported, got %+v", rs)
	}
	if strings.Contains(rs.GraphLoadError, string(os.PathSeparator)) {
		t.Fatalf("GraphLoadError must not carry filesystem paths, got %q", rs.GraphLoadError)
	}
	if strings.Contains(rs.GraphLoadError, home) {
		t.Fatalf("GraphLoadError leaked the repo location: %q", rs.GraphLoadError)
	}
	// The diagnosis must survive the scrub: what failed, and on which
	// generation — just not where the repo lives.
	for _, want := range []string{"manifest", "graph.3", "invalid character"} {
		if !strings.Contains(rs.GraphLoadError, want) {
			t.Fatalf("scrubbing must keep the diagnosis (missing %q), got %q", want, rs.GraphLoadError)
		}
	}

	var buf bytes.Buffer
	PrintStatusSummary(&buf, s)
	if strings.Contains(buf.String(), home) {
		t.Fatalf("rendered status leaked the repo location:\n%s", buf.String())
	}
}

// TestStatus_TotalLineMarkedIncompleteOnLoadFailure: the aggregated TOTAL line
// under-reports whenever any repo's graph failed to load. Marking only the
// per-repo line leaves the headline number silently wrong.
func TestStatus_TotalLineMarkedIncompleteOnLoadFailure(t *testing.T) {
	home := withSandboxHome(t)
	bad, badDir := statusFixtureRepo(t, home, "broken")
	good, goodDir := statusFixtureRepo(t, home, "fine")
	writeTruncatedGraph(t, badDir)
	writeHealthyGraph(t, goodDir)

	var buf bytes.Buffer
	PrintStatusSummary(&buf, ComputeStatusSummary("g", []registry.Repo{bad, good}))
	out := buf.String()

	totalLine := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "TOTAL:") {
			totalLine = l
		}
	}
	if totalLine == "" {
		t.Fatalf("no TOTAL line in:\n%s", out)
	}
	if !strings.Contains(totalLine, "INCOMPLETE") {
		t.Fatalf("the TOTAL line must be marked incomplete when a repo's graph failed to load, got %q", totalLine)
	}
	if !strings.Contains(totalLine, "1 repo") {
		t.Fatalf("the TOTAL marker must say how many repos are missing, got %q", totalLine)
	}
}

// TestStatus_TotalLineCleanWhenAllGraphsLoad: the marker must not fire when
// every graph is readable.
func TestStatus_TotalLineCleanWhenAllGraphsLoad(t *testing.T) {
	home := withSandboxHome(t)
	good, goodDir := statusFixtureRepo(t, home, "fine")
	writeHealthyGraph(t, goodDir)
	fresh, _ := statusFixtureRepo(t, home, "never")

	var buf bytes.Buffer
	PrintStatusSummary(&buf, ComputeStatusSummary("g", []registry.Repo{good, fresh}))
	if strings.Contains(buf.String(), "INCOMPLETE") {
		t.Fatalf("no repo failed to load; TOTAL must not be marked:\n%s", buf.String())
	}
}

// TestStatus_OneBadGraphDoesNotHideTheOthers: status stays NON-FATAL. A repo
// with a corrupt graph must not stop the healthy repos in the same group from
// being loaded and counted.
func TestStatus_OneBadGraphDoesNotHideTheOthers(t *testing.T) {
	home := withSandboxHome(t)
	bad, badDir := statusFixtureRepo(t, home, "aaa-broken")
	good, goodDir := statusFixtureRepo(t, home, "zzz-healthy")
	writeTruncatedGraph(t, badDir)
	writeHealthyGraph(t, goodDir)

	s := ComputeStatusSummary("g", []registry.Repo{bad, good})
	if s.RepoStats["aaa-broken"].GraphLoadError == "" {
		t.Fatal("corrupt repo must be flagged")
	}
	if got := s.RepoStats["zzz-healthy"]; got == nil || got.GraphLoadError != "" {
		t.Fatalf("healthy repo must survive a sibling's corrupt graph, got %+v", got)
	}
	if s.HTTPEndpoints != 1 {
		t.Fatalf("healthy repo's derived counts must still be computed, HTTPEndpoints=%d", s.HTTPEndpoints)
	}

	var buf bytes.Buffer
	PrintStatusSummary(&buf, s)
	out := buf.String()
	if !strings.Contains(out, "aaa-broken") || !strings.Contains(out, "zzz-healthy") {
		t.Fatalf("both repos must appear in the output, got:\n%s", out)
	}
}
