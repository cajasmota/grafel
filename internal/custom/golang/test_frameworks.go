package golang

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// test_frameworks.go — custom extractors for Go test-framework constructs that
// the base Go extractor and the stdlib testing path (internal/engine/rules/go/
// test_patterns.yaml + cross/testmap) do not model structurally:
//
//   - testify  : suite structs (embed suite.Suite), suite.Run() registration,
//                suite receiver-method test cases, and assert/require assertion
//                calls.
//   - ginkgo   : the Describe/Context/When container DSL, It/Specify spec
//                cases, and Before/AfterEach setup hooks.
//   - gomega   : Expect(...).To(matcher) / Ω(...).Should(matcher) assertions,
//                capturing the matcher name.
//
// All three emit SCOPE.Pattern nodes carrying a synthetic, non-colliding Name
// (prefixed by framework + kind) so they never shadow the base extractor's
// SCOPE.Function / SCOPE.Class nodes for the same source line. The
// dependency_graph aspect is carried in properties (suite, container,
// asserted_target) that downstream passes consume to link cases→suites and
// assertions→subjects. Pattern subtypes:
//
//	test_suite | test_case | assertion | test_hook | suite_run

func init() {
	extractor.Register("custom_go_testify", &testifyExtractor{})
	extractor.Register("custom_go_ginkgo", &ginkgoExtractor{})
	extractor.Register("custom_go_gomega", &gomegaExtractor{})
}

// ---------------------------------------------------------------------------
// Shared scaffolding
// ---------------------------------------------------------------------------

// emitter wraps the dedup-on-(kind,name) accumulation shared by the three
// extractors, mirroring chi.go's local `add` closure.
type emitter struct {
	out  []types.EntityRecord
	seen map[string]bool
}

func newEmitter() *emitter { return &emitter{seen: map[string]bool{}} }

func (em *emitter) add(ent types.EntityRecord) {
	key := ent.Kind + ":" + ent.Name
	if em.seen[key] {
		return
	}
	em.seen[key] = true
	em.out = append(em.out, ent)
}

// ---------------------------------------------------------------------------
// testify
// ---------------------------------------------------------------------------

type testifyExtractor struct{}

func (e *testifyExtractor) Language() string { return "custom_go_testify" }

var (
	// type MySuite struct { ... suite.Suite ... } — capture the suite type name.
	reTestifySuiteType = regexp.MustCompile(
		`(?ms)\btype\s+(\w+)\s+struct\s*\{[^}]*\bsuite\.Suite\b`,
	)
	// suite.Run(t, new(MySuite)) / suite.Run(t, &MySuite{}) — registration call.
	reTestifySuiteRun = regexp.MustCompile(
		`(?m)\bsuite\.Run\s*\(\s*\w+\s*,\s*(?:new\s*\(\s*(\w+)\s*\)|&\s*(\w+)\s*\{)`,
	)
	// func (s *MySuite) TestFoo() { — suite receiver-method test case.
	reTestifySuiteMethod = regexp.MustCompile(
		`(?m)^\s*func\s+\(\s*\w+\s+\*(\w+)\s*\)\s+(Test\w+)\s*\([^)]*\)\s*\{`,
	)
	// assert.Equal(...) / require.NoError(...) / s.Assert().True(...) /
	// s.Require().Equal(...) — assertion calls. Capture package/qualifier + the
	// assertion method name.
	reTestifyAssert = regexp.MustCompile(
		`(?m)\b(assert|require)\.(\w+)\s*\(`,
	)
	// Receiver-bound assertions: s.Equal(...) where the suite exposes the
	// embedded assert API directly. We restrict to the well-known testify
	// assertion verbs to avoid matching arbitrary method calls.
	reTestifyRecvAssert = regexp.MustCompile(
		`(?m)\b(?:\w+)\.(Equal|NotEqual|Nil|NotNil|True|False|Error|NoError|Len|Contains|NotContains|ElementsMatch|Empty|NotEmpty|EqualValues|JSONEq|ErrorIs|ErrorAs|Panics|NotPanics|Greater|Less|Eventually|Regexp|Subset|Zero|NotZero)\s*\(`,
	)
)

func (e *testifyExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.testify_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "testify"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "go" {
		return nil, nil
	}
	src := string(file.Content)
	// Gate on a testify import marker to avoid firing on unrelated Go files.
	if !strings.Contains(src, "stretchr/testify") &&
		!strings.Contains(src, "suite.Suite") &&
		!regexp.MustCompile(`\b(assert|require)\.`).MatchString(src) {
		return nil, nil
	}

	em := newEmitter()

	// 1. Suite structs -> SCOPE.Pattern/test_suite
	suiteNames := map[string]bool{}
	for _, m := range reTestifySuiteType.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		suiteNames[name] = true
		ent := makeEntity("testify_suite:"+name, "SCOPE.Pattern", "test_suite", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "testify", "provenance", "INFERRED_FROM_TESTIFY_SUITE",
			"test_framework", "testify", "suite", name)
		em.add(ent)
	}

	// 2. suite.Run(t, new(MySuite)) -> SCOPE.Pattern/suite_run (registration edge)
	for _, m := range reTestifySuiteRun.FindAllStringSubmatchIndex(src, -1) {
		name := submatch(src, m, 2)
		if name == "" {
			name = submatch(src, m, 4)
		}
		if name == "" {
			continue
		}
		ent := makeEntity("testify_run:"+name, "SCOPE.Pattern", "suite_run", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "testify", "provenance", "INFERRED_FROM_TESTIFY_SUITE_RUN",
			"test_framework", "testify", "suite", name)
		em.add(ent)
	}

	// 3. Suite receiver-method test cases -> SCOPE.Pattern/test_case
	//    Only when the receiver type is a known suite struct (embeds suite.Suite).
	for _, m := range reTestifySuiteMethod.FindAllStringSubmatchIndex(src, -1) {
		recv := src[m[2]:m[3]]
		testName := src[m[4]:m[5]]
		if !suiteNames[recv] {
			continue
		}
		ent := makeEntity("testify_case:"+recv+"."+testName, "SCOPE.Pattern", "test_case", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "testify", "provenance", "INFERRED_FROM_TESTIFY_SUITE_METHOD",
			"test_framework", "testify", "suite", recv, "test_name", testName)
		em.add(ent)
	}

	// 4. assert.* / require.* assertion calls -> SCOPE.Pattern/assertion
	for _, m := range reTestifyAssert.FindAllStringSubmatchIndex(src, -1) {
		pkg := src[m[2]:m[3]]
		method := src[m[4]:m[5]]
		line := lineOf(src, m[0])
		ent := makeEntity("testify_assert:"+pkg+"."+method+"@"+itoa(line), "SCOPE.Pattern", "assertion", file.Path, file.Language, line)
		setProps(&ent, "framework", "testify", "provenance", "INFERRED_FROM_TESTIFY_ASSERT",
			"test_framework", "testify", "assertion_pkg", pkg, "assertion", method)
		em.add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(em.out)))
	return em.out, nil
}

// ---------------------------------------------------------------------------
// ginkgo
// ---------------------------------------------------------------------------

type ginkgoExtractor struct{}

func (e *ginkgoExtractor) Language() string { return "custom_go_ginkgo" }

var (
	// Container nodes: Describe / Context / When ("desc", func() { ... }).
	// Ginkgo v2 also exposes the F- (focused) and P-/X- (pending) variants.
	reGinkgoContainer = regexp.MustCompile(
		`(?m)\b([FPX]?)(Describe|Context|When)\s*\(\s*"([^"]{1,300})"`,
	)
	// Spec cases: It / Specify ("does x", func() { ... }), with the same
	// focus/pending prefixes.
	reGinkgoSpec = regexp.MustCompile(
		`(?m)\b([FPX]?)(It|Specify)\s*\(\s*"([^"]{1,300})"`,
	)
	// Setup/teardown hooks: BeforeEach / AfterEach / BeforeSuite / AfterSuite /
	// JustBeforeEach / JustAfterEach.
	reGinkgoHook = regexp.MustCompile(
		`(?m)\b(BeforeEach|AfterEach|BeforeSuite|AfterSuite|JustBeforeEach|JustAfterEach|BeforeAll|AfterAll)\s*\(`,
	)
)

func (e *ginkgoExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.ginkgo_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "ginkgo"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "go" {
		return nil, nil
	}
	src := string(file.Content)
	// Gate on a ginkgo import / DSL marker.
	if !strings.Contains(src, "onsi/ginkgo") && !strings.Contains(src, "ginkgo") {
		return nil, nil
	}

	em := newEmitter()

	// 1. Containers (Describe/Context/When) -> SCOPE.Pattern/test_suite
	for _, m := range reGinkgoContainer.FindAllStringSubmatchIndex(src, -1) {
		prefix := src[m[2]:m[3]]
		dsl := src[m[4]:m[5]]
		desc := src[m[6]:m[7]]
		line := lineOf(src, m[0])
		ent := makeEntity("ginkgo_container:"+desc+"@"+itoa(line), "SCOPE.Pattern", "test_suite", file.Path, file.Language, line)
		setProps(&ent, "framework", "ginkgo", "provenance", "INFERRED_FROM_GINKGO_CONTAINER",
			"test_framework", "ginkgo", "container_dsl", dsl, "description", desc,
			"focus_state", ginkgoFocusState(prefix))
		em.add(ent)
	}

	// 2. Specs (It/Specify) -> SCOPE.Pattern/test_case
	for _, m := range reGinkgoSpec.FindAllStringSubmatchIndex(src, -1) {
		prefix := src[m[2]:m[3]]
		dsl := src[m[4]:m[5]]
		desc := src[m[6]:m[7]]
		line := lineOf(src, m[0])
		ent := makeEntity("ginkgo_spec:"+desc+"@"+itoa(line), "SCOPE.Pattern", "test_case", file.Path, file.Language, line)
		setProps(&ent, "framework", "ginkgo", "provenance", "INFERRED_FROM_GINKGO_SPEC",
			"test_framework", "ginkgo", "spec_dsl", dsl, "description", desc,
			"focus_state", ginkgoFocusState(prefix))
		em.add(ent)
	}

	// 3. Setup/teardown hooks -> SCOPE.Pattern/test_hook
	for _, m := range reGinkgoHook.FindAllStringSubmatchIndex(src, -1) {
		hook := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity("ginkgo_hook:"+hook+"@"+itoa(line), "SCOPE.Pattern", "test_hook", file.Path, file.Language, line)
		setProps(&ent, "framework", "ginkgo", "provenance", "INFERRED_FROM_GINKGO_HOOK",
			"test_framework", "ginkgo", "hook", hook)
		em.add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(em.out)))
	return em.out, nil
}

// ginkgoFocusState maps the Ginkgo node prefix to a human-readable run state.
func ginkgoFocusState(prefix string) string {
	switch prefix {
	case "F":
		return "focused"
	case "P", "X":
		return "pending"
	default:
		return "normal"
	}
}

// ---------------------------------------------------------------------------
// gomega
// ---------------------------------------------------------------------------

type gomegaExtractor struct{}

func (e *gomegaExtractor) Language() string { return "custom_go_gomega" }

var (
	// Expect(actual).To(matcher) / .ToNot(...) / .NotTo(...) /
	// Eventually(...).Should(matcher) / Ω(...).Should(matcher) /
	// Expect(...).Should(matcher). Capture the entry verb, the polarity method,
	// and the matcher constructor name.
	// Leading boundary is (?:^|[^\w.]) rather than \b because Go's RE2 \b is
	// ASCII-only and fails to recognise the boundary before the multibyte Ω
	// rune. The group is non-capturing so submatch indices are unaffected.
	reGomegaAssert = regexp.MustCompile(
		`(?m)(?:^|[^\w.])(Expect|Ω|Expectf|Eventually|Consistently)\s*\((?:[^()]|\([^()]*\))*\)\s*\.\s*(To|ToNot|NotTo|Should|ShouldNot)\s*\(\s*([A-Za-z_]\w*)`,
	)
)

func (e *gomegaExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.gomega_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "gomega"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "go" {
		return nil, nil
	}
	src := string(file.Content)
	// Gate on a gomega import / DSL marker.
	if !strings.Contains(src, "onsi/gomega") &&
		!strings.Contains(src, "gomega") &&
		!strings.Contains(src, "Expect(") &&
		!strings.Contains(src, "Ω(") {
		return nil, nil
	}

	em := newEmitter()

	// Expect(x).To(Equal(y)) -> SCOPE.Pattern/assertion (matcher captured).
	for _, m := range reGomegaAssert.FindAllStringSubmatchIndex(src, -1) {
		entry := src[m[2]:m[3]]
		polarity := src[m[4]:m[5]]
		matcher := src[m[6]:m[7]]
		line := lineOf(src, m[0])
		ent := makeEntity("gomega_assert:"+matcher+"@"+itoa(line), "SCOPE.Pattern", "assertion", file.Path, file.Language, line)
		setProps(&ent, "framework", "gomega", "provenance", "INFERRED_FROM_GOMEGA_MATCHER",
			"test_framework", "gomega", "assertion_entry", entry, "polarity", polarity, "matcher", matcher)
		em.add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(em.out)))
	return em.out, nil
}
