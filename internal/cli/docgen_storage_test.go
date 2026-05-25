package cli

// Tests for the storage-discipline helpers introduced by #2190 and the
// output-discipline helpers introduced by #2194:
//   - `archigraph docgen migrate-in-repo`   (#2190)
//   - `archigraph docgen audit`             (#2190)
//   - `archigraph docgen cleanup-scaffolding` (#2194)
//   - ssgArtifactReason / findSSGScaffoldingArtifacts unit tests
//
// Fixtures use synthetic ("client-fixture-X") directory names — no real
// client or product names appear in any test path.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/registry"
)

// makeFixtureGroup creates a synthetic group config under tmpDir that
// registers two repos: client-fixture-a and client-fixture-b.
// Each repo gets a .git directory so isDocgenOutput won't confuse them
// with store directories.
func makeFixtureGroup(t *testing.T, tmpDir string) (cfgPath string, repoA, repoB string) {
	t.Helper()

	repoA = filepath.Join(tmpDir, "client-fixture-a")
	repoB = filepath.Join(tmpDir, "client-fixture-b")
	for _, r := range []string{repoA, repoB} {
		if err := os.MkdirAll(filepath.Join(r, ".git"), 0o755); err != nil {
			t.Fatalf("create repo dir: %v", err)
		}
	}

	cfg := registry.GroupConfig{
		Name: "fixture-group",
		Repos: []registry.Repo{
			{Slug: "client-fixture-a", Path: repoA},
			{Slug: "client-fixture-b", Path: repoB},
		},
	}
	cfgPath = filepath.Join(tmpDir, "fixture-group.fleet.json")
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		t.Fatalf("write group config: %v", err)
	}
	return cfgPath, repoA, repoB
}

// plantDocgenMarker writes one of the heuristic marker files into dir/docs/
// so that isDocgenOutput returns true for that directory.
func plantDocgenMarker(t *testing.T, repoDir, markerFile string) string {
	t.Helper()
	docsDir := filepath.Join(repoDir, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("create docs dir: %v", err)
	}
	markerPath := filepath.Join(docsDir, markerFile)
	if err := os.WriteFile(markerPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	return docsDir
}

// ---------------------------------------------------------------------------
// isDocgenOutput unit tests
// ---------------------------------------------------------------------------

func TestIsDocgenOutput_NoMarkers(t *testing.T) {
	dir := t.TempDir()
	if isDocgenOutput(dir) {
		t.Error("empty dir should not be detected as docgen output")
	}
}

func TestIsDocgenOutput_PlanMd(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".plan.md"), []byte("# plan"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !isDocgenOutput(dir) {
		t.Error("dir with .plan.md should be detected as docgen output")
	}
}

func TestIsDocgenOutput_InventoryJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".inventory.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !isDocgenOutput(dir) {
		t.Error("dir with .inventory.json should be detected as docgen output")
	}
}

func TestIsDocgenOutput_MetadataJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".metadata.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !isDocgenOutput(dir) {
		t.Error("dir with .metadata.json should be detected as docgen output")
	}
}

func TestIsDocgenOutput_UnrelatedDocsDir(t *testing.T) {
	// A docs/ with only README.md should NOT be flagged.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# docs"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isDocgenOutput(dir) {
		t.Error("docs/ with only README.md should not be flagged as docgen output")
	}
}

// ---------------------------------------------------------------------------
// findInRepoDocgenDirs unit tests
// ---------------------------------------------------------------------------

func TestFindInRepoDocgenDirs_NonePresent(t *testing.T) {
	tmpDir := t.TempDir()
	_, repoA, repoB := makeFixtureGroup(t, tmpDir)
	// repos exist but have no docs/ at all
	_ = repoA
	_ = repoB

	cfgPath := filepath.Join(tmpDir, "fixture-group.fleet.json")
	cfg, err := registry.LoadGroupConfig(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	dirs := findInRepoDocgenDirs(cfg)
	if len(dirs) != 0 {
		t.Errorf("expected 0 dirs, got %d: %v", len(dirs), dirs)
	}
}

func TestFindInRepoDocgenDirs_OnePresent(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath, repoA, _ := makeFixtureGroup(t, tmpDir)
	plantDocgenMarker(t, repoA, ".plan.md")

	cfg, err := registry.LoadGroupConfig(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	dirs := findInRepoDocgenDirs(cfg)
	if len(dirs) != 1 {
		t.Fatalf("expected 1 dir, got %d: %v", len(dirs), dirs)
	}
	if !strings.HasSuffix(dirs[0], filepath.Join("client-fixture-a", "docs")) {
		t.Errorf("unexpected dir: %s", dirs[0])
	}
}

func TestFindInRepoDocgenDirs_BothPresent(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath, repoA, repoB := makeFixtureGroup(t, tmpDir)
	plantDocgenMarker(t, repoA, ".inventory.json")
	plantDocgenMarker(t, repoB, ".metadata.json")

	cfg, err := registry.LoadGroupConfig(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	dirs := findInRepoDocgenDirs(cfg)
	if len(dirs) != 2 {
		t.Fatalf("expected 2 dirs, got %d: %v", len(dirs), dirs)
	}
}

// ---------------------------------------------------------------------------
// migrate-in-repo: move happens when confirmed
// ---------------------------------------------------------------------------

func TestMigrateInRepo_MovesDir(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath, repoA, _ := makeFixtureGroup(t, tmpDir)

	// Plant docgen output inside repoA.
	srcDocs := plantDocgenMarker(t, repoA, ".plan.md")
	// Also write a real doc file so we can verify content moved.
	if err := os.WriteFile(filepath.Join(srcDocs, "overview.md"), []byte("# overview"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Override ARCHIGRAPH_HOME to a temp dir so we don't touch real user state.
	storeRoot := filepath.Join(tmpDir, "store")
	t.Setenv("ARCHIGRAPH_HOME", storeRoot)

	// Build the cobra command tree.
	root := newRoot()
	_ = cfgPath

	// We need to invoke migrate-in-repo --group fixture-group --yes
	// But the group config is at a non-standard path. Seed the registry.
	seedRegistry(t, tmpDir, cfgPath)

	root.SetArgs([]string{"docgen", "migrate-in-repo", "--group", "fixture-group", "--yes"})
	if err := root.Execute(); err != nil {
		t.Fatalf("migrate-in-repo failed: %v", err)
	}

	// Source should be gone.
	if _, err := os.Stat(srcDocs); !os.IsNotExist(err) {
		t.Errorf("source docs dir should have been removed, stat err: %v", err)
	}

	// Target should exist with the overview file.
	targetDocs := filepath.Join(storeRoot, "docs", "fixture-group", "client-fixture-a")
	if _, err := os.Stat(filepath.Join(targetDocs, "overview.md")); err != nil {
		t.Errorf("overview.md should exist in target: %v", err)
	}
}

// ---------------------------------------------------------------------------
// migrate-in-repo: idempotent (target already exists → skip)
// ---------------------------------------------------------------------------

func TestMigrateInRepo_IdempotentSkipsExisting(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath, repoA, _ := makeFixtureGroup(t, tmpDir)
	srcDocs := plantDocgenMarker(t, repoA, ".plan.md")

	storeRoot := filepath.Join(tmpDir, "store")
	t.Setenv("ARCHIGRAPH_HOME", storeRoot)

	// Pre-create the target so the idempotency guard triggers.
	targetDocs := filepath.Join(storeRoot, "docs", "fixture-group", "client-fixture-a")
	if err := os.MkdirAll(targetDocs, 0o755); err != nil {
		t.Fatal(err)
	}

	seedRegistry(t, tmpDir, cfgPath)

	root := newRoot()
	root.SetArgs([]string{"docgen", "migrate-in-repo", "--group", "fixture-group", "--yes"})
	if err := root.Execute(); err != nil {
		t.Fatalf("migrate-in-repo failed: %v", err)
	}

	// Source should STILL exist because the target was already there.
	if _, err := os.Stat(srcDocs); err != nil {
		t.Errorf("source docs dir should NOT have been removed (target existed): %v", err)
	}
}

// ---------------------------------------------------------------------------
// audit: detects without moving
// ---------------------------------------------------------------------------

func TestDocgenAudit_ReportsWithoutMoving(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath, repoA, _ := makeFixtureGroup(t, tmpDir)
	srcDocs := plantDocgenMarker(t, repoA, ".inventory.json")

	storeRoot := filepath.Join(tmpDir, "store")
	t.Setenv("ARCHIGRAPH_HOME", storeRoot)
	seedRegistry(t, tmpDir, cfgPath)

	root := newRoot()
	var out strings.Builder
	root.SetOut(&out)
	root.SetArgs([]string{"docgen", "audit", "--group", "fixture-group"})
	err := root.Execute()

	// audit returns a non-nil error (exit code 1) when offenders found.
	if err == nil {
		t.Error("audit should return non-nil error when offenders found")
	}

	// Source should still exist (audit never moves).
	if _, statErr := os.Stat(srcDocs); statErr != nil {
		t.Errorf("audit should not have moved source: %v", statErr)
	}

	output := out.String()
	if !strings.Contains(output, "client-fixture-a") {
		t.Errorf("audit output should mention the offending repo; got: %s", output)
	}
}

func TestDocgenAudit_CleanGroupReturnsNil(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath, _, _ := makeFixtureGroup(t, tmpDir)
	// No docgen markers planted.

	storeRoot := filepath.Join(tmpDir, "store")
	t.Setenv("ARCHIGRAPH_HOME", storeRoot)
	seedRegistry(t, tmpDir, cfgPath)

	root := newRoot()
	root.SetArgs([]string{"docgen", "audit", "--group", "fixture-group"})
	if err := root.Execute(); err != nil {
		t.Errorf("audit on clean group should return nil, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// doctor --audit-docs integration
// ---------------------------------------------------------------------------

func TestDoctorAuditDocs_ReportsOffenders(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath, repoA, _ := makeFixtureGroup(t, tmpDir)
	plantDocgenMarker(t, repoA, ".plan.md")

	storeRoot := filepath.Join(tmpDir, "store")
	t.Setenv("ARCHIGRAPH_HOME", storeRoot)
	seedRegistry(t, tmpDir, cfgPath)

	root := newRoot()
	var out strings.Builder
	root.SetOut(&out)
	root.SetArgs([]string{"doctor", "--audit-docs"})
	// doctor returns nil even with offenders (it's a report command).
	if err := root.Execute(); err != nil {
		t.Logf("doctor --audit-docs returned error (may be expected): %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Storage Discipline Audit") {
		t.Errorf("expected audit header in output; got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// DocsDirFor helper
// ---------------------------------------------------------------------------

func TestDocsDirFor(t *testing.T) {
	storeRoot := filepath.Join(t.TempDir(), "store")
	t.Setenv("ARCHIGRAPH_HOME", storeRoot)

	got, err := DocsDirFor("my-group")
	if err != nil {
		t.Fatalf("DocsDirFor: %v", err)
	}
	want := filepath.Join(storeRoot, "docs", "my-group")
	if got != want {
		t.Errorf("DocsDirFor = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// seedRegistry writes a minimal registry.json pointing to cfgPath so that
// resolveGroup("fixture-group") works in tests.
func seedRegistry(t *testing.T, tmpDir, cfgPath string) {
	t.Helper()
	storeRoot := os.Getenv("ARCHIGRAPH_HOME")
	if storeRoot == "" {
		storeRoot = filepath.Join(tmpDir, "store")
	}
	if err := os.MkdirAll(storeRoot, 0o755); err != nil {
		t.Fatalf("create store root: %v", err)
	}
	regPath := filepath.Join(storeRoot, "registry.json")
	reg := map[string]interface{}{
		"version": 1,
		"groups": []map[string]interface{}{
			{"name": "fixture-group", "config_path": cfgPath},
		},
	}
	data, _ := json.Marshal(reg)
	if err := os.WriteFile(regPath, data, 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
}

// ---------------------------------------------------------------------------
// OUTPUT DISCIPLINE (#2194) — ssgArtifactReason unit tests
// ---------------------------------------------------------------------------

func TestSsgArtifactReason_Vitepress(t *testing.T) {
	got := ssgArtifactReason(".vitepress", true)
	if got == "" {
		t.Error("expected non-empty reason for .vitepress dir")
	}
	if !strings.Contains(strings.ToLower(got), "vitepress") {
		t.Errorf("expected 'vitepress' in reason, got %q", got)
	}
}

func TestSsgArtifactReason_Docusaurus(t *testing.T) {
	got := ssgArtifactReason(".docusaurus", true)
	if got == "" {
		t.Error("expected non-empty reason for .docusaurus dir")
	}
}

func TestSsgArtifactReason_Sphinx(t *testing.T) {
	got := ssgArtifactReason("sphinx", true)
	if got == "" {
		t.Error("expected non-empty reason for sphinx dir")
	}
}

func TestSsgArtifactReason_MkdocsYml(t *testing.T) {
	got := ssgArtifactReason("mkdocs.yml", false)
	if got == "" {
		t.Error("expected non-empty reason for mkdocs.yml")
	}
}

func TestSsgArtifactReason_ConfigTs(t *testing.T) {
	got := ssgArtifactReason("config.ts", false)
	if got == "" {
		t.Error("expected non-empty reason for config.ts")
	}
}

func TestSsgArtifactReason_PackageJson(t *testing.T) {
	got := ssgArtifactReason("package.json", false)
	if got == "" {
		t.Error("expected non-empty reason for package.json at docs root")
	}
}

func TestSsgArtifactReason_CleanFile(t *testing.T) {
	for _, name := range []string{"index.md", "overview.md", "README.md", "score.json", "plan.md"} {
		got := ssgArtifactReason(name, false)
		if got != "" {
			t.Errorf("expected empty reason for clean file %q, got %q", name, got)
		}
	}
}

func TestSsgArtifactReason_VitepressAsFile(t *testing.T) {
	// .vitepress as a FILE (not dir) should not be flagged.
	got := ssgArtifactReason(".vitepress", false)
	if got != "" {
		t.Errorf("expected empty reason for .vitepress as file, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// OUTPUT DISCIPLINE (#2194) — findSSGScaffoldingArtifacts unit tests
// ---------------------------------------------------------------------------

func TestFindSSGScaffoldingArtifacts_NoArtifacts(t *testing.T) {
	tmpDir := t.TempDir()
	docsDir := filepath.Join(tmpDir, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Plant only clean markdown files.
	for _, f := range []string{"index.md", "overview.md"} {
		if err := os.WriteFile(filepath.Join(docsDir, f), []byte("# doc"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	artifacts := findSSGScaffoldingArtifacts([]string{docsDir})
	if len(artifacts) != 0 {
		t.Errorf("expected 0 artifacts in clean docs dir, got %d: %v", len(artifacts), artifacts)
	}
}

func TestFindSSGScaffoldingArtifacts_VitePress(t *testing.T) {
	tmpDir := t.TempDir()
	docsDir := filepath.Join(tmpDir, "docs")
	if err := os.MkdirAll(filepath.Join(docsDir, ".vitepress"), 0o755); err != nil {
		t.Fatal(err)
	}

	artifacts := findSSGScaffoldingArtifacts([]string{docsDir})
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact (.vitepress), got %d: %v", len(artifacts), artifacts)
	}
	if !strings.Contains(artifacts[0].path, ".vitepress") {
		t.Errorf("expected .vitepress in artifact path, got %q", artifacts[0].path)
	}
}

func TestFindSSGScaffoldingArtifacts_MkdocsYml(t *testing.T) {
	tmpDir := t.TempDir()
	docsDir := filepath.Join(tmpDir, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "mkdocs.yml"), []byte("site_name: test"), 0o644); err != nil {
		t.Fatal(err)
	}

	artifacts := findSSGScaffoldingArtifacts([]string{docsDir})
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact (mkdocs.yml), got %d: %v", len(artifacts), artifacts)
	}
}

func TestFindSSGScaffoldingArtifacts_MultipleRoots(t *testing.T) {
	tmpDir := t.TempDir()

	// Root 1: clean.
	root1 := filepath.Join(tmpDir, "clean")
	if err := os.MkdirAll(root1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root1, "index.md"), []byte("# doc"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Root 2: has .vitepress dir + mkdocs.yml.
	root2 := filepath.Join(tmpDir, "dirty")
	if err := os.MkdirAll(filepath.Join(root2, ".vitepress"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root2, "mkdocs.yml"), []byte("site_name: test"), 0o644); err != nil {
		t.Fatal(err)
	}

	artifacts := findSSGScaffoldingArtifacts([]string{root1, root2})
	if len(artifacts) != 2 {
		t.Errorf("expected 2 artifacts, got %d: %v", len(artifacts), artifacts)
	}
}

// TestCleanupScaffoldingCmd_RemovesArtifactsWithYes verifies the --yes flag
// auto-removes all detected SSG-scaffolding artifacts without prompting.
func TestCleanupScaffoldingCmd_RemovesArtifactsWithYes(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath, repoA, _ := makeFixtureGroup(t, tmpDir)
	t.Setenv("ARCHIGRAPH_HOME", filepath.Join(tmpDir, "store"))
	seedRegistry(t, tmpDir, cfgPath)

	// Plant a .vitepress dir in repoA/docs/ (simulating a misbehaving agent).
	repoADocs := filepath.Join(repoA, "docs")
	vitepressDir := filepath.Join(repoADocs, ".vitepress")
	if err := os.MkdirAll(vitepressDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vitepressDir, "config.ts"), []byte("export default {}"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Also plant a mkdocs.yml.
	if err := os.WriteFile(filepath.Join(repoADocs, "mkdocs.yml"), []byte("site_name: x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newDocgenCleanupScaffoldingCmd()
	cmd.SetArgs([]string{"--group", "fixture-group", "--yes"})

	var outBuf strings.Builder
	cmd.SetOut(&outBuf)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("cleanup-scaffolding returned error: %v", err)
	}

	out := outBuf.String()
	if !strings.Contains(out, "removed") {
		t.Errorf("expected 'removed' in output, got:\n%s", out)
	}

	// The artifacts must be gone.
	if _, statErr := os.Stat(vitepressDir); statErr == nil {
		t.Error(".vitepress dir still exists after cleanup-scaffolding --yes")
	}
	if _, statErr := os.Stat(filepath.Join(repoADocs, "mkdocs.yml")); statErr == nil {
		t.Error("mkdocs.yml still exists after cleanup-scaffolding --yes")
	}
}

// TestCleanupScaffoldingCmd_NoArtifacts verifies idempotency on a clean tree.
func TestCleanupScaffoldingCmd_NoArtifacts(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath, repoA, _ := makeFixtureGroup(t, tmpDir)
	t.Setenv("ARCHIGRAPH_HOME", filepath.Join(tmpDir, "store"))
	seedRegistry(t, tmpDir, cfgPath)

	// Clean docs dir (only markdown).
	repoADocs := filepath.Join(repoA, "docs")
	if err := os.MkdirAll(repoADocs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoADocs, "index.md"), []byte("# doc"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newDocgenCleanupScaffoldingCmd()
	cmd.SetArgs([]string{"--group", "fixture-group", "--yes"})

	var outBuf strings.Builder
	cmd.SetOut(&outBuf)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("cleanup-scaffolding on clean tree returned error: %v", err)
	}

	out := outBuf.String()
	if !strings.Contains(out, "No SSG-scaffolding artifacts detected") {
		t.Errorf("expected clean message, got:\n%s", out)
	}
}
