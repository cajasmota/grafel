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

// BuildComponentStructuralRef returns the canonical Format A structural-ref
// for class→nested-class CONTAINS edges. Shape:
//
//	scope:component:class:<lang>:<file>:<name>
//
// where <file> is forward-slash normalized via filepath.ToSlash. Used by the
// Java extractor to emit CONTAINS edges from a class entity to synthesized
// builder / companion classes (e.g. Lombok @Builder emits an `OrderBuilder`
// class). Giving each synthesized SCOPE.Component at least one inbound edge
// prevents it from counting as an orphan in the graph.
//
// Counterpart of BuildOperationStructuralRef for SCOPE.Operation entities.
func BuildComponentStructuralRef(lang, filePath, name string) string {
	return "scope:component:class:" + lang + ":" + filepath.ToSlash(filePath) + ":" + name
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

// BuildSchemaFieldStructuralRef returns a Format A structural-ref for
// class→field CONTAINS edges. Shape:
//
//	scope:schema:field:<lang>:<file>:<Class>.<attr>
//
// The trailing name segment exactly matches the SCOPE.Schema/field entity's
// Name ("<dottedClass>.<attr>") and the SourceFile is the class's file, so
// the resolver's byLocation fallback in internal/resolve/refs.go binds the
// stub to the field entity ID without needing a kind-family widening for
// scope="schema" (structuralKindFamilies returns nil for "schema").
//
// Counterpart of BuildOperationStructuralRef for class→method CONTAINS:
// both emit a stub keyed on the parent class's file path so two classes in
// different files declaring an attribute of the same bare name produce
// distinct stubs that resolve to distinct entities.
//
// Used by the Python extractor to close the SCOPE.Schema/field orphan gap:
// extractClassFields previously emitted field entities with no inbound or
// outbound edges (see #526 deferred-emission comment in
// internal/extractors/python/extractor.go), leaving them as the single
// largest residual orphan class (~56% on django-realworld). The stub form
// avoids the ComputeID circularity that motivated the original deferral
// (the resolver fills the hex ID in after buildDocument runs).
func BuildSchemaFieldStructuralRef(lang, filePath, name string) string {
	return "scope:schema:field:" + lang + ":" + filepath.ToSlash(filePath) + ":" + name
}
