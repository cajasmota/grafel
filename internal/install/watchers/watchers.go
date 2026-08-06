// Package watchers generates per-platform unit files that launch
// `grafel watch <repo>` at user login.
//
// We deliberately keep the unit-generation pure: each function returns
// the on-disk text for a unit/plist/scheduled-task. Tests can string-
// compare those bytes without ever needing the surrounding OS.
package watchers

import (
	"crypto/sha256"
	"encoding/hex"
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

	// legacy selects the PRE-#6183 label derivation (basename only, no path
	// digest). It exists for exactly one purpose: naming the already-installed
	// unit that a migration has to boot out and delete. Set it only via
	// LegacyOf — the zero value is always the current derivation, so nothing
	// can accidentally install a legacy-labelled unit.
	legacy bool
}

// Label returns the platform-agnostic label for a unit.
//
// The label is also the unit FILENAME and the OS scheduler's job identity, so
// two units that share a label share a file and a job. Before #6183 the slug
// was filepath.Base(repo) alone, which meant two repos in one group with equal
// basenames — `api`, `web`, `docs`, `infra` across different orgs, near-certain
// on a large fleet — produced one label for two repos. The consequences were
// that only one of the pair was ever actually watched, and that
// ReconcileWatcherUnits never converged: each pass rewrote the shared file to
// repo A's body, found it stale for repo B, rewrote it again, and re-registered
// both, forever.
//
// Label depends on nothing but this unit's own fields. That matters: a
// derivation that disambiguated only "where needed" would have to consult the
// rest of the fleet, and then adding an unrelated repo would silently rename an
// existing unit — the orphaning problem below, repeated on every fleet edit
// rather than once.
func (u Unit) Label() string {
	slug := slugify(u.Repo)
	if u.legacy {
		slug = legacySlugify(u.Repo)
	}
	return fmt.Sprintf("com.grafel.watcher.%s.%s", u.Group, slug)
}

// LegacyOf returns a copy of u whose Label() is the pre-#6183 label.
//
// Migration needs to name the old unit EXACTLY. The alternative — globbing
// com.grafel.watcher.* in the unit directory and guessing which entries are
// stale — cannot distinguish a genuinely orphaned unit from one belonging to a
// group this binary has no config for, and would boot out the latter. Deriving
// the old label from the same (group, repo) the new one comes from names a unit
// that belongs to this repo.
//
// Not STRICTLY so, and the caveat is worth writing down: the legacy and current
// label spaces are not disjoint. A repo literally named `api-3f9c1e07` has the
// legacy label `…watcher.g.api-3f9c1e07`, which is also the current label of a
// repo named `api` whose path digests to 3f9c1e07. The MigrateLegacyUnit guard
// below only catches the case where those two are the SAME unit. Cross-repo it
// is ~2^-32 per pair within a group and requires an adversarially named repo;
// the alternative (a separator no path component may contain) does not exist,
// since the slug maps every non-alphanumeric byte to '-'.
func LegacyOf(u Unit) Unit {
	u.legacy = true
	return u
}

// LegacyUnitPath returns the path a pre-#6183 unit for u occupies on this OS.
func LegacyUnitPath(u Unit) (string, error) {
	return UnitPath(LegacyOf(u))
}

// MigrateLegacyUnit removes the pre-#6183 unit for u, if one is installed.
//
// Changing the label orphans every unit already on disk: the old file stays
// there and stays LOADED under the old identity, while the new label registers
// alongside it. Without this the fix would leave a user with two watchers per
// repo — strictly worse than the collision it repairs. So the old job is booted
// out of the scheduler first (while the file it was bootstrapped from still
// exists — on launchd the file is what a bootout names) and only then deleted.
//
// newLoader is called ONLY when there is actually a legacy unit to remove. That
// preserves ReconcileWatcherUnits' property that an up-to-date machine
// constructs no Loader and issues no launchctl at all (#6179).
//
// Idempotent: with no legacy file present it stats one path and returns ("",
// nil) — no loader, no writes. It returns the removed path when it did work.
func MigrateLegacyUnit(u Unit, newLoader func() Loader) (string, error) {
	legacy := LegacyOf(u)
	legacyPath, err := UnitPath(legacy)
	if err != nil {
		return "", err
	}
	currentPath, err := UnitPath(u)
	if err != nil {
		return "", err
	}
	// Defensive: if the derivations ever coincide, removing the "legacy" file
	// would delete the live unit.
	if legacyPath == currentPath {
		return "", nil
	}
	if _, err := os.Stat(legacyPath); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if newLoader != nil {
		if l := newLoader(); l != nil {
			// Tolerate errors: "not loaded" already satisfies the desired
			// absent state, and a scheduler that refuses must not stop the
			// stale file from being deleted.
			_ = l.Unload(legacy)
		}
	}
	if err := os.Remove(legacyPath); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return legacyPath, nil
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
	loader := newLoader()
	// Deregister from the OS scheduler before deleting the file so the OS does
	// not try to relaunch a missing binary. Errors are tolerated: "not loaded"
	// already satisfies the desired absent state.
	_ = loader.Unload(u)
	_ = Remove(u)
	// Also clear any pre-#6183 unit for this repo. Cleanup is the only place
	// that runs when a repo stops being registered, so if it ignored the legacy
	// label an old-scheme plist would survive the group's removal, stay loaded,
	// and keep relaunching a watcher for a repo grafel no longer knows about —
	// with nothing left in the registry to derive its name from later.
	_ = loader.Unload(LegacyOf(u))
	_ = Remove(LegacyOf(u))
}

// newLoader is the Loader constructor Cleanup uses. It is a package var so
// tests can observe deregistrations without a real launchd/systemd session:
// darwin's Unload probes `launchctl list` before booting out and short-circuits
// when the label is not loaded, which it never is under a sandboxed HOME, so a
// stubbed command runner cannot show which label was named.
var newLoader = NewLoader

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

// pathDigestLen is how many hex characters of the path digest go into a slug.
//
// 8 hex chars is 32 bits. Against the birthday bound that is a ~1-in-2.7-million
// chance of one accidental collision across a 140-repo group, and it keeps the
// label short enough that the basename in front of it is still what the eye
// reads: com.grafel.watcher.g.api-3f9c1e07.
const pathDigestLen = 8

// slugify produces a label-safe, path-unique slug.
//
// basename + short digest of the full path, not the digest alone: the label is
// the plist filename a user greps for and the identity `launchctl list` prints,
// and a fleet of bare hashes would be a real usability regression. The digest
// is over filepath.Clean of the whole path, so /x/api and /y/api differ.
func slugify(s string) string {
	return legacySlugify(s) + "-" + pathDigest(s)
}

// pathDigest returns the first pathDigestLen hex chars of SHA-256 over the
// cleaned path.
//
// It is a function of the STRING the registry holds, not of the directory that
// string names. filepath.Clean normalises separators and . / .. — `/x/api/`,
// `/x/./api`, `/x/y/../api` and `/x/api//` all collapse to one label — but it
// does not touch the filesystem, so two spellings that resolve to the same
// directory still produce two labels:
//
//   - Case variants on a case-insensitive volume: on APFS `/X/Api` and `/x/api`
//     are one directory and now two labels. The pre-#6183 slug lowercased the
//     basename and collided them into one, so this is a behaviour change, in the
//     direction of more units rather than fewer.
//   - Symlinked aliases: no EvalSymlinks, deliberately — resolving would make
//     the label depend on filesystem state at derivation time, so a moved or
//     broken symlink would silently rename an installed unit, which is the
//     orphaning problem this whole change exists to avoid paying twice.
//
// In both cases a repo registered twice under different spellings gets two
// watcher units rather than one. That is wasteful, not incorrect, and it is
// what registering the same repo twice already meant elsewhere in grafel.
func pathDigest(s string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(s)))
	return hex.EncodeToString(sum[:])[:pathDigestLen]
}

// legacySlugify is the pre-#6183 slug: filepath.Base, lowercased, with every
// other byte mapped to '-'. It is retained ONLY so MigrateLegacyUnit can name
// the units this scheme installed. Do not use it for new units — it is the
// collision described on Label.
func legacySlugify(s string) string {
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
