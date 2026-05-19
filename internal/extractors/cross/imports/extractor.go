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
	jsaliases "github.com/cajasmota/archigraph/internal/extractors/javascript"
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

// extractJSWithAliases is the JS/TS variant that consults the per-repo
// tsconfig / metro / vite / babel alias map (chain-fix C, wave-9). When
// an import specifier matches a known alias (`@/components/Foo`,
// `@components/Foo`, `~/lib/foo`), the resolved repo-relative target
// path replaces the raw spec — turning what would have been a bogus
// `ext:@` external into a proper local DEPENDS_ON edge that the
// downstream resolver can bind to a real file.
//
// Falls back to the legacy non-aliased extractJS when:
//   - repoRoot is empty (test harness without a real repo)
//   - no alias map is declared (no tsconfig/metro/vite/babel config)
//   - the specifier doesn't match any alias prefix
//
// The alias substitution preserves the import's locality classification:
// every alias target is repo-relative by construction (aliases.go skips
// absolute paths outside the repo at parse time), so substituted imports
// are always marked is_local=true.
func extractJSWithAliases(source, repoRoot string) []importRecord {
	imports := extractJS(source)
	if repoRoot == "" {
		return imports
	}
	am := jsaliases.AliasMapFor(repoRoot)
	for i := range imports {
		raw := imports[i].raw
		// Only consider unresolved non-relative specifiers — relative
		// imports already flow through the standard local path.
		if imports[i].isLocal {
			continue
		}
		// Skip specifiers without an `@`, `~` or `#` alias-ish leading
		// character to avoid a hot-path lookup on every npm spec.
		first := byte(0)
		if len(raw) > 0 {
			first = raw[0]
		}
		if first != '@' && first != '~' && first != '#' {
			continue
		}
		resolved := am.Resolve(raw)
		if resolved == "" {
			continue
		}
		imports[i] = importRecord{module: resolved, isLocal: true, raw: raw}
	}
	return imports
}

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

type extractFn func(source, repoRoot string) []importRecord

// adaptExtractor wraps a legacy source-only extractor into the
// (source, repoRoot) signature so the dispatcher table stays uniform.
// Per-language extractors that don't need repo-relative path-alias
// resolution use this — only the JS/TS extractor consumes repoRoot
// to query the tsconfig / metro / vite / babel alias map.
func adaptExtractor(fn func(string) []importRecord) extractFn {
	return func(src, _ string) []importRecord { return fn(src) }
}

var langExtractors = map[string]extractFn{
	"javascript": extractJSWithAliases,
	"typescript": extractJSWithAliases,
	"python":     adaptExtractor(extractPython),
	"java":       adaptExtractor(extractJava),
	"csharp":     adaptExtractor(extractCSharp),
	"ruby":       adaptExtractor(extractRuby),
	"go":         adaptExtractor(extractGo),
	"rust":       adaptExtractor(extractRust),
	"elixir":     adaptExtractor(extractElixir),
}

// ---------------------------------------------------------------------------
// Entity / relationship builders
// ---------------------------------------------------------------------------

func buildEntitiesAndRels(filePath string, imports []importRecord) []types.EntityRecord {
	var out []types.EntityRecord
	fRef := fileRef(filePath)
	// indexOf maps modRef -> position in `out` of the SCOPE.Component carrying
	// that module, so the DEPENDS_ON edge can be embedded directly on the real
	// entity instead of a synthetic "relationship"-kind container (#560).
	indexOf := map[string]int{}

	for _, imp := range imports {
		modRef := moduleRef(imp.module, imp.isLocal)

		idx, seen := indexOf[modRef]
		if !seen {
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
			idx = len(out) - 1
			indexOf[modRef] = idx
		}

		// Embed the DEPENDS_ON relationship on the SCOPE.Component for this
		// module rather than emitting a synthetic relationship-kind entity.
		// The downstream pipeline (cmd/archigraph/index.go) walks every
		// EntityRecord.Relationships, so the edge still reaches the document.
		out[idx].Relationships = append(out[idx].Relationships, types.RelationshipRecord{
			FromID: fRef,
			ToID:   modRef,
			Kind:   "DEPENDS_ON",
			Properties: map[string]string{
				"kind":       "import",
				"module":     imp.module,
				"is_local":   boolStr(imp.isLocal),
				"raw_import": imp.raw,
			},
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

	imports := fn(string(file.Content), file.RepoRoot)

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
