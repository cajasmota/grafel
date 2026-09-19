package external

import (
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// #7274 — npm specifiers with a subpath ("better-auth/node") and scoped
// specifiers whose @scope is not on the static allowlist ("@base-ui/react")
// never reached jsExternalPackageRoot: the former was killed by the
// language-agnostic path-separator reject, the latter by the scoped-npm
// allowlist gate. Both landed in bug-extractor / ImportFormatOther, the same
// bucket a genuinely malformed reference falls into.
//
// The table is the full cross-product of
//
//	(scoped | unscoped) × (subpath | none) × (allowlisted | not)
//
// so a fix cannot be graded on hand-picked rows. `@base-ui/react` (scoped,
// NO subpath, uncatalogued scope) is the discriminating row: it separates
// "subpaths are broken" from "the allowlist is the sole gate" — a
// subpath-only fix leaves it failing.
func TestClassifyExternal_NpmSpecifierCrossProduct_7274(t *testing.T) {
	const imports = string(types.RelationshipKindImports)

	cases := []struct {
		name    string
		spec    string
		scoped  bool
		subpath bool
		allowed bool
		want    string
	}{
		{"unscoped/none/allowlisted", "lodash", false, false, true, "lodash"},
		{"unscoped/subpath/allowlisted", "lodash/fp", false, true, true, "lodash"},
		{"unscoped/none/uncatalogued", "better-auth", false, false, false, "better-auth"},
		{"unscoped/subpath/uncatalogued", "better-auth/node", false, true, false, "better-auth"},
		{"unscoped/deep-subpath/uncatalogued", "better-auth/adapters/prisma", false, true, false, "better-auth"},
		{"scoped/none/allowlisted", "@prisma/client", true, false, true, "@prisma/client"},
		{"scoped/subpath/allowlisted", "@prisma/client/edge", true, true, true, "@prisma/client"},
		{"scoped/none/uncatalogued", "@base-ui/react", true, false, false, "@base-ui/react"},
		{"scoped/subpath/uncatalogued", "@base-ui/react/toast", true, true, false, "@base-ui/react"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, lang := range []string{"javascript", "typescript"} {
				got, subtype, ok := classifyExternal(tc.spec, imports, lang, "src/app.ts", nil, nil, internalRoots{})
				if !ok {
					t.Fatalf("lang=%s spec=%q: classifyExternal returned ok=false (want external %q)", lang, tc.spec, tc.want)
				}
				if got != tc.want {
					t.Fatalf("lang=%s spec=%q: canonical = %q, want %q", lang, tc.spec, got, tc.want)
				}
				if subtype != "package" {
					t.Fatalf("lang=%s spec=%q: subtype = %q, want %q", lang, tc.spec, subtype, "package")
				}
			}
		})
	}
}

// #7274 forbidden rows. The path-separator reject at the heart of this fix is
// NOT JS-specific — it governs every language reaching classifyExternal.
// Each row below asserts a shape that must NOT newly classify as an npm
// package. Every row is slash-bearing and reaches the reject (rather than
// being disposed of earlier for an unrelated reason); see
// TestClassifyExternal_NpmForbiddenRowsAreReachable_7274.
func TestClassifyExternal_NpmForbiddenRows_7274(t *testing.T) {
	const imports = string(types.RelationshipKindImports)
	const calls = string(types.RelationshipKindCalls)

	cases := []struct {
		name    string
		spec    string
		relKind string
		lang    string
	}{
		// Language gate — the very specifier the fix rescues for JS/TS must
		// stay rejected for every other ecosystem.
		{"go/unresolved-repo-relative", "better-auth/node", imports, "go"},
		{"go/internal-package-path", "internal/util/helper", imports, "go"},
		{"java/slash-shaped-fqn", "org/springframework/boot/SpringApplication", imports, "java"},
		{"python/dotted-with-path", "shop/order/views", imports, "python"},
		{"ruby/require-path", "billing/charge", imports, "ruby"},
		{"csharp/namespace-path", "contoso/app/svc", imports, "csharp"},
		// Relationship-kind gate — a bare CALLS/REFERENCES operand may still
		// be a local symbol, so the catch-all must not swallow it.
		{"js/calls-not-imports", "better-auth/node", calls, "javascript"},
		{"ts/calls-scoped", "@base-ui/react/toast", calls, "typescript"},
		// Project-internal file shapes.
		{"js/relative-subpath", "./utils/helper", imports, "javascript"},
		{"js/parent-relative", "../../lib/db", imports, "typescript"},
		{"js/absolute-path", "/usr/local/lib/node_modules/x", imports, "javascript"},
		{"js/windows-path", `C:\Users\dev\app\x`, imports, "typescript"},
		{"js/url-specifier", "https://esm.sh/lodash", imports, "javascript"},
		{"js/bare-at-alias", "@/components/Button", imports, "typescript"},
		{"js/malformed-scope", "@/", imports, "javascript"},
		{"js/whitespace-residue", "some pkg/sub", imports, "javascript"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _, ok := classifyExternal(tc.spec, tc.relKind, tc.lang, "src/app.ts", nil, nil, internalRoots{})
			if ok {
				t.Fatalf("lang=%s kind=%s spec=%q: classified as external %q — must be rejected", tc.lang, tc.relKind, tc.spec, got)
			}
		})
	}
}

// #7274 reachability / positive control for the forbidden rows above. A
// forbidden row that returns the right answer via the wrong mechanism grades
// nothing. Each non-JS / non-IMPORTS row below is re-run with the gate
// satisfied (lang=javascript, relKind=IMPORTS): it MUST then classify as an
// npm package. That proves the language gate and the relationship-kind gate
// are the only things holding the row back — the row is a live violation that
// would fire if either gate were widened, not a shape disposed of earlier for
// an unrelated reason.
func TestClassifyExternal_NpmForbiddenRowsAreReachable_7274(t *testing.T) {
	const imports = string(types.RelationshipKindImports)

	// spec → the root it WOULD classify to with the gate satisfied.
	planted := map[string]string{
		"better-auth/node":                           "better-auth",
		"internal/util/helper":                       "internal",
		"org/springframework/boot/SpringApplication": "org",
		"shop/order/views":                           "shop",
		"billing/charge":                             "billing",
		"contoso/app/svc":                            "contoso",
		"@base-ui/react/toast":                       "@base-ui/react",
	}
	for spec, want := range planted {
		t.Run(spec, func(t *testing.T) {
			got, _, ok := classifyExternal(spec, imports, "javascript", "src/app.ts", nil, nil, internalRoots{})
			if !ok || got != want {
				t.Fatalf("planted violation %q did not fire: got (%q, ok=%v), want (%q, ok=true) — "+
					"the matching forbidden row is being rejected by some earlier mechanism and grades nothing", spec, got, ok, want)
			}
		})
	}

	// The remaining forbidden rows are slash-bearing and DO enter the new
	// branch (lang + relKind + separator all satisfied); they are rejected
	// inside jsExternalPackageRoot itself. Pin that, so a widening of the
	// helper's own rejects is caught here rather than silently.
	for _, spec := range []string{
		"./utils/helper", "../../lib/db", "/usr/local/lib/node_modules/x",
		`C:\Users\dev\app\x`, "@/components/Button", "some pkg/sub",
	} {
		t.Run("helper-reject/"+spec, func(t *testing.T) {
			if !strings.ContainsAny(spec, `/\`) {
				t.Fatalf("%q carries no path separator, so it never reaches the #7274 branch", spec)
			}
			if root, ok := jsExternalPackageRoot(spec, nil); ok {
				t.Fatalf("jsExternalPackageRoot(%q) = (%q, true), want reject", spec, root)
			}
		})
	}
}

// #7274 — the internal-root guard. An unresolved non-relative import whose
// root is a directory this repo owns is an alias/baseUrl resolution failure
// (a fidelity bug), not an npm dependency. Without the guard the slash-bearing
// catch-all would mask it as ext:components.
func TestClassifyExternal_NpmInternalRootGuard_7274(t *testing.T) {
	const imports = string(types.RelationshipKindImports)
	owned := internalRoots{js: map[string]bool{"components": true}}

	// Positive control: with no owned roots the specifier DOES classify, so
	// the guard below is the operative mechanism and not a coincidence.
	if got, _, ok := classifyExternal("components/Button", imports, "typescript", "src/app.ts", nil, nil, internalRoots{}); !ok || got != "components" {
		t.Fatalf("control: classifyExternal(components/Button) = (%q, ok=%v), want (\"components\", true)", got, ok)
	}
	if got, _, ok := classifyExternal("components/Button", imports, "typescript", "src/app.ts", nil, nil, owned); ok {
		t.Fatalf("repo-owned root masked as external %q — must stay an unresolved fidelity bug", got)
	}
	// A real npm package must be unaffected by an unrelated owned root.
	if got, _, ok := classifyExternal("better-auth/node", imports, "typescript", "src/app.ts", nil, nil, owned); !ok || got != "better-auth" {
		t.Fatalf("classifyExternal(better-auth/node) = (%q, ok=%v), want (\"better-auth\", true)", got, ok)
	}
}

// #7274 — jsFileRoots feeds the internal-root set; a root it fails to record
// is a hole in the guard above.
func TestJSFileRoots_7274(t *testing.T) {
	cases := []struct {
		path string
		want []string
	}{
		{"src/components/Button.tsx", []string{"src", "components"}},
		{"components/Button.tsx", []string{"components"}},
		{"packages/ui/src/index.ts", []string{"packages", "ui", "src"}},
		// The "never the trailing filename" invariant was asserted only for
		// the non-monorepo arm; the extra monorepo hop could run off the end.
		{"packages/index.ts", []string{"packages"}},
		// The extra hop belongs to packages/ and apps/ ONLY. Applied to every
		// wrapper it walks one level too deep and registers a nested source
		// directory ("ui") as an importable root.
		{"src/components/ui/Button.tsx", []string{"src", "components"}},
		{"lib/components/ui/x.ts", []string{"lib", "components"}},
		{"apps/main.tsx", []string{"apps"}},
		{"app/routes/home.jsx", []string{"app", "routes"}},
		{"lib/db.ts", []string{"lib"}},
		{"index.ts", nil},
		{"", nil},
		{"./src/x/y.ts", []string{"src", "x"}},
		{"src\\win\\path.ts", []string{"src", "win"}},
		// Case-folded on insert: the lookup key is lower-cased, and the
		// named-import route applies no lower-case gate of its own, so an
		// unfolded "Components" root would never match a "components" lookup.
		{"src/Components/Button.tsx", []string{"src", "components"}},
		{"Src/Components/Button.tsx", []string{"src", "components"}},
		{"Packages/UI/Src/index.ts", []string{"packages", "ui", "src"}},
	}
	for _, tc := range cases {
		got := jsFileRoots(tc.path)
		if len(got) != len(tc.want) {
			t.Fatalf("jsFileRoots(%q) = %v, want %v", tc.path, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("jsFileRoots(%q) = %v, want %v", tc.path, got, tc.want)
			}
		}
	}
}

// #7274 — the OVER-masking direction of the internal-root guard, which the
// first round left ungraded (a mutant widening jsFileRoots' wrapper-dir list
// to every directory survived the whole package suite).
//
// jsFileRoots only promotes the SECOND path segment to an internal root when
// the first is a recognised source-root wrapper (src/lib/app/packages/apps).
// If it promoted the second segment unconditionally, a repo vendoring its
// dependencies would register those package names as repo-owned roots, the
// import would be masked as internal, never get an ext: prefix, and land
// right back in ImportFormatOther: the exact #7274 symptom, reintroduced by
// the guard that exists to prevent over-firing.
//
// EVERY ROOT HERE MUST BE UNCATALOGUED, and the loop asserts it. An
// allowlisted root (the original "vendor/react", "third_party/lodash") is
// classified by the earlier isKnownExternalPackage branch, which never
// consults the internal-root set — so such a row passes whatever the root
// set contains and grades nothing. Three of the four original rows were
// vacuous this way; the self-check keeps a future edit from reintroducing
// one by picking a name that happens to be on the list.
//
// The original "scoped-vendored" row was vacuous by CONSTRUCTION and is
// deleted rather than repaired: jsFileRoots only ever emits directory names,
// so internal.js can never hold a key of the form "@scope/pkg", and no
// mutation of the root set can make a scoped row fire.
//
// Asserted through the production path (Synthesize), not jsFileRoots alone,
// so the wiring is graded too.
func TestSynthesize_VendoredDirDoesNotBecomeInternalRoot_7274(t *testing.T) {
	cases := []struct {
		name       string
		vendorFile string
		spec       string
		wantToID   string
	}{
		{"vendor", "vendor/better-auth/index.js", "better-auth/node", "ext:better-auth"},
		{"third_party", "third_party/zzcorp-ui/util.js", "zzcorp-ui/button", "ext:zzcorp-ui"},
		{"node_modules", "node_modules/tiny-invariant-x/dist/i.js", "tiny-invariant-x/esm", "ext:tiny-invariant-x"},
		{"deep-vendor", "vendor/js/better-auth/index.js", "better-auth/node", "ext:better-auth"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Self-check: an allowlisted root is classified before the
			// internal-root guard is ever consulted, so a row built on one
			// passes regardless of the root set and grades nothing.
			root := strings.SplitN(tc.spec, "/", 2)[0]
			if isKnownExternalPackage(root) {
				t.Fatalf("row %q uses the allowlisted root %q — it would pass whatever internal.js contains; pick an uncatalogued package", tc.name, root)
			}
			doc := &graph.Document{
				Entities: []graph.Entity{
					{ID: "aaaaaaaaaaaaaaaa", Name: "app", Kind: "SCOPE.Component", SourceFile: "src/app.ts", Language: "typescript"},
					// The vendored copy is itself an indexed JS/TS source — this
					// is what feeds jsFileRoots and can poison the root set.
					{ID: "bbbbbbbbbbbbbbbb", Name: "vendored", Kind: "SCOPE.Component", SourceFile: tc.vendorFile, Language: "javascript"},
				},
				Relationships: []graph.Relationship{
					{ID: "rel-1", FromID: "aaaaaaaaaaaaaaaa", ToID: tc.spec, Kind: "IMPORTS"},
				},
			}
			Synthesize(doc)
			if got := doc.Relationships[0].ToID; got != tc.wantToID {
				t.Fatalf("vendored source %q poisoned the internal-root set: import %q resolved to %q, want %q "+
					"(a masked import gets no ext: prefix and is counted as an extraction bug — the #7274 symptom)",
					tc.vendorFile, tc.spec, got, tc.wantToID)
			}
		})
	}

	// Positive control: the guard is NOT inert. A source file directly under a
	// recognised wrapper DOES own its second segment, so the same import shape
	// is correctly withheld from classification.
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Name: "app", Kind: "SCOPE.Component", SourceFile: "src/app.ts", Language: "typescript"},
			{ID: "bbbbbbbbbbbbbbbb", Name: "owned", Kind: "SCOPE.Component", SourceFile: "src/components/Button.tsx", Language: "typescript"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "aaaaaaaaaaaaaaaa", ToID: "components/Button", Kind: "IMPORTS"},
		},
	}
	Synthesize(doc)
	if got := doc.Relationships[0].ToID; got != "components/Button" {
		t.Fatalf("control: repo-owned %q was rewritten to %q — the wrapper list is inert, so the forbidden rows above grade nothing", "components/Button", got)
	}
}

// #7274 — jsFileRoots must NOT promote the second segment under a directory
// that is not a source-root wrapper. Unit-level twin of the production-path
// rows above; kills a widening that the cross-product table cannot see.
func TestJSFileRoots_NonWrapperDirsDoNotPromote_7274(t *testing.T) {
	for _, tc := range []struct {
		path string
		want []string
	}{
		{"vendor/react/index.js", []string{"vendor"}},
		{"third_party/lodash/util.js", []string{"third_party"}},
		{"node_modules/better-auth/dist/index.js", []string{"node_modules"}},
		{"dist/react/index.js", []string{"dist"}},
		{"test/fixtures/x.ts", []string{"test"}},
	} {
		got := jsFileRoots(tc.path)
		if len(got) != len(tc.want) || got[0] != tc.want[0] {
			t.Fatalf("jsFileRoots(%q) = %v, want %v — a non-wrapper directory must not promote its child to an internal root", tc.path, got, tc.want)
		}
	}
}

// #7274 — the wrapper list is a DECISION, not an accident. Pinned exactly,
// the way canonical name sets are pinned elsewhere in this tree.
//
// Consequence of each direction, so a future editor can weigh a change:
//   - ADDING a directory makes every child of it a repo-owned root, so real
//     npm imports of that name stop being classified as external and are
//     counted as extraction bugs (the #7274 symptom). Only add a directory
//     that genuinely holds FIRST-PARTY source.
//   - REMOVING one re-opens the over-firing hole the guard exists to close:
//     an unresolved "components/Button" alias import in a repo whose sources
//     live under that directory gets masked as ext:components.
//
// Update this pin and the rationale together, never the map alone.
func TestJSSourceWrapperDirs_PinnedContents_7274(t *testing.T) {
	want := []string{"app", "apps", "lib", "packages", "src"}
	got := make([]string, 0, len(jsSourceWrapperDirs))
	for d := range jsSourceWrapperDirs {
		got = append(got, d)
	}
	sort.Strings(got)
	if len(got) != len(want) {
		t.Fatalf("jsSourceWrapperDirs = %v, want exactly %v — see this test's doc comment for what each direction costs", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("jsSourceWrapperDirs = %v, want exactly %v — see this test's doc comment for what each direction costs", got, want)
		}
	}
}

// #7274 B1 — the internal-root guard must hold across repo layouts, not just
// flat src/. Review found it covered exactly ONE layout: jsFileRoots promoted
// a single level under a wrapper, and the wrapper lookup was exact-match, so
// monorepo, nested and capitalised layouts all lost the guard and fabricated
// an npm placeholder for an unresolved alias import.
//
// Driven through Synthesize. Every row owns a components/ directory in some
// layout and imports "components/Button": the edge must be LEFT UNRESOLVED
// (a fidelity bug is the correct disposition), never rewritten to
// ext:components.
func TestSynthesize_InternalRootGuardAcrossLayouts_7274(t *testing.T) {
	for _, tc := range []struct{ name, owned string }{
		{"flat-src", "src/components/Button.tsx"},
		{"flat-noprefix", "components/Button.tsx"},
		{"capital-Src", "Src/components/Button.tsx"},
		{"capital-App", "App/components/Button.tsx"},
		{"capital-Lib", "Lib/components/Button.tsx"},
		{"monorepo-packages", "packages/ui/src/components/Button.tsx"},
		{"monorepo-apps", "apps/web/src/components/Button.tsx"},
		{"nested-lib", "lib/components/Button.tsx"},
		// Population: the directory holds no .ts/.js at all.
		{"vue-only-dir", "src/components/Button.vue"},
		{"svelte-only-dir", "src/components/Button.svelte"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := &graph.Document{
				Entities: []graph.Entity{
					{ID: "aaaaaaaaaaaaaaaa", Name: "app", Kind: "SCOPE.Component", SourceFile: "src/app.ts", Language: "typescript"},
					{ID: "bbbbbbbbbbbbbbbb", Name: "Button", Kind: "SCOPE.Component", SourceFile: tc.owned, Language: "typescript"},
				},
				Relationships: []graph.Relationship{
					{ID: "rel-1", FromID: "aaaaaaaaaaaaaaaa", ToID: "components/Button", Kind: "IMPORTS"},
				},
			}
			Synthesize(doc)
			if got := doc.Relationships[0].ToID; got != "components/Button" {
				t.Fatalf("layout %q owns %q yet the alias import was masked as %q — "+
					"an unresolved alias must stay a fidelity bug, not become an npm placeholder",
					tc.name, tc.owned, got)
			}
		})
	}
}

// #7274 B1 residual — the ONE layout the guard still cannot cover, pinned so
// it is a recorded decision rather than a surprise. When the repo indexes no
// file under the aliased directory at all, nothing distinguishes an
// unresolved first-party alias from a real npm subpath import, and the
// branch classifies it. Cost: a baseUrl alias into a directory grafel does
// not index is reported as an external package.
func TestSynthesize_InternalRootGuardResidualGap_7274(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Name: "app", Kind: "SCOPE.Component", SourceFile: "src/app.ts", Language: "typescript"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "aaaaaaaaaaaaaaaa", ToID: "components/Button", Kind: "IMPORTS"},
		},
	}
	Synthesize(doc)
	if got := doc.Relationships[0].ToID; got != "ext:components" {
		t.Fatalf("recorded residual gap changed: got %q, want %q. If this is now guarded, "+
			"delete this test and say so; if it changed some other way, the guard's population rule moved", got, "ext:components")
	}
}

// #7274 B2 — malformed references must KEEP counting as extraction bugs.
// #7274 is about working dependencies being mis-counted as bugs; letting a
// malformed reference escape ImportFormatOther is the same error with the
// sign flipped, and worse, because it hides a real defect instead of
// inflating a number.
//
// The rows do NOT all grade the same mechanism, and saying so matters: with
// isLegalNpmImportSpecifier deleted, only some of them classify. The rest are
// rejected by pre-existing jsExternalPackageRoot / isNpmSegment machinery, or
// (for "pkg:x/sub") by the branch reading the raw stub instead of the
// kind-prefix-stripped name. Which row grades which mechanism is asserted by
// TestClassifyExternal_MalformedRowMechanisms_7274 rather than claimed here,
// so the table cannot quietly become a set of rows that all pass for reasons
// this change does not own.
func TestClassifyExternal_MalformedNpmSpecifiersStayBugs_7274(t *testing.T) {
	const imports = string(types.RelationshipKindImports)
	for _, spec := range []string{
		// npm forbids a leading dot.
		".hidden/x",
		"..x/y",
		".npmrc/x",
		// Leading '_' and '-' (see isLegalNpmImportSpecifier for the '-' call).
		"_private/x",
		"-bad/pkg",
		// Empty segments and trailing separators.
		"a//b",
		"pkg/",
		"pkg//",
		"/",
		"//pkg",
		// Mixed case — npm has refused new mixed-case names for years.
		"UPPER/Case",
		"com.example.Foo/Bar",
		"MyPkg/sub",
		// SCOPED — the alphabet #7274's second defect lives in, and the half
		// the first cut of this table left entirely open. Deleting the scope
		// validation (validating only the package segment) left the suite
		// green while "@UPPER/pkg/x" and "@_bad/pkg/x" were handed
		// placeholders.
		"@UPPER/pkg/x",
		"@_bad/pkg/x",
		"@.bad/pkg/x",
		"@a/UPPER/x",
		"@-bad/pkg/x",
		"@a/_bad/x",
		// Dotted root that is a module host, not a package.
		"github.com/x",
		// Length — npm caps a name at 214. Reachable: nothing upstream caps
		// specifier length.
		strings.Repeat("a", 215) + "/sub",
		// Non-URL-safe characters.
		"pk g/sub",
		"pkg!/sub",
		"pkg~/sub",
		"pkg%20x/sub",
		"pkg:x/sub",
	} {
		t.Run(spec, func(t *testing.T) {
			for _, lang := range []string{"javascript", "typescript"} {
				if got, _, ok := classifyExternal(spec, imports, lang, "src/app.ts", nil, nil, internalRoots{}); ok {
					t.Fatalf("lang=%s malformed specifier %q escaped the audit bucket as %q — "+
						"it must keep counting as an extraction bug", lang, spec, got)
				}
			}
		})
	}

	// Positive control: the legality gate is not swallowing everything. Real
	// package shapes that differ from the rows above by exactly one property
	// must still classify.
	for _, tc := range []struct{ spec, want string }{
		{"better-auth/node", "better-auth"},
		{"my-pkg/sub", "my-pkg"},
		{"pkg_x/sub", "pkg_x"},
		{"pkg2/sub", "pkg2"},
		{"@base-ui/react/toast", "@base-ui/react"},
		{"@scope-x/pkg_y", "@scope-x/pkg_y"},
		// The 214 boundary is a boundary, not a blanket reject.
		{strings.Repeat("a", 214) + "/sub", strings.Repeat("a", 214)},
		{strings.Repeat("a", 213) + "/sub", strings.Repeat("a", 213)},
		// The dotted-root rule's ESCAPE clause. It must be exercised with a
		// SCOPED root: an unscoped dotted allowlisted root ("lodash.debounce/x")
		// is claimed by the earlier isGoImportPath + allowlist branch and never
		// reaches the legality gate, so a row built on one grades nothing —
		// which is exactly how the first attempt at this row was vacuous.
		// "@bam.tech" is an allowlisted scope containing a dot, and a leading
		// '@' makes isGoImportPath decline, so this row does reach the gate.
		{"@bam.tech/react-native-image-resizer/x", "@bam.tech/react-native-image-resizer"},
		// Kept as a regression row for the upstream branch, explicitly NOT as
		// a control for this gate.
		{"lodash.debounce/x", "lodash.debounce"},
	} {
		if got, _, ok := classifyExternal(tc.spec, imports, "typescript", "src/app.ts", nil, nil, internalRoots{}); !ok || got != tc.want {
			t.Fatalf("control: %q = (%q, ok=%v), want (%q, true) — the legality gate is over-rejecting", tc.spec, got, ok, tc.want)
		}
	}
}

// #7274 F1 — the branch must gate on the SAME string it classifies. The
// separator gate used to read the stub while jsExternalPackageRoot classified
// Properties["import_path"], so a slash-free stub could smuggle an unrelated
// slash-bearing import_path past the gate and change an answer the branch
// claimed it could not reach.
func TestClassifyExternal_GatesOnTheClassifiedSpecifier_7274(t *testing.T) {
	const imports = string(types.RelationshipKindImports)

	// Slash-free stub, slash-bearing import_path: the branch must classify
	// the import_path (the string it reads), not the stub.
	props := map[string]string{"import_path": "better-auth/node"}
	if got, _, ok := classifyExternal("better-auth", imports, "typescript", "src/app.ts", nil, props, internalRoots{}); !ok || got != "better-auth" {
		t.Fatalf("stub+import_path disagreement: got (%q, ok=%v), want (\"better-auth\", true)", got, ok)
	}

	// The invariant itself: a specifier the branch reads must never be
	// classified from a DIFFERENT string. A scoped stub with an unrelated
	// import_path must resolve from import_path consistently in both the
	// gate and the classifier — never return the stub's own root while
	// having been admitted on the other string.
	props = map[string]string{"import_path": "lodash/fp"}
	got, _, ok := classifyExternal("@prisma/client", imports, "typescript", "src/app.ts", nil, props, internalRoots{})
	if !ok || got != "lodash" {
		t.Fatalf("gate and classifier read different strings: got (%q, ok=%v), want (\"lodash\", true) — "+
			"the branch must gate on jsSpecifierFor, the same value jsExternalPackageRoot classifies", got, ok)
	}
}

// #7274 F2 — the named-import route must honour the internal-root set too.
// It was the only language arm that dropped its guard, so the same root was
// guarded on the slash-bearing spelling and unguarded on the bare one inside
// a single Synthesize call.
func TestBuildNamedImportIndex_HonoursInternalJSRoots_7274(t *testing.T) {
	if _, ok := importEdgePackageRoot("components/Button", "typescript", nil,
		internalRoots{js: map[string]bool{"components": true}}); ok {
		t.Fatal("importEdgePackageRoot ignored internal.js — a repo-owned root must not mint ext:components:<Symbol>")
	}
	// Control: without the owned root it DOES classify, so the assertion
	// above is graded by the guard and not by an unrelated reject.
	if root, ok := importEdgePackageRoot("components/Button", "typescript", nil, internalRoots{}); !ok || root != "components" {
		t.Fatalf("control: importEdgePackageRoot = (%q, ok=%v), want (\"components\", true)", root, ok)
	}
	// The case-folded spelling must be guarded too (#7274 B1 case finding).
	if _, ok := importEdgePackageRoot("Components/Button", "typescript", nil,
		internalRoots{js: map[string]bool{"components": true}}); ok {
		t.Fatal("importEdgePackageRoot: case-folded internal root not honoured")
	}
}

// #7274 F2 (production path) — the internal-root set must actually REACH the
// named-import route. Scoring the helper alone left the PLUMBING ungraded:
// setting `js: nil` at the buildNamedImportIndex call site survived, which is
// the definition of dead plumbing that looks like a guard.
//
// Fixture: app.ts named-imports { Button } from the repo-owned "components"
// root and then references `Button` bare. Without the guard the reference is
// minted as ext:components:Button — a per-symbol placeholder for a first-party
// component.
func TestSynthesize_NamedImportRouteHonoursInternalJSRoots_7274(t *testing.T) {
	newDoc := func(owned string) *graph.Document {
		return &graph.Document{
			Entities: []graph.Entity{
				{ID: "aaaaaaaaaaaaaaaa", Name: "app", Kind: "SCOPE.Component", SourceFile: "src/app.ts", Language: "typescript"},
				{ID: "bbbbbbbbbbbbbbbb", Name: "Button", Kind: "SCOPE.Component", SourceFile: owned, Language: "typescript"},
			},
			Relationships: []graph.Relationship{
				graph.Relationship{ID: "rel-imp", FromID: "aaaaaaaaaaaaaaaa", ToID: "components/Button", Kind: "IMPORTS"}.
					WithProperties(map[string]string{"local_name": "Button", "imported_name": "Button"}),
				{ID: "rel-ref", FromID: "aaaaaaaaaaaaaaaa", ToID: "Button", Kind: "CALLS"},
			},
		}
	}

	doc := newDoc("src/components/Button.tsx")
	Synthesize(doc)
	for _, r := range doc.Relationships {
		if r.ID == "rel-ref" && strings.HasPrefix(r.ToID, "ext:components") {
			t.Fatalf("named-import route minted %q for a repo-owned root — internal.js is not reaching "+
				"buildNamedImportIndex, so the guard holds on the slash-bearing spelling and not the bare one", r.ToID)
		}
	}

	// Control: a genuine npm named import DOES still get its per-symbol node,
	// so the assertion above is graded by the guard and not by an inert route.
	ctl := &graph.Document{
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Name: "app", Kind: "SCOPE.Component", SourceFile: "src/app.ts", Language: "typescript"},
		},
		Relationships: []graph.Relationship{
			graph.Relationship{ID: "rel-imp", FromID: "aaaaaaaaaaaaaaaa", ToID: "better-auth/node", Kind: "IMPORTS"}.
				WithProperties(map[string]string{"local_name": "Session", "imported_name": "Session"}),
			{ID: "rel-ref", FromID: "aaaaaaaaaaaaaaaa", ToID: "Session", Kind: "CALLS"},
		},
	}
	Synthesize(ctl)
	hit := false
	for _, r := range ctl.Relationships {
		if r.ID == "rel-ref" && r.ToID == "ext:better-auth:Session" {
			hit = true
		}
	}
	if !hit {
		t.Fatal("control: a real npm named import did not produce ext:better-auth:Session — the named-import route is inert, so the row above grades nothing")
	}
}

// #7274 — which mechanism actually rejects each malformed row. The first cut
// of the malformed table claimed in prose that every row "classified as an
// ext: placeholder before the isLegalNpmImportSpecifier gate". That was false
// for 7 of 19 rows, and it named the wrong mechanism for "pkg:x/sub". A
// forbidden table whose rows are saved by machinery the change does not own
// is not vacuous, but it grades less than it appears to — so the split is
// asserted here instead of asserted in a comment.
//
// legalityGated  — jsExternalPackageRoot ACCEPTS; only the new legality gate
//
//	rejects. These are the rows that grade this change.
//
// helperGated    — jsExternalPackageRoot itself rejects (pre-existing
//
//	isNpmSegment / relative-path machinery). Kept as
//	regression rows, but they do not grade the legality gate.
func TestClassifyExternal_MalformedRowMechanisms_7274(t *testing.T) {
	legalityGated := []string{
		".hidden/x", "..x/y", ".npmrc/x", "_private/x", "-bad/pkg",
		"a//b", "pkg/", "pkg//", "UPPER/Case", "com.example.Foo/Bar",
		"MyPkg/sub", "github.com/x",
		"@UPPER/pkg/x", "@_bad/pkg/x", "@.bad/pkg/x", "@a/UPPER/x",
		"@-bad/pkg/x", "@a/_bad/x",
		strings.Repeat("a", 215) + "/sub",
	}
	for _, spec := range legalityGated {
		t.Run("legality/"+spec, func(t *testing.T) {
			root, ok := jsExternalPackageRoot(spec, nil)
			if !ok {
				t.Fatalf("jsExternalPackageRoot(%q) already rejects — this row does NOT grade the legality gate; move it to helperGated", spec)
			}
			if isLegalNpmImportSpecifier(spec, root) {
				t.Fatalf("isLegalNpmImportSpecifier(%q, %q) = true — the row would classify", spec, root)
			}
		})
	}

	// The dotted-root rule has an ESCAPE clause, and it is asserted directly
	// rather than through classifyExternal: an end-to-end row for an UNSCOPED
	// dotted root is shadowed by the earlier isGoImportPath branch and cannot
	// observe this gate at all.
	if !isLegalNpmImportSpecifier("@bam.tech/pkg/x", "@bam.tech/pkg") {
		t.Fatal("allowlisted dotted SCOPED root rejected — the escape clause in the dotted rule is gone, and with it the recall its documented departure claims")
	}
	if isLegalNpmImportSpecifier("@nope.invalid/pkg/x", "@nope.invalid/pkg") {
		t.Fatal("uncatalogued dotted scoped root accepted — the dotted rule no longer rejects module-host shapes")
	}

	helperGated := []string{"/", "//pkg", "pk g/sub", "pkg!/sub", "pkg~/sub", "pkg%20x/sub"}
	for _, spec := range helperGated {
		t.Run("helper/"+spec, func(t *testing.T) {
			if root, ok := jsExternalPackageRoot(spec, nil); ok {
				t.Fatalf("jsExternalPackageRoot(%q) = (%q, true) — this row now grades the legality gate; move it to legalityGated", spec, root)
			}
		})
	}

	// "pkg:x/sub" is its own mechanism: the branch reads the RAW stub, so the
	// kind-prefix strip never gets to turn it into the legal-looking "x/sub".
	// Pin both halves — the stub form must reject AND the stripped form must
	// be acceptable — or the reason this row passes is unobserved.
	if root, ok := jsExternalPackageRoot("pkg:x/sub", nil); ok {
		t.Fatalf("jsExternalPackageRoot(\"pkg:x/sub\") = (%q, true), want reject", root)
	}
	root, ok := jsExternalPackageRoot("x/sub", nil)
	if !ok || !isLegalNpmImportSpecifier("x/sub", root) {
		t.Fatalf("the kind-stripped form %q no longer classifies (root=%q ok=%v) — "+
			"the raw-stub gate is what saves \"pkg:x/sub\", and this pin no longer demonstrates it", "x/sub", root, ok)
	}
}

// #7274 — the two classification routes must agree about the internal-root
// guard. The F2 fix introduced a divergence in the opposite direction to the
// one it repaired: classifyExternal classifies an ALLOWLISTED root through an
// earlier branch that never consults internal.js, while importEdgePackageRoot
// applied the guard to every root. In a repo owning src/react/,
// `import "react/jsx-runtime"` yielded ext:react while
// `import { useState } from "react"` silently lost its per-symbol node.
//
// Precedence is now identical on both routes: allowlisted root wins over a
// same-named repo directory; uncatalogued root is guarded.
func TestRoutesAgreeOnInternalRootPrecedence_7274(t *testing.T) {
	const imports = string(types.RelationshipKindImports)

	for _, tc := range []struct {
		name        string
		owned       string
		slashSpec   string
		bareSpec    string
		wantAllowed bool // true => both routes classify despite the owned root
	}{
		{"allowlisted-root-wins", "react", "react/jsx-runtime", "react", true},
		{"allowlisted-root-wins-lodash", "lodash", "lodash/fp", "lodash", true},
		{"uncatalogued-root-guarded", "components", "components/Button", "components", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			roots := internalRoots{js: map[string]bool{tc.owned: true}}
			_, _, slashOK := classifyExternal(tc.slashSpec, imports, "typescript", "src/app.ts", nil, nil, roots)
			_, bareOK := importEdgePackageRoot(tc.bareSpec, "typescript", nil, roots)
			if slashOK != bareOK {
				t.Fatalf("routes disagree for owned root %q: classifyExternal(%q) ok=%v, importEdgePackageRoot(%q) ok=%v — "+
					"the same root must not be guarded on one spelling and unguarded on the other within a single Synthesize call",
					tc.owned, tc.slashSpec, slashOK, tc.bareSpec, bareOK)
			}
			if slashOK != tc.wantAllowed {
				t.Fatalf("owned root %q: ok=%v, want %v", tc.owned, slashOK, tc.wantAllowed)
			}
		})
	}
}
