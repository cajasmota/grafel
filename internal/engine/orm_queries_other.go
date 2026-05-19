// Go / Java / Ruby ORM matchers for applyORMQueries (#723).
//
// Covers (phase 1):
//   - Go    : gorm (`db.First(&user, ...)`, `.Find(&users)`, `.Where(...)`,
//             `.Create(...)`, `.Updates(...)`, `.Delete(...)`)
//   - Java  : JPA EntityManager (`em.find(User.class, id)`),
//             Spring Data repository methods (`userRepository.findById(...)`)
//   - Ruby  : ActiveRecord (`User.find`, `User.where`, `User.create`,
//             `User.find_by`, `User.all`, `User.first`, `User.last`)
//
// Out of phase 1 (filed as #723 follow-up):
//   - Go: ent (`ent.Client.User.Query()`), sqlx/pgx table-name extraction
//   - Java: Hibernate `session.get(User.class, id)`, MyBatis mappers,
//     jOOQ `dsl.selectFrom(USER)`
//   - Kotlin: Exposed (`User.select { ... }`), Ktorm
//   - PHP: Eloquent (`User::find(...)`), Doctrine
//   - Rust: Diesel (`users::table.find(...)`), SeaORM, sqlx
//   - C#: Entity Framework Core (`db.Users.Find(...)`), Dapper
package engine

import (
	"regexp"
	"strings"
)

// Go gorm. The struct pointer argument (`&user`) carries the model name;
// we capture the identifier after `&` and uppercase the first letter to
// recover the type. Bare `*User` arguments are also accepted.
//
// Pattern matches one of:
//   db.First(&user, ...)
//   db.Find(&users, ...)
//   tx.Create(&user)
//   db.Where(...).First(&user)
//   db.Model(&User{}).Updates(...)
//
// The leading receiver identifier is unconstrained (`db`, `tx`, `r.db`,
// `s.gormDB`) so the matcher fires across the typical gorm idioms.
var goGormRe = regexp.MustCompile(
	`\b(?:[A-Za-z_]\w*\.)*(First|Find|Take|Last|Create|Save|Updates?|Delete|Count|Pluck|Where)\s*\(\s*&\s*([A-Za-z_]\w*)\b`,
)

// Go gorm with Model() prefix: `db.Model(&User{})...` — picked up as a
// separate matcher because the model name appears inside a struct
// literal rather than as a plain pointer arg.
var goGormModelRe = regexp.MustCompile(
	`\b(?:[A-Za-z_]\w*\.)*Model\s*\(\s*&\s*([A-Z][A-Za-z0-9_]*)\s*\{`,
)

// JPA EntityManager.find / persist / remove / merge. The model class is
// the `<Class>.class` literal — capture the bare identifier.
var javaEMRe = regexp.MustCompile(
	`\b(?:[A-Za-z_]\w*\.)*(?:find|persist|remove|merge|getReference|createNamedQuery|createQuery)\s*\(\s*([A-Z][A-Za-z0-9_]*)\.class\b`,
)

// Spring Data repository methods. Inferred-model variant: `<x>Repository`
// / `<x>Repo` naming convention — strip the suffix to recover the model.
var javaRepoRe = regexp.MustCompile(
	`\b([a-z][A-Za-z0-9_]*?)(Repository|Repo)\.(find|findAll|findById|findOne|findBy[A-Za-z]+|save|saveAll|delete|deleteAll|deleteById|count|existsBy[A-Za-z]+|exists)\s*\(`,
)

// Ruby ActiveRecord: `<Model>.<verb>(...)`. Pinned to capitalised model
// + AR verbs to avoid colliding with arbitrary class methods.
var rubyARRe = regexp.MustCompile(
	`(?:^|[^.\w])([A-Z][A-Za-z0-9_]*)\.(find|find_by|find_or_create_by|find_or_initialize_by|where|create|create!|update|update!|destroy|destroy_all|delete|delete_all|all|first|last|count|exists\?|includes|joins|select|order|limit|group|having|pluck|distinct|new|build|save|save!)\b`,
)

func scanGoORM(src string, funcs []funcSpan, emit emitORMQueryFn) {
	for _, m := range goGormRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		verb := src[m[2]:m[3]]
		varName := src[m[4]:m[5]]
		// Inferred model: capitalise the variable's first letter. gorm
		// idioms use `&user` for a User model, `&users` for a slice; we
		// strip a trailing `s` to recover the type. Short variable names
		// (`&u`, `&x`) yield a single-letter model and are skipped because
		// they can't be resolved back to a model class entity.
		if len(varName) < 2 {
			continue
		}
		model := strings.ToUpper(varName[:1]) + strings.TrimSuffix(varName[1:], "s")
		caller := enclosingFuncAt(funcs, m[0])
		argsBlob := extractCallArgs(src, m[3])
		filterKeys := parseFilterKeys(argsBlob)
		// is_join: scan a small window BEFORE and AFTER the call site for
		// gorm's chain methods that signal a relation traversal (`.Preload`,
		// `.Joins`). The chain may appear on either side of the matched
		// verb (e.g. `db.Preload("Posts").Find(&users)` puts Preload
		// before; `db.Find(&users).Joins(...)` puts it after).
		windowStart := m[0] - 256
		if windowStart < 0 {
			windowStart = 0
		}
		windowEnd := m[1] + 256
		if windowEnd > len(src) {
			windowEnd = len(src)
		}
		window := src[windowStart:windowEnd]
		isJoin := strings.Contains(window, ".Preload(") ||
			strings.Contains(window, ".Joins(") ||
			strings.Contains(argsBlob, "Preload") ||
			strings.Contains(argsBlob, "Joins")
		emit(caller, model, canonicalOp(verb), filterKeys, "gorm", isJoin)
	}
	for _, m := range goGormModelRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		model := src[m[2]:m[3]]
		caller := enclosingFuncAt(funcs, m[0])
		emit(caller, model, "find", "", "gorm", false)
	}
}

func scanJavaORM(src string, funcs []funcSpan, emit emitORMQueryFn) {
	for _, m := range javaEMRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		model := src[m[2]:m[3]]
		caller := enclosingFuncAt(funcs, m[0])
		// Try to recover the verb from the captured leading method name.
		// The regex used a non-capturing group for the verb so we recover
		// it by re-scanning the match text.
		verb := guessJavaVerb(src, m[0], m[2])
		argsBlob := extractCallArgs(src, m[3])
		filterKeys := parseFilterKeys(argsBlob)
		emit(caller, model, canonicalOp(verb), filterKeys, "jpa", false)
	}
	for _, m := range javaRepoRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 8 {
			continue
		}
		base := src[m[2]:m[3]]
		verb := src[m[6]:m[7]]
		caller := enclosingFuncAt(funcs, m[0])
		argsBlob := extractCallArgs(src, m[7])
		filterKeys := parseFilterKeys(argsBlob)
		// Spring's `findByX_Y` cross-field traversals signal a join.
		isJoin := strings.Contains(verb, "_") || strings.Contains(argsBlob, "join")
		model := strings.ToUpper(base[:1]) + base[1:]
		emit(caller, model, canonicalOp(strings.SplitN(verb, "By", 2)[0]), filterKeys, "spring_data", isJoin)
	}
}

// guessJavaVerb scans the matched substring for the EM verb keyword.
// Defaults to "find" — that's the dominant JPA verb in real corpora.
func guessJavaVerb(src string, start, end int) string {
	snippet := src[start:end]
	for _, verb := range []string{"find", "persist", "remove", "merge", "getReference", "createNamedQuery", "createQuery"} {
		if strings.Contains(snippet, verb) {
			return verb
		}
	}
	return "find"
}

func scanRubyORM(src string, funcs []funcSpan, emit emitORMQueryFn) {
	for _, m := range rubyARRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		model := src[m[2]:m[3]]
		verb := src[m[4]:m[5]]
		// Reject obvious Ruby standard-library / framework class refs
		// that share the AR verb surface. (`Array.new`, `Hash.new`,
		// `Time.now`, `String.new`.)
		switch model {
		case "Array", "Hash", "Time", "Date", "String", "Integer",
			"Float", "Range", "Rails", "Module", "Class", "Object",
			"Kernel", "File", "Dir", "IO", "JSON", "YAML", "Process":
			continue
		}
		caller := enclosingFuncAt(funcs, m[0])
		argsBlob := extractCallArgs(src, m[5])
		filterKeys := parseFilterKeys(argsBlob)
		// is_join: also inspect a small look-ahead window so chained
		// `.includes(:posts)` / `.joins(:posts)` after the initial verb
		// (`User.where(...).includes(:posts)`) flags the parent edge.
		tail := lookAheadChain(src, m[5], 256)
		isJoin := verb == "includes" || verb == "joins" ||
			strings.Contains(argsBlob, ":includes") ||
			strings.Contains(argsBlob, ":joins") ||
			strings.Contains(tail, ".includes(") ||
			strings.Contains(tail, ".joins(")
		emit(caller, model, canonicalOp(verb), filterKeys, "activerecord", isJoin)
	}
}
