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
//	  -> SCOPE.Pattern/container_topology; the test DEPENDS_ON_SERVICE the
//	  container'd service, addressed by the canonical external-service ref
//	  (scope:externalservice:<image-or-container>, #6123 — no service node is
//	  fabricated, so the edge dangles honestly unless one already exists). The node carries
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
)

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
	// colon-bearing), neither of which is a canonical service name; normalising
	// them would mean inventing a second, competing service dictionary. Nor does
	// the #6131 "repair the edge rather than accept a dangle" precedent transfer:
	// there a correct binding already existed in the record set, and here the
	// only candidates are the container node itself (a self-edge) and the
	// constructor call site (the mis-bind we are removing).
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
	//  2. Even without (1), splitStub cuts at the FIRST colon, so the byName
	//     probe would be "externalservice:<name>" — a string no entity Name
	//     carries. The old ref's probe was the bare leaf `<name>`, which real
	//     entities do carry; that is the whole difference.
	//
	// The ref still binds correctly, via byQualifiedName, if a genuine service
	// node with that name is ever present.
	// -------------------------------------------------------------------------
	emitContainer := func(name, image, ctype string, line int) {
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
		ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
			ToID: extractor.ExternalServiceTargetID(name),
			Kind: string(types.RelationshipKindDependsOnService),
			Properties: types.Props{
				{K: "container_type", V: ctype},
				{K: "framework", V: "test_doubles"},
				{K: "image", V: image},
				{K: "line", V: itoa(line)},
				{K: "role", V: "container_topology"},
			},
		})
		add(ent)
	}

	for _, m := range reTcTypedContainer.FindAllStringSubmatchIndex(src, -1) {
		ctype := src[m[2]:m[3]]
		// ContainerBuilder is the generic builder, not a service container.
		if ctype == "ContainerBuilder" {
			continue
		}
		emitContainer(ctype, "", ctype, lineOf(src, m[0]))
	}
	for _, m := range reTcWithImage.FindAllStringSubmatchIndex(src, -1) {
		image := src[m[2]:m[3]]
		emitContainer(image, image, "", lineOf(src, m[0]))
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
