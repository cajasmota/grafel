package detect

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStack(t *testing.T) {
	t.Run("go", func(t *testing.T) {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "go.mod"), "module x\n")
		if got := Stack(dir); got != "go" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("next", func(t *testing.T) {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "package.json"), `{"name":"x"}`)
		write(t, filepath.Join(dir, "next.config.js"), "")
		if got := Stack(dir); got != "next" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("python", func(t *testing.T) {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "manage.py"), "")
		if got := Stack(dir); got != "python" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestDetectMonorepoPNPM(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "pnpm-workspace.yaml"), "packages:\n  - 'packages/*'\n  - 'apps/*'\n")
	write(t, filepath.Join(dir, "packages/a/package.json"), `{"name":"a"}`)
	write(t, filepath.Join(dir, "packages/b/package.json"), `{"name":"b"}`)
	write(t, filepath.Join(dir, "apps/web/package.json"), `{"name":"web"}`)
	m, err := DetectMonorepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != KindPNPM {
		t.Fatalf("kind: %q", m.Kind)
	}
	if len(m.Packages) != 3 {
		t.Fatalf("packages: %+v", m.Packages)
	}
}

func TestDetectMonorepoNPMWorkspaces(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "package.json"), `{"name":"root","workspaces":["packages/*"]}`)
	write(t, filepath.Join(dir, "packages/a/package.json"), `{"name":"a"}`)
	write(t, filepath.Join(dir, "packages/b/package.json"), `{"name":"b"}`)
	m, err := DetectMonorepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != KindNPM {
		t.Fatalf("kind: %q", m.Kind)
	}
	if len(m.Packages) != 2 {
		t.Fatalf("packages: %+v", m.Packages)
	}
}

func TestDetectMonorepoNx(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "nx.json"), `{}`)
	write(t, filepath.Join(dir, "package.json"), `{"workspaces":["apps/*","libs/*"]}`)
	write(t, filepath.Join(dir, "apps/web/package.json"), `{}`)
	write(t, filepath.Join(dir, "libs/util/package.json"), `{}`)
	m, _ := DetectMonorepo(dir)
	if m.Kind != KindNx || len(m.Packages) != 2 {
		t.Fatalf("nx: %+v", m)
	}
}

func TestDetectMonorepoNone(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module x\n")
	m, _ := DetectMonorepo(dir)
	if m.Kind != KindNone {
		t.Fatalf("expected KindNone, got %q", m.Kind)
	}
}
