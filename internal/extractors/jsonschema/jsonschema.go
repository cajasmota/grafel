// Package jsonschema implements a content-based extractor for JSON Schema
// data-contract documents (files following the *.schema.json convention, routed
// here by the classifier). JSON Schema documents are JSON, so this extractor
// json-decodes the file body rather than using a tree-sitter grammar (the
// dispatcher passes a nil Tree for the "jsonschema" language).
//
// Extracted entities (issue #3690):
//
//   - The root object schema → Kind="SCOPE.Schema", Subtype="object". Named from
//     "title" when present, else the file basename (sans .schema.json). Each
//     declared property becomes a SCOPE.Schema/field child entity named
//     "<Schema>.<prop>" carrying Properties["type"] (the JSON Schema type token,
//     e.g. "integer", "string", "array<#/$defs/Order>" reduced to "array<Order>").
//   - Each named subschema under "$defs" / "definitions" → its own
//     SCOPE.Schema/object entity with its own property field children.
//
// Relationships:
//
//   - CONTAINS    schema → each property (Format-B member ref so the resolver
//     binds it via byMember[file][Schema][prop]).
//   - REFERENCES  schema → each referenced schema named by a "$ref"
//     ("#/$defs/Address" → Address). The edge ToID is the Format-A operation ref
//     for the same file so the resolver binds it to the sibling $defs entity.
//
// content-sniff: a document is only treated as a JSON Schema when it carries a
// "$schema", "properties", "$defs", "definitions", or "$ref" key, so a
// misrouted JSON file is a harmless no-op.
package jsonschema

import (
	"context"
	"encoding/json"
	"path"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("jsonschema", &Extractor{})
}

// Extractor implements extractor.Extractor for JSON Schema.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "jsonschema" }

// Extract json-decodes the schema document and emits schema + field entities.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	var root map[string]interface{}
	if err := json.Unmarshal(file.Content, &root); err != nil {
		return nil, nil
	}
	if !looksLikeSchema(root) {
		return nil, nil
	}

	var entities []types.EntityRecord
	rootName := schemaName(root, file.Path)
	buildObjectSchema(root, rootName, file, &entities)

	// Named subschemas under $defs / definitions become their own entities.
	for _, key := range []string{"$defs", "definitions"} {
		defs, ok := root[key].(map[string]interface{})
		if !ok {
			continue
		}
		names := make([]string, 0, len(defs))
		for n := range defs {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			sub, ok := defs[n].(map[string]interface{})
			if !ok {
				continue
			}
			buildObjectSchema(sub, n, file, &entities)
		}
	}
	return entities, nil
}

// looksLikeSchema content-sniffs a decoded JSON document for the JSON Schema
// shape so misrouted JSON files emit nothing.
func looksLikeSchema(root map[string]interface{}) bool {
	for _, k := range []string{"$schema", "properties", "$defs", "definitions", "$ref"} {
		if _, ok := root[k]; ok {
			return true
		}
	}
	return false
}

// buildObjectSchema emits a SCOPE.Schema/object entity for a (sub)schema plus a
// field child per declared property, with CONTAINS + REFERENCES edges.
func buildObjectSchema(schema map[string]interface{}, name string, file extractor.FileInput, out *[]types.EntityRecord) {
	if name == "" {
		return
	}
	var rels []types.RelationshipRecord
	var fieldEnts []types.EntityRecord
	refSeen := map[string]bool{}

	// A schema may $ref another schema directly (composition).
	for _, ref := range refsOf(schema) {
		if refSeen[ref] {
			continue
		}
		refSeen[ref] = true
		rels = append(rels, types.RelationshipRecord{
			ToID:       extractor.BuildOperationStructuralRef("jsonschema", file.Path, ref),
			Kind:       "REFERENCES",
			Properties: types.Props{{K: "type", V: ref}},
		})
	}

	props, _ := schema["properties"].(map[string]interface{})
	pnames := make([]string, 0, len(props))
	for p := range props {
		pnames = append(pnames, p)
	}
	sort.Strings(pnames)
	for _, pname := range pnames {
		pschema, _ := props[pname].(map[string]interface{})
		typeStr := renderType(pschema)
		rels = append(rels, types.RelationshipRecord{
			ToID: fieldMemberRef(file.Path, name, pname),
			Kind: "CONTAINS",
		})
		fieldEnts = append(fieldEnts, buildField(file, name, pname, typeStr))

		for _, ref := range refsOf(pschema) {
			if refSeen[ref] {
				continue
			}
			refSeen[ref] = true
			rels = append(rels, types.RelationshipRecord{
				ToID:       extractor.BuildOperationStructuralRef("jsonschema", file.Path, ref),
				Kind:       "REFERENCES",
				Properties: types.Props{{K: "type", V: ref}, {K: "via_field", V: pname}},
			})
		}
	}

	*out = append(*out, types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Schema",
		Subtype:            "object",
		SourceFile:         file.Path,
		Language:           "jsonschema",
		Signature:          "schema " + name,
		EnrichmentRequired: false,
		Relationships:      rels,
	})
	*out = append(*out, fieldEnts...)
}

// buildField emits a SCOPE.Schema/field child named "<parent>.<prop>".
func buildField(file extractor.FileInput, parent, fname, ftype string) types.EntityRecord {
	return types.EntityRecord{
		Name:               parent + "." + fname,
		Kind:               "SCOPE.Schema",
		Subtype:            "field",
		SourceFile:         file.Path,
		Language:           "jsonschema",
		Signature:          ftype + " " + fname,
		QualifiedName:      parent + "." + fname,
		Properties:         map[string]string{"type": ftype},
		EnrichmentRequired: false,
	}
}

func fieldMemberRef(filePath, parent, member string) string {
	return "scope:schema:column:jsonschema:" + filePath + ":" + parent + "#" + member
}

// renderType produces a stable token for a property schema: its "type" keyword,
// an "array<items>" wrapper, or the trailing name of a "$ref" target.
func renderType(prop map[string]interface{}) string {
	if prop == nil {
		return ""
	}
	if ref, ok := prop["$ref"].(string); ok {
		return refLocalName(ref)
	}
	switch t := prop["type"].(type) {
	case string:
		if t == "array" {
			if items, ok := prop["items"].(map[string]interface{}); ok {
				return "array<" + renderType(items) + ">"
			}
			return "array"
		}
		return t
	case []interface{}:
		parts := make([]string, 0, len(t))
		for _, x := range t {
			if s, ok := x.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "|")
	}
	// composition keywords
	for _, k := range []string{"allOf", "anyOf", "oneOf"} {
		if _, ok := prop[k]; ok {
			return k
		}
	}
	return ""
}

// refsOf returns every $ref target name reachable one level inside a schema
// node: a direct "$ref", an array "items.$ref", and composition
// allOf/anyOf/oneOf branch "$ref"s.
func refsOf(schema map[string]interface{}) []string {
	if schema == nil {
		return nil
	}
	set := map[string]bool{}
	add := func(s map[string]interface{}) {
		if ref, ok := s["$ref"].(string); ok {
			if n := refLocalName(ref); n != "" {
				set[n] = true
			}
		}
	}
	add(schema)
	if items, ok := schema["items"].(map[string]interface{}); ok {
		add(items)
	}
	for _, k := range []string{"allOf", "anyOf", "oneOf"} {
		branches, ok := schema[k].([]interface{})
		if !ok {
			continue
		}
		for _, b := range branches {
			if bm, ok := b.(map[string]interface{}); ok {
				add(bm)
			}
		}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// refLocalName reduces a JSON-Pointer $ref ("#/$defs/Address",
// "#/definitions/Order", "other.schema.json#/$defs/Foo") to its trailing
// schema name ("Address", "Order", "Foo").
func refLocalName(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		ref = ref[i+1:]
	}
	return ref
}

// schemaName derives the root schema's name from its "title", falling back to
// the file basename with the .schema.json / .json suffix stripped.
func schemaName(root map[string]interface{}, filePath string) string {
	if title, ok := root["title"].(string); ok && strings.TrimSpace(title) != "" {
		return strings.TrimSpace(title)
	}
	base := path.Base(filePath)
	base = strings.TrimSuffix(base, ".json")
	base = strings.TrimSuffix(base, ".schema")
	return base
}
