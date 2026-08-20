// Package extractors — file_anchored_rels_guard_test.go
//
// Issue #6298. A guard against the shape @arthurgeron found in Solidity
// (#6295 / #6297) and named in verilog, re-derived from source on every run so
// it catches the NEXT extractor, not just the ones already fixed.
//
// THE SHAPE. An extractor emits a relationship record with FromID set to the
// source file's path. That value is non-empty and non-hex, so
// ReferencesEmbeddedWithAllowlist (internal/resolve/refs.go) rewrites it, and
// both graph-assembly paths — cmd/grafel/index.go's record loop and
// relRecordToGraphRel in internal/extractors/incremental.go — substitute the
// owning record's own entity id ONLY when FromID is EMPTY. So a path-valued
// FromID on a record that is NOT file-scoped goes one of two ways:
//
//   - some entity in the same extraction carries that exact path as its Name /
//     QualifiedName (an extractor.FileEntity does, and so does any hand-rolled
//     synthetic container named after the path), and the rewrite lands on THAT
//     node. The edge leaves the file rather than the type that declares it, and
//     several types in one file merge their edges onto that one node. This is
//     Solidity's and verilog's failure. MISANCHORED.
//   - nothing carries that id, so the raw path reaches the graph verbatim. This
//     was astro's failure (#6298) and svelte's (#6366, now fixed). DANGLING —
//     worse, because there is no node at the other end at all.
//
// THE RESOLUTION RULE, PRECISELY. internal/resolve/refs.go has no path→entity
// index. A path-valued FromID resolves if and only if some emitted node carries
// that exact string as Name / QualifiedName. looksLikeSourceFilePath
// (refs.go:2729) does NOT resolve anything — it routes the reference to
// DispositionDynamic, which suppresses the bug-extractor metric while leaving
// the edge dangling. Anything whose extension is missing from
// sourceFileExtensions (refs.go:2752-2767 — `.bicep` and `Dockerfile` are)
// misses even that and counts as DispositionBugExtractor. "The owning record is
// conceptually file-scoped" is therefore NOT the criterion; "a node carrying
// that exact path string exists" is. The first round of this allow-list used the
// conceptual criterion and, on the strength of it, blessed six entries across
// five languages that are real defects — they are re-labelled KNOWN OFFENDER
// below rather than fixed, because each needs its own measurement. #6367's hcl
// arm has since measured and fixed two of the six.
//
// WHY A SOURCE SCAN AND NOT A RUNTIME ONE. The runtime form of this check —
// drive every registered extractor and inspect the emitted records — needs a
// syntactically valid sample per language, and there is no such corpus in the
// tree (testdata/fixtures covers 27 languages; astro, verilog and solidity are
// all absent, which is exactly why the benchmark never caught #6295). A table
// of hand-written snippets would be a curated list of languages wearing a
// derive-shaped coat: it would grow only when someone remembered to grow it.
// The source scan derives its candidate set from the extractor tree itself, so
// a new extractor is covered the day it lands, with no sample needed.
//
// ── WHAT IT CAN AND CANNOT SEE ──────────────────────────────────────────────
//
// A statement of the gap, not a disclaimer. Every line below was established by
// running a probe package against this scan and reading the output, not by
// reasoning about the code. Probe results, #6365 review round 2:
//
// THE ONE THING THAT MATTERS FIRST: this scan bottoms out on a SPELLING LIST at
// the leaves. Every structural form below is recognised only when the value it
// ultimately reads is a bare identifier in filePathIdents or a selector whose
// FIELD name is in filePathFields. A probe with FromID: fp — a plain parameter
// named `fp` holding the file path — was NOT seen. Structure is handled; naming
// is not, and cannot be without type information this scan does not build.
//
// SEEN (probe made the test fail, at the exact line):
//
//	A. post-construction assignment — `rel.FromID = file.Path`, record appended
//	   elsewhere. LIVE IN-TREE at internal/extractors/yaml/helm.go:875, whose
//	   Kind is set in a different literal (helm.go:918 INCLUDES, helm.go:931
//	   BINDS), so it lands with kind "?". This form existed before this guard was
//	   written, which is why "composite literals only" was never a safe
//	   assumption. Intra-function only.
//	B. intra-function alias — `sourceOfTruth := filePath; …FromID: sourceOfTruth`.
//	   ONE hop, no fixed-point iteration, no crossing of function boundaries or
//	   struct fields, and only from a PURE path RHS: `x := "scope:…:" + path` is
//	   a structural ref, not a path, and is deliberately not aliased.
//	C. any struct field whose FIELD NAME is a path spelling — `c.SourceFile`,
//	   `file.Path`, `whatever.RelPath`. Matched on the field name alone, so a new
//	   receiver name needs no edit here.
//	E. `filepath.Join(dir, filePath)` — seen when at least one ARGUMENT is itself
//	   a recognised path spelling. `filepath.Join(dir, base)` is not seen.
//	F. concatenation, BOUNDED — `filePath + ""`, `filePath + ".x"`: the path must
//	   come FIRST and every appended operand must be a string literal.
//	   A literal PREFIX (`"scope:ormmodel:" + filePath + "#" + name`) builds a
//	   STRUCTURAL REF, which resolves byQualifiedName and is a different
//	   deliberate mechanism, not this bug. But that spelling is only ONE of the
//	   ways a structural-ref FromID escapes this scan, and it is not the dominant
//	   one. MEASURED 2026-08-19 over non-test sources under internal/extractors/,
//	   not inferred:
//	     - the DOMINANT form is D, an opaque helper call at the FromID site: 12
//	       direct `extractor.Build*StructuralRef(...)` FromIDs — hcl/terraform_deep.go
//	       :88, :150, :199, :243, :354; hcl/relationships.go:241;
//	       hcl/module_instantiation.go:99; hcl/cross_module.go:87;
//	       graphql/graphql.go:233; sql/sql.go:275 and :318; lua/oop.go:139 — plus
//	       cross/abibridge/extractor.go's bridgeMarkerRef (:203, read at :239 and
//	       :294). "Exactly four prefix schemes" (first round) was wrong.
//	     - the prefix concat itself NEVER reaches a FromID directly. All three
//	       in-tree uses assign to a LOCAL first — cross/ormlink/extractor.go:621
//	       (read at :638), haskell/depth.go:155 (read at :176), and
//	       cross/abibridge/extractor.go:283 — and what hides them is NOT this
//	       bound: collectPathAliases's type switch admits only *ast.Ident,
//	       *ast.SelectorExpr and *ast.CallExpr, so a *ast.BinaryExpr RHS never
//	       becomes an alias and the FromID is read as a plain unrecognised ident.
//	   There is therefore NO cost figure to quote for the PREFIX half of the
//	   bound: relaxing it would surface ZERO additional sites. MEASURED — every
//	   inline concat at a FromID in this tree is four sites (config/discover.go:310
//	   `"module:" + dir`, yaml/helm.go:879 `"helm_template:" + definer`,
//	   swift/package.go:189 and :242), and neither `dir` (discover.go:305,
//	   `filepath.ToSlash(filepath.Dir(rel))` — a CallExpr that is not
//	   filepath.Join, so not an alias) nor `definer` (a closure parameter,
//	   helm.go:873) is a recognised path spelling. No FromID in this tree is
//	   spelled `"<literal>" + <recognised path>`.
//	   The one REAL cost of the bound is the TRAILING half: a genuine anchor
//	   spelled `path + literal + ident` is missed — `filePath + "::" + d.name`.
//	   This is NOT a structural ref and it is LIVE IN-TREE at swift/package.go:189
//	   and :242. It was mis-attributed to the prefix group in the first round of
//	   this disclosure; see knownInvisibleOffenders below, which pins those two
//	   sites so the claim cannot rot into a comment nobody checks.
//	   markdown was ALSO mis-attributed to the prefix group, by the very sentence
//	   written to correct that mistake on swift. It is not a prefix site at all:
//	   markdown/markdown.go:171 is `qname := docQName + "::" + slug` with
//	   `docQName := file.Path` at :136 — path first, then a literal, then a
//	   non-literal ident, byte-for-byte the swift shape. It is invisible for a
//	   FURTHER reason as well; see the third category under
//	   knownInvisibleOffenders below.
//	G. positional composite literals of a relationship-shaped type, including the
//	   elided-type form inside `[]types.RelationshipRecord{{…}}`. No field names
//	   exist, so ANY path-valued element reports the site and the kind stays "?" —
//	   deliberately over-eager.
//	I. split literals — FromID in one composite literal, Kind assigned in another.
//	   Kind is no longer required; when absent the site is recorded as "?" rather
//	   than skipped.
//
// NOT SEEN (probe ran and the test stayed GREEN):
//
//	   THE LEAF SPELLING. `FromID: fp` where fp is the file path. No cheap fix.
//	D. a helper's return value — `FromID: pathOf(filePath)`. Only filepath.Join is
//	   special-cased; every other call is opaque. proto.go's fileContainsRel is
//	   this shape at its three call sites, and is caught only because the
//	   composite literal INSIDE the helper is itself visible — had the helper
//	   built the string another way, all three sites would be invisible.
//	   Also not seen, same root cause: an alias assigned in one function and read
//	   in another; a path stored on a struct field with an unlisted name and read
//	   back; form A performed across a function boundary.
//	   A computed Kind (`Kind: k`, k a parameter) reduces to "?" and so cannot be
//	   IMPORTS-filtered; such a site must be allow-listed by hand.
//
// ── FOUR BOUNDARIES THE FIRST DISCLOSURE LEFT OUT ───────────────────────────
//
// An incomplete disclosure is worse than a narrow scan, because a stated
// boundary is read as THE boundary. These four were omitted and are stated now.
//
//  1. THE SCAN ROOT IS `internal/extractors/` ONLY. The walk starts at ".", i.e.
//     the package directory of this test. What was SEARCHED is stated below
//     separately from what was CONCLUDED, because the first round ran the two
//     together and got the search badly wrong — it named SIX files.
//
//     SEARCHED (MEASURED 2026-08-19, not inferred). Non-test `.go` files outside
//     internal/extractors/ carrying a `FromID:` key: 125 files. 66 of them are
//     under internal/engine/ and 31 under internal/custom/ — internal/custom is
//     an entire parallel tree of language-specific relationship producers whose
//     existence the first round did not mention at all. Separately, 25
//     post-construction `.FromID =` assignments exist repo-wide, 16 of them
//     outside this tree: cmd/grafel/index.go x8, internal/resolve/refs.go x2,
//     internal/custom x3 (dart/flutter.go:216, java/patterns_dispatch.go:388,
//     csharp/aspnet_request_response.go:158), internal/engine x2
//     (workflow_edges.go:1142, precision_dedup.go:229), internal/graph/graph.go:249.
//
//     CONCLUDED. Of all of that, the FromID values that mention a path token are:
//     internal/patterns/openapi_extractor.go's 7 sites (:135, :238, :259, :284,
//     :301, :312, :323 — all entity Names, e.g. `entityName := "openapi_schema_" + name`
//     at :118); internal/resolve/bazel_overlay.go:200 and :226
//     (`fromID := labelToID[…]`, :187/:218 — an entity id);
//     internal/engine/feature_flag_edges.go:568, whose buildFeatureFlagCallerID
//     (:648-653) returns `"Function:" + caller` or the structural ref
//     `"File:" + path`; internal/engine/plugin_system_edges.go:96
//     (`fromID := "File:" + filePath`); internal/engine/http_endpoint_synthesis.go:643
//     and five internal/custom sites reached through one local hop
//     (elixir/absinthe_typegraph.go:196, javascript/graphql_codefirst_typegraph.go:193,
//     python/graphql_codefirst_typegraph.go:198, rust/graphql_codefirst_typegraph.go:216,
//     rust/juniper_typegraph.go:298), all `BuildOperationStructuralRef`, i.e.
//     `"scope:operation:method:" + lang + ":" + path + ":" + name`
//     (internal/extractor/structural_ref.go:15-17);
//     internal/engine/spring_routes.go:257 and django_routes.go:543
//     (`fmt.Sprintf("Route:%s", composedPath)`, where composedPath comes from
//     joinRoutePaths / joinDjangoRoutePaths — a URL route, not a file path); and
//     cmd/grafel/index.go:4688 plus internal/membench/fixture.go:154, both hex
//     graph.EntityID values. NONE is raw-path-valued, so there is NO LIVE MISS.
//
//     HOW COMPLETE THAT CONCLUSION IS. It rests on a name-based grep of FromID
//     value spellings plus ONE hop of same-file local assignment — the same leaf-
//     spelling weakness this scan admits above, applied by hand.
//     plugin_system_edges.go:96 is the in-repo proof that the direct grep misses
//     indirection: its FromID site reads `fromID` and only the assignment says
//     "path", so it appears in the list only because the one-hop sweep caught it.
//     A path reaching a FromID through a struct field, or through a second
//     function, would escape this sweep as well.
//
//     Independent of the miss count, the claim above that this scan "derives its
//     candidate set from the extractor tree itself, so a new extractor is covered
//     the day it lands" is a completeness claim about ONE tree, not about the
//     repo: a new relationship producer added under internal/custom,
//     internal/engine or internal/patterns would NOT be covered by this guard.
//
//  2. FORM G'S SHAPE TEST IS A SUBSTRING MATCH ON THE TYPE'S SPELLING.
//     relationshipShaped returns true only when exprString(t) contains
//     "relationship" (lower-cased). So a POSITIONAL literal of a locally
//     declared `type myRel struct{ FromID, ToID, Kind string }` is invisible —
//     the type name carries no such substring and form G returns early. KEYED
//     literals are unaffected: the keyed branch never consults
//     relationshipShaped, so a keyed `myRel{FromID: filePath, …}` is still seen
//     whatever the type is called.
//
//  3. A SPLIT-LITERAL `IMPORTS` EDGE LOSES ITS EXEMPTION AND IS WRONGLY FLAGGED.
//     The IMPORTS exemption (#120) is applied only to a Kind visible IN THE SAME
//     composite literal. Probe:
//     `r := types.RelationshipRecord{FromID: filePath, ToID: "mod"}; r.Kind = "IMPORTS"`
//     — the literal is reported under kind "?" and the guard fails. The
//     already-disclosed case was a COMPUTED Kind; this is a LITERAL "IMPORTS"
//     that merely lives in a second statement. It is a false-positive tax on the
//     documented, deliberate #120 convention, payable by hand-listing the site.
//
//  4. THE COUNT PINS CARDINALITY, NOT IDENTITY. The check is `len(ss) !=
//     entry.count`; it does not compare the FORM or the expression. Mutant:
//     replacing a blessed form-A site with a DIFFERENT offending construction
//     (form I, also keying "?") inside the same function left the count at 1 and
//     the test GREEN. The swap is NOT special to "?" keys — that framing, from
//     the previous round, presented the narrower case as the whole exposure. ANY
//     entry with count >= 2 already blesses any pair of sites of that kind in
//     that function, whatever the sites actually are. MEASURED 2026-08-19: four
//     such entries existed then — svelte:Extract:CONTAINS {2},
//     svelte:extractReactiveStatements:USES {2},
//     hcl:emitFileLevelRelationships:CONTAINS {2}, and
//     knownInvisibleOffenders["swift:extractTargets:DEPENDS_ON"] {2}. The two
//     svelte entries went away with the #6366 fix and the hcl one with #6367;
//     the only remaining count>=2 entry is swift (RE-MEASURED 2026-08-21).
//     What a "?" key ADDS on top of that is collapsing ACROSS FORMS: a form-A
//     site and a form-I site key identically, so one can be replaced by the
//     other. The only "?" entry today (yaml:extractHelmHelpers:?) has count 1,
//     so the across-forms half has no live consequence; the same-kind half is
//     live at ONE key right now, and it is where the next regression would hide.
//
// This is a guard against the careless repetition of a known pattern, not a
// proof. It is honest about being one.
package extractors

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// filePathIdents are bare identifier spellings this repo's extractors use for
// "the path of the file being extracted". Lower-cased before comparison.
// Extended at scan time by the intra-function alias pass (form B).
var filePathIdents = map[string]bool{
	"filepath": true, "path": true, "frompath": true, "srcpath": true,
	"relpath": true, "sourcefile": true, "srcfile": true, "filename": true,
}

// filePathFields are FIELD names that mean "the path of the file". Matched on
// the selector's field name alone (`x.Path`, `c.SourceFile`), so a receiver
// spelled a new way needs no edit here — form C.
var filePathFields = map[string]bool{
	"path": true, "filepath": true, "sourcefile": true, "srcpath": true,
	"relpath": true, "frompath": true, "srcfile": true, "filename": true,
}

// allowEntry records how many sites a key is expected to cover and why. The
// COUNT is the load-bearing half: keying by package+kind alone let a brand-new
// offender hide behind an existing key (proved by mutant — a class-anchored
// verilog USES edge collided with the allow-listed tool USES edge and the guard
// stayed green). Keys are package + enclosing function + kind, and the count
// pins the number of sites inside that one function.
type allowEntry struct {
	count  int
	reason string
}

// allowedFileAnchored maps "<extractor package>:<enclosing func>:<Kind>" to the
// number of sites expected there and the reason they are correct or knowingly
// unfixed. Keyed by package + function + Kind rather than by line so it does
// not rot when code moves, and counted so a NEW site in an already-listed
// function fails rather than inheriting the blessing.
//
// IMPORTS is not listed and is never reported: refs.go:3658-3669 documents
// file-anchored FromID as the deliberate cross-language convention for import
// edges (#120). An import statement belongs to a file; a type relationship
// belongs to a type.
//
// A kind of "?" means the scan saw a path-valued FromID but could not see the
// Kind (forms A, G and I above). Such a site cannot be IMPORTS-filtered and so
// must be listed here explicitly.
var allowedFileAnchored = map[string]allowEntry{
	// ── Correct AND functional: a node carrying that exact path exists ───────
	//
	// vhdl / verilog: the owning record is the TOOL component, not a file
	// record — buildToolEntities emits one SCOPE.Component per detected
	// toolchain and hangs the USES edge off it. The FromID resolves because the
	// package ALSO emits an extractor.FileEntity whose Name is that same path,
	// so the edge reads file→tool as intended. The first round stated this as
	// "the owning record IS file-scoped", which is factually wrong, and that
	// wrong reasoning is what produced the six bad entries re-labelled below.
	"vhdl:buildVHDLToolEntities:USES": {1,
		"file → toolchain component. The owner is the TOOL record; the path-valued " +
			"FromID resolves because the package emits an extractor.FileEntity carrying " +
			"that exact path. VERIFIED by reading the site + the FileEntity emission."},
	"verilog:buildToolEntities:USES": {1,
		"file → toolchain component. The owner is the TOOL record; the path-valued " +
			"FromID resolves because the package emits an extractor.FileEntity carrying " +
			"that exact path. VERIFIED by reading the site + the FileEntity emission."},

	// graphql CONTAINS: the owning record is an explicit synthetic file-level
	// container built with Name: filePath in the same function, so the FromID is
	// a self-reference onto a node that demonstrably exists in the emission.
	"graphql:extractGraphQL:CONTAINS": {1,
		"the synthetic file-level container is emitted with Name: filePath in this " +
			"same function, so the edge is a benign self-reference onto a node that " +
			"exists. VERIFIED by reading the container emission."},

	// yaml OVERRIDES / BINDS / INCLUDES and the form-A site: all resolve via the
	// SCOPE.Document anchor the yaml dispatcher prepends, which carries the
	// file path. Functional, not instances of this bug.
	"yaml:extractHelmValues:OVERRIDES": {1,
		"Helm values file → subchart values key; the values FILE is the overriding " +
			"thing and the package emits a SCOPE.Document anchor carrying that path. " +
			"VERIFIED by reading the anchor emission."},
	"yaml:extractHelmTemplate:BINDS": {1,
		"`fromRef := file.Path` (form B alias) → helm_values key; resolves via the " +
			"SCOPE.Document anchor. VERIFIED by reading the site; the site's own " +
			"comment names the anchor."},
	"yaml:extractHelmTemplate:INCLUDES": {1,
		"`fromRef := file.Path` (form B alias) → helm_template; resolves via the " +
			"SCOPE.Document anchor. VERIFIED by reading the site."},
	// helm.go:875 — form A, post-construction `rel.FromID = file.Path` inside
	// the attachFromDefiner closure. Its kinds (INCLUDES at helm.go:918, BINDS
	// at helm.go:931) live in different literals, so this lands as "?". Listed
	// because it is the in-tree proof that the composite-literal-only shape
	// assumption was violated before this guard was ever written.
	"yaml:extractHelmHelpers:?": {1,
		"post-construction `rel.FromID = file.Path` (form A) in the attachFromDefiner " +
			"fallback path; resolves via the SCOPE.Document anchor. VERIFIED by reading " +
			"the site. Kind is unknowable at this site by construction."},

	// markdown: `docQName := file.Path` (form B alias), and the Document entity
	// is emitted with QualifiedName: docQName in the same function, so the edge
	// is a self-reference onto a node that exists. This entry covers exactly ONE
	// site — markdown.go:296, `FromID: docQName`. It does NOT cover the heading
	// CONTAINS edges at :243 and :290, which carry the path-first `docQName +
	// "::" + slug` value (:171) through a struct field and are invisible to both
	// matchers in this file; see the third category at knownInvisibleOffenders.
	// markdown.go:486's `FromID: filePath` is a kind-IMPORTS site and is filtered
	// by the #120 exemption, not by this entry.
	"markdown:Extract:CONTAINS": {1,
		"`docQName := file.Path` (form B alias) → code block; the Document entity is " +
			"emitted with QualifiedName: docQName in this same function (markdown.go:142), " +
			"so the FromID resolves. VERIFIED by reading both sites."},

	// ── KNOWN OFFENDERS, deliberately NOT fixed in #6298 ─────────────────────
	//
	// EVIDENCE STATUS. Everything below is INFERRED from reading the owning
	// record against the resolution rule stated at the top of this file — no
	// runtime measurement. #6298's own lesson is that astro was ASSUMED to match
	// verilog and turned out to be a different, worse failure, so nothing here is
	// claimed as a finding. Follow-up issue covers all remaining languages, each
	// with its own measurement.
	//
	// svelte USED TO BE LISTED HERE (nine entries, four relationship kinds) and
	// is gone: #6366 measured all four — RENDERS, USES, NAVIGATES_TO and
	// CONTAINS — through ResolveImports → ReferencesEmbedded, found every one of
	// them dangling on the FROM side (15 of 15 on the measurement fixture), and
	// dropped FromID at all eleven sites so assembly stamps entities[0], the
	// component. See TestSvelte_ComponentRelsAnchoredOnComponent in
	// internal/extractors/svelte. Do NOT re-add a svelte entry here without a
	// fresh measurement: the scan below fails on a STALE entry too.

	// graphql FEDERATES — MISANCHORED, and the site's own comment says so:
	// graphql.go:366-368 states the edge comes "from the extending stub", but
	// FromID is filePath, so the rewrite lands on the synthetic file container
	// instead. Every extending stub in one file merges its federation edges onto
	// that one node. The first-round reason ("the subgraph IS the file")
	// contradicted the code it blessed.
	"graphql:extractGraphQL:FEDERATES": {1,
		"KNOWN OFFENDER (#6298): misanchored. The site's own comment says the edge is " +
			"'from the extending stub', but FromID is filePath, so it lands on the " +
			"synthetic file container and every stub in the file merges onto it. " +
			"INFERRED from the site, NOT measured."},

	// proto CONTAINS — DANGLING. fileContainsRel builds FromID: file.Path, but
	// the proto package emits no node named for the containing file; the only
	// path-named entity in a proto extraction is the IMPORTED path. Two lines
	// below one of its call sites, proto.go:260's sibling service→rpc CONTAINS
	// edge correctly leaves FromID empty — the two shapes sit side by side.
	"proto:fileContainsRel:CONTAINS": {1,
		"KNOWN OFFENDER (#6298): dangling. No proto node carries the CONTAINING " +
			"file's path (only the IMPORTED path is path-named), and the sibling edge " +
			"at proto.go:260 correctly leaves FromID empty. Helper is called from 3 " +
			"sites. INFERRED from the site, NOT measured."},

	// hcl USED TO BE LISTED HERE (emitFileLevelRelationships:CONTAINS {2} and
	// parseDependsOnTuple:DEPENDS_ON {1}) and is gone: #6367 MEASURED both
	// through ResolveImports -> ReferencesEmbedded on a nested and a root path.
	// Nested "infra/envs/prod/main.tf": 5 of 5 CONTAINS and 2 of 2 DEPENDS_ON
	// DANGLING. Root "main.tf": CONTAINS resolved onto the file component --
	// the right node BY ACCIDENT, since the component's Name is the BASENAME --
	// while DEPENDS_ON was MISANCHORED onto that same file component, off the
	// resource and module records that actually carry it. All three sites now
	// leave FromID empty so assembly stamps the owner; 0 of 7 after. See
	// TestHCL_ContainsAndDependsOnAnchoredOnOwner in internal/extractors/hcl.
	// Do NOT re-add an hcl entry here without a fresh measurement: the scan
	// below fails on a STALE entry too.

	// bicep — DANGLING unconditionally and misowned. Worse than the hcl pair:
	// `.bicep` is absent from sourceFileExtensions (refs.go:2752-2767), so these
	// do not even reach DispositionDynamic; they count as
	// DispositionBugExtractor. The first-round allow-list was blessing a live
	// bug-extractor figure.
	"bicep:dependencyEdges:DEPENDS_ON": {1,
		"KNOWN OFFENDER (#6298): dangling unconditionally, wrong owner, and `.bicep` " +
			"is not in sourceFileExtensions, so it lands in DispositionBugExtractor " +
			"rather than DispositionDynamic. INFERRED from the site, NOT measured."},

	// dockerfile — DANGLING for any Dockerfile not at the repo root, and
	// `Dockerfile` is likewise absent from sourceFileExtensions, so these also
	// count as DispositionBugExtractor.
	"dockerfile:collectCopy:USES": {1,
		"KNOWN OFFENDER (#6298): dangling for any non-root Dockerfile, and " +
			"`Dockerfile` is not in sourceFileExtensions, so it lands in " +
			"DispositionBugExtractor. INFERRED from the site, NOT measured."},
}

// knownInvisibleOffenders pins the file anchors that the MAIN scan cannot see
// because of form F's trailing-literal bound: `<path> + <literal> + <ident>`.
// They are NOT structural refs and they are NOT exempt; they are simply below
// the main matcher's resolution, and the first round of this disclosure
// mis-attributed them to the structural-ref prefix group.
//
// This list is enforced by TestKnownInvisibleFileAnchoredOffenders, which runs
// its own narrow matcher over the same tree. That makes the entry load-bearing
// in BOTH directions: deleting the entry fails as an unaccounted site, and
// fixing the code fails the entry as stale. It is not a comment.
//
// A THIRD CATEGORY exists and is deliberately NOT pinned here, because pinning it
// would be a comment wearing a test's coat. markdown/markdown.go builds
// `qname := docQName + "::" + slug` (:171) from `docQName := file.Path` (:136) —
// byte-for-byte the swift shape, and mis-called a literal-PREFIX site by the
// first two rounds of this disclosure. But that value never appears AT a FromID:
// it is stored on the heading entity records and read back as `parentQName`
// (:225, assigned from `headingEntities[i].QualifiedName`) at :243, and as
// `headingEntities[parentIdx].QualifiedName` at :290. Both matchers in this file
// inspect the FromID EXPRESSION only, and "QualifiedName" is not in
// filePathFields, so neither sees it — form F's bound is not what hides it.
// Adding a "markdown:Extract:CONTAINS" key to this map was TRIED (see the mutant
// log on #6365) and fails instantly as stale, because scanPathFirstConcatFromIDs
// matches zero markdown sites. A load-bearing pin would need the matcher to
// follow a value through a struct field and back out, which is the form-D
// limitation stated at the top of this file, not a bound that can be relaxed.
// The site APPEARS benign — the heading entities are emitted with
// `QualifiedName: qname` at :199, so a node carrying that exact string does
// exist — but that is INFERRED from reading the emission site, NOT measured, and
// "appears benign" is not the same claim as "a deliberate structural-ref
// mechanism", which is what the first two rounds asserted about it.
//
// Keyed and counted exactly like allowedFileAnchored.
var knownInvisibleOffenders = map[string]allowEntry{
	// swift/package.go:189 (product deps) and :242 (bare target deps) — the
	// failure message reports 188 and 241, the lines the enclosing composite
	// literals open on. Both are
	// `FromID: filePath + "::" + d.name` — path FIRST, then a literal, then a
	// non-literal ident, so isFilePathExpr's BinaryExpr case rejects them at the
	// trailing operand. Not a structural ref:
	//   - the owning record (swift/package.go:164-177) sets Name: d.name and NO
	//     QualifiedName at all; the only two "::" occurrences in the package are
	//     these two FromIDs;
	//   - nothing in internal/resolve knows the swift_package / swiftpm "::"
	//     spelling (grep for swiftpm|swift_package under internal/resolve is
	//     empty), so there is no byQualifiedName scheme to land on;
	//   - both edges are appended to rec.Relationships — the target component
	//     itself — so FromID should simply be EMPTY.
	// DANGLING and MISOWNED: the astro failure mode.
	"swift:extractTargets:DEPENDS_ON": {2,
		"KNOWN OFFENDER (#6298): dangling AND misowned. `filePath + \"::\" + d.name` on a " +
			"record whose owner is the swiftpm target component; no node carries that " +
			"string and internal/resolve has no swiftpm \"::\" scheme, so the raw value " +
			"reaches the graph. INFERRED from the site + the record emission + an empty " +
			"grep of internal/resolve, NOT measured. Invisible to the main scan (form F " +
			"trailing-literal bound). Not fixed here for the same reason as the other six: " +
			"each language needs its own measurement, and astro proved that assuming a " +
			"shared shape is how this goes wrong."},
}

type fileAnchoredSite struct {
	pkg, fn, kind, key, file string
	line                     int
	fromExpr                 string
	form                     string
}

// scanFileAnchoredRels walks root for non-test Go sources and returns every
// site that binds a relationship's FromID to a file-path expression under a
// kind that is not IMPORTS. See the "WHAT IT CANNOT SEE" block above for the
// forms it covers and the ones it does not.
func scanFileAnchoredRels(t *testing.T, root string) []fileAnchoredSite {
	t.Helper()
	fset := token.NewFileSet()
	var out []fileAnchoredSite

	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// testdata is fixture input, not extractor source. Two Go files under
		// this root live there (testdata/incrfixture/main.go,
		// golang/testdata/issue4426/constants.go); a fixture carrying a
		// `FromID:` line would be reported under a bogus package key, and a
		// deliberately-malformed Go fixture would trip the parse-error branch
		// and fail this guard for an unrelated reason.
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, p, nil, 0)
		if perr != nil {
			t.Errorf("parse %s: %v", p, perr)
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		pkg := filepath.Dir(rel)
		// Nested extractor groups (cross/httpclient, …) keep their full path so
		// the allow-list key stays unambiguous.
		if pkg == "." {
			pkg = "extractors"
		}

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			aliases := collectPathAliases(fn.Body)
			isPath := func(e ast.Expr) bool { return isFilePathExpr(e, aliases) }
			// Elements of `[]types.RelationshipRecord{{…}}` elide their type, so
			// the inner literal has cl.Type == nil. ast.Inspect is top-down, so
			// the outer literal is always seen first and can mark its children.
			elided := map[*ast.CompositeLit]bool{}

			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch v := n.(type) {
				case *ast.AssignStmt:
					// Form A: post-construction `rel.FromID = <path>`.
					for i, lhs := range v.Lhs {
						sel, ok := lhs.(*ast.SelectorExpr)
						if !ok || sel.Sel.Name != "FromID" || i >= len(v.Rhs) {
							continue
						}
						if !isPath(v.Rhs[i]) {
							continue
						}
						out = append(out, mkSite(fset, pkg, fn.Name.Name, "?", rel,
							v.Pos(), exprString(v.Rhs[i]), "A:assignment"))
					}
				case *ast.CompositeLit:
					if eltShaped(v.Type) {
						for _, el := range v.Elts {
							if inner, ok := el.(*ast.CompositeLit); ok && inner.Type == nil {
								elided[inner] = true
							}
						}
					}
					var from, kind ast.Expr
					keyed := false
					for _, el := range v.Elts {
						kv, ok := el.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						keyed = true
						id, ok := kv.Key.(*ast.Ident)
						if !ok {
							continue
						}
						switch id.Name {
						case "FromID":
							from = kv.Value
						case "Kind":
							kind = kv.Value
						}
					}
					if !keyed && len(v.Elts) > 0 {
						// Form G: positional literal of a relationship-shaped
						// type. No field names available, so any path-valued
						// element counts and the kind stays unknown.
						if !relationshipShaped(v.Type) && !elided[v] {
							return true
						}
						for _, el := range v.Elts {
							if isPath(el) {
								out = append(out, mkSite(fset, pkg, fn.Name.Name, "?", rel,
									v.Pos(), exprString(el), "G:positional"))
								break
							}
						}
						return true
					}
					if from == nil || !isPath(from) {
						return true
					}
					// Form I: Kind absent from this literal. Recorded as "?"
					// rather than skipped, so a split literal cannot hide.
					k := "?"
					if kind != nil {
						k = relKindString(kind)
					}
					if k == "IMPORTS" {
						return true
					}
					form := "keyed"
					if kind == nil {
						form = "I:split-literal"
					}
					out = append(out, mkSite(fset, pkg, fn.Name.Name, k, rel,
						v.Pos(), exprString(from), form))
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	slices.SortFunc(out, func(a, b fileAnchoredSite) int {
		if a.file != b.file {
			return strings.Compare(a.file, b.file)
		}
		return a.line - b.line
	})
	return out
}

func mkSite(fset *token.FileSet, pkg, fn, kind, file string, pos token.Pos, from, form string) fileAnchoredSite {
	return fileAnchoredSite{
		pkg:      pkg,
		fn:       fn,
		kind:     kind,
		key:      pkg + ":" + fn + ":" + kind,
		file:     file,
		line:     fset.Position(pos).Line,
		fromExpr: from,
		form:     form,
	}
}

// collectPathAliases does one hop of intra-function alias tracking (form B):
// `sourceFile := filePath` makes `sourceFile` a path spelling for the rest of
// that function. It does NOT iterate to a fixed point and does NOT cross
// function boundaries.
func collectPathAliases(body *ast.BlockStmt) map[string]bool {
	aliases := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range as.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok || i >= len(as.Rhs) {
				continue
			}
			// Only a PURE path RHS makes an alias. `x := "scope:…:" + path`
			// is a structural ref, not a path (see isFilePathExpr's
			// BinaryExpr case), and aliasing it would flag every structural-ref
			// scheme in the tree.
			switch as.Rhs[i].(type) {
			case *ast.Ident, *ast.SelectorExpr, *ast.CallExpr:
				if isFilePathExpr(as.Rhs[i], nil) {
					aliases[strings.ToLower(id.Name)] = true
				}
			}
		}
		return true
	})
	return aliases
}

// isFilePathExpr reports whether e is a syntactically recognisable "path of the
// file being extracted". Covers bare idents and aliases (B), any selector whose
// FIELD name is a path spelling (C), filepath.Join (E) and concatenation (F).
// A call to anything else is opaque (D).
func isFilePathExpr(e ast.Expr, aliases map[string]bool) bool {
	switch v := e.(type) {
	case *ast.Ident:
		n := strings.ToLower(v.Name)
		return filePathIdents[n] || aliases[n]
	case *ast.SelectorExpr:
		return filePathFields[strings.ToLower(v.Sel.Name)]
	case *ast.BinaryExpr:
		// Form F, bounded. A concatenation is still path-SHAPED only when the
		// path comes FIRST and everything appended to it is a string literal
		// (`filePath + ""`, `filePath + ".x"`). A literal PREFIX — the
		// `"scope:ormmodel:" + filePath + "#" + name` spelling used by ormlink
		// (cross/ormlink/extractor.go:621), haskell (depth.go:155) and abibridge
		// (cross/abibridge/extractor.go:283) — builds a STRUCTURAL REF, which is
		// a different, deliberate resolution scheme (byQualifiedName), not a file
		// anchor. But note what the prefix half of this bound actually buys:
		// NOTHING. All three of those sites assign the concat to a local and are
		// already excluded by collectPathAliases's type switch (a *ast.BinaryExpr
		// RHS never becomes an alias), and MEASURED 2026-08-19 no FromID anywhere
		// in this tree is spelled `"<literal>" + <recognised path>`, so relaxing
		// the path-first rule would surface ZERO extra sites. It is kept because
		// it is cheap and states the intent, not because it is load-bearing. See
		// form F in the block comment at the top of this file for the site list.
		// markdown is NOT one of these — markdown.go:171 is path-FIRST
		// (`docQName + "::" + slug`), the swift shape; it is invisible for a
		// different reason, documented at knownInvisibleOffenders.
		//
		// The trailing-literal half of the bound is what hides
		// `filePath + "::" + d.name` (swift/package.go:189,242), which is a real
		// file anchor and NOT a structural ref. It is pinned separately by
		// knownInvisibleOffenders / TestKnownInvisibleFileAnchoredOffenders
		// rather than by relaxing this predicate, because the path-first rule is
		// the only thing keeping the structural-ref schemes out.
		ops := flattenConcat(v)
		if len(ops) == 0 || !isFilePathExpr(ops[0], aliases) {
			return false
		}
		for _, o := range ops[1:] {
			lit, ok := o.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return false
			}
		}
		return true
	case *ast.CallExpr:
		sel, ok := v.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		pkg, isIdent := sel.X.(*ast.Ident)
		if !isIdent || pkg.Name != "filepath" || sel.Sel.Name != "Join" {
			return false
		}
		for _, a := range v.Args {
			if isFilePathExpr(a, aliases) {
				return true
			}
		}
		return false
	case *ast.ParenExpr:
		return isFilePathExpr(v.X, aliases)
	}
	return false
}

// flattenConcat flattens a left-nested `a + b + c` string concatenation into
// its operands, in source order. Returns nil for any other operator.
func flattenConcat(e ast.Expr) []ast.Expr {
	be, ok := e.(*ast.BinaryExpr)
	if !ok {
		return []ast.Expr{e}
	}
	if be.Op != token.ADD {
		return nil
	}
	left := flattenConcat(be.X)
	right := flattenConcat(be.Y)
	if left == nil || right == nil {
		return nil
	}
	return append(left, right...)
}

// eltShaped reports whether t is a slice / array / map of a relationship-shaped
// type, i.e. whether its elements elide a relationship type name.
func eltShaped(t ast.Expr) bool {
	switch v := t.(type) {
	case *ast.ArrayType:
		return relationshipShaped(v.Elt)
	case *ast.MapType:
		return relationshipShaped(v.Value)
	}
	return false
}

// relationshipShaped reports whether a composite literal's type looks like a
// relationship record, used to bound the positional-literal scan (form G).
func relationshipShaped(t ast.Expr) bool {
	if t == nil {
		return false
	}
	return strings.Contains(strings.ToLower(exprString(t)), "relationship")
}

// relKindString renders a Kind expression as the edge kind. A plain string
// literal is unquoted; `string(types.RelationshipKindOverrides)` and the bare
// constant both reduce to OVERRIDES. Anything else is returned verbatim so an
// unrecognised spelling shows up as an unknown key rather than passing quietly.
func relKindString(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.BasicLit:
		return strings.Trim(v.Value, `"`)
	case *ast.CallExpr:
		if len(v.Args) == 1 {
			if id, ok := v.Fun.(*ast.Ident); ok && id.Name == "string" {
				return relKindString(v.Args[0])
			}
		}
	case *ast.SelectorExpr:
		return strings.ToUpper(strings.TrimPrefix(v.Sel.Name, "RelationshipKind"))
	case *ast.Ident:
		return strings.ToUpper(strings.TrimPrefix(v.Name, "RelationshipKind"))
	}
	return exprString(e)
}

func exprString(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return exprString(v.X) + "." + v.Sel.Name
	case *ast.BasicLit:
		return v.Value
	case *ast.CallExpr:
		return exprString(v.Fun) + "(…)"
	case *ast.BinaryExpr:
		return exprString(v.X) + v.Op.String() + exprString(v.Y)
	case *ast.ParenExpr:
		return "(" + exprString(v.X) + ")"
	case *ast.StarExpr:
		return "*" + exprString(v.X)
	}
	return "<expr>"
}

// TestNoNewFileAnchoredTypeRelationships fails when an extractor binds a
// non-IMPORTS relationship's FromID to the source file path from a site the
// allow-list above does not account for, INCLUDING a new site inside a function
// that already has an allow-listed site of the same kind.
func TestNoNewFileAnchoredTypeRelationships(t *testing.T) {
	sites := scanFileAnchoredRels(t, ".")

	// Vacuity guard 1: a matcher that matches nothing passes for free.
	if len(sites) == 0 {
		t.Fatal("scanner found no file-anchored relationship sites at all — " +
			"the walk or the AST match has broken, and a guard that matches " +
			"nothing passes for free")
	}

	observed := map[string][]fileAnchoredSite{}
	for _, s := range sites {
		observed[s.key] = append(observed[s.key], s)
	}

	fmtSites := func(ss []fileAnchoredSite) string {
		var b []string
		for _, s := range ss {
			b = append(b, fmt.Sprintf("%s:%d  FromID: %s  [%s]", s.file, s.line, s.fromExpr, s.form))
		}
		return strings.Join(b, "\n      ")
	}

	keys := make([]string, 0, len(observed))
	for k := range observed {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var unaccounted []string
	for _, k := range keys {
		ss := observed[k]
		entry, ok := allowedFileAnchored[k]
		if !ok {
			unaccounted = append(unaccounted, fmt.Sprintf("key %q (%d site(s)):\n      %s", k, len(ss), fmtSites(ss)))
			continue
		}
		// Vacuity guard 2: the count pins the number of sites, so a NEW site in
		// an already-blessed function+kind fails instead of inheriting the
		// blessing. This is the granularity mutant from the #6365 review.
		if len(ss) != entry.count {
			unaccounted = append(unaccounted, fmt.Sprintf(
				"key %q: allow-list expects %d site(s), found %d:\n      %s",
				k, entry.count, len(ss), fmtSites(ss)))
		}
	}
	if len(unaccounted) > 0 {
		t.Errorf("relationship FromID bound to the source file path at unaccounted site(s) (#6298):\n\n  %s\n\n"+
			"Leave FromID EMPTY so graph assembly stamps the owning record's own entity id\n"+
			"(cmd/grafel/index.go and relRecordToGraphRel in internal/extractors/incremental.go).\n"+
			"A path-valued FromID resolves ONLY if some emitted node carries that exact path\n"+
			"as its Name/QualifiedName — being 'conceptually file-scoped' is not enough.\n"+
			"If it really does resolve, add the key to allowedFileAnchored in this file with\n"+
			"the site count and the reason, and say what you MEASURED vs what you INFERRED.",
			strings.Join(unaccounted, "\n\n  "))
	}

	// Vacuity guard 3: a stale allow-list entry is its own defect — it hides the
	// next offender under a key nobody is watching any more, and it is also how
	// a NARROWING of the matcher announces itself (a narrowed matcher stops
	// producing sites, and every orphaned key fails here).
	for key := range allowedFileAnchored {
		if len(observed[key]) == 0 {
			t.Errorf("allowedFileAnchored[%q] matches no site any more — either the code "+
				"was fixed (delete the entry) or the scanner was narrowed (fix the scanner)", key)
		}
	}
}

// scanPathFirstConcatFromIDs is the narrow companion matcher for
// knownInvisibleOffenders. It finds FromID values that are a concatenation
// STARTING with a recognised path spelling but which isFilePathExpr REJECTS,
// i.e. at least one appended operand is not a string literal
// (`filePath + "::" + d.name`). The path-first requirement is retained so the
// literal-PREFIX structural-ref spellings — ormlink, haskell, abibridge — stay
// excluded here exactly as in form F; in practice they are excluded twice over,
// since all three reach FromID through a local that collectPathAliases refuses
// to alias. markdown is NOT in that group: its concat IS path-first, but it
// reaches FromID through a struct field (`…QualifiedName`), which this matcher
// does not follow either. See knownInvisibleOffenders for why it is documented
// rather than listed.
//
// It walks independently of scanFileAnchoredRels so that a narrowing of the
// main matcher cannot silently narrow this one too.
func scanPathFirstConcatFromIDs(t *testing.T, root string) []fileAnchoredSite {
	t.Helper()
	fset := token.NewFileSet()
	var out []fileAnchoredSite

	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, p, nil, 0)
		if perr != nil {
			t.Errorf("parse %s: %v", p, perr)
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		pkg := filepath.Dir(rel)
		if pkg == "." {
			pkg = "extractors"
		}

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			aliases := collectPathAliases(fn.Body)
			// A concat whose FIRST operand is a path but which the main
			// matcher rejects.
			hidden := func(e ast.Expr) bool {
				be, ok := e.(*ast.BinaryExpr)
				if !ok {
					return false
				}
				ops := flattenConcat(be)
				if len(ops) < 2 || !isFilePathExpr(ops[0], aliases) {
					return false
				}
				return !isFilePathExpr(be, aliases)
			}

			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch v := n.(type) {
				case *ast.AssignStmt:
					for i, lhs := range v.Lhs {
						sel, ok := lhs.(*ast.SelectorExpr)
						if !ok || sel.Sel.Name != "FromID" || i >= len(v.Rhs) {
							continue
						}
						if !hidden(v.Rhs[i]) {
							continue
						}
						out = append(out, mkSite(fset, pkg, fn.Name.Name, "?", rel,
							v.Pos(), exprString(v.Rhs[i]), "F-hidden:assignment"))
					}
				case *ast.CompositeLit:
					var from, kind ast.Expr
					for _, el := range v.Elts {
						kv, ok := el.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						id, ok := kv.Key.(*ast.Ident)
						if !ok {
							continue
						}
						switch id.Name {
						case "FromID":
							from = kv.Value
						case "Kind":
							kind = kv.Value
						}
					}
					if from == nil || !hidden(from) {
						return true
					}
					k := "?"
					if kind != nil {
						k = relKindString(kind)
					}
					if k == "IMPORTS" {
						return true
					}
					out = append(out, mkSite(fset, pkg, fn.Name.Name, k, rel,
						v.Pos(), exprString(from), "F-hidden:keyed"))
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	slices.SortFunc(out, func(a, b fileAnchoredSite) int {
		if a.file != b.file {
			return strings.Compare(a.file, b.file)
		}
		return a.line - b.line
	})
	return out
}

// TestKnownInvisibleFileAnchoredOffenders makes knownInvisibleOffenders
// load-bearing rather than decorative. Every `<path> + <literal> + <non-literal>`
// FromID in the tree must be listed with its site count; every listed key must
// still match that many sites.
func TestKnownInvisibleFileAnchoredOffenders(t *testing.T) {
	sites := scanPathFirstConcatFromIDs(t, ".")

	observed := map[string][]fileAnchoredSite{}
	for _, s := range sites {
		observed[s.key] = append(observed[s.key], s)
	}

	// Stale-entry check FIRST. swift is currently the only site of this shape in
	// the tree, so fixing it empties the matcher; if the vacuity guard ran first
	// it would report "the matcher has broken" for what is actually a fix. Both
	// fire, but the accurate diagnosis leads.
	for key := range knownInvisibleOffenders {
		if len(observed[key]) == 0 {
			t.Errorf("knownInvisibleOffenders[%q] matches no site any more — either the code "+
				"was fixed (delete the entry) or the matcher was narrowed (fix the matcher)", key)
		}
	}

	// Vacuity guard: the matcher must still match the shape it was written for.
	if len(sites) == 0 {
		t.Error("path-first-concat matcher found no sites at all — either every listed " +
			"offender above was genuinely fixed, or the walk / AST match has broken and " +
			"a guard that matches nothing passes for free")
		return
	}
	keys := make([]string, 0, len(observed))
	for k := range observed {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		ss := observed[k]
		entry, ok := knownInvisibleOffenders[k]
		if !ok {
			var b []string
			for _, s := range ss {
				b = append(b, fmt.Sprintf("%s:%d  FromID: %s  [%s]", s.file, s.line, s.fromExpr, s.form))
			}
			t.Errorf("relationship FromID is a path-first concatenation the MAIN scan cannot see, "+
				"at unaccounted key %q (%d site(s)):\n      %s\n\n"+
				"This is the form-F trailing-literal blind spot. Either it is a real file anchor "+
				"(leave FromID EMPTY), or it resolves — in which case add it to "+
				"knownInvisibleOffenders with its site count and say what you MEASURED vs INFERRED.",
				k, len(ss), strings.Join(b, "\n      "))
			continue
		}
		if len(ss) != entry.count {
			t.Errorf("knownInvisibleOffenders[%q]: expects %d site(s), found %d", k, entry.count, len(ss))
		}
	}
}
