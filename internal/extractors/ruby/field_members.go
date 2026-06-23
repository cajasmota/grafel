package ruby

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitRubyAttrFields returns one SCOPE.Schema/field EntityRecord per symbol
// declared by an attr_accessor / attr_reader / attr_writer call directly inside
// the body of the class named ownerName, plus a class→field CONTAINS edge
// attached to the owner at ownerIdx.
//
// Issue #4854 — Ruby has no static field declarations; the declaratively
// present members of a plain data class are its attr_* accessors (and
// Struct.new / Data.define members, handled separately). Before this pass those
// were only surfaced by the framework-bound custom validation emitter
// (internal/custom/ruby/validation.go) as orphan SCOPE.Schema/dto_field nodes
// with no CONTAINS edge, so a plain Ruby model resolved to a SCOPE.Component
// with ZERO field children and the dashboard shape endpoint returned rows:[].
//
//	Kind      = "SCOPE.Schema"
//	Subtype   = "field"
//	Name      = "<Owner>.<attr>"
//	Signature = "attr <attr>"
func emitRubyAttrFields(
	records *[]types.EntityRecord,
	ownerIdx int,
	body ts.Node,
	src []byte,
	ownerName, filePath string,
) {
	if body == nil || ownerName == "" {
		return
	}
	seen := make(map[string]bool)
	for i := 0; i < int(body.ChildCount()); i++ {
		call := body.Child(i)
		if call == nil || call.Type() != "call" {
			continue
		}
		mname := rubyCallIdentifier(call, src)
		if mname != "attr_accessor" && mname != "attr_reader" && mname != "attr_writer" {
			continue
		}
		for _, sym := range rubyCallSymbolArgs(call, src) {
			if sym == "" || seen[sym] {
				continue
			}
			seen[sym] = true
			fe := rubyFieldEntity(ownerName, sym, "attr", filePath, call)
			toID := extractor.BuildSchemaFieldStructuralRef("ruby", filePath, fe.Name)
			(*records)[ownerIdx].Relationships = append((*records)[ownerIdx].Relationships,
				types.RelationshipRecord{ToID: toID, Kind: "CONTAINS"})
			*records = append(*records, fe)
		}
	}
}

// emitRubyStructDefine handles a top-level / nested `Const = Struct.new(:a, :b)`
// or `Const = Data.define(:a, :b)` assignment: it synthesises a SCOPE.Component
// (subtype "struct") named after the constant and one SCOPE.Schema/field per
// member, with class→field CONTAINS edges, mirroring the data-class shape the
// shape walker expects. Returns nil when the assignment is not a Struct.new /
// Data.define form.
func emitRubyStructDefine(node ts.Node, file extractor.FileInput) []types.EntityRecord {
	if node == nil || node.Type() != "assignment" {
		return nil
	}
	// LHS constant name.
	lhs := node.ChildByFieldName("left")
	if lhs == nil || lhs.Type() != "constant" {
		return nil
	}
	name := nodeText(lhs, file.Content)
	// RHS call: Struct.new(...) / Data.define(...).
	rhs := node.ChildByFieldName("right")
	if rhs == nil || rhs.Type() != "call" {
		return nil
	}
	recv := rhs.ChildByFieldName("receiver")
	meth := rubyCallIdentifier(rhs, file.Content)
	if recv == nil {
		return nil
	}
	recvName := nodeText(recv, file.Content)
	if !((recvName == "Struct" && meth == "new") || (recvName == "Data" && meth == "define")) {
		return nil
	}
	syms := rubyCallSymbolArgs(rhs, file.Content)
	if name == "" || len(syms) == 0 {
		return nil
	}

	comp := types.EntityRecord{
		Name:               name,
		QualifiedName:      name,
		Kind:               "SCOPE.Component",
		Subtype:            "struct",
		SourceFile:         file.Path,
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Language:           "ruby",
		Signature:          name + " = " + recvName + "." + meth,
		EnrichmentRequired: false,
	}
	out := []types.EntityRecord{comp}
	seen := make(map[string]bool)
	for _, sym := range syms {
		if sym == "" || seen[sym] {
			continue
		}
		seen[sym] = true
		fe := rubyFieldEntity(name, sym, recvName, file.Path, node)
		toID := extractor.BuildSchemaFieldStructuralRef("ruby", file.Path, fe.Name)
		out[0].Relationships = append(out[0].Relationships,
			types.RelationshipRecord{ToID: toID, Kind: "CONTAINS"})
		out = append(out, fe)
	}
	return out
}

// rubyFieldEntity builds a SCOPE.Schema/field entity for member `field` of
// owner `owner`, positioned at `at`.
func rubyFieldEntity(owner, field, source, filePath string, at ts.Node) types.EntityRecord {
	dotted := owner + "." + field
	return types.EntityRecord{
		Name:          dotted,
		QualifiedName: dotted,
		Kind:          "SCOPE.Schema",
		Subtype:       "field",
		SourceFile:    filePath,
		StartLine:     int(at.StartPoint().Row) + 1,
		EndLine:       int(at.EndPoint().Row) + 1,
		Language:      "ruby",
		Signature:     source + " " + field,
		QualityScore:  1.0,
		Properties: map[string]string{
			"field_name":   field,
			"parent_class": owner,
			"source":       source,
		},
		Metadata:           map[string]interface{}{"subtype": "field", "owner": owner},
		EnrichmentRequired: false,
	}
}

// rubyCallIdentifier returns the bare method name of a `call` node, whether it
// carries an explicit "method" field (receiver.method form) or a leading
// identifier child (bare attr_accessor form).
func rubyCallIdentifier(call ts.Node, src []byte) string {
	if m := call.ChildByFieldName("method"); m != nil {
		return nodeText(m, src)
	}
	for i := 0; i < int(call.ChildCount()); i++ {
		if c := call.Child(i); c != nil && c.Type() == "identifier" {
			return nodeText(c, src)
		}
	}
	return ""
}

// rubyCallSymbolArgs returns the bare names of the simple_symbol arguments of a
// `call` node (`attr_accessor :a, :b` → ["a","b"]).
func rubyCallSymbolArgs(call ts.Node, src []byte) []string {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		for i := 0; i < int(call.ChildCount()); i++ {
			if c := call.Child(i); c != nil && c.Type() == "argument_list" {
				args = c
				break
			}
		}
	}
	if args == nil {
		return nil
	}
	var out []string
	for i := 0; i < int(args.ChildCount()); i++ {
		a := args.Child(i)
		if a == nil || a.Type() != "simple_symbol" {
			continue
		}
		out = append(out, strings.TrimPrefix(nodeText(a, src), ":"))
	}
	return out
}

// attachRubyExtends emits a class→superclass EXTENDS edge from each owner's
// stashed Metadata "base_candidate", restricted to superclasses declared in
// this same file so the dashboard shape walker can recurse into inherited
// attr fields.
func attachRubyExtends(records []types.EntityRecord) []types.EntityRecord {
	known := make(map[string]bool)
	for i := range records {
		if records[i].Kind == "SCOPE.Component" {
			known[records[i].Name] = true
		}
	}
	for i := range records {
		if records[i].Kind != "SCOPE.Component" || records[i].Metadata == nil {
			continue
		}
		base, _ := records[i].Metadata["base_candidate"].(string)
		owner := records[i].Name
		if base != "" && base != owner && known[base] {
			dup := false
			for _, ex := range records[i].Relationships {
				if ex.Kind == "EXTENDS" && ex.ToID == base {
					dup = true
					break
				}
			}
			if !dup {
				records[i].Relationships = append(records[i].Relationships,
					types.RelationshipRecord{FromID: owner, ToID: base, Kind: "EXTENDS"})
			}
		}
		delete(records[i].Metadata, "base_candidate")
	}
	return records
}
