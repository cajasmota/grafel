package patterns

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// sharedTestHelperDetector detects shared test helper/fixture files.
// Matches Python shared_test_helper_detector.py.
type sharedTestHelperDetector struct{}

var (
	sthJSMocksDirRE      = regexp.MustCompile(`(?:^|/)__mocks__/[^/]+\.(?:ts|js)$`)
	sthGoTestingGoRE     = regexp.MustCompile(`(?:^|/)[^/]*_test[^/]*/testing\.go$`)
	sthJavaHelperSuffRE  = regexp.MustCompile(`(?:TestHelper|TestUtils|TestFixture)\.java$`)
	sthJavaSupportPathRE = regexp.MustCompile(`src/test/java/.*?/support/[^/]+\.java$`)
	sthKotlinHelperRE    = regexp.MustCompile(`(?:TestHelper|TestUtils|TestFixture)\.kt$`)
	sthRubySpecSupportRE = regexp.MustCompile(`(?:^|/)spec/support/[^/]+\.rb$`)
	sthCSharpHelperRE    = regexp.MustCompile(`(?:TestBase|TestFixture|TestHelper)\.cs$`)
	sthSwiftHelperRE     = regexp.MustCompile(`TestHelper\.swift$`)
)

func (s *sharedTestHelperDetector) Category() string { return "shared_test_helper" }

func (s *sharedTestHelperDetector) AppliesTo(src string) bool {
	return true // path-based, always evaluate
}

func (s *sharedTestHelperDetector) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord

	isTestHelper := false
	helperType := ""

	switch {
	case sthJSMocksDirRE.MatchString(filePath):
		isTestHelper = true
		helperType = "js_mock"
	case sthGoTestingGoRE.MatchString(filePath):
		isTestHelper = true
		helperType = "go_testing_helper"
	case sthJavaHelperSuffRE.MatchString(filePath):
		isTestHelper = true
		helperType = "java_test_helper"
	case sthJavaSupportPathRE.MatchString(filePath):
		isTestHelper = true
		helperType = "java_test_support"
	case sthKotlinHelperRE.MatchString(filePath):
		isTestHelper = true
		helperType = "kotlin_test_helper"
	case sthRubySpecSupportRE.MatchString(filePath):
		isTestHelper = true
		helperType = "ruby_spec_support"
	case sthCSharpHelperRE.MatchString(filePath):
		isTestHelper = true
		helperType = "csharp_test_helper"
	case sthSwiftHelperRE.MatchString(filePath):
		isTestHelper = true
		helperType = "swift_test_helper"
	case strings.Contains(filePath, "conftest.py"):
		isTestHelper = true
		helperType = "pytest_conftest"
	case strings.Contains(filePath, "test_helpers") || strings.Contains(filePath, "testhelpers"):
		isTestHelper = true
		helperType = "generic_test_helper"
	}

	if isTestHelper {
		results = append(results, makeEntity(filePath,
			"shared_test_helper_"+helperType,
			"SCOPE.Pattern", "shared_test_helper", language, 1,
			map[string]string{"kind": "shared_test_helper", "helper_type": helperType}))
	}

	return results
}

func init() {
	Register(&sharedTestHelperDetector{})
}
