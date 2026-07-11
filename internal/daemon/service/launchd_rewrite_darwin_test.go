//go:build darwin

package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// legacyDaemonPlist is a snapshot of the pre-PR5 rendered plist (invokes
// `grafel daemon`, the back-compat shim) — what an existing install's
// on-disk LaunchAgent plist looks like before the next `grafel update` /
// `grafel install` re-renders it.
const legacyDaemonPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
    "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.grafel.daemon</string>

    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/grafel</string>
        <string>daemon</string>
    </array>

    <key>RunAtLoad</key>
    <true/>

    <key>KeepAlive</key>
    <true/>
</dict>
</plist>
`

// TestWriteUnit_RewritesLegacyDaemonUnitToServe verifies the idempotent
// re-render contract PR5 (ADR-0024, epic #5729) relies on: an existing
// install whose plist still literally execs `daemon` gets rewritten to
// `serve` the next time service.Install (WriteUnit) runs — no separate
// migration code, it falls out of GeneratePlist always rendering the
// current template.
func TestWriteUnit_RewritesLegacyDaemonUnitToServe(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	plistDir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(plistDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(plistDir, launchLabel+".plist")
	if err := os.WriteFile(path, []byte(legacyDaemonPlist), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := Options{
		BinPath:    "/usr/local/bin/grafel",
		SocketPath: filepath.Join(home, ".grafel", "sockets", "daemon.sock"),
		LogDir:     filepath.Join(home, ".grafel", "logs"),
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
		t.Fatalf("read rewritten plist: %v", err)
	}
	if !strings.Contains(string(rewritten), "<string>serve</string>") {
		t.Errorf("rewritten plist missing 'serve' argument:\n%s", rewritten)
	}
	if strings.Contains(string(rewritten), "<string>daemon</string>") {
		t.Errorf("rewritten plist still contains legacy 'daemon' argument:\n%s", rewritten)
	}

	// Idempotency: a second WriteUnit pass must produce byte-identical output.
	if err := sm.WriteUnit(); err != nil {
		t.Fatalf("WriteUnit (second pass): %v", err)
	}
	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read twice-written plist: %v", err)
	}
	if string(again) != string(rewritten) {
		t.Errorf("WriteUnit is not idempotent:\nfirst:\n%s\nsecond:\n%s", rewritten, again)
	}
}
