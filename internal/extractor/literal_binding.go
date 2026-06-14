// literal_binding.go — a reusable per-scope literal-binding resolver for
// indirect-call-through-variable patterns (issue #5158).
//
// Background
// ----------
// A recurring cross-language shape is a call whose *target* is a variable that
// was assigned a string literal earlier in the same scope:
//
//	COBOL:   MOVE 'TAXCALC' TO WS-PROG.   CALL WS-PROG.
//	Python:  name = "handle_order";       getattr(self, name)()
//	JS/TS:   const m = "process";          obj[m]()
//	shell:   cmd=do_work;                  $cmd
//
// In each case the raw extractor leaves a CALLS/USES edge pointing at the bare
// variable name. To recover the real target we trace the literal assigned to
// that variable, last-write-wins, within the variable's scope.
//
// #5040 implemented this COBOL-scoped (paragraph-scoped MOVE-literal tracking).
// This helper extracts the *binding-table* mechanics so any extractor can reuse
// the same semantics:
//
//   - Bind:   record name → literal (last-write-wins).
//   - Taint:  a reassignment from a NON-literal expression clears the binding,
//             so we never resolve to a stale literal.
//   - Scope:  Reset() drops all bindings at a scope boundary (new function,
//             paragraph, block — the caller decides the granularity).
//   - Resolve: look up the literal currently bound to a name; ("", false) when
//             unbound or tainted.
//
// The resolver is intentionally syntax-agnostic: callers feed it (name, value)
// pairs they have already parsed from their own grammar (tree-sitter node,
// regex, COBOL fixed-form line, …). It owns only the binding semantics, which
// are identical everywhere. Callers stamp the recovered edge with
// `resolved_via=literal-binding` and `dynamic_target=<original var>` so the
// graph records both that the edge was recovered and what the source variable
// was.
//
// Not safe across control flow: like the COBOL implementation, this is a
// best-effort straight-line / last-write-wins trace. Conditional or
// loop-carried reassignment is approximated by taint (any non-literal write
// clears the binding); branch-sensitive resolution is out of scope.
package extractor

// ResolvedViaLiteralBinding is the canonical Properties value stamped on a
// CALLS/USES edge whose target was recovered through a literal binding. Wired
// extractors set Properties["resolved_via"] = ResolvedViaLiteralBinding and
// Properties["dynamic_target"] = <original variable name>.
const ResolvedViaLiteralBinding = "literal-binding"

// LiteralBindingResolver tracks `<name> = <string-literal>` bindings within a
// single scope so an indirect call/use on `<name>` can be resolved to the
// literal value. Last-write-wins; a non-literal reassignment taints (clears)
// the binding. The zero value is NOT ready — use NewLiteralBindingResolver.
//
// Not safe for concurrent use; an extractor processes one file/scope serially.
type LiteralBindingResolver struct {
	// bindings maps a (caller-normalized) variable name to the string literal
	// most recently assigned to it. Absence means unbound or tainted.
	bindings map[string]string
	// keyFn normalizes a variable name before it is used as a map key, so
	// case-insensitive languages (COBOL) can fold case while case-sensitive
	// ones (Python/JS/shell) pass through. nil ⇒ identity.
	keyFn func(string) string
}

// NewLiteralBindingResolver returns an empty resolver. keyFn normalizes names
// to map keys (pass nil for identity / case-sensitive languages; pass
// strings.ToUpper for COBOL-style case-insensitive names).
func NewLiteralBindingResolver(keyFn func(string) string) *LiteralBindingResolver {
	return &LiteralBindingResolver{
		bindings: map[string]string{},
		keyFn:    keyFn,
	}
}

func (r *LiteralBindingResolver) key(name string) string {
	if r.keyFn == nil {
		return name
	}
	return r.keyFn(name)
}

// Bind records that `name` was assigned the string literal `lit`
// (last-write-wins). An empty name is ignored. A subsequent Bind for the same
// name overwrites the previous literal.
func (r *LiteralBindingResolver) Bind(name, lit string) {
	if name == "" {
		return
	}
	r.bindings[r.key(name)] = lit
}

// Taint clears any literal binding for `name`, modelling a reassignment from a
// non-literal expression (`x = foo()`, `MOVE WS-OTHER TO X`). After Taint a
// Resolve for `name` returns ("", false) until the next Bind. Tainting an
// already-unbound name is a no-op.
func (r *LiteralBindingResolver) Taint(name string) {
	if name == "" {
		return
	}
	delete(r.bindings, r.key(name))
}

// Resolve returns the string literal currently bound to `name`, or ("", false)
// when `name` is unbound or has been tainted.
func (r *LiteralBindingResolver) Resolve(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	lit, ok := r.bindings[r.key(name)]
	return lit, ok
}

// Reset drops every binding, modelling a scope boundary (new function /
// paragraph / block). Callers invoke this when they cross into a scope that
// should not inherit the previous scope's literal bindings.
func (r *LiteralBindingResolver) Reset() {
	r.bindings = map[string]string{}
}

// Len reports the number of live bindings. Primarily for tests.
func (r *LiteralBindingResolver) Len() int { return len(r.bindings) }
