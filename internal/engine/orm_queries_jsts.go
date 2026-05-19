// JS/TS ORM matchers for applyORMQueries (#723).
//
// Covers:
//   - Prisma     : `prisma.<model>.<verb>(...)` (also `db.<model>.<verb>`,
//                  `ctx.prisma.<model>.<verb>`, generic <client>.<model>.<verb>)
//                  Recognised verbs map directly to canonical ops.
//   - Mongoose   : `<Model>.findOne(...)`, `<Model>.findById(...)`,
//                  `<Model>.create(...)`, etc. Anchors on capitalised
//                  model variable + Mongoose-style verbs.
//   - TypeORM    : `<repo>.find(...)` / `.findOne(...)` / `.save(...)` /
//                  `.delete(...)` — relies on a `<x>Repo`/`<x>Repository`
//                  naming convention so the matcher can infer the model
//                  from the receiver identifier.
//   - Sequelize  : same surface as Mongoose; `Model.findAll`, `Model.create`,
//                  etc. Distinguished only by the verb list.
//   - Supabase   : `supabase.from('<table>').<verb>(...)` / generic
//                  `<client>.from(...).<verb>` — Supabase doesn't expose
//                  a model class, so we use the table name as the target
//                  (singularised + capitalised).
//
// Out of phase 1:
//   - Drizzle (`db.select().from(users).where(...)`)
//   - MikroORM EntityManager API
//   - Knex / generic-builder shapes (covered as raw-SQL by the
//     raw-SQL pass in orm_queries_raw_sql.go, phase 2)
package engine

import (
	"regexp"
	"strings"
)

// jsPrismaRe matches `<client>.<model>.<verb>(`. The client identifier
// is constrained to a small allow-list to avoid colliding with arbitrary
// fluent-API chains; the model and verb are pinned to common Prisma
// shapes. The trailing `\b` after the verb ensures we don't half-match
// names like `findUniqueOrThrow` against `findUnique`.
var jsPrismaRe = regexp.MustCompile(
	`\b(prisma|db|ctx\.prisma|this\.prisma|this\.db|client|conn|tx)\.([a-z][A-Za-z0-9_]*)\.(findUnique|findUniqueOrThrow|findFirst|findFirstOrThrow|findMany|create|createMany|update|updateMany|upsert|delete|deleteMany|count|aggregate|groupBy)\s*\(`,
)

// jsMongooseSequelizeRe matches `<Model>.<verb>(`. The verb list is the
// UNION of common Mongoose and Sequelize methods; ORM disambiguation is
// done downstream by other signals (presence of `mongoose` / `Schema`
// imports in the file, etc. — phase 2). The model identifier must be
// capitalised so we don't match `req.query`.
var jsMongooseSequelizeRe = regexp.MustCompile(
	`\b([A-Z][A-Za-z0-9_]+)\.(findOne|findById|findOneAndUpdate|findOneAndDelete|findOneAndRemove|findByIdAndUpdate|findByIdAndDelete|find|findAll|create|bulkCreate|save|update|updateOne|updateMany|destroy|deleteOne|deleteMany|insertMany|countDocuments|count|aggregate|exists|distinct)\s*\(`,
)

// jsTypeORMRepoRe matches `<x>Repo.<verb>(...)` and
// `<x>Repository.<verb>(...)`. We strip the `Repo`/`Repository` suffix
// to recover the inferred model name (capitalised first letter).
var jsTypeORMRepoRe = regexp.MustCompile(
	`\b([a-zA-Z_$][\w$]*?)(Repo|Repository)\.(find|findOne|findOneBy|findBy|findAndCount|findOneOrFail|save|create|insert|update|delete|remove|softDelete|count|exists)\s*\(`,
)

// jsSupabaseRe matches `<client>.from('<table>').<verb>(`. The verb is
// recognised against Supabase's tiny verb surface. Table is captured as
// a string literal — single, double, or backtick quotes.
var jsSupabaseRe = regexp.MustCompile(
	"\\b(supabase|sb|client|db)\\.from\\(\\s*['\"`]([a-zA-Z_][\\w]*)['\"`]\\s*\\)\\.(select|insert|update|delete|upsert|rpc)\\s*\\(",
)

// scanJSORM walks `src` and emits QUERIES edges for every detected JS/TS
// ORM call site. Models for Prisma + Supabase are inferred (singularised
// + capitalised) since those ORMs use a lowercase plural property name.
func scanJSORM(src string, funcs []funcSpan, emit emitORMQueryFn) {
	// Prisma
	for _, m := range jsPrismaRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 8 {
			continue
		}
		lowerModel := src[m[4]:m[5]]
		verb := src[m[6]:m[7]]
		caller := enclosingFuncAt(funcs, m[0])
		argsBlob := extractCallArgs(src, m[7])
		filterKeys := parseFilterKeys(argsBlob)
		isJoin := strings.Contains(argsBlob, "include:") ||
			strings.Contains(argsBlob, "include :") ||
			strings.Contains(argsBlob, ".include")
		model := capitalisedSingular(lowerModel)
		emit(caller, model, canonicalOp(verb), filterKeys, "prisma", isJoin)
	}

	// Mongoose / Sequelize. Gate on import-presence so the broad
	// `<Capitalised>.<verb>(` matcher doesn't fire on every static
	// method call in the file. A simple substring check on `mongoose`
	// / `sequelize` covers the dominant module-import shapes (CommonJS
	// require and ESM import).
	if mentionsMongooseSequelize(src) {
	for _, m := range jsMongooseSequelizeRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		// Skip if this looks like a Prisma client invocation we've already
		// covered (`<client>.user.findUnique` would have a capitalised first
		// segment only if the client itself is `Db`/`Client`/etc; both are
		// allow-listed in the Prisma regex above and matched there). A
		// rougher de-dup heuristic: don't match if the start of the captured
		// model name is preceded by `.` (chained method on something else).
		if m[0] > 0 && src[m[0]] == '.' {
			continue
		}
		// Drop common false positives (class instantiation, type refs).
		model := src[m[2]:m[3]]
		verb := src[m[4]:m[5]]
		// Reject if preceded by `new ` (constructor call), `import `, `from `.
		if hasPrecedingKeyword(src, m[2], "new ") ||
			hasPrecedingKeyword(src, m[2], "import ") ||
			hasPrecedingKeyword(src, m[2], "from ") ||
			hasPrecedingKeyword(src, m[2], "class ") {
			continue
		}
		caller := enclosingFuncAt(funcs, m[0])
		argsBlob := extractCallArgs(src, m[5])
		filterKeys := parseFilterKeys(argsBlob)
		isJoin := strings.Contains(argsBlob, "populate") ||
			strings.Contains(argsBlob, "include:")
		emit(caller, model, canonicalOp(verb), filterKeys, "mongoose_sequelize", isJoin)
	}
	}

	// TypeORM repository pattern
	for _, m := range jsTypeORMRepoRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 8 {
			continue
		}
		base := src[m[2]:m[3]]
		verb := src[m[6]:m[7]]
		if base == "" {
			continue
		}
		caller := enclosingFuncAt(funcs, m[0])
		argsBlob := extractCallArgs(src, m[7])
		filterKeys := parseFilterKeys(argsBlob)
		isJoin := strings.Contains(argsBlob, "relations:") ||
			strings.Contains(argsBlob, "join:")
		model := strings.ToUpper(base[:1]) + base[1:]
		emit(caller, model, canonicalOp(verb), filterKeys, "typeorm", isJoin)
	}

	// Supabase
	for _, m := range jsSupabaseRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 8 {
			continue
		}
		table := src[m[4]:m[5]]
		verb := src[m[6]:m[7]]
		caller := enclosingFuncAt(funcs, m[0])
		argsBlob := extractCallArgs(src, m[7])
		filterKeys := parseFilterKeys(argsBlob)
		model := capitalisedSingular(table)
		emit(caller, model, canonicalOp(verb), filterKeys, "supabase", false)
	}
}

// capitalisedSingular turns a lowercase plural identifier like "users"
// into a model-class-shaped name like "User". This is the heuristic
// used to map Prisma's lowercase delegate names and Supabase's table
// strings onto the model class entities the detector emitted (which use
// the singular, capitalised form).
//
// Strategy (intentionally simple):
//   - Strip a trailing `ies` → `y` (categories → Category)
//   - Strip a trailing `s` (users → User) — but only when len > 1
//   - Capitalise first letter
//
// Edge cases (mice, geese, children, etc.) are left as follow-ups; the
// vast majority of real-world ORM model names use regular plurals.
func capitalisedSingular(s string) string {
	if s == "" {
		return ""
	}
	out := s
	switch {
	case strings.HasSuffix(out, "ies") && len(out) > 3:
		out = out[:len(out)-3] + "y"
	case strings.HasSuffix(out, "ses") && len(out) > 3:
		out = out[:len(out)-2]
	case strings.HasSuffix(out, "s") && len(out) > 1:
		out = out[:len(out)-1]
	}
	if out == "" {
		return ""
	}
	return strings.ToUpper(out[:1]) + out[1:]
}

// mentionsMongooseSequelize is the import-gate for the broad
// `<Capitalised>.<verb>(` matcher. Files that don't visibly import
// mongoose or sequelize are skipped to avoid the matcher firing on
// every static-method call in arbitrary JS/TS source. The check is
// intentionally substring-based so it matches both `require('mongoose')`
// and `import mongoose from 'mongoose'` plus the Sequelize equivalents.
func mentionsMongooseSequelize(src string) bool {
	return strings.Contains(src, "mongoose") ||
		strings.Contains(src, "sequelize") ||
		strings.Contains(src, "Sequelize")
}

// hasPrecedingKeyword reports whether the substring immediately before
// `pos` (after skipping trailing whitespace) matches `kw`. Used to gate
// out class definitions / imports from the Mongoose model matcher.
func hasPrecedingKeyword(src string, pos int, kw string) bool {
	if pos <= 0 {
		return false
	}
	// Walk back over whitespace.
	p := pos - 1
	for p >= 0 && (src[p] == ' ' || src[p] == '\t') {
		p--
	}
	end := p + 1
	start := end - len(kw)
	if start < 0 {
		return false
	}
	return src[start:end] == kw
}
