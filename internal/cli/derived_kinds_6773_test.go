package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/registry"
)

// derived_kinds_6773_test.go — #6773.
//
// Declaring COMMIT_COUPLED takes 27,407 of the 27,645 edges out of doctor's
// non-enum line. These tests require the population to reappear on its own
// line, because a count that large silently leaving the only surface that
// reported it is the same defect as never counting it.

func TestDerivedKindsLineNamesTheKindsAndKeepsTheTotalsExact(t *testing.T) {
	line := DerivedKindsLine(27407, 1, map[string]int{"COMMIT_COUPLED": 27407})
	if line == "" {
		t.Fatal("27,407 derived edges rendered nothing")
	}
	for _, want := range []string{"27,407", "COMMIT_COUPLED"} {
		if !strings.Contains(line, want) {
			t.Errorf("line = %q, missing %q", line, want)
		}
	}
	// The line has to say WHAT these edges are, or a reader sees a large
	// number with no way to tell it apart from the gap the line above reports.
	if !strings.Contains(line, "DERIVED") {
		t.Errorf("line = %q, does not mark the population as derived", line)
	}
}

func TestDerivedKindsLineIsSilentWhenThereAreNoDerivedEdges(t *testing.T) {
	if got := DerivedKindsLine(0, 0, nil); got != "" {
		t.Errorf("DerivedKindsLine(0,0,nil) = %q, want \"\"", got)
	}
}

// TestDerivedKindsLineReportsUncappedTotalsWithATruncatedMap holds the same
// asymmetry as the non-enum line: the map reaching doctor is already truncated
// at the writer's cap, so the totals come from the arguments.
func TestDerivedKindsLineReportsUncappedTotalsWithATruncatedMap(t *testing.T) {
	line := DerivedKindsLine(900, 40, map[string]int{"A": 5, "B": 4})
	if !strings.Contains(line, "900") || !strings.Contains(line, "40 DERIVED") {
		t.Errorf("line = %q, want the UNCAPPED totals 900 / 40", line)
	}
	if !strings.Contains(line, "+38 more") {
		t.Errorf("line = %q, want the 38 unnamed kinds accounted for", line)
	}
}

// TestPrintDoctorHealthRendersBothPopulationsOnSeparateLines is the wiring,
// and the assertions are scoped PER LINE: a whole-output grep for
// "COMMIT_COUPLED" would still pass if the derived edges were folded back into
// the non-enum count, which is the exact regression this issue forbids.
func TestPrintDoctorHealthRendersBothPopulationsOnSeparateLines(t *testing.T) {
	g := &DoctorGroupHealth{
		GroupName: "grafel",
		Repos: []*DoctorRepoHealth{{
			Slug:                   "grafel",
			Status:                 "OK",
			Entities:               10,
			Relationships:          20,
			EdgesKindNotInEnum:     238,
			DistinctKindsNotInEnum: 5,
			KindsNotInEnum:         map[string]int{"STEP_IN_PROCESS": 175},
			EdgesDerivedKind:       27407,
			DistinctDerivedKinds:   1,
			DerivedKinds:           map[string]int{"COMMIT_COUPLED": 27407},
		}},
	}
	var buf bytes.Buffer
	PrintDoctorHealth(&buf, []*DoctorGroupHealth{g})

	var gapLine, derivedLine string
	for _, l := range strings.Split(buf.String(), "\n") {
		switch {
		case strings.Contains(l, "absent from the enum"):
			gapLine = l
		case strings.Contains(l, "DERIVED"):
			derivedLine = l
		}
	}
	if gapLine == "" {
		t.Fatalf("doctor rendered no non-enum line at all. Output:\n%s", buf.String())
	}
	if derivedLine == "" {
		t.Fatalf("doctor rendered no derived-kind line; declaring COMMIT_COUPLED removed 27,407 "+
			"edges from the line above and nothing reports them. Output:\n%s", buf.String())
	}
	if gapLine == derivedLine {
		t.Fatal("both populations were rendered as one line")
	}
	// Each line carries its own totals and its own names.
	if !strings.Contains(gapLine, "238") || !strings.Contains(gapLine, "STEP_IN_PROCESS") {
		t.Errorf("non-enum line = %q, want its own 238 / STEP_IN_PROCESS", gapLine)
	}
	if strings.Contains(gapLine, "COMMIT_COUPLED") || strings.Contains(gapLine, "27,407") {
		t.Errorf("non-enum line = %q has absorbed the derived population", gapLine)
	}
	if !strings.Contains(derivedLine, "27,407") || !strings.Contains(derivedLine, "COMMIT_COUPLED") {
		t.Errorf("derived line = %q, want its own 27,407 / COMMIT_COUPLED", derivedLine)
	}
	if strings.Contains(derivedLine, "STEP_IN_PROCESS") {
		t.Errorf("derived line = %q has absorbed the non-enum population", derivedLine)
	}

	// A repo with neither population must not gain either line.
	clean := &DoctorGroupHealth{
		GroupName: "grafel",
		Repos:     []*DoctorRepoHealth{{Slug: "grafel", Status: "OK", Entities: 10, Relationships: 20}},
	}
	var cleanBuf bytes.Buffer
	PrintDoctorHealth(&cleanBuf, []*DoctorGroupHealth{clean})
	if strings.Contains(cleanBuf.String(), "DERIVED") {
		t.Errorf("a repo with no derived edges gained a line:\n%s", cleanBuf.String())
	}
}

// seedDerivedSidecar6773 registers a single-repo group whose graph-stats.json
// carries a derived-kind tally, and returns doctor's RENDERED report.
//
// The sidecar is written with the real writer and read back by the real path
// (ComputeDoctorHealth → computeRepoHealth → the graph-stats.json decode), so
// what is under test is the shipping plumbing and not a struct literal. This
// is the half the rendering test above cannot reach: a doctor that stopped
// READING the sidecar's derived fields would still render them perfectly from
// a hand-built DoctorRepoHealth.
func seedDerivedSidecar6773(t *testing.T, group string, scanned bool) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRAFEL_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv(daemon.EnvRoot, t.TempDir())

	repoPath := filepath.Join(home, "repos", "coupled")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	side := &graph.GraphStatsSidecar{
		Version:       1,
		ComputedAt:    time.Now(),
		TotalEntities: 5,

		RelationshipKindsScanned:         scanned,
		RelationshipEdgesDerivedKind:     27407,
		RelationshipDistinctDerivedKinds: 1,
		RelationshipDerivedKinds:         map[string]int{"COMMIT_COUPLED": 27407},
	}
	if err := graph.WriteSidecar(graph.SidecarPath(stateDir), side, false); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(home, "groups", group+".json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`{"name":%q,"repos":[{"slug":"coupled","path":%q}]}`, group, repoPath)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := registry.AddGroup(group, cfgPath); err != nil {
		t.Fatal(err)
	}
	groups, err := registry.Groups()
	if err != nil {
		t.Fatalf("registry.Groups: %v", err)
	}
	reports := ComputeDoctorHealth(groups, false)

	// PREMISE GUARD: without it, "no derived line" below could pass because
	// the fixture produced no repo at all.
	if len(reports) != 1 || len(reports[0].Repos) != 1 {
		t.Fatalf("fixture produced %d group(s); nothing below observes the sidecar", len(reports))
	}
	if got := reports[0].Repos[0].Entities; got != 5 {
		t.Fatalf("fixture repo entities = %d, want 5 — the sidecar was never decoded, so its "+
			"derived fields were never on the table either", got)
	}
	var sb strings.Builder
	PrintDoctorHealth(&sb, reports)
	return sb.String()
}

// TestDoctorReadsTheDerivedCountsFromTheSidecar closes the read half. The
// sidecar is the ONLY source: nothing in the graph marks an edge as derived,
// so a consumer traversing it cannot tell.
func TestDoctorReadsTheDerivedCountsFromTheSidecar(t *testing.T) {
	out := seedDerivedSidecar6773(t, "derived6773", true)
	var derivedLine string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "DERIVED") {
			derivedLine = l
		}
	}
	if derivedLine == "" {
		t.Fatalf("doctor read the sidecar but reported no derived kinds. Output:\n%s", out)
	}
	if !strings.Contains(derivedLine, "27,407") || !strings.Contains(derivedLine, "COMMIT_COUPLED") {
		t.Errorf("derived line = %q, want the sidecar's 27,407 / COMMIT_COUPLED", derivedLine)
	}
}

// TestDoctorDoesNotReportDerivedCountsFromAnUNSCANNEDSidecar is the #6534
// direction, held for the derived half too: a sidecar written by a pass that
// never ran the tally must not have its (zero-valued, or stale) numbers
// reported as if something had counted them.
func TestDoctorDoesNotReportDerivedCountsFromAnUNSCANNEDSidecar(t *testing.T) {
	out := seedDerivedSidecar6773(t, "derived6773unscanned", false)
	if strings.Contains(out, "DERIVED") {
		t.Errorf("doctor reported derived kinds from a sidecar whose write path never tallied "+
			"them. Output:\n%s", out)
	}
}
