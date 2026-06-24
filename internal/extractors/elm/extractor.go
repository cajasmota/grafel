// Package elm implements a regex-based extractor for Elm source files.
//
// Extracted entities:
//   - Module declarations (`module Main exposing (...)`) → SCOPE.Component (subtype="module")
//   - Function/value declarations (top-level with `name : Type` annotation or bare `name args = body`)
//     → SCOPE.Operation (subtype="function")
//   - Type alias declarations (`type alias Model = {...}`) → SCOPE.Component (subtype="typealias")
//   - Custom type declarations (`type Msg = ... | ...`) → SCOPE.Component (subtype="type")
//   - Import statements (`import Html.Attributes as Attr`) → IMPORTS edges
//   - CALLS edges from function bodies (deduped)
//   - Module CONTAINS top-level declarations
//
// Elm is a whitespace-sensitive language. Top-level declarations start at column 0.
// This extractor uses regular expressions with that layout convention for entity discovery.
//
// Registers itself via init() and is imported by registry_gen.go.
package elm

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("elm", &Extractor{})
}

// Extractor implements extractor.Extractor for Elm.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "elm" }

// -----------------------------------------------------------------------
// Compiled regex patterns
// -----------------------------------------------------------------------

var (
	// moduleRE matches: module Main exposing (..)
	//                   module Html.Attributes exposing (class, id)
	moduleRE = regexp.MustCompile(
		`(?m)^module\s+((?:[A-Z][a-zA-Z0-9_]*\.)*[A-Z][a-zA-Z0-9_]*)\s+exposing\s*\(`,
	)

	// typeAnnotationRE matches top-level type annotations:
	//   functionName : Type -> Type
	// Elm names start with lowercase; type annotations use a single colon.
	typeAnnotationRE = regexp.MustCompile(
		`(?m)^([a-z_][a-zA-Z0-9_]*)\s*:\s*(.+)`,
	)

	// funcDefRE matches a top-level function definition at column 0.
	// name args = body (the name must start with lowercase).
	funcDefRE = regexp.MustCompile(
		`(?m)^([a-z_][a-zA-Z0-9_]*)\s+(?:[^\n=]*=|=)`,
	)

	// typeAliasRE matches: type alias Model = { ... }
	//                      type alias Flags = { name : String }
	typeAliasRE = regexp.MustCompile(
		`(?m)^type\s+alias\s+([A-Z][a-zA-Z0-9_]*)\s*=`,
	)

	// customTypeRE matches: type Msg = Increment | Decrement | ...
	// We use a negative lookahead simulation: skip if the capture starts with "alias ".
	customTypeRE = regexp.MustCompile(
		`(?m)^type\s+([A-Z][a-zA-Z0-9_]*(?:\s+[a-zA-Z][a-zA-Z0-9_]*)*)\s*=`,
	)

	// importRE matches:
	//   import Html
	//   import Html.Attributes as Attr
	//   import Html.Attributes exposing (class, id)
	//   import Browser.Navigation as Nav
	importRE = regexp.MustCompile(
		`(?m)^import\s+((?:[A-Z][a-zA-Z0-9_]*\.)*[A-Z][a-zA-Z0-9_]*)(?:\s+(?:as\s+[A-Z][a-zA-Z0-9_]*|exposing\s*\([^)]*\)))*`,
	)

	// callRE matches lowercase identifiers used as potential function calls.
	callRE = regexp.MustCompile(
		`\b([a-z_][a-zA-Z0-9_]*)\b`,
	)

	// qualifiedCallRE matches Module.function call patterns (e.g. List.map, Html.div).
	qualifiedCallRE = regexp.MustCompile(
		`\b([A-Z][a-zA-Z0-9_]*(?:\.[A-Z][a-zA-Z0-9_]*)*\.[a-z_][a-zA-Z0-9_]*)\b`,
	)
)

// elmKeywords is the set of tokens to exclude from CALLS edges.
var elmKeywords = map[string]bool{
	// Language keywords
	"module": true, "exposing": true, "import": true, "as": true,
	"type": true, "alias": true,
	"if": true, "then": true, "else": true,
	"case": true, "of": true,
	"let": true, "in": true,
	"port": true, "effect": true, "where": true,
	// Common short names that are Elm syntax but emitted as identifiers
	"main": false, // main is a valid entity — don't skip it
	// Elm literals / operators as identifiers
	"True": true, "False": true,
	// Common one-letter variable names
	"a": true, "b": true, "c": true, "d": true, "e": true,
	"f": true, "g": true, "h": true, "i": true, "j": true,
	"k": true, "m": true, "n": true, "p": true, "r": true,
	"s": true, "t": true, "v": true, "x": true, "y": true, "z": true,
}

// Extract processes the Elm source and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	out := extractElm(string(file.Content), file.Path)
	extractor.TagRelationshipsLanguage(out, "elm")
	extractor.TagEntitiesLanguage(out, "elm")
	return out, nil
}

func extractElm(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord

	// Emit file-level entity.
	entities = append(entities, extractor.FileEntity(extractor.FileInput{
		Path:     filePath,
		Language: "elm",
	}))

	imports := collectImports(src)
	importEntities := buildImportEntities(filePath, imports)
	entities = append(entities, importEntities...)

	// 1. Module declaration.
	var moduleName string
	if m := moduleRE.FindStringSubmatch(src); m != nil {
		moduleName = m[1]
		startLine := strings.Count(src[:strings.Index(src, m[0])], "\n") + 1
		entities = append(entities, types.EntityRecord{
			Name:       moduleName,
			Kind:       "SCOPE.Component",
			Subtype:    "module",
			SourceFile: filePath,
			Language:   "elm",
			StartLine:  startLine,
			EndLine:    startLine,
			Signature:  "module " + moduleName + " exposing (..)",
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
		})
	}

	// 2. type alias declarations (must be checked before customTypeRE since
	//    customTypeRE would otherwise match "type alias Foo").
	typeAliasSeen := make(map[string]bool)
	for _, m := range typeAliasRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		name := strings.TrimSpace(src[m[2]:m[3]])
		if typeAliasSeen[name] {
			continue
		}
		typeAliasSeen[name] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "typealias",
			SourceFile: filePath,
			Language:   "elm",
			StartLine:  startLine,
			EndLine:    startLine,
			Signature:  "type alias " + name,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
		})
	}

	// 3. Custom type declarations (skip "type alias ...").
	customTypeSeen := make(map[string]bool)
	for _, m := range customTypeRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		rawName := strings.TrimSpace(src[m[2]:m[3]])
		// Skip "alias" — handled above.
		if strings.HasPrefix(rawName, "alias") {
			continue
		}
		// Extract just the type name (strip type params).
		name := extractTypeName(rawName)
		if name == "" || name == "alias" {
			continue
		}
		if customTypeSeen[name] {
			continue
		}
		customTypeSeen[name] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "type",
			SourceFile: filePath,
			Language:   "elm",
			StartLine:  startLine,
			EndLine:    startLine,
			Signature:  "type " + name,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
		})
	}

	// 4. Collect all top-level function names (from type annotations + definitions).
	funcNames := collectFunctions(src)

	// 5. Emit SCOPE.Operation entities for each function.
	for _, fn := range funcNames {
		sigPos := findFunctionPos(src, fn)
		startLine := 1
		if sigPos >= 0 {
			startLine = strings.Count(src[:sigPos], "\n") + 1
		}
		body := extractFunctionBody(src, fn)
		endLine := startLine + strings.Count(body, "\n")
		calls := collectCalls(body, fn)

		sig := fn
		if ann := findTypeAnnotation(src, fn); ann != "" {
			sig = fn + " : " + ann
		}

		entities = append(entities, types.EntityRecord{
			Name:          fn,
			Kind:          "SCOPE.Operation",
			Subtype:       "function",
			SourceFile:    filePath,
			Language:      "elm",
			StartLine:     startLine,
			EndLine:       endLine,
			Signature:     sig,
			Properties:    map[string]string{"imports": strings.Join(imports, ",")},
			Relationships: calls,
		})
	}

	// 6. Add CONTAINS edges from module to all top-level declarations.
	if moduleName != "" {
		var containsRels []types.RelationshipRecord
		for _, fn := range funcNames {
			ref := extractor.BuildOperationStructuralRef("elm", filePath, fn)
			containsRels = append(containsRels, types.RelationshipRecord{
				ToID: ref,
				Kind: "CONTAINS",
			})
		}
		for i := range entities {
			if entities[i].Name == moduleName && entities[i].Kind == "SCOPE.Component" && entities[i].Subtype == "module" {
				entities[i].Relationships = append(entities[i].Relationships, containsRels...)
				break
			}
		}
	}

	// #5375: The Elm Architecture decoration. Import-gated — a no-op for any
	// file that does not import Browser/Html. Mutates entities in place (re-kinds
	// Model/Msg, tags the init/update/view triad, flags the Browser program).
	applyTEA(src, imports, entities)

	return entities
}

// -----------------------------------------------------------------------
// Helper: import collection
// -----------------------------------------------------------------------

func collectImports(src string) []string {
	seen := make(map[string]bool)
	var imports []string
	for _, m := range importRE.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		mod := strings.TrimSpace(m[1])
		if mod == "" || seen[mod] {
			continue
		}
		seen[mod] = true
		imports = append(imports, mod)
	}
	return imports
}

func buildImportEntities(filePath string, imports []string) []types.EntityRecord {
	if len(imports) == 0 {
		return nil
	}
	out := make([]types.EntityRecord, 0, len(imports))
	seen := make(map[string]bool, len(imports))
	for _, mod := range imports {
		if seen[mod] {
			continue
		}
		seen[mod] = true
		displayName := importDisplayName(mod)
		out = append(out, types.EntityRecord{
			Name:       displayName,
			Kind:       "SCOPE.Component",
			SourceFile: filePath,
			Language:   "elm",
			Relationships: []types.RelationshipRecord{
				{
					FromID: filePath,
					ToID:   mod,
					Kind:   "IMPORTS",
				},
			},
		})
	}
	return out
}

func importDisplayName(mod string) string {
	if dot := strings.LastIndexByte(mod, '.'); dot >= 0 {
		return mod[dot+1:]
	}
	return mod
}

// -----------------------------------------------------------------------
// Helper: function discovery
// -----------------------------------------------------------------------

// collectFunctions returns deduplicated top-level function names.
func collectFunctions(src string) []string {
	seen := make(map[string]bool)
	var funcs []string

	// First pass: type annotations (most reliable for Elm top-level functions).
	for _, m := range typeAnnotationRE.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		name := strings.TrimSpace(m[1])
		if name == "" || seen[name] || elmKeywords[name] {
			continue
		}
		seen[name] = true
		funcs = append(funcs, name)
	}

	// Second pass: function definitions at column 0 (catches definitions without annotations).
	for _, m := range funcDefRE.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		name := m[1]
		if name == "" || seen[name] || elmKeywords[name] {
			continue
		}
		// Skip names that start with uppercase (those are type constructors).
		if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
			continue
		}
		seen[name] = true
		funcs = append(funcs, name)
	}

	return funcs
}

// findFunctionPos returns the byte offset of the function's type annotation
// or first definition in src.
func findFunctionPos(src, name string) int {
	// Try type annotation first.
	annPattern := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `\s*:(?:[^:])`)
	if loc := annPattern.FindStringIndex(src); loc != nil {
		return loc[0]
	}
	// Fall back to first definition.
	defPattern := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `\s+`)
	if loc := defPattern.FindStringIndex(src); loc != nil {
		return loc[0]
	}
	return -1
}

// findTypeAnnotation returns the type annotation string for a function name.
func findTypeAnnotation(src, name string) string {
	annPattern := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `\s*:\s*(.+)`)
	if m := annPattern.FindStringSubmatch(src); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// extractFunctionBody returns the body lines for a function.
// In Elm, top-level function definitions start at column 0; continuation
// lines are indented with at least one space or tab.
func extractFunctionBody(src, name string) string {
	// Match definition lines (not type annotations: avoid "name :" without "=").
	defPattern := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `(?:\s+[^\n]*)?=`)
	locs := defPattern.FindAllStringIndex(src, -1)
	if len(locs) == 0 {
		return ""
	}
	var bodies []string
	for _, loc := range locs {
		// Skip if line looks like a type annotation (contains " : " without "=").
		lineEnd := strings.Index(src[loc[0]:], "\n")
		var lineText string
		if lineEnd >= 0 {
			lineText = src[loc[0] : loc[0]+lineEnd]
		} else {
			lineText = src[loc[0]:]
		}
		// Skip if the only colon is part of type annotation pattern (no equals sign before it)
		if strings.Contains(lineText, " : ") && !strings.Contains(lineText, "=") {
			continue
		}
		bodies = append(bodies, extractIndentBody(src, loc[1]))
	}
	return strings.Join(bodies, "\n")
}

// extractIndentBody collects all lines following afterPos that are indented
// (Elm layout convention: top-level starts at column 0).
func extractIndentBody(src string, afterPos int) string {
	rest := src[afterPos:]
	lines := strings.Split(rest, "\n")
	var body []string
	for i, line := range lines {
		if i == 0 {
			body = append(body, line)
			continue
		}
		if line == "" || strings.TrimSpace(line) == "" {
			body = append(body, line)
			continue
		}
		// Continuation lines in Elm start with at least one space/tab.
		if line[0] == ' ' || line[0] == '\t' {
			body = append(body, line)
		} else {
			break
		}
	}
	return strings.Join(body, "\n")
}

// -----------------------------------------------------------------------
// Helper: type name extraction
// -----------------------------------------------------------------------

// extractTypeName extracts just the type constructor name from a raw name
// string that may include type variables.
func extractTypeName(raw string) string {
	raw = strings.TrimSpace(raw)
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

// -----------------------------------------------------------------------
// Helper: CALLS edge collection
// -----------------------------------------------------------------------

// collectCalls extracts CALLS relationships from a function body.
func collectCalls(body, callerName string) []types.RelationshipRecord {
	if body == "" {
		return nil
	}
	scrubbed := stripStringsAndComments(body)
	seen := make(map[string]bool)
	out := make([]types.RelationshipRecord, 0)

	// Qualified calls: Module.function (e.g. List.map, Html.div)
	for _, m := range qualifiedCallRE.FindAllStringSubmatchIndex(scrubbed, -1) {
		if len(m) < 4 || m[2] < 0 || m[3] < 0 {
			continue
		}
		target := scrubbed[m[2]:m[3]]
		if target == "" || target == callerName || seen[target] {
			continue
		}
		seen[target] = true
		lineNum := 1 + strings.Count(scrubbed[:m[0]], "\n")
		out = append(out, types.RelationshipRecord{
			ToID: target,
			Kind: "CALLS",
			Properties: map[string]string{
				"line": strconv.Itoa(lineNum),
			},
		})
	}

	// Unqualified calls: bare function names
	for _, m := range callRE.FindAllStringSubmatchIndex(scrubbed, -1) {
		if len(m) < 4 || m[2] < 0 || m[3] < 0 {
			continue
		}
		target := scrubbed[m[2]:m[3]]
		if target == "" || target == callerName {
			continue
		}
		if elmKeywords[target] {
			continue
		}
		if len(target) <= 1 {
			continue
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		lineNum := 1 + strings.Count(scrubbed[:m[0]], "\n")
		out = append(out, types.RelationshipRecord{
			ToID: target,
			Kind: "CALLS",
			Properties: map[string]string{
				"line": strconv.Itoa(lineNum),
			},
		})
	}

	return out
}

// stripStringsAndComments replaces string literals and -- line comments
// with spaces to avoid false call detection.
func stripStringsAndComments(src string) string {
	out := make([]byte, len(src))
	i := 0
	inStr := false
	inChar := false
	depth := 0 // for {- block comments -}

	for i < len(src) {
		ch := src[i]

		if inStr {
			out[i] = ' '
			if ch == '\\' && i+1 < len(src) {
				out[i+1] = ' '
				i += 2
				continue
			}
			if ch == '"' {
				inStr = false
			}
			i++
			continue
		}

		if inChar {
			out[i] = ' '
			if ch == '\\' && i+1 < len(src) {
				out[i+1] = ' '
				i += 2
				continue
			}
			if ch == '\'' {
				inChar = false
			}
			i++
			continue
		}

		if depth > 0 {
			// Inside a block comment {- ... -}
			if ch == '{' && i+1 < len(src) && src[i+1] == '-' {
				out[i] = ' '
				out[i+1] = ' '
				i += 2
				depth++
				continue
			}
			if ch == '-' && i+1 < len(src) && src[i+1] == '}' {
				out[i] = ' '
				out[i+1] = ' '
				i += 2
				depth--
				continue
			}
			out[i] = ' '
			i++
			continue
		}

		// Line comment: -- to end of line
		if ch == '-' && i+1 < len(src) && src[i+1] == '-' {
			for i < len(src) && src[i] != '\n' {
				out[i] = ' '
				i++
			}
			continue
		}

		// Block comment: {- ... -} (nested, as Elm supports)
		if ch == '{' && i+1 < len(src) && src[i+1] == '-' {
			out[i] = ' '
			out[i+1] = ' '
			i += 2
			depth++
			continue
		}

		switch ch {
		case '"':
			inStr = true
			out[i] = ' '
		case '\'':
			inChar = true
			out[i] = ' '
		default:
			out[i] = ch
		}
		i++
	}
	return string(out)
}
