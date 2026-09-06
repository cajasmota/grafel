package daemon_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/walk"
	"github.com/cajasmota/grafel/internal/daemon/watch"
	"github.com/cajasmota/grafel/internal/gitmeta"
	"github.com/cajasmota/grafel/internal/indexer/diff"
)

// bootWatcher boots a daemon with the given poll setting and returns the live
// Watcher the engine plane constructed.
func bootWatcher(t *testing.T, poll bool) *watch.Watcher {
	t.Helper()
	return bootWatcherCfg(t, poll, time.Hour)
}

func bootWatcherCfg(t *testing.T, poll bool, iv time.Duration) *watch.Watcher {
	t.Helper()
	isolateDaemonEnv(t)
	layout, err := daemon.DefaultLayout()
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	if err := daemon.EnsureLayout(layout); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	ready := make(chan *watch.Watcher, 1)
	cfg := daemon.Config{
		Layout:              layout,
		GroupsForRepo:       func(string) []string { return nil },
		SchedulerIndex:      func(context.Context, string, string) error { return nil },
		SchedulerLinks:      func(context.Context, string) error { return nil },
		SchedulerGroupAlgo:  func(context.Context, string) error { return nil },
		ChangeDetectionPoll: poll,
		ChangePollInterval:  iv,
		OnWatcherReady: func(w *watch.Watcher) {
			select {
			case ready <- w:
			default:
			}
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = daemon.Run(ctx, cfg) }()

	select {
	case w := <-ready:
		return w
	case <-time.After(30 * time.Second):
		t.Fatal("watcher never became ready")
		return nil
	}
}

// bootWatcherInterval is bootWatcher with a caller-chosen cadence.
func bootWatcherInterval(t *testing.T, poll bool, iv time.Duration) *watch.Watcher {
	t.Helper()
	return bootWatcherCfg(t, poll, iv)
}

func pollModeRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"a", "b", "a/c", "a/c/d"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "a", "x.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// #6932 arm A, end to end through the engine plane: with
// Config.ChangeDetectionPoll set, every existing subscribe path (they all go
// through Watcher.AddRepo) takes ZERO fs watch descriptors. That is the entire
// point of the mode — on Linux each of those descriptors is one inotify watch
// out of a per-UID, host-level, non-namespaced pool a container cannot raise.
func TestEnginePlane_PollModeTakesNoWatchDescriptors(t *testing.T) {
	w := bootWatcher(t, true)
	repo := pollModeRepo(t)
	n, err := w.AddRepo(repo)
	if err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if n != 0 {
		t.Fatalf("poll mode subscribed %d directories; must be 0", n)
	}
	if _, dirs, _, _, _ := w.Stats(); dirs != 0 {
		t.Fatalf("poll mode holds %d directory subscriptions; must be 0", dirs)
	}
	// The configured cadence must reach the poller. bootWatcher asks for an
	// hour, which is nothing like watch.DefaultChangePollInterval, so a
	// dropped Config.ChangePollInterval cannot pass for the default.
	cp, ok := w.ChangeDelegate().(*watch.ChangePoller)
	if !ok {
		t.Fatalf("delegate is %T, want *watch.ChangePoller", w.ChangeDelegate())
	}
	if got := cp.Interval(); got != time.Hour {
		t.Fatalf("poll interval = %v, want the configured 1h", got)
	}
}

// The control. Without the flag the engine plane is byte-for-byte the
// pre-#6932 daemon and still subscribes — otherwise the assertion above is
// measuring a broken watcher rather than a working mode.
func TestEnginePlane_DefaultModeStillSubscribes(t *testing.T) {
	w := bootWatcher(t, false)
	repo := pollModeRepo(t)
	n, err := w.AddRepo(repo)
	if err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if n == 0 {
		t.Fatal("default (fsnotify) mode subscribed 0 directories — the control is vacuous")
	}
	if d := w.ChangeDelegate(); d != nil {
		t.Fatalf("default mode installed a change delegate: %T", d)
	}
}

// gitRepoWithManifest builds a real git repo and seeds the manifest at the
// per-(repo, ref) state dir the engine plane's StateDir closure resolves to.
// It returns the repo path and that state dir.
func gitRepoWithManifest(t *testing.T) (string, string) {
	t.Helper()
	repo := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "alpha.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-q", "-m", "init")

	ref := gitmeta.Capture(repo).Ref
	if ref == "" {
		t.Fatal("fixture premise broken: no ref captured for the repo")
	}
	state := daemon.StateDirForRepoRef(repo, ref)
	if state == "" {
		t.Fatal("StateDirForRepoRef returned empty")
	}
	// Premise for the V2 grade below: the per-ref dir must differ from the
	// ref-less one, or a mutant that drops the ref would land in the same place.
	if refless := daemon.StateDirForRepoRef(repo, ""); refless == state {
		t.Fatalf("fixture premise broken: ref and ref-less state dirs are identical (%s)", state)
	}
	files, _, err := walk.WalkRepo(repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := diff.LoadManifest(state)
	changed, _ := diff.Filter(repo, files, m)
	diff.UpdateManifestScoped(repo, changed, files, m)
	if err := diff.SaveManifestAtCommit(state, m, "", ""); err != nil {
		t.Fatal(err)
	}
	return repo, state
}

// #6932 review, V1 + V2: the two ends of the boot wiring. Asserting that poll
// mode subscribes 0 directories and that the interval reached the poller says
// nothing about whether a cycle EVER RUNS — under a mutant that deletes
// changePoller.Start(), poll mode subscribes nothing AND detects nothing, i.e.
// the daemon runs with no change detector at all: the exact silent
// half-failure #6932 exists to remove. And under a mutant that drops the ref
// from the StateDir closure, the poller sweeps the wrong manifest.
//
// So this asserts the observable consequence — a detector that detects — end
// to end through daemon.Run, on a real repo with a real seeded manifest.
func TestEnginePlane_PollModeActuallyDetects(t *testing.T) {
	w := bootWatcherInterval(t, true, 25*time.Millisecond)
	repo, _ := gitRepoWithManifest(t)
	if _, err := w.AddRepo(repo); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	cp, ok := w.ChangeDelegate().(*watch.ChangePoller)
	if !ok {
		t.Fatalf("delegate is %T", w.ChangeDelegate())
	}

	// Quiescent first: no submission may happen before there is a change.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && cp.Cycles() < 3 {
		time.Sleep(20 * time.Millisecond)
	}
	if cp.Cycles() < 3 {
		t.Fatalf("the poll loop never ran: cycles=%d (is changePoller.Start() wired?)", cp.Cycles())
	}
	if n := cp.Submits(); n != 0 {
		t.Fatalf("poller submitted %d reindex requests on a quiescent repo", n)
	}

	if err := os.WriteFile(filepath.Join(repo, "alpha.go"), []byte("package a\n\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && cp.Submits() == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if cp.Submits() == 0 {
		t.Fatalf("poll mode detected nothing after a real edit (cycles=%d) — the daemon is running with no change detector", cp.Cycles())
	}
}
