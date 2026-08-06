// Regression test for issue #6178 (read-side, dashboard twin):
// defaultLinksFile mirrored mcp.defaultLinksFile's bug — os.UserHomeDir()
// directly, ignoring GRAFEL_HOME.
package dashboard

import (
	"path/filepath"
	"testing"
)

func TestDefaultLinksFile_HonorsGRAFELHomeEnv_6178(t *testing.T) {
	sandboxHome := t.TempDir()
	grafelHome := t.TempDir()
	t.Setenv("HOME", sandboxHome)
	t.Setenv("USERPROFILE", sandboxHome) // os.UserHomeDir() reads this on Windows
	t.Setenv("GRAFEL_HOME", grafelHome)

	got := defaultLinksFile("g6178")
	want := filepath.Join(grafelHome, "groups", "g6178-links.json")
	if got != want {
		t.Fatalf("defaultLinksFile = %q, want %q", got, want)
	}
	fallback := filepath.Join(sandboxHome, ".grafel", "groups", "g6178-links.json")
	if got == fallback {
		t.Fatalf("regression: resolved via HOME (%s) instead of GRAFEL_HOME", sandboxHome)
	}
}
