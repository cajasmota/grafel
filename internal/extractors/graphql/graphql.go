// Package graphql implements a regex-based extractor for GraphQL schema/operation files.
//
// Extracted entities:
//   - type  definitions        → Kind="SCOPE.Schema", Subtype="type"
//   - interface definitions    → Kind="SCOPE.Schema", Subtype="interface"
//   - enum  definitions        → Kind="SCOPE.Schema", Subtype="enum"
//   - union definitions        → Kind="SCOPE.Schema", Subtype="union"
//   - input definitions        → Kind="SCOPE.Schema", Subtype="input"
//   - scalar definitions       → Kind="SCOPE.Schema", Subtype="scalar"
//   - query operations         → Kind="SCOPE.Schema", Subtype="query"
//   - mutation operations      → Kind="SCOPE.Schema", Subtype="mutation"
//   - subscription operations  → Kind="SCOPE.Schema", Subtype="subscription"
//   - fragment definitions     → Kind="SCOPE.Schema", Subtype="fragment"
//   - field declarations       → Kind="SCOPE.Component", Subtype="field"
//   - extend-type import stubs → Kind="SCOPE.Component", Subtype="import"
//
// Issue #385 (PORT-RELS-GRAPHQL) — emits two relationship kinds:
//
//   - CONTAINS: the file acts as the structural container for every top-level
//     definition (types, interfaces, enums, unions, inputs, scalars, queries,
//     mutations, subscriptions, fragments). type/interface/input definitions
//     additionally CONTAIN each declared field. CONTAINS ToIDs use the
//     canonical Format-A structural-ref shape
//     `scope:operation:method:graphql:<file>:<name>` via
//     extractor.BuildOperationStructuralRef (Format A, #144).
//
//   - IMPORTS: federation `extend type Foo` directives become SCOPE.Component
//     import-stub entities carrying a single IMPORTS edge from the source file
//     → the extended type name. Fragment spreads (`...FragmentName`) inside
//     operation/fragment bodies emit IMPORTS edges from the operation/fragment
//     to the spread fragment name. Properties carry
//     {source_module, import_kind} matching the contract used by the other
//     ported extractors.
//
// No tree-sitter grammar for GraphQL is bundled in smacker/go-tree-sitter.
// Registers itself via init() and is imported by registry_gen.go.
package graphql

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("graphql", &Extractor{})
}

// Extractor implements extractor.Extractor for GraphQL.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "graphql" }

// Patterns for GraphQL constructs.
var (
	// type Foo { / interface Foo { / enum Foo { / union Foo / input Foo { / scalar Foo
	typeDefRE = regexp.MustCompile(
		`(?m)^(type|interface|enum|union|input|scalar)\s+(\w+)`,
	)
	// extend type Foo … — federation external-type extension.
	extendTypeRE = regexp.MustCompile(
		`(?m)^extend\s+(?:type|interface|enum|union|input)\s+(\w+)`,
	)
	// query|mutation|subscription Name(
	operationRE = regexp.MustCompile(
		`(?m)^(query|mutation|subscription)\s+(\w+)`,
	)
	// fragment Name on Type {
	fragmentRE = regexp.MustCompile(
		`(?m)^fragment\s+(\w+)\s+on\s+\w+`,
	)
	// `...FragmentName` — fragment spread inside an operation/fragment body.
	// The leading dots must not be preceded by another non-dot character on
	// the same token (so `....Foo` would not match, but `...Foo` does).
	fragmentSpreadRE = regexp.MustCompile(
		`\.\.\.([A-Za-z_]\w*)`,
	)
	// Field declaration inside a type/interface/input body. Match the leading
	// identifier of a non-blank line followed by a colon and a type. We reject
	// lines that look like nested directives (starting with `@`) or the
	// closing brace.
	fieldRE = regexp.MustCompile(
		`(?m)^[ \t]+([A-Za-z_]\w*)\s*(?:\([^)]*\))?\s*:\s*[^\n]+`,
	)
)

// Extract processes the GraphQL source and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	entities := extractGraphQL(string(file.Content), file.Path)
	// Issue #90 — language tag for resolver dynamic-pattern dispatch.
	extractor.TagRelationshipsLanguage(entities, "graphql")
	return entities, nil
}

func extractGraphQL(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	// File-level container entity. Inserted at index 0 so subsequent CONTAINS
	// edges for top-level definitions can be appended to entities[0].
	entities = append(entities, types.EntityRecord{
		Name:       filePath,
		Kind:       "SCOPE.Component",
		Subtype:    "file",
		SourceFile: filePath,
		Language:   "graphql",
	})

	addFileContains := func(name string) {
		toID := extractor.BuildOperationStructuralRef("graphql", filePath, name)
		entities[0].Relationships = append(entities[0].Relationships, types.RelationshipRecord{
			FromID: filePath,
			ToID:   toID,
			Kind:   "CONTAINS",
		})
	}

	// Type system definitions.
	for _, m := range typeDefRE.FindAllStringSubmatchIndex(src, -1) {
		subtype := src[m[2]:m[3]]
		name := src[m[4]:m[5]]
		key := "def:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine, bodyStart, bodyEnd := findBlockBounds(src, m[0])

		ent := types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Schema",
			Subtype:            subtype,
			SourceFile:         filePath,
			Language:           "graphql",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          subtype + " " + name,
			EnrichmentRequired: false,
		}

		// Fields are only meaningful for type/interface/input. enum members
		// and union members are intentionally not emitted as fields here.
		if (subtype == "type" || subtype == "interface" || subtype == "input") &&
			bodyStart >= 0 && bodyEnd > bodyStart {
			body := src[bodyStart:bodyEnd]
			fields := collectFields(body)
			fieldSeen := make(map[string]bool)
			for _, f := range fields {
				if fieldSeen[f.name] {
					continue
				}
				fieldSeen[f.name] = true
				toID := extractor.BuildOperationStructuralRef("graphql", filePath, name+"."+f.name)
				ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
					FromID: extractor.BuildOperationStructuralRef("graphql", filePath, name),
					ToID:   toID,
					Kind:   "CONTAINS",
				})
				// Emit the field as its own SCOPE.Component entity so the
				// resolver can attach the structural-ref ToID.
				fieldStartLine := strings.Count(src[:bodyStart], "\n") + 1 + f.lineOffset
				entities = append(entities, types.EntityRecord{
					Name:       name + "." + f.name,
					Kind:       "SCOPE.Component",
					Subtype:    "field",
					SourceFile: filePath,
					Language:   "graphql",
					StartLine:  fieldStartLine,
					EndLine:    fieldStartLine,
					Signature:  name + "." + f.name,
				})
			}
		}

		entities = append(entities, ent)
		addFileContains(name)
	}

	// Operation definitions.
	for _, m := range operationRE.FindAllStringSubmatchIndex(src, -1) {
		opType := src[m[2]:m[3]]
		name := src[m[4]:m[5]]
		key := opType + ":" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine, bodyStart, bodyEnd := findBlockBounds(src, m[0])

		ent := types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Schema",
			Subtype:            opType,
			SourceFile:         filePath,
			Language:           "graphql",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          opType + " " + name,
			EnrichmentRequired: false,
		}

		// Fragment spreads inside operation body → IMPORTS.
		if bodyStart >= 0 && bodyEnd > bodyStart {
			body := src[bodyStart:bodyEnd]
			ent.Relationships = append(ent.Relationships, fragmentSpreadImports(body, filePath)...)
		}

		entities = append(entities, ent)
		addFileContains(name)
	}

	// Fragment definitions.
	for _, m := range fragmentRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		key := "fragment:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine, bodyStart, bodyEnd := findBlockBounds(src, m[0])

		ent := types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Schema",
			Subtype:            "fragment",
			SourceFile:         filePath,
			Language:           "graphql",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          "fragment " + name,
			EnrichmentRequired: false,
		}

		// Fragment-spread imports inside the fragment body too.
		if bodyStart >= 0 && bodyEnd > bodyStart {
			body := src[bodyStart:bodyEnd]
			ent.Relationships = append(ent.Relationships, fragmentSpreadImports(body, filePath)...)
		}

		entities = append(entities, ent)
		addFileContains(name)
	}

	// Federation `extend type` directives → SCOPE.Component import stubs.
	seenExtend := make(map[string]bool)
	for _, m := range extendTypeRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if seenExtend[name] {
			continue
		}
		seenExtend[name] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "import",
			SourceFile: filePath,
			Language:   "graphql",
			StartLine:  startLine,
			EndLine:    startLine,
			Relationships: []types.RelationshipRecord{
				{
					FromID: filePath,
					ToID:   name,
					Kind:   "IMPORTS",
					Properties: map[string]string{
						"source_module": name,
						"imported_name": name,
						"import_kind":   "extend",
					},
				},
			},
		})
	}

	return entities
}

// fieldHit is one captured field declaration inside a type/interface/input body.
type fieldHit struct {
	name       string
	lineOffset int // 0-based line offset within the body
}

// collectFields scans a type/interface/input body for field declarations.
// Field names are returned in declaration order; deduping is the caller's job.
func collectFields(body string) []fieldHit {
	var out []fieldHit
	for _, m := range fieldRE.FindAllStringSubmatchIndex(body, -1) {
		name := body[m[2]:m[3]]
		// Filter out reserved field-shaped tokens that aren't fields.
		if name == "" {
			continue
		}
		offset := strings.Count(body[:m[0]], "\n")
		out = append(out, fieldHit{name: name, lineOffset: offset})
	}
	return out
}

// fragmentSpreadImports returns one IMPORTS relationship per unique
// `...FragmentName` spread in body. The FromID is set to the file path so the
// resolver can attach edges via its standard import path.
func fragmentSpreadImports(body, filePath string) []types.RelationshipRecord {
	if body == "" {
		return nil
	}
	seen := make(map[string]bool)
	var out []types.RelationshipRecord
	for _, m := range fragmentSpreadRE.FindAllStringSubmatch(body, -1) {
		if len(m) < 2 {
			continue
		}
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, types.RelationshipRecord{
			FromID: filePath,
			ToID:   name,
			Kind:   "IMPORTS",
			Properties: map[string]string{
				"source_module": name,
				"imported_name": name,
				"import_kind":   "fragment_spread",
			},
		})
	}
	return out
}

// findBlockBounds returns the line where the { ... } block starting after pos
// closes, plus the byte offsets [bodyStart, bodyEnd) of the block body
// (everything strictly between the opening `{` and the matching `}`). For
// definitions without braces (scalars, unions on a single line) bodyStart
// returns -1.
func findBlockBounds(src string, startPos int) (endLine, bodyStart, bodyEnd int) {
	bracePos := strings.Index(src[startPos:], "{")
	if bracePos < 0 {
		return strings.Count(src[:startPos], "\n") + 1, -1, -1
	}
	abs := startPos + bracePos
	bodyStart = abs + 1
	depth := 0
	for i, ch := range src[abs:] {
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				closing := abs + i
				return strings.Count(src[:closing], "\n") + 1, bodyStart, closing
			}
		}
	}
	return strings.Count(src, "\n") + 1, bodyStart, len(src)
}

// findBlockEnd is retained for callers that only need the closing line.
func findBlockEnd(src string, startPos int) int {
	endLine, _, _ := findBlockBounds(src, startPos)
	return endLine
}
