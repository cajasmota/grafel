package mcpreg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// codexConfig returns the Codex config.toml path under the isolated HOME.
func codexConfig(t *testing.T) string {
	t.Helper()
	p, err := SettingsPath(Codex)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(p, ".toml") {
		t.Fatalf("Codex SettingsPath should be a .toml file, got %q", p)
	}
	return p
}

func TestTOML_RegisterCreatesTable(t *testing.T) {
	withHome(t)
	path := codexConfig(t)
	if _, err := Register(Codex, "/bin/grafel", "/r.json"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, "[mcp_servers.grafel]") {
		t.Fatalf("missing grafel table:\n%s", got)
	}
	if !strings.Contains(got, `command = "/bin/grafel"`) {
		t.Fatalf("missing/incorrect command:\n%s", got)
	}
	if !strings.Contains(got, `args = ["mcp-bridge"]`) {
		t.Fatalf("missing/incorrect args:\n%s", got)
	}
}

func TestTOML_PreservesForeignTablesAndTopLevel(t *testing.T) {
	withHome(t)
	path := codexConfig(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	pre := `model = "o4-mini"
approval_policy = "on-request"

[mcp_servers.other]
command = "/usr/bin/other"
args = ["serve"]

[history]
persistence = "save-all"
`
	if err := os.WriteFile(path, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Register(Codex, "/bin/grafel", "/r.json"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	s := string(got)
	for _, want := range []string{
		`model = "o4-mini"`,
		`approval_policy = "on-request"`,
		"[mcp_servers.other]",
		`command = "/usr/bin/other"`,
		"[history]",
		`persistence = "save-all"`,
		"[mcp_servers.grafel]",
		`command = "/bin/grafel"`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("expected %q preserved/present, got:\n%s", want, s)
		}
	}
}

func TestTOML_Idempotent(t *testing.T) {
	withHome(t)
	path := codexConfig(t)
	if _, err := Register(Codex, "/bin/grafel", "/r.json"); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)
	if _, err := Register(Codex, "/bin/grafel", "/r.json"); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Fatalf("re-register changed file:\nFIRST:\n%s\nSECOND:\n%s", first, second)
	}
	// Exactly one grafel table.
	if n := strings.Count(string(second), "[mcp_servers.grafel]"); n != 1 {
		t.Fatalf("expected exactly 1 grafel table, got %d:\n%s", n, second)
	}
}

func TestTOML_UpdatesCommandOnReRegister(t *testing.T) {
	withHome(t)
	path := codexConfig(t)
	if _, err := Register(Codex, "/old/grafel", "/r.json"); err != nil {
		t.Fatal(err)
	}
	if _, err := Register(Codex, "/new/grafel", "/r.json"); err != nil {
		t.Fatal(err)
	}
	s, _ := os.ReadFile(path)
	if strings.Contains(string(s), "/old/grafel") {
		t.Fatalf("stale command not replaced:\n%s", s)
	}
	if !strings.Contains(string(s), `command = "/new/grafel"`) {
		t.Fatalf("new command missing:\n%s", s)
	}
	if n := strings.Count(string(s), "[mcp_servers.grafel]"); n != 1 {
		t.Fatalf("expected exactly 1 grafel table, got %d:\n%s", n, s)
	}
}

func TestTOML_UnregisterRemovesOnlyGrafel(t *testing.T) {
	withHome(t)
	path := codexConfig(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	pre := `model = "o4-mini"

[mcp_servers.other]
command = "/usr/bin/other"
`
	if err := os.WriteFile(path, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Register(Codex, "/bin/grafel", "/r.json"); err != nil {
		t.Fatal(err)
	}
	if err := Unregister(Codex); err != nil {
		t.Fatal(err)
	}
	s, _ := os.ReadFile(path)
	str := string(s)
	if strings.Contains(str, "[mcp_servers.grafel]") {
		t.Fatalf("grafel table not removed:\n%s", str)
	}
	if !strings.Contains(str, "[mcp_servers.other]") || !strings.Contains(str, `model = "o4-mini"`) {
		t.Fatalf("foreign content lost on unregister:\n%s", str)
	}
}

func TestTOML_UnregisterIdempotentAndMissingFile(t *testing.T) {
	withHome(t)
	// No file at all: no-op.
	if err := Unregister(Codex); err != nil {
		t.Fatalf("unregister with no file should be nil, got %v", err)
	}
	// File with only foreign content, no grafel: no-op, unchanged.
	path := codexConfig(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	pre := "model = \"x\"\n\n[mcp_servers.other]\ncommand = \"/o\"\n"
	if err := os.WriteFile(path, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Unregister(Codex); err != nil {
		t.Fatal(err)
	}
	s, _ := os.ReadFile(path)
	if string(s) != pre {
		t.Fatalf("file changed despite no grafel block:\n%s", s)
	}
}

func TestTOML_UnregisterDeletesSoleGrafelFile(t *testing.T) {
	withHome(t)
	path := codexConfig(t)
	// grafel-created file (no prior content).
	if _, err := Register(Codex, "/bin/grafel", "/r.json"); err != nil {
		t.Fatal(err)
	}
	if err := Unregister(Codex); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		b, _ := os.ReadFile(path)
		t.Fatalf("expected sole-grafel file to be deleted, still present:\n%s", b)
	}
}

func TestTOML_RestoreRollsBackToOriginal(t *testing.T) {
	withHome(t)
	path := codexConfig(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := "model = \"o4-mini\"\n\n[mcp_servers.other]\ncommand = \"/o\"\n"
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Register(Codex, "/bin/grafel", "/r.json"); err != nil {
		t.Fatal(err)
	}
	// Rollback restores the byte-exact original (backup is format-agnostic).
	if err := RestorePath(path); err != nil {
		t.Fatal(err)
	}
	s, _ := os.ReadFile(path)
	if string(s) != orig {
		t.Fatalf("restore did not reproduce original:\nGOT:\n%s\nWANT:\n%s", s, orig)
	}
}

func TestTOML_RestoreDeletesCreatedFile(t *testing.T) {
	withHome(t)
	path := codexConfig(t)
	// grafel created the file (no original) → restore deletes it.
	if _, err := Register(Codex, "/bin/grafel", "/r.json"); err != nil {
		t.Fatal(err)
	}
	if err := RestorePath(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("restore should have deleted grafel-created file %s", path)
	}
}
