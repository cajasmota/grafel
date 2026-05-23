package resolve

import "regexp"

// nimDynamicPatterns are per-language patterns for Nim.
// Registered via init() into dynamicPatternsByLang.
//
// The Nim extractor (internal/extractors/nim/nim.go) emits CALLS edges
// whose ToID is the bare function/proc name at the call site.
//
// Two categories of patterns:
//
//  1. Nim-unique stdlib identifiers — proc names that exist only in Nim's
//     standard library and are highly unlikely to appear in any other
//     language as user-defined identifiers:
//     strutils: split, contains, startsWith, endsWith, strip, replace,
//     toLowerAscii, toUpperAscii, parseInt, parseFloat,
//     join, indent, dedent, repeat, multiReplace
//     sequtils: map, filter, foldl, foldr, zip, unzip, distribute,
//     flatten, deduplicate, all, any, count, toSeq
//     tables:   initTable, newTable, toTable, getOrDefault, hasKey
//     asyncdispatch/chronos: waitFor, asyncCheck, runForever
//     json:     parseJson, toJson, getStr, getInt, getBool, getFloat,
//     newJObject, newJArray, newJString, newJInt, newJBool
//     options:  some, none, isSome, isNone, get, unsafeGet
//
//  2. Nim patterns gated to lang=="nim" — common enough words requiring
//     the gate but always stdlib calls in real Nim code:
//     echo, add, len, high, low, inc, dec, newSeq, newString
//
// All patterns are gated to lang=="nim" (safer-bias rule).
var nimDynamicPatterns = []*regexp.Regexp{
	// ── strutils ─────────────────────────────────────────────────────
	regexp.MustCompile(`^toLowerAscii$`),    // strutils.toLowerAscii(s)
	regexp.MustCompile(`^toUpperAscii$`),    // strutils.toUpperAscii(s)
	regexp.MustCompile(`^capitalizeAscii$`), // strutils.capitalizeAscii(s)
	regexp.MustCompile(`^multiReplace$`),    // strutils.multiReplace(s, replacements)
	regexp.MustCompile(`^dedent$`),          // strutils.dedent(s)
	regexp.MustCompile(`^unescape$`),        // strutils.unescape(s)
	regexp.MustCompile(`^isDigit$`),         // strutils.isDigit(c)
	regexp.MustCompile(`^isAlphaAscii$`),    // strutils.isAlphaAscii(c)
	regexp.MustCompile(`^isSpaceAscii$`),    // strutils.isSpaceAscii(c)

	// ── sequtils ─────────────────────────────────────────────────────
	regexp.MustCompile(`^foldl$`),       // sequtils.foldl(s, op)
	regexp.MustCompile(`^foldr$`),       // sequtils.foldr(s, op)
	regexp.MustCompile(`^unzip$`),       // sequtils.unzip(s)
	regexp.MustCompile(`^distribute$`),  // sequtils.distribute(s, n)
	regexp.MustCompile(`^flatten$`),     // sequtils.flatten(s)
	regexp.MustCompile(`^deduplicate$`), // sequtils.deduplicate(s)
	regexp.MustCompile(`^toSeq$`),       // sequtils.toSeq(iter)

	// ── tables ───────────────────────────────────────────────────────
	regexp.MustCompile(`^initTable$`),        // tables.initTable[K,V]()
	regexp.MustCompile(`^newTable$`),         // tables.newTable[K,V]()
	regexp.MustCompile(`^toTable$`),          // tables.toTable(pairs)
	regexp.MustCompile(`^getOrDefault$`),     // tables.getOrDefault(t, key, default)
	regexp.MustCompile(`^initOrderedTable$`), // tables.initOrderedTable[K,V]()
	regexp.MustCompile(`^newOrderedTable$`),  // tables.newOrderedTable[K,V]()
	regexp.MustCompile(`^initHashSet$`),      // sets.initHashSet[T]()
	regexp.MustCompile(`^newHashSet$`),       // sets.newHashSet[T]()
	regexp.MustCompile(`^toHashSet$`),        // sets.toHashSet(s)
	regexp.MustCompile(`^incl$`),             // sets.incl(s, elem)
	regexp.MustCompile(`^excl$`),             // sets.excl(s, elem)

	// ── asyncdispatch / chronos async patterns ────────────────────────
	regexp.MustCompile(`^waitFor$`),       // asyncdispatch.waitFor(future)
	regexp.MustCompile(`^asyncCheck$`),    // asyncdispatch.asyncCheck(future)
	regexp.MustCompile(`^runForever$`),    // asyncdispatch.runForever()
	regexp.MustCompile(`^newAsyncEvent$`), // asyncdispatch.newAsyncEvent()
	regexp.MustCompile(`^poll$`),          // asyncdispatch.poll(timeout)
	regexp.MustCompile(`^sleepAsync$`),    // asyncdispatch.sleepAsync(ms)

	// ── json ─────────────────────────────────────────────────────────
	regexp.MustCompile(`^parseJson$`),  // json.parseJson(s)
	regexp.MustCompile(`^toJson$`),     // json.toJson(v)  (jsony/jsonutils)
	regexp.MustCompile(`^newJObject$`), // json.newJObject()
	regexp.MustCompile(`^newJArray$`),  // json.newJArray()
	regexp.MustCompile(`^newJString$`), // json.newJString(s)
	regexp.MustCompile(`^newJInt$`),    // json.newJInt(n)
	regexp.MustCompile(`^newJBool$`),   // json.newJBool(b)
	regexp.MustCompile(`^newJFloat$`),  // json.newJFloat(f)
	regexp.MustCompile(`^getStr$`),     // json.getStr(node)
	regexp.MustCompile(`^getInt$`),     // json.getInt(node)
	regexp.MustCompile(`^getBool$`),    // json.getBool(node)
	regexp.MustCompile(`^getFloat$`),   // json.getFloat(node)
	regexp.MustCompile(`^getElems$`),   // json.getElems(node)
	regexp.MustCompile(`^getFields$`),  // json.getFields(node)
	regexp.MustCompile(`^hasKey$`),     // json.hasKey(node, key) / tables.hasKey

	// ── options ──────────────────────────────────────────────────────
	regexp.MustCompile(`^isSome$`),    // options.isSome(opt)
	regexp.MustCompile(`^isNone$`),    // options.isNone(opt)
	regexp.MustCompile(`^unsafeGet$`), // options.unsafeGet(opt)
	regexp.MustCompile(`^get$`),       // options.get(opt) — gated to Nim; Python uses .get() on dict

	// ── os / io ──────────────────────────────────────────────────────
	regexp.MustCompile(`^readFile$`),        // io.readFile(path)
	regexp.MustCompile(`^writeFile$`),       // io.writeFile(path, content)
	regexp.MustCompile(`^fileExists$`),      // os.fileExists(path)
	regexp.MustCompile(`^dirExists$`),       // os.dirExists(path)
	regexp.MustCompile(`^getAppDir$`),       // os.getAppDir()
	regexp.MustCompile(`^getTempDir$`),      // os.getTempDir()
	regexp.MustCompile(`^joinPath$`),        // os.joinPath(parts...)
	regexp.MustCompile(`^splitPath$`),       // os.splitPath(path)
	regexp.MustCompile(`^extractFilename$`), // os.extractFilename(path)
	regexp.MustCompile(`^changeFileExt$`),   // os.changeFileExt(path, ext)
	regexp.MustCompile(`^createDir$`),       // os.createDir(path)
	regexp.MustCompile(`^removeDir$`),       // os.removeDir(path)
	regexp.MustCompile(`^copyFile$`),        // os.copyFile(src, dest)
	regexp.MustCompile(`^moveFile$`),        // os.moveFile(src, dest)
	regexp.MustCompile(`^removeFile$`),      // os.removeFile(path)
	regexp.MustCompile(`^walkFiles$`),       // os.walkFiles(pattern)
	regexp.MustCompile(`^walkDir$`),         // os.walkDir(path)
	regexp.MustCompile(`^walkDirRec$`),      // os.walkDirRec(path)

	// ── math ─────────────────────────────────────────────────────────
	regexp.MustCompile(`^sqrt$`),           // math.sqrt(x)
	regexp.MustCompile(`^pow$`),            // math.pow(x, y)
	regexp.MustCompile(`^floor$`),          // math.floor(x)
	regexp.MustCompile(`^ceil$`),           // math.ceil(x)
	regexp.MustCompile(`^round$`),          // math.round(x)
	regexp.MustCompile(`^trunc$`),          // math.trunc(x)
	regexp.MustCompile(`^isPowerOfTwo$`),   // math.isPowerOfTwo(n)
	regexp.MustCompile(`^nextPowerOfTwo$`), // math.nextPowerOfTwo(n)

	// ── Nim-gated common names ────────────────────────────────────────
	// These exist in multiple languages but under the Nim gate they map
	// specifically to Nim stdlib usage patterns.
	regexp.MustCompile(`^newSeq$`),         // system.newSeq[T](n)
	regexp.MustCompile(`^newSeqOfCap$`),    // system.newSeqOfCap[T](cap)
	regexp.MustCompile(`^newString$`),      // system.newString(len)
	regexp.MustCompile(`^newStringOfCap$`), // system.newStringOfCap(cap)
	regexp.MustCompile(`^setLen$`),         // system.setLen(s, n)
	regexp.MustCompile(`^del$`),            // system.del(s, i)
	regexp.MustCompile(`^delete$`),         // system.delete(s, i)
	regexp.MustCompile(`^insert$`),         // system.insert(s, item, i)
	regexp.MustCompile(`^reversed$`),       // algorithm.reversed(s)
	regexp.MustCompile(`^sorted$`),         // algorithm.sorted(s, cmp)
	regexp.MustCompile(`^sort$`),           // algorithm.sort(s, cmp)
	regexp.MustCompile(`^binarySearch$`),   // algorithm.binarySearch(s, v)
	regexp.MustCompile(`^lowerBound$`),     // algorithm.lowerBound(s, v)
	regexp.MustCompile(`^upperBound$`),     // algorithm.upperBound(s, v)
}

func init() {
	dynamicPatternsByLang["nim"] = nimDynamicPatterns
}
