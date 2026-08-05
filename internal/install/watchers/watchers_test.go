package watchers

import (
	"strings"
	"testing"
)

var sample = Unit{Group: "demo", Repo: "/tmp/test/core", BinPath: "/usr/local/bin/grafel"}

func TestLabelStable(t *testing.T) {
	if got := sample.Label(); got != "com.grafel.watcher.demo.core" {
		t.Fatalf("label: %q", got)
	}
}

func TestLaunchdPlist(t *testing.T) {
	body := LaunchdPlist(sample)
	for _, want := range []string{
		`<key>Label</key>`,
		`<string>com.grafel.watcher.demo.core</string>`,
		`<string>/usr/local/bin/grafel</string>`,
		`<string>watch</string>`,
		`<string>/tmp/test/core</string>`,
		`<key>RunAtLoad</key><true/>`,
		// #6179: KeepAlive is now a conditional dict, asserted structurally in
		// plist_respawn_6179_test.go.
		`<key>KeepAlive</key>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("plist missing %q\n%s", want, body)
		}
	}
}

func TestSystemdUnit(t *testing.T) {
	body := SystemdUnit(sample)
	for _, want := range []string{
		"Description=grafel watcher (demo/core)",
		`ExecStart="/usr/local/bin/grafel" watch "/tmp/test/core"`,
		"WorkingDirectory=/tmp/test/core",
		"Restart=on-failure",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("service missing %q\n%s", want, body)
		}
	}
}

func TestSchtasksXML(t *testing.T) {
	body := SchtasksXML(sample)
	for _, want := range []string{
		"<Command>/usr/local/bin/grafel</Command>",
		`<Arguments>watch "/tmp/test/core"</Arguments>`,
		"<LogonTrigger>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("schtasks missing %q\n%s", want, body)
		}
	}
}
