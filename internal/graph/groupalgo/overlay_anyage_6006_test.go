// overlay_anyage_6006_test.go — ReadOverlayAnyAge (#6006 D2): staleness is
// tolerated, but the two conditions that make an overlay INAPPLICABLE rather
// than merely old are not.
package groupalgo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

func anyAgeOverlay(version int) *Overlay {
	return &Overlay{
		AlgoVersion:  version,
		Group:        "acme",
		SourceMtimes: map[string]int64{"svc": 12345},
		Results:      map[string]EntityOverlay{"svc:A": {CommunityID: 7}},
		Communities:  []graph.CommunityResult{{ID: 7, Size: 1}},
	}
}

// The whole point: an overlay whose recorded source mtimes match nothing is
// returned anyway, so a caller can apply a slightly-old partition rather than
// refuse to answer. ReadOverlay, by contrast, rejects it.
func TestReadOverlayAnyAge_StaleIsReturned(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "acme-algo.json")
	if err := WriteOverlayTo(path, anyAgeOverlay(OverlayAlgoVersion)); err != nil {
		t.Fatalf("write: %v", err)
	}
	current := map[string]int64{"svc": 99999} // nothing matches

	if _, ok := ReadOverlay(path, current); ok {
		t.Fatal("precondition failed: ReadOverlay accepted a stale overlay, so this " +
			"test cannot distinguish the two readers")
	}
	ov, ok := ReadOverlayAnyAge(path)
	if !ok || ov == nil {
		t.Fatal("ReadOverlayAnyAge must return a stale overlay — refusing it is the #6006 D2 regression")
	}
	if !IsOverlayStale(ov, current) {
		t.Error("the returned overlay should still REPORT as stale so callers can flag it")
	}
	if ov.Results["svc:A"].CommunityID != 7 {
		t.Errorf("stale overlay content mangled: %+v", ov.Results)
	}
}

// Version mismatch is NOT staleness: an overlay produced by a different
// algorithm implementation is a different partition, and applying it would mix
// two numbering schemes. Both directions must be refused.
func TestReadOverlayAnyAge_VersionMismatchIsRefused(t *testing.T) {
	for _, v := range []int{0, OverlayAlgoVersion - 1, OverlayAlgoVersion + 1} {
		dir := t.TempDir()
		path := filepath.Join(dir, "acme-algo.json")
		if err := WriteOverlayTo(path, anyAgeOverlay(v)); err != nil {
			t.Fatalf("write: %v", err)
		}
		if ov, ok := ReadOverlayAnyAge(path); ok {
			t.Errorf("algo_version=%d is not this build's contract (%d) and must be refused, got %+v",
				v, OverlayAlgoVersion, ov)
		}
	}
	// Control: the current version IS accepted, so the loop above is not passing
	// simply because ReadOverlayAnyAge refuses everything.
	dir := t.TempDir()
	path := filepath.Join(dir, "acme-algo.json")
	if err := WriteOverlayTo(path, anyAgeOverlay(OverlayAlgoVersion)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, ok := ReadOverlayAnyAge(path); !ok {
		t.Fatal("control: the current algo version must be accepted")
	}
}

// Absent and corrupt collapse to a miss, matching ReadOverlay's contract.
func TestReadOverlayAnyAge_AbsentAndCorrupt(t *testing.T) {
	dir := t.TempDir()
	if _, ok := ReadOverlayAnyAge(filepath.Join(dir, "nope.json")); ok {
		t.Error("absent overlay must be a miss")
	}
	bad := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, ok := ReadOverlayAnyAge(bad); ok {
		t.Error("corrupt overlay must be a miss")
	}
}
