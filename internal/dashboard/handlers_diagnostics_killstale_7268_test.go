package dashboard

// handlers_diagnostics_killstale_7268_test.go — identity gate on the
// POST /api/diagnostics/kill-stale selection predicate (issue #7268).
//
// THIS IS A KILL PATH. isStaleDiagnosticsProc's output is fed straight to
// process.Kill (SIGTERM), so the permissive direction is the dangerous one and
// is scored first here: a selected row that is not our binary signals a
// stranger's process. The conservative direction is pinned too — a fix that
// selects nothing silently disables the endpoint.
//
// The dashboard is a SEPARATE implementation from internal/cli's isStaleProc
// (#7258), so a verdict on one says nothing about the other; these rows are
// asserted against the dashboard's own predicate, not inherited from the CLI
// test.

import "testing"

func TestIsStaleDiagnosticsProc_IdentityGate_7268(t *testing.T) {
	const selfExe = "/usr/local/bin/grafel"

	cases := []struct {
		name string
		exe  string
		ppid int
		want bool
	}{
		// permissive direction — planted violations, must NOT be selected
		{"HEADLINE foreign helper under a grafel-daemon dir",
			"/Users/jane smith/Library/grafel-daemon-helper/bin/helper", 501, false},
		{"foreign binary named daemon-helper", "/Users/jane/grafel/bin/daemon-helper", 501, false},
		{"foreign orphan under /tmp (criterion 1)", "/tmp/grafel-fixtures/bin/fixture-server", 1, false},
		{"foreign esbuild under a grafel project", "/Users/jane/grafel/node_modules/.bin/esbuild", 1, false},
		{"relative path", "grafel-daemon/grafel", 1, false},
		{"bare command name", "grafel", 1, false},

		// conservative direction — genuine stale daemons must STILL be selected
		{"genuine orphaned /tmp daemon", "/tmp/agent-worktree-1/grafel", 1, true},
		{"genuine daemon under a directory named daemon", "/opt/grafel/daemon/bin/grafel", 100, true},
		{"case variations", "/opt/Grafel/Daemon/bin/GRAFEL", 100, true},

		// boundaries
		{"plain second grafel process", "/usr/local/bin/grafel", 100, false},
		{"canonical under /tmp with a live parent", "/tmp/agent-worktree-1/grafel", 4242, false},
		{"self", selfExe, 100, false},
		// The CLI twin rejects /tmpfoo; this predicate used to accept it via
		// exe[:4] == "/tmp". Held here so the two cannot drift apart again.
		{"/tmpfoo orphan is not a /tmp orphan", "/tmpfoo/grafel", 1, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isStaleDiagnosticsProc(tc.exe, tc.ppid, selfExe); got != tc.want {
				t.Errorf("isStaleDiagnosticsProc(%q, ppid=%d) = %v, want %v", tc.exe, tc.ppid, got, tc.want)
			}
		})
	}
}

// TestIsStaleDiagnosticsProc_NeverSelectsForeignBinary_7268 enumerates the cross product of
// the directory shapes that satisfy process.FindByName("grafel") with
// non-grafel basenames, and requires none of them to be SIGTERM-eligible.
func TestIsStaleDiagnosticsProc_NeverSelectsForeignBinary_7268(t *testing.T) {
	const selfExe = "/usr/local/bin/grafel"

	dirs := []string{
		"/Users/jane smith/Library/grafel-daemon-helper/bin",
		"/tmp/grafel-daemon/bin",
		"/tmp",
		"/tmpfoo/grafel",
		"/opt/grafel/daemon",
		"/usr/local/grafel/DAEMON/bin",
		"/home/jane/grafel",
	}
	bases := []string{"helper", "esbuild", "daemon", "daemonize", "grafel-daemon-old", "node", "grafeld"}

	for _, d := range dirs {
		for _, b := range bases {
			for _, ppid := range []int{1, 100, 4242} {
				exe := d + "/" + b
				if isStaleDiagnosticsProc(exe, ppid, selfExe) {
					t.Errorf("selected %q (ppid=%d) for SIGTERM — not a grafel binary", exe, ppid)
				}
			}
		}
	}
}
