//go:build darwin

package install

import (
	"testing"

	"github.com/cajasmota/grafel/internal/install/watchers"
)

// stubLaunchctlRunner neutralises the launchctl command runner for the duration
// of a test.
//
// Every test in this package that reaches an activation path must call this, or
// swap newWatcherLoader, or both. The cost of forgetting is not a flake — it is
// a permanently loaded launchd job in the developer's own gui/<uid> session
// pointing at a deleted temp binary, which exits 78 and which launchd then
// reschedules forever under KeepAlive{SuccessfulExit:false}. Nothing in grafel
// can ever name it again: ReconcileWatcherUnits, Uninstall and Cleanup all
// derive labels from the registry, and a temp repo is in no registry.
//
// Under #6183's path-digest label that leak is UNBOUNDED. The pre-#6183 label
// was a constant for a given (group, basename), so repeated runs reused one job;
// the digest is over os.MkdirTemp's unique path, so every run mints a new label
// and leaks a new job. 23 were found accumulated on one machine.
// It delegates to watchers.StubServiceCallsForTest, the single cross-platform
// stub mechanism, rather than swapping launchctlRunner directly — see
// watchers/stub_6183_darwin_test.go for why there is only one.
func stubLaunchctlRunner(t *testing.T) {
	t.Helper()
	t.Cleanup(watchers.StubServiceCallsForTest())
}
