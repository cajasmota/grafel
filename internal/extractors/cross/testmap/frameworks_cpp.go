// Package testmap — C/C++ test-framework detection and call resolution.
//
// Deep linkage (#3495) for the six dominant C/C++ unit-test frameworks. Each
// detector returns the test cases in a file together with each case's
// brace-delimited body so the shared resolver can scan it for direct
// production calls (high), the suite/fixture subject (medium), or the
// file-name convention (low).
//
//	gtest (GoogleTest):
//	    TEST(Suite, Name) / TEST_F(Fixture, Name) / TEST_P(Fixture, Name).
//	    The Suite/Fixture name is the subject-under-test; the body is scanned
//	    for production calls. EXPECT_*/ASSERT_* assertion macros are
//	    stop-worded (see resolver.go).
//
//	catch2 / doctest:
//	    TEST_CASE("name", "[tag]") with optional SECTION("…") leaves. Both
//	    share the same surface (doctest deliberately mirrors Catch2), so one
//	    detector serves both. CHECK/REQUIRE assertions are stop-worded.
//
//	boost.test:
//	    BOOST_AUTO_TEST_CASE(name) / BOOST_FIXTURE_TEST_CASE(name, Fixture).
//	    The fixture (when present) is the subject; BOOST_CHECK/REQUIRE macros
//	    are stop-worded.
//
//	cppunit:
//	    CPPUNIT_TEST(testMethod) registrations inside a TestFixture, plus the
//	    `void Class::testMethod()` method definitions. The class name (minus a
//	    trailing "Test") is the subject; CPPUNIT_ASSERT* macros are
//	    stop-worded.
//
//	cpputest:
//	    TEST(group, name) / TEST_GROUP(group). The group name is the subject;
//	    CHECK*/LONGS_EQUAL/STRCMP_EQUAL macros are stop-worded.
//
// C/C++ bodies are brace-delimited, so all detectors reuse the shared
// extractBraceBody helper.
package testmap

import (
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Shared C/C++ subject helpers
// ---------------------------------------------------------------------------

// cppStripTestAffix removes a leading or trailing "Test"/"Tests"/"Fixture"
// affix from a gtest suite / cppunit class name so the residual is the
// production subject. Examples:
//
//	UserServiceTest   → UserService
//	TestUserService   → UserService
//	AccountFixture    → Account
//	CalculatorTests   → Calculator
//
// When stripping would empty the name (e.g. the name is exactly "Test") the
// original is returned unchanged so the caller still has a usable subject.
func cppStripTestAffix(name string) string {
	for _, suf := range []string{"Tests", "Test", "Fixture", "Suite", "Spec"} {
		if strings.HasSuffix(name, suf) && len(name) > len(suf) {
			return name[:len(name)-len(suf)]
		}
	}
	if strings.HasPrefix(name, "Test") && len(name) > len("Test") {
		return name[len("Test"):]
	}
	return name
}

// ---------------------------------------------------------------------------
// gtest — GoogleTest
// ---------------------------------------------------------------------------

// gtestCaseRE matches a GoogleTest case macro:
//
//	TEST(SuiteName, TestName) { … }
//	TEST_F(FixtureName, TestName) { … }
//	TEST_P(FixtureName, TestName) { … }
//	TYPED_TEST(SuiteName, TestName) { … }
//	TYPED_TEST_P(SuiteName, TestName) { … }
//
// Group 1 = suite/fixture name (subject), group 2 = test name.
var gtestCaseRE = regexp.MustCompile(
	`(?m)\b(?:TYPED_TEST_P|TYPED_TEST|TEST_F|TEST_P|TEST)\s*\(\s*(\w+)\s*,\s*(\w+)\s*\)`,
)

func detectGTest(source string) []testFunction {
	var out []testFunction
	seen := map[string]bool{}
	for _, m := range gtestCaseRE.FindAllStringSubmatchIndex(source, -1) {
		suite := source[m[2]:m[3]]
		name := source[m[4]:m[5]]
		qname := suite + "_" + name
		if seen[qname] {
			continue
		}
		seen[qname] = true
		// Body is the { … } block following the macro's closing paren.
		body := extractBraceBody(source, m[1])
		out = append(out, testFunction{
			qname:           qname,
			body:            body,
			describeSubject: cppStripTestAffix(suite),
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// catch2 / doctest — TEST_CASE("name", "[tag]")
// ---------------------------------------------------------------------------

// catch2CaseRE matches a Catch2 / doctest test case:
//
//	TEST_CASE("name") { … }
//	TEST_CASE("name", "[tag]") { … }
//	SCENARIO("name") { … }              (Catch2 BDD)
//	TEST_CASE_METHOD(Fixture, "name") { … }
//
// Group 1 = the fixture name for TEST_CASE_METHOD (else empty), group 2 = the
// description string. The leading optional `(\w+)\s*,` only fires for the
// _METHOD form because the description is always a quoted string.
var catch2CaseRE = regexp.MustCompile(
	`(?m)\b(?:TEST_CASE_METHOD\s*\(\s*(\w+)\s*,|TEST_CASE|SCENARIO)\s*(?:\(\s*)?["']([^"']{1,200})["']`,
)

func detectCatch2(source string) []testFunction {
	var out []testFunction
	seen := map[string]bool{}
	for _, m := range catch2CaseRE.FindAllStringSubmatchIndex(source, -1) {
		var fixture string
		if m[2] >= 0 {
			fixture = source[m[2]:m[3]]
		}
		desc := source[m[4]:m[5]]
		qname := jestCaseQName(desc) // reuse JS normaliser: spaces → underscores, "it_" prefix
		if qname == "" || seen[qname] {
			continue
		}
		seen[qname] = true
		body := extractBraceBody(source, m[1])
		subject := ""
		if fixture != "" {
			subject = cppStripTestAffix(fixture)
		}
		out = append(out, testFunction{
			qname:           qname,
			body:            body,
			describeSubject: subject,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// boost.test — BOOST_AUTO_TEST_CASE / BOOST_FIXTURE_TEST_CASE
// ---------------------------------------------------------------------------

// boostCaseRE matches a Boost.Test case:
//
//	BOOST_AUTO_TEST_CASE(name) { … }
//	BOOST_FIXTURE_TEST_CASE(name, Fixture) { … }
//	BOOST_AUTO_TEST_CASE_TEMPLATE(name, T, types) { … }
//
// Group 1 = test name, group 2 = fixture name (only for the FIXTURE form).
var boostCaseRE = regexp.MustCompile(
	`(?m)\bBOOST_(?:FIXTURE_TEST_CASE|AUTO_TEST_CASE_TEMPLATE|AUTO_TEST_CASE)\s*\(\s*(\w+)\s*(?:,\s*(\w+))?`,
)

func detectBoostTest(source string) []testFunction {
	var out []testFunction
	seen := map[string]bool{}
	for _, m := range boostCaseRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		var fixture string
		if m[4] >= 0 {
			fixture = source[m[4]:m[5]]
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		body := extractBraceBody(source, m[1])
		subject := ""
		if fixture != "" {
			subject = cppStripTestAffix(fixture)
		}
		out = append(out, testFunction{
			qname:           name,
			body:            body,
			describeSubject: subject,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// cppunit — CPPUNIT_TEST registrations + void Class::testMethod() bodies
// ---------------------------------------------------------------------------

// cppunitScopedMethodRE matches an OUT-OF-LINE CppUnit test-method definition:
//
//	void UserServiceTest::testAddUser() { … }
//
// Group 1 = test fixture class, group 2 = method name.
var cppunitScopedMethodRE = regexp.MustCompile(
	`(?m)\bvoid\s+(\w+)\s*::\s*(\w+)\s*\(\s*\)`,
)

// cppunitInlineMethodRE matches an INLINE method definition (inside the fixture
// class body): `void testAddUser() {`. Group 1 = method name. The fixture class
// is taken from the CPPUNIT_TEST_SUITE(Class) macro since the inline form omits
// the scope qualifier.
var cppunitInlineMethodRE = regexp.MustCompile(
	`(?m)\bvoid\s+(\w+)\s*\(\s*\)\s*\{`,
)

// cppunitRegistrationRE matches the CPPUNIT_TEST(method) registration macro so
// we only treat method definitions as tests when they are actually registered
// (avoids picking up helper methods on the same fixture class).
var cppunitRegistrationRE = regexp.MustCompile(
	`\bCPPUNIT_TEST\s*\(\s*(\w+)\s*\)`,
)

// cppunitSuiteRE matches CPPUNIT_TEST_SUITE(Class) so the inline-method form can
// recover the fixture class name.
var cppunitSuiteRE = regexp.MustCompile(
	`\bCPPUNIT_TEST_SUITE\s*\(\s*(\w+)\s*\)`,
)

func detectCppUnit(source string) []testFunction {
	// Collect the set of registered test method names.
	registered := map[string]bool{}
	for _, m := range cppunitRegistrationRE.FindAllStringSubmatch(source, -1) {
		registered[m[1]] = true
	}
	// Fixture class declared by the suite macro (used for the inline form).
	suiteClass := ""
	if m := cppunitSuiteRE.FindStringSubmatch(source); m != nil {
		suiteClass = m[1]
	}

	var out []testFunction
	seen := map[string]bool{}
	emit := func(class, method string, bodyStart int) {
		if class == "" {
			class = "CppUnit"
		}
		qname := class + "_" + method
		if seen[qname] {
			return
		}
		seen[qname] = true
		out = append(out, testFunction{
			qname:           qname,
			body:            extractBraceBody(source, bodyStart),
			describeSubject: cppStripTestAffix(class),
		})
	}

	registeredOrTest := func(method string) bool {
		if len(registered) > 0 {
			return registered[method]
		}
		return strings.HasPrefix(method, "test")
	}

	// Out-of-line definitions: void Class::method() { … }.
	for _, m := range cppunitScopedMethodRE.FindAllStringSubmatchIndex(source, -1) {
		class := source[m[2]:m[3]]
		method := source[m[4]:m[5]]
		if !registeredOrTest(method) {
			continue
		}
		emit(class, method, m[1])
	}
	// Inline definitions inside the fixture body: void method() { … }.
	for _, m := range cppunitInlineMethodRE.FindAllStringSubmatchIndex(source, -1) {
		method := source[m[2]:m[3]]
		if !registeredOrTest(method) {
			continue
		}
		// Pass the match START so extractBraceBody locates this method's own
		// opening brace (cppunitInlineMethodRE ends AT the `{`, so m[1] is just
		// past it and would make the extractor seek the next brace).
		emit(suiteClass, method, m[0])
	}
	return out
}

// ---------------------------------------------------------------------------
// cpputest — TEST(group, name) / TEST_GROUP(group)
// ---------------------------------------------------------------------------

// cpputestCaseRE matches a CppUTest case:
//
//	TEST(GroupName, TestName) { … }
//	IGNORE_TEST(GroupName, TestName) { … }
//
// Group 1 = group name (subject), group 2 = test name. This shares the
// TEST(a, b) surface with gtest; the cpputest framework entry is only selected
// for files whose imports/markers name CppUTest (CppUTest/TestHarness.h), so
// the overlap does not cause cross-framework confusion.
var cpputestCaseRE = regexp.MustCompile(
	`(?m)\b(?:IGNORE_TEST|TEST)\s*\(\s*(\w+)\s*,\s*(\w+)\s*\)`,
)

func detectCppUTest(source string) []testFunction {
	var out []testFunction
	seen := map[string]bool{}
	for _, m := range cpputestCaseRE.FindAllStringSubmatchIndex(source, -1) {
		group := source[m[2]:m[3]]
		name := source[m[4]:m[5]]
		qname := group + "_" + name
		if seen[qname] {
			continue
		}
		seen[qname] = true
		body := extractBraceBody(source, m[1])
		out = append(out, testFunction{
			qname:           qname,
			body:            body,
			describeSubject: cppStripTestAffix(group),
		})
	}
	return out
}
