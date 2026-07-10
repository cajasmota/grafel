package statusfile_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/statusfile"
)

// TestWriteRead_Roundtrip is the RED test for the #5729-W1 status-plane
// heartbeat file: the engine (writer) and a poll-safe reader (CLI/statusline)
// must agree on a typed schema persisted atomically to disk.
func TestWriteRead_Roundtrip(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())

	repo := "/some/repo/path"
	want := &statusfile.File{
		EnginePID:     4242,
		HeartbeatAt:   time.Now().UTC().Truncate(time.Second),
		Version:       "test-version",
		RepoPath:      repo,
		IndexedRef:    "main",
		IndexedCommit: "deadbeef1234",
		Entities:      100,
		Relationships: 250,
		GraphFBMtime:  1234567890,
		Indexing:      true,
		QueueLen:      3,
		LastErr:       "",
	}

	if err := statusfile.Write(repo, want); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := statusfile.Read(repo)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.EnginePID != want.EnginePID {
		t.Errorf("EnginePID = %d, want %d", got.EnginePID, want.EnginePID)
	}
	if !got.HeartbeatAt.Equal(want.HeartbeatAt) {
		t.Errorf("HeartbeatAt = %v, want %v", got.HeartbeatAt, want.HeartbeatAt)
	}
	if got.Version != want.Version {
		t.Errorf("Version = %q, want %q", got.Version, want.Version)
	}
	if got.IndexedRef != want.IndexedRef || got.IndexedCommit != want.IndexedCommit {
		t.Errorf("IndexedRef/IndexedCommit = %q/%q, want %q/%q",
			got.IndexedRef, got.IndexedCommit, want.IndexedRef, want.IndexedCommit)
	}
	if got.Entities != want.Entities || got.Relationships != want.Relationships {
		t.Errorf("Entities/Relationships = %d/%d, want %d/%d",
			got.Entities, got.Relationships, want.Entities, want.Relationships)
	}
	if got.GraphFBMtime != want.GraphFBMtime {
		t.Errorf("GraphFBMtime = %d, want %d", got.GraphFBMtime, want.GraphFBMtime)
	}
	if !got.Indexing {
		t.Error("Indexing should be true")
	}
	if got.QueueLen != want.QueueLen {
		t.Errorf("QueueLen = %d, want %d", got.QueueLen, want.QueueLen)
	}
}

// TestWrite_Atomic asserts Write never leaves a partial/torn file visible at
// the final path: it must write to a temp file and rename into place, so a
// concurrent Read either sees the old complete file or the new complete file,
// never a half-written one.
func TestWrite_Atomic(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	repo := "/atomic/repo"

	if err := statusfile.Write(repo, &statusfile.File{EnginePID: 1, Version: "v1"}); err != nil {
		t.Fatalf("Write 1: %v", err)
	}
	path, err := statusfile.PathFor(repo)
	if err != nil {
		t.Fatalf("PathFor: %v", err)
	}
	// No leftover .tmp file after a successful write.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("leftover tmp file after Write: %s", e.Name())
		}
	}

	if err := statusfile.Write(repo, &statusfile.File{EnginePID: 2, Version: "v2"}); err != nil {
		t.Fatalf("Write 2: %v", err)
	}
	got, err := statusfile.Read(repo)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Version != "v2" {
		t.Errorf("expected the latest write to win, got version %q", got.Version)
	}
}

// TestRead_MissingFile_ReturnsNotFound ensures a poll-safe reader can
// distinguish "never written yet" from a real I/O error, so it can fall back
// to "unknown" rather than treating the absence as a hang or crash.
func TestRead_MissingFile_ReturnsNotFound(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	_, err := statusfile.Read("/never/written/repo")
	if err == nil {
		t.Fatal("expected an error for a repo with no status file written yet")
	}
	if !os.IsNotExist(err) {
		t.Errorf("expected an os.IsNotExist error, got: %v", err)
	}
}

// TestPathFor_DeterministicPerRepo asserts the same repo path always maps to
// the same on-disk file (so a writer and a reader agree without coordination)
// and different repos map to different files.
func TestPathFor_DeterministicPerRepo(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	p1, err := statusfile.PathFor("/repo/a")
	if err != nil {
		t.Fatal(err)
	}
	p1b, err := statusfile.PathFor("/repo/a")
	if err != nil {
		t.Fatal(err)
	}
	if p1 != p1b {
		t.Errorf("PathFor not deterministic: %q != %q", p1, p1b)
	}
	p2, err := statusfile.PathFor("/repo/b")
	if err != nil {
		t.Fatal(err)
	}
	if p1 == p2 {
		t.Errorf("PathFor collided for distinct repos: %q", p1)
	}
}
