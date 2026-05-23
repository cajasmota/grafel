//go:build windows

package watchers

import (
	"encoding/csv"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// windowsLoader implements Loader using schtasks for Windows Task Scheduler.
type windowsLoader struct{}

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
	out, err := exec.Command(
		"schtasks",
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
	if out, err := exec.Command("schtasks", "/run", "/tn", tn).CombinedOutput(); err != nil {
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

	// Stop any running instance — ignore errors (may not be running).
	_ = exec.Command("schtasks", "/end", "/tn", tn).Run()

	out, err := exec.Command("schtasks", "/delete", "/tn", tn, "/f").CombinedOutput()
	if err != nil {
		s := string(out)
		if strings.Contains(s, "cannot find") ||
			strings.Contains(s, "does not exist") {
			return nil // already gone — treat as success
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
	out, qerr := exec.Command("schtasks", "/query", "/tn", tn, "/fo", "csv", "/v").Output()
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
