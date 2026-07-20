package mcp

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// buildSyntheticDoc fabricates a Document with roughly nEnt entities and
// ~3.75*nEnt relationships, with realistic names/paths/docstrings so the
// BM25 tokenizer does real work (mirrors a ~2k/7.5k repo).
func buildSyntheticDoc(nEnt int) *graph.Document {
	doc := &graph.Document{}
	kinds := []string{"function", "method", "class", "struct", "interface", "http_endpoint"}
	langs := []string{"go", "python", "typescript", "java"}
	doc.Entities = make([]graph.Entity, nEnt)
	for i := 0; i < nEnt; i++ {
		k := kinds[i%len(kinds)]
		doc.Entities[i] = graph.Entity{
			ID:            fmt.Sprintf("ent-%06d", i),
			Name:          fmt.Sprintf("handleOrderRequestForCustomer%dProcessor", i),
			QualifiedName: fmt.Sprintf("svc.orders.v%d.handleOrderRequestForCustomer%d", i%9, i),
			Kind:          k,
			Subtype:       k,
			SourceFile:    fmt.Sprintf("src/services/orders/sub%d/order_handler_%d.go", i%40, i),
			Language:      langs[i%len(langs)],
			StartLine:     i,
			EndLine:       i + 20,
		}.WithProperties(map[string]string{
			"docstring": fmt.Sprintf("Handles the order request for a customer by validating "+
				"the payload, persisting the order entity %d, and emitting a downstream "+
				"kafka event so the fulfilment pipeline can pick it up asynchronously.", i),
			"discriminators": fmt.Sprintf("checklistType=%d,orderKind=premium%d", i%5, i%3),
		},
		)
	}
	nRel := nEnt * 15 / 4 // ~3.75x
	doc.Relationships = make([]graph.Relationship, nRel)
	rkinds := []string{"CALLS", "IMPORTS", "TESTS", "STEP_IN_PROCESS", "REFERENCES"}
	for i := 0; i < nRel; i++ {
		doc.Relationships[i] = graph.Relationship{
			ID:     fmt.Sprintf("rel-%06d", i),
			FromID: fmt.Sprintf("ent-%06d", i%nEnt),
			ToID:   fmt.Sprintf("ent-%06d", (i*7+3)%nEnt),
			Kind:   rkinds[i%len(rkinds)],
		}
	}
	return doc
}

// TestReloadCostProfile is a (non-bench) profiling probe printing the
// per-component reload cost split for a realistic fixture. Run with:
//
//	go test ./internal/mcp/ -run TestReloadCostProfile -v
func TestReloadCostProfile(t *testing.T) {
	doc := buildSyntheticDoc(2000)
	dir := t.TempDir()
	fbPath := filepath.Join(dir, "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, doc); err != nil {
		t.Fatalf("write fb: %v", err)
	}

	const iters = 30
	var (
		parseTotal, labelTotal, bm25Total, byIDTotal, resetTotal time.Duration
	)
	for n := 0; n < iters; n++ {
		t0 := time.Now()
		parsed, err := readDocumentFromDir(dir)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		parseTotal += time.Since(t0)

		t1 := time.Now()
		li := BuildLabelIndex(parsed)
		labelTotal += time.Since(t1)

		t2 := time.Now()
		_ = BuildBM25(parsed)
		bm25Total += time.Since(t2)

		// ByID standalone (the lazy getter path, when LabelIndex absent).
		t3 := time.Now()
		m := make(map[string]*graph.Entity, len(parsed.Entities))
		for i := range parsed.Entities {
			m[parsed.Entities[i].ID] = &parsed.Entities[i]
		}
		byIDTotal += time.Since(t3)

		lr := &LoadedRepo{Doc: parsed, LabelIndex: li}
		t4 := time.Now()
		lr.resetIndexes()
		resetTotal += time.Since(t4)
	}

	// AFTER: a content-unchanged reload now pays only the FNV-1a hash of the
	// graph.fb bytes (content-hash skip) — measure it.
	var hashTotal time.Duration
	for n := 0; n < iters; n++ {
		t0 := time.Now()
		if _, err := hashGraphFile(fbPath); err != nil {
			t.Fatalf("hash: %v", err)
		}
		hashTotal += time.Since(t0)
	}

	parse := parseTotal / iters
	label := labelTotal / iters
	bm25 := bm25Total / iters
	byID := byIDTotal / iters
	reset := resetTotal / iters
	hash := hashTotal / iters
	total := parse + label + bm25 + reset // OLD eager reload
	afterChanged := parse + label + hash  // NEW reload, content CHANGED (BM25 now lazy)
	afterSame := hash                     // NEW reload, content UNCHANGED (skip)

	t.Logf("RELOAD COST PROFILE (2000 ent / 7500 rel, avg of %d):", iters)
	t.Logf("  Doc parse (LoadGraphFromDir) : %10v  (%4.1f%%)", parse, 100*float64(parse)/float64(total))
	t.Logf("  BuildLabelIndex              : %10v  (%4.1f%%)", label, 100*float64(label)/float64(total))
	t.Logf("  BuildBM25  *** DOMINATES ***  : %10v  (%4.1f%%)", bm25, 100*float64(bm25)/float64(total))
	t.Logf("  resetIndexes                 : %10v  (%4.1f%%)", reset, 100*float64(reset)/float64(total))
	t.Logf("  hashGraphFile (skip probe)   : %10v", hash)
	t.Logf("  (standalone ByID for ref)    : %10v", byID)
	t.Logf("  ---")
	t.Logf("  BEFORE  eager reload total           : %10v", total)
	t.Logf("  AFTER   reload, content CHANGED      : %10v  (%.1fx faster)", afterChanged, float64(total)/float64(afterChanged))
	t.Logf("  AFTER   reload, content UNCHANGED    : %10v  (%.1fx faster, hash-only skip)", afterSame, float64(total)/float64(afterSame))
}
