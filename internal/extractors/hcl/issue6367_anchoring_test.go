package hcl_test

import (
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// issue6367_anchoring_test.go — hcl CONTAINS and DEPENDS_ON must be anchored on
// the record that OWNS them, not on the source file's path.
//
// THE TWO SITES (#6367, allow-list entries hcl:emitFileLevelRelationships:CONTAINS{2}
// and hcl:parseDependsOnTuple:DEPENDS_ON{1} in
// internal/extractors/file_anchored_rels_guard_test.go):
//
//   - relationships.go — emitFileLevelRelationships built CONTAINS with
//     FromID: path at TWO sites, the `locals` per-key branch and the default
//     per-block branch. The owner there IS the file component, so an empty
//     FromID is exactly right.
//   - extractor.go — parseDependsOnTuple built DEPENDS_ON with FromID: fromPath,
//     but its records are appended to the RESOURCE / DATA / MODULE entity
//     (extractor.go:251, :312, :366), so the path moved the edge OFF its owner.
//
// WHAT WAS MEASURED, not inferred. The two fixtures below were extracted,
// id-stamped and pushed through the production resolver pipeline
// (ResolveImports → ReferencesEmbedded). The hcl file component's Name is the
// BASENAME (relationships.go strips everything up to the last '/'), and no hcl
// entity carries the full path as Name or QualifiedName, so the outcome splits
// on whether the path HAPPENS to equal its own basename:
//
//	nested "infra/envs/prod/main.tf"  → 5 of 5 CONTAINS DANGLING
//	                                    2 of 2 DEPENDS_ON DANGLING
//	root   "main.tf"                  → 5 of 5 CONTAINS resolve, onto the file
//	                                    component — right node BY ACCIDENT
//	                                    2 of 2 DEPENDS_ON MISANCHORED onto that
//	                                    same file component, off the resource
//	                                    and module that own them
//
// So hcl is DANGLING in the general case and MISANCHORED in the root-file
// special case — and DEPENDS_ON is wrong in BOTH, because even the resolving
// spelling lands on the file rather than the resource. After the fix all four
// counts are 0 and every edge lands on its owning record.
//
// IMPORTS is deliberately untouched: #120 keeps the file path on IMPORTS edges.

// anchorFixture is one hcl file plus the endpoints its edges must land on.
const anchorSrc = `
locals {
  prefix = "x"
  suffix = "y"
}

resource "aws_s3_bucket" "b" {
  depends_on = [aws_iam_role.r]
}

resource "aws_iam_role" "r" {}

module "vpc" {
  source     = "terraform-aws-modules/vpc/aws"
  depends_on = [aws_s3_bucket.b]
}
`

// measureAnchoring extracts the fixture at path, replays graph assembly's
// "substitute the owner's id only when FromID is empty" rule, and returns, per
// relationship kind, the resolved FROM endpoint of every edge together with the
// owner it should have landed on.
type anchorEdge struct {
	kind string
	// ownerName, fromLabel and wantLabel are for the FAILURE MESSAGE only.
	// A label is Subtype+":"+Name, which is NOT unique: two records sharing
	// both would compare equal and mask a misanchor. The assertion below
	// therefore compares fromID against wantID — the resolved identities —
	// and uses the labels purely to say WHICH nodes those ids are.
	ownerName string
	fromLabel string
	wantLabel string
	// fromID is the endpoint the edge actually lands on after assembly's
	// substitution rule; wantID is the owning record's own id. resolved
	// reports whether fromID names a record that exists at all (a false
	// here is a DANGLING edge, not a misanchored one).
	fromID    string
	wantID    string
	resolved  bool
	rawFromID string
}

func measureAnchoring(t *testing.T, path string) []anchorEdge {
	t.Helper()

	recs, err := extractHCL(anchorSrc, path)
	if err != nil {
		t.Fatalf("extract %s: %v", path, err)
	}
	if len(recs) == 0 {
		t.Fatalf("extract %s: no records", path)
	}
	for i := range recs {
		if recs[i].Name == "" {
			continue
		}
		recs[i].ID = graph.EntityID("issue6367", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}

	resolve.ResolveImports(recs, resolve.BuildImportTable(recs))
	resolve.ReferencesEmbedded(recs, resolve.BuildIndex(recs))

	byID := make(map[string]*types.EntityRecord, len(recs))
	for i := range recs {
		byID[recs[i].ID] = &recs[i]
	}
	label := func(id string) string {
		if e := byID[id]; e != nil {
			return e.Subtype + ":" + e.Name
		}
		return "<UNRESOLVED:" + id + ">"
	}

	var out []anchorEdge
	for i := range recs {
		owner := &recs[i]
		for _, r := range owner.Relationships {
			if r.Kind != "CONTAINS" && r.Kind != "DEPENDS_ON" {
				continue // IMPORTS keeps the file path on purpose (#120).
			}
			// Replay graph assembly: cmd/grafel/index.go and
			// relRecordToGraphRel in internal/extractors/incremental.go
			// substitute the owning record's own id ONLY when FromID is empty.
			from := r.FromID
			if from == "" {
				from = owner.ID
			}
			_, ok := byID[from]
			out = append(out, anchorEdge{
				kind:      r.Kind,
				ownerName: owner.Name,
				fromLabel: label(from),
				wantLabel: label(owner.ID),
				fromID:    from,
				wantID:    owner.ID,
				resolved:  ok,
				rawFromID: r.FromID,
			})
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].kind != out[b].kind {
			return out[a].kind < out[b].kind
		}
		return out[a].ownerName < out[b].ownerName
	})
	return out
}

// TestHCL_ContainsAndDependsOnAnchoredOnOwner is the fix's behavioural test: on
// BOTH a nested and a root path, every CONTAINS and DEPENDS_ON edge must land on
// the record that carries it, which requires FromID to be empty.
func TestHCL_ContainsAndDependsOnAnchoredOnOwner(t *testing.T) {
	// BOTH paths are load-bearing and neither may be dropped "to simplify":
	//
	//   - "infra/envs/prod/main.tf" (nested) is the ONLY path that pins
	//     CONTAINS. For CONTAINS the owner IS the file component, so on a
	//     root path FromID: path resolves to that very component and the
	//     subtest is STRUCTURALLY INCAPABLE of failing — it passes with or
	//     without the fix. Trim the list to the root path alone and the
	//     CONTAINS pin dies silently while the suite stays green.
	//   - "main.tf" (root) is the ONLY path that pins the DEPENDS_ON
	//     MISANCHOR. On the nested path the bad FromID merely dangles;
	//     only at the root does it resolve onto the file component and
	//     land off the resource/module that owns the edge — the failure
	//     mode a dangling-edge check alone would never see.
	//
	// Removing either path loses a distinct guarantee.
	for _, path := range []string{"infra/envs/prod/main.tf", "main.tf"} {
		t.Run(path, func(t *testing.T) {
			edges := measureAnchoring(t, path)

			dangling := map[string]int{}
			misanchored := map[string]int{}
			total := map[string]int{}
			for _, e := range edges {
				total[e.kind]++
				// IDENTITY, not label: two records could share
				// Subtype+Name and compare equal by label.
				if e.fromID == e.wantID {
					continue
				}
				if !e.resolved {
					dangling[e.kind]++
				} else {
					misanchored[e.kind]++
				}
				t.Errorf("%s owned by %q: FROM = %s (id %q), want %s (id %q) "+
					"(FromID=%q must be empty so assembly stamps the owning record)",
					e.kind, e.ownerName, e.fromLabel, e.fromID, e.wantLabel, e.wantID, e.rawFromID)
			}
			for _, kind := range []string{"CONTAINS", "DEPENDS_ON"} {
				if total[kind] == 0 {
					t.Errorf("no %s edges produced by the fixture — the measurement is vacuous", kind)
				}
				if dangling[kind] != 0 || misanchored[kind] != 0 {
					t.Logf("MEASURED %s at %s: %d dangling, %d misanchored, of %d",
						kind, path, dangling[kind], misanchored[kind], total[kind])
				}
			}
		})
	}
}
