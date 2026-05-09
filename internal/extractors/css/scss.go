// scss.go — regex-based SCSS variable, mixin, and function extractor.
//
// Extracted entities (SCOPE.Component per spec):
//   - SCSS variables   ($name: value)     → Kind="SCOPE.Component", Subtype="variable", Metadata{"kind":"variable","value":"..."}
//   - SCSS mixins      (@mixin name(...)) → Kind="SCOPE.Component", Subtype="mixin",    Metadata{"kind":"mixin","params":[...]}
//   - SCSS functions   (@function name)   → Kind="SCOPE.Component", Subtype="function", Metadata{"kind":"function","params":[...]}
//
// Uses regex rather than tree-sitter because go-tree-sitter does not bundle a
// dedicated SCSS grammar — only the plain CSS grammar is available.
//
// OTel: emits span "extractor.scss" with attributes scss_variable_count,
// scss_mixin_count, scss_function_count (S11-03 mandatory).
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

// scssVarRE matches SCSS variable declarations: $name: value
// Captures: 1=name, 2=value (trimmed, may be empty for complex multi-line).
var scssVarRE = regexp.MustCompile(`^\s*\$([a-zA-Z_-][a-zA-Z0-9_-]*)\s*:\s*(.+?)\s*;`)

// scssMixinRE matches @mixin declarations: @mixin name or @mixin name(params)
// Captures: 1=name, 2=params string (may be empty).
var scssMixinRE = regexp.MustCompile(`^\s*@mixin\s+([a-zA-Z_-][a-zA-Z0-9_-]*)(?:\s*\(([^)]*)\))?`)

// scssFunctionRE matches @function declarations: @function name(params)
// Captures: 1=name, 2=params string (may be empty).
var scssFunctionRE = regexp.MustCompile(`^\s*@function\s+([a-zA-Z_-][a-zA-Z0-9_-]*)(?:\s*\(([^)]*)\))?`)

// scssImportLineRE matches the start of an SCSS-flavour @import / @use /
// @forward directive. Captures: 1=keyword (import|use|forward), 2=tail
// (everything after the keyword up to ; or EOL — parsed for module refs).
var scssImportLineRE = regexp.MustCompile(`^\s*@(import|use|forward)\s+(.+?)\s*;?\s*$`)

// scssImportModuleRE matches a single quoted module ref inside an @import /
// @use / @forward tail, including url("...") wrappers. Captures: 1=double-
// quoted body, 2=single-quoted body (one will be empty per match).
var scssImportModuleRE = regexp.MustCompile(`(?:url\(\s*)?(?:"([^"]*)"|'([^']*)')`)

// ExtractSCSS extracts SCSS entities from raw source and appends them to out.
// It is exported (uppercase) so tests in the same package can call it directly.
func ExtractSCSS(ctx context.Context, file extractor.FileInput, out *[]types.EntityRecord) (varCount, mixinCount, fnCount, importCount int) {
	tracer := otel.Tracer("extractor.scss")

	var span trace.Span
	_, span = tracer.Start(ctx, "extractor.scss",
		trace.WithAttributes(
			attribute.String("file_path", file.Path),
			attribute.String("language", "scss"),
		),
	)
	defer func() {
		span.SetAttributes(
			attribute.Int("scss_variable_count", varCount),
			attribute.Int("scss_mixin_count", mixinCount),
			attribute.Int("scss_function_count", fnCount),
			attribute.Int("scss_import_count", importCount),
		)
		span.End()
	}()

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

		// SCSS @import / @use / @forward — module refs are quoted strings,
		// possibly comma-separated (`@import "a", "b";`). Each ref becomes
		// its own SCOPE.Component "import" entity with an embedded IMPORTS
		// edge. @use / @forward use the same dependency-resolution model as
		// @import in modern SCSS, so we treat them identically here.
		if m := scssImportLineRE.FindStringSubmatch(line); m != nil {
			matches := scssImportModuleRE.FindAllStringSubmatch(m[2], -1)
			for _, mm := range matches {
				module := mm[1]
				if module == "" {
					module = mm[2]
				}
				if module == "" {
					continue
				}
				*out = append(*out, types.EntityRecord{
					Name:       module,
					Kind:       "SCOPE.Component",
					Subtype:    "import",
					SourceFile: file.Path,
					Language:   "scss",
					StartLine:  lineNum,
					EndLine:    lineNum,
					Signature:  "@" + m[1] + " " + module,
					Relationships: []types.RelationshipRecord{
						buildImportRel(file.Path, module),
					},
					EnrichmentRequired: false,
				})
				importCount++
			}
			continue
		}

		// SCSS variable: $name: value;
		if m := scssVarRE.FindStringSubmatch(line); m != nil {
			name := "$" + m[1]
			val := m[2]
			*out = append(*out, types.EntityRecord{
				Name:       name,
				Kind:       "SCOPE.Component",
				Subtype:    "variable",
				SourceFile: file.Path,
				Language:   "scss",
				StartLine:  lineNum,
				EndLine:    lineNum,
				Signature:  name + ": " + val,
				Metadata: map[string]interface{}{
					"kind":  "variable",
					"value": val,
				},
				EnrichmentRequired: false,
			})
			varCount++
			continue
		}

		// SCSS mixin: @mixin name(params)
		if m := scssMixinRE.FindStringSubmatch(line); m != nil {
			name := m[1]
			params := parseParams(m[2])
			*out = append(*out, types.EntityRecord{
				Name:       name,
				Kind:       "SCOPE.Component",
				Subtype:    "mixin",
				SourceFile: file.Path,
				Language:   "scss",
				StartLine:  lineNum,
				EndLine:    lineNum,
				Signature:  "@mixin " + name + "(" + m[2] + ")",
				Metadata: map[string]interface{}{
					"kind":   "mixin",
					"params": params,
				},
				EnrichmentRequired: false,
			})
			mixinCount++
			continue
		}

		// SCSS function: @function name(params)
		if m := scssFunctionRE.FindStringSubmatch(line); m != nil {
			name := m[1]
			params := parseParams(m[2])
			*out = append(*out, types.EntityRecord{
				Name:       name,
				Kind:       "SCOPE.Component",
				Subtype:    "function",
				SourceFile: file.Path,
				Language:   "scss",
				StartLine:  lineNum,
				EndLine:    lineNum,
				Signature:  "@function " + name + "(" + m[2] + ")",
				Metadata: map[string]interface{}{
					"kind":   "function",
					"params": params,
				},
				EnrichmentRequired: false,
			})
			fnCount++
			continue
		}
	}

	return varCount, mixinCount, fnCount, importCount
}

// parseParams splits a raw params string (e.g. "$bg: red, $color: #fff") into
// a slice of trimmed parameter names. Returns an empty slice if raw is empty.
func parseParams(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
