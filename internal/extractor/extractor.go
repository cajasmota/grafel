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

// FileEntity returns a per-source-file SCOPE.Component (subtype="file")
// entity that all per-language extractors emit at the top of Extract so
// the cross-repo import linker (#566) can map IMPORTS edges back to the
// originating repo via the resolver's byName index.
//
// Issue #577 — generalises the JS/TS pattern from #570/#575 to every
// per-language extractor. Without this entity, IMPORTS edges whose
// FromID is the importing file path don't appear in the resolver's
// entity-id → repo index, so the cross-repo linker silently skips
// them. With it, ReferencesEmbeddedWithAllowlist rewrites the IMPORTS
// FromID from the path string to the file entity's stamped hex ID
// (graph.EntityID(repoTag, "SCOPE.Component", path, path)) via byName,
// and the linker then matches the edge back to the source repo.
//
// The extractor deliberately keeps FromID = file path on the IMPORTS
// edges themselves (not a pre-stamped hex): it doesn't know the
// indexer's repoTag seed, so any hex it wrote would be short-circuited
// by isHexID in the resolver and the rewrite would never happen.
func FileEntity(file FileInput) types.EntityRecord {
	return types.EntityRecord{
		Name:       file.Path,
		Kind:       "SCOPE.Component",
		SourceFile: file.Path,
		Language:   file.Language,
		Subtype:    "file",
		Properties: map[string]string{
			"kind":    "SCOPE.Component",
			"subtype": "file",
		},
		EnrichmentStatus: types.StatusPending,
		QualityScore:     1.0,
	}
}

// TagEntitiesLanguage stamps Language = lang and Properties["language"] = lang
// on every entity in records. It is the symmetric counterpart of
// TagRelationshipsLanguage for entities (issue #2371).
//
// Both fields are set so that consumers that read EntityRecord.Language and
// consumers that consult Properties["language"] (e.g. the resolver's
// per-language dynamic-pattern dispatch — the tunnel introduced in PR #2365)
// see a consistent value.
//
// Entities that already carry a non-empty Language are left untouched so that
// explicit per-extractor overrides (e.g. a dialect variant) are preserved.
// Properties maps are allocated lazily. No-op when lang is empty.
func TagEntitiesLanguage(records []types.EntityRecord, lang string) {
	if lang == "" {
		return
	}
	for i := range records {
		e := &records[i]
		// If the entity already carries an explicit language assignment, leave
		// both Language and Properties["language"] untouched so per-extractor
		// dialect overrides are preserved.
		if e.Language != "" {
			continue
		}
		e.Language = lang
		if e.Properties == nil {
			e.Properties = map[string]string{"language": lang}
			continue
		}
		if _, ok := e.Properties["language"]; !ok {
			e.Properties["language"] = lang
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
