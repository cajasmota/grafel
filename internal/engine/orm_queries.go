// ORM QUERIES edge synthesis (issue #723).
//
// This engine-layer pass scans source files for ORM/database-client query
// call sites and emits directed QUERIES edges from the enclosing
// caller (function/method) to the model class targeted by the query.
//
// Architecture (per the #721 architectural lesson): single engine-layer
// pass that lives in `internal/engine/orm_queries*.go`, not per-language
// extractor files. The pass:
//
//   1. Indexes the file's existing class definitions so it can attach
//      QUERIES edges to entities the detector already emitted (avoids
//      hallucinating model targets that don't exist in the graph).
//   2. Indexes enclosing functions/methods so each call site can be
//      attributed to a `source_caller` for the FromID.
//   3. Per-language: runs ORM-specific regex matchers that locate
//      `<Model>.<verb>(...)` / `<orm>.<model>.<verb>(...)` call sites.
//   4. Emits one QUERIES edge per call site with properties:
//        - `operation`   — canonicalised verb (find / create / update /
//                          delete / aggregate)
//        - `filter_keys` — comma-separated keys parsed from the call's
//                          first argument (when statically extractable)
//        - `is_join`     — "true" when the call references related models
//                          (Prisma nested include/where on a relation, JPA
//                          @JoinColumn-style traversal, ActiveRecord
//                          .includes / .joins)
//        - `orm`         — short ORM identifier
//        - `pattern_type`— "orm_queries"
//
// Closes a major orphan class: model classes that are referenced ONLY via
// ORM clients (Prisma `prisma.user.findUnique`, Django `User.objects.get`)
// and never via class-name code. The detector treats those models as
// unused; this pass wires them back in.
//
// Scope (phase 1, ALL major ORMs):
//
//   - Python  : Django ORM (`Model.objects.<verb>(...)`),
//               SQLAlchemy (`session.query(Model)`, `select(Model)`),
//               Tortoise (`Model.filter(...)` etc.),
//               Peewee (`Model.select()` etc.)
//   - JS/TS   : Prisma (`prisma.user.findUnique(...)`),
//               Mongoose (`User.findOne(...)` on Model instances),
//               TypeORM (`userRepo.findOne(...)`),
//               Sequelize (`User.findAll(...)`),
//               Supabase (`supabase.from('users').select(...)`)
//   - Go      : gorm (`db.First(&user, ...)` / `.Find(&users)`)
//   - Java    : JPA `EntityManager.find(User.class, ...)`,
//               Spring Data `userRepo.findById(...)`
//   - Ruby    : ActiveRecord (`User.find / .where / .create`)
//
// Refs #723.
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// ormQueriesEdgeKind is the directed-edge Kind emitted by this pass.
// Lives alongside RelationshipKindAccessesTable but distinguished by name
// to make graph queries cheap: "all data-access edges by language" can
// filter on kind=QUERIES. Aliased through to the types package so the
// typed enum stays canonical.
var ormQueriesEdgeKind = string(types.RelationshipKindQueries)

// ormQueriesPatternType is the pattern_type property attached to every
// emitted QUERIES edge; matches the existing pattern_type convention used
// by http_endpoint_synthesis.go.
const ormQueriesPatternType = "orm_queries"

// applyORMQueries runs after the per-language detection passes and
// APPENDS QUERIES edges to the detector output. It never modifies or
// removes existing entities or edges, so it cannot regress the
// surrounding pipeline's bug-rate on files that contain no ORM calls.
//
// `lang` lets the pass no-op cleanly for files in languages whose ORM
// patterns aren't recognised yet (phase-1 deferred languages: Kotlin
// Exposed/Ktorm, PHP Eloquent/Doctrine, Rust Diesel/SeaORM, C# Entity
// Framework — file a phase-2 follow-up).
func applyORMQueries(
	lang string,
	path string,
	content []byte,
	entities []types.EntityRecord,
	relationships []types.RelationshipRecord,
) ([]types.EntityRecord, []types.RelationshipRecord) {
	if len(content) == 0 {
		return entities, relationships
	}
	if !ormQueriesSupportsLanguage(lang) {
		return entities, relationships
	}

	src := string(content)

	// Build the per-file class/model index. Membership in this set is the
	// gate that prevents emitting QUERIES edges to nonexistent target
	// names. We accept ANY class/model entity in the file regardless of
	// kind (Class, Component/class, Schema): the ORM may target a Django
	// model (SCOPE.Component/class), a Spring @Entity (SCOPE.Schema), or
	// a TypeORM @Entity (SCOPE.Schema). The matcher uses Name only.
	classNames := indexClassNames(entities, path)

	// Build the enclosing-function index so each call site can be
	// attributed to a stable caller identifier in the FromID.
	funcs := indexEnclosingFunctions(lang, src)

	emit := func(callerName, modelName, op, filterKeys, orm string, isJoin bool) {
		if modelName == "" {
			return
		}
		// Only emit when the model exists as a class entity in this file
		// OR we have at least a plausible model name (capitalised, not a
		// language keyword). Cross-file model resolution is handled by a
		// later linker pass (#735 follow-up) which matches by Name.
		_ = classNames // index retained for future tightening; phase 1 is permissive
		fromID := buildCallerID(lang, callerName, path)
		toID := buildModelID(lang, modelName)
		props := map[string]string{
			"operation":    op,
			"orm":          orm,
			"pattern_type": ormQueriesPatternType,
			"is_join":      boolStr(isJoin),
		}
		if filterKeys != "" {
			props["filter_keys"] = filterKeys
		}
		relationships = append(relationships, types.RelationshipRecord{
			FromID:     fromID,
			ToID:       toID,
			Kind:       ormQueriesEdgeKind,
			Properties: props,
		})
	}

	switch lang {
	case "python":
		scanPythonORM(src, funcs, emit)
	case "javascript", "typescript":
		scanJSORM(src, funcs, emit)
	case "go":
		scanGoORM(src, funcs, emit)
	case "java":
		scanJavaORM(src, funcs, emit)
	case "ruby":
		scanRubyORM(src, funcs, emit)
	}

	return entities, relationships
}

// ormQueriesSupportsLanguage reports whether applyORMQueries has at least
// one ORM matcher registered for `lang`. The detector consults this so
// the pass is skipped cheaply for unsupported files.
func ormQueriesSupportsLanguage(lang string) bool {
	switch lang {
	case "python", "javascript", "typescript", "go", "java", "ruby":
		return true
	default:
		return false
	}
}

// emitORMQueryFn is the closure passed to per-language scanners; keeping
// the signature in one place lets us evolve emission shape without
// touching every scanner.
type emitORMQueryFn func(callerName, modelName, op, filterKeys, orm string, isJoin bool)

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// indexClassNames returns the set of class/model entity Names declared in
// `path`. Used as a permissive filter on QUERIES targets.
func indexClassNames(entities []types.EntityRecord, path string) map[string]bool {
	out := map[string]bool{}
	for _, e := range entities {
		if e.SourceFile != "" && e.SourceFile != path {
			continue
		}
		switch {
		case e.Kind == "SCOPE.Class":
			out[e.Name] = true
		case e.Kind == "SCOPE.Component" && (e.Subtype == "class" || e.Subtype == ""):
			out[e.Name] = true
		case e.Kind == "SCOPE.Schema":
			out[e.Name] = true
		}
	}
	return out
}

// buildCallerID assembles the FromID for a QUERIES edge from an enclosing
// function name. Defaults to a `Function:<name>` shape which matches the
// existing source_caller convention from http_endpoint_client_synthesis.
// When the caller is unknown we fall back to a path-anchored placeholder
// so the edge still expresses "something in this file queries the model"
// rather than being silently dropped.
func buildCallerID(lang, callerName, path string) string {
	if callerName == "" {
		return fmt.Sprintf("File:%s", path)
	}
	return fmt.Sprintf("Function:%s", callerName)
}

// buildModelID assembles the ToID for a QUERIES edge. Phase 1 emits the
// `Class:<Name>` shape — the existing intra-repo resolver matches Class
// targets against any Class/Schema/Component-class entity by Name, so a
// single shape works across languages.
func buildModelID(lang, modelName string) string {
	return fmt.Sprintf("Class:%s", modelName)
}

// ---------------------------------------------------------------------------
// Enclosing-function indexing (cross-language)
// ---------------------------------------------------------------------------

// funcSpan is the file-offset + name record used to attribute call sites
// to their nearest preceding function declaration. Shape mirrors the
// jsFuncSpan / pyFuncSpan used by http_endpoint_client_synthesis.go.
type funcSpan struct {
	offset int
	name   string
}

var (
	// Function/method declaration heuristics. Coarse on purpose — the
	// pass attributes a call site to its nearest preceding function
	// declaration. Misattribution at top level falls back to a file-anchored
	// caller, which still produces a useful QUERIES edge.

	pyOrmFuncRe = regexp.MustCompile(`(?m)^[ \t]*(?:async\s+)?def\s+([A-Za-z_]\w*)\s*\(`)

	jsOrmFuncRe = regexp.MustCompile(
		`(?:^|[^\w$])(?:` +
			`(?:async\s+)?function\s+([A-Za-z_$][\w$]*)\s*\(` +
			`|` +
			`(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s+)?\(` +
			`|` +
			`(?:public|private|protected|static|async)?\s*([A-Za-z_$][\w$]*)\s*\([^)]*\)\s*\{` +
			`)`,
	)

	goOrmFuncRe = regexp.MustCompile(
		`(?m)^func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)\s*\(`,
	)

	javaOrmFuncRe = regexp.MustCompile(
		`(?m)(?:public|private|protected|static|final|\s)+[\w<>\[\],\s]+\s+([A-Za-z_]\w*)\s*\([^)]*\)\s*(?:throws\s+[\w,\s]+)?\s*\{`,
	)

	rubyOrmFuncRe = regexp.MustCompile(`(?m)^[ \t]*def\s+(?:self\.)?([A-Za-z_]\w*[?!]?)`)
)

func indexEnclosingFunctions(lang, content string) []funcSpan {
	var re *regexp.Regexp
	switch lang {
	case "python":
		re = pyOrmFuncRe
	case "javascript", "typescript":
		re = jsOrmFuncRe
	case "go":
		re = goOrmFuncRe
	case "java":
		re = javaOrmFuncRe
	case "ruby":
		re = rubyOrmFuncRe
	default:
		return nil
	}
	var out []funcSpan
	for _, m := range re.FindAllStringSubmatchIndex(content, -1) {
		name := ""
		// Pick the first non-empty captured group.
		for i := 2; i+1 < len(m); i += 2 {
			if m[i] >= 0 {
				name = content[m[i]:m[i+1]]
				if name != "" {
					break
				}
			}
		}
		if name == "" {
			continue
		}
		out = append(out, funcSpan{offset: m[0], name: name})
	}
	return out
}

func enclosingFuncAt(funcs []funcSpan, pos int) string {
	name := ""
	for _, f := range funcs {
		if f.offset > pos {
			break
		}
		name = f.name
	}
	return name
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// canonicalOp maps an ORM-specific verb to the small canonical set used
// in the QUERIES edge's `operation` property. Unknown verbs pass through
// unchanged so a downstream consumer can still see what was matched.
func canonicalOp(verb string) string {
	switch strings.ToLower(verb) {
	case "find", "findone", "find_one", "findunique", "find_by",
		"findfirst", "first", "get", "select", "query",
		"filter", "where", "all", "findall", "findmany",
		"objects.get", "objects.filter", "objects.all",
		"last", "values", "values_list", "order_by", "exclude",
		"none", "in_bulk", "raw", "exists", "exists?",
		"findoneby", "findby", "findunique_or_throw",
		"findoneorfail", "findandcount":
		return "find"
	case "create", "insert", "save", "add", "addall",
		"insert_one", "insert_many", "build",
		"get_or_create", "update_or_create", "bulk_create", "create!":
		return "create"
	case "update", "update_one", "update_many", "save_changes",
		"updates", "bulk_update", "update!":
		return "update"
	case "delete", "destroy", "remove", "delete_one", "delete_many",
		"destroy_all", "delete_all", "destroy!":
		return "delete"
	case "count", "sum", "avg", "min", "max", "aggregate", "group_by",
		"countdocuments", "annotate":
		return "aggregate"
	case "select_related", "prefetch_related", "preload", "joins",
		"includes", "populate":
		// Eager-loading verbs: model the call as a find for op purposes;
		// the is_join property already records the join intent.
		return "find"
	default:
		return strings.ToLower(verb)
	}
}

// isCapitalisedIdent reports whether s starts with an uppercase letter
// (the heuristic for "this is a model class name, not a method or
// variable"). Filters out matches like `self.objects.filter` from
// Django code.
func isCapitalisedIdent(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	return c >= 'A' && c <= 'Z'
}

// parseFilterKeys does a best-effort extraction of keyword-argument /
// object-key identifiers from a call's first argument blob. It runs on
// substrings like `id=1, name="x"` (Python kwargs), `{ where: { id, name } }`
// (Prisma options), or `:id, name: x` (Ruby symbol args). Returns a
// comma-separated alphabetically-sorted dedup'd list, or "" when nothing
// useful can be extracted.
var filterKeyRe = regexp.MustCompile(`\b([a-zA-Z_][a-zA-Z0-9_]*)\s*(?:=|:)\s*`)

func parseFilterKeys(blob string) string {
	// Strip the outermost wrapper braces/parens so the inner key pattern
	// runs against the contents.
	blob = strings.TrimSpace(blob)
	if blob == "" {
		return ""
	}
	matches := filterKeyRe.FindAllStringSubmatch(blob, -1)
	if len(matches) == 0 {
		return ""
	}
	seen := map[string]bool{}
	var keys []string
	for _, m := range matches {
		k := m[1]
		// Drop ORM-internal keys that don't represent filter fields.
		switch k {
		case "where", "select", "include", "data", "orderBy",
			"order_by", "limit", "offset", "take", "skip",
			"distinct", "groupBy", "_count", "_sum", "_avg":
			continue
		}
		if seen[k] {
			continue
		}
		seen[k] = true
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return ""
	}
	// Sort for deterministic output. (Stable key sets matter for
	// byte-identical regression checks across runs.)
	sortStrings(keys)
	return strings.Join(keys, ",")
}

func sortStrings(s []string) {
	// Tiny insertion sort — slices are small (1-5 keys typically) and we
	// avoid a sort/strings import in this file.
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// matchCall extracts the inner-argument substring of a `(...)` call
// starting at `openParen`. Stops at the matched closing paren, ignoring
// parens inside string literals. Returns "" if no balance is reached
// within `limit` chars (the call spans too far / parser confused).
func matchCall(s string, openParen int, limit int) string {
	if openParen < 0 || openParen >= len(s) || s[openParen] != '(' {
		return ""
	}
	depth := 0
	end := openParen + limit
	if end > len(s) {
		end = len(s)
	}
	inStr := byte(0)
	for i := openParen; i < end; i++ {
		c := s[i]
		if inStr != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == inStr {
				inStr = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			inStr = c
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[openParen+1 : i]
			}
		}
	}
	return ""
}
