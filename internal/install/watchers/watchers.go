// Package watchers generates per-platform unit files that launch
// `grafel watch <repo>` at user login.
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

// ThrottleIntervalSeconds is the minimum number of seconds launchd must let
// elapse between two launches of the same watcher label (issue #6179).
//
// Why a value at all: with the key absent, launchd applies its own 10-second
// default. On a 140-repo machine that is 140 labels each eligible to relaunch
// every 10s — a ceiling of ~14 process launches per second, sustained forever,
// which is what produced the unbroken stream of macOS Background Activity
// notifications.
//
// Why 60: it has to clear three bars.
//   - It must exceed launchd's 10s default, or it buys nothing.
//   - It must be at least the watcher's own default poll interval (30s, see
//     newWatchCmd's --interval): relaunching a watcher faster than one duty
//     cycle cannot make progress, it only pays process-startup cost.
//   - It must still recover a genuinely crashed watcher promptly. 60s is one
//     minute of lost reactivity on a poll loop that already tolerates 30s.
//
// 60s also lowers the worst-case fleet-wide launch ceiling from ~14/s to
// ~2.3/s at 140 repos.
//
// ThrottleInterval is a flat floor, NOT a backoff, and launchd never abandons a
// KeepAlive job. On its own it therefore reduces the AMPLITUDE of a genuine
// crash loop (panic, OOM, a BinPath that a package manager moved) but not its
// DURATION: 140 crash-looping labels would relaunch at ~2.3/s indefinitely.
// That remaining unbounded-duration case is closed on the watcher side instead,
// by the rapid-restart detector in internal/cli/watch.go (watchExitFlapping),
// which gives up and exits 0 — launchd has no equivalent of systemd's
// StartLimitBurst, so the give-up has to live in the process.
const ThrottleIntervalSeconds = 60

// RestartSecSeconds and the StartLimit* values below are the systemd
// counterparts of ThrottleIntervalSeconds (#6179 F4).
//
// The unit already said Restart=on-failure, so the exit-status half of the fix
// (deliberate give-ups now exit 0) lands on Linux for free. The rate-limit half
// did not: RestartSec=5 with 140 units is ~28 restarts/second, worse than
// unfixed macOS. RestartSec moves to the same 60s floor for the same reasons.
//
// Raising RestartSec alone would have REMOVED Linux's only give-up, though.
// systemd's defaults (StartLimitBurst=5 within StartLimitIntervalSec=10s)
// eventually push a crash-looping unit into a failed state — but five restarts
// 60s apart span 300s, so the 10s window can never trip and the unit would
// restart forever. The limit window is therefore set explicitly to an hour, so
// StartLimitBurst still means something at the new interval.
const (
	RestartSecSeconds         = 60
	StartLimitIntervalSeconds = 3600
	StartLimitBurst           = 5
)

// xmlEsc escapes s for inclusion in XML character data or an XML attribute.
//
// #6179 F5: Group names and repo paths are user-supplied, and the plist/task
// bodies below are assembled by string concatenation. A group named `R&D`, or
// any path containing `&`, `<` or `>`, previously produced a body that fails
// `plutil -lint` ("unknown ampersand-escape sequence") — launchd then silently
// rejects the job, so the watcher never runs and nothing says why.
//
// Note this escapes the RENDERED body only. Unit.Label() is deliberately left
// alone: the label is also the plist FILENAME and launchd's job identity, so
// changing how it is derived would orphan every already-installed unit.
func xmlEsc(s string) string {
	var b strings.Builder
	// EscapeText writes to an io.Writer and never fails on a strings.Builder.
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// Unit describes a single watcher unit to install.
type Unit struct {
	Group   string
	Repo    string
	BinPath string // absolute path to the grafel binary
}

// Label returns the platform-agnostic label for a unit.
func (u Unit) Label() string {
	slug := slugify(u.Repo)
	return fmt.Sprintf("com.grafel.watcher.%s.%s", u.Group, slug)
}

// LaunchdPlist returns the macOS launchd .plist body for a watcher.
func LaunchdPlist(u Unit) string {
	// (An unused `pl` struct used to sit here describing the plist keys; it was
	// never marshalled and drifted out of sync with the hand-built body below,
	// claiming an unconditional KeepAlive bool. Removed with #6179.)
	logDir := filepath.Join(u.Repo, ".grafel", "logs")
	body := strings.Builder{}
	body.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	body.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple Computer//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	body.WriteString(`<plist version="1.0">` + "\n")
	body.WriteString("<dict>\n")
	body.WriteString("  <key>Label</key>\n")
	body.WriteString(fmt.Sprintf("  <string>%s</string>\n", xmlEsc(u.Label())))
	body.WriteString("  <key>ProgramArguments</key>\n")
	body.WriteString("  <array>\n")
	body.WriteString(fmt.Sprintf("    <string>%s</string>\n", xmlEsc(u.BinPath)))
	body.WriteString("    <string>watch</string>\n")
	body.WriteString(fmt.Sprintf("    <string>%s</string>\n", xmlEsc(u.Repo)))
	body.WriteString("  </array>\n")
	body.WriteString("  <key>RunAtLoad</key><true/>\n")
	// KeepAlive is CONDITIONAL, not unconditional (#6179). `grafel watch` has
	// deliberate exit paths — the repo path no longer stats, and the
	// consecutive-index-failure ceiling from #5140 — and it exits 0 on those
	// (see internal/cli/watch.go's watchExitRespawn table). SuccessfulExit=false
	// means "respawn only when the last exit was NOT successful", so those
	// give-ups stay dead and a genuine crash (panic, OOM, signal death) still
	// comes back. Under the previous <true/> launchd respawned everything
	// including the give-ups, which is how a single diagnosable failure became a
	// permanent relaunch loop.
	body.WriteString("  <key>KeepAlive</key>\n")
	body.WriteString("  <dict>\n")
	body.WriteString("    <key>SuccessfulExit</key><false/>\n")
	body.WriteString("  </dict>\n")
	// Bound the relaunch rate. Absent this key launchd uses a 10s default; see
	// ThrottleIntervalSeconds for why 60.
	body.WriteString("  <key>ThrottleInterval</key>\n")
	body.WriteString(fmt.Sprintf("  <integer>%d</integer>\n", ThrottleIntervalSeconds))
	body.WriteString("  <key>WorkingDirectory</key>\n")
	body.WriteString(fmt.Sprintf("  <string>%s</string>\n", xmlEsc(u.Repo)))
	body.WriteString("  <key>StandardOutPath</key>\n")
	body.WriteString(fmt.Sprintf("  <string>%s/watcher.out.log</string>\n", xmlEsc(logDir)))
	body.WriteString("  <key>StandardErrorPath</key>\n")
	body.WriteString(fmt.Sprintf("  <string>%s/watcher.err.log</string>\n", xmlEsc(logDir)))
	body.WriteString("</dict>\n")
	body.WriteString("</plist>\n")
	return body.String()
}

// SystemdUnit returns the Linux systemd-user .service body.
//
// Restart=on-failure is the systemd analogue of the plist's
// KeepAlive={SuccessfulExit:false}: the deliberate give-ups in
// internal/cli/watch.go now exit 0, so systemd leaves them stopped. RestartSec
// and the StartLimit* pair are the rate-limit and give-up halves — see the
// constants for why 60/3600/5.
func SystemdUnit(u Unit) string {
	return fmt.Sprintf(`[Unit]
Description=grafel watcher (%s/%s)
After=default.target
StartLimitIntervalSec=%d
StartLimitBurst=%d

[Service]
Type=simple
ExecStart=%q watch %q
WorkingDirectory=%s
Restart=on-failure
RestartSec=%d

[Install]
WantedBy=default.target
`, u.Group, filepath.Base(u.Repo), StartLimitIntervalSeconds, StartLimitBurst,
		u.BinPath, u.Repo, u.Repo, RestartSecSeconds)
}

// SchtasksXML returns a Windows Task Scheduler XML definition.
//
// Every interpolated field is XML-escaped for the same reason as the plist
// (#6179 F5): group names and repo paths are user-supplied, and an unescaped
// `&` produces a task definition schtasks /create refuses to parse.
func SchtasksXML(u Unit) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>grafel watcher (%s/%s)</Description>
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
`, xmlEsc(u.Group), xmlEsc(filepath.Base(u.Repo)), xmlEsc(u.BinPath), xmlEsc(u.Repo), xmlEsc(u.Repo))
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
		return filepath.Join(home, "AppData", "Local", "grafel", "tasks"), nil
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

// Cleanup fully deactivates and removes the watcher unit for a single repo:
// it Unloads the unit from the OS scheduler (launchctl bootout / systemctl
// disable / schtasks delete) and then removes the on-disk unit file. Both
// steps are idempotent — a not-loaded unit and a missing file are treated as
// success — so Cleanup is safe to call when deleting a group whether or not a
// watcher was ever activated. This prevents stale com.grafel.watcher.<group>.*
// launchd jobs / plists from lingering and fighting a later recreate (#5338).
func Cleanup(group, repoPath, binPath string) {
	u := Unit{Group: group, Repo: repoPath, BinPath: binPath}
	loader := NewLoader()
	// Deregister from the OS scheduler before deleting the file so the OS does
	// not try to relaunch a missing binary. Errors are tolerated: "not loaded"
	// already satisfies the desired absent state.
	_ = loader.Unload(u)
	_ = Remove(u)
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
