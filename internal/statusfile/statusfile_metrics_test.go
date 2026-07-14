package statusfile_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/statusfile"
)

// TestWriteRead_RSSAndCPURoundtrip is the RED test for the wizard CPU/RAM
// readout (engine-liveness sidecar): RSSMB and CPUPct must round-trip through
// Write/Read unchanged so the wizard TUI's periodic metrics reader can surface
// them without any daemon RPC.
func TestWriteRead_RSSAndCPURoundtrip(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())

	repo := "/some/engine-liveness/key"
	want := &statusfile.File{
		EnginePID: 4242,
		RepoPath:  repo,
		RSSMB:     2355,
		CPUPct:    412.5,
	}

	if err := statusfile.Write(repo, want); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := statusfile.Read(repo)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.RSSMB != want.RSSMB {
		t.Errorf("RSSMB = %d, want %d", got.RSSMB, want.RSSMB)
	}
	if got.CPUPct != want.CPUPct {
		t.Errorf("CPUPct = %v, want %v", got.CPUPct, want.CPUPct)
	}
}

// TestWriteRead_RSSAndCPUOmittedWhenZero asserts a status file written by an
// older engine (or one that failed to sample process metrics) round-trips
// RSSMB/CPUPct as zero, not an error — the field is omitempty so an old file
// on disk simply lacks the key, and Read must tolerate that transparently.
func TestWriteRead_RSSAndCPUOmittedWhenZero(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())

	repo := "/some/other/repo"
	want := &statusfile.File{EnginePID: 1, RepoPath: repo}

	if err := statusfile.Write(repo, want); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := statusfile.Read(repo)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.RSSMB != 0 || got.CPUPct != 0 {
		t.Errorf("RSSMB/CPUPct = %d/%v, want 0/0", got.RSSMB, got.CPUPct)
	}
}
