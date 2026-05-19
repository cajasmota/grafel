//go:build linux

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
	unitName          = "archigraph-daemon"
	socketWaitTimeout = 5 * time.Second
)

// unitTemplate is the systemd user unit file. Type=simple is correct
// because the daemon does not fork. Restart=on-failure covers crashes;
// WantedBy=default.target ensures the service starts at user login when
// lingering is enabled (or when a user session starts on standard
// desktop systems).
const unitTemplate = `[Unit]
Description=archigraph knowledge-graph daemon
Documentation=https://github.com/cajasmota/archigraph
After=network.target

[Service]
Type=simple
ExecStart={{.BinPath}} daemon
Restart=on-failure
RestartSec=3s
Environment=HOME={{.Home}}

[Install]
WantedBy=default.target
`

type unitVars struct {
	BinPath string
	Home    string
}

// unitPath returns ~/.config/systemd/user/archigraph-daemon.service.
func unitPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user", unitName+".service"), nil
}

// GenerateUnit renders the systemd unit for the given options.
// Exported for testing.
func GenerateUnit(opts Options) ([]byte, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New("unit").Parse(unitTemplate)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, unitVars{
		BinPath: opts.BinPath,
		Home:    home,
	}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// install is the Linux implementation of Install.
func install(opts Options) (StatusInfo, error) {
	path, err := unitPath()
	if err != nil {
		return StatusInfo{}, err
	}

	// Idempotency check.
	if _, err := os.Stat(path); err == nil {
		st, sterr := status(opts)
		if sterr == nil && st.Running {
			return st, nil
		}
	}

	// Ensure log dir exists even though systemd handles stdout/stderr
	// via the journal; downstream code may write there directly.
	if err := os.MkdirAll(opts.LogDir, 0o700); err != nil {
		return StatusInfo{}, fmt.Errorf("create log dir %s: %w", opts.LogDir, err)
	}

	unit, err := GenerateUnit(opts)
	if err != nil {
		return StatusInfo{}, fmt.Errorf("generate unit: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return StatusInfo{}, fmt.Errorf("create systemd user dir: %w", err)
	}

	if err := os.WriteFile(path, unit, 0o644); err != nil {
		return StatusInfo{}, fmt.Errorf("write unit %s: %w", path, err)
	}

	// Stop any running daemon before enabling+starting the systemd unit.
	stopRunningDaemon(opts.SocketPath)

	// Reload unit files so systemd picks up the new file.
	if out, err := exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput(); err != nil {
		return StatusInfo{}, fmt.Errorf("systemctl daemon-reload: %w\n%s", err, out)
	}

	// Enable + start in one shot.
	if out, err := exec.Command("systemctl", "--user", "enable", "--now", unitName+".service").CombinedOutput(); err != nil {
		return StatusInfo{}, fmt.Errorf("systemctl enable --now: %w\n%s", err, out)
	}

	if werr := waitForSocket(opts.SocketPath, socketWaitTimeout); werr != nil {
		return StatusInfo{UnitFile: path, Installed: true},
			fmt.Errorf("service loaded but socket not ready: %w", werr)
	}

	return status(opts)
}

// uninstall is the Linux implementation of Uninstall.
func uninstall(opts Options) error {
	path, err := unitPath()
	if err != nil {
		return err
	}

	// stop + disable; ignore errors (service may not be loaded).
	_ = exec.Command("systemctl", "--user", "disable", "--now", unitName+".service").Run()
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove unit %s: %w", path, err)
	}
	return nil
}

// status is the Linux implementation of Status.
func status(opts Options) (StatusInfo, error) {
	path, err := unitPath()
	if err != nil {
		return StatusInfo{}, err
	}

	info := StatusInfo{UnitFile: path}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return info, nil
	}
	info.Installed = true

	// systemctl --user show outputs KEY=VALUE pairs.
	out, err := exec.Command("systemctl", "--user", "show",
		"--property=ActiveState,MainPID", unitName+".service").Output()
	if err != nil {
		return info, nil
	}
	for _, line := range strings.Split(string(out), "\n") {
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "ActiveState":
			if kv[1] == "active" {
				info.Running = true
			}
		case "MainPID":
			if pid, perr := strconv.Atoi(kv[1]); perr == nil && pid > 0 {
				info.PID = pid
			}
		}
	}
	return info, nil
}
