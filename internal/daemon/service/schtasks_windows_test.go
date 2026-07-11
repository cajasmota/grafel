//go:build windows

package service_test

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon/service"
)

// windowsOpts returns Options with all fields explicit so that
// GenerateTaskXML does not depend on os.Executable or the real
// user home directory.
func windowsOpts() service.Options {
	return service.Options{
		BinPath:    `C:\Program Files\grafel\grafel.exe`,
		SocketPath: `\\.\pipe\grafel-daemon-testuser`,
		LogDir:     `C:\Users\testuser\AppData\Local\grafel\logs`,
	}
}

// TestGenerateTaskXML_XMLDeclaration verifies the output begins with a valid
// XML declaration (required by Task Scheduler XML format).
func TestGenerateTaskXML_XMLDeclaration(t *testing.T) {
	xml, err := service.GenerateTaskXML(windowsOpts())
	if err != nil {
		t.Fatalf("GenerateTaskXML: %v", err)
	}
	s := strings.TrimSpace(string(xml))
	if !strings.HasPrefix(s, "<?xml") {
		t.Errorf("task XML does not start with XML declaration:\n%s", s)
	}
}

// TestGenerateTaskXML_TaskNamespace verifies the Task element uses the
// canonical Windows Task Scheduler namespace.
func TestGenerateTaskXML_TaskNamespace(t *testing.T) {
	xml, err := service.GenerateTaskXML(windowsOpts())
	if err != nil {
		t.Fatalf("GenerateTaskXML: %v", err)
	}
	s := string(xml)
	if !strings.Contains(s, "schemas.microsoft.com/windows/2004/02/mit/task") {
		t.Errorf("task XML missing Windows Task Scheduler namespace:\n%s", s)
	}
}

// TestGenerateTaskXML_LogonTrigger verifies the task uses a LogonTrigger so it
// starts at user login (equivalent to launchd RunAtLoad / systemd WantedBy).
func TestGenerateTaskXML_LogonTrigger(t *testing.T) {
	xml, err := service.GenerateTaskXML(windowsOpts())
	if err != nil {
		t.Fatalf("GenerateTaskXML: %v", err)
	}
	s := string(xml)
	if !strings.Contains(s, "<LogonTrigger>") {
		t.Errorf("task XML missing LogonTrigger:\n%s", s)
	}
}

// TestGenerateTaskXML_RestartOnFailure verifies RestartOnFailure is set so the
// daemon restarts after crashes (equivalent to launchd KeepAlive / systemd
// Restart=on-failure).
func TestGenerateTaskXML_RestartOnFailure(t *testing.T) {
	xml, err := service.GenerateTaskXML(windowsOpts())
	if err != nil {
		t.Fatalf("GenerateTaskXML: %v", err)
	}
	s := string(xml)
	if !strings.Contains(s, "<RestartOnFailure>") {
		t.Errorf("task XML missing RestartOnFailure:\n%s", s)
	}
}

// TestGenerateTaskXML_BinPath verifies the binary path appears in the
// Command element.
func TestGenerateTaskXML_BinPath(t *testing.T) {
	xml, err := service.GenerateTaskXML(windowsOpts())
	if err != nil {
		t.Fatalf("GenerateTaskXML: %v", err)
	}
	s := string(xml)
	wantBin := `C:\Program Files\grafel\grafel.exe`
	if !strings.Contains(s, wantBin) {
		t.Errorf("task XML missing BinPath %q:\n%s", wantBin, s)
	}
}

// TestGenerateTaskXML_ServeArgument verifies the task passes "serve" as
// the argument to the binary — not "daemon", "start", or "run". PR5
// (ADR-0024, epic #5729) retargets the OS unit from the back-compat `daemon`
// shim to `serve` directly. Task Scheduler owns the process lifecycle so we
// invoke grafel serve directly.
func TestGenerateTaskXML_ServeArgument(t *testing.T) {
	xml, err := service.GenerateTaskXML(windowsOpts())
	if err != nil {
		t.Fatalf("GenerateTaskXML: %v", err)
	}
	s := string(xml)
	if !strings.Contains(s, "<Arguments>serve</Arguments>") {
		t.Errorf("task XML missing <Arguments>serve</Arguments>:\n%s", s)
	}
	// Must NOT contain "daemon" or "start" as an argument — "daemon" is the
	// legacy back-compat shim entrypoint, and "start" would fork a separate
	// process and shadow the scheduler-managed instance.
	if strings.Contains(s, "<Arguments>daemon</Arguments>") {
		t.Errorf("task XML must not use 'daemon' argument (retargeted to 'serve'):\n%s", s)
	}
	if strings.Contains(s, "<Arguments>start</Arguments>") {
		t.Errorf("task XML must not use 'start' argument:\n%s", s)
	}
}

// TestGenerateTaskXML_LeastPrivilege verifies the task runs at LeastPrivilege
// (no UAC elevation required — user-level service, mirroring macOS/Linux).
func TestGenerateTaskXML_LeastPrivilege(t *testing.T) {
	xml, err := service.GenerateTaskXML(windowsOpts())
	if err != nil {
		t.Fatalf("GenerateTaskXML: %v", err)
	}
	s := string(xml)
	if !strings.Contains(s, "<RunLevel>LeastPrivilege</RunLevel>") {
		t.Errorf("task XML missing LeastPrivilege run level:\n%s", s)
	}
}

// TestGenerateTaskXML_TaskName verifies the task is registered under the
// canonical reverse-DNS name com.grafel.daemon.
func TestGenerateTaskXML_TaskName(t *testing.T) {
	xml, err := service.GenerateTaskXML(windowsOpts())
	if err != nil {
		t.Fatalf("GenerateTaskXML: %v", err)
	}
	s := string(xml)
	if !strings.Contains(s, "com.grafel.daemon") {
		t.Errorf("task XML missing task name com.grafel.daemon:\n%s", s)
	}
}
