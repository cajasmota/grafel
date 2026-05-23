// Package pony implements a regex-based extractor for Pony source files.
//
// Pony is an actor-based, capability-secure language with ML-influenced syntax.
// Reference: https://www.ponylang.io/
//
// Extracted entities:
//   - actor/class/primitive/type/interface/trait/struct/object declarations
//     → SCOPE.Component (subtype = actor|class|primitive|type|interface|trait|struct|object)
//   - `fun name(args): R => body`  → SCOPE.Operation (subtype="function")
//   - `new name(...)` (constructor) → SCOPE.Operation (subtype="constructor")
//   - `be name(args) => body` (behaviors / async messages) → SCOPE.Operation (subtype="behavior")
//   - `use "package"` → IMPORTS edges
//   - CALLS edges (deduped, from function/behavior/constructor bodies)
//   - CONTAINS edges (type declaration → member operations)
//
// File extension: .pony
//
// No tree-sitter grammar for Pony is bundled in smacker/go-tree-sitter, so
// this extractor uses regular expressions with indentation heuristics.
//
// Registers itself via init() and is imported by registry_gen.go.
package pony

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("pony", &Extractor{})
}

// Extractor implements extractor.Extractor for Pony.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "pony" }

// -----------------------------------------------------------------------
// Compiled regex patterns
// -----------------------------------------------------------------------

var (
	// typeDeclarationRE matches top-level Pony type declarations:
	//   actor MyActor
	//   class MyClass
	//   primitive MyPrimitive
	//   interface Foo
	//   trait Bar
	//   struct Point
	//   type MyAlias is ...
	// Groups: [1] keyword, [2] name (optional generic params stripped)
	typeDeclarationRE = regexp.MustCompile(
		`(?m)^(actor|class|primitive|interface|trait|struct)\s+([A-Za-z_][A-Za-z0-9_]*)`,
	)

	// typeAliasRE matches: type Foo is Bar
	typeAliasRE = regexp.MustCompile(
		`(?m)^type\s+([A-Za-z_][A-Za-z0-9_]*)`,
	)

	// funRE matches function declarations inside a type body:
	//   fun ref greet(name: String): String =>
	//   fun box size(): USize =>
	//   fun apply() =>
	// Groups: [1] name
	funRE = regexp.MustCompile(
		`(?m)^[ \t]+fun\s+(?:ref\s+|box\s+|val\s+|tag\s+|iso\s+|trn\s+)?([a-z_][a-zA-Z0-9_']*)\s*(?:\[.*?\])?\s*\(`,
	)

	// constructorRE matches constructor declarations:
	//   new ref create(...)
	//   new create(...)
	// Groups: [1] name
	constructorRE = regexp.MustCompile(
		`(?m)^[ \t]+new\s+(?:ref\s+|iso\s+|val\s+|tag\s+|box\s+|trn\s+)?([a-z_][a-zA-Z0-9_']*)\s*\(`,
	)

	// behaviorRE matches behavior (async message) declarations:
	//   be run(value: String) =>
	//   be apply() =>
	// Groups: [1] name
	behaviorRE = regexp.MustCompile(
		`(?m)^[ \t]+be\s+([a-z_][a-zA-Z0-9_']*)\s*\(`,
	)

	// useRE matches Pony use statements:
	//   use "collections"
	//   use col = "collections"
	//   use "files"
	// Groups: [1] package string (may have alias prefix stripped)
	useRE = regexp.MustCompile(
		`(?m)^use\s+(?:[a-zA-Z_][a-zA-Z0-9_]*\s*=\s*)?"([^"]+)"`,
	)

	// callRE matches bare identifier calls (for CALLS edges).
	callRE = regexp.MustCompile(
		`\b([a-z_][a-zA-Z0-9_']*)\s*\(`,
	)
)

// ponyKeywords are tokens excluded from CALLS edges.
var ponyKeywords = map[string]bool{
	"actor": true, "class": true, "primitive": true, "interface": true,
	"trait": true, "struct": true, "type": true,
	"fun": true, "be": true, "new": true,
	"let": true, "var": true, "embed": true,
	"if": true, "elseif": true, "else": true, "end": true,
	"for": true, "while": true, "repeat": true, "until": true,
	"do": true, "break": true, "continue": true, "return": true,
	"try": true, "with": true, "then": true, "error": true,
	"recover": true, "consume": true, "match": true, "where": true,
	"as": true, "is": true, "isnt": true, "and": true, "or": true,
	"not": true, "xor": true,
	"use": true, "compile_intrinsic": true, "compile_error": true,
	"object": true,
	"ifdef":  true, "iftype": true,
}

// Extract processes the Pony source and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	out := extractPony(string(file.Content), file.Path)
	extractor.TagRelationshipsLanguage(out, "pony")
	return out, nil
}

func extractPony(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord

	// Emit file-level entity.
	entities = append(entities, extractor.FileEntity(extractor.FileInput{
		Path:     filePath,
		Language: "pony",
	}))

	// 1. Collect use/import statements.
	imports := collectUses(src)
	entities = append(entities, buildImportEntities(filePath, imports)...)

	importProp := strings.Join(imports, ",")

	// 2. Top-level type declarations (actor/class/primitive/interface/trait/struct).
	for _, m := range typeDeclarationRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		keyword := src[m[2]:m[3]]
		name := src[m[4]:m[5]]
		startLine := strings.Count(src[:m[0]], "\n") + 1

		// Extract the body block for this type to find members.
		body := extractPonyBlock(src, m[1])
		endLine := startLine + strings.Count(body, "\n")

		// Collect member operations (fun/be/new) within this body.
		var containsRels []types.RelationshipRecord

		// Functions within this type body.
		for _, fm := range funRE.FindAllStringSubmatchIndex(body, -1) {
			if len(fm) < 4 {
				continue
			}
			fnName := body[fm[2]:fm[3]]
			fnLine := startLine + strings.Count(body[:fm[0]], "\n")
			fnBody := extractIndentBody(body, fm[1])
			fnEndLine := fnLine + strings.Count(fnBody, "\n")
			calls := collectCalls(fnBody, fnName)

			ref := extractor.BuildOperationStructuralRef("pony", filePath, name+"."+fnName)
			containsRels = append(containsRels, types.RelationshipRecord{
				ToID: ref,
				Kind: "CONTAINS",
			})
			entities = append(entities, types.EntityRecord{
				Name:          name + "." + fnName,
				Kind:          "SCOPE.Operation",
				Subtype:       "function",
				SourceFile:    filePath,
				Language:      "pony",
				StartLine:     fnLine,
				EndLine:       fnEndLine,
				Signature:     "fun " + fnName,
				Properties:    map[string]string{"imports": importProp},
				Relationships: calls,
			})
		}

		// Behaviors within this type body.
		for _, bm := range behaviorRE.FindAllStringSubmatchIndex(body, -1) {
			if len(bm) < 4 {
				continue
			}
			beName := body[bm[2]:bm[3]]
			beLine := startLine + strings.Count(body[:bm[0]], "\n")
			beBody := extractIndentBody(body, bm[1])
			beEndLine := beLine + strings.Count(beBody, "\n")
			calls := collectCalls(beBody, beName)

			ref := extractor.BuildOperationStructuralRef("pony", filePath, name+"."+beName)
			containsRels = append(containsRels, types.RelationshipRecord{
				ToID: ref,
				Kind: "CONTAINS",
			})
			entities = append(entities, types.EntityRecord{
				Name:          name + "." + beName,
				Kind:          "SCOPE.Operation",
				Subtype:       "behavior",
				SourceFile:    filePath,
				Language:      "pony",
				StartLine:     beLine,
				EndLine:       beEndLine,
				Signature:     "be " + beName,
				Properties:    map[string]string{"imports": importProp},
				Relationships: calls,
			})
		}

		// Constructors within this type body.
		for _, cm := range constructorRE.FindAllStringSubmatchIndex(body, -1) {
			if len(cm) < 4 {
				continue
			}
			ctorName := body[cm[2]:cm[3]]
			ctorLine := startLine + strings.Count(body[:cm[0]], "\n")
			ctorBody := extractIndentBody(body, cm[1])
			ctorEndLine := ctorLine + strings.Count(ctorBody, "\n")
			calls := collectCalls(ctorBody, ctorName)

			ref := extractor.BuildOperationStructuralRef("pony", filePath, name+"."+ctorName)
			containsRels = append(containsRels, types.RelationshipRecord{
				ToID: ref,
				Kind: "CONTAINS",
			})
			entities = append(entities, types.EntityRecord{
				Name:          name + "." + ctorName,
				Kind:          "SCOPE.Operation",
				Subtype:       "constructor",
				SourceFile:    filePath,
				Language:      "pony",
				StartLine:     ctorLine,
				EndLine:       ctorEndLine,
				Signature:     "new " + ctorName,
				Properties:    map[string]string{"imports": importProp},
				Relationships: calls,
			})
		}

		entities = append(entities, types.EntityRecord{
			Name:          name,
			Kind:          "SCOPE.Component",
			Subtype:       keyword,
			SourceFile:    filePath,
			Language:      "pony",
			StartLine:     startLine,
			EndLine:       endLine,
			Signature:     keyword + " " + name,
			Properties:    map[string]string{"imports": importProp},
			Relationships: containsRels,
		})
	}

	// 3. Type aliases.
	for _, m := range typeAliasRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		name := src[m[2]:m[3]]
		startLine := strings.Count(src[:m[0]], "\n") + 1
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "type",
			SourceFile: filePath,
			Language:   "pony",
			StartLine:  startLine,
			EndLine:    startLine,
			Signature:  "type " + name,
			Properties: map[string]string{"imports": importProp},
		})
	}

	return entities
}

// -----------------------------------------------------------------------
// Helper: use/import collection
// -----------------------------------------------------------------------

func collectUses(src string) []string {
	seen := make(map[string]bool)
	var uses []string
	for _, m := range useRE.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		pkg := strings.TrimSpace(m[1])
		if pkg == "" || seen[pkg] {
			continue
		}
		seen[pkg] = true
		uses = append(uses, pkg)
	}
	return uses
}

func buildImportEntities(filePath string, uses []string) []types.EntityRecord {
	if len(uses) == 0 {
		return nil
	}
	out := make([]types.EntityRecord, 0, len(uses))
	for _, pkg := range uses {
		displayName := pkg
		if idx := strings.LastIndexByte(pkg, '/'); idx >= 0 {
			displayName = pkg[idx+1:]
		}
		out = append(out, types.EntityRecord{
			Name:       displayName,
			Kind:       "SCOPE.Component",
			SourceFile: filePath,
			Language:   "pony",
			Relationships: []types.RelationshipRecord{
				{
					FromID: filePath,
					ToID:   pkg,
					Kind:   "IMPORTS",
				},
			},
		})
	}
	return out
}

// -----------------------------------------------------------------------
// Helper: block / body extraction
// -----------------------------------------------------------------------

// extractPonyBlock extracts the indented block that follows a type declaration.
// Pony uses "end" as the terminator, but for our purposes extracting indented
// lines is a reliable heuristic for member discovery.
func extractPonyBlock(src string, afterPos int) string {
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
		// Stop at the next top-level (non-indented) keyword.
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			// Allow "end" to be included.
			stripped := strings.TrimSpace(line)
			if stripped == "end" {
				body = append(body, line)
			}
			break
		}
		body = append(body, line)
	}
	return strings.Join(body, "\n")
}

// extractIndentBody collects all indented continuation lines after afterPos.
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
		if ponyKeywords[target] {
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

// stripStringsAndComments replaces string/char literals and // and /* */ comments
// with spaces to avoid false call detection in Pony source.
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

		// Line comment: // to end of line
		if ch == '/' && i+1 < len(src) && src[i+1] == '/' {
			for i < len(src) && src[i] != '\n' {
				out[i] = ' '
				i++
			}
			continue
		}

		// Block comment: /* ... */
		if ch == '/' && i+1 < len(src) && src[i+1] == '*' {
			out[i] = ' '
			out[i+1] = ' '
			i += 2
			for i < len(src) {
				if src[i] == '*' && i+1 < len(src) && src[i+1] == '/' {
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
