// Package javascript — unit tests for issue #2553.
//
// Verifies that const X: Record<string, Fn> = { a: fnA, b: fnB }; X[k]()
// dispatch patterns produce synthetic CALLS edges from the dispatch site to
// each registered handler, tagged with Properties["via"]="dynamic_dispatch_map".
//
// Two positive cases and one negative (literal-key) case are covered.
package javascript_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	tstypescript "github.com/smacker/go-tree-sitter/typescript/typescript"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// parseTSDispatch parses source with the TypeScript grammar.
func parseTSDispatch(t *testing.T, src []byte) ts.Tree {
	t.Helper()
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tstypescript.GetLanguage()))
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parseTSDispatch: %v", err)
	}
	return tree
}

// extractTSDispatch is a local helper that runs the typescript extractor on
// the given source and returns the entity slice.
func extractTSDispatch(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	content := []byte(src)
	tree := parseTSDispatch(t, content)
	ext, ok := extreg.Get("typescript")
	if !ok {
		t.Fatalf("typescript extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extreg.FileInput{
		Path:     "syncEngine.ts",
		Content:  content,
		Language: "typescript",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

// hasDispatchCallEdge returns true when entity named fromName has a CALLS
// relationship to toID with Properties["via"]="dynamic_dispatch_map".
func hasDispatchCallEdge(ents []types.EntityRecord, fromName, toID string) bool {
	for i := range ents {
		if ents[i].Name != fromName {
			continue
		}
		for _, r := range ents[i].Relationships {
			if r.Kind == "CALLS" && r.ToID == toID &&
				r.Properties != nil && r.Properties["via"] == "dynamic_dispatch_map" {
				return true
			}
		}
	}
	return false
}

// hasPlainCallEdge returns true when entity named fromName has a CALLS edge
// to toID WITHOUT the dynamic_dispatch_map via property.
func hasPlainCallEdge(ents []types.EntityRecord, fromName, toID string) bool {
	for i := range ents {
		if ents[i].Name != fromName {
			continue
		}
		for _, r := range ents[i].Relationships {
			if r.Kind == "CALLS" && r.ToID == toID {
				if r.Properties == nil || r.Properties["via"] == "" {
					return true
				}
			}
		}
	}
	return false
}

// countDispatchCallEdges returns the number of CALLS edges on fromName that
// carry Properties["via"]="dynamic_dispatch_map".
func countDispatchCallEdges(ents []types.EntityRecord, fromName string) int {
	for i := range ents {
		if ents[i].Name != fromName {
			continue
		}
		n := 0
		for _, r := range ents[i].Relationships {
			if r.Kind == "CALLS" && r.Properties != nil && r.Properties["via"] == "dynamic_dispatch_map" {
				n++
			}
		}
		return n
	}
	return 0
}

// TestTSExtractor_RecordDispatch_EmitsCallsEdges verifies that a dynamic
// dispatch through a Record<string, Fn> map produces one synthetic CALLS
// edge per registered handler, tagged with via="dynamic_dispatch_map".
//
// Fixture mirrors the syncEngine.ts / syncResolvers.ts pattern from the
// core-mobile offline-sync subsystem (issue #2553).
func TestTSExtractor_RecordDispatch_EmitsCallsEdges(t *testing.T) {
	src := `
import type { SyncAction } from './types';

function fnA(action: SyncAction): void {}
function fnB(action: SyncAction): void {}

const RESOLVERS: Record<string, (action: SyncAction) => void> = {
  create_deficiency: fnA,
  update_deficiency: fnB,
};

function processSyncQueue(action: SyncAction): void {
  RESOLVERS[action.kind](action);
}
`
	ents := extractTSDispatch(t, src)

	// The dispatcher must have synthetic CALLS edges to both handlers.
	if !hasDispatchCallEdge(ents, "processSyncQueue", "fnA") {
		t.Errorf("expected synthetic CALLS edge processSyncQueue→fnA (dynamic_dispatch_map), got: %v", relSummary(ents, "processSyncQueue"))
	}
	if !hasDispatchCallEdge(ents, "processSyncQueue", "fnB") {
		t.Errorf("expected synthetic CALLS edge processSyncQueue→fnB (dynamic_dispatch_map), got: %v", relSummary(ents, "processSyncQueue"))
	}

	// Both edges must be tagged.
	n := countDispatchCallEdges(ents, "processSyncQueue")
	if n != 2 {
		t.Errorf("expected 2 dynamic_dispatch_map CALLS edges from processSyncQueue, got %d", n)
	}
}

// TestTSExtractor_RecordDispatch_NoTypeAnnotation verifies that the heuristic
// also fires when there is no explicit Record<> annotation but all values in
// the object literal are plain identifier references.
func TestTSExtractor_RecordDispatch_NoTypeAnnotation(t *testing.T) {
	src := `
function handlerCreate() {}
function handlerDelete() {}

const ACTIONS = {
  create: handlerCreate,
  delete: handlerDelete,
};

function run(kind) {
  ACTIONS[kind]();
}
`
	ents := extractTSDispatch(t, src)

	if !hasDispatchCallEdge(ents, "run", "handlerCreate") {
		t.Errorf("expected synthetic CALLS edge run→handlerCreate, got: %v", relSummary(ents, "run"))
	}
	if !hasDispatchCallEdge(ents, "run", "handlerDelete") {
		t.Errorf("expected synthetic CALLS edge run→handlerDelete, got: %v", relSummary(ents, "run"))
	}
}

// TestTSExtractor_NoDynamicEdgesForLiteralAccess verifies that a literal-key
// subscript call RESOLVERS['create_deficiency']() resolves directly to the
// single matching handler rather than fanning out to all handlers. The edge
// must still carry via="dynamic_dispatch_map" for traceability.
func TestTSExtractor_NoDynamicEdgesForLiteralAccess(t *testing.T) {
	src := `
function fnA(action: any): void {}
function fnB(action: any): void {}

const RESOLVERS: Record<string, (action: any) => void> = {
  create_deficiency: fnA,
  update_deficiency: fnB,
};

function callSpecific(action: any): void {
  RESOLVERS['create_deficiency'](action);
}
`
	ents := extractTSDispatch(t, src)

	// Only fnA should be targeted (the literal-key match).
	if !hasDispatchCallEdge(ents, "callSpecific", "fnA") {
		t.Errorf("expected CALLS edge callSpecific→fnA for literal key, got: %v", relSummary(ents, "callSpecific"))
	}
	// fnB must NOT receive a CALLS edge from the literal lookup.
	if hasDispatchCallEdge(ents, "callSpecific", "fnB") {
		t.Errorf("did not expect CALLS edge callSpecific→fnB for literal key")
	}
	// Exactly one dynamic_dispatch_map edge from callSpecific.
	n := countDispatchCallEdges(ents, "callSpecific")
	if n != 1 {
		t.Errorf("expected exactly 1 dynamic_dispatch_map CALLS edge from callSpecific, got %d", n)
	}
}

// relSummary returns a compact string of the relationships on a named entity
// for use in test failure messages.
func relSummary(ents []types.EntityRecord, name string) string {
	for i := range ents {
		if ents[i].Name != name {
			continue
		}
		var parts []string
		for _, r := range ents[i].Relationships {
			via := ""
			if r.Properties != nil && r.Properties["via"] != "" {
				via = "[via=" + r.Properties["via"] + "]"
			}
			parts = append(parts, r.Kind+"→"+r.ToID+via)
		}
		if len(parts) == 0 {
			return "(no relationships)"
		}
		result := ""
		for j, p := range parts {
			if j > 0 {
				result += ", "
			}
			result += p
		}
		return result
	}
	return "(entity not found)"
}
