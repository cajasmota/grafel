// Package extractor defines the core Extractor interface, FileInput type,
// and the global registration function used by all language extractor
// sub-packages.
//
// This package is intentionally kept dependency-light so that extractor
// sub-packages (e.g., internal/extractors/golang) can import it without
// creating an import cycle with the dispatch layer (internal/extractors).
package extractor

import (
	"context"
	"sync"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/types"
)

// FileInput is the input contract for all language extractors.
type FileInput struct {
	// Path is the file path relative to repo root (used as source_file in entities).
	Path string
	// Content is the raw source bytes.
	Content []byte
	// Language is the canonical language name (e.g., "python", "go", "typescript").
	Language string
	// Tree is the tree-sitter parse tree. May be nil if parsing was skipped.
	Tree *sitter.Tree
	// RepoRoot is the absolute filesystem path of the repository root.
	// Optional — extractors that need to read project-level configuration
	// (e.g. JS/TS path-alias maps in tsconfig.json / vite.config / metro
	// / babel.config — issue #505) consult this. Empty string is
	// permitted; alias-aware extractors fall back to a no-op map.
	RepoRoot string
}

// Extractor is the interface all language extractors must implement.
type Extractor interface {
	// Extract processes a parsed source file and returns entity records.
	// Implementations must return partial results on failure — never abort
	// the whole file because of a single node extraction error.
	Extract(ctx context.Context, file FileInput) ([]types.EntityRecord, error)
	// Language returns the canonical language name this extractor handles.
	Language() string
}

// registry is the global extractor map. Populated by extractor sub-package
// init() calls via Register.
var (
	mu       sync.RWMutex
	registry = make(map[string]Extractor)
)

// Register adds an extractor to the global registry.
// Typically called from init() functions in extractor sub-packages.
// Registering the same language name twice overwrites the previous extractor.
func Register(language string, e Extractor) {
	mu.Lock()
	defer mu.Unlock()
	registry[language] = e
}

// Get retrieves the extractor registered for the given language.
// Returns false if no extractor is registered for that language.
func Get(language string) (Extractor, bool) {
	mu.RLock()
	defer mu.RUnlock()
	e, ok := registry[language]
	return e, ok
}

// TagRelationshipsLanguage stamps Properties["language"] = lang on every
// embedded relationship of every record (and recursively on nested records
// where applicable). Issue #90: the resolver's per-language dynamic-pattern
// dispatch consults this property to pick the right pattern catalog. Without
// it, pass-2 standalone rels and a chunk of embedded rels fall through to
// the cross-language catalog only and the dynamic disposition stays at ~0%.
//
// Existing Properties[language] values are preserved (per-extractor or
// per-rel overrides win). Properties maps are allocated lazily.
func TagRelationshipsLanguage(records []types.EntityRecord, lang string) {
	if lang == "" {
		return
	}
	for i := range records {
		rels := records[i].Relationships
		for j := range rels {
			r := &rels[j]
			if r.Properties == nil {
				r.Properties = map[string]string{"language": lang}
				continue
			}
			if _, ok := r.Properties["language"]; !ok {
				r.Properties["language"] = lang
			}
		}
	}
}

// TagStandaloneRelationshipsLanguage is TagRelationshipsLanguage for a slice
// of standalone (pass-2) relationships rather than entity-embedded ones.
func TagStandaloneRelationshipsLanguage(rels []types.RelationshipRecord, lang string) {
	if lang == "" {
		return
	}
	for j := range rels {
		r := &rels[j]
		if r.Properties == nil {
			r.Properties = map[string]string{"language": lang}
			continue
		}
		if _, ok := r.Properties["language"]; !ok {
			r.Properties["language"] = lang
		}
	}
}

// List returns a snapshot of all registered language names (unsorted).
func List() []string {
	mu.RLock()
	defer mu.RUnlock()
	langs := make([]string, 0, len(registry))
	for lang := range registry {
		langs = append(langs, lang)
	}
	return langs
}
