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
	"os"
	"strconv"
	"sync"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/grafel/internal/types"
)

// ExtractorConfig carries per-extractor feature toggles that were previously
// communicated exclusively via ad-hoc environment variables. The Config channel
// is the primary source; env vars remain as a backward-compatible fallback so
// that existing scripts (e.g. GRAFEL_MARKDOWN_EMIT_HEADINGS=1) continue
// to work unchanged.
//
// Priority: Config field (if non-nil and the relevant field is set) wins over
// the corresponding env var. A nil Config pointer or an unset field falls
// through to the env var. A missing env var uses the documented default.
//
// Population: the indexer constructs an ExtractorConfig from env vars (and
// optionally a future config file) and stamps it on every FileInput before
// dispatch. Extractors must not call os.Getenv directly — use the typed
// accessor methods (EmitHeadings, EmitDestructureDetail, etc.) which implement
// the Config-first / env-fallback logic in one place.
type ExtractorConfig struct {
	// Incremental reindex toggles (GRAFEL_INCREMENTAL_REINDEX,
	// GRAFEL_INCREMENTAL_MAX_FILES). The effective default when neither
	// Config nor env sets a value is ON (#5231) — see IsIncrementalEnabled;
	// the zero value here only means "not explicitly set via Config"
	// (IncrementalReindexSet=false), which defers to that default.
	IncrementalReindex  bool
	IncrementalMaxFiles int // 0 means "auto" (gitmeta-based heuristic)

	// MarkdownEmitHeadings controls SCOPE.Heading entity emission.
	// Corresponds to GRAFEL_MARKDOWN_EMIT_HEADINGS. Tri-state:
	//   nil  — not set in Config; fall through to env var
	//   &true — emit headings (overrides env)
	//   &false — suppress headings (overrides env)
	MarkdownEmitHeadings *bool

	// JSEmitDestructureDetail controls const_destructure / const_destructure_call
	// subtype emission. Corresponds to GRAFEL_EMIT_DESTRUCTURE_DETAIL.
	// Tri-state: nil means "not set in Config; fall through to env var".
	JSEmitDestructureDetail *bool

	// IncrementalReindexSet records whether IncrementalReindex was explicitly
	// set via Config (as opposed to being the zero value). This lets callers
	// distinguish "Config says false" from "Config not consulted".
	IncrementalReindexSet bool
}

// boolFromEnv parses a truthy env var ("1" or "true") and returns (value, true)
// when the var is set to a recognised value. Returns (false, false) when the
// var is unset or empty.
func boolFromEnv(name string) (bool, bool) {
	v := os.Getenv(name)
	if v == "1" || v == "true" {
		return true, true
	}
	if v != "" {
		// Recognised-but-not-truthy value — var IS set, value is falsy.
		return false, true
	}
	return false, false
}

// ConfigFromEnv builds an ExtractorConfig populated entirely from the process
// environment. This is the default population path used by the indexer before
// Config-file support is wired in. Calling this once per index run and
// attaching the result to every FileInput keeps os.Getenv out of the hot
// per-file extract path and makes the toggles testable without mutating the
// process environment.
func ConfigFromEnv() ExtractorConfig {
	cfg := ExtractorConfig{}

	// Incremental reindex.
	if v, ok := boolFromEnv("GRAFEL_INCREMENTAL_REINDEX"); ok {
		cfg.IncrementalReindex = v
		cfg.IncrementalReindexSet = true
	}
	if raw := os.Getenv("GRAFEL_INCREMENTAL_MAX_FILES"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			cfg.IncrementalMaxFiles = n
		}
	}

	// Markdown heading emission.
	if v, ok := boolFromEnv("GRAFEL_MARKDOWN_EMIT_HEADINGS"); ok {
		cfg.MarkdownEmitHeadings = &v
	}

	// JS destructure-detail emission.
	if v, ok := boolFromEnv("GRAFEL_EMIT_DESTRUCTURE_DETAIL"); ok {
		cfg.JSEmitDestructureDetail = &v
	}

	return cfg
}

// EmitHeadings returns whether SCOPE.Heading entities should be emitted by
// the markdown extractor. Config wins; env var is the fallback; default is false.
func (c *ExtractorConfig) EmitHeadings() bool {
	if c != nil && c.MarkdownEmitHeadings != nil {
		return *c.MarkdownEmitHeadings
	}
	v, _ := boolFromEnv("GRAFEL_MARKDOWN_EMIT_HEADINGS")
	return v
}

// EmitDestructureDetail returns whether const_destructure / const_destructure_call
// subtypes should be emitted by the JS/TS extractor. Config wins; env var is the
// fallback; default is false.
func (c *ExtractorConfig) EmitDestructureDetail() bool {
	if c != nil && c.JSEmitDestructureDetail != nil {
		return *c.JSEmitDestructureDetail
	}
	v, _ := boolFromEnv("GRAFEL_EMIT_DESTRUCTURE_DETAIL")
	return v
}

// IsIncrementalEnabled reports whether the incremental reindex path is active.
// Config (when IncrementalReindexSet=true) wins; env var is the fallback.
//
// Default flipped to ON (#5231, the reindex-storm fix): the incremental
// file-level patch is ~25× faster than a full reindex for single-file edits
// and has a proven safe fall-through to the full IndexFn on any precondition
// failure. Operators can still force the legacy full-reindex-every-time
// behaviour with GRAFEL_INCREMENTAL_REINDEX=0 (or =false), which boolFromEnv
// reports as an explicitly-set falsy value.
func (c *ExtractorConfig) IsIncrementalEnabled() bool {
	if c != nil && c.IncrementalReindexSet {
		return c.IncrementalReindex
	}
	v, set := boolFromEnv("GRAFEL_INCREMENTAL_REINDEX")
	if !set {
		return true // default ON (#5231)
	}
	return v
}

// EffectiveIncrementalMaxFiles returns the effective trigger-limit for the
// incremental reindex path, given the current Config and the env var fallback.
// Returns 0 when neither source sets a value (caller should apply branch-aware
// heuristic).
func (c *ExtractorConfig) EffectiveIncrementalMaxFiles() int {
	if c != nil && c.IncrementalMaxFiles > 0 {
		return c.IncrementalMaxFiles
	}
	if raw := os.Getenv("GRAFEL_INCREMENTAL_MAX_FILES"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// FileInput is the input contract for all language extractors.
//
// Config (issue #2320) is the typed channel for per-extractor feature toggles
// that previously required ad-hoc environment variables. Callers (the indexer,
// TryIncremental, and tests) should populate Config before dispatch. When Config
// is nil the extractors fall through to their env-var fallbacks so existing
// scripts and tests that set env vars directly continue to work unchanged.
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
	// Config carries typed per-extractor feature toggles (issue #2320).
	// Nil is valid — all accessors fall through to the env-var fallbacks.
	// The indexer populates this from ConfigFromEnv() before dispatch.
	Config *ExtractorConfig
	// Pass1Entities is the side-channel that threads Pass 1's per-file
	// entity records forward to Pass 2.5 engine passes (issue #2352).
	//
	// The original ORM field-edge synthesis (#2279 / #2295) reconstructed
	// the `<Model>.<field>` index by re-parsing the source with a regex
	// because Pass 1's SCOPE.Schema(subtype=field) entities weren't
	// visible inside the YAML-driven detector. This field plumbs them
	// through: the indexer groups Pass 1 records by source file in
	// runPass25FrameworkRules and stamps the matching slice here before
	// calling Detector.Detect.
	//
	// Nil / empty is valid — engine passes that consume this MUST fall
	// back to their pre-#2352 source-scan behaviour so direct test
	// fixtures (which call Detector.Detect without going through the full
	// pipeline) keep working unchanged.
	Pass1Entities []types.EntityRecord

	// CrossFileFields is the cross-file ORM field-resolution closure
	// (issue #2448 / Phase B).
	//
	// applyORMFieldEdges resolves <Model>.<field> references first via
	// Pass1Entities (intra-file), and falls back to this closure when the
	// model lives in a SIBLING file (the canonical Django split:
	// `models.py` defines `class User` while `views.py` imports it and
	// calls `User.objects.filter(cognito_id=…)`).
	//
	// The closure is built ONCE per indexing run by the coordinator
	// (in-process: cmd/grafel/index.go runPass25FrameworkRules;
	// subprocess: internal/daemon/extract/subproc.go) from the union of
	// SCOPE.Schema(subtype=field) records across ALL files in the
	// indexed scope (or the batch, in the subprocess case). It is then
	// attached to every per-file FileInput before Detect.
	//
	// Lookup contract: given a model class name (e.g. "User"), return
	// every SCOPE.Schema(subtype=field) entity for that model regardless
	// of source file. Returning nil / empty is valid and means "no
	// cross-file fields known for this model" — the caller drops the
	// edge silently.
	//
	// Nil is valid — direct test fixtures that don't go through the
	// coordinator path get the intra-file-only behaviour unchanged.
	CrossFileFields func(modelName string) []types.EntityRecord
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
