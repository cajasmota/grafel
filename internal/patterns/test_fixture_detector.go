package patterns

import (
	"regexp"

	"github.com/cajasmota/archigraph/internal/types"
)

// testFixtureDetector detects test fixture and setup patterns.
// Matches Python test_fixture_detector.py.
type testFixtureDetector struct{}

var (
	tfPytestFixtureTrigRE = regexp.MustCompile(`@pytest\.fixture\b`)
	tfPytestFixtureRE     = regexp.MustCompile(`@pytest\.fixture\s*(?:\([^)]*\))?\s*\ndef\s+(\w+)`)
	tfFactoryDefineRE     = regexp.MustCompile(`factory_boy|Factory\.define|FactoryGirl\.define|FactoryBot\.define`)
	tfJestSetupFilesRE    = regexp.MustCompile(`(?:beforeAll|beforeEach|afterAll|afterEach)\s*\(`)
	tfSpringTestCfgRE     = regexp.MustCompile(`@(?:TestConfiguration|SpringBootTest|DataJpaTest)\b`)
	tfJUnitBeforeRE       = regexp.MustCompile(`@(?:BeforeEach|BeforeAll|Before|BeforeClass)\b`)
	tfGoTestMainRE        = regexp.MustCompile(`(?m)^func\s+TestMain\s*\(`)
	tfGoSetupRE           = regexp.MustCompile(`func\s+setup\s*\(\s*\)|func\s+teardown\s*\(\s*\)`)
	tfRubyRSpecRE         = regexp.MustCompile(`(?:before\s*(?:\(:each\)|\(:all\)|\(:suite\))|let!\?\s*\()`)
)

func (t *testFixtureDetector) Category() string { return "test_fixture" }

func (t *testFixtureDetector) AppliesTo(src string) bool {
	return tfPytestFixtureTrigRE.MatchString(src) ||
		tfFactoryDefineRE.MatchString(src) ||
		tfJestSetupFilesRE.MatchString(src) ||
		tfSpringTestCfgRE.MatchString(src) ||
		tfJUnitBeforeRE.MatchString(src) ||
		tfGoTestMainRE.MatchString(src) ||
		tfRubyRSpecRE.MatchString(src)
}

func (t *testFixtureDetector) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	emit := func(key, name, fixKind string, line int) {
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			name, "SCOPE.Pattern", "test_fixture", language, line,
			map[string]string{"kind": "test_fixture", "fixture_kind": fixKind}))
	}

	// pytest fixtures
	for _, m := range tfPytestFixtureRE.FindAllStringSubmatchIndex(src, -1) {
		fixtureName := src[m[2]:m[3]]
		emit("pytest:"+fixtureName, "fixture_pytest_"+fixtureName, "pytest_fixture", lineOf(src, m[0]))
	}

	// factory_boy / FactoryBot
	if m := tfFactoryDefineRE.FindStringIndex(src); m != nil {
		emit("factory_bot", "fixture_factory_bot", "factory", lineOf(src, m[0]))
	}

	// Jest beforeAll/beforeEach
	for _, m := range tfJestSetupFilesRE.FindAllStringIndex(src, -1) {
		emit("jest:lifecycle", "fixture_jest_lifecycle", "jest_lifecycle", lineOf(src, m[0]))
		break
	}

	// Spring @TestConfiguration
	if m := tfSpringTestCfgRE.FindStringIndex(src); m != nil {
		emit("spring:test_cfg", "fixture_spring_test_config", "spring_test_config", lineOf(src, m[0]))
	}

	// JUnit @BeforeEach
	if m := tfJUnitBeforeRE.FindStringIndex(src); m != nil {
		emit("junit:before", "fixture_junit_before", "junit_lifecycle", lineOf(src, m[0]))
	}

	// Go TestMain
	if m := tfGoTestMainRE.FindStringIndex(src); m != nil {
		emit("go:test_main", "fixture_go_test_main", "go_test_main", lineOf(src, m[0]))
	}

	// Go setup/teardown
	if m := tfGoSetupRE.FindStringIndex(src); m != nil {
		emit("go:setup", "fixture_go_setup", "go_setup", lineOf(src, m[0]))
	}

	// RSpec before
	if m := tfRubyRSpecRE.FindStringIndex(src); m != nil {
		emit("rspec:before", "fixture_rspec_before", "rspec_lifecycle", lineOf(src, m[0]))
	}

	return results
}

func init() {
	Register(&testFixtureDetector{})
}
