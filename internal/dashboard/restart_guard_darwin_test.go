//go:build darwin

package dashboard

// restart_guard_darwin_test.go pins that restartViaBinary cannot force-restart
// the developer's live daemon from a test.
//
// `launchctl kickstart -k gui/<uid>/com.grafel.daemon` restarts the label
// serving the user's MCP session. No test in this package reaches it today —
// a PATH-shim run observes zero calls — but that is a statement about the
// current suite, not a property of the code, and it is the reasoning that let
// install.go drive real launchctl calls for as long as it did.

import (
	"testing"
)

// TestRestartViaBinaryIsGuarded calls the REAL function and requires the guard
// to stop it before it shells out.
func TestRestartViaBinaryIsGuarded(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("restartViaBinary is NOT guarded — a test can kickstart the user's live daemon")
		}
	}()
	restartViaBinary()
}
