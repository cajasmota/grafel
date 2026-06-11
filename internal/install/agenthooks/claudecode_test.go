package agenthooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// readDoc loads .claude/settings.json from a repo root as a generic doc.
func readDoc(t *testing.T, repoRoot string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, SettingsRelPath))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	doc := map[string]any{}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	return doc
}

// managedCount counts marker-identified managed PreToolUse entries.
func managedCount(doc map[string]any) int {
	hooks, _ := doc["hooks"].(map[string]any)
	if hooks == nil {
		return 0
	}
	pre, _ := hooks["PreToolUse"].([]any)
	n := 0
	for _, raw := range pre {
		group, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		inner, _ := group["hooks"].([]any)
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			if cmd, _ := hm["command"].(string); containsMarker(cmd) {
				n++
			}
		}
	}
	return n
}

// (a) fresh .claude/settings.json gets the marker-wrapped PreToolUse entry.
func TestInstall_Fresh(t *testing.T) {
	root := t.TempDir()
	path, err := installClaudeCode(root)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if path != filepath.Join(root, SettingsRelPath) {
		t.Fatalf("unexpected settings path: %s", path)
	}
	if !claudeCodeInstalled(root) {
		t.Fatal("IsInstalled = false after Install")
	}
	doc := readDoc(t, root)
	if got := managedCount(doc); got != 1 {
		t.Fatalf("managed entries = %d, want 1", got)
	}
	// Nudge script written and executable.
	scriptPath := filepath.Join(root, NudgeScriptRelPath)
	fi, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("nudge script not written: %v", err)
	}
	// Windows has no unix exec bit: os.Chmod's 0o100 is a no-op there and the
	// nudge hook is invoked via its interpreter (executability is by extension,
	// not file mode). Only assert the x-bit on non-Windows platforms.
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o100 == 0 {
		t.Fatalf("nudge script not executable: %v", fi.Mode())
	}
	// Matcher targets the structural tool surface.
	hooks := doc["hooks"].(map[string]any)
	pre := hooks["PreToolUse"].([]any)
	group := pre[0].(map[string]any)
	if group["matcher"] != MatcherTools {
		t.Fatalf("matcher = %v, want %v", group["matcher"], MatcherTools)
	}
}

// (b) re-run is idempotent (no duplicate, surrounding JSON preserved).
func TestInstall_Idempotent(t *testing.T) {
	root := t.TempDir()

	// Seed with unrelated settings + an unmanaged PreToolUse hook the user
	// authored. Both must survive install untouched.
	seed := map[string]any{
		"model":       "claude-opus",
		"customField": map[string]any{"nested": []any{"a", "b"}},
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Write",
					"hooks": []any{
						map[string]any{"type": "command", "command": "echo user-hook"},
					},
				},
			},
		},
	}
	seedBytes, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, SettingsRelPath), seedBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		if _, err := installClaudeCode(root); err != nil {
			t.Fatalf("Install #%d: %v", i, err)
		}
	}

	doc := readDoc(t, root)
	if got := managedCount(doc); got != 1 {
		t.Fatalf("managed entries after 3 installs = %d, want 1", got)
	}
	// Unrelated keys preserved.
	if doc["model"] != "claude-opus" {
		t.Fatalf("model key lost: %v", doc["model"])
	}
	cf, ok := doc["customField"].(map[string]any)
	if !ok || cf["nested"] == nil {
		t.Fatalf("customField lost: %v", doc["customField"])
	}
	// User's unmanaged Write hook preserved alongside ours.
	hooks := doc["hooks"].(map[string]any)
	pre := hooks["PreToolUse"].([]any)
	foundUser := false
	for _, raw := range pre {
		g := raw.(map[string]any)
		if g["matcher"] == "Write" {
			foundUser = true
		}
	}
	if !foundUser {
		t.Fatal("user's unmanaged Write hook was dropped")
	}
	if len(pre) != 2 {
		t.Fatalf("PreToolUse len = %d, want 2 (user + managed)", len(pre))
	}
}

// (c) an existing managed block is replaced in place (not appended).
func TestInstall_ReplacesExistingManaged(t *testing.T) {
	root := t.TempDir()
	if _, err := installClaudeCode(root); err != nil {
		t.Fatal(err)
	}

	// Corrupt/age the managed entry's command but keep the marker so it is
	// still identifiable, then re-install: it must be replaced, not dupped.
	doc := readDoc(t, root)
	hooks := doc["hooks"].(map[string]any)
	pre := hooks["PreToolUse"].([]any)
	group := pre[0].(map[string]any)
	inner := group["hooks"].([]any)
	hm := inner[0].(map[string]any)
	hm["command"] = "# " + Marker + "\nsh /old/stale/path.sh"
	b, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(filepath.Join(root, SettingsRelPath), b, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := installClaudeCode(root); err != nil {
		t.Fatal(err)
	}
	doc2 := readDoc(t, root)
	if got := managedCount(doc2); got != 1 {
		t.Fatalf("managed entries after replace = %d, want 1", got)
	}
	// The stale path must be gone; current command points at the real script.
	hooks2 := doc2["hooks"].(map[string]any)
	pre2 := hooks2["PreToolUse"].([]any)
	cmd := pre2[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"].(string)
	if want := NudgeScriptRelPath; !containsSub(cmd, "archigraph-grep-nudge.sh") {
		t.Fatalf("command not refreshed to current script (%s): %q", want, cmd)
	}
	if containsSub(cmd, "/old/stale/path.sh") {
		t.Fatalf("stale command path survived replace: %q", cmd)
	}
}

// Uninstall removes our entry + script, leaving user content intact.
func TestUninstall(t *testing.T) {
	root := t.TempDir()
	// Seed an unrelated user setting.
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := []byte(`{"model":"x","hooks":{"PreToolUse":[{"matcher":"Write","hooks":[{"type":"command","command":"echo hi"}]}]}}`)
	if err := os.WriteFile(filepath.Join(root, SettingsRelPath), seed, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := installClaudeCode(root); err != nil {
		t.Fatal(err)
	}
	if err := uninstallClaudeCode(root); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if claudeCodeInstalled(root) {
		t.Fatal("still installed after Uninstall")
	}
	if _, err := os.Stat(filepath.Join(root, NudgeScriptRelPath)); !os.IsNotExist(err) {
		t.Fatalf("nudge script not removed: %v", err)
	}
	doc := readDoc(t, root)
	if doc["model"] != "x" {
		t.Fatal("unrelated key lost on uninstall")
	}
	hooks := doc["hooks"].(map[string]any)
	pre := hooks["PreToolUse"].([]any)
	if len(pre) != 1 || pre[0].(map[string]any)["matcher"] != "Write" {
		t.Fatalf("user hook not preserved on uninstall: %v", pre)
	}
	// Idempotent second uninstall.
	if err := uninstallClaudeCode(root); err != nil {
		t.Fatalf("second Uninstall: %v", err)
	}
}

// (d) the heuristic classifies symbol-def greps structural and TODO/string
// greps NOT.
func TestClassifyStructural(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
	}{
		// Structural: definition hunts.
		{`grep -n 'def place_order' service.py`, true},
		{`grep -rn "class OrderViewSet" .`, true},
		{`grep -n "func validateOrder" *.go`, true},
		{`rg "function handleSubmit" src/`, true},
		{`grep -rn "interface PaymentGateway" .`, true},
		// Structural: recursive symbol sweep (who-calls / usage).
		{`grep -rn placeOrder .`, true},
		{`rg OrderService`, true},
		// NOT structural: TODO/FIXME/string sweeps.
		{`grep -rn TODO .`, false},
		{`grep -rn "FIXME: refactor" .`, false},
		{`grep -n XXX file.go`, false},
		// NOT structural: not a grep at all.
		{`ls -la`, false},
		{`cat service.py`, false},
		{`go test ./...`, false},
		// NOT structural: recursive grep for a non-symbol literal.
		{`grep -rn "===" .`, false},
	}
	for _, c := range cases {
		if got := classifyStructural(c.cmd); got != c.want {
			t.Errorf("classifyStructural(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}

// The embedded shell script must carry the marker and never use a blocking
// exit code (advisory-only contract).
func TestNudgeScript_AdvisoryContract(t *testing.T) {
	if !containsSub(NudgeScript, Marker) {
		t.Fatal("nudge script missing marker")
	}
	if !containsSub(NudgeScript, "exit 0") {
		t.Fatal("nudge script must exit 0 (advisory only)")
	}
	// No deny/exit-2 blocking semantics.
	if containsSub(NudgeScript, "exit 2") || containsSub(NudgeScript, `"deny"`) {
		t.Fatal("nudge script must never block/deny")
	}
	// Once-per-session dedup marker logic present.
	if !containsSub(NudgeScript, "marker") {
		t.Fatal("nudge script missing once-per-session dedup")
	}
}

func containsSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
