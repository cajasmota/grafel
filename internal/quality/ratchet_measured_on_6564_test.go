package quality

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// The property under test — `measured_on` names a commit the default branch can
// reach, permanently — is a property of the WRITER, and the writer is Python.
// Asserting on the committed baseline.json cannot catch it: by the time the
// file is on main the orphaning squash has already happened, which is exactly
// how it was found all three times.
//
// scripts/quality/test_ratchet.py drives ratchet.py against a synthetic
// repository shaped like a re-record (a feature branch moved past the default
// branch), which is not something a Go test of the checked-in JSON can build.
// This wrapper exists so those tests run in CI with the gate they protect,
// rather than only when someone remembers to invoke python by hand — a
// remembered manual step being the failure mode this whole change removes.
func TestRatchetMeasuredOnSelfTests_6564(t *testing.T) {
	requirePython3(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH; ratchet self-tests need a repository")
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "quality", "test_ratchet.py"))
	if err != nil {
		t.Fatalf("resolve test_ratchet.py: %v", err)
	}
	out, err := exec.Command("python3", script).CombinedOutput()
	if err != nil {
		t.Fatalf("scripts/quality/test_ratchet.py failed: %v\n%s", err, out)
	}
}
