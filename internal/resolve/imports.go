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
	"path"
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
	// ResolvedToID is the ToID of the IMPORTS relationship that created
	// this binding, as it stood when BuildImportTable read it. For Python
	// (after extractor-side resolveImportToIDs) this is `ext:<root>[:<name>]`
	// when the source module's root is a known external package; otherwise
	// it remains the raw dotted module path emitted by the extractor.
	// Used by the cross-file REFERENCES resolver (chain-fix: python-
	// references-cross-file): when a same-file structural REFERENCES stub
	// can't bind (because the target lives in another file), the resolver
	// looks the local name up in this table and, if the binding has an
	// `ext:` ResolvedToID, rewrites the REFERENCES edge to that ext: ID;
	// otherwise it falls back to `lookupModuleEntity(SourceModule,
	// ImportedName)` for in-project cross-file resolution.
	ResolvedToID string
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
	// entityFile / entityKind track the SourceFile and Kind for every
	// entity recorded into entitiesByModuleName. Used by the same-file
	// collision dedup in BuildImportTable so two projections of the same
	// physical class (e.g. PHP SCOPE.Component + framework Model) don't
	// flip the (module, name) tuple into ambigModuleName and tank PHP
	// IMPORTS resolution (issue #485 PHP wave-3 follow-up).
	entityFile map[string]string
	entityKind map[string]string
	// candidatesByModuleName[module_path][name] = all entity IDs that were
	// seen for this (module, name) pair, including the ones that caused
	// the ambiguity. Populated alongside ambigModuleName so that
	// lookupModuleEntityJavaCanonical can apply a Java-specific tie-break
	// (prefer the entity whose SourceFile basename matches the class name)
	// without requiring a full entity scan. Issue #778.
	candidatesByModuleName map[string]map[string][]string
	// methodsByFileName[source_file][method_name] = entity_id, populated
	// only for PHP method entities and only when the (file, name) tuple
	// resolves to exactly one entity. Ambiguous tuples are tracked in
	// ambigMethodFileName. Used by ResolvePHPFQNMethodTarget (issue #422)
	// to bind an FQN-method like `App\Controller\BlogController::list` to
	// the method declared in the class's source file.
	methodsByFileName   map[string]map[string]string
	ambigMethodFileName map[string]map[string]bool
	// docByFilePath maps a normalised file path → the entity ID that
	// "represents" that file for cross-file binding purposes. Used by the
	// markdown extractor's IMPORTS path (issue #44 follow-up): a link
	// `[x](./plugins/foo/README.md)` emits ToID="plugins/foo/README.md",
	// and a link `[x](applicationset/list.yaml)` emits ToID="applicationset/list.yaml".
	// Neither shape carries a dot-separator that ResolveDottedImportTarget
	// can split on, so previously these landed in bug-extractor despite
	// the target file having entities in the graph.
	//
	// Preference rules (first match wins, evaluated at insert time):
	//   1. A SCOPE.Document entity for the file (markdown's primary entity)
	//   2. The lexicographically lowest entity ID for the file (stable
	//      tie-break — any entity proves the file exists in the graph)
	// Lower-preference candidates never overwrite a higher-preference hit.
	docByFilePath map[string]string
	// docByFilePathRank tracks the preference rank of the entry stored
	// in docByFilePath so a later, higher-priority candidate can win.
	// Lower is better. 0 = unset, 1 = Document, 2 = first.
	docByFilePathRank map[string]int
	// docByDir maps a normalised directory path → the Document entity ID
	// for `<dir>/README.md` (any case). Markdown links to bare directories
	// (`[plugins](./plugins)`) resolve to the directory's README.
	docByDir map[string]string
	// moduleFileEntity maps a dotted Python module path to the entity ID
	// of the SCOPE.Component/file entity that represents it. Populated in
	// BuildImportTable Pass 2 for every Python file-level SCOPE.Component
	// entity (kind=="SCOPE.Component" with a source-file-matching name).
	// Used by ResolvePythonModuleImport to bind IMPORTS edges of the form
	// `to_id = "users.views"` to the concrete file entity (id hex) so
	// `from users import views` resolves to the views.py file entity
	// instead of landing in bug-extractor. Refs #44.
	moduleFileEntity map[string]string
	// localNamesByFile[file_path][name] = true when an entity named `name`
	// is declared in `file_path`. Used by the cross-file REFERENCES
	// resolver to skip rewriting when a same-file entity already shadows
	// the imported name (the structural-ref pass will bind it locally via
	// byLocation). Chain-fix: python-references-cross-file.
	localNamesByFile map[string]map[string]bool
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
		byFile:                 make(map[string]map[string]ImportBinding),
		ambig:                  make(map[string]map[string]bool),
		wildcardModules:        make(map[string][]string),
		modulesByName:          make(map[string]map[string]bool),
		entitiesByModuleName:   make(map[string]map[string]string),
		ambigModuleName:        make(map[string]map[string]bool),
		entityFile:             make(map[string]string),
		entityKind:             make(map[string]string),
		candidatesByModuleName: make(map[string]map[string][]string),
		methodsByFileName:      make(map[string]map[string]string),
		ambigMethodFileName:    make(map[string]map[string]bool),
		docByFilePath:          make(map[string]string),
		docByFilePathRank:      make(map[string]int),
		docByDir:               make(map[string]string),
		localNamesByFile:       make(map[string]map[string]bool),
		moduleFileEntity:       make(map[string]string),
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
				ResolvedToID: strings.TrimSpace(rel.ToID),
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

		// Same-file name index (chain-fix: python-references-cross-file).
		// Tracks whether an entity named e.Name is declared in
		// e.SourceFile so the REFERENCES resolver can skip a cross-file
		// rewrite when a same-file definition would shadow the import.
		if file := normalizePath(e.SourceFile); file != "" && e.Name != "" {
			bucket := tbl.localNamesByFile[file]
			if bucket == nil {
				bucket = make(map[string]bool)
				tbl.localNamesByFile[file] = bucket
			}
			bucket[e.Name] = true
		}

		modules := modulesForFile(normalizePath(e.SourceFile))

		// Refs #44 — Python module-level IMPORTS resolution. When the
		// extractor emits `from users import views` it produces an IMPORTS
		// edge with to_id = "users.views". ResolveDottedImportTarget splits
		// on the last dot (module="users", leaf="views") and looks for an
		// entity named "views" in module "users" — but the file entity is
		// named "users/views.py", not "views", so the lookup misses. We fix
		// this by recording the file-level SCOPE.Component entity (the one
		// whose Name matches its own SourceFile) under each dotted-module
		// form of the file. ResolvePythonModuleImport then tries a direct
		// module-name lookup when the (module, leaf) pair fails. Only Python
		// is gated here; other languages have their own module-import shapes.
		if e.Language == "python" && e.Kind == "SCOPE.Component" &&
			e.ID != "" && e.SourceFile != "" {
			normalFile := normalizePath(e.SourceFile)
			if normalFile != "" && normalizePath(e.Name) == normalFile {
				// This entity IS the file-level SCOPE.Component for this file.
				// Register it under every dotted-module form of the file so
				// "users.views" → entity-id of users/views.py SCOPE.Component.
				for _, mod := range modules {
					if _, exists := tbl.moduleFileEntity[mod]; !exists {
						tbl.moduleFileEntity[mod] = e.ID
					}
				}
			}
		}

		for _, mod := range modules {
			files := tbl.modulesByName[mod]
			if files == nil {
				files = make(map[string]bool)
				tbl.modulesByName[mod] = files
			}
			files[normalizePath(e.SourceFile)] = true

			if tbl.ambigModuleName[mod] != nil && tbl.ambigModuleName[mod][e.Name] {
				// Still record this entity as a candidate so
				// lookupModuleEntityJavaCanonical can apply the
				// canonical-file tie-break even when earlier collisions
				// already set the ambig flag (issue #778). Without this,
				// entities that arrive after two non-canonical ones have
				// already triggered ambig are silently excluded from the
				// candidate list and the canonical SCOPE.Component is
				// never considered.
				incomingFileLate := normalizePath(e.SourceFile)
				if incomingFileLate != "" {
					if tbl.candidatesByModuleName[mod] == nil {
						tbl.candidatesByModuleName[mod] = make(map[string][]string)
					}
					tbl.candidatesByModuleName[mod][e.Name] = append(
						tbl.candidatesByModuleName[mod][e.Name], e.ID)
					tbl.entityFile[e.ID] = incomingFileLate
					tbl.entityKind[e.ID] = e.Kind
				}
				continue
			}
			bucket := tbl.entitiesByModuleName[mod]
			if bucket == nil {
				bucket = make(map[string]string)
				tbl.entitiesByModuleName[mod] = bucket
			}
			if existing, ok := bucket[e.Name]; ok && existing != e.ID {
				// Same-file framework-projection dedup (issue #485 PHP
				// wave-3 follow-up, extended to Scala in #498 follow-up).
				// PHP / Laravel / Symfony index emits duplicate entities
				// for the same physical class file: the PHP extractor
				// produces a SCOPE.Component, while yaml-driven framework
				// synth produces a Model / Controller entity with the
				// same Name and SourceFile. Play / Scala behaves the
				// same way — a class `AsyncController` is emitted as
				// SCOPE.Component by the Scala extractor and as
				// `Controller` by the Play YAML rules.
				//
				// Both describe the same class. PSR-4 / scalac guarantee
				// one class per FQN per file, so when (module, name)
				// collides on a single SourceFile AND the two entities
				// have DIFFERENT kinds AND at least one side is a SCOPE.*
				// canonical entity, we are NOT ambiguous — we just have
				// parallel projections of the same class. Prefer the
				// SCOPE.* projection so IMPORTS targets like
				// `controllers.AsyncController` and
				// `App\Entity\User` bind to their declaring class entity
				// instead of landing in bug-extractor.
				//
				// Guardrails (preserve existing ambiguity semantics):
				//   - require existingFile == incomingFile (same file)
				//   - require existing.Kind != incoming.Kind (true
				//     projection, not a real duplicate)
				//   - require at least one side to be SCOPE.Component or
				//     SCOPE.Operation (otherwise it's two framework
				//     projections, still genuinely ambiguous).
				existingFile := tbl.entityFile[existing]
				existingKind := tbl.entityKind[existing]
				incomingFile := normalizePath(e.SourceFile)
				if existingFile != "" && existingFile == incomingFile &&
					existingKind != e.Kind &&
					(isScopeKind(existingKind) || isScopeKind(e.Kind)) {
					if preferEntityKind(e.Kind, existingKind) {
						bucket[e.Name] = e.ID
						tbl.entityFile[e.ID] = incomingFile
						tbl.entityKind[e.ID] = e.Kind
						// Also replace the existing entity in candidatesByModuleName
						// with the preferred incoming entity so
						// lookupModuleEntityJavaCanonical sees the canonical
						// SCOPE.* entity even when a later Dependency collision
						// re-reads the candidates list (issue #778 follow-up).
						if tbl.candidatesByModuleName[mod] != nil {
							prev := tbl.candidatesByModuleName[mod][e.Name]
							updated := prev[:0:len(prev)] // same backing array, zero length
							for _, cid := range prev {
								if cid == existing {
									updated = append(updated, e.ID)
								} else {
									updated = append(updated, cid)
								}
							}
							tbl.candidatesByModuleName[mod][e.Name] = updated
						}
					}
					continue
				}
				// Record both the evicted entity and the incoming entity as
				// candidates so lookupModuleEntityJavaCanonical can apply a
				// Java file-name tie-break later (issue #778).
				if tbl.candidatesByModuleName[mod] == nil {
					tbl.candidatesByModuleName[mod] = make(map[string][]string)
				}
				candidates := tbl.candidatesByModuleName[mod][e.Name]
				if len(candidates) == 0 {
					candidates = []string{existing}
				}
				candidates = append(candidates, e.ID)
				tbl.candidatesByModuleName[mod][e.Name] = candidates
				// Ensure entityFile is populated for the incoming entity too —
				// it never reaches the first-write path below, so we must
				// record its SourceFile here. Without this, entityFile[e.ID]
				// stays "" and lookupModuleEntityJavaCanonical silently skips
				// the canonical entity when it happens to arrive second.
				// (incomingFile is already computed above for the same-file
				// dedup check.)
				if incomingFile != "" {
					tbl.entityFile[e.ID] = incomingFile
					tbl.entityKind[e.ID] = e.Kind
				}
				delete(bucket, e.Name)
				if tbl.ambigModuleName[mod] == nil {
					tbl.ambigModuleName[mod] = make(map[string]bool)
				}
				tbl.ambigModuleName[mod][e.Name] = true
				continue
			}
			// Also record every first-write as a candidate so subsequent
			// collisions have the evicted ID available (issue #778).
			if tbl.candidatesByModuleName[mod] == nil {
				tbl.candidatesByModuleName[mod] = make(map[string][]string)
			}
			if len(tbl.candidatesByModuleName[mod][e.Name]) == 0 {
				tbl.candidatesByModuleName[mod][e.Name] = []string{e.ID}
			}
			bucket[e.Name] = e.ID
			tbl.entityFile[e.ID] = normalizePath(e.SourceFile)
			tbl.entityKind[e.ID] = e.Kind
		}
	}

	// Pass 3 — file-path → entity index for markdown cross-file IMPORTS
	// (issue #44 follow-up). Markdown emits `[text](path)` IMPORTS edges
	// whose ToID is the resolved file path (slash-separated, with file
	// extension). The dotted-import resolver above can't bind these
	// because the separator semantics differ. We pick the most
	// representative entity per file (preferring SCOPE.Document, which is
	// what the markdown extractor emits for every .md file).
	for k := range records {
		e := &records[k]
		if e.ID == "" {
			continue
		}
		file := normalizePath(e.SourceFile)
		if file == "" {
			continue
		}
		rank := 0
		switch e.Kind {
		case "SCOPE.Document":
			rank = 1
		default:
			rank = 2
		}
		existingRank := tbl.docByFilePathRank[file]
		if existingRank == 0 || rank < existingRank {
			tbl.docByFilePath[file] = e.ID
			tbl.docByFilePathRank[file] = rank
		} else if rank == existingRank {
			// Stable tie-break: keep the lexicographically lower entity
			// ID so the result is deterministic across map-iteration
			// orders. Documents are unique per file in practice, so this
			// only matters for rank=2 ties.
			if e.ID < tbl.docByFilePath[file] {
				tbl.docByFilePath[file] = e.ID
			}
		}
	}
	// Build docByDir from SCOPE.Document entities whose file's basename
	// is README.md (case-insensitive). A link to `./plugins/foo` should
	// bind to `plugins/foo/README.md`'s Document. First README per dir
	// wins; collisions are vanishingly rare (one README per dir is the
	// convention).
	for k := range records {
		e := &records[k]
		if e.ID == "" || e.Kind != "SCOPE.Document" {
			continue
		}
		file := normalizePath(e.SourceFile)
		if file == "" {
			continue
		}
		base := file
		if slash := strings.LastIndexByte(file, '/'); slash >= 0 {
			base = file[slash+1:]
		}
		if !strings.EqualFold(base, "README.md") {
			continue
		}
		dir := ""
		if slash := strings.LastIndexByte(file, '/'); slash >= 0 {
			dir = file[:slash]
		}
		if dir == "" {
			continue
		}
		if _, exists := tbl.docByDir[dir]; !exists {
			tbl.docByDir[dir] = e.ID
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
	case strings.HasSuffix(p, ".scala"):
		return modulesForScalaFile(p)
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
	// PLT #537 — JS/TS directory-import rollup. `components/branding/index.ts`
	// is the conventional "barrel" file for the `components/branding` module:
	// imports of `@/components/branding` resolve to this file, and a named
	// import like `import { BrandLogo } from '@/components/branding'` emits
	// IMPORTS ToID `components.branding.BrandLogo`. Without the rollup, the
	// per-module reverse index only carries the file's `.index` form
	// (`components.branding.index`), the (module, leaf) lookup misses, and
	// every consumer of the barrel lands in bug-extractor. Mirrors the
	// existing Python `__init__.py → parent dir` rollup in
	// modulesForPythonFile.
	if strings.HasSuffix(stripped, "/index") {
		parent := strings.TrimSuffix(stripped, "/index")
		if parent != "" {
			out = append(out, strings.ReplaceAll(parent, "/", "."))
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

// jsExtensions enumerates the canonical JavaScript/TypeScript source
// extensions modulesForJSFile recognises. Order matches the resolver's
// extractor-side resolveRelativeImport so module derivation and import
// resolution agree on which extension to strip.
var jsExtensions = []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"}

// isJSImportSource reports whether the importing file path looks like a
// JS/TS source file. Used to gate ResolveDottedImportTargetForJS so the
// default-export fallback only fires for JS/TS IMPORTS edges (PLT #537).
func isJSImportSource(p string) bool {
	low := strings.ToLower(p)
	for _, ext := range jsExtensions {
		if strings.HasSuffix(low, ext) {
			return true
		}
	}
	return false
}

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
	// #4705a: src-layout / Django source roots nested under a project
	// container. Many Django repos place apps under `src/`, `app/`, or a
	// Django `apps/` package one or more directories below the repo root
	// (e.g. `backend/src/core/models.py`, `server/apps/users/views.py`).
	// The leading-prefix strip above misses these because the source-root
	// marker is interior. Strip ONE recognised source-root segment found
	// anywhere on a segment boundary so `from core.models import X` /
	// `from users.views import V` bind to the internal file. This is the
	// dominant under-linking pattern for the Django oracle. The markers
	// are conventional package roots (`apps.`, `src.`, `app.`, `lib.`),
	// matched with the same single-strip, boundary-anchored discipline as
	// the Java source-root strip to avoid monorepo tail collisions.
	for _, prefix := range pythonInteriorSourceRoots {
		if tail, ok := stripAfterSourceRoot(out[0], prefix); ok && tail != "" {
			out = appendUnique(out, tail)
			break
		}
	}
	return out
}

// pythonInteriorSourceRoots lists conventional Python/Django source-root
// package segments that modulesForPythonFile may strip once when found on
// an interior segment boundary (#4705a). Dotted, trailing-dot form. Kept
// deliberately small to avoid monorepo collisions.
var pythonInteriorSourceRoots = []string{"apps.", "src.", "app."}

// appendUnique appends s to out only when not already present. Keeps the
// dotted-module alias slices free of duplicates when multiple strip rules
// converge on the same tail.
func appendUnique(out []string, s string) []string {
	for _, e := range out {
		if e == s {
			return out
		}
	}
	return append(out, s)
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
	// Strip the canonical Maven/Gradle source root. #4705b: the source
	// root is matched ANYWHERE in the dotted path (anchored on segment
	// boundaries), not only as a leading prefix, so Gradle multi-module
	// layouts — `lib/src/main/java/com/acme/Bar.java`,
	// `app/src/main/java/...` — strip their leading module-container
	// segment and the import `com.acme.Bar` binds to the internal class.
	// The canonical `src.main.java.`-style markers are specific enough
	// that an interior match is safe (a real package would not contain
	// the literal `src.main.java` triple). The pre-strip form is kept too
	// so a corpus indexed at the source root still resolves.
	for _, prefix := range javaSourceRootPrefixes {
		if tail, ok := stripAfterSourceRoot(dotted, prefix); ok {
			out = append(out, tail)
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

// stripAfterSourceRoot returns the dotted-path tail that follows the
// canonical source-root marker `root` (a dotted, trailing-dot form such
// as "src.main.java."), matched as a leading prefix OR anchored at a
// segment boundary anywhere in `dotted`. Returns (tail, true) on a match,
// ("", false) otherwise. Used to honor Gradle/Maven multi-module layouts
// where a module-container segment precedes the source root (#4705b).
func stripAfterSourceRoot(dotted, root string) (string, bool) {
	if strings.HasPrefix(dotted, root) {
		return strings.TrimPrefix(dotted, root), true
	}
	// Interior match must sit on a segment boundary: ".<root>".
	if idx := strings.Index(dotted, "."+root); idx >= 0 {
		return dotted[idx+1+len(root):], true
	}
	return "", false
}

// modulesForScalaFile derives the dotted-package forms of a Scala source
// file. Scala uses the same package-by-parent-directory convention as
// Java, but the canonical project layouts differ:
//
//   - sbt / Mill: `src/main/scala/<pkg>/Foo.scala`
//   - Play Framework: `app/<pkg>/Foo.scala` (no `src/main/scala` prefix)
//   - test sources: `src/test/scala/<pkg>/FooSpec.scala`
//
// The function mirrors modulesForJavaFile: the parent directory's
// slash-to-dot form is the primary module path, and any leading
// well-known source-root is stripped once to produce an alias. The
// generic `app.` / `src.` / `lib.` prefixes from sourceRootPrefixes
// cover Play (and most monorepo layouts) — a Play file
// `app/controllers/AsyncController.scala` resolves to dotted module
// "app.controllers" plus the post-strip alias "controllers", which is
// the literal form the in-repo `import controllers.AsyncController`
// edge carries.
//
// Files at the repo root with no parent directory return nil; the
// caller's nil-guards treat that as "no module".
func modulesForScalaFile(p string) []string {
	stripped := strings.TrimSuffix(p, ".scala")
	dir := stripped
	if slash := strings.LastIndexByte(stripped, '/'); slash >= 0 {
		dir = stripped[:slash]
	} else {
		return nil
	}
	dotted := strings.ReplaceAll(dir, "/", ".")
	out := []string{dotted}
	// Strip canonical sbt/Mill Scala source roots once, mirroring the
	// Java prefix list. The post-strip form is the dotted package path
	// the in-repo `import` statement was written against.
	for _, prefix := range scalaSourceRootPrefixes {
		if strings.HasPrefix(dotted, prefix) {
			out = append(out, strings.TrimPrefix(dotted, prefix))
			break
		}
	}
	// Generic top-level source roots (src./lib./app.) cover Play's
	// `app/` layout and any monorepo layout that nests Scala under one
	// of these conventional roots. Same single-strip policy as Python/
	// Java/PHP to avoid suffix-explosion collisions in monorepos.
	for _, prefix := range sourceRootPrefixes {
		if strings.HasPrefix(out[0], prefix) {
			out = append(out, strings.TrimPrefix(out[0], prefix))
			break
		}
	}
	return out
}

// scalaSourceRootPrefixes lists the canonical sbt / Mill Scala layout
// prefixes modulesForScalaFile may strip once when deriving the
// dotted-package form of a `.scala` source file. Matched against the
// dotted form of the path's parent directory (slashes already replaced
// with dots), so entries end in a dot. Mirrors javaSourceRootPrefixes
// for consistency.
var scalaSourceRootPrefixes = []string{
	"src.main.scala.",
	"src.test.scala.",
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
	//
	// Also handle the bare source-root case (`app/User.php` →
	// dir="app" → dotted="app", which matches no `prefix+"."` form).
	// PSR-4 maps this file to the top-level `App` namespace, so emit
	// "App" without a tail. Issue #485 PHP wave-3 follow-up:
	// pre-fix, `use App\User;` for `app/User.php` failed to bind
	// because module="App" had no entries, causing every top-level
	// App\X import in Laravel-style layouts to land in bug-extractor.
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
		// Bare source root match: dotted is exactly the prefix without
		// the trailing dot (e.g. "app" matches phpPSR4SourceRootPrefixes
		// entry "app."). Emit the canonical "App" top-level namespace.
		if dotted+"." == prefix {
			out = append(out, "App")
			break
		}
	}
	// PSR-4 test-root strip (`tests/Command/X.php` →
	// `App\Tests\Command\X`). Symfony's `autoload-dev` ships
	// `App\Tests\` → `tests/`, so test files satisfy the
	// `App.Tests.<subpath>` dotted module form. Issue #485 PHP wave-3.
	for _, prefix := range phpPSR4TestRootPrefixes {
		if strings.HasPrefix(dotted, prefix) {
			tail := strings.TrimPrefix(dotted, prefix)
			if tail == "" {
				out = append(out, "App.Tests")
			} else {
				out = append(out, "App.Tests."+tail)
			}
			break
		}
		if dotted+"." == prefix {
			out = append(out, "App.Tests")
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

// preferEntityKind reports whether an incoming entity Kind should replace
// an existing same-file (module, name) entry in entitiesByModuleName.
// Returns true when incoming has a strictly more canonical kind than
// existing. Used by the BuildImportTable same-file dedup (issue #485
// PHP wave-3 follow-up) to keep PHP IMPORTS resolvable when the PHP
// extractor's SCOPE.Component collides with a framework Model entity
// produced from the same source file. Lower rank = more canonical.
//
// Ranking:
//   - SCOPE.Component (PHP/JS/Py class entity)              → 0
//   - SCOPE.Operation                                       → 1
//   - everything else (Model, Controller, framework synth)  → 2
//
// The ranking is deliberately conservative: only SCOPE.* kinds get
// preference because IMPORTS edges in the per-language extractors
// always emit the canonical SCOPE.Component as their binding target.
// isScopeKind reports whether k is one of the canonical SCOPE.* entity
// kinds (SCOPE.Component, SCOPE.Operation, SCOPE.Document, SCOPE.Module).
// Used by the BuildImportTable same-file dedup to gate the framework-
// projection preference so non-SCOPE duplicates (two Models, two
// Controllers) still trip the existing ambiguity tracking.
func isScopeKind(k string) bool {
	return strings.HasPrefix(k, "SCOPE.")
}

func preferEntityKind(incoming, existing string) bool {
	rank := func(k string) int {
		switch k {
		case "SCOPE.Component":
			return 0
		case "SCOPE.Operation":
			return 1
		default:
			return 2
		}
	}
	return rank(incoming) < rank(existing)
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

// phpPSR4TestRootPrefixes lists the canonical PSR-4 test-root directories
// that map to the `App\Tests\` sub-namespace under composer's autoload-dev
// section. Symfony's default `composer.json` ships
// `"App\\Tests\\": "tests/"`, so `tests/Command/X.php` satisfies
// `App\Tests\Command\X`. Issue #485 PHP wave-3 — without this, every
// PHPUnit test class import lands in bug-extractor. Matched the same
// way as phpPSR4SourceRootPrefixes (dotted parent dir, trailing dot).
var phpPSR4TestRootPrefixes = []string{
	"tests.",
	"test.",
	"Tests.",
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
			// Issue #778 — Java canonical tie-break for bare CALLS that
			// map to an ambiguous (module, name) tuple. When the generic
			// module lookup fails (two entities share the class name, e.g.
			// the canonical WSException.java entity PLUS a hierarchy-
			// inference entity in EntityNotFoundException.java), try the
			// file-name tie-break: prefer the entity whose SourceFile
			// basename matches the class name ("WSException.java" →
			// "WSException"). Safe to apply unconditionally because
			// lookupModuleEntityJavaCanonical itself checks for the
			// ambiguous flag and the canonical-suffix match — if neither
			// condition holds it returns (false) immediately.
			if id, ok := t.lookupModuleEntityJavaCanonical(b.SourceModule, b.ImportedName); ok {
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

// splitFormatAStructuralRef parses a Format A structural-ref stub
// (`scope:<kind>:<subtype>:<lang>:<file>:<name>`) and returns the
// normalised file path and the bare tail name. Returns ok=false for
// shapes that aren't a 6-segment Format A stub, or whose tail contains
// the Format B member delimiter `#`.
//
// Used by the cross-file REFERENCES resolver in ResolveImports.
func splitFormatAStructuralRef(stub string) (filePath, name string, ok bool) {
	if !strings.HasPrefix(stub, stubPrefixScope) {
		return "", "", false
	}
	parts := strings.SplitN(stub, stubDelim, stubScopeSegments)
	if len(parts) != stubScopeSegments {
		return "", "", false
	}
	filePath = normalizePath(parts[stubScopeFileIndex])
	tail := parts[stubScopeTailIndex]
	if filePath == "" || tail == "" {
		return "", "", false
	}
	// Format B uses `#` in the tail. We only rewrite Format A bare names.
	if strings.IndexByte(tail, stubMemberDelim) >= 0 {
		return "", "", false
	}
	return filePath, tail, true
}

// ResolveCrossFileReferenceTarget looks up the bare name N in the import
// table for callerFile and, if N was introduced into that file by an
// IMPORTS edge, returns the cross-file target the REFERENCES edge should
// point at.
//
// Resolution order (mirrors ResolveBareCallTarget, but additionally
// honours `ext:` ResolvedToID values pre-stamped by the Python
// extractor's resolveImportToIDs pass):
//
//  1. Explicit binding for (file, N) — `from x import N`, `import x as N`,
//     etc. If the binding already carries an `ext:` ResolvedToID (set by
//     the Python extractor when the source module's root is a known
//     external package), return that ext-ID. Otherwise fall back to
//     lookupModuleEntity(source_module, imported_name) for in-project
//     cross-file resolution.
//  2. Module-attribute access for plain `import x` bindings — if exactly
//     one plain-import binding's module contains an entity named N,
//     return that entity ID. Skipped when multiple plain imports yield
//     conflicting hits (conservative — matches CALLS policy).
//  3. Wildcard `from x import *` — best-effort lookup in the wildcard
//     modules for callerFile.
//
// Returns ("", false) when none of the paths resolve. The caller leaves
// the original REFERENCES stub alone (no fabrication).
//
// Chain-fix: python-references-cross-file. The Python extractor's
// emitReferences pass (PR #650 Track A) builds a file-local symbol
// table; imported names landed in that table as same-file structural
// refs because the extractor doesn't know the imported entity's
// declaring file. This resolver bridges that gap.
func (t ImportTable) ResolveCrossFileReferenceTarget(callerFile, name string) (string, bool) {
	if name == "" {
		return "", false
	}
	callerFile = normalizePath(callerFile)
	bucket := t.byFile[callerFile]
	if bucket != nil {
		if b, ok := bucket[name]; ok {
			// `ext:` shapes are stamped on the IMPORTS edge by the
			// Python extractor for known-external roots and are the
			// authoritative external target for this binding.
			if strings.HasPrefix(b.ResolvedToID, "ext:") {
				return b.ResolvedToID, true
			}
			if id, ok := t.lookupModuleEntity(b.SourceModule, b.ImportedName); ok {
				return id, true
			}
		}
	}
	// Module-attribute access via plain `import x`. Mirrors
	// ResolveBareCallTarget's branch 2; collect across plain-import
	// bindings deterministically and require exactly one disagreement-free
	// hit.
	var (
		plainCandidate string
		plainHits      int
	)
	for _, b := range bucket {
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

// ResolveCrossModuleCallTarget resolves a Python attribute-call site of the
// shape `<alias>.<leaf>(...)` where `alias` is a local name introduced by an
// import in callerFile. Issue #1694.
//
// The Python extractor stamps two Properties on every CALLS edge that
// originated from a `<alias>.<leaf>(...)` shape:
//
//	Properties["import_alias"] = "<alias>"
//	Properties["call_leaf"]    = "<leaf>"
//
// The ToID is intentionally left as the bare leaf name so the
// `ContainsAny(":.#")` skip in ResolveImports doesn't drop the edge before
// this function runs. This function looks the alias up in the file's
// import bucket and probes `lookupModuleEntity` with the correct
// (module, name) tuple depending on which import shape introduced the
// alias:
//
//	import x[.y]           → (alias=x, source_module=imported_name=x[.y])
//	                          → lookup (x[.y], leaf)
//	from x import y        → (alias=y, source_module=x, imported_name=y)
//	                          → lookup (x.y, leaf)   — y is a submodule
//	                          → lookup (x, leaf)     — y is a class/struct
//	                            (rare: same-class chained call where the
//	                            class lives in x and exposes a classmethod
//	                            named `leaf`; the resolver's byMember path
//	                            would otherwise miss because the receiver
//	                            type is unknown at extraction time)
//
// The two-step probe for the `from x import y` shape is conservative — the
// submodule lookup is tried first and only falls through to the
// (x, leaf) shape when the submodule probe finds no match. This avoids
// binding a true submodule call to a same-module same-named symbol.
//
// Returns ("", false) when:
//   - leaf is empty
//   - the alias is not in the file's import bucket
//   - neither (module, leaf) tuple resolves unambiguously
//
// In those cases the caller leaves the original bare-name ToID in place
// and the downstream bare-name resolver gets a turn.
func (t ImportTable) ResolveCrossModuleCallTarget(callerFile, alias, leaf string) (string, bool) {
	if alias == "" || leaf == "" {
		return "", false
	}
	callerFile = normalizePath(callerFile)
	bucket := t.byFile[callerFile]
	if bucket == nil {
		return "", false
	}
	b, ok := bucket[alias]
	if !ok {
		return "", false
	}
	if b.SourceModule == "" {
		return "", false
	}
	// `import x` shape — alias IS the module name.
	if b.ImportedName == b.SourceModule {
		if id, ok := t.lookupModuleEntity(b.SourceModule, leaf); ok {
			return id, true
		}
		return "", false
	}
	// `from x import y` shape — the alias may be a submodule of x or a
	// symbol exposed by x. Try submodule first.
	if b.ImportedName != "" {
		submod := b.SourceModule + "." + b.ImportedName
		if id, ok := t.lookupModuleEntity(submod, leaf); ok {
			return id, true
		}
	}
	// Same-class fallback: the alias is a class imported from module x,
	// and `<alias>.<leaf>` is a classmethod call.
	if id, ok := t.lookupModuleEntity(b.SourceModule, leaf); ok {
		// Sanity guard — only accept this fallback when the class itself
		// also lives in (source_module, imported_name). Otherwise we could
		// bind to an unrelated function named `leaf` in module x.
		if classID, classOk := t.lookupModuleEntity(b.SourceModule, b.ImportedName); classOk && classID != "" {
			_ = classID // we only need to verify the class is in the module
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

// lookupModuleEntityJavaCanonical is the Java-specific tie-breaker for the
// case where (module, name) is ambiguous in entitiesByModuleName (issue #778).
//
// In Java, every top-level class is declared in a file whose basename
// (minus the `.java` extension) matches the class name: `WSException` lives
// in `WSException.java`. When the hierarchy extractor emits a reference to
// `WSException` inside a peer file (`EntityNotFoundException.java`) as a
// hierarchy-inference entity, both the canonical definition in `WSException.java`
// AND the inference entity in `EntityNotFoundException.java` land in the same
// module bucket, flipping the tuple ambiguous.
//
// The tie-break: among all candidate entity IDs stored for (module, name) in
// candidatesByModuleName, prefer the one whose SourceFile path ends with
// "/<name>.java". If exactly one candidate matches that criterion, return it.
// If zero or more than one match (corner case: two files named identically),
// fall through to ("", false) — safety first, no false bindings.
//
// This function is ONLY called when lookupModuleEntity fails due to ambiguity
// AND the caller has confirmed the edge language is Java.
func (t ImportTable) lookupModuleEntityJavaCanonical(module, name string) (string, bool) {
	if module == "" || name == "" {
		return "", false
	}
	// Only applies to the ambiguous case.
	if t.ambigModuleName[module] == nil || !t.ambigModuleName[module][name] {
		// Not ambiguous — caller should use lookupModuleEntity.
		return "", false
	}
	candidates := t.candidatesByModuleName[module][name]
	if len(candidates) == 0 {
		return "", false
	}
	// The canonical Java file for class <name> is "<name>.java".
	canonicalSuffix := "/" + name + ".java"
	// Collect all candidate IDs whose SourceFile ends with the canonical suffix.
	var canonical []string
	for _, id := range candidates {
		file := t.entityFile[id]
		if file == "" {
			continue
		}
		if !strings.HasSuffix(file, canonicalSuffix) {
			continue
		}
		canonical = append(canonical, id)
	}
	switch len(canonical) {
	case 0:
		return "", false
	case 1:
		return canonical[0], true
	default:
		// Multiple entities from the canonical file — this happens when a
		// Java class has both a SCOPE.Component (emitted by the extractor)
		// and a framework projection like `Service` / `Repository` (emitted
		// by the YAML-driven framework synth) with the same Name and file.
		// In that case apply the same same-file projection preference used
		// by BuildImportTable's Pass 2: prefer the SCOPE.* entity because
		// it is the canonical structural entity for the class. This mirrors
		// the `preferEntityKind` tie-break for PHP/Scala projections (issue
		// #485 / #498 follow-ups).
		// If exactly one SCOPE.* entity survives, return it. If zero or
		// two+ survive, leave ambiguous (safety first).
		var scopeMatch string
		for _, id := range canonical {
			if isScopeKind(t.entityKind[id]) {
				if scopeMatch != "" && scopeMatch != id {
					return "", false // two SCOPE.* entities — still ambiguous
				}
				scopeMatch = id
			}
		}
		if scopeMatch != "" {
			return scopeMatch, true
		}
		return "", false
	}
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
	// MarkdownFilePathConsidered counts every embedded IMPORTS edge
	// whose ToID matched the markdown cross-file file-path shape (slash
	// in the path or a known file extension; no `::`, no `\`, not a
	// dotted module). Issue #44 follow-up.
	MarkdownFilePathConsidered int
	// MarkdownFilePathRewritten counts the subset of
	// MarkdownFilePathConsidered that resolved to a 16-char entity ID
	// via the docByFilePath / docByDir indices.
	MarkdownFilePathRewritten int
	// ReferencesConsidered counts every embedded REFERENCES edge whose
	// ToID was an unresolved same-file structural ref
	// (`scope:<kind>:<subtype>:<lang>:<file>:<name>`) and whose tail
	// name matched a local import in the source file (i.e. a candidate
	// for cross-file rewrite). Chain-fix: python-references-cross-file.
	ReferencesConsidered int
	// ReferencesRewritten counts the subset of ReferencesConsidered
	// that resolved either to a 16-char in-project entity ID (via
	// lookupModuleEntity over the binding's (source_module,
	// imported_name)) or to an `ext:<root>[:<name>]` placeholder (when
	// the IMPORTS edge that introduced the local name already carried
	// an `ext:` ResolvedToID from the Python extractor's
	// resolveImportToIDs pass).
	ReferencesRewritten int
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

// ResolvePythonModuleImport resolves an IMPORTS edge whose ToID is a
// plain Python dotted module path (e.g. "users.views") referring to a
// project-internal module — not a symbol within it. This arises from
// `from users import views` or `import users.views` statements where the
// imported name IS the module itself, not a class or function.
//
// ResolveDottedImportTarget splits on the last dot and looks for a symbol
// named "views" in module "users" — which misses because the file entity
// is named "users/views.py", not "views". This function instead probes
// the moduleFileEntity index built from SCOPE.Component/file entities:
// "users.views" → the hex ID of the users/views.py file-level entity.
//
// Returns ("", false) when the dotted path is not a known in-project
// Python module. Callers MUST gate on language=="python" so other
// languages' IMPORTS edges (which may use dotted paths for different
// reasons) are not shadowed. Refs #44.
func (t ImportTable) ResolvePythonModuleImport(dotted string) (string, bool) {
	if dotted == "" || t.moduleFileEntity == nil {
		return "", false
	}
	id, ok := t.moduleFileEntity[dotted]
	if !ok || id == "" {
		return "", false
	}
	return id, true
}

// ResolveDottedImportTargetForJS performs the same (module, leaf) lookup
// as ResolveDottedImportTarget, plus a JS/TS-specific default-export
// fallback (PLT #537). React / React Native source files commonly emit a
// single `export default function Foo` per file, so a consumer's
// `import Foo from '@/components/.../Foo'` lands an IMPORTS edge with
// ToID `<module>.default`. The base (module, leaf) lookup misses because
// the file's primary entity is named `Foo`, not `default`. When leaf is
// "default", retry with the module's last segment (the file's basename,
// which by convention matches the default export name in PascalCase
// component files). The retry is conservative — we only bind when the
// (module, basename) tuple resolves unambiguously through the existing
// per-module reverse index, so an off-convention default export still
// stays unresolved rather than picking an arbitrary entity.
//
// Also handles barrel re-exports through `index.ts`. The directory-roll
// in modulesForJSFile registers `components/branding/index.ts` entities
// under module `components.branding`, so `components.branding.BrandLogo`
// resolves directly when the consumer uses a directory import. The leaf
// fallback below targets the explicit-file-import case where the entity
// is the default export of a single-component file.
//
// Caller MUST gate on lang ∈ {javascript, typescript}.
func (t ImportTable) ResolveDottedImportTargetForJS(dotted string) (string, bool) {
	if id, ok := t.ResolveDottedImportTarget(dotted); ok {
		return id, true
	}
	// default-leaf fallback: try (module, last-segment(module)).
	dot := strings.LastIndexByte(dotted, '.')
	if dot <= 0 || dot == len(dotted)-1 {
		return "", false
	}
	leaf := dotted[dot+1:]
	if leaf != "default" {
		return "", false
	}
	module := dotted[:dot]
	innerDot := strings.LastIndexByte(module, '.')
	var basename string
	if innerDot < 0 {
		basename = module
	} else {
		basename = module[innerDot+1:]
	}
	if basename == "" {
		return "", false
	}
	if id, ok := t.lookupModuleEntityCaseFold(module, basename); ok {
		return id, true
	}
	// `<dir>/index.ts` barrel case: try the parent directory's last
	// segment instead. `src.features.new-note.index.default` →
	// (module=src.features.new-note, leaf=new-note). The directory
	// rollup in modulesForJSFile makes index-file entities visible
	// under both module forms, so this lookup binds whichever side
	// owns the default export.
	if basename == "index" {
		if innerDot <= 0 {
			return "", false
		}
		parentModule := module[:innerDot]
		parentDot := strings.LastIndexByte(parentModule, '.')
		var parentBase string
		if parentDot < 0 {
			parentBase = parentModule
		} else {
			parentBase = parentModule[parentDot+1:]
		}
		if parentBase == "" {
			return "", false
		}
		return t.lookupModuleEntityCaseFold(parentModule, parentBase)
	}
	return t.lookupModuleEntityCaseFold(module, basename)
}

// lookupModuleEntityCaseFold tries exact (module, name); if it misses,
// tries a PascalCased variant of name (kebab-case/snake_case → Pascal);
// if that misses, walks the module's bucket once looking for an entry
// whose name case-folds (and dash/underscore-strips) to the same key.
// Used by ResolveDottedImportTargetForJS to bridge the
// `file-basename ≠ default-export-name` gap for React-component files
// where the file is kebab-cased (`themed-text.tsx`) and the default
// export is PascalCased (`ThemedText`).
//
// Returns (id, true) only on a unique single match. If the case-folded
// scan finds two or more entries that normalise to the same key, leave
// it ambiguous — we never guess between collisions.
func (t ImportTable) lookupModuleEntityCaseFold(module, name string) (string, bool) {
	if id, ok := t.lookupModuleEntity(module, name); ok {
		return id, true
	}
	if pascal := toPascalCase(name); pascal != "" && pascal != name {
		if id, ok := t.lookupModuleEntity(module, pascal); ok {
			return id, true
		}
	}
	bucket, ok := t.entitiesByModuleName[module]
	if !ok || len(bucket) == 0 {
		return "", false
	}
	key := normaliseIdent(name)
	if key != "" {
		var match string
		for ename, eid := range bucket {
			if normaliseIdent(ename) != key {
				continue
			}
			if match != "" && match != eid {
				return "", false
			}
			match = eid
		}
		if match != "" {
			return match, true
		}
	}
	// Last-resort default-export bind: a single PascalCase entity in the
	// module (excluding obviously-non-component entries like lowercase
	// locals or `default`/`index`) most likely IS the default export.
	// This catches the `feature/index.tsx` shape where the default
	// export name doesn't track the directory basename (e.g.
	// `new-note/index.tsx` exporting `NewNoteFeature`). Conservative
	// gating: only fires when the module bucket has exactly one
	// PascalCase candidate, so multi-component barrels still miss
	// rather than guess.
	// Prefer SCOPE.Operation entries (the actual function/component). A
	// React-component module typically also exports a Props type/schema
	// with a related PascalCase name (`NewNoteFeature` + `NewNoteProps`);
	// the default export is the Operation, not the Schema. When exactly
	// one Operation lives in the module, bind to it.
	var (
		opMatch string
		opCount int
	)
	for ename, eid := range bucket {
		if ename == "" || ename == "default" || ename == "index" {
			continue
		}
		r0 := ename[0]
		if !(r0 >= 'A' && r0 <= 'Z') {
			continue
		}
		kind := t.entityKind[eid]
		if kind != "SCOPE.Operation" {
			continue
		}
		if opCount == 0 {
			opMatch = eid
			opCount = 1
		} else if eid != opMatch {
			opCount++
		}
	}
	if opCount == 1 {
		return opMatch, true
	}
	return "", false
}

// toPascalCase converts kebab-case / snake_case to PascalCase.
// `themed-text` → `ThemedText`; `new_note` → `NewNote`. ASCII only;
// unrecognised characters pass through unchanged.
func toPascalCase(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	upNext := true
	for _, r := range s {
		if r == '-' || r == '_' {
			upNext = true
			continue
		}
		if upNext && r >= 'a' && r <= 'z' {
			b.WriteRune(r - 32)
		} else {
			b.WriteRune(r)
		}
		upNext = false
	}
	return b.String()
}

// normaliseIdent lowercases s and strips '-' and '_'. Used as the
// case-fold key in lookupModuleEntityCaseFold so `themed-text`,
// `ThemedText`, `themedText`, and `themed_text` all collide on the
// same bucket key.
func normaliseIdent(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '-' || r == '_' {
			continue
		}
		if r >= 'A' && r <= 'Z' {
			b.WriteRune(r + 32)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ResolveDottedImportTargetForPHP performs the same (module, leaf)
// lookup as ResolveDottedImportTarget, plus a PHP-specific namespace-
// prefix fallback for IMPORTS edges whose ToID names a sub-namespace
// (directory) rather than a specific class. PHP projects routinely
// `use App\Http\Middleware;` or `use App\Console;` to bring a
// sub-namespace into scope without naming a particular symbol. The
// standard (module, leaf) split misses — `App.Http` has no entity
// named `Middleware` — but the FULL dotted form does name a real
// project directory with indexed entities. Bind to a representative
// entity from that namespace so the edge classifies as resolved rather
// than bug-extractor.
//
// Caller MUST gate this on lang == "php"; other languages (Python's
// `import x.y` shape covered by TestResolveImports_DottedImportPlainModule)
// rely on the strict ResolveDottedImportTarget semantics where plain
// module imports without a leaf binding stay unresolved. Issue #485
// PHP wave-3.
func (t ImportTable) ResolveDottedImportTargetForPHP(dotted string) (string, bool) {
	if id, ok := t.ResolveDottedImportTarget(dotted); ok {
		return id, true
	}
	return t.resolveNamespaceTarget(dotted)
}

// resolveNamespaceTarget binds a dotted IMPORTS target that names a
// project-internal namespace (directory) to a stable representative
// entity from that namespace. Used as a fallback when the standard
// (module, leaf) lookup misses — typical of PHP `use App\Sub\Namespace;`
// statements that import a sub-namespace prefix rather than a specific
// class. The representative entity ID is deterministic: the
// lexicographically lowest entity ID in entitiesByModuleName[dotted],
// preferring SCOPE.Component over other kinds via the same ordering
// applied at insert time (preferEntityKind). Returns ("", false) when
// no entity in that namespace is indexed.
func (t ImportTable) resolveNamespaceTarget(dotted string) (string, bool) {
	bucket, ok := t.entitiesByModuleName[dotted]
	if !ok || len(bucket) == 0 {
		return "", false
	}
	var picked string
	for _, id := range bucket {
		if picked == "" || id < picked {
			picked = id
		}
	}
	if picked == "" {
		return "", false
	}
	return picked, true
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

// ResolveMarkdownFilePathTarget binds a markdown cross-file IMPORTS
// ToID to an entity ID. Two shapes are accepted:
//
//  1. Exact file-path match — ToID is a slash-separated path that
//     equals the SourceFile of one or more indexed entities. Returns
//     the file's preferred representative entity (SCOPE.Document if
//     present, otherwise a stable pick).
//  2. Directory match — ToID has no file extension and equals the
//     parent dir of a `<dir>/README.md` SCOPE.Document. Returns that
//     Document's ID.
//
// Returns ("", false) for: empty input, no matching file/dir, or input
// that doesn't look like a path (no slash and no recognised extension).
//
// Issue #44 follow-up — markdown extractor's `[text](path)` emits these
// shapes; before this resolver they landed in bug-extractor despite the
// target file existing in the graph.
func (t ImportTable) ResolveMarkdownFilePathTarget(toID string) (string, bool) {
	if toID == "" {
		return "", false
	}
	p := normalizePath(toID)
	if p == "" {
		return "", false
	}
	// Direct file-path hit (preferred path — covers
	// `applicationset/list.yaml`, `plugins/foo/README.md`, etc.).
	if id, ok := t.docByFilePath[p]; ok && id != "" {
		return id, true
	}
	// Directory match (bare-dir links like `[plugins/foo](plugins/foo)`
	// resolve to plugins/foo/README.md). Only try when the path has no
	// file extension — a path like `foo.yaml` that wasn't a direct hit
	// is a missing file, not a directory.
	if !hasFileExtension(p) {
		if id, ok := t.docByDir[p]; ok && id != "" {
			return id, true
		}
	}
	return "", false
}

// hasFileExtension reports whether the basename of p contains a `.`
// after the last slash. Used by ResolveMarkdownFilePathTarget to avoid
// treating misspelled files like `foo.yaml` as directory references.
func hasFileExtension(p string) bool {
	base := p
	if slash := strings.LastIndexByte(p, '/'); slash >= 0 {
		base = p[slash+1:]
	}
	return strings.ContainsRune(base, '.')
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
				// Issue #1694 — Python cross-module attribute call.
				// The extractor stamps import_alias + call_leaf properties on
				// CALLS edges that came from a `<alias>.<leaf>(...)` shape.
				// Resolve those against the per-file import bucket BEFORE the
				// generic bare-name path so a cross-module call doesn't
				// silently bind to a same-named symbol via the wildcard or
				// plain-import fallbacks. When the cross-module probe fails we
				// fall through to ResolveBareCallTarget so the existing
				// resolution surface still gets a turn (e.g. the leaf might be
				// directly imported elsewhere in the file).
				if rel.Properties != nil {
					if alias := rel.Properties["import_alias"]; alias != "" {
						leaf := rel.Properties["call_leaf"]
						if leaf == "" {
							leaf = to
						}
						if id, ok := tbl.ResolveCrossModuleCallTarget(callerFile, alias, leaf); ok {
							rel.ToID = id
							stats.CallsRewritten++
							continue
						}
					}
				}
				id, ok := tbl.ResolveBareCallTarget(callerFile, to)
				if !ok {
					continue
				}
				rel.ToID = id
				stats.CallsRewritten++
			case "REFERENCES":
				// Chain-fix: python-references-cross-file. The Python
				// extractor's emitReferences pass (PR #650 Track A) emits
				// REFERENCES ToIDs as same-file structural refs
				// (`scope:<kind>:ref:<lang>:<file>:<name>`). When the
				// referenced symbol is an imported name, the actual entity
				// lives in another file, so the structural lookup
				// (byLocation[callerFile][name]) misses and the edge lands
				// orphan. Mirror the CALLS path: parse the structural ref,
				// confirm the tail name maps to an IMPORTS binding in the
				// caller's file, and rewrite the ToID to either the
				// in-project entity ID or the binding's `ext:` placeholder.
				//
				// Conservative gating:
				//   - skip non-structural-ref ToIDs (already resolved, or
				//     not a candidate)
				//   - skip Format B (`tail#member`) — only Format A bare
				//     names participate in import-aware rewrite
				//   - skip when the caller file declares a same-file entity
				//     by the same name (the local definition shadows the
				//     import; lookupStructural will bind it).
				if !strings.HasPrefix(to, stubPrefixScope) {
					continue
				}
				stubFile, stubName, ok := splitFormatAStructuralRef(to)
				if !ok {
					continue
				}
				if stubFile != callerFile {
					// Defensive: the stub embeds the caller file, but if a
					// future emitter ever stamps a different file in the
					// stub we shouldn't rewrite against the wrong file's
					// imports.
					continue
				}
				if tbl.localNamesByFile[stubFile] != nil &&
					tbl.localNamesByFile[stubFile][stubName] {
					continue
				}
				stats.ReferencesConsidered++
				id, ok := tbl.ResolveCrossFileReferenceTarget(stubFile, stubName)
				if !ok {
					continue
				}
				rel.ToID = id
				stats.ReferencesRewritten++
			case importRelKind:
				// Markdown cross-file file-path shape (issue #44 follow-up):
				// `[text](path)` emits ToID="<resolved-path>", which is a
				// slash-separated repo-relative path with no dotted-module
				// semantics. Try this BEFORE the dotted-import lookup so a
				// path like `applicationset/list.yaml` (whose last dot lies
				// inside `.yaml`, not a module separator) doesn't get
				// nonsensically split into module="applicationset/list",
				// leaf="yaml". Markdown-emitted paths never contain `\` or
				// `::`, so we gate on `/` presence plus the absence of those
				// non-path delimiters.
				if strings.ContainsRune(to, '/') &&
					!strings.ContainsRune(to, '\\') &&
					!strings.Contains(to, "::") {
					stats.MarkdownFilePathConsidered++
					if id, ok := tbl.ResolveMarkdownFilePathTarget(to); ok {
						rel.ToID = id
						stats.MarkdownFilePathRewritten++
						continue
					}
				}
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
				isPHP := false
				if strings.ContainsRune(normalized, '\\') {
					normalized = strings.ReplaceAll(normalized, "\\", ".")
					// A backslash in the ToID is a strong PHP signal: only
					// the PHP extractor emits `\`-separated namespaces on
					// IMPORTS edges. Enable the namespace-prefix fallback
					// (issue #485 PHP wave-3) below so `use App\Sub\Namespace;`
					// resolves to a representative entity in that namespace.
					isPHP = true
				}
				if !strings.ContainsRune(normalized, '.') {
					continue
				}
				if strings.ContainsAny(normalized, ":#") {
					continue
				}
				stats.ImportsConsidered++
				var (
					id string
					ok bool
				)
				if isPHP {
					id, ok = tbl.ResolveDottedImportTargetForPHP(normalized)
				} else if isJSImportSource(callerFile) {
					id, ok = tbl.ResolveDottedImportTargetForJS(normalized)
				} else {
					id, ok = tbl.ResolveDottedImportTarget(normalized)
					// Issue #778 — Java FQCN ambiguity tie-break.
					// When the generic dotted-import lookup fails because
					// (module, name) is ambiguous AND the edge carries explicit
					// source_module + imported_name properties (Java IMPORTS
					// edges always do), try the Java-specific canonical-file
					// tie-break: prefer the entity whose SourceFile basename
					// matches the class name (Java naming convention).
					if !ok && rel.Properties != nil &&
						rel.Properties[importPropSourceModule] != "" &&
						rel.Properties[importPropImportedName] != "" &&
						rel.Properties["language"] == "java" {
						srcMod := rel.Properties[importPropSourceModule]
						impName := rel.Properties[importPropImportedName]
						id, ok = tbl.lookupModuleEntityJavaCanonical(srcMod, impName)
					}
					// Refs #44 — Python module-level import resolution.
					// `from users import views` emits to_id = "users.views".
					// ResolveDottedImportTarget splits on the last dot and
					// looks for a symbol named "views" in module "users",
					// which misses (the file entity is named "users/views.py").
					// Fallback: probe moduleFileEntity for the dotted path
					// as a whole module name, resolving to the file's
					// SCOPE.Component entity. Only fires for Python (language
					// property on the IMPORTS edge) so other languages with
					// dotted paths are not widened.
					if !ok && rel.Properties != nil &&
						rel.Properties["language"] == "python" {
						id, ok = tbl.ResolvePythonModuleImport(normalized)
					}
					// #1991 — Python __init__.py re-exports of module
					// bindings. `from .celery import app` is normalised by
					// the extractor (#2026) to ToID="upvate_core.celery.app"
					// where `app` is a module-level binding (the
					// `app = Celery(...)` assignment), not a top-level
					// function/class entity. The base (module, leaf) lookup
					// misses because no entity named "app" exists in module
					// "upvate_core.celery". The whole-path fallback above
					// also misses because moduleFileEntity is keyed on
					// "upvate_core.celery", not the synthetic ".app" tail.
					// Strip the leaf and rebind to the parent module entity
					// — the re-export refers to a symbol *defined inside*
					// that module, and the module is the closest live
					// in-graph anchor we have. Without this the edge falls
					// through to the external-synthesis pass and produces
					// an unresolved EXTERNAL synthetic node, breaking the
					// re-export chain and the dead-imports detector.
					if !ok && rel.Properties != nil &&
						rel.Properties["language"] == "python" {
						if dot := strings.LastIndexByte(normalized, '.'); dot > 0 {
							parent := normalized[:dot]
							id, ok = tbl.ResolvePythonModuleImport(parent)
						}
					}
				}
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

// ResolveGoInTreeImports rewrites Go IMPORTS edges whose ToID is an in-tree
// package import path (e.g. "github.com/cajasmota/archigraph/internal/types")
// to the hex entity ID of a representative file in the imported package
// directory. This resolves the dominant unresolved-import pattern for Go-heavy
// corpora: the extractor emits raw module paths as ToIDs, but the resolver's
// entity index contains file-level SCOPE.Component entities keyed by path
// (e.g. "internal/types/types.go"), not by module path.
//
// The pass requires Properties["go_pkg_dir"] to be stamped on the IMPORTS edge
// by the Go extractor's extractImportEntities (only when go.mod is present and
// the import path starts with the repo's module root). Edges without this
// property are left unchanged — they either already have an ext: rewrite or
// will be caught by the external-synthesis pass.
//
// Returns the number of edges rewritten.
//
// Algorithm:
//  1. Build byGoPkgDir: pkgDir → representative entity ID from all Go
//     SCOPE.Component subtype="file" entities. Uses lexicographically smallest
//     SourceFile within each package directory as the canonical representative
//     (stable, deterministic across map-iteration orders).
//  2. Walk every IMPORTS edge; when Properties["go_pkg_dir"] is non-empty and
//     the ToID is not already a hex ID or ext: form, look up pkgDir → entity
//     and rewrite.
func ResolveGoInTreeImports(records []types.EntityRecord) int {
	// Pass 1 — build pkgDir → representative entity ID index.
	byGoPkgDir := buildGoPkgDirIndex(records)
	if len(byGoPkgDir) == 0 {
		return 0
	}

	// Pass 2 — rewrite IMPORTS ToIDs.
	rewrites := 0
	for k := range records {
		e := &records[k]
		for j := range e.Relationships {
			r := &e.Relationships[j]
			if r.Kind != importRelKind {
				continue
			}
			if r.Properties == nil {
				continue
			}
			pkgDir := r.Properties["go_pkg_dir"]
			if pkgDir == "" {
				continue
			}
			// Already resolved (hex ID or ext: form) — skip.
			if isHexID(r.ToID) || strings.HasPrefix(r.ToID, "ext:") {
				continue
			}
			id, ok := byGoPkgDir[pkgDir]
			if !ok || id == "" {
				continue
			}
			r.ToID = id
			rewrites++
		}
	}
	return rewrites
}

// ResolveGoCrossPackageCalls binds Go CALLS edges that target an exported
// symbol in another in-tree package — `resolve.BuildIndex()` from a file in
// internal/engine — to the callee's entity ID.
//
// Background (issue #4332): the Go extractor emits a selector call
// `pkg.Func()` as a CALLS edge whose ToID is the BARE leaf name (`BuildIndex`),
// dropping the `pkg` qualifier. A bare name resolves through the global byName
// index, which goes ambiguous the moment two packages define a symbol of the
// same name (`BuildIndex` in both internal/resolve and internal/symbols) — so
// the edge is dropped and the callee package looks falsely uncalled. This is
// the CALLS analogue of the IMPORTS under-resolution the in-tree import pass
// (#1841) fixed.
//
// The extractor now stamps Properties["go_call_pkg_dir"] (the imported
// package's directory, derived from the import path minus the module root)
// and Properties["call_leaf"] (the bare callee name) on every such edge. This
// pass reads those, looks up byPackageOperation[pkgDir][leaf] — the same
// package-scoped operation index the resolver already builds and uses for
// same-package cross-file calls — and rewrites the ToID to the exact callee.
//
// Conservative by construction:
//   - Only edges carrying go_call_pkg_dir AND call_leaf participate.
//   - Edges whose ToID is already a hex ID are left alone (idempotent).
//   - A blank-string sentinel in byPackageOperation marks (pkgDir, leaf)
//     collisions (e.g. build-tag variants); those are skipped, never guessed.
//
// Must run AFTER BuildIndex (needs byPackageOperation) and BEFORE the
// embedded-reference resolver so the rewritten hex ID is seen as resolved.
// Returns the number of edges rewritten.
func (idx Index) ResolveGoCrossPackageCalls(records []types.EntityRecord) int {
	if len(idx.byPackageOperation) == 0 {
		return 0
	}
	rewrites := 0
	for k := range records {
		rec := &records[k]
		for j := range rec.Relationships {
			r := &rec.Relationships[j]
			if r.Kind != "CALLS" || r.Properties == nil {
				continue
			}
			pkgDir := r.Properties["go_call_pkg_dir"]
			leaf := r.Properties["call_leaf"]
			if pkgDir == "" || leaf == "" {
				continue
			}
			if r.ToID == "" || isHexID(r.ToID) {
				continue // already resolved
			}
			bucket, ok := idx.byPackageOperation[pkgDir]
			if !ok {
				continue
			}
			id, ok := bucket[leaf]
			if !ok || id == "" { // miss or ambiguity sentinel
				continue
			}
			r.ToID = id
			rewrites++
		}
	}
	return rewrites
}

// ResolveRustCrossModuleCalls binds Rust CALLS edges that reach a function or
// associated method in another in-crate module through a path qualifier —
// `crate::services::order::place_order()`, `self::sibling::helper()`,
// `super::parent::helper()`, an aliased `use ... as ord; ord::place_order()`,
// or an associated `OrderService::new()` — to the callee's entity ID.
//
// Background (issue #4373): the Rust extractor historically emitted a
// scoped-identifier call `a::b::leaf()` as a CALLS edge whose ToID was the
// BARE leaf (`leaf`), dropping the whole `a::b` path qualifier. A bare name
// resolves through the global byName index, which goes ambiguous the moment
// two modules define a symbol of the same name (`place_order` in both
// services::order and services::invoice) — so the edge is dropped and the
// callee module looks falsely uncalled. This is the Rust analogue of the Go
// cross-package qualifier drop fixed in #4332.
//
// The extractor now stamps, on each path-qualified CALLS edge:
//   - Properties["rust_call_pkg_dirs"]: ";"-separated candidate package
//     directories the target module's items are keyed under (the mod.rs vs
//     file.rs layout ambiguity yields two; tried in order).
//   - Properties["call_leaf"]: the bare callee identifier.
//   - Properties["rust_call_scope"]: (associated calls only) the Type the leaf
//     is a member of, so the bind goes through byPackageMember[dir][Type][leaf].
//
// This pass reads those and rewrites ToID to the exact callee:
//   - free function: byPackageOperation[dir][leaf]
//   - associated method: byPackageMember[dir][scope][leaf], with a crate-wide
//     fallback to byMember when the type name is unique (an unqualified
//     `Type::method` whose type lives in a sibling module).
//
// Conservative by construction:
//   - Only edges carrying rust_call_pkg_dirs AND call_leaf participate.
//   - Edges whose ToID is already a hex ID are left alone (idempotent).
//   - A blank-string sentinel marks (dir, name) collisions; those are skipped,
//     never guessed. The first candidate dir that yields an unambiguous hit
//     wins; if two candidate dirs both resolve to DIFFERENT ids the edge is
//     left alone (ambiguous layout).
//
// Must run AFTER BuildIndex (needs the package-scoped indexes) and BEFORE the
// embedded-reference resolver so the rewritten hex ID is seen as resolved.
// Returns the number of edges rewritten.
func (idx Index) ResolveRustCrossModuleCalls(records []types.EntityRecord) int {
	if len(idx.byPackageOperation) == 0 && len(idx.byPackageMember) == 0 {
		return 0
	}
	rewrites := 0
	for k := range records {
		rec := &records[k]
		for j := range rec.Relationships {
			r := &rec.Relationships[j]
			if r.Kind != "CALLS" || r.Properties == nil {
				continue
			}
			dirsRaw := r.Properties["rust_call_pkg_dirs"]
			leaf := r.Properties["call_leaf"]
			if dirsRaw == "" || leaf == "" {
				continue
			}
			if r.ToID == "" || isHexID(r.ToID) {
				continue // already resolved
			}
			scope := r.Properties["rust_call_scope"]
			dirs := strings.Split(dirsRaw, ";")

			// Try each candidate directory; require a single unambiguous
			// non-empty id across the candidates that resolve.
			resolved := ""
			conflict := false
			for _, dir := range dirs {
				if dir == "" {
					continue
				}
				id := ""
				if scope != "" {
					if pkgBucket, ok := idx.byPackageMember[dir]; ok {
						if scopeBucket, ok := pkgBucket[scope]; ok {
							id = scopeBucket[leaf] // "" => collision sentinel
						}
					}
				} else {
					if pkgBucket, ok := idx.byPackageOperation[dir]; ok {
						id = pkgBucket[leaf] // "" => collision sentinel or miss
					}
				}
				if id == "" {
					continue
				}
				if resolved == "" {
					resolved = id
				} else if resolved != id {
					conflict = true
					break
				}
			}

			// Associated-call crate-wide fallback: an `OrderService::new()`
			// whose type is unique in the crate but lives outside the offered
			// candidate dirs. Bind through the unambiguous global member index.
			if resolved == "" && !conflict && scope != "" {
				if id := idx.lookupUniqueMember(scope, leaf); id != "" {
					resolved = id
				}
			}

			if conflict || resolved == "" {
				continue
			}
			r.ToID = resolved
			rewrites++
		}
	}
	return rewrites
}

// ResolveCSharpCrossNamespaceCalls binds C# CALLS edges that reach a method on
// a type in another namespace through a qualifier on a member-access call —
// `App.Services.Orders.OrderService.Place()`, an aliased
// `using Ord = App.Services.Orders; Ord.OrderService.Place()`, a
// `using static App.Services.Orders.OrderService; Place()`, a same-namespace
// static `OrderService.Create()`, or a `global::App.Services...Place()` — to
// the callee's entity ID.
//
// Background (issue #4374): the C# extractor only types a single-level receiver,
// so a multi-segment qualified call collapses to the bare leaf method name
// (`Place`). A bare name resolves through the global byName index, which goes
// ambiguous the moment two namespaces define a same-named method/type
// (`OrderService.Place` in both App.Services.Orders and App.Services.Billing) —
// so the CALLS edge drops and the callee namespace looks falsely uncalled. This
// is the C# analogue of the Go cross-package (#4332) and Rust cross-module
// (#4373) qualifier drops.
//
// Unlike Go/Rust, C# namespaces are NOT directory-bound, so this pass keys on
// the C# NAMESPACE via byNamespaceMember[namespace][Type][leaf] rather than a
// source directory. The extractor stamps, on each qualified CALLS edge:
//   - Properties["csharp_call_ns"]: ";"-separated candidate namespaces (a
//     fully-qualified path yields one; a `Type.method()` static call yields the
//     static-import / file / using namespaces, most-specific first).
//   - Properties["csharp_call_type"]: the declaring type name.
//   - Properties["call_leaf"]: the bare callee method name.
//
// Conservative by construction, mirroring the Go/Rust passes:
//   - Only edges carrying csharp_call_ns, csharp_call_type AND call_leaf
//     participate.
//   - Edges whose ToID is already a hex ID are left alone (idempotent).
//   - The first candidate namespace that yields an unambiguous non-blank id
//     wins; if two candidates resolve to DIFFERENT ids the edge is left alone
//     (ambiguous). A blank-string sentinel (collision) is skipped, never
//     guessed.
//
// Must run AFTER BuildIndex (needs byNamespaceMember) and BEFORE the
// embedded-reference resolver so the rewritten hex ID is seen as resolved.
// Returns the number of edges rewritten.
func (idx Index) ResolveCSharpCrossNamespaceCalls(records []types.EntityRecord) int {
	if len(idx.byNamespaceMember) == 0 {
		return 0
	}
	rewrites := 0
	for k := range records {
		rec := &records[k]
		for j := range rec.Relationships {
			r := &rec.Relationships[j]
			if r.Kind != "CALLS" || r.Properties == nil {
				continue
			}
			nsRaw := r.Properties["csharp_call_ns"]
			typ := r.Properties["csharp_call_type"]
			leaf := r.Properties["call_leaf"]
			if nsRaw == "" || typ == "" || leaf == "" {
				continue
			}
			if r.ToID == "" || isHexID(r.ToID) {
				continue // already resolved
			}
			resolved := ""
			conflict := false
			for _, ns := range strings.Split(nsRaw, ";") {
				if ns == "" {
					continue
				}
				nsBucket, ok := idx.byNamespaceMember[ns]
				if !ok {
					continue
				}
				typeBucket, ok := nsBucket[typ]
				if !ok {
					continue
				}
				id := typeBucket[leaf] // "" => collision sentinel or miss
				if id == "" {
					continue
				}
				if resolved == "" {
					resolved = id
				} else if resolved != id {
					conflict = true
					break
				}
			}
			if conflict || resolved == "" {
				continue
			}
			r.ToID = resolved
			rewrites++
		}
	}
	return rewrites
}

// ResolveKotlinCrossPackageCalls binds Kotlin CALLS edges that reach a
// function/method in another package through a qualifier on a navigation
// invocation — a fully-qualified `com.app.services.OrderService.place()`, an
// imported top-level function (`import com.app.services.placeOrder;
// placeOrder()`), an imported/aliased type member (`import
// com.app.services.Orders; Orders.place()`), or a same-package companion/object
// member (`OrderService.create()`) — to the callee's entity ID.
//
// Background (issue #4375): the Kotlin extractor's call target is the trailing
// simple_identifier of the navigation chain, so a multi-segment qualified call
// collapses to the bare leaf method name (`place`). A bare name resolves through
// the global byName index, which goes ambiguous the moment two packages define a
// same-named function/type (`OrderService.place` in both com.app.services and
// com.app.billing) — so the CALLS edge drops and the callee package looks
// falsely uncalled. This is the Kotlin analogue of the Go cross-package (#4332),
// Rust cross-module (#4373), and C# cross-namespace (#4374) qualifier drops.
//
// Like C# namespaces, Kotlin `package` declarations are NOT directory-bound, so
// this pass keys on the Kotlin PACKAGE via byKotlinPkgMember[pkg][Type][leaf]
// (members) and byKotlinPkgFunc[pkg][leaf] (top-level functions) rather than a
// source directory. The extractor stamps, on each qualified CALLS edge:
//   - Properties["kotlin_call_pkg"]: ";"-separated candidate packages (a
//     fully-qualified path yields one; a `Type.method()` member call yields the
//     imported-type / file packages, most-specific first).
//   - Properties["kotlin_call_type"]: the declaring type (absent for a
//     top-level-function call).
//   - Properties["call_leaf"]: the bare callee name.
//
// Conservative by construction, mirroring the Go/Rust/C# passes:
//   - Only edges carrying kotlin_call_pkg AND call_leaf participate.
//   - Edges whose ToID is already a hex ID are left alone (idempotent).
//   - The first candidate package that yields an unambiguous non-blank id wins;
//     if two candidates resolve to DIFFERENT ids the edge is left alone
//     (ambiguous). A blank-string sentinel (collision) is skipped, never guessed.
//
// Must run AFTER BuildIndex (needs the Kotlin package indexes) and BEFORE the
// embedded-reference resolver so the rewritten hex ID is seen as resolved.
// Returns the number of edges rewritten.
func (idx Index) ResolveKotlinCrossPackageCalls(records []types.EntityRecord) int {
	if len(idx.byKotlinPkgMember) == 0 && len(idx.byKotlinPkgFunc) == 0 {
		return 0
	}
	rewrites := 0
	for k := range records {
		rec := &records[k]
		for j := range rec.Relationships {
			r := &rec.Relationships[j]
			if r.Kind != "CALLS" || r.Properties == nil {
				continue
			}
			pkgRaw := r.Properties["kotlin_call_pkg"]
			leaf := r.Properties["call_leaf"]
			if pkgRaw == "" || leaf == "" {
				continue
			}
			if r.ToID == "" || isHexID(r.ToID) {
				continue // already resolved
			}
			typ := r.Properties["kotlin_call_type"] // "" => top-level function
			resolved := ""
			conflict := false
			for _, pkg := range strings.Split(pkgRaw, ";") {
				if pkg == "" {
					continue
				}
				id := ""
				if typ != "" {
					if pkgBucket, ok := idx.byKotlinPkgMember[pkg]; ok {
						if typeBucket, ok := pkgBucket[typ]; ok {
							id = typeBucket[leaf] // "" => collision sentinel or miss
						}
					}
				} else {
					if pkgBucket, ok := idx.byKotlinPkgFunc[pkg]; ok {
						id = pkgBucket[leaf] // "" => collision sentinel or miss
					}
				}
				if id == "" {
					continue
				}
				if resolved == "" {
					resolved = id
				} else if resolved != id {
					conflict = true
					break
				}
			}
			if conflict || resolved == "" {
				continue
			}
			r.ToID = resolved
			rewrites++
		}
	}
	return rewrites
}

// lookupUniqueMember returns the entity id for scope.member if and only if it
// is unique across the whole crate (every byMember file-bucket that defines it
// agrees on the same id). Returns "" on miss or any disagreement (ambiguous).
func (idx Index) lookupUniqueMember(scope, member string) string {
	found := ""
	for _, fileBucket := range idx.byMember {
		scopeBucket, ok := fileBucket[scope]
		if !ok {
			continue
		}
		id, ok := scopeBucket[member]
		if !ok || id == "" {
			continue
		}
		if found == "" {
			found = id
		} else if found != id {
			return "" // ambiguous across files
		}
	}
	return found
}

// buildGoPkgDirIndex builds a map from Go package directory (e.g.
// "internal/types") to the entity ID of a representative file in that package.
// Only Go SCOPE.Component subtype="file" entities are considered. When multiple
// files exist in the same directory, the one with the lexicographically smallest
// SourceFile path wins (stable tie-break independent of entity-slice order).
func buildGoPkgDirIndex(records []types.EntityRecord) map[string]string {
	// pkgDir → (bestSourceFile, bestEntityID) — we track both so we can
	// apply the stable lex-sort tie-break without a separate sort pass.
	type entry struct {
		sourceFile string
		id         string
	}
	best := make(map[string]entry)

	for k := range records {
		e := &records[k]
		if e.Language != "go" || e.Kind != "SCOPE.Component" || e.Subtype != "file" {
			continue
		}
		if e.ID == "" || e.SourceFile == "" {
			continue
		}
		sf := normalizePath(e.SourceFile)
		if !strings.HasSuffix(sf, ".go") {
			continue
		}
		// Package directory = parent directory of the source file.
		pkgDir := sf
		if slash := strings.LastIndexByte(sf, '/'); slash >= 0 {
			pkgDir = sf[:slash]
		} else {
			// File at repo root; package dir is "" (root package).
			pkgDir = ""
		}
		// Stable lex-sort: keep the entity whose SourceFile path is
		// lexicographically smallest within the package directory.
		if cur, ok := best[pkgDir]; !ok || sf < cur.sourceFile {
			best[pkgDir] = entry{sourceFile: sf, id: e.ID}
		}
	}

	out := make(map[string]string, len(best))
	for dir, ent := range best {
		out[dir] = ent.id
	}
	return out
}

// PruneImportPlaceholderStats summarises the prune pass for the
// indexer's stderr log.
type PruneImportPlaceholderStats struct {
	// Considered is the number of kind=SCOPE.Component, subtype="import"
	// entities the prune pass saw.
	Considered int
	// Pruned is the number actually removed from the returned slice.
	Pruned int
	// RelsHoisted is the number of embedded relationship records that
	// were transplanted from a pruned placeholder onto the file-level
	// SCOPE.Component entity for the same SourceFile.
	RelsHoisted int
	// RelsOrphaned is the number of embedded relationship records that
	// were attached to a pruned placeholder but had no matching
	// file-level SCOPE.Component entity to receive them. These are
	// returned alongside the entity slice as standalone records via
	// the second return value of PruneImportPlaceholders so the
	// downstream assembly loop still emits them on the document.
	RelsOrphaned int
	// PlaceholderKept is the number of kind=SCOPE.Component
	// subtype="import" entities the pass intentionally KEPT in the
	// graph because nothing else points at them yet AND the rels they
	// carried could not be safely hoisted (empty FromID on at least
	// one rel, which would lose provenance). Surfaced so a future
	// regression that silently inflates this number is visible.
	PlaceholderKept int
	// EdgeToIDRewrites is the number of IMPORTS edges whose ToID was
	// rewritten from a placeholder reference (by hex ID, or by raw
	// module string matched within the importer's file) to the
	// file-level SCOPE.Component (subtype="file") entity for the
	// resolved import target. Surfaced so a regression that silently
	// stops rewriting (and therefore drops every JS/TS relative
	// IMPORTS edge from the graph) is visible.
	EdgeToIDRewrites int
}

// PruneImportPlaceholders removes import-placeholder entities (kind =
// SCOPE.Component, subtype = "import") from the merged EntityRecord
// slice. These entities were emitted by the JS/TS extractor (issue
// #421/#570/#578) and the cross-language imports extractor
// (internal/extractors/cross/imports) as a structural carrier for
// IMPORTS / DEPENDS_ON relationships. After the import-resolver and
// references-resolver passes have rewritten ToID / FromID values, the
// placeholders themselves have no incoming edges — they are pure
// structural overhead and account for the largest single bucket of
// orphan entities on the verify2 corpus (2,583 of 9,390 fixture-b
// orphans, root-cause analysis 2026-05-19).
//
// The function preserves the placeholders' embedded relationships by
// hoisting them onto the file-level SCOPE.Component entity that
// reflects the same SourceFile (subtype = "file", Name = source
// path). When no such carrier exists for a placeholder's SourceFile,
// the embedded rels with non-empty FromID are returned as standalone
// RelationshipRecords through the second return value so the
// indexer's assembly loop can still emit them on the document.
//
// Cross-repo linker (#566/#570/#578) note: the linker matches on
// file-level SCOPE.Component (subtype="file") entities and qualified
// `ext:<module>:<name>` ToIDs. Neither of those entity classes is
// pruned by this function — only the placeholder shape with
// subtype="import" is removed. The linker continues to find both its
// match-target classes after pruning.
//
// Returns the filtered slice, any rels that couldn't be hoisted, and
// a stats record. The input slice's element ordering among survivors
// is preserved.
func PruneImportPlaceholders(records []types.EntityRecord) ([]types.EntityRecord, []types.RelationshipRecord, PruneImportPlaceholderStats) {
	var stats PruneImportPlaceholderStats
	if len(records) == 0 {
		return records, nil, stats
	}

	// Pre-pass: collect placeholder indices and build the file-level
	// carrier path -> original-index map. We do not yet know the
	// post-prune index of each carrier — we compute that below by
	// counting prunable predecessors in a single pass.
	carrierByPath := make(map[string]int, len(records))
	// carrierIDByPath maps normalized file path -> file-level carrier
	// entity's stamped hex ID, used by the IMPORTS-edge ToID rewrite
	// pass below. Identical keyspace to carrierByPath; populated in the
	// same scan to avoid a second walk.
	carrierIDByPath := make(map[string]string, len(records))
	for i := range records {
		r := &records[i]
		if r.Kind == "SCOPE.Component" && r.Subtype == "file" && r.SourceFile != "" {
			key := normalizePath(r.SourceFile)
			if _, seen := carrierByPath[key]; !seen {
				carrierByPath[key] = i
				carrierIDByPath[key] = r.ID
			}
		}
	}

	// Pre-prune ToID rewrite (issue #642 regression fix). The JS/TS
	// extractor emits IMPORTS edges with ToID == the relative-path
	// module string (`./pages/Home`); the dotted-import resolver may
	// have already rewritten that to the placeholder's stamped hex ID
	// during ResolveImports. Either shape becomes a dangling reference
	// the moment the placeholder is pruned, which silently drops every
	// JS/TS relative IMPORTS edge from the graph (typescript-react-mini
	// recall 16/16 → 10/16 in the first cut of PR #642).
	//
	// The fix: BEFORE pruning, walk every placeholder we're about to
	// drop, resolve its module string against the importer's directory
	// trying each canonical JS/TS extension, look the resulting path up
	// in carrierIDByPath, and rewrite every IMPORTS edge whose ToID
	// points at the placeholder to the file-level carrier's hex ID.
	// The IMPORTS edge then survives prune pointing at a real, durable
	// entity.
	//
	// We collect both rewrite keys per placeholder:
	//   - by hex ID: matches edges the dotted resolver has already
	//     rewritten (ResolveImports / ResolveDottedImportTargetForJS).
	//   - by raw module string scoped to the placeholder's SourceFile:
	//     matches edges the resolver could not rewrite (no JS dotted
	//     form, e.g. `../types/user`) but that still live on a record
	//     emitted by the same importer file.
	rewriteByID := make(map[string]string, len(records))
	rewriteByNameInFile := make(map[string]map[string]string)
	for i := range records {
		r := &records[i]
		if !(r.Kind == "SCOPE.Component" && r.Subtype == "import") {
			continue
		}
		module := r.Name
		if r.Properties != nil {
			if m := r.Properties["module"]; m != "" {
				module = m
			}
		}
		if module == "" {
			continue
		}
		importer := normalizePath(r.SourceFile)
		targetID, ok := resolveRelativeImportTarget(importer, module, carrierIDByPath)
		if !ok {
			continue
		}
		if r.ID != "" {
			rewriteByID[r.ID] = targetID
		}
		if importer != "" {
			m, exists := rewriteByNameInFile[importer]
			if !exists {
				m = make(map[string]string, 4)
				rewriteByNameInFile[importer] = m
			}
			// Index by both the raw Name and the Properties.module
			// form — extractors that emit IMPORTS ToID as the entity
			// Name and those that emit it as the canonical module
			// string both resolve.
			m[r.Name] = targetID
			m[module] = targetID
		}
	}
	if len(rewriteByID) > 0 || len(rewriteByNameInFile) > 0 {
		for i := range records {
			r := &records[i]
			importer := normalizePath(r.SourceFile)
			fileMap := rewriteByNameInFile[importer]
			for j := range r.Relationships {
				rel := &r.Relationships[j]
				if rel.Kind != importRelKind {
					continue
				}
				if id, ok := rewriteByID[rel.ToID]; ok {
					rel.ToID = id
					stats.EdgeToIDRewrites++
					continue
				}
				if fileMap != nil {
					if id, ok := fileMap[rel.ToID]; ok {
						rel.ToID = id
						stats.EdgeToIDRewrites++
					}
				}
			}
		}
	}

	// First pass: decide for each record whether it survives. We need
	// this decided before we can compute post-prune carrier indices.
	prunable := make([]bool, len(records))
	hoistTo := make([]int, len(records)) // original-index of carrier or -1
	for i := range records {
		hoistTo[i] = -1
	}
	for i := range records {
		r := &records[i]
		if !(r.Kind == "SCOPE.Component" && r.Subtype == "import") {
			continue
		}
		stats.Considered++
		key := normalizePath(r.SourceFile)
		if key == "" {
			// No SourceFile; can't hoist. Try to migrate rels
			// directly to the orphan-rel slice if every rel has a
			// non-empty FromID.
			if canMigrate(r.Relationships) {
				prunable[i] = true
			}
			continue
		}
		if origIdx, ok := carrierByPath[key]; ok {
			prunable[i] = true
			hoistTo[i] = origIdx
			continue
		}
		if canMigrate(r.Relationships) {
			prunable[i] = true
		}
	}

	// Second pass: compute the post-prune index for each original
	// index. Original indices that aren't pruned land at
	// (originalIdx - prunedBefore). Original indices that are pruned
	// have no post-prune mapping (we'll never need them on the LHS).
	postIdx := make([]int, len(records))
	{
		pruned := 0
		for i := range records {
			if prunable[i] {
				pruned++
				postIdx[i] = -1
				continue
			}
			postIdx[i] = i - pruned
		}
	}

	// Third pass: materialise the survivor slice and hoist rels onto
	// the file-level carrier entities at their post-prune indices.
	out := make([]types.EntityRecord, 0, len(records)-stats.Considered)
	var orphanRels []types.RelationshipRecord
	for i := range records {
		r := &records[i]
		if !prunable[i] {
			out = append(out, *r)
			continue
		}
		stats.Pruned++
		if hoistTo[i] >= 0 {
			carrierPostIdx := postIdx[hoistTo[i]]
			if carrierPostIdx >= 0 && carrierPostIdx < len(out) {
				out[carrierPostIdx].Relationships = append(out[carrierPostIdx].Relationships, r.Relationships...)
				stats.RelsHoisted += len(r.Relationships)
				continue
			}
		}
		// No carrier — every rel here has a non-empty FromID (we
		// gated on canMigrate above), so each can stand alone.
		orphanRels = append(orphanRels, r.Relationships...)
		stats.RelsOrphaned += len(r.Relationships)
	}

	// Account for placeholders we INTENTIONALLY kept (rels couldn't
	// be safely migrated). Considered minus Pruned is exactly that.
	stats.PlaceholderKept = stats.Considered - stats.Pruned

	return out, orphanRels, stats
}

// resolveRelativeImportTarget resolves a JS/TS relative import string
// (`./pages/Home`, `../hooks/useUsers`, `./UserCard`) against the
// importer's directory and returns the stamped hex ID of the
// file-level SCOPE.Component carrier for the resolved target file, if
// one exists in carrierIDByPath.
//
// The resolver tries each canonical JS/TS extension (.ts, .tsx, .js,
// .jsx, .mjs, .cjs) plus the directory-index forms (resolved/index.ts
// and friends) in the same order as the JS extractor's
// resolveRelativeImport so module derivation and import resolution
// agree on which extension wins. Non-relative specifiers (anything
// not starting with `./` or `../`) and empty importers return ok=false
// — those are bare-name or alias-resolved imports and the dotted-import
// resolver already handled them in ResolveImports.
func resolveRelativeImportTarget(importer, module string, carrierIDByPath map[string]string) (string, bool) {
	if importer == "" || module == "" {
		return "", false
	}
	if !(strings.HasPrefix(module, "./") || strings.HasPrefix(module, "../")) {
		return "", false
	}
	dir := path.Dir(importer)
	base := path.Clean(path.Join(dir, module))
	// Direct hit — module already includes a recognised extension.
	if id, ok := carrierIDByPath[base]; ok {
		return id, true
	}
	for _, ext := range jsExtensions {
		if id, ok := carrierIDByPath[base+ext]; ok {
			return id, true
		}
	}
	// Directory-index form: `./components/branding` → `components/branding/index.ts`.
	for _, ext := range jsExtensions {
		if id, ok := carrierIDByPath[path.Join(base, "index"+ext)]; ok {
			return id, true
		}
	}
	return "", false
}

// canMigrate reports whether every rel in the slice has a non-empty
// FromID. PruneImportPlaceholders uses this gate to decide whether a
// placeholder whose SourceFile has no file-level carrier can still be
// dropped: when every rel can stand alone (FromID already rewritten
// to a file-path or hex ID), migrating them to the standalone-rel
// list preserves graph semantics. When any rel has empty FromID the
// assembly loop would substitute the parent placeholder's hex ID, so
// dropping the placeholder would lose provenance.
func canMigrate(rels []types.RelationshipRecord) bool {
	for i := range rels {
		if rels[i].FromID == "" {
			return false
		}
	}
	return true
}
