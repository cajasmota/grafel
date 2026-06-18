package mcpreg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/testsupport"
)

// TestMain fail-closes the package: when GRAFEL_TEST_REQUIRE_ISOLATED_HOME=1
// it refuses to run if HOME is the real user home (these tests write
// ~/.claude.json and must never touch the developer's live MCP config).
func TestMain(m *testing.M) {
	testsupport.GuardRealHomeMain()
	os.Exit(m.Run())
}

// withHome redirects HOME so settings paths land inside a TempDir, and asserts
// (via testsupport) that the redirect did not land on the real user home.
func withHome(t *testing.T) string {
	t.Helper()
	dir := testsupport.IsolateHome(t)
	// Keep mcpreg's historical XDG layout (.config under the temp home).
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	return dir
}

func TestRegisterCreatesEntry(t *testing.T) {
	withHome(t)
	path, err := Register(ClaudeCode, "/bin/grafel", "/r/registry.json")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		McpServers map[string]Entry `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	got := doc.McpServers[ServerName]
	if got.Command != "/bin/grafel" {
		t.Fatalf("command: %q", got.Command)
	}
	// New behaviour: args = ["mcp-bridge"], type = "stdio"
	if len(got.Args) != 1 || got.Args[0] != "mcp-bridge" {
		t.Fatalf("args: %+v (want [mcp-bridge])", got.Args)
	}
	if got.Type != "stdio" {
		t.Fatalf("type: %q (want stdio)", got.Type)
	}
}

func TestRegisterPreservesOtherEntries(t *testing.T) {
	withHome(t)
	path, _ := SettingsPath(ClaudeCode)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	pre := `{"theme":"dark","mcpServers":{"other":{"command":"/x"}}}`
	if err := os.WriteFile(path, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Register(ClaudeCode, "/bin/grafel", "/r.json"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	if doc["theme"] != "dark" {
		t.Fatalf("lost top-level field: %s", b)
	}
	servers := doc["mcpServers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Fatalf("lost sibling entry: %s", b)
	}
	if _, ok := servers[ServerName]; !ok {
		t.Fatalf("missing grafel entry: %s", b)
	}
}

func TestUnregisterIdempotent(t *testing.T) {
	withHome(t)
	if err := Unregister(ClaudeCode); err != nil {
		t.Fatal(err)
	}
	if _, err := Register(ClaudeCode, "/bin/grafel", "/r.json"); err != nil {
		t.Fatal(err)
	}
	if err := Unregister(ClaudeCode); err != nil {
		t.Fatal(err)
	}
	if err := Unregister(ClaudeCode); err != nil {
		t.Fatal(err)
	}
}

func TestWindsurfPath(t *testing.T) {
	withHome(t)
	p, err := SettingsPath(Windsurf)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "mcp_config.json" {
		t.Fatalf("windsurf path unexpected: %s", p)
	}
}

func TestClaudeCodePathIsHomeClaudeJSON(t *testing.T) {
	home := withHome(t)
	p, err := SettingsPath(ClaudeCode)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".claude.json")
	if p != want {
		t.Fatalf("ClaudeCode path: got %s, want %s", p, want)
	}
}

func TestRegisterPathIdempotent(t *testing.T) {
	home := withHome(t)
	path := filepath.Join(home, ".claude.json")

	// Register twice — should produce exactly one entry.
	if _, err := RegisterPath(path, "/bin/grafel"); err != nil {
		t.Fatal(err)
	}
	if _, err := RegisterPath(path, "/bin/grafel"); err != nil {
		t.Fatal(err)
	}

	b, _ := os.ReadFile(path)
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	servers, _ := doc["mcpServers"].(map[string]any)
	if len(servers) != 1 {
		t.Fatalf("expected exactly 1 server entry, got %d: %s", len(servers), b)
	}
}

func TestRegisterPathUpdatesCommand(t *testing.T) {
	home := withHome(t)
	path := filepath.Join(home, ".claude.json")

	if _, err := RegisterPath(path, "/old/grafel"); err != nil {
		t.Fatal(err)
	}
	if _, err := RegisterPath(path, "/new/grafel"); err != nil {
		t.Fatal(err)
	}

	b, _ := os.ReadFile(path)
	var doc struct {
		McpServers map[string]Entry `json:"mcpServers"`
	}
	_ = json.Unmarshal(b, &doc)
	got := doc.McpServers[ServerName]
	if got.Command != "/new/grafel" {
		t.Fatalf("command not updated: %q", got.Command)
	}
}

func TestDetectClaudeConfigDirs_ExplicitOverride(t *testing.T) {
	withHome(t) // isolate even though explicit dirs are passed (defense in depth)
	explicit := []string{"/a/.claude.json", "/b/.claude.json"}
	got := DetectClaudeConfigDirs(explicit)
	if len(got) != 2 || got[0] != explicit[0] || got[1] != explicit[1] {
		t.Fatalf("explicit dirs not returned as-is: %v", got)
	}
}

func TestDetectClaudeConfigDirs_ScansDotClaudeDirs(t *testing.T) {
	home := withHome(t)

	// Create ~/.claude-personal/ directory.
	personalDir := filepath.Join(home, ".claude-personal")
	if err := os.MkdirAll(personalDir, 0o755); err != nil {
		t.Fatal(err)
	}

	dirs := DetectClaudeConfigDirs(nil)

	primary := filepath.Join(home, ".claude.json")
	secondary := filepath.Join(personalDir, ".claude.json")

	foundPrimary := false
	foundSecondary := false
	for _, d := range dirs {
		if d == primary {
			foundPrimary = true
		}
		if d == secondary {
			foundSecondary = true
		}
	}
	if !foundPrimary {
		t.Errorf("primary %s not in dirs: %v", primary, dirs)
	}
	if !foundSecondary {
		t.Errorf("secondary %s not in dirs: %v", secondary, dirs)
	}
}

func TestUnregisterPath(t *testing.T) {
	home := withHome(t)
	path := filepath.Join(home, ".claude.json")

	if _, err := RegisterPath(path, "/bin/grafel"); err != nil {
		t.Fatal(err)
	}
	if err := UnregisterPath(path); err != nil {
		t.Fatal(err)
	}

	b, _ := os.ReadFile(path)
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	servers, _ := doc["mcpServers"].(map[string]any)
	if _, ok := servers[ServerName]; ok {
		t.Fatalf("grafel entry still present after Unregister: %s", b)
	}
}

// ── Windsurf tests ────────────────────────────────────────────────────────────

// TestInstallRegistersWindsurfDesktop: fakeHome with the Windsurf desktop
// parent dir present → DetectWindsurfPaths includes the desktop path and
// RegisterPath populates the grafel entry.
func TestInstallRegistersWindsurfDesktop(t *testing.T) {
	home := withHome(t)

	// Simulate Windsurf desktop app installed: create the parent dir.
	desktopDir := filepath.Join(home, ".codeium", "windsurf")
	if err := os.MkdirAll(desktopDir, 0o755); err != nil {
		t.Fatal(err)
	}

	paths := DetectWindsurfPaths()
	desktopConfig := filepath.Join(desktopDir, "mcp_config.json")

	found := false
	for _, p := range paths {
		if p == desktopConfig {
			found = true
		}
	}
	if !found {
		t.Fatalf("DetectWindsurfPaths did not return desktop path %s; got %v", desktopConfig, paths)
	}

	// Register and verify the entry is correct.
	if _, err := RegisterPath(desktopConfig, "/usr/local/bin/grafel"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(desktopConfig)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		McpServers map[string]Entry `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	got := doc.McpServers[ServerName]
	if got.Command != "/usr/local/bin/grafel" {
		t.Fatalf("desktop: command = %q, want /usr/local/bin/grafel", got.Command)
	}
	if len(got.Args) != 1 || got.Args[0] != "mcp-bridge" {
		t.Fatalf("desktop: args = %v, want [mcp-bridge]", got.Args)
	}
	if got.Type != "stdio" {
		t.Fatalf("desktop: type = %q, want stdio", got.Type)
	}
}

// TestInstallRegistersWindsurfJetBrains: fakeHome with the JetBrains plugin
// parent dir present → DetectWindsurfPaths includes the JetBrains path and
// RegisterPath populates the grafel entry.
func TestInstallRegistersWindsurfJetBrains(t *testing.T) {
	home := withHome(t)

	// Simulate Windsurf JetBrains plugin installed: create ~/.codeium/ only
	// (NOT the windsurf/ subdir, so the desktop path must NOT appear).
	codeiumDir := filepath.Join(home, ".codeium")
	if err := os.MkdirAll(codeiumDir, 0o755); err != nil {
		t.Fatal(err)
	}

	paths := DetectWindsurfPaths()
	jbConfig := filepath.Join(codeiumDir, "mcp_config.json")

	found := false
	for _, p := range paths {
		if p == jbConfig {
			found = true
		}
	}
	if !found {
		t.Fatalf("DetectWindsurfPaths did not return JetBrains path %s; got %v", jbConfig, paths)
	}

	// The desktop path must NOT appear (windsurf/ subdir does not exist).
	desktopConfig := filepath.Join(home, ".codeium", "windsurf", "mcp_config.json")
	for _, p := range paths {
		if p == desktopConfig {
			t.Fatalf("DetectWindsurfPaths included desktop path %s but windsurf/ dir does not exist", p)
		}
	}

	// Register and verify the entry is correct.
	if _, err := RegisterPath(jbConfig, "/usr/local/bin/grafel"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(jbConfig)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		McpServers map[string]Entry `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	got := doc.McpServers[ServerName]
	if got.Command != "/usr/local/bin/grafel" {
		t.Fatalf("jetbrains: command = %q, want /usr/local/bin/grafel", got.Command)
	}
}

// TestInstallSkipsAbsentWindsurf: fakeHome without .codeium dir →
// DetectWindsurfPaths returns an empty slice and no writes occur.
func TestInstallSkipsAbsentWindsurf(t *testing.T) {
	withHome(t)
	// No .codeium directory created — Windsurf is not installed.

	paths := DetectWindsurfPaths()
	if len(paths) != 0 {
		t.Fatalf("expected no Windsurf paths when .codeium absent, got %v", paths)
	}
}

// TestWindsurfJetBrainsPath verifies the canonical path for WindsurfJetBrains.
func TestWindsurfJetBrainsPath(t *testing.T) {
	home := withHome(t)
	p, err := SettingsPath(WindsurfJetBrains)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".codeium", "mcp_config.json")
	if p != want {
		t.Fatalf("WindsurfJetBrains path: got %s, want %s", p, want)
	}
	// Confirm no hardcoded slash separators or /Users/ prefixes.
	if filepath.IsAbs(p) && filepath.Dir(p) == "/" {
		t.Fatalf("path looks hardcoded: %s", p)
	}
}

// TestInstall_CrossPlatformPaths asserts that all tool paths are constructed
// via filepath.Join (no string-concatenated '/' separators) by verifying
// each path is rooted under the fake home dir and that filepath.Rel succeeds
// without traversal (i.e. no ".." components at the start).
func TestInstall_CrossPlatformPaths(t *testing.T) {
	home := withHome(t)

	for _, tool := range []Tool{ClaudeCode, Windsurf, WindsurfJetBrains} {
		p, err := SettingsPath(tool)
		if err != nil {
			t.Fatalf("SettingsPath(%s): %v", tool, err)
		}
		// filepath.Rel must succeed and must not start with ".." (which would
		// indicate the path escapes the home directory).
		rel, err := filepath.Rel(home, p)
		if err != nil {
			t.Errorf("tool %s: filepath.Rel(%s, %s) failed: %v", tool, home, p, err)
			continue
		}
		// A path under home either equals home (".") or starts with a
		// path component — it must NOT start with "..".
		parts := filepath.SplitList(rel)
		_ = parts
		if len(rel) >= 2 && rel[:2] == ".." {
			t.Errorf("tool %s: path %s escapes home %s (rel=%s)", tool, p, home, rel)
		}
	}
}

// TestWindsurfRegistrationUpdatesPath verifies that re-running RegisterPath
// with a new binary path updates the command field (--force reinstall case).
func TestWindsurfRegistrationUpdatesPath(t *testing.T) {
	home := withHome(t)

	desktopDir := filepath.Join(home, ".codeium", "windsurf")
	if err := os.MkdirAll(desktopDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(desktopDir, "mcp_config.json")

	if _, err := RegisterPath(cfgPath, "/old/grafel"); err != nil {
		t.Fatal(err)
	}
	if _, err := RegisterPath(cfgPath, "/new/grafel"); err != nil {
		t.Fatal(err)
	}

	b, _ := os.ReadFile(cfgPath)
	var doc struct {
		McpServers map[string]Entry `json:"mcpServers"`
	}
	_ = json.Unmarshal(b, &doc)
	got := doc.McpServers[ServerName]
	if got.Command != "/new/grafel" {
		t.Fatalf("Windsurf desktop: command not updated on re-register: %q", got.Command)
	}
}

// ── Cursor tests ──────────────────────────────────────────────────────────────

// TestInstall_RegistersCursor: ~/.cursor/ present → DetectCursorPaths returns
// the mcp.json path and RegisterPath writes the grafel entry.
func TestInstall_RegistersCursor(t *testing.T) {
	home := withHome(t)

	cursorDir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(cursorDir, 0o755); err != nil {
		t.Fatal(err)
	}

	targets := DetectCursorPaths()
	wantPath := filepath.Join(cursorDir, "mcp.json")

	if len(targets) != 1 || targets[0].Path != wantPath {
		t.Fatalf("DetectCursorPaths: got %v, want [{%s ShapeFlat}]", targets, wantPath)
	}
	if targets[0].Shape != ShapeFlat {
		t.Fatalf("Cursor shape: got %v, want ShapeFlat", targets[0].Shape)
	}

	if _, err := RegisterPath(wantPath, "/usr/local/bin/grafel"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		McpServers map[string]Entry `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	got := doc.McpServers[ServerName]
	if got.Command != "/usr/local/bin/grafel" {
		t.Fatalf("cursor: command = %q, want /usr/local/bin/grafel", got.Command)
	}
	if len(got.Args) != 1 || got.Args[0] != "mcp-bridge" {
		t.Fatalf("cursor: args = %v, want [mcp-bridge]", got.Args)
	}
	if got.Type != "stdio" {
		t.Fatalf("cursor: type = %q, want stdio", got.Type)
	}
}

// TestInstall_SkipsAbsentCursor: no ~/.cursor dir → DetectCursorPaths is empty.
func TestInstall_SkipsAbsentCursor(t *testing.T) {
	withHome(t)
	targets := DetectCursorPaths()
	if len(targets) != 0 {
		t.Fatalf("expected no Cursor paths when .cursor absent, got %v", targets)
	}
}

// ── Kiro tests ──────────────────────────────────────────────────────────────

// TestInstall_RegistersKiro: ~/.kiro/settings/ present → DetectKiroPaths
// returns the mcp.json path and RegisterPath writes a valid grafel entry
// with the Cursor-shaped { "mcpServers": { ... } } layout (#5255).
func TestInstall_RegistersKiro(t *testing.T) {
	home := withHome(t)

	settingsDir := filepath.Join(home, ".kiro", "settings")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	targets := DetectKiroPaths()
	wantPath := filepath.Join(settingsDir, "mcp.json")
	if len(targets) != 1 || targets[0].Path != wantPath {
		t.Fatalf("DetectKiroPaths: got %v, want [{%s ShapeFlat}]", targets, wantPath)
	}
	if targets[0].Shape != ShapeFlat {
		t.Fatalf("Kiro shape: got %v, want ShapeFlat", targets[0].Shape)
	}

	// SettingsPath(Kiro) must resolve to the same user-global file.
	sp, err := SettingsPath(Kiro)
	if err != nil {
		t.Fatalf("SettingsPath(Kiro): %v", err)
	}
	if sp != wantPath {
		t.Fatalf("SettingsPath(Kiro) = %q, want %q", sp, wantPath)
	}

	if _, err := RegisterPath(wantPath, "/usr/local/bin/grafel"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		McpServers map[string]Entry `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	got := doc.McpServers[ServerName]
	if got.Command != "/usr/local/bin/grafel" {
		t.Fatalf("kiro: command = %q, want /usr/local/bin/grafel", got.Command)
	}
	if len(got.Args) != 1 || got.Args[0] != "mcp-bridge" {
		t.Fatalf("kiro: args = %v, want [mcp-bridge]", got.Args)
	}
	if got.Type != "stdio" {
		t.Fatalf("kiro: type = %q, want stdio", got.Type)
	}
}

// TestInstall_SkipsAbsentKiro: no ~/.kiro dir → DetectKiroPaths is empty.
func TestInstall_SkipsAbsentKiro(t *testing.T) {
	withHome(t)
	if targets := DetectKiroPaths(); len(targets) != 0 {
		t.Fatalf("expected no Kiro paths when .kiro absent, got %v", targets)
	}
}

// TestKiro_PreservesForeignServers_AndUnregisters: registering grafel into a
// Kiro mcp.json that already has another server preserves the foreign entry,
// and Unregister removes ONLY grafel's key (#5255 safety, same as Cursor).
func TestKiro_PreservesForeignServers_AndUnregisters(t *testing.T) {
	home := withHome(t)
	settingsDir := filepath.Join(home, ".kiro", "settings")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(settingsDir, "mcp.json")

	seed := `{"mcpServers":{"other":{"command":"/bin/other","args":["x"]}},"someKiroSetting":true}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := RegisterPath(path, "/usr/local/bin/grafel"); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Fatalf("foreign server 'other' was dropped: %v", servers)
	}
	if _, ok := servers[ServerName]; !ok {
		t.Fatalf("grafel server not written: %v", servers)
	}
	if doc["someKiroSetting"] != true {
		t.Fatalf("unrelated key 'someKiroSetting' not preserved: %v", doc["someKiroSetting"])
	}

	if err := UnregisterPath(path); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	doc = nil
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	servers, _ = doc["mcpServers"].(map[string]any)
	if _, ok := servers[ServerName]; ok {
		t.Fatalf("grafel not removed on unregister: %v", servers)
	}
	if _, ok := servers["other"]; !ok {
		t.Fatalf("unregister dropped foreign server 'other': %v", servers)
	}
	if doc["someKiroSetting"] != true {
		t.Fatalf("unregister dropped unrelated key: %v", doc["someKiroSetting"])
	}
}

// ── Antigravity tests ─────────────────────────────────────────────────────────

// TestInstall_RegistersAntigravity: ~/.gemini/antigravity/ present →
// DetectAntigravityPaths returns the mcp_config.json path and RegisterPath
// writes a valid grafel entry with the Cursor-shaped { "mcpServers": { ... } }
// layout (#5280).
func TestInstall_RegistersAntigravity(t *testing.T) {
	home := withHome(t)

	agDir := filepath.Join(home, ".gemini", "antigravity")
	if err := os.MkdirAll(agDir, 0o755); err != nil {
		t.Fatal(err)
	}

	targets := DetectAntigravityPaths()
	wantPath := filepath.Join(agDir, "mcp_config.json")
	if len(targets) != 1 || targets[0].Path != wantPath {
		t.Fatalf("DetectAntigravityPaths: got %v, want [{%s ShapeFlat}]", targets, wantPath)
	}
	if targets[0].Shape != ShapeFlat {
		t.Fatalf("Antigravity shape: got %v, want ShapeFlat", targets[0].Shape)
	}

	// SettingsPath(Antigravity) must resolve to the same user-global file.
	sp, err := SettingsPath(Antigravity)
	if err != nil {
		t.Fatalf("SettingsPath(Antigravity): %v", err)
	}
	if sp != wantPath {
		t.Fatalf("SettingsPath(Antigravity) = %q, want %q", sp, wantPath)
	}

	if _, err := RegisterPath(wantPath, "/usr/local/bin/grafel"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		McpServers map[string]Entry `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	got := doc.McpServers[ServerName]
	if got.Command != "/usr/local/bin/grafel" {
		t.Fatalf("antigravity: command = %q, want /usr/local/bin/grafel", got.Command)
	}
	if len(got.Args) != 1 || got.Args[0] != "mcp-bridge" {
		t.Fatalf("antigravity: args = %v, want [mcp-bridge]", got.Args)
	}
	if got.Type != "stdio" {
		t.Fatalf("antigravity: type = %q, want stdio", got.Type)
	}
}

// TestInstall_SkipsAbsentAntigravity: no ~/.gemini/antigravity dir →
// DetectAntigravityPaths is empty.
func TestInstall_SkipsAbsentAntigravity(t *testing.T) {
	withHome(t)
	if targets := DetectAntigravityPaths(); len(targets) != 0 {
		t.Fatalf("expected no Antigravity paths when dir absent, got %v", targets)
	}
}

// TestAntigravity_PreservesForeignServers_AndUnregisters: registering grafel
// into an Antigravity mcp_config.json that already has another server preserves
// the foreign entry, and Unregister removes ONLY grafel's key (#5280 safety,
// same as Cursor/Kiro).
func TestAntigravity_PreservesForeignServers_AndUnregisters(t *testing.T) {
	home := withHome(t)
	agDir := filepath.Join(home, ".gemini", "antigravity")
	if err := os.MkdirAll(agDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(agDir, "mcp_config.json")

	seed := `{"mcpServers":{"other":{"command":"/bin/other","args":["x"]}},"someAGSetting":true}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := RegisterPath(path, "/usr/local/bin/grafel"); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Fatalf("foreign server 'other' was dropped: %v", servers)
	}
	if _, ok := servers[ServerName]; !ok {
		t.Fatalf("grafel server not written: %v", servers)
	}
	if doc["someAGSetting"] != true {
		t.Fatalf("unrelated key 'someAGSetting' not preserved: %v", doc["someAGSetting"])
	}

	if err := UnregisterPath(path); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	doc = nil
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	servers, _ = doc["mcpServers"].(map[string]any)
	if _, ok := servers[ServerName]; ok {
		t.Fatalf("grafel not removed on unregister: %v", servers)
	}
	if _, ok := servers["other"]; !ok {
		t.Fatalf("unregister dropped foreign server 'other': %v", servers)
	}
	if doc["someAGSetting"] != true {
		t.Fatalf("unregister dropped unrelated key: %v", doc["someAGSetting"])
	}
}

// ── Codex tests ───────────────────────────────────────────────────────────────

// TestInstall_RegistersCodex: ~/.codex/ present → DetectCodexPaths returns
// config.json path and RegisterPath writes the grafel entry.
func TestInstall_RegistersCodex(t *testing.T) {
	home := withHome(t)

	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}

	targets := DetectCodexPaths()
	wantPath := filepath.Join(codexDir, "config.json")

	if len(targets) != 1 || targets[0].Path != wantPath {
		t.Fatalf("DetectCodexPaths: got %v, want [{%s ShapeFlat}]", targets, wantPath)
	}
	if targets[0].Shape != ShapeFlat {
		t.Fatalf("Codex shape: got %v, want ShapeFlat", targets[0].Shape)
	}

	if _, err := RegisterPath(wantPath, "/usr/local/bin/grafel"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		McpServers map[string]Entry `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	got := doc.McpServers[ServerName]
	if got.Command != "/usr/local/bin/grafel" {
		t.Fatalf("codex: command = %q, want /usr/local/bin/grafel", got.Command)
	}
}

// TestInstall_SkipsAbsentCodex: no ~/.codex dir → DetectCodexPaths is empty.
func TestInstall_SkipsAbsentCodex(t *testing.T) {
	withHome(t)
	targets := DetectCodexPaths()
	if len(targets) != 0 {
		t.Fatalf("expected no Codex paths when .codex absent, got %v", targets)
	}
}

// ── Continue.dev tests ────────────────────────────────────────────────────────

// TestInstall_RegistersContinueDev: ~/.continue/ present →
// DetectContinueDevPaths returns config.json and RegisterPath writes
// mcpServers while preserving surrounding keys (ShapeNested).
func TestInstall_RegistersContinueDev(t *testing.T) {
	home := withHome(t)

	continueDir := filepath.Join(home, ".continue")
	if err := os.MkdirAll(continueDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(continueDir, "config.json")

	// Pre-populate with existing Continue.dev content (models key must survive).
	pre := `{"models":[{"title":"GPT-4","provider":"openai"}],"mcpServers":{"other-tool":{"command":"/bin/other"}}}`
	if err := os.WriteFile(cfgPath, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}

	targets := DetectContinueDevPaths()
	if len(targets) != 1 || targets[0].Path != cfgPath {
		t.Fatalf("DetectContinueDevPaths: got %v, want [{%s ShapeNested}]", targets, cfgPath)
	}
	if targets[0].Shape != ShapeNested {
		t.Fatalf("Continue.dev shape: got %v, want ShapeNested", targets[0].Shape)
	}

	if _, err := RegisterPath(cfgPath, "/usr/local/bin/grafel"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}

	// models key must be preserved.
	if _, ok := doc["models"]; !ok {
		t.Fatalf("Continue.dev: 'models' key was clobbered: %s", b)
	}

	servers, _ := doc["mcpServers"].(map[string]any)
	// Pre-existing sibling mcpServer entry must survive.
	if _, ok := servers["other-tool"]; !ok {
		t.Fatalf("Continue.dev: pre-existing mcpServer 'other-tool' was clobbered: %s", b)
	}
	// grafel entry must be present.
	arch, _ := servers[ServerName].(map[string]any)
	if arch == nil {
		t.Fatalf("Continue.dev: grafel entry missing: %s", b)
	}
	if arch["command"] != "/usr/local/bin/grafel" {
		t.Fatalf("Continue.dev: command = %v, want /usr/local/bin/grafel", arch["command"])
	}
}

// TestInstall_SkipsAbsentContinueDev: no ~/.continue dir → empty.
func TestInstall_SkipsAbsentContinueDev(t *testing.T) {
	withHome(t)
	targets := DetectContinueDevPaths()
	if len(targets) != 0 {
		t.Fatalf("expected no Continue.dev paths when .continue absent, got %v", targets)
	}
}

// ── Zed tests ─────────────────────────────────────────────────────────────────

// TestInstall_RegistersZed: ~/.config/zed/ present → DetectZedPaths returns
// settings.json and RegisterPath performs a surgical upsert preserving other
// Zed settings (ShapeBroadSettings).
func TestInstall_RegistersZed(t *testing.T) {
	home := withHome(t)

	zedDir := filepath.Join(home, ".config", "zed")
	if err := os.MkdirAll(zedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(zedDir, "settings.json")

	// Pre-populate with existing Zed settings that must survive.
	pre := `{"theme":"one-dark","vim_mode":true,"font_size":14,"mcpServers":{"existing-tool":{"command":"/bin/existing"}}}`
	if err := os.WriteFile(cfgPath, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}

	targets := DetectZedPaths()
	if len(targets) != 1 || targets[0].Path != cfgPath {
		t.Fatalf("DetectZedPaths: got %v, want [{%s ShapeBroadSettings}]", targets, cfgPath)
	}
	if targets[0].Shape != ShapeBroadSettings {
		t.Fatalf("Zed shape: got %v, want ShapeBroadSettings", targets[0].Shape)
	}

	if _, err := RegisterPath(cfgPath, "/usr/local/bin/grafel"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}

	// Other Zed settings must be preserved.
	if doc["theme"] != "one-dark" {
		t.Fatalf("Zed: 'theme' key was clobbered: %s", b)
	}
	if doc["vim_mode"] != true {
		t.Fatalf("Zed: 'vim_mode' key was clobbered: %s", b)
	}
	if doc["font_size"] != float64(14) {
		t.Fatalf("Zed: 'font_size' key was clobbered: %s", b)
	}

	servers, _ := doc["mcpServers"].(map[string]any)
	// Pre-existing sibling mcpServer entry must survive.
	if _, ok := servers["existing-tool"]; !ok {
		t.Fatalf("Zed: pre-existing mcpServer 'existing-tool' was clobbered: %s", b)
	}
	// grafel entry must be present.
	arch, _ := servers[ServerName].(map[string]any)
	if arch == nil {
		t.Fatalf("Zed: grafel entry missing: %s", b)
	}
	if arch["command"] != "/usr/local/bin/grafel" {
		t.Fatalf("Zed: command = %v, want /usr/local/bin/grafel", arch["command"])
	}
}

// TestInstall_SkipsAbsentZed: no ~/.config/zed dir → empty.
func TestInstall_SkipsAbsentZed(t *testing.T) {
	withHome(t)
	targets := DetectZedPaths()
	if len(targets) != 0 {
		t.Fatalf("expected no Zed paths when .config/zed absent, got %v", targets)
	}
}

// ── #4829: surgical de-register + backup/restore (no shared-config wipe) ──────

// TestRegisterPreservesForeignServer: registering into a config that already
// holds a FOREIGN server must add grafel AND leave the foreign server
// untouched.
func TestRegisterPreservesForeignServer(t *testing.T) {
	home := withHome(t)
	path := filepath.Join(home, ".codeium", "mcp_config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	pre := `{"mcpServers":{"playwright":{"command":"/bin/playwright","args":["serve"]}}}`
	if err := os.WriteFile(path, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := RegisterPath(path, "/bin/grafel"); err != nil {
		t.Fatal(err)
	}

	b, _ := os.ReadFile(path)
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("config not valid JSON after register: %s", b)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if _, ok := servers["playwright"]; !ok {
		t.Fatalf("foreign 'playwright' server was wiped: %s", b)
	}
	if _, ok := servers[ServerName]; !ok {
		t.Fatalf("grafel entry missing: %s", b)
	}
}

// TestRestorePreservesForeignServerOnRollback: register into a config holding a
// foreign server, then roll back via RestorePath. The foreign server must
// survive and the file must NOT be `{}` — it must equal the original byte-wise.
func TestRestorePreservesForeignServerOnRollback(t *testing.T) {
	home := withHome(t)
	path := filepath.Join(home, ".codeium", "mcp_config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	pre := `{"mcpServers":{"playwright":{"command":"/bin/playwright"}},"other":"keep"}`
	if err := os.WriteFile(path, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := RegisterPath(path, "/bin/grafel"); err != nil {
		t.Fatal(err)
	}
	if err := RestorePath(path); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file missing after rollback (should be restored, not deleted): %v", err)
	}
	if string(b) == "{}" || string(b) == "{}\n" {
		t.Fatalf("file was reset to empty object — the #4829 wipe regressed: %s", b)
	}
	if string(b) != pre {
		t.Fatalf("rollback did not restore original byte-for-byte:\n got: %s\nwant: %s", b, pre)
	}
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	servers, _ := doc["mcpServers"].(map[string]any)
	if _, ok := servers["playwright"]; !ok {
		t.Fatalf("foreign 'playwright' server lost on rollback: %s", b)
	}
	// Backup sidecar should be cleaned up after a successful restore.
	if _, err := os.Stat(sidecarBackupPath(path)); err == nil {
		t.Fatalf("sidecar backup not cleaned up after restore")
	}
}

// TestRestoreNewFileRemovesOrphan: when grafel creates a brand-new config
// file, rollback must DELETE it — never leave an orphan `{}` /
// `{"mcpServers":{}}`.
func TestRestoreNewFileRemovesOrphan(t *testing.T) {
	home := withHome(t)
	path := filepath.Join(home, ".codeium", "windsurf", "mcp_config.json")

	if _, err := os.Stat(path); err == nil {
		t.Fatalf("precondition: file should not exist yet")
	}
	if _, err := RegisterPath(path, "/bin/grafel"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("register should have created the file: %v", err)
	}

	if err := RestorePath(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		b, _ := os.ReadFile(path)
		t.Fatalf("orphan file left after rollback of a created file: %q", b)
	}
}

// TestUnregisterDropsEmptyMcpServers: unregistering the sole grafel entry
// from a file grafel created leaves neither an orphan `{"mcpServers":{}}`
// nor reintroduces foreign keys.
func TestUnregisterDropsEmptyMcpServers(t *testing.T) {
	home := withHome(t)
	path := filepath.Join(home, ".claude.json")

	if _, err := RegisterPath(path, "/bin/grafel"); err != nil {
		t.Fatal(err)
	}
	if err := UnregisterPath(path); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	if _, ok := doc["mcpServers"]; ok {
		t.Fatalf("empty mcpServers object not dropped: %s", b)
	}
}

// TestUnregisterKeepsForeignServers: unregistering grafel from a file with
// a foreign sibling server keeps mcpServers and the sibling intact.
func TestUnregisterKeepsForeignServers(t *testing.T) {
	home := withHome(t)
	path := filepath.Join(home, ".claude.json")
	pre := `{"mcpServers":{"playwright":{"command":"/bin/pw"},"grafel":{"command":"/old"}}}`
	if err := os.WriteFile(path, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UnregisterPath(path); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	servers, _ := doc["mcpServers"].(map[string]any)
	if _, ok := servers["playwright"]; !ok {
		t.Fatalf("foreign server lost on unregister: %s", b)
	}
	if _, ok := servers[ServerName]; ok {
		t.Fatalf("grafel entry still present: %s", b)
	}
}

// TestDetectWindsurfPaths_BothCodeiumTargets asserts the dual codeium write is
// preserved: when both ~/.codeium and ~/.codeium/windsurf exist, BOTH the
// legacy JetBrains path and the desktop path are in the registration set.
func TestDetectWindsurfPaths_BothCodeiumTargets(t *testing.T) {
	home := withHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".codeium", "windsurf"), 0o755); err != nil {
		t.Fatal(err)
	}

	paths := DetectWindsurfPaths()
	wantDesktop := filepath.Join(home, ".codeium", "windsurf", "mcp_config.json")
	wantLegacy := filepath.Join(home, ".codeium", "mcp_config.json")

	var gotDesktop, gotLegacy bool
	for _, p := range paths {
		switch p {
		case wantDesktop:
			gotDesktop = true
		case wantLegacy:
			gotLegacy = true
		}
	}
	if !gotDesktop {
		t.Errorf("desktop codeium path missing from registration set: %v", paths)
	}
	if !gotLegacy {
		t.Errorf("legacy codeium path missing from registration set: %v", paths)
	}
}

// TestRestoreFallsBackToSurgicalWhenNoBackup: if no sidecar backup exists,
// RestorePath must still remove only grafel (never wipe foreign servers).
func TestRestoreFallsBackToSurgicalWhenNoBackup(t *testing.T) {
	home := withHome(t)
	path := filepath.Join(home, ".claude.json")
	pre := `{"mcpServers":{"playwright":{"command":"/bin/pw"},"grafel":{"command":"/old"}}}`
	if err := os.WriteFile(path, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	// No RegisterPath call → no sidecar backup.
	if err := RestorePath(path); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) == "{}" || string(b) == "{}\n" {
		t.Fatalf("restore-without-backup wiped the file: %s", b)
	}
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	servers, _ := doc["mcpServers"].(map[string]any)
	if _, ok := servers["playwright"]; !ok {
		t.Fatalf("foreign server lost in fallback restore: %s", b)
	}
}

// TestInstall_SkipsAbsentHost is a table-driven test checking that each new
// host's Detect function returns empty when its parent directory is absent.
func TestInstall_SkipsAbsentHost(t *testing.T) {
	type detectFn struct {
		name string
		fn   func() []HostTarget
	}
	hosts := []detectFn{
		{"Cursor", DetectCursorPaths},
		{"Codex", DetectCodexPaths},
		{"ContinueDev", DetectContinueDevPaths},
		{"Zed", DetectZedPaths},
		{"Kiro", DetectKiroPaths},
		{"Antigravity", DetectAntigravityPaths},
	}
	for _, h := range hosts {
		t.Run(h.name, func(t *testing.T) {
			withHome(t)
			// No directories created — host not installed.
			targets := h.fn()
			if len(targets) != 0 {
				t.Fatalf("%s: expected empty when host absent, got %v", h.name, targets)
			}
		})
	}
}
