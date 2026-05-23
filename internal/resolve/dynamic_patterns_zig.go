package resolve

import "regexp"

// zigDynamicPatterns are per-language patterns for Zig.
// Registered via init() into dynamicPatternsByLang.
//
// The Zig extractor (internal/extractors/zig/zig.go) emits CALLS edges
// whose ToID is the rightmost identifier in a dotted call chain.  For a
// stdlib call like `mem.splitScalar(u8, s, '/')` the receiver prefix is
// discarded and the stub is "splitScalar".  Many Zig std lib namespaces
// (std.mem, std.fmt, std.ascii, std.json, std.meta, std.ArrayList) expose
// function names that are entirely specific to Zig's standard library.
//
// Two categories:
//
//  1. Zig-unique std.mem / std.ascii / std.meta identifiers — names that
//     exist in no other language's standard library with the exact same
//     spelling and casing:
//     splitScalar, splitSequence, splitBackwardsScalar, tokenizeScalar
//     (std.mem split/tokenize family); indexOfScalar, lastIndexOfScalar
//     (std.mem search); eqlIgnoreCase, isPrint (std.ascii); stringToEnum
//     (std.meta); toOwnedSlice (std.ArrayList); parseFromSlice
//     (std.json); allocPrint (std.fmt).
//
//  2. Zig std lib leaf names gated to lang=="zig" — common enough words
//     that require the gate but are virtually always stdlib calls in real
//     Zig code:
//     stringify (std.json.stringify — safe; Go uses json.Marshal),
//     writeAll  (std.io Writer.writeAll — safe; Java uses write/println),
//     trim      (std.mem.trim — safe; Python .strip(), JS .trim() are
//     method calls not bare functions in Zig context),
//     startsWith (std.mem.startsWith — safe; Python str.startswith() is
//     a method, Zig exposes it as a bare module function),
//     parseInt   (std.fmt.parseInt — safe under gate; TypeScript
//     parseInt is a global but Zig gate is tight enough).
//
// Excluded (too generic even under gate): assert, print, next, first, get,
// put, init, deinit, append, clamp, writer, entry, notFound, phrase.
//
// All patterns are gated to lang=="zig" (safer-bias rule #94).
var zigDynamicPatterns = []*regexp.Regexp{
	// ── Tier 1: Zig-unique std.mem split/tokenize family ────────────
	// These camelCase compound names exist in no other language stdlib.
	regexp.MustCompile(`^splitScalar$`),          // mem.splitScalar(T, slice, delimiter)
	regexp.MustCompile(`^splitSequence$`),        // mem.splitSequence(T, slice, delimiter)
	regexp.MustCompile(`^splitBackwardsScalar$`), // mem.splitBackwardsScalar(T, slice, delimiter)
	regexp.MustCompile(`^tokenizeScalar$`),       // mem.tokenizeScalar(T, slice, delimiter)
	regexp.MustCompile(`^tokenizeSequence$`),     // mem.tokenizeSequence(T, slice, delimiter)
	regexp.MustCompile(`^tokenizeAny$`),          // mem.tokenizeAny(T, slice, delimiter_bytes)

	// ── Tier 1: Zig-unique std.mem search functions ─────────────────
	regexp.MustCompile(`^indexOfScalar$`),     // mem.indexOfScalar(T, slice, value)
	regexp.MustCompile(`^lastIndexOfScalar$`), // mem.lastIndexOfScalar(T, slice, value)
	regexp.MustCompile(`^indexOfPosLinear$`),  // mem.indexOfPosLinear(T, slice, pos, needle)
	regexp.MustCompile(`^indexOfPos$`),        // mem.indexOfPos(T, slice, start, needle)
	regexp.MustCompile(`^lastIndexOf$`),       // mem.lastIndexOf(T, slice, value) — under Zig gate
	regexp.MustCompile(`^containsAtLeast$`),   // mem.containsAtLeast(T, slice, count, value)

	// ── Tier 1: Zig-unique std.ascii functions ───────────────────────
	regexp.MustCompile(`^eqlIgnoreCase$`), // ascii.eqlIgnoreCase(a, b)
	regexp.MustCompile(`^isPrint$`),       // ascii.isPrint(c)
	regexp.MustCompile(`^isAlpha$`),       // ascii.isAlpha(c)
	regexp.MustCompile(`^isAlNum$`),       // ascii.isAlNum(c)
	regexp.MustCompile(`^isDigit$`),       // ascii.isDigit(c)
	regexp.MustCompile(`^isUpper$`),       // ascii.isUpper(c)
	regexp.MustCompile(`^isLower$`),       // ascii.isLower(c)
	regexp.MustCompile(`^toUpper$`),       // ascii.toUpper(c)
	regexp.MustCompile(`^toLower$`),       // ascii.toLower(c)
	regexp.MustCompile(`^lowerString$`),   // ascii.lowerString(buffer, input)
	regexp.MustCompile(`^upperString$`),   // ascii.upperString(buffer, input)

	// ── Tier 1: Zig-unique std.meta / std.fmt / std.json ────────────
	regexp.MustCompile(`^stringToEnum$`),   // meta.stringToEnum(T, str)
	regexp.MustCompile(`^toOwnedSlice$`),   // ArrayList.toOwnedSlice()
	regexp.MustCompile(`^parseFromSlice$`), // json.parseFromSlice(T, alloc, slice, opts)
	regexp.MustCompile(`^allocPrint$`),     // fmt.allocPrint(alloc, template, args)
	regexp.MustCompile(`^allocPrintZ$`),    // fmt.allocPrintZ (null-terminated variant)
	regexp.MustCompile(`^bufPrint$`),       // fmt.bufPrint(buf, template, args)
	regexp.MustCompile(`^parseInt$`),       // fmt.parseInt(T, str, radix)

	// ── Tier 2: Zig std lib names gated to lang=="zig" ──────────────
	// Common words but safe under the Zig gate (real Zig code only uses
	// them as std module calls, never as user-defined method names).
	regexp.MustCompile(`^stringify$`),    // json.stringify(value, opts, writer)
	regexp.MustCompile(`^writeAll$`),     // Writer.writeAll(slice)
	regexp.MustCompile(`^startsWith$`),   // mem.startsWith(T, slice, prefix)
	regexp.MustCompile(`^endsWith$`),     // mem.endsWith(T, slice, suffix)
	regexp.MustCompile(`^trim$`),         // mem.trim(T, slice, chars)
	regexp.MustCompile(`^trimLeft$`),     // mem.trimLeft(T, slice, chars)
	regexp.MustCompile(`^trimRight$`),    // mem.trimRight(T, slice, chars)
	regexp.MustCompile(`^eql$`),          // mem.eql(T, a, b) — Zig gate: Go uses bytes.Equal
	regexp.MustCompile(`^replaceOwned$`), // mem.replaceOwned(T, alloc, input, needle, replacement)
	regexp.MustCompile(`^concat$`),       // mem.concat(T, alloc, slices) — Zig gate; Lua already gated
	// Zig std.io.Writer extended methods — Zig-specific vectored I/O.
	regexp.MustCompile(`^writeVecAll$`),      // Writer.writeVecAll(vecs) vectored write
	regexp.MustCompile(`^writeBytesNTimes$`), // Writer.writeBytesNTimes(bytes, n)
	// Zig std.mem fill/copy functions.
	regexp.MustCompile(`^copyForwards$`),  // mem.copyForwards(T, dest, src)
	regexp.MustCompile(`^copyBackwards$`), // mem.copyBackwards(T, dest, src)
	regexp.MustCompile(`^zeroes$`),        // mem.zeroes(T)
	// Zig std.fmt number-to-string helpers.
	regexp.MustCompile(`^formatInt$`),    // fmt.formatInt(value, radix, case, buf)
	regexp.MustCompile(`^formatIntBuf$`), // fmt.formatIntBuf(buf, value, radix, case, fill)
	regexp.MustCompile(`^parseFloat$`),   // fmt.parseFloat(T, buf)
	// Zig heap/allocator helpers.
	regexp.MustCompile(`^ArenaAllocator$`),       // std.heap.ArenaAllocator — type constructor call
	regexp.MustCompile(`^allocator$`),            // ArenaAllocator.allocator()
	regexp.MustCompile(`^toOwnedSliceSentinel$`), // ArrayList.toOwnedSliceSentinel(0)
	// std.debug helpers — gated to Zig so they don't match Go's testing.T
	// or Python's assert statements (which are keywords, not function calls).
	regexp.MustCompile(`^print$`),  // std.debug.print(fmt, args) — extremely common
	regexp.MustCompile(`^panic$`),  // std.debug.panic(msg) — Zig-gate: Go uses panic() too
	regexp.MustCompile(`^assert$`), // std.debug.assert(cond) — Zig uses it as fn call
	// std.math helpers — clamp and abs exist in other stdlibs but under
	// the Zig gate they map uniquely to std.math.{clamp,abs}.
	regexp.MustCompile(`^clamp$`), // math.clamp(val, min, max)
	regexp.MustCompile(`^isNan$`), // math.isNan(v)
	regexp.MustCompile(`^isInf$`), // math.isInf(v, sign)
	regexp.MustCompile(`^log2$`),  // math.log2(v)
	regexp.MustCompile(`^log10$`), // math.log10(v)
	// std.mem — additional patterns not covered above.
	regexp.MustCompile(`^bytesAsSlice$`),   // mem.bytesAsSlice(T, bytes)
	regexp.MustCompile(`^sliceAsBytes$`),   // mem.sliceAsBytes(slice)
	regexp.MustCompile(`^asBytes$`),        // mem.asBytes(ptr)
	regexp.MustCompile(`^readInt$`),        // mem.readInt(T, bytes, endian)
	regexp.MustCompile(`^writeInt$`),       // mem.writeInt(T, bytes, value, endian)
	regexp.MustCompile(`^writeIntNative$`), // mem.writeIntNative(T, bytes, value)
}

func init() {
	dynamicPatternsByLang["zig"] = zigDynamicPatterns
}
