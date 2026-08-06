//go:build linux

package watchers

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// systemctlRunner runs `systemctl <args...>` bounded by both a deadline and a
// WaitDelay, and returns its combined output.
//
// It is a package var for the same two reasons the darwin loader has one: tests
// must be able to observe the call without mutating the developer's real
// systemd user session, and the guard in test_isolation_guard.go must be able
// to catch any that slip through unseamed.
//
// The bound matters as much here as on macOS: `systemctl --user disable --now`
// blocks on the unit actually stopping, and `grafel watch` drains on SIGTERM,
// so a fleet-wide stop across 140 units could otherwise hang indefinitely.
// WaitDelay is the load-bearing half — CommandContext's kill reaches only the
// direct child while CombinedOutput waits on every pipe writer.
var systemctlRunner = func(args ...string) ([]byte, error) {
	if serviceCallsAreStubbed() {
		return nil, nil
	}
	guardServiceCall("systemctl", args)
	ctx, cancel := context.WithTimeout(context.Background(), systemctlTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "systemctl", args...)
	cmd.WaitDelay = systemctlWaitDelay
	return cmd.CombinedOutput()
}

var (
	systemctlTimeout   = 15 * time.Second
	systemctlWaitDelay = 3 * time.Second
)

// linuxLoader implements Loader using systemctl --user for Linux systemd units.
type linuxLoader struct{}

// NewLoader returns the Linux systemd-user-based Loader.
func NewLoader() Loader { return linuxLoader{} }

// Load enables and immediately starts the systemd user unit. The unit file
// must already exist on disk (placed by Write). If the unit is already
// running it is a no-op.
func (linuxLoader) Load(u Unit) error {
	path, err := UnitPath(u)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("unit file not found — call Write(u) first: %s", path)
	}

	// Reload the unit manager so it picks up the new file.
	if out, err := systemctlRunner("--user", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl --user daemon-reload: %w\n%s", err, out)
	}

	// Enable + start atomically.
	out, err := systemctlRunner("--user", "enable", "--now", u.Label()+".service")
	if err != nil {
		return fmt.Errorf("systemctl --user enable --now %s: %w\n%s", u.Label(), err, out)
	}
	return nil
}

// Unload disables and stops the systemd user unit. Idempotent — if the unit
// is already disabled/stopped the call succeeds.
//
// "Already gone" is detected by stat-ing the unit file on disk (locale- and
// version-invariant) rather than by matching the localized `systemctl disable`
// error text ("No such file" etc.), which breaks on non-English locales. If the
// unit file is absent there is nothing to disable.
func (linuxLoader) Unload(u Unit) error {
	path, err := UnitPath(u)
	if err != nil {
		return err
	}
	if _, serr := os.Stat(path); os.IsNotExist(serr) {
		return nil // no unit file — already gone
	}
	// --now stops the unit in addition to disabling it.
	out, derr := systemctlRunner("--user", "disable", "--now", u.Label()+".service")
	if derr != nil {
		// Race: the unit file vanished between the stat above and disable.
		// Re-stat; if it is gone now, the desired absent state is reached.
		// Never match the localized error text.
		if _, serr := os.Stat(path); os.IsNotExist(serr) {
			return nil // gone now — success-to-proceed
		}
		return fmt.Errorf("systemctl --user disable %s: %w\n%s", u.Label(), derr, out)
	}
	return nil
}

// Status queries systemctl for the watcher unit state.
func (linuxLoader) Status(u Unit) (WatcherStatus, error) {
	path, err := UnitPath(u)
	if err != nil {
		return WatcherStatus{TaskName: u.Label()}, err
	}

	ws := WatcherStatus{TaskName: u.Label()}

	if _, serr := os.Stat(path); !os.IsNotExist(serr) {
		ws.Installed = true
	}

	// is-active exits 0 when the unit is active.
	if _, err := systemctlRunner("--user", "is-active", "--quiet", u.Label()+".service"); err == nil {
		ws.Running = true
	}
	return ws, nil
}
