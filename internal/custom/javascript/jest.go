package javascript

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extreg.Register("custom_js_jest", &jestExtractor{})
}

type jestExtractor struct{}

func (e *jestExtractor) Language() string { return "custom_js_jest" }

var (
	// describe / describe.only / describe.skip / describe.each / xdescribe / fdescribe
	reJestDescribe = regexp.MustCompile(
		`(?:^|[;\n])[ \t]*(?:x|f)?describe(?:\.(?:only|skip|concurrent|each\([^)]+\)))?\s*\(\s*(['` + "`" + `"][^'` + "`" + `"]+['` + "`" + `"])`,
	)
	// it / it.only / it.skip / it.todo / it.concurrent / test / test.only / etc.
	reJestTest = regexp.MustCompile(
		`(?:^|[;\n])[ \t]*(?:x|f)?(?:it|test)(?:\.(?:only|skip|todo|concurrent|each\([^)]+\)))?\s*\(\s*(['` + "`" + `"][^'` + "`" + `"]+['` + "`" + `"])`,
	)
	// beforeAll / afterAll / beforeEach / afterEach
	reJestHook = regexp.MustCompile(
		`(?:^|[;\n])[ \t]*(beforeAll|afterAll|beforeEach|afterEach)\s*\(`,
	)
	// jest.mock("module") / jest.spyOn(obj, "method")
	reJestMock = regexp.MustCompile(
		`jest\s*\.\s*(mock|spyOn|fn|useFakeTimers|useRealTimers|resetAllMocks|clearAllMocks)\s*\(`,
	)
)

func (e *jestExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.jest_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "jest"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	lang := strings.ToLower(file.Language)
	if lang != "typescript" && lang != "javascript" {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	addEntity := func(ent types.EntityRecord) {
		key := fmt.Sprintf("%s:%s:%s", ent.Kind, ent.Name, ent.Subtype)
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	stripQuotes := func(s string) string {
		s = strings.TrimSpace(s)
		if len(s) >= 2 {
			s = s[1 : len(s)-1]
		}
		return s
	}

	// describe blocks → test_suite
	for _, m := range reJestDescribe.FindAllStringSubmatchIndex(src, -1) {
		label := stripQuotes(src[m[2]:m[3]])
		ent := makeEntity(label, "SCOPE.Operation", "test_suite", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "jest", "provenance", "INFERRED_FROM_JEST_DESCRIBE")
		addEntity(ent)
	}

	// it/test blocks → test_case
	for _, m := range reJestTest.FindAllStringSubmatchIndex(src, -1) {
		label := stripQuotes(src[m[2]:m[3]])
		ent := makeEntity(label, "SCOPE.Operation", "test_case", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "jest", "provenance", "INFERRED_FROM_JEST_TEST")
		addEntity(ent)
	}

	// Setup/teardown hooks
	for _, m := range reJestHook.FindAllStringSubmatchIndex(src, -1) {
		hookName := src[m[2]:m[3]]
		ent := makeEntity(hookName, "SCOPE.Pattern", "test_hook", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "jest", "hook_name", hookName,
			"provenance", "INFERRED_FROM_JEST_HOOK")
		addEntity(ent)
	}

	// jest.mock / jest.spyOn
	for _, m := range reJestMock.FindAllStringSubmatchIndex(src, -1) {
		methodName := src[m[2]:m[3]]
		name := "jest." + methodName
		ent := makeEntity(name, "SCOPE.Pattern", "mock_setup", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "jest", "jest_method", methodName,
			"provenance", "INFERRED_FROM_JEST_MOCK")
		addEntity(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
