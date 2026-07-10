package daemon

import (
	"os"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/indexstate"
	"github.com/cajasmota/grafel/internal/statusfile"
)

// TestWriteRepoStatusFile_WritesReadableSchema is the RED test for the
// #5725/#5729-W1 engine-side status-file writer: the daemon must write a
// statusfile.File for a repo whose fields a poll-safe reader (grafel status
// --json / a statusline) can consume WITHOUT any daemon RPC.
func TestWriteRepoStatusFile_WritesReadableSchema(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())

	repo := t.TempDir()

	// Publish a live in-flight index state for this repo so the writer's
	// "indexing" bit reflects real scheduler state, not just disk.
	indexstate.SetRepoStates([]indexstate.RepoState{
		{Path: repo, State: indexstate.StateIndexing, HeadRef: "main"},
	})
	t.Cleanup(func() { indexstate.SetRepoStates(nil) })

	writeRepoStatusFile(repo, nil)

	got, err := statusfile.Read(repo)
	if err != nil {
		t.Fatalf("statusfile.Read: %v", err)
	}
	if got.EnginePID != os.Getpid() {
		t.Errorf("EnginePID = %d, want %d", got.EnginePID, os.Getpid())
	}
	if got.HeartbeatAt.IsZero() {
		t.Error("HeartbeatAt should be stamped")
	}
	if time.Since(got.HeartbeatAt) > time.Minute {
		t.Errorf("HeartbeatAt too old: %v", got.HeartbeatAt)
	}
	if got.Version == "" {
		t.Error("Version should be populated")
	}
	if got.RepoPath != repo {
		t.Errorf("RepoPath = %q, want %q", got.RepoPath, repo)
	}
	if !got.Indexing {
		t.Error("Indexing should be true — scheduler reports this repo as StateIndexing")
	}
}

// TestOnRepoStatesChanged_TriggersStatusFileRefresh proves the daemon's single
// serialized statusWriter (startStatusWriter) wires
// indexstate.SetOnRepoStatesChanged so a scheduler state transition (index
// start/complete) refreshes the status file promptly via the coalescing notify
// hook, not just on the next periodic heartbeat tick. A very long heartbeat
// interval ensures the file can only appear because of the state-change
// trigger, not the ticker.
func TestOnRepoStatesChanged_TriggersStatusFileRefresh(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())

	repo := t.TempDir()
	stop := startStatusWriter(func() []string { return []string{repo} }, time.Hour, nil)
	t.Cleanup(stop)

	// startStatusWriter writes once immediately at startup; clear that file so
	// the assertion below can only pass because of the state-change trigger.
	if p, err := statusfile.PathFor(repo); err == nil {
		// Poll briefly for the startup write, then remove it.
		for i := 0; i < 100; i++ {
			if _, rerr := statusfile.Read(repo); rerr == nil {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		_ = os.Remove(p)
	}

	indexstate.SetRepoStates([]indexstate.RepoState{
		{Path: repo, State: indexstate.StateIndexing},
	})
	t.Cleanup(func() { indexstate.SetRepoStates(nil) })

	// The notify hook fires in its own goroutine (see indexstate.SetRepoStates)
	// and coalesces into the writer — poll briefly for the file to reappear.
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if _, err := statusfile.Read(repo); err == nil {
			return
		} else {
			lastErr = err
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("status file was not written after a repo-state change: %v", lastErr)
}
