package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/indexstate"
)

// TestGraphStatsIsIndexing verifies the P5 dogfooding ask: grafel_stats exposes
// the live reindex state sourced from the process-global indexstate record, so
// a coordinator can query it instead of polling `ps aux`. When idle the flag is
// false and the detail fields are omitted; when the scheduler marks a job in
// flight the flag is true and indexing_in_flight / indexing_started_at appear.
func TestGraphStatsIsIndexing(t *testing.T) {
	t.Cleanup(func() { indexstate.Set(0) })

	dir := t.TempDir()
	r1 := filepath.Join(dir, "alpha")
	_ = os.MkdirAll(r1, 0o755)
	writeGraph(t, r1, fixtureDoc("alpha"))
	regPath := makeRegistry(t, dir, map[string]map[string]string{
		"g": {"alpha": r1},
	})
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}

	parse := func() map[string]any {
		t.Helper()
		res := callTool(t, srv, "grafel_stats", nil)
		var m map[string]any
		if err := json.Unmarshal([]byte(resultText(res)), &m); err != nil {
			t.Fatalf("unmarshal stats: %v", err)
		}
		return m
	}

	// Idle: is_indexing=false, no detail fields.
	indexstate.Set(0)
	idle := parse()
	if v, ok := idle["is_indexing"].(bool); !ok || v {
		t.Fatalf("idle: is_indexing = %v, want false", idle["is_indexing"])
	}
	if _, present := idle["indexing_in_flight"]; present {
		t.Fatalf("idle: indexing_in_flight should be omitted, got %v", idle["indexing_in_flight"])
	}
	if _, present := idle["indexing_started_at"]; present {
		t.Fatalf("idle: indexing_started_at should be omitted")
	}

	// Busy: is_indexing=true with detail fields populated.
	indexstate.Set(2)
	busy := parse()
	if v, ok := busy["is_indexing"].(bool); !ok || !v {
		t.Fatalf("busy: is_indexing = %v, want true", busy["is_indexing"])
	}
	if n, ok := busy["indexing_in_flight"].(float64); !ok || int(n) != 2 {
		t.Fatalf("busy: indexing_in_flight = %v, want 2", busy["indexing_in_flight"])
	}
	if _, ok := busy["indexing_started_at"].(string); !ok {
		t.Fatalf("busy: indexing_started_at should be an RFC3339 string, got %v", busy["indexing_started_at"])
	}
}
