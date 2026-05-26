// Package idris implements a regex-based extractor for Idris source files.
//
// Idris is a dependently-typed functional programming language similar to
// Haskell but with first-class types and theorem proving features.
// Reference: https://www.idris-lang.org/
//
// Extracted entities:
//   - `module Foo` declarations → SCOPE.Component (subtype="module")
//   - Function definitions (type signatures + bodies) → SCOPE.Operation (subtype="function")
//   - `data` declarations → SCOPE.Component (subtype="data")
//   - `record` declarations → SCOPE.Component (subtype="record")
//   - `interface` declarations (type classes) → SCOPE.Component (subtype="interface")
//   - `implementation` declarations → SCOPE.Component (subtype="implementation") + IMPLEMENTS edges
//   - `import Data.List` statements → IMPORTS edges
//   - CALLS edges (deduped, from function bodies)
//   - CONTAINS edges (module → top-level declarations)
//
// File extension: .idr
//
// No tree-sitter grammar for Idris is bundled in smacker/go-tree-sitter, so
// this extractor uses regular expressions similar to the Haskell extractor.
// Idris follows layout rules similar to Haskell (top-level bindings at column 0).
//
// Registers itself via init() and is imported by registry_gen.go.
package idris

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("idris", &Extractor{})
}

// Extractor implements extractor.Extractor for Idris.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "idris" }

// -----------------------------------------------------------------------
// Compiled regex patterns
// -----------------------------------------------------------------------

var (
	// moduleRE matches: module Foo.Bar
	moduleRE = regexp.MustCompile(
		`(?m)^module\s+((?:[A-Z][a-zA-Z0-9_']*\.)*[A-Z][a-zA-Z0-9_']*)`,
	)

	// typeSigRE matches top-level type signatures:
	//   functionName : Type -> Type  (Idris uses single colon)
	//   functionName : (x : Nat) -> Vect x a
	typeSigRE = regexp.MustCompile(
		`(?m)^([a-z_][a-zA-Z0-9_']*(?:\s*,\s*[a-z_][a-zA-Z0-9_']*)*)\s*:\s+(.+)`,
	)

	// funcDefRE matches a top-level function definition (name at column 0 with pattern or =).
	funcDefRE = regexp.MustCompile(
		`(?m)^([a-z_][a-zA-Z0-9_']*)\s+(?:[^\n=]*=|=)`,
	)

	// dataRE matches: data Maybe a = Nothing | Just a
	//                 data Vect : Nat -> Type -> Type where
	dataRE = regexp.MustCompile(
		`(?m)^data\s+([A-Z][a-zA-Z0-9_']*(?:\s+[a-zA-Z][a-zA-Z0-9_']*)*)\s*(?::|=|where)`,
	)

	// recordRE matches: record Pair a b where
	recordRE = regexp.MustCompile(
		`(?m)^record\s+([A-Z][a-zA-Z0-9_']*(?:\s+[a-zA-Z][a-zA-Z0-9_']*)*)\s*(?:where|$)`,
	)

	// interfaceRE matches: interface Show a where
	//                      interface (Eq a) => Ord a where
	interfaceRE = regexp.MustCompile(
		`(?m)^interface\s+(?:\([^)]*\)\s*=>\s*)?([A-Z][a-zA-Z0-9_']*(?:\s+[a-zA-Z][a-zA-Z0-9_']*)*)?\s+where`,
	)

	// implementationRE matches: implementation Show Nat where
	//                           implementation (Eq a) => Eq (List a) where
	implementationRE = regexp.MustCompile(
		`(?m)^implementation\s+(?:\([^)]*\)\s*=>\s*)?([A-Z][a-zA-Z0-9_']*)\s+((?:\([^)]*\)|[A-Z][a-zA-Z0-9_']*(?:\s+[a-z][a-zA-Z0-9_']*)*|[a-z][a-zA-Z0-9_']*))\s+where`,
	)

	// importRE matches: import Data.List
	//                   import Data.Vect as V
	importRE = regexp.MustCompile(
		`(?m)^import\s+([A-Z][a-zA-Z0-9_'.]*(?:\.[A-Z][a-zA-Z0-9_']*)*)(?:\s+as\s+[A-Z][a-zA-Z0-9_']*)?`,
	)

	// callRE detects function application — bare identifiers.
	callRE = regexp.MustCompile(
		`\b([a-z_][a-zA-Z0-9_']*)\b`,
	)
)

// idrisKeywords are tokens excluded from CALLS edges.
var idrisKeywords = map[string]bool{
	// Language keywords
	"module": true, "import": true, "as": true, "public": true, "private": true,
	"data": true, "record": true, "where": true, "interface": true, "implementation": true,
	"mutual": true, "namespace": true, "parameters": true, "using": true,
	"if": true, "then": true, "else": true, "case": true, "of": true,
	"let": true, "in": true, "do": true,
	"rewrite": true, "with": true, "impossible": true,
	"total": true, "partial": true, "covering": true, "assert_total": true,
	"export": true,
	"infixl": true, "infixr": true, "infix": true, "prefix": true,
	"implicit": true, "auto": true, "default": true, "hint": true,
	// Common identifiers that are effectively syntax
	"Type": true, "Void": true,
	"True": true, "False": true,
}

// Extract processes the Idris source and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	out := extractIdris(string(file.Content), file.Path)
	extractor.TagRelationshipsLanguage(out, "idris")
	extractor.TagEntitiesLanguage(out, "idris")
	return out, nil
}

func extractIdris(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord

	// Emit file-level entity.
	entities = append(entities, extractor.FileEntity(extractor.FileInput{
		Path:     filePath,
		Language: "idris",
	}))

	imports := collectImports(src)
	entities = append(entities, buildImportEntities(filePath, imports)...)

	importProp := strings.Join(imports, ",")

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
			Language:   "idris",
			StartLine:  startLine,
			EndLine:    startLine,
			Signature:  "module " + moduleName,
			Properties: map[string]string{"imports": importProp},
		})
	}

	// 2. Collect top-level function names.
	funcNames := collectFunctions(src)

	// 3. Emit SCOPE.Operation for each function.
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
		if sigText := findTypeSig(src, fn); sigText != "" {
			sig = fn + " : " + sigText
		}

		entities = append(entities, types.EntityRecord{
			Name:          fn,
			Kind:          "SCOPE.Operation",
			Subtype:       "function",
			SourceFile:    filePath,
			Language:      "idris",
			StartLine:     startLine,
			EndLine:       endLine,
			Signature:     sig,
			Properties:    map[string]string{"imports": importProp},
			Relationships: calls,
		})
	}

	// 4. Data type declarations.
	dataSeen := make(map[string]bool)
	for _, m := range dataRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		rawName := strings.TrimSpace(src[m[2]:m[3]])
		name := extractTypeName(rawName)
		if dataSeen[name] {
			continue
		}
		dataSeen[name] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "data",
			SourceFile: filePath,
			Language:   "idris",
			StartLine:  startLine,
			EndLine:    startLine,
			Signature:  "data " + name,
			Properties: map[string]string{"imports": importProp},
		})
	}

	// 5. Record declarations.
	recordSeen := make(map[string]bool)
	for _, m := range recordRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		rawName := strings.TrimSpace(src[m[2]:m[3]])
		name := extractTypeName(rawName)
		if recordSeen[name] {
			continue
		}
		recordSeen[name] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "record",
			SourceFile: filePath,
			Language:   "idris",
			StartLine:  startLine,
			EndLine:    startLine,
			Signature:  "record " + name,
			Properties: map[string]string{"imports": importProp},
		})
	}

	// 6. Interface declarations (type classes).
	interfaceSeen := make(map[string]bool)
	for _, m := range interfaceRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		rawName := strings.TrimSpace(src[m[2]:m[3]])
		name := extractTypeName(rawName)
		if name == "" || interfaceSeen[name] {
			continue
		}
		interfaceSeen[name] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "interface",
			SourceFile: filePath,
			Language:   "idris",
			StartLine:  startLine,
			EndLine:    startLine,
			Signature:  "interface " + name,
			Properties: map[string]string{"imports": importProp},
		})
	}

	// 7. Implementation declarations.
	for _, m := range implementationRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		ifaceName := strings.TrimSpace(src[m[2]:m[3]])
		typeArg := strings.TrimSpace(src[m[4]:m[5]])
		typeArg = strings.Trim(typeArg, "()")
		typeConstructor := extractTypeName(typeArg)

		implName := ifaceName + " " + typeConstructor
		startLine := strings.Count(src[:m[0]], "\n") + 1

		rels := []types.RelationshipRecord{
			{
				ToID: ifaceName,
				Kind: "IMPLEMENTS",
			},
		}
		entities = append(entities, types.EntityRecord{
			Name:          implName,
			Kind:          "SCOPE.Component",
			Subtype:       "implementation",
			SourceFile:    filePath,
			Language:      "idris",
			StartLine:     startLine,
			EndLine:       startLine,
			Signature:     "implementation " + ifaceName + " " + typeConstructor,
			Properties:    map[string]string{"imports": importProp},
			Relationships: rels,
		})
	}

	// 8. CONTAINS edges from module to top-level declarations.
	if moduleName != "" {
		var containsRels []types.RelationshipRecord
		for _, fn := range funcNames {
			ref := extractor.BuildOperationStructuralRef("idris", filePath, fn)
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
	for _, mod := range imports {
		displayName := mod
		if dot := strings.LastIndexByte(mod, '.'); dot >= 0 {
			displayName = mod[dot+1:]
		}
		out = append(out, types.EntityRecord{
			Name:       displayName,
			Kind:       "SCOPE.Component",
			SourceFile: filePath,
			Language:   "idris",
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

// -----------------------------------------------------------------------
// Helper: function discovery
// -----------------------------------------------------------------------

func collectFunctions(src string) []string {
	seen := make(map[string]bool)
	var funcs []string

	// First pass: type signatures (single colon in Idris).
	for _, m := range typeSigRE.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		names := strings.Split(m[1], ",")
		for _, name := range names {
			name = strings.TrimSpace(name)
			if name == "" || seen[name] || idrisKeywords[name] {
				continue
			}
			seen[name] = true
			funcs = append(funcs, name)
		}
	}

	// Second pass: function definitions at column 0.
	for _, m := range funcDefRE.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		name := m[1]
		if name == "" || seen[name] || idrisKeywords[name] {
			continue
		}
		if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
			continue
		}
		seen[name] = true
		funcs = append(funcs, name)
	}

	return funcs
}

func findFunctionPos(src, name string) int {
	// Idris uses single colon for type signatures.
	sigPattern := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `\s*:`)
	if loc := sigPattern.FindStringIndex(src); loc != nil {
		return loc[0]
	}
	defPattern := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `\s+`)
	if loc := defPattern.FindStringIndex(src); loc != nil {
		return loc[0]
	}
	return -1
}

func findTypeSig(src, name string) string {
	sigPattern := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `(?:\s*,\s*[a-z_][a-zA-Z0-9_']*)?\s*:\s+(.+)`)
	if m := sigPattern.FindStringSubmatch(src); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func extractFunctionBody(src, name string) string {
	defPattern := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `(?:[^:]|:[^:]|$)`)
	locs := defPattern.FindAllStringIndex(src, -1)
	if len(locs) == 0 {
		return ""
	}
	var bodies []string
	for _, loc := range locs {
		lineEnd := strings.Index(src[loc[0]:], "\n")
		var lineText string
		if lineEnd >= 0 {
			lineText = src[loc[0] : loc[0]+lineEnd]
		} else {
			lineText = src[loc[0]:]
		}
		// Skip type signatures (single colon not followed by another colon).
		if strings.Contains(lineText, " : ") && !strings.Contains(lineText, "=") {
			continue
		}
		bodies = append(bodies, extractIndentBody(src, loc[1]))
	}
	return strings.Join(bodies, "\n")
}

func extractIndentBody(src string, afterPos int) string {
	if afterPos >= len(src) {
		return ""
	}
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

func collectCalls(body, callerName string) []types.RelationshipRecord {
	if body == "" {
		return nil
	}
	scrubbed := stripStringsAndComments(body)
	matches := callRE.FindAllStringSubmatch(scrubbed, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	var out []types.RelationshipRecord
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		target := m[1]
		if target == "" || target == callerName {
			continue
		}
		if idrisKeywords[target] {
			continue
		}
		if len(target) <= 1 {
			continue
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		out = append(out, types.RelationshipRecord{
			ToID: target,
			Kind: "CALLS",
		})
	}
	return out
}

// stripStringsAndComments replaces string literals and -- line comments
// and {- block comments -} with spaces (same as Haskell comment style).
func stripStringsAndComments(src string) string {
	out := make([]byte, len(src))
	i := 0
	inStr := false

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

		// Line comment: -- to end of line
		if ch == '-' && i+1 < len(src) && src[i+1] == '-' {
			for i < len(src) && src[i] != '\n' {
				out[i] = ' '
				i++
			}
			continue
		}

		// Block comment: {- ... -}
		if ch == '{' && i+1 < len(src) && src[i+1] == '-' {
			out[i] = ' '
			out[i+1] = ' '
			i += 2
			for i < len(src) {
				if src[i] == '-' && i+1 < len(src) && src[i+1] == '}' {
					out[i] = ' '
					out[i+1] = ' '
					i += 2
					break
				}
				out[i] = ' '
				i++
			}
			continue
		}

		switch ch {
		case '"':
			inStr = true
			out[i] = ' '
		default:
			out[i] = ch
		}
		i++
	}
	return string(out)
}
