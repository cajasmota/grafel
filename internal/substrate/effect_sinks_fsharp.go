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
//       * SQLProvider (#4999): the `query { for x in ctx.Dbo.T ... }` CE
//         (shared with fsharpEFQueryCERe) plus a direct table enumeration
//         `ctx.Dbo.T |> Seq.toList` (fsharpSQLProviderReadRe).
//   - db_write  :
//       * EF Core (F#): ctx.SaveChanges()/SaveChangesAsync(),
//         ctx.Users.Add/AddAsync/AddRange/Update/UpdateRange/Remove/
//         RemoveRange/ExecuteUpdate/ExecuteDelete.
//       * Dapper / Dapper.FSharp: conn.Execute/ExecuteAsync/ExecuteScalar*
//         and Dapper.FSharp `insert`/`update`/`delete` CEs +
//         conn.InsertAsync/UpdateAsync/DeleteAsync.
//       * Npgsql.FSharp: `Sql.query "INSERT|UPDATE|DELETE|..."` literal
//         (write SQL verb — fsharpNpgsqlWriteRe).
//       * SQLProvider (#4999): ctx.SubmitUpdates()/SubmitUpdatesAsync(),
//         the table ``.Create``(...) row factory, and row.Delete()
//         (fsharpSQLProviderWriteRe). Best-effort `ctx.Schema.Table`
//         attribution is folded into the Sink tag.
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

// fsharpDapperReceiverNames is the static receiver-name heuristic for Dapper
// connections (the conventional names an IDbConnection binding carries). It is
// the FALLBACK; receiver-type resolution (fsharpDapperTypedReceivers) augments
// it at sniff time with any binding statically typed/constructed as a
// (System.)Data IDbConnection so differently-named connections are also caught.
const fsharpDapperReceiverNames = `conn|connection|db|_conn|_db|cnn|dbConn|dbConnection|_connection`

// fsharpDapperUnambiguousReadVerbs are the Dapper methods whose name alone
// fixes them as a read (Query*/Select*/Get*). These never run write SQL.
const fsharpDapperUnambiguousReadVerbs = `Query|QueryAsync|QueryFirst|QueryFirstAsync|QueryFirstOrDefault|` +
	`QueryFirstOrDefaultAsync|QuerySingle|QuerySingleAsync|` +
	`QuerySingleOrDefault|QuerySingleOrDefaultAsync|QueryMultiple|` +
	`QueryMultipleAsync|SelectAsync|GetAsync|GetListAsync`

// fsharpDapperUnambiguousWriteVerbs are the Dapper.FSharp CRUD helpers whose
// name alone fixes them as a write (Insert/Update/Delete extension methods).
const fsharpDapperUnambiguousWriteVerbs = `InsertAsync|UpdateAsync|DeleteAsync`

// fsharpDapperAmbiguousVerbs are the Dapper methods that run ARBITRARY SQL and
// so are read-or-write depending on the verb of the SQL string argument:
// Execute/ExecuteAsync (stored-proc reads AND DML writes), and ExecuteScalar/
// ExecuteReader (often a SELECT count/aggregate, but can wrap any statement).
const fsharpDapperAmbiguousVerbs = `Execute|ExecuteAsync|ExecuteScalar|ExecuteScalarAsync|` +
	`ExecuteReader|ExecuteReaderAsync`

// fsharpDapperReadRe matches Dapper / Dapper.FSharp NAME-UNAMBIGUOUS read
// primitives (the Query*/Select*/Get* family) plus the Dapper.FSharp
// `select { ... }` CE. The receiver alternation is rendered at sniff time so
// type-resolved receivers can be folded in (see sniffEffectsFSharp).
var fsharpDapperReadRe = regexp.MustCompile(
	`\b(?:` + fsharpDapperReceiverNames + `)\s*\.\s*` +
		`(?:` + fsharpDapperUnambiguousReadVerbs + `)\b` +
		`|\bselect\s*\{\s*(?:for\b|table\b)`,
)

// fsharpDapperWriteRe matches Dapper / Dapper.FSharp NAME-UNAMBIGUOUS write
// primitives (Insert/Update/Delete extension methods) plus the Dapper.FSharp
// `insert`/`update`/`delete` CEs. The ambiguous Execute* family is handled
// separately by fsharpDapperExecuteRe + leading-verb inspection.
var fsharpDapperWriteRe = regexp.MustCompile(
	`\b(?:` + fsharpDapperReceiverNames + `)\s*\.\s*` +
		`(?:` + fsharpDapperUnambiguousWriteVerbs + `)\b` +
		`|\b(?:insert|update|delete)\s*\{\s*(?:for\b|into\b|table\b)`,
)

// fsharpDapperExecuteRe matches the AMBIGUOUS Dapper Execute* family. Group 1
// captures the first string-literal argument (`@?"""..."""` / `@?"..."`) when
// present, so the leading SQL verb can be inspected to classify read vs write.
// The receiver alternation is rendered at sniff time (type-resolved receivers
// folded in). When no inspectable literal is present the call defaults to a
// write (conservative — DML is the common Execute use and over-recording a
// write is safer than missing one).
var fsharpDapperExecuteRe = regexp.MustCompile(
	`\b(?:` + fsharpDapperReceiverNames + `)\s*\.\s*` +
		`(?:` + fsharpDapperAmbiguousVerbs + `)\s*` +
		`(?:<[^>]*>)?\s*\(\s*(?:@?"""(?s:(.*?))"""|@?"((?:[^"\\]|\\.)*)")?`,
)

// fsharpSQLReadVerbRe matches a SQL statement whose leading verb is a read
// (SELECT / WITH ... SELECT). Leading whitespace/comments are tolerated.
var fsharpSQLReadVerbRe = regexp.MustCompile(
	`(?is)^\s*(?:--[^\n]*\n\s*|/\*.*?\*/\s*)*(?:SELECT|WITH)\b`,
)

// fsharpDapperReceiverTypeRe resolves receiver-type bindings: an F# parameter
// or value statically annotated as a (System.Data) IDbConnection-family type,
// or constructed via `new SqlConnection(...)` / `new NpgsqlConnection(...)`.
// Group 1 / group 2 capture the bound NAME so differently-named connections
// (e.g. `database`, `pg`, `sqlite`) are recognised regardless of the static
// name heuristic. Recognised types: IDbConnection and the concrete ADO.NET
// connection classes (SqlConnection, NpgsqlConnection, SqliteConnection,
// MySqlConnection, OracleConnection, DbConnection, SqliteConnection).
var fsharpDapperReceiverTypeRe = regexp.MustCompile(
	`\(\s*([A-Za-z_][\w']*)\s*:\s*(?:[\w.]+\.)?` + fsharpDapperConnTypes + `\b` +
		`|\b(?:let|use)\s+(?:mutable\s+)?([A-Za-z_][\w']*)\s*` +
		`(?::\s*(?:[\w.]+\.)?` + fsharpDapperConnTypes + `\b[^=]*)?` +
		`=\s*(?:new\s+)?(?:[\w.]+\.)?` + fsharpDapperConnTypes + `\s*\(`,
)

// fsharpDapperConnTypes is the IDbConnection-family type alternation.
const fsharpDapperConnTypes = `(?:IDbConnection|DbConnection|SqlConnection|NpgsqlConnection|` +
	`SqliteConnection|SQLiteConnection|MySqlConnection|MySqlConnector\.MySqlConnection|` +
	`OracleConnection|OdbcConnection|OleDbConnection|FbConnection)`

// SQLProvider (F# type provider) recognition (#4999, follow-up #4941).
//
// SQLProvider exposes an erased data context whose tables are reached via
// `ctx.Dbo.TableName` (the leading segment after the context is the SQL
// schema — Dbo/Public/Main/etc.). There is no stable static call shape on
// the provided types, so the provider's idiomatic surface is matched
// syntactically:
//
//   - db_read  : the generic F# `query { for x in ctx.Dbo.T ... }` CE is
//     already caught by fsharpEFQueryCERe. In addition a direct table
//     enumeration `ctx.Dbo.TableName |> Seq.toList` / `|> Seq.map` /
//     `|> List.ofSeq` (materialising the erased IQueryable) is a read
//     (fsharpSQLProviderReadRe).
//   - db_write : the provider commits via `ctx.SubmitUpdates()` /
//     `ctx.SubmitUpdatesAsync()`; rows are inserted with the table's
//     ``.Create``(...) / `.Create(...)` factory and removed with
//     `row.Delete()` (fsharpSQLProviderWriteRe).
//
// Table attribution is best-effort: fsharpSQLProviderTableRe extracts the
// `ctx.Schema.TableName` table segment so the matched read/write carries a
// candidate table in its Sink tag (`sqlprovider.read:Users`). ACCESSES_TABLE
// wiring stays a separate concern, as for every other sink language (see the
// package note above).

// fsharpSQLProviderReadRe matches a direct SQLProvider table enumeration
// (`ctx.Dbo.Users |> Seq.toList` and friends). The `query { for ... }` read
// path is already covered by fsharpEFQueryCERe, so this only adds the direct
// pipe-to-collection-combinator materialisation shape.
var fsharpSQLProviderReadRe = regexp.MustCompile(
	`\b[A-Za-z_][\w']*\s*\.\s*(?:Dbo|Public|Main|dbo|public|main)\s*\.\s*[A-Z][\w']*\s*` +
		`\|>\s*(?:Seq|List|Array)\s*\.\s*` +
		`(?:toList|toArray|ofSeq|map|filter|tryHead|head|find|tryFind|length|isEmpty|iter|fold|sortBy)\b`,
)

// fsharpSQLProviderWriteRe matches SQLProvider commit / row-mutation
// primitives: ctx.SubmitUpdates()/SubmitUpdatesAsync(), the table
// ``.Create``(...)/.Create(...) row factory, and `row.Delete()`.
var fsharpSQLProviderWriteRe = regexp.MustCompile(
	`\b[A-Za-z_][\w']*\s*\.\s*(?:SubmitUpdates|SubmitUpdatesAsync)\s*\(` +
		"|\\b[A-Za-z_][\\w']*\\s*\\.\\s*(?:Dbo|Public|Main|dbo|public|main)\\s*\\.\\s*[A-Z][\\w']*\\s*\\.\\s*(?:`Create`|Create)\\s*\\(" +
		`|\b[A-Za-z_][\w']*\s*\.\s*Delete\s*\(\s*\)`,
)

// fsharpSQLProviderTableRe extracts the `ctx.Schema.TableName` table
// segment for best-effort table attribution. Group 1 = schema, group 2 =
// table. Schema is restricted to the common SQLProvider schema segments so
// arbitrary `A.B.C` member chains do not false-match.
var fsharpSQLProviderTableRe = regexp.MustCompile(
	`\b[A-Za-z_][\w']*\s*\.\s*(?:Dbo|Public|Main|dbo|public|main)\s*\.\s*([A-Z][\w']*)`,
)

// fsharpNpgsqlReadRe matches Npgsql.FSharp `Sql.query "SELECT|WITH ..."`.
var fsharpNpgsqlReadRe = regexp.MustCompile(
	`\bSql\s*\.\s*query\s+(?:@?"""|@?")\s*(?i:SELECT|WITH)\b`,
)

// fsharpNpgsqlWriteRe matches Npgsql.FSharp `Sql.query "INSERT|UPDATE|..."`.
var fsharpNpgsqlWriteRe = regexp.MustCompile(
	`\bSql\s*\.\s*query\s+(?:@?"""|@?")\s*(?i:INSERT|UPDATE|DELETE|CREATE|DROP|ALTER|TRUNCATE|MERGE|UPSERT)\b`,
)

// fsharpResolveDapperReceivers returns the per-call regexes for the Dapper
// read / write / Execute primitives, with the receiver-name alternation
// EXTENDED by any binding statically resolved to an IDbConnection-family type
// (#5001). When no typed receivers are present the package-level regexes are
// reused (no allocation). This drops the reliance on the bare name heuristic
// so differently-named connections are also classified.
func fsharpResolveDapperReceivers(content string) (readRe, writeRe, execRe *regexp.Regexp) {
	extra := fsharpTypedDapperReceiverNames(content)
	if len(extra) == 0 {
		return fsharpDapperReadRe, fsharpDapperWriteRe, fsharpDapperExecuteRe
	}
	recv := fsharpDapperReceiverNames
	for _, n := range extra {
		recv += `|` + regexp.QuoteMeta(n)
	}
	readRe = regexp.MustCompile(
		`\b(?:` + recv + `)\s*\.\s*` +
			`(?:` + fsharpDapperUnambiguousReadVerbs + `)\b` +
			`|\bselect\s*\{\s*(?:for\b|table\b)`,
	)
	writeRe = regexp.MustCompile(
		`\b(?:` + recv + `)\s*\.\s*` +
			`(?:` + fsharpDapperUnambiguousWriteVerbs + `)\b` +
			`|\b(?:insert|update|delete)\s*\{\s*(?:for\b|into\b|table\b)`,
	)
	execRe = regexp.MustCompile(
		`\b(?:` + recv + `)\s*\.\s*` +
			`(?:` + fsharpDapperAmbiguousVerbs + `)\s*` +
			`(?:<[^>]*>)?\s*\(\s*(?:@?"""(?s:(.*?))"""|@?"((?:[^"\\]|\\.)*)")?`,
	)
	return readRe, writeRe, execRe
}

// fsharpTypedDapperReceiverNames scans for IDbConnection-family bindings whose
// name is NOT already in the static heuristic, returning the de-duplicated
// extra names to fold into the receiver alternation.
func fsharpTypedDapperReceiverNames(content string) []string {
	seen := map[string]bool{
		"conn": true, "connection": true, "db": true, "_conn": true,
		"_db": true, "cnn": true, "dbConn": true, "dbConnection": true,
		"_connection": true,
	}
	var out []string
	for _, m := range fsharpDapperReceiverTypeRe.FindAllStringSubmatch(content, -1) {
		name := m[1]
		if name == "" {
			name = m[2]
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

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
	dapperReadRe, dapperWriteRe, dapperExecRe := fsharpResolveDapperReceivers(content)
	out = appendFSharpMatches(out, content, headers, dapperReadRe, EffectDBRead, "dapper.read", 0.85)
	out = appendFSharpMatches(out, content, headers, dapperWriteRe, EffectDBWrite, "dapper.write", 0.85)
	// Dapper ambiguous Execute* family (#5001): classify read vs write by the
	// leading SQL verb of the string-literal argument; type-resolved receivers
	// (#5001) extend the static name heuristic so differently-named
	// IDbConnection bindings (`database`, `pg`, ...) are also caught.
	out = appendFSharpDapperExecuteMatches(out, content, headers, dapperExecRe)
	out = appendFSharpMatches(out, content, headers, fsharpNpgsqlReadRe, EffectDBRead, "npgsql.fsharp.read", 0.9)
	out = appendFSharpMatches(out, content, headers, fsharpNpgsqlWriteRe, EffectDBWrite, "npgsql.fsharp.write", 0.9)
	// SQLProvider type-provider (#4999): direct table enumeration -> db_read,
	// SubmitUpdates/.Create/.Delete() -> db_write, each with best-effort
	// `ctx.Schema.Table` attribution folded into the Sink tag.
	out = appendFSharpSQLProviderMatches(out, content, headers, fsharpSQLProviderReadRe, EffectDBRead, "sqlprovider.read", 0.75)
	out = appendFSharpSQLProviderMatches(out, content, headers, fsharpSQLProviderWriteRe, EffectDBWrite, "sqlprovider.write", 0.75)
	return out
}

// appendFSharpSQLProviderMatches is appendFSharpMatches with best-effort
// SQLProvider table attribution: when the matched line carries a
// `ctx.Schema.TableName` segment, the resolved table name is appended to the
// Sink tag (`sqlprovider.read:Users`). SQLProvider provided types are erased,
// so the table is a best-effort hint, not a resolved entity (honest-partial).
func appendFSharpSQLProviderMatches(out []EffectMatch, content string, headers []funcHeader, re *regexp.Regexp, eff Effect, sink string, conf float64) []EffectMatch {
	for _, m := range re.FindAllStringIndex(content, -1) {
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		s := sink
		if tbl := fsharpSQLProviderTableOnLine(content, m[0]); tbl != "" {
			s = sink + ":" + tbl
		}
		out = append(out, EffectMatch{
			Function:   fn,
			Line:       line,
			Effect:     eff,
			Sink:       s,
			Confidence: conf,
		})
	}
	return out
}

// appendFSharpDapperExecuteMatches classifies the ambiguous Dapper Execute*
// family (#5001). The string-literal argument's leading SQL verb decides the
// effect: SELECT / WITH (... SELECT) -> db_read; any other (or no inspectable)
// literal -> db_write. A read carries higher confidence (the verb is explicit);
// a defaulted write (no literal to inspect — e.g. a SQL value bound elsewhere)
// drops confidence to reflect the heuristic fallback. The Sink tag records the
// classification basis (`dapper.execute.read` / `.write` / `.write?`).
func appendFSharpDapperExecuteMatches(out []EffectMatch, content string, headers []funcHeader, execRe *regexp.Regexp) []EffectMatch {
	for _, m := range execRe.FindAllStringSubmatchIndex(content, -1) {
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		// Group 1 = triple-quoted literal body, group 2 = quoted literal body.
		lit := ""
		if m[2] >= 0 {
			lit = content[m[2]:m[3]]
		} else if m[4] >= 0 {
			lit = content[m[4]:m[5]]
		}
		eff := EffectDBWrite
		sink := "dapper.execute.write"
		conf := 0.85
		switch {
		case lit == "":
			// No inspectable literal (SQL bound elsewhere / a stored proc
			// name): default to write, but flag the lower-confidence guess.
			sink = "dapper.execute.write?"
			conf = 0.7
		case fsharpSQLReadVerbRe.MatchString(lit):
			eff = EffectDBRead
			sink = "dapper.execute.read"
			conf = 0.9
		}
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

// fsharpSQLProviderTableOnLine returns the best-effort `ctx.Schema.Table`
// table name for the source line containing offset off, or "" when none is
// present (e.g. a `ctx.SubmitUpdates()` commit with no table on the line).
func fsharpSQLProviderTableOnLine(content string, off int) string {
	start := off
	for start > 0 && content[start-1] != '\n' {
		start--
	}
	end := off
	for end < len(content) && content[end] != '\n' {
		end++
	}
	if mm := fsharpSQLProviderTableRe.FindStringSubmatch(content[start:end]); mm != nil {
		return mm[1]
	}
	return ""
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
