//go:build darwin

package service

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"
)

const (
	// launchLabel is the launchd service label / plist basename.
	launchLabel = "com.archigraph.daemon"

	// socketWaitTimeout is the maximum time Install will block waiting
	// for the daemon socket to become connectable after launchctl loads
	// the agent.
	socketWaitTimeout = 5 * time.Second
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

// install is the macOS implementation of Install.
func install(opts Options) (StatusInfo, error) {
	path, err := plistPath()
	if err != nil {
		return StatusInfo{}, err
	}

	// Idempotency: if plist already exists and the service is running,
	// just return the current status.
	if _, err := os.Stat(path); err == nil {
		st, sterr := status(opts)
		if sterr == nil && st.Running {
			return st, nil
		}
	}

	// Ensure log directory exists before launchd writes to the log file.
	if err := os.MkdirAll(opts.LogDir, 0o700); err != nil {
		return StatusInfo{}, fmt.Errorf("create log dir %s: %w", opts.LogDir, err)
	}

	plist, err := GeneratePlist(opts)
	if err != nil {
		return StatusInfo{}, fmt.Errorf("generate plist: %w", err)
	}

	// Ensure ~/Library/LaunchAgents exists (it normally does, but may
	// be missing on headless / CI machines).
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return StatusInfo{}, fmt.Errorf("create LaunchAgents dir: %w", err)
	}

	if err := os.WriteFile(path, plist, 0o644); err != nil {
		return StatusInfo{}, fmt.Errorf("write plist %s: %w", path, err)
	}

	// Stop any running daemon before bootstrapping. A leftover daemon
	// from a previous binary (or a manual 'archigraph start') holds the
	// PID file; if it's still running the new launchd-managed process
	// will exit immediately with "daemon already running".
	stopRunningDaemon(opts.SocketPath)

	// If a stale service entry exists (e.g. from a previous crash that
	// left the plist without unloading), bootout first so bootstrap can
	// succeed cleanly.
	uid := strconv.Itoa(os.Getuid())
	_ = exec.Command("launchctl", "bootout", "gui/"+uid+"/"+launchLabel).Run()

	// Bootstrap loads the plist and starts the service.
	out, err := exec.Command("launchctl", "bootstrap", "gui/"+uid, path).CombinedOutput()
	if err != nil {
		return StatusInfo{}, fmt.Errorf("launchctl bootstrap: %w\n%s", err, out)
	}

	// Wait for the socket so callers know the daemon is ready.
	if werr := waitForSocket(opts.SocketPath, socketWaitTimeout); werr != nil {
		return StatusInfo{UnitFile: path, Installed: true},
			fmt.Errorf("service loaded but socket not ready: %w", werr)
	}

	return status(opts)
}

// uninstall is the macOS implementation of Uninstall.
func uninstall(opts Options) error {
	path, err := plistPath()
	if err != nil {
		return err
	}

	uid := strconv.Itoa(os.Getuid())
	// bootout stops + unloads. Ignore errors (service may not be loaded).
	_ = exec.Command("launchctl", "bootout", "gui/"+uid+"/"+launchLabel).Run()

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist %s: %w", path, err)
	}
	return nil
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
