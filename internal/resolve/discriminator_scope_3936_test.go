package resolve

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #3936 — a scope-local synthetic DISCRIMINATES_ON stub ("var:order",
// emitted by the pymongo/JS discriminator extractors for a local sort-key /
// compared variable) MUST NOT cross-resolve into a same-named global entity
// of an UNRELATED kind. The canonical false edge: a pymongo aggregation
// builder's `var:order` sort key binding to an OpenAPI `order` query-param
// node from open-api/buildings.yml — a bare-leaf-name collision across the
// code↔spec boundary.
//
// These tests assert the false edge is GONE (the var: stub stays verbatim /
// unresolved, no cross-boundary edge) AND that a legitimate DISCRIMINATES_ON
// to a real same-scope discriminator field still resolves.

// openAPIParam models the spec-side `order` query-param node that lives in
// open-api/buildings.yml: an entity of a different kind than any local
// variable, indexed under the global byName index by its bare leaf name.
func openAPIParam(id, name string) types.EntityRecord {
	return types.EntityRecord{
		ID:         id,
		Kind:       "SCOPE.Component",
		Name:       name,
		SourceFile: "open-api/buildings.yml",
	}
}

// TestDiscriminator_VarStub_DoesNotBindToOpenAPIParam is the #3936 regression:
// the ONLY global entity named "order" is the OpenAPI param node. A
// DISCRIMINATES_ON edge whose ToID is the scope-local synthetic "var:order"
// must stay unresolved — it must NOT bind to that spec node.
func TestDiscriminator_VarStub_DoesNotBindToOpenAPIParam(t *testing.T) {
	entities := []types.EntityRecord{
		// the spec-side OpenAPI `order` query param (the false-edge target)
		openAPIParam("0123456789abcdef", "order"),
		// the pymongo aggregation builder that emits the discriminator edge
		entAt("fedcba9876543210", "SCOPE.Operation", "build_pipeline", "app/agg.py"),
	}
	idx := BuildIndex(entities)
	rels := []types.RelationshipRecord{{
		FromID: "fedcba9876543210",
		ToID:   "var:order",
		Kind:   "DISCRIMINATES_ON",
	}}
	stats := References(rels, idx)

	// The false edge must be GONE: the var: stub is preserved verbatim, NOT
	// rewritten to the OpenAPI param's hex ID.
	if rels[0].ToID == "0123456789abcdef" {
		t.Fatalf("FALSE EDGE: var:order cross-resolved to the OpenAPI `order` param node")
	}
	if rels[0].ToID != "var:order" {
		t.Fatalf("var:order should stay verbatim/unresolved, got %q", rels[0].ToID)
	}
	if stats.Rewritten != 0 {
		t.Fatalf("expected 0 rewrites for a scope-local var stub, got %d", stats.Rewritten)
	}
}

// TestDiscriminator_VarStub_ClassifiedDynamic asserts the unresolved var:
// stub lands in DispositionDynamic (scope-local by design), NOT a bug bucket.
func TestDiscriminator_VarStub_ClassifiedDynamic(t *testing.T) {
	entities := []types.EntityRecord{openAPIParam("0123456789abcdef", "order")}
	idx := BuildIndex(entities)
	d := idx.classifyDispositionLang("var:order", "var:order", "python", nil)
	if d != DispositionDynamic {
		t.Fatalf("var:order disposition = %v, want DispositionDynamic", d)
	}
}

// TestDiscriminator_VarStub_LookupGuards asserts both resolver entry points
// (Lookup and LookupStatusHint) refuse to cross-resolve a var: stub even when
// a same-named global entity exists.
func TestDiscriminator_VarStub_LookupGuards(t *testing.T) {
	entities := []types.EntityRecord{openAPIParam("0123456789abcdef", "order")}
	idx := BuildIndex(entities)

	if id, ok := idx.Lookup("var:order"); ok {
		t.Fatalf("Lookup(var:order) cross-resolved to %q, want miss", id)
	}
	if id, st := idx.LookupStatusHint("var:order", "DISCRIMINATES_ON"); st == statusRewritten {
		t.Fatalf("LookupStatusHint(var:order) rewrote to %q, want non-rewritten", id)
	}
}

// TestDiscriminator_LegitimateFieldEdge_StillResolves asserts the TRUE edge
// survives: a DISCRIMINATES_ON edge that legitimately targets a real
// same-scope discriminator field (addressed by a concrete structural ref, the
// shape the resolver IS meant to bind) still resolves to that field's entity.
// The var: guard only short-circuits the scope-local synthetic shape; it must
// not touch concrete structural refs.
func TestDiscriminator_LegitimateFieldEdge_StillResolves(t *testing.T) {
	// A real discriminator field entity defined in the same file as the
	// operation that branches on it. Addressed by a Format A structural ref.
	field := types.EntityRecord{
		ID:         "aaaaaaaaaaaaaaaa",
		Kind:       "SCOPE.Operation",
		Name:       "status",
		SourceFile: "app/models.py",
	}
	// An unrelated global entity also named "status" to prove the resolution
	// is scope/structural, not a lucky bare-name hit.
	other := openAPIParam("bbbbbbbbbbbbbbbb", "status")
	idx := BuildIndex([]types.EntityRecord{field, other})

	rels := []types.RelationshipRecord{{
		FromID: "cccccccccccccccc",
		ToID:   "scope:operation:method:python:app/models.py:status",
		Kind:   "DISCRIMINATES_ON",
	}}
	stats := References(rels, idx)

	if rels[0].ToID != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("TRUE EDGE LOST: legitimate same-scope discriminator field did not resolve, got %q", rels[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite for the legitimate field edge, got %d", stats.Rewritten)
	}
}
