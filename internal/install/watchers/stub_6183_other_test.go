//go:build !darwin

package watchers

import "testing"

// stubLaunchctl neutralises service-manager calls off macOS too.
//
// It used to be a no-op on the reasoning that "there is no launchctl to stub".
// That was true of launchctl and false of the property: on Linux the loader
// shells out to `systemctl --user disable --now`, and on Windows to
// `schtasks /delete`, both of which mutate the developer's real session.
// StubServiceCallsForTest covers all three.
func stubLaunchctl(t *testing.T) {
	t.Helper()
	t.Cleanup(StubServiceCallsForTest())
}
