package install

import (
	"path/filepath"
	"strings"
	"testing"
)

// writeMinimalState writes a valid install.json into dir and returns its path,
// so RunUninstall has state to read (it returns early when state is nil).
func writeMinimalState(t *testing.T, dir string) string {
	t.Helper()
	statePath := filepath.Join(dir, "install.json")
	if err := WriteState(statePath, NewState(ModeCopy)); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
	return statePath
}

// TestEvaluateDaemonStop_DecisionTable exercises the pure #5277 guard logic for
// every root/isolation combination that matters. This is the heart of the fix:
// the uninstall must STOP the daemon only when the global service belongs to
// the uninstall target's root, and must SKIP (leaving the live daemon running)
// on a root mismatch or under an isolated sandbox home.
func TestEvaluateDaemonStop_DecisionTable(t *testing.T) {
	tests := []struct {
		name        string
		regRoot     string
		regFound    bool
		regErr      bool
		targetRoot  string
		isolated    bool
		wantStop    bool
		reasonHas   string
	}{
		{
			name:       "matching roots → stop",
			regRoot:    "/Users/dev",
			regFound:   true,
			targetRoot: "/Users/dev",
			wantStop:   true,
		},
		{
			name:       "matching roots, isolated → still stop (own daemon)",
			regRoot:    "/tmp/sandbox",
			regFound:   true,
			targetRoot: "/tmp/sandbox",
			isolated:   true,
			wantStop:   true,
		},
		{
			// THE OUTAGE SCENARIO: live daemon serves the real home, uninstall
			// runs from an isolated sandbox home → must NOT stop it.
			name:       "mismatched roots, isolated → skip",
			regRoot:    "/Users/dev",
			regFound:   true,
			targetRoot: "/tmp/agsbx-uninstallsafe",
			isolated:   true,
			wantStop:   false,
			reasonHas:  "!= uninstall target root",
		},
		{
			name:       "mismatched roots, NOT isolated → skip (belongs to other root)",
			regRoot:    "/Users/dev",
			regFound:   true,
			targetRoot: "/Users/other",
			wantStop:   false,
			reasonHas:  "!= uninstall target root",
		},
		{
			name:       "casing/spelling variant of same root → stop",
			regRoot:    "/Users/Dev/",
			regFound:   true,
			targetRoot: "/Users/dev",
			wantStop:   true,
		},
		{
			name:     "no service installed → stop (idempotent no-op)",
			regFound: false,
			wantStop: true,
		},
		{
			name:      "read error → skip (fail closed)",
			regFound:  true,
			regErr:    true,
			wantStop:  false,
			reasonHas: "could not read",
		},
		{
			// Legacy unit with no recorded HOME, isolated home → can't prove
			// ownership, so refuse to touch the global label.
			name:       "unknown recorded root, isolated → skip",
			regRoot:    "",
			regFound:   true,
			targetRoot: "/tmp/sandbox",
			isolated:   true,
			wantStop:   false,
			reasonHas:  "isolated home",
		},
		{
			name:       "unknown recorded root, NOT isolated → stop (primary install)",
			regRoot:    "",
			regFound:   true,
			targetRoot: "/Users/dev",
			isolated:   false,
			wantStop:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateDaemonStop(tt.regRoot, tt.regFound, tt.regErr, tt.targetRoot, tt.isolated)
			if got.Stop != tt.wantStop {
				t.Fatalf("Stop = %v, want %v (reason: %q)", got.Stop, tt.wantStop, got.Reason)
			}
			if tt.reasonHas != "" && !strings.Contains(got.Reason, tt.reasonHas) {
				t.Errorf("reason %q does not contain %q", got.Reason, tt.reasonHas)
			}
			if got.Reason == "" {
				t.Error("decision reason should never be empty")
			}
		})
	}
}

// TestRunUninstall_GuardSkipsForeignDaemon is a white-box test of the wiring:
// it injects a recorded root different from the uninstall target and asserts
// the stop hook is NEVER called and a WARN is emitted — without any real
// launchctl/systemctl invocation.
func TestRunUninstall_GuardSkipsForeignDaemon(t *testing.T) {
	dir := t.TempDir()
	statePath := writeMinimalState(t, dir)

	stopCalled := false
	var warns []string

	res, err := RunUninstall(UninstallOptions{
		StatePath: statePath,
		Yes:       true,
		registeredRootFn: func() (string, bool, error) {
			return "/Users/dev", true, nil // live daemon serves the real home
		},
		targetRootFn:   func() string { return "/tmp/agsbx-sandbox" },
		isolatedHomeFn: func() bool { return true },
		stopDaemonFn:   func() error { stopCalled = true; return nil },
		warnFn:         func(msg string) { warns = append(warns, msg) },
	})
	if err != nil {
		t.Fatalf("RunUninstall: %v", err)
	}
	if stopCalled {
		t.Fatal("stopDaemonFn was called — guard FAILED to protect a foreign daemon")
	}
	if res.DaemonStopped {
		t.Error("DaemonStopped should be false when the stop was skipped")
	}
	if len(warns) == 0 {
		t.Fatal("expected a WARN to be emitted when skipping the daemon stop")
	}
	if !strings.Contains(strings.Join(warns, "\n"), "!= uninstall target root") {
		t.Errorf("WARN did not explain the root mismatch: %v", warns)
	}
}

// TestRunUninstall_GuardStopsOwnDaemon asserts the normal path: a matching root
// still stops the daemon.
func TestRunUninstall_GuardStopsOwnDaemon(t *testing.T) {
	dir := t.TempDir()
	statePath := writeMinimalState(t, dir)

	stopCalled := false
	res, err := RunUninstall(UninstallOptions{
		StatePath: statePath,
		Yes:       true,
		registeredRootFn: func() (string, bool, error) {
			return "/Users/dev", true, nil
		},
		targetRootFn:   func() string { return "/Users/dev" },
		isolatedHomeFn: func() bool { return false },
		stopDaemonFn:   func() error { stopCalled = true; return nil },
		warnFn:         func(string) {},
	})
	if err != nil {
		t.Fatalf("RunUninstall: %v", err)
	}
	if !stopCalled {
		t.Fatal("stopDaemonFn was NOT called for a matching-root uninstall")
	}
	if !res.DaemonStopped {
		t.Error("DaemonStopped should be true after a successful stop")
	}
}
