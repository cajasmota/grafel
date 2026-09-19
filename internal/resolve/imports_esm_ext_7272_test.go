package resolve

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// Issue #7272 Finding 1 — TypeScript under Node16/NodeNext ESM requires the
// import specifier to carry a `.js` extension even though the file on disk is
// `.ts`:
//
//	import { CategoriesService } from './categories.service.js'; // file: .ts
//
// resolveRelativeImportTarget's fallback loop APPENDED each canonical
// extension to the joined path, producing `categories.service.js.ts`,
// `.js.tsx`, `.js.js` — none of which can ever name a real file. The loop
// never tried REPLACING the specifier's extension, so every such import was
// unresolvable by construction and its IMPORTS edge landed in bug-extractor.
//
// These tests pin the REPLACEMENT behaviour, and — because recall cannot
// detect over-firing — pin an explicit set of specifiers that must NOT
// resolve after the widening.

// TestResolveRelativeImportTarget_ExtensionCrossProduct_7272 enumerates the
// full cross-product of (specifier extension) x (real carrier extension),
// including the no-extension and non-canonical cases, and grades each cell
// against a HAND-WRITTEN truth table. The expectation is tabulated, never
// recomputed from the production rule.
func TestResolveRelativeImportTarget_ExtensionCrossProduct_7272(t *testing.T) {
	// Every specifier extension we exercise. "" is the extensionless form
	// (classic bundler/CJS style); ".json"/".css" are non-canonical asset
	// specifiers; ".min.js" is the compound-extension trap.
	specExts := []string{"", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".json", ".css", ".min.js"}
	// Every real on-disk extension we place the single carrier at.
	carrierExts := []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".json", ".css"}

	// allCanonical is the carrier set an extensionless-or-canonical
	// specifier must reach. Written out per row rather than shared by
	// reference so a future edit to one row cannot silently move another.
	allCanonical := []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"}

	// wantResolve[specExt] lists EXACTLY the carrier extensions that must
	// resolve for that specifier. Anything not listed is a forbidden row:
	// it must return ok=false.
	wantResolve := map[string][]string{
		// No extension: the classic form. Reaches every canonical carrier;
		// must NOT invent a binding to a .json/.css asset.
		"": allCanonical,
		// A canonical specifier extension is a HINT, not a constraint:
		// TS ESM emits `.js` for a `.ts` source, and `.mjs`/`.cjs` specs
		// appear for the same reason. Each must reach every canonical
		// carrier and no asset file.
		".ts":  {".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"},
		".tsx": {".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"},
		".js":  {".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"},
		".jsx": {".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"},
		".mjs": {".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"},
		".cjs": {".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"},
		// Non-canonical specifier extensions are NOT hints. `./m.json`
		// names a JSON asset; it must bind to `m.json` if that carrier
		// exists (direct hit) and to NOTHING else. A resolver that
		// stripped any trailing `.`-segment would bind it to `m.ts`.
		".json": {".json"},
		".css":  {".css"},
		// Compound extension: stripping the trailing `.js` leaves the stem
		// `m.min`, so the only legitimate targets are `m.min.<canonical>`.
		// The carriers in this table all live at `m.<ext>`, so NOTHING may
		// resolve. A too-greedy strip would reach `m.ts`.
		".min.js": {},
	}

	for _, specExt := range specExts {
		for _, carrierExt := range carrierExts {
			specExt, carrierExt := specExt, carrierExt
			name := "spec" + orNone(specExt) + "_carrier" + carrierExt
			t.Run(name, func(t *testing.T) {
				carriers := map[string]string{
					"src/m" + carrierExt: "carrier-id",
				}
				id, ok := resolveRelativeImportTarget("src/app.ts", "./m"+specExt, carriers)
				want := containsExt(wantResolve[specExt], carrierExt)
				if want && (!ok || id != "carrier-id") {
					t.Fatalf("specifier %q against carrier %q = (%q, %v), want (\"carrier-id\", true): "+
						"a canonical specifier extension is a hint, not a constraint — TS ESM writes "+
						"`.js` for a `.ts` source", "./m"+specExt, "src/m"+carrierExt, id, ok)
				}
				if !want && ok {
					t.Fatalf("specifier %q resolved to carrier %q (id %q) — FORBIDDEN: the extension "+
						"replacement must strip only a canonical extension, never an arbitrary "+
						"trailing `.`-segment", "./m"+specExt, "src/m"+carrierExt, id)
				}
			})
		}
	}
}

// TestResolveRelativeImportTarget_DirectHitOutranksReplacement_7272 pins the
// precedence the widening must not disturb: a carrier that literally matches
// the specifier wins over the extension-replacement candidates, even when the
// replacement set would otherwise prefer `.ts`.
func TestResolveRelativeImportTarget_DirectHitOutranksReplacement_7272(t *testing.T) {
	carriers := map[string]string{
		"src/foo.ts": "ts-id",
		"src/foo.js": "js-id",
	}
	// Direct hit: `./foo.js` names a real `foo.js`, so it must win even
	// though `.ts` sorts first in the replacement order.
	if id, ok := resolveRelativeImportTarget("src/app.ts", "./foo.js", carriers); !ok || id != "js-id" {
		t.Fatalf("./foo.js with both carriers present = (%q, %v), want (\"js-id\", true): "+
			"the literal on-disk match must outrank extension replacement", id, ok)
	}
	// Negative control for the direct hit: the extensionless form has no
	// literal match, so canonical order decides and `.ts` wins. This row is
	// GREEN both before and after the fix — the table is not uniformly
	// sensitive to the change.
	if id, ok := resolveRelativeImportTarget("src/app.ts", "./foo", carriers); !ok || id != "ts-id" {
		t.Fatalf("./foo with both carriers present = (%q, %v), want (\"ts-id\", true)", id, ok)
	}
}

// TestResolveRelativeImportTarget_MidNameExtension_7272 covers a file whose
// real name legitimately contains a canonical extension mid-name.
func TestResolveRelativeImportTarget_MidNameExtension_7272(t *testing.T) {
	carriers := map[string]string{
		"src/react.js.helpers.ts": "helpers-id",
		"src/react.ts":            "react-id",
	}
	cases := []struct {
		spec string
		want string // "" means must not resolve
		why  string
	}{
		{"./react.js.helpers", "helpers-id",
			"no trailing canonical extension — the mid-name `.js` must not be stripped"},
		{"./react.js.helpers.js", "helpers-id",
			"TS ESM form of the same file; only the TRAILING `.js` is stripped"},
		{"./react.js", "react-id",
			"trailing `.js` strips to `react`, which has a `.ts` carrier"},
		{"./react.js.missing", "",
			"stem `react.js.missing` has no canonical carrier; must not fall back to `react.ts`"},
	}
	for _, tc := range cases {
		id, ok := resolveRelativeImportTarget("src/app.ts", tc.spec, carriers)
		if tc.want == "" {
			if ok {
				t.Errorf("%s resolved to %q — FORBIDDEN (%s)", tc.spec, id, tc.why)
			}
			continue
		}
		if !ok || id != tc.want {
			t.Errorf("%s = (%q, %v), want (%q, true) (%s)", tc.spec, id, ok, tc.want, tc.why)
		}
	}
}

// TestResolveRelativeImportTarget_DirectoryIndex_7272 records why the
// directory-index loop needs no extension-replacement treatment of its own:
// the ESM spelling of a barrel import is `./widgets/index.js`, whose
// extension sits on the FILE segment and is therefore handled by the main
// replacement loop. `./widgets.js` is not a legal spelling of a directory
// import and must stay unresolved.
func TestResolveRelativeImportTarget_DirectoryIndex_7272(t *testing.T) {
	carriers := map[string]string{"src/widgets/index.ts": "barrel-id"}
	if id, ok := resolveRelativeImportTarget("src/app.ts", "./widgets", carriers); !ok || id != "barrel-id" {
		t.Errorf("./widgets = (%q, %v), want (\"barrel-id\", true): directory-index form", id, ok)
	}
	if id, ok := resolveRelativeImportTarget("src/app.ts", "./widgets/index.js", carriers); !ok || id != "barrel-id" {
		t.Errorf("./widgets/index.js = (%q, %v), want (\"barrel-id\", true): the ESM barrel spelling "+
			"is handled by the main replacement loop, not the index loop", id, ok)
	}
	if id, ok := resolveRelativeImportTarget("src/app.ts", "./widgets.js", carriers); ok {
		t.Errorf("./widgets.js resolved to %q — FORBIDDEN: `./widgets.js` is not a legal spelling "+
			"of a directory import", id)
	}
}

// TestPruneRewritesESMExtensionImport_7272 drives the fix through its real
// caller — the #642 pre-prune IMPORTS ToID rewrite in
// PruneImportPlaceholdersWithLiveIDs — using the exact shape the reporter's
// NestJS backend emits.
func TestPruneRewritesESMExtensionImport_7272(t *testing.T) {
	records := []types.EntityRecord{
		{
			ID: "id-app", Kind: "SCOPE.Component", Subtype: "file",
			Name: "app.module.ts", SourceFile: "src/app.module.ts",
			Relationships: []types.RelationshipRecord{
				{FromID: "id-app", ToID: "ph-1", Kind: importRelKind},
			},
		},
		{
			ID: "id-svc", Kind: "SCOPE.Component", Subtype: "file",
			Name: "categories.service.ts", SourceFile: "src/categories/categories.service.ts",
		},
		{
			ID: "ph-1", Kind: "SCOPE.Component", Subtype: "import",
			Name:       "./categories/categories.service.js",
			SourceFile: "src/app.module.ts",
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
	if got != "id-svc" {
		t.Fatalf("IMPORTS ToID after prune = %q, want %q: a Node16/NodeNext ESM `.js` specifier "+
			"naming a `.ts` file must rewrite to the file carrier, not survive as the pruned "+
			"placeholder", got, "id-svc")
	}
}

func orNone(ext string) string {
	if ext == "" {
		return "None"
	}
	return ext
}

func containsExt(list []string, ext string) bool {
	for _, e := range list {
		if e == ext {
			return true
		}
	}
	return false
}
