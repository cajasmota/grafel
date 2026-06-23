package groupalgo

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// #5403: OverlayNeedsRecompute is the settled-group freshness predicate the
// daemon's overlay sweep uses. It must return:
//   - false for an ABSENT overlay (let the first-compute path run),
//   - false for a FRESH overlay (mtimes match the live graph.fb),
//   - true  for a STALE overlay (a source graph.fb advanced past source_mtimes).

// setupGroupWithOverlay registers a 2-repo group on disk and returns the group
// name. When writeOverlay is true it also writes a <group>-algo.json whose
// source_mtimes are seeded from the supplied map.
func setupGroupWithOverlay(t *testing.T, name string, seedMtimes map[string]int64) {
	t.Helper()
	ov := &Overlay{
		Group:        name,
		ComputedAt:   time.Now().UTC(),
		SourceMtimes: seedMtimes,
		Results:      map[string]EntityOverlay{},
	}
	if err := WriteOverlay(name, ov); err != nil {
		t.Fatalf("write overlay: %v", err)
	}
}

func registerTwoRepoGroup(t *testing.T, root, name string) {
	t.Helper()
	testsupport.IsolateHome(t)
	repoA, repoB, _ := fixtureGraphs()
	rA := writeFixtureRepo(t, "svc", filepath.Join(root, "repoA"), repoA)
	rB := writeFixtureRepo(t, "web", filepath.Join(root, "repoB"), repoB)
	cfgPath, err := registry.ConfigPathFor(name)
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	cfg := &registry.GroupConfig{Name: name, Repos: []registry.Repo{rA, rB}}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save group config: %v", err)
	}
	if err := registry.AddGroup(name, cfgPath); err != nil {
		t.Fatalf("add group: %v", err)
	}
}

func TestOverlayNeedsRecompute_Absent(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GRAFEL_HOME", filepath.Join(root, "home"))
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemon"))
	registerTwoRepoGroup(t, root, "acme")

	// No overlay written → not "needs recompute" (first-compute path owns it).
	if OverlayNeedsRecompute("acme") {
		t.Fatal("absent overlay should NOT report needs-recompute")
	}
}

func TestOverlayNeedsRecompute_Fresh(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GRAFEL_HOME", filepath.Join(root, "home"))
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemon"))
	registerTwoRepoGroup(t, root, "acme")

	cur, err := CurrentSourceMtimes("acme")
	if err != nil {
		t.Fatalf("CurrentSourceMtimes: %v", err)
	}
	setupGroupWithOverlay(t, "acme", cur) // overlay matches live mtimes exactly

	if OverlayNeedsRecompute("acme") {
		t.Fatal("fresh overlay (mtimes match) should NOT report needs-recompute")
	}
}

func TestOverlayNeedsRecompute_Stale(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GRAFEL_HOME", filepath.Join(root, "home"))
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemon"))
	registerTwoRepoGroup(t, root, "acme")

	cur, err := CurrentSourceMtimes("acme")
	if err != nil {
		t.Fatalf("CurrentSourceMtimes: %v", err)
	}
	// Seed the overlay with a DELIBERATELY-OFF mtime for one repo so it no longer
	// matches the live graph.fb → stale.
	stale := map[string]int64{}
	for k, v := range cur {
		stale[k] = v
	}
	for k := range stale {
		stale[k] = stale[k] - 1 // perturb every recorded source mtime
		break
	}
	setupGroupWithOverlay(t, "acme", stale)

	if !OverlayNeedsRecompute("acme") {
		t.Fatal("stale overlay (a source mtime drifted) SHOULD report needs-recompute")
	}
}
