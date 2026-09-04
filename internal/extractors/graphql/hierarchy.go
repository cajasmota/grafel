// hierarchy.go — GraphQL inheritance topology: `type X implements A & B` and
// `interface X implements Y` → IMPLEMENTS (#6370).
//
// Before this, this extractor emitted no hierarchy edge by either of the two
// paths #6370 enumerates: graphql is absent from `supportedLanguages` in
// `internal/extractors/cross/hierarchy/extractor.go`, and the word
// `implements` appeared in no pattern in this package — `typeDefRE`
// (graphql.go) stops at the declared name and everything after it was
// discarded.
//
// There is a THIRD path #6370's audit did not cover, and graphql is on it:
// the YAML rule packs. `internal/engine/rules/graphql/frameworks/
// graphql_schema.yaml` already matches `type\s+(\w+)\s+implements\s+(\w+)`
// and emits IMPLEMENTS. What it does NOT cover is why this file exists: it
// ignores `interface X implements Y` entirely, it takes only the FIRST name of
// an `&` list, and it is unanchored and comment-blind — on a draft of the
// golden fixture it emitted an edge for a COMMENTED-OUT
// `# type Ghost implements Node {`, along with a `Ghost` entity to hang it on.
// Recorded on #6370; not fixed here.
//
// # Why IMPLEMENTS, and why there is no kind ladder
//
// SDL has exactly one inheritance construct. An object type or an interface
// type lists the interfaces it satisfies after the keyword `implements`; there
// is no `extends`, no single-inheritance base, and no second spelling. So,
// unlike `internal/extractors/csharp/hierarchy.go`, there is nothing here to
// select between: a `type` and an `interface` name their targets with the same
// keyword and get the same edge kind. A kind ladder here would be a ladder
// with one rung, and every branch on it would be untestable.
//
// # Why the edges are emitted HERE and not by registering graphql in cross/hierarchy
//
// That pass invents its own graph nodes: for every type it addEntity's a
// SCOPE.Component for the type AND another for each parent. `extractGraphQL`
// already emits one SCOPE.Schema per definition, so registering graphql there
// would mint a duplicate component per type plus a second, differently-kinded
// node for every interface that is already declared in the file — with the
// edges anchored on the pass's own nodes rather than on the ones the rest of
// the graphql graph uses. That is why #6335 emitted F#'s edges from the F#
// extractor, #6437 groovy's from the groovy one and #6797 erlang's from the
// erlang one. TestGraphQLImplements_NoDuplicateEntities guards it.
//
// # How much text is read, and what that does and does not exclude
//
// The clause is read from the DECLARATION HEADER only: the bytes between the
// end of the declared name and the first `{`, `#`, `@`, or a newline whose next
// line is not indented. That bound is what excludes the two decoys that
// matter, and it excludes them for a real reason rather than by luck:
//
//   - a field NAMED `implements` (`type Settings { implements: String }`) is
//     past the `{` and so is never read at all; and
//   - a `#` comment on the header line is a hard stop.
//
// What it does NOT do is give this extractor comment or string awareness,
// which it has nowhere. A line-initial `type X implements Y {` inside a `"""`
// block description over-fires — but it over-fires ALREADY, as a bogus
// SCOPE.Schema ENTITY from typeDefRE, which has no string awareness either;
// this adds an edge to a defect that predates it.
// TestGraphQLImplementsInsideBlockDescriptionOverFires_KnownDivergence asserts
// that current WRONG output on purpose, so whoever teaches typeDefRE about
// `"""` is told by a failing test that they have also fixed this.
//
// The unindented-next-line stop exists so an incomplete header cannot run into
// whatever follows it. Column 0 is this extractor's own definition of "top
// level" — `typeDefRE` is `(?m)^` anchored — so the header bound and the
// definition scan agree by construction.
//
// # Two shapes this bound reads too little of, both pinned
//
// Both are SAFE directions — an edge is dropped, never invented — and both are
// asserted as today's output by a KnownDivergence test rather than left as
// prose, because a limitation nobody pins is an invitation to widen the parser
// with nothing to notice:
//
//   - A continuation line beginning with `&` AT COLUMN 0
//     (`type User implements Node\n& Timestamped {`) ends the header, so only
//     `Node` is read. SDL does not require the continuation to be indented;
//     this bound does.
//     TestGraphQLImplementsColumnZeroAmpersandContinuationIsDropped_KnownDivergence.
//   - A COMMA-separated list (`implements Node, Timestamped`), which graphql-js
//     historically accepted, yields NOTHING: the segment is not a single Name,
//     so it is discarded whole rather than split on a separator SDL never
//     specified. TestGraphQLImplementsCommaSeparatedListIsDropped_KnownDivergence.
//
// It is NOT what stops `type X implements` running into a following
// `type Y {`: that segment reads `type Y`, two words, which the Name check in
// implementsTargets discards anyway. The stop is load-bearing for the
// single-token case — a column-0 bare Name — and a mutant deleting it survived
// a test that only covered the two-word one, which is why
// TestGraphQLImplementsHeaderStopsAtUnindentedLine now covers both and says
// which half each one is excluded by.
package graphql

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

var (
	// The SDL keyword, as a whole word — BOTH boundaries, each load-bearing on
	// its own side. Without the trailing one `implementsNode` is read as a
	// clause naming Node; without the leading one `reimplements Node` is. Both
	// directions are scored by TestGraphQLImplementsKeywordMustBeAWholeWord.
	implementsKeywordRE = regexp.MustCompile(`\bimplements\b`)
	// A GraphQL Name (spec: /[_A-Za-z][_0-9A-Za-z]*/), anchored so a segment
	// carrying anything else — `A B`, `A,`, a stray `!` — is discarded rather
	// than half-parsed.
	graphqlNameRE = regexp.MustCompile(`^[_A-Za-z][_0-9A-Za-z]*$`)
)

// declarationHeader returns the bytes of a type-system definition between the
// end of its declared name (nameEnd) and the end of its header. See the
// package-level note above for what the four stops exclude.
// There is deliberately no `nameEnd < 0 || nameEnd > len(src)` guard. The sole
// call site passes m[5] — the end offset of group 2 of a SUCCESSFUL
// typeDefRE.FindAllStringSubmatchIndex match — which is by construction in
// [0, len(src)]. Re-introducing the check scores equivalent under any suite,
// which is the evidence it belongs out of the code: a branch no input can
// reach is not a guard, it is a claim no test can observe. Same treatment, and
// the same reason, as the `}` that is absent from the stop set below.
func declarationHeader(src string, nameEnd int) string {
	for i := nameEnd; i < len(src); i++ {
		switch src[i] {
		// No `}` here. A closing brace can only be met before an opening one
		// in source that is not SDL at all, and with the newline stop below it
		// is unreachable even then, so it would be a branch no input can
		// observe rather than a guard.
		case '{', '#', '@':
			return src[nameEnd:i]
		case '\n':
			if i+1 >= len(src) || (src[i+1] != ' ' && src[i+1] != '\t') {
				return src[nameEnd:i]
			}
		}
	}
	return src[nameEnd:]
}

// implementsTargets returns the interface names named by a declaration
// header's `implements` clause, in source order.
//
// The list separator is `&`, and SDL permits a leading one
// (`implements & A & B`), so empty segments are dropped rather than treated as
// a parse failure. A segment that is not a single GraphQL Name is dropped too:
// `implements A B` (a missing `&`) yields NOTHING rather than a target called
// "A B" or a silent guess at which half was meant.
func implementsTargets(header string) []string {
	m := implementsKeywordRE.FindStringIndex(header)
	if m == nil {
		return nil
	}
	var out []string
	for _, seg := range strings.Split(header[m[1]:], "&") {
		name := strings.TrimSpace(seg)
		if name == "" || !graphqlNameRE.MatchString(name) {
			continue
		}
		out = append(out, name)
	}
	return out
}

// implementsEdges returns one IMPLEMENTS relationship per interface named by
// the declaration at nameEnd, meant to be EMBEDDED on the owning definition's
// EntityRecord.
//
// FromID is deliberately left EMPTY. The assembly loop stamps the owning
// record's own entity id, the only value that anchors the edge on the TYPE.
// A non-empty non-hex FromID (e.g. the file path) would be rewritten by
// ReferencesEmbedded onto the FILE entity, merging every type in a schema file
// onto one node — the defect fixed in #6295 (Solidity) and #6298 (Verilog,
// Astro) and now guarded by
// internal/extractors/file_anchored_rels_guard_test.go (#6367).
//
// ToID is the bare written interface name, matching the groovy/erlang/F#
// convention for #6370. A Format-A structural ref was tried first, on the
// reasoning that an interface is usually declared in the same file and
// `type_graph.go` already addresses one that way for field types. Measured on
// the golden fixture it bound for exactly one of six edges: the YAML rule pack
// at internal/engine/rules/graphql/frameworks/graphql_schema.yaml mints its
// OWN `Interface` entity for every `interface X {` and its own `Model` for
// every `type X {`, so most in-file interfaces are already two nodes and the
// structural ref resolves AMBIGUOUS rather than binding. The bare name binds
// the SAME one edge (Named, the single interface the pack's
// `interface\s+(\w+)\s*\{` does not match, because its header carries an
// `implements` clause) — measured, not assumed — so nothing is lost by
// preferring it, and it leaves ONE address dialect instead of two.
// TestGraphQLImplementsToIDIsAlwaysTheBareWrittenName fails if a second one is
// reintroduced. The five stranded edges are a pipeline defect, not this one's:
// resolution runs BEFORE the class-shadow fold that merges the duplicate pair,
// so it sees two candidates for a node that ends up singular. Recorded in
// internal/quality/golden/graphql-schema-mini/expected.json, whose five
// to_bare_name rows fail the day it is fixed.
//
// A definition naming ITSELF yields no edge: a self-edge is never information
// and is the signature of a mis-attributed owner (#6369).
func implementsEdges(src, owner string, nameEnd, startLine int) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	seen := make(map[string]bool)
	for _, target := range implementsTargets(declarationHeader(src, nameEnd)) {
		if target == owner || seen[target] {
			continue
		}
		seen[target] = true
		out = append(out, types.RelationshipRecord{
			// FromID intentionally empty — see the doc comment above.
			ToID: target,
			Kind: "IMPLEMENTS",
			Properties: types.Props{
				{K: "line", V: strconv.Itoa(startLine)},
				{K: "provenance", V: "sdl_implements"},
			},
		})
	}
	return out
}
