//go:build windows

package service

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/transport"
	"github.com/cajasmota/grafel/internal/executil"
	"github.com/cajasmota/grafel/internal/install/watchers"
)

const (
	// taskName is the Windows Task Scheduler task name.
	taskName = `com.grafel.daemon`
)

// taskXMLPath returns the path where the task XML is staged before being
// imported by schtasks. We use %LOCALAPPDATA%\grafel\tasks\ which is
// user-private and does not require elevation.
func taskXMLPath() (string, error) {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("os.UserHomeDir: %w", err)
		}
		localAppData = filepath.Join(home, "AppData", "Local")
	}
	return filepath.Join(localAppData, "grafel", "tasks", taskName+".xml"), nil
}

func taskWrapperPath(xmlPath string) string {
	return strings.TrimSuffix(xmlPath, filepath.Ext(xmlPath)) + ".vbs"
}

func wscriptPath() string {
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		systemRoot = os.Getenv("WINDIR")
	}
	if systemRoot == "" {
		systemRoot = `C:\Windows`
	}
	return filepath.Join(systemRoot, "System32", "wscript.exe")
}

// currentUserSID returns the SID string for the running user.
// On failure it returns an empty string — the task template degrades a missing
// UserId to "fire on any logon" rather than emitting invalid XML.
//
// We use the native os/user API rather than shelling out to `whoami /user`:
// on a Windows dev shell whose PATH resolves `whoami` to the MSYS/Git Bash
// binary (not System32), that Unix `whoami` does not understand `/user` and
// fails, leaving the SID empty. On Windows, user.Current().Uid *is* the SID.
func currentUserSID() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(u.Uid)
}

// schtasksCmd returns an exec.Cmd for schtasks.exe with CREATE_NO_WINDOW set
// so that the subprocess never flashes a visible console window when grafel is
// launched from a GUI context (e.g., Task Scheduler running grafel install,
// or the daemon task itself restarting on logon).
func schtasksCmd(args ...string) *exec.Cmd {
	if watchers.GuardServiceCall("schtasks", args) != nil {
		return exec.Command("cmd", "/c", "exit", "1")
	}
	cmd := exec.Command("schtasks", args...)
	executil.NoWindow(cmd)
	return cmd
}

// schtasksCmdContext is schtasksCmd with a deadline. Used for `/run`, whose
// exit code we now act on (#7051): an invocation that never returned would
// turn a transient failure into a hang — strictly worse than the bug being
// fixed — so every attempt carries its own bound.
func schtasksCmdContext(ctx context.Context, args ...string) *exec.Cmd {
	if watchers.GuardServiceCall("schtasks", args) != nil {
		return exec.CommandContext(ctx, "cmd", "/c", "exit", "1")
	}
	cmd := exec.CommandContext(ctx, "schtasks", args...)
	executil.NoWindow(cmd)
	return cmd
}

// GenerateTaskXML renders the Task Scheduler XML for the given options and
// wrapper path. Exported for testing; production code calls WriteUnit, which
// calls generateTaskXML with the path the manager already resolved.
//
// wrapperPath is a parameter rather than a taskXMLPath() call (#6325 F5): the
// renderer is otherwise pure, and re-deriving the path here coupled it to
// %LOCALAPPDATA% with an os.UserHomeDir() fallback for no reason.
func GenerateTaskXML(opts Options, wrapperPath string) ([]byte, error) {
	return generateTaskXML(opts, wrapperPath)
}

// generateTaskXML resolves the environment-dependent fields (user SID, wscript
// location) and hands them to the pure renderer in schtasks_policy.go.
func generateTaskXML(opts Options, wrapperPath string) ([]byte, error) {
	return renderTaskXML(daemonTaskVars{
		TaskName:    taskName,
		UserSID:     currentUserSID(),
		WrapperHost: wscriptPath(),
		WrapperPath: wrapperPath,
		// Injected, never written as a literal in the template: the readiness
		// budget schtasksReadiness is derived from this same constant, and the
		// two silently desynchronising is the whole of #7051.
		RestartInterval: intervalXML(restartOnFailureInterval),
		RestartCount:    restartOnFailureCount,
	})
}

// schtasksManager is the Windows ServiceManager implementation. It is a thin
// adapter over schtasks / Task Scheduler; all orchestration lives in manager.go.
type schtasksManager struct {
	opts        Options
	xmlPath     string
	wrapperPath string

	// loadWarningLog collects the non-fatal failures Load() swallowed — today,
	// a `schtasks /run` that never succeeded. Embedded rather than inlined as
	// a []string so the recording behaviour is executed by an untagged test
	// (#7051; #7058 review R5). It supplies LoadWarnings, satisfying
	// loadDiagnostics.
	loadWarningLog
}

func newServiceManager(opts Options) (ServiceManager, error) {
	path, err := taskXMLPath()
	if err != nil {
		return nil, err
	}
	return &schtasksManager{opts: opts, xmlPath: path, wrapperPath: taskWrapperPath(path)}, nil
}

func (m *schtasksManager) WriteUnit() error {
	if err := os.MkdirAll(m.opts.LogDir, 0o700); err != nil {
		return fmt.Errorf("create log dir %s: %w", m.opts.LogDir, err)
	}
	xml, err := generateTaskXML(m.opts, m.wrapperPath)
	if err != nil {
		return fmt.Errorf("generate task XML: %w", err)
	}
	wrapper, err := generateDaemonWrapper(m.opts)
	if err != nil {
		return fmt.Errorf("generate daemon wrapper: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(m.xmlPath), 0o755); err != nil {
		return fmt.Errorf("create task XML dir: %w", err)
	}
	// Both artifacts are published by temp+rename (writeServiceArtifact), so
	// neither destination is ever observable half-written — which matters
	// because both paths are FIXED: taskWrapperPath and taskXMLPath are pure
	// functions of the constant taskName, so every re-render targets exactly
	// the file the already-registered task is pointing at. (The pre-#6325
	// comment here claimed writing the wrapper first left the scheduler on
	// "its previous valid action"; that was wrong — the in-place truncate
	// destroyed the live action's script.)
	//
	// Wrapper before XML is retained for the one case where order still says
	// anything: on a FIRST install a partial run leaves a wrapper with no task
	// definition (inert) rather than a task definition pointing at a file that
	// does not exist (a launch failure at next logon).
	if err := writeServiceArtifact(m.wrapperPath, wrapper, 0o600); err != nil {
		return fmt.Errorf("write daemon wrapper %s: %w", m.wrapperPath, err)
	}
	if err := writeServiceArtifact(m.xmlPath, xml, 0o644); err != nil {
		return fmt.Errorf("write task XML %s: %w", m.xmlPath, err)
	}
	return nil
}

func (m *schtasksManager) IsLoaded() (bool, error) {
	if err := schtasksCmd("/query", "/tn", taskName).Run(); err != nil {
		return false, nil // task doesn't exist
	}
	return true, nil
}

func (m *schtasksManager) Unload() error {
	stopRunningDaemon(m.opts.SocketPath)
	// If the task isn't registered there is nothing to delete — and on a clean
	// install schtasks /delete returns a localized "cannot find the file"
	// error. IsLoaded() is exit-code based (schtasks /query), so it is
	// locale-independent, unlike the English-only string match below. The
	// contract of Unload() is that "not loaded" counts as success.
	if loaded, _ := m.IsLoaded(); !loaded {
		return nil
	}
	// /end stops a running instance; /delete removes the registration. Both are
	// idempotent against a missing task: "cannot find" / "does not exist" are
	// success-to-proceed (the desired absent state is reached). The English
	// string match below remains as a best-effort fallback for races where the
	// task disappears between IsLoaded() and /delete.
	_ = schtasksCmd("/end", "/tn", taskName).Run()
	out, err := schtasksCmd("/delete", "/tn", taskName, "/f").CombinedOutput()
	if err != nil {
		s := string(out)
		// best-effort race fallback only; the PRIMARY, locale-invariant decision
		// is the IsLoaded() exit-code check above (schtasks /query). This English
		// text-match merely tolerates the task disappearing between IsLoaded() and
		// /delete and is never the sole signal. See #5317.
		if strings.Contains(s, "cannot find") || strings.Contains(s, "does not exist") { // nolint:localematch
			return nil
		}
		return fmt.Errorf("schtasks /delete: %w\n%s", err, out)
	}
	return nil
}

func (m *schtasksManager) Load() error {
	// Each Load reports on its own attempt, not on a previous one's.
	m.reset()
	// /f forces overwrite of any existing task (callers Unload first, but /f
	// keeps Load itself idempotent against a leftover registration).
	if out, err := schtasksCmd("/create", "/tn", taskName, "/xml", m.xmlPath, "/f").CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks /create: %w\n%s", err, out)
	}
	// Start now; it would otherwise fire only at next logon.
	//
	// The exit code used to be discarded outright, on the reasoning that the
	// readiness poll is the real success signal. #7051 showed what that costs:
	// the first /run immediately after /create can fail, write nothing
	// anywhere, and leave the daemon down indefinitely — while a plain retry of
	// the same unmodified task succeeds at once. So we retry it ourselves,
	// bounded (runAttemptPolicy), rather than relying on Task Scheduler's
	// RestartOnFailure, which is a whole minute away.
	//
	// It stays NON-FATAL after the last attempt: the LogonTrigger is still
	// armed and RestartOnFailure is still registered, so a failed /run is a
	// degraded start, not a failed install. What changes is that it is no
	// longer SILENT — the failure is recorded on the manager and surfaced by
	// ensureLoaded through LoadWarnings.
	if err := m.runTaskNow(context.Background()); err != nil {
		m.note(err.Error())
	}
	return nil
}

// runTaskNow fires the registered task and, unlike its predecessor, reports
// what happened.
func (m *schtasksManager) runTaskNow(ctx context.Context) error {
	return retryRun(ctx, defaultRunAttempts, time.Sleep, func(attemptCtx context.Context, n int) error {
		out, err := schtasksCmdContext(attemptCtx, "/run", "/tn", taskName).CombinedOutput()
		if err == nil {
			return nil
		}
		if trimmed := strings.TrimSpace(string(out)); trimmed != "" {
			return fmt.Errorf("schtasks /run attempt %d: %w: %s", n, err, trimmed)
		}
		return fmt.Errorf("schtasks /run attempt %d: %w", n, err)
	})
}

func (m *schtasksManager) RemoveArtifacts() error {
	if err := os.Remove(m.xmlPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove task XML %s: %w", m.xmlPath, err)
	}
	if err := os.Remove(m.wrapperPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove daemon wrapper %s: %w", m.wrapperPath, err)
	}
	return nil
}

func (m *schtasksManager) Probe() bool {
	conn, err := transport.DialTimeout(m.opts.SocketPath, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (m *schtasksManager) Status() (StatusInfo, error) { return status(m.opts) }

// install is the Windows implementation of Install.
func install(opts Options) (StatusInfo, error) {
	sm, err := newServiceManager(opts)
	if err != nil {
		return StatusInfo{}, err
	}
	if st, serr := sm.Status(); serr == nil && st.Running && sm.Probe() {
		return st, nil
	}
	// schtasksReadiness rather than the platform-neutral budget: on Windows the
	// wait has to outlast RestartOnFailure, or the safety net covering a failed
	// launch lands after we have already reported failure (#7051).
	return ensureLoaded(context.Background(), sm, schtasksReadiness, nil)
}

// restartService is the Windows implementation of Restart: always converges
// via unload→load→wait-ready (schtasks /end → /run), skipping Install's
// "already running" fast path so callers get a genuine restart.
func restartService(opts Options) (StatusInfo, error) {
	sm, err := newServiceManager(opts)
	if err != nil {
		return StatusInfo{}, err
	}
	return restart(context.Background(), sm, schtasksReadiness, nil)
}

// stopService is the Windows implementation of Stop: schtasks task deletion
// (already persistent — the task will not fire at next logon) via Unload,
// then confirm — by polling the daemon socket — that the daemon is actually
// down (issue #6044).
func stopService(opts Options) (StatusInfo, error) {
	sm, err := newServiceManager(opts)
	if err != nil {
		return StatusInfo{}, err
	}
	return stopConverge(context.Background(), sm, defaultReadiness, nil)
}

// uninstall is the Windows implementation of Uninstall.
func uninstall(opts Options) error {
	sm, err := newServiceManager(opts)
	if err != nil {
		return err
	}
	return teardown(sm)
}

// registeredRoot is the Windows implementation. The Task Scheduler XML does not
// bake a HOME/root the way the launchd plist and systemd unit do (the daemon
// derives its root from GRAFEL_DAEMON_ROOT/APPDATA at runtime), so there is no
// recorded root to read back. We report found=false (no error): the uninstall
// guard then relies on the isolated-home belt-and-suspenders check (#5277) to
// avoid tearing down a global task from an isolated install. The Windows
// named-pipe is already root-scoped (#5264/#5269), so a future enhancement can
// scope the task name per root; until then the isolated-home guard is the
// safety net.
func registeredRoot() (string, bool, error) {
	if _, err := taskXMLPath(); err != nil {
		return "", false, err
	}
	return "", false, nil
}

// status is the Windows implementation of Status.
//
// It queries Task Scheduler via:
//
//	schtasks /query /tn com.grafel.daemon /fo csv /v
//
// The CSV output includes a "Status" column and a "PID" column. We parse
// the header row to find column indices so we are not fragile against
// locale or Windows version variations in column order.
func status(opts Options) (StatusInfo, error) {
	xmlPath, err := taskXMLPath()
	if err != nil {
		return StatusInfo{}, err
	}

	info := StatusInfo{UnitFile: xmlPath}

	// Check whether the XML file exists as a proxy for "installed".
	if _, serr := os.Stat(xmlPath); os.IsNotExist(serr) {
		// Also check the scheduler directly in case XML was deleted manually.
		out, qerr := schtasksCmd("/query", "/tn", taskName, "/fo", "csv", "/v").Output()
		if qerr != nil {
			return info, nil // task doesn't exist
		}
		// Task exists in scheduler even though XML is gone — mark installed.
		info.Installed = true
		return parseTaskStatus(info, out)
	}
	info.Installed = true

	out, err := schtasksCmd("/query", "/tn", taskName, "/fo", "csv", "/v").Output()
	if err != nil {
		// The task XML exists but schtasks can't find it — scheduler and
		// filesystem are out of sync; report installed-but-not-running.
		return info, nil
	}
	return parseTaskStatus(info, out)
}

// parseTaskStatus reads the CSV output of `schtasks /query /fo csv /v` and
// fills in info.Running and info.PID. It locates columns by name so it is
// resilient to column-order changes across Windows versions.
func parseTaskStatus(info StatusInfo, csvData []byte) (StatusInfo, error) {
	r := csv.NewReader(strings.NewReader(strings.TrimSpace(string(csvData))))
	records, err := r.ReadAll()
	if err != nil || len(records) < 2 {
		return info, nil
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
			strings.EqualFold(col, "Run As User") == false &&
				strings.Contains(strings.ToLower(col), "pid"):
			pidIdx = i
		}
	}

	// Scan data rows (skip header). There may be multiple rows for the
	// same task name (one per trigger); we use the first data row.
	for _, row := range records[1:] {
		if statusIdx >= 0 && statusIdx < len(row) {
			st := strings.TrimSpace(row[statusIdx])
			if strings.EqualFold(st, "Running") {
				info.Running = true
			}
		}
		if pidIdx >= 0 && pidIdx < len(row) {
			if pid, perr := strconv.Atoi(strings.TrimSpace(row[pidIdx])); perr == nil && pid > 0 {
				info.PID = pid
			}
		}
		break // first data row is sufficient
	}
	return info, nil
}
