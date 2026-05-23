package resolve

import "regexp"

// reasonmlDynamicPatterns are per-language patterns for ReasonML.
// Registered via init() into dynamicPatternsByLang.
//
// The ReasonML extractor (internal/extractors/reasonml/extractor.go) emits CALLS
// edges whose ToID is either:
//   - a bare function name: "helper"
//   - a module-qualified call: "Belt.List.map", "Belt.Option.getWithDefault"
//   - a pipe-chain target: "Belt.Array.reduce", "ReactDOMRe.renderToElementWithId"
//
// Three categories of patterns:
//
//  1. Belt stdlib — BuckleScript's Belt standard library, ReasonML's primary stdlib.
//     Module-qualified identifiers unique to Belt.
//
//  2. ReactDOMRe — ReasonReact DOM bindings (renderToElement, renderToElementWithId).
//
//  3. React / ReasonReact — React bindings commonly used in ReasonML components.
//
//  4. Pipe operator targets — captured via |> in function pipelines.
//
// All patterns are gated to lang=="reasonml" (safer-bias rule).
var reasonmlDynamicPatterns = []*regexp.Regexp{
	// ── Belt.List module ─────────────────────────────────────────────────────
	regexp.MustCompile(`^Belt\.List\.map$`),              // Belt.List.map(lst, f)
	regexp.MustCompile(`^Belt\.List\.filter$`),           // Belt.List.filter(lst, pred)
	regexp.MustCompile(`^Belt\.List\.keep$`),             // Belt.List.keep(lst, pred) — alias for filter
	regexp.MustCompile(`^Belt\.List\.reduce$`),           // Belt.List.reduce(lst, init, f)
	regexp.MustCompile(`^Belt\.List\.forEach$`),          // Belt.List.forEach(lst, f)
	regexp.MustCompile(`^Belt\.List\.forEachWithIndex$`), // Belt.List.forEachWithIndex(lst, f)
	regexp.MustCompile(`^Belt\.List\.mapWithIndex$`),     // Belt.List.mapWithIndex(lst, f)
	regexp.MustCompile(`^Belt\.List\.filterMap$`),        // Belt.List.filterMap(lst, f)
	regexp.MustCompile(`^Belt\.List\.getBy$`),            // Belt.List.getBy(lst, pred)
	regexp.MustCompile(`^Belt\.List\.get$`),              // Belt.List.get(lst, idx)
	regexp.MustCompile(`^Belt\.List\.getExn$`),           // Belt.List.getExn(lst, idx)
	regexp.MustCompile(`^Belt\.List\.head$`),             // Belt.List.head(lst)
	regexp.MustCompile(`^Belt\.List\.headExn$`),          // Belt.List.headExn(lst)
	regexp.MustCompile(`^Belt\.List\.tail$`),             // Belt.List.tail(lst)
	regexp.MustCompile(`^Belt\.List\.tailExn$`),          // Belt.List.tailExn(lst)
	regexp.MustCompile(`^Belt\.List\.length$`),           // Belt.List.length(lst)
	regexp.MustCompile(`^Belt\.List\.size$`),             // Belt.List.size(lst)
	regexp.MustCompile(`^Belt\.List\.add$`),              // Belt.List.add(lst, x)
	regexp.MustCompile(`^Belt\.List\.concat$`),           // Belt.List.concat(lst1, lst2)
	regexp.MustCompile(`^Belt\.List\.concatMany$`),       // Belt.List.concatMany(lsts)
	regexp.MustCompile(`^Belt\.List\.flatten$`),          // Belt.List.flatten(lst)
	regexp.MustCompile(`^Belt\.List\.flatMap$`),          // Belt.List.flatMap(lst, f)
	regexp.MustCompile(`^Belt\.List\.sort$`),             // Belt.List.sort(lst, cmp)
	regexp.MustCompile(`^Belt\.List\.sortU$`),            // Belt.List.sortU(lst, cmp) — uncurried
	regexp.MustCompile(`^Belt\.List\.reverse$`),          // Belt.List.reverse(lst)
	regexp.MustCompile(`^Belt\.List\.every$`),            // Belt.List.every(lst, pred)
	regexp.MustCompile(`^Belt\.List\.some$`),             // Belt.List.some(lst, pred)
	regexp.MustCompile(`^Belt\.List\.has$`),              // Belt.List.has(lst, x, eq)
	regexp.MustCompile(`^Belt\.List\.zip$`),              // Belt.List.zip(lst1, lst2)
	regexp.MustCompile(`^Belt\.List\.zipBy$`),            // Belt.List.zipBy(lst1, lst2, f)
	regexp.MustCompile(`^Belt\.List\.unzip$`),            // Belt.List.unzip(lst)
	regexp.MustCompile(`^Belt\.List\.fromArray$`),        // Belt.List.fromArray(arr)
	regexp.MustCompile(`^Belt\.List\.toArray$`),          // Belt.List.toArray(lst)
	regexp.MustCompile(`^Belt\.List\.make$`),             // Belt.List.make(n, x)
	regexp.MustCompile(`^Belt\.List\.makeBy$`),           // Belt.List.makeBy(n, f)
	regexp.MustCompile(`^Belt\.List\.shuffle$`),          // Belt.List.shuffle(lst)
	regexp.MustCompile(`^Belt\.List\.drop$`),             // Belt.List.drop(lst, n)
	regexp.MustCompile(`^Belt\.List\.take$`),             // Belt.List.take(lst, n)
	regexp.MustCompile(`^Belt\.List\.splitAt$`),          // Belt.List.splitAt(lst, n)
	regexp.MustCompile(`^Belt\.List\.partition$`),        // Belt.List.partition(lst, pred)

	// ── Belt.Array module ────────────────────────────────────────────────────
	regexp.MustCompile(`^Belt\.Array\.map$`),                     // Belt.Array.map(arr, f)
	regexp.MustCompile(`^Belt\.Array\.filter$`),                  // Belt.Array.filter(arr, pred)
	regexp.MustCompile(`^Belt\.Array\.keep$`),                    // Belt.Array.keep(arr, pred)
	regexp.MustCompile(`^Belt\.Array\.reduce$`),                  // Belt.Array.reduce(arr, init, f)
	regexp.MustCompile(`^Belt\.Array\.forEach$`),                 // Belt.Array.forEach(arr, f)
	regexp.MustCompile(`^Belt\.Array\.forEachWithIndex$`),        // Belt.Array.forEachWithIndex(arr, f)
	regexp.MustCompile(`^Belt\.Array\.mapWithIndex$`),            // Belt.Array.mapWithIndex(arr, f)
	regexp.MustCompile(`^Belt\.Array\.filterMap$`),               // Belt.Array.filterMap(arr, f)
	regexp.MustCompile(`^Belt\.Array\.keepMap$`),                 // Belt.Array.keepMap(arr, f) — alias
	regexp.MustCompile(`^Belt\.Array\.get$`),                     // Belt.Array.get(arr, idx)
	regexp.MustCompile(`^Belt\.Array\.getExn$`),                  // Belt.Array.getExn(arr, idx)
	regexp.MustCompile(`^Belt\.Array\.length$`),                  // Belt.Array.length(arr)
	regexp.MustCompile(`^Belt\.Array\.size$`),                    // Belt.Array.size(arr)
	regexp.MustCompile(`^Belt\.Array\.concat$`),                  // Belt.Array.concat(arr1, arr2)
	regexp.MustCompile(`^Belt\.Array\.concatMany$`),              // Belt.Array.concatMany(arrs)
	regexp.MustCompile(`^Belt\.Array\.reverse$`),                 // Belt.Array.reverse(arr)
	regexp.MustCompile(`^Belt\.Array\.sort$`),                    // Belt.Array.sort(arr, cmp)
	regexp.MustCompile(`^Belt\.Array\.sortInPlace$`),             // Belt.Array.sortInPlace(arr, cmp)
	regexp.MustCompile(`^Belt\.Array\.every$`),                   // Belt.Array.every(arr, pred)
	regexp.MustCompile(`^Belt\.Array\.some$`),                    // Belt.Array.some(arr, pred)
	regexp.MustCompile(`^Belt\.Array\.find$`),                    // Belt.Array.find(arr, pred) — unofficial
	regexp.MustCompile(`^Belt\.Array\.getBy$`),                   // Belt.Array.getBy(arr, pred)
	regexp.MustCompile(`^Belt\.Array\.has$`),                     // Belt.Array.has(arr, x, eq)
	regexp.MustCompile(`^Belt\.Array\.zip$`),                     // Belt.Array.zip(arr1, arr2)
	regexp.MustCompile(`^Belt\.Array\.zipBy$`),                   // Belt.Array.zipBy(arr1, arr2, f)
	regexp.MustCompile(`^Belt\.Array\.unzip$`),                   // Belt.Array.unzip(arr)
	regexp.MustCompile(`^Belt\.Array\.fromList$`),                // Belt.Array.fromList(lst)
	regexp.MustCompile(`^Belt\.Array\.toList$`),                  // Belt.Array.toList(arr)
	regexp.MustCompile(`^Belt\.Array\.make$`),                    // Belt.Array.make(n, x)
	regexp.MustCompile(`^Belt\.Array\.makeBy$`),                  // Belt.Array.makeBy(n, f)
	regexp.MustCompile(`^Belt\.Array\.makeUninitializedUnsafe$`), // Belt.Array.makeUninitializedUnsafe(n)
	regexp.MustCompile(`^Belt\.Array\.copy$`),                    // Belt.Array.copy(arr)
	regexp.MustCompile(`^Belt\.Array\.fill$`),                    // Belt.Array.fill(arr, offset, len, x)
	regexp.MustCompile(`^Belt\.Array\.blit$`),                    // Belt.Array.blit(src, srcOff, dst, dstOff, len)
	regexp.MustCompile(`^Belt\.Array\.blitUnsafe$`),              // Belt.Array.blitUnsafe(...)
	regexp.MustCompile(`^Belt\.Array\.shuffle$`),                 // Belt.Array.shuffle(arr)
	regexp.MustCompile(`^Belt\.Array\.shuffleInPlace$`),          // Belt.Array.shuffleInPlace(arr)
	regexp.MustCompile(`^Belt\.Array\.slice$`),                   // Belt.Array.slice(arr, offset, len)
	regexp.MustCompile(`^Belt\.Array\.sliceToEnd$`),              // Belt.Array.sliceToEnd(arr, offset)
	regexp.MustCompile(`^Belt\.Array\.partition$`),               // Belt.Array.partition(arr, pred)
	regexp.MustCompile(`^Belt\.Array\.uniqBy$`),                  // Belt.Array.uniqBy(arr, f)
	regexp.MustCompile(`^Belt\.Array\.joinWith$`),                // Belt.Array.joinWith(arr, sep, f)
	regexp.MustCompile(`^Belt\.Array\.range$`),                   // Belt.Array.range(start, end)
	regexp.MustCompile(`^Belt\.Array\.rangeBy$`),                 // Belt.Array.rangeBy(start, end, step)

	// ── Belt.Option module ───────────────────────────────────────────────────
	regexp.MustCompile(`^Belt\.Option\.map$`),            // Belt.Option.map(opt, f)
	regexp.MustCompile(`^Belt\.Option\.flatMap$`),        // Belt.Option.flatMap(opt, f)
	regexp.MustCompile(`^Belt\.Option\.getWithDefault$`), // Belt.Option.getWithDefault(opt, default)
	regexp.MustCompile(`^Belt\.Option\.getExn$`),         // Belt.Option.getExn(opt)
	regexp.MustCompile(`^Belt\.Option\.isSome$`),         // Belt.Option.isSome(opt)
	regexp.MustCompile(`^Belt\.Option\.isNone$`),         // Belt.Option.isNone(opt)
	regexp.MustCompile(`^Belt\.Option\.eq$`),             // Belt.Option.eq(opt1, opt2, eq)
	regexp.MustCompile(`^Belt\.Option\.cmp$`),            // Belt.Option.cmp(opt1, opt2, cmp)
	regexp.MustCompile(`^Belt\.Option\.forEach$`),        // Belt.Option.forEach(opt, f)
	regexp.MustCompile(`^Belt\.Option\.mapWithDefault$`), // Belt.Option.mapWithDefault(opt, default, f)
	regexp.MustCompile(`^Belt\.Option\.orElse$`),         // Belt.Option.orElse(opt, other)
	regexp.MustCompile(`^Belt\.Option\.keep$`),           // Belt.Option.keep(opt, pred)

	// ── Belt.Map module ──────────────────────────────────────────────────────
	regexp.MustCompile(`^Belt\.Map\.get$`),       // Belt.Map.get(map, key)
	regexp.MustCompile(`^Belt\.Map\.getExn$`),    // Belt.Map.getExn(map, key)
	regexp.MustCompile(`^Belt\.Map\.set$`),       // Belt.Map.set(map, key, val)
	regexp.MustCompile(`^Belt\.Map\.remove$`),    // Belt.Map.remove(map, key)
	regexp.MustCompile(`^Belt\.Map\.has$`),       // Belt.Map.has(map, key)
	regexp.MustCompile(`^Belt\.Map\.map$`),       // Belt.Map.map(map, f)
	regexp.MustCompile(`^Belt\.Map\.filter$`),    // Belt.Map.filter(map, pred)
	regexp.MustCompile(`^Belt\.Map\.reduce$`),    // Belt.Map.reduce(map, init, f)
	regexp.MustCompile(`^Belt\.Map\.forEach$`),   // Belt.Map.forEach(map, f)
	regexp.MustCompile(`^Belt\.Map\.toArray$`),   // Belt.Map.toArray(map)
	regexp.MustCompile(`^Belt\.Map\.fromArray$`), // Belt.Map.fromArray(arr)
	regexp.MustCompile(`^Belt\.Map\.size$`),      // Belt.Map.size(map)
	regexp.MustCompile(`^Belt\.Map\.isEmpty$`),   // Belt.Map.isEmpty(map)
	regexp.MustCompile(`^Belt\.Map\.keys$`),      // Belt.Map.keys(map)
	regexp.MustCompile(`^Belt\.Map\.values$`),    // Belt.Map.values(map)
	regexp.MustCompile(`^Belt\.Map\.merge$`),     // Belt.Map.merge(map1, map2, f)
	regexp.MustCompile(`^Belt\.Map\.mergeMany$`), // Belt.Map.mergeMany(map, arr)
	regexp.MustCompile(`^Belt\.Map\.partition$`), // Belt.Map.partition(map, pred)
	regexp.MustCompile(`^Belt\.Map\.every$`),     // Belt.Map.every(map, pred)
	regexp.MustCompile(`^Belt\.Map\.some$`),      // Belt.Map.some(map, pred)

	// ── Belt.Map.String / Belt.Map.Int shortcuts ─────────────────────────────
	regexp.MustCompile(`^Belt\.Map\.String\.get$`),    // Belt.Map.String.get(map, key)
	regexp.MustCompile(`^Belt\.Map\.String\.set$`),    // Belt.Map.String.set(map, key, val)
	regexp.MustCompile(`^Belt\.Map\.String\.has$`),    // Belt.Map.String.has(map, key)
	regexp.MustCompile(`^Belt\.Map\.String\.remove$`), // Belt.Map.String.remove(map, key)
	regexp.MustCompile(`^Belt\.Map\.Int\.get$`),       // Belt.Map.Int.get(map, key)
	regexp.MustCompile(`^Belt\.Map\.Int\.set$`),       // Belt.Map.Int.set(map, key, val)
	regexp.MustCompile(`^Belt\.Map\.Int\.has$`),       // Belt.Map.Int.has(map, key)

	// ── Belt.Set module ──────────────────────────────────────────────────────
	regexp.MustCompile(`^Belt\.Set\.add$`),       // Belt.Set.add(set, x)
	regexp.MustCompile(`^Belt\.Set\.remove$`),    // Belt.Set.remove(set, x)
	regexp.MustCompile(`^Belt\.Set\.has$`),       // Belt.Set.has(set, x)
	regexp.MustCompile(`^Belt\.Set\.size$`),      // Belt.Set.size(set)
	regexp.MustCompile(`^Belt\.Set\.isEmpty$`),   // Belt.Set.isEmpty(set)
	regexp.MustCompile(`^Belt\.Set\.toArray$`),   // Belt.Set.toArray(set)
	regexp.MustCompile(`^Belt\.Set\.fromArray$`), // Belt.Set.fromArray(arr)
	regexp.MustCompile(`^Belt\.Set\.union$`),     // Belt.Set.union(s1, s2)
	regexp.MustCompile(`^Belt\.Set\.intersect$`), // Belt.Set.intersect(s1, s2)
	regexp.MustCompile(`^Belt\.Set\.diff$`),      // Belt.Set.diff(s1, s2)
	regexp.MustCompile(`^Belt\.Set\.subset$`),    // Belt.Set.subset(s1, s2)

	// ── Belt.Result module ───────────────────────────────────────────────────
	regexp.MustCompile(`^Belt\.Result\.map$`),            // Belt.Result.map(result, f)
	regexp.MustCompile(`^Belt\.Result\.flatMap$`),        // Belt.Result.flatMap(result, f)
	regexp.MustCompile(`^Belt\.Result\.getWithDefault$`), // Belt.Result.getWithDefault(result, default)
	regexp.MustCompile(`^Belt\.Result\.getExn$`),         // Belt.Result.getExn(result)
	regexp.MustCompile(`^Belt\.Result\.isOk$`),           // Belt.Result.isOk(result)
	regexp.MustCompile(`^Belt\.Result\.isError$`),        // Belt.Result.isError(result)
	regexp.MustCompile(`^Belt\.Result\.mapError$`),       // Belt.Result.mapError(result, f)

	// ── Belt.Float / Belt.Int module ─────────────────────────────────────────
	regexp.MustCompile(`^Belt\.Float\.fromInt$`), // Belt.Float.fromInt(i)
	regexp.MustCompile(`^Belt\.Float\.toInt$`),   // Belt.Float.toInt(f)
	regexp.MustCompile(`^Belt\.Int\.fromFloat$`), // Belt.Int.fromFloat(f)
	regexp.MustCompile(`^Belt\.Int\.toFloat$`),   // Belt.Int.toFloat(i)

	// ── ReactDOMRe bindings ──────────────────────────────────────────────────
	regexp.MustCompile(`^ReactDOMRe\.renderToElement$`),        // ReactDOMRe.renderToElement(el, domEl)
	regexp.MustCompile(`^ReactDOMRe\.renderToElementWithId$`),  // ReactDOMRe.renderToElementWithId(el, id)
	regexp.MustCompile(`^ReactDOMRe\.unmountComponentAtNode$`), // ReactDOMRe.unmountComponentAtNode(domEl)
	regexp.MustCompile(`^ReactDOMRe\.hydrateToElementWithId$`), // ReactDOMRe.hydrateToElementWithId(el, id)

	// ── React bindings ───────────────────────────────────────────────────────
	regexp.MustCompile(`^React\.string$`),         // React.string(s) — JSX string node
	regexp.MustCompile(`^React\.array$`),          // React.array(arr) — JSX array node
	regexp.MustCompile(`^React\.element$`),        // React.element(el) — JSX element
	regexp.MustCompile(`^React\.null$`),           // React.null — empty node (value)
	regexp.MustCompile(`^React\.int$`),            // React.int(n) — JSX int node
	regexp.MustCompile(`^React\.float$`),          // React.float(f) — JSX float node
	regexp.MustCompile(`^React\.createElement$`),  // React.createElement(comp, props, children)
	regexp.MustCompile(`^React\.cloneElement$`),   // React.cloneElement(el, props)
	regexp.MustCompile(`^React\.useReducer$`),     // React.useReducer(reducer, init)
	regexp.MustCompile(`^React\.useState$`),       // React.useState(init)
	regexp.MustCompile(`^React\.useEffect$`),      // React.useEffect(f)
	regexp.MustCompile(`^React\.useEffect0$`),     // React.useEffect0(f) — no deps
	regexp.MustCompile(`^React\.useEffect1$`),     // React.useEffect1(f, deps)
	regexp.MustCompile(`^React\.useCallback$`),    // React.useCallback(f)
	regexp.MustCompile(`^React\.useCallback0$`),   // React.useCallback0(f)
	regexp.MustCompile(`^React\.useCallback1$`),   // React.useCallback1(f, deps)
	regexp.MustCompile(`^React\.useMemo$`),        // React.useMemo(f)
	regexp.MustCompile(`^React\.useMemo0$`),       // React.useMemo0(f)
	regexp.MustCompile(`^React\.useMemo1$`),       // React.useMemo1(f, deps)
	regexp.MustCompile(`^React\.useRef$`),         // React.useRef(init)
	regexp.MustCompile(`^React\.useContext$`),     // React.useContext(ctx)
	regexp.MustCompile(`^React\.createContext$`),  // React.createContext(default)
	regexp.MustCompile(`^React\.createRef$`),      // React.createRef()
	regexp.MustCompile(`^React\.forwardRef$`),     // React.forwardRef(f)
	regexp.MustCompile(`^React\.memo$`),           // React.memo(comp)
	regexp.MustCompile(`^React\.memo1$`),          // React.memo1(comp, eq)
	regexp.MustCompile(`^React\.lazy_$`),          // React.lazy_(f)
	regexp.MustCompile(`^React\.Suspense\.make$`), // React.Suspense.make(...)
}

func init() {
	dynamicPatternsByLang["reasonml"] = reasonmlDynamicPatterns
}
