package resolve

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6372. An import placeholder's module specifier was read as
//
//	module := r.Name
//	if r.Properties != nil { if m := r.Properties["module"]; m != "" { module = m } }
//
// in two places — the #642 pre-prune relative-import ToID rewrite inside
// PruneImportPlaceholdersWithLiveIDs, and the #6156 `ext:` module restore in
// buildPlaceholderModuleRestores. QualifiedName was never consulted, so the
// four extractors that carry the FULL module path in QualifiedName and only a
// short segment in Name (razor/extractor.go:248 topSegment(ns);
// vue/extractor.go:1361 moduleShortName(modulePath); markdown, graphql) handed
// the resolver a basename where a specifier was expected — silently.
//
// Precedence: Properties["module"] > QualifiedName > Name.
//
//   - QualifiedName must beat Name: the razor row (Name = topSegment(ns) =
//     "Microsoft", QualifiedName = "Microsoft.AspNetCore.Components") and the
//     vue row (Name = moduleShortName, QualifiedName = the full "./pages/Home")
//     both break under the Name-first ordering. No emission site sets
//     QualifiedName to anything other than the full module path, so this half
//     is safe for all 16 sites — the other ten set no QualifiedName at all and
//     keep falling back to Name.
//   - Properties["module"] stays on top: it is the explicit, purpose-named
//     channel (javascript/extractor.go:3790 is the only site that sets it, and
//     sets it equal to Name) and keeping it highest is the no-regression
//     choice for anything that stamps it.
//
// Both sites must agree; a divergence would be a new silent bug of exactly the
// kind being fixed. Every case below is asserted against BOTH sites.
type moduleSpecCase struct {
	name string
	// how the placeholder carries its specifier
	entName       string
	qualifiedName string
	props         map[string]string
	// site 1 (relative form) and site 2 (non-relative form) expectations
	wantRelative    string // "" == no rewrite expected
	wantRestoreFrom string // non-relative specifier the restore must record
}

func moduleSpecCases() []moduleSpecCase {
	return []moduleSpecCase{
		{
			// razor/vue shape: full path in QualifiedName only.
			name:            "qualified_name_only",
			entName:         "Home",
			qualifiedName:   "%SPEC%",
			wantRelative:    "id-home",
			wantRestoreFrom: "%SPEC%",
		},
		{
			// Properties["module"] must still win over QualifiedName.
			name:            "properties_module_beats_qualified_name",
			entName:         "Home",
			qualifiedName:   "./pages/Wrong",
			props:           map[string]string{"module": "%SPEC%"},
			wantRelative:    "id-home",
			wantRestoreFrom: "%SPEC%",
		},
		{
			// The ten extractors that put the full path in Name and set no
			// QualifiedName must keep resolving off Name.
			name:            "name_only",
			entName:         "%SPEC%",
			wantRelative:    "id-home",
			wantRestoreFrom: "%SPEC%",
		},
	}
}

// subst replaces the %SPEC% placeholder with the specifier shape a given site
// exercises (relative for site 1, dotted-namespace for site 2).
func subst(s, spec string) string {
	if s == "%SPEC%" {
		return spec
	}
	return s
}

// TestPrunePreservesQualifiedNameSpecifier_6372 covers site 1: the #642
// pre-prune relative-import ToID rewrite in PruneImportPlaceholdersWithLiveIDs.
func TestPrunePreservesQualifiedNameSpecifier_6372(t *testing.T) {
	const spec = "./pages/Home"
	for _, tc := range moduleSpecCases() {
		t.Run(tc.name, func(t *testing.T) {
			props := map[string]string(nil)
			if tc.props != nil {
				props = make(map[string]string, len(tc.props))
				for k, v := range tc.props {
					props[k] = subst(v, spec)
				}
			}
			records := []types.EntityRecord{
				{
					ID:         "id-app",
					Kind:       "SCOPE.Component",
					Subtype:    "file",
					Name:       "App.tsx",
					SourceFile: "src/App.tsx",
					Relationships: []types.RelationshipRecord{
						{FromID: "id-app", ToID: "ph-1", Kind: importRelKind},
					},
				},
				{
					ID:         "id-home",
					Kind:       "SCOPE.Component",
					Subtype:    "file",
					Name:       "Home.tsx",
					SourceFile: "src/pages/Home.tsx",
				},
				{
					ID:            "ph-1",
					Kind:          "SCOPE.Component",
					Subtype:       "import",
					Name:          subst(tc.entName, spec),
					QualifiedName: subst(tc.qualifiedName, spec),
					Properties:    props,
					SourceFile:    "src/App.tsx",
				},
			}

			out, _, _ := PruneImportPlaceholdersWithLiveIDs(records, nil)

			var got string
			for i := range out {
				if out[i].ID != "id-app" {
					continue
				}
				for _, rel := range out[i].Relationships {
					if rel.Kind == importRelKind {
						got = rel.ToID
					}
				}
			}
			want := tc.wantRelative
			if want == "" {
				want = "ph-1"
			}
			if got != want {
				t.Fatalf("IMPORTS ToID after prune = %q, want %q (placeholder Name=%q QualifiedName=%q Properties=%v)",
					got, want, subst(tc.entName, spec), subst(tc.qualifiedName, spec), props)
			}
		})
	}
}

// TestModuleRestoreUsesQualifiedNameSpecifier_6372 covers site 2: the #6156
// `ext:` endpoint restore in buildPlaceholderModuleRestores.
func TestModuleRestoreUsesQualifiedNameSpecifier_6372(t *testing.T) {
	const spec = "Microsoft.AspNetCore.Components"
	for _, tc := range moduleSpecCases() {
		t.Run(tc.name, func(t *testing.T) {
			props := map[string]string(nil)
			if tc.props != nil {
				props = make(map[string]string, len(tc.props))
				for k, v := range tc.props {
					props[k] = subst(v, spec)
				}
			}
			qn := subst(tc.qualifiedName, spec)
			if qn == "./pages/Wrong" {
				// non-relative counterpart of the "wrong" specifier
				qn = "Wrong.Namespace"
			}
			records := []types.EntityRecord{
				{
					ID:            "ph-1",
					Kind:          "SCOPE.Component",
					Subtype:       "import",
					Name:          subst(tc.entName, spec),
					QualifiedName: qn,
					Properties:    props,
					SourceFile:    "Pages/Index.razor",
				},
			}

			got := buildPlaceholderModuleRestores(records, []bool{true}, map[string]bool{})

			want := subst(tc.wantRestoreFrom, spec)
			if got["ph-1"] != want {
				t.Fatalf("restored module = %q, want %q (placeholder Name=%q QualifiedName=%q Properties=%v)",
					got["ph-1"], want, subst(tc.entName, spec), qn, props)
			}
		})
	}
}
