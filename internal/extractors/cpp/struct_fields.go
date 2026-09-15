package cpp

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitClassFieldMembers returns one SCOPE.Schema/field EntityRecord per data
// member of the class/struct/union named ownerName, plus EXTENDS
// RelationshipRecords (second result, to be attached to the OWNER component)
// for each base class.
//
// Issue #4854 — before this pass C/C++ struct/class members were only stashed
// in the owner Component's Metadata["fields"]; they were never emitted as graph
// entities. A plain (non-ORM, non-endpoint-bound) C++ data class therefore
// resolved to a SCOPE.Component with ZERO field children, so the dashboard
// shape endpoint returned rows:[] and classHasFieldChildren was false — the
// same gap #4850/#4851/#4855 closed for Go/JS-TS DTOs.
//
// The field entity shape mirrors the Go (#4855) / Java (#690) emitters so the
// shared dashboard shape walker parses them uniformly:
//
//	Kind      = "SCOPE.Schema"
//	Subtype   = "field"
//	Name      = "<Owner>.<member>"   (dotted, file-scoped resolver key)
//	Signature = "<type> <member>"
//
// The class→field CONTAINS edge is attached by the caller via
// extractor.BuildSchemaFieldStructuralRef keyed on the same dotted Name + file.
//
// A data member whose declarator is wrapped in pointer/array/reference
// decoration (`int* p;`, `char buf[8];`, `T& ref;`) still resolves to its inner
// field_identifier.
//
// MEMBER FUNCTIONS ARE ONLY PARTLY EXCLUDED, AND THE LIMIT IS NOT WHERE THIS
// COMMENT USED TO CLAIM IT WAS. The guard below skips a field_declaration whose
// DIRECT child is a function_declarator, which covers `void greet();`. It does
// NOT cover a function declarator wrapped in pointer/reference decoration —
// `virtual const VideoInfo& GetVideoInfo() = 0;` is a reference_declarator
// around a function_declarator, so cppFieldNames descends straight through to
// the function's NAME and mints a SCOPE.Schema/field entity for a pure virtual
// member function whose `field_type` is its RETURN type. The same holds for a
// pointer-to-member-function data member (`AVSMap& (C::* p)();`), whose
// field_type is likewise the return type rather than the member's own type.
//
// Both shapes are live in real headers (avisynth.h in our corpus) and both are
// a CAPTURE defect predating this comment — filed as #7061, NOT fixed here,
// because correcting the capture changes entity counts, the #6118 digest and
// the #4854 contract. What IS done here is to mark them: every such record
// carries Metadata["function_shaped"], so a downstream pass can refuse to treat
// it as a plain data member. #6912's field→declared-type pass
// (field_type_refs.go) is the first consumer and refuses them as edge SOURCES.
func emitClassFieldMembers(
	body ts.Node,
	src []byte,
	ownerName, filePath, lang string,
) []types.EntityRecord {
	if body == nil || ownerName == "" {
		return nil
	}

	var fields []types.EntityRecord
	seen := make(map[string]bool)

	for i := 0; i < int(body.ChildCount()); i++ {
		ch := body.Child(i)
		if ch.Type() != "field_declaration" {
			continue
		}
		// A function_declarator child ⇒ inline method, not a data member.
		if cppFirstChildOfType(ch, "function_declarator") != nil {
			continue
		}
		typeNode := ch.ChildByFieldName("type")
		typeText := ""
		if typeNode != nil {
			typeText = strings.TrimSpace(nodeText(typeNode, src))
		}
		// A field_declaration may declare several members sharing one type
		// (`int a, b;`). Collect every field_identifier reachable through the
		// declarator decoration.
		fnShaped := cppFunctionShapedNames(ch, src)
		for _, fname := range cppFieldNames(ch, src) {
			fname = strings.TrimSpace(fname)
			if fname == "" || seen[fname] {
				continue
			}
			seen[fname] = true
			startLine := int(ch.StartPoint().Row) + 1
			endLine := int(ch.EndPoint().Row) + 1
			dotted := ownerName + "." + fname
			sig := fname
			if typeText != "" {
				sig = typeText + " " + fname
			}
			meta := map[string]interface{}{"subtype": "field", "owner": ownerName}
			if fnShaped[fname] {
				meta["function_shaped"] = true
			}
			fields = append(fields, types.EntityRecord{
				Name:          dotted,
				QualifiedName: dotted,
				Kind:          "SCOPE.Schema",
				Subtype:       "field",
				SourceFile:    filePath,
				StartLine:     startLine,
				EndLine:       endLine,
				Language:      lang,
				Signature:     sig,
				QualityScore:  1.0,
				Properties: map[string]string{
					"field_name":   fname,
					"field_type":   typeText,
					"parent_class": ownerName,
				},
				Metadata:           meta,
				EnrichmentRequired: false,
			})
		}
	}

	return fields
}

// cppFieldNames returns every member name declared by a field_declaration,
// descending through pointer/array/reference declarator decoration to the
// inner field_identifier(s). Returns nil for a member function declaration.
func cppFieldNames(fieldDecl ts.Node, src []byte) []string {
	var names []string
	for i := 0; i < int(fieldDecl.ChildCount()); i++ {
		ch := fieldDecl.Child(i)
		switch ch.Type() {
		case "field_identifier":
			names = append(names, nodeText(ch, src))
		case "pointer_declarator", "array_declarator", "reference_declarator",
			"init_declarator":
			if id := cppDescendantFieldIdentifier(ch); id != nil {
				names = append(names, nodeText(id, src))
			}
		}
	}
	return names
}

// cppFunctionShapedNames returns the subset of a field_declaration's member
// names whose declarator subtree contains a function_declarator — i.e. the names
// this package mints a SCOPE.Schema/field for even though the construct is not a
// plain data member.
//
// THREE distinct source shapes land here — the count was stated as two and the
// third is a different node kind, not a restatement — and all three matter,
// because in every one the `type` field holds the RETURN type rather than the
// member's own type, so a field→declared-type edge built from them asserts
// something false:
//
//	virtual const VideoInfo& GetVideoInfo() = 0;   MEMBER FUNCTION, reference_declarator
//	virtual AVSMap* GetMap() = 0;                  MEMBER FUNCTION, pointer_declarator
//	AVSMap& (VideoFrame::* getProperties)();       POINTER-TO-MEMBER-FUNCTION member
//
// The first two are not data members at all; the third is, but its declared type
// is a function type, not `AVSMap`. Callers that need "plain data member" must
// test this marker; callers that only need "a name the class declares" need not.
//
// THE SCAN IS RECURSIVE AND THE RECURSION IS LOAD-BEARING, not defensive. The
// decoration nests without limit and each extra layer is ordinary C++:
// `Order** getPP();` puts the function_declarator at depth 2, `Order*** getPPP()`
// at depth 3, and `Order*& getPR()` mixes the two. A scan of the node plus its
// direct children answers the depth-1 cases and lets every deeper one through to
// emit a wrong edge. Graded as its own mutant rather than inside a compound —
// see TestCppFieldTypeRefs_FunctionShapedMemberIsNeverASource.
//
// A bare `void greet();` never reaches here — emitClassFieldMembers' direct-child
// guard already skips it, and this function only inspects the DECORATED
// declarator children, which is exactly the set that guard misses.
func cppFunctionShapedNames(fieldDecl ts.Node, src []byte) map[string]bool {
	var out map[string]bool
	for i := 0; i < int(fieldDecl.ChildCount()); i++ {
		ch := fieldDecl.Child(i)
		switch ch.Type() {
		case "pointer_declarator", "array_declarator", "reference_declarator",
			"init_declarator":
			if !cppHasDescendantOfType(ch, "function_declarator") {
				continue
			}
			id := cppDescendantFieldIdentifier(ch)
			if id == nil {
				continue
			}
			if out == nil {
				out = make(map[string]bool)
			}
			out[nodeText(id, src)] = true
		}
	}
	return out
}

// cppHasDescendantOfType reports whether n or any of its descendants has the
// given node type.
func cppHasDescendantOfType(n ts.Node, typ string) bool {
	if n == nil {
		return false
	}
	if n.Type() == typ {
		return true
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		if cppHasDescendantOfType(n.Child(i), typ) {
			return true
		}
	}
	return false
}

// cppDescendantFieldIdentifier returns the first field_identifier reachable
// from n (used to unwrap pointer/array/reference/init declarator decoration).
func cppDescendantFieldIdentifier(n ts.Node) ts.Node {
	if n == nil {
		return nil
	}
	if n.Type() == "field_identifier" {
		return n
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		if r := cppDescendantFieldIdentifier(n.Child(i)); r != nil {
			return r
		}
	}
	return nil
}

// cppLeafTypeName strips template/namespace decoration from a base-class name
// to the bare leaf identifier used as the intra-file EXTENDS target.
// `ns::Base<T>` → "Base".
func cppLeafTypeName(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.IndexByte(name, '<'); i >= 0 {
		name = strings.TrimSpace(name[:i])
	}
	if i := strings.LastIndex(name, "::"); i >= 0 {
		name = name[i+2:]
	}
	return strings.TrimSpace(name)
}

// attachCppFieldMembership wires, in one post-walk pass:
//
//   - class→field CONTAINS edges for every SCOPE.Schema/field member emitted
//     by emitClassFieldMembers (mirrors the Go #4855 attachClassContains field
//     loop). Without this edge a C++ data class has zero field children and the
//     dashboard shape tree returns rows:[].
//   - class→base EXTENDS edges from each owner's stashed Metadata
//     "base_candidates", restricted to base types declared in this same file so
//     the shape walker can recurse into inherited members (mirrors the Go
//     embedded-field EXTENDS policy).
func attachCppFieldMembership(records []types.EntityRecord, filePath, lang string) []types.EntityRecord {
	// In-file component name set (for conservative EXTENDS).
	known := make(map[string]bool)
	for i := range records {
		if records[i].Kind == "SCOPE.Component" {
			known[records[i].Name] = true
		}
	}

	// Index components by name for edge attachment.
	compIdx := make(map[string]int)
	for i := range records {
		if records[i].Kind == "SCOPE.Component" {
			if _, dup := compIdx[records[i].Name]; !dup {
				compIdx[records[i].Name] = i
			}
		}
	}

	hasEdge := func(rels []types.RelationshipRecord, toID, kind string) bool {
		for _, ex := range rels {
			if ex.ToID == toID && ex.Kind == kind {
				return true
			}
		}
		return false
	}

	// class→field CONTAINS.
	for _, r := range records {
		if r.Kind != "SCOPE.Schema" || r.Subtype != "field" {
			continue
		}
		owner, _ := r.Metadata["owner"].(string)
		i, ok := compIdx[owner]
		if owner == "" || !ok {
			continue
		}
		toID := extractor.BuildSchemaFieldStructuralRef(lang, filePath, r.Name)
		if !hasEdge(records[i].Relationships, toID, "CONTAINS") {
			records[i].Relationships = append(records[i].Relationships,
				types.RelationshipRecord{ToID: toID, Kind: "CONTAINS"})
		}
	}

	// class→base EXTENDS (in-file bases only).
	for i := range records {
		if records[i].Kind != "SCOPE.Component" || records[i].Metadata == nil {
			continue
		}
		cands, _ := records[i].Metadata["base_candidates"].([]string)
		if len(cands) == 0 {
			continue
		}
		owner := records[i].Name
		for _, raw := range cands {
			base := cppLeafTypeName(raw)
			if base == "" || base == owner || !known[base] {
				continue
			}
			if !hasEdge(records[i].Relationships, base, "EXTENDS") {
				records[i].Relationships = append(records[i].Relationships,
					types.RelationshipRecord{FromID: owner, ToID: base, Kind: "EXTENDS"})
			}
		}
		delete(records[i].Metadata, "base_candidates")
	}

	return records
}
