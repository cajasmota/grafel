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

// TestOnRepoStatesChanged_TriggersStatusFileRefresh proves the daemon wires
// indexstate.SetOnRepoStatesChanged so a scheduler state transition (index
// start/complete) refreshes the status file promptly, not just on the next
// periodic heartbeat tick.
func TestOnRepoStatesChanged_TriggersStatusFileRefresh(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())

	repo := t.TempDir()
	registerStatusFileHook(func() []string { return []string{repo} }, nil)
	t.Cleanup(func() { indexstate.SetOnRepoStatesChanged(nil) })

	indexstate.SetRepoStates([]indexstate.RepoState{
		{Path: repo, State: indexstate.StateIndexing},
	})
	t.Cleanup(func() { indexstate.SetRepoStates(nil) })

	// The hook fires in its own goroutine (see indexstate.SetRepoStates) —
	// poll briefly for the file to land rather than sleeping a fixed amount.
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
