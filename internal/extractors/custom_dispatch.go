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

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
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
	// Groovy framework extractors (#4749 Grails/Ratpack test→endpoint route-hit
	// linkage). The base "groovy" tree-sitter extractor key is shorter than this
	// prefix, so the `len(k) > len(prefix)` guard in CustomExtractorsFor excludes
	// it and only the custom_groovy_* framework extractors match.
	"groovy": "custom_groovy_",
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
	"clojure": "custom_clojure_",
	"erlang":  "custom_erlang_",
	"csharp":  "custom_csharp_",
	"cpp":     "custom_cpp_",
	"crystal": "custom_crystal_",
	"fsharp":  "custom_fsharp_",
	// Nim framework extractors (#4749 Jester/Prologue test→endpoint route-hit
	// linkage). The base "nim" extractor key is shorter than this prefix, so the
	// `len(k) > len(prefix)` guard in CustomExtractorsFor excludes it and only
	// the custom_nim_* framework extractors match.
	"nim": "custom_nim_",
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
	// lua's PRIMARY prefix is the bare `lua_` namespace used by its framework
	// extractors (lua_routing, lua_middleware, lua_kong, …). The coverage-linkage
	// tail extractor (#4749) however registered under the canonical
	// `custom_lua_tests_route_e2e` key — matching every other language's
	// `custom_<lang>_` convention — which does NOT share the `lua_` prefix
	// (`custom_lua_…` starts with "custom", not "lua"). Without this extra
	// prefix CustomExtractorsFor("lua") silently never returned the e2e
	// extractor in production (only its direct-registration unit test passed).
	// Dispatch both namespaces so the legacy lua_* framework extractors AND the
	// custom_lua_* tail extractor are picked up.
	"lua": {"custom_lua_"},
	// dart's PRIMARY prefix in customPrefixForLanguage is ALREADY `custom_dart_`,
	// so the #4758 coverage-linkage tail extractor `custom_dart_tests_route_e2e`
	// is selected by CustomExtractorsFor("dart") via that primary entry. This
	// explicit re-listing is the #4769 belt-and-suspenders guard: it documents
	// and locks the dispatch prefix so a future refactor of dart's primary entry
	// (the Lua mismatch class of bug, where the framework prefix and the
	// canonical `custom_<lang>_` tail key diverge) cannot silently drop the tail
	// extractor. The keySet dedup in CustomExtractorsFor makes the duplicate a
	// no-op when the primary already matches.
	"dart": {"custom_dart_"},
	// javascript/typescript's PRIMARY prefix in customPrefixForLanguage is
	// ALREADY `custom_js_`, so the #4399 browser-e2e coverage-linkage tail
	// extractor `custom_js_tests_route_e2e` is selected by
	// CustomExtractorsFor("javascript"/"typescript") via that primary entry.
	// This explicit re-listing is the #4769 belt-and-suspenders guard: it
	// documents and locks the dispatch prefix so a future refactor of the JS
	// primary entry (the Lua mismatch class of bug, where the framework prefix
	// and the canonical `custom_<lang>_` tail key diverge) cannot silently drop
	// the tail extractor. The keySet dedup in CustomExtractorsFor makes the
	// duplicate a no-op when the primary already matches.
	"javascript": {"custom_js_"},
	"typescript": {"custom_js_"},
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

// ---------------------------------------------------------------------------
// MERGE POLICY (issue #6104)
// ---------------------------------------------------------------------------
//
// The old rule was "a custom entity with the same NAME as a base entity
// replaces it". Name is not an identity. The graph's identity function is
// EntityRecord.ComputeID = sha256(OrgID+ProjectID+SourceFile+Kind+Name), so
// keying the merge on Name alone superseded on strictly LESS information than
// the identity being superseded. On a 361-file fixture that destroyed 280
// entities across 5 kinds and 2 languages, in three distinct shapes:
//
//   1. cross-kind      Task|send_receipt replaced by SCOPE.Service|send_receipt
//   2. same-kind       a second Task|send_receipt replacing the first
//   3. in-place        SCOPE.Operation|C.list|method rewritten to |endpoint
//                      with EndLine truncated 12 -> 9, propagating into the
//                      derived SCOPE.Process and http_endpoint_definition
//
// Keying on (Kind, Name) closes only shape 1. The defect is the POLICY, not
// the key. The policy is now:
//
//	Tier A — SAME IDENTITY (SourceFile, Kind, Name).
//	    These two records ARE the same graph node; the ID-keyed store would
//	    collapse them anyway. They are COMBINED into one survivor. Custom
//	    values win where they disagree (the framework extractor is the more
//	    specific observer), base fills every gap, and:
//	      * the SPAN IS UNIONED — StartLine takes the minimum of the non-zero
//	        values, EndLine the maximum. A merge never narrows a span. This is
//	        what closes shape 3: EndLine 12 survives the "endpoint" rewrite.
//	      * a displaced non-empty base Subtype is retained under
//	        BaseSubtypeProperty. "This method is really an endpoint" is real
//	        information and the custom value wins, but the value it displaced
//	        is preserved rather than discarded — enrichment, not replacement.
//
//	Tier B — NAME COLLISION, DIFFERENT IDENTITY (different Kind, or different
//	    SourceFile). These are DIFFERENT graph nodes. BOTH SURVIVE. This is
//	    what closes shapes 1 and 2. The base node is left untouched; the
//	    custom node is ENRICHED from its base twin (same file, same name) with
//	    the base-only state #4402 was created to preserve — QualifiedName
//	    (#4379), span, descriptive fields, and a re-keyed COPY of the base
//	    node's structural edges (class->field CONTAINS, #526/#4366). #4402
//	    obtained that state by destroying the base node; enrichment obtains
//	    the same state with the base node still standing.
//
//	Tier C — no collision. Appended in dispatch order, unchanged.
//
// EDGE RE-KEYING. When a Tier A combine retires an ID (the base and custom
// records carried different pre-stamped IDs), EVERY relationship visible to
// the merge is re-keyed off the retired ID — not just the survivor's own
// self-edges, which is all #4402 did, leaving edges emitted by other passes
// dangling. MergeWithCustomRels additionally accepts and re-keys a standalone
// relationship set for callers that hold one.
//
// The survivor prefers the BASE record's ID when it has one: base and custom
// agree on every identity field in Tier A, and the base ID is the one the rest
// of the base pass's edges already point at.
//
// Cross-kind dedup is still handled downstream by run-extractor's
// deduplicateEntities pass, which runs after this merge.

// BaseSubtypeProperty is the Properties key under which a base-path Subtype
// displaced by a more specific custom-extractor Subtype is retained, so the
// displaced value is preserved rather than lost (#6104).
const BaseSubtypeProperty = types.EntityBaseSubtypeProperty

// TwinOfProperty marks a Tier B facet with the ID of the base entity it is a
// facet of, so the resolver can treat it as an alias rather than as a second
// competing definition of the same name (#6104).
const TwinOfProperty = types.EntityTwinOfProperty

// identityKey is the graph's own entity identity, minus the org/project scope
// (constant within one merge).
type identityKey struct{ file, kind, name string }

// twinKey identifies "the same source construct, modelled under a different
// Kind" — used for Tier B enrichment only, never for supersede.
type twinKey struct{ file, name string }

// MergeWithCustom merges baseEntities with customEntities under the merge
// policy documented above. It is MergeWithCustomRels with no standalone
// relationship set.
func MergeWithCustom(baseEntities, customEntities []types.EntityRecord) []types.EntityRecord {
	ents, _ := MergeWithCustomRels(baseEntities, customEntities, nil)
	return ents
}

// MergeWithCustomRels merges baseEntities with customEntities and re-keys
// `rels` against any ID retired by a Tier A combine.
//
// The signature exists because the #6104 re-keying requirement cannot be met
// without the merge seeing the relationship set: #4402 could only re-key the
// superseded node's own self-edges, so edges produced by other passes were
// left pointing at an ID that no longer existed.
func MergeWithCustomRels(baseEntities, customEntities []types.EntityRecord, rels []types.RelationshipRecord) ([]types.EntityRecord, []types.RelationshipRecord) {
	if len(customEntities) == 0 {
		return baseEntities, rels
	}

	// Work on a copy: Tier B enriches custom records in place and the caller's
	// slice must not be mutated.
	custom := make([]types.EntityRecord, len(customEntities))
	copy(custom, customEntities)

	byIdentity := make(map[identityKey][]int, len(custom))
	byTwin := make(map[twinKey][]int, len(custom))
	for i, ce := range custom {
		ik := identityKey{ce.SourceFile, ce.Kind, ce.Name}
		tk := twinKey{ce.SourceFile, ce.Name}
		byIdentity[ik] = append(byIdentity[ik], i)
		byTwin[tk] = append(byTwin[tk], i)
	}

	combined := make([]bool, len(custom))
	enriched := make([]bool, len(custom))
	retired := make(map[string]string) // retired ID -> survivor ID
	merged := make([]types.EntityRecord, 0, len(baseEntities)+len(custom))

	for _, be := range baseEntities {
		// --- Tier A: same identity -> combine into one survivor -------------
		//
		// A custom record with an empty SourceFile is matched against the base
		// record's file: dispatch is per-file, so "no file" means "this file".
		idxs := pickUnused(byIdentity[identityKey{be.SourceFile, be.Kind, be.Name}], combined)
		if be.SourceFile != "" {
			idxs = append(idxs, pickUnused(byIdentity[identityKey{"", be.Kind, be.Name}], combined)...)
		}
		if len(idxs) > 0 {
			survivor := be
			for _, i := range idxs {
				survivor = combineSameIdentity(survivor, custom[i])
				combined[i] = true
			}
			survivorID := effectiveID(&survivor)
			if id := effectiveID(&be); id != "" && id != survivorID {
				retired[id] = survivorID
			}
			for _, i := range idxs {
				if id := effectiveID(&custom[i]); id != "" && id != survivorID {
					retired[id] = survivorID
				}
			}
			merged = append(merged, survivor)
			continue
		}

		// --- Tier B: name twin of a different Kind -> BOTH survive ----------
		twins := byTwin[twinKey{be.SourceFile, be.Name}]
		if be.SourceFile != "" {
			twins = append(twins, byTwin[twinKey{"", be.Name}]...)
		}
		for _, i := range twins {
			if combined[i] || enriched[i] {
				continue
			}
			custom[i] = enrichFromTwin(custom[i], be)
			enriched[i] = true
		}

		merged = append(merged, be)
	}

	// --- Tier C: custom entities that collided with nothing ----------------
	for i := range custom {
		if combined[i] {
			continue
		}
		merged = append(merged, custom[i])
	}

	if len(retired) > 0 {
		for i := range merged {
			rekeyRelationships(merged[i].Relationships, retired)
		}
		rekeyRelationships(rels, retired)
	}
	return merged, rels
}

// pickUnused returns the indices in idxs not already consumed by a combine.
func pickUnused(idxs []int, combined []bool) []int {
	var out []int
	for _, i := range idxs {
		if !combined[i] {
			out = append(out, i)
		}
	}
	return out
}

// effectiveID is the entity's stamped ID, or the deterministic computed one
// when the pipeline has not stamped it yet.
func effectiveID(e *types.EntityRecord) string {
	if e.ID != "" {
		return e.ID
	}
	return e.ComputeID()
}

// rekeyRelationships rewrites every endpoint naming a retired ID, in place.
func rekeyRelationships(rels []types.RelationshipRecord, retired map[string]string) {
	for i := range rels {
		if to, ok := retired[rels[i].FromID]; ok {
			rels[i].FromID = to
		}
		if to, ok := retired[rels[i].ToID]; ok {
			rels[i].ToID = to
		}
	}
}

// combineSameIdentity returns the single survivor for two records that share
// the graph's identity (SourceFile, Kind, Name) and are therefore the same
// node. See the merge policy block above.
func combineSameIdentity(be, ce types.EntityRecord) types.EntityRecord {
	survivor := supersedeBase(be, ce)

	// The survivor carries the base ID when the base has one: the identity
	// fields are equal, and the base ID is what the rest of the base pass's
	// edges already point at.
	if be.ID != "" {
		survivor.ID = be.ID
	}

	// (I2) NEVER NARROW A SPAN. 0 means "position unknown", not "line 0", so
	// StartLine takes the minimum of the NON-ZERO values while EndLine takes
	// the plain maximum.
	survivor.StartLine = minNonZero(be.StartLine, ce.StartLine)
	if be.EndLine > survivor.EndLine {
		survivor.EndLine = be.EndLine
	}

	// A displaced base Subtype is retained rather than discarded.
	if be.Subtype != "" && survivor.Subtype != be.Subtype {
		if survivor.Properties == nil {
			survivor.Properties = make(map[string]string, 1)
		}
		if _, exists := survivor.Properties[BaseSubtypeProperty]; !exists {
			survivor.Properties[BaseSubtypeProperty] = be.Subtype
		}
	}

	// Identity scope: a custom extractor never sees the org/project stamps.
	if survivor.OrgID == "" {
		survivor.OrgID = be.OrgID
	}
	if survivor.ProjectID == "" {
		survivor.ProjectID = be.ProjectID
	}
	if survivor.ProjectSlug == "" {
		survivor.ProjectSlug = be.ProjectSlug
	}
	if survivor.RepoID == "" {
		survivor.RepoID = be.RepoID
	}
	return survivor
}

// enrichFromTwin returns the custom entity `ce` enriched from its base twin
// `be` — a record for the same source construct under a DIFFERENT Kind. The
// base entity is NOT consumed: the caller keeps it in the merged output.
//
// This is the non-destructive replacement for #4402's supersede. It carries
// the same base-only state onto the custom node (QualifiedName, span,
// descriptive fields, structural edges) without deleting the node it came
// from.
func enrichFromTwin(ce, be types.EntityRecord) types.EntityRecord {
	out := ce

	// Mark the facet. Both nodes are in the graph, but only the base node is a
	// competing DEFINITION of this name — the facet is an alias of it, and the
	// symbol index must not read the pair as an ambiguity.
	if out.Properties == nil {
		out.Properties = make(map[string]string, 1)
	}
	if _, exists := out.Properties[TwinOfProperty]; !exists {
		out.Properties[TwinOfProperty] = effectiveID(&be)
	}

	if out.QualifiedName == "" {
		out.QualifiedName = be.QualifiedName
	}
	if out.Signature == "" {
		out.Signature = be.Signature
	}
	if out.Description == "" {
		out.Description = be.Description
	}
	if out.Domain == "" {
		out.Domain = be.Domain
	}
	if out.Content == "" {
		out.Content = be.Content
	}
	if out.Language == "" {
		out.Language = be.Language
	}
	if out.SourceFile == "" {
		out.SourceFile = be.SourceFile
	}
	if out.OrgID == "" {
		out.OrgID = be.OrgID
	}
	if out.ProjectID == "" {
		out.ProjectID = be.ProjectID
	}
	if out.ProjectSlug == "" {
		out.ProjectSlug = be.ProjectSlug
	}
	if out.RepoID == "" {
		out.RepoID = be.RepoID
	}
	// (I2) the twin describes the same construct, so widen rather than clip.
	out.StartLine = minNonZero(out.StartLine, be.StartLine)
	if be.EndLine > out.EndLine {
		out.EndLine = be.EndLine
	}

	if len(be.Properties) > 0 {
		if out.Properties == nil {
			out.Properties = make(map[string]string, len(be.Properties))
		}
		for k, v := range be.Properties {
			if _, ok := out.Properties[k]; !ok {
				out.Properties[k] = v
			}
		}
	}
	if len(be.Metadata) > 0 {
		if out.Metadata == nil {
			out.Metadata = make(map[string]interface{}, len(be.Metadata))
		}
		for k, v := range be.Metadata {
			if _, ok := out.Metadata[k]; !ok {
				out.Metadata[k] = v
			}
		}
	}

	// COPY (never move) the base node's owned structural edges, re-keyed onto
	// the custom node. An edge whose FromID names some third entity belongs to
	// that entity and is left alone.
	if len(be.Relationships) > 0 {
		baseID := effectiveID(&be)
		outID := effectiveID(&out)
		type relKey struct{ from, to, kind string }
		seen := make(map[relKey]bool, len(out.Relationships)+len(be.Relationships))
		for _, r := range out.Relationships {
			seen[relKey{r.FromID, r.ToID, r.Kind}] = true
		}
		for _, r := range be.Relationships {
			if r.FromID != "" && r.FromID != baseID {
				continue
			}
			br := r
			// An empty FromID means "implicitly owned by whichever node
			// carries this edge" — that is now the custom node, so leave it
			// empty rather than materialising an ID that would defeat the
			// dedup against the custom node's own implicit edges.
			if br.FromID == baseID {
				br.FromID = outID
			}
			if br.ToID == baseID {
				br.ToID = outID
			}
			k := relKey{br.FromID, br.ToID, br.Kind}
			if seen[k] {
				continue
			}
			seen[k] = true
			out.Relationships = append(out.Relationships, br)
		}
	}
	return out
}

// minNonZero returns the smaller of two line numbers, treating 0 as "unknown"
// rather than as line zero.
func minNonZero(a, b int) int {
	switch {
	case a == 0:
		return b
	case b == 0:
		return a
	case a < b:
		return a
	default:
		return b
	}
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
// supersedeBase is the Tier A field-level carry-over, called only by
// combineSameIdentity — where base and custom already agree on the full
// identity (SourceFile, Kind, Name), so no node is being displaced. Span and
// ID handling are finished by combineSameIdentity, which enforces the
// never-narrow invariant on top of the gap-fills below.
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
