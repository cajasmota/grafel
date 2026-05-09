// Package resolve rewrites stub-form RelationshipRecord endpoint references
// (e.g. "View:User", "Model:Article", or a bare "Hello") into deterministic
// 16-char graph entity IDs by looking them up in the merged entity set.
//
// This is the substance of PORT-2-FIX (issue #24). PORT-2 produced thousands
// of relationships but every cross-file ToID was left as a stub string, so
// graph traversal dead-ended at the first cross-file reference. The resolver
// closes that gap.
//
// PORT-2-FIX-3 (issue #31) extends the resolver to handle two additional
// reference shapes emitted by Pass 3 cross-language extractors:
//
//   - Format A: scope:<kind>:<subtype>:<lang>:<file_path>:<name>
//   - Format B: scope:<kind>:<subtype>:<lang>:<file_path>:<scope_name>#<member_name>
//
// and adds a kind-hint code path (driven by the relationship's Kind field)
// that biases ambiguous bare-name lookups toward the kind families typically
// referenced by EXTENDS / IMPLEMENTS / CALLS edges.
package resolve

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// Stub-format constants. The resolver speaks a small grammar of "stub"
// strings emitted by upstream extractors; collecting the literal tokens
// here keeps the parsing logic legible and avoids the magic-string drift
// that caused issue #49.
const (
	// stubPrefixScope marks a structural-ref stub of the form
	//   scope:<kind>:<subtype>:<lang>:<file_path>:<tail>
	// (Format A) or with `#` separating scope/member in the tail
	// (Format B).
	stubPrefixScope = "scope:"
	// stubPrefixExternal marks an external-package placeholder
	// emitted by the external synthesiser (e.g. "ext:django").
	stubPrefixExternal = "ext:"
	// scopeKindPrefix is the optional family prefix on entity kinds
	// emitted by Pass 3 cross-language extractors (e.g. "SCOPE.View").
	scopeKindPrefix = "SCOPE."

	// stubDelim separates the segments of a colon-joined stub. Stub
	// keys are graph identifiers and use forward-slash file paths; we
	// never embed an OS-native path separator in them.
	stubDelim = ":"
	// stubMemberDelim separates the scope and member halves of the
	// Format B tail.
	stubMemberDelim = '#'
	// stubScopeSegments is the number of colon-delimited segments in a
	// well-formed structural-ref stub:
	//   scope:<kind>:<subtype>:<lang>:<file_path>:<tail>
	stubScopeSegments = 6
	// stubScopeKindIndex / stubScopeFileIndex / stubScopeTailIndex are
	// the canonical positions of the segments after SplitN. Indexing
	// them by name keeps lookup-structural readable.
	stubScopeKindIndex = 1
	stubScopeLangIndex = 3
	stubScopeFileIndex = 4
	stubScopeTailIndex = 5

	// dottedNameSep is the character that splits a qualified entity
	// name into <scope>.<member> when building the byMember index.
	dottedNameSep = '.'

	// hexIDLen is the length of a graph.EntityID() output string.
	hexIDLen = 16

	// maxDispositionSamples caps the per-disposition sample list.
	maxDispositionSamples = 5

	// Property keys read off a RelationshipRecord to recover the
	// source language of an edge.
	propLanguage = "language"
	propLang     = "lang"
)

// LookupStatus result codes. These were previously defined as untyped
// const blocks inside multiple functions; centralising them eliminates
// the chance of drift and lets callers type-check on the named values.
const (
	statusSkip      = 0
	statusRewritten = 1
	statusAmbiguous = 2
	statusUnmatched = 3
)

// normalizePath rewrites an OS-native file-system path into the
// forward-slash form used as a graph identifier. Stub keys, the
// byLocation index, and structural-ref file segments all live in this
// canonical form so a Windows extractor emitting "src\foo\bar.py" and
// a POSIX extractor emitting "src/foo/bar.py" agree on a single key.
//
// Only call filepath.FromSlash at the OS-disk boundary (i.e. when
// reading from disk). Inside the resolver every path stays in slash
// form.
func normalizePath(p string) string {
	if p == "" {
		return ""
	}
	return filepath.ToSlash(p)
}

// pkgDirOf returns the directory portion of an already-slash-normalised
// source file path, used as the package key for issue #148's same-package
// method-dispatch index. A path with no separator (file in repo root)
// returns "." so a caller in the root package still hits a non-empty
// bucket; an empty input returns "". The result is in slash form to match
// the rest of the resolver's identifiers.
func pkgDirOf(p string) string {
	if p == "" {
		return ""
	}
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		if i == 0 {
			return "/"
		}
		return p[:i]
	}
	return "."
}

// Disposition classifies the outcome the resolver assigned to an individual
// relationship endpoint. Every endpoint inspected by References() and
// ReferencesEmbedded() falls into exactly one bucket. The bug-rate metric
// (issue #44) is computed as (BugExtractor + BugResolver) / total.
type Disposition int

const (
	// DispositionResolved — the stub was rewritten to a 16-char entity ID.
	DispositionResolved Disposition = iota
	// DispositionExternalKnown — the endpoint points at an "ext:<pkg>"
	// placeholder AND the package is on the static external-package
	// allowlist (e.g. django, react, fmt).
	DispositionExternalKnown
	// DispositionExternalUnknown — the endpoint points at an "ext:<pkg>"
	// placeholder but the package is NOT on the allowlist. Likely a real
	// external dep we haven't catalogued yet.
	DispositionExternalUnknown
	// DispositionDynamic — the stub matches a pattern that is intrinsically
	// static-unresolvable (reflection, dynamic import, env-driven names,
	// template-built strings). Not a bug; the call cannot be resolved
	// statically by design.
	DispositionDynamic
	// DispositionBugExtractor — stub of form "Kind:Name" where the graph
	// has 0 entities with that Name. An extractor SHOULD have emitted an
	// entity but didn't. This is a bug to fix.
	DispositionBugExtractor
	// DispositionBugResolver — stub points at a Name that DOES exist in the
	// graph (potentially under different kinds), but the resolver couldn't
	// disambiguate it. Resolver bug.
	DispositionBugResolver
	// DispositionUnclassified — catch-all. Should be 0 in production runs;
	// non-zero values warrant investigation.
	DispositionUnclassified
)

// String returns a stable, log-friendly label for a Disposition.
func (d Disposition) String() string {
	switch d {
	case DispositionResolved:
		return "resolved"
	case DispositionExternalKnown:
		return "external-known"
	case DispositionExternalUnknown:
		return "external-unknown"
	case DispositionDynamic:
		return "dynamic"
	case DispositionBugExtractor:
		return "bug-extractor"
	case DispositionBugResolver:
		return "bug-resolver"
	case DispositionUnclassified:
		return "unclassified"
	}
	return "unknown"
}

// AllDispositions enumerates every Disposition value in canonical order.
// Used by the verbose log emitter so the breakdown is always printed in the
// same order regardless of map iteration randomness.
var AllDispositions = []Disposition{
	DispositionResolved,
	DispositionExternalKnown,
	DispositionExternalUnknown,
	DispositionDynamic,
	DispositionBugExtractor,
	DispositionBugResolver,
	DispositionUnclassified,
}

// Per-language dynamic-dispatch pattern catalogs (Refs #44).
//
// Matches here tag a stub as DispositionDynamic instead of bug-extractor /
// bug-resolver. The original Refs #44 commit used a single flat slice tested
// against every stub regardless of source language; that produced false
// positives (a Node `res.send("hello")` matched the Ruby `.send(` pattern,
// `repo.Lookup(id)` matched the Go `plugin.Lookup` pattern, etc.).
//
// The fix groups patterns by the language that owns the runtime-dispatch
// idiom. Patterns that are intrinsically reflective regardless of language
// (template-built names like `${x}`) live in crossLangDynamicPatterns.
// Receiver-anchored reflection APIs that have a unique fully-qualified
// shape (Go's `plugin.Lookup`, JVM `Method.invoke` /
// `Class.forName().newInstance()`) stay in their per-language slice.
//
// Language identifiers follow the structural-ref `<lang>:` segment
// convention: "python", "go", "javascript" (also "typescript"), "ruby",
// "java" (also "kotlin", "scala", "jvm"). Unknown / empty languages fall
// back to crossLangDynamicPatterns only.
var (
	pythonDynamicPatterns = []*regexp.Regexp{
		// Bare-identifier forms: per-language extractors emit only the
		// leaf callee identifier (e.g. ToID="getattr") for `getattr(...)`
		// call sites. Without bare-name anchors none of the parens-
		// requiring patterns below ever match real stubs (issue #90).
		regexp.MustCompile(`^getattr$`),
		regexp.MustCompile(`^setattr$`),
		regexp.MustCompile(`^hasattr$`),
		regexp.MustCompile(`^delattr$`),
		regexp.MustCompile(`^eval$`),
		regexp.MustCompile(`^exec$`),
		regexp.MustCompile(`^compile$`),
		regexp.MustCompile(`^__import__$`),
		regexp.MustCompile(`^hasattr\(`),              // hasattr(obj, name)
		regexp.MustCompile(`^delattr\(`),              // delattr(obj, name)
		regexp.MustCompile(`^compile\(`),              // compile(src, ...)
		regexp.MustCompile(`^getattr\(`),              // getattr(obj, name)(...)
		regexp.MustCompile(`^__getattr__$`),           // __getattr__ magic name
		regexp.MustCompile(`^.*\.__getattr__\(`),      // obj.__getattr__("name")
		regexp.MustCompile(`^.*\.__getattribute__\(`), // obj.__getattribute__(...)
		regexp.MustCompile(`^setattr\(`),              // setattr-driven dispatch
		regexp.MustCompile(`^globals\(\)\[`),          // globals()[name](...)
		regexp.MustCompile(`^locals\(\)\[`),           // locals()[name](...)
		regexp.MustCompile(`^vars\(\)\[`),             // vars()[name](...)
		regexp.MustCompile(`^eval\(`),                 // eval(...)
		regexp.MustCompile(`^exec\(`),                 // exec(...)
		regexp.MustCompile(`^__import__\(`),           // __import__("modname")
		regexp.MustCompile(`^importlib\.`),            // importlib.import_module / etc
		regexp.MustCompile(`^functools\.partial\(`),   // functools.partial(...)
		regexp.MustCompile(`^functools\.partialmethod\(`),
		regexp.MustCompile(`^functools\.reduce\(`),
		regexp.MustCompile(`^operator\.methodcaller\(`), // operator.methodcaller("name")
		regexp.MustCompile(`^operator\.attrgetter\(`),   // operator.attrgetter(...)
		regexp.MustCompile(`^operator\.itemgetter\(`),   // operator.itemgetter(...)
		regexp.MustCompile(`^os\.environ\[`),            // env-driven (Python)
		regexp.MustCompile(`^os\.getenv\(`),             // env-driven (Python)
		// dispatch via dict/list subscript: handlers[key](...), funcs["x"](...).
		// Anchored "<ident>[...](...)" so we don't bite plain attribute access.
		regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*\[[^\]]+\]\(`),
	}

	goDynamicPatterns = []*regexp.Regexp{
		regexp.MustCompile(`^reflect\.`),       // reflect.* (Call, ValueOf, MethodByName, ...)
		regexp.MustCompile(`\.MethodByName\(`), // v.MethodByName("X").Call(...)
		regexp.MustCompile(`\.FieldByName\(`),  // v.FieldByName("X")
		regexp.MustCompile(`^plugin\.Open\(`),  // Go plugin loader
		// Anchored: only `plugin.Lookup(` (or `<x>.plugin.Lookup(`) — bare
		// `repo.Lookup(id)` / `cache.Lookup(...)` are NOT reflection.
		regexp.MustCompile(`\bplugin\.Lookup\(`),
	}

	jsDynamicPatterns = []*regexp.Regexp{
		regexp.MustCompile(`^Reflect\.`),      // Reflect.apply / Reflect.construct / Reflect.get
		regexp.MustCompile(`^eval$`),          // bare eval (issue #95)
		regexp.MustCompile(`^eval\(`),         // eval(src)
		regexp.MustCompile(`^Function$`),      // bare Function constructor reference
		regexp.MustCompile(`^Function\(`),     // Function(src)
		regexp.MustCompile(`^new Function\(`), // new Function(src)
		// Dynamic import / require: must NOT be a literal-string first arg —
		// `require("fs")` and `import("./mod")` are statically resolvable.
		regexp.MustCompile("^require\\([^\"'`)]"),
		regexp.MustCompile("^import\\([^\"'`)]"),
		regexp.MustCompile(`^process\.env\.`), // env-driven (JS)
		// JS reflective `Function.prototype.{bind,apply,call}` is real, but
		// the bare `.bind(` / `.apply(` / `.call(` patterns collide with too
		// many domain methods (DB driver `bind`, `discount.apply(order)`,
		// `controller.call(...)`). Keep them out of the JS catalog; the
		// extractors tag truly reflective uses (e.g. `Reflect.apply`) which
		// the explicit `Reflect\.` pattern above already covers.
	}

	rubyDynamicPatterns = []*regexp.Regexp{
		// Bare-identifier forms: per-language extractors (Ruby, etc.)
		// emit only the leaf callee identifier so the parens-requiring
		// patterns below never match a real stub. Reflective Ruby method
		// names are unique enough to be safe as bare-name anchors
		// (issue #90).
		regexp.MustCompile(`^send$`),
		regexp.MustCompile(`^public_send$`),
		regexp.MustCompile(`^__send__$`),
		regexp.MustCompile(`^define_method$`),
		regexp.MustCompile(`^instance_eval$`),
		regexp.MustCompile(`^class_eval$`),
		regexp.MustCompile(`^.*\.send\(`),        // obj.send(:name) — Ruby ONLY
		regexp.MustCompile(`^send\(`),            // bare send(:name)
		regexp.MustCompile(`^.*\.public_send\(`), // obj.public_send(:name)
		regexp.MustCompile(`^public_send\(`),
		regexp.MustCompile(`^.*\.__send__\(`),  // obj.__send__(:name)
		regexp.MustCompile(`^method_missing$`), // ruby method_missing hook
		regexp.MustCompile(`^.*\.method_missing\(`),
		regexp.MustCompile(`^define_method\(`), // metaprogramming
		regexp.MustCompile(`^.*\.define_method\(`),
		regexp.MustCompile(`^.*\.instance_eval\(`),
		regexp.MustCompile(`^.*\.class_eval\(`),

		// Rails ActionController / ActionDispatch DSL methods (issue
		// #107). These are method_missing-driven and routing/render
		// helpers — bare-name calls in Rails controllers and views the
		// resolver can't bind statically. Classify as Dynamic so they
		// don't pollute bug-extractor (rails-realworld 38.93%, sidekiq
		// 29.83% pre-fix). Names are Rails-unique enough not to collide
		// with the generic Object/Kernel allowlist below.
		regexp.MustCompile(`^render$`),
		regexp.MustCompile(`^permit$`),
		regexp.MustCompile(`^require$`),
		regexp.MustCompile(`^redirect_to$`),
		regexp.MustCompile(`^respond_to$`),
		regexp.MustCompile(`^before_action$`),
		regexp.MustCompile(`^skip_before_action$`),
		regexp.MustCompile(`^after_action$`),
		regexp.MustCompile(`^around_action$`),
		regexp.MustCompile(`^helper_method$`),
		regexp.MustCompile(`^params$`),
		regexp.MustCompile(`^session$`),
		regexp.MustCompile(`^flash$`),
		regexp.MustCompile(`^cookies$`),
		regexp.MustCompile(`^request$`),
		regexp.MustCompile(`^response$`),
		// ActiveRecord dynamic finders — `find_by_<attr>` /
		// `find_or_create_by_<attr>` (with optional `!` bang variant)
		// are method_missing-generated by AR at runtime.
		regexp.MustCompile(`^find_by_\w+!?$`),
		regexp.MustCompile(`^find_or_create_by_\w+!?$`),

		// ActiveRecord query-builder methods (issue #107). Chained query
		// DSL on AR relations — bare-name calls the resolver can't bind
		// to a local entity. Multi-language collisions exist for
		// `where`/`order`/etc., but the per-language gate (Ruby only)
		// keeps them safely scoped. Generic collection ops (each / map /
		// select / find / count / length / size) are deliberately
		// EXCLUDED — they collide with user methods on any class.
		regexp.MustCompile(`^order$`),
		regexp.MustCompile(`^where$`),
		regexp.MustCompile(`^joins$`),
		regexp.MustCompile(`^includes$`),
		regexp.MustCompile(`^eager_load$`),
		regexp.MustCompile(`^preload$`),
		regexp.MustCompile(`^pluck$`),
		regexp.MustCompile(`^distinct$`),
		regexp.MustCompile(`^group$`),
		regexp.MustCompile(`^having$`),
		regexp.MustCompile(`^limit$`),
		regexp.MustCompile(`^offset$`),
		regexp.MustCompile(`^scope$`),
		regexp.MustCompile(`^belongs_to$`),
		regexp.MustCompile(`^has_many$`),
		regexp.MustCompile(`^has_one$`),
		regexp.MustCompile(`^has_and_belongs_to_many$`),
		regexp.MustCompile(`^validates$`),
		regexp.MustCompile(`^validate$`),
		regexp.MustCompile(`^before_save$`),
		regexp.MustCompile(`^after_save$`),
		regexp.MustCompile(`^before_create$`),
		regexp.MustCompile(`^after_create$`),
		regexp.MustCompile(`^before_destroy$`),
		regexp.MustCompile(`^after_destroy$`),

		// ActiveRecord migration DSL (issue #124). Rails migrations are
		// method_missing-driven schema DSL — `t.string :name`,
		// `create_table :users do |t|`, `add_index :users, :email`,
		// `references :user`. The Ruby extractor strips the receiver and
		// the resolver sees only the bare leaf identifier. These names
		// are the dominant residue in rails-realworld bug-extractor
		// after #143. Per-language gate (Ruby) keeps the column-type
		// names (`string`, `integer`, `boolean`, `text`) safely scoped
		// — they would collide trivially with user-method names in any
		// other ecosystem.
		regexp.MustCompile(`^create_table$`),
		regexp.MustCompile(`^drop_table$`),
		regexp.MustCompile(`^change_table$`),
		regexp.MustCompile(`^rename_table$`),
		regexp.MustCompile(`^add_column$`),
		regexp.MustCompile(`^remove_column$`),
		regexp.MustCompile(`^rename_column$`),
		regexp.MustCompile(`^change_column$`),
		regexp.MustCompile(`^add_index$`),
		regexp.MustCompile(`^remove_index$`),
		regexp.MustCompile(`^add_reference$`),
		regexp.MustCompile(`^remove_reference$`),
		regexp.MustCompile(`^add_foreign_key$`),
		regexp.MustCompile(`^remove_foreign_key$`),
		regexp.MustCompile(`^references$`),
		regexp.MustCompile(`^timestamps$`),
		regexp.MustCompile(`^string$`),
		regexp.MustCompile(`^integer$`),
		regexp.MustCompile(`^boolean$`),
		regexp.MustCompile(`^text$`),
		regexp.MustCompile(`^datetime$`),
		regexp.MustCompile(`^date$`),
		regexp.MustCompile(`^float$`),
		regexp.MustCompile(`^decimal$`),
		regexp.MustCompile(`^binary$`),
		regexp.MustCompile(`^execute$`),
	}

	jvmDynamicPatterns = []*regexp.Regexp{
		// Bare-identifier forms: Java/Kotlin extractors emit only the
		// leaf callee identifier (e.g. `m.invoke(target, args)` →
		// ToID="invoke"). Real reflection in the wild stores the
		// reflective handle in a local var (`Method m = clazz.getMethod(name); m.invoke(...)`),
		// so the receiver-typed pattern `Method.invoke(` never sees the
		// type and the call lands in BugExtractor instead of Dynamic
		// (issue #72). The bare-name anchors below promote those leaf
		// callees into the Dynamic disposition.
		//
		// Collisions with non-reflective user methods (`cli.invoke(...)`,
		// `factory.newInstance()`) are accepted: Dynamic is the
		// appropriate "we know it's dispatch we can't statically resolve"
		// bucket — these stubs are equally unresolvable either way, and
		// classifying them as Dynamic keeps them out of bug-extractor.
		// The per-language gate (Java/Kotlin/Scala/JVM only) keeps these
		// names from polluting other languages.
		regexp.MustCompile(`^forName$`),                 // Class.forName
		regexp.MustCompile(`^invoke$`),                  // Method.invoke / Constructor handle
		regexp.MustCompile(`^newInstance$`),             // Class.newInstance / Constructor.newInstance
		regexp.MustCompile(`^getClass$`),                // Object.getClass — reflection entry point
		regexp.MustCompile(`^getMethod$`),               // Class.getMethod
		regexp.MustCompile(`^getMethods$`),              // Class.getMethods
		regexp.MustCompile(`^getDeclaredMethod$`),       // Class.getDeclaredMethod
		regexp.MustCompile(`^getDeclaredMethods$`),      // Class.getDeclaredMethods
		regexp.MustCompile(`^getField$`),                // Class.getField
		regexp.MustCompile(`^getFields$`),               // Class.getFields
		regexp.MustCompile(`^getDeclaredField$`),        // Class.getDeclaredField
		regexp.MustCompile(`^getDeclaredFields$`),       // Class.getDeclaredFields
		regexp.MustCompile(`^getConstructor$`),          // Class.getConstructor
		regexp.MustCompile(`^getConstructors$`),         // Class.getConstructors
		regexp.MustCompile(`^getDeclaredConstructor$`),  // Class.getDeclaredConstructor
		regexp.MustCompile(`^getDeclaredConstructors$`), // Class.getDeclaredConstructors
		// JVM reflection invoke is `Method.invoke(...)` or
		// `Constructor.invoke(...)`. Anchored to those receivers when
		// the full call expression is present (extractor pre-strip
		// stubs) so a user-defined `cli.invoke(...)` / `cmd.invoke(...)`
		// does NOT match.
		regexp.MustCompile(`\b(?:Method|Constructor)\.invoke\(`),
		regexp.MustCompile(`^Class\.forName\(`), // Class.forName("...")
		// Anchored to the reflective `Class.forName(...).newInstance()` /
		// `<Type>.class.newInstance()` shape so a plain factory method
		// named `newInstance()` on a domain class does NOT match.
		regexp.MustCompile(`Class\.forName\([^)]*\)\.newInstance\(`),
		regexp.MustCompile(`\.class\.newInstance\(`),
		regexp.MustCompile(`^ServiceLoader\.load\(`), // ServiceLoader.load(...)
		regexp.MustCompile(`^System\.getenv\(`),      // env-driven (JVM)
	}

	// Cross-language patterns that are safe to evaluate when language is
	// unknown. Template-built names (`${x}` interpolation) are reflection-
	// shaped in every language that has them.
	crossLangDynamicPatterns = []*regexp.Regexp{
		regexp.MustCompile(`.*\$\{.*\}.*`), // template-built strings ${x}
	}

	// dynamicPatternsByLang dispatches a normalized language tag to its
	// per-language pattern slice. Keys are lower-case canonical names; the
	// resolver normalizes incoming tags before lookup.
	dynamicPatternsByLang = map[string][]*regexp.Regexp{
		"python":     pythonDynamicPatterns,
		"go":         goDynamicPatterns,
		"javascript": jsDynamicPatterns,
		"typescript": jsDynamicPatterns,
		"ruby":       rubyDynamicPatterns,
		"java":       jvmDynamicPatterns,
		"kotlin":     jvmDynamicPatterns,
		"scala":      jvmDynamicPatterns,
		"jvm":        jvmDynamicPatterns,
	}
)

// normalizeLang lowercases a language tag and maps a few common aliases to
// the canonical key used by dynamicPatternsByLang. Unknown tags pass
// through unchanged so the lookup miss falls through to the cross-language
// catalog.
func normalizeLang(lang string) string {
	l := strings.ToLower(strings.TrimSpace(lang))
	switch l {
	case "py":
		return "python"
	case "js":
		return "javascript"
	case "ts":
		return "typescript"
	case "rb":
		return "ruby"
	case "kt":
		return "kotlin"
	}
	return l
}

// inferLangFromStub extracts the language tag from a structural-ref stub
// (`scope:<kind>:<subtype>:<lang>:<file>:<tail>`). Returns "" for stubs that
// aren't structural refs.
func inferLangFromStub(stub string) string {
	if !strings.HasPrefix(stub, stubPrefixScope) {
		return ""
	}
	parts := strings.SplitN(stub, stubDelim, stubScopeSegments)
	if len(parts) <= stubScopeLangIndex {
		return ""
	}
	return normalizeLang(parts[stubScopeLangIndex])
}

// isDynamicPattern reports whether the stub matches any reflective /
// runtime-dispatch pattern. Equivalent to isDynamicPatternLang with a
// best-effort language inference (structural-ref segment when available;
// empty otherwise → cross-language catalog only).
func isDynamicPattern(stub string) bool {
	return isDynamicPatternLang(stub, inferLangFromStub(stub))
}

// isDynamicPatternLang gates pattern evaluation on the supplied language.
// When lang resolves to a known per-language catalog only that catalog plus
// the cross-language catalog runs; the receiver-anchored patterns inside
// each per-language slice are already tight enough to be safe.
//
// Empty / unknown languages run only the cross-language catalog. This is
// deliberately conservative: a language-agnostic call site like
// `res.send("hello")` (Node) or `repo.Lookup(id)` (Go domain code) must
// NOT be classified Dynamic without positive evidence.
func isDynamicPatternLang(stub, lang string) bool {
	if stub == "" {
		return false
	}
	for _, re := range crossLangDynamicPatterns {
		if re.MatchString(stub) {
			return true
		}
	}
	if patterns, ok := dynamicPatternsByLang[normalizeLang(lang)]; ok {
		for _, re := range patterns {
			if re.MatchString(stub) {
				return true
			}
		}
	}
	return false
}

// ExternalAllowlist is the function signature used by the resolver to
// decide whether an "ext:<pkg>" endpoint is a known package or not. The
// caller injects the actual allowlist (typically a wrapper around
// internal/external) so this package stays free of an upward import.
//
// The argument is the canonical package name with the "ext:" prefix already
// stripped. A nil ExternalAllowlist treats every external as Unknown.
type ExternalAllowlist func(pkg string) bool

// Index is a kind-aware (kind, name) -> entity_id lookup. The inner map only
// retains a name when the (kind, name) tuple resolves to exactly one entity;
// ambiguous tuples are tracked separately in the embedded ambig set so the
// resolver can leave them as stubs rather than silently picking a wrong match.
type Index struct {
	// byKind[kind][name] = entity_id (only when unique within that kind).
	byKind map[string]map[string]string
	// ambigKind[kind][name] = true when a (kind, name) tuple is ambiguous.
	ambigKind map[string]map[string]bool

	// byName[name] = entity_id (only when unique across ALL kinds). Used
	// for the kind-agnostic fallback when a stub has no "Kind:" prefix or
	// when the kind-specific lookup misses.
	byName map[string]string
	// ambigName[name] = true when a name appears in two or more entities.
	ambigName map[string]bool

	// nameKinds[name][kind] = entity_id for every entity sharing this
	// name. A blank string sentinel means two entities share that
	// (name, kind) tuple — i.e. the kind itself is ambiguous for this
	// name and the kind hint cannot disambiguate via this family.
	nameKinds map[string]map[string]string

	// byLocation[file_path][name] = entity_id, retained only when unique
	// within the file. Used by structural-ref Format A resolution.
	byLocation LocationIndex
	// ambigLocation[file_path][name] = true when (file, name) collides.
	ambigLocation map[string]map[string]bool

	// byLocationKind[file_path][name][kind] = entity_id. Kind-aware
	// (file, name) lookup. PORT-2-FIX-2 emissions can produce two entities
	// at the same (file, name) with different kinds (e.g. SCOPE.Component
	// class + SCOPE.Operation method); kind-aware lookup picks the correct
	// one when the relationship's kind hint maps to a single family.
	// A blank string sentinel marks (file, name, kind) collisions.
	byLocationKind LocationKindIndex

	// byQualifiedName[qualified_name] = entity_id. Direct lookup for
	// stubs whose ToID is an entity QualifiedName verbatim (e.g. markdown
	// CONTAINS edges where ToID = "<file>::<heading-slug>"). Issue #100.
	// First writer wins; a blank-string sentinel marks collisions so we
	// never resolve an ambiguous QualifiedName.
	byQualifiedName map[string]string

	// byMember[file_path][scope_name][member_name] = entity_id. Used by
	// structural-ref Format B resolution. A blank string sentinel marks
	// (scope, member) collisions inside the same file. Entities are
	// indexed by splitting their dotted Name on the LAST '.' so multi-
	// level scopes (e.g. "Outer.Inner.foo" → scope="Outer.Inner",
	// member="foo") survive — issue #68.
	byMember map[string]map[string]map[string]string

	// byPackageMember[pkg_dir][scope_name][member_name] = entity_id. Used
	// by issue #148's Go same-package method-dispatch path. Go's compilation
	// unit is the directory, so a method declared in `chi/mux.go` is in the
	// same package as a call site in `chi/tree.go`. byMember alone (file-
	// scoped) misses this; byPackageMember spans sibling files in one dir.
	// Indexed only when an entity carries dotted Name "<scope>.<member>"
	// AND a non-empty SourceFile. A blank-string sentinel marks (pkg, scope,
	// member) collisions so the resolver leaves the stub alone instead of
	// silently picking a wrong overload.
	byPackageMember map[string]map[string]map[string]string
}

// LocationIndex maps file_path -> name -> entity_id, retaining only entries
// that are unique within their file. Returned by BuildLocationIndex.
type LocationIndex map[string]map[string]string

// LocationKindIndex maps file_path -> name -> kind -> entity_id. Used by the
// kind-aware structural-ref / location resolver path to disambiguate
// same-file (file, name) collisions when the relationship supplies a kind
// hint. A blank string value is the ambiguous-within-kind sentinel.
type LocationKindIndex map[string]map[string]map[string]string

// Stats reports how many relationship endpoints the resolver rewrote and how
// many it left as stubs because of ambiguity / missing matches. Surfaced via
// the log line in cmd/archigraph/index.go for instrumentation.
//
// Rewritten/Ambiguous/Unmatched are aggregate counters covering every endpoint
// the resolver inspected (FromID + ToID combined). PORT-2-FIX-4 added the
// per-endpoint counters so callers can tell which side of an edge is failing
// to resolve.
type Stats struct {
	Rewritten int
	Ambiguous int
	Unmatched int

	FromRewritten int
	FromAmbiguous int
	FromUnmatched int
	ToRewritten   int
	ToAmbiguous   int
	ToUnmatched   int

	// VERIFY-2-PREP — every endpoint is also tagged with a Disposition.
	// DispositionCounts holds the tallies; DispositionSamples retains up
	// to 5 distinct representative stubs per disposition so the verbose
	// log can show concrete examples of each bucket. BugRate is the
	// (bug_extractor + bug_resolver) / total ratio surfaced as the v1.0
	// acceptance metric. Total here is the sum of every counter — the
	// number of endpoints the resolver inspected.
	DispositionCounts  map[Disposition]int
	DispositionSamples map[Disposition][]string
	BugRate            float64
}

// initDispositions lazily allocates the disposition maps. Cheap to call on
// every endpoint; we keep it explicit rather than relying on zero values so
// callers reading Stats.DispositionCounts on an unused endpoint never see a
// nil map.
func (s *Stats) initDispositions() {
	if s.DispositionCounts == nil {
		s.DispositionCounts = make(map[Disposition]int, len(AllDispositions))
	}
	if s.DispositionSamples == nil {
		s.DispositionSamples = make(map[Disposition][]string, len(AllDispositions))
	}
}

// recordDisposition adds one endpoint to the disposition tallies and stores
// the stub as a sample if fewer than 5 unique samples have been recorded
// for that disposition.
func (s *Stats) recordDisposition(d Disposition, stub string) {
	s.initDispositions()
	s.DispositionCounts[d]++
	cur := s.DispositionSamples[d]
	if len(cur) >= maxDispositionSamples {
		return
	}
	for _, existing := range cur {
		if existing == stub {
			return
		}
	}
	s.DispositionSamples[d] = append(cur, stub)
}

// finalizeDispositions computes the BugRate field from the per-disposition
// counters. Called once at the end of References / ReferencesEmbedded.
func (s *Stats) finalizeDispositions() {
	if s.DispositionCounts == nil {
		return
	}
	var total int
	for _, n := range s.DispositionCounts {
		total += n
	}
	if total == 0 {
		s.BugRate = 0
		return
	}
	bugs := s.DispositionCounts[DispositionBugExtractor] +
		s.DispositionCounts[DispositionBugResolver]
	s.BugRate = float64(bugs) / float64(total)
}

// ClassifyEndpoints walks the supplied (fromID, toID) pairs and produces a
// Stats value populated only with disposition counters / samples / BugRate.
// The aggregate Rewritten/Ambiguous/Unmatched counters are NOT populated
// because by the time this is called the rewrite has already happened —
// callers wanting those numbers use Stats from References / ReferencesEmbedded.
//
// Endpoint pairs come from doc.Relationships AFTER external synthesis so
// "ext:" placeholders are already in place. allow is the external-package
// allowlist (typically external.IsKnownExternalPackage).
func (idx Index) ClassifyEndpoints(endpoints []EndpointPair, allow ExternalAllowlist) Stats {
	var stats Stats
	for _, ep := range endpoints {
		if ep.FromID != "" {
			d := idx.classifyDispositionLang(ep.FromID, ep.FromOriginal, ep.Language, allow)
			stub := ep.FromOriginal
			if stub == "" {
				stub = ep.FromID
			}
			stats.recordDisposition(d, stub)
		}
		if ep.ToID != "" {
			d := idx.classifyDispositionLang(ep.ToID, ep.ToOriginal, ep.Language, allow)
			stub := ep.ToOriginal
			if stub == "" {
				stub = ep.ToID
			}
			stats.recordDisposition(d, stub)
		}
	}
	stats.finalizeDispositions()
	return stats
}

// EndpointPair carries the post-rewrite IDs and pre-rewrite stubs for one
// relationship's endpoints. Used by ClassifyEndpoints when the caller has
// already finished resolving + synthesising and just wants disposition
// numbers over the final edge state.
type EndpointPair struct {
	FromID       string
	FromOriginal string
	ToID         string
	ToOriginal   string
	// Language is the source language of the relationship (typically read
	// from RelationshipRecord.Properties["language"]). Threaded through to
	// classifyDispositionLang so the per-language dynamic-pattern catalog
	// runs at final-classification time. Issue #90.
	Language string
}

// MergeDispositions sums the per-disposition counts and samples from src
// into dst. Sample lists are deduplicated and capped at 5 entries per
// disposition. BugRate is recomputed from the merged totals. Existing
// counter fields (Rewritten/Ambiguous/Unmatched + per-endpoint variants)
// are NOT touched — callers merge those explicitly.
func MergeDispositions(dst, src *Stats) {
	if dst == nil || src == nil || src.DispositionCounts == nil {
		if dst != nil {
			dst.finalizeDispositions()
		}
		return
	}
	dst.initDispositions()
	for d, n := range src.DispositionCounts {
		dst.DispositionCounts[d] += n
	}
	for d, samples := range src.DispositionSamples {
		cur := dst.DispositionSamples[d]
	sampleLoop:
		for _, s := range samples {
			if len(cur) >= maxDispositionSamples {
				break
			}
			for _, existing := range cur {
				if existing == s {
					continue sampleLoop
				}
			}
			cur = append(cur, s)
		}
		dst.DispositionSamples[d] = cur
	}
	dst.finalizeDispositions()
}

// BuildIndex constructs a (kind, name) -> entity_id lookup from a slice of
// EntityRecords. Records whose ID field is empty are skipped — the caller is
// expected to populate ID with graph.EntityID(...) before calling BuildIndex.
//
// The returned Index handles two kind forms emitted by upstream extractors:
//
//   - Plain kind, e.g. "Function", "Class", "Model".
//   - SCOPE-prefixed kind, e.g. "SCOPE.View", "SCOPE.Service" — emitted by
//     Pass 3 cross-language extractors. The lookup strips the "SCOPE." prefix
//     so a stub like "View:User" matches an entity of kind "SCOPE.View".
func BuildIndex(entities []types.EntityRecord) Index {
	idx := Index{
		byKind:          make(map[string]map[string]string),
		ambigKind:       make(map[string]map[string]bool),
		byName:          make(map[string]string),
		ambigName:       make(map[string]bool),
		nameKinds:       make(map[string]map[string]string),
		byLocation:      make(LocationIndex),
		ambigLocation:   make(map[string]map[string]bool),
		byLocationKind:  make(LocationKindIndex),
		byMember:        make(map[string]map[string]map[string]string),
		byPackageMember: make(map[string]map[string]map[string]string),
		byQualifiedName: make(map[string]string),
	}
	for k := range entities {
		e := &entities[k]
		if e.ID == "" || e.Name == "" {
			continue
		}
		// QualifiedName index — direct lookup for stubs that arrive as a
		// verbatim QualifiedName (issue #100). The markdown extractor
		// emits CONTAINS edges with ToID="<file>::<heading-slug>" which
		// matches the heading entity's QualifiedName exactly, but neither
		// the byKind nor byName paths see it (splitStub on the first ':'
		// produces a non-existent "kind" segment). First writer wins;
		// collisions blank the entry so the resolver leaves the stub.
		if e.QualifiedName != "" {
			if existing, ok := idx.byQualifiedName[e.QualifiedName]; ok && existing != e.ID {
				idx.byQualifiedName[e.QualifiedName] = ""
			} else {
				idx.byQualifiedName[e.QualifiedName] = e.ID
			}
		}

		// Index under both the plain kind and the trimmed kind ("SCOPE.View"
		// → "View"), so stubs can match either form.
		kinds := []string{e.Kind}
		if trimmed := strings.TrimPrefix(e.Kind, scopeKindPrefix); trimmed != e.Kind && trimmed != "" {
			kinds = append(kinds, trimmed)
		}
		// File paths are graph identifiers — keep them in forward-slash
		// form regardless of the host OS (issue #49). Without this a
		// Windows extractor emitting "src\foo\bar.py" indexes against a
		// key that no structural-ref stub will ever request.
		sourceFile := normalizePath(e.SourceFile)
		for _, kind := range kinds {
			if kind == "" {
				continue
			}
			if idx.ambigKind[kind] != nil && idx.ambigKind[kind][e.Name] {
				continue
			}
			bucket := idx.byKind[kind]
			if bucket == nil {
				bucket = make(map[string]string)
				idx.byKind[kind] = bucket
			}
			if existing, ok := bucket[e.Name]; ok && existing != e.ID {
				delete(bucket, e.Name)
				if idx.ambigKind[kind] == nil {
					idx.ambigKind[kind] = make(map[string]bool)
				}
				idx.ambigKind[kind][e.Name] = true
				continue
			}
			bucket[e.Name] = e.ID
		}

		// Track every (name, kind) -> id so the kind-hint fallback can
		// disambiguate when byName flips to ambiguous. The plain entity
		// kind is enough here; SCOPE.* kinds are tracked under both forms
		// to mirror the byKind dual-indexing above.
		nameKindBucket := idx.nameKinds[e.Name]
		if nameKindBucket == nil {
			nameKindBucket = make(map[string]string)
			idx.nameKinds[e.Name] = nameKindBucket
		}
		for _, kind := range kinds {
			if kind == "" {
				continue
			}
			// First writer wins per kind; if a second entity shares the
			// (name, kind) we mark the kind ambiguous for that name by
			// blanking the entry so the hint falls through.
			if existing, ok := nameKindBucket[kind]; ok && existing != e.ID {
				nameKindBucket[kind] = ""
			} else {
				nameKindBucket[kind] = e.ID
			}
		}

		// Location index — (file_path, name) -> entity_id. Same logic as
		// byKind: ambiguous (file, name) tuples are tracked separately so
		// the structural-ref resolver leaves the stub alone.
		if sourceFile != "" {
			// Kind-aware (file, name, kind) bucket — collision-safe under
			// PORT-2-FIX-2 emissions. Indexed under both raw and SCOPE-
			// trimmed kinds to mirror byKind.
			fileKindBucket := idx.byLocationKind[sourceFile]
			if fileKindBucket == nil {
				fileKindBucket = make(map[string]map[string]string)
				idx.byLocationKind[sourceFile] = fileKindBucket
			}
			nameKindBucketLoc := fileKindBucket[e.Name]
			if nameKindBucketLoc == nil {
				nameKindBucketLoc = make(map[string]string)
				fileKindBucket[e.Name] = nameKindBucketLoc
			}
			for _, kind := range kinds {
				if kind == "" {
					continue
				}
				if existing, ok := nameKindBucketLoc[kind]; ok && existing != e.ID {
					nameKindBucketLoc[kind] = "" // ambiguous within (file, name, kind)
				} else {
					nameKindBucketLoc[kind] = e.ID
				}
			}

			if idx.ambigLocation[sourceFile] == nil || !idx.ambigLocation[sourceFile][e.Name] {
				bucket := idx.byLocation[sourceFile]
				if bucket == nil {
					bucket = make(map[string]string)
					idx.byLocation[sourceFile] = bucket
				}
				if existing, ok := bucket[e.Name]; ok && existing != e.ID {
					delete(bucket, e.Name)
					if idx.ambigLocation[sourceFile] == nil {
						idx.ambigLocation[sourceFile] = make(map[string]bool)
					}
					idx.ambigLocation[sourceFile][e.Name] = true
				} else {
					bucket[e.Name] = e.ID
				}
			}

			// Member index — Format B references address a member of an
			// enclosing scope (class/module/etc.) by qualified name. Pass 3
			// records typically encode this as "<scope>.<member>" in the
			// Name field. We split on the LAST '.' so multi-level dotted
			// scopes ("Outer.Inner.foo" — issue #68) bind scope="Outer.Inner"
			// and member="foo". Single-level names ("Foo.bar") still bind
			// scope="Foo", member="bar" — unchanged from issue #45.
			if dot := strings.LastIndexByte(e.Name, dottedNameSep); dot > 0 {
				scope, member := e.Name[:dot], e.Name[dot+1:]
				fileBucket := idx.byMember[sourceFile]
				if fileBucket == nil {
					fileBucket = make(map[string]map[string]string)
					idx.byMember[sourceFile] = fileBucket
				}
				scopeBucket := fileBucket[scope]
				if scopeBucket == nil {
					scopeBucket = make(map[string]string)
					fileBucket[scope] = scopeBucket
				}
				if existing, ok := scopeBucket[member]; ok && existing != e.ID {
					scopeBucket[member] = "" // blank sentinel → ambiguous
				} else {
					scopeBucket[member] = e.ID
				}

				// Package-scoped member index (issue #148). Go's compilation
				// unit is the directory, so methods on the same receiver
				// type spread across sibling files share a package. Index
				// under the dir of sourceFile so a CALLS edge from
				// chi/tree.go can find Mux.handle declared in chi/mux.go.
				// Only Go entities benefit from this (other languages
				// resolve same-class methods via byMember already), but we
				// index unconditionally — a (pkg_dir, scope, member) tuple
				// from another language won't be probed because the
				// receiver_type stamp is Go-extractor-only.
				pkgDir := pkgDirOf(sourceFile)
				if pkgDir != "" {
					pkgBucket := idx.byPackageMember[pkgDir]
					if pkgBucket == nil {
						pkgBucket = make(map[string]map[string]string)
						idx.byPackageMember[pkgDir] = pkgBucket
					}
					pkgScopeBucket := pkgBucket[scope]
					if pkgScopeBucket == nil {
						pkgScopeBucket = make(map[string]string)
						pkgBucket[scope] = pkgScopeBucket
					}
					if existing, ok := pkgScopeBucket[member]; ok && existing != e.ID {
						pkgScopeBucket[member] = "" // ambiguous within (pkg, scope, member)
					} else {
						pkgScopeBucket[member] = e.ID
					}
				}
			}
		}

		// Kind-agnostic name index. Two different entities sharing a name
		// (even across kinds) flips the name to ambiguous.
		if idx.ambigName[e.Name] {
			continue
		}
		if existing, ok := idx.byName[e.Name]; ok && existing != e.ID {
			delete(idx.byName, e.Name)
			idx.ambigName[e.Name] = true
			continue
		}
		idx.byName[e.Name] = e.ID
	}
	return idx
}

// BuildLocationIndex returns a (file_path, name) -> entity_id map built from
// the supplied entity slice. Entries that are not unique within their file
// are dropped. Exposed for callers that only need the location lookup.
func BuildLocationIndex(entities []types.EntityRecord) LocationIndex {
	loc := make(LocationIndex)
	ambig := make(map[string]map[string]bool)
	for k := range entities {
		e := &entities[k]
		if e.ID == "" || e.Name == "" || e.SourceFile == "" {
			continue
		}
		// Forward-slash form so Windows extractors and POSIX stubs hit
		// the same key (issue #49).
		sourceFile := normalizePath(e.SourceFile)
		if ambig[sourceFile] != nil && ambig[sourceFile][e.Name] {
			continue
		}
		bucket := loc[sourceFile]
		if bucket == nil {
			bucket = make(map[string]string)
			loc[sourceFile] = bucket
		}
		if existing, ok := bucket[e.Name]; ok && existing != e.ID {
			delete(bucket, e.Name)
			if ambig[sourceFile] == nil {
				ambig[sourceFile] = make(map[string]bool)
			}
			ambig[sourceFile][e.Name] = true
			continue
		}
		bucket[e.Name] = e.ID
	}
	return loc
}

// Lookup resolves a stub string to an entity ID. The stub is split on the
// first ':' into (kind, name). If only the right-hand side is supplied (no
// ':' present) we fall back to the kind-agnostic name index.
//
// Returns (id, true) only when the lookup is unambiguous. Returns
// ("", false) when the stub has zero matches OR multiple matches — the
// caller leaves the original string in place in either case but tracks the
// outcome in Stats.
func (idx Index) Lookup(stub string) (string, bool) {
	if stub == "" {
		return "", false
	}
	// Direct QualifiedName hit short-circuits the kind/name paths
	// (issue #100). Blank-string sentinel = ambiguous → treat as miss.
	if qid, ok := idx.byQualifiedName[stub]; ok {
		if qid == "" {
			return "", false
		}
		return qid, true
	}
	kind, name := splitStub(stub)
	if kind != "" {
		if bucket, ok := idx.byKind[kind]; ok {
			if id, ok := bucket[name]; ok {
				return id, true
			}
		}
		// Ambiguous within this kind: fall through to the kind-agnostic
		// path; it succeeds only if the bare name is itself unique.
	}
	// Kind-agnostic fallback: bare name (no prefix) OR missed kind lookup.
	lookupName := name
	if kind == "" {
		lookupName = stub
	}
	if id, ok := idx.byName[lookupName]; ok {
		return id, true
	}
	return "", false
}

// LookupStatus reports whether a stub is unambiguous, ambiguous, or unmatched.
// Used by References to populate Stats counters without doing two passes.
func (idx Index) LookupStatus(stub string) (id string, status int) {
	return idx.LookupStatusHint(stub, "")
}

// LookupStatusHint is LookupStatus with an optional relationship-kind hint.
// The hint is the RelationshipRecord.Kind value (e.g. "EXTENDS", "CALLS"),
// not the entity kind. When the bare-name path would otherwise be ambiguous
// the hint biases the lookup toward the entity-kind family typically
// targeted by that relationship. The hint is ignored when the structural-ref
// path or an explicit Kind: prefix already resolves.
//
// When passed "" the function behaves exactly like LookupStatus.
func (idx Index) LookupStatusHint(stub, relKind string) (id string, status int) {
	if stub == "" {
		return "", statusUnmatched
	}

	// Direct QualifiedName match (issue #100). Some extractors — markdown
	// CONTAINS edges, code-block-relative references — emit ToIDs that are
	// the target entity's QualifiedName verbatim. Probing the QualifiedName
	// index first short-circuits the structural / kind / name paths for
	// these unambiguous exact hits. A blank-string sentinel means the
	// QualifiedName collided across entities; treat as ambiguous.
	if qid, ok := idx.byQualifiedName[stub]; ok {
		if qid == "" {
			return "", statusAmbiguous
		}
		return qid, statusRewritten
	}

	// Structural-ref forms (Format A / B). Recognised by the "scope:"
	// prefix and resolved through the location/member indexes — bypasses
	// the kind / name path entirely.
	if id, st, handled := idx.lookupStructural(stub); handled {
		return id, st
	}

	kind, name := splitStub(stub)
	if kind != "" {
		if bucket, ok := idx.byKind[kind]; ok {
			if id, ok := bucket[name]; ok {
				return id, statusRewritten
			}
		}
		if idx.ambigKind[kind] != nil && idx.ambigKind[kind][name] {
			return "", statusAmbiguous
		}
	}
	lookupName := name
	if kind == "" {
		lookupName = stub
	}
	if id, ok := idx.byName[lookupName]; ok {
		return id, statusRewritten
	}
	if idx.ambigName[lookupName] {
		// Ambiguous bare-name. Try the kind hint: pick a family that
		// the relKind biases toward, and if exactly one entity with this
		// name lives in that family, resolve to it.
		if id, ok := idx.lookupByKindHint(lookupName, relKind); ok {
			return id, statusRewritten
		}
		return "", statusAmbiguous
	}
	return "", statusUnmatched
}

// componentKindFamily / operationKindFamily are the entity-kind families
// the hint resolver biases toward for type-shaped vs call-shaped edges.
// Centralising the slices keeps hintKinds and structuralKindFamilies in
// agreement (issue #49).
var (
	componentKindFamily = []string{
		"Component", "Class", "View", "Model",
		scopeKindPrefix + "Component",
		scopeKindPrefix + "View",
		scopeKindPrefix + "Model",
	}
	operationKindFamily = []string{
		"Operation", "Function", "Method",
		scopeKindPrefix + "Operation",
	}
)

// hintKinds returns the entity-kind families preferred for a given
// relationship kind. EXTENDS / IMPLEMENTS prefer Component-shaped kinds;
// CALLS prefers Operation-shaped kinds. Everything else returns nil.
func hintKinds(relKind string) []string {
	switch strings.ToUpper(relKind) {
	case "EXTENDS", "IMPLEMENTS":
		return componentKindFamily
	case "CALLS":
		return operationKindFamily
	}
	return nil
}

// lookupByKindHint disambiguates a name using the relKind hint. Returns
// (id, true) only when exactly one entity in the hinted family has this
// name; otherwise ("", false).
func (idx Index) lookupByKindHint(name, relKind string) (string, bool) {
	families := hintKinds(relKind)
	if len(families) == 0 {
		return "", false
	}
	bucket := idx.nameKinds[name]
	if len(bucket) == 0 {
		return "", false
	}
	var match string
	for _, k := range families {
		id := bucket[k]
		if id == "" {
			continue
		}
		if match != "" && match != id {
			// Two distinct entities in the hinted family share this
			// name — hint cannot disambiguate.
			return "", false
		}
		match = id
	}
	if match == "" {
		return "", false
	}
	return match, true
}

// lookupStructural resolves Format A / B references. Returns handled=false
// when the stub doesn't start with "scope:" so the caller falls through to
// the normal Kind:Name / bare-name path.
//
// Format A: scope:<kind>:<subtype>:<lang>:<file_path>:<name>
// Format B: scope:<kind>:<subtype>:<lang>:<file_path>:<scope_name>#<member_name>
func (idx Index) lookupStructural(stub string) (id string, status int, handled bool) {
	if !strings.HasPrefix(stub, stubPrefixScope) {
		return "", statusSkip, false
	}
	parts := strings.SplitN(stub, stubDelim, stubScopeSegments)
	if len(parts) != stubScopeSegments {
		return "", statusUnmatched, true
	}
	scopeKind := parts[stubScopeKindIndex] // e.g. "component", "operation"
	// Stubs encode file paths in forward-slash form; normalise defensively
	// in case an upstream emitter slipped an OS-native separator through
	// (issue #49).
	filePath := normalizePath(parts[stubScopeFileIndex])
	tail := parts[stubScopeTailIndex]
	if filePath == "" || tail == "" {
		return "", statusUnmatched, true
	}

	// Format B: tail contains stubMemberDelim → (scope_name, member_name).
	if hash := strings.IndexByte(tail, stubMemberDelim); hash >= 0 {
		scopeName, memberName := tail[:hash], tail[hash+1:]
		if scopeName == "" || memberName == "" {
			return "", statusUnmatched, true
		}
		fileBucket := idx.byMember[filePath]
		if fileBucket == nil {
			return "", statusUnmatched, true
		}
		scopeBucket := fileBucket[scopeName]
		if scopeBucket == nil {
			return "", statusUnmatched, true
		}
		if id, ok := scopeBucket[memberName]; ok {
			if id == "" {
				return "", statusAmbiguous, true
			}
			return id, statusRewritten, true
		}
		return "", statusUnmatched, true
	}

	// Format A: tail is the entity name. Try the kind-aware location
	// index first using the structural-ref's scope-kind segment; this
	// resolves PORT-2-FIX-2 same-file collisions.
	if id, ok := idx.lookupLocationKind(filePath, tail, structuralKindFamilies(scopeKind)); ok {
		return id, statusRewritten, true
	}
	if idx.ambigLocation[filePath] != nil && idx.ambigLocation[filePath][tail] {
		return "", statusAmbiguous, true
	}
	if bucket, ok := idx.byLocation[filePath]; ok {
		if id, ok := bucket[tail]; ok {
			return id, statusRewritten, true
		}
	}
	return "", statusUnmatched, true
}

// structuralKindFamilies maps a scope-kind segment from a structural ref
// (e.g. "component", "operation") to the entity-kind families it might be
// indexed under. Returns nil for unknown segments.
func structuralKindFamilies(scopeKind string) []string {
	switch strings.ToLower(scopeKind) {
	case "component":
		return componentKindFamily
	case "operation":
		return operationKindFamily
	}
	return nil
}

// lookupLocationKind picks an entity by (file, name) constrained to the
// supplied kind families. Returns (id, true) only when exactly one family
// resolves to a non-blank entity ID for this (file, name).
func (idx Index) lookupLocationKind(filePath, name string, families []string) (string, bool) {
	if len(families) == 0 {
		return "", false
	}
	fileBucket := idx.byLocationKind[filePath]
	if fileBucket == nil {
		return "", false
	}
	nameBucket := fileBucket[name]
	if len(nameBucket) == 0 {
		return "", false
	}
	var match string
	for _, k := range families {
		id := nameBucket[k]
		if id == "" {
			continue
		}
		if match != "" && match != id {
			return "", false
		}
		match = id
	}
	if match == "" {
		return "", false
	}
	return match, true
}

// looksLikeSourceFilePath reports whether s has the shape of a source
// code file path — a forward-slashed path ending in one of the
// well-known per-language extensions. Used by classifyDispositionLang
// to route IMPORTS-edge FromIDs (which every language extractor sets
// to the importing file's path) into DispositionDynamic rather than
// DispositionBugExtractor.
//
// Conservative checks: must contain at least one '/', must NOT contain
// ':' (would be a structural-ref), must NOT start with '/' (absolute
// system paths are not extractor-emitted, and disqualifying them keeps
// us from accepting accidental Unix paths that escaped a higher
// layer), and must end with one of the catalogued source extensions.
//
// The extension list is intentionally narrow — only extensions actively
// used by the per-language extractors that emit IMPORTS edges with a
// raw file-path FromID.
func looksLikeSourceFilePath(s string) bool {
	if s == "" || s[0] == '/' {
		return false
	}
	if strings.ContainsAny(s, ": \\") {
		return false
	}
	if !strings.ContainsRune(s, '/') {
		return false
	}
	// Compare against the small allowlist of source-file extensions
	// the IMPORTS-emitting extractors actually use. Adding new
	// languages here is a one-line change in lockstep with the
	// extractor that introduces the new extension.
	for _, ext := range sourceFileExtensions {
		if strings.HasSuffix(s, ext) {
			return true
		}
	}
	return false
}

// sourceFileExtensions is the allowlist of file-path suffixes the
// looksLikeSourceFilePath heuristic accepts. Curated from the set of
// extractors that emit raw file-path FromIDs on IMPORTS edges.
var sourceFileExtensions = []string{
	".py", ".java", ".kt", ".kts", ".scala", ".groovy",
	".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs",
	".go", ".rs", ".rb", ".php", ".cs", ".cpp", ".cc", ".c", ".h", ".hpp",
	".swift", ".dart", ".lua", ".ex", ".exs", ".clj", ".cljs", ".cljc",
	".zig", ".sql",
}

// isHeuristicScopeStub reports whether s is a short-form structural-ref
// emitted by a cross-language extractor whose target is by design not a
// single graph entity. Such stubs should land in DispositionDynamic, not
// the bug buckets — see classifyDispositionLang for the categorisation
// rationale. Issue #89.
//
// Issue #94 follow-up: the original list over-reached. Prefixes that are
// in fact concrete (file-path-keyed, with a verifiable producer in the
// extractors that maps to a real entity ID) MUST NOT be routed here, or
// the apparent bug-rate drop becomes artificial. Verified producers for
// scope:operation:, scope:component:project:, scope:dataaccess:,
// scope:endpoint:, and scope:schema: emit concrete file-keyed IDs and
// were removed from this list.
//
// Kept: short-form stubs whose target genuinely cannot resolve to a
// single entity at link time (runtime-built URLs for http callers,
// unresolved local imports, file/coverage wrappers).
func isHeuristicScopeStub(s string) bool {
	if !strings.HasPrefix(s, stubPrefixScope) {
		return false
	}
	switch {
	// testmap coverage entity (the wrapper Pattern itself).
	case strings.HasPrefix(s, "scope:testcoverage:"):
		return true
	// imports cross-language extractor — local relative imports the
	// extractor doesn't resolve to a specific file.
	case strings.HasPrefix(s, "scope:component:import:local:"):
		return true
	// http-client cross-language extractor — http-caller component scope
	// where the target URL is runtime-built and cannot be tied to a
	// specific external_api entity.
	case strings.HasPrefix(s, "scope:component:http_caller:"):
		return true
	// imports cross-language extractor — file component scope. These are
	// internal "the file is a component" markers; targets aren't real
	// individual entities.
	case strings.HasPrefix(s, "scope:component:file:"):
		return true
	}
	return false
}

// splitStub splits a stub string on the first ':' into (kind, name). If no
// ':' is present the full string is returned as the name and kind is empty.
func splitStub(s string) (kind, name string) {
	if i := strings.IndexByte(s, stubDelim[0]); i >= 0 {
		return s[:i], s[i+1:]
	}
	return "", s
}

// lookupPackageMember probes the byPackageMember index (issue #148). When
// pkgDir + receiverType + member resolves to a single entity ID, returns
// (id, true). Returns ("", false) for missing entries; returns ("", true)
// for the blank-sentinel ambiguous case so the caller can leave the stub
// alone instead of falling back to global bare-name lookup (which would
// risk binding to a foreign-package method of the same name).
func (idx Index) lookupPackageMember(pkgDir, receiverType, member string) (string, bool) {
	if pkgDir == "" || receiverType == "" || member == "" {
		return "", false
	}
	pkgBucket := idx.byPackageMember[pkgDir]
	if pkgBucket == nil {
		return "", false
	}
	scopeBucket := pkgBucket[receiverType]
	if scopeBucket == nil {
		return "", false
	}
	id, ok := scopeBucket[member]
	if !ok {
		return "", false
	}
	// Blank sentinel = ambiguous; treat as "handled but not rewritten" so
	// the caller does NOT fall through to a global bare-name lookup that
	// might silently pick a foreign-package overload.
	if id == "" {
		return "", true
	}
	return id, true
}

// rewriteOne resolves a single endpoint reference. It returns the (possibly
// rewritten) ID string and the status code from LookupStatusHint. Hex IDs
// and empty strings short-circuit with a zero status, signalling "skip".
func (idx Index) rewriteOne(ref, relKind string) (string, int) {
	if ref == "" || isHexID(ref) {
		return ref, 0
	}
	id, st := idx.LookupStatusHint(ref, relKind)
	if st == statusRewritten {
		return id, st
	}
	return ref, st
}

// nameExists reports whether the supplied name appears anywhere in the
// graph, regardless of kind. Used by the disposition classifier to
// distinguish bug-extractor (no entity by this name exists) from
// bug-resolver (entity exists but lookup failed).
func (idx Index) nameExists(name string) bool {
	if name == "" {
		return false
	}
	if _, ok := idx.byName[name]; ok {
		return true
	}
	if idx.ambigName[name] {
		return true
	}
	if bucket, ok := idx.nameKinds[name]; ok && len(bucket) > 0 {
		return true
	}
	return false
}

// BugResolverDiag is a diagnostic record describing why a stub flagged as
// bug-resolver failed to bind. Returned by DiagnoseBugResolver. Fields are
// stable for the lifetime of issue #92; callers should not depend on
// values being non-empty across releases.
type BugResolverDiag struct {
	// Category is a short token suitable for histogram bucketing. One of:
	//   "kind-mismatch"          — stub had Kind:Name; that kind exists but
	//                              not for this name; bare-name is also
	//                              ambiguous or missing.
	//   "ambig-kind"             — Kind:Name where (kind, name) is ambiguous.
	//   "ambig-bare-no-hint"     — bare-name lookup ambiguous and no relKind
	//                              hint was supplied or the hint had no
	//                              registered family.
	//   "ambig-bare-hint-fail"   — relKind hint family didn't disambiguate
	//                              (zero or >=2 candidates in the family).
	//   "ambig-qualified"        — stub matched a QualifiedName but it
	//                              collided with another entity.
	//   "unknown"                — none of the above matched; should be rare.
	Category string
	// Name is the bare leaf name probed against byName / nameKinds.
	Name string
	// StubKind is the Kind: prefix segment when present (else "").
	StubKind string
	// KindsPresent is the sorted list of entity kinds the graph holds for
	// this Name. A value with multiple entries plus a missing StubKind
	// match is the "kind-mismatch" pattern; a single entry is an
	// "ambig-bare-*" pattern (multiple entities share that name+kind).
	KindsPresent []string
	// RelKindHint is the relationship-kind hint that was tried (e.g.
	// CALLS, EXTENDS). Empty when the caller didn't supply one.
	RelKindHint string
	// HintFamily lists the entity-kind families the hint biases toward.
	// Empty when relKind has no registered hint or no hint was passed.
	HintFamily []string
}

// DiagnoseBugResolver returns a BugResolverDiag describing the failure
// mode for a stub that classifyDispositionLang labelled
// DispositionBugResolver. The classifier's own decision is NOT re-checked
// here; callers feed only stubs they have already classified as
// bug-resolver. Issue #92 — diagnostic instrumentation, not a hot path.
func (idx Index) DiagnoseBugResolver(originalStub, relKind string) BugResolverDiag {
	diag := BugResolverDiag{Category: "unknown", RelKindHint: relKind}
	if originalStub == "" {
		return diag
	}

	// Direct QualifiedName collision sentinel — byQualifiedName carries a
	// blank string when two entities share the same QualifiedName.
	if qid, ok := idx.byQualifiedName[originalStub]; ok && qid == "" {
		diag.Category = "ambig-qualified"
		diag.Name = originalStub
		return diag
	}

	kind, name := splitStub(originalStub)
	if strings.HasPrefix(originalStub, stubPrefixScope) {
		parts := strings.SplitN(originalStub, stubDelim, stubScopeSegments)
		if len(parts) == stubScopeSegments {
			tail := parts[stubScopeTailIndex]
			if hash := strings.IndexByte(tail, stubMemberDelim); hash >= 0 {
				name = tail[hash+1:]
			} else {
				name = tail
			}
			kind = ""
		}
	}
	diag.Name = name
	diag.StubKind = kind

	if bucket, ok := idx.nameKinds[name]; ok {
		kinds := make([]string, 0, len(bucket))
		for k := range bucket {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		diag.KindsPresent = kinds
	}

	families := hintKinds(relKind)
	diag.HintFamily = families

	switch {
	case kind != "":
		// Kind: prefix path. Kind-bucket missed for this name. Two
		// shapes: either the (kind, name) tuple is itself ambiguous, or
		// the name lives under DIFFERENT kinds entirely.
		if idx.ambigKind[kind] != nil && idx.ambigKind[kind][name] {
			diag.Category = "ambig-kind"
			return diag
		}
		diag.Category = "kind-mismatch"
		return diag
	case idx.ambigName[name]:
		// Bare-name ambiguous globally.
		if len(families) == 0 {
			diag.Category = "ambig-bare-no-hint"
			return diag
		}
		diag.Category = "ambig-bare-hint-fail"
		return diag
	case len(diag.KindsPresent) > 0:
		// nameKinds carries this name (so nameExists returned true) but
		// neither byName nor ambigName tracks it — a same-(name,kind)
		// duplicate registered as a blank-string sentinel inside a
		// nameKinds bucket. Treat as ambig-kind for histogram purposes.
		diag.Category = "ambig-kind"
		return diag
	}
	return diag
}

// classifyDisposition returns the Disposition for an endpoint after the
// resolver has finished with it. resolvedID is the value the endpoint now
// carries (post-rewrite); originalStub is the value the endpoint had on
// entry. allow is the optional external-package allowlist.
//
// Equivalent to classifyDispositionLang with no caller-supplied language.
// Language is inferred from the stub itself (structural-ref `<lang>:`
// segment) when possible.
func (idx Index) classifyDisposition(resolvedID, originalStub string, allow ExternalAllowlist) Disposition {
	return idx.classifyDispositionLang(resolvedID, originalStub, "", allow)
}

// classifyDispositionLang is classifyDisposition with an explicit language
// tag (typically pulled from RelationshipRecord.Properties["language"] or
// the equivalent edge-level field). The language gates which per-language
// dynamic-dispatch catalog runs.
//
// Order of checks matters:
//  1. Already a 16-char hex → Resolved.
//  2. Dynamic-pattern match on the ORIGINAL stub (gated by language) →
//     Dynamic. Runs BEFORE the external-prefix check so reflection
//     builtins that the external synthesiser also tags as `ext:<pkg>`
//     (Python `getattr` / `setattr` / `eval` / `exec`, JS `Function`,
//     etc.) land in the dynamic bucket — they are intrinsically
//     reflective dispatch, not real external imports (Refs #95).
//  3. "ext:<pkg>" prefix → ExternalKnown / ExternalUnknown depending on allow.
//  4. Stub of form "Kind:Name" or bare "Name" → BugExtractor when the name
//     has zero entities in the graph; BugResolver when it does.
//  5. Anything else → Unclassified.
func (idx Index) classifyDispositionLang(resolvedID, originalStub, lang string, allow ExternalAllowlist) Disposition {
	if isHexID(resolvedID) {
		return DispositionResolved
	}
	// Dynamic-pattern check runs BEFORE the external-prefix check (Refs
	// #95). The external synthesiser stamps reflection builtins like
	// `getattr` / `setattr` / `eval` with an `ext:` prefix because they
	// happen to live in the stdlib stop-list, but they are dynamic
	// dispatch by nature — not real external imports. Matching the
	// original (pre-synth) stub against the per-language dynamic catalog
	// here promotes them out of `external-unknown` and into `dynamic`.
	effLang := lang
	if effLang == "" {
		effLang = inferLangFromStub(originalStub)
	}
	if isDynamicPatternLang(originalStub, effLang) {
		return DispositionDynamic
	}
	if strings.HasPrefix(resolvedID, stubPrefixExternal) {
		pkg := strings.TrimPrefix(resolvedID, stubPrefixExternal)
		// Collapse dotted submodules to root for the allowlist check.
		root := pkg
		if dot := strings.IndexByte(pkg, dottedNameSep); dot > 0 {
			root = pkg[:dot]
		}
		if allow != nil && (allow(pkg) || allow(root)) {
			return DispositionExternalKnown
		}
		return DispositionExternalUnknown
	}
	// Endpoint still carries its original stub (resolver left it alone).
	// Language preference order: caller-supplied tag (from the edge's
	// Properties["language"], threaded through ReferencesWithAllowlist),
	// then structural-ref `<lang>:` segment as fallback. Non-structural
	// stubs without a caller-supplied language run only the cross-language
	// catalog — see isDynamicPatternLang.
	// Issue #89 — short structural-ref stubs emitted by cross-language
	// extractors that the resolver intentionally leaves untouched (they
	// don't have the 6-segment scope:<kind>:<subtype>:<lang>:<file>:<tail>
	// shape, so rewriteOne can't index them). They are NOT extractor bugs:
	//
	//   - scope:operation:<file>#<name> (testmap) — test-to-production
	//     mapping inferred from a regex over test bodies; the production
	//     symbol may legitimately live in a file the convention guesser
	//     can't predict (e.g. tests/test_basic.py → src/click/core.py).
	//   - scope:component:import:local:<module> (imports) — Python relative
	//     import that the cross-language extractor records without resolving
	//     to a specific file.
	//   - scope:testcoverage:..., scope:dataaccess:..., scope:endpoint:...
	//     same family, all pattern entities pointing at heuristically
	//     identified production scopes that aren't a single graph entity.
	//
	// Tagging them DispositionDynamic is the right bucket: by design these
	// edges aren't resolvable by static name lookup. They keep the v1.0
	// bug-rate metric honest while leaving the edges visible in graph.json.
	if isHeuristicScopeStub(originalStub) {
		return DispositionDynamic
	}
	// Issue #120 — IMPORTS edges across every language extractor
	// emit FromID = the importing file's source path (the file the
	// import statement lives in). The path itself is not a missing
	// entity — it's a structural identifier the extractor uses to
	// link the import to its origin file. Without this branch every
	// IMPORTS edge contributes one bug-extractor count for the
	// FromID endpoint, regardless of whether the target resolved.
	// Treat raw source-file paths as DispositionDynamic for the same
	// reason `scope:component:file:<path>` is — both are
	// extractor-internal structural markers, not extractor bugs.
	if looksLikeSourceFilePath(originalStub) {
		return DispositionDynamic
	}
	// Strip a "Kind:" prefix when present so the name-existence check is
	// kind-agnostic. Structural-ref ("scope:...") stubs pull their name
	// from the trailing segment after the last ':' or '#'.
	_, name := splitStub(originalStub)
	if strings.HasPrefix(originalStub, stubPrefixScope) {
		// scope:<kind>:<subtype>:<lang>:<file>:<tail>
		parts := strings.SplitN(originalStub, stubDelim, stubScopeSegments)
		if len(parts) == stubScopeSegments {
			tail := parts[stubScopeTailIndex]
			if hash := strings.IndexByte(tail, stubMemberDelim); hash >= 0 {
				name = tail[hash+1:]
			} else {
				name = tail
			}
		}
	}
	if name == "" {
		return DispositionUnclassified
	}
	if idx.nameExists(name) {
		return DispositionBugResolver
	}
	return DispositionBugExtractor
}

// applyEndpointStats records a single endpoint's outcome into the Stats
// counters, updating both the per-endpoint totals and the aggregate ones.
func applyEndpointStats(stats *Stats, status int, isFrom bool) {
	switch status {
	case statusRewritten:
		stats.Rewritten++
		if isFrom {
			stats.FromRewritten++
		} else {
			stats.ToRewritten++
		}
	case statusAmbiguous:
		stats.Ambiguous++
		if isFrom {
			stats.FromAmbiguous++
		} else {
			stats.ToAmbiguous++
		}
	case statusUnmatched:
		stats.Unmatched++
		if isFrom {
			stats.FromUnmatched++
		} else {
			stats.ToUnmatched++
		}
	}
}

// References rewrites ToID and FromID values in rels in place. It returns
// per-endpoint stats — one rel with both endpoints rewritten counts twice in
// Stats.Rewritten (once per endpoint). The 16-char hex IDs already present
// (matching the shape of graph.EntityID output) are left untouched.
//
// This wrapper preserves the pre-VERIFY-2-PREP signature; callers that want
// disposition tagging should use ReferencesWithAllowlist.
func References(rels []types.RelationshipRecord, idx Index) Stats {
	return ReferencesWithAllowlist(rels, idx, nil)
}

// ReferencesWithAllowlist is References with an optional allowlist for
// classifying "ext:<pkg>" endpoints as ExternalKnown vs ExternalUnknown.
// A nil allowlist treats every external as Unknown.
func ReferencesWithAllowlist(rels []types.RelationshipRecord, idx Index, allow ExternalAllowlist) Stats {
	var stats Stats
	for k := range rels {
		r := &rels[k]
		lang := relLanguage(r)
		if r.FromID != "" && !isHexID(r.FromID) {
			orig := r.FromID
			newID, st := idx.rewriteOne(r.FromID, r.Kind)
			r.FromID = newID
			applyEndpointStats(&stats, st, true)
			d := idx.classifyDispositionLang(r.FromID, orig, lang, allow)
			stats.recordDisposition(d, orig)
		} else if isHexID(r.FromID) {
			stats.recordDisposition(DispositionResolved, r.FromID)
		}
		if r.ToID != "" && !isHexID(r.ToID) {
			orig := r.ToID
			newID, st := idx.rewriteOne(r.ToID, r.Kind)
			r.ToID = newID
			applyEndpointStats(&stats, st, false)
			d := idx.classifyDispositionLang(r.ToID, orig, lang, allow)
			stats.recordDisposition(d, orig)
		} else if isHexID(r.ToID) {
			stats.recordDisposition(DispositionResolved, r.ToID)
		}
	}
	stats.finalizeDispositions()
	return stats
}

// relLanguage extracts the source-language tag for a RelationshipRecord.
// Looks first at Properties["language"] (the canonical key emitted by the
// per-language extractors), then Properties["lang"] (legacy alias), then
// returns "" so the classifier falls back to structural-ref inference.
func relLanguage(r *types.RelationshipRecord) string {
	if r == nil || r.Properties == nil {
		return ""
	}
	if v, ok := r.Properties[propLanguage]; ok && v != "" {
		return v
	}
	if v, ok := r.Properties[propLang]; ok && v != "" {
		return v
	}
	return ""
}

// ReferencesEmbedded walks every EntityRecord's embedded Relationships slice
// and applies the same resolver. Pass 1 extractors emit cross-file CALLS
// edges as embedded relationships, so this is where most of the rewriting
// happens on real codebases.
//
// PORT-2-FIX-4 extends this function to rewrite FromID in addition to ToID.
// Pass 3 cross-language extractors increasingly emit edges where the source
// endpoint is itself a stub (e.g. structural-ref Format A targeting an
// entity in another file). When FromID is empty the caller is still
// expected to substitute the parent entity ID at edge-emission time.
func ReferencesEmbedded(records []types.EntityRecord, idx Index) Stats {
	return ReferencesEmbeddedWithAllowlist(records, idx, nil)
}

// ReferencesEmbeddedWithAllowlist is ReferencesEmbedded with an optional
// external-package allowlist for disposition classification.
func ReferencesEmbeddedWithAllowlist(records []types.EntityRecord, idx Index, allow ExternalAllowlist) Stats {
	var stats Stats
	for k := range records {
		rels := records[k].Relationships
		// Embedded relationships inherit the parent entity's language when
		// the edge itself doesn't carry one — Pass 1 extractors emit edges
		// without a language property because their parent entity already
		// pins it.
		parentLang := records[k].Language
		// Issue #148 — same-package method-dispatch lookup needs the caller's
		// package directory. Embedded edges are anchored on records[k] so the
		// parent's SourceFile is the caller file.
		parentPkgDir := pkgDirOf(normalizePath(records[k].SourceFile))
		for j := range rels {
			r := &rels[j]
			lang := relLanguage(r)
			if lang == "" {
				lang = parentLang
			}
			if r.FromID != "" && !isHexID(r.FromID) {
				orig := r.FromID
				newID, st := idx.rewriteOne(r.FromID, r.Kind)
				r.FromID = newID
				applyEndpointStats(&stats, st, true)
				d := idx.classifyDispositionLang(r.FromID, orig, lang, allow)
				stats.recordDisposition(d, orig)
			} else if isHexID(r.FromID) {
				stats.recordDisposition(DispositionResolved, r.FromID)
			}
			if r.ToID != "" && !isHexID(r.ToID) {
				orig := r.ToID
				// Issue #148 — Go same-package method dispatch. When the
				// extractor stamped Properties["receiver_type"] on a CALLS
				// edge (a method calling another method on its own receiver),
				// probe the package-scoped member index FIRST so a bare-name
				// target like "handle" binds to the local "<pkg>/Mux.handle"
				// rather than colliding with same-named methods elsewhere.
				if recvType := r.Properties["receiver_type"]; recvType != "" && parentPkgDir != "" {
					if id, ok := idx.lookupPackageMember(parentPkgDir, recvType, r.ToID); ok {
						if id != "" {
							r.ToID = id
							applyEndpointStats(&stats, statusRewritten, false)
							d := idx.classifyDispositionLang(r.ToID, orig, lang, allow)
							stats.recordDisposition(d, orig)
							continue
						}
						// Ambiguous within (pkg, recv, member) — fall through
						// to record as unmatched (preserve the stub).
					}
				}
				newID, st := idx.rewriteOne(r.ToID, r.Kind)
				r.ToID = newID
				applyEndpointStats(&stats, st, false)
				d := idx.classifyDispositionLang(r.ToID, orig, lang, allow)
				stats.recordDisposition(d, orig)
			} else if isHexID(r.ToID) {
				stats.recordDisposition(DispositionResolved, r.ToID)
			}
		}
	}
	stats.finalizeDispositions()
	return stats
}

// isHexID reports whether s is a 16-char lower-hex string — the shape of
// graph.EntityID() output. Anything matching this shape is assumed to be an
// already-resolved entity ID and is left untouched.
func isHexID(s string) bool {
	if len(s) != hexIDLen {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
