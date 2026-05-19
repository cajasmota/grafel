package service_test

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/daemon/service"
)

// resolvedOpts returns an Options struct with all fields filled in so
// that template rendering does not depend on os.Executable or the real
// user home directory.
func resolvedOpts() service.Options {
	return service.Options{
		BinPath:    "/usr/local/bin/archigraph",
		SocketPath: "/home/testuser/.archigraph/sockets/daemon.sock",
		LogDir:     "/home/testuser/.archigraph/logs",
	}
}

// --- macOS plist tests ---------------------------------------------------

// TestGeneratePlist_ContainsLabel verifies that the rendered plist
// contains the canonical launchd label.
func TestGeneratePlist_ContainsLabel(t *testing.T) {
	t.Helper()
	plist := renderPlist(t)
	if !strings.Contains(plist, "com.archigraph.daemon") {
		t.Errorf("plist missing label com.archigraph.daemon:\n%s", plist)
	}
}

// TestGeneratePlist_BinPath verifies that the resolved binary path
// appears in the ProgramArguments array.
func TestGeneratePlist_BinPath(t *testing.T) {
	plist := renderPlist(t)
	if !strings.Contains(plist, "/usr/local/bin/archigraph") {
		t.Errorf("plist missing BinPath:\n%s", plist)
	}
}

// TestGeneratePlist_DaemonSubcmd verifies the plist invokes
// `archigraph daemon` — the hidden long-running mode. launchd owns
// the process lifecycle, so no `start` sub-subcommand is needed.
func TestGeneratePlist_DaemonSubcmd(t *testing.T) {
	plist := renderPlist(t)
	if !strings.Contains(plist, "<string>daemon</string>") {
		t.Errorf("plist missing daemon subcommand:\n%s", plist)
	}
	// Plist must NOT contain a 'start' argument — the old
	// watcher_ctl start command forks the binary with its own
	// already-running check that would exit 0 under launchd.
	if strings.Contains(plist, "<string>start</string>") {
		t.Errorf("plist must not contain 'start' argument (use 'archigraph daemon' directly):\n%s", plist)
	}
}

// TestGeneratePlist_KeepAlive verifies the KeepAlive key is set to
// true so launchd restarts the daemon on crash.
func TestGeneratePlist_KeepAlive(t *testing.T) {
	plist := renderPlist(t)
	if !strings.Contains(plist, "<key>KeepAlive</key>") {
		t.Errorf("plist missing KeepAlive:\n%s", plist)
	}
	// The true element must follow KeepAlive — just check <true/> appears
	// after the key (simplistic but reliable for well-formed output).
	keepIdx := strings.Index(plist, "<key>KeepAlive</key>")
	trueIdx := strings.Index(plist[keepIdx:], "<true/>")
	if trueIdx < 0 {
		t.Errorf("plist KeepAlive not set to true:\n%s", plist)
	}
}

// TestGeneratePlist_RunAtLoad verifies RunAtLoad=true so the daemon
// starts automatically when the user logs in.
func TestGeneratePlist_RunAtLoad(t *testing.T) {
	plist := renderPlist(t)
	if !strings.Contains(plist, "<key>RunAtLoad</key>") {
		t.Errorf("plist missing RunAtLoad:\n%s", plist)
	}
	runIdx := strings.Index(plist, "<key>RunAtLoad</key>")
	trueIdx := strings.Index(plist[runIdx:], "<true/>")
	if trueIdx < 0 {
		t.Errorf("plist RunAtLoad not set to true:\n%s", plist)
	}
}

// TestGeneratePlist_LogPaths verifies that stdout and stderr are
// redirected to the configured log directory.
func TestGeneratePlist_LogPaths(t *testing.T) {
	plist := renderPlist(t)
	if !strings.Contains(plist, "/home/testuser/.archigraph/logs/daemon.log") {
		t.Errorf("plist missing stdout log path:\n%s", plist)
	}
	if !strings.Contains(plist, "/home/testuser/.archigraph/logs/daemon.err") {
		t.Errorf("plist missing stderr log path:\n%s", plist)
	}
}

// TestGeneratePlist_ValidXML does a minimal sanity check that the
// output looks like well-formed plist XML.
func TestGeneratePlist_ValidXML(t *testing.T) {
	plist := renderPlist(t)
	if !strings.HasPrefix(strings.TrimSpace(plist), "<?xml") {
		t.Errorf("plist does not start with XML declaration:\n%s", plist)
	}
	if !strings.Contains(plist, "</plist>") {
		t.Errorf("plist missing closing </plist> tag:\n%s", plist)
	}
}

// --- Linux systemd unit tests --------------------------------------------

// TestGenerateUnit_ExecStart verifies the ExecStart line invokes
// `archigraph daemon` — the hidden long-running mode. systemd owns
// the process lifecycle; no sub-subcommand is required.
func TestGenerateUnit_ExecStart(t *testing.T) {
	unit := renderUnit(t)
	if !strings.Contains(unit, "ExecStart=/usr/local/bin/archigraph daemon") {
		t.Errorf("unit missing ExecStart:\n%s", unit)
	}
}

// TestGenerateUnit_RestartPolicy verifies Restart=on-failure is set.
func TestGenerateUnit_RestartPolicy(t *testing.T) {
	unit := renderUnit(t)
	if !strings.Contains(unit, "Restart=on-failure") {
		t.Errorf("unit missing Restart=on-failure:\n%s", unit)
	}
}

// TestGenerateUnit_WantedBy verifies WantedBy=default.target so the
// service is enabled for normal user login sessions.
func TestGenerateUnit_WantedBy(t *testing.T) {
	unit := renderUnit(t)
	if !strings.Contains(unit, "WantedBy=default.target") {
		t.Errorf("unit missing WantedBy=default.target:\n%s", unit)
	}
}

// TestGenerateUnit_TypeSimple verifies Type=simple (the daemon does not
// fork; launchd / systemd owns the process lifecycle).
func TestGenerateUnit_TypeSimple(t *testing.T) {
	unit := renderUnit(t)
	if !strings.Contains(unit, "Type=simple") {
		t.Errorf("unit missing Type=simple:\n%s", unit)
	}
}

// TestGenerateUnit_Sections verifies the three required ini-style
// section headers are present.
func TestGenerateUnit_Sections(t *testing.T) {
	unit := renderUnit(t)
	for _, section := range []string{"[Unit]", "[Service]", "[Install]"} {
		if !strings.Contains(unit, section) {
			t.Errorf("unit missing section %s:\n%s", section, unit)
		}
	}
}
