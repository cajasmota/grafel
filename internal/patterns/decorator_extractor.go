package patterns

import (
	"fmt"
	"regexp"

	"github.com/cajasmota/archigraph/internal/types"
)

// decoratorExtractor extracts decorator/annotation usages.
// Matches Python decorator_extractor.py.
type decoratorExtractor struct{}

var (
	decPyDecoratorRE = regexp.MustCompile(`(?m)^\s*@(\w[\w.]*)\s*(?:\([^)]*\))?\s*\n\s*(?:async\s+)?def\s+(\w+)`)
	decPyTriggerRE   = regexp.MustCompile(`(?m)^\s*@\w`)
	decJavaDecRE     = regexp.MustCompile(`@(\w[\w.]*)(?:\s*\([^)]*\))?`)
	decJavaTriggerRE = regexp.MustCompile(`@[A-Z]\w*`)
	decTSDecRE       = regexp.MustCompile(`@(\w[\w.]*)\s*(?:\([^)]*\))?\s*(?:class|async|function|get|set|\w+)\s+(\w+)`)
	decTSTriggerRE   = regexp.MustCompile(`@\w`)
	decCSDecRE       = regexp.MustCompile(`(?m)^\s*\[([A-Z]\w*(?:\.\w+)*)\s*(?:\([^)]*\))?\s*\]`)
	decRustOuterRE   = regexp.MustCompile(`#\[(\w[\w:]*)\s*(?:\([^)]*\))?\]`)
	decRustInnerRE   = regexp.MustCompile(`#!\[(\w[\w:]*)\s*(?:\([^)]*\))?\]`)
	decRustTriggerRE = regexp.MustCompile(`#\[|#!\[`)
	decSwiftDecRE    = regexp.MustCompile(`@(\w[\w.]*)(?:\s*\([^)]*\))?`)
	decSwiftTriggerRE = regexp.MustCompile(`@\w`)
)

func (d *decoratorExtractor) Category() string { return "decorator" }

func (d *decoratorExtractor) AppliesTo(src string) bool {
	return decPyTriggerRE.MatchString(src) ||
		decJavaTriggerRE.MatchString(src) ||
		decTSTriggerRE.MatchString(src) ||
		decRustTriggerRE.MatchString(src) ||
		decSwiftTriggerRE.MatchString(src)
}

func (d *decoratorExtractor) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}
	idx := 0

	emit := func(decoratorName, targetName string, line int) {
		key := fmt.Sprintf("%s:%s:%d", decoratorName, targetName, line)
		if seen[key] {
			return
		}
		seen[key] = true
		idx++
		// Name format: <decorator>@<file>:<line> — matches Python parity.
		results = append(results, makeEntity(filePath,
			fmt.Sprintf("%s@%s:%d", decoratorName, filePath, line),
			"SCOPE.Pattern", "decorator", language, line,
			map[string]string{
				"kind":           "decorator",
				"decorator_name": decoratorName,
				"target_name":    targetName,
			}))
	}

	switch language {
	case "python":
		for _, m := range decPyDecoratorRE.FindAllStringSubmatchIndex(src, -1) {
			emit(src[m[2]:m[3]], src[m[4]:m[5]], lineOf(src, m[0]))
		}
	case "java", "kotlin":
		for _, m := range decJavaDecRE.FindAllStringSubmatchIndex(src, -1) {
			decoratorName := src[m[2]:m[3]]
			targetName := ""
			if len(m) > 4 && m[4] >= 0 {
				targetName = src[m[4]:m[5]]
			}
			emit(decoratorName, targetName, lineOf(src, m[0]))
		}
	case "typescript", "javascript":
		for _, m := range decTSDecRE.FindAllStringSubmatchIndex(src, -1) {
			emit(src[m[2]:m[3]], src[m[4]:m[5]], lineOf(src, m[0]))
		}
	case "csharp":
		for _, m := range decCSDecRE.FindAllStringSubmatchIndex(src, -1) {
			decoratorName := src[m[2]:m[3]]
			targetName := ""
			if len(m) > 4 && m[4] >= 0 {
				targetName = src[m[4]:m[5]]
			}
			emit(decoratorName, targetName, lineOf(src, m[0]))
		}
	case "rust":
		for _, m := range decRustOuterRE.FindAllStringSubmatchIndex(src, -1) {
			decoratorName := src[m[2]:m[3]]
			emit(decoratorName, "", lineOf(src, m[0]))
		}
		for _, m := range decRustInnerRE.FindAllStringSubmatchIndex(src, -1) {
			decoratorName := src[m[2]:m[3]]
			emit(decoratorName, "", lineOf(src, m[0]))
		}
	case "swift":
		for _, m := range decSwiftDecRE.FindAllStringSubmatchIndex(src, -1) {
			decoratorName := src[m[2]:m[3]]
			emit(decoratorName, "", lineOf(src, m[0]))
		}
	case "dart", "shell", "proto", "protobuf", "clojure", "zig", "groovy":
		// These languages use @ for non-decorator purposes or Python doesn't emit
		// decorator entities for them:
		// - Dart: annotations (@override) — Python doesn't emit decorator entities
		// - Clojure: @atom-name = atom dereference
		// - Zig: @import, @intCast = builtin functions
		// - Groovy: annotations exist but Python suppresses decorator extraction
		// Suppress to maintain parity.

	default:
		// Try all
		for _, m := range decPyDecoratorRE.FindAllStringSubmatchIndex(src, -1) {
			emit(src[m[2]:m[3]], src[m[4]:m[5]], lineOf(src, m[0]))
		}
		for _, m := range decJavaDecRE.FindAllStringSubmatchIndex(src, -1) {
			decoratorName := src[m[2]:m[3]]
			targetName := ""
			if len(m) > 4 && m[4] >= 0 {
				targetName = src[m[4]:m[5]]
			}
			emit(decoratorName, targetName, lineOf(src, m[0]))
		}
	}

	return results
}

func init() {
	Register(&decoratorExtractor{})
}
