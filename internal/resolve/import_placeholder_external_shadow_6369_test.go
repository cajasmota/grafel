// Package resolve — #6369 follow-up: an import placeholder must not SHADOW the
// external-library binder.
//
// The placeholder-precedence rule in indexByName has two directions, and the
// first pass only pinned one of them:
//
//   - a placeholder may OCCUPY an unclaimed name (css `@import "theme.css"`,
//     graphql `extend type User`) — pinned by
//     TestImportPlaceholderStillResolvesOwnEdge_6369;
//   - a placeholder may NOT become the sole owner of a name that many files
//     import, because a stdlib/external import has to stay UNBOUND through the
//     bare-name pass so the downstream external-library binder can turn it into
//     `ext:<pkg>`. That is what this file pins.
//
// THE SHAPE THAT BROKE IT — measured, not hypothetical.
//
// graph.EntityID is sha256(repo, kind, name, sourceFile). Subtype is NOT an
// input. So the per-import `SCOPE.Component` placeholder shares its ID BY
// CONSTRUCTION with any other SCOPE.Component of the same name in the same
// file — and the Go corpus emits exactly that: for `import "strings"` the
// language extractor emits unmarked `SCOPE.Component` records and
// cross/imports emits the `Subtype:"import"` one, all on ONE ID (observed
// three records per file on the corpus in
// cmd/grafel/incremental_dup_rows_6094_test.go).
//
// Those extra records are RE-INDEXES of one entity, not collisions, so they
// skip the collision branch entirely. When the trailing placeholder record was
// allowed to re-raise nameHolderImport, the flag stopped describing the entity
// in byName and started describing the last record that mentioned it. The next
// file's real record then took the "a real declaration displaces the
// placeholder" branch instead of colliding — file after file, so a name every
// file in the repo imports NEVER went ambiguous (ambiguous=0), the last file
// processed won the slot outright, and every other file's `strings` import
// bound to THAT file's placeholder: 12 bogus intra-repo edges where 12
// `ext:strings` edges belonged.
package resolve

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

const (
	stringsR00ID6369 = "bbbb000000000001" // r00.go's `strings` entity
	stringsR01ID6369 = "bbbb000000000002" // r01.go's `strings` entity
)

// externalImportFile6369 emits the records ONE Go file produces for
// `import "strings"`: the unmarked SCOPE.Component the language extractor
// mints, then the Subtype:"import" placeholder cross/imports mints. Both carry
// the SAME id on purpose — graph.EntityID does not hash Subtype, so in
// production they cannot differ. The bare-name IMPORTS edge rides on the
// placeholder, exactly as the cross/imports records do.
func externalImportFile6369(id, file string) []types.EntityRecord {
	return []types.EntityRecord{
		{
			ID: id, Kind: "SCOPE.Component", Name: "strings",
			SourceFile: file, Language: "go",
		},
		{
			ID: id, Kind: "SCOPE.Component", Name: "strings", Subtype: "import",
			SourceFile: file, Language: "go",
			Properties: map[string]string{
				"external_dependency": "true",
				"provenance":          "INFERRED_FROM_IMPORT_STATEMENT",
			},
			Relationships: []types.RelationshipRecord{
				{FromID: file, ToID: "strings", Kind: "IMPORTS"},
			},
		},
	}
}

// TestExternalImportDoesNotBindToAnotherFilesPlaceholder_6369 is the gate.
//
// Two files import the same stdlib package. Neither declares it. The name is
// therefore owned by nobody, and every IMPORTS edge must survive the bare-name
// pass UNBOUND so the external-library binder can emit `ext:strings` — the
// binder is the only thing downstream of this that knows what an external
// package is.
func TestExternalImportDoesNotBindToAnotherFilesPlaceholder_6369(t *testing.T) {
	recs := append(
		externalImportFile6369(stringsR00ID6369, "r00.go"),
		externalImportFile6369(stringsR01ID6369, "r01.go")...,
	)

	idx := BuildIndex(recs)

	// The name is contested by two files' placeholders and claimed by no
	// declaration, so it must be AMBIGUOUS. rewriteOneWithCaller only reaches
	// its fallbacks on statusAmbiguous — an arbitrary winner here is precisely
	// what pre-empts the external binder.
	if !idx.ambigName["strings"] {
		t.Errorf("byName[%q] = %q (ambiguous=false): one file's import placeholder was handed "+
			"sole ownership of a name no file declares — every other file's stdlib import now binds "+
			"to that file's placeholder instead of falling through to the external-library binder",
			"strings", idx.byName["strings"])
	}

	ReferencesEmbedded(recs, idx)

	// Non-vacuity: the two files really are distinct entities, and each file's
	// two records really do share one ID (the production shape this pins).
	if stringsR00ID6369 == stringsR01ID6369 {
		t.Fatal("fixture is vacuous: both files share an entity ID")
	}
	foreign := map[string]string{stringsR00ID6369: "r01.go", stringsR01ID6369: "r00.go"}

	seen := 0
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			seen++
			if other, isPlaceholder := foreign[r.ToID]; isPlaceholder && other != recs[i].SourceFile {
				t.Errorf("%s -IMPORTS-> %q bound to %s's import placeholder; a stdlib/external import "+
					"must stay unbound here so the external-library binder can emit ext:strings",
					recs[i].SourceFile, r.ToID, other)
			}
			if r.ToID != "strings" {
				t.Errorf("%s -IMPORTS-> %q: want the bare name %q left unresolved for the "+
					"external-library binder", recs[i].SourceFile, r.ToID, "strings")
			}
		}
	}
	if seen != 2 {
		t.Fatalf("fixture is vacuous: found %d IMPORTS edge(s), want 2", seen)
	}
}
