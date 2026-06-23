package javascript

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// classHeritageRels extracts the generic EXTENDS / IMPLEMENTS edges from a
// class (or interface) declaration's heritage clause (#4322).
//
// Before this pass the JS/TS extractor only emitted IMPLEMENTS edges for the
// narrow Angular guard / interceptor interfaces (angular_rxjs_guards.go); every
// other heritage relationship was dropped. That left whole families of
// framework-interface implementors and base-class subclasses as graph islands
// on real NestJS/TypeORM corpora — e.g. `class FooInterceptor implements
// NestInterceptor`, `class Bar implements NestMiddleware`,
// `class AuditSub implements EntitySubscriberInterface`,
// `class User extends AuditableEntity`. Each such class was extracted as a
// SCOPE.Component with no inbound or outbound structural edge.
//
// This helper mirrors the Java extractor's heritage emission
// (internal/extractors/java/java.go, #1996): it walks the heritage clause and
// emits one EXTENDS edge per superclass and one IMPLEMENTS edge per implemented
// interface, with a BARE leaf type name as ToID. A bare name resolves through
// the global byName index to a same-repo class/interface when one exists
// (e.g. AuditableEntity, MinimalEntity defined in the same project); when the
// target is an external framework interface (NestInterceptor, NestMiddleware,
// OnApplicationBootstrap, …) there is no entity to bind to and the edge stays
// unresolved — but it STILL connects the implementer, so the class is no
// longer an orphan island. This matches the convention already used by the
// Java and Python extractors.
//
// tree-sitter (TypeScript / JavaScript) grammar shape:
//
//	class_declaration
//	  class_heritage
//	    extends_clause      extends Base, Mixin(...)   -> EXTENDS Base, Mixin
//	    implements_clause   implements IFoo, IBar      -> IMPLEMENTS IFoo, IBar
//
// JavaScript classes only carry `extends`; TypeScript adds `implements` and
// allows multiple `extends` targets on interface declarations. We accept the
// union so the same helper serves both grammars.
//
// Guardrail (#4322): edges are emitted ONLY from a real heritage clause node.
// No name is synthesised, and a clause with no resolvable leaf type contributes
// no edge. Each ToID carries the implementer/target names in Properties for
// docgen (ClassManifest bases/interfaces) parity with the Java path.
func (x *extractor) classHeritageRels(class ts.Node, className string) []types.RelationshipRecord {
	if class == nil || className == "" {
		return nil
	}
	var rels []types.RelationshipRecord
	for i := 0; i < int(class.ChildCount()); i++ {
		h := class.Child(i)
		if h == nil || h.Type() != "class_heritage" {
			continue
		}
		for j := 0; j < int(h.ChildCount()); j++ {
			clause := h.Child(j)
			if clause == nil {
				continue
			}
			var kind types.RelationshipKind
			switch clause.Type() {
			case "extends_clause":
				kind = types.RelationshipKindExtends
			case "implements_clause":
				kind = types.RelationshipKindImplements
			default:
				continue
			}
			for _, name := range x.heritageClauseTypeNames(clause) {
				rels = append(rels, types.RelationshipRecord{
					ToID: name,
					Kind: string(kind),
					Properties: map[string]string{
						"subtype": strings.ToLower(string(kind)),
						"from":    className,
						"to":      name,
					},
				})
			}
		}
	}
	return rels
}

// heritageClauseTypeNames returns the distinct leaf type names referenced by an
// extends_clause / implements_clause node, in source order. It accepts the
// identifier shapes tree-sitter produces for heritage targets — bare
// identifiers, type_identifiers, generic_type (Foo<T>), member/qualified names
// (ns.Foo) — and reduces each to its leaf type name via heritageLeafTypeName.
// Punctuation tokens (the `extends`/`implements` keywords, commas) are skipped.
func (x *extractor) heritageClauseTypeNames(clause ts.Node) []string {
	var out []string
	seen := map[string]bool{}
	for k := 0; k < int(clause.ChildCount()); k++ {
		c := clause.Child(k)
		if c == nil {
			continue
		}
		switch c.Type() {
		case "identifier", "type_identifier", "generic_type",
			"member_expression", "nested_type_identifier":
			name := heritageLeafTypeName(x.nodeText(c))
			if name != "" && !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		case "call_expression":
			// Issue #4845 — NestJS mapped-type DTOs declare their base via a
			// helper CALL in the extends clause:
			//
			//	class UpdateThingBody extends PartialType(CreateThingBody) {}
			//	class PickThingBody   extends PickType(CreateThingBody, [...]) {}
			//	class OmitThingBody   extends OmitType(CreateThingBody, [...]) {}
			//	class MergedBody      extends IntersectionType(A, B) {}
			//
			// tree-sitter parses this as a call_expression, which the
			// identifier/generic_type cases above do not match — so before
			// this branch the *real* base classes (the type ARGUMENTS that
			// carry the fields) were dropped and the mapped DTO became a leaf
			// with no EXTENDS edge to walk. We emit an EXTENDS edge to each
			// type-identifier argument of a known mapped-type helper so the
			// shape walker can recurse into the source DTO's fields. The
			// helper name itself (PartialType/PickType/…) is intentionally NOT
			// emitted — it is an external @nestjs/mapped-types/swagger symbol
			// with no fields of its own.
			for _, name := range mappedTypeBaseNames(x, c) {
				if name != "" && !seen[name] {
					seen[name] = true
					out = append(out, name)
				}
			}
		}
	}
	return out
}

// nestMappedTypeHelpers are the NestJS @nestjs/mapped-types / @nestjs/swagger
// helper functions whose type ARGUMENTS are the field-bearing base DTO(s).
var nestMappedTypeHelpers = map[string]bool{
	"PartialType":      true,
	"PickType":         true,
	"OmitType":         true,
	"IntersectionType": true,
}

// mappedTypeBaseNames returns the base DTO type names referenced by the
// arguments of a mapped-type helper call (PartialType(X) -> ["X"]). It returns
// nil for any call whose callee is not a recognized NestJS mapped-type helper,
// so non-mapped extends-call shapes contribute no edge (#4845).
func mappedTypeBaseNames(x *extractor, call ts.Node) []string {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return nil
	}
	if !nestMappedTypeHelpers[heritageLeafTypeName(x.nodeText(fn))] {
		return nil
	}
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	var out []string
	for i := 0; i < int(args.ChildCount()); i++ {
		a := args.Child(i)
		if a == nil {
			continue
		}
		switch a.Type() {
		case "identifier", "type_identifier", "generic_type",
			"member_expression", "nested_type_identifier":
			if name := heritageLeafTypeName(x.nodeText(a)); name != "" {
				out = append(out, name)
			}
		}
	}
	return out
}

// appendHeritageDeduped appends heritage edges (EXTENDS/IMPLEMENTS) to rels,
// skipping any whose (Kind, ToID) pair already appears in rels. Used by the
// Angular class path where a narrow guard IMPLEMENTS edge may already be
// present for the same interface (#4322).
func appendHeritageDeduped(rels, heritage []types.RelationshipRecord) []types.RelationshipRecord {
	if len(heritage) == 0 {
		return rels
	}
	seen := map[string]bool{}
	for _, r := range rels {
		if r.Kind == string(types.RelationshipKindExtends) ||
			r.Kind == string(types.RelationshipKindImplements) {
			seen[r.Kind+"\x00"+r.ToID] = true
		}
	}
	for _, r := range heritage {
		key := r.Kind + "\x00" + r.ToID
		if seen[key] {
			continue
		}
		seen[key] = true
		rels = append(rels, r)
	}
	return rels
}

// heritageLeafTypeName reduces a heritage target's source text to its leaf type
// name: strips a generic argument list (`Foo<Bar>` -> `Foo`), a qualified
// namespace prefix (`ns.Sub.Foo` -> `Foo`), and surrounding whitespace. Returns
// "" for empty / non-identifier input so callers emit no edge.
func heritageLeafTypeName(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '<'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	// Qualified name: keep the last dotted segment (the leaf type).
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Must start with an identifier character (letter, _ or $).
	c := s[0]
	if !(c == '_' || c == '$' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) {
		return ""
	}
	return s
}
