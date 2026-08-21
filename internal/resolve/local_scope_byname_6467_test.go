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
// A formal parameter is scoped to its callable in every language, so it can
// never legitimately own a repository-wide name — which is why the second
// signal keys on the parameter subtypes rather than on `local_scope` alone.
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
			// `export function Rows(toolkit) { … }` — a formal parameter.
			// No local_scope stamp: the extractor does not tag parameters.
			ID: rowsLocalConstID6467, Kind: "SCOPE.Operation", Name: "toolkit",
			Subtype: "component_prop", SourceFile: rowsFile6467, Language: "typescript",
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
