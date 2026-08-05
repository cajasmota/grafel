package sresolver_test

import (
	"fmt"
	"testing"

	"github.com/cajasmota/grafel/internal/extractors/sresolver"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// Cross-resolver AGREEMENT gate for the leaf-name tiers (#6141, refs #6129).
//
// Why this exists, and why it is not the #6129 corpus gate
// ───────────────────────────────────────────────────────
// internal/resolve (full rebuild) and internal/extractors/sresolver
// (incremental) are separate code with separate indexes that must bind the
// same source identically. #6141 shipped a tier reordering to both; the
// first attempt got sresolver's order wrong and a full rebuild and an
// incremental run bound the same call to DIFFERENT entities.
//
// The #6129 content-parity gate in cmd/grafel is the corpus-level instrument
// for that class, and it PASSED while the divergence was live: its fixture
// keeps the competing member in a different file from the caller, so it
// never reaches the shape that separates the two orderings.
//
// Extending that fixture was attempted and is blocked by an unrelated,
// undiagnosed divergence: adding ANY class to its delta file — a bare
// `class C: def m(self): return 1`, with no #6141 shape at all — makes the
// full rebuild classify it `Controller` and the incremental run
// `SCOPE.Component`, producing four spurious CONTAINS divergences. That is
// a genuine pre-existing defect in the incremental path (classes in STATIC
// files classify identically on both sides), but it needs its own issue
// before the gate's allow-list can name it, and that gate's rules forbid an
// allow entry without a filed issue.
//
// So the invariant is pinned HERE instead, directly and at unit cost. This
// is a tighter guard than the corpus gate for this specific defect: it
// drives both resolvers over one entity set and asserts they agree on the
// bound target BY CONTENT.
//
// If you add a leaf-name tier, a preference, or a reordering to either
// resolver, add its shape to agreementShapes below.

// agreementShape is one fixture: a caller with a bare CALLS stub, plus the
// members that compete for that leaf name.
type agreementShape struct {
	name string
	// members are (name, file, kind) triples; the caller is always
	// "Local07" in callerFile.
	callerFile string
	members    [][3]string
	stub       string
}

var agreementShapes = []agreementShape{
	{
		// THE shape that separates within-tier from across-tiers ordering,
		// and the one the #6129 corpus fixture cannot reach. A field in the
		// caller's OWN file versus an operation in a SIBLING file.
		name:       "caller_file_field_beside_sibling_file_operation",
		callerFile: "svc/a.go",
		members: [][3]string{
			{"Other.owner", "svc/a.go", "SCOPE.Schema"},
			{"Owned.owner", "svc/b.go", "SCOPE.Operation"},
		},
		stub: "owner",
	},
	{
		// The recall half: a sibling FIELD must not shadow a sibling
		// OPERATION.
		name:       "sibling_field_must_not_shadow_sibling_operation",
		callerFile: "svc/a.go",
		members: [][3]string{
			{"Owned.owner", "svc/b.go", "SCOPE.Operation"},
			{"Other.owner", "svc/c.go", "SCOPE.Schema"},
		},
		stub: "owner",
	},
	{
		// The precision gap: only a field exists, both must still bind it
		// (the Ruby attr_accessor / JS function-field constraint).
		name:       "field_only_still_binds",
		callerFile: "svc/a.go",
		members:    [][3]string{{"Other.owner", "svc/c.go", "SCOPE.Schema"}},
		stub:       "owner",
	},
	{
		// Two operations must stay ambiguous in BOTH resolvers.
		name:       "two_operations_stay_ambiguous",
		callerFile: "svc/a.go",
		members: [][3]string{
			{"RepoA.find", "svc/b.go", "SCOPE.Operation"},
			{"RepoB.find", "svc/c.go", "SCOPE.Operation"},
		},
		stub: "find",
	},
	{
		// Same-file operation beats same-file field: the operation
		// preference inside the file tier.
		name:       "same_file_operation_beats_same_file_field",
		callerFile: "svc/a.go",
		members: [][3]string{
			{"Owned.owner", "svc/a.go", "SCOPE.Operation"},
			{"Other.owner", "svc/a.go", "SCOPE.Schema"},
		},
		stub: "owner",
	},
	{
		// Caller-file OPERATION beside a sibling-file operation: locality
		// wins, and it must win identically in both.
		name:       "caller_file_operation_beside_sibling_file_operation",
		callerFile: "svc/a.go",
		members: [][3]string{
			{"Owned.owner", "svc/a.go", "SCOPE.Operation"},
			{"Sib.owner", "svc/b.go", "SCOPE.Operation"},
		},
		stub: "owner",
	},
}

// TestLeafTier_FullAndIncrementalResolversAgree_6141 drives both resolvers
// over each shape and fails when they bind the same stub differently.
func TestLeafTier_FullAndIncrementalResolversAgree_6141(t *testing.T) {
	for _, sh := range agreementShapes {
		t.Run(sh.name, func(t *testing.T) {
			const callerID = "1111111111111111"

			// Describe the world once; feed both resolvers from it so a
			// divergence can only come from the resolvers themselves.
			type member struct{ id, name, file, kind string }
			members := make([]member, 0, len(sh.members))
			for i, m := range sh.members {
				members = append(members, member{
					id:   fmt.Sprintf("%016x", i+2),
					name: m[0], file: m[1], kind: m[2],
				})
			}

			// ---- full-rebuild resolver (internal/resolve) ----
			recs := []types.EntityRecord{{
				ID: callerID, Kind: "SCOPE.Operation", Name: "Local07",
				SourceFile: sh.callerFile,
				Relationships: []types.RelationshipRecord{{
					FromID: callerID, ToID: sh.stub, Kind: "CALLS",
				}},
			}}
			for _, m := range members {
				recs = append(recs, types.EntityRecord{
					ID: m.id, Kind: m.kind, Name: m.name, SourceFile: m.file,
				})
			}
			idx := resolve.BuildIndex(recs)
			resolve.ReferencesEmbedded(recs, idx)
			full := recs[0].Relationships[0].ToID

			// ---- incremental resolver (sresolver) ----
			fresh := []graph.Entity{lfEnt(callerID, "Local07", sh.callerFile, "SCOPE.Operation")}
			var existing []graph.Entity
			for _, m := range members {
				e := lfEnt(m.id, m.name, m.file, m.kind)
				// Members in the caller's own file are part of the delta.
				if m.file == sh.callerFile {
					fresh = append(fresh, e)
					continue
				}
				existing = append(existing, e)
			}
			newRels := []graph.Relationship{mtCallRel(callerID, sh.stub, "")}
			res := sresolver.ResolveScoped(fresh, existing, newRels, nil, nil)
			incr := mtOnlyCall(t, res.ResolvedNewRelationships, callerID).ToID

			if full != incr {
				describe := func(id string) string {
					for _, m := range members {
						if m.id == id {
							return fmt.Sprintf("%s [%s] @ %s", m.name, m.kind, m.file)
						}
					}
					return fmt.Sprintf("«unbound»%s", id)
				}
				t.Fatalf("full rebuild and incremental disagree on bare CALLS %q:\n"+
					"  internal/resolve            -> %s\n"+
					"  internal/extractors/sresolver -> %s\n"+
					"A full reindex and an incremental reindex must produce the same graph "+
					"for the same source. Align the tier ORDER in sresolver.lookupLeaf with "+
					"scanLeafMembersPreferring in internal/resolve/refs.go.",
					sh.stub, describe(full), describe(incr))
			}
		})
	}
}
