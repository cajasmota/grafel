package graphql_test

// #6370 — GraphQL `implements` → IMPLEMENTS edges.
//
// Coverage map. Every test below names the ONE axis it varies; everything else
// in its source is held constant against the shape used by
// TestGraphQLImplementsSingleInterfaceEmitsOneEdge, which is the baseline.
//
//	owner subtype        type / interface / enum / input          (4, 5, 12)
//	clause arity         one / three / duplicate / none           (1, 2, 3, 13)
//	clause syntax        `&` / leading `&` / newline-continued /
//	                     missing `&` / glued keyword /
//	                     trailing directive                       (2, 6, 7, 8, 9)
//	target addressing    declared interface / declared type /
//	                     undeclared — one dialect for all three   (14)
//	decoys               field named `implements` / header `#` /
//	                     leading description / comment-only line  (17, 18, 19, 20)
//	bounds               unindented next line / block description (10, 21)
//	anchoring            empty FromID / no duplicate entity       (22, 23)
//	provenance           line + provenance properties             (11)

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// implementsByOwner groups every IMPLEMENTS edge by the NAME of the entity it
// is embedded on. Grouping by owner rather than flattening is deliberate: an
// edge on the wrong record is the #6369 defect, and a flat list cannot see it.
func implementsByOwner(entities []types.EntityRecord) map[string][]types.RelationshipRecord {
	out := map[string][]types.RelationshipRecord{}
	for _, e := range entities {
		for _, r := range e.Relationships {
			if r.Kind == "IMPLEMENTS" {
				out[e.Name] = append(out[e.Name], r)
			}
		}
	}
	return out
}

// implementsToIDs returns the ToIDs of the IMPLEMENTS edges on one owner, in
// emission order.
func implementsToIDs(entities []types.EntityRecord, owner string) []string {
	var out []string
	for _, r := range implementsByOwner(entities)[owner] {
		out = append(out, r.ToID)
	}
	return out
}

func totalImplements(entities []types.EntityRecord) int {
	n := 0
	for _, rs := range implementsByOwner(entities) {
		n += len(rs)
	}
	return n
}

// baselineSchema is the shape every test below varies exactly one thing from.
const baselineSchema = `interface Node {
  id: ID!
}

type User implements Node {
  id: ID!
  name: String!
}
`

// ---- 1. the edge exists at all ---------------------------------------------

// Varies: the presence of an `implements` clause. Holds: two definitions, one
// interface target, everything else.
func TestGraphQLImplementsSingleInterfaceEmitsOneEdge(t *testing.T) {
	ents := extractGQL(t, "schema.graphql", baselineSchema)
	got := implementsToIDs(ents, "User")
	want := []string{"Node"}
	if !equalStrings(got, want) {
		t.Fatalf("User IMPLEMENTS ToIDs = %v, want %v", got, want)
	}
	if n := totalImplements(ents); n != 1 {
		t.Fatalf("total IMPLEMENTS = %d, want 1 — the edge must sit on User and nowhere else", n)
	}
}

// ---- 2. arity and separator -------------------------------------------------

// Varies: the number of targets in one clause (1 → 3). Holds: owner subtype,
// separator style, target declaredness.
func TestGraphQLImplementsThreeInterfacesViaAmpersandEmitsThreeEdgesInOrder(t *testing.T) {
	src := `interface Node { id: ID! }
interface Timestamped { createdAt: String! }
interface Named { name: String! }

type User implements Node & Timestamped & Named {
  id: ID!
}
`
	ents := extractGQL(t, "s.graphql", src)
	got := implementsToIDs(ents, "User")
	want := []string{"Node", "Timestamped", "Named"}
	if !equalStrings(got, want) {
		t.Fatalf("ToIDs = %v, want %v", got, want)
	}
}

// Varies: a leading `&` before the first target — legal SDL that a naive
// `strings.Split` would turn into an empty first name. Holds: arity 2.
func TestGraphQLImplementsLeadingAmpersandIsNotATarget(t *testing.T) {
	src := `interface Node { id: ID! }
interface Named { name: String! }

type User implements & Node & Named {
  id: ID!
}
`
	ents := extractGQL(t, "s.graphql", src)
	got := implementsToIDs(ents, "User")
	want := []string{"Node", "Named"}
	if !equalStrings(got, want) {
		t.Fatalf("ToIDs = %v, want %v", got, want)
	}
}

// Varies: the same target named twice. Holds: everything else.
func TestGraphQLImplementsDuplicateTargetEmitsOneEdge(t *testing.T) {
	src := `interface Node { id: ID! }

type User implements Node & Node {
  id: ID!
}
`
	ents := extractGQL(t, "s.graphql", src)
	if got := implementsToIDs(ents, "User"); len(got) != 1 {
		t.Fatalf("ToIDs = %v, want exactly one", got)
	}
}

// ---- 3. owner subtype -------------------------------------------------------

// Varies: the owner's subtype (`type` → `interface`). Holds: clause shape.
// SDL lets an interface implement an interface; the edge kind does not change
// with the owner, which is why there is no kind ladder in hierarchy.go.
func TestGraphQLImplementsInterfaceImplementingInterfaceEmitsSameKind(t *testing.T) {
	src := `interface Node { id: ID! }

interface Named implements Node {
  id: ID!
  name: String!
}
`
	ents := extractGQL(t, "s.graphql", src)
	rels := implementsByOwner(ents)["Named"]
	if len(rels) != 1 {
		t.Fatalf("Named IMPLEMENTS = %v, want exactly one", rels)
	}
	if rels[0].Kind != "IMPLEMENTS" {
		t.Fatalf("kind = %q, want IMPLEMENTS", rels[0].Kind)
	}
	if rels[0].ToID != "Node" {
		t.Fatalf("ToID = %q, want %q", rels[0].ToID, "Node")
	}
}

// Varies: the owner's subtype to one that CANNOT carry the clause. Holds: the
// clause text. `enum X implements Y` is not SDL; the header is never offered
// to a subtype that cannot declare one, so nothing is emitted. This is a
// deliberate decision, not an accident of the header bound: the call site in
// graphql.go gates on subtype before reading a single byte of header.
func TestGraphQLImplementsOnEnumAndInputEmitsNothing(t *testing.T) {
	for _, subtype := range []string{"enum", "input", "union", "scalar"} {
		t.Run(subtype, func(t *testing.T) {
			src := fmt.Sprintf(`interface Node { id: ID! }

%s Weird implements Node {
  id: ID!
}
`, subtype)
			ents := extractGQL(t, "s.graphql", src)
			if n := totalImplements(ents); n != 0 {
				t.Fatalf("%s owner emitted %d IMPLEMENTS edges, want 0: %v",
					subtype, n, implementsByOwner(ents))
			}
		})
	}
}

// ---- 4. clause syntax edge cases -------------------------------------------

// Varies: the clause spanning lines, with each continuation INDENTED. Holds:
// arity 2, target declaredness.
func TestGraphQLImplementsNewlineContinuedClauseIsRead(t *testing.T) {
	src := `interface Node { id: ID! }
interface Named { name: String! }

type User implements Node
    & Named
{
  id: ID!
}
`
	ents := extractGQL(t, "s.graphql", src)
	got := implementsToIDs(ents, "User")
	want := []string{"Node", "Named"}
	if !equalStrings(got, want) {
		t.Fatalf("ToIDs = %v, want %v", got, want)
	}
}

// Varies: a missing `&` between two names — invalid SDL. Holds: everything
// else. The segment is not a single GraphQL Name, so it is DROPPED: neither
// name is guessed at, and no target called "Node Named" is invented.
func TestGraphQLImplementsMissingAmpersandEmitsNothing(t *testing.T) {
	src := `interface Node { id: ID! }
interface Named { name: String! }

type User implements Node Named {
  id: ID!
}
`
	ents := extractGQL(t, "s.graphql", src)
	if n := totalImplements(ents); n != 0 {
		t.Fatalf("got %d IMPLEMENTS edges from an unseparated list, want 0: %v",
			n, implementsByOwner(ents))
	}
}

// Varies: the keyword glued to the first target name. Holds: everything else.
// `implementsNode` is one identifier, not a clause; without the word boundary
// on the keyword pattern this over-fires with a target of "Node".
func TestGraphQLImplementsKeywordMustBeAWholeWord(t *testing.T) {
	src := `interface Node { id: ID! }

type User implementsNode {
  id: ID!
}
`
	ents := extractGQL(t, "s.graphql", src)
	if n := totalImplements(ents); n != 0 {
		t.Fatalf("`implementsNode` was read as a clause and emitted %d edges: %v",
			n, implementsByOwner(ents))
	}
}

// Varies: a directive after the clause. Holds: arity 1. The `@` ends the
// header, so the directive's own arguments — which may contain any text at
// all — are never scanned for a second `implements`.
func TestGraphQLImplementsClauseFollowedByDirectiveIsRead(t *testing.T) {
	src := `interface Node { id: ID! }

type User implements Node @key(fields: "id implements Named") {
  id: ID!
}
`
	ents := extractGQL(t, "s.graphql", src)
	got := implementsToIDs(ents, "User")
	want := []string{"Node"}
	if !equalStrings(got, want) {
		t.Fatalf("ToIDs = %v, want %v — the @ must end the header", got, want)
	}
}

// Varies: the owner naming itself. Holds: everything else. A self-edge is
// never information and is the signature of a mis-attributed owner (#6369).
func TestGraphQLImplementsSelfReferenceEmitsNoEdge(t *testing.T) {
	src := `interface Node implements Node {
  id: ID!
}
`
	ents := extractGQL(t, "s.graphql", src)
	if n := totalImplements(ents); n != 0 {
		t.Fatalf("self-implements emitted %d edges, want 0", n)
	}
}

// ---- 5. bounds --------------------------------------------------------------

// Varies: what sits on the column-0 line after an incomplete header. Holds:
// the incomplete header itself.
//
// Both sub-cases emit nothing, and they are excluded by DIFFERENT halves of
// the implementation — which is the point of running both. `bare_name` is the
// one the unindented-line stop actually earns: without it the scan reaches
// `Node` and emits User -> Node. `keyword_pair` would be excluded even with
// the stop deleted, because `type Post` is two words and the Name check in
// implementsTargets discards it; it is kept as the negative control that
// proves the first sub-case is carrying the assertion. An earlier version of
// this test had only `keyword_pair`, and a mutant deleting the stop survived
// it while the test's name went on claiming the stop was pinned.
func TestGraphQLImplementsHeaderStopsAtUnindentedLine(t *testing.T) {
	cases := map[string]string{
		"bare_name": `type User implements
Node {
  id: ID!
}
`,
		"keyword_pair": `type User implements
type Post {
  id: ID!
}
`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			ents := extractGQL(t, "s.graphql", src)
			if n := totalImplements(ents); n != 0 {
				t.Fatalf("header ran past the unindented line and emitted %d edges: %v",
					n, implementsByOwner(ents))
			}
		})
	}
}

// ---- 6. decoys --------------------------------------------------------------

// Varies: a FIELD named `implements`. Holds: the owner has no clause.
//
// This is excluded because the header bound stops at the body's `{`, so the
// field line is never read — not because the extractor knows what a field is.
func TestGraphQLImplementsFieldNamedImplementsIsNotAClause(t *testing.T) {
	src := `interface Node { id: ID! }

type Settings {
  theme: String!
  implements: String
}
`
	ents := extractGQL(t, "s.graphql", src)
	if n := totalImplements(ents); n != 0 {
		t.Fatalf("a field named `implements` produced %d edges: %v", n, implementsByOwner(ents))
	}
}

// Varies: a `#` comment on the declaration's own header line. Holds: the owner
// declares no interface.
//
// Excluded by the `#` stop in the header bound. That is a stop, NOT comment
// awareness: this extractor has none anywhere, and a `#` inside a string
// elsewhere would end a header just the same.
func TestGraphQLImplementsHeaderCommentIsNotAClause(t *testing.T) {
	src := `interface Node { id: ID! }

type Settings # implements Node
{
  theme: String!
}
`
	ents := extractGQL(t, "s.graphql", src)
	if n := totalImplements(ents); n != 0 {
		t.Fatalf("a header comment produced %d edges: %v", n, implementsByOwner(ents))
	}
}

// Varies: a whole-line `#` comment that spells a complete declaration. Holds:
// the file's real definitions.
//
// Excluded one level up, by typeDefRE's `(?m)^(type|…)` anchor: the line
// starts with `#`, so no definition is recognised and no owner exists to hang
// an edge on. Asserted on the ENTITY set as well as the edge count, because
// "no edge" would also be true if the ghost type had been extracted and simply
// had its clause dropped.
func TestGraphQLImplementsCommentedOutDeclarationYieldsNoTypeAndNoEdge(t *testing.T) {
	src := `interface Node { id: ID! }

# type Ghost implements Node {
#   id: ID!
# }

type User {
  id: ID!
}
`
	ents := extractGQL(t, "s.graphql", src)
	if n := totalImplements(ents); n != 0 {
		t.Fatalf("a commented-out declaration produced %d edges: %v", n, implementsByOwner(ents))
	}
	for _, e := range ents {
		if e.Name == "Ghost" {
			t.Fatalf("a commented-out declaration was extracted as an entity: %+v", e)
		}
	}
}

// Varies: a `"""` block description ABOVE the declaration whose prose contains
// the word. Holds: the declaration itself has no clause.
//
// Excluded because the header starts AFTER the declared name, so nothing
// preceding the declaration is ever read.
func TestGraphQLImplementsWordInLeadingDescriptionIsNotAClause(t *testing.T) {
	src := `interface Node { id: ID! }

"""
Settings implements nothing at all; this sentence is prose.
"""
type Settings {
  theme: String!
}
`
	ents := extractGQL(t, "s.graphql", src)
	if n := totalImplements(ents); n != 0 {
		t.Fatalf("a leading description produced %d edges: %v", n, implementsByOwner(ents))
	}
}

// KNOWN DIVERGENCE, asserted on purpose so it cannot be fixed silently.
//
// A LINE-INITIAL `type X implements Y {` inside a `"""` block description
// over-fires. This extractor has no string awareness anywhere: typeDefRE
// already extracts the quoted text as a bogus SCOPE.Schema ENTITY today, and
// #6370 adds an edge to that pre-existing defect rather than creating it.
//
// The assertion is written in the WRONG direction deliberately. Whoever
// teaches typeDefRE about `"""` blocks will be told by this failing test that
// they have also fixed the edge half — which is the outcome the erlang round
// of #6370 found the suite could not deliver.
func TestGraphQLImplementsInsideBlockDescriptionOverFires_KnownDivergence(t *testing.T) {
	src := `interface Node { id: ID! }

"""
An example, quoted in prose:

type Quoted implements Node {
  id: ID!
}
"""
type Real {
  id: ID!
}
`
	ents := extractGQL(t, "s.graphql", src)
	got := implementsToIDs(ents, "Quoted")
	want := []string{"Node"}
	if !equalStrings(got, want) {
		t.Fatalf("KNOWN DIVERGENCE CHANGED: expected the documented false-positive edge "+
			"Quoted -> Node from inside a \"\"\" block description, got %v.\n"+
			"If you taught this extractor about block strings, that is a FIX: delete this "+
			"test and say so — do not re-pin the new behaviour here.", got)
	}
}

// KNOWN DIVERGENCE, asserted on purpose. `extend type Foo implements Bar` is
// legal SDL, and it emits NO hierarchy edge: extendTypeRE has its own loop and
// its own IMPORTS/FEDERATES contract, and typeDefRE's `(?m)^(type|…)` anchor
// does not match a line beginning `extend`. Widening #6370 to the extend loop
// is a separate decision about which node owns the edge (the import stub or
// the extended type), and this test is what tells whoever makes it that they
// have changed something.
func TestGraphQLImplementsOnExtendTypeEmitsNothing_KnownDivergence(t *testing.T) {
	src := `interface Node { id: ID! }

extend type User implements Node {
  id: ID!
}
`
	ents := extractGQL(t, "s.graphql", src)
	if n := totalImplements(ents); n != 0 {
		t.Fatalf("KNOWN DIVERGENCE CHANGED: `extend type … implements` emitted %d edges, "+
			"and this test recorded that it emits none. If you extended #6370 to the "+
			"extend loop, that is a FIX: replace this test with the real assertion.\n%v",
			n, implementsByOwner(ents))
	}
}

// ---- 7. target addressing ---------------------------------------------------

// Varies: whether the target is declared in this file, and as what. Holds: the
// clause and the owner.
//
// All three get the SAME address — the bare written name. There is ONE ToID
// dialect, deliberately: a Format-A structural ref was measured first and bound
// for one of the golden fixture's six edges, because the YAML rule pack
// (internal/engine/rules/graphql/frameworks/graphql_schema.yaml) mints a second
// node for most in-file interfaces and the ref resolves ambiguous. This test is
// what fails if a later author reintroduces the second dialect.
func TestGraphQLImplementsToIDIsAlwaysTheBareWrittenName(t *testing.T) {
	src := `interface Declared { id: ID! }
type NotAnInterface { id: ID! }

type User implements Declared & NotAnInterface & Absent {
  id: ID!
}
`
	ents := extractGQL(t, "s.graphql", src)
	got := implementsToIDs(ents, "User")
	// Declared: an interface in this file. NotAnInterface: declared here but
	// not as an interface (implementing it is not SDL). Absent: not declared
	// here at all — another schema file, or another subgraph.
	want := []string{"Declared", "NotAnInterface", "Absent"}
	if !equalStrings(got, want) {
		t.Fatalf("ToIDs = %v, want %v — one dialect, the bare written name", got, want)
	}
	for _, id := range got {
		if strings.Contains(id, ":") {
			t.Errorf("ToID %q is a structural ref; #6370 emits bare names for graphql", id)
		}
	}
}

// ---- 8. anchoring -----------------------------------------------------------

// Varies: nothing — this is a property of every IMPLEMENTS edge the extractor
// can emit, so the source deliberately mixes owners and target dialects.
//
// FromID must be EMPTY so the assembly loop stamps the OWNING TYPE's entity id.
// A file path here would be rewritten onto the FILE entity by
// ReferencesEmbedded, merging every type in a schema file onto one node
// (#6295, #6298).
func TestGraphQLImplementsFromIDIsAlwaysEmpty(t *testing.T) {
	src := `interface Node { id: ID! }
interface Named implements Node { name: String! }

type User implements Node & Named & Elsewhere {
  id: ID!
}
`
	ents := extractGQL(t, "s.graphql", src)
	if n := totalImplements(ents); n != 4 {
		t.Fatalf("total IMPLEMENTS = %d, want 4 (Named→Node, User→Node/Named/Elsewhere)", n)
	}
	for owner, rels := range implementsByOwner(ents) {
		for _, r := range rels {
			if r.FromID != "" {
				t.Errorf("%s IMPLEMENTS %s: FromID = %q, want empty so assembly anchors "+
					"the edge on the type rather than the file", owner, r.ToID, r.FromID)
			}
		}
	}
}

// Varies: nothing. Emitting from the language extractor rather than registering
// graphql in cross/hierarchy exists precisely so no duplicate node is minted
// for the type OR for its targets. Both halves are asserted: every extracted
// name appears exactly once, and the undeclared target `Elsewhere` gets no node
// of its own at all.
func TestGraphQLImplements_NoDuplicateEntities(t *testing.T) {
	src := `interface Node { id: ID! }

type User implements Node & Elsewhere {
  id: ID!
}
`
	ents := extractGQL(t, "s.graphql", src)
	count := map[string]int{}
	for _, e := range ents {
		count[e.Kind+" "+e.Name]++
	}
	var dupes []string
	for k, n := range count {
		if n > 1 {
			dupes = append(dupes, fmt.Sprintf("%s ×%d", k, n))
		}
	}
	sort.Strings(dupes)
	if len(dupes) != 0 {
		t.Errorf("duplicate entities: %s", strings.Join(dupes, ", "))
	}
	for _, e := range ents {
		if e.Name == "Elsewhere" {
			t.Errorf("an undeclared IMPLEMENTS target was minted as an entity: %+v", e)
		}
	}
}

// ---- 9. provenance ----------------------------------------------------------

// Varies: nothing. Pins the two properties the edge carries, including the
// 1-based line of the DECLARATION (not of the file, and not of the target).
func TestGraphQLImplementsCarriesLineAndProvenance(t *testing.T) {
	src := `interface Node { id: ID! }


type User implements Node {
  id: ID!
}
`
	ents := extractGQL(t, "s.graphql", src)
	rels := implementsByOwner(ents)["User"]
	if len(rels) != 1 {
		t.Fatalf("User IMPLEMENTS = %v, want one", rels)
	}
	props := map[string]string{}
	for _, p := range rels[0].Properties {
		props[p.K] = p.V
	}
	if props["line"] != "4" {
		t.Errorf("line = %q, want \"4\" (the `type User` line)", props["line"])
	}
	if props["provenance"] != "sdl_implements" {
		t.Errorf("provenance = %q, want \"sdl_implements\"", props["provenance"])
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
