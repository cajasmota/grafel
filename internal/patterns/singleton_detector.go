package patterns

import (
	"regexp"

	"github.com/cajasmota/archigraph/internal/types"
)

// singletonDetector detects singleton pattern implementations.
// Matches Python singleton_detector.py.
type singletonDetector struct{}

var (
	sdJavaAnnotRE      = regexp.MustCompile(`@(?:Singleton|ApplicationScoped)\b`)
	sdGetInstanceRE    = regexp.MustCompile(`\bgetInstance\s*\(\s*\)`)
	sdPyModuleAssignRE = regexp.MustCompile(`(?m)^[A-Z][A-Z0-9_]*\s*=\s*\w+\s*\(`)
	sdGoSyncOnceRE     = regexp.MustCompile(`\bsync\.Once\b`)
	sdGoOnceDo         = regexp.MustCompile(`\.Do\s*\(`)
	sdCSharpInstanceRE = regexp.MustCompile(`private\s+static\s+\w+\s+_?[Ii]nstance`)
)

func (s *singletonDetector) Category() string { return "singleton" }

func (s *singletonDetector) AppliesTo(src string) bool {
	return sdJavaAnnotRE.MatchString(src) ||
		sdGetInstanceRE.MatchString(src) ||
		sdGoSyncOnceRE.MatchString(src) ||
		sdCSharpInstanceRE.MatchString(src) ||
		sdPyModuleAssignRE.MatchString(src)
}

func (s *singletonDetector) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	emit := func(key, name, impl string, line int) {
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			name, "SCOPE.Pattern", "singleton", language, line,
			map[string]string{"kind": "singleton", "implementation": impl}))
	}

	switch language {
	case "java", "kotlin":
		if m := sdJavaAnnotRE.FindStringIndex(src); m != nil {
			emit("java:annotation", "singleton_annotation", "annotation_singleton", lineOf(src, m[0]))
		}
		if m := sdGetInstanceRE.FindStringIndex(src); m != nil {
			emit("java:get_instance", "singleton_get_instance", "static_factory_singleton", lineOf(src, m[0]))
		}
	case "go":
		if sdGoSyncOnceRE.MatchString(src) && sdGoOnceDo.MatchString(src) {
			m := sdGoSyncOnceRE.FindStringIndex(src)
			emit("go:sync_once", "singleton_sync_once", "sync_once_singleton", lineOf(src, m[0]))
		}
	case "csharp":
		if m := sdCSharpInstanceRE.FindStringIndex(src); m != nil {
			emit("csharp:static_instance", "singleton_csharp", "static_instance_singleton", lineOf(src, m[0]))
		}
	case "python":
		if m := sdPyModuleAssignRE.FindStringIndex(src); m != nil {
			emit("py:module_level", "singleton_py_module", "module_level_singleton", lineOf(src, m[0]))
		}
	default:
		// Generic: check for getInstance or sync.Once
		if m := sdGetInstanceRE.FindStringIndex(src); m != nil {
			emit("generic:get_instance", "singleton_generic", "static_factory_singleton", lineOf(src, m[0]))
		}
	}

	return results
}

func init() {
	Register(&singletonDetector{})
}
