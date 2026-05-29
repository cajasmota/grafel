package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot returns the repository root, assuming tests run from
// tools/coverage/.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// tools/coverage -> ../../
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

// fixturePath returns the absolute path to testdata/fixture.json.
func fixturePath(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Join(wd, "testdata", "fixture.json")
}

// copyFixture copies the fixture into a temp file so write subcommands
// don't mutate the checked-in testdata.
func copyFixture(t *testing.T) string {
	t.Helper()
	src := fixturePath(t)
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dst := filepath.Join(t.TempDir(), "coverage.json")
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	return dst
}

func runCmd(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := run(args, &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

func TestList(t *testing.T) {
	out, _, err := runCmd(t, "list", "--file", fixturePath(t))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "lang.python.framework.django-drf") {
		t.Errorf("expected django-drf in output, got:\n%s", out)
	}
}

func TestListFilterByLanguage(t *testing.T) {
	out, _, err := runCmd(t, "list", "--file", fixturePath(t), "--by-language", "python")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if strings.Contains(out, "jsts") {
		t.Errorf("expected no jsts records when filtering by python, got:\n%s", out)
	}
	if !strings.Contains(out, "lang.python.framework.fastapi") {
		t.Errorf("expected fastapi in output, got:\n%s", out)
	}
}

func TestGet(t *testing.T) {
	out, _, err := runCmd(t, "get", "--file", fixturePath(t), "lang.python.framework.flask")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.Contains(out, "Flask") {
		t.Errorf("expected Flask label, got:\n%s", out)
	}
}

func TestGetMissing(t *testing.T) {
	_, _, err := runCmd(t, "get", "--file", fixturePath(t), "no.such.id")
	if err == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestGetJSON(t *testing.T) {
	out, _, err := runCmd(t, "get", "--file", fixturePath(t), "--json", "lang.python.framework.flask")
	if err != nil {
		t.Fatalf("get json: %v", err)
	}
	if !strings.Contains(out, "\"id\": \"lang.python.framework.flask\"") {
		t.Errorf("expected JSON id field, got:\n%s", out)
	}
}

func TestAddAndRemove(t *testing.T) {
	tmp := copyFixture(t)
	if _, _, err := runCmd(t, "add", "--file", tmp,
		"--id", "lang.go.framework.chi",
		"--category", "http_framework",
		"--language", "go",
		"--label", "Chi"); err != nil {
		t.Fatalf("add: %v", err)
	}
	out, _, err := runCmd(t, "get", "--file", tmp, "lang.go.framework.chi")
	if err != nil {
		t.Fatalf("get after add: %v", err)
	}
	if !strings.Contains(out, "Chi") {
		t.Errorf("expected Chi after add")
	}
	if _, _, err := runCmd(t, "remove", "--file", tmp, "lang.go.framework.chi"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, _, err := runCmd(t, "get", "--file", tmp, "lang.go.framework.chi"); err == nil {
		t.Fatal("expected error after remove")
	}
}

func TestAddDuplicate(t *testing.T) {
	tmp := copyFixture(t)
	_, _, err := runCmd(t, "add", "--file", tmp,
		"--id", "lang.python.framework.flask",
		"--category", "http_framework",
		"--language", "python",
		"--label", "dup")
	if err == nil {
		t.Fatal("expected duplicate-id error")
	}
}

func TestAddInvalidID(t *testing.T) {
	tmp := copyFixture(t)
	_, _, err := runCmd(t, "add", "--file", tmp,
		"--id", "BadID",
		"--category", "http_framework",
		"--language", "go",
		"--label", "x")
	if err == nil {
		t.Fatal("expected invalid-id error")
	}
}

func TestUpdate(t *testing.T) {
	tmp := copyFixture(t)
	if _, _, err := runCmd(t, "update", "--file", tmp,
		"--capability", "middleware_coverage",
		"--status", "missing",
		"--issue", "https://example.com/i/1",
		"lang.python.framework.flask"); err != nil {
		t.Fatalf("update: %v", err)
	}
	out, _, err := runCmd(t, "get", "--file", tmp, "--json", "lang.python.framework.flask")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.Contains(out, "middleware_coverage") {
		t.Errorf("expected middleware_coverage in record, got:\n%s", out)
	}
}

func TestUpdateInvalidCapability(t *testing.T) {
	tmp := copyFixture(t)
	_, _, err := runCmd(t, "update", "--file", tmp,
		"--capability", "not_a_real_key",
		"--status", "missing",
		"lang.python.framework.flask")
	if err == nil {
		t.Fatal("expected error for invalid capability key")
	}
}

func TestGaps(t *testing.T) {
	out, _, err := runCmd(t, "gaps", "--file", fixturePath(t))
	if err != nil {
		t.Fatalf("gaps: %v", err)
	}
	if !strings.Contains(out, "django-drf") {
		t.Errorf("expected django-drf (has partial auth_coverage) in gaps, got:\n%s", out)
	}
	if strings.Contains(out, "fastapi") {
		t.Errorf("fastapi has no gaps, should not appear")
	}
}

func TestStats(t *testing.T) {
	out, _, err := runCmd(t, "stats", "--file", fixturePath(t))
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if !strings.Contains(out, "total records:    7") {
		t.Errorf("expected 7 total records, got:\n%s", out)
	}
}

func TestStatsJSON(t *testing.T) {
	out, _, err := runCmd(t, "stats", "--file", fixturePath(t), "--json")
	if err != nil {
		t.Fatalf("stats json: %v", err)
	}
	if !strings.Contains(out, "\"total\": 7") {
		t.Errorf("expected total=7 in JSON, got:\n%s", out)
	}
}

func TestValidate(t *testing.T) {
	_, errout, err := runCmd(t, "validate", "--file", fixturePath(t), "--repo-root", repoRoot(t), "--skip-map")
	if err != nil {
		t.Fatalf("validate: %v\nstderr:\n%s", err, errout)
	}
}

func TestValidateRejectsBadCite(t *testing.T) {
	tmp := copyFixture(t)
	// Inject a bogus cite via update.
	if _, _, err := runCmd(t, "update", "--file", tmp,
		"--capability", "endpoint_synthesis",
		"--status", "full",
		"--cites", "this/file/does/not/exist.go",
		"lang.python.framework.flask"); err != nil {
		t.Fatalf("update: %v", err)
	}
	_, _, err := runCmd(t, "validate", "--file", tmp, "--repo-root", repoRoot(t), "--skip-map")
	if err == nil {
		t.Fatal("expected validate to fail on missing cite path")
	}
}

func TestSaveIsDeterministic(t *testing.T) {
	// Load, save twice, compare bytes.
	src := fixturePath(t)
	reg, err := loadRegistry(src)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	a := filepath.Join(t.TempDir(), "a.json")
	b := filepath.Join(t.TempDir(), "b.json")
	if err := saveRegistry(a, reg); err != nil {
		t.Fatalf("save a: %v", err)
	}
	if err := saveRegistry(b, reg); err != nil {
		t.Fatalf("save b: %v", err)
	}
	da, _ := os.ReadFile(a)
	db, _ := os.ReadFile(b)
	if !bytes.Equal(da, db) {
		t.Errorf("save output is non-deterministic")
	}
}

func TestUnknownSubcommand(t *testing.T) {
	_, _, err := runCmd(t, "nonsense")
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}

// ---------------------------------------------------------------------------
// Tests for auto-clear issue, --notes, and --clear-issue (#2952)
// ---------------------------------------------------------------------------

// TestUpdateFullAutoClearsIssue verifies that flipping a partial cell
// (which carries an issue tag) to "full" removes the issue automatically.
func TestUpdateFullAutoClearsIssue(t *testing.T) {
	tmp := copyFixture(t)
	// django-drf auth_coverage is partial with an issue; flip to full.
	if _, _, err := runCmd(t, "update", "--file", tmp,
		"--capability", "auth_coverage",
		"--status", "full",
		"lang.python.framework.django-drf"); err != nil {
		t.Fatalf("update: %v", err)
	}
	out, _, err := runCmd(t, "get", "--file", tmp, "--json", "lang.python.framework.django-drf")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// The issue should have been cleared automatically.
	if strings.Contains(out, "https://github.com/cajasmota/archigraph/issues/1942") {
		t.Errorf("expected issue to be auto-cleared on flip to full, but it persists:\n%s", out)
	}
}

// TestUpdateNotApplicableAutoClearsIssue verifies auto-clear for not_applicable.
func TestUpdateNotApplicableAutoClearsIssue(t *testing.T) {
	tmp := copyFixture(t)
	if _, _, err := runCmd(t, "update", "--file", tmp,
		"--capability", "auth_coverage",
		"--status", "not_applicable",
		"lang.python.framework.django-drf"); err != nil {
		t.Fatalf("update: %v", err)
	}
	out, _, err := runCmd(t, "get", "--file", tmp, "--json", "lang.python.framework.django-drf")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if strings.Contains(out, "https://github.com/cajasmota/archigraph/issues/1942") {
		t.Errorf("expected issue to be auto-cleared on flip to not_applicable, but it persists:\n%s", out)
	}
}

// TestUpdateFullWithExplicitIssuePreservesIssue verifies that --issue overrides
// the auto-clear so a caller can intentionally keep or set an issue on a full cell.
func TestUpdateFullWithExplicitIssuePreservesIssue(t *testing.T) {
	tmp := copyFixture(t)
	const keepIssue = "https://example.com/i/override"
	if _, _, err := runCmd(t, "update", "--file", tmp,
		"--capability", "auth_coverage",
		"--status", "full",
		"--issue", keepIssue,
		"lang.python.framework.django-drf"); err != nil {
		t.Fatalf("update: %v", err)
	}
	out, _, err := runCmd(t, "get", "--file", tmp, "--json", "lang.python.framework.django-drf")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.Contains(out, keepIssue) {
		t.Errorf("expected explicit --issue to be preserved on full cell, got:\n%s", out)
	}
}

// TestUpdatePartialKeepsIssue verifies that a partial/missing flip does NOT
// auto-clear the issue — only full/not_applicable cells are cleared.
func TestUpdatePartialKeepsIssue(t *testing.T) {
	tmp := copyFixture(t)
	// Start: django-drf auth_coverage is partial with an issue.
	// Update to missing (still a gap) without passing --issue.
	if _, _, err := runCmd(t, "update", "--file", tmp,
		"--capability", "auth_coverage",
		"--status", "missing",
		"lang.python.framework.django-drf"); err != nil {
		t.Fatalf("update: %v", err)
	}
	out, _, err := runCmd(t, "get", "--file", tmp, "--json", "lang.python.framework.django-drf")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// Issue should remain — the cell is still a gap.
	if !strings.Contains(out, "https://github.com/cajasmota/archigraph/issues/1942") {
		t.Errorf("expected issue to be preserved on partial→missing flip, got:\n%s", out)
	}
}

// TestUpdateNotesFlag verifies that --notes writes Capability.Notes.
func TestUpdateNotesFlag(t *testing.T) {
	tmp := copyFixture(t)
	const notesText = "JSX navigation only; class components not supported"
	if _, _, err := runCmd(t, "update", "--file", tmp,
		"--capability", "endpoint_synthesis",
		"--status", "full",
		"--notes", notesText,
		"lang.python.framework.flask"); err != nil {
		t.Fatalf("update: %v", err)
	}
	out, _, err := runCmd(t, "get", "--file", tmp, "--json", "lang.python.framework.flask")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.Contains(out, notesText) {
		t.Errorf("expected notes to be written, got:\n%s", out)
	}
}

// TestUpdateClearIssueFlag verifies that --clear-issue removes the issue field
// regardless of the new status.
func TestUpdateClearIssueFlag(t *testing.T) {
	tmp := copyFixture(t)
	// django-drf auth_coverage is partial with an issue; keep partial but drop issue.
	if _, _, err := runCmd(t, "update", "--file", tmp,
		"--capability", "auth_coverage",
		"--status", "partial",
		"--clear-issue",
		"lang.python.framework.django-drf"); err != nil {
		t.Fatalf("update: %v", err)
	}
	out, _, err := runCmd(t, "get", "--file", tmp, "--json", "lang.python.framework.django-drf")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if strings.Contains(out, "https://github.com/cajasmota/archigraph/issues/1942") {
		t.Errorf("expected --clear-issue to remove issue on partial cell, but it persists:\n%s", out)
	}
}

// TestUpdatePositionalAfterFlagsError verifies the "flags must precede record-id"
// error message is emitted when the positional arg count is wrong.
func TestUpdateMissingPositional(t *testing.T) {
	tmp := copyFixture(t)
	_, _, err := runCmd(t, "update", "--file", tmp,
		"--capability", "endpoint_synthesis",
		"--status", "full")
	if err == nil {
		t.Fatal("expected error when record-id positional is missing")
	}
	if !strings.Contains(err.Error(), "expected exactly one ID argument") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestGenDispatch exercises the gen subcommand through the top-level
// run() dispatcher so the wiring in main.go is covered alongside the
// generator itself. Deeper rendering behaviour lives in gen_test.go.
func TestGenDispatch(t *testing.T) {
	root := t.TempDir()
	out, _, err := runCmd(t, "gen", "--file", fixturePath(t), "--out", root)
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	if !strings.Contains(out, "generated") {
		t.Errorf("expected confirmation, got: %s", out)
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "coverage", "summary.md")); err != nil {
		t.Errorf("summary.md missing: %v", err)
	}
}
