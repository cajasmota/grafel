package java

import (
	"regexp"
	"sort"
	"strings"
)

// JUnit (4 + 5) / TestNG custom extractor.
//
// ISSUE #4359 — orphan collapse + TESTS edge (the Java analog of the Jest #4343
// and Go #4358 fixes).
//
// Previously this extractor emitted a FIRST-CLASS entity per @Test method
// (SCOPE.Operation), per @BeforeEach/@AfterEach/@BeforeAll/@AfterAll lifecycle
// method (SCOPE.Operation), per @Nested class (SCOPE.Component), and per
// @ExtendWith extension (SCOPE.Pattern). The OWNS / DEPENDS_ON edges all hung
// off a synthetic `scope:component:junit5_test_class:…` ref that was never
// materialised as a real entity, and NO edge ever pointed at the production
// class under test. On a real Java codebase those per-method / per-lifecycle
// nodes dominate the orphan ring, exactly mirroring the Jest/Vitest and
// testify/ginkgo orphan rings collapsed by #4343 / #4358.
//
// Root-cause fix at extraction (not a downstream repair pass), mirroring the
// SHAPE of #4343 / #4358:
//
//   - Emit exactly ONE test_suite entity per test-class file. The per-@Test /
//     per-lifecycle / per-@Nested / per-@ExtendWith / per-assertion nodes are
//     NO LONGER emitted as standalone entities; their counts are folded into
//     properties (test_method_count, lifecycle_count, nested_count,
//     extension_count, assertion_count, plus test_annotations / extensions /
//     nested_classes lists) so no information is lost while the orphan blast
//     radius collapses from O(methods+lifecycle+nested+extensions) to at most
//     one node per file.
//
//   - Synthesize a TESTS edge from the file's test_suite to the production
//     symbol under test, resolved Java-idiomatically (see resolveJavaTestSubject):
//     OrderServiceTest / TestOrderService / OrderServiceTests / OrderServiceIT
//     → OrderService, gated on the SUT type actually being referenced in the
//     file (@InjectMocks / @Autowired field of the SUT type, `new OrderService(`,
//     or a declared field/variable of that type). The edge ToID is the
//     `Class:<Subject>` structural ref the cross-file resolver binds by name
//     (the same ref neo4j.go already emits for Java classes).
//
//   - The suite entity name is namespaced (`junit_suite:<base>`) so it never
//     collides with the production symbol of the same name in the resolver's
//     by-name index (which would blank both as ambiguous and re-orphan the
//     test, exactly as in #4343).
//
// Reuses the existing SCOPE.Pattern kind + test_suite subtype and the TESTS
// relationship kind — no new producer Kind.
//
// Coverage: JUnit 5 (@Test/@ParameterizedTest/@RepeatedTest, jupiter lifecycle),
// JUnit 4 (@Test + @Before/@After/@BeforeClass/@AfterClass, @RunWith), and
// TestNG (@Test + @BeforeMethod/@AfterMethod/@BeforeClass/@AfterClass and
// @BeforeSuite/@AfterSuite). Mockito @InjectMocks and `new SUT(...)` SUT
// inference are fully covered; @Autowired-field SUT inference is covered for the
// common single-field case.

// junit5Frameworks lists all framework identifiers for which the JUnit/TestNG
// extractor is active. Jakarta EE and MicroProfile projects typically use
// JUnit 5 (via Arquillian or plain JUnit) as their test runner — enabling
// tests_linkage for those records (#2996).
var junit5Frameworks = map[string]bool{
	"junit5": true, "junit-jupiter": true, "junit_jupiter": true,
	"junit_5": true, "junit 5": true,
	// Plain JUnit 4 and TestNG test classes with no other framework signal
	// (#4359) — added so pure JUnit4/TestNG suites are linked, not dropped.
	"junit4": true, "junit-4": true, "junit_4": true, "junit": true,
	"testng": true, "test_ng": true, "test-ng": true,
	// Jakarta EE and MicroProfile use JUnit 5 for tests_linkage (#2996).
	"jakarta_ee": true, "jakarta-ee": true, "jakartaee": true,
	"microprofile": true, "eclipse-microprofile": true,
	"open_liberty": true, "payara": true, "helidon": true,
	// Spring Boot and Spring WebFlux projects use JUnit 5 (via @SpringBootTest /
	// @WebFluxTest) for tests_linkage (#2991).
	"spring_boot": true, "spring-boot": true, "springboot": true,
	"spring_webflux": true, "spring-webflux": true, "springwebflux": true,
	// Dropwizard uses JUnit 5 with DropwizardExtensionsSupport for tests_linkage (#3087).
	"dropwizard": true,
	// Javalin uses JUnit 5 with JavalinTest.create / TestUtil.test for tests_linkage (#3085).
	"javalin": true,
	// Vert.x uses JUnit 5 with VertxExtension / VertxTestContext for tests_linkage (#3086).
	"vertx": true, "vert.x": true, "vert_x": true, "vertx_web": true, "vertx-web": true,
	// Struts uses JUnit 5 (or JUnit 4 via Struts Test Plugin) for tests_linkage (#3089).
	"struts": true, "struts2": true, "struts-2": true, "apache_struts": true, "apache-struts": true,
	"struts_2": true,
	// GWT uses JUnit 5 via GWTTestCase for tests_linkage (#3177).
	"gwt": true, "google_web_toolkit": true, "google-web-toolkit": true,
	// Vaadin uses JUnit 5 via @SpringBootTest or plain JUnit 5 for tests_linkage (#3177).
	"vaadin": true,
	// Android SDK and Jetpack use JUnit 5 via @ExtendWith(AndroidJUnit4Runner) for tests_linkage (#3177).
	"android_sdk": true, "android-sdk": true,
	"android_jetpack": true, "android-jetpack": true,
}

var (
	// @Test (JUnit 4/5/TestNG) / @ParameterizedTest / @RepeatedTest — counts
	// the test methods. A @Test annotation may carry args (TestNG's
	// @Test(groups=…) / JUnit5 @Test on a method with throws clause), hence the
	// optional (...) group and tolerant modifier/return-type run before void.
	j5TestMethodRE = regexp.MustCompile(
		`(?s)@(Test|ParameterizedTest|RepeatedTest)\b(?:\s*\([^)]*\))?` +
			`(?:\s*@\w+(?:\s*\([^)]*\))?\s*)*\s*(?:public\s+|protected\s+|package\s+|private\s+)?(?:\w+\s+)*` +
			`(?:void|\w+(?:<[^>]*>)?)\s+(\w+)\s*\(`)
	// Lifecycle methods across JUnit 5 (BeforeAll/BeforeEach/AfterAll/AfterEach),
	// JUnit 4 (Before/After/BeforeClass/AfterClass) and TestNG
	// (BeforeMethod/AfterMethod/BeforeClass/AfterClass/BeforeSuite/AfterSuite/
	// BeforeTest/AfterTest).
	j5LifecycleRE = regexp.MustCompile(
		`(?s)@(BeforeAll|BeforeEach|AfterAll|AfterEach|BeforeClass|AfterClass|Before|After|BeforeMethod|AfterMethod|BeforeSuite|AfterSuite|BeforeTest|AfterTest)\b(?:\s*\([^)]*\))?` +
			`(?:\s*@\w+(?:\s*\([^)]*\))?\s*)*\s*(?:public\s+|protected\s+|static\s+|private\s+)*` +
			`void\s+(\w+)\s*\(`)
	j5NestedClassRE = regexp.MustCompile(
		`(?s)@Nested\b(?:\s*@\w+(?:\s*\([^)]*\))?\s*)*\s*` +
			`(?:(?:public|protected|private|static|inner)\s+)*class\s+(\w+)`)
	j5ExtendWithRE = regexp.MustCompile(
		`(?s)@(?:ExtendWith|RunWith)\s*\(\s*(?:\{([^}]*)\}|([^)]+))\s*\)`)
	j5DisabledRE = regexp.MustCompile(
		`@(?:Disabled|Ignore)\b(?:\s*\(\s*\"[^\"]*\"\s*\))?`)
	j5ClassExtRE = regexp.MustCompile(`(\w+)\.class`)

	// assertEquals(...) / assertThat(...) / assertTrue(...) / assertNull(...) /
	// fail(...) — JUnit/Hamcrest/AssertJ/TestNG assertion calls. Folded as a
	// count only (the per-assertion orphan nodes are the worst offender).
	j5AssertionRE = regexp.MustCompile(
		`(?m)\b(assert\w*|fail|verify|expectThrows|assertThrows)\s*\(`)
)

// ExtractJUnit5 runs the JUnit / TestNG extractor, emitting exactly one
// test_suite entity per test-class file (#4359).
func ExtractJUnit5(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" || !junit5Frameworks[ctx.Framework] {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath

	// ── per-file signal collection (folded onto the single suite entity) ────
	testMethods := j5TestMethodRE.FindAllStringSubmatch(source, -1)
	lifecycleMethods := j5LifecycleRE.FindAllStringSubmatch(source, -1)
	nestedMatches := j5NestedClassRE.FindAllStringSubmatch(source, -1)
	assertionCount := len(j5AssertionRE.FindAllStringIndex(source, -1))

	// Collect distinct test annotations + extension class names + nested names.
	testAnnotations := map[string]bool{}
	for _, m := range testMethods {
		testAnnotations[m[1]] = true
	}
	extensions := map[string]bool{}
	for _, m := range j5ExtendWithRE.FindAllStringSubmatch(source, -1) {
		raw := m[1]
		if raw == "" {
			raw = m[2]
		}
		for _, ext := range j5ClassExtRE.FindAllStringSubmatch(raw, -1) {
			extensions[ext[1]] = true
		}
	}
	nestedClasses := map[string]bool{}
	for _, m := range nestedMatches {
		nestedClasses[m[1]] = true
	}

	// Nothing JUnit/TestNG-shaped to model → emit nothing, so non-test Java
	// files that merely happen to flow through a junit5Frameworks token (e.g. a
	// spring_boot production class) never mint an empty suite.
	if len(testMethods) == 0 && len(lifecycleMethods) == 0 && len(nestedMatches) == 0 {
		return result
	}

	// Outer class line (for the suite entity's source line).
	line := 1
	if m := classDeclRE.FindStringIndex(source); m != nil {
		line = lineOf(source, m[0])
	}

	// ── one linked test_suite per file ──────────────────────────────────────
	outerClassName := ""
	if m := classDeclRE.FindStringSubmatch(source); m != nil {
		outerClassName = m[1]
	}
	suiteRef := "scope:pattern:junit5_suite:" + fp + ":" + junitBaseName(fp)
	props := map[string]any{
		"framework":         "junit5",
		"test_method_count": itoa(len(testMethods)),
		"lifecycle_count":   itoa(len(lifecycleMethods)),
		"nested_count":      itoa(len(nestedMatches)),
		"extension_count":   itoa(len(extensions)),
		"assertion_count":   itoa(assertionCount),
	}
	if outerClassName != "" {
		props["test_class"] = outerClassName
	}
	if len(testAnnotations) > 0 {
		props["test_annotations"] = strings.Join(sortedKeys(testAnnotations), ",")
	}
	if len(extensions) > 0 {
		props["extensions"] = strings.Join(sortedKeys(extensions), ",")
	}
	if len(nestedClasses) > 0 {
		props["nested_classes"] = strings.Join(sortedKeys(nestedClasses), ",")
	}
	if j5DisabledRE.MatchString(source) {
		props["has_disabled"] = true
	}

	suite := SecondaryEntity{
		Name:       junitBaseName(fp),
		Kind:       "SCOPE.Pattern",
		Subtype:    "test_suite",
		SourceFile: fp,
		LineStart:  line, LineEnd: line,
		Provenance: "INFERRED_FROM_JUNIT5_TEST",
		Ref:        suiteRef,
		Properties: props,
	}

	// ── TESTS edge to the production class under test (name affinity + ref) ──
	if subject := resolveJavaTestSubject(source, outerClassName); subject != "" {
		suite.Properties["tests_target"] = subject
		result.Relationships = append(result.Relationships, Relationship{
			SourceRef:        suiteRef,
			TargetRef:        "Class:" + subject,
			RelationshipType: "TESTS",
			Properties: map[string]string{
				"framework":    "junit5",
				"match_source": "java_test_name_affinity",
				"target_type":  subject,
			},
		})
	}

	result.Entities = append(result.Entities, suite)
	return result
}

// junitBaseName derives a human/file label from a Java test file path, e.g.
// `src/test/java/com/x/OrderServiceTest.java` → `OrderServiceTest`. Falls back
// to the outer-class style base when the path has no separators.
func junitBaseName(path string) string {
	p := path
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		p = p[i+1:]
	}
	return strings.TrimSuffix(p, ".java")
}

// sortedKeys returns the keys of a set in deterministic order (so folded list
// properties are stable across runs and don't churn the graph).
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var javaIdentRE = regexp.MustCompile(`^[A-Z][A-Za-z0-9_]*$`)

// subjectFromTestClassName derives the production-class name a Java test class
// affinity-maps to, by stripping the conventional Test / Tests / IT / ITCase /
// TestCase suffix or the leading Test prefix:
//
//	OrderServiceTest      → OrderService
//	OrderServiceTests     → OrderService
//	OrderServiceIT        → OrderService   (Failsafe integration test)
//	OrderServiceITCase    → OrderService
//	OrderServiceTestCase  → OrderService
//	TestOrderService      → OrderService   (TestNG/legacy prefix convention)
//
// Returns "" when nothing plausible remains.
func subjectFromTestClassName(cls string) string {
	if cls == "" {
		return ""
	}
	// Suffix conventions, longest-first so "ITCase"/"TestCase" win over "IT".
	for _, suf := range []string{"ITCase", "TestCase", "Tests", "Test", "IT"} {
		if strings.HasSuffix(cls, suf) && len(cls) > len(suf) {
			base := cls[:len(cls)-len(suf)]
			if javaIdentRE.MatchString(base) {
				return base
			}
		}
	}
	// Leading "Test" prefix (TestOrderService → OrderService).
	if strings.HasPrefix(cls, "Test") && len(cls) > len("Test") {
		base := cls[len("Test"):]
		if javaIdentRE.MatchString(base) {
			return base
		}
	}
	return ""
}

var (
	// @InjectMocks OrderService subject;  /  @Autowired OrderService subject;
	// Captures the injected field's *type*, which (for a single such field) is
	// the strongest SUT signal Mockito/Spring give us.
	reJavaInjectField = regexp.MustCompile(
		`@(?:InjectMocks|Autowired)\b(?:\s*\([^)]*\))?\s+(?:private\s+|public\s+|protected\s+|final\s+)*([A-Z][A-Za-z0-9_]*)\b`)
	// new OrderService(...) — direct construction of the SUT in the test body.
	reJavaNew = regexp.MustCompile(`\bnew\s+([A-Z][A-Za-z0-9_]*)\s*\(`)
)

// referencedJavaTypes returns the set of class names that are referenced in the
// test file via the high-confidence SUT signals: @InjectMocks / @Autowired
// field types and `new X(` construction. Only names in this set are eligible to
// become a TESTS subject, keeping the edge pointed at an in-repo production
// entity rather than a fixture/util/JDK type.
func referencedJavaTypes(src string) map[string]bool {
	out := map[string]bool{}
	for _, m := range reJavaInjectField.FindAllStringSubmatch(src, -1) {
		if javaIdentRE.MatchString(m[1]) && !primitiveTypes[m[1]] {
			out[m[1]] = true
		}
	}
	for _, m := range reJavaNew.FindAllStringSubmatch(src, -1) {
		if javaIdentRE.MatchString(m[1]) && !primitiveTypes[m[1]] {
			out[m[1]] = true
		}
	}
	return out
}

// resolveJavaTestSubject determines the unique production class under test for a
// Java test-class file. A subject is emitted ONLY when it is both (a) derivable
// by name affinity from the test class name, AND (b) actually referenced
// (@InjectMocks / @Autowired / `new X(`) in the file. Requiring BOTH keeps the
// TESTS edge conservative and unique — name affinity alone would over-link a
// renamed/wrapper class, and a referenced type alone would link a collaborator
// or fixture. Returns "" when no confident unique subject exists.
func resolveJavaTestSubject(src, testClassName string) string {
	subject := subjectFromTestClassName(testClassName)
	if subject == "" {
		return ""
	}
	if referencedJavaTypes(src)[subject] {
		return subject
	}
	return ""
}
