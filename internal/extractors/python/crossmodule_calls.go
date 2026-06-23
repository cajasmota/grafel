// crossmodule_calls.go — cross-module CALL-target resolution for the Python
// extractor (issue #1694).
//
// Background
// ----------
// The base extractor's extractCallRelationships emits one CALLS edge per
// `call` node in a function body. For attribute calls (`recv.leaf(...)`) the
// `callTarget` resolver only qualifies the target when the receiver can be
// statically typed (`self`, `cls`, `ClassName()`, or PEP-8 PascalCase
// identifiers). Every other shape — including the dominant cross-module
// idioms:
//
//	import x;            x.fn(...)
//	from x import y;     y.fn(...)
//	from . import steps; steps.create_order(...)
//
// — collapses to the bare leaf name (`fn` / `create_order`) with the
// `disposition_hint: ambiguous` property. The import-aware CALLS rewrite
// (ResolveImports in internal/resolve/imports.go) can only bind a bare-name
// CALLS target via its file-level import bucket; when the bucket has no
// binding for the bare leaf (because the leaf is a *method on a module*,
// not an imported symbol itself), the edge stays orphan.
//
// The orchestrator/saga pattern is the canonical failure case: a
// `PlaceOrderSaga.run` method that calls `steps.create_order(...)` etc.
// emits ZERO CALLS edges to the four step Operations, blocking every saga
// / orchestrator question on polyglot fixtures.
//
// Fix
// ---
// 1. Pre-scan top-level imports into a per-file map from local_name to
//    (source_module, imported_name). Relative imports (`from . import X`,
//    `from ..foo import bar`) are resolved to their absolute dotted module
//    form using the current file's path so the resolver's `(module, leaf)`
//    reverse index can bind them.
//
// 2. When the call extractor sees `recv.leaf(...)` where `recv` is a bare
//    identifier matching a local import binding, it stamps two Properties
//    on the emitted CALLS edge:
//
//        import_alias  = "<recv>"      // the local name in this file
//        call_leaf     = "<leaf>"      // the called member name
//
//    The ToID itself stays as the bare leaf name so the existing
//    `ContainsAny(":.#")` skip in ResolveImports doesn't drop the edge
//    before our new resolution path runs.
//
// 3. The resolver's ResolveCrossModuleCallTarget (this PR, imports.go)
//    consumes those properties: it looks up the alias in the file's
//    import bucket and tries the appropriate `(module, leaf)` tuple
//    against `lookupModuleEntity`. For `import x` shape the module is
//    `b.SourceModule`; for `from x import y` shape (where y is itself a
//    submodule) the module is `b.SourceModule + "." + b.ImportedName`.
//
// Local-call behaviour (same-module function references like a top-level
// `foo()` from inside another top-level function in the same file) is
// untouched — those are bare-identifier calls with no `attribute` parent
// and never enter this code path.

package python

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
)

// pythonImportBinding mirrors the (source_module, imported_name) pair the
// resolver's ImportTable carries per local name. It is intentionally a
// trimmed copy so the extractor package stays decoupled from
// internal/resolve.
type pythonImportBinding struct {
	sourceModule string
	importedName string
	// plainModule is true when the binding came from `import x` rather than
	// `from x import y`. Detected at build time so callers don't have to
	// re-check `sourceModule == importedName`.
	plainModule bool
}

// pythonImportMap is the per-file local_name → binding map consumed by
// extractCallRelationships when qualifying attribute-call targets.
type pythonImportMap map[string]pythonImportBinding

// buildPythonImportMap walks the same top-level import_statement /
// import_from_statement nodes that extractImports walks and produces a
// local_name → binding lookup table. Wildcard imports and `*` shapes are
// skipped — they bind no specific local name and the bare-name resolver's
// wildcard branch already handles them.
//
// Relative source modules (`.`, `..foo`, `...x.y`) are resolved against
// the current file's dotted module form so the resolver can bind them
// without further normalisation.
//
// The map is intentionally small and per-file — it is rebuilt for every
// extracted file and never escapes the Extract call frame.
func buildPythonImportMap(root ts.Node, file extractor.FileInput) pythonImportMap {
	if root == nil {
		return nil
	}
	out := make(pythonImportMap)

	// `import a, b.c [as alias]` — direct module imports. local_name is the
	// alias when present, else the top-level package segment of the dotted
	// path. source_module == imported_name (the full dotted path).
	for _, n := range findAll(root, "import_statement") {
		for i := 0; i < int(n.NamedChildCount()); i++ {
			ch := n.NamedChild(i)
			path, alias := dottedNameAndAlias(ch, file.Content)
			if path == "" || path == "*" {
				continue
			}
			localName := alias
			if localName == "" {
				if dot := strings.IndexByte(path, '.'); dot > 0 {
					localName = path[:dot]
				} else {
					localName = path
				}
			}
			if _, exists := out[localName]; exists {
				continue // first-binding-wins, matches extractImports
			}
			out[localName] = pythonImportBinding{
				sourceModule: path,
				importedName: path,
				plainModule:  true,
			}
		}
	}

	// `from x import a, b [as alias]` — symbol imports. Relative source
	// modules are resolved against the current file's dotted form.
	for _, n := range findAll(root, "import_from_statement") {
		modNode := n.ChildByFieldName("module_name")
		modPath := resolvePythonImportModule(modNode, file)
		if modPath == "" {
			continue
		}
		for i := 0; i < int(n.NamedChildCount()); i++ {
			ch := n.NamedChild(i)
			if ch == modNode {
				continue
			}
			name, alias := dottedNameAndAlias(ch, file.Content)
			if name == "" || name == "*" {
				continue
			}
			localName := alias
			if localName == "" {
				localName = name
			}
			if _, exists := out[localName]; exists {
				continue
			}
			out[localName] = pythonImportBinding{
				sourceModule: modPath,
				importedName: name,
				plainModule:  false,
			}
		}
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

// resolvePythonImportModule returns the dotted module path for an
// import_from_statement's module_name field. For a non-relative
// `from x.y import ...` it returns the literal "x.y". For a relative
// import (`from . import ...` / `from ..foo import bar`) it climbs the
// current file's module path by the count of leading dots and appends any
// trailing dotted_name to produce an absolute dotted form.
//
// Returns "" when modNode is nil, when the parse cannot produce a path,
// or when the relative climb would escape the file's package (more dots
// than parent segments) — in which case the caller leaves the binding
// out of the map rather than emit a malformed key.
func resolvePythonImportModule(modNode ts.Node, file extractor.FileInput) string {
	if modNode == nil {
		return ""
	}
	if modNode.Type() != "relative_import" {
		// `from x.y import ...` — already absolute, reuse the existing
		// path-flattener so behaviour matches extractImports exactly.
		return dottedNamePath(modNode, file.Content)
	}

	// Count the leading dots and capture any trailing dotted_name suffix.
	dots := 0
	var suffix string
	for i := 0; i < int(modNode.ChildCount()); i++ {
		ch := modNode.Child(i)
		switch ch.Type() {
		case "import_prefix":
			// Each "." child is one level up.
			for j := 0; j < int(ch.ChildCount()); j++ {
				if ch.Child(j).Type() == "." {
					dots++
				}
			}
		case "dotted_name":
			suffix = strings.TrimSpace(nodeText(ch, file.Content))
		}
	}
	if dots == 0 {
		// Defensive — relative_import without dots shouldn't happen.
		if suffix != "" {
			return suffix
		}
		return ""
	}

	// Convert the file's path to its dotted package form, then climb.
	mod := filePathToModule(file.Path)
	if mod == "" {
		// File at repo root with no module — relative import has no
		// meaningful absolute form. Leave the binding out.
		return ""
	}
	parts := strings.Split(mod, ".")
	// Python's "from ." semantics: one dot = same package = drop the
	// current file's leaf module. Two dots = parent package = drop two
	// leaves (current leaf + parent package boundary), and so on.
	//
	// Issue #2019 — __init__.py special case: filePathToModule already
	// collapses `pkg/__init__.py` to `pkg` (stripping the `__init__`
	// leaf). For a regular file `pkg/sub/mod.py` the module path is
	// `pkg.sub.mod`, so `from .X` (dots=1) should drop the leaf `mod`
	// and resolve to `pkg.sub.X`. For `pkg/__init__.py` the module path
	// is already `pkg` (no leaf to drop), so `from .X` should resolve to
	// `pkg.X` — i.e. drop zero additional levels.
	//
	// We detect this case by checking whether the original file path ends
	// with `__init__.py`. When it does, one dot of the climb is already
	// accounted for by the filePathToModule collapse, so we reduce `drop`
	// by one to avoid stripping the package root itself.
	drop := dots
	isInitFile := strings.HasSuffix(file.Path, "__init__.py") ||
		strings.HasSuffix(file.Path, "__init__")
	if isInitFile && drop > 0 {
		drop--
	}
	if drop > len(parts) {
		// Escapes the file's known package — leave unresolved rather than
		// guess. The existing bare-name resolver still has the option of
		// binding via wildcard or alias fallbacks.
		return ""
	}
	base := strings.Join(parts[:len(parts)-drop], ".")
	if suffix == "" {
		return base
	}
	if base == "" {
		return suffix
	}
	return base + "." + suffix
}

// extractCallTargetImportAlias returns the (alias, leaf) tuple for an
// attribute-shaped call whose receiver is a bare identifier present in
// `imports`. Returns ("", "") for any shape that isn't `<alias>.<leaf>(...)`
// or whose alias isn't a known import binding.
//
// This is a narrow, conservative check: chained attribute calls like
// `a.b.c()`, subscript receivers, and method calls on non-identifier
// receivers are deliberately not handled here — those are either already
// covered by callTarget's receiver-typing branches or are syntactically
// ambiguous (e.g. `a.b.c()` could be a class method on a submodule member
// or a deeper attribute chain; emitting a guess risks binding edges to the
// wrong entity).
func extractCallTargetImportAlias(
	call ts.Node,
	src []byte,
	imports pythonImportMap,
) (alias, leaf string) {
	if call == nil || imports == nil {
		return "", ""
	}
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "attribute" {
		return "", ""
	}
	recv := fn.ChildByFieldName("object")
	if recv == nil || recv.Type() != "identifier" {
		return "", ""
	}
	attr := fn.ChildByFieldName("attribute")
	if attr == nil {
		return "", ""
	}
	recvName := nodeText(recv, src)
	if _, ok := imports[recvName]; !ok {
		return "", ""
	}
	leafName := nodeText(attr, src)
	if leafName == "" {
		return "", ""
	}
	return recvName, leafName
}
