package audit

import (
	"strings"
	"unicode"

	"github.com/cajasmota/archigraph/internal/graph"
)

// ClassifyOrphan buckets a single orphan entity by root cause. The order of
// the checks matters: more specific patterns (import-placeholder,
// framework-synthetic) are tested before the broader fallbacks so an entity
// matched by two heuristics ends up in the most actionable bucket.
//
// The rules below mirror the manual analysis verbatim:
//   1. SCOPE.Component / subtype "import"            -> import_placeholder
//   2. Operation/Component + subtype "const_*"       -> const_no_references
//   3. PascalCase / camelCase exported-looking name  -> cross_file_export
//   4. Synthetic framework prefixes (manifest:/...)  -> framework_synthetic
//   5. Class / Function / Method / Interface         -> real_construct_bug
//   6. anything else                                 -> misc
func ClassifyOrphan(e *graph.Entity) OrphanCause {
	kind := e.Kind
	st := e.Subtype
	lkind := strings.ToLower(kind)
	lst := strings.ToLower(st)

	// (1) Import placeholders: the SCOPE.Component/import family the manual
	// analysis identified as the single biggest orphan bucket on TS/JS fixtures.
	if (kind == "SCOPE.Component" || lkind == "scope.component") && lst == "import" {
		return CauseImportPlaceholder
	}
	if lst == "import" && (strings.Contains(lkind, "component") || strings.Contains(lkind, "scope")) {
		return CauseImportPlaceholder
	}

	// (4) Synthetic framework / manifest / HTTP / DB-map nodes. Detect by
	// kind prefix — these are emitted by per-framework passes and never
	// participate in code-level edges, so being orphan is expected and not a
	// bug. We check this BEFORE the real_construct fallback so an
	// "http_endpoint" entity (which technically is a Method-like construct)
	// doesn't get flagged as a real bug.
	for _, p := range []string{"manifest:", "hierarchy:", "http_endpoint", "dbmap:", "openapi:", "framework:"} {
		if strings.HasPrefix(lkind, p) {
			return CauseFrameworkSynth
		}
	}

	// (2) const_* family on Operation/Component kinds: in TS/JS the extractor
	// emits `const X = ...` as an Operation/const_call (and many variants).
	// When nothing in the same file or any other file references it, it lands
	// here. The manual analysis tied these directly to the missing-REFERENCES
	// finding.
	if (strings.Contains(lkind, "operation") || strings.Contains(lkind, "component")) &&
		strings.Contains(lst, "const") {
		return CauseConstNoReferences
	}

	// (3) Cross-file export candidates: a non-const-family entity whose name
	// looks like a PascalCase type or camelCase function. This is the
	// "exported for consumers" bucket from the manual analysis. We require
	// the name to start with a letter and contain at least one upper/lower
	// transition, which filters out single-token lowercase noise.
	if isExportishName(e.Name) {
		// Functions, methods, classes, interfaces are reported separately
		// below as "real construct" — only count the misc-but-named entities
		// here as export candidates. Otherwise every orphan function falls
		// into both buckets.
		if !isCoreConstructKind(lkind) {
			return CauseCrossFileExport
		}
	}

	// (5) Real-construct bug: a Class/Function/Method/Interface entity that
	// has zero edges. These are the orphans that genuinely indicate an
	// extractor or resolver bug and most warrant follow-up.
	if isCoreConstructKind(lkind) {
		return CauseRealConstructBug
	}

	return CauseMisc
}

// isCoreConstructKind returns true for the kinds that should virtually never
// be orphan in healthy graphs: language-level construct definitions.
func isCoreConstructKind(lkind string) bool {
	for _, k := range []string{"class", "function", "method", "interface", "struct", "trait", "enum"} {
		if strings.Contains(lkind, k) {
			return true
		}
	}
	return false
}

// isExportishName returns true when the name has the shape of a Pascal- or
// camelCase identifier (common heuristic for cross-file exports). We
// deliberately accept both because, e.g., React component names are Pascal
// while hook / util exports are camel.
func isExportishName(name string) bool {
	if name == "" {
		return false
	}
	r0 := rune(name[0])
	if !unicode.IsLetter(r0) {
		return false
	}
	if r0 == '_' {
		return false
	}
	hasLower := false
	hasUpper := false
	for _, r := range name {
		if unicode.IsUpper(r) {
			hasUpper = true
		}
		if unicode.IsLower(r) {
			hasLower = true
		}
	}
	// Need both — pure UPPER_SNAKE constants and pure lower tokens don't
	// look like exported types or functions to a human reader.
	return hasUpper && hasLower
}
