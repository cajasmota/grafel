package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// HOME is redundant with GRAFEL_HOME today (registry.HomeDir() checks
	// GRAFEL_HOME first and never falls through to os.UserHomeDir() when it's
	// set), but per the hard isolation gate (round-2 review of #6184/#6185/
	// #6186), isolate all three so this helper stays safe if that check order
	// ever changes.
	t.Setenv("HOME", dir)
	t.Setenv("GRAFEL_HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	return dir
}

func TestLoadEmpty(t *testing.T) {
	withHome(t)
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != 1 || len(r.Groups) != 0 {
		t.Fatalf("expected empty registry, got %+v", r)
	}
}

func TestAddGroupValidatesConfigExists(t *testing.T) {
	home := withHome(t)
	cfgPath := filepath.Join(home, "xdg", "grafel", "missing.fleet.json")

	// Try to add a group with a non-existent config file.
	err := AddGroup("missing", cfgPath)
	if err == nil {
		t.Fatal("expected error for missing config file, got nil")
	}
	errMsg := err.Error()
	if !contains(errMsg, "does not exist") && !contains(errMsg, "cannot access") {
		t.Fatalf("expected error about config file not being accessible, got: %v", err)
	}

	// Verify the group was not added.
	groups, _ := Groups()
	if len(groups) != 0 {
		t.Fatalf("expected no groups, got %d", len(groups))
	}
}

func TestAddRemoveGroup(t *testing.T) {
	home := withHome(t)
	cfgPath, err := ConfigPathFor("alpha")
	if err != nil {
		t.Fatal(err)
	}
	// Create the config file first.
	os.MkdirAll(filepath.Dir(cfgPath), 0o755)
	if err := os.WriteFile(cfgPath, []byte(`{"name":"alpha"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := AddGroup("alpha", cfgPath); err != nil {
		t.Fatal(err)
	}

	betaCfgPath := filepath.Join(home, "xdg", "grafel", "beta.fleet.json")
	os.MkdirAll(filepath.Dir(betaCfgPath), 0o755)
	if err := os.WriteFile(betaCfgPath, []byte(`{"name":"beta"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AddGroup("beta", betaCfgPath); err != nil {
		t.Fatal(err)
	}
	groups, err := Groups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 || groups[0].Name != "alpha" || groups[1].Name != "beta" {
		t.Fatalf("groups: %+v", groups)
	}
	// Idempotent re-add.
	if err := AddGroup("alpha", cfgPath); err != nil {
		t.Fatal(err)
	}
	groups, _ = Groups()
	if len(groups) != 2 {
		t.Fatalf("idempotent add broken: %+v", groups)
	}
	if err := RemoveGroup("alpha"); err != nil {
		t.Fatal(err)
	}
	groups, _ = Groups()
	if len(groups) != 1 || groups[0].Name != "beta" {
		t.Fatalf("after remove: %+v", groups)
	}
	// Idempotent remove of unknown group.
	if err := RemoveGroup("ghost"); err != nil {
		t.Fatal(err)
	}
}

func TestSaveLoadGroupConfig(t *testing.T) {
	dir := withHome(t)
	cfg := &GroupConfig{
		Name:  "demo",
		Repos: []Repo{{Slug: "core", Path: "/tmp/core", Stack: StackList{"go"}}},
	}
	cfg.Features.Watchers = true
	cfg.Features.GitHooks = true
	p := filepath.Join(dir, "demo.fleet.json")
	if err := SaveGroupConfig(p, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := LoadGroupConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "demo" || len(got.Repos) != 1 || !got.Features.Watchers {
		t.Fatalf("roundtrip: %+v", got)
	}
}

func TestLoadManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".grafel"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"group":"demo","repos":[{"slug":"core","clone_url":"git@x:y.git"}]}`
	if err := os.WriteFile(filepath.Join(dir, ".grafel", "group.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Group != "demo" || len(m.Repos) != 1 || m.Repos[0].Slug != "core" {
		t.Fatalf("manifest: %+v", m)
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// ---------------------------------------------------------------------------
// StackList custom JSON unmarshal / marshal tests (fix for #1771)
// ---------------------------------------------------------------------------

func TestStackList_UnmarshalJSON_String(t *testing.T) {
	// Old shape: bare string.
	input := `{"slug":"repo","path":"/tmp","stack":"go"}`
	var r Repo
	if err := json.Unmarshal([]byte(input), &r); err != nil {
		t.Fatalf("unmarshal bare string: %v", err)
	}
	if len(r.Stack) != 1 || r.Stack[0] != "go" {
		t.Fatalf("want Stack=[go], got %v", r.Stack)
	}
	if r.Stack.Primary() != "go" {
		t.Fatalf("Primary() want go, got %q", r.Stack.Primary())
	}
}

func TestStackList_UnmarshalJSON_Array(t *testing.T) {
	// New shape: array of strings (e.g. what grafel.fleet.json now has).
	input := `{"slug":"repo","path":"/tmp","stack":["go","typescript"]}`
	var r Repo
	if err := json.Unmarshal([]byte(input), &r); err != nil {
		t.Fatalf("unmarshal array: %v", err)
	}
	if len(r.Stack) != 2 || r.Stack[0] != "go" || r.Stack[1] != "typescript" {
		t.Fatalf("want Stack=[go,typescript], got %v", r.Stack)
	}
	if r.Stack.Primary() != "go" {
		t.Fatalf("Primary() want go, got %q", r.Stack.Primary())
	}
	if r.Stack.String() != "go/typescript" {
		t.Fatalf("String() want go/typescript, got %q", r.Stack.String())
	}
}

func TestStackList_UnmarshalJSON_Null(t *testing.T) {
	input := `{"slug":"repo","path":"/tmp","stack":null}`
	var r Repo
	if err := json.Unmarshal([]byte(input), &r); err != nil {
		t.Fatalf("unmarshal null: %v", err)
	}
	if !r.Stack.IsEmpty() {
		t.Fatalf("want empty stack, got %v", r.Stack)
	}
	if r.Stack.Primary() != "" {
		t.Fatalf("Primary() on empty want \"\", got %q", r.Stack.Primary())
	}
}

func TestStackList_UnmarshalJSON_Absent(t *testing.T) {
	// Field absent altogether (omitempty).
	input := `{"slug":"repo","path":"/tmp"}`
	var r Repo
	if err := json.Unmarshal([]byte(input), &r); err != nil {
		t.Fatalf("unmarshal absent: %v", err)
	}
	if !r.Stack.IsEmpty() {
		t.Fatalf("want empty stack when field absent, got %v", r.Stack)
	}
}

func TestStackList_UnmarshalJSON_Malformed(t *testing.T) {
	// Malformed input: a number, not a string or array.
	input := `{"slug":"repo","path":"/tmp","stack":42}`
	var r Repo
	err := json.Unmarshal([]byte(input), &r)
	if err == nil {
		t.Fatal("expected error for numeric stack value, got nil")
	}
	if !strings.Contains(err.Error(), "stack") {
		t.Fatalf("error should mention 'stack', got: %v", err)
	}
}

func TestStackList_MarshalJSON_AlwaysArray(t *testing.T) {
	// After parsing a bare string, re-serializing should produce an array.
	r := Repo{Slug: "r", Path: "/p", Stack: StackList{"go"}}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(b)
	if !strings.Contains(got, `"stack":["go"]`) {
		t.Fatalf("want stack as array in JSON, got: %s", got)
	}
}

func TestStackList_RoundTrip_OldConfigShape(t *testing.T) {
	// Simulate loading the grafel.fleet.json that caused the original crash:
	//   "stack": ["go", "typescript"]
	// Verify it round-trips through LoadGroupConfig + SaveGroupConfig.
	dir := withHome(t)
	oldShape := `{
  "name": "grafel",
  "repos": [
    {"slug": "grafel", "path": "/tmp/grafel", "stack": ["go", "typescript"]}
  ],
  "features": {}
}`
	p := filepath.Join(dir, "grafel.fleet.json")
	if err := os.WriteFile(p, []byte(oldShape), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadGroupConfig(p)
	if err != nil {
		t.Fatalf("LoadGroupConfig with array stack: %v", err)
	}
	if len(cfg.Repos) != 1 {
		t.Fatalf("want 1 repo, got %d", len(cfg.Repos))
	}
	r := cfg.Repos[0]
	if len(r.Stack) != 2 || r.Stack[0] != "go" || r.Stack[1] != "typescript" {
		t.Fatalf("stack parse: want [go typescript], got %v", r.Stack)
	}
	// Save and reload — should still be correct.
	p2 := filepath.Join(dir, "grafel2.fleet.json")
	if err := SaveGroupConfig(p2, cfg); err != nil {
		t.Fatalf("SaveGroupConfig: %v", err)
	}
	cfg2, err := LoadGroupConfig(p2)
	if err != nil {
		t.Fatalf("LoadGroupConfig after save: %v", err)
	}
	if cfg2.Repos[0].Stack.String() != "go/typescript" {
		t.Fatalf("after roundtrip: want go/typescript, got %q", cfg2.Repos[0].Stack.String())
	}
}

func TestValidateFleetConfigs_AllValid(t *testing.T) {
	home := withHome(t)
	// Create two valid fleet configs.
	for _, name := range []string{"alpha", "beta"} {
		p, _ := ConfigPathFor(name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		body := `{"name":"` + name + `","repos":[{"slug":"r","path":"/tmp","stack":["go"]}],"features":{}}`
		os.WriteFile(p, []byte(body), 0o644)
		os.WriteFile(filepath.Join(home, name+".touch"), nil, 0o644) // just a marker
		AddGroup(name, p)
	}
	errs := ValidateFleetConfigs()
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
}

func TestValidateFleetConfigs_OneInvalid(t *testing.T) {
	home := withHome(t)
	// Alpha has array stack (valid new shape).
	alphaPath := filepath.Join(home, "alpha.fleet.json")
	os.WriteFile(alphaPath, []byte(`{"name":"alpha","repos":[{"slug":"r","path":"/tmp","stack":["go"]}],"features":{}}`), 0o644)
	// Beta has a malformed JSON body.
	betaPath := filepath.Join(home, "beta.fleet.json")
	os.WriteFile(betaPath, []byte(`{broken json`), 0o644)

	os.MkdirAll(filepath.Dir(alphaPath), 0o755)
	AddGroup("alpha", alphaPath)
	AddGroup("beta", betaPath)

	errs := ValidateFleetConfigs()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error (beta), got %d: %v", len(errs), errs)
	}
	if errs[0].GroupName != "beta" {
		t.Fatalf("expected error for beta, got %q", errs[0].GroupName)
	}
}
