// Shared Django model field-index helper (issue #2295).
//
// BuildFieldIndex is extracted from the inline indexDjangoModelFields
// function that was defined solely in orm_field_edges.go. Any engine
// pass that needs a "<Model>.<field>" presence index for a Django source
// file should call BuildFieldIndex rather than re-implement the regex
// scan. The key format — `<Model>.<field>` — is byte-identical to the
// Name the Python extractor emits at python/extractor.go:1411-1412, so
// consumers that see both pipelines can match by name without further
// normalisation.
//
// Refs #2295.
package engine

import "regexp"

// djangoClassDeclRe locates Django model class declarations:
//
//	class <Name>(... Model ...):
//
// The base class list is matched loosely (any parenthesised group that
// contains "Model") so subclasses of project-local abstract bases
// (e.g. `class User(TimestampedModel):` where TimestampedModel itself
// inherits from models.Model) are still recognised. False positives
// (a plain class whose parens contain the word "Model" but is not
// actually a Django model) are inert: their fields just won't be the
// target of any ORM filter_keys so no spurious edges are emitted.
var djangoClassDeclRe = regexp.MustCompile(
	`(?m)^class\s+([A-Z][A-Za-z0-9_]*)\s*\(([^)]*Model[^)]*)\)\s*:`,
)

// djangoFieldDeclRe locates field declarations inside a model body:
//
//	    <name> = models.<SomethingField>(...)
//	    <name> = <CustomField>(...)
//
// We accept either the `models.` namespace (stdlib Django fields) or a
// bare `<Capitalised>Field(` call (project-local custom field classes,
// django-money MoneyField, etc.). The leading indentation is required
// so top-level assignments at module scope (e.g. `User = get_user_model()`)
// are NOT treated as field declarations.
var djangoFieldDeclRe = regexp.MustCompile(
	`(?m)^[ \t]+([a-z_][a-zA-Z0-9_]*)\s*=\s*(?:models\.[A-Z]\w*|[A-Z]\w*?Field)\s*\(`,
)

// BuildFieldIndex scans src for Django model class bodies and returns
// the set of "<Model>.<field>" names discovered, mirroring the Name
// convention the Python extractor uses at python/extractor.go:1411.
//
// Strategy: locate each `class X(... Model ...):` header, then scan
// forward to the next class declaration (or EOF) and harvest every
// indented `<name> = models.…Field(…)` or `<name> = <Custom>Field(…)`
// assignment as a field of the enclosing class.
//
// The returned map is a presence set — values are always true. A nil /
// empty map is returned for source files that contain no recognisable
// Django model definitions.
func BuildFieldIndex(src string) map[string]bool {
	out := map[string]bool{}
	classMatches := djangoClassDeclRe.FindAllStringSubmatchIndex(src, -1)
	if len(classMatches) == 0 {
		return out
	}
	for i, m := range classMatches {
		className := src[m[2]:m[3]]
		// Body extends from the end of the matched header to the start
		// of the next class declaration (or EOF for the last class).
		bodyStart := m[1]
		bodyEnd := len(src)
		if i+1 < len(classMatches) {
			bodyEnd = classMatches[i+1][0]
		}
		body := src[bodyStart:bodyEnd]
		for _, fm := range djangoFieldDeclRe.FindAllStringSubmatch(body, -1) {
			if len(fm) < 2 {
				continue
			}
			fieldName := fm[1]
			if fieldName == "" {
				continue
			}
			out[className+"."+fieldName] = true
		}
	}
	return out
}
