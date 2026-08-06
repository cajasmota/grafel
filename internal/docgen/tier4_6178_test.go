// Regression test for issue #6178 (read-side, docgen twin):
// loadGroupCrossRepoLinks used os.UserHomeDir() directly, ignoring
// GRAFEL_HOME, while this same file's defaultTier4OutDir correctly
// resolves the grafel home via tier1HomeDir(). That split — GRAFEL_HOME
// honored in one function, hand-rolled in another, within the SAME file —
// is exactly what the issue warned a mechanical grep sweep would miss.
package docgen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadGroupCrossRepoLinks_HonorsGRAFELHomeEnv_6178(t *testing.T) {
	sandboxHome := t.TempDir()
	grafelHome := t.TempDir()
	t.Setenv("HOME", sandboxHome)
	t.Setenv("USERPROFILE", sandboxHome) // os.UserHomeDir() reads this on Windows
	t.Setenv("GRAFEL_HOME", grafelHome)

	const group = "g6178-docgen"

	// The correct location: under GRAFEL_HOME.
	correctDir := filepath.Join(grafelHome, "groups")
	if err := os.MkdirAll(correctDir, 0o755); err != nil {
		t.Fatal(err)
	}
	correctDoc, err := json.Marshal([]groupCrossRepoLink{{Source: "a::x", Target: "b::y", Kind: "http"}})
	if err != nil {
		t.Fatal(err)
	}
	correctPath := filepath.Join(correctDir, group+"-links.json")
	if err := os.WriteFile(correctPath, correctDoc, 0o644); err != nil {
		t.Fatal(err)
	}

	// A DECOY at the HOME-derived fallback location, with different
	// content, so a regression (reading via os.UserHomeDir() instead of
	// GRAFEL_HOME) is caught by content mismatch, not just presence.
	decoyDir := filepath.Join(sandboxHome, ".grafel", "groups")
	if err := os.MkdirAll(decoyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	decoyDoc, err := json.Marshal([]groupCrossRepoLink{{Source: "decoy::1", Target: "decoy::2", Kind: "decoy"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(decoyDir, group+"-links.json"), decoyDoc, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := loadGroupCrossRepoLinks(group)
	if err != nil {
		t.Fatalf("loadGroupCrossRepoLinks: %v", err)
	}
	if len(got) != 1 || got[0].Source != "a::x" || got[0].Target != "b::y" {
		t.Fatalf("regression: got %+v, want the GRAFEL_HOME links, not the HOME-derived decoy", got)
	}
}
