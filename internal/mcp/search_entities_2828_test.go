package mcp

// search_entities_2828_test.go — #2828 token-cost optimisation for the
// high-volume archigraph_search_entities tool (248 calls in live telemetry).
// Verifies the opt-in format=terse compact-line output is smaller than the
// default `results` array while preserving id/name/kind/location, and that
// token_budget caps the returned list with a truncation marker.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

func buildSearchDoc(n int) *graph.Document {
	ents := make([]graph.Entity, 0, n)
	for i := 0; i < n; i++ {
		ents = append(ents, graph.Entity{
			ID:            fmt.Sprintf("svc_%d", i),
			Name:          fmt.Sprintf("OrderService%d", i),
			QualifiedName: fmt.Sprintf("app.services.OrderService%d", i),
			Kind:          "SCOPE.Class",
			SourceFile:    fmt.Sprintf("app/services/order_%d.py", i),
			StartLine:     5 + i,
		})
	}
	return &graph.Document{Entities: ents}
}

func rawSearchBytes(t *testing.T, s *Server, args map[string]any) (int, map[string]any) {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := s.handleSearchEntities(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("tool error: %+v", res)
	}
	text := extractResultText(t, res)
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, text)
	}
	return len(text), out
}

func TestSearchEntities_2828_TerseSmallerPreservesFacts(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, buildSearchDoc(30))

	defBytes, defOut := rawSearchBytes(t, s, map[string]any{"group": "test", "query": "OrderService"})
	terseBytes, terseOut := rawSearchBytes(t, s, map[string]any{"group": "test", "query": "OrderService", "format": "terse"})

	// Default shape unchanged (results array present, no format key forced).
	if _, ok := defOut["results"]; !ok {
		t.Error("default search shape lost `results`")
	}
	if terseBytes >= defBytes {
		t.Fatalf("terse (%d B) not smaller than default results (%d B)", terseBytes, defBytes)
	}
	t.Logf("search_entities bytes: terse=%d default=%d (%.1f%% smaller)",
		terseBytes, defBytes, 100*float64(defBytes-terseBytes)/float64(defBytes))

	lines, ok := terseOut["lines"].([]any)
	if !ok || len(lines) == 0 {
		t.Fatalf("terse lines is %T", terseOut["lines"])
	}
	first := lines[0].(string)
	// Essential facts: prefixed id, name, kind (scope-stripped), file:line.
	for _, want := range []string{"OrderService", "Class", "app/services/order_", ":"} {
		if !strings.Contains(first, want) {
			t.Errorf("terse line missing %q: %s", want, first)
		}
	}
	if strings.Contains(first, "SCOPE.") {
		t.Errorf("terse kind should be scope-stripped: %s", first)
	}
}

func TestSearchEntities_2828_TokenBudgetTruncates(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, buildSearchDoc(60))
	_, out := rawSearchBytes(t, s, map[string]any{
		"group": "test", "query": "OrderService", "format": "terse",
		"limit": 100, "token_budget": 200,
	})
	count := int(out["count"].(float64))
	total := int(out["total"].(float64))
	if count >= total {
		t.Fatalf("token_budget did not truncate: count=%d total=%d", count, total)
	}
	if out["truncated"] != true {
		t.Error("expected truncated=true under token_budget")
	}
}
