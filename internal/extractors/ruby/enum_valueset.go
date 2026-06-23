package ruby

// enum_valueset.go — value-carrying SCOPE.Enum value-set nodes for Rails
// ActiveRecord `enum` declarations (data-model, epic #3628 / completes #3806).
// Reuses the shared cross-language builder in internal/extractor (EnumEntity /
// EnumMember / StripLiteralQuotes) so Rails enums converge on the same node
// model as Python, C#, TS, Java and Go.
//
// Recognised forms (class-body `call` nodes whose method identifier is `enum`):
//
//	enum status: { active: 0, archived: 1 }   # hash → members WITH values
//	enum status: [:active, :archived]         # array → members, value-less
//	enum :status, { active: 0 }               # Rails 7 positional name form
//
// The enum's NAME is the attribute (`status`); its MEMBERS are the hash keys /
// array symbols, and member values are the hash values when statically known.
// Honest-partial: a dynamic argument (a method call, a constant reference) is
// skipped — only literal hash/array forms produce a node.

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// buildRailsEnumValueSet inspects a `call` node and, when it is a Rails
// `enum <name>: {...}` / `enum <name>: [...]` / `enum :<name>, {...}`
// declaration, returns the SCOPE.Enum value-set node for it.
func buildRailsEnumValueSet(node ts.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	if node == nil || node.Type() != "call" {
		return types.EntityRecord{}, false
	}
	if !rubyCallIsNamed(node, file.Content, "enum") {
		return types.EntityRecord{}, false
	}
	args := node.ChildByFieldName("arguments")
	if args == nil {
		args = firstChildOfType(node, "argument_list")
	}
	if args == nil {
		return types.EntityRecord{}, false
	}

	enumName, valueNode := railsEnumNameAndValues(args, file.Content)
	if enumName == "" || valueNode == nil {
		return types.EntityRecord{}, false
	}

	members := railsEnumMembers(valueNode, file.Content)
	return extractor.EnumEntity(
		enumName, "ruby", "rails_enum", file.Path,
		int(node.StartPoint().Row)+1, int(node.EndPoint().Row)+1, members,
	)
}

// railsEnumNameAndValues extracts the enum attribute name and the node holding
// its members from a `enum` call's argument_list. Handles both the
// `status: {...}` pair form and the `:status, {...}` positional form.
func railsEnumNameAndValues(args ts.Node, src []byte) (string, ts.Node) {
	// Pair form: `enum status: { ... }` → a single `pair` argument.
	if pair := firstChildOfType(args, "pair"); pair != nil {
		key := pairKeyName(pair, src)
		val := pairValueNode(pair)
		if key != "" && (nodeIsHashOrArray(val)) {
			return key, val
		}
	}
	// Positional form: `enum :status, { ... }` → simple_symbol then hash/array.
	var name string
	var values ts.Node
	for i := 0; i < int(args.ChildCount()); i++ {
		ch := args.Child(i)
		if ch == nil || !ch.IsNamed() {
			continue
		}
		switch ch.Type() {
		case "simple_symbol":
			if name == "" {
				name = trimLeadingColon(nodeText(ch, src))
			}
		case "hash", "array":
			if values == nil {
				values = ch
			}
		}
	}
	if name != "" && values != nil {
		return name, values
	}
	return "", nil
}

// railsEnumMembers builds EnumMembers from a hash (`{ active: 0 }`) or array
// (`[:active, :archived]`) value node.
func railsEnumMembers(val ts.Node, src []byte) []extractor.EnumMember {
	var members []extractor.EnumMember
	switch val.Type() {
	case "hash":
		for i := 0; i < int(val.ChildCount()); i++ {
			pair := val.Child(i)
			if pair == nil || pair.Type() != "pair" {
				continue
			}
			key := pairKeyName(pair, src)
			if key == "" {
				continue
			}
			mval := ""
			if v := pairValueNode(pair); v != nil {
				mval = railsLiteral(v, src)
			}
			members = append(members, extractor.EnumMember{Name: key, Value: mval})
		}
	case "array":
		for i := 0; i < int(val.ChildCount()); i++ {
			el := val.Child(i)
			if el == nil || !el.IsNamed() {
				continue
			}
			switch el.Type() {
			case "simple_symbol":
				members = append(members, extractor.EnumMember{Name: trimLeadingColon(nodeText(el, src))})
			case "string":
				members = append(members, extractor.EnumMember{Name: extractor.StripLiteralQuotes(nodeText(el, src))})
			}
		}
	}
	return members
}

// railsLiteral returns the statically-known literal value of a hash value node,
// or "" for non-literal values (honest-partial).
func railsLiteral(n ts.Node, src []byte) string {
	switch n.Type() {
	case "integer", "float":
		return nodeText(n, src)
	case "string":
		return extractor.StripLiteralQuotes(nodeText(n, src))
	case "simple_symbol":
		return trimLeadingColon(nodeText(n, src))
	}
	return ""
}

// --- small node helpers (local to this file) ---------------------------------

func rubyCallIsNamed(call ts.Node, src []byte, name string) bool {
	if m := call.ChildByFieldName("method"); m != nil {
		return nodeText(m, src) == name
	}
	// Command form: first identifier child is the method name.
	if id := firstChildOfType(call, "identifier"); id != nil {
		return nodeText(id, src) == name
	}
	return false
}

func pairKeyName(pair ts.Node, src []byte) string {
	if k := pair.ChildByFieldName("key"); k != nil {
		return trimLeadingColon(nodeText(k, src))
	}
	if k := firstChildOfType(pair, "hash_key_symbol"); k != nil {
		return nodeText(k, src)
	}
	if k := firstChildOfType(pair, "simple_symbol"); k != nil {
		return trimLeadingColon(nodeText(k, src))
	}
	return ""
}

func pairValueNode(pair ts.Node) ts.Node {
	if v := pair.ChildByFieldName("value"); v != nil {
		return v
	}
	// Fall back to the last named child (after the key).
	for i := int(pair.ChildCount()) - 1; i >= 0; i-- {
		ch := pair.Child(i)
		if ch != nil && ch.IsNamed() && ch.Type() != "hash_key_symbol" {
			return ch
		}
	}
	return nil
}

func nodeIsHashOrArray(n ts.Node) bool {
	return n != nil && (n.Type() == "hash" || n.Type() == "array")
}

func trimLeadingColon(s string) string {
	if len(s) > 0 && s[0] == ':' {
		return s[1:]
	}
	return s
}

func firstChildOfType(n ts.Node, typ string) ts.Node {
	if n == nil {
		return nil
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		if ch != nil && ch.Type() == typ {
			return ch
		}
	}
	return nil
}

func nodeText(n ts.Node, src []byte) string {
	if n == nil {
		return ""
	}
	return string(src[n.StartByte():n.EndByte()])
}
