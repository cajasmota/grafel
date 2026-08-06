//go:build !darwin

package install

import "testing"

// stubLaunchctlRunner is a no-op off macOS; there is no launchctl to stub.
func stubLaunchctlRunner(t *testing.T) { t.Helper() }
