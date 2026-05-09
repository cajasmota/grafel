// relationships.go — IMPORTS edge emission for the CSS / SCSS / Less
// extractor (issue #383 PORT-RELS-CSS).
//
// The CSS family's only meaningful relationship surface is the @import
// directive, which links one stylesheet to another. We emit one
// SCOPE.Component "import" entity per @import directive, with an embedded
// IMPORTS edge attached to it, so the cross-file resolver can wire the
// importing file to the imported module.
//
// Forms covered:
//
//	plain CSS (tree-sitter)   @import url("foo.css");
//	                          @import "foo.css";
//	                          @import url('foo.css') screen;
//	                          @import "foo.css" print;
//	SCSS (regex)              @import "foo";
//	                          @import "foo", "bar";
//	                          @use   "foo" as f;
//	                          @forward "foo";
//	Less (regex)              @import "foo.less";
//	                          @import (reference) "foo.less";
//	                          @import (less) "foo";
//
// Property contract on every IMPORTS edge (matches issue #383 spec):
//
//	local_name    — basename of the imported module (filename without
//	                directory; not stripped of extension because CSS refs
//	                are typically already bare names).
//	source_module — the raw module path as written in source (the value
//	                between the quotes / inside url()).
//	imported_name — "" — CSS @import does not introduce a named binding.
//
// CALLS: not modelled. CSS has no function calls in the architectural
// sense. var(--x) is a value reference inside a declaration — not an
// edge worth wiring at the entity layer. Pinned with a regression test.
//
// CONTAINS: not modelled. Stylesheets nominally contain rule_sets, but
// rule_sets are themselves the SCOPE.Stylesheet/selector entities and
// there is no parent stylesheet entity to hang them from. Pinned with a
// regression test.

package css

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// buildImportRel constructs the IMPORTS RelationshipRecord attached to an
// @import entity. fromPath is the source file (used as FromID); module is
// the imported module ref as written in source.
func buildImportRel(fromPath, module string) types.RelationshipRecord {
	return types.RelationshipRecord{
		FromID: fromPath,
		ToID:   module,
		Kind:   "IMPORTS",
		Properties: map[string]string{
			"local_name":    importBasename(module),
			"source_module": module,
			"imported_name": "",
		},
	}
}

// importBasename returns the trailing path segment of an @import target.
// Falls back to the full ref if there is no directory separator.
func importBasename(ref string) string {
	s := ref
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	if s == "" {
		return ref
	}
	return s
}
