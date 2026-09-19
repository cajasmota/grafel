//go:build darwin

package process

import (
	"path/filepath"
	"strings"
	"testing"
)

// MEASURED PREMISE for this whole file (darwin 25.6.0, 2026-09-19).
//
// `ps -eo pid,ppid,comm` prints COMM as the LAST column and COMM is the
// executable path WITHOUT arguments. Verified against a process started from
// a directory whose name contains spaces, with an argument:
//
//	$ ps -o pid,ppid,comm= -p 24212
//	24212 24208 /private/tmp/.../my dir with spaces/my probe bin
//	$ ps -o args= -p 24212
//	/private/tmp/.../my dir with spaces/my probe bin sleep
//
// The argument ("sleep") appears in `args` and NOT in `comm`. That is why
// parsePsEo may take the whole remainder of the line as the path: there is no
// trailing column to swallow.
//
// `ps aux` is the OPPOSITE: its COMMAND column is the full argv, arguments
// included (same process: `... /private/tmp/.../my probe bin sleep`). The two
// parsers are therefore NOT symmetric, and TestParsePsAux_TwinIsNotSymmetric
// below pins that asymmetry so nobody "fixes" parsePsAux by copying parsePsEo.

func TestParsePsEo_CommWithSpacesIsNotTruncated(t *testing.T) {
	// The observed real row from the machine that motivated #7259.
	const chrome = "/Applications/Google Chrome.app/Contents/Frameworks/" +
		"Google Chrome Framework.framework/Versions/151.0.7922.175/Helpers/" +
		"Google Chrome Helper (Renderer).app/Contents/MacOS/Google Chrome Helper (Renderer)"
	const grafel = "/Users/jane smith/my worktree/.grafel/bin/grafel"

	out := "  PID  PPID COMM\n" +
		"  138  4080 " + chrome + "\n" +
		" 1207     1 " + grafel + "\n"

	t.Run("chrome", func(t *testing.T) {
		got := parsePsEo([]byte(out), "chrome")
		if len(got) != 1 {
			t.Fatalf("want 1 match, got %d: %+v", len(got), got)
		}
		if got[0].Exe != chrome {
			t.Errorf("Exe truncated:\n got %q\nwant %q", got[0].Exe, chrome)
		}
		if got[0].Name != chrome {
			t.Errorf("Name truncated:\n got %q\nwant %q", got[0].Name, chrome)
		}
	})

	t.Run("grafel_under_a_home_with_a_space", func(t *testing.T) {
		got := parsePsEo([]byte(out), "grafel")
		if len(got) != 1 {
			t.Fatalf("want 1 match, got %d: %+v", len(got), got)
		}
		if got[0].Exe != grafel {
			t.Errorf("Exe truncated:\n got %q\nwant %q", got[0].Exe, grafel)
		}
		if got[0].PID != 1207 || got[0].PPID != 1 {
			t.Errorf("pid/ppid: got %d/%d want 1207/1", got[0].PID, got[0].PPID)
		}
		// The consequence that matters to findCanonicalDaemon (#857, #7211):
		// the basename gate is the thing that decides canonicity, and it is
		// only answerable on an intact path. Truncation yields "jane".
		if base := filepath.Base(got[0].Exe); base != "grafel" {
			t.Errorf("filepath.Base(Exe) = %q, want %q", base, "grafel")
		}
	})
}

// The permissive direction: not truncating must not start swallowing text that
// is not part of the path. `ps -eo pid,ppid,comm` has no column after COMM, so
// the remainder IS the path — but the parser must still consume exactly two
// leading columns, no more and no fewer, whatever the column padding.
func TestParsePsEo_RemainderIsExactlyCommNoMoreNoLess(t *testing.T) {
	cases := []struct {
		name   string
		line   string
		needle string
		want   string
	}{
		{"single_space_padding", "7 1 /a b/c", "/a", "/a b/c"},
		{"wide_right_aligned_padding", "     7      1 /a b/c", "/a", "/a b/c"},
		{"tab_padding", "7\t1\t/a b/c", "/a", "/a b/c"},
		// A comm with no space must be unchanged from the old behaviour.
		{"no_space_comm", "  7     1 /usr/bin/grafel", "grafel", "/usr/bin/grafel"},
		// Row-level TrimSpace already strips edge whitespace; the remainder
		// must not reintroduce it as part of the path.
		{"trailing_padding_is_not_path", "7 1 /a b/c   ", "/a", "/a b/c"},
		// Multiple interior spaces inside the path are part of the path.
		{"double_space_inside_path", "7 1 /a  b/c", "/a", "/a  b/c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parsePsEo([]byte("PID PPID COMM\n"+tc.line+"\n"), tc.needle)
			if len(got) != 1 {
				t.Fatalf("want 1 match, got %d: %+v", len(got), got)
			}
			if got[0].Exe != tc.want {
				t.Errorf("Exe = %q, want %q", got[0].Exe, tc.want)
			}
			if got[0].PID != 7 || got[0].PPID != 1 {
				t.Errorf("pid/ppid = %d/%d, want 7/1", got[0].PID, got[0].PPID)
			}
		})
	}
}

// restAfterFields is documented as a general counterpart to strings.Fields, so
// it is graded directly rather than only through parsePsEo. parsePsEo happens
// to TrimSpace each row before calling it, which leaves the helper's own
// leading-whitespace handling ungraded from that call site (a mutant dropping
// the leading TrimLeft survived the parsePsEo tests) — the first two rows here
// are what grade it.
func TestRestAfterFields(t *testing.T) {
	cases := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"leading_spaces", "   7 1 /a b/c", 2, "/a b/c"},
		{"leading_tabs", "\t\t7 1 /a b/c", 2, "/a b/c"},
		{"n_zero_is_identity_after_trim", "  /a b/c", 0, "/a b/c"},
		{"n_one", "7 /a b/c", 1, "/a b/c"},
		{"exactly_n_fields_has_no_remainder", "7 1", 2, ""},
		{"fewer_than_n_fields", "7", 2, ""},
		{"empty", "", 2, ""},
		{"interior_runs_of_whitespace_are_separators", "7 \t  1 \t /a b/c", 2, "/a b/c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := restAfterFields(tc.in, tc.n); got != tc.want {
				t.Errorf("restAfterFields(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
			}
		})
	}
}

// Rows that cannot yield a path must still be dropped, not turned into a
// bogus one. These pin the direction the widened remainder could regress.
func TestParsePsEo_RejectsUnusableRows(t *testing.T) {
	cases := []string{
		"PID PPID COMM\n7 1\n",         // no COMM column at all
		"PID PPID COMM\nxx 1 /a b/c\n", // non-numeric pid
		"PID PPID COMM\n\n",            // blank line
	}
	// The EMPTY needle is the load-bearing one. strings.Contains(x, "") is
	// true for every x, so it is the only needle that cannot mask a
	// row-rejection failure behind the name match — with any non-empty needle
	// a row that should have been rejected is dropped by the Contains test
	// instead, and the assertion passes for the wrong reason.
	//
	// It is therefore what pins parsePsEo's "no empty-remainder guard is
	// needed: the len(fields) >= 3 test above already establishes ...
	// restAfterFields cannot return \"\" here" comment to the code. Weakening
	// that bound to >= 2 makes the first row parse with an empty Name/Exe, and
	// nothing else in the suite notices.
	for _, needle := range []string{"/a", "grafel", ""} {
		for _, out := range cases {
			if got := parsePsEo([]byte(out), needle); len(got) != 0 {
				t.Errorf("parsePsEo(%q, needle=%q) = %+v, want no matches", out, needle, got)
			}
		}
	}
}

// TWIN VERDICT for parsePsAux (#7259 asks for the same fix here; it would be
// WRONG). `ps aux`'s COMMAND column is the full argv, so the remainder of the
// line is "exe arg arg...", not a path. Taking the remainder would hand
// findCanonicalDaemon a string whose filepath.Base is the LAST ARGUMENT, which
// is strictly worse than a truncated path: the basename gate would then never
// fire for any daemon started with arguments.
//
// argv[0] containing spaces is genuinely unrecoverable from `ps aux` alone, so
// parsePsAux deliberately keeps taking the first whitespace-delimited token of
// COMMAND. This test states that as the intended behaviour so the asymmetry is
// a decision on the record rather than an oversight.
func TestParsePsAux_TwinIsNotSymmetric(t *testing.T) {
	// A real `ps aux` row: USER PID %CPU %MEM VSZ RSS TTY STAT START TIME COMMAND...
	const line = "jorgecajas 24296 0.1 0.0 411831616 4272 ?? S 11:01AM 0:00.00 /tmp/probe/grafel serve --root /x"
	got := parsePsAux([]byte(line+"\n"), "grafel")
	if len(got) != 1 {
		t.Fatalf("want 1 match, got %d: %+v", len(got), got)
	}
	if got[0].PID != 24296 {
		t.Errorf("PID = %d, want 24296", got[0].PID)
	}
	// The argument list must NOT be in Exe.
	if strings.Contains(got[0].Exe, "serve") || strings.Contains(got[0].Exe, "--root") {
		t.Errorf("Exe carries arguments: %q — `ps aux` COMMAND is argv, not a path", got[0].Exe)
	}
	if got[0].Exe != "/tmp/probe/grafel" {
		t.Errorf("Exe = %q, want %q", got[0].Exe, "/tmp/probe/grafel")
	}
	if base := filepath.Base(got[0].Exe); base != "grafel" {
		t.Errorf("filepath.Base(Exe) = %q, want grafel", base)
	}
}

// The ACCEPTED COST of the twin decision, pinned so it is visible: an argv[0]
// that contains a space is still truncated by parsePsAux. `ps aux` is only a
// fallback (FindByName uses it when `ps -eo` fails outright), so this path is
// not the one #7211/#857 run through — but it is not fixed, and a future
// change that can tell argv[0] from argv[1:] should delete this test.
func TestParsePsAux_SpacedArgv0IsStillTruncated_KnownLimitation(t *testing.T) {
	const line = "jorgecajas 42 0.1 0.0 1 1 ?? S 11:01AM 0:00.00 /Users/jane smith/.grafel/bin/grafel serve"
	got := parsePsAux([]byte(line+"\n"), "grafel")
	if len(got) != 1 {
		t.Fatalf("want 1 match, got %d: %+v", len(got), got)
	}
	if got[0].Exe != "/Users/jane" {
		t.Fatalf("parsePsAux behaviour changed: Exe = %q. If argv[0] recovery was "+
			"implemented, delete this test; if the remainder is now joined, that "+
			"is the regression TestParsePsAux_TwinIsNotSymmetric forbids.", got[0].Exe)
	}
}
