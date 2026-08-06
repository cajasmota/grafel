package cli

// Split out of watcher_fleet_6180_test.go, which is otherwise fully portable.
//
// This test drives watchers.SetLaunchctlRunnerForTest, a seam that exists only
// in loader_darwin.go, so the file it lived in did not COMPILE on linux or
// windows — its `runtime.GOOS != "darwin"` t.Skip guarded the run but not the
// build, which is what `go vet ./...` reports. The `_darwin_test.go` suffix is
// this package's existing convention for the same situation (see
// watcher_ctl_detect_darwin_test.go), and it makes the t.Skip redundant, so it
// is gone: the file is now excluded from linux/windows builds entirely.
//
// The assertion is genuinely darwin-only — it is about launchctl's
// disable/enable verbs and their ordering relative to bootstrap. There is no
// systemd or schtasks equivalent to assert, so nothing is lost off macOS.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/install/watchers"
)

// TestDarwinWatcherUnloadIsPersistent: on macOS, `launchctl bootout` only
// clears the CURRENT login session — the plist stays in ~/Library/LaunchAgents
// and RunAtLoad fires again at next login. The daemon's own service already
// pairs bootout with `launchctl disable` for exactly this reason (#6044,
// internal/daemon/service/launchd_darwin.go Unload). The per-repo watcher
// loader must do the same, and Load must re-enable.
func TestDarwinWatcherUnloadIsPersistent(t *testing.T) {
	var calls []string
	restore := watchers.SetLaunchctlRunnerForTest(func(args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		return nil, nil
	})
	t.Cleanup(restore)

	home := t.TempDir()
	t.Setenv("HOME", home)
	u := watchers.Unit{Group: "grp", Repo: filepath.Join(home, "alpha"), BinPath: "/usr/local/bin/grafel"}
	if _, err := watchers.Write(u); err != nil {
		t.Fatalf("write unit: %v", err)
	}

	if err := watchers.NewLoader().Unload(u); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "bootout") {
		t.Fatalf("Unload did not bootout:\n%s", joined)
	}
	if !strings.Contains(joined, "disable") {
		t.Fatalf("Unload did not persist the stop (no `launchctl disable`):\n%s", joined)
	}

	calls = nil
	if err := watchers.NewLoader().Load(u); err != nil {
		t.Fatalf("Load: %v", err)
	}
	joined = strings.Join(calls, "\n")
	if !strings.Contains(joined, "enable") {
		t.Fatalf("Load did not clear a persisted disable (no `launchctl enable`):\n%s", joined)
	}
	ei := strings.Index(joined, "enable")
	bi := strings.Index(joined, "bootstrap")
	if bi >= 0 && ei > bi {
		t.Fatalf("`launchctl enable` must precede bootstrap:\n%s", joined)
	}
}
