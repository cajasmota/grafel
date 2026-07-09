//go:build linux

package service_test

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon/service"
)

// TestGenerateUnit_LimitNOFILE verifies the generated systemd unit sets an fd
// limit in the [Service] section (#5675). This is the MOST important
// service-unit change because the primary deployment target is a Linux VPS
// under systemd; without it a worktree indexing storm can exhaust fds and
// crash-loop under Restart=on-failure. Runs without a live systemd session.
func TestGenerateUnit_LimitNOFILE(t *testing.T) {
	unitBytes, err := service.GenerateUnit(service.Options{BinPath: "/usr/local/bin/grafel"})
	if err != nil {
		t.Fatalf("GenerateUnit: %v", err)
	}
	unit := string(unitBytes)
	if !strings.Contains(unit, "LimitNOFILE=65536") {
		t.Errorf("systemd unit missing LimitNOFILE=65536:\n%s", unit)
	}
	// The directive must live in the [Service] section, not [Unit]/[Install].
	svcIdx := strings.Index(unit, "[Service]")
	instIdx := strings.Index(unit, "[Install]")
	limIdx := strings.Index(unit, "LimitNOFILE=")
	if svcIdx < 0 || limIdx < svcIdx || (instIdx >= 0 && limIdx > instIdx) {
		t.Errorf("LimitNOFILE not inside [Service] section:\n%s", unit)
	}
}
