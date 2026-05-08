// Package imports implements the cross-language import statement extractor.
//
// Parses source files for import/require/use/using statements and emits
// DEPENDS_ON(kind=import) relationships from the file entity to each
// imported module.
//
// Supported languages:
//   - JavaScript / TypeScript: import x from 'y', require('y'), import('y')
//   - Python:                  import x, from x import y
//   - Java:                    import x.y.z
//   - C#:                      using X.Y.Z
//   - Ruby:                    require 'x', require_relative 'x'
//   - Go:                      import "x", import ( "x" )
//   - Rust:                    use x::y
//   - Elixir:                  import X, alias X, use X, require X
//
// Entity kind: "SCOPE.Component"
// Relationships emitted: DEPENDS_ON(kind=import)
//
// OTel span: indexer.import_extractor.extract
// Attributes: file_path, language, imports_found, local_imports_found,
//
//	external_imports_found
//
// Registration key: "_cross_imports"
package imports

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("_cross_imports", &Extractor{})
}

// Extractor parses import statements from source files.
type Extractor struct{}

// Language returns the registration key.
func (e *Extractor) Language() string { return "_cross_imports" }

// ---------------------------------------------------------------------------
// import record
// ---------------------------------------------------------------------------

type importRecord struct {
	module  string
	isLocal bool
	raw     string
}

// ---------------------------------------------------------------------------
// Ref helpers
// ---------------------------------------------------------------------------

func fileRef(filePath string) string {
	return "scope:component:file:" + filePath
}

func moduleRef(module string, isLocal bool) string {
	locality := "external"
	if isLocal {
		locality = "local"
	}
	return "scope:component:import:" + locality + ":" + module
}

// ---------------------------------------------------------------------------
// Compiled regular expressions — per language
// ---------------------------------------------------------------------------

// JavaScript / TypeScript
var jsImportFromRE = regexp.MustCompile(
	`(?m)import\s+(?:[^'"()\s][^'"()]*\s+from\s+)?['"]([^'"]+)['"]`,
)

// require without leading dot/word (plain require at statement level)
var jsRequirePlainRE = regexp.MustCompile(
	`(?m)(?:^|[^.\w])require\s*\(\s*['"]([^'"]+)['"]\s*\)`,
)

// Python
var pyImportRE = regexp.MustCompile(`(?m)^import\s+([\w.]+)`)
var pyFromImportRE = regexp.MustCompile(`(?m)^from\s+(\.{0,3}[\w.]*)\s+import\s+`)

// Java
var javaImportRE = regexp.MustCompile(`(?m)^import\s+(?:static\s+)?([\w.]+)(?:\.\*)?;`)

// C#
var csharpUsingRE = regexp.MustCompile(`(?m)^using\s+(?:static\s+)?(?:[\w]+\s*=\s*)?([\w.]+)\s*;`)

// Ruby
var rubyRequireRelativeRE = regexp.MustCompile("(?m)require_relative\\s+['\"]([^'\"]+)['\"]")
var rubyRequireRE = regexp.MustCompile("(?m)(?:^|\\s)require(?:_relative)?\\s+['\"]([^'\"]+)['\"]")

// Go
var goImportBlockRE = regexp.MustCompile(`(?s)import\s*\(([^)]+)\)`)
var goImportSingleRE = regexp.MustCompile("(?m)^import\\s+(?:[\\w]+\\s+)?[`\"]([^`\"\\s]+)[`\"]")
var goPkgQuotedRE = regexp.MustCompile("[`\"]([^`\"\\s]+)[`\"]")

// Rust
var rustUseRE = regexp.MustCompile(`(?m)^use\s+((?:(?:crate|super|self)::)?[\w:{},*\s]+?)\s*;`)

// Elixir
var elixirImportRE = regexp.MustCompile(`(?m)^\s*(?:import|alias|use|require)\s+([\w.]+)`)

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func isRelativePath(s string) bool {
	return strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") ||
		s == "." || s == ".."
}

func topLevelModule(path string) string {
	if strings.Contains(path, "/") {
		return path // Go-style: keep full path
	}
	parts := strings.SplitN(path, ".", 2)
	return parts[0]
}

// ---------------------------------------------------------------------------
// Per-language extractors
// ---------------------------------------------------------------------------

func extractJS(source string) []importRecord {
	var out []importRecord
	seen := map[string]bool{}

	for _, m := range jsImportFromRE.FindAllStringSubmatch(source, -1) {
		raw := m[1]
		if seen[raw] {
			continue
		}
		seen[raw] = true
		isLocal := isRelativePath(raw)
		var module string
		if strings.HasPrefix(raw, "@") {
			parts := strings.SplitN(raw, "/", 3)
			if len(parts) >= 2 {
				module = parts[0] + "/" + parts[1]
			} else {
				module = raw
			}
		} else if isLocal {
			module = raw
		} else {
			module = strings.SplitN(raw, "/", 2)[0]
		}
		out = append(out, importRecord{module: module, isLocal: isLocal, raw: raw})
	}

	for _, m := range jsRequirePlainRE.FindAllStringSubmatch(source, -1) {
		raw := m[1]
		if seen[raw] {
			continue
		}
		seen[raw] = true
		isLocal := isRelativePath(raw)
		var module string
		if strings.HasPrefix(raw, "@") {
			parts := strings.SplitN(raw, "/", 3)
			if len(parts) >= 2 {
				module = parts[0] + "/" + parts[1]
			} else {
				module = raw
			}
		} else {
			module = strings.SplitN(raw, "/", 2)[0]
		}
		out = append(out, importRecord{module: module, isLocal: isLocal, raw: raw})
	}
	return out
}

func extractPython(source string) []importRecord {
	var out []importRecord
	seen := map[string]bool{}

	for _, m := range pyImportRE.FindAllStringSubmatch(source, -1) {
		raw := m[1]
		if seen[raw] {
			continue
		}
		seen[raw] = true
		out = append(out, importRecord{module: topLevelModule(raw), isLocal: false, raw: raw})
	}

	for _, m := range pyFromImportRE.FindAllStringSubmatch(source, -1) {
		raw := m[1]
		if seen[raw] {
			continue
		}
		seen[raw] = true
		var module string
		isLocal := false
		if strings.HasPrefix(raw, ".") {
			stripped := strings.TrimLeft(raw, ".")
			if stripped == "" {
				module = "."
			} else {
				module = stripped
			}
			isLocal = true
		} else {
			module = topLevelModule(raw)
		}
		out = append(out, importRecord{module: module, isLocal: isLocal, raw: raw})
	}
	return out
}

func extractJava(source string) []importRecord {
	var out []importRecord
	seen := map[string]bool{}

	for _, m := range javaImportRE.FindAllStringSubmatch(source, -1) {
		raw := m[1]
		if seen[raw] {
			continue
		}
		seen[raw] = true
		parts := strings.SplitN(raw, ".", 2)
		out = append(out, importRecord{module: parts[0], isLocal: false, raw: raw})
	}
	return out
}

func extractCSharp(source string) []importRecord {
	var out []importRecord
	seen := map[string]bool{}

	for _, m := range csharpUsingRE.FindAllStringSubmatch(source, -1) {
		raw := m[1]
		if seen[raw] {
			continue
		}
		seen[raw] = true
		module := strings.SplitN(raw, ".", 2)[0]
		out = append(out, importRecord{module: module, isLocal: false, raw: raw})
	}
	return out
}

func extractRuby(source string) []importRecord {
	var out []importRecord
	seen := map[string]bool{}

	for _, m := range rubyRequireRelativeRE.FindAllStringSubmatch(source, -1) {
		raw := m[1]
		if seen[raw] {
			continue
		}
		seen[raw] = true
		out = append(out, importRecord{module: raw, isLocal: true, raw: raw})
	}

	for _, m := range rubyRequireRE.FindAllStringSubmatch(source, -1) {
		raw := m[1]
		if seen[raw] {
			continue
		}
		seen[raw] = true
		isLocal := isRelativePath(raw)
		module := raw
		if !isLocal {
			module = strings.SplitN(raw, "/", 2)[0]
		}
		out = append(out, importRecord{module: module, isLocal: isLocal, raw: raw})
	}
	return out
}

func extractGo(source string) []importRecord {
	var out []importRecord
	seen := map[string]bool{}

	// Block imports
	for _, bm := range goImportBlockRE.FindAllStringSubmatch(source, -1) {
		block := bm[1]
		for _, pm := range goPkgQuotedRE.FindAllStringSubmatch(block, -1) {
			raw := pm[1]
			if seen[raw] {
				continue
			}
			seen[raw] = true
			isLocal := isRelativePath(raw)
			out = append(out, importRecord{module: raw, isLocal: isLocal, raw: raw})
		}
	}

	// Single-line imports
	for _, m := range goImportSingleRE.FindAllStringSubmatch(source, -1) {
		raw := m[1]
		if seen[raw] {
			continue
		}
		seen[raw] = true
		isLocal := isRelativePath(raw)
		out = append(out, importRecord{module: raw, isLocal: isLocal, raw: raw})
	}
	return out
}

func extractRust(source string) []importRecord {
	var out []importRecord
	seen := map[string]bool{}

	for _, m := range rustUseRE.FindAllStringSubmatch(source, -1) {
		raw := strings.TrimSpace(m[1])
		if seen[raw] {
			continue
		}
		seen[raw] = true
		isLocal := strings.HasPrefix(raw, "crate::") ||
			strings.HasPrefix(raw, "self::") ||
			strings.HasPrefix(raw, "super::")
		module := strings.SplitN(raw, "::", 2)[0]
		out = append(out, importRecord{module: module, isLocal: isLocal, raw: raw})
	}
	return out
}

func extractElixir(source string) []importRecord {
	var out []importRecord
	seen := map[string]bool{}

	for _, m := range elixirImportRE.FindAllStringSubmatch(source, -1) {
		raw := m[1]
		if seen[raw] {
			continue
		}
		seen[raw] = true
		module := strings.SplitN(raw, ".", 2)[0]
		out = append(out, importRecord{module: module, isLocal: false, raw: raw})
	}
	return out
}

// ---------------------------------------------------------------------------
// Language dispatcher
// ---------------------------------------------------------------------------

type extractFn func(string) []importRecord

var langExtractors = map[string]extractFn{
	"javascript": extractJS,
	"typescript": extractJS,
	"python":     extractPython,
	"java":       extractJava,
	"csharp":     extractCSharp,
	"ruby":       extractRuby,
	"go":         extractGo,
	"rust":       extractRust,
	"elixir":     extractElixir,
}

// ---------------------------------------------------------------------------
// Entity / relationship builders
// ---------------------------------------------------------------------------

func buildEntitiesAndRels(filePath string, imports []importRecord) []types.EntityRecord {
	var out []types.EntityRecord
	fRef := fileRef(filePath)
	seenRefs := map[string]bool{}

	for _, imp := range imports {
		modRef := moduleRef(imp.module, imp.isLocal)

		if !seenRefs[modRef] {
			seenRefs[modRef] = true
			extDep := "true"
			if imp.isLocal {
				extDep = "false"
			}
			out = append(out, types.EntityRecord{
				Name:       imp.module,
				Kind:       "SCOPE.Component",
				Subtype:    "import",
				SourceFile: filePath,
				Properties: map[string]string{
					"is_local":            boolStr(imp.isLocal),
					"external_dependency": extDep,
					"ref":                 modRef,
					"provenance":          "INFERRED_FROM_IMPORT_STATEMENT",
				},
				QualityScore: 0.8,
			})
		}

		// Relationship entity
		out = append(out, types.EntityRecord{
			Name:       "DEPENDS_ON:" + fRef + "->" + modRef,
			Kind:       "relationship",
			Subtype:    "depends_on",
			SourceFile: filePath,
			Relationships: []types.RelationshipRecord{
				{
					FromID: fRef,
					ToID:   modRef,
					Kind:   "DEPENDS_ON",
					Properties: map[string]string{
						"kind":       "import",
						"module":     imp.module,
						"is_local":   boolStr(imp.isLocal),
						"raw_import": imp.raw,
					},
				},
			},
			QualityScore: 0.8,
		})
	}
	return out
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// ---------------------------------------------------------------------------
// Extract implements extractor.Extractor
// ---------------------------------------------------------------------------

// Extract parses import statements from the source file.
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("extractor._cross_imports")
	ctx, span := tracer.Start(ctx, "indexer.import_extractor.extract")
	defer span.End()
	_ = ctx

	span.SetAttributes(
		attribute.String("file_path", file.Path),
		attribute.String("language", file.Language),
	)

	lang := strings.ToLower(file.Language)
	fn, ok := langExtractors[lang]
	if !ok {
		span.SetAttributes(
			attribute.Int("imports_found", 0),
			attribute.Int("local_imports_found", 0),
			attribute.Int("external_imports_found", 0),
		)
		return nil, nil
	}

	imports := fn(string(file.Content))

	localCount := 0
	externalCount := 0
	for _, imp := range imports {
		if imp.isLocal {
			localCount++
		} else {
			externalCount++
		}
	}

	span.SetAttributes(
		attribute.Int("imports_found", len(imports)),
		attribute.Int("local_imports_found", localCount),
		attribute.Int("external_imports_found", externalCount),
	)

	return buildEntitiesAndRels(file.Path, imports), nil
}
