package kotlin_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// ktResolveFieldTypeEdges drives the REAL resolver over an extracted record set
// and returns (bound, dangling) counts for field→declared-type edges.
//
// This is the gate the whole issue rests on: a non-binding edge is WORSE than no
// edge, because an unresolved stub is kept verbatim, dangles, and is classified
// `bug-extractor`. Reading refs.go is not evidence; arms D, F, G and H each
// drove it, and two of them found the rule they had reasoned to was wrong.
func ktResolveFieldTypeEdges(t *testing.T, recs []types.EntityRecord) (bound, dangling int) {
	t.Helper()
	for i := range recs {
		if recs[i].ID == "" {
			recs[i].ID = graph.EntityID("issue6912", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
		}
	}
	idx := resolve.BuildIndex(recs)
	ids := map[string]bool{}
	for i := range recs {
		ids[recs[i].ID] = true
	}
	resolve.ReferencesEmbedded(recs, idx)
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "REFERENCES" || r.Properties.Get("ref_kind") != "field_target_type" {
				continue
			}
			if ids[r.ToID] {
				bound++
			} else {
				t.Logf("DANGLING: %s -> %s", recs[i].Name, r.ToID)
				dangling++
			}
		}
	}
	return bound, dangling
}

// TestKotlinFieldTypeRefs_ResolverBindsTheTypeAliasTierToo grades the SECOND
// tier separately. A SCOPE.Schema/type_alias target is addressed with the same
// component-space structural ref but is NOT visible to lookupLocationKind's
// componentKindFamily filter, so it can only bind through ambigLocation. If that
// fall-through did not exist, this edge would dangle while every Component edge
// bound — and the arm would look perfect.
func TestKotlinFieldTypeRefs_ResolverBindsTheTypeAliasTierToo(t *testing.T) {
	src := `package p

typealias Handler = (String) -> Unit

class Holder {
    val h: Handler = x
}
`
	recs := ktExtract(t, "H.kt", src)
	edges := ktFieldTypeEdges(recs)
	if len(edges) != 1 {
		t.Fatalf("got %d edges, want 1: %v", len(edges), edges)
	}
	bound, dangling := ktResolveFieldTypeEdges(t, recs)
	if bound != 1 || dangling != 0 {
		t.Fatalf("typealias tier: bound=%d dangling=%d, want 1/0", bound, dangling)
	}
}

// TestKotlinFieldTypeRefs_RefusedAliasEdgeWouldActuallyDangle proves the REASON
// for the alias tier's wider count instead of asserting it.
//
// Arms C, D and E all carry a justification comment saying a refusal "would
// guarantee a dangling stub", and arm E found that claim FALSE in general by
// hand-emitting the refused edge and watching it bind. So the refusal is
// checked the same way here: the edge the pass declines is emitted by hand and
// driven through the real resolver.
//
// The Component mirror in the same record set is what makes the row mean
// something: the SAME rival (a top-level `fun Handler`) leaves a Component
// target binding, so the difference is the TIER, not the rival.
func TestKotlinFieldTypeRefs_RefusedAliasEdgeWouldActuallyDangle(t *testing.T) {
	aliasSrc := `package p

typealias Handler = (String) -> Unit

fun Handler(): Int = 1

class Holder {
    val h: Handler = x
}
`
	compSrc := `package p

class Handler

fun Handler(): Int = 1

class Holder {
    val h: Handler = x
}
`
	for _, tc := range []struct {
		label    string
		src      string
		wantBind bool
	}{
		{"typealias target, rival out of family", aliasSrc, false},
		{"component target, same rival", compSrc, true},
	} {
		t.Run(tc.label, func(t *testing.T) {
			recs := ktExtract(t, "H.kt", tc.src)
			// Hand-emit the edge the pass may have declined, so both rows
			// drive the resolver with exactly one candidate edge.
			if len(ktFieldTypeEdges(recs)) == 0 {
				for i := range recs {
					if recs[i].Name != "Holder.h" {
						continue
					}
					recs[i].Relationships = append(recs[i].Relationships, types.RelationshipRecord{
						ToID: "scope:component:class:kotlin:H.kt:Handler",
						Kind: "REFERENCES",
						Properties: types.Props{
							{K: "ref_kind", V: "field_target_type"},
						},
					})
				}
			}
			bound, dangling := ktResolveFieldTypeEdges(t, recs)
			if tc.wantBind && (bound != 1 || dangling != 0) {
				t.Fatalf("%s: bound=%d dangling=%d, want 1/0", tc.label, bound, dangling)
			}
			if !tc.wantBind && (bound != 0 || dangling != 1) {
				t.Fatalf("%s: bound=%d dangling=%d, want 0/1 — the refusal's "+
					"stated reason does not hold", tc.label, bound, dangling)
			}
		})
	}
}
