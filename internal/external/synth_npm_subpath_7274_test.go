package external

import (
	"strings"
	"testing"

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
