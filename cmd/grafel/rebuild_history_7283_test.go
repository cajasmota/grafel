package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/quality/analytics"
	"github.com/cajasmota/grafel/internal/registry"
)

// rebuild_history_7283_test.go — the daemon's own writer of health-history.jsonl
// must not record an unmeasured bug rate as a measured zero (#7283).
//
// These assertions read the SERIALISED JSONL BYTES rather than the HealthEntry
// struct. The whole defect is what a later reader decodes: a struct-level
// assertion passes with the persistence still wrong, because `"bug_rate":0`
// and "no bug rate" are the same Go value on a bare float64.

// rawHistoryLine returns the single JSONL line the batch appended, decoded
// only as far as a key→raw-JSON map so the test can ask which keys are
// PRESENT — the question a bare struct decode cannot answer.
func rawHistoryLine(t *testing.T, root string) (map[string]json.RawMessage, string) {
	t.Helper()
	var path string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(p, "health-history.jsonl") {
			path = p
		}
		return nil
	})
	if err != nil || path == "" {
		t.Fatalf("health-history.jsonl not written under %s", root)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	var lines []string
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(ln) != "" {
			lines = append(lines, ln)
		}
	}
	if len(lines) != 1 {
		t.Fatalf("history lines = %d, want 1:\n%s", len(lines), string(b))
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(lines[0]), &m); err != nil {
		t.Fatalf("decode history line: %v", err)
	}
	return m, lines[0]
}

// stubScansByStateDir installs a scanRepoAnalytics that answers per state dir.
// A repo whose path is absent from the map fails its scan — the exact
// condition rebuild_history.go's `continue` swallows.
func stubScansByStateDir(t *testing.T, byRepo map[string]analytics.RepoScan) {
	t.Helper()
	orig := scanRepoAnalytics
	byStateDir := make(map[string]analytics.RepoScan, len(byRepo))
	for repo, sc := range byRepo {
		byStateDir[stateDirForRepoPath(repo)] = sc
	}
	scanRepoAnalytics = func(stateDir string) (analytics.RepoScan, error) {
		sc, ok := byStateDir[stateDir]
		if !ok {
			return analytics.RepoScan{}, errors.New("scan failed (fixture)")
		}
		return sc, nil
	}
	t.Cleanup(func() { scanRepoAnalytics = orig })
}

// TestAppendRebuildHistory_AllScansFail_RecordsNoMeasurement is the forbidden
// row. Every repo's scan errors, so nothing at all was measured — and the
// pre-change code wrote orphan_rate 0, bug_rate 0 and, through
// ComputeHealthScore(0, 0), a PERFECT health_score of 100. The dashboard's
// fidelity badge reads exactly this file.
//
// Three repos, not one: a single-repo fixture cannot tell "the loop ran and
// every iteration was skipped" apart from "the loop never ran".
func TestAppendRebuildHistory_AllScansFail_RecordsNoMeasurement(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())

	repos := []string{t.TempDir(), t.TempDir(), t.TempDir()}
	stubScansByStateDir(t, nil) // every repo fails

	cfg := &registry.GroupConfig{Name: "acme"}
	if err := appendRebuildHistory(root, "acme", cfg, repos); err != nil {
		t.Fatalf("appendRebuildHistory: %v", err)
	}

	m, line := rawHistoryLine(t, root)

	if raw, ok := m["health_score"]; ok {
		t.Errorf("no repo scanned, yet the record carries health_score=%s — a score from zero measurements\nline: %s", raw, line)
	}
	if raw, ok := m["bug_rate"]; ok {
		t.Errorf("no repo scanned, yet the record carries bug_rate=%s, which a later reader decodes as a measured rate\nline: %s", raw, line)
	}
}

// TestAppendRebuildHistory_PartialScan_MeasuresOnlyScannedRepos pins the
// other half: a bug rate IS recorded when at least one repo scanned, and it is
// derived from the scanned repos alone.
//
// The counts are chosen so the partial and total answers differ: the two
// scanned repos give 4 unresolved of 20 = 20.0%, while folding the failed
// repo's (fixture-only) 100/100 in would give 20.0/120 — a different number.
// A 1-repo fixture, or equal per-repo counts, would collapse the two.
func TestAppendRebuildHistory_PartialScan_MeasuresOnlyScannedRepos(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())

	failing, okA, okB := t.TempDir(), t.TempDir(), t.TempDir()
	stubScansByStateDir(t, map[string]analytics.RepoScan{
		okA: {Entities: 10, Orphans: 1, ImportsTotal: 8, ImportsResolved: 7},
		okB: {Entities: 30, Orphans: 3, ImportsTotal: 12, ImportsResolved: 9},
	})

	cfg := &registry.GroupConfig{Name: "acme"}
	if err := appendRebuildHistory(root, "acme", cfg, []string{failing, okA, okB}); err != nil {
		t.Fatalf("appendRebuildHistory: %v", err)
	}

	m, line := rawHistoryLine(t, root)

	raw, ok := m["bug_rate"]
	if !ok {
		t.Fatalf("two repos scanned with 20 IMPORTS edges between them, yet no bug_rate was recorded\nline: %s", line)
	}
	var got float64
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("bug_rate is not a number: %s", raw)
	}
	// (8+12) imports, (7+9) resolved → 4/20 = 20%.
	if got != 20.0 {
		t.Errorf("bug_rate = %v, want 20 (4 unresolved of the 20 imports the two scanned repos carry)\nline: %s", got, line)
	}

	rawHS, ok := m["health_score"]
	if !ok {
		t.Fatalf("bug rate measured, yet no health_score recorded\nline: %s", line)
	}
	var hs float64
	if err := json.Unmarshal(rawHS, &hs); err != nil {
		t.Fatalf("health_score is not a number: %s", rawHS)
	}
	// 40 entities, 4 orphans → orphan rate 10; 100 - 10 - 20 = 70.
	if hs != 70.0 {
		t.Errorf("health_score = %v, want 70 (100 − 10 orphan − 20 bug)\nline: %s", hs, line)
	}
	if hs == 100.0 {
		t.Errorf("health_score is a perfect 100 despite a measured 20%% bug rate\nline: %s", line)
	}
}

// TestAppendRebuildHistory_ScannedButNoImports_RecordsNoBugRate covers the
// third state, which the `if totalImports > 0` guard also collapses: repos
// scanned fine and simply have no IMPORTS edges. The rate is undefined, not
// zero, and a health score built on it would be inflated by the same 0.
func TestAppendRebuildHistory_ScannedButNoImports_RecordsNoBugRate(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())

	repoA, repoB := t.TempDir(), t.TempDir()
	stubScansByStateDir(t, map[string]analytics.RepoScan{
		repoA: {Entities: 5, Orphans: 1},
		repoB: {Entities: 5, Orphans: 0},
	})

	cfg := &registry.GroupConfig{Name: "acme"}
	if err := appendRebuildHistory(root, "acme", cfg, []string{repoA, repoB}); err != nil {
		t.Fatalf("appendRebuildHistory: %v", err)
	}

	m, line := rawHistoryLine(t, root)
	if raw, ok := m["bug_rate"]; ok {
		t.Errorf("no IMPORTS edge was seen, yet bug_rate=%s was recorded as measured\nline: %s", raw, line)
	}
	if raw, ok := m["health_score"]; ok {
		t.Errorf("bug rate undefined, yet health_score=%s was recorded\nline: %s", raw, line)
	}
	// The entities WERE measured, so the record must still carry them —
	// otherwise this test would pass on a change that stopped writing anything.
	var te int
	if raw, ok := m["total_entities"]; !ok {
		t.Errorf("total_entities missing from the record\nline: %s", line)
	} else if err := json.Unmarshal(raw, &te); err != nil || te != 10 {
		t.Errorf("total_entities = %s, want 10\nline: %s", raw, line)
	}
}
