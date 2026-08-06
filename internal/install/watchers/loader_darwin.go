//go:build darwin

package watchers

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// darwinLoader implements Loader using launchctl for macOS LaunchAgents.
type darwinLoader struct{}

// NewLoader returns the macOS launchctl-based Loader.
func NewLoader() Loader { return darwinLoader{} }

// launchctlRunner runs `launchctl <args...>` and returns its combined output
// and error. It is a package var so tests can inject a fake launchctl (e.g. to
// simulate the flaky err-5 bootstrap) without shelling out.
var launchctlRunner = func(args ...string) ([]byte, error) {
	return exec.Command("launchctl", args...).CombinedOutput()
}

// SetLaunchctlRunnerForTest swaps the launchctl command runner and returns a
// restore func. It exists so cross-package tests (e.g. install.Apply's
// watcher-activation path) can simulate launchctl failures without shelling
// out. Test-only; do not use in production code.
func SetLaunchctlRunnerForTest(fn func(args ...string) ([]byte, error)) (restore func()) {
	orig := launchctlRunner
	launchctlRunner = fn
	return func() { launchctlRunner = orig }
}

// bootstrapRetries is the number of bootout→bootstrap attempts made when
// launchctl bootstrap returns the flaky err 5 (EIO / "Input/output error").
// launchd intermittently fails the very first bootstrap of a freshly written
// plist with exit 5; a bounded retry (with a small backoff) clears it.
const bootstrapRetries = 3

// bootstrapBackoff is the pause between err-5 bootstrap retries.
var bootstrapBackoff = 200 * time.Millisecond

// isLaunchctlErr5 reports whether err is a launchctl exit-code-5 failure.
// launchctl returns exit 5 for the transient "Bootstrap failed: 5:
// Input/output error" condition; it is locale-invariant (exit code, not text).
func isLaunchctlErr5(err error) bool {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode() == 5
	}
	return false
}

// Load writes the plist (via Write) and bootstraps it into the current user's
// launchd domain. If the unit is already running it is a no-op.
//
// launchd intermittently fails the first bootstrap of a freshly written plist
// with the flaky exit code 5 ("Bootstrap failed: 5: Input/output error"). This
// is not a real configuration error — a bootout→bootstrap retry clears it. We
// therefore retry the bootout+bootstrap pair a bounded number of times,
// specifically on err 5, with a small backoff between attempts.
func (darwinLoader) Load(u Unit) error {
	path, err := UnitPath(u)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("unit file not found — call Write(u) first: %s", path)
	}

	uid := strconv.Itoa(os.Getuid())
	label := u.Label()

	// Clear any persisted disable override before bootstrapping. Unload writes
	// one (see below) so that `grafel stop` survives logout/reboot; without a
	// matching enable here, RunAtLoad silently does nothing on a disabled job
	// and `grafel start` would report success over a watcher that never runs.
	// Best-effort, exactly like the daemon service's own enable/disable pairing
	// in internal/daemon/service/launchd_darwin.go: bootstrap below is the real
	// convergence signal.
	_, _ = launchctlRunner("enable", "gui/"+uid+"/"+label)

	var lastOut []byte
	var lastErr error
	for attempt := 1; attempt <= bootstrapRetries; attempt++ {
		// Bootout any stale entry so bootstrap succeeds cleanly.
		_, _ = launchctlRunner("bootout", "gui/"+uid+"/"+label)

		out, berr := launchctlRunner("bootstrap", "gui/"+uid, path)
		if berr == nil {
			return nil
		}
		lastOut, lastErr = out, berr

		// Only the flaky err-5 is worth retrying; any other failure is real.
		if !isLaunchctlErr5(berr) {
			break
		}
		if attempt < bootstrapRetries {
			time.Sleep(bootstrapBackoff)
		}
	}
	return fmt.Errorf("launchctl bootstrap %s: %w\n%s", label, lastErr, lastOut)
}

// Unload bootouts the LaunchAgent for the given unit. Errors are suppressed
// when the unit was never loaded.
//
// "Already gone" is detected via the exit code of `launchctl list <label>`
// (locale-invariant) rather than by matching the localized bootout error text
// ("No such process" etc.), which breaks on non-English macOS. If the service
// is not listed there is nothing to bootout — the desired absent state is
// already reached, so we report success without shelling out to bootout.
// The stop is PERSISTENT: bootout alone only clears the CURRENT login
// session, because the plist stays in ~/Library/LaunchAgents and RunAtLoad
// fires again at next login — so a bare bootout is "stop until you log in
// again", not a stop. `launchctl disable` writes an override that suppresses
// that automatic relaunch across login/reboot. This is the same pairing the
// grafel daemon's own service already uses (#6044,
// internal/daemon/service/launchd_darwin.go Unload) and it puts macOS on equal
// footing with systemd's `disable --now` (loader_linux.go) and the schtasks
// delete (loader_windows.go), both of which already persisted. Load() above
// re-enables, so the ordinary install/reconcile Unload;Load cycle is unaffected
// — the disable only matters for a caller that stops WITHOUT a following Load,
// i.e. `grafel stop`.
//
// Every launchctl invocation goes through launchctlRunner rather than a bare
// exec.Command so this whole path is testable without booting out a real
// watcher on the developer's own machine. Before this, only Load was seamed:
// deleting the bootout outright left the suite green.
func (darwinLoader) Unload(u Unit) error {
	uid := strconv.Itoa(os.Getuid())
	label := u.Label()

	// Persist the stop first, so it holds even if bootout races or the process
	// is already gone. disable is idempotent and independent of load state.
	_, _ = launchctlRunner("disable", "gui/"+uid+"/"+label)

	// launchctl list <label> exits non-zero when the label is not loaded; that
	// is the locale-invariant signal that the desired absent state already holds.
	if _, err := launchctlRunner("list", label); err != nil {
		return nil // not loaded — already gone
	}
	if out, err := launchctlRunner("bootout", "gui/"+uid+"/"+label); err != nil {
		// Race: the service was listed above but disappeared before bootout.
		// Re-check via the exit code of `launchctl list`; if it is now gone,
		// the desired state is reached. Never match the localized error text.
		if _, lerr := launchctlRunner("list", label); lerr != nil {
			return nil // gone now — success-to-proceed
		}
		return fmt.Errorf("launchctl bootout %s: %w\n%s", label, err, out)
	}
	return nil
}

// Status queries launchctl list for the watcher label.
func (darwinLoader) Status(u Unit) (WatcherStatus, error) {
	path, err := UnitPath(u)
	if err != nil {
		return WatcherStatus{TaskName: u.Label()}, err
	}

	ws := WatcherStatus{TaskName: u.Label()}

	if _, serr := os.Stat(path); !os.IsNotExist(serr) {
		ws.Installed = true
	}

	// launchctl list <label> prints: <pid | -> <exit> <label>
	out, err := exec.Command("launchctl", "list", u.Label()).Output()
	if err != nil {
		return ws, nil // not loaded — not running
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) >= 1 && fields[0] != "-" {
		ws.Running = true
		if pid, perr := strconv.Atoi(fields[0]); perr == nil {
			ws.PID = pid
		}
	}
	return ws, nil
}
