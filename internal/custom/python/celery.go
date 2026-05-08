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

func init() {
	extractor.Register("python_celery", &CeleryExtractor{})
}

// CeleryExtractor extracts Celery patterns: @shared_task, @app.task, canvas
// primitives, beat_schedule, Task lifecycle hooks, and task_routes.
type CeleryExtractor struct{}

func (e *CeleryExtractor) Language() string { return "python_celery" }

var (
	celSharedTaskRe = regexp.MustCompile(
		`(?m)@shared_task\s*(\([^)]*\))?\s*\n(?:\s*#[^\n]*\n)*\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	celAppTaskRe = regexp.MustCompile(
		`(?m)@(\w+)\.task\s*(\([^)]*\))?\s*\n(?:\s*#[^\n]*\n)*\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	celQueueRe      = regexp.MustCompile(`queue\s*=\s*["']([^"']+)["']`)
	celBindRe       = regexp.MustCompile(`bind\s*=\s*(True|False)`)
	celMaxRetriesRe = regexp.MustCompile(`max_retries\s*=\s*(\d+)`)
	celTaskNameRe   = regexp.MustCompile(`(?:^|[^\w])name\s*=\s*["']([^"']+)["']`)
	celChainRe      = regexp.MustCompile(`(?m)(?:^|[^\w])(chain)\s*\(`)
	celGroupRe      = regexp.MustCompile(`(?m)(?:^|[^\w])(group)\s*\(`)
	celChordRe      = regexp.MustCompile(`(?m)(?:^|[^\w])(chord)\s*\(`)
	celCanvasTaskRe = regexp.MustCompile(`(\w+)\.s(?:i)?\s*\(`)
	celBeatEntryRe  = regexp.MustCompile(
		`(?s)["'](\w[\w.-]*)["']:\s*\{[^}]*["']task["']:\s*["']([^"']+)["']`)
	celTaskClassRe = regexp.MustCompile(
		`(?m)^class\s+(\w+)\s*\(\s*(?:celery\.)?Task\s*\)\s*:`)
	celHookMethodRe = regexp.MustCompile(
		`(?m)^\s{4,}def\s+(on_failure|on_success|on_retry|on_revoke)\s*\(`)
	celTaskRoutesEntryRe = regexp.MustCompile(
		`(?m)["']([^"']+)["']:\s*\{([^}]*)\}`)
	celRoutingQueueRe = regexp.MustCompile(`["']queue["']:\s*["']([^"']+)["']`)
)

func (e *CeleryExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_celery")
	_, span := tracer.Start(ctx, "custom.python_celery")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}

	source := string(file.Content)
	var out []types.EntityRecord

	// 1. @shared_task
	for _, idx := range allMatchesIndex(celSharedTaskRe, source) {
		decoratorArgs := ""
		if idx[2] != -1 {
			decoratorArgs = source[idx[2]:idx[3]]
		}
		funcName := source[idx[4]:idx[5]]
		line := lineOf(source, idx[0])
		props := parseTaskDecoratorArgs(decoratorArgs)
		props["framework"] = "celery"
		props["pattern_type"] = "shared_task"
		out = append(out, entity(funcName, "SCOPE.Service", "task", file.Path, line, props))
	}

	// 2. @app.task
	for _, idx := range allMatchesIndex(celAppTaskRe, source) {
		appVar := source[idx[2]:idx[3]]
		decoratorArgs := ""
		if idx[4] != -1 {
			decoratorArgs = source[idx[4]:idx[5]]
		}
		funcName := source[idx[6]:idx[7]]
		line := lineOf(source, idx[0])
		props := parseTaskDecoratorArgs(decoratorArgs)
		props["framework"] = "celery"
		props["pattern_type"] = "app_task"
		props["app_var"] = appVar
		out = append(out, entity(funcName, "SCOPE.Service", "task", file.Path, line, props))
	}

	// 3. Canvas primitives
	canvasPatterns := []struct {
		re         *regexp.Regexp
		canvasType string
	}{
		{celChainRe, "chain"},
		{celGroupRe, "group"},
		{celChordRe, "chord"},
	}
	for _, cp := range canvasPatterns {
		for _, idx := range allMatchesIndex(cp.re, source) {
			// Extract balanced parens after the keyword
			openParenPos := idx[1] - 1
			innerArgs := extractBalancedParens(source, openParenPos)
			taskRefs := celCanvasTaskRe.FindAllStringSubmatch(innerArgs, -1)
			if len(taskRefs) == 0 {
				continue
			}
			seen := make(map[string]bool)
			var tasks []string
			for _, tr := range taskRefs {
				if !seen[tr[1]] {
					seen[tr[1]] = true
					tasks = append(tasks, tr[1])
				}
			}
			line := lineOf(source, idx[0])
			name := cp.canvasType + "(" + strings.Join(tasks, ",") + ")"
			out = append(out, entity(name, "SCOPE.Pattern", "canvas", file.Path, line,
				map[string]string{"framework": "celery", "pattern_type": "canvas", "canvas_type": cp.canvasType, "task_refs": strings.Join(tasks, ",")}))
		}
	}

	// 4. Beat schedule entries
	if strings.Contains(source, "beat_schedule") {
		for _, idx := range allMatchesIndex(celBeatEntryRe, source) {
			entryName := source[idx[2]:idx[3]]
			taskName := source[idx[4]:idx[5]]
			line := lineOf(source, idx[0])
			out = append(out, entity(entryName, "SCOPE.Pattern", "beat_entry", file.Path, line,
				map[string]string{"framework": "celery", "pattern_type": "beat_schedule", "task": taskName}))
		}
	}

	// 5. Class-based Task lifecycle hooks
	for _, idx := range allMatchesIndex(celTaskClassRe, source) {
		className := source[idx[2]:idx[3]]
		classLine := lineOf(source, idx[0])

		out = append(out, entity(className, "SCOPE.Service", "task", file.Path, classLine,
			map[string]string{"framework": "celery", "pattern_type": "class_task", "task_type": "class_based"}))

		// Find class body and extract hooks
		rest := source[idx[1]:]
		nextToplevel := regexp.MustCompile(`(?m)^\S`).FindStringIndex(rest)
		var classBody string
		if nextToplevel != nil {
			classBody = rest[:nextToplevel[0]]
		} else {
			classBody = rest
		}
		for _, hIdx := range allMatchesIndex(celHookMethodRe, classBody) {
			hookName := classBody[hIdx[2]:hIdx[3]]
			hookLine := classLine + strings.Count(classBody[:hIdx[0]], "\n") + 1
			out = append(out, entity(className+"."+hookName, "SCOPE.Operation", "function", file.Path, hookLine,
				map[string]string{"framework": "celery", "pattern_type": "task_hook", "hook_type": hookName}))
		}
	}

	// 6. task_routes entries
	if strings.Contains(source, "task_routes") {
		for _, idx := range allMatchesIndex(celTaskRoutesEntryRe, source) {
			taskName := source[idx[2]:idx[3]]
			routingArgs := source[idx[4]:idx[5]]
			line := lineOf(source, idx[0])
			props := map[string]string{"framework": "celery", "pattern_type": "task_route", "task": taskName}
			if qm := celRoutingQueueRe.FindStringSubmatch(routingArgs); qm != nil {
				props["queue"] = qm[1]
			}
			out = append(out, entity(taskName, "SCOPE.Pattern", "task_route", file.Path, line, props))
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

func parseTaskDecoratorArgs(args string) map[string]string {
	props := make(map[string]string)
	if qm := celQueueRe.FindStringSubmatch(args); qm != nil {
		props["queue"] = qm[1]
	}
	if bm := celBindRe.FindStringSubmatch(args); bm != nil {
		props["bind"] = bm[1]
	}
	if mr := celMaxRetriesRe.FindStringSubmatch(args); mr != nil {
		props["max_retries"] = mr[1]
	}
	if tn := celTaskNameRe.FindStringSubmatch(args); tn != nil {
		props["task_name_override"] = tn[1]
	}
	return props
}

func extractBalancedParens(source string, openPos int) string {
	depth := 0
	start := openPos
	for i := openPos; i < len(source); i++ {
		switch source[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return source[start+1 : i]
			}
		}
	}
	return ""
}
