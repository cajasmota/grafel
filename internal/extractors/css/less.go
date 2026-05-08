// less.go — regex-based Less variable and mixin extractor.
//
// Extracted entities (SCOPE.Component per MX-1054 spec):
//   - Less variables   (@name: value)      → Kind="SCOPE.Component", Subtype="variable", Metadata{"kind":"variable","value":"..."}
//   - Less mixins      (.name() { or .name(@param) { → Kind="SCOPE.Component", Subtype="mixin", Metadata{"kind":"mixin","params":[...]}
//
// Less variable syntax (@name: value) conflicts with SCSS @mixin — this extractor
// handles the Less dialect only and is called when file.Path ends in ".less".
//
// OTel: emits span "extractor.less" with attributes less_variable_count,
// less_mixin_count (S11-03 mandatory).
package css

import (
	"bufio"
	"bytes"
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// lessVarRE matches Less variable declarations: @name: value;
// Must NOT match Less at-rules like @media, @import, @charset etc.
// Strategy: require the value to be non-empty and end with ; on the same line.
// Captures: 1=name, 2=value.
var lessVarRE = regexp.MustCompile(`^\s*@([a-zA-Z_-][a-zA-Z0-9_-]*)\s*:\s*(.+?)\s*;`)

// lessMixinRE matches Less class-style mixin definitions:
//
//	.name() { or .name(@param) { or .name(@p1; @p2) {
//
// Captures: 1=name, 2=params string (may be empty).
var lessMixinRE = regexp.MustCompile(`^\s*\.([a-zA-Z_-][a-zA-Z0-9_-]*)\s*\(([^)]*)\)\s*\{`)

// ExtractLess extracts Less entities from raw source and appends them to out.
// It is exported (uppercase) so tests in the same package can call it directly.
func ExtractLess(ctx context.Context, file extractor.FileInput, out *[]types.EntityRecord) (varCount, mixinCount int) {
	tracer := otel.Tracer("extractor.less")

	var span trace.Span
	_, span = tracer.Start(ctx, "extractor.less",
		trace.WithAttributes(
			attribute.String("file_path", file.Path),
			attribute.String("language", "less"),
		),
	)
	defer func() {
		span.SetAttributes(
			attribute.Int("less_variable_count", varCount),
			attribute.Int("less_mixin_count", mixinCount),
		)
		span.End()
	}()

	// lessAtRuleKeywords are Less at-rules that look like variable declarations
	// but are NOT variables. We skip lines whose @name matches any of these.
	// (e.g. @media, @import, @charset, @keyframes, @font-face, @mixin-like)
	lessAtRuleKeywords := map[string]bool{
		"media":         true,
		"import":        true,
		"charset":       true,
		"keyframes":     true,
		"font-face":     true,
		"namespace":     true,
		"supports":      true,
		"page":          true,
		"document":      true,
		"viewport":      true,
		"counter-style": true,
	}

	scanner := bufio.NewScanner(bytes.NewReader(file.Content))
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Skip comment lines.
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
			continue
		}

		// Less mixin definition: .name() {
		if m := lessMixinRE.FindStringSubmatch(line); m != nil {
			name := m[1]
			params := parseLessParams(m[2])
			*out = append(*out, types.EntityRecord{
				Name:       name,
				Kind:       "SCOPE.Component",
				Subtype:    "mixin",
				SourceFile: file.Path,
				Language:   "less",
				StartLine:  lineNum,
				EndLine:    lineNum,
				Signature:  "." + name + "(" + m[2] + ")",
				Metadata: map[string]interface{}{
					"kind":   "mixin",
					"params": params,
				},
				EnrichmentRequired: false,
			})
			mixinCount++
			continue
		}

		// Less variable: @name: value;
		if m := lessVarRE.FindStringSubmatch(line); m != nil {
			name := m[1]
			// Skip if this is a known at-rule keyword (e.g. @media: ...).
			if lessAtRuleKeywords[name] {
				continue
			}
			val := m[2]
			*out = append(*out, types.EntityRecord{
				Name:       "@" + name,
				Kind:       "SCOPE.Component",
				Subtype:    "variable",
				SourceFile: file.Path,
				Language:   "less",
				StartLine:  lineNum,
				EndLine:    lineNum,
				Signature:  "@" + name + ": " + val,
				Metadata: map[string]interface{}{
					"kind":  "variable",
					"value": val,
				},
				EnrichmentRequired: false,
			})
			varCount++
			continue
		}
	}

	return varCount, mixinCount
}

// parseLessParams splits a raw Less params string (e.g. "@bg: red; @color: #fff")
// into a slice of trimmed parameter names. Returns an empty slice if raw is empty.
// Less uses both comma and semicolon as parameter separators.
func parseLessParams(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	// Normalise: replace semicolons with commas, then split.
	normalised := strings.ReplaceAll(raw, ";", ",")
	parts := strings.Split(normalised, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
