package watchers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #6185 R3 (found on round-2 review): '\' is not a control byte, so
// validateUnitFields accepted it, but systemd.syntax(7) treats a line ending
// in '\' as continuing onto the next physical line — the two lines are
// concatenated as if the newline between them were not there. A repo path
// of "/tmp/back\\" renders:
//
//	WorkingDirectory=/tmp/back\
//	Restart=on-failure
//
// which systemd reads as "WorkingDirectory=/tmp/back Restart=on-failure":
// Restart=on-failure silently disappears (removing #6179's crash-recovery
// half) and WorkingDirectory points at a directory that doesn't exist, so
// the unit fails to start. '\' is a legal character in a Linux filename, so
// this is reachable without any other validation catching it.
//
// WorkingDirectory=%s is the only line in the current template where a raw
// (non-%q-quoted) field is the last thing before the newline — ExecStart's
// two fields go through Go's %q, which always closes with an actual '"'
// (it escapes a trailing backslash inside the string as "\\", never leaves
// a bare one at the end), and every other line ends in a literal (")",
// a digit, or a fixed suffix) supplied by the format string itself, not by
// the field. So only a trailing backslash on Repo is reachable.

// TestWrite_RejectsTrailingBackslashInRepo pins the fix: Write refuses a
// Repo ending in '\' rather than silently deleting the next directive.
func TestWrite_RejectsTrailingBackslashInRepo(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("GRAFEL_HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))

	u := Unit{Group: "ok", Repo: "/tmp/back\\", BinPath: "/usr/local/bin/grafel"}
	if path, err := Write(u); err == nil {
		os.Remove(path)
		t.Fatalf("Write(%+v) succeeded, want rejection of the trailing backslash "+
			"(#6185 R3); wrote %s", u, path)
	}
}

// TestSystemdUnit_TrailingBackslashDeletesTheNextDirective demonstrates the
// defect directly against the raw renderer (bypassing Write's guard, as the
// unfixed code would have persisted it) — the render this test inspects is
// exactly what ships on disk if validateUnitFields does not catch it.
func TestSystemdUnit_TrailingBackslashDeletesTheNextDirective(t *testing.T) {
	u := Unit{Group: "ok", Repo: "/tmp/back\\", BinPath: "/usr/local/bin/grafel"}
	body := SystemdUnit(u)
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "WorkingDirectory=") && strings.HasSuffix(line, "\\") {
			return // confirmed: the line-continuation hazard is present in the raw render
		}
	}
	t.Fatalf("expected a WorkingDirectory= line ending in a raw backslash in the unguarded "+
		"render; the test fixture or renderer changed:\n%s", body)
}
