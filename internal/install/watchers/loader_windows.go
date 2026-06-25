//go:build windows

package watchers

import (
	"encoding/csv"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/executil"
)

// windowsLoader implements Loader using schtasks for Windows Task Scheduler.
type windowsLoader struct{}

// schtasksCmd returns an exec.Cmd for schtasks.exe with CREATE_NO_WINDOW set
// so that the subprocess never flashes a visible console window.
func schtasksCmd(args ...string) *exec.Cmd {
	cmd := exec.Command("schtasks", args...)
	executil.NoWindow(cmd)
	return cmd
}

// NewLoader returns the Windows schtasks-based Loader.
func NewLoader() Loader { return windowsLoader{} }

// Load registers the watcher as a scheduled task using schtasks /create /xml.
// The XML file must already exist on disk (written by Write). If the task
// is already registered it is replaced (/f flag) so that the binary path
// stays current. After registration the task is started immediately so the
// watcher does not wait until the next logon.
func (windowsLoader) Load(u Unit) error {
	path, err := UnitPath(u)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("task XML not found — call Write(u) first: %s", path)
	}

	tn := u.Label()

	// /f forces overwrite of an existing task with the same name so Load
	// is idempotent even when the task is already registered.
	out, err := schtasksCmd(
		"/create",
		"/tn", tn,
		"/xml", path,
		"/f",
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks /create %s: %w\n%s", tn, err, out)
	}

	// Start the task immediately; it will also fire at next logon via
	// LogonTrigger. A start failure is non-fatal — the task is registered
	// and will activate on next logon.
	if out, err := schtasksCmd("/run", "/tn", tn).CombinedOutput(); err != nil {
		// Log the failure via the returned error wrapped as a non-fatal hint
		// so callers can surface it as a warning rather than an error.
		return fmt.Errorf("task registered but /run failed (starts at next logon): %w\n%s", errNonFatal{err}, out)
	}
	return nil
}

// errNonFatal wraps an error that indicates partial success: the primary
// operation (task registration) succeeded; only the immediate /run failed.
type errNonFatal struct{ cause error }

func (e errNonFatal) Error() string    { return e.cause.Error() }
func (e errNonFatal) Unwrap() error    { return e.cause }
func (e errNonFatal) IsNonFatal() bool { return true }

// Unload stops the scheduled task and deletes it from the Task Scheduler.
// It does not remove the XML file from disk. Idempotent — if the task does
// not exist the call succeeds.
func (windowsLoader) Unload(u Unit) error {
	tn := u.Label()

	// "Already gone" is detected via the exit code of `schtasks /query`
	// (locale-invariant) rather than by matching the localized /delete error
	// text ("cannot find" etc.), which breaks on non-English Windows. If the
	// task is not registered there is nothing to delete.
	if err := schtasksCmd("/query", "/tn", tn).Run(); err != nil {
		return nil // task doesn't exist — already gone
	}

	// Stop any running instance — ignore errors (may not be running).
	_ = schtasksCmd("/end", "/tn", tn).Run()

	out, err := schtasksCmd("/delete", "/tn", tn, "/f").CombinedOutput()
	if err != nil {
		// Race: the task was registered above but disappeared before /delete.
		// Re-check via the /query exit code; if it is gone now, the desired
		// absent state is reached. Never match the localized error text.
		if qerr := schtasksCmd("/query", "/tn", tn).Run(); qerr != nil {
			return nil // gone now — success-to-proceed
		}
		return fmt.Errorf("schtasks /delete %s: %w\n%s", tn, err, out)
	}
	return nil
}

// Status queries Task Scheduler for the watcher task state.
// It uses `schtasks /query /fo csv /v` and locates columns by header name
// so it is resilient to locale/version differences in column order.
func (windowsLoader) Status(u Unit) (WatcherStatus, error) {
	path, err := UnitPath(u)
	if err != nil {
		return WatcherStatus{TaskName: u.Label()}, err
	}

	ws := WatcherStatus{TaskName: u.Label()}

	// XML file on disk → unit is installed.
	if _, serr := os.Stat(path); !os.IsNotExist(serr) {
		ws.Installed = true
	}

	tn := u.Label()
	out, qerr := schtasksCmd("/query", "/tn", tn, "/fo", "csv", "/v").Output()
	if qerr != nil {
		// Task doesn't exist in the scheduler.
		return ws, nil
	}
	ws.Installed = true // task exists in scheduler even if XML is absent
	return parseWatcherTaskStatus(ws, out), nil
}

// parseWatcherTaskStatus parses `schtasks /query /fo csv /v` output and
// fills Running and PID into ws. It locates columns by header name so it
// is resilient to ordering differences across Windows versions.
func parseWatcherTaskStatus(ws WatcherStatus, csvData []byte) WatcherStatus {
	r := csv.NewReader(strings.NewReader(strings.TrimSpace(string(csvData))))
	records, err := r.ReadAll()
	if err != nil || len(records) < 2 {
		return ws
	}

	header := records[0]
	statusIdx := -1
	pidIdx := -1
	for i, col := range header {
		col = strings.TrimSpace(col)
		switch {
		case strings.EqualFold(col, "Status"):
			statusIdx = i
		case strings.EqualFold(col, "PID") ||
			(strings.Contains(strings.ToLower(col), "pid") &&
				!strings.EqualFold(col, "Run As User")):
			pidIdx = i
		}
	}

	for _, row := range records[1:] {
		if statusIdx >= 0 && statusIdx < len(row) {
			if strings.EqualFold(strings.TrimSpace(row[statusIdx]), "Running") {
				ws.Running = true
			}
		}
		if pidIdx >= 0 && pidIdx < len(row) {
			if pid, perr := strconv.Atoi(strings.TrimSpace(row[pidIdx])); perr == nil && pid > 0 {
				ws.PID = pid
			}
		}
		break // first data row is sufficient
	}
	return ws
}

// WatcherTaskName returns the Task Scheduler task name for a Unit.
// It is the same as Unit.Label() — exported as a convenience for callers
// that need to reference the task name without a full Unit.
func WatcherTaskName(u Unit) string { return u.Label() }
