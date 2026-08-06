//go:build !darwin

package install

import (
	"testing"

	"github.com/cajasmota/grafel/internal/install/watchers"
)

// stubLaunchctlRunner neutralises service-manager calls off macOS too: the
// Linux loader shells out to `systemctl --user disable --now` and the Windows
// one to `schtasks /delete`, both of which mutate the developer's real session.
// "There is no launchctl to stub" was true of the command and false of the
// property.
func stubLaunchctlRunner(t *testing.T) {
	t.Helper()
	t.Cleanup(watchers.StubServiceCallsForTest())
}
