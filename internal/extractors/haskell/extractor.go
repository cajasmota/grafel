// Package haskell implements a regex-based extractor for Haskell source files.
//
// Extracted entities:
//   - Module declarations (`module Foo.Bar where`) → SCOPE.Component (subtype="module")
//   - Function type signatures (`name :: Type`) combined with definitions → SCOPE.Operation
//   - Data type declarations (`data Maybe a = ...`) → SCOPE.Component (subtype="data")
//   - Newtype declarations (`newtype Wrapper a = ...`) → SCOPE.Component (subtype="newtype")
//   - Type synonym declarations (`type Alias = ...`) → SCOPE.Component (subtype="type")
//   - Type class declarations (`class Foo a where`) → SCOPE.Component (subtype="typeclass")
//   - Type class instance declarations → SCOPE.Component (subtype="instance") + IMPLEMENTS edges
//   - Import statements → IMPORTS edges
//   - CALLS edges (deduped, from function bodies; excludes keywords/operators)
//   - CONTAINS edges (module → top-level declarations)
//
// No tree-sitter grammar for Haskell is bundled in smacker/go-tree-sitter, so
// this extractor uses regular expressions with indentation heuristics. Haskell
// is layout-rule sensitive, but for entity-discovery purposes we detect
// top-level declarations by their starting at column 0, which is Haskell's
// layout convention for top-level bindings.
//
// Registers itself via init() and is imported by registry_gen.go.
package haskell

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("haskell", &Extractor{})
}

// Extractor implements extractor.Extractor for Haskell.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "haskell" }

// -----------------------------------------------------------------------
// Compiled regex patterns
// -----------------------------------------------------------------------

var (
	// moduleRE matches: module Foo.Bar.Baz where  or  module Foo where
	// The module name can contain dots and apostrophes (e.g. Data.Map.Strict).
	moduleRE = regexp.MustCompile(
		`(?m)^module\s+((?:[A-Z][a-zA-Z0-9_']*\.)*[A-Z][a-zA-Z0-9_']*)\s+where`,
	)

	// typeSigRE matches top-level type signatures:
	//   functionName :: Type -> Type
	// Haskell names can start with lowercase or be operators in parens.
	// We only match lowercase-starting identifiers (operators are excluded).
	typeSigRE = regexp.MustCompile(
		`(?m)^([a-z_][a-zA-Z0-9_']*(?:\s*,\s*[a-z_][a-zA-Z0-9_']*)*)\s*::\s*(.+)`,
	)

	// funcDefRE matches a top-level function definition (LHS = ...).
	// We capture the name at column 0.
	funcDefRE = regexp.MustCompile(
		`(?m)^([a-z_][a-zA-Z0-9_']*)\s+(?:[^\n=]*=|=)`,
	)

	// dataRE matches: data Maybe a = Nothing | Just a
	//                 data Map k v = ...
	//                 data Foo = Foo { ... } deriving (...)
	dataRE = regexp.MustCompile(
		`(?m)^data\s+([A-Z][a-zA-Z0-9_']*(?:\s+[a-zA-Z][a-zA-Z0-9_']*)*)\s*(?:where|=)`,
	)

	// newtypeRE matches: newtype Wrapper a = Wrapper { unWrap :: a }
	newtypeRE = regexp.MustCompile(
		`(?m)^newtype\s+([A-Z][a-zA-Z0-9_']*(?:\s+[a-zA-Z][a-zA-Z0-9_']*)*)\s*=`,
	)

	// typeSynonymRE matches: type Alias = Int
	// (but not "type class" — that's handled by classRE)
	typeSynonymRE = regexp.MustCompile(
		`(?m)^type\s+([A-Z][a-zA-Z0-9_']*(?:\s+[a-zA-Z][a-zA-Z0-9_']*)*)\s*=`,
	)

	// classRE matches: class (Eq a) => Foo a where
	//                  class Foo a where
	classRE = regexp.MustCompile(
		`(?m)^class\s+(?:\([^)]*\)\s*=>\s*)?([A-Z][a-zA-Z0-9_']*(?:\s+[a-zA-Z][a-zA-Z0-9_']*)*)?\s+where`,
	)

	// instanceRE matches: instance Show MyType where
	//                     instance (Eq a) => Ord (Maybe a) where
	instanceRE = regexp.MustCompile(
		`(?m)^instance\s+(?:\([^)]*\)\s*=>\s*)?([A-Z][a-zA-Z0-9_']*)\s+((?:\([^)]*\)|[A-Z][a-zA-Z0-9_']*(?:\s+[a-z][a-zA-Z0-9_']*)*|[a-z][a-zA-Z0-9_']*))\s+where`,
	)

	// importRE matches: import Data.Map
	//                   import qualified Data.Map as M
	//                   import Data.Map (lookup, insert)
	//                   import Data.Map hiding (lookup)
	importRE = regexp.MustCompile(
		`(?m)^import\s+(?:qualified\s+)?([A-Z][a-zA-Z0-9_'.]*(?:\.[A-Z][a-zA-Z0-9_']*)*)(?:\s+as\s+[A-Z][a-zA-Z0-9_']*)?`,
	)

	// callRE detects bare function application: identifier(  or  identifier <space>
	// We pick up names that appear as potential function calls in bodies.
	callRE = regexp.MustCompile(
		`\b([a-z_][a-zA-Z0-9_']*)\b`,
	)
)

// haskellKeywords is the set of tokens to exclude from CALLS edges.
var haskellKeywords = map[string]bool{
	// Language keywords
	"case": true, "of": true, "where": true, "let": true, "in": true,
	"if": true, "then": true, "else": true, "do": true,
	"module": true, "import": true, "qualified": true, "as": true, "hiding": true,
	"data": true, "newtype": true, "type": true, "class": true, "instance": true,
	"deriving": true, "via": true, "stock": true, "anyclass": true,
	"infixl": true, "infixr": true, "infix": true,
	"forall": true, "family": true,
	// Common identifiers that are effectively syntax
	"otherwise": true, "undefined": true, "error": true,
	// Literals
	"True": true, "False": true,
}

// Extract processes the Haskell source and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	out := extractHaskell(string(file.Content), file.Path)
	extractor.TagRelationshipsLanguage(out, "haskell")
	return out, nil
}

func extractHaskell(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord

	// Emit file-level entity (issue #577 pattern).
	entities = append(entities, extractor.FileEntity(extractor.FileInput{
		Path:     filePath,
		Language: "haskell",
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
			Language:   "haskell",
			StartLine:  startLine,
			EndLine:    startLine,
			Signature:  "module " + moduleName + " where",
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
		})
	}

	// 2. Collect all top-level function names (from type signatures + definitions).
	funcNames := collectFunctions(src)

	// 3. Emit SCOPE.Operation entities for each function.
	for _, fn := range funcNames {
		// Find the position of the type signature or first definition.
		sigPos := findFunctionPos(src, fn)
		startLine := 1
		if sigPos >= 0 {
			startLine = strings.Count(src[:sigPos], "\n") + 1
		}
		body := extractFunctionBody(src, fn)
		endLine := startLine + strings.Count(body, "\n")
		calls := collectCalls(body, fn)

		var sig string
		// Find type signature for this function.
		sigText := findTypeSig(src, fn)
		if sigText != "" {
			sig = fn + " :: " + sigText
		} else {
			sig = fn
		}

		var rels []types.RelationshipRecord
		rels = append(rels, calls...)

		// If we have a module, add CONTAINS edge from module to function.
		if moduleName != "" {
			// The CONTAINS edge goes on the module entity; we'll add it below.
			_ = moduleName // handled after
		}

		entities = append(entities, types.EntityRecord{
			Name:       fn,
			Kind:       "SCOPE.Operation",
			Subtype:    "function",
			SourceFile: filePath,
			Language:   "haskell",
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  sig,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
			Relationships: rels,
		})
	}

	// 4. Data type declarations.
	dataSeen := make(map[string]bool)
	for _, m := range dataRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		rawName := strings.TrimSpace(src[m[2]:m[3]])
		// The raw name may contain type vars; extract just the type constructor name.
		name := extractTypeName(rawName)
		if dataSeen[name] {
			continue
		}
		dataSeen[name] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		body := extractIndentBody(src, m[1])
		endLine := startLine + strings.Count(body, "\n")

		// Find constructor names from the RHS.
		constructors := extractDataConstructors(src, m[0])

		var rels []types.RelationshipRecord
		for _, ctor := range constructors {
			ctorRef := extractor.BuildOperationStructuralRef("haskell", filePath, ctor)
			rels = append(rels, types.RelationshipRecord{
				ToID: ctorRef,
				Kind: "CONTAINS",
			})
		}

		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "data",
			SourceFile: filePath,
			Language:   "haskell",
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  "data " + name,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
			Relationships: rels,
		})
	}

	// 5. Newtype declarations.
	newtypeSeen := make(map[string]bool)
	for _, m := range newtypeRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		rawName := strings.TrimSpace(src[m[2]:m[3]])
		name := extractTypeName(rawName)
		if newtypeSeen[name] {
			continue
		}
		newtypeSeen[name] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "newtype",
			SourceFile: filePath,
			Language:   "haskell",
			StartLine:  startLine,
			EndLine:    startLine,
			Signature:  "newtype " + name,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
		})
	}

	// 6. Type synonym declarations (avoid matching "type family", "type instance").
	typeSeen := make(map[string]bool)
	for _, m := range typeSynonymRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		rawName := strings.TrimSpace(src[m[2]:m[3]])
		name := extractTypeName(rawName)
		// Skip "family" — it's a type family, not a synonym.
		if name == "family" || name == "instance" || name == "role" {
			continue
		}
		if typeSeen[name] {
			continue
		}
		typeSeen[name] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "type",
			SourceFile: filePath,
			Language:   "haskell",
			StartLine:  startLine,
			EndLine:    startLine,
			Signature:  "type " + name,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
		})
	}

	// 7. Type class declarations.
	classSeen := make(map[string]bool)
	for _, m := range classRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		rawName := strings.TrimSpace(src[m[2]:m[3]])
		name := extractTypeName(rawName)
		if name == "" || classSeen[name] {
			continue
		}
		classSeen[name] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		body := extractIndentBody(src, m[1])
		endLine := startLine + strings.Count(body, "\n")
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "typeclass",
			SourceFile: filePath,
			Language:   "haskell",
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  "class " + name,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
		})
	}

	// 8. Type class instance declarations.
	for _, m := range instanceRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		className := strings.TrimSpace(src[m[2]:m[3]])
		typeArg := strings.TrimSpace(src[m[4]:m[5]])
		// Normalize: strip parens from type args like "(Maybe a)"
		typeArg = strings.Trim(typeArg, "()")
		typeArg = strings.TrimSpace(typeArg)
		// Extract just the type constructor name.
		typeConstructor := extractTypeName(typeArg)

		instanceName := fmt.Sprintf("%s %s", className, typeConstructor)
		startLine := strings.Count(src[:m[0]], "\n") + 1

		rels := []types.RelationshipRecord{
			{
				ToID: className,
				Kind: "IMPLEMENTS",
			},
		}

		entities = append(entities, types.EntityRecord{
			Name:       instanceName,
			Kind:       "SCOPE.Component",
			Subtype:    "instance",
			SourceFile: filePath,
			Language:   "haskell",
			StartLine:  startLine,
			EndLine:    startLine,
			Signature:  fmt.Sprintf("instance %s %s", className, typeConstructor),
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
			Relationships: rels,
		})
	}

	// 9. Add CONTAINS edges from module to all top-level declarations.
	if moduleName != "" {
		var containsRels []types.RelationshipRecord
		for _, fn := range funcNames {
			ref := extractor.BuildOperationStructuralRef("haskell", filePath, fn)
			containsRels = append(containsRels, types.RelationshipRecord{
				ToID: ref,
				Kind: "CONTAINS",
			})
		}
		// Find the module entity and attach CONTAINS edges.
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
			Language:   "haskell",
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

// collectFunctions returns deduplicated top-level function names, combining
// type signatures and definitions. Type signatures take priority for ordering.
func collectFunctions(src string) []string {
	seen := make(map[string]bool)
	var funcs []string

	// First pass: type signatures (most reliable — Haskell functions almost always
	// have a type signature if they're top-level exported or major).
	for _, m := range typeSigRE.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		// Handle comma-separated names: "f, g :: Type"
		names := strings.Split(m[1], ",")
		for _, name := range names {
			name = strings.TrimSpace(name)
			if name == "" || seen[name] || haskellKeywords[name] {
				continue
			}
			seen[name] = true
			funcs = append(funcs, name)
		}
	}

	// Second pass: function definitions at column 0 (catches definitions without sigs).
	for _, m := range funcDefRE.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		name := m[1]
		if name == "" || seen[name] || haskellKeywords[name] {
			continue
		}
		// Skip names that look like type constructors or imports.
		if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
			continue
		}
		seen[name] = true
		funcs = append(funcs, name)
	}

	return funcs
}

// findFunctionPos returns the byte offset of the function's type signature
// or first definition in src.
func findFunctionPos(src, name string) int {
	// Try type signature first.
	sigPattern := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `\s*::`)
	if loc := sigPattern.FindStringIndex(src); loc != nil {
		return loc[0]
	}
	// Fall back to first definition.
	defPattern := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `\s+`)
	if loc := defPattern.FindStringIndex(src); loc != nil {
		return loc[0]
	}
	return -1
}

// findTypeSig returns the type annotation string for a function name.
func findTypeSig(src, name string) string {
	sigPattern := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `(?:\s*,\s*[a-z_][a-zA-Z0-9_']*)?\s*::\s*(.+)`)
	if m := sigPattern.FindStringSubmatch(src); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// extractFunctionBody returns the concatenated bodies of all definition
// equations for a function. In Haskell, a top-level function may have
// multiple equations (pattern matching). We collect lines from the first
// definition through all continuation lines.
func extractFunctionBody(src, name string) string {
	// Only match definition lines (not type signatures) — the definition
	// starts with the name at column 0 followed by a space, (, |, or = but NOT ::
	defPattern := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `(?:[^:]|:[^:]|$)`)
	locs := defPattern.FindAllStringIndex(src, -1)
	if len(locs) == 0 {
		return ""
	}
	var bodies []string
	for _, loc := range locs {
		// Skip if this is actually a type signature (contains ::).
		lineEnd := strings.Index(src[loc[0]:], "\n")
		var lineText string
		if lineEnd >= 0 {
			lineText = src[loc[0] : loc[0]+lineEnd]
		} else {
			lineText = src[loc[0]:]
		}
		if strings.Contains(lineText, "::") {
			continue
		}
		bodies = append(bodies, extractIndentBody(src, loc[1]))
	}
	return strings.Join(bodies, "\n")
}

// extractIndentBody collects all lines following afterPos that are indented
// (i.e., don't start at column 0 with a non-space character), representing
// the body of a Haskell layout block.
func extractIndentBody(src string, afterPos int) string {
	rest := src[afterPos:]
	lines := strings.Split(rest, "\n")
	var body []string
	for i, line := range lines {
		if i == 0 {
			// The rest of the first line is always part of the body.
			body = append(body, line)
			continue
		}
		if line == "" || strings.TrimSpace(line) == "" {
			body = append(body, line)
			continue
		}
		// In Haskell, top-level items start at column 0 with a non-space char.
		// A continuation line starts with at least one space.
		if line[0] == ' ' || line[0] == '\t' {
			body = append(body, line)
		} else {
			// Reached the next top-level declaration.
			break
		}
	}
	return strings.Join(body, "\n")
}

// -----------------------------------------------------------------------
// Helper: type name extraction
// -----------------------------------------------------------------------

// extractTypeName extracts just the type constructor name from a raw name
// string that may include type variables (e.g. "Maybe a" → "Maybe").
func extractTypeName(raw string) string {
	raw = strings.TrimSpace(raw)
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

// -----------------------------------------------------------------------
// Helper: data constructor extraction
// -----------------------------------------------------------------------

// extractDataConstructors extracts the names of data constructors from a
// data declaration (the RHS of "data Foo = ...").
var dataCtorRE = regexp.MustCompile(`[|=]\s*([A-Z][a-zA-Z0-9_']*)`)

func extractDataConstructors(src string, declStart int) []string {
	// Find the end of the data declaration (next top-level decl).
	rest := src[declStart:]
	endIdx := len(rest)
	// Look for the first line that starts at column 0 and is a new declaration.
	topLevelRE := regexp.MustCompile(`\n([a-zA-Z\-{#])`)
	if loc := topLevelRE.FindStringIndex(rest[1:]); loc != nil {
		endIdx = loc[0] + 2 // +2 to include the \n
	}
	block := rest[:endIdx]

	seen := make(map[string]bool)
	var ctors []string
	for _, m := range dataCtorRE.FindAllStringSubmatch(block, -1) {
		if len(m) < 2 {
			continue
		}
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		ctors = append(ctors, name)
	}
	return ctors
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
	matches := callRE.FindAllStringSubmatch(scrubbed, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	out := make([]types.RelationshipRecord, 0, len(matches))
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		target := m[1]
		if target == "" || target == callerName {
			continue
		}
		if haskellKeywords[target] {
			continue
		}
		// Skip very short names (likely variables, not functions).
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
// (and {- block comments -}) with spaces to avoid false call detection.
func stripStringsAndComments(src string) string {
	out := make([]byte, len(src))
	i := 0
	inStr := false
	inChar := false

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
