package install

import (
	"strings"
	"testing"
)

// fakeGrafelWatcherSummaryScript stands in for a real `grafel install
// --refresh-state` that found stale watcher units and repaired them.
const fakeGrafelWatcherSummaryScript = `#!/usr/bin/env bash
echo "$@" >> "$GRAFEL_ARG_LOG"
echo "install state already current (/tmp/bin/grafel)"
echo "✓ watcher units refreshed: 140 rewritten, 140 re-registered, 0 already current"
echo "  macOS may show up to 140 Background Items notifications while these re-register"
exit 0
`

// fakeGrafelSilentScript stands in for the steady state: nothing stale, nothing
// to say.
const fakeGrafelSilentScript = `#!/usr/bin/env bash
echo "$@" >> "$GRAFEL_ARG_LOG"
echo "install state already current (/tmp/bin/grafel)"
exit 0
`

// TestInstallSh_RecordInstallState_SurfacesWatcherSummary pins #6179 F1-b on
// the curl-installer side.
//
// install.sh discarded the refresh's output entirely (`>/dev/null 2>&1`). On a
// machine whose watcher units are all stale, that command re-registers every
// one of them — hundreds of launchctl invocations and a wave of macOS
// Background Items notifications — immediately after the user upgrades, with
// nothing on screen to explain it. The only available reading is that the
// upgrade caused the storm.
func TestInstallSh_RecordInstallState_SurfacesWatcherSummary(t *testing.T) {
	_, combined, err := runRecordInstallState(t, fakeGrafelWatcherSummaryScript)
	if err != nil {
		t.Fatalf("record_install_state: %v\noutput:\n%s", err, combined)
	}
	if !strings.Contains(combined, "watcher units refreshed") {
		t.Errorf("the installer swallowed the watcher-unit summary; the user sees a launchctl "+
			"burst with no explanation (#6179 F1-b)\noutput:\n%s", combined)
	}
	if !strings.Contains(combined, "Background Items") {
		t.Errorf("the notification warning must survive to the installer's output too\noutput:\n%s", combined)
	}
	// The install-state bookkeeping line is not the installer's business — it
	// is why the output was piped to /dev/null in the first place.
	if strings.Contains(combined, "install state already current") {
		t.Errorf("record_install_state should surface only the watcher summary, not the "+
			"install-state bookkeeping\noutput:\n%s", combined)
	}
}

// TestInstallSh_RecordInstallState_SilentWhenNothingStale: the steady state
// must stay quiet. A one-liner installer that starts narrating bookkeeping on
// every run is its own regression.
func TestInstallSh_RecordInstallState_SilentWhenNothingStale(t *testing.T) {
	_, combined, err := runRecordInstallState(t, fakeGrafelSilentScript)
	if err != nil {
		t.Fatalf("record_install_state: %v\noutput:\n%s", err, combined)
	}
	if strings.TrimSpace(combined) != "" {
		t.Errorf("record_install_state must print nothing when no watcher units were "+
			"refreshed, got:\n%s", combined)
	}
}

// TestInstallSh_RecordInstallState_StillBestEffortOnFailure re-asserts the
// existing contract now that output is no longer blanket-discarded: a failing
// refresh must still not abort the installer.
func TestInstallSh_RecordInstallState_StillBestEffortOnFailure(t *testing.T) {
	_, combined, err := runRecordInstallState(t, fakeGrafelArgLogFailScript)
	if err != nil {
		t.Fatalf("a failing refresh must not abort the installer: %v\noutput:\n%s", err, combined)
	}
}
