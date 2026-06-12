// F# effect-sink sniffer (#4941 — db_effect for the F# data stack).
//
// Builds on the F# coverage from #4906 (base extractor + Giraffe/Saturn
// routing + testmap). #4906 shipped NO db_effect coverage; this sniffer
// adds data-access classification for the high-value F# data drivers.
//
// Recognises F# data-access sink primitives:
//
//   - db_read   :
//       * EF Core (F#): DbSet LINQ reads — ctx.Users.Find/Where/Single/
//         First/FirstOrDefault/Any/Count/ToList/ToListAsync/AsNoTracking
//         (...Async variants), and the F# `query { for x in ctx.T ... }`
//         computation expression (fsharpEFQueryCERe).
//       * Dapper / Dapper.FSharp: conn.Query/QueryAsync/QueryFirst*/
//         QuerySingle*/QueryMultiple* and Dapper.FSharp `select { ... }`
//         CE + conn.SelectAsync<T>.
//       * Npgsql.FSharp: a `Sql.query "SELECT ..."` literal (classified by
//         the leading SQL verb — fsharpNpgsqlReadRe).
//   - db_write  :
//       * EF Core (F#): ctx.SaveChanges()/SaveChangesAsync(),
//         ctx.Users.Add/AddAsync/AddRange/Update/UpdateRange/Remove/
//         RemoveRange/ExecuteUpdate/ExecuteDelete.
//       * Dapper / Dapper.FSharp: conn.Execute/ExecuteAsync/ExecuteScalar*
//         and Dapper.FSharp `insert`/`update`/`delete` CEs +
//         conn.InsertAsync/UpdateAsync/DeleteAsync.
//       * Npgsql.FSharp: `Sql.query "INSERT|UPDATE|DELETE|..."` literal
//         (write SQL verb — fsharpNpgsqlWriteRe).
//   - http_out  : System.Net.Http HttpClient.GetAsync/PostAsync/PutAsync/
//     PatchAsync/DeleteAsync/SendAsync/GetStringAsync/GetByteArrayAsync,
//     and FsHttp `http { GET ... }` / Http.get|post helpers.
//
// Function attribution uses F# `let [rec] name` / `member [this|_|x].Name`
// declaration headers (the same shapes the #4906 base extractor names as
// SCOPE.Operation). F# is off-side-rule; nearestHeader binds each sink to
// its nearest preceding declaration by line, matching the Crystal/Dart
// precedent. Table attribution (ACCESSES_TABLE) is out of scope for the
// sink sniffer — it emits the standard db_read/db_write/http_out effects,
// consumed downstream by internal/links/effect_propagation.go (mirrors
// every other language's effect sink).
package substrate

import "regexp"

func init() { RegisterEffectSniffer("fsharp", sniffEffectsFSharp) }

// fsharpFuncHeaderRe matches F# `let`/`member` declaration headers.
// Capture group 1 is the binding/member name. Covers:
//
//	let name ...        / let rec name ...   / let inline name ...
//	let private name    / let mutable name
//	member this.Name    / member _.Name      / member x.Name
//	member val Name     / override this.Name / abstract member Name
var fsharpFuncHeaderRe = regexp.MustCompile(
	`(?m)^\s*(?:(?:override|abstract|default|static|private|internal|public)\s+)*` +
		`(?:let(?:\s+(?:rec|inline|mutable|private))*\s+([A-Za-z_][\w']*)` +
		`|member(?:\s+val)?\s+(?:[A-Za-z_][\w']*\s*\.\s*)?([A-Za-z_][\w']*))`,
)

// fsharpHTTPRe matches outbound HTTP primitives (System.Net.Http + FsHttp).
var fsharpHTTPRe = regexp.MustCompile(
	`\b(?:client|_client|httpClient|http)\s*\.\s*` +
		`(?:GetAsync|PostAsync|PutAsync|PatchAsync|DeleteAsync|SendAsync|` +
		`GetStringAsync|GetByteArrayAsync|GetStreamAsync)\s*\(` +
		`|\bHttp\s*\.\s*(?:get|post|put|patch|delete|request)\b` +
		`|\bhttp\s*\{\s*(?:GET|POST|PUT|PATCH|DELETE)\b`,
)

// fsharpEFReadRe matches EF Core (F#) DbSet LINQ read primitives.
// dbSet is the DbSet member access `ctx.Users.` / `db.Orders.` etc.
var fsharpEFReadRe = regexp.MustCompile(
	`\b[A-Za-z_][\w']*\s*\.\s*[A-Z][\w']*\s*\.\s*` +
		`(?:Find|FindAsync|Where|Single|SingleAsync|SingleOrDefault|SingleOrDefaultAsync|` +
		`First|FirstAsync|FirstOrDefault|FirstOrDefaultAsync|Any|AnyAsync|All|` +
		`Count|CountAsync|LongCount|ToList|ToListAsync|ToArray|ToArrayAsync|` +
		`AsNoTracking|Include|Select|OrderBy|FromSqlRaw|FromSqlInterpolated)\s*\(`,
)

// fsharpEFQueryCERe matches the F# `query { for x in ctx.Table ... }` CE.
var fsharpEFQueryCERe = regexp.MustCompile(
	`\bquery\s*\{\s*for\b`,
)

// fsharpEFWriteRe matches EF Core (F#) write primitives.
var fsharpEFWriteRe = regexp.MustCompile(
	`\b[A-Za-z_][\w']*\s*\.\s*(?:SaveChanges|SaveChangesAsync)\s*\(` +
		`|\b[A-Za-z_][\w']*\s*\.\s*[A-Z][\w']*\s*\.\s*` +
		`(?:Add|AddAsync|AddRange|AddRangeAsync|Update|UpdateRange|` +
		`Remove|RemoveRange|ExecuteUpdate|ExecuteUpdateAsync|` +
		`ExecuteDelete|ExecuteDeleteAsync)\s*\(`,
)

// fsharpDapperReadRe matches Dapper / Dapper.FSharp read primitives.
var fsharpDapperReadRe = regexp.MustCompile(
	`\b(?:conn|connection|db|_conn|_db|cnn)\s*\.\s*` +
		`(?:Query|QueryAsync|QueryFirst|QueryFirstAsync|QueryFirstOrDefault|` +
		`QueryFirstOrDefaultAsync|QuerySingle|QuerySingleAsync|` +
		`QuerySingleOrDefault|QuerySingleOrDefaultAsync|QueryMultiple|` +
		`QueryMultipleAsync|SelectAsync|GetAsync|GetListAsync)\b` +
		`|\bselect\s*\{\s*(?:for\b|table\b)`,
)

// fsharpDapperWriteRe matches Dapper / Dapper.FSharp write primitives.
var fsharpDapperWriteRe = regexp.MustCompile(
	`\b(?:conn|connection|db|_conn|_db|cnn)\s*\.\s*` +
		`(?:Execute|ExecuteAsync|ExecuteScalar|ExecuteScalarAsync|` +
		`ExecuteReader|ExecuteReaderAsync|InsertAsync|UpdateAsync|` +
		`DeleteAsync)\b` +
		`|\b(?:insert|update|delete)\s*\{\s*(?:for\b|into\b|table\b)`,
)

// fsharpNpgsqlReadRe matches Npgsql.FSharp `Sql.query "SELECT|WITH ..."`.
var fsharpNpgsqlReadRe = regexp.MustCompile(
	`\bSql\s*\.\s*query\s+(?:@?"""|@?")\s*(?i:SELECT|WITH)\b`,
)

// fsharpNpgsqlWriteRe matches Npgsql.FSharp `Sql.query "INSERT|UPDATE|..."`.
var fsharpNpgsqlWriteRe = regexp.MustCompile(
	`\bSql\s*\.\s*query\s+(?:@?"""|@?")\s*(?i:INSERT|UPDATE|DELETE|CREATE|DROP|ALTER|TRUNCATE|MERGE|UPSERT)\b`,
)

func sniffEffectsFSharp(content string) []EffectMatch {
	if content == "" {
		return nil
	}
	headers := scanFSharpEffectHeaders(content)
	var out []EffectMatch
	out = appendFSharpMatches(out, content, headers, fsharpHTTPRe, EffectHTTPOut, "HttpClient/FsHttp", 0.95)
	out = appendFSharpMatches(out, content, headers, fsharpEFReadRe, EffectDBRead, "efcore.dbset.read", 0.85)
	out = appendFSharpMatches(out, content, headers, fsharpEFQueryCERe, EffectDBRead, "efcore.query-ce", 0.8)
	out = appendFSharpMatches(out, content, headers, fsharpEFWriteRe, EffectDBWrite, "efcore.write", 0.85)
	out = appendFSharpMatches(out, content, headers, fsharpDapperReadRe, EffectDBRead, "dapper.read", 0.85)
	out = appendFSharpMatches(out, content, headers, fsharpDapperWriteRe, EffectDBWrite, "dapper.write", 0.85)
	out = appendFSharpMatches(out, content, headers, fsharpNpgsqlReadRe, EffectDBRead, "npgsql.fsharp.read", 0.9)
	out = appendFSharpMatches(out, content, headers, fsharpNpgsqlWriteRe, EffectDBWrite, "npgsql.fsharp.write", 0.9)
	return out
}

func scanFSharpEffectHeaders(content string) []funcHeader {
	var hs []funcHeader
	for _, m := range fsharpFuncHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		// Group 1 = let-binding name, group 2 = member name; exactly one fires.
		name := ""
		if m[2] >= 0 {
			name = content[m[2]:m[3]]
		} else if m[4] >= 0 {
			name = content[m[4]:m[5]]
		}
		if name == "" {
			continue
		}
		hs = append(hs, funcHeader{Line: lineOfOffset(content, m[0]), Name: name})
	}
	return hs
}

func appendFSharpMatches(out []EffectMatch, content string, headers []funcHeader, re *regexp.Regexp, eff Effect, sink string, conf float64) []EffectMatch {
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
