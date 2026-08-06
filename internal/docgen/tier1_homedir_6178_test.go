// Regression test for issue #6178 round 3: tier1HomeDir used to reimplement
// registry.HomeDir()'s GRAFEL_HOME/os.UserHomeDir() logic in-file rather
// than calling it. The duplicate happened to be correct at the time it was
// written, but a duplicate resolver is exactly the shape that let #6178
// round 2 happen (loadGroupCrossRepoLinks, in this same package's tier4.go,
// hand-rolled the home join instead of calling this very function). Now
// tier1HomeDir just delegates.
package docgen

import (
	"testing"
)

func TestTier1HomeDir_HonorsGRAFELHomeEnv_6178(t *testing.T) {
	sandboxHome := t.TempDir()
	grafelHome := t.TempDir()
	t.Setenv("HOME", sandboxHome)
	t.Setenv("USERPROFILE", sandboxHome)
	t.Setenv("GRAFEL_HOME", grafelHome)

	got, err := tier1HomeDir()
	if err != nil {
		t.Fatalf("tier1HomeDir: %v", err)
	}
	if got != grafelHome {
		t.Fatalf("tier1HomeDir() = %q, want %q (GRAFEL_HOME)", got, grafelHome)
	}
}
