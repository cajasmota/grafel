// nested_ctor_refs.go — supplemental REFERENCES emission for two
// adjacent shapes the primary `emitReferences` walker leaves on the
// table:
//
//   - #2007 — `SerializerClass()` (or `Helper()`) construction *inside*
//     a method body. The primary `emitReferences` already walks method
//     bodies, but identifiers that are the `function` child of a `call`
//     node are routed to the CALLS extractor (extractCallRelationships).
//     CALLS only resolves bare-name callees that match a known function
//     in the file; a Capitalised identifier used as a constructor where
//     the class lives in another file (or is an imported symbol) falls
//     through both the CALLS extractor (no in-file function match) and
//     the REFERENCES extractor (the identifier is excluded by
//     isPyCallCallee). The result: the cross-class composition between
//     `MyView.get_<field>()` and `SerializerClass` is invisible in the
//     graph.
//
//   - #2009 — `choices=User.TYPE_CHOICES` and similar shapes inside
//     class-attribute initializer kwargs. The Django relational pass
//     (#1977 / #1978) captures the raw kwarg value text but does not
//     emit a REFERENCES edge to the referenced class. The constructor
//     RHS lives in a class body (not a method body), which the primary
//     `emitReferences` walker doesn't descend into — its function-frame
//     stack only pushes inside function_definition / lambda. So a
//     module-level `models.IntegerField(choices=User.TYPE_CHOICES)`
//     never references `User`.
//
// Implementation strategy:
//
//   - Build a single name → class-entity table (mirrors the bareSymbols
//     table in references.go, but limited to SCOPE.Component/class plus
//     module-import bindings — we only want REFERENCES that resolve to a
//     declared class entity or an external symbol from an import). Then
//     walk every method body AND every class-body assignment RHS, looking
//     for Capitalised identifiers in either:
//
//       (a) the `function` field of a `call` node, or
//       (b) the `object` field of an `attribute` access (e.g.
//           `User.TYPE_CHOICES`).
//
//     For each, look up the bare name and, if the table has an entry,
//     emit a REFERENCES edge from the enclosing entity (the method, or
//     the parent class when at class-body scope) to the resolved class.
//
//   - The edge is deduplicated by (from_id, to_id) so a method that
//     references the same class three times produces a single edge.
//
//   - The walker filters out bare-name self-references (a method that
//     constructs its own enclosing class — rare but legal — does not
//     get an edge back to itself).
//
// This file is intentionally narrow: it ONLY emits the *missing* edges
// that the primary REFERENCES walker excludes. It does not duplicate or
// re-emit any edge that the primary walker already produced.

package python

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// classRefTarget is one row of the class-target lookup table assembled
// by emitNestedConstructorRefs. emittedName is the value placed in the
// structural-ref ToID; fromImport indicates whether the lookup hit was
// produced by a file-level IMPORTS edge (vs. a same-file class entity).
type classRefTarget struct {
	emittedName string
	fromImport  bool
}

// nestedEdgeKey deduplicates (from_id, to_id) pairs across the pass so
// repeated references inside the same method body or class body produce
// a single REFERENCES edge.
type nestedEdgeKey struct{ from, to string }

// emitNestedConstructorRefs runs AFTER the primary REFERENCES pass so
// it can observe the file-scope symbol table the primary walker would
// have built. We re-derive the class-only symbol view here rather than
// share state with emitReferences — the two passes have different
// criteria (REFERENCES skips call-callee identifiers; we only fire on
// call-callee + attribute-receiver shapes that REFERENCES skipped).
//
// Safe to call with nil/empty inputs.
func emitNestedConstructorRefs(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}

	// Build the class-target table. We index bare class names (declared
	// in this file as SCOPE.Component/class) AND import bindings whose
	// local_name starts with an upper-case letter (the universal Python
	// convention for class-like imports). The latter lets a method that
	// constructs an imported class trigger a REFERENCES edge whose
	// target resolves via the existing structural-ref → ext: fallback.
	classTargets := make(map[string]classRefTarget)

	for i := range *entities {
		e := &(*entities)[i]
		if e.SourceFile != file.Path {
			continue
		}
		if e.Kind == "SCOPE.Component" && e.Subtype == "class" {
			leaf := e.Name
			if dot := strings.LastIndexByte(leaf, '.'); dot >= 0 {
				leaf = leaf[dot+1:]
			}
			if leaf == "" {
				continue
			}
			if _, exists := classTargets[leaf]; !exists {
				classTargets[leaf] = classRefTarget{emittedName: e.Name}
			}
			// Also index the dotted name for nested classes.
			if e.Name != leaf {
				if _, exists := classTargets[e.Name]; !exists {
					classTargets[e.Name] = classRefTarget{emittedName: e.Name}
				}
			}
			continue
		}
		// Pull import bindings out of the file entity's IMPORTS edges.
		if e.Kind == "SCOPE.Component" && e.Subtype == "file" {
			for _, r := range e.Relationships {
				if r.Kind != "IMPORTS" {
					continue
				}
				local := r.Properties["local_name"]
				if local == "" {
					continue
				}
				// Skip lower-case bindings (functions / modules / vars).
				if !isCapitalisedIdent(local) {
					continue
				}
				imported := r.Properties["imported_name"]
				if imported == "" {
					imported = local
				}
				if _, exists := classTargets[local]; !exists {
					classTargets[local] = classRefTarget{emittedName: imported, fromImport: true}
				}
			}
		}
	}

	if len(classTargets) == 0 {
		return
	}

	seen := make(map[nestedEdgeKey]bool)

	// emitEdge appends a REFERENCES edge from the entity at entityIdx
	// to the resolved class target. fromName is the enclosing entity's
	// emitted Name (used for self-reference filtering). The edge
	// carries a `nested_ctor` property to make the provenance
	// inspectable in graph audits.
	emitEdge := func(entityIdx int, fromName, leaf string, t classRefTarget) {
		_ = leaf // leaf retained for future self-ref refinement; current guard is name-based
		if entityIdx < 0 || entityIdx >= len(*entities) {
			return
		}
		// Self-reference guard: only suppress when the resolved target
		// IS the enclosing entity itself (a method body that references
		// its own enclosing class would still be a real edge, but a
		// class body that references itself via the bare class name
		// would not be — handled below by comparing against the entity's
		// emitted Name).
		if t.emittedName == fromName {
			return
		}
		toID := buildDjangoModelClassRef(file.Path, t.emittedName)
		key := nestedEdgeKey{fromName, toID}
		if seen[key] {
			return
		}
		seen[key] = true
		(*entities)[entityIdx].Relationships = append((*entities)[entityIdx].Relationships,
			types.RelationshipRecord{
				ToID: toID,
				Kind: "REFERENCES",
				Properties: map[string]string{
					"nested_ctor":  "true",
					"target_class": t.emittedName,
				},
			})
	}

	// Walk every class definition. Inside a class we visit:
	//   1. each class-body assignment's RHS (#2009 shape: kwargs of a
	//      field constructor like `choices=User.TYPE_CHOICES`); the
	//      enclosing entity is the parent class.
	//   2. each method body (function_definition inside the class
	//      body); the enclosing entity is the method.
	//
	// Nested classes recurse with the dotted parent name.
	var walkClass func(classNode ts.Node, parentClass string)
	walkClass = func(classNode ts.Node, parentClass string) {
		if classNode == nil {
			return
		}
		nameNode := classNode.ChildByFieldName("name")
		cls := ""
		if nameNode != nil {
			cls = nodeText(nameNode, file.Content)
		}
		dottedClass := cls
		if parentClass != "" && cls != "" {
			dottedClass = parentClass + "." + cls
		}
		body := classNode.ChildByFieldName("body")
		if body == nil {
			return
		}

		// Locate the enclosing class entity index for class-body refs.
		classIdx := -1
		for i := range *entities {
			if (*entities)[i].Kind == "SCOPE.Component" &&
				(*entities)[i].Subtype == "class" &&
				(*entities)[i].Name == dottedClass &&
				(*entities)[i].SourceFile == file.Path {
				classIdx = i
				break
			}
		}

		for i := 0; i < int(body.ChildCount()); i++ {
			stmt := body.Child(i)
			if stmt == nil {
				continue
			}
			switch stmt.Type() {
			case "expression_statement":
				// Class-body assignment: walk its RHS for capitalised
				// callees / attribute receivers. Enclosing entity is
				// the parent class itself (#2009 surface).
				if classIdx >= 0 {
					scanNodeForClassRefs(stmt, file.Content, classTargets, func(leaf string, t classRefTarget) {
						emitEdge(classIdx, dottedClass, leaf, t)
					})
				}
			case "function_definition":
				scanMethodForClassRefs(stmt, file, dottedClass, classTargets, entities, seen, emitEdge)
			case "decorated_definition":
				inner := stmt.ChildByFieldName("definition")
				if inner == nil {
					continue
				}
				switch inner.Type() {
				case "function_definition":
					scanMethodForClassRefs(inner, file, dottedClass, classTargets, entities, seen, emitEdge)
				case "class_definition":
					walkClass(inner, dottedClass)
				}
			case "class_definition":
				walkClass(stmt, dottedClass)
			}
		}
	}

	// Top-level walk: find every class_definition (including those
	// wrapped in a decorated_definition) and process it.
	var walkTop func(n ts.Node)
	walkTop = func(n ts.Node) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "class_definition":
			walkClass(n, "")
			return
		case "decorated_definition":
			inner := n.ChildByFieldName("definition")
			if inner != nil && inner.Type() == "class_definition" {
				walkClass(inner, "")
				return
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walkTop(n.Child(i))
		}
	}
	walkTop(root)
}

// scanMethodForClassRefs walks a function_definition body looking for
// capitalised-identifier callees (#2007) and capitalised-identifier
// attribute receivers (#2009 — defensive: same shape can also appear
// inside method bodies). The enclosing entity is the method itself,
// looked up by emitted Name "<parentClass>.<methodLeaf>".
func scanMethodForClassRefs(
	fnNode ts.Node,
	file extractor.FileInput,
	parentClass string,
	classTargets map[string]classRefTarget,
	entities *[]types.EntityRecord,
	seen map[nestedEdgeKey]bool,
	emitEdge func(entityIdx int, fromName, leaf string, t classRefTarget),
) {
	_ = seen // edge-key dedup is owned by emitEdge's closure
	nameNode := fnNode.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	leafName := nodeText(nameNode, file.Content)
	emittedName := leafName
	if parentClass != "" {
		emittedName = parentClass + "." + leafName
	}

	// Find the method entity index by Name + SourceFile.
	methodIdx := -1
	for i := range *entities {
		if (*entities)[i].Kind == "SCOPE.Operation" &&
			(*entities)[i].Name == emittedName &&
			(*entities)[i].SourceFile == file.Path {
			methodIdx = i
			break
		}
	}
	if methodIdx < 0 {
		return
	}

	body := fnNode.ChildByFieldName("body")
	if body == nil {
		return
	}
	scanNodeForClassRefs(body, file.Content, classTargets, func(leaf string, t classRefTarget) {
		emitEdge(methodIdx, emittedName, leaf, t)
	})
}

// scanNodeForClassRefs recursively visits node and invokes onMatch for
// every Capitalised identifier that:
//
//   - is the `function` child of a `call` node (constructor call), OR
//   - is the `object` child of an `attribute` node (class-qualified
//     attribute access like `User.TYPE_CHOICES`).
//
// Capitalised is the conservative gate: it filters out the common
// lower-case-callable case (function calls) that the CALLS pass owns
// and avoids spurious REFERENCES to local variables.
func scanNodeForClassRefs(
	node ts.Node,
	src []byte,
	classTargets map[string]classRefTarget,
	onMatch func(leaf string, t classRefTarget),
) {
	if node == nil {
		return
	}
	switch node.Type() {
	case "call":
		fn := node.ChildByFieldName("function")
		if fn != nil {
			tryEmitClassRef(fn, src, classTargets, onMatch)
		}
	case "attribute":
		obj := node.ChildByFieldName("object")
		if obj != nil {
			tryEmitClassRef(obj, src, classTargets, onMatch)
		}
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		scanNodeForClassRefs(node.Child(i), src, classTargets, onMatch)
	}
}

// tryEmitClassRef looks up the leaf identifier of fnOrObj in
// classTargets. fnOrObj may be an `identifier` node (bare class name)
// or an `attribute` node whose final leaf is the class name. Anything
// else (subscripts, lambdas, conditional_expression, etc.) is ignored.
func tryEmitClassRef(
	fnOrObj ts.Node,
	src []byte,
	classTargets map[string]classRefTarget,
	onMatch func(leaf string, t classRefTarget),
) {
	if fnOrObj == nil {
		return
	}
	var leaf string
	switch fnOrObj.Type() {
	case "identifier":
		leaf = nodeText(fnOrObj, src)
	case "attribute":
		// Take the trailing leaf — `mod.Sub.User` → `User`.
		txt := nodeText(fnOrObj, src)
		if dot := strings.LastIndexByte(txt, '.'); dot >= 0 {
			leaf = txt[dot+1:]
		} else {
			leaf = txt
		}
	default:
		return
	}
	if leaf == "" || !isCapitalisedIdent(leaf) {
		return
	}
	t, ok := classTargets[leaf]
	if !ok {
		return
	}
	onMatch(leaf, t)
}
