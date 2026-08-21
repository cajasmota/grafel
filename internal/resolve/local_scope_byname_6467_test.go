// Package resolve_test — #6467: the #6427/#6369 byName precedence ranks a
// record on ONE property (is it a per-import placeholder?) and treats every
// non-placeholder as a declaration of the name, eligible for the
// REPOSITORY-WIDE slot. Neither arm looks at where the winner was declared.
//
// So a binding that only exists inside a function body — non-addressable
// outside it, hidden from grafel_find by internal/mcp/denoise.go for exactly
// that reason — takes the slot for the whole repository, and a same-named
// package import binds to it instead of staying unclaimed for the
// external-library binder.
//
// THE REPORTER'S SHAPE (@arthurgeron, #6467), reproduced verbatim below:
//
//	// src/consumer.tsx
//	import { useToolkit } from "toolkit";
//	export function Panel() { const value = useToolkit(); ... }
//
//	// src/rows.tsx
//	export function Rows({ ids }: { ids: number[] }) {
//	  const toolkit: Array<{ id: number }> = [];
//	  ids.forEach((id) => toolkit.push({ id }));
//	  ...
//	}
//
// On 2872b201a (#6427's parent) the export carries
// `src/consumer.tsx -IMPORTS-> ext:toolkit`; on da80569f2 (main) that one line
// became `src/consumer.tsx -IMPORTS-> 8dc7162e164ff6ea`, the `const toolkit`
// on line 2 of src/rows.tsx — a SCOPE.Component, Subtype "const", carrying
// Properties["local_scope"]="true".
//
// This file is `package resolve_test` on purpose: pinning the reported symptom
// means asserting the edge LANDS ON ext:toolkit, which is internal/external's
// job, and only an external test package may import it from here.
package resolve_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/external"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

const (
	consumerFile6467 = "src/consumer.tsx"
	rowsFile6467     = "src/rows.tsx"

	// Stand-ins for the production sha256 ids. Distinct by construction so
	// the fixture cannot pass by ID collision.
	panelID6467            = "aaaa000000000001"
	consumerPlaceholder467 = "aaaa000000000002" // consumer.tsx's `toolkit` import placeholder
	rowsFnID6467           = "bbbb000000000001"
	rowsLocalConstID6467   = "8dc7162e164ff6ea" // the reporter's own id for `const toolkit`

	// The Format-A, file-scoped REFERENCES stub the JS/TS extractor emits.
	refStub6467 = "scope:component:ref:typescript:" + rowsFile6467 + ":toolkit"
)

// reporterRecords6467 mints the entity records the JS/TS extractor plus
// cross/imports produce for the reporter's two files.
func reporterRecords6467() []types.EntityRecord {
	return []types.EntityRecord{
		// ── src/consumer.tsx ────────────────────────────────────────────
		{
			ID: panelID6467, Kind: "SCOPE.Operation", Name: "Panel",
			Subtype: "function", SourceFile: consumerFile6467, Language: "typescript",
		},
		{
			// The per-import placeholder minted once for
			// `import { useToolkit } from "toolkit"`. The bare-name IMPORTS
			// edge rides on it, exactly as cross/imports emits it.
			ID: consumerPlaceholder467, Kind: "SCOPE.Component", Name: "toolkit",
			Subtype: "import", SourceFile: consumerFile6467, Language: "typescript",
			Properties: map[string]string{
				"external_dependency": "true",
				"provenance":          "INFERRED_FROM_IMPORT_STATEMENT",
			},
			Relationships: []types.RelationshipRecord{
				{FromID: consumerFile6467, ToID: "toolkit", Kind: "IMPORTS"},
			},
		},

		// ── src/rows.tsx ────────────────────────────────────────────────
		{
			ID: rowsFnID6467, Kind: "SCOPE.Operation", Name: "Rows",
			Subtype: "function", SourceFile: rowsFile6467, Language: "typescript",
			// #1748 is the reason the local const is in the graph at all:
			// this same-file REFERENCES edge must keep binding to it. The
			// JS/TS extractor emits it as the file-scoped Format-A stub
			// buildReferenceTargetID mints (references.go:611), NOT as a bare
			// name — which is why the reporter measures this edge as correct
			// on BOTH binaries while the module's IMPORTS edge moved.
			Relationships: []types.RelationshipRecord{
				{FromID: rowsFnID6467, ToID: refStub6467, Kind: "REFERENCES"},
			},
		},
		{
			// `const toolkit: Array<{ id: number }> = []` INSIDE Rows's body.
			// tagLocalScope (javascript/extractor.go:776) stamps it.
			ID: rowsLocalConstID6467, Kind: "SCOPE.Component", Name: "toolkit",
			Subtype: "const", SourceFile: rowsFile6467, Language: "typescript",
			StartLine: 2, EndLine: 2,
			Properties: map[string]string{"local_scope": "true"},
		},
	}
}

// asDocument mirrors the indexer's hand-off from the resolver to
// internal/external: the records, with their (now rewritten) relationships,
// become a graph.Document that Synthesize scans for still-unresolved stubs.
func asDocument6467(recs []types.EntityRecord) *graph.Document {
	doc := &graph.Document{}
	seenFile := map[string]bool{}
	for i := range recs {
		e := graph.Entity{
			ID: recs[i].ID, Name: recs[i].Name, Kind: recs[i].Kind,
			Subtype: recs[i].Subtype, SourceFile: recs[i].SourceFile,
			Language: recs[i].Language,
		}
		if len(recs[i].Properties) > 0 {
			e = e.WithProperties(recs[i].Properties)
		}
		doc.Entities = append(doc.Entities, e)
		if f := recs[i].SourceFile; f != "" && !seenFile[f] {
			seenFile[f] = true
			doc.Entities = append(doc.Entities, graph.Entity{
				ID: f, Name: f, Kind: "SCOPE.Component", Subtype: "file",
				SourceFile: f, Language: recs[i].Language,
			})
		}
	}
	n := 0
	for i := range recs {
		for _, r := range recs[i].Relationships {
			n++
			rel := graph.Relationship{
				ID:     "r" + string(rune('a'+n)),
				FromID: r.FromID, ToID: r.ToID, Kind: r.Kind,
			}.WithProperties(map[string]string{"language": "typescript"})
			doc.Relationships = append(doc.Relationships, rel)
		}
	}
	return doc
}

func findRel6467(doc *graph.Document, kind, from string) *graph.Relationship {
	for i := range doc.Relationships {
		if doc.Relationships[i].Kind == kind && doc.Relationships[i].FromID == from {
			return &doc.Relationships[i]
		}
	}
	return nil
}

// TestFunctionLocalConstDoesNotCaptureImportedPackageName_6467 is the gate for
// the reported regression: with the reporter's two files and nothing else, the
// module's own IMPORTS edge must reach ext:toolkit.
//
// Run in BOTH extraction orders. #6369's own comment says extraction order is
// not stable, and the two arms of the precedence are separate code paths —
// "the placeholder arrived first" and "the local arrived first" are different
// branches, and a fix that only guards one of them is half a fix that the
// corpus will find.
func TestFunctionLocalConstDoesNotCaptureImportedPackageName_6467(t *testing.T) {
	t.Run("placeholder indexed first", func(t *testing.T) {
		assertReporterShape6467(t, reporterRecords6467())
	})
	t.Run("local const indexed first", func(t *testing.T) {
		recs := reporterRecords6467()
		reversed := make([]types.EntityRecord, 0, len(recs))
		for i := len(recs) - 1; i >= 0; i-- {
			reversed = append(reversed, recs[i])
		}
		assertReporterShape6467(t, reversed)
	})
}

func assertReporterShape6467(t *testing.T, recs []types.EntityRecord) {
	t.Helper()

	idx := resolve.BuildIndex(recs)
	resolve.ReferencesEmbedded(recs, idx)

	doc := asDocument6467(recs)
	external.Synthesize(doc)

	imp := findRel6467(doc, "IMPORTS", consumerFile6467)
	if imp == nil {
		t.Fatalf("fixture is vacuous: no IMPORTS edge from %s", consumerFile6467)
	}
	const wantImport = "ext:toolkit"
	if imp.ToID != wantImport {
		t.Errorf("%s -IMPORTS-> %q, want %q: a `const` declared inside a function body "+
			"captured the repository-wide slot for an imported package name, so the import "+
			"never fell through to the external-library binder (#6467)",
			consumerFile6467, imp.ToID, wantImport)
	}

	// #1748 — the local const is in the graph so SAME-FILE edges can bind to
	// it. Withholding it from the repo-wide slot must not cost that.
	ref := findRel6467(doc, "REFERENCES", rowsFnID6467)
	if ref == nil {
		t.Fatalf("fixture is vacuous: no REFERENCES edge from %s", rowsFnID6467)
	}
	if ref.ToID != rowsLocalConstID6467 {
		t.Errorf("Rows -REFERENCES-> %q, want %q: the local binding must stay bindable "+
			"from its own file (#1748)", ref.ToID, rowsLocalConstID6467)
	}
}

// TestManifestDeclarationStillOutranksImportPlaceholder_6467 pins #6427 in the
// direction that matters: this fix is a NARROWING of which records compete for
// the repository-wide slot, not a reversal.
//
// A dependency declared in a package manifest (pyproject.toml → `requests`,
// carrying version and provenance) is a real, addressable declaration. #6427
// exists so it outranks the per-import placeholders that every importing file
// mints, and so the IMPORTS edges bind to it rather than to the property-less
// ext:requests. If this test fails, the #6467 fix reverted #6427.
func TestManifestDeclarationStillOutranksImportPlaceholder_6467(t *testing.T) {
	const (
		manifestID  = "cccc000000000001"
		ph1         = "cccc000000000002"
		ph2         = "cccc000000000003"
		fileA       = "app/a.py"
		fileB       = "app/b.py"
		manifest    = "pyproject.toml"
		wantTargetN = 2
	)
	importPlaceholder := func(id, file string) types.EntityRecord {
		return types.EntityRecord{
			ID: id, Kind: "SCOPE.Component", Name: "requests",
			Subtype: "import", SourceFile: file, Language: "python",
			Properties: map[string]string{
				"external_dependency": "true",
				"provenance":          "INFERRED_FROM_IMPORT_STATEMENT",
			},
			Relationships: []types.RelationshipRecord{
				{FromID: file, ToID: "requests", Kind: "IMPORTS"},
			},
		}
	}
	recs := []types.EntityRecord{
		importPlaceholder(ph1, fileA),
		{
			// The manifest-declared dependency — module scope, no local_scope,
			// addressable repository-wide. This is #6427's beneficiary.
			ID: manifestID, Kind: "SCOPE.Component", Name: "requests",
			Subtype: "package", SourceFile: manifest, Language: "python",
			Properties: map[string]string{
				"version":    ">=2.0.0",
				"provenance": "INFERRED_FROM_PACKAGE_MANIFEST",
			},
		},
		importPlaceholder(ph2, fileB),
	}

	idx := resolve.BuildIndex(recs)
	resolve.ReferencesEmbedded(recs, idx)

	seen := 0
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			seen++
			if r.ToID != manifestID {
				t.Errorf("%s -IMPORTS-> %q, want the manifest declaration %q: a real "+
					"module-scope declaration must still outrank the per-import placeholders "+
					"(#6427); narrowing the slot for function-locals must not un-narrow this",
					recs[i].SourceFile, r.ToID, manifestID)
			}
		}
	}
	if seen != wantTargetN {
		t.Fatalf("fixture is vacuous: found %d IMPORTS edge(s), want %d", seen, wantTargetN)
	}
}

// TestFormalParameterDoesNotCaptureImportedPackageName_6467 establishes the
// CLASS, not just the reported instance.
//
// #6427's failure was treating one PROPERTY as sufficient; repeating that with
// one SHAPE would be the same mistake. `local_scope` is stamped only by the
// JavaScript/TypeScript extractor, and even there it does not cover every
// non-addressable binding: a plain `function Rows(toolkit) {…}` parameter is
// emitted as SCOPE.Operation / Subtype "component_prop" with NO local_scope
// stamp (measured against the extractor), and it took the repository-wide slot
// for exactly the same reason the const did.
//
// A formal parameter is scoped to its callable, so it can never legitimately
// own a repository-wide name — which is why the second signal exists at all
// rather than keying on `local_scope` alone. It matches "component_prop" only
// when the record also carries Properties["framework"]=="react"; see the round-3
// block at the end of this file for the measurement that forced that gate.
func TestFormalParameterDoesNotCaptureImportedPackageName_6467(t *testing.T) {
	recs := []types.EntityRecord{
		{
			ID: consumerPlaceholder467, Kind: "SCOPE.Component", Name: "toolkit",
			Subtype: "import", SourceFile: consumerFile6467, Language: "typescript",
			Properties: map[string]string{
				"external_dependency": "true",
				"provenance":          "INFERRED_FROM_IMPORT_STATEMENT",
			},
			Relationships: []types.RelationshipRecord{
				{FromID: consumerFile6467, ToID: "toolkit", Kind: "IMPORTS"},
			},
		},
		{
			// `export function Rows(toolkit) { … }` — a formal parameter, as
			// javascript/dataflow_react.go:77 actually emits it (Properties
			// verbatim from that emitter). No local_scope stamp: the dataflow
			// pass runs outside tagLocalScope's reach.
			ID: rowsLocalConstID6467, Kind: "SCOPE.Operation", Name: "toolkit",
			Subtype: "component_prop", SourceFile: rowsFile6467, Language: "typescript",
			Properties: map[string]string{
				"kind": "SCOPE.Operation", "subtype": "component_prop",
				"component": "Rows", "prop": "toolkit", "framework": "react",
			},
		},
	}

	idx := resolve.BuildIndex(recs)
	resolve.ReferencesEmbedded(recs, idx)

	doc := asDocument6467(recs)
	external.Synthesize(doc)

	imp := findRel6467(doc, "IMPORTS", consumerFile6467)
	if imp == nil {
		t.Fatalf("fixture is vacuous: no IMPORTS edge from %s", consumerFile6467)
	}
	const wantImport = "ext:toolkit"
	if imp.ToID != wantImport {
		t.Errorf("%s -IMPORTS-> %q, want %q: a formal parameter captured the "+
			"repository-wide slot for an imported package name (#6467). `local_scope` does "+
			"not cover this shape, so a tier keyed on it alone is half a fix",
			consumerFile6467, imp.ToID, wantImport)
	}
}

// TestRazorParameterIsNotALocalBinding_6467 and TestBicepParamIsNotALocalBinding_6467
// pin the boundary of the locality tier from the OTHER side.
//
// The first draft of isLocalBindingKind also matched the subtypes "parameter"
// and "param", on the stated grounds that they were "the spellings csharp,
// rust, kotlin, scala, groovy, razor and verilog use". That was wrong for six
// of the seven — those greps matched tree-sitter NODE TYPES, not entity
// subtypes. The only two entity emitters were these, and neither is a
// callable-local:
//
//   - razor/extractor.go:415 emits a Blazor `[Parameter]` PUBLIC COMPONENT
//     PROPERTY: Kind SCOPE.Component, bare Name, QualifiedName "Comp.Prop".
//     It is addressable as an attribute from every other .razor file.
//   - bicep/extractor.go:315 emits a template `param`: Kind SCOPE.Schema,
//     Name prefixed "param.".
//
// byName is repo-wide AND cross-language, so classifying them as local
// collided each declaration into ambiguity against ANY import placeholder
// anywhere carrying the same name — measured on both shapes: byName went from
// "d1" to "" and ambiguous from false to true. Neither #6427's tests nor the
// first draft of this file covered a razor-shaped record, which is why it
// slipped through; these two tests exist so it cannot slip again.
func TestRazorParameterIsNotALocalBinding_6467(t *testing.T) {
	assertDeclarationKeepsSlot6467(t, "razor [Parameter] component property",
		types.EntityRecord{
			ID: "dddd000000000001", Kind: "SCOPE.Component", Name: "Data",
			QualifiedName: "Chart.Data", Subtype: "parameter",
			SourceFile: "Components/Chart.razor", Language: "razor",
		})
}

func TestBicepParamIsNotALocalBinding_6467(t *testing.T) {
	assertDeclarationKeepsSlot6467(t, "bicep template param",
		types.EntityRecord{
			ID: "dddd000000000002", Kind: "SCOPE.Schema", Name: "param.location",
			Subtype: "param", SourceFile: "infra/main.bicep", Language: "bicep",
		})
}

// assertDeclarationKeepsSlot6467 pairs one declaration against an unrelated
// file's import placeholder carrying the same name, in BOTH extraction orders,
// and asserts the declaration still holds the repository-wide slot: the
// placeholder's IMPORTS edge binds to it. If the declaration is (mis)treated
// as a local binding the pairing collides, the name goes ambiguous, and the
// edge falls through to the external binder instead.
func assertDeclarationKeepsSlot6467(t *testing.T, label string, decl types.EntityRecord) {
	t.Helper()
	const phID = "dddd00000000000f"
	placeholder := types.EntityRecord{
		ID: phID, Kind: "SCOPE.Component", Name: decl.Name,
		Subtype: "import", SourceFile: "app/importer.ts", Language: "typescript",
		Properties: map[string]string{
			"external_dependency": "true",
			"provenance":          "INFERRED_FROM_IMPORT_STATEMENT",
		},
		Relationships: []types.RelationshipRecord{
			{FromID: "app/importer.ts", ToID: decl.Name, Kind: "IMPORTS"},
		},
	}
	orders := []struct {
		tag  string
		recs []types.EntityRecord
	}{
		{"placeholder indexed first", []types.EntityRecord{placeholder, decl}},
		{"declaration indexed first", []types.EntityRecord{decl, placeholder}},
	}
	for _, o := range orders {
		t.Run(o.tag, func(t *testing.T) {
			recs := append([]types.EntityRecord(nil), o.recs...)
			idx := resolve.BuildIndex(recs)
			resolve.ReferencesEmbedded(recs, idx)

			var got string
			seen := 0
			for i := range recs {
				for _, r := range recs[i].Relationships {
					if r.Kind == "IMPORTS" {
						seen++
						got = r.ToID
					}
				}
			}
			if seen != 1 {
				t.Fatalf("fixture is vacuous: found %d IMPORTS edge(s), want 1", seen)
			}
			if got != decl.ID {
				t.Errorf("app/importer.ts -IMPORTS-> %q, want the declaration %q: a %s is "+
					"addressable outside the file that declares it, so it must keep the "+
					"repository-wide byName slot. Treating its subtype (%q) as a "+
					"callable-local collides it into ambiguity against any same-named "+
					"import placeholder in ANY language (#6467)",
					got, decl.ID, label, decl.Subtype)
			}
		})
	}
}

// TestLocalBindingDoesNotReclaimPlaceholderOnlyAmbiguity_6467 covers the third
// application site — the #6369 placeholder-only-ambiguity RECOVERY guard.
//
// #6369 lets a real declaration arriving after two colliding placeholders
// reclaim the name, because extraction order is not stable. #6467 adds that a
// function-local binding is not such a declaration. Without the `isLocal ||`
// term the local const clears the ambiguity and takes the slot for the whole
// repository — which is the reported regression, reached by a different path
// than the two precedence arms: here the name is ALREADY ambiguous when the
// local arrives, so neither arm of the collision switch ever runs.
//
// Shape: two files import the same package (two distinct placeholders → the
// name is placeholder-only ambiguous), and a third file declares a
// function-body `const` of that name. All three imports must still reach
// ext:toolkit.
func TestLocalBindingDoesNotReclaimPlaceholderOnlyAmbiguity_6467(t *testing.T) {
	const (
		phA    = "eeee000000000001"
		phB    = "eeee000000000002"
		localC = "eeee000000000003"
		fileA  = "src/a.tsx"
		fileB  = "src/b.tsx"
	)
	placeholder := func(id, file string) types.EntityRecord {
		return types.EntityRecord{
			ID: id, Kind: "SCOPE.Component", Name: "toolkit",
			Subtype: "import", SourceFile: file, Language: "typescript",
			Properties: map[string]string{
				"external_dependency": "true",
				"provenance":          "INFERRED_FROM_IMPORT_STATEMENT",
			},
			Relationships: []types.RelationshipRecord{
				{FromID: file, ToID: "toolkit", Kind: "IMPORTS"},
			},
		}
	}
	recs := []types.EntityRecord{
		placeholder(phA, fileA),
		placeholder(phB, fileB),
		{
			// `const toolkit = …` inside a function body in a third file,
			// arriving AFTER the name is already placeholder-only ambiguous.
			ID: localC, Kind: "SCOPE.Component", Name: "toolkit",
			Subtype: "const", SourceFile: rowsFile6467, Language: "typescript",
			Properties: map[string]string{"local_scope": "true"},
		},
	}

	idx := resolve.BuildIndex(recs)
	resolve.ReferencesEmbedded(recs, idx)

	doc := asDocument6467(recs)
	external.Synthesize(doc)

	seen := 0
	for _, from := range []string{fileA, fileB} {
		imp := findRel6467(doc, "IMPORTS", from)
		if imp == nil {
			t.Fatalf("fixture is vacuous: no IMPORTS edge from %s", from)
		}
		seen++
		if imp.ToID != "ext:toolkit" {
			t.Errorf("%s -IMPORTS-> %q, want %q: a function-local binding must not "+
				"reclaim a name that only import placeholders contested — #6369's "+
				"recovery is for real declarations, and a body-local const is not one "+
				"(#6467)", from, imp.ToID, "ext:toolkit")
		}
	}
	if seen != 2 {
		t.Fatalf("fixture is vacuous: found %d IMPORTS edge(s), want 2", seen)
	}
}

// TestLocalRecordDoesNotUnclaimAnAddressableID_6467 covers the fourth
// application site — the AND-ed insert tail.
//
// EntityID is sha256(orgID, projectID, sourceFile, kind, name) and hashes
// NEITHER Subtype NOR Properties, so two records that differ only in subtype
// share ONE id by construction. The production shape: one file exports
// `function Widget() {…}` AND destructures a prop of the same name in a
// sibling component, `function Panel({ Widget }) {…}`. Both are SCOPE.Operation
// named "Widget" in that file, so the addressable function and the
// component_prop arrive at indexByName as re-indexes of a single entity.
//
// Locality status is therefore AND-ed: an id known under any addressable
// record is addressable, and the trailing local record must not flip the flag
// back. If it does, the flag describes the last record that MENTIONED the name
// rather than the entity sitting in byName, and the next file's import
// placeholder collides with a declaration that is in fact perfectly
// addressable — the exact bookkeeping bug #6369's follow-up had to fix on
// nameHolderImport, reproduced on nameHolderLocal.
func TestLocalRecordDoesNotUnclaimAnAddressableID_6467(t *testing.T) {
	const (
		sharedID = "ffff000000000001"
		phID     = "ffff000000000002"
		panelTSX = "src/panel.tsx"
		userTSX  = "src/user.tsx"
	)
	recs := []types.EntityRecord{
		{
			// `export function Widget() {…}` — addressable, module scope.
			ID: sharedID, Kind: "SCOPE.Operation", Name: "Widget",
			Subtype: "function", SourceFile: panelTSX, Language: "typescript",
		},
		{
			// `function Panel({ Widget }) {…}` in the SAME file — a
			// component_prop. Same repo+file+kind+name, so ComputeID gives it
			// the same id as the function above.
			ID: sharedID, Kind: "SCOPE.Operation", Name: "Widget",
			Subtype: "component_prop", SourceFile: panelTSX, Language: "typescript",
		},
		{
			// Another file's `import Widget from "widget"` placeholder.
			ID: phID, Kind: "SCOPE.Component", Name: "Widget",
			Subtype: "import", SourceFile: userTSX, Language: "typescript",
			Properties: map[string]string{
				"external_dependency": "true",
				"provenance":          "INFERRED_FROM_IMPORT_STATEMENT",
			},
			Relationships: []types.RelationshipRecord{
				{FromID: userTSX, ToID: "Widget", Kind: "IMPORTS"},
			},
		},
	}

	idx := resolve.BuildIndex(recs)
	resolve.ReferencesEmbedded(recs, idx)

	seen := 0
	var got string
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == "IMPORTS" {
				seen++
				got = r.ToID
			}
		}
	}
	if seen != 1 {
		t.Fatalf("fixture is vacuous: found %d IMPORTS edge(s), want 1", seen)
	}
	if got != sharedID {
		t.Errorf("%s -IMPORTS-> %q, want the exported function %q: locality status must be "+
			"AND-ed, never re-raised. A trailing component_prop record sharing the id of an "+
			"addressable declaration must not mark that id local, or the next file's "+
			"placeholder collides with a declaration it should bind to (#6467)",
			userTSX, got, sharedID)
	}
}

// ── #6467 round 3: which component_prop emitters are actually locals ─────────
//
// isLocalBindingKind's second signal was `subtype == "component_prop"`, with no
// further qualification. There are exactly three emitters of that subtype and
// only ONE of them is a callable-local:
//
//   - javascript/dataflow_react.go:77 — the destructured or whole-object props
//     PARAMETER of a React component. A binding in the callable's own scope.
//   - javascript/angular.go:665 — an `@Input()` CLASS FIELD.
//   - vue/extractor.go:854 — a `defineProps` entry.
//
// The last two are the component's PUBLIC surface, addressable from a parent
// template (`<chart [Data]="…">`, `<Chart :Data="…">`) — structurally the same
// record as razor's `[Parameter]` above. Measured against main@640ea7784 with
// a probe reproducing each emitter's real record: on main all three held the
// slot (byName=<decl>, ambig=false); with the unguarded arm all three went
// ambiguous (byName="", ambig=true). Two of those three were regressions.
//
// Nothing record-local but Properties["framework"] separates the shapes — same
// Kind (SCOPE.Operation), same bare Name, same "Comp.Prop" QualifiedName, no
// local_scope stamp on any of them, and react's and angular's share Language
// "typescript" — so the arm is gated on framework=="react". These three tests
// pin both sides of that gate.
func TestAngularInputIsNotALocalBinding_6467(t *testing.T) {
	assertDeclarationKeepsSlot6467(t, "angular @Input() component property",
		types.EntityRecord{
			ID: "dddd000000000003", Kind: "SCOPE.Operation", Name: "Data",
			QualifiedName: "ChartComponent.Data", Subtype: "component_prop",
			SourceFile: "src/chart.component.ts", Language: "typescript",
			Properties: map[string]string{
				"kind": "SCOPE.Operation", "subtype": "component_prop",
				"component": "ChartComponent", "prop": "Data",
				"prop_direction": "input", "framework": "angular",
			},
		})
}

func TestVueDefinePropsIsNotALocalBinding_6467(t *testing.T) {
	assertDeclarationKeepsSlot6467(t, "vue defineProps component property",
		types.EntityRecord{
			ID: "dddd000000000004", Kind: "SCOPE.Operation", Name: "Data",
			QualifiedName: "Chart.Data", Subtype: "component_prop",
			SourceFile: "src/Chart.vue", Language: "vue",
			Properties: map[string]string{
				"prop": "Data", "component": "Chart", "framework": "vue",
			},
		})
}

// TestReactPropsParameterIsALocalBinding_6467 pins the OTHER side of the gate:
// narrowing to framework=="react" must not have narrowed the arm out of
// existence. A React props parameter carries no local_scope stamp (the
// dataflow pass emits outside tagLocalScope's reach), so this arm is the only
// thing that stops it owning the repository-wide slot.
func TestReactPropsParameterIsALocalBinding_6467(t *testing.T) {
	assertDeclarationLosesSlot6467(t, "react props parameter",
		types.EntityRecord{
			ID: "dddd000000000005", Kind: "SCOPE.Operation", Name: "Data",
			QualifiedName: "Chart.Data", Subtype: "component_prop",
			SourceFile: "src/Chart.tsx", Language: "typescript",
			Properties: map[string]string{
				"kind": "SCOPE.Operation", "subtype": "component_prop",
				"component": "Chart", "prop": "Data", "framework": "react",
			},
		})
}

// assertDeclarationLosesSlot6467 is the mirror of assertDeclarationKeepsSlot6467:
// the record IS a callable-local, so it must NOT capture the repository-wide
// slot — the placeholder's IMPORTS edge must not bind to it, leaving the name
// for the external-library binder.
func assertDeclarationLosesSlot6467(t *testing.T, label string, decl types.EntityRecord) {
	t.Helper()
	placeholder := types.EntityRecord{
		ID: "dddd00000000001f", Kind: "SCOPE.Component", Name: decl.Name,
		Subtype: "import", SourceFile: "app/importer.ts", Language: "typescript",
		Properties: map[string]string{
			"external_dependency": "true",
			"provenance":          "INFERRED_FROM_IMPORT_STATEMENT",
		},
		Relationships: []types.RelationshipRecord{
			{FromID: "app/importer.ts", ToID: decl.Name, Kind: "IMPORTS"},
		},
	}
	orders := []struct {
		tag  string
		recs []types.EntityRecord
	}{
		{"placeholder indexed first", []types.EntityRecord{placeholder, decl}},
		{"declaration indexed first", []types.EntityRecord{decl, placeholder}},
	}
	for _, o := range orders {
		t.Run(o.tag, func(t *testing.T) {
			recs := append([]types.EntityRecord(nil), o.recs...)
			idx := resolve.BuildIndex(recs)
			resolve.ReferencesEmbedded(recs, idx)

			var got string
			seen := 0
			for i := range recs {
				for _, r := range recs[i].Relationships {
					if r.Kind == "IMPORTS" {
						seen++
						got = r.ToID
					}
				}
			}
			if seen != 1 {
				t.Fatalf("fixture is vacuous: found %d IMPORTS edge(s), want 1", seen)
			}
			if got == decl.ID {
				t.Errorf("app/importer.ts -IMPORTS-> %q, the %s declaration: a binding that "+
					"exists only inside the callable that declares it must NOT hold the "+
					"repository-wide byName slot, or an unrelated file's import of the same "+
					"name binds to it instead of reaching the external library (#6467)",
					got, label)
			}
		})
	}
}

// TestReactComponentDeclarationIsNotALocalBinding_6467 pins the SUBTYPE half of
// the react arm, which the framework gate left unpinned.
//
// Surviving mutant (found in review): with the gate in place,
//
//	return subtype != "" && props["framework"] == "react"
//
// passed `go vet` and the whole ./internal/resolve/... suite. Nothing
// distinguished a react `component_prop` from ANY other react-stamped subtype,
// so the framework check alone was carrying the arm — and a later relaxation of
// the subtype side, trusting `framework == "react"` to hold the line, would have
// drawn no objection from any test.
//
// The killer is a record that is react-stamped and NOT a props parameter:
// cross/react_props/extractor.go:816 `buildComponentEntity` emits the COMPONENT
// ITSELF as SCOPE.Operation / Subtype "react_component" with
// Properties["framework"]="react" (map copied verbatim from that emitter,
// including `ref` and `provenance`). A component declaration is the single most
// addressable record in a React codebase — `import { Chart } from "./Chart"` in
// any other file must bind to it — so it must keep the repository-wide slot.
func TestReactComponentDeclarationIsNotALocalBinding_6467(t *testing.T) {
	assertDeclarationKeepsSlot6467(t, "react component declaration",
		types.EntityRecord{
			ID: "dddd000000000006", Kind: "SCOPE.Operation", Name: "Chart",
			Subtype: "react_component", SourceFile: "src/Chart.tsx",
			Language: "typescript",
			Properties: map[string]string{
				"framework":  "react",
				"component":  "true",
				"props":      "Data, onSelect",
				"ref":        "scope:operation:src/Chart.tsx#Chart",
				"provenance": "INFERRED_FROM_REACT_PROPS_EXTRACTOR",
			},
		})
}
