package astro

// file_carrier_6852_test.go — #6852, astro arm (arm 7 of twelve).
//
// extractImports (extractor.go) stamps `FromID: filePath` on EVERY IMPORTS edge
// it emits, while NOTHING this package emits is named after the file:
// internal/resolve/refs.go has no path→entity index, so a path-valued FromID
// resolves if and only if some emitted record carries that exact string as its
// Name. Nothing did, at any path depth, so the raw path reached the graph as
// the edge's FROM end. Same defect #6815 fixed in erlang, nim and groovy and
// #6852 fixed in bicep, terraform, html, fsharp, shell, dockerfile and lua;
// same fix, the CONDITIONAL carrier in internal/extractor/file_carrier.go.
//
// ONE ANCHOR, ONE SITE, N EDGES. extractImports is the only producer of a
// path-anchored FromID in this package: every other relationship astro emits
// (RENDERS and IMPLEMENTS, extractTemplateRelationships) leaves FromID EMPTY on
// purpose — that is #6298, fixed by 45b1e2013's follow-up, and re-anchoring
// them on the path is the defect that issue was filed for. So the resolution
// requirement is ONE string and one carrier serves every import in the file,
// however many there are. TestAstro_OneCarrierPerFileNotPerImport_6852 drives
// three distinct imports and requires exactly one carrier.
//
// BOTH DEPTHS DANGLED — measured, not assumed, and this is the axis where each
// arm of #6852 has answered differently. terraform already emitted a component
// named basename(path), so its ROOT case resolved by accident. shell emitted
// one only for a script declaring a function, giving a three-way split. astro
// is html's, fsharp's and lua's answer: it names its whole-file component
// componentNameFromPath(path) — the basename with ".astro" STRIPPED — which is
// not the path at either depth, and the classifier routes only ".astro"
// (classifier.go:497), so the stripped form can never equal the path in
// production. Every other record is named after a prop, a marker or an island.
//
// CLAUSE 3 OF FileCarrierFor IS THEREFORE NOT PRODUCTION-REACHABLE HERE, which
// is a different answer again from hcl (root .tf), fsharp (a dotted `module`
// declaration) and shell (a self-sourcing stub). It is reachable only through
// Extract called directly with an EXTENSIONLESS ROOT path, and
// TestAstro_PathNamedComponentGetsNoSecondCarrier_6852 drives exactly that,
// labelled as the non-production input it is rather than dressed up as a
// production case.
//
// GRADED IN BOTH DIRECTIONS. A recall-shaped assertion ("the carrier exists")
// licenses an UNCONDITIONAL carrier, which would mint one bare orphan node per
// .astro file across a whole repo — invisible to every such assertion. A
// presentational .astro component that imports nothing is the ordinary case, so
// the forbidden-row controls below are the half of the grade that forbids it.

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// extractAstro6852 runs the REGISTERED astro extractor, so the test drives the
// same entry point production does rather than an internal helper.
func extractAstro6852(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("astro")
	if !ok {
		t.Fatal("astro extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "astro",
	})
	if err != nil {
		t.Fatalf("extract %s: %v", path, err)
	}
	return recs
}

// astroNamedExactly6852 returns every record whose Name or QualifiedName is
// path. This is the resolution question refs.go actually asks — it has no
// path→entity index — so it is the forbidden-row form: a carrier smuggled in
// under a different Kind or Subtype is caught too.
func astroNamedExactly6852(recs []types.EntityRecord, path string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Name == path || r.QualifiedName == path {
			out = append(out, r)
		}
	}
	return out
}

// astroPathAnchored6852 returns every relationship whose FromID is exactly path
// — the shape whose FROM end has nothing to resolve onto.
func astroPathAnchored6852(recs []types.EntityRecord, path string) []types.RelationshipRecord {
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

// resolveAstro6852 extracts src at path, stamps ids the way graph assembly
// does, runs the production resolver pipeline, and returns the records plus the
// id→record index. The assertion is on the EMITTED ARTEFACT after resolution —
// the edge's FROM end — not on a helper's return value or a counter the code
// keeps about itself.
func resolveAstro6852(t *testing.T, src, path string) ([]types.EntityRecord, map[string]*types.EntityRecord) {
	t.Helper()
	recs := extractAstro6852(t, src, path)
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

// assertAstroImportsResolve6852 fails for every IMPORTS edge whose FROM end
// names no record, and fails vacuously-empty fixtures.
func assertAstroImportsResolve6852(t *testing.T, recs []types.EntityRecord, byID map[string]*types.EntityRecord) {
	t.Helper()
	seen := 0
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			seen++
			if _, ok := byID[r.FromID]; !ok {
				t.Errorf("IMPORTS owned by %q: FROM end %q resolves to no record "+
					"(refs.go has no path→entity index; a path-valued FromID resolves "+
					"iff some record carries that exact string as its Name — emit a file "+
					"carrier, internal/extractor/file_carrier.go)", recs[i].Name, r.FromID)
			}
		}
	}
	if seen == 0 {
		t.Fatal("fixture produced no IMPORTS edges — this measurement is vacuous")
	}
}

// The import SPELLINGS importRE routes, one fixture each, so a fix wired to one
// spelling cannot pass by riding on another. Each contains exactly ONE import
// and no other, which the premise assertions below enforce rather than assume.
const (
	defaultImportSrc6852 = "---\nimport Nav from './Nav.astro';\n---\n<div><Nav /></div>\n"
	namedImportSrc6852   = "---\nimport { formatDate } from '../lib/date';\n---\n<p>hi</p>\n"
	nsImportSrc6852      = "---\nimport * as utils from '../lib/utils';\n---\n<p>hi</p>\n"
)

// anchoringFixtures6852 is the fixture set the resolution and shape tests
// share. Only fixtures that actually anchor belong here; a fixture that stopped
// anchoring fails the premise assertion in every test that uses it rather than
// quietly weakening one.
func anchoringFixtures6852() map[string]string {
	return map[string]string{
		"default_import":   defaultImportSrc6852,
		"named_import":     namedImportSrc6852,
		"namespace_import": nsImportSrc6852,
	}
}

// astroDepths6852 is the path-depth axis. "Header.astro" is a repository-root
// .astro file; the nested spelling is the ordinary Astro project layout.
func astroDepths6852() []string {
	return []string{"src/components/Header.astro", "Header.astro"}
}

// TestAstro_ImportsFromEndResolves_6852 is the fix's behavioural test, driven
// over the CROSS PRODUCT of the import spelling and the path depth. Axes
// VARIED: the import form (default / named / namespace) and the path depth
// (nested / root). HELD CONSTANT: exactly one import per fixture, the astro
// token, the same resolver pipeline, the same frontmatter delimiters.
//
// BOTH DEPTHS FAIL BEFORE THE FIX, unlike terraform's arm and unlike shell's:
// componentNameFromPath strips ".astro" from the basename, so the whole-file
// component is named "Header" where the path is "Header.astro" — the root case
// never resolved by accident.
func TestAstro_ImportsFromEndResolves_6852(t *testing.T) {
	for form, src := range anchoringFixtures6852() {
		for _, path := range astroDepths6852() {
			t.Run(form+"/"+path, func(t *testing.T) {
				// The premise is read BEFORE resolution: ReferencesEmbedded
				// rewrites a resolved FromID onto the carrier's id, so counting
				// path-anchored edges afterwards would report 0 for a working
				// fix and 1 for a broken one — backwards.
				if n := len(astroPathAnchored6852(extractAstro6852(t, src, path), path)); n != 1 {
					t.Fatalf("premise: want exactly 1 path-anchored IMPORTS edge as EMITTED, got %d", n)
				}
				recs, byID := resolveAstro6852(t, src, path)
				assertAstroImportsResolve6852(t, recs, byID)
			})
		}
	}
}

// TestAstro_NoCarrierWithoutAnImport_6852 is the OVER-FIRING control, and it is
// the half of the grade a "the edge now resolves" test cannot supply. It maps
// to FileCarrierFor's CLAUSE 2 return path (`if !anchored`): the file has
// records, and none of their relationships is anchored on the path.
//
// Axis VARIED: the presence of any import statement (absent), across two
// sub-shapes so the control is not one accident — a file with NO frontmatter at
// all, and a file whose frontmatter is present and full but importless. HELD
// CONSTANT: a template that still yields RENDERS and IMPLEMENTS edges and an
// island marker entity, so the file extracts a full record set and only the
// path-anchored edge is missing.
func TestAstro_NoCarrierWithoutAnImport_6852(t *testing.T) {
	shapes := map[string]string{
		"no_frontmatter": "<div><Nav client:load /><Logo /></div>\n",
		"frontmatter_without_import": "---\nconst { title } = Astro.props;\n" +
			"const data = await fetch('https://api.example.com/x');\n---\n" +
			"<div><Nav client:load /><Logo /></div>\n",
	}
	for shape, src := range shapes {
		for _, path := range astroDepths6852() {
			t.Run(shape+"/"+path, func(t *testing.T) {
				recs := extractAstro6852(t, src, path)
				if len(recs) < 2 {
					t.Fatalf("premise: the importless fixture must still extract a full "+
						"record set, got %d records — an empty extraction would make the "+
						"forbidden row below vacuous", len(recs))
				}
				if n := len(astroPathAnchored6852(recs, path)); n != 0 {
					t.Fatalf("premise: the importless fixture must anchor nothing on %q, got %d edges",
						path, n)
				}
				if got := astroNamedExactly6852(recs, path); len(got) != 0 {
					t.Errorf("a file with no import got %d record(s) named %q — the carrier is "+
						"CONDITIONAL (#6815, #6518): an unconditional one mints one bare orphan "+
						"node per .astro file across a whole repo, which no recall-shaped "+
						"assertion can see (first: kind=%s subtype=%s)",
						len(got), path, got[0].Kind, got[0].Subtype)
				}
			})
		}
	}
}

// TestAstro_EmptyPathGetsNoCarrier_6852 maps to FileCarrierFor's CLAUSE 1
// return path (`path == ""`), which is a DISTINCT rejection from clause 2 and
// not reachable through it: with an empty path extractImports stamps
// `FromID: ""`, and an empty FromID trivially EQUALS an empty path, so clause 2
// is SATISFIED here. Clause 1 is the only thing standing between this input and
// a blank-named carrier. That is exactly why file_carrier.go keeps the two
// clauses separate rather than folding them together.
func TestAstro_EmptyPathGetsNoCarrier_6852(t *testing.T) {
	recs := extractAstro6852(t, defaultImportSrc6852, "")
	if len(recs) == 0 {
		t.Fatal("premise: the empty-path fixture extracted nothing, so the row below is vacuous")
	}
	// At an empty path clause 2 is satisfied TWICE OVER: the IMPORTS edge is
	// stamped FromID: "" because filePath is "", and RENDERS/IMPLEMENTS leave
	// FromID empty by design (#6298). Both trivially equal the empty path, which
	// is the whole reason clause 1 cannot be folded into clause 2.
	if n := len(astroPathAnchored6852(recs, "")); n < 1 {
		t.Fatalf("premise: clause 2 must be SATISFIED for this input (an empty FromID equals "+
			"an empty path), got %d edges anchored on \"\" — without that, this test grades "+
			"clause 2 and not clause 1", n)
	}
	for i := range recs {
		if recs[i].Subtype == "file" {
			t.Errorf("record %d is a file carrier (name=%q) for an EMPTY path — a nameless "+
				"carrier resolves nothing and adds a blank node (FileCarrierFor clause 1)",
				i, recs[i].Name)
		}
	}
}

// TestAstro_PathNamedComponentGetsNoSecondCarrier_6852 maps to FileCarrierFor's
// CLAUSE 3 return path (`records[i].Name == path`).
//
// NOT PRODUCTION-REACHABLE, and named as such rather than presented as a
// production case. componentNameFromPath returns the basename with ".astro"
// stripped, so it equals the whole path only for an EXTENSIONLESS ROOT path —
// and classifier.go:497 routes only ".astro" to this extractor, so no file the
// classifier hands astro can produce it. The input below reaches Extract
// directly. It is driven because the clause is what stands between such an
// input and TWO nodes under one id (graph.EntityID hashes Kind, Name and
// SourceFile — not Subtype, the #6369/#6480 hazard), and because the nested
// contrast is what stops this passing on a carrier that is never emitted at all.
func TestAstro_PathNamedComponentGetsNoSecondCarrier_6852(t *testing.T) {
	t.Run("root_extensionless_path_named_component", func(t *testing.T) {
		const path = "Header" // componentNameFromPath("Header") == "Header"
		recs := extractAstro6852(t, defaultImportSrc6852, path)
		named := astroNamedExactly6852(recs, path)
		if len(named) != 1 {
			t.Fatalf("want exactly 1 record named %q, got %d — a second one puts two nodes "+
				"under one entity id", path, len(named))
		}
		if named[0].Subtype == "file" {
			t.Errorf("the single record named %q is the CARRIER, not the component — the "+
				"pre-existing path-named record was replaced rather than deferred to", path)
		}
	})
	t.Run("nested_contrast_does_get_a_carrier", func(t *testing.T) {
		const path = "src/components/Header.astro"
		recs := extractAstro6852(t, defaultImportSrc6852, path)
		named := astroNamedExactly6852(recs, path)
		if len(named) != 1 || named[0].Subtype != "file" {
			t.Fatalf("contrast: a nested path must get exactly one carrier, got %d record(s) "+
				"named %q — without this the root subtest passes on a carrier that is never "+
				"emitted at all", len(named), path)
		}
	})
}

// TestAstro_OneCarrierPerFileNotPerImport_6852 drives the multiplicity axis:
// extractImports emits N edges from ONE site, and the carrier is per FILE.
func TestAstro_OneCarrierPerFileNotPerImport_6852(t *testing.T) {
	const src = "---\n" +
		"import Nav from './Nav.astro';\n" +
		"import { formatDate } from '../lib/date';\n" +
		"import * as utils from '../lib/utils';\n" +
		"---\n<div><Nav /></div>\n"
	for _, path := range astroDepths6852() {
		t.Run(path, func(t *testing.T) {
			recs := extractAstro6852(t, src, path)
			if n := len(astroPathAnchored6852(recs, path)); n != 3 {
				t.Fatalf("premise: want 3 path-anchored IMPORTS edges, got %d", n)
			}
			if n := len(astroNamedExactly6852(recs, path)); n != 1 {
				t.Errorf("want exactly 1 carrier for 3 imports, got %d — the carrier is per "+
					"FILE, and duplicates land under one entity id", n)
			}
		})
	}
}

// TestAstro_CarrierShape_6852 pins what the carrier IS: stamped with the
// extractor's language token, anchored on the file it names, first in the
// record slice (#577), and owning NO relationships of its own — the whole-file
// component still carries the IMPORTS edges, so re-homing them onto the carrier
// would double every edge.
//
// The Language assertion is load-bearing and not decorative, and it is why this
// test is named in nonTaggingCallers
// (internal/extractor/carrier_caller_set_6861_test.go): internal/extractors/astro
// runs NO extractor.TagEntitiesLanguage — every record it emits sets
// `Language: "astro"` as a literal at its construction site — so the carrier
// keeps whatever token PrependFileCarrier is handed. A wrong OR EMPTY token
// would survive every other test in this file and in this package.
func TestAstro_CarrierShape_6852(t *testing.T) {
	const path = "src/components/Header.astro"
	for form, src := range anchoringFixtures6852() {
		t.Run(form, func(t *testing.T) {
			recs := extractAstro6852(t, src, path)
			cs := astroNamedExactly6852(recs, path)
			if len(cs) != 1 {
				t.Fatalf("premise: want 1 carrier, got %d", len(cs))
			}
			c := cs[0]
			if c.Kind != "SCOPE.Component" || c.Subtype != "file" {
				t.Errorf("carrier kind/subtype = %q/%q, want %q/%q",
					c.Kind, c.Subtype, "SCOPE.Component", "file")
			}
			if c.Language != "astro" {
				t.Errorf("carrier Language = %q, want %q — the token comes from the "+
					"PrependFileCarrier argument, and internal/extractors/astro runs no "+
					"TagEntitiesLanguage to repair an empty or wrong one", c.Language, "astro")
			}
			if c.SourceFile != path {
				t.Errorf("carrier SourceFile = %q, want %q", c.SourceFile, path)
			}
			if n := len(c.Relationships); n != 0 {
				t.Errorf("the astro file carrier must own no relationships, got %d", n)
			}
			// TagEntitiesLanguage stamps Properties["language"] only on the fill
			// path, so its ABSENCE is the premise for reading Language as
			// evidence that the argument was passed through (#6852, dockerfile).
			if _, ok := c.Properties["language"]; ok {
				t.Errorf("carrier carries Properties[\"language\"] — something filled the token " +
					"instead of it being passed in, so the Language assertion above no longer " +
					"grades the PrependFileCarrier argument")
			}
			if n := len(astroPathAnchored6852(recs, path)); n != 1 {
				t.Errorf("the IMPORTS edge must still be emitted exactly once, got %d", n)
			}
			// #577 convention: the file entity is the FIRST record. python's
			// re_exports.go and prune_import_placeholders.go both rely on index 0.
			if recs[0].Name != path {
				t.Errorf("carrier must be record 0 (#577 convention), got %q at index 0", recs[0].Name)
			}
		})
	}
}

// TestAstro_CarrierPlacementDoesNotRehomeComponentEdges_6852 is the placement
// grade, and astro is the second caller in #6852 (after lua) whose extractor
// keeps state keyed by INDEX into the entity slice.
//
// Extract writes `entities[0].Relationships = append(...)` at THREE sites — the
// IMPORTS edges from extractImports, then the RENDERS and IMPLEMENTS edges from
// extractTemplateRelationships — each meaning "the whole-file component", which
// is index 0 only because it is appended first. PrependFileCarrier inserts at
// position 0, so a carrier prepended anywhere ABOVE those sites silently
// re-homes every edge below it onto a record that must own none:
//
//   - above the frontmatter section: the carrier is not emitted at all (no edge
//     is anchored yet, so clause 2 rejects) and every import dangles again;
//   - between the frontmatter and template sections: the carrier IS emitted,
//     and RENDERS/IMPLEMENTS land on it instead of on the component — which is
//     precisely the #6298 defect this package already fixed once, reintroduced
//     from the other end.
//
// Each conjunct is scored separately in the PR body rather than left as prose.
// The fixture below anchors an import AND renders two children AND hydrates one
// island, so all three consumers of index 0 are live at once; the premise row
// confirms a carrier is really emitted, so the assertions are about a SHIFTED
// slice and not about a change that never happened.
func TestAstro_CarrierPlacementDoesNotRehomeComponentEdges_6852(t *testing.T) {
	const src = "---\nimport Nav from './Nav.astro';\n---\n" +
		"<div><Nav client:load /><Logo /></div>\n"
	for _, path := range astroDepths6852() {
		t.Run(path, func(t *testing.T) {
			recs := extractAstro6852(t, src, path)
			if n := len(astroNamedExactly6852(recs, path)); n != 1 {
				t.Fatalf("premise: want exactly 1 carrier so these assertions are about a "+
					"shifted slice, got %d", n)
			}
			var carrier, component *types.EntityRecord
			for i := range recs {
				switch {
				case recs[i].Name == path:
					carrier = &recs[i]
				case recs[i].Kind == "SCOPE.Component" && recs[i].Name == "Header":
					component = &recs[i]
				}
			}
			if component == nil || carrier == nil {
				t.Fatal("premise: extraction did not yield both a Header component and a " +
					"path-named carrier record")
			}
			if n := len(carrier.Relationships); n != 0 {
				t.Errorf("the carrier owns %d relationship(s) — Extract's three "+
					"`entities[0].Relationships = append(...)` sites mean the WHOLE-FILE "+
					"COMPONENT, so a carrier prepended above them re-homes the edges "+
					"(#6298 is the same edges landing on the wrong node)", n)
			}
			kinds := map[string]int{}
			for _, r := range component.Relationships {
				kinds[r.Kind]++
			}
			for _, want := range []string{"IMPORTS", "RENDERS", "IMPLEMENTS"} {
				if kinds[want] == 0 {
					t.Errorf("the Header component owns no %s edge (has %v) — index 0 moved "+
						"under one of Extract's three consumers of it", want, kinds)
				}
			}
			if kinds["RENDERS"] != 2 {
				t.Errorf("Header RENDERS = %d, want 2 (Nav and Logo)", kinds["RENDERS"])
			}
		})
	}
}
