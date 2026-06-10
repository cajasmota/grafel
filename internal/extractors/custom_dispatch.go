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
// This file wires : invoke registered custom extractors in the
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
	// Prisma schema files (.prisma) and raw SQL migration files (.sql) are
	// classified as their own languages but carry ORM model/migration content
	// the JS custom extractors parse (Prisma model DSL, Prisma/Drizzle
	// migration SQL). Route them to the JS extractor set; each extractor
	// path/language-gates internally and no-ops on files it does not own.
	"prisma": "custom_js_",
	"sql":    "custom_js_",
	"java":   "custom_java_",
	"kotlin": "custom_kotlin_",
	// Lua framework extractors (OpenResty, Lapis, Kong, APISIX) register under
	// the bare `lua_` prefix (lua_routing, lua_middleware, lua_kong, …) rather
	// than `custom_lua_`. The base "lua" tree-sitter extractor key is shorter
	// than this prefix, so the `len(k) > len(prefix)` guard in
	// CustomExtractorsFor excludes it and only the framework extractors match.
	"lua":   "lua_",
	"scala": "custom_scala_",
	"ruby":  "custom_ruby_",
	"php":   "custom_php_",
	// GraphQL SDL files (.graphql/.gql) are classified as their own "graphql"
	// language but carry Lighthouse (Laravel) server-side resolver directives
	// (@all, @paginate, @field, …) parsed by the PHP custom Lighthouse
	// extractor. Route them to the PHP extractor set; each php-language
	// extractor gates on language=="php" internally and no-ops, while the
	// Lighthouse extractor gates on language=="graphql" plus a Lighthouse
	// directive signal. Mirrors the prisma/sql → JS routing above.
	"graphql": "custom_php_",
	"rust":    "custom_rust_",
	"swift":   "custom_swift_",
	"dart":    "custom_dart_",
	"elixir":  "custom_elixir_",
	"csharp":  "custom_csharp_",
	"cpp":     "custom_cpp_",
	// Protocol Buffers IDL files (.proto) are classified as their own
	// "protobuf" language but carry message/service definitions parsed by the
	// C/C++ protobuf custom extractor (which path/language-gates internally and
	// no-ops on non-proto files). Route them to the C++ extractor set, mirroring
	// the prisma/sql → JS routing above.
	"protobuf": "custom_cpp_",
}

// extraCustomPrefixesForLanguage lists ADDITIONAL custom-extractor prefixes a
// language's files should be dispatched to, beyond its primary prefix in
// customPrefixForLanguage. This covers files whose content is owned by more
// than one language family.
//
// graphql: standalone `.graphql` SDL schema files are the model surface for the
// grafeo-ogm Neo4j TS OGM (custom_js_grafeo), which declares its entire graph
// model in GraphQL SDL — either inline in a TS template literal (handled by the
// typescript/javascript primary prefix) or in a standalone `.graphql` file
// (handled here). The grafeo extractor gates on a @node + @relationship signal,
// so it no-ops on Lighthouse (custom_php_) GraphQL schemas and vice-versa.
var extraCustomPrefixesForLanguage = map[string][]string{
	"graphql": {"custom_js_"},
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
	prefixes := make([]string, 0, 2)
	if prefix, ok := customPrefixForLanguage[language]; ok {
		prefixes = append(prefixes, prefix)
	}
	prefixes = append(prefixes, extraCustomPrefixesForLanguage[language]...)
	if len(prefixes) == 0 {
		return []Extractor{}
	}

	keySet := make(map[string]bool)
	var keys []string
	for _, k := range extractor.List() {
		for _, prefix := range prefixes {
			if strings.HasPrefix(k, prefix) && len(k) > len(prefix) {
				if !keySet[k] {
					keySet[k] = true
					keys = append(keys, k)
				}
				break
			}
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
// Semantics:
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
// dedup rule: when a custom extractor emits an entity with the same
// Name as a base extractor entity, the custom extractor's version wins.
//
// The merge preserves the original ordering of base entities, replacing any
// base entity in place when a custom entity with the same Name exists. Custom
// entities whose Name does not collide with any base entity are appended after
// the base entities in their dispatch order (already deterministic via
// CustomExtractorsFor's sort).
//
// SUPERSEDE SEMANTICS (issue #4402). A naive "custom wins, base discarded"
// replacement silently drops two kinds of state the base node carried and the
// custom node usually does not:
//
//   - QualifiedName — the module-qualified name that drives byQualifiedName
//     resolution and cross-repo joins. Custom/framework extractors typically
//     emit a bare Name with no QualifiedName; dropping the base one leaves the
//     surviving entity unresolvable by qualified name (root cause behind the
//     #4379 settings→middleware late-binding workaround).
//   - Structural edges — relationships already attached to the base node,
//     notably the class→field CONTAINS membership emitted by the base
//     class-body walk (#526). Dropping these orphans every field (root cause
//     behind the #4366 per-language CONTAINS re-emit workaround).
//
// supersedeBase therefore carries base-only state onto the surviving custom
// entity: it fills the QualifiedName (and other) gaps the custom node left
// empty WITHOUT overriding any value the custom node provided, and UNIONS the
// base node's relationships into the survivor (re-keying base self-edges from
// the base ID to the survivor ID and deduping). This makes the dropped-state
// bug impossible at the merge boundary rather than patching it per-language
// downstream.
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

	// Replace base entities in place when a custom entity shares the Name,
	// carrying base-only state (QualifiedName + structural edges) onto the
	// surviving custom entity (issue #4402).
	for _, be := range baseEntities {
		if idx, ok := customByName[be.Name]; ok && !used[idx] {
			merged = append(merged, supersedeBase(be, customEntities[idx]))
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

// supersedeBase returns the surviving entity for the case where custom node
// `ce` replaces base node `be` (same Name). The custom node's identity and all
// of its non-empty fields WIN; base-only state is carried over to fill gaps:
//
//   - QualifiedName is taken from base when the custom node left it empty.
//   - A conservative set of descriptive/structural string fields (Subtype,
//     Signature, Description, Domain, Content, Language, SourceFile) are filled
//     from base only when the custom node left them empty — never overridden.
//   - StartLine/EndLine are filled from base when the custom node left them 0.
//   - Properties and Metadata maps are gap-filled key-by-key (custom keys win).
//   - Tags from base are unioned in (deduped).
//   - Relationships are UNIONED: base relationships survive on the merged
//     entity, with base self-edges (FromID == base.ID) re-keyed to the survivor
//     ID so class→field CONTAINS membership is not dropped. Duplicates (same
//     from/to/kind) are removed.
//
// The custom node's ID/Kind/Name are NOT changed — supersede preserves the
// custom extractor's chosen identity.
func supersedeBase(be, ce types.EntityRecord) types.EntityRecord {
	survivor := ce

	// Resolve the survivor's effective ID for self-edge re-keying. Custom
	// nodes may not have stamped an ID yet at merge time; fall back to the
	// deterministic ComputeID so re-keying targets a stable value.
	survivorID := survivor.ID
	if survivorID == "" {
		survivorID = survivor.ComputeID()
	}
	baseID := be.ID
	if baseID == "" {
		baseID = be.ComputeID()
	}

	// (1) Preserve QualifiedName — the field that drives byQualifiedName
	// resolution and cross-repo joins (issue #4379 root cause).
	if survivor.QualifiedName == "" {
		survivor.QualifiedName = be.QualifiedName
	}

	// (2) Gap-fill conservative base-only descriptive/structural fields. Only
	// fill when the custom node left the field empty — never override a value
	// the custom extractor deliberately provided.
	if survivor.Subtype == "" {
		survivor.Subtype = be.Subtype
	}
	if survivor.Signature == "" {
		survivor.Signature = be.Signature
	}
	if survivor.Description == "" {
		survivor.Description = be.Description
	}
	if survivor.Domain == "" {
		survivor.Domain = be.Domain
	}
	if survivor.Content == "" {
		survivor.Content = be.Content
	}
	if survivor.Language == "" {
		survivor.Language = be.Language
	}
	if survivor.SourceFile == "" {
		survivor.SourceFile = be.SourceFile
	}
	if survivor.StartLine == 0 {
		survivor.StartLine = be.StartLine
	}
	if survivor.EndLine == 0 {
		survivor.EndLine = be.EndLine
	}

	// (3) Gap-fill Properties / Metadata maps (custom keys win).
	if len(be.Properties) > 0 {
		if survivor.Properties == nil {
			survivor.Properties = make(map[string]string, len(be.Properties))
		}
		for k, v := range be.Properties {
			if _, ok := survivor.Properties[k]; !ok {
				survivor.Properties[k] = v
			}
		}
	}
	if len(be.Metadata) > 0 {
		if survivor.Metadata == nil {
			survivor.Metadata = make(map[string]interface{}, len(be.Metadata))
		}
		for k, v := range be.Metadata {
			if _, ok := survivor.Metadata[k]; !ok {
				survivor.Metadata[k] = v
			}
		}
	}

	// (4) Union Tags (dedupe).
	if len(be.Tags) > 0 {
		seenTag := make(map[string]bool, len(survivor.Tags)+len(be.Tags))
		for _, t := range survivor.Tags {
			seenTag[t] = true
		}
		for _, t := range be.Tags {
			if !seenTag[t] {
				seenTag[t] = true
				survivor.Tags = append(survivor.Tags, t)
			}
		}
	}

	// (5) Union relationships. Base edges must survive on the merged entity —
	// re-key base self-edges (FromID == baseID) to the survivor ID so
	// structural membership (class→field CONTAINS, #526) is preserved, then
	// dedupe against the custom node's own relationships.
	if len(be.Relationships) > 0 {
		type relKey struct{ from, to, kind string }
		seen := make(map[relKey]bool, len(survivor.Relationships)+len(be.Relationships))
		for _, r := range survivor.Relationships {
			seen[relKey{r.FromID, r.ToID, r.Kind}] = true
		}
		for _, r := range be.Relationships {
			br := r
			if br.FromID == baseID {
				br.FromID = survivorID
			}
			if br.ToID == baseID {
				br.ToID = survivorID
			}
			k := relKey{br.FromID, br.ToID, br.Kind}
			if seen[k] {
				continue
			}
			seen[k] = true
			survivor.Relationships = append(survivor.Relationships, br)
		}
	}

	return survivor
}
