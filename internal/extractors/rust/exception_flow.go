// exception_flow.go — supplemental pass that emits THROWS / CATCHES edges from
// Rust functions and methods to a shared SCOPE.ExceptionType node (epic #3628).
// It lets the graph answer "what error types can this function raise?" (outbound
// THROWS) and "where is NotFoundError handled?" (inbound CATCHES), keeping
// error-contract parity cross-language with the Java / Python / Go / JS / C# /
// C++ / PHP / Ruby / Scala / Kotlin / Elixir flagships. Node/edge construction
// is delegated to extractor.EmitExceptionEdges so the convergence invariant
// (identical type names → ONE node) is identical everywhere.
//
// HONEST-FIT NOTE (Rust has no exceptions): Rust models errors with
// Result<T, E> / panic, not throw/catch. This pass credits only the constructs
// that carry a STATIC error TYPE, which genuinely correspond to the flagship
// THROWS/CATCHES model. Bare `?` propagation and `Box<dyn Error>` carry no
// specific static type and are honestly LEFT OUT (see "Deliberately NOT
// recorded" below). The coverage is therefore an honest PARTIAL of the Rust
// error surface, but every edge it does emit asserts a real, specific type.
//
// Detected THROWS shapes (a function producing/raising a specific error TYPE):
//
//	return Err(NotFoundError::new())  → THROWS NotFoundError  (Err(Type::ctor()))
//	Err(IoError::from(e))             → THROWS IoError        (tail-expression Err)
//	Err(MyError::NotFound)            → THROWS MyError         (enum variant → ENUM type)
//	bail!(MyError::Boom)              → THROWS MyError         (anyhow/eyre bail! macro)
//	ensure!(c, MyError::Bad)          → THROWS MyError         (anyhow/eyre ensure! macro)
//	x.ok_or(MyError::Missing)         → THROWS MyError         (Option→Result with a type)
//	x.ok_or_else(|| MyError::Gone)    → THROWS MyError         (lazy variant)
//
// Detected CATCHES shapes (a handler matching a specific error TYPE):
//
//	match r { Err(NotFoundError) => .. }   → CATCHES NotFoundError (typed match arm)
//	match r { Err(MyError::Bad) => .. }    → CATCHES MyError        (enum variant → ENUM type)
//	if let Err(ParseError) = parse() {}    → CATCHES ParseError     (typed if-let)
//	r.map_err(|e: IoError| wrap(e))        → CATCHES IoError         (typed closure param)
//
// ENUM-VARIANT NORMALIZATION CONVENTION: a scoped error path in error position
// is normalized to its FIRST path segment — the carrying TYPE — not its last.
// So `MyError::NotFound` (enum variant) → `MyError` (the enum) and
// `NotFoundError::new()` (associated ctor) → `NotFoundError` (the struct). This
// is deliberately the OPPOSITE of extractor.NormalizeExceptionType's
// last-segment rule (which is right for `std::io::Error`-style module paths in
// other languages): in Rust the leading segment of an error path is the static
// type and the trailing segment is the variant/associated-fn, which carries no
// type identity of its own. rustErrorTypeToken applies this first-segment rule,
// then hands the bare identifier to extractor.NormalizeExceptionType (which
// leaves a bare identifier unchanged) so all the shared dynamic-token guards
// still apply. A `NotFoundError` raised via Err(..) in one file and caught in a
// match arm in another therefore converge on ONE `exception:NotFoundError` node.
//
// Deliberately NOT recorded (honest-missing — would carry no specific type):
//
//	thing?                 bare `?` propagation — re-raises whatever Err flows
//	                       through; no static type at this site.
//	Err(Box<dyn Error>)    boxed trait object — `<`/spaces token is rejected by
//	Box::new(e)            extractor.NormalizeExceptionType (dynamic type).
//	panic!("msg")          string-payload panic — a message, not a type.
//	Ok(x) / Some(x)        success values — no error.
//	Err(e) / Err(make())   re-raise of a variable / dynamic constructor — the
//	                       inner operand is a bare identifier or non-type call,
//	                       so rustErrorTypeToken yields "" (no NEW static type).
//
// FromName is the host operation Name. For free functions it is the bare leaf
// (e.g. `find`); for impl methods it is the impl-qualified `Type.method` form
// the rest of the extractor emits (rust.go qualifies impl methods as
// `implName + "." + fnName`), so THROWS / CATCHES edges attach to the same
// SCOPE.Operation host. Throws/catches outside any function fall back to the
// file entity via extractor.EmitExceptionEdges.

package rust

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitExceptionFlowEdges scans every function/method body for typed error
// constructs and appends exception-type entities + THROWS / CATCHES edges to
// *entities.
//
// entities[0] MUST be the file entity. Mutates *entities in place. Safe with
// nil / empty input.
func emitExceptionFlowEdges(root ts.Node, src []byte, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}

	var edges []extractor.ExceptionEdge

	// walk tracks the enclosing operation Name so each edge attaches to the
	// right host. enclosingFn mirrors rust.go's operation naming:
	//   - free fn          → bare leaf ("find")
	//   - impl Type { fn } → impl-qualified ("Type.find")
	// implType is the current impl's implementing-type name ("" when not in an
	// impl), so a nested function_item inside an impl gets "Type.fn".
	var walk func(n ts.Node, enclosingFn, implType string)
	walk = func(n ts.Node, enclosingFn, implType string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "impl_item":
			implType = rustImplTypeName(n, src)
		case "function_item":
			if name := childIdentText(n, src); name != "" {
				if implType != "" {
					enclosingFn = implType + "." + name
				} else {
					enclosingFn = name
				}
			}
		case "call_expression":
			// Err(Type::ctor()) / Err(Type::Variant) / Err(Type(..)) and
			// receiver.ok_or(Type::Variant) / receiver.ok_or_else(|| Type::Variant).
			if t := rustThrowFromCall(n, src); t != "" {
				edges = append(edges, extractor.ExceptionEdge{
					Type: t, FromName: enclosingFn, Pattern: "err_value",
				})
			}
			if t := rustThrowFromOkOr(n, src); t != "" {
				edges = append(edges, extractor.ExceptionEdge{
					Type: t, FromName: enclosingFn, Pattern: "ok_or",
				})
			}
			// map_err(|e: T| ..) closure with a typed parameter → CATCHES T.
			if t := rustCatchFromMapErr(n, src); t != "" {
				edges = append(edges, extractor.ExceptionEdge{
					Type: t, FromName: enclosingFn, Catch: true, Pattern: "map_err",
				})
			}
		case "macro_invocation":
			// bail!(Type::Variant) / ensure!(cond, Type::Variant) (anyhow/eyre).
			if t := rustThrowFromMacro(n, src); t != "" {
				edges = append(edges, extractor.ExceptionEdge{
					Type: t, FromName: enclosingFn, Pattern: "bail_macro",
				})
			}
		case "match_arm":
			// match r { Err(Type) => .. } → CATCHES Type.
			if t := rustCatchFromErrPattern(matchArmPattern(n), src); t != "" {
				edges = append(edges, extractor.ExceptionEdge{
					Type: t, FromName: enclosingFn, Catch: true, Pattern: "match_arm",
				})
			}
		case "let_condition":
			// if let Err(Type) = .. → CATCHES Type.
			if t := rustCatchFromErrPattern(letConditionPattern(n), src); t != "" {
				edges = append(edges, extractor.ExceptionEdge{
					Type: t, FromName: enclosingFn, Catch: true, Pattern: "if_let",
				})
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), enclosingFn, implType)
		}
	}
	walk(root, "", "")

	extractor.EmitExceptionEdges(entities, "rust", edges)
}

// rustImplTypeName returns the implementing-type name of an impl_item, mirroring
// buildImpl's resolution (the "type" field, with a type_identifier/generic_type
// fallback). Returns "" if it cannot be resolved.
func rustImplTypeName(n ts.Node, src []byte) string {
	if ty := n.ChildByFieldName("type"); ty != nil {
		return nodeStr(ty, src)
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		if t := ch.Type(); t == "type_identifier" || t == "generic_type" {
			return nodeStr(ch, src)
		}
	}
	return ""
}

// childIdentText returns the text of a node's "name" field, or "" if absent.
func childIdentText(n ts.Node, src []byte) string {
	if name := n.ChildByFieldName("name"); name != nil {
		return nodeStr(name, src)
	}
	return ""
}

// rustThrowFromCall recognises an `Err(<operand>)` call_expression and returns
// the static error TYPE carried by the operand, or "" when the operand carries
// no NEW static type (a re-raised variable, a dynamic constructor, a boxed trait
// object, etc.). Only the `Err` constructor is matched — `Ok(..)` / `Some(..)`
// are success values and produce nothing.
//
// Operand shapes that carry a type:
//
//	Err(NotFoundError::new())  call_expression → fn scoped_identifier → TYPE = NotFoundError
//	Err(IoError::from(e))      call_expression → fn scoped_identifier → TYPE = IoError
//	Err(MyError::NotFound)     scoped_identifier (enum variant)        → TYPE = MyError
//	Err(NotFoundError(..))     call_expression → fn identifier (tuple-struct ctor) → TYPE = NotFoundError
//
// Operand shapes that carry NO new type (→ ""):
//
//	Err(e)                 identifier — re-raise of a variable
//	Err(make_error())      call whose fn is a bare identifier that is NOT a type
//	                       constructor pattern we can trust → conservatively only
//	                       Type::assoc()/Type(..) forms are credited.
//	Err(Box::new(e))       scoped ctor whose leading segment `Box` would be
//	                       credited; this is the boxed-trait-object false-positive
//	                       guarded against by rejecting the `Box` leading segment.
func rustThrowFromCall(call ts.Node, src []byte) string {
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "identifier" || nodeStr(fn, src) != "Err" {
		return ""
	}
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	operand := firstNamedChild(args)
	if operand == nil {
		return ""
	}
	switch operand.Type() {
	case "scoped_identifier":
		// Enum variant `MyError::NotFound` → ENUM type (first segment).
		return rustErrorTypeToken(leadingScopedSegment(operand, src))
	case "call_expression":
		cfn := operand.ChildByFieldName("function")
		if cfn == nil {
			return ""
		}
		switch cfn.Type() {
		case "scoped_identifier":
			// `Type::ctor(..)` / `Type::from(..)` → the receiver TYPE.
			return rustErrorTypeToken(leadingScopedSegment(cfn, src))
		case "identifier":
			// `Type(..)` tuple-struct ctor → that identifier as a TYPE iff it
			// looks like a type (UpperCamel). A lowercase `make_error()` is a
			// factory call carrying no static type → dropped by the heuristic.
			id := nodeStr(cfn, src)
			if looksLikeTypeName(id) {
				return rustErrorTypeToken(id)
			}
		}
	}
	return ""
}

// rustThrowFromOkOr recognises `recv.ok_or(<type-arg>)` /
// `recv.ok_or_else(|| <type-expr>)` — the Option→Result conversions that inject
// a specific error type into a Result — and returns that error TYPE, or "".
func rustThrowFromOkOr(call ts.Node, src []byte) string {
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "field_expression" {
		return ""
	}
	field := fn.ChildByFieldName("field")
	if field == nil {
		return ""
	}
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	switch nodeStr(field, src) {
	case "ok_or":
		// First argument is the error value expression.
		return rustErrorExprType(firstNamedChild(args), src)
	case "ok_or_else":
		// Argument is a closure whose body yields the error value.
		cl := firstNamedChild(args)
		if cl == nil || cl.Type() != "closure_expression" {
			return ""
		}
		return rustErrorExprType(closureBody(cl), src)
	}
	return ""
}

// rustThrowFromMacro recognises `bail!(Type::Variant)` and
// `ensure!(cond, Type::Variant)` (anyhow / eyre) and returns the error TYPE
// carried by the LAST token-tree argument, or "" when none carries a type.
//
// The macro token_tree is an unparsed token stream; we scan it for the LAST
// `Ident :: Ident` run (the error expression) and credit its leading segment.
// `bail!("plain message")` (string only) yields "".
func rustThrowFromMacro(mac ts.Node, src []byte) string {
	name := mac.ChildByFieldName("macro")
	if name == nil {
		return ""
	}
	switch nodeStr(name, src) {
	case "bail", "ensure":
	default:
		return ""
	}
	tt := childOfType(mac, "token_tree")
	if tt == nil {
		return ""
	}
	// Find the last `identifier :: identifier` pair in the token stream and
	// credit the leading identifier as the carrying type.
	var lastType string
	for i := 0; i+2 < int(tt.ChildCount()); i++ {
		a, op, b := tt.Child(i), tt.Child(i+1), tt.Child(i+2)
		if a.Type() == "identifier" && op.Type() == "::" && b.Type() == "identifier" {
			lastType = nodeStr(a, src)
		}
	}
	return rustErrorTypeToken(lastType)
}

// rustCatchFromMapErr recognises `recv.map_err(|e: T| ..)` and returns the typed
// closure-parameter type T (the error being handled), or "".
func rustCatchFromMapErr(call ts.Node, src []byte) string {
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "field_expression" {
		return ""
	}
	field := fn.ChildByFieldName("field")
	if field == nil || nodeStr(field, src) != "map_err" {
		return ""
	}
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	cl := firstNamedChild(args)
	if cl == nil || cl.Type() != "closure_expression" {
		return ""
	}
	params := childOfType(cl, "closure_parameters")
	if params == nil {
		return ""
	}
	// |e: T| → a `parameter` node with a "type" field; |e| → bare identifier,
	// no type, no edge.
	for i := 0; i < int(params.NamedChildCount()); i++ {
		p := params.NamedChild(i)
		if p.Type() != "parameter" {
			continue
		}
		if ty := p.ChildByFieldName("type"); ty != nil {
			return rustErrorTypeToken(nodeStr(ty, src))
		}
	}
	return ""
}

// rustCatchFromErrPattern returns the caught error TYPE from an `Err(<pat>)`
// pattern node (a tuple_struct_pattern), or "". Used for both match arms and
// `if let` conditions.
//
//	Err(NotFoundError)     inner identifier         → NotFoundError
//	Err(MyError::Bad)      inner scoped_identifier  → MyError (enum → ENUM type)
//	Err(_) / Err(e)        wildcard / binding       → "" (no type, no edge)
func rustCatchFromErrPattern(pat ts.Node, src []byte) string {
	if pat == nil || pat.Type() != "tuple_struct_pattern" {
		return ""
	}
	// The constructor of the tuple-struct pattern must be `Err`.
	ctor := firstNamedChild(pat)
	if ctor == nil || ctor.Type() != "identifier" || nodeStr(ctor, src) != "Err" {
		return ""
	}
	// The inner element after the `Err` ctor is the matched payload pattern.
	var inner ts.Node
	seenCtor := false
	for i := 0; i < int(pat.NamedChildCount()); i++ {
		c := pat.NamedChild(i)
		if !seenCtor {
			seenCtor = true // first named child is the `Err` ctor identifier
			continue
		}
		inner = c
		break
	}
	if inner == nil {
		return ""
	}
	switch inner.Type() {
	case "identifier":
		id := nodeStr(inner, src)
		// A lowercase binding (`Err(e)`) carries no type; only a type-shaped
		// identifier (`Err(NotFoundError)`) is a unit-struct/variant pattern.
		if looksLikeTypeName(id) {
			return rustErrorTypeToken(id)
		}
	case "scoped_identifier":
		// `Err(MyError::Bad)` enum variant → ENUM type (leading segment).
		return rustErrorTypeToken(leadingScopedSegment(inner, src))
	case "tuple_struct_pattern":
		// `Err(MyError::Bad(x))` variant with payload → constructor's leading
		// segment is the enum type.
		if c := firstNamedChild(inner); c != nil && c.Type() == "scoped_identifier" {
			return rustErrorTypeToken(leadingScopedSegment(c, src))
		}
	}
	return ""
}

// rustErrorExprType returns the error TYPE carried by an error-value expression
// (the argument of ok_or / the body of ok_or_else's closure), or "".
//
//	MyError::Variant       scoped_identifier  → MyError
//	MyError::new()         call → scoped fn    → MyError
//	MyError                identifier          → MyError (if type-shaped)
func rustErrorExprType(expr ts.Node, src []byte) string {
	if expr == nil {
		return ""
	}
	switch expr.Type() {
	case "scoped_identifier":
		return rustErrorTypeToken(leadingScopedSegment(expr, src))
	case "identifier":
		id := nodeStr(expr, src)
		if looksLikeTypeName(id) {
			return rustErrorTypeToken(id)
		}
	case "call_expression":
		cfn := expr.ChildByFieldName("function")
		if cfn != nil && cfn.Type() == "scoped_identifier" {
			return rustErrorTypeToken(leadingScopedSegment(cfn, src))
		}
		if cfn != nil && cfn.Type() == "identifier" {
			id := nodeStr(cfn, src)
			if looksLikeTypeName(id) {
				return rustErrorTypeToken(id)
			}
		}
	}
	return ""
}

// rustErrorTypeToken applies the Rust-specific first-segment convention and then
// the shared dynamic-token guard. The input is already expected to be the bare
// leading identifier (callers extract it via leadingScopedSegment); this final
// pass through extractor.NormalizeExceptionType rejects any token that slipped
// through carrying generic/dynamic punctuation (e.g. `Box<dyn Error>`), keeping
// every emitted node a clean static type. Returns "" for the boxed-trait-object
// escape hatch types (`Box`) which are not real error types.
func rustErrorTypeToken(raw string) string {
	t := strings.TrimSpace(raw)
	if t == "" {
		return ""
	}
	// Boxed trait objects carry no specific static type — honest-missing.
	if t == "Box" {
		return ""
	}
	return extractor.NormalizeExceptionType(t)
}

// leadingScopedSegment returns the FIRST identifier segment of a
// scoped_identifier (`MyError::NotFound` → `MyError`, `a::b::C` → `a`). For Rust
// error paths the leading segment is the carrying TYPE. Returns "" if the node
// has no leading identifier.
func leadingScopedSegment(n ts.Node, src []byte) string {
	for i := 0; i < int(n.ChildCount()); i++ {
		c := n.Child(i)
		switch c.Type() {
		case "identifier", "type_identifier":
			return nodeStr(c, src)
		case "scoped_identifier":
			// Nested path `a::b::C` — recurse into the left to reach the head.
			if s := leadingScopedSegment(c, src); s != "" {
				return s
			}
		}
	}
	return ""
}

// looksLikeTypeName reports whether an identifier is shaped like a Rust type /
// variant (starts with an uppercase ASCII letter). Distinguishes a unit-struct
// or variant pattern (`NotFoundError`) and a tuple-struct constructor (`Type`)
// from a lowercase value binding (`e`) or a snake_case factory call
// (`make_error`) which carry no static type.
func looksLikeTypeName(id string) bool {
	if id == "" {
		return false
	}
	c := id[0]
	return c >= 'A' && c <= 'Z'
}

// --- small CST helpers (kept local so the pass is self-contained) ---

func nodeStr(n ts.Node, src []byte) string {
	if n == nil {
		return ""
	}
	return string(src[n.StartByte():n.EndByte()])
}

func firstNamedChild(n ts.Node) ts.Node {
	if n == nil {
		return nil
	}
	if n.NamedChildCount() == 0 {
		return nil
	}
	return n.NamedChild(0)
}

func childOfType(n ts.Node, typ string) ts.Node {
	if n == nil {
		return nil
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		if c := n.Child(i); c.Type() == typ {
			return c
		}
	}
	return nil
}

// closureBody returns the body expression of a closure_expression (the child
// after the closure_parameters), or nil.
func closureBody(cl ts.Node) ts.Node {
	if cl == nil {
		return nil
	}
	seenParams := false
	for i := 0; i < int(cl.NamedChildCount()); i++ {
		c := cl.NamedChild(i)
		if c.Type() == "closure_parameters" {
			seenParams = true
			continue
		}
		if seenParams {
			return c
		}
	}
	// Closure with no explicit parameters list (`|| Expr`) — first named child
	// after the `||` is the body; NamedChild(0) is the body in that case.
	return firstNamedChild(cl)
}

// matchArmPattern returns the tuple_struct_pattern inside a match_arm's
// match_pattern, or nil. A match_arm is `match_pattern => body`; the pattern
// wraps the actual pattern (here we want `Err(..)`).
func matchArmPattern(arm ts.Node) ts.Node {
	mp := childOfType(arm, "match_pattern")
	if mp == nil {
		return nil
	}
	return childOfType(mp, "tuple_struct_pattern")
}

// letConditionPattern returns the tuple_struct_pattern inside a let_condition
// (`let Err(..) = expr`), or nil.
func letConditionPattern(lc ts.Node) ts.Node {
	return childOfType(lc, "tuple_struct_pattern")
}
