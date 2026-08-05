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
	// lang and signatures carry the extra entity fields the #6177 eligibility
	// rule reads. lang defaults to "go" and signatures to none, which is what
	// every pre-#6177 shape wants; signatures is indexed like members.
	lang       string
	signatures []string
	// want, when set, is the member Name the stub MUST resolve to, or
	// wantDangle when it must resolve to nothing. Agreement alone cannot catch
	// both resolvers being wrong the SAME way, which is the specific risk when
	// a rule is expressed differently on each side — #6177 skips per candidate
	// in internal/resolve and withholds from the index in sresolver. Shapes
	// that leave it empty assert agreement only, as they did before.
	want string
}

const wantDangle = "«nothing»"

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
	{
		// #6177 — a non-public Solidity state variable is INELIGIBLE, not
		// merely unpreferred, so both resolvers must refuse. This shape is
		// what stops the eligibility rule from being fixed on one side only:
		// a full rebuild would dangle the impossible edge while an incremental
		// build kept binding it.
		name:       "solidity_internal_field_is_ineligible_6177",
		callerFile: "contracts/a.sol",
		lang:       "solidity",
		members: [][3]string{
			{"Config.threshold", "contracts/b.sol", "SCOPE.Schema"},
		},
		signatures: []string{"uint256 internal threshold"},
		stub:       "threshold",
		want:       wantDangle,
	},
	{
		// The regression half of the same rule: `public` DOES synthesise a
		// getter, so the call is real and both resolvers must still bind it.
		name:       "solidity_public_field_still_binds_6177",
		callerFile: "contracts/a.sol",
		lang:       "solidity",
		members: [][3]string{
			{"Vault.totalAssets", "contracts/b.sol", "SCOPE.Schema"},
		},
		signatures: []string{"uint256 public totalAssets"},
		stub:       "totalAssets",
		want:       "Vault.totalAssets",
	},
	{
		// #6177 — the first of the two shapes where "skip per candidate" and
		// "withhold from the index" can come apart. An ineligible field sits
		// beside an ELIGIBLE sibling of the same leaf name in one package: the
		// internal field must neither bind nor make the public one look
		// ambiguous, on BOTH sides. internal/resolve pins this alone in
		// TestSolidityIneligibleFieldIsSkippedNotFatal_6177; without this shape
		// sresolver's put() sentinel could poison leafByPkg and dangle the call
		// while a full rebuild kept binding it.
		name:       "solidity_ineligible_field_beside_eligible_sibling_6177",
		callerFile: "contracts/a.sol",
		lang:       "solidity",
		members: [][3]string{
			{"Hidden.cap", "contracts/b.sol", "SCOPE.Schema"},
			{"Exposed.cap", "contracts/c.sol", "SCOPE.Schema"},
		},
		signatures: []string{"uint256 internal cap", "uint256 public cap"},
		stub:       "cap",
		want:       "Exposed.cap",
	},
	{
		// #6177 — the second one, and the one that DID come apart. Two sibling
		// files declare a contract of the SAME name, which is legal Solidity and
		// ordinary in mock and test trees, so both `Hidden.cap` declarations
		// collide on one key in internal/resolve's byPackageMember and are
		// stored as the blank ambiguity sentinel. The sentinel has erased the
		// IDs, so the per-candidate skip cannot fire for it and the scan
		// abandoned EVERY remaining scope, dangling a call `Exposed.cap`
		// answers. sresolver withholds an ineligible entity before the write,
		// so it never builds that sentinel and bound `Exposed.cap`.
		//
		// Measured: at the parent commit both resolvers dangled and AGREED; with
		// the per-candidate skip alone they disagreed and the incremental side
		// was the right one. internal/resolve now resolves a sentinel key over
		// the eligible subset (Index.eligibleMember), pinned there by
		// TestSolidityIneligibleSentinel_ScanContinuesToEligibleSibling_6177.
		name:       "solidity_colliding_ineligible_scopes_6177",
		callerFile: "contracts/a.sol",
		lang:       "solidity",
		members: [][3]string{
			{"Hidden.cap", "contracts/b.sol", "SCOPE.Schema"},
			{"Hidden.cap", "contracts/d.sol", "SCOPE.Schema"},
			{"Exposed.cap", "contracts/c.sol", "SCOPE.Schema"},
		},
		signatures: []string{
			"uint256 internal cap", "uint256 internal cap", "uint256 public cap",
		},
		stub: "cap",
		want: "Exposed.cap",
	},
}

// TestLeafTier_FullAndIncrementalResolversAgree_6141 drives both resolvers
// over each shape and fails when they bind the same stub differently.
func TestLeafTier_FullAndIncrementalResolversAgree_6141(t *testing.T) {
	for _, sh := range agreementShapes {
		t.Run(sh.name, func(t *testing.T) {
			const callerID = "1111111111111111"

			lang := sh.lang
			if lang == "" {
				lang = "go"
			}

			// Describe the world once; feed both resolvers from it so a
			// divergence can only come from the resolvers themselves.
			type member struct{ id, name, file, kind, sig string }
			members := make([]member, 0, len(sh.members))
			for i, m := range sh.members {
				var sig string
				if i < len(sh.signatures) {
					sig = sh.signatures[i]
				}
				members = append(members, member{
					id:   fmt.Sprintf("%016x", i+2),
					name: m[0], file: m[1], kind: m[2], sig: sig,
				})
			}

			// ---- full-rebuild resolver (internal/resolve) ----
			recs := []types.EntityRecord{{
				ID: callerID, Kind: "SCOPE.Operation", Name: "Local07",
				SourceFile: sh.callerFile, Language: lang,
				Relationships: []types.RelationshipRecord{{
					FromID: callerID, ToID: sh.stub, Kind: "CALLS",
				}},
			}}
			for _, m := range members {
				recs = append(recs, types.EntityRecord{
					ID: m.id, Kind: m.kind, Name: m.name, SourceFile: m.file,
					Language: lang, Signature: m.sig,
				})
			}
			idx := resolve.BuildIndex(recs)
			resolve.ReferencesEmbedded(recs, idx)
			full := recs[0].Relationships[0].ToID

			// ---- incremental resolver (sresolver) ----
			caller := lfEnt(callerID, "Local07", sh.callerFile, "SCOPE.Operation")
			caller.Language = lang
			fresh := []graph.Entity{caller}
			var existing []graph.Entity
			for _, m := range members {
				e := lfEnt(m.id, m.name, m.file, m.kind)
				e.Language = lang
				e.Signature = m.sig
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

			// Both resolvers agree by here, so one of them names the answer.
			if sh.want == "" {
				return
			}
			bound := wantDangle
			for _, m := range members {
				if m.id == full {
					bound = m.name
				}
			}
			if bound != sh.want {
				t.Fatalf("both resolvers agree, and both are WRONG: bare CALLS %q resolved "+
					"to %s, want %s. Agreement is not correctness — an eligibility rule "+
					"expressed as a per-candidate skip on one side and an index omission on "+
					"the other can fail identically on both.", sh.stub, bound, sh.want)
			}
		})
	}
}
