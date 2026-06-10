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
// ISSUE #4358 — orphan collapse + TESTS edge.
//
// Previously each of these extractors emitted a FIRST-CLASS SCOPE.Pattern entity
// per testify suite struct / suite.Run / suite-method / assert call, per ginkgo
// container / spec / hook, and per gomega matcher assertion — NONE of which
// carried any relationship. On a real Go codebase those edge-less nodes
// (assertions especially) dominate the orphan ring, exactly mirroring the
// Jest/Vitest orphan ring that issue #4343 collapsed.
//
// Root-cause fix at extraction (not a downstream repair pass), mirroring the
// SHAPE of #4343:
//
//   - Emit exactly ONE test_suite entity per *_test.go file per framework. The
//     per-suite-struct / per-case / per-assertion / per-hook / per-container /
//     per-spec / per-matcher nodes are NO LONGER emitted as standalone entities;
//     their counts are folded into properties (suite_count, test_case_count,
//     assertion_count, hook_count, container_count, spec_count, matcher_count,
//     suite_run_count) so no information is lost while the orphan blast radius
//     collapses from O(suites+cases+hooks+assertions) to at most one node.
//
//   - Synthesize a TESTS edge from the file's test_suite to the production
//     symbol(s) under test, resolved Go-idiomatically by NAME AFFINITY against
//     symbols actually referenced in the test file (high-confidence only, see
//     resolveGoTestSubjects). The edge ToID is the `Class:<Subject>` structural
//     ref the existing cross-file resolver binds by name (the same ref dto.go
//     already emits for Go structs).
//
//   - The suite entity name is namespaced (`<framework>_suite:<base>`) so it
//     never collides with the production symbol of the same name in the
//     resolver's by-name index (which would blank both as ambiguous and
//     re-orphan the test, exactly as in #4343).
//
// Suite subtype: test_suite (the existing kind/subtype; reused, not new).

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
// Shared TESTS-target resolution (issue #4358)
// ---------------------------------------------------------------------------

// reGoTestFunc matches a top-level `func TestXxx(t *testing.T)` test function.
var reGoTestFunc = regexp.MustCompile(
	`(?m)^\s*func\s+(Test\w+)\s*\(\s*\w+\s+\*testing\.T\s*\)`,
)

// reGoIdentToken matches a bare exported Go identifier (TitleCase), used to
// validate that a derived subject name is a plausible production symbol.
var reGoIdentToken = regexp.MustCompile(`^[A-Z][A-Za-z0-9_]*$`)

// reGoTypeRef matches references to a named type that strongly indicate the
// unit under test is being constructed or referenced in the test file:
//
//	new(Foo)        — pointer construction
//	&Foo{           — composite-literal address
//	Foo{            — composite literal
//	NewFoo(         — idiomatic constructor call
//
// The captured group is the (exported) type / constructor-suffix name. These
// are the Go analog of the Jest `new Subject()` / `.get<Subject>()` signals:
// they name a concrete in-repo symbol the test exercises, which gates the
// name-affinity match so we never link to a framework/util identifier.
var (
	reGoNewBuiltin = regexp.MustCompile(`\bnew\s*\(\s*([A-Z][A-Za-z0-9_]*)\s*\)`)
	reGoAddrLit    = regexp.MustCompile(`&\s*([A-Z][A-Za-z0-9_]*)\s*\{`)
	reGoCompLit    = regexp.MustCompile(`\b([A-Z][A-Za-z0-9_]*)\s*\{`)
	reGoConstructor = regexp.MustCompile(`\bNew([A-Z][A-Za-z0-9_]*)\s*\(`)
)

// collectGoReferencedSymbols returns the set of exported symbol names that are
// constructed or referenced in the test file via the high-confidence signals
// above. Only names in this set are eligible to become a TESTS subject, which
// keeps the edge pointed at an in-repo production entity (resolved by the
// cross-file symbol table as `Class:<Subject>`) rather than a stdlib/util name.
func collectGoReferencedSymbols(src string) map[string]bool {
	out := make(map[string]bool)
	addAll := func(re *regexp.Regexp, src string) {
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			name := strings.TrimSpace(m[1])
			if reGoIdentToken.MatchString(name) {
				out[name] = true
			}
		}
	}
	addAll(reGoNewBuiltin, src)
	addAll(reGoAddrLit, src)
	addAll(reGoCompLit, src)
	// `NewFoo(` → the constructed type is `Foo` (constructor convention).
	for _, m := range reGoConstructor.FindAllStringSubmatch(src, -1) {
		name := strings.TrimSpace(m[1])
		if reGoIdentToken.MatchString(name) {
			out[name] = true
		}
	}
	return out
}

// subjectCandidatesFromTestName derives the production-symbol candidates a Go
// test function name affinity-maps to, most-specific first:
//
//	TestPlaceOrder            → ["PlaceOrder"]
//	TestOrderService_Place    → ["OrderService.Place", "OrderService", "Place"]
//	TestOrderService_PlaceX   → ["OrderService.PlaceX", "OrderService", "PlaceX"]
//
// The leading `Test` is stripped; a single underscore splits the
// Type_Method form (the idiomatic Go subtest/method naming convention). Names
// that are not plausible exported identifiers are dropped.
func subjectCandidatesFromTestName(testName string) []string {
	base := strings.TrimPrefix(testName, "Test")
	if base == "" {
		return nil
	}
	var cands []string
	if i := strings.IndexByte(base, '_'); i > 0 && i < len(base)-1 {
		typ := base[:i]
		meth := base[i+1:]
		if reGoIdentToken.MatchString(typ) && reGoIdentToken.MatchString(meth) {
			cands = append(cands, typ+"."+meth, typ, meth)
			return cands
		}
	}
	if reGoIdentToken.MatchString(base) {
		cands = append(cands, base)
	}
	return cands
}

// suiteSubjectFromStructName derives the production symbol a testify suite
// struct exercises by stripping the conventional `Suite` / `TestSuite` suffix:
//
//	UserServiceSuite     → UserService
//	OrderServiceTestSuite → OrderService
//
// Returns "" when nothing plausible remains.
func suiteSubjectFromStructName(structName string) string {
	for _, suf := range []string{"TestSuite", "Suite"} {
		if strings.HasSuffix(structName, suf) && len(structName) > len(suf) {
			base := structName[:len(structName)-len(suf)]
			if reGoIdentToken.MatchString(base) {
				return base
			}
		}
	}
	return ""
}

// resolveGoTestSubjects determines the unit(s) under test for a Go *_test.go
// file, de-duplicated and in priority order. A subject is emitted ONLY when it
// is both (a) derivable by name affinity from a TestXxx function or a testify
// suite struct, AND (b) actually referenced (constructed/called) in the test
// file via collectGoReferencedSymbols. Requiring both keeps the TESTS edge
// conservative and unique — name affinity alone would over-link (e.g.
// TestParse → any Parse), and reference alone would link helper fixtures.
//
// suiteStructs is the set of testify suite struct names found in the file (may
// be empty for non-testify frameworks).
func resolveGoTestSubjects(src string, suiteStructs map[string]bool) []string {
	referenced := collectGoReferencedSymbols(src)
	var ordered []string
	seen := map[string]bool{}
	add := func(name string) {
		// A suite struct is test scaffolding, not the unit under test — never
		// link to it (it is referenced via new(MySuite) for suite.Run).
		if name == "" || seen[name] || !referenced[name] || suiteStructs[name] {
			return
		}
		seen[name] = true
		ordered = append(ordered, name)
	}

	// 1. testify suite structs → strip Suite suffix → subject.
	for struct_ := range suiteStructs {
		add(suiteSubjectFromStructName(struct_))
	}

	// 2. Top-level TestXxx funcs → name affinity. We take the FIRST referenced
	//    candidate per test func (most specific that is actually referenced).
	for _, m := range reGoTestFunc.FindAllStringSubmatch(src, -1) {
		for _, cand := range subjectCandidatesFromTestName(m[1]) {
			// Type_Method form yields a "Type.Method" candidate that is not a
			// bare referenced symbol; map it back to its Type for the reference
			// gate while still recording the qualified subject.
			gate := cand
			if dot := strings.IndexByte(cand, '.'); dot > 0 {
				gate = cand[:dot]
			}
			if referenced[gate] {
				add(gate)
				break
			}
		}
	}
	return ordered
}

// attachTestsEdges stamps the resolved subjects onto the suite entity as TESTS
// edges (ToID `Class:<Subject>`) plus a `tests_target` property. Shared by all
// three framework extractors.
func attachTestsEdges(ent *types.EntityRecord, framework string, subjects []string) {
	if len(subjects) == 0 {
		return
	}
	setProps(ent, "tests_target", strings.Join(subjects, ","))
	for _, subj := range subjects {
		ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
			ToID: "Class:" + subj,
			Kind: string(types.RelationshipKindTests),
			Properties: map[string]string{
				"framework":    framework,
				"match_source": "go_test_name_affinity",
				"target_type":  subj,
			},
			Confidence: 0.9,
		})
	}
}

// goTestBaseName derives a human label from a Go test file path, e.g.
// `internal/svc/order_test.go` → `order`.
func goTestBaseName(path string) string {
	p := path
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		p = p[i+1:]
	}
	p = strings.TrimSuffix(p, ".go")
	p = strings.TrimSuffix(p, "_test")
	return p
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
	// assert.Equal(...) / require.NoError(...) — package-qualified assertion.
	reTestifyAssert = regexp.MustCompile(
		`(?m)\b(assert|require)\.(\w+)\s*\(`,
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

	// ── collect per-file counts (folded onto the single suite entity) ───────
	suiteStructs := map[string]bool{}
	suiteStructMatches := reTestifySuiteType.FindAllStringSubmatchIndex(src, -1)
	for _, m := range suiteStructMatches {
		suiteStructs[src[m[2]:m[3]]] = true
	}
	suiteRuns := reTestifySuiteRun.FindAllStringSubmatchIndex(src, -1)
	// Suite receiver-method test cases (only those bound to a real suite struct).
	caseCount := 0
	for _, m := range reTestifySuiteMethod.FindAllStringSubmatchIndex(src, -1) {
		if suiteStructs[src[m[2]:m[3]]] {
			caseCount++
		}
	}
	assertCount := len(reTestifyAssert.FindAllStringIndex(src, -1))
	topLevelTests := len(reGoTestFunc.FindAllStringIndex(src, -1))

	// Nothing testify-shaped to model → emit nothing (keeps non-suite plain
	// `assert.X` helper files from minting an empty suite when there is also
	// no Test function — but a file with assertions and tests still emits one).
	if len(suiteStructs) == 0 && len(suiteRuns) == 0 && caseCount == 0 &&
		assertCount == 0 && topLevelTests == 0 {
		span.SetAttributes(attribute.Int("entity_count", 0))
		return nil, nil
	}

	// ── one linked test_suite per file ──────────────────────────────────────
	line := 1
	if len(suiteStructMatches) > 0 {
		line = lineOf(src, suiteStructMatches[0][0])
	}
	ent := makeEntity("testify_suite:"+goTestBaseName(file.Path), "SCOPE.Pattern", "test_suite",
		file.Path, file.Language, line)
	setProps(&ent, "framework", "testify", "provenance", "INFERRED_FROM_TESTIFY_SUITE",
		"test_framework", "testify",
		"suite_count", itoa(len(suiteStructs)),
		"suite_run_count", itoa(len(suiteRuns)),
		"test_case_count", itoa(caseCount),
		"assertion_count", itoa(assertCount),
		"test_func_count", itoa(topLevelTests),
	)
	if len(suiteStructs) > 0 {
		setProps(&ent, "suites", strings.Join(sortedKeys(suiteStructs), ","))
	}

	attachTestsEdges(&ent, "testify", resolveGoTestSubjects(src, suiteStructs))

	span.SetAttributes(attribute.Int("entity_count", 1))
	return []types.EntityRecord{ent}, nil
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
	// Setup/teardown hooks.
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

	containers := reGinkgoContainer.FindAllStringSubmatchIndex(src, -1)
	specs := reGinkgoSpec.FindAllStringIndex(src, -1)
	hooks := reGinkgoHook.FindAllStringIndex(src, -1)
	if len(containers) == 0 && len(specs) == 0 {
		span.SetAttributes(attribute.Int("entity_count", 0))
		return nil, nil
	}

	// Suite label: the first (top-level) container description, else the file base.
	label := goTestBaseName(file.Path)
	line := 1
	if len(containers) > 0 {
		label = src[containers[0][6]:containers[0][7]]
		line = lineOf(src, containers[0][0])
	}

	ent := makeEntity("ginkgo_suite:"+goTestBaseName(file.Path), "SCOPE.Pattern", "test_suite",
		file.Path, file.Language, line)
	setProps(&ent, "framework", "ginkgo", "provenance", "INFERRED_FROM_GINKGO_CONTAINER",
		"test_framework", "ginkgo",
		"suite_label", label,
		"container_count", itoa(len(containers)),
		"spec_count", itoa(len(specs)),
		"hook_count", itoa(len(hooks)),
	)

	// ginkgo subjects come purely from name affinity on referenced symbols (no
	// TestXxx funcs / suite structs in the DSL), so pass an empty suite set.
	attachTestsEdges(&ent, "ginkgo", resolveGoTestSubjects(src, nil))

	span.SetAttributes(attribute.Int("entity_count", 1))
	return []types.EntityRecord{ent}, nil
}

// ---------------------------------------------------------------------------
// gomega
// ---------------------------------------------------------------------------

type gomegaExtractor struct{}

func (e *gomegaExtractor) Language() string { return "custom_go_gomega" }

var (
	// Expect(actual).To(matcher) / Eventually(...).Should(matcher) / Ω(...) …
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

	matches := reGomegaAssert.FindAllStringSubmatchIndex(src, -1)
	if len(matches) == 0 {
		span.SetAttributes(attribute.Int("entity_count", 0))
		return nil, nil
	}

	// Distinct matcher constructors used, folded as a property.
	matcherSet := map[string]bool{}
	for _, m := range matches {
		matcherSet[src[m[6]:m[7]]] = true
	}

	ent := makeEntity("gomega_suite:"+goTestBaseName(file.Path), "SCOPE.Pattern", "test_suite",
		file.Path, file.Language, lineOf(src, matches[0][0]))
	setProps(&ent, "framework", "gomega", "provenance", "INFERRED_FROM_GOMEGA_MATCHER",
		"test_framework", "gomega",
		"assertion_count", itoa(len(matches)),
		"matcher_count", itoa(len(matcherSet)),
		"matchers", strings.Join(sortedKeys(matcherSet), ","),
	)

	attachTestsEdges(&ent, "gomega", resolveGoTestSubjects(src, nil))

	span.SetAttributes(attribute.Int("entity_count", 1))
	return []types.EntityRecord{ent}, nil
}

// sortedKeys returns the keys of a string-set in deterministic (sorted) order
// so folded property values (suites=, matchers=) are stable across runs.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Small N — insertion sort keeps this dependency-free.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
