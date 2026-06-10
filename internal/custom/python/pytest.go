package python

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// ISSUE #4357 — Python test orphan collapse + TESTS edge.
//
// The pytest/unittest custom extractor previously emitted a first-class entity
// per @pytest.fixture, per Test class, per test method, and per top-level
// test_ function — NONE of which carried any relationship beyond the narrow
// Celery .delay() TESTS special-case. On a real Python test suite (Django
// unittest TestCase classes, pytest function/class style, parametrize cases,
// setUp/conftest fixtures, bare assert / self.assertEqual calls) those edge-less
// nodes dominate the orphan ring — exactly mirroring the Jest #4343 / Go #4358
// orphan rings.
//
// Root-cause fix at extraction (not a downstream repair), mirroring #4343/#4358:
//
//   - Emit exactly ONE test_suite entity per test file. The per-fixture /
//     per-class / per-method / per-function nodes are no longer emitted as
//     standalone entities; their counts are folded into properties
//     (test_func_count, test_class_count, test_method_count, fixture_count,
//     assertion_count, parametrize_count, mark counts) so no information is lost
//     while the orphan blast radius collapses from O(funcs+classes+fixtures+
//     asserts) to one node per file.
//
//   - Synthesize a TESTS edge from the file's test_suite to the production
//     symbol(s) under test, resolved Python-idiomatically by NAME AFFINITY
//     (test_place_order → place_order; TestOrderService / OrderServiceTest →
//     OrderService and order_service; module test_foo.py → foo) gated on the
//     subject being BOTH derivable by name AND actually imported/referenced in
//     the test file. Requiring both keeps the edge conservative (name affinity
//     alone over-links; reference alone links fixtures/util). The edge ToID is
//     the bare subject name, which the resolver binds through its byName index
//     to the production function/class entity.
//
// Reuses the existing SCOPE.Pattern kind + "test_suite" subtype and the TESTS
// relationship kind — no new producer Kind. The single suite entity is named
// `pytest_suite:<base>` so it never collides by-name with a production
// function/class node (which would re-orphan it through the resolver's byName
// index, as in #4343), and so MergeWithCustom's name-keyed replace does not
// clobber the base tree-sitter class/func nodes (lesson from #4366).
//
// The cross/testmap Python path (test_coverage entities via direct body-call
// detection, and #2549 Django test-client → ViewSet route linkage) is unchanged
// and complementary — this fix covers the test→SUT-symbol axis at extraction.

func init() {
	extractor.Register("python_pytest", &PytestExtractor{})
}

// PytestExtractor extracts pytest + unittest test patterns and collapses them
// into one linked test_suite per file (issue #4357).
type PytestExtractor struct{}

func (e *PytestExtractor) Language() string { return "python_pytest" }

var (
	ptTestFuncRe = regexp.MustCompile(
		`(?m)^(?:async\s+)?def\s+(test_\w+)\s*\(([^)]*)\)\s*:`)
	// Test classes: pytest TestXxx OR unittest XxxTest(Case)/extends (Test)?Case.
	ptTestClassRe = regexp.MustCompile(
		`(?m)^class\s+(Test\w+|\w*Test|\w*TestCase)\s*(?:\(([^)]*)\))?\s*:`)
	ptTestMethodRe = regexp.MustCompile(
		`(?m)^\s{4,}(?:async\s+)?def\s+(test_\w+)\s*\(([^)]*)\)\s*:`)
	ptFixtureRe = regexp.MustCompile(
		`(?m)@pytest\.fixture\s*(\([^)]*\))?\s*\n(?:\s*#[^\n]*\n)*\s*(?:async\s+)?def\s+(\w+)\s*\(([^)]*)\)`)
	ptParametrizeRe = regexp.MustCompile(`@pytest\.mark\.parametrize\s*\(\s*["']([^"']+)["']`)
	ptMarkCustomRe  = regexp.MustCompile(`@pytest\.mark\.(\w+)\s*(?:\([^)]*\))?`)

	// unittest lifecycle fixtures (setUp / tearDown / setUpClass / tearDownClass /
	// setUpModule / tearDownModule) and bare assertion calls.
	ptUnittestFixtureRe = regexp.MustCompile(
		`(?m)^\s*(?:async\s+)?def\s+(setUp|tearDown|setUpClass|tearDownClass|setUpModule|tearDownModule)\s*\(`)
	ptUnittestAssertRe = regexp.MustCompile(`\bself\.(assert\w+)\s*\(`)
	ptBareAssertRe     = regexp.MustCompile(`(?m)^\s*assert\b`)

	// Celery TESTS edges: task.delay() / task.apply() / task.apply_async() call
	// sites inside the file → TESTS edge to the task (issue #3346), folded onto
	// the single suite entity.
	ptCeleryCallRe = regexp.MustCompile(
		`(?m)\b(\w+)\.(delay|apply_async|apply)\s*\(`)

	// importedSymbolsRe captures names bound by `from X import a, b, c`,
	// `from X import (\n a,\n b,\n)` (parenthesized multi-line), and `import x`
	// so the TESTS resolver can reference-gate subjects.
	ptFromImportRe      = regexp.MustCompile(`(?m)^\s*from\s+[\w.]+\s+import\s+([^\n#]+)`)
	ptFromImportParenRe = regexp.MustCompile(`(?s)from\s+[\w.]+\s+import\s*\(([^)]*)\)`)
	ptImportRe          = regexp.MustCompile(`(?m)^\s*import\s+([\w.]+)`)

	ptIdentRe = regexp.MustCompile(`^[A-Za-z_]\w*$`)
)

var ptSkipMarks = map[string]bool{"fixture": true, "parametrize": true, "usefixtures": true}

func (e *PytestExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_pytest")
	_, span := tracer.Start(ctx, "custom.python_pytest")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}

	source := string(file.Content)
	isConftest := strings.HasSuffix(file.Path, "conftest.py")

	// ── collect per-file counts (folded onto the single suite entity) ────────
	testFuncs := allMatchesIndex(ptTestFuncRe, source)     // includes methods + top-level
	testMethods := allMatchesIndex(ptTestMethodRe, source) // indented test_ defs
	classMatches := allMatchesIndex(ptTestClassRe, source)
	fixtureMatches := allMatchesIndex(ptFixtureRe, source)
	lifecycleFixtures := allMatchesIndex(ptUnittestFixtureRe, source)
	parametrizeCount := len(ptParametrizeRe.FindAllString(source, -1))
	assertionCount := len(ptUnittestAssertRe.FindAllString(source, -1)) +
		len(ptBareAssertRe.FindAllString(source, -1))
	fixtureCount := len(fixtureMatches) + len(lifecycleFixtures)

	// Top-level test functions = all test_ defs not nested inside a class.
	var classRanges []struct{ start, end int }
	for _, idx := range classMatches {
		rest := source[idx[1]:]
		nextToplevel := regexp.MustCompile(`(?m)^\S`).FindStringIndex(rest)
		end := len(source)
		if nextToplevel != nil {
			end = idx[1] + nextToplevel[0]
		}
		classRanges = append(classRanges, struct{ start, end int }{idx[0], end})
	}
	topLevelFuncCount := 0
	for _, idx := range testFuncs {
		inside := false
		for _, cr := range classRanges {
			if idx[0] > cr.start && idx[0] < cr.end {
				inside = true
				break
			}
		}
		if !inside {
			topLevelFuncCount++
		}
	}

	// Collect distinct custom marks across the file.
	markSet := map[string]bool{}
	for _, m := range ptMarkCustomRe.FindAllStringSubmatch(source, -1) {
		if !ptSkipMarks[m[1]] {
			markSet[m[1]] = true
		}
	}

	// Nothing test-shaped → emit nothing (conftest with only fixtures still
	// emits a suite so its fixtures are accounted for; a plain module that
	// merely imports pytest does not).
	if len(testFuncs) == 0 && len(classMatches) == 0 && fixtureCount == 0 {
		span.SetAttributes(attribute.Int("entity_count", 0))
		return nil, nil
	}

	// ── one linked test_suite per file ───────────────────────────────────────
	provenance := "INFERRED_FROM_PYTEST_FILE"
	framework := "pytest"
	if len(classMatches) > 0 && hasUnittestBase(source, classMatches) {
		framework = "unittest"
		provenance = "INFERRED_FROM_UNITTEST_FILE"
	}
	if isConftest {
		provenance = "INFERRED_FROM_PYTEST_CONFTEST"
	}

	line := 1
	if len(classMatches) > 0 {
		line = lineOf(source, classMatches[0][0])
	} else if len(testFuncs) > 0 {
		line = lineOf(source, testFuncs[0][0])
	} else if len(fixtureMatches) > 0 {
		line = lineOf(source, fixtureMatches[0][0])
	}

	props := map[string]string{
		"framework":           framework,
		"test_framework":      framework,
		"pattern_type":        "test_suite",
		"provenance":          provenance,
		"test_func_count":     strconv.Itoa(len(testFuncs)),
		"toplevel_func_count": strconv.Itoa(topLevelFuncCount),
		"test_method_count":   strconv.Itoa(len(testMethods)),
		"test_class_count":    strconv.Itoa(len(classMatches)),
		"fixture_count":       strconv.Itoa(fixtureCount),
		"assertion_count":     strconv.Itoa(assertionCount),
		"parametrize_count":   strconv.Itoa(parametrizeCount),
	}
	if isConftest {
		props["conftest"] = "true"
	}
	if len(markSet) > 0 {
		props["marks"] = strings.Join(sortedSet(markSet), ",")
	}

	ent := entity("pytest_suite:"+pyTestBaseName(file.Path), "SCOPE.Pattern", "test_suite",
		file.Path, line, props)

	// ── TESTS edges: name affinity, reference-gated (issue #4357) ────────────
	referenced := collectPyReferencedSymbols(source)
	subjects := resolvePyTestSubjects(source, file.Path, classMatches, testFuncs, referenced)
	if len(subjects) > 0 {
		ent.Properties["tests_target"] = strings.Join(subjects, ",")
		for _, subj := range subjects {
			ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
				ToID: subj,
				Kind: string(types.RelationshipKindTests),
				Properties: map[string]string{
					"framework":    framework,
					"match_source": "python_test_name_affinity",
					"target":       subj,
				},
				Confidence: 0.9,
			})
		}
	}

	// Celery TESTS edges (folded onto the suite): scan for .delay()/.apply_async()/
	// .apply() call sites and emit a TESTS relationship to the task (issue #3346).
	seenTask := map[string]bool{}
	for _, cIdx := range allMatchesIndex(ptCeleryCallRe, source) {
		taskRef := source[cIdx[2]:cIdx[3]]
		callKind := source[cIdx[4]:cIdx[5]]
		if seenTask[taskRef] {
			continue
		}
		seenTask[taskRef] = true
		ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
			ToID: "Task:" + taskRef,
			Kind: string(types.RelationshipKindTests),
			Properties: map[string]string{
				"framework": "celery",
				"call_kind": callKind,
			},
		})
	}

	span.SetAttributes(attribute.Int("entity_count", 1))
	return []types.EntityRecord{ent}, nil
}

// hasUnittestBase reports whether any detected test class extends a unittest /
// Django TestCase base (the strong signal that this is a unittest-style file).
func hasUnittestBase(source string, classMatches [][]int) bool {
	for _, idx := range classMatches {
		if idx[4] != -1 {
			bases := source[idx[4]:idx[5]]
			if strings.Contains(bases, "TestCase") || strings.Contains(bases, "unittest.") {
				return true
			}
		}
	}
	return strings.Contains(source, "import unittest") ||
		strings.Contains(source, "from django.test") ||
		strings.Contains(source, "rest_framework.test")
}

// pyTestBaseName derives a human label from a Python test file path, e.g.
// `core/tests/test_schedule_import.py` → `test_schedule_import`.
func pyTestBaseName(path string) string {
	p := path
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		p = p[i+1:]
	}
	return strings.TrimSuffix(p, ".py")
}

// pySubjectFromModulePath derives the SUT module-name candidate from a test
// file's path: test_foo.py → foo, foo_test.py → foo, tests.py → "".
func pySubjectFromModulePath(path string) string {
	stem := pyTestBaseName(path)
	switch stem {
	case "tests", "test", "conftest":
		return ""
	}
	if strings.HasPrefix(stem, "test_") {
		return strings.TrimPrefix(stem, "test_")
	}
	if strings.HasSuffix(stem, "_test") {
		return strings.TrimSuffix(stem, "_test")
	}
	return ""
}

// collectPyReferencedSymbols returns the set of names imported into the test
// file (via `from X import a, b` and `import x`). A TESTS subject is eligible
// only when it is in this set — i.e. actually pulled into the test file —
// which keeps the edge pointed at an in-repo production symbol rather than a
// helper/util/fixture name.
func collectPyReferencedSymbols(source string) map[string]bool {
	out := map[string]bool{}
	addClause := func(clause string) {
		// Strip parentheses/backslashes used for multi-line import groups, drop
		// inline comments, then split on commas.
		clause = strings.NewReplacer("(", "", ")", "", "\\", "").Replace(clause)
		for _, part := range strings.Split(clause, ",") {
			name := strings.TrimSpace(part)
			if i := strings.IndexByte(name, '#'); i >= 0 {
				name = strings.TrimSpace(name[:i])
			}
			// handle `foo as bar` → bind name is `bar`.
			if i := strings.Index(name, " as "); i >= 0 {
				name = strings.TrimSpace(name[i+4:])
			}
			if ptIdentRe.MatchString(name) {
				out[name] = true
			}
		}
	}
	// Parenthesized multi-line: `from X import (\n a,\n b,\n)`.
	for _, m := range ptFromImportParenRe.FindAllStringSubmatch(source, -1) {
		addClause(m[1])
	}
	// Single-line: `from X import a, b`.
	for _, m := range ptFromImportRe.FindAllStringSubmatch(source, -1) {
		clause := m[1]
		// Skip the opening of a parenthesized group (handled above).
		if strings.Contains(clause, "(") && !strings.Contains(clause, ")") {
			continue
		}
		addClause(clause)
	}
	for _, m := range ptImportRe.FindAllStringSubmatch(source, -1) {
		mod := m[1]
		// `import a.b.c` binds the leaf? In Python it binds `a`, but the SUT
		// reference convention uses the leaf module; record both ends.
		parts := strings.Split(mod, ".")
		if ptIdentRe.MatchString(parts[0]) {
			out[parts[0]] = true
		}
		if leaf := parts[len(parts)-1]; ptIdentRe.MatchString(leaf) {
			out[leaf] = true
		}
	}
	return out
}

// classSubjectCandidates derives production-symbol candidates from a unittest /
// pytest test class name, most-specific first:
//
//	TestOrderService    → ["OrderService", "order_service"]
//	OrderServiceTest    → ["OrderService", "order_service"]
//	ResolveDeviceTest   → ["ResolveDevice", "resolve_device"]
//	ParseCsvFileMissingColumnsTest → ["ParseCsvFileMissingColumns", "parse_csv_file_missing_columns"]
//
// Returns both the CamelCase base (matches a production class) and its
// snake_case form (matches a production function) — the reference gate then
// selects whichever was actually imported.
func classSubjectCandidates(className string) []string {
	base := className
	switch {
	case strings.HasPrefix(base, "Test"):
		base = base[len("Test"):]
	case strings.HasSuffix(base, "TestCase"):
		base = base[:len(base)-len("TestCase")]
	case strings.HasSuffix(base, "Test"):
		base = base[:len(base)-len("Test")]
	}
	if base == "" || !ptIdentRe.MatchString(base) {
		return nil
	}
	cands := []string{base}
	if snake := camelToSnake(base); snake != base {
		cands = append(cands, snake)
	}
	return cands
}

// funcSubjectCandidates derives the production-symbol candidate from a pytest
// test function name: test_place_order → place_order.
func funcSubjectCandidates(funcName string) []string {
	base := strings.TrimPrefix(funcName, "test_")
	if base == "" || base == funcName || !ptIdentRe.MatchString(base) {
		return nil
	}
	return []string{base}
}

// camelToSnake converts CamelCase/PascalCase to snake_case, treating runs of
// uppercase as acronyms (CSVFile → csv_file).
func camelToSnake(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				prev := runes[i-1]
				next := rune(0)
				if i+1 < len(runes) {
					next = runes[i+1]
				}
				prevLower := prev >= 'a' && prev <= 'z' || prev >= '0' && prev <= '9'
				nextLower := next >= 'a' && next <= 'z'
				if prevLower || (nextLower && prev >= 'A' && prev <= 'Z') {
					b.WriteByte('_')
				}
			}
			b.WriteRune(r - 'A' + 'a')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// resolvePyTestSubjects determines the unit(s) under test for a Python test
// file, de-duplicated and in priority order. A subject is emitted ONLY when it
// is both (a) derivable by name affinity from a test class, a test_ function,
// or the module path, AND (b) actually imported/referenced in the test file.
func resolvePyTestSubjects(source, path string, classMatches, testFuncs [][]int, referenced map[string]bool) []string {
	var ordered []string
	seen := map[string]bool{}
	add := func(name string) {
		if name == "" || seen[name] || !referenced[name] {
			return
		}
		seen[name] = true
		ordered = append(ordered, name)
	}

	// 1. test classes → strip Test prefix/suffix → CamelCase + snake candidates.
	for _, idx := range classMatches {
		for _, cand := range classSubjectCandidates(source[idx[2]:idx[3]]) {
			if referenced[cand] {
				add(cand)
				break
			}
		}
	}

	// 2. top-level test_ functions → strip test_ prefix.
	for _, idx := range testFuncs {
		for _, cand := range funcSubjectCandidates(source[idx[2]:idx[3]]) {
			if referenced[cand] {
				add(cand)
				break
			}
		}
	}

	// 3. module-path affinity (test_foo.py → foo) — only when nothing else
	//    resolved, to avoid over-linking on broad multi-subject files.
	if len(ordered) == 0 {
		add(pySubjectFromModulePath(path))
	}

	return ordered
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
