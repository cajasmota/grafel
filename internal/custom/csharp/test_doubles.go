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
//	  container'd service (service:<image-or-container>). The node carries
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

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
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
	tracer := otel.Tracer("archigraph/custom/csharp")
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
			Properties: map[string]string{
				"library":   library,
				"target":    target,
				"role":      "mock_binding",
				"framework": "test_doubles",
				"line":      itoa(line),
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
			ToID: "service:" + name,
			Kind: string(types.RelationshipKindDependsOnService),
			Properties: map[string]string{
				"image":          image,
				"container_type": ctype,
				"role":           "container_topology",
				"framework":      "test_doubles",
				"line":           itoa(line),
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
			Properties: map[string]string{
				"library":   library,
				"target":    target,
				"role":      "test_data_builder",
				"framework": "test_doubles",
				"line":      itoa(line),
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
					Properties: map[string]string{
						"interface":      target,
						"implementation": impl,
						"role":           "mock_di_resolution",
						"framework":      "test_doubles",
						"resolution":     "by_name",
						"line":           itoa(line),
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
