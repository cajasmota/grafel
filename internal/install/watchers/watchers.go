// Package watchers generates per-platform unit files that launch
// `archigraph watch <repo>` at user login.
//
// We deliberately keep the unit-generation pure: each function returns
// the on-disk text for a unit/plist/scheduled-task. Tests can string-
// compare those bytes without ever needing the surrounding OS.
package watchers

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Unit describes a single watcher unit to install.
type Unit struct {
	Group   string
	Repo    string
	BinPath string // absolute path to the archigraph binary
}

// Label returns the platform-agnostic label for a unit.
func (u Unit) Label() string {
	slug := slugify(u.Repo)
	return fmt.Sprintf("com.archigraph.watcher.%s.%s", u.Group, slug)
}

// LaunchdPlist returns the macOS launchd .plist body for a watcher.
func LaunchdPlist(u Unit) string {
	type pl struct {
		XMLName     xml.Name `xml:"dict"`
		Label       string
		ProgramArgs []string
		WorkingDir  string
		RunAtLoad   bool
		KeepAlive   bool
		StdOutPath  string
		StdErrPath  string
	}
	logDir := filepath.Join(u.Repo, ".archigraph", "logs")
	body := strings.Builder{}
	body.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	body.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple Computer//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	body.WriteString(`<plist version="1.0">` + "\n")
	body.WriteString("<dict>\n")
	body.WriteString("  <key>Label</key>\n")
	body.WriteString(fmt.Sprintf("  <string>%s</string>\n", u.Label()))
	body.WriteString("  <key>ProgramArguments</key>\n")
	body.WriteString("  <array>\n")
	body.WriteString(fmt.Sprintf("    <string>%s</string>\n", u.BinPath))
	body.WriteString("    <string>watch</string>\n")
	body.WriteString(fmt.Sprintf("    <string>%s</string>\n", u.Repo))
	body.WriteString("  </array>\n")
	body.WriteString("  <key>RunAtLoad</key><true/>\n")
	body.WriteString("  <key>KeepAlive</key><true/>\n")
	body.WriteString("  <key>WorkingDirectory</key>\n")
	body.WriteString(fmt.Sprintf("  <string>%s</string>\n", u.Repo))
	body.WriteString("  <key>StandardOutPath</key>\n")
	body.WriteString(fmt.Sprintf("  <string>%s/watcher.out.log</string>\n", logDir))
	body.WriteString("  <key>StandardErrorPath</key>\n")
	body.WriteString(fmt.Sprintf("  <string>%s/watcher.err.log</string>\n", logDir))
	body.WriteString("</dict>\n")
	body.WriteString("</plist>\n")
	return body.String()
}

// SystemdUnit returns the Linux systemd-user .service body.
func SystemdUnit(u Unit) string {
	return fmt.Sprintf(`[Unit]
Description=archigraph watcher (%s/%s)
After=default.target

[Service]
Type=simple
ExecStart=%q watch %q
WorkingDirectory=%s
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`, u.Group, filepath.Base(u.Repo), u.BinPath, u.Repo, u.Repo)
}

// SchtasksXML returns a Windows Task Scheduler XML definition.
func SchtasksXML(u Unit) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>archigraph watcher (%s/%s)</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger><Enabled>true</Enabled></LogonTrigger>
  </Triggers>
  <Settings>
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>3</Count>
    </RestartOnFailure>
  </Settings>
  <Actions>
    <Exec>
      <Command>%s</Command>
      <Arguments>watch "%s"</Arguments>
      <WorkingDirectory>%s</WorkingDirectory>
    </Exec>
  </Actions>
</Task>
`, u.Group, filepath.Base(u.Repo), u.BinPath, u.Repo, u.Repo)
}

// PlistDir returns the user-level launchd directory.
func PlistDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents"), nil
}

// SystemdDir returns ~/.config/systemd/user.
func SystemdDir() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "systemd", "user"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user"), nil
}

// UnitDir returns the directory where a unit/plist for the current OS
// should be written.
func UnitDir() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return PlistDir()
	case "linux":
		return SystemdDir()
	case "windows":
		// Use a per-user data dir; the actual schtasks /create call is
		// what registers it with the scheduler.
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "AppData", "Local", "archigraph", "tasks"), nil
	}
	return "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
}

// UnitPath returns the canonical path for a Unit on this OS.
func UnitPath(u Unit) (string, error) {
	dir, err := UnitDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(dir, u.Label()+".plist"), nil
	case "linux":
		return filepath.Join(dir, u.Label()+".service"), nil
	case "windows":
		return filepath.Join(dir, u.Label()+".xml"), nil
	}
	return "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
}

// Render returns the unit body for the current OS.
func Render(u Unit) string {
	switch runtime.GOOS {
	case "darwin":
		return LaunchdPlist(u)
	case "linux":
		return SystemdUnit(u)
	case "windows":
		return SchtasksXML(u)
	}
	return ""
}

// Write writes the unit file to its canonical path. Caller is
// responsible for invoking the OS-native loader (`launchctl load`,
// `systemctl --user daemon-reload`, or `schtasks /create /xml`) — we
// keep this package free of side effects beyond the file.
func Write(u Unit) (string, error) {
	path, err := UnitPath(u)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(Render(u)), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// Remove deletes the unit file if it exists.
func Remove(u Unit) error {
	path, err := UnitPath(u)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// slugify produces a label-safe slug from a path.
func slugify(s string) string {
	s = filepath.Base(s)
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			out = append(out, c)
		case c >= 'A' && c <= 'Z':
			out = append(out, c+32)
		default:
			out = append(out, '-')
		}
	}
	if len(out) == 0 {
		return "repo"
	}
	return string(out)
}
