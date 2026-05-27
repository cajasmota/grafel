// Issue #2670 — production-pipeline integration test for DISCRIMINATES_ON.
//
// PR #2667 (#2666) introduced DISCRIMINATES_ON edges in the per-language
// extractors with unit tests that drive stampDiscriminators directly. Issue
// #2670 is the #2604-class meta-bug guard: assert that the FULL production
// index pipeline (cmd/archigraph Indexer.Run, the same path the daemon
// invokes on rebuild) emits the edges on a fixture containing
// `x === 2` / `x == 2` discriminator patterns.
//
// Why this test exists: the unit tests in internal/extractors/{javascript,
// python}/issue2666_discriminator_edges_test.go call extractor.Extract()
// directly, which exercises one level of the pipeline. They cannot catch a
// regression where (a) the synthetic "var:<name>" ToID gets pruned by a
// post-resolver pass, (b) the DISCRIMINATES_ON kind is not registered for
// serialisation, or (c) some other downstream stage drops the edge before
// it lands in graph.Document.Relationships. This test runs the entire
// pipeline against a fixture on disk and asserts the edges survive all the
// way to the final graph.Document — the same surface the daemon hands to
// MCP queries.
package main

import (
	"strings"
	"testing"
)

// TestDiscriminatorPipeline_EmitsEdgesEndToEnd indexes the discriminator
// fixture through the production Indexer and asserts that DISCRIMINATES_ON
// edges with the expected synthetic var:* targets are present in the final
// graph.Document. Guards against the #2670 meta-bug class where unit tests
// pass but real builds emit zero edges.
func TestDiscriminatorPipeline_EmitsEdgesEndToEnd(t *testing.T) {
	doc := runIndexerOn(t, "testdata/discriminator_fixture", "discriminator_fixture", nil)

	// Aggregate every DISCRIMINATES_ON edge in the final document so the
	// failure diagnostic can show what the pipeline actually produced.
	type edgeView struct {
		fromID, toID, literal, line, lang string
	}
	var hits []edgeView
	for _, r := range doc.Relationships {
		if r.Kind != "DISCRIMINATES_ON" {
			continue
		}
		hits = append(hits, edgeView{
			fromID:  r.FromID,
			toID:    r.ToID,
			literal: r.Properties["literal"],
			line:    r.Properties["line"],
			lang:    r.Properties["language"],
		})
	}

	if len(hits) == 0 {
		t.Logf("graph stats: entities=%d relationships=%d", len(doc.Entities), len(doc.Relationships))
		// Dump a sample of relationship kinds so a future regression points
		// straight at the missing pipeline stage rather than a generic
		// "no edges" failure.
		kindCounts := map[string]int{}
		for _, r := range doc.Relationships {
			kindCounts[r.Kind]++
		}
		t.Logf("relationship kinds in graph: %v", kindCounts)
		t.Fatalf("issue #2670: expected ≥1 DISCRIMINATES_ON edge from the production index pipeline, got 0 — the extractor or a downstream pass is dropping edges")
	}

	// We want at least one var:* edge per language fixture (TS + Python).
	// Both fixtures share the same identifiers (checklistType / checklist_type,
	// role, status) so simply assert ≥3 total edges and that at least one
	// of each language showed up.
	if len(hits) < 3 {
		t.Errorf("issue #2670: expected ≥3 DISCRIMINATES_ON edges across the fixture (one per discriminator literal × 2 languages), got %d: %+v", len(hits), hits)
	}

	wantTargets := map[string]bool{
		"var:checklistType":  false, // TS
		"var:role":           false, // TS + Python
		"var:status":         false, // TS + Python
		"var:checklist_type": false, // Python
	}
	for _, h := range hits {
		if _, ok := wantTargets[h.toID]; ok {
			wantTargets[h.toID] = true
		}
		// Every edge must carry the line and literal properties stamped by
		// the extractor (#2666). If the daemon's serializer drops unknown
		// edge properties the next regression would silently degrade the
		// inspect / find_callers surfaces.
		if h.literal == "" {
			t.Errorf("DISCRIMINATES_ON edge to %s missing Properties[literal]: %+v", h.toID, h)
		}
		if h.line == "" {
			t.Errorf("DISCRIMINATES_ON edge to %s missing Properties[line]: %+v", h.toID, h)
		}
	}
	missing := []string{}
	for k, seen := range wantTargets {
		if !seen {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		var sb strings.Builder
		for _, h := range hits {
			sb.WriteString("  ")
			sb.WriteString(h.toID)
			sb.WriteString(" literal=")
			sb.WriteString(h.literal)
			sb.WriteString(" line=")
			sb.WriteString(h.line)
			sb.WriteString("\n")
		}
		t.Errorf("issue #2670: expected DISCRIMINATES_ON edges to targets %v but missing %v.\nObserved edges:\n%s", wantTargets, missing, sb.String())
	}
}
