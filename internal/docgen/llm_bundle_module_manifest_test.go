package docgen_test

// llm_bundle_module_manifest_test.go — unit tests for the ModuleManifest field
// populated by BuildBundle when the seed entity is a Module kind (#1881).
//
// Each test sets up an isolated in-memory group (via env overrides), writes a
// graph.json with a Module entity and child entities connected by CONTAINS and
// IMPORTS edges, and asserts that bundle.GraphContext.ModuleManifest is
// correctly populated.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/docgen"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/registry"
)

// ---------------------------------------------------------------------------
// Test harness
// ---------------------------------------------------------------------------

// moduleManifestHarness creates an isolated test environment with a Module
// entity that has class, function, constant, import, endpoint, and model
// children connected by CONTAINS and IMPORTS edges.
//
// Returns the groupName, module entity ID, and a cleanup func.
func moduleManifestHarness(t *testing.T) (groupName, moduleEntityID string) {
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

	t.Setenv("GRAFEL_HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Setenv(daemon.EnvRoot, daemonRoot)

	groupName = "module-manifest-test-group"

	// Write fleet config.
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

	// Build entity IDs.
	moduleID := graph.EntityID("repo", "SCOPE.Module", "com.example.api", "src/main/java/com/example/api")
	controllerID := graph.EntityID("repo", "SCOPE.Controller", "UserController", "src/main/java/com/example/api/UserController.java")
	serviceID := graph.EntityID("repo", "SCOPE.Service", "UserService", "src/main/java/com/example/api/UserService.java")
	modelID := graph.EntityID("repo", "SCOPE.Model", "UserDTO", "src/main/java/com/example/api/UserDTO.java")
	hookID := graph.EntityID("repo", "SCOPE.Operation", "useUsers", "src/main/java/com/example/api/hooks.js")
	constID := graph.EntityID("repo", "SCOPE.Schema", "MAX_PAGE_SIZE", "src/main/java/com/example/api/constants.java")
	importedModID := graph.EntityID("repo", "SCOPE.Module", "com.example.core", "src/main/java/com/example/core")
	endpointID := graph.EntityID("repo", "SCOPE.http_endpoint_definition", "GET /users", "src/main/java/com/example/api/UserController.java")

	// Write graph.json.
	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}

	doc := graph.Document{
		Version:        1,
		GeneratedAt:    time.Now().UTC(),
		Repo:           repoPath,
		IndexerVersion: "test",
		Stats:          graph.Stats{Files: 5, Entities: 8, Relationships: 7},
		Entities: []graph.Entity{
			{
				ID:         moduleID,
				Name:       "com.example.api",
				Kind:       "SCOPE.Module",
				SourceFile: "src/main/java/com/example/api",
				Language:   "java",
			},
			{
				ID:         controllerID,
				Name:       "UserController",
				Kind:       "SCOPE.Controller",
				Subtype:    "class",
				SourceFile: "src/main/java/com/example/api/UserController.java",
				StartLine:  10,
				Language:   "java",
				Signature:  "@RestController class UserController",
			},
			{
				ID:         serviceID,
				Name:       "UserService",
				Kind:       "SCOPE.Service",
				Subtype:    "class",
				SourceFile: "src/main/java/com/example/api/UserService.java",
				StartLine:  5,
				Language:   "java",
			},
			{
				ID:         modelID,
				Name:       "UserDTO",
				Kind:       "SCOPE.Model",
				Subtype:    "model",
				SourceFile: "src/main/java/com/example/api/UserDTO.java",
				StartLine:  3,
				Language:   "java",
			},
			{
				ID:         hookID,
				Name:       "useUsers",
				Kind:       "SCOPE.Operation",
				Subtype:    "hook",
				SourceFile: "src/main/java/com/example/api/hooks.js",
				StartLine:  1,
				Language:   "javascript",
				Signature:  "export function useUsers()",
			},
			graph.Entity{
				ID:         constID,
				Name:       "MAX_PAGE_SIZE",
				Kind:       "SCOPE.Schema",
				Subtype:    "constant",
				SourceFile: "src/main/java/com/example/api/constants.java",
				StartLine:  2,
			}.WithProperties(map[string]string{"value_literal": "100"}),
			{
				ID:         importedModID,
				Name:       "com.example.core",
				Kind:       "SCOPE.Module",
				SourceFile: "src/main/java/com/example/core",
				Language:   "java",
			},
			graph.Entity{
				ID:         endpointID,
				Name:       "GET /users",
				Kind:       "SCOPE.http_endpoint_definition",
				SourceFile: "src/main/java/com/example/api/UserController.java",
				StartLine:  15,
			}.WithProperties(map[string]string{"http_method": "GET", "path": "/users"}),
		},
		Relationships: []graph.Relationship{
			{
				ID:     graph.RelationshipID(moduleID, controllerID, "CONTAINS"),
				FromID: moduleID,
				ToID:   controllerID,
				Kind:   "CONTAINS",
			},
			{
				ID:     graph.RelationshipID(moduleID, serviceID, "CONTAINS"),
				FromID: moduleID,
				ToID:   serviceID,
				Kind:   "CONTAINS",
			},
			{
				ID:     graph.RelationshipID(moduleID, modelID, "CONTAINS"),
				FromID: moduleID,
				ToID:   modelID,
				Kind:   "CONTAINS",
			},
			{
				ID:     graph.RelationshipID(moduleID, hookID, "CONTAINS"),
				FromID: moduleID,
				ToID:   hookID,
				Kind:   "CONTAINS",
			},
			{
				ID:     graph.RelationshipID(moduleID, constID, "CONTAINS"),
				FromID: moduleID,
				ToID:   constID,
				Kind:   "CONTAINS",
			},
			{
				ID:     graph.RelationshipID(moduleID, importedModID, "IMPORTS"),
				FromID: moduleID,
				ToID:   importedModID,
				Kind:   "IMPORTS",
			},
			{
				ID:     graph.RelationshipID(moduleID, endpointID, "CONTAINS"),
				FromID: moduleID,
				ToID:   endpointID,
				Kind:   "CONTAINS",
			},
		},
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

// buildModuleManifestBundle is a test helper that calls BuildBundle for the given
// module entity and returns the bundle.
func buildModuleManifestBundle(t *testing.T, groupName, moduleID string) *docgen.LLMPromptBundle {
	t.Helper()
	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: moduleID,
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
// Tests
// ---------------------------------------------------------------------------

// TestBuildBundle_ModuleManifest_Populated verifies that BuildBundle sets
// graph_context.module_manifest (non-nil) when the seed entity is a Module.
func TestBuildBundle_ModuleManifest_Populated(t *testing.T) {
	groupName, moduleID := moduleManifestHarness(t)
	bundle := buildModuleManifestBundle(t, groupName, moduleID)

	if bundle.GraphContext.ModuleManifest == nil {
		t.Fatal("graph_context.module_manifest is nil — expected non-nil for Module seed entity")
	}
}

// TestBuildBundle_ModuleManifest_ClassesBucket verifies that CONTAINS neighbours
// with Controller/Service kinds land in the Classes bucket (not Models).
func TestBuildBundle_ModuleManifest_ClassesBucket(t *testing.T) {
	groupName, moduleID := moduleManifestHarness(t)
	bundle := buildModuleManifestBundle(t, groupName, moduleID)

	m := bundle.GraphContext.ModuleManifest
	if m == nil {
		t.Fatal("module_manifest is nil")
	}

	// Expect UserController and UserService in Classes (not Models).
	if len(m.Classes) != 2 {
		t.Errorf("Classes: expected 2, got %d: %+v", len(m.Classes), m.Classes)
	}

	classNames := map[string]bool{}
	for _, c := range m.Classes {
		classNames[c.Name] = true
	}
	for _, want := range []string{"UserController", "UserService"} {
		if !classNames[want] {
			t.Errorf("Classes: %q not found; got %v", want, m.Classes)
		}
	}
}

// TestBuildBundle_ModuleManifest_ModelsBucket verifies that CONTAINS neighbours
// with Model kind land in the separate Models bucket.
func TestBuildBundle_ModuleManifest_ModelsBucket(t *testing.T) {
	groupName, moduleID := moduleManifestHarness(t)
	bundle := buildModuleManifestBundle(t, groupName, moduleID)

	m := bundle.GraphContext.ModuleManifest
	if m == nil {
		t.Fatal("module_manifest is nil")
	}

	if len(m.Models) != 1 {
		t.Errorf("Models: expected 1, got %d: %+v", len(m.Models), m.Models)
	}
	if len(m.Models) > 0 && m.Models[0].Name != "UserDTO" {
		t.Errorf("Models[0].Name: got %q, want %q", m.Models[0].Name, "UserDTO")
	}
	if len(m.Models) > 0 && m.Models[0].KindSubtype != "model" {
		t.Errorf("Models[0].KindSubtype: got %q, want %q", m.Models[0].KindSubtype, "model")
	}
}

// TestBuildBundle_ModuleManifest_FunctionsBucket verifies that CONTAINS
// neighbours with Operation kind and hook subtype land in Functions.
func TestBuildBundle_ModuleManifest_FunctionsBucket(t *testing.T) {
	groupName, moduleID := moduleManifestHarness(t)
	bundle := buildModuleManifestBundle(t, groupName, moduleID)

	m := bundle.GraphContext.ModuleManifest
	if m == nil {
		t.Fatal("module_manifest is nil")
	}

	if len(m.Functions) != 1 {
		t.Errorf("Functions: expected 1, got %d: %+v", len(m.Functions), m.Functions)
	}
	if len(m.Functions) > 0 {
		fn := m.Functions[0]
		if fn.Name != "useUsers" {
			t.Errorf("Functions[0].Name: got %q, want %q", fn.Name, "useUsers")
		}
		if fn.Signature == "" {
			t.Error("Functions[0].Signature is empty")
		}
	}
}

// TestBuildBundle_ModuleManifest_ConstantsBucket verifies that CONTAINS
// neighbours with Schema kind + "constant" subtype land in Constants with
// value_literal populated.
func TestBuildBundle_ModuleManifest_ConstantsBucket(t *testing.T) {
	groupName, moduleID := moduleManifestHarness(t)
	bundle := buildModuleManifestBundle(t, groupName, moduleID)

	m := bundle.GraphContext.ModuleManifest
	if m == nil {
		t.Fatal("module_manifest is nil")
	}

	if len(m.Constants) != 1 {
		t.Errorf("Constants: expected 1, got %d: %+v", len(m.Constants), m.Constants)
	}
	if len(m.Constants) > 0 {
		c := m.Constants[0]
		if c.Name != "MAX_PAGE_SIZE" {
			t.Errorf("Constants[0].Name: got %q, want %q", c.Name, "MAX_PAGE_SIZE")
		}
		if c.ValueLiteral != "100" {
			t.Errorf("Constants[0].ValueLiteral: got %q, want %q", c.ValueLiteral, "100")
		}
		if c.StartLine != 2 {
			t.Errorf("Constants[0].StartLine: got %d, want 2", c.StartLine)
		}
	}
}

// TestBuildBundle_ModuleManifest_ImportsBucket verifies that IMPORTS-edge
// neighbours land in the Imports bucket.
func TestBuildBundle_ModuleManifest_ImportsBucket(t *testing.T) {
	groupName, moduleID := moduleManifestHarness(t)
	bundle := buildModuleManifestBundle(t, groupName, moduleID)

	m := bundle.GraphContext.ModuleManifest
	if m == nil {
		t.Fatal("module_manifest is nil")
	}

	if len(m.Imports) != 1 {
		t.Errorf("Imports: expected 1, got %d: %+v", len(m.Imports), m.Imports)
	}
	if len(m.Imports) > 0 {
		imp := m.Imports[0]
		if imp.Name == "" {
			t.Error("Imports[0].Name is empty")
		}
		if imp.TargetModule == "" {
			t.Error("Imports[0].TargetModule is empty")
		}
	}
}

// TestBuildBundle_ModuleManifest_EndpointsBucket verifies that CONTAINS
// neighbours with http_endpoint_definition kind land in Endpoints with
// method and path populated.
func TestBuildBundle_ModuleManifest_EndpointsBucket(t *testing.T) {
	groupName, moduleID := moduleManifestHarness(t)
	bundle := buildModuleManifestBundle(t, groupName, moduleID)

	m := bundle.GraphContext.ModuleManifest
	if m == nil {
		t.Fatal("module_manifest is nil")
	}

	if len(m.Endpoints) != 1 {
		t.Errorf("Endpoints: expected 1, got %d: %+v", len(m.Endpoints), m.Endpoints)
	}
	if len(m.Endpoints) > 0 {
		ep := m.Endpoints[0]
		if ep.Method != "GET" {
			t.Errorf("Endpoints[0].Method: got %q, want %q", ep.Method, "GET")
		}
		if ep.Path != "/users" {
			t.Errorf("Endpoints[0].Path: got %q, want %q", ep.Path, "/users")
		}
		if ep.StartLine != 15 {
			t.Errorf("Endpoints[0].StartLine: got %d, want 15", ep.StartLine)
		}
	}
}

// TestBuildBundle_ModuleManifest_Nil_ForNonModule verifies that non-Module
// entity kinds do NOT get a ModuleManifest.
func TestBuildBundle_ModuleManifest_Nil_ForNonModule(t *testing.T) {
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

	t.Setenv("GRAFEL_HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Setenv(daemon.EnvRoot, daemonRoot)

	groupName := "non-module-manifest-test"
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

	// A Function entity — should NOT get a ModuleManifest.
	fnID := graph.EntityID("repo", "SCOPE.Function", "processUsers", "src/util.go")

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
				Name:       "processUsers",
				Kind:       "SCOPE.Function",
				Subtype:    "function",
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
	if bundle.GraphContext.ModuleManifest != nil {
		t.Errorf("expected nil ModuleManifest for Function entity, got %+v", bundle.GraphContext.ModuleManifest)
	}
}

// TestBuildBundle_ModuleManifest_BucketCap verifies that buckets with more than
// ModuleManifestBucketCap entries are capped and the truncated count is correct.
func TestBuildBundle_ModuleManifest_BucketCap(t *testing.T) {
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

	t.Setenv("GRAFEL_HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Setenv(daemon.EnvRoot, daemonRoot)

	groupName := "module-manifest-cap-test"
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

	moduleID := graph.EntityID("repo", "SCOPE.Module", "big-module", "src/big")

	entities := []graph.Entity{
		{
			ID:         moduleID,
			Name:       "big-module",
			Kind:       "SCOPE.Module",
			SourceFile: "src/big",
			Language:   "java",
		},
	}
	rels := []graph.Relationship{}

	// Emit 150 Controller children — exceeds ModuleManifestBucketCap (100).
	for i := 0; i < 150; i++ {
		name := "Controller" + string(rune('A'+i%26)) + string(rune('0'+i/26))
		cID := graph.EntityID("repo", "SCOPE.Controller", name, "src/big/"+name+".java")
		entities = append(entities, graph.Entity{
			ID:         cID,
			Name:       name,
			Kind:       "SCOPE.Controller",
			Subtype:    "class",
			SourceFile: "src/big/" + name + ".java",
			StartLine:  i + 1,
			Language:   "java",
		})
		rels = append(rels, graph.Relationship{
			ID:     graph.RelationshipID(moduleID, cID, "CONTAINS"),
			FromID: moduleID,
			ToID:   cID,
			Kind:   "CONTAINS",
		})
	}

	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	doc := graph.Document{
		Version:        1,
		GeneratedAt:    time.Now().UTC(),
		Repo:           repoPath,
		IndexerVersion: "test",
		Entities:       entities,
		Relationships:  rels,
	}
	docJSON, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(stateDir, "graph.json"), docJSON, 0o644); err != nil {
		t.Fatalf("write graph: %v", err)
	}

	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: moduleID,
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
	m := bundle.GraphContext.ModuleManifest
	if m == nil {
		t.Fatal("module_manifest is nil")
	}

	if len(m.Classes) != docgen.ModuleManifestBucketCap {
		t.Errorf("Classes: expected exactly %d (cap), got %d", docgen.ModuleManifestBucketCap, len(m.Classes))
	}
	if m.ClassesTruncatedCount != 50 {
		t.Errorf("ClassesTruncatedCount: got %d, want 50", m.ClassesTruncatedCount)
	}
}
