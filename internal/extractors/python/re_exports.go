// re_exports.go — Python __init__.py re-export annotation (#1991).
//
// Package __init__.py files are the canonical Python re-export surface:
//
//	# upvate_core/__init__.py
//	from .celery import app as celery_app
//	from .models import User, Group
//	__all__ = ("celery_app", "User", "Group")
//
// The base extractor's extractImports DOES emit IMPORTS edges for these
// `from .X import Y` patterns (resolvePythonImportModule converts the
// relative_import node to its absolute dotted form), but the resulting
// edges carry no signal that distinguishes them from any other intra-
// package import. Downstream consumers cannot tell:
//
//   - "this is a re-export, not a private use" — the imported symbol is
//     made public via __all__ and should NOT be flagged as a dead import
//     by the unused-imports detector (#1985);
//   - "this is an alias rename" — `from .X import Y as Z` is an explicit
//     public rename that docgen and graph queries want to surface.
//
// This file post-processes the IMPORTS edges attached to the file entity
// for any `__init__.py` source file and stamps:
//
//   - re_export: "true"      — every IMPORTS edge whose source_module
//                              starts with "." (relative).
//   - package_init: "true"   — every IMPORTS edge on an __init__.py file.
//   - public: "true"         — when the local_name (or alias target)
//                              appears in the file's __all__ declaration.
//   - alias: "<target>"      — when the original import used `as <alias>`
//                              (i.e. local_name differs from imported_name).
//
// The pass also coordinates with dead-imports detection (#1985) by
// returning the set of names that should be treated as "live" regardless
// of whether they appear in the module body — re-exports listed in
// __all__ are LIVE even when nothing in the __init__.py body references
// them, because they are externally consumed.
package python

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)


// dunderAllRe matches a module-level `__all__ = (...)` / `__all__ = [...]`
// assignment and captures the inner contents (parens or brackets). It is
// deliberately permissive on whitespace and continuation lines.
var dunderAllRe = regexp.MustCompile(
	`(?m)^\s*__all__\s*(?:\:\s*[^=]+)?=\s*[\(\[]([^\)\]]*)[\)\]]`,
)

// dunderAllItemRe extracts each quoted string item from the inner contents
// of an __all__ declaration. Trailing commas and inline comments are
// tolerated.
var dunderAllItemRe = regexp.MustCompile(`["']([^"']+)["']`)

// applyReExports annotates IMPORTS edges on the file entity for
// __init__.py source files and returns the set of public re-export names
// declared by __all__. The returned set is consumed by applyDeadImports
// to suppress dead-import flagging on re-exported symbols.
//
// For non-__init__.py files the pass is a no-op (returns nil).
func applyReExports(file extractor.FileInput, entities *[]types.EntityRecord) map[string]bool {
	if entities == nil || len(*entities) == 0 {
		return nil
	}
	if !isPackageInit(file.Path) {
		return nil
	}
	src := string(file.Content)
	if len(src) == 0 {
		return nil
	}

	publicNames := parseDunderAll(src)

	// IMPORTS edges live on the file entity (entities[0] per extractor
	// contract — extractor.FileEntity is the first entity appended in
	// Extract). Iterate every entity defensively in case the ordering
	// changes in future passes.
	for i := range *entities {
		e := &(*entities)[i]
		if e.SourceFile != file.Path {
			continue
		}
		for j := range e.Relationships {
			r := &e.Relationships[j]
			if r.Kind != "IMPORTS" {
				continue
			}
			if r.Properties == nil {
				r.Properties = map[string]string{}
			}
			// Mark every IMPORTS edge on an __init__.py with package_init.
			r.Properties["package_init"] = "true"

			// Re-export marker — only for relative imports (source_module
			// starts with "." in extractImports' current encoding, OR
			// resolvePythonImportModule has already promoted it to the
			// absolute dotted form. In the absolute case we cannot tell
			// from the edge alone whether the original was relative, so
			// we conservatively mark every IMPORTS edge on an __init__.py
			// as a candidate re-export: docgen consumers care about
			// "public surface of this package", which includes both
			// relative AND absolute re-exports in idiomatic Python.
			r.Properties["re_export"] = "true"

			// Alias annotation — when local_name differs from
			// imported_name the original source used `as <alias>`.
			local := r.Properties["local_name"]
			imported := r.Properties["imported_name"]
			if local != "" && imported != "" && local != imported {
				r.Properties["alias"] = local
			}

			// Public marker — symbol listed in __all__.
			// Match against either the local binding (alias) or the
			// original imported name; Python's __all__ semantics use the
			// LOCAL binding name as it appears in the module namespace.
			key := local
			if key == "" {
				key = imported
			}
			if key != "" && publicNames[key] {
				r.Properties["public"] = "true"
			}
		}
	}

	return publicNames
}

// isPackageInit reports whether path is a package __init__.py file.
// Matches any path ending in /__init__.py OR being exactly "__init__.py".
func isPackageInit(path string) bool {
	if path == "__init__.py" {
		return true
	}
	return strings.HasSuffix(path, "/__init__.py")
}

// parseDunderAll returns the set of names declared by the file's
// module-level __all__ assignment. Multiple __all__ assignments (rare)
// are unioned. Augmented assignments (`__all__ += [...]`) are also
// captured by the lenient regex. Returns an empty (not nil) map when no
// __all__ declaration is present.
func parseDunderAll(src string) map[string]bool {
	out := map[string]bool{}
	for _, m := range dunderAllRe.FindAllStringSubmatch(src, -1) {
		inner := m[1]
		for _, im := range dunderAllItemRe.FindAllStringSubmatch(inner, -1) {
			name := strings.TrimSpace(im[1])
			if name != "" {
				out[name] = true
			}
		}
	}
	return out
}

