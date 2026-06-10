//go:build linux

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
	unitName = "archigraph-daemon"
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

// systemdManager is the Linux ServiceManager implementation. It is a thin
// adapter over systemctl --user; all orchestration lives in manager.go.
type systemdManager struct {
	opts     Options
	unitPath string
	unitID   string
}

func newServiceManager(opts Options) (ServiceManager, error) {
	path, err := unitPath()
	if err != nil {
		return nil, err
	}
	return &systemdManager{
		opts:     opts,
		unitPath: path,
		unitID:   unitName + ".service",
	}, nil
}

func (m *systemdManager) WriteUnit() error {
	if err := os.MkdirAll(m.opts.LogDir, 0o700); err != nil {
		return fmt.Errorf("create log dir %s: %w", m.opts.LogDir, err)
	}
	unit, err := GenerateUnit(m.opts)
	if err != nil {
		return fmt.Errorf("generate unit: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(m.unitPath), 0o755); err != nil {
		return fmt.Errorf("create systemd user dir: %w", err)
	}
	if err := os.WriteFile(m.unitPath, unit, 0o644); err != nil {
		return fmt.Errorf("write unit %s: %w", m.unitPath, err)
	}
	// Reload so systemd picks up the (re)written unit file. Non-fatal if the
	// user systemd manager isn't reachable yet — Load surfaces real failures.
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	return nil
}

func (m *systemdManager) IsLoaded() (bool, error) {
	// is-active exits 0 only when active; is-enabled covers loaded-but-stopped.
	if err := exec.Command("systemctl", "--user", "is-active", "--quiet", m.unitID).Run(); err == nil {
		return true, nil
	}
	out, err := exec.Command("systemctl", "--user", "is-enabled", m.unitID).Output()
	if err != nil {
		return false, nil
	}
	state := strings.TrimSpace(string(out))
	return state == "enabled" || state == "static" || state == "linked", nil
}

func (m *systemdManager) Unload() error {
	stopRunningDaemon(m.opts.SocketPath)
	// disable --now stops + disables. systemctl exits non-zero when the unit is
	// not loaded; treat that as success-to-proceed (desired state reached).
	_ = exec.Command("systemctl", "--user", "disable", "--now", m.unitID).Run()
	_ = exec.Command("systemctl", "--user", "reset-failed", m.unitID).Run()
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	return nil
}

func (m *systemdManager) Load() error {
	if out, err := exec.Command("systemctl", "--user", "enable", "--now", m.unitID).CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl enable --now: %w\n%s", err, out)
	}
	return nil
}

func (m *systemdManager) RemoveArtifacts() error {
	if err := os.Remove(m.unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove unit %s: %w", m.unitPath, err)
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	return nil
}

func (m *systemdManager) Probe() bool {
	conn, err := transport.DialTimeout(m.opts.SocketPath, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (m *systemdManager) Status() (StatusInfo, error) { return status(m.opts) }

// install is the Linux implementation of Install.
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

// uninstall is the Linux implementation of Uninstall.
func uninstall(opts Options) error {
	sm, err := newServiceManager(opts)
	if err != nil {
		return err
	}
	return teardown(sm)
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
