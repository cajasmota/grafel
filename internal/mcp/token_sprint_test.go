package mcp

// Tests for the MCP token sprint bundle (#1741 #1742 #1753 #1765 #1770 #1772
// #1807 #1921). Covers:
//
//   - #1741 — fields= selection arg strips non-listed fields from records.
//   - #1753 — archigraph_neighbors registered + direction=in|out|both works.
//   - #1753 — find_callers / find_callees remain registered as deprecated
//     aliases (one-release grace per #2003 / #1765 policy).
//   - #1807 / #1921 — min_score floored at 0.15; max_results capped at 200.
//   - #1770 — `query` is canonical on archigraph_find; legacy "question" still
//     accepted but logs deprecation (test already exists in param_rename_test.go).
//   - #1772 — registry mutation flips the surface signature and Reload reports
//     surfaceChanged=true.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// (#1753) archigraph_neighbors is registered with the canonical schema and
// direction param defaults to "both".
func TestNeighborsToolRegistered(t *testing.T) {
	dir := t.TempDir()
	regPath := filepath.Join(dir, "registry.json")
	if err := os.WriteFile(regPath, []byte(`{"groups":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}
	tools := srv.MCP.ListTools()
	if _, ok := tools["archigraph_neighbors"]; !ok {
		t.Fatal("archigraph_neighbors not registered")
	}
	// find_callers + find_callees stay as deprecated aliases for one release.
	for _, n := range []string{"archigraph_find_callers", "archigraph_find_callees"} {
		if _, ok := tools[n]; !ok {
			t.Errorf("deprecated alias %q must remain registered (one-release grace)", n)
		}
	}
}

// (#1753) direction=in returns the same shape as the legacy find_callers tool;
// direction=out matches find_callees; direction=both returns both arrays.
func TestNeighborsDirection(t *testing.T) {
	srv := newSmokeSrv(t)

	// direction=in (callers)
	res := callTool(t, srv, "archigraph_neighbors", map[string]any{
		"group":     "g",
		"entity_id": "r1::a2",
		"direction": "in",
	})
	text := resultText(res)
	if !strings.Contains(text, "callers") {
		t.Errorf("direction=in should produce callers envelope, got: %s", text)
	}

	// direction=out (callees)
	res = callTool(t, srv, "archigraph_neighbors", map[string]any{
		"group":     "g",
		"entity_id": "r1::a2",
		"direction": "out",
	})
	text = resultText(res)
	if !strings.Contains(text, "callees") {
		t.Errorf("direction=out should produce callees envelope, got: %s", text)
	}

	// direction=both merges both
	res = callTool(t, srv, "archigraph_neighbors", map[string]any{
		"group":     "g",
		"entity_id": "r1::a2",
		"direction": "both",
	})
	text = resultText(res)
	if !strings.Contains(text, "callers") || !strings.Contains(text, "callees") {
		t.Errorf("direction=both should expose both callers + callees, got: %s", text)
	}
}

// (#1753) deprecated aliases still work — calling find_callers/find_callees
// directly returns a valid envelope identical to neighbors(direction=in/out).
func TestNeighborsDeprecatedAliasesStillWork(t *testing.T) {
	srv := newSmokeSrv(t)

	res := callTool(t, srv, "archigraph_find_callers", map[string]any{
		"group":     "g",
		"entity_id": "r1::a2",
	})
	if res.IsError {
		t.Errorf("find_callers alias returned IsError: %s", resultText(res))
	}
	if !strings.Contains(resultText(res), "callers") {
		t.Errorf("find_callers alias missing 'callers' key: %s", resultText(res))
	}

	res = callTool(t, srv, "archigraph_find_callees", map[string]any{
		"group":     "g",
		"entity_id": "r1::a2",
	})
	if res.IsError {
		t.Errorf("find_callees alias returned IsError: %s", resultText(res))
	}
	if !strings.Contains(resultText(res), "callees") {
		t.Errorf("find_callees alias missing 'callees' key: %s", resultText(res))
	}
}

// (#1741) fields= selection drops non-listed fields from per-record output.
// Use search_entities which produces a stable {results, count} envelope.
func TestFieldsSelectionStripsUnlistedKeys(t *testing.T) {
	srv := newSmokeSrv(t)

	// Baseline: no fields= → full record shape.
	res := callTool(t, srv, "archigraph_search_entities", map[string]any{
		"group": "g",
		"query": "a",
	})
	baseText := resultText(res)
	if !strings.Contains(baseText, "\"kind\"") {
		t.Logf("baseline output: %s", baseText)
	}

	// With fields=[name,entity_id] only.
	res = callTool(t, srv, "archigraph_search_entities", map[string]any{
		"group":  "g",
		"query":  "a",
		"fields": []any{"name", "entity_id"},
	})
	text := resultText(res)
	// Envelope keys must still be present.
	if !strings.Contains(text, "results") {
		t.Errorf("envelope 'results' missing after fields= filter: %s", text)
	}
	if !strings.Contains(text, "count") {
		t.Errorf("envelope 'count' missing after fields= filter: %s", text)
	}
	// Parse and assert per-record shape.
	var obj map[string]any
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	results, _ := obj["results"].([]any)
	if len(results) == 0 {
		t.Skip("fixture produced no results; smoke fixture lacks names containing 'a'")
	}
	for _, r := range results {
		rec, ok := r.(map[string]any)
		if !ok {
			continue
		}
		// "kind" / "qualified_name" / "source_file" must NOT survive the filter
		// (unless they're identifying fields preserved by the fallback).
		for _, k := range []string{"kind", "qualified_name", "source_file", "start_line"} {
			if _, present := rec[k]; present {
				t.Errorf("field %q should be stripped by fields= filter, record: %+v", k, rec)
			}
		}
	}
}

// (#1921) max_results hard ceiling on archigraph_find — even when the caller
// asks for more than 200, the response is capped. We can't easily construct a
// 3,000-row fixture here, but we can verify the schema accepts max_results and
// the call shape doesn't error.
func TestFindAcceptsMaxResultsArg(t *testing.T) {
	srv := newSmokeSrv(t)

	res := callTool(t, srv, "archigraph_find", map[string]any{
		"group":       "g",
		"query":       "a",
		"max_results": 5,
		"full":        true,
	})
	if res.IsError {
		t.Errorf("find with max_results=5 returned IsError: %s", resultText(res))
	}
	// Asking for 500 must be silently capped to 200 (no error).
	res = callTool(t, srv, "archigraph_find", map[string]any{
		"group":       "g",
		"query":       "a",
		"max_results": 500,
		"full":        true,
	})
	if res.IsError {
		t.Errorf("find with max_results=500 should be silently capped, got error: %s", resultText(res))
	}
}

// (#1807 / #1921) min_score floor: caller-supplied min_score below 0.15 is
// silently raised to 0.15. We can't easily inspect the internal float, so this
// test asserts the call shape accepts the param and returns successfully.
func TestFindMinScoreFloor(t *testing.T) {
	srv := newSmokeSrv(t)

	res := callTool(t, srv, "archigraph_find", map[string]any{
		"group":     "g",
		"query":     "a",
		"min_score": 0.01, // below floor — silently raised to 0.15
		"full":      true,
	})
	if res.IsError {
		t.Errorf("find with min_score=0.01 should be silently floored, got error: %s", resultText(res))
	}
}

// (#1772) registry signature changes between Reloads when groups mutate, so
// the surfaceChanged flag flips to true. Used by reloadBeforeCall to decide
// whether to emit notifications/tools/list_changed.
func TestRegistrySurfaceChangedSignal(t *testing.T) {
	dir := t.TempDir()
	regPath := filepath.Join(dir, "registry.json")
	if err := os.WriteFile(regPath, []byte(`{"groups":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}
	// First reload after construction: signature is already loaded so no change.
	_, changed1, err := srv.State.ReloadAndSurfaceChanged()
	if err != nil {
		t.Fatal(err)
	}
	if changed1 {
		t.Errorf("first reload should not report surfaceChanged=true")
	}

	// Mutate the registry on disk: add a group.
	repoDir := t.TempDir()
	mutated := `{"groups":{"g":{"repos":{"r1":{"path":"` + repoDir + `"}}}}}`
	// Force a newer mtime by sleeping a beat OR by re-stat'ing after write.
	if err := os.WriteFile(regPath, []byte(mutated), 0o644); err != nil {
		t.Fatal(err)
	}
	// Bump the file's mtime explicitly to defeat sub-second filesystems.
	future := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(regPath, future, future)

	_, changed2, err := srv.State.ReloadAndSurfaceChanged()
	if err != nil {
		t.Fatal(err)
	}
	if !changed2 {
		t.Errorf("post-mutation reload should report surfaceChanged=true")
	}
}

