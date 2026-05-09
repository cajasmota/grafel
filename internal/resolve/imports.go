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
	// methodsByFileName[source_file][method_name] = entity_id, populated
	// only for PHP method entities and only when the (file, name) tuple
	// resolves to exactly one entity. Ambiguous tuples are tracked in
	// ambigMethodFileName. Used by ResolvePHPFQNMethodTarget (issue #422)
	// to bind an FQN-method like `App\Controller\BlogController::list` to
	// the method declared in the class's source file.
	methodsByFileName   map[string]map[string]string
	ambigMethodFileName map[string]map[string]bool
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
		methodsByFileName:    make(map[string]map[string]string),
		ambigMethodFileName:  make(map[string]map[string]bool),
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
		// PHP method index for FQN-method resolution (issue #422). Index
		// every PHP SCOPE.Operation by (file, name). Methods declared
		// inside a class (the dominant case) have Name == method name —
		// the extractor doesn't pre-qualify with the class. We reuse the
		// SourceFile <-> class file 1:1 mapping (PSR-4 convention) at
		// lookup time: resolve the class FQN to its file, then probe
		// (file, method_name) here.
		if e.Language == "php" && e.Kind == "SCOPE.Operation" {
			file := normalizePath(e.SourceFile)
			if file != "" && e.Name != "" {
				if tbl.ambigMethodFileName[file] == nil ||
					!tbl.ambigMethodFileName[file][e.Name] {
					bucket := tbl.methodsByFileName[file]
					if bucket == nil {
						bucket = make(map[string]string)
						tbl.methodsByFileName[file] = bucket
					}
					if existing, ok := bucket[e.Name]; ok && existing != e.ID {
						delete(bucket, e.Name)
						if tbl.ambigMethodFileName[file] == nil {
							tbl.ambigMethodFileName[file] = make(map[string]bool)
						}
						tbl.ambigMethodFileName[file][e.Name] = true
					} else {
						bucket[e.Name] = e.ID
					}
				}
			}
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
// ("requests/__init__.py" → "requests"). Paths outside known languages
// return an empty slice.
func modulesForFile(p string) []string {
	if p == "" {
		return nil
	}
	switch {
	case strings.HasSuffix(p, ".py"):
		return modulesForPythonFile(p)
	case strings.HasSuffix(p, ".java"):
		return modulesForJavaFile(p)
	case strings.HasSuffix(p, ".php"):
		return modulesForPHPFile(p)
	case strings.HasSuffix(p, ".ts"),
		strings.HasSuffix(p, ".tsx"),
		strings.HasSuffix(p, ".js"),
		strings.HasSuffix(p, ".jsx"),
		strings.HasSuffix(p, ".mjs"),
		strings.HasSuffix(p, ".cjs"):
		return modulesForJSFile(p)
	}
	return nil
}

// modulesForJSFile derives the dotted-module forms of a JavaScript /
// TypeScript source file (issue #421). Unlike Python's package-dotted
// form or Java's package declaration, JS/TS has no language-level
// package concept — modules are FILE-RELATIVE. The dotted form mirrors
// the canonical path emitted by the JS extractor's
// dottedModuleFromPath: strip a recognised JS/TS extension and replace
// forward slashes with dots.
//
// `src/services/user.service.ts` → "src.services.user.service"
//
// Source-root strip: the canonical TypeScript layouts use a leading
// `src/` (Nest, Angular, generic), `app/` (Next.js app-router), or
// `lib/` (npm packages). Strip ONE leading segment to keep parity with
// the Python/PHP single-strip policy. The pre-strip form is preserved
// in the returned slice so a corpus indexed under the repo root still
// resolves an import whose source_module was emitted from a sibling
// file using the post-strip form.
//
// Files at the repo root with no parent directory return only their
// dotted leaf — e.g. `index.ts` → ["index"]. The resolver's nil-guards
// treat empty modules as no-ops.
func modulesForJSFile(p string) []string {
	if p == "" {
		return nil
	}
	// Strip the canonical JS/TS extension once.
	stripped := p
	for _, ext := range jsExtensions {
		if strings.HasSuffix(stripped, ext) {
			stripped = strings.TrimSuffix(stripped, ext)
			break
		}
	}
	dotted := strings.ReplaceAll(stripped, "/", ".")
	out := []string{dotted}
	for _, prefix := range sourceRootPrefixes {
		if strings.HasPrefix(out[0], prefix) {
			out = append(out, strings.TrimPrefix(out[0], prefix))
			break
		}
	}
	return out
}

// jsExtensions enumerates the canonical JavaScript/TypeScript source
// extensions modulesForJSFile recognises. Order matches the resolver's
// extractor-side resolveRelativeImport so module derivation and import
// resolution agree on which extension to strip.
var jsExtensions = []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"}

// modulesForPythonFile is the original Python-specific dotted-module
// derivation (issue #93). Extracted from modulesForFile so the
// language dispatch reads cleanly.
func modulesForPythonFile(p string) []string {
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

// modulesForJavaFile derives the dotted package path of a Java source
// file (issue #120). Java files map their PARENT directory to the
// Java package: `src/main/java/com/foo/Bar.java` belongs to package
// "com.foo" and the entity name is "Bar". The leading source-root
// (`src/main/java/`, `src/test/java/`, plus the same well-known
// repo-relative prefixes Python uses) is stripped so the dotted form
// matches the literal `import com.foo.Bar;` path the resolver is
// asked to bind.
//
// Returned slice always has at least one entry (the package path) and
// optionally a second when a non-canonical leading prefix is present.
// Files at the repo root with no parent directory return a single
// empty string; the caller's nil-guards handle that gracefully.
func modulesForJavaFile(p string) []string {
	stripped := strings.TrimSuffix(p, ".java")
	dir := stripped
	if slash := strings.LastIndexByte(stripped, '/'); slash >= 0 {
		dir = stripped[:slash]
	} else {
		// File at repo root — no package. Caller treats empty
		// returns as a no-op.
		return nil
	}
	dotted := strings.ReplaceAll(dir, "/", ".")
	out := []string{dotted}
	// Strip well-known Java source-root prefixes once. Keep the
	// pre-strip form too so an in-corpus class indexed under its
	// repo-relative dotted form continues to resolve. The strip is
	// conservative — only the canonical Maven/Gradle layouts, plus
	// the same generic prefixes used by Python.
	for _, prefix := range javaSourceRootPrefixes {
		if strings.HasPrefix(dotted, prefix) {
			out = append(out, strings.TrimPrefix(dotted, prefix))
			break
		}
	}
	for _, prefix := range sourceRootPrefixes {
		if strings.HasPrefix(out[0], prefix) {
			out = append(out, strings.TrimPrefix(out[0], prefix))
			break
		}
	}
	return out
}

// modulesForPHPFile derives the dotted-namespace forms of a PHP source
// file (issue #113). PHP uses PSR-4 to map a top-level namespace to a
// source root directory; Symfony's `composer.json` ships the canonical
// `App\` → `src/` map, and Laravel ships `App\` → `app/`. Every
// project-internal class lives in a file whose path-after-the-source-root
// equals its sub-namespace (e.g. `src/Entity/Post.php` ↔
// `App\Entity\Post`).
//
// The returned slice always contains the dotted form derived from the
// raw path (slash → dot, `.php` stripped, parent directory only).
// When the path begins with one of the well-known PSR-4 source roots
// the function additionally emits the canonical `App.` form so an
// IMPORTS edge whose ToID was emitted as `App\Entity\Post`
// (source_module = `App.Entity`) binds against the file's
// `src/Entity/Post.php` location regardless of whether the corpus was
// indexed under the PSR-4 root or as a generic repo.
//
// Files at the repo root (no parent directory) return nil — the caller's
// nil-guards treat that as "no module".
func modulesForPHPFile(p string) []string {
	stripped := strings.TrimSuffix(p, ".php")
	dir := stripped
	if slash := strings.LastIndexByte(stripped, '/'); slash >= 0 {
		dir = stripped[:slash]
	} else {
		// File at repo root — no namespace.
		return nil
	}
	dotted := strings.ReplaceAll(dir, "/", ".")
	out := []string{dotted}
	// PSR-4: src/Foo/Bar.php → App\Foo\Bar (Symfony default);
	//        app/Foo/Bar.php → App\Foo\Bar (Laravel default).
	// Strip the leading source root once and re-prefix with the
	// canonical "App" segment so an import whose source_module is
	// "App.Foo" binds regardless of whether the corpus was rooted at
	// the package root or the repo root.
	for _, prefix := range phpPSR4SourceRootPrefixes {
		if strings.HasPrefix(dotted, prefix) {
			tail := strings.TrimPrefix(dotted, prefix)
			if tail == "" {
				out = append(out, "App")
			} else {
				out = append(out, "App."+tail)
			}
			break
		}
	}
	// Also try the generic source-root strip (src./lib./app.) so a
	// non-PSR-4 layout still resolves under its repo-relative dotted
	// form. Same conservative single-strip policy as Python/Java.
	for _, prefix := range sourceRootPrefixes {
		if strings.HasPrefix(out[0], prefix) {
			out = append(out, strings.TrimPrefix(out[0], prefix))
			break
		}
	}
	return out
}

// phpPSR4SourceRootPrefixes lists the canonical PSR-4 source-root
// directories that map to the conventional `App\` top-level namespace.
// Matched against the dotted form of the parent directory (slashes
// already replaced with dots), so entries end with a dot. "src." covers
// Symfony's composer.json default; "app." covers Laravel's.
var phpPSR4SourceRootPrefixes = []string{
	"src.",
	"app.",
}

// javaSourceRootPrefixes lists the canonical Maven/Gradle layout
// prefixes modulesForJavaFile may strip once when deriving the
// dotted-package form of a `.java` source file. The prefixes are
// matched against the dotted form of the path's parent directory
// (slashes already replaced with dots), so the entries themselves end
// in a dot.
var javaSourceRootPrefixes = []string{
	"src.main.java.",
	"src.test.java.",
	"src.main.kotlin.", // Kotlin-in-Java repo blends; harmless when absent
	"src.test.kotlin.",
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
	// ImportsConsidered counts every embedded IMPORTS edge whose ToID
	// was a non-empty, non-hex dotted module path (issue #142).
	ImportsConsidered int
	// ImportsRewritten counts the subset of ImportsConsidered that
	// resolved to a 16-char entity ID via the per-module reverse index.
	ImportsRewritten int
	// PHPFQNMethodConsidered counts every embedded CALLS edge whose
	// ToID matched the PHP FQN-method shape `<namespace>::<method>`
	// (issue #422 — Symfony YAML routes emit these via _controller).
	PHPFQNMethodConsidered int
	// PHPFQNMethodRewritten counts the subset of PHPFQNMethodConsidered
	// that resolved to a 16-char entity ID via the class-file method
	// lookup.
	PHPFQNMethodRewritten int
}

// ResolveDottedImportTarget looks up a project-internal IMPORTS ToID of
// the form "<module>.<leaf>" against the per-module reverse index built
// in BuildImportTable. The dotted path is split on the LAST dot; the
// left segment is the module path, the right segment is the leaf
// symbol. Returns (id, true) when (module, leaf) maps to exactly one
// entity; ("", false) otherwise (external package, plain module import
// with no leaf, ambiguous tuple, or unknown module).
//
// Issue #142 — flask-realworld emits IMPORTS edges with ToIDs like
// "conduit.database.db". Pre-fix these flowed through the Index
// resolver as bare-name stubs, missed every (kind, name, qualified)
// index, and landed on bug-resolver. The dotted-path → entity mapping
// is the same data BuildImportTable already builds for CALLS rewrite;
// this function simply exposes it for the IMPORTS-edge code path.
func (t ImportTable) ResolveDottedImportTarget(dotted string) (string, bool) {
	if dotted == "" {
		return "", false
	}
	dot := strings.LastIndexByte(dotted, '.')
	if dot <= 0 || dot == len(dotted)-1 {
		// No leaf separator — this is a plain module import like
		// `import conduit.database`. There is no per-symbol entity to
		// bind to; leave the edge alone.
		return "", false
	}
	module, leaf := dotted[:dot], dotted[dot+1:]
	return t.lookupModuleEntity(module, leaf)
}

// isPHPFQNMethodShape reports whether s looks like a PHP FQN-method
// reference (`App\Controller\BlogController::list` or its dotted
// equivalent). The shape is: a `::` infix with non-empty halves, where
// the left half contains either a backslash or a dot (i.e. a real
// namespace, not a single identifier — `Foo::bar` is the receiver-typed
// form already handled by the base resolver via byMember).
func isPHPFQNMethodShape(s string) bool {
	sep := strings.Index(s, "::")
	if sep <= 0 || sep+2 >= len(s) {
		return false
	}
	left := s[:sep]
	right := s[sep+2:]
	if right == "" {
		return false
	}
	// Reject CONTAINS-style file references (`doc.md::heading`) — the
	// left half there is a path with a slash. Issue #100 owns those.
	if strings.ContainsRune(left, '/') {
		return false
	}
	return strings.ContainsAny(left, "\\.")
}

// ResolvePHPFQNMethodTarget binds a PHP FQN-method ToID of the form
// `App\Controller\BlogController::list` (or its dotted equivalent
// `App.Controller.BlogController::list`) to the entity ID of the
// method declared in the resolved class's source file.
//
// Issue #422 — Symfony YAML routes carry `_controller: <FQN>::<method>`
// strings that the YAML cross-extractor stamps verbatim onto CALLS /
// IMPORTS edges. Pre-fix these flowed through the base resolver as
// opaque names, missed every (kind, name, qualified) index, and landed
// on bug-resolver. The resolution is two-step:
//
//  1. Split the input on `::`. The left side is a class FQN; normalise
//     backslashes to dots, then split on the LAST dot to obtain
//     (module, class). Pass to lookupModuleEntity to find the class
//     entity ID.
//  2. From the class ID, recover the class entity's SourceFile (via the
//     methodsByFileName index built at BuildImportTable time we already
//     have a file → method-name → method-id map; we use the class file
//     directly via the same modulesByName mapping).
//
// Returns ("", false) for: malformed shapes (no `::`, empty halves),
// classes that resolve into external namespaces (Symfony, Doctrine, …),
// ambiguous (file, method) tuples, and methods not present in the
// class's file.
func (t ImportTable) ResolvePHPFQNMethodTarget(toID string) (string, bool) {
	if toID == "" {
		return "", false
	}
	sep := strings.Index(toID, "::")
	if sep <= 0 || sep+2 >= len(toID) {
		return "", false
	}
	classFQN := toID[:sep]
	method := toID[sep+2:]
	if method == "" {
		return "", false
	}
	// Normalise backslash to dot (PHP namespace separator). Either form
	// flows through the same dotted lookup.
	if strings.ContainsRune(classFQN, '\\') {
		classFQN = strings.ReplaceAll(classFQN, "\\", ".")
	}
	dot := strings.LastIndexByte(classFQN, '.')
	if dot <= 0 || dot == len(classFQN)-1 {
		return "", false
	}
	module, className := classFQN[:dot], classFQN[dot+1:]
	// Resolve the class entity. lookupModuleEntity returns the unique
	// entity ID for (module, name); ambiguous or unknown → ("", false).
	classID, ok := t.lookupModuleEntity(module, className)
	if !ok {
		return "", false
	}
	// Recover the class's SourceFile via modulesByName: the file that
	// satisfies `module` and contains the class. PHP PSR-4 maps a
	// namespace to exactly one file, so the (module, file) intersection
	// converges. We pick the file whose method index contains `method`.
	files := t.modulesByName[module]
	if len(files) == 0 {
		return "", false
	}
	_ = classID // class ID is not needed for the method lookup; presence
	// in the per-module reverse index is the project-internal gate. The
	// method is bound by (class_file, method_name). When two project
	// files declare the same module (rare but possible — duplicate
	// namespace fragments split across roots), we accept the first
	// unambiguous (file, method) hit.
	var (
		hitID string
		hits  int
	)
	for file := range files {
		if t.ambigMethodFileName[file] != nil && t.ambigMethodFileName[file][method] {
			continue
		}
		bucket := t.methodsByFileName[file]
		if bucket == nil {
			continue
		}
		id, ok := bucket[method]
		if !ok {
			continue
		}
		if hits == 0 {
			hitID = id
			hits = 1
		} else if id != hitID {
			hits++
		}
	}
	if hits != 1 {
		return "", false
	}
	return hitID, true
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
			to := rel.ToID
			if to == "" || isHexID(to) {
				continue
			}
			switch rel.Kind {
			case "CALLS":
				// Issue #422 — PHP FQN-method shape `<NS>::<method>`
				// (Symfony YAML _controller). Detect before the generic
				// `ContainsAny(":.#")` skip below, which would otherwise
				// drop these on the floor. The shape is unambiguous: a
				// `::` infix with non-empty halves and either backslash
				// or dot separators in the namespace.
				if strings.Contains(to, "::") {
					if isPHPFQNMethodShape(to) {
						stats.PHPFQNMethodConsidered++
						if id, ok := tbl.ResolvePHPFQNMethodTarget(to); ok {
							rel.ToID = id
							stats.PHPFQNMethodRewritten++
						}
					}
					continue
				}
				// Skip stubs that already encode a kind ("Kind:Name") or
				// a receiver-typed dotted target ("Class.method"). The
				// base resolver handles those via byKind / byMember.
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
			case importRelKind:
				// IMPORTS ToID is a dotted module path like
				// "conduit.database.db" (issue #142) or — for PHP
				// (issue #113) — a backslash-separated FQN like
				// `App\Entity\Post`. Both shapes are normalized to
				// dotted form here and then looked up against the
				// per-module reverse index. External packages
				// ("marshmallow.Schema", "Symfony\\...") miss the
				// lookup and are left for the external-synthesis
				// pass.
				normalized := to
				if strings.ContainsRune(normalized, '\\') {
					normalized = strings.ReplaceAll(normalized, "\\", ".")
				}
				if !strings.ContainsRune(normalized, '.') {
					continue
				}
				if strings.ContainsAny(normalized, ":#") {
					continue
				}
				stats.ImportsConsidered++
				id, ok := tbl.ResolveDottedImportTarget(normalized)
				if !ok {
					continue
				}
				rel.ToID = id
				stats.ImportsRewritten++
			}
		}
	}
	return stats
}
