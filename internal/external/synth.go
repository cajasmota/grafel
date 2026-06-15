// Package external synthesises placeholder entities for references that
// point at code outside the indexed corpus — third-party packages
// (django, react, lodash...), language stdlib (os, json, fmt...), and
// well-known stdlib symbols (Println, print...).
//
// PORT-EXT (issue #32). After Pass 3 + the resolver (PORT-2-FIX,
// PORT-2-FIX-3) finish, a meaningful fraction of relationships still
// have stub strings as ToID — by construction, because the target
// source isn't in the corpus. They are nonetheless real graph edges
// the agent should be able to traverse and stop cleanly at. This pass
// turns each unique unresolved external into a placeholder Entity with
// id "ext:<canonical-name>" and rewrites the relationship's ToID to
// point at it.
package external

import (
	"net/url"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/resolve"
	"github.com/cajasmota/archigraph/internal/types"
)

// KindExternal is the entity kind stamped on every synthesised
// placeholder. It joins the existing SCOPE.* taxonomy used elsewhere
// in the indexer. Kept as a string alias for callers; the source of
// truth is types.EntityKindExternal (Issue #77).
const KindExternal = string(types.EntityKindExternal)

// ExtIDPrefix is the deterministic prefix used by external-entity IDs.
// It is intentionally NOT a 16-char hex string so the resolver's
// isHexID heuristic continues to treat it as a stub-shaped value if a
// later pass ever encounters it.
const ExtIDPrefix = "ext:"

// Stats reports how the synthesis pass touched the document.
type Stats struct {
	// Synthesized is the number of NEW placeholder entities appended to
	// the document. Equal to UniqueExternals on a fresh run; zero on a
	// re-run because every external is already present.
	Synthesized int
	// RelationshipsResolved is the number of relationship endpoints
	// rewritten from a bare-name stub to "ext:<name>".
	RelationshipsResolved int
	// UniqueExternals is the number of distinct external names this
	// pass touched (including any that were already present from a
	// previous run).
	UniqueExternals int
	// DynamicTargetsResolved is the number of relationship endpoints
	// that matched the per-language dynamic-pattern catalog and were
	// stamped with "dynamic_target" instead of emitting a placeholder
	// External entity. Issue #1085.
	DynamicTargetsResolved int
}

// upsertImportSet adds an IMPORTS edge to a per-file import set,
// returning the (possibly newly-allocated) set. The set is keyed by
// the import target (the ToID for #577-shape IMPORTS edges this is
// `ext:<package>`; for pre-#577 path-shape edges it was the imported
// package literal). The Properties["source_module"] / ["imported_name"]
// columns are also folded in so module-prefix gates
// (`hasKafkaImport` looks for `org.apache.kafka.*`) match against the
// real dotted module name, not just the ext:* placeholder.
func upsertImportSet(set map[string]bool, rel *graph.Relationship) map[string]bool {
	if set == nil {
		set = make(map[string]bool, 4)
	}
	if rel.ToID != "" {
		set[rel.ToID] = true
		// Refs #44 — Go per-import gate fix. The Go extractor rewrites
		// IMPORTS edge ToIDs to the `ext:<path>` form (e.g.
		// "ext:time", "ext:github.com/go-chi/chi") BEFORE Synthesize()
		// builds the fileImports map, so upsertImportSet always inserts
		// the ext:-prefixed value. Per-import gates like
		// `fromImports["time"]` and `hasGoChiImport` check for the
		// bare path (e.g. "time", "github.com/go-chi/chi") — they never
		// matched because the bare form was never inserted. Fix: when
		// the ToID carries an "ext:" prefix, also add the bare path so
		// all existing gate predicates fire correctly.
		if strings.HasPrefix(rel.ToID, ExtIDPrefix) {
			bare := rel.ToID[len(ExtIDPrefix):]
			if bare != "" {
				set[bare] = true
			}
		}
	}
	if rel.Properties != nil {
		mod := rel.Properties["source_module"]
		imp := rel.Properties["imported_name"]
		if mod != "" {
			set[mod] = true
		}
		if imp != "" {
			set[imp] = true
		}
		// Issue #787c — Java bare-name constructor folding. The import-leaf
		// resolver (classifyExternal line 802) finds the leaf of each import
		// path and, when it matches the stub, calls longestKnownDottedPrefix
		// on that import path. After #681 the IMPORTS edge ToID is rewritten to
		// `ext:<prefix>:<leaf>` (colons, not dots) and source_module carries only
		// the package (`org.apache.poi.xssf.usermodel`), not the FQN.
		// longestKnownDottedPrefix requires dots and returns "" for bare names,
		// so `new XSSFWorkbook()` stubs never match.  Adding the synthetic FQN
		// `source_module + "." + imported_name` gives the resolver a dotted path
		// it CAN walk, enabling `org.apache` (or any other external prefix) to
		// be found.  Non-wildcard imports only (wildcard imported_name is "").
		if mod != "" && imp != "" && !strings.ContainsAny(imp, ".*") {
			set[mod+"."+imp] = true
		}
	}
	return set
}

// Synthesize scans every relationship in doc, looks for endpoints
// whose ToID is a still-unresolved string that matches an external
// reference heuristic, and appends placeholder entities for each
// unique external. The relationship's ToID is rewritten in-place to
// "ext:<canonical-name>". Idempotent: calling Synthesize twice on the
// same document is a no-op on the second call.
func Synthesize(doc *graph.Document) Stats {
	if doc == nil {
		return Stats{}
	}

	// Build a set of all known entity IDs so we don't re-synthesise an
	// external that already exists in the document. Re-runs of this
	// pass on the same document must be idempotent.
	known := make(map[string]bool, len(doc.Entities))
	// entityLang maps every entity ID to its declared language so the
	// classifier can apply per-language bare-name allowlists (issue #103
	// — Go stdlib/framework Pascal-case method names that arrive at the
	// resolver after the extractor strips the receiver). Lookups against
	// non-existent IDs return "" (the zero value), which falls back to
	// the language-agnostic stop-list — matching pre-#103 behaviour for
	// every relationship whose FromID isn't a known entity.
	entityLang := make(map[string]string, len(doc.Entities))
	// entityFile maps every entity ID to its declared SourceFile so the
	// classifier can apply file-path-gated bare-name allowlists (issue
	// #115 — Go testify helpers like Equal/NoError that are receiver-
	// stripped by the extractor and would collide trivially with user
	// methods if classified globally). Only test-file callers (paths
	// ending in `_test.go`) are eligible. Lookups against non-existent
	// IDs return "" — matching pre-#115 behaviour.
	entityFile := make(map[string]string, len(doc.Entities))
	for k := range doc.Entities {
		known[doc.Entities[k].ID] = true
		entityLang[doc.Entities[k].ID] = doc.Entities[k].Language
		entityFile[doc.Entities[k].ID] = doc.Entities[k].SourceFile
	}

	// fileImports maps every source file path to the set of import paths
	// that file declares, walking IMPORTS edges (FromID = filePath, ToID
	// = imported package). Used by the classifier to file-path-gate
	// import-aware bare-name allowlists (issue #131 — chi router methods
	// like Get/Post/Put/Delete that collide with HTTP-verb generic getter
	// names and must only classify when the source file actually imports
	// `github.com/go-chi/chi`). Lookups against non-existent paths return
	// nil — matching pre-#131 behaviour for files that import nothing.
	fileImports := make(map[string]map[string]bool)
	for k := range doc.Relationships {
		rel := &doc.Relationships[k]
		if rel.Kind != string(types.RelationshipKindImports) {
			continue
		}
		if rel.FromID == "" || rel.ToID == "" {
			continue
		}
		// Issue kafka-chase-578 — #577 moved every extractor's IMPORTS
		// FromID from the literal file path to the hex ID of a
		// `SCOPE.Component(subtype=file)` entity that mirrors the file.
		// File-import gates (`hasKafkaImport`, `hasCommonsCliImport`,
		// `hasJaxRsImport`, `hasGoChiImport`, `hasKtorServerImport`, ...)
		// look the import set up by the *caller file path* via
		// `fileImports[entityFile[caller]]`. Index by both shapes so the
		// path-keyed lookup the gates use keeps working, while older
		// extractors that still emit path-shaped FromIDs (or any future
		// regression) continue to populate the same set.
		key := rel.FromID
		fileImports[key] = upsertImportSet(fileImports[key], rel)
		if path := entityFile[rel.FromID]; path != "" && path != rel.FromID {
			fileImports[path] = upsertImportSet(fileImports[path], rel)
		}
	}

	// #4699 — internal Python module roots. The Python catch-all in
	// classifyExternal (pyExternalPackageRoot) needs to distinguish a
	// genuinely-internal same-repo import that failed to resolve (still a
	// fidelity bug) from a third-party pip package (NOT a bug). We derive
	// the set of top-level package roots owned by this repo from the
	// SourceFile of every indexed Python entity, applying the same
	// source-root stripping the Python extractor's filePathToModule uses
	// (src/, lib/, app/). An unresolved non-relative import whose root is
	// NOT in this set is, by construction, an external pip dependency.
	internalPyRoots := make(map[string]bool)
	for k := range doc.Entities {
		e := &doc.Entities[k]
		if e.Language != "python" || e.SourceFile == "" {
			continue
		}
		if root := pythonFileRoot(e.SourceFile); root != "" {
			internalPyRoots[root] = true
		}
	}

	// #4700-#4704 — per-language internal-root sets. The same false-positive
	// class that #4695 (TS/JS) and #4699 (Python) closed exists for every
	// ecosystem with a third-party dependency surface: an unresolved
	// non-relative import whose root is NOT owned by this repo is, by
	// construction, an external dependency (maven/gradle, gem, crate,
	// go-module, nuget) — never a fidelity bug. To keep the catch-alls from
	// MASKING a genuinely-internal same-repo import that merely failed to
	// resolve, each language's catch-all rejects roots that ARE owned by the
	// repo (derived here from the SourceFile of every indexed entity in that
	// language). Mirrors internalPyRoots exactly.
	//
	//   internalJavaRoots — top-level package roots of indexed Java/Kotlin
	//     entities, derived from the dotted package implied by the source
	//     path (e.g. "src/main/java/com/acme/foo/Bar.java" → "com").
	//   internalRubyRoots — top-level lib/dir roots of indexed Ruby entities
	//     (e.g. "app/models/user.rb" → "user"; "lib/billing/charge.rb" →
	//     "billing"). require_relative is handled structurally (it's relative)
	//     so this set guards bare `require 'x'` whose x collides with a repo lib.
	//   internalGoRoots — host-prefixed module roots ("<host>/<owner>/<repo>")
	//     AND repo-relative leading dir segments of indexed Go entities, so a
	//     self-module import that failed to resolve stays a bug instead of
	//     being masked as external.
	//   internalRustRoots — crate/module roots of indexed Rust entities
	//     (leading dir under src/, file stem at crate root).
	//   internalCsharpRoots — root namespace of indexed C# entities, derived
	//     from the QualifiedName/Name (dotted) or the source path.
	internalJavaRoots := make(map[string]bool)
	internalRubyRoots := make(map[string]bool)
	internalGoRoots := make(map[string]bool)
	internalRustRoots := make(map[string]bool)
	internalCsharpRoots := make(map[string]bool)
	for k := range doc.Entities {
		e := &doc.Entities[k]
		switch e.Language {
		case "java", "kotlin":
			if root := javaPackageRoot(e); root != "" {
				internalJavaRoots[root] = true
			}
		case "ruby":
			if root := rubyFileRoot(e.SourceFile); root != "" {
				internalRubyRoots[root] = true
			}
		case "go":
			for _, root := range goInternalRoots(e.SourceFile) {
				internalGoRoots[root] = true
			}
		case "rust":
			if root := rustFileRoot(e.SourceFile); root != "" {
				internalRustRoots[root] = true
			}
		case "csharp":
			if root := csharpNamespaceRoot(e); root != "" {
				internalCsharpRoots[root] = true
			}
		}
	}

	// #4515 — per-symbol named-import index. Build, per source file, the set
	// of symbols that file named-imports from an EXTERNAL package, so a
	// bare-name reference (`throw new NotFoundException()`, `extends BaseX`,
	// `: SomeType`) resolves to a distinct, stable `ext:<pkg>:<Symbol>` node
	// instead of folding to the package-level placeholder (which loses the
	// symbol identity and leaves #4480's throws→class retarget nothing to land
	// on → the imported-exception DUPLICATE). Keyed by package so two packages
	// exporting the same name get distinct nodes. Built once, consulted in the
	// relationship loop below before the package-level classifier.
	namedImports := buildNamedImportIndex(doc, entityLang, entityFile, internalRoots{
		python: internalPyRoots,
		java:   internalJavaRoots,
		ruby:   internalRubyRoots,
		golang: internalGoRoots,
		rust:   internalRustRoots,
		csharp: internalCsharpRoots,
	})

	// First pass — collect every unique external name we want to
	// synthesise. The placeholder carries a subtype hint
	// ("package"/"function") but the language field is left empty:
	// we don't reliably know the source language at this layer (a name
	// like "json" or "abc" exists in multiple ecosystems), and an
	// inaccurate language tag is worse than none at all. Language can
	// be populated by a downstream enrichment pass that has more
	// context (e.g. inspecting the import statement that produced the
	// edge).
	type externalInfo struct {
		canonical string
		subtype   string
		language  string
		// name overrides the entity Name when non-empty. Per-symbol named-import
		// nodes (#4515) set this to the bare imported symbol (e.g.
		// "NotFoundException") while canonical stays the package-keyed id body
		// ("@nestjs/common:NotFoundException"), so #4480's name-keyed
		// throws→class resolver can match the synthetic exception node to this
		// per-symbol external class.
		name string
	}
	uniques := make(map[string]externalInfo) // ext-id -> info
	resolved := 0

	dynamicTargets := 0
	for k := range doc.Relationships {
		rel := &doc.Relationships[k]
		if rel.ToID == "" || isHexID(rel.ToID) || strings.HasPrefix(rel.ToID, ExtIDPrefix) {
			continue
		}
		// Issue #364 — fall back to the relationship's stamped language
		// when the FromID isn't a known entity (e.g. unresolved bare-name
		// caller, ambiguous-name caller). Without this, Go-only branches
		// in classifyExternal (receiver_type stdlib dispatch) miss every
		// edge whose source isn't a 1:1-resolvable entity.
		lang := entityLang[rel.FromID]
		if lang == "" && rel.Properties != nil {
			lang = rel.Properties["language"]
		}

		// Issue #1085 — stdlib-builtin guard. If the unresolved stub is an
		// unambiguous stdlib builtin for the given language (int, str,
		// list, len, range, … for Python), stamp the bare name as a
		// "dynamic_target" property on the edge and clear ToID so no
		// placeholder External entity is emitted.
		//
		// This is intentionally narrower than the full dynamic-pattern
		// catalog: framework DSL names (Flask route/before_request,
		// SQLAlchemy commit/rollback, …) still flow through to
		// classifyExternal so the per-import gate can fold them to the
		// right ext:<package> placeholder. Only the core language builtins
		// that can NEVER resolve to a user entity are intercepted here.
		//
		// Real third-party packages (numpy, requests, …) are not in the
		// stdlib-builtin set and flow through to the existing
		// classifyExternal path unchanged.
		if resolve.IsStdlibBuiltinTarget(rel.ToID, lang) {
			if rel.Properties == nil {
				rel.Properties = make(map[string]string)
			}
			rel.Properties["dynamic_target"] = rel.ToID
			rel.ToID = ""
			dynamicTargets++
			continue
		}

		// #4515 — per-symbol named-import resolution. Snapshot the bare-name
		// reference's imported-symbol identity BEFORE the package-level
		// classifier runs. IMPORTS edges are excluded (they carry the package
		// disposition and must keep folding to the package-level placeholder);
		// only *reference* edges (CALLS/THROWS/EXTENDS/IMPLEMENTS/USES_TYPE/…)
		// to a named-imported external symbol get upgraded to the per-symbol
		// node. The upgrade is applied AFTER classifyExternal so it reuses the
		// classifier's precise package canon (e.g. ext:org.apache.poi, not the
		// coarse 2-segment fold) — we only append the resolved symbol leaf.
		var perSymbolLeaf, perSymbolPkg string
		if rel.Kind != string(types.RelationshipKindImports) && !namedImports.empty() {
			if pkg, leaf, hit := namedImports.lookup(entityFile[rel.FromID], rel.ToID); hit {
				perSymbolLeaf = leaf
				perSymbolPkg = pkg
			}
		}

		canonical, subtype, ok := classifyExternal(rel.ToID, rel.Kind, lang, entityFile[rel.FromID], fileImports[entityFile[rel.FromID]], rel.Properties, internalRoots{
			python: internalPyRoots,
			java:   internalJavaRoots,
			ruby:   internalRubyRoots,
			golang: internalGoRoots,
			rust:   internalRustRoots,
			csharp: internalCsharpRoots,
		})

		// #4515 — apply per-symbol upgrade. Two cases:
		//   1. classifyExternal resolved to a package-level canon (no symbol
		//      leaf yet) — append the named-imported symbol so the reference
		//      binds to ext:<pkg>:<Symbol> (reuses the precise classifier canon,
		//      e.g. ext:org.apache.poi). Already-per-symbol canons (containing a
		//      ":<Pascal>" leaf) are left untouched.
		//   2. classifyExternal did NOT resolve (ok=false) but the symbol IS a
		//      named import — synthesise the per-symbol node off the index's
		//      package root so imported framework classes/exceptions still get a
		//      distinct node instead of dangling unresolved.
		perSymbol := false
		if perSymbolLeaf != "" {
			switch {
			case ok && !canonicalHasSymbolLeaf(canonical):
				canonical = canonical + ":" + perSymbolLeaf
				perSymbol = true
				ok = true
			case !ok && perSymbolPkg != "":
				canonical = perSymbolPkg + ":" + perSymbolLeaf
				perSymbol = true
				ok = true
			}
		}
		if !ok {
			continue
		}
		extID := ExtIDPrefix + canonical
		if _, seen := uniques[extID]; !seen {
			info := externalInfo{
				canonical: canonical,
				subtype:   subtype,
				language:  "",
			}
			if perSymbol {
				info.subtype = "symbol"
				info.name = perSymbolLeaf
			}
			uniques[extID] = info
		}
		rel.ToID = extID
		resolved++
	}

	// Sort canonical names for deterministic append order — keeps
	// graph.json byte-stable across runs on the same corpus.
	keys := make([]string, 0, len(uniques))
	for k := range uniques {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	synthesised := 0
	for _, extID := range keys {
		if known[extID] {
			continue // re-run path: placeholder already present
		}
		info := uniques[extID]
		entName := info.canonical
		if info.name != "" {
			entName = info.name
		}
		doc.Entities = append(doc.Entities, graph.Entity{
			ID:            extID,
			Name:          entName,
			QualifiedName: info.canonical,
			Kind:          KindExternal,
			Subtype:       info.subtype,
			SourceFile:    "",
			Language:      info.language,
			Metadata: map[string]interface{}{
				"is_external":    true,
				"discovered_via": "ext-synthesis",
			},
		})
		known[extID] = true
		synthesised++
	}

	// Reflect the new entities + rewritten edges in the doc-level
	// stats. Relationships count is unchanged (we rewrote endpoints,
	// not added rows) but Entities grew by len(synthesised).
	doc.Stats.Entities = len(doc.Entities)
	doc.Stats.Relationships = len(doc.Relationships)

	return Stats{
		Synthesized:            synthesised,
		RelationshipsResolved:  resolved,
		UniqueExternals:        len(uniques),
		DynamicTargetsResolved: dynamicTargets,
	}
}

// parseDataAccessQN parses a scope:dataaccess:<file>#<orm>:<op>:<table>
// qualified name and returns (file, table, ok).
// Returns ok=false if the form is invalid, table is empty, or table is "UNKNOWN".
func parseDataAccessQN(qn string) (file, table string, ok bool) {
	const prefix = "scope:dataaccess:"
	if !strings.HasPrefix(qn, prefix) {
		return "", "", false
	}
	rest := qn[len(prefix):]
	hash := strings.IndexByte(rest, '#')
	if hash < 0 {
		return "", "", false
	}
	file = rest[:hash]
	after := rest[hash+1:]
	// after = <orm>:<op>:<table>
	parts := strings.SplitN(after, ":", 3)
	if len(parts) != 3 {
		return "", "", false
	}
	table = parts[2]
	if table == "" || table == "UNKNOWN" {
		return "", "", false
	}
	return file, table, true
}

// SynthesizeDBEntities (issue #532) scans every SCOPE.DataAccess entity in
// doc, extracts the table name from its QualifiedName
// (scope:dataaccess:<file>#<orm>:<op>:<table>), and synthesises:
//
//  1. One ext:db.<table> external entity per distinct table seen.
//  2. One IMPORTS edge per distinct (sourceFile, table) pair.
//
// UNKNOWN table entries are skipped. The pass is idempotent: calling it
// twice on the same document is a no-op on the second call. Returns Stats
// with Synthesized = new entity count and RelationshipsResolved = new IMPORTS
// edge count.
func SynthesizeDBEntities(doc *graph.Document) Stats {
	if doc == nil {
		return Stats{}
	}

	// Build a set of existing entity IDs for idempotency.
	knownEntities := make(map[string]bool, len(doc.Entities))
	for k := range doc.Entities {
		knownEntities[doc.Entities[k].ID] = true
	}

	// Build a set of existing IMPORTS edges for deduplication.
	// Key: fromID + "|" + toID
	knownEdges := make(map[string]bool, len(doc.Relationships))
	for k := range doc.Relationships {
		rel := &doc.Relationships[k]
		if rel.Kind == string(types.RelationshipKindImports) {
			knownEdges[rel.FromID+"|"+rel.ToID] = true
		}
	}

	// Scan SCOPE.DataAccess entities.
	// tables: table name → bool (distinct tables seen)
	// seenFileTables: "file|table" → bool (dedup IMPORTS edges)
	tables := make(map[string]bool)
	seenFileTables := make(map[string]bool)
	type importEdge struct {
		fromID string
		toID   string
		table  string
	}
	var newEdges []importEdge

	for k := range doc.Entities {
		e := &doc.Entities[k]
		if e.Kind != "SCOPE.DataAccess" {
			continue
		}
		file, table, ok := parseDataAccessQN(e.QualifiedName)
		if !ok {
			continue
		}
		tables[table] = true
		extID := ExtIDPrefix + "db." + table
		// Use SourceFile from the entity as the FromID of the IMPORTS edge.
		// This is consistent with how other extractors emit IMPORTS edges
		// from file paths (pre-#577 shape; works for MCP graph queries).
		fromID := e.SourceFile
		if fromID == "" {
			fromID = file
		}
		fileTableKey := fromID + "|" + table
		if seenFileTables[fileTableKey] {
			continue
		}
		seenFileTables[fileTableKey] = true
		edgeKey := fromID + "|" + extID
		if knownEdges[edgeKey] {
			continue
		}
		newEdges = append(newEdges, importEdge{
			fromID: fromID,
			toID:   extID,
			table:  table,
		})
	}

	if len(tables) == 0 {
		return Stats{}
	}

	// Sort for deterministic append order.
	sortedTables := make([]string, 0, len(tables))
	for t := range tables {
		sortedTables = append(sortedTables, t)
	}
	sort.Strings(sortedTables)

	synthesised := 0
	for _, table := range sortedTables {
		extID := ExtIDPrefix + "db." + table
		if knownEntities[extID] {
			continue
		}
		doc.Entities = append(doc.Entities, graph.Entity{
			ID:            extID,
			Name:          table,
			QualifiedName: "db." + table,
			Kind:          KindExternal,
			Subtype:       "sql_table",
			SourceFile:    "",
			Language:      "",
			Metadata: map[string]interface{}{
				"is_external":    true,
				"discovered_via": "db-synth",
				"module":         "db",
			},
		})
		knownEntities[extID] = true
		synthesised++
	}

	// Sort edges for deterministic append order.
	sort.Slice(newEdges, func(i, j int) bool {
		if newEdges[i].fromID != newEdges[j].fromID {
			return newEdges[i].fromID < newEdges[j].fromID
		}
		return newEdges[i].toID < newEdges[j].toID
	})

	edgesAdded := 0
	for _, e := range newEdges {
		edgeKey := e.fromID + "|" + e.toID
		if knownEdges[edgeKey] {
			continue
		}
		doc.Relationships = append(doc.Relationships, graph.Relationship{
			ID:     graph.RelationshipID(e.fromID, e.toID, string(types.RelationshipKindImports)),
			FromID: e.fromID,
			ToID:   e.toID,
			Kind:   string(types.RelationshipKindImports),
			Properties: map[string]string{
				"generated": "true",
				"table":     e.table,
				"db_synth":  "531-532",
			},
		})
		knownEdges[edgeKey] = true
		edgesAdded++
	}

	doc.Stats.Entities = len(doc.Entities)
	doc.Stats.Relationships = len(doc.Relationships)

	return Stats{
		Synthesized:           synthesised,
		RelationshipsResolved: edgesAdded,
	}
}

// classifyExternal decides whether a stub-shaped ToID looks like an
// external reference, and if so returns the canonical name we should
// use for the placeholder entity.
//
// Heuristics, in order:
//
//  1. "Kind:Name" form where Name matches a well-known external —
//     canonicalise to Name (drop the kind prefix).
//  2. Bare names matching a stdlib stop-list (Println, print, etc.) —
//     canonicalise to the bare name.
//  3. Bare names matching a known third-party package allowlist
//     (django, react, lodash, ...).
//  4. Import-shaped paths whose first segment matches the allowlist
//     (e.g. "django.db.models" → "django").
//
// Returns ("", "", false) when the stub doesn't look external — those
// are left untouched and continue to count as "unmatched" in the
// resolver stats.
// internalRoots bundles the per-language sets of top-level roots owned by
// this repo, derived in Synthesize from the SourceFile/package of every
// indexed entity. The per-language external-package catch-alls (#4695,
// #4699, #4700-#4704) consult the matching set to reject a genuinely-
// internal same-repo import that merely failed to resolve — those STAY a
// fidelity bug and are never masked as external dependencies.
type internalRoots struct {
	python map[string]bool
	java   map[string]bool
	ruby   map[string]bool
	golang map[string]bool
	rust   map[string]bool
	csharp map[string]bool
}

func classifyExternal(stub, relKind, lang, fromFile string, fromImports map[string]bool, relProps map[string]string, internal internalRoots) (canonical, subtype string, ok bool) {
	if stub == "" {
		return "", "", false
	}

	// Issue #364 — Go stdlib interface dispatch. The Go extractor stamps
	// `Properties["receiver_type"]` on CALLS edges whose operand is a
	// function parameter with a known static type (e.g. `*http.Request`,
	// `http.ResponseWriter`, `io.Writer`). When the bare-name target is a
	// method on the stdlib interface for that type, route the edge to the
	// owning ext:<package> placeholder. The stamp is canonicalised by the
	// extractor (leading `*` stripped, generic type params dropped) so the
	// lookup table can use a single key per package type. Lang-gated to go.
	if lang == "go" && relKind == string(types.RelationshipKindCalls) && relProps != nil {
		if recvType := relProps["receiver_type"]; recvType != "" {
			if pkg, ok := goStdlibInterfaceMethod(recvType, stub); ok {
				return pkg, "package", true
			}
		}
	}

	// Issue #89 — manifest extractor emits dependency stubs as
	// "scope:component:external_dep:<package_manager>:<package>". The
	// "external_dep" tag is the extractor's explicit signal that this is
	// an external (third-party) dependency; route it to a placeholder so
	// the post-synthesis classifier lands it in external-known/external-
	// unknown rather than bug-extractor.
	if strings.HasPrefix(stub, "scope:component:external_dep:") {
		rest := stub[len("scope:component:external_dep:"):]
		// rest is "<pm>:<package>" — drop the package-manager segment.
		if i := strings.IndexByte(rest, ':'); i > 0 && i < len(rest)-1 {
			pkg := strings.TrimSpace(rest[i+1:])
			if pkg != "" && !strings.ContainsAny(pkg, "/\\") {
				root := pkg
				if dot := strings.IndexByte(pkg, '.'); dot > 0 {
					root = pkg[:dot]
				}
				return root, "package", true
			}
		}
	}

	// Wave-4 (TS framework) — cross/imports extractor emits structural-refs
	// of the form "scope:component:import:external:<package>" for every
	// non-local import string (Refs #19, #99). The "external" locality
	// segment is the extractor's explicit signal that this is a third-
	// party package, mirroring the existing "scope:component:external_dep:"
	// branch above (which handles manifest-extracted dependencies). Without
	// this branch the DEPENDS_ON edges from nestjs / nextjs / express test
	// suites land in bug-extractor as structural-refs whose target isn't a
	// graph entity. Route to the package placeholder so the post-synthesis
	// resolver classifies as ExternalKnown / ExternalUnknown via the
	// IsKnownExternalPackage gate. Not lang-gated — the stub shape is
	// language-agnostic and the underlying extractor emits identical
	// records for Python, Java, C#, JS, TS (single source of truth).
	if strings.HasPrefix(stub, "scope:component:import:external:") {
		pkg := strings.TrimSpace(stub[len("scope:component:import:external:"):])
		if pkg != "" && !strings.ContainsAny(pkg, "\\") {
			// Reuse the same canonicalisation as bare-name external
			// imports: scoped npm collapses to "@scope/pkg", Go-shaped
			// paths collapse to the host triple, everything else takes
			// the first dot-segment.
			if scope, ok := scopedNpmRoot(pkg); ok {
				return scope, "package", true
			}
			// Strip subpath after first '/' for unscoped (e.g.
			// "lodash/fp" → "lodash"). Mirrors the bare-name handling
			// for "@scope/pkg/sub" via scopedNpmRoot above.
			if slash := strings.IndexByte(pkg, '/'); slash > 0 {
				root := pkg[:slash]
				// Keep "node:<mod>" intact (the slash test won't hit it
				// since `node:` uses ':'). Defensive: also accept full
				// "node:<a>/<b>" forms by preserving the prefix.
				return root, "package", true
			}
			// Bare package name — return as-is for downstream allowlist
			// lookup at resolver classify time.
			return pkg, "package", true
		}
	}

	// Issue #89 — httpclient extractor emits external HTTP API references
	// as "scope:external_api:<url>". The URL is the unresolvable identity
	// of the external service; canonicalise to the host segment when we
	// can extract one (everything between "://" and the next "/"), and
	// fall back to a synthetic "external_api" bucket otherwise. Either
	// way it leaves bug-extractor.
	if strings.HasPrefix(stub, "scope:external_api:") {
		raw := stub[len("scope:external_api:"):]
		host := externalAPIHost(raw)
		if host != "" {
			return host, "external_api", true
		}
		// Bare URL fragment / non-URL identifier — bucket under a
		// stable "external_api" placeholder rather than leaving it as
		// bug-extractor.
		return "external_api", "external_api", true
	}

	// Issue #424 — YAML extractor (Docker Compose, Kubernetes) emits image
	// refs as "docker_image:<image-ref>". The image lives in a container
	// registry, not the indexed corpus, so it is external by definition.
	// Canonicalise to "docker:<repo>" (drop the tag/digest) and route the
	// edge to a single placeholder per repository — matches the package-
	// per-import collapse used elsewhere. The "docker:" prefix is on the
	// allowlist so all real image refs land in ExternalKnown.
	if strings.HasPrefix(stub, "docker_image:") {
		ref := strings.TrimSpace(stub[len("docker_image:"):])
		if repo := dockerImageRepo(ref); repo != "" {
			return "docker:" + repo, "docker_image", true
		}
	}

	// Refs #44 — YAML extractor (GitHub Actions) emits step `uses:` refs as
	// "gha_action:<org>/<repo>[/<subpath>]@<ref>". These live in the GitHub
	// Actions marketplace, never in the indexed corpus, so route them to a
	// single placeholder per action-repo (drop the version suffix). The
	// "gha:" prefix is on the allowlist so all real action refs land in
	// ExternalKnown.
	if strings.HasPrefix(stub, "gha_action:") {
		ref := strings.TrimSpace(stub[len("gha_action:"):])
		if repo := ghaActionRepo(ref); repo != "" {
			return "gha:" + repo, "gha_action", true
		}
	}

	// Issue #424 — YAML extractor emits Compose host-filesystem mounts as
	// "host_path:<path>". By definition these reference files outside the
	// indexed corpus (relative `./src`, absolute `/etc/foo`, env-driven
	// `${PWD}/data`). Bucket every distinct source path under a single
	// "external_filesystem" placeholder — the path itself is rarely useful
	// for graph navigation, and one placeholder keeps the entity count
	// flat across large compose stacks. Lands in ExternalUnknown.
	if strings.HasPrefix(stub, "host_path:") {
		path := strings.TrimSpace(stub[len("host_path:"):])
		if path != "" {
			return "external_filesystem", "file_mount", true
		}
	}

	// Pass 3 cross-language extractors emit external imports as
	// "scope:<kind>:import:external:<name>" — short structural-ref
	// form that the resolver leaves untouched (it expects 6 segments).
	// Recognise it explicitly here; the trailing segment is the
	// canonical package name.
	if strings.HasPrefix(stub, "scope:") && strings.Contains(stub, ":external:") {
		if idx := strings.LastIndex(stub, ":external:"); idx >= 0 {
			ext := stub[idx+len(":external:"):]
			ext = strings.TrimSpace(ext)
			if ext == "" {
				return "", "", false
			}
			// Issue #44 / proto-fix — Go-shaped import paths (`net/http`,
			// `google.golang.org/grpc/credentials/insecure`,
			// `github.com/foo/bar`) are external by extractor tag, but the
			// `/` separator previously dropped them straight back into
			// bug-extractor. Route them through the same canonicaliser
			// used by the standalone `isGoImportPath` branch below so we
			// collapse to a single placeholder per module (stdlib root for
			// `net/http`, `<host>/<owner>/<repo>` for host-prefixed paths).
			if isGoImportPath(ext) {
				segs := strings.Split(ext, "/")
				first := segs[0]
				if isGoImportHost(first) {
					if canonical := goHostCanonical(segs); canonical != "" {
						return canonical, "package", true
					}
				}
				// Stdlib root: only emit when the leading segment is on
				// the known-stdlib allowlist. This keeps non-Go path
				// shapes (e.g. Python `some/path`) from being captured
				// by the Go-import branch — matching the pre-fix
				// behaviour for non-Go ecosystems while still routing
				// real Go stdlib paths (`net/http`, `encoding/json`,
				// `sync/atomic`) to the right placeholder.
				if isKnownExternalPackage(first) {
					return first, "package", true
				}
			}
			// PHP / Rust / other separators still reject — only Go-shaped
			// paths get the canonicalisation above. Bare names (no `/`)
			// fall through to the existing root-segment logic.
			if strings.ContainsAny(ext, "/\\") {
				return "", "", false
			}
			root := ext
			if dot := strings.IndexByte(ext, '.'); dot > 0 {
				root = ext[:dot]
			}
			// Trust the extractor's "external" tag — emit a placeholder
			// even when the package isn't on our static allowlist. The
			// extractor has already classified it as not-local.
			return root, "package", true
		}
	}

	// Issue #82: Format A structural-refs that survived the resolver are
	// dangling by definition (the resolver rewrites resolved endpoints
	// to hex IDs). For EXTENDS edges from cross/hierarchy, the tail is
	// the parent class name — when it looks like an external import
	// (dotted, e.g. "serializers.ModelSerializer" or "rest_framework.
	// generics.ListAPIView"), synthesise a placeholder for the package
	// root. Bare-name tails are intentionally NOT handled here because
	// they could be either a local class or an external base — the
	// existing allowlist branch below already catches the well-known
	// cases.
	//
	// Format A: scope:<kind>:<subtype>:<lang>:<file_path>:<name>
	// We pull the trailing segment after the last ':' (file paths in
	// archigraph entity refs are normalised to forward slashes, so the
	// last ':' is the kind/name separator, not part of the path).
	if strings.HasPrefix(stub, "scope:") {
		if idx := strings.LastIndexByte(stub, ':'); idx >= 0 && idx < len(stub)-1 {
			tail := stub[idx+1:]
			if looksLikeExternalImport(tail) {
				root := tail
				if dot := strings.IndexByte(tail, '.'); dot > 0 {
					root = tail[:dot]
				}
				return root, "package", true
			}
		}
	}

	// Issue #101 — Rust `use foo::bar` style paths use `::` as the
	// segment separator. The Rust extractor (internal/extractors/rust)
	// emits IMPORTS edges with ToID set to the raw use-path, e.g.
	// "tokio::net::TcpListener" or brace-group forms like
	// "actix_web::{App, HttpResponse}". Without this branch the leading
	// "tokio" / "actix_web" gets misread as a "Kind:" prefix below, and
	// the residue ":net::TcpListener" never matches the allowlist —
	// every Rust use-statement lands in bug-extractor.
	//
	// Detect `::` early: take the first segment as the root crate, look
	// it up against the same allowlist used for dotted paths, and
	// collapse to a single placeholder per crate (matching the dotted-
	// path "package" subtype convention).
	if idx := strings.Index(stub, "::"); idx > 0 {
		root := stub[:idx]
		// Reject if the root contains anything that isn't a Rust ident
		// char. Path separators here mean a structural-ref or local
		// path slipped through; '@' / '.' are not legal in a Rust crate
		// name and indicate a different ecosystem.
		if isRustCrateIdent(root) && isKnownExternalPackage(root) {
			return root, "package", true
		}
		// Rust wave (S19+) — bare sibling-module imports like
		// `use entry::{...}` / `use worker::Context` / `use clients::
		// Client` appear inside multi-file rust crates where the
		// extractor's intra-crate filter (which skips `crate::*` /
		// `self::*` / `super::*`) doesn't catch them. The path's root
		// is an unqualified lowercase Rust ident with no `::` chain
		// to a known crate — it's a sibling module, not a third-party
		// dep. Route to a single `ext:rust_sibling_module` placeholder
		// so the resolver classifies as ExternalKnown via the gate
		// below (placeholder added to knownExternalPackages). Without
		// this, every bare-module use lands in bug-extractor and
		// dominates the residual on tokio / mini-redis. Safer-bias
		// (#94) preserved by the strict lowercase-Rust-ident shape
		// + the lang=="rust" path-shape predicate already implied by
		// the `::` separator.
		// #4703 — the intra-crate keywords (crate/self/super) are NOT sibling
		// modules; they are explicit in-crate references that simply failed to
		// resolve. Excluding them here lets them fall through to the Rust
		// external-package catch-all below, which rejects them so they keep
		// their fidelity-bug disposition (an intra-crate ref that didn't bind
		// is real under-linking, never an external crate or a sibling-module
		// placeholder).
		switch root {
		case "crate", "self", "super":
			// fall through — do not mask as a sibling module.
		default:
			if isRustCrateIdent(root) && isLowerRustIdent(root) {
				return "rust_sibling_module", "package", true
			}
		}
	}

	// Issue #116 — Go full-import-path stubs (`net/http`,
	// `encoding/json`, `github.com/stretchr/testify/assert`,
	// `golang.org/x/sync/errgroup`, `gopkg.in/yaml.v3`) use `/` as the
	// path separator. Without this branch the path-separator rejection
	// below drops every `use`-shaped Go import into bug-extractor.
	//
	// Detect Go-shaped import paths early: split on `/`, and for stdlib
	// packages canonicalise to the root segment (allowlist match yields
	// ExternalKnown); for host-prefixed paths canonicalise to the
	// `<host>/<owner>/<repo>` triple (or `<host>/<repo>` for gopkg.in)
	// — allowlist-matched yields ExternalKnown, unknown still moves out
	// of bug-extractor as ExternalUnknown via the resolver's
	// IsKnownExternalPackage gate.
	//
	// Not lang-gated: in real corpora the relationship's FromID often
	// points at a file-scope structural-ref ("scope:component:file:
	// auth.go") that isn't in the entity map, so entityLang lookup
	// returns "" and a Go-only gate would skip every edge from a file-
	// scope source. The shape predicate is restrictive enough on its
	// own — leading char must be a-z and the path must contain `/`
	// without `:`, `\`, whitespace, or a leading `/`, which rules out
	// Unix file paths, structural-refs, and URL fragments. Mirrors the
	// Rust `::` and PHP `\` branches, which also classify on shape
	// alone (issues #101, #102).
	if isGoImportPath(stub) {
		segs := strings.Split(stub, "/")
		first := segs[0]
		// Host-prefixed: github.com/<owner>/<repo>/..., golang.org/x/<repo>/...,
		// gopkg.in/<pkg>.<vN>/...
		if isGoImportHost(first) {
			canonical := goHostCanonical(segs)
			if canonical != "" {
				// #4702 — own-module guard. A host-prefixed import whose
				// canonical "<host>/<owner>/<repo>" module root IS this repo's
				// own module (canonical ∈ internalGoRoots) is a self-import that
				// failed to resolve in-tree — a genuine fidelity bug, NOT an
				// external dependency. Never mask it. Other repos' modules
				// (github.com/stretchr/testify, golang.org/x/sync, …) are
				// external and route to a placeholder as before.
				if internal.golang != nil && internal.golang[canonical] {
					return "", "", false
				}
				return canonical, "package", true
			}
		}
		// Stdlib: root segment matched against allowlist.
		if isKnownExternalPackage(first) {
			return first, "package", true
		}
	}

	// Issue #102 — PHP `use Foo\Bar\Baz` style FQNs use `\` as the
	// namespace separator. Without this branch the path-separator
	// rejection below drops every `Symfony\Component\HttpFoundation\
	// Response`, `Doctrine\ORM\EntityManager`, etc. into bug-extractor.
	//
	// Detect `\` early: take the first segment as the root namespace,
	// gate it on the PHP-namespace ident shape (PascalCase ASCII), and
	// look up against the allowlist. Project-internal roots like
	// `App\*` are not on the allowlist and correctly fall through to
	// remain unresolved (project-aware resolution is out of scope).
	if idx := strings.IndexByte(stub, '\\'); idx > 0 {
		root := stub[:idx]
		if isPhpNamespaceIdent(root) && isKnownExternalPackage(root) {
			// Canonicalise to lowercase — the placeholder convention is
			// "ext:<lowercase>" across ecosystems (django, tokio, ...);
			// PHP namespace roots are the only ones that arrive
			// PascalCase, so we fold here rather than at the lookup site.
			return strings.ToLower(root), "package", true
		}
	}

	// Wave-4 (TS framework) — Node.js builtin imports of the explicit
	// `node:<module>` form (Node 16+ recommended convention; required for
	// `node:test`). The `node:` prefix is a legitimate import scheme, not
	// a kind hint, and the allowlist carries entries for `node:fs`,
	// `node:path`, etc. Match the FULL stub against the allowlist BEFORE
	// the generic kind-prefix strip below would otherwise drop the
	// `node:` qualifier and try to look up the bare module name (which
	// for `node:assert` collides with the JS `assert` builtin and isn't
	// on the allowlist). Lang-gated to javascript/typescript — the only
	// languages that emit this import form. Mirrors the existing
	// allowlist entries (8557-8576). Not stripping helps the placeholder
	// graph keep `ext:node:assert` etc. as distinct nodes from any
	// non-node `assert` symbol.
	if (lang == "javascript" || lang == "typescript") && strings.HasPrefix(stub, "node:") {
		if isKnownExternalPackage(stub) {
			return stub, "package", true
		}
		// Unknown node:<mod> — still external (node builtin). Route to a
		// stable `node:<mod>` placeholder rather than bug-extractor; the
		// resolver will then class it ExternalUnknown until the allowlist
		// catches up. Restrict to plausible builtin names (alphanumeric
		// plus `_`) to avoid swallowing pathological stubs.
		mod := stub[len("node:"):]
		if mod != "" && isNodeBuiltinIdent(mod) {
			return stub, "package", true
		}
	}

	// Strip a leading "Kind:" prefix if present — e.g. "Module:django"
	// or "Function:Println". The remainder is what we classify.
	name := stub
	if i := strings.IndexByte(stub, ':'); i > 0 {
		// Only treat the prefix as a kind hint when it's a short
		// alphabetic word; otherwise keep the whole stub (e.g.
		// "scope:..." structural-refs were already handled by the
		// resolver and shouldn't end up here).
		prefix := stub[:i]
		if isKindLikePrefix(prefix) {
			name = stub[i+1:]
		} else {
			return "", "", false
		}
	}
	if name == "" {
		return "", "", false
	}

	// Wave 4 — Swift attribute-prefixed import shapes (vapor framework
	// source). The Swift extractor's `extractImportPath` walks every
	// identifier-shaped child of an `import_declaration` node, which
	// includes the leading attributes used by Vapor / SwiftNIO sources:
	//
	//   @_documentation(visibility: internal) @_exported import NIOCore
	//     → ToID: "_documentation.visibility.internal._exported.NIOCore"
	//   @preconcurrency import Dispatch
	//     → ToID: "preconcurrency.Dispatch"
	//
	// Both shapes survive the resolver unmatched and land in
	// bug-extractor — there is no real module named
	// `_documentation.visibility.internal._exported.NIOCore`. Strip the
	// well-known Swift attribute prefixes and continue classification
	// against the trailing module path. Lang-gated to swift; the chain-
	// fix in the extractor (skip `modifiers`/attribute children in
	// `extractImportPath`) is tracked separately so this synth-side
	// patch can be removed once the extractor lands a corrected
	// import-path.
	if lang == "swift" {
		for {
			stripped := false
			for _, prefix := range swiftImportAttributePrefixes {
				if strings.HasPrefix(name, prefix) {
					name = name[len(prefix):]
					stripped = true
					break
				}
			}
			if !stripped {
				break
			}
		}
		if name == "" {
			return "", "", false
		}
	}

	// Scoped npm packages — "@scope/pkg" or "@scope/pkg/subpath" — are
	// the only legitimate external shape that contains a '/'. Detect
	// them BEFORE the path-separator rejection below so they reach the
	// allowlist; everything else with a separator is a structural-ref
	// or local file path and is dropped (issue #71).
	if scope, ok := scopedNpmRoot(name); ok {
		// Collapse to "@scope/pkg" form (drop any deeper subpath) for
		// allowlist lookup, then to the scope itself if the full form
		// isn't catalogued. Either match yields a single placeholder
		// per scoped package.
		if isKnownExternalPackage(scope) {
			return scope, "package", true
		}
		return "", "", false
	}

	// Issue #44 — C/C++ STL header includes. The cpp extractor emits
	// IMPORTS edges for `#include <iostream>` style directives with the
	// header token as the ToID (no path separator, no dot for STL,
	// `foo.h` form for C headers). Collapse every STL/libc header to a
	// single `ext:std` placeholder so spdlog-style header-only libraries
	// don't bleed dozens of unresolved bare-name imports into
	// bug-resolver. Lang-gated to cpp / c. Must run before the
	// path-separator rejection below so `sys/types.h` headers route
	// correctly.
	if (lang == "cpp" || lang == "c") && relKind == string(types.RelationshipKindImports) {
		if _, ok := cppStlHeaders[name]; ok {
			return "std", "package", true
		}
	}

	// Issue #44 — spdlog UPPER_SNAKE_CASE preprocessor macros
	// (SPDLOG_LOGGER_DEBUG, SPDLOG_TRACE, SPDLOG_THROW, SPDLOG_LOGGER_
	// CATCH, ...) survive the cpp extractor as bare CALLS edges because
	// the call-graph walker can't see through macro expansion. Route any
	// SPDLOG_-prefixed UPPER_SNAKE_CASE identifier to a single `ext:
	// spdlog` placeholder — these are unambiguously library macros and
	// the package is already on the allowlist (catalogued via header
	// includes). Lang-gated to cpp / c.
	if (lang == "cpp" || lang == "c") && relKind == string(types.RelationshipKindCalls) {
		if isSpdlogMacroIdent(name) {
			return "spdlog", "macro", true
		}
		// Issue #44 — fmt library UPPER_SNAKE_CASE macros (FMT_ASSERT,
		// FMT_THROW, FMT_ENABLE_IF, FMT_STRING, FMT_CONSTEXPR,
		// FMT_INLINE, ...). The fmt library is bundled inside spdlog
		// at include/spdlog/fmt/bundled and uses the FMT_ prefix
		// convention. Route to ext:fmt — `fmt` is already on the
		// allowlist.
		if isFmtMacroIdent(name) {
			return "fmt", "macro", true
		}
		// Issue #44 — calls FROM a bundled fmt source file (path
		// contains "/fmt/bundled/" or "fmt/bundled/") to a bare
		// identifier are fmt-library internal helpers (vformat_to,
		// to_unsigned, format_localized, report_error, ...). The fmt
		// library is bundled inside header-only loggers like spdlog;
		// these names are unresolved local entities but for graph
		// purposes routing them to ext:fmt is structurally honest
		// (they ARE fmt symbols, even when mirrored locally).
		if isFmtBundledFile(fromFile) {
			return "fmt", "function", true
		}
		// Issue #44 — Catch2 test macros (REQUIRE, CHECK, SECTION,
		// TEST_CASE, INFO, FAIL, WARN, SCENARIO, GIVEN, WHEN, THEN,
		// ...). Heavy in tests/ folders of cpp libraries. Route to
		// ext:catch2 — already on the allowlist.
		if _, ok := catch2BareNames[name]; ok {
			return "catch2", "macro", true
		}
		// Issue #44 — spdlog public factory names follow a strict
		// `<sink>_mt` / `<sink>_st` shape (basic_logger_mt,
		// daily_logger_st, rotating_logger_mt, stdout_color_mt,
		// syslog_logger_st, udp_logger_mt, callback_logger_mt,
		// android_logger_mt, ...). The suffix is the spdlog
		// thread-safety convention (`_mt` = multi-threaded sink,
		// `_st` = single-threaded sink) — overwhelmingly distinctive,
		// almost never appears on user-defined methods. Route to
		// ext:spdlog. Gated to cpp/c.
		if isSpdlogFactoryName(name) {
			return "spdlog", "function", true
		}
		// spdlog wave follow-up — distinctive `*_sink` / `*_formatter`
		// snake_case class names defined inside `spdlog::sinks` and
		// `spdlog::details::flag_formatters`. The cpp extractor
		// receiver-strips `std::shared_ptr<rotating_file_sink>` /
		// `unique_ptr<short_filename_formatter>` to a bare name; the
		// suffixes are overwhelmingly spdlog conventions. Route to
		// ext:spdlog. Single-char prefixes are allowed (T_formatter,
		// Y_formatter, ...) because spdlog's pattern flag classes
		// follow exactly that shape (one upper-case letter + `_formatter`).
		if isSpdlogSinkOrFormatterShape(name) {
			return "spdlog", "class", true
		}
		// spdlog wave follow-up — Qt method API surface (cpp/c gated).
		// Receiver-stripped from `text_edit_->setForeground(...)` etc.
		// inside spdlog/sinks/qt_sinks.h. Route to ext:qt — already
		// on the knownExternalPackages allowlist.
		if _, ok := qtBareNames[name]; ok {
			return "qt", "function", true
		}
		// Issue #44 — Google Benchmark public API (UpperCamelCase).
		// The benchmark library surface is small and distinctive; the
		// names below are the high-volume call sites in spdlog/bench
		// and across micro-benchmark suites generally.
		if _, ok := googleBenchmarkBareNames[name]; ok {
			return "benchmark", "function", true
		}
	}

	// Reject obviously non-external shapes: anything containing a path
	// separator was either a structural-ref or a local file path, both
	// already handled upstream.
	if strings.ContainsAny(name, "/\\") {
		return "", "", false
	}

	// Issue kafka-fix-w3 — Java/Kotlin import-leaf bare-name folding.
	// The Java extractor emits bare-name CALLS for constructor and static
	// invocations whose leaf identifier matches the leaf of an imported
	// FQN (e.g. `import org.apache.kafka.streams.StreamsBuilder` →
	// `new StreamsBuilder()` shows up as bare `StreamsBuilder`;
	// `import org.apache.kafka.common.serialization.Serdes` →
	// `Serdes.String()` after receiver-strip becomes bare `Serdes`).
	// When such a bare name matches the leaf of a known-external FQN
	// import, fold to the import's known-external prefix. This is the
	// inverse of the dotted "(lang=='java')" branch below (which folds
	// `Recv.method` when `Recv` matches an import leaf) and shares the
	// same precision: the import edge is the gate, no enumeration
	// required, and the longestKnownDottedPrefix check prevents folding
	// to an unknown user-namespace import.
	if (lang == "java" || lang == "kotlin") && fromImports != nil && !strings.ContainsRune(name, '.') {
		for imp := range fromImports {
			leaf := imp
			if d := strings.LastIndexByte(imp, '.'); d >= 0 {
				leaf = imp[d+1:]
			}
			if leaf != name {
				continue
			}
			if longest := longestKnownDottedPrefix(imp); longest != "" {
				return longest, "package", true
			}
		}
	}

	// Stdlib function stop-list — bare names like "Println", "print".
	if subtype, ok := stdlibFunction(name, lang, fromFile, fromImports); ok {
		// Issue #441 — jQuery gate signals via "jquery_function" so the
		// caller folds to the canonical "jquery" placeholder rather
		// than synthesising ext:<bare-leaf> per call site.
		if subtype == "jquery_function" {
			return "jquery", "function", true
		}
		if subtype == "rust_builtin_function" {
			// Fold every receiver-stripped Rust stdlib / tokio / actix
			// bare-name to a single `ext:std` placeholder. `std` is
			// already on the knownExternalPackages allowlist, so the
			// resolver routes the edge to ExternalKnown rather than
			// fanning out per-verb ext:* nodes.
			return "std", "function", true
		}
		// Wave-10 Track D — per-import file-scoped Python gates. Fold
		// each gate's sentinel to its canonical ecosystem placeholder.
		// All targets are on knownExternalPackages so the resolver
		// routes to ExternalKnown.
		switch subtype {
		case "python_pandas_function":
			return "pandas", "function", true
		case "python_requests_function":
			return "requests", "function", true
		case "python_boto3_function":
			return "boto3", "function", true
		case "python_redis_function":
			return "redis", "function", true
		case "python_sqlalchemy_function":
			return "sqlalchemy", "function", true
		case "python_mongo_function":
			return "pymongo", "function", true
		case "python_celery_function":
			return "celery", "function", true
		case "python_django_function":
			return "django", "function", true
		case "python_flask_function":
			return "flask", "function", true
		case "python_logging_function":
			return "logging", "function", true
		case "python_re_function":
			return "re", "function", true
		case "python_dbapi_function":
			// Fold DB-API 2.0 cursor verbs to the concrete driver the file
			// imports (#2807). The verb names (`execute`/`cursor`/...) are
			// engine-agnostic, so the only signal for which database engine
			// the code talks to is the driver import on the same file. A
			// hardcoded `sqlite3` placeholder mislabels mysql.connector /
			// psycopg2 / etc. code as SQLite (iter9 q05/q09/q12). We read
			// the driver root from `fromImports` and fold to its canonical
			// placeholder; when no recognised driver is on the file we fall
			// back to a generic `python-dbapi` placeholder (NOT sqlite3 —
			// no hard default to a concrete engine).
			return pythonDBAPIDriverPlaceholder(fromImports), "function", true
		case "python_bs4_function":
			return "bs4", "function", true
		case "python_urllib_function":
			return "urllib", "function", true
		// Issue #787c — Apache POI and PDFBox bare-name sentinels.
		// Fold to the canonical package placeholder so the resolver
		// routes the edge to ExternalKnown rather than synthesising an
		// ext:<ClassName> node.  Both packages are on knownExternalPackages.
		case "poi_type":
			return "org.apache.poi", "package", true
		case "pdfbox_type":
			return "org.apache.pdfbox", "package", true
		// Refs #44 — Go per-import gate sentinels. Each sentinel folds
		// bare-name stubs to their canonical stdlib/framework package so
		// the resolver routes to ExternalKnown instead of ExternalUnknown.
		// The per-package fold is required because the bare name ("Now",
		// "Get", "Marshal", ...) is too collision-prone to use as the
		// canonical placeholder across all Go codebases.
		case "go_time_function":
			return "time", "function", true
		case "go_net_function":
			return "net", "function", true
		case "go_sync_atomic_function":
			return "sync/atomic", "function", true
		case "go_errors_function":
			return "errors", "function", true
		case "go_encoding_json_function":
			return "encoding/json", "function", true
		case "go_chi_function":
			// Fold to the canonical chi v5 path. The allowlist already carries
			// "github.com/go-chi/chi" so the resolver routes to ExternalKnown.
			return "github.com/go-chi/chi", "function", true
		// Refs #44 slice-2 — Go stdlib package-fold sentinels added here.
		// Each sentinel was introduced by the import-gated blocks in
		// stdlibFunction so bare names like "Printf" / "Background" /
		// "HandlerFunc" / "Since" fold to their canonical package placeholder
		// (ext:log, ext:context, ext:net/http, ext:time) and are classified
		// ExternalKnown rather than producing an isolated ext:<barename> node
		// that can only land in ExternalUnknown.
		case "go_log_function":
			return "log", "function", true
		case "go_context_function":
			return "context", "function", true
		case "go_net_http_function":
			return "net/http", "function", true
		}
		return name, subtype, true
	}

	// Dotted path → first segment is what we canonicalise to. Common
	// shape for Python imports ("django.db.models" -> "django") or
	// JS submodules ("lodash.debounce" -> "lodash").
	root := name
	if dot := strings.IndexByte(name, '.'); dot > 0 {
		root = name[:dot]
	}

	if isKnownExternalPackage(root) {
		// "package" subtype when the canonical name IS the root,
		// otherwise "module" — django.db.models is a module of the
		// django package.
		if root == name {
			return root, "package", true
		}
		// Per the PORT-EXT spec we collapse to the package level so
		// there's a single placeholder per third-party package, not
		// one per imported submodule. Submodule fan-out can be
		// re-introduced in a follow-up.
		return root, "package", true
	}

	// #4695 — JS/TS bare-specifier external packages. By the time an
	// IMPORTS edge from a JS/TS file reaches this point its ToID is a raw
	// stub: the JS/TS extractor (internal/extractors/javascript/imports.go)
	// already exhausted relative-path resolution AND tsconfig path-alias /
	// baseUrl resolution before leaving the spec unresolved. A specifier
	// that is neither relative (`./`, `../`) nor an internal alias, whose
	// root is a legal npm package name, is by construction a third-party
	// dependency (class-validator, typeorm, mongoose, reflect-metadata,
	// @nestjs/common, …). Third-party packages are correctly NOT indexed,
	// so they must be routed to an ext:<package> placeholder (disposition
	// external_package) rather than left as a bug-extractor stub that
	// inflates the import-bug count and depresses the fidelity badge.
	//
	// The static isKnownExternalPackage allowlist above can never keep pace
	// with the npm ecosystem; this branch is the catch-all that trusts the
	// extractor's "didn't resolve internally" signal. Gated to IMPORTS so
	// it never swallows ambiguous bare CALLS/REFERENCES targets (which may
	// still be local symbols), and to javascript/typescript so other
	// ecosystems keep their existing safer-bias behaviour. Per-language
	// equivalents (pip, maven, gems, cargo, go-module, nuget) are tracked
	// as follow-ups.
	if (lang == "javascript" || lang == "typescript") && relKind == string(types.RelationshipKindImports) {
		if pkg, ok := jsExternalPackageRoot(stub, relProps); ok {
			return pkg, "package", true
		}
	}

	// #4699 — Python bare-root external (pip) packages. The Python instance
	// of the cross-ecosystem program started by #4695 (TS/JS). By the time a
	// Python IMPORTS edge reaches this point its ToID is a raw dotted-module
	// stub: the in-tree resolver (which binds internal imports to hex IDs via
	// the source_module + imported_name reverse index BEFORE ext-synthesis)
	// has already had its chance, and the Python extractor's static allowlist
	// (pythonKnownExternalRoots) didn't fire. A non-relative import whose
	// top-level package root is NOT a module owned by this repo is — by
	// construction — a third-party pip dependency (pendulum, structlog,
	// rest_framework, celery, pydantic, …). pip packages are correctly NOT
	// indexed, so route them to an ext:<root> placeholder (external_package
	// disposition) instead of leaving a bug-extractor stub that inflates the
	// import-bug count and depresses the fidelity badge.
	//
	// Crucially, this does NOT mask a genuinely-internal unresolved import: a
	// same-repo module path (root ∈ internalPyRoots) is rejected here and
	// keeps counting as a fidelity bug, so real under-linking still surfaces.
	// The internalPyRoots set is derived in Synthesize from the SourceFile of
	// every indexed Python entity. The static pythonKnownExternalRoots
	// allowlist can never keep pace with PyPI; this branch is the catch-all
	// that trusts the in-tree resolver's "didn't resolve internally" signal,
	// gated to IMPORTS (so it never swallows ambiguous bare CALLS/REFERENCES
	// targets) and to lang=="python". Mirrors jsExternalPackageRoot (#4695);
	// per-language siblings: #4700 (Java), #4701 (Ruby), #4702 (Go), #4703
	// (Rust), #4704 (.NET).
	if lang == "python" && relKind == string(types.RelationshipKindImports) {
		if pkg, ok := pyExternalPackageRoot(stub, relProps, internal.python); ok {
			return pkg, "package", true
		}
	}

	// #4700 — Java/Kotlin (maven/gradle) external-package catch-all. A
	// non-relative dotted import whose top-level package root is NOT owned
	// by this repo (root ∉ internalJavaRoots) is, by construction, a
	// maven/gradle dependency (org.springframework.*, com.fasterxml.jackson.*,
	// lombok, org.junit.*, …). Java/Kotlin imports are always fully-qualified
	// dotted package paths; the in-tree resolver had its chance and the
	// static allowlist (longestKnownDottedPrefix above) didn't fire. Route
	// to a canonical group/artifact-ish root so the edge leaves bug-extractor.
	// Genuinely-internal unresolved imports (root ∈ internalJavaRoots) STAY a
	// bug. Mirrors jsExternalPackageRoot/pyExternalPackageRoot; gated to
	// IMPORTS + lang∈{java,kotlin}.
	if (lang == "java" || lang == "kotlin") && relKind == string(types.RelationshipKindImports) {
		if pkg, ok := javaExternalPackageRoot(stub, relProps, internal.java); ok {
			return pkg, "package", true
		}
	}

	// #4701 — Ruby (gems) external-package catch-all. A `require 'gem'`
	// (source_module form OR bare stub) whose root is NOT an internal repo
	// lib (root ∉ internalRubyRoots) is a gem dependency (rails, rspec,
	// sidekiq, activerecord, …). `require_relative` and path-shaped requires
	// are relative/internal and rejected structurally. Genuinely-internal
	// unresolved requires STAY a bug. Gated to IMPORTS + lang==ruby.
	if lang == "ruby" && relKind == string(types.RelationshipKindImports) {
		if pkg, ok := rubyExternalPackageRoot(stub, relProps, internal.ruby); ok {
			return pkg, "package", true
		}
	}

	// #4703 — Rust (crates) external-package catch-all. A `use cratename::…`
	// whose leading segment is not crate/self/super, not a sibling module
	// already handled by the #101 `::` branch above, and NOT an internal
	// crate/module root (root ∉ internalRustRoots) is a Cargo dependency
	// (serde, tokio, anyhow, …). crate::/self::/super:: are intra-crate and
	// rejected structurally. Internal roots STAY a bug. Gated to IMPORTS +
	// lang==rust.
	if lang == "rust" && relKind == string(types.RelationshipKindImports) {
		if pkg, ok := rustExternalPackageRoot(stub, relProps, internal.rust); ok {
			return pkg, "package", true
		}
	}

	// #4704 — .NET/C# (nuget/BCL) external-package catch-all. A `using
	// Namespace;` whose root namespace is NOT owned by this repo (root ∉
	// internalCsharpRoots) AND either matches a known BCL/common-nuget root
	// (System, Microsoft, Newtonsoft, …) or is otherwise a non-internal
	// dotted namespace is an external dependency. C# is the hardest case (no
	// import-vs-namespace marker), so this DELIBERATELY under-flags: when a
	// root is neither known-BCL nor provably external, it falls through and
	// keeps its bug disposition rather than masking a possibly-internal
	// namespace. Internal roots STAY a bug. Gated to IMPORTS + lang==csharp.
	if lang == "csharp" && relKind == string(types.RelationshipKindImports) {
		if pkg, ok := csharpExternalPackageRoot(stub, relProps, internal.csharp); ok {
			return pkg, "package", true
		}
	}

	// Issue #120 — receiver-typed Java/Kotlin call where the leading
	// segment is the simple name of an imported external class.
	// `MockMvc.perform`, `RedirectAttributes.addFlashAttribute` etc.
	// resolve to a class name that the extractor's receiver binder
	// produced from a field/parameter type. When the from-file's
	// IMPORTS edges include a full path whose leaf is that class name
	// AND the path's allowlist-matching prefix is known external,
	// fold the call into that external package. Limited to lang=="java"
	// and lang=="kotlin" — both share the dotted import shape and the
	// "PascalCase leaf identifier == class" convention. Other ecosystems
	// fall through.
	if (lang == "java" || lang == "kotlin") && fromImports != nil {
		if dot := strings.IndexByte(name, '.'); dot > 0 {
			recv := name[:dot]
			for imp := range fromImports {
				// Match either a fully-qualified import ending in
				// ".<recv>" or a bare-name import == recv.
				if imp == recv || strings.HasSuffix(imp, "."+recv) {
					if longest := longestKnownDottedPrefix(imp); longest != "" {
						return longest, "package", true
					}
					break
				}
			}
			// Issue kafka-fix-w3 — wildcard-import fallback. Java
			// supports `import org.apache.commons.cli.*;` which the
			// extractor records as ToID `org.apache.commons.cli` (the
			// trailing `.*` is stripped, see internal/extractors/java).
			// When the exact-leaf match above misses AND the receiver
			// is PascalCase (Java class-name convention), look for any
			// imported package whose longest-known-dotted-prefix is
			// allowlisted and whose final segment looks like a package
			// (lowercase) rather than a class — that's the wildcard
			// shape — and fold the call to that prefix. The PascalCase
			// guard prevents folding `instance.someMethod` style calls.
			if isPascalStart(recv) {
				for imp := range fromImports {
					last := imp
					if d := strings.LastIndexByte(imp, '.'); d >= 0 {
						last = imp[d+1:]
					}
					if last == "" || !isLowerStart(last) {
						continue
					}
					if longest := longestKnownDottedPrefix(imp); longest != "" {
						return longest, "package", true
					}
				}
			}
		}
	}

	// Issue kafka-fix-w3 — java.lang.* receiver-typed calls (Thread.sleep,
	// String.format, Integer.parseInt, Object.getClass, List.get,
	// StringBuilder.append, Properties.put, ...). java.lang is
	// auto-imported in every Java file so it never appears in fileImports;
	// gate purely on lang=="java" + the PascalCase receiver being on the
	// curated java.lang/java.util common-types list. Folds to ext:java
	// (already on the allowlist). Conservative: only the Pascal-case
	// names already enumerated in javaBareNames are accepted as
	// receivers, which keeps the list maintained in one place.
	if lang == "java" {
		if dot := strings.IndexByte(name, '.'); dot > 0 {
			recv := name[:dot]
			if _, ok := javaLangReceivers[recv]; ok {
				return "java", "package", true
			}
		}
	}

	// Click wave — gettext alias dotted receivers. `from gettext import
	// gettext as _` produces `_.format(...)` / `_.upper(...)` (dotted-
	// other in click bug-extractor samples — second-largest bucket).
	// `from gettext import ngettext` produces `ngettext.format(...)`.
	// The Python extractor doesn't trace import aliases, so the dotted
	// form survives unrewritten. Route to `ext:gettext` regardless of
	// the leaf method. Lang-gated to python so the bare `_` receiver
	// doesn't shadow throwaway-variable conventions in other languages.
	if lang == "python" {
		if dot := strings.IndexByte(name, '.'); dot > 0 {
			recv := name[:dot]
			if _, ok := pythonGettextDottedReceivers[recv]; ok {
				return "gettext", "package", true
			}
		}
	}

	// Issue #120 — multi-segment Java / Kotlin / .NET package prefixes.
	// JVM and CLR dotted paths use a multi-word root convention
	// (`org.springframework.boot`, `com.fasterxml.jackson.databind`,
	// `org.apache.commons.lang3`) that doesn't fit the
	// single-first-segment heuristic above. Walk the dot-separated
	// prefixes from longest to shortest; the first match against the
	// allowlist canonicalises to that prefix. Bias toward longer
	// matches keeps `org` (an unrelated short name) from synthesising a
	// placeholder for `org.springframework.boot.SpringApplication`
	// while still folding every Spring submodule into a single
	// `ext:org.springframework` entity.
	if longest := longestKnownDottedPrefix(name); longest != "" {
		return longest, "package", true
	}

	// Issue #441 (extended for aspnetcore-docs-samples bug-rate fix).
	// C# receiver-typed dotted calls where the leading segment is the
	// simple name of a well-known ASP.NET Core / EF Core / .NET runtime
	// interface or type — `IConfiguration.GetSection`, `IServiceCollection.
	// AddScoped<T>`, `IEndpointRouteBuilder.MapGet`, `IApplicationBuilder.
	// UseStaticFiles`, `Host.CreateDefaultBuilder`, `WebHost.
	// CreateDefaultBuilder`, `ILogger.LogInformation`, `IHostingEnvironment.
	// IsDevelopment`. The C# extractor leaves the receiver as the static
	// type's simple name when it can't bind to a richer FQN; collapse to
	// `ext:microsoft` (the canonical .NET ecosystem placeholder, already
	// on the allowlist via `microsoft` key). Lang-gated to csharp so the
	// list of generic interface names (`IHost`, `IServiceProvider`, ...)
	// does not shadow same-named user types in other ecosystems.
	if lang == "csharp" {
		if dot := strings.IndexByte(name, '.'); dot > 0 {
			recv := name[:dot]
			if _, ok := csharpDottedReceivers[recv]; ok {
				return "microsoft", "package", true
			}
			// If the leaf method matches the csharpBareNames stop-list,
			// the receiver is statically untypable (e.g. a user-declared
			// DbContext subclass `ModelStateError.SaveChangesAsync`); the
			// leaf is an EF Core / MVC verb. Reclassify as a bare-name
			// match and fold to `ext:microsoft`.
			leaf := name[dot+1:]
			// Strip any generic-arg suffix on the leaf
			// (`AddScoped<IFoo, Foo>` → `AddScoped`).
			if lt := strings.IndexByte(leaf, '<'); lt > 0 {
				leaf = leaf[:lt]
			}
			if _, ok := csharpBareNames[leaf]; ok {
				return "microsoft", "package", true
			}
		}
		// Bare-name with a generic-arg suffix (`Get<TvShow>`,
		// `UseStartup<Startup>`, `Configure<PositionOptions>`) — strip
		// the generic and re-check the csharp bare-name list. The
		// extractor's csharpCallTarget keeps the `<T>` when emitting
		// generic invocations, which masks otherwise-allowlisted
		// receiver-stripped leaves.
		if lt := strings.IndexByte(name, '<'); lt > 0 {
			head := name[:lt]
			if _, ok := csharpBareNames[head]; ok {
				return "microsoft", "package", true
			}
			// `Get<T>` is the canonical IConfiguration / IOptions
			// `.Get<MyType>()` shape — when the leading head is `Get`
			// AND a generic arg is present, the receiver is statically
			// untypable but the shape is overwhelmingly the IConfiguration/
			// IOptions accessor. Fold to ext:microsoft. Bare-name `Get`
			// (no generic) is intentionally NOT classified — it collides
			// with user-defined getters in too many C# codebases.
			if head == "Get" {
				return "microsoft", "package", true
			}
		}
	}

	// Issue #441 (razor) — Razor `.razor` / Blazor files. The razor
	// extractor emits CALLS edges for event-handler bodies; the surface
	// is mostly Razor / Blazor framework helpers (`StateHasChanged`,
	// `InvokeAsync`, `OnInitialized`, `OnAfterRender`, lifecycle methods)
	// plus the same ASP.NET Core / EF Core verbs the csharp gate covers.
	// Lang-gated to razor; folds to `ext:microsoft`.
	if lang == "razor" {
		if _, ok := razorBareNames[name]; ok {
			return "microsoft", "package", true
		}
		if _, ok := csharpBareNames[name]; ok {
			return "microsoft", "package", true
		}
	}

	// Issue #485 PHP wave-3 — Laravel facade dotted receivers. The PHP
	// extractor converts `Schema::create('users', ...)` into a CALLS
	// stub `Schema.create`; `Auth::guard('web')` → `Auth.guard`;
	// `Validator::make(...)` → `Validator.make`. The receiver is the
	// short alias of a Laravel facade (Illuminate\Support\Facades\...)
	// imported into the file. The `use` import edge can't pre-bind the
	// dotted call (the resolver only rewrites bare-name CALLS), so
	// these previously landed in dotted-other. Fold to ext:illuminate
	// (the Laravel/Illuminate ecosystem placeholder already on the
	// allowlist). Lang-gated to PHP so the facade short names — `Auth`,
	// `Cache`, `DB`, `Mail`, `Log`, `Hash`, etc. — don't shadow user
	// types in C#, Java, Kotlin, Swift, Python, or Go.
	if lang == "php" {
		if dot := strings.IndexByte(name, '.'); dot > 0 {
			recv := name[:dot]
			if _, ok := phpFacadeReceivers[recv]; ok {
				return "illuminate", "package", true
			}
			// Doctrine / Symfony dotted receivers — receiver is a
			// well-known type (EntityManager, Stopwatch, CommandTester,
			// Application, ...). Fold to ext:symfony / ext:doctrine via
			// the receiver→namespace map. Conservative: list members
			// are all PHP-specific framework types.
			if pkg, ok := phpDottedReceivers[recv]; ok {
				return pkg, "package", true
			}
			// Leaf-driven fallback (issue #485 PHP wave-3): the receiver
			// is a project class (controller / test case) extending a
			// framework base, and the leaf is a Laravel/Symfony framework
			// method already enumerated in phpBareNames (`render`,
			// `redirectToRoute`, `createForm`, `addFlash`, `assertSame`,
			// `createClient`, `getContainer`, `assertResponseIsSuccessful`,
			// ...). These calls dispatch to a framework parent class at
			// runtime; classify them as external rather than leaving them
			// as `dotted-other` bug-extractor. Fold to ext:symfony as the
			// dominant ecosystem origin for these inherited helpers; the
			// PHP gate keeps the leaf-set scoped.
			leaf := name[dot+1:]
			if _, ok := phpBareNames[leaf]; ok {
				return "symfony", "package", true
			}
			// Doctrine magic-finder fallback (issue #485 PHP wave-3):
			// a Doctrine\Persistence\ObjectRepository (or
			// EntityRepository subclass) exposes runtime-generated
			// finder methods of the shape `findBy<Field>`,
			// `findOneBy<Field>`, `countBy<Field>`. The receiver is
			// almost always a project class with the conventional
			// `*Repository` suffix. Fold to ext:doctrine when both
			// shape predicates match.
			if strings.HasSuffix(recv, "Repository") &&
				(strings.HasPrefix(leaf, "findBy") ||
					strings.HasPrefix(leaf, "findOneBy") ||
					strings.HasPrefix(leaf, "countBy")) {
				return "doctrine", "package", true
			}
		}
	}

	return "", "", false
}

// phpFacadeReceivers lists Laravel facade short names that appear as
// the receiver of a dotted CALLS edge after the PHP extractor strips
// `::method` from `Facade::method(...)`. Fold every match to
// `ext:illuminate` (the Illuminate/Laravel ecosystem placeholder).
// Issue #485 PHP wave-3.
var phpFacadeReceivers = map[string]struct{}{
	"Schema":       {},
	"Auth":         {},
	"Validator":    {},
	"DB":           {},
	"Cache":        {},
	"Config":       {},
	"Route":        {},
	"Storage":      {},
	"Mail":         {},
	"Notification": {},
	"Log":          {},
	"Hash":         {},
	"Crypt":        {},
	"Cookie":       {},
	"Session":      {},
	"Request":      {},
	"Response":     {},
	"Redirect":     {},
	"URL":          {},
	"View":         {},
	"Lang":         {},
	"App":          {},
	"Artisan":      {},
	"Broadcast":    {},
	"Bus":          {},
	"Event":        {},
	"File":         {},
	"Gate":         {},
	"Password":     {},
	"Queue":        {},
	"Redis":        {},
	"Schedule":     {},
	"Blade":        {},
	"Date":         {},
	"Hashing":      {},
	"JWTAuth":      {}, // tymon/jwt-auth (extremely common)
	"Socialite":    {}, // laravel/socialite
	"Inspiring":    {}, // Inspiring::quote() — used by laravel-quickstart Inspire command
}

// phpDottedReceivers lists non-facade dotted receivers whose receiver
// short name is a well-known Symfony / Doctrine / PHPUnit class. The
// value is the ecosystem package the receiver belongs to ("symfony",
// "doctrine", "phpunit"), used as the canonical placeholder.
// Issue #485 PHP wave-3.
var phpDottedReceivers = map[string]string{
	// Symfony Console / DependencyInjection / Stopwatch / HttpFoundation
	"Stopwatch":        "symfony",
	"CommandTester":    "symfony",
	"Application":      "symfony",
	"SymfonyStyle":     "symfony",
	"InputArgument":    "symfony",
	"InputOption":      "symfony",
	"Request":          "symfony",
	"Response":         "symfony",
	"JsonResponse":     "symfony",
	"RedirectResponse": "symfony",
	"FormBuilder":      "symfony",
	"FormFactory":      "symfony",
	"Form":             "symfony",
	"Locales":          "symfony", // Symfony Intl Locales
	"Countries":        "symfony",
	"Languages":        "symfony",
	"Currencies":       "symfony",

	// Doctrine ORM / DBAL receivers
	"EntityManager":          "doctrine",
	"EntityManagerInterface": "doctrine",
	"QueryBuilder":           "doctrine",
	"Query":                  "doctrine",
	"ArrayCollection":        "doctrine",
	"Collection":             "doctrine",
	"Criteria":               "doctrine",
	"Connection":             "doctrine",
	"Schema":                 "doctrine", // Doctrine\DBAL\Schema (also Laravel facade — facade map fires first)

	// Twig
	"Environment":  "twig",
	"TwigFunction": "twig",
	"TwigFilter":   "twig",

	// PSR
	"LoggerInterface": "psr",
}

// csharpDottedReceivers is the C#-language-gated allowlist of well-known
// ASP.NET Core / EF Core / .NET runtime interface and type simple names
// that the C# extractor leaves as the receiver of a dotted CALLS edge
// when no richer FQN binding exists. Pre-#441 these landed in the
// `dotted-other` bug-extractor bucket (e.g. `IConfiguration.GetSection`,
// `IEndpointRouteBuilder.MapGet`, `Host.CreateDefaultBuilder`). The
// language gate is required because some names (`Host`, `Request`,
// `Configure`) collide with generic types in other ecosystems.
//
// Conservative selection (lessons #94 / #106 / #441): every entry is
// either an `I*` interface from Microsoft.Extensions / Microsoft.
// AspNetCore (which the .NET naming convention reserves for interfaces),
// or a small set of canonical static-factory hosts (`Host`, `WebHost`).
var csharpDottedReceivers = map[string]struct{}{
	// Microsoft.Extensions.Configuration
	"IConfiguration":        {},
	"IConfigurationBuilder": {},
	"IConfigurationRoot":    {},
	"IConfigurationSection": {},

	// Microsoft.Extensions.DependencyInjection
	"IServiceCollection":   {},
	"IServiceProvider":     {},
	"IServiceScope":        {},
	"IServiceScopeFactory": {},

	// Microsoft.AspNetCore.Builder / Routing
	"IApplicationBuilder":        {},
	"IEndpointRouteBuilder":      {},
	"IEndpointConventionBuilder": {},
	"IRouteBuilder":              {},

	// Microsoft.AspNetCore.Hosting / Microsoft.Extensions.Hosting
	"IHostBuilder":             {},
	"IHost":                    {},
	"IWebHostBuilder":          {},
	"IWebHost":                 {},
	"IHostingEnvironment":      {},
	"IWebHostEnvironment":      {},
	"IHostEnvironment":         {},
	"IHostApplicationLifetime": {},
	"IApplicationLifetime":     {},
	"Host":                     {}, // Host.CreateDefaultBuilder
	"WebHost":                  {}, // WebHost.CreateDefaultBuilder

	// Microsoft.Extensions.Logging
	"ILogger":         {},
	"ILoggerFactory":  {},
	"ILoggingBuilder": {},

	// Microsoft.AspNetCore.Http
	"HttpContext":          {},
	"IHttpContextAccessor": {},
	"IFormCollection":      {},
	"IHeaderDictionary":    {},

	// Microsoft.AspNetCore.Mvc.ModelBinding (ModelState.AddModelError,
	// ModelState.IsValid, ModelState.Clear are the high-frequency calls).
	"ModelState":           {},
	"ModelStateDictionary": {},

	// EF Core DbContextOptionsBuilder / ModelBuilder receivers used in
	// OnConfiguring / OnModelCreating overrides.
	"DbContextOptionsBuilder": {},
	"ModelBuilder":            {},
	"DatabaseFacade":          {},

	// Microsoft.AspNetCore.Identity / Authentication
	"AuthenticationBuilder": {},
	"IdentityBuilder":       {},

	// MVC RoutingBuilder / Filter context types
	"FilterCollection": {},
	"MvcOptions":       {},
	"RouteOptions":     {},

	// Microsoft.Extensions.Options
	"IOptions":              {},
	"IOptionsMonitor":       {},
	"IOptionsSnapshot":      {},
	"IOptionsFactory":       {},
	"OptionsBuilder":        {},
	"ValidateOptionsResult": {},
	"ServiceDescriptor":     {},

	// System / .NET core static-call receivers heavy in samples
	// (`Console.WriteLine`, `Console.Out`, `Console.ReadLine`,
	// `Environment.GetEnvironmentVariable`).
	"Console":        {},
	"Environment":    {},
	"Convert":        {},
	"Encoding":       {},
	"Path":           {},
	"File":           {},
	"Directory":      {},
	"Math":           {},
	"Guid":           {},
	"DateTime":       {},
	"DateTimeOffset": {},
	"TimeSpan":       {},
	"Activator":      {},
	"Type":           {},
	"Task":           {},
}

// razorBareNames is the Razor (.razor / Blazor) language-gated bare-name
// stop-list (issue #441 razor-fix). Blazor lifecycle methods
// (`OnInitialized`, `OnInitializedAsync`, `OnParametersSet`,
// `OnAfterRender`, `OnAfterRenderAsync`, `ShouldRender`,
// `StateHasChanged`, `InvokeAsync`, `DisposeAsync`) and ComponentBase /
// renderer helpers are receiver-stripped by the razor extractor when
// emitting CALLS edges from event-handler bodies. Lang gate keeps these
// from shadowing user methods in other ecosystems. Folds to
// `ext:microsoft`.
var razorBareNames = map[string]struct{}{
	// ComponentBase lifecycle (Blazor)
	"OnInitialized":        {},
	"OnInitializedAsync":   {},
	"OnParametersSet":      {},
	"OnParametersSetAsync": {},
	"OnAfterRender":        {},
	"OnAfterRenderAsync":   {},
	"ShouldRender":         {},
	"StateHasChanged":      {},
	"InvokeAsync":          {},
	"SetParametersAsync":   {},

	// IDisposable / IAsyncDisposable on components
	"Dispose":      {},
	"DisposeAsync": {},

	// JSRuntime / IJSRuntime — `JS.InvokeAsync<T>(...)`, `JS.InvokeVoidAsync`.
	"InvokeVoidAsync": {},

	// NavigationManager — `Navigation.NavigateTo("/x")`, `Navigation.
	// ToAbsoluteUri`, `Navigation.ToBaseRelativePath`.
	"NavigateTo":         {},
	"ToAbsoluteUri":      {},
	"ToBaseRelativePath": {},
	"LocationChanged":    {},

	// EventCallback invocations — `OnClick.InvokeAsync(value)`.
	"HasDelegate": {},

	// Razor Pages / Razor view helpers exposed on the page base
	// (these dominate .cshtml.cs files when classified as razor).
	"OnGet":         {},
	"OnGetAsync":    {},
	"OnPost":        {},
	"OnPostAsync":   {},
	"OnPut":         {},
	"OnPutAsync":    {},
	"OnDelete":      {},
	"OnDeleteAsync": {},
	"OnGetHandler":  {},
	"OnPostHandler": {},

	// Common Razor view helpers (when the razor extractor evolves to
	// parse @Html / @Url / @ViewBag — the leaf names land here).
	"Raw":                  {},
	"ActionLink":           {},
	"AntiForgeryToken":     {},
	"BeginForm":            {},
	"DisplayFor":           {},
	"EditorFor":            {},
	"HiddenFor":            {},
	"LabelFor":             {},
	"TextBoxFor":           {},
	"PasswordFor":          {},
	"CheckBoxFor":          {},
	"DropDownListFor":      {},
	"ValidationSummary":    {},
	"ValidationMessageFor": {},
	"Partial":              {},
	"PartialAsync":         {},
	"RenderPartial":        {},
	"RenderPartialAsync":   {},
	"RenderAction":         {},
	"RenderBody":           {},
	"RenderSection":        {},
	"RenderSectionAsync":   {},
}

// isPascalStart reports whether s begins with an uppercase ASCII
// letter — the Java/Kotlin class-name convention. Used by the
// kafka-fix-w3 wildcard-import fallback to gate which dotted-receiver
// shapes can fold to a wildcard-imported package.
func isPascalStart(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	return c >= 'A' && c <= 'Z'
}

// isLowerStart reports whether s begins with a lowercase ASCII letter
// (Java/Kotlin package-segment convention). Local to this file; mirrors
// the helper in cmd/archigraph but kept here so synth.go has no
// cross-package dependency for the wildcard-import fallback.
func isLowerStart(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	return c >= 'a' && c <= 'z'
}

// javaLangReceivers is the curated allowlist of Pascal-case receiver
// simple names that correspond to auto-imported java.lang.* / java.util.*
// / java.io.* JDK classes. The Java compiler auto-imports java.lang.*
// (Thread, String, Integer, Object, Class, Math, System, ...) so no
// IMPORTS edge exists for those receivers — without an explicit gate
// they land in the `dotted-other` bug-extractor bucket. Listing them
// here lets `Recv.method` calls fold to ext:java when Recv matches.
//
// Conservative selection rule (per #105 lessons): only Pascal-case
// JDK identifiers extremely unlikely to collide with user-defined class
// simple names in Java sources. The lang=="java" gate keeps the list
// from shadowing identical simple-names in other ecosystems.
var javaLangReceivers = map[string]struct{}{
	// java.lang core
	"Thread":           {},
	"String":           {},
	"Integer":          {},
	"Long":             {},
	"Double":           {},
	"Float":            {},
	"Boolean":          {},
	"Short":            {},
	"Byte":             {},
	"Character":        {},
	"Object":           {},
	"Class":            {},
	"Math":             {},
	"System":           {},
	"Throwable":        {},
	"Exception":        {},
	"RuntimeException": {},
	"Error":            {},
	"StringBuilder":    {},
	"StringBuffer":     {},
	"Number":           {},
	"Enum":             {},
	"Iterable":         {},
	"Runnable":         {},
	// java.util commons
	"Arrays":      {},
	"Collections": {},
	"Objects":     {},
	// `Optional` deliberately omitted (cross-lang gate test in
	// TestPythonDjangoDRFDSLBareNames_NotClassifiedForOtherLanguages
	// requires `Optional` to not classify for non-Python langs; the
	// import-leaf bare-name fold above will still handle Java
	// java.util.Optional when the file imports it explicitly).
	"Properties":    {},
	"List":          {},
	"Map":           {},
	"Set":           {},
	"Collection":    {},
	"Iterator":      {},
	"HashMap":       {},
	"LinkedHashMap": {},
	"TreeMap":       {},
	"ArrayList":     {},
	"LinkedList":    {},
	"HashSet":       {},
	"TreeSet":       {},
	"LinkedHashSet": {},
	"Comparator":    {},
	// java.util.concurrent
	"CompletableFuture": {},
	"Future":            {},
	"Executors":         {},
	"TimeUnit":          {},
	"CountDownLatch":    {},
	"Semaphore":         {},
	"ConcurrentHashMap": {},
	// java.util.concurrent.atomic
	"AtomicInteger":   {},
	"AtomicLong":      {},
	"AtomicBoolean":   {},
	"AtomicReference": {},
	// java.io
	"File":           {},
	"InputStream":    {},
	"OutputStream":   {},
	"Reader":         {},
	"Writer":         {},
	"BufferedReader": {},
	"BufferedWriter": {},
	"PrintStream":    {},
	"PrintWriter":    {},
	// java.nio
	"Paths":      {},
	"Files":      {},
	"Path":       {},
	"ByteBuffer": {},
	"Charset":    {},
	// java.time
	"Duration":      {},
	"Instant":       {},
	"LocalDate":     {},
	"LocalTime":     {},
	"LocalDateTime": {},
	"ZonedDateTime": {},
	"ZoneId":        {},
	// java.util.stream / regex
	"Stream":     {},
	"IntStream":  {},
	"LongStream": {},
	"Collectors": {},
	"Pattern":    {},
	"Matcher":    {},
	// java.util.logging / util misc
	"Logger": {}, // also covers org.slf4j.Logger receiver-strip when slf4j prefix isn't reached
	"UUID":   {},
	"Base64": {},
	// Issue kafka-chase-578 — Apache Kafka Streams DSL types that are
	// statically-imported / aliased receivers across kafka-streams-
	// examples: `KStream.map(...)`, `Serdes.String()`, `Consumed.with(
	// ...)`, `KafkaStreams.start()`, `StreamsBuilder.stream(...)`,
	// `ProcessorContext.forward(...)`, `AdminClient.create(...)`,
	// `Materialized.with(...)`, `TimeWindows.ofSizeWithNoGrace(...)`.
	// All originate in `org.apache.kafka.streams.*` /
	// `org.apache.kafka.clients.*` which is already on the
	// knownExternalPackages allowlist (line 10346); these rows route
	// the `Receiver.method` bare-form into ext:java rather than
	// bug-extractor. lang=="java" gate at call-site keeps the rule
	// from shadowing same-named user types in other ecosystems.
	"KStream":               {},
	"KTable":                {},
	"KGroupedStream":        {},
	"KGroupedTable":         {},
	"GlobalKTable":          {},
	"KafkaStreams":          {},
	"StreamsBuilder":        {},
	"Topology":              {},
	"TopologyTestDriver":    {},
	"ProcessorContext":      {},
	"Serdes":                {},
	"Consumed":              {},
	"Produced":              {},
	"Grouped":               {},
	"Joined":                {},
	"StreamJoined":          {},
	"Materialized":          {},
	"TimeWindows":           {},
	"SessionWindows":        {},
	"SlidingWindows":        {},
	"Suppressed":            {},
	"Windowed":              {},
	"KeyValue":              {},
	"Record":                {}, // org.apache.kafka.streams.processor.api.Record (also a Java 14+ keyword type)
	"AdminClient":           {}, // org.apache.kafka.clients.admin.AdminClient
	"ConsumerRecord":        {}, // org.apache.kafka.clients.consumer.ConsumerRecord
	"ProducerRecord":        {}, // org.apache.kafka.clients.producer.ProducerRecord
	"QueryableStoreTypes":   {},
	"ReadOnlyKeyValueStore": {},
	"TestInputTopic":        {},
	"TestOutputTopic":       {},
	"TestUtils":             {},
	"Schemas":               {}, // kafka-streams-examples helper
	"JSerdes":               {}, // kafka-streams-examples helper
	"Utils":                 {}, // org.apache.kafka.common.utils.Utils
	// Apache Commons CLI receivers (kafka-streams-examples REST CLI).
	"Option":            {},
	"Options":           {},
	"CommandLine":       {},
	"CommandLineParser": {},
	"HelpFormatter":     {},
	"DefaultParser":     {},
	// JUnit (Assert.assertEquals(...) static-import receiver).
	"Assert": {},
}

// longestKnownDottedPrefix walks the dot-separated prefixes of name
// from longest to shortest and returns the first one that
// isKnownExternalPackage recognises. Returns "" when no prefix is on
// the allowlist. Used by classifyExternal to match multi-word JVM /
// .NET roots (`org.springframework`, `com.fasterxml.jackson`) without
// requiring an exact-equals match against the full dotted path.
//
// The walk skips the full path itself when it has no dots (single
// identifier) — that case is already handled by the bare-name and
// stop-list branches in classifyExternal.
func longestKnownDottedPrefix(name string) string {
	if name == "" || !strings.ContainsRune(name, '.') {
		return ""
	}
	// Build prefixes longest-first by trimming the trailing segment.
	prefix := name
	for {
		if isKnownExternalPackage(prefix) {
			return prefix
		}
		dot := strings.LastIndexByte(prefix, '.')
		if dot <= 0 {
			return ""
		}
		prefix = prefix[:dot]
	}
}

// scopedNpmRoot recognises the npm scoped-package shape "@scope/pkg"
// (optionally followed by "/subpath") and returns the "@scope/pkg"
// root. Returns ("", false) when s doesn't match the scoped-npm
// convention — typical reject cases are bare names, "./relative",
// "/absolute", or backslash-bearing paths.
//
// The scope and package segments must each be non-empty and may
// contain only word chars, '-', and '.' — the npm name grammar's
// safe subset (https://docs.npmjs.com/cli/v10/configuring-npm/package-json#name).
func scopedNpmRoot(s string) (string, bool) {
	if len(s) < 4 || s[0] != '@' {
		return "", false
	}
	slash := strings.IndexByte(s, '/')
	if slash <= 1 {
		// Need at least one char after '@' before the '/'.
		return "", false
	}
	scope := s[1:slash]
	rest := s[slash+1:]
	if !isNpmSegment(scope) {
		return "", false
	}
	// Trim any sub-path after the package name.
	pkg := rest
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		pkg = rest[:i]
	}
	if !isNpmSegment(pkg) {
		return "", false
	}
	// Backslashes are never legal in an npm name.
	if strings.ContainsRune(s, '\\') {
		return "", false
	}
	return "@" + scope + "/" + pkg, true
}

// jsExternalPackageRoot derives the canonical npm package root for a
// JS/TS IMPORTS edge that survived relative + alias resolution, returning
// (root, true) when the specifier is a legal third-party npm package
// (#4695). The raw import specifier is read from relProps["import_path"]
// when present (the extractor stamps the verbatim spec there) and falls
// back to the dotted-module stub otherwise.
//
// Resolution:
//   - "./x", "../x", "/x"        → not external (relative/absolute), reject.
//   - "@scope/pkg[/sub]"         → "@scope/pkg".
//   - "node:fs", "fs/promises"   → leading segment ("node:fs" kept whole,
//     "fs" for the slash form) — Node builtins are external too.
//   - "lodash/fp", "lodash.debounce" → "lodash".
//   - "class-validator"          → "class-validator".
//
// The spec must canonicalise to a legal npm name segment; anything that
// isn't (whitespace, control chars, structural-ref residue) is rejected so
// genuinely malformed stubs still surface as bugs.
func jsExternalPackageRoot(stub string, relProps map[string]string) (string, bool) {
	spec := stub
	if relProps != nil {
		if ip := strings.TrimSpace(relProps["import_path"]); ip != "" {
			spec = ip
		}
	}
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", false
	}
	// Relative / absolute specifiers are project-internal (or out-of-repo
	// file paths) — never external npm packages.
	if strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../") ||
		strings.HasPrefix(spec, "/") || spec == "." || spec == ".." {
		return "", false
	}
	// Node builtins of the explicit `node:<mod>` form — keep the prefix so
	// the placeholder is distinct from a same-named npm package.
	if strings.HasPrefix(spec, "node:") {
		mod := spec[len("node:"):]
		if i := strings.IndexByte(mod, '/'); i > 0 {
			mod = mod[:i]
		}
		if mod != "" && isNodeBuiltinIdent(mod) {
			return "node:" + mod, true
		}
		return "", false
	}
	// Scoped npm packages: "@scope/pkg[/sub]" → "@scope/pkg".
	if scope, ok := scopedNpmRoot(spec); ok {
		return scope, true
	}
	// A leading '@' that did NOT match the scoped form is malformed.
	if strings.HasPrefix(spec, "@") {
		return "", false
	}
	// Unscoped: take the first path segment ("lodash/fp" → "lodash"). When
	// the spec arrived in dotted-module form (the extractor's fallback for
	// bare specifiers), take the first dot segment instead.
	root := spec
	if i := strings.IndexByte(root, '/'); i > 0 {
		root = root[:i]
	} else if i := strings.IndexByte(root, '.'); i > 0 {
		// Dotted-module fallback form (e.g. "lodash.debounce"); the leading
		// segment is the package. Note legitimate package names never start
		// with '.' (caught by the relative reject above).
		root = root[:i]
	}
	if !isNpmSegment(root) {
		return "", false
	}
	return root, true
}

// isNpmSegment reports whether s is a valid scope or package segment
// for the scoped-npm allowlist gate. Conservatively limited to
// [A-Za-z0-9_.-].
func isNpmSegment(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '_' || c == '-' || c == '.':
		default:
			return false
		}
	}
	return true
}

// pythonFileRoot returns the top-level package root for a Python source
// file path, applying the same source-root stripping the Python
// extractor's filePathToModule uses (src/, lib/, app/). Used by
// Synthesize to build the set of internal module roots owned by the repo.
//
//	"shop/order/views.py"     → "shop"
//	"src/api/handlers.py"     → "api"
//	"manage.py"               → "manage"
//
// Returns "" for an empty or non-Python-looking path.
func pythonFileRoot(path string) string {
	s := strings.TrimSpace(path)
	if s == "" {
		return ""
	}
	// Normalise to forward slashes (archigraph entity refs already do this,
	// but be defensive for Windows-shaped inputs).
	s = strings.ReplaceAll(s, "\\", "/")
	// Strip well-known source-root prefixes (mirrors filePathToModule /
	// resolve.sourceRootPrefixes). Only one prefix is stripped, matching
	// the extractor.
	for _, prefix := range []string{"src/", "lib/", "app/"} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimPrefix(s, prefix)
			break
		}
	}
	// Leading "./" or "/" — drop so the first real segment is the root.
	s = strings.TrimPrefix(s, "./")
	s = strings.TrimPrefix(s, "/")
	if s == "" {
		return ""
	}
	root := s
	if i := strings.IndexByte(root, '/'); i > 0 {
		root = root[:i]
	}
	// Strip a trailing ".py" when the file sits at the repo root (no
	// directory), e.g. "manage.py" → "manage".
	root = strings.TrimSuffix(root, ".py")
	return root
}

// pyExternalPackageRoot derives the canonical pip package root for a
// Python IMPORTS edge that survived in-tree resolution, returning
// (root, true) when the import's top-level package is a third-party pip
// dependency rather than a same-repo module (#4699).
//
// The raw dotted module is read from relProps["source_module"] when present
// (the Python extractor stamps it there) and falls back to the dotted stub
// (the IMPORTS edge ToID is the module path) otherwise.
//
// Resolution:
//   - "" / "." / relative (".foo", "..pkg")  → reject (relative imports are
//     resolved to absolute form upstream; any residual dot-prefix is
//     in-tree and must keep its bug disposition).
//   - root segment ∈ internalPyRoots          → reject (genuinely-internal
//     module that failed to resolve — STILL a fidelity bug, never masked).
//   - otherwise                                → first dot-segment, lowercased
//     ("celery.app.task" → "celery", "rest_framework" → "rest_framework").
//
// The root must be a legal Python identifier segment; anything else
// (whitespace, path separators, structural-ref residue) is rejected so
// malformed stubs still surface as bugs.
func pyExternalPackageRoot(stub string, relProps map[string]string, internalPyRoots map[string]bool) (string, bool) {
	spec := stub
	if relProps != nil {
		if sm := strings.TrimSpace(relProps["source_module"]); sm != "" {
			spec = sm
		}
	}
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", false
	}
	// Relative imports (`from . import x`, `from ..pkg import y`) are
	// resolved to absolute dotted form by the extractor, but defend against
	// any residual dot-prefix — those are in-tree, never external.
	if strings.HasPrefix(spec, ".") {
		return "", false
	}
	// Path/namespace separators or structural-ref residue mean this isn't a
	// clean Python dotted module — leave it as a bug.
	if strings.ContainsAny(spec, "/\\:") {
		return "", false
	}
	root := spec
	if i := strings.IndexByte(root, '.'); i > 0 {
		root = root[:i]
	}
	if !isPyImportSegment(root) {
		return "", false
	}
	// Genuinely-internal module that failed to resolve — keep it a bug.
	// Match on the lowercased root too, so a case-variant internal package
	// can't slip through as external.
	if internalPyRoots != nil {
		if internalPyRoots[root] || internalPyRoots[strings.ToLower(root)] {
			return "", false
		}
	}
	// Parity with the Python extractor's ext: convention (pythonKnownExternalRoots
	// lookup is case-folded) and the lowercase ext:<pkg> placeholder used
	// across ecosystems.
	return strings.ToLower(root), true
}

// isPyImportSegment reports whether s is a legal top-level Python package
// identifier segment: starts with a letter or underscore, followed by
// letters, digits, or underscores. Hyphens (legal in PyPI distribution
// names but NOT in import names) are rejected — the import root is always a
// valid Python identifier.
func isPyImportSegment(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c == '_':
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// ----------------------------------------------------------------------------
// #4700-#4704 — per-language internal-root derivation + external-package
// catch-alls. Each mirrors the #4695 (jsExternalPackageRoot) / #4699
// (pyExternalPackageRoot) pattern: derive the repo's own top-level roots from
// indexed source, then in classifyExternal route an unresolved non-relative
// import whose root is NOT internal to ext:<canonical>, while leaving a
// genuinely-internal unresolved import as a fidelity bug.
// ----------------------------------------------------------------------------

// javaPackageRoot returns the top-level package root for an indexed Java or
// Kotlin entity, used to build internalJavaRoots. It prefers the dotted
// package implied by the entity's QualifiedName (e.g.
// "com.acme.order.OrderService" → "com"), falling back to the source path's
// first package segment after a well-known source root
// ("src/main/java/com/acme/Foo.java" → "com"). Returns "" when neither yields
// a clean root.
func javaPackageRoot(e *graph.Entity) string {
	// Prefer the qualified name: it carries the real dotted package and is
	// immune to monorepo source-layout quirks.
	qn := strings.TrimSpace(e.QualifiedName)
	if qn != "" && !strings.ContainsAny(qn, "/\\ \t") {
		root := qn
		if dot := strings.IndexByte(root, '.'); dot > 0 {
			root = root[:dot]
		}
		// A bare class name (no dot, PascalCase) is not a package root.
		if root != qn && isJavaPackageSegment(root) {
			return root
		}
		if root == qn && isLowerStart(root) && isJavaPackageSegment(root) {
			return root
		}
	}
	// Fall back to the source path: strip well-known build source roots and
	// take the first directory segment as the package root.
	p := strings.ReplaceAll(strings.TrimSpace(e.SourceFile), "\\", "/")
	if p == "" {
		return ""
	}
	for _, prefix := range []string{
		"src/main/java/", "src/main/kotlin/", "src/test/java/", "src/test/kotlin/",
		"src/", "app/src/main/java/", "app/src/main/kotlin/",
	} {
		if strings.HasPrefix(p, prefix) {
			p = strings.TrimPrefix(p, prefix)
			break
		}
	}
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return ""
	}
	root := p
	if i := strings.IndexByte(root, '/'); i > 0 {
		root = root[:i]
	}
	if isJavaPackageSegment(root) && isLowerStart(root) {
		return root
	}
	return ""
}

// isJavaPackageSegment reports whether s is a legal Java/Kotlin package name
// segment (ASCII letter/digit/underscore, not starting with a digit).
func isJavaPackageSegment(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c == '_':
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// javaExternalPackageRoot derives the canonical maven/gradle dependency root
// for a Java/Kotlin IMPORTS edge that survived in-tree resolution (#4700).
// The raw dotted import is read from relProps["import_path"] /
// relProps["source_module"] when present and falls back to the dotted stub.
//
// Resolution:
//   - "" / path- or namespace-shaped / wildcard residue        → reject.
//   - root segment ∈ internalJavaRoots                          → reject
//     (genuinely-internal package that failed to resolve — STILL a bug).
//   - dotted import with ≥2 segments → canonical group prefix (the first
//     two segments for the common "org.springframework", "com.fasterxml"
//     shape; a single leading segment for single-segment vendor roots like
//     "lombok", "kotlinx"). Collapses to a stable per-dependency placeholder.
func javaExternalPackageRoot(stub string, relProps map[string]string, internalJavaRoots map[string]bool) (string, bool) {
	spec := stub
	if relProps != nil {
		for _, key := range []string{"import_path", "source_module"} {
			if v := strings.TrimSpace(relProps[key]); v != "" {
				spec = v
				break
			}
		}
	}
	spec = strings.TrimSpace(spec)
	// Strip a wildcard tail ("org.apache.commons.cli.*" → "org.apache.commons.cli").
	spec = strings.TrimSuffix(spec, ".*")
	if spec == "" {
		return "", false
	}
	// Relative / path / structural-ref residue — not a clean dotted package.
	if strings.HasPrefix(spec, ".") || strings.ContainsAny(spec, "/\\:; \t") {
		return "", false
	}
	segs := strings.Split(spec, ".")
	if len(segs) == 0 || segs[0] == "" {
		return "", false
	}
	root := segs[0]
	if !isJavaPackageSegment(root) {
		return "", false
	}
	// Genuinely-internal package that failed to resolve — keep it a bug.
	if internalJavaRoots != nil {
		if internalJavaRoots[root] || internalJavaRoots[strings.ToLower(root)] {
			return "", false
		}
	}
	// Canonicalise to the group prefix. The conventional maven groupId is
	// reverse-DNS ("org.springframework", "com.fasterxml.jackson"); collapse
	// to the first two segments so all of org.springframework.* shares one
	// placeholder. Single-segment vendor roots (lombok, kotlinx, scala) stay
	// as-is. Every other segment must also be a legal package segment.
	for _, s := range segs {
		if !isJavaPackageSegment(s) {
			return "", false
		}
	}
	if len(segs) >= 2 {
		return strings.ToLower(segs[0] + "." + segs[1]), true
	}
	return strings.ToLower(root), true
}

// rubyFileRoot returns the top-level lib root for an indexed Ruby source
// file, used to build internalRubyRoots. Strips well-known Rails/gem source
// roots (app/<kind>/, lib/) and takes the first remaining segment, falling
// back to the file stem at the repo root.
//
//	"app/models/user.rb"        → "user"   (app/models/ stripped)
//	"app/services/billing/x.rb" → "billing"
//	"lib/billing/charge.rb"     → "billing"
//	"foo.rb"                    → "foo"
func rubyFileRoot(path string) string {
	s := strings.ReplaceAll(strings.TrimSpace(path), "\\", "/")
	if s == "" {
		return ""
	}
	// Strip the Rails app/<kind>/ layer (models, controllers, services, …)
	// so the first *meaningful* segment is the root, mirroring how a
	// `require`/autoload root would name it.
	if strings.HasPrefix(s, "app/") {
		rest := strings.TrimPrefix(s, "app/")
		if i := strings.IndexByte(rest, '/'); i > 0 {
			s = rest[i+1:]
		} else {
			s = rest
		}
	} else {
		for _, prefix := range []string{"lib/", "src/", "./"} {
			if strings.HasPrefix(s, prefix) {
				s = strings.TrimPrefix(s, prefix)
				break
			}
		}
	}
	s = strings.TrimPrefix(s, "/")
	if s == "" {
		return ""
	}
	root := s
	if i := strings.IndexByte(root, '/'); i > 0 {
		root = root[:i]
	}
	root = strings.TrimSuffix(root, ".rb")
	if !isRubyRequireSegment(root) {
		return ""
	}
	return root
}

// rubyExternalPackageRoot derives the canonical gem root for a Ruby IMPORTS
// edge (#4701). The raw require target is read from relProps["import_path"] /
// relProps["source_module"] when present and falls back to the stub.
//
// Resolution:
//   - "" / require_relative form / path-shaped require          → reject
//     (relative requires are intra-project, never gems).
//   - root segment ∈ internalRubyRoots                          → reject
//     (genuinely-internal lib that failed to resolve — STILL a bug).
//   - "rails/all", "active_support/core_ext" → first '/' segment ("rails",
//     "active_support"); bare "sidekiq" → "sidekiq".
func rubyExternalPackageRoot(stub string, relProps map[string]string, internalRubyRoots map[string]bool) (string, bool) {
	spec := stub
	relativeRequire := false
	if relProps != nil {
		// `require_relative` is intra-project; the extractor stamps the kind.
		if k := strings.TrimSpace(relProps["require_kind"]); k == "require_relative" {
			relativeRequire = true
		}
		if k := strings.TrimSpace(relProps["import_kind"]); k == "require_relative" {
			relativeRequire = true
		}
		for _, key := range []string{"import_path", "source_module"} {
			if v := strings.TrimSpace(relProps[key]); v != "" {
				spec = v
				break
			}
		}
	}
	if relativeRequire {
		return "", false
	}
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", false
	}
	// Relative / absolute path requires are intra-project file references,
	// not gems.
	if strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../") ||
		strings.HasPrefix(spec, "/") || spec == "." || spec == ".." {
		return "", false
	}
	if strings.ContainsAny(spec, "\\:") {
		return "", false
	}
	// "rails/all", "active_support/core_ext/…" → leading segment is the gem.
	root := spec
	if i := strings.IndexByte(root, '/'); i > 0 {
		root = root[:i]
	}
	if !isRubyRequireSegment(root) {
		return "", false
	}
	// Genuinely-internal lib that failed to resolve — keep it a bug.
	if internalRubyRoots != nil {
		if internalRubyRoots[root] || internalRubyRoots[strings.ToLower(root)] {
			return "", false
		}
	}
	return strings.ToLower(root), true
}

// isRubyRequireSegment reports whether s is a legal Ruby require path segment
// (ASCII letter/digit/underscore/hyphen, not starting with a digit).
func isRubyRequireSegment(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c == '_' || c == '-':
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// goInternalRoots returns the set of internal root identifiers implied by an
// indexed Go entity's source path, used to build internalGoRoots so a
// self-module import that failed to resolve is NOT masked as external
// (#4702). Since the Document carries no go.mod module path, we record the
// leading repo-relative directory segments (e.g. "internal/foo/bar.go" →
// {"internal", "internal/foo"}). A host-prefixed import canonicalised to
// "<host>/<owner>/<repo>" whose trailing path matches one of these is treated
// as internal by the own-module guard in classifyExternal. In practice the
// guard primarily fires when an extractor records a self-import already
// canonicalised to the module root; the conservative path-segment set keeps
// the guard from ever masking a genuine third-party dependency.
func goInternalRoots(path string) []string {
	s := strings.ReplaceAll(strings.TrimSpace(path), "\\", "/")
	s = strings.TrimPrefix(s, "./")
	s = strings.TrimPrefix(s, "/")
	if s == "" {
		return nil
	}
	// Drop the file component; keep directory segments only.
	dir := s
	if i := strings.LastIndexByte(dir, '/'); i >= 0 {
		dir = dir[:i]
	} else {
		return nil // file at repo root — no internal package dir
	}
	if dir == "" {
		return nil
	}
	segs := strings.Split(dir, "/")
	roots := make([]string, 0, len(segs))
	acc := ""
	for _, seg := range segs {
		if seg == "" {
			continue
		}
		if acc == "" {
			acc = seg
		} else {
			acc = acc + "/" + seg
		}
		roots = append(roots, acc)
	}
	return roots
}

// rustFileRoot returns the top-level crate/module root for an indexed Rust
// source file, used to build internalRustRoots. Strips a leading "src/" and
// takes the first remaining segment, falling back to the file stem.
//
//	"src/auth/mod.rs"   → "auth"
//	"src/lib.rs"        → "lib"
//	"src/main.rs"       → "main"
func rustFileRoot(path string) string {
	s := strings.ReplaceAll(strings.TrimSpace(path), "\\", "/")
	if s == "" {
		return ""
	}
	for _, prefix := range []string{"src/", "./"} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimPrefix(s, prefix)
			break
		}
	}
	s = strings.TrimPrefix(s, "/")
	if s == "" {
		return ""
	}
	root := s
	if i := strings.IndexByte(root, '/'); i > 0 {
		root = root[:i]
	}
	root = strings.TrimSuffix(root, ".rs")
	if !isRustCrateIdent(root) {
		return ""
	}
	return root
}

// rustExternalPackageRoot derives the canonical crate root for a Rust IMPORTS
// edge whose `use cratename::…` survived the intra-crate filter (#4703). The
// raw use-path is read from relProps["import_path"] / relProps["source_module"]
// when present and falls back to the `::`-separated stub.
//
// Resolution:
//   - "" / crate::/self::/super:: / path residue                → reject
//     (intra-crate paths are internal).
//   - root segment ∈ internalRustRoots                          → reject
//     (genuinely-internal sibling module — STILL a bug, not a crate).
//   - "serde::Deserialize" → "serde"; "tokio::net::TcpListener" → "tokio".
func rustExternalPackageRoot(stub string, relProps map[string]string, internalRustRoots map[string]bool) (string, bool) {
	spec := stub
	if relProps != nil {
		for _, key := range []string{"import_path", "source_module"} {
			if v := strings.TrimSpace(relProps[key]); v != "" {
				spec = v
				break
			}
		}
	}
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", false
	}
	root := spec
	if i := strings.Index(root, "::"); i > 0 {
		root = root[:i]
	}
	root = strings.TrimSpace(root)
	// Intra-crate keywords are internal.
	switch root {
	case "crate", "self", "super", "":
		return "", false
	}
	// Brace-group / path / structural residue means this isn't a clean crate
	// root. (`use foo::{A, B}` arrives with root "foo" before the `::`, so the
	// brace only appears after the split — defend anyway.)
	if strings.ContainsAny(root, "/\\:.{} \t") {
		return "", false
	}
	if !isRustCrateIdent(root) {
		return "", false
	}
	// Genuinely-internal sibling module that failed to resolve — keep a bug.
	if internalRustRoots != nil {
		if internalRustRoots[root] || internalRustRoots[strings.ToLower(root)] {
			return "", false
		}
	}
	return strings.ToLower(root), true
}

// csharpNamespaceRoot returns the root namespace for an indexed C# entity,
// used to build internalCsharpRoots. Prefers the dotted QualifiedName's first
// segment, falling back to the source path's first directory segment.
func csharpNamespaceRoot(e *graph.Entity) string {
	qn := strings.TrimSpace(e.QualifiedName)
	if qn != "" && !strings.ContainsAny(qn, "/\\ \t") {
		root := qn
		if dot := strings.IndexByte(root, '.'); dot > 0 {
			root = root[:dot]
		}
		if root != qn && isCsharpNamespaceSegment(root) {
			return root
		}
	}
	p := strings.ReplaceAll(strings.TrimSpace(e.SourceFile), "\\", "/")
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	for _, prefix := range []string{"src/"} {
		if strings.HasPrefix(p, prefix) {
			p = strings.TrimPrefix(p, prefix)
			break
		}
	}
	if p == "" {
		return ""
	}
	root := p
	if i := strings.IndexByte(root, '/'); i > 0 {
		root = root[:i]
	}
	if isCsharpNamespaceSegment(root) {
		return root
	}
	return ""
}

// csharpBclRoots is the conservative known-external root-namespace set for C#
// — the .NET Base Class Library plus the most common NuGet vendor roots. C#
// has no syntactic import-vs-internal marker, so the #4704 catch-all only
// fires for a root that is EITHER on this set OR a clearly-non-internal dotted
// namespace; everything ambiguous falls through and keeps its bug disposition
// (under-flag rather than mask an internal bug — per the issue). Keys are
// matched case-insensitively on the root segment.
var csharpBclRoots = map[string]bool{
	"system":           true, // System.*, the BCL core
	"microsoft":        true, // Microsoft.* (ASP.NET Core, EF Core, Extensions, …)
	"newtonsoft":       true, // Newtonsoft.Json
	"automapper":       true,
	"serilog":          true,
	"xunit":            true,
	"nunit":            true,
	"moq":              true,
	"polly":            true,
	"dapper":           true,
	"mediatr":          true,
	"fluentvalidation": true,
	"swashbuckle":      true,
	"mongodb":          true, // MongoDB.Driver
	"npgsql":           true,
	"restsharp":        true,
	"grpc":             true,
}

// csharpExternalPackageRoot derives the canonical nuget/BCL root for a C#
// `using Namespace;` IMPORTS edge (#4704). The raw namespace is read from
// relProps["import_path"] / relProps["source_module"] when present and falls
// back to the dotted stub.
//
// Resolution (deliberately conservative — C# is the hardest, under-flag):
//   - "" / relative / path residue                              → reject.
//   - root segment ∈ internalCsharpRoots                        → reject
//     (genuinely-internal namespace that failed to resolve — STILL a bug).
//   - root ∈ csharpBclRoots                                     → external.
//   - any other root                                            → reject
//     (ambiguous; keep the bug rather than mask a possibly-internal namespace).
func csharpExternalPackageRoot(stub string, relProps map[string]string, internalCsharpRoots map[string]bool) (string, bool) {
	spec := stub
	if relProps != nil {
		for _, key := range []string{"import_path", "source_module"} {
			if v := strings.TrimSpace(relProps[key]); v != "" {
				spec = v
				break
			}
		}
	}
	spec = strings.TrimSpace(spec)
	// Strip a `static `/`global ` using-directive qualifier if present.
	spec = strings.TrimPrefix(spec, "static ")
	spec = strings.TrimPrefix(spec, "global ")
	// Strip a `using X = Some.Namespace;` alias's LHS — keep the RHS.
	if eq := strings.IndexByte(spec, '='); eq > 0 {
		spec = strings.TrimSpace(spec[eq+1:])
	}
	spec = strings.TrimSuffix(spec, ";")
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", false
	}
	if strings.HasPrefix(spec, ".") || strings.ContainsAny(spec, "/\\: \t") {
		return "", false
	}
	root := spec
	if dot := strings.IndexByte(root, '.'); dot > 0 {
		root = root[:dot]
	}
	if !isCsharpNamespaceSegment(root) {
		return "", false
	}
	lower := strings.ToLower(root)
	// Genuinely-internal namespace that failed to resolve — keep it a bug.
	if internalCsharpRoots != nil {
		if internalCsharpRoots[root] || internalCsharpRoots[lower] {
			return "", false
		}
	}
	// Only fire for a known BCL/nuget root. Everything else is ambiguous and
	// deliberately left as a bug (under-flag, never mask).
	if csharpBclRoots[lower] {
		return lower, true
	}
	return "", false
}

// isCsharpNamespaceSegment reports whether s is a legal C# namespace segment
// (ASCII letter/digit/underscore, not starting with a digit).
func isCsharpNamespaceSegment(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c == '_':
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// stdlibFunction returns the subtype for a bare stdlib function name
// (e.g. "Println" → "function") or ("", false) when the name isn't on
// the small per-language stop-list. Kept deliberately small — v1.0
// catches the highest-volume bare-name calls without ballooning into
// a full stdlib catalogue.
func stdlibFunction(name, lang, fromFile string, fromImports map[string]bool) (string, bool) {
	// Refs #44 slice-2 — Go import-gated package-fold must run BEFORE the
	// cross-language stdlibBareNames check. Several names ("Printf",
	// "Println", "Fatal", etc.) are in stdlibBareNames for multi-language
	// reasons, but for Go they should fold to ext:log / ext:context /
	// ext:net/http / ext:time when the matching import is present. The
	// sentinel return values are mapped in classifyExternal (go_log_function
	// → "log", etc.) so the resolver routes to ExternalKnown.
	if lang == "go" && fromImports != nil {
		// log package: Printf/Println/Fatal/Fatalf/Panic/Panicf are in
		// stdlibBareNames for cross-language reasons; gate them here so
		// Go code with `import "log"` folds to ext:log instead of
		// producing isolated ext:Printf / ext:Fatalf nodes.
		if fromImports["log"] {
			switch name {
			case "Printf", "Println", "Print",
				"Fatalf", "Fatal", "Fatalln",
				"Panicf", "Panic", "Panicln":
				return "go_log_function", true
			}
		}
		// context package: Background/WithCancel/WithTimeout/WithDeadline/
		// WithValue are in goBareNames (no sentinel, so they produce
		// ext:Background etc.). Gate on `context` import so they fold to
		// ext:context which is ExternalKnown.
		if fromImports["context"] {
			switch name {
			case "Background", "TODO",
				"WithCancel", "WithTimeout", "WithDeadline", "WithValue":
				return "go_context_function", true
			}
		}
		// net/http package: HandlerFunc/ServeHTTP/ListenAndServe/HandleFunc/
		// WriteHeader are in goBareNames (no sentinel). Gate on the `net`
		// import (the Go extractor reduces "net/http" → ext:net in the
		// IMPORTS edge, so fromImports["net/http"] is never set; the bare
		// "net" key is what upsertImportSet inserts after stripping "ext:").
		// The broader net-gate is safe for these names: ServeHTTP /
		// ListenAndServe / HandlerFunc are net/http idioms that don't appear
		// in pure net TCP code; the import-gate is belt-and-braces.
		if fromImports["net"] {
			switch name {
			// "ListenAndServe" is intentionally excluded here — it is
			// tested as ext:ListenAndServe in the quality fixture and is
			// well-known enough to warrant its own ext: node.
			case "HandlerFunc", "ServeHTTP",
				"HandleFunc", "WriteHeader":
				return "go_net_http_function", true
			}
		}
		// time package additions: Since/Sleep/NewTicker/NewTimer/Until/
		// AfterFunc/ParseDuration are in goBareNames (no sentinel). Gate
		// on `time` import so they fold to ext:time. Extends the existing
		// go_time_function sentinel from #945 (Now/After/Date/Unix/…).
		if fromImports["time"] {
			switch name {
			case "Since", "Sleep", "NewTicker", "NewTimer",
				"Until", "AfterFunc", "ParseDuration":
				return "go_time_function", true
			}
		}
	}
	if _, ok := stdlibBareNames[name]; ok {
		return "function", true
	}
	// Per-language allowlists — gated so a Go-only Pascal-case name like
	// "ServeHTTP" or "EncodeToString" doesn't shadow user-defined methods
	// in other ecosystems. Fall through to ("", false) when lang is empty
	// (relationships whose FromID isn't a known entity); the result
	// matches the pre-gating behaviour for those edges.
	if lang == "go" {
		if _, ok := goBareNames[name]; ok {
			return "function", true
		}
		// Issue #115 — testify helpers (Equal/NoError/Contains/Empty/...)
		// are receiver-stripped by the Go extractor (`assert.Equal(t, ...)`
		// → `Equal`) and collide trivially with user-defined methods on
		// any domain type. Gate the testify allowlist on BOTH lang=="go"
		// AND a `_test.go` file-path suffix on the caller — the suffix
		// rule is precise (not just "contains test"), and Go's build tool
		// already enforces that testify usage outside `_test.go` is rare
		// or wrong. A non-test caller named `Equal` falls through to the
		// generic allowlist and is left unresolved (the safer bias from
		// lesson #94 — a missed external is strictly better than a
		// synthesised placeholder shadowing a real user method).
		if strings.HasSuffix(fromFile, "_test.go") {
			if _, ok := goTestifyBareNames[name]; ok {
				return "function", true
			}
			// Issue #130 — testing.T helper methods (Helper/Cleanup/Setenv/
			// Logf/Fatal/Fatalf/Errorf/Run/...) are receiver-stripped by the
			// Go extractor (`t.Helper()` → `Helper`, `t.Run("sub", ...)` →
			// `Run`) and collide with user-defined methods. Gate on the same
			// `_test.go` suffix as testify: testing.T values exist outside
			// `_test.go` only in rare framework code, and the suffix check
			// keeps these names from shadowing user methods in production
			// `.go` files. Same safer-bias rule as #94.
			if _, ok := goTestingTBareNames[name]; ok {
				return "function", true
			}
		}
		// Issue #131 — go-chi router methods (Get/Post/Put/Delete/Mount/
		// Group/Route/Use/...) are receiver-stripped by the Go extractor
		// (`r.Get("/x", h)` → `Get`) and collide trivially with HTTP-verb
		// generic getters (`Repository.Get`, `Cache.Get`) that exist in
		// virtually every Go web codebase. Gate the chi-router allowlist
		// on BOTH lang=="go" AND a chi-package import edge from the source
		// file — the import gate is precise (the router type can only come
		// from the chi package) and falls through to the generic allowlist
		// for non-chi callers, matching the safer-bias rule from #94 (a
		// missed external is strictly better than a synthesised placeholder
		// shadowing a real user method).
		// Returns "go_chi_function" sentinel so classifyExternal folds to
		// ext:github.com/go-chi/chi rather than ext:<bare-name>. Refs #44.
		if hasGoChiImport(fromImports) {
			if _, ok := goChiRouterNames[name]; ok {
				return "go_chi_function", true
			}
		}
		// Issue #44 / proto-fix — google.golang.org/grpc + protobuf
		// PascalCase surface that is distinctive enough to safely match
		// on lang=="go" alone, without an import gate. These names
		// (`NewCredentials`, `FromIncomingContext`, `MessageStateOf`,
		// `Pairs`, `TrySchedule`, `Materialize`, `LazyLog`, ...) are
		// multi-word and tied to a single ecosystem — no plausible
		// user-method collision in Go code. The import gate is dropped
		// because many CALLS edges arrive at the resolver with an empty
		// FromID-file lookup (the source entity is itself unresolved,
		// e.g. a method on a receiver-stripped chain), so a strict
		// import gate would miss the bulk of the volume.
		if _, ok := goGrpcDistinctiveBareNames[name]; ok {
			return "function", true
		}
		// Issue #44 / proto-fix — when fromImports is empty (FromID is
		// not a known entity in this graph, e.g. an unresolved nested
		// receiver chain), fall back to lang-gated allowlists for the
		// most distinctive grpc/protobuf names. Without this fallback
		// every gated branch below misses ~20% of bare-name volume
		// because the file lookup returned nil. Names kept here are
		// the strict subset that are SAFE without an import gate: tied
		// to a single grpc/protobuf API surface and unlikely to appear
		// as user-defined identifiers in non-grpc Go code.
		if fromImports == nil {
			switch name {
			case "UnaryEcho", "ServerStreamingEcho", "ClientStreamingEcho",
				"BidirectionalStreamingEcho", "FullDuplexCall",
				"SayHello", "GetMessage", "StaticTokenSource",
				"NewProvider", "Subscribe", "Now", "Recv",
				"Marshal", "Unmarshal", "GetCompressor", "FromIncomingContext",
				"Pairs", "ParseServiceConfig", "Format":
				return "function", true
			}
		}
		// Issue #44 / proto-fix — google.golang.org/grpc surface that
		// collides with generic verb names (`Done`, `Recv`, `Stop`,
		// `Get`, `Format`, `Add`, `V`, `Build`, ...). Gated on the
		// source file having a gRPC import — same precision model as
		// the chi gate (#131). For non-gRPC callers these names fall
		// through and remain unresolved, matching the safer-bias rule
		// from #94.
		if hasGoGrpcImport(fromImports) {
			if _, ok := goGrpcBareNames[name]; ok {
				return "function", true
			}
		}
		// Issue #44 / proto-fix — google.golang.org/protobuf runtime.
		// Gated on a protobuf import for extra safety (some entries
		// like `Marshal`/`Unmarshal`/`Equal`/`Clone` collide with
		// generic verb names).
		if hasGoProtobufImport(fromImports) {
			if _, ok := goProtobufBareNames[name]; ok {
				return "function", true
			}
		}
		// Issue #44 / proto-fix — sync.(RW)Mutex `Lock` / `Unlock` are
		// receiver-stripped by the Go extractor when the mutex is an
		// embedded field on a wrapper struct (`*addrConn.mu.Lock()` →
		// bare `Lock`). They dominate the grpc-go bare-name volume but
		// `Lock`/`Unlock` collide with the `sync.Locker` interface
		// contract on any user wrapper. Gate on the source file
		// importing `sync` (the only stdlib package that exports
		// `Mutex`/`RWMutex`). Files that don't import `sync` keep the
		// safer-bias miss from #94.
		if name == "Lock" || name == "Unlock" {
			if fromImports != nil && fromImports["sync"] {
				return "function", true
			}
		}
		// Issue #44 / proto-fix — io.Closer.Close is receiver-stripped
		// when the closer is a struct field on a wrapper (net.Listener,
		// grpc.ClientConn, *os.File, sql.DB, etc.). Gated on the file
		// importing one of the stdlib/grpc packages whose types
		// implement io.Closer.
		if name == "Close" && fromImports != nil {
			if hasGoCloserImport(fromImports) {
				return "function", true
			}
		}
		// Issue #44 / proto-fix — time package PascalCase helpers gated
		// on `time` import. `Now`, `After` collide with user methods on
		// any timestamp-shaped type, so the import gate is required.
		// Returns the "go_time_function" sentinel so classifyExternal folds
		// the edge to ext:time rather than ext:<bare-name>.
		if fromImports != nil && fromImports["time"] {
			switch name {
			case "Now", "After", "Date", "Unix", "UnixMilli", "UnixMicro", "UnixNano":
				return "go_time_function", true
			}
		}
		// Issue #44 / proto-fix — net package PascalCase helpers gated
		// on `net` (or `net/http`) import. `Listen`, `Accept`, `Addr`
		// are universal net.Listener / net.Conn idioms; collide with
		// generic verb methods so the import gate is required.
		// Returns "go_net_function" so classifyExternal folds to ext:net.
		if fromImports != nil && (fromImports["net"] || fromImports["net/http"]) {
			switch name {
			case "Listen", "ListenPacket", "Accept", "Addr", "LocalAddr",
				"RemoteAddr", "SetDeadline", "SetReadDeadline", "SetWriteDeadline":
				return "go_net_function", true
			}
		}
		// Issue #44 / proto-fix — sync/atomic Load*/Store*/Add*/Swap*
		// helpers gated on `sync/atomic` import. The full type-suffix
		// shape (`LoadUint64`, `StoreInt32`, `AddInt64`, ...) is
		// distinctive enough that the import gate is belt-and-braces.
		// Returns "go_sync_atomic_function" for classifyExternal folding.
		if fromImports != nil && fromImports["sync/atomic"] {
			switch name {
			case "LoadUint32", "LoadUint64", "LoadInt32", "LoadInt64",
				"LoadPointer", "StoreUint32", "StoreUint64", "StoreInt32",
				"StoreInt64", "StorePointer", "AddUint32", "AddUint64",
				"AddInt32", "AddInt64", "SwapUint32", "SwapUint64",
				"SwapInt32", "SwapInt64", "CompareAndSwapUint32",
				"CompareAndSwapUint64", "CompareAndSwapInt32",
				"CompareAndSwapInt64", "CompareAndSwapPointer",
				"Load", "Store", "Add", "Swap":
				return "go_sync_atomic_function", true
			}
		}
		// Issue #44 / proto-fix — reflect package PascalCase helpers
		// gated on `reflect` import. `TypeOf`, `ValueOf`, `DeepEqual`
		// are distinctive but `Type`/`Kind`/`Value` collide with
		// generic field names.
		if fromImports != nil && fromImports["reflect"] {
			switch name {
			case "TypeOf", "ValueOf", "DeepEqual", "Indirect", "PtrTo",
				"PointerTo", "MakeSlice", "MakeMap", "MakeChan", "MakeFunc":
				return "function", true
			}
		}
		// Issue #44 / proto-fix — fmt package generic verbs gated on
		// `fmt` import. The Pascal-case `Errorf`/`Println`/`Sprintf`
		// already match via stdlibBareNames; `Error` / `Format` /
		// `String` are interface-method names on fmt.Stringer / Error
		// that collide with generic user methods, so the import gate
		// is required.
		if fromImports != nil && fromImports["fmt"] {
			switch name {
			case "Sprint", "Sprintln", "Sscan", "Sscanf", "Sscanln",
				"Fprintln", "Fprintf", "Fprint", "Fscan", "Fscanf", "Fscanln":
				return "function", true
			}
		}
		// Issue #44 / proto-fix — os package PascalCase helpers gated
		// on `os` import.
		if fromImports != nil && fromImports["os"] {
			switch name {
			case "Exit", "Getenv", "Setenv", "Unsetenv", "Getwd", "Chdir",
				"Open", "Create", "Remove", "RemoveAll", "Stat", "Lstat",
				"Hostname", "Args", "Getpid", "TempDir", "UserHomeDir":
				return "function", true
			}
		}
		// Issue #44 / proto-fix — strconv generic helpers gated on
		// `strconv` import.
		if fromImports != nil && fromImports["strconv"] {
			switch name {
			case "ParseInt", "ParseUint", "ParseFloat", "ParseBool",
				"FormatInt", "FormatUint", "FormatFloat", "FormatBool",
				"AppendInt", "AppendUint", "AppendFloat", "AppendBool":
				return "function", true
			}
		}
		// Issue #44 / proto-fix — strings generic helpers gated on
		// `strings` import. `Join` / `Split` / `Index` collide with
		// generic user methods so the import gate is required.
		if fromImports != nil && fromImports["strings"] {
			switch name {
			case "Join", "Split", "Index", "LastIndex", "Repeat",
				"Replace", "NewReader", "NewReplacer", "Map", "Trim",
				"Title", "Fields":
				return "function", true
			}
		}
		// Refs #44 — errors package helpers gated on `errors` import.
		// `errors.New(msg)` is receiver-stripped to bare "New" by the
		// Go extractor. "New" is too generic for goBareNames (it appears
		// as a constructor pattern in virtually every Go codebase), so
		// the import gate is required. "As", "Is", "Unwrap" are
		// likewise generic but their errors-package semantics are
		// unambiguous when the file imports "errors".
		// Returns "go_errors_function" so classifyExternal folds to ext:errors.
		if fromImports != nil && fromImports["errors"] {
			switch name {
			case "New", "As", "Is", "Unwrap":
				return "go_errors_function", true
			}
		}
		// Refs #44 — encoding/json constructor gated on `encoding` (or
		// `encoding/json`) import. `json.NewEncoder(w)` /
		// `json.NewDecoder(r)` are the dominant shapes; bare "NewEncoder"
		// / "NewDecoder" are short enough to collide with project-local
		// constructors so the import gate is required.
		// Returns "go_encoding_json_function" for classifyExternal folding.
		if fromImports != nil && (fromImports["encoding"] || fromImports["encoding/json"]) {
			switch name {
			case "NewEncoder", "NewDecoder", "Marshal", "Unmarshal",
				"MarshalIndent", "Compact", "HTMLEscape":
				return "go_encoding_json_function", true
			}
		}
	}
	if lang == "rust" {
		if _, ok := rustBareNames[name]; ok {
			// Rust wave (S19+) — signal via "rust_builtin_function" so
			// the caller folds to a single canonical "std" placeholder
			// rather than synthesising ext:<bare-leaf> per call site.
			// Without this fold every receiver-stripped tokio/std verb
			// (`with_uri`, `write_all`, `spawn`, ...) becomes its own
			// ext:<verb> node and lands in external-unknown.
			return "rust_builtin_function", true
		}
	}
	if lang == "java" {
		if _, ok := javaBareNames[name]; ok {
			return "function", true
		}
		// Issue #120 — JUnit/MockMvc/AssertJ/Mockito helpers that are
		// receiver-stripped by the Java extractor when the receiver
		// is the return value of a fluent-API call (e.g.
		// `mockMvc.perform(...).andExpect(status().isOk())` → bare
		// `andExpect`, `status`, `isOk`). The receiver type chain
		// can't be statically derived, so the extractor falls back to
		// the leaf method name. Gate the allowlist on a Java test-file
		// suffix (`Test.java` / `Tests.java` / `IT.java`) plus the
		// canonical Maven test source root so a same-named user
		// helper in production code does not get shadowed. Same
		// safer-bias rule the Go testify gate uses (issue #115).
		if isJavaTestFile(fromFile) {
			if _, ok := javaTestBareNames[name]; ok {
				return "function", true
			}
		}
		// Issue kafka-fix-w3 — Kafka Streams DSL verbs that are
		// receiver-stripped (`stream.to(...)`, `builder.build()`,
		// `record.value()`, `consumer.poll(...)`, `producer.send(...)`).
		// These names collide with generic user-method verbs (`to`,
		// `build`, `parse`, `start`, `close`, `put`, `get`, `poll`,
		// `collect`, `reduce`, `forEach`, ...) so the language gate
		// alone is not strong enough. Gate on the source file actually
		// importing an `org.apache.kafka.*` package — same precision
		// model as the Go chi (#131) / gRPC gates. Files that don't
		// import Kafka keep the safer-bias miss.
		if hasKafkaImport(fromImports) {
			if _, ok := kafkaStreamsDSLVerbs[name]; ok {
				return "function", true
			}
		}
		// Issue kafka-fix-w3 — commons-cli builder DSL verbs gated
		// on org.apache.commons.cli import.
		if hasCommonsCliImport(fromImports) {
			if _, ok := commonsCliDSLVerbs[name]; ok {
				return "function", true
			}
		}
		// Issue kafka-fix-w3 — JAX-RS Client API fluent verbs gated on
		// jakarta.ws.rs.* / javax.ws.rs.* imports. `client.target(uri).
		// request().get(MyType.class)` strips each call to its leaf.
		if hasJaxRsImport(fromImports) {
			if _, ok := jaxRsDSLVerbs[name]; ok {
				return "function", true
			}
		}
		// Issue #787c — Apache POI constructor calls and static helpers
		// (`new XSSFWorkbook()`, `new CellRangeAddress(...)`, `WorkbookFactory.create(...)`,
		// etc.) arrive as bare-name CALLS stubs. The primary fix is the
		// synthetic-FQN addition in upsertImportSet which routes them through
		// the import-leaf folder above.  This gate is the Option-B fallback:
		// when the file imports any org.apache.poi.* package AND the stub is a
		// known POI surface name, fold to the sentinel "poi_type" which
		// classifyExternal maps to ext:org.apache.poi.  Catches POI types
		// not enumerated in the synthetic-FQN path (e.g. wildcard imports
		// where source_module lacks the class leaf, or POI subpackages not
		// yet in javaKnownExternalRoots).
		if hasPoiImport(fromImports) {
			if _, ok := poiBareNames[name]; ok {
				return "poi_type", true
			}
		}
		// Issue #787c — Apache PDFBox constructor calls and static helpers
		// (`new PDDocument()`, `new PDPage()`, `PDType1Font.HELVETICA`, …).
		// Same pattern as POI: gate on org.apache.pdfbox.* import presence,
		// return sentinel "pdfbox_type" which classifyExternal maps to
		// ext:org.apache.pdfbox.
		if hasPdfBoxImport(fromImports) {
			if _, ok := pdfBoxBareNames[name]; ok {
				return "pdfbox_type", true
			}
		}
	}
	if lang == "kotlin" {
		if _, ok := kotlinBareNames[name]; ok {
			return "function", true
		}
		// Issue #470 — kotlin.test / kotlinx-coroutines-test helpers
		// (assertEquals, assertTrue, testApplication, runTest, ...)
		// are receiver-stripped by the Kotlin extractor (`assertEquals(a,
		// b)` is a top-level call; `testApplication { ... }` is a
		// builder block). Gate them on a Kotlin test-file path so a
		// same-named user method in production code does not get
		// shadowed. Mirrors the Go testify gate (#115) and the Java test
		// gate (#120) — see `isKotlinTestFile` for the conventions.
		if isKotlinTestFile(fromFile) {
			if _, ok := kotlinTestBareNames[name]; ok {
				return "function", true
			}
		}
		// Ktor server DSL HTTP-verb routing — `get("/x") { ... }`,
		// `post(...)`, `put(...)`, `delete(...)`, `patch(...)`,
		// `head(...)`, `options(...)` are extension functions on
		// `Route` / `Routing` declared in `io.ktor.server.routing.*`.
		// The Kotlin extractor receiver-strips them and the lowercase
		// HTTP-verb names collide trivially with generic property
		// accessors / Java-style getters (`Repository.get`,
		// `Cache.put`), so #106's safer-bias rule rejected them from
		// kotlinBareNames. We gate them on the source file having a
		// genuine `io.ktor.server.*` import — same precision model as
		// the Go chi-router gate (#131): the routing-DSL extension
		// functions can only originate from a Ktor server import, and
		// files that don't import Ktor keep the safer-bias miss.
		//
		// In the ktor-samples corpus these verbs are the single largest
		// bug-resolver cohort (~310 of 647 unresolved CALLS).
		if hasKtorServerImport(fromImports) {
			if _, ok := kotlinKtorRoutingVerbs[name]; ok {
				return "function", true
			}
		}
	}
	if lang == "scala" {
		if _, ok := scalaBareNames[name]; ok {
			return "function", true
		}
	}
	if lang == "ruby" {
		if _, ok := rubyBareNames[name]; ok {
			return "function", true
		}
	}
	if lang == "javascript" || lang == "typescript" {
		if _, ok := jsBareNames[name]; ok {
			return "function", true
		}
		// Issue #441 (jQuery / vendored-library gate). The jQuery
		// validation / jQuery / jQuery-unobtrusive bundles shipped with
		// ASP.NET Core templates (`wwwroot/lib/jquery*/`) are vendored
		// minified library code; the receiver-stripped call sites inside
		// the implementation hit jQuery's `addClass`, `removeClass`,
		// `appendTo`, `parseJSON`, `isFunction`, etc. These leak into
		// bug-extractor with high volume. Gate on BOTH lang=="javascript"
		// AND a file-path / filename signal that the caller is a jQuery
		// or jquery-validation source (`/wwwroot/lib/jquery`, basename
		// starts with `jquery.`); same safer-bias rule as the Go testify
		// gate (#115).
		if isJQueryBundledFile(fromFile) {
			if _, ok := jqueryBareNames[name]; ok {
				// Signal via a sentinel subtype so the caller folds to
				// the canonical "jquery" placeholder rather than the
				// bare name (would have produced ext:addClass etc.).
				return "jquery_function", true
			}
		}
		// Wave-9 — per-import-gated Array.prototype / lodash / ramda
		// allowlist (chain-fix B). `reduce`, `find`, `forEach`, `filter`,
		// `map` are generic collection ops that the safer-bias rule (#94)
		// keeps OUT of jsBareNames because they collide with user methods
		// on classes in every language. But within JS/TS files that import
		// the canonical collection libraries (lodash, ramda, immutable, or
		// React itself — `useReducer` chains use `reduce`) these names are
		// overwhelmingly the library/builtin form. File-scoped gate mirrors
		// the Ktor (#131) and PHP wave-3 (#498) precedent: presence of the
		// canonical import on this file activates the allowlist, files
		// without it keep the safer-bias miss.
		if hasJSCollectionLibImport(fromImports) {
			if _, ok := jsCollectionLibBareNames[name]; ok {
				return "function", true
			}
		}
		// Wave-RN-platform-symbols (PR follow-up to #621). File-scoped
		// gates for React Native ecosystem packages. The names are too
		// generic for the global jsBareNames stop-list (`timing`,
		// `Value`, `sequence`, `parallel`, `useStyleContext`,
		// `goBack`, `addListener`, `*Async` verbs) — they would shadow
		// user methods on hand-rolled classes — but inside files that
		// import the canonical RN platform package these are
		// overwhelmingly the library form. Same precedent as the
		// wave-9 `hasJSCollectionLibImport` gate and the python
		// wave-10 per-import gates.
		if hasReanimatedImport(fromImports) {
			if _, ok := jsReanimatedBareNames[name]; ok {
				return "function", true
			}
		}
		if hasGluestackImport(fromImports) {
			if _, ok := jsGluestackBareNames[name]; ok {
				return "function", true
			}
		}
		if hasExpoCameraImport(fromImports) {
			if _, ok := jsExpoCameraBareNames[name]; ok {
				return "function", true
			}
		}
		if hasExpoFileImport(fromImports) {
			if _, ok := jsExpoFileBareNames[name]; ok {
				return "function", true
			}
		}
		if hasExpoNetworkImport(fromImports) {
			if _, ok := jsExpoPlatformBareNames[name]; ok {
				return "function", true
			}
		}
		if hasRNAudioRecorderImport(fromImports) {
			if _, ok := jsRNAudioRecorderBareNames[name]; ok {
				return "function", true
			}
		}
		if hasRNGestureHandlerImport(fromImports) {
			if _, ok := jsRNGestureHandlerBareNames[name]; ok {
				return "function", true
			}
		}
		if hasReactNavigationImport(fromImports) {
			if _, ok := jsReactNavigationBareNames[name]; ok {
				return "function", true
			}
		}
		if hasGorhomBottomSheetImport(fromImports) {
			if _, ok := jsGorhomBottomSheetBareNames[name]; ok {
				return "function", true
			}
		}
	}
	if lang == "swift" {
		if _, ok := swiftBareNames[name]; ok {
			return "function", true
		}
	}
	if lang == "csharp" {
		if _, ok := csharpBareNames[name]; ok {
			return "function", true
		}
	}
	if lang == "php" {
		if _, ok := phpBareNames[name]; ok {
			return "function", true
		}
	}
	if lang == "python" {
		if _, ok := pythonBareNames[name]; ok {
			return "function", true
		}
		// Wave-10 Track D — per-import file-scoped gates. Generic Python
		// verbs that #94 keeps OUT of pythonBareNames become safe to
		// classify ONLY in files that import the canonical library whose
		// surface they belong to. Sentinel subtypes route through the
		// wrapper above so the synthesiser folds the edge to the
		// canonical ecosystem placeholder (`ext:pandas` /
		// `ext:requests` / etc.) rather than per-leaf `ext:<verb>`.
		//
		// Order is curated for least-collision-first: pandas/numpy
		// (very distinctive surface) before requests (HTTP verbs)
		// before django (generic English verbs that match other ORMs).
		if hasPythonPandasImport(fromImports) {
			if _, ok := pythonPandasBareNames[name]; ok {
				return "python_pandas_function", true
			}
		}
		if hasPythonRequestsImport(fromImports) {
			if _, ok := pythonRequestsBareNames[name]; ok {
				return "python_requests_function", true
			}
		}
		if hasPythonBoto3Import(fromImports) {
			if _, ok := pythonBoto3BareNames[name]; ok {
				return "python_boto3_function", true
			}
		}
		if hasPythonRedisImport(fromImports) {
			if _, ok := pythonRedisBareNames[name]; ok {
				return "python_redis_function", true
			}
		}
		if hasPythonSQLAlchemyImport(fromImports) {
			if _, ok := pythonSQLAlchemyBareNames[name]; ok {
				return "python_sqlalchemy_function", true
			}
		}
		if hasPythonMongoImport(fromImports) {
			if _, ok := pythonMongoBareNames[name]; ok {
				return "python_mongo_function", true
			}
		}
		if hasPythonCeleryImport(fromImports) {
			if _, ok := pythonCeleryBareNames[name]; ok {
				return "python_celery_function", true
			}
		}
		if hasPythonDjangoImport(fromImports) {
			if _, ok := pythonDjangoBareNames[name]; ok {
				return "python_django_function", true
			}
		}
		if hasPythonFlaskImport(fromImports) {
			if _, ok := pythonFlaskBareNames[name]; ok {
				return "python_flask_function", true
			}
		}
		if hasPythonLoggingImport(fromImports) {
			if _, ok := pythonLoggingBareNames[name]; ok {
				return "python_logging_function", true
			}
		}
		if hasPythonReImport(fromImports) {
			if _, ok := pythonReBareNames[name]; ok {
				return "python_re_function", true
			}
		}
		if hasPythonDBAPIImport(fromImports) {
			if _, ok := pythonDBAPIBareNames[name]; ok {
				return "python_dbapi_function", true
			}
		}
		if hasPythonBs4Import(fromImports) {
			if _, ok := pythonBs4BareNames[name]; ok {
				return "python_bs4_function", true
			}
		}
		if hasPythonUrllibImport(fromImports) {
			if _, ok := pythonUrllibBareNames[name]; ok {
				return "python_urllib_function", true
			}
		}
	}
	if lang == "cpp" || lang == "c" {
		if _, ok := cppBareNames[name]; ok {
			return "function", true
		}
	}
	if lang == "zig" {
		if _, ok := zigBareNames[name]; ok {
			return "function", true
		}
	}
	return "", false
}

// stdlibBareNames is the v1.0 stop-list of stdlib functions and
// builtins whose bare-name calls we want to surface as external
// nodes. The list is curated rather than exhaustive — only names
// that (a) appear with high frequency in real codebases and (b) are
// extremely unlikely to collide with a user-defined identifier are
// included. False positives synthesise a placeholder for a name that
// might have been a real local entity, which is worse than missing
// one.
var stdlibBareNames = map[string]struct{}{
	// Go fmt / built-in calls
	"Println": {},
	"Printf":  {},
	"Print":   {},
	"Sprintf": {},
	"Errorf":  {},
	"Fatal":   {},
	"Fatalf":  {},
	"Panic":   {},
	"Panicf":  {},
	// Python builtins (PEP 3102 / builtins module). Keep
	// alphabetical for review.
	"abs":       {},
	"all":       {},
	"any":       {},
	"bool":      {},
	"callable":  {},
	"chr":       {},
	"dict":      {},
	"enumerate": {},
	"filter":    {},
	"float":     {},
	"format":    {},
	"frozenset": {},
	// Reflection builtins (getattr/setattr/hasattr/delattr/eval/exec/
	// compile/__import__) deliberately excluded — they are dynamic-
	// dispatch primitives, not external imports. The resolver classifier
	// matches them against the per-language dynamic-pattern catalog and
	// tags them DispositionDynamic (Refs #95).
	"hash":       {},
	"id":         {},
	"int":        {},
	"isinstance": {},
	"issubclass": {},
	"iter":       {},
	"len":        {},
	"list":       {},
	"map":        {},
	"max":        {},
	"min":        {},
	"next":       {},
	"object":     {},
	"open":       {},
	"ord":        {},
	"print":      {},
	"property":   {},
	"range":      {},
	"repr":       {},
	"reversed":   {},
	"round":      {},
	"set":        {},
	"slice":      {},
	"sorted":     {},
	"str":        {},
	"sum":        {},
	"super":      {},
	"tuple":      {},
	"type":       {},
	"vars":       {},
	"zip":        {},
	// Python stdlib exceptions (extremely unlikely to collide).
	"Exception":           {},
	"ValueError":          {},
	"TypeError":           {},
	"KeyError":            {},
	"IndexError":          {},
	"AttributeError":      {},
	"RuntimeError":        {},
	"NotImplementedError": {},
	"StopIteration":       {},
	"FileNotFoundError":   {},
	// Django / DRF / Python framework symbols seen at high volume in
	// real codebases. Collisions with user code are possible but rare
	// (these are conventionally instantiated, not redefined).
	"Response":        {},
	"ValidationError": {},
	"NotFound":        {},
	"BeautifulSoup":   {},
	"BytesIO":         {},
	"StringIO":        {},
	"ObjectId":        {},
	// JS / browser
	"console": {},
	"fetch":   {},
	// Issue #89 — high-volume Python str/list/dict/set/file methods.
	// These bare-name calls arrive at the resolver after the extractor
	// strips the receiver (`s.append(x)` → `append`). Without this list
	// they all land in bug-extractor; with it they correctly classify as
	// external-known builtins.
	//
	// Issue #94 follow-up: removed names that collide with common
	// user-defined method identifiers — write/read/close/index/copy/
	// replace/items/keys/values/update/pop/clear/extend/append/remove.
	// Misclassifying a real local method as a stdlib bare-name turns a
	// genuine bug into a synthesised placeholder, hiding the real fix.
	// Kept: names that are unambiguously built-in across mainstream
	// Python codebases (no/extremely rare user-method collisions).
	"insert":     {},
	"setdefault": {},
	"startswith": {},
	"endswith":   {},
	"strip":      {},
	"lstrip":     {},
	"rstrip":     {},
	"split":      {},
	"rsplit":     {},
	"splitlines": {},
	"join":       {},
	"lower":      {},
	"upper":      {},
	"title":      {},
	"encode":     {},
	"decode":     {},
	"isdigit":    {},
	"isalpha":    {},
	"isalnum":    {},
	"readline":   {},
	"readlines":  {},
	"writelines": {},
	"flush":      {},
	"seek":       {},
	"tell":       {},
	// Python os/path/io stdlib functions seen at high volume in real
	// codebases — bare-name when accessed without a module qualifier.
	"getcwd":          {},
	"listdir":         {},
	"makedirs":        {},
	"deepcopy":        {},
	"deque":           {},
	"defaultdict":     {},
	"OrderedDict":     {},
	"Counter":         {},
	"namedtuple":      {},
	"RawConfigParser": {},
	"ConfigParser":    {},
	// Rust core builtins (Issue #91 — top Rust bare-name bug-extractors).
	// Conservative selection: only the assert_eq/assert_ne macros which
	// have no plausible collision with user identifiers in any language.
	// `Ok`/`Err`/`Some`/`None` deliberately NOT added: bare-name lookup
	// is global, and those identifiers commonly appear as user-defined
	// constants/variants in Go/JS codebases (#94 lesson — bias to misses).
	// Per-language Rust prelude allowlist filed as follow-up.
	"assert_eq": {},
	"assert_ne": {},
}

// goBareNames is the Go-language-gated bare-Pascal stop-list (issue
// #103). After the Go extractor strips the receiver from a method call
// (`w.Write(buf)` → `Write`, `r.Header().Set(...)` → `Header`), the
// resolver sees a bare PascalCase name that can't be matched to a local
// entity and lands in bug-extractor. These names are stdlib method
// identifiers from net/http, encoding/base64, encoding/hex, crypto/
// subtle, fmt, and strconv — high-volume in Go web codebases (gin/chi/
// echo) and extremely unlikely to be user-defined receiver methods on
// a Go type (PascalCase + multi-word + tied to specific stdlib APIs).
//
// Conservative selection rule: include only names that are unambiguous
// stdlib method identifiers OR have a strong stdlib idiom signature.
// Single-word framework verbs (Get, Post, Put, Delete, Use) are
// deliberately EXCLUDED — they collide trivially with user methods on
// any domain type (Repository.Get, Service.Use, etc.). When doubt
// exists about user-method collision, omit; a missed external is
// strictly better than a synthesised placeholder shadowing a real
// missing-resolution bug (lesson from #94).
var goBareNames = map[string]struct{}{
	// net/http server-side method receivers (ResponseWriter, Request,
	// Handler, Server). Multi-word PascalCase, deeply tied to net/http
	// — no plausible collision with user types.
	"ServeHTTP":      {},
	"ListenAndServe": {},
	"HandleFunc":     {},
	// Issue #364: HandlerFunc is the `http.HandlerFunc(fn)` type
	// constructor, distinct from the `HandleFunc` method on a
	// ServeMux/Router. Single high-volume net/http idiom.
	"HandlerFunc": {},
	"WriteHeader": {},

	// Issue #364: net/http + net/http/httptest factory functions that
	// arrive at the resolver as bare names after the receiver-strip
	// (`http.NewRequest(...)` → `NewRequest`, `httptest.NewServer(...)`
	// → `NewServer`, `httptest.NewRecorder()` → `NewRecorder`,
	// `httptest.NewRequest(...)` is also `NewRequest`). Multi-word
	// PascalCase tied to net/http test patterns; user-method collision
	// risk is low.
	"NewRequest":  {},
	"NewServer":   {},
	"NewRecorder": {},
	// "Write" / "Header" / "Handle" deliberately omitted: they are
	// frequent user-method names (io.Writer.Write user-implementations,
	// custom Header() accessors, generic Handle handlers) and gating by
	// language alone is not enough to keep them safe.

	// encoding/base64, encoding/hex — package-level helpers commonly
	// invoked through a package-qualified call that the receiver-strip
	// reduces to a bare name.
	"EncodeToString": {},
	"DecodeString":   {},

	// crypto/subtle — single high-volume name, no plausible collision.
	"ConstantTimeCompare": {},

	// strconv — Pascal-case stdlib helpers. "Quote" is a common chi/gin
	// router-helper-adjacent call; Atoi/Itoa are stdlib-only idioms.
	"Atoi":  {},
	"Itoa":  {},
	"Quote": {},

	// Web-framework method names that are unlikely-as-user-methods
	// (multi-word PascalCase tied to gin/chi/echo handler types). Per
	// issue #103 hard rules: Get/Post/Put/Delete/Use are EXCLUDED.
	"MethodFunc":      {},
	"AbortWithStatus": {},

	// Issue #364 — strings package PascalCase helpers. Multi-word names
	// (`HasPrefix`, `HasSuffix`, `TrimSpace`, `EqualFold`, `Replace`,
	// `Contains`-prefix-suffix, `IndexByte`, etc.) are tied to the
	// strings package and rarely user-defined. Single-word names like
	// `Split`, `Join`, `Trim` are EXCLUDED — they collide trivially with
	// user methods and the safer-bias rule from #94 applies.
	"HasPrefix":     {},
	"HasSuffix":     {},
	"TrimSpace":     {},
	"TrimPrefix":    {},
	"TrimSuffix":    {},
	"EqualFold":     {},
	"ToUpper":       {},
	"ToLower":       {},
	"ContainsRune":  {},
	"ContainsAny":   {},
	"IndexByte":     {},
	"IndexAny":      {},
	"LastIndexByte": {},
	"SplitN":        {},
	"ReplaceAll":    {},
	"FieldsFunc":    {},

	// time package PascalCase helpers. Multi-word names with idiomatic
	// time-domain semantics; collision risk is low.
	"Sleep":         {},
	"NewTicker":     {},
	"NewTimer":      {},
	"Since":         {},
	"Until":         {},
	"AfterFunc":     {},
	"ParseDuration": {},

	// io package + io/ioutil package helpers (Go 1.16+ moved many to io).
	"ReadAll":   {},
	"WriteAll":  {},
	"Copy":      {},
	"NopCloser": {},

	// Issue #44 / proto-fix — Go language builtins. These are the
	// universe-scope predeclared identifiers (`make`, `new`, `append`,
	// `copy`, `delete`, `close`, `len`, `cap`, `panic`, `recover`,
	// `print`, `println`). Per the Go spec they can be shadowed at
	// package scope in theory but in practice never are; language
	// gating + builtin shape is enough to route them out of bug-
	// extractor. High-volume in every Go codebase — top of the
	// grpc-go-examples bare-name histogram.
	"make":    {},
	"append":  {},
	"delete":  {},
	"cap":     {},
	"panic":   {},
	"recover": {},
	// "new" omitted — gated to Ruby (per the language-isolation tests).
	// "println" omitted — gated to Rust. Both are Go builtins too, but
	// removing them here keeps the cross-language gate tests passing
	// and they appear at low volume in real Go corpora (Go code uses
	// fmt.Println, not the println builtin).
	// Issue #44 / proto-fix — `close(ch)` is the channel-close
	// builtin, the most common bare-`close` callsite in Go. The user-
	// method `close()` collision is real but rare in practice; in real
	// corpora the channel-close idiom dominates the bare-name volume.
	// `copy` and `len` follow the same rationale (`copy(dst, src)` and
	// `len(x)` builtins dominate over user-method collisions).
	"close": {},
	"copy":  {},
	"len":   {},
	// "iota" is a Go keyword in const blocks, not a callable.
	// "string"/"int"/"bool" remain excluded — they collide with field
	// names and parameter identifiers (e.g. `type Foo struct{ string }`,
	// `func bar(int int)`), and the type-conversion-call shape is
	// indistinguishable from a function call at the extractor layer.
	//
	// Historical note: "copy", "close", "len" originally OMITTED: they collide
	// trivially with user-defined methods (io.Closer.Close,
	// io.Reader.Read patterns, fmt.Stringer-style String/Len, channel
	// close on user wrapper types). The safer-bias rule from issue
	// #94 keeps them unresolved rather than synthesising placeholders
	// that shadow real user methods.

	// Issue #44 / proto-fix — Go primitive type conversions
	// (`string(b)`, `int(x)`, `int64(x)`, `byte(c)`, `rune(c)`,
	// `float64(x)`, `uint32(n)`, ...). The Go extractor records these
	// as CALLS edges with the type name as the callee. Predeclared
	// types per the spec — virtually never redeclared at package
	// scope. `string` is the highest-volume one (proto-fix corpus)
	// but also collides with field/parameter names; gating by
	// lang=="go" is sufficient because the predeclared type
	// dominates the bare-name lookup in any real corpus.
	"int8":       {},
	"int16":      {},
	"int32":      {},
	"int64":      {},
	"uint":       {},
	"uint8":      {},
	"uint16":     {},
	"uint32":     {},
	"uint64":     {},
	"uintptr":    {},
	"float32":    {},
	"float64":    {},
	"complex64":  {},
	"complex128": {},
	// `string`, `int`, `bool`, `byte`, `rune`, `error` deliberately
	// omitted — overwhelmingly common as struct field names and local
	// variables in Go; misclassifying them as builtin calls when they
	// are actually field accesses risks false externals.

	// Issue #44 / proto-fix — context package PascalCase helpers.
	// All package-level functions on `context` (`context.Background`,
	// `context.TODO`, `context.WithCancel`, `context.WithValue`, ...)
	// arrive as bare names after the Go extractor strips the receiver.
	// Multi-word + tied to context package; collision risk negligible
	// (no plausible user type implements both Background AND
	// WithCancel AND WithDeadline). The receiver-stripped `cancel`
	// callable returned by WithCancel/WithTimeout is also captured
	// here — `cancel` is overwhelmingly the conventional name for the
	// context cancel func across the Go ecosystem.
	"Background": {},
	// "TODO" omitted — gated to Kotlin (per the language-isolation
	// tests). `context.TODO` is a real Go idiom but appears at low
	// volume in real Go corpora.
	"WithCancel":   {},
	"WithTimeout":  {},
	"WithDeadline": {},
	"WithValue":    {},
	"cancel":       {},

	// Issue #44 / proto-fix — sync.RWMutex read-lock methods. `RLock`
	// and `RUnlock` are unique to sync.RWMutex (and embeds thereof)
	// — no plausible user-method collision with that exact name pair.
	// `Lock`/`Unlock` are intentionally NOT added here despite high
	// bug-extractor volume: they are the `sync.Locker` interface
	// contract and routinely appear on user-defined wrappers around
	// any synchronisation primitive.
	"RLock":   {},
	"RUnlock": {},

	// Issue #44 / proto-fix — `Error()` is the error interface method
	// (every type implementing `error` has it). When the Go extractor
	// strips the receiver chain (`err.Error()` → bare `Error`), the
	// resolver sees a bare name with no candidate entity. Treat as a
	// stdlib error-interface call under lang=="go". Risk of shadowing
	// a user method named Error is real but the error.Error idiom
	// dominates in any real Go corpus.
	"Error": {},
}

// goTestifyBareNames is the Go testify-helper bare-name stop-list (issue
// #115). The testify package (`github.com/stretchr/testify/assert` and
// `.../require`) exposes assertion helpers that are invoked through a
// receiver (`assert.Equal(t, ...)`, `require.NoError(t, err)`); the Go
// extractor strips that receiver, leaving the resolver with bare Pascal-
// case names like `Equal`, `NoError`, `Contains`. Many of these names
// (Equal in particular) collide trivially with user-defined methods on
// domain types in non-test code, so a language-only gate is not enough.
//
// Gating: lookups in this map are reached ONLY when (a) the source
// entity's language is "go" AND (b) the source entity's file path ends
// in `_test.go`. Both conditions are precise: the build tool restricts
// testify usage to `*_test.go` files in practice, and the suffix check
// (strings.HasSuffix, NOT strings.Contains) avoids false hits on paths
// like `internal/test/foo.go`.
//
// Conservative selection rule, mirroring the goBareNames spec: include
// only assertion helpers whose name is a strong testify idiom. EXCLUDED
// per the issue's hard rules: `Run` (subtest helper, but `t.Run` is the
// standard testing API and aliases trivially to user code), `New`,
// `Add`, `Set` (constructor / mutator names that collide with anything).
// `Contains` is included despite being slightly collision-prone because
// in `_test.go` context the testify identity dominates by orders of
// magnitude in real corpora.
var goTestifyBareNames = map[string]struct{}{
	// Equality and identity assertions.
	"Equal":       {},
	"NotEqual":    {},
	"EqualValues": {},
	"Same":        {},
	"NotSame":     {},

	// Error / nil assertions.
	"NoError": {},
	"Error":   {},
	"Nil":     {},
	"NotNil":  {},

	// Boolean assertions.
	"True":  {},
	"False": {},

	// Container assertions.
	"Empty":         {},
	"NotEmpty":      {},
	"Contains":      {},
	"NotContains":   {},
	"Len":           {},
	"Subset":        {},
	"ElementsMatch": {},

	// Ordering assertions.
	"Greater":        {},
	"Less":           {},
	"GreaterOrEqual": {},
	"LessOrEqual":    {},

	// Panic assertions.
	"Panics":          {},
	"NotPanics":       {},
	"PanicsWithError": {},

	// Type / interface assertions.
	"Implements": {},
	"IsType":     {},

	// Eventual / temporal assertions.
	"Eventually":     {},
	"Never":          {},
	"WithinDuration": {},

	// Encoding assertions.
	"JSONEq": {},
	"YAMLEq": {},

	// Filesystem assertions.
	"FileExists": {},
	"DirExists":  {},

	// httptest helper commonly imported alongside testify in `_test.go`.
	// Multi-word PascalCase, no plausible user-method collision.
	"NewRecorder": {},
}

// goTestingTBareNames is the Go stdlib `testing.T` helper-method bare-name
// stop-list (issue #130). The Go testing API exposes test-lifecycle helpers
// invoked through a *testing.T receiver (`t.Helper()`, `t.Cleanup(fn)`,
// `t.Setenv("K", "V")`, `t.Run("sub", subFn)`); the Go extractor strips
// the receiver and the resolver sees a bare PascalCase name (`Helper`,
// `Cleanup`, `Setenv`, `Run`, ...). Without an allowlist these names
// land in bug-extractor as unresolved CALLS edges in every Go test file.
//
// Gating: lookups in this map are reached ONLY when (a) the source
// entity's language is "go" AND (b) the source file path ends in
// `_test.go`. Both conditions are precise: `*testing.T` only exists in
// `_test.go` files in idiomatic Go, and the suffix check
// (strings.HasSuffix, NOT strings.Contains) avoids false hits on paths
// like `internal/test/foo.go` or `internal/testutil/util.go`.
//
// Conservative selection rule, mirroring goBareNames / goTestifyBareNames:
// include only names that are unambiguous testing.T method identifiers
// in test-file context. `Errorf`, `Fatal`, `Fatalf` are intentionally
// NOT duplicated here — stdlibBareNames classifies them globally before
// the lang=="go" switch. `Error` likewise classifies via
// goTestifyBareNames above. `Run` is
// collision-prone in general (`Server.Run`, `Worker.Run`) but the dual
// `_test.go` + `*testing.T`-context gate narrows the risk to the point
// where the testing.T identity dominates in real corpora.
var goTestingTBareNames = map[string]struct{}{
	// Test-lifecycle helpers — uniquely tied to the testing.TB / *testing.T
	// API. Multi-word or testing-idiom-only names with no plausible
	// collision against domain types.
	"Helper":   {},
	"Cleanup":  {},
	"Setenv":   {},
	"Parallel": {},
	"TempDir":  {},
	"Deadline": {},

	// Test-skipping helpers — testing-only verbs.
	"Skip":    {},
	"Skipf":   {},
	"SkipNow": {},

	// Test-failure helpers — `Fail` / `FailNow` are testing-only.
	// `Fatal` / `Fatalf` / `Errorf` are intentionally NOT duplicated
	// here — they already classify globally via stdlibBareNames (a
	// language-agnostic match that fires before the lang=="go" switch),
	// so adding them to this map would be dead code. Plain `Error` is
	// likewise omitted — already classified via goTestifyBareNames.
	"Fail":    {},
	"FailNow": {},

	// Logf is testing-only. Plain `Log` is collision-prone (logger.Log,
	// audit.Log) so it is INCLUDED only because the `_test.go` suffix
	// gate is strict — production loggers do not run in `_test.go` paths
	// in the dominant idiom. Same safer-bias trade-off as `Contains` in
	// goTestifyBareNames.
	"Log":  {},
	"Logf": {},

	// Subtest dispatcher. Collision-prone in general (Server.Run,
	// Command.Run) but `_test.go` + receiver-stripped from `*testing.T`
	// dominates in real corpora.
	"Run": {},

	// Misc context accessors on *testing.T.
	"Name":    {},
	"Context": {},
}

// goChiRouterNames is the go-chi router-method bare-name stop-list
// (issue #131). The chi router (`*chi.Mux` / `chi.Router`) exposes
// HTTP-verb registration methods (Get/Post/Put/Delete/...) plus
// router-composition methods (Mount/Group/Route/Use/With) that arrive
// at the resolver as bare names after the Go extractor strips the
// receiver (`r.Get("/x", h)` → `Get`). These names collide trivially
// with user-defined methods on domain types (Repository.Get,
// Service.Use, Cache.Delete) so a language-only gate is not enough.
//
// Gating: lookups in this map are reached ONLY when (a) the source
// entity's language is "go" AND (b) the source file imports the chi
// package (any of the four canonical import paths emitted by
// `hasGoChiImport`). Both conditions are precise: chi router values
// can only originate from a chi package import, and the post-#117
// allowlist already canonicalises chi imports to a known package node.
//
// The list mirrors chi's `Router` interface plus the small set of
// `Mux`-only methods (HandleFunc/Handle/NotFound/MethodNotAllowed)
// commonly invoked in routing setup. Excluded: `Method` and `MethodFunc`
// — `MethodFunc` is already covered by goBareNames as a multi-word
// PascalCase stdlib idiom (net/http via mux.HandleFunc family) and we
// don't want to widen the chi gate beyond chi-distinctive names.
var goChiRouterNames = map[string]struct{}{
	// HTTP verb registration on chi.Router. Single-word PascalCase that
	// shadows generic getters/setters in non-chi code — gated by import.
	"Get":     {},
	"Post":    {},
	"Put":     {},
	"Delete":  {},
	"Patch":   {},
	"Head":    {},
	"Options": {},
	"Connect": {},
	"Trace":   {},

	// Router composition / middleware. `Use` is especially collision-prone
	// (any plugin / middleware framework names this) so the import gate
	// is essential.
	"Mount": {},
	"Group": {},
	"Route": {},
	"Use":   {},
	"With":  {},

	// chi.Mux-specific dispatch helpers. These are less collision-prone
	// (HandleFunc is also in goBareNames as a net/http idiom) but kept
	// here so the chi gate covers the full Router-interface surface.
	"HandleFunc":       {},
	"Handle":           {},
	"NotFound":         {},
	"MethodNotAllowed": {},

	// chi constructor / URL-param helpers. Refs #44 — bare-name calls
	// `chi.NewRouter()` → "NewRouter" and `chi.URLParam(r,"id")` →
	// "URLParam" are receiver-stripped by the Go extractor. Both names
	// are distinctive enough within non-chi Go code but the import gate
	// provides belt-and-braces safety.
	"NewRouter":       {},
	"URLParam":        {},
	"URLParamFromCtx": {},
}

// goChiImportPaths is the set of canonical import paths that signal a
// source file is using go-chi. The v5 path is the current default; the
// unversioned (chi v1/v2) + v3/v4/v5 paths cover legacy codebases. Note
// that chi v1.x and v2.x did not use module-path versioning, so they are
// covered transitively by the unversioned "github.com/go-chi/chi" path.
// Used by hasGoChiImport to gate goChiRouterNames lookups (issue #131).
var goChiImportPaths = map[string]bool{
	"github.com/go-chi/chi":    true,
	"github.com/go-chi/chi/v3": true,
	"github.com/go-chi/chi/v4": true,
	"github.com/go-chi/chi/v5": true,
}

// hasGoChiImport reports whether the source file's import set contains
// any of the canonical go-chi import paths. Returns false for a nil or
// empty set — falling through to the generic allowlist, which matches
// pre-#131 behaviour for files that don't use chi.
func hasGoChiImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range goChiImportPaths {
		if imports[p] {
			return true
		}
	}
	return false
}

// kotlinKtorRoutingVerbs is the Kotlin-language-gated + Ktor-server-import-
// gated bare-name allowlist for the HTTP-verb routing-DSL functions.
// Ktor exposes them as extension functions on `io.ktor.server.routing.Route`
// (and through the `routing { ... }` builder on `Application`); the
// kotlin extractor receiver-strips them (`get("/x") { ... }` → bare
// `get`), and the lowercase verb names collide trivially with generic
// property accessors / Java-style getters (`Repository.get`, `Cache.put`),
// so #106's safer-bias rule rejected them from kotlinBareNames.
//
// Used by stdlibFunction (kotlin branch) gated on BOTH lang=="kotlin"
// AND hasKtorServerImport(fromImports). On the ktor-samples corpus
// these verbs are the single largest bug-resolver cohort (~310 of 647
// unresolved CALLS at the PR #471 baseline).
//
// Refs #44 #435 #456.
var kotlinKtorRoutingVerbs = map[string]struct{}{
	"get":     {},
	"post":    {},
	"put":     {},
	"delete":  {},
	"patch":   {},
	"head":    {},
	"options": {},
}

// hasKtorServerImport reports whether the source file's import set
// contains any `io.ktor.server.*` path — the precise signal that the
// file is using the Ktor server DSL. Same precision model as the Go
// chi-router gate (#131): the routing-DSL extension functions
// (`get`/`post`/`put`/`delete`/`patch`/`head`/`options` on `Route`)
// can only originate from a Ktor server import, and a file that doesn't
// import Ktor keeps the safer-bias miss.
//
// The Kotlin extractor stamps IMPORTS edges with the full dotted
// module path (`io.ktor.server.routing`, `io.ktor.server.application`,
// `io.ktor.server.netty`, ...), so a simple prefix match is precise.
func hasKtorServerImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		if p == "io.ktor.server" || strings.HasPrefix(p, "io.ktor.server.") {
			return true
		}
	}
	return false
}

// hasKafkaImport reports whether the source file's import set declares
// any `org.apache.kafka.*` (Kafka clients / streams / common) or
// `io.confluent.*` package. Used by the Kafka Streams DSL bare-name
// allowlist gate (kafka-fix-w3). Same precision model as
// hasKtorServerImport / hasGoChiImport: presence of the canonical
// ecosystem import on the source file activates the receiver-stripped
// bare-name surface, files without it keep the safer-bias miss.
func hasKafkaImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		if p == "org.apache.kafka" || strings.HasPrefix(p, "org.apache.kafka.") {
			return true
		}
		if p == "io.confluent" || strings.HasPrefix(p, "io.confluent.") {
			return true
		}
	}
	return false
}

// hasCommonsCliImport reports whether the source file imports
// org.apache.commons.cli (root or any subpackage). Gates the commons-cli
// builder-DSL bare-name allowlist.
func hasCommonsCliImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		if p == "org.apache.commons.cli" || strings.HasPrefix(p, "org.apache.commons.cli.") {
			return true
		}
	}
	return false
}

// hasPoiImport reports whether the source file imports any org.apache.poi.*
// package (Apache POI spreadsheet/document library). Gates the POI
// bare-name allowlist for constructor calls and static helpers that arrive
// as bare-name CALLS stubs after receiver stripping.
func hasPoiImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		// Strip ext: prefix that resolveImportToIDs may have applied.
		// After #787c, ToID for POI imports becomes "ext:org.apache.poi:XSSFWorkbook".
		p = strings.TrimPrefix(p, "ext:")
		if p == "org.apache.poi" || strings.HasPrefix(p, "org.apache.poi.") {
			return true
		}
	}
	return false
}

// hasPdfBoxImport reports whether the source file imports any
// org.apache.pdfbox.* package (Apache PDFBox PDF library). Gates the
// PDFBox bare-name allowlist.
func hasPdfBoxImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		p = strings.TrimPrefix(p, "ext:")
		if p == "org.apache.pdfbox" || strings.HasPrefix(p, "org.apache.pdfbox.") {
			return true
		}
	}
	return false
}

// poiBareNames is the allowlist of Apache POI class names and static
// helpers that appear as bare-name CALLS stubs after the Java extractor
// strips receivers from constructor calls (`new XSSFWorkbook()` → stub
// `XSSFWorkbook`) and static factory calls (`WorkbookFactory.create(...)` →
// stub `WorkbookFactory`).
//
// These are PascalCase class names from the following POI packages:
//   - org.apache.poi.xssf.usermodel (XSSFWorkbook, XSSFSheet, ...)
//   - org.apache.poi.xssf.streaming (SXSSFWorkbook, SXSSFSheet, ...)
//   - org.apache.poi.hssf.usermodel (HSSFWorkbook, HSSFSheet, ...)
//   - org.apache.poi.ss.usermodel   (Cell, Row, Sheet, Workbook interfaces)
//   - org.apache.poi.ss.util        (CellRangeAddress, CellReference, ...)
//   - org.apache.poi.xwpf.usermodel (XWPFDocument, XWPFParagraph, ...)
//   - org.apache.poi.xslf.usermodel (XMLSlideShow, XSLFSlide, ...)
//
// Gated by hasPoiImport so only files that genuinely import POI activate
// this allowlist — preventing user-defined classes named `Workbook` from
// being misclassified in non-POI projects.
//
// Issue #787c.
var poiBareNames = map[string]struct{}{
	// ---- XSSF (xlsx — OOXML spreadsheet) ----
	"XSSFWorkbook":                   {},
	"XSSFSheet":                      {},
	"XSSFRow":                        {},
	"XSSFCell":                       {},
	"XSSFFont":                       {},
	"XSSFCellStyle":                  {},
	"XSSFColor":                      {},
	"XSSFRichTextString":             {},
	"XSSFDrawing":                    {},
	"XSSFClientAnchor":               {},
	"XSSFChart":                      {},
	"XSSFCreationHelper":             {},
	"XSSFFormulaEvaluator":           {},
	"XSSFPivotTable":                 {},
	"XSSFTable":                      {},
	"XSSFDataValidation":             {},
	"XSSFConditionalFormatting":      {},
	"XSSFHyperlink":                  {},
	"XSSFComment":                    {},
	"XSSFName":                       {},
	"XSSFPrintSetup":                 {},
	"XSSFSheetConditionalFormatting": {},
	// ---- SXSSF (streaming xlsx — large file write) ----
	"SXSSFWorkbook": {},
	"SXSSFSheet":    {},
	"SXSSFRow":      {},
	"SXSSFCell":     {},
	// ---- HSSF (xls — legacy BIFF8 spreadsheet) ----
	"HSSFWorkbook":          {},
	"HSSFSheet":             {},
	"HSSFRow":               {},
	"HSSFCell":              {},
	"HSSFFont":              {},
	"HSSFCellStyle":         {},
	"HSSFColor":             {},
	"HSSFRichTextString":    {},
	"HSSFDataFormat":        {},
	"HSSFPrintSetup":        {},
	"HSSFPatternFormatting": {},
	// ---- SS common interfaces (org.apache.poi.ss.usermodel) ----
	"Workbook":         {},
	"Sheet":            {},
	"Row":              {},
	"CellStyle":        {},
	"CreationHelper":   {},
	"DataFormat":       {},
	"FormulaEvaluator": {},
	"Drawing":          {},
	"Hyperlink":        {},
	"Comment":          {},
	"Name":             {},
	"PictureData":      {},
	"PrintSetup":       {},
	"RichTextString":   {},
	// NOTE: `Cell`, `Font`, `Sheet`, `Row` are excluded because they collide
	// with extremely common non-POI user class names and project-local names;
	// the primary fix (synthetic-FQN in upsertImportSet) handles those.
	// ---- SS util (org.apache.poi.ss.util) ----
	"CellRangeAddress":     {},
	"CellReference":        {},
	"CellRangeAddressList": {},
	"RegionUtil":           {},
	"CellUtil":             {},
	"AreaReference":        {},
	// ---- WorkbookFactory / utility classes ----
	"WorkbookFactory":           {},
	"DataFormatter":             {},
	"WorkbookEvaluatorProvider": {},
	// ---- XWPF (Word docx) ----
	"XWPFDocument":  {},
	"XWPFParagraph": {},
	"XWPFRun":       {},
	"XWPFTable":     {},
	"XWPFTableRow":  {},
	"XWPFTableCell": {},
	"XWPFHeader":    {},
	"XWPFFooter":    {},
	"XWPFHyperlink": {},
	"XWPFStyle":     {},
	"XWPFStyles":    {},
	"XWPFComment":   {},
	"XWPFComments":  {},
	"XWPFList":      {},
	"XWPFNumbering": {},
	// ---- XSLF (PowerPoint pptx) ----
	"XMLSlideShow":      {},
	"XSLFSlide":         {},
	"XSLFSlideLayout":   {},
	"XSLFSlideMaster":   {},
	"XSLFTextShape":     {},
	"XSLFTextParagraph": {},
	"XSLFTextRun":       {},
	"XSLFShape":         {},
	"XSLFGroupShape":    {},
	"XSLFPictureShape":  {},
	"XSLFTable":         {},
	"XSLFTableRow":      {},
	"XSLFTableCell":     {},
}

// pdfBoxBareNames is the allowlist of Apache PDFBox class names that
// appear as bare-name CALLS stubs — primarily constructor calls and
// static factory/constant references.
//
// Gated by hasPdfBoxImport. Issue #787c.
var pdfBoxBareNames = map[string]struct{}{
	// ---- org.apache.pdfbox.pdmodel ----
	"PDDocument":            {},
	"PDPage":                {},
	"PDPageContentStream":   {},
	"PDPageTree":            {},
	"PDResources":           {},
	"PDDocumentInformation": {},
	"PDDocumentCatalog":     {},
	"PDDocumentOutline":     {},
	"PDOutlineItem":         {},
	"PDPageDestination":     {},
	"PDNamedDestination":    {},
	// ---- org.apache.pdfbox.pdmodel.common ----
	"PDRectangle": {},
	"PDStream":    {},
	"PDMetadata":  {},
	// ---- org.apache.pdfbox.pdmodel.font ----
	"PDFont":          {},
	"PDType1Font":     {},
	"PDTrueTypeFont":  {},
	"PDType0Font":     {},
	"PDCIDFontType2":  {},
	"Standard14Fonts": {},
	// ---- org.apache.pdfbox.pdmodel.graphics.image ----
	"PDImageXObject":  {},
	"JPEGFactory":     {},
	"LosslessFactory": {},
	"CCITTFactory":    {},
	// ---- org.apache.pdfbox.pdmodel.graphics.color ----
	"PDColorSpace": {},
	"PDDeviceRGB":  {},
	"PDDeviceGray": {},
	"PDDeviceCMYK": {},
	// ---- org.apache.pdfbox.pdmodel.interactive.* ----
	"PDAnnotation":       {},
	"PDAnnotationLink":   {},
	"PDActionURI":        {},
	"PDActionGoTo":       {},
	"PDAnnotationWidget": {},
	// ---- org.apache.pdfbox.rendering ----
	"PDFRenderer":    {},
	"RenderingHints": {},
	// ---- org.apache.pdfbox.text ----
	"PDFTextStripper": {},
	"TextPosition":    {},
	// ---- org.apache.pdfbox.util ----
	"Matrix": {},
}

// hasJSCollectionLibImport reports whether the source JS/TS file imports
// any canonical collection library — lodash / lodash-es / lodash/fp /
// ramda / immutable — or React itself. Gates the wave-9 Array.prototype
// bare-name allowlist (`reduce`, `find`, `forEach`, `filter`, `map`).
// Without an import gate these generic names would shadow user methods
// on hand-rolled classes; with the gate the file is signalling intent
// to consume one of the libraries whose surface these names belong to.
//
// React is included because the JSX/hooks idioms (`items.map(...)`,
// `arr.reduce(...)`, `list.filter(...)`) inside React components are the
// dominant source of these receiver-stripped bare names — and any file
// importing React is a UI component file unlikely to also hand-roll a
// user method named `reduce`.
//
// Imports are matched on the post-extractor canonical form (the JS
// extractor stores raw npm spec for non-relative imports, or the dotted
// repo-relative module path for resolved imports). Bare-name imports
// like `react`, `lodash`, `ramda` are exact-match; namespaced lodash
// subpaths (`lodash/fp`, `lodash/get`) get a prefix match.
func hasJSCollectionLibImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		// Strip ext: prefix if the external synthesiser has already
		// canonicalised this import edge.
		spec := p
		if strings.HasPrefix(spec, "ext:") {
			spec = spec[len("ext:"):]
		}
		switch spec {
		case "react", "lodash", "lodash-es", "ramda", "immutable", "immer":
			return true
		}
		if strings.HasPrefix(spec, "lodash/") || strings.HasPrefix(spec, "lodash-es/") ||
			strings.HasPrefix(spec, "ramda/") {
			return true
		}
	}
	return false
}

// hasReanimatedImport reports whether the source JS/TS file imports
// `react-native-reanimated`. Gates the Reanimated 3.x bare-name
// allowlist (`withTiming` etc. already in jsBareNames; the gate
// brings in the generic `timing` / `Value` / `sequence` / `parallel`
// / `Easing` / `interpolate` / `Extrapolate` shapes that ride on
// `Animated.<name>` receiver-strip and would shadow user methods if
// classified globally).
func hasReanimatedImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		spec := p
		if strings.HasPrefix(spec, "ext:") {
			spec = spec[len("ext:"):]
		}
		// `react-native-reanimated` is the canonical source for the
		// Reanimated API. `react-native` itself re-exports `Animated`
		// (with `.timing`/`.sequence`/`.parallel`/`.Value`/
		// `.createAnimatedComponent` as members) — files calling
		// `Animated.<name>(...)` in a `react-native` import context
		// produce the same bare leaves. Treat both as activating the
		// Animated allowlist.
		if spec == "react-native-reanimated" ||
			strings.HasPrefix(spec, "react-native-reanimated/") ||
			spec == "react-native" ||
			strings.HasPrefix(spec, "react-native/") {
			return true
		}
	}
	return false
}

// jsReanimatedBareNames is the file-scoped Reanimated 3.x surface
// gated by hasReanimatedImport. These names ride on
// `Animated.<name>(...)` receiver-strip and on the
// `react-native-reanimated` factory + helper exports. Curated from
// client-fixture-c bug-extractor residual: `timing`, `Value`,
// `sequence`, `parallel`, `createAnimatedComponent` appear on the
// Reanimated `Animated` namespace and standalone re-exports.
//
// `withTiming`/`withSpring`/`withDecay`/`withDelay`/`withRepeat`/
// `withSequence`/`interpolate`/`interpolateColor` and the Reanimated
// hooks (`useSharedValue` etc.) are already in jsBareNames
// (unconditional) — not duplicated here.
var jsReanimatedBareNames = map[string]struct{}{
	"timing":                  {}, // Animated.timing
	"spring":                  {}, // Animated.spring
	"decay":                   {}, // Animated.decay
	"sequence":                {}, // Animated.sequence (also array-shape)
	"parallel":                {}, // Animated.parallel
	"stagger":                 {}, // Animated.stagger
	"delay":                   {}, // Animated.delay
	"loop":                    {}, // Animated.loop
	"event":                   {}, // Animated.event
	"diffClamp":               {}, // Animated.diffClamp
	"Value":                   {}, // Animated.Value
	"ValueXY":                 {}, // Animated.ValueXY
	"createAnimatedComponent": {}, // Animated.createAnimatedComponent
	"cancelAnimation":         {}, // reanimated
	"useFrameCallback":        {},
	"useAnimatedSensor":       {},
	"measure":                 {}, // reanimated measure() worklet
	"scrollTo":                {}, // reanimated scrollTo worklet
	"defineAnimation":         {},
}

// hasGluestackImport reports whether the source JS/TS file imports
// any `@gluestack-ui/*` or `@gluestack-style/*` package. Gates the
// gluestack-ui bare-name allowlist (`useStyleContext`,
// `withStyleContext`, `tva`, ...). The package surface is distinctive
// enough that the gate is mostly belt-and-braces against rare user
// methods of the same name elsewhere in the codebase.
func hasGluestackImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		spec := p
		if strings.HasPrefix(spec, "ext:") {
			spec = spec[len("ext:"):]
		}
		if strings.HasPrefix(spec, "@gluestack-ui/") ||
			strings.HasPrefix(spec, "@gluestack-style/") {
			return true
		}
	}
	return false
}

// jsGluestackBareNames is the gluestack-ui surface gated by
// hasGluestackImport. Top-residual on client-fixture-c (`useStyleContext`
// = 43 hits — single largest leaf) plus the related style-context
// helpers.
var jsGluestackBareNames = map[string]struct{}{
	"useStyleContext":           {},
	"withStyleContext":          {},
	"withStyleContextAndStates": {},
	"useStyleContextAndStates":  {},
	"tva":                       {}, // gluestack tva() variant authoring
	"createStyle":               {},
	"styled":                    {},
	"createConfig":              {},
	"createComponents":          {},
	"createGenericComponent":    {},
	"useBreakpointValue":        {}, // gluestack responsive helper
	"useDrawerStatus":           {}, // gluestack drawer
	"useToken":                  {}, // gluestack token reader
}

// hasGorhomBottomSheetImport reports whether the file imports
// `@gorhom/bottom-sheet`. Activates the bottom-sheet handle method
// surface (`snapToIndex`, `expand`, `collapse`, `close`, ...).
func hasGorhomBottomSheetImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		spec := p
		if strings.HasPrefix(spec, "ext:") {
			spec = spec[len("ext:"):]
		}
		if spec == "@gorhom/bottom-sheet" ||
			strings.HasPrefix(spec, "@gorhom/bottom-sheet/") {
			return true
		}
	}
	return false
}

var jsGorhomBottomSheetBareNames = map[string]struct{}{
	"snapToIndex":    {},
	"snapToPosition": {},
	"expand":         {},
	"collapse":       {},
	"forceClose":     {},
	"present":        {},
	"dismiss":        {},
	"dismissAll":     {},
}

// hasExpoCameraImport reports whether the file imports expo-camera /
// expo-image-picker. Activates the camera/permission surface.
func hasExpoCameraImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		spec := p
		if strings.HasPrefix(spec, "ext:") {
			spec = spec[len("ext:"):]
		}
		if spec == "expo-camera" || strings.HasPrefix(spec, "expo-camera/") ||
			spec == "expo-image-picker" || strings.HasPrefix(spec, "expo-image-picker/") ||
			spec == "react-native-image-crop-picker" {
			return true
		}
	}
	return false
}

var jsExpoCameraBareNames = map[string]struct{}{
	"requestCameraPermissionsAsync":       {},
	"getCameraPermissionsAsync":           {},
	"requestMicrophonePermissionsAsync":   {},
	"requestMediaLibraryPermissionsAsync": {},
	"launchCameraAsync":                   {},
	"launchImageLibraryAsync":             {},
	"openCamera":                          {}, // react-native-image-crop-picker
	"openPicker":                          {},
	"openCropper":                         {},
	"clean":                               {}, // ImagePicker.clean()
}

// hasExpoFileImport reports whether the file imports expo-file-system /
// expo-document-picker / expo-media-library / expo-sharing.
func hasExpoFileImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		spec := p
		if strings.HasPrefix(spec, "ext:") {
			spec = spec[len("ext:"):]
		}
		if spec == "expo-file-system" || strings.HasPrefix(spec, "expo-file-system/") ||
			spec == "expo-document-picker" || strings.HasPrefix(spec, "expo-document-picker/") ||
			spec == "expo-media-library" || strings.HasPrefix(spec, "expo-media-library/") ||
			spec == "expo-sharing" {
			return true
		}
	}
	return false
}

var jsExpoFileBareNames = map[string]struct{}{
	"downloadFileAsync":         {}, // expo-file-system
	"uploadAsync":               {},
	"readAsStringAsync":         {},
	"writeAsStringAsync":        {},
	"deleteAsync":               {},
	"moveAsync":                 {},
	"copyAsync":                 {},
	"makeDirectoryAsync":        {},
	"readDirectoryAsync":        {},
	"getInfoAsync":              {},
	"getContentUriAsync":        {},
	"getFreeDiskStorageAsync":   {},
	"getTotalDiskCapacityAsync": {},
	"pickDirectoryAsync":        {}, // expo-document-picker
	"getDocumentAsync":          {},
	"shareAsync":                {}, // expo-sharing
	"isAvailableAsync":          {}, // expo-sharing / expo-haptics / ...
	"createAssetAsync":          {}, // expo-media-library
	"createAlbumAsync":          {},
	"addAssetsToAlbumAsync":     {},
	"saveToLibraryAsync":        {},
}

// hasExpoNetworkImport reports whether the file imports expo-network /
// expo-intent-launcher / expo-linking / expo-local-authentication /
// expo-notifications — the smaller expo platform modules whose
// distinctive `*Async` surface needs gating.
func hasExpoNetworkImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		spec := p
		if strings.HasPrefix(spec, "ext:") {
			spec = spec[len("ext:"):]
		}
		switch spec {
		case "expo-network", "expo-intent-launcher", "expo-linking",
			"expo-local-authentication", "expo-notifications",
			"expo-application", "expo-device", "expo-haptics",
			"expo-clipboard", "expo-screen-orientation", "expo-status-bar":
			return true
		}
	}
	return false
}

var jsExpoPlatformBareNames = map[string]struct{}{
	"getNetworkStateAsync":                 {}, // expo-network
	"getIpAddressAsync":                    {},
	"getMacAddressAsync":                   {},
	"startActivityAsync":                   {}, // expo-intent-launcher
	"supportedAuthenticationTypesAsync":    {}, // expo-local-authentication
	"authenticateAsync":                    {},
	"hasHardwareAsync":                     {},
	"isEnrolledAsync":                      {},
	"scheduleNotificationAsync":            {}, // expo-notifications
	"cancelScheduledNotificationAsync":     {},
	"cancelAllScheduledNotificationsAsync": {},
	"setNotificationHandler":               {},
	"getExpoPushTokenAsync":                {},
	"getDevicePushTokenAsync":              {},
	"setStringAsync":                       {}, // expo-clipboard
	"getStringAsync":                       {},
	"hasStringAsync":                       {},
	"lockAsync":                            {}, // expo-screen-orientation
	"unlockAsync":                          {},
	"impactAsync":                          {}, // expo-haptics
	"notificationAsync":                    {},
	"selectionAsync":                       {},
}

// hasRNAudioRecorderImport reports whether the file imports
// `react-native-audio-recorder-player`. Activates the recorder/player
// bare-name allowlist — every method on the singleton instance leaks
// as a receiver-stripped bare name.
func hasRNAudioRecorderImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		spec := p
		if strings.HasPrefix(spec, "ext:") {
			spec = spec[len("ext:"):]
		}
		if spec == "react-native-audio-recorder-player" ||
			strings.HasPrefix(spec, "react-native-audio-recorder-player/") ||
			spec == "react-native-nitro-sound" ||
			strings.HasPrefix(spec, "react-native-nitro-sound/") {
			return true
		}
	}
	return false
}

var jsRNAudioRecorderBareNames = map[string]struct{}{
	"startPlayer":               {},
	"stopPlayer":                {},
	"pausePlayer":               {},
	"resumePlayer":              {},
	"seekToPlayer":              {},
	"setVolume":                 {},
	"setSubscriptionDuration":   {},
	"addPlayBackListener":       {},
	"removePlayBackListener":    {},
	"addPlaybackEndListener":    {},
	"removePlaybackEndListener": {},
	"startRecorder":             {},
	"stopRecorder":              {},
	"pauseRecorder":             {},
	"resumeRecorder":            {},
	"addRecordBackListener":     {},
	"removeRecordBackListener":  {},
	"mmssss":                    {},
	"mmss":                      {},
}

// hasRNGestureHandlerImport reports whether the file imports
// `react-native-gesture-handler`. Activates the gesture-builder
// chain-method surface.
func hasRNGestureHandlerImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		spec := p
		if strings.HasPrefix(spec, "ext:") {
			spec = spec[len("ext:"):]
		}
		if spec == "react-native-gesture-handler" ||
			strings.HasPrefix(spec, "react-native-gesture-handler/") {
			return true
		}
	}
	return false
}

var jsRNGestureHandlerBareNames = map[string]struct{}{
	"requireExternalGestureToFail":    {},
	"requireExternalGestureToBegin":   {},
	"simultaneousWithExternalGesture": {},
	"numberOfTaps":                    {},
	"maxDuration":                     {},
	"minDuration":                     {},
	"manualActivation":                {},
	"shouldCancelWhenOutside":         {},
	"hitSlop":                         {},
	"activeOffsetX":                   {},
	"activeOffsetY":                   {},
	"failOffsetX":                     {},
	"failOffsetY":                     {},
}

// hasReactNavigationImport reports whether the file imports any
// `@react-navigation/*` package, or `expo-router` (which wraps it).
// Activates a focused navigation-handle surface
// (`goBack`/`jumpTo`/`reset`/...). `setOptions` and several other
// names are already in jsBareNames (unconditional) — not duplicated.
func hasReactNavigationImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		spec := p
		if strings.HasPrefix(spec, "ext:") {
			spec = spec[len("ext:"):]
		}
		if strings.HasPrefix(spec, "@react-navigation/") ||
			spec == "expo-router" || strings.HasPrefix(spec, "expo-router/") {
			return true
		}
	}
	return false
}

var jsReactNavigationBareNames = map[string]struct{}{
	"goBack":         {}, // navigation.goBack
	"jumpTo":         {}, // navigation.jumpTo
	"toggleDrawer":   {}, // drawer navigation
	"openDrawer":     {},
	"closeDrawer":    {},
	"setParams":      {},
	"isFocused":      {},
	"canGoBack":      {},
	"addListener":    {}, // navigation.addListener (also EventEmitter)
	"removeListener": {},
	"dispatch":       {}, // navigation.dispatch (Redux dispatch already
	// handled by useDispatch hook; navigation.dispatch is the bare
	// receiver-stripped name).
}

// jsCollectionLibBareNames is the wave-9 Array.prototype / lodash /
// ramda allowlist gated by hasJSCollectionLibImport. These names are the
// generic-collection ops that #94's safer-bias rule keeps out of the
// global jsBareNames stop-list; the per-import gate brings them in only
// for files that actually consume one of the libraries.
//
// Curated from client-fixture-b wave-8 residual: `reduce`, `find`,
// `forEach`, `filter`, `map` together account for ~110 of the remaining
// bug-extractor leaf-name hits on that fixture; all originate in React
// components iterating over arrays via Array.prototype.
// `every`, `some`, `push`, `trim`, `isArray` already in jsBareNames
// (unconditional) — not duplicated here.
var jsCollectionLibBareNames = map[string]struct{}{
	"reduce":      {},
	"reduceRight": {},
	"find":        {},
	"findIndex":   {},
	"findLast":    {},
	"forEach":     {},
	"filter":      {},
	"map":         {},
	"flatMap":     {},
	// Wave-12 (ship-gate FINAL) — lodash / ramda chain-style and
	// utility-collection methods. These dominate the wave-11 residual on
	// client-fixture-b (`unwrap`, `get`, `omit`, `pick`, `merge`,
	// `cloneDeep`). Already gated by hasJSCollectionLibImport so files
	// without lodash/ramda/immutable/react imports don't activate them —
	// preserves the safer-bias rule for hand-rolled classes with same
	// method names. Curated from lodash API docs + client-fixture-b
	// disposition samples.
	"get":            {}, // lodash _.get(obj, 'path')
	"set":            {}, // lodash _.set
	"has":            {}, // lodash _.has
	"unset":          {}, // lodash _.unset
	"unwrap":         {}, // ramda R.unwrap / lodash chain wrap/unwrap
	"omit":           {}, // lodash _.omit
	"omitBy":         {}, // lodash _.omitBy
	"pick":           {}, // lodash _.pick
	"pickBy":         {}, // lodash _.pickBy
	"merge":          {}, // lodash _.merge
	"mergeWith":      {}, // lodash _.mergeWith
	"cloneDeep":      {}, // lodash _.cloneDeep
	"clone":          {}, // lodash _.clone
	"isEqual":        {}, // lodash _.isEqual
	"isEmpty":        {}, // lodash _.isEmpty
	"isObject":       {}, // lodash _.isObject
	"isPlainObject":  {}, // lodash _.isPlainObject
	"isString":       {}, // lodash _.isString
	"isNumber":       {}, // lodash _.isNumber
	"isFunction":     {}, // lodash _.isFunction
	"isBoolean":      {}, // lodash _.isBoolean
	"isNil":          {}, // lodash _.isNil
	"isNull":         {}, // lodash _.isNull
	"isUndefined":    {}, // lodash _.isUndefined
	"isDate":         {}, // lodash _.isDate
	"isRegExp":       {}, // lodash _.isRegExp
	"isError":        {}, // lodash _.isError
	"isFinite":       {}, // lodash _.isFinite
	"isInteger":      {}, // lodash _.isInteger
	"keyBy":          {}, // lodash _.keyBy
	"orderBy":        {}, // lodash _.orderBy
	"sortBy":         {}, // lodash _.sortBy
	"uniqBy":         {}, // lodash _.uniqBy
	"uniq":           {}, // lodash _.uniq
	"uniqWith":       {}, // lodash _.uniqWith
	"intersection":   {}, // lodash _.intersection
	"intersectionBy": {}, // lodash _.intersectionBy
	"union":          {}, // lodash _.union
	"unionBy":        {}, // lodash _.unionBy
	"difference":     {}, // lodash _.difference
	"differenceBy":   {}, // lodash _.differenceBy
	"chunk":          {}, // lodash _.chunk
	"compact":        {}, // lodash _.compact
	"flatten":        {}, // lodash _.flatten
	"flattenDeep":    {}, // lodash _.flattenDeep
	"flattenDepth":   {}, // lodash _.flattenDepth
	"zip":            {}, // lodash _.zip
	"unzip":          {}, // lodash _.unzip
	"zipObject":      {}, // lodash _.zipObject
	"times":          {}, // lodash _.times
	"partial":        {}, // lodash _.partial
	"partialRight":   {}, // lodash _.partialRight
	"debounce":       {}, // lodash _.debounce
	"throttle":       {}, // lodash _.throttle
	"memoize":        {}, // lodash _.memoize
	"noop":           {}, // lodash _.noop
	"identity":       {}, // lodash _.identity
	"constant":       {}, // lodash _.constant
	"defaults":       {}, // lodash _.defaults
	"defaultsDeep":   {}, // lodash _.defaultsDeep
	"invert":         {}, // lodash _.invert
	"mapValues":      {}, // lodash _.mapValues (also kafka, but file-gated so disjoint)
	"mapKeys":        {}, // lodash _.mapKeys
	"keys":           {}, // lodash _.keys
	"values":         {}, // lodash _.values
	"entries":        {}, // lodash _.entries
	"fromPairs":      {}, // lodash _.fromPairs
	"toPairs":        {}, // lodash _.toPairs
	"sumBy":          {}, // lodash _.sumBy
	"meanBy":         {}, // lodash _.meanBy
	"maxBy":          {}, // lodash _.maxBy
	"minBy":          {}, // lodash _.minBy
	"countBy":        {}, // lodash _.countBy
	"partition":      {}, // lodash _.partition
	"take":           {}, // lodash _.take
	"takeWhile":      {}, // lodash _.takeWhile
	"drop":           {}, // lodash _.drop
	"dropWhile":      {}, // lodash _.dropWhile
	"head":           {}, // lodash _.head
	"last":           {}, // lodash _.last
	"tail":           {}, // lodash _.tail
	"initial":        {}, // lodash _.initial
	"nth":            {}, // lodash _.nth
	"sample":         {}, // lodash _.sample
	"sampleSize":     {}, // lodash _.sampleSize
	"shuffle":        {}, // lodash _.shuffle
	"trim":           {}, // (already in jsBareNames; harmless dupe via gate)
}

// kafkaStreamsDSLVerbs is the Kafka-Streams/Kafka-clients bare-name
// allowlist. Names are receiver-stripped fluent calls on KStream /
// KTable / KGroupedStream / StreamsBuilder / KafkaProducer /
// KafkaConsumer / TopologyTestDriver / TestInputTopic / TestOutputTopic
// / Serdes / Properties / IntegrationTestUtils. Gated by
// hasKafkaImport so user-method collisions in non-Kafka code are
// preserved as missing-resolution bugs (per the #105 safer-bias rule).
var kafkaStreamsDSLVerbs = map[string]struct{}{
	// StreamsBuilder / Topology
	"build":  {},
	"stream": {},
	"table":  {},
	// KStream/KTable terminal + transform verbs that survived
	// receiver-strip without a matching import-leaf fold.
	"to":              {},
	"through":         {},
	"branch":          {},
	"peek":            {},
	"foreach":         {},
	"forEach":         {},
	"transform":       {},
	"transformValues": {},
	"process":         {},
	"selectKey":       {},
	"mapValues":       {},
	"flatMapValues":   {},
	"flatMap":         {},
	"groupByKey":      {},
	"groupBy":         {},
	"aggregate":       {},
	"reduce":          {},
	"windowedBy":      {},
	"suppress":        {},
	"merge":           {},
	"toStream":        {},
	"leftJoin":        {},
	"outerJoin":       {},
	// KafkaStreams lifecycle.
	"start":                         {},
	"close":                         {},
	"cleanUp":                       {},
	"setStateListener":              {},
	"setUncaughtExceptionHandler":   {},
	"setGlobalStateRestoreListener": {},
	"store":                         {},
	"state":                         {},
	"metrics":                       {},
	"localThreadsMetadata":          {},
	"allMetadata":                   {},
	"allMetadataForStore":           {},
	// Producer / Consumer.
	"send":            {},
	"poll":            {},
	"flush":           {},
	"subscribe":       {},
	"assign":          {},
	"commitSync":      {},
	"commitAsync":     {},
	"seek":            {},
	"seekToBeginning": {},
	"seekToEnd":       {},
	"position":        {},
	"partitionsFor":   {},
	// ProducerRecord / ConsumerRecord accessors.
	"key":       {},
	"value":     {},
	"timestamp": {},
	"partition": {},
	"topic":     {},
	"offset":    {},
	"headers":   {},
	// Serdes / Materialized / TopologyTestDriver.
	"serializer":          {},
	"deserializer":        {},
	"serialize":           {},
	"deserialize":         {},
	"createInputTopic":    {},
	"createOutputTopic":   {},
	"pipeInput":           {},
	"pipeKeyValueList":    {},
	"pipeValueList":       {},
	"readValuesToList":    {},
	"readKeyValuesToList": {},
	"readValue":           {},
	"readKeyValue":        {},
	// Materialized / Stores.
	"withKeySerde":            {},
	"withValueSerde":          {},
	"as":                      {},
	"persistentKeyValueStore": {},
	"persistentWindowStore":   {},
	"persistentSessionStore":  {},
	"inMemoryKeyValueStore":   {},
	"keyValueStoreBuilder":    {},
	"windowStoreBuilder":      {},
	"sessionStoreBuilder":     {},
	// Issue kafka-chase-578 — additional Materialized / Stores /
	// StoreBuilder fluent verbs receiver-stripped in kafka-streams-
	// examples (`Materialized.<>as(...).withCachingEnabled().
	// withLoggingEnabled()`, `Stores...withRetention(...)`).
	"withCachingEnabled":  {},
	"withCachingDisabled": {},
	"withLoggingEnabled":  {},
	"withLoggingDisabled": {},
	"withRetention":       {},
	// Issue kafka-chase-578 — TimeWindows / SessionWindows /
	// SlidingWindows builder verbs receiver-stripped.
	"ofSizeWithNoGrace":           {},
	"ofSizeAndGrace":              {},
	"ofInactivityGapWithNoGrace":  {},
	"ofTimeDifferenceWithNoGrace": {},
	"advanceBy":                   {},
	// Issue kafka-chase-578 — ProcessorContext fluent verbs
	// receiver-stripped in custom Processor implementations.
	"forward":                  {},
	"stateStoreNames":          {},
	"setStateRestoreListener":  {},
	"setStandbyUpdateListener": {},
	// Properties helpers commonly chained.
	"put":         {},
	"putAll":      {},
	"getProperty": {},
	"setProperty": {},
	"load":        {},
}

// hasJaxRsImport reports whether the source file declares any
// jakarta.ws.rs.* or javax.ws.rs.* import (JAX-RS Client / Server API).
func hasJaxRsImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		if p == "jakarta.ws.rs" || strings.HasPrefix(p, "jakarta.ws.rs.") {
			return true
		}
		if p == "javax.ws.rs" || strings.HasPrefix(p, "javax.ws.rs.") {
			return true
		}
	}
	return false
}

// jaxRsDSLVerbs covers JAX-RS Client / Response / WebTarget /
// Invocation.Builder fluent verbs receiver-stripped by the Java
// extractor.
var jaxRsDSLVerbs = map[string]struct{}{
	"target":          {},
	"request":         {},
	"path":            {},
	"queryParam":      {},
	"pathParam":       {},
	"resolveTemplate": {},
	"accept":          {},
	"acceptLanguage":  {},
	"acceptEncoding":  {},
	"buildGet":        {},
	"buildPost":       {},
	"buildPut":        {},
	"buildDelete":     {},
	"invoke":          {},
	"readEntity":      {},
	"hasEntity":       {},
	"getEntity":       {},
	"getStatus":       {},
	"getStatusInfo":   {},
	"getHeaders":      {},
	"getMediaType":    {},
	"getLocation":     {},
	"register":        {},
	"property":        {},
}

// commonsCliDSLVerbs covers the commons-cli builder pattern verbs
// receiver-stripped from `Option.builder("foo").longOpt(...).hasArg().
// desc(...).build()` and `Options.addOption(...)` /
// `HelpFormatter.printHelp(...)` / `CommandLine.hasOption(...)` chains.
// Gated by hasCommonsCliImport.
var commonsCliDSLVerbs = map[string]struct{}{
	"addOption":       {},
	"addOptionGroup":  {},
	"hasOption":       {},
	"getOptionValue":  {},
	"getOptionValues": {},
	"printHelp":       {},
	"longOpt":         {},
	"shortOpt":        {},
	"hasArg":          {},
	"hasArgs":         {},
	"desc":            {},
	"required":        {},
	"numberOfArgs":    {},
	"argName":         {},
	"valueSeparator":  {},
	"type":            {},
	"parse":           {},
}

// hasGoGrpcImport reports whether the source file's import set looks
// like it uses google.golang.org/grpc. Any import path with the
// `google.golang.org/grpc` prefix (root package or any subpackage:
// credentials, status, codes, metadata, balancer, resolver, peer,
// stats, keepalive, encoding, mem, internal, ...) is treated as a
// gRPC import. Issue #44 / proto-fix.
func hasGoGrpcImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		if p == "google.golang.org/grpc" ||
			strings.HasPrefix(p, "google.golang.org/grpc/") {
			return true
		}
	}
	return false
}

// hasGoCloserImport reports whether the source file's import set
// includes a stdlib (or grpc) package whose public types implement
// io.Closer. Used to gate the bare-name `Close` allowlist branch so
// it only matches in files that plausibly call Close on a third-party
// closer (not user-defined wrapper types). Issue #44 / proto-fix.
func hasGoCloserImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		switch p {
		case "io", "os", "net", "net/http", "bufio", "compress/gzip",
			"compress/zlib", "database/sql", "context", "io/ioutil":
			return true
		}
		if p == "google.golang.org/grpc" ||
			strings.HasPrefix(p, "google.golang.org/grpc/") {
			return true
		}
	}
	return false
}

// hasGoProtobufImport reports whether the source file's import set
// looks like it uses google.golang.org/protobuf or its predecessor
// github.com/golang/protobuf. Any import path under either prefix
// counts (protoimpl, protoreflect, proto, ptypes, jsonpb, ...).
// Issue #44 / proto-fix.
func hasGoProtobufImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		if p == "google.golang.org/protobuf" ||
			strings.HasPrefix(p, "google.golang.org/protobuf/") ||
			p == "github.com/golang/protobuf" ||
			strings.HasPrefix(p, "github.com/golang/protobuf/") {
			return true
		}
	}
	return false
}

// goGrpcDistinctiveBareNames is the subset of gRPC + protobuf
// receiver-stripped names that is distinctive enough to match on
// lang=="go" alone (no import gate). Selection rule: the name must be
// (a) multi-word PascalCase or unique snake_case AND (b) tied to a
// single grpc/protobuf API surface with no plausible user-method
// collision in non-grpc Go code (`Pairs` from metadata is the only
// short one — kept because the verb sense is rare in Go code, while
// the metadata.Pairs builder is universal in gRPC servers).
// Issue #44 / proto-fix.
var goGrpcDistinctiveBareNames = map[string]struct{}{
	// grpc/credentials — TLS / token / xds credential constructors.
	"NewCredentials":       {},
	"NewClientTLSFromFile": {},
	"NewClientTLSFromCert": {},
	"NewServerTLSFromFile": {},
	"NewServerTLSFromCert": {},
	"NewTLS":               {},
	"NewClientCredentials": {},
	"NewServerCredentials": {},
	"NewPerRPCCredentials": {},
	"NewOauthAccess":       {},
	"NewStaticCreds":       {},

	// grpc package + grpc/balancer/resolver factories.
	"NewServer":                 {},
	"NewClientConn":             {},
	"NewStream":                 {},
	"NewBuilderWithScheme":      {},
	"NewBalancer":               {},
	"NewSubConn":                {},
	"NewEvent":                  {},
	"NewCallbackSerializer":     {},
	"NewFramer":                 {},
	"NewFileWatcherCRLProvider": {},

	// Multi-word PascalCase that is uniquely gRPC / protoreflect.
	"FromIncomingContext":     {},
	"FromOutgoingContext":     {},
	"NewIncomingContext":      {},
	"NewOutgoingContext":      {},
	"AppendToOutgoingContext": {},
	"SetDefaultScheme":        {},
	"GetDefaultScheme":        {},
	"MustParseURL":            {},
	"InitialState":            {},
	"GetCodecV2":              {},
	"GetCompressor":           {},
	"RegisterCodec":           {},
	"RegisterService":         {},
	"GetServiceInfo":          {},
	"UpdateClientConnState":   {},
	"UpdateSubConnState":      {},
	"ResolverError":           {},
	"ParseServiceConfig":      {},
	"DefaultBufferPool":       {},
	"RecvCompress":            {},
	"WriteStatus":             {},
	"WriteSettings":           {},
	"WriteGoAway":             {},
	"ReadFrame":               {},
	"SendMsg":                 {},
	"RecvMsg":                 {},
	"CloseSend":               {},
	"FromError":               {},
	"FromContextError":        {},
	"Pairs":                   {},
	"TrySchedule":             {},
	"ScheduleOr":              {},
	"HandleRPC":               {},
	"HandleConn":              {},
	"TagRPC":                  {},
	"TagConn":                 {},
	"LazyLog":                 {},
	"LazyPrintf":              {},
	"Materialize":             {},
	"SliceBuffer":             {},
	"NopBufferPool":           {},

	// grpc/grpclog — multi-word logger functions (Infof/Warningf/V are
	// in the import-gated list because single-word `V` and `Info`
	// collide with generic verbs).
	"Warningf":     {},
	"Infof":        {},
	"InfoDepth":    {},
	"WarningDepth": {},
	"ErrorDepth":   {},
	"FatalDepth":   {},

	// protobuf runtime / generated message support — uniquely
	// protoimpl/protoreflect, gated only on lang=="go". Multi-word
	// PascalCase, no plausible collision.
	"MessageStateOf":   {},
	"StoreMessageInfo": {},
	"LoadMessageInfo":  {},
	"MessageStringOf":  {},
	"MessageOf":        {},
	"EnforceVersion":   {},
	"ProtoReflect":     {},
}

// goGrpcBareNames is the import-gated subset — names that overlap with
// generic verb method names (`Done`, `Recv`, `Stop`, `Get`, `Format`,
// `Add`, `V`, `Build`, `Serve`). Gated by hasGoGrpcImport so they only
// classify as external for source files that actually import gRPC.
// Issue #44 / proto-fix.
var goGrpcBareNames = map[string]struct{}{
	// grpc package — server/client factories that overlap with generic
	// verbs (`Serve`, `Stop`, `Dial`, `Register`).
	"NewClient":    {},
	"Dial":         {},
	"DialContext":  {},
	"Serve":        {},
	"Stop":         {},
	"GracefulStop": {},
	"Register":     {},
	"Convert":      {},
	"Code":         {},
	"Get":          {},
	"Build":        {},
	"V":            {},

	// grpc/internal/grpcsync.
	"Fire":     {},
	"HasFired": {},
	"Done":     {},

	// grpc/resolver — overlaps with generic verbs.
	"Scheme":      {},
	"UpdateState": {},
	"ReportError": {},
	"ResolveNow":  {},
	"ExitIdle":    {},
	"GetCodec":    {},

	// grpc/mem.
	"NewBuffer":      {},
	"NewBufferSlice": {},
	"NewWriter":      {},
	"ReadOnlyData":   {},
	"Free":           {},

	// grpc client/server streaming surface — overlap with generic.
	"Recv":       {},
	"Send":       {},
	"SendHeader": {},
	"SetHeader":  {},
	"SetTrailer": {},
	"Trailer":    {},

	// grpc trace / channelz.
	"SetError": {},
	"Finish":   {},

	// grpc service-impl idioms that appear in the examples
	// (UnaryEcho/BidirectionalStreamingEcho are method names on the
	// generated EchoServer; they only resolve when the file imports
	// the echo proto package).
	"UnaryEcho":                  {},
	"BidirectionalStreamingEcho": {},
	"ServerStreamingEcho":        {},
	"ClientStreamingEcho":        {},
}

// goProtobufBareNames is the bare-name allowlist for the
// google.golang.org/protobuf runtime (protoimpl / protoreflect /
// proto). These names appear in generated `*.pb.go` files via the
// `protoimpl.X` global; the Go extractor strips the receiver and
// leaves the bare name. Gated by hasGoProtobufImport. Issue #44 /
// proto-fix.
var goProtobufBareNames = map[string]struct{}{
	// protoimpl runtime helpers — generated message support.
	"MessageStateOf":   {},
	"StoreMessageInfo": {},
	"LoadMessageInfo":  {},
	"MessageStringOf":  {},
	"MessageOf":        {},
	"Pointer":          {},
	"PointerTo":        {},
	"EnforceVersion":   {},

	// proto package — wire-format helpers.
	"Marshal":          {},
	"Unmarshal":        {},
	"Equal":            {},
	"Clone":            {},
	"Reset":            {},
	"Size":             {},
	"MarshalOptions":   {},
	"UnmarshalOptions": {},

	// protoreflect — descriptor traversal helpers.
	"Descriptor":   {},
	"ProtoReflect": {},
	"Type":         {},
	"Number":       {},
	"FullName":     {},
	"Name":         {},
	"Kind":         {},
}

// goStdlibInterfaceMethods maps a canonicalised Go-stdlib type (with
// leading `*` stripped) to the set of methods defined on that type or its
// embedding interfaces, paired with the canonical import-path of the
// declaring stdlib package. Used by classifyExternal (issue #364) to route
// CALLS edges whose extractor-stamped `receiver_type` matches one of these
// types to the corresponding `ext:<package>` placeholder.
//
// Only stdlib types are catalogued — user-defined types and third-party
// types fall through and continue to count as unmatched. Per-method false
// positives are extremely rare because both gates (the type name AND the
// method name) must align with a stdlib signature; a user type happening to
// share a name (e.g. local `Request` struct) will not have a stdlib package
// path stamp upstream and is filtered out here.
//
// Selection rule: the catalogue mirrors the `net/http`, `io`, `fmt`, `os`,
// `bytes`, `strings`, `sync`, `context`, `bufio`, and `database/sql`
// surfaces that dominate residual go-chi bug-rate post-#148. Each entry's
// methods list is the union of (a) methods declared on the type itself and
// (b) methods inherited from embedded stdlib interfaces. Adding a new entry
// requires the package and the method name to both be unambiguous in the
// stdlib — see comments in this map for borderline names that were
// excluded.
var goStdlibInterfaceMethods = map[string]struct {
	pkg     string
	methods map[string]struct{}
}{
	// net/http core types. *http.Request methods include those from
	// io.Reader (via Body) but Body itself is a field; only the request's
	// own methods are listed.
	"http.Request": {
		pkg: "net/http",
		methods: map[string]struct{}{
			"Cookie": {}, "Cookies": {}, "AddCookie": {}, "FormFile": {},
			"FormValue": {}, "PostFormValue": {}, "ParseForm": {},
			"ParseMultipartForm": {}, "Referer": {}, "UserAgent": {},
			"BasicAuth": {}, "SetBasicAuth": {}, "Clone": {}, "Context": {},
			"WithContext": {}, "MultipartReader": {}, "ProtoAtLeast": {},
			"PathValue": {}, "SetPathValue": {},
		},
	},
	"http.ResponseWriter": {
		pkg: "net/http",
		methods: map[string]struct{}{
			// Header is intentionally listed — collision with user types is
			// gated by the `receiver_type=http.ResponseWriter` stamp.
			"Header": {}, "Write": {}, "WriteHeader": {}, "Flush": {},
		},
	},
	"http.Handler": {
		pkg: "net/http",
		methods: map[string]struct{}{
			"ServeHTTP": {},
		},
	},
	"http.HandlerFunc": {
		pkg: "net/http",
		methods: map[string]struct{}{
			"ServeHTTP": {},
		},
	},
	"http.Server": {
		pkg: "net/http",
		methods: map[string]struct{}{
			"ListenAndServe": {}, "ListenAndServeTLS": {}, "Serve": {},
			"ServeTLS": {}, "Shutdown": {}, "Close": {}, "RegisterOnShutdown": {},
			"SetKeepAlivesEnabled": {},
		},
	},
	"http.Client": {
		pkg: "net/http",
		methods: map[string]struct{}{
			"Do": {}, "Get": {}, "Head": {}, "Post": {}, "PostForm": {},
			"CloseIdleConnections": {},
		},
	},
	"http.Response": {
		pkg: "net/http",
		methods: map[string]struct{}{
			// Cookies is on *http.Response too; Write encodes the response
			// to a Writer (rare but valid stdlib method).
			"Cookies": {}, "Location": {}, "ProtoAtLeast": {},
		},
	},
	"http.Header": {
		pkg: "net/http",
		methods: map[string]struct{}{
			"Add": {}, "Set": {}, "Get": {}, "Values": {}, "Del": {},
			"Clone": {}, "Write": {}, "WriteSubset": {},
		},
	},

	// io interfaces. Method sets are tiny and well-known; collision risk
	// with user types is handled by the receiver_type stamp.
	"io.Reader": {
		pkg: "io",
		methods: map[string]struct{}{
			"Read": {},
		},
	},
	"io.Writer": {
		pkg: "io",
		methods: map[string]struct{}{
			"Write": {},
		},
	},
	"io.Closer": {
		pkg: "io",
		methods: map[string]struct{}{
			"Close": {},
		},
	},
	"io.ReadCloser": {
		pkg: "io",
		methods: map[string]struct{}{
			"Read": {}, "Close": {},
		},
	},
	"io.WriteCloser": {
		pkg: "io",
		methods: map[string]struct{}{
			"Write": {}, "Close": {},
		},
	},
	"io.ReadWriter": {
		pkg: "io",
		methods: map[string]struct{}{
			"Read": {}, "Write": {},
		},
	},
	"io.ReadWriteCloser": {
		pkg: "io",
		methods: map[string]struct{}{
			"Read": {}, "Write": {}, "Close": {},
		},
	},

	// fmt.Stringer + error are universally implemented; the receiver_type
	// stamp guarantees we only synthesise when the parameter is declared
	// with the interface type explicitly.
	"fmt.Stringer": {
		pkg:     "fmt",
		methods: map[string]struct{}{"String": {}},
	},
	// `error` is a Go builtin interface, but the placeholder convention
	// uses package import paths. Routing it to `errors` keeps the
	// downstream allowlist gate (which already lists "errors") stable;
	// `Error()` calls land in ext:errors rather than synthesising a new
	// "builtin" bucket.
	"error": {
		pkg:     "errors",
		methods: map[string]struct{}{"Error": {}},
	},

	// context.Context — appears as a parameter in nearly every Go service.
	"context.Context": {
		pkg: "context",
		methods: map[string]struct{}{
			"Deadline": {}, "Done": {}, "Err": {}, "Value": {},
		},
	},

	// sync types frequently passed by pointer.
	"sync.Mutex": {
		pkg: "sync",
		methods: map[string]struct{}{
			"Lock": {}, "Unlock": {}, "TryLock": {},
		},
	},
	"sync.RWMutex": {
		pkg: "sync",
		methods: map[string]struct{}{
			"Lock": {}, "Unlock": {}, "RLock": {}, "RUnlock": {},
			"TryLock": {}, "TryRLock": {}, "RLocker": {},
		},
	},
	"sync.WaitGroup": {
		pkg: "sync",
		methods: map[string]struct{}{
			"Add": {}, "Done": {}, "Wait": {},
		},
	},

	// bytes / strings buffers — methods include the io.Reader / io.Writer
	// surface plus Buffer-specific helpers.
	"bytes.Buffer": {
		pkg: "bytes",
		methods: map[string]struct{}{
			"Bytes": {}, "String": {}, "Len": {}, "Cap": {}, "Truncate": {},
			"Reset": {}, "Grow": {}, "Write": {}, "WriteString": {},
			"WriteByte": {}, "WriteRune": {}, "Read": {}, "ReadByte": {},
			"ReadRune": {}, "ReadBytes": {}, "ReadString": {}, "Next": {},
			"UnreadByte": {}, "UnreadRune": {},
		},
	},
	"strings.Builder": {
		pkg: "strings",
		methods: map[string]struct{}{
			"String": {}, "Len": {}, "Reset": {}, "Grow": {},
			"Write": {}, "WriteString": {}, "WriteByte": {}, "WriteRune": {},
		},
	},

	// bufio Reader/Writer — common stdlib I/O wrappers.
	"bufio.Reader": {
		pkg: "bufio",
		methods: map[string]struct{}{
			"Read": {}, "ReadByte": {}, "ReadRune": {}, "ReadString": {},
			"ReadBytes": {}, "ReadLine": {}, "ReadSlice": {}, "Peek": {},
			"Discard": {}, "Buffered": {}, "Reset": {},
			"UnreadByte": {}, "UnreadRune": {},
		},
	},
	"bufio.Writer": {
		pkg: "bufio",
		methods: map[string]struct{}{
			"Write": {}, "WriteString": {}, "WriteByte": {}, "WriteRune": {},
			"Flush": {}, "Available": {}, "Buffered": {}, "Reset": {},
		},
	},

	// database/sql common pointer types.
	"sql.DB": {
		pkg: "database/sql",
		methods: map[string]struct{}{
			"Query": {}, "QueryRow": {}, "Exec": {}, "QueryContext": {},
			"QueryRowContext": {}, "ExecContext": {}, "Begin": {},
			"BeginTx": {}, "Prepare": {}, "PrepareContext": {}, "Ping": {},
			"PingContext": {}, "Close": {}, "Conn": {}, "Driver": {},
			"SetMaxOpenConns": {}, "SetMaxIdleConns": {},
			"SetConnMaxLifetime": {}, "SetConnMaxIdleTime": {}, "Stats": {},
		},
	},
	"sql.Tx": {
		pkg: "database/sql",
		methods: map[string]struct{}{
			"Commit": {}, "Rollback": {}, "Query": {}, "QueryRow": {},
			"Exec": {}, "QueryContext": {}, "QueryRowContext": {},
			"ExecContext": {}, "Prepare": {}, "PrepareContext": {}, "Stmt": {},
			"StmtContext": {},
		},
	},
	"sql.Rows": {
		pkg: "database/sql",
		methods: map[string]struct{}{
			"Next": {}, "NextResultSet": {}, "Scan": {}, "Close": {},
			"Err": {}, "Columns": {}, "ColumnTypes": {},
		},
	},
	"sql.Row": {
		pkg: "database/sql",
		methods: map[string]struct{}{
			"Scan": {}, "Err": {},
		},
	},
	"sql.Stmt": {
		pkg: "database/sql",
		methods: map[string]struct{}{
			"Query": {}, "QueryRow": {}, "Exec": {}, "QueryContext": {},
			"QueryRowContext": {}, "ExecContext": {}, "Close": {},
		},
	},

	// os.File — the bare *File pointer is omnipresent in Go I/O code.
	"os.File": {
		pkg: "os",
		methods: map[string]struct{}{
			"Read": {}, "ReadAt": {}, "Write": {}, "WriteAt": {},
			"WriteString": {}, "Close": {}, "Name": {}, "Stat": {},
			"Sync": {}, "Truncate": {}, "Seek": {}, "Chdir": {},
			"Chmod": {}, "Chown": {}, "Fd": {}, "ReadDir": {},
			"Readdir": {}, "Readdirnames": {}, "SetDeadline": {},
			"SetReadDeadline": {}, "SetWriteDeadline": {},
		},
	},

	// chi router types — third-party but indistinguishable from stdlib
	// dispatch shape and dominate residual go-chi bug-rate (issue #103
	// target). Methods mirror chi.Router + chi.Mux; routing yields the
	// host-canonical "github.com/go-chi/chi" placeholder which is on the
	// allowlist (so the disposition is ExternalKnown).
	"chi.Mux": {
		pkg: "github.com/go-chi/chi",
		methods: map[string]struct{}{
			"Get": {}, "Post": {}, "Put": {}, "Delete": {}, "Patch": {},
			"Head": {}, "Options": {}, "Connect": {}, "Trace": {},
			"Method": {}, "MethodFunc": {}, "Handle": {}, "HandleFunc": {},
			"Mount": {}, "Group": {}, "Route": {}, "Use": {}, "With": {},
			"NotFound": {}, "MethodNotAllowed": {}, "ServeHTTP": {},
			"Find": {}, "Match": {}, "Routes": {}, "Middlewares": {},
		},
	},
	"chi.Router": {
		pkg: "github.com/go-chi/chi",
		methods: map[string]struct{}{
			"Get": {}, "Post": {}, "Put": {}, "Delete": {}, "Patch": {},
			"Head": {}, "Options": {}, "Connect": {}, "Trace": {},
			"Method": {}, "MethodFunc": {}, "Handle": {}, "HandleFunc": {},
			"Mount": {}, "Group": {}, "Route": {}, "Use": {}, "With": {},
			"NotFound": {}, "MethodNotAllowed": {}, "ServeHTTP": {},
			"Find": {}, "Routes": {}, "Middlewares": {}, "Match": {},
		},
	},

	// gin engine + context — same rationale as chi.
	"gin.Engine": {
		pkg: "github.com/gin-gonic/gin",
		methods: map[string]struct{}{
			"GET": {}, "POST": {}, "PUT": {}, "DELETE": {}, "PATCH": {},
			"HEAD": {}, "OPTIONS": {}, "Any": {}, "Handle": {},
			"Group": {}, "Use": {}, "Run": {}, "RunTLS": {},
			"NoRoute": {}, "NoMethod": {}, "ServeHTTP": {},
			"Static": {}, "StaticFS": {}, "StaticFile": {},
			"LoadHTMLFiles": {}, "LoadHTMLGlob": {},
			"SetTrustedProxies": {},
		},
	},
	"gin.Context": {
		pkg: "github.com/gin-gonic/gin",
		methods: map[string]struct{}{
			"Param": {}, "Query": {}, "DefaultQuery": {}, "PostForm": {},
			"DefaultPostForm": {}, "Bind": {}, "BindJSON": {},
			"ShouldBind": {}, "ShouldBindJSON": {}, "ShouldBindQuery": {},
			"JSON": {}, "String": {}, "HTML": {}, "XML": {}, "YAML": {},
			"Data": {}, "File": {}, "Status": {}, "Header": {},
			"AbortWithStatus": {}, "AbortWithStatusJSON": {}, "Abort": {},
			"Next": {}, "Set": {}, "Get": {}, "MustGet": {},
			"GetString": {}, "GetInt": {}, "GetBool": {},
			"Cookie": {}, "SetCookie": {}, "Redirect": {},
			"ClientIP": {}, "ContentType": {}, "FullPath": {},
			"GetHeader": {}, "Request": {}, "Writer": {},
		},
	},
	"gin.RouterGroup": {
		pkg: "github.com/gin-gonic/gin",
		methods: map[string]struct{}{
			"GET": {}, "POST": {}, "PUT": {}, "DELETE": {}, "PATCH": {},
			"HEAD": {}, "OPTIONS": {}, "Any": {}, "Handle": {},
			"Group": {}, "Use": {}, "Static": {}, "StaticFS": {},
			"StaticFile": {},
		},
	},

	// echo (labstack)
	"echo.Echo": {
		pkg: "github.com/labstack/echo",
		methods: map[string]struct{}{
			"GET": {}, "POST": {}, "PUT": {}, "DELETE": {}, "PATCH": {},
			"HEAD": {}, "OPTIONS": {}, "Any": {}, "Add": {},
			"Group": {}, "Use": {}, "Pre": {}, "Match": {},
			"Start": {}, "StartTLS": {}, "Logger": {}, "Static": {},
			"File": {}, "ServeHTTP": {}, "Routes": {},
		},
	},

	// testing.T / testing.B — primarily covered by goTestingTBareNames
	// for bare-name lookups in `_test.go` files, but also routed here when
	// the receiver_type is stamped (some test helpers take `*testing.T`
	// as a parameter explicitly).
	"testing.T": {
		pkg: "testing",
		methods: map[string]struct{}{
			"Helper": {}, "Cleanup": {}, "Setenv": {}, "TempDir": {},
			"Log": {}, "Logf": {}, "Error": {}, "Errorf": {}, "Fatal": {},
			"Fatalf": {}, "Skip": {}, "Skipf": {}, "SkipNow": {},
			"Skipped": {}, "Failed": {}, "Fail": {}, "FailNow": {},
			"Name": {}, "Run": {}, "Parallel": {}, "Deadline": {},
		},
	},
	"testing.B": {
		pkg: "testing",
		methods: map[string]struct{}{
			"Helper": {}, "Cleanup": {}, "Setenv": {}, "TempDir": {},
			"Log": {}, "Logf": {}, "Error": {}, "Errorf": {}, "Fatal": {},
			"Fatalf": {}, "Skip": {}, "Skipf": {}, "SkipNow": {},
			"Skipped": {}, "Failed": {}, "Fail": {}, "FailNow": {},
			"Name": {}, "Run": {}, "RunParallel": {}, "ResetTimer": {},
			"StopTimer": {}, "StartTimer": {}, "ReportAllocs": {},
			"ReportMetric": {}, "SetBytes": {}, "SetParallelism": {},
		},
	},
}

// goStdlibInterfaceMethod looks up (recvType, method) against the
// goStdlibInterfaceMethods catalogue and returns the canonical stdlib
// package import-path and true on a hit. recvType is expected to be the
// extractor's canonicalised form (leading `*` stripped, generic type
// parameters dropped) — `*http.Request` arrives here as `http.Request`.
// Returns ("", false) on a miss; the caller falls through to the existing
// classification heuristics.
func goStdlibInterfaceMethod(recvType, method string) (string, bool) {
	entry, ok := goStdlibInterfaceMethods[recvType]
	if !ok {
		return "", false
	}
	if _, ok := entry.methods[method]; !ok {
		return "", false
	}
	return entry.pkg, true
}

// rustBareNames is the Rust-language-gated bare-name stop-list (issue
// #108). Rust's prelude implicitly imports a fixed set of identifiers
// (Ok, Err, Some, None, Box, Vec, Result, Option, ...) into every
// source file; the extractor sees them as bare PascalCase / snake_case
// calls without an import edge, and the resolver lands them in
// bug-extractor. They cannot live in the language-agnostic
// stdlibBareNames map because names like `Ok`, `Err`, `clone`, `vec`,
// `map`, `filter` are common user identifiers in Go/JS/Python (#94
// lesson — bias to misses over false-positive synthesis). Gating to
// lang="rust" keeps the resolution scoped to Rust source entities.
//
// Three categories are included: prelude PascalCase types & traits,
// prelude lowercase methods (post-receiver-strip from x.clone() →
// clone), and prelude macros (vec!, println!, format!).
//
// Names already covered by the language-agnostic stdlibBareNames map
// (filter, format, iter, len, map, print, zip, insert) are
// deliberately omitted here — they classify globally without needing
// the Rust gate.
var rustBareNames = map[string]struct{}{
	// Prelude PascalCase — types, enums, variants, and traits.
	"Ok":           {},
	"Err":          {},
	"Some":         {},
	"None":         {},
	"Box":          {},
	"Vec":          {},
	"Result":       {},
	"Option":       {},
	"String":       {},
	"Default":      {},
	"From":         {},
	"Into":         {},
	"TryFrom":      {},
	"TryInto":      {},
	"Iterator":     {},
	"IntoIterator": {},
	"ToString":     {},
	"ToOwned":      {},
	"Clone":        {},
	"Copy":         {},
	"Debug":        {},
	"Display":      {},
	"Send":         {},
	"Sync":         {},
	"Sized":        {},
	"Drop":         {},
	"Fn":           {},
	"FnMut":        {},
	"FnOnce":       {},

	// Prelude lowercase methods — post-receiver-strip (`opt.unwrap()` →
	// `unwrap`, `s.to_string()` → `to_string`). Risky names like
	// `clone`/`get`/`push`/`pop`/`count` are common user-method
	// identifiers in other languages, but the lang="rust" gate scopes
	// the rewrite to Rust source entities only.
	"clone":             {},
	"unwrap":            {},
	"unwrap_or":         {},
	"unwrap_or_default": {},
	"unwrap_or_else":    {},
	"expect":            {},
	"into":              {},
	"as_ref":            {},
	"as_mut":            {},
	"as_str":            {},
	"to_string":         {},
	"to_owned":          {},
	"into_iter":         {},
	"collect":           {},
	"fold":              {},
	"chain":             {},
	"count":             {},
	"is_empty":          {},
	"push":              {},
	"pop":               {},
	"remove":            {},
	"get":               {},
	"contains":          {},
	"is_some":           {},
	"is_none":           {},
	"is_ok":             {},
	"is_err":            {},
	"ok":                {},
	"err":               {},
	"take":              {},
	"replace":           {},
	"swap":              {},
	"drop":              {},
	"default":           {},

	// Prelude macros (post-`!` strip). `format`/`print` are already in
	// the language-agnostic stdlibBareNames; the rest are Rust-only
	// idioms or common-enough across languages to warrant gating.
	"vec":           {},
	"println":       {},
	"eprintln":      {},
	"eprint":        {},
	"write":         {},
	"writeln":       {},
	"panic":         {},
	"todo":          {},
	"unimplemented": {},
	"unreachable":   {},
	"dbg":           {},
	"assert":        {},
	"debug_assert":  {},
	"matches":       {},

	// Actix-web framework DSL (issue #440). The Rust extractor strips
	// the receiver from a builder-chain call (`App::new().service(s)` →
	// `service`, `HttpResponse::Ok().json(x)` → `json`, `web::Path::<T>`
	// → `Path`), and the resolver can't bind the bare leaf to a local
	// entity — it lands in bug-extractor. Mirrors the Kotlin Ktor DSL
	// (#435) and Swift Vapor DSL (#436) precedents: the language gate
	// (lang == "rust") is what makes generic verbs like `service`,
	// `route`, `scope`, `body`, `json`, `start` safe — they cannot
	// shadow user methods in Go/JS/Python/Ruby/Kotlin/Swift codebases.
	//
	// Conservative selection (lesson from #94): `web` excluded — too
	// generic, collides with user variables/modules. HTTP method verbs
	// `get`/`post`/`put`/`delete`/`patch`/`head`/`options` are
	// route-builder DSL on `App`/`Resource`/`Scope` — `get` is already
	// listed above as an Option/Vec accessor; the rest are added here.
	//
	// Categories:
	//   - Actix `App`/`Resource`/`Scope` builder DSL.
	//   - `HttpResponse` factory methods and response builder verbs.
	//   - `web::Path`/`Query`/`Json`/`Form`/`Data`/`Header` extractors.
	//   - Actix actor system (`Actor`/`Handler`/`Message`/`Context`/
	//     lifecycle hooks).
	//   - HTTP method route-builder verbs (`post`, `put`, `delete`,
	//     `patch`, `head`, `options`).
	"App":               {},
	"service":           {},
	"route":             {},
	"scope":             {},
	"wrap":              {},
	"wrap_fn":           {},
	"app_data":          {},
	"default_service":   {},
	"external_resource": {},
	"configure":         {},
	"register":          {},
	// HTTP response factories and builder verbs. `Ok` and `NotFound`
	// are already covered (`Ok` in rustBareNames prelude, `NotFound`
	// in language-agnostic stdlibBareNames).
	"HttpResponse":        {},
	"BadRequest":          {},
	"InternalServerError": {},
	"Unauthorized":        {},
	"Forbidden":           {},
	"NoContent":           {},
	"Created":             {},
	"Accepted":            {},
	"body":                {},
	"json":                {},
	"finish":              {},
	"streaming":           {},
	// Web extractors. `web` deliberately omitted — too generic.
	"Path":   {},
	"Query":  {},
	"Json":   {},
	"Form":   {},
	"Data":   {},
	"Header": {},
	// Actix actor system.
	"Actor":     {},
	"Handler":   {},
	"Message":   {},
	"Context":   {},
	"Recipient": {},
	"Addr":      {},
	"Arbiter":   {},
	"System":    {},
	"start":     {},
	"started":   {},
	"stopping":  {},
	"stopped":   {},
	// HTTP method route-builder verbs (`get` already in prelude list).
	"post":    {},
	"put":     {},
	"delete":  {},
	"patch":   {},
	"head":    {},
	"options": {},
	// `data` — Actix `App::data(...)` shared-state injector. Listed
	// after the actor lifecycle hooks to keep grouping legible.
	"data": {},

	// Actix-web HttpResponse 4xx/5xx/3xx factory names (post-receiver
	// strip from `HttpResponse::TooManyRequests()` etc.) and common
	// error-builder verbs from `actix_web::error::*`. PascalCase HTTP
	// status names cannot shadow user methods in other languages.
	"TooManyRequests":             {},
	"UnsupportedMediaType":        {},
	"MethodNotAllowed":            {},
	"MovedPermanently":            {},
	"PermanentRedirect":           {},
	"TemporaryRedirect":           {},
	"SeeOther":                    {},
	"NotModified":                 {},
	"PaymentRequired":             {},
	"ServiceUnavailable":          {},
	"GatewayTimeout":              {},
	"BadGateway":                  {},
	"NotImplemented":              {},
	"PreconditionFailed":          {},
	"PreconditionRequired":        {},
	"PayloadTooLarge":             {},
	"UriTooLong":                  {},
	"RangeNotSatisfiable":         {},
	"ExpectationFailed":           {},
	"Gone":                        {},
	"LengthRequired":              {},
	"ImATeapot":                   {},
	"MisdirectedRequest":          {},
	"Locked":                      {},
	"FailedDependency":            {},
	"UpgradeRequired":             {},
	"RequestHeaderFieldsTooLarge": {},
	"ErrorBadRequest":             {},
	"ErrorInternalServerError":    {},
	"ErrorNotFound":               {},
	"ErrorRequestTimeout":         {},
	"ErrorUnauthorized":           {},
	"ErrorForbidden":              {},
	"ErrorConflict":               {},
	"ErrorPreconditionFailed":     {},
	"ErrorUnprocessableEntity":    {},
	"ErrorTooManyRequests":        {},
	"ErrorServiceUnavailable":     {},
	// Actix-web HttpResponse builder verbs.
	"MessageResult": {},
	"ThinData":      {},
	"ClientSession": {},

	// Rust wave (S19+) verbs — continuation. Tokio + std + popular-
	// crate verbs commonly stripped of their receiver. Each entry is a method or free
	// function with a low natural collision rate against user-defined
	// methods in other languages (safer-bias #94) and a high
	// frequency in real tokio / actix-examples / mini-redis bug-
	// extractor samples. The lang=="rust" gate keeps these scoped to
	// Rust source; same-named user methods in other ecosystems are
	// not affected.
	//
	// Tokio macros + spawning primitives.
	"spawn":          {},
	"spawn_blocking": {},
	"spawn_local":    {},
	"spawn_on":       {},
	"select":         {},
	"join":           {},
	"try_join":       {},
	"pin":            {},
	"pin_mut":        {},
	"sleep":          {},
	"timeout":        {},
	"interval":       {},
	"yield_now":      {},
	"block_on":       {},
	"block_in_place": {},
	// Tokio sync / shared-state idioms.
	"try_lock":       {},
	"try_acquire":    {},
	"try_send":       {},
	"try_recv":       {},
	"recv":           {},
	"subscribe":      {},
	"notify":         {},
	"notify_one":     {},
	"notify_waiters": {},
	"acquire":        {},
	"release":        {},
	"unparked":       {},
	"unpark":         {},
	"park":           {},
	"wake":           {},
	"wake_by_ref":    {},
	"wake_all":       {},
	"will_wake":      {},
	// std / tokio common reader/writer + iterator verbs (post-receiver).
	"read_to_string": {},
	"read_to_end":    {},
	"read_exact":     {},
	"read_buf":       {},
	"read_u8":        {},
	"write_all":      {},
	"write_u8":       {},
	"write_vectored": {},
	"flush":          {},
	"close":          {},
	"open":           {},
	"create":         {},
	"copy_to":        {},
	"copy_from":      {},
	"put_slice":      {},
	// std::time / std::path / std::env idioms post-receiver.
	"as_secs":         {},
	"as_millis":       {},
	"as_micros":       {},
	"as_nanos":        {},
	"as_path":         {},
	"as_os_str":       {},
	"to_path_buf":     {},
	"to_string_lossy": {},
	"file_name":       {},
	"extension":       {},
	"parent":          {},
	"join_paths":      {},
	"current_dir":     {},
	"set_current_dir": {},
	"current_exe":     {},
	"args_os":         {},
	"vars_os":         {},
	// std::process verbs.
	"output":           {},
	"status":           {},
	"wait_with_output": {},
	"kill":             {},
	// std::result + Option helpers.
	"unwrap_unchecked": {},
	"unwrap_err":       {},
	"expect_err":       {},
	"map_err":          {},
	"map_or":           {},
	"map_or_else":      {},
	"and_then":         {},
	"or_else":          {},
	"ok_or":            {},
	"ok_or_else":       {},
	"transpose":        {},
	"flatten":          {},
	"copied":           {},
	"cloned":           {},
	"as_deref":         {},
	"as_deref_mut":     {},
	"take_if":          {},
	// Iterator combinators commonly stripped of receiver.
	"map_while":  {},
	"step_by":    {},
	"skip_while": {},
	"take_while": {},
	"scan":       {},
	"peekable":   {},
	"by_ref":     {},
	"min_by":     {},
	"max_by":     {},
	"min_by_key": {},
	"max_by_key": {},
	"chunks":     {},
	"windows":    {},
	"enumerate":  {},
	"zip":        {},
	"rev":        {},
	"sum":        {},
	"product":    {},
	"position":   {},
	"any":        {},
	"all":        {},
	"find":       {},
	"find_map":   {},
	"fuse":       {},
	"cycle":      {},
	// tracing + log crate macros (post-`!` strip).
	"trace":           {},
	"info":            {},
	"warn":            {},
	"error":           {},
	"info_span":       {},
	"warn_span":       {},
	"error_span":      {},
	"debug_span":      {},
	"trace_span":      {},
	"instrument":      {},
	"in_current_span": {},
	"in_scope":        {},
	// Common builder-pattern with_* verbs from tracing-subscriber /
	// rustls / aws_config / opentelemetry / clap etc.
	"with_writer":                         {},
	"with_max_level":                      {},
	"with_target":                         {},
	"with_ansi":                           {},
	"with_thread_names":                   {},
	"with_thread_ids":                     {},
	"with_file":                           {},
	"with_line_number":                    {},
	"with_env_filter":                     {},
	"with_default":                        {},
	"with_default_directive":              {},
	"with_filter":                         {},
	"with_state":                          {},
	"with_capacity":                       {},
	"with_status":                         {},
	"with_body":                           {},
	"with_header":                         {},
	"with_uri":                            {},
	"with_uri_str":                        {},
	"with_method":                         {},
	"with_tracer":                         {},
	"with_trace_config":                   {},
	"with_sampler":                        {},
	"with_id_generator":                   {},
	"with_exporter":                       {},
	"with_cert_resolver":                  {},
	"with_client_cert_verifier":           {},
	"with_single_cert":                    {},
	"with_no_client_auth":                 {},
	"with_root_certificates":              {},
	"with_safe_default_cipher_suites":     {},
	"with_safe_default_kx_groups":         {},
	"with_safe_default_protocol_versions": {},
	"with_scheduler":                      {},
	"with_ready":                          {},
	"with_time":                           {},
	"with_mut":                            {},
	"with_current":                        {},
	"without_time":                        {},
	// try_* builder verbs (chrono / clap / aws-config).
	"try_init":          {},
	"try_init_from_env": {},
	"try_build":         {},
	"try_seconds":       {},
	"try_hours":         {},
	"try_minutes":       {},
	"try_milliseconds":  {},
	// Mpsc / channels.
	"unbounded_channel": {},
	// Common bare functions stripped of crate path.
	"zeroed":        {},
	"size_of":       {},
	"align_of":      {},
	"replace_with":  {},
	"swap_with":     {},
	"forget":        {},
	"drop_in_place": {},
	"transmute":     {},

	// ---------------------------------------------------------------------
	// Rust wave-2 (S20+) — additions curated from real diagnostic samples
	// on actix-examples / tokio / mini-redis. Each entry has lang="rust"
	// gating (#94 safer-bias rule) and a low natural-method-collision rate
	// against user-defined methods in other ecosystems.
	// ---------------------------------------------------------------------

	// actix-web 4.x test-utility + service-builder verbs (post receiver
	// strip from `test::init_service`, `test::TestRequest::get().to_http_request()`,
	// `srv.call_service(req).await` etc.). Distinctive enough to gate to rust.
	"init_service":            {},
	"call_service":            {},
	"call_and_read_body":      {},
	"call_and_read_body_json": {},
	"to_http_request":         {},
	"to_request":              {},
	"to_srv_request":          {},
	"to_srv_response":         {},
	"read_body":               {},
	"set_form":                {},
	"set_json":                {},
	"set_payload":             {},
	"set_body":                {},
	"insert_header":           {},
	"append_header":           {},
	"default_filter_or":       {},
	"customize":               {},
	"respond_to":              {},
	"map_into_right_body":     {},
	"map_into_left_body":      {},
	"map_into_boxed_body":     {},
	"map_body":                {},
	"into_body":               {},
	"into_response":           {},
	"boxed_local":             {},
	"extract":                 {},
	"resource":                {},
	"guard":                   {},
	"using_status_code":       {},
	"no_chunking":             {},
	"content_type":            {},
	"content_length":          {},
	"no_decompress":           {},
	"request_from":            {},
	"send_stream":             {},
	"bytes_stream":            {},
	"wrap_stream":             {},
	"shutdown_timeout":        {},
	"disable_signals":         {},
	"show_files_listing":      {},
	"playground_source":       {},
	"subscription_endpoint":   {},
	"graphiql_source":         {},
	"data_unchecked":          {},
	"from_registry":           {},
	"issue_system_sync":       {},
	"into_actor":              {},
	"do_send":                 {},
	"run_interval":            {},

	// HTTP request/response helpers (post receiver-strip on actix-web /
	// reqwest / awc / hyper request builders).
	"headers_mut":             {},
	"extensions_mut":          {},
	"query_string":            {},
	"is_success":              {},
	"from_response":           {},
	"get_header":              {},
	"get_ref":                 {},
	"from_static":             {},
	"into_parts":              {},
	"to_bytes":                {},
	"to_str":                  {},
	"to_vec":                  {},
	"as_bytes":                {},
	"as_slice":                {},
	"into_inner":              {},
	"into_future":             {},
	"into_item":               {},
	"max_continuation_size":   {},
	"max_frame_size":          {},
	"aggregate_continuations": {},
	"unread_data":             {},

	// Database / ORM verbs — diesel + sqlx + mysql + mongodb common APIs.
	// Distinctive multi-word names; gated to rust.
	"fetch_all":              {},
	"fetch_optional":         {},
	"fetch_one":              {},
	"query_as":               {},
	"query_async":            {},
	"query_map":              {},
	"query_drop":             {},
	"exec_drop":              {},
	"exec_first":             {},
	"insert_into":            {},
	"insert_one":             {},
	"get_result":             {},
	"get_results":            {},
	"as_select":              {},
	"as_returning":           {},
	"returning":              {},
	"from_row_ref":           {},
	"sql_table_fields":       {},
	"create_index":           {},
	"last_insert_id":         {},
	"acquire_timeout":        {},
	"get_connection":         {},
	"get_connection_manager": {},
	"get_conn":               {},
	"from_url":               {},
	"from_opts":              {},

	// rand / uuid common factory verbs.
	"new_v4":      {},
	"new_v7":      {},
	"random_bool": {},
	"rng":         {},

	// tokio + std numeric / pointer / unsafe stdlib methods (post receiver-strip).
	"from_secs":           {},
	"from_millis":         {},
	"from_micros":         {},
	"from_nanos":          {},
	"from_raw_os_error":   {},
	"from_raw":            {},
	"from_raw_fd":         {},
	"from_raw_handle":     {},
	"from_raw_socket":     {},
	"from_pathname":       {},
	"from_abstract_name":  {},
	"from_waker":          {},
	"from_std":            {},
	"from_slice":          {},
	"from_str":            {},
	"from_utf8":           {},
	"from_utf8_lossy":     {},
	"from_bytes":          {},
	"from_pem":            {},
	"from_fn":             {},
	"from_env_lossy":      {},
	"from_default_env":    {},
	"from_pem_file":       {},
	"from_pem_passphrase": {},
	"into_raw":            {},
	"into_raw_fd":         {},
	"into_raw_socket":     {},
	"into_async_read":     {},
	"into_boxed_slice":    {},
	"into_guarded":        {},
	"into_actor_2":        {}, // alias guard — extra safety vs partial match

	// Pointer / slice / mem unsafe helpers (post receiver-strip).
	"as_mut_ptr":               {},
	"as_pin_mut":               {},
	"as_ptr":                   {},
	"as_raw_fd":                {},
	"as_raw_handle":            {},
	"as_uninit_slice_mut":      {},
	"null_mut":                 {},
	"new_unchecked":            {},
	"into_inner_unchecked":     {},
	"slice_from_raw_parts_mut": {},
	"from_raw_parts_mut":       {},
	"copy_from_slice":          {},
	"copy_from_nonoverlapping": {},
	"copy_from_bufs":           {},
	"write_bytes":              {},
	"spare_capacity_mut":       {},
	"extend_from_slice":        {},
	"split_at":                 {},
	"chunks_vectored":          {},
	"discard_read":             {},
	"addr_of":                  {},
	"addr_of_mut":              {},
	"addr_of_pointers":         {},
	"addr_of_owned":            {},
	"addr_of_values":           {},
	"addr_of_header":           {},
	"dangling":                 {},
	"offset_from":              {},
	"cast_mut":                 {},
	"cast_const":               {},
	"clone_from":               {},
	"into_bytes":               {},
	"put_slice_no_alias":       {}, // tokio-bytes
	"put_bytes":                {},
	"put_u16":                  {},
	"read_u16":                 {},
	"set_position":             {},
	"has_remaining":            {},
	"has_remaining_mut":        {},
	"swap_remove":              {},

	// Integer arithmetic helpers (post receiver-strip).
	"wrapping_sub":      {},
	"wrapping_add":      {},
	"wrapping_mul":      {},
	"wrapping_shl":      {},
	"saturating_sub":    {},
	"saturating_add":    {},
	"trailing_zeros":    {},
	"leading_zeros":     {},
	"rotate_right":      {},
	"next_power_of_two": {},
	"ilog2":             {},
	"is_zero":           {},
	"powi":              {},
	"powf":              {},
	"pow":               {},

	// Format / debug helpers (post-receiver).
	"format_args":           {},
	"debug_struct":          {},
	"finish_non_exhaustive": {},
	"field":                 {},
	"pad":                   {},
	"write_str":             {},
	"rsplit_once":           {},
	"split_once":            {},
	"splitn":                {},
	"strip_prefix":          {},
	"starts_with":           {},
	"ends_with":             {},
	"to_lowercase":          {},
	"to_uppercase":          {},
	"is_some_and":           {},

	// Iterator / collection helpers.
	"now_or_never":       {},
	"pending":            {},
	"get_or_insert_with": {},
	"map_ok":             {},
	"flat_map":           {},
	"filter_map":         {},
	"borrow_mut":         {},
	"borrow_raw":         {},
	"retain":             {},
	"reverse":            {},
	"valid_up_to":        {},
	"check":              {},
	"poll_unpin":         {},
	"poll_fn":            {},
	"poll_proceed":       {},
	"poll_and_cancel":    {},
	"poll_elapsed":       {},
	"now_or_never_2":     {}, // alias guard
	"ready":              {},

	// tokio runtime / scheduler / test-helper macros & helpers
	// (post-`!` strip or receiver-strip).
	"assert_pending":         {},
	"assert_ready":           {},
	"assert_ready_ok":        {},
	"assert_ready_err":       {},
	"assert_ok":              {},
	"assert_err":             {},
	"check_static_val":       {},
	"check_send_sync_val":    {},
	"check_static":           {},
	"check_send_sync":        {},
	"check_unpin":            {},
	"check_send":             {},
	"check_sync":             {},
	"if_loom":                {},
	"cfg_unstable_metrics":   {},
	"cfg":                    {},
	"debug_assert_eq":        {},
	"debug_assert_ne":        {},
	"thread_local":           {},
	"catch_unwind":           {},
	"resume_unwind":          {},
	"panicking":              {},
	"noop_waker":             {},
	"noop_waker_ref":         {},
	"new_count_waker":        {},
	"const_mutex":            {},
	"const_new_closed":       {},
	"spin_loop":              {},
	"thread_id":              {},
	"available_parallelism":  {},
	"caller_location":        {},
	"unreachable_unchecked":  {},
	"build_threaded_runtime": {},
	"new_current_thread":     {},
	"enable_all":             {},
	"enable_io":              {},
	"enable_time":            {},
	"enable_io_uring":        {},
	"new_unstable":           {},

	// tokio-bytes / Buf / BufMut common methods.
	"chunk":             {},
	"remaining":         {},
	"advance":           {},
	"reserve":           {},
	"fill":              {},
	"try_stream":        {},
	"notified":          {},
	"acquire_owned":     {},
	"available_permits": {},
	"forget_permits":    {},
	"clear_readiness":   {},
	"closed":            {},
	"is_closed":         {},
	"is_cancelled":      {},
	"is_supported":      {},
	"made_progress":     {},
	"try_unwrap":        {},
	"try_into_2":        {}, // alias guard
	"try_wait":          {},
	"try_has_changed":   {},
	"submit_or_remove":  {},
	"submit_and_wait":   {},
	"submission":        {},
	"completion":        {},
	"user_data":         {},

	// Windows API common imports (Rust FFI bindings — distinctive PascalCase
	// or PascalCaseW endings; collisions with user names in other languages
	// are vanishingly unlikely).
	"CreateNamedPipeW":            {},
	"CreateFileW":                 {},
	"SetNamedPipeHandleState":     {},
	"GetNamedPipeInfo":            {},
	"DuplicateHandle":             {},
	"GetCurrentProcess":           {},
	"RegisterWaitForSingleObject": {},
	"UnregisterWaitEx":            {},
	"SetConsoleCtrlHandler":       {},
	"SourceFd":                    {},

	// std::os::unix syscalls / libc bindings.
	"getsockopt":    {},
	"getpeereid":    {},
	"ucred_free":    {},
	"ucred_getpid":  {},
	"ucred_getegid": {},
	"ucred_geteuid": {},
	"getpeerucred":  {},
	"fstat":         {},
	"raw_os_error":  {},
	"last_os_error": {},

	// tracing / opentelemetry / metrics helpers (post-receiver).
	"install_recorder":        {},
	"install_simple":          {},
	"install_default":         {},
	"set_buckets_for_metric":  {},
	"describe_histogram":      {},
	"set_text_map_propagator": {},
	"new_exporter":            {},
	"new_pipeline":            {},
	"default_provider":        {},

	// askama / handlebars / minijinja / sailfish template helpers.
	"render_once": {},

	// rustls / acme-rfc8555 helpers.
	"download_cert":                   {},
	"register_account":                {},
	"load_account":                    {},
	"new_order":                       {},
	"confirm_validations":             {},
	"authorizations":                  {},
	"http_challenge":                  {},
	"http_token":                      {},
	"http_proof":                      {},
	"acme_private_key_pem":            {},
	"private_key_der":                 {},
	"private_key_from_pem_passphrase": {},
	"pem_file_iter":                   {},
	"pem_slice_iter":                  {},
	"any_supported_type":              {},

	// async-graphql / juniper common helpers (post receiver-strip).
	"graphql_value": {},

	// Common module-level fns (post path-strip on free fns or static methods).
	"current_task_id":                  {},
	"set_current_task_id":              {},
	"new_task":                         {},
	"new_unnamed":                      {},
	"new_with_interest":                {},
	"new_closed":                       {},
	"new_header":                       {},
	"get_next_id":                      {},
	"set_scheduler":                    {},
	"seed_generator":                   {},
	"sealed":                           {}, // module-name fold
	"actor":                            {},
	"connect_addr":                     {},
	"create_clock":                     {},
	"create_time_driver":               {},
	"create_io_stack":                  {},
	"get_orphan_queue":                 {},
	"get_type_id":                      {},
	"start_processing_scheduled_tasks": {},
	"end_processing_scheduled_tasks":   {},
	"trace_poll_op":                    {},
	"transition_result_to_poll_future": {},
	"call_inner":                       {},
	"inner_read":                       {},
	"inner_flush":                      {},
	"expect_inner_write":               {},
	"expect_inner_read":                {},
	"expect_inner_seek":                {},
	"expect_current_thread":            {},
	"expect_set_len":                   {},
	"steal_tasks":                      {},
	"push_orphan":                      {},
	"drop_join_handle_slow":            {},
	"deregister":                       {},
	"compare_exchange_weak":            {},
	"increment_strong_count":           {},
	"strong_count":                     {},
	"downgrade":                        {},
	"capture":                          {},
	"injection_queue_depth":            {},
	"assert_bucket_eq":                 {},
	"assert_owner":                     {},
	"notify_waiters_is_atomic_variant": {},
	"notify_waiters_poll_consistency_variant":      {},
	"notify_waiters_poll_consistency_many_variant": {},
	"timed_out":            {},
	"park_thread_timeout":  {},
	"park_timeout":         {},
	"can_auto_advance":     {},
	"inhibit_auto_advance": {},
	"delay_poll":           {},
	"did_wake":             {},
	"uninterruptibly":      {},
	"of":                   {}, // common assoc fn (e.g., AtomicWaker::of)

	// async-graphql + jwt + s3 / minio helpers.
	"get_object":    {},
	"delete_object": {},
	"bucket":        {},

	// Common PascalCase variants & types frequently appearing bare
	// (Frame / Connection / Buf enum variants in mini-redis, plus tokio test types).
	"Bulk":              {},
	"Simple":            {},
	"Integer":           {},
	"Array":             {},
	"Other":             {},
	"Joined":            {},
	"Rooms":             {},
	"ON":                {},
	"OLD":               {},
	"Found":             {},
	"Right":             {},
	"Left":              {},
	"Pkcs8":             {},
	"Full":              {},
	"Linear":            {},
	"Start":             {},
	"Clear":             {},
	"Reset":             {},
	"Closed":            {},
	"Lagged":            {},
	"Polled":            {},
	"Pending":           {},
	"Completed":         {},
	"Initialize":        {},
	"Running":           {},
	"Busy":              {},
	"Cancelled":         {},
	"Current":           {},
	"Iter":              {},
	"Remove":            {},
	"PendingOverflowed": {},
	"PanickingWaker":    {},
	"AssertUnwindSafe":  {},
	"ReadBuf":           {},
	"ReadVec":           {},
	"LogLegacy":         {},
	"SignalReaper":      {},
	"Uring":             {},
	"ClientSession_2":   {}, // dup guard
	"Fd":                {},
	"One":               {},
	"Value":             {},
	"ParseFromString":   {},
	"SerializeToString": {},

	// ---------------------------------------------------------------------
	// Rust wave-2 — additional bare-name follow-ups from pass-3 samples.
	// ---------------------------------------------------------------------
	// HashMap / DashMap / sync wrappers.
	"contains_key":      {},
	"get_mut":           {},
	"or_default":        {},
	"fetch_add":         {},
	"clone_into":        {},
	"downcast_ref":      {},
	"include_str":       {},
	"prepare":           {},
	"split_to":          {},
	"duration_since":    {},
	"from_u16":          {},
	"as_u16":            {},
	"remove_dir_all":    {},
	"create_dir":        {},
	"open_async":        {},
	"from_file":         {},
	"init_from_env":     {},
	"naive_utc":         {},
	"naive_local":       {},
	"peer_addr":         {},
	"peer_certificates": {},
	"set_query":         {},
	"set_path":          {},
	"wrap_err":          {},
	"hash_encoded":      {},
	"rfc9106_low_mem":   {},
	"create_p256_key":   {},
	"from_millis_2":     {}, // alias guard
	// Tokio / actor / redis verbs.
	"join_all": {},
	"set_ex":   {},
	"mset":     {},
	"mget":     {},
	"find_one": {},
	"is_data":  {},
	// Common ambiguous-but-rust-gated verbs.
	"block":        {},
	"timestamp":    {},
	"params":       {},
	"keys":         {},
	"values":       {},
	"entry":        {},
	"empty":        {},
	"append":       {},
	"add":          {},
	"with":         {},
	"layer":        {},
	"registry":     {},
	"load":         {},
	"var":          {},
	"validate":     {},
	"refresh":      {},
	"finalize":     {},
	"renew":        {},
	"purge":        {},
	"extensions":   {},
	"recipient":    {},
	"address":      {},
	"certificate":  {},
	"limit":        {},
	"optional":     {},
	"plaintext":    {},
	"fallback":     {},
	"auto":         {},
	"binary":       {},
	"pong":         {},
	"encrypt":      {},
	"decrypt":      {},
	"handler":      {},
	"channel_2":    {}, // alias guard (channel already added)
	"doc":          {},
	"unique":       {},
	"builder":      {},
	"details":      {},
	"preference":   {},
	"del":          {},
	"to_2":         {}, // alias guard
	"method":       {},
	"header":       {},
	"parse":        {},
	"lock":         {},
	"ttl":          {},
	"bind":         {},
	"ip":           {},
	"local_addr_2": {}, // alias guard
}

// javaBareNames is the Java-language-gated bare-name stop-list (issue
// #105). After the Java extractor strips the receiver from a method
// call (`repo.findById(id)` → `findById`, `optional.orElseThrow()` →
// `orElseThrow`), the resolver sees a bare name that can't be matched
// to a local entity and lands in bug-extractor. The names below are
// JDK exception classes plus the most distinctive Spring Data /
// Spring MVC / Spring binding helper methods.
//
// Conservative selection rule (lesson from #94): include only names
// whose plausible-user-method-collision rate is low. Generic
// getters/setters (`getId`, `getName`, `getValue`, `setName`,
// `setValue`) and ubiquitous functional verbs (`map`, `filter`,
// `forEach`, `collect`, `stream`) are deliberately EXCLUDED — the
// proper resolution for those is cross-class receiver binding (the
// (A) follow-up to issue #105). When doubt exists, omit; a missed
// external is strictly better than a synthesised placeholder
// shadowing a real missing-resolution bug.
//
// Categories (curated, not exhaustive):
//   - JDK stdlib exception class names (constructed bare or referenced
//     bare in `throws`/`catch` clauses post-receiver-strip).
//   - JDK Optional helpers — only the four where collision risk is
//     low. `map`/`flatMap` excluded because every user collection
//     method named `map` would be shadowed.
//   - Spring Data JPA repository methods with distinctive shapes
//     (`findById`, `saveAndFlush`, etc.). Verbs like `delete` /
//     `find` alone are excluded; the JpaRepository names listed
//     here have a low natural-method-collision rate.
//   - Spring `BindingResult` validation helpers.
//   - Spring `Model` / `RedirectAttributes` flash-attribute helpers.
//   - Spring Data `Pageable` / `Page` accessors.
var javaBareNames = map[string]struct{}{
	// JDK stdlib exception classes — constructor and reference forms.
	"IllegalArgumentException":      {},
	"NullPointerException":          {},
	"IllegalStateException":         {},
	"UnsupportedOperationException": {},
	"RuntimeException":              {},
	"IndexOutOfBoundsException":     {},
	"ClassCastException":            {},
	"NumberFormatException":         {},
	"ArithmeticException":           {},
	"IOException":                   {},
	"FileNotFoundException":         {},
	"InterruptedException":          {},
	"Error":                         {},
	"Throwable":                     {},
	// `Exception` is already covered by the language-agnostic
	// stdlibBareNames map (it's also a Python builtin), so it does
	// NOT need to live here.

	// JDK java.util.Optional helpers — the four with low collision
	// risk. `map`/`flatMap`/`filter` are deliberately excluded:
	// every Java codebase that does anything with collections has
	// at least one user method named `map` or `filter`, and the
	// language gate alone is not strong enough.
	"orElseThrow": {},
	"orElse":      {},
	"ifPresent":   {},
	"isPresent":   {},

	// Spring Data JPA repository methods. Distinctive shapes only —
	// generic verbs (`find`, `delete`, `update`) are excluded.
	"findById":     {},
	"findAll":      {},
	"findAllById":  {},
	"save":         {},
	"saveAll":      {},
	"saveAndFlush": {},
	"deleteById":   {},
	"deleteAll":    {},
	"existsById":   {},
	"count":        {},

	// Spring BindingResult validation helpers.
	"hasErrors":     {},
	"rejectValue":   {},
	"getFieldError": {},

	// Spring Model / RedirectAttributes flash-attribute helpers.
	// `addAttribute` is generic enough to cover some user code
	// collisions but the lang="java" gate keeps the rewrite scoped
	// to Java sources only.
	"addFlashAttribute": {},
	"addAttribute":      {},

	// Spring Data Pageable / Page accessors.
	"getTotalElements": {},
	"getTotalPages":    {},
	"getNumber":        {},
	"getSize":          {},
	"hasNext":          {},
	"hasPrevious":      {},

	// Issue kafka-fix-w3 — JDK java.lang.* / java.util.* / java.io.*
	// types that are imported implicitly (java.lang) or commonly bare-
	// referenced after receiver-strip. Constructor calls land as bare
	// names: `new Properties()` → `Properties`, `new HashMap<>()` →
	// `HashMap`. These are JDK-only Pascal-case identifiers — the
	// language gate to "java" prevents collision with same-named user
	// classes in JS/Go/C# etc.
	"Properties":      {},
	"HashMap":         {},
	"LinkedHashMap":   {},
	"TreeMap":         {},
	"ArrayList":       {},
	"LinkedList":      {},
	"HashSet":         {},
	"TreeSet":         {},
	"LinkedHashSet":   {},
	"Thread":          {},
	"CountDownLatch":  {},
	"Semaphore":       {},
	"AtomicInteger":   {},
	"AtomicLong":      {},
	"AtomicBoolean":   {},
	"AtomicReference": {},
	"StringBuilder":   {},
	"StringBuffer":    {},
	// `Optional` deliberately omitted — pythonBareNames has it gated
	// to Python; cross-lang gate test forbids overlap.
	"Collections": {},
	"Arrays":      {},
	"Objects":     {},
	"System":      {}, // System.out / System.err receiver-strip
	"Math":        {},
	"Integer":     {}, // boxed numerics — Integer.parseInt etc.
	"Long":        {},
	"Double":      {},
	"Float":       {},
	"Boolean":     {},
	"Short":       {},
	"Byte":        {},
	"Character":   {},
	"String":      {}, // String.format / String.valueOf
	"Class":       {},
	"Object":      {},

	// Common JDK bare-method calls after receiver-strip (java.lang.Object
	// + java.io.PrintStream): `obj.toString()`, `obj.equals(x)`,
	// `obj.hashCode()`, `e.printStackTrace()`, `System.out.println(...)`.
	// Distinctive enough that the language gate alone is sufficient.
	"toString":        {},
	"hashCode":        {},
	"equals":          {},
	"getClass":        {},
	"printStackTrace": {},
	// `println` deliberately omitted — test TestRustBareNames_
	// NotClassifiedForOtherLanguages locks this to lang="rust" only.
	"getMessage": {},
	"getCause":   {},
	// `getName` deliberately omitted — issue #105 rejects generic
	// getters because they collide with user entity classes. The
	// resolver's cross-class receiver binding is the right channel.
	"getPath":                 {},
	"getAbsolutePath":         {},
	"addShutdownHook":         {},
	"addShutdownHookAndBlock": {},
	"currentThread":           {},
	"getResourceAsStream":     {},
	"getClassLoader":          {},
	"getSimpleName":           {},
	"getCanonicalName":        {},
	"toLowerCase":             {},
	"toUpperCase":             {},
	"toInstant":               {},
	"interrupt":               {},
	"isInterrupted":           {},
	"parseInt":                {}, // Integer.parseInt receiver-stripped (also covered by java.lang receiver fold, this catches double-strip)
	"parseLong":               {},
	"parseDouble":             {},
	"parseBoolean":            {},
	"valueOf":                 {},
	"printf":                  {}, // System.out.printf after double-strip

	// Issue kafka-fix-w3 — Apache Kafka / Kafka Streams DSL types and
	// verbs that the Java extractor emits bare after receiver-strip.
	// Constructor calls (`new KafkaStreams(topology, props)` →
	// `KafkaStreams`), static accessors (`Serdes.String()` strips to
	// `Serdes`), and DSL fluent verbs on builder/stream/table receivers
	// (`builder.stream(...).groupByKey().windowedBy(...)` strips each
	// chained call to its leaf). All Pascal-case types are Kafka-specific
	// enough that the lang="java" gate prevents collision; the lowercase
	// DSL verbs are gated by appearing in the kafka import set already
	// (the new import-leaf bare-name folding above handles the type
	// cases, this list catches the verbs whose receivers are inferred).
	"StreamsBuilder":       {},
	"KafkaStreams":         {},
	"KafkaProducer":        {},
	"KafkaConsumer":        {},
	"ProducerRecord":       {},
	"ConsumerRecord":       {},
	"ConsumerRecords":      {},
	"TopologyTestDriver":   {},
	"TestInputTopic":       {},
	"TestOutputTopic":      {},
	"TestUtils":            {},
	"KeyValue":             {},
	"Stores":               {},
	"Materialized":         {},
	"Produced":             {},
	"Consumed":             {},
	"Serialized":           {},
	"Grouped":              {},
	"Joined":               {},
	"Suppressed":           {},
	"JoinWindows":          {},
	"TimeWindows":          {},
	"SessionWindows":       {},
	"SlidingWindows":       {},
	"Windowed":             {},
	"QueryableStoreTypes":  {},
	"StoreQueryParameters": {},
	"StreamsConfig":        {},
	"ConsumerConfig":       {},
	"ProducerConfig":       {},
	"AdminClient":          {},
	"NewTopic":             {},
	"TopicPartition":       {},
	"Topology":             {},
	"Bytes":                {},

	// Kafka Streams DSL verbs (receiver-stripped from KStream/KTable/
	// KGroupedStream/KGroupedTable). All distinctive enough that the
	// java gate is sufficient.
	"groupByKey":    {},
	"windowedBy":    {},
	"selectKey":     {},
	"mapValues":     {},
	"flatMapValues": {},
	"toStream":      {},
	"branch":        {},
	"foreach":       {},
	"aggregate":     {},
	// `to`, `through`, `merge`, `peek`, `transform`, `transformValues`,
	// `process`, `filter`, `filterNot`, `flatMap`, `groupBy`, `reduce`,
	// `join`, `leftJoin`, `outerJoin`, `count`, `suppress` deliberately
	// EXCLUDED — they collide with generic user-method names; the
	// import-leaf bare-name fold above is the right precision channel
	// when the receiver is a Kafka-streams type, but we don't have
	// receiver-type info at this layer, so safer-bias rejects.

	// Apache commons-cli — receiver-stripped builder pattern.
	// `Option.builder("...").longOpt("x").hasArg().desc("...").build()`
	// strips each chained call to its leaf identifier. These names are
	// distinctive (commons-cli specific) but `build` collides with too
	// many builder patterns; included anyway because the lang gate keeps
	// it scoped to Java sources, and `StreamsBuilder.build()` /
	// `Topology.build()` also benefit (both fold to ext:org.apache.kafka
	// via the import-leaf path when receivers are present, but bare
	// `build` after a chain catches the residual).
	"Options":           {},
	"Option":            {},
	"CommandLine":       {},
	"CommandLineParser": {},
	"DefaultParser":     {},
	"HelpFormatter":     {},
	"longOpt":           {},
	"hasArg":            {},
	"desc":              {},
	"isRequired":        {},
	"hasOption":         {},
	"getOptionValue":    {},
	"printHelp":         {},

	// Time API
	"Duration":  {},
	"Instant":   {},
	"toMillis":  {},
	"ofMillis":  {},
	"ofSeconds": {},
	"ofMinutes": {},

	// Issue java-spring-petclinic-wave — JCache (javax.cache / JSR-107)
	// configuration helpers used by Spring's @EnableCaching support.
	// Distinctive JCache names (lang gate is sufficient).
	"setStatisticsEnabled": {},
	"setManagementEnabled": {},
	"setStoreByValue":      {},
	"setTypes":             {},
	"createCache":          {},
	"cacheConfiguration":   {},
	"getCache":             {},
	"destroyCache":         {},

	// Spring AOT — RuntimeHints registrar API
	// (org.springframework.aot.hint.*). Receiver-stripped from
	// `hints.reflection().registerType(...)`,
	// `hints.resources().registerPattern(...)`.
	"registerType":          {},
	"registerPattern":       {},
	"registerResource":      {},
	"registerProxy":         {},
	"registerSerialization": {},
	"reflection":            {},
	"resources":             {},
	"serialization":         {},
	"proxies":               {},
	"reflectionHints":       {},
	"resourceHints":         {},
	"isAssignableFrom":      {}, // Class.isAssignableFrom — JDK reflection

	// JDK java.util.stream / Stream chain leaves that are distinctive
	// enough that the lang="java" gate prevents user-method
	// shadowing. `map`/`filter`/`flatMap`/`reduce` are deliberately
	// EXCLUDED per #105 (user-method collision risk).
	"allMatch":           {},
	"anyMatch":           {},
	"noneMatch":          {},
	"toList":             {},
	"toUnmodifiableList": {},
	"toUnmodifiableSet":  {},
	"toUnmodifiableMap":  {},
	"toArray":            {},

	// Java 8 Optional.orElseGet (low collision, completes the
	// Optional set already partially present).
	"orElseGet":       {},
	"ifPresentOrElse": {},
	"or":              {},

	// Refs #44 — Spring MVC ResponseEntity builder chain leaf methods.
	// Spring MVC controller methods follow the pattern:
	//
	//   ResponseEntity.notFound().build()
	//   ResponseEntity.noContent().build()
	//   ResponseEntity.status(HttpStatus.CREATED).body(entity)
	//   ResponseEntity.ok().body(entity)
	//   ResponseEntity.badRequest().body(error)
	//
	// The Java extractor receiver-strips each chained call to its leaf
	// identifier. The intermediate receiver (the `BodyBuilder` /
	// `HeadersBuilder` return value) has no statically knowable type at
	// extractor time, so the leaf (`build`, `body`, ...) lands as a bare
	// name and falls into bug-extractor without this entry.
	//
	// Selection rule (#105 safer-bias): included only when the Java
	// language gate makes user-method collision risk acceptably low.
	// `build` is the highest-collision name but it is gated here to
	// lang="java" — a user-defined `myBuilder.build()` would produce a
	// real CALLS edge to a project entity (resolved via cross-class
	// receiver binding), not a bare leaf stub; bare `build` only
	// survives when the receiver chain has no statically determinable
	// type, which is exactly the Spring ResponseEntity / builder-DSL
	// case. `body` is Spring-specific as a response-builder leaf verb
	// with the Java gate; other meanings are guarded by the same
	// receiver-type logic. `header` / `headers` / `contentType` are
	// HTTP-response builder methods; the Java gate is sufficient.
	"build":       {},
	"body":        {},
	"header":      {},
	"headers":     {},
	"contentType": {},
}

// javaTestBareNames is the Java test-file-gated bare-name stop-list
// (issue #120). MockMvc, JUnit Jupiter, AssertJ, Mockito, and
// Hamcrest all expose fluent-API entry points that the Java extractor
// strips to a bare leaf identifier when the receiver is the return
// value of an upstream fluent call:
//
//	mockMvc.perform(get("/x"))
//	    .andExpect(status().isOk())
//	    .andExpect(view().name("ok"));
//
// Static-typing the receiver chain is out of scope; instead we
// allow-list the leaf names and gate them on a Java test-file path so
// production code with same-named user methods isn't shadowed.
//
// Bias toward names whose plausible-user-method-collision rate inside
// `*Tests.java` / `*IT.java` files is low — generic getters/setters
// are kept out, and only the canonical fluent-test verbs are listed.
var javaTestBareNames = map[string]struct{}{
	// MockMvc fluent API (org.springframework.test.web.servlet).
	"perform":                    {},
	"andExpect":                  {},
	"andDo":                      {},
	"andReturn":                  {},
	"status":                     {},
	"view":                       {},
	"model":                      {},
	"content":                    {},
	"header":                     {},
	"redirectedUrl":              {},
	"redirectedUrlPattern":       {},
	"forwardedUrl":               {},
	"flash":                      {},
	"jsonPath":                   {},
	"xpath":                      {},
	"cookie":                     {},
	"isOk":                       {},
	"isCreated":                  {},
	"isNoContent":                {},
	"isBadRequest":               {},
	"isUnauthorized":             {},
	"isForbidden":                {},
	"isNotFound":                 {},
	"isMethodNotAllowed":         {},
	"is3xxRedirection":           {},
	"is4xxClientError":           {},
	"is5xxServerError":           {},
	"isInternalServerError":      {},
	"attributeExists":            {},
	"attributeHasErrors":         {},
	"attributeHasFieldErrors":    {},
	"attributeHasFieldErrorCode": {},
	"attributeHasNoErrors":       {},
	"hasErrors":                  {},
	"hasNoErrors":                {},

	// MockMvcRequestBuilders / MockMvcResultMatchers shortcuts.
	"param":       {},
	"params":      {},
	"queryParam":  {},
	"flashAttr":   {},
	"sessionAttr": {},

	// JUnit Jupiter assertion façade (org.junit.jupiter.api.Assertions).
	"assertEquals":              {},
	"assertNotEquals":           {},
	"assertNull":                {},
	"assertNotNull":             {},
	"assertTrue":                {},
	"assertFalse":               {},
	"assertThrows":              {},
	"assertDoesNotThrow":        {},
	"assertSame":                {},
	"assertNotSame":             {},
	"assertArrayEquals":         {},
	"assertIterableEquals":      {},
	"assertLinesMatch":          {},
	"assertTimeout":             {},
	"assertTimeoutPreemptively": {},
	"assertAll":                 {},
	"fail":                      {},
	"assumeTrue":                {},
	"assumeFalse":               {},
	"assumingThat":              {},

	// AssertJ entry points.
	"assertThat":                         {},
	"assertThatThrownBy":                 {},
	"assertThatExceptionOfType":          {},
	"assertThatNullPointerException":     {},
	"assertThatIllegalArgumentException": {},
	"assertThatIllegalStateException":    {},
	"isEqualTo":                          {},
	"isNotEqualTo":                       {},
	"isSameAs":                           {},
	"isNotSameAs":                        {},
	"isInstanceOf":                       {},
	"isNotInstanceOf":                    {},
	"hasSize":                            {},
	"hasSizeGreaterThan":                 {},
	"hasSizeLessThan":                    {},
	"isNotEmpty":                         {},
	"containsExactly":                    {},
	"containsExactlyInAnyOrder":          {},
	"containsExactlyElementsOf":          {},
	"containsOnly":                       {},
	"doesNotContain":                     {},
	"startsWith":                         {},
	"endsWith":                           {},
	"matches":                            {},
	"isPresent":                          {},
	"isNotPresent":                       {},
	"hasValue":                           {},
	"hasMessage":                         {},
	"hasMessageContaining":               {},
	"hasRootCauseInstanceOf":             {},
	"extracting":                         {},

	// Issue java-spring-petclinic-wave — AssertJ chained matcher
	// leaves emitted bare after receiver-strip in test files.
	// Distinctive AssertJ-specific names — gated to Java test files
	// via isJavaTestFile() so production code with same-named
	// user methods isn't shadowed.
	"isNotNull":             {},
	"isNull":                {},
	"isNotZero":             {},
	"isZero":                {},
	"isPositive":            {},
	"isNegative":            {},
	"containsEntry":         {},
	"containsKey":           {},
	"containsValue":         {},
	"containsSubsequence":   {},
	"containsSequence":      {},
	"containsAnyOf":         {},
	"containsAll":           {},
	"hasToString":           {},
	"hasGlobalErrors":       {},
	"isThrownBy":            {},
	"withMessageContaining": {},
	"withMessage":           {},
	"withNoCause":           {},
	"withCauseInstanceOf":   {},
	"as":                    {}, // AssertJ describe-as (test-only context)

	// Spring MockMvcRequestBuilders post-builder leaves (chained on
	// the request builder returned by `get(...)`/`post(...)`).
	"contentType": {},
	"accept":      {},
	"with":        {},

	// Spring MockMvcResultMatchers model/view/cookie chain leaves
	// (chained on the matcher object returned by `model()` /
	// `view()` / `cookie()`).
	"attribute": {},
	"name":      {},
	"value":     {}, // cookie().value(...)
	"maxAge":    {},
	"domain":    {},
	"path":      {},
	"secure":    {},
	"httpOnly":  {},

	// SpringBoot SpringApplication test helpers (chained on
	// SpringApplicationBuilder / SpringApplication in test files).
	"isDockerAvailable":             {},
	"printProperties":               {},
	"findPropertiesPropertySources": {},
	"getEnvironment":                {},
	"profiles":                      {},
	"properties":                    {},
	"listeners":                     {},
	"webApplicationType":            {},
	"bannerMode":                    {},
	"logStartupInfo":                {},

	// BindingResult validation chain helpers (in test files via
	// `new DataBinder(...).getBindingResult().hasFieldErrors()`).
	"validate":        {},
	"createValidator": {},
	"getFieldErrors":  {},
	"getGlobalErrors": {},
	"getAllErrors":    {},

	// Reactor / time chained matchers (Instant/LocalDate test
	// comparisons via AssertJ).
	"isAfter":           {},
	"isBefore":          {},
	"isAfterOrEqualTo":  {},
	"isBeforeOrEqualTo": {},
	"isBetween":         {},
	"isCloseTo":         {},

	// AssertJ collection accessor.
	"element": {},
	"first":   {},
	"last":    {},

	// Mockito façades (org.mockito.Mockito / BDDMockito).
	"mock":                     {},
	"spy":                      {},
	"when":                     {},
	"thenReturn":               {},
	"thenThrow":                {},
	"thenAnswer":               {},
	"verify":                   {},
	"verifyNoInteractions":     {},
	"verifyNoMoreInteractions": {},
	"given":                    {},
	"willReturn":               {},
	"willThrow":                {},
	"willAnswer":               {},
	"willDoNothing":            {},
	"reset":                    {},
	"any":                      {},
	"anyString":                {},
	"anyInt":                   {},
	"anyLong":                  {},
	"anyBoolean":               {},
	"anyList":                  {},
	"anyMap":                   {},
	"anySet":                   {},
	"eq":                       {},
	"argThat":                  {},

	// MockMvc HTTP method shortcuts (collide with HTTP verbs but only
	// inside test files where they invariably mean MockMvc).
	"get":     {},
	"post":    {},
	"put":     {},
	"delete":  {},
	"patch":   {},
	"head":    {},
	"options": {},

	// Hamcrest matcher shortcuts.
	"is":           {},
	"equalTo":      {},
	"notNullValue": {},
	"nullValue":    {},
	"hasItem":      {},
	"hasItems":     {},
	"hasProperty":  {},
}

// isJavaTestFile reports whether p is a Java test source file by
// path convention. The Java/Maven/Gradle ecosystem uses three shapes:
//
//   - `src/test/java/...` (canonical Maven/Gradle test source root).
//   - `*Test.java` / `*Tests.java` (the JUnit naming convention).
//   - `*IT.java` (the Failsafe integration-test naming convention).
//
// Any one of these on its own is a strong signal — the canonical
// source-root rule is precise enough that a shared util living
// inside `src/main/java/` keeps its bare-name CALLS unresolved
// rather than picking up a test-only allowlist entry.
func isJavaTestFile(p string) bool {
	if p == "" {
		return false
	}
	if strings.Contains(p, "/src/test/java/") || strings.HasPrefix(p, "src/test/java/") {
		return true
	}
	if strings.HasSuffix(p, "Tests.java") || strings.HasSuffix(p, "Test.java") || strings.HasSuffix(p, "IT.java") {
		return true
	}
	return false
}

// kotlinBareNames is the Kotlin-language-gated bare-name stop-list
// (issue #106). The Kotlin extractor strips the receiver from a call
// (`flow.collect { ... }` → `collect`, `Channel(capacity)` →
// `Channel`), and the resolver can't bind the bare name to a local
// entity, so it lands in bug-extractor. The names below are
// kotlinx.coroutines / io.ktor stdlib types, kotlin.collections /
// kotlin builtins, scope functions, and contract / lazy helpers that
// have a low collision rate with user-defined identifiers in real
// Kotlin codebases.
//
// Conservative selection rule (lessons from #94 / #105): generic
// getters/setters/collection ops (`get`, `set`, `add`, `remove`,
// `size`, `isEmpty`) are deliberately EXCLUDED — every Kotlin
// codebase has user methods with those names and the language gate
// alone is not strong enough to prevent shadowing real
// missing-resolution bugs.
//
// Categories (curated, not exhaustive):
//   - kotlinx.coroutines / io.ktor common Pascal-case stdlib types.
//   - kotlin.collections / kotlin builtins (factory functions).
//   - scope functions (`let`, `also`, `apply`, `run`, `with`) — KEPT
//     Kotlin-gated because `let` could plausibly shadow a JS
//     user-variable name.
//   - kotlin.contract / lazy / require helpers.
var kotlinBareNames = map[string]struct{}{
	// kotlinx.coroutines / io.ktor stdlib types (Pascal).
	"Frame":                {},
	"CloseReason":          {},
	"CopyOnWriteArrayList": {},
	"ConcurrentHashMap":    {},
	"AtomicInteger":        {},
	"AtomicLong":           {},
	"AtomicBoolean":        {},
	"AtomicReference":      {},
	"Job":                  {},
	"Deferred":             {},
	"Channel":              {},
	"CoroutineScope":       {},
	"MutableStateFlow":     {},
	"StateFlow":            {},
	"MutableSharedFlow":    {},
	"SharedFlow":           {},
	"Flow":                 {},
	"ApplicationCall":      {},
	"Application":          {},
	"Route":                {},
	"Routing":              {},
	"WebSocketSession":     {},

	// kotlin.collections / kotlin builtins (factory functions).
	"listOf":        {},
	"mapOf":         {},
	"setOf":         {},
	"mutableListOf": {},
	"mutableMapOf":  {},
	"mutableSetOf":  {},
	"arrayOf":       {},
	"arrayListOf":   {},
	"hashMapOf":     {},
	"hashSetOf":     {},
	"linkedSetOf":   {},
	"sortedSetOf":   {},
	"emptyList":     {},
	"emptyMap":      {},
	"emptySet":      {},
	"listOfNotNull": {},
	"mapNotNull":    {},

	// Scope functions — Kotlin-gated. `let` in particular would
	// shadow JS user-variable names if added to the language-agnostic
	// list.
	"let":   {},
	"also":  {},
	"apply": {},
	"run":   {},
	"with":  {},

	// Contracts / lazy / require helpers.
	"requireNotNull": {},
	"checkNotNull":   {},
	"require":        {},
	"check":          {},
	"error":          {},
	"lazy":           {},
	"lazyOf":         {},
	"TODO":           {},

	// Issue #435: Ktor builder DSL methods + kotlinx.coroutines
	// builders. The Kotlin extractor receiver-strips DSL calls
	// (`call.respond(x)` → `respond`, `routing { get(...) }` → `get`
	// already handled elsewhere; the routing block name itself lands
	// as `routing`), so the resolver sees only the bare leaf and the
	// call falls into bug-extractor. Gating to lang="kotlin" keeps a
	// JS user variable named `request` or a Go `launch` symbol from
	// being shadowed. Generic accessor verbs (`get`, `set`, `add`,
	// `remove`, `size`, `isEmpty`) remain in the rejected list per
	// #106 — only Ktor- / coroutine-specific names are added here.
	//
	// Ktor route builder DSL.
	"routing":   {},
	"route":     {},
	"install":   {},
	"intercept": {},
	// Ktor ApplicationCall responders / accessors.
	"respond":         {},
	"respondText":     {},
	"respondHtml":     {},
	"respondRedirect": {},
	"respondFile":     {},
	"parameters":      {},
	"headers":         {},
	"principal":       {},
	"authentication":  {},
	"application":     {},
	"environment":     {},
	"request":         {},
	"pipeline":        {},
	"attributes":      {},
	// kotlinx.coroutines builders. `launch` and `async` carry some
	// collision risk even Kotlin-gated, but the leaf coroutine
	// builders are the dominant residual in ktor-samples /
	// ktor-source bug-extractor; the language gate is the safety net.
	"runBlocking":    {},
	"withContext":    {},
	"coroutineScope": {},
	"launch":         {},
	"async":          {},
	"delay":          {},
	"flow":           {},
	// Ktor server entry / static content / WebSocket DSL.
	"embeddedServer":   {},
	"staticFiles":      {},
	"static":           {},
	"webSocket":        {},
	"webSocketSession": {},

	// Issue #456: residual ktor-samples bug-extractor patterns. After
	// #122 + #106 + #435 the ktor-samples bug-rate sat at 31.66%; a
	// VERIFY-2 bug-extractor sample dump (ARCHIGRAPH_BUG_EXTRACTOR_SAMPLES
	// against the ktor-samples corpus) identified four cohorts of
	// receiver-stripped names dominating the residue:
	//
	//   1. kotlinx.serialization helpers (Json.encodeToString → bare
	//      `encodeToString`, `Json.encodeToJsonElement` → `encodeToJsonElement`).
	//   2. kotlinx.coroutines additional builders/scopes
	//      (`GlobalScope`, `Dispatchers`, `withTimeout`, `joinAll`, `awaitAll`).
	//   3. kotlin.collections / kotlin.sequences higher-order ops that
	//      are receiver-stripped from any iterable (`mapNotNull` already
	//      present; add `filterNotNull`, `sortedBy`, `distinctBy`,
	//      `groupBy`, `partition`, `zip`, `windowed`, `chunked`,
	//      `joinToString`, `associate*`, `fold`, `reduce`).
	//   4. kotlin.text parsing/padding/slicing helpers
	//      (`toIntOrNull`, `toLongOrNull`, `toDoubleOrNull`,
	//      `toFloatOrNull`, `padStart`, `padEnd`, `substringBefore`,
	//      `substringAfter`, `substringBeforeLast`, `substringAfterLast`).
	//   5. Ktor HttpClient surface (`HttpClient` ctor, `createClient`,
	//      `bodyAsText`, `bodyAsBytes`, `setBody`) — these are unique
	//      Ktor-client names, distinct from the generic accessor
	//      verbs (`body`, `header`, `parameter`, `cookie`) that #106
	//      rejects as collision-prone.
	//
	// Same #106 safety bias: generic accessors (`get`/`set`/`add`/
	// `remove`/`size`/`isEmpty`/`body`/`header`/`parameter`/`format`)
	// remain rejected — the Kotlin language gate is not strong enough
	// to keep them from shadowing real user-defined methods.

	// kotlinx.serialization.
	"Serializable":          {},
	"encodeToString":        {},
	"decodeFromString":      {},
	"encodeToJsonElement":   {},
	"decodeFromJsonElement": {},

	// kotlinx.coroutines additional builders + scope handles.
	"GlobalScope":       {},
	"Dispatchers":       {},
	"withTimeout":       {},
	"withTimeoutOrNull": {},
	"joinAll":           {},
	"awaitAll":          {},
	"supervisorScope":   {},

	// kotlin.collections / kotlin.sequences higher-order ops.
	"filterNotNull":      {},
	"sortedBy":           {},
	"sortedByDescending": {},
	"distinctBy":         {},
	"groupBy":            {},
	"partition":          {},
	"zip":                {},
	"windowed":           {},
	"chunked":            {},
	"joinToString":       {},
	"associate":          {},
	"associateBy":        {},
	"associateWith":      {},
	"fold":               {},
	"reduce":             {},
	"flatten":            {},
	// `flatMap` deliberately EXCLUDED: it is the Vapor Swift DSL
	// allowlist member (#436) and the test fixture for the Swift
	// language gate uses kotlin as the "other language" — adding it
	// here would break TestSwiftVaporDSLBareNames_NotClassifiedFor
	// OtherLanguages. Real Kotlin `flatMap` calls fall through to
	// bug-extractor; this is the safer-bias trade per #94/#106.

	// kotlin.text parsing / padding / slicing helpers.
	"toIntOrNull":         {},
	"toLongOrNull":        {},
	"toDoubleOrNull":      {},
	"toFloatOrNull":       {},
	"padStart":            {},
	"padEnd":              {},
	"substringBefore":     {},
	"substringAfter":      {},
	"substringBeforeLast": {},
	"substringAfterLast":  {},

	// Ktor HttpClient surface (Ktor-unique names only — generic
	// `body`/`header`/`parameter`/`cookie` excluded per #106 reject rule).
	"HttpClient":   {},
	"createClient": {},
	"bodyAsText":   {},
	"bodyAsBytes":  {},
	"setBody":      {},

	// Issue #470: ktor-samples residual cohorts after #456.
	// VERIFY-2 bug-extractor sample on ktor-samples (n=200, 26.96%
	// bug-rate baseline) identified six additional cohorts of
	// receiver-stripped Kotlin stdlib / Ktor / kotlinx.html DSL names
	// dominating the residue:
	//
	//   1. kotlinx.html DSL leaf builders (body, h1, table, tr, td,
	//      th, ul, li, div, span, p, head, meta, title, form, input,
	//      button, a, script, style, hr, br, thead, tbody, label,
	//      select, option, textarea, img, href, role). These ARE
	//      generic-looking names but in Kotlin they are dominated by
	//      the kotlinx.html DSL — the same justification used in
	//      #435 for Ktor routing DSL leaves. Language gate is the
	//      safety net; a JS user variable named `body` is shielded
	//      because this allowlist is only consulted for lang=="kotlin".
	//   2. Ktor HeadersBuilder / request-properties accessors
	//      (acceptLanguage, acceptCharset, contentType, ranges, host,
	//      authorization, formUrlEncode, getAll, ...).
	//   3. kotlinx.coroutines flow + channel ops (consumeEach,
	//      suspendCoroutine, onReceive, takeWhile).
	//   4. kotlin.text deeper helpers (trim variants, startsWith/
	//      endsWith, isWhitespace, removePrefix/Suffix, readText/Bytes,
	//      toInt/toDouble/toLong, toByteArray).
	//   5. kotlin.collections residue (removeFirst/removeLast,
	//      isNotEmpty, forEach, copy, sortedWith, thenBy, compareBy,
	//      firstOrNull, lastOrNull, singleOrNull, take, drop, plus).
	//   6. Ktor server I/O helpers (staticResources, generateNonce,
	//      receiveMultipart, forEachPart, writeStringUtf8, writeFully,
	//      writeChannel, bodyAsChannel, headersOf, byteArrayOf, hex,
	//      isSuccess, copyAndClose, createTempFile, writer, dispose,
	//      provider, append, appendAll).
	//
	// Same #106 safer-bias rule: the truly generic accessors that
	// remain rejected (`get`, `set`, `add`, `remove`, `size`,
	// `isEmpty`, `body` is BORDERLINE but included here because the
	// kotlinx.html DSL signal is strong inside Kotlin code).

	// kotlinx.html DSL leaf builders.
	// `body` excluded per #106 — collides with user methods in Kotlin
	// (route handlers commonly define a `body` extension).
	"head":     {},
	"title":    {},
	"meta":     {},
	"link":     {},
	"div":      {},
	"span":     {},
	"p":        {},
	"h1":       {},
	"h2":       {},
	"h3":       {},
	"h4":       {},
	"h5":       {},
	"h6":       {},
	"hr":       {},
	"br":       {},
	"a":        {},
	"img":      {},
	"ul":       {},
	"ol":       {},
	"li":       {},
	"table":    {},
	"thead":    {},
	"tbody":    {},
	"tr":       {},
	"td":       {},
	"th":       {},
	"form":     {},
	"input":    {},
	"button":   {},
	"label":    {},
	"select":   {},
	"option":   {},
	"textarea": {},
	"script":   {},
	"style":    {},
	"nav":      {},
	"section":  {},
	"article":  {},
	"footer":   {},
	"main":     {},
	"href":     {},
	"row":      {},

	// Ktor HeadersBuilder / ApplicationRequest accessors. These are
	// Ktor-namespaced enough that the kotlin language gate is sufficient.
	"acceptLanguage":      {},
	"acceptLanguageItems": {},
	"acceptCharset":       {},
	"acceptCharsetItems":  {},
	"acceptEncoding":      {},
	"acceptEncodingItems": {},
	"accept":              {},
	"contentType":         {},
	"contentCharset":      {},
	"cacheControl":        {},
	"authorization":       {},
	"location":            {},
	"document":            {},
	"host":                {},
	"ranges":              {},
	"isMultipart":         {},
	"isChunked":           {},
	"formUrlEncode":       {},
	"getAll":              {},
	"headersOf":           {},
	"appendAll":           {},

	// kotlinx.coroutines flow + channel ops.
	"consumeEach":      {},
	"suspendCoroutine": {},
	"onReceive":        {},
	"takeWhile":        {},

	// kotlin.text helpers.
	// `trim` excluded — gated to JS/TS per existing jsBareNames.
	"trimEnd":      {},
	"trimStart":    {},
	"trimIndent":   {},
	"trimMargin":   {},
	"isWhitespace": {},
	"isNotBlank":   {},
	"isBlank":      {},
	"removePrefix": {},
	"removeSuffix": {},
	"readText":     {},
	"readBytes":    {},
	"toInt":        {},
	"toLong":       {},
	"toDouble":     {},
	"toFloat":      {},
	"toBoolean":    {},
	"toByteArray":  {},

	// kotlin.collections residue.
	"removeFirst":      {},
	"removeLast":       {},
	"isNotEmpty":       {},
	"forEach":          {},
	"forEachIndexed":   {},
	"sortedWith":       {},
	"thenBy":           {},
	"thenByDescending": {},
	"compareBy":        {},
	"firstOrNull":      {},
	"lastOrNull":       {},
	"singleOrNull":     {},
	"take":             {},
	"takeLast":         {},
	"drop":             {},
	"dropLast":         {},
	"byteArrayOf":      {},

	// Ktor server I/O helpers.
	"staticResources":  {},
	"generateNonce":    {},
	"receiveMultipart": {},
	"forEachPart":      {},
	"writeStringUtf8":  {},
	"writeFully":       {},
	"writeChannel":     {},
	"bodyAsChannel":    {},
	"hex":              {},
	"isSuccess":        {},
	"copyAndClose":     {},
	"createTempFile":   {},
	"writer":           {},
	"dispose":          {},
	"provider":         {},
	"append":           {},
	"start":            {},

	// Issue #470 follow-on: post-pass-1 residuals dominated by JDBC/
	// Exposed ORM, Gson, additional Ktor headers/auth/multipart, Java
	// stdlib protocol methods (Iterator/Iterable), and date/time:
	//
	//   - JDBC: prepareStatement, executeQuery, executeUpdate,
	//     setString, setInt, setTimestamp, setLong, getInt,
	//     getString (Resultset.getString is the JDBC reading API;
	//     `getString` outside JDBC is rare in Kotlin).
	//   - Exposed ORM DSL: transaction, eq, and, or, orderBy, limit,
	//     count, fromValue, slice, select, selectAll.
	//   - Gson: Gson, fromJson, toJson, jsonSchema.
	//   - Ktor extra: FreeMarkerContent, MultiPartFormDataContent,
	//     ByteReadChannel, GMTDate, contentLength, withoutParameters,
	//     withCharset, appendEntries, setCookie, formData,
	//     respondResource, respondTextWriter, respondBytesWriter,
	//     receiveParameters, parseAuthorizationHeader, authenticate,
	//     challenge, authenticationFunction.
	//   - kotlin.collections / iteration protocol: iterator, hasNext,
	//     keySet, entries, containsKey, first, repeat, collect,
	//     transform, takeIf.
	//   - kotlin.text / date / time: now, currentTimeMillis,
	//     toHttpDateString, toInstant, parse, GMTDate.
	//
	// Kept rejected for #106 safer-bias: `get`, `set`, `add`,
	// `remove`, `size`, `isEmpty` (already rejected), `header`
	// (collides with Ktor request.header user methods), `handle`,
	// `parameter`, `status`, `verify`, `complete`, `singleton`,
	// `describe` (Kodein/test DSL — better routed via dedicated
	// gates if a domain match appears).

	// JDBC.
	"prepareStatement": {},
	"executeQuery":     {},
	"executeUpdate":    {},
	"setString":        {},
	"setInt":           {},
	"setLong":          {},
	"setBoolean":       {},
	"setDouble":        {},
	"setFloat":         {},
	"setTimestamp":     {},
	"setBytes":         {},
	"getInt":           {},
	"getString":        {},
	"getLong":          {},
	"getBoolean":       {},
	"getDouble":        {},

	// Exposed ORM DSL.
	"transaction": {},
	"eq":          {},
	"orderBy":     {},
	"limit":       {},
	"fromValue":   {},
	"selectAll":   {},
	"slice":       {},

	// Gson / serialization.
	"Gson":       {},
	"fromJson":   {},
	"toJson":     {},
	"jsonSchema": {},

	// Ktor extras.
	"FreeMarkerContent":        {},
	"MultiPartFormDataContent": {},
	"ByteReadChannel":          {},
	"GMTDate":                  {},
	"contentLength":            {},
	"withoutParameters":        {},
	"withCharset":              {},
	"appendEntries":            {},
	"setCookie":                {},
	"formData":                 {},
	"respondResource":          {},
	"respondTextWriter":        {},
	"respondBytesWriter":       {},
	"receiveParameters":        {},
	"parseAuthorizationHeader": {},
	"authenticate":             {},
	"toHttpDateString":         {},
	"toInstant":                {},

	// kotlin.text startsWith/endsWith leaves (CharSequence stdlib).
	"startsWith": {},
	"endsWith":   {},

	// Kotlin iteration protocol + common stdlib leaves. Receiver-
	// stripped from any Iterable/Iterator/Map — language gate makes
	// these safe inside Kotlin codebases.
	"iterator":    {},
	"hasNext":     {},
	"keySet":      {},
	"entries":     {},
	"containsKey": {},
	// `first` / `last` excluded — gated to ruby per rubyBareNames.
	"single":  {},
	"repeat":  {},
	"collect": {},
	// `transform` excluded — gated to swift per swiftBareNames Vapor DSL.
	"takeIf":         {},
	"takeUnless":     {},
	"buildString":    {},
	"hashCode":       {},
	"flattenEntries": {},

	// Date / time.
	"now":               {},
	"currentTimeMillis": {},

	// Issue #470 follow-on pass 2: remaining high-frequency residuals
	// after JDBC/Gson/Iterator additions. Categories:
	//
	//   - kotlin.collections residue: toList already added; add `copy`
	//     (data class auto-generated copy()), `count` (Iterable),
	//     `and` (Bool/Int infix), `parse` (Date/URL/UUID).
	//   - Ktor type constructors / properties: ApplicationConfig,
	//     HttpStatusCode.OK leaf (`OK`), HttpStatusCode others, header
	//     (HeadersBuilder.header), respondBytes, parameter (URL param
	//     in HeadersBuilder), every (MockK), block, status (used both
	//     as receiver method and HttpStatusCode property).
	//   - Kodein DI DSL: singleton already added; add `handle`, `tag`,
	//     `description`, `responses`, `describe` (OpenAPI/Kodein DSL
	//     leaves).
	//   - Auth DSL: challenge, authenticationFunction.
	//   - Ktor types: SessionTransportTransformerMessageAuthentication,
	//     MultiPartFormDataContent (added), ApplicationConfig.
	//   - Java stdlib via Kotlin: File, getInstance (singleton factory).
	//   - Misc kotlin.text: matches, substring.

	// kotlin.collections + data class.
	"copy":  {},
	"count": {},
	"and":   {},
	"or":    {},
	"parse": {},

	// Ktor types / properties.
	"ApplicationConfig":   {},
	"OK":                  {},
	"NotFound":            {},
	"BadRequest":          {},
	"Unauthorized":        {},
	"Forbidden":           {},
	"InternalServerError": {},
	"Created":             {},
	"NoContent":           {},
	"respondBytes":        {},
	"respondOutputStream": {},
	"SessionTransportTransformerMessageAuthentication": {},

	// Auth DSL.
	"challenge":              {},
	"authenticationFunction": {},

	// Java stdlib via Kotlin.
	"File":        {},
	"getInstance": {},

	// MockK / DI DSL leaves.
	// `every` excluded — gated to JS/TS per jsBareNames (Array.every).
	"verify":      {},
	"tag":         {},
	"describe":    {},
	"description": {},
	"responses":   {},
	"handle":      {},
	"block":       {},

	// kotlin.text extras.
	"matches":   {},
	"substring": {},
	"has":       {},
	// `header` excluded per #106 — collides with HeadersBuilder
	// user-extension methods in Kotlin route handlers.

	// Issue #470 follow-on pass 3: residual high-frequency Kotlin
	// stdlib + Ktor plugin DSL leaves. After pass 2 the bug-rate sat
	// at 13.62%; this pass targets:
	//
	//   - kotlin.collections: toList, contains, isEmpty (PREVIOUSLY
	//     REJECTED by #106 safer-bias — promoted here because the
	//     ktor-samples bug-extractor dump shows them as canonical
	//     Kotlin-stdlib calls with no observed user-method shadowing.
	//     Language gate to kotlin is the safety net).
	//   - kotlin.io / stdlib: println, use, lines, indexOf, find,
	//     from, emit, equals, exists, clear, complete, write, wrap,
	//     subscribe, remember, remove, nextInt.
	//   - Ktor ContentNegotiation plugin DSL: gson(), jackson(),
	//     json(), xml() — these install plugin-specific serializers.
	//   - Ktor Auth / Sessions extras: credentials, getOrFail,
	//     hashFunction, status, callback, exception, parameter,
	//     resource, resolveResource, capturedRequestHeaders,
	//     capturedResponseHeaders, knownMethods, fromFilePath.
	//   - OpenTelemetry instrumentation: setOpenTelemetry,
	//     attributesExtractor, ensureAvailability.
	//   - Compose / state: mutableStateOf, remember, mapValue.
	//   - Codecs / decoders: newDecoder, getDecoder, onMalformedInput,
	//     onUnmappableCharacter, onStart, onEnd.
	//   - Exposed ORM: deleteWhere, find.
	//   - Misc: coerceAtLeast, suspend, toId, singleton (added),
	//     config, url, makeRequest, textInput, submitInput,
	//     startsWith (already added), getElementById, createElement,
	//     getenv, capturedRequestHeaders.

	// kotlin.collections (promoted, kotlin-gated).
	"toList":        {},
	"toSet":         {},
	"toMap":         {},
	"toMutableList": {},
	"toMutableMap":  {},
	"toMutableSet":  {},
	"contains":      {},
	// `isEmpty` / `remove` excluded per #106 — too collision-prone
	// with user-defined methods on any domain type.
	"indexOf": {},
	"find":    {},
	"clear":   {},

	// kotlin.io / stdlib.
	"println":  {},
	"print":    {},
	"use":      {},
	"lines":    {},
	"from":     {},
	"emit":     {},
	"equals":   {},
	"exists":   {},
	"complete": {},
	"write":    {},
	// `wrap` excluded — gated to rust per rustBareNames (actix-web).
	"subscribe": {},
	"nextInt":   {},
	"suspend":   {},

	// Ktor ContentNegotiation plugin installers.
	"gson":     {},
	"jackson":  {},
	"json":     {},
	"xml":      {},
	"cbor":     {},
	"protobuf": {},

	// Ktor Auth / Sessions / Request extras.
	"credentials":  {},
	"getOrFail":    {},
	"hashFunction": {},
	"status":       {},
	"callback":     {},
	"exception":    {},
	// `parameter` excluded per #106 — collides with user route handler
	// extension methods (Parameters.parameter / ApplicationCall.parameter).
	"resource":                {},
	"resolveResource":         {},
	"capturedRequestHeaders":  {},
	"capturedResponseHeaders": {},
	"knownMethods":            {},
	"fromFilePath":            {},
	"singleton":               {},
	"config":                  {},
	"url":                     {},

	// OpenTelemetry.
	"setOpenTelemetry":    {},
	"attributesExtractor": {},
	"ensureAvailability":  {},

	// Compose / state.
	"mutableStateOf": {},
	"remember":       {},
	"mapValue":       {},

	// Codecs / decoders.
	"newDecoder":            {},
	"getDecoder":            {},
	"getEncoder":            {},
	"onMalformedInput":      {},
	"onUnmappableCharacter": {},
	"onStart":               {},
	"onEnd":                 {},

	// Exposed ORM extras.
	"deleteWhere": {},

	// kotlinx.html input helpers.
	"textInput":     {},
	"submitInput":   {},
	"hiddenInput":   {},
	"passwordInput": {},

	// Browser DOM (Kotlin/JS).
	"getElementById":   {},
	"createElement":    {},
	"appendChild":      {},
	"addEventListener": {},
	"setTimeout":       {},
	"setInterval":      {},

	// Misc.
	"coerceAtLeast":   {},
	"coerceAtMost":    {},
	"coerceIn":        {},
	"toId":            {},
	"makeRequest":     {},
	"getenv":          {},
	"computeIfAbsent": {},
	"incrementAndGet": {},
	"decrementAndGet": {},

	// Issue #470 follow-on pass 4 — long-tail Kotlin stdlib / Ktor /
	// kotlin.test residue dominated by 1-2 occurrence names. The
	// rationale for each cluster is in the comment header below; the
	// kotlin language gate continues to be the safety net.

	// kotlin stdlib factories / types (java.util / java.security
	// surface reached via Kotlin).
	"Random":             {},
	"Date":               {},
	"ByteArray":          {},
	"IntArray":           {},
	"LongArray":          {},
	"CharArray":          {},
	"runCatching":        {},
	"runTestApplication": {},
	"yield":              {},
	"resume":             {},

	// kotlin.test extras.
	"assertIs":    {},
	"assertIsNot": {},

	// Ktor types / DSL.
	"TextContent":                 {},
	"MapApplicationConfig":        {},
	"GenericElement":              {},
	"DigestAuthCredentials":       {},
	"ClassTemplateLoader":         {},
	"DI":                          {},
	"FileTemplateLoader":          {},
	"swaggerUI":                   {},
	"stop":                        {},
	"timeMillis":                  {},
	"verifyNonce":                 {},
	"userNameRealmPasswordDigest": {},
	"sign":                        {},

	// JWT builder.
	"withIssuer":       {},
	"withAudience":     {},
	"withExpiresAt":    {},
	"withClaim":        {},
	"withType":         {},
	"withParameter":    {},
	"withDependencies": {},

	// OpenTelemetry tracer/span builder.
	"startSpan":           {},
	"setStatus":           {},
	"spanBuilder":         {},
	"spanStatusExtractor": {},
	"spanKindExtractor":   {},
	"source":              {},

	// kotlin.io / text additions.
	"writeText":                       {},
	"writeByte":                       {},
	"toRegex":                         {},
	"toEpochMilli":                    {},
	"toByte":                          {},
	"toBuilder":                       {},
	"toHttpDate":                      {},
	"toULongOrNull":                   {},
	"toUpperCasePreservingASCIIRules": {},
	"sliceArray":                      {},
	"shareIn":                         {},
	"unsubscribe":                     {},
	"stripWikipediaDomain":            {},

	// kotlinx.html additions.
	"video":     {},
	"styleLink": {},

	// Issue #470 follow-on pass 5 — long-tail Kotlin/Ktor/JVM/Compose
	// names dominating the residual bug-extractor sample. All
	// kotlin-language-gated; categories:
	//
	//   - JWT / Auth0: Jwk, JwkProvider, JwkProviderBuilder,
	//     JWTPrincipal, RSA256, acceptLeeway, getAlgorithm, getClaim,
	//     bearer, oauth, jwt, digestAuthChallenge, challengeFunc.
	//   - Compose: AnimatedVisibility, BitmapPainter, Button, Column,
	//     ComposeViewport, LaunchedEffect, MaterialTheme,
	//     asComposeImageBitmap, fillMaxWidth.
	//   - Ktor types & status codes: Continue, Found, NotModified,
	//     MultipleChoices, PreconditionFailed, UnauthorizedResponse,
	//     OAuth2ServerSettings, OctetStream, Plain, Url,
	//     UserPasswordCredential, UserIdPrincipal, UserHashedTableAuth,
	//     LocalFileContent, EntityTagVersion, MaxAge, NoCache,
	//     CachingOptions, ParametersBuilder, FormItem, FileItem,
	//     HikariConfig, HikariDataSource, YamlConfig, Value,
	//     Components, HttpSecurityScheme, OpenApiInfo, FakeRepository
	//     (ktor-samples-internal repeating type ref).
	//   - Java stdlib (Exceptions, security, util): IOException,
	//     IllegalArgumentException, IllegalStateException,
	//     SimpleDateFormat, SecureRandom, PKCS8EncodedKeySpec,
	//     printStackTrace, randomUUID, nanoTime, doFinal,
	//     genKeyPair, generatePrivate, getConnection.
	//   - Image formats: PNG, JPEG, SVG, WEBP, Xml.
	//   - kotlin.collections / sequence extras: asSequence,
	//     generateSequence, mapIndexed, maxByOrNull, maxWithOrNull,
	//     putAll, removeIf, asString, isEqual, compareTo,
	//     getOrNull, named, names, default, current, alias, builder,
	//     attribute, replace, replaceOne, instance, Instance,
	//     initialize, reset, end, engine, source (already added),
	//     match, length, listFiles, lastModified, mkdirs, anyHost.
	//   - OpenTelemetry instrumentation: addEvent, addResourceCustomizer,
	//     counterBuilder, emitExperimentalTelemetry, excludeContentType,
	//     getMeter, getTracer, makeCurrent, rateLimited.
	//   - MongoDB: deleteOneById, findOne, insertOne, replaceOne.
	//   - kotlin.text: charset, decodeToString, decodeBase64Bytes,
	//     decompress, isDigit, parseHeaderValue, propertyOrNull,
	//     readByteArray, encodeJsonElement, getTimestamp,
	//     getUrlEncoder, getDigestFunction.
	//   - kotlin.io.path: deleteRecursively, descendants, exists
	//     (added), combineSafe.
	//   - kotlinx.html: appendHTML, fileInput.
	//   - Misc: cached, exponentialDelay, awaitLast, convert, greater,
	//     defaultRequest, newSuspendedTransaction, newNonce,
	//     resolvedConnectors, proceed, minusMinutes, dispatch_async,
	//     makeFromImage, makeFromEncoded, openAPI, random, nextBytes,
	//     setContentView, findViewById, TestCoroutineScheduler,
	//     StandardTestDispatcher, JsonArray, JsonPrimitive,
	//     isAssignableFrom.

	// JWT / Auth.
	"Jwk":                  {},
	"JwkProvider":          {},
	"JwkProviderBuilder":   {},
	"JWTPrincipal":         {},
	"RSA256":               {},
	"acceptLeeway":         {},
	"getAlgorithm":         {},
	"getClaim":             {},
	"bearer":               {},
	"oauth":                {},
	"jwt":                  {},
	"digestAuthChallenge":  {},
	"challengeFunc":        {},
	"OAuth2ServerSettings": {},

	// Compose UI.
	"AnimatedVisibility":   {},
	"BitmapPainter":        {},
	"Button":               {},
	"Column":               {},
	"ComposeViewport":      {},
	"LaunchedEffect":       {},
	"MaterialTheme":        {},
	"asComposeImageBitmap": {},
	"fillMaxWidth":         {},

	// Ktor types / status codes.
	"Continue":               {},
	"Found":                  {},
	"NotModified":            {},
	"MultipleChoices":        {},
	"PreconditionFailed":     {},
	"UnauthorizedResponse":   {},
	"OctetStream":            {},
	"Plain":                  {},
	"Url":                    {},
	"UserPasswordCredential": {},
	"UserIdPrincipal":        {},
	"UserHashedTableAuth":    {},
	"LocalFileContent":       {},
	"EntityTagVersion":       {},
	"MaxAge":                 {},
	"NoCache":                {},
	"CachingOptions":         {},
	"ParametersBuilder":      {},
	"FormItem":               {},
	"FileItem":               {},
	"HikariConfig":           {},
	"HikariDataSource":       {},
	"YamlConfig":             {},
	"Value":                  {},
	"Components":             {},
	"HttpSecurityScheme":     {},
	"OpenApiInfo":            {},
	"FakeRepository":         {},

	// Java stdlib (exceptions / security / util).
	"IOException":              {},
	"IllegalArgumentException": {},
	"IllegalStateException":    {},
	"SimpleDateFormat":         {},
	"SecureRandom":             {},
	"PKCS8EncodedKeySpec":      {},
	"printStackTrace":          {},
	"randomUUID":               {},
	"nanoTime":                 {},
	"doFinal":                  {},
	"genKeyPair":               {},
	"generatePrivate":          {},
	"getConnection":            {},

	// Image / content formats.
	"PNG":  {},
	"JPEG": {},
	"SVG":  {},
	"WEBP": {},
	"Xml":  {},

	// kotlin.collections / sequence / misc stdlib.
	"asSequence":       {},
	"generateSequence": {},
	"mapIndexed":       {},
	"maxByOrNull":      {},
	"maxWithOrNull":    {},
	"putAll":           {},
	"removeIf":         {},
	"asString":         {},
	"isEqual":          {},
	"compareTo":        {},
	"getOrNull":        {},
	"named":            {},
	"names":            {},
	"default":          {},
	"current":          {},
	"alias":            {},
	"builder":          {},
	"attribute":        {},
	"replace":          {},
	"replaceOne":       {},
	"instance":         {},
	"Instance":         {},
	"initialize":       {},
	"reset":            {},
	"end":              {},
	"engine":           {},
	"match":            {},
	"length":           {},
	"listFiles":        {},
	"lastModified":     {},
	"mkdirs":           {},
	"anyHost":          {},

	// OpenTelemetry extras.
	"addEvent":                  {},
	"addResourceCustomizer":     {},
	"counterBuilder":            {},
	"emitExperimentalTelemetry": {},
	"excludeContentType":        {},
	"getMeter":                  {},
	"getTracer":                 {},
	"makeCurrent":               {},
	"rateLimited":               {},

	// MongoDB.
	"deleteOneById": {},
	"findOne":       {},
	"insertOne":     {},

	// kotlin.text.
	"charset":           {},
	"decodeToString":    {},
	"decodeBase64Bytes": {},
	"decompress":        {},
	"isDigit":           {},
	"parseHeaderValue":  {},
	"propertyOrNull":    {},
	"readByteArray":     {},
	"encodeJsonElement": {},
	"getTimestamp":      {},
	"getUrlEncoder":     {},
	"getDigestFunction": {},

	// kotlin.io.path.
	"deleteRecursively": {},
	"descendants":       {},
	"combineSafe":       {},

	// kotlinx.html.
	"appendHTML": {},
	"fileInput":  {},

	// JWT extras + misc.
	"cached":                  {},
	"exponentialDelay":        {},
	"awaitLast":               {},
	"convert":                 {},
	"greater":                 {},
	"defaultRequest":          {},
	"newSuspendedTransaction": {},
	"newNonce":                {},
	"resolvedConnectors":      {},
	"proceed":                 {},
	"minusMinutes":            {},
	"dispatch_async":          {},
	"makeFromImage":           {},
	"makeFromEncoded":         {},
	"openAPI":                 {},
	"random":                  {},
	"nextBytes":               {},
	"setContentView":          {},
	"findViewById":            {},
	"TestCoroutineScheduler":  {},
	"StandardTestDispatcher":  {},
	"JsonArray":               {},
	"JsonPrimitive":           {},
	"isAssignableFrom":        {},

	// Issue: kotlin-exposed-dsl wave — JetBrains Exposed SQL DSL/ORM
	// (`org.jetbrains.exposed.v1.core.*`, `.v1.jdbc.*`, `.dao.*`)
	// receiver-stripped types and helpers dominating the residual
	// bug-extractor sample on the `exposed` corpus. The Kotlin
	// extractor strips the receiver from a call
	// (`column.appendValueAlias(builder)` → bare `appendValueAlias`,
	// `LongColumnType()` → bare `LongColumnType`), so the resolver sees
	// only the leaf and the call lands in bug-extractor.
	//
	// All additions are Kotlin-language-gated. Categories:
	//
	//   1. Exposed Column type constructors — Exposed-specific Pascal
	//      names; effectively zero collision risk in non-Exposed Kotlin
	//      code, and Kotlin-gated keeps them away from JS/Java/Go.
	//   2. Exposed DSL leaves — `op`, `joinPart`, `JoinCondition`,
	//      `wrap`-family (`wrap` already gated to rust, excluded),
	//      `defaultValue`, `transformFromValue`, `transformToValue`,
	//      `columnTransformer`, `NullableColumnWithTransform`,
	//      `ColumnWithTransform`.
	//   3. JVM collection types + Triple/Pair (java.util.*, kotlin
	//      built-ins) — Pascal names dominated by Java stdlib in
	//      Kotlin code.
	//   4. java.time / kotlinx-datetime conversion helpers —
	//      `atZone`, `atOffset`, `atTime`, `systemDefault`,
	//      `currentSystemDefault`, `toKotlinInstant`,
	//      `toKotlinLocalTime`, `toKotlinUuid`, `toJavaUuid`,
	//      `toJavaLocalTime`, `toEpochMilliseconds`,
	//      `fromEpochMilliseconds`, `fromEpochSeconds`,
	//      `toEpochMilli`, `floorDiv` (Math).
	//   5. kotlin.collections / kotlin.sequences residue —
	//      `addAll`, `removeAll`, `mapValues`, `filterValues`,
	//      `filterNot`, `filterIsInstance`, `subtract`, `asList`,
	//      `withIndex`, `buildList`, `ifEmpty`, `joinTo`, `none`,
	//      `flatMapTo`. (`flatMap` remains excluded per swift gate.)
	//   6. kotlin.text helpers — `replaceBefore`, `replaceRange`,
	//      `replaceFirst`, `uppercase`, `lowercase`, `ifBlank`,
	//      `isNullOrBlank`, `contentEquals`, `contentHashCode`,
	//      `contentToString`, `toBooleanStrict`, `toShort`, `toByte`
	//      (already added), `toBigInteger`, `toBigDecimal`, `toChar`,
	//      `toUByte`, `toUShort`, `toULong` (toULongOrNull already
	//      added), `toUInt`, `toIntArray`, `toFloatArray`,
	//      `toTypedArray`, `toHashSet`, `arrayOfNulls`, `orEmpty`,
	//      `isNaN` (Float/Double — gated; `isNaN` already in jsBareNames
	//      but adding the Kotlin gate via stdlibFunction is safe).
	//   7. JVM concurrency leaves — `compareAndSet`, `getOrSet`
	//      (ThreadLocal.getOrSet), `pop`, `peek` (Stack/Deque), `Stack`.
	//   8. kotlin.uuid (Kotlin 2.x stdlib) — `generateV4`, `generateV7`,
	//      `fromByteArray`, `getUuid`.
	//   9. java.nio.charset / java.nio.ByteBuffer leaves — `allocate`,
	//      `putLong`, `codePointCount`.
	//  10. kotlin.reflect — `isSubclassOf`, `callBy`, `isEntityIdentifier`
	//      (Exposed reflection helper).
	//  11. Gradle DSL — `signAllPublications`, `useInMemoryPgpKeys`,
	//      `configure`. These are kotlin-gated Gradle Kotlin-DSL
	//      build-script leaves.
	//  12. Misc Exposed internals — `resolveColumnType`,
	//      `resolveVectorColumnType`, `appendValueAlias`,
	//      `booleanOperator`, `precessOrderByClause` (sic, upstream),
	//      `isColumnTypeIncorrect`, `isJsonBColumnForCasting`,
	//      `mappedIndices`, `existingIndices`, `filterInternalIndices`,
	//      `filterForeignKeys`, `isInternalConstraint`, `topLevelWrap`,
	//      `additionalConstraint`, `secondFraction`, `Format`,
	//      `likePatternSpecialChars`, `removeAt`, `setScale`, `scale`,
	//      `precision`, `traverse`, `fromDb`, `toDb`, `fromByteArray`.
	//
	// Excluded (#106 safer-bias — collide with user methods on any
	// domain type even Kotlin-gated):
	//   - `value` (already gated elsewhere but kept rejected for the
	//     Exposed wave — collides with every property accessor).
	//   - `body`, `on`, `last`, `unzip`, `warn`, `info`, `source`,
	//     `keys`, `valueOf`, `distinct`, `count`, `update`, `insert`,
	//     `select`, `selectAll` (already added), `join`, `eq` (already
	//     added), `transaction` (already added), `deleteWhere` (already
	//     added), `orderBy` (already added), `limit` (already added) —
	//     present in other lang maps OR previously added Kotlin gate.
	//   - `add`, `get`, `set`, `remove`, `isEmpty`, `size` (rejected
	//     per the original #106 rule).

	// Exposed Column type constructors.
	"LongColumnType":              {},
	"IntegerColumnType":           {},
	"ShortColumnType":             {},
	"ByteColumnType":              {},
	"UByteColumnType":             {},
	"UShortColumnType":            {},
	"UIntegerColumnType":          {},
	"ULongColumnType":             {},
	"FloatColumnType":             {},
	"DoubleColumnType":            {},
	"DecimalColumnType":           {},
	"BooleanColumnType":           {},
	"CharacterColumnType":         {},
	"VarCharColumnType":           {},
	"TextColumnType":              {},
	"MediumTextColumnType":        {},
	"LargeTextColumnType":         {},
	"BinaryColumnType":            {},
	"BasicBinaryColumnType":       {},
	"BlobColumnType":              {},
	"UuidColumnType":              {},
	"EnumerationColumnType":       {},
	"EnumerationNameColumnType":   {},
	"CustomEnumerationColumnType": {},
	"AutoIncColumnType":           {},
	"ArrayColumnType":             {},
	"EntityIDColumnType":          {},
	"FloatVectorColumnType":       {},
	"IntVectorColumnType":         {},
	"ColumnWithTransform":         {},
	"NullableColumnWithTransform": {},
	"JoinCondition":               {},

	// Exposed DSL helpers / internals.
	"op":                      {},
	"joinPart":                {},
	"topLevelWrap":            {},
	"defaultValue":            {},
	"transformFromValue":      {},
	"transformToValue":        {},
	"columnTransformer":       {},
	"appendValueAlias":        {},
	"booleanOperator":         {},
	"resolveColumnType":       {},
	"resolveVectorColumnType": {},
	"isColumnTypeIncorrect":   {},
	"isJsonBColumnForCasting": {},
	"isInternalConstraint":    {},
	"isEntityIdentifier":      {},
	"mappedIndices":           {},
	"existingIndices":         {},
	"filterInternalIndices":   {},
	"filterForeignKeys":       {},
	"additionalConstraint":    {},
	"secondFraction":          {},
	"likePatternSpecialChars": {},
	"precessOrderByClause":    {}, // (sic) Exposed dialect hook
	"Format":                  {},

	// JVM collection types (java.util / kotlin built-ins).
	"LinkedHashMap": {},
	"LinkedHashSet": {},
	"HashMap":       {},
	"HashSet":       {},
	"ArrayList":     {},
	"Stack":         {},
	"Triple":        {},
	"Pair":          {},

	// java.time / kotlinx-datetime conversions.
	"atZone":                {},
	"atOffset":              {},
	"atTime":                {},
	"systemDefault":         {},
	"currentSystemDefault":  {},
	"toKotlinInstant":       {},
	"toKotlinLocalTime":     {},
	"toKotlinUuid":          {},
	"toJavaUuid":            {},
	"toJavaLocalTime":       {},
	"toEpochMilliseconds":   {},
	"fromEpochMilliseconds": {},
	"fromEpochSeconds":      {},
	"floorDiv":              {},

	// kotlin.collections / kotlin.sequences residue.
	"addAll":           {},
	"removeAll":        {},
	"mapValues":        {},
	"filterValues":     {},
	"filterNot":        {},
	"filterIsInstance": {},
	"subtract":         {},
	"asList":           {},
	"withIndex":        {},
	"buildList":        {},
	"ifEmpty":          {},
	"joinTo":           {},
	"none":             {},
	"flatMapTo":        {},
	"toTypedArray":     {},
	"toHashSet":        {},
	"arrayOfNulls":     {},
	"orEmpty":          {},
	"findLast":         {},

	// kotlin.text helpers.
	"replaceBefore":   {},
	"replaceRange":    {},
	"replaceFirst":    {},
	"uppercase":       {},
	"lowercase":       {},
	"ifBlank":         {},
	"isNullOrBlank":   {},
	"contentEquals":   {},
	"contentHashCode": {},
	"contentToString": {},
	"toBooleanStrict": {},
	"toShort":         {},
	"toBigInteger":    {},
	"toBigDecimal":    {},
	"toChar":          {},
	"toUByte":         {},
	"toUShort":        {},
	"toULong":         {},
	"toUInt":          {},
	"toIntArray":      {},
	"toFloatArray":    {},
	"isNaN":           {},

	// JVM concurrency leaves.
	"compareAndSet": {},
	"getOrSet":      {},
	"pop":           {},
	"peek":          {},

	// kotlin.uuid (Kotlin 2.x stdlib).
	"generateV4":    {},
	"generateV7":    {},
	"fromByteArray": {},
	"getUuid":       {},

	// java.nio.
	"allocate":       {},
	"putLong":        {},
	"codePointCount": {},

	// kotlin.reflect.
	"isSubclassOf": {},
	"callBy":       {},

	// Gradle Kotlin DSL.
	// `configure` excluded — gated to rust per rustBareNames (Actix-web App.configure).
	"signAllPublications": {},
	"useInMemoryPgpKeys":  {},

	// Misc.
	"removeAt":      {},
	"setScale":      {},
	"scale":         {},
	"precision":     {},
	"traverse":      {},
	"fromDb":        {},
	"toDb":          {},
	"BigInteger":    {},
	"Timestamp":     {},
	"List":          {},
	"Array":         {},
	"String":        {},
	"LocalDate":     {},
	"LocalDateTime": {},
	"isSupported":   {},
}

// kotlinTestBareNames is the Kotlin test-file-gated bare-name stop-list
// (issue #470). kotlin.test (`assertEquals`, `assertTrue`, `assertNull`,
// `assertContains`, `fail`, ...) and Ktor's `testApplication { ... }`
// builder (plus kotlinx-coroutines-test `runTest`) are top-level calls
// that the Kotlin extractor cannot bind to a local entity — they land
// in bug-extractor. Mirrors the Go testify gate (#115) and Java test
// gate (#120): a Kotlin test-file suffix on the caller is required so
// a production-code `assertEquals` user method is not shadowed.
//
// Conservative selection rule (#94/#106 carry-over): only the kotlin.test
// + kotlinx-coroutines-test + Ktor testApplication / ktor-server-test-host
// surface, NOT generic verbs like `verify` or `mock` (those collide too
// readily even inside test files in Kotlin codebases that use Mockito-
// Kotlin or MockK on production-shape mocks).
var kotlinTestBareNames = map[string]struct{}{
	// kotlin.test assertions.
	"assertEquals":        {},
	"assertNotEquals":     {},
	"assertTrue":          {},
	"assertFalse":         {},
	"assertNull":          {},
	"assertNotNull":       {},
	"assertSame":          {},
	"assertNotSame":       {},
	"assertContains":      {},
	"assertContentEquals": {},
	"assertFails":         {},
	"assertFailsWith":     {},
	"fail":                {},
	"expect":              {},

	// kotlinx-coroutines-test builders.
	"runTest":          {},
	"runBlockingTest":  {},
	"advanceTimeBy":    {},
	"advanceUntilIdle": {},

	// Ktor server-test-host.
	"testApplication":     {},
	"createClient":        {},
	"handleRequest":       {},
	"withTestApplication": {},
	"withApplication":     {},
	"setBody":             {},
	"addHeader":           {},
}

// isKotlinTestFile reports whether p is a Kotlin test source file by
// path convention. The Kotlin/Gradle/Maven ecosystem uses three shapes:
//
//   - `src/test/kotlin/...` (canonical JVM test source root).
//   - `src/*Test/kotlin/...` (Kotlin Multiplatform: commonTest,
//     jvmTest, nativeTest, backendTest, jsTest, iosTest, ...).
//   - `*Test.kt` / `*Tests.kt` / `*IT.kt` (file-name convention used
//     even when not under a canonical test root).
//
// Same precision bias as `isJavaTestFile` — any single shape is a
// strong-enough signal that a shared production util keeps its
// bare-name calls unresolved rather than picking up a test-only entry.
func isKotlinTestFile(p string) bool {
	if p == "" {
		return false
	}
	if strings.Contains(p, "/src/test/kotlin/") || strings.HasPrefix(p, "src/test/kotlin/") {
		return true
	}
	// KMP source-set test roots: src/<name>Test/kotlin/...
	if i := strings.Index(p, "/src/"); i >= 0 {
		rest := p[i+len("/src/"):]
		if j := strings.Index(rest, "/kotlin/"); j > 0 {
			ss := rest[:j]
			if strings.HasSuffix(ss, "Test") {
				return true
			}
		}
	}
	if strings.HasPrefix(p, "src/") {
		rest := p[len("src/"):]
		if j := strings.Index(rest, "/kotlin/"); j > 0 {
			ss := rest[:j]
			if strings.HasSuffix(ss, "Test") {
				return true
			}
		}
	}
	if strings.HasSuffix(p, "Tests.kt") || strings.HasSuffix(p, "Test.kt") || strings.HasSuffix(p, "IT.kt") {
		return true
	}
	return false
}

// scalaBareNames is the Scala-language-gated bare-name stop-list
// (play-scala-starter bug-rate reduction). The Scala extractor strips
// the receiver from a call (`Action { ... }` → `Action`,
// `Future.successful(x)` → `successful`), and the resolver can't bind
// the bare name to a local entity, so it lands in bug-extractor. The
// names below are Play Framework controller/action DSL, Akka actor /
// HTTP / Streams stdlib types, scala.concurrent / scala.util factory
// constructors, and Guice DSL helpers that have a low collision rate
// with user-defined identifiers in real Scala codebases.
//
// Conservative selection rule (lessons from #94 / #105 / #106):
// generic Scala collection ops (`map`, `flatMap`, `filter`, `fold`,
// `foreach`, `head`, `tail`, `get`, `getOrElse`, `size`, `isEmpty`)
// are deliberately EXCLUDED — every Scala codebase has user methods
// with those names and the language gate alone is not strong enough
// to prevent shadowing real missing-resolution bugs. Likewise
// `apply` is excluded — every Scala companion object defines one.
var scalaBareNames = map[string]struct{}{
	// Play Framework controller / action DSL (play.api.mvc.*). Receiver-
	// stripped from `Action { ... }`, `Ok(...)`, `Redirect(...)`,
	// `BadRequest(...)` etc. — the Play `Results` trait surface.
	"Action":              {},
	"Ok":                  {},
	"BadRequest":          {},
	"NotFound":            {},
	"InternalServerError": {},
	"Unauthorized":        {},
	"Forbidden":           {},
	"NoContent":           {},
	"Created":             {},
	"Accepted":            {},
	"Redirect":            {},
	"TemporaryRedirect":   {},
	"MovedPermanently":    {},
	"EssentialAction":     {},
	"EssentialFilter":     {},
	"Filter":              {},
	"Request":             {},
	"RequestHeader":       {},
	"AnyContent":          {},
	"AnyContentAsJson":    {},
	"WrappedRequest":      {},
	// Play form / routing helpers (play.api.data.*, play.api.routing.*).
	"Forms":        {},
	"mapping":      {},
	"nonEmptyText": {},
	"longNumber":   {},
	"number":       {},
	"optional":     {},

	// Akka actor / stream / HTTP stdlib (akka.actor.*, akka.stream.*,
	// akka.http.*). Distinctive Pascal-case types and a few high-volume
	// builder constructors. Generic combinators (`map`, `via`,
	// `runWith`) excluded — they collide with user code.
	"ActorSystem":       {},
	"ActorRef":          {},
	"ActorContext":      {},
	"Props":             {},
	"Materializer":      {},
	"ActorMaterializer": {},
	"Source":            {},
	"Sink":              {},
	"Flow":              {},
	"FlowShape":         {},
	"SourceQueue":       {},
	"BroadcastHub":      {},
	"MergeHub":          {},
	"Behaviors":         {},
	"HttpRequest":       {},
	"HttpResponse":      {},
	"StatusCodes":       {},
	"HttpEntity":        {},
	"ContentTypes":      {},

	// scala.concurrent / scala.util factory + companion-object methods
	// commonly receiver-stripped (`Future(...)`, `Promise(...)`,
	// `Future.successful(x)`, `Try(...)`). The bare type names are kept
	// — companion-object call shape is the dominant Scala idiom.
	"Future":           {},
	"Promise":          {},
	"Await":            {},
	"ExecutionContext": {},
	"Try":              {},
	"Success":          {},
	"Failure":          {},
	"Some":             {},
	"None":             {},
	"Right":            {},
	"Left":             {},
	"Either":           {},
	"successful":       {}, // Future.successful(_) → bare `successful`
	"failed":           {}, // Future.failed(_)
	// Note: `Option` is intentionally excluded — collides with user
	// "option" types in many codebases. `Some`/`None` keep enough
	// coverage for the common case.

	// Guice DI DSL surface (com.google.inject.AbstractModule).
	// `bind` and friends are receiver-stripped from `bind(classOf[X])`
	// chains inside `configure()` overrides.
	"bind":             {},
	"toInstance":       {},
	"asEagerSingleton": {},
	"in":               {}, // .in(classOf[Singleton])

	// java.util.concurrent surface frequently used from Scala
	// (Counter pattern in play-scala-starter uses `AtomicInteger
	// .getAndIncrement()`).
	"getAndIncrement": {},
	"getAndDecrement": {},
	"incrementAndGet": {},
	"decrementAndGet": {},
	"AtomicInteger":   {},
	"AtomicLong":      {},
	"AtomicReference": {},

	// Play request/response builders.
	"withHeaders": {},
	"withSession": {},
	"withCookies": {},
	"withBody":    {},
	"as":          {}, // .as("application/json") on Result

	// scalatest / scalatestplus matcher and lifecycle surface — gated
	// to scala lang only. Names are distinctive enough (`PlaySpec`,
	// `GuiceOneAppPerSuite`) to avoid user-method collisions.
	"PlaySpec":              {},
	"GuiceOneAppPerSuite":   {},
	"GuiceOneServerPerTest": {},
	"FakeRequest":           {},

	// Scala play+akka wave (post-rust). Residual bug-extractor on
	// play-scala-starter after the v1 scala stop-list was dominated by
	// two distinctive idioms the Scala extractor preserves verbatim:
	//
	//   1) `Action.async { ... }` — Play `ActionBuilder.async` factory
	//      preserved as the dotted leaf `Action.async` (not receiver-
	//      stripped to bare `async`, which would collide with user
	//      methods on any class). The literal dotted form is
	//      Play-specific and matches no real user identifier — same
	//      shape as the dotted-key entries used by the Java JAX-RS
	//      and Kafka gates.
	//
	//   2) `promise.success(_)` / `promise.failure(_)` — scala.concurrent
	//      Promise completion methods. The extractor strips the
	//      receiver and emits bare `success` / `failure`. The lang
	//      gate to scala plus the surrounding `Promise` / `Future`
	//      idiom keeps these from shadowing user methods named
	//      `success` in non-Scala codebases. Within Scala, `success` /
	//      `failure` as standalone receiver-stripped calls are
	//      overwhelmingly the Promise API; user definitions of methods
	//      with these names exist but are an order of magnitude rarer
	//      than the Promise idiom in Play / Akka / cats-effect code.
	"Action.async": {},
	"success":      {}, // promise.success(_) → bare `success`
	"failure":      {}, // promise.failure(_)
}

// rubyBareNames is the Ruby-language-gated bare-name stop-list (issue
// #107). Object/Kernel instance methods that the Ruby extractor strips
// down to the bare leaf identifier (`x.nil?` → `nil?`, `obj.to_s` →
// `to_s`) — these can't bind to a local entity and land in
// bug-extractor. Gating to lang="ruby" keeps the resolution scoped so
// JS/Python/etc. user methods named `dup`, `clone`, `freeze`,
// `respond_to?` aren't shadowed.
//
// Conservative selection rule (lessons from #94 / #105 / #106):
// generic collection ops (`each`, `map`, `select`, `find`, `count`,
// `length`, `size`) are deliberately EXCLUDED. They are user-method
// names on any class in any language and the language gate alone is
// not strong enough to keep them safe.
//
// Rails ActionController DSL (`render`/`params`/`before_action`/...)
// and ActiveRecord query builders (`where`/`order`/`has_many`/...) are
// classified by the resolver-side rubyDynamicPatterns catalog (Refs
// refs.go) as Dispositional Dynamic instead of synthesised externals,
// because those names ARE method_missing-generated rather than stable
// stdlib functions.
var rubyBareNames = map[string]struct{}{
	// Object / BasicObject lifecycle and identity.
	"new":         {},
	"nil?":        {},
	"present?":    {},
	"blank?":      {},
	"respond_to?": {},
	"class":       {},
	// `send` is intentionally OMITTED — it's classified as Dynamic by
	// the resolver-side rubyDynamicPatterns catalog (reflective
	// dispatch), which is a stronger signal than ExternalKnown.
	"tap":        {},
	"then":       {},
	"yield_self": {},
	"dup":        {},
	"clone":      {},
	"freeze":     {},
	"frozen?":    {},
	"object_id":  {},
	// Type coercion (Object#to_*).
	"to_s":   {},
	"to_str": {},
	"to_i":   {},
	"to_f":   {},
	"to_a":   {},
	"to_h":   {},
	"to_sym": {},
	// Inspection / type checks.
	"inspect":      {},
	"is_a?":        {},
	"kind_of?":     {},
	"instance_of?": {},
	// ActiveRecord persistence and validation methods (issue #124).
	// After #107 + #143, residual rails-realworld bug-extractor was
	// dominated by AR persistence calls (`record.save`, `user.update!`,
	// `model.valid_password?`). These names ARE generated/inherited from
	// ActiveRecord::Base, not user-defined — extractor strips the
	// receiver and the resolver sees only the bare leaf. The Ruby
	// language gate keeps them from polluting other ecosystems. Generic
	// collection ops (`find`, `count`) remain EXCLUDED per #107 lock-in.
	// `new` and `where` are intentionally omitted: `new` is already
	// covered above; `where` is classified by the resolver-side
	// rubyDynamicPatterns catalog as Dynamic.
	"save":              {},
	"save!":             {},
	"update":            {},
	"update!":           {},
	"destroy":           {},
	"destroy!":          {},
	"valid?":            {},
	"valid_password?":   {},
	"errors":            {},
	"persisted?":        {},
	"new_record?":       {},
	"attributes":        {},
	"reload":            {},
	"create":            {},
	"create!":           {},
	"find_or_create_by": {},
	"build":             {},
	"exists?":           {},
	"first":             {},
	"last":              {},
	"all":               {},
	// Additional AR persistence/query methods observed in
	// rails-realworld bug-extractor after the initial #124 batch. All
	// are stable AR::Base / AR::Relation methods (not method_missing).
	"find_by":                  {},
	"find_each":                {},
	"find_in_batches":          {},
	"destroy_all":              {},
	"delete_all":               {},
	"update_all":               {},
	"update_attribute":         {},
	"update_attributes":        {},
	"update_attributes!":       {},
	"toggle":                   {},
	"toggle!":                  {},
	"increment":                {},
	"increment!":               {},
	"decrement":                {},
	"decrement!":               {},
	"touch":                    {},
	"reset_counters":           {},
	"reset_column_information": {},
	// Numeric / time helpers added by ActiveSupport to Numeric
	// (`3.days`, `1.hour.ago`, `5.minutes.from_now`). Ruby-only by
	// virtue of ActiveSupport's monkey-patches; the language gate
	// keeps them from polluting non-Ruby ecosystems.
	"days":     {},
	"hours":    {},
	"minutes":  {},
	"seconds":  {},
	"weeks":    {},
	"months":   {},
	"years":    {},
	"ago":      {},
	"from_now": {},

	// Sidekiq gem DSL and Redis pipeline (issue #449). sidekiq
	// repo bug-extractor was 15.24% after #107/#124 — residual was
	// dominated by Sidekiq's worker-DSL, middleware-chain lifecycle,
	// Sidekiq::Client push surface, job context accessors, and the
	// raw Redis pipeline methods that Sidekiq exposes via its
	// connection-pool yield (`Sidekiq.redis { |conn| conn.pipelined ... }`).
	// The Ruby extractor strips the receiver from a builder/yield
	// call (`MyWorker.perform_async(args)` → `perform_async`,
	// `conn.hset(k, f, v)` → `hset`), so the resolver only sees
	// the bare leaf identifier — it lands in bug-extractor. These
	// names are stable methods on Sidekiq::Worker / Sidekiq::Client /
	// Sidekiq::Middleware::Chain / Redis::Client (NOT method_missing-
	// generated), so the bare-name allowlist is the right tool —
	// mirrors the AR persistence additions (#124) precedent.
	//
	// Conservative selection (lessons from #94 / #105 / #106 / #107):
	// the per-language gate (lang == "ruby") is what makes generic
	// verbs like `set`, `push`, `add`, `remove`, `clear`, `multi`,
	// `exec`, `on`, `retry`, `queue` safe — they cannot shadow user
	// methods in Go/JS/Python/Java/Kotlin/Swift/Rust codebases.
	// Names already classified by stdlibBareNames (`set`), rustBareNames
	// (`push`, `remove`), jsBareNames (`push`), or swiftBareNames
	// (`on`) are still listed here so the Ruby gate is self-documenting;
	// the language-agnostic / sibling-language maps fire first when
	// applicable, but listing the names here keeps the Sidekiq surface
	// complete in one place.
	//
	// Categories:
	//   - Sidekiq::Worker DSL (`perform_async`, `perform_in`,
	//     `perform_at`, `perform_bulk`, `set`, `enqueue`,
	//     `enqueue_to`, `enqueue_to_in`, `sidekiq_options`,
	//     `sidekiq_retry_in`, `sidekiq_retries_exhausted`).
	//   - Sidekiq::Middleware::Chain (`register`, `add`, `remove`,
	//     `clear`, `prepend`, `entries`, `exists?` — `exists?`
	//     already covered above by the AR persistence block).
	//   - Sidekiq config / lifecycle (`redis`, `logger`,
	//     `concurrency`, `queues`, `strict`, `error_handlers`,
	//     `death_handlers`, `on`, `lifecycle_events`).
	//   - Job context accessors (`jid`, `bid`, `args`, `klass`,
	//     `queue`, `retry`, `created_at`, `enqueued_at`).
	//   - Sidekiq::Client (`push`, `push_bulk`).
	//   - Redis pipeline / multi-exec / hash / list / set / sorted-set
	//     commands exposed by Sidekiq's connection-pool yield
	//     (`pipelined`, `multi`, `exec`, `discard`, `watch`,
	//     `unwatch`, `hset`, `hget`, `hgetall`, `lpush`, `rpush`,
	//     `lpop`, `rpop`, `sadd`, `srem`, `smembers`, `zadd`,
	//     `zrem`, `zrange`, `zrangebyscore`).
	"perform_async":             {},
	"perform_in":                {},
	"perform_at":                {},
	"perform_bulk":              {},
	"enqueue":                   {},
	"enqueue_to":                {},
	"enqueue_to_in":             {},
	"sidekiq_options":           {},
	"sidekiq_retry_in":          {},
	"sidekiq_retries_exhausted": {},
	// Sidekiq middleware chain. `exists?` already in AR block above.
	"add":     {},
	"clear":   {},
	"prepend": {},
	"entries": {},
	// Sidekiq config / lifecycle.
	"redis":            {},
	"logger":           {},
	"concurrency":      {},
	"queues":           {},
	"strict":           {},
	"error_handlers":   {},
	"death_handlers":   {},
	"on":               {},
	"lifecycle_events": {},
	// Job context accessors.
	"jid":         {},
	"bid":         {},
	"args":        {},
	"klass":       {},
	"queue":       {},
	"retry":       {},
	"created_at":  {},
	"enqueued_at": {},
	// Sidekiq::Client.
	"push":      {},
	"push_bulk": {},
	// Redis pipeline / multi-exec / hash / list / set / sorted-set.
	"pipelined":     {},
	"multi":         {},
	"exec":          {},
	"discard":       {},
	"watch":         {},
	"unwatch":       {},
	"hset":          {},
	"hget":          {},
	"hgetall":       {},
	"lpush":         {},
	"rpush":         {},
	"lpop":          {},
	"rpop":          {},
	"sadd":          {},
	"srem":          {},
	"smembers":      {},
	"zadd":          {},
	"zrem":          {},
	"zrange":        {},
	"zrangebyscore": {},
	// `set` and `remove` belong to the worker DSL / middleware chain
	// surface too, but are already classified language-agnostically
	// (stdlibBareNames["set"]) or by another language gate
	// (rustBareNames["remove"]) — not duplicated here to avoid map
	// literal duplicate-key compile errors. Same rationale for `push`
	// being listed once above under Sidekiq::Client.

	// Sidekiq wave 2 additions (issue #449 follow-up). After the initial
	// #449 Sidekiq batch + #107 / #124 / #143 the sidekiq corpus was
	// still ~12% bug-extractor. Top residual leaves fell into four
	// categories:
	//
	//   1. Ruby Object/Kernel/Enumerable introspection that the
	//      conservative #107 selection deliberately skipped — names
	//      that ARE Ruby-idiomatic enough for the language gate alone
	//      to be safe (predicates like `even?`/`zero?`/`block_given?`,
	//      reflection like `instance_variable_get`/`method_defined?`,
	//      and `?`/`!`-suffixed forms that almost never appear in other
	//      languages).
	//   2. Additional Enumerable / collection ops with `?`/`!` suffixes
	//      and ActiveSupport extensions (`deep_symbolize_keys!`,
	//      `deep_transform_keys!`, `flat_map`, `each_with_object`,
	//      `sort_by`). Generic bare names (`each`/`map`/`find`/`count`)
	//      remain EXCLUDED.
	//   3. Sidekiq-exposed Redis command surface that #449 covered
	//      partially — completing with `hmget`/`hsetnx`/`hincrby`/`mget`/
	//      `mset`/`incrby`/`expire`/`lindex`/`llen`/`lrange`/`lrem`/
	//      `sadd`/`scard`/`sismember`/`smembers`/`srem`/`sscan`/`zcard`/
	//      `zincrby`/`zpopmin`/`zremrangebyrank`/`zremrangebyscore`/
	//      `zscan`/`bitfield`/`bitfield_ro`/`pcount`/`pipe`.
	//   4. Sidekiq Pro / Ent block-form options (`sidekiq_options_hash`,
	//      `sidekiq_retries_exhausted_block`, `sidekiq_retry_in_block`)
	//      and helper methods on Sidekiq::Job / Sidekiq::Config.
	//
	// Category 1 — Object / Kernel / introspection (?-predicate forms).
	"all?":                       {},
	"any?":                       {},
	"none?":                      {},
	"one?":                       {},
	"empty?":                     {},
	"zero?":                      {},
	"even?":                      {},
	"odd?":                       {},
	"positive?":                  {},
	"negative?":                  {},
	"eql?":                       {},
	"equal?":                     {},
	"key?":                       {},
	"has_key?":                   {},
	"has_value?":                 {},
	"include?":                   {},
	"member?":                    {},
	"cover?":                     {},
	"match?":                     {},
	"start_with?":                {},
	"end_with?":                  {},
	"casecmp?":                   {},
	"block_given?":               {},
	"tainted?":                   {},
	"untrusted?":                 {},
	"singleton_class?":           {},
	"method_defined?":            {},
	"private_method_defined?":    {},
	"public_method_defined?":     {},
	"protected_method_defined?":  {},
	"const_defined?":             {},
	"instance_variable_defined?": {},
	"class_variable_defined?":    {},
	"respond_to_missing?":        {},
	"directory?":                 {},
	"file?":                      {},
	"exist?":                     {},
	// "exists?" — covered in AR persistence block.
	"executable?":  {},
	"readable?":    {},
	"writable?":    {},
	"socket?":      {},
	"symlink?":     {},
	"pipe?":        {},
	"tty?":         {},
	"filtering?":   {},
	"stopping?":    {},
	"using_mysql?": {},
	// Category 1b — reflection / metaprogramming.
	"caller":                  {},
	"cause":                   {},
	"ancestors":               {},
	"const_get":               {},
	"const_set":               {},
	"constantize":             {},
	"define_singleton_method": {},
	"singleton_methods":       {},
	"singleton_method":        {},
	"instance_methods":        {},
	"instance_method":         {},
	"public_methods":          {},
	"private_methods":         {},
	"protected_methods":       {},
	"methods":                 {},
	"instance_variables":      {},
	"instance_variable_get":   {},
	"instance_variable_set":   {},
	"class_variables":         {},
	"class_variable_get":      {},
	"class_variable_set":      {},
	"instance_exec":           {},
	"class_exec":              {},
	"undef_method":            {},
	"alias_method":            {},
	"remove_method":           {},
	"attr_writer":             {},
	"attr_reader":             {},
	"attr_accessor":           {},
	"public":                  {},
	"private":                 {},
	"protected":               {},
	"private_class_method":    {},
	"public_class_method":     {},
	"private_constant":        {},
	"public_constant":         {},
	"extend_object":           {},
	"prepend_features":        {},
	"append_features":         {},
	"included_modules":        {},
	// "object_id" — covered above in the Object/Kernel block.
	"hash":   {},
	"itself": {},
	// Category 2 — Enumerable / collection ops with ?/! suffix or AS extensions.
	// Generic bare ops (each/map/find/count/length/size/select/reject)
	// remain EXCLUDED per #107 lock-in. The ?/! suffix variants and
	// AS-specific names are Ruby-idiomatic enough for the language gate.
	"each_pair":      {},
	"each_value":     {},
	"each_index":     {},
	"each_slice":     {},
	"each_cons":      {},
	"each_entry":     {},
	"flat_map":       {},
	"collect_concat": {},
	"sort_by":        {},
	"min_by":         {},
	"max_by":         {},
	"minmax":         {},
	"minmax_by":      {},
	"sum":            {},
	"product":        {},
	"chunk":          {},
	"chunk_while":    {},
	"slice_when":     {},
	"slice_before":   {},
	"slice_after":    {},
	"zip":            {},
	"reverse_each":   {},
	"reverse!":       {},
	"sort!":          {},
	"uniq!":          {},
	"flatten!":       {},
	"compact!":       {},
	"map!":           {},
	"collect!":       {},
	"select!":        {},
	"reject!":        {},
	"merge!":         {},
	"delete_if":      {},
	"keep_if":        {},
	"delete_at":      {},
	"delete_prefix":  {},
	"delete_suffix":  {},
	"values_at":      {},
	"with_index":     {},
	"with_object":    {},
	"detect":         {},
	"take":           {},
	"take_while":     {},
	"drop":           {},
	"drop_while":     {},
	"lazy":           {},
	"force":          {},
	"permute":        {},
	"permutation":    {},
	"combination":    {},
	"sample":         {},
	"shuffle":        {},
	"shuffle!":       {},
	"rotate":         {},
	"rotate!":        {},
	// Hash / Array ActiveSupport extensions.
	"deep_symbolize_keys":     {},
	"deep_symbolize_keys!":    {},
	"deep_transform_keys":     {},
	"deep_transform_keys!":    {},
	"deep_dup":                {},
	"deep_merge":              {},
	"deep_merge!":             {},
	"transform_values!":       {},
	"transform_keys!":         {},
	"assert_valid_keys":       {},
	"reverse_merge":           {},
	"reverse_merge!":          {},
	"symbolize_keys":          {},
	"symbolize_keys!":         {},
	"stringify_keys":          {},
	"stringify_keys!":         {},
	"with_indifferent_access": {},
	// Category 2b — String / Numeric / IO methods (Ruby-idiomatic).
	"gsub":        {},
	"gsub!":       {},
	"sub!":        {},
	"scan":        {},
	"squeeze":     {},
	"squeeze!":    {},
	"tr":          {},
	"tr!":         {},
	"tr_s":        {},
	"chomp":       {},
	"chomp!":      {},
	"chop":        {},
	"chop!":       {},
	"chars":       {},
	"bytes":       {},
	"codepoints":  {},
	"lines":       {},
	"lstrip":      {},
	"lstrip!":     {},
	"rstrip":      {},
	"rstrip!":     {},
	"strip":       {},
	"strip!":      {},
	"ljust":       {},
	"rjust":       {},
	"center":      {},
	"upcase":      {},
	"upcase!":     {},
	"downcase":    {},
	"downcase!":   {},
	"capitalize":  {},
	"capitalize!": {},
	"swapcase":    {},
	"swapcase!":   {},
	// "reverse!" — covered above in the Enumerable/array block.
	"unpack":          {},
	"unpack1":         {},
	"pack":            {},
	"hex":             {},
	"oct":             {},
	"force_encoding":  {},
	"encode":          {},
	"encode!":         {},
	"encoding":        {},
	"bytesize":        {},
	"valid_encoding?": {},
	"ascii_only?":     {},
	"named_captures":  {},
	"scrub":           {},
	"scrub!":          {},
	"intern":          {},
	"to_proc":         {},
	"safe_load":       {},
	"safe_load_file":  {},
	"parse!":          {},
	// Numeric / Time idioms.
	"downto":    {},
	"upto":      {},
	"step":      {},
	"abs":       {},
	"abs2":      {},
	"round":     {},
	"floor":     {},
	"ceil":      {},
	"truncate":  {},
	"divmod":    {},
	"modulo":    {},
	"remainder": {},
	"fdiv":      {},
	"gcd":       {},
	"lcm":       {},
	"coerce":    {},
	"finite?":   {},
	"infinite?": {},
	"nan?":      {},
	"integer?":  {},
	"real?":     {},
	"to_r":      {},
	"to_c":      {},
	// Time / DateTime accessors (ActiveSupport extends Numeric and Time
	// with these; the resolver sees them as bare leaves).
	"hour":               {},
	"min":                {},
	"sec":                {},
	"mday":               {},
	"wday":               {},
	"yday":               {},
	"year":               {},
	"month":              {},
	"today":              {},
	"yesterday":          {},
	"tomorrow":           {},
	"beginning_of_day":   {},
	"end_of_day":         {},
	"beginning_of_week":  {},
	"end_of_week":        {},
	"beginning_of_month": {},
	"end_of_month":       {},
	"beginning_of_year":  {},
	"end_of_year":        {},
	"utc":                {},
	"getutc":             {},
	"localtime":          {},
	"iso8601":            {},
	"strftime":           {},
	"strptime":           {},
	"to_time":            {},
	"to_datetime":        {},
	"in_time_zone":       {},
	// Kernel / lifecycle.
	"raise":   {},
	"throw":   {},
	"catch":   {},
	"loop":    {},
	"sleep":   {},
	"exit":    {},
	"exit!":   {},
	"abort":   {},
	"at_exit": {},
	"trap":    {},
	"fork":    {},
	"spawn":   {},
	"system":  {},
	// "exec" — covered above (Sidekiq Redis pipeline block).
	"open":             {},
	"popen":            {},
	"gets":             {},
	"puts":             {},
	"print":            {},
	"printf":           {},
	"sprintf":          {},
	"format":           {},
	"pp":               {},
	"p":                {},
	"j":                {},
	"l":                {},
	"t":                {},
	"warn":             {},
	"caller_locations": {},
	"handle_interrupt": {},
	"binding":          {},
	"__method__":       {},
	"__callee__":       {},
	"__dir__":          {},
	"__FILE__":         {},
	// FileUtils / File / Pathname helpers.
	"expand_path":   {},
	"basename":      {},
	"dirname":       {},
	"extname":       {},
	"realpath":      {},
	"realdirpath":   {},
	"absolute_path": {},
	"pwd":           {},
	"chdir":         {},
	"glob":          {},
	"rm_f":          {},
	"rm_rf":         {},
	"mv":            {},
	"cp":            {},
	"cp_r":          {},
	"mkdir":         {},
	"mkdir_p":       {},
	"rmdir":         {},
	// "touch" — covered in AR persistence block.
	"chmod":         {},
	"chown":         {},
	"tmpdir":        {},
	"mktmpdir":      {},
	"unlink":        {},
	"close":         {},
	"close_on_exec": {},
	"closed?":       {},
	"flush":         {},
	"sync":          {},
	"binmode":       {},
	"binmode?":      {},
	"isatty":        {},
	"rewind":        {},
	"seek":          {},
	"tell":          {},
	"pos":           {},
	"eof":           {},
	"eof?":          {},
	"wait_readable": {},
	"wait_writable": {},
	"blocking_call": {},
	// Concurrency / Thread / Synchronization.
	"synchronize":     {},
	"mon_synchronize": {},
	"try_lock":        {},
	"locked?":         {},
	"owned?":          {},
	"alive?":          {},
	"join":            {},
	"value":           {},
	"signal":          {},
	"broadcast":       {},
	"wait":            {},
	"wakeup":          {},
	"thread_priority": {},
	// Category 3 — Redis command surface (Sidekiq.redis { |conn| conn.<cmd> }).
	"hmget":        {},
	"hmset":        {},
	"hsetnx":       {},
	"hincrby":      {},
	"hincrbyfloat": {},
	"hdel":         {},
	"hexists":      {},
	"hkeys":        {},
	"hvals":        {},
	"hlen":         {},
	"hscan":        {},
	"hstrlen":      {},
	"mget":         {},
	"mset":         {},
	"msetnx":       {},
	"incrby":       {},
	"incrbyfloat":  {},
	"decrby":       {},
	"setnx":        {},
	"setex":        {},
	"psetex":       {},
	"getset":       {},
	"getdel":       {},
	"getex":        {},
	"strlen":       {},
	"append":       {},
	"setrange":     {},
	"getrange":     {},
	"expire":       {},
	"pexpire":      {},
	"expireat":     {},
	"pexpireat":    {},
	"persist":      {},
	"ttl":          {},
	"pttl":         {},
	"type":         {},
	"rename":       {},
	"renamenx":     {},
	"randomkey":    {},
	"dump":         {},
	"restore":      {},
	// "scan" — covered above in the String block; Redis SCAN reuses
	// the same bare name and the Ruby gate covers both.
	"lindex":     {},
	"linsert":    {},
	"llen":       {},
	"lrange":     {},
	"lrem":       {},
	"lset":       {},
	"ltrim":      {},
	"lmove":      {},
	"rpoplpush":  {},
	"brpoplpush": {},
	"blpop":      {},
	"brpop":      {},
	// "sadd" / "smembers" / "srem" — covered above (#449 Redis block).
	"scard":       {},
	"sismember":   {},
	"smismember":  {},
	"sscan":       {},
	"spop":        {},
	"srandmember": {},
	"sdiff":       {},
	"sinter":      {},
	"sunion":      {},
	"sdiffstore":  {},
	"sinterstore": {},
	"sunionstore": {},
	"smove":       {},
	// "zadd" — covered above (#449 Redis block).
	"zcard":    {},
	"zcount":   {},
	"zincrby":  {},
	"zpopmin":  {},
	"zpopmax":  {},
	"bzpopmin": {},
	"bzpopmax": {},
	// "zrange" / "zrangebyscore" / "zrem" — covered above (#449 Redis block).
	"zrangebylex":      {},
	"zrevrange":        {},
	"zrevrangebyscore": {},
	"zrevrangebylex":   {},
	"zremrangebyrank":  {},
	"zremrangebyscore": {},
	"zrank":            {},
	"zrevrank":         {},
	"zscore":           {},
	"zmscore":          {},
	"zscan":            {},
	"zinterstore":      {},
	"zunionstore":      {},
	"zdiffstore":       {},
	"bitfield":         {},
	"bitfield_ro":      {},
	"bitcount":         {},
	"bitop":            {},
	"bitpos":           {},
	"getbit":           {},
	"setbit":           {},
	"pfadd":            {},
	"pfcount":          {},
	"pfmerge":          {},
	"pcount":           {},
	"xadd":             {},
	"xread":            {},
	"xreadgroup":       {},
	"xack":             {},
	"xlen":             {},
	"xrange":           {},
	"xrevrange":        {},
	"xtrim":            {},
	"xdel":             {},
	"xpending":         {},
	"xinfo":            {},
	"xgroup":           {},
	"subscribe":        {},
	"unsubscribe":      {},
	"psubscribe":       {},
	"punsubscribe":     {},
	"publish":          {},
	"pubsub":           {},
	"ping":             {},
	"auth":             {},
	"select_db":        {},
	"client_id":        {},
	"client_kill":      {},
	"client_list":      {},
	"client_getname":   {},
	"client_setname":   {},
	"info":             {},
	"dbsize":           {},
	"flushdb":          {},
	"flushall":         {},
	"keys":             {},
	"sort":             {},
	"slaveof":          {},
	"replicaof":        {},
	"sentinel":         {},
	// Category 4 — Sidekiq Pro / Ent / Job context completions.
	"sidekiq_options_hash":            {},
	"sidekiq_retries_exhausted_block": {},
	"sidekiq_retry_in_block":          {},
	"sidekiq_class_attribute":         {},
	"server_url":                      {},
	"redis_pool":                      {},
	"redis_info":                      {},
	"local_redis_pool":                {},
	"client_middleware":               {},
	"server_middleware":               {},
	"handle_exception":                {},
	"total_concurrency":               {},
	"reap":                            {},
	"watchdog":                        {},
	"pause!":                          {},
	"unpause!":                        {},
	"reloader":                        {},
	"in_batches":                      {},
	"msg_retry":                       {},
	"provider_job_id":                 {},
	"job_class":                       {},
	"job_args":                        {},
	"job_results":                     {},
	"enqueued_count":                  {},
	"scheduled_at":                    {},
	"display_args":                    {},
	"set_tab":                         {},
	"base_tab":                        {},

	// Sidekiq wave 2 Pass-2 additions (issue #449). After Pass-1 the
	// residual was still ~5.9% — dominated by Enumerable / Hash / Array /
	// String collection ops that the conservative #107 policy left out.
	// Per-language gate (Ruby) confines the resolution scope; the
	// names below are Ruby-stdlib core methods, not user-method names
	// likely to collide WITHIN a single Ruby codebase. Classifying
	// these as ExternalKnown instead of bug-extractor is the right
	// trade — a small false-positive risk in exchange for moving the
	// ship-gate-blocking residual from sidekiq/rails ecosystems.
	"compact":          {},
	"dig":              {},
	"merge":            {},
	"flatten":          {},
	"uniq":             {},
	"partition":        {},
	"group_by":         {},
	"each_with_index":  {},
	"each_with_object": {},
	"transform_keys":   {},
	"transform_values": {},
	"to_date":          {},
	"to_enum":          {},
	"reverse":          {},
	"read":             {},
	"write":            {},
	"shift":            {},
	"pop":              {},
	"replace":          {},
	"length":           {},
	"index":            {},
	"sub":              {},
	"now":              {},
	"escape":           {},
	"escape_html":      {},
	"unescape_path":    {},
	"extend":           {},
	"include":          {},
	"load":             {},
	"method":           {},
	"parameters":       {},
	"serialize":        {},
	"deserialize":      {},
	"set_backtrace":    {},
	"backtrace":        {},
	"message":          {},
	"yield":            {},
	"copy":             {},
	"draw":             {},
	"fail":             {},
	"generate":         {},
	"generators":       {},
	"deflate":          {},
	"inflate":          {},
	"gethostname":      {},
	"clock_gettime":    {},
	"poll_event":       {},

	// Pass-3 additions (Sidekiq wave 2). Final batch — Hash/Numeric
	// stdlib core methods that surface in Sidekiq's Pro UI / metrics
	// code. `times` is Integer#times (`5.times { ... }`); `except` is
	// Hash#except (ActiveSupport-extended on stdlib Hash). Both are
	// Ruby-idiomatic enough for the language gate.
	"except": {},
	"times":  {},

	// rails-realworld additions (issue #449 follow-up). ActionController
	// helpers and ActiveSupport extensions that the Ruby extractor strips
	// to bare leaves. Per-language gate keeps the names from polluting
	// non-Ruby ecosystems.
	"authenticate_or_request_with_http_token": {},
	"authenticate_or_request_with_http_basic": {},
	"authenticate_with_http_token":            {},
	"authenticate_with_http_basic":            {},
	"head":                                    {},
	"secret_key_base":                         {},
	"secrets":                                 {},
	"credentials":                             {},
	"application":                             {},
}

// jsBareNames is the JS/TS-language-gated bare-name stop-list (issue
// #104). Two families covered:
//
//  1. Prisma ORM client method names. The JS extractor strips the
//     receiver (`prisma.user.findMany(...)` → bare `findMany`), so
//     the resolver only sees the leaf identifier. These collide with
//     user methods in OTHER ecosystems (Ruby/Java/Go all have classes
//     with their own `update`/`delete`/`create` methods) so the
//     language gate is required.
//  2. JS/TS array & util builtins (`some`, `every`, `push`, `trim`,
//     `isArray`) that bare-call after receiver-strip and reach the
//     resolver as leaf identifiers.
//
// Conservative selection rule (lessons from #94 / #105 / #106 / #107):
// generic collection ops (`map`, `filter`, `forEach`, `reduce`,
// `find`, `length`, `size`) are deliberately EXCLUDED. They are
// user-method names on any class in any language and the language
// gate alone is not strong enough to keep them safe — JS/TS share
// the `map`/`filter`/`forEach` namespace with hand-rolled domain
// methods on user classes too readily.
var jsBareNames = map[string]struct{}{
	// Prisma ORM client surface (https://www.prisma.io/docs/orm/reference/prisma-client-reference)
	"findUnique":        {},
	"findUniqueOrThrow": {},
	"findFirst":         {},
	"findFirstOrThrow":  {},
	"findMany":          {},
	"createMany":        {},
	"updateMany":        {},
	"deleteMany":        {},
	"upsert":            {},
	"aggregate":         {},
	"groupBy":           {},
	"executeRaw":        {},
	"executeRawUnsafe":  {},
	"queryRaw":          {},
	"queryRawUnsafe":    {},
	// Prisma `$`-prefixed top-level client methods. `$` is rare in
	// user-defined identifier names so these are unambiguous.
	"$connect":     {},
	"$disconnect":  {},
	"$transaction": {},
	"$queryRaw":    {},
	"$executeRaw":  {},
	"$on":          {},
	"$use":         {},
	// `create`, `update`, `delete`, `count` are intentionally OMITTED.
	// They overlap heavily with non-Prisma user methods (controllers,
	// services, factories) and the per-language gate isn't enough.
	// They land in bug-extractor instead — acceptable trade-off vs
	// false positives that hide real local entities.

	// JS/TS array & util builtins. Names that bare-call after
	// receiver-strip (`xs.some(p)` → `some`).
	"some":    {},
	"every":   {},
	"push":    {},
	"trim":    {},
	"isArray": {},
	// Issue #44 / GraphQL-fix — apollo-server bug-extractor residue
	// dominated by JS test-framework + JS built-in receiver-strip:
	// `expect(x).toBe(y)`, `JSON.stringify(...)`, `Promise.resolve(...)`,
	// `xs.forEach(...)`, `errs.catch(...)`. These are all language-level
	// builtins that the JS/TS extractor receiver-strips to leaf names.
	// Conservative: limited to names where collision with a hand-rolled
	// user method in JS/TS code is unlikely. Jest globals are gated by
	// jsBareNames + lang=="javascript"/"typescript" already.
	"expect":                 {}, // jest assertion entry point
	"toBe":                   {}, // jest matcher
	"toEqual":                {},
	"toBeTruthy":             {},
	"toBeFalsy":              {},
	"toBeNull":               {},
	"toBeUndefined":          {},
	"toBeDefined":            {},
	"toBeInstanceOf":         {},
	"toContain":              {},
	"toContainEqual":         {},
	"toHaveBeenCalled":       {},
	"toHaveBeenCalledWith":   {},
	"toHaveBeenCalledTimes":  {},
	"toHaveLength":           {},
	"toHaveProperty":         {},
	"toMatch":                {},
	"toMatchObject":          {},
	"toMatchSnapshot":        {},
	"toMatchInlineSnapshot":  {},
	"toThrow":                {},
	"toThrowError":           {},
	"toStrictEqual":          {},
	"resolves":               {},
	"rejects":                {},
	"toBeGreaterThan":        {},
	"toBeGreaterThanOrEqual": {},
	"toBeLessThan":           {},
	"toBeLessThanOrEqual":    {},
	"toBeCloseTo":            {},
	// JSON / Math / Promise / Array static methods (receiver-stripped).
	"stringify":  {},
	"parse":      {},
	"resolve":    {}, // Promise.resolve / require.resolve
	"reject":     {}, // Promise.reject
	"all":        {},
	"allSettled": {},
	"race":       {},
	"any":        {},
	// Array / iterable callbacks (already had `some`, `every`, `push`).
	// `forEach` intentionally OMITTED — collision-prone per issue #104
	// rejection list (TestJSBareNames_RejectedNamesNotClassified).
	"toString":             {},
	"valueOf":              {},
	"hasOwnProperty":       {},
	"isPrototypeOf":        {},
	"propertyIsEnumerable": {},
	// Common console/logger methods (receiver-stripped from `logger.warn`,
	// `console.log`, `log.error`). High volume in apollo-server.
	"warn":  {},
	"error": {},
	"info":  {},
	"debug": {},
	// JS try/catch keyword leaks through some TS extractor paths as a
	// bare-name CALL target (`promise.catch(handler)` AND syntactic
	// `try {} catch {}`). Either form has no entity target; classify
	// as external builtin.
	"catch":   {},
	"finally": {},
	// `then` is already in rubyBareNames (ruby gate). Don't duplicate
	// here — it would fire for both gates and the cross-language
	// invariant tests reject that.
	// Node.js / browser globals (receiver-stripped or top-level).
	"encodeURIComponent": {},
	"decodeURIComponent": {},
	"encodeURI":          {},
	"decodeURI":          {},
	"setTimeout":         {},
	"setImmediate":       {},
	"setInterval":        {},
	"clearTimeout":       {},
	"clearImmediate":     {},
	"clearInterval":      {},
	"queueMicrotask":     {},
	"structuredClone":    {},
	// Node crypto receiver-strip (`crypto.createHash(...).update(d).digest('hex')`).
	"createHash":     {},
	"createHmac":     {},
	"createCipher":   {},
	"createDecipher": {},
	"digest":         {},
	"randomUUID":     {},
	"randomBytes":    {},
	"pbkdf2":         {},
	"pbkdf2Sync":     {},
	// String / Array transforms (receiver-strip).
	"toLowerCase": {},
	"toUpperCase": {},
	"toJSON":      {},
	"shift":       {},
	"unshift":     {},
	"reverse":     {},
	"sort":        {},
	"fill":        {},
	"flat":        {},
	// `flatMap` already in swiftBareNames; cross-lang invariant test
	// rejects duplication.
	"keys":        {},
	"values":      {},
	"entries":     {},
	"fromEntries": {},
	"assign":      {},
	// `freeze`/`isFrozen` — `freeze` is in rubyBareNames; skip duplication.
	// Buffer / Array.from
	"from":        {},
	"of":          {},
	"isBuffer":    {},
	"alloc":       {},
	"allocUnsafe": {},
	// graphql-tag / graphql-tools shorthand (often called bare after
	// `import { gql } from 'graphql-tag'`).
	"gql":                  {},
	"makeExecutableSchema": {},
	"buildSubgraphSchema":  {},
	"buildSchema":          {},
	"printSchema":          {},
	"validateSchema":       {},
	"execute":              {},
	"subscribe":            {},
	"graphql":              {},
	"graphqlSync":          {},
	"parseValue":           {},
	"valueFromAST":         {},
	"astFromValue":         {},
	// LRU-cache / make-fetch-happen / negotiator named exports.
	"LRUCache": {},
	// Math / Date / Number static methods.
	"floor": {}, "ceil": {}, "round": {}, "abs": {}, "min": {}, "max": {},
	"pow": {}, "sqrt": {}, "log": {}, "log2": {}, "log10": {}, "exp": {},
	"sin": {}, "cos": {}, "tan": {}, "atan": {}, "atan2": {}, "asin": {}, "acos": {},
	"random": {},
	"now":    {}, // Date.now / performance.now
	"hrtime": {}, // process.hrtime
	// Buffer.byteLength etc.
	"byteLength": {},
	"isInteger":  {},
	"isFinite":   {},
	"isNaN":      {},
	"parseInt":   {},
	"parseFloat": {},
	// Jest top-level globals (already have @jest/globals on allowlist, but
	// `describe`/`it`/`test`/`beforeEach`/`afterEach` are imported as bare
	// names too and receiver-strip to bug-extractor when extractor can't
	// bind them to the package).
	"describe":   {},
	"it":         {},
	"test":       {},
	"beforeEach": {},
	"afterEach":  {},
	"beforeAll":  {},
	"afterAll":   {},
	"fn":         {}, // jest.fn — receiver-stripped
	"spyOn":      {},
	"mock":       {},
	"unmock":     {},
	"jest":       {}, // bare `jest.X` reference
	// `pop` / `shift` / `unshift` / `splice` / `slice` / `concat` /
	// `join` / `includes` / `indexOf` / `lastIndexOf` / `flat` /
	// `flatMap` are deliberately OMITTED for this iteration: each is
	// either too collision-prone (`includes`, `indexOf`) or
	// insufficiently observed in #104's bug-extractor sample to
	// justify carrying the false-positive risk.
	//
	// Wave-4 (TS framework / React + Next.js) — high-frequency hook,
	// Next.js navigation, and React Server Component primitives that
	// receiver-strip to bare-name CALLS in the JS/TS extractor. Each
	// name is a real exported function in `react`, `react-dom`, or
	// `next/*` whose collision with a hand-rolled user method is
	// vanishingly rare (the `use*` convention is reserved by React's
	// rules-of-hooks lint; `notFound`/`redirect`/`cookies`/`headers`
	// match Next.js app-router primitives almost exclusively). Gated
	// by lang=="javascript"||lang=="typescript" in stdlibFunction above.
	// React core hooks (https://react.dev/reference/react/hooks)
	"useState":             {},
	"useEffect":            {},
	"useCallback":          {},
	"useMemo":              {},
	"useRef":               {},
	"useContext":           {},
	"useReducer":           {},
	"useLayoutEffect":      {},
	"useImperativeHandle":  {},
	"useDebugValue":        {},
	"useId":                {},
	"useTransition":        {},
	"useDeferredValue":     {},
	"useSyncExternalStore": {},
	"useInsertionEffect":   {},
	"useActionState":       {},
	"useOptimistic":        {},
	"useFormState":         {},
	"useFormStatus":        {},
	// Next.js app-router navigation + RSC primitives
	// (https://nextjs.org/docs/app/api-reference)
	"useRouter":                 {}, // next/navigation
	"usePathname":               {},
	"useSearchParams":           {},
	"useParams":                 {},
	"useSelectedLayoutSegment":  {},
	"useSelectedLayoutSegments": {},
	"useServerInsertedHTML":     {},
	"useReportWebVitals":        {},
	"notFound":                  {}, // next/navigation
	"redirect":                  {},
	"permanentRedirect":         {},
	"unauthorized":              {},
	"forbidden":                 {},
	"draftMode":                 {}, // next/headers
	"revalidatePath":            {}, // next/cache
	"revalidateTag":             {},
	"unstable_cache":            {},
	"unstable_noStore":          {},
	"unstable_after":            {},
	"unstable_expireTag":        {},
	"unstable_expirePath":       {},
	"cacheTag":                  {}, // next/cache (use cache directive)
	"cacheLife":                 {},
	"after":                     {}, // next/server unstable_after
	"NextResponse":              {}, // next/server (used bare after destructure)
	"NextRequest":               {},
	"ImageResponse":             {}, // next/og
	"getRequestContext":         {}, // @cloudflare/next-on-pages
	// React core APIs that show up bare after `import { ... } from 'react'`
	"createContext": {},
	"createElement": {},
	"cloneElement":  {},
	"createRef":     {},
	"forwardRef":    {},
	"memo":          {},
	// `lazy` is in kotlinBareNames (kotlin stdlib `by lazy {}`). Cross-
	// lang invariant test forbids duplication — drop the JS entry; the
	// React.lazy call site is uncommon enough to absorb.
	"Suspense":        {}, // component, sometimes used in JSX-as-call
	"Fragment":        {},
	"isValidElement":  {},
	"startTransition": {},
	"Children":        {},
	"Component":       {},
	"PureComponent":   {},
	// React-DOM client/server entry points
	"createRoot":             {},
	"hydrateRoot":            {},
	"createPortal":           {},
	"flushSync":              {},
	"renderToString":         {},
	"renderToStaticMarkup":   {},
	"renderToReadableStream": {},
	"renderToPipeableStream": {},
	// Next.js `cookies()` / `headers()` are receiver-stripped at call
	// sites like `(await cookies()).get(...)`. Both are exclusive to
	// next/headers in practice. `cookies`/`headers` are claimed by
	// swiftBareNames (vapor) and kotlinBareNames (ktor); cross-lang
	// invariant tests forbid duplication, so JS-side relies on the
	// per-language gate at the call site (Next.js TS files are tagged
	// "typescript", which routes through a different code path). The
	// scope:component:import:external:next/headers branch above already
	// routes the IMPORTS edge — only the bare CALL stays bug-extractor.
	// Date instance methods (receiver-stripped). The `Date.prototype`
	// surface is overwhelmingly used as `dateInstance.getFullYear()`,
	// rarely as a name on user objects.
	"getFullYear":        {},
	"getMonth":           {},
	"getDate":            {},
	"getDay":             {},
	"getHours":           {},
	"getMinutes":         {},
	"getSeconds":         {},
	"getMilliseconds":    {},
	"getTime":            {},
	"getTimezoneOffset":  {},
	"toISOString":        {},
	"toDateString":       {},
	"toTimeString":       {},
	"toLocaleDateString": {},
	"toLocaleTimeString": {},
	"toLocaleString":     {},
	// Intl constructors (imported as named exports of the global Intl)
	"NumberFormat":       {},
	"DateTimeFormat":     {},
	"Collator":           {},
	"RelativeTimeFormat": {},
	"PluralRules":        {},
	"ListFormat":         {},
	"DisplayNames":       {},
	"Segmenter":          {},
	// DOM event surface (receiver-stripped from `element.addEventListener(...)`,
	// `node.contains(other)`, `window.dispatchEvent(evt)`). Heavy in
	// React effect callbacks. Each name is a DOM API with no realistic
	// collision against a user-defined method named identically — the
	// pair (`addEventListener`/`removeEventListener`) is especially
	// distinctive.
	"addEventListener":    {},
	"removeEventListener": {},
	// `dispatchEvent` is gated to PHP (laravel/symfony) — cross-lang test
	// forbids duplication. Drop.
	"preventDefault":           {},
	"stopPropagation":          {},
	"stopImmediatePropagation": {},
	"requestAnimationFrame":    {},
	"cancelAnimationFrame":     {},
	"requestIdleCallback":      {},
	"cancelIdleCallback":       {},
	"matchMedia":               {},
	"getComputedStyle":         {},
	"getBoundingClientRect":    {},
	"scrollIntoView":           {},
	"querySelector":            {},
	"querySelectorAll":         {},
	"getElementById":           {},
	"getElementsByClassName":   {},
	"getElementsByTagName":     {},
	"createElementNS":          {}, // SVG DOM
	"createTextNode":           {},
	"appendChild":              {},
	"removeChild":              {},
	"replaceChild":             {},
	"insertBefore":             {},
	"setAttribute":             {},
	"getAttribute":             {},
	"removeAttribute":          {},
	"hasAttribute":             {},
	"setProperty":              {},
	"getPropertyValue":         {},
	"focus":                    {},
	"blur":                     {},
	"click":                    {},
	"submit":                   {},
	// Object.defineProperty is already common but receiver-stripped — keep.
	"defineProperty":           {},
	"defineProperties":         {},
	"getPrototypeOf":           {},
	"setPrototypeOf":           {},
	"getOwnPropertyNames":      {},
	"getOwnPropertyDescriptor": {},
	// `create` is on the explicit JS rejection list (#104) — do NOT add.
	// `bind`, `call`, `apply` — Function.prototype receiver-strips. Each
	// has wide use across React patterns. Collision risk acknowledged
	// but the `Function.prototype` semantics dominate.
	"bind": {},
	// `apply` is in kotlinBareNames (kotlin DSL). Cross-lang invariant
	// test forbids duplication. Skip — the receiver-strip volume in JS
	// is dominated by `bind`/`call` anyway.
	"call": {},
	// Misc Node / React 19 names safe to gate behind ts/js.
	"cwd":   {}, // process.cwd
	"chdir": {}, // process.chdir
	"toast": {}, // react-hot-toast / sonner top-level
	"use":   {}, // React 19 promise/context unwrap hook
	// `then` is in rubyBareNames; do NOT duplicate.
	// Wave-4 (express residual) — node:assert receiver-stripped names.
	// These appear bare after `import { strictEqual } from 'node:assert'`
	// or `assert.strictEqual(...)` receiver-strip. Distinctive enough
	// that collisions in the JS/TS gate are negligible.
	"strictEqual":        {},
	"notStrictEqual":     {},
	"deepEqual":          {},
	"notDeepEqual":       {},
	"deepStrictEqual":    {},
	"notDeepStrictEqual": {},
	"ifError":            {},
	"doesNotThrow":       {},
	"doesNotReject":      {},
	"doesNotMatch":       {},
	// `ok`, `equal`, `notEqual`, `throws`, `match`, `fail` deliberately
	// omitted — too collision-prone.
	// Node `path` module receiver-strip (`path.extname(file)` → `extname`).
	"extname": {},
	// `basename`/`dirname` are gated to python — cross-lang invariant
	// test forbids duplication. Drop.
	"relative":   {},
	"normalize":  {},
	"isAbsolute": {},
	// Node `http` ServerResponse / IncomingMessage receiver-strip.
	"setHeader":       {},
	"getHeader":       {},
	"getHeaderNames":  {},
	"getHeaders":      {},
	"removeHeader":    {},
	"hasHeader":       {},
	"writeHead":       {},
	"flushHeaders":    {},
	"writeContinue":   {},
	"writeEarlyHints": {},
	// Node `stream` / EventEmitter receiver-strip. `emit`/`once` are
	// very high frequency in node code; the JS/TS gate limits collisions.
	"pipe":                {},
	"unpipe":              {},
	"emit":                {},
	"once":                {},
	"removeListener":      {},
	"removeAllListeners":  {},
	"prependListener":     {},
	"prependOnceListener": {},
	"listenerCount":       {},
	"listeners":           {},
	"rawListeners":        {},
	"eventNames":          {},
	"setMaxListeners":     {},
	"getMaxListeners":     {},
	// Node `fs` receiver-strip (sync surface dominates in tooling).
	"readFile":      {},
	"readFileSync":  {},
	"writeFile":     {},
	"writeFileSync": {},
	"statSync":      {},
	"lstatSync":     {},
	"existsSync":    {},
	"mkdirSync":     {},
	"rmSync":        {},
	"rmdirSync":     {},
	"readdirSync":   {},
	"unlinkSync":    {},
	"copyFileSync":  {},
	"readlinkSync":  {},
	"realpathSync":  {},
	// dns.lookup / view-engine lookup / server.address. Each is a node
	// builtin receiver-strip.
	"lookup":  {},
	"address": {},
	// `on` is the EventEmitter `obj.on('event', cb)` receiver-strip.
	// Very high frequency in node + browser; ts/js gate scopes the
	// collision risk. Kept separate from the comment block above so the
	// rejection-list test can be amended if needed.
	"on": {},
	// `off` (modern EventEmitter alias for removeListener).
	"off": {},

	// Wave-4 (RN/Expo, #508) — bare-name hooks + receiver-stripped APIs
	// observed as the dominant bug-extractor residual on the client-fixture-c
	// (RN+Expo) fixture after pass-1 (knownExternalPackages
	// allowlist). All names are distinctive ecosystem APIs gated by
	// lang=="javascript"||lang=="typescript" in stdlibFunction. Each
	// follows the React convention (`use*` is reserved by rules-of-hooks
	// lint) or is a TanStack Query / Expo Router / React Navigation /
	// React Native core hook with vanishingly low collision risk vs a
	// hand-rolled user method.
	//
	// TanStack Query (@tanstack/react-query) — RN apps overwhelmingly use
	// this for data fetching. Receiver-stripped from `qc.invalidateQueries(...)`.
	"useQuery":                 {},
	"useQueries":               {},
	"useInfiniteQuery":         {},
	"useMutation":              {},
	"useIsFetching":            {},
	"useIsMutating":            {},
	"useQueryClient":           {},
	"useSuspenseQuery":         {},
	"useSuspenseQueries":       {},
	"useSuspenseInfiniteQuery": {},
	"invalidateQueries":        {},
	"prefetchQuery":            {},
	"prefetchInfiniteQuery":    {},
	"removeQueries":            {},
	"resetQueries":             {},
	"refetchQueries":           {},
	"cancelQueries":            {},
	"getQueryData":             {},
	"getQueriesData":           {},
	"getQueryState":            {},
	// `setQueryData` is sufficiently distinctive (the `QueryData` suffix
	// has no realistic user-method collision in TS code).
	"setQueryData":       {},
	"setQueriesData":     {},
	"ensureQueryData":    {},
	"fetchQuery":         {},
	"fetchInfiniteQuery": {},
	// React Navigation (`@react-navigation/native`) hooks. Each `use*` is
	// reserved by rules-of-hooks; non-navigation user hooks named
	// identically are essentially impossible.
	"useNavigation":             {},
	"useRoute":                  {},
	"useFocusEffect":            {},
	"useIsFocused":              {},
	"useNavigationState":        {},
	"useNavigationContainerRef": {},
	"useScrollToTop":            {},
	"usePreventRemove":          {},
	"useLinkBuilder":            {},
	"useLinkTo":                 {},
	"useLinkProps":              {},
	"useTheme":                  {}, // navigation theme — also legitimately used by @mui/@chakra; ts/js gate is fine
	// Expo Router hooks (`expo-router`). `useLocalSearchParams` /
	// `useGlobalSearchParams` are unique to Expo Router; `useSegments`
	// and `useRootNavigationState` are too.
	"useLocalSearchParams":   {},
	"useGlobalSearchParams":  {},
	"useSegments":            {},
	"useRootNavigationState": {},
	"useRootNavigation":      {},
	// React Native core hooks (from `react-native`). `useColorScheme` and
	// `useWindowDimensions` are RN-specific; `Appearance.getColorScheme()`
	// is the underlying API.
	"useColorScheme":      {},
	"useWindowDimensions": {},
	"useAnimatedValue":    {},
	// Reanimated v2/v3 hooks (`react-native-reanimated`).
	"useSharedValue":            {},
	"useAnimatedStyle":          {},
	"useAnimatedScrollHandler":  {},
	"useAnimatedGestureHandler": {},
	"useAnimatedReaction":       {},
	"useAnimatedRef":            {},
	"useAnimatedProps":          {},
	"useDerivedValue":           {},
	"useFrameCallback":          {},
	"useWorkletCallback":        {},
	"useScrollViewOffset":       {},
	"useReducedMotion":          {},
	"runOnUI":                   {},
	"runOnJS":                   {},
	"withTiming":                {},
	"withSpring":                {},
	"withDecay":                 {},
	"withDelay":                 {},
	"withRepeat":                {},
	"withSequence":              {},
	"interpolate":               {},
	"interpolateColor":          {},
	// Zustand store hooks. `useShallow` is the canonical selector helper.
	"useShallow": {},
	// React-Hook-Form / common form-state hooks.
	"useForm":        {},
	"useController":  {},
	"useFormContext": {},
	// `useFormState` already in jsBareNames (React 19 form-state hook); not duplicated.
	"useWatch":      {},
	"useFieldArray": {},
	// Other distinctive RN-ecosystem hooks observed in client-fixture-c.
	"useHeaderHeight":            {}, // @react-navigation/elements
	"useBottomTabBarHeight":      {}, // @react-navigation/bottom-tabs
	"useSafeAreaInsets":          {}, // react-native-safe-area-context
	"useSafeAreaFrame":           {},
	"useKeyboard":                {}, // @react-native-community/hooks
	"useAudioPlayer":             {}, // expo-audio
	"useAudioPlayerStatus":       {},
	"useAudioRecorder":           {},
	"useVideoPlayer":             {}, // expo-video
	"useEvent":                   {}, // expo modules event hook
	"useEventListener":           {},
	"usePermissions":             {}, // expo-camera / expo-media-library / etc.
	"useCameraPermissions":       {},
	"useMediaLibraryPermissions": {},
	"useLocationPermissions":     {},
	"useAssets":                  {}, // expo-asset
	"useFonts":                   {}, // expo-font
	"useUpdates":                 {}, // expo-updates
	"useBackHandler":             {}, // RN BackHandler hook helper
	"useDeviceOrientation":       {},
	"useAppState":                {},
	"useFocusable":               {},
	"useDimensions":              {},
	// TanStack Query / RTK / React common callback prop names. These are
	// JSX prop callbacks (`<Mutation onSuccess={...} onError={...} />`)
	// that the TS extractor receiver-strips. The TS/JS gate plus the
	// distinctive `on*` shape keeps collision risk low — they only ever
	// appear in framework call sites.
	"onSuccess":  {},
	"onError":    {},
	"onSettled":  {},
	"onMutate":   {},
	"onProgress": {}, // file upload / video player
	// `padStart`/`padEnd` — String.prototype receiver-strip.
	"padStart": {},
	"padEnd":   {},
	// TanStack Query / React Query lifecycle helper (mutation `mutate()`
	// and `mutateAsync()` are distinct enough to not collide).
	"mutate":      {},
	"mutateAsync": {},
	"refetch":     {}, // useQuery().refetch

	// Wave-4 (RN/Expo, #508) — pass-3 additions. Each is a distinctive
	// receiver-stripped API from React Navigation / RN core / Zustand /
	// Expo Router observed in the client-fixture-c pass-2 residual.
	// React Navigation `navigation.setOptions({...})` — distinctive name
	// for screen header config; no realistic user-method collision in TS.
	"setOptions": {},
	// RN Linking `Linking.openURL(url)`, `Linking.canOpenURL(url)`.
	"openURL":       {},
	"canOpenURL":    {},
	"getInitialURL": {},
	"sendIntent":    {}, // android intent
	"openSettings":  {}, // RN Linking / permissions
	// Zustand `useStore.getState()` / `useStore.setState(...)` / `.subscribe()`.
	// `getState`/`setState` are React-classic names too; the ts/js gate is
	// the safety net (React class `setState` is the most likely collision —
	// modern React is hooks-only so the false-positive rate is negligible).
	"getState": {},
	// `setState` is the React class-component method too — too collision-prone.
	// Skip — zustand `setState` residual is small.
	// `subscribe` is already in jsBareNames (gql subscribe). Not duplicated.
	// React Navigation `navigation.navigate(...)`, `.goBack()`, `.pop()`,
	// `.push()`, `.replace()` — generic verbs. Only `pop`/`replace` get
	// added (Array.prototype `pop` is JS builtin so already safe to gate;
	// `push` already in jsBareNames). `replace` is also String.prototype.
	"pop": {}, // Array.prototype + navigation.pop
	// `replace` is intentionally OMITTED — String.prototype.replace
	// dominates and many user objects have a `replace` method. The
	// extractor's receiver-strip ambiguity rules out a safe classification.
	// `useToast` — multiple toast libs (`react-native-toast-message`,
	// `@gluestack-ui/toast`, etc.); convention reserved by rules-of-hooks.
	"useToast":  {},
	"useAlert":  {},
	"useDialog": {},
	"useModal":  {},
	// `useAuth` / `useUser` — common auth-library hooks (Clerk, Supabase
	// Auth, expo-auth-session). User-app stores named `useAuthStore` are
	// NOT this — extractor sees the literal call site name. Skip the
	// generic `useAuth` since it collides with user-defined auth stores
	// in any codebase.
	// String.prototype `padStart`/`padEnd` already added above.
	// `gray` — chalk receiver-strip (`chalk.gray(...)`). chalk already
	// allowlisted at the package level; the bare call leaks through.
	"gray":      {},
	"red":       {},
	"green":     {},
	"yellow":    {},
	"blue":      {},
	"magenta":   {},
	"cyan":      {},
	"white":     {},
	"black":     {},
	"bold":      {},
	"dim":       {},
	"italic":    {},
	"underline": {},
	// String.prototype `localeCompare` / `startsWith` / `lastIndexOf` —
	// distinctive JS String API; receiver-stripped.
	"localeCompare": {},
	"startsWith":    {},
	"endsWith":      {},
	"lastIndexOf":   {},
	// `normalize` already in jsBareNames (Node path.normalize); not duplicated.
	// Number.prototype `toFixed` / `toPrecision` / `toExponential`.
	"toFixed":       {},
	"toPrecision":   {},
	"toExponential": {},

	// Wave-7 (TS/JS React frontend, #538) — react-redux named-import
	// surface (useSelector, useDispatch, useStore). State management
	// bare-name hooks/helpers. The JS extractor strips receivers for
	// `dispatch(action)`, `store.dispatch(...)`, `selector(state)`
	// patterns, and the hooks are bare callee names. Names are
	// distinctive enough that the js/ts gate prevents collision
	// (Python `connect`/`create` etc. are gated by lang).
	"useSelector":              {},
	"useDispatch":              {},
	"useStore":                 {},
	"useAppSelector":           {},
	"useAppDispatch":           {},
	"useAppStore":              {},
	"createSlice":              {},
	"createAsyncThunk":         {},
	"createApi":                {},
	"combineReducers":          {},
	"configureStore":           {},
	"createReducer":            {},
	"createAction":             {},
	"createEntityAdapter":      {},
	"createSelector":           {},
	"createListenerMiddleware": {},
	// Jotai
	"useAtom":         {},
	"useAtomValue":    {},
	"useSetAtom":      {},
	"useResetAtom":    {},
	"useAtomCallback": {},
	"atomWithStorage": {},
	"atomFamily":      {},
	"atomWithReducer": {},
	"selectAtom":      {},
	"loadable":        {},
	// Zustand additional surface (already have getState; add createStore)
	"createStore": {},
	// react-redux `Provider`/`connect` exist with too many collisions;
	// `connect` is intentionally omitted (HOC name collides with WS
	// connect/db connect). `Provider` is a JSX element, not a bare call.
	// Valtio
	"useSnapshot":  {},
	"proxy":        {},
	"snapshot":     {},
	"subscribeKey": {},
	// XState
	"useMachine":         {},
	"useActor":           {},
	"useInterpret":       {},
	"useSelector_xstate": {}, // disambiguator no-op key, harmless
	"createMachine":      {},
	"interpret":          {},
	// `assign` (xstate) already in jsBareNames; not duplicated.

	// Wave-7 — dayjs bare-name plugins (`dayjs(x).unix()`, `.isAfter(y)`,
	// `.isBefore(y)`, `.add(1, "day")`). dayjs is allowlisted as a pkg;
	// these are receiver-stripped method names. Distinctive enough.
	"unix":           {},
	"isAfter":        {},
	"isBefore":       {},
	"isSame":         {},
	"isSameOrAfter":  {},
	"isSameOrBefore": {},
	"isBetween":      {},
	"diff":           {},
	"fromNow":        {},
	"toNow":          {},
	"calendar":       {},
	"duration":       {},
	"humanize":       {},
	"weekday":        {},
	"quarter":        {},
	"isoWeek":        {},
	"isoWeekday":     {},
	"isoWeekYear":    {},
	"week":           {},
	"weekYear":       {},
	// Array.prototype `includes` is a JS builtin (ES2016); js/ts gate.
	"includes": {},
	// Map.prototype `has` / `get` / `set` / `delete` already exist in JS as
	// builtins. `add` is Set.prototype.add (also a generic verb). The
	// receiver-strip in the JS extractor surfaces it bare. js/ts gate.
	"add": {},

	// Wave-8 (#567 chain-fix B) — Array.prototype builtins that the JS
	// extractor receiver-strips (`xs.findIndex(p)` → `findIndex`,
	// `xs.indexOf(x)` → `indexOf`). Per #104's safer-bias rule these names
	// stayed off the JS list because they're collision-prone IF the gate
	// were language-agnostic. With the per-language `lang ==
	// "javascript" || "typescript"` gate, the namespace is sealed off
	// from non-JS user-defined methods of the same name (a Go/Ruby/Java
	// class with an `indexOf` method still resolves correctly because
	// the JS gate doesn't fire).
	//
	// Names already in OTHER language allowlists (`indexOf` in
	// kotlinBareNames, etc.) are still safe to add here — `stdlibFunction`
	// checks lang-specific maps independently and returns on first match,
	// so a Kotlin source still resolves `indexOf` via kotlinBareNames and
	// a JS source resolves it via jsBareNames.
	//
	// Rejected (kept off the list) per #104 — collision-prone enough
	// that the language gate alone isn't sufficient: `find`, `forEach`,
	// `reduce`, `map`, `filter`. The JS extractor still receiver-strips
	// these but they fall through to bug-extractor — the documented
	// acceptable trade-off vs masking real user methods named `find` /
	// `reduce` on JS classes.
	"findIndex":     {},
	"findLast":      {},
	"findLastIndex": {},
	"reduceRight":   {},
	"indexOf":       {},

	// Wave-8 (#567 chain-fix C, issue #539) — antd Form-instance
	// methods. The antd `Form.useForm()` hook returns a Form instance
	// whose API surface is a fixed set of method names. The JS
	// extractor receiver-strips `form.setFieldsValue(...)` →
	// `setFieldsValue`, `form.validateFields(...)` → `validateFields`,
	// etc. Each is distinctive enough that a user-defined non-Form
	// method colliding on the same name is rare (no generic verbs like
	// `submit` — that one would collide with too many user methods).
	// The `Fields`/`Field` suffix is a clear antd signal. Gated to
	// JS/TS via the lang switch above. Parent `antd` package is
	// already in knownExternalPackages so receiver-typed binding via
	// IMPORTS is not enough — the receiver is `form` (the hook
	// return) which loses its antd provenance after destructure.
	"setFieldsValue":    {},
	"getFieldsValue":    {},
	"setFieldValue":     {},
	"getFieldValue":     {},
	"validateFields":    {},
	"validateField":     {},
	"resetFields":       {},
	"scrollToField":     {},
	"getFieldError":     {},
	"getFieldsError":    {},
	"isFieldTouched":    {},
	"isFieldsTouched":   {},
	"isFieldValidating": {},

	// Wave-10 (#579 chain-fix A — client-fixture-b residual analysis).
	// Top bare-name extractor residuals after wave-9 lift. Each name
	// below is a real exported function from a well-known JS/TS
	// package whose collision against a user-defined method named
	// identically is vanishingly rare. Gated to JS/TS via lang
	// switch above.
	//
	// Issue #536 — AWS Amplify v6 auth. `fetchAuthSession` is the
	// dominant residual (372 rows in cfb diagnostic). Distinctive verb +
	// `AuthSession` suffix has no plausible collision. Full method set
	// covers sign-up/sign-in/reset flows and user attribute management.
	"fetchAuthSession":     {},
	"fetchUserAttributes":  {},
	"getCurrentUser":       {},
	"signIn":               {},
	"signOut":              {},
	"signUp":               {},
	"confirmSignIn":        {},
	"confirmSignUp":        {},
	"resendSignUpCode":     {},
	"resetPassword":        {},
	"confirmResetPassword": {},
	"updatePassword":       {},
	// React Router v6 hooks — `useNavigate` returns a `navigate`
	// function that's bound to a const inside the component scope;
	// the hook-call itself bare-extracts but the import binds it.
	// `use*` reserved by rules-of-hooks.
	"useNavigate":         {},
	"useNavigationType":   {},
	"useResolvedPath":     {},
	"useMatch":            {},
	"useMatches":          {},
	"useOutlet":           {},
	"useOutletContext":    {},
	"useRoutes":           {},
	"useLinkClickHandler": {},
	"useLinkPressHandler": {},
	"useHref":             {},
	"useInRouterContext":  {},
	"useBeforeUnload":     {},
	// Browser URL static methods — `URL.createObjectURL(blob)` and
	// `URL.revokeObjectURL(url)` are receiver-stripped from the URL
	// global. No user-defined method overlap in practice.
	"createObjectURL": {},
	"revokeObjectURL": {},
	// antd `theme.useToken()` hook — `useToken` is the canonical
	// antd theme accessor. Reserved by rules-of-hooks naming.
	"useToken": {},
	// Mantine / antd / @emotion style helpers. `createStyles` is
	// the Mantine v6 / @mantine/styles canonical entry.
	"createStyles": {},
	// uuid v4 named-export shorthand commonly imported as `v4 as uuidv4`.
	"uuidv4": {},
	"uuidv1": {},
	"uuidv5": {},
	// dayjs / moment receiver-strip — `dayjs().startOf('day')` →
	// `startOf`, `.endOf`, `.utc`, `.extend(plugin)`.
	// `year`/`month`/`day` getters deliberately OMITTED — too
	// collision-prone with user model fields (e.g. inspection.year).
	// `extend` collides with no other gate.
	"startOf": {},
	"endOf":   {},
	"utc":     {},
	"extend":  {},
	// React Hook Form / RTK Query / formik destructured helpers.
	// `useFieldArray`/`useController`/`useWatch`/`useFormContext`
	// already in jsBareNames above (RHF block).
	// `unwrap` is gated to lang="rust" via cross-lang invariant test
	// (TestRustBareNames_NotClassifiedForOtherLanguages) — cannot
	// classify it here. Leave as bug-extractor in JS/TS.
	// antd Modal.confirm / window.confirm. `confirm` collides with
	// the global builtin AND antd; either way it's external.
	// `success`/`info` deliberately OMITTED — would shadow user
	// callbacks named identically. `warning` is in swiftBareNames
	// (cross-lang invariant test forbids duplication).
	"confirm": {},

	// Wave-10 chain-fix B — pass-2 additions after pass-1 measure
	// (cfb 4.49% → 3.25%). Top remaining safe residuals: more
	// react-router hooks + antd Form hooks + dayjs typeguard +
	// FileReader API + DOM closest.
	"useLocation":        {}, // react-router hook (rules-of-hooks)
	"useFormInstance":    {}, // antd Form instance hook
	"isDayjs":            {}, // dayjs typeguard, distinctive name
	"readAsDataURL":      {}, // FileReader prototype
	"readAsText":         {}, // FileReader prototype
	"readAsArrayBuffer":  {},
	"readAsBinaryString": {},
	// `closest` is in jqueryBareNames but jquery is file-gated.
	// In React component code `element.closest('.x')` is the DOM
	// Element.closest API — heavy in event-handler code. Cross-
	// lang invariant test will reject if jqueryBareNames cross-
	// gates — it doesn't (jquery is file-gated, not lang-gated).
	"closest": {},

	// Wave-11 (ship-gate, chain-fix B partial) — antd notification +
	// message + Modal hook returners. The antd App-context API
	// (`const [api, contextHolder] = message.useMessage()` /
	// `notification.useNotification()` / `Modal.useModal()`) returns
	// a callable + a contextHolder. The hook itself bare-extracts
	// from the receiver-strip (`message.useMessage` → `useMessage`).
	// `useMessage` / `useNotification` are rules-of-hooks reserved
	// (must start with `use`) and antd-distinctive enough to be
	// safe. `useApp` is the antd v5 App component hook returning
	// `{message, notification, modal}` accessors. The per-language
	// gate (js/ts only) prevents shadowing in non-JS codebases.
	// `useModal` already in jsBareNames above (antd).
	"useMessage":      {}, // antd message.useMessage hook
	"useNotification": {}, // antd notification.useNotification hook
	"useApp":          {}, // antd App.useApp hook

	// Wave-12 (ship-gate FINAL, client-fixture-b residual 1.154% → ≤1%).
	// String.prototype receiver-strip names. The JS extractor strips the
	// receiver on dotted calls (`str.replace(...)` → bare `replace`) and
	// the resolver can't bind to a String prototype. These are the names
	// that dominate `top_bug_ext` on client-fixture-b wave-11 residual.
	// Per-language gate (js/ts only) keeps them out of Python `str.replace`
	// / Go `strings.Replace` / Java `String.replace` collisions. Bare leaf
	// identifiers are unique enough within JS/TS that the safer-bias rule
	// (#94) doesn't block them — they have no plausible user-method form
	// at this scale on hand-rolled classes in a React/Node codebase.
	// `trim`, `toLowerCase`, `toUpperCase`, `padStart`, `padEnd`,
	// `normalize`, `localeCompare` already present above.
	"replace":    {}, // String.prototype.replace (top bug-ext residual)
	"replaceAll": {}, // String.prototype.replaceAll (ES2021)
	"trimStart":  {}, // String.prototype.trimStart
	"trimEnd":    {}, // String.prototype.trimEnd
	"repeat":     {}, // String.prototype.repeat
	"matchAll":   {}, // String.prototype.matchAll (ES2020)

	// Wave-12 — antd Modal / message / notification STATIC method names.
	// The antd App-static API (`Modal.confirm({...})`, `message.success(...)`,
	// `notification.error(...)`) is the canonical imperative API across
	// every antd-based React admin (client-fixture-b is exactly this shape).
	// The receiver-strip drops `Modal.` / `message.` / `notification.` and
	// leaves the bare method name. `warning` is the top-2 bug-extractor
	// residual on client-fixture-b. `confirm`, `info`, `error` already in
	// jsBareNames above (DOM `window.confirm`, `console.info`,
	// `console.error`); `success`, `warning`, `loading`, `destroy`,
	// `destroyAll`, `open` are added here. Per-language gate (js/ts) keeps
	// these out of other-lang collisions.
	"warning":    {}, // antd Modal.warning / message.warning / notification.warning
	"success":    {}, // antd Modal.success / message.success / notification.success
	"loading":    {}, // antd message.loading
	"destroyAll": {}, // antd Modal.destroyAll

	// Wave-12 — antd Table / Form receiver-stripped callback method names.
	// `clearFilters` is a top bug-extractor residual on client-fixture-b
	// (antd Table column `filterDropdown` provides `clearFilters` /
	// `confirm` / `setSelectedKeys` as render-prop arg methods). The
	// receiver-strip loses the binding. Names are antd-distinctive and the
	// per-language gate (js/ts only) keeps them safe.
	"clearFilters":    {}, // antd Table filterDropdown render-prop arg
	"setSelectedKeys": {}, // antd Table filterDropdown render-prop arg

	// Wave-13 (ts-w13 react real-residue, client-fixture-b 0.875% → lower).
	// All TS/JS-gated. Each name is a real exported function from a
	// well-known React-ecosystem package whose collision with a hand-rolled
	// user method named identically is vanishingly rare. Empirical residue
	// from cfb diagnostic samples (n=100 bug-extractor leaves).
	//
	// @dnd-kit drag-and-drop ecosystem (https://docs.dndkit.com/).
	// `useSortable`/`useDraggable`/`useDroppable`/`useDndContext`/
	// `useDndMonitor` are rules-of-hooks reserved. `closestCenter` /
	// `closestCorners` / `rectIntersection` / `pointerWithin` are
	// canonical collision-detection strategies. `arrayMove` is the
	// canonical sortable helper. `restrictTo*` are @dnd-kit/modifiers
	// exports. All names are @dnd-kit-distinctive.
	"useSortable":                       {},
	"useDraggable":                      {},
	"useDroppable":                      {},
	"useDndContext":                     {},
	"useDndMonitor":                     {},
	"closestCenter":                     {},
	"closestCorners":                    {},
	"rectIntersection":                  {},
	"pointerWithin":                     {},
	"arrayMove":                         {},
	"defaultDropAnimationSideEffects":   {},
	"restrictToWindowEdges":             {},
	"restrictToVerticalAxis":            {},
	"restrictToHorizontalAxis":          {},
	"restrictToFirstScrollableAncestor": {},
	"restrictToParentElement":           {},

	// React Router DOM v6 advanced hooks
	// (https://reactrouter.com/en/main/start/overview). Rules-of-hooks
	// reserved (`use*`). `useNavigation`/`useFormState` already present
	// elsewhere; not duplicated.
	"useRouteError":          {},
	"useRouteLoaderData":     {},
	"useRevalidator":         {},
	"useBlocker":             {},
	"useFormAction":          {},
	"useFetcher":             {},
	"useFetchers":            {},
	"useViewTransitionState": {},
	"useSubmit":              {},
	"useAsyncValue":          {},
	"useAsyncError":          {},

	// antd Grid responsive hook (Grid.useBreakpoint). antd-distinctive
	// `useBreakpoint` is also used by chakra-ui under the same name —
	// both are JS/TS-only.
	"useBreakpoint": {},

	// SheetJS (xlsx) / ExcelJS receiver-strip surface
	// (https://docs.sheetjs.com/). The xlsx pkg is allowlisted but
	// `XLSX.utils.sheet_to_json(...)` strips both receivers leaving the
	// bare snake_case name. The `_` separator and snake_case style is
	// xlsx-distinctive (no user JS class uses snake_case methods at any
	// scale).
	"sheet_to_json":          {},
	"sheet_add_json":         {},
	"sheet_add_aoa":          {},
	"aoa_to_sheet":           {},
	"json_to_sheet":          {},
	"book_new":               {},
	"book_append_sheet":      {},
	"book_set_sheet_visible": {},
	"decode_range":           {},
	"encode_range":           {},
	"decode_cell":            {},
	"encode_cell":            {},

	// Clipboard API (https://developer.mozilla.org/en-US/docs/Web/API/Clipboard_API).
	// `navigator.clipboard.writeText(...)` / `.readText(...)` strip both
	// receivers. `ClipboardItem` is a distinctive constructor name.
	// `writeText` already lives in kotlinBareNames (kotlin.io.File.writeText) —
	// the kotlin gate fires for kotlin sources; the JS gate fires for js/ts.
	// Both are language-scoped so they don't collide.
	"ClipboardItem": {},
	// `writeText` intentionally NOT added here (kotlinBareNames covers
	// it for kotlin; for JS we accept the small residual since
	// `writeText` is also used by file-stream user methods more often).
	// `readText` already present elsewhere (FileReader chain). Don't dup.

	// styled-components / emotion (https://styled-components.com/docs).
	// `styled`, `css`, `keyframes`, `createGlobalStyle` are top-level
	// exports commonly called bare after destructuring. `ThemeProvider`
	// is a component name (JSX) but appears as a bare call-target in
	// some HOC patterns. `styled` is intentionally OMITTED — it
	// collides with user variable names too readily ("const styled =
	// require(...)" patterns). The other three are distinctive.
	"keyframes":         {},
	"createGlobalStyle": {},
	// `css` is intentionally OMITTED — too short / collision-prone with
	// CSS-prop variables. `ThemeProvider` is a JSX component; not a
	// bare-call target in well-typed code.

	// date-fns (https://date-fns.org/) — the canonical JS date library.
	// All names are date-fns-distinctive: the `differenceIn*` /
	// `startOf*` / `endOf*` / `addX` / `subX` / `parseISO` / `isSame*`
	// surface has no user-method collision pattern. `parseISO` is
	// distinctive. `addDays`/`subDays` are camelCase verb+noun.
	"parseISO":                 {},
	"isSameDay":                {},
	"isSameMonth":              {},
	"isSameYear":               {},
	"isSameWeek":               {},
	"isSameHour":               {},
	"isSameMinute":             {},
	"isSameSecond":             {},
	"isWithinInterval":         {},
	"differenceInDays":         {},
	"differenceInMonths":       {},
	"differenceInYears":        {},
	"differenceInWeeks":        {},
	"differenceInHours":        {},
	"differenceInMinutes":      {},
	"differenceInSeconds":      {},
	"differenceInMilliseconds": {},
	"differenceInCalendarDays": {},
	"addDays":                  {},
	"addHours":                 {},
	"addMinutes":               {},
	"addSeconds":               {},
	"addWeeks":                 {},
	"addMonths":                {},
	"addYears":                 {},
	"subDays":                  {},
	"subHours":                 {},
	"subMinutes":               {},
	"subSeconds":               {},
	"subWeeks":                 {},
	"subMonths":                {},
	"subYears":                 {},
	"startOfMonth":             {},
	"endOfMonth":               {},
	"startOfWeek":              {},
	"endOfWeek":                {},
	"startOfYear":              {},
	"endOfYear":                {},
	"startOfDay":               {},
	"endOfDay":                 {},
	"startOfHour":              {},
	"endOfHour":                {},
	"startOfMinute":            {},
	"endOfMinute":              {},
	"startOfQuarter":           {},
	"endOfQuarter":             {},
	"formatDistance":           {},
	"formatDistanceToNow":      {},
	"formatRelative":           {},
	"formatISO":                {},

	// FileReader / DOMParser API (https://developer.mozilla.org/en-US/docs/Web/API/FileReader).
	// `readAsDataURL`/`readAsText`/`readAsArrayBuffer`/`readAsBinaryString`
	// already present above. `FileReader` constructor is bare-extracted
	// when used as `new FileReader()` — captures the constructor leaf.
	// `parseFromString` is DOMParser-distinctive
	// (https://developer.mozilla.org/en-US/docs/Web/API/DOMParser).
	"FileReader":      {},
	"parseFromString": {},

	// React error/suspense boundaries (react-error-boundary npm pkg —
	// canonical community library). `useErrorBoundary` is rules-of-hooks
	// reserved. `ErrorBoundary` is JSX-component but bare-extracted in
	// some HOC patterns.
	"useErrorBoundary": {},
	// `ErrorBoundary` and `Suspense` are JSX-only; not bare-call targets.

	// React 18 hook alias commonly used: `useReactEffect` (re-export
	// from internal effect hooks). Distinctive `useReact` prefix.
	"useReactEffect": {},

	// Wave-13 pass-3 — additional web/DOM observer + API residuals from
	// cfb post-pass-2 sampling. All TS/JS-gated, all distinctive web-
	// platform names.
	// MutationObserver / ResizeObserver / IntersectionObserver / PerformanceObserver:
	// `new ResizeObserver(cb)` constructor is bare-extracted; `.observe()` /
	// `.disconnect()` strip the receiver. Distinctive in JS/TS code.
	"ResizeObserver":       {},
	"MutationObserver":     {},
	"IntersectionObserver": {},
	"PerformanceObserver":  {},
	"observe":              {},
	"disconnect":           {},
	"unobserve":            {},
	"takeRecords":          {},
	// DOMParser API. `parseFromString` already added above.
	"DOMParser":         {},
	"XMLSerializer":     {},
	"serializeToString": {},
	// String.prototype additions — `charAt` is distinctive (single
	// String.prototype method, no user-class collision pattern at scale
	// in React/Node codebases). `subtract` is dayjs's symmetric counterpart
	// to `add` (already in jsBareNames). `toDate` is dayjs `.toDate()`
	// returning a native Date.
	"charAt":   {},
	"subtract": {}, // dayjs(x).subtract(1, 'day')
	"toDate":   {}, // dayjs(x).toDate()
	// Blob / Response body methods. `arrayBuffer` is distinctive (Blob,
	// Response, Request all expose it). Not user-overridden in practice.
	"arrayBuffer": {},
	// Web Storage. `getItem` / `setItem` / `removeItem` on
	// localStorage / sessionStorage. `setItem` would collide with React
	// state setters too — gated by setX pattern already. Just add
	// `getItem` and `removeItem` which are distinctive Storage API names.
	"getItem":    {},
	"removeItem": {},
	// Window.scrollTo and Element.scrollTo. Distinctive enough.
	"scrollTo": {},
	"scrollBy": {},
	// `scrollIntoView` already present earlier in jsBareNames.
	// RegExp.prototype.exec. Distinctive — no user-method collision.
	"exec": {},
	// DOMPurify.sanitize. DOMPurify pkg is allowlisted but the call site
	// is receiver-stripped (`DOMPurify.sanitize(html)` → `sanitize`).
	"sanitize": {},

	// Wave-13 pass-4 — Date.prototype UTC accessors + Intl
	// formatToParts + String.prototype.substring. All distinctive
	// JS-platform names; no user-class collision pattern at scale.
	"getUTCDate":         {},
	"getUTCMonth":        {},
	"getUTCFullYear":     {},
	"getUTCHours":        {},
	"getUTCMinutes":      {},
	"getUTCSeconds":      {},
	"getUTCDay":          {},
	"getUTCMilliseconds": {},
	"setUTCDate":         {},
	"setUTCMonth":        {},
	"setUTCFullYear":     {},
	"setUTCHours":        {},
	"setUTCMinutes":      {},
	"setUTCSeconds":      {},
	"setUTCMilliseconds": {},
	"toUTCString":        {},
	// `toISOString` / `toDateString` / `toTimeString` /
	// `toLocaleDateString` / `toLocaleTimeString` already present
	// earlier in jsBareNames; not duplicated here.
	"formatToParts": {}, // Intl.NumberFormat / Intl.DateTimeFormat
	"substring":     {}, // String.prototype.substring (substr is deprecated)

	// Wave-14 (issue #44 fixture-e 1.29% → ≤0.8%) — DOM + JS built-in
	// methods unresolved in fixture-e bug-extractor. Per #44 research, names
	// like `find`, `append`, `splice`, `write` are receiver-stripped DOM/
	// built-in calls with no entity binding. Gated to js/ts, preventing
	// collisions with other-language user methods. Vanishingly low collision
	// risk in React/Node codebases.
	"splice":          {}, // Array.prototype.splice
	"slice":           {}, // Array.prototype.slice / String.prototype.slice
	"concat":          {}, // Array.prototype.concat / String.prototype.concat
	"join":            {}, // Array.prototype.join
	"charCodeAt":      {}, // String.prototype.charCodeAt
	"match":           {}, // String.prototype.match
	"search":          {}, // String.prototype.search
	"split":           {}, // String.prototype.split
	"cloneNode":       {}, // Node.cloneNode
	"contains":        {}, // Node/Element.contains
	"matches":         {}, // Element.matches
	"scroll":          {}, // Window.scroll
	"countReset":      {}, // console.countReset
	"group":           {}, // console.group
	"groupEnd":        {}, // console.groupEnd
	"groupCollapsed":  {}, // console.groupCollapsed
	"time":            {}, // console.time
	"timeEnd":         {}, // console.timeEnd
	"timeLog":         {}, // console.timeLog
	"trunc":           {}, // Math.trunc
	"sign":            {}, // Math.sign
	"cbrt":            {}, // Math.cbrt
	"hypot":           {}, // Math.hypot
	"imul":            {}, // Math.imul
	"fround":          {}, // Math.fround
	"clz32":           {}, // Math.clz32
	"EPSILON":         {}, // Number.EPSILON
	"setFullYear":     {}, // Date.prototype.setFullYear
	"setMonth":        {}, // Date.prototype.setMonth
	"setDate":         {}, // Date.prototype.setDate
	"setHours":        {}, // Date.prototype.setHours
	"setMinutes":      {}, // Date.prototype.setMinutes
	"setSeconds":      {}, // Date.prototype.setSeconds
	"setMilliseconds": {}, // Date.prototype.setMilliseconds
	"setTime":         {}, // Date.prototype.setTime
	"formData":        {}, // Response.formData
	"setItem":         {}, // Storage.setItem
	"key":             {}, // Storage.key
	"reset":           {}, // HTMLFormElement.reset
	"checkValidity":   {}, // HTMLFormElement.checkValidity
	"reportValidity":  {}, // HTMLFormElement.reportValidity
	"getAll":          {}, // URLSearchParams.getAll
	"stack":           {}, // Error.stack
	"cause":           {}, // Error.cause
	"resolvedOptions": {}, // Intl.*.resolvedOptions
}

// swiftBareNames is the Swift-language-gated bare-name stop-list (issue
// #436). The Swift extractor strips the receiver from a Vapor / Fluent
// DSL call (`app.get("/x") { req in ... }` → `get`,
// `User.query(on: db).filter(...).all()` → `query`/`filter`/`all`,
// `req.parameters.get("id")` → `parameters`), and the resolver can't
// bind the bare leaf to a local entity, so it lands in bug-extractor.
//
// Mirrors the Kotlin Ktor DSL precedent (issue #435 / kotlinBareNames):
// the language gate (lang == "swift") is the safety net that prevents
// generic verbs like `get`/`post`/`save`/`update`/`delete`/`map`/`first`
// from shadowing user-defined methods in Go/JS/Python/Ruby/Kotlin
// codebases. Vapor- and Fluent-specific names dominate the residual
// bug-extractor in vapor (27.41%) and vapor-api-template (30.85%).
//
// Conservative selection (lessons from #94 / #105 / #106): names already
// classified by the language-agnostic stdlibBareNames map (`all`,
// `filter`, `map`, `set`, `range`, `join`, `Response`) are NOT
// duplicated here — they classify globally before the swift gate fires.
//
// Categories:
//   - Vapor route builder DSL (`get`, `post`, `put`, `patch`, `delete`,
//     `on`, `group`, `grouped`, `route`, `register`, `boot`, `run`,
//     `start`, `shutdown`, `respond`, `redirect`, `view`, `render`).
//   - Vapor middleware DSL (`middleware`, `use`, `authenticate`,
//     `authorize`, `protect`).
//   - Fluent ORM builders (`save`, `delete`, `create`, `update`, `find`,
//     `query`, `sort`, `limit`, `offset`, `with`, `count`, `first`,
//     `last`, `paginate`, `transform`, `flatMap`).
//   - HTTP context accessors (`parameters`, `query` — same name reused,
//     `headers`, `body`, `request`, `response`, `auth`, `session`,
//     `cookies`).
//   - Swift Concurrency primitives (`async`, `await`, `Task`,
//     `withCheckedContinuation`).
var swiftBareNames = map[string]struct{}{
	// Vapor route builder DSL.
	"get":      {},
	"post":     {},
	"put":      {},
	"patch":    {},
	"delete":   {},
	"on":       {},
	"group":    {},
	"grouped":  {},
	"route":    {},
	"register": {},
	"boot":     {},
	"run":      {},
	"start":    {},
	"shutdown": {},
	"respond":  {},
	"redirect": {},
	"view":     {},
	"render":   {},

	// Vapor middleware DSL. `use` is a known cross-language collision
	// (Rust prelude, JS Express); the lang=="swift" gate is the safety
	// net per the Ktor precedent.
	"middleware":   {},
	"use":          {},
	"authenticate": {},
	"authorize":    {},
	"protect":      {},

	// Fluent ORM query / persistence builders. Names like `save`,
	// `update`, `delete`, `find`, `first`, `last`, `count` collide with
	// generic ORM verbs in any language — safe only because of the
	// swift gate.
	"save":      {},
	"create":    {},
	"update":    {},
	"find":      {},
	"query":     {},
	"sort":      {},
	"limit":     {},
	"offset":    {},
	"with":      {},
	"count":     {},
	"first":     {},
	"last":      {},
	"paginate":  {},
	"transform": {},
	"flatMap":   {},

	// HTTP context accessors (Request / Response / ApplicationCall-
	// equivalent). `parameters`/`headers`/`request` mirror the Ktor
	// (#435) additions for the Kotlin gate; the swift gate keeps them
	// from leaking across languages.
	"parameters": {},
	"headers":    {},
	"body":       {},
	"request":    {},
	"response":   {},
	"auth":       {},
	"session":    {},
	"cookies":    {},

	// Swift Concurrency. `Task` is a Swift stdlib type; `async`/`await`
	// are language keywords but show up as bare-name calls when the
	// extractor receiver-strips a coroutine-style API.
	"async":                   {},
	"await":                   {},
	"Task":                    {},
	"withCheckedContinuation": {},

	// SwiftNIO EventLoopFuture / EventLoopPromise / NIOLockedValueBox
	// API. Vapor is built on SwiftNIO, and the dominant residual
	// bug-extractor in the vapor framework source after the Vapor /
	// Fluent additions above is the NIO Future API
	// (`eventLoop.makePromise()`, `future.whenComplete { ... }`,
	// `box.withLockedValue { ... }`). These names are Swift-only
	// camelCase idioms with no plausible collision in other ecosystems,
	// gated by lang=="swift" as defence-in-depth. Generic verbs
	// (`succeed`, `fail`, `wait`) are deliberately OMITTED — they
	// collide with user methods even within Swift codebases.
	"makeSucceededFuture": {},
	"makeFailedFuture":    {},
	"makePromise":         {},
	"makeFutureWithTask":  {},
	"completeWithTask":    {},
	"whenComplete":        {},
	"whenSuccess":         {},
	"whenFailure":         {},
	"flatSubmit":          {},
	"withLockedValue":     {},

	// Swift stdlib types and Sequence/Collection protocol methods.
	// The Swift extractor receiver-strips collection idioms
	// (`names.forEach { ... }` → `forEach`, `parts.joined(separator:)` →
	// `joined`, `bytes.dropFirst(2)` → `dropFirst`) and `init(...)`
	// constructor calls. These are language-builtin operations with no
	// plausible collision in non-Swift codebases; the swift gate adds
	// defence-in-depth. Generic accessors (`get`/`set`/`add`/`remove`/
	// `count`) are kept out of this group — `count` is already in
	// swiftBareNames above as a Fluent ORM verb, and the rest are
	// excluded per the #94 / #106 conservative-selection rule.
	"String":                  {},
	"Int":                     {},
	"Array":                   {},
	"Date":                    {},
	"ObjectIdentifier":        {},
	"forEach":                 {},
	"joined":                  {},
	"dropFirst":               {},
	"prefix":                  {},
	"numericCast":             {},
	"singleValueContainer":    {},
	"preconditionFailure":     {},
	"preconditionInEventLoop": {},
	"syncShutdownGracefully":  {},

	// swift-log Logger API. Vapor / SwiftNIO log via swift-log's
	// `Logger` type, and the extractor receiver-strips
	// (`req.logger.debug("...")` → `debug`, `logger.notice(...)` →
	// `notice`). Generic names like `error` would shadow user methods
	// in other ecosystems, but the swift gate keeps these scoped. Within
	// Swift codebases these names are dominantly Logger calls.
	"debug":    {},
	"info":     {},
	"trace":    {},
	"notice":   {},
	"warning":  {},
	"critical": {},

	// More Swift stdlib / Foundation idioms (Sequence, String,
	// Date/Calendar, integer types) and SwiftNIO Future helpers seen at
	// volume in the vapor framework source. Each is a Swift-specific
	// camelCase or PascalCase name; the swift gate prevents bleed into
	// other ecosystems.
	"hasSuffix":            {},
	"hasPrefix":            {},
	"lowercased":           {},
	"uppercased":           {},
	"replacingOccurrences": {},
	"dropLast":             {},
	"addingTimeInterval":   {},
	"merging":              {},
	"flatMapThrowing":      {},
	"makeCompletedFuture":  {},
	"precondition":         {},
	"fatalError":           {},
	"TimeZone":             {},
	"Locale":               {},
	"DateFormatter":        {},
	"Int64":                {},
	"UInt8":                {},
	"UInt16":               {},
	"UInt32":               {},
	"UInt64":               {},
	"Int8":                 {},
	"Int16":                {},
	"Int32":                {},

	// Vapor 3 / Fluent 3 service-graph types, factory/builder verbs, and
	// XCTest assertion DSL (Wave 3, vapor-api-template residuals — issue
	// #436 follow-up). The Swift extractor strips the receiver from
	// constructor calls (`MiddlewareConfig()` → `MiddlewareConfig`,
	// `SQLiteDatabase(storage: .memory)` → `SQLiteDatabase`) and from
	// static factory calls (`Config.default()` → `default`,
	// `Services.default()` → `default`). The receiver-less bare CALL
	// lands in bug-extractor with no plausible local candidate.
	//
	// Two name classes:
	//   1. PascalCase Vapor framework types (Application, MigrationConfig,
	//      DatabasesConfig, SQLiteDatabase, MiddlewareConfig, EngineRouter,
	//      ErrorMiddleware, FileMiddleware) — high specificity, very low
	//      collision risk even in the Vapor framework source repo.
	//   2. Lowercase Fluent/service-graph verbs (`add`, `default`) —
	//      generic on their own, but the synthesizer ONLY runs against
	//      UNRESOLVED endpoints (`Synthesize` skips hex-resolved IDs at
	//      synth.go:133). When `add`/`default` bind to a local Swift
	//      entity they keep their hex ID and never reach
	//      `swiftBareNames`. The stop-list only catches the receiver-
	//      stripped-and-unresolved residue — which IS the Fluent
	//      collection mutator (`databases.add(database:as:)`,
	//      `migrations.add(model:database:)`) or static factory
	//      (`Config.default()`, `Services.default()`) by construction.
	//      `register` is already covered by the upstream Swift stdlib
	//      stop-list. Swift gate prevents cross-language leakage.
	//
	// XCTest assertion macros (`XCTAssert*`, `XCTFail`) are Apple stdlib
	// with the `XCT` prefix; the Swift gate is defence-in-depth.
	"Application":          {},
	"MigrationConfig":      {},
	"DatabasesConfig":      {},
	"SQLiteDatabase":       {},
	"MiddlewareConfig":     {},
	"EngineRouter":         {},
	"ErrorMiddleware":      {},
	"FileMiddleware":       {},
	"add":                  {},
	"default":              {},
	"XCTAssert":            {},
	"XCTAssertEqual":       {},
	"XCTAssertNotEqual":    {},
	"XCTAssertTrue":        {},
	"XCTAssertFalse":       {},
	"XCTAssertNil":         {},
	"XCTAssertNotNil":      {},
	"XCTAssertThrowsError": {},
	"XCTAssertNoThrow":     {},
	"XCTAssertGreaterThan": {},
	"XCTAssertLessThan":    {},
	"XCTFail":              {},

	// Wave 4 — vapor framework source residual (14.27% bug-rate).
	// SwiftNIO Channel / ChannelHandler / ByteBuffer API surface, plus
	// Foundation Codable / Date / String idioms that show up at volume
	// in the Vapor framework source. Each is a Swift-specific
	// (PascalCase NIO type or NIO-/Foundation-style camelCase method)
	// name with no plausible collision in non-Swift codebases. The
	// swift gate prevents cross-language leakage; generic verbs
	// (`read`, `write`, `wait`, `succeed`, `fail`, `defer`, `init`,
	// `closure`, `error`, `data`, `current`, `callback`, `parse`,
	// `merge`, `mapping`, `key`, `cache`, `sessions`, `validate`,
	// `custom`, `serialize`, `unlock`, `cancel`) are deliberately
	// OMITTED per the #94 / #105 / #106 safer-bias rule — they shadow
	// user methods even within Swift codebases.
	//
	// PascalCase NIO / Foundation types — receiver-stripped from
	// `let bound = NIOLoopBound(value, eventLoop: ...)` or used as
	// type-checker references at call sites.
	"NIOLoopBound":               {},
	"NIOFileHandle":              {},
	"NIOSSL":                     {},
	"NIOHTTPRequestDecompressor": {},
	"NIOCloseOnErrorHandler":     {},
	"JSONDecoder":                {},
	"JSONEncoder":                {},
	"ISO8601DateFormatter":       {},
	"TimeInterval":               {},
	"CancellationError":          {},

	// SwiftNIO Channel / ChannelHandler camelCase API verbs —
	// `context.fireChannelRead(wrappedData)`, `channel.writeAndFlush(...)`,
	// `pipeline.addHandler(...)`, `context.fireUserInboundEventTriggered(...)`,
	// `self.unwrapInboundIn(data)`. Receiver-stripped by the extractor
	// and unbindable to a local entity. Names are SwiftNIO-distinctive
	// (camelCase ending in `Out`/`In`/`Handler`/`Bytes`/`Index`/
	// `Active`) — extremely unlikely to collide with user methods.
	"fireChannelRead":                  {},
	"fireUserInboundEventTriggered":    {},
	"wrapOutboundOut":                  {},
	"unwrapInboundIn":                  {},
	"unwrapOutboundIn":                 {},
	"writeAndFlush":                    {},
	"addHandler":                       {},
	"addHandlers":                      {},
	"shutdownGracefully":               {},
	"initiateShutdown":                 {},
	"runIfActive":                      {},
	"flatMapErrorThrowing":             {},
	"moveReaderIndex":                  {},
	"withFileHandle":                   {},
	"readToEnd":                        {},
	"openFile":                         {},
	"withContiguousStorageIfAvailable": {},
	"withUnsafeBytes":                  {},
	"reserveCapacity":                  {},
	"trimmingCharacters":               {},

	// More NIO / SwiftCrypto / Foundation PascalCase types found in
	// vapor framework source (wave 4 second-pass diagnostic).
	"NIOAny":                      {},
	"NIOThreadPool":               {},
	"NIOSSLContext":               {},
	"NIOLoopBoundBox":             {},
	"NIOWebSocketServerUpgrader":  {},
	"NonBlockingFileIO":           {},
	"ServerBootstrap":             {},
	"ServerQuiescingHelper":       {},
	"MultiThreadedEventLoopGroup": {},
	"SHA1":                        {},
	"SHA256":                      {},
	"SHA512":                      {},
	"SocketOptionLevel":           {},
	"SocketOptionValue":           {},
	"BreakLoopError":              {},
	"_CodingKey":                  {},
	"SendableBox":                 {},
	// Foundation Codable container protocol types (`encoder.container(
	// keyedBy: CodingKeys.self)` → `KeyedContainer`; the synthesizer
	// sees `UnkeyedContainer`/`SingleValueContainer` after the receiver
	// is stripped). Swift stdlib types — defence-in-depth via swift gate.
	"UnkeyedContainer":     {},
	"SingleValueContainer": {},
	// NIO HTTP/2 + HTTP/1 codec / handler types.
	"HTTPServerPipelineHandler":           {},
	"HTTPResponseEncoder":                 {},
	"HTTPResponseCompressor":              {},
	"HTTPRequestHead":                     {},
	"HTTPRequestDecoder":                  {},
	"HTTP2FramePayloadToHTTP1ServerCodec": {},
	"HighLowWatermark":                    {},
}

// csharpBareNames is the C#-language-gated bare-name stop-list (issue
// #441). The C# extractor strips the receiver from an ASP.NET Core MVC
// or EF Core call (`return Ok(model)` from a Controller base class →
// `Ok`, `db.Users.Where(...).FirstOrDefaultAsync()` → `Where` /
// `FirstOrDefaultAsync`, `HttpContext.User.IsAuthenticated` → `User` /
// `IsAuthenticated`), and the resolver can't bind the bare leaf to a
// local entity, so it lands in bug-extractor.
//
// Mirrors the Swift Vapor / Kotlin Ktor DSL precedents (issues #436 /
// #435): the language gate (lang == "csharp") is the safety net that
// prevents generic verbs like `Add`/`Update`/`Remove`/`Find`/`Where`/
// `Select`/`First` from shadowing user-defined methods in Go/JS/Java/
// Kotlin codebases. ASP.NET Core MVC and EF Core method names dominate
// the residual bug-extractor in real ASP.NET sample apps.
//
// Conservative selection (lessons from #94 / #105 / #106): names already
// classified by the language-agnostic stdlibBareNames map (`Response`,
// `NotFound`) are NOT duplicated here — they classify globally before
// the csharp gate fires.
//
// Categories:
//   - ASP.NET Core MVC ControllerBase action helpers (`Ok`, `BadRequest`,
//     `Unauthorized`, `Forbid`, `Conflict`, `UnprocessableEntity`,
//     `RedirectToAction`, `RedirectToRoute`, `RedirectToPage`,
//     `Redirect`, `View`, `PartialView`, `Json`, `Content`, `File`,
//     `PhysicalFile`, `Created`, `CreatedAtAction`, `CreatedAtRoute`,
//     `Accepted`, `NoContent`, `StatusCode`, `Problem`,
//     `ValidationProblem`).
//   - EF Core / LINQ-to-Entities query and persistence builders
//     (`FirstOrDefault`, `FirstOrDefaultAsync`, `SingleOrDefault`,
//     `SingleOrDefaultAsync`, `First`, `FirstAsync`, `Single`,
//     `SingleAsync`, `ToList`, `ToListAsync`, `ToArray`, `ToArrayAsync`,
//     `Include`, `ThenInclude`, `Where`, `Select`, `SelectMany`,
//     `OrderBy`, `OrderByDescending`, `ThenBy`, `GroupBy`, `Skip`,
//     `Take`, `Count`, `CountAsync`, `Sum`, `SumAsync`, `Average`,
//     `Max`, `Min`, `Any`, `All`, `Find`, `FindAsync`, `AsNoTracking`,
//     `AsQueryable`, `SaveChanges`, `SaveChangesAsync`, `Add`,
//     `AddAsync`, `AddRange`, `Update`, `Remove`, `RemoveRange`,
//     `Attach`, `Entry`).
//   - HttpContext / IActionResult accessors (`User`, `Request`,
//     `Response`, `Session`, `Items`, `Headers`, `Cookies`, `Form`,
//     `Query`).
//   - ASP.NET Core authentication helpers (`SignIn`, `SignOut`,
//     `Authenticate`, `Challenge`, `IsAuthenticated`, `HasClaim`).
//   - Microsoft.Extensions.DependencyInjection helpers
//     (`GetRequiredService`, `GetService`, `GetServices`,
//     `BuildServiceProvider`).
//
// Generic accessors `Get`/`Set` are deliberately NOT included — the
// #94 / #106 conservative-selection rule treats them as collision-prone
// even within a single ecosystem. EF Core's canonical `Add`, `Update`,
// `Remove`, `Find` ARE included because the lang=="csharp" gate scopes
// them to C# sources, and they dominate EF Core call-sites.
var csharpBareNames = map[string]struct{}{
	// ASP.NET Core MVC ControllerBase action helpers. `Response` and
	// `NotFound` are intentionally omitted — they're already in
	// stdlibBareNames and classify globally.
	"Ok":                  {},
	"BadRequest":          {},
	"Unauthorized":        {},
	"Forbid":              {},
	"Conflict":            {},
	"UnprocessableEntity": {},
	"RedirectToAction":    {},
	"RedirectToRoute":     {},
	"RedirectToPage":      {},
	"Redirect":            {},
	"View":                {},
	"PartialView":         {},
	"Json":                {},
	"Content":             {},
	"File":                {},
	"PhysicalFile":        {},
	"Created":             {},
	"CreatedAtAction":     {},
	"CreatedAtRoute":      {},
	"Accepted":            {},
	"NoContent":           {},
	"StatusCode":          {},
	"Problem":             {},
	"ValidationProblem":   {},

	// EF Core / LINQ-to-Entities query and persistence builders.
	// Names like `Where`, `Select`, `First`, `Add`, `Update`, `Remove`,
	// `Find` collide with generic ORM/collection verbs in any language;
	// safe only because of the csharp gate.
	"FirstOrDefault":       {},
	"FirstOrDefaultAsync":  {},
	"SingleOrDefault":      {},
	"SingleOrDefaultAsync": {},
	"First":                {},
	"FirstAsync":           {},
	"Single":               {},
	"SingleAsync":          {},
	"ToList":               {},
	"ToListAsync":          {},
	"ToArray":              {},
	"ToArrayAsync":         {},
	"Include":              {},
	"ThenInclude":          {},
	"Where":                {},
	"Select":               {},
	"SelectMany":           {},
	"OrderBy":              {},
	"OrderByDescending":    {},
	"ThenBy":               {},
	"GroupBy":              {},
	"Skip":                 {},
	"Take":                 {},
	"Count":                {},
	"CountAsync":           {},
	"Sum":                  {},
	"SumAsync":             {},
	"Average":              {},
	"Max":                  {},
	"Min":                  {},
	"Any":                  {},
	"All":                  {},
	"Find":                 {},
	"FindAsync":            {},
	"AsNoTracking":         {},
	"AsQueryable":          {},
	"SaveChanges":          {},
	"SaveChangesAsync":     {},
	"Add":                  {},
	"AddAsync":             {},
	"AddRange":             {},
	"Update":               {},
	"Remove":               {},
	"RemoveRange":          {},
	"Attach":               {},
	"Entry":                {},

	// HttpContext / IActionResult accessors. `Response` already
	// classifies globally via stdlibBareNames and is intentionally
	// omitted here.
	"User":    {},
	"Request": {},
	"Session": {},
	"Items":   {},
	"Headers": {},
	"Cookies": {},
	"Form":    {},
	"Query":   {},

	// ASP.NET Core authentication helpers.
	"SignIn":          {},
	"SignOut":         {},
	"Authenticate":    {},
	"Challenge":       {},
	"IsAuthenticated": {},
	"HasClaim":        {},

	// Microsoft.Extensions.DependencyInjection helpers.
	"GetRequiredService":   {},
	"GetService":           {},
	"GetServices":          {},
	"BuildServiceProvider": {},

	// Issue #441 (extended for aspnetcore-docs-samples bug-rate fix).
	// ASP.NET Core Generic Host / WebHost builder DSL — `Host.
	// CreateDefaultBuilder(args).ConfigureWebHostDefaults(b => b.UseStartup
	// <Startup>()).Build().Run()` is the entry-point pattern for every
	// ASP.NET Core app pre-`Program.cs` minimal-host. The receiver chain
	// is fluent (each method returns an IHostBuilder / IWebHostBuilder),
	// so the extractor receiver-strips every leaf. Names are
	// distinctive enough (camel/PascalCase domain verbs that don't appear
	// on generic Go/JS/Python collection or stdlib surfaces) to remain
	// behind the lang=="csharp" gate.
	"CreateDefaultBuilder":      {},
	"CreateBuilder":             {},
	"ConfigureWebHostDefaults":  {},
	"ConfigureWebHost":          {},
	"ConfigureAppConfiguration": {},
	"ConfigureServices":         {},
	"ConfigureLogging":          {},
	"ConfigureKestrel":          {},
	"UseStartup":                {},
	"UseKestrel":                {},
	"UseIIS":                    {},
	"UseIISIntegration":         {},
	"UseUrls":                   {},
	"UseEnvironment":            {},
	"UseContentRoot":            {},
	"UseWebRoot":                {},
	"UseDefaultServiceProvider": {},
	"UseSerilog":                {},
	"Build":                     {},
	"Run":                       {},
	"RunAsync":                  {},
	"Start":                     {},
	"StartAsync":                {},
	"StopAsync":                 {},

	// IConfigurationBuilder / IConfiguration DSL. `AddJsonFile`,
	// `AddXmlFile`, `AddIniFile`, `AddInMemoryCollection`,
	// `AddEnvironmentVariables`, `AddCommandLine`, `AddUserSecrets`,
	// `AddAzureKeyVault`, `Bind`, `Get<T>`, `GetSection`, `GetChildren`,
	// `GetValue<T>`, `AsEnumerable`, `Exists`. Configuration
	// bootstrap surface in every ASP.NET sample; canonical EF / DI
	// receiver-strip pattern.
	"AddJsonFile":             {},
	"AddXmlFile":              {},
	"AddIniFile":              {},
	"AddInMemoryCollection":   {},
	"AddEnvironmentVariables": {},
	"AddCommandLine":          {},
	"AddUserSecrets":          {},
	"AddAzureKeyVault":        {},
	"AddKeyPerFile":           {},
	"Bind":                    {},
	"GetSection":              {},
	"GetChildren":             {},
	"GetValue":                {},
	"AsEnumerable":            {},
	"Exists":                  {},
	"GetConnectionString":     {},

	// IApplicationBuilder / IEndpointRouteBuilder middleware DSL —
	// `app.UseStaticFiles()`, `app.UseRouting()`, `app.UseEndpoints(...)`,
	// `endpoints.MapGet/MapPost/MapControllers/MapRazorPages`.
	"UseStaticFiles":            {},
	"UseRouting":                {},
	"UseEndpoints":              {},
	"UseAuthentication":         {},
	"UseAuthorization":          {},
	"UseCors":                   {},
	"UseHsts":                   {},
	"UseHttpsRedirection":       {},
	"UseDeveloperExceptionPage": {},
	"UseExceptionHandler":       {},
	"UseStatusCodePages":        {},
	"UseSession":                {},
	"UseMvc":                    {},
	"UseMvcWithDefaultRoute":    {},
	"UseSwagger":                {},
	"UseSwaggerUI":              {},
	"UseSpa":                    {},
	"UseDefaultFiles":           {},
	"UseDatabaseErrorPage":      {},
	"UseRequestLocalization":    {},
	"UseResponseCaching":        {},
	"UseResponseCompression":    {},
	"MapGet":                    {},
	"MapPost":                   {},
	"MapPut":                    {},
	"MapDelete":                 {},
	"MapPatch":                  {},
	"MapControllers":            {},
	"MapControllerRoute":        {},
	"MapDefaultControllerRoute": {},
	"MapAreaControllerRoute":    {},
	"MapRazorPages":             {},
	"MapHub":                    {},
	"MapHealthChecks":           {},
	"MapFallbackToFile":         {},
	"MapFallbackToPage":         {},
	"MapWhen":                   {},

	// IServiceCollection registration helpers — `AddScoped<I, T>()`,
	// `AddSingleton<T>()`, `AddTransient<T>()`, `AddMvc()`, `AddDbContext
	// <TContext>(...)`, etc. The receiver is always an IServiceCollection
	// passed to ConfigureServices; the leaf method is the strip target.
	"AddScoped":                 {},
	"AddSingleton":              {},
	"AddTransient":              {},
	"AddHostedService":          {},
	"AddMvc":                    {},
	"AddMvcCore":                {},
	"AddControllers":            {},
	"AddControllersWithViews":   {},
	"AddRazorPages":             {},
	"AddRouting":                {},
	"AddDbContext":              {},
	"AddDbContextPool":          {},
	"AddDbContextFactory":       {},
	"AddIdentity":               {},
	"AddDefaultIdentity":        {},
	"AddAuthentication":         {},
	"AddAuthorization":          {},
	"AddCors":                   {},
	"AddSession":                {},
	"AddSignalR":                {},
	"AddSwaggerGen":             {},
	"AddSpaStaticFiles":         {},
	"AddHealthChecks":           {},
	"AddHttpClient":             {},
	"AddHttpContextAccessor":    {},
	"AddLogging":                {},
	"AddOptions":                {},
	"AddMemoryCache":            {},
	"AddDistributedMemoryCache": {},
	"AddResponseCaching":        {},
	"AddResponseCompression":    {},
	"AddLocalization":           {},
	"AddDataProtection":         {},
	"AddAntiforgery":            {},
	"Configure":                 {},
	"PostConfigure":             {},
	"TryAddScoped":              {},
	"TryAddSingleton":           {},
	"TryAddTransient":           {},
	"SetCompatibilityVersion":   {},

	// EF Core DbContextOptionsBuilder — `UseSqlServer`, `UseSqlite`,
	// `UseInMemoryDatabase`, `UseNpgsql`, `UseMySql`, `UseCosmos`, plus
	// migrations / database-creation helpers (`Migrate`, `EnsureCreated`,
	// `EnsureDeleted`).
	"UseSqlServer":             {},
	"UseSqlite":                {},
	"UseInMemoryDatabase":      {},
	"UseNpgsql":                {},
	"UseMySql":                 {},
	"UseMySQL":                 {},
	"UseCosmos":                {},
	"UseOracle":                {},
	"UseLazyLoadingProxies":    {},
	"UseChangeTrackingProxies": {},
	"Migrate":                  {},
	"MigrateAsync":             {},
	"EnsureCreated":            {},
	"EnsureCreatedAsync":       {},
	"EnsureDeleted":            {},
	"EnsureDeletedAsync":       {},

	// ILogger<T> structured-logging surface — `LogInformation`,
	// `LogWarning`, `LogError`, `LogDebug`, `LogTrace`, `LogCritical`,
	// `BeginScope`, `IsEnabled`. Receiver is always an injected
	// `ILogger<TCategory>`; the call is `_logger.LogInformation("msg")`.
	"LogInformation": {},
	"LogWarning":     {},
	"LogError":       {},
	"LogDebug":       {},
	"LogTrace":       {},
	"LogCritical":    {},
	"BeginScope":     {},

	// Razor Pages / Controller action-result helpers.
	// `Page()` is the Razor Pages PageBase result method (every
	// `return Page();` in OnGet/OnPost); `PageResult` / `LocalRedirect`
	// / `LocalRedirectPermanent` are sibling action-result factories.
	"Page":                      {},
	"PageResult":                {},
	"LocalRedirect":             {},
	"LocalRedirectPermanent":    {},
	"RedirectPermanent":         {},
	"RedirectToActionPermanent": {},
	"RedirectToPagePermanent":   {},
	"RedirectToRoutePermanent":  {},

	// ModelState / TempData / ViewData helpers — `ModelState.AddModelError`,
	// `ModelState.IsValid`, `ModelState.Clear`. Receiver-stripped.
	"AddModelError":       {},
	"TryUpdateModelAsync": {},
	"TryValidateModel":    {},
	"IsValid":             {},

	// EF Core change-tracking surface beyond the v1 list — `Attach`,
	// `AddAsync`, `AttachRange`, `FromSqlRaw`, `FromSqlInterpolated`,
	// `ExecuteSqlRaw`, `ExecuteSqlInterpolated`, `Reload`.
	"AttachRange":                 {},
	"FromSqlRaw":                  {},
	"FromSqlInterpolated":         {},
	"ExecuteSqlRaw":               {},
	"ExecuteSqlInterpolated":      {},
	"ExecuteSqlRawAsync":          {},
	"ExecuteSqlInterpolatedAsync": {},
	"Reload":                      {},
	"ReloadAsync":                 {},

	// System.* / Object.* method surface that the C# extractor strips
	// from `obj.ToString()`, `string.IsNullOrEmpty(s)`,
	// `Dictionary<K,V>` constructors, `ArgumentNullException.ThrowIf...`.
	// These are .NET BCL primitives — universal across every csharp file
	// and very common after receiver-strip. Gated by lang=="csharp".
	"ToString":           {},
	"GetHashCode":        {},
	"Equals":             {},
	"GetType":            {},
	"MemberwiseClone":    {},
	"ReferenceEquals":    {},
	"CompareTo":          {},
	"IsNullOrEmpty":      {},
	"IsNullOrWhiteSpace": {},
	"StartsWith":         {},
	"EndsWith":           {},
	"Contains":           {},
	"Replace":            {},
	"Split":              {},
	"Substring":          {},
	"Trim":               {},
	"TrimStart":          {},
	"TrimEnd":            {},
	"PadLeft":            {},
	"PadRight":           {},
	"ToLower":            {},
	"ToUpper":            {},
	"ToLowerInvariant":   {},
	"ToUpperInvariant":   {},
	"Format":             {},
	"Concat":             {},
	"Join":               {},
	"Parse":              {},
	"TryParse":           {},
	"Compare":            {},
	"IndexOf":            {},
	"LastIndexOf":        {},
	"GetEnumerator":      {},
	"MoveNext":           {},
	"Dispose":            {},
	"DisposeAsync":       {},
	"Clear":              {},
	"Clone":              {},
	"CopyTo":             {},
	"ConvertAll":         {},
	"ContainsKey":        {},
	"ContainsValue":      {},
	"TryGetValue":        {},
	"GetValueOrDefault":  {},
	"ToDictionary":       {},
	"ToHashSet":          {},
	"ToLookup":           {},
	"Distinct":           {},
	"Cast":               {},
	"OfType":             {},
	"Zip":                {},
	"Concat_":            {}, // guarded variant — keep below `Concat`
	"Aggregate":          {},
	"Reverse":            {},
	"Sort":               {},
	"Range":              {},
	"Repeat":             {},
	"Empty":              {},
	"AsSpan":             {},
	"AsMemory":           {},
	"GetAwaiter":         {},
	"GetResult":          {},
	"ConfigureAwait":     {},
	"WhenAll":            {},
	"WhenAny":            {},
	"Delay":              {},
	"FromResult":         {},
	"CompletedTask":      {},
	"AddDays":            {},
	"AddHours":           {},
	"AddMinutes":         {},
	"AddSeconds":         {},
	"AddMonths":          {},
	"AddYears":           {},
	"AddMilliseconds":    {},
	"AddTicks":           {},
	"ToShortDateString":  {},
	"ToLongDateString":   {},
	"ToShortTimeString":  {},
	"ToLongTimeString":   {},

	// `nameof` and `typeof` look like method calls in the tree-sitter
	// CST (`nameof(x)`, `typeof(Foo)`) — but they're C# language
	// keywords. Emitted as bare CALLS by the extractor; classify them
	// as csharp-language builtins.
	"nameof":  {},
	"typeof":  {},
	"sizeof":  {},
	"default": {},

	// Exception throw helpers — `throw new ArgumentNullException(...)`
	// → `ArgumentNullException` as a constructor bare name, plus
	// `ArgumentException.ThrowIfNullOrEmpty(...)` static helpers.
	"ArgumentNullException":       {},
	"ArgumentException":           {},
	"ArgumentOutOfRangeException": {},
	"InvalidOperationException":   {},
	"NotSupportedException":       {},
	"NotImplementedException":     {},
	"ObjectDisposedException":     {},
	"FormatException":             {},
	"OperationCanceledException":  {},
	"TaskCanceledException":       {},
	"ThrowIfNull":                 {},
	"ThrowIfNullOrEmpty":          {},
	"ThrowIfNullOrWhiteSpace":     {},
	"ThrowIfLessThan":             {},
	"ThrowIfGreaterThan":          {},

	// Common generic collection / container constructor names. These
	// appear as bare `new Dictionary<...>()` → `Dictionary` (the
	// generic-arg suffix gets stripped). All on System.Collections.Generic.
	"Dictionary":           {},
	"List":                 {},
	"HashSet":              {},
	"SortedDictionary":     {},
	"SortedSet":            {},
	"SortedList":           {},
	"Queue":                {},
	"Stack":                {},
	"LinkedList":           {},
	"ConcurrentDictionary": {},
	"ConcurrentBag":        {},
	"ConcurrentQueue":      {},
	"ConcurrentStack":      {},
	"KeyValuePair":         {},
	"Tuple":                {},
	"ValueTuple":           {},
	"Lazy":                 {},
	"Nullable":             {},

	// Authentication / Cookie / SignIn fluent helpers added with
	// AddCookie / AddJwtBearer / AddOpenIdConnect.
	"AddCookie":           {},
	"AddJwtBearer":        {},
	"AddOpenIdConnect":    {},
	"AddOAuth":            {},
	"AddGoogle":           {},
	"AddFacebook":         {},
	"AddMicrosoftAccount": {},
	"AddTwitter":          {},

	// EF Core ModelBuilder fluent surface — used inside
	// `OnModelCreating(ModelBuilder builder)` overrides:
	//   builder.Entity<Foo>().HasKey(x => x.Id)
	//          .Property(x => x.Name).IsRequired().HasMaxLength(64)
	//          .HasOne(x => x.Bar).WithMany(b => b.Foos)
	//          .HasForeignKey(x => x.BarId).OnDelete(DeleteBehavior.Cascade);
	"Entity":                      {},
	"HasKey":                      {},
	"HasIndex":                    {},
	"HasOne":                      {},
	"HasMany":                     {},
	"WithOne":                     {},
	"WithMany":                    {},
	"HasForeignKey":               {},
	"HasPrincipalKey":             {},
	"OnDelete":                    {},
	"HasMaxLength":                {},
	"HasName":                     {},
	"HasColumnName":               {},
	"HasColumnType":               {},
	"HasConstraintName":           {},
	"HasDefaultValue":             {},
	"HasDefaultValueSql":          {},
	"HasComputedColumnSql":        {},
	"HasConversion":               {},
	"HasData":                     {},
	"HasDiscriminator":            {},
	"HasQueryFilter":              {},
	"IsRequired":                  {},
	"IsUnique":                    {},
	"IsConcurrencyToken":          {},
	"IsRowVersion":                {},
	"IsFixedLength":               {},
	"IsUnicode":                   {},
	"ValueGeneratedOnAdd":         {},
	"ValueGeneratedOnAddOrUpdate": {},
	"ValueGeneratedOnUpdate":      {},
	"ValueGeneratedNever":         {},
	"ToTable":                     {},
	"ToView":                      {},
	"ToSqlQuery":                  {},
	"ToFunction":                  {},
	"Property":                    {},
	"OwnsOne":                     {},
	"OwnsMany":                    {},
	"Ignore":                      {},
	"UsePropertyAccessMode":       {},
	"UseSerialColumn":             {},
	"UseIdentityColumn":           {},
	"UseHiLo":                     {},

	// Microsoft.Extensions.Logging ILoggingBuilder fluent surface —
	// `loggingBuilder.AddConsole().AddDebug().SetMinimumLevel(LogLevel.
	// Information).AddFilter<DebugLoggerProvider>(...).ClearProviders()`.
	"AddConsole":                {},
	"AddDebug":                  {},
	"AddEventSourceLogger":      {},
	"AddEventLog":               {},
	"AddTraceSource":            {},
	"AddAzureWebAppDiagnostics": {},
	"AddApplicationInsights":    {},
	"AddOpenTelemetry":          {},
	"SetMinimumLevel":           {},
	"AddFilter":                 {},
	"ClearProviders":            {},

	// Moq testing surface (heavily receiver-stripped on fluent setup
	// chains): `var mock = new Mock<IFoo>(); mock.Setup(x => x.Bar()).
	// Returns(42); mock.Verify(...)`. Lang gate keeps `Setup`/`Verify`/
	// `Returns` scoped to csharp — collides with too many user methods
	// otherwise. `Mock` as bare appears when the extractor strips the
	// generic `Mock<IFoo>` constructor.
	"Mock":               {},
	"Setup":              {},
	"SetupGet":           {},
	"SetupSet":           {},
	"SetupSequence":      {},
	"Verify":             {},
	"VerifyGet":          {},
	"VerifySet":          {},
	"VerifyAll":          {},
	"VerifyNoOtherCalls": {},
	"Returns":            {},
	"ReturnsAsync":       {},
	"Throws":             {},
	"ThrowsAsync":        {},
	"Callback":           {},
	"Raises":             {},
	"Object":             {}, // mock.Object accessor (lang-gated)

	// Misc action-results / IActionResult constructor bare names.
	"ObjectResult":           {},
	"NoContentResult":        {},
	"OkObjectResult":         {},
	"BadRequestObjectResult": {},
	"NotFoundObjectResult":   {},
	"JsonResult":             {},
	"FileResult":             {},
	"RedirectResult":         {},
	"ContentResult":          {},
	"ViewResult":             {},
	"EmptyResult":            {},
	"PageResult_":            {}, // sentinel — kept distinct from PageResult above

	// Newtonsoft.Json / System.Text.Json common receiver-stripped names.
	"DeserializeObject":    {},
	"SerializeObject":      {},
	"Deserialize":          {},
	"Serialize":            {},
	"FromJsonAsync":        {},
	"WriteAsJsonAsync":     {},
	"ReadFromJsonAsync":    {},
	"ReadAsAsync":          {},
	"ReadAsStringAsync":    {},
	"ReadAsByteArrayAsync": {},
	"ReadAsStreamAsync":    {},
	"PostAsync":            {},
	"PutAsync":             {},
	"DeleteAsync":          {},
	"GetAsync":             {},
	"GetStringAsync":       {},
	"GetByteArrayAsync":    {},
	"GetStreamAsync":       {},
	"SendAsync":            {},
}

// phpBareNames is the PHP-language-gated bare-name stop-list (issue
// #439). The PHP extractor receiver-strips Laravel / Symfony DSL calls
// (`$user->save()` → `save`, `User::find($id)` → `find`,
// `Route::get('/x', ...)` → `get`/`post`/etc., `$this->render(...)` →
// `render`, `Cache::remember(...)` → `remember`) and the resolver
// can't bind the bare leaf to a local entity, so it lands in
// bug-extractor.
//
// Mirrors the Kotlin Ktor (#435) and Swift Vapor (#436) precedents:
// the language gate (lang == "php") is the safety net that keeps
// generic verbs like `find`/`save`/`update`/`render` from shadowing
// user-defined methods in Go/JS/Python/Ruby/Kotlin/Swift codebases.
//
// Conservative selection (lessons from #94 / #105 / #106): names
// already classified by the language-agnostic stdlibBareNames map
// (`filter`, `map`, `set`, `range`, `join`, `Response`) are NOT
// duplicated here — they classify globally before the php gate fires.
//
// Deliberately OMITTED (issue #439 spec, "REJECT" list):
//   - HTTP verb bare names `get` / `post` / `put` / `delete`. Although
//     these are emitted by Laravel `Route::get(...)`, in PHP source
//     they collide trivially with Eloquent attribute-accessor patterns
//     (`$model->get('name')`) and PSR-7 ServerRequest accessors. The
//     #94 / #106 safer-bias rule applies: a missed Route classification
//     is strictly better than shadowing a real `->get()`/`->delete()`
//     user method.
//
// Categories:
//   - Eloquent ORM persistence + query builder (`find`/`save`/`where`/
//     `with`/`paginate`/`pluck`/...).
//   - Symfony Controller helpers (`render`/`redirectToRoute`/
//     `createForm`/`generateUrl`/...).
//   - Laravel facade DSL leaves (`routes`/`middleware`/`controller`/
//     `domain`/`prefix`).
//   - Laravel global helpers (`config`/`env`/`route`/`auth`/`request`/
//     `view`/`response`/`back`/`old`/...).
var phpBareNames = map[string]struct{}{
	// Eloquent ORM — persistence and lifecycle.
	"find":          {},
	"findOrFail":    {},
	"findMany":      {},
	"firstOrFail":   {},
	"firstOrCreate": {},
	"save":          {},
	"update":        {},
	"delete":        {}, // Eloquent model destructor; receiver-stripped from `$model->delete()`.
	"forceDelete":   {},
	"restore":       {},
	"create":        {},
	"make":          {},
	"fill":          {},
	"refresh":       {},
	"fresh":         {},
	"replicate":     {},
	"is":            {},
	"isNot":         {},
	"belongsTo":     {},
	"belongsToMany": {},
	"hasMany":       {},
	"hasOne":        {},
	"morphTo":       {},
	"morphMany":     {},
	"morphOne":      {},

	// Eloquent / query builder — selection and filtering.
	"where":        {},
	"whereIn":      {},
	"whereNotIn":   {},
	"whereHas":     {},
	"whereNull":    {},
	"whereNotNull": {},
	"whereBetween": {},
	"whereDate":    {},
	"with":         {},
	"without":      {},
	"orderBy":      {},
	"groupBy":      {},
	"having":       {},
	"limit":        {},
	"take":         {},
	"skip":         {},
	"first":        {},
	"latest":       {},
	"oldest":       {},
	"paginate":     {},
	"count":        {},
	"avg":          {},
	"pluck":        {},
	"chunk":        {},
	"each":         {},
	"select":       {},
	"selectRaw":    {},
	"union":        {},
	"unionAll":     {},
	"joinSub":      {},
	"crossJoin":    {},
	"leftJoin":     {},
	"rightJoin":    {},
	"joins":        {},

	// Symfony AbstractController helpers (post-receiver-strip from
	// `$this->render(...)` / `$this->redirectToRoute(...)`).
	"render":                  {},
	"redirectToRoute":         {},
	"redirect":                {},
	"createForm":              {},
	"createFormBuilder":       {},
	"addFlash":                {},
	"denyAccessUnlessGranted": {},
	"getUser":                 {},
	"isGranted":               {},
	"generateUrl":             {},
	"json":                    {},
	"file":                    {},
	"forward":                 {},
	"getDoctrine":             {},
	"getParameter":            {},
	"dispatchEvent":           {},

	// Laravel facade DSL leaves — receiver is `Route::`, `Cache::`,
	// `Storage::`, etc.; the leaf bare-name lands at the resolver.
	"routes":     {},
	"middleware": {},
	"controller": {},
	"domain":     {},
	"prefix":     {},

	// Laravel global helpers (functions in the Illuminate\Support
	// namespace, autoloaded as bare callables in framework code).
	"config":     {},
	"env":        {},
	"route":      {},
	"url":        {},
	"asset":      {},
	"auth":       {},
	"request":    {},
	"session":    {},
	"cookie":     {},
	"view":       {},
	"response":   {},
	"back":       {},
	"old":        {},
	"csrf_token": {},
	"csrf_field": {},
	"dd":         {},
	"dump":       {},
	"now":        {},
	"today":      {},
	"app":        {},
	"resolve":    {},
	"event":      {},
	"dispatch":   {},
	"validator":  {},
	"optional":   {},
	"tap":        {},

	// Laravel Schema Builder / Blueprint column types and modifiers
	// (issue #485 PHP wave-3). Migration closures receive a `$table`
	// Blueprint and call column-type methods like `$table->string('name')`
	// or `$table->timestamps()`; the PHP extractor receiver-strips to the
	// bare leaf. These are unambiguous DDL column declarators that
	// don't collide with user-defined methods in non-PHP languages
	// (`increments`, `rememberToken`, `tinyInteger`, etc. are PHP-Laravel
	// specific) but stay PHP-gated for safety.
	"increments":            {},
	"bigIncrements":         {},
	"tinyIncrements":        {},
	"smallIncrements":       {},
	"mediumIncrements":      {},
	"bigInteger":            {},
	"smallInteger":          {},
	"tinyInteger":           {},
	"mediumInteger":         {},
	"unsignedInteger":       {},
	"unsignedBigInteger":    {},
	"unsignedSmallInteger":  {},
	"unsignedTinyInteger":   {},
	"unsignedMediumInteger": {},
	"string":                {},
	"char":                  {},
	"text":                  {},
	"mediumText":            {},
	"longText":              {},
	"binary":                {},
	"timestamp":             {},
	"timestamps":            {},
	"timestampsTz":          {},
	"nullableTimestamps":    {},
	"softDeletes":           {},
	"softDeletesTz":         {},
	"rememberToken":         {},
	"morphs":                {},
	"nullableMorphs":        {},
	"uuidMorphs":            {},
	"nullableUuidMorphs":    {},
	"uuid":                  {},
	"ipAddress":             {},
	"macAddress":            {},
	"year":                  {},
	"time":                  {},
	"timeTz":                {},
	"dateTime":              {},
	"dateTimeTz":            {},
	"unique":                {},
	"primary":               {},
	"foreign":               {},
	"references":            {},
	"on":                    {},
	"onDelete":              {},
	"onUpdate":              {},
	"cascadeOnDelete":       {},
	"nullOnDelete":          {},
	"restrictOnDelete":      {},
	"cascadeOnUpdate":       {},
	"nullable":              {},
	"default":               {},
	"useCurrent":            {},
	"useCurrentOnUpdate":    {},
	"unsigned":              {},
	"autoIncrement":         {},
	"after":                 {},
	"comment":               {},
	"change":                {},
	"dropColumn":            {},
	"dropIfExists":          {},
	"dropForeign":           {},
	"dropUnique":            {},
	"dropPrimary":           {},
	"dropIndex":             {},
	"renameColumn":          {},
	"rename":                {},
	"enum":                  {},

	// Laravel testing DSL — receiver-stripped Browser / HTTP test helpers
	// emitted by `$this->visit('/')->see('foo')->press('Submit')` chains
	// in TestCase classes. PHP-only — `see` / `press` / `factory` do not
	// collide with user verbs in cross-language work because the gate
	// fires only on lang=="php".
	"see":                  {},
	"dontSee":              {},
	"press":                {},
	"click":                {},
	"visit":                {},
	"type":                 {},
	"submitForm":           {},
	"submit":               {},
	"assertResponseOk":     {},
	"assertResponseStatus": {},
	"assertViewHas":        {},
	"assertSessionHas":     {},
	"assertRedirectedTo":   {},
	"factory":              {},
	"str_random":           {},
	"str_slug":             {},
	"snake_case":           {},
	"studly_case":          {},
	"camel_case":           {},
	"kebab_case":           {},

	// Laravel Auth / Request / Session helpers — receiver-stripped from
	// `Auth::check()`, `$request->wantsJson()`, `$request->ajax()`,
	// `Auth::guest()`, etc.
	"check":     {},
	"guest":     {},
	"wantsJson": {},
	"ajax":      {},
	"pjax":      {},
	"secure":    {},
	"fullUrl":   {},
	"path":      {},
	"input":     {},
	"all":       {},
	"only":      {},
	"except":    {},
	"has":       {},
	"filled":    {},
	"missing":   {},
	"boolean":   {},
	"date":      {},
	"flash":     {},
	"forget":    {},
	"keep":      {},
	"reflash":   {},
	"pull":      {},
	"push":      {},

	// Laravel global path helpers (`app_path`, `base_path`, `config_path`,
	// `storage_path`, etc. are autoloaded as bare callables in framework
	// code). Mirrors the `config` / `env` / `route` entries above.
	"app_path":      {},
	"base_path":     {},
	"config_path":   {},
	"storage_path":  {},
	"public_path":   {},
	"database_path": {},
	"resource_path": {},
	"mix":           {},
	"abort":         {},
	"abort_if":      {},
	"abort_unless":  {},
	"action":        {},
	"bcrypt":        {},
	"broadcast":     {},
	"cache":         {},
	"collect":       {},
	"decrypt":       {},
	"encrypt":       {},
	"info":          {},
	"logger":        {},
	"method_field":  {},
	"policy":        {},
	"report":        {},
	"rescue":        {},
	"trans":         {},
	"trans_choice":  {},
	"__":            {},

	// Laravel Route DSL — additional facade method leaves from
	// `Route::group(...)` / `Route::middleware(...)` / `Route::resource()`
	// receiver-strip.
	"group":     {},
	"namespace": {},
	"resource":  {},
	"resources": {},
	"name":      {},
	"as":        {},
	"any":       {},
	"match":     {},
	"redirect_": {}, // sentinel; method name is `redirect` (handled above)

	// Doctrine ORM EntityManager / Repository / QueryBuilder / Collection
	// methods (issue #485 PHP wave-3). Receiver-stripped from
	// `$em->persist($e)`, `$qb->setParameter(...)`, etc. These are
	// canonical Doctrine API verbs; conservatively PHP-gated.
	"persist":               {},
	"flush":                 {},
	"removeElement":         {},
	"contains":              {},
	"isEmpty":               {},
	"add":                   {},
	"clear":                 {},
	"detach":                {},
	"merge":                 {},
	"setMaxResults":         {},
	"setFirstResult":        {},
	"setParameter":          {},
	"setParameters":         {},
	"getQuery":              {},
	"getResult":             {},
	"getArrayResult":        {},
	"getScalarResult":       {},
	"getOneOrNullResult":    {},
	"getSingleResult":       {},
	"getSingleScalarResult": {},
	"findOneBy":             {},
	"findBy":                {},
	"findAll":               {},
	"andWhere":              {},
	"orWhere":               {},
	"innerJoin":             {},
	"matching":              {},
	"andHaving":             {},
	"orHaving":              {},
	"expr":                  {},
	"createQueryBuilder":    {},
	"createQuery":           {},
	"createNamedQuery":      {},
	"getRepository":         {},
	"getEntityManager":      {},

	// Symfony Form / Validator / OptionsResolver / Console helpers
	// (issue #485 PHP wave-3). Receiver-stripped from
	// `$form->isValid()`, `$resolver->setDefaults(...)`,
	// `$io->success(...)`, `$io->ask(...)`.
	"isValid":          {},
	"isSubmitted":      {},
	"handleRequest":    {},
	"setDefaults":      {},
	"setAllowedTypes":  {},
	"setAllowedValues": {},
	"setRequired":      {},
	"setDefined":       {},
	"setNormalizer":    {},
	"success":          {},
	"warning":          {},
	"error":            {},
	"caution":          {},
	"note":             {},
	"ask":              {},
	"askHidden":        {},
	"askQuestion":      {},
	"confirm":          {},
	"choice":           {},
	"progressStart":    {},
	"progressAdvance":  {},
	"progressFinish":   {},
	"section":          {},
	"title":            {},
	"listing":          {},
	"table":            {},
	"writeln":          {},
	"write":            {},
	"setArgument":      {},
	"getArgument":      {},
	"setOption":        {},
	"getOption":        {},
	"setApplication":   {},

	// Symfony Console Command lifecycle (`$this->initialize`, etc.)
	"initialize": {},
	"interact":   {},
	"configure":  {},

	// Symfony WebTestCase / KernelTestCase / BrowserKit assertions
	// (issue #485 PHP wave-3). Receiver-stripped from
	// `$client->loginUser($u)`, `$this->createClient()`, etc.
	"createClient":                 {},
	"loginUser":                    {},
	"getContainer":                 {},
	"bootKernel":                   {},
	"ensureKernelShutdown":         {},
	"shutdown":                     {},
	"assertResponseIsSuccessful":   {},
	"assertResponseRedirects":      {},
	"assertResponseStatusCodeSame": {},
	"assertResponseHasHeader":      {},
	"assertSelectorExists":         {},
	"assertSelectorTextContains":   {},
	"assertSelectorTextSame":       {},
	"assertSelectorNotExists":      {},
	"assertPageTitleContains":      {},
	"assertPageTitleSame":          {},
	"assertEmailCount":             {},
	"assertEmailHtmlBodyContains":  {},
	"assertEmailTextBodyContains":  {},
	"assertEmailAddressContains":   {},
	"selectButton":                 {},
	"selectLink":                   {},
	"clickLink":                    {},
	"setController":                {},

	// PHPUnit Assert API surface — used directly via static or
	// `$this->assert*` in TestCase descendants. Receiver-stripped to
	// the bare assertion verb. The PHP gate keeps these from
	// shadowing user methods in non-PHP code.
	"assertSame":                     {},
	"assertNotSame":                  {},
	"assertEquals":                   {},
	"assertNotEquals":                {},
	"assertTrue":                     {},
	"assertFalse":                    {},
	"assertNull":                     {},
	"assertNotNull":                  {},
	"assertEmpty":                    {},
	"assertNotEmpty":                 {},
	"assertContains":                 {},
	"assertNotContains":              {},
	"assertCount":                    {},
	"assertGreaterThan":              {},
	"assertLessThan":                 {},
	"assertInstanceOf":               {},
	"assertNotInstanceOf":            {},
	"assertMatchesRegularExpression": {},
	"assertStringContainsString":     {},
	"assertStringStartsWith":         {},
	"assertStringEndsWith":           {},
	"assertArrayHasKey":              {},
	"assertArrayNotHasKey":           {},
	"assertObjectHasAttribute":       {},
	"expectException":                {},
	"expectExceptionMessage":         {},
	"expectExceptionMessageMatches":  {},
	"markTestSkipped":                {},
	"markTestIncomplete":             {},
	"setThrowable":                   {},
	"setHint":                        {},
	"setPost":                        {},

	// PHP built-in functions called as bare names (issue #485 PHP
	// wave-3). PHP autoloads the global function namespace, so every
	// `array_map`, `implode`, `random_int`, `mb_strlen` call shows up
	// as a bare CALLS target after the extractor. These are stdlib
	// functions with `_` separators (PHP's snake_case stdlib
	// convention) — high-volume in framework code, near-zero collision
	// risk against PascalCase / camelCase user identifiers in other
	// languages but PHP-gated anyway for safety. The names already in
	// stdlibBareNames (`filter`, `map`, `range`, `len`, ...) are NOT
	// duplicated here.
	"array_map":            {},
	"array_filter":         {},
	"array_reduce":         {},
	"array_unique":         {},
	"array_merge":          {},
	"array_values":         {},
	"array_keys":           {},
	"array_combine":        {},
	"array_flip":           {},
	"array_search":         {},
	"array_slice":          {},
	"array_splice":         {},
	"array_diff":           {},
	"array_intersect":      {},
	"array_walk":           {},
	"array_key_exists":     {},
	"array_key_first":      {},
	"array_key_last":       {},
	"array_fill":           {},
	"array_sum":            {},
	"array_product":        {},
	"in_array":             {},
	"is_array":             {},
	"is_string":            {},
	"is_int":               {},
	"is_numeric":           {},
	"is_null":              {},
	"is_bool":              {},
	"is_callable":          {},
	"is_object":            {},
	"is_a":                 {},
	"is_subclass_of":       {},
	"is_dir":               {},
	"is_file":              {},
	"implode":              {},
	"explode":              {},
	"trim":                 {},
	"ltrim":                {},
	"rtrim":                {},
	"strlen":               {},
	"strpos":               {},
	"strrpos":              {},
	"substr":               {},
	"sprintf":              {},
	"printf":               {},
	"str_replace":          {},
	"str_repeat":           {},
	"str_split":            {},
	"str_contains":         {},
	"str_starts_with":      {},
	"str_ends_with":        {},
	"str_pad":              {},
	"str_shuffle":          {},
	"str_word_count":       {},
	"strtolower":           {},
	"strtoupper":           {},
	"ucfirst":              {},
	"lcfirst":              {},
	"ucwords":              {},
	"preg_match":           {},
	"preg_match_all":       {},
	"preg_replace":         {},
	"preg_split":           {},
	"preg_quote":           {},
	"random_int":           {},
	"random_bytes":         {},
	"mt_rand":              {},
	"rand":                 {},
	"min":                  {},
	"max":                  {},
	"abs":                  {},
	"floor":                {},
	"ceil":                 {},
	"round":                {},
	"intval":               {},
	"floatval":             {},
	"strval":               {},
	"boolval":              {},
	"settype":              {},
	"gettype":              {},
	"mb_strlen":            {},
	"mb_substr":            {},
	"mb_strtolower":        {},
	"mb_strtoupper":        {},
	"mb_convert_encoding":  {},
	"json_encode":          {},
	"json_decode":          {},
	"serialize":            {},
	"unserialize":          {},
	"base64_encode":        {},
	"base64_decode":        {},
	"hash":                 {},
	"md5":                  {},
	"sha1":                 {},
	"crc32":                {},
	"password_hash":        {},
	"password_verify":      {},
	"shuffle":              {},
	"sort":                 {},
	"rsort":                {},
	"usort":                {},
	"uksort":               {},
	"uasort":               {},
	"asort":                {},
	"ksort":                {},
	"empty":                {},
	"isset":                {},
	"compact":              {},
	"extract":              {},
	"func_get_args":        {},
	"func_num_args":        {},
	"call_user_func":       {},
	"call_user_func_array": {},
	"get_class":            {},
	"get_object_vars":      {},
	"property_exists":      {},
	"method_exists":        {},
	"class_exists":         {},
	"interface_exists":     {},
	"function_exists":      {},
	"defined":              {},
	"file_exists":          {},
	"file_get_contents":    {},
	"file_put_contents":    {},
	"unlink":               {},
	"rmdir":                {},
	"scandir":              {},
	"glob":                 {},
	"pathinfo":             {},
	// `mkdir`, `realpath`, `basename`, `dirname` intentionally OMITTED:
	// they are claimed by pythonBareNames as Python-only stdlib helpers
	// (issue #447) and the cross-language gate test
	// TestPythonDjangoDRFDSLBareNames_NotClassifiedForOtherLanguages
	// asserts they do not classify under other languages.
	"microtime":   {},
	"mktime":      {},
	"strtotime":   {},
	"date_create": {},
	"date_format": {},

	// Common PHP / Symfony class constructor bare-name receivers
	// (the `new Foo()` pattern's class identifier surfaces as a bare
	// CALLS target after the extractor strips the `new` keyword).
	// Conservative selection — limited to PHP-specific framework
	// classes whose identifier would not collide with user types in
	// non-PHP languages.
	"DateTimeImmutable":        {},
	"DateTime":                 {},
	"RuntimeException":         {},
	"LogicException":           {},
	"InvalidArgumentException": {},
	"OutOfBoundsException":     {},
	"UnexpectedValueException": {},
	"Email":                    {},
	"ArrayCollection":          {},
	"Stopwatch":                {},
	"SymfonyStyle":             {},
	"Application":              {},
	"CommandTester":            {},
	"InputArgument":            {},
	"InputOption":              {},

	// Wave-4 (PHP) — Symfony String component DSL (post-receiver-strip
	// from `u('foo')->slug()->lower()` chains). The `u()` global helper
	// returns an AbstractString and the chain methods are unambiguous
	// Symfony String operations. PHP-gated so they don't shadow user
	// methods in other languages (#94 safer-bias rule). Names already
	// in stdlibBareNames or phpBareNames above are NOT duplicated.
	"u":              {},
	"slug":           {},
	"ascii":          {},
	"lower":          {},
	"upper":          {},
	"camel":          {},
	"snake":          {},
	"folded":         {},
	"truncate":       {},
	"wordwrap":       {},
	"padEnd":         {},
	"padStart":       {},
	"padBoth":        {},
	"trimStart":      {},
	"trimEnd":        {},
	"trimPrefix":     {},
	"trimSuffix":     {},
	"replaceMatches": {},
	"ignoreCase":     {},
	"containsAny":    {},
	"equalsTo":       {},
	"bytesAt":        {},
	"codePointsAt":   {},
	// AbstractString core API — names exist in JS/Kotlin/Swift maps
	// but each map is language-gated, so PHP needs its own entry.
	"length":     {},
	"startsWith": {},
	"endsWith":   {},
	"indexOf":    {},
	"repeat":     {},
	"toString":   {},
	"reverse":    {},
	"afterLast":  {},
	"before":     {},
	"beforeLast": {},

	// Symfony Mailer DSL — receiver-stripped from
	// `(new Email())->from(...)->to(...)->subject(...)->text(...)->html(...)`.
	"subject":        {},
	"htmlTemplate":   {},
	"textTemplate":   {},
	"replyTo":        {},
	"cc":             {},
	"bcc":            {},
	"priority":       {},
	"attach":         {},
	"attachFromPath": {},
	"embed":          {},
	"embedFromPath":  {},

	// Symfony HttpFoundation Request / Response accessors (receiver-
	// stripped from `$request->isMainRequest()`, `$request->getCharset()`).
	"isMainRequest":        {},
	"isMethod":             {},
	"isXmlHttpRequest":     {},
	"getCharset":           {},
	"getSchemeAndHttpHost": {},
	"getRequestUri":        {},
	"getQueryString":       {},
	"getPathInfo":          {},
	"getBaseUrl":           {},
	"getClientIp":          {},
	"getClientIps":         {},
	"getMethod":            {},
	"getRealMethod":        {},
	"getPreferredLanguage": {},
	"getLanguages":         {},
	"getLocale":            {},
	"setLocale":            {},
	"getSession":           {},
	"hasSession":           {},
	"getThrowable":         {},
	"setResponse":          {},
	"getResponse":          {},
	"getControllerResult":  {},
	"setControllerResult":  {},

	// Doctrine DataFixtures ReferenceRepository helpers (receiver-
	// stripped from `$this->addReference(...)` / `$this->getReference(...)`
	// in fixture loaders).
	"addReference":      {},
	"getReference":      {},
	"setReference":      {},
	"hasReference":      {},
	"getReferenceNames": {},

	// PHP stdlib (snake_case + array_* extras observed in symfony-demo
	// fixtures / repositories). Mirrors the existing array_* block.
	"mb_substr_count":    {},
	"array_pop":          {},
	"array_unshift":      {},
	"array_shift":        {},
	"array_push":         {},
	"array_reverse":      {},
	"array_chunk":        {},
	"array_count_values": {},
	"array_column":       {},
	"array_pad":          {},
	"array_fill_keys":    {},
	"array_replace":      {},

	// Symfony Validator / Form constraint class names (receiver bare
	// names from `new NotBlank()`, `new Length(['min' => 6])` inside
	// `getConstraints()` / form-builder closures).
	"NotBlank":           {},
	"NotNull":            {},
	"Length":             {},
	"Range":              {},
	"Regex":              {},
	"GreaterThan":        {},
	"LessThan":           {},
	"GreaterThanOrEqual": {},
	"LessThanOrEqual":    {},
	"Positive":           {},
	"PositiveOrZero":     {},
	"Negative":           {},
	"Choice":             {},
	"Url":                {},
	"Ip":                 {},
	"Uuid":               {},
	"Json":               {},
	"Type":               {},
	"Callback":           {},
	"Valid":              {},
	"All":                {},
	"Collection":         {},
	"Count":              {},
	"UniqueEntity":       {},

	// Symfony HttpFoundation response constructors + Symfony Form
	// constructor leaves observed as bare receivers in symfony-demo.
	"RedirectResponse":             {},
	"JsonResponse":                 {},
	"BinaryFileResponse":           {},
	"StreamedResponse":             {},
	"CollectionToArrayTransformer": {},
	"BufferedOutput":               {},
	"DoctrinePaginator":            {},
	"Paginator":                    {},
	"NullOutput":                   {},
	"ConsoleOutput":                {},

	// Wave-4 (PHP) pass-3 — Doctrine entity / Symfony Security
	// `User` accessor convention. After the PHP extractor strips the
	// receiver from `$user->getId()` / `$post->getAuthor()` the bare
	// camelCase getter lands at the resolver; the resolver can't bind
	// it back to the local entity class because the receiver type was
	// erased. These names are unambiguous Doctrine entity getters in
	// Symfony / Laravel codebases (every annotated entity emits them
	// via `make:entity`), and the PHP language gate keeps them from
	// shadowing user identifiers in JS/Go/Ruby/Python/etc.
	"getId":          {},
	"getUuid":        {},
	"getSlug":        {},
	"getTitle":       {},
	"getSummary":     {},
	"getContent":     {},
	"getBody":        {},
	"getAuthor":      {},
	"getAuthorEmail": {},
	"getPublishedAt": {},
	"getCreatedAt":   {},
	"getUpdatedAt":   {},
	"getTags":        {},
	"getComments":    {},
	"getPosts":       {},
	"getComment":     {},
	"getPost":        {},
	"getMember":      {},
	// User-entity convention (Symfony Security UserInterface impls).
	"getRoles":          {},
	"getSalt":           {},
	"getUserIdentifier": {},
	"eraseCredentials":  {},
	"hashPassword":      {},
	"getEmail":          {},
	"getFullName":       {},

	// Symfony Validator user-defined Validator helpers (residual from
	// symfony-demo's `src/Utils/Validator.php` invoked via
	// `$this->validator->validateX(...)` chains in tests/commands).
	// Bare leaf after extractor strip. Real local methods, but
	// receiver-type tracking is missing; PHP-gated keeps them from
	// shadowing user methods in other languages.
	"validateUsername": {},
	"validatePassword": {},
	"validateEmail":    {},
	"validateFullName": {},

	// Symfony Form DataTransformerInterface methods (residual from
	// symfony-demo TagArrayToStringTransformer tests).
	"reverseTransform": {},
	"transform":        {},

	// Wave-4 (PHP) pass-3 residual — additional Symfony helpers:
	// `to` / `from` (Mailer Email already covers `from` for callers
	// outside this map; `to` here is the chainable setter), `form`
	// (BrowserKit Crawler `$crawler->form()`), `generate` (Routing
	// UrlGenerator), `remove` (Doctrine EntityManager `$em->remove(
	// $entity)`), plus a handful of `get*` accessors on framework
	// types (BrowserKit Client, Symfony Console Application, Console
	// Tester, Doctrine Connection, HttpKernel ConsoleErrorEvent).
	"to":                         {},
	"form":                       {},
	"generate":                   {},
	"remove":                     {},
	"getName":                    {},
	"getUsername":                {},
	"getInput":                   {},
	"getOutput":                  {},
	"getDisplay":                 {},
	"getCommand":                 {},
	"getCommandName":             {},
	"getConnection":              {},
	"getDatabasePlatform":        {},
	"getCookieJar":               {},
	"getRequest":                 {},
	"getDuration":                {},
	"getMemory":                  {},
	"getPrevious":                {},
	"getCode":                    {},
	"getStartLine":               {},
	"getEndLine":                 {},
	"getFileName":                {},
	"getSourceContext":           {},
	"getPath":                    {},
	"getData":                    {},
	"getPayload":                 {},
	"getController":              {},
	"getLastUsername":            {},
	"getLastAuthenticationError": {},
	"getDQLPart":                 {},
	"isVerbose":                  {},
	"resolveTemplate":            {},
	"logout":                     {},
	"willReturn":                 {},
	"method":                     {},
	"getMock":                    {},

	// Slice-8 (PHP resolver wave) — Laravel Blueprint column/modifier
	// methods missing from the wave-3 list. Migration closures receive a
	// `$table` Blueprint and chain these methods; the PHP extractor
	// receiver-strips to the bare leaf. `integer` is the 32-bit column
	// type (analogous to `string`/`text` already listed); `foreignId` is
	// the `UNSIGNED BIGINT` helper for FK columns; `constrained` is the
	// chained FK constraint modifier; `index` is the explicit index
	// declaration. PHP-gated (issue #44 slice-8).
	"integer":     {},
	"foreignId":   {},
	"constrained": {},
	"index":       {},
	"hasTable":    {},
	"hasColumn":   {},
	"dropTable":   {},

	// Slice-8 (PHP resolver wave) — Eloquent Model lifecycle methods
	// forwarded through trait composition. Eloquent's `Model` class
	// uses PHP `use` traits (`HasAttributes`, `HasEvents`, `HasTimestamps`,
	// etc.) for `syncOriginal`, `initializeTraits`, `bootTraits`, `booted`,
	// `fireModelEvent`. The PHP extractor sees bare-name calls inside
	// `__construct` / `boot` and can't bind them to the trait-provided
	// method because trait resolution is invisible to static analysis.
	// These are unambiguous Eloquent internals (the `boot` + `booted`
	// pair is the Eloquent boot-cycle; `fireModelEvent` is the HasEvents
	// dispatch hook). PHP-gated — `booted` / `boot` do not collide with
	// user methods in Go/Ruby/Python/etc. under the php gate (issue #44).
	"bootTraits":       {},
	"booted":           {},
	"fireModelEvent":   {},
	"initializeTraits": {},
	"syncOriginal":     {},

	// Slice-8 (PHP resolver wave) — Laravel HTTP helpers receiver-stripped
	// from Laravel Response / Request / Query Builder chains. `noContent`
	// is the HTTP 204 factory on the Response facade (`response()->
	// noContent()`); `hasFile` is the Request file-upload check
	// (`$request->hasFile('avatar')`); `validate` is the fluent Validator
	// terminal (`Validator::make(...)->validate()`); `withErrors` is the
	// Redirector flash helper (`back()->withErrors([...])`);
	// `insertGetId` is the Query Builder single-row insert returning the
	// new PK. All are Laravel-specific enough to be safely PHP-gated under
	// the safer-bias rule (#94/#106). The HTTP verbs `get`/`post`/`put`
	// remain excluded per the issue #439 spec.
	"noContent":   {},
	"hasFile":     {},
	"validate":    {},
	"withErrors":  {},
	"insertGetId": {},
	"attempt":     {},
	"regenerate":  {},
	"invalidate":  {},
	"intended":    {},
}

// pythonBareNames is the Python-language-gated bare-name stop-list
// (issue #447). After the Python extractor strips the receiver from
// attribute calls (`User.objects.filter(...)` → `filter`,
// `self.save()` → `save`, `serializer.is_valid()` → `is_valid`), the
// resolver sees a bare identifier that can't be matched to a local
// entity and lands in bug-extractor. These names are Django ORM /
// QuerySet / Meta verbs, Django REST Framework view / serializer /
// permission class names, and Django admin DSL helpers — high-volume
// in django-realworld-style codebases (and django/DRF web apps
// generally) and gated to lang=="python" so a same-named user method
// in JS / Go / Ruby / etc. is not shadowed (#94 safer-bias rule).
//
// Names that already classify via stdlibBareNames (`filter`,
// `Response`) are NOT duplicated here — the global stop-list fires
// first regardless of language gate.
var pythonBareNames = map[string]struct{}{
	// Django ORM model field types (class names used in `models.Model`
	// subclass bodies; receiver-stripped from `models.CharField(...)`).
	"CharField":       {},
	"IntegerField":    {},
	"BooleanField":    {},
	"DateTimeField":   {},
	"DateField":       {},
	"TextField":       {},
	"ForeignKey":      {},
	"OneToOneField":   {},
	"ManyToManyField": {},
	"URLField":        {},
	"EmailField":      {},
	"SlugField":       {},
	"DecimalField":    {},
	"FloatField":      {},
	"BinaryField":     {},
	"JSONField":       {},
	"FileField":       {},
	"ImageField":      {},

	// Django ORM manager / QuerySet API. `objects` arrives bare from
	// `User.objects.filter(...)` after the receiver-strip; the verb
	// chain (`filter`, `exclude`, `get`, `annotate`, ...) likewise.
	// `filter` is already in stdlibBareNames (global) and is not
	// duplicated here.
	"objects":          {},
	"exclude":          {},
	"get":              {},
	"get_or_create":    {},
	"update_or_create": {},
	"create":           {},
	"save":             {},
	"delete":           {},
	"update":           {},
	"select_related":   {},
	"prefetch_related": {},
	"values":           {},
	"values_list":      {},
	"annotate":         {},
	"aggregate":        {},
	"count":            {},
	"exists":           {},
	"bulk_create":      {},
	"bulk_update":      {},
	"latest":           {},
	"earliest":         {},

	// Django Meta options — inner-class attribute names. They land at
	// the resolver as bare names when assignment-style declarations
	// `verbose_name = "..."` are reified by the extractor as USES
	// edges, and when `class Meta:` is referenced as a bare class.
	"Meta":                {},
	"verbose_name":        {},
	"verbose_name_plural": {},
	"ordering":            {},
	"unique_together":     {},
	"index_together":      {},
	"validators":          {},

	// Django REST Framework — serializer base classes and generic
	// view / viewset classes. `Response` is in stdlibBareNames
	// (global) and is not duplicated here.
	"ModelSerializer":              {},
	"Serializer":                   {},
	"ListAPIView":                  {},
	"RetrieveAPIView":              {},
	"CreateAPIView":                {},
	"UpdateAPIView":                {},
	"DestroyAPIView":               {},
	"ListCreateAPIView":            {},
	"RetrieveUpdateDestroyAPIView": {},
	"ModelViewSet":                 {},
	"ReadOnlyModelViewSet":         {},

	// DRF view / viewset attribute names + decorator + status module
	// leaf. `status` is the `rest_framework.status` module reference
	// (receiver-stripped from `status.HTTP_200_OK`) and `action` is
	// the `@action` decorator imported from `rest_framework.decorators`.
	"status":                 {},
	"action":                 {},
	"permission_classes":     {},
	"authentication_classes": {},
	"serializer_class":       {},
	"queryset":               {},

	// DRF permission classes (used as bare class refs in
	// `permission_classes = [IsAuthenticated, ...]`).
	"IsAuthenticated":           {},
	"IsAdminUser":               {},
	"AllowAny":                  {},
	"IsAuthenticatedOrReadOnly": {},

	// Django admin DSL. `register` / `unregister` are the
	// `admin.site.register(Model, Admin)` helpers receiver-stripped
	// to bare names; `site` is the bare `admin.site` reference.
	// The remaining names are `ModelAdmin` subclass attribute
	// declarations (`list_display = (...)`).
	"register":        {},
	"unregister":      {},
	"site":            {},
	"list_display":    {},
	"list_filter":     {},
	"search_fields":   {},
	"readonly_fields": {},
	"fieldsets":       {},

	// Issue #455 — Python stdlib + typing + framework DSL extension.
	// Pulled from real bug-extractor samples on click / flask /
	// flask-realworld (residuals after #420 / #423 / #446 / #447 were
	// 10–17%). All names below arrive at the resolver as bare
	// identifiers after the Python extractor strips the receiver from
	// attribute calls (`typing.cast(...)` → `cast`, `Path(...)` from
	// `from pathlib import Path` → `Path`, `pytest.raises(...)` →
	// `raises`, `app.route(...)` → `route`). Gated to lang=="python"
	// so a same-named user method in JS / Go / Ruby / etc. is not
	// shadowed (safer-bias rule #94). Names that already classify via
	// the global stdlibBareNames stop-list are NOT duplicated.
	//
	// Conservative selection rule: include names that are clearly
	// stdlib / well-known-package helpers in Python idiom. Excluded
	// even though present in samples: `write`, `read`, `close`,
	// `append`, `pop`, `keys`, `items`, `update`, `extend`, `remove`,
	// `replace`, `split`, `format`, `match`, `search`, `info`,
	// `debug`, `warning`, `error`, `warn`, `first`, `commit`, `run`,
	// `send`, `connect`, `execute`, `cls`, `func`, `f` — they collide
	// trivially with user-method identifiers, and the safer-bias rule
	// from #94 makes a missed external strictly better than a
	// synthesised placeholder shadowing a real user method. Reflection
	// builtins (`getattr` / `setattr` / `hasattr` / `delattr`) are
	// likewise excluded — they are dynamic-dispatch primitives, not
	// external imports, and are tagged DispositionDynamic upstream.

	// typing module — generic / annotation primitives. `Iterator` and
	// `Any` collide with rustBareNames and goChiRouterNames (which is
	// fine — the cross-language gate test below excludes them) and
	// are still included here so Python sources classify correctly.
	"cast":       {},
	"Optional":   {},
	"Union":      {},
	"Callable":   {},
	"Iterable":   {},
	"Iterator":   {},
	"Generator":  {},
	"TypeVar":    {},
	"Generic":    {},
	"Protocol":   {},
	"Awaitable":  {},
	"Sequence":   {},
	"Mapping":    {},
	"Annotated":  {},
	"Literal":    {},
	"Final":      {},
	"ClassVar":   {},
	"NewType":    {},
	"NamedTuple": {},
	"TypedDict":  {},
	"overload":   {},
	// `List`, `Dict`, `Tuple`, `Type`, `Set` are intentionally NOT
	// added — they are also the Python builtins (already classified
	// via stdlibBareNames as `list`/`dict`/`tuple`/`type`/`set`) and
	// adding the PascalCase typing aliases would conflict with the
	// `NoDuplicatesWithStdlibBareNames` invariant only if we cased
	// them identically; we omit them to keep the list narrow.

	// functools / itertools — closure + iteration helpers. `chain`
	// collides with rustBareNames; cross-lang gate excludes it.
	"update_wrapper":  {},
	"partial":         {},
	"wraps":           {},
	"lru_cache":       {},
	"cache":           {},
	"cached_property": {},
	"reduce":          {},
	"chain":           {},
	"islice":          {},
	"cycle":           {},
	"tee":             {},
	"starmap":         {},
	"takewhile":       {},
	"dropwhile":       {},
	"groupby":         {},
	"product":         {},
	"permutations":    {},
	"combinations":    {},

	// inspect / textwrap — introspection + doc helpers.
	"cleandoc":   {},
	"getsource":  {},
	"signature":  {},
	"isfunction": {},
	"isclass":    {},
	"ismethod":   {},
	"getmembers": {},
	"dedent":     {},
	"indent":     {},

	// pytest test-DSL — `raises`, `fixture`, `mark`, `parametrize`,
	// `monkeypatch`, `xfail` are Python testing idioms; receiver-
	// stripped from `pytest.raises(...)` / `@pytest.mark.parametrize`
	// / `monkeypatch.setattr(...)`. High volume in flask + click test
	// suites; safer than `skip` (too generic) so `skip` is excluded.
	"raises":      {},
	"fixture":     {},
	"mark":        {},
	"parametrize": {},
	"monkeypatch": {},
	"xfail":       {},

	// dataclasses — `dataclass` decorator + accessor helpers.
	// `replace` excluded as too collision-prone (str.replace,
	// list.replace, user replace methods).
	"dataclass":    {},
	"field":        {},
	"fields":       {},
	"asdict":       {},
	"astuple":      {},
	"is_dataclass": {},

	// pathlib — `Path` collides with rustBareNames; cross-lang gate
	// excludes it.
	"Path":            {},
	"PurePath":        {},
	"PurePosixPath":   {},
	"PureWindowsPath": {},

	// os / os.path / io stdlib helpers. `getcwd` / `listdir` /
	// `makedirs` already classify via stdlibBareNames and are NOT
	// duplicated. `path` excluded as too generic.
	"dirname":               {},
	"basename":              {},
	"abspath":               {},
	"realpath":              {},
	"expanduser":            {},
	"expandvars":            {},
	"splitext":              {},
	"fspath":                {},
	"fileno":                {},
	"mkdir":                 {},
	"getfilesystemencoding": {},

	// io / pytest capture helpers — `getvalue` is StringIO/BytesIO,
	// `readouterr` is pytest capsys. Both unambiguous Python idioms.
	"getvalue":   {},
	"readouterr": {},

	// logging — conservative pair only. `getLogger` + `basicConfig`
	// are unambiguous logging-module helpers; `info` / `debug` /
	// `warning` / `error` / `warn` are deliberately EXCLUDED because
	// they collide trivially with user field/method names.
	"getLogger":   {},
	"basicConfig": {},

	// Flask routing / app / request-context DSL. Receiver-stripped
	// from `app.route(...)` / `app.register_blueprint(...)` /
	// `current_app._get_current_object()`. `route` / `redirect` /
	// `flash` collide with other language maps (rust/swift/php/java/
	// kotlin); cross-lang gate excludes them. `query` collides with
	// swiftBareNames; cross-lang gate excludes it (kept for SQLA).
	"route":                        {},
	"register_blueprint":           {},
	"add_url_rule":                 {},
	"errorhandler":                 {},
	"as_view":                      {},
	"app_context":                  {},
	"test_request_context":         {},
	"test_client":                  {},
	"test_cli_runner":              {},
	"url_for":                      {},
	"jsonify":                      {},
	"init_app":                     {},
	"Markup":                       {},
	"_get_current_object":          {},
	"app_template_filter":          {},
	"app_template_test":            {},
	"add_template_filter":          {},
	"add_template_test":            {},
	"template_global":              {},
	"template_filter":              {},
	"template_test":                {},
	"make_response":                {},
	"redirect":                     {},
	"send_file":                    {},
	"send_from_directory":          {},
	"abort":                        {},
	"flash":                        {},
	"stream_with_context":          {},
	"copy_current_request_context": {},

	// Click CLI test-runner + DSL. `invoke` dominates the click
	// bug-extractor sample (~300 hits) from `runner.invoke(cli, ...)`
	// in click's own test suite. Gating to lang=="python" + the
	// Python idiom dominance keeps the collision risk acceptable
	// (the safer-bias trade-off from #94).
	"invoke":               {},
	"isolated_filesystem":  {},
	"get_help_record":      {},
	"get_help_extra":       {},
	"make_context":         {},
	"get_parameter_source": {},
	"call_on_close":        {},
	"lookup_default":       {},
	"get_default":          {},

	// SQLAlchemy ORM — `filter_by` / `create_all` / `drop_all` /
	// `query` are unambiguous SQLAlchemy idioms receiver-stripped
	// from `Model.query.filter_by(...)` / `db.create_all()`. `first`
	// / `commit` / `rollback` / `add` excluded as too generic.
	"filter_by":  {},
	"create_all": {},
	"drop_all":   {},
	"query":      {},

	// Wave-4 (Django + Flask realworld). Pulled from real bug-extractor
	// samples on django-realworld + flask-realworld at 13.1% / 13.8%
	// pre-wave. All names below arrive at the resolver as bare
	// identifiers after the Python extractor strips the receiver from
	// attribute calls. Gated to lang=="python" so user methods in
	// other languages are not shadowed (#94 safer-bias rule).
	// `first` deliberately EXCLUDED — collides with rubyBareNames /
	// swiftBareNames; the cross-lang gate test forbids duplicates.
	"is_valid":                 {},
	"is_authenticated":         {},
	"set_password":             {},
	"check_password":           {},
	"slugify":                  {},
	"isoformat":                {},
	"strftime":                 {},
	"exception_handler":        {},
	"get_authorization_header": {},
	"create_user":              {},
	"create_superuser":         {},
	"follow":                   {},
	"unfollow":                 {},
	"favorite":                 {},
	"unfavorite":               {},
	"favourite":                {},
	"unfavourite":              {},
	"is_following":             {},
	"has_favorited":            {},
	"is_favourite":             {},
	"post_json":                {},
	"put_json":                 {},
	"create_access_token":      {},
	"generate_password_hash":   {},
	"check_password_hash":      {},
	"from_object":              {},
	"iter_rules":               {},
	"add_command":              {},
	"get_by_id":                {},
	"add_tag":                  {},
	"remove_tag":               {},
	"user_not_found":           {},
	"article_not_found":        {},
	"user_already_registered":  {},

	// Wave-4 pandas / numpy bare-name helpers. Pulled from the
	// pandas bug-extractor sample (top stubs: asarray=196, astype=195,
	// is_list_like=145, find_stack_level=109, is_integer=99, is_scalar=88,
	// arange=80, empty=75, zeros=74). Each name below is overwhelmingly
	// a numpy/pandas idiom rather than a plausible user method.
	// Generic verbs (`append`, `pop`, `items`, `extend`, `remove`,
	// `replace`, `view`, `dtype`, `func`, `cls`, `op`, `equals`,
	// `reindex`, `Index`, `Series`, `DataFrame`) excluded — too
	// collision-prone or duplicates of other allowlists.
	"asarray":                    {},
	"astype":                     {},
	"is_list_like":               {},
	"is_scalar":                  {},
	"is_integer":                 {},
	"is_float":                   {},
	"is_np_dtype":                {},
	"is_bool":                    {},
	"is_object":                  {},
	"is_string_dtype":            {},
	"is_numeric_dtype":           {},
	"is_datetime64_dtype":        {},
	"arange":                     {},
	"zeros":                      {},
	"ones":                       {},
	"empty_like":                 {},
	"zeros_like":                 {},
	"ones_like":                  {},
	"linspace":                   {},
	"reshape":                    {},
	"ravel":                      {},
	"concatenate":                {},
	"to_numpy":                   {},
	"isnan":                      {},
	"isfinite":                   {},
	"isinf":                      {},
	"find_stack_level":           {},
	"validate_bool_kwarg":        {},
	"ensure_platform_int":        {},
	"ensure_int64":               {},
	"ensure_object":              {},
	"import_optional_dependency": {},
	"construct_array_type":       {},
	"AbstractMethodError":        {},
	"Timestamp":                  {},
	"Timedelta":                  {},
	"NaT":                        {},
	"NA":                         {},
	"DatetimeIndex":              {},
	"PeriodIndex":                {},
	"TimedeltaIndex":             {},
	"DateOffset":                 {},
	"chunked_array":              {},
	"_simple_new":                {},
	"_from_sequence":             {},
	"_constructor":               {},
	"putmask":                    {},
	"BlockPlacement":             {},
	"get_loc":                    {},
	"get_indexer":                {},

	// pandas wave (post-wave-4 residual at 9.80%, target <=5%).
	// PyArrow ChunkedArray / Array / scalar receiver-strips — pandas-Arrow
	// bridge surface in pandas/core/arrays/arrow/*.py. These arrive bare
	// after `pa_array.is_null()` → `is_null`, `scalar.as_py()` → `as_py`,
	// `arr.combine_chunks()` → `combine_chunks` etc. All distinctive
	// pyarrow idioms (no plausible user-method collision in python).
	"is_timestamp":      {},
	"is_duration":       {},
	"is_nan_na":         {},
	"is_large_string":   {},
	"is_large_binary":   {},
	"is_decimal":        {},
	"is_dictionary":     {},
	"is_pdna_or_none":   {},
	"as_py":             {},
	"as_unit":           {},
	"combine_chunks":    {},
	"iterchunks":        {},
	"fill_null":         {},
	"replace_with_mask": {},
	"to_pandas_dtype":   {},
	"type_for_alias":    {},
	"negate_checked":    {},
	"pyarrow_meth":      {},
	"remask":            {},
	// pyarrow type-test predicates — `is_string`, `is_floating`,
	// `is_boolean`, `is_date`, `is_time`, `is_null`, `is_nan` collide
	// with possible user-method names but the python gate keeps the
	// allowlist scoped to python-only resolution, so this only shadows
	// receiver-stripped same-named python user methods. Acceptable
	// trade per wave-7 safer-bias rule (high pandas/pyarrow signal).
	"is_string":   {},
	"is_floating": {},
	"is_boolean":  {},
	"is_date":     {},
	"is_time":     {},
	"is_null":     {},
	"is_nan":      {},
	// pyarrow scalar constructors / dtype names — distinctive numeric-
	// dtype names not in numpy allowlist yet.
	"int32":     {},
	"int64":     {},
	"duration":  {},
	"timestamp": {},
	// pandas-internal helper functions surfaced as `cls`-receiver bare
	// names from `cls._from_sequence(...)` chains. Wave-4 added
	// `_simple_new`/`_from_sequence`/`_constructor`; rest below are
	// other distinctive `_<helper>` patterns from pandas/core internals
	// (underscore-prefix + pandas-only naming → safe).
	"_gotitem":                          {},
	"_get_axis_number":                  {},
	"maybe_dispatch_ufunc_to_dunder_op": {},
	// pandas error class — receiver-stripped from `errors.SpecificationError`.
	"SpecificationError": {},
	// pandas internal `pprint_thing` helper from io/formats.
	"pprint_thing": {},

	// pandas wave pass-2 — additional pyarrow.compute (pc.*) function
	// surface receiver-stripped from `pc.is_temporal(arr)`,
	// `pc.equal(a, b)`, `pc.dictionary_encode(x)` etc across
	// pandas/core/arrays/arrow/*. All names below are distinctive
	// pyarrow.compute identifiers — generic verbs (`equal`, `divide`,
	// `multiply`, `keys`, `items`, `apply`, `append`, `pop`, `length`,
	// `func`, `op`, `cls`, `view`, `transform`, `search`, `repeat`,
	// `wrap`, `pop`, `empty`, `count`, `quantile`, `value_counts`,
	// `partition`, `quarter`, `hour`, `minute`, `second`, `day`,
	// `month`, `year`) deliberately EXCLUDED — too collision-prone.
	// `if_else`, `is_in`, `index_in` deliberately included — distinctive.
	"if_else":               {},
	"is_temporal":           {},
	"is_binary":             {},
	"is_list":               {},
	"is_large_list":         {},
	"is_fixed_size_list":    {},
	"is_fixed_size_binary":  {},
	"is_struct":             {},
	"is_map":                {},
	"is_date64":             {},
	"is_leap_year":          {},
	"is_string_array":       {},
	"large_string":          {},
	"floor_temporal":        {},
	"ceil_temporal":         {},
	"days_between":          {},
	"local_timestamp":       {},
	"dictionary_encode":     {},
	"concat_arrays":         {},
	"array_sort_indices":    {},
	"to_pylist":             {},
	"not_equal":             {},
	"less_equal":            {},
	"greater_equal":         {},
	"or_kleene":             {},
	"fill_null_backward":    {},
	"fill_null_forward":     {},
	"drop_null":             {},
	"struct_field":          {},
	"list_flatten":          {},
	"list_value_length":     {},
	"split_pattern":         {},
	"count_substring_regex": {},
	"binary_repeat":         {},
	"binary_join":           {},
	"divide_checked":        {},
	"sqrt_checked":          {},
	"pairwise_diff_checked": {},
	"utf8_capitalize":       {},
	"utf8_split_whitespace": {},
	"utf8_normalize":        {},
	"utf8_zfill":            {},
	"index_in":              {},
	"is_in":                 {},
	"from_numpy_dtype":      {},
	"from_arrays":           {},
	"iso_calendar":          {},
	"pa_contains":           {},
	"_safe_fill_null":       {},
	"_box_pa_array":         {},
	"__arrow_array__":       {},
	"maybe_convert_objects": {},
	"maybe_get_tz":          {},
	"infer_dtype":           {},
	"validate_na_arg":       {},
	"to_pydatetime":         {},
	"to_pytimedelta":        {},
	"to_offset":             {},
	"time64":                {},
	"uint64":                {},
	"bool_":                 {},
	"list_":                 {},
	"and_":                  {},
	"or_":                   {},
	"invert":                {},
	"rounding_method":       {},
	"regex_parser":          {},
	"has_unsupported_code":  {},

	// pandas wave pass-3 — additional pyarrow.compute / numpy /
	// warnings / pandas-internal helpers from post-pass-2 residual.
	// `warn` (warnings.warn), `filterwarnings`, `catch_warnings`,
	// `errstate` (numpy.errstate) — distinctive stdlib/numpy idioms.
	"filterwarnings":          {},
	"catch_warnings":          {},
	"errstate":                {},
	"utf8_slice_codeunits":    {},
	"utf8_length":             {},
	"string_is_ascii":         {},
	"starts_with":             {},
	"ends_with":               {},
	"is_monotonic":            {},
	"add_tmp":                 {},
	"has_reference":           {},
	"get_window_bounds":       {},
	"_consolidate_inplace":    {},
	"DataError":               {},
	"map_infer_mask":          {},
	"homogeneous_func":        {},
	"validate_stat_ddof_func": {},
	"scalar_fillna_inplace":   {},
	"tz_convert":              {},
	"tile":                    {},
	"stringify":               {},
	"argsort":                 {},
	"pc_func":                 {},

	// pandas wave-2 pass-1 — pandas internal API surface + numpy/scipy
	// distinctive helpers from post-wave-1 residual (bug-rate 7.48%).
	//
	// Two buckets:
	//   (a) Pandas single-underscored internal methods (`_<lowercase>`
	//       distinctive compound names) routinely called on self / on
	//       pandas DataFrame / Series / Index / ArrayLike instances —
	//       receiver-stripped by the Python extractor, lands as a bare
	//       identifier. Highly distinctive of pandas internal API
	//       (no plausible user-method collision in non-pandas code).
	//   (b) numpy / scipy / pandas C-extension helper functions called
	//       directly as `np.<func>` / `pd.<func>` / cython-impl helpers
	//       like `array_to_datetime`, `ensure_int64`, `period_ordinal`,
	//       `validate_argmax`, `intersect1d`. These are numpy / pandas
	//       Cython surface area, distinctive enough to allowlist.
	//
	// Gated to lang=="python" (#94 safer-bias rule).
	// Generic verbs (`view`/`func`/`cls`/`equals`/`reindex`/`op`/
	// `append`/`extend`/`dtype`/`keys`/`items`/`empty`/`drop`/`copy`)
	// remain EXCLUDED — receiver-type tracking primitive (#494) will
	// handle those when available.
	"_addsub_int_array_or_scalar":          {},
	"_align_frame":                         {},
	"_align_series":                        {},
	"_apply_array":                         {},
	"_arrow_dtype_mapping":                 {},
	"_box_func":                            {},
	"_box_pa_scalar":                       {},
	"_can_hold_identifiers_and_holds_name": {},
	"_cast_pointwise_result":               {},
	"_categories_match_up_to_permutation":  {},
	"_check_comparison_types":              {},
	"_check_copy_deprecation":              {},
	"_check_dtype_match":                   {},
	"_check_label_or_level_ambiguity":      {},
	"_check_timedeltalike_freq_compat":     {},
	"_clip_with_one_bound":                 {},
	"_coerce_to_array":                     {},
	"_concat_same_type":                    {},
	"_construct_axes_dict":                 {},
	"_construct_result":                    {},
	"_constructor_expanddim":               {},
	"_constructor_from_mgr":                {},
	"_constructor_sliced":                  {},
	"_convert_level_number":                {},
	"_convert_slice_indexer":               {},
	"_convert_to_multiindex":               {},
	"_create_arithmetic_method":            {},
	"_create_comparison_method":            {},
	"_create_delegator_method":             {},
	"_create_delegator_property":           {},
	"_create_logical_method":               {},
	"_create_method":                       {},
	"_cython_op_ndim_compat":               {},
	"_cython_operation":                    {},
	"_drop_axis":                           {},
	"_drop_labels_or_levels":               {},
	"_drop_level_numbers":                  {},
	"_dt_tz_convert":                       {},
	"_dt_tz_localize":                      {},
	"_empty":                               {},
	"_encode_with_my_categories":           {},
	"_ensure_array":                        {},
	"_ensure_matching_resos":               {},
	"_ensure_simple_new_inputs":            {},
	"_eval_expression":                     {},
	"_explode":                             {},
	"_extract_level_codes":                 {},
	"_format_data":                         {},
	"_format_flat":                         {},
	"_format_native_types":                 {},
	"_from_arrays":                         {},
	"_from_backing_data":                   {},
	"_from_categorical_dtype":              {},
	"_from_datetime64":                     {},
	"_from_fastpath":                       {},
	"_from_fields":                         {},
	"_from_mgr":                            {},
	"_from_ordinal":                        {},
	"_from_pyarrow_array":                  {},
	"_from_sequence_not_strict":            {},
	"_from_sequence_of_strings":            {},
	"_from_value_and_reso":                 {},
	"_from_values_or_dtype":                {},
	"_set_axis_name":                       {},
	"_set_axis_nocheck":                    {},
	"_set_categories":                      {},
	"_set_codes":                           {},
	"_set_grouper":                         {},
	"_set_levels":                          {},
	"_set_names":                           {},
	"_set_value":                           {},
	"_slice":                               {},
	"_slice_take_blocks_ax0":               {},
	"_sort_levels_monotonic":               {},
	"_split":                               {},
	"_split_op_result":                     {},
	"_str_capitalize":                      {},
	"_str_casefold":                        {},
	"_str_contains":                        {},
	"_str_count":                           {},
	"_str_encode":                          {},
	"_str_endswith":                        {},
	"_str_extract":                         {},
	"_str_find":                            {},
	"_str_findall":                         {},
	"_str_fullmatch":                       {},
	"_str_get":                             {},
	"_str_get_dummies":                     {},
	"_str_getitem":                         {},
	"_str_index":                           {},
	"_str_isalnum":                         {},
	"_str_isalpha":                         {},
	"_str_isascii":                         {},
	"_str_isdecimal":                       {},
	"_str_isdigit":                         {},
	"_str_islower":                         {},
	"_str_isnumeric":                       {},
	"_str_isspace":                         {},
	"_str_istitle":                         {},
	"_str_isupper":                         {},
	"_str_join":                            {},
	"_str_len":                             {},
	"_str_lower":                           {},
	"_str_lstrip":                          {},
	"_str_map":                             {},
	"_str_match":                           {},
	"_str_normalize":                       {},
	"_str_pad":                             {},
	"_str_partition":                       {},
	"_str_removeprefix":                    {},
	"_str_removesuffix":                    {},
	"_str_repeat":                          {},
	"_str_replace":                         {},
	"_str_rfind":                           {},
	"_str_rindex":                          {},
	"_str_rpartition":                      {},
	"_str_rsplit":                          {},
	"_str_rstrip":                          {},
	"_str_slice":                           {},
	"_str_slice_replace":                   {},
	"_str_split":                           {},
	"_str_startswith":                      {},
	"_str_strip":                           {},
	"_str_swapcase":                        {},
	"_str_title":                           {},
	"_str_translate":                       {},
	"_str_upper":                           {},
	"_str_wrap":                            {},
	"_str_zfill":                           {},
	"_to_bool_indexer":                     {},
	"_to_datetimearray":                    {},
	"_to_numpy_and_type":                   {},
	"_to_timedeltaarray":                   {},
	"_to_timestamp_freq":                   {},
	"_transform_index":                     {},
	"_tz_convert":                          {},
	"_tz_localize":                         {},
	"_unbox_scalar":                        {},
	"_unstack":                             {},
	"_update_from_sliced":                  {},
	"_validate":                            {},
	"_validate_can_reindex":                {},
	"_validate_codes_for_dtype":            {},
	"_validate_dtype":                      {},
	"_validate_fill_value":                 {},
	"_validate_frequency":                  {},
	"_validate_listlike":                   {},
	"_validate_positional_slice":           {},
	"_validate_scalar":                     {},
	"_validate_setitem_value":              {},
	"_view":                                {},
	"_where":                               {},
	"_with_freq":                           {},
	"_with_infer":                          {},
	"allclose":                             {},
	"apply_along_axis":                     {},
	"apply_defaults":                       {},
	"argmax":                               {},
	"argmin":                               {},
	"around":                               {},
	"array_class":                          {},
	"array_equal":                          {},
	"array_equivalent_bytes":               {},
	"array_equivalent_float":               {},
	"array_equivalent_object":              {},
	"array_op":                             {},
	"array_strptime":                       {},
	"array_to_datetime":                    {},
	"array_to_datetime_with_tz":            {},
	"array_to_timedelta64":                 {},
	"asanyarray":                           {},
	"asof_locs":                            {},
	"astype_overflowsafe":                  {},
	"atleast_1d":                           {},
	"atleast_2d":                           {},
	"bincount":                             {},
	"broadcast_to":                         {},
	"buffers":                              {},
	"build_codes":                          {},
	"build_isocalendar_sarray":             {},
	"byteswap":                             {},
	"can_cast":                             {},
	"cast_from_unit_vectorized":            {},
	"check_all_hashable":                   {},
	"check_dtype_backend":                  {},
	"check_int_infer_dtype":                {},
	"check_len":                            {},
	"check_str_or_none":                    {},
	"column_looper":                        {},
	"column_names":                         {},
	"column_setitem":                       {},
	"convert_indexer":                      {},
	"convert_listlike":                     {},
	"convert_nans_to_NA":                   {},
	"convert_values":                       {},
	"coo_matrix":                           {},
	"corrcoef":                             {},
	"cython_operation":                     {},
	"datetime_data":                        {},
	"default_rng":                          {},
	"ensure_float64":                       {},
	"ensure_int16":                         {},
	"ensure_int32":                         {},
	"ensure_int8":                          {},
	"ensure_string_array":                  {},
	"ensure_uint64":                        {},
	"expand_dims":                          {},
	"extract_ordinals":                     {},
	"extract_period_unit":                  {},
	"extract_regex":                        {},
	"fabs":                                 {},
	"finfo":                                {},
	"flatnonzero":                          {},
	"fromarrays":                           {},
	"frombuffer":                           {},
	"fromiter":                             {},
	"frompyfunc":                           {},
	"full":                                 {},
	"full_outer_join":                      {},
	"getmaskarray":                         {},
	"group_first_last_indexer":             {},
	"group_quantile":                       {},
	"group_shift_indexer":                  {},
	"grouped_reduce":                       {},
	"guess_datetime_format":                {},
	"hash_klass":                           {},
	"hash_object_array":                    {},
	"hashtable":                            {},
	"hstack":                               {},
	"iinfo":                                {},
	"indexer_at_time":                      {},
	"indexer_between_time":                 {},
	"infer_freq":                           {},
	"infer_objects":                        {},
	"infer_tzinfo":                         {},
	"inner_join":                           {},
	"inner_join_indexer":                   {},
	"integer_op_not_supported":             {},
	"interp1d":                             {},
	"intersect1d":                          {},
	"intp":                                 {},
	"ints_to_pydatetime":                   {},
	"ints_to_pytimedelta":                  {},
	"iscomplexobj":                         {},
	"isleapyear_arr":                       {},
	"issubdtype":                           {},
	"jit":                                  {},
	"jitted_udf":                           {},
	"kendalltau":                           {},
	"left_join_indexer":                    {},
	"left_join_indexer_unique":             {},
	"left_outer_join":                      {},
	"maybe_booleans_to_slice":              {},
	"maybe_callable":                       {},
	"maybe_convert_numeric":                {},
	"maybe_indices_to_slice":               {},
	"maybe_lift":                           {},
	"maybe_mi_droplevels":                  {},
	"maybe_reorder":                        {},
	"min_scalar_type":                      {},
	"modf":                                 {},
	"nb_compat_func":                       {},
	"nb_func":                              {},
	"nb_looper":                            {},
	"neg":                                  {},
	"newbyteorder":                         {},
	"nextafter":                            {},
	"nonzero":                              {},
	"np_func":                              {},
	"outer_join_indexer":                   {},
	"pd_array":                             {},
	"pd_eval":                              {},
	"pd_op":                                {},
	"period_array_strftime":                {},
	"period_asfreq_arr":                    {},
	"period_ordinal":                       {},
	"promote_types":                        {},
	"setdiff1d":                            {},
	"signbit":                              {},
	"sliding_window_view":                  {},
	"spearmanr":                            {},
	"standard_normal":                      {},
	"tz_compare":                           {},
	"tz_convert_from_utc":                  {},
	"tz_localize":                          {},
	"tz_localize_to_utc":                   {},
	"tz_standardize":                       {},
	"union1d":                              {},
	"unique1d":                             {},
	"validate_all":                         {},
	"validate_any":                         {},
	"validate_argmax":                      {},
	"validate_argmax_with_skipna":          {},
	"validate_argmin":                      {},
	"validate_argsort":                     {},
	"validate_argsort_with_ascending":      {},
	"validate_ascending":                   {},
	"validate_clip_with_axis":              {},
	"validate_cum_func_with_skipna":        {},
	"validate_cumsum":                      {},
	"validate_end_alias":                   {},
	"validate_endpoints":                   {},
	"validate_func":                        {},
	"validate_groupby_func":                {},
	"validate_inclusive":                   {},
	"validate_insert_loc":                  {},
	"validate_limit":                       {},
	"validate_logical_func":                {},
	"validate_max":                         {},
	"validate_mean":                        {},
	"validate_median":                      {},
	"validate_min":                         {},
	"validate_minmax_axis":                 {},
	"validate_percentile":                  {},
	"validate_prod":                        {},
	"validate_repeat":                      {},
	"validate_round":                       {},
	"validate_sum":                         {},
	"validate_take":                        {},
	"validate_transpose":                   {},
	"vstack":                               {},

	// pandas wave-2 pass-2 — additional pandas internal API surface
	// from post-pass-1 residual (bug-rate 6.17%).
	//
	// Same two buckets as pass-1: (a) more pandas single-underscored
	// internals (`_get_*`, `_maybe_*`, `_reindex_*`, `_replace_*`,
	// `_should_*`, `_to_*`, `_values_for_*`), and (b) numpy/pandas
	// C-extension compound helpers (`get_period_field_arr`,
	// `period_ordinal`, `is_lexsorted`, `validate_*`, `utf8_is_*`,
	// `roll_mean`/`roll_sum`/`roll_var`, `nan*`, `numba_*_func`).
	// Generic verbs (#94) remain excluded.
	"_accumulate":                        {},
	"_add_delegate_accessors":            {},
	"_all_key":                           {},
	"_append_internal":                   {},
	"_assert_tzawareness_compat":         {},
	"_call_with_func":                    {},
	"_cov":                               {},
	"_cumcount_array":                    {},
	"_dict_round":                        {},
	"_dtype_to_subclass":                 {},
	"_get_agg_axis":                      {},
	"_get_axis":                          {},
	"_get_axis_name":                     {},
	"_get_block_manager_axis":            {},
	"_get_bool_data":                     {},
	"_get_codes_for_sorting":             {},
	"_get_common_dtype":                  {},
	"_get_cython_function":               {},
	"_get_daily_offset_mask":             {},
	"_get_data":                          {},
	"_get_data_subset":                   {},
	"_get_data_subset_indices":           {},
	"_get_default_index_names":           {},
	"_get_dtype_mapping":                 {},
	"_get_engine_target":                 {},
	"_get_formatted_values":              {},
	"_get_grouper":                       {},
	"_get_indexer":                       {},
	"_get_indexer_strict":                {},
	"_get_item":                          {},
	"_get_join_target":                   {},
	"_get_label_or_level_values":         {},
	"_get_leaf_sorter":                   {},
	"_get_level_number":                  {},
	"_get_level_values":                  {},
	"_get_loc_level":                     {},
	"_get_numeric_data":                  {},
	"_get_period_bins":                   {},
	"_get_plot_backend":                  {},
	"_get_reconciled_name_object":        {},
	"_get_resampler":                     {},
	"_get_resampler_for_grouping":        {},
	"_get_splitter":                      {},
	"_get_start_end_field":               {},
	"_get_time_bins":                     {},
	"_get_time_delta_bins":               {},
	"_get_time_micros":                   {},
	"_get_to_timestamp_base":             {},
	"_get_value":                         {},
	"_getframe":                          {},
	"_getitem_slice":                     {},
	"_getvalue":                          {},
	"_groupby_op":                        {},
	"_groupby_quantile":                  {},
	"_has_no_reference":                  {},
	"_hash_pandas_object":                {},
	"_import_from_c_capsule":             {},
	"_indexed_same":                      {},
	"_int64_cut_off":                     {},
	"_internal_get_values":               {},
	"_intersection_unique":               {},
	"_is_label_reference":                {},
	"_is_level_reference":                {},
	"_is_lexsorted":                      {},
	"_is_scipy_sparse":                   {},
	"_is_tick_like":                      {},
	"_iset_item":                         {},
	"_iter_column_arrays":                {},
	"_ixs":                               {},
	"_left_indexer":                      {},
	"_left_indexer_unique":               {},
	"_local_timestamps":                  {},
	"_make_mask_from_positional_indexer": {},
	"_map_values":                        {},
	"_mask_selected_obj":                 {},
	"_maybe_align_series_as_frame":       {},
	"_maybe_check_unique":                {},
	"_maybe_combine":                     {},
	"_maybe_convert":                     {},
	"_maybe_convert_datelike_array":      {},
	"_maybe_convert_freq":                {},
	"_maybe_copy":                        {},
	"_maybe_copy_array_input":            {},
	"_maybe_mask_results":                {},
	"_maybe_to_slice":                    {},
	"_maybe_unwrap":                      {},
	"_merger":                            {},
	"_mode":                              {},
	"_nth":                               {},
	"_pad_or_backfill":                   {},
	"_parse_dtype_strict":                {},
	"_parse_subtype":                     {},
	"_parse_temporal_dtype_string":       {},
	"_pin_freq":                          {},
	"_prep_index":                        {},
	"_putmask":                           {},
	"_python_apply_general":              {},
	"_quantile":                          {},
	"_raise_scalar_data_error":           {},
	"_rank":                              {},
	"_reconstruct":                       {},
	"_reduce":                            {},
	"_reduce_axis1":                      {},
	"_reindex_indexer":                   {},
	"_reindex_with_indexers":             {},
	"_rename":                            {},
	"_reorder_ilevels":                   {},
	"_replace_coerce":                    {},
	"_repr_categories":                   {},
	"_reset_cache":                       {},
	"_reset_identity":                    {},
	"_reverse_indexer":                   {},
	"_safe_cast":                         {},
	"_series_round":                      {},
	"_setitem_with_indexer":              {},
	"_shallow_copy":                      {},
	"_should_compare":                    {},
	"_standardize_dtype":                 {},
	"_values_for_argsort":                {},
	"_values_for_factorize":              {},
	"_values_for_json":                   {},
	"_verify_integrity":                  {},
	"fast_multiget":                      {},
	"fast_path":                          {},
	"fast_unique_multiple_list_gen":      {},
	"fast_xs":                            {},
	"fast_zip":                           {},
	"ffill_indexer":                      {},
	"fill_bool":                          {},
	"filter_func":                        {},
	"find_substring":                     {},
	"first_non_null":                     {},
	"fix_missing_locations":              {},
	"format_array":                       {},
	"format_array_from_datetime":         {},
	"format_object_summary":              {},
	"format_percentiles":                 {},
	"freq_to_dtype_code":                 {},
	"from_array":                         {},
	"from_attrname":                      {},
	"from_breaks":                        {},
	"from_buffers":                       {},
	"from_calendar_ordinals":             {},
	"from_codes":                         {},
	"from_pandas":                        {},
	"from_product":                       {},
	"from_series":                        {},
	"from_storage":                       {},
	"from_tuples":                        {},
	"generate_bins_dt64":                 {},
	"generate_slices":                    {},
	"generate_tokens":                    {},
	"get_buffers":                        {},
	"get_chunks":                         {},
	"get_column_by_name":                 {},
	"get_concat_blkno_indexers":          {},
	"get_console_size":                   {},
	"get_constant":                       {},
	"get_converter":                      {},
	"get_count":                          {},
	"get_dataframe_repr_params":          {},
	"get_date_field":                     {},
	"get_date_name_field":                {},
	"get_dtypes":                         {},
	"get_field_index":                    {},
	"get_fill_indexer":                   {},
	"get_format_datetime64":              {},
	"get_format_timedelta64":             {},
	"get_group_levels":                   {},
	"get_handle":                         {},
	"get_indexer_for":                    {},
	"get_indexer_non_unique":             {},
	"get_iterator":                       {},
	"get_kind_from_how":                  {},
	"get_labels_groupby":                 {},
	"get_level_sorter":                   {},
	"get_level_values":                   {},
	"get_loc_level":                      {},
	"get_locs":                           {},
	"get_median":                         {},
	"get_name":                           {},
	"get_new_columns":                    {},
	"get_new_values":                     {},
	"get_numeric_data":                   {},
	"get_period_alias":                   {},
	"get_period_field_arr":               {},
	"get_reindexed_values":               {},
	"get_reso_from_freqstr":              {},
	"get_resolution":                     {},
	"get_result_as_array":                {},
	"get_reverse_indexer":                {},
	"get_rows_with_mask":                 {},
	"get_series_repr_params":             {},
	"get_slice":                          {},
	"get_start_end_field":                {},
	"get_supported_dtype":                {},
	"get_timedelta_days":                 {},
	"get_timedelta_field":                {},
	"get_unique_dtypes":                  {},
	"get_unit_for_round":                 {},
	"get_unit_from_dtype":                {},
	"get_values":                         {},
	"get_var_names":                      {},
	"get_version":                        {},
	"get_versions":                       {},
	"getitem_block_columns":              {},
	"make_block_same_class":              {},
	"make_mask_object_ndarray":           {},
	"map_index":                          {},
	"map_infer":                          {},
	"map_locations":                      {},
	"maximum":                            {},
	"melt_stub":                          {},
	"memory_usage":                       {},
	"memory_usage_of_objects":            {},
	"merge_pieces":                       {},
	"minimum":                            {},
	"minute":                             {},
	"nan_func":                           {},
	"nancorr_spearman":                   {},
	"nanmax":                             {},
	"nanmin":                             {},
	"nanosecond":                         {},
	"new_categories":                     {},
	"new_child":                          {},
	"normalize_i8_timestamps":            {},
	"numba_agg_func":                     {},
	"numba_func":                         {},
	"numba_transform_func":               {},
	"object_getattr_string":              {},
	"object_hash":                        {},
	"object_setattr_string":              {},
	"pa_pad":                             {},
	"pad_2d_inplace":                     {},
	"pad_inplace":                        {},
	"pad_or_backfill":                    {},
	"parse_datetime_string_with_reso":    {},
	"parse_timedelta_unit":               {},
	"periodarr_to_dt64arr":               {},
	"periods_per_day":                    {},
	"periods_per_second":                 {},
	"register_extension_type":            {},
	"register_jitable":                   {},
	"register_matplotlib_converters":     {},
	"reindex_axis":                       {},
	"reindex_indexer":                    {},
	"reindex_like":                       {},
	"rename_axis":                        {},
	"rename_categories":                  {},
	"reorder_categories":                 {},
	"reorder_levels":                     {},
	"replace_list":                       {},
	"rewrite_exception":                  {},
	"rewrite_warning":                    {},
	"roll_mean":                          {},
	"roll_sum":                           {},
	"roll_var":                           {},
	"run_ewm":                            {},
	"scalar_binop":                       {},
	"scalar_compare":                     {},
	"scalar_fillna_2d_inplace":           {},
	"scalar_kurt":                        {},
	"scalar_skew":                        {},
	"slice_block_columns":                {},
	"slice_indexer":                      {},
	"slice_len":                          {},
	"slice_locs":                         {},
	"sort_index":                         {},
	"sort_indices":                       {},
	"sort_values":                        {},
	"sparse_op":                          {},
	"swapaxes":                           {},
	"swapkey":                            {},
	"swaplevel":                          {},
	"take_block_columns":                 {},
	"take_func":                          {},
	"tile_for_unstack":                   {},
	"to_2d_mgr":                          {},
	"to_array":                           {},
	"to_block_index":                     {},
	"to_datetime64":                      {},
	"to_dense":                           {},
	"to_flat_index":                      {},
	"to_frame":                           {},
	"to_html":                            {},
	"to_integral_exact":                  {},
	"to_iter_dict":                       {},
	"to_julian_date":                     {},
	"to_latex":                           {},
	"to_list":                            {},
	"to_object_array":                    {},
	"to_object_array_tuples":             {},
	"to_pandas":                          {},
	"to_period":                          {},
	"to_series":                          {},
	"to_timedelta64":                     {},
	"to_timestamp":                       {},
	"true_and_notna":                     {},
	"truediv_object_array":               {},
	"tuples_to_object_array":             {},
	"type_of_values":                     {},
	"typeof_impl":                        {},
	"untokenize":                         {},
	"update_blklocs_and_blknos":          {},
	"using_string_dtype":                 {},
	"utf8_is_alnum":                      {},
	"utf8_is_alpha":                      {},
	"utf8_is_decimal":                    {},
	"utf8_is_digit":                      {},
	"utf8_is_lower":                      {},
	"utf8_is_numeric":                    {},
	"utf8_is_space":                      {},
	"utf8_is_title":                      {},
	"utf8_is_upper":                      {},
	"utf8_lower":                         {},
	"utf8_ltrim":                         {},
	"utf8_ltrim_whitespace":              {},
	"utf8_replace_slice":                 {},
	"utf8_rtrim":                         {},
	"utf8_rtrim_whitespace":              {},
	"utf8_swapcase":                      {},
	"utf8_title":                         {},
	"utf8_trim":                          {},
	"utf8_trim_whitespace":               {},
	"utf8_upper":                         {},
	"value_count":                        {},
	"value_getitem":                      {},
	"vec_binop":                          {},
	"vec_compare":                        {},
	"window_func":                        {},
	"wrap_function":                      {},
	"write_file":                         {},
	"write_output":                       {},

	// pandas wave-2 pass-3 — push toward <=3% ship-gate from
	// post-pass-2 residual (bug-rate 4.997%).
	//
	// Three buckets:
	//   (a) Remaining pandas-internal compound lowercase helpers
	//       (`accum_func`, `binary_join_element_wise`, `cache_readonly`,
	//       `compile_internal`, `pprint_thing_encoded`, `should_store`,
	//       `unique_label_indices`, ...).
	//   (b) Pandas-defined exception classes + engine + index types
	//       (`OutOfBoundsDatetime`, `IndexingError`, `MergeError`,
	//       `IntCastingNaNError`, `NullFrequencyError`, `BoolEngine`,
	//       `DatetimeEngine`, `Complex64Engine`, `BlockIndex`,
	//       `IntervalTree`, ...) — used as constructor calls / except
	//       clauses; pandas-distinctive names safe to allowlist.
	//   (c) scipy interpolator classes referenced from pandas
	//       (`CubicSpline`, `UnivariateSpline`, `Akima1DInterpolator`).
	"Akima1DInterpolator":              {},
	"BlockIndex":                       {},
	"BlockValuesRefs":                  {},
	"BoolEngine":                       {},
	"Complex128Engine":                 {},
	"Complex64Engine":                  {},
	"CubicSpline":                      {},
	"DataFrameFormatter":               {},
	"DataFrameInfo":                    {},
	"DataFrameRenderer":                {},
	"DatetimeEngine":                   {},
	"DictType":                         {},
	"DuplicateLabelError":              {},
	"ExcelFormatter":                   {},
	"ExtensionEngine":                  {},
	"FloatArrayFormatter":              {},
	"ForbiddenExtensionType":           {},
	"IncompatibleFrequency":            {},
	"IndexingError":                    {},
	"Int64HashTable":                   {},
	"IntCastingNaNError":               {},
	"IntIndex":                         {},
	"InteractiveShell":                 {},
	"IntervalTree":                     {},
	"InvalidComparison":                {},
	"InvalidIndexError":                {},
	"MergeError":                       {},
	"NativeValue":                      {},
	"NoBufferPresent":                  {},
	"NullFrequencyError":               {},
	"NumExprClobberingError":           {},
	"NumbaUtilError":                   {},
	"OutOfBoundsDatetime":              {},
	"OutOfBoundsTimedelta":             {},
	"PrettyDict":                       {},
	"SeriesFormatter":                  {},
	"SeriesInfo":                       {},
	"StringObjectEngine":               {},
	"TextWrapper":                      {},
	"TimedeltaEngine":                  {},
	"TreeBuilder":                      {},
	"UndefinedVariableError":           {},
	"UnivariateSpline":                 {},
	"UnsortedIndexError":               {},
	"ZoneInfoNotFoundError":            {},
	"abbrev_to_npy_unit":               {},
	"abs_checked":                      {},
	"accessor_mapping":                 {},
	"accum_func":                       {},
	"addinivalue_line":                 {},
	"agg_series":                       {},
	"all_nans":                         {},
	"alloca_once_value":                {},
	"alt_format_":                      {},
	"apply_groupwise":                  {},
	"arr_type":                         {},
	"as_array":                         {},
	"as_ctypes_type":                   {},
	"assume_timezone":                  {},
	"axis_kurt":                        {},
	"axis_skew":                        {},
	"backfill_2d_inplace":              {},
	"backfill_inplace":                 {},
	"binary_join_element_wise":         {},
	"bit_wise_not":                     {},
	"bit_wise_xor":                     {},
	"block_type":                       {},
	"bn_func":                          {},
	"c_dt64arr_to_periodarr":           {},
	"cache_readonly":                   {},
	"calculate_variable_window_bounds": {},
	"call_function_objargs":            {},
	"call_method":                      {},
	"clear_mapping":                    {},
	"col_func":                         {},
	"compare_mismatched_resolutions":   {},
	"compile_internal":                 {},
	"concat_horizontal":                {},
	"cons_row":                         {},
	"construct_from_string":            {},
	"constructor_sliced":               {},
	"count_level_2d":                   {},
	"create_struct_proxy":              {},
	"dataframe_from_int_dict":          {},
	"day_of_week":                      {},
	"day_of_year":                      {},
	"default_pprint":                   {},
	"delta_to_tick":                    {},
	"deregister_matplotlib_converters": {},
	"describe_func":                    {},
	"detect_number_of_cores":           {},
	"dicts_to_array":                   {},
	"diff_2d":                          {},
	"disallow_ambiguous_unit":          {},
	"drop_duplicates":                  {},
	"dtype_cls":                        {},
	"dtype_predicate":                  {},
	"dtypes_all_equal":                 {},
	"enable_data_resource_formatter":   {},
	"ewm_func":                         {},
	"float_format_":                    {},
	"floordiv_object_array":            {},
	"foreign_buffer":                   {},
	"func_":                            {},
	"groupsort_indexer":                {},
	"iget_values":                      {},
	"impl_ret_borrowed":                {},
	"increment_above":                  {},
	"indices_fast":                     {},
	"internal_values":                  {},
	"intervals_to_interval_bounds":     {},
	"into_c":                           {},
	"isna_string":                      {},
	"item_from_zerodim":                {},
	"ix_":                              {},
	"join_op":                          {},
	"kth_smallest":                     {},
	"left_error_msg":                   {},
	"list_element":                     {},
	"list_slice":                       {},
	"logical_and":                      {},
	"logical_or":                       {},
	"lookup_array":                     {},
	"may_share_memory":                 {},
	"op_left":                          {},
	"op_right":                         {},
	"option_context":                   {},
	"overload_method":                  {},
	"pprint_thing_encoded":             {},
	"quarter_to_myear":                 {},
	"rank_1d":                          {},
	"rank_2d":                          {},
	"rc_context":                       {},
	"references_same_values":           {},
	"remove_unused_levels":             {},
	"reset_index":                      {},
	"result_type":                      {},
	"right_error_msg":                  {},
	"round_nsint64":                    {},
	"select_dtypes":                    {},
	"ser_method":                       {},
	"serialize_object":                 {},
	"setitem_inplace":                  {},
	"shares_memory":                    {},
	"should_store":                     {},
	"slow_path":                        {},
	"soften_mask":                      {},
	"split_func":                       {},
	"stack_factorize":                  {},
	"supr_new":                         {},
	"term_type":                        {},
	"unique_deltas":                    {},
	"unique_label_indices":             {},
	"unpickle_block":                   {},
	"unregister_extension_type":        {},
	"update_dtype":                     {},
	"validation_func":                  {},
	"visited_value":                    {},
	"word_len":                         {},
	"zip_longest":                      {},

	// Wave-6 (client-fixture-a, Django + MongoDB). Pulled from the
	// post-wave-5 bug-extractor sample on client-fixture-a (Django
	// REST + PyMongo backend at 11.99% pre-wave). All names below
	// arrive at the resolver as bare identifiers after the Python
	// extractor strips the receiver from attribute calls
	// (`db.users.find_one(...)` → `find_one`,
	// `coll.update_one({...}, {...})` → `update_one`,
	// `datetime.strptime(s, fmt)` → `strptime`,
	// `datetime.utcnow()` → `utcnow`).  Gated to lang=="python" so
	// same-named user methods in other languages are not shadowed
	// (#94 safer-bias rule).
	//
	// Conservative selection rule (same as #455 + wave-4 waves):
	// include a name only when it is overwhelmingly a stdlib /
	// well-known-package idiom in Python.  Generic single-word verbs
	// (`find`, `update`, `delete`, `insert`, `count`, `replace`) and
	// MongoDB aggregation operators that collide with cross-language
	// verbs (`match`, `group`, `project`, `sort`, `limit`, `skip`,
	// `lookup`) are deliberately EXCLUDED.

	// PyMongo Collection / Database method surface.  Suffixed
	// `_one` / `_many` forms are distinctive (no plausible user-method
	// collision) and dominate the residual on client-fixture-a.
	"get_collection":           {},
	"get_database":             {},
	"insert_one":               {},
	"insert_many":              {},
	"update_one":               {},
	"update_many":              {},
	"delete_one":               {},
	"delete_many":              {},
	"find_one":                 {},
	"find_one_and_update":      {},
	"find_one_and_delete":      {},
	"find_one_and_replace":     {},
	"replace_one":              {},
	"bulk_write":               {},
	"count_documents":          {},
	"estimated_document_count": {},
	"distinct":                 {},
	// PyMongo index management.
	"create_index":      {},
	"create_indexes":    {},
	"drop_index":        {},
	"drop_indexes":      {},
	"list_indexes":      {},
	"index_information": {},
	// bson value types.  `ObjectId`, `Timestamp` are already in
	// stdlibBareNames / pandas allowlist — not duplicated here.
	// `Regex` excluded — PascalCase fallback heuristic synthesises
	// it for non-python sources, which would trip the cross-language
	// gate test.  `Decimal128` kept (no PascalCase collision).
	"Decimal128": {},

	// datetime / dateutil — receiver-stripped from
	// `datetime.strptime(...)`, `datetime.utcnow()`,
	// `dt.fromisoformat(s)`, `datetime.now()`, `date.today()`.
	// `strptime` collides with cppBareNames (libc time) — the
	// lang-gate dispatches each language to its own map so this is
	// behaviourally safe; the collision is only excluded from the
	// cross-language test assertion below (same pattern as `Path` /
	// `Iterator` / `Any`).  `now`/`today` collide with phpBareNames
	// (Laravel global helpers) and several JS / Swift maps — same
	// pattern: excluded from cross-language test only.
	"strptime":         {},
	"fromisoformat":    {},
	"fromtimestamp":    {},
	"utcnow":           {},
	"utcfromtimestamp": {},
	"now":              {},
	"today":            {},
	"timedelta":        {},
	"relativedelta":    {},

	// random — `randint` / `randrange` / `choice` / `sample` are the
	// canonical `random` module helpers (`from random import choice`).
	// `choice` was previously excluded as "too generic"; fixture-a
	// evidence shows every occurrence is `random.choice` or
	// `choices`/`choice` imported from `random`, not a user method.
	// Wave-4 stragglers fix (issue #529).
	"randint":   {},
	"randrange": {},
	"choice":    {},
	"choices":   {},
	"sample":    {},

	// uuid — `uuid4` / `uuid1` are the standard generators
	// (`from uuid import uuid4`).
	"uuid4": {},
	"uuid1": {},

	// requests — `raise_for_status` is the canonical
	// `response.raise_for_status()` call; distinctive idiom.
	"raise_for_status": {},

	// Django ORM extras receiver-stripped from QuerySet chains
	// (`qs.order_by("-created_at")`) and Django QueryDict
	// (`request.GET.getlist("ids")`).
	"order_by": {},
	"getlist":  {},

	// Python DB-API — `cursor.fetchall()` / `cursor.fetchone()`.
	// `cursor` excluded (too generic; many ORMs expose
	// `model.cursor`).
	"fetchall":  {},
	"fetchone":  {},
	"fetchmany": {},

	// Wave-7 — django+drf+celery+pandas+boto3 residual on
	// client-fixture-a (django app at 9.64% after pass-1).
	//
	// Django ORM Q / Case / When / Value query expressions
	// (`from django.db.models import Q, Case, When, Value`).
	// All Pascal-case and uniquely Django ORM idioms.
	"Q":     {},
	"Case":  {},
	"When":  {},
	"Value": {},
	// `F` excluded (single-letter, collision-prone with type-params).
	"QueryDict":          {}, // django.http.QueryDict
	"DRFValidationError": {}, // re-export alias of rest_framework ValidationError
	// Django shortcuts + atomic transactions.
	"get_object_or_404": {},
	"get_list_or_404":   {},
	"atomic":            {}, // django.db.transaction.atomic
	"parse_datetime":    {}, // django.utils.dateparse
	"parse_date":        {},
	"parse_time":        {},
	"parse_duration":    {},
	// Django management BaseCommand handler attributes / writers.
	// `handle` and `write` are too generic — excluded.
	"add_arguments":                {},
	"add_mutually_exclusive_group": {},
	"add_argument":                 {}, // argparse + Command.add_arguments
	// BaseCommand style-helper constants (SUCCESS / WARNING / ERROR)
	// — actual usage is `self.style.SUCCESS(...)` receiver-stripped.
	// `ERROR` excluded — collides too broadly (logging, JS, etc).
	"SUCCESS": {},
	"WARNING": {},
	// unittest TestCase assertion methods — receiver-stripped from
	// `self.assertEqual(...)`. These are unambiguously stdlib unittest
	// when seen Python-side.
	"assertEqual":          {},
	"assertNotEqual":       {},
	"assertTrue":           {},
	"assertFalse":          {},
	"assertIs":             {},
	"assertIsNot":          {},
	"assertIsNone":         {},
	"assertIsNotNone":      {},
	"assertIn":             {},
	"assertNotIn":          {},
	"assertIsInstance":     {},
	"assertNotIsInstance":  {},
	"assertRaises":         {},
	"assertRaisesMessage":  {},
	"assertRaisesRegex":    {},
	"assertWarns":          {},
	"assertLogs":           {},
	"assertAlmostEqual":    {},
	"assertNotAlmostEqual": {},
	"assertGreater":        {},
	"assertGreaterEqual":   {},
	"assertLess":           {},
	"assertLessEqual":      {},
	"assertRegex":          {},
	"assertNotRegex":       {},
	"assertCountEqual":     {},
	"assertListEqual":      {},
	"assertTupleEqual":     {},
	"assertSetEqual":       {},
	"assertDictEqual":      {},
	"assertSequenceEqual":  {},
	"assertMultiLineEqual": {},
	"assertNumQueries":     {},
	"assertQuerysetEqual":  {},
	"assertContains":       {},
	"assertNotContains":    {},
	"assertRedirects":      {},
	"assertTemplateUsed":   {},
	"assertFormError":      {},
	"assertJSONEqual":      {},
	"assertHTMLEqual":      {},
	"assertXMLEqual":       {},
	// importlib — `import_module` is the canonical helper.
	"import_module": {},
	// pandas read/transform helpers (Excel + DataFrame). Receiver-
	// stripped from `pd.read_excel(...)` / `df.iterrows()` / etc.
	// `apply` excluded — too generic (collides with JS Function.apply,
	// Ruby Proc.call patterns, etc.). `fillna` is distinctly pandas.
	"read_excel":  {},
	"read_csv":    {},
	"to_datetime": {},
	"to_numeric":  {},
	"iterrows":    {},
	"itertuples":  {},
	"fillna":      {},
	"dropna":      {},
	"isnull":      {},
	"notnull":     {},
	// Celery task DSL — `delay` is `Task.delay(...)` the canonical
	// async-submit shortcut.
	"delay": {},
	// boto3 / botocore SQS + S3 + client helpers. `Session` is the
	// botocore `Session()` constructor; the rest are SQS verbs from
	// the queue-consumer fixture.
	"Session":              {}, // botocore.session.Session / requests.Session
	"get_queue_attributes": {},
	"receive_message":      {},
	"delete_message":       {},
	"send_message":         {},
	"get_credentials":      {},
	// stdlib os.getenv (already exists via os.environ but `getenv` is
	// the bare-name receiver-strip from `os.getenv("FOO")`).
	"getenv": {},

	// Flask-realworld wave — flask + sqlalchemy + werkzeug + click
	// distinctive bare-name receiver-strips not yet covered. Conservative
	// addition rule preserved: all names below are dominantly framework
	// idioms in Python and unlikely to collide with user-defined methods
	// of the same simple name in JS / Go / etc. Excluded generic verbs:
	// `first`, `commit`, `add`, `remove`, `append`, `pop`, `items`,
	// `update`, `extend`, `replace`, `post`, `limit`, `offset`, `walk`,
	// `bind`, `exit` (too collision-prone per #94 safer-bias rule).

	// Flask-Login decorator + helpers. `current_user` is the proxy
	// receiver-stripped from `current_user.is_authenticated`; the rest
	// are decorator + helper functions.
	"current_user":             {},
	"login_user":               {},
	"logout_user":              {},
	"login_required":           {},
	"fresh_login_required":     {},
	"user_loaded_from_request": {},
	"login_fresh":              {},
	"confirm_login":            {},
	"login_url":                {},
	// Flask-JWT-Extended decorators + helpers (`create_access_token`
	// already in wave-4; add the verification + identity helpers).
	"jwt_required":                   {},
	"jwt_optional":                   {},
	"create_refresh_token":           {},
	"get_jwt_identity":               {},
	"get_jwt_claims":                 {},
	"get_jti":                        {},
	"verify_jwt_in_request":          {},
	"verify_jwt_in_request_optional": {},
	"jwt_refresh_token_required":     {},
	"set_access_cookies":             {},
	"set_refresh_cookies":            {},
	"unset_jwt_cookies":              {},
	// Flask-RESTful helpers — `marshal`/`marshal_with` decorator,
	// `reqparse` parser DSL.
	"marshal":            {},
	"marshal_with":       {},
	"marshal_with_field": {},
	"reqparse":           {},
	"RequestParser":      {},
	// Flask-CORS / Flask-SocketIO decorators.
	"cross_origin": {},
	"join_room":    {},
	"leave_room":   {},
	"close_room":   {},
	"rooms":        {},
	"disconnect":   {},
	"on_namespace": {},
	// Flask-WTF / WTForms — `validate_on_submit` + CSRF helpers +
	// distinctive validator constructors (Length / NumberRange /
	// Required already too generic; `DataRequired` is distinctive).
	"validate_on_submit": {},
	"hidden_tag":         {},
	"populate_obj":       {},
	"DataRequired":       {},
	"InputRequired":      {},
	"EqualTo":            {},
	"AnyOf":              {},
	"generate_csrf":      {},
	"validate_csrf":      {},
	// Marshmallow Schema decorators + dump/load helpers. `dump`/`load`
	// excluded as too generic; the `_many`/`_dict` suffix + decorator
	// hooks are distinctive.
	"validates_schema": {},
	"pre_load":         {},
	"post_load":        {},
	"pre_dump":         {},
	"post_dump":        {},
	"dump_one":         {},
	"dump_many":        {},
	"load_one":         {},
	"load_many":        {},
	// SQLAlchemy query / session DSL beyond wave-4 set. `desc`/`asc`
	// are short but distinctive when python-gated (SQLAlchemy ORDER BY
	// modifiers).
	"desc":             {}, // sqlalchemy.desc / Column.desc
	"asc":              {}, // sqlalchemy.asc / Column.asc
	"nullsfirst":       {},
	"nullslast":        {},
	"subqueryload":     {},
	"joinedload":       {},
	"selectinload":     {},
	"lazyload":         {},
	"noload":           {},
	"raiseload":        {},
	"defer":            {},
	"undefer":          {},
	"with_entities":    {},
	"with_polymorphic": {},
	"options":          {},
	"having":           {},
	"group_by":         {},
	"order_by_clause":  {},
	"backref":          {},
	"validates":        {},
	"column_property":  {},
	"hybrid_property":  {},
	"hybrid_method":    {},
	"declared_attr":    {},
	"declarative_base": {},
	"sessionmaker":     {},
	"scoped_session":   {},
	"event":            {},
	// SQLAlchemy column types receiver-stripped from `db.Column(db.Text)`
	// → `Text` etc. Distinctive PascalCase types.
	"BigInteger":   {},
	"SmallInteger": {},
	"LargeBinary":  {},
	"Interval":     {},
	"Numeric":      {},
	// Werkzeug helpers receiver-stripped from `werkzeug.utils.X` /
	// `werkzeug.security.X` / `werkzeug.exceptions.X`.
	"secure_filename": {},
	"escape":          {},
	"unescape":        {},
	"safe_str_cmp":    {},
	"safe_join":       {},
	"http_date":       {},
	"BadRequest":      {},
	"Unauthorized":    {},
	"Forbidden":       {},
	// `NotFound` already in stdlibBareNames
	// `Conflict`/`UnprocessableEntity` collide with csharp aspnet DSL
	"Gone":                 {},
	"UnsupportedMediaType": {},
	"TooManyRequests":      {},
	"InternalServerError":  {},
	// Flask request/response inline helpers. `g` is the flask global
	// proxy receiver-stripped from `g.user`; `session` collides with
	// SQLA `db.session` (intentionally NOT added — too generic).
	"shell_context_processor": {},
	"to_json":                 {}, // Flask InvalidUsage idiom in this corpus
	"iter_blueprints":         {},
	"app_errorhandler":        {},
	"before_request":          {},
	"after_request":           {},
	"teardown_request":        {},
	"teardown_appcontext":     {},
	"context_processor":       {},
	"url_value_preprocessor":  {},
	"url_defaults":            {},
	"before_first_request":    {},
	// Click CLI test runner + DSL (`invoke` already in wave-7; add the
	// `echo` helper + group decorators). `echo` collides with PHP/Go
	// idioms but is python-gated here.
	"echo":    {},
	"secho":   {},
	"prompt":  {},
	"confirm": {},
	"clear":   {},
	"getchar": {},
	"pause":   {},
	"edit":    {},
	// `launch` excluded — collides with kotlin coroutine launch
	"open_file":           {},
	"format_filename":     {},
	"argument":            {},
	"option":              {},
	"password_option":     {},
	"confirmation_option": {},
	"version_option":      {},
	"help_option":         {},
	"pass_context":        {},
	"pass_obj":            {},
	"make_pass_decorator": {},
	"BadParameter":        {},
	"UsageError":          {},
	"MissingParameter":    {},
	"NoSuchOption":        {},
	"FileError":           {},
	// Python `cls` receiver. Bare-name receiver-strip from
	// `cls.method(...)` inside classmethods. Strictly python idiom —
	// the literal identifier `cls` is the conventional first parameter
	// of `@classmethod`-decorated methods, treated as keyword-like by
	// every Python style guide. Gating python-only avoids shadowing
	// user variables in other languages. Wave-realised on
	// flask-realworld (6 hits) but high-frequency across django + drf
	// corpora.
	"cls": {},
	// SQLAlchemy `relationship()` parameter name receiver-strips.
	// `lazy` excluded — collides with kotlin stdlib lazy
	"uselist":         {},
	"viewonly":        {},
	"cascade":         {},
	"single_parent":   {},
	"passive_deletes": {},
	"passive_updates": {},
	// pytest assertion + capture (already have `raises`, `fixture`,
	// `mark`, `parametrize`, `monkeypatch`, `xfail`). Add `approx`
	// (numerical comparison helper) + `deprecated_call`.
	"approx":          {},
	"deprecated_call": {},
	"warns":           {},
	"importorskip":    {},
	"skipif":          {},
	"skipunless":      {},
	"usefixtures":     {},
	"freeze_time":     {},
	// Marshmallow `Method` / `Function` / `Constant` field constructors.
	"Method":     {},
	"Function":   {},
	"Constant":   {},
	"Nested":     {},
	"Pluck":      {},
	"auto_field": {},
	// Flask-Marshmallow / marshmallow-sqlalchemy exports.
	"SQLAlchemyAutoSchema": {},
	// Flask-APISpec marshaller decorators.
	"marshal_with_apispec": {},
	"use_kwargs":           {},
	"doc":                  {},

	// Click wave — gettext convention. `from gettext import gettext as _`
	// + `from gettext import ngettext` is the canonical i18n idiom in
	// CPython CLI codebases (click, flask, django i18n). Bare `_(...)`
	// arrives at the resolver as `_` (45 hits in click samples — 12%
	// of the bug-extractor bucket) and `ngettext(...)` as `ngettext`
	// (7 hits). Both alias-style identifiers are reserved-by-convention
	// for gettext in mainstream Python; the python gate keeps the
	// short `_` token from shadowing throwaway variable assignments in
	// other languages. Single source of truth: gettext.
	"_":        {},
	"ngettext": {},

	// Click wave — Python stdlib helpers seen receiver-stripped in
	// click's own source and tests. Each is unambiguously a stdlib
	// API on CPython:
	//   - shutil/os helpers: `which`, `unlink`, `dup2`
	//   - ctypes primitives: `byref`, `c_ulong`, `c_int`, `c_void_p`,
	//     `c_char_p`, `c_uint`, `c_long`, `c_short`, `c_byte`, `WinError`,
	//     `GetLastError`, `HANDLE`
	//   - tempfile / contextlib classes: `TemporaryDirectory`, `ExitStack`
	//   - subprocess: `Popen` (constructor; class name is rarely
	//     redefined)
	//   - urllib.parse: `urlparse` (when imported bare via
	//     `from urllib.parse import urlparse`)
	//   - io classes: `BufferedWriter`
	//   - dict / str builtins missing from stdlibBareNames:
	//     `partition`, `rpartition`, `casefold`, `fromkeys`
	//   - warnings: `warn` (the `warnings.warn(...)` receiver-stripped
	//     bare-name form)
	//   - inspect: `getfullargspec`, `cleandoc` is already in pythonBareNames
	//   - utime: os.utime
	// All names are Python-gated; collisions with user methods in
	// other languages are blocked by the lang=="python" check.
	"which":              {},
	"unlink":             {},
	"dup2":               {},
	"utime":              {},
	"warn":               {},
	"partition":          {},
	"rpartition":         {},
	"casefold":           {},
	"fromkeys":           {},
	"urlparse":           {},
	"byref":              {},
	"c_ulong":            {},
	"c_int":              {},
	"c_uint":             {},
	"c_long":             {},
	"c_short":            {},
	"c_byte":             {},
	"c_void_p":           {},
	"c_char_p":           {},
	"c_wchar_p":          {},
	"c_size_t":           {},
	"c_bool":             {},
	"WinError":           {},
	"GetLastError":       {},
	"HANDLE":             {},
	"TemporaryDirectory": {},
	"ExitStack":          {},
	"Popen":              {},
	"BufferedWriter":     {},

	// Click wave Pass-2 — Python logging API. After
	// `logger = logging.getLogger(__name__)` the receiver-stripped
	// dispatch produces bare `setLevel`, `addHandler`, `removeHandler`,
	// `StreamHandler` (constructor), `Handler`, `Formatter`. These are
	// distinctive PascalCase / camelCase identifiers tied to the
	// `logging` stdlib surface — extremely rare as user-method names
	// in Python idiom. Already python-gated. `getLogger` and
	// `basicConfig` are present elsewhere in pythonBareNames via the
	// issue #455 block; not duplicated here.
	"setLevel":      {},
	"addHandler":    {},
	"removeHandler": {},
	"StreamHandler": {},
	"FileHandler":   {},
	"NullHandler":   {},
	"Handler":       {},
	"Formatter":     {},
	"LogRecord":     {},
	"Filter":        {},

	// Click wave Pass-2 — pathlib.Path read/write helpers and argparse.
	// `path.read_text()` / `path.write_text()` / `path.read_bytes()` /
	// `path.write_bytes()` are receiver-stripped pathlib methods. They
	// have the snake_case `_text`/`_bytes` suffix that overwhelmingly
	// signals pathlib — vanishingly rare as user-method shapes.
	// `parse_args` is the argparse / click parser dispatcher leaf.
	"read_text":   {},
	"write_text":  {},
	"read_bytes":  {},
	"write_bytes": {},
	"parse_args":  {},

	// Click wave Pass-2 — pytest monkeypatch surface. After
	// `monkeypatch.setenv("FOO", "bar")` / `.delenv` / `.setitem` /
	// `.delitem` / `.chdir` / `.syspath_prepend` / `.context` the bare
	// leaf reaches the resolver. These are pytest-monkeypatch idioms
	// (`monkeypatch` is already in pythonBareNames at the pytest block);
	// the methods complete that surface.
	"setenv":          {},
	"delenv":          {},
	"setitem":         {},
	"delitem":         {},
	"syspath_prepend": {},

	// Click wave Pass-2 — unittest.mock patch decorator and re module
	// (`re.match`, `re.search`, `re.sub`, `re.findall`, `re.fullmatch`,
	// `re.compile`). The `re.*` calls survive receiver-stripping as
	// bare leaf names. `match` is EXCLUDED — Python 3.10+ structural
	// pattern matching uses `match` as a keyword; while the bare-name
	// lookup wouldn't shadow keyword usage, the leaf is generic enough
	// that user code routinely defines `Foo.match(...)` as a method
	// (regex-like APIs on parsers/AST nodes). `sub` is EXCLUDED — too
	// generic (subscribe / subtract / subprocess accessors). Kept the
	// rarer leaves only.
	"fullmatch":   {},
	"sre_parse":   {},
	"sre_compile": {},

	// Click wave Pass-2 — common pytest / mock surface beyond what
	// the issue #455 block already covered. `patch` is the canonical
	// `unittest.mock.patch` decorator; `MagicMock`, `Mock`, `PropertyMock`,
	// `AsyncMock`, `call`, `ANY` are the supporting unittest.mock
	// classes. All are PascalCase / unambiguous mock idioms.
	"patch":        {},
	"MagicMock":    {},
	"Mock":         {},
	"PropertyMock": {},
	"AsyncMock":    {},
	"ANY":          {},
	"DEFAULT":      {},
	"sentinel":     {},

	// Click wave Pass-3 — concurrent.futures.Executor + os/gc/contextlib
	// stdlib surface. Each leaf is unambiguously a stdlib method receiver-
	// stripped from the canonical receiver:
	//   - `executor.submit(fn)` → `submit` (ThreadPoolExecutor /
	//     ProcessPoolExecutor; the method is rarely a user-method name)
	//   - `os.urandom(n)` → `urandom` (distinctive)
	//   - `os.rmdir(path)` / `path.rmdir()` → `rmdir` (distinctive)
	//   - `gc.collect()` → `collect` (gc is distinctive; `collect` does
	//     collide with itertools-style user code but receiver-strip
	//     dominates the click corpus)
	//   - `stack.with_resource(cm)` → `with_resource` (contextlib
	//     ExitStack; the snake-case verb shape is overwhelmingly
	//     contextlib)
	//   - `os.execvp` / `os.execv` family
	//   - `os.environ` ops not in stdlibBareNames: `putenv`, `unsetenv`
	//   - shutil: `rmtree` (Wave-7 has `chdir`/`mkdtemp` already; not
	//     added here)
	"submit":        {},
	"urandom":       {},
	"rmdir":         {},
	"collect":       {},
	"with_resource": {},
	"putenv":        {},
	"unsetenv":      {},
	"rmtree":        {},
	"copyfile":      {},
	"copytree":      {},
	"copystat":      {},
	"chown":         {},
	"chmod":         {},
	"symlink":       {},
	"readlink":      {},
	"link":          {},
	"sendfile":      {},
	"fsync":         {},
	"truncate":      {},
	"ftruncate":     {},
	"isatty":        {},
	"tcgetattr":     {},
	"tcsetattr":     {},
	"setraw":        {},
	"cbreak":        {},

	// Click wave Pass-3 — pathlib Path / PurePath operations that
	// arrive bare after `Path(...).is_file()` / `.is_dir()` /
	// `.iterdir()` / `.glob()` / `.exists()` / `.with_suffix()` /
	// `.relative_to()` / `.absolute()` / `.resolve()`. The snake-case
	// `with_*` / `is_*` / `_to` shapes are highly distinctive pathlib
	// idioms. `exists` / `is_file` / `is_dir` are EXCLUDED — they
	// collide with `models.exists()` QuerySet + Django model methods
	// and would shadow real local resolutions (#94 safer-bias).
	"with_suffix":     {},
	"with_name":       {},
	"with_stem":       {},
	"relative_to":     {},
	"iterdir":         {},
	"hardlink_to":     {},
	"symlink_to":      {},
	"chmod_to":        {},
	"as_posix":        {},
	"as_uri":          {},
	"is_symlink":      {},
	"is_absolute":     {},
	"is_relative_to":  {},
	"is_reserved":     {},
	"is_mount":        {},
	"is_socket":       {},
	"is_block_device": {},
	"is_char_device":  {},
	"is_fifo":         {},
	"touch":           {},
	"samefile":        {},
	"lchmod":          {},
	"group":           {},

	// Click wave Pass-3 — time / sleep functions when imported bare.
	// `time.sleep(s)` receiver-stripped to `sleep`. `sleep` does have
	// some collision risk (Test.sleep methods, custom polling DSLs) but
	// the click test suite has 4+ direct receiver-strip hits and the
	// python gate restricts to python sources. Trade-off accepted.
	"sleep":        {},
	"perf_counter": {},
	"monotonic":    {},
	"time_ns":      {},
	"process_time": {},

	// Click wave Pass-3 — warnings module. `warnings.warn` is already
	// added above. `simplefilter` / `resetwarnings` / `showwarning`
	// complete the surface. `filterwarnings` already exists.
	"simplefilter":  {},
	"resetwarnings": {},
	"showwarning":   {},

	// Click wave Pass-3 — pickle protocol classes / functions.
	"Pickler":          {},
	"Unpickler":        {},
	"HIGHEST_PROTOCOL": {},
	"PickleError":      {},

	// Click wave Pass-3 — atexit module (small surface).
	"atexit": {},

	// Click wave Pass-3 — io.* class surface beyond BufferedWriter.
	"BufferedReader": {},
	"BufferedRandom": {},
	"BufferedRWPair": {},
	"TextIOWrapper":  {},
	"FileIO":         {},
	// `BytesIO` already in stdlibBareNames — not duplicated here.
	"RawIOBase":            {},
	"BufferedIOBase":       {},
	"TextIOBase":           {},
	"IOBase":               {},
	"UnsupportedOperation": {},

	// Click wave Pass-3 — datetime constructors and helpers seen
	// receiver-stripped (`datetime.datetime.now()` → `now` is too
	// generic, EXCLUDED; `datetime.timedelta(...)` → `timedelta`
	// kept). `now`, `utcnow`, `today` excluded per safer-bias.
	// `timedelta` already exists in pythonBareNames.
	"timezone": {},
	"tzinfo":   {},

	// Click wave Pass-3 — uuid module. `uuid1` and `uuid4` already
	// exist in pythonBareNames; add the remaining.
	"uuid3": {},
	"uuid5": {},
	"UUID":  {},

	// Click wave Pass-3 — hashlib digests not in stdlibBareNames.
	"md5":       {},
	"sha1":      {},
	"sha256":    {},
	"sha512":    {},
	"blake2b":   {},
	"blake2s":   {},
	"sha3_256":  {},
	"sha3_512":  {},
	"hexdigest": {},
	"digest":    {},
	"hmac":      {},
	// `new` excluded — gated to ruby (rubyBareNames).

	// Click wave Pass-3 — base64 / binascii encodings.
	"b64encode":         {},
	"b64decode":         {},
	"urlsafe_b64encode": {},
	"urlsafe_b64decode": {},
	"b32encode":         {},
	"b32decode":         {},
	"a85encode":         {},
	"a85decode":         {},
	"hexlify":           {},
	"unhexlify":         {},

	// Click wave Pass-3 — json module helpers receiver-stripped.
	// `json.loads` / `json.dumps` are in stdlibBareNames already (no
	// they're not — only `json` as a package root). Add the verb pair
	// here gated to python so it doesn't shadow user methods elsewhere.
	"loads": {},
	"dumps": {},
	"load":  {},
	"dump":  {},

	// Click wave Pass-3 — os.path / glob / fnmatch leaves.
	"fnmatchcase":  {},
	"normpath":     {},
	"normcase":     {},
	"commonpath":   {},
	"commonprefix": {},
	"splitdrive":   {},

	// Click wave Pass-3 — typing module additions not yet listed.
	"runtime_checkable": {},
	"final":             {},
	"override":          {},
	"reveal_type":       {},
	"assert_type":       {},
	"assert_never":      {},
	"get_type_hints":    {},
	"get_origin":        {},
	"get_args":          {},
	"is_typeddict":      {},
	"ParamSpec":         {},
	"TypeVarTuple":      {},
	"Unpack":            {},
	"TypeAlias":         {},
	"Self":              {},
	"Concatenate":       {},
	"Required":          {},
	"NotRequired":       {},

	// Click wave Pass-3 — functools module additions not in
	// pythonBareNames. `partial`, `reduce`, `cache` already exist.
	"singledispatch":       {},
	"singledispatchmethod": {},
	"total_ordering":       {},
	"cmp_to_key":           {},
	"partialmethod":        {},

	// Click wave Pass-3 — inspect introspection (those not in
	// pythonBareNames issue #455 block). `signature` already exists.
	"Signature":           {},
	"Parameter":           {},
	"BoundArguments":      {},
	"getfullargspec":      {},
	"iscoroutine":         {},
	"iscoroutinefunction": {},
	"isasyncgen":          {},
	"isawaitable":         {},
	"ismodule":            {},
	"isabstract":          {},
	"isbuiltin":           {},
	"isgenerator":         {},
	"isgeneratorfunction": {},

	// Click wave Pass-3 — importlib top-level helpers.
	// `import_module` already exists. `reload` excluded — gated to ruby
	// (rubyBareNames). Python's `importlib.reload` is the leaf name but
	// the cross-lang gate test forbids duplicates.
	"invalidate_caches": {},
	"find_spec":         {},
	"module_from_spec":  {},

	// Click wave Pass-2 — IO/console-write surface from click's own
	// `HelpFormatter` and tests: `write_dl`, `write_paragraph`,
	// `write_usage`, `write_text` (already added above), `make_formatter`,
	// `indentation`, `section`. EXCLUDED — these are local Click
	// methods on the `HelpFormatter` class; adding them would shadow
	// real local entities (#94 safer-bias). The bug-extractor hits
	// are a separate problem (missing self.method resolution) not
	// solved by the external bare-name route.

	// Click wave — gettext-aliased call surface. After `from gettext
	// import gettext as _`, the dotted form `_.format(...)` arrives
	// at the resolver because the Python extractor doesn't trace the
	// rename. `format` already exists as a builtin in stdlibBareNames
	// and is not re-added; `_.format` is the dotted shape with `_`
	// as the receiver — handled separately by the dotted-receiver
	// branch in classifyExternal below (issue: click i18n).
}

// pythonGettextDottedReceivers lists the gettext-import aliases that
// click's own source uses as call receivers. `from gettext import
// gettext as _` produces `_.format(...)` / `_.upper(...)` (28 +
// scattered hits in click bug-extractor samples — second-largest
// bucket); `from gettext import ngettext` produces
// `ngettext.format(...)` (7 hits). The Python extractor doesn't
// trace import aliases, so the dotted form survives unrewritten.
// Route them to `ext:gettext` via the dotted-receiver branch in
// classifyExternal. Lang-gated to python to avoid shadowing the
// throwaway-`_` variable convention in other ecosystems (Ruby /
// Go / Rust / JS test scaffolding all use `_` for ignored values).
var pythonGettextDottedReceivers = map[string]struct{}{
	"_":        {},
	"ngettext": {},
}

// ---------------------------------------------------------------------
// Wave-10 Track D — Per-import file-scoped Python gates.
//
// Background: client-fixture-a (private Django app) residual after
// wave-7 is dominated by generic Python verbs (`append`/`first`/
// `items`/`replace`/`pop`/`error`/`info`/`execute`/`cursor`/etc.)
// that #94's safer-bias rule keeps OUT of pythonBareNames because they
// collide with user-defined methods on hand-rolled classes across the
// Python corpus (and other languages). Same blocker that wave-7/9
// React solved for JS: file-scoped gates (`hasJSCollectionLibImport`)
// activate the generic-verb allowlist only on files that consume the
// canonical library whose surface those names belong to.
//
// Implementation mirrors the JS / Ktor / Kafka / commons-cli / jax-rs
// precedents: per-library `hasPython<Lib>Import` predicate + per-
// library bare-name map + sentinel subtype routed through the
// stdlibFunction wrapper at line ~833 so the synthesiser folds the
// edge to the canonical ecosystem placeholder (`ext:pandas` /
// `ext:requests` / `ext:django` / ...) rather than `ext:<bare-leaf>`.
//
// All gates are lang=="python" + file-scoped. A python file that
// doesn't import the gated library keeps the safer-bias miss
// (preserves the rule from #94). A non-python file with a same-named
// import (e.g. a JS file importing "django" by typo) cannot activate
// the gate because the lang gate fires first.
//
// Refs #94 #131 #498.
// ---------------------------------------------------------------------

// pythonImportRoot returns the canonical first segment of a Python
// import path. `django.db.models` -> `django`, `flask_sqlalchemy` ->
// `flask_sqlalchemy`. Strips a leading `ext:` so the gates work both
// before and after external synthesis canonicalises the IMPORTS edge.
func pythonImportRoot(p string) string {
	if strings.HasPrefix(p, "ext:") {
		p = p[len("ext:"):]
	}
	if dot := strings.IndexByte(p, '.'); dot > 0 {
		return p[:dot]
	}
	return p
}

// pythonImportRootIn reports whether the file's import set contains any
// import whose first-segment root is in `roots`.
func pythonImportRootIn(imports map[string]bool, roots map[string]struct{}) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		if _, ok := roots[pythonImportRoot(p)]; ok {
			return true
		}
	}
	return false
}

// pythonImportRootHasPrefix reports whether any import in the file's
// set starts with one of the listed prefixes. Used for flask_* shape
// (e.g. flask_sqlalchemy / flask_login / flask_restful).
func pythonImportRootHasPrefix(imports map[string]bool, prefix string) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		root := pythonImportRoot(p)
		if root == prefix || strings.HasPrefix(root, prefix+"_") {
			return true
		}
	}
	return false
}

// hasPythonPandasImport reports whether the file imports pandas /
// numpy / pyarrow. Activates the pandas/numpy DataFrame surface
// allowlist (head, tail, query, groupby, merge, etc.).
func hasPythonPandasImport(imports map[string]bool) bool {
	return pythonImportRootIn(imports, pythonPandasImportRoots)
}

var pythonPandasImportRoots = map[string]struct{}{
	"pandas":  {},
	"numpy":   {},
	"pyarrow": {},
	"pd":      {}, // sometimes the extractor stamps the alias as a root
	"np":      {},
}

// hasPythonRequestsImport reports whether the file imports
// requests / httpx / aiohttp (synchronous + async HTTP clients).
func hasPythonRequestsImport(imports map[string]bool) bool {
	return pythonImportRootIn(imports, pythonRequestsImportRoots)
}

var pythonRequestsImportRoots = map[string]struct{}{
	"requests": {},
	"httpx":    {},
	"aiohttp":  {},
}

// hasPythonBoto3Import reports whether the file imports boto3 /
// botocore / aiobotocore. Activates the AWS SDK client surface.
func hasPythonBoto3Import(imports map[string]bool) bool {
	return pythonImportRootIn(imports, pythonBoto3ImportRoots)
}

var pythonBoto3ImportRoots = map[string]struct{}{
	"boto3":       {},
	"botocore":    {},
	"aiobotocore": {},
}

// hasPythonRedisImport reports whether the file imports redis /
// aioredis. Activates the redis-client surface.
func hasPythonRedisImport(imports map[string]bool) bool {
	return pythonImportRootIn(imports, pythonRedisImportRoots)
}

var pythonRedisImportRoots = map[string]struct{}{
	"redis":    {},
	"aioredis": {},
}

// hasPythonDjangoImport reports whether the file imports django or
// rest_framework (DRF). Activates the Django ORM / DRF generic-verb
// surface (`first`, `last`, `all`, `count`, `exists`, ...).
func hasPythonDjangoImport(imports map[string]bool) bool {
	return pythonImportRootIn(imports, pythonDjangoImportRoots)
}

var pythonDjangoImportRoots = map[string]struct{}{
	"django":         {},
	"rest_framework": {},
}

// hasPythonFlaskImport reports whether the file imports flask or
// any flask_* extension (flask_sqlalchemy, flask_login, ...).
func hasPythonFlaskImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		root := pythonImportRoot(p)
		if root == "flask" || strings.HasPrefix(root, "flask_") {
			return true
		}
	}
	return false
}

// hasPythonSQLAlchemyImport reports whether the file imports
// sqlalchemy or flask_sqlalchemy. Activates the ORM session /
// Column / relationship surface (`commit`, `rollback`, `Column`,
// `relationship`, `backref`).
func hasPythonSQLAlchemyImport(imports map[string]bool) bool {
	return pythonImportRootIn(imports, pythonSQLAlchemyImportRoots)
}

var pythonSQLAlchemyImportRoots = map[string]struct{}{
	"sqlalchemy":       {},
	"flask_sqlalchemy": {},
}

// hasPythonMongoImport reports whether the file imports pymongo /
// motor / bson / mongoengine. Activates the Mongo collection
// surface (`find`, `insert`, `update`, `delete`, `aggregate`).
func hasPythonMongoImport(imports map[string]bool) bool {
	return pythonImportRootIn(imports, pythonMongoImportRoots)
}

var pythonMongoImportRoots = map[string]struct{}{
	"pymongo":     {},
	"motor":       {},
	"bson":        {},
	"mongoengine": {},
}

// hasPythonCeleryImport reports whether the file imports celery.
// Activates the task-DSL surface (`delay`, `apply_async`, `s`).
func hasPythonCeleryImport(imports map[string]bool) bool {
	return pythonImportRootIn(imports, pythonCeleryImportRoots)
}

var pythonCeleryImportRoots = map[string]struct{}{
	"celery":      {},
	"celery_once": {},
	"kombu":       {},
}

// hasPythonLoggingImport reports whether the file imports the stdlib
// `logging` module or `logger`/`loguru`/`structlog`. The python
// extractor stamps stdlib imports too, so files that do
// `import logging` show `logging` in their import set.
func hasPythonLoggingImport(imports map[string]bool) bool {
	return pythonImportRootIn(imports, pythonLoggingImportRoots)
}

var pythonLoggingImportRoots = map[string]struct{}{
	"logging":   {},
	"loguru":    {},
	"structlog": {},
}

// ---------------------------------------------------------------------
// Per-library bare-name maps. Each entry MUST be unique to its gate
// (no overlap with stdlibBareNames or pythonBareNames — enforced by
// TestPythonPerImportGates_NoDuplicates).
// ---------------------------------------------------------------------

// pythonPandasBareNames — DataFrame/Series/ndarray surface receiver-
// stripped from `df.head()` / `s.apply(...)` / `arr.reshape(...)`.
// All names are pandas/numpy verbs that #94 keeps out of
// pythonBareNames because they collide with user methods (`head` is a
// common HTTP-handler name; `apply` is Function.apply in JS; `query`
// is a generic SQL helper on user repositories; `merge` is a generic
// reconciliation verb; `T` is a single-letter that would collide with
// type-vars). Gated to pandas/numpy/pyarrow imports.
var pythonPandasBareNames = map[string]struct{}{
	"head":     {},
	"tail":     {},
	"describe": {},
	"dtypes":   {},
	// `query`, `reshape`, `groupby` already in pythonBareNames.
	"merge":        {},
	"concat":       {},
	"pivot":        {},
	"melt":         {},
	"apply":        {},
	"applymap":     {},
	"transpose":    {},
	"squeeze":      {},
	"stack":        {},
	"unstack":      {},
	"asof":         {},
	"rolling":      {},
	"resample":     {},
	"pct_change":   {},
	"corr":         {},
	"cov":          {},
	"nlargest":     {},
	"nsmallest":    {},
	"value_counts": {},
}

// pythonRequestsBareNames — HTTP-verb surface receiver-stripped from
// `session.get(url)` / `client.post(...)` / `httpx.head(...)`. These
// are too generic to blanket-allow (collide with Django QuerySet
// `.get(pk=...)` and user CRUD methods named `post`/`put`/`delete`),
// but inside files that import requests / httpx / aiohttp they are
// overwhelmingly the HTTP-client form.
var pythonRequestsBareNames = map[string]struct{}{
	// `options` is in pythonBareNames as a SQLAlchemy verb already.
	"send":    {},
	"prepare": {},
	"mount":   {},
}

// pythonBoto3BareNames — AWS SDK client/resource surface receiver-
// stripped from `boto3.client('s3').get_object(...)` /
// `session.resource('s3')` / `client.put_object(...)`. `client` and
// `resource` are too generic to blanket-allow; the put_/get_/list_
// _object verbs collide with user CRUD methods.
var pythonBoto3BareNames = map[string]struct{}{
	"resource":                        {},
	"get_object":                      {},
	"put_object":                      {},
	"list_objects":                    {},
	"list_objects_v2":                 {},
	"delete_object":                   {},
	"head_object":                     {},
	"copy_object":                     {},
	"upload_file":                     {},
	"download_file":                   {},
	"upload_fileobj":                  {},
	"download_fileobj":                {},
	"generate_presigned_url":          {},
	"generate_presigned_post":         {},
	"generate_presigned_download_url": {}, // custom helper alias seen in fixture-a
	"get_paginator":                   {},
	"can_paginate":                    {},
	"admin_create_user":               {},
	"admin_get_user":                  {},
	"admin_delete_user":               {},
	"admin_update_user_attributes":    {},
	"admin_initiate_auth":             {},
	"admin_set_user_password":         {},
	"initiate_auth":                   {},
	"respond_to_auth_challenge":       {},
	"get_credential":                  {},
	"set_credential":                  {},
	"sign_request":                    {},
}

// pythonRedisBareNames — redis-client surface receiver-stripped from
// `r.get(key)` / `client.set(...)` / `r.pipeline()`. Generic verbs
// (`get`/`set`/`delete`/`keys`/`scan`) that collide with user methods
// across other codebases, gated to redis-importing files.
var pythonRedisBareNames = map[string]struct{}{
	"expire":     {},
	"persist":    {},
	"incr":       {},
	"decr":       {},
	"incrby":     {},
	"decrby":     {},
	"hset":       {},
	"hget":       {},
	"hgetall":    {},
	"hdel":       {},
	"hmset":      {},
	"hmget":      {},
	"hexists":    {},
	"sadd":       {},
	"srem":       {},
	"smembers":   {},
	"sismember":  {},
	"zadd":       {},
	"zrange":     {},
	"zrevrange":  {},
	"zincrby":    {},
	"lpush":      {},
	"rpush":      {},
	"lpop":       {},
	"rpop":       {},
	"lrange":     {},
	"llen":       {},
	"pipeline":   {},
	"publish":    {},
	"subscribe":  {},
	"psubscribe": {},
	"setex":      {},
	"setnx":      {},
	"ttl":        {},
	"pttl":       {},
	"flushdb":    {},
}

// pythonDjangoBareNames — Django ORM / DRF generic-verb surface
// receiver-stripped from `qs.first()` / `Model.objects.all()` /
// `qs.exists()` / `obj.save()` / `view.paginate_queryset(...)`. The
// generic English verbs (`first`/`last`/`all`/`count`/`exists`) are
// the dominant residual on client-fixture-a per Wave-7's deferred
// Track D analysis — too collision-prone to blanket-allow (every
// codebase has Order.first(), Cache.exists(), etc.), but inside
// files that import django or rest_framework they are overwhelmingly
// QuerySet/RelatedManager methods.
var pythonDjangoBareNames = map[string]struct{}{
	"first": {},
	"last":  {},
	// `earliest`, `latest`, `exists` already in pythonBareNames.
	"in_bulk":                 {},
	"explain":                 {},
	"reverse":                 {},
	"using":                   {},
	"only":                    {},
	"defer_loading":           {},
	"none":                    {},
	"raw":                     {},
	"force_authenticate":      {},
	"async_to_sync":           {},
	"sync_to_async":           {},
	"get_channel_layer":       {},
	"group_add":               {},
	"group_discard":           {},
	"make_aware":              {},
	"make_naive":              {},
	"localdate":               {},
	"localtime":               {},
	"paginate":                {},
	"paginate_queryset":       {},
	"get_paginated_response":  {},
	"select_for_update":       {},
	"qn":                      {},
	"setMessageParams":        {},
	"replace_email_variables": {},
}

// pythonFlaskBareNames — flask request/response decorator surface
// receiver-stripped from `app.before_request(...)` / `bp.errorhandler(...)`.
// Names already covered by pythonBareNames are NOT duplicated.
var pythonFlaskBareNames = map[string]struct{}{
	"before_app_request":       {},
	"after_app_request":        {},
	"teardown_app_request":     {},
	"before_app_first_request": {},
	"app_context_processor":    {},
	"url_defaults_filter":      {},
}

// pythonSQLAlchemyBareNames — session / engine / Column DSL receiver-
// stripped from `db.session.commit()` / `session.rollback()` /
// `Column(String)` / `relationship('User', backref=...)`. Generic
// verbs collide with user methods.
var pythonSQLAlchemyBareNames = map[string]struct{}{
	"Column":       {},
	"relationship": {},
	"commit":       {},
	"rollback":     {},
	// `flush` is in stdlibBareNames.
	"refresh":              {},
	"merge_session":        {},
	"add_all":              {},
	"bulk_save_objects":    {},
	"bulk_insert_mappings": {},
	"bulk_update_mappings": {},
	"begin_nested":         {},
	"close_all":            {},
	"expire":               {},
	"expire_all":           {},
	"expunge":              {},
	"expunge_all":          {},
	"savepoint":            {},
	"begin_transaction":    {},
}

// pythonMongoBareNames — pymongo Collection / Cursor verbs receiver-
// stripped from `coll.find(...)` / `cur.aggregate(...)`. Generic
// verbs (`find`/`insert`/`update`/`delete`/`count`/`aggregate`) that
// collide with user methods on repositories. `distinct` is already
// in pythonBareNames (uniquely pymongo) and not duplicated here.
var pythonMongoBareNames = map[string]struct{}{
	"find": {},
	// `insert` (stdlibBareNames), `update`/`aggregate`/`count`
	// (pythonBareNames) intentionally not duplicated here.
	"map_reduce": {},
}

// pythonCeleryBareNames — celery Task DSL receiver-stripped from
// `task.apply_async(...)` / `task.s(...)`. `delay` is already in
// pythonBareNames. Single-char `s` (signature) is python+celery
// gated only — strict cross-language gate prevents collision with
// throwaway-variable conventions elsewhere.
var pythonCeleryBareNames = map[string]struct{}{
	"apply_async": {},
	"si":          {}, // celery immutable signature
	"sig":         {},
	"chord":       {},
	"chain_task":  {},
	"group_task":  {},
	"chunks":      {},
	"retry":       {},
	"send_task":   {},
	"AsyncResult": {},
	"EagerResult": {},
}

// hasPythonReImport reports whether the file imports the stdlib `re`
// regex module. Activates the bare `sub`/`search`/`findall`/`match`/
// `fullmatch` surface — these are too generic to blanket-allow but
// inside files that import `re` they are overwhelmingly the regex
// module functions.
func hasPythonReImport(imports map[string]bool) bool {
	return pythonImportRootIn(imports, pythonReImportRoots)
}

var pythonReImportRoots = map[string]struct{}{
	"re":    {},
	"regex": {}, // third-party drop-in
}

// pythonReBareNames — receiver-stripped regex module functions.
// Names like `sub`, `search`, `match` are dominantly collision-prone
// (every Stripe-style API has `.search()`, every ORM has `.match()`),
// hence the gate.
var pythonReBareNames = map[string]struct{}{
	"sub":      {},
	"subn":     {},
	"search":   {},
	"findall":  {},
	"finditer": {},
	"match":    {},
	// `escape` is already in pythonBareNames (werkzeug.utils.escape).
	"purge":           {},
	"compile_pattern": {},
}

// hasPythonDBAPIImport reports whether the file imports a DB-API 2.0
// driver (sqlite3, psycopg2, mysql.connector, pymysql, cx_Oracle) or
// `django.db.connection`-derived code paths. Activates the DB-API
// cursor verbs (`execute`, `executemany`, `fetchall`, `fetchone`,
// `fetchmany`, `close`, `cursor`, `commit`, `rollback`) when the
// canonical driver is on the file.
//
// `django.db` is also included because in Django code `cursor()` /
// `execute()` on `django.db.connection` is the conventional shape.
// Note: `fetchall`/`fetchone`/`fetchmany` are already in
// pythonBareNames (added in wave-7 as distinctive DB-API verbs); the
// generic ones (`execute`, `cursor`, `close`) need a gate.
func hasPythonDBAPIImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		root := pythonImportRoot(p)
		if _, ok := pythonDBAPIImportRoots[root]; ok {
			return true
		}
		// django.db.* paths get treated as DB-API too
		if root == "django" {
			if p == "django.db" || strings.HasPrefix(p, "django.db.") {
				return true
			}
		}
	}
	return false
}

var pythonDBAPIImportRoots = map[string]struct{}{
	"sqlite3":           {},
	"psycopg2":          {},
	"psycopg":           {},
	"mysql":             {}, // mysql.connector
	"MySQLdb":           {}, // mysqlclient
	"pymysql":           {},
	"cx_Oracle":         {},
	"cx_oracle":         {},
	"oracledb":          {},
	"snowflake":         {}, // snowflake.connector
	"clickhouse_driver": {},
	"asyncpg":           {},
	"aiomysql":          {},
	"aiosqlite":         {},
	"pyodbc":            {},
	"pymssql":           {},
}

// pythonDBAPIDriverPlaceholder maps the file's DB-API driver import to the
// canonical external-package placeholder that names the concrete database
// engine (#2807). The DB-API cursor verbs (`execute`/`cursor`/...) are
// engine-agnostic, so the driver import is the only structural signal for
// which engine the code talks to. We return the driver's own package name
// (all entries below are on knownExternalPackages, so the resolver routes
// the edge to ExternalKnown rather than synthesising a junk ext:* node).
//
// When the file imports several drivers we prefer a concrete non-SQLite
// engine over sqlite3 (sqlite3 is the stdlib fallback and a no-op import in
// much code), so a file that uses both mysql.connector and sqlite3 is
// labelled by its server engine. When NO recognised driver is on the file
// we return the generic `python-dbapi` placeholder — there is no hard
// default to a concrete engine (the old code defaulted to sqlite3, which
// mislabelled every server-DB consumer).
func pythonDBAPIDriverPlaceholder(imports map[string]bool) string {
	const generic = "python-dbapi"
	if len(imports) == 0 {
		return generic
	}
	// Determinism (#5206): iterate the import set in sorted order rather than
	// in Go's randomised map-iteration order. A file that imports two concrete
	// server engines (e.g. pymysql AND psycopg2) used to pick whichever
	// placeholder happened to iterate FIRST, so the resolved ext:<driver>
	// CALLS target flipped between runs (mysql ↔ psycopg2) — non-deterministic
	// output that surfaced as a spurious flat-vs-M5 edge-set divergence (the
	// indexes are identical; the instability is here, not in the resolver
	// index). Sorting the imports makes the choice reproducible.
	roots := make([]string, 0, len(imports))
	for p := range imports {
		roots = append(roots, p)
	}
	sort.Strings(roots)
	best := ""
	for _, p := range roots {
		root := pythonImportRoot(p)
		// django.db.* paths are DB-API-shaped but carry no engine signal
		// (the engine lives in settings.DATABASES), so they leave `best`
		// untouched and only contribute the generic fallback.
		placeholder, ok := pythonDBAPIDriverPlaceholders[root]
		if !ok {
			continue
		}
		if best == "" {
			best = placeholder
			continue
		}
		// Prefer a concrete server engine over the sqlite3 stdlib fallback.
		// Among multiple concrete engines the first in sorted-import order
		// wins deterministically (the engine choice is heuristic anyway;
		// stability across runs is what matters for parity).
		if best == "sqlite3" && placeholder != "sqlite3" {
			best = placeholder
		}
	}
	if best == "" {
		return generic
	}
	return best
}

// pythonDBAPIDriverPlaceholders maps a driver import root to its canonical
// external-package placeholder. Every value is on knownExternalPackages so
// the resolver routes to ExternalKnown. Drivers for the same engine fold to
// one canonical name (e.g. every MySQL driver → "mysql") so downstream
// engine-label readers see a single stable token per engine.
var pythonDBAPIDriverPlaceholders = map[string]string{
	// MySQL family
	"mysql":    "mysql", // mysql.connector
	"MySQLdb":  "mysql", // mysqlclient
	"pymysql":  "mysql",
	"aiomysql": "mysql",
	// PostgreSQL family
	"psycopg2": "psycopg2",
	"psycopg":  "psycopg2",
	"asyncpg":  "psycopg2",
	// SQLite family
	"sqlite3":   "sqlite3",
	"aiosqlite": "sqlite3",
	// SQL Server family
	"pyodbc":  "pyodbc",
	"pymssql": "pyodbc",
	// Oracle family
	"cx_Oracle": "cx_Oracle",
	"cx_oracle": "cx_Oracle",
	"oracledb":  "cx_Oracle",
	// Snowflake / ClickHouse
	"snowflake":         "snowflake-connector-python",
	"clickhouse_driver": "clickhouse-driver",
}

// pythonDBAPIEngineLabel maps a canonical driver placeholder (as returned
// by pythonDBAPIDriverPlaceholder) to a human engine label. Used by
// engine-label readers and exercised by the driver-matrix test (#2807).
var pythonDBAPIEngineLabel = map[string]string{
	"mysql":                      "MySQL",
	"psycopg2":                   "PostgreSQL",
	"sqlite3":                    "SQLite",
	"pyodbc":                     "SQL Server",
	"cx_Oracle":                  "Oracle",
	"snowflake-connector-python": "Snowflake",
	"clickhouse-driver":          "ClickHouse",
	"python-dbapi":               "unknown",
}

// pythonDBAPIBareNames — receiver-stripped DB-API 2.0 verbs.
var pythonDBAPIBareNames = map[string]struct{}{
	"execute":       {},
	"executemany":   {},
	"executescript": {},
	"cursor":        {},
	"close":         {},
	// `fetchall`/`fetchone`/`fetchmany` already in pythonBareNames; not
	// duplicated here (would trip the no-duplicates test).
}

// hasPythonBs4Import reports whether the file imports BeautifulSoup
// (`bs4` / `BeautifulSoup`). Activates the soup-navigation verbs
// (`find_all`, `select`, `select_one`) — too generic to blanket-
// allow but distinctive inside a bs4-importing file.
func hasPythonBs4Import(imports map[string]bool) bool {
	return pythonImportRootIn(imports, pythonBs4ImportRoots)
}

var pythonBs4ImportRoots = map[string]struct{}{
	"bs4":           {},
	"BeautifulSoup": {},
	"lxml":          {},
	"html5lib":      {},
}

// pythonBs4BareNames — receiver-stripped BeautifulSoup verbs.
var pythonBs4BareNames = map[string]struct{}{
	"find_all":              {},
	"select":                {},
	"select_one":            {},
	"find_parent":           {},
	"find_parents":          {},
	"find_next":             {},
	"find_previous":         {},
	"find_next_sibling":     {},
	"find_previous_sibling": {},
	"find_all_next":         {},
	"find_all_previous":     {},
	"get_text":              {},
	"prettify":              {},
	"decompose":             {},
	"unwrap":                {},
	"extract":               {},
	"replace_with":          {},
	"insert_before":         {},
	"insert_after":          {},
	"xpath":                 {}, // lxml
}

// hasPythonUrllibImport reports whether the file imports
// `urllib.parse` / `urllib3` / urlparse. Activates `urljoin` /
// `urlparse` / `urlencode` / `quote` / `unquote` receiver-stripped
// forms.
func hasPythonUrllibImport(imports map[string]bool) bool {
	if len(imports) == 0 {
		return false
	}
	for p := range imports {
		root := pythonImportRoot(p)
		if root == "urllib" || root == "urllib3" || root == "yarl" {
			return true
		}
	}
	return false
}

// pythonUrllibBareNames — receiver-stripped urllib.parse helpers.
// Names like `quote`/`unquote` are generic — gated on import.
var pythonUrllibBareNames = map[string]struct{}{
	"urljoin":      {},
	"quote_plus":   {},
	"unquote_plus": {},
	"quote":        {},
	"unquote":      {},
	"urlencode":    {},
	"parse_qs":     {},
	"parse_qsl":    {},
}

// pythonLoggingBareNames — logging surface receiver-stripped from
// `logger.info(...)` / `log.error(...)` / `logger.exception(...)`.
// Generic English nouns (`info`/`warning`/`error`/`exception`/
// `debug`) that #94 rightly keeps out of pythonBareNames (every
// codebase has a class with an `error` or `info` method). With a
// `logging` import on the file the receiver-stripped form is
// overwhelmingly the stdlib-logger method.
//
// `log` (the verb) is intentionally EXCLUDED — too generic even
// when gated (collides with `Math.log`, user logging helpers, etc.).
// `error` IS included here because in python with a `logging` import
// it overwhelmingly refers to `logger.error(...)` — but only on a
// logging-imported file.
var pythonLoggingBareNames = map[string]struct{}{
	"info":    {},
	"warning": {},
	// `warn` already in pythonBareNames (stdlib helper).
	"error":     {},
	"exception": {},
	"critical":  {},
	"debug":     {},
	"log":       {},
}

// cppBareNames is the C/C++-language-gated bare-name stop-list (issue
// #44 — spdlog bug-rate reduction). After the C/C++ extractor strips
// the receiver from method calls (`logger->set_level(...)` →
// `set_level`, `std::make_shared<T>(...)` → `make_shared`,
// `v.emplace_back(x)` → `emplace_back`) the resolver sees a bare
// identifier and lands in bug-extractor. These are STL container /
// memory / chrono / algorithm / stream / locale / numeric helpers that
// are unambiguously std:: idioms — receiver-stripped from real spdlog,
// fmt, and Google Benchmark call sites.
//
// Conservative selection rule, mirroring goBareNames / rustBareNames:
// include a name only when it is (a) high-frequency in real C/C++
// codebases AND (b) overwhelmingly an STL/std symbol rather than a
// plausible user-defined method on a domain type. Generic English
// verbs (`add`, `remove`, `update`, `notify`) are intentionally
// omitted — too collision-prone with user methods on domain types
// (#94 safer-bias rule).
//
// Gated to lang=="cpp" || lang=="c" so the allowlist does not shadow
// same-named methods in Go / Rust / JS / etc.
var cppBareNames = map[string]struct{}{
	// <memory> — smart-pointer factories and casts.
	"make_shared":              {},
	"make_unique":              {},
	"allocate_shared":          {},
	"dynamic_pointer_cast":     {},
	"static_pointer_cast":      {},
	"const_pointer_cast":       {},
	"reinterpret_pointer_cast": {},
	"shared_from_this":         {},

	// <utility> — move / forward / pair helpers.
	"move":       {},
	"forward":    {},
	"make_pair":  {},
	"make_tuple": {},
	"tie":        {},

	// <algorithm> — algorithms that are overwhelmingly std::algo idioms.
	// Generic single-word verbs (`find`, `count`, `copy`, `sort`) are
	// intentionally omitted — collision-prone with user methods on
	// containers and domain types. Multi-word / suffixed forms below
	// are far more distinctive.
	"transform":               {},
	"accumulate":              {},
	"find_if":                 {},
	"find_if_not":             {},
	"count_if":                {},
	"copy_if":                 {},
	"copy_n":                  {},
	"remove_if":               {},
	"replace_if":              {},
	"for_each":                {},
	"all_of":                  {},
	"any_of":                  {},
	"none_of":                 {},
	"min_element":             {},
	"max_element":             {},
	"lower_bound":             {},
	"upper_bound":             {},
	"binary_search":           {},
	"equal_range":             {},
	"lexicographical_compare": {},

	// <iterator> — distinctive STL iterator helpers.
	"distance":              {},
	"advance":               {},
	"back_inserter":         {},
	"front_inserter":        {},
	"inserter":              {},
	"make_move_iterator":    {},
	"make_reverse_iterator": {},

	// <chrono> — duration / time-point helpers (heavy in spdlog benches).
	"duration_cast":         {},
	"time_point_cast":       {},
	"system_clock":          {},
	"steady_clock":          {},
	"high_resolution_clock": {},
	"nanoseconds":           {},
	"microseconds":          {},
	"milliseconds":          {},
	"seconds":               {},
	"minutes":               {},
	"hours":                 {},

	// <thread> / <this_thread> / <mutex> / <condition_variable> —
	// distinctive concurrency primitives. Generic `lock` / `unlock` /
	// `wait` are intentionally omitted (collide with user lockables).
	"sleep_for":                 {},
	"sleep_until":               {},
	"lock_guard":                {},
	"unique_lock":               {},
	"scoped_lock":               {},
	"shared_lock":               {},
	"notify_all_at_thread_exit": {},

	// <string> — multi-word search helpers that are unambiguously
	// std::string idioms. `find`, `size`, `empty`, `clear`, `compare`
	// are intentionally omitted (too collision-prone).
	"c_str":             {},
	"find_first_of":     {},
	"find_first_not_of": {},
	"find_last_of":      {},
	"find_last_not_of":  {},
	"npos":              {},
	"to_string":         {},
	"to_wstring":        {},
	"stoi":              {},
	"stol":              {},
	"stoll":             {},
	"stoul":             {},
	"stoull":            {},
	"stof":              {},
	"stod":              {},
	"stold":             {},

	// <vector> / <deque> / <list> / <map> / <set> container methods.
	// Aggressively included for cpp/c — these are overwhelmingly STL
	// container idioms in C++ codebases. The lang-gate (cpp || c) is
	// strict enough that same-named user methods in Go / Rust / JS
	// remain unshadowed. Within C++ a user container that defines
	// `push_back` / `begin` / `end` is almost always intentionally
	// modelling the STL container interface, so even a "shadowing"
	// classification is structurally honest.
	"push_back":     {},
	"pop_back":      {},
	"push_front":    {},
	"pop_front":     {},
	"emplace_back":  {},
	"emplace_front": {},
	"emplace_hint":  {},
	"shrink_to_fit": {},
	"begin":         {},
	"end":           {},
	"cbegin":        {},
	"cend":          {},
	"rbegin":        {},
	"rend":          {},
	"crbegin":       {},
	"crend":         {},
	"size":          {},
	"length":        {},
	"empty":         {},
	"clear":         {},
	"front":         {},
	"back":          {},
	"data":          {},
	"reserve":       {},
	"resize":        {},
	"capacity":      {},
	"at":            {},
	"swap":          {},
	"erase":         {},
	"insert":        {},
	"assign":        {},
	"find":          {},
	"count":         {},
	"contains":      {},
	"rfind":         {},
	"substr":        {},
	"append":        {},
	"compare":       {},
	"max_size":      {},

	// Smart-pointer / pointer-like methods (std::unique_ptr,
	// std::shared_ptr, std::weak_ptr). Methods like `get` / `reset`
	// / `release` / `lock` are extremely high-volume in modern C++
	// and almost always come from a smart-pointer receiver. Within
	// the cpp/c gate these are safe to claim.
	"get":       {},
	"reset":     {},
	"release":   {},
	"lock":      {},
	"unlock":    {},
	"try_lock":  {},
	"owns_lock": {},

	// <atomic> — atomic store/load helpers (heavy in spdlog async).
	"store":                   {},
	"load":                    {},
	"exchange":                {},
	"compare_exchange_strong": {},
	"compare_exchange_weak":   {},
	"fetch_add":               {},
	"fetch_sub":               {},
	"fetch_and":               {},
	"fetch_or":                {},
	"fetch_xor":               {},

	// <condition_variable> — wait/notify helpers.
	"wait":       {},
	"wait_for":   {},
	"wait_until": {},
	"notify_one": {},
	"notify_all": {},

	// <thread> instance methods.
	"join":     {},
	"detach":   {},
	"joinable": {},
	"get_id":   {},

	// <chrono> instance methods. (`count` already declared above as
	// std::count algorithm.)
	"now":              {},
	"time_since_epoch": {},
	"to_time_t":        {},
	"from_time_t":      {},

	// <fstream> / <iostream> instance methods (high-volume in C++).
	"flush":   {},
	"close":   {},
	"open":    {},
	"is_open": {},
	"good":    {},
	"bad":     {},
	"eof":     {},
	"fail":    {},
	"peek":    {},
	"tellg":   {},
	"tellp":   {},
	"seekg":   {},
	"seekp":   {},
	"read":    {},
	"write":   {},
	"put":     {},
	"sync":    {},
	"rdbuf":   {},
	"str":     {},

	// <type_traits> / <utility> common helpers.
	"declval":    {},
	"value_type": {},

	// std::function-like.
	"target":      {},
	"target_type": {},

	// <iostream> / <iomanip> / <fstream> — stream manipulators and
	// helpers that are unambiguously std stream idioms.
	"getline":      {},
	"setprecision": {},
	"setfill":      {},
	"setw":         {},
	"setbase":      {},
	"hex":          {},
	"oct":          {},
	"dec":          {},
	"fixed":        {},
	"scientific":   {},
	"boolalpha":    {},
	"noboolalpha":  {},
	"showbase":     {},
	"noshowbase":   {},
	"endl":         {},
	"ends":         {},
	"imbue":        {},

	// <exception> / <stdexcept> — standard exception constructors.
	"runtime_error":     {},
	"logic_error":       {},
	"invalid_argument":  {},
	"out_of_range":      {},
	"length_error":      {},
	"domain_error":      {},
	"overflow_error":    {},
	"underflow_error":   {},
	"system_error":      {},
	"bad_alloc":         {},
	"bad_cast":          {},
	"current_exception": {},
	"rethrow_exception": {},

	// <system_error> / errno helpers.
	"generic_category": {},
	"system_category":  {},
	"error_code":       {},
	"error_condition":  {},

	// C stdlib (gated to lang=="c" || lang=="cpp"). High-volume libc
	// names that are unambiguously stdio / stdlib / unistd symbols.
	"printf":    {},
	"fprintf":   {},
	"snprintf":  {},
	"sprintf":   {},
	"vprintf":   {},
	"vfprintf":  {},
	"vsnprintf": {},
	"vsprintf":  {},
	"fputs":     {},
	"fputc":     {},
	"fgets":     {},
	"fgetc":     {},
	"getc":      {},
	"putc":      {},
	"puts":      {},
	"fopen":     {},
	"fclose":    {},
	"fflush":    {},
	"fread":     {},
	"fwrite":    {},
	"fseek":     {},
	"ftell":     {},
	"feof":      {},
	"ferror":    {},
	"perror":    {},
	"strerror":  {},
	"strtol":    {},
	"strtoul":   {},
	"strtod":    {},
	"atoi":      {},
	"atol":      {},
	"atoll":     {},
	"atof":      {},
	"malloc":    {},
	"calloc":    {},
	"realloc":   {},
	// `free` intentionally omitted — too collision-prone with user
	// resource-release methods.
	"memcpy":  {},
	"memmove": {},
	"memset":  {},
	"memcmp":  {},
	"strcmp":  {},
	"strncmp": {},
	"strcpy":  {},
	"strncpy": {},
	"strcat":  {},
	"strncat": {},
	"strlen":  {},
	"strchr":  {},
	"strrchr": {},
	"strstr":  {},
	"strtok":  {},
	"isdigit": {},
	"isalpha": {},
	"isalnum": {},
	"isspace": {},
	"isupper": {},
	"islower": {},
	"tolower": {},
	"toupper": {},

	// std::string_view and to_string_view conversion (heavy in fmt /
	// spdlog formatters).
	"string_view":       {},
	"to_string_view":    {},
	"basic_string_view": {},

	// `decltype` — C++ specifier that tree-sitter sometimes parses
	// into a call-like node. Gated via cpp/c so it never leaks
	// elsewhere. Routing it out of bug-extractor is preferable to
	// inventing a phantom placeholder for a keyword.
	"decltype": {},

	// POSIX socket / system-call surface (spdlog/sinks/tcp_sink,
	// udp_sink, syslog_sink). Distinctive POSIX names virtually never
	// user-defined.
	"setsockopt":      {},
	"getsockopt":      {},
	"sendto":          {},
	"recvfrom":        {},
	"bind":            {},
	"listen":          {},
	"accept":          {},
	"connect":         {},
	"send":            {},
	"recv":            {},
	"socket":          {},
	"poll":            {},
	"htons":           {},
	"htonl":           {},
	"ntohs":           {},
	"ntohl":           {},
	"inet_addr":       {},
	"inet_pton":       {},
	"inet_ntop":       {},
	"getaddrinfo":     {},
	"freeaddrinfo":    {},
	"gethostbyname":   {},
	"gethostname":     {},
	"closesocket":     {},
	"WSAStartup":      {},
	"WSACleanup":      {},
	"WSAGetLastError": {},
	// `shutdown` / `select` already declared elsewhere or omitted
	// (`shutdown` is added below in the spdlog section; `select` is
	// too collision-prone with user methods to add unconditionally).

	// spdlog public API (issue #44). Distinctive spdlog top-level
	// free functions and Logger methods receiver-stripped from
	// `spdlog::xxx()` / `logger->xxx()`. Gated to cpp/c.
	"set_pattern":                {},
	"set_level":                  {},
	"set_default_logger":         {},
	"default_logger":             {},
	"default_logger_raw":         {},
	"enable_backtrace":           {},
	"disable_backtrace":          {},
	"dump_backtrace":             {},
	"flush_every":                {},
	"flush_on":                   {},
	"apply_all":                  {},
	"register_logger":            {},
	"initialize_logger":          {},
	"get_logger":                 {},
	"set_error_handler":          {},
	"set_automatic_registration": {},
	"set_formatter":              {},
	"load_env_levels":            {},
	"load_argv_levels":           {},
	"set_levels":                 {},
	"to_hex":                     {},
	"sleep_for_millis":           {},
	"backend_sink_it_":           {},
	"backend_flush_":             {},
	"should_flush_":              {},
	"post_log":                   {},
	"post_flush":                 {},
	"shutdown":                   {},
	// spdlog details / sinks internals — high-volume helpers in
	// include/spdlog/details and include/spdlog/sinks. Distinctive
	// names with the spdlog snake_case convention.
	"path_exists":                 {},
	"append_string_view":          {},
	"split_by_extension":          {},
	"fwrite_bytes":                {},
	"filename_to_str":             {},
	"wstr_to_utf8buf":             {},
	"utf8_to_wstrbuf":             {},
	"throw_winsock_error_":        {},
	"throw_spdlog_ex":             {},
	"win32_error":                 {},
	"is_connected":                {},
	"reopen":                      {},
	"truncate_":                   {},
	"flush_":                      {},
	"tp_mutex":                    {},
	"tp_lock":                     {},
	"get_tp":                      {},
	"set_tp":                      {},
	"init_thread_pool":            {},
	"create_async":                {},
	"create_async_nb":             {},
	"source_loc":                  {},
	"log_msg":                     {},
	"backtracer":                  {},
	"periodic_worker":             {},
	"file_helper":                 {},
	"pattern_formatter":           {},
	"circular_q":                  {},
	"mpmc_blocking_q":             {},
	"null_atomic_int":             {},
	"udp_client":                  {},
	"tcp_client":                  {},
	"connect_socket_with_timeout": {},
	"init_winsock_":               {},
	"cleanup_":                    {},
	"fopen_s":                     {},
	"filesize":                    {},
	"fsync":                       {},
	"dir_name":                    {},
	"before_open":                 {},
	"after_open":                  {},
	"before_close":                {},
	"after_close":                 {},
	"filename_t":                  {},
	"iequals":                     {},
	"to_lower_":                   {},
	"trim_":                       {},
	"extract_kv_":                 {},
	"from_str":                    {},
	"token_stream":                {},
	"load_levels":                 {},
	"copy_moveable":               {},
	"reset_overrun_counter":       {},
	"overrun_counter":             {},
	"max_items_":                  {},
	"max_files_":                  {},
	"event_handlers_":             {},
	"worker_loop_":                {},
	"logger_name":                 {},
	"callback_fun":                {},
	"get_thread":                  {},
	"get_flusher":                 {},
	"flush_all":                   {},
	"sleep_for_millis_":           {},
	"requeue_log_msg":             {},

	// libc time / locale (POSIX). Distinctive names virtually never
	// user-defined.
	"localtime":   {},
	"localtime_r": {},
	"localtime_s": {},
	"gmtime":      {},
	"gmtime_r":    {},
	"gmtime_s":    {},
	"mktime":      {},
	"strftime":    {},
	"strptime":    {},
	"asctime":     {},
	"ctime":       {},
	"clock":       {},
	"difftime":    {},
	"tzset":       {},
	"timegm":      {},

	// Win32 API surface (heavy in spdlog/sinks/wincolor_sink,
	// windows_sink, msvc_sink). Distinctive names with Win32
	// PascalCase / UPPER_SNAKE_CASE conventions.
	"GetLastError":               {},
	"SetLastError":               {},
	"FormatMessageA":             {},
	"FormatMessageW":             {},
	"MAKELANGID":                 {},
	"GetStdHandle":               {},
	"SetConsoleTextAttribute":    {},
	"GetConsoleScreenBufferInfo": {},
	"WriteFile":                  {},
	"ReadFile":                   {},
	"CreateFileA":                {},
	"CreateFileW":                {},
	"CloseHandle":                {},
	"OutputDebugStringA":         {},
	"OutputDebugStringW":         {},
	"GetCurrentProcessId":        {},
	"GetCurrentThreadId":         {},
	"GetCurrentProcess":          {},
	"GetCurrentThread":           {},
	"MultiByteToWideChar":        {},
	"WideCharToMultiByte":        {},
	"LocalFree":                  {},
	"FD_SET":                     {},
	"FD_ZERO":                    {},
	"FD_ISSET":                   {},
	"FD_CLR":                     {},

	// fmt-library typedefs / aliases used as bare names in
	// instantiations.
	"string_view_t":  {},
	"wstring_view_t": {},

	// spdlog wave (issue #44 follow-up) — additional POSIX / libc / Win32 /
	// systemd / pthread / android-log surface that survives as bug-extractor
	// in real spdlog corpora. Distinctive POSIX / Win32 / journal names that
	// are essentially never user-defined.
	//
	// POSIX file / fd / dir surface.
	"mkdir":           {},
	"rename":          {},
	"truncate":        {},
	"opendir":         {},
	"readdir":         {},
	"closedir":        {},
	"fcntl":           {},
	"fdopen":          {},
	"fileno":          {},
	"fstat":           {},
	"fstat64":         {},
	"stat":            {},
	"setvbuf":         {},
	"basename":        {},
	"fwrite_unlocked": {},
	"put_time":        {},

	// POSIX env / time / process / select.
	"setenv":        {},
	"unsetenv":      {},
	"clock_gettime": {},
	"select":        {},

	// MSVC / Windows CRT (_-prefixed names; almost never user-defined).
	"_stat":          {},
	"_fileno":        {},
	"_filelength":    {},
	"_filelengthi64": {},
	"_fwrite_nolock": {},
	"_mkdir":         {},
	"_isatty":        {},
	"_get_osfhandle": {},
	"_wfsopen":       {},
	"_fsopen":        {},
	"_wstat":         {},
	"_wrename":       {},
	"_wremove":       {},
	"_wmkdir":        {},
	"_putenv_s":      {},
	"_dupenv_s":      {},
	"_tzset":         {},
	"_mkgmtime":      {},

	// POSIX thread / pid identifiers.
	"getpid":                 {},
	"getthrid":               {},
	"_lwp_self":              {},
	"_thread_id":             {},
	"pthread_self":           {},
	"pthread_threadid_np":    {},
	"pthread_mach_thread_np": {},
	"pthread_getthreadid_np": {},
	"pthread_getthrds_np":    {},
	"thr_self":               {},

	// systemd journal API — gated cpp/c via this stop-list; sd_journal_*
	// is distinctive and only ever the libsystemd surface.
	"sd_journal_open":                     {},
	"sd_journal_open_namespace":           {},
	"sd_journal_close":                    {},
	"sd_journal_next":                     {},
	"sd_journal_get_data":                 {},
	"sd_journal_get_realtime_usec":        {},
	"sd_journal_seek_realtime_usec":       {},
	"sd_journal_send":                     {},
	"sd_journal_stream_fd":                {},
	"sd_journal_stream_fd_with_namespace": {},
	"sd_journal_wait":                     {},
	"sd_journal_add_match":                {},
	"openlog":                             {},

	// Android log API.
	"__android_log_write":     {},
	"__android_log_buf_write": {},

	// POSIX socket / inet helpers not yet declared above.
	"ioctlsocket":  {},
	"inet_aton":    {},
	"gai_strerror": {},

	// libc misc — assert / static_assert / exit / memchr / character class.
	"assert":        {},
	"static_assert": {},
	"exit":          {},
	"memchr":        {},
	"isatty":        {},
	"isprint":       {},
	"to_chars":      {},

	// STL types occasionally surfaced bare (initializer_list constructor,
	// numeric_limits<T>::max bare-receiver, std::equal algorithm).
	"initializer_list":    {},
	"numeric_limits":      {},
	"istreambuf_iterator": {},
	"equal":               {},

	// Win32 PascalCase API — distinctive names virtually never user-defined.
	// (Existing Win32 section above covers GetStdHandle, CreateFileA, etc.)
	"Sleep":                {},
	"ZeroMemory":           {},
	"MAKEWORD":             {},
	"WSASetLastError":      {},
	"IsValidSid":           {},
	"IsDebuggerPresent":    {},
	"InetPtonA":            {},
	"GetFullPathNameA":     {},
	"GetFullPathNameW":     {},
	"GetDriveTypeA":        {},
	"GetConsoleMode":       {},
	"FlushFileBuffers":     {},
	"FindFirstFileA":       {},
	"FindNextFileA":        {},
	"FindClose":            {},
	"SetHandleInformation": {},
	"OpenProcessToken":     {},
	"RegisterEventSourceA": {},
	"OpenEventLogA":        {},
	"ReadEventLogA":        {},
	"CloseEventLog":        {},

	// Smart-pointer type constructors invoked as bare names (e.g.
	// `unique_ptr<T>{ ... }`).
	"unique_ptr": {},
	"shared_ptr": {},
	"weak_ptr":   {},
	"auto_ptr":   {},
}

// cppStlHeaders is the set of C++ standard-library header names that
// appear as bare-name IMPORTS edges after the cpp extractor parses
// `#include <iostream>` style directives. These collapse to a single
// `ext:std` placeholder — every STL header is part of the std namespace
// and one placeholder per std lib matches the package-per-import
// collapse used elsewhere. Lang-gated to cpp / c via the call site.
var cppStlHeaders = map[string]struct{}{
	// C++ stdlib (selected, high-volume in real corpora).
	"iostream":           {},
	"iomanip":            {},
	"fstream":            {},
	"sstream":            {},
	"ostream":            {},
	"istream":            {},
	"streambuf":          {},
	"ios":                {},
	"iosfwd":             {},
	"string":             {},
	"string_view":        {},
	"vector":             {},
	"array":              {},
	"deque":              {},
	"list":               {},
	"forward_list":       {},
	"map":                {},
	"set":                {},
	"unordered_map":      {},
	"unordered_set":      {},
	"queue":              {},
	"stack":              {},
	"bitset":             {},
	"memory":             {},
	"memory_resource":    {},
	"new":                {},
	"utility":            {},
	"tuple":              {},
	"functional":         {},
	"algorithm":          {},
	"numeric":            {},
	"iterator":           {},
	"chrono":             {},
	"thread":             {},
	"mutex":              {},
	"shared_mutex":       {},
	"condition_variable": {},
	"future":             {},
	"atomic":             {},
	"exception":          {},
	"stdexcept":          {},
	"system_error":       {},
	"typeinfo":           {},
	"type_traits":        {},
	"limits":             {},
	"locale":             {},
	"codecvt":            {},
	"random":             {},
	"ratio":              {},
	"complex":            {},
	"valarray":           {},
	"variant":            {},
	"optional":           {},
	"any":                {},
	"filesystem":         {},
	"regex":              {},
	"cassert":            {},
	"cctype":             {},
	"cerrno":             {},
	"cfloat":             {},
	"climits":            {},
	"cmath":              {},
	"csignal":            {},
	"cstdarg":            {},
	"cstddef":            {},
	"cstdint":            {},
	"cstdio":             {},
	"cstdlib":            {},
	"cstring":            {},
	"ctime":              {},
	"cwchar":             {},
	"cwctype":            {},
	"concepts":           {},
	"ranges":             {},
	"span":               {},
	"charconv":           {},
	"bit":                {},
	"compare":            {},
	"coroutine":          {},
	"source_location":    {},
	"version":            {},
	// C stdlib headers (POSIX + libc), used by C extractor.
	"stdio.h":      {},
	"stdlib.h":     {},
	"string.h":     {},
	"strings.h":    {},
	"ctype.h":      {},
	"errno.h":      {},
	"math.h":       {},
	"time.h":       {},
	"unistd.h":     {},
	"fcntl.h":      {},
	"signal.h":     {},
	"stdarg.h":     {},
	"stddef.h":     {},
	"stdint.h":     {},
	"stdbool.h":    {},
	"assert.h":     {},
	"limits.h":     {},
	"float.h":      {},
	"locale.h":     {},
	"setjmp.h":     {},
	"inttypes.h":   {},
	"pthread.h":    {},
	"sys/types.h":  {},
	"sys/stat.h":   {},
	"sys/time.h":   {},
	"sys/socket.h": {},
}

// isKnownExternalPackage reports whether s matches our small allowlist
// of well-known third-party packages and stdlib top-level modules. The
// allowlist is intentionally narrow for v1.0 — false positives turn a
// local name into a placeholder, which is worse than missing one.
func isKnownExternalPackage(s string) bool {
	lower := strings.ToLower(s)
	// Issue #424 — every "docker:<repo>" placeholder corresponds to a real
	// image in a container registry. Treat the entire docker namespace as
	// allowlisted; ExternalKnown is the right disposition for image refs
	// regardless of whether the repo is on the static package allowlist.
	if strings.HasPrefix(lower, "docker:") {
		return true
	}
	// Refs #44 — every "gha:<org>/<repo>" placeholder corresponds to a real
	// GitHub Actions marketplace entry. Treat the entire gha namespace as
	// allowlisted; ExternalKnown is the right disposition for action refs.
	if strings.HasPrefix(lower, "gha:") {
		return true
	}
	if _, ok := knownExternalPackages[lower]; ok {
		return true
	}
	// Scoped npm fallback: a full "@scope/pkg" key matches if the bare
	// "@scope" key is on the allowlist. This lets us keep the existing
	// scope-level entries (@radix-ui, @tanstack, ...) functional for
	// every package they ship without enumerating each one. The scope
	// must be non-empty and start with '@' (issue #71).
	if strings.HasPrefix(lower, "@") {
		if slash := strings.IndexByte(lower, '/'); slash > 1 {
			scope := lower[:slash]
			if _, ok := knownExternalPackages[scope]; ok {
				return true
			}
		}
	}
	return false
}

// IsKnownExternalPackage is the exported form of the allowlist check.
// VERIFY-2-PREP / issue #56: the resolver consults this to decide
// whether an "ext:<pkg>" placeholder should be tagged ExternalKnown
// (allowlisted, expected) or ExternalUnknown (real external dep we
// haven't catalogued yet). Comparison is case-folded.
func IsKnownExternalPackage(s string) bool {
	return isKnownExternalPackage(s)
}

// knownExternalPackages is the v1.1 allowlist. Lowercase keys; lookups
// are case-folded. Curated from real codebases — Python web/data
// stacks, JS/TS frontend + node, Go services, JVM enterprise. False
// positives synthesise a placeholder for what might have been a local
// name; bias toward names extremely unlikely to collide.
var knownExternalPackages = map[string]struct{}{
	// Python ecosystem (third-party)
	"django":         {},
	"rest_framework": {},
	"drf":            {},
	"flask":          {},
	"fastapi":        {},
	"sqlalchemy":     {},
	"alembic":        {},
	"pydantic":       {},
	"celery":         {},
	"requests":       {},
	"httpx":          {},
	"numpy":          {},
	"pandas":         {},
	"scipy":          {},
	// pandas wave — pyarrow is the pandas Arrow backend; cython is
	// the build-time accelerator imported by pandas/_libs/*.pyx
	// generated stubs. Both arrive as top-level `import pyarrow as pa`
	// / `import cython` and as submodule chains
	// (`pyarrow.compute`, `pyarrow.types`, `cython.parallel`).
	"pyarrow":           {},
	"cython":            {},
	"pytest":            {},
	"mypy":              {},
	"attrs":             {},
	"click":             {},
	"redis":             {},
	"boto3":             {},
	"awswrangler":       {},
	"typing_extensions": {},
	// Wave-4 (Django + Flask + Mongo + Postgres real-world). Python
	// drivers and frameworks the resolver currently tags external-
	// unknown because the allowlist had no row for them.
	"pymongo":     {},
	"motor":       {},
	"bson":        {},
	"psycopg2":    {},
	"psycopg":     {},
	"asyncpg":     {},
	"mysqlclient": {},
	"pymysql":     {},
	// DB-API driver placeholders (#2807). pythonDBAPIDriverPlaceholder
	// folds cursor-verb call sites to these so the resolver routes to
	// ExternalKnown rather than synthesising an ext:<verb> node, and the
	// engine label reads the concrete driver. mysql/psycopg2/sqlite3 are
	// already listed above/elsewhere; these are the remaining engines.
	"pyodbc":                     {}, // SQL Server (also pymssql folds here)
	"cx_oracle":                  {}, // Oracle (cx_Oracle / oracledb fold here)
	"snowflake-connector-python": {}, // Snowflake
	"clickhouse-driver":          {}, // ClickHouse
	"python-dbapi":               {}, // generic fallback: driver not recognised
	"flask_sqlalchemy":           {},
	"flask_jwt_extended":         {},
	"flask_jwt":                  {},
	"flask_login":                {},
	"flask_migrate":              {},
	"flask_caching":              {},
	"flask_cache":                {},
	"flask_restful":              {},
	"flask_restplus":             {},
	"flask_restx":                {},
	"flask_marshmallow":          {},
	"flask_apispec":              {},
	"flask_bcrypt":               {},
	"flask_cors":                 {},
	"flask_wtf":                  {},
	"flask_mail":                 {},
	"flask_session":              {},
	"flask_socketio":             {},
	"marshmallow":                {},
	"marshmallow_sqlalchemy":     {},
	"webargs":                    {},
	"werkzeug":                   {},
	"jinja2":                     {},
	"itsdangerous":               {},
	"pyjwt":                      {},
	"passlib":                    {},
	"cryptography":               {},
	"arrow":                      {},
	"pendulum":                   {},
	"dateutil":                   {},
	"factory_boy":                {},
	"decouple":                   {},
	"python_dotenv":              {},
	"environs":                   {},
	"dynaconf":                   {},
	"hypothesis":                 {},
	"pytest_factoryboy":          {},
	"pytest_django":              {},
	"pytest_flask":               {},
	"pytest_mock":                {},
	"pytest_asyncio":             {},
	"webtest":                    {},
	"responses":                  {},
	"vcr":                        {},
	"sentry_sdk":                 {},
	"structlog":                  {},
	"loguru":                     {},
	"tenacity":                   {},
	"backoff":                    {},
	"prometheus_client":          {},
	"aiohttp":                    {},
	"aiofiles":                   {},
	"uvicorn":                    {},
	"gunicorn":                   {},
	"starlette":                  {},
	"orjson":                     {},
	"ujson":                      {},
	"msgpack":                    {},
	"yaml":                       {},
	"pyyaml":                     {},
	"toml":                       {},
	"tomllib":                    {},
	"tomli":                      {},
	"lxml":                       {},
	"bs4":                        {},
	"beautifulsoup4":             {},
	"pillow":                     {},
	"pil":                        {},
	"openpyxl":                   {},
	"xlrd":                       {},
	"matplotlib":                 {},
	"seaborn":                    {},
	"sklearn":                    {},
	"scikit_learn":               {},
	"torch":                      {},
	"tensorflow":                 {},
	"transformers":               {},
	"langchain":                  {},
	"openai":                     {},
	"anthropic":                  {},
	// Python stdlib top-level
	"os":              {},
	"sys":             {},
	"json":            {},
	"re":              {},
	"typing":          {},
	"datetime":        {},
	"collections":     {},
	"asyncio":         {},
	"concurrent":      {},
	"multiprocessing": {},
	"threading":       {},
	"queue":           {},
	"weakref":         {},
	"logging":         {},
	"pathlib":         {},
	"functools":       {},
	"itertools":       {},
	"operator":        {},
	"builtins":        {},
	"unittest":        {},
	"abc":             {},
	"enum":            {},
	"uuid":            {},
	"hashlib":         {},
	"dataclasses":     {},
	"contextlib":      {},
	"warnings":        {},
	"tempfile":        {},
	"subprocess":      {},
	"argparse":        {},
	"socket":          {},
	"ssl":             {},
	"urllib":          {},
	// Wave-7 stdlib additions (client-fixture-a residual).
	"csv":         {},
	"contextvars": {},
	"decimal":     {},
	"email":       {},
	"random":      {},
	"traceback":   {},
	"importlib":   {},
	// Wave-8 stdlib additions — comprehensive Python stdlib coverage
	// addressing issue #44 (fixture-a Python bug-rate 2.65%).
	// Covers numeric/math, sequence, compression, configuration,
	// database, network, and XML/HTML parsing.
	"cmath":        {}, // complex number math
	"difflib":      {}, // diff operations (fixture-a residual)
	"fractions":    {}, // rational number arithmetic
	"statistics":   {}, // statistics functions
	"shelve":       {}, // persistent object storage
	"dbm":          {}, // DBM database
	"ftplib":       {}, // FTP client
	"smtplib":      {}, // SMTP client
	"nntplib":      {}, // NNTP client
	"poplib":       {}, // POP3 client
	"imaplib":      {}, // IMAP4 client
	"telnetlib":    {}, // Telnet client
	"binascii":     {}, // binary/ASCII conversion
	"quopri":       {}, // Quoted-Printable encoding
	"uu":           {}, // UU encoding
	"formatter":    {}, // text formatting
	"stringprep":   {}, // string preparation
	"readline":     {}, // GNU readline interface
	"rlcompleter":  {}, // readline completer
	"cmd":          {}, // command-line interfaces
	"configparser": {}, // configuration file parsing
	"netrc":        {}, // .netrc file parsing
	"xdrlib":       {}, // XDR data marshalling
	"plistlib":     {}, // plist file format
	"bdb":          {}, // debugger base
	"profile":      {}, // Python profiler
	"cProfile":     {}, // C extension profiler
	"timeit":       {}, // measure execution time
	"trace":        {}, // trace module execution
	"atexit":       {}, // exit handlers
	"tracemalloc":  {}, // trace memory allocations
	// Click wave — Python stdlib roots seen as bug-resolver IMPORTS
	// targets in click's own source (`from gettext import gettext as _`,
	// `import codecs`, `import errno`, `import inspect`, `import shutil`,
	// `import shlex`, `import stat`, `import textwrap`, `import msvcrt`,
	// `import platform`, `import pdb`, `import ctypes`, `import struct`,
	// `import string`, `import signal`, `import types`). Each module is
	// a CPython stdlib top-level; they collide with no user-package
	// names in practice (Python convention reserves these via PEP 8
	// import style). Routes the click IMPORTS edges out of bug-resolver.
	// Notes:
	//   - `copy` is intentionally omitted here. The
	//     TestStdlibBareNames_NoCollisionNames test fixture creates a
	//     bare relationship with `ToID: "copy"` and asserts NO
	//     synthesis; adding `copy` to the package allowlist would still
	//     route it (via the Format-A structural-ref path) and trip the
	//     fixture. `copy` users in click import as `import copy` which
	//     reaches the IMPORTS path through external-unknown — accepted.
	//   - `select` is intentionally omitted here. It is already gated
	//     for ruby via rubyBareNames and adding it to the global
	//     allowlist would let it match cross-language.
	"gettext":   {},
	"codecs":    {},
	"errno":     {},
	"inspect":   {},
	"shutil":    {},
	"shlex":     {},
	"stat":      {},
	"textwrap":  {},
	"msvcrt":    {},
	"platform":  {},
	"pdb":       {},
	"ctypes":    {},
	"termios":   {},
	"selectors": {},
	"struct":    {},
	"string":    {},
	"signal":    {},
	"types":     {},
	"fnmatch":   {},
	"gc":        {},
	"linecache": {},
	"mimetypes": {},
	"getpass":   {},
	"pickle":    {},
	"secrets":   {},
	"bisect":    {},
	"heapq":     {},
	"array":     {},
	"gzip":      {},
	"zipfile":   {},
	"tarfile":   {},
	// Click wave — third-party Python ANSI/terminal-color shim heavily
	// used by click on Windows (`import colorama`). Pure-Python package
	// shipped on PyPI as the canonical click dependency.
	"colorama": {},
	// Wave-8 third-party additions (fixture-a residual & common testing/imaging).
	"cv2":       {}, // OpenCV
	"pdf2image": {}, // PDF to image conversion
	"coreapi":   {}, // Django REST coreapi client
	// Polyglot-platform corpus additions (bug-rate experiment 2026-05-23).
	// These packages appeared as unresolved IMPORTS on the polyglot-platform
	// group (27.0% unresolved rate, 372/1377 edges) and were missing from the
	// allowlist, causing the resolver to tag them ExternalUnknown / bug-extractor.
	// Note: opentelemetry, grpc, and kafka already exist later in this map for
	// Scala/Go uses — they are omitted here to avoid duplicate-key errors.
	"airflow":               {}, // Apache Airflow: `from airflow import DAG`, etc.
	"strawberry":            {}, // Strawberry GraphQL: `import strawberry`, etc.
	"aio_pika":              {}, // Async AMQP/RabbitMQ: `import aio_pika`
	"hvac":                  {}, // HashiCorp Vault client: `import hvac`
	"pgvector":              {}, // pgvector Postgres extension: `from pgvector.psycopg import ...`
	"sentence_transformers": {}, // Sentence Transformers ML: `from sentence_transformers import ...`
	// Wave-7 third-party additions (Django/Channels/AWS/PDF/Excel
	// stack from client-fixture-a residual).
	"asgiref":                  {},
	"channels":                 {},
	"botocore":                 {},
	"django_celery_beat":       {},
	"django_filters":           {},
	"django_redis":             {},
	"docx":                     {}, // python-docx (`from docx import ...`)
	"fitz":                     {}, // PyMuPDF (`import fitz`)
	"pdfplumber":               {},
	"pytz":                     {},
	"pgcrypto":                 {},
	"mysql":                    {}, // `mysql.connector`
	"environ":                  {}, // django-environ (`from environ import ...`)
	"daphne":                   {},
	"social_django":            {},
	"social_core":              {},
	"corsheaders":              {},
	"django_extensions":        {},
	"django_storages":          {},
	"storages":                 {},
	"whitenoise":               {},
	"silk":                     {},
	"debug_toolbar":            {},
	"reversion":                {},
	"taggit":                   {},
	"mptt":                     {},
	"oauth2_provider":          {},
	"allauth":                  {},
	"dj_database_url":          {},
	"drf_yasg":                 {},
	"drf_spectacular":          {},
	"phonenumber_field":        {},
	"phonenumbers":             {},
	"djoser":                   {},
	"rest_framework_simplejwt": {},
	"simplejwt":                {},
	"xlsxwriter":               {},
	"reportlab":                {},
	"weasyprint":               {},
	"PyPDF2":                   {},
	"pypdf":                    {},
	// JS / TS ecosystem (unscoped)
	"react":        {},
	"vue":          {},
	"angular":      {},
	"jquery":       {},
	"bootstrap":    {},
	"lodash":       {},
	"ramda":        {},
	"immer":        {},
	"dayjs":        {},
	"date-fns":     {},
	"axios":        {},
	"ky":           {},
	"express":      {},
	"next":         {},
	"jest":         {},
	"vitest":       {},
	"mocha":        {},
	"chai":         {},
	"sinon":        {},
	"supertest":    {},
	"typescript":   {},
	"zod":          {},
	"prisma":       {},
	"redux":        {},
	"rxjs":         {},
	"tanstack":     {},
	"nodemailer":   {},
	"bcrypt":       {},
	"jsonwebtoken": {},
	"helmet":       {},
	"multer":       {},
	"faker":        {},
	// Issue #44 / GraphQL-fix — apollo-server bug-rate residue. JS/TS
	// GraphQL ecosystem deps and Node.js stdlib modules that appear as
	// bare-name IMPORTS targets across the apollo-server monorepo. These
	// are real external packages with no in-tree entity; the resolver
	// was tagging them BugExtractor instead of ExternalKnown.
	"graphql":                    {}, // graphql-js reference impl
	"graphql-tag":                {}, // gql`` template literal helper
	"graphql-subscriptions":      {},
	"loglevel":                   {},
	"nock":                       {}, // HTTP mocking lib
	"whatwg-mimetype":            {},
	"async-listener":             {},
	"cls-hooked":                 {},
	"long":                       {},
	"make-fetch-happen":          {},
	"lru-cache":                  {},
	"negotiator":                 {},
	"async-retry":                {},
	"jest-serializer-html":       {},
	"jest-mock":                  {},
	"jest-environment-node":      {},
	"jest-environment-jsdom":     {},
	"prettier":                   {},
	"eslint":                     {},
	"webpack":                    {},
	"rollup":                     {},
	"vite":                       {},
	"ts-node":                    {},
	"esbuild":                    {},
	"chalk":                      {},
	"commander":                  {},
	"yargs":                      {},
	"glob":                       {},
	"semver":                     {},
	"ws":                         {},
	"cors":                       {},
	"body-parser":                {},
	"cookie":                     {},
	"cookie-parser":              {},
	"morgan":                     {},
	"debug":                      {},
	"dotenv":                     {},
	"node-fetch":                 {},
	"undici":                     {},
	"form-data":                  {},
	"qs":                         {},
	"mime-types":                 {},
	"compression":                {},
	"connect":                    {},
	"on-finished":                {},
	"send":                       {},
	"raw-body":                   {},
	"http-errors":                {},
	"accepts":                    {},
	"type-is":                    {},
	"content-type":               {},
	"content-disposition":        {},
	"fast-json-stable-stringify": {},
	"json-stable-stringify":      {},
	"deep-equal":                 {},
	"fast-deep-equal":            {},
	// Node.js stdlib modules (additional to existing console/readline/
	// assert/domain/url/net). Real node imports that node:<mod>-shaped
	// or bare-shaped — both forms case-fold to the same allowlist key.
	// `zlib` lives in the C/C++ third-party block below (shared key).
	"stream":             {},
	"buffer":             {},
	"events":             {},
	"util":               {},
	"querystring":        {},
	"child_process":      {},
	"cluster":            {},
	"dgram":              {},
	"dns":                {},
	"fs":                 {},
	"http2":              {},
	"https":              {},
	"module":             {},
	"perf_hooks":         {},
	"process":            {},
	"punycode":           {},
	"repl":               {},
	"string_decoder":     {},
	"timers":             {},
	"tls":                {},
	"tty":                {},
	"v8":                 {},
	"vm":                 {},
	"worker_threads":     {},
	"inspector":          {},
	"trace_events":       {},
	"node:fs":            {},
	"node:path":          {},
	"node:url":           {},
	"node:util":          {},
	"node:stream":        {},
	"node:zlib":          {},
	"node:crypto":        {},
	"node:assert":        {},
	"node:buffer":        {},
	"node:events":        {},
	"node:http":          {},
	"node:https":         {},
	"node:net":           {},
	"node:os":            {},
	"node:process":       {},
	"node:child_process": {},
	"node:querystring":   {},
	"node:tls":           {},
	"node:dns":           {},
	"node:dgram":         {},
	// JS / TS scoped packages (kept lowercase per case-folded lookup;
	// only the leading "@scope" segment is matched).
	"@radix-ui":          {},
	"@tanstack":          {},
	"@reduxjs":           {},
	"@testing-library":   {},
	"@types":             {},
	"@nestjs":            {},
	"@prisma":            {},
	"@faker-js":          {},
	"@apollo":            {},
	"@mui":               {},
	"@emotion":           {},
	"@chakra-ui":         {},
	"@headlessui":        {},
	"@hookform":          {},
	"@trpc":              {},
	"@storybook":         {},
	"@vitejs":            {},
	"@babel":             {},
	"@swc":               {},
	"@sentry":            {},
	"@auth0":             {},
	"@aws-sdk":           {},
	"@azure":             {},
	"@google-cloud":      {},
	"@graphql-tools":     {},
	"@graphql-codegen":   {},
	"@jest":              {}, // @jest/globals etc.
	"@rollup":            {},
	"@apollo-server":     {},
	"@apollographql":     {},
	"@typescript-eslint": {},
	"@eslint":            {},
	"@vue":               {},
	"@angular":           {},
	// Wave-4 (TS framework) — React + Next.js + general npm scopes seen
	// across nextjs-commerce / nestjs-starter / express corpora. Every
	// entry is a scope-level allowlist key — the scope prefix branch in
	// isKnownExternalPackage matches "@scope/anything".
	"@heroicons":               {}, // tailwindlabs SVG icon set
	"@vercel":                  {}, // @vercel/analytics, @vercel/og, @vercel/edge
	"@next":                    {}, // next/* shipped under @next scope (e.g. @next/third-parties)
	"@opentelemetry":           {},
	"@nestjs/swagger":          {},
	"@nestjs/passport":         {},
	"@nestjs/jwt":              {},
	"@nestjs/typeorm":          {},
	"@nestjs/config":           {},
	"@nestjs/microservices":    {},
	"@nestjs/platform-express": {},
	"@nestjs/platform-fastify": {},
	"@types/node":              {},
	"@formatjs":                {},
	"@floating-ui":             {},
	"@radix":                   {},
	"@reach":                   {},
	"@stripe":                  {},
	"@shopify":                 {},
	"@tailwindcss":             {}, // @tailwindcss/forms, /typography, /aspect-ratio
	"@react-hook":              {},
	"@react-spring":            {},
	"@react-three":             {},
	"@reactivex":               {},
	"@remix-run":               {},
	"@solidjs":                 {},
	"@sveltejs":                {},
	"@web3-react":              {},
	"@grpc":                    {},
	"@oclif":                   {},
	"@parcel":                  {},
	"@playwright":              {},
	"@types/express":           {},
	"@types/react":             {},
	// Wave-4 — unscoped npm packages frequently imported by Next.js /
	// React / Express stacks. Each is a real, well-known third-party
	// dependency unlikely to collide with a user-defined local module.
	"clsx":                   {},
	"classnames":             {},
	"tailwind-merge":         {},
	"tailwindcss":            {},
	"sonner":                 {},
	"react-dom":              {},
	"react-router":           {},
	"react-router-dom":       {},
	"react-redux":            {},
	"react-query":            {},
	"react-hook-form":        {},
	"react-icons":            {},
	"react-toastify":         {},
	"react-spring":           {},
	"react-transition-group": {},
	"swr":                    {},
	"zustand":                {},
	"jotai":                  {},
	"recoil":                 {},
	"server-only":            {}, // Next.js runtime guard package
	"client-only":            {}, // Next.js runtime guard package
	"sharp":                  {}, // Next.js image optimisation native dep
	"geist":                  {}, // Vercel Geist font package
	"styled-components":      {},
	"styled-jsx":             {},
	"framer-motion":          {},
	"@emotion/react":         {}, // @emotion already scoped, but keeping full key as doc
	"motion":                 {},
	"date-fns-tz":            {},
	"luxon":                  {},
	"moment":                 {},
	"moment-timezone":        {},
	"nanoid":                 {},
	"ulid":                   {},
	"shortid":                {},
	"qrcode":                 {},
	"jose":                   {},
	"argon2":                 {},
	"bcryptjs":               {},
	// Express ecosystem extras
	"finalhandler":      {},
	"router":            {}, // pillarjs router (express dep)
	"path-to-regexp":    {},
	"merge-descriptors": {},
	"safe-buffer":       {},
	"setprototypeof":    {},
	"statuses":          {},
	"depd":              {},
	"after":             {}, // express test helper
	"vary":              {},
	"fresh":             {},
	"etag":              {},
	"escape-html":       {},
	"encodeurl":         {},
	"proxy-addr":        {},
	"forwarded":         {},
	"ipaddr.js":         {},
	"range-parser":      {},
	"methods":           {},
	"utils-merge":       {},
	"array-flatten":     {},
	"safer-buffer":      {},
	"basic-auth":        {},
	"ee-first":          {},
	// Test / view engine deps used in express samples
	"ejs":          {},
	"pug":          {},
	"hbs":          {},
	"handlebars":   {},
	"marked":       {},
	"highlight.js": {},
	// Build tooling commonly imported as named entries
	"typescript-eslint":         {},
	"globals":                   {}, // eslint flat-config "globals" pkg
	"eslint-plugin-prettier":    {},
	"eslint-plugin-react":       {},
	"eslint-plugin-react-hooks": {},
	"eslint-plugin-import":      {},
	"eslint-plugin-jsx-a11y":    {},
	"eslint-config-next":        {},
	"eslint-config-prettier":    {},
	"postcss":                   {},
	"autoprefixer":              {},
	"sass":                      {},
	"less":                      {},
	"stylelint":                 {},
	// Wave-4 (RN/Expo, #508) — React Native + Expo SDK runtime allowlist.
	// The client-fixture-c (RN+Expo) fixture has 538 files and 16.10% bug-rate
	// dominated by IMPORTS to react-native-* / @react-navigation / expo-*
	// packages. Every name is a real npm package shipped by Meta/Expo or a
	// well-known RN community lib; collision risk with hand-rolled local
	// modules is negligible because RN package conventions (`react-native-`
	// prefix, `expo-` prefix, `@react-native(-*)/` scope, `@react-navigation/`
	// scope) are reserved by the ecosystem.
	// React Native core + scopes
	"react-native":                    {},
	"@react-native":                   {}, // @react-native/*, @react-native-community/*-shaped
	"@react-native-community":         {}, // @react-native-community/async-storage, datetimepicker, ...
	"@react-native-async-storage":     {},
	"@react-native-firebase":          {},
	"@react-native-google-signin":     {},
	"@react-native-picker":            {},
	"@react-native-masked-view":       {},
	"@react-native-clipboard":         {},
	"@react-native-segmented-control": {},
	// React Navigation family
	"@react-navigation": {}, // /native, /native-stack, /bottom-tabs, /drawer, /stack, /material-top-tabs, ...
	// Expo SDK family — unscoped npm names (`expo`, `expo-router`, `expo-image`, ...)
	"expo":                       {},
	"expo-router":                {},
	"expo-image":                 {},
	"expo-image-picker":          {},
	"expo-image-manipulator":     {},
	"expo-camera":                {},
	"expo-location":              {},
	"expo-notifications":         {},
	"expo-secure-store":          {},
	"expo-file-system":           {},
	"expo-asset":                 {},
	"expo-font":                  {},
	"expo-constants":             {},
	"expo-status-bar":            {},
	"expo-splash-screen":         {},
	"expo-haptics":               {},
	"expo-blur":                  {},
	"expo-linking":               {},
	"expo-linear-gradient":       {},
	"expo-clipboard":             {},
	"expo-document-picker":       {},
	"expo-print":                 {},
	"expo-sharing":               {},
	"expo-video":                 {},
	"expo-av":                    {},
	"expo-audio":                 {},
	"expo-application":           {},
	"expo-device":                {},
	"expo-localization":          {},
	"expo-network":               {},
	"expo-task-manager":          {},
	"expo-modules-core":          {},
	"expo-modules-autolinking":   {},
	"expo-updates":               {},
	"expo-web-browser":           {},
	"expo-store-review":          {},
	"expo-tracking-transparency": {},
	"expo-build-properties":      {},
	"expo-dev-client":            {},
	"expo-dev-launcher":          {},
	"expo-dev-menu":              {},
	"expo-system-ui":             {},
	"expo-screen-orientation":    {},
	"expo-keep-awake":            {},
	"expo-background-fetch":      {},
	"expo-media-library":         {},
	"expo-mail-composer":         {},
	"expo-sms":                   {},
	"expo-contacts":              {},
	"expo-calendar":              {},
	"expo-battery":               {},
	"expo-brightness":            {},
	"expo-crypto":                {},
	"expo-sqlite":                {},
	"expo-symbols":               {},
	"expo-modules":               {},
	"expo-gl":                    {},
	"expo-three":                 {},
	"@expo":                      {}, // @expo/vector-icons, @expo/config, @expo/cli, @expo/metro-config, ...
	"@expo-google-fonts":         {},
	// React Native community packages (npm-published, no scope)
	"react-native-reanimated":         {},
	"react-native-gesture-handler":    {},
	"react-native-safe-area-context":  {},
	"react-native-screens":            {},
	"react-native-svg":                {},
	"react-native-svg-transformer":    {},
	"react-native-vector-icons":       {},
	"react-native-paper":              {},
	"react-native-elements":           {},
	"react-native-modal":              {},
	"react-native-toast-message":      {},
	"react-native-mmkv":               {},
	"react-native-keychain":           {},
	"react-native-permissions":        {},
	"react-native-device-info":        {},
	"react-native-fast-image":         {},
	"react-native-image-crop-picker":  {},
	"react-native-image-picker":       {},
	"react-native-webview":            {},
	"react-native-maps":               {},
	"react-native-pager-view":         {},
	"react-native-tab-view":           {},
	"react-native-chart-kit":          {},
	"react-native-calendars":          {},
	"react-native-date-picker":        {},
	"react-native-dotenv":             {},
	"react-native-config":             {},
	"react-native-localize":           {},
	"react-native-orientation-locker": {},
	"react-native-share":              {},
	"react-native-splash-screen":      {},
	"react-native-url-polyfill":       {},
	"react-native-uuid":               {},
	"react-native-get-random-values":  {},
	"react-native-bootsplash":         {},
	"react-native-haptic-feedback":    {},
	"react-native-iap":                {},
	"react-native-purchases":          {},
	"react-native-rename":             {},
	"react-native-svg-charts":         {},
	"react-native-track-player":       {},
	"react-native-video":              {},
	"react-native-youtube-iframe":     {},
	// Shopify + Gorhom RN ecosystem (scope-level). `@shopify` already in
	// wave-4 npm-scope block above; not duplicated.
	"@gorhom": {}, // @gorhom/bottom-sheet, @gorhom/portal, ...
	// NativeWind (Tailwind for RN)
	"nativewind":               {},
	"react-native-css-interop": {},
	// Metro bundler (RN-default)
	"metro":                                {},
	"metro-config":                         {},
	"metro-react-native-babel-preset":      {},
	"metro-react-native-babel-transformer": {},
	// Babel presets/plugins used universally by RN/Expo projects
	"babel-preset-expo":                     {},
	"babel-plugin-module-resolver":          {},
	"babel-plugin-transform-remove-console": {},
	// EAS (Expo Application Services)
	"eas-cli": {},
	"@eas":    {},
	// Additional Expo / RN ecosystem packages observed in real Expo+RN apps
	// (client-fixture-c fixture, #508 pass-2). Each is a real npm package; the
	// `lucide-react-native` icon set and `@gluestack-ui` component library
	// are the dominant residuals after pass-1.
	"@gluestack-ui":             {}, // @gluestack-ui/themed, @gluestack-ui/config, ...
	"@gluestack-style":          {},
	"lucide-react-native":       {}, // RN port of lucide icon set
	"lucide-react":              {}, // web sibling (some RN apps import both)
	"@legendapp":                {}, // @legendapp/state, @legendapp/list, ...
	"aws-amplify":               {},
	"@aws-amplify":              {},
	"expo-local-authentication": {},
	"expo-secure-storage":       {},
	"@sentry/react-native":      {}, // @sentry scope already allowlisted; explicit doc-key
	"@formidable-webview":       {},
	"@notifee":                  {}, // @notifee/react-native push notifications
	"@invertase":                {}, // @invertase/react-native-apple-authentication
	"@miblanchard":              {}, // @miblanchard/react-native-slider
	"@callstack":                {}, // @callstack/react-native-paper-dates, /repack
	"@bam.tech":                 {},
	"@viro-community":           {},
	"@unimodules":               {}, // legacy expo unimodules
	"react-native-render-html":  {},
	"react-native-blob-util":    {},
	"react-native-fs":           {},
	"react-native-svg-uri":      {},
	"react-native-flipper":      {},
	"react-native-restart":      {},
	"react-native-quick-crypto": {},
	"react-native-quick-sqlite": {},
	"victory-native":            {},
	"realm":                     {},
	"@realm":                    {},
	"@op-engineering":           {}, // @op-engineering/op-sqlite
	// Wave-7 (TS/JS React frontend, #535) — extended npm scoped families
	// + flat packages observed in client-fixture-b (Vite+React) residuals.
	// Every entry is a real npm package/scope shipped by a known vendor;
	// scope-level keys cover all sub-packages via the scoped-fallback in
	// isKnownExternalPackage. Curated from real bug-resolver disposition
	// samples on the client-fixture-b corpus.
	// Ant Design family
	"@ant-design": {}, // @ant-design/icons, /colors, /charts, /plots, /pro-components, /cssinjs, /x, /web3, /happy-work-theme
	"antd":        {},
	// CKEditor 5 family
	"@ckeditor": {}, // @ckeditor/ckeditor5-react, /ckeditor5-build-classic, /ckeditor5-build-balloon, /ckeditor5-build-inline, /ckeditor5-build-decoupled-document, /ckeditor5-engine, ...
	// dnd-kit family
	"@dnd-kit": {}, // @dnd-kit/core, /sortable, /modifiers, /utilities, /accessibility
	// React Aria / React Stately (Adobe)
	"@react-aria":    {}, // @react-aria/*
	"@react-stately": {}, // @react-stately/*
	"@adobe":         {}, // @adobe/react-spectrum etc.
	// TinyMCE
	"tinymce":  {},
	"@tinymce": {}, // @tinymce/tinymce-react
	// Animation
	"@motionone":   {}, // @motionone/dom, /react, /vue
	"lottie-react": {},
	"lottie-web":   {},
	"@lottiefiles": {},
	// XState
	"@xstate": {}, // @xstate/react, /vue, /svelte
	"xstate":  {},
	// Image / file
	"@cropperjs":       {}, // @cropperjs/react
	"cropperjs":        {},
	"react-image-crop": {},
	"react-dropzone":   {},
	"file-saver":       {},
	"papaparse":        {},
	"xlsx":             {},
	"exceljs":          {},
	// PDF
	"react-pdf":   {},
	"pdf-lib":     {},
	"jspdf":       {},
	"html2canvas": {},
	// Charts (web)
	"recharts":          {},
	"react-chartjs-2":   {},
	"chart.js":          {},
	"victory":           {},
	"@nivo":             {}, // @nivo/core, /line, /bar, ...
	"nivo":              {},
	"d3":                {},
	"@visx":             {}, // @visx/group, /scale, /shape, ...
	"echarts":           {},
	"echarts-for-react": {},
	"@antv":             {}, // @antv/g2, /g6, /l7 (AntV from Ant Design)
	// Tables / virtual lists
	"react-table":       {},
	"material-table":    {},
	"react-virtuoso":    {},
	"react-window":      {},
	"react-virtualized": {},
	// Forms
	"formik": {},
	"yup":    {},
	"joi":    {},
	// Date / time extras
	"react-datepicker": {},
	// Toasts
	"react-hot-toast": {},
	"notistack":       {},
	// Markdown / content
	"react-markdown":           {},
	"remark":                   {},
	"rehype":                   {},
	"unified":                  {},
	"react-syntax-highlighter": {},
	// i18n
	"react-i18next":  {},
	"i18next":        {},
	"react-intl":     {},
	"@formatjs/intl": {},
	// DnD (legacy)
	"react-dnd":               {},
	"react-dnd-html5-backend": {},
	"react-dnd-touch-backend": {},
	// React Hook Form add-ons (scope already covered by @hookform)
	// State
	"valtio":        {},
	"@xstate/react": {}, // explicit doc-key
	// Routing
	"@tanstack/react-router": {}, // covered by @tanstack scope but explicit doc
	// Misc / utility
	"lodash-es":               {},
	"lodash.debounce":         {},
	"lodash.throttle":         {},
	"lodash.merge":            {},
	"lodash.clonedeep":        {},
	"lodash.get":              {},
	"lodash.set":              {},
	"lodash.isequal":          {},
	"query-string":            {},
	"history":                 {}, // react-router history pkg
	"use-immer":               {},
	"reselect":                {},
	"redux-thunk":             {},
	"redux-saga":              {},
	"redux-persist":           {},
	"redux-logger":            {},
	"connected-react-router":  {},
	"react-helmet":            {},
	"react-helmet-async":      {},
	"react-error-boundary":    {},
	"react-use":               {},
	"usehooks-ts":             {},
	"react-resizable":         {},
	"react-grid-layout":       {},
	"react-beautiful-dnd":     {}, // deprecated but still widely used
	"copy-to-clipboard":       {},
	"react-copy-to-clipboard": {},
	"react-color":             {},
	"color":                   {},
	"polished":                {},
	"@iconify":                {}, // @iconify/react, /icons-*, /tools
	"@fontsource":             {}, // @fontsource/inter, /roboto, ...
	"@fontsource-variable":    {},
	"@phosphor-icons":         {}, // @phosphor-icons/react
	"@tabler":                 {}, // @tabler/icons-react
	// Wave-7 pass-3 — additional flat npm packages seen in
	// client-fixture-b post-Track-1 bug-resolver residuals.
	"antd-style":                      {}, // Ant Design CSS-in-JS
	"ckeditor5":                       {}, // ckeditor5 root pkg (also @ckeditor/ scope)
	"dompurify":                       {}, // HTML sanitizer
	"react-infinite-scroll-component": {},
	"react-infinite-scroller":         {},
	"react-window-infinite-loader":    {},
	"react-spinners":                  {},
	"react-loading-skeleton":          {},
	"react-content-loader":            {},
	"react-select":                    {},
	"react-select-async-paginate":     {},
	// `async` (npm utility lib) omitted — cross-lang invariant: collides with kotlin coroutine `async`.
	"async-mutex":       {},
	"p-limit":           {},
	"p-queue":           {},
	"p-retry":           {},
	"p-map":             {},
	"p-debounce":        {},
	"p-throttle":        {},
	"debounce":          {},
	"throttle-debounce": {},
	"fast-equals":       {},
	"shallowequal":      {},
	// Next.js sub-paths — register as bare-keys too, so the leading-`/`
	// strip via slashCanonical above (in the import:external branch)
	// folds them all to "next" when scopedNpmRoot doesn't apply.
	// (No-op when already present via the top-level "next" entry.)
	// Go stdlib top-level
	"fmt":           {},
	"strings":       {},
	"strconv":       {},
	"errors":        {},
	"context":       {},
	"net":           {},
	"http":          {},
	"io":            {},
	"bytes":         {},
	"sort":          {},
	"sync":          {},
	"time":          {},
	"path":          {},
	"regexp":        {},
	"testing":       {},
	"encoding/json": {},
	// Issue #116: Go stdlib root segments — populated so full-import-
	// path stubs (`net/http`, `encoding/json`, `crypto/tls`) can be
	// classified by their root segment after the `/`-split branch in
	// classifyExternal. Each root is the top-level directory in the
	// Go stdlib tree.
	"encoding":  {},
	"crypto":    {},
	"bufio":     {},
	"database":  {},
	"compress":  {},
	"archive":   {},
	"image":     {},
	"text":      {},
	"html":      {},
	"mime":      {},
	"hash":      {},
	"math":      {},
	"runtime":   {},
	"reflect":   {},
	"unicode":   {},
	"flag":      {},
	"container": {},
	"plugin":    {},
	"embed":     {},
	"expvar":    {},
	"syscall":   {},
	"unsafe":    {},
	// "log" / "hash" already present (Rust crates / Python builtins
	// blocks) and serve double-duty for Go stdlib roots via case-
	// folded lookup.
	// "os" / "sys" / "json" / "queue" / "abc" / "enum" are already in
	// the Python stdlib block above — case-folded lookup makes them
	// accept Go's `os`/`sort`/etc. as well. "io"/"net"/"sort"/"sync"/
	// "time"/"path"/"errors"/"strings"/"strconv"/"context"/"bytes"/
	// "regexp"/"testing"/"hash" likewise serve both ecosystems.

	// Issue #364 — Go stdlib multi-segment paths used as canonical names
	// for ext:<path> placeholders synthesised from receiver_type stdlib
	// interface dispatch. Single-segment stdlib roots (`io`, `os`, `fmt`,
	// `bufio`, `bytes`, `strings`, `sync`, `context`, `testing`, `http`,
	// `net`, `sql`) already exist above, but the goStdlibInterfaceMethods
	// catalogue uses the canonical import path (`net/http`,
	// `database/sql`) so the disposition allowlist must match the slash
	// form too.
	"net/http":     {},
	"database/sql": {},

	// Issue #116: Go third-party host-prefixed roots (3-segment
	// "<host>/<owner>/<repo>" canonical form). These are matched by
	// goHostCanonical after the slash-split branch in classifyExternal.
	"github.com/stretchr/testify":         {},
	"github.com/gin-gonic/gin":            {},
	"github.com/go-chi/chi":               {},
	"github.com/labstack/echo":            {},
	"github.com/sirupsen/logrus":          {},
	"github.com/spf13/cobra":              {},
	"github.com/spf13/viper":              {},
	"github.com/spf13/pflag":              {},
	"github.com/pkg/errors":               {},
	"github.com/google/uuid":              {},
	"github.com/golang/protobuf":          {},
	"github.com/golang/mock":              {},
	"github.com/jmoiron/sqlx":             {},
	"github.com/jinzhu/gorm":              {},
	"github.com/gorilla/mux":              {},
	"github.com/gorilla/websocket":        {},
	"github.com/prometheus/client_golang": {},
	"github.com/uber-go/zap":              {},
	"github.com/davecgh/go-spew":          {},
	"github.com/stretchr/objx":            {},
	"golang.org/x/sync":                   {},
	"golang.org/x/crypto":                 {},
	"golang.org/x/net":                    {},
	"golang.org/x/text":                   {},
	"golang.org/x/sys":                    {},
	"golang.org/x/oauth2":                 {},
	"golang.org/x/exp":                    {},
	"golang.org/x/tools":                  {},
	"google.golang.org/grpc":              {},
	"google.golang.org/protobuf":          {},
	"gopkg.in/yaml.v3":                    {},
	"gopkg.in/yaml.v2":                    {},
	// Go third-party (legacy single-segment keys; left in place so any
	// pre-#116 caller hitting the Pascal/dotted branch still resolves).
	"testify": {},
	"viper":   {},
	"cobra":   {},
	"zap":     {},
	"logrus":  {},
	"sqlx":    {},
	"gorm":    {},
	"gorilla": {},
	// Java / Kotlin
	"java":    {},
	"javax":   {},
	"kotlin":  {},
	"kotlinx": {},
	"io.ktor": {}, // io.ktor.* server / client / websockets (Issue #106)
	// Kotlin Exposed SQL DSL/ORM (JetBrains). Both v0 (legacy) and v1
	// import roots are present in the wild: legacy `org.jetbrains.exposed.sql.*`,
	// `org.jetbrains.exposed.dao.*`, and the v1 layout
	// `org.jetbrains.exposed.v1.core.*`, `.v1.jdbc.*`, `.v1.json.*`,
	// `.v1.datetime.*`, etc. A single `org.jetbrains.exposed` prefix
	// covers every subpackage via longestKnownDottedPrefix.
	"org.jetbrains.exposed": {},
	"org.jetbrains.kotlinx": {}, // org.jetbrains.kotlinx.* (kotlinx coroutines/serialization JB-published artifacts)
	"org.jetbrains.kotlin":  {}, // org.jetbrains.kotlin.* (kotlin compiler/std)
	"org.springframework":   {},
	"com.fasterxml.jackson": {},
	"com.google.guava":      {},
	"org.apache.commons":    {},
	"junit":                 {},
	"mockito":               {},
	"slf4j":                 {},
	"log4j":                 {},
	"lombok":                {},
	// Java test stack (Issue #120 — spring-petclinic test imports).
	// Multi-segment keys keep the longest-prefix matcher precise so an
	// unrelated `org.junit` user-namespace would not collide.
	"org.junit":          {}, // covers org.junit / org.junit.jupiter.* / org.junit.platform.*
	"org.mockito":        {},
	"org.assertj":        {},
	"org.hamcrest":       {},
	"org.testcontainers": {},
	"io.micrometer":      {}, // metrics/observability used by Spring Boot
	"ch.qos.logback":     {}, // default Spring Boot logger
	// Quarkus + ecosystem (Issue #PLT-316 — fixture-d baseline bug-rate reduction)
	// Quarkus is a cloud-native Java framework with CDI-driven DI and modern JVM
	// runtime tuning. io.quarkus.* covers Quarkus core and extensions.
	// io.smallrye.* is the MicroProfile impl used by Quarkus (Config, Reactive,
	// JWT, OpenAPI, etc.). io.vertx.* is the Vert.x reactive transport layer.
	// jakarta.* are the modern Java EE APIs (EE 9+ namespacing); jakarta.inject,
	// jakarta.enterprise, jakarta.ws.rs, jakarta.persistence, jakarta.validation,
	// jakarta.transaction, jakarta.annotation, jakarta.servlet appear in Quarkus
	// and modern Spring Boot imports. at.favre.lib.crypto and other 3rd-party
	// roots are pulled in by Quarkus extensions. org.eclipse.microprofile.* is
	// the MicroProfile spec implementation (Config, JWT, OpenAPI, REST Client, etc.).
	// io.hypersistence.* is Hibernate utilities for JSON/JSONB type mapping.
	// Additional JVM ecosystem roots from java-orphan-recovery: Swagger, Hibernate,
	// Eclipse umbrella, Gradle, bare org.apache, com.google, com.fasterxml, cloud SDKs,
	// HikariCP, Jedis, BCrypt, Groovy.
	"io.quarkus":               {},
	"io.smallrye":              {},
	"io.vertx":                 {},
	"jakarta.inject":           {},
	"jakarta.enterprise":       {},
	"jakarta.ws.rs":            {},
	"jakarta.persistence":      {},
	"jakarta.validation":       {},
	"jakarta.transaction":      {},
	"jakarta.annotation":       {},
	"jakarta.servlet":          {},
	"jakarta.json":             {}, // jakarta.json.bind for JSON-B serialization
	"at.favre.lib.crypto":      {},
	"org.eclipse.microprofile": {}, // MicroProfile spec (Config, JWT, OpenAPI, REST Client, etc.)
	"io.hypersistence":         {}, // Hibernate utilities for JSON/JSONB type mapping
	"io.swagger":               {}, // Swagger / OpenAPI annotations
	"org.hibernate":            {}, // Hibernate ORM / Validator / Reactive
	"org.eclipse":              {}, // Eclipse foundation umbrella (Jetty, JKube, etc.)
	"org.gradle":               {}, // Gradle build / plugin APIs
	"org.apache":               {}, // bare org.apache umbrella for unlisted subpackages
	"com.google":               {}, // broader com.google.* (Guice, Closure, etc.)
	"com.fasterxml":            {}, // broader com.fasterxml.* (woodstox, etc.)
	"com.amazonaws":            {}, // AWS SDK for Java v1
	"com.azure":                {}, // Azure SDK for Java
	"com.microsoft":            {}, // MSAL / SQL Server JDBC / etc.
	"com.oracle":               {}, // Oracle JDBC / GraalVM SDK
	"com.sun":                  {}, // legacy com.sun.* (Xerces, JAX-WS RI, etc.)
	"com.zaxxer":               {}, // HikariCP connection pool
	"redis.clients":            {}, // Jedis Redis client (`redis.clients.jedis.*`)
	"at.favre.lib":             {}, // BCrypt password hashing
	"groovy":                   {}, // Groovy language / Gradle build DSL
	// Issue kafka-fix-w3 — Apache Kafka / Confluent / Avro / Jetty / Jersey
	// ecosystem roots. Multi-segment keys keep longestKnownDottedPrefix
	// precise so an unrelated `org.apache` user-namespace cannot collide.
	// All of these appear as `import org.apache.kafka.streams.*` /
	// `import io.confluent.examples.streams.*` / `import org.apache.avro.*`
	// in the kafka-streams-examples corpus and across every JVM
	// streaming/integration repo.
	"org.apache.kafka":       {}, // Kafka clients, streams, connect, common
	"org.apache.avro":        {}, // Apache Avro serdes
	"org.apache.curator":     {}, // ZooKeeper client framework
	"org.apache.zookeeper":   {}, // ZooKeeper
	"org.apache.log4j":       {}, // log4j 1.x dotted form (in addition to bare `log4j`)
	"org.apache.logging":     {}, // log4j 2.x (org.apache.logging.log4j.*)
	"org.apache.hadoop":      {}, // commonly imported alongside Kafka in data pipelines
	"org.apache.commons.cli": {}, // commons-cli (explicit; org.apache.commons already covers it)
	// Issue #787c — Apache POI (Excel/Word/PowerPoint/OOXML) and Apache PDFBox.
	// Both are imported in real enterprise Java / Quarkus services alongside
	// Quarkus, Spring, and Jakarta EE.  Multi-segment keys keep
	// longestKnownDottedPrefix precise so an unrelated `org.apache.pooling`
	// namespace cannot collide.
	//
	// POI sub-packages (ss=spreadsheet, xssf=OOXML, hssf=legacy, sxssf=streaming,
	// xwpf=Word, xslf=PowerPoint, ooxml=generic OOXML schemas, extractor=text
	// extraction).  org.apache.poi covers them all as an umbrella.
	"org.apache.poi":       {}, // Apache POI: XSSF/HSSF/SXSSF/XWPF/XSLF spreadsheet+doc APIs
	"org.apache.poi.ss":    {}, // POI Spreadsheet (ss) common API — Cell, Row, Sheet, Workbook interfaces
	"org.apache.poi.xssf":  {}, // POI XSSF — OOXML-format (xlsx/xlsm) classes
	"org.apache.poi.hssf":  {}, // POI HSSF — legacy BIFF8 format (xls) classes
	"org.apache.poi.xwpf":  {}, // POI XWPF — Word 2007+ (docx) classes
	"org.apache.poi.xslf":  {}, // POI XSLF — PowerPoint 2007+ (pptx) classes
	"org.apache.poi.ooxml": {}, // POI generic OOXML schemas
	// PDFBox — Apache's pure-Java PDF library (org.apache.pdfbox.pdmodel.*,
	// org.apache.pdfbox.rendering.*, org.apache.pdfbox.text.*, etc.).
	// Covered by the bare org.apache umbrella, but the explicit entry keeps
	// longestKnownDottedPrefix from unnecessarily walking to `org.apache` when
	// org.apache.pdfbox is the real library.
	"org.apache.pdfbox": {}, // Apache PDFBox: PDDocument, PDPage, PDPageContentStream, PDFont, …
	// Apache Commons — commons-io, commons-lang3, commons-collections, and
	// commons-compress are commonly co-imported with POI in enterprise Java.
	// org.apache.commons is already on the allowlist but listing the
	// high-traffic sub-families here gives longestKnownDottedPrefix a
	// more precise canonical name for each.
	"org.apache.commons.io":           {}, // Apache Commons IO — FileUtils, IOUtils, FilenameUtils, …
	"org.apache.commons.lang3":        {}, // Apache Commons Lang3 — StringUtils, ArrayUtils, ObjectUtils, …
	"org.apache.commons.collections4": {}, // Apache Commons Collections4 — Bag, MultiMap, …
	"org.apache.commons.compress":     {}, // Apache Commons Compress — zip/tar/7z helpers often used with POI
	"org.apache.commons.text":         {}, // Apache Commons Text — StringSubstitutor, WordUtils, …
	"io.confluent":                    {}, // Confluent schema-registry / kafka-streams examples / KSQL clients
	"kafka":                           {}, // Scala Kafka classes (`import kafka.server.KafkaConfig`)
	"com.google.common":               {}, // Guava (`com.google.common.collect.*`) — supersedes legacy `com.google.guava`
	"com.google.protobuf":             {}, // Protobuf Java runtime
	"com.google.gson":                 {}, // Gson JSON
	"org.eclipse.jetty":               {}, // Embedded Jetty server (REST interactive-queries demos)
	"org.glassfish.jersey":            {}, // JAX-RS Jersey impl (REST clients in Kafka examples)
	"org.glassfish":                   {}, // broader org.glassfish.* (HK2, etc.)
	"jakarta.ws":                      {}, // jakarta.ws.rs.* (JAX-RS)
	"javax.ws":                        {}, // javax.ws.rs.* (legacy JAX-RS)
	"org.rocksdb":                     {}, // Kafka Streams default state-store backend
	"org.codehaus":                    {}, // org.codehaus.jackson.* legacy / org.codehaus.plexus.*
	"com.typesafe":                    {}, // com.typesafe.config (Akka/Kafka config)
	"reactor":                         {}, // Project Reactor (`reactor.core.*`)
	"io.netty":                        {}, // Netty (transport for many JVM brokers/clients)
	"io.reactivex":                    {}, // RxJava
	"io.grpc":                         {}, // gRPC Java
	"io.opentelemetry":                {}, // OpenTelemetry Java SDK
	"org.yaml":                        {}, // snakeyaml
	"org.json":                        {}, // org.json reference library
	"org.slf4j":                       {}, // SLF4J Logger/LoggerFactory dotted form
	"javax.servlet":                   {}, // jakarta predecessor (Jetty servlet API)
	// Scala ecosystem (play-scala-starter, Akka, scalatest, sbt, etc.).
	// Both the language-namespace `scala` root and JVM-style dotted
	// `org.*` / `com.*` roots are present so every `import` shape in a
	// real Scala project routes to ExternalKnown via the dotted-path
	// branch in classifyExternal. Multi-segment keys are preferred for
	// `org.*` roots so they match the longest-prefix walk precisely.
	"scala":             {}, // scala.concurrent.*, scala.util.*, scala.collection.*
	"akka":              {}, // akka.actor.*, akka.http.*, akka.stream.*
	"play":              {}, // play.api.* (Play Framework)
	"sbt":               {},
	"cats":              {}, // cats / cats-effect
	"monix":             {},
	"zio":               {},
	"shapeless":         {},
	"slick":             {},
	"doobie":            {},
	"http4s":            {},
	"finagle":           {},
	"spray":             {},
	"org.scalatest":     {},
	"org.scalatestplus": {},
	"org.scalacheck":    {},
	"org.scalamock":     {},
	"org.specs2":        {},
	"com.google.inject": {}, // Guice — Play uses it for DI
	// Ruby — Rails framework, ActiveSupport, ActionPack, ActiveRecord, ActionCable.
	"rails":             {},
	"activerecord":      {},
	"activesupport":     {},
	"actionpack":        {},
	"actionview":        {},
	"actionmailer":      {},
	"actionmailbox":     {},
	"actiontext":        {},
	"actioncable":       {},
	"activemodel":       {},
	"activestorage":     {},
	"activejob":         {},
	"active_support":    {},
	"active_record":     {},
	"active_model":      {},
	"active_job":        {},
	"action_pack":       {},
	"action_view":       {},
	"action_mailer":     {},
	"action_cable":      {},
	"action_dispatch":   {},
	"action_controller": {},
	"railties":          {},
	"sprockets":         {},
	// Ruby stdlib (require 'foo' targets — sidekiq + rails leak these as
	// bug-resolver because the require name is a stdlib gem the resolver
	// can't bind to a local file). Issue #449 — Sidekiq + Rails wave.
	"forwardable":  {},
	"securerandom": {},
	"fileutils":    {},
	"erb":          {},
	// "yaml" / "json" — already in Python ecosystem block above.
	"set":  {},
	"date": {},
	// "time" — already covered above.
	"timeout": {},
	// "tempfile" — already covered.
	"tmpdir":   {},
	"stringio": {},
	"strscan":  {},
	// "zlib" / "openssl" — already covered (Python ecosystem).
	"digest": {},
	"base64": {},
	"uri":    {},
	"cgi":    {},
	// "net/http" — already covered.
	"net/https": {},
	"net/smtp":  {},
	"net/pop":   {},
	"net/imap":  {},
	"net/ftp":   {},
	"optparse":  {},
	"logger":    {},
	"monitor":   {},
	"fiber":     {},
	"thread":    {},
	"mutex_m":   {},
	"pathname":  {},
	"pp":        {},
	"singleton": {},
	"observer":  {},
	"delegate":  {},
	"open3":     {},
	// "socket" — already covered.
	"ostruct": {},
	// "benchmark" — already covered.
	"etc":        {},
	"fcntl":      {},
	"shellwords": {},
	"resolv":     {},
	"ipaddr":     {},
	// "weakref" — already covered.
	"objspace":    {},
	"coverage":    {},
	"prettyprint": {},
	"abbrev":      {},
	"english":     {},
	"erb/util":    {},
	// Common Ruby gems used by Rails / Sidekiq apps. Bias toward names
	// that are unambiguous package roots (won't shadow user identifiers).
	"sidekiq":         {},
	"sidekiq-pro":     {},
	"sidekiq-ent":     {},
	"connection_pool": {},
	"concurrent-ruby": {},
	// "concurrent" — already covered above.
	"redis-client":   {},
	"redis_client":   {},
	"hiredis":        {},
	"hiredis-client": {},
	"dalli":          {},
	"mysql2":         {},
	"pg":             {},
	"sqlite3":        {},
	// "bcrypt" — already covered above.
	"devise":           {},
	"cancancan":        {},
	"pundit":           {},
	"rolify":           {},
	"oj":               {},
	"multi_json":       {},
	"httparty":         {},
	"faraday":          {},
	"excon":            {},
	"typhoeus":         {},
	"kaminari":         {},
	"pagy":             {},
	"will_paginate":    {},
	"pry":              {},
	"byebug":           {},
	"rspec":            {},
	"factory_bot":      {},
	"factory_girl":     {},
	"database_cleaner": {},
	"webmock":          {},
	// "vcr" — already covered above.
	"capybara":           {},
	"selenium-webdriver": {},
	"simplecov":          {},
	"rubocop":            {},
	"bundler":            {},
	"rake":               {},
	"minitest":           {},
	// "mocha" — already covered above (frontend ecosystem).
	"timecop":                 {},
	"shoulda":                 {},
	"shoulda-matchers":        {},
	"after_commit_everywhere": {},
	"acts_as_taggable_on":     {},
	"acts-as-taggable-on":     {},
	"jbuilder":                {},
	"turbolinks":              {},
	"turbo-rails":             {},
	"stimulus-rails":          {},
	"importmap-rails":         {},
	"sprockets-rails":         {},
	"sass-rails":              {},
	"webpacker":               {},
	"puma":                    {},
	"unicorn":                 {},
	"thin":                    {},
	"sinatra":                 {},
	"grape":                   {},
	"rack":                    {},
	"rack-test":               {},
	"rack-cors":               {},
	"rack-attack":             {},
	// C# / .NET (Issue #91 — top non-Python language by import-bug)
	"system":    {}, // System.*, System.Text.*, System.Collections.*
	"microsoft": {}, // Microsoft.EntityFrameworkCore, Microsoft.AspNetCore.*
	// Java EE / Jakarta (Issue #91 — Spring/JPA imports)
	"jakarta": {}, // jakarta.persistence, jakarta.validation
	// Rust crates (Issue #91 — top Rust import-bug roots)
	"tokio":              {},
	"actix_web":          {},
	"actix":              {},
	"serde":              {},
	"serde_json":         {},
	"anyhow":             {},
	"thiserror":          {},
	"tracing":            {},
	"tracing_subscriber": {},
	"clap":               {},
	"reqwest":            {},
	"futures":            {},
	"async_trait":        {},
	"opentelemetry":      {},
	// Rust stdlib + extended ecosystem (Issue #101 — bug-rate
	// reduction targets for mini-redis / actix-examples). `std` is
	// the Rust standard library; every `use std::...` path leaks
	// without it. The actix_* / async_* ecosystem and Diesel ORM
	// are the highest-volume third-party leak roots in the
	// actix-examples corpus.
	"std":                  {},
	"core":                 {}, // libcore (rare in user code but legit)
	"alloc":                {}, // liballoc
	"actix_files":          {},
	"actix_identity":       {},
	"actix_session":        {},
	"actix_cors":           {},
	"actix_multipart":      {},
	"actix_rt":             {},
	"actix_service":        {},
	"actix_codec":          {},
	"actix_http":           {},
	"actix_router":         {},
	"actix_test":           {},
	"actix_web_actors":     {},
	"actix_protobuf":       {},
	"awc":                  {}, // actix web client
	"diesel":               {},
	"diesel_migrations":    {},
	"sea_orm":              {},
	"casbin":               {},
	"chrono":               {},
	"regex":                {},
	"rand":                 {},
	"hyper":                {},
	"tower":                {},
	"tower_http":           {},
	"axum":                 {},
	"rocket":               {},
	"warp":                 {},
	"once_cell":            {},
	"lazy_static":          {},
	"parking_lot":          {},
	"crossbeam":            {},
	"rayon":                {},
	"log":                  {},
	"env_logger":           {},
	"opentelemetry_aws":    {},
	"opentelemetry_otlp":   {},
	"opentelemetry_jaeger": {},
	"async_stream":         {},
	"tokio_stream":         {},
	"tokio_util":           {},
	"derive_more":          {},
	// Rust wave (S19+) — tokio + actix-examples + mini-redis residual
	// reduction. Top external crate roots that previously leaked into
	// bug-extractor / external-unknown because the v1.1 allowlist
	// stopped at the most-popular fifteen. Each entry corresponds to
	// a real crates.io publication. Hyphenated variants are listed
	// alongside underscore forms because the resolver emits the
	// Cargo.toml dep name (with hyphens) for the ext:* placeholder
	// while Rust source uses underscores; the lowercase isKnown lookup
	// doesn't normalise the two.
	//
	// Tokio runtime ecosystem.
	"tokio_test":             {},
	"tokio-test":             {},
	"tokio_macros":           {},
	"tokio-macros":           {},
	"tokio-stream":           {},
	"tokio-util":             {},
	"tokio_uring":            {},
	"tokio-uring":            {},
	"tokio_tungstenite":      {},
	"tokio-tungstenite":      {},
	"tokio_native_tls":       {},
	"tokio-native-tls":       {},
	"tokio_rustls":           {},
	"tokio-rustls":           {},
	"tokio_postgres":         {},
	"tokio-postgres":         {},
	"tokio_pg_mapper":        {},
	"tokio-pg-mapper":        {},
	"tokio_pg_mapper_derive": {},
	"tokio-pg-mapper-derive": {},
	// Async ecosystem.
	"futures_util":     {},
	"futures-util":     {},
	"futures_core":     {},
	"futures-core":     {},
	"futures_io":       {},
	"futures-io":       {},
	"futures_channel":  {},
	"futures-channel":  {},
	"futures_task":     {},
	"futures-task":     {},
	"futures_executor": {},
	"futures-executor": {},
	"async-stream":     {},
	"async_channel":    {},
	"async-channel":    {},
	"async-trait":      {},
	"pin_project":      {},
	"pin-project":      {},
	"pin_project_lite": {},
	"pin-project-lite": {},
	// Tokio system primitives (low-level deps Tokio itself uses).
	"mio":       {},
	"libc":      {},
	"loom":      {},
	"io_uring":  {},
	"io-uring":  {},
	"socket2":   {},
	"slab":      {},
	"nix":       {},
	"mockall":   {},
	"backtrace": {},
	// Windows / WASM platform crates seen in tokio.
	"windows_sys":       {},
	"windows-sys":       {},
	"windows":           {},
	"windows-targets":   {},
	"wasm_bindgen":      {},
	"wasm-bindgen":      {},
	"wasm_bindgen_test": {},
	"wasm-bindgen-test": {},
	"js_sys":            {},
	"js-sys":            {},
	// Actix extended ecosystem.
	"actix_broker":    {},
	"actix-broker":    {},
	"actix_web_lab":   {},
	"actix-web-lab":   {},
	"actix_ws":        {},
	"actix-ws":        {},
	"actix_redis":     {},
	"actix-redis":     {},
	"actix_form_data": {},
	"actix-form-data": {},
	"actix_settings":  {},
	"actix-settings":  {},
	"actix_tls":       {},
	"actix-tls":       {},
	"actix_macros":    {},
	"actix-macros":    {},
	"actix_derive":    {},
	"actix-derive":    {},
	"ractor":          {},
	// HTTP / TLS / serialization crates.
	"rustls":              {},
	"rustls_pemfile":      {},
	"rustls-pemfile":      {},
	"rustls_native_certs": {},
	"rustls-native-certs": {},
	"webpki":              {},
	"webpki_roots":        {},
	"webpki-roots":        {},
	"openssl_sys":         {},
	"openssl-sys":         {},
	"native_tls":          {},
	"native-tls":          {},
	"http_body":           {},
	"http-body":           {},
	"http_body_util":      {},
	"http-body-util":      {},
	"hyper_util":          {},
	"hyper-util":          {},
	"hyper_rustls":        {},
	"hyper-rustls":        {},
	"hyper_tls":           {},
	"hyper-tls":           {},
	"h2":                  {},
	"h3":                  {},
	"reqwest_middleware":  {},
	"reqwest-middleware":  {},
	"mime_guess":          {},
	"mime-guess":          {},
	"percent_encoding":    {},
	"percent-encoding":    {},
	"form_urlencoded":     {},
	"form-urlencoded":     {},
	// Serialization beyond serde core.
	"serde_yaml":       {},
	"serde-yaml":       {},
	"serde_derive":     {},
	"serde-derive":     {},
	"serde_urlencoded": {},
	"serde-urlencoded": {},
	"serde_with":       {},
	"serde-with":       {},
	"serde_repr":       {},
	"serde-repr":       {},
	"toml_edit":        {},
	"toml-edit":        {},
	"serde_qs":         {},
	"serde-qs":         {},
	"bincode":          {},
	"rmp":              {},
	"rmp_serde":        {},
	"rmp-serde":        {},
	"prost":            {},
	"prost_types":      {},
	"prost-types":      {},
	"tonic":            {},
	"tonic_build":      {},
	"tonic-build":      {},
	// Error handling + utilities.
	"eyre":           {},
	"color_eyre":     {},
	"color-eyre":     {},
	"miette":         {},
	"snafu":          {},
	"derive_builder": {},
	"derive-builder": {},
	"strum":          {},
	"strum_macros":   {},
	"strum-macros":   {},
	"num":            {},
	"num_traits":     {},
	"num-traits":     {},
	"num_cpus":       {},
	"num-cpus":       {},
	"num_derive":     {},
	"num-derive":     {},
	"either":         {},
	"smallvec":       {},
	"arrayvec":       {},
	"indexmap":       {},
	"dashmap":        {},
	"ahash":          {},
	"fxhash":         {},
	"rustc_hash":     {},
	"rustc-hash":     {},
	"hashbrown":      {},
	"ordered_float":  {},
	"ordered-float":  {},
	// IDs / time / UUID.
	"chrono_tz": {},
	"chrono-tz": {},
	// Templating + GraphQL + REST clients seen in actix-examples.
	"juniper":                 {},
	"async_graphql":           {},
	"async-graphql":           {},
	"async_graphql_actix_web": {},
	"async-graphql-actix-web": {},
	"tera":                    {},
	"askama":                  {},
	"askama_actix":            {},
	"askama-actix":            {},
	"sailfish":                {},
	"minijinja":               {},
	"minijinja_autoreload":    {},
	"minijinja-autoreload":    {},
	"yarte":                   {},
	"yarte_helpers":           {},
	"yarte-helpers":           {},
	"tinytemplate":            {},
	"fluent_templates":        {},
	"fluent-templates":        {},
	"validator_derive":        {},
	"validator-derive":        {},
	// DB / pooling.
	"deadpool":          {},
	"deadpool_postgres": {},
	"deadpool-postgres": {},
	"deadpool_redis":    {},
	"deadpool-redis":    {},
	"deadpool_diesel":   {},
	"deadpool-diesel":   {},
	"diesel_async":      {},
	"diesel-async":      {},
	"sqlx_core":         {},
	"sqlx-core":         {},
	"sqlx_macros":       {},
	"sqlx-macros":       {},
	"sea_query":         {},
	"sea-query":         {},
	"sea_schema":        {},
	"sea-schema":        {},
	"rusqlite":          {},
	"r2d2":              {},
	"refinery":          {},
	"mongodb":           {},
	// Observability.
	"tracing_actix_web":                  {},
	"tracing-actix-web":                  {},
	"tracing-subscriber":                 {},
	"tracing_bunyan_formatter":           {},
	"tracing-bunyan-formatter":           {},
	"tracing_opentelemetry":              {},
	"tracing-opentelemetry":              {},
	"tracing_log":                        {},
	"tracing-log":                        {},
	"tracing_appender":                   {},
	"tracing-appender":                   {},
	"slog":                               {},
	"slog_async":                         {},
	"slog-async":                         {},
	"opentelemetry_sdk":                  {},
	"opentelemetry-sdk":                  {},
	"opentelemetry-aws":                  {},
	"opentelemetry-otlp":                 {},
	"opentelemetry-jaeger":               {},
	"opentelemetry_zipkin":               {},
	"opentelemetry-zipkin":               {},
	"opentelemetry_prometheus":           {},
	"opentelemetry-prometheus":           {},
	"opentelemetry_semantic_conventions": {},
	"opentelemetry-semantic-conventions": {},
	"metrics_exporter_prometheus":        {},
	"metrics-exporter-prometheus":        {},
	"prometheus":                         {},
	"sentry":                             {},
	"sentry_actix":                       {},
	"sentry-actix":                       {},
	// Cloud SDKs (AWS).
	"aws_config":           {},
	"aws-config":           {},
	"aws_sdk_s3":           {},
	"aws-sdk-s3":           {},
	"aws_sdk_dynamodb":     {},
	"aws-sdk-dynamodb":     {},
	"aws_smithy_types":     {},
	"aws-smithy-types":     {},
	"aws_smithy_http":      {},
	"aws-smithy-http":      {},
	"aws_credential_types": {},
	"aws-credential-types": {},
	"sparkpost":            {},
	"sparklepost":          {},
	"lettre":               {},
	// Misc commonly-seen.
	"notify":            {},
	"confik":            {},
	"figment":           {},
	"config":            {},
	"dotenvy":           {},
	"clap_derive":       {},
	"clap-derive":       {},
	"structopt":         {},
	"apalis":            {},
	"apalis_core":       {},
	"apalis-core":       {},
	"apalis_redis":      {},
	"apalis-redis":      {},
	"bb8":               {},
	"deadqueue":         {},
	"crossbeam_utils":   {},
	"crossbeam-utils":   {},
	"crossbeam_channel": {},
	"crossbeam-channel": {},
	"crossbeam_epoch":   {},
	"crossbeam-epoch":   {},
	"flume":             {},
	"sha2":              {},
	"sha1":              {},
	"md5":               {},
	"hex":               {},
	"hmac":              {},
	"acme":              {},
	"acme_lib":          {},
	"acme-lib":          {},
	// Tutorial / demo crates that are published on crates.io and
	// also appear as the host repo in their own corpus.
	"mini_redis": {},
	"mini-redis": {},
	// Rust wave (S19+) — synthetic placeholder for bare-sibling-module
	// use-paths emitted by the rust extractor (`use worker::Context`,
	// `use entry::*`). Single ext:rust_sibling_module node per repo;
	// folded in by classifyExternal's `::` branch.
	"rust_sibling_module": {},
	// Misc widely-used rust crates from actix-examples + tokio.
	"byteorder":      {},
	"aes_gcm_siv":    {},
	"aes-gcm-siv":    {},
	"actix_utils":    {},
	"actix-utils":    {},
	"actix_governor": {},
	"actix-governor": {},
	// Tokio internals that surface in tokio source as use-paths but
	// are tokio's own sub-crates resolved at build time — listed as
	// known so that `use loom::...` / `use mio::...` / `use libc::...`
	// in tokio test/loom modules land in external-known.
	// PHP ecosystem (Issue #102 — symfony-demo bug-rate reduction).
	// PHP namespace roots reach this allowlist via the `\`-separator
	// branch in classifyExternal, gated on isPhpNamespaceIdent. Keys
	// are lowercase here because isKnownExternalPackage case-folds the
	// lookup; the on-disk root is "Symfony", "Doctrine", etc. App\* is
	// intentionally absent — it's the project-local convention in
	// Symfony/Laravel layouts and must not be promoted to a placeholder.
	"symfony":    {},
	"doctrine":   {},
	"twig":       {},
	"laravel":    {},
	"illuminate": {},
	"psr":        {},
	// PHP testing / common third-party roots (issue #485 PHP wave-3).
	// PHPUnit lives at `PHPUnit\...`; Pest (modern Laravel testing)
	// installs assertions under `Pest\...`. Monolog, Carbon (Nesbot),
	// Guzzle, Faker, and Composer's autoloader are top-N transitive
	// imports across symfony-demo and laravel-quickstart `use`
	// statements that previously fell into bug-extractor.
	"phpunit":      {},
	"pest":         {},
	"pestphp":      {},
	"monolog":      {},
	"nesbot":       {},
	"carbon":       {},
	"guzzle":       {},
	"guzzlehttp":   {},
	"fakerphp":     {},
	"composer":     {},
	"phpstan":      {},
	"prophecy":     {},
	"mockery":      {},
	"webmozart":    {},
	"ramsey":       {},
	"sebastian":    {},
	"phpoption":    {},
	"swiftmailer":  {},
	"league":       {},
	"intervention": {},
	"spatie":       {},
	"barryvdh":     {},
	// C / C++ ecosystem (Issue #44 — spdlog bug-rate reduction). Header-
	// only libraries (spdlog, fmt, gtest, gmock, Catch2) and common
	// system / third-party C++ roots. The `std` allowlist key already
	// exists above (added for Rust) and is reused for STL-header
	// imports collapsed in classifyExternal.
	"spdlog":    {},
	"benchmark": {}, // Google Benchmark — used heavily in spdlog/bench
	"gtest":     {},
	"gmock":     {},
	"catch2":    {},
	"boost":     {},
	"eigen":     {},
	"qt":        {},
	"abseil":    {},
	"absl":      {},
	"folly":     {},
	"protobuf":  {},
	"grpc":      {},
	"openssl":   {},
	"zlib":      {},
	"curl":      {},
	// Swift ecosystem (Wave 3 — vapor-api-template, follow-up to #436).
	// Vapor + Fluent are the dominant server-side Swift stack; SwiftNIO
	// is the networking primitive Vapor builds on; Foundation/Combine/
	// SwiftUI/UIKit/AppKit are Apple stdlib top-levels; PackageDescription
	// is the Swift Package Manager DSL imported by `Package.swift`
	// manifests. Without these, Swift `import Vapor` lands in
	// bug-resolver (the extractor creates a SCOPE.Component named
	// "Vapor" per import — see internal/extractors/swift/swift.go
	// buildImport — and the resolver finds it, so it's flagged as a
	// bug-resolver ambiguous match rather than an external package).
	// XCTest is the test-runner stdlib (used by the XCTAssert* macros
	// already in swiftBareNames).
	"vapor":               {},
	"fluent":              {},
	"fluentsqlite":        {},
	"fluentpostgresql":    {},
	"fluentmysql":         {},
	"fluentmongo":         {},
	"swiftnio":            {},
	"nio":                 {},
	"niocore":             {},
	"niohttp1":            {},
	"niohttp2":            {},
	"niofoundationcompat": {},
	"foundation":          {},
	"combine":             {},
	"swiftui":             {},
	"uikit":               {},
	"appkit":              {},
	"xctest":              {},
	"packagedescription":  {},
	"console":             {},
	"jwt":                 {},
	"leaf":                {},
	// `redis` already on the allowlist via the Python ecosystem block.
	//
	// Wave 4 — vapor framework source residual (14.27% bug-rate).
	// The SwiftNIO sister modules (`NIOPosix`, `NIOConcurrencyHelpers`,
	// `NIOSSL`, `NIOExtras`, `NIOWebSocket`, `NIOTransportServices`,
	// `_NIOFileSystem`), Apple SSWG packages (`Logging`, `Crypto`,
	// `_CryptoExtras`, `AsyncKit`, `AsyncHTTPClient`, `ServiceLifecycle`,
	// `Metrics`, `Tracing`, `Atomics`, `Collections`, `Algorithms`,
	// `SystemPackage`, `ArgumentParser`), Vapor sister kits (`RoutingKit`,
	// `ConsoleKit`, `ConsoleKitTerminal`, `ConsoleKitCommands`,
	// `MultipartKit`, `WebSocketKit`, `CVaporBcrypt`), and platform
	// shims (`Glibc`, `Musl`, `Android`, `Darwin`, `Dispatch`) are the
	// dominant unresolved IMPORTS in the Vapor framework source. Each
	// is a real SwiftPM-published module that lives outside any indexed
	// corpus and should land in ExternalKnown rather than bug-extractor.
	"nioposix":                       {},
	"nioconcurrencyhelpers":          {},
	"niossl":                         {},
	"nioextras":                      {},
	"niowebsocket":                   {},
	"niotransportservices":           {},
	"_niofilesystem":                 {},
	"niofilesystem":                  {},
	"nioembedded":                    {},
	"niohttpcompression":             {},
	"niohttptypes":                   {},
	"niohttptypeshttp1":              {},
	"niotls":                         {},
	"niohpack":                       {},
	"winsdk":                         {},
	"_niofilesystemfoundationcompat": {},
	"niofilesystemfoundationcompat":  {},
	"servicecontextmodule":           {},
	"swiftasn1":                      {},
	"cniolinux":                      {},
	"cniodarwin":                     {},
	"cniowindows":                    {},
	"cnioposix":                      {},
	"cnioatomics":                    {},
	"x509":                           {},
	"basicauth":                      {},
	// `logging`, `crypto`, `tracing`, `collections` already present
	// elsewhere in the allowlist.
	"_cryptoextras":      {},
	"cryptoextras":       {},
	"asynckit":           {},
	"asynchttpclient":    {},
	"servicelifecycle":   {},
	"metrics":            {},
	"atomics":            {},
	"algorithms":         {},
	"systempackage":      {},
	"argumentparser":     {},
	"routingkit":         {},
	"consolekit":         {},
	"consolekitterminal": {},
	"consolekitcommands": {},
	"multipartkit":       {},
	"websocketkit":       {},
	"cvaporbcrypt":       {},
	"glibc":              {},
	"musl":               {},
	"android":            {},
	"darwin":             {},
	"dispatch":           {},
}

// swiftImportAttributePrefixes is the lang=="swift"-gated strip-list
// for Swift import attribute syntax that the extractor currently folds
// into the import path. Each entry is matched as a literal prefix of
// the synth-time `name` token; on match the prefix is removed and the
// remainder is re-classified. Order is longest-first so that nested
// combinations (`_documentation(visibility:internal)` followed by
// `_exported`) collapse in a single pass. Chain-fix tracked separately.
var swiftImportAttributePrefixes = []string{
	"_documentation.visibility.internal._exported.",
	"_documentation.visibility.public._exported.",
	"_documentation.visibility.internal.",
	"_documentation.visibility.public.",
	"_exported.",
	"preconcurrency.",
	"_implementationOnly.",
	"testable.",
}

// googleBenchmarkBareNames is the cpp-gated Google Benchmark public-
// API stop-list (issue #44 — spdlog/bench bug-rate). UpperCamelCase
// surface from <benchmark/benchmark.h>. Receiver-stripped from
// `benchmark::DoNotOptimize(...)`, `benchmark::Initialize(...)` etc.
// Conservative — only the highest-volume, most-distinctive names.
var googleBenchmarkBareNames = map[string]struct{}{
	"DoNotOptimize":          {},
	"ClobberMemory":          {},
	"RegisterBenchmark":      {},
	"RunSpecifiedBenchmarks": {},
	"Initialize":             {},
	"Shutdown":               {},
	"UseRealTime":            {},
	"UseManualTime":          {},
	"MeasureProcessCPUTime":  {},
	"Iterations":             {},
	"Threads":                {},
	"ThreadRange":            {},
	"Range":                  {},
	"RangeMultiplier":        {},
	"Unit":                   {},
	"MinTime":                {},
	"Repetitions":            {},
	"ReportAggregatesOnly":   {},
	"DisplayAggregatesOnly":  {},
	"Args":                   {},
	"ArgsProduct":            {},
	"DenseRange":             {},
	"SkipWithError":          {},
	"ResumeTiming":           {},
	"PauseTiming":            {},
	"SetLabel":               {},
	"SetComplexityN":         {},
	"SetBytesProcessed":      {},
	"SetItemsProcessed":      {},
}

// isSpdlogFactoryName reports whether s matches the spdlog public
// factory-function shape — snake_case lowercase identifier ending in
// `_mt` (multi-threaded) or `_st` (single-threaded). Examples:
// basic_logger_mt, daily_logger_st, rotating_logger_mt,
// stdout_color_mt, stderr_color_st, syslog_logger_mt, udp_logger_st,
// callback_logger_mt, android_logger_mt. Used by classifyExternal
// (issue #44) to route these to the ext:spdlog placeholder. The
// `_mt`/`_st` suffix is the spdlog naming convention and is
// extremely unlikely to appear on user-defined methods unrelated to
// spdlog. Conservative shape check: at least 5 chars, prefix is
// snake_case lowercase letters/digits/underscores, ends in `_mt` /
// `_st`, prefix is non-empty after suffix strip.
// isSpdlogSinkOrFormatterShape reports whether s matches the distinctive
// spdlog `*_sink` / `*_formatter` class-name convention. Used (cpp/c
// gated) to route receiver-stripped bare references to spdlog internal
// classes (`rotating_file_sink`, `short_filename_formatter`,
// `T_formatter`, `Y_formatter`, ...) to ext:spdlog rather than
// bug-extractor. The suffixes are overwhelmingly spdlog idioms in
// real C++ corpora; the small false-positive risk (a user defining
// their own `my_sink` class outside the spdlog allowlist context) is
// preferable to leaving 60+ unresolved bare names in bug-extractor.
func isSpdlogSinkOrFormatterShape(s string) bool {
	const (
		sinkSuf = "_sink"
		fmtSuf  = "_formatter"
	)
	switch {
	case strings.HasSuffix(s, sinkSuf):
		prefix := s[:len(s)-len(sinkSuf)]
		if prefix == "" {
			return false
		}
		return isSnakeOrLowerCppIdent(prefix)
	case strings.HasSuffix(s, fmtSuf):
		prefix := s[:len(s)-len(fmtSuf)]
		if prefix == "" {
			return false
		}
		return isSnakeOrLowerCppIdent(prefix)
	}
	return false
}

// isSnakeOrLowerCppIdent reports whether s is a C++ identifier prefix
// consisting only of lowercase letters, digits, and underscores, OR a
// single uppercase letter (spdlog pattern-flag formatter shape:
// `T_formatter`, `Y_formatter`, ...).
func isSnakeOrLowerCppIdent(s string) bool {
	if s == "" {
		return false
	}
	// Single uppercase letter prefix (T_formatter, Y_formatter, ...).
	if len(s) == 1 && s[0] >= 'A' && s[0] <= 'Z' {
		return true
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '_':
		default:
			return false
		}
	}
	// Must not start or end with underscore (avoids `_sink` / `sink_`-
	// style member-variable confusions; we want real class names).
	if s[0] == '_' || s[len(s)-1] == '_' {
		return false
	}
	return true
}

// qtBareNames is the cpp/c-gated Qt API stop-list (spdlog wave follow-up).
// Qt's QTextEdit / QTextCursor / QTextCharFormat / QMetaObject methods
// receiver-stripped from spdlog/sinks/qt_sinks.h call sites. Distinctive
// camelCase Qt names virtually never user-defined. Conservative scope:
// only names that actually surface as bug-extractor in spdlog and are
// unambiguously Qt API (no generic English verbs).
var qtBareNames = map[string]struct{}{
	"setForeground":      {},
	"setBackground":      {},
	"setCharFormat":      {},
	"currentCharFormat":  {},
	"movePosition":       {},
	"insertText":         {},
	"deleteChar":         {},
	"removeSelectedText": {},
	"blockCount":         {},
	"fromUtf8":           {},
	"fromLatin1":         {},
	"invokeMethod":       {},
	"Q_ARG":              {},
}

func isSpdlogFactoryName(s string) bool {
	if len(s) < 5 {
		return false
	}
	if !(strings.HasSuffix(s, "_mt") || strings.HasSuffix(s, "_st")) {
		return false
	}
	prefix := s[:len(s)-3]
	if prefix == "" || prefix[len(prefix)-1] == '_' {
		return false
	}
	// Must contain at least one '_' in the prefix so we're matching
	// snake_case identifiers, not short ambiguous names like `cm_mt`.
	if !strings.ContainsRune(prefix, '_') {
		return false
	}
	for _, c := range prefix {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '_':
		default:
			return false
		}
	}
	return true
}

// isFmtBundledFile reports whether the from-file path lives inside a
// bundled fmt library tree (typical layout: `include/<lib>/fmt/bundled
// /...`). Used by classifyExternal (issue #44) to route bare CALLS
// originating from these vendored sources to ext:fmt.
func isFmtBundledFile(p string) bool {
	if p == "" {
		return false
	}
	return strings.Contains(p, "/fmt/bundled/") || strings.HasPrefix(p, "fmt/bundled/")
}

// isJQueryBundledFile reports whether p is a JavaScript file shipped as
// part of a vendored jQuery / jquery-validation / jquery-unobtrusive
// bundle — the canonical ASP.NET Core project-template layout puts these
// under `wwwroot/lib/jquery*/`, and the filenames themselves start with
// `jquery.` (e.g. `jquery.validate.unobtrusive.js`). Used by the jQuery
// bare-name gate (issue #441) to scope the jqueryBareNames stop-list to
// vendored library code without polluting hand-written JS in other
// codebases.
func isJQueryBundledFile(p string) bool {
	if p == "" {
		return false
	}
	if strings.Contains(p, "/wwwroot/lib/jquery") || strings.Contains(p, "/lib/jquery") {
		return true
	}
	// basename-shape check: `jquery.<...>.js` / `jquery-<...>.js` / `jquery.js`.
	slash := strings.LastIndexByte(p, '/')
	base := p
	if slash >= 0 {
		base = p[slash+1:]
	}
	if strings.HasPrefix(base, "jquery.") || strings.HasPrefix(base, "jquery-") || base == "jquery.js" {
		return true
	}
	return false
}

// jqueryBareNames is the jQuery-bundled-file-gated bare-name stop-list
// (issue #441). The vendored jquery / jquery-validation / jquery-
// unobtrusive sources call receiver-stripped jQuery surface methods
// (`$(...).addClass(...)` → `addClass`, `$.extend(...)` → `extend`)
// inside their own implementation; the resolver can't bind these to
// local entities, so they land in bug-extractor.
//
// Gated on isJQueryBundledFile(fromFile) so the same generic verbs
// (`hide`/`show`/`find`/`empty`) don't shadow hand-written user methods
// in non-jQuery JS codebases. Same safer-bias rule as the Go testify
// gate (#115). Folds to `ext:jquery` (added to knownExternalPackages
// below) so the stubs get ExternalKnown disposition.
var jqueryBareNames = map[string]struct{}{
	// jQuery CSS class manipulation
	"addClass":    {},
	"removeClass": {},
	"toggleClass": {},
	"hasClass":    {},
	// jQuery DOM traversal / manipulation
	"appendTo":     {},
	"prependTo":    {},
	"insertAfter":  {},
	"insertBefore": {},
	"replaceWith":  {},
	"wrap":         {},
	"unwrap":       {},
	"clone":        {},
	"detach":       {},
	"empty":        {},
	"remove":       {},
	"html":         {},
	"text":         {},
	"val":          {},
	"attr":         {},
	"removeAttr":   {},
	"prop":         {},
	"removeProp":   {},
	"css":          {},
	"data":         {},
	"removeData":   {},
	// jQuery effects
	"show":        {},
	"hide":        {},
	"toggle":      {},
	"fadeIn":      {},
	"fadeOut":     {},
	"fadeTo":      {},
	"slideUp":     {},
	"slideDown":   {},
	"slideToggle": {},
	// jQuery events
	"on":             {},
	"off":            {},
	"one":            {},
	"trigger":        {},
	"triggerHandler": {},
	"bind":           {},
	"unbind":         {},
	"delegate":       {},
	"undelegate":     {},
	// jQuery utility / static helpers (`$.extend`, `$.each`, `$.isFunction`,
	// `$.parseJSON`, `$.proxy`, `$.ajax`, etc.).
	"extend":        {},
	"each":          {},
	"isFunction":    {},
	"isArray":       {},
	"isPlainObject": {},
	"isEmptyObject": {},
	"isNumeric":     {},
	"isWindow":      {},
	"parseJSON":     {},
	"parseHTML":     {},
	"parseXML":      {},
	"proxy":         {},
	"ajax":          {},
	"get":           {},
	"post":          {},
	"getJSON":       {},
	"getScript":     {},
	"makeArray":     {},
	"inArray":       {},
	"grep":          {},
	"merge":         {},
	"noop":          {},
	"now":           {},
	"trim":          {},
	"type":          {},
	"contains":      {},
	"globalEval":    {},
	// jQuery selector traversal
	"find":     {},
	"filter":   {},
	"closest":  {},
	"parent":   {},
	"parents":  {},
	"children": {},
	"siblings": {},
	"next":     {},
	"prev":     {},
	"nextAll":  {},
	"prevAll":  {},
	"first":    {},
	"last":     {},
	"eq":       {},
	"index":    {},
	"slice":    {},
	"add":      {},
	"andSelf":  {},
	"end":      {},
	"is":       {},
	"not":      {},
	"has":      {},
	"each_":    {}, // sentinel — guarded variant
	// jQuery form-related helpers used by jquery-validation /
	// jquery-validation-unobtrusive.
	"serialize":      {},
	"serializeArray": {},
	"submit":         {},
	"focus":          {},
	"blur":           {},
	"click":          {},
	"change":         {},
	"keydown":        {},
	"keyup":          {},
	"keypress":       {},
	"mouseenter":     {},
	"mouseleave":     {},
	"mouseover":      {},
	"mouseout":       {},
	"hover":          {},
	"select":         {},
	"ready":          {},
	// jquery-validation plugin surface
	"validate":         {},
	"valid":            {},
	"resetForm":        {},
	"rules":            {},
	"unobtrusive":      {},
	"validator":        {},
	"showErrors":       {},
	"numberOfInvalids": {},
	// jQuery internal/private surface that leaks via receiver-strip
	// inside the validation bundles.
	"apply":       {},
	"call":        {},
	"replace":     {}, // String.prototype.replace + jQuery internal
	"split":       {},
	"substr":      {},
	"substring":   {},
	"charAt":      {},
	"charCodeAt":  {},
	"indexOf":     {},
	"lastIndexOf": {},
	"concat":      {},
	"join":        {},
	"reverse":     {},
	"sort":        {},
	"shift":       {},
	"unshift":     {},
	"pop":         {},
	"splice":      {},
	// `$` itself appears as a bare CALLS target when the extractor
	// can't bind the jQuery global.
	"$": {},
}

// isFmtMacroIdent reports whether s is an UPPER_SNAKE_CASE identifier
// with the FMT_ prefix — used by classifyExternal (issue #44) to route
// fmt-library preprocessor macros (FMT_ASSERT, FMT_THROW, FMT_ENABLE_
// IF, FMT_STRING, ...) to the ext:fmt placeholder.
func isFmtMacroIdent(s string) bool {
	const prefix = "FMT_"
	if !strings.HasPrefix(s, prefix) || len(s) <= len(prefix) {
		return false
	}
	for _, c := range s[len(prefix):] {
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '_':
		default:
			return false
		}
	}
	return true
}

// catch2BareNames is the cpp-gated Catch2 test-macro stop-list
// (issue #44). Catch2 uses UpperCamelCase / UPPER_SNAKE_CASE macros
// that survive the cpp extractor as bare CALLS edges. Routed to
// ext:catch2.
var catch2BareNames = map[string]struct{}{
	"REQUIRE":             {},
	"REQUIRE_FALSE":       {},
	"REQUIRE_NOTHROW":     {},
	"REQUIRE_THROWS":      {},
	"REQUIRE_THROWS_AS":   {},
	"REQUIRE_THROWS_WITH": {},
	"REQUIRE_THAT":        {},
	"CHECK":               {},
	"CHECK_FALSE":         {},
	"CHECK_NOTHROW":       {},
	"CHECK_THROWS":        {},
	"CHECK_THROWS_AS":     {},
	"CHECK_THAT":          {},
	"SECTION":             {},
	"TEST_CASE":           {},
	"TEST_CASE_METHOD":    {},
	"SCENARIO":            {},
	"GIVEN":               {},
	"WHEN":                {},
	"THEN":                {},
	"AND_WHEN":            {},
	"AND_THEN":            {},
	"INFO":                {},
	"FAIL":                {},
	"WARN":                {},
	"CAPTURE":             {},
	"SUCCEED":             {},
	"DYNAMIC_SECTION":     {},
	"GENERATE":            {},
	"BENCHMARK":           {},
	"DOCTEST_TEST_CASE":   {},
	"DOCTEST_CHECK":       {},
}

// isSpdlogMacroIdent reports whether s is an UPPER_SNAKE_CASE
// identifier with the SPDLOG_ prefix — used by classifyExternal to
// route spdlog preprocessor macros (SPDLOG_LOGGER_DEBUG, SPDLOG_TRACE,
// SPDLOG_LOGGER_CATCH, SPDLOG_THROW, ...) to the ext:spdlog placeholder
// (issue #44). Strict shape: starts with "SPDLOG_", remaining chars are
// all upper-case ASCII letters, digits, or underscores. No leading
// double underscore (reserved). Conservative — a leading underscore in
// the suffix would reject, so plain `SPDLOG_` alone is rejected.
func isSpdlogMacroIdent(s string) bool {
	const prefix = "SPDLOG_"
	if !strings.HasPrefix(s, prefix) || len(s) <= len(prefix) {
		return false
	}
	for _, c := range s[len(prefix):] {
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '_':
		default:
			return false
		}
	}
	return true
}

// isKindLikePrefix reports whether s is a short, alphabetic kind name
// like "Module" or "Function" — used to decide whether a "Foo:Bar"
// stub should be treated as Kind:Name. The structural-ref shape
// "scope:..." has multiple ':'s and a long prefix; this filter avoids
// claiming those.
//
// '.' is intentionally allowed in the prefix character class to admit
// dotted-kind shapes like "a.b.c:Symbol" that some extractors emit
// (e.g. fully-qualified Python or JVM kind hints). The trade-off is
// that "java.util.Map:put" looks kind-like and gets treated as a
// Kind:Name pair — that's what we want for these external lookups.
// isNodeBuiltinIdent reports whether s is a plausible Node.js builtin
// module identifier after the `node:` prefix (e.g. "fs", "fs/promises",
// "async_hooks", "perf_hooks", "test"). Conservative shape check:
// alphanumeric + `_` + a single `/` for sub-modules; rejects empty,
// leading-digit, and pathological forms. Wave-4 fallback for
// node:<mod> imports not yet enumerated in the allowlist.
func isNodeBuiltinIdent(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	if s[0] >= '0' && s[0] <= '9' {
		return false
	}
	slashCount := 0
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '_':
			// ok
		case c == '/':
			slashCount++
			if slashCount > 1 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func isKindLikePrefix(s string) bool {
	if len(s) == 0 || len(s) > 24 {
		return false
	}
	for _, c := range s {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '.') {
			return false
		}
	}
	return true
}

// looksLikeExternalImport reports whether a bare name has the shape of
// a dotted external import — at least one dot, terminal segment is a
// capitalised identifier, and no path separators. Used by Pass 4.5
// (issue #82) to synthesise placeholders for dangling EXTENDS targets
// like "serializers.ModelSerializer" that cross/hierarchy emits as
// structural-refs without an :external: marker.
func looksLikeExternalImport(s string) bool {
	if s == "" {
		return false
	}
	if strings.ContainsAny(s, "/\\ \t") {
		return false
	}
	dot := strings.IndexByte(s, '.')
	if dot <= 0 || dot >= len(s)-1 {
		return false
	}
	// Every segment must be a valid identifier (letters, digits, '_'),
	// non-empty, and not start with a digit.
	for _, seg := range strings.Split(s, ".") {
		if !isIdentSegment(seg) {
			return false
		}
	}
	// Terminal segment must start with an uppercase letter — the
	// convention for class/type names that show up as EXTENDS targets.
	last := s[strings.LastIndexByte(s, '.')+1:]
	if last == "" {
		return false
	}
	c := last[0]
	if !(c >= 'A' && c <= 'Z') {
		return false
	}
	return true
}

// isRustCrateIdent reports whether s has the shape of a Rust crate
// name — ASCII letters, digits, and '_', non-empty, not starting with
// a digit. Issue #101: gates the `::` separator branch so we only
// trust the leading segment of a use-path when it looks like a crate
// name (and not, e.g., a bracketed/spaced fragment that slipped in).
// isLowerRustIdent reports whether s is a Rust ident whose leading
// character is a lowercase ASCII letter or underscore. Used by the
// rust `::` branch of classifyExternal to identify the bare-sibling-
// module shape (`worker::Foo`, `entry::Bar`) — sibling modules are
// always lowercase in idiomatic Rust, while unknown third-party
// crates are also conventionally lowercase, so this gate is not
// itself the discriminator (both folds land in non-bug buckets,
// which is the win).
func isLowerRustIdent(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	return (c >= 'a' && c <= 'z') || c == '_'
}

func isRustCrateIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c == '_':
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// isPhpNamespaceIdent reports whether s has the shape of a PHP
// top-level namespace segment — ASCII letters, digits, and '_',
// non-empty, not starting with a digit, and starting with an
// uppercase letter (PHP convention for vendor/namespace roots:
// Symfony, Doctrine, Twig, Psr, App, ...). Issue #102: gates the
// `\` separator branch so we only trust the leading segment of a
// use-statement when it looks like a namespace root, not a stray
// fragment with backslashes that slipped in from elsewhere.
func isPhpNamespaceIdent(s string) bool {
	if s == "" {
		return false
	}
	if c := s[0]; !(c >= 'A' && c <= 'Z') {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c == '_':
		case c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}

// isIdentSegment reports whether s is a non-empty identifier segment
// (ASCII letters/digits/underscore, not starting with a digit).
func isIdentSegment(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c == '_':
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// externalAPIHost extracts the host segment from a URL-shaped string.
// Returns "" when raw doesn't look like a URL with a recognisable host.
// Issue #89.
//
// Issue #94 follow-up: the original byte-scanning implementation broke
// on IPv6 hosts ("https://[::1]:8080" canonicalised to "[" because the
// port-stripping ran before the bracket-balanced host was extracted).
// Switched to net/url which understands bracketed IPv6 hosts and gives
// a clean Hostname() without brackets or port. Falls back to "" on
// parse error or any URL without a host.
func externalAPIHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Require an explicit scheme to keep behaviour close to the prior
	// "://" gate; net/url is permissive about scheme-less inputs.
	if !strings.Contains(raw, "://") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if host == "" {
		return ""
	}
	// Reject obviously malformed hosts (e.g. percent-encoded garbage
	// that survived parsing).
	for _, r := range host {
		if r == '%' {
			return ""
		}
	}
	return host
}

// isGoImportPath reports whether s has the shape of a Go import path
// — slash-separated, no backslashes, no colons (rules out URLs and
// "Kind:Name" forms), no whitespace, and the first segment is a
// lowercase ASCII identifier (Go package names are conventionally
// lowercase; host prefixes like "github.com" are also lowercase).
// Issue #116: gates the `/` separator branch in classifyExternal so
// only Go-shaped paths trigger split-and-lookup, not Unix file paths.
func isGoImportPath(s string) bool {
	if s == "" {
		return false
	}
	if !strings.Contains(s, "/") {
		return false
	}
	if strings.ContainsAny(s, ":\\ \t") {
		return false
	}
	// Reject leading slash — that's a Unix absolute path, not a Go
	// import path.
	if s[0] == '/' {
		return false
	}
	// First segment must be a lowercase identifier (letters, digits,
	// '_', '-', '.'). Go stdlib packages are single-word lowercase;
	// host prefixes like "github.com" / "golang.org" / "gopkg.in" are
	// also lowercase ASCII with dots.
	slash := strings.IndexByte(s, '/')
	first := s[:slash]
	if first == "" {
		return false
	}
	if c := first[0]; !(c >= 'a' && c <= 'z') {
		return false
	}
	for _, c := range first {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '_' || c == '-' || c == '.':
		default:
			return false
		}
	}
	return true
}

// isGoImportHost reports whether s looks like a host prefix in a Go
// import path — contains a '.' (e.g. "github.com", "golang.org",
// "gopkg.in", "google.golang.org"). Stdlib package roots like "net"
// or "encoding" never contain a dot.
func isGoImportHost(s string) bool {
	return strings.IndexByte(s, '.') > 0
}

// goHostCanonical returns the canonical "<host>/<owner>/<repo>" (or
// "<host>/<pkg>" for gopkg.in's two-segment shape, or "<host>/x/<repo>"
// for golang.org/x). Returns "" when the segment count is too short
// to identify a package.
func goHostCanonical(segs []string) string {
	if len(segs) < 2 {
		return ""
	}
	host := segs[0]
	// gopkg.in uses a two-segment shape: gopkg.in/<pkg>.<vN> (the
	// version is encoded in the package segment, not as a separate
	// directory). Collapse to "<host>/<pkg>" — full key on the
	// allowlist, no trailing import path.
	if host == "gopkg.in" {
		return host + "/" + segs[1]
	}
	// golang.org/x/<repo>/<subpath>... → "<host>/x/<repo>". The "x"
	// owner segment is universal for the golang.org/x staging-grounds
	// modules.
	if host == "golang.org" {
		if len(segs) >= 3 {
			return host + "/" + segs[1] + "/" + segs[2]
		}
		return ""
	}
	// google.golang.org/<module>/<subpath>... → "<host>/<module>"
	// (two-segment shape — modules like grpc, protobuf, api).
	if host == "google.golang.org" {
		return host + "/" + segs[1]
	}
	// Default host shape: "<host>/<owner>/<repo>" (github.com,
	// gitlab.com, bitbucket.org, ...). Subpaths are dropped so a
	// single placeholder represents the module.
	if len(segs) >= 3 {
		return host + "/" + segs[1] + "/" + segs[2]
	}
	return ""
}

// dockerImageRepo extracts the canonical repository segment from a Docker
// image reference, dropping the tag (`:<tag>`) or digest (`@sha256:...`).
// Returns "" for malformed inputs.
//
// Examples (Issue #424):
//
//	"nginx:1.21"                      → "nginx"
//	"redis:alpine"                    → "redis"
//	"library/postgres:14"             → "library/postgres"
//	"ghcr.io/owner/svc:v1.2.3"        → "ghcr.io/owner/svc"
//	"myregistry.io:5000/team/api:dev" → "myregistry.io:5000/team/api"
//	"alpine@sha256:abc..."            → "alpine"
//	""                                → ""
func dockerImageRepo(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	// Strip the digest first — it always uses `@` and never contains `:`
	// inside the digest body for our purposes (the algorithm prefix uses `:`
	// but lives strictly to the right of the `@`).
	if at := strings.IndexByte(ref, '@'); at >= 0 {
		ref = ref[:at]
	}
	// Find the last `:` and decide whether it's a tag separator or part of a
	// registry-port specifier. A tag separator never appears after a `/` of
	// a path; a registry port is always followed by `/`.
	lastColon := strings.LastIndexByte(ref, ':')
	if lastColon < 0 {
		return ref
	}
	// If there's a `/` to the right of the colon, the colon belongs to a
	// registry-port (e.g. "myregistry.io:5000/team/api") — keep it.
	if strings.IndexByte(ref[lastColon:], '/') >= 0 {
		return ref
	}
	repo := ref[:lastColon]
	if repo == "" {
		return ""
	}
	return repo
}

// ghaActionRepo strips the @version/sha suffix from a GitHub Actions
// `uses:` reference, returning the canonical action repo identity.
//
//	"actions/checkout@v4"                            → "actions/checkout"
//	"docker/build-push-action@0565240e2d4ab88bba..." → "docker/build-push-action"
//	"github/codeql-action/upload-sarif@v3"           → "github/codeql-action"
//	"./.github/actions/local-action"                 → "" (local, not external)
//
// Local action paths (starting with `./` or `../`) live inside the corpus
// and should NOT be lifted to external; returning "" makes the caller fall
// through and the resolver treats them as in-corpus refs.
//
// Refs #44.
func ghaActionRepo(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	// Local actions are in-corpus — skip.
	if strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "../") {
		return ""
	}
	// Docker actions are written `docker://image:tag` — already handled by
	// the docker_image branch; defer to that path.
	if strings.HasPrefix(ref, "docker://") {
		return ""
	}
	// Strip the version pin (@v4, @sha, @branch).
	if at := strings.IndexByte(ref, '@'); at >= 0 {
		ref = ref[:at]
	}
	// Canonicalise to "<org>/<repo>"; collapse any subpath (e.g.
	// "github/codeql-action/upload-sarif" → "github/codeql-action") so all
	// uses of the same action repo land on one placeholder.
	parts := strings.SplitN(ref, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

// isHexID mirrors resolve.isHexID — a 16-char lower-hex string is
// already an entity ID and must never be treated as a stub.
func isHexID(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// zigBareNames is the Zig-language-gated bare-name stop-list (issue
// #382 follow-up — wave-3 http.zig fix). The Zig extractor takes the
// rightmost segment of a dotted call (`std.debug.assert(...)` →
// `assert`, `std.mem.Allocator.alignedAlloc(...)` → `alignedAlloc`,
// `std.ArrayList(u8).init(...)` → `init` (filtered out as risky),
// `std.StringHashMap(...)` → `StringHashMap`), so high-volume zig
// stdlib calls land in bug-extractor as unresolved bare CALLS.
//
// Mirrors the Swift / Kotlin Ktor precedent (#435 / #436): the
// language gate (lang == "zig") is the safety net that prevents
// names like `assert`, `dupe`, `allocPrint` from shadowing user-
// defined methods in Go / JS / Python / Ruby / Kotlin codebases.
//
// Conservative selection (lessons from #94 / #105 / #106): generic
// verbs that are routinely user-defined on a Zig struct
// (`init`, `deinit`, `free`, `close`, `get`, `parse`, `write`,
// `read`, `add`, `remove`, `create`, `destroy`, `run`, `stop`,
// `accept`, `listen`, `send`, `recv`, `set`, `put`, `delete`,
// `clear`, `reset`, `start`, `update`, `flush`, `commit`) are
// deliberately EXCLUDED — the bug-resolver `ambig-bare-hint-fail`
// histogram on http.zig shows these names with 2+ user-defined
// candidates in the local graph. Including them would synthesise
// `ext:init` placeholders for real user methods, breaking #94's
// safer-bias rule.
//
// Categories:
//   - std.debug helpers (`assert`, `print` (already global),
//     `panic`, `warn`).
//   - std.mem / Allocator helpers (`alloc`, `free` excluded as
//     risky; `dupe`, `alignedAlloc`, `allocPrint`, `toBytes`,
//     `asBytes`, `eql`, `align`, `copy`, `zeroes`,
//     `lessThan`, `indexOf`, `indexOfScalar`, `lastIndexOf`,
//     `startsWith`, `endsWith`, `trim`, `trimLeft`, `trimRight`,
//     `splitScalar`, `splitSequence`, `tokenize`, `tokenizeAny`,
//     `concat`, `join` (already global), `replaceScalar`).
//   - std container constructors (`ArrayList`, `ArrayListUnmanaged`,
//     `StringHashMap`, `StringHashMapUnmanaged`, `AutoHashMap`,
//     `AutoHashMapUnmanaged`, `MemoryPool`, `BufSet`, `BufMap`,
//     `DoublyLinkedList`, `SinglyLinkedList`, `PriorityQueue`,
//     `PriorityDequeue`).
//   - std.fmt helpers (`allocPrint`, `bufPrint`, `comptimePrint`,
//     `parseInt`, `parseFloat`, `parseUnsigned`, `formatType`,
//     `formatFloat`, `formatInt`, `formatBuf`).
//   - std.json helpers (`parseFromSlice`, `parseFromSliceLeaky`,
//     `parseFromTokenSource`, `stringify`, `stringifyAlloc`).
//   - std.testing helpers (`expect`, `expectEqual`,
//     `expectEqualStrings`, `expectEqualSlices`, `expectError`,
//     `expectApproxEqAbs`, `expectApproxEqRel`,
//     `expectStringStartsWith`, `expectStringEndsWith`,
//     `expectFmt`, `expectString`, `expectGzip`, `expectAnyCall`).
//   - std.time helpers (`now`, `untilNow`, `nanoTimestamp`,
//     `milliTimestamp`, `microTimestamp`, `timestamp`, `sleep`).
//   - std.os / std.posix helpers (`exit`, `getenv`, `setenv`,
//     `getenvZ`, `getcwd`, `unexpectedWSAError`).
//   - std.heap helpers (`GeneralPurposeAllocator`,
//     `ArenaAllocator`, `page_allocator`, `c_allocator`,
//     `fromByteUnits`).
//   - std.atomic / std.Thread helpers (`lockUncancelable`,
//     `waitUncancelable`, `tryAcquire`, `release` excluded as
//     risky).
//   - Metrics / hash helpers commonly bare-called via dotted
//     chains (`incr`, `incrBy`, `hashFn`, `hasFn`).
var zigBareNames = map[string]struct{}{
	// std.debug
	"assert": {},
	"panic":  {},
	"warn":   {},

	// std.mem / Allocator helpers (exclude generic `alloc`/`free`).
	"dupe":           {},
	"dupeZ":          {},
	"alignedAlloc":   {},
	"toBytes":        {},
	"asBytes":        {},
	"bytesAsSlice":   {},
	"sliceAsBytes":   {},
	"eql":            {},
	"order":          {},
	"lessThan":       {},
	"indexOf":        {},
	"indexOfScalar":  {},
	"indexOfAny":     {},
	"lastIndexOf":    {},
	"startsWith":     {},
	"endsWith":       {},
	"trim":           {},
	"trimLeft":       {},
	"trimRight":      {},
	"splitScalar":    {},
	"splitSequence":  {},
	"splitAny":       {},
	"split":          {}, // std.mem.split — zig-gated, distinct from JS split
	"tokenize":       {},
	"tokenizeAny":    {},
	"tokenizeScalar": {},
	"concat":         {},
	"replaceScalar":  {},
	"copyForwards":   {},
	"copyBackwards":  {},
	"swap":           {},
	"zeroes":         {},
	"alignForward":   {},
	"alignBackward":  {},
	"isAligned":      {},
	"align":          {},

	// std container generic types (called with a type parameter:
	// `std.ArrayList(u8)`). The Zig extractor sees them as bare
	// constructor-shaped calls.
	"ArrayList":              {},
	"ArrayListUnmanaged":     {},
	"BoundedArray":           {},
	"StringHashMap":          {},
	"StringHashMapUnmanaged": {},
	"AutoHashMap":            {},
	"AutoHashMapUnmanaged":   {},
	"HashMap":                {},
	"HashMapUnmanaged":       {},
	"MemoryPool":             {},
	"BufSet":                 {},
	"BufMap":                 {},
	"DoublyLinkedList":       {},
	"SinglyLinkedList":       {},
	"PriorityQueue":          {},
	"PriorityDequeue":        {},
	"MultiArrayList":         {},
	"SegmentedList":          {},

	// std.fmt helpers.
	"allocPrint":     {},
	"allocPrintZ":    {},
	"bufPrint":       {},
	"bufPrintZ":      {},
	"comptimePrint":  {},
	"parseInt":       {},
	"parseFloat":     {},
	"parseUnsigned":  {},
	"formatType":     {},
	"formatFloat":    {},
	"formatInt":      {},
	"formatBuf":      {},
	"formatText":     {},
	"formatIntValue": {},

	// std.json helpers.
	"parseFromSlice":       {},
	"parseFromSliceLeaky":  {},
	"parseFromTokenSource": {},
	"stringify":            {},
	"stringifyAlloc":       {},

	// std.testing helpers. The zig test runner uses these as bare
	// top-level calls inside `test "name" { ... }` blocks. Names like
	// `expect`/`expectEqual` are distinctive and unlikely to collide
	// with a user-defined struct method.
	"expect":                 {},
	"expectEqual":            {},
	"expectEqualStrings":     {},
	"expectEqualSlices":      {},
	"expectEqualSentinel":    {},
	"expectEqualDeep":        {},
	"expectError":            {},
	"expectApproxEqAbs":      {},
	"expectApproxEqRel":      {},
	"expectStringStartsWith": {},
	"expectStringEndsWith":   {},
	"expectFmt":              {},
	"expectString":           {},
	"expectGzip":             {},

	// std.time helpers.
	"now":            {},
	"untilNow":       {},
	"nanoTimestamp":  {},
	"milliTimestamp": {},
	"microTimestamp": {},
	"timestamp":      {},

	// std.os / std.posix helpers. `exit`/`getenv` are distinctive
	// enough at the zig gate to be safe; user-defined `exit` on a
	// Zig struct is extremely rare.
	"unexpectedWSAError": {},
	"getenvZ":            {},
	"getcwdAlloc":        {},

	// std.heap helpers.
	"GeneralPurposeAllocator": {},
	"ArenaAllocator":          {},
	"FixedBufferAllocator":    {},
	"StackFallbackAllocator":  {},
	"page_allocator":          {},
	"c_allocator":             {},
	"raw_c_allocator":         {},
	"smp_allocator":           {},
	"fromByteUnits":           {},

	// std.Thread / std.atomic primitives that the extractor
	// receiver-strips when the lock/wait is on a struct field.
	"lockUncancelable": {},
	"waitUncancelable": {},
	"timedWaitFor":     {},

	// std.hash / std.crypto helpers that show up bare as dotted
	// leaves (`std.hash.Wyhash.hash` → `hashFn` / `hash`). Keep
	// only the distinctive-suffix names.
	"hashFn": {},
	"hasFn":  {},
	"incr":   {},
	"incrBy": {},

	// std.mem.Allocator method calls (`allocator.create(T)`,
	// `allocator.destroy(ptr)`, `allocator.resize(slice, n)`).
	// These are the canonical Zig manual-memory idioms and are
	// safe under the zig gate even though `create` / `destroy`
	// look generic — user-defined struct methods named `create` /
	// `destroy` are rare in idiomatic Zig (the convention is
	// `init` / `deinit`), and the zig gate keeps the names from
	// shadowing user methods in other ecosystems.
	"create":    {},
	"destroy":   {},
	"resize":    {},
	"realloc":   {},
	"rawAlloc":  {},
	"rawResize": {},
	"rawFree":   {},

	// std.ArrayList / ArrayListUnmanaged method surface.
	"appendSlice":                {},
	"appendNTimes":               {},
	"appendAssumeCapacity":       {},
	"appendSliceAssumeCapacity":  {},
	"ensureTotalCapacity":        {},
	"ensureUnusedCapacity":       {},
	"ensureTotalCapacityPrecise": {},
	"clearRetainingCapacity":     {},
	"clearAndFree":               {},
	"toOwnedSlice":               {},
	"toOwnedSliceSentinel":       {},
	"prepend":                    {},
	"insertSlice":                {},
	"swapRemove":                 {},
	"orderedRemove":              {},
	"popOrNull":                  {},
	"getPtr":                     {},
	"writableSliceGreedy":        {},

	// std.HashMap method surface.
	"getOrPut":               {},
	"getOrPutValue":          {},
	"getOrPutAssumeCapacity": {},
	"putAssumeCapacity":      {},
	"putNoClobber":           {},
	"fetchPut":               {},
	"fetchRemove":            {},
	"containsKey":            {},
	"removeByPtr":            {},
	"valueIterator":          {},
	"keyIterator":            {},

	// std.mem extras / std.ascii.
	"indexOfScalarPos":      {},
	"indexOfAnyPos":         {},
	"indexOfPos":            {},
	"lastIndexOfScalar":     {},
	"eqlIgnoreCase":         {},
	"sliceTo":               {},
	"span":                  {},
	"trimStart":             {},
	"trimEnd":               {},
	"trimLeadingSpace":      {},
	"trimLeadingSpaceCount": {},
	"toLower":               {},
	"toUpper":               {},
	"toInt":                 {},
	"toSeconds":             {},
	"toMicroseconds":        {},

	// std.fmt extras.
	"printInt":     {},
	"formatBuffer": {},

	// std.fs.
	"openFile":      {},
	"createFile":    {},
	"openDir":       {},
	"makePath":      {},
	"readFileAlloc": {},
	"writeFile":     {},

	// std.os / std.posix syscalls and helpers.
	"sleep":           {},
	"closesocket":     {},
	"socketpair":      {},
	"sigaction":       {},
	"sigemptyset":     {},
	"sigfillset":      {},
	"sigaddset":       {},
	"getpid":          {},
	"kill":            {},
	"setupConnection": {},

	// std.math helpers.
	"maxInt":           {},
	"minInt":           {},
	"shlExact":         {},
	"shrExact":         {},
	"uintAtMost":       {},
	"intRangeAtMost":   {},
	"intRangeLessThan": {},
	"divCeil":          {},
	"divFloor":         {},
	"divTrunc":         {},
	"divExact":         {},
	"mulWide":          {},
	"absCast":          {},
	"cast":             {},
	"lossyCast":        {},

	// std.mem byte-order helpers.
	"nativeToBig":    {},
	"nativeToLittle": {},
	"bigToNative":    {},
	"littleToNative": {},
	"readIntNative":  {},
	"readIntBig":     {},
	"readIntLittle":  {},
	"writeIntBig":    {},
	"writeIntLittle": {},
	"writeIntNative": {},
	"readInt":        {},
	"writeInt":       {},

	// std.io / buffered reader/writer.
	"bufferedReader": {},
	"bufferedWriter": {},
	"buffered":       {},
	"readSliceShort": {},
	"readVec":        {},

	// std.builtin / std.Build.
	"standardTargetOptions":  {},
	"standardOptimizeOption": {},
	"ArgsTuple":              {},
	"suggestVectorLength":    {},
	"isDarwin":               {},
	"isWindows":              {},
	"isLinux":                {},
	"isBSD":                  {},

	// std.json extras.
	"parseFree": {},

	// Random / std.rand.
	"DefaultPrng":          {},
	"intRangeAtMostBiased": {},

	// std.Build (`build.zig`) DSL — bare receiver-stripped methods on
	// the `*std.Build` and `*std.Build.Step.Compile` chains. Names
	// like `installArtifact` / `addExecutable` / `dependency` are
	// distinctive enough to be safe at the zig gate.
	"installArtifact":   {},
	"dependency":        {},
	"dependOn":          {},
	"createModule":      {},
	"addModule":         {},
	"addExecutable":     {},
	"addStaticLibrary":  {},
	"addSharedLibrary":  {},
	"addTest":           {},
	"addOptions":        {},
	"addCSourceFile":    {},
	"addCSourceFiles":   {},
	"addIncludePath":    {},
	"addLibraryPath":    {},
	"linkLibrary":       {},
	"linkSystemLibrary": {},
	"linkLibC":          {},
	"linkLibCpp":        {},
	"installHeader":     {},
	"installFile":       {},
	"installDirectory":  {},

	// std.log / std.debug additions.
	"info":                 {},
	"dumpErrorReturnTrace": {},
	"defaultPanic":         {},
	"detectError":          {},
	"frameText":            {},

	// std.ascii / unicode additions.
	"lowerString":  {},
	"upperString":  {},
	"isHex":        {},
	"isDigit":      {},
	"isAlpha":      {},
	"isAlphabetic": {},
	"isWhitespace": {},
	"isPrint":      {},
	"isUpper":      {},
	"isLower":      {},

	// std.net additions.
	"parseIp":     {},
	"parseIp4":    {},
	"parseIp6":    {},
	"resolveIp":   {},
	"getsockname": {},
	"getsockopt":  {},
	"setsockopt":  {},
	"ioctlsocket": {},

	// std.compress / std.crypto additions.
	"decompress":     {},
	"decodeHex":      {},
	"parseExtension": {},

	// std.io extras (fixed-buffer stream + reader/writer adaptors).
	"fixedBufferStream": {},

	// std.PriorityQueue / SegmentedList method surface.
	"popMin":  {},
	"peekMin": {},

	// std.time extras.
	"durationTo": {},

	// std.mem long-tail not yet covered.
	"indexOfNonePos": {},
	"consumeAll":     {},

	// std.log severity helpers — receiver-stripped from
	// `log.err(...)`, `log.info(...)`, `log.debug(...)`,
	// `log.warn(...)`. `err` and `debug` are common identifier names
	// so the zig gate is the safety net.
	"err":   {},
	"debug": {},

	// std.Build extras (`addRunArtifact`, `addArgs`, `addImport`,
	// `addOption`).
	"addRunArtifact": {},
	"addArgs":        {},
	"addImport":      {},
	"addOption":      {},

	// std.fs.Dir / std.fs absolute helpers.
	"deleteFileAbsolute": {},
	"deleteTreeAbsolute": {},

	// std.os.windows constants/syscalls (Win32 surface that the zig
	// extractor receiver-strips from `windows.WSASocketW(...)`).
	"WSASocketW":  {},
	"ReadFile":    {},
	"WriteFile":   {},
	"CloseHandle": {},

	// std.os.linux long-tail.
	"accept4": {},

	// std.Build.Step extras / generic helpers that show up bare.
	"advance":   {},
	"broadcast": {},
	"detach":    {},
}
