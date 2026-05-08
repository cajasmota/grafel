package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestInstallIdempotent(t *testing.T) {
	repo := gitRepo(t)
	if err := Install(repo, "/usr/local/bin/archigraph"); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(repo, ".git/hooks/post-commit"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Install(repo, "/usr/local/bin/archigraph"); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(filepath.Join(repo, ".git/hooks/post-commit"))
	if string(first) != string(second) {
		t.Fatalf("non-idempotent install:\n%s\n---\n%s", first, second)
	}
	// Every hook installed.
	for _, name := range HookNames {
		b, err := os.ReadFile(filepath.Join(repo, ".git/hooks", name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), MarkerBegin) {
			t.Fatalf("%s missing marker: %s", name, b)
		}
	}
}

func TestInstallPreservesUserContent(t *testing.T) {
	repo := gitRepo(t)
	user := "#!/bin/sh\necho user-script\n"
	hookPath := filepath.Join(repo, ".git/hooks/post-commit")
	if err := os.WriteFile(hookPath, []byte(user), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Install(repo, "/bin/archigraph"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(hookPath)
	if !strings.Contains(string(got), "echo user-script") {
		t.Fatalf("user content lost: %s", got)
	}
	if err := Uninstall(repo); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(hookPath)
	if !strings.Contains(string(got), "echo user-script") {
		t.Fatalf("user content lost on uninstall: %s", got)
	}
	if strings.Contains(string(got), MarkerBegin) {
		t.Fatalf("marker still present after uninstall: %s", got)
	}
}

func TestStripBlockUnterminated(t *testing.T) {
	got := stripBlock("a\n" + MarkerBegin + "\noops")
	if strings.Contains(got, MarkerBegin) {
		t.Fatalf("unterminated block not stripped: %q", got)
	}
}
