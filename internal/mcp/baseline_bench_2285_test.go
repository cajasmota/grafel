package mcp

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// Inline copy of the PRE-#2285 linear-scan implementation, for benchmark
// comparison. Not part of the production code path.
func agentResolvedEdges_baseline(doc *graph.Document, repo string, entityID string, scopeIsOne bool) []map[string]any {
	if doc == nil {
		return nil
	}
	var out []map[string]any
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if r.FromID != entityID {
			continue
		}
		if r.PropGet("resolved_by") != "agent-repair" {
			continue
		}
		toID := r.ToID
		if !scopeIsOne {
			toID = prefixedID(repo, toID)
		}
		entry := map[string]any{
			"kind":        r.Kind,
			"target":      toID,
			"resolved_by": "agent-repair",
		}
		if v := r.PropGet("resolved_by_agent"); v != "" {
			entry["resolved_by_agent"] = v
		}
		if v := r.PropGet("repair_reasoning"); v != "" {
			entry["repair_reasoning"] = v
		}
		out = append(out, entry)
	}
	return out
}

func BenchmarkAgentResolvedEdges_Baseline(b *testing.B) {
	doc := benchDoc(10000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = agentResolvedEdges_baseline(doc, "repo1", "src", true)
	}
}
