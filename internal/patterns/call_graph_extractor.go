package patterns

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// callGraphExtractor detects function call relationships across languages.
// Consolidates Python call_graph_extractor_*.py files.
type callGraphExtractor struct{}

var (
	cgGoDottedCallRE    = regexp.MustCompile(`\b([A-Za-z_]\w*)\.([A-Za-z_]\w*)\s*\(`)
cgJVMDottedCallRE   = regexp.MustCompile(`\b([A-Za-z_]\w*)\.([A-Za-z_]\w*)\s*\(`)
	cgJSCallRE          = regexp.MustCompile(`\b([A-Za-z_$][A-Za-z0-9_$]*)\.([A-Za-z_$][A-Za-z0-9_$]*)\s*\(`)
	cgPyCallRE          = regexp.MustCompile(`\b([A-Za-z_]\w*)\.([A-Za-z_]\w*)\s*\(`)
	cgRubyDottedCallRE  = regexp.MustCompile(`\b([A-Za-z_]\w*)\.([a-z_][a-zA-Z0-9_?!]*)\s*\(`)
	cgRustPathCallRE    = regexp.MustCompile(`\b([A-Za-z_]\w*)::([A-Za-z_]\w*)\s*\(`)
	cgSQLCallProcRE     = regexp.MustCompile(`(?i)\bCALL\s+([A-Za-z_]\w*(?:\.[A-Za-z_]\w*)?)\s*\(`)
)

var cgJSKeywords = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true, "catch": true, "function": true,
	"class": true, "return": true, "typeof": true, "instanceof": true, "new": true,
}

func (c *callGraphExtractor) Category() string { return "call_graph" }

func (c *callGraphExtractor) AppliesTo(src string) bool {
	// Applies to all files that have function call syntax
	return strings.Contains(src, "(") && (strings.Contains(src, ".") || strings.Contains(src, "::"))
}

func (c *callGraphExtractor) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	var re *regexp.Regexp
	switch language {
	case "go":
		re = cgGoDottedCallRE
	case "java", "kotlin", "scala", "groovy":
		re = cgJVMDottedCallRE
	case "javascript", "typescript":
		re = cgJSCallRE
	case "python":
		re = cgPyCallRE
	case "ruby":
		re = cgRubyDottedCallRE
	case "rust":
		re = cgRustPathCallRE
	default:
		re = cgJVMDottedCallRE // generic dotted call
	}

	limit := 200 // cap to avoid huge outputs
	count := 0
	for _, m := range re.FindAllStringSubmatchIndex(src, -1) {
		if count >= limit {
			break
		}
		receiver := src[m[2]:m[3]]
		method := src[m[4]:m[5]]
		if cgJSKeywords[strings.ToLower(receiver)] || cgJSKeywords[strings.ToLower(method)] {
			continue
		}
		key := fmt.Sprintf("%s.%s", receiver, method)
		if seen[key] {
			continue
		}
		seen[key] = true
		count++
		results = append(results, makeEntity(filePath,
			fmt.Sprintf("call_%s_%s", receiver, method),
			"SCOPE.Operation", "call_graph", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "call_graph", "receiver": receiver, "method": method}))
	}

	// SQL CALL proc
	for _, m := range cgSQLCallProcRE.FindAllStringSubmatchIndex(src, -1) {
		proc := src[m[2]:m[3]]
		key := "sql:call:" + proc
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"call_sql_"+proc, "SCOPE.Operation", "call_graph", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "call_graph", "receiver": "sql", "method": proc}))
	}

	return results
}

func init() {
	Register(&callGraphExtractor{})
}
