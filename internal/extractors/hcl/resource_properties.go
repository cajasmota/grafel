package hcl

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// resource_properties.go — epic #4194 (iac_resource_property_extraction).
//
// Stamp a CURATED, bounded allow-list of high-signal SCALAR configuration
// attributes from a Terraform/OpenTofu `resource` block body onto the resource
// entity's Properties map, as plain key→value strings. This ADDS scalar config
// alongside the existing reference-edge mining (DEPENDS_ON / CALLS / iteration
// meta) — it does NOT replace it.
//
// Bounded scope (avoid graph bloat): we only stamp attributes whose key is in
// curatedResourceScalarKeys AND whose value is a *literal scalar*
// (string_lit / numeric_lit / bool_lit). We deliberately SKIP:
//   - nested blocks (already handled as dynamic child entities / meta-blocks),
//   - collection values (objects, tuples/lists),
//   - interpolations / traversal references (var.*, data.*, resource refs) —
//     those are reference edges, not scalar config, and are mined elsewhere.
//
// Typical stamped count is small: 2–6 props on a real resource (e.g. an
// aws_instance gets instance_type + count; a lambda gets runtime + memory_size
// + timeout). The allow-list is intentionally curated rather than "every
// attribute" to keep per-resource property fan-out bounded.

// curatedResourceScalarKeys is the allow-list of high-signal scalar resource
// attribute keys we stamp. Chosen to be cross-provider useful (compute sizing,
// runtime, scaling, networking, engine/version) while staying bounded. Keys
// not in this set are left to the reference-edge miner and are not stamped as
// scalar config.
var curatedResourceScalarKeys = map[string]struct{}{
	// compute sizing / SKU
	"instance_type":  {},
	"machine_type":   {},
	"size":           {},
	"sku":            {},
	"tier":           {},
	"instance_class": {},
	"node_type":      {},
	"vm_size":        {},
	// memory / timeout (serverless + containers)
	"memory_size": {},
	"memory":      {},
	"timeout":     {},
	// runtime / engine / version
	"runtime":        {},
	"engine":         {},
	"engine_version": {},
	"version":        {},
	// scaling / count / replicas
	"count":            {},
	"desired_capacity": {},
	"min_size":         {},
	"max_size":         {},
	"replicas":         {},
	// networking
	"port":     {},
	"protocol": {},
	// storage
	"allocated_storage": {},
	"storage_type":      {},
}

// stampResourceScalarProperties scans a resource block body for the curated
// scalar attributes and stamps them onto rec.Properties. No-op when the body
// has no matching scalar attributes (Properties stays nil to avoid empty maps).
func stampResourceScalarProperties(rec *EntityProps, body *sitter.Node, src []byte) {
	if body == nil || rec == nil {
		return
	}
	count := int(body.ChildCount())
	for i := 0; i < count; i++ {
		attr := body.Child(i)
		if attr == nil || attr.Type() != "attribute" {
			continue
		}
		keyNode := firstChildByType(attr, "identifier")
		if keyNode == nil {
			continue
		}
		key := nodeText(keyNode, src)
		if _, ok := curatedResourceScalarKeys[key]; !ok {
			continue
		}
		val, ok := scalarLiteralValue(attr, src)
		if !ok {
			// Value is a reference / collection / interpolation — skip it.
			// Reference values remain edges (mined by extractCalls), they are
			// intentionally NOT stamped as scalar config.
			continue
		}
		if rec.Properties == nil {
			rec.Properties = map[string]string{}
		}
		rec.Properties[key] = val
	}
}

// EntityProps is the minimal interface into the property map we need. It mirrors
// the relevant fields of types.EntityRecord so this helper can be unit-tested
// without constructing a full record, and so the call site stays a one-liner.
type EntityProps struct {
	Properties map[string]string
}

// scalarLiteralValue returns the literal scalar value of an attribute's
// expression and true, IFF the attribute value is exactly a single literal
// (string_lit / numeric_lit / bool_lit). For string values the surrounding
// quotes are stripped (the template_literal text is returned). For any
// non-literal value (references, interpolated templates, collections, function
// calls) it returns ("", false).
func scalarLiteralValue(attr *sitter.Node, src []byte) (string, bool) {
	expr := firstChildByType(attr, "expression")
	if expr == nil {
		return "", false
	}
	// A pure scalar is: expression → literal_value → {string_lit|numeric_lit|bool_lit}.
	// References produce variable_expr/get_attr children directly under
	// expression (no literal_value); collections produce collection_value;
	// interpolated strings produce a template with interpolation children.
	lit := firstChildByType(expr, "literal_value")
	if lit == nil {
		return "", false
	}
	// literal_value must have exactly one typed scalar child.
	for i := 0; i < int(lit.ChildCount()); i++ {
		c := lit.Child(i)
		if c == nil {
			continue
		}
		switch c.Type() {
		case "string_lit":
			return scalarStringLit(c, src)
		case "numeric_lit":
			return strings.TrimSpace(nodeText(c, src)), true
		case "bool_lit":
			return strings.TrimSpace(nodeText(c, src)), true
		}
	}
	return "", false
}

// scalarStringLit returns the inner text of a string_lit, but ONLY when it is a
// plain literal (a single template_literal with no interpolation/template
// expressions). A string containing ${...} interpolation is a reference-bearing
// template and is NOT a scalar — those are mined as CALLS edges, so we return
// ("", false) for them.
func scalarStringLit(n *sitter.Node, src []byte) (string, bool) {
	if n == nil {
		return "", false
	}
	var literal string
	sawLiteral := false
	for i := 0; i < int(n.ChildCount()); i++ {
		c := n.Child(i)
		if c == nil {
			continue
		}
		switch c.Type() {
		case "quoted_template_start", "quoted_template_end":
			// quote delimiters — ignore
		case "template_literal":
			if sawLiteral {
				// more than one literal segment → interpolated; not scalar
				return "", false
			}
			literal = nodeText(c, src)
			sawLiteral = true
		default:
			// template_interpolation, template_expr, etc. → not a plain scalar
			return "", false
		}
	}
	// Empty string literal ("") is a valid scalar value.
	if !sawLiteral {
		// could be the empty string "" (no template_literal child)
		// confirm it really is just the two quote delimiters
		return "", true
	}
	return literal, true
}
