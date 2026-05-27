// Package javascript — unit tests for issue #2565.
//
// Verifies two dispatch-map gaps not covered by #2553:
//
//	(1) Inline arrow handlers: `const X = { foo: () => svc.foo() }; X[k]()`
//	    should emit CALLS from the dispatch site to svc.foo (the inner callee).
//
//	(2) Exported object (intra-file): `export const X = { foo: handler }; X[k]()`
//	    should emit CALLS from the dispatch site to handler (same as non-exported).
//
// Cross-file imports (gap (3)) are deferred.
package javascript_test

import (
	"testing"
)

// TestTSExtractor_RecordDispatch_InlineArrows verifies that a dispatch map
// whose values are inline arrow functions (not identifier references) still
// produces synthetic CALLS edges from the dispatch site to the callees inside
// each arrow body. Issue #2565 gap (1).
func TestTSExtractor_RecordDispatch_InlineArrows(t *testing.T) {
	src := `
import { svc } from './svc';

const HANDLERS = {
  foo: () => svc.foo(),
  bar: () => svc.bar(),
};

function dispatch(key: string): void {
  HANDLERS[key]();
}
`
	ents := extractTSDispatch(t, src)

	// The dispatcher must have synthetic CALLS edges to both inline callees.
	if !hasDispatchCallEdge(ents, "dispatch", "foo") {
		t.Errorf("expected synthetic CALLS edge dispatch→foo (dynamic_dispatch_map), got: %v", relSummary(ents, "dispatch"))
	}
	if !hasDispatchCallEdge(ents, "dispatch", "bar") {
		t.Errorf("expected synthetic CALLS edge dispatch→bar (dynamic_dispatch_map), got: %v", relSummary(ents, "dispatch"))
	}

	n := countDispatchCallEdges(ents, "dispatch")
	if n != 2 {
		t.Errorf("expected 2 dynamic_dispatch_map CALLS edges from dispatch, got %d", n)
	}
}

// TestTSExtractor_RecordDispatch_InlineArrows_LiteralKey verifies that a
// literal-key subscript call resolves to the single matching inline-arrow
// callee rather than fanning out to all handlers. Issue #2565 gap (1).
func TestTSExtractor_RecordDispatch_InlineArrows_LiteralKey(t *testing.T) {
	src := `
import { svc } from './svc';

const HANDLERS = {
  foo: () => svc.foo(),
  bar: () => svc.bar(),
};

function callFoo(): void {
  HANDLERS['foo']();
}
`
	ents := extractTSDispatch(t, src)

	// Only foo's inner callee should be targeted.
	if !hasDispatchCallEdge(ents, "callFoo", "foo") {
		t.Errorf("expected CALLS edge callFoo→foo for literal key, got: %v", relSummary(ents, "callFoo"))
	}
	// bar's callee must NOT receive an edge.
	if hasDispatchCallEdge(ents, "callFoo", "bar") {
		t.Errorf("did not expect CALLS edge callFoo→bar for literal key 'foo'")
	}
	n := countDispatchCallEdges(ents, "callFoo")
	if n != 1 {
		t.Errorf("expected exactly 1 dynamic_dispatch_map CALLS edge from callFoo, got %d", n)
	}
}

// TestTSExtractor_RecordDispatch_ExportedObject verifies that an exported
// const object literal is treated the same as a non-exported one — the
// dispatch CALLS edges are emitted when the object is used intra-file.
// Issue #2565 gap (2).
func TestTSExtractor_RecordDispatch_ExportedObject(t *testing.T) {
	src := `
function handlerA(): void {}
function handlerB(): void {}

export const ACTIONS = {
  create: handlerA,
  delete: handlerB,
};

function run(kind: string): void {
  ACTIONS[kind]();
}
`
	ents := extractTSDispatch(t, src)

	if !hasDispatchCallEdge(ents, "run", "handlerA") {
		t.Errorf("expected synthetic CALLS edge run→handlerA (dynamic_dispatch_map), got: %v", relSummary(ents, "run"))
	}
	if !hasDispatchCallEdge(ents, "run", "handlerB") {
		t.Errorf("expected synthetic CALLS edge run→handlerB (dynamic_dispatch_map), got: %v", relSummary(ents, "run"))
	}
	n := countDispatchCallEdges(ents, "run")
	if n != 2 {
		t.Errorf("expected 2 dynamic_dispatch_map CALLS edges from run, got %d", n)
	}
}

// TestTSExtractor_RecordDispatch_ExportedObject_LiteralKey verifies that a
// literal-key subscript access on an exported const object resolves to the
// single matching handler. Issue #2565 gap (2).
func TestTSExtractor_RecordDispatch_ExportedObject_LiteralKey(t *testing.T) {
	src := `
function handlerA(): void {}
function handlerB(): void {}

export const ACTIONS = {
  create: handlerA,
  delete: handlerB,
};

function runCreate(): void {
  ACTIONS['create']();
}
`
	ents := extractTSDispatch(t, src)

	if !hasDispatchCallEdge(ents, "runCreate", "handlerA") {
		t.Errorf("expected CALLS edge runCreate→handlerA for literal key, got: %v", relSummary(ents, "runCreate"))
	}
	if hasDispatchCallEdge(ents, "runCreate", "handlerB") {
		t.Errorf("did not expect CALLS edge runCreate→handlerB for literal key 'create'")
	}
	n := countDispatchCallEdges(ents, "runCreate")
	if n != 1 {
		t.Errorf("expected exactly 1 dynamic_dispatch_map CALLS edge from runCreate, got %d", n)
	}
}
