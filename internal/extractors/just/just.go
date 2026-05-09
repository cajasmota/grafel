// Package just implements a regex-based extractor for Justfile recipe files.
//
// Justfiles are the config format for the `just` command runner. They are
// make-adjacent but have their own grammar (recipes have dependencies after
// the colon, variables use `:=`, shebang recipes are allowed, etc.). No
// tree-sitter grammar is bundled, so this extractor is regex-only.
//
// Extracted entities:
//   - recipe_name [deps...]:          → Kind="SCOPE.Operation", Subtype="recipe"
//   - variable := value                → Kind="SCOPE.Schema",   Subtype="variable"
//   - import "<path>"                  → Kind="SCOPE.Component", Subtype="import"
//                                        (carries one IMPORTS edge)
//   - file-level container            → Kind="SCOPE.Component", Subtype="file"
//                                        (carries CONTAINS edges)
//
// (SCOPE.Schema matches the convention used by the Dockerfile extractor for
// ENV/ARG — both are configuration-style name/value pairs bound at build
// time. It is one of the 14 canonical the graph SCOPE types.)
//
// Recipe dependencies (the tokens after the colon on a recipe line) are
// recorded in the recipe entity's Properties["dependencies"] —
// they are relationship metadata, not standalone entities.
//
// Issue #374 (PORT-RELS-JUST) — emits the same three relationship kinds the
// other ported extractors emit:
//
//   - IMPORTS: every `import "<path>"` directive (including `import?` for
//     optional imports) becomes a SCOPE.Component import-stub entity carrying
//     a single IMPORTS edge from the justfile → the imported path. Mirrors
//     the contract used by the fish / lua extractors.
//
//   - CALLS: every recipe dependency token (the names following the
//     recipe-separating `:`) emits one CALLS edge per unique callee, attached
//     to the recipe entity. `release: (lint "strict") (vet)` therefore emits
//     CALLS edges to `lint` and `vet`. Self-recursion is dropped.
//
//   - CONTAINS: a file-level SCOPE.Component (subtype="file") emits one
//     CONTAINS edge per declared recipe via BuildOperationStructuralRef
//     (Format A, #144).
//
// Registers itself via init() and is imported by registry_gen.go.
package just

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("just", &Extractor{})
}

// Extractor implements extractor.Extractor for Justfiles.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "just" }

// Patterns mirror the functional requirements in.
//
// Recipe header: starts at column 0 (recipes must be flush-left; recipe
// bodies are indented). Optional attributes on a preceding line (e.g.
// `[private]`) are not required for detection. Parameters in parens and
// dependencies following the colon are captured as a raw tail.
//
// Examples:
//
//	build:
//	test: build
//	deploy env="prod": test (lint "strict")
//	_helper msg:
var (
	// recipeLineRE captures a full recipe header line. Groups:
	//   1: name  (letters/digits/_- starting with letter or _)
	//   2: raw tail between name and the first `:` on the line. May contain
	//      a parameter list in parens and/or bare-word parameters with
	//      quoted default values (e.g. `env="prod"`). Parens and quotes are
	//      allowed but `:` is not — the first bare `:` on the line ends
	//      this group.
	//   3: everything after the recipe-separating `:` up to end of line,
	//      i.e. the dependency list (possibly with parenthesised arg groups).
	//
	// The immediate-`=` check on the post-colon side rules out
	// `NAME := value` (a variable assignment) — those are caught by
	// variableRE instead.
	recipeLineRE = regexp.MustCompile(
		`(?m)^([A-Za-z_][A-Za-z0-9_\-]*)([^\n:]*):(?:([^=\n][^\n#]*)|(?:\s*$))`,
	)
	// paramsParenRE extracts the contents of the first `(...)` group in the
	// pre-colon tail, which in just is the parameter list for a recipe.
	paramsParenRE = regexp.MustCompile(`\(([^)]*)\)`)
	// variableRE captures: name := value. Exported variables use `export name := …`.
	variableRE = regexp.MustCompile(
		`(?m)^(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*:=\s*([^\n#]*)`,
	)
	// importRE captures: `import "path"`, `import 'path'`, `import? "path"`.
	// Just's `import` directive must appear at column 0; the path may be
	// double- or single-quoted.
	importRE = regexp.MustCompile(
		`(?m)^import\??\s+(?:"([^"\n]+)"|'([^'\n]+)')`,
	)
)

// Extract processes the Justfile source and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	// Strip line comments so commented-out import/recipe lines don't match.
	stripped := stripLineComments(src)

	var entities []types.EntityRecord
	seenRecipe := make(map[string]bool)
	seenVar := make(map[string]bool)

	// File-level container entity (CONTAINS edges appended below).
	fileEntity := types.EntityRecord{
		Name:       file.Path,
		Kind:       "SCOPE.Component",
		Subtype:    "file",
		SourceFile: file.Path,
		Language:   "just",
	}
	entities = append(entities, fileEntity)
	const fileIdx = 0

	// IMPORTS — `import "path"` / `import? "path"` directives become
	// SCOPE.Component import-stub entities, one per unique imported path.
	seenImport := make(map[string]bool)
	for _, m := range importRE.FindAllStringSubmatchIndex(stripped, -1) {
		var path string
		// Two alternation groups: double-quoted (g1) and single-quoted (g2).
		if m[2] >= 0 && m[3] > m[2] {
			path = stripped[m[2]:m[3]]
		} else if m[4] >= 0 && m[5] > m[4] {
			path = stripped[m[4]:m[5]]
		}
		if path == "" || seenImport[path] {
			continue
		}
		seenImport[path] = true
		startLine := strings.Count(stripped[:m[0]], "\n") + 1
		entities = append(entities, types.EntityRecord{
			Name:       path,
			Kind:       "SCOPE.Component",
			Subtype:    "import",
			SourceFile: file.Path,
			Language:   "just",
			StartLine:  startLine,
			EndLine:    startLine,
			Relationships: []types.RelationshipRecord{
				{
					FromID: file.Path,
					ToID:   path,
					Kind:   "IMPORTS",
					Properties: map[string]string{
						"source_module": path,
						"imported_name": path,
						"import_kind":   "import",
					},
				},
			},
		})
	}

	// Variables first — they don't conflict with recipe names because the
	// regexes require different terminators (`:=` vs `:`).
	for _, m := range variableRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if seenVar[name] {
			continue
		}
		seenVar[name] = true
		value := strings.TrimSpace(src[m[4]:m[5]])
		startLine := strings.Count(src[:m[0]], "\n") + 1
		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Schema",
			Subtype:            "variable",
			SourceFile:         file.Path,
			Language:           "just",
			StartLine:          startLine,
			EndLine:            startLine,
			Signature:          name + " := " + truncate(value, 80),
			EnrichmentRequired: false,
		})
	}

	// Recipes.
	for _, m := range recipeLineRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		// Skip top-level directives (`set foo := …`, `import "x"`, etc.).
		if isReservedKeyword(name) {
			continue
		}
		// Skip variable-assignment lines — the name in `NAME := value` would
		// satisfy the recipe regex only if the variable's value starts with
		// `=`, which is impossible for `:=`. Belt-and-braces: seenVar guard.
		if seenVar[name] {
			continue
		}
		if seenRecipe[name] {
			continue
		}
		seenRecipe[name] = true

		preColon := ""
		if m[4] >= 0 {
			preColon = src[m[4]:m[5]]
		}
		// Extract parameters — prefer the first parenthesised group if
		// present; otherwise the bare-word tail (`recipe a b c:`) is the
		// parameter list.
		var params string
		if pm := paramsParenRE.FindStringSubmatch(preColon); len(pm) > 1 {
			params = strings.TrimSpace(pm[1])
		} else {
			params = strings.TrimSpace(preColon)
		}

		var deps string
		if m[6] >= 0 {
			deps = strings.TrimSpace(src[m[6]:m[7]])
		}
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine := findRecipeEnd(src, m[1], startLine)

		sig := name
		if params != "" {
			sig = name + "(" + params + ")"
		}
		sig += ":"
		if deps != "" {
			sig += " " + deps
		}

		props := map[string]string{}
		normDeps := ""
		if deps != "" {
			// Dependency list is extracted as relationship metadata per JIRA.
			normDeps = normalizeDeps(deps)
			props["dependencies"] = normDeps
		}
		if params != "" {
			props["parameters"] = params
		}

		// CALLS edges — one per unique dependency name (filter self-recursion).
		var callRels []types.RelationshipRecord
		if normDeps != "" {
			seenCall := make(map[string]bool)
			for _, dep := range strings.Split(normDeps, ",") {
				dep = strings.TrimSpace(dep)
				if dep == "" || dep == name || seenCall[dep] {
					continue
				}
				seenCall[dep] = true
				callRels = append(callRels, types.RelationshipRecord{
					ToID: dep,
					Kind: "CALLS",
				})
			}
		}

		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Operation",
			Subtype:            "recipe",
			SourceFile:         file.Path,
			Language:           "just",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          sig,
			EnrichmentRequired: false,
			Properties:         props,
			Relationships:      callRels,
		})

		// CONTAINS edge: file → recipe (Format A structural-ref).
		toID := extractor.BuildOperationStructuralRef("just", file.Path, name)
		entities[fileIdx].Relationships = append(entities[fileIdx].Relationships, types.RelationshipRecord{
			ToID: toID,
			Kind: "CONTAINS",
		})
	}

	// Issue #90 — language tag for resolver dynamic-pattern dispatch.
	extractor.TagRelationshipsLanguage(entities, "just")
	return entities, nil
}

// stripLineComments removes the portion of each line starting at `#` to
// end-of-line. Justfile comments are line-terminated; quote handling is
// minimal but adequate for structural extraction.
func stripLineComments(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	for _, line := range strings.SplitAfter(src, "\n") {
		if strings.HasPrefix(line, "#!") {
			b.WriteString(line)
			continue
		}
		if idx := strings.Index(line, "#"); idx >= 0 {
			nl := ""
			if strings.HasSuffix(line, "\n") {
				nl = "\n"
			}
			b.WriteString(line[:idx])
			b.WriteString(nl)
			continue
		}
		b.WriteString(line)
	}
	return b.String()
}

// findRecipeEnd returns the line number of the last line in a recipe body.
// Recipe bodies continue until the next non-indented, non-blank line (a new
// recipe or top-level statement).
func findRecipeEnd(src string, headerEnd, startLine int) int {
	// Advance past the header line's newline so `line` counting starts on
	// the first line of the body.
	pos := headerEnd
	if nl := strings.IndexByte(src[pos:], '\n'); nl >= 0 {
		pos += nl + 1
	} else {
		return startLine
	}

	line := startLine
	lastBody := startLine
	i := pos
	n := len(src)
	for i < n {
		line++
		nl := strings.IndexByte(src[i:], '\n')
		var segment string
		if nl < 0 {
			segment = src[i:]
			i = n
		} else {
			segment = src[i : i+nl]
			i += nl + 1
		}
		trimmed := strings.TrimSpace(segment)
		if trimmed == "" {
			continue
		}
		// Indented line (space or tab) → part of the recipe body.
		if len(segment) > 0 && (segment[0] == ' ' || segment[0] == '\t') {
			lastBody = line
			continue
		}
		// Non-indented line → end of recipe.
		break
	}
	return lastBody
}

// normalizeDeps converts the raw dep tail into a compact comma-separated list.
// Just allows parenthesised deps with arguments, e.g. `test: build (lint "x")`.
// For we only need the dep *names*, so we strip argument groups.
func normalizeDeps(raw string) string {
	// Drop parenthesised groups — they contain args, not dep names.
	var b strings.Builder
	depth := 0
	for _, r := range raw {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	fields := strings.Fields(b.String())
	return strings.Join(fields, ",")
}

// isReservedKeyword returns true for tokens that the recipe regex may
// accidentally match but which are not recipe names (just's top-level
// directives).
func isReservedKeyword(tok string) bool {
	switch tok {
	case "set", "import", "mod", "export", "alias":
		return true
	}
	return false
}

// truncate caps s to max runes, appending "…" if truncated.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
