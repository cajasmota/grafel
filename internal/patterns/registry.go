package patterns

import (
	"sync"

	"github.com/cajasmota/archigraph/internal/types"
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
func makeEntity(filePath, name, kind, subtype, language string, startLine int, props map[string]string) types.EntityRecord {
	e := types.EntityRecord{
		Name:       name,
		Kind:       kind,
		Subtype:    subtype,
		SourceFile: filePath,
		StartLine:  startLine,
		Language:   normalizeLanguage(language),
		Properties: props,
	}
	e.ID = e.ComputeID()
	return e
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
