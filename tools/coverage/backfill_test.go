package main

import (
	"os"
	"strings"
	"testing"
)

// TestBackfillSeedsMissingLaneCells confirms backfill seeds at least one
// declared-but-absent lane cell and stamps it {missing, default issue}.
func TestBackfillSeedsMissingLaneCells(t *testing.T) {
	dst := copyFixture(t)
	if _, _, err := runCmd(t, "backfill", "--file", dst); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	reg, err := loadRegistry(dst)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	rec := findRecord(reg, "lang.jsts.framework.nestjs")
	if rec == nil {
		t.Fatal("nestjs record missing after backfill")
	}
	// route_extraction is declared under Routing in the http_backend
	// taxonomy but absent from the fixture — it must now exist as a
	// missing placeholder.
	seeded, ok := rec.Groups["Routing"]["route_extraction"]
	if !ok {
		t.Fatal("expected route_extraction seeded under Routing")
	}
	if seeded.Status != StatusMissing {
		t.Errorf("seeded status = %q, want %q", seeded.Status, StatusMissing)
	}
	if seeded.Issue != defaultBackfillIssue {
		t.Errorf("seeded issue = %q, want %q", seeded.Issue, defaultBackfillIssue)
	}
}

// TestBackfillIdempotent confirms a second backfill run produces no
// byte-level change to the registry.
func TestBackfillIdempotent(t *testing.T) {
	dst := copyFixture(t)
	if _, _, err := runCmd(t, "backfill", "--file", dst); err != nil {
		t.Fatalf("first backfill: %v", err)
	}
	after1, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read after first: %v", err)
	}
	if _, _, err := runCmd(t, "backfill", "--file", dst); err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	after2, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read after second: %v", err)
	}
	if string(after1) != string(after2) {
		t.Error("second backfill changed the registry; expected idempotent no-op")
	}
}

// TestBackfillNoClobber confirms an existing cell (any status, cites,
// verified_at) survives backfill untouched.
func TestBackfillNoClobber(t *testing.T) {
	dst := copyFixture(t)
	// endpoint_synthesis is a pre-set `full` cell with cites + verified_at
	// in the fixture; backfill must never touch it.
	before, err := loadRegistry(dst)
	if err != nil {
		t.Fatalf("load before: %v", err)
	}
	preset := findRecord(before, "lang.jsts.framework.nestjs").Groups["Routing"]["endpoint_synthesis"]
	if preset.Status != StatusFull {
		t.Fatalf("fixture precondition: endpoint_synthesis status = %q, want full", preset.Status)
	}

	if _, _, err := runCmd(t, "backfill", "--file", dst); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	after, err := loadRegistry(dst)
	if err != nil {
		t.Fatalf("load after: %v", err)
	}
	got := findRecord(after, "lang.jsts.framework.nestjs").Groups["Routing"]["endpoint_synthesis"]
	if got.Status != StatusFull {
		t.Errorf("endpoint_synthesis status changed to %q", got.Status)
	}
	if len(got.Cites) != len(preset.Cites) || (len(got.Cites) > 0 && got.Cites[0] != preset.Cites[0]) {
		t.Errorf("endpoint_synthesis cites changed: %v -> %v", preset.Cites, got.Cites)
	}
	if got.VerifiedAt != preset.VerifiedAt {
		t.Errorf("endpoint_synthesis verified_at changed: %q -> %q", preset.VerifiedAt, got.VerifiedAt)
	}
}

// TestBackfillCheckReportsPending confirms --check exits non-zero when
// cells would be seeded and prints the per-language report.
func TestBackfillCheckReportsPending(t *testing.T) {
	dst := copyFixture(t)
	out, _, err := runCmd(t, "backfill", "--file", dst, "--check")
	if err == nil {
		t.Fatal("expected non-zero exit from --check with pending seeds")
	}
	if !strings.Contains(out, "per-language pending-seed counts:") {
		t.Errorf("expected per-language report in output, got:\n%s", out)
	}
	// --check must not have written anything.
	out2, _, err := runCmd(t, "backfill", "--file", dst, "--check")
	if err == nil || out2 != out {
		t.Error("--check mutated the registry or behaved nondeterministically")
	}
}

// TestBackfillDryRunWritesNothing confirms --dry-run leaves the file
// byte-identical while still printing the tuples + counts.
func TestBackfillDryRunWritesNothing(t *testing.T) {
	dst := copyFixture(t)
	before, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}
	out, _, err := runCmd(t, "backfill", "--file", dst, "--dry-run")
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	after, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(before) != string(after) {
		t.Error("--dry-run wrote to the registry")
	}
	if !strings.Contains(out, "total:") {
		t.Errorf("expected total line in dry-run output, got:\n%s", out)
	}
}

// TestCompletenessErrorsZeroAfterBackfill confirms that once backfill has
// seeded every declared lane cell, validateRegistry emits zero grouped-
// completeness errors. (The gate was flipped to error in #2971; the
// function was previously named TestCompletenessWarningsZeroAfterBackfill.)
func TestCompletenessErrorsZeroAfterBackfill(t *testing.T) {
	dst := copyFixture(t)

	// Before: the fixture's grouped nestjs record is incomplete, so at
	// least one completeness error must be present.
	before, err := loadRegistry(dst)
	if err != nil {
		t.Fatalf("load before: %v", err)
	}
	if n := countCompletenessErrors(validateRegistry(before, repoRoot(t))); n == 0 {
		t.Fatal("fixture precondition: expected completeness errors before backfill")
	}

	if _, _, err := runCmd(t, "backfill", "--file", dst); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	after, err := loadRegistry(dst)
	if err != nil {
		t.Fatalf("load after: %v", err)
	}
	if n := countCompletenessErrors(validateRegistry(after, repoRoot(t))); n != 0 {
		t.Errorf("expected 0 completeness errors after backfill, got %d", n)
	}
}

// countCompletenessErrors counts errors emitted by
// validateGroupedCompleteness (identified by their stable message stem).
// Before #2971 these were warnings; the gate is now true so they are errors.
func countCompletenessErrors(res *ValidationResult) int {
	n := 0
	for _, e := range res.Errors {
		if strings.Contains(e, "declared by subcategory") {
			n++
		}
	}
	return n
}

// TestGroupedAuthoringFullCycle verifies the end-to-end authoring flow for
// a freshly `add`-ed record in a grouped subcategory (http_backend), whose
// capabilities field starts empty. Before this fix, `update` wrote flat
// cells and `backfill` skipped the record — both gates were IsGrouped()
// which is false until Groups is non-empty. Now both route by the
// subcategory's dictionary taxonomy instead.
//
//  1. `add` creates the record with an empty capabilities map.
//  2. `update` places the first cell into its canonical group (Routing).
//  3. `backfill` seeds every other declared lane cell.
//  4. `validate` reports 0 errors on the resulting record.
func TestGroupedAuthoringFullCycle(t *testing.T) {
	dst := copyFixture(t)
	const newID = "lang.jsts.framework.koa-grouped"

	// 1. add — creates record with Capabilities:{} and no Groups.
	if _, _, err := runCmd(t, "add", "--file", dst,
		"--id", newID,
		"--category", "http_framework",
		"--subcategory", "http_backend",
		"--language", "jsts",
		"--label", "Koa (grouped test)"); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Confirm the record is freshly flat (IsGrouped == false).
	before, err := loadRegistry(dst)
	if err != nil {
		t.Fatalf("load after add: %v", err)
	}
	fresh := findRecord(before, newID)
	if fresh == nil {
		t.Fatal("record not found after add")
	}
	if fresh.IsGrouped() {
		t.Fatal("precondition: freshly-added record should not be grouped yet")
	}

	// 2. update — must auto-place into the canonical group (Routing)
	// rather than writing a flat cell. Before the fix this wrote flat.
	if _, _, err := runCmd(t, "update", "--file", dst,
		"--capability", "endpoint_synthesis",
		"--status", "full",
		"--cites", "internal/engine/http_endpoint_synthesis.go",
		newID); err != nil {
		t.Fatalf("update: %v", err)
	}
	afterUpdate, err := loadRegistry(dst)
	if err != nil {
		t.Fatalf("load after update: %v", err)
	}
	updated := findRecord(afterUpdate, newID)
	if updated == nil {
		t.Fatal("record missing after update")
	}
	if !updated.IsGrouped() {
		t.Fatal("update should have produced a grouped record for a grouped subcategory")
	}
	if _, ok := updated.Groups["Routing"]["endpoint_synthesis"]; !ok {
		t.Fatal("update should have placed endpoint_synthesis under Routing")
	}
	if len(updated.Capabilities) > 0 {
		t.Fatalf("update must not write flat cells for a grouped subcategory; flat caps: %v", updated.Capabilities)
	}

	// 3. backfill — must seed the remaining declared lane cells that
	// `update` hasn't filled yet. Before the fix, backfill skipped
	// this record because IsGrouped() was false before step 2 set
	// the first group cell.
	if _, _, err := runCmd(t, "backfill", "--file", dst); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	afterBackfill, err := loadRegistry(dst)
	if err != nil {
		t.Fatalf("load after backfill: %v", err)
	}
	backfilled := findRecord(afterBackfill, newID)
	if backfilled == nil {
		t.Fatal("record missing after backfill")
	}
	// Every lane key in the http_backend taxonomy must now exist on the
	// record (either set by update or seeded by backfill).
	groups := groupsForSubcategory("http_backend")
	for _, g := range groups {
		for _, key := range g.Keys {
			all := backfilled.AllCapabilities()
			if _, ok := all[key]; !ok {
				t.Errorf("expected lane key %q (group %q) after backfill, but it is absent", key, g.Name)
			}
		}
	}

	// 4. validate — must report 0 errors on the resulting record.
	res := validateRegistry(afterBackfill, repoRoot(t))
	var newRecErrs []string
	for _, e := range res.Errors {
		if strings.Contains(e, newID) {
			newRecErrs = append(newRecErrs, e)
		}
	}
	if len(newRecErrs) > 0 {
		t.Errorf("validateRegistry reported errors for %s after full authoring cycle:\n%s",
			newID, strings.Join(newRecErrs, "\n"))
	}
}
