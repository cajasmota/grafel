package watchers

import (
	"strings"
	"testing"
)

var sample = Unit{Group: "demo", Repo: "/tmp/test/core", BinPath: "/usr/local/bin/archigraph"}

func TestLabelStable(t *testing.T) {
	if got := sample.Label(); got != "com.archigraph.watcher.demo.core" {
		t.Fatalf("label: %q", got)
	}
}

func TestLaunchdPlist(t *testing.T) {
	body := LaunchdPlist(sample)
	for _, want := range []string{
		`<key>Label</key>`,
		`<string>com.archigraph.watcher.demo.core</string>`,
		`<string>/usr/local/bin/archigraph</string>`,
		`<string>watch</string>`,
		`<string>/tmp/test/core</string>`,
		`<key>RunAtLoad</key><true/>`,
		`<key>KeepAlive</key><true/>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("plist missing %q\n%s", want, body)
		}
	}
}

func TestSystemdUnit(t *testing.T) {
	body := SystemdUnit(sample)
	for _, want := range []string{
		"Description=archigraph watcher (demo/core)",
		`ExecStart="/usr/local/bin/archigraph" watch "/tmp/test/core"`,
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
		"<Command>/usr/local/bin/archigraph</Command>",
		`<Arguments>watch "/tmp/test/core"</Arguments>`,
		"<LogonTrigger>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("schtasks missing %q\n%s", want, body)
		}
	}
}
