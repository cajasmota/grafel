package patterns

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// namingConventionDetector detects naming convention inconsistencies.
// Matches Python naming_convention_detector.py.
type namingConventionDetector struct{}

var (
	ncSnakeRE     = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)+$`)
	ncScreamingRE = regexp.MustCompile(`^[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+$`)
	ncPascalRE    = regexp.MustCompile(`^[A-Z][a-zA-Z0-9]+$`)
	ncCamelRE     = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*[A-Z][a-zA-Z0-9]*$`)
	ncKebabRE     = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)+$`)

	// Extract identifiers from various languages
	ncPyDefRE      = regexp.MustCompile(`(?m)^\s*(?:async\s+)?def\s+(\w+)`)
	ncGoFuncRE     = regexp.MustCompile(`(?m)^\s*func\s+(?:\([^)]*\)\s*)?(\w+)\s*\(`)
	ncJSFuncRE     = regexp.MustCompile(`\bfunction\s+(\w+)\s*\(`)
	ncSwiftFuncRE  = regexp.MustCompile(`(?m)\bfunc\s+(\w+)\s*[\(<]`)
	ncRubyDefRE    = regexp.MustCompile(`(?m)^\s*def\s+(\w+)`)
	ncRustFnRE     = regexp.MustCompile(`(?m)\bfn\s+(\w+)\s*[\(<]`)
	ncScalaDefRE   = regexp.MustCompile(`(?m)\bdef\s+(\w+)\s*[\(<:]`)
	ncElixirDefRE  = regexp.MustCompile(`(?m)^\s*def[p]?\s+(\w+)`)
	ncKotlinFunRE  = regexp.MustCompile(`(?m)\bfun\s+(\w+)\s*[\(<]`)
	ncJavaMethodRE = regexp.MustCompile(`(?m)\b(?:void|int|String|boolean|long|float|double|Object|[\w<>\[\]]+)\s+(\w+)\s*\(`)
	ncGenericRE    = regexp.MustCompile(`(?m)(?:def|func|fn|fun|function)\s+(\w+)`)
	// Shell: matches both "function name {" and POSIX "name() {" forms.
	ncShellFuncRE = regexp.MustCompile(`(?m)(?:^|\n)(?:function\s+)?(\w+)\s*\(\s*\)\s*\{`)
	// Proto: matches message, service, enum, and rpc names.
	ncProtoNameRE = regexp.MustCompile(`(?m)^\s*(?:message|service|enum|rpc)\s+(\w+)`)
	// Dart: matches class, mixin, extension, and method/function names.
	ncDartClassRE  = regexp.MustCompile(`(?m)^\s*(?:abstract\s+)?(?:class|mixin|extension)\s+(\w+)`)
	ncDartMethodRE = regexp.MustCompile(`(?m)^\s*(?:(?:static|async|override|final|const)\s+)*(?:[\w<>\[\]?]+\s+)?(\w+)\s*\([^)]*\)\s*(?:async\s*)?\{`)
	// CSS: matches class selectors (.name), id selectors (#name), and custom properties (--name).
	ncCSSClassRE = regexp.MustCompile(`(?m)(?:^|[,\s{])\.([a-zA-Z_][\w-]*)`)
	ncCSSVarRE   = regexp.MustCompile(`(?m)--([a-zA-Z_][\w-]*)`)
	// GraphQL: matches type, input, enum, interface, union, scalar, and field names.
	ncGraphQLTypeRE  = regexp.MustCompile(`(?m)^\s*(?:type|input|enum|interface|union|scalar)\s+(\w+)`)
	ncGraphQLFieldRE = regexp.MustCompile(`(?m)^\s+(\w+)\s*[\(:]`)
	// SQL: matches table, column, function, view, index names.
	ncSQLTableRE = regexp.MustCompile(`(?mi)CREATE\s+(?:TABLE|VIEW)\s+(?:IF\s+NOT\s+EXISTS\s+)?(\w+)`)
	ncSQLFuncRE  = regexp.MustCompile(`(?mi)CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+(\w+)`)
	ncSQLColRE   = regexp.MustCompile(`(?m)^\s+(\w+)\s+(?:SERIAL|INTEGER|VARCHAR|TEXT|TIMESTAMP|BOOLEAN|INT|BIGINT|FLOAT|DOUBLE|NUMERIC|DATE|UUID)`)
	// AppliesTo triggers for shell (POSIX function syntax) and proto/dart/css/graphql/sql.
	ncShellTriggerRE   = regexp.MustCompile(`(?m)^\w+\s*\(\s*\)\s*\{`)
	ncProtoTriggerRE   = regexp.MustCompile(`(?m)^\s*(?:message|service|enum|rpc)\s+\w`)
	ncDartTriggerRE    = regexp.MustCompile(`(?m)^\s*(?:abstract\s+)?(?:class|mixin)\s+\w`)
	ncCSSTriggerRE     = regexp.MustCompile(`(?m)\.\w[\w-]*\s*\{`)
	ncGraphQLTriggerRE = regexp.MustCompile(`(?m)^\s*(?:type|input|enum)\s+\w`)
	ncSQLTriggerRE     = regexp.MustCompile(`(?mi)CREATE\s+(?:TABLE|VIEW|FUNCTION|INDEX)`)
)

// skipDartNCKeywords are Dart control flow / declaration keywords that the
// method regex can accidentally match — same set as the dart extractor.
var skipDartNCKeywords = map[string]bool{
	"if": true, "else": true, "for": true, "while": true,
	"do": true, "switch": true, "try": true, "catch": true,
	"finally": true, "return": true, "assert": true, "throw": true,
	"import": true, "export": true, "class": true, "abstract": true,
	"mixin": true, "extension": true, "enum": true,
}

func classifyName(name string) string {
	if ncScreamingRE.MatchString(name) {
		return "SCREAMING_SNAKE"
	}
	if ncSnakeRE.MatchString(name) {
		return "snake_case"
	}
	if ncKebabRE.MatchString(name) {
		return "kebab-case"
	}
	if ncPascalRE.MatchString(name) {
		return "PascalCase"
	}
	if ncCamelRE.MatchString(name) {
		return "camelCase"
	}
	// Single-word all-lowercase identifiers are snake_case (no underscore needed)
	if len(name) > 1 && name[0] >= 'a' && name[0] <= 'z' {
		allLower := true
		for _, c := range name {
			if c >= 'A' && c <= 'Z' {
				allLower = false
				break
			}
		}
		if allLower {
			return "snake_case"
		}
	}
	return "other"
}

func (n *namingConventionDetector) Category() string { return "naming_convention" }

func (n *namingConventionDetector) AppliesTo(src string) bool {
	return ncGenericRE.MatchString(src) ||
		ncJavaMethodRE.MatchString(src) ||
		ncShellTriggerRE.MatchString(src) ||
		ncProtoTriggerRE.MatchString(src) ||
		ncDartTriggerRE.MatchString(src) ||
		ncCSSTriggerRE.MatchString(src) ||
		ncGraphQLTriggerRE.MatchString(src) ||
		ncSQLTriggerRE.MatchString(src)
}

func (n *namingConventionDetector) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	conventions := map[string]int{} // convention → count

	var re *regexp.Regexp
	switch language {
	case "python":
		re = ncPyDefRE
	case "go":
		re = ncGoFuncRE
	case "javascript", "typescript":
		re = ncJSFuncRE
	case "swift":
		re = ncSwiftFuncRE
	case "ruby":
		re = ncRubyDefRE
	case "rust":
		re = ncRustFnRE
	case "scala":
		re = ncScalaDefRE
	case "elixir":
		re = ncElixirDefRE
	case "kotlin":
		re = ncKotlinFunRE
	case "java":
		re = ncJavaMethodRE
	case "shell":
		re = ncShellFuncRE
	case "proto", "protobuf":
		re = ncProtoNameRE
	case "dart":
		// Dart: collect class names and method names together.
		for _, m := range ncDartClassRE.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			convention := classifyName(name)
			if convention == "other" || convention == "skip" {
				continue
			}
			conventions[convention]++
		}
		for _, m := range ncDartMethodRE.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			if skipDartNCKeywords[name] {
				continue
			}
			convention := classifyName(name)
			if convention == "other" || convention == "skip" {
				continue
			}
			conventions[convention]++
		}
		re = nil // already populated above
	case "css":
		// CSS: collect class selectors and custom property names.
		for _, m := range ncCSSClassRE.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			convention := classifyName(name)
			if convention == "other" || convention == "skip" {
				continue
			}
			conventions[convention]++
		}
		for _, m := range ncCSSVarRE.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			convention := classifyName(name)
			if convention == "other" || convention == "skip" {
				continue
			}
			conventions[convention]++
		}
		re = nil
	case "graphql":
		// GraphQL: collect type/input/enum names and field names.
		for _, m := range ncGraphQLTypeRE.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			convention := classifyName(name)
			if convention == "other" || convention == "skip" {
				continue
			}
			conventions[convention]++
		}
		for _, m := range ncGraphQLFieldRE.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			convention := classifyName(name)
			if convention == "other" || convention == "skip" {
				continue
			}
			conventions[convention]++
		}
		re = nil
	case "sql":
		// SQL: collect table, function, column, and view names.
		for _, m := range ncSQLTableRE.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			convention := classifyName(name)
			if convention == "other" || convention == "skip" {
				continue
			}
			conventions[convention]++
		}
		for _, m := range ncSQLFuncRE.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			convention := classifyName(name)
			if convention == "other" || convention == "skip" {
				continue
			}
			conventions[convention]++
		}
		for _, m := range ncSQLColRE.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			convention := classifyName(name)
			if convention == "other" || convention == "skip" {
				continue
			}
			conventions[convention]++
		}
		re = nil
	default:
		re = ncGenericRE
	}

	if re != nil {
		for _, m := range re.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			convention := classifyName(name)
			if convention == "other" || convention == "skip" {
				continue
			}
			conventions[convention]++
		}
	}

	// NOTE: Do NOT emit per-convention entities (naming_convention_<type>).
	// Python only emits the per-file summary entity (naming_convention@<file>).
	// Emitting per-convention entities creates ghost entities that fail parity.

	// Emit per-file summary entity: naming_convention@<file>
	// Lists all conventions found in this file, matching Python parity.
	if len(conventions) > 0 {
		var conventionList []string
		for conv := range conventions {
			conventionList = append(conventionList, conv)
		}
		// Sort for deterministic output
		sortStrings(conventionList)
		// Normalize language tag: Python uses "protobuf" for proto files.
		emitLang := language
		if emitLang == "proto" {
			emitLang = "protobuf"
		}
		results = append(results, makeEntity(filePath,
			fmt.Sprintf("naming_convention@%s", filePath),
			"SCOPE.Pattern", "naming_convention", emitLang, 1,
			map[string]string{
				"kind":        "naming_convention",
				"summary":     "true",
				"conventions": strings.Join(conventionList, ","),
				"total_names": fmt.Sprintf("%d", totalNames(conventions)),
			}))
	}

	return results
}

func sortStrings(s []string) { sort.Strings(s) }

func totalNames(m map[string]int) int {
	total := 0
	for _, v := range m {
		total += v
	}
	return total
}

func init() {
	Register(&namingConventionDetector{})
}
