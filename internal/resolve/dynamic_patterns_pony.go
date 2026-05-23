package resolve

import "regexp"

// ponyDynamicPatterns are per-language patterns for Pony.
// Registered via init() into dynamicPatternsByLang.
//
// The Pony extractor (internal/extractors/pony/extractor.go) emits CALLS
// edges whose ToID is the bare function name at the call site.
//
// Three categories of patterns:
//
//  1. Pony stdlib Array/Map/String/Iter — distinctive collection methods.
//     These appear as bare names in Pony method calls and function bodies.
//
//  2. Actor primitives — apply, dispose, and create are common actor entry
//     points in idiomatic Pony code.
//
//  3. Environment I/O helpers — env.out.print-family and error handling.
//
// All patterns are gated to lang=="pony" (safer-bias rule).
var ponyDynamicPatterns = []*regexp.Regexp{
	// ── Array ─────────────────────────────────────────────────────────
	regexp.MustCompile(`^push$`),      // Array.push(x)
	regexp.MustCompile(`^pop$`),       // Array.pop()
	regexp.MustCompile(`^shift$`),     // Array.shift()
	regexp.MustCompile(`^unshift$`),   // Array.unshift(x)
	regexp.MustCompile(`^append$`),    // Array.append(other)
	regexp.MustCompile(`^concat$`),    // Array.concat(other)
	regexp.MustCompile(`^clear$`),     // Array.clear()
	regexp.MustCompile(`^values$`),    // Array.values() iterator
	regexp.MustCompile(`^pairs$`),     // Array.pairs() iterator
	regexp.MustCompile(`^keys$`),      // Array.keys() iterator
	regexp.MustCompile(`^size$`),      // Array/Map.size()
	regexp.MustCompile(`^reserve$`),   // Array.reserve(n)
	regexp.MustCompile(`^copy_from$`), // Array.copy_from(src, ...)
	regexp.MustCompile(`^slice$`),     // Array.slice(from, to)
	regexp.MustCompile(`^reverse$`),   // Array.reverse()
	regexp.MustCompile(`^sort$`),      // Array.sort()
	regexp.MustCompile(`^contains$`),  // Array.contains(x)

	// ── Map ───────────────────────────────────────────────────────────
	regexp.MustCompile(`^insert$`),      // Map.insert(key, value)
	regexp.MustCompile(`^remove$`),      // Map.remove(key)
	regexp.MustCompile(`^upsert$`),      // Map.upsert(key, value, fn)
	regexp.MustCompile(`^get_or_else$`), // Map.get_or_else(key, default)

	// ── String ────────────────────────────────────────────────────────
	regexp.MustCompile(`^clone$`),      // String.clone()
	regexp.MustCompile(`^add$`),        // String.add(other)
	regexp.MustCompile(`^split$`),      // String.split(delim)
	regexp.MustCompile(`^strip$`),      // String.strip()
	regexp.MustCompile(`^lower$`),      // String.lower()
	regexp.MustCompile(`^upper$`),      // String.upper()
	regexp.MustCompile(`^find$`),       // String.find(sub)
	regexp.MustCompile(`^startswith$`), // String.startswith(prefix)
	regexp.MustCompile(`^endswith$`),   // String.endswith(suffix)
	regexp.MustCompile(`^at$`),         // String.at(index)

	// ── Iter (iterator combinators) ───────────────────────────────────
	regexp.MustCompile(`^map$`),      // Iter.map(fn)
	regexp.MustCompile(`^filter$`),   // Iter.filter(fn)
	regexp.MustCompile(`^fold$`),     // Iter.fold(acc, fn)
	regexp.MustCompile(`^count$`),    // Iter.count()
	regexp.MustCompile(`^collect$`),  // Iter.collect()
	regexp.MustCompile(`^zip$`),      // Iter.zip(other)
	regexp.MustCompile(`^take$`),     // Iter.take(n)
	regexp.MustCompile(`^skip$`),     // Iter.skip(n)
	regexp.MustCompile(`^flat_map$`), // Iter.flat_map(fn)
	regexp.MustCompile(`^run$`),      // Iter.run()

	// ── Actor primitives / standard entry points ───────────────────────
	regexp.MustCompile(`^apply$`),   // common actor/class entry point (also used by Lambda)
	regexp.MustCompile(`^dispose$`), // lifecycle: explicit resource cleanup
	regexp.MustCompile(`^create$`),  // constructor entry point (new create)
	regexp.MustCompile(`^write$`),   // TCPConnection.write / OutStream.write
	regexp.MustCompile(`^print$`),   // OutStream.print (env.out.print)
	regexp.MustCompile(`^flush$`),   // OutStream.flush

	// ── Env / capabilities ────────────────────────────────────────────
	regexp.MustCompile(`^exit$`), // Env.exitcode
	regexp.MustCompile(`^err$`),  // Env.err (StderrStream)

	// ── Error handling ────────────────────────────────────────────────
	regexp.MustCompile(`^error$`), // Pony error keyword (used as call in some patterns)
}

func init() {
	dynamicPatternsByLang["pony"] = ponyDynamicPatterns
}
