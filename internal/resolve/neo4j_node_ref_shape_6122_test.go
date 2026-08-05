package resolve

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6122 — the resolver facts the neo4j `node:` cluster fix rests on.
//
// The Cypher-string neo4j extractors mint a synthetic node-label entity named
// `node:<label>` with no QualifiedName, and address it with `ToID:
// "node:"+label`. LookupStatusHint reaches byName through splitStub
// (refs.go:2658), which cuts at the FIRST colon and probes with the REMAINDER,
// so the ref can only ever probe `byName["<label>"]` — a key the intended target
// does not carry, and one a real code symbol very often does.
//
// These tests pin THREE facts, so that a later resolver change cannot silently
// take the producer fix's ground away, and so that the design choice is
// falsifiable rather than asserted:
//
//  1. `node:<label>` mis-binds to a same-named code symbol. (The defect.)
//  2. `Class:<label>` — the form the WORKING java/python/ruby siblings use —
//     mis-binds in exactly the same way HERE. That is the measured reason the
//     five Cypher extractors must NOT converge on the siblings' ref string:
//     the siblings' targets are real classes whose entity Name is the BARE
//     class name, so their leaf probe hits the right node. A Cypher label's
//     target is a synthetic `node:`-namespaced entity, so the same leaf probe
//     hits the code symbol instead.
//  3. `scope:schema:<file>#<name>` — the PLT #537 short-form at refs.go:1929 —
//     binds `node:<label>` correctly, with the collider present, because it
//     probes byLocation[file][Name] with the FULL name including its colon.
//
// Neither asserts a count; all assert which entity an endpoint lands on.
//
// NOT a test of the fix itself — the behavioural gate for that is
// cmd/grafel.TestNeo4jGraphRelatesBindsTheNodeEntityNotTheCollider6122.

// neo4jColliderSet6122 returns the two entities every subtest below shares: the
// synthetic Cypher node-label entity, and a real code symbol of the same bare
// leaf name in the same file. The collider is the whole point — "does not
// mis-bind" is only meaningful when something wrong is available to bind to.
func neo4jColliderSet6122() (node, collider types.EntityRecord) {
	node = entAt("1111000011110000", "SCOPE.Schema", "node:Movie", "store/store.go")
	collider = entAt("2222000022220000", "SCOPE.Component", "Movie", "store/store.go")
	return node, collider
}

// TestNodeLabelRefCannotAddressANodeEntityAndMisbinds6122 shows the old ref
// shape was unusable in BOTH directions: the intended target is present and is
// NOT what the ref binds to.
func TestNodeLabelRefCannotAddressANodeEntityAndMisbinds6122(t *testing.T) {
	node, collider := neo4jColliderSet6122()
	idx := BuildIndex([]types.EntityRecord{node, collider})
	rels := []types.RelationshipRecord{
		{FromID: "3333000033330000", ToID: "node:Movie", Kind: "GRAPH_RELATES"},
	}
	References(rels, idx)

	if rels[0].ToID == node.ID {
		t.Fatalf("`node:Movie` now binds to the node entity by Name — splitStub no " +
			"longer eats the prefix. The #6122 producer fix rests on it doing so; " +
			"re-check internal/custom/*/neo4j.go before relaxing anything here.")
	}
	if rels[0].ToID != collider.ID {
		t.Fatalf("`node:Movie` bound to %q, want the SCOPE.Component collider %q — this "+
			"test is meant to DEMONSTRATE the mis-bind, so if the shape has changed the "+
			"demonstration must be rebuilt, not deleted", rels[0].ToID, collider.ID)
	}
}

// TestClassRefWouldMisbindForACypherLabel6122 is the measurement that decides
// the design question: should the five Cypher extractors converge on the
// `"Class:"+label` form their java/python/ruby siblings use?
//
// No, and this is why. `Class:` resolves for the siblings only because THEIR
// target entity Name is the bare class name (java/neo4j.go emitNode,
// ruby/neo4j_activegraph.go:188, python/neo4j_neomodel.go:247) — the leaf probe
// lands on it. Point the same ref at a Cypher label and the leaf probe lands on
// the code symbol instead, which is the identical defect wearing a different
// prefix. Convergence on the ref STRING would be convergence on a bug; the two
// families address structurally different targets.
//
// Renaming the synthetic entity to the bare label to make `Class:` work is the
// other half of that trade and is worse still: the `node:` prefix exists
// precisely to keep a database label recovered from query text from colliding
// with a code symbol, and dropping it moves the collision from the edge to the
// entity.
func TestClassRefWouldMisbindForACypherLabel6122(t *testing.T) {
	node, collider := neo4jColliderSet6122()
	idx := BuildIndex([]types.EntityRecord{node, collider})
	rels := []types.RelationshipRecord{
		{FromID: "3333000033330000", ToID: "Class:Movie", Kind: "GRAPH_RELATES"},
	}
	References(rels, idx)

	if rels[0].ToID != collider.ID {
		t.Fatalf("`Class:Movie` bound to %q, want the collider %q. This test records WHY "+
			"the five Cypher extractors do not adopt the siblings' ref form; if the "+
			"resolver now sends `Class:` somewhere else, re-argue the design choice in "+
			"internal/extractor/neo4j_node.go before changing it", rels[0].ToID, collider.ID)
	}
}

// TestNeo4jNodeLocationRefBindsTheNodeEntity6122 is the property the fix relies
// on: the PLT #537 `scope:schema:<file>#<name>` short-form probes
// byLocation[file][Name] with the full, colon-bearing entity Name, so it reaches
// a target `node:<label>` that no byName tier can address — and it declines
// rather than falling through to the collider when the target is absent.
func TestNeo4jNodeLocationRefBindsTheNodeEntity6122(t *testing.T) {
	node, collider := neo4jColliderSet6122()
	const ref = "scope:schema:store/store.go#node:Movie"

	t.Run("node entity present: binds to it, not the collider", func(t *testing.T) {
		idx := BuildIndex([]types.EntityRecord{node, collider})
		rels := []types.RelationshipRecord{
			{FromID: "3333000033330000", ToID: ref, Kind: "GRAPH_RELATES"},
		}
		References(rels, idx)
		if rels[0].ToID != node.ID {
			t.Fatalf("location ref bound to %q, want the node entity %q", rels[0].ToID, node.ID)
		}
	})

	t.Run("structural tier consumes a miss and declines", func(t *testing.T) {
		// Asserted IN-PACKAGE and DIRECTLY. The end-to-end arm below cannot
		// distinguish "lookupStructural consumed the stub" from "the byName
		// probe happened to miss", and a guard that is covered twice looks
		// alive when only one mechanism is doing the work (the #6123 lesson).
		idx := BuildIndex([]types.EntityRecord{collider})
		id, status, handled := idx.lookupStructural(ref)
		if !handled {
			t.Fatalf("lookupStructural did not claim %q (handled=%v) — a `scope:`-prefixed "+
				"stub that is not six segments must be consumed there, not passed down to "+
				"the byName / kind-hint tiers where the collider lives", ref, handled)
		}
		if status != statusUnmatched || id != "" {
			t.Fatalf("lookupStructural returned (%q, status=%d), want (\"\", statusUnmatched=%d)",
				id, status, statusUnmatched)
		}
	})

	t.Run("node entity absent: stays verbatim, does not take the collider", func(t *testing.T) {
		idx := BuildIndex([]types.EntityRecord{collider})
		rels := []types.RelationshipRecord{
			{FromID: "3333000033330000", ToID: ref, Kind: "GRAPH_RELATES"},
		}
		References(rels, idx)
		if rels[0].ToID != ref {
			t.Fatalf("location ref was rewritten to %q; it must be left verbatim so the "+
				"edge dangles honestly rather than taking the collider", rels[0].ToID)
		}
	})

	// THE BOUNDARY, and the reason Neo4jNodeTargetID refuses colon-bearing
	// inputs instead of merely documenting them.
	//
	// The short-form at refs.go:1932 guards `!strings.Contains(filePath, ":")`
	// and, on a decline OR a byLocation miss, execution falls through to the
	// Format A parse at refs.go:2037, which fires at EXACTLY six segments with
	// parts[4] read as a file path and parts[5] as an entity name. Two colons in
	// the file path reach six. This is the same trap the #6123 fix walked into
	// in its own output; here the producer bound is what keeps real refs out of
	// it.
	t.Run("six segments: the resolver DOES mis-bind, hence the producer bound", func(t *testing.T) {
		const hazard = "scope:schema:a:b:c#node:Movie"
		victim := entAt("8888000088880000", "Class", "Movie", "c#node")
		idx := BuildIndex([]types.EntityRecord{victim, collider})
		id, status, handled := idx.lookupStructural(hazard)
		if !handled || status != statusRewritten || id != victim.ID {
			t.Fatalf("six-segment schema stub resolved to (%q, status=%d, handled=%v); this "+
				"test documents that it MIS-BINDS to the entity named parts[5] in the file "+
				"named parts[4]. If the resolver now declines it, Neo4jNodeTargetID's colon "+
				"refusal may be relaxed — re-measure before doing so", id, status, handled)
		}
	})
}
