// custom_dispatch.go provides discovery and safe invocation of custom
// framework extractors registered by internal/custom/<lang>/ sub-packages.
//
// Custom extractors live in the same global registry as base language
// extractors, but use prefixed keys that encode their target language:
//
//	python_*          → Python framework extractors (Django, Flask, …)
//	custom_<lang>_*   → All other languages (custom_go_gin, custom_js_react, …)
//
// The language-to-prefix mapping in customPrefixForLanguage is the single
// source of truth for dispatch. Languages whose base key is shared with the
// prefix namespace (e.g. Python) are mapped to their own prefix.
//
// Registered keys ending at exactly the prefix itself (e.g. "python" for
// language=python) are NOT treated as custom extractors — only keys strictly
// longer than the prefix qualify.
//
// This file wires MX-1044: invoke registered custom extractors in the
// extraction pipeline after base language extraction.
package extractors

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// customPrefixForLanguage maps a canonical base language name to the
// registry-key prefix used by its custom framework extractors.
//
// Languages not listed here have no custom extractors (yet) and will return
// an empty list from CustomExtractorsFor.
var customPrefixForLanguage = map[string]string{
	"python":     "python_",
	"go":         "custom_go_",
	"javascript": "custom_js_",
	"typescript": "custom_js_", // TS reuses the JS framework extractor set
	"java":       "custom_java_",
	"kotlin":     "custom_kotlin_",
	"scala":      "custom_scala_",
	"ruby":       "custom_ruby_",
	"php":        "custom_php_",
	"rust":       "custom_rust_",
	"swift":      "custom_swift_",
	"dart":       "custom_dart_",
	"elixir":     "custom_elixir_",
	"csharp":     "custom_csharp_",
	"cpp":        "custom_cpp_",
}

// CustomExtractorsFor returns all registered custom/framework extractors
// whose registry key matches the custom-extractor prefix for the given
// base language. The result is sorted by language key for deterministic
// dispatch order (important for test stability and parity comparison).
//
// Returns an empty slice if the language has no custom extractor prefix
// or if no custom extractors are currently registered for that prefix.
// Never returns nil.
func CustomExtractorsFor(language string) []Extractor {
	prefix, ok := customPrefixForLanguage[language]
	if !ok {
		return []Extractor{}
	}

	var keys []string
	for _, k := range extractor.List() {
		if strings.HasPrefix(k, prefix) && len(k) > len(prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	out := make([]Extractor, 0, len(keys))
	for _, k := range keys {
		ext, ok := extractor.Get(k)
		if !ok {
			continue // race: key removed between List() and Get(); skip.
		}
		out = append(out, ext)
	}
	return out
}

// RunCustomExtractors dispatches every custom extractor matching file.Language
// and returns the merged entity list from all successful invocations.
//
// Semantics (matches MX-1044 acceptance criteria):
//   - Each extractor is invoked in its own panic-recovery wrapper; a panic in
//     one extractor logs+continues and does not abort the pipeline.
//   - Errors from individual extractors are collected into the returned
//     errs slice but never short-circuit the dispatch.
//   - Partial output is preserved on error: extractors returning both a slice
//     and an error still contribute their entities.
//   - An OTel span "extractor.custom_dispatch" wraps the whole dispatch with
//     attributes custom_extractor_count, language, file and entity_count so
//     operators can observe framework-pass work per file.
//
// The caller is responsible for merging the returned entities with base
// extractor output and running the final dedup pass (see MergeWithCustom).
func RunCustomExtractors(ctx context.Context, file FileInput) (entities []types.EntityRecord, errs []error) {
	exts := CustomExtractorsFor(file.Language)

	start := time.Now()
	t := getTracer()
	var span trace.Span
	if t != nil {
		ctx, span = t.Start(ctx, "extractor.custom_dispatch",
			trace.WithAttributes(
				attribute.String("language", file.Language),
				attribute.String("file", file.Path),
				attribute.Int("custom_extractor_count", len(exts)),
			),
		)
		defer func() {
			span.SetAttributes(
				attribute.Int64("duration_ms", time.Since(start).Milliseconds()),
				attribute.Int("entity_count", len(entities)),
				attribute.Int("error_count", len(errs)),
			)
			if len(errs) > 0 {
				// Don't mark the span as Error — partial failures are expected
				// and must not flag the whole pipeline. Errors are recorded
				// as events so operators can still see them.
				for _, e := range errs {
					span.RecordError(e)
				}
			}
			span.End()
		}()
	}

	if len(exts) == 0 {
		return nil, nil
	}

	for _, ext := range exts {
		recs, err := safeExtract(ctx, ext, file)
		if err != nil {
			errs = append(errs, fmt.Errorf("custom extractor %s on %s: %w", ext.Language(), file.Path, err))
		}
		entities = append(entities, recs...)
	}
	return entities, errs
}

// MergeWithCustom merges baseEntities with customEntities applying the
// MX-1044 dedup rule: when a custom extractor emits an entity with the same
// Name as a base extractor entity, the custom extractor's version wins.
//
// The merge preserves the original ordering of base entities, replacing any
// base entity in place when a custom entity with the same Name exists. Custom
// entities whose Name does not collide with any base entity are appended after
// the base entities in their dispatch order (already deterministic via
// CustomExtractorsFor's sort).
//
// This function does NOT perform cross-kind dedup — that is handled downstream
// by run-extractor's deduplicateEntities pass which runs after this merge.
func MergeWithCustom(baseEntities, customEntities []types.EntityRecord) []types.EntityRecord {
	if len(customEntities) == 0 {
		return baseEntities
	}

	// Index the first custom entity per Name (dispatch order).
	customByName := make(map[string]int, len(customEntities))
	for i, e := range customEntities {
		if _, seen := customByName[e.Name]; !seen {
			customByName[e.Name] = i
		}
	}

	used := make(map[int]bool, len(customEntities))
	merged := make([]types.EntityRecord, 0, len(baseEntities)+len(customEntities))

	// Replace base entities in place when a custom entity shares the Name.
	for _, be := range baseEntities {
		if idx, ok := customByName[be.Name]; ok && !used[idx] {
			merged = append(merged, customEntities[idx])
			used[idx] = true
			continue
		}
		merged = append(merged, be)
	}

	// Append remaining custom entities that did not replace a base entity.
	for i, ce := range customEntities {
		if used[i] {
			continue
		}
		merged = append(merged, ce)
	}
	return merged
}
