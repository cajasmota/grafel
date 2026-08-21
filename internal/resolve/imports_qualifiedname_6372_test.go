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

// TestPlaceholderModuleSpecifierPrefersImportModule_6369 pins the ORDERING of
// the `import_module` key against the three legacy channels.
//
// The `import_module` > `module` row is NOT hypothetical, which is the whole
// reason it is checked first rather than last. On the daemon's incremental
// reindex — the default path (#5231) — placeholders are never pruned, and
// stampModuleOnEntities (internal/extractors/incremental.go) then stamps the
// path-derived module-rollup label onto every entity that does not already
// carry one. A placeholder therefore reaches this reader carrying BOTH keys:
// `import_module` = the import specifier the extractor emitted, and `module` =
// a directory prefix like "src/Domain" that has nothing to do with the import.
// Reading `module` first would hand the #642 rewrite and the #6156 restore a
// source directory where a module specifier belongs.
//
// It is also the migration contract for the ~26 extractors that still park
// their specifier on one of the legacy channels: adding `import_module` must
// win outright, so a site can be moved over without a flag day.
func TestPlaceholderModuleSpecifierPrefersImportModule_6369(t *testing.T) {
	for _, tc := range []struct {
		name string
		rec  types.EntityRecord
		want string
	}{
		{
			name: "import_module beats the module-rollup label stamped by the incremental path",
			rec: types.EntityRecord{
				Name:       "Generic",
				Kind:       "SCOPE.Component",
				Subtype:    "import",
				SourceFile: "src/Domain/Core.fs",
				Properties: map[string]string{
					"import_module": "System.Collections.Generic",
					"module":        "src/Domain",
				},
			},
			want: "System.Collections.Generic",
		},
		{
			name: "import_module beats QualifiedName",
			rec: types.EntityRecord{
				Name:          "Generic",
				QualifiedName: "Some.Other.Path",
				Kind:          "SCOPE.Component",
				Subtype:       "import",
				Properties:    map[string]string{"import_module": "System.Collections.Generic"},
			},
			want: "System.Collections.Generic",
		},
		{
			name: "import_module beats Name",
			rec: types.EntityRecord{
				Name:       "Generic",
				Kind:       "SCOPE.Component",
				Subtype:    "import",
				Properties: map[string]string{"import_module": "System.Collections.Generic"},
			},
			want: "System.Collections.Generic",
		},
		{
			name: "empty import_module falls through to module, so the legacy sites are unchanged",
			rec: types.EntityRecord{
				Name:       "user",
				Kind:       "SCOPE.Component",
				Subtype:    "import",
				Properties: map[string]string{"import_module": "", "module": "./types/user"},
			},
			want: "./types/user",
		},
		{
			name: "absent import_module leaves the QualifiedName channel (razor/vue) intact",
			rec: types.EntityRecord{
				Name:          "Components",
				QualifiedName: "Acme.Web.Components",
				Kind:          "SCOPE.Component",
				Subtype:       "import",
			},
			want: "Acme.Web.Components",
		},
		{
			name: "absent everything still falls back to Name",
			rec: types.EntityRecord{
				Name:    "lodash",
				Kind:    "SCOPE.Component",
				Subtype: "import",
			},
			want: "lodash",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := tc.rec
			if got := placeholderModuleSpecifier(&rec); got != tc.want {
				t.Errorf("placeholderModuleSpecifier = %q, want %q", got, tc.want)
			}
		})
	}
}
