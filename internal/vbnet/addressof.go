package vbnet

import (
	"fmt"
	"strings"
)

// addressof.go — S7b of #6327: the `AddressOf` operand scan.
//
// # Why this is not a Ref
//
// Ref is documented as "one `name(` use site", and every count the epic has
// published — 41,748 use sites, 8,702 of them IsCall, 1,624 qualified-with-no-
// qualifier — is a count of parenthesis sites. `AddressOf Foo` carries no
// parenthesis, so the reference pass has never seen it; S5's package doc names
// that as a known recall limit. Folding it into Refs would either invalidate
// those measurements or require a Ref that is deliberately excluded from every
// one of them, which is a worse contract than a second, smaller slice.
//
// It is also a different KIND of fact. A Ref needs the declaration table to
// decide what its parenthesis MEANS — invocation, index, default-property
// access or generic instantiation — and that ambiguity is the whole reason
// #6327 exists. `AddressOf` has no such ambiguity: the grammar admits exactly
// one production after the keyword, a member-access expression naming a
// method, so the operand is settled syntactically and needs no table verdict.
//
// # Why the scan hangs off scanRefs
//
// The three places an operand can appear — an ordinary statement, a local
// declarator's initialiser and a field declarator's initialiser — are exactly
// the three call sites of scanRefs, with exactly the `from` offset that skips
// the declarator names. Scanning from the same entry point inherits that,
// inherits the masked/attribute-blanked text, and inherits the continuation
// map that makes a site on the fourth physical line report that line.
//
// A method SIGNATURE is not one of those sites, so `Optional f As Action =
// AddressOf Foo` in a parameter list is not recorded. Measured on the 302-file
// corpus: 0 occurrences.

// MethodRef is a method named as a VALUE rather than invoked — the operand of
// an `AddressOf` expression.
//
// Unlike Ref there is no Qualified-without-Qualifier case: the VB.NET grammar
// admits only a member-access expression after `AddressOf`, never an arbitrary
// expression, so an operand whose qualifier this pass cannot name does not
// exist. `AddressOf (f)()` and `AddressOf x.Foo(1)` are both syntax errors.
type MethodRef struct {
	// Name is the final segment: the method being named.
	Name string
	// Qualifier is the dotted prefix, "" for a bare operand.
	Qualifier string
	// Line is the 1-based physical line of the OPERAND, resolved through the
	// continuation map — not the line the statement started on.
	Line int
}

// String renders a MethodRef compactly for test failure messages, matching
// Ref.String's shape.
func (m MethodRef) String() string {
	if m.Qualifier != "" {
		return fmt.Sprintf("%s.%s@%d", m.Qualifier, m.Name, m.Line)
	}
	return fmt.Sprintf("%s@%d", m.Name, m.Line)
}

// addressOfKeyword is matched case-INSENSITIVELY, like every other keyword in
// this package: VB.NET folds identifiers and keywords alike, which is why
// FoldName exists and why strings.EqualFold is used rather than a == on a
// pre-folded slice (folding the whole line would move every byte offset and
// break LineAt).
const addressOfKeyword = "AddressOf"

// scanAddressOf records every `AddressOf <operand>` in text[from:] on the
// innermost open node.
//
// text, textOff, ll and from carry the same meaning as in scanRefs.
func (p *parser) scanAddressOf(text string, textOff int, ll LogicalLine, from int) {
	p.scanAddressOfOn(p.top().node, text, textOff, ll, from)
}

// scanAddressOfOn is scanAddressOf with the owning node given explicitly.
//
// The one caller that needs it is the auto-property arm: walkProperty OPENS
// the property node without PUSHING it (an auto-property has no `End
// Property`, so growing the stack would swallow every later sibling), which
// leaves the innermost open node as the enclosing TYPE. Passing the property
// keeps `Property P As New T With {.F = AddressOf Foo}` on P.
func (p *parser) scanAddressOfOn(owner *Node, text string, textOff int, ll LogicalLine, from int) {
	if from < 0 {
		from = 0
	}
	if owner == nil {
		return
	}
	// No literal-skip arm. Every caller hands over text that walkLine already
	// ran through MaskStringLiterals, so a literal's content arrives as
	// StringFill bytes and cannot spell the keyword — an arm here would be
	// code no reachable input can kill, the same standard that refused an arm
	// for the bracket escape below. If masking is ever moved off this path,
	// that arm comes back with a test that fails without it.
	for i := from; i < len(text); i++ {
		switch text[i] {
		case 'a', 'A':
		default:
			continue
		}

		// Left boundary. An identifier byte before the match means the letters
		// are the tail of a longer name (`theAddressOf As Action` would
		// otherwise record an operand named `As`); a '.' means a MEMBER
		// spelled AddressOf (`obj.AddressOf And x`, legal because the
		// declaration can bracket-escape the reserved word). Neither is the
		// keyword.
		//
		// The bracket escape ITSELF (`[AddressOf]`) needs no arm here: a ']'
		// always follows, and the right-boundary check below already refuses
		// it. An arm for '[' would be code no test could kill.
		if i > 0 && (isIdentByte(text[i-1]) || text[i-1] == '.') {
			continue
		}
		end := i + len(addressOfKeyword)
		if end > len(text) || !strings.EqualFold(text[i:end], addressOfKeyword) {
			continue
		}
		// Right boundary. `AddressOfThing` is one identifier, and `AddressOf]`
		// closes a bracket escape. The keyword must also be SEPARATED from its
		// operand: the grammar has no `AddressOf(` production, so requiring
		// whitespace costs nothing and refuses a call to a member of that name.
		if end == len(text) || (text[end] != ' ' && text[end] != '\t') {
			continue
		}

		qual, name, nameOff, ok := takeDottedName(text[end:])
		// The operand advance is unconditional: whether or not the operand
		// parsed, the keyword itself has been consumed and cannot begin a
		// second match.
		i = end
		if !ok {
			continue
		}
		owner.AddressOfs = append(owner.AddressOfs, MethodRef{
			Name:      name,
			Qualifier: qual,
			Line:      ll.LineAt(textOff + end + nameOff),
		})
	}
}

// takeDottedName peels a dotted identifier chain (`A.B.C`, `Me.Foo`,
// `[Global].Bar`) off the front of s.
//
// It returns everything before the final segment as qualifier, the final
// segment as name, and the offset of that final segment within s so the caller
// can resolve the OPERAND's physical line rather than the keyword's. ok is
// false when s does not begin with an identifier, which is the `AddressOf` at
// end-of-statement case.
//
// Whitespace is allowed around the dots because a continued statement puts a
// line break there — `AddressOf _ \n Handler` arrives here as one logical line
// with the continuation collapsed to a space.
func takeDottedName(s string) (qualifier, name string, nameOff int, ok bool) {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	var parts []string
	for {
		segStart := i
		switch {
		case i < len(s) && s[i] == '[':
			shut := strings.IndexByte(s[i:], ']')
			if shut <= 1 {
				return "", "", 0, false
			}
			parts = append(parts, s[i+1:i+shut])
			i += shut + 1
		default:
			j := i
			for j < len(s) && isIdentByte(s[j]) {
				j++
			}
			// An identifier may not begin with a digit; `AddressOf 1` is not
			// an operand, and neither is a trailing dot with nothing after it.
			if j == i || (s[i] >= '0' && s[i] <= '9') {
				return "", "", 0, false
			}
			parts = append(parts, s[i:j])
			i = j
		}
		nameOff = segStart

		// Look ahead for another '.' segment.
		k := i
		for k < len(s) && (s[k] == ' ' || s[k] == '\t') {
			k++
		}
		if k >= len(s) || s[k] != '.' {
			break
		}
		i = k + 1
	}
	name = parts[len(parts)-1]
	qualifier = strings.Join(parts[:len(parts)-1], ".")
	return qualifier, name, nameOff, true
}
