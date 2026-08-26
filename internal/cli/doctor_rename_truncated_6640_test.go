package cli

// #6640 — RenameDetectTruncated was written to the graph-stats.json sidecar so
// that a programmatic consumer could see a partial rename scan, and then no
// consumer was ever written: outside tests the only non-declaration reference
// was the WRITE at cmd/grafel/sidecar_build.go. The stderr warning the flag was
// meant to replace stayed the only surface.
//
// cmd/grafel/rename_truncation_report_6087_test.go already pins the WRITE half
// end-to-end (index a repo whose every function was renamed under a tiny budget
// → the sidecar flag is true; index one under the real budget → it is false).
// What follows pins the READ half, which nothing covered: that `grafel doctor`
// consumes the flag and SURFACES it.
//
// Everything here asserts on doctor's EMITTED OUTPUT — the bytes a human sees —
// never on DoctorRepoHealth.RenameTruncated. A test reading the field back
// would survive the exact mutant that matters ("the consumer stops reading
// it"), because the field would still be there and still be set by the sidecar
// decode. Both directions are asserted: DEGRADED when truncated, and NOT
// DEGRADED when not, so the reporting rule cannot be satisfied by a consumer
// that simply always complains.

import (
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

// seedGroup6640 registers a single-repo group whose sidecar carries the
// rename-detection outcome, and returns doctor's rendered health report for it.
//
// The sidecar is written with the REAL writer (graph.WriteSidecar) and read
// back by the REAL path (ComputeDoctorHealth → computeRepoHealth →
// graph-stats.json decode), so the plumbing under test is the shipping one.
func seedGroup6640(t *testing.T, group string, truncated bool, addedSkipped int) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRAFEL_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv(daemon.EnvRoot, t.TempDir())

	repoPath := filepath.Join(home, "repos", "legacy")
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

		RenameDetectTruncated:    truncated,
		RenameDetectAddedSkipped: addedSkipped,
	}
	if err := graph.WriteSidecar(graph.SidecarPath(stateDir), side, false); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(home, "groups", group+".json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`{"name":%q,"repos":[{"slug":"legacy","path":%q}]}`, group, repoPath)
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

	// PREMISE GUARD. Everything below reads a rendered string; if the fixture
	// silently produced no group at all (an unregistered group, a state dir the
	// sidecar never landed in, a repo path that failed os.Stat and came back
	// MISSING before the sidecar was ever decoded) then "output does not
	// contain DEGRADED" would pass for a reason that has nothing to do with
	// the flag, and the whole negative direction would be vacuous.
	if len(reports) != 1 {
		t.Fatalf("fixture produced %d groups, want exactly 1 — nothing below observes the flag", len(reports))
	}
	if n := len(reports[0].Repos); n != 1 {
		t.Fatalf("fixture group has %d repos, want exactly 1", n)
	}
	if got := reports[0].Repos[0].Status; got != "OK" {
		t.Fatalf("fixture repo status = %q, want OK — a STALE/MISSING repo makes the group DEGRADED "+
			"on its own and neither direction below would be attributable to the rename flag", got)
	}
	if reports[0].Repos[0].RebuildFailure != nil {
		t.Fatalf("fixture repo carries a rebuild failure, which is an independent DEGRADED trigger")
	}
	// The sidecar must actually have been found and decoded: without this, an
	// unread sidecar and a read-but-false one are indistinguishable.
	if reports[0].Repos[0].Entities != 5 {
		t.Fatalf("fixture repo entities = %d, want 5 — the sidecar was not read at all, "+
			"so the rename field in it was never on the table either", reports[0].Repos[0].Entities)
	}

	return renderDoctor6640(t, reports)
}

func renderDoctor6640(t *testing.T, reports []*DoctorGroupHealth) string {
	t.Helper()
	var sb strings.Builder
	PrintDoctorHealth(&sb, reports)
	out := sb.String()
	if out == "" {
		t.Fatal("PrintDoctorHealth emitted nothing")
	}
	return out
}

// TestDoctorReportsTruncatedRenameScan is the direction the defect was in: the
// flag is true and doctor said nothing at all.
func TestDoctorReportsTruncatedRenameScan(t *testing.T) {
	out := seedGroup6640(t, "acme", true, 1234)

	if !strings.Contains(out, "DEGRADED") {
		t.Errorf("doctor reports the group healthy after a TRUNCATED rename scan — "+
			"a consumer reading this index would report \"no renames\" off partial data.\n%s", out)
	}
	// The status word alone is not the artefact: a reader has to be told WHICH
	// repo and WHAT is incomplete, or DEGRADED is unactionable.
	if !strings.Contains(out, "TRUNCATED") {
		t.Errorf("doctor's output never says the rename scan was truncated.\n%s", out)
	}
	if !strings.Contains(out, "INCOMPLETE") {
		t.Errorf("doctor does not say the RENAMED_FROM edges are incomplete.\n%s", out)
	}
	if !strings.Contains(out, "RENAMED_FROM") {
		t.Errorf("doctor does not name the edge kind that is partial.\n%s", out)
	}
	if !strings.Contains(out, "legacy") {
		t.Errorf("doctor does not name the affected repo.\n%s", out)
	}
	// The count comes from the sidecar's companion field, so a wiring that
	// hard-codes a message without reading the numbers is caught too. fmtInt
	// group-separates, hence the comma: assert the RENDERED form.
	if !strings.Contains(out, "1,234") {
		t.Errorf("doctor does not report how many added entities were skipped (want 1,234).\n%s", out)
	}
	// Both surfaces, pinned SEPARATELY. Asserting only on the whole output
	// cannot tell them apart, and deleting the per-repo table line while
	// keeping the Issues entry (or vice versa) is a mutant that survived the
	// first draft of this test. The per-repo warning is what a reader sees
	// next to the offending repo; the Issues entry is what the summary and
	// the dashboard's diagnostics handler consume.
	table, issues, ok := splitDoctorOutput6640(out)
	if !ok {
		t.Fatalf("cannot locate doctor's per-repo table and Issues sections; "+
			"the section assertions below would be vacuous.\n%s", out)
	}
	if !strings.Contains(table, "TRUNCATED") {
		t.Errorf("the per-repo table carries no truncation warning beside the repo row.\n%s", out)
	}
	if !strings.Contains(table, "1,234") {
		t.Errorf("the per-repo warning omits the skipped-entity count.\n%s", out)
	}
	// The warning is ADDITIVE and ALIGNED: it must not have replaced the repo's
	// own status row, it must follow that row, and its marker must land in the
	// same column as the row's status field — the table is fixed-width and a
	// single deleted space in the indent silently ragged it. Checking a
	// leading-space PREFIX does not catch that: the slug-padding column absorbs
	// the missing space and the line still begins with four spaces (verified by
	// deleting one and watching the prefix check pass). Column equality is what
	// makes the byte load-bearing.
	var rowLine, warnLine string
	for _, line := range strings.Split(table, "\n") {
		switch {
		case strings.Contains(line, "rename detection TRUNCATED"):
			warnLine = line
		case strings.Contains(line, "legacy") && strings.Contains(line, "entities"):
			rowLine = line
		}
	}
	if rowLine == "" {
		t.Fatalf("the repo's own status row is gone — the warning replaced it "+
			"instead of being added beside it.\n%s", out)
	}
	if warnLine == "" {
		t.Fatalf("no truncation warning line in the per-repo table.\n%s", out)
	}
	if strings.Index(table, warnLine) < strings.Index(table, rowLine) {
		t.Errorf("the truncation warning is not printed under its repo row.\n%s", out)
	}
	// Everything left of the marker is ASCII, so byte offsets are columns.
	if got, want := strings.Index(warnLine, "⚠"), strings.Index(rowLine, "OK"); got != want {
		t.Errorf("truncation warning starts at column %d, the repo row's status column is %d — "+
			"the fixed-width table is ragged.\nrow:  %q\nwarn: %q", got, want, rowLine, warnLine)
	}
	if strings.Contains(issues, "[none]") {
		t.Errorf("truncation is absent from the Issues section.\n%s", out)
	}
	if !strings.Contains(issues, "TRUNCATED") {
		t.Errorf("Issues section does not carry the truncation.\n%s", out)
	}
}

// TestDoctorIsSilentOnCompleteRenameScan is the permissive direction. A doctor
// that flags every index equally satisfies the test above and is worthless;
// the flag only means something if a complete scan stays quiet.
func TestDoctorIsSilentOnCompleteRenameScan(t *testing.T) {
	out := seedGroup6640(t, "acme", false, 0)

	if strings.Contains(out, "DEGRADED") {
		t.Errorf("doctor reports DEGRADED on an index whose rename scan ran to completion.\n%s", out)
	}
	if !strings.Contains(out, "HEALTHY") {
		t.Errorf("doctor does not report the clean index as HEALTHY.\n%s", out)
	}
	if strings.Contains(out, "TRUNCATED") || strings.Contains(out, "RENAMED_FROM") {
		t.Errorf("doctor mentions rename truncation on a complete scan.\n%s", out)
	}
	_, issues, ok := splitDoctorOutput6640(out)
	if !ok {
		t.Fatalf("cannot locate doctor's sections; the assertion below is vacuous.\n%s", out)
	}
	if !strings.Contains(issues, "[none]") {
		t.Errorf("a clean index raises issues.\n%s", out)
	}
}

// A sidecar written before the field existed has no rename_detect_truncated key
// at all (it is `omitempty`). That must decode as "no truncation", not as a
// warning every pre-#6087 index would suddenly start emitting.
func TestDoctorTreatsAbsentRenameFieldAsComplete(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRAFEL_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv(daemon.EnvRoot, t.TempDir())

	repoPath := filepath.Join(home, "repos", "legacy")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Hand-written legacy payload: the key is genuinely absent, which is what
	// a re-marshalled struct with omitempty would also produce but for the
	// wrong reason. Guard that it really is absent.
	legacy := fmt.Sprintf(`{"version":1,"computed_at":%q,"total_entities":5}`,
		time.Now().Format(time.RFC3339Nano))
	if strings.Contains(legacy, "rename_detect") {
		t.Fatal("fixture is not a pre-field sidecar")
	}
	if err := os.WriteFile(graph.SidecarPath(stateDir), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(home, "groups", "legacygrp.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`{"name":"legacygrp","repos":[{"slug":"legacy","path":%q}]}`, repoPath)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := registry.AddGroup("legacygrp", cfgPath); err != nil {
		t.Fatal(err)
	}
	groups, err := registry.Groups()
	if err != nil {
		t.Fatal(err)
	}
	reports := ComputeDoctorHealth(groups, false)
	if len(reports) != 1 || len(reports[0].Repos) != 1 || reports[0].Repos[0].Entities != 5 {
		t.Fatalf("legacy sidecar was not read; nothing below is meaningful: %+v", reports)
	}

	out := renderDoctor6640(t, reports)
	if strings.Contains(out, "TRUNCATED") {
		t.Errorf("a pre-#6087 sidecar is reported as a truncated rename scan.\n%s", out)
	}
	if strings.Contains(out, "DEGRADED") {
		t.Errorf("a pre-#6087 sidecar makes the group DEGRADED.\n%s", out)
	}
}

// splitDoctorOutput6640 cuts doctor's rendered report into the per-repo table
// and the Issues section, so an assertion aimed at one cannot be satisfied by
// text that only appears in the other. Returns false when either marker is
// missing rather than slicing on a -1 index, so a renamed section header shows
// up as a failed premise instead of a silently-empty haystack that every
// "does not contain" assertion would pass against.
func splitDoctorOutput6640(out string) (table, issues string, ok bool) {
	q := strings.Index(out, "Quality:")
	i := strings.Index(out, "Issues found:")
	if q < 0 || i < 0 || q > i {
		return "", "", false
	}
	return out[:q], out[i:], true
}
