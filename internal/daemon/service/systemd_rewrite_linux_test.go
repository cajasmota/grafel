//go:build linux

package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// legacyDaemonUnit is a snapshot of the pre-PR5 rendered systemd unit
// (execs `grafel daemon`, the back-compat shim) — what an existing install's
// on-disk unit looks like before the next `grafel update` / `grafel install`
// re-renders it.
const legacyDaemonUnit = `[Unit]
Description=grafel knowledge-graph daemon
Documentation=https://github.com/cajasmota/grafel
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/grafel daemon
Restart=on-failure
RestartSec=3s
Environment=HOME=/home/testuser
LimitNOFILE=65536

[Install]
WantedBy=default.target
`

// TestWriteUnit_RewritesLegacyDaemonUnitToServe verifies the idempotent
// re-render contract PR5 (ADR-0024, epic #5729) relies on: an existing
// install whose unit file still literally execs `daemon` gets rewritten to
// `serve` the next time service.Install (WriteUnit) runs — no separate
// migration code, it falls out of GenerateUnit always rendering the current
// template.
func TestWriteUnit_RewritesLegacyDaemonUnitToServe(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	unitDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(unitDir, unitName+".service")
	if err := os.WriteFile(path, []byte(legacyDaemonUnit), 0o644); err != nil {
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
		t.Fatalf("read rewritten unit: %v", err)
	}
	if !strings.Contains(string(rewritten), "ExecStart=/usr/local/bin/grafel serve") {
		t.Errorf("rewritten unit missing 'serve' ExecStart:\n%s", rewritten)
	}
	if strings.Contains(string(rewritten), "ExecStart=/usr/local/bin/grafel daemon") {
		t.Errorf("rewritten unit still contains legacy 'daemon' ExecStart:\n%s", rewritten)
	}

	// Idempotency: a second WriteUnit pass must produce byte-identical output.
	if err := sm.WriteUnit(); err != nil {
		t.Fatalf("WriteUnit (second pass): %v", err)
	}
	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read twice-written unit: %v", err)
	}
	if string(again) != string(rewritten) {
		t.Errorf("WriteUnit is not idempotent:\nfirst:\n%s\nsecond:\n%s", rewritten, again)
	}
}
