// Package deprecation implements the cross-language deprecation annotation extractor.
//
// Scans source files for deprecation annotations and emits
// SCOPE.Pattern entities (subtype = language tag, properties.pattern_kind =
// "deprecation_annotation") with deprecated=true in their properties.
//
// MX-1094: SCOPE.DeprecationAnnotation is not in the 14-type SCOPE
// allowlist; deprecation markers are mapped to SCOPE.Pattern (the canonical
// bucket for inferred coding patterns) and the original semantic is preserved
// on properties.pattern_kind for downstream filtering. Java microprofile and
// quarkus extractors already use SCOPE.Pattern for similar annotation-style
// emissions, so this is consistent with prior art.
//
// Supported patterns:
//   - Java:       @Deprecated annotation
//   - JavaScript / TypeScript: /** @deprecated <message> */ JSDoc tag
//   - Rust:       #[deprecated] or #[deprecated(since="…", note="…")]
//   - C#:         [Obsolete] or [Obsolete("message")]
//   - Python:     warnings.warn("…deprecated…")
//   - Elixir:     @deprecated "message"
//
// Entity kind: "SCOPE.Pattern" (with subtype = language tag)
// No relationships emitted — entity is self-contained.
//
// OTel span: indexer.deprecation_extractor.extract
// Attributes: file_path, language, deprecations_found
//
// Registration key: "_cross_deprecation"
package deprecation

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("_cross_deprecation", &Extractor{})
}

// Extractor detects deprecation annotations across all supported languages.
type Extractor struct{}

// Language returns the registration key.
func (e *Extractor) Language() string { return "_cross_deprecation" }

// ---------------------------------------------------------------------------
// Compiled regular expressions
// ---------------------------------------------------------------------------

// Java: @Deprecated (standalone annotation)
var javaDeprecatedRE = regexp.MustCompile(`@Deprecated\b`)

// JavaScript / TypeScript: JSDoc @deprecated tag
// Captures optional message after @deprecated up to end-of-comment or end-of-line.
// Uses DOTALL to match multi-line JSDoc blocks.
var jsJSDocDeprecatedRE = regexp.MustCompile(`(?s)/\*\*.*?@deprecated([^\n*/]{0,200}).*?\*/`)

// Rust: #[deprecated] or #[deprecated(since="…", note="…")]
var rustDeprecatedRE = regexp.MustCompile(`#\[deprecated([^\]]{0,300})?\]`)

// Rust note= attribute extractor
var rustNoteRE = regexp.MustCompile(`note\s*=\s*"([^"]{0,200})"`)

// C#: [Obsolete] or [Obsolete("message")]
var csharpObsoleteRE = regexp.MustCompile(`\[Obsolete(?:\s*\(\s*"([^"]{0,300})"[^)]{0,100}\))?\s*\]`)

// Python: warnings.warn("…deprecated…")
// Two variants: double-quote and single-quote string arguments.
// Go's RE2 does not support backreferences, so we use two separate patterns.
var pyWarnDoubleRE = regexp.MustCompile(`(?i)warnings\.warn\s*\(\s*"([^"]{0,300})"`)
var pyWarnSingleRE = regexp.MustCompile(`(?i)warnings\.warn\s*\(\s*'([^']{0,300})'`)

// Elixir: @deprecated "message"
var elixirDeprecatedRE = regexp.MustCompile(`@deprecated\s+"([^"]{0,300})"`)

// ---------------------------------------------------------------------------
// Language normalisation
// ---------------------------------------------------------------------------

// langAliases maps caller-supplied language names to canonical internal tags.
var langAliases = map[string]string{
	"typescript":            "javascript",
	"javascript_typescript": "javascript",
	"kotlin":                "java",
}

func normaliseLanguage(language string) string {
	low := strings.ToLower(language)
	if alias, ok := langAliases[low]; ok {
		return alias
	}
	return low
}

// ---------------------------------------------------------------------------
// Ref builder
// ---------------------------------------------------------------------------

func deprecationRef(filePath string, line int) string {
	return fmt.Sprintf("deprecation:%s:%d", filePath, line)
}

// ---------------------------------------------------------------------------
// Line number helper
// ---------------------------------------------------------------------------

// lineOf returns the 1-based line number of the byte at offset pos in source.
func lineOf(source string, pos int) int {
	return strings.Count(source[:pos], "\n") + 1
}

// ---------------------------------------------------------------------------
// Per-language extractors
// ---------------------------------------------------------------------------

func extractJava(source, filePath string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, loc := range javaDeprecatedRE.FindAllStringIndex(source, -1) {
		line := lineOf(source, loc[0])
		out = append(out, types.EntityRecord{
			Name:       "@Deprecated",
			Kind:       "SCOPE.Pattern", // MX-1094: was SCOPE.DeprecationAnnotation
			Subtype:    "java",
			SourceFile: filePath,
			StartLine:  line,
			EndLine:    line,
			Language:   "java",
			Properties: map[string]string{
				// MX-1094: pattern_kind preserves the original DeprecationAnnotation
				// semantic after we collapsed Kind into the SCOPE.Pattern bucket.
				"pattern_kind":        "deprecation_annotation",
				"deprecated":          "true",
				"deprecation_message": "",
				"language":            "java",
				"annotation":          "@Deprecated",
				"provenance":          "INFERRED_FROM_DEPRECATION_ANNOTATION",
				"ref":                 deprecationRef(filePath, line),
			},
			QualityScore: 0.9,
		})
	}
	return out
}

func extractJSTS(source, filePath, langTag string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, m := range jsJSDocDeprecatedRE.FindAllStringSubmatchIndex(source, -1) {
		line := lineOf(source, m[0])
		msg := ""
		if m[2] >= 0 && m[3] >= 0 {
			msg = strings.TrimSpace(strings.Trim(source[m[2]:m[3]], " \t*"))
		}
		out = append(out, types.EntityRecord{
			Name:       "@deprecated",
			Kind:       "SCOPE.Pattern", // MX-1094: was SCOPE.DeprecationAnnotation
			Subtype:    langTag,
			SourceFile: filePath,
			StartLine:  line,
			EndLine:    line,
			Language:   langTag,
			Properties: map[string]string{
				// MX-1094: pattern_kind preserves the original DeprecationAnnotation
				// semantic after we collapsed Kind into the SCOPE.Pattern bucket.
				"pattern_kind":        "deprecation_annotation",
				"deprecated":          "true",
				"deprecation_message": msg,
				"language":            langTag,
				"annotation":          "@deprecated",
				"provenance":          "INFERRED_FROM_DEPRECATION_ANNOTATION",
				"ref":                 deprecationRef(filePath, line),
			},
			QualityScore: 0.9,
		})
	}
	return out
}

func extractRust(source, filePath string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, m := range rustDeprecatedRE.FindAllStringSubmatchIndex(source, -1) {
		line := lineOf(source, m[0])
		attrs := ""
		if m[2] >= 0 && m[3] >= 0 {
			attrs = source[m[2]:m[3]]
		}
		msg := ""
		if nm := rustNoteRE.FindStringSubmatch(attrs); nm != nil {
			msg = nm[1]
		}
		annotation := source[m[0]:m[1]]
		out = append(out, types.EntityRecord{
			Name:       "#[deprecated]",
			Kind:       "SCOPE.Pattern", // MX-1094: was SCOPE.DeprecationAnnotation
			Subtype:    "rust",
			SourceFile: filePath,
			StartLine:  line,
			EndLine:    line,
			Language:   "rust",
			Properties: map[string]string{
				// MX-1094: pattern_kind preserves the original DeprecationAnnotation
				// semantic after we collapsed Kind into the SCOPE.Pattern bucket.
				"pattern_kind":        "deprecation_annotation",
				"deprecated":          "true",
				"deprecation_message": msg,
				"language":            "rust",
				"annotation":          annotation,
				"provenance":          "INFERRED_FROM_DEPRECATION_ANNOTATION",
				"ref":                 deprecationRef(filePath, line),
			},
			QualityScore: 0.9,
		})
	}
	return out
}

func extractCSharp(source, filePath string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, m := range csharpObsoleteRE.FindAllStringSubmatchIndex(source, -1) {
		line := lineOf(source, m[0])
		msg := ""
		if m[2] >= 0 && m[3] >= 0 {
			msg = source[m[2]:m[3]]
		}
		out = append(out, types.EntityRecord{
			Name:       "[Obsolete]",
			Kind:       "SCOPE.Pattern", // MX-1094: was SCOPE.DeprecationAnnotation
			Subtype:    "csharp",
			SourceFile: filePath,
			StartLine:  line,
			EndLine:    line,
			Language:   "csharp",
			Properties: map[string]string{
				// MX-1094: pattern_kind preserves the original DeprecationAnnotation
				// semantic after we collapsed Kind into the SCOPE.Pattern bucket.
				"pattern_kind":        "deprecation_annotation",
				"deprecated":          "true",
				"deprecation_message": msg,
				"language":            "csharp",
				"annotation":          "[Obsolete]",
				"provenance":          "INFERRED_FROM_DEPRECATION_ANNOTATION",
				"ref":                 deprecationRef(filePath, line),
			},
			QualityScore: 0.9,
		})
	}
	return out
}

func extractPython(source, filePath string) []types.EntityRecord {
	var out []types.EntityRecord

	// Collect all matches from both single and double-quote patterns.
	type matchEntry struct {
		pos int
		msg string
	}
	var matches []matchEntry

	for _, m := range pyWarnDoubleRE.FindAllStringSubmatchIndex(source, -1) {
		if m[2] >= 0 && m[3] >= 0 {
			matches = append(matches, matchEntry{pos: m[0], msg: source[m[2]:m[3]]})
		}
	}
	for _, m := range pyWarnSingleRE.FindAllStringSubmatchIndex(source, -1) {
		if m[2] >= 0 && m[3] >= 0 {
			matches = append(matches, matchEntry{pos: m[0], msg: source[m[2]:m[3]]})
		}
	}

	for _, me := range matches {
		// Only emit if message contains "deprecated" (case-insensitive).
		if !strings.Contains(strings.ToLower(me.msg), "deprecated") {
			continue
		}
		line := lineOf(source, me.pos)
		out = append(out, types.EntityRecord{
			Name:       "warnings.warn",
			Kind:       "SCOPE.Pattern", // MX-1094: was SCOPE.DeprecationAnnotation
			Subtype:    "python",
			SourceFile: filePath,
			StartLine:  line,
			EndLine:    line,
			Language:   "python",
			Properties: map[string]string{
				// MX-1094: pattern_kind preserves the original DeprecationAnnotation
				// semantic after we collapsed Kind into the SCOPE.Pattern bucket.
				"pattern_kind":        "deprecation_annotation",
				"deprecated":          "true",
				"deprecation_message": me.msg,
				"language":            "python",
				"annotation":          "warnings.warn",
				"provenance":          "INFERRED_FROM_DEPRECATION_ANNOTATION",
				"ref":                 deprecationRef(filePath, line),
			},
			QualityScore: 0.9,
		})
	}
	return out
}

func extractElixir(source, filePath string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, m := range elixirDeprecatedRE.FindAllStringSubmatchIndex(source, -1) {
		line := lineOf(source, m[0])
		msg := ""
		if m[2] >= 0 && m[3] >= 0 {
			msg = source[m[2]:m[3]]
		}
		out = append(out, types.EntityRecord{
			Name:       "@deprecated",
			Kind:       "SCOPE.Pattern", // MX-1094: was SCOPE.DeprecationAnnotation
			Subtype:    "elixir",
			SourceFile: filePath,
			StartLine:  line,
			EndLine:    line,
			Language:   "elixir",
			Properties: map[string]string{
				// MX-1094: pattern_kind preserves the original DeprecationAnnotation
				// semantic after we collapsed Kind into the SCOPE.Pattern bucket.
				"pattern_kind":        "deprecation_annotation",
				"deprecated":          "true",
				"deprecation_message": msg,
				"language":            "elixir",
				"annotation":          "@deprecated",
				"provenance":          "INFERRED_FROM_DEPRECATION_ANNOTATION",
				"ref":                 deprecationRef(filePath, line),
			},
			QualityScore: 0.9,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Dispatch table
// ---------------------------------------------------------------------------

type extractFn func(source, filePath string) []types.EntityRecord

var extractors = map[string]extractFn{
	"java":       extractJava,
	"javascript": func(s, fp string) []types.EntityRecord { return extractJSTS(s, fp, "javascript") },
	"python":     extractPython,
	"csharp":     extractCSharp,
	"rust":       extractRust,
	"elixir":     extractElixir,
}

// ---------------------------------------------------------------------------
// Extract implements extractor.Extractor
// ---------------------------------------------------------------------------

// Extract scans a source file for deprecation annotations.
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("extractor._cross_deprecation")
	ctx, span := tracer.Start(ctx, "indexer.deprecation_extractor.extract")
	defer span.End()
	_ = ctx

	span.SetAttributes(
		attribute.String("file_path", file.Path),
		attribute.String("language", file.Language),
	)

	langTag := normaliseLanguage(file.Language)

	// TypeScript uses the JavaScript extractor but retains its language tag.
	var entities []types.EntityRecord
	if langTag == "javascript" {
		originalLang := strings.ToLower(file.Language)
		label := "javascript"
		if originalLang == "typescript" || originalLang == "javascript_typescript" {
			label = "typescript"
		}
		entities = extractJSTS(string(file.Content), file.Path, label)
	} else if fn, ok := extractors[langTag]; ok {
		entities = fn(string(file.Content), file.Path)
	}

	span.SetAttributes(attribute.Int("deprecations_found", len(entities)))
	return entities, nil
}
