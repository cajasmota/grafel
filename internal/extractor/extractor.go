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
