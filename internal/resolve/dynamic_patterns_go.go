package resolve

import "regexp"

// goDynamicPatterns is the per-language dynamic-dispatch pattern catalog for
// Go. Matches here tag a stub as DispositionDynamic.
//
// See the per-language catalog overview comment in refs.go for the design
// rationale behind the language-gated approach (Refs #44).
var goDynamicPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^reflect\.`),       // reflect.* (Call, ValueOf, MethodByName, ...)
	regexp.MustCompile(`\.MethodByName\(`), // v.MethodByName("X").Call(...)
	regexp.MustCompile(`\.FieldByName\(`),  // v.FieldByName("X")
	regexp.MustCompile(`^plugin\.Open\(`),  // Go plugin loader
	// Anchored: only `plugin.Lookup(` (or `<x>.plugin.Lookup(`) — bare
	// `repo.Lookup(id)` / `cache.Lookup(...)` are NOT reflection.
	regexp.MustCompile(`\bplugin\.Lookup\(`),
}

func init() {
	dynamicPatternsByLang["go"] = goDynamicPatterns
}
