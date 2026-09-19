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
		{"csharp/namespace-path", "Acme/Billing/Invoice", imports, "csharp"},
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
		"Acme/Billing/Invoice":                       "Acme",
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
		{"packages/ui/src/index.ts", []string{"packages", "ui"}},
		{"app/routes/home.jsx", []string{"app", "routes"}},
		{"lib/db.ts", []string{"lib"}},
		{"index.ts", nil},
		{"", nil},
		{"./src/x/y.ts", []string{"src", "x"}},
		{"src\\win\\path.ts", []string{"src", "win"}},
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
// dependencies — "vendor/react/index.js", "third_party/lodash/util.js" —
// would register `react` / `lodash` as repo-owned roots. An
// `import "react/jsx-runtime"` would then be masked as internal, never get an
// ext: prefix, and land right back in ImportFormatOther: the exact #7274
// symptom, reintroduced by the guard that exists to prevent over-firing.
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
		{"vendor", "vendor/react/index.js", "react/jsx-runtime", "ext:react"},
		{"third_party", "third_party/lodash/util.js", "lodash/fp", "ext:lodash"},
		{"node_modules", "node_modules/better-auth/dist/index.js", "better-auth/node", "ext:better-auth"},
		{"scoped-vendored", "vendor/base-ui/react/toast.js", "@base-ui/react/toast", "ext:@base-ui/react"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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
