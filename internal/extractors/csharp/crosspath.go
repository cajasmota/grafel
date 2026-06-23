// Cross-namespace CALLS qualifier reconciliation for C# (issue #4374).
//
// C# reaches a method in another namespace/type through a qualifier on a
// member_access_expression — a fully-qualified
// `App.Services.Orders.OrderService.Place()`, an aliased
// `using Ord = App.Services.Orders; Ord.OrderService.Place()`, a
// `using static App.Services.Orders.OrderService; Place()`, a same-namespace
// static `OrderService.Create()`, or a `global::App.Services...Place()`. The
// base extractor (csharpCallTarget / receiverTypeName) only types a single-
// level receiver, so a multi-segment qualified call collapses to the bare leaf
// method name (`Place`). The bare leaf resolves through the global byName index,
// which goes ambiguous the moment two namespaces define a same-named
// method/type (`OrderService.Place` in both App.Services.Orders and
// App.Services.Billing) — so the CALLS edge drops and the callee namespace
// looks falsely uncalled. This is the C# analogue of the Go cross-package
// (#4332) and Rust cross-module (#4373) qualifier drops.
//
// Unlike Go/Rust, C# namespaces are NOT directory-bound: a namespace may span
// files and directories, and a file's directory need not equal its namespace.
// So the resolver keys on the C# NAMESPACE (not the source directory). The
// extractor stamps the resolved (namespace, type, leaf) onto the CALLS edge;
// the resolver binds it through a namespace-keyed member index built in
// BuildIndex (ResolveCSharpCrossNamespaceCalls in internal/resolve/imports.go).
//
// Conservative by construction: only fires for a member-access invocation whose
// receiver chain is a static type-qualified path the file context can map to a
// concrete (namespace, type). Bare unqualified calls, instance-receiver calls,
// and chains we cannot statically resolve are left to the base extractor — no
// false stamps.
package csharp

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
)

// csharpCrossCtx is the per-file resolution context derived from the file's
// namespace declarations and using directives. It is built once per file and
// threaded through the call extractor so qualified calls can map a leading
// path qualifier to a concrete C# namespace.
type csharpCrossCtx struct {
	// fileNamespaces holds every namespace declared in the file (block and
	// file-scoped forms), e.g. "App.Services.Orders". A same-namespace
	// static call `OrderService.Create()` is resolved against these.
	fileNamespaces []string

	// usingNamespaces holds the namespaces brought into scope by a plain
	// `using App.Services.Orders;` — candidate namespaces a `Type.method()`
	// static call may bind into.
	usingNamespaces []string

	// aliasNamespaces maps a `using Ord = App.Services.Orders;` alias to its
	// target namespace path. A call `Ord.OrderService.Place()` rewrites the
	// alias head to the target before resolution.
	aliasNamespaces map[string]string

	// staticTypes maps the leaf type imported by `using static
	// App.Services.Orders.OrderService;` to its declaring namespace, so a
	// bare `Place()` (or `OrderService.Place()`) can recover the namespace.
	// leafType -> namespace (e.g. "OrderService" -> "App.Services.Orders").
	staticTypes map[string]string
}

// buildCrossCtx scans the compilation unit for namespace + using declarations
// and assembles the per-file C# cross-namespace context.
func buildCrossCtx(root ts.Node, src []byte) *csharpCrossCtx {
	if root == nil {
		return nil
	}
	ctx := &csharpCrossCtx{
		aliasNamespaces: map[string]string{},
		staticTypes:     map[string]string{},
	}
	// Namespaces declared in the file (block + file-scoped forms).
	for _, n := range findAllNodes(root,
		"namespace_declaration", "file_scoped_namespace_declaration") {
		if nf := n.ChildByFieldName("name"); nf != nil {
			if ns := strings.TrimSpace(string(src[nf.StartByte():nf.EndByte()])); ns != "" {
				ctx.fileNamespaces = appendUnique(ctx.fileNamespaces, ns)
			}
		}
	}
	// Using directives.
	for _, u := range findAllNodes(root, "using_directive") {
		raw, alias := extractUsingTargetWithAlias(u, src)
		if raw == "" {
			continue
		}
		full := strings.TrimSpace(string(src[u.StartByte():u.EndByte()]))
		isStatic := strings.HasPrefix(full, "using static") ||
			strings.HasPrefix(full, "global using static")
		switch {
		case alias != "":
			// `using Ord = App.Services.Orders;` — namespace (or type) alias.
			ctx.aliasNamespaces[alias] = raw
		case isStatic:
			// `using static App.Services.Orders.OrderService;` — the leaf is
			// the imported Type, the prefix is its namespace.
			if dot := strings.LastIndexByte(raw, '.'); dot > 0 {
				ctx.staticTypes[raw[dot+1:]] = raw[:dot]
			}
		default:
			// `using App.Services.Orders;` — namespace brought into scope.
			ctx.usingNamespaces = appendUnique(ctx.usingNamespaces, raw)
		}
	}
	return ctx
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// qualifiedCallBinding describes the (namespace, type, leaf) a qualified C#
// call resolves to. ns may be "" when the call is `Type.method()` and the
// namespace must be recovered from the using/file context by the resolver via
// candidate namespaces (carried in nsCandidates).
type qualifiedCallBinding struct {
	leaf         string   // method name
	typ          string   // declaring type name
	nsCandidates []string // candidate namespaces (most-specific first)
}

// resolveQualifiedCall inspects a member_access_expression invocation function
// node and, when the receiver chain is a static type-qualified path, returns
// the (namespace candidates, type, leaf) binding. Returns nil for shapes that
// are not a statically-resolvable cross-namespace call (instance receivers,
// unqualified bare calls, unresolvable chains) so the base extractor's target
// is used unchanged.
func (c *csharpCrossCtx) resolveQualifiedCall(
	fn ts.Node,
	src []byte,
	cc *classCtx,
	locals map[string]string,
) *qualifiedCallBinding {
	if c == nil || fn == nil || fn.Type() != "member_access_expression" {
		return nil
	}
	nameNode := fn.ChildByFieldName("name")
	expr := fn.ChildByFieldName("expression")
	if nameNode == nil || expr == nil {
		return nil
	}
	leaf := string(src[nameNode.StartByte():nameNode.EndByte()])
	if leaf == "" {
		return nil
	}
	// Flatten the receiver chain to its dotted segments. A non-static chain
	// (one containing an instance receiver we can't type) yields no usable
	// path, so we only flatten pure identifier/qualified chains.
	segs, ok := flattenStaticPath(expr, src)
	if !ok || len(segs) == 0 {
		return nil
	}
	// Instance-receiver guard: when the head segment is a known field,
	// property, parameter, or local variable, this is an instance call
	// (`_h.Do()`, `order.Place()`) the base extractor already types via the
	// receiver's declared type — NOT a cross-namespace static qualifier. Do
	// not stamp; no false cross-namespace binding. `global`-prefixed chains
	// can never be instance receivers, so they bypass this guard.
	if head := segs[0]; head != "global" {
		if cc != nil {
			if _, ok := cc.fields[head]; ok {
				return nil
			}
		}
		if _, ok := locals[head]; ok {
			return nil
		}
	}
	// Drop a leading `global` alias-qualifier segment (`global::App...`).
	if len(segs) > 1 && segs[0] == "global" {
		segs = segs[1:]
	}
	if len(segs) == 0 {
		return nil
	}
	// Rewrite a leading namespace alias (`using Ord = App.Services.Orders;`).
	if target, isAlias := c.aliasNamespaces[segs[0]]; isAlias {
		expanded := strings.Split(target, ".")
		segs = append(append([]string{}, expanded...), segs[1:]...)
	}
	// The rightmost segment of the receiver chain is the declaring Type; the
	// segments before it form the namespace path. A lone Type segment
	// (`OrderService.Place()`) leaves an empty namespace path → recover via
	// the file/using/static context as candidates.
	typ := segs[len(segs)-1]
	if typ == "" {
		return nil
	}
	nsPath := strings.Join(segs[:len(segs)-1], ".")

	b := &qualifiedCallBinding{leaf: leaf, typ: typ}
	if nsPath != "" {
		// Fully-qualified namespace path present — that is the namespace.
		b.nsCandidates = []string{nsPath}
		return b
	}
	// `Type.method()` with no namespace path: candidate namespaces are the
	// type's `using static` namespace, the file's own namespaces, and the
	// plain `using` namespaces (most-specific first).
	if ns, ok := c.staticTypes[typ]; ok && ns != "" {
		b.nsCandidates = appendUnique(b.nsCandidates, ns)
	}
	for _, ns := range c.fileNamespaces {
		b.nsCandidates = appendUnique(b.nsCandidates, ns)
	}
	for _, ns := range c.usingNamespaces {
		b.nsCandidates = appendUnique(b.nsCandidates, ns)
	}
	if len(b.nsCandidates) == 0 {
		return nil
	}
	return b
}

// flattenStaticPath flattens a receiver expression composed solely of
// identifiers, qualified_name, member_access_expression, and a single leading
// alias_qualified_name (`global::App`) into its dotted segments. Returns
// (nil, false) the moment a non-static node (e.g. `this`, an invocation, an
// element access) appears — those are instance chains the base extractor owns.
func flattenStaticPath(n ts.Node, src []byte) ([]string, bool) {
	if n == nil {
		return nil, false
	}
	switch n.Type() {
	case "identifier":
		return []string{string(src[n.StartByte():n.EndByte()])}, true
	case "qualified_name":
		// qualified_name is (left qualified_name|identifier) '.' (right identifier).
		left := n.ChildByFieldName("qualifier")
		right := n.ChildByFieldName("name")
		if left == nil || right == nil {
			// Field names vary by grammar build; fall back to named children.
			if int(n.NamedChildCount()) == 2 {
				left = n.NamedChild(0)
				right = n.NamedChild(1)
			}
		}
		ls, ok := flattenStaticPath(left, src)
		if !ok {
			return nil, false
		}
		rs, ok := flattenStaticPath(right, src)
		if !ok {
			return nil, false
		}
		return append(ls, rs...), true
	case "member_access_expression":
		nameNode := n.ChildByFieldName("name")
		expr := n.ChildByFieldName("expression")
		if nameNode == nil || expr == nil {
			return nil, false
		}
		ls, ok := flattenStaticPath(expr, src)
		if !ok {
			return nil, false
		}
		return append(ls, string(src[nameNode.StartByte():nameNode.EndByte()])), true
	case "alias_qualified_name":
		// `global::App` — two identifier children (alias, name).
		if int(n.NamedChildCount()) == 2 {
			a := n.NamedChild(0)
			b := n.NamedChild(1)
			return []string{
				string(src[a.StartByte():a.EndByte()]),
				string(src[b.StartByte():b.EndByte()]),
			}, true
		}
		return nil, false
	}
	return nil, false
}
