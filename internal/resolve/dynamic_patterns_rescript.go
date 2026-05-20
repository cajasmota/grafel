package resolve

import "regexp"

// rescriptDynamicPatterns are per-language patterns for ReScript.
// Registered via init() into dynamicPatternsByLang.
//
// The ReScript extractor (internal/extractors/rescript/extractor.go) emits
// CALLS edges whose ToID is either:
//   - a bare function name: "helper"
//   - a module-qualified dotted name: "Belt.List.map", "React.useState"
//   - a pipe-first target: "Belt.Array.keep", "Js.Promise.then_"
//
// Three categories of patterns:
//
//  1. Belt standard library — ReScript's replacement for the OCaml stdlib.
//     Module-qualified names are distinct to ReScript (Belt.List.*, etc.).
//
//  2. React bindings — React.useState, React.useEffect, React.string, etc.
//     Gated to rescript because these dotted forms don't appear in other langs
//     after the language gate.
//
//  3. Js namespace — Js.Promise.then_, Js.log, Js.Array.*, etc.
//     These are the ReScript bindings to JavaScript built-ins.
//
// All patterns are gated to lang=="rescript" (safer-bias rule).
var rescriptDynamicPatterns = []*regexp.Regexp{
	// ── Belt.List ────────────────────────────────────────────────────────
	regexp.MustCompile(`^Belt\.List\.map$`),         // Belt.List.map(f, lst)
	regexp.MustCompile(`^Belt\.List\.filter$`),       // Belt.List.keep(pred, lst) — Belt alias
	regexp.MustCompile(`^Belt\.List\.keep$`),         // Belt.List.keep(pred, lst)
	regexp.MustCompile(`^Belt\.List\.reduce$`),       // Belt.List.reduce(lst, init, f)
	regexp.MustCompile(`^Belt\.List\.forEach$`),      // Belt.List.forEach(lst, f)
	regexp.MustCompile(`^Belt\.List\.length$`),       // Belt.List.length(lst)
	regexp.MustCompile(`^Belt\.List\.head$`),         // Belt.List.head(lst)
	regexp.MustCompile(`^Belt\.List\.tail$`),         // Belt.List.tail(lst)
	regexp.MustCompile(`^Belt\.List\.fromArray$`),    // Belt.List.fromArray(arr)
	regexp.MustCompile(`^Belt\.List\.toArray$`),      // Belt.List.toArray(lst)
	regexp.MustCompile(`^Belt\.List\.reverse$`),      // Belt.List.reverse(lst)
	regexp.MustCompile(`^Belt\.List\.concat$`),       // Belt.List.concat(lst1, lst2)
	regexp.MustCompile(`^Belt\.List\.flatten$`),      // Belt.List.flatten(lsts)
	regexp.MustCompile(`^Belt\.List\.find$`),         // Belt.List.getBy(lst, pred)
	regexp.MustCompile(`^Belt\.List\.getBy$`),        // Belt.List.getBy(lst, pred)
	regexp.MustCompile(`^Belt\.List\.some$`),         // Belt.List.some(lst, pred)
	regexp.MustCompile(`^Belt\.List\.every$`),        // Belt.List.every(lst, pred)
	regexp.MustCompile(`^Belt\.List\.sort$`),         // Belt.List.sort(lst, cmp)
	regexp.MustCompile(`^Belt\.List\.sortBy$`),       // Belt.List.sortBy(lst, f)
	regexp.MustCompile(`^Belt\.List\.zip$`),          // Belt.List.zip(lst1, lst2)
	regexp.MustCompile(`^Belt\.List\.unzip$`),        // Belt.List.unzip(lst)
	regexp.MustCompile(`^Belt\.List\.partition$`),    // Belt.List.partition(lst, pred)
	regexp.MustCompile(`^Belt\.List\.makeBy$`),       // Belt.List.makeBy(n, f)
	regexp.MustCompile(`^Belt\.List\.make$`),         // Belt.List.make(n, x)

	// ── Belt.Array ───────────────────────────────────────────────────────
	regexp.MustCompile(`^Belt\.Array\.map$`),         // Belt.Array.map(arr, f)
	regexp.MustCompile(`^Belt\.Array\.keep$`),        // Belt.Array.keep(arr, pred)
	regexp.MustCompile(`^Belt\.Array\.filter$`),      // alias
	regexp.MustCompile(`^Belt\.Array\.reduce$`),      // Belt.Array.reduce(arr, init, f)
	regexp.MustCompile(`^Belt\.Array\.forEach$`),     // Belt.Array.forEach(arr, f)
	regexp.MustCompile(`^Belt\.Array\.length$`),      // Belt.Array.length(arr)
	regexp.MustCompile(`^Belt\.Array\.get$`),         // Belt.Array.get(arr, i)
	regexp.MustCompile(`^Belt\.Array\.getUnsafe$`),   // Belt.Array.getUnsafe(arr, i)
	regexp.MustCompile(`^Belt\.Array\.set$`),         // Belt.Array.set(arr, i, x)
	regexp.MustCompile(`^Belt\.Array\.sortBy$`),      // Belt.Array.sortBy(arr, f)
	regexp.MustCompile(`^Belt\.Array\.sort$`),        // Belt.Array.sort(arr, cmp)
	regexp.MustCompile(`^Belt\.Array\.reverse$`),     // Belt.Array.reverse(arr)
	regexp.MustCompile(`^Belt\.Array\.concat$`),      // Belt.Array.concat(arr1, arr2)
	regexp.MustCompile(`^Belt\.Array\.joinWith$`),    // Belt.Array.joinWith(arr, sep, f)
	regexp.MustCompile(`^Belt\.Array\.some$`),        // Belt.Array.some(arr, pred)
	regexp.MustCompile(`^Belt\.Array\.every$`),       // Belt.Array.every(arr, pred)
	regexp.MustCompile(`^Belt\.Array\.getBy$`),       // Belt.Array.getBy(arr, pred)
	regexp.MustCompile(`^Belt\.Array\.getIndexBy$`),  // Belt.Array.getIndexBy(arr, pred)
	regexp.MustCompile(`^Belt\.Array\.toList$`),      // Belt.Array.toList(arr)
	regexp.MustCompile(`^Belt\.Array\.fromList$`),    // Belt.Array.fromList(lst)
	regexp.MustCompile(`^Belt\.Array\.makeBy$`),      // Belt.Array.makeBy(n, f)
	regexp.MustCompile(`^Belt\.Array\.make$`),        // Belt.Array.make(n, x)
	regexp.MustCompile(`^Belt\.Array\.copy$`),        // Belt.Array.copy(arr)
	regexp.MustCompile(`^Belt\.Array\.flatMap$`),     // Belt.Array.flatMap(arr, f)
	regexp.MustCompile(`^Belt\.Array\.partition$`),   // Belt.Array.partition(arr, pred)
	regexp.MustCompile(`^Belt\.Array\.zip$`),         // Belt.Array.zip(arr1, arr2)
	regexp.MustCompile(`^Belt\.Array\.unzip$`),       // Belt.Array.unzip(arr)
	regexp.MustCompile(`^Belt\.Array\.slice$`),       // Belt.Array.slice(arr, ~offset, ~len)
	regexp.MustCompile(`^Belt\.Array\.sliceToEnd$`),  // Belt.Array.sliceToEnd(arr, offset)

	// ── Belt.Map / Belt.Map.String / Belt.Map.Int ────────────────────────
	regexp.MustCompile(`^Belt\.Map\.make$`),           // Belt.Map.make(~id)
	regexp.MustCompile(`^Belt\.Map\.fromArray$`),      // Belt.Map.fromArray(arr, ~id)
	regexp.MustCompile(`^Belt\.Map\.set$`),            // Belt.Map.set(m, k, v)
	regexp.MustCompile(`^Belt\.Map\.get$`),            // Belt.Map.get(m, k)
	regexp.MustCompile(`^Belt\.Map\.has$`),            // Belt.Map.has(m, k)
	regexp.MustCompile(`^Belt\.Map\.remove$`),         // Belt.Map.remove(m, k)
	regexp.MustCompile(`^Belt\.Map\.map$`),            // Belt.Map.map(m, f)
	regexp.MustCompile(`^Belt\.Map\.filter$`),         // Belt.Map.keep(m, pred)
	regexp.MustCompile(`^Belt\.Map\.keep$`),           // Belt.Map.keep(m, pred)
	regexp.MustCompile(`^Belt\.Map\.reduce$`),         // Belt.Map.reduce(m, init, f)
	regexp.MustCompile(`^Belt\.Map\.merge$`),          // Belt.Map.merge(m1, m2, f)
	regexp.MustCompile(`^Belt\.Map\.toArray$`),        // Belt.Map.toArray(m)
	regexp.MustCompile(`^Belt\.Map\.fromArray$`),      // Belt.Map.fromArray(arr, ~id)
	regexp.MustCompile(`^Belt\.Map\.size$`),           // Belt.Map.size(m)
	regexp.MustCompile(`^Belt\.Map\.isEmpty$`),        // Belt.Map.isEmpty(m)
	regexp.MustCompile(`^Belt\.Map\.String\.make$`),   // Belt.Map.String module
	regexp.MustCompile(`^Belt\.Map\.String\.set$`),
	regexp.MustCompile(`^Belt\.Map\.String\.get$`),
	regexp.MustCompile(`^Belt\.Map\.String\.has$`),
	regexp.MustCompile(`^Belt\.Map\.String\.remove$`),
	regexp.MustCompile(`^Belt\.Map\.String\.toArray$`),
	regexp.MustCompile(`^Belt\.Map\.Int\.make$`),
	regexp.MustCompile(`^Belt\.Map\.Int\.set$`),
	regexp.MustCompile(`^Belt\.Map\.Int\.get$`),
	regexp.MustCompile(`^Belt\.Map\.Int\.has$`),

	// ── Belt.Option ──────────────────────────────────────────────────────
	regexp.MustCompile(`^Belt\.Option\.map$`),             // Belt.Option.map(opt, f)
	regexp.MustCompile(`^Belt\.Option\.flatMap$`),         // Belt.Option.flatMap(opt, f)
	regexp.MustCompile(`^Belt\.Option\.getWithDefault$`),  // Belt.Option.getWithDefault(opt, default)
	regexp.MustCompile(`^Belt\.Option\.getExn$`),          // Belt.Option.getExn(opt)
	regexp.MustCompile(`^Belt\.Option\.isSome$`),          // Belt.Option.isSome(opt)
	regexp.MustCompile(`^Belt\.Option\.isNone$`),          // Belt.Option.isNone(opt)
	regexp.MustCompile(`^Belt\.Option\.filter$`),          // Belt.Option.filter(opt, pred)
	regexp.MustCompile(`^Belt\.Option\.forEach$`),         // Belt.Option.forEach(opt, f)

	// ── Belt.Result ──────────────────────────────────────────────────────
	regexp.MustCompile(`^Belt\.Result\.map$`),             // Belt.Result.map(r, f)
	regexp.MustCompile(`^Belt\.Result\.flatMap$`),         // Belt.Result.flatMap(r, f)
	regexp.MustCompile(`^Belt\.Result\.getWithDefault$`),  // Belt.Result.getWithDefault(r, def)
	regexp.MustCompile(`^Belt\.Result\.getExn$`),          // Belt.Result.getExn(r)
	regexp.MustCompile(`^Belt\.Result\.isOk$`),            // Belt.Result.isOk(r)
	regexp.MustCompile(`^Belt\.Result\.isError$`),         // Belt.Result.isError(r)
	regexp.MustCompile(`^Belt\.Result\.mapError$`),        // Belt.Result.mapError(r, f)

	// ── Belt.Set ─────────────────────────────────────────────────────────
	regexp.MustCompile(`^Belt\.Set\.make$`),               // Belt.Set.make(~id)
	regexp.MustCompile(`^Belt\.Set\.add$`),                // Belt.Set.add(s, x)
	regexp.MustCompile(`^Belt\.Set\.remove$`),             // Belt.Set.remove(s, x)
	regexp.MustCompile(`^Belt\.Set\.has$`),                // Belt.Set.has(s, x)
	regexp.MustCompile(`^Belt\.Set\.size$`),               // Belt.Set.size(s)
	regexp.MustCompile(`^Belt\.Set\.toArray$`),            // Belt.Set.toArray(s)
	regexp.MustCompile(`^Belt\.Set\.union$`),              // Belt.Set.union(s1, s2)
	regexp.MustCompile(`^Belt\.Set\.intersect$`),          // Belt.Set.intersect(s1, s2)
	regexp.MustCompile(`^Belt\.Set\.diff$`),               // Belt.Set.diff(s1, s2)

	// ── React bindings ───────────────────────────────────────────────────
	regexp.MustCompile(`^React\.useState$`),      // React.useState(() => initialState)
	regexp.MustCompile(`^React\.useEffect$`),     // React.useEffect(() => { ... }, deps)
	regexp.MustCompile(`^React\.useReducer$`),    // React.useReducer(reducer, init)
	regexp.MustCompile(`^React\.useCallback$`),   // React.useCallback(f, deps)
	regexp.MustCompile(`^React\.useMemo$`),       // React.useMemo(() => v, deps)
	regexp.MustCompile(`^React\.useRef$`),        // React.useRef(initialValue)
	regexp.MustCompile(`^React\.useContext$`),    // React.useContext(ctx)
	regexp.MustCompile(`^React\.createContext$`), // React.createContext(defaultValue)
	regexp.MustCompile(`^React\.createElement$`), // React.createElement(comp, props)
	regexp.MustCompile(`^React\.string$`),        // React.string("text") — string to ReactElement
	regexp.MustCompile(`^React\.array$`),         // React.array(arr) — array to ReactElement
	regexp.MustCompile(`^React\.int$`),           // React.int(n) — int to ReactElement
	regexp.MustCompile(`^React\.float$`),         // React.float(f) — float to ReactElement
	regexp.MustCompile(`^React\.null$`),          // React.null — render nothing
	regexp.MustCompile(`^React\.cloneElement$`),  // React.cloneElement(el, props)
	regexp.MustCompile(`^React\.memo$`),          // React.memo(comp)
	regexp.MustCompile(`^React\.forwardRef$`),    // React.forwardRef(f)
	regexp.MustCompile(`^React\.lazy_$`),         // React.lazy_(f) — lazy loading

	// ── Js namespace (JS interop) ────────────────────────────────────────
	regexp.MustCompile(`^Js\.log$`),              // Js.log(x) — console.log
	regexp.MustCompile(`^Js\.log2$`),             // Js.log2(a, b)
	regexp.MustCompile(`^Js\.logMany$`),          // Js.logMany(arr)
	regexp.MustCompile(`^Js\.Promise\.make$`),    // Js.Promise.make(~resolve, ~reject)
	regexp.MustCompile(`^Js\.Promise\.resolve$`), // Js.Promise.resolve(x)
	regexp.MustCompile(`^Js\.Promise\.reject$`),  // Js.Promise.reject(exn)
	regexp.MustCompile(`^Js\.Promise\.then_$`),   // Js.Promise.then_(p, f)
	regexp.MustCompile(`^Js\.Promise\.catch$`),   // Js.Promise.catch(p, f)
	regexp.MustCompile(`^Js\.Promise\.all$`),     // Js.Promise.all(arr)
	regexp.MustCompile(`^Js\.Promise\.race$`),    // Js.Promise.race(arr)
	regexp.MustCompile(`^Js\.Array\.map$`),       // Js.Array.map(arr, f) (legacy)
	regexp.MustCompile(`^Js\.Array\.filter$`),    // Js.Array.filter(arr, f)
	regexp.MustCompile(`^Js\.Array\.length$`),    // Js.Array.length(arr)
	regexp.MustCompile(`^Js\.Array\.push$`),      // Js.Array.push(arr, x)
	regexp.MustCompile(`^Js\.Array\.concat$`),    // Js.Array.concat(arr1, arr2)
	regexp.MustCompile(`^Js\.Array\.join$`),      // Js.Array.join(arr, sep)
	regexp.MustCompile(`^Js\.Array\.slice$`),     // Js.Array.slice(arr, from, to)
	regexp.MustCompile(`^Js\.Array\.indexOf$`),   // Js.Array.indexOf(arr, x)
	regexp.MustCompile(`^Js\.String\.make$`),     // Js.String.make(x)
	regexp.MustCompile(`^Js\.String\.split$`),    // Js.String.split(sep, s)
	regexp.MustCompile(`^Js\.String\.includes$`), // Js.String.includes(s, sub)
	regexp.MustCompile(`^Js\.String\.startsWith$`), // Js.String.startsWith(s, prefix)
	regexp.MustCompile(`^Js\.String\.endsWith$`),   // Js.String.endsWith(s, suffix)
	regexp.MustCompile(`^Js\.String\.trim$`),       // Js.String.trim(s)
	regexp.MustCompile(`^Js\.String\.toUpperCase$`), // Js.String.toUpperCase(s)
	regexp.MustCompile(`^Js\.String\.toLowerCase$`), // Js.String.toLowerCase(s)
	regexp.MustCompile(`^Js\.Dict\.make$`),          // Js.Dict.make()
	regexp.MustCompile(`^Js\.Dict\.get$`),           // Js.Dict.get(d, k)
	regexp.MustCompile(`^Js\.Dict\.set$`),           // Js.Dict.set(d, k, v)
	regexp.MustCompile(`^Js\.Dict\.keys$`),          // Js.Dict.keys(d)
	regexp.MustCompile(`^Js\.Dict\.values$`),        // Js.Dict.values(d)
	regexp.MustCompile(`^Js\.Dict\.entries$`),       // Js.Dict.entries(d)
	regexp.MustCompile(`^Js\.Json\.parseExn$`),      // Js.Json.parseExn(s)
	regexp.MustCompile(`^Js\.Json\.stringify$`),     // Js.Json.stringify(v)
	regexp.MustCompile(`^Js\.Math\.random$`),        // Js.Math.random()
	regexp.MustCompile(`^Js\.Math\.abs_int$`),       // Js.Math.abs_int(n)
	regexp.MustCompile(`^Js\.Math\.floor_int$`),     // Js.Math.floor_int(f)
	regexp.MustCompile(`^Js\.Math\.ceil_int$`),      // Js.Math.ceil_int(f)
	regexp.MustCompile(`^Js\.Math\.round$`),         // Js.Math.round(f)
	regexp.MustCompile(`^Js\.Math\.min_int$`),       // Js.Math.min_int(a, b)
	regexp.MustCompile(`^Js\.Math\.max_int$`),       // Js.Math.max_int(a, b)
	regexp.MustCompile(`^Js\.Date\.make$`),          // Js.Date.make()
	regexp.MustCompile(`^Js\.Date\.now$`),           // Js.Date.now()

	// ── Pipe-first pattern — any dotted Belt/React/Js call ───────────────
	// This catches long-tail Belt/React/Js module function calls that aren't
	// enumerated above. The pattern is restricted to the Belt., React., Js.
	// namespaces to remain conservative.
	regexp.MustCompile(`^Belt\.[A-Z][a-zA-Z]*\.[a-z][a-zA-Z_]*$`),
	regexp.MustCompile(`^React\.[a-z][a-zA-Z]*$`),
	regexp.MustCompile(`^Js\.[A-Z][a-zA-Z]*\.[a-z][a-zA-Z_]*$`),
}

func init() {
	dynamicPatternsByLang["rescript"] = rescriptDynamicPatterns
}
