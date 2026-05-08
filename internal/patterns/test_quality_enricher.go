package patterns

import (
	"regexp"

	"github.com/cajasmota/archigraph/internal/types"
)

// testQualityEnricher detects test quality signals (integration tests, real DB, etc).
// Matches Python test_quality_enricher.py.
type testQualityEnricher struct{}

var (
	tqJavaSpringBootTestRE = regexp.MustCompile(`@SpringBootTest\b`)
	tqJavaDataJpaTestRE    = regexp.MustCompile(`@DataJpaTest\b`)
	tqJavaTestcontainersRE = regexp.MustCompile(`\borg\.testcontainers\b`)
	tqPyTestcontainersRE   = regexp.MustCompile(`\btestcontainers\b`)
	tqPyDBFixtureRE        = regexp.MustCompile(`(?:db_session|db_engine|database_url)\s*=`)
	tqJSBeforeAllRE        = regexp.MustCompile(`\bbeforeAll\s*\(`)
	tqJSDBConnectRE        = regexp.MustCompile(`(?:pg\.Pool|mysql\.createConnection|mongoose\.connect)\s*\(`)
	tqJSSupertestRE        = regexp.MustCompile(`\bsupertest\b`)
	tqGoSQLOpenTestMainRE  = regexp.MustCompile(`(?s)func\s+TestMain\s*\(.*?\bsql\.Open\b`)
	tqGoIntegrationTagRE   = regexp.MustCompile(`(?m)^//go:build\s+integration`)
)

func (t *testQualityEnricher) Category() string { return "test_quality" }

func (t *testQualityEnricher) AppliesTo(src string) bool {
	return tqJavaSpringBootTestRE.MatchString(src) ||
		tqJavaDataJpaTestRE.MatchString(src) ||
		tqJavaTestcontainersRE.MatchString(src) ||
		tqPyTestcontainersRE.MatchString(src) ||
		tqPyDBFixtureRE.MatchString(src) ||
		tqJSBeforeAllRE.MatchString(src) ||
		tqJSSupertestRE.MatchString(src) ||
		tqGoSQLOpenTestMainRE.MatchString(src) ||
		tqGoIntegrationTagRE.MatchString(src)
}

func (t *testQualityEnricher) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	emit := func(key, name, signal, testType string, line int) {
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			name, "SCOPE.Pattern", "test_quality", language, line,
			map[string]string{
				"kind":      "test_quality",
				"signal":    signal,
				"test_type": testType,
			}))
	}

	if m := tqJavaSpringBootTestRE.FindStringIndex(src); m != nil {
		emit("java:spring_boot_test", "test_quality_spring_boot", "spring_integration_test", "integration", lineOf(src, m[0]))
	}
	if m := tqJavaTestcontainersRE.FindStringIndex(src); m != nil {
		emit("java:testcontainers", "test_quality_testcontainers_java", "testcontainers", "integration", lineOf(src, m[0]))
	}
	if m := tqPyTestcontainersRE.FindStringIndex(src); m != nil {
		emit("py:testcontainers", "test_quality_testcontainers_py", "testcontainers", "integration", lineOf(src, m[0]))
	}
	if m := tqPyDBFixtureRE.FindStringIndex(src); m != nil {
		emit("py:db_fixture", "test_quality_py_db_fixture", "db_fixture", "integration", lineOf(src, m[0]))
	}
	if m := tqJSDBConnectRE.FindStringIndex(src); m != nil {
		emit("js:db_connect", "test_quality_js_db", "real_db_connection", "integration", lineOf(src, m[0]))
	}
	if m := tqJSSupertestRE.FindStringIndex(src); m != nil {
		emit("js:supertest", "test_quality_supertest", "http_integration", "integration", lineOf(src, m[0]))
	}
	if m := tqGoSQLOpenTestMainRE.FindStringIndex(src); m != nil {
		emit("go:sql_open_test_main", "test_quality_go_db", "real_db_test_main", "integration", lineOf(src, m[0]))
	}
	if m := tqGoIntegrationTagRE.FindStringIndex(src); m != nil {
		emit("go:build_tag_integration", "test_quality_go_integration_tag", "build_tag", "integration", lineOf(src, m[0]))
	}

	return results
}

func init() {
	Register(&testQualityEnricher{})
}
