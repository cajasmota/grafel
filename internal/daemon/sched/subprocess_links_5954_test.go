package sched

// subprocess_links_5954_test.go — the cross-repo link child's contract (#5954).
//
// The link pass is the third heavy batch stage the daemon runs and the last one
// that still ran IN-PROCESS, inside the long-lived engine. It materialises the
// whole group union twice (loadAllGraphs for the pass window, then the
// phantom-edge reload) and the engine holds the resulting arena for the rest of
// its life. Forking it gives the OS the pages back on child exit and — the part
// this file pins — finally puts it under the same pacing the other two children
// already have.
//
// group-algo shipped WITHOUT the GODEBUG merge and needed a follow-up to add
// it, which is precisely why the env is asserted here from day one rather than
// left to a measurement to notice.

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestLinksChildEnvSetsMadvDontNeed asserts the constructed child env carries
// the reclaim setting AND the CPU bound. GODEBUG is read once at process start,
// so — unlike GOGC, which the child sets on itself in main() — madvdontneed can
// only be delivered through the environment; asserting on the constructed env
// is the only way to pin it without fork-execing.
func TestLinksChildEnvSetsMadvDontNeed(t *testing.T) {
	env := linksChildEnv([]string{"PATH=/usr/bin", "HOME=/home/x"}, 2)

	godebug, ok := lookupEnv(env, "GODEBUG")
	if !ok {
		t.Fatalf("links child env has no GODEBUG entry: %v", env)
	}
	if !strings.Contains(godebug, madvDontNeedSetting) {
		t.Errorf("GODEBUG = %q, want it to carry %q", godebug, madvDontNeedSetting)
	}
	if got, ok := lookupEnv(env, "GOMAXPROCS"); !ok || got != "2" {
		t.Errorf("GOMAXPROCS = %q (present=%v), want \"2\" — the CPU bound must survive the GODEBUG merge", got, ok)
	}
	if got, ok := lookupEnv(env, "PATH"); !ok || got != "/usr/bin" {
		t.Errorf("PATH = %q (present=%v), want the inherited value preserved", got, ok)
	}
}

// TestLinksChildEnvMergesInheritedGODEBUG mirrors the group-algo contract: an
// operator's other GODEBUG settings are merged, not clobbered, and an explicit
// madvdontneed=0 is left alone.
func TestLinksChildEnvMergesInheritedGODEBUG(t *testing.T) {
	env := linksChildEnv([]string{"GODEBUG=http2debug=1"}, 3)
	godebug, _ := lookupEnv(env, "GODEBUG")
	if !strings.Contains(godebug, "http2debug=1") || !strings.Contains(godebug, madvDontNeedSetting) {
		t.Errorf("GODEBUG = %q, want both the inherited setting and %q", godebug, madvDontNeedSetting)
	}

	env = linksChildEnv([]string{"GODEBUG=madvdontneed=0"}, 1)
	godebug, _ = lookupEnv(env, "GODEBUG")
	if godebug != "madvdontneed=0" {
		t.Errorf("GODEBUG = %q, want an explicit operator madvdontneed=0 left alone", godebug)
	}
}

func TestLinksGOMAXPROCSDefaultAndOverride(t *testing.T) {
	t.Setenv("GRAFEL_LINKS_CPU", "")
	if got := LinksGOMAXPROCS(); got != linksGOMAXPROCSDefault {
		t.Fatalf("default LinksGOMAXPROCS = %d, want %d", got, linksGOMAXPROCSDefault)
	}
	for _, c := range []struct {
		env  string
		want int
	}{
		{"3", 3},
		{"1", 1},
		{"0", linksGOMAXPROCSDefault},
		{"garbage", linksGOMAXPROCSDefault},
		{"-4", linksGOMAXPROCSDefault},
	} {
		t.Setenv("GRAFEL_LINKS_CPU", c.env)
		if got := LinksGOMAXPROCS(); got != c.want {
			t.Errorf("LinksGOMAXPROCS(%q) = %d, want %d", c.env, got, c.want)
		}
	}
}

// fakeChildScript writes an executable shell script and points the links runner
// at it, so the fork/cancel/argv contract can be exercised without running the
// real (multi-minute) pass or the test binary's own main.
func fakeChildScript(t *testing.T, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script child stand-in is POSIX-only")
	}
	path := filepath.Join(t.TempDir(), "fake-links-child.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write fake child: %v", err)
	}
	prev := linksChildBinary
	linksChildBinary = func() (string, error) { return path, nil }
	t.Cleanup(func() { linksChildBinary = prev })
}

// TestRunSubprocessLinks_PassesGroupToChild pins the argv the child is forked
// with. The group name is the ONLY state handed over — everything else the pass
// needs (the group config, each repo's path for links.SetRepoSourcePaths, the
// state dirs it writes into) is re-derived in the child from the registry via
// the inherited GRAFEL_HOME / GRAFEL_DAEMON_ROOT, which is why the inherited
// environment is asserted alongside the arguments.
func TestRunSubprocessLinks_PassesGroupToChild(t *testing.T) {
	out := filepath.Join(t.TempDir(), "argv.txt")
	fakeChildScript(t, `printf '%s\n' "$@" > `+out+`
printf 'root=%s\n' "$GRAFEL_DAEMON_ROOT" >> `+out)
	t.Setenv("GRAFEL_DAEMON_ROOT", "/tmp/some-daemon-root")

	if err := RunSubprocessLinks(context.Background(), "mygroup", nil); err != nil {
		t.Fatalf("RunSubprocessLinks: %v", err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read argv capture: %v", err)
	}
	got := string(b)
	if !strings.Contains(got, "links-internal") {
		t.Errorf("child argv = %q, want the hidden links-internal subcommand", got)
	}
	if !strings.Contains(got, "mygroup") {
		t.Errorf("child argv = %q, want the group name", got)
	}
	if !strings.Contains(got, "root=/tmp/some-daemon-root") {
		t.Errorf("child env = %q, want the daemon root inherited so the child resolves the same registry", got)
	}
}

// TestRunSubprocessLinks_BlocksUntilChildExits is the stage-gate constraint in
// test form. daemonSchedulerLinks runs while the scheduler holds the EXCLUSIVE
// heavy-stage token, released by a defer around the callback — so if the runner
// returned before the child finished, the token would be released with the
// heavy work still running and the gate would stop binding.
func TestRunSubprocessLinks_BlocksUntilChildExits(t *testing.T) {
	fakeChildScript(t, "sleep 1")
	start := time.Now()
	if err := RunSubprocessLinks(context.Background(), "g", nil); err != nil {
		t.Fatalf("RunSubprocessLinks: %v", err)
	}
	if el := time.Since(start); el < 900*time.Millisecond {
		t.Fatalf("returned after %v — must not return before the child exits", el)
	}
}

// TestRunSubprocessLinks_CancelTerminatesChild is the cancellation contract.
// In-process the pass honoured ctx at each heavy boundary; across a fork the
// same cancellation must kill the child — exec.CommandContext's default Cancel
// is cmd.Process.Kill(), i.e. SIGKILL, which is why the stand-in below is a
// bare `sleep` with no trap: nothing in this path may depend on the child
// getting a chance to clean up. The call must return the context error so the
// scheduler can tell cancellation from failure.
func TestRunSubprocessLinks_CancelTerminatesChild(t *testing.T) {
	fakeChildScript(t, "sleep 60")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunSubprocessLinks(ctx, "g", nil) }()

	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled run returned nil error, want a cancellation error")
		}
		if !strings.Contains(err.Error(), "cancel") && !strings.Contains(err.Error(), context.Canceled.Error()) {
			t.Fatalf("err = %v, want it to report cancellation", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child was not terminated by context cancellation")
	}
}

// TestRunSubprocessLinks_ChildFailureIsReported keeps a real failure
// distinguishable from a cancellation — the scheduler logs links_err off this.
func TestRunSubprocessLinks_ChildFailureIsReported(t *testing.T) {
	fakeChildScript(t, "exit 3")
	err := RunSubprocessLinks(context.Background(), "g", nil)
	if err == nil {
		t.Fatal("child exit 3 reported success")
	}
	if strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("err = %v, want a child-exit error, not a cancellation", err)
	}
}
