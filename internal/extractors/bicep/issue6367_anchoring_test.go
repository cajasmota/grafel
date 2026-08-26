package bicep_test

import (
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// issue6367_anchoring_test.go — bicep DEPENDS_ON must be anchored on the record
// that OWNS it, not on the source file's path.
//
// THE SITE (#6367, allow-list entry bicep:dependencyEdges:DEPENDS_ON{1} in
// internal/extractors/file_anchored_rels_guard_test.go): dependencyEdges built
// every DEPENDS_ON with FromID: path, but its records are attached to the
// RESOURCE record (extractor.go, extractResources) and to the MODULE record
// (extractModules) — so the path moved the edge OFF the record that owns it.
//
// WHAT WAS MEASURED, not inferred. The fixture below is extracted, id-stamped
// and pushed through the production resolver pipeline (ResolveImports →
// ReferencesEmbedded) at two paths. The bicep package emits NO file entity and
// no bicep entity carries the containing file's path as Name or QualifiedName
// (resources are named by symbolic name, modules by their `source` path), so
// unlike hcl there is no root-path accident to resolve onto:
//
//	nested "infra/envs/prod/main.bicep" → 3 of 3 DEPENDS_ON DANGLING
//	root   "main.bicep"                 → 3 of 3 DEPENDS_ON DANGLING
//
// After the fix all counts are 0 and every DEPENDS_ON lands on the resource or
// module that carries it.
//
// IMPORTS is deliberately untouched: #120 keeps the file path on IMPORTS edges,
// and TestBicep_ImportsKeepsFilePathAnchor below pins that so a fix that blanks
// FromID too broadly is caught rather than silently accepted.

// anchorSrcBicep exercises BOTH dependencyEdges call sites — the one on the
// resource record and the one on the module record — and both ways a dependency
// is expressed in bicep (an explicit dependsOn array and a dotted property
// access), so a fix that reaches only one call site or only one syntax fails.
const anchorSrcBicep = `
resource stg 'Microsoft.Storage/storageAccounts@2021-04-01' = {
  name: 'stg1'
}

resource plan 'Microsoft.Web/serverfarms@2021-01-01' = {
  name: 'plan1'
  dependsOn: [
    stg
  ]
}

resource site 'Microsoft.Web/sites@2021-01-01' = {
  name: 'site1'
  properties: {
    serverFarmId: plan.id
  }
}

module net './modules/network.bicep' = {
  name: 'netmod'
  params: {
    storageId: stg.id
  }
}
`

// anchorEdge mirrors the hcl harness (TestHCL_ContainsAndDependsOnAnchoredOnOwner).
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

// measureBicepAnchoring extracts the fixture at path, replays graph assembly's
// "substitute the owner's id only when FromID is empty" rule, and returns the
// resolved FROM endpoint of every DEPENDS_ON edge with the owner it should have
// landed on.
func measureBicepAnchoring(t *testing.T, path string) []anchorEdge {
	t.Helper()

	recs := extractBicep(t, anchorSrcBicep, path)
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
			if r.Kind != "DEPENDS_ON" {
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
		if out[a].ownerName != out[b].ownerName {
			return out[a].ownerName < out[b].ownerName
		}
		return out[a].fromID < out[b].fromID
	})
	return out
}

// TestBicep_DependsOnAnchoredOnOwner is the fix's behavioural test: at BOTH a
// nested and a root path, every DEPENDS_ON edge must land on the resource or
// module that carries it, which requires FromID to be empty.
func TestBicep_DependsOnAnchoredOnOwner(t *testing.T) {
	// Both paths are kept even though bicep — unlike hcl — has no root-path
	// accident: no bicep entity is ever named after the containing file, so
	// the bad FromID dangles at BOTH depths. The root path is therefore not
	// redundant coverage of a second failure MODE; it is the pin that the
	// outcome does NOT depend on path depth, which is exactly the assumption
	// that made the hcl arm's root case behave differently from its nested
	// case. If bicep ever grows a file entity, the root row starts carrying a
	// misanchor and this test already observes it.
	for _, path := range []string{"infra/envs/prod/main.bicep", "main.bicep"} {
		t.Run(path, func(t *testing.T) {
			edges := measureBicepAnchoring(t, path)

			// The fixture must produce edges from BOTH call sites, or the
			// measurement is vacuous for whichever site it missed.
			owners := map[string]bool{}
			dangling, misanchored := 0, 0
			for _, e := range edges {
				owners[e.ownerName] = true
				// IDENTITY, not label: two records could share
				// Subtype+Name and compare equal by label.
				if e.fromID == e.wantID {
					continue
				}
				if !e.resolved {
					dangling++
				} else {
					misanchored++
				}
				t.Errorf("%s owned by %q: FROM = %s (id %q), want %s (id %q) "+
					"(FromID=%q must be empty so assembly stamps the owning record)",
					e.kind, e.ownerName, e.fromLabel, e.fromID, e.wantLabel, e.wantID, e.rawFromID)
			}
			if len(edges) == 0 {
				t.Fatal("no DEPENDS_ON edges produced by the fixture — the measurement is vacuous")
			}
			// "plan" and "site" are RESOURCE records (extractResources call
			// site); "net" is a MODULE record (extractModules call site).
			for _, want := range []string{"plan", "site", "net"} {
				if !owners[want] {
					t.Errorf("fixture produced no DEPENDS_ON owned by %q — "+
						"the measurement no longer covers that call site", want)
				}
			}
			if dangling != 0 || misanchored != 0 {
				t.Logf("MEASURED DEPENDS_ON at %s: %d dangling, %d misanchored, of %d",
					path, dangling, misanchored, len(edges))
			}
		})
	}
}

// TestBicep_ImportsKeepsFilePathAnchor pins the OTHER direction: the module
// IMPORTS edge deliberately keeps the file path as FromID (#120). Without this
// row, a fix that blanked FromID across the whole extractor — or a later
// refactor that did — would leave TestBicep_DependsOnAnchoredOnOwner green.
func TestBicep_ImportsKeepsFilePathAnchor(t *testing.T) {
	for _, path := range []string{"infra/envs/prod/main.bicep", "main.bicep"} {
		t.Run(path, func(t *testing.T) {
			recs := extractBicep(t, anchorSrcBicep, path)
			seen := 0
			for i := range recs {
				for _, r := range recs[i].Relationships {
					if r.Kind != "IMPORTS" {
						continue
					}
					seen++
					if r.FromID != path {
						t.Errorf("IMPORTS owned by %q: FromID = %q, want the file path %q (#120)",
							recs[i].Name, r.FromID, path)
					}
				}
			}
			if seen == 0 {
				t.Fatal("fixture produced no IMPORTS edges — this pin is vacuous")
			}
		})
	}
}
