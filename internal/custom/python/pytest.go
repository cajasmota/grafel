package python

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// extractTestFuncBody returns the body text of a function starting at bodyStart
// (i.e. the byte offset just after the def line's colon). It stops at the next
// top-level definition or EOF.
func extractTestFuncBody(source string, bodyStart int) string {
	rest := source[bodyStart:]
	// Top-level code starts at indent 0 with non-whitespace.
	nextToplevel := regexp.MustCompile(`(?m)^\S`).FindStringIndex(rest)
	if nextToplevel != nil {
		return rest[:nextToplevel[0]]
	}
	return rest
}

func init() {
	extractor.Register("python_pytest", &PytestExtractor{})
}

// PytestExtractor extracts pytest patterns: test functions, test classes,
// fixtures, parametrize, marks, and conftest.py fixtures.
type PytestExtractor struct{}

func (e *PytestExtractor) Language() string { return "python_pytest" }

var (
	ptTestFuncRe = regexp.MustCompile(
		`(?m)^(?:async\s+)?def\s+(test_\w+)\s*\(([^)]*)\)\s*:`)
	ptTestClassRe = regexp.MustCompile(
		`(?m)^class\s+(Test\w+)\s*(?:\([^)]*\))?\s*:`)
	ptTestMethodRe = regexp.MustCompile(
		`(?m)^\s{4,}(?:async\s+)?def\s+(test_\w+)\s*\(([^)]*)\)\s*:`)
	ptFixtureRe = regexp.MustCompile(
		`(?m)@pytest\.fixture\s*(\([^)]*\))?\s*\n(?:\s*#[^\n]*\n)*\s*(?:async\s+)?def\s+(\w+)\s*\(([^)]*)\)`)
	ptFixtureScopeRe   = regexp.MustCompile(`scope\s*=\s*["'](\w+)["']`)
	ptFixtureAutouseRe = regexp.MustCompile(`autouse\s*=\s*(True|False)`)
	ptParametrizeRe    = regexp.MustCompile(`@pytest\.mark\.parametrize\s*\(\s*["']([^"']+)["']`)
	ptMarkCustomRe     = regexp.MustCompile(`@pytest\.mark\.(\w+)\s*(?:\([^)]*\))?`)

	// Celery TESTS edges: task.delay() / task.apply() / task.apply_async() call
	// sites inside test functions → TESTS edge from test to task (issue #3346).
	ptCeleryCallRe = regexp.MustCompile(
		`(?m)\b(\w+)\.(delay|apply_async|apply)\s*\(`)
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
	var out []types.EntityRecord

	// 1. Fixtures
	provenance := "pytest_fixture"
	if isConftest {
		provenance = "pytest_conftest"
	}
	for _, idx := range allMatchesIndex(ptFixtureRe, source) {
		decoratorArgs := ""
		if idx[2] != -1 {
			decoratorArgs = source[idx[2]:idx[3]]
		}
		funcName := source[idx[4]:idx[5]]
		fixtureScope := "function"
		if sm := ptFixtureScopeRe.FindStringSubmatch(decoratorArgs); sm != nil {
			fixtureScope = sm[1]
		}
		autouse := "false"
		if am := ptFixtureAutouseRe.FindStringSubmatch(decoratorArgs); am != nil && am[1] == "True" {
			autouse = "true"
		}
		line := lineOf(source, idx[0])
		out = append(out, entity(funcName, "SCOPE.Pattern", "", file.Path, line,
			map[string]string{"framework": "pytest", "pattern_type": provenance, "fixture_scope": fixtureScope, "autouse": autouse}))
	}

	// 2. Test classes + their test methods
	type classRange struct{ start, end int }
	var classRanges []classRange

	for _, idx := range allMatchesIndex(ptTestClassRe, source) {
		className := source[idx[2]:idx[3]]
		classStart := idx[0]
		classLine := lineOf(source, classStart)

		// Determine class body end
		rest := source[idx[1]:]
		nextToplevel := regexp.MustCompile(`(?m)^\S`).FindStringIndex(rest)
		classEnd := len(source)
		if nextToplevel != nil {
			classEnd = idx[1] + nextToplevel[0]
		}
		classRanges = append(classRanges, classRange{classStart, classEnd})
		classBody := source[idx[1]:classEnd]

		// Collect class-level marks
		preClass := collectDecorators(source, classStart)
		marks := collectMarks(preClass)
		props := map[string]string{"framework": "pytest", "pattern_type": "test_class"}
		if len(marks) > 0 {
			props["marks"] = strings.Join(marks, ",")
		}
		out = append(out, entity(className, "SCOPE.Component", "", file.Path, classLine, props))

		// Test methods inside the class
		for _, mIdx := range allMatchesIndex(ptTestMethodRe, classBody) {
			methodName := classBody[mIdx[2]:mIdx[3]]
			methodLine := classLine + strings.Count(classBody[:mIdx[0]], "\n")
			preMethod := collectDecorators(classBody, mIdx[0])
			methodMarks := collectMarks(preMethod)
			parametrized := len(ptParametrizeRe.FindAllString(preMethod, -1)) > 0
			mProps := map[string]string{"framework": "pytest", "pattern_type": "test"}
			if len(methodMarks) > 0 {
				mProps["marks"] = strings.Join(methodMarks, ",")
			}
			if parametrized {
				mProps["parametrized"] = "true"
			}
			out = append(out, entity(className+"."+methodName, "SCOPE.Operation", "function", file.Path, methodLine, mProps))
		}
	}

	// 3. Top-level test functions (skip those inside classes)
	for _, idx := range allMatchesIndex(ptTestFuncRe, source) {
		funcStart := idx[0]
		insideClass := false
		for _, cr := range classRanges {
			if funcStart > cr.start && funcStart < cr.end {
				insideClass = true
				break
			}
		}
		if insideClass {
			continue
		}
		funcName := source[idx[2]:idx[3]]
		line := lineOf(source, funcStart)
		preFunc := collectDecorators(source, funcStart)
		marks := collectMarks(preFunc)
		parametrized := len(ptParametrizeRe.FindAllString(preFunc, -1)) > 0
		props := map[string]string{"framework": "pytest", "pattern_type": "test"}
		if len(marks) > 0 {
			props["marks"] = strings.Join(marks, ",")
		}
		if parametrized {
			props["parametrized"] = "true"
		}
		ent := entity(funcName, "SCOPE.Operation", "function", file.Path, line, props)

		// Celery TESTS edges: scan this test body for .delay()/.apply_async()/.apply() call
		// sites and emit a TESTS relationship from the test function to the task (issue #3346).
		funcBody := extractTestFuncBody(source, idx[1])
		for _, cIdx := range allMatchesIndex(ptCeleryCallRe, funcBody) {
			taskRef := funcBody[cIdx[2]:cIdx[3]]
			callKind := funcBody[cIdx[4]:cIdx[5]]
			ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
				ToID: "Task:" + taskRef,
				Kind: "TESTS",
				Properties: map[string]string{
					"framework": "celery",
					"call_kind": callKind,
				},
			})
		}

		out = append(out, ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

func collectDecorators(source string, defStart int) string {
	lines := strings.Split(source[:defStart], "\n")
	var result []string
	for i := len(lines) - 1; i >= 0; i-- {
		stripped := strings.TrimSpace(lines[i])
		if strings.HasPrefix(stripped, "@") || stripped == "" {
			result = append([]string{lines[i]}, result...)
		} else {
			break
		}
	}
	return strings.Join(result, "\n")
}

func collectMarks(decoratorBlock string) []string {
	var marks []string
	for _, m := range ptMarkCustomRe.FindAllStringSubmatch(decoratorBlock, -1) {
		markName := m[1]
		if !ptSkipMarks[markName] {
			marks = append(marks, markName)
		}
	}
	return marks
}
