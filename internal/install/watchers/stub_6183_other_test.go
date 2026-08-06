//go:build !darwin

package watchers

import "testing"

// stubLaunchctl is a no-op off macOS; there is no launchctl to stub.
func stubLaunchctl(t *testing.T) { t.Helper() }
