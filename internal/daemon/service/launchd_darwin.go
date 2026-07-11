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

	"github.com/cajasmota/grafel/internal/daemon/transport"
)

const (
	// launchLabel is the launchd service label / plist basename.
	launchLabel = "com.grafel.daemon"
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
        <string>serve</string>
    </array>

    <key>RunAtLoad</key>
    <true/>

    <key>KeepAlive</key>
    <true/>

    <!-- #5675: fd headroom so a worktree indexing storm (each subscribed
         working tree costs ~1 fd per directory) cannot exhaust fds and
         crash-loop under KeepAlive. -->
    <key>SoftResourceLimits</key>
    <dict>
        <key>NumberOfFiles</key>
        <integer>65536</integer>
    </dict>

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

// plistPath returns ~/Library/LaunchAgents/com.grafel.daemon.plist.
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

	// bootout unconditionally. launchctl exits non-zero when the service is not
	// loaded (err 3 / "No such process") — that is success-to-proceed, not a
	// failure. Every bootout outcome (not-loaded, transient I/O, success) is
	// non-fatal here: the subsequent Load + readiness poll is the real success
	// signal. So we ignore the result entirely rather than branching on the
	// localized error text, which would break on non-English macOS.
	_ = exec.Command("launchctl", "bootout", "gui/"+m.uid+"/"+launchLabel).Run()
	return nil
}

func (m *launchdManager) Load() error {
	out, err := exec.Command("launchctl", "bootstrap", "gui/"+m.uid, m.plistPath).CombinedOutput()
	if err != nil {
		// bootstrap exits non-zero (err 5 / "already bootstrapped") when a
		// previous bootout did not fully clear the service. The goal is "loaded";
		// confirm convergence via IsLoaded() (which uses the `launchctl list`
		// exit code, locale-invariant) rather than matching the localized
		// "already loaded" / "service already bootstrapped" text, which breaks
		// on non-English macOS.
		if loaded, _ := m.IsLoaded(); loaded {
			return nil // already loaded — desired state reached
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

// registeredRoot is the macOS implementation: it reads the installed
// LaunchAgent plist and extracts the HOME baked into its
// EnvironmentVariables dict — the root the live daemon serves (#5277).
func registeredRoot() (string, bool, error) {
	path, err := plistPath()
	if err != nil {
		return "", false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil // not installed — nothing to guard against
		}
		return "", false, fmt.Errorf("read plist %s: %w", path, err)
	}
	root := extractPlistHome(string(data))
	if root == "" {
		// Installed but no HOME recorded (legacy plist). Report found=true with
		// an empty root so the caller fails closed rather than assuming a match.
		return "", true, nil
	}
	return root, true, nil
}

// extractPlistHome pulls the value following the <key>HOME</key> entry from a
// rendered LaunchAgent plist. It is a small, dependency-free scan keyed on the
// plist structure this package emits (GeneratePlist); it does not attempt to be
// a general plist parser.
func extractPlistHome(plist string) string {
	const key = "<key>HOME</key>"
	idx := strings.Index(plist, key)
	if idx < 0 {
		return ""
	}
	rest := plist[idx+len(key):]
	open := strings.Index(rest, "<string>")
	if open < 0 {
		return ""
	}
	rest = rest[open+len("<string>"):]
	close := strings.Index(rest, "</string>")
	if close < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:close])
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

	// launchctl list com.grafel.daemon prints a tab-separated line:
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
