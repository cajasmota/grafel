//go:build darwin

package service

import (
	"os"
	"path/filepath"
	"testing"
)

// TestExtractPlistHome verifies the HOME value is recovered from a rendered
// LaunchAgent plist — the signal the #5277 uninstall guard compares against.
func TestExtractPlistHome(t *testing.T) {
	opts := Options{BinPath: "/usr/local/bin/grafel", LogDir: "/tmp/logs"}
	// GeneratePlist bakes os.UserHomeDir() as HOME; redirect HOME so it is
	// deterministic.
	t.Setenv("HOME", "/tmp/sandbox-home")
	plist, err := GeneratePlist(opts)
	if err != nil {
		t.Fatalf("GeneratePlist: %v", err)
	}
	if got := extractPlistHome(string(plist)); got != "/tmp/sandbox-home" {
		t.Errorf("extractPlistHome = %q, want %q", got, "/tmp/sandbox-home")
	}

	if got := extractPlistHome("<plist>no home here</plist>"); got != "" {
		t.Errorf("extractPlistHome(no HOME) = %q, want empty", got)
	}
}

// TestRegisteredRoot_NotInstalled returns found=false (no error) when no plist
// exists.
func TestRegisteredRoot_NotInstalled(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // a fresh home with no LaunchAgents plist
	root, found, err := registeredRoot()
	if err != nil {
		t.Fatalf("registeredRoot: %v", err)
	}
	if found {
		t.Errorf("found = true; want false for an uninstalled service (root=%q)", root)
	}
}

// TestRegisteredRoot_ReadsInstalledPlist writes a plist into a sandbox HOME and
// confirms the recorded root is read back.
func TestRegisteredRoot_ReadsInstalledPlist(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	plistDir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(plistDir, 0o755); err != nil {
		t.Fatal(err)
	}
	plist, err := GeneratePlist(Options{BinPath: "/bin/grafel", LogDir: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plistDir, launchLabel+".plist"), plist, 0o644); err != nil {
		t.Fatal(err)
	}

	root, found, err := registeredRoot()
	if err != nil {
		t.Fatalf("registeredRoot: %v", err)
	}
	if !found {
		t.Fatal("found = false; want true")
	}
	if root != home {
		t.Errorf("root = %q, want %q", root, home)
	}
}
