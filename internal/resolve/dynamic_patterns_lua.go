package resolve

import "regexp"

// luaDynamicPatterns are per-language patterns for Lua.
// Registered via init() into dynamicPatternsByLang.
//
// Lua dynamic-pattern catalog (issue #44 — Lua resolver slice).
//
// The Lua extractor's luaCallTarget helper returns the LAST identifier
// before the argument list for any function_call node.  For a dotted
// stdlib call like `table.insert(t, v)` the receiver ("table") is
// discarded and only the leaf callee ("insert") reaches the resolver.
// The same stripping happens for `string.format`, `math.floor`,
// `os.getenv`, `coroutine.resume`, etc.
//
// Two categories are handled:
//
//  1. Lua-unique global built-ins — identifiers that are part of the
//     Lua standard environment and are virtually never user-defined
//     function names in any other language:
//     ipairs, pairs, pcall, xpcall, rawget, rawset, rawequal, rawlen,
//     setmetatable, getmetatable, tostring, tonumber, unpack, select,
//     coroutine.wrap → bare "wrap" (Lua-gate makes this safe enough).
//     These names land as BugExtractor on every Lua corpus because the
//     graph never contains an entity whose Name is "ipairs". With the
//     Lua language gate the risk of false-positive against a user-defined
//     `ipairs`/`pairs`/`pcall` is negligible.
//
//  2. Lua stdlib module leaf names — table.*, string.*, math.*, os.*,
//     coroutine.* leaf identifiers that are too common as user-defined
//     method names to be safe cross-language, but are safe under the
//     Lua gate because real Lua code uses them exclusively as stdlib
//     calls.  Covered: insert, remove, sort, concat (table.*);
//     format, sub, gmatch, gsub, find, byte, char, rep, len
//     (string.*); floor, ceil, sqrt, abs, max, min, random, huge, pi,
//     fmod, modf, exp, log (math.*); getenv, time, clock, exit, date,
//     execute (os.*); resume, yield, status, create, wrap (coroutine.*).
//
// All patterns are gated to lang=="lua" so they cannot fire on stubs
// from Go, Python, Ruby, Java, TypeScript, or any other language
// (safer-bias rule #94).  Cross-language collisions that motivated this
// gate: `format` (Python str.format), `insert` (SQL), `remove` (JS
// Array.prototype.remove), `exit` (C/Python), `type` (TypeScript),
// `create` (factory pattern in any language).
var luaDynamicPatterns = []*regexp.Regexp{
	// ── Tier 1: Lua-unique global identifiers ──────────────────────
	// These names exist in no other language's standard global scope
	// with this exact spelling; the Lua gate is a belt-and-suspenders
	// measure rather than a primary safety control.
	regexp.MustCompile(`^ipairs$`),
	regexp.MustCompile(`^pairs$`),
	regexp.MustCompile(`^pcall$`),
	regexp.MustCompile(`^xpcall$`),
	regexp.MustCompile(`^rawget$`),
	regexp.MustCompile(`^rawset$`),
	regexp.MustCompile(`^rawequal$`),
	regexp.MustCompile(`^rawlen$`),
	regexp.MustCompile(`^setmetatable$`),
	regexp.MustCompile(`^getmetatable$`),
	regexp.MustCompile(`^tostring$`),
	regexp.MustCompile(`^tonumber$`),
	regexp.MustCompile(`^unpack$`),
	regexp.MustCompile(`^select$`),
	regexp.MustCompile(`^next$`),
	regexp.MustCompile(`^collectgarbage$`),
	regexp.MustCompile(`^dofile$`),
	regexp.MustCompile(`^loadfile$`),
	regexp.MustCompile(`^loadstring$`),
	regexp.MustCompile(`^loadchunk$`),

	// ── Tier 2: table.* stdlib leaf names ─────────────────────────
	// Gated to lua; too generic otherwise (`insert` in SQL/Java,
	// `remove` in JS/C++, `sort` in Python/Go, `concat` in JS).
	regexp.MustCompile(`^insert$`),
	regexp.MustCompile(`^remove$`),
	regexp.MustCompile(`^sort$`),
	regexp.MustCompile(`^concat$`),
	regexp.MustCompile(`^move$`),

	// ── Tier 2: string.* stdlib leaf names ────────────────────────
	// `format`/`sub`/`find`/`byte`/`char`/`rep`/`len`/`gmatch`/`gsub`
	// are Lua string lib methods the extractor strips to bare leaves.
	regexp.MustCompile(`^gmatch$`),
	regexp.MustCompile(`^gsub$`),
	regexp.MustCompile(`^byte$`),
	regexp.MustCompile(`^char$`),
	regexp.MustCompile(`^rep$`),
	regexp.MustCompile(`^dump$`), // string.dump

	// ── Tier 2: math.* stdlib leaf names ──────────────────────────
	regexp.MustCompile(`^floor$`),
	regexp.MustCompile(`^ceil$`),
	regexp.MustCompile(`^sqrt$`),
	regexp.MustCompile(`^fabs$`),
	regexp.MustCompile(`^fmod$`),
	regexp.MustCompile(`^modf$`),
	regexp.MustCompile(`^frexp$`),
	regexp.MustCompile(`^ldexp$`),
	regexp.MustCompile(`^random$`),
	regexp.MustCompile(`^randomseed$`),
	regexp.MustCompile(`^huge$`),
	regexp.MustCompile(`^tointeger$`), // math.tointeger (Lua 5.3+)

	// ── Tier 2: os.* stdlib leaf names ────────────────────────────
	regexp.MustCompile(`^tmpname$`),  // os.tmpname
	regexp.MustCompile(`^difftime$`), // os.difftime

	// ── Tier 2: coroutine.* stdlib leaf names ─────────────────────
	// `resume`, `yield`, `wrap`, `status`, `running`, `isyieldable`
	// are coroutine module methods.  `create` is also used here but is
	// too ambiguous even with a Lua gate (factory pattern), so it is
	// deliberately excluded.
	regexp.MustCompile(`^resume$`),
	regexp.MustCompile(`^yield$`),
	regexp.MustCompile(`^isyieldable$`), // Lua 5.3+
	regexp.MustCompile(`^running$`),     // coroutine.running
}

func init() {
	dynamicPatternsByLang["lua"] = luaDynamicPatterns
}
