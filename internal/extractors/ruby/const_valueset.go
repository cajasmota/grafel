package ruby

// const_valueset.go — value-carrying SCOPE.Enum value-set nodes for Ruby
// CONSTANT COLLECTIONS (#4427, extends #4429 / epic #4419, ref #4334).
//
// #4429 indexed Rails ActiveRecord `enum` declarations (see enum_valueset.go).
// This file generalises the same SCOPE.Enum value-set model to the plain Ruby
// constant-collection shapes that act as source-of-truth maps but were
// invisible to search_entities and could not be diffed by a downstream
// cross-graph parity-audit:
//
//	# frozen / plain constant HASH (symbol or string keys)
//	PERMISSION_PAGES = { core_admin: 'core-admin', billing: 'billing' }.freeze
//	LIMITS = { 'free' => 10, 'pro' => 100 }
//
//	# constant ARRAY of literals (%w / %i / [...] / .freeze)
//	STATUSES = %w[active inactive].freeze
//	IDS      = %i[admin user]
//	ROLES    = ['admin', 'user']
//
//	# module-level / class-level constant GROUP
//	module Roles
//	  ADMIN = 'admin'
//	  USER  = 'user'
//	end
//
// Reuses the shared cross-language builder in internal/extractor (EnumEntity /
// EnumMember / StripLiteralQuotes) so Ruby constant collections converge on the
// same node model — and the same structured members_json [{key,value,line}] —
// as Python const maps, TS const-objects, and Rails enums.
//
// Honest-partial: an empty collection emits NO node. A member whose value is a
// non-literal expression (a method call, a constant reference) still records
// the member — with its source EXPRESSION TEXT as the value (#4427 scope) —
// rather than being dropped, so the key set stays complete for a parity-audit.

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// buildConstCollectionValueSet inspects an `assignment` node and, when its
// left side is a `constant` bound to a hash / array / string_array /
// symbol_array literal collection (optionally wrapped in a `.freeze` call),
// returns the SCOPE.Enum value-set node for it.
//
// kind_hint is "ruby_const_hash" for hash collections and "ruby_const_array"
// for array collections, so a downstream consumer can tell the member shape.
func buildConstCollectionValueSet(node ts.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	if node == nil || node.Type() != "assignment" {
		return types.EntityRecord{}, false
	}
	lhs := node.ChildByFieldName("left")
	if lhs == nil || lhs.Type() != "constant" {
		return types.EntityRecord{}, false
	}
	rhs := node.ChildByFieldName("right")
	if rhs == nil {
		return types.EntityRecord{}, false
	}

	coll := unwrapFreeze(rhs)
	if coll == nil {
		return types.EntityRecord{}, false
	}

	name := nodeText(lhs, file.Content)
	var members []extractor.EnumMember
	var kindHint string
	switch coll.Type() {
	case "hash":
		members = constHashMembers(coll, file.Content)
		kindHint = "ruby_const_hash"
	case "array", "string_array", "symbol_array":
		members = constArrayMembers(coll, file.Content)
		kindHint = "ruby_const_array"
	default:
		return types.EntityRecord{}, false
	}
	if len(members) == 0 {
		return types.EntityRecord{}, false
	}

	return extractor.EnumEntity(
		name, "ruby", kindHint, file.Path,
		int(node.StartPoint().Row)+1, int(node.EndPoint().Row)+1, members,
	)
}

// buildModuleConstGroupValueSet aggregates the constant assignments declared
// directly in a module/class body into a single SCOPE.Enum value-set named
// after the module/class. Each `CONST = <literal-or-expr>` becomes one member
// {key=CONST, value=<literal>|<expr-text>, line}. Returns ok=false when the
// body has no such constant assignments, or when a single constant already
// holds a collection (that constant gets its own node via
// buildConstCollectionValueSet, so it is excluded here).
//
// kind_hint is "ruby_const_module".
func buildModuleConstGroupValueSet(scopeName string, body ts.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	if body == nil || scopeName == "" {
		return types.EntityRecord{}, false
	}
	var members []extractor.EnumMember
	for i := 0; i < int(body.ChildCount()); i++ {
		child := body.Child(i)
		if child == nil || child.Type() != "assignment" {
			continue
		}
		lhs := child.ChildByFieldName("left")
		if lhs == nil || lhs.Type() != "constant" {
			continue
		}
		rhs := child.ChildByFieldName("right")
		if rhs == nil {
			continue
		}
		// A constant bound to a collection literal is its OWN value-set
		// (buildConstCollectionValueSet handles it) — don't fold it into the
		// group as a single opaque member.
		if coll := unwrapFreeze(rhs); coll != nil && isCollectionNode(coll) {
			continue
		}
		members = append(members, extractor.EnumMember{
			Name:  nodeText(lhs, file.Content),
			Value: constScalarValue(rhs, file.Content),
			Line:  int(child.StartPoint().Row) + 1,
		})
	}
	if len(members) == 0 {
		return types.EntityRecord{}, false
	}
	return extractor.EnumEntity(
		scopeName, "ruby", "ruby_const_module", file.Path,
		int(body.StartPoint().Row)+1, int(body.EndPoint().Row)+1, members,
	)
}

// unwrapFreeze returns the underlying collection node, peeling a trailing
// `.freeze` (or any other no-arg method call) wrapper. For
// `{ a: 1 }.freeze` tree-sitter wraps the hash in a `call` whose receiver is
// the hash; this returns that hash. A bare `{ a: 1 }` / `%w[..]` returns the
// node unchanged.
func unwrapFreeze(n ts.Node) ts.Node {
	if n == nil {
		return nil
	}
	if n.Type() == "call" {
		if recv := n.ChildByFieldName("receiver"); recv != nil {
			return recv
		}
		// Receiver is not exposed as a field in some grammar versions — fall
		// back to the first named child, which is the call's receiver.
		for i := 0; i < int(n.ChildCount()); i++ {
			ch := n.Child(i)
			if ch != nil && ch.IsNamed() && isCollectionNode(ch) {
				return ch
			}
		}
		return nil
	}
	return n
}

// isCollectionNode reports whether n is one of the literal collection node
// types this extractor materialises into a value-set.
func isCollectionNode(n ts.Node) bool {
	if n == nil {
		return false
	}
	switch n.Type() {
	case "hash", "array", "string_array", "symbol_array":
		return true
	}
	return false
}

// constHashMembers builds EnumMembers from a `hash` node. Symbol keys
// (`core_admin:`) and string/rocket keys (`'free' => 10`) are both handled.
// A non-literal value records its source expression text (#4427) rather than
// being dropped.
func constHashMembers(hash ts.Node, src []byte) []extractor.EnumMember {
	var members []extractor.EnumMember
	for i := 0; i < int(hash.ChildCount()); i++ {
		pair := hash.Child(i)
		if pair == nil || pair.Type() != "pair" {
			continue
		}
		// Resolve the key. Symbol keys (`core_admin:`) come through the
		// dedicated hash_key_symbol/simple_symbol path; string/rocket keys
		// (`'free' => 10`) and constant keys come through the `key` field and
		// must have their quotes stripped (constScalarValue), so pairKeyName's
		// raw-text result for non-symbol keys is not used here.
		var key string
		if k := pair.ChildByFieldName("key"); k != nil {
			switch k.Type() {
			case "hash_key_symbol":
				key = nodeText(k, src)
			case "simple_symbol":
				key = trimLeadingColon(nodeText(k, src))
			default:
				key = constScalarValue(k, src)
			}
		} else {
			key = pairKeyName(pair, src)
		}
		if key == "" {
			continue
		}
		val := ""
		if v := pairValueNode(pair); v != nil {
			val = constScalarValue(v, src)
		}
		members = append(members, extractor.EnumMember{
			Name:  key,
			Value: val,
			Line:  int(pair.StartPoint().Row) + 1,
		})
	}
	return members
}

// constArrayMembers builds EnumMembers from an `array` / `string_array`
// (`%w[...]`) / `symbol_array` (`%i[...]`) node. Each element's literal becomes
// BOTH the member key and value so the value-set carries the literal set; a
// non-literal element records its expression text.
func constArrayMembers(arr ts.Node, src []byte) []extractor.EnumMember {
	var members []extractor.EnumMember
	for i := 0; i < int(arr.ChildCount()); i++ {
		el := arr.Child(i)
		if el == nil || !el.IsNamed() {
			continue
		}
		var lit string
		switch el.Type() {
		case "bare_string", "bare_symbol":
			// `%w[..]` / `%i[..]` elements: the literal is the inner content.
			lit = nodeText(el, src)
		default:
			lit = constScalarValue(el, src)
		}
		if lit == "" {
			continue
		}
		members = append(members, extractor.EnumMember{
			Name:  lit,
			Value: lit,
			Line:  int(el.StartPoint().Row) + 1,
		})
	}
	return members
}

// constScalarValue returns the statically-known literal of a scalar node
// (string / number / symbol / boolean / constant ref), or — for any other
// non-literal node (a method call, an interpolation) — the trimmed source
// EXPRESSION TEXT (#4427: non-literal values are recorded as expression text,
// not dropped).
func constScalarValue(n ts.Node, src []byte) string {
	if n == nil {
		return ""
	}
	switch n.Type() {
	case "string", "bare_string":
		return extractor.StripLiteralQuotes(nodeText(n, src))
	case "integer", "float":
		return nodeText(n, src)
	case "simple_symbol", "bare_symbol":
		return trimLeadingColon(nodeText(n, src))
	case "hash_key_symbol", "constant":
		return nodeText(n, src)
	case "true", "false", "nil":
		return nodeText(n, src)
	}
	// Non-literal expression (method call, ternary, interpolation, …): keep
	// the source text so the key set stays complete and a parity-audit can see
	// the value is dynamic.
	return nodeText(n, src)
}
