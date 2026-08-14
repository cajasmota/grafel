//go:build windows

package daemon

import (
	"syscall"
	"testing"
)

func TestDefaultEngineChildCommandWindowsFlags(t *testing.T) {
	cmd := defaultEngineChildCommand(`C:\grafel\grafel.exe`, `C:\grafel`)

	wantArgs := []string{`C:\grafel\grafel.exe`, "engine", "--foreground"}
	if len(cmd.Args) != len(wantArgs) {
		t.Fatalf("args = %v, want %v", cmd.Args, wantArgs)
	}
	for i := range wantArgs {
		if cmd.Args[i] != wantArgs[i] {
			t.Fatalf("args = %v, want %v", cmd.Args, wantArgs)
		}
	}

	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr is nil")
	}
	const createNoWindow = 0x08000000
	wantFlags := uint32(syscall.CREATE_NEW_PROCESS_GROUP | createNoWindow)
	if got := cmd.SysProcAttr.CreationFlags; got&wantFlags != wantFlags {
		t.Fatalf("CreationFlags = %#x, want both CREATE_NEW_PROCESS_GROUP and CREATE_NO_WINDOW (%#x)", got, wantFlags)
	}
}
