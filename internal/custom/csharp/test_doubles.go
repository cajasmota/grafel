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
// All three reuse existing entity Kinds (SCOPE.Pattern) and edge kinds
// (USES, DEPENDS_ON_SERVICE) — no new Kind is introduced. Bogus / AutoFixture
// test-data builders are an honest follow-up (see issue #5005).
//
// Registration key: "custom_csharp_test_doubles"
// Issue #5005.
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
	if !regexpAny(src, "Mock<", "Mock.Of", "Substitute.For", "Container", ".WithImage", "[Binding") {
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
		add(ent)
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

	return entities, nil
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
