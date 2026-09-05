package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/links"
)

// writeReachabilitySidecarForTest lands a reachability sidecar where
// handleDeadCode will look for it, under an isolated GRAFEL_HOME.
func writeReachabilitySidecarForTest(t *testing.T, group string, body map[string]any) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)
	path, err := links.PassSidecarPath("", group, "reachability")
	if err != nil {
		t.Fatalf("PassSidecarPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
}

// TestDeadCodeSurfacesDegradedRepos is the consumer half of #6839. The link
// pass declines to call anything dead in a repo whose entry-point file it
// could not read; grafel_dead_code must say so rather than presenting the
// resulting short list as a complete answer — "no dead code here" and "we
// could not tell" are different claims.
func TestDeadCodeSurfacesDegradedRepos(t *testing.T) {
	writeReachabilitySidecarForTest(t, "test", map[string]any{
		"version":                      1,
		"group":                        "test",
		"total_entities":               10,
		"reachable":                    4,
		"unreachable":                  1,
		"entry_points":                 2,
		"unknown":                      5,
		"degraded_repos":               []string{"repo-bad"},
		"unreadable_entry_point_files": 1,
		"entries": []map[string]any{
			{"repo": "repo-good", "entity_id": "dead-1", "name": "DeadFunc",
				"kind": "Function", "source_file": "dead.go", "reachable": false},
			{"repo": "repo-good", "entity_id": "live-1", "name": "LiveFunc",
				"kind": "Function", "source_file": "live.go", "reachable": true},
		},
	})

	srv := newTestServer(t, buildDeadCodeDoc())
	out := callFlowTool(t, srv.handleDeadCode, map[string]any{"limit": float64(100)})

	if src, _ := out["source"].(string); src != "sidecar" {
		t.Fatalf("source: want sidecar (the degradation only exists there), got %q", src)
	}
	degraded, ok := out["degraded_repos"].([]any)
	if !ok || len(degraded) != 1 || degraded[0] != "repo-bad" {
		t.Errorf("degraded_repos: want [repo-bad], got %v", out["degraded_repos"])
	}
	if unknown, _ := out["unknown"].(float64); unknown != 5 {
		t.Errorf("unknown: want 5, got %v", out["unknown"])
	}
	note, _ := out["note"].(string)
	if !strings.Contains(note, "PARTIAL") || !strings.Contains(note, "repo-bad") {
		t.Errorf("note must tell the caller the answer is partial and name the repo; got %q", note)
	}
	// The repo we COULD compute still reports its dead code.
	dead, _ := out["dead_code"].([]any)
	if len(dead) != 1 {
		t.Fatalf("dead_code: want the one computable finding, got %v", dead)
	}
	if name, _ := dead[0].(map[string]any)["name"].(string); name != "DeadFunc" {
		t.Errorf("dead_code[0].name: want DeadFunc, got %q", name)
	}
}

// TestDeadCodeUndegradedResponseHasNoPartialMarker is the other direction:
// a healthy sidecar must not carry a degradation flag or a PARTIAL note, or
// every clean answer starts reading as a hedge.
func TestDeadCodeUndegradedResponseHasNoPartialMarker(t *testing.T) {
	writeReachabilitySidecarForTest(t, "test", map[string]any{
		"version":        1,
		"group":          "test",
		"total_entities": 2,
		"reachable":      1,
		"unreachable":    1,
		"entry_points":   1,
		"entries": []map[string]any{
			{"repo": "repo-good", "entity_id": "dead-1", "name": "DeadFunc",
				"kind": "Function", "source_file": "dead.go", "reachable": false},
		},
	})

	srv := newTestServer(t, buildDeadCodeDoc())
	out := callFlowTool(t, srv.handleDeadCode, map[string]any{"limit": float64(100)})

	if _, present := out["degraded_repos"]; present {
		t.Errorf("degraded_repos must be absent when nothing degraded; got %v", out["degraded_repos"])
	}
	if _, present := out["unknown"]; present {
		t.Errorf("unknown must be absent when nothing degraded; got %v", out["unknown"])
	}
	if note, _ := out["note"].(string); strings.Contains(note, "PARTIAL") {
		t.Errorf("note must not claim partiality on a complete answer; got %q", note)
	}
	if dead, _ := out["dead_code"].([]any); len(dead) != 1 {
		t.Errorf("dead_code: want 1 finding, got %v", dead)
	}
}
