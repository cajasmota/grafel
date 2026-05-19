//go:build linux

package service_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/daemon/service"
)

// renderUnit renders a systemd user unit using test-fixture options
// and returns it as a string. It does NOT install anything.
func renderUnit(t *testing.T) string {
	t.Helper()
	data, err := service.GenerateUnit(resolvedOpts())
	if err != nil {
		t.Fatalf("GenerateUnit: %v", err)
	}
	return string(data)
}

// renderPlist is a no-op stub on Linux: launchd plist tests only run
// on macOS. Returning a non-empty string lets the shared test file
// compile, but the tests are skipped at runtime.
func renderPlist(t *testing.T) string {
	t.Skip("launchd plist tests only run on darwin")
	return ""
}
