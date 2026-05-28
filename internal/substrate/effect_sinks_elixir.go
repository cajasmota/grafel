// Elixir effect-sink sniffer (#2765 Phase 1A T2).
//
// Recognises Elixir sink primitives:
//
//   - http_out  : HTTPoison.<get|post|put|patch|delete|head|request>,
//                 Tesla.<verb> / Tesla.Client, Finch.build / Finch.request,
//                 Mint.HTTP, Req.<verb> / Req.new, :httpc.request
//   - db_read   : Ecto `Repo.all / Repo.get / Repo.get_by / Repo.one /
//                 Repo.stream / Repo.preload / Repo.aggregate /
//                 Repo.exists?`, `from(... in ..., select: ...) |> Repo.all`,
//                 raw `Ecto.Adapters.SQL.query` with SELECT
//   - db_write  : Ecto `Repo.insert / Repo.insert! / Repo.insert_all /
//                 Repo.update / Repo.update! / Repo.update_all /
//                 Repo.delete / Repo.delete! / Repo.delete_all /
//                 Repo.transaction / Repo.insert_or_update`
//   - fs_read   : `File.read / File.read! / File.stream! / File.open` (no
//                 :write mode), File.exists?, File.ls, File.dir?, IO.read
//   - fs_write  : `File.write / File.write! / File.cp / File.cp_r /
//                 File.rm / File.rm_rf / File.mkdir / File.mkdir_p /
//                 File.rename`, File.open with `[:write|:append|:exclusive]`
//   - mutation  : `var = ...` rebinding is not a side-effect in Elixir
//                 (immutable bindings); we approximate "mutation" via
//                 `Agent.update / GenServer.cast / :ets.insert /
//                 :persistent_term.put / Process.put` — observable state
//                 mutations to OTP / runtime state.
//
// Function attribution uses the nearest preceding `def` / `defp` /
// `defmacro` / `defmacrop` header.
package substrate

import "regexp"

func init() { RegisterEffectSniffer("elixir", sniffEffectsElixir) }

// elixirFuncHeaderRe matches `def name(` / `defp name(` / `defmacro(p)?`.
// Capture group 1 is the bare function name.
var elixirFuncHeaderRe = regexp.MustCompile(
	`(?m)^\s*def(?:p|macro|macrop)?\s+([A-Za-z_][\w]*[!?]?)\s*[\(,\sdo]`,
)

// elixirHTTPRe matches outbound HTTP primitives.
var elixirHTTPRe = regexp.MustCompile(
	`\bHTTPoison\s*\.\s*(?:get|get!|post|post!|put|put!|patch|patch!|delete|delete!|head|options|request|request!)\s*\(` +
		`|\bTesla\s*\.\s*(?:get|post|put|patch|delete|head|options|request|client)\s*\(` +
		`|\bFinch\s*\.\s*(?:build|request|stream)\s*\(` +
		`|\bReq\s*\.\s*(?:get|post|put|patch|delete|head|new|request|request!)\s*\(` +
		`|\bMint\s*\.\s*HTTP\b` +
		`|\b:httpc\s*\.\s*request\s*\(` +
		`|\b:hackney\s*\.\s*request\s*\(`,
)

// elixirDBReadRe matches Ecto read primitives.
var elixirDBReadRe = regexp.MustCompile(
	`\b(?:[A-Z]\w*\.)?Repo\s*\.\s*(?:all|get|get!|get_by|get_by!|one|one!|stream|preload|aggregate|exists\?|reload|reload!|in_transaction\?)\s*\(` +
		`|\b(?:[A-Z]\w*\.)?Repo\s*\.\s*(?:all|stream)\b`,
)

// elixirRawSelectRe matches raw SQL adapter SELECT.
var elixirRawSelectRe = regexp.MustCompile(
	`\bEcto\s*\.\s*Adapters\s*\.\s*SQL\s*\.\s*query[!]?\s*\(\s*[^,]+,\s*"(?i:\s*(?:SELECT|WITH)\b)`,
)

// elixirDBWriteRe matches Ecto write primitives.
var elixirDBWriteRe = regexp.MustCompile(
	`\b(?:[A-Z]\w*\.)?Repo\s*\.\s*(?:insert|insert!|insert_all|insert_or_update|insert_or_update!|update|update!|update_all|delete|delete!|delete_all|transaction)\s*\(`,
)

// elixirRawWriteRe matches raw SQL adapter INSERT/UPDATE/DELETE.
var elixirRawWriteRe = regexp.MustCompile(
	`\bEcto\s*\.\s*Adapters\s*\.\s*SQL\s*\.\s*query[!]?\s*\(\s*[^,]+,\s*"(?i:\s*(?:INSERT|UPDATE|DELETE|REPLACE|MERGE|TRUNCATE)\b)`,
)

// elixirFSReadRe matches read-only filesystem primitives.
var elixirFSReadRe = regexp.MustCompile(
	`\bFile\s*\.\s*(?:read|read!|stream!|exists\?|ls|ls!|dir\?|regular\?|stat|stat!|lstat|lstat!)\s*\(` +
		`|\bFile\s*\.\s*open\s*\(\s*[^,)]+\s*\)` + // no mode arg defaults to read
		`|\bFile\s*\.\s*open\s*\(\s*[^,)]+,\s*\[:read\]` +
		`|\bIO\s*\.\s*read\s*\(`,
)

// elixirFSWriteRe matches write filesystem primitives.
var elixirFSWriteRe = regexp.MustCompile(
	`\bFile\s*\.\s*(?:write|write!|cp|cp!|cp_r|cp_r!|rm|rm!|rm_rf|rm_rf!|mkdir|mkdir!|mkdir_p|mkdir_p!|rename|rename!|touch|touch!|chmod|chmod!|chown|chown!)\s*\(` +
		`|\bFile\s*\.\s*open\s*\(\s*[^,)]+,\s*\[(?:[^]]*:write|[^]]*:append|[^]]*:exclusive)`,
)

// elixirProcessRe matches process-spawn primitives.
var elixirProcessRe = regexp.MustCompile(
	`\bSystem\s*\.\s*(?:cmd|shell|find_executable)\s*\(` +
		`|\bPort\s*\.\s*open\s*\(\s*\{:spawn`,
)

// elixirMutationRe matches OTP / runtime observable mutations. Elixir's
// immutable bindings mean there is no `=`-as-mutation primitive; the
// substrate's mutation effect tracks side-effects on shared state.
var elixirMutationRe = regexp.MustCompile(
	`\bAgent\s*\.\s*(?:update|update!|cast|get_and_update|get_and_update!)\s*\(` +
		`|\bGenServer\s*\.\s*(?:cast|call)\s*\(` +
		`|\b:ets\s*\.\s*(?:insert|insert_new|update_element|update_counter|delete|delete_all_objects|delete_object|new)\s*\(` +
		`|\b:persistent_term\s*\.\s*(?:put|erase)\s*\(` +
		`|\bProcess\s*\.\s*(?:put|delete)\s*\(`,
)

func sniffEffectsElixir(content string) []EffectMatch {
	if content == "" {
		return nil
	}
	headers := scanElixirFuncHeaders(content)
	var out []EffectMatch
	out = appendElixirMatches(out, content, headers, elixirHTTPRe, EffectHTTPOut, "HTTPoison/Tesla/Finch/Req", 1.0)
	out = appendElixirMatches(out, content, headers, elixirDBReadRe, EffectDBRead, "Repo.read", 0.9)
	out = appendElixirMatches(out, content, headers, elixirRawSelectRe, EffectDBRead, "SQL.query(SELECT)", 1.0)
	out = appendElixirMatches(out, content, headers, elixirDBWriteRe, EffectDBWrite, "Repo.write", 0.9)
	out = appendElixirMatches(out, content, headers, elixirRawWriteRe, EffectDBWrite, "SQL.query(WRITE)", 1.0)
	out = appendElixirMatches(out, content, headers, elixirFSReadRe, EffectFSRead, "File.read/IO.read", 1.0)
	out = appendElixirMatches(out, content, headers, elixirFSWriteRe, EffectFSWrite, "File.write/File.cp", 1.0)
	out = appendElixirMatches(out, content, headers, elixirProcessRe, EffectFSWrite, "System.cmd/Port.open", 0.9)
	out = appendElixirMatches(out, content, headers, elixirMutationRe, EffectMutation, "Agent/GenServer/:ets", 0.85)
	return out
}

func scanElixirFuncHeaders(content string) []funcHeader {
	var hs []funcHeader
	for _, m := range elixirFuncHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		hs = append(hs, funcHeader{Line: lineOfOffset(content, m[0]), Name: content[m[2]:m[3]]})
	}
	return hs
}

func appendElixirMatches(out []EffectMatch, content string, headers []funcHeader, re *regexp.Regexp, eff Effect, sink string, conf float64) []EffectMatch {
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
