package patterns

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/cajasmota/grafel/internal/types"
)

// PatternDetector is the interface all pattern detectors must implement.
// Detectors are language-agnostic: they run on raw source text and file path.
type PatternDetector interface {
	// Category returns the detector category name (e.g. "cors", "sql_injection_risk").
	Category() string
	// AppliesTo returns true when the detector should run on the given source text.
	// Fast-path gate: return false quickly when no relevant tokens are present.
	AppliesTo(src string) bool
	// Detect scans src and returns extracted EntityRecord entries.
	// Must return partial results on error — never abort the whole file.
	Detect(filePath, language string, src string) []types.EntityRecord
}

var (
	mu       sync.RWMutex
	registry []PatternDetector
)

// Register adds a PatternDetector to the global registry.
// Typically called from init() in each detector file.
func Register(d PatternDetector) {
	mu.Lock()
	defer mu.Unlock()
	registry = append(registry, d)
}

// All returns a snapshot of all registered detectors.
func All() []PatternDetector {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]PatternDetector, len(registry))
	copy(out, registry)
	return out
}

// ClearForTesting removes all registered detectors.
// Only for use in unit tests.
func ClearForTesting() {
	mu.Lock()
	defer mu.Unlock()
	registry = nil
}

// normalizeLanguage maps language aliases to canonical names used in Python golden fixtures.
func normalizeLanguage(lang string) string {
	switch lang {
	case "proto":
		return "protobuf"
	default:
		return lang
	}
}

// makeEntity constructs a minimal EntityRecord for a pattern match.
//
// Issue #1725 — SCOPE.Pattern entities were emitted with empty
// qualified_name in 100% of cases (1156/1156 on acme-core). The fix
// derives a stable, file-scoped QN from the source path so downstream
// consumers (denoise, dashboards, search) can resolve the pattern back
// to its containing module. Format:
//
//	<file-module-path>.<pattern-name>
//
// where file-module-path is the source-file path with its extension
// stripped and slashes converted to dots (e.g.
// "core/serializers/deficiency_serializer.py" →
// "core.serializers.deficiency_serializer"). The trailing pattern-name
// already encodes kind + line (e.g. "error_handling:try_catch:179"),
// keeping the QN unique within a file and stable across runs.
func makeEntity(filePath, name, kind, subtype, language string, startLine int, props map[string]string) types.EntityRecord {
	types.ValidateProducedEntityKind("internal/patterns.makeEntity", kind)
	e := types.EntityRecord{
		Name:          name,
		QualifiedName: derivePatternQualifiedName(filePath, name),
		Kind:          kind,
		Subtype:       subtype,
		SourceFile:    filePath,
		StartLine:     startLine,
		Language:      normalizeLanguage(language),
		Properties:    props,
	}
	e.ID = e.ComputeID()
	return e
}

// derivePatternQualifiedName builds a file-scoped dotted module path and
// appends the pattern's leaf name. The result is deterministic and stable:
// the same (filePath, name) pair always produces the same QN. Empty
// filePath falls back to the bare name so the QN is never blank.
func derivePatternQualifiedName(filePath, name string) string {
	if name == "" {
		return ""
	}
	mod := pathToModule(filePath)
	if mod == "" {
		return name
	}
	return mod + "." + name
}

// pathToModule converts a repo-relative source path to a dotted module
// path: strips the extension and replaces slashes with dots. Backslashes
// (Windows-style) are normalised to forward slashes first. Returns ""
// when the path is empty or has no resolvable stem.
func pathToModule(filePath string) string {
	if filePath == "" {
		return ""
	}
	p := strings.ReplaceAll(filePath, "\\", "/")
	// Strip extension (last "." after the final "/").
	if ext := filepath.Ext(p); ext != "" {
		p = strings.TrimSuffix(p, ext)
	}
	// Trim leading "./" or "/".
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return ""
	}
	return strings.ReplaceAll(p, "/", ".")
}

// lineOf returns the 1-indexed line number for a byte offset in src.
func lineOf(src string, offset int) int {
	line := 1
	for i := 0; i < offset && i < len(src); i++ {
		if src[i] == '\n' {
			line++
		}
	}
	return line
}
