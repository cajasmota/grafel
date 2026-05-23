package docgen_test

// llm_bundle_class_manifest_test.go — unit tests for the ClassManifest field
// populated by BuildBundle when the seed entity is class-like (#1861).
//
// Each test sets up an isolated in-memory group (via env overrides), writes a
// graph.json with class + child entities and CONTAINS/EXTENDS/IMPLEMENTS edges,
// and asserts that bundle.GraphContext.ClassManifest is correctly populated.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/docgen"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/registry"
)

// ---------------------------------------------------------------------------
// Test harness
// ---------------------------------------------------------------------------

// classManifestHarness creates an isolated test environment with a class entity
// that has method children (CONTAINS), a base class (EXTENDS), interfaces
// (IMPLEMENTS), and annotations in the Signature.
//
// Returns the groupName, class entity ID, and a cleanup func.
func classManifestHarness(t *testing.T) (groupName, classEntityID string) {
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

	groupName = "class-manifest-test-group"

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
	classID := graph.EntityID("repo", "SCOPE.Component", "AuthService", "src/auth.java")
	methodLoginID := graph.EntityID("repo", "SCOPE.Operation", "AuthService.login", "src/auth.java")
	methodLogoutID := graph.EntityID("repo", "SCOPE.Operation", "AuthService.logout", "src/auth.java")
	fieldRepoID := graph.EntityID("repo", "SCOPE.Schema", "AuthService.userRepository", "src/auth.java")
	baseClassID := graph.EntityID("repo", "SCOPE.Component", "BaseService", "src/base.java")
	ifaceID := graph.EntityID("repo", "SCOPE.Component", "Authenticator", "src/auth.java")

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
		Stats:          graph.Stats{Files: 1, Entities: 6, Relationships: 4},
		Entities: []graph.Entity{
			{
				ID:         classID,
				Name:       "AuthService",
				Kind:       "SCOPE.Component",
				Subtype:    "class",
				SourceFile: "src/auth.java",
				StartLine:  5,
				EndLine:    50,
				Language:   "java",
				// Annotations in Signature — as emitted by buildClassSignature.
				Signature: "@Service @Transactional class AuthService",
			},
			{
				ID:         methodLoginID,
				Name:       "AuthService.login",
				Kind:       "SCOPE.Operation",
				Subtype:    "method",
				SourceFile: "src/auth.java",
				StartLine:  10,
				EndLine:    20,
				Language:   "java",
				Signature:  "public String login(String username, String password)",
			},
			{
				ID:         methodLogoutID,
				Name:       "AuthService.logout",
				Kind:       "SCOPE.Operation",
				Subtype:    "method",
				SourceFile: "src/auth.java",
				StartLine:  22,
				EndLine:    28,
				Language:   "java",
				Signature:  "public void logout(String token)",
			},
			{
				ID:         fieldRepoID,
				Name:       "AuthService.userRepository",
				Kind:       "SCOPE.Schema",
				Subtype:    "field",
				SourceFile: "src/auth.java",
				StartLine:  7,
				Language:   "java",
				Signature:  "UserRepository userRepository",
			},
			{
				ID:         baseClassID,
				Name:       "BaseService",
				Kind:       "SCOPE.Component",
				Subtype:    "class",
				SourceFile: "src/base.java",
				StartLine:  1,
				Language:   "java",
			},
			{
				ID:         ifaceID,
				Name:       "Authenticator",
				Kind:       "SCOPE.Component",
				Subtype:    "interface",
				SourceFile: "src/auth.java",
				StartLine:  1,
				Language:   "java",
			},
		},
		Relationships: []graph.Relationship{
			{
				ID:     graph.RelationshipID(classID, methodLoginID, "CONTAINS"),
				FromID: classID,
				ToID:   methodLoginID,
				Kind:   "CONTAINS",
			},
			{
				ID:     graph.RelationshipID(classID, methodLogoutID, "CONTAINS"),
				FromID: classID,
				ToID:   methodLogoutID,
				Kind:   "CONTAINS",
			},
			{
				ID:     graph.RelationshipID(classID, fieldRepoID, "CONTAINS"),
				FromID: classID,
				ToID:   fieldRepoID,
				Kind:   "CONTAINS",
			},
			{
				ID:     graph.RelationshipID(classID, baseClassID, "EXTENDS"),
				FromID: classID,
				ToID:   baseClassID,
				Kind:   "EXTENDS",
			},
			{
				ID:     graph.RelationshipID(classID, ifaceID, "IMPLEMENTS"),
				FromID: classID,
				ToID:   ifaceID,
				Kind:   "IMPLEMENTS",
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

	return groupName, classID
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestBuildBundle_ClassManifest_Populated verifies that BuildBundle sets
// graph_context.class_manifest (non-nil) when the seed entity is class-like.
func TestBuildBundle_ClassManifest_Populated(t *testing.T) {
	groupName, classID := classManifestHarness(t)

	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: classID,
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
	if bundle.GraphContext.ClassManifest == nil {
		t.Fatal("graph_context.class_manifest is nil — expected non-nil for class-like seed entity")
	}
}

// TestBuildBundle_ClassManifest_Methods verifies that methods discovered via
// CONTAINS edges appear in ClassManifest.Methods with correct fields.
func TestBuildBundle_ClassManifest_Methods(t *testing.T) {
	groupName, classID := classManifestHarness(t)

	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: classID,
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
	m := bundle.GraphContext.ClassManifest
	if m == nil {
		t.Fatal("class_manifest is nil")
	}

	// Expect exactly 2 methods: login and logout.
	if len(m.Methods) != 2 {
		t.Errorf("expected 2 methods, got %d: %+v", len(m.Methods), m.Methods)
	}

	// Find the "login" method.
	var loginEntry *docgen.ClassMethodEntry
	for i := range m.Methods {
		if m.Methods[i].Name == "login" {
			loginEntry = &m.Methods[i]
			break
		}
	}
	if loginEntry == nil {
		t.Fatal("method 'login' not found in ClassManifest.Methods")
	}
	if loginEntry.Signature == "" {
		t.Error("login entry has empty Signature")
	}
	if loginEntry.Visibility != "public" {
		t.Errorf("login visibility: got %q, want %q", loginEntry.Visibility, "public")
	}
	if loginEntry.StartLine != 10 {
		t.Errorf("login StartLine: got %d, want 10", loginEntry.StartLine)
	}
	if loginEntry.EndLine != 20 {
		t.Errorf("login EndLine: got %d, want 20", loginEntry.EndLine)
	}
	if loginEntry.Subtype != "method" {
		t.Errorf("login Subtype: got %q, want %q", loginEntry.Subtype, "method")
	}
}

// TestBuildBundle_ClassManifest_Fields verifies that fields discovered via
// CONTAINS edges appear in ClassManifest.Fields.
func TestBuildBundle_ClassManifest_Fields(t *testing.T) {
	groupName, classID := classManifestHarness(t)

	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: classID,
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
	m := bundle.GraphContext.ClassManifest
	if m == nil {
		t.Fatal("class_manifest is nil")
	}

	if len(m.Fields) != 1 {
		t.Errorf("expected 1 field, got %d: %+v", len(m.Fields), m.Fields)
	}
	if len(m.Fields) > 0 {
		f := m.Fields[0]
		if f.Name != "userRepository" {
			t.Errorf("field name: got %q, want %q", f.Name, "userRepository")
		}
		if f.TypeHint != "UserRepository" {
			t.Errorf("field TypeHint: got %q, want %q", f.TypeHint, "UserRepository")
		}
		if f.StartLine != 7 {
			t.Errorf("field StartLine: got %d, want 7", f.StartLine)
		}
	}
}

// TestBuildBundle_ClassManifest_BasesAndInterfaces verifies that EXTENDS and
// IMPLEMENTS neighbours are captured in Bases and Interfaces.
func TestBuildBundle_ClassManifest_BasesAndInterfaces(t *testing.T) {
	groupName, classID := classManifestHarness(t)

	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: classID,
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
	m := bundle.GraphContext.ClassManifest
	if m == nil {
		t.Fatal("class_manifest is nil")
	}

	if len(m.Bases) != 1 || m.Bases[0] != "BaseService" {
		t.Errorf("Bases: got %v, want [BaseService]", m.Bases)
	}
	if len(m.Interfaces) != 1 || m.Interfaces[0] != "Authenticator" {
		t.Errorf("Interfaces: got %v, want [Authenticator]", m.Interfaces)
	}
}

// TestBuildBundle_ClassManifest_Decorators verifies that @Annotation tokens in
// the class Signature are captured in ClassManifest.Decorators.
func TestBuildBundle_ClassManifest_Decorators(t *testing.T) {
	groupName, classID := classManifestHarness(t)

	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: classID,
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
	m := bundle.GraphContext.ClassManifest
	if m == nil {
		t.Fatal("class_manifest is nil")
	}

	// The class Signature is "@Service @Transactional class AuthService".
	// Expect @Service and @Transactional to be captured.
	decoratorSet := map[string]bool{}
	for _, d := range m.Decorators {
		decoratorSet[d] = true
	}
	for _, want := range []string{"@Service", "@Transactional"} {
		if !decoratorSet[want] {
			t.Errorf("decorator %q not found in ClassManifest.Decorators: %v", want, m.Decorators)
		}
	}
}

// TestBuildBundle_ClassManifest_Nil_ForNonClass verifies that non-class-like
// entity kinds (Function, Operation) do NOT get a ClassManifest.
func TestBuildBundle_ClassManifest_Nil_ForNonClass(t *testing.T) {
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

	groupName := "non-class-manifest-test"
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
	if bundle.GraphContext.ClassManifest != nil {
		t.Errorf("expected nil ClassManifest for Function entity, got %+v", bundle.GraphContext.ClassManifest)
	}
}

// TestBuildBundle_ClassManifest_Truncation verifies that classes with > 100
// methods produce a MethodsTruncatedCount field and exactly 100 entries.
func TestBuildBundle_ClassManifest_Truncation(t *testing.T) {
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

	groupName := "truncation-test"
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

	classID := graph.EntityID("repo", "SCOPE.Component", "BigClass", "src/big.java")

	entities := []graph.Entity{
		{
			ID:         classID,
			Name:       "BigClass",
			Kind:       "SCOPE.Component",
			Subtype:    "class",
			SourceFile: "src/big.java",
			StartLine:  1,
			Language:   "java",
			Signature:  "class BigClass",
		},
	}
	rels := []graph.Relationship{}

	// Emit 150 methods — exceeds ClassManifestMaxMethods (100).
	for i := 0; i < 150; i++ {
		import_f := func(i int) string {
			return "BigClass.method" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		}
		mName := import_f(i)
		mID := graph.EntityID("repo", "SCOPE.Operation", mName, "src/big.java")
		entities = append(entities, graph.Entity{
			ID:         mID,
			Name:       mName,
			Kind:       "SCOPE.Operation",
			Subtype:    "method",
			SourceFile: "src/big.java",
			StartLine:  10 + i,
			Language:   "java",
			Signature:  "public void " + mName + "()",
		})
		rels = append(rels, graph.Relationship{
			ID:     graph.RelationshipID(classID, mID, "CONTAINS"),
			FromID: classID,
			ToID:   mID,
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
			SeedEntityID: classID,
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
	m := bundle.GraphContext.ClassManifest
	if m == nil {
		t.Fatal("class_manifest is nil")
	}

	if len(m.Methods) != docgen.ClassManifestMaxMethods {
		t.Errorf("expected exactly %d methods (cap), got %d", docgen.ClassManifestMaxMethods, len(m.Methods))
	}
	if m.MethodsTruncatedCount != 50 {
		t.Errorf("MethodsTruncatedCount: got %d, want 50", m.MethodsTruncatedCount)
	}
}

// TestBuildBundle_ClassManifest_ShortNameStripping verifies that method names
// like "AuthService.login" are stored as "login" in the manifest (short name).
func TestBuildBundle_ClassManifest_ShortNameStripping(t *testing.T) {
	groupName, classID := classManifestHarness(t)

	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: classID,
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
	m := bundle.GraphContext.ClassManifest
	if m == nil {
		t.Fatal("class_manifest is nil")
	}

	for _, me := range m.Methods {
		if me.Name == "AuthService.login" || me.Name == "AuthService.logout" {
			t.Errorf("method name not stripped: got %q, expected short name without class prefix", me.Name)
		}
	}
	for _, fe := range m.Fields {
		if fe.Name == "AuthService.userRepository" {
			t.Errorf("field name not stripped: got %q, expected short name", fe.Name)
		}
	}
}
