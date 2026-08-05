// Package csharp — test-double extractor for C# (issue #5005).
//
// Spun out of #4968 (the WCF service-contract subset delivered the RPC slice).
// This extractor covers the .NET test-double surface so the graph records what
// a unit/integration test stands in for and what infrastructure it depends on:
//
//	Mock-binding (Moq / NSubstitute):
//	  new Mock<T>()            (Moq)         -> SCOPE.Pattern/test_double, the
//	  Substitute.For<T>()      (NSubstitute)    test USES the mocked type T
//	  (mock_type:<T>); USES edge -> type:<T>. The mock node carries
//	  library=moq|nsubstitute and target=<T>.
//
//	Container topology (Testcontainers):
//	  new XxxContainer()       /  new ContainerBuilder().WithImage("img")
//	  -> SCOPE.Pattern/container_topology; the ENCLOSING TEST TYPE
//	  DEPENDS_ON_SERVICE the container'd service (#6144 — until then the edge
//	  hung off the container node, so it ran container:X -> service X with both
//	  endpoints from one token and the test nowhere in it; the edge now lands on
//	  the base extractor's class node via the #6104 Tier A identity merge, and
//	  falls back to the container node only when no enclosing type is found),
//	  addressed by the canonical external-service ref
//	  (scope:externalservice:<image-or-container>, #6123 — no service node is
//	  fabricated, so the edge dangles honestly unless one already exists, and the
//	  ref is colon-bounded by containerServiceRef so it can never reach the
//	  resolver's six-segment structural form). The node carries
//	  image=<docker-image> when expressed via .WithImage("…") and
//	  container_type=<XxxContainer> for the typed-builder forms.
//
//	BDD step definitions (SpecFlow / Reqnroll):
//	  [Binding] classes with [Given]/[When]/[Then]/[StepDefinition] methods
//	  -> SCOPE.Pattern/step_definition (one per step, carrying the step text).
//
//	Test-data builders (Bogus / AutoFixture) — issue #5071:
//	  new Faker<T>().RuleFor(x => x.Name, …)  (Bogus)   -> SCOPE.Pattern/
//	  fixture.Create<T>() / fixture.Build<T>() (AutoFixture)   test_data_builder
//	  for the built type T; USES edge -> type:<T>. Bogus builders additionally
//	  carry the faked field list (fields=Name,Email,…) harvested from the
//	  .RuleFor(x => x.Field, …) chain. The node carries library=bogus|autofixture
//	  and target=<T>.
//
//	Mock-target -> DI-impl resolution — issue #5071:
//	  when a mock is wired into production code — registered into a DI container
//	  (services.AddSingleton(mock.Object)) or passed to a system-under-test
//	  constructor (new Sut(mock.Object)) — the mocked interface is resolved to
//	  its concrete implementation by the dotnet_di naming convention (strip the
//	  leading `I`, e.g. IOrderRepository -> OrderRepository) and a RESOLVES_TO
//	  edge is emitted -> impl:<Impl>, the same node the dotnet_di extractor lands
//	  as its BINDS target. This stitches the test-double surface to the
//	  production DI graph. Honest-partial: the resolution is by-name only (the
//	  mock and the registration usually live in the same test file, but the impl
//	  class lives elsewhere); custom factory registrations are not resolved.
//
// All of these reuse existing entity Kinds (SCOPE.Pattern) and edge kinds
// (USES, DEPENDS_ON_SERVICE, RESOLVES_TO) — no new Kind is introduced.
//
// Registration key: "custom_csharp_test_doubles"
// Issues #5005, #5071.
package csharp

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("custom_csharp_test_doubles", &testDoublesExtractor{})
}

type testDoublesExtractor struct{}

func (e *testDoublesExtractor) Language() string { return "custom_csharp_test_doubles" }

// ---------------------------------------------------------------------------
// Regex catalog
// ---------------------------------------------------------------------------

var (
	// Moq: new Mock<IFoo>()  — captures the mocked type (first generic arg,
	// leaf token without trailing generics/namespace).
	reMoqNew = regexp.MustCompile(`\bnew\s+Mock\s*<\s*([\w.]+)\s*>`)

	// Moq: Mock.Of<IFoo>() — the loose-mock factory form.
	reMoqOf = regexp.MustCompile(`\bMock\.Of\s*<\s*([\w.]+)\s*>`)

	// NSubstitute: Substitute.For<IFoo>() — captures the first type arg.
	reNSubstituteFor = regexp.MustCompile(`\bSubstitute\.For\s*<\s*([\w.,\s]+?)\s*>`)

	// Testcontainers typed builders: new PostgreSqlContainer(), new
	// RedisContainer(), new MsSqlContainer(), etc. The "…Container" suffix is
	// the Testcontainers module convention. We exclude the generic
	// ContainerBuilder (handled by the .WithImage form below).
	reTcTypedContainer = regexp.MustCompile(`\bnew\s+(\w+Container)\s*\(`)

	// Testcontainers builder image binding: .WithImage("postgres:16") on a
	// ContainerBuilder / builder chain. Captures the docker image string.
	reTcWithImage = regexp.MustCompile(`\.WithImage\s*\(\s*"([^"]+)"\s*\)`)

	// SpecFlow / Reqnroll [Binding] class — gates BDD step extraction.
	reBindingAttr = regexp.MustCompile(`\[Binding\b[^\]]*\]`)

	// SpecFlow / Reqnroll step attribute: [Given("…")], [When(@"…")],
	// [Then("…")], [StepDefinition("…")]. Captures the step keyword + text.
	reStepAttr = regexp.MustCompile(`\[(Given|When|Then|StepDefinition)\s*\(\s*@?"([^"]*)"`)

	// Bogus: new Faker<Customer>() — captures the built type. The RuleFor
	// chain that follows is harvested separately for the faked field list.
	reBogusFaker = regexp.MustCompile(`\bnew\s+Faker\s*<\s*([\w.]+)\s*>`)

	// Bogus: .RuleFor(x => x.Name, …) — captures the faked property name.
	reBogusRuleFor = regexp.MustCompile(`\.RuleFor\s*\(\s*\w+\s*=>\s*\w+\.(\w+)`)

	// AutoFixture: fixture.Create<Customer>() — the one-shot factory form.
	reAutoFixtureCreate = regexp.MustCompile(`\.Create\s*<\s*([\w.]+)\s*>\s*\(`)

	// AutoFixture: fixture.Build<Customer>() — the customisable-builder form
	// (typically followed by .With(...).Create()).
	reAutoFixtureBuild = regexp.MustCompile(`\.Build\s*<\s*([\w.]+)\s*>\s*\(`)

	// Mock-target -> DI registration / SUT wiring. Captures the mock variable
	// whose .Object is registered into a DI container or passed to a ctor:
	//   services.AddSingleton(repoMock.Object)  /  new Sut(repoMock.Object)
	// Group 1 = the mock variable name (matched against new Mock<T> bindings).
	reMockObjectUse = regexp.MustCompile(`\b(\w+)\.Object\b`)

	// new Mock<T>() assigned to a variable: var repo = new Mock<IFoo>();
	// Group 1 = variable, group 2 = mocked type. Used to resolve .Object uses
	// back to the interface they double.
	reMoqVarBinding = regexp.MustCompile(`\b(\w+)\s*=\s*new\s+Mock\s*<\s*([\w.]+)\s*>`)

	// #6144 — enclosing CLASS declaration, used to attach the container's
	// DEPENDS_ON_SERVICE edge to the test class rather than to the container node.
	//
	// PLAIN `class` ONLY, DELIBERATELY. An earlier revision matched the four
	// declaration forms the base extractor turns into SCOPE.Component entities
	// (csharp.go:153 — class/interface/struct/record_declaration). That was wrong
	// twice over, and adversarial review caught the first:
	//
	//  1. KEYWORD ORDER. A `(?:class|record|struct|interface)` alternation matches
	//     `record` FIRST in `record struct Row(int Id)`, capturing the NEXT word —
	//     "struct" — as the type name. Positional records beside a test class are
	//     ordinary modern C#, and the consequence is severe: SCOPE.Component/
	//     class/"struct" matches no base entity, survives the #6104 merge as a
	//     standalone Tier C node, and carries the edge. That is exactly the node
	//     fabrication this function's contract promises never happens.
	//
	//  2. SUBTYPE. Reordering the alternation fixes the capture but not the
	//     identity. The facet claims Subtype "class"; the base extractor maps
	//     record_declaration to "type" and struct_declaration to "struct". Tier A
	//     keys on (SourceFile, Kind, Name) and lets the CUSTOM value win on
	//     conflict, so a facet for a record would silently displace the base
	//     node's Subtype — corrupting an entity to attach an edge to it.
	//
	// Matching plain `class` alone removes both. A Testcontainers fixture is
	// constructed in a class, never in a record or an interface; `class` is the
	// one form whose base Subtype is unambiguously "class", so the facet can
	// never displace anything; and a `record struct` declared INSIDE a test class
	// no longer shadows it, because the nearest preceding `class` is still the
	// test class — which is the correct attribution anyway. `record class X` is
	// deliberately NOT matched (its base Subtype is "type"): that falls back to
	// the container node, which is honest rather than corrupting.
	reCSharpClassDecl = regexp.MustCompile(`(?m)^[\t ]*(?:\[[^\]]*\][\t ]*)*(?:(?:public|internal|private|protected|static|sealed|abstract|partial|file)\s+)*class\s+(\w+)`)
)

// enclosingCSharpClass returns the name of the nearest plain `class`
// declaration that starts at or before offset, or "" when there is none.
//
// #6144 — WHY THIS EXISTS AND WHAT IT IS FOR. The container's
// DEPENDS_ON_SERVICE edge used to hang off the container node itself, so the
// emitted edge was `container:PostgreSqlContainer -> service PostgreSqlContainer`:
// both endpoints derived from a single token, and the TEST — the thing this
// file's package doc says depends on the service — appeared nowhere in it.
// Attaching the edge to the enclosing test type instead makes it say something
// a caller cannot already read off the container node's properties: "this test
// suite needs a Postgres".
//
// HOW THE EDGE REACHES THE TEST CLASS WITHOUT FABRICATING A NODE. The FROM
// endpoint of a relationship is whichever EntityRecord carries it, and this
// extractor does not emit test classes. It does not need to: MergeWithCustom's
// Tier A (issue #6104, extractors/custom_dispatch.go) COMBINES a custom record
// with a base record of the same identity — (SourceFile, Kind, Name) — into one
// survivor, carrying the custom record's relationships onto it. The base csharp
// extractor emits every class as Kind="SCOPE.Component", Subtype="class", Name=
// the class name, in this same file. So emitting a minimal facet with exactly
// that identity lands the edge on the real class node rather than creating a
// second one. The facet sets no field the base does not already set to the same
// value, and its span is a single line at the declaration, so the Tier A span
// union (min non-zero start, max end) cannot narrow the base class's span.
//
// HONEST-PARTIAL, AND WHERE IT STOPS. The scan is lexical and takes the NEAREST
// PRECEDING class declaration; it does not track braces, so a container
// constructed inside a nested class is attributed to that nested class (correct)
// but one constructed after a nested class has closed is attributed to the
// nested class too (wrong). Both are still a class IN THE SAME FILE, so the edge
// remains vastly more informative than the tautology it replaces, and when no
// class declaration precedes the match at all the caller keeps the edge on the
// container node rather than dropping it — a relationship the extractor meant
// to express is never silently lost (#6123). Restricting to `class` also means
// a record/struct/interface declared INSIDE a test class cannot shadow it; see
// reCSharpClassDecl for why that restriction is load-bearing and not merely
// convenient.
//
// Returns the declaration's byte offset alongside the name so the merge facet
// can be stamped with the class's own declaration line rather than the
// container's — a facet line inside the class body would drag the Tier A span
// union's StartLine down below the real declaration.
func enclosingCSharpClass(src string, offset int) (string, int) {
	if offset <= 0 || offset > len(src) {
		return "", 0
	}
	name, at := "", 0
	for _, m := range reCSharpClassDecl.FindAllStringSubmatchIndex(src[:offset], -1) {
		name, at = src[m[2]:m[3]], m[2]
	}
	return name, at
}

// reLeafType strips a dotted/namespaced C# type to its leaf token, e.g.
// "Acme.Domain.IFoo" -> "IFoo". Used so mock targets match the type entities
// the base extractor emits.
var reLeafType = regexp.MustCompile(`([\w]+)$`)

func leafCSharpType(t string) string {
	if m := reLeafType.FindStringSubmatch(t); m != nil {
		return m[1]
	}
	return t
}

// containerServiceRefMaxColons is the most colons a container name may carry
// before `scope:externalservice:<name>` reaches the resolver's six-segment
// structural-ref threshold (stubScopeSegments, internal/resolve/refs.go:2037-2039).
// The prefix contributes two segments, so three colons in the name is the point
// where the stub stops being REJECTED and starts being PARSED as Format A.
const containerServiceRefMaxColons = 2

// containerServiceRef returns the external-service ref for a container name, or
// "" when no ref can be minted without risking a mis-bind.
//
// WHY THIS EXISTS. #6123 repaired this extractor's DEPENDS_ON_SERVICE target by
// switching it from the unaddressable Name `service:<X>` to the canonical
// `scope:externalservice:<X>` ref, whose safety rests on lookupStructural
// rejecting any `scope:` stub that is not six segments. Adversarial review found
// that the repaired shape reintroduces the same defect class at three colons:
// once the stub IS six segments it is no longer rejected, and Format A reads
// parts[4] as a FILE PATH and parts[5] as an entity NAME. Measured against a live
// index, `scope:externalservice:registry.io:5000/app/pg:15@sha256:deadbeef` binds
// to an entity named "deadbeef" in a file named "15@sha256". A registry port plus
// tag plus digest is a legal docker reference, and reTcWithImage (:96) captures an
// arbitrary unvalidated string literal, so the token cannot be assumed safe.
//
// Two steps, which fail on different inputs:
//
//  1. Drop the content digest (`@sha256:…`). A digest pins a BUILD, not a service
//     identity, so it has no place in a convergence ref — and it is the third
//     colon in every legal reference (registry port + tag + digest is the maximum;
//     tags and repository paths cannot themselves contain colons). After this
//     step every well-formed reference is at most two colons, i.e. safe. The raw
//     reference is still recorded verbatim in the `image` property on both the
//     container node and the edge, so nothing a caller could act on is lost.
//
//  2. Refuse anything still above the ceiling. Unreachable from a well-formed
//     reference; it exists because the capture is unvalidated. Refusing means NO
//     DEPENDS_ON_SERVICE edge for that container — deliberately, and narrowly.
//     The general principle that dropping an extractor-intended relationship is
//     worse than a dangle does not apply here, because the alternative is not a
//     dangle: it is a ref that PARSES, and therefore mis-binds. The container
//     node and its properties are still emitted; only the unnameable service
//     reference is withheld.
func containerServiceRef(name string) string {
	if at := strings.IndexByte(name, '@'); at > 0 {
		name = name[:at]
	}
	if strings.Count(name, ":") > containerServiceRefMaxColons {
		return ""
	}
	return extractor.ExternalServiceTargetID(name)
}

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *testDoublesExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("grafel/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.test_doubles_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "test_doubles"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "csharp" {
		return nil, nil
	}
	src := string(file.Content)

	// Cheap gate: only fire on files that mention one of the supported
	// test-double surfaces.
	if !regexpAny(src, "Mock<", "Mock.Of", "Substitute.For", "Container", ".WithImage", "[Binding",
		"Faker<", "Create<", "Build<") {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// -------------------------------------------------------------------------
	// Mock-binding — Moq / NSubstitute. The test USES the mocked type T.
	// -------------------------------------------------------------------------
	// mockEnts indexes the emitted mock node per target type so the DI/SUT
	// resolution pass can attach a RESOLVES_TO edge to the same node.
	mockEnts := make(map[string]int) // target -> index in entities
	emitMock := func(target, library string, line int) {
		target = leafCSharpType(target)
		if target == "" || csharpPrimitives[target] {
			return
		}
		ent := makeEntity("mock:"+library+":"+target, "SCOPE.Pattern", "test_double",
			file.Path, "csharp", line)
		setProps(&ent, "framework", "test_doubles", "library", library,
			"target", target, "provenance", "INFERRED_FROM_MOCK_BINDING")
		ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
			ToID: "type:" + target,
			Kind: string(types.RelationshipKindUses),
			Properties: types.Props{
				{K: "framework", V: "test_doubles"},
				{K: "library", V: library},
				{K: "line", V: itoa(line)},
				{K: "role", V: "mock_binding"},
				{K: "target", V: target},
			},
		})
		before := len(entities)
		add(ent)
		if len(entities) > before {
			mockEnts[target] = len(entities) - 1
		}
	}

	for _, m := range reMoqNew.FindAllStringSubmatchIndex(src, -1) {
		emitMock(src[m[2]:m[3]], "moq", lineOf(src, m[0]))
	}
	for _, m := range reMoqOf.FindAllStringSubmatchIndex(src, -1) {
		emitMock(src[m[2]:m[3]], "moq", lineOf(src, m[0]))
	}
	for _, m := range reNSubstituteFor.FindAllStringSubmatchIndex(src, -1) {
		// NSubstitute supports multi-interface substitutes — take the first arg.
		arg := src[m[2]:m[3]]
		if idx := indexByteAny(arg, ",<"); idx >= 0 {
			arg = arg[:idx]
		}
		emitMock(arg, "nsubstitute", lineOf(src, m[0]))
	}

	// -------------------------------------------------------------------------
	// Container topology — Testcontainers. The test DEPENDS_ON_SERVICE the
	// container'd service.
	//
	// #6144 — FIXED: that sentence now describes what is emitted. It previously
	// described the FROM endpoint the extractor MEANT, not the one it produced:
	// the relationship was appended to `ent`, so the edge ran
	// container:X -> service X, both endpoints derived from one token, with the
	// test nowhere in it. Since the `image` / `container_type` payload is ALSO
	// duplicated as properties on the container node, the edge carried nothing an
	// MCP caller could not already read off the node — a tautology, not a
	// relationship.
	//
	// The FROM endpoint is now the enclosing test type (enclosingCSharpClass +
	// the #6104 Tier A identity merge, both documented there). Of the three
	// options #6144 laid out, this is (1). (2) "drop the edge" was rejected: the
	// edge is the only thing that would state a test's infrastructure dependency
	// at all, and #6123's principle — a silently dropped relationship is worse
	// than an honest dangle, and unmeasurable afterwards — applies once the edge
	// is no longer vacuous. (3) "keep it and fix the doc" was rejected as
	// documenting a tautology rather than removing one. The blocker #6144 cited
	// for (1) — "needs an enclosing-scope lookup this closure does not do" — was
	// real but small: the lookup is lexical, and the merge mechanism for landing
	// an edge on an entity another extractor owns already existed.
	//
	// #6123 — the target ref. This edge used to carry `service:<name>`, which
	// LOOKS like the Name of an external-service node (extractor.ExternalServiceName
	// mints exactly that shape) but can never address one: LookupStatusHint runs
	// splitStub (internal/resolve/refs.go:2658), which cuts at the first colon
	// and probes byName with the REMAINDER, so `service:X` probes byName["X"].
	// Real service nodes are addressed by QualifiedName — the
	// `scope:externalservice:<svc>` ref that ExternalServiceTargetID mints
	// (internal/extractor/external_service.go:102) — never by Name. The old ref
	// therefore had exactly two outcomes: dangle, or bind to some unrelated
	// entity that happened to be called X. On the #6105 fixture it took the
	// second: it bound to the base extractor's SCOPE.Operation for the
	// `new PostgreSqlContainer()` call site, via the DEPENDS_ON_SERVICE
	// operation-family hint (refs.go:1782). A mis-bind IMPROVES a dangling
	// count, so no count-based gate could see it.
	//
	// WHY NOT FABRICATE THE SERVICE NODE. Emitting an ExternalServiceEntity here
	// would make the edge resolve, and was rejected. The external-service node is
	// a corpus-wide CONVERGENCE node whose inbound DEPENDS_ON_SERVICE set is
	// documented as "the codebase's full <vendor> footprint"
	// (external_service.go:20-30), and the namespace is deliberately gated on a
	// curated SDK dictionary. The names available here are a .NET type
	// (`PostgreSqlContainer`) or a raw docker image (`postgres:15` — itself
	// colon-bearing), neither of which is a canonical service name.
	//
	// AND NORMALISING WOULD NOT HAVE HELPED. This is the decisive point, not the
	// "competing dictionary" one: even a perfect `PostgreSqlContainer` ->
	// `postgres` normalisation leaves the edge dangling, because the curated
	// dictionary (external_service.go:52-79) is Stripe / Twilio / SendGrid / AWS /
	// SaaS SDKs and contains NO DATABASES AT ALL. There is no `postgres` node to
	// bind to under any spelling. So the choice was never normalise-or-dangle; it
	// was fabricate-or-dangle, and an honest dangle wins.
	//
	// Nor does the #6131 "repair the edge rather than accept a dangle" precedent
	// transfer: there a correct binding already existed in the record set, and
	// here the only candidates are the container node itself (a self-edge) and
	// the constructor call site (the mis-bind we are removing).
	//
	// So the ref is the canonical one and the edge dangles honestly wherever no
	// service node matches. That is deliberate, and it is safe for TWO
	// independent reasons — both pinned in
	// internal/resolve/service_ref_shape_6123_test.go, because a mutation that
	// disabled the first alone was covered for by the second:
	//
	//  1. `scope:`-prefixed stubs are consumed by lookupStructural, which
	//     rejects anything that is not six segments (refs.go:2037-2039) and
	//     returns statusUnmatched WITHOUT reaching the byName / kind-hint tiers.
	//     This is the load-bearing one: an inverse mutant that changed splitStub
	//     to cut at the LAST colon left every #6123 assertion passing, so (1)
	//     alone is sufficient and the fix survives a splitStub change.
	//  2. Redundantly — and only accidentally so — splitStub cuts at the FIRST
	//     colon, so the byName probe would be "externalservice:<name>", a string
	//     no entity Name carries. The old ref's probe was the bare leaf `<name>`,
	//     which real entities do carry; that is the whole difference.
	//
	// (1) cannot block a LEGITIMATE bind, because the byQualifiedName tier runs
	// BEFORE lookupStructural (refs.go:1615): a genuine service node whose
	// QualifiedName equals this ref is matched and returned before the structural
	// tier is ever consulted. That ordering is why the guard is safe.
	//
	// The colon ceiling below is the third part, and it exists because the first
	// two are conditional on the ref NOT reaching six segments. See
	// containerServiceRef.
	// -------------------------------------------------------------------------
	// testTypeFacets indexes the #6144 merge facet emitted per enclosing test
	// type, so several containers in one class accumulate their edges on ONE
	// facet instead of colliding in `add`'s dedup and losing all but the first.
	testTypeFacets := make(map[string]int) // type name -> index in entities

	emitContainer := func(name, image, ctype string, offset, line int) {
		ent := makeEntity("container:"+name, "SCOPE.Pattern", "container_topology",
			file.Path, "csharp", line)
		props := []string{"framework", "test_doubles",
			"provenance", "INFERRED_FROM_TESTCONTAINER"}
		if image != "" {
			props = append(props, "image", image)
		}
		if ctype != "" {
			props = append(props, "container_type", ctype)
		}
		setProps(&ent, props...)

		ref := containerServiceRef(name)
		rel := types.RelationshipRecord{
			ToID: ref,
			Kind: string(types.RelationshipKindDependsOnService),
			Properties: types.Props{
				{K: "container_type", V: ctype},
				{K: "framework", V: "test_doubles"},
				{K: "image", V: image},
				{K: "line", V: itoa(line)},
				{K: "role", V: "container_topology"},
			},
		}

		// #6144 — the FROM endpoint. Prefer the enclosing test type: the edge
		// then reads "MyIntegrationTests DEPENDS_ON_SERVICE postgres", which is
		// what the package doc has always claimed and what an MCP caller cannot
		// reconstruct from the container node alone. Falling back to the
		// container node when no enclosing type is found keeps the pre-#6144
		// behaviour rather than dropping the relationship.
		owner, declAt := "", 0
		if ref != "" {
			owner, declAt = enclosingCSharpClass(src, offset)
		}
		if owner != "" {
			// Set, not append: types.Props is binary-searched (find/Get) and must
			// stay key-sorted, so "container" has to be inserted in position.
			rel.Properties.Set("container", "container:"+name)
			idx, ok := testTypeFacets[owner]
			if !ok {
				// Identity-matched facet for the base extractor's class node —
				// Kind/Subtype/Name/SourceFile all equal to what csharp.go emits,
				// so MergeWithCustom Tier A combines rather than duplicating.
				facet := makeEntity(owner, "SCOPE.Component", "class",
					file.Path, "csharp", lineOf(src, declAt))
				setProps(&facet, "framework", "test_doubles",
					"provenance", "INFERRED_FROM_TESTCONTAINER")
				before := len(entities)
				add(facet)
				if len(entities) == before {
					// Deduped against an earlier facet for the same type that is
					// not in the map (cannot happen today, but never silently
					// drop the edge if it ever does).
					ent.Relationships = append(ent.Relationships, rel)
					add(ent)
					return
				}
				idx = len(entities) - 1
				testTypeFacets[owner] = idx
			}
			entities[idx].Relationships = append(entities[idx].Relationships, rel)
			add(ent)
			return
		}

		if ref != "" {
			ent.Relationships = append(ent.Relationships, rel)
		}
		add(ent)
	}

	for _, m := range reTcTypedContainer.FindAllStringSubmatchIndex(src, -1) {
		ctype := src[m[2]:m[3]]
		// ContainerBuilder is the generic builder, not a service container.
		if ctype == "ContainerBuilder" {
			continue
		}
		emitContainer(ctype, "", ctype, m[0], lineOf(src, m[0]))
	}
	for _, m := range reTcWithImage.FindAllStringSubmatchIndex(src, -1) {
		image := src[m[2]:m[3]]
		emitContainer(image, image, "", m[0], lineOf(src, m[0]))
	}

	// -------------------------------------------------------------------------
	// BDD step definitions — SpecFlow / Reqnroll. Only fire inside [Binding].
	// -------------------------------------------------------------------------
	if reBindingAttr.MatchString(src) {
		for _, m := range reStepAttr.FindAllStringSubmatchIndex(src, -1) {
			keyword := src[m[2]:m[3]]
			text := src[m[4]:m[5]]
			line := lineOf(src, m[0])
			ent := makeEntity("step:"+keyword+":"+itoa(line),
				"SCOPE.Pattern", "step_definition", file.Path, "csharp", line)
			setProps(&ent, "framework", "specflow", "keyword", keyword,
				"step_text", text, "provenance", "INFERRED_FROM_STEP_DEFINITION")
			add(ent)
		}
	}

	// -------------------------------------------------------------------------
	// Test-data builders — Bogus / AutoFixture. The builder USES the built type.
	// -------------------------------------------------------------------------
	emitBuilder := func(target, library, fields string, line int) {
		target = leafCSharpType(target)
		if target == "" || csharpPrimitives[target] {
			return
		}
		ent := makeEntity("builder:"+library+":"+target, "SCOPE.Pattern",
			"test_data_builder", file.Path, "csharp", line)
		props := []string{"framework", "test_doubles", "library", library,
			"target", target, "provenance", "INFERRED_FROM_TEST_DATA_BUILDER"}
		if fields != "" {
			props = append(props, "fields", fields)
		}
		setProps(&ent, props...)
		ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
			ToID: "type:" + target,
			Kind: string(types.RelationshipKindUses),
			Properties: types.Props{
				{K: "framework", V: "test_doubles"},
				{K: "library", V: library},
				{K: "line", V: itoa(line)},
				{K: "role", V: "test_data_builder"},
				{K: "target", V: target},
			},
		})
		add(ent)
	}

	// Bogus: new Faker<T>() with the trailing .RuleFor(x => x.Field, …) chain.
	// We harvest the faked fields from the whole source (RuleFor calls are
	// commonly chained across lines) and attach them to every Faker<T> in the
	// file; the field set is shared at file granularity (honest-partial — we do
	// not scope RuleFor to a specific Faker<T> binding).
	if reBogusFaker.MatchString(src) {
		var fields []string
		seenField := make(map[string]bool)
		for _, m := range reBogusRuleFor.FindAllStringSubmatch(src, -1) {
			f := m[1]
			if !seenField[f] {
				seenField[f] = true
				fields = append(fields, f)
			}
		}
		fieldList := joinStrings(fields, ",")
		for _, m := range reBogusFaker.FindAllStringSubmatchIndex(src, -1) {
			emitBuilder(src[m[2]:m[3]], "bogus", fieldList, lineOf(src, m[0]))
		}
	}

	// AutoFixture: fixture.Create<T>() and fixture.Build<T>(). Both forms only
	// fire when the file actually references AutoFixture (gate on "Fixture" /
	// "AutoFixture") so we don't match unrelated generic Create<T>/Build<T>.
	if regexpAny(src, "Fixture", "AutoFixture") {
		for _, m := range reAutoFixtureCreate.FindAllStringSubmatchIndex(src, -1) {
			emitBuilder(src[m[2]:m[3]], "autofixture", "", lineOf(src, m[0]))
		}
		for _, m := range reAutoFixtureBuild.FindAllStringSubmatchIndex(src, -1) {
			emitBuilder(src[m[2]:m[3]], "autofixture", "", lineOf(src, m[0]))
		}
	}

	// -------------------------------------------------------------------------
	// Mock-target -> DI-impl resolution. When a mock's .Object is wired into
	// production code (DI registration / SUT constructor), resolve the mocked
	// interface to the concrete impl the dotnet_di extractor binds.
	// -------------------------------------------------------------------------
	if len(mockEnts) > 0 && reMockObjectUse.MatchString(src) {
		// Map mock variable -> mocked type from `var x = new Mock<T>()`.
		varToType := make(map[string]string)
		for _, m := range reMoqVarBinding.FindAllStringSubmatch(src, -1) {
			varToType[m[1]] = leafCSharpType(m[2])
		}
		// Each `x.Object` use where x is a known mock variable wires that mock
		// into production code: resolve its interface to the impl by-name.
		resolved := make(map[string]bool)
		for _, m := range reMockObjectUse.FindAllStringSubmatchIndex(src, -1) {
			v := src[m[2]:m[3]]
			target, ok := varToType[v]
			if !ok {
				continue
			}
			idx, ok := mockEnts[target]
			if !ok || resolved[target] {
				continue
			}
			impl := implOfInterface(target)
			line := lineOf(src, m[0])
			entities[idx].Relationships = append(entities[idx].Relationships,
				types.RelationshipRecord{
					ToID: "impl:" + impl,
					Kind: string(types.RelationshipKindResolvesTo),
					Properties: types.Props{
						{K: "framework", V: "test_doubles"},
						{K: "implementation", V: impl},
						{K: "interface", V: target},
						{K: "line", V: itoa(line)},
						{K: "resolution", V: "by_name"},
						{K: "role", V: "mock_di_resolution"},
					},
				})
			setProps(&entities[idx], "resolved_impl", impl)
			resolved[target] = true
		}
	}

	return entities, nil
}

// implOfInterface maps a C# interface name to its conventional implementation
// name by stripping a leading capital-I prefix (IOrderRepository ->
// OrderRepository). When the name does not follow the convention it is returned
// unchanged. This matches the impl node the dotnet_di extractor binds.
func implOfInterface(iface string) string {
	if len(iface) >= 2 && iface[0] == 'I' && iface[1] >= 'A' && iface[1] <= 'Z' {
		return iface[1:]
	}
	return iface
}

// joinStrings joins parts with sep without pulling in strings just for Join in
// this regex-only package's hot path keeping the dependency surface small.
func joinStrings(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}

// indexByteAny returns the index of the first byte in s that is one of the
// bytes in chars, or -1.
func indexByteAny(s, chars string) int {
	for i := 0; i < len(s); i++ {
		for j := 0; j < len(chars); j++ {
			if s[i] == chars[j] {
				return i
			}
		}
	}
	return -1
}
