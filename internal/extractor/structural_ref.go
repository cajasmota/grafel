package extractor

import "path/filepath"

// BuildOperationStructuralRef returns the canonical Format A structural-ref
// for class→method CONTAINS edges. Shape:
//
//	scope:operation:method:<lang>:<file>:<name>
//
// where <file> is forward-slash normalized via filepath.ToSlash so that
// extractor output is stable across POSIX and Windows callers. This helper
// centralizes the literal that was previously duplicated across every
// language extractor that emits class→method CONTAINS edges (java,
// javascript, kotlin, python, ruby, rust).
func BuildOperationStructuralRef(lang, filePath, name string) string {
	return "scope:operation:method:" + lang + ":" + filepath.ToSlash(filePath) + ":" + name
}

// BuildSchemaColumnStructuralRef returns the canonical Format B structural-ref
// for table→column CONTAINS edges (and column-as-FromID on column REFERENCES
// edges). Shape:
//
//	scope:schema:column:sql:<file>:<table>#<column>
//
// where <file> is forward-slash normalized via filepath.ToSlash. Format B is
// resolved through Index.byMember[<file>][<table>][<column>] in
// internal/resolve/refs.go, which requires the column entity's Name to be
// "<table>.<column>" so the dotted-name split during index build assigns
// scope=<table>, member=<column>.
//
// Issue #141: SQL schema CONTAINS / REFERENCES edges previously emitted bare
// column names (e.g. "name", "id"), which collided cross-language with Java
// method calls and tripped the bug-resolver classifier. Emitting a
// structural-ref qualified by file + table removes the collision class.
func BuildSchemaColumnStructuralRef(filePath, table, column string) string {
	return "scope:schema:column:sql:" + filepath.ToSlash(filePath) + ":" + table + "#" + column
}
