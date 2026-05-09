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
