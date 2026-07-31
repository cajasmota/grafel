package daemon

// guard_6053_liveness_test.go — no private process-liveness probes anywhere
// under internal/.
//
// #6053: reaper.go carried its own pidAliveProbe, a signal-0 existence check
// whose own doc comment said "portable on darwin/linux". On Windows
// (*os.Process).Signal returns EWINDOWS for anything but Kill, so that probe
// answered "dead" for EVERY live pid. watchreg.Sweep checks !Alive BEFORE the
// ownership comparison, so on Windows every registered `grafel watch` entry
// was classified dead and dropped each sweep — the daemon lost its whole
// watcher inventory, and the fail-closed orphan contract #5933 established was
// unreachable dead code there. internal/process.IsAlive has had a correct
// OpenProcess/GetExitCodeProcess implementation all along; pidfile.go's
// pidAlive already used it, the reaper did not.
//
// On unix the two probes differ in exactly one case (EPERM — see
// reaper_eperm_unix_test.go) and agree on everything a unix test can easily
// construct, so no behavioural test here catches a reintroduction on the
// platform CI mostly runs. That is how the second copy survived review. This
// guard catches it.
//
// Scope: the WHOLE of internal/, walked from the module root, not just
// package daemon's own directory. daemon/{watch,watchreg,sched,service,...}
// are at least as likely a place for a third copy to appear, and the original
// bug was one package away from the correct helper already. internal/process
// is the one exemption: the platform split belongs there and nowhere else.

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestNoPrivateSignalZeroLivenessProbe(t *testing.T) {
	root := moduleRoot(t)
	internal := filepath.Join(root, "internal")

	// The signal-ZERO idiom in the forms it appears in Go code.
	//
	// Scoped to signal 0 on purpose: syscall.Kill with a REAL signal is
	// legitimate and pre-existing in this tree (sched/nice_unix.go and
	// supervise_unix.go SIGKILL a process group, on //go:build !windows
	// files). Only the zero-signal EXISTENCE PROBE is the portability defect,
	// so a broader "no raw Kill" rule would be three false positives and would
	// be deleted by the next person rather than obeyed.
	//
	// Patterns are compiled from fragments so this file — which lives under
	// internal/ and is therefore in scope — holds no literal occurrence and
	// cannot pass merely by exempting itself.
	sig := `syscall\.Signal\(`
	pats := []*regexp.Regexp{
		regexp.MustCompile(sig + `0x?0*\)`), // p.Signal(syscall.Signal(0))
		regexp.MustCompile(`(?:syscall|unix)\.Kill\([^,]+,\s*` + // syscall.Kill(pid, 0)
			`(?:0x?0*|` + sig + `0x?0*\))\)`),
	}

	exempt := filepath.Join(internal, "process")

	scanned := 0
	sawReaper := false
	err := filepath.WalkDir(internal, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == exempt || d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		scanned++
		rel, _ := filepath.Rel(root, path)
		if rel == filepath.Join("internal", "daemon", "reaper.go") {
			sawReaper = true
		}
		for i, line := range strings.Split(string(b), "\n") {
			// Match code, not prose: pidfile.go's doc comment legitimately
			// spells out the portable idiom. Cutting at "//" can also clip a
			// "//" inside a string literal, which would only make the guard
			// more permissive on that one line — acceptable, since a signal-0
			// probe hidden in a string is not the reintroduction this guards.
			if c := strings.Index(line, "//"); c >= 0 {
				line = line[:c]
			}
			for _, re := range pats {
				if re.MatchString(line) {
					t.Errorf("%s:%d contains a signal-0 liveness probe (%q) — it always reports "+
						"DEAD on Windows. Use process.IsAlive; the platform split lives in "+
						"internal/process (#6053).", rel, i+1, strings.TrimSpace(line))
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/: %v", err)
	}

	// A guard that scanned nothing, or that missed the very file the defect
	// was in, proves nothing.
	if scanned < 100 || !sawReaper {
		t.Fatalf("guard scanned %d production files under internal/ and saw reaper.go = %v "+
			"— the scan is not reaching the tree it claims to cover", scanned, sawReaper)
	}
}

// moduleRoot walks up from the test's working directory to the directory
// holding go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found above the test working directory")
		}
		dir = parent
	}
}
