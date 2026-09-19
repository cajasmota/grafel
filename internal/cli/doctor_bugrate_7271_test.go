package cli

// doctor_bugrate_7271_test.go — the `grafel doctor` Quality section must report
// a REAL unresolved-import rate, and must not decorate an unmeasured one with a
// health mark (#7271).
//
// Every assertion here is on the RENDERED line, not on the struct field: the
// reported defect was half a hardcoded metric (BugRate = 0.0) and half a
// hardcoded verdict (the "✓" was a string literal argument to Fprintf), and a
// struct-level assertion is blind to the second half.

import (
	"bytes"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/quality"
	"github.com/cajasmota/grafel/internal/quality/audit"
	"github.com/cajasmota/grafel/internal/registry"
)

// importsDoc builds a graph whose IMPORTS edges are deliberately mixed:
//
//	4 IMPORTS edges total
//	3 resolved  — two 16-hex entity ids and one qualified ext: placeholder
//	1 unresolved — a raw relative path string the resolver never bound
//
// so the group's unresolved-import rate is 25.0% — a number that is neither 0
// (which the placeholder produced) nor 100, and which no rounding can confuse
// with either.
func importsDoc(computedAt time.Time) *graph.Document {
	return &graph.Document{
		Version:     1,
		GeneratedAt: computedAt,
		Stats:       graph.Stats{Entities: 3, Relationships: 4, Files: 3},
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Name: "A", Kind: "function", SourceFile: "a.go", Language: "go"},
			{ID: "bbbbbbbbbbbbbbbb", Name: "B", Kind: "function", SourceFile: "b.go", Language: "go"},
			{ID: "cccccccccccccccc", Name: "C", Kind: "function", SourceFile: "c.go", Language: "go"},
		},
		Relationships: []graph.Relationship{
			{FromID: "cccccccccccccccc", ToID: "aaaaaaaaaaaaaaaa", Kind: "IMPORTS"},
			{FromID: "cccccccccccccccc", ToID: "bbbbbbbbbbbbbbbb", Kind: "IMPORTS"},
			{FromID: "cccccccccccccccc", ToID: "ext:react:useState", Kind: "IMPORTS"},
			{FromID: "cccccccccccccccc", ToID: "./unresolved/target", Kind: "IMPORTS"},
		},
	}
}

// noImportsDoc has entities and edges but not one IMPORTS edge, so there is
// nothing from which an unresolved-import rate could be derived.
func noImportsDoc(computedAt time.Time) *graph.Document {
	return &graph.Document{
		Version:     1,
		GeneratedAt: computedAt,
		Stats:       graph.Stats{Entities: 2, Relationships: 1, Files: 2},
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Name: "A", Kind: "function", SourceFile: "a.go", Language: "go"},
			{ID: "bbbbbbbbbbbbbbbb", Name: "B", Kind: "function", SourceFile: "b.go", Language: "go"},
		},
		Relationships: []graph.Relationship{
			{FromID: "aaaaaaaaaaaaaaaa", ToID: "bbbbbbbbbbbbbbbb", Kind: "CALLS"},
		},
	}
}

// renderDoctorBugRateLine wires a one-repo group through the real
// ComputeDoctorHealth + PrintDoctorHealth path and returns the single rendered
// "Bug-rate" line.
func renderDoctorBugRateLine(t *testing.T, doc *graph.Document) string {
	t.Helper()

	tmp := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmp)

	repoPath := writeRepoWithGraph(t, tmp, "svc", doc, doc.Stats.Entities, doc.Stats.Relationships)

	cfgPath := filepath.Join(tmp, "group.json")
	cfg := &registry.GroupConfig{
		Name:  "g1",
		Repos: []registry.Repo{{Slug: "svc", Path: repoPath, Stack: registry.StackList{"go"}}},
	}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatalf("SaveGroupConfig: %v", err)
	}

	reports := ComputeDoctorHealth([]registry.GroupRef{{Name: "g1", ConfigPath: cfgPath}}, false)
	if len(reports) != 1 {
		t.Fatalf("got %d group reports, want 1", len(reports))
	}

	var buf bytes.Buffer
	PrintDoctorHealth(&buf, reports)

	var found string
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, "Bug-rate") {
			if found != "" {
				t.Fatalf("more than one Bug-rate line rendered:\n%s", buf.String())
			}
			found = strings.TrimSpace(line)
		}
	}
	if found == "" {
		t.Fatalf("no Bug-rate line rendered:\n%s", buf.String())
	}
	return found
}

// TestDoctorBugRate_RendersMeasuredRate is the primary regression pin: the rate
// printed by doctor must be derived from the graph it just read. One of the four
// IMPORTS edges is unresolved, so the line must say 25.0%.
func TestDoctorBugRate_RendersMeasuredRate(t *testing.T) {
	line := renderDoctorBugRateLine(t, importsDoc(time.Now()))

	if !strings.Contains(line, "25.0%") {
		t.Errorf("bug-rate line does not report the measured 25.0%% rate (#7271 placeholder?):\n  %s", line)
	}
	if strings.Contains(line, "0.0%") {
		t.Errorf("bug-rate line reports 0.0%% for a graph with an unresolved import:\n  %s", line)
	}
}

// TestDoctorBugRate_MeasuredBadRateIsNotMarkedHealthy pins the second half of
// the defect: the "✓" was a literal, so wiring a real number up without
// touching it would print a genuinely bad rate under a checkmark — a stronger
// false assurance than the obvious 0.0% was.
func TestDoctorBugRate_MeasuredBadRateIsNotMarkedHealthy(t *testing.T) {
	line := renderDoctorBugRateLine(t, importsDoc(time.Now()))

	if strings.Contains(line, "✓") {
		t.Errorf("a 25.0%% unresolved-import rate is rendered with a healthy mark:\n  %s", line)
	}
}

// numberWithCheck matches "<number>% ✓" — the shape that says "this figure was
// measured and it is fine".
var numberWithCheck = regexp.MustCompile(`[0-9]+\.[0-9]+%[^\n]*✓`)

// TestDoctorBugRate_NotMeasuredIsNotRenderedAsZero is the forbidden row for the
// third state. A group whose graphs contain no IMPORTS edge has no
// unresolved-import rate at all; rendering that as "0.0% ✓" is the exact
// falsehood #7271 reports, and a naive wiring (guard the division on a nonzero
// denominator, leave the zero value alone) reproduces it byte for byte.
func TestDoctorBugRate_NotMeasuredIsNotRenderedAsZero(t *testing.T) {
	line := renderDoctorBugRateLine(t, noImportsDoc(time.Now()))

	if numberWithCheck.MatchString(line) {
		t.Errorf("unmeasurable bug-rate rendered as a healthy number:\n  %s", line)
	}
	if strings.Contains(line, "✓") {
		t.Errorf("unmeasurable bug-rate rendered with a healthy mark:\n  %s", line)
	}
	if strings.Contains(line, "0.0%") {
		t.Errorf("unmeasurable bug-rate rendered as 0.0%%:\n  %s", line)
	}
	if !strings.Contains(line, "not measured") {
		t.Errorf("unmeasurable bug-rate does not say so:\n  %s", line)
	}
}

// TestDoctorBugRate_AgreesWithTheAuditedRate is the cross-surface pin behind
// #7271: the reporter watched `grafel doctor` say 0.0% while the dashboard
// showed 33%. The dashboard derives its figure by auditing the repo
// (audit.AuditPath → audit.BugRateFromReport); doctor derives its own by
// walking the graph it already loaded. Two different traversals, one shared
// derivation — and this test fails if they ever stop landing on the same
// number for the same repo.
//
// Nothing here recomputes the rate: both sides of the comparison are
// production code, and the literal 25.0% above pins the pair to a known truth
// so they cannot agree on a wrong answer.
func TestDoctorBugRate_AgreesWithTheAuditedRate(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmp)

	doc := importsDoc(time.Now())
	repoPath := writeRepoWithGraph(t, tmp, "svc", doc, doc.Stats.Entities, doc.Stats.Relationships)

	cfgPath := filepath.Join(tmp, "group.json")
	cfg := &registry.GroupConfig{
		Name:  "g1",
		Repos: []registry.Repo{{Slug: "svc", Path: repoPath, Stack: registry.StackList{"go"}}},
	}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatalf("SaveGroupConfig: %v", err)
	}

	// The dashboard's route into the metric.
	rep, err := audit.AuditPath(repoPath, false)
	if err != nil {
		t.Fatalf("AuditPath: %v", err)
	}
	if len(rep.Repos) != 1 {
		t.Fatalf("audit returned %d repos, want 1", len(rep.Repos))
	}
	audited := audit.BugRateFromReport(rep.Repos[0])
	if !audited.Known() {
		t.Fatalf("audit measured no import edges; the fixture cannot pin agreement")
	}

	// Doctor's route into the metric.
	reports := ComputeDoctorHealth([]registry.GroupRef{{Name: "g1", ConfigPath: cfgPath}}, false)
	if len(reports) != 1 {
		t.Fatalf("got %d group reports, want 1", len(reports))
	}
	doctored := reports[0].BugRate

	if doctored.TotalImports != audited.TotalImports || doctored.ResolvedImports != audited.ResolvedImports {
		t.Errorf("doctor and audit disagree on the import tally: doctor=%+v audit=%+v", doctored, audited)
	}
	if doctored.Pct() != audited.Pct() {
		t.Errorf("doctor reports %.4f%%, the audited (dashboard) figure is %.4f%%", doctored.Pct(), audited.Pct())
	}
	if doctored.Pct() != 25.0 {
		t.Errorf("both surfaces agree on %.4f%%, but the fixture's rate is 25.0%%", doctored.Pct())
	}
}

// TestRebuildSummaryBugRate_IsMeasured covers the outbound half of #7271: the
// rebuild path fed a hardcoded BugRate: 0 into the quality webhook payload, and
// a webhook consumer has no source to read to discover the number is fake. The
// snapshot is built straight from this tally, so pinning the tally pins what
// goes on the wire.
func TestRebuildSummaryBugRate_IsMeasured(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmp)

	doc := importsDoc(time.Now())
	repoPath := writeRepoWithGraph(t, tmp, "svc", doc, doc.Stats.Entities, doc.Stats.Relationships)

	sum := ComputeRebuildSummary("g1", []string{repoPath}, 0)
	if !sum.BugRate.Known() {
		t.Fatalf("rebuild summary measured no bug rate for a graph with 4 IMPORTS edges")
	}
	if got := sum.BugRate.Pct(); got != 25.0 {
		t.Errorf("rebuild summary bug rate = %.4f%%, want 25.0%%", got)
	}
	if p := sum.BugRate.PctPtr(); p == nil || *p != 25.0 {
		t.Errorf("webhook-bound pointer = %v, want a 25.0%% value", p)
	}
}

// TestRebuildSummaryBugRate_UnmeasuredIsNil is the forbidden row for the
// outbound half: with nothing to measure the payload must carry an explicit
// null, never a 0 that reads as a clean bill of health.
func TestRebuildSummaryBugRate_UnmeasuredIsNil(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmp)

	doc := noImportsDoc(time.Now())
	repoPath := writeRepoWithGraph(t, tmp, "svc", doc, doc.Stats.Entities, doc.Stats.Relationships)

	sum := ComputeRebuildSummary("g1", []string{repoPath}, 0)
	if sum.BugRate.Known() {
		t.Fatalf("rebuild summary claims to have measured a bug rate with no IMPORTS edges: %+v", sum.BugRate)
	}
	if p := sum.BugRate.PctPtr(); p != nil {
		t.Errorf("webhook-bound pointer = %v, want nil for an unmeasured rate", *p)
	}
}

// healthyImportsDoc has the same four IMPORTS edges, all of them resolved — a
// group whose bug rate was genuinely measured and is genuinely fine.
func healthyImportsDoc(computedAt time.Time) *graph.Document {
	doc := importsDoc(computedAt)
	doc.Relationships[3].ToID = "aaaaaaaaaaaaaaaa"
	return doc
}

// TestDoctorBugRate_MeasuredHealthyRateKeepsTheMark is the other end of the
// pattern. Deleting the "✓" outright would satisfy every assertion above, so
// this pins that a measured, healthy rate still earns the mark — and that the
// mark now means something, since the 25% case above does not get one.
func TestDoctorBugRate_MeasuredHealthyRateKeepsTheMark(t *testing.T) {
	line := renderDoctorBugRateLine(t, healthyImportsDoc(time.Now()))

	if !strings.Contains(line, "0.0%") {
		t.Errorf("a fully resolved group should report 0.0%%:\n  %s", line)
	}
	if !strings.Contains(line, "✓") {
		t.Errorf("a measured, healthy bug rate lost its mark:\n  %s", line)
	}
	if strings.Contains(line, "not measured") {
		t.Errorf("a measured rate is reported as unmeasured:\n  %s", line)
	}
}

// TestRebuildQualitySnapshot_CarriesTheMeasuredRate pins the payload the
// rebuild path actually broadcasts. The tally is measured; the snapshot must
// carry it rather than the literal 0 it used to send.
func TestRebuildQualitySnapshot_CarriesTheMeasuredRate(t *testing.T) {
	sum := &RebuildSummary{
		Group:         "g1",
		TotalEntities: 100,
		OrphanRate:    12.5,
		BugRate:       audit.BugRate{TotalImports: 4, ResolvedImports: 3},
	}

	snap := rebuildQualitySnapshot("g1", sum, 80)
	if snap.BugRate == nil {
		t.Fatalf("snapshot dropped a measured bug rate: %+v", snap)
	}
	if *snap.BugRate != 25.0 {
		t.Errorf("snapshot bug_rate = %v, want 25", *snap.BugRate)
	}
	if snap.OrphanRate != 12.5 {
		t.Errorf("snapshot orphan_rate = %v, want 12.5 (control)", snap.OrphanRate)
	}
}

// TestRebuildQualitySnapshot_UnmeasuredIsNull is the forbidden row on the wire:
// no number at all when none was measured.
func TestRebuildQualitySnapshot_UnmeasuredIsNull(t *testing.T) {
	sum := &RebuildSummary{Group: "g1", TotalEntities: 100, OrphanRate: 12.5}

	snap := rebuildQualitySnapshot("g1", sum, 80)
	if snap.BugRate != nil {
		t.Errorf("snapshot bug_rate = %v for an unmeasured rate, want null", *snap.BugRate)
	}
	if snap.OrphanRate != 12.5 {
		t.Errorf("snapshot orphan_rate = %v, want 12.5 (control: the rest of the snapshot is still built)", snap.OrphanRate)
	}
}

// TestRecordHealthHistory_StoresTheMeasuredRate covers the persisted record the
// dashboard's fidelity reading is derived from. It was written with a hardcoded
// 0 bug rate AND scored with one, so a group with a third of its imports
// unresolved was recorded as flawless.
func TestRecordHealthHistory_StoresTheMeasuredRate(t *testing.T) {
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)

	sum := &RebuildSummary{
		Group:         "g1",
		TotalEntities: 100,
		OrphanRate:    0,
		BugRate:       audit.BugRate{TotalImports: 4, ResolvedImports: 3},
	}
	recordHealthHistory("g1", sum)

	entries, err := quality.ReadHistory(root, "g1", 3650)
	if err != nil {
		t.Fatalf("ReadHistory: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d history entries, want 1", len(entries))
	}
	if entries[0].BugRate != 25.0 {
		t.Errorf("recorded bug_rate = %v, want 25", entries[0].BugRate)
	}
	if entries[0].HealthScore == 100 {
		t.Errorf("health score is a perfect 100 despite a 25%% bug rate: %+v", entries[0])
	}
}
