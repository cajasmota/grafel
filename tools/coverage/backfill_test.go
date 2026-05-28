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

// TestCompletenessWarningsZeroAfterBackfill confirms that once backfill
// has seeded every declared lane cell, validateRegistry emits zero
// grouped-completeness warnings.
func TestCompletenessWarningsZeroAfterBackfill(t *testing.T) {
	dst := copyFixture(t)

	// Before: the fixture's grouped nestjs record is incomplete, so at
	// least one completeness warning must be present.
	before, err := loadRegistry(dst)
	if err != nil {
		t.Fatalf("load before: %v", err)
	}
	if n := countCompletenessWarnings(validateRegistry(before, repoRoot(t))); n == 0 {
		t.Fatal("fixture precondition: expected completeness warnings before backfill")
	}

	if _, _, err := runCmd(t, "backfill", "--file", dst); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	after, err := loadRegistry(dst)
	if err != nil {
		t.Fatalf("load after: %v", err)
	}
	if n := countCompletenessWarnings(validateRegistry(after, repoRoot(t))); n != 0 {
		t.Errorf("expected 0 completeness warnings after backfill, got %d", n)
	}
}

// countCompletenessWarnings counts warnings emitted by
// validateGroupedCompleteness (identified by their stable message stem).
func countCompletenessWarnings(res *ValidationResult) int {
	n := 0
	for _, w := range res.Warnings {
		if strings.Contains(w, "declared by subcategory") {
			n++
		}
	}
	return n
}
