package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/testsupport"
)

// TestHomeDirIgnoresDaemonRoot makes executable the fact that ADR-0017's #745
// amendment got wrong for three months.
//
// The ADR said GRAFEL_DAEMON_ROOT isolates "the socket and registry", and
// AGENTS.md repeated it. HomeDir reads GRAFEL_HOME and nothing else, so
// GRAFEL_DAEMON_ROOT alone leaves the registry — and every path derived from it
// — resolving under the real home while the daemon plane moves. That is the
// #6134 shape, and it is why the #6331 guard refuses GRAFEL_DAEMON_ROOT-only
// environments even though the documents used to bless them.
//
// If this ever starts failing because the daemon root DOES move the registry,
// the guard's daemon-root leg and both documents need revisiting together.
func TestHomeDirIgnoresDaemonRoot(t *testing.T) {
	tmp := testsupport.IsolateHome(t)

	elsewhere := filepath.Join(tmp, "daemon-root")
	t.Setenv("GRAFEL_DAEMON_ROOT", elsewhere)
	_ = os.Unsetenv("GRAFEL_HOME")
	t.Setenv("GRAFEL_HOME", "")

	got, err := HomeDir()
	if err != nil {
		t.Fatalf("HomeDir: %v", err)
	}
	if got == elsewhere || filepath.Clean(got) == filepath.Clean(elsewhere) {
		t.Fatalf("HomeDir followed GRAFEL_DAEMON_ROOT (%q) — ADR-0017's amendment "+
			"and the #6331 daemon-root leg must be revisited together", got)
	}
	if want := filepath.Join(tmp, ".grafel"); filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("HomeDir = %q, want %q (the HOME-derived default, unaffected by the daemon root)", got, want)
	}
}
