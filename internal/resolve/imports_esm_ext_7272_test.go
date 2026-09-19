package resolve

import (
	"strings"
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
	// specifiers; ".min.js" and ".ts.js" are the compound-extension traps.
	specExts := []string{
		"", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".mts", ".cts",
		".json", ".css", ".min.js", ".ts.js",
	}
	// Every real on-disk extension we place the single carrier at.
	carrierExts := []string{
		".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".mts", ".cts",
		".json", ".css",
	}

	// wantResolve[specExt] lists EXACTLY the carrier extensions that must
	// resolve for that specifier. Anything not listed is a forbidden row:
	// it must return ok=false. Each row is written out in full rather than
	// shared by reference so a future edit to one row cannot silently move
	// another.
	wantResolve := map[string][]string{
		// No extension: the classic form, no module-system signal at all.
		// Reaches every canonical carrier; must NOT invent a binding to a
		// .json/.css asset.
		"": {".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".mts", ".cts"},
		// The ordinary family. A canonical specifier extension here is a
		// HINT, not a constraint — TS ESM emits `.js` for a `.ts` source.
		// It must NOT cross into the .mjs/.cjs families: `./m.js` naming
		// `m.mts` is not a thing any toolchain does.
		".ts":  {".ts", ".tsx", ".js", ".jsx"},
		".tsx": {".ts", ".tsx", ".js", ".jsx"},
		".js":  {".ts", ".tsx", ".js", ".jsx"},
		".jsx": {".ts", ".tsx", ".js", ".jsx"},
		// Always-ESM family. `./m.mjs` can only mean `m.mts` or `m.mjs`.
		// Reaching `m.ts` would mint an edge to a file the author never
		// referenced — a NEW false positive, which is the exact defect
		// class #7272 was filed about.
		".mjs": {".mts", ".mjs"},
		".mts": {".mts", ".mjs"},
		// Always-CommonJS family, same argument.
		".cjs": {".cts", ".cjs"},
		".cts": {".cts", ".cjs"},
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
		// STACKED canonical extensions — the row that grades "strip ONE".
		// `./m.ts.js` strips to `m.ts`, whose probes are `m.ts.ts`,
		// `m.ts.tsx`, ... — all misses. A strip that repeated until no
		// canonical suffix remained would reach `m`, and then bind to the
		// `m.ts` carrier. `.min.js` cannot discriminate this because its
		// greedy stem (`m.min`) is refused either way.
		".ts.js": {},
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
					t.Fatalf("specifier %q resolved to carrier %q (id %q) — FORBIDDEN: replacement "+
						"strips exactly ONE canonical extension and stays inside that extension's "+
						"family (.js<->.ts/.tsx/.js/.jsx, .mjs<->.mts/.mjs, .cjs<->.cts/.cjs); "+
						"no edge is the correct answer here, a wrong edge is not",
						"./m"+specExt, "src/m"+carrierExt, id)
				}
			})
		}
	}
}

// TestJSExtensionsAreNotSuffixesOfEachOther_7272 grades the property every
// strip loop over jsExtensions silently depends on: each loop breaks on the
// FIRST suffix match, so if any entry were a string suffix of another the
// result would depend on slice order. Adding an extension like ".es.js"
// (which ends in ".js") breaks this and must be accompanied by a reordering.
func TestJSExtensionsAreNotSuffixesOfEachOther_7272(t *testing.T) {
	for _, a := range jsExtensions {
		for _, b := range jsExtensions {
			if a == b {
				continue
			}
			if strings.HasSuffix(a, b) {
				t.Errorf("jsExtensions entry %q ends in %q — the first-match-wins strip loops in "+
					"resolveRelativeImportTarget, modulesForJSFile and isJSImportSource become "+
					"order-dependent and %q can be stripped to %q instead of \"\"",
					a, b, a, strings.TrimSuffix(a, b))
			}
		}
	}
}

// TestJSExtensionReplacementsCoverEveryCanonicalExtension_7272 pins that the
// family table is total over jsExtensions. A canonical extension missing from
// the map silently falls back to the FULL jsExtensions replacement set — the
// exact over-firing this change exists to prevent — and no cross-product cell
// would necessarily catch it for a newly added extension.
func TestJSExtensionReplacementsCoverEveryCanonicalExtension_7272(t *testing.T) {
	for _, ext := range jsExtensions {
		family, ok := jsExtensionReplacements[ext]
		if !ok {
			t.Errorf("jsExtensions has %q but jsExtensionReplacements does not — it would fall "+
				"back to the full canonical set and could bind across module-system families", ext)
			continue
		}
		if !containsExt(family, ext) {
			t.Errorf("jsExtensionReplacements[%q] = %v, which does not contain %q itself — a "+
				"specifier must always be able to name its own on-disk extension", ext, family, ext)
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
	// This next row serves two purposes.
	//
	// (1) Negative control: the extensionless form has no literal match, so
	// canonical order decides and `.ts` wins. It is GREEN both before and
	// after the #7272 fix — the table is not uniformly sensitive to the
	// change.
	//
	// (2) It is NOT inert. It is the only assertion in this package that
	// grades the ORDER of jsExtensions: reversing that slice fails this row
	// and nothing else. Do not delete it as a redundant control.
	if id, ok := resolveRelativeImportTarget("src/app.ts", "./foo", carriers); !ok || id != "ts-id" {
		t.Fatalf("./foo with both carriers present = (%q, %v), want (\"ts-id\", true)", id, ok)
	}
}

// TestResolveRelativeImportTarget_ModuleSystemFamily_7272 is the decisive
// case for #7272's F1: a `.mjs` specifier with BOTH a `.ts` and a `.mts`
// carrier present. The cross-product table places one carrier at a time, so
// it can show that `./m.mjs` refuses `m.ts` in isolation — it cannot show
// which file wins when the wrong one is also available. Before the family
// scoping, `./x.mjs` bound to `x.ts`: the widening reached a file the author
// never referenced, turning "no edge" into "wrong edge".
func TestResolveRelativeImportTarget_ModuleSystemFamily_7272(t *testing.T) {
	cases := []struct {
		name     string
		carriers map[string]string
		spec     string
		want     string // "" means must not resolve
		why      string
	}{
		{
			name:     "mjs_prefers_mts_over_ts",
			carriers: map[string]string{"src/x.ts": "ts-id", "src/x.mts": "mts-id"},
			spec:     "./x.mjs",
			want:     "mts-id",
			why:      "`./x.mjs` is the Node16 emitted form of `x.mts`; `x.ts` emits `x.js`, never `x.mjs`",
		},
		{
			name:     "mjs_reaches_mts_alone",
			carriers: map[string]string{"src/x.mts": "mts-id"},
			spec:     "./x.mjs",
			want:     "mts-id",
			why:      "the .mts half of the Node16 mapping must be implemented, not just the .ts half",
		},
		{
			name:     "cjs_reaches_cts_alone",
			carriers: map[string]string{"src/y.cts": "cts-id"},
			spec:     "./y.cjs",
			want:     "cts-id",
			why:      "same for the always-CommonJS family",
		},
		{
			name:     "mjs_must_not_reach_ts",
			carriers: map[string]string{"src/x.ts": "ts-id"},
			spec:     "./x.mjs",
			want:     "",
			why:      "no edge is correct here; a wrong edge inflates exactly the bug-rate #7272 reports",
		},
		{
			name:     "cjs_must_not_reach_ts",
			carriers: map[string]string{"src/y.ts": "ts-id"},
			spec:     "./y.cjs",
			want:     "",
			why:      "`./y.cjs` can only mean `y.cts` or `y.cjs`",
		},
		{
			name:     "js_must_not_reach_mts",
			carriers: map[string]string{"src/z.mts": "mts-id"},
			spec:     "./z.js",
			want:     "",
			why:      "the reverse direction: `z.mts` emits `z.mjs`, so `./z.js` cannot name it",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			id, ok := resolveRelativeImportTarget("src/app.ts", tc.spec, tc.carriers)
			if tc.want == "" {
				if ok {
					t.Fatalf("%s with carriers %v resolved to %q — FORBIDDEN (%s)",
						tc.spec, tc.carriers, id, tc.why)
				}
				return
			}
			if !ok || id != tc.want {
				t.Fatalf("%s with carriers %v = (%q, %v), want (%q, true) (%s)",
					tc.spec, tc.carriers, id, ok, tc.want, tc.why)
			}
		})
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

	// Precedence between the two loops, with BOTH candidates present: a
	// directory literally named `foo.js` holding an `index.ts`, beside a
	// file `foo.ts`. Before #7272 the specifier `./foo.js` reached only the
	// directory index; now the file wins, which matches tsc — the
	// `.js`->`.ts` substitution is tried before directory resolution. The
	// previous row varies only the "no file sibling" axis and would stay
	// green whichever loop ran first, so it cannot state this.
	both := map[string]string{
		"src/foo.js/index.ts": "dir-id",
		"src/foo.ts":          "file-id",
	}
	if id, ok := resolveRelativeImportTarget("src/app.ts", "./foo.js", both); !ok || id != "file-id" {
		t.Errorf("./foo.js with both a `foo.js/` directory index and a `foo.ts` file = (%q, %v), "+
			"want (\"file-id\", true): extension replacement is tried before directory-index "+
			"resolution, as tsc does", id, ok)
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

// TestMtsCtsAreTreatedAsJSSources_7272 grades the OTHER two consumers of
// jsExtensions that adding ".mts"/".cts" changed. Neither had a direct test,
// so without this the widening's effect on module derivation would be
// unobserved: a `.mts` file previously derived the dotted module
// `src.x.mts` (extension not stripped) and was not recognised as a JS import
// source at all, even though internal/classifier types it as "typescript".
func TestMtsCtsAreTreatedAsJSSources_7272(t *testing.T) {
	for _, ext := range []string{".mts", ".cts"} {
		mods := modulesForJSFile("src/x" + ext)
		if containsExt(mods, "src.x"+ext) {
			t.Errorf("modulesForJSFile(%q) = %v — still carries the unstripped extension; the "+
				"dotted module of a TypeScript source must not embed its file extension",
				"src/x"+ext, mods)
		}
		if !containsExt(mods, "src.x") {
			t.Errorf("modulesForJSFile(%q) = %v, want it to contain \"src.x\"", "src/x"+ext, mods)
		}
		if !isJSImportSource("src/x" + ext) {
			t.Errorf("isJSImportSource(%q) = false — a .mts/.cts file is a TypeScript source "+
				"(internal/classifier maps both to \"typescript\") and must gate the JS "+
				"default-export fallback like any other", "src/x"+ext)
		}
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
