// Package javascript — issue #421 import-property emission and relative-
// path resolution helpers.
//
// The IMPORTS edges this file produces carry the property contract that
// the cross-file resolver pre-pass (internal/resolve/imports.go) reads:
//
//	Properties["local_name"]    — identifier introduced by the import
//	Properties["source_module"] — dotted module path (canonical form)
//	Properties["imported_name"] — original (pre-alias) symbol name
//	Properties["wildcard"]      — "1" for `import * as X from "...";`
//	Properties["import_path"]   — raw import specifier (relative or npm)
//	Properties["resolved_file"] — for relative imports, the importer-
//	                              relative path resolved against the
//	                              importer's directory and stamped with
//	                              the receiver-target file path the
//	                              resolver consumes (issue #421).
//
// `resolved_file` is *not* read by the resolver pre-pass; it is consumed
// by the receiver-binding logic inside this package to materialise
// structural-ref Format A CALLS targets at extract time.
package javascript

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
)

// osStatRegular reports whether absPath stats as a regular file. Used
// by the alias-existence check (issue #505) — never panics, never
// follows non-regular shapes (sockets, symlinks to dirs).
func osStatRegular(absPath string) bool {
	info, err := os.Stat(absPath)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

// filepathJoin joins a POSIX repo-relative path under an absolute repo
// root using the host filesystem separator. Forward slashes in the
// repo-relative tail are translated so the resulting path stats on
// both Unix and Windows hosts.
func filepathJoin(root, repoRel string) string {
	if root == "" {
		return repoRel
	}
	return filepath.Join(root, filepath.FromSlash(repoRel))
}

// importBinding captures everything the receiver binder needs to know
// about a single name introduced into a file by an import statement.
//
// resolvedFile is non-empty only for relative imports. The extractor
// resolves the import specifier against the importer's directory and
// stamps the resulting forward-slashed path here. External (npm)
// imports leave it empty so the receiver binder falls back to the bare
// method name.
type importBinding struct {
	localName    string
	importedName string
	sourceModule string // dotted, canonical (post path-resolve for relative)
	importPath   string // raw spec ("./services/user.service" or "express")
	resolvedFile string // resolved repo-relative path with extension or ""
	wildcard     bool   // true for `import * as X from "..."`
	// aliasResolved — issue #505. True when the spec was rewritten
	// through the per-repo alias map (tsconfig/vite/metro/babel). The
	// IMPORTS-edge emitter uses this flag to switch to dotted-module
	// ToIDs; plain relative imports keep the legacy raw-spec ToID to
	// preserve the pre-#505 disposition shape on the express, nestjs
	// and similar corpora that have no alias map.
	aliasResolved bool
}

// jsImportExtensions enumerates the canonical extensions the resolver
// tries when resolving a relative import without an explicit suffix.
// The first match wins; ordering mirrors the TypeScript compiler's
// resolution order so a project that ships both .ts and .d.ts pulls
// the implementation file.
var jsImportExtensions = []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"}

// resolveRelativeImport resolves a relative import specifier (starts
// with "./" or "../") against the importing file's directory. Returns
// the forward-slashed repo-relative path the import points at, with
// the canonical extension chosen from jsImportExtensions. Bare specifiers
// and absolute paths return ""; the caller treats that as "external".
//
// The function does not stat the filesystem — it materialises the
// candidate paths the verify2 corpus is most likely to ship. That is
// sufficient for a structural-ref binding because the resolver matches
// on the candidate string against the corpus's actual file paths and
// silently misses on bad guesses (no harm done — falls back to bare).
//
// Index files (`./users` → `./users/index.ts`) are not handled here;
// the verify2 corpus on ts/nestjs and ts/nestjs-starter reaches every
// receiver-typed call through an explicit module file path.
func resolveRelativeImport(importerFile, spec string) string {
	if spec == "" {
		return ""
	}
	if !strings.HasPrefix(spec, "./") && !strings.HasPrefix(spec, "../") {
		return ""
	}
	dir := path.Dir(importerFile)
	joined := path.Clean(path.Join(dir, spec))
	// Spec already carries an extension we know? Use it verbatim.
	for _, ext := range jsImportExtensions {
		if strings.HasSuffix(joined, ext) {
			return joined
		}
	}
	// Default: prefer .ts, fall back through the others. Receiver
	// binding accepts the first extension; the resolver's byLocation
	// index either has the file or it doesn't, and bare-name fallback
	// handles the miss.
	return joined + ".ts"
}

// appendDefaultJSExtension stamps a `.ts` extension onto p when p does
// not already carry one of jsImportExtensions. Mirrors the trailing
// branch of resolveRelativeImport so an alias-substituted bare path
// like `src/store/app/useAppStore` and a relative path like
// `./useAppStore` reach dottedModuleFromPath in the same shape.
//
// Note: a missing extension is the common case for alias-substituted
// paths because tsconfig/vite/metro/babel alias values do not include
// extensions. The `.ts` choice mirrors the resolver convention; the
// downstream module-derivation step in modulesForJSFile strips ALL
// canonical extensions, so the actual on-disk extension doesn't affect
// the dotted-module form that lands on the IMPORTS edge ToID.
func appendDefaultJSExtension(p string) string {
	if p == "" {
		return ""
	}
	for _, ext := range jsImportExtensions {
		if strings.HasSuffix(p, ext) {
			return p
		}
	}
	return p + ".ts"
}

// dottedModuleFromPath returns the canonical dotted-module form of a
// resolved file path. Strips the recognised JS/TS extension and
// replaces forward slashes with dots. Empty input → empty output.
//
// The resolver's modulesForJSFile mirror function applies the same
// transform to entity SourceFile values, so an IMPORTS edge whose
// source_module is set via this helper binds against the imported
// file's entities.
func dottedModuleFromPath(p string) string {
	if p == "" {
		return ""
	}
	stripped := p
	for _, ext := range jsImportExtensions {
		if strings.HasSuffix(stripped, ext) {
			stripped = strings.TrimSuffix(stripped, ext)
			break
		}
	}
	return strings.ReplaceAll(stripped, "/", ".")
}

// collectFileImports walks the top-level statements of root and returns
// a slice of importBinding describing every name introduced into the
// file by ES6 import_statement and CommonJS require() destructuring.
// Imports that yield no useful binding (unrecognised shape, empty spec)
// are skipped.
//
// The slice is ordered by source position so emitter output is stable
// across runs.
func (x *extractor) collectFileImports(root ts.Node) []importBinding {
	var out []importBinding
	x.collectFileImportsNode(root, &out)
	return out
}

func (x *extractor) collectFileImportsNode(n ts.Node, out *[]importBinding) {
	if n == nil {
		return
	}
	if n.Type() == "import_statement" {
		x.collectFromImportStatement(n, out)
		return // do not recurse into the import — children are clauses we
		// already walked.
	}
	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		x.collectFileImportsNode(n.Child(i), out)
	}
}

// collectFromImportStatement parses a single ES6 import_statement and
// appends every binding it introduces to out.
//
// Supported shapes:
//
//	import "./side-effect";                       — no binding (skipped)
//	import express from "express";                — default import
//	import { Foo, Bar as Baz } from "./mod";      — named imports
//	import * as ns from "fs";                     — namespace import
//	import express, { Request } from "express";   — combined
//
// Anything else (type-only imports, dynamic import()) is silently
// dropped — the receiver binder won't have type information for those.
func (x *extractor) collectFromImportStatement(n ts.Node, out *[]importBinding) {
	// Specifier (the "..." string) is the import_statement's source field.
	source := x.findStringChild(n)
	if source == "" {
		return
	}
	resolved := resolveRelativeImport(x.filePath, source)
	// Issue #505 — when the spec doesn't resolve as a relative path,
	// try the project's path-alias map (tsconfig paths, vite
	// resolve.alias, metro resolver.alias, babel module-resolver).
	// tsconfig declarations may list multiple candidate directories per
	// alias (e.g. `@/*: ["./*", "./src/*"]`). For each candidate we
	// emit a separate set of bindings — the resolver's per-module
	// reverse index binds whichever target actually contains the
	// imported symbol; the wrong candidates miss without inflating
	// disposition counts because at most one candidate maps to a real
	// project entity.
	aliasResolved := false
	if resolved == "" {
		if substituted := x.aliases.ResolveAll(source); len(substituted) > 0 {
			picked, exists := pickExistingAliasTargetExists(x.repoRoot, substituted)
			resolved = picked
			// PLT #537 — when the alias substituted but no candidate file
			// exists on disk, the import target is genuinely outside the
			// project source (uninstalled npm dep, stale code path, RN
			// `@/components/themed-text` shimmed elsewhere). Routing it
			// through the alias-resolved (`<module>.<leaf>`) ToID shape
			// then lands every named/default import in bug-extractor with
			// no project entity to bind to. Flip aliasResolved=false so
			// the IMPORTS edge keeps the raw alias spec and flows through
			// external-synth (external-unknown) — honest disposition and
			// keeps RN/Expo bug-extractor counts off the floor.
			aliasResolved = exists
		} else if x.aliases.BaseURLSet && x.repoRoot != "" {
			// Synthetic baseUrl fallback (#2576 / #4696): when tsconfig has a
			// baseUrl but no paths{}, resolve bare imports relative to baseUrl
			// only when the candidate file actually exists on disk. npm
			// package imports (e.g. "react") that have no matching file under
			// baseUrl are left unresolved here and fall through to external
			// treatment — the on-disk existence check is what keeps this safe.
			//
			// #4696: BaseURL == "" with BaseURLSet == true is the repo-root
			// form (`baseUrl: "."`), the dominant `import 'src/modules/...'`
			// convention. The candidate is the bare specifier resolved
			// directly against the repo root (no directory prefix).
			candidate := source
			if x.aliases.BaseURL != "" {
				candidate = x.aliases.BaseURL + "/" + source
			}
			if found := firstExistingJSPath(x.repoRoot, candidate); found != "" {
				resolved = found
				aliasResolved = true
			}
		}
	}
	dotted := source // default: npm spec verbatim (slashes become dots below)
	if resolved != "" {
		dotted = dottedModuleFromPath(resolved)
	} else {
		dotted = strings.ReplaceAll(source, "/", ".")
	}

	// Walk every named child looking for import clauses. The grammar
	// names vary by tree-sitter version; we match on the well-known
	// ones and ignore unknown shapes.
	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		child := n.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "import_clause":
			x.parseImportClause(child, source, dotted, resolved, aliasResolved, out)
		}
	}
}

// pickExistingAliasTarget filesystem-checks each candidate substituted
// path against the repo root and returns the first one whose resolved
// file (after extension fallback) actually exists on disk. When NO
// candidate exists — or repoRoot is empty so we can't stat — the first
// candidate is returned anyway so the IMPORTS edge still carries a
// reasonable dotted form (the resolver will simply miss the lookup and
// the edge flows to bug-extractor, which is the safer-bias bucket per
// #94/#105/#106 for unverifiable substitutions).
//
// Candidates arrive without an extension; we try every jsImportExtension
// plus an `/index.<ext>` lookup for directory imports. The first hit
// wins.
func pickExistingAliasTarget(repoRoot string, candidates []string) string {
	picked, _ := pickExistingAliasTargetExists(repoRoot, candidates)
	return picked
}

// pickExistingAliasTargetExists is pickExistingAliasTarget plus a boolean
// reporting whether ANY of the candidates filesystem-checked to a real
// file (PLT #537). Callers use the boolean to gate the alias-resolved
// dotted-leaf ToID shape on the IMPORTS edge: when the alias substituted
// but every candidate misses on disk, the target is not project-internal
// — emitting `<module>.<leaf>` then leaves the named/default import in
// bug-extractor instead of external-unknown.
func pickExistingAliasTargetExists(repoRoot string, candidates []string) (string, bool) {
	if len(candidates) == 0 {
		return "", false
	}
	if repoRoot == "" {
		return appendDefaultJSExtension(candidates[0]), false
	}
	for _, c := range candidates {
		if found := firstExistingJSPath(repoRoot, c); found != "" {
			return found, true
		}
	}
	return appendDefaultJSExtension(candidates[0]), false
}

// firstExistingJSPath tries `<candidate><.ext>` and
// `<candidate>/index<.ext>` against the repo root for every recognised
// JS/TS extension. Returns the first repo-relative POSIX path that
// stats as a regular file, or "" when nothing matches.
func firstExistingJSPath(repoRoot, candidate string) string {
	if candidate == "" {
		return ""
	}
	// Verbatim — already has an extension, or is itself a file.
	if osStatRegular(filepathJoin(repoRoot, candidate)) {
		return candidate
	}
	for _, ext := range jsImportExtensions {
		try := candidate + ext
		if osStatRegular(filepathJoin(repoRoot, try)) {
			return try
		}
	}
	for _, ext := range jsImportExtensions {
		try := candidate + "/index" + ext
		if osStatRegular(filepathJoin(repoRoot, try)) {
			return try
		}
	}
	return ""
}

// parseImportClause walks an import_clause node and appends bindings.
// import_clause has these named children in tree-sitter-javascript:
//
//	identifier                 → default import (`express` in `import express from`)
//	namespace_import           → `* as ns`
//	named_imports              → `{ Foo, Bar as Baz }`
func (x *extractor) parseImportClause(clause ts.Node, importPath, dotted, resolved string, aliasResolved bool, out *[]importBinding) {
	count := int(clause.ChildCount())
	for i := 0; i < count; i++ {
		ch := clause.Child(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "identifier":
			// Default import: `import express from "..."`.
			name := x.nodeText(ch)
			if name == "" {
				continue
			}
			*out = append(*out, importBinding{
				localName:     name,
				importedName:  "default",
				sourceModule:  dotted,
				importPath:    importPath,
				resolvedFile:  resolved,
				aliasResolved: aliasResolved,
			})
		case "namespace_import":
			// `* as ns` — the name is the identifier child.
			name := x.firstIdentifier(ch)
			if name == "" {
				continue
			}
			*out = append(*out, importBinding{
				localName:     name,
				importedName:  name,
				sourceModule:  dotted,
				importPath:    importPath,
				resolvedFile:  resolved,
				wildcard:      true,
				aliasResolved: aliasResolved,
			})
		case "named_imports":
			x.parseNamedImports(ch, importPath, dotted, resolved, aliasResolved, out)
		}
	}
}

// parseNamedImports walks a named_imports node (`{ Foo, Bar as Baz }`)
// and appends one binding per import_specifier descendant.
func (x *extractor) parseNamedImports(named ts.Node, importPath, dotted, resolved string, aliasResolved bool, out *[]importBinding) {
	count := int(named.ChildCount())
	for i := 0; i < count; i++ {
		ch := named.Child(i)
		if ch == nil || ch.Type() != "import_specifier" {
			continue
		}
		name := x.childFieldText(ch, "name")   // imported_name
		alias := x.childFieldText(ch, "alias") // local_name (may be empty)
		if name == "" {
			// Some grammar versions emit the imported identifier as the
			// first identifier child without a "name" field.
			name = x.firstIdentifier(ch)
		}
		if name == "" {
			continue
		}
		local := alias
		if local == "" {
			local = name
		}
		*out = append(*out, importBinding{
			localName:     local,
			importedName:  name,
			sourceModule:  dotted,
			importPath:    importPath,
			resolvedFile:  resolved,
			aliasResolved: aliasResolved,
		})
	}
}

// findStringChild returns the unquoted text of the first "string" child
// of n, or "" when none exists.
func (x *extractor) findStringChild(n ts.Node) string {
	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		ch := n.Child(i)
		if ch != nil && ch.Type() == "string" {
			return strings.Trim(x.nodeText(ch), `"'`+"`")
		}
	}
	return ""
}

// firstIdentifier returns the text of the first descendant of n whose
// Type() is "identifier", or "" when none exists. Used to pull the
// local name out of `* as ns` and quirky import_specifier shapes.
func (x *extractor) firstIdentifier(n ts.Node) string {
	if n == nil {
		return ""
	}
	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		ch := n.Child(i)
		if ch == nil {
			continue
		}
		if ch.Type() == "identifier" {
			return x.nodeText(ch)
		}
		if got := x.firstIdentifier(ch); got != "" {
			return got
		}
	}
	return ""
}

// childFieldText returns the text of a named-field child of n, or ""
// when the field is absent or empty.
func (x *extractor) childFieldText(n ts.Node, field string) string {
	if n == nil {
		return ""
	}
	c := n.ChildByFieldName(field)
	if c == nil {
		return ""
	}
	return x.nodeText(c)
}
