// Package testmap implements the cross-language test-to-production mapping extractor.
//
// Scans source files that look like test files and, for every test function
// found, emits one or more SCOPE.Pattern entities (subtype "test_coverage")
// together with TESTS relationship edges pointing at the production functions
// they exercise. See for the rationale on the Pattern kind.
//
// Detection strategy:
//
//  1. Decide whether the file is a test file:
//     a. test framework import present (pytest, junit, jest, rspec, …), OR
//     b. file name matches a known convention (*_test.go, test_*.py, …).
//     Files that are neither are skipped — the hot path on non-test files is
//     a handful of import-token regex matches.
//  2. Identify every test function in the file via framework-specific naming
//     rules (TestXxx for Go, test_* / Test* class for pytest, it()/test()/describe()
//     for Jest, @Test for JUnit, xxxSpec for Spock, …).
//  3. For each test function, look for direct calls to production identifiers
//     inside its body. A call whose target identifier matches a known
//     production symbol (or, absent a symbol table, any non-test identifier)
//     produces a TESTS edge with confidence=high.
//  4. If step 3 yields nothing, look for a mock set-up line whose target name
//     matches a production identifier → confidence=medium.
//  5. If step 4 yields nothing, fall back to the file-name convention
//     (foo_test.go → foo.go, test_user.py → user.py, UserTest.java → User.java)
//     and emit a single low-confidence TESTS edge targeting that production
//     file's likely top-level symbol.
//
// Entity kind:       "SCOPE.Pattern" with subtype "test_coverage"
// Relationship kind: "TESTS"  (test function → production function)
//
// Coverage records emit as SCOPE.Pattern (with subtype "test_coverage"
// and the test framework on properties.test_framework) because the 14-type
// SCOPE allowlist does not include a standalone "TestCoverage" or "Test" type.
// SCOPE.Pattern is the canonical bucket for inferred structural patterns and
// the TESTS relationship edge already carries the precise test→prod link.
//
// OTel span:   indexer.test_coverage_extract
// Attributes:  language, test_framework, tests_found, file_path
//
// Registration key: "_cross_testmap"
package testmap

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("_cross_testmap", &Extractor{})
}

// Extractor implements extractor.Extractor for test→production mapping.
type Extractor struct{}

// Language returns the registration key.
func (e *Extractor) Language() string { return "_cross_testmap" }

// ---------------------------------------------------------------------------
// Test-type inference
// ---------------------------------------------------------------------------

// inferTestType derives "unit" | "integration" | "e2e" from the file path.
// A path containing "/e2e/" or "_e2e" is tagged e2e first, then integration,
// then falls through to unit.
func inferTestType(path string) string {
	lower := strings.ToLower(filepath.ToSlash(path))
	switch {
	case strings.Contains(lower, "/e2e/"),
		strings.Contains(lower, "_e2e"),
		strings.Contains(lower, ".e2e."),
		strings.HasSuffix(lower, "e2e.test.ts"),
		strings.HasSuffix(lower, "e2e.spec.ts"):
		return "e2e"
	case strings.Contains(lower, "/integration/"),
		strings.Contains(lower, "_integration"),
		strings.Contains(lower, ".integration."),
		strings.HasSuffix(lower, "integration.test.ts"),
		strings.HasSuffix(lower, "integration.spec.ts"):
		return "integration"
	}
	return "unit"
}

// ---------------------------------------------------------------------------
// Production file heuristics (naming-convention fallback)
// ---------------------------------------------------------------------------

// productionFileFromTestPath returns the best-guess production file path
// that a given test file corresponds to, together with a best-guess
// production symbol name derived from the file stem. Both may be empty
// when the convention does not apply.
//
// The input path is a repo-relative slash path (a scope URI, not an OS
// filesystem path). path.Dir / path.Join are used intentionally so the
// output always uses forward slashes regardless of host OS.
func productionFileFromTestPath(filePath string) (prodFile, prodSymbol string) {
	// Ensure forward slashes regardless of OS (paths are repo-relative URIs).
	filePath = filepath.ToSlash(filePath)
	base := path.Base(filePath)
	dir := path.Dir(filePath)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	lowerStem := strings.ToLower(stem)

	switch ext {
	case ".go":
		// foo_test.go → foo.go
		if strings.HasSuffix(lowerStem, "_test") {
			prodStem := stem[:len(stem)-len("_test")]
			return path.Join(dir, prodStem+".go"), titleCase(prodStem)
		}
	case ".py":
		// test_foo.py or foo_test.py → foo.py
		switch {
		case strings.HasPrefix(lowerStem, "test_"):
			prodStem := stem[len("test_"):]
			return path.Join(dir, prodStem+".py"), prodStem
		case strings.HasSuffix(lowerStem, "_test"):
			prodStem := stem[:len(stem)-len("_test")]
			return path.Join(dir, prodStem+".py"), prodStem
		}
	case ".ts", ".tsx", ".js", ".jsx":
		// foo.test.ts / foo.spec.ts → foo.ts (or similar)
		for _, suf := range []string{".test", ".spec"} {
			if strings.HasSuffix(lowerStem, suf) {
				prodStem := stem[:len(stem)-len(suf)]
				return path.Join(dir, prodStem+ext), prodStem
			}
		}
	case ".rb":
		// foo_spec.rb → foo.rb
		if strings.HasSuffix(lowerStem, "_spec") {
			prodStem := stem[:len(stem)-len("_spec")]
			return path.Join(dir, prodStem+".rb"), prodStem
		}
	case ".java":
		// XxxTest.java / XxxTests.java / XxxIT.java → Xxx.java
		for _, suf := range []string{"Tests", "Test", "IT"} {
			if strings.HasSuffix(stem, suf) && len(stem) > len(suf) {
				prodStem := stem[:len(stem)-len(suf)]
				return path.Join(dir, prodStem+".java"), prodStem
			}
		}
	case ".kt":
		for _, suf := range []string{"Tests", "Test"} {
			if strings.HasSuffix(stem, suf) && len(stem) > len(suf) {
				prodStem := stem[:len(stem)-len(suf)]
				return path.Join(dir, prodStem+".kt"), prodStem
			}
		}
	case ".scala":
		// FooSpec.scala / FooTest.scala → Foo.scala
		for _, suf := range []string{"Spec", "Test"} {
			if strings.HasSuffix(stem, suf) && len(stem) > len(suf) {
				prodStem := stem[:len(stem)-len(suf)]
				return path.Join(dir, prodStem+".scala"), prodStem
			}
		}
	case ".cs":
		for _, suf := range []string{"Tests", "Test"} {
			if strings.HasSuffix(stem, suf) && len(stem) > len(suf) {
				prodStem := stem[:len(stem)-len(suf)]
				return path.Join(dir, prodStem+".cs"), prodStem
			}
		}
	case ".rs":
		// no standard filename convention for Rust tests — leave empty
	case ".php":
		// FooTest.php → Foo.php
		if strings.HasSuffix(stem, "Test") && len(stem) > len("Test") {
			prodStem := stem[:len(stem)-len("Test")]
			return path.Join(dir, prodStem+".php"), prodStem
		}
	case ".swift":
		for _, suf := range []string{"Tests", "Test"} {
			if strings.HasSuffix(stem, suf) && len(stem) > len(suf) {
				prodStem := stem[:len(stem)-len(suf)]
				return path.Join(dir, prodStem+".swift"), prodStem
			}
		}
	}
	return "", ""
}

// titleCase upper-cases the first rune of s. Used to turn a Go file stem
// ("handler") into its idiomatic symbol guess ("Handler"). Returns s
// unchanged when s is empty.
func titleCase(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 'a' - 'A'
	}
	return string(r)
}

// ---------------------------------------------------------------------------
// Entity / relationship builders
// ---------------------------------------------------------------------------

// testCoverageEntityID builds a stable identity string for a TestCoverage entity.
// Format: "scope:testcoverage:<file>#<test>→<prod>"
func testCoverageEntityID(filePath, testQName, prodQName string) string {
	return "scope:testcoverage:" + filePath + "#" + testQName + "->" + prodQName
}

// testFunctionRef builds the ref used for the TESTS edge source — the already
// extracted production Operation representation of the test function itself.
func testFunctionRef(filePath, testQName string) string {
	return "scope:operation:" + filePath + "#" + testQName
}

// productionFunctionRef builds the ref used for the TESTS edge target. When
// prodFile is empty we fall back to the test file's own path — this keeps the
// ref syntactically valid even when convention-only fallback could not guess a
// file.
func productionFunctionRef(prodFile, prodQName string) string {
	if prodQName == "" {
		return ""
	}
	if prodFile == "" {
		return "scope:operation:" + "?" + "#" + prodQName
	}
	return "scope:operation:" + prodFile + "#" + prodQName
}

// buildCollapsedEntity assembles a single SCOPE.Pattern EntityRecord
// (subtype "test_coverage") for one test function, collapsing ALL
// testedCalls into TESTS edges on the same record.
//
// Issue #2080: the previous design emitted one entity per (test, production)
// pair. For a test with N parametrize sets or multiple production calls that
// meant N separate SCOPE.Pattern nodes. Each node's entity ID was distinct
// from the TESTS edge's FromID (which used scope:operation: refs), so the
// nodes never appeared in the graph's "touched" set and were all counted as
// degree-0 orphans.
//
// The fix: one entity per test function with all TESTS edges embedded.
// Each edge carries FromID = testCoverageEntityID(...), the same stub that
// is stored in Properties["ref"]. The resolver indexes this stub via the
// Properties["ref"] → byQualifiedName path (BuildIndex, scope:testcoverage:
// branch) so the assembly resolves the FromID to the entity's own hex ID.
// This makes the entity the FROM node of its own TESTS edges, ensuring it
// appears in the "touched" set regardless of how many @pytest.mark.parametrize
// parameter sets the test function has.
//
// Properties carry the data of the highest-confidence testedCall so that
// existing consumers using Properties["tested_function"] continue to work.
func buildCollapsedEntity(
	filePath string,
	language string,
	framework string,
	testType string,
	testQName string,
	calls []testedCall,
) types.EntityRecord {
	// Primary call: highest-confidence entry (calls is already sorted high→low).
	var primary testedCall
	if len(calls) > 0 {
		primary = calls[0]
	}

	entityID := testCoverageEntityID(filePath, testQName, primary.qname)
	props := map[string]string{
		"test_framework":  framework,
		"test_type":       testType,
		"tested_function": primary.qname,
		"confidence":      primary.confidence,
		"test_function":   testQName,
		"ref":             entityID,
		"provenance":      confidenceProvenance(primary.confidence),
		// pattern_kind preserves the original semantic ("test_coverage")
		// after we collapsed the entity Kind into the SCOPE.Pattern bucket so
		// the graph allowlist validation passes.
		"pattern_kind": "test_coverage",
	}
	// tested_file is only stamped for the low-confidence naming-convention
	// fallback (issue #2060) — for high/medium calls prodFile is now also
	// populated as a resolver hint, but the file is a convention guess and
	// not a verified location, so we keep the property meaning ("the
	// best-guess tested file when no direct call/mock was found").
	if primary.prodFile != "" && primary.confidence == "low" {
		props["tested_file"] = primary.prodFile
	}

	name := testQName
	if primary.qname != "" {
		name = testQName + " -> " + primary.qname
	}

	rec := types.EntityRecord{
		Name: name,
		// SCOPE.TestCoverage is not in the 14-type allowlist.
		// Coverage records map to SCOPE.Pattern (canonical bucket for inferred
		// structural patterns); the framework-specific test type is preserved on
		// Subtype, and the originating test framework remains on Properties.
		Kind:         "SCOPE.Pattern",
		SourceFile:   filePath,
		Language:     language,
		Subtype:      testType,
		Properties:   props,
		QualityScore: confidenceScore(primary.confidence),
	}

	// Emit one TESTS edge per unique production call. FromID is set to
	// entityID (the scope:testcoverage: stub stored in Properties["ref"]).
	// The resolver indexes this stub under byQualifiedName via the
	// scope:testcoverage: ref-property branch in BuildIndex, so the assembly
	// rewrites FromID to the entity's own hex ID. The entity then appears in
	// the orphan-classifier's "touched" set as the source of a TESTS edge.
	for _, tc := range calls {
		toID := productionFunctionRef(tc.prodFile, tc.qname)
		if toID == "" {
			continue
		}
		rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
			FromID: entityID,
			ToID:   toID,
			Kind:   "TESTS",
			Properties: map[string]string{
				"test_framework": framework,
				"confidence":     tc.confidence,
				"test_function":  testQName,
				"tested":         tc.qname,
			},
		})
	}

	return rec
}

// buildEntity assembles a SCOPE.Pattern EntityRecord (subtype "test_coverage")
// plus its TESTS edge from the test function to the production function.
//
// Deprecated: prefer buildCollapsedEntity which emits one entity per test
// function with all TESTS edges collapsed in, avoiding degree-0 orphans.
// Retained for any callers that need the original one-entity-per-call form.
func buildEntity(
	filePath string,
	language string,
	framework string,
	testType string,
	testQName string,
	tc testedCall,
) types.EntityRecord {
	entityID := testCoverageEntityID(filePath, testQName, tc.qname)
	props := map[string]string{
		"test_framework":  framework,
		"test_type":       testType,
		"tested_function": tc.qname,
		"confidence":      tc.confidence,
		"test_function":   testQName,
		"ref":             entityID,
		"provenance":      confidenceProvenance(tc.confidence),
		// pattern_kind preserves the original semantic ("test_coverage")
		// after we collapsed the entity Kind into the SCOPE.Pattern bucket so the graph
		// allowlist validation passes.
		"pattern_kind": "test_coverage",
	}
	// tested_file is only stamped for the low-confidence naming-convention
	// fallback (issue #2060) — for high/medium calls prodFile is now also
	// populated as a resolver hint, but the file is a convention guess and
	// not a verified location, so we keep the property meaning ("the
	// best-guess tested file when no direct call/mock was found").
	if tc.prodFile != "" && tc.confidence == "low" {
		props["tested_file"] = tc.prodFile
	}

	rec := types.EntityRecord{
		Name: testQName + " -> " + tc.qname,
		// SCOPE.TestCoverage is not in the 14-type allowlist.
		// Coverage records map to SCOPE.Pattern (canonical bucket for inferred
		// structural patterns); the framework-specific test type is preserved on
		// Subtype, and the originating test framework remains on Properties.
		Kind:         "SCOPE.Pattern",
		SourceFile:   filePath,
		Language:     language,
		Subtype:      testType,
		Properties:   props,
		QualityScore: confidenceScore(tc.confidence),
	}

	fromID := testFunctionRef(filePath, testQName)
	toID := productionFunctionRef(tc.prodFile, tc.qname)
	if toID != "" {
		rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
			FromID: fromID,
			ToID:   toID,
			Kind:   "TESTS",
			Properties: map[string]string{
				"test_framework": framework,
				"confidence":     tc.confidence,
				"test_function":  testQName,
				"tested":         tc.qname,
			},
		})
	}

	return rec
}

func confidenceProvenance(c string) string {
	switch c {
	case "high":
		return "DIRECT_CALL_IN_TEST_BODY"
	case "medium":
		return "MOCK_TARGET_MATCH"
	case "low":
		return "INFERRED_FROM_NAMING_CONVENTION"
	default:
		return "INFERRED_FROM_NAMING_CONVENTION"
	}
}

func confidenceScore(c string) float64 {
	switch c {
	case "high":
		return 0.9
	case "medium":
		return 0.7
	case "low":
		return 0.5
	default:
		return 0.5
	}
}

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

// Extract scans a source file, decides whether it is a test file, and emits
// SCOPE.Pattern (subtype "test_coverage") entities + TESTS edges for each detected test→production
// mapping. Files that are not test files return an empty slice. AST parse
// failures are logged upstream — this extractor is regex based and never
// propagates an error to the caller.
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("extractor._cross_testmap")
	_, span := tracer.Start(ctx, "indexer.test_coverage_extract")
	defer span.End()

	span.SetAttributes(
		attribute.String("file_path", file.Path),
		attribute.String("language", file.Language),
	)

	source := string(file.Content)
	if source == "" {
		span.SetAttributes(
			attribute.String("test_framework", ""),
			attribute.Int("tests_found", 0),
		)
		return nil, nil
	}

	tokens := extractImportTokens(source)
	fw := selectFramework(tokens, file.Path)
	if fw == nil {
		span.SetAttributes(
			attribute.String("test_framework", ""),
			attribute.Int("tests_found", 0),
		)
		return nil, nil
	}

	tests := fw.detect(source)
	if len(tests) == 0 {
		span.SetAttributes(
			attribute.String("test_framework", fw.name),
			attribute.Int("tests_found", 0),
		)
		return nil, nil
	}

	testType := inferTestType(file.Path)
	prodFile, prodSymbol := productionFileFromTestPath(file.Path)

	// Issue #2080: emit ONE SCOPE.Pattern entity per test function.
	// buildCollapsedEntity folds all resolved production calls into a single
	// record with multiple embedded TESTS edges (FromID="" → entity owns the
	// edge) so the entity is never degree-0 in the graph regardless of how
	// many @pytest.mark.parametrize parameter sets the test function has.
	out := make([]types.EntityRecord, 0, len(tests))
	for _, tf := range tests {
		called := resolveCalls(tf, prodFile, prodSymbol)
		if len(called) == 0 {
			continue
		}
		out = append(out, buildCollapsedEntity(file.Path, file.Language, fw.name, testType, tf.qname, called))
	}

	span.SetAttributes(
		attribute.String("test_framework", fw.name),
		attribute.Int("tests_found", len(out)),
	)
	return out, nil
}

// ---------------------------------------------------------------------------
// Debug helper
// ---------------------------------------------------------------------------

// String returns a short human-readable description. Exported for consumers
// that log the registered extractor list.
func (e *Extractor) String() string {
	return fmt.Sprintf("testmap[%s]", e.Language())
}
