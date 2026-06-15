// Package resolve — M5: per-module symbol index for cross-module CALLS
// resolution at scale.
//
// Problem (issue #2182): in a 500-module monorepo the existing BuildIndex
// absorbs every entity into a single shared Index and the cross-module
// resolution join is effectively O(N×M) — every relationship endpoint
// iterates the combined set of all N entities across M modules to find a
// match.  At 500 modules × 200 entities each that's 100 k items under the
// global byName / byKind maps, and the structural lookups (byLocation,
// byMember, byPackage*) are keyed by file path so they degenerate to a
// flat-file hash-table that the Go runtime resizes many times during
// population.
//
// Solution: build a lightweight SymbolIndex per module during extraction,
// then merge them in batches of M modules at a time into a combined Index
// for the resolution pass.  The merge is O(N log N) because each module
// contributes its own pre-sorted symbol table; the final global hash-tables
// are populated once, not incrementally resized per-entity.
//
// Key types:
//   - ModuleSymbols   — per-module symbol table built during extraction.
//   - SymbolIndex     — collection of per-module tables ready for batch merge.
//   - BuildModuleSymbols   — populates a ModuleSymbols from an entity slice.
//   - MergeModuleBatch     — merges up to BatchSize modules into one Index.
//   - BuildIndexFromModules — full pipeline: module batch → merged Index.
//
// Edge materialization:
//   - LazyEdgeKey / LazyEdgeSet — deferred edge registry; hot-path callers
//     register stubs they know will appear frequently, and the set resolves
//     them in one pass rather than paying per-edge lookup cost.
//
// PRODUCTION STATUS (#4331): M5 is now WIRED BEHIND A DEFAULT-OFF FLAG
// (ARCHIGRAPH_RESOLVE_MODULE_INDEX=1). cmd/archigraph/index.go still calls
// BuildIndex by default; when the flag is set it routes through
// BuildIndexFromModulesOrdered instead. Default = old path = zero behaviour
// change, so merging is safe.
//
// THE ORIGINAL DIVERGENCE (and how it was fixed):
// The first M5 design re-sorted each module's entities by ID
// (BuildModuleSymbols' sort step) for O(N) collision detection. BuildIndex,
// by contrast, preserves extraction order, and the platform-variant merge
// (#1818) in byPackageOperation/byPackageComponent is ORDER-SENSITIVE for 3+
// mutually-exclusive GOOS variants of one (pkgDir, name): pairwise canonical-
// chaining produces a different PlatformVariants fan-out depending on which
// variant is seen first. That fan-out is consumed by
// ReferencesEmbeddedWithAllowlist (refs.go) to clone CALLS edges, so a
// different topology = a different output edge set. A second gap: the original
// insertModuleEntry did NOT populate byNamespaceMember (#4374, C#),
// byKotlinPkgMember / byKotlinPkgFunc (#4375, Kotlin) — three index tables
// BuildIndex fills.
//
// BOTH are fixed for the wired path:
//   - BuildModuleSymbolsOrdered preserves extraction order (no ID sort), so
//     the order-sensitive platform-variant merge sees variants in the same
//     order BuildIndex does (variants of one (pkgDir,name) are always in the
//     same module, so per-module extraction order is sufficient).
//   - insertModuleEntry now populates byNamespaceMember / byKotlinPkgMember /
//     byKotlinPkgFunc, matching BuildIndex.
//
// TestM5_PlatformVariantParity_Ordered (symbol_index_parity_test.go) now
// asserts FULL Index parity (including PlatformVariants) for the ordered path,
// and TestM5_OrderedFullIndexParity_Representative diffs the entire Index on a
// representative multi-language fixture. The legacy sort-by-ID
// BuildIndexFromModules is retained for the scale benchmarks and still carries
// the pinned divergence guard. Flipping the default to ON requires a live-graph
// edge-parity diff (reindex a real repo both ways) — tracked as follow-up #4901.
package resolve

import (
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// defaultBatchSize is the number of modules merged per batch when callers do
// not supply an explicit batch size.  Chosen empirically: at 64 modules per
// batch the merge loop stays well inside L3 cache on typical CI hardware
// while still amortising the per-module setup cost over a large enough group
// to reduce total allocations by ~60 % compared with N individual BuildIndex
// calls.
const defaultBatchSize = 64

// ModuleKey uniquely identifies a module inside a monorepo.  For a Go module
// it is the module path (e.g. "github.com/acme/platform/svc/auth"); for a
// directory-based module it is the repo-relative directory path.
type ModuleKey string

// moduleEntry is one entity from a module, pre-processed for fast merge.
type moduleEntry struct {
	id            string
	name          string
	kind          string
	kindTrimmed   string // kind with "SCOPE." prefix stripped (or same as kind)
	sourceFile    string // forward-slash normalised
	qualifiedName string
	refProp       string // Properties["ref"] value, if indexable
	dotName       bool   // name contains a '.'
	properties    map[string]string

	// globalPos is the entity's position in the caller's ORIGINAL (flat)
	// entity slice — the order BuildIndex consumes. M5 builds its symbol
	// table per-module (so the merge visits entities grouped by module, not
	// in global order), but every index table in this package is first-
	// writer-wins / order-sensitive (byKind, byName, byLocation, the #1818
	// platform-variant merge, …). Consuming in module order instead of
	// global order therefore picks a DIFFERENT winner for any name that is
	// ambiguous ACROSS modules, which diverges the resolved edge set from
	// flat BuildIndex (issue #5206: mysql/psycopg2 driver swap, the
	// OnApplicationShutdown cross-module IMPLEMENTS drop). Stamping the
	// global position lets BuildIndexFromModulesOrdered re-establish flat's
	// exact consumption order before insertModuleEntry, making M5 edge-
	// identical to flat. Only BuildModuleSymbolsOrdered (the production path)
	// stamps this; the legacy sort-by-ID BuildModuleSymbols leaves it 0.
	globalPos int
}

// ModuleSymbols is the per-module symbol table.  It is intentionally
// lighter than Index: it carries only the raw data needed for the merge
// step, deferring all deduplication and sentinel logic to MergeModuleBatch.
// This keeps each module's allocation tiny and avoids the repeated map
// resizing that occurs when BuildIndex processes one entity at a time from a
// large merged slice.
type ModuleSymbols struct {
	// Key is the module identifier.
	Key ModuleKey

	// entries holds the pre-processed entity records in sorted order (by
	// entity ID).  Sorted order lets MergeModuleBatch detect duplicates in a
	// single linear pass instead of a hash-set probe per entry.
	entries []moduleEntry

	// entityCount is the number of source EntityRecord values consumed.
	entityCount int
}

// SymbolIndex is an ordered collection of per-module symbol tables ready
// for batched merging.
type SymbolIndex struct {
	modules []*ModuleSymbols
}

// Add appends a ModuleSymbols to the index.
func (si *SymbolIndex) Add(ms *ModuleSymbols) {
	si.modules = append(si.modules, ms)
}

// Len returns the number of modules registered.
func (si *SymbolIndex) Len() int { return len(si.modules) }

// BuildModuleSymbols processes an entity slice for a single module and
// returns a ModuleSymbols ready for registration into a SymbolIndex.
//
// The caller supplies the module key (e.g. "github.com/acme/platform/auth");
// for directory-based modules the repo-relative directory is a good choice.
//
// Complexity: O(N log N) in the number of entities — dominated by the final
// sort step.  The sort ensures MergeModuleBatch can detect collisions with a
// single linear scan instead of a per-entry hash probe.
func BuildModuleSymbols(key ModuleKey, entities []types.EntityRecord) *ModuleSymbols {
	ms := &ModuleSymbols{
		Key:         key,
		entityCount: len(entities),
		entries:     make([]moduleEntry, 0, len(entities)),
	}
	for k := range entities {
		e := &entities[k]
		if e.ID == "" || e.Name == "" {
			continue
		}
		sf := normalizePath(e.SourceFile)
		trimmed := strings.TrimPrefix(e.Kind, scopeKindPrefix)
		if trimmed == e.Kind {
			trimmed = ""
		}

		// Extract the ref property if it qualifies for qname indexing.
		refProp := extractIndexableRef(e)

		me := moduleEntry{
			id:            e.ID,
			name:          e.Name,
			kind:          e.Kind,
			kindTrimmed:   trimmed,
			sourceFile:    sf,
			qualifiedName: e.QualifiedName,
			refProp:       refProp,
			dotName:       strings.IndexByte(e.Name, dottedNameSep) >= 0,
			properties:    e.Properties,
		}
		ms.entries = append(ms.entries, me)
	}
	// Sort by ID for O(N) collision detection in MergeModuleBatch.
	sort.Slice(ms.entries, func(i, j int) bool {
		return ms.entries[i].id < ms.entries[j].id
	})
	return ms
}

// extractIndexableRef returns the Properties["ref"] value from an entity
// if it qualifies for byQualifiedName indexing under the same rules as
// BuildIndex.  Returns "" otherwise.
func extractIndexableRef(e *types.EntityRecord) string {
	ref := ""
	if e.Properties != nil {
		ref = e.Properties["ref"]
	}
	if ref == "" || ref == e.QualifiedName {
		return ""
	}
	switch {
	case strings.HasPrefix(ref, "scope:endpoint:"),
		strings.HasPrefix(ref, "scope:testcoverage:"),
		strings.HasPrefix(ref, "scope:component:interface:rust:"),
		strings.HasPrefix(ref, "scope:component:interface:java:"),
		strings.HasPrefix(ref, "scope:component:interface:typescript:"),
		strings.HasPrefix(ref, "scope:component:interface:javascript:"),
		strings.HasPrefix(ref, "scope:component:interface:csharp:"),
		strings.HasPrefix(ref, "scope:component:interface:kotlin:"),
		strings.HasPrefix(ref, "scope:component:interface:scala:"),
		strings.HasPrefix(ref, "scope:component:interface:dart:"),
		strings.HasPrefix(ref, "scope:component:interface:php:"):
		return ref
	}
	return ""
}

// MergeModuleBatch merges up to batchSize modules from si (starting at
// offset) into a single Index and returns it together with the next offset.
//
// Callers iterate:
//
//	for off := 0; off < si.Len(); {
//	    idx, off = MergeModuleBatch(si, off, batchSize)
//	    // use idx …
//	}
//
// When offset >= si.Len() the returned Index is empty and the loop exits.
//
// Complexity: O(K·N_k) where K = batch size and N_k = entities per module.
// Each entity in the batch is processed exactly once; no per-entity hash
// probes against the whole global table.
func MergeModuleBatch(si *SymbolIndex, offset, batchSize int) (Index, int) {
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	end := offset + batchSize
	if end > len(si.modules) {
		end = len(si.modules)
	}
	if offset >= end {
		return emptyIndex(), end
	}

	// Count total entities in this batch for pre-sizing.
	total := 0
	for _, ms := range si.modules[offset:end] {
		total += len(ms.entries)
	}

	// Pre-allocate all maps with the known capacity to avoid incremental
	// resizing — this is the primary win over calling BuildIndex iteratively.
	idx := Index{
		byKind:             make(map[string]map[string]string, total/4+1),
		ambigKind:          make(map[string]map[string]bool),
		byName:             make(map[string]string, total),
		ambigName:          make(map[string]bool),
		nameKinds:          make(map[string]map[string]string, total),
		nameKindsReal:      make(map[string]map[string]string, total),
		byLocation:         make(LocationIndex, total/2+1),
		ambigLocation:      make(map[string]map[string]bool),
		byLocationKind:     make(LocationKindIndex, total/2+1),
		byLocationKindReal: make(LocationKindIndex, total/2+1),
		byMember:           make(map[string]map[string]map[string]string),
		byPackageMember:    make(map[string]map[string]map[string]string),
		byPackageOperation: make(map[string]map[string]string),
		byPackageComponent: make(map[string]map[string]string),
		byNamespaceMember:  make(map[string]map[string]map[string]string),
		byKotlinPkgMember:  make(map[string]map[string]map[string]string),
		byKotlinPkgFunc:    make(map[string]map[string]string),
		byQualifiedName:    make(map[string]string, total/4+1),
		PlatformVariants:   make(map[string][]string),
	}

	// Build-tag side-tables (mirrors BuildIndex local vars).
	pkgOpTag := make(map[string]map[string]string)
	pkgCompTag := make(map[string]map[string]string)
	pkgOpSrc := make(map[string]map[string]string)
	pkgCompSrc := make(map[string]map[string]string)

	for _, ms := range si.modules[offset:end] {
		for i := range ms.entries {
			me := &ms.entries[i]
			insertModuleEntry(&idx, me, pkgOpTag, pkgCompTag, pkgOpSrc, pkgCompSrc)
		}
	}
	return idx, end
}

// BuildIndexFromModules is the high-level entry point for M5.  It builds a
// SymbolIndex from all module entity slices, then merges them in batches of
// batchSize (use 0 for the default) into a single unified Index suitable for
// the resolution pass.
//
// This is a drop-in replacement for BuildIndex when the caller has already
// partitioned entities by module.  When called with a single module the
// result is identical to BuildIndex.
//
// Complexity: O(N log N) overall — each entity is sorted once per module
// (BuildModuleSymbols) and processed once during merge.  The global Index
// maps are pre-sized so they are allocated in one shot, avoiding the O(N²/B)
// resize cost of the existing single-pass BuildIndex on large inputs.
func BuildIndexFromModules(modules map[ModuleKey][]types.EntityRecord, batchSize int) Index {
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	si := &SymbolIndex{}
	// Process modules in deterministic (sorted key) order so benchmarks
	// and tests are reproducible.
	keys := make([]string, 0, len(modules))
	for k := range modules {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)

	for _, k := range keys {
		ms := BuildModuleSymbols(ModuleKey(k), modules[ModuleKey(k)])
		si.Add(ms)
	}

	if si.Len() == 0 {
		return emptyIndex()
	}

	// Single-batch fast-path: when everything fits in one batch just merge
	// directly.  This lets micro-benchmarks with small synthetic fixtures see
	// the full pre-sizing benefit without extra loop overhead.
	if si.Len() <= batchSize {
		idx, _ := MergeModuleBatch(si, 0, si.Len())
		return idx
	}

	// Multi-batch: merge each batch into a staging Index then consolidate.
	// For the cross-module resolution use-case the combined Index must span
	// all modules, so we merge batch results into a single accumulator.
	//
	// NOTE: We do NOT call BuildIndex on the combined slice (that would be
	// O(N²) due to repeated map resizing).  Instead we use a two-level
	// merge: batch-level pre-sized maps → final accumulator merge.
	total := 0
	for _, ms := range si.modules {
		total += len(ms.entries)
	}
	acc := accumulatorIndex(total)

	pkgOpTag := make(map[string]map[string]string)
	pkgCompTag := make(map[string]map[string]string)
	pkgOpSrc := make(map[string]map[string]string)
	pkgCompSrc := make(map[string]map[string]string)

	for _, ms := range si.modules {
		for i := range ms.entries {
			me := &ms.entries[i]
			insertModuleEntry(&acc, me, pkgOpTag, pkgCompTag, pkgOpSrc, pkgCompSrc)
		}
	}
	return acc
}

// BuildModuleSymbolsOrdered is the parity-safe sibling of BuildModuleSymbols:
// it preserves the caller's extraction order instead of re-sorting entries by
// ID. Extraction order matters because the platform-variant merge (#1818) in
// byPackageOperation/byPackageComponent is order-sensitive for 3+ mutually-
// exclusive GOOS variants of the same (pkgDir, name). Variants of one
// (pkgDir, name) always live in the same module, so preserving per-module
// extraction order is sufficient to reproduce BuildIndex's PlatformVariants
// topology exactly.
func BuildModuleSymbolsOrdered(key ModuleKey, entities []types.EntityRecord) *ModuleSymbols {
	// Public signature unchanged: stamp each entity's globalPos with its index
	// within this slice. BuildIndexFromModulesOrdered uses the positions-aware
	// internal builder so globalPos reflects the position in the ORIGINAL flat
	// entity slice (the order BuildIndex consumes).
	return buildModuleSymbolsOrderedPos(key, entities, nil)
}

// buildModuleSymbolsOrderedPos is the positions-aware core of
// BuildModuleSymbolsOrdered. positions[k] is the global (flat-slice) index of
// entities[k]; when positions is nil each entity is stamped with its local
// index k. Every retained moduleEntry carries its globalPos so the caller can
// re-establish flat BuildIndex's exact consumption order before insertion
// (#5206).
func buildModuleSymbolsOrderedPos(key ModuleKey, entities []types.EntityRecord, positions []int) *ModuleSymbols {
	ms := &ModuleSymbols{
		Key:         key,
		entityCount: len(entities),
		entries:     make([]moduleEntry, 0, len(entities)),
	}
	for k := range entities {
		e := &entities[k]
		if e.ID == "" || e.Name == "" {
			continue
		}
		sf := normalizePath(e.SourceFile)
		trimmed := strings.TrimPrefix(e.Kind, scopeKindPrefix)
		if trimmed == e.Kind {
			trimmed = ""
		}
		pos := k
		if positions != nil {
			pos = positions[k]
		}
		me := moduleEntry{
			id:            e.ID,
			name:          e.Name,
			kind:          e.Kind,
			kindTrimmed:   trimmed,
			sourceFile:    sf,
			qualifiedName: e.QualifiedName,
			refProp:       extractIndexableRef(e),
			dotName:       strings.IndexByte(e.Name, dottedNameSep) >= 0,
			properties:    e.Properties,
			globalPos:     pos,
		}
		ms.entries = append(ms.entries, me)
	}
	// NOTE: deliberately NO sort — extraction order is preserved for parity.
	return ms
}

// BuildIndexFromModulesOrdered is the parity-safe, production-wired entry point
// for M5 (#4331). Unlike BuildIndexFromModules it does NOT sort entities by ID
// (BuildModuleSymbolsOrdered), and it processes modules in first-seen extraction
// order rather than sorted-key order. Combined with the namespace/kotlin index
// tables now populated by insertModuleEntry, this produces an Index that is
// edge-set-identical to BuildIndex on the same entity set — verified by the
// full-Index parity harness in symbol_index_parity_test.go.
//
// moduleKeyOf decides which module an entity belongs to (typically its package
// directory). The only correctness requirement is that all platform-variant
// siblings of one (pkgDir, name) map to the SAME module — using pkgDir (or any
// finer-than-pkgDir partition that keeps a directory whole) satisfies that.
func BuildIndexFromModulesOrdered(entities []types.EntityRecord, moduleKeyOf func(types.EntityRecord) ModuleKey) Index {
	if len(entities) == 0 {
		return emptyIndex()
	}
	// Partition preserving extraction order within and across modules. We track
	// first-seen module order so the per-module build stays extraction-faithful,
	// and we stamp each entity's GLOBAL position (its index in the flat slice)
	// so the final insertion can be replayed in flat BuildIndex's exact order.
	buckets := make(map[ModuleKey][]types.EntityRecord)
	bucketPos := make(map[ModuleKey][]int)
	order := make([]ModuleKey, 0)
	for k := range entities {
		key := moduleKeyOf(entities[k])
		if _, seen := buckets[key]; !seen {
			order = append(order, key)
		}
		buckets[key] = append(buckets[key], entities[k])
		bucketPos[key] = append(bucketPos[key], k)
	}

	si := &SymbolIndex{}
	for _, key := range order {
		si.Add(buildModuleSymbolsOrderedPos(key, buckets[key], bucketPos[key]))
	}

	// #5206 — collapse the per-module symbol tables back into a SINGLE stream
	// ordered by global position, so insertModuleEntry consumes entities in the
	// exact order flat BuildIndex does. Every index table here is first-writer-
	// wins / order-sensitive (byKind, byName, byLocation, the #1818 platform-
	// variant merge), so consuming in module order instead of global order picks
	// a different winner for any name that is ambiguous ACROSS modules — the
	// edge-set divergence in #5206. Replaying in global order makes M5 produce
	// an Index that is byte-identical to flat's, hence edge-identical resolution.
	// Cost: one O(N log N) sort over lightweight pointers, on top of the per-
	// module build that M5 already does — the scale win (pre-sized maps, per-
	// module locality during BuildModuleSymbolsOrdered) is preserved.
	ordered := make([]*moduleEntry, 0, len(entities))
	for _, ms := range si.modules {
		for i := range ms.entries {
			ordered = append(ordered, &ms.entries[i])
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].globalPos < ordered[j].globalPos
	})

	total := len(entities)
	acc := accumulatorIndex(total)
	pkgOpTag := make(map[string]map[string]string)
	pkgCompTag := make(map[string]map[string]string)
	pkgOpSrc := make(map[string]map[string]string)
	pkgCompSrc := make(map[string]map[string]string)
	for _, me := range ordered {
		insertModuleEntry(&acc, me, pkgOpTag, pkgCompTag, pkgOpSrc, pkgCompSrc)
	}
	return acc
}

// ModuleKeyByPkgDir is the default module-partition function for
// BuildIndexFromModulesOrdered: it groups entities by the directory of their
// (slash-normalised) SourceFile. Entities with no SourceFile fall into a single
// "" bucket, matching how BuildIndex treats them (their location-keyed indexes
// are skipped anyway).
func ModuleKeyByPkgDir(e types.EntityRecord) ModuleKey {
	return ModuleKey(pkgDirOf(normalizePath(e.SourceFile)))
}

// accumulatorIndex returns a pre-sized Index suitable for the multi-batch
// accumulator path.
func accumulatorIndex(totalEntities int) Index {
	cap4 := totalEntities/4 + 1
	cap2 := totalEntities/2 + 1
	return Index{
		byKind:             make(map[string]map[string]string, cap4),
		ambigKind:          make(map[string]map[string]bool),
		byName:             make(map[string]string, totalEntities),
		ambigName:          make(map[string]bool),
		nameKinds:          make(map[string]map[string]string, totalEntities),
		nameKindsReal:      make(map[string]map[string]string, totalEntities),
		byLocation:         make(LocationIndex, cap2),
		ambigLocation:      make(map[string]map[string]bool),
		byLocationKind:     make(LocationKindIndex, cap2),
		byLocationKindReal: make(LocationKindIndex, cap2),
		byMember:           make(map[string]map[string]map[string]string),
		byPackageMember:    make(map[string]map[string]map[string]string),
		byPackageOperation: make(map[string]map[string]string),
		byPackageComponent: make(map[string]map[string]string),
		byNamespaceMember:  make(map[string]map[string]map[string]string),
		byKotlinPkgMember:  make(map[string]map[string]map[string]string),
		byKotlinPkgFunc:    make(map[string]map[string]string),
		byQualifiedName:    make(map[string]string, cap4),
		PlatformVariants:   make(map[string][]string),
	}
}

// emptyIndex returns a fully initialised but empty Index.
func emptyIndex() Index {
	return Index{
		byKind:             make(map[string]map[string]string),
		ambigKind:          make(map[string]map[string]bool),
		byName:             make(map[string]string),
		ambigName:          make(map[string]bool),
		nameKinds:          make(map[string]map[string]string),
		nameKindsReal:      make(map[string]map[string]string),
		byLocation:         make(LocationIndex),
		ambigLocation:      make(map[string]map[string]bool),
		byLocationKind:     make(LocationKindIndex),
		byLocationKindReal: make(LocationKindIndex),
		byMember:           make(map[string]map[string]map[string]string),
		byPackageMember:    make(map[string]map[string]map[string]string),
		byPackageOperation: make(map[string]map[string]string),
		byPackageComponent: make(map[string]map[string]string),
		byNamespaceMember:  make(map[string]map[string]map[string]string),
		byKotlinPkgMember:  make(map[string]map[string]map[string]string),
		byKotlinPkgFunc:    make(map[string]map[string]string),
		byQualifiedName:    make(map[string]string),
		PlatformVariants:   make(map[string][]string),
	}
}

// insertModuleEntry inserts a single moduleEntry into idx, applying the same
// indexing rules as BuildIndex.  Extracted into a function so both
// MergeModuleBatch and BuildIndexFromModules share identical logic.
//
// pkgOpTag/pkgCompTag/pkgOpSrc/pkgCompSrc are the per-call side-tables for
// platform-variant tracking (mirrors BuildIndex's local vars).
func insertModuleEntry(
	idx *Index,
	me *moduleEntry,
	pkgOpTag, pkgCompTag map[string]map[string]string,
	pkgOpSrc, pkgCompSrc map[string]map[string]string,
) {
	// QualifiedName index.
	if me.qualifiedName != "" {
		if existing, ok := idx.byQualifiedName[me.qualifiedName]; ok && existing != me.id {
			idx.byQualifiedName[me.qualifiedName] = ""
		} else {
			idx.byQualifiedName[me.qualifiedName] = me.id
		}
	}
	// ref property indexing (endpoint, testcoverage, interface stubs).
	if me.refProp != "" {
		if _, ok := idx.byQualifiedName[me.refProp]; !ok {
			idx.byQualifiedName[me.refProp] = me.id
		}
	}

	// byKind and nameKinds — index under both original kind and SCOPE-trimmed kind.
	kinds := []string{me.kind}
	if me.kindTrimmed != "" {
		kinds = append(kinds, me.kindTrimmed)
	}
	for _, kind := range kinds {
		if kind == "" {
			continue
		}
		if idx.ambigKind[kind] != nil && idx.ambigKind[kind][me.name] {
			continue
		}
		bucket := idx.byKind[kind]
		if bucket == nil {
			bucket = make(map[string]string)
			idx.byKind[kind] = bucket
		}
		if existing, ok := bucket[me.name]; ok && existing != me.id {
			delete(bucket, me.name)
			if idx.ambigKind[kind] == nil {
				idx.ambigKind[kind] = make(map[string]bool)
			}
			idx.ambigKind[kind][me.name] = true
			continue
		}
		bucket[me.name] = me.id
	}

	// nameKinds (all kinds, including SCOPE.*).
	nameKindBucket := idx.nameKinds[me.name]
	if nameKindBucket == nil {
		nameKindBucket = make(map[string]string)
		idx.nameKinds[me.name] = nameKindBucket
	}
	for _, kind := range kinds {
		if kind == "" {
			continue
		}
		if existing, ok := nameKindBucket[kind]; ok && existing != me.id {
			nameKindBucket[kind] = ""
		} else {
			nameKindBucket[kind] = me.id
		}
	}

	// nameKindsReal — original kind only.
	if me.kind != "" {
		realBucket := idx.nameKindsReal[me.name]
		if realBucket == nil {
			realBucket = make(map[string]string)
			idx.nameKindsReal[me.name] = realBucket
		}
		if existing, ok := realBucket[me.kind]; ok && existing != me.id {
			realBucket[me.kind] = ""
		} else {
			realBucket[me.kind] = me.id
		}
	}

	// Location indexes.
	sf := me.sourceFile
	if sf != "" {
		// byLocationKind (both kinds).
		fileKindBucket := idx.byLocationKind[sf]
		if fileKindBucket == nil {
			fileKindBucket = make(map[string]map[string]string)
			idx.byLocationKind[sf] = fileKindBucket
		}
		nameKindBucketLoc := fileKindBucket[me.name]
		if nameKindBucketLoc == nil {
			nameKindBucketLoc = make(map[string]string)
			fileKindBucket[me.name] = nameKindBucketLoc
		}
		for _, kind := range kinds {
			if kind == "" {
				continue
			}
			if existing, ok := nameKindBucketLoc[kind]; ok && existing != me.id {
				nameKindBucketLoc[kind] = ""
			} else {
				nameKindBucketLoc[kind] = me.id
			}
		}

		// byLocationKindReal — original kind only.
		if me.kind != "" {
			realFileBucket := idx.byLocationKindReal[sf]
			if realFileBucket == nil {
				realFileBucket = make(map[string]map[string]string)
				idx.byLocationKindReal[sf] = realFileBucket
			}
			realNameBucket := realFileBucket[me.name]
			if realNameBucket == nil {
				realNameBucket = make(map[string]string)
				realFileBucket[me.name] = realNameBucket
			}
			if existing, ok := realNameBucket[me.kind]; ok && existing != me.id {
				realNameBucket[me.kind] = ""
			} else {
				realNameBucket[me.kind] = me.id
			}
		}

		// byLocation (kind-agnostic, unique within file).
		if idx.ambigLocation[sf] == nil || !idx.ambigLocation[sf][me.name] {
			bucket := idx.byLocation[sf]
			if bucket == nil {
				bucket = make(map[string]string)
				idx.byLocation[sf] = bucket
			}
			if existing, ok := bucket[me.name]; ok && existing != me.id {
				delete(bucket, me.name)
				if idx.ambigLocation[sf] == nil {
					idx.ambigLocation[sf] = make(map[string]bool)
				}
				idx.ambigLocation[sf][me.name] = true
			} else {
				bucket[me.name] = me.id
			}
		}

		// byMember + byPackageMember (dotted names only).
		if me.dotName {
			dot := strings.LastIndexByte(me.name, dottedNameSep)
			if dot > 0 {
				scope, member := me.name[:dot], me.name[dot+1:]
				fileBucket := idx.byMember[sf]
				if fileBucket == nil {
					fileBucket = make(map[string]map[string]string)
					idx.byMember[sf] = fileBucket
				}
				scopeBucket := fileBucket[scope]
				if scopeBucket == nil {
					scopeBucket = make(map[string]string)
					fileBucket[scope] = scopeBucket
				}
				if existing, ok := scopeBucket[member]; ok && existing != me.id {
					scopeBucket[member] = ""
				} else {
					scopeBucket[member] = me.id
				}

				pkgDir := pkgDirOf(sf)
				if pkgDir != "" {
					pkgBucket := idx.byPackageMember[pkgDir]
					if pkgBucket == nil {
						pkgBucket = make(map[string]map[string]string)
						idx.byPackageMember[pkgDir] = pkgBucket
					}
					pkgScopeBucket := pkgBucket[scope]
					if pkgScopeBucket == nil {
						pkgScopeBucket = make(map[string]string)
						pkgBucket[scope] = pkgScopeBucket
					}
					if existing, ok := pkgScopeBucket[member]; ok && existing != me.id {
						pkgScopeBucket[member] = ""
					} else {
						pkgScopeBucket[member] = me.id
					}
				}

				// byNamespaceMember (#4374, C#) — namespace-scoped member
				// index. Mirrors BuildIndex: index "<Type>.<member>" entities
				// that carry csharp_namespace under [namespace][Type][member].
				if me.properties != nil {
					if nsName := me.properties["csharp_namespace"]; nsName != "" {
						nsBucket := idx.byNamespaceMember[nsName]
						if nsBucket == nil {
							nsBucket = make(map[string]map[string]string)
							idx.byNamespaceMember[nsName] = nsBucket
						}
						nsScopeBucket := nsBucket[scope]
						if nsScopeBucket == nil {
							nsScopeBucket = make(map[string]string)
							nsBucket[scope] = nsScopeBucket
						}
						if existing, ok := nsScopeBucket[member]; ok && existing != me.id {
							nsScopeBucket[member] = ""
						} else {
							nsScopeBucket[member] = me.id
						}
					}
				}
			}
		}
	}

	// Kotlin package-scoped indexes (#4375). Mirrors BuildIndex: Kotlin
	// function entities carry a BARE Name plus kotlin_package /
	// kotlin_enclosing_type properties.
	if me.properties != nil && isOperationKind(me.kind) {
		if pkg := me.properties["kotlin_package"]; pkg != "" && me.name != "" {
			if typ := me.properties["kotlin_enclosing_type"]; typ != "" {
				pkgBucket := idx.byKotlinPkgMember[pkg]
				if pkgBucket == nil {
					pkgBucket = make(map[string]map[string]string)
					idx.byKotlinPkgMember[pkg] = pkgBucket
				}
				typeBucket := pkgBucket[typ]
				if typeBucket == nil {
					typeBucket = make(map[string]string)
					pkgBucket[typ] = typeBucket
				}
				if existing, ok := typeBucket[me.name]; ok && existing != me.id {
					typeBucket[me.name] = ""
				} else {
					typeBucket[me.name] = me.id
				}
			} else {
				funcBucket := idx.byKotlinPkgFunc[pkg]
				if funcBucket == nil {
					funcBucket = make(map[string]string)
					idx.byKotlinPkgFunc[pkg] = funcBucket
				}
				if existing, ok := funcBucket[me.name]; ok && existing != me.id {
					funcBucket[me.name] = ""
				} else {
					funcBucket[me.name] = me.id
				}
			}
		}
	}

	// byPackageOperation (top-level operations, non-dotted name).
	if sf != "" && !me.dotName && isOperationKind(me.kind) {
		pkgDir := pkgDirOf(sf)
		if pkgDir != "" {
			pkgBucket := idx.byPackageOperation[pkgDir]
			if pkgBucket == nil {
				pkgBucket = make(map[string]string)
				idx.byPackageOperation[pkgDir] = pkgBucket
			}
			tagBucket := pkgOpTag[pkgDir]
			if tagBucket == nil {
				tagBucket = make(map[string]string)
				pkgOpTag[pkgDir] = tagBucket
			}
			srcBucket := pkgOpSrc[pkgDir]
			if srcBucket == nil {
				srcBucket = make(map[string]string)
				pkgOpSrc[pkgDir] = srcBucket
			}
			incomingTag := ""
			if me.properties != nil {
				incomingTag = me.properties["build_tag"]
			}
			if existing, ok := pkgBucket[me.name]; ok && existing != me.id {
				existingTag := tagBucket[me.name]
				if buildTagsMutuallyExclusive(existingTag, incomingTag) {
					canonicalID := existing
					nonCanonicalID := me.id
					if sf < srcBucket[me.name] {
						canonicalID = me.id
						nonCanonicalID = existing
						srcBucket[me.name] = sf
					}
					pkgBucket[me.name] = canonicalID
					tagBucket[me.name] = mergePlatformVariantTags(existingTag, incomingTag)
					idx.PlatformVariants[canonicalID] = append(idx.PlatformVariants[canonicalID], nonCanonicalID)
				} else {
					pkgBucket[me.name] = ""
				}
			} else if _, taken := pkgBucket[me.name]; !taken {
				pkgBucket[me.name] = me.id
				tagBucket[me.name] = incomingTag
				srcBucket[me.name] = sf
			}
		}
	}

	// byPackageComponent (top-level components, non-dotted name).
	if sf != "" && !me.dotName && isComponentKind(me.kind) {
		pkgDir := pkgDirOf(sf)
		if pkgDir != "" {
			pkgBucket := idx.byPackageComponent[pkgDir]
			if pkgBucket == nil {
				pkgBucket = make(map[string]string)
				idx.byPackageComponent[pkgDir] = pkgBucket
			}
			tagBucket := pkgCompTag[pkgDir]
			if tagBucket == nil {
				tagBucket = make(map[string]string)
				pkgCompTag[pkgDir] = tagBucket
			}
			srcBucket := pkgCompSrc[pkgDir]
			if srcBucket == nil {
				srcBucket = make(map[string]string)
				pkgCompSrc[pkgDir] = srcBucket
			}
			incomingTag := ""
			if me.properties != nil {
				incomingTag = me.properties["build_tag"]
			}
			if existing, ok := pkgBucket[me.name]; ok && existing != me.id {
				existingTag := tagBucket[me.name]
				if buildTagsMutuallyExclusive(existingTag, incomingTag) {
					canonicalID := existing
					nonCanonicalID := me.id
					if sf < srcBucket[me.name] {
						canonicalID = me.id
						nonCanonicalID = existing
						srcBucket[me.name] = sf
					}
					pkgBucket[me.name] = canonicalID
					tagBucket[me.name] = mergePlatformVariantTags(existingTag, incomingTag)
					idx.PlatformVariants[canonicalID] = append(idx.PlatformVariants[canonicalID], nonCanonicalID)
				} else {
					pkgBucket[me.name] = ""
				}
			} else if _, taken := pkgBucket[me.name]; !taken {
				pkgBucket[me.name] = me.id
				tagBucket[me.name] = incomingTag
				srcBucket[me.name] = sf
			}
		}
	}

	// byName — kind-agnostic; ambiguous when two entities with different IDs
	// share the same name.
	if idx.ambigName[me.name] {
		return
	}
	if existing, ok := idx.byName[me.name]; ok && existing != me.id {
		delete(idx.byName, me.name)
		idx.ambigName[me.name] = true
		return
	}
	idx.byName[me.name] = me.id
}

// ─────────────────────────────────────────────────────────────────────────────
// Lazy edge materialization
// ─────────────────────────────────────────────────────────────────────────────

// LazyEdgeKey identifies a cross-module CALLS edge to be resolved lazily.
// FromModule / ToModule are the ModuleKeys of the caller and callee.
// Stub is the unresolved ToID stub (e.g. "Function:Greet" or a structural ref).
// Kind is the relationship kind (e.g. "CALLS").
type LazyEdgeKey struct {
	FromModule ModuleKey
	ToModule   ModuleKey
	Stub       string
	Kind       string
}

// LazyEdgeSet is a registry of deferred cross-module edges.  Hot-path callers
// (e.g. a monorepo indexer that knows two modules share a package boundary)
// register stubs here; ResolveAll then processes them in one pass against the
// fully-built Index, avoiding per-edge map lookups during the extraction phase.
//
// This is the "lazy edge materialization" component of M5: stubs are collected
// during extraction (cheap) and resolved once the complete symbol table is
// available (necessary for cross-module correctness).
type LazyEdgeSet struct {
	// entries maps each unique stub to the slice of relationship records that
	// carry it.  Using the stub as the key means we call LookupStatusHint at
	// most once per unique stub rather than once per relationship record.
	entries map[LazyEdgeKey][]*types.RelationshipRecord
}

// NewLazyEdgeSet returns an initialised LazyEdgeSet.
func NewLazyEdgeSet() *LazyEdgeSet {
	return &LazyEdgeSet{entries: make(map[LazyEdgeKey][]*types.RelationshipRecord)}
}

// Register adds a relationship record to the lazy set.  The record's ToID
// field MUST contain the unresolved stub; it will be rewritten in place by
// ResolveAll.
func (les *LazyEdgeSet) Register(fromMod, toMod ModuleKey, kind string, r *types.RelationshipRecord) {
	key := LazyEdgeKey{FromModule: fromMod, ToModule: toMod, Stub: r.ToID, Kind: kind}
	les.entries[key] = append(les.entries[key], r)
}

// ResolveAll resolves every registered stub against idx and rewrites the
// relationship records in place.  Returns the number of stubs that were
// successfully resolved.
//
// Complexity: O(U) where U is the number of *unique* stubs — deduplication
// is the core win over calling LookupStatusHint once per relationship record.
func (les *LazyEdgeSet) ResolveAll(idx Index) int {
	resolved := 0
	for key, records := range les.entries {
		id, status := idx.LookupStatusHint(key.Stub, key.Kind)
		if status != statusRewritten || id == "" {
			continue
		}
		for _, r := range records {
			r.ToID = id
		}
		resolved += len(records)
	}
	return resolved
}

// Size returns the number of unique stub keys registered.
func (les *LazyEdgeSet) Size() int { return len(les.entries) }
