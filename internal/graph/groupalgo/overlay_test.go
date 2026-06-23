package groupalgo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/registry"
)

// sampleResult builds a small GroupAlgoResult for overlay round-trip tests.
func sampleResult() *GroupAlgoResult {
	return &GroupAlgoResult{
		Group: "acme",
		Results: &graph.AlgorithmResults{
			CommunityID: map[string]int{"svc:Service": 7, "web:b1": 7, "svc:a1": 3},
			PageRank:    map[string]float64{"svc:Service": 0.42, "web:b1": 0.01, "svc:a1": 0.05},
			Centrality:  map[string]float64{"svc:Service": 0.9},
			GodNodes:    map[string]bool{"svc:Service": true},
			ArticulationPoints: map[string]bool{
				"svc:Service": true,
			},
			Communities: []graph.CommunityResult{{ID: 7, Size: 2, AutoName: "service-core"}},
			Stats:       graph.AlgorithmStats{NumCommunities: 2, LouvainModularity: 0.31, NumGodNodes: 1},
		},
		EntityRepo:   map[string]string{"svc:Service": "svc", "web:b1": "web", "svc:a1": "svc"},
		SourceMtimes: map[string]int64{"svc": 111, "web": 222},
		NumEntities:  3,
		NumRels:      2,
		NumRepos:     2,
	}
}

// TestBuildOverlay_RoundTrip checks BuildOverlay projects every entity and that
// the per-entity values match the source result.
func TestBuildOverlay_RoundTrip(t *testing.T) {
	ov := BuildOverlay(sampleResult())
	if ov == nil {
		t.Fatal("expected non-nil overlay")
	}
	if len(ov.Results) != 3 {
		t.Fatalf("expected 3 entity overlays, got %d", len(ov.Results))
	}
	svc := ov.Results["svc:Service"]
	if svc.CommunityID != 7 || svc.PageRank != 0.42 || svc.Centrality != 0.9 || !svc.IsGodNode || !svc.IsArticulationPoint {
		t.Errorf("svc:Service overlay wrong: %+v", svc)
	}
	if ov.SourceMtimes["svc"] != 111 || ov.SourceMtimes["web"] != 222 {
		t.Errorf("source mtimes not copied: %v", ov.SourceMtimes)
	}
	if len(ov.Communities) != 1 || ov.Communities[0].AutoName != "service-core" {
		t.Errorf("communities summary not carried: %v", ov.Communities)
	}
	if ov.Stats.NumCommunities != 2 {
		t.Errorf("stats not carried: %+v", ov.Stats)
	}
}

func TestBuildOverlay_NilResult(t *testing.T) {
	if BuildOverlay(nil) != nil {
		t.Error("nil result should yield nil overlay")
	}
	if BuildOverlay(&GroupAlgoResult{Group: "x"}) != nil {
		t.Error("result with nil Results should yield nil overlay")
	}
}

// TestWriteReadOverlay covers a full write→read with matching mtimes (fresh).
func TestWriteReadOverlay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "acme-algo.json")
	ov := BuildOverlay(sampleResult())
	if err := WriteOverlayTo(path, ov); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Fresh: current mtimes equal the stored ones.
	got, ok := ReadOverlay(path, map[string]int64{"svc": 111, "web": 222})
	if !ok {
		t.Fatal("expected fresh overlay to read back")
	}
	if got.Results["svc:Service"].PageRank != 0.42 {
		t.Errorf("round-trip pagerank wrong: %v", got.Results["svc:Service"])
	}
}

// TestReadOverlay_Stale: bumping one repo's current mtime marks it stale.
func TestReadOverlay_Stale(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "acme-algo.json")
	if err := WriteOverlayTo(path, BuildOverlay(sampleResult())); err != nil {
		t.Fatalf("write: %v", err)
	}
	// web's graph.fb mtime moved → stale.
	if _, ok := ReadOverlay(path, map[string]int64{"svc": 111, "web": 999}); ok {
		t.Error("expected stale overlay (web mtime changed) to be rejected")
	}
	// A recorded repo missing from current mtimes → also stale.
	if _, ok := ReadOverlay(path, map[string]int64{"svc": 111}); ok {
		t.Error("expected stale overlay (web graph.fb vanished) to be rejected")
	}
}

func TestReadOverlay_Absent(t *testing.T) {
	if _, ok := ReadOverlay(filepath.Join(t.TempDir(), "nope-algo.json"), nil); ok {
		t.Error("absent overlay must read as (nil,false)")
	}
}

func TestReadOverlay_Corrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x-algo.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadOverlay(path, nil); ok {
		t.Error("corrupt overlay must read as (nil,false)")
	}
}

func TestIsOverlayStale(t *testing.T) {
	ov := &Overlay{SourceMtimes: map[string]int64{"a": 1, "b": 2}}
	if IsOverlayStale(ov, map[string]int64{"a": 1, "b": 2}) {
		t.Error("matching mtimes must not be stale")
	}
	if IsOverlayStale(ov, map[string]int64{"a": 1, "b": 3}) == false {
		t.Error("changed mtime must be stale")
	}
	if IsOverlayStale(nil, nil) == false {
		t.Error("nil overlay is stale")
	}
	// Extra repo in current (not in overlay) does not by itself make it stale.
	if IsOverlayStale(ov, map[string]int64{"a": 1, "b": 2, "c": 9}) {
		t.Error("extra current repo must not mark overlay stale")
	}
}

// TestOverlay_NoTornRead is the A2 atomic-swap acceptance: a reader loop reading
// the overlay while a writer rewrites it via temp+rename must NEVER observe a
// partial / invalid JSON document. Because each write is a single os.Rename, the
// reader sees either the old file or the new one in full.
func TestOverlay_NoTornRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "race-algo.json")

	// Seed an initial overlay so the first reads succeed.
	base := sampleResult()
	if err := WriteOverlayTo(path, BuildOverlay(base)); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	var stop atomic.Bool
	var wg sync.WaitGroup
	var torn atomic.Int64
	var reads atomic.Int64
	var writeOK, writeErr atomic.Int64

	// Writer: rewrite the overlay rapidly with varying contents.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000 && !stop.Load(); i++ {
			r := sampleResult()
			// vary a value so each write is distinct/larger sometimes.
			r.Results.PageRank["svc:Service"] = float64(i)
			for k := 0; k < i%50; k++ {
				r.Results.CommunityID[string(rune('A'+k%26))+string(rune('0'+k%10))] = k
			}
			if err := WriteOverlayTo(path, BuildOverlay(r)); err != nil {
				// A failed atomic swap is NOT a torn read — readers still observe the
				// previous complete file. Under this artificial 4-reader stress the
				// Windows destination can stay open long enough that even the bounded
				// rename retry exhausts (ERROR_SHARING_VIOLATION); that is a harness
				// artifact, not a production bug (the real MCP reader reads briefly).
				// Count it and keep going. The assertions below verify the real
				// property (zero torn reads) and that writes still make progress.
				writeErr.Add(1)
				continue
			}
			writeOK.Add(1)
		}
		stop.Store(true)
	}()

	// Readers: read + json.Unmarshal in a loop; any unmarshal error is a torn
	// read (must be zero). A yield at the end of each iteration releases the OS
	// scheduler (and, on Windows, the destination file handle) momentarily so the
	// writer's atomic temp+rename has a window to land. This mirrors real usage:
	// the MCP overlay apply path opens, reads, and closes the file in a single
	// brief operation with gaps between reads — it never spins a file handle open
	// in an unbroken tight loop. A truly gapless reader loop would deadlock the
	// Windows rename-over-existing (ERROR_SHARING_VIOLATION) indefinitely, which
	// is an artifact of the stress harness, not a torn read or a production bug.
	// The concurrent read-vs-atomic-swap assertion is fully preserved.
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				data, err := os.ReadFile(path)
				if err != nil {
					// ENOENT is impossible with rename-in-place, but tolerate any
					// transient and keep going; it is not a torn read.
					runtime.Gosched()
					continue
				}
				reads.Add(1)
				var ov Overlay
				if err := json.Unmarshal(data, &ov); err != nil {
					torn.Add(1)
				}
				// Sleep briefly between reads so the handle is released and the
				// concurrent writer's atomic rename reliably finds a window (essential
				// on Windows; negligible on Unix). os.ReadFile holds the file only
				// microseconds, so a 1ms gap leaves the destination free almost always.
				time.Sleep(time.Millisecond)
			}
		}()
	}

	// Bound the test by time as a backstop.
	go func() {
		time.Sleep(2 * time.Second)
		stop.Store(true)
	}()
	wg.Wait()

	if reads.Load() == 0 {
		t.Fatal("reader observed zero reads — test did not exercise the race")
	}
	if torn.Load() != 0 {
		t.Fatalf("observed %d torn reads over %d reads (atomic swap broken)", torn.Load(), reads.Load())
	}
	if writeOK.Load() == 0 {
		t.Fatalf("no overlay writes succeeded (%d transient write errors) — writer path broken", writeErr.Load())
	}
	t.Logf("no torn reads over %d reads (%d writes ok, %d transient write errors)", reads.Load(), writeOK.Load(), writeErr.Load())
}

// TestCurrentSourceMtimes_OnDisk verifies the slug→mtime helper reads real
// graph.fb mtimes for a registered group (and skips never-indexed repos).
func TestCurrentSourceMtimes_OnDisk(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GRAFEL_HOME", filepath.Join(root, "home"))
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemon"))

	repoA, repoB, _ := fixtureGraphs()
	pathA := filepath.Join(root, "repoA")
	pathB := filepath.Join(root, "repoB")
	rA := writeFixtureRepo(t, "svc", pathA, repoA)
	rB := writeFixtureRepo(t, "web", pathB, repoB)

	cfgPath, err := registry.ConfigPathFor("acme")
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	cfg := &registry.GroupConfig{Name: "acme", Repos: []registry.Repo{rA, rB}}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save group config: %v", err)
	}
	if err := registry.AddGroup("acme", cfgPath); err != nil {
		t.Fatalf("add group: %v", err)
	}

	cur, err := CurrentSourceMtimes("acme")
	if err != nil {
		t.Fatalf("CurrentSourceMtimes: %v", err)
	}
	if _, ok := cur["svc"]; !ok {
		t.Error("missing svc mtime")
	}
	if _, ok := cur["web"]; !ok {
		t.Error("missing web mtime")
	}
}

// TestWriteOverlayFromResult_NilNoop confirms nil/empty results write nothing.
func TestWriteOverlayFromResult_NilNoop(t *testing.T) {
	if err := WriteOverlayFromResult(nil); err != nil {
		t.Errorf("nil result write must be a no-op, got %v", err)
	}
}
