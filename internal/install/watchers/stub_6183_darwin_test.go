package watchers

import "testing"

// stubLaunchctl replaces the launchctl runner for the duration of a test. No
// test in this package may mutate the developer's real launchd session.
func stubLaunchctl(t *testing.T) {
	t.Helper()
	restore := SetLaunchctlRunnerForTest(func(args ...string) ([]byte, error) {
		return nil, nil
	})
	t.Cleanup(restore)
}
