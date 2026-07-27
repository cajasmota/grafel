package sched

// foreground_nice_5954_test.go — OS SCHEDULING PRIORITY is the other half of
// "interactive remains uncapped".
//
// THE HAZARD. Both batch children renice THEMSELVES to +10 at startup
// (cmd/grafel/group_algo.go, cmd/grafel/links_internal.go → sched.NiceSelf),
// unconditionally. So a `grafel reset` the user is blocking on runs its
// group-algo pass below the user's editor, below their browser, and below
// grafel's own un-niced index children. Lifting the GOMAXPROCS cap does not
// touch that.
//
// HOW MUCH WALL TIME THIS BUYS IS UNMEASURED, and this file does not claim any.
// nice weighting bites under oversubscription; these passes are single-threaded
// and the reported box had idle cores, where a single nice+10 thread gets a
// core on demand. The justification here is policy, not throughput: work a
// human is blocking on should not be demoted below the machine's other work,
// and on an idle box the lift costs nothing. If the elapsed time does not move,
// the answer is upstream of priority — see the note in nice_foreground.go.
//
// THE TRAP THIS FILE EXISTS TO CATCH. The renice is issued BY THE CHILD, so a
// parent-side-only fix does not bind — the recurring bug class of this epic: a
// guard that exists, looks correct, and does not bind the path that runs.
// Therefore both halves are pinned:
//
//   - the parent must DELIVER the signal (asserted through a real fork), and
//   - the child must ACT on it (asserted by re-execing this test binary and
//     reading back its real getpriority(2) value — not by trusting a bool).

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// --- parent side: the signal is delivered ------------------------------------

func TestChildEnvCarriesTheForegroundSignal(t *testing.T) {
	fgEnv := groupAlgoChildEnv([]string{"PATH=/usr/bin"}, 4, true)
	if got, _ := lookupEnv(fgEnv, childForegroundEnv); got != "1" {
		t.Errorf("group-algo foreground child env %s = %q, want \"1\" — "+
			"without it the child renices itself to +10 and the user's rebuild runs below their editor",
			childForegroundEnv, got)
	}
	bgEnv := groupAlgoChildEnv([]string{"PATH=/usr/bin"}, 2, false)
	if got, ok := lookupEnv(bgEnv, childForegroundEnv); ok && got == "1" {
		t.Errorf("background group-algo child env %s = %q, want it absent or 0 — "+
			"background children keep nice+10 so the machine stays responsive", childForegroundEnv, got)
	}

	fgEnv = linksChildEnv([]string{"PATH=/usr/bin"}, 4, true)
	if got, _ := lookupEnv(fgEnv, childForegroundEnv); got != "1" {
		t.Errorf("links foreground child env %s = %q, want \"1\"", childForegroundEnv, got)
	}
	bgEnv = linksChildEnv([]string{"PATH=/usr/bin"}, 2, false)
	if got, ok := lookupEnv(bgEnv, childForegroundEnv); ok && got == "1" {
		t.Errorf("background links child env %s = %q, want it absent or 0", childForegroundEnv, got)
	}
}

// An operator's pre-existing GRAFEL_CHILD_FOREGROUND in the daemon's own
// environment must not leak a foreground exemption into every background child.
func TestChildEnvOverridesAnInheritedForegroundSignal(t *testing.T) {
	env := groupAlgoChildEnv([]string{childForegroundEnv + "=1"}, 2, false)
	// lookupEnv returns the LAST occurrence, which is what exec honours.
	if got, _ := lookupEnv(env, childForegroundEnv); got == "1" {
		t.Errorf("a background child inherited %s=1 from the daemon env — "+
			"every background pass would run un-niced", childForegroundEnv)
	}
}

// Real fork: the value the links child is ACTUALLY launched with.
func TestRunSubprocessLinks_ForegroundChildIsToldItIsForeground(t *testing.T) {
	resetForegroundForTest(t)
	out := filepath.Join(t.TempDir(), "fg.txt")
	fakeChildScript(t, `printf 'fg=%s\n' "$`+childForegroundEnv+`" > `+out)

	defer MarkGroupForeground("hot")()
	if err := RunSubprocessLinks(context.Background(), "hot", nil); err != nil {
		t.Fatalf("RunSubprocessLinks: %v", err)
	}
	b, _ := os.ReadFile(out)
	if got := strings.TrimSpace(string(b)); got != "fg=1" {
		t.Fatalf("links child env %q, want fg=1 — the parent never told the child it is foreground", got)
	}

	if err := RunSubprocessLinks(context.Background(), "cold", nil); err != nil {
		t.Fatalf("RunSubprocessLinks: %v", err)
	}
	b, _ = os.ReadFile(out)
	if got := strings.TrimSpace(string(b)); got == "fg=1" {
		t.Fatalf("an unrelated background group's links child was told it is foreground (%q)", got)
	}
}

// --- child side: the child ACTS on it ----------------------------------------

// niceProbeEnv makes this test binary run the probe below instead of the suite.
const niceProbeEnv = "GRAFEL_TEST_NICE_PROBE"

// niceAtStartup is this binary's OS priority BEFORE any test ran. Captured at
// package-var init so the baseline guard below can tell the two causes of a
// niced parent apart: an already-demoted runner (`nice -n 10 go test ./...`, a
// CI worker that demotes jobs), which is nobody's bug and should skip, versus a
// test in this binary renicing the shared process, which disarms the assertion
// and must fail loudly. Without the split the guard would misdiagnose the
// former as the latter.
var niceAtStartup = func() int {
	n, err := strconv.Atoi(itoaPriority())
	if err != nil {
		return 0
	}
	return n
}()

// TestNiceProbeChild is not a test: it is the re-exec entry point. Under the
// probe env it reports its inherited priority, applies the real nice policy to
// itself, and reports the result — so the assertions below read the OS's answer
// rather than the function's return value.
func TestNiceProbeChild(t *testing.T) {
	if os.Getenv(niceProbeEnv) == "" {
		t.Skip("not the re-exec probe")
	}
	pre := itoaPriority()
	applied := NiceSelfUnlessForeground()
	os.Stdout.WriteString("pre=" + pre + " post=" + itoaPriority() +
		" applied=" + strconv.FormatBool(applied) + "\n")
	os.Exit(0)
}

type niceProbe struct {
	pre, post int
	applied   bool
}

func runNiceProbe(t *testing.T, foreground bool) niceProbe {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(exe, "-test.run=TestNiceProbeChild")
	cmd.Env = append(os.Environ(), niceProbeEnv+"=1")
	if foreground {
		cmd.Env = append(cmd.Env, childForegroundEnv+"=1")
	} else {
		cmd.Env = append(cmd.Env, childForegroundEnv+"=0")
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("nice probe: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "pre=") {
			continue
		}
		var p niceProbe
		if _, err := fmt.Sscanf(strings.TrimSpace(line), "pre=%d post=%d applied=%t",
			&p.pre, &p.post, &p.applied); err != nil {
			t.Fatalf("parse probe line %q: %v", line, err)
		}
		return p
	}
	t.Fatalf("probe printed no result:\n%s", out)
	return niceProbe{}
}

// The load-bearing assertion: a child told it is foreground must NOT end up
// niced, and a child not told so MUST. Read back from getpriority(2) in a real
// forked process, because the bug being prevented is precisely a policy that
// computes the right answer and never reaches the syscall.
//
// Everything is expressed relative to the INHERITED priority. A child inherits
// its parent's nice, and nothing can lower it again without privilege, so any
// test in this binary that renices the process in-process would otherwise turn
// this assertion into a tautology (it did — see TestGroupAlgoNiceValue). Rather
// than skip in that case, the inherited baseline is asserted too, so a future
// in-process renice fails loudly here instead of silently disarming this test.
func TestNiceSelfUnlessForeground_RealPriorityInAForkedChild(t *testing.T) {
	if !niceIsSupported() {
		t.Skip("no setpriority on this platform")
	}
	bg := runNiceProbe(t, false)
	fg := runNiceProbe(t, true)

	if fg.pre >= groupAlgoNice {
		if niceAtStartup >= groupAlgoNice {
			t.Skipf("this test binary was launched already niced (%d >= the demotion %d), so a "+
				"foreground child and a background child are indistinguishable by priority. "+
				"Re-run without the external nice to exercise this assertion.",
				niceAtStartup, groupAlgoNice)
		}
		t.Fatalf("the forked child INHERITED nice %d (>= the demotion %d) but this binary started at "+
			"%d — a test in this package reniced the shared process, which makes the foreground and "+
			"background cases indistinguishable and disarms this test",
			fg.pre, groupAlgoNice, niceAtStartup)
	}

	if bg.post == fg.post {
		t.Fatalf("forked child nice is %d whether foreground or not — "+
			"the child self-renices unconditionally, so the foreground signal never binds", bg.post)
	}
	if fg.applied {
		t.Error("foreground child applied the background demotion")
	}
	if fg.post != fg.pre {
		t.Errorf("foreground child nice %d -> %d: a rebuild the user is blocking on "+
			"must not be demoted below their editor", fg.pre, fg.post)
	}
	if !bg.applied {
		t.Error("background child skipped the demotion")
	}
	if bg.post != groupAlgoNice {
		t.Errorf("background child nice = %d, want %d — background children keep the demotion",
			bg.post, groupAlgoNice)
	}
}
