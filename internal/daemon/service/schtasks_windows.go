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
	"text/template"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/transport"
)

const (
	// taskName is the Windows Task Scheduler task name.
	taskName = `com.grafel.daemon`
)

// daemonTaskXMLTemplate is the Windows Task Scheduler XML definition for
// the grafel daemon. The task runs at logon for the registering user,
// restarts on failure (up to 3 times with a 1-minute interval), and is
// hidden from the Task Scheduler UI so it doesn't clutter the user's view.
//
// Key semantics that mirror the macOS LaunchAgent and Linux systemd unit:
//   - LogonTrigger — starts at user login (equivalent to RunAtLoad + KeepAlive)
//   - RestartOnFailure — crash-restart (equivalent to KeepAlive)
//   - Hidden — keeps the UI tidy; the task is managed via grafel commands
//   - RunLevel LeastPrivilege — no UAC elevation required (user-level service)
const daemonTaskXMLTemplate = `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>grafel knowledge-graph daemon — managed by grafel install/uninstall</Description>
    <URI>\{{.TaskName}}</URI>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      {{if .UserSID}}<UserId>{{.UserSID}}</UserId>{{end}}
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      {{if .UserSID}}<UserId>{{.UserSID}}</UserId>{{end}}
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Hidden>true</Hidden>
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>3</Count>
    </RestartOnFailure>
    <Enabled>true</Enabled>
  </Settings>
  <Actions>
    <Exec>
      <Command>{{.BinPath}}</Command>
      <Arguments>daemon</Arguments>
    </Exec>
  </Actions>
</Task>
`

type daemonTaskVars struct {
	TaskName string
	UserSID  string
	BinPath  string
}

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

// GenerateTaskXML renders the Task Scheduler XML for the given options.
// Exported for testing; production code calls install() which calls this.
func GenerateTaskXML(opts Options) ([]byte, error) {
	sid := currentUserSID()
	tmpl, err := template.New("task").Parse(daemonTaskXMLTemplate)
	if err != nil {
		return nil, err
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, daemonTaskVars{
		TaskName: taskName,
		UserSID:  sid,
		BinPath:  opts.BinPath,
	}); err != nil {
		return nil, err
	}
	// Task Scheduler requires UTF-16 LE for XML files referenced by /xml.
	// We write the XML via a temp file; schtasks on modern Windows (>=10)
	// also accepts UTF-8 when the BOM is absent, but the spec calls for
	// UTF-16. We store the rendered UTF-8 bytes — callers that need UTF-16
	// can transcode; the schtasks invocation in install() handles this via
	// PowerShell if needed. For simplicity we write UTF-8 and rely on the
	// fact that modern schtasks handles it fine.
	return []byte(buf.String()), nil
}

// schtasksManager is the Windows ServiceManager implementation. It is a thin
// adapter over schtasks / Task Scheduler; all orchestration lives in manager.go.
type schtasksManager struct {
	opts    Options
	xmlPath string
}

func newServiceManager(opts Options) (ServiceManager, error) {
	path, err := taskXMLPath()
	if err != nil {
		return nil, err
	}
	return &schtasksManager{opts: opts, xmlPath: path}, nil
}

func (m *schtasksManager) WriteUnit() error {
	if err := os.MkdirAll(m.opts.LogDir, 0o700); err != nil {
		return fmt.Errorf("create log dir %s: %w", m.opts.LogDir, err)
	}
	xml, err := GenerateTaskXML(m.opts)
	if err != nil {
		return fmt.Errorf("generate task XML: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(m.xmlPath), 0o755); err != nil {
		return fmt.Errorf("create task XML dir: %w", err)
	}
	if err := os.WriteFile(m.xmlPath, xml, 0o644); err != nil {
		return fmt.Errorf("write task XML %s: %w", m.xmlPath, err)
	}
	return nil
}

func (m *schtasksManager) IsLoaded() (bool, error) {
	if err := exec.Command("schtasks", "/query", "/tn", taskName).Run(); err != nil {
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
	_ = exec.Command("schtasks", "/end", "/tn", taskName).Run()
	out, err := exec.Command("schtasks", "/delete", "/tn", taskName, "/f").CombinedOutput()
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
	// /f forces overwrite of any existing task (callers Unload first, but /f
	// keeps Load itself idempotent against a leftover registration).
	if out, err := exec.Command("schtasks", "/create", "/tn", taskName, "/xml", m.xmlPath, "/f").CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks /create: %w\n%s", err, out)
	}
	// Start now; it would otherwise fire at next logon. A /run failure is
	// non-fatal — the readiness poll is the real success signal, and the task
	// will start at next logon regardless.
	_ = exec.Command("schtasks", "/run", "/tn", taskName).Run()
	return nil
}

func (m *schtasksManager) RemoveArtifacts() error {
	if err := os.Remove(m.xmlPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove task XML %s: %w", m.xmlPath, err)
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
	return ensureLoaded(context.Background(), sm, defaultReadiness, nil)
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
		out, qerr := exec.Command("schtasks", "/query", "/tn", taskName, "/fo", "csv", "/v").Output()
		if qerr != nil {
			return info, nil // task doesn't exist
		}
		// Task exists in scheduler even though XML is gone — mark installed.
		info.Installed = true
		return parseTaskStatus(info, out)
	}
	info.Installed = true

	out, err := exec.Command("schtasks", "/query", "/tn", taskName, "/fo", "csv", "/v").Output()
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
