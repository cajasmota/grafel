package watchers

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestLaunchdPlist_DeclaresThrottleInterval pins issue #6179 defect 1.
//
// The generated watcher plist declared no ThrottleInterval, so launchd's
// 10-second default applied. On the reporting machine that is 140 labels each
// eligible to relaunch every 10s — ~14 process launches per second, forever,
// which is what produced the unbroken stream of macOS Background Activity
// notifications.
func TestLaunchdPlist_DeclaresThrottleInterval(t *testing.T) {
	body := LaunchdPlist(sample)
	if !strings.Contains(body, "<key>ThrottleInterval</key>") {
		t.Fatalf("plist must declare ThrottleInterval — without it launchd applies its 10s default\n%s", body)
	}
	// The value must actually bound the rate: launchd's own default is 10s, so
	// anything <= 10 buys nothing.
	if ThrottleIntervalSeconds <= 10 {
		t.Fatalf("ThrottleIntervalSeconds = %d; must exceed launchd's 10s default to bound the respawn rate",
			ThrottleIntervalSeconds)
	}
	// It must also be at least the watcher's own default poll interval (30s):
	// relaunching a watcher faster than one duty cycle cannot make progress.
	if ThrottleIntervalSeconds < 30 {
		t.Fatalf("ThrottleIntervalSeconds = %d; must be >= the watcher's 30s default poll interval",
			ThrottleIntervalSeconds)
	}
	want := fmt.Sprintf("<integer>%d</integer>", ThrottleIntervalSeconds)
	if !strings.Contains(body, want) {
		t.Fatalf("plist ThrottleInterval value must render as %s\n%s", want, body)
	}
}

// TestLaunchdPlist_KeepAliveOnlyOnUnsuccessfulExit pins issue #6179 defect 2.
//
// KeepAlive=true is an unconditional respawn contract: launchd relaunches the
// job no matter WHY it exited. `grafel watch` has deliberate exit paths (the
// repo path no longer stats; the consecutive-index-failure ceiling), so an
// unconditional contract converts a diagnosable one-shot failure into a
// permanent respawn loop. KeepAlive must be conditional on SuccessfulExit=false
// so only genuine crashes are respawned.
func TestLaunchdPlist_KeepAliveOnlyOnUnsuccessfulExit(t *testing.T) {
	body := LaunchdPlist(sample)
	if strings.Contains(body, "<key>KeepAlive</key><true/>") ||
		strings.Contains(body, "<key>KeepAlive</key>\n  <true/>") {
		t.Fatalf("KeepAlive must not be unconditionally true — that respawns deliberate exits\n%s", body)
	}

	// Structural: SuccessfulExit must be nested INSIDE the KeepAlive dict. A
	// flat `<key>SuccessfulExit</key><false/>` sitting in the top-level job
	// dict is meaningless to launchd, so a substring check alone is not enough.
	inner, ok := keepAliveDictBody(body)
	if !ok {
		t.Fatalf("KeepAlive must be a <dict>\n%s", body)
	}
	if !strings.Contains(inner, "<key>SuccessfulExit</key>") {
		t.Fatalf("KeepAlive dict must contain SuccessfulExit, got:\n%s", inner)
	}
	if !strings.Contains(inner, "<false/>") || strings.Contains(inner, "<true/>") {
		t.Fatalf("KeepAlive.SuccessfulExit must be <false/> (respawn only on unsuccessful exit), got:\n%s", inner)
	}
}

// keepAliveDictBody returns the raw text between the <dict> that immediately
// follows <key>KeepAlive</key> and its matching </dict>.
func keepAliveDictBody(body string) (string, bool) {
	i := strings.Index(body, "<key>KeepAlive</key>")
	if i < 0 {
		return "", false
	}
	rest := body[i+len("<key>KeepAlive</key>"):]
	open := strings.Index(rest, "<dict>")
	if open < 0 || strings.TrimSpace(rest[:open]) != "" {
		return "", false // KeepAlive's value is not a dict
	}
	rest = rest[open+len("<dict>"):]
	end := strings.Index(rest, "</dict>")
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

// TestLaunchdPlist_WellFormedXML guards against the hand-built dict losing its
// nesting — the plist body is assembled by string concatenation, so a
// mismatched <dict> is a plausible regression, and launchd would reject it at
// bootstrap time with an opaque error.
func TestLaunchdPlist_WellFormedXML(t *testing.T) {
	body := LaunchdPlist(sample)
	dec := xml.NewDecoder(strings.NewReader(body))
	dec.Strict = true
	for {
		_, err := dec.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("plist is not well-formed XML: %v\n%s", err, body)
		}
	}
}

// TestWrite_RewritesStalePlistInPlace covers the migration question in #6179:
// existing installs already have 140 plists on disk carrying the old settings.
// A template-only change helps nobody unless the writer overwrites what is
// already there. Write must replace the file's full contents, not merge or
// skip.
//
// Uses a sandboxed HOME. It never invokes launchctl and never touches the real
// ~/Library/LaunchAgents.
func TestWrite_RewritesStalePlistInPlace(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("launchd plist migration is macOS-only")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := UnitPath(sample)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, home) {
		t.Fatalf("sandbox escape: UnitPath = %q, want it under %q", path, home)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// A pre-#6179 plist: unconditional KeepAlive, no ThrottleInterval.
	stale := "<?xml version=\"1.0\"?>\n<plist version=\"1.0\">\n<dict>\n" +
		"  <key>Label</key><string>" + sample.Label() + "</string>\n" +
		"  <key>RunAtLoad</key><true/>\n" +
		"  <key>KeepAlive</key><true/>\n</dict>\n</plist>\n"
	if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Write(sample)
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("Write returned %q, want %q", got, path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	if strings.Contains(body, "<key>KeepAlive</key><true/>") {
		t.Fatalf("stale unconditional KeepAlive survived the rewrite\n%s", body)
	}
	if !strings.Contains(body, "<key>ThrottleInterval</key>") {
		t.Fatalf("rewritten plist still lacks ThrottleInterval\n%s", body)
	}
}
