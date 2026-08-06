package watchers

import "testing"

// stubLaunchctl neutralises service-manager calls for the duration of a test.
// No test in this package may mutate the developer's real launchd session.
//
// It delegates to StubServiceCallsForTest rather than swapping launchctlRunner
// directly (#6183 landed the latter; the fleet-stop work landed the former).
// Two mechanisms for one property is how one of them ends up not covering a
// path: StubServiceCallsForTest is the cross-platform one and it short-circuits
// systemctl and schtasks too, so it is the single mechanism both now use. The
// name is kept so #6183's call sites read unchanged.
func stubLaunchctl(t *testing.T) {
	t.Helper()
	t.Cleanup(StubServiceCallsForTest())
}
