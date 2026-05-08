package patterns

import (
	"fmt"
	"regexp"

	"github.com/cajasmota/archigraph/internal/types"
)

// reExportDetector detects re-export barrel files.
// Matches Python re_export_detector.py.
type reExportDetector struct{}

var (
	reJSNamedReexportRE     = regexp.MustCompile(`export\s*\{[^}]+\}\s*from\s*["']([^"']+)["']`)
	reJSWildcardReexportRE  = regexp.MustCompile(`export\s*\*\s*from\s*["']([^"']+)["']`)
	reJSNamespaceReexportRE = regexp.MustCompile(`export\s*\*\s+as\s+(\w+)\s+from\s*["']([^"']+)["']`)
	rePyAllAssignRE         = regexp.MustCompile(`__all__\s*=\s*\[([^\]]+)\]`)
	rePyWildcardImportRE    = regexp.MustCompile(`from\s+(\S+)\s+import\s+\*`)
	reRustPubUseRE          = regexp.MustCompile(`pub\s+use\s+([^;]+);`)
)

func (r *reExportDetector) Category() string { return "re_export" }

func (r *reExportDetector) AppliesTo(src string) bool {
	return reJSNamedReexportRE.MatchString(src) ||
		reJSWildcardReexportRE.MatchString(src) ||
		rePyAllAssignRE.MatchString(src) ||
		rePyWildcardImportRE.MatchString(src) ||
		reRustPubUseRE.MatchString(src)
}

func (r *reExportDetector) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	emit := func(key, name, from, kind string, line int) {
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			name, "SCOPE.Pattern", "re_export", language, line,
			map[string]string{"kind": "re_export", "re_export_kind": kind, "from_module": from}))
	}

	// JS/TS named re-exports
	for idx, m := range reJSNamedReexportRE.FindAllStringSubmatchIndex(src, -1) {
		from := src[m[2]:m[3]]
		emit(fmt.Sprintf("js:named:%s", from), fmt.Sprintf("re_export_named_%d", idx), from, "named", lineOf(src, m[0]))
	}

	// JS/TS wildcard re-exports
	for idx, m := range reJSWildcardReexportRE.FindAllStringSubmatchIndex(src, -1) {
		from := src[m[2]:m[3]]
		emit(fmt.Sprintf("js:wildcard:%s", from), fmt.Sprintf("re_export_wildcard_%d", idx), from, "wildcard", lineOf(src, m[0]))
	}

	// JS/TS namespace re-exports
	for idx, m := range reJSNamespaceReexportRE.FindAllStringSubmatchIndex(src, -1) {
		ns := src[m[2]:m[3]]
		from := src[m[4]:m[5]]
		emit(fmt.Sprintf("js:namespace:%s", ns), fmt.Sprintf("re_export_namespace_%d", idx), from, "namespace", lineOf(src, m[0]))
	}

	// Python __all__
	if m := rePyAllAssignRE.FindStringIndex(src); m != nil {
		emit("py:__all__", "py_all_exports", filePath, "python_all", lineOf(src, m[0]))
	}

	// Python wildcard imports
	for idx, m := range rePyWildcardImportRE.FindAllStringSubmatchIndex(src, -1) {
		from := src[m[2]:m[3]]
		emit(fmt.Sprintf("py:wildcard:%s", from), fmt.Sprintf("py_wildcard_import_%d", idx), from, "python_wildcard", lineOf(src, m[0]))
	}

	// Rust pub use
	for idx, m := range reRustPubUseRE.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		emit(fmt.Sprintf("rust:pub_use:%s", path), fmt.Sprintf("rust_pub_use_%d", idx), path, "rust_pub_use", lineOf(src, m[0]))
	}

	return results
}

func init() {
	Register(&reExportDetector{})
}
