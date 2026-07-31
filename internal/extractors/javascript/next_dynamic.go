package javascript

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"
)

// next/dynamic recognition (issue #6054).
//
// `next/dynamic` is Next.js's code-splitting primitive and the direct analogue
// of React.lazy(): it takes a loader callback that performs a dynamic import()
// and returns a component. The bound name is therefore a function, exactly like
// the React.lazy shape already handled by isLazyWrapper.
//
// The recognition rule differs from every other entry in isFunctionWrapperCall's
// name switch. Those match on the callee leaf name alone, which is acceptable
// for names like `unstable_cache` or `forwardRef` that are effectively owned by
// one library. `dynamic` is not such a name — it is a plausible ordinary
// identifier for a user helper or a local binding — so matching it by name
// would misclassify unrelated code. Instead the recognition is gated on the
// argument shape that makes the call a code-split point at all:
//
//	first argument is a function literal whose body performs a dynamic import()
//
// Everything that is not that shape is left alone, including `dynamic(x)`,
// `dynamic({loader: ...})` and `dynamic()`. The gate is deliberately narrower
// than Next.js's full accepted surface; see nextDynamicModule for what that
// costs.
//
// Recognised (Next.js documented forms):
//
//	dynamic(() => import('./Chart'), { ssr: false })
//	dynamic(async () => (await import('./Chart')).Chart)
//	dynamic(() => import(`./panels/${name}`))     → specifier normalised to {*}
//	dynamic(() => import(getPath()))              → recognised, no lazy_module
//
// Deliberately NOT recognised:
//
//	dynamic(SomeLoaderRef)          — no import() in an inspectable subtree
//	dynamic({ loader: () => … })    — the legacy object form; first argument is
//	                                  not a function, and accepting it would
//	                                  mean searching argument positions that a
//	                                  non-Next `dynamic(opts)` call also has
//	import dyn from 'next/dynamic'; dyn(() => import('x'))
//	                                — renamed default import. The wrapper switch
//	                                  has no import tracking (see the comment on
//	                                  isFunctionWrapperCall), so a renamed
//	                                  binding is invisible to it. Consistent
//	                                  with how `lazy`, `memo` etc. already
//	                                  behave.

// isNextDynamicWrapper reports whether n is a `dynamic(loader, …)` call whose
// first argument is a function literal performing a dynamic import() — i.e. a
// next/dynamic code-split point. The name check is necessary but not
// sufficient; the argument shape is what carries the recognition.
func (x *extractor) isNextDynamicWrapper(n ts.Node) bool {
	return x.nextDynamicLoader(n) != nil
}

// nextDynamicLoader returns the loader-callback node of a next/dynamic call, or
// nil when n is not one. Single source of truth for the gate so that
// isNextDynamicWrapper and nextDynamicModule cannot drift apart.
func (x *extractor) nextDynamicLoader(n ts.Node) ts.Node {
	if n == nil || n.Type() != "call_expression" {
		return nil
	}
	if x.calleeLeaf(n) != "dynamic" {
		return nil
	}
	arg := firstCallArg(n)
	if arg == nil {
		return nil
	}
	switch arg.Type() {
	case "arrow_function", "function_expression", "function":
	default:
		return nil
	}
	if !hasDynamicImportCall(arg) {
		return nil
	}
	return arg
}

// nextDynamicModule returns the import specifier of a next/dynamic split point,
// using the same recovery rules as React.lazy (string literal, pure template,
// interpolated template → {*}, computed → ""). Returns "" when n is not a
// next/dynamic wrapper or the specifier is not statically recoverable.
func (x *extractor) nextDynamicModule(n ts.Node) string {
	loader := x.nextDynamicLoader(n)
	if loader == nil {
		return ""
	}
	// Search only the loader callback, never the options object: a stray
	// import() in `{ loading: () => import('spinner') }` is not the split
	// target and must not be reported as one.
	return x.dynamicImportSpecifier(loader)
}

// firstCallArg returns the first named argument node of a call_expression, or
// nil when the call has no arguments.
func firstCallArg(n ts.Node) ts.Node {
	args := n.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	for i := 0; i < int(args.ChildCount()); i++ {
		c := args.Child(i)
		if c != nil && c.IsNamed() {
			return c
		}
	}
	return nil
}

// hasDynamicImportCall reports whether the subtree rooted at n contains a
// dynamic `import(...)` call. Tree-sitter models `import('m')` as a
// call_expression whose function child is the `import` keyword node.
//
// This is the presence check that dynamicImportSpecifier cannot serve: a
// computed specifier (`import(getPath())`) is still a code-split point even
// though the specifier is unrecoverable, the same distinction isLazyWrapper vs
// lazyImportModule draws for React.lazy (issue #2958).
func hasDynamicImportCall(n ts.Node) bool {
	for _, c := range findAllNodes(n, "call_expression") {
		if fn := c.ChildByFieldName("function"); fn != nil && fn.Type() == "import" {
			return true
		}
	}
	return false
}
