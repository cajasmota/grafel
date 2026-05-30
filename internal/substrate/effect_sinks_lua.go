// Lua effect-sink sniffer — OpenResty / Lapis / plain Lua.
//
// Recognises Lua sink primitives:
//
//   - db_read / db_write  : resty.mysql (mysql:query with SELECT/INSERT/…),
//     resty.redis (red:get/hget/lrange/smembers for reads,
//     red:set/hset/lpush/sadd/del/expire for writes),
//     pgmoon postgres:query(SELECT/INSERT/…),
//     Lapis db.select / db.query / db.insert / db.update /
//     db.delete, generic db:query / db:execute
//
//   - http_out             : resty.http httpc:request/request_uri/connect,
//     ngx.location.capture / capture_multi
//
//   - fs_read / fs_write  : io.open (mode determines read vs write),
//     io.read / io.lines / file:read / file:lines,
//     io.write / file:write, os.rename / os.remove
//
//   - mutation             : table field assignment  t.x = …  /  t[k] = …
//     (excludes local-variable declarations and ==)
//
// Function attribution uses the nearest preceding `function` header —
// the same heuristic the other sniffers use. Lua's free-form grammar
// makes this imprecise for nested closures, but accurate enough for the
// substrate's needs (Phase 1A; Phase 4 will tighten via real AST).
package substrate

import "regexp"

func init() { RegisterEffectSniffer("lua", sniffEffectsLua) }

// luaEffectFuncHeaderRe matches `function name(` and `function obj.name(`
// and `function obj:name(`. Capture group 1 is the last component of the
// function name.
var luaEffectFuncHeaderRe = regexp.MustCompile(
	`(?m)^\s*(?:local\s+)?function\s+(?:[A-Za-z_][\w]*[.:])*([A-Za-z_][\w]*)\s*\(`,
)

// ---- DB ---------------------------------------------------------------

// luaDBReadRe matches read-flavoured DB calls.
//   - resty.mysql: mysql:query("SELECT …")
//   - resty.redis: red:get / red:hget / red:lrange / red:smembers / red:mget / red:keys
//   - pgmoon: postgres:query("SELECT …") or postgres:query("WITH …")
//   - Lapis db.select(…) / db.query("SELECT …")
//   - generic: db:query("SELECT …") / db:execute("SELECT …")
var luaDBReadRe = regexp.MustCompile(
	`\b(?:[a-z_][\w]*)\s*:\s*query\s*\(\s*["'](?i:SELECT|WITH)\b` +
		`|\b(?:[a-z_][\w]*)\s*:\s*execute\s*\(\s*["'](?i:SELECT|WITH)\b` +
		`|\bred\s*:\s*(?:get|hget|hgetall|hmget|mget|lrange|llen|lindex|smembers|scard|srandmember|sunion|sinter|sdiff|zrange|zrangebyscore|zscore|zcard|zrank|keys|scan|type|ttl|pttl|exists|strlen)\s*\(` +
		`|\bdb\s*\.\s*(?:select|raw_query)\s*\(` +
		`|\bdb\s*\.\s*query\s*\(\s*["'](?i:SELECT|WITH)\b`,
)

// luaDBWriteRe matches write-flavoured DB calls.
//   - resty.mysql: mysql:query("INSERT|UPDATE|DELETE|REPLACE|TRUNCATE …")
//   - resty.redis: red:set/hset/hmset/lpush/rpush/sadd/del/expire/persist/
//     incr/decr/incrby/decrby/append/setex/setnx/getset
//   - pgmoon: postgres:query("INSERT|UPDATE|DELETE|…")
//   - Lapis db.insert / db.update / db.delete / db.query("INSERT …")
//   - generic: db:query("INSERT …") / db:execute("INSERT …")
var luaDBWriteRe = regexp.MustCompile(
	`\b(?:[a-z_][\w]*)\s*:\s*query\s*\(\s*["'](?i:INSERT|UPDATE|DELETE|REPLACE|TRUNCATE|MERGE|CREATE|DROP|ALTER)\b` +
		`|\b(?:[a-z_][\w]*)\s*:\s*execute\s*\(\s*["'](?i:INSERT|UPDATE|DELETE|REPLACE|TRUNCATE|MERGE|CREATE|DROP|ALTER)\b` +
		`|\bred\s*:\s*(?:set|hset|hsetnx|hmset|mset|msetnx|lpush|rpush|linsert|lset|lrem|ltrim|rpoplpush|sadd|srem|smove|spop|zadd|zrem|zremrangebyscore|zremrangebyrank|zincrby|del|expire|pexpire|expireat|persist|rename|renamenx|append|setex|psetex|setnx|getset|incr|incrby|incrbyfloat|decr|decrby|publish|rpoplpush)\s*\(` +
		`|\bdb\s*\.\s*(?:insert|update|delete)\s*\(` +
		`|\bdb\s*\.\s*query\s*\(\s*["'](?i:INSERT|UPDATE|DELETE|REPLACE|TRUNCATE|MERGE)\b`,
)

// ---- HTTP -------------------------------------------------------------

// luaHTTPRe matches outbound HTTP primitives.
//   - resty.http: httpc:request(…) / httpc:request_uri(…) / httpc:connect(…)
//   - ngx.location.capture(…) / ngx.location.capture_multi(…)
var luaHTTPRe = regexp.MustCompile(
	`\b(?:[a-z_][\w]*)\s*:\s*(?:request_uri|request|connect)\s*\(` +
		`|\bngx\s*\.\s*location\s*\.\s*capture(?:_multi)?\s*\(`,
)

// ---- FS ---------------------------------------------------------------

// luaFSReadRe matches read-only filesystem primitives.
//   - io.open(path, "r"/"rb"/"rt") — default mode (no second arg) is read
//   - io.open(path) — two-arg with no mode defaults to "r"
//   - io.read / io.lines / file:read / file:lines
var luaFSReadRe = regexp.MustCompile(
	`\bio\s*\.\s*open\s*\([^,)]+(?:,\s*["'](?:r|rb|rt)["'])?\s*\)` +
		`|\bio\s*\.\s*open\s*\(\s*[^,)]+\s*\)` + // no second arg → "r"
		`|\bio\s*\.\s*(?:read|lines)\s*\(` +
		`|\b(?:[a-z_][\w]*)\s*:\s*(?:read|lines)\s*\(`,
)

// luaFSWriteRe matches write/mutating filesystem primitives.
//   - io.open(path, "w"/"wb"/"wt"/"a"/"ab"/"at"/"r+"/…)
//   - io.write(…) — write to stdout / current output file (side-effect)
//   - file:write(…)
//   - os.rename(…) / os.remove(…)
var luaFSWriteRe = regexp.MustCompile(
	`\bio\s*\.\s*open\s*\(\s*[^,)]+\s*,\s*["'](?:w|wb|wt|a|ab|at|r\+|w\+|a\+)["']` +
		`|\bio\s*\.\s*write\s*\(` +
		`|\b(?:[a-z_][\w]*)\s*:\s*write\s*\(` +
		`|\bos\s*\.\s*(?:rename|remove)\s*\(`,
)

// ---- Mutation ---------------------------------------------------------

// luaMutationRe matches table-field assignment:  t.x = …  or  t[k] = …
// Excludes:
//   - local variable declarations (local name = …)
//   - equality tests (==)
//   - lines starting with `function` (function foo = …)
//
// Pattern: identifier (possibly chained with dots/brackets) followed by
// `= ` but not `==`.
var luaMutationRe = regexp.MustCompile(
	`(?m)(?:^|[^=<>!~])` +
		`\b([A-Za-z_][\w]*(?:\s*(?:\.[A-Za-z_][\w]*|\[[^\]]+\]))+)\s*=(?:[^=])`,
)

// scanLuaFuncHeaders extracts function declaration headers for attribution.
func scanLuaFuncHeaders(content string) []funcHeader {
	var hs []funcHeader
	for _, m := range luaEffectFuncHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		hs = append(hs, funcHeader{
			Line: lineOfOffset(content, m[0]),
			Name: content[m[2]:m[3]],
		})
	}
	return hs
}

// appendLuaMatches appends EffectMatch records for every hit of re in content.
func appendLuaMatches(out []EffectMatch, content string, headers []funcHeader, re *regexp.Regexp, eff Effect, sink string, conf float64) []EffectMatch {
	for _, m := range re.FindAllStringIndex(content, -1) {
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		out = append(out, EffectMatch{
			Function:   fn,
			Line:       line,
			Effect:     eff,
			Sink:       sink,
			Confidence: conf,
		})
	}
	return out
}

func sniffEffectsLua(content string) []EffectMatch {
	if content == "" {
		return nil
	}
	headers := scanLuaFuncHeaders(content)
	var out []EffectMatch

	// DB effects
	out = appendLuaMatches(out, content, headers, luaDBReadRe, EffectDBRead, "resty.mysql/redis/pgmoon/lapis-db.read", 0.85)
	out = appendLuaMatches(out, content, headers, luaDBWriteRe, EffectDBWrite, "resty.mysql/redis/pgmoon/lapis-db.write", 0.85)

	// HTTP effects
	out = appendLuaMatches(out, content, headers, luaHTTPRe, EffectHTTPOut, "resty.http/ngx.location.capture", 0.9)

	// FS effects
	out = appendLuaMatches(out, content, headers, luaFSReadRe, EffectFSRead, "io.open/io.read/io.lines", 0.9)
	out = appendLuaMatches(out, content, headers, luaFSWriteRe, EffectFSWrite, "io.open(write)/io.write/os.rename", 0.9)

	// Mutation — table-field assignment
	out = appendLuaMatches(out, content, headers, luaMutationRe, EffectMutation, "table-field-assign", 0.7)

	return out
}
