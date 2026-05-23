// dead_imports.go — Dead-import detection for Python (#1985).
//
// The base extractor emits one IMPORTS edge per import statement. Today
// the graph cannot distinguish:
//
//	from drf.permissions import HasPermission        # used in this file
//	from drf.permissions import HasPermission        # legacy import, no usage
//
// Both produce an identical IMPORTS edge, so a query like "find all
// callers of HasPermission" returns false positives from files that
// migrated to a different permission class but left the import line in
// place. Refactoring confidence drops; bug-rate metrics inflate with
// dead links.
//
// This pass:
//
//   1. Collects every imported local binding name from IMPORTS edges
//      attached to the file entity (the local_name property, which
//      defaults to the leaf imported name when no `as <alias>` clause
//      is present — same encoding extractImports uses).
//
//   2. Builds the set of identifiers referenced in the source body
//      EXCLUDING the import statements themselves. We use a simple
//      identifier regex scan over the source minus the import lines;
//      the goal is recall, not precision — false negatives (a name
//      flagged dead but actually used dynamically) are worse than
//      false positives (a dead-flagged name that is in fact dead).
//
//   3. For every IMPORTS edge whose local_name is NOT referenced AND
//      is NOT a re-export (public via __all__ on a package __init__.py
//      — coordinated with applyReExports), stamp:
//
//        live: "false"
//        dead_import: "true"
//
//      The edge KIND remains "IMPORTS" — downstream consumers filter
//      on Properties["live"] rather than splitting the edge into a
//      separate DEAD_IMPORT kind. This preserves graph-walk semantics
//      (existing "all imports" queries still return everything) while
//      letting the resolver and docgen filter live vs dead.
//
// Wildcard imports (`from x import *`) and module-level imports
// (`import x` / `import x.y`) are intentionally NEVER flagged dead:
//   - wildcards bind an unknown set of names whose usage cannot be
//     determined without a full symbol-table resolution;
//   - module-level imports are commonly imported for side-effects
//     (registration / monkeypatching), which is invisible to a
//     name-reference scan.
//
// Coordinates with re_exports.go: re-exports listed in __all__ are
// live by definition. applyReExports returns the publicNames set;
// applyDeadImports unions it with the in-body reference set before
// computing the dead-import flag.
package python

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// identifierRe matches a Python identifier (PEP 3131 ASCII subset). The
// scan is line-oriented and excludes string/comment context only at the
// coarse line level (lines starting with `#`); we accept false positives
// inside docstrings and string literals as the conservative bias is to
// avoid flagging an imported name dead when it might be a string
// reference. Tree-sitter-precise extraction would be more accurate but
// the marginal gain does not justify the parse-traversal cost on every
// file.
var identifierRe = regexp.MustCompile(`\b([A-Za-z_][A-Za-z_0-9]*)\b`)

// importLineRe matches the start of an import statement line so the
// usage scan can skip them. We only need to elide the import lines
// themselves; subsequent lines that happen to reference the imported
// name are real usage and must remain in the scan corpus.
var importLineRe = regexp.MustCompile(`(?m)^\s*(?:from\s+[\w.]+\s+import\s+.+|import\s+[\w.,\s]+)$`)

// applyDeadImports runs the dead-import annotation pass. publicNames is
// the set returned by applyReExports for package __init__.py files (or
// nil for non-package files). Mutates the IMPORTS edge Properties on
// the file entity in place; never removes or rewrites edges.
func applyDeadImports(
	file extractor.FileInput,
	entities *[]types.EntityRecord,
	publicNames map[string]bool,
) {
	if entities == nil || len(*entities) == 0 {
		return
	}
	src := string(file.Content)
	if len(src) == 0 {
		return
	}

	// Collect imported local names from IMPORTS edges (across all entities
	// in the file — IMPORTS edges live on the file entity but we scan
	// defensively in case future passes move them).
	importedLocals := map[string]bool{}
	for i := range *entities {
		e := &(*entities)[i]
		if e.SourceFile != file.Path {
			continue
		}
		for j := range e.Relationships {
			r := &e.Relationships[j]
			if r.Kind != "IMPORTS" || r.Properties == nil {
				continue
			}
			if r.Properties["wildcard"] == "1" {
				continue
			}
			local := r.Properties["local_name"]
			if local == "" {
				continue
			}
			importedLocals[local] = true
		}
	}
	if len(importedLocals) == 0 {
		return
	}

	// Build the set of identifiers referenced in the file body, excluding
	// the import statements themselves.
	bodyCorpus := importLineRe.ReplaceAllString(src, "")
	referenced := map[string]bool{}
	for _, m := range identifierRe.FindAllStringSubmatch(bodyCorpus, -1) {
		referenced[m[1]] = true
	}

	// Union with publicNames so re-exports from package __init__.py
	// (passed through publicNames) are always treated as live.
	if len(publicNames) > 0 {
		for n := range publicNames {
			referenced[n] = true
		}
	}

	// Stamp the dead-import flag on edges whose local binding is
	// unreferenced AND not a wildcard / module-side-effect import.
	for i := range *entities {
		e := &(*entities)[i]
		if e.SourceFile != file.Path {
			continue
		}
		for j := range e.Relationships {
			r := &e.Relationships[j]
			if r.Kind != "IMPORTS" || r.Properties == nil {
				continue
			}
			if r.Properties["wildcard"] == "1" {
				continue
			}
			// Side-effect module imports (`import x` / `import x.y`)
			// where local_name == source_module top segment: skip.
			// We treat these as live regardless — registration imports
			// are common (e.g. `import myapp.signals` to wire receivers).
			local := r.Properties["local_name"]
			source := r.Properties["source_module"]
			if local == "" {
				continue
			}
			if local == source ||
				(source != "" && strings.HasPrefix(source, local+".")) {
				// `import x` (local==source) or `import x.y` (where
				// local_name is set to the top segment "x" by
				// extractImports). Both classes commonly serve as
				// side-effect imports; do not flag dead.
				continue
			}
			// Re-export annotation wins: a re-export is live even when
			// the body never references the symbol (coordinated with
			// applyReExports which sets re_export="true" on __init__.py
			// IMPORTS edges).
			if r.Properties["re_export"] == "true" {
				continue
			}
			if referenced[local] {
				continue
			}
			r.Properties["live"] = "false"
			r.Properties["dead_import"] = "true"
		}
	}
}
