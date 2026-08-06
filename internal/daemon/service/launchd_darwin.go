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
	"github.com/cajasmota/grafel/internal/install/watchers"
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

	// lastDisableErr records the outcome of the most recent launchctlDisable
	// call made by Unload (issue #6044 review item 3). stopConverge's
	// ServiceManager interface has no room for a second, persistence-specific
	// error channel, so stopService (which holds the concrete *launchdManager,
	// not just the interface) reads this directly after a successful
	// stopConverge to decide whether it can honestly claim the stop survives
	// reboot/login, instead of asserting that unconditionally over a
	// discarded error.
	lastDisableErr error
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

// launchctlBootout / launchctlBootstrap / launchctlDisable / launchctlEnable
// are package vars (not direct inline exec.Command calls) so Unload/Load's
// full behavior — including the #6044 persistent-stop pairing (review item
// 2) — is unit-testable without ever invoking a real `launchctl bootout` /
// `bootstrap` against gui/$UID/com.grafel.daemon, which on a dev machine is
// the user's actual, live daemon (see launchd_darwin_stop_test.go: it
// overrides all four and calls the REAL Unload()/Load() methods, so the full
// call graph — not just an isolated helper — is under test). Before this,
// disable/enable were bare, result-discarding exec.Command(...).Run() calls
// with no seam to observe them at all, so deleting either line outright left
// the whole suite green.
var launchctlBootout = func(uid string) error {
	watchers.GuardServiceCall("launchctl", []string{"bootout", "gui/" + uid + "/" + launchLabel})
	return exec.Command("launchctl", "bootout", "gui/"+uid+"/"+launchLabel).Run()
}

var launchctlBootstrap = func(uid, plistPath string) ([]byte, error) {
	watchers.GuardServiceCall("launchctl", []string{"bootstrap", "gui/" + uid, plistPath})
	return exec.Command("launchctl", "bootstrap", "gui/"+uid, plistPath).CombinedOutput()
}

var launchctlDisable = func(uid string) error {
	watchers.GuardServiceCall("launchctl", []string{"disable", "gui/" + uid + "/" + launchLabel})
	return exec.Command("launchctl", "disable", "gui/"+uid+"/"+launchLabel).Run()
}

var launchctlEnable = func(uid string) error {
	watchers.GuardServiceCall("launchctl", []string{"enable", "gui/" + uid + "/" + launchLabel})
	return exec.Command("launchctl", "enable", "gui/"+uid+"/"+launchLabel).Run()
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
	_ = launchctlBootout(m.uid)

	// #6044: bootout alone only clears the CURRENT session's loaded job — the
	// plist on disk is untouched, and RunAtLoad fires again at the next
	// login, so a bare bootout is a "stop until next login", not a real stop.
	// `launchctl disable` writes a persistent override that suppresses that
	// automatic relaunch (survives login/reboot), which is what puts macOS on
	// equal footing with systemd's `disable --now` (systemd_linux.go Unload)
	// and schtasks's task deletion (schtasks_windows.go Unload) — both of
	// which already persist past a reboot. Load() below always re-enables
	// before bootstrap, so this has no effect on the ordinary
	// install/start/restart Unload;Load cycle — it only matters for a caller
	// (`grafel stop`) that stops WITHOUT a following Load.
	//
	// The outcome is recorded (not discarded) on lastDisableErr: Unload's own
	// contract stays best-effort (a disable hiccup must not fail
	// ensureLoaded's restart/install path, which re-enables via Load()
	// immediately afterward regardless), but stopService reads this field to
	// decide whether it can honestly report the stop as persistent (#6044
	// review item 3) instead of asserting that unconditionally.
	m.lastDisableErr = launchctlDisable(m.uid)
	return nil
}

func (m *launchdManager) Load() error {
	// Clear any persisted disable — from a prior `grafel stop`, or a stale
	// state — before bootstrap. Without this, RunAtLoad silently does
	// nothing on a disabled service and WaitReady times out with no
	// explanation. See the disable call in Unload for the pairing. Best
	// effort like Unload's own disable: bootstrap immediately below is the
	// real convergence signal, and a failed enable here does not block it.
	_ = launchctlEnable(m.uid)
	out, err := launchctlBootstrap(m.uid, m.plistPath)
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

// restartService is the macOS implementation of Restart: always converges via
// unload→load→wait-ready (launchd bootout→bootstrap), skipping Install's
// "already running" fast path so callers get a genuine restart.
func restartService(opts Options) (StatusInfo, error) {
	sm, err := newServiceManager(opts)
	if err != nil {
		return StatusInfo{}, err
	}
	return restart(context.Background(), sm, defaultReadiness, nil)
}

// stopService is the macOS implementation of Stop: bootout + persistent
// disable, then confirm — by polling the daemon socket, not launchd-domain
// membership — that the daemon is actually down (issue #6044 item 1).
//
// If the daemon is confirmed down but the disable call itself failed
// (m.lastDisableErr), stop does NOT report the persistent-stop success —
// it returns that failure instead (#6044 review item 3): the caller asked
// for a stop that survives reboot/login, so failing to make it persistent is
// a genuine failure of the requested operation, not a detail to gloss over
// in the success message.
func stopService(opts Options) (StatusInfo, error) {
	sm, err := newServiceManager(opts)
	if err != nil {
		return StatusInfo{}, err
	}
	st, err := stopConverge(context.Background(), sm, defaultReadiness, nil)
	if err != nil {
		return st, err
	}
	if lm, ok := sm.(*launchdManager); ok && lm.lastDisableErr != nil {
		return st, fmt.Errorf("daemon stopped, but could not persist the stop across reboot/login "+
			"(launchctl disable failed): %w", lm.lastDisableErr)
	}
	return st, nil
}

// uninstall is the macOS implementation of Uninstall: idempotent teardown.
func uninstall(opts Options) error {
	sm, err := newServiceManager(opts)
	if err != nil {
		return err
	}
	if err := teardown(sm); err != nil {
		return err
	}
	// #6044 follow-up (review item 6): teardown's Unload() persists a
	// `launchctl disable` override (see Unload's doc comment) so a bare
	// `grafel stop` survives reboot. A full uninstall removes the plist
	// entirely, so nothing on disk explains that override afterward — clear
	// it so the label carries no residue past uninstall. Best-effort: this is
	// cleanup, not the operation's success signal (RemoveArtifacts already
	// succeeded by this point), and a fresh `grafel install` would write a
	// new plist and re-enable via Load() regardless.
	if lm, ok := sm.(*launchdManager); ok {
		_ = launchctlEnable(lm.uid)
	}
	return nil
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
