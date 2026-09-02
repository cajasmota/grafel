package main

// #6757 arm C — the non-enum-relationship-kind tally must be SURFACED, not
// merely counted.
//
// The precedent is RenameDetectTruncated and UnsupportedExtensions in the same
// sidecar: "the stderr warning is invisible to every programmatic consumer:
// MCP, the dashboard and `grafel doctor` read the graph, not the indexer's
// log." A log line nobody reads is not a report, so the tally lands in
// graph-stats.json alongside them (and `doctor` renders it — see
// internal/cli/kinds_not_in_enum_6757_test.go).
//
// The tests here pin, in order:
//   - the sidecar carries the report, with the counts UNCAPPED while only the
//     name list is truncated (review D1: capping the count alongside the list
//     was an ALIVE mutant while no test used more than three kinds);
//   - a graph write FAILURE publishes nothing (review D2: the graph on disk is
//     then the previous generation, which this tally does not describe);
//   - "counted zero" and "never counted" are distinguishable in the artefact
//     (review D3: that distinction is the entire reason this arm counts rather
//     than drops — #6534 was a scanner reporting a repo clean it had read zero
//     bytes of);
//   - and the NON-VACUITY proof for the whole arm: a REAL index over a real
//     repo, in which a known non-enum kind reaches the report. A counter that
//     only ever sees test doubles proves nothing.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/algorithms"
	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/types"
)

func TestStatsSidecarCarriesTheNonEnumKindReport(t *testing.T) {
	rep := fbwriter.NonEnumKindReport{
		Scanned:       true,
		Edges:         9,
		DistinctKinds: 3,
		Kinds: []fbwriter.NonEnumKind{
			{Kind: "OWNS", Edges: 6},
			{Kind: "INDEXES", Edges: 2},
			{Kind: "REGISTERED_ON", Edges: 1},
		},
	}
	doc := &graph.Document{}
	side := buildStatsSidecar(doc, 0, nil, false, nil, time.Unix(0, 0).UTC(), algorithms.RenameStats{}, nil, rep)

	if !side.RelationshipKindsScanned {
		t.Error("RelationshipKindsScanned = false for a scanned report")
	}
	if side.RelationshipEdgesKindNotInEnum != 9 {
		t.Errorf("RelationshipEdgesKindNotInEnum = %d, want 9", side.RelationshipEdgesKindNotInEnum)
	}
	if side.RelationshipDistinctKindsNotInEnum != 3 {
		t.Errorf("RelationshipDistinctKindsNotInEnum = %d, want 3", side.RelationshipDistinctKindsNotInEnum)
	}
	// The NAMES, not just a total — a bare count says something is wrong, the
	// names say what.
	want := map[string]int{"OWNS": 6, "INDEXES": 2, "REGISTERED_ON": 1}
	if len(side.RelationshipKindsNotInEnum) != len(want) {
		t.Fatalf("RelationshipKindsNotInEnum = %v, want %v", side.RelationshipKindsNotInEnum, want)
	}
	for k, n := range want {
		if side.RelationshipKindsNotInEnum[k] != n {
			t.Errorf("RelationshipKindsNotInEnum[%q] = %d, want %d", k, side.RelationshipKindsNotInEnum[k], n)
		}
	}
}

// TestStatsSidecarKeepsTheCountsUncappedWhenTheNameListIsTruncated is review
// D1. The writer truncates the NAME LIST at NonEnumKindListCap and leaves the
// two totals exact; the sidecar conversion must preserve that asymmetry.
//
// Deriving the distinct count from the truncated list instead (the ALIVE
// mutant) caps the report in precisely the dimension it exists to measure: a
// graph with 200 distinct unrecognised kinds would report 32 and look bounded.
// No previous test used more than three kinds, so nothing noticed.
func TestStatsSidecarKeepsTheCountsUncappedWhenTheNameListIsTruncated(t *testing.T) {
	const distinct = fbwriter.NonEnumKindListCap + 1 // 33 — one past the cap
	rep := fbwriter.NonEnumKindReport{Scanned: true, Edges: 500, DistinctKinds: distinct}
	for i := 0; i < fbwriter.NonEnumKindListCap; i++ { // the writer already truncated
		rep.Kinds = append(rep.Kinds, fbwriter.NonEnumKind{Kind: fmt.Sprintf("ZZ_%02d", i), Edges: 1})
	}
	if len(rep.Kinds) == distinct {
		t.Fatalf("fixture is inert: the name list (%d) is not shorter than the distinct count (%d), "+
			"so a capped count would be indistinguishable from an uncapped one", len(rep.Kinds), distinct)
	}

	side := buildStatsSidecar(&graph.Document{}, 0, nil, false, nil, time.Unix(0, 0).UTC(),
		algorithms.RenameStats{}, nil, rep)

	if side.RelationshipDistinctKindsNotInEnum != distinct {
		t.Errorf("RelationshipDistinctKindsNotInEnum = %d, want %d — the distinct count must NOT be "+
			"capped with the name list (it is len(Kinds)=%d if it was)",
			side.RelationshipDistinctKindsNotInEnum, distinct, len(rep.Kinds))
	}
	if side.RelationshipEdgesKindNotInEnum != 500 {
		t.Errorf("RelationshipEdgesKindNotInEnum = %d, want 500 — the edge total must NOT be capped",
			side.RelationshipEdgesKindNotInEnum)
	}
	if len(side.RelationshipKindsNotInEnum) != fbwriter.NonEnumKindListCap {
		t.Errorf("len(RelationshipKindsNotInEnum) = %d, want the cap %d",
			len(side.RelationshipKindsNotInEnum), fbwriter.NonEnumKindListCap)
	}
}

// TestStatsSidecarDistinguishesNeverScannedFromScannedClean is review D3.
//
// With omitempty alone, a clean run and a run that never counted produce
// byte-identical JSON, so no consumer can tell "we looked and found nothing"
// from "nothing looked". That is the #6534 shape — a scanner reporting a repo
// clean that it had read zero bytes of — and a counting arm that reproduces it
// has given up its only advantage over the dropping design that was rejected.
func TestStatsSidecarDistinguishesNeverScannedFromScannedClean(t *testing.T) {
	doc := &graph.Document{}
	scannedClean := buildStatsSidecar(doc, 0, nil, false, nil, time.Unix(0, 0).UTC(),
		algorithms.RenameStats{}, nil, fbwriter.NonEnumKindReport{Scanned: true})
	neverScanned := buildStatsSidecar(doc, 0, nil, false, nil, time.Unix(0, 0).UTC(),
		algorithms.RenameStats{}, nil, fbwriter.NonEnumKindReport{})

	if !scannedClean.RelationshipKindsScanned {
		t.Error("a scanned-and-clean run did not record that it scanned")
	}
	if neverScanned.RelationshipKindsScanned {
		t.Error("an unscanned run claims to have scanned")
	}

	cleanBlob, err := json.Marshal(scannedClean)
	if err != nil {
		t.Fatal(err)
	}
	neverBlob, err := json.Marshal(neverScanned)
	if err != nil {
		t.Fatal(err)
	}
	if string(cleanBlob) == string(neverBlob) {
		t.Fatal("the ARTEFACT cannot tell a clean scan from no scan at all — both serialize to the " +
			"same bytes, so every consumer reads a graph nobody counted as a graph with nothing to report")
	}
	if !strings.Contains(string(cleanBlob), `"relationship_kinds_scanned":true`) {
		t.Errorf("a scanned-clean sidecar does not record the scan: %s", cleanBlob)
	}
	// The count fields still stay out of a clean sidecar entirely.
	for _, key := range []string{"relationship_kinds_not_in_enum", "relationship_edges_kind_not_in_enum",
		"relationship_distinct_kinds_not_in_enum"} {
		if strings.Contains(string(cleanBlob), `"`+key+`"`) {
			t.Errorf("a clean index emitted %q; the counts must be omitted when there is nothing to count", key)
		}
	}
	if strings.Contains(string(neverBlob), "relationship_kinds") {
		t.Errorf("an unscanned run emitted a relationship-kind field: %s", neverBlob)
	}
}

// TestFailedGraphWriteSurfacesNoKindReport is review D2.
//
// When the graph write fails (the #5726 oversized-graph fail-soft, EPERM, a
// failed gen-flip) the index continues — the write is non-fatal by design —
// and the graph on disk stays the PREVIOUS generation. A tally from the
// attempted write describes a file that was never persisted, and with
// omitempty a zeroed partial is indistinguishable from clean. So it must not
// be published at all: the sidecar records "not scanned".
func TestFailedGraphWriteSurfacesNoKindReport(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a real index; skipped under -short")
	}
	useInProcessIndex(t)
	repo := fallbackManifestRepo(t, "kindwritefail", "main")
	writeFixtureFile(t, repo, "schema.sql",
		"CREATE TABLE users (id INT PRIMARY KEY, email TEXT);\n"+
			"CREATE INDEX users_email_idx ON users (email);\n")
	seedGitRun(t, repo, "add", "-A")
	seedGitRun(t, repo, "commit", "-q", "-m", "sql")

	// The write fails, but the writer still reports what it had tallied — the
	// realistic shape, since the failure can happen after serialization.
	var seamFired bool
	prevWriter := writeGraphGen
	writeGraphGen = func(dir string, doc *graph.Document) (string, fbwriter.NonEnumKindReport, error) {
		seamFired = true
		return "", fbwriter.NonEnumKindReport{
			Scanned:       true,
			Edges:         7,
			DistinctKinds: 1,
			Kinds:         []fbwriter.NonEnumKind{{Kind: "INDEXES", Edges: 7}},
		}, errors.New("injected: graph.fb write failed (disk full / EPERM / gen-flip)")
	}
	t.Cleanup(func() { writeGraphGen = prevWriter })

	_ = daemonSchedulerIndex(context.Background(), repo, "")
	if !seamFired {
		t.Fatal("fixture is inert: the writeGraphGen seam never fired, so no write was attempted")
	}

	side, err := graph.LoadSidecar(daemon.StateDirForRepo(repo))
	if err != nil || side == nil {
		t.Skipf("no sidecar written on the failed-write path (%v) — nothing to over-report", err)
	}
	if side.RelationshipKindsScanned {
		t.Errorf("the graph write FAILED, so the graph on disk is the previous generation — but the "+
			"sidecar claims this tally describes it (scanned=true, edges=%d kinds=%v)",
			side.RelationshipEdgesKindNotInEnum, side.RelationshipKindsNotInEnum)
	}
	if side.RelationshipEdgesKindNotInEnum != 0 || len(side.RelationshipKindsNotInEnum) != 0 {
		t.Errorf("a failed write published a partial tally: edges=%d kinds=%v",
			side.RelationshipEdgesKindNotInEnum, side.RelationshipKindsNotInEnum)
	}
}

// TestRealIndexReportsKindsNotInEnumReachingTheWritePath is the non-vacuity
// proof. INDEXES is emitted by the SQL extractor (internal/extractors/sql/
// sql.go:456) and is NOT in types.AllRelationshipKinds(), so a real index over
// a repo containing one CREATE INDEX must report it — from the write path, not
// from a fixture.
func TestRealIndexReportsKindsNotInEnumReachingTheWritePath(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a real index; skipped under -short")
	}
	if types.IsValidRelationshipKind("INDEXES") {
		t.Skip("INDEXES has since been added to the enum; pick another kind for this probe")
	}
	useInProcessIndex(t) // no forked child: this test drives the indexer directly
	repo := fallbackManifestRepo(t, "nonenumkinds", "main")
	writeFixtureFile(t, repo, "schema.sql",
		"CREATE TABLE users (id INT PRIMARY KEY, email TEXT);\n"+
			"CREATE INDEX users_email_idx ON users (email);\n")
	seedGitRun(t, repo, "add", "-A")
	seedGitRun(t, repo, "commit", "-q", "-m", "sql")

	if err := daemonSchedulerIndex(context.Background(), repo, ""); err != nil {
		t.Fatalf("daemonSchedulerIndex: %v", err)
	}

	stateDir := daemon.StateDirForRepo(repo)
	blob, err := os.ReadFile(filepath.Join(stateDir, "graph-stats.json"))
	if err != nil {
		t.Fatalf("read graph-stats.json: %v", err)
	}
	var side graph.GraphStatsSidecar
	if err := json.Unmarshal(blob, &side); err != nil {
		t.Fatalf("decode sidecar: %v", err)
	}
	if side.TotalRelationships == 0 {
		t.Fatal("fixture is inert: the index wrote no relationships at all, so the write path observed nothing")
	}
	if !side.RelationshipKindsScanned {
		t.Fatal("a successful real index did not record that the write path scanned the kinds it wrote")
	}
	if side.RelationshipKindsNotInEnum["INDEXES"] == 0 {
		t.Fatalf("a real index of a repo with a CREATE INDEX did not report the non-enum kind INDEXES "+
			"— the counter is not observing the real write path (sidecar: edges=%d kinds=%v, %d relationships written)",
			side.RelationshipEdgesKindNotInEnum, side.RelationshipKindsNotInEnum, side.TotalRelationships)
	}
	if side.RelationshipEdgesKindNotInEnum < side.RelationshipDistinctKindsNotInEnum {
		t.Errorf("edges=%d < distinct kinds=%d — impossible",
			side.RelationshipEdgesKindNotInEnum, side.RelationshipDistinctKindsNotInEnum)
	}
	// And nothing was dropped: the edge is in the graph as well as in the report.
	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	var found bool
	for _, r := range doc.Relationships {
		if r.Kind == "INDEXES" {
			found = true
			break
		}
	}
	if !found {
		t.Error("the INDEXES edge was reported but is absent from the written graph — arm C counts, it never drops")
	}
}
