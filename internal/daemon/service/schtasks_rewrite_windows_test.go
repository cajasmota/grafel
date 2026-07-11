//go:build windows

package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// legacyDaemonTaskXML is a snapshot of the pre-PR5 rendered Task Scheduler XML
// (execs `grafel daemon`, the back-compat shim) — what an existing install's
// on-disk task XML looks like before the next `grafel update` / `grafel
// install` re-renders it.
const legacyDaemonTaskXML = `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>grafel knowledge-graph daemon — managed by grafel install/uninstall</Description>
    <URI>\com.grafel.daemon</URI>
  </RegistrationInfo>
  <Actions>
    <Exec>
      <Command>C:\Program Files\grafel\grafel.exe</Command>
      <Arguments>daemon</Arguments>
    </Exec>
  </Actions>
</Task>
`

// TestWriteUnit_RewritesLegacyDaemonUnitToServe verifies the idempotent
// re-render contract PR5 (ADR-0024, epic #5729) relies on: an existing
// install whose task XML still literally execs `daemon` gets rewritten to
// `serve` the next time service.Install (WriteUnit) runs — no separate
// migration code, it falls out of GenerateTaskXML always rendering the
// current template.
func TestWriteUnit_RewritesLegacyDaemonUnitToServe(t *testing.T) {
	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)

	taskDir := filepath.Join(localAppData, "grafel", "tasks")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(taskDir, taskName+".xml")
	if err := os.WriteFile(path, []byte(legacyDaemonTaskXML), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := Options{
		BinPath:    `C:\Program Files\grafel\grafel.exe`,
		SocketPath: `\\.\pipe\grafel-daemon-testuser`,
		LogDir:     filepath.Join(localAppData, "grafel", "logs"),
	}
	sm, err := newServiceManager(opts)
	if err != nil {
		t.Fatalf("newServiceManager: %v", err)
	}

	if err := sm.WriteUnit(); err != nil {
		t.Fatalf("WriteUnit (rewrite pass): %v", err)
	}

	rewritten, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read rewritten task XML: %v", err)
	}
	if !strings.Contains(string(rewritten), "<Arguments>serve</Arguments>") {
		t.Errorf("rewritten task XML missing 'serve' argument:\n%s", rewritten)
	}
	if strings.Contains(string(rewritten), "<Arguments>daemon</Arguments>") {
		t.Errorf("rewritten task XML still contains legacy 'daemon' argument:\n%s", rewritten)
	}

	// Idempotency: a second WriteUnit pass must produce byte-identical output.
	if err := sm.WriteUnit(); err != nil {
		t.Fatalf("WriteUnit (second pass): %v", err)
	}
	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read twice-written task XML: %v", err)
	}
	if string(again) != string(rewritten) {
		t.Errorf("WriteUnit is not idempotent:\nfirst:\n%s\nsecond:\n%s", rewritten, again)
	}
}
