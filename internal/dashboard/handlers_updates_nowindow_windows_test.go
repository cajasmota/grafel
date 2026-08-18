//go:build windows

package dashboard

import (
	"context"
	"testing"
)

// TestNewUpdateCmd_SetsCreateNoWindow is the real behavioural assertion for
// #6325 F1 — it can only run on Windows, because executil.NoWindow is a no-op
// on every other GOOS (internal/executil/nowindow_other.go). The portable
// half of this guard lives in handlers_updates_nowindow_6325_test.go.
func TestNewUpdateCmd_SetsCreateNoWindow(t *testing.T) {
	cmd, err := newUpdateCmd(context.Background(), []string{"update"})
	if err != nil {
		t.Fatalf("newUpdateCmd: %v", err)
	}
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr is nil; CREATE_NO_WINDOW was never applied (#6325 F1)")
	}
	const createNoWindow = 0x08000000
	if got := cmd.SysProcAttr.CreationFlags; got&createNoWindow != createNoWindow {
		t.Fatalf("CreationFlags = %#x, want CREATE_NO_WINDOW (%#x) set — the daemon-spawned "+
			"`grafel update` child would pop a console window (#6325 F1)", got, createNoWindow)
	}
}
