package bicep_test

// file_carrier_6852_test.go — #6852, bicep arm. extractModules anchors the
// module IMPORTS edge on the .bicep file's own path (extractor.go, `FromID:
// path`), and nothing bicep emits carries that path as its Name, so the FROM
// end of every module import resolved to nothing and the raw path reached the
// graph. #6815 fixed the same defect in erlang, nim and groovy with the
// CONDITIONAL carrier in internal/extractor/file_carrier.go; this is the same
// fix for bicep.
//
// BICEP IS THE ONE OF THE TWELVE THAT IS NOT EVEN MASKED. `.bicep` is absent
// from sourceFileExtensions (internal/resolve/refs.go), so
// looksLikeSourceFilePath returns false for it and classifyDispositionLang
// falls through to DispositionBugExtractor rather than the DispositionDynamic
// that hides the other eleven. That premise is grounded by
// TestBicepPathIsNotMaskedAsDynamic6852 in internal/resolve rather than taken
// on trust — it needs the unexported helper, so it cannot live here.
//
// GRADED IN BOTH DIRECTIONS. Recall-shaped assertions ("the carrier exists")
// cannot see over-emission, and an UNCONDITIONAL carrier — which is all a
// carrier-exists test licenses — mints one bare orphan node per .bicep file
// across a whole repo. The over-firing controls below
// (TestBicep_NoCarrierWithoutAModuleImport_6852,
// TestBicep_OneCarrierPerFileNotPerModule_6852,
// TestBicep_BicepConfigGetsNoCarrier_6852) are what forbid that.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// bicepCarriers6852 returns every record that IS the file carrier for path —
// the SCOPE.Component/file record extractor.FileEntity mints.
func bicepCarriers6852(recs []types.EntityRecord, path string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" && r.Subtype == "file" && r.Name == path {
			out = append(out, r)
		}
	}
	return out
}

// bicepPathAnchored6852 returns every relationship in recs whose FromID is
// exactly path — the shape whose FROM end has nothing to resolve onto.
func bicepPathAnchored6852(recs []types.EntityRecord, path string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.FromID == path {
				out = append(out, r)
			}
		}
	}
	return out
}

// bicepNamedExactly6852 returns every record whose Name or QualifiedName is
// path. This is the resolution question internal/resolve/refs.go actually
// asks: it has no path→entity index, so a path-valued FromID resolves if and
// only if such a record exists.
func bicepNamedExactly6852(recs []types.EntityRecord, path string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Name == path || r.QualifiedName == path {
			out = append(out, r)
		}
	}
	return out
}

// carrierSrc6852 declares ONE module and no resources, so the only
// path-anchored edge in the extraction is the module IMPORTS this issue is
// about. Kept separate from anchorSrcBicep (issue6367_anchoring_test.go), which
// mixes in three resources and their DEPENDS_ON edges.
const carrierSrc6852 = `
module net './modules/network.bicep' = {
  name: 'netmod'
}
`

// resolveBicep6852 extracts src at path, stamps ids the way graph assembly
// does, runs the production resolver pipeline, and returns the records plus the
// id→record index. This asserts on the EMITTED ARTEFACT after resolution — the
// edge's FROM end — not on a helper's return value or a counter.
func resolveBicep6852(t *testing.T, src, path string) ([]types.EntityRecord, map[string]*types.EntityRecord) {
	t.Helper()
	recs := extractBicep(t, src, path)
	if len(recs) == 0 {
		t.Fatalf("extract %s: no records", path)
	}
	for i := range recs {
		if recs[i].Name == "" {
			continue
		}
		recs[i].ID = graph.EntityID("issue6852", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}
	resolve.ResolveImports(recs, resolve.BuildImportTable(recs))
	resolve.ReferencesEmbedded(recs, resolve.BuildIndex(recs))
	byID := make(map[string]*types.EntityRecord, len(recs))
	for i := range recs {
		byID[recs[i].ID] = &recs[i]
	}
	return recs, byID
}

// TestBicep_ModuleImportsFromEndResolves_6852 is the fix's behavioural test.
// Both a nested and a root path are driven: bicep names no entity after its
// containing file, so the dangle is path-depth-independent and neither depth is
// a special case that could carry the other.
func TestBicep_ModuleImportsFromEndResolves_6852(t *testing.T) {
	for _, path := range []string{"infra/envs/prod/main.bicep", "main.bicep"} {
		t.Run(path, func(t *testing.T) {
			recs, byID := resolveBicep6852(t, carrierSrc6852, path)

			seen := 0
			for i := range recs {
				for _, r := range recs[i].Relationships {
					if r.Kind != "IMPORTS" {
						continue
					}
					seen++
					if _, ok := byID[r.FromID]; !ok {
						t.Errorf("IMPORTS owned by %q: FROM end %q resolves to no record "+
							"(refs.go has no path→entity index; a path-valued FromID "+
							"resolves iff some record carries that exact string as its "+
							"Name — emit a file carrier, internal/extractor/file_carrier.go)",
							recs[i].Name, r.FromID)
					}
				}
			}
			if seen == 0 {
				t.Fatal("fixture produced no IMPORTS edges — this measurement is vacuous")
			}
		})
	}
}

// TestBicep_RegistryModuleImportsFromEndResolves_6852 varies the MODULE TARGET
// FORM: a `br:` registry reference sends extractModules down its
// classifyModuleRegistry branch, which builds a completely different ToID
// (scope:component:external:…) while keeping the same path-anchored FromID.
// HELD CONSTANT: one module, one file, the same nested path. A carrier wired to
// the local-path branch alone would pass the case above and leave every
// registry import dangling.
func TestBicep_RegistryModuleImportsFromEndResolves_6852(t *testing.T) {
	const src = `
module shared 'br:contoso.azurecr.io/bicep/modules/storage:v1' = {
  name: 'sharedmod'
}
`
	const path = "infra/envs/prod/main.bicep"
	recs, byID := resolveBicep6852(t, src, path)

	seen := 0
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			seen++
			if _, ok := byID[r.FromID]; !ok {
				t.Errorf("registry IMPORTS owned by %q: FROM end %q resolves to no record",
					recs[i].Name, r.FromID)
			}
		}
	}
	if seen == 0 {
		t.Fatal("fixture produced no IMPORTS edges — this measurement is vacuous")
	}
}

// TestBicep_NoCarrierWithoutAModuleImport_6852 is the OVER-FIRING control, and
// it is the half of the grade a "the edge now resolves" test cannot supply.
// Axis VARIED: the module declaration (absent). HELD CONSTANT: the same
// resource / param / var / output declarations, so the file still extracts a
// full set of records and still exercises dependencyEdges — only the
// path-anchored edge is gone.
func TestBicep_NoCarrierWithoutAModuleImport_6852(t *testing.T) {
	const src = `
param location string

var prefix = 'demo'

resource stg 'Microsoft.Storage/storageAccounts@2021-04-01' = {
  name: 'stg1'
  location: location
}

resource plan 'Microsoft.Web/serverfarms@2021-01-01' = {
  name: 'plan1'
  dependsOn: [
    stg
  ]
}

output stgId string = stg.id
`
	for _, path := range []string{"infra/envs/prod/main.bicep", "main.bicep"} {
		t.Run(path, func(t *testing.T) {
			recs := extractBicep(t, src, path)
			if len(recs) == 0 {
				t.Fatal("premise: fixture produced no records at all")
			}
			if n := len(bicepPathAnchored6852(recs, path)); n != 0 {
				t.Fatalf("premise: want 0 path-anchored relationships, got %d", n)
			}
			if n := len(bicepCarriers6852(recs, path)); n != 0 {
				t.Errorf("a bicep file with nothing to carry must emit no file carrier, got %d — "+
					"an unconditional carrier mints one bare orphan node per .bicep file "+
					"across a whole repo, which no recall-shaped assertion can see", n)
			}
			// Forbidden-row form: no record of ANY kind may be named after the
			// file here, so a carrier smuggled in under a different Kind or
			// Subtype is caught too.
			for _, r := range bicepNamedExactly6852(recs, path) {
				t.Errorf("no record may be named %q here, got kind=%q subtype=%q name=%q qname=%q",
					path, r.Kind, r.Subtype, r.Name, r.QualifiedName)
			}
		})
	}
}

// TestBicep_OneCarrierPerFileNotPerModule_6852 is the second over-firing
// control. Axis VARIED: the NUMBER of module declarations (three, mixing local
// and registry forms). HELD CONSTANT: one file, one path. The carrier is
// per-FILE, not per-EDGE; a per-edge carrier would put three nodes under one id.
func TestBicep_OneCarrierPerFileNotPerModule_6852(t *testing.T) {
	const src = `
module net './modules/network.bicep' = {
  name: 'netmod'
}

module db './modules/database.bicep' = {
  name: 'dbmod'
}

module shared 'br:contoso.azurecr.io/bicep/modules/storage:v1' = {
  name: 'sharedmod'
}
`
	const path = "infra/envs/prod/main.bicep"
	recs := extractBicep(t, src, path)
	if n := len(bicepPathAnchored6852(recs, path)); n != 3 {
		t.Fatalf("premise: want 3 path-anchored IMPORTS edges, got %d", n)
	}
	if n := len(bicepCarriers6852(recs, path)); n != 1 {
		t.Errorf("3 module imports must still yield exactly 1 file carrier, got %d", n)
	}
	if n := len(bicepNamedExactly6852(recs, path)); n != 1 {
		t.Errorf("exactly 1 record may be named %q, got %d", path, n)
	}
}

// TestBicep_BicepConfigGetsNoCarrier_6852 is the third over-firing control, on
// the OTHER branch of Extract. bicepconfig.json returns early from
// extractBicepConfig with entities and NO relationships at all, so it has
// nothing to anchor and must gain nothing. Its config record already carries
// the file path as its QualifiedName, so an unconditional carrier here would
// also put a second node under that path.
func TestBicep_BicepConfigGetsNoCarrier_6852(t *testing.T) {
	const src = `{
  "moduleAliases": {
    "br": {
      "contoso": { "registry": "contoso.azurecr.io", "modulePath": "bicep/modules" }
    }
  }
}`
	const path = "infra/bicepconfig.json"
	recs := extractBicep(t, src, path)
	if len(recs) == 0 {
		t.Fatal("premise: bicepconfig fixture produced no records")
	}
	if n := len(bicepPathAnchored6852(recs, path)); n != 0 {
		t.Fatalf("premise: want 0 path-anchored relationships, got %d", n)
	}
	if n := len(bicepCarriers6852(recs, path)); n != 0 {
		t.Errorf("bicepconfig.json must gain no file carrier, got %d", n)
	}
	// The config record's QualifiedName is the path; exactly one record may
	// answer to it, not two.
	if n := len(bicepNamedExactly6852(recs, path)); n != 1 {
		t.Errorf("exactly 1 record may answer to %q (the config record's QualifiedName), got %d", path, n)
	}
}

// TestBicep_CarrierShape_6852 pins what the carrier IS: stamped bicep, anchored
// on the file it names, and owning no relationships of its own — the module
// records still carry the IMPORTS edges, so re-homing them onto the carrier
// would double every edge.
func TestBicep_CarrierShape_6852(t *testing.T) {
	const path = "infra/envs/prod/main.bicep"
	recs := extractBicep(t, carrierSrc6852, path)
	cs := bicepCarriers6852(recs, path)
	if len(cs) != 1 {
		t.Fatalf("premise: want 1 carrier, got %d", len(cs))
	}
	if cs[0].Language != "bicep" {
		t.Errorf("carrier Language = %q, want %q", cs[0].Language, "bicep")
	}
	if cs[0].SourceFile != path {
		t.Errorf("carrier SourceFile = %q, want %q", cs[0].SourceFile, path)
	}
	if n := len(cs[0].Relationships); n != 0 {
		t.Errorf("the bicep file carrier must own no relationships, got %d", n)
	}
	if n := len(bicepPathAnchored6852(recs, path)); n != 1 {
		t.Errorf("the module IMPORTS edge must still be emitted exactly once, got %d", n)
	}
	// #577 convention: the file entity is the FIRST record.
	if recs[0].Name != path {
		t.Errorf("carrier must be record 0 (#577 convention), got %q at index 0", recs[0].Name)
	}
}
