//go:build darwin

package service_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/daemon/service"
)

// renderPlist renders a LaunchAgent plist using test-fixture options
// and returns it as a string. It does NOT install anything.
func renderPlist(t *testing.T) string {
	t.Helper()
	data, err := service.GeneratePlist(resolvedOpts())
	if err != nil {
		t.Fatalf("GeneratePlist: %v", err)
	}
	return string(data)
}

// renderUnit is a no-op stub on macOS: systemd unit tests only run on
// Linux. Returning a non-empty string lets the shared test file
// compile, but the tests are skipped at runtime.
func renderUnit(t *testing.T) string {
	t.Skip("systemd unit tests only run on linux")
	return ""
}
