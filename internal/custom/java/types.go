// Package java provides custom extractors for Java frameworks.
//
// Each extractor matches against language+framework gates and emits
// SecondaryEntity and Relationship records for patterns that YAML rules
// cannot capture (DI graphs, request mappings, ORM associations, etc.).
//
// The extractors are regex-based and operate on raw source text.
// They do NOT use tree-sitter -- that is handled by the primary extractor
// in internal/extractors/java.
package java

import "regexp"

// SecondaryEntity represents a framework-inferred entity discovered by
// a custom extractor. These are emitted alongside the primary tree-sitter
// entities and carry framework-specific provenance.
type SecondaryEntity struct {
	Name       string         `json:"name"`
	Kind       string         `json:"kind"`
	Subtype    string         `json:"subtype,omitempty"`
	SourceFile string         `json:"source_file"`
	LineStart  int            `json:"line_start"`
	LineEnd    int            `json:"line_end"`
	Provenance string         `json:"provenance"`
	Ref        string         `json:"ref"`
	Properties map[string]any `json:"properties,omitempty"`
}

// Relationship represents a directed edge between two entities identified
// by their ref strings.
//
// SourceRef names the CARRIER entity the edge is hung off (the entity whose
// Ref == SourceRef, or a synthesised carrier). By default the edge's FromID is
// left implicit (the carrier's own ID). When FromName is set, it overrides the
// edge's FromID with an explicit resolvable stub (e.g. `Class:<Owner>`) so the
// edge's source binds to a DIFFERENT entity than its carrier — used for #4367
// field-membership CONTAINS edges, where the field entity carries the edge but
// the source must resolve to its (separately-extracted) owning class by name.
type Relationship struct {
	SourceRef        string            `json:"source_ref"`
	TargetRef        string            `json:"target_ref"`
	RelationshipType string            `json:"relationship_type"`
	FromName         string            `json:"from_name,omitempty"`
	Properties       map[string]string `json:"properties,omitempty"`
}

// PatternContext is the input contract for all custom extractors.
type PatternContext struct {
	Source    string
	Language  string
	Framework string
	FilePath  string
}

// PatternResult holds the output of a custom extractor.
type PatternResult struct {
	Entities      []SecondaryEntity
	Relationships []Relationship
}

// lineOf returns the 1-indexed line number for offset in source.
func lineOf(source string, offset int) int {
	n := 1
	for i := 0; i < offset && i < len(source); i++ {
		if source[i] == '\n' {
			n++
		}
	}
	return n
}

// primitiveTypes is the set of Java types that should not be treated as
// injectable beans or DTO types.
var primitiveTypes = map[string]bool{
	"int": true, "long": true, "double": true, "float": true,
	"boolean": true, "char": true, "byte": true, "short": true,
	"void": true, "String": true, "Integer": true, "Long": true,
	"Double": true, "Float": true, "Boolean": true, "Object": true,
	"List": true, "Map": true, "Set": true, "Collection": true,
	"Optional": true,
}

// classDeclRE matches a class declaration to find the enclosing class name.
var classDeclRE = regexp.MustCompile(
	`(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`,
)

// findEnclosingClass returns the name of the class whose declaration
// appears before offset in source. Returns empty string if none found.
func findEnclosingClass(source string, offset int) string {
	var best string
	for _, m := range classDeclRE.FindAllStringSubmatchIndex(source, -1) {
		if m[0] <= offset {
			best = source[m[2]:m[3]]
		} else {
			break
		}
	}
	return best
}

// findRefForType looks up an existing entity ref by class name from emitted
// entities, falling back to a synthetic dependency ref.
func findRefForType(typeName, filePath, prefix string, result *PatternResult) string {
	for _, e := range result.Entities {
		var entityClass string
		for i := len(e.Name) - 1; i >= 0; i-- {
			if e.Name[i] == '.' {
				entityClass = e.Name[i+1:]
				break
			}
		}
		if entityClass == "" {
			entityClass = e.Name
		}
		if entityClass == typeName {
			return e.Ref
		}
	}
	return "scope:dependency:" + prefix + ":" + filePath + ":" + typeName
}

// addEntity is a dedup helper that appends e to result only if its ref
// has not been seen before.
func addEntity(result *PatternResult, seen map[string]bool, e SecondaryEntity) bool {
	if seen[e.Ref] {
		return false
	}
	seen[e.Ref] = true
	result.Entities = append(result.Entities, e)
	return true
}

// relKey is a dedup key for relationships.
type relKey struct {
	src, tgt, kind string
}

// addRel is a dedup helper that appends r to result only if the
// (source, target, type) triple has not been seen.
func addRel(result *PatternResult, seen map[relKey]bool, r Relationship) bool {
	k := relKey{r.SourceRef, r.TargetRef, r.RelationshipType}
	if seen[k] {
		return false
	}
	seen[k] = true
	result.Relationships = append(result.Relationships, r)
	return true
}

// constructorParamRE extracts individual typed parameters from a
// constructor or method parameter list.
var constructorParamRE = regexp.MustCompile(
	`(?:@\w+(?:\([^)]*\))?\s+)*(\w+)(?:\s*<[^>]*>)?\s+\w+`,
)

// pathVarTokenRE matches a path-variable expression like {id}, {userId},
// {orderId:[0-9]+}, or {name:.+} inside a URL template.
var pathVarTokenRE = regexp.MustCompile(`\{(\w+)(?::[^}]*)?\}`)

// extractPathParams returns the list of path-variable names from a URL
// template string, e.g. "/api/users/{id}/orders/{orderId}" → ["id","orderId"].
func extractPathParams(path string) []string {
	var params []string
	for _, m := range pathVarTokenRE.FindAllStringSubmatch(path, -1) {
		params = append(params, m[1])
	}
	return params
}
