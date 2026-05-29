package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFlatORMFixture writes a temp registry holding a single flat ORM
// record tagged subcategory orm_mapper, carrying the three legacy flat
// keys with payloads (status/cites/issue/verified_at) so the regroup
// move can be asserted to preserve them.
func writeFlatORMFixture(t *testing.T) string {
	t.Helper()
	reg := &Registry{
		SchemaVersion: SchemaVersion,
		Records: []Record{{
			ID:          "lang.python.orm.sqlalchemy",
			Category:    "orm",
			Subcategory: "orm_mapper",
			Language:    "python",
			Label:       "SQLAlchemy",
			Capabilities: map[string]Capability{
				"model_extraction":  {Status: StatusFull, Cites: []string{"internal/custom/python/sqlalchemy.go"}, VerifiedAt: "2026-05-01"},
				"query_attribution": {Status: StatusPartial, Issue: "https://example.com/i/q"},
				"migration_parsing": {Status: StatusMissing},
			},
		}},
	}
	dst := filepath.Join(t.TempDir(), "orm.json")
	if err := saveRegistry(dst, reg); err != nil {
		t.Fatalf("save flat ORM fixture: %v", err)
	}
	return dst
}

// TestRegroupMovesFlatKeysIntoCanonicalGroups confirms regroup relocates
// each flat key into its dictionary-declared group and preserves the full
// cell payload (status/cites/verified_at/issue) verbatim. After the move
// the record must read as grouped (no flat cells remain).
func TestRegroupMovesFlatKeysIntoCanonicalGroups(t *testing.T) {
	dst := writeFlatORMFixture(t)
	if _, _, err := runCmd(t, "regroup", "--file", dst); err != nil {
		t.Fatalf("regroup: %v", err)
	}
	reg, err := loadRegistry(dst)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	rec := findRecord(reg, "lang.python.orm.sqlalchemy")
	if rec == nil {
		t.Fatal("record missing after regroup")
	}
	if len(rec.Capabilities) != 0 {
		t.Errorf("flat capabilities not cleared after regroup: %v", rec.Capabilities)
	}
	if !rec.IsGrouped() {
		t.Fatal("record should be grouped after regroup")
	}

	// model_extraction → Models, preserving full + cites + verified_at.
	model, ok := rec.Groups["Models"]["model_extraction"]
	if !ok {
		t.Fatal("model_extraction not placed under Models")
	}
	if model.Status != StatusFull {
		t.Errorf("model_extraction status = %q, want full", model.Status)
	}
	if len(model.Cites) != 1 || model.Cites[0] != "internal/custom/python/sqlalchemy.go" {
		t.Errorf("model_extraction cites not preserved: %v", model.Cites)
	}
	if model.VerifiedAt != "2026-05-01" {
		t.Errorf("model_extraction verified_at not preserved: %q", model.VerifiedAt)
	}

	// query_attribution → Queries, preserving partial + issue.
	q, ok := rec.Groups["Queries"]["query_attribution"]
	if !ok {
		t.Fatal("query_attribution not placed under Queries")
	}
	if q.Status != StatusPartial || q.Issue != "https://example.com/i/q" {
		t.Errorf("query_attribution payload not preserved: %+v", q)
	}

	// migration_parsing → Migrations.
	if m, ok := rec.Groups["Migrations"]["migration_parsing"]; !ok || m.Status != StatusMissing {
		t.Errorf("migration_parsing not placed under Migrations as missing: %+v", m)
	}
}

// TestRegroupIdempotent confirms a second regroup run produces no
// byte-level change to the registry (nothing flat remains to move).
func TestRegroupIdempotent(t *testing.T) {
	dst := writeFlatORMFixture(t)
	if _, _, err := runCmd(t, "regroup", "--file", dst); err != nil {
		t.Fatalf("first regroup: %v", err)
	}
	after1, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read after first: %v", err)
	}
	out, _, err := runCmd(t, "regroup", "--file", dst)
	if err != nil {
		t.Fatalf("second regroup: %v", err)
	}
	if !strings.Contains(out, "nothing to move") {
		t.Errorf("second run should be a no-op, got: %s", out)
	}
	after2, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read after second: %v", err)
	}
	if string(after1) != string(after2) {
		t.Error("second regroup changed the registry; expected idempotent no-op")
	}
}

// TestRegroupThenValidateZeroErrors confirms that after regroup the record
// has no flat-shape-forbidden / shape errors (validate reports 0 errors
// for it). Backfill-able missing relationship cells are advisory warnings,
// not errors.
func TestRegroupThenValidateZeroErrors(t *testing.T) {
	dst := writeFlatORMFixture(t)
	if _, _, err := runCmd(t, "regroup", "--file", dst); err != nil {
		t.Fatalf("regroup: %v", err)
	}
	reg, err := loadRegistry(dst)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	res := validateRegistry(reg, repoRoot(t))
	var errs []string
	for _, e := range res.Errors {
		if strings.Contains(e, "lang.python.orm.sqlalchemy") {
			errs = append(errs, e)
		}
	}
	if len(errs) > 0 {
		t.Errorf("validate reported errors for regrouped record:\n%s", strings.Join(errs, "\n"))
	}
}

// TestRegroupDryRunWritesNothing confirms --dry-run prints the moves but
// leaves the registry byte-identical.
func TestRegroupDryRunWritesNothing(t *testing.T) {
	dst := writeFlatORMFixture(t)
	before, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}
	out, _, err := runCmd(t, "regroup", "--file", dst, "--dry-run")
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

// TestRegroupCheckReportsPending confirms --check exits non-zero when
// cells would be moved and writes nothing.
func TestRegroupCheckReportsPending(t *testing.T) {
	dst := writeFlatORMFixture(t)
	before, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}
	out, _, err := runCmd(t, "regroup", "--file", dst, "--check")
	if err == nil {
		t.Fatal("expected non-zero exit from --check with pending moves")
	}
	if !strings.Contains(out, "per-language pending-move counts:") {
		t.Errorf("expected per-language report, got:\n%s", out)
	}
	after, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(before) != string(after) {
		t.Error("--check mutated the registry")
	}
}

// TestRegroupSubcategoryFilter confirms --subcategory scopes the move to a
// single subcategory: a non-matching filter is a no-op.
func TestRegroupSubcategoryFilter(t *testing.T) {
	dst := writeFlatORMFixture(t)
	out, _, err := runCmd(t, "regroup", "--file", dst, "--subcategory", "validation_lib")
	if err != nil {
		t.Fatalf("regroup: %v", err)
	}
	if !strings.Contains(out, "nothing to move") {
		t.Errorf("non-matching subcategory filter should be a no-op, got: %s", out)
	}
}
