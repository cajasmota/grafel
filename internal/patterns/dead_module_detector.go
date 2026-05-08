package patterns

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// deadModuleDetector detects potentially unused/dead modules.
// Matches Python dead_module_detector.py.
type deadModuleDetector struct{}

var (
	deadJSExportRE         = regexp.MustCompile(`(?m)^\s*export\s+(?:default\s+)?(?:function|class|const|let|var|interface|type)\s+(\w+)`)
	deadJSImportRE         = regexp.MustCompile(`import\s+(?:\{[^}]+\}|[\w*]+)\s+from\s+["']([^"']+)["']`)
	deadPyDefRE            = regexp.MustCompile(`(?m)^def\s+(\w+)\s*\(`)
	deadGoFuncRE           = regexp.MustCompile(`(?m)^func\s+([A-Za-z_]\w*)\s*\(`)
)

func (d *deadModuleDetector) Category() string { return "dead_module" }

func (d *deadModuleDetector) AppliesTo(src string) bool {
	// Applies when file has exports or public functions
	return deadJSExportRE.MatchString(src) ||
		deadGoFuncRE.MatchString(src) ||
		deadPyDefRE.MatchString(src)
}

func (d *deadModuleDetector) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord

	switch language {
	case "javascript", "typescript":
		exports := deadJSExportRE.FindAllStringSubmatch(src, -1)
		imports := deadJSImportRE.FindAllStringSubmatch(src, -1)
		// If file has exports but no imports, might be a leaf
		if len(exports) > 0 && len(imports) == 0 {
			for _, e := range exports {
				results = append(results, makeEntity(filePath,
					"dead_export_"+e[1], "SCOPE.Pattern", "dead_module_candidate", language, 1,
					map[string]string{"kind": "dead_module", "exported_name": e[1], "reason": "no_imports"}))
			}
		}
	case "go":
		// Go: unexported-only file with no test file is a candidate
		if !strings.Contains(filePath, "_test.go") {
			funcs := deadGoFuncRE.FindAllStringSubmatch(src, -1)
			hasPublic := false
			for _, f := range funcs {
				if len(f[1]) > 0 && f[1][0] >= 'A' && f[1][0] <= 'Z' {
					hasPublic = true
					break
				}
			}
			if !hasPublic && len(funcs) > 0 && !strings.Contains(filePath, "internal/") {
				results = append(results, makeEntity(filePath,
					"dead_module_go_unexported", "SCOPE.Pattern", "dead_module_candidate", language, 1,
					map[string]string{"kind": "dead_module", "reason": "no_public_exports"}))
			}
		}
	}

	return results
}

func init() {
	Register(&deadModuleDetector{})
}
