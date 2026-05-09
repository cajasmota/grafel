// Package resolve — import-aware cross-file CALLS resolution (issue #93).
//
// The base resolver (refs.go) maps a stub like "get" to the unique entity
// named "get" via byName. When two or more entities in the merged graph
// share a name (e.g. requests/api.py defines `get`, and a dozen tests also
// define `get`), the base resolver flips the name to ambiguous and the
// CALLS edge is left as a bug-* disposition.
//
// In real Python codebases the dominant share of post-#94 bug-extractor /
// bug-resolver dispositions are precisely this shape: a function imports
// a symbol from another module and then calls it bare. The extractor sees
// the CALLS site (`get(...)`), emits a bare-name target ("get"), but the
// resolver has no way to know which `get` was meant.
//
// This file adds an import-aware resolution pass:
//
//  1. BuildImportTable walks the merged EntityRecord slice and, from
//     IMPORTS relationships emitted by the per-language extractor (Python
//     is the first language plumbed through; others can opt in by
//     emitting the same Properties), builds a per-file map:
//
//     file_path → local_name → (source_module, imported_name)
//
//  2. ResolveImports walks every EntityRecord and rewrites embedded CALLS
//     edges whose ToID is a bare local name imported in the parent's
//     SourceFile. The rewrite picks an entity whose Name == imported_name
//     and whose SourceFile lives in the source_module's file set.
//
// The pass runs BEFORE BuildIndex / References so all subsequent stages
// (disposition classification, external synthesis, downstream traversal)
// see the rewritten ID.
package resolve

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// importRelKind is the relationship kind emitted by the Python (and any
// future) extractor for import statements. ImportTable consumes only
// relationships whose Kind matches this constant. Unexported because no
// caller outside this package needs it; the in-package tests reference it
// directly.
const importRelKind = "IMPORTS"

// Property keys read off an IMPORTS relationship. See
// internal/extractors/python/extractor.go:extractImports for the producer
// side. Languages other than Python can opt into import-aware resolution
// by emitting these same keys on their IMPORTS edges.
const (
	importPropLocalName    = "local_name"
	importPropSourceModule = "source_module"
	importPropImportedName = "imported_name"
	importPropWildcard     = "wildcard"
)

// ImportBinding describes a single name introduced into a file by an
// import statement.
type ImportBinding struct {
	// LocalName is the identifier as referenced inside the importing
	// file. For `import x.y` this is "x"; for `import x.y as z` this is
	// "z"; for `from a.b import c` this is "c"; for
	// `from a.b import c as d` this is "d".
	LocalName string
	// SourceModule is the dotted module path the symbol was imported
	// from. For `import x.y[.z]` this is the full path; for
	// `from a.b import c` this is "a.b".
	SourceModule string
	// ImportedName is the original (pre-alias) leaf identifier inside
	// the source module. Equal to LocalName when no alias is present.
	// For `from a import b as c` this is "b". For module imports
	// (`import x.y`) this is the full module path.
	ImportedName string
	// Wildcard is true for `from x import *`. The resolver treats these
	// best-effort: a bare CALLS target N is rewritten to <module>.N if
	// such an entity exists.
	Wildcard bool
}

// ImportTable maps file path → local-name → ImportBinding, plus the
// list of wildcard source modules per file. Local names that collide
// inside a single file are dropped (last-writer-wins is unsafe — Python
// rebinds, but we'd rather miss than misresolve).
type ImportTable struct {
	// byFile[file_path][local_name] = binding. Files with no imports
	// don't appear; local names that collide inside a single file are
	// removed (the resolver leaves the original CALLS stub alone).
	byFile map[string]map[string]ImportBinding
	// ambig[file_path][local_name] = true once a (file, local_name)
	// collision has been observed; further bindings for the same key
	// are ignored.
	ambig map[string]map[string]bool
	// wildcardModules[file_path] = list of dotted source modules that
	// were imported via `from X import *`. Best-effort lookup at
	// resolve time iterates this list when a bare name has no explicit
	// binding.
	wildcardModules map[string][]string
	// modulesByName[module_path] = list of entity SourceFiles that
	// belong to that dotted module path. Built from EntityRecord
	// SourceFile values, after normalising to forward-slash form. A
	// path "requests/api.py" contributes to modules "requests.api" and
	// (when it ends with `__init__.py`) "requests".
	modulesByName map[string]map[string]bool
	// entitiesByModuleName[module_path][name] = entity_id, populated
	// only when the (module_path, name) tuple resolves to exactly one
	// entity. Ambiguous tuples are tracked in ambigModuleName.
	entitiesByModuleName map[string]map[string]string
	ambigModuleName      map[string]map[string]bool
}

// BuildImportTable scans every embedded IMPORTS relationship in records
// and constructs the per-file import binding map plus a module → entity
// reverse index used by ResolveImports.
//
// The function reads only Properties on IMPORTS relationships; it does
// not mutate records. Callers typically invoke BuildImportTable AFTER
// stampEntityIDs so the entity ID is already populated when ResolveImports
// rewrites a CALLS target.
func BuildImportTable(records []types.EntityRecord) ImportTable {
	tbl := ImportTable{
		byFile:               make(map[string]map[string]ImportBinding),
		ambig:                make(map[string]map[string]bool),
		wildcardModules:      make(map[string][]string),
		modulesByName:        make(map[string]map[string]bool),
		entitiesByModuleName: make(map[string]map[string]string),
		ambigModuleName:      make(map[string]map[string]bool),
	}

	// Pass 1 — per-file import bindings.
	for k := range records {
		r := &records[k]
		for j := range r.Relationships {
			rel := &r.Relationships[j]
			if rel.Kind != importRelKind || rel.Properties == nil {
				continue
			}
			file := normalizePath(rel.FromID)
			if file == "" {
				file = normalizePath(r.SourceFile)
			}
			if file == "" {
				continue
			}
			module := strings.TrimSpace(rel.Properties[importPropSourceModule])
			if module == "" {
				continue
			}
			if rel.Properties[importPropWildcard] == "1" {
				tbl.wildcardModules[file] = append(tbl.wildcardModules[file], module)
				continue
			}
			local := strings.TrimSpace(rel.Properties[importPropLocalName])
			if local == "" {
				continue
			}
			imported := strings.TrimSpace(rel.Properties[importPropImportedName])
			if imported == "" {
				imported = local
			}
			if tbl.ambig[file] != nil && tbl.ambig[file][local] {
				continue
			}
			fileBucket := tbl.byFile[file]
			if fileBucket == nil {
				fileBucket = make(map[string]ImportBinding)
				tbl.byFile[file] = fileBucket
			}
			if existing, ok := fileBucket[local]; ok {
				if existing.SourceModule != module || existing.ImportedName != imported {
					delete(fileBucket, local)
					if tbl.ambig[file] == nil {
						tbl.ambig[file] = make(map[string]bool)
					}
					tbl.ambig[file][local] = true
				}
				continue
			}
			fileBucket[local] = ImportBinding{
				LocalName:    local,
				SourceModule: module,
				ImportedName: imported,
			}
		}
	}

	// Pass 2 — module → entity reverse index. We map every entity's
	// SourceFile to the dotted-module form(s) that path could satisfy
	// and record (module, name) → id when unique.
	for k := range records {
		e := &records[k]
		if e.ID == "" || e.Name == "" || e.SourceFile == "" {
			continue
		}
		// Skip the import-marker entities themselves so a `from x import y`
		// statement does not register `x.y` as a callable target — the
		// real `y` lives in module x and gets its own EntityRecord
		// elsewhere in the merged set.
		if e.Kind == "SCOPE.Component" && e.Subtype == "module" {
			continue
		}
		modules := modulesForFile(normalizePath(e.SourceFile))
		for _, mod := range modules {
			files := tbl.modulesByName[mod]
			if files == nil {
				files = make(map[string]bool)
				tbl.modulesByName[mod] = files
			}
			files[normalizePath(e.SourceFile)] = true

			if tbl.ambigModuleName[mod] != nil && tbl.ambigModuleName[mod][e.Name] {
				continue
			}
			bucket := tbl.entitiesByModuleName[mod]
			if bucket == nil {
				bucket = make(map[string]string)
				tbl.entitiesByModuleName[mod] = bucket
			}
			if existing, ok := bucket[e.Name]; ok && existing != e.ID {
				delete(bucket, e.Name)
				if tbl.ambigModuleName[mod] == nil {
					tbl.ambigModuleName[mod] = make(map[string]bool)
				}
				tbl.ambigModuleName[mod][e.Name] = true
				continue
			}
			bucket[e.Name] = e.ID
		}
	}

	return tbl
}

// modulesForFile returns the dotted-module forms of a file path. A path
// like "requests/api.py" satisfies module "requests.api". A path ending
// in "/__init__.py" also satisfies the parent directory's dotted form
// ("requests/__init__.py" → "requests"). Paths outside .py files return
// an empty slice; non-Python languages don't currently use this index.
func modulesForFile(p string) []string {
	if p == "" {
		return nil
	}
	if !strings.HasSuffix(p, ".py") {
		return nil
	}
	stripped := strings.TrimSuffix(p, ".py")
	out := []string{strings.ReplaceAll(stripped, "/", ".")}
	// __init__ rolls up to its parent directory's dotted name.
	if strings.HasSuffix(stripped, "/__init__") {
		parent := strings.TrimSuffix(stripped, "/__init__")
		if parent != "" {
			out = append(out, strings.ReplaceAll(parent, "/", "."))
		}
	}
	// A repo-relative path such as "src/requests/api.py" should also
	// satisfy "requests.api" so a CALLS site that imports `requests`
	// resolves regardless of whether the corpus checks the package out
	// at the repo root or under a `src/` prefix. We only strip ONE
	// leading segment, and only if it is one of the well-known source
	// roots below. This avoids the prior suffix-explosion behaviour
	// that exposed every tail of a dotted path ("a.b.c" → "b.c", "c")
	// and could collide with unrelated top-level packages in a
	// monorepo (e.g. a tools/ helper named the same as a real lib).
	for _, prefix := range sourceRootPrefixes {
		if strings.HasPrefix(out[0], prefix) {
			out = append(out, strings.TrimPrefix(out[0], prefix))
			break
		}
	}
	return out
}

// sourceRootPrefixes is the small allowlist of leading dotted-path
// segments that modulesForFile may strip once when computing alias
// dotted forms for an entity's source file. Anything else is left
// alone — broader stripping caused false positives in monorepos.
var sourceRootPrefixes = []string{"src.", "lib.", "app."}

// ResolveBareCallTarget looks up a bare-name CALLS target N in the import
// table for callerFile. Returns (entity_id, true) when an unambiguous
// match exists; ("", false) otherwise.
//
// Resolution order:
//  1. Explicit import binding for (file, name) — e.g. `from x import y`
//     → look up y in module x.
//  2. Module-attribute access — for every plain `import x[.y]` binding
//     in the file, try (source_module, name). This catches the
//     `x.foo()` call shape where the extractor stripped the receiver
//     and emitted ToID="foo".
//  3. Wildcard imports — `from x import *` makes every entity in x
//     callable as a bare name; best-effort.
func (t ImportTable) ResolveBareCallTarget(callerFile, name string) (string, bool) {
	if name == "" {
		return "", false
	}
	callerFile = normalizePath(callerFile)
	bucket := t.byFile[callerFile]
	if bucket != nil {
		if b, ok := bucket[name]; ok {
			if id, ok := t.lookupModuleEntity(b.SourceModule, b.ImportedName); ok {
				return id, true
			}
		}
	}
	// Module-attribute access: any plain `import x` in this file means
	// `x.foo()` extracted as bare "foo" should resolve to module x's foo.
	// We collect ALL candidate IDs across plain imports first; if exactly
	// one plain import yields a hit, use it; if two or more yield hits
	// (and disagree), the lookup is ambiguous and we drop — same
	// conservative policy as a (module, name) collision. Iterating the
	// map and short-circuiting on first hit would be non-deterministic.
	var (
		plainCandidate string
		plainHits      int
	)
	for _, b := range bucket {
		// "Plain" module imports are detected by source_module ==
		// imported_name (the extractor sets imported_name to the full
		// dotted module path for `import a.b`). Skip from-imports
		// (where imported_name is the leaf symbol name).
		if b.SourceModule != b.ImportedName {
			continue
		}
		if id, ok := t.lookupModuleEntity(b.SourceModule, name); ok {
			if plainHits == 0 {
				plainCandidate = id
				plainHits = 1
			} else if id != plainCandidate {
				plainHits++
			}
		}
	}
	if plainHits == 1 {
		return plainCandidate, true
	}
	if plainHits > 1 {
		return "", false
	}
	for _, mod := range t.wildcardModules[callerFile] {
		if id, ok := t.lookupModuleEntity(mod, name); ok {
			return id, true
		}
	}
	return "", false
}

// lookupModuleEntity returns (id, true) when (module, name) maps to
// exactly one entity. Ambiguous tuples return ("", false); the caller
// leaves the original CALLS stub alone.
func (t ImportTable) lookupModuleEntity(module, name string) (string, bool) {
	if module == "" || name == "" {
		return "", false
	}
	if t.ambigModuleName[module] != nil && t.ambigModuleName[module][name] {
		return "", false
	}
	bucket, ok := t.entitiesByModuleName[module]
	if !ok {
		return "", false
	}
	id, ok := bucket[name]
	if !ok {
		return "", false
	}
	return id, true
}

// ImportResolveStats reports how many CALLS endpoints the import-aware
// pass rewrote. Surfaced via the index.go stderr log so the verify2
// harness can attribute the bug-rate delta.
type ImportResolveStats struct {
	// CallsConsidered counts every embedded CALLS edge whose ToID was a
	// non-empty, non-hex bare name (i.e. a candidate for import-aware
	// rewrite).
	CallsConsidered int
	// CallsRewritten counts the subset of CallsConsidered that resolved
	// to a 16-char entity ID via the import table.
	CallsRewritten int
}

// ResolveImports rewrites embedded CALLS edges in records using the
// supplied import table. Returns counters describing the rewrite. Edges
// whose ToID is empty, already a hex ID, or contains a "." (already
// dotted) are skipped — those have either been resolved already or
// belong to the receiver-typed CALLS path that the base resolver
// handles via byMember.
func ResolveImports(records []types.EntityRecord, tbl ImportTable) ImportResolveStats {
	var stats ImportResolveStats
	for k := range records {
		e := &records[k]
		callerFile := normalizePath(e.SourceFile)
		if callerFile == "" {
			continue
		}
		for j := range e.Relationships {
			rel := &e.Relationships[j]
			if rel.Kind != "CALLS" {
				continue
			}
			to := rel.ToID
			if to == "" || isHexID(to) {
				continue
			}
			// Skip stubs that already encode a kind ("Kind:Name") or a
			// receiver-typed dotted target ("Class.method"). The base
			// resolver handles those via byKind / byMember.
			if strings.ContainsAny(to, ":.#") {
				continue
			}
			stats.CallsConsidered++
			id, ok := tbl.ResolveBareCallTarget(callerFile, to)
			if !ok {
				continue
			}
			rel.ToID = id
			stats.CallsRewritten++
		}
	}
	return stats
}
