package watchers

import (
	"runtime"
	"strings"
	"testing"
)

var sample = Unit{Group: "demo", Repo: "/tmp/test/core", BinPath: "/usr/local/bin/grafel"}

// TestLabelStable pins the exact label bytes. The digest suffix landed with
// #6183 (see Label); this test exists so any further change to the derivation
// is a deliberate edit here, because changing it renames every installed unit.
//
// It pins TWO digests, and the second one is the lesson. Until the slash
// normalisation went in, this test held one constant over sample.Repo — and
// sample.Repo is spelled with forward slashes, which is the form the
// normalisation leaves untouched. So the canary was structurally blind on
// Windows: the derivation changed there, every installed unit was renamed, and
// the constant guarding exactly that did not move. A pinned constant is only a
// canary for the platform whose bytes it happens to describe, so both
// derivations are now pinned over LITERAL strings — which is host-independent
// and therefore checks on all three legs.
func TestLabelStable(t *testing.T) {
	if got := sample.Label(); got != "com.grafel.watcher.demo.core-96d54b5d" {
		t.Fatalf("label: %q", got)
	}
	if got := LegacyOf(sample).Label(); got != "com.grafel.watcher.demo.core" {
		t.Fatalf("legacy label: %q", got)
	}

	// The current derivation hashes the slash-normalised path on every OS.
	if got := digestOf("/tmp/test/core"); got != "96d54b5d" {
		t.Fatalf("current digest derivation moved: %q, want 96d54b5d — every installed unit is renamed", got)
	}
	// The SUPERSEDED derivation hashed filepath.Clean's output, which on
	// Windows is the backslash form. This is the label Windows machines have
	// on disk and in Task Scheduler today; NativeDigestOf must keep reaching
	// it, or the migration cannot name what it has to retire.
	if got := digestOf(`\tmp\test\core`); got != "e419b067" {
		t.Fatalf("superseded digest derivation moved: %q, want e419b067 — "+
			"the Windows migration can no longer name the unit it must retire", got)
	}

	// And that the superseded derivation is wired to those bytes: on Windows it
	// must produce the old label, everywhere else it must collapse onto the
	// current one so the migration built on it is a no-op rather than a
	// deletion of a live unit.
	native := NativeDigestOf(sample).Label()
	if runtime.GOOS == "windows" {
		if native != "com.grafel.watcher.demo.core-e419b067" {
			t.Fatalf("superseded label on windows: %q", native)
		}
	} else if native != sample.Label() {
		t.Fatalf("off windows the superseded label must equal the current one "+
			"(%q vs %q); a difference here means the migration would retire a LIVE unit",
			native, sample.Label())
	}
}

func TestLaunchdPlist(t *testing.T) {
	body := LaunchdPlist(sample)
	for _, want := range []string{
		`<key>Label</key>`,
		`<string>com.grafel.watcher.demo.core-96d54b5d</string>`,
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
