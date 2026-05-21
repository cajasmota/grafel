//go:build !windows

package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSelectSocketPath_PreferXDGWhenSet(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	t.Setenv("HOME", "/home/user")

	path, err := selectSocketPath()
	if err != nil {
		t.Fatalf("selectSocketPath: %v", err)
	}

	expected := "/run/user/1000/archigraph/daemon.sock"
	if path != expected {
		t.Fatalf("expected %q, got %q", expected, path)
	}

	if len(path) > UnixSocketPathMax {
		t.Fatalf("XDG path exceeds limit: %d > %d", len(path), UnixSocketPathMax)
	}
}

func TestSelectSocketPath_FallbackToHomeWhenXDGTooLong(t *testing.T) {
	// Simulate XDG path exceeding limit
	t.Setenv("XDG_RUNTIME_DIR", "/very/long/path/that/would/exceed/the/socket/limit/when/combined/with/archigraph/daemon/sock")
	t.Setenv("HOME", "/home/user")

	path, err := selectSocketPath()
	if err != nil {
		t.Fatalf("selectSocketPath: %v", err)
	}

	expected := "/home/user/.archigraph/sockets/daemon.sock"
	if path != expected {
		t.Fatalf("expected fallback to home %q, got %q", expected, path)
	}
}

func TestSelectSocketPath_UseHomeWhenXDGNotSet(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("HOME", "/home/user")

	path, err := selectSocketPath()
	if err != nil {
		t.Fatalf("selectSocketPath: %v", err)
	}

	expected := "/home/user/.archigraph/sockets/daemon.sock"
	if path != expected {
		t.Fatalf("expected %q, got %q", expected, path)
	}

	if len(path) > UnixSocketPathMax {
		t.Fatalf("home path exceeds limit: %d > %d", len(path), UnixSocketPathMax)
	}
}

func TestSelectSocketPath_ErrorWhenBothTooLong(t *testing.T) {
	// Both XDG and home paths exceed 104 char limit
	// Use a realistic but long path: /very/long/directory/path/that/when/combined/with/archigraph/daemon/sock/exceeds/limit
	longPath := "/very/long/directory/path/that/when/combined/with/archigraph/daemon/sock/exceeds/the/limit/constraint"
	t.Setenv("XDG_RUNTIME_DIR", longPath)
	t.Setenv("HOME", longPath)

	_, err := selectSocketPath()
	if err == nil {
		t.Fatal("expected error when both paths exceed 104 char limit")
	}
}

func TestDefaultLayout_XDGPathWhenAvailable(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	t.Setenv("HOME", "/home/user")
	t.Setenv(EnvRoot, "")

	layout, err := DefaultLayout()
	if err != nil {
		t.Fatalf("DefaultLayout: %v", err)
	}

	if len(layout.SocketPath) > UnixSocketPathMax {
		t.Fatalf("socket path exceeds limit: %d > %d", len(layout.SocketPath), UnixSocketPathMax)
	}

	// Home-based paths should still work (PID, logs under ~/.archigraph)
	if !filepath.HasPrefix(layout.LogDir, "/home/user/.archigraph") {
		t.Fatalf("expected log dir under home, got %q", layout.LogDir)
	}
}

func TestDefaultLayout_DaemonRootEnvOverridesXDG(t *testing.T) {
	tmpRoot := t.TempDir()

	t.Setenv("ARCHIGRAPH_DAEMON_ROOT", tmpRoot)
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	t.Setenv("HOME", "/home/user")

	layout, err := DefaultLayout()
	if err != nil {
		t.Fatalf("DefaultLayout: %v", err)
	}

	// When ARCHIGRAPH_DAEMON_ROOT is set, it overrides all paths
	if !filepath.HasPrefix(layout.SocketPath, tmpRoot) {
		t.Fatalf("expected socket under ARCHIGRAPH_DAEMON_ROOT %q, got %q", tmpRoot, layout.SocketPath)
	}
	if !filepath.HasPrefix(layout.LogDir, tmpRoot) {
		t.Fatalf("expected log dir under ARCHIGRAPH_DAEMON_ROOT %q, got %q", tmpRoot, layout.LogDir)
	}
}

func TestEnsureLayout_CreatesDirs(t *testing.T) {
	tmpRoot := t.TempDir()

	layout := Layout{
		Root:       tmpRoot,
		SocketDir:  filepath.Join(tmpRoot, "sockets"),
		SocketPath: filepath.Join(tmpRoot, "sockets", "daemon.sock"),
		PIDPath:    filepath.Join(tmpRoot, "daemon.pid"),
		LogDir:     filepath.Join(tmpRoot, "logs"),
		LogPath:    filepath.Join(tmpRoot, "logs", "daemon.log"),
	}

	if err := EnsureLayout(layout); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	// Check directories were created
	for _, dir := range []string{layout.Root, layout.SocketDir, layout.LogDir} {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("directory not created: %q: %v", dir, err)
		}
	}
}

func TestEnsureLayout_HandlesSeparateSocketDir(t *testing.T) {
	// Test when socket dir is outside root (e.g., XDG path)
	tmpRoot := t.TempDir()
	tmpXDG := t.TempDir()

	layout := Layout{
		Root:       tmpRoot,
		SocketDir:  filepath.Join(tmpXDG, "archigraph"),
		SocketPath: filepath.Join(tmpXDG, "archigraph", "daemon.sock"),
		PIDPath:    filepath.Join(tmpRoot, "daemon.pid"),
		LogDir:     filepath.Join(tmpRoot, "logs"),
		LogPath:    filepath.Join(tmpRoot, "logs", "daemon.log"),
	}

	if err := EnsureLayout(layout); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	// Both socket dir and log dir should exist
	if _, err := os.Stat(layout.SocketDir); err != nil {
		t.Fatalf("socket dir not created: %v", err)
	}
	if _, err := os.Stat(layout.LogDir); err != nil {
		t.Fatalf("log dir not created: %v", err)
	}
}
