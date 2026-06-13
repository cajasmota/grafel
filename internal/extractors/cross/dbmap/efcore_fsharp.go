// Package dbmap — EF Core (F#) DbSet table attribution (#5106, follow-up #5000).
//
// #5000 wired the F# RAW-SQL drivers (Npgsql.FSharp `Sql.query`, Dapper string
// SQL) through the shared raw-SQL table extractor. It explicitly DEFERRED the
// SECOND axis to this ticket: EF Core (F#) DbSet reads/writes name the table
// via the DbSet MEMBER (`ctx.Users.Where(...)`, the `query { for u in ctx.Users
// ... }` CE, `ctx.Users.Add(...)`), NOT via a SQL string literal, so they never
// flow through detectRawSQL.
//
// detectEFCoreFSharp closes that axis. It mirrors the EF Core C# convention
// (DbSet property NAME == table name) used in internal/custom/csharp/ef_core.go
// and the read/write LINQ call shapes already recognised by the per-language F#
// substrate sniffer (fsharpEFReadRe / fsharpEFQueryCERe / fsharpEFWriteRe in
// internal/substrate/effect_sinks_fsharp.go):
//
//  1. Collect DbSet members declared on the F# DbContext type:
//     `[<DefaultValue>] val mutable Users : DbSet<User>`,
//     `member val Users : DbSet<User>` (and `member val Users = ... : DbSet<User>`),
//     plain `member this.Users : DbSet<User>`. The member name is the default
//     table (EF Core convention); the entity type `User` carries any override.
//  2. Resolve table overrides: a `[<Table("tbl")>]` attribute on the entity type
//     declaration, or a Fluent `modelBuilder.Entity<User>().ToTable("tbl")` /
//     `.ToTable("tbl")` call in OnModelCreating. Override-by-entity wins over the
//     member-name convention.
//  3. Attribute accesses:
//     - `ctx.Users.<readVerb>(` -> SELECT
//     - `ctx.Users.<writeVerb>(` -> INSERT/UPDATE/DELETE
//     - `query { for x in ctx.Users do ... }` -> SELECT
//     each emitting a SCOPE.DataAccess + ACCESSES_TABLE edge (orm "efcore_fsharp"),
//     reusing the same builder/edge kinds as every other ORM.
//
// Import gate: `open Microsoft.EntityFrameworkCore` (importHints
// "microsoft.entityframeworkcore"), so non-EF F# files are untouched.
package dbmap

import "regexp"

// efFSharpDbSetDeclRE matches an F# DbSet member/field declaration on the
// DbContext type and captures (1) the member name and (2) the entity type
// inside DbSet<...>. It tolerates the common F# declaration forms:
//
//	[<DefaultValue>] val mutable Users : DbSet<User>
//	val mutable Users : DbSet<User>
//	member val Users : DbSet<User> = ... with get, set
//	member val Users = ... : DbSet<User>
//	member this.Users : DbSet<User>
//	member _.Users with get () : DbSet<User> = ...
//
// We require `DbSet<Entity>` to appear; everything between the member name and
// the `DbSet<` token is skipped (`[^=\n]*` keeps the match on one logical decl
// and avoids leaking across line/expression boundaries).
var efFSharpDbSetDeclRE = regexp.MustCompile(
	`(?m)\b(?:val(?:\s+mutable)?|member(?:\s+val)?(?:\s+(?:this|_|[A-Za-z_][\w']*))?\s*\.?)\s*` +
		`([A-Z][\w']*)\b[^=\n]*?\bDbSet\s*<\s*([A-Za-z_][\w'.]*)\s*>`,
)

// efFSharpTableAttrRE matches a `[<Table("name")>]` attribute immediately
// preceding an entity `type Name =` declaration, capturing (1) the table
// override and (2) the entity type name it annotates. F# attributes sit on the
// line(s) above the `type` keyword.
var efFSharpTableAttrRE = regexp.MustCompile(
	`(?s)\[<\s*Table\s*\(\s*"([^"]+)"[^)]*\)\s*>\]\s*type\s+([A-Za-z_][\w']*)`,
)

// efFSharpToTableRE matches a Fluent `.Entity<User>().ToTable("name")` /
// `modelBuilder.Entity<User>(...).ToTable("name")` override inside
// OnModelCreating, capturing (1) the entity type and (2) the table override.
// The `[^"]*?` between Entity<...> and ToTable tolerates intervening fluent
// calls on the same chain.
var efFSharpToTableRE = regexp.MustCompile(
	`(?s)\bEntity\s*<\s*([A-Za-z_][\w'.]*)\s*>\s*\([^)]*\)[^"\n]*?\.\s*ToTable\s*\(\s*"([^"]+)"`,
)

// efFSharpReadVerbs are the DbSet LINQ read terminals/operators (mirrors
// fsharpEFReadRe in the substrate sniffer). A `ctx.Set.<verb>(` hit is a SELECT.
const efFSharpReadVerbs = `Find|FindAsync|Where|Single|SingleAsync|SingleOrDefault|SingleOrDefaultAsync|` +
	`First|FirstAsync|FirstOrDefault|FirstOrDefaultAsync|Any|AnyAsync|All|` +
	`Count|CountAsync|LongCount|ToList|ToListAsync|ToArray|ToArrayAsync|` +
	`AsNoTracking|Include|Select|OrderBy|OrderByDescending|FromSqlRaw|FromSqlInterpolated`

// efFSharpWriteVerbs split by SQL op (mirrors fsharpEFWriteRe).
const (
	efFSharpInsertVerbs = `Add|AddAsync|AddRange|AddRangeAsync`
	efFSharpUpdateVerbs = `Update|UpdateRange|ExecuteUpdate|ExecuteUpdateAsync`
	efFSharpDeleteVerbs = `Remove|RemoveRange|ExecuteDelete|ExecuteDeleteAsync`
)

// efFSharpAccessRE matches a `<recv>.<Set>.<verb>(` DbSet member access where
// <verb> is any EF read or write terminal. Captures (1) the DbSet member name
// and (2) the verb. The receiver (ctx/db/_db/...) is matched but not captured.
var efFSharpAccessRE = regexp.MustCompile(
	`\b[A-Za-z_][\w']*\s*\.\s*([A-Z][\w']*)\s*\.\s*(` +
		efFSharpReadVerbs + `|` +
		efFSharpInsertVerbs + `|` + efFSharpUpdateVerbs + `|` + efFSharpDeleteVerbs +
		`)\s*\(`,
)

// efFSharpQueryCERE matches the F# `query { for x in <recv>.<Set> ... }` CE and
// captures the DbSet member name. This is the idiomatic F# LINQ read shape; it
// names the table via the DbSet member, not a SQL string.
var efFSharpQueryCERE = regexp.MustCompile(
	`\bquery\s*\{\s*for\s+[A-Za-z_][\w']*\s+in\s+[A-Za-z_][\w']*\s*\.\s*([A-Z][\w']*)\b`,
)

// efFSharpVerbOp maps a captured EF verb to its SQL operation.
func efFSharpVerbOp(verb string) string {
	switch {
	case reMatchAlt(efFSharpInsertVerbs, verb):
		return OpInsert
	case reMatchAlt(efFSharpUpdateVerbs, verb):
		return OpUpdate
	case reMatchAlt(efFSharpDeleteVerbs, verb):
		return OpDelete
	default:
		return OpSelect
	}
}

// reMatchAlt reports whether verb is one of the `|`-separated tokens in alt.
func reMatchAlt(alt, verb string) bool {
	start := 0
	for i := 0; i <= len(alt); i++ {
		if i == len(alt) || alt[i] == '|' {
			if alt[start:i] == verb {
				return true
			}
			start = i + 1
		}
	}
	return false
}

// detectEFCoreFSharp implements the EF Core (F#) DbSet -> ACCESSES_TABLE axis.
func detectEFCoreFSharp(source string) []access {
	// 1. entity-type -> table overrides ([<Table>] attr + Fluent ToTable).
	tableByEntity := map[string]string{}
	for _, m := range efFSharpTableAttrRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 3 {
			tableByEntity[m[2]] = m[1]
		}
	}
	for _, m := range efFSharpToTableRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 3 {
			tableByEntity[lastTypeSegment(m[1])] = m[2]
		}
	}

	// 2. DbSet member -> table. Default = member name (EF convention); an
	//    entity-type override wins.
	tableBySet := map[string]string{}
	for _, m := range efFSharpDbSetDeclRE.FindAllStringSubmatch(source, -1) {
		if len(m) < 3 {
			continue
		}
		member := m[1]
		entity := lastTypeSegment(m[2])
		table := member // EF Core convention: DbSet property name == table.
		if override, ok := tableByEntity[entity]; ok {
			table = override
		}
		tableBySet[member] = table
	}

	if len(tableBySet) == 0 {
		// No DbSet declaration in this file -> nothing to attribute. (A repo
		// that splits the DbContext from its callers is a documented
		// single-file limitation, mirroring the other dbmap ORM detectors.)
		return nil
	}

	var out []access

	// 3a. `ctx.Set.<verb>(` accesses.
	for _, m := range efFSharpAccessRE.FindAllStringSubmatchIndex(source, -1) {
		member := source[m[2]:m[3]]
		verb := source[m[4]:m[5]]
		table, ok := tableBySet[member]
		if !ok {
			continue // member is not a known DbSet -> not a table access.
		}
		out = append(out, access{
			table:         table,
			operation:     efFSharpVerbOp(verb),
			orm:           "efcore_fsharp",
			functionQName: enclosingFunc(source, m[0]),
		})
	}

	// 3b. `query { for x in ctx.Set ... }` CE reads.
	for _, m := range efFSharpQueryCERE.FindAllStringSubmatchIndex(source, -1) {
		member := source[m[2]:m[3]]
		table, ok := tableBySet[member]
		if !ok {
			continue
		}
		out = append(out, access{
			table:         table,
			operation:     OpSelect,
			orm:           "efcore_fsharp",
			functionQName: enclosingFunc(source, m[0]),
		})
	}

	return out
}

// lastTypeSegment returns the final dotted segment of a (possibly namespace-
// qualified) F# type name: `Domain.User` -> `User`.
func lastTypeSegment(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '.' {
			return name[i+1:]
		}
	}
	return name
}
