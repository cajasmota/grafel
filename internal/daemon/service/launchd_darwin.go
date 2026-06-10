//go:build darwin

package service

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon/transport"
)

const (
	// launchLabel is the launchd service label / plist basename.
	launchLabel = "com.archigraph.daemon"
)

// plistTemplate is the LaunchAgent property list. The daemon runs as
// the current user (no UserName key needed in the user launchd domain).
// KeepAlive + RunAtLoad provide auto-start + crash-restart semantics.
const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
    "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{.Label}}</string>

    <key>ProgramArguments</key>
    <array>
        <string>{{.BinPath}}</string>
        <string>daemon</string>
    </array>

    <key>RunAtLoad</key>
    <true/>

    <key>KeepAlive</key>
    <true/>

    <key>StandardOutPath</key>
    <string>{{.LogDir}}/daemon.log</string>

    <key>StandardErrorPath</key>
    <string>{{.LogDir}}/daemon.err</string>

    <key>EnvironmentVariables</key>
    <dict>
        <key>HOME</key>
        <string>{{.Home}}</string>
    </dict>
</dict>
</plist>
`

type plistVars struct {
	Label   string
	BinPath string
	LogDir  string
	Home    string
}

// plistPath returns ~/Library/LaunchAgents/com.archigraph.daemon.plist.
func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchLabel+".plist"), nil
}

// GeneratePlist renders the LaunchAgent plist for the given options.
// Exported for testing; production code calls install() which calls
// this internally.
func GeneratePlist(opts Options) ([]byte, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New("plist").Parse(plistTemplate)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, plistVars{
		Label:   launchLabel,
		BinPath: opts.BinPath,
		LogDir:  opts.LogDir,
		Home:    home,
	}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// launchdManager is the macOS ServiceManager implementation. It is a thin
// adapter over launchctl; all orchestration (clear-then-load ordering,
// readiness polling, idempotent teardown) lives in the platform-agnostic
// manager.go so it can be unit-tested with a fake.
type launchdManager struct {
	opts      Options
	plistPath string
	uid       string
}

func newServiceManager(opts Options) (ServiceManager, error) {
	path, err := plistPath()
	if err != nil {
		return nil, err
	}
	return &launchdManager{
		opts:      opts,
		plistPath: path,
		uid:       strconv.Itoa(os.Getuid()),
	}, nil
}

func (m *launchdManager) WriteUnit() error {
	if err := os.MkdirAll(m.opts.LogDir, 0o700); err != nil {
		return fmt.Errorf("create log dir %s: %w", m.opts.LogDir, err)
	}
	plist, err := GeneratePlist(m.opts)
	if err != nil {
		return fmt.Errorf("generate plist: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(m.plistPath), 0o755); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}
	if err := os.WriteFile(m.plistPath, plist, 0o644); err != nil {
		return fmt.Errorf("write plist %s: %w", m.plistPath, err)
	}
	return nil
}

func (m *launchdManager) IsLoaded() (bool, error) {
	// `launchctl list <label>` exits non-zero (113) when the service is not
	// loaded; that is a clean "false", not an error.
	if err := exec.Command("launchctl", "list", launchLabel).Run(); err != nil {
		return false, nil
	}
	return true, nil
}

func (m *launchdManager) Unload() error {
	// Stop any running daemon first so it releases the PID file before launchd
	// tears the service down.
	stopRunningDaemon(m.opts.SocketPath)

	// bootout unconditionally. launchctl returns err 3 ("No such process") when
	// the service is not loaded — that is success-to-proceed, not a failure.
	out, err := exec.Command("launchctl", "bootout", "gui/"+m.uid+"/"+launchLabel).CombinedOutput()
	if err != nil {
		s := string(out)
		if strings.Contains(s, "No such process") || strings.Contains(s, "Boot-out failed: 3") ||
			strings.Contains(s, "could not find") {
			return nil // not loaded — desired state already reached
		}
		// Other bootout failures (e.g. transient I/O) are non-fatal: the
		// subsequent Load + readiness poll is the real success signal.
		return nil
	}
	return nil
}

func (m *launchdManager) Load() error {
	out, err := exec.Command("launchctl", "bootstrap", "gui/"+m.uid, m.plistPath).CombinedOutput()
	if err != nil {
		s := string(out)
		// "service already loaded" / err 5 means a previous bootout did not
		// fully clear it. Converge: the service IS loaded, which is the goal.
		if strings.Contains(s, "already loaded") || strings.Contains(s, "service already bootstrapped") {
			return nil
		}
		return fmt.Errorf("launchctl bootstrap: %w\n%s", err, out)
	}
	return nil
}

func (m *launchdManager) RemoveArtifacts() error {
	if err := os.Remove(m.plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist %s: %w", m.plistPath, err)
	}
	return nil
}

func (m *launchdManager) Probe() bool {
	conn, err := transport.DialTimeout(m.opts.SocketPath, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (m *launchdManager) Status() (StatusInfo, error) { return status(m.opts) }

// install is the macOS implementation of Install: converge to loaded+ready via
// the agnostic orchestrator.
func install(opts Options) (StatusInfo, error) {
	sm, err := newServiceManager(opts)
	if err != nil {
		return StatusInfo{}, err
	}
	// Fast idempotent path: already running and connectable.
	if st, serr := sm.Status(); serr == nil && st.Running && sm.Probe() {
		return st, nil
	}
	return ensureLoaded(context.Background(), sm, defaultReadiness, nil)
}

// uninstall is the macOS implementation of Uninstall: idempotent teardown.
func uninstall(opts Options) error {
	sm, err := newServiceManager(opts)
	if err != nil {
		return err
	}
	return teardown(sm)
}

// status is the macOS implementation of Status.
func status(opts Options) (StatusInfo, error) {
	path, err := plistPath()
	if err != nil {
		return StatusInfo{}, err
	}

	info := StatusInfo{UnitFile: path}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return info, nil
	}
	info.Installed = true

	// launchctl list com.archigraph.daemon prints a tab-separated line:
	// <pid | -> <last-exit-status> <label>
	out, err := exec.Command("launchctl", "list", launchLabel).Output()
	if err != nil {
		// Exit 113 means the service isn't loaded; that's a valid "not running" state.
		return info, nil
	}
	line := strings.TrimSpace(string(out))
	fields := strings.Fields(line)
	if len(fields) >= 1 && fields[0] != "-" {
		info.Running = true
		if pid, perr := strconv.Atoi(fields[0]); perr == nil {
			info.PID = pid
		}
	}
	return info, nil
}
