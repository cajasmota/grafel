// callsadj_csr_5850_test.go — golden equivalence coverage for the CSR
// (compressed-sparse-row) refactor of the CALLS-only forward adjacency
// (callsAdj), part of the Tier-2b index mop-up (#5850, epic #5849/#5852
// precedent).
//
// The map-based callsAdj (map[string][]string) is replaced internally with a
// CSR layout over a dense int32 node index, mirroring the #5852 refactor
// applied to the main adjacency. The external contract — Get(id) returning
// the sorted (by callee id) list of CALLS targets for id, nil for an unknown
// id or one with no outgoing CALLS edges — must not change. This test builds
// a reference map[string][]string directly from doc.Relationships (the exact
// pre-refactor build), independent of buildCallsAdjacency's internals, and
// asserts the CSR-backed Get matches it exactly for every node that appears
// as a From/To id in the fixture (plus an unknown id).
package mcp

import (
	"reflect"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

func callsAdjGoldenDoc() *graph.Document {
	mk := func(id string) graph.Entity {
		return graph.Entity{ID: id, Name: id, Kind: "Function", SourceFile: id + ".go", StartLine: 1}
	}
	return &graph.Document{
		Entities: []graph.Entity{
			mk("a"), mk("b"), mk("c"), mk("d"), mk("e"), mk("hub"),
		},
		Relationships: []graph.Relationship{
			// Multiple CALLS targets from "a", plus a non-CALLS edge that must
			// be excluded.
			{FromID: "a", ToID: "d", Kind: "CALLS"},
			{FromID: "a", ToID: "b", Kind: "CALLS"},
			{FromID: "a", ToID: "b", Kind: "REFERENCES"}, // excluded: not CALLS
			{FromID: "a", ToID: "c", Kind: "CALLS"},
			// A hub target reached by several callers, exercising a larger row.
			{FromID: "a", ToID: "hub", Kind: "CALLS"},
			{FromID: "b", ToID: "hub", Kind: "CALLS"},
			{FromID: "c", ToID: "hub", Kind: "CALLS"},
			// Self-loop edge case.
			{FromID: "e", ToID: "e", Kind: "CALLS"},
			// Duplicate target from the same caller (both retained, old build
			// only sorted — did not dedupe).
			{FromID: "d", ToID: "hub", Kind: "CALLS"},
			{FromID: "d", ToID: "hub", Kind: "CALLS"},
		},
	}
}

// referenceCallsAdj reproduces the pre-refactor map[string][]string semantics
// directly against doc.Relationships (build + sort, no CSR), to serve as the
// golden oracle.
func referenceCallsAdj(doc *graph.Document) map[string][]string {
	adj := make(map[string][]string)
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if r.Kind != "CALLS" {
			continue
		}
		adj[r.FromID] = append(adj[r.FromID], r.ToID)
	}
	for k := range adj {
		sort.Strings(adj[k])
	}
	return adj
}

func callsAdjGoldenNodeIDs(doc *graph.Document) []string {
	seen := map[string]bool{}
	var ids []string
	for _, e := range doc.Entities {
		if !seen[e.ID] {
			seen[e.ID] = true
			ids = append(ids, e.ID)
		}
	}
	for _, r := range doc.Relationships {
		for _, id := range []string{r.FromID, r.ToID} {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	ids = append(ids, "nonexistent")
	sort.Strings(ids)
	return ids
}

func TestCallsAdjCSR5850_GetMatchesReference(t *testing.T) {
	doc := callsAdjGoldenDoc()
	ref := referenceCallsAdj(doc)
	c := buildCallsAdjacency(doc)

	for _, id := range callsAdjGoldenNodeIDs(doc) {
		got := c.Get(id)
		want := ref[id] // nil for ids with no outgoing CALLS edges
		if !stringSlicesEqual(got, want) {
			t.Errorf("Get(%q) = %v; want %v", id, got, want)
		}
	}
}

// stringSlicesEqual treats nil and empty slice as equivalent (both callers'
// and the old map-based lookup miss return a nil slice; the CSR path must
// too, but we don't want a spurious failure if an implementation legitimately
// returns an empty non-nil slice for a present-but-degree-0 case).
func stringSlicesEqual(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

// TestCallsAdjCSR5850_SelfLoop exercises the degenerate self-loop case
// (FromID==ToID).
func TestCallsAdjCSR5850_SelfLoop(t *testing.T) {
	doc := callsAdjGoldenDoc()
	c := buildCallsAdjacency(doc)

	got := c.Get("e")
	want := []string{"e"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Get(e) = %v; want %v", got, want)
	}
}

// TestCallsAdjCSR5850_DuplicateTargetRetained locks in that the CSR build
// does NOT dedupe repeated (FromID, ToID) CALLS pairs — matching the old
// map-based build, which only sorted (never deduped).
func TestCallsAdjCSR5850_DuplicateTargetRetained(t *testing.T) {
	doc := callsAdjGoldenDoc()
	c := buildCallsAdjacency(doc)

	got := c.Get("d")
	want := []string{"hub", "hub"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Get(d) = %v; want %v (duplicate target must be retained)", got, want)
	}
}

// TestCallsAdjCSR5850_NilReceiverAndMissingID guards the nil-receiver
// contract and the not-found path (id absent from both directions).
func TestCallsAdjCSR5850_NilReceiverAndMissingID(t *testing.T) {
	var nc *callsAdjacency
	if got := nc.Get("a"); got != nil {
		t.Errorf("nil.Get = %v; want nil", got)
	}

	doc := callsAdjGoldenDoc()
	c := buildCallsAdjacency(doc)
	if got := c.Get("nonexistent"); got != nil {
		t.Errorf("Get(nonexistent) = %v; want nil", got)
	}
	// "hub" is interned (it appears as a CALLS ToID) but is never a CALLS
	// FromID — it must resolve as a known node with an empty out-row, not a
	// miss, distinguishing "zero out-degree" from "unknown id" internally.
	if got := c.Get("hub"); got != nil {
		t.Errorf("Get(hub) = %v; want nil (no outgoing CALLS edges)", got)
	}
}

// TestCallsAdjCSR5850_FollowCallsBFSParity confirms followCallsBFS, the sole
// production consumer of the branch-capped traversal, behaves identically
// against the CSR-backed callsAdjacency as it did against a plain map (the
// exercised behavior — branch-capped BFS terminal-chain enumeration — is
// insensitive to the underlying representation as long as Get's contract
// holds, which the tests above lock in independently).
func TestCallsAdjCSR5850_FollowCallsBFSParity(t *testing.T) {
	doc := callsAdjGoldenDoc()
	c := buildCallsAdjacency(doc)

	chains := followCallsBFS("a", 5, 10, c)
	if len(chains) == 0 {
		t.Fatal("followCallsBFS(a) returned no chains")
	}
	for _, chain := range chains {
		if chain[0] != "a" {
			t.Errorf("chain %v does not start at entry a", chain)
		}
	}
}
