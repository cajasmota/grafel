// Template-pattern catalog substrate (#2774 Phase 3D).
//
// Per-language sniffers lift template-shaped string literals from source.
// Three template families are recognised:
//
//   - TemplateKindI18n   : translation keys (t("..."), gettext("..."),
//     i18n.t("..."), trans("..."), _("..."))
//   - TemplateKindLog    : log-format primitives (console.log("..."),
//     logger.<level>("..."), log.<level>("...")
//     where the literal contains a format token
//     like "%s", "%d", "{}", "{name}").
//   - TemplateKindSQL    : SQL template literals — string literals whose
//     content starts with a SQL verb keyword
//     (SELECT/INSERT/UPDATE/DELETE/WITH).
//
// The generic catalog pass in internal/links/template_pattern_pass.go
// stores every match as a TemplatePattern record in the sidecar JSON
// surfaced by archigraph_template_patterns. Overlaps with Phase 2B
// (taint-flow SQL injection) are intentional: that pass tracks data
// flow into the literal; this pass catalogues the literal itself.
//
// Design mirrors Phase 0/1A: per-language sniffers are pure functions
// over file content, stateless and deterministic.
package substrate

import "sort"

// TemplateKind labels the family of a template-pattern match.
type TemplateKind string

const (
	TemplateKindI18n TemplateKind = "i18n"
	TemplateKindLog  TemplateKind = "log_format"
	TemplateKindSQL  TemplateKind = "sql"
)

// TemplatePattern is one lifted template-pattern match.
type TemplatePattern struct {
	// Function is the declaring function/method the literal occurs in.
	// Empty when at module scope; the pass attributes those to the file.
	Function string
	// Line is the 1-indexed source line of the literal.
	Line int
	// Kind is the template family.
	Kind TemplateKind
	// Literal is the raw literal content (quote characters stripped).
	// Bounded at maxLiteralLength to keep the sidecar focused.
	Literal string
	// Tag is a short identifier of the recogniser that matched
	// (e.g. "t()", "console.log", "logger.info", "select").
	Tag string
}

// maxLiteralLength bounds the per-match literal payload — longer ones
// truncate with an ellipsis so the sidecar JSON stays a manageable size.
const maxLiteralLength = 240

// TruncateLiteral returns s truncated to maxLiteralLength with a "..."
// suffix when truncation happened. Exposed so per-language sniffers can
// share the same bound without re-implementing it.
func TruncateLiteral(s string) string {
	if len(s) <= maxLiteralLength {
		return s
	}
	return s[:maxLiteralLength] + "..."
}

// TemplatePatternSnifferFn is the contract for per-language template
// sniffers. Deterministic; nil-safe on empty content.
type TemplatePatternSnifferFn func(content string) []TemplatePattern

var templateRegistry = map[string]TemplatePatternSnifferFn{}

// RegisterTemplatePatternSniffer installs a per-language template sniffer.
func RegisterTemplatePatternSniffer(lang string, fn TemplatePatternSnifferFn) {
	if lang == "" || fn == nil {
		return
	}
	templateRegistry[lang] = fn
}

// TemplatePatternSnifferFor returns the registered sniffer for lang, or nil.
func TemplatePatternSnifferFor(lang string) TemplatePatternSnifferFn {
	return templateRegistry[lang]
}

// TemplatePatternLanguages returns the slugs of every registered sniffer.
func TemplatePatternLanguages() []string {
	out := make([]string, 0, len(templateRegistry))
	for k := range templateRegistry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
