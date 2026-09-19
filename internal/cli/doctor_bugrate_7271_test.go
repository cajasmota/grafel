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
	"encoding/json"
	"fmt"
	"os"
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

// emptyToIDDoc is the reviewer's probe for #7271's second divergence: an
// IMPORTS edge with no target at all. audit classifies "" as unresolved;
// doctor's adjacency walk skips empty-ToID edges before its orphan bookkeeping,
// so for a while it measured a different edge population and printed
// "0.0% … ✓" for this graph while the dashboard said 50%.
//
//	2 IMPORTS edges, 1 bound to a hex id, 1 with to_id ""
//	→ 1 of 2 unresolved → 50.0%
func emptyToIDDoc(computedAt time.Time) *graph.Document {
	return &graph.Document{
		Version:     1,
		GeneratedAt: computedAt,
		Stats:       graph.Stats{Entities: 2, Relationships: 2, Files: 2},
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Name: "A", Kind: "function", SourceFile: "a.go", Language: "go"},
			{ID: "bbbbbbbbbbbbbbbb", Name: "B", Kind: "function", SourceFile: "b.go", Language: "go"},
		},
		Relationships: []graph.Relationship{
			{FromID: "bbbbbbbbbbbbbbbb", ToID: "aaaaaaaaaaaaaaaa", Kind: "IMPORTS"},
			{FromID: "bbbbbbbbbbbbbbbb", ToID: "", Kind: "IMPORTS"},
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
	return renderDoctorBugRateLineForRepos(t, doc)
}

// renderDoctorBugRateLineForRepos is the same wiring for a group of N repos —
// which is what a group normally is, and the axis the single-repo helper above
// cannot observe.
func renderDoctorBugRateLineForRepos(t *testing.T, docs ...*graph.Document) string {
	t.Helper()

	tmp := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmp)

	repos := make([]registry.Repo, 0, len(docs))
	for i, doc := range docs {
		slug := fmt.Sprintf("svc%d", i)
		repoPath := writeRepoWithGraph(t, tmp, slug, doc, doc.Stats.Entities, doc.Stats.Relationships)
		repos = append(repos, registry.Repo{Slug: slug, Path: repoPath, Stack: registry.StackList{"go"}})
	}

	cfgPath := filepath.Join(tmp, "group.json")
	cfg := &registry.GroupConfig{
		Name:  "g1",
		Repos: repos,
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
	// Forbidden row, end to end: this group's single repo WAS read, so there is
	// nothing partial to declare. A guard keyed on "any repo was read" instead
	// of "some repo was not" would print "PARTIAL: 0 of 1 repo graphs could not
	// be read" on every healthy group.
	if strings.Contains(line, "PARTIAL") {
		t.Errorf("a fully-read group is declared partial:\n  %s", line)
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
	if entries[0].BugRate == nil || *entries[0].BugRate != 25.0 {
		t.Errorf("recorded bug_rate = %v, want 25", entries[0].BugRate)
	}
	if entries[0].HealthScore == nil || *entries[0].HealthScore == 100 {
		t.Errorf("health score is a perfect 100 despite a 25%% bug rate: %+v", entries[0])
	}
}

// importsDocN builds a graph with exactly total IMPORTS edges, of which
// unresolved point at raw path strings the resolver never bound. Everything
// else about the graph is held constant across the repos built with it, so the
// only thing that varies between them is the rate itself.
func importsDocN(computedAt time.Time, total, unresolved int) *graph.Document {
	doc := &graph.Document{
		Version:     1,
		GeneratedAt: computedAt,
		Stats:       graph.Stats{Entities: 2, Relationships: total, Files: 2},
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Name: "A", Kind: "function", SourceFile: "a.go", Language: "go"},
			{ID: "bbbbbbbbbbbbbbbb", Name: "B", Kind: "function", SourceFile: "b.go", Language: "go"},
		},
	}
	for i := 0; i < total; i++ {
		to := "aaaaaaaaaaaaaaaa"
		if i < unresolved {
			to = fmt.Sprintf("./unresolved/%d", i)
		}
		doc.Relationships = append(doc.Relationships,
			graph.Relationship{FromID: "bbbbbbbbbbbbbbbb", ToID: to, Kind: "IMPORTS"})
	}
	return doc
}

// TestDoctorBugRate_GroupTotalPoolsEveryRepo is the cross-repo pin. `doctor`
// reports per GROUP, and a group is plural: the reported scenario is a Node
// backend whose imports are largely unresolved sitting beside a Next.js
// frontend whose imports mostly resolve. A group figure that silently reflects
// one of them is a confident wrong number — the same defect class this issue
// exists to fix, one level up.
//
// The two repos are chosen so that every wrong aggregation is distinguishable
// from the right one:
//
//	repo A   1 of 4 unresolved → 25.0%
//	repo B   3 of 6 unresolved → 50.0%
//	pooled   4 of 10           → 40.0%   ← the only correct answer
//	mean-of-means              → 37.5%
//
// so first-repo-only, last-repo-only and averaging the rates all produce a
// number this test rejects.
func TestDoctorBugRate_GroupTotalPoolsEveryRepo(t *testing.T) {
	now := time.Now()
	line := renderDoctorBugRateLineForRepos(t, importsDocN(now, 4, 1), importsDocN(now, 6, 3))

	if !strings.Contains(line, "40.0%") {
		t.Errorf("group rate is not the pooled 40.0%% of 10 import edges:\n  %s", line)
	}
	if !strings.Contains(line, "4 of 10 import edges unresolved") {
		t.Errorf("group counts are not the pooled 4-of-10:\n  %s", line)
	}
	for _, wrong := range []struct{ pct, why string }{
		{"25.0%", "only the first repo"},
		{"50.0%", "only the last repo"},
		{"37.5%", "the mean of the per-repo rates"},
	} {
		if strings.Contains(line, wrong.pct) {
			t.Errorf("group rate reflects %s (%s):\n  %s", wrong.why, wrong.pct, line)
		}
	}
}

// TestDoctorBugRate_GroupTotalSurvivesAnUnmeasurableRepo is the same axis with
// one repo contributing nothing: a repo with no IMPORTS edge must neither
// erase the group's measurement nor dilute it toward zero.
func TestDoctorBugRate_GroupTotalSurvivesAnUnmeasurableRepo(t *testing.T) {
	now := time.Now()
	line := renderDoctorBugRateLineForRepos(t, importsDocN(now, 4, 1), noImportsDoc(now))

	if !strings.Contains(line, "25.0%") {
		t.Errorf("a repo with no IMPORTS edges changed the group rate:\n  %s", line)
	}
	if strings.Contains(line, "not measured") {
		t.Errorf("an unmeasurable repo erased a measured group rate:\n  %s", line)
	}
}

// TestRebuildSummaryBugRate_PoolsEveryRepo is the same cross-repo axis on the
// outbound path. ComputeRebuildSummary accumulates across every rebuilt repo,
// and the webhook snapshot is built straight from that tally — so a group of
// two repos with different rates must broadcast the pooled figure, not one
// repo's.
//
//	repo A   1 of 4 unresolved → 25.0%
//	repo B   3 of 6 unresolved → 50.0%
//	pooled   4 of 10           → 40.0%   ← the only correct answer
//	mean-of-means              → 37.5%
func TestRebuildSummaryBugRate_PoolsEveryRepo(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmp)

	now := time.Now()
	docA := importsDocN(now, 4, 1)
	docB := importsDocN(now, 6, 3)
	repoA := writeRepoWithGraph(t, tmp, "svc0", docA, docA.Stats.Entities, docA.Stats.Relationships)
	repoB := writeRepoWithGraph(t, tmp, "svc1", docB, docB.Stats.Entities, docB.Stats.Relationships)

	sum := ComputeRebuildSummary("g1", []string{repoA, repoB}, 0)

	if sum.BugRate.TotalImports != 10 || sum.BugRate.ResolvedImports != 6 {
		t.Fatalf("pooled tally = %+v, want {10 6}", sum.BugRate)
	}
	if got := sum.BugRate.Pct(); got != 40.0 {
		t.Errorf("pooled bug rate = %v, want 40 (25 = first repo only, 50 = last repo only, 37.5 = mean of the per-repo rates)", got)
	}

	// The wire value the webhook actually carries.
	snap := rebuildQualitySnapshot("g1", sum, 70)
	if snap.BugRate == nil || *snap.BugRate != 40.0 {
		t.Errorf("snapshot bug_rate = %v, want the pooled 40", snap.BugRate)
	}
}

// TestDoctorBugRate_CountsEdgesWithNoTarget is the forbidden row for the edge
// population. An IMPORTS edge with an empty to_id is the most unresolved an
// edge can be; dropping it would shrink BOTH the numerator and the denominator,
// so the surface that exists to report unresolved imports would go quiet
// precisely when an import is maximally broken.
//
// Before the fix this rendered as "0.0% (0 of 1 import edges unresolved) ✓" —
// #7271 verbatim, on the branch that fixes #7271.
func TestDoctorBugRate_CountsEdgesWithNoTarget(t *testing.T) {
	line := renderDoctorBugRateLine(t, emptyToIDDoc(time.Now()))

	if !strings.Contains(line, "50.0%") {
		t.Errorf("an IMPORTS edge with no target is not counted as unresolved:\n  %s", line)
	}
	if !strings.Contains(line, "1 of 2 import edges unresolved") {
		t.Errorf("the empty-target edge is missing from the denominator too:\n  %s", line)
	}
	if strings.Contains(line, "✓") {
		t.Errorf("half the imports point nowhere and the line is marked healthy:\n  %s", line)
	}
}

// TestDoctorBugRate_AgreesWithTheAuditedRate_EmptyToID is the cross-surface pin
// extended to the class that defeated the original one: its fixture had no
// empty-to_id edge, so it could not observe a population difference at all.
func TestDoctorBugRate_AgreesWithTheAuditedRate_EmptyToID(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmp)

	doc := emptyToIDDoc(time.Now())
	repoPath := writeRepoWithGraph(t, tmp, "svc", doc, doc.Stats.Entities, doc.Stats.Relationships)

	cfgPath := filepath.Join(tmp, "group.json")
	cfg := &registry.GroupConfig{
		Name:  "g1",
		Repos: []registry.Repo{{Slug: "svc", Path: repoPath, Stack: registry.StackList{"go"}}},
	}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatalf("SaveGroupConfig: %v", err)
	}

	rep, err := audit.AuditPath(repoPath, false)
	if err != nil {
		t.Fatalf("AuditPath: %v", err)
	}
	if len(rep.Repos) != 1 {
		t.Fatalf("audit returned %d repos, want 1", len(rep.Repos))
	}
	audited := audit.BugRateFromReport(rep.Repos[0])
	if audited.TotalImports != 2 {
		t.Fatalf("audit saw %d import edges, want 2 — the fixture cannot pin the population", audited.TotalImports)
	}

	reports := ComputeDoctorHealth([]registry.GroupRef{{Name: "g1", ConfigPath: cfgPath}}, false)
	if len(reports) != 1 {
		t.Fatalf("got %d group reports, want 1", len(reports))
	}
	doctored := reports[0].BugRate

	if doctored != audited {
		t.Errorf("doctor and audit measure different edge populations: doctor=%+v audit=%+v", doctored, audited)
	}
	if doctored.Pct() != 50.0 {
		t.Errorf("both surfaces report %.4f%%, but 1 of the 2 import edges has no target (50%%)", doctored.Pct())
	}
}

// TestRebuildSummaryBugRate_CountsEdgesWithNoTarget is the same population on
// the third surface — scored on its own, because a DEAD verdict on doctor says
// nothing about the rebuild walk.
func TestRebuildSummaryBugRate_CountsEdgesWithNoTarget(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmp)

	doc := emptyToIDDoc(time.Now())
	repoPath := writeRepoWithGraph(t, tmp, "svc", doc, doc.Stats.Entities, doc.Stats.Relationships)

	sum := ComputeRebuildSummary("g1", []string{repoPath}, 0)
	if sum.BugRate.TotalImports != 2 || sum.BugRate.ResolvedImports != 1 {
		t.Fatalf("rebuild tally = %+v, want {2 1}", sum.BugRate)
	}
	if got := sum.BugRate.Pct(); got != 50.0 {
		t.Errorf("rebuild bug rate = %v, want 50", got)
	}
}

// TestBugRateLine_HealthyBandBoundary grades the band itself. Without a fixture
// on the boundary the constant could be set to 0 (nothing is ever healthy) or
// to 24.9 (the 25% case above passes as healthy) with the suite still green —
// the mark would be back to meaning nothing.
//
// The three tallies below are exact in binary: 100.0 * u / t is evaluated as
// (100*u)/t, so 2900/1000, 3000/1000 and 3100/1000 land on the doubles the
// literals below name.
func TestBugRateLine_HealthyBandBoundary(t *testing.T) {
	for _, tc := range []struct {
		name       string
		unresolved int
		total      int
		pct        float64
		wantMark   string
	}{
		{"just inside the band", 29, 1000, 2.9, "✓"},
		{"exactly on the band", 30, 1000, 3.0, "✓"},
		{"just outside the band", 31, 1000, 3.1, "⚠"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := audit.BugRate{TotalImports: tc.total, ResolvedImports: tc.total - tc.unresolved}
			if got := b.Pct(); got != tc.pct {
				t.Fatalf("fixture drifted: Pct = %v, want %v", got, tc.pct)
			}
			line := BugRateLine(b, 1, 1)
			if !strings.Contains(line, tc.wantMark) {
				t.Errorf("%.1f%% rendered without %s:\n  %s", tc.pct, tc.wantMark, line)
			}
			other := "⚠"
			if tc.wantMark == "⚠" {
				other = "✓"
			}
			if strings.Contains(line, other) {
				t.Errorf("%.1f%% rendered with %s:\n  %s", tc.pct, other, line)
			}
		})
	}
}

// TestDoctorBugRate_NoReadableGraphSaysSo covers the third state's OWN honesty.
// For a group whose repo was never indexed the old line still read "no IMPORTS
// edges found in this group's graphs" — but there were no graphs to find them
// in. Asserting an unchecked cause is the same unearned claim this issue is
// about, one layer down.
func TestDoctorBugRate_NoReadableGraphSaysSo(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmp)

	repoPath := filepath.Join(tmp, "never-indexed")
	if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(tmp, "group.json")
	cfg := &registry.GroupConfig{
		Name:  "g1",
		Repos: []registry.Repo{{Slug: "never-indexed", Path: repoPath, Stack: registry.StackList{"go"}}},
	}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatalf("SaveGroupConfig: %v", err)
	}
	reports := ComputeDoctorHealth([]registry.GroupRef{{Name: "g1", ConfigPath: cfgPath}}, false)
	if len(reports) != 1 {
		t.Fatalf("got %d group reports, want 1", len(reports))
	}
	if reports[0].ReposGraphRead != 0 {
		t.Fatalf("fixture read %d graphs, want 0", reports[0].ReposGraphRead)
	}

	var buf bytes.Buffer
	PrintDoctorHealth(&buf, reports)
	line := bugRateLineOf(t, buf.String())

	if !strings.Contains(line, "no repo graph in this group could be read") {
		t.Errorf("line blames the graphs for a group whose graphs were never read:\n  %s", line)
	}
	if strings.Contains(line, "no IMPORTS edges") {
		t.Errorf("line asserts a cause it never checked:\n  %s", line)
	}
	if strings.Contains(line, "✓") {
		t.Errorf("unreadable group rendered as healthy:\n  %s", line)
	}
}

// TestDoctorBugRate_PartialCoverageIsDeclared is the mixed case: one repo reads,
// one does not. The rate is real but drawn from part of the group, and the
// counts beside it would otherwise read as complete.
func TestDoctorBugRate_PartialCoverageIsDeclared(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmp)

	doc := importsDocN(time.Now(), 4, 1)
	readable := writeRepoWithGraph(t, tmp, "svc0", doc, doc.Stats.Entities, doc.Stats.Relationships)
	missing := filepath.Join(tmp, "svc1")
	if err := os.MkdirAll(filepath.Join(missing, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(tmp, "group.json")
	cfg := &registry.GroupConfig{
		Name: "g1",
		Repos: []registry.Repo{
			{Slug: "svc0", Path: readable, Stack: registry.StackList{"go"}},
			{Slug: "svc1", Path: missing, Stack: registry.StackList{"go"}},
		},
	}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatalf("SaveGroupConfig: %v", err)
	}
	reports := ComputeDoctorHealth([]registry.GroupRef{{Name: "g1", ConfigPath: cfgPath}}, false)
	if len(reports) != 1 {
		t.Fatalf("got %d group reports, want 1", len(reports))
	}
	if reports[0].ReposGraphRead != 1 {
		t.Fatalf("read %d of 2 graphs, want exactly 1 — the fixture cannot pin partial coverage", reports[0].ReposGraphRead)
	}

	var buf bytes.Buffer
	PrintDoctorHealth(&buf, reports)
	line := bugRateLineOf(t, buf.String())

	if !strings.Contains(line, "25.0%") {
		t.Errorf("the readable repo's rate is missing:\n  %s", line)
	}
	if !strings.Contains(line, "PARTIAL") {
		t.Errorf("a rate drawn from 1 of 2 repos is presented as complete:\n  %s", line)
	}
	if !strings.Contains(line, "1 of 2 repo graphs could not be read") {
		t.Errorf("the partial notice does not say how much is missing:\n  %s", line)
	}
}

// bugRateLineOf extracts the single rendered Bug-rate line from doctor output.
func bugRateLineOf(t *testing.T, out string) string {
	t.Helper()
	var found string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Bug-rate") {
			if found != "" {
				t.Fatalf("more than one Bug-rate line rendered:\n%s", out)
			}
			found = strings.TrimSpace(line)
		}
	}
	if found == "" {
		t.Fatalf("no Bug-rate line rendered:\n%s", out)
	}
	return found
}

// TestBugRateLine_RepoCoverageCounts grades the two repo counts the line
// renders. Both were unobservable before: the only PARTIAL fixture was 1-of-2,
// where read (1) and unread (1) are the same number, and the only
// nothing-measured fixture was 1-of-1, where read and total are the same
// number. Two different expressions rendered identically, so neither was pinned.
//
// Every discriminating row below picks reposRead and reposTotal so that read,
// total and (total-read) are three DIFFERENT numbers — never read == total and
// never total == 2*read — which is what makes "wrote the wrong one of the
// three" visible at all.
func TestBugRateLine_RepoCoverageCounts(t *testing.T) {
	healthy := audit.BugRate{TotalImports: 100, ResolvedImports: 100} // 0.0% ✓
	bad := audit.BugRate{TotalImports: 4, ResolvedImports: 3}         // 25.0% ⚠
	var unmeasured audit.BugRate

	for _, tc := range []struct {
		name       string
		b          audit.BugRate
		read       int
		total      int
		want       []string
		notWant    []string
		wantReason string
	}{
		{
			name: "fully read, one repo",
			b:    healthy, read: 1, total: 1,
			want:       []string{"0.0%", "✓"},
			notWant:    []string{"PARTIAL"},
			wantReason: "a fully covered group has nothing partial to declare, and 'PARTIAL: 0 … could not be read' contradicts itself",
		},
		{
			name: "fully read, three repos",
			b:    healthy, read: 3, total: 3,
			want:       []string{"0.0%"},
			notWant:    []string{"PARTIAL"},
			wantReason: "same, with reposRead > 1 so a guard keyed on 'read > 0' cannot pass here either",
		},
		{
			name: "one of three read",
			b:    bad, read: 1, total: 3,
			want: []string{"25.0%", "PARTIAL: 2 of 3 repo graphs could not be read"},
			// read=1, unread=2, total=3 — all different, so rendering the
			// read count or the total instead of the unread count is visible.
			notWant:    []string{"1 of 3 repo graphs could not be read", "3 of 3 repo graphs could not be read"},
			wantReason: "the count names how many repos are MISSING from the rate",
		},
		{
			name: "two of three read",
			b:    bad, read: 2, total: 3,
			want: []string{"PARTIAL: 1 of 3 repo graphs could not be read"},
			// The mirror of the row above: here the read count is the larger
			// number, so a swap shows up in the opposite direction.
			notWant:    []string{"2 of 3 repo graphs could not be read", "3 of 3 repo graphs could not be read"},
			wantReason: "same count, with read and unread exchanged",
		},
		{
			name: "nothing measured, one of three read",
			b:    unmeasured, read: 1, total: 3,
			want: []string{"not measured", "no IMPORTS edges in the 1 repo graph(s) read"},
			// Claiming 3 graphs were read when 1 was is an unchecked claim
			// inside the message added to stop making unchecked claims.
			notWant:    []string{"3 repo graph(s) read"},
			wantReason: "the count names how many graphs were actually READ, not how many exist",
		},
		{
			name: "nothing measured, two of five read",
			b:    unmeasured, read: 2, total: 5,
			want:       []string{"no IMPORTS edges in the 2 repo graph(s) read"},
			notWant:    []string{"5 repo graph(s) read"},
			wantReason: "second shape, wider gap between read and total",
		},
		{
			name: "nothing read at all",
			b:    unmeasured, read: 0, total: 2,
			want:       []string{"not measured (no repo graph in this group could be read)"},
			notWant:    []string{"IMPORTS", "repo graph(s) read", "PARTIAL"},
			wantReason: "with no graph read there is no edge population to describe, so the line must not describe one",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line := BugRateLine(tc.b, tc.read, tc.total)
			for _, w := range tc.want {
				if !strings.Contains(line, w) {
					t.Errorf("line is missing %q (%s):\n  %s", w, tc.wantReason, line)
				}
			}
			for _, nw := range tc.notWant {
				if strings.Contains(line, nw) {
					t.Errorf("line contains %q (%s):\n  %s", nw, tc.wantReason, line)
				}
			}
		})
	}
}

// fptr7283 wraps a float literal for the pointer-typed HealthEntry fields
// (#7283 — nil is "not measured", which a bare float64 could not express).
func fptr7283(f float64) *float64 { return &f }

// TestRecordHealthHistory_UnmeasuredRateIsNotPersistedAsZero is the same
// forbidden row one layer down from TestRebuildQualitySnapshot_UnmeasuredIsNull
// (#7283). The webhook snapshot could already say "unknown"; the persisted
// JSONL record — the one the dashboard's fidelity badge actually reads — could
// not, and stored a measured 0 with a health score computed from it.
//
// The assertion is on the serialised bytes: the whole point is what a later
// reader decodes, and a decoded-struct check cannot tell an absent key from a
// present zero on a field that is about to be made a pointer either way.
func TestRecordHealthHistory_UnmeasuredRateIsNotPersistedAsZero(t *testing.T) {
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)

	// A rebuild that counted entities and orphans but saw no IMPORTS edge at
	// all: the bug rate is undefined, not zero.
	sum := &RebuildSummary{Group: "g1", TotalEntities: 100, OrphanRate: 12.5}
	recordHealthHistory("g1", sum)

	b, err := os.ReadFile(filepath.Join(root, "health-history.jsonl"))
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	line := strings.TrimSpace(string(b))
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		t.Fatalf("decode history line %q: %v", line, err)
	}

	if v, ok := raw["bug_rate"]; ok {
		t.Errorf("no IMPORTS edge was measured, yet bug_rate=%s was persisted\nline: %s", v, line)
	}
	if v, ok := raw["health_score"]; ok {
		t.Errorf("bug rate unmeasured, yet health_score=%s was persisted — "+
			"ComputeHealthScore has no unknown state, so a 0 here inflates the score\nline: %s", v, line)
	}
	// Control: the rest of the record IS written, so the assertions above
	// cannot pass merely because nothing was persisted.
	if v, ok := raw["orphan_rate"]; !ok {
		t.Errorf("orphan_rate missing — the record was not written at all\nline: %s", line)
	} else if string(v) != "12.5" {
		t.Errorf("orphan_rate = %s, want 12.5\nline: %s", v, line)
	}
}
