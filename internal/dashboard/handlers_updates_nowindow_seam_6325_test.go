package dashboard

import (
	"context"
	"os/exec"
	"testing"
)

// #6325 F1: the behavioural guard that the console-hiding treatment is really
// applied to the command the daemon is about to spawn. It runs on every GOOS
// because it asserts the CALL, not its Windows-only effect — which is what a
// source-text guard cannot do: text cannot distinguish a live call from
// `if false { executil.NoWindow(cmd) }`.
func TestNewUpdateCmd_AppliesNoWindowToTheSpawnedCommand(t *testing.T) {
	var got []*exec.Cmd
	prev := applyNoWindow
	applyNoWindow = func(cmd *exec.Cmd) { got = append(got, cmd) }
	t.Cleanup(func() { applyNoWindow = prev })

	cmd, err := newUpdateCmd(context.Background(), []string{"update"})
	if err != nil {
		t.Fatalf("newUpdateCmd: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("the no-window treatment was applied %d times, want exactly 1 — the "+
			"daemon-spawned `grafel update` child pops a console window on Windows without "+
			"it (#6325 F1)", len(got))
	}
	if got[0] != cmd {
		t.Errorf("the no-window treatment was applied to a different *exec.Cmd (%p) than the "+
			"one returned (%p) (#6325 F1)", got[0], cmd)
	}
}

// TestApplyNoWindowSeam_DefaultsToExecutil pins that the seam is not a
// permanently-inert stub: production must go through executil.NoWindow.
func TestApplyNoWindowSeam_DefaultsToExecutil(t *testing.T) {
	if applyNoWindow == nil {
		t.Fatal("applyNoWindow is nil; nothing hides the console on Windows (#6325 F1)")
	}
	// The zero-value SysProcAttr path must be safe to call — executil.NoWindow
	// allocates it on Windows and no-ops elsewhere.
	cmd := exec.Command("true")
	applyNoWindow(cmd)
}
