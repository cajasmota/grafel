package docgen_test

// llm_bundle_module_supplements_test.go — unit tests for the ModuleReadme and
// ModuleConfigs fields populated by BuildBundle when the seed entity is a
// Module kind (#1880).
//
// Each test uses an isolated in-memory group (via env overrides), writes a
// graph.json with a Module entity + optional Config neighbours, and optionally
// writes README / config files to the temp repo directory.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/docgen"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/registry"
)

// ---------------------------------------------------------------------------
// Test harness helpers
// ---------------------------------------------------------------------------

// moduleConfigSpec describes one Config neighbour for the test harness.
type moduleConfigSpec struct {
	name  string
	props map[string]string
}

// moduleSupplementsHarness sets up an isolated test environment with a Module
// entity, optional Config neighbours, and optional filesystem files.
//
//   - groupSuffix: suffix appended to the group name for test isolation
//   - moduleSourceFile: SourceFile field for the Module entity (can be "")
//   - fsFiles: map of repo-relative path → file content to write on disk
//   - configSpecs: Config entities to add as DEPENDS_ON_CONFIG neighbours
func moduleSupplementsHarness(
	t *testing.T,
	groupSuffix string,
	moduleSourceFile string,
	fsFiles map[string]string,
	configSpecs []moduleConfigSpec,
) (groupName, moduleEntityID string) {
	t.Helper()

	tmp := t.TempDir()
	homeDir := filepath.Join(tmp, "home")
	xdgDir := filepath.Join(tmp, "xdg")
	daemonRoot := filepath.Join(tmp, "daemon")
	repoPath := filepath.Join(tmp, "repo")

	for _, d := range []string{homeDir, xdgDir, daemonRoot, repoPath} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	t.Setenv("ARCHIGRAPH_HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Setenv(daemon.EnvRoot, daemonRoot)

	groupName = "module-supplements-" + groupSuffix

	cfgPath, err := registry.ConfigPathFor(groupName)
	if err != nil {
		t.Fatalf("ConfigPathFor: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir fleet config dir: %v", err)
	}
	fleetJSON, _ := json.Marshal(map[string]interface{}{
		"name":  groupName,
		"repos": []map[string]interface{}{{"path": repoPath, "slug": "repo"}},
	})
	if err := os.WriteFile(cfgPath, fleetJSON, 0o644); err != nil {
		t.Fatalf("write fleet config: %v", err)
	}

	// Write filesystem files (README, pyproject.toml, package.json, etc.).
	for relPath, content := range fsFiles {
		absPath := filepath.Join(repoPath, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", relPath, err)
		}
		if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", relPath, err)
		}
	}

	// Build the Module entity.
	moduleID := graph.EntityID("repo", "Module", "mymodule", moduleSourceFile)

	entities := []graph.Entity{
		{
			ID:         moduleID,
			Name:       "mymodule",
			Kind:       "Module",
			Subtype:    "",
			SourceFile: moduleSourceFile,
			StartLine:  1,
			Language:   "",
		},
	}
	rels := []graph.Relationship{}

	// Add Config neighbours with DEPENDS_ON_CONFIG edges.
	for _, cs := range configSpecs {
		cfgID := graph.EntityID("repo", "SCOPE.Config", cs.name, cs.name)
		entities = append(entities, graph.Entity{
			ID:         cfgID,
			Name:       cs.name,
			Kind:       "SCOPE.Config",
			SourceFile: cs.name,
			Properties: cs.props,
		})
		rels = append(rels, graph.Relationship{
			ID:     graph.RelationshipID(moduleID, cfgID, "DEPENDS_ON_CONFIG"),
			FromID: moduleID,
			ToID:   cfgID,
			Kind:   "DEPENDS_ON_CONFIG",
		})
	}

	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}

	doc := graph.Document{
		Version:        1,
		GeneratedAt:    time.Now().UTC(),
		Repo:           repoPath,
		IndexerVersion: "test",
		Stats:          graph.Stats{Files: 1, Entities: len(entities), Relationships: len(rels)},
		Entities:       entities,
		Relationships:  rels,
	}
	docJSON, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal doc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "graph.json"), docJSON, 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}

	return groupName, moduleID
}

func buildBundleForModuleSupplements(t *testing.T, groupName, entityID string) *docgen.LLMPromptBundle {
	t.Helper()
	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: entityID,
			Section:      "overview",
			NoCache:      true,
		},
		Tier:    0,
		NoCache: true,
	}
	bundle, err := docgen.BuildBundle(context.Background(), opts)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	return bundle
}

// ---------------------------------------------------------------------------
// README tests
// ---------------------------------------------------------------------------

// TestBuildBundle_ModuleReadme_Found verifies that README.md in the module
// directory is embedded into graph_context.module_readme.
func TestBuildBundle_ModuleReadme_Found(t *testing.T) {
	readmeContent := "# My Module\n\nThis module does important things.\n"
	groupName, moduleID := moduleSupplementsHarness(t, "readme-found", "",
		map[string]string{"README.md": readmeContent},
		nil,
	)

	bundle := buildBundleForModuleSupplements(t, groupName, moduleID)

	if bundle.GraphContext.ModuleReadme == nil {
		t.Fatal("module_readme is nil — expected non-nil when README.md exists")
	}
	r := bundle.GraphContext.ModuleReadme
	if r.File == "" {
		t.Error("module_readme.file is empty")
	}
	if !strings.Contains(r.Content, "My Module") {
		t.Errorf("module_readme.content does not contain expected text; got: %q", r.Content)
	}
	if r.Language != "markdown" {
		t.Errorf("module_readme.language: got %q, want %q", r.Language, "markdown")
	}
}

// TestBuildBundle_ModuleReadme_NotFound verifies that module_readme is nil
// when no README exists in the module directory.
func TestBuildBundle_ModuleReadme_NotFound(t *testing.T) {
	groupName, moduleID := moduleSupplementsHarness(t, "readme-not-found", "",
		nil,
		nil,
	)

	bundle := buildBundleForModuleSupplements(t, groupName, moduleID)

	if bundle.GraphContext.ModuleReadme != nil {
		t.Errorf("expected module_readme nil when no README exists, got: %+v", bundle.GraphContext.ModuleReadme)
	}
}

// TestBuildBundle_ModuleReadme_Truncation verifies that a README with more than
// ModuleReadmeMaxLines lines is capped at that limit.
func TestBuildBundle_ModuleReadme_Truncation(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 600; i++ {
		sb.WriteString("line content here\n")
	}

	groupName, moduleID := moduleSupplementsHarness(t, "readme-truncation", "",
		map[string]string{"README.md": sb.String()},
		nil,
	)

	bundle := buildBundleForModuleSupplements(t, groupName, moduleID)

	if bundle.GraphContext.ModuleReadme == nil {
		t.Fatal("module_readme is nil")
	}
	lines := strings.Split(bundle.GraphContext.ModuleReadme.Content, "\n")
	// Allow off-by-one from trailing newline split.
	if len(lines) > docgen.ModuleReadmeMaxLines+1 {
		t.Errorf("module_readme.content has %d lines, want at most %d",
			len(lines), docgen.ModuleReadmeMaxLines)
	}
}

// TestBuildBundle_ModuleReadme_RST verifies that README.rst is found and
// language is set to "rst".
func TestBuildBundle_ModuleReadme_RST(t *testing.T) {
	groupName, moduleID := moduleSupplementsHarness(t, "readme-rst", "",
		map[string]string{"README.rst": "Module\n======\n\nSome docs.\n"},
		nil,
	)

	bundle := buildBundleForModuleSupplements(t, groupName, moduleID)

	if bundle.GraphContext.ModuleReadme == nil {
		t.Fatal("module_readme is nil for README.rst")
	}
	if bundle.GraphContext.ModuleReadme.Language != "rst" {
		t.Errorf("language: got %q, want %q", bundle.GraphContext.ModuleReadme.Language, "rst")
	}
}

// ---------------------------------------------------------------------------
// Config embed tests
// ---------------------------------------------------------------------------

// TestBuildBundle_ModuleConfigs_PyprojectEmbed verifies that a pyproject.toml
// Config neighbour is embedded with the correct fields.
func TestBuildBundle_ModuleConfigs_PyprojectEmbed(t *testing.T) {
	groupName, moduleID := moduleSupplementsHarness(t, "pyproject", "",
		nil,
		[]moduleConfigSpec{
			{
				name: "pyproject.toml",
				props: map[string]string{
					"format":         "toml",
					"subtype":        "python_project",
					"project_name":   "client-fixture-a",
					"dependencies":   "django,requests,celery",
					"scripts":        "test,lint",
					"keys_top_level": "build-system,project,tool",
				},
			},
		},
	)

	bundle := buildBundleForModuleSupplements(t, groupName, moduleID)

	if len(bundle.GraphContext.ModuleConfigs) == 0 {
		t.Fatal("module_configs is empty — expected pyproject.toml entry")
	}
	cfg := bundle.GraphContext.ModuleConfigs[0]
	if cfg.Name != "pyproject.toml" {
		t.Errorf("config name: got %q, want %q", cfg.Name, "pyproject.toml")
	}
	if cfg.ProjectName != "client-fixture-a" {
		t.Errorf("project_name: got %q, want %q", cfg.ProjectName, "client-fixture-a")
	}
	if cfg.Dependencies == "" {
		t.Error("dependencies is empty for pyproject.toml")
	}
	if cfg.Scripts == "" {
		t.Error("scripts is empty for pyproject.toml")
	}
	if cfg.KeysTopLevel == "" {
		t.Error("keys_top_level is empty for pyproject.toml")
	}
}

// TestBuildBundle_ModuleConfigs_PackageJSONEmbed verifies that a package.json
// Config neighbour is embedded with the correct fields.
func TestBuildBundle_ModuleConfigs_PackageJSONEmbed(t *testing.T) {
	groupName, moduleID := moduleSupplementsHarness(t, "packagejson", "",
		nil,
		[]moduleConfigSpec{
			{
				name: "package.json",
				props: map[string]string{
					"format":       "json",
					"subtype":      "node_project",
					"project_name": "client-fixture-b",
					"dependencies": "react,react-dom,axios",
					"scripts":      "build,test,dev",
				},
			},
		},
	)

	bundle := buildBundleForModuleSupplements(t, groupName, moduleID)

	if len(bundle.GraphContext.ModuleConfigs) == 0 {
		t.Fatal("module_configs is empty — expected package.json entry")
	}
	cfg := bundle.GraphContext.ModuleConfigs[0]
	if cfg.Name != "package.json" {
		t.Errorf("config name: got %q, want %q", cfg.Name, "package.json")
	}
	if cfg.ProjectName != "client-fixture-b" {
		t.Errorf("project_name: got %q, want %q", cfg.ProjectName, "client-fixture-b")
	}
	if !strings.Contains(cfg.Dependencies, "react") {
		t.Errorf("dependencies does not contain 'react': %q", cfg.Dependencies)
	}
	if cfg.Scripts == "" {
		t.Error("scripts is empty for package.json")
	}
	// package.json should NOT populate KeysTopLevel (spec: not in the named list).
	if cfg.KeysTopLevel != "" {
		t.Errorf("keys_top_level should be empty for package.json, got %q", cfg.KeysTopLevel)
	}
}

// TestBuildBundle_ModuleConfigs_PomXMLEmbed verifies that a pom.xml Config
// neighbour is embedded with project_name and dependencies.
func TestBuildBundle_ModuleConfigs_PomXMLEmbed(t *testing.T) {
	groupName, moduleID := moduleSupplementsHarness(t, "pomxml", "",
		nil,
		[]moduleConfigSpec{
			{
				name: "pom.xml",
				props: map[string]string{
					"format":       "xml",
					"subtype":      "maven_project",
					"project_name": "auth-service",
					"dependencies": "org.springframework.boot:spring-boot-starter,com.h2database:h2",
				},
			},
		},
	)

	bundle := buildBundleForModuleSupplements(t, groupName, moduleID)

	if len(bundle.GraphContext.ModuleConfigs) == 0 {
		t.Fatal("module_configs is empty — expected pom.xml entry")
	}
	cfg := bundle.GraphContext.ModuleConfigs[0]
	if cfg.Name != "pom.xml" {
		t.Errorf("config name: got %q, want %q", cfg.Name, "pom.xml")
	}
	if cfg.ProjectName != "auth-service" {
		t.Errorf("project_name: got %q, want %q", cfg.ProjectName, "auth-service")
	}
	if cfg.Dependencies == "" {
		t.Error("dependencies is empty for pom.xml")
	}
	// pom.xml should NOT populate Scripts or KeysTopLevel.
	if cfg.Scripts != "" {
		t.Errorf("scripts should be empty for pom.xml, got %q", cfg.Scripts)
	}
}

// TestBuildBundle_ModuleConfigs_GoModEmbed verifies that a go.mod Config
// neighbour is embedded with the module path as project_name.
func TestBuildBundle_ModuleConfigs_GoModEmbed(t *testing.T) {
	groupName, moduleID := moduleSupplementsHarness(t, "gomod", "",
		nil,
		[]moduleConfigSpec{
			{
				name: "go.mod",
				props: map[string]string{
					"format":       "go_mod",
					"subtype":      "go_module",
					"project_name": "github.com/client-fixture-c/backend",
					"dependencies": "github.com/gin-gonic/gin,github.com/jmoiron/sqlx",
				},
			},
		},
	)

	bundle := buildBundleForModuleSupplements(t, groupName, moduleID)

	if len(bundle.GraphContext.ModuleConfigs) == 0 {
		t.Fatal("module_configs is empty — expected go.mod entry")
	}
	cfg := bundle.GraphContext.ModuleConfigs[0]
	if cfg.Name != "go.mod" {
		t.Errorf("config name: got %q, want %q", cfg.Name, "go.mod")
	}
	if cfg.ProjectName != "github.com/client-fixture-c/backend" {
		t.Errorf("project_name: got %q, want %q", cfg.ProjectName, "github.com/client-fixture-c/backend")
	}
}

// TestBuildBundle_ModuleConfigs_GenericFallback verifies that an unrecognised
// config file name is embedded using the generic fallback path.
func TestBuildBundle_ModuleConfigs_GenericFallback(t *testing.T) {
	groupName, moduleID := moduleSupplementsHarness(t, "generic-cfg", "",
		nil,
		[]moduleConfigSpec{
			{
				name: "build.gradle",
				props: map[string]string{
					"format":         "gradle",
					"subtype":        "gradle_project",
					"dependencies":   "com.google.guava:guava,org.junit.jupiter:junit-jupiter",
					"keys_top_level": "plugins,dependencies,repositories",
				},
			},
		},
	)

	bundle := buildBundleForModuleSupplements(t, groupName, moduleID)

	if len(bundle.GraphContext.ModuleConfigs) == 0 {
		t.Fatal("module_configs is empty — expected build.gradle entry")
	}
	cfg := bundle.GraphContext.ModuleConfigs[0]
	if cfg.Name != "build.gradle" {
		t.Errorf("config name: got %q, want %q", cfg.Name, "build.gradle")
	}
	if cfg.Format != "gradle" {
		t.Errorf("format: got %q, want %q", cfg.Format, "gradle")
	}
	if cfg.Dependencies == "" {
		t.Error("dependencies is empty for gradle fallback")
	}
}

// TestBuildBundle_ModuleConfigs_NoConfigs verifies that module_configs is nil
// when there are no DEPENDS_ON_CONFIG edges from the Module.
func TestBuildBundle_ModuleConfigs_NoConfigs(t *testing.T) {
	groupName, moduleID := moduleSupplementsHarness(t, "no-configs", "",
		nil,
		nil,
	)

	bundle := buildBundleForModuleSupplements(t, groupName, moduleID)

	if len(bundle.GraphContext.ModuleConfigs) != 0 {
		t.Errorf("expected module_configs nil/empty, got %d entries",
			len(bundle.GraphContext.ModuleConfigs))
	}
}

// TestBuildBundle_ModuleConfigs_Max3Cap verifies that at most ModuleConfigMaxConfigs
// sibling configs are embedded even when more DEPENDS_ON_CONFIG neighbours exist.
func TestBuildBundle_ModuleConfigs_Max3Cap(t *testing.T) {
	configs := []moduleConfigSpec{
		{name: "pyproject.toml", props: map[string]string{"format": "toml", "subtype": "python_project"}},
		{name: "Makefile", props: map[string]string{"format": "makefile", "subtype": "makefile"}},
		{name: ".flake8", props: map[string]string{"format": "ini", "subtype": "python_flake8"}},
		{name: "tox.ini", props: map[string]string{"format": "ini", "subtype": "python_tox"}},
		{name: "mypy.ini", props: map[string]string{"format": "ini", "subtype": "python_mypy"}},
	}

	groupName, moduleID := moduleSupplementsHarness(t, "max3cap", "", nil, configs)

	bundle := buildBundleForModuleSupplements(t, groupName, moduleID)

	if len(bundle.GraphContext.ModuleConfigs) > docgen.ModuleConfigMaxConfigs {
		t.Errorf("module_configs has %d entries, want at most %d (cap)",
			len(bundle.GraphContext.ModuleConfigs), docgen.ModuleConfigMaxConfigs)
	}
}

// TestBuildBundle_ModuleSupplements_NonModuleNil verifies that non-Module entity
// kinds do NOT get module_readme or module_configs populated.
func TestBuildBundle_ModuleSupplements_NonModuleNil(t *testing.T) {
	tmp := t.TempDir()
	homeDir := filepath.Join(tmp, "home")
	xdgDir := filepath.Join(tmp, "xdg")
	daemonRoot := filepath.Join(tmp, "daemon")
	repoPath := filepath.Join(tmp, "repo")

	for _, d := range []string{homeDir, xdgDir, daemonRoot, repoPath} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	t.Setenv("ARCHIGRAPH_HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Setenv(daemon.EnvRoot, daemonRoot)

	// Write a README so we can verify it is NOT picked up for a non-Module seed.
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# Readme\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	groupName := "non-module-supplements-test"
	cfgPath, err := registry.ConfigPathFor(groupName)
	if err != nil {
		t.Fatalf("ConfigPathFor: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	fleetJSON, _ := json.Marshal(map[string]interface{}{
		"name":  groupName,
		"repos": []map[string]interface{}{{"path": repoPath, "slug": "repo"}},
	})
	if err := os.WriteFile(cfgPath, fleetJSON, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	fnID := graph.EntityID("repo", "SCOPE.Function", "doSomething", "src/util.go")

	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	doc := graph.Document{
		Version:        1,
		GeneratedAt:    time.Now().UTC(),
		Repo:           repoPath,
		IndexerVersion: "test",
		Entities: []graph.Entity{
			{
				ID:         fnID,
				Name:       "doSomething",
				Kind:       "SCOPE.Function",
				SourceFile: "src/util.go",
				StartLine:  1,
				Language:   "go",
			},
		},
	}
	docJSON, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(stateDir, "graph.json"), docJSON, 0o644); err != nil {
		t.Fatalf("write graph: %v", err)
	}

	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: fnID,
			Section:      "overview",
			NoCache:      true,
		},
		Tier:    0,
		NoCache: true,
	}

	bundle, err := docgen.BuildBundle(context.Background(), opts)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	if bundle.GraphContext.ModuleReadme != nil {
		t.Errorf("expected module_readme nil for Function entity, got %+v", bundle.GraphContext.ModuleReadme)
	}
	if len(bundle.GraphContext.ModuleConfigs) != 0 {
		t.Errorf("expected module_configs nil/empty for Function entity, got %v", bundle.GraphContext.ModuleConfigs)
	}
}

// TestBuildBundle_ModuleConfigs_KeysTopLevelCap verifies that keys_top_level
// strings exceeding ModuleConfigMaxKeys entries are capped with "+N more".
func TestBuildBundle_ModuleConfigs_KeysTopLevelCap(t *testing.T) {
	// Build a keys_top_level string with 70 comma-separated entries.
	keys := make([]string, 70)
	for i := range keys {
		keys[i] = "key" + strings.Repeat("x", i%5+1)
	}
	rawKeys := strings.Join(keys, ",")

	groupName, moduleID := moduleSupplementsHarness(t, "keys-cap", "",
		nil,
		[]moduleConfigSpec{
			{
				name: "setup.cfg",
				props: map[string]string{
					"format":         "ini",
					"subtype":        "python_project_legacy",
					"keys_top_level": rawKeys,
				},
			},
		},
	)

	bundle := buildBundleForModuleSupplements(t, groupName, moduleID)

	if len(bundle.GraphContext.ModuleConfigs) == 0 {
		t.Fatal("module_configs is empty")
	}
	cfg := bundle.GraphContext.ModuleConfigs[0]

	// Should be capped at ModuleConfigMaxKeys with "+N more" suffix.
	if !strings.Contains(cfg.KeysTopLevel, "+") {
		t.Errorf("keys_top_level should have '+N more' truncation marker, got: %q", cfg.KeysTopLevel)
	}
	parts := strings.Split(strings.Split(cfg.KeysTopLevel, ",+")[0], ",")
	if len(parts) > docgen.ModuleConfigMaxKeys {
		t.Errorf("keys_top_level has %d entries before truncation marker, want at most %d",
			len(parts), docgen.ModuleConfigMaxKeys)
	}
}
