// JS/TS effect-sink sniffer (#2764 Phase 1A T1).
//
// Recognises the canonical sink primitives that contribute to each
// effect in JavaScript / TypeScript codebases:
//
//   - http_out  : fetch(...), axios.<method>(...), got/ky/superagent
//   - db_read   : Sequelize/TypeORM/Prisma `.find*` / `.findOne` / `.query`,
//     Mongoose/native-driver `.find()` / `.findById()` /
//     `.aggregate()` / `.countDocuments()` / `.distinct()`,
//     knex `.select`
//   - db_write  : `.save`/`.create`/`.update`/`.delete`/`.insert`/`.upsert`,
//     Mongoose/native-driver `.findOneAndUpdate` /
//     `.findByIdAndDelete` / `.replaceOne` / `.bulkWrite`
//   - fs_read   : `fs.readFile*`, `fs.readSync`, `fs.createReadStream`
//   - fs_write  : `fs.writeFile*`, `fs.writeSync`, `fs.createWriteStream`,
//     `fs.appendFile*`, `fs.mkdir*`, `fs.unlink*`
//   - mutation  : `this.<field> = ...` assignment inside a method body
//
// Function attribution is line-based: each match is assigned to the
// nearest enclosing `function` / `=>` / class-method header on a line ≤
// the match's line. This is intentionally syntactic — accurate enough
// for the substrate, with full lexical accuracy deferred to Phase 4.
package substrate

import "regexp"

func init() { RegisterEffectSniffer("jsts", sniffEffectsJSTS) }

// jstsFuncHeaderRe matches function-introducing syntax we attribute sinks to:
//
//	function foo(             // named function declaration
//	async function foo(
//	foo(args) {               // method shorthand in object/class
//	foo = function(           // function-expression assignment
//	foo = (args) => {         // arrow function assignment
//	const foo = (args) =>     // const arrow
//	async foo(args) {         // async class method shorthand
//
// Capture group 1 is the declaring identifier name.
var jstsFuncHeaderRe = regexp.MustCompile(
	`(?m)^\s*(?:export\s+)?(?:default\s+)?` +
		`(?:async\s+)?` +
		`(?:` +
		`function\s*\*?\s+([A-Za-z_$][\w$]*)\s*\(` +
		`|(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s*)?(?:function\b|\([^)]*\)\s*=>|[A-Za-z_$][\w$]*\s*=>)` +
		`|([A-Za-z_$][\w$]*)\s*\(([^)\n]*)\)\s*(?::\s*[A-Za-z_$][\w$<>\[\],\s|&?]+)?\s*\{` +
		`)`,
)

// jstsHTTPRe matches outbound HTTP call sites. Conservative — we want
// the function name on the LHS of the `(` so we don't tag every `fetch`
// substring (e.g. inside a string literal).
var jstsHTTPRe = regexp.MustCompile(
	`\b(?:fetch|axios(?:\s*\.\s*(?:get|post|put|patch|delete|head|options|request))?|` +
		`got(?:\s*\.\s*(?:get|post|put|patch|delete|head))?|ky(?:\s*\.\s*(?:get|post|put|patch|delete|head))?|` +
		`superagent\s*\.\s*(?:get|post|put|patch|delete|head)|` +
		`XMLHttpRequest|navigator\s*\.\s*sendBeacon)\s*\(`,
)

// jstsDBReadRe matches read-flavoured ORM / query-builder primitives.
// Includes Mongoose / native MongoDB driver collection reads — `.findById`,
// `.countDocuments`, `.estimatedDocumentCount`, `.distinct` (#3440 ask 4);
// `.find`/`.findOne`/`.aggregate`/`.count` were already covered.
//
// #4336 adds the Mongoose aggregate-fluent join terminals `.populate(` and
// `.lookup(` — both are distinctive Mongoose/aggregation read names (a
// `Model.find().populate('author')` join or a `Model.aggregate().lookup({...})`
// stage), safe to bare-match. `aggregate` was already covered.
var jstsDBReadRe = regexp.MustCompile(
	`\.\s*(findOne|findAll|findMany|findUnique|findFirst|findById|find|aggregate|lookup|populate|countDocuments|estimatedDocumentCount|count|distinct|exists|query|select|raw|exec)\s*\(`,
)

// jstsDBWriteRe matches write-flavoured ORM / query-builder primitives.
// Includes Mongoose / native MongoDB driver writes — the find-and-modify
// family (`.findOneAndUpdate`, `.findByIdAndUpdate`, `.findOneAndDelete`,
// `.findByIdAndDelete`, `.findOneAndReplace`, `.findByIdAndRemove`),
// `.replaceOne`, `.bulkWrite`, `.remove` (#3440 ask 4); `.save`/`.create`/
// `.insertOne`/`.deleteMany`/... were already covered.
var jstsDBWriteRe = regexp.MustCompile(
	`\.\s*(save|create|createMany|update(?:Many|One)?|upsert|delete(?:Many|One)?|destroy|` +
		`insert(?:Many|One)?|bulkCreate|bulkUpdate|bulkDelete|bulkWrite|replaceOne|remove|` +
		`findOneAndUpdate|findByIdAndUpdate|findOneAndDelete|findByIdAndDelete|findOneAndReplace|findByIdAndRemove)\s*\(`,
)

// jstsFSReadRe matches Node fs / fs/promises read primitives.
var jstsFSReadRe = regexp.MustCompile(
	`\b(?:fs|fsp|fs\s*\.\s*promises)\s*\.\s*(readFile|readFileSync|readdir|readdirSync|createReadStream|stat|statSync|lstat|lstatSync|access|accessSync|open|openSync)\s*\(`,
)

// jstsFSWriteRe matches Node fs / fs/promises write primitives.
var jstsFSWriteRe = regexp.MustCompile(
	`\b(?:fs|fsp|fs\s*\.\s*promises)\s*\.\s*(writeFile|writeFileSync|appendFile|appendFileSync|mkdir|mkdirSync|rmdir|rmdirSync|unlink|unlinkSync|rm|rmSync|rename|renameSync|copyFile|copyFileSync|createWriteStream|chmod|chmodSync|chown|chownSync|truncate|truncateSync|symlink|symlinkSync)\s*\(`,
)

// --- #4335 / #4336 fluent query-builder receiver-typed data-access ---
//
// The receiver-typed read/write discipline established for Python (#4691),
// C# (#4692), Rust (#4737) and Scala-Slick (#4736) extended to the JS/TS
// fluent query builders whose TERMINAL verb determines read vs write but
// whose terminal NAMES collide with plain array/Promise combinators
// (`.getMany()` is fine, but `.first()`/`.execute()`/`.pluck()` would
// false-positive on a non-builder receiver). These ambiguous terminals are
// credited ONLY when chained off a query-builder-typed receiver:
//
//   - TypeORM    `repo.createQueryBuilder('x')` result (#4335)
//   - Prisma     a `prisma.<model>` delegate (#4335)
//   - Knex       a `knex('t')` / `knex.table('t')` builder (#4336)
//
// Distinctive read/write terminals with no collision (`getMany`, `getRawMany`,
// `findMany`, `$queryRaw`, ...) are bare-matched by jstsDBReadRe/jstsDBWriteRe;
// this layer adds the AMBIGUOUS terminals gated to a builder-typed receiver,
// preserving the collection/Promise false-positive guard.

// jstsBuilderRootRe seeds the set of query-builder-typed local/field names.
// Group 1 captures the assigned name. RHS alternatives:
//   - `const qb = repo.createQueryBuilder('u')`        (TypeORM QueryBuilder)
//   - `const qb = this.repo.createQueryBuilder()`
//   - `const qb = conn.createQueryBuilder()`
//   - `const q = knex('users')` / `const q = knex.table('users')`           (Knex)
//   - `const q = prisma.user`                          (Prisma delegate handle)
//   - `const q = this.prisma.user`
var jstsBuilderRootRe = regexp.MustCompile(
	`(?m)(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:await\s+)?(?:` +
		`[A-Za-z_$][\w$.]*\.createQueryBuilder\s*\(` + // TypeORM QueryBuilder
		`|[A-Za-z_$][\w$.]*?knex\s*(?:\(|\.\s*table\s*\()` + // Knex builder
		`|\bknex\s*(?:\(|\.\s*table\s*\()` +
		`|(?:[A-Za-z_$][\w$]*\.)*prisma\s*\.\s*[A-Za-z_$][\w$]*\s*[;\n]` + // Prisma delegate (no call → handle)
		`)`,
)

// jstsBuilderChainAssignRe propagates builder typing across reassignments:
// `const qb2 = qb.where(...)` where qb is already builder-typed. Group 1 =
// new name, group 2 = source receiver. The chain ops are the builder shaper
// verbs (TypeORM/Knex) that RETURN the builder.
var jstsBuilderChainAssignRe = regexp.MustCompile(
	`(?m)(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:await\s+)?([A-Za-z_$][\w$]*)\s*\.\s*` +
		`(?:where|andWhere|orWhere|leftJoin|leftJoinAndSelect|innerJoin|innerJoinAndSelect|` +
		`join|select|addSelect|orderBy|addOrderBy|groupBy|having|limit|offset|skip|take|` +
		`from|into|whereIn|whereNot|whereNull|distinct|with|returning)\s*\(`,
)

// jstsBuilderReadVerbs are the read TERMINALS that are ambiguous with plain
// array/Promise combinators, credited db_read only on a builder-typed receiver.
//   - TypeORM   getMany/getOne/getRawMany/getRawOne/getManyAndCount/getCount/getRawOne
//   - Knex      first/pluck/select (select also shapes, but as a terminal it reads)
const jstsBuilderReadVerbs = `getMany|getOne|getManyAndCount|getRawMany|getRawOne|getRawAndEntities|getCount|getExists|first|pluck|select`

// jstsBuilderWriteVerbs are the write TERMINALS gated to a builder-typed
// receiver — `.execute()` terminates a TypeORM Insert/Update/Delete builder
// and is far too generic to bare-match; `.del()`/`.truncate()` are Knex.
const jstsBuilderWriteVerbs = `execute|del|truncate`

// jstsBuilderInlineReadRe credits an ambiguous read terminal chained DIRECTLY
// off a builder root inline — `repo.createQueryBuilder('u').where(...).getMany()`,
// `knex('t').where(...).first()`, `prisma.user.findFirst()` (findFirst already
// distinctive, but the inline knex/QB forms need this). The root must be a
// createQueryBuilder() call, a knex(...) builder, or a prisma.<model> delegate.
var jstsBuilderInlineReadRe = regexp.MustCompile(
	`(?:[A-Za-z_$][\w$.]*\.createQueryBuilder\s*\([^;]*?\)|` +
		`\bknex\s*(?:\([^;)]*\)|\.\s*table\s*\([^;)]*\))|` +
		`(?:[A-Za-z_$][\w$]*\.)*prisma\s*\.\s*[A-Za-z_$][\w$]*)` +
		`(?:\s*\.\s*[A-Za-z_$][\w$]*\s*\([^;]*?\))*?` +
		`\s*\.\s*(?:` + jstsBuilderReadVerbs + `)\s*\(`,
)

// jstsBuilderInlineWriteRe credits an ambiguous write terminal chained inline
// off a builder root — `repo.createQueryBuilder().update(User).set(...).execute()`,
// `knex('t').where(...).del()`.
var jstsBuilderInlineWriteRe = regexp.MustCompile(
	`(?:[A-Za-z_$][\w$.]*\.createQueryBuilder\s*\([^;]*?\)|` +
		`\bknex\s*(?:\([^;)]*\)|\.\s*table\s*\([^;)]*\)))` +
		`(?:\s*\.\s*[A-Za-z_$][\w$]*\s*\([^;]*?\))*?` +
		`\s*\.\s*(?:` + jstsBuilderWriteVerbs + `)\s*\(`,
)

// jstsPrismaRawRe matches Prisma raw escape hatches — `$queryRaw`/`$queryRawUnsafe`
// (db_read) handled here; `$executeRaw`/`$executeRawUnsafe` are db_write
// (jstsPrismaRawWriteRe). These are distinctive, no-collision names safe to
// bare-match on any receiver.
var jstsPrismaRawRe = regexp.MustCompile(
	`\.\s*\$queryRaw(?:Unsafe)?\s*[\(` + "`" + `]`,
)

// jstsPrismaRawWriteRe matches Prisma `$executeRaw`/`$executeRawUnsafe` writes.
var jstsPrismaRawWriteRe = regexp.MustCompile(
	`\.\s*\$executeRaw(?:Unsafe)?\s*[\(` + "`" + `]`,
)

// --- #5490 Prisma model-layer data-access (db/prisma client gate + model) ---
//
// The data-access layer in a Prisma codebase is the set of model functions in
// `*.server.ts` / `*.data.ts` modules that wrap the Prisma client. The generic
// bare-match (jstsDBReadRe/jstsDBWriteRe) already credits `prisma.user.find*` /
// `prisma.user.create` to the enclosing function, and the propagation pass
// unions that up the CALLS graph so a caller of `getUsers()` transitively shows
// db_read. This pass UPLIFTS that credit with a Prisma-specific, RECEIVER-GATED
// match that captures the target model in the sink tag (`prisma.read:User`,
// `prisma.write:Post`) so the data-access flow is queryable BY MODEL and ties to
// the Prisma model entity (#5489). It is additive — the EffectSet accumulates
// sink tags — so it never removes or weakens the generic credit.
//
// The receiver gate (`(?:this\.)?(?:prisma|db|tx)\.`) requires an imported
// Prisma client / delegate as the receiver, so an unrelated `foo.create(...)` or
// a builder `repo.create(...)` is NOT misread as a Prisma data-access effect.
// `tx` covers the interactive-transaction callback handle
// (`prisma.$transaction(async (tx) => tx.user.create(...))`).
//
// Verb → effect mapping (mirrors the documented Prisma delegate classification):
//   - db_read : findUnique/findUniqueOrThrow/findFirst/findFirstOrThrow/
//               findMany/count/aggregate/groupBy
//   - db_write: create/createMany/update/updateMany/upsert/delete/deleteMany
const jstsPrismaReadVerbs = `findUnique(?:OrThrow)?|findFirst(?:OrThrow)?|findMany|count|aggregate|groupBy`
const jstsPrismaWriteVerbs = `create(?:Many)?|update(?:Many)?|upsert|delete(?:Many)?`

// jstsPrismaModelReadRe matches a read-verb delegate call on a Prisma client
// receiver, capturing the model name (group 1, lower-camel delegate → PascalCase
// model). Receiver is `prisma`, `db`, or a `tx` transaction handle, optionally
// `this.`-qualified.
var jstsPrismaModelReadRe = regexp.MustCompile(
	`(?:this\s*\.\s*)?(?:prisma|db|tx)\s*\.\s*([a-z][A-Za-z0-9_]*)\s*\.\s*(?:` + jstsPrismaReadVerbs + `)\s*\(`,
)

// jstsPrismaModelWriteRe matches a write-verb delegate call on a Prisma client
// receiver, capturing the model name (group 1).
var jstsPrismaModelWriteRe = regexp.MustCompile(
	`(?:this\s*\.\s*)?(?:prisma|db|tx)\s*\.\s*([a-z][A-Za-z0-9_]*)\s*\.\s*(?:` + jstsPrismaWriteVerbs + `)\s*\(`,
)

// Note on path scoping: the effect sniffer is given only file content (no
// path), so we cannot gate on the `*.server.ts` filename directly. The
// model-bearing uplift is harmless on any file — it only fires on a real Prisma
// client receiver (prisma/db/tx) — so we run it everywhere; the canonical
// data-access location is the model layer (`*.server.ts`), documented by the
// registry note and the fixture.

// prismaModelFromDelegate maps a Prisma delegate accessor (lower-camel, e.g.
// `user`, `userProfile`) to its PascalCase model name (`User`, `UserProfile`),
// matching the convention used by the #5489 schema extractor.
func prismaModelFromDelegate(delegate string) string {
	if delegate == "" {
		return ""
	}
	b := []byte(delegate)
	if b[0] >= 'a' && b[0] <= 'z' {
		b[0] -= 'a' - 'A'
	}
	return string(b)
}

// appendPrismaModelMatches credits each Prisma client delegate call to its
// enclosing function with a model-bearing sink tag (`prisma.read:User`).
func appendPrismaModelMatches(out []EffectMatch, content string, headers []funcHeader, re *regexp.Regexp, eff Effect) []EffectMatch {
	verb := "read"
	if eff == EffectDBWrite {
		verb = "write"
	}
	for _, m := range re.FindAllStringSubmatchIndex(content, -1) {
		line := lineOfOffset(content, m[0])
		model := prismaModelFromDelegate(content[m[2]:m[3]])
		out = append(out, EffectMatch{
			Function:   nearestHeader(headers, line),
			Line:       line,
			Effect:     eff,
			Sink:       "prisma." + verb + ":" + model,
			Confidence: 0.9,
		})
	}
	return out
}

// jstsMutationRe matches `this.<field> = ...` — assignment to a receiver
// field. We require an assignment operator on the same line and reject
// `this.<field> ==` (comparison) by anchoring on `=` followed by a
// non-`=` character.
var jstsMutationRe = regexp.MustCompile(
	`\bthis\s*\.\s*[A-Za-z_$][\w$]*\s*=(?:[^=])`,
)

// sniffEffectsJSTS is the JS/TS effect sniffer entry point.
func sniffEffectsJSTS(content string) []EffectMatch {
	if content == "" {
		return nil
	}
	headers := scanJSTSFuncHeaders(content)
	var out []EffectMatch
	out = appendJSTSMatches(out, content, headers, jstsHTTPRe, EffectHTTPOut, "fetch/axios", 1.0)
	out = appendJSTSMatches(out, content, headers, jstsDBReadRe, EffectDBRead, "orm.read", 0.8)
	out = appendJSTSMatches(out, content, headers, jstsDBWriteRe, EffectDBWrite, "orm.write", 0.8)
	out = appendJSTSMatches(out, content, headers, jstsPrismaRawRe, EffectDBRead, "prisma.$queryRaw", 0.9)
	out = appendJSTSMatches(out, content, headers, jstsPrismaRawWriteRe, EffectDBWrite, "prisma.$executeRaw", 0.9)
	// #5490 model-layer data-access: receiver-gated Prisma delegate calls credited
	// with a model-bearing sink tag so the data-access flow is queryable by model.
	out = appendPrismaModelMatches(out, content, headers, jstsPrismaModelReadRe, EffectDBRead)
	out = appendPrismaModelMatches(out, content, headers, jstsPrismaModelWriteRe, EffectDBWrite)
	out = append(out, jstsBuilderDataAccessMatches(content, headers)...)
	out = appendJSTSMatches(out, content, headers, jstsFSReadRe, EffectFSRead, "fs.read", 1.0)
	out = appendJSTSMatches(out, content, headers, jstsFSWriteRe, EffectFSWrite, "fs.write", 1.0)
	out = appendJSTSMatches(out, content, headers, jstsMutationRe, EffectMutation, "this.field=", 0.7)
	return out
}

// funcHeader records a function-header line so sinks can be attributed
// to the nearest preceding header. Lexical scoping is approximated by
// "nearest preceding" — fine for module-level and class-level methods,
// imprecise for deeply nested closures (acceptable for Phase 1A; Phase
// 4 will tighten via the real AST).
type funcHeader struct {
	Line int
	Name string
}

func scanJSTSFuncHeaders(content string) []funcHeader {
	var hs []funcHeader
	for _, m := range jstsFuncHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		var name string
		switch {
		case m[2] >= 0:
			name = content[m[2]:m[3]]
		case m[4] >= 0:
			name = content[m[4]:m[5]]
		case m[6] >= 0:
			name = content[m[6]:m[7]]
		}
		if name == "" || jstsControlKeyword(name) {
			continue
		}
		hs = append(hs, funcHeader{Line: lineOfOffset(content, m[0]), Name: name})
	}
	return hs
}

// jstsControlKeyword rejects keywords that the method-shorthand regex
// can accidentally match (e.g. `if (...) {`). Keeps the header set
// honest without an AST.
func jstsControlKeyword(s string) bool {
	switch s {
	case "if", "for", "while", "switch", "catch", "do", "return", "throw", "typeof", "instanceof", "new", "delete", "void", "in", "of", "yield", "await":
		return true
	}
	return false
}

func appendJSTSMatches(out []EffectMatch, content string, headers []funcHeader, re *regexp.Regexp, eff Effect, sink string, conf float64) []EffectMatch {
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

// jstsBuilderDataAccessMatches implements the #4335/#4336 receiver-typed
// fluent query-builder data-access credit. It (1) collects the set of names
// known to hold a TypeORM QueryBuilder / Knex builder / Prisma delegate
// (seeded from jstsBuilderRootRe, propagated across builder-shaper
// reassignments to a fixpoint), then (2) emits db_read for each ambiguous
// READ terminal and db_write for each ambiguous WRITE terminal invoked on one
// of those typed names, AND for ambiguous terminals chained directly off a
// builder root inline. An ambiguous terminal on an UNTYPED receiver (a plain
// array `.first()`/Promise `.execute()`) earns no credit — the collection /
// Promise false-positive guard is preserved.
func jstsBuilderDataAccessMatches(content string, headers []funcHeader) []EffectMatch {
	typed := collectJSTSBuilderTypedNames(content)
	var out []EffectMatch
	emit := func(off int, eff Effect, sink string) {
		line := lineOfOffset(content, off)
		out = append(out, EffectMatch{
			Function:   nearestHeader(headers, line),
			Line:       line,
			Effect:     eff,
			Sink:       sink,
			Confidence: 0.85,
		})
	}
	// chainHop matches zero-or-more intermediate fluent shaper calls between
	// the typed receiver and the terminal — `qb.where(...).leftJoin(...).getMany()`.
	const chainHop = `(?:\s*\.\s*[A-Za-z_$][\w$]*\s*\([^;]*?\))*?`
	for name := range typed {
		q := regexp.QuoteMeta(name)
		readRe := regexp.MustCompile(`\b` + q + chainHop + `\s*\.\s*(?:` + jstsBuilderReadVerbs + `)\s*\(`)
		for _, m := range readRe.FindAllStringIndex(content, -1) {
			emit(m[0], EffectDBRead, "builder.read")
		}
		writeRe := regexp.MustCompile(`\b` + q + chainHop + `\s*\.\s*(?:` + jstsBuilderWriteVerbs + `)\s*\(`)
		for _, m := range writeRe.FindAllStringIndex(content, -1) {
			emit(m[0], EffectDBWrite, "builder.write")
		}
	}
	for _, m := range jstsBuilderInlineReadRe.FindAllStringIndex(content, -1) {
		emit(m[0], EffectDBRead, "builder.read")
	}
	for _, m := range jstsBuilderInlineWriteRe.FindAllStringIndex(content, -1) {
		emit(m[0], EffectDBWrite, "builder.write")
	}
	return out
}

// collectJSTSBuilderTypedNames returns the set of names that hold a TypeORM
// QueryBuilder / Knex builder / Prisma delegate. Seeds from jstsBuilderRootRe,
// then iterates jstsBuilderChainAssignRe to a fixpoint so a builder bound from
// an already-typed receiver (`const qb2 = qb.where(...)`) is itself typed.
func collectJSTSBuilderTypedNames(content string) map[string]bool {
	typed := map[string]bool{}
	for _, m := range jstsBuilderRootRe.FindAllStringSubmatch(content, -1) {
		if len(m) >= 2 && m[1] != "" {
			typed[m[1]] = true
		}
	}
	chains := jstsBuilderChainAssignRe.FindAllStringSubmatch(content, -1)
	for {
		changed := false
		for _, m := range chains {
			if len(m) < 3 {
				continue
			}
			if typed[m[2]] && !typed[m[1]] {
				typed[m[1]] = true
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return typed
}

// nearestHeader returns the name of the function header whose Line is
// the greatest value ≤ line, or "" when no header precedes line (i.e.
// the match is at module scope).
func nearestHeader(headers []funcHeader, line int) string {
	name := ""
	for _, h := range headers {
		if h.Line <= line {
			name = h.Name
		} else {
			break
		}
	}
	return name
}
