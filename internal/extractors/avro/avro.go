// Package avro implements a content-based extractor for Apache Avro schema
// files (.avsc) and Avro protocol files (.avpr). Avro schemas are JSON
// documents, so this extractor json-decodes the file body rather than using a
// tree-sitter grammar (the dispatcher passes a nil Tree for the "avro" language;
// classifier routes .avsc/.avpr here).
//
// Extracted entities (issue #3690):
//
//   - record  → Kind="SCOPE.Schema", Subtype="record" — a named Avro record
//     data-contract. Each declared field becomes a SCOPE.Schema/field child
//     entity named "<Record>.<field>" carrying Properties["type"] (the resolved
//     Avro type token, e.g. "long", "string", "Address", "array<Order>").
//   - enum    → Kind="SCOPE.Schema", Subtype="enum" — a named Avro enum; its
//     symbols become field children.
//   - fixed   → Kind="SCOPE.Schema", Subtype="fixed" — a named fixed-size type.
//
// Relationships:
//
//   - CONTAINS  record → each field (Format-B member ref so the resolver binds
//     it via byMember[file][Record][field]); enum → each symbol.
//   - REFERENCES record → each named (non-primitive) field type, so a field
//     `{"name":"orders","type":{"type":"array","items":"Order"}}` yields an
//     edge Record → Order. The edge ToID is the Format-A operation ref for the
//     same file so the resolver binds it to the sibling record entity.
//
// Nested inline record definitions inside a field's "type" are recursively
// extracted as their own SCOPE.Schema/record entities. Avro namespaces are
// stripped to the trailing type name so references bind to the local record.
package avro

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("avro", &Extractor{})
}

// Extractor implements extractor.Extractor for Avro schemas.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "avro" }

// avroPrimitives is the set of Avro primitive type names. A field whose
// resolved type is one of these carries no REFERENCES edge.
var avroPrimitives = map[string]bool{
	"null": true, "boolean": true, "int": true, "long": true,
	"float": true, "double": true, "bytes": true, "string": true,
}

// Extract json-decodes the Avro schema and walks named types.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	var root interface{}
	if err := json.Unmarshal(file.Content, &root); err != nil {
		// Not valid JSON — a harmless no-op (an .avsc that doesn't parse).
		return nil, nil
	}

	var entities []types.EntityRecord
	// .avpr protocol files wrap schemas in a top-level "types" array; .avsc
	// files are a single schema (object) or a union (array). walkSchema handles
	// both, plus the protocol "types"/"messages" envelope.
	if obj, ok := root.(map[string]interface{}); ok {
		if rawTypes, ok := obj["types"].([]interface{}); ok {
			// Avro protocol envelope.
			for _, t := range rawTypes {
				walkSchema(t, file, &entities)
			}
			return entities, nil
		}
	}
	walkSchema(root, file, &entities)
	return entities, nil
}

// walkSchema dispatches on the shape of an Avro schema node, emitting entities
// for named record/enum/fixed types and recursing into nested definitions.
func walkSchema(node interface{}, file extractor.FileInput, out *[]types.EntityRecord) {
	switch v := node.(type) {
	case []interface{}:
		// A union — walk each branch.
		for _, b := range v {
			walkSchema(b, file, out)
		}
	case map[string]interface{}:
		typeName, _ := v["type"].(string)
		switch typeName {
		case "record", "error":
			buildRecord(v, file, out)
		case "enum":
			buildEnum(v, file, out)
		case "fixed":
			buildFixed(v, file, out)
		case "array":
			walkSchema(v["items"], file, out)
		case "map":
			walkSchema(v["values"], file, out)
		}
	}
}

// buildRecord emits a SCOPE.Schema/record entity plus a field child per
// declared field, with CONTAINS + REFERENCES edges.
func buildRecord(v map[string]interface{}, file extractor.FileInput, out *[]types.EntityRecord) {
	name, _ := v["name"].(string)
	name = localName(name)
	if name == "" {
		return
	}

	var rels []types.RelationshipRecord
	var fieldEnts []types.EntityRecord
	refSeen := map[string]bool{}

	rawFields, _ := v["fields"].([]interface{})
	for _, rf := range rawFields {
		fm, ok := rf.(map[string]interface{})
		if !ok {
			continue
		}
		fname, _ := fm["name"].(string)
		if fname == "" {
			continue
		}
		ftypeNode := fm["type"]
		typeStr := renderType(ftypeNode)

		// CONTAINS record → field.
		rels = append(rels, types.RelationshipRecord{
			ToID: fieldMemberRef(file.Path, name, fname),
			Kind: "CONTAINS",
		})
		fieldEnts = append(fieldEnts, buildField(file, name, fname, typeStr))

		// REFERENCES to each named (non-primitive) type the field points at.
		for _, ref := range namedTypeRefs(ftypeNode) {
			if refSeen[ref] {
				continue
			}
			refSeen[ref] = true
			rels = append(rels, types.RelationshipRecord{
				ToID:       extractor.BuildOperationStructuralRef("avro", file.Path, ref),
				Kind:       "REFERENCES",
				Properties: types.Props{{K: "type", V: ref}, {K: "via_field", V: fname}},
			})
		}

		// Recurse into any inline nested record/enum/fixed definition so it is
		// emitted as its own named entity.
		walkSchema(ftypeNode, file, &fieldEnts)
	}

	rec := types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Schema",
		Subtype:            "record",
		SourceFile:         file.Path,
		Language:           "avro",
		Signature:          "record " + name,
		EnrichmentRequired: false,
		Relationships:      rels,
	}
	*out = append(*out, rec)
	*out = append(*out, fieldEnts...)
}

// buildEnum emits a SCOPE.Schema/enum entity with a field child per symbol.
func buildEnum(v map[string]interface{}, file extractor.FileInput, out *[]types.EntityRecord) {
	name := localName(asString(v["name"]))
	if name == "" {
		return
	}
	var rels []types.RelationshipRecord
	var symEnts []types.EntityRecord
	symbols, _ := v["symbols"].([]interface{})
	for _, s := range symbols {
		sym, _ := s.(string)
		if sym == "" {
			continue
		}
		rels = append(rels, types.RelationshipRecord{
			ToID: fieldMemberRef(file.Path, name, sym),
			Kind: "CONTAINS",
		})
		symEnts = append(symEnts, buildField(file, name, sym, "enum_symbol"))
	}
	*out = append(*out, types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Schema",
		Subtype:            "enum",
		SourceFile:         file.Path,
		Language:           "avro",
		Signature:          "enum " + name,
		EnrichmentRequired: false,
		Relationships:      rels,
	})
	*out = append(*out, symEnts...)
}

// buildFixed emits a SCOPE.Schema/fixed entity.
func buildFixed(v map[string]interface{}, file extractor.FileInput, out *[]types.EntityRecord) {
	name := localName(asString(v["name"]))
	if name == "" {
		return
	}
	props := map[string]string{}
	if size, ok := v["size"].(float64); ok {
		props["size"] = fmt.Sprintf("%d", int(size))
	}
	*out = append(*out, types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Schema",
		Subtype:            "fixed",
		SourceFile:         file.Path,
		Language:           "avro",
		Signature:          "fixed " + name,
		Properties:         props,
		EnrichmentRequired: false,
	})
}

// buildField emits a SCOPE.Schema/field child named "<parent>.<field>" so the
// Format-B member resolver binds the CONTAINS edge.
func buildField(file extractor.FileInput, parent, fname, ftype string) types.EntityRecord {
	return types.EntityRecord{
		Name:               parent + "." + fname,
		Kind:               "SCOPE.Schema",
		Subtype:            "field",
		SourceFile:         file.Path,
		Language:           "avro",
		Signature:          ftype + " " + fname,
		QualifiedName:      parent + "." + fname,
		Properties:         map[string]string{"type": ftype},
		EnrichmentRequired: false,
	}
}

// fieldMemberRef returns the Format-B member structural-ref for a parent#member
// edge inside an Avro file.
func fieldMemberRef(filePath, parent, member string) string {
	return "scope:schema:column:avro:" + filePath + ":" + parent + "#" + member
}

// renderType produces a stable human-readable token for an Avro field type
// node: a primitive/named string, a union "a|b", an "array<T>", a "map<T>", or
// the name of an inline record/enum/fixed definition.
func renderType(node interface{}) string {
	switch v := node.(type) {
	case string:
		return localName(v)
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, b := range v {
			parts = append(parts, renderType(b))
		}
		return strings.Join(parts, "|")
	case map[string]interface{}:
		t, _ := v["type"].(string)
		switch t {
		case "array":
			return "array<" + renderType(v["items"]) + ">"
		case "map":
			return "map<" + renderType(v["values"]) + ">"
		case "record", "error", "enum", "fixed":
			return localName(asString(v["name"]))
		default:
			// Logical type or primitive wrapped in an object.
			return localName(t)
		}
	}
	return ""
}

// namedTypeRefs returns the named (non-primitive) type names referenced by a
// field-type node, deduped and sorted for deterministic output.
func namedTypeRefs(node interface{}) []string {
	set := map[string]bool{}
	collectRefs(node, set)
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func collectRefs(node interface{}, set map[string]bool) {
	switch v := node.(type) {
	case string:
		n := localName(v)
		if n != "" && !avroPrimitives[n] {
			set[n] = true
		}
	case []interface{}:
		for _, b := range v {
			collectRefs(b, set)
		}
	case map[string]interface{}:
		t, _ := v["type"].(string)
		switch t {
		case "array":
			collectRefs(v["items"], set)
		case "map":
			collectRefs(v["values"], set)
		case "record", "error", "enum", "fixed":
			if n := localName(asString(v["name"])); n != "" {
				set[n] = true
			}
		default:
			n := localName(t)
			if n != "" && !avroPrimitives[n] {
				set[n] = true
			}
		}
	}
}

// localName strips an Avro namespace, returning the trailing type name so a
// fully-qualified reference ("com.example.Order") binds to the local record
// ("Order").
func localName(name string) string {
	name = strings.TrimSpace(name)
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		name = name[idx+1:]
	}
	return name
}

func asString(v interface{}) string {
	s, _ := v.(string)
	return s
}
