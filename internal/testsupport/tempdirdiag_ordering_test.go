package testsupport

// tempdirdiag_ordering_test.go — the cleanup-ORDERING mechanism of the #6512
// diagnostic, made observable on macOS and Linux.
//
// The claim the diagnostic rests on is that it runs AFTER testing's deferred
// RemoveAll rather than before it. #6512 already records what a before-cleanup
// snapshot is worth: a goroutine dump registered after t.TempDir() ran
// immediately BEFORE the RemoveAll and saw nothing at all. Getting the order
// wrong therefore reproduces a measurement already known to be useless —
// silently, on a platform none of us can run on demand.
//
// fakeTB reproduces the two properties of testing.T that make the order what
// it is: cleanups run LIFO, and testing registers its RemoveAll cleanup INSIDE
// the first TempDir() call. That is enough to bind the ordering here, without
// Windows and without a real failing RemoveAll.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeTB struct {
	name      string
	base      string
	cleanups  []func()
	logs      []string
	order     []string
	removeAll func(string) error
}

func (f *fakeTB) Helper()      {}
func (f *fakeTB) Name() string { return f.name }

func (f *fakeTB) Cleanup(fn func()) { f.cleanups = append(f.cleanups, fn) }

func (f *fakeTB) Logf(format string, a ...any) {
	f.logs = append(f.logs, fmt.Sprintf(format, a...))
	f.order = append(f.order, "diag")
}

// TempDir mirrors testing.common.TempDir: it registers the removal cleanup at
// CALL time and hands back <base>/001.
func (f *fakeTB) TempDir() string {
	f.Cleanup(func() {
		f.order = append(f.order, "removeall")
		if f.removeAll != nil {
			_ = f.removeAll(f.base)
		}
	})
	return filepath.Join(f.base, "001")
}

// runCleanups drains the stack LIFO, as testing does.
func (f *fakeTB) runCleanups() {
	for i := len(f.cleanups) - 1; i >= 0; i-- {
		f.cleanups[i]()
	}
}

// captureDiagOut redirects the stderr mirror for the duration of a test.
func captureDiagOut(t *testing.T) *strings.Builder {
	t.Helper()
	var b strings.Builder
	prev := diagOut
	diagOut = &b
	t.Cleanup(func() { diagOut = prev })
	return &b
}

// TestDiagnosticRunsAfterTheRemoveAllNotBefore is the crown assertion. It goes
// red if the arming call is moved below t.TempDir() — the one-line edit that
// would quietly reduce this to the pre-cleanup snapshot #6512 already took and
// learned nothing from.
func TestDiagnosticRunsAfterTheRemoveAllNotBefore(t *testing.T) {
	captureDiagOut(t)
	f := &fakeTB{name: "TestResolveMemLimitMB_SettingsOverride", base: plantResidue(t)}
	// A RemoveAll that fails and leaves the tree standing — the #6512 shape.
	f.removeAll = func(string) error { return errors.New("The directory is not empty.") }

	dir := armTempDirDiagnostic(f, "windows")
	if want := filepath.Join(f.base, "001"); dir != want {
		t.Fatalf("helper must hand back the TempDir path unchanged: want %s got %s", want, dir)
	}

	f.runCleanups()

	if got := strings.Join(f.order, ","); got != "removeall,diag" {
		t.Fatalf("want the diagnostic to observe the tree AFTER the removal attempt; cleanup order was %q", got)
	}

	// The report names residue that exists only because RemoveAll failed,
	// which is observable only from a cleanup that ran after it.
	report := strings.Join(f.logs, "\n")
	for _, want := range []string{
		filepath.Join(f.base, "001", "settings.json"),
		"#6512 WINDOWS TEMPDIR CLEANUP DIAGNOSTIC",
		"TestResolveMemLimitMB_SettingsOverride",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report does not mention %q:\n%s", want, report)
		}
	}
}

// TestDiagnosticSilentWhenCleanupSucceeded is the other half: a healthy run
// must add NOTHING. A diagnostic that prints on green builds is exactly the
// noise #6512 warns trains readers to dismiss red Windows legs.
func TestDiagnosticSilentWhenCleanupSucceeded(t *testing.T) {
	mirror := captureDiagOut(t)
	f := &fakeTB{name: "TestHealthy", base: plantResidue(t)}
	f.removeAll = os.RemoveAll // succeeds on POSIX

	armTempDirDiagnostic(f, "windows")
	f.runCleanups()

	if len(f.logs) != 0 {
		t.Fatalf("diagnostic spoke on a successful cleanup: %v", f.logs)
	}
	if mirror.Len() != 0 {
		t.Fatalf("diagnostic mirrored to stderr on a successful cleanup: %s", mirror.String())
	}
}

// TestDiagnosticNotArmedOffWindows binds the platform guard by counting the
// cleanups actually registered — one (testing's own RemoveAll) off Windows,
// two once armed. Asserting on the returned directory alone, as an earlier
// draft of this file did, observes nothing about arming.
func TestDiagnosticNotArmedOffWindows(t *testing.T) {
	captureDiagOut(t)
	for _, goos := range []string{"darwin", "linux", "freebsd"} {
		f := &fakeTB{name: "T", base: plantResidue(t)}
		f.removeAll = func(string) error { return errors.New("would have failed") }
		armTempDirDiagnostic(f, goos)
		if len(f.cleanups) != 1 {
			t.Errorf("goos=%s: want only testing's own RemoveAll cleanup, got %d", goos, len(f.cleanups))
		}
		f.runCleanups()
		if len(f.logs) != 0 {
			t.Errorf("goos=%s: diagnostic spoke on a non-Windows platform: %v", goos, f.logs)
		}
	}

	f := &fakeTB{name: "T", base: plantResidue(t)}
	f.removeAll = func(string) error { return errors.New("failed") }
	armTempDirDiagnostic(f, "windows")
	if len(f.cleanups) != 2 {
		t.Fatalf("goos=windows: want the RemoveAll cleanup plus the armed diagnostic, got %d", len(f.cleanups))
	}
}

// TestDiagnosticMirrorsToStderr pins the second delivery channel. The
// diagnostic has to reach the CI log with no special flag and no re-run;
// `go test -json` consumers have dropped cleanup-phase t.Log output before, so
// the stderr mirror is load-bearing rather than decoration.
func TestDiagnosticMirrorsToStderr(t *testing.T) {
	mirror := captureDiagOut(t)
	f := &fakeTB{name: "T", base: plantResidue(t)}
	f.removeAll = func(string) error { return errors.New("The directory is not empty.") }
	armTempDirDiagnostic(f, "windows")
	f.runCleanups()

	if !strings.Contains(mirror.String(), "#6512 WINDOWS TEMPDIR CLEANUP DIAGNOSTIC") {
		t.Fatalf("diagnostic did not reach stderr:\n%s", mirror.String())
	}
	if !strings.Contains(mirror.String(), filepath.Join(f.base, "001")) {
		t.Fatalf("stderr mirror omits the residual path:\n%s", mirror.String())
	}
}
