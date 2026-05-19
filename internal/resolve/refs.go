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
		// Issue #432 — Python relative-import targets. The Python extractor
		// preserves the leading dot for `from .compat import urlparse` and
		// `from ..views import View` (extractor.go:653-655); the resolver
		// then sees a bare ToID like `.compat.urlparse` or `..views.View`.
		// One SCOPE.Component placeholder is emitted per importing file
		// for the same module path, so bare-name lookup is ambiguous in
		// any project where two or more files share the relative-import
		// path — driving the edge to bug-resolver despite the placeholder
		// being bookkeeping rather than the imported symbol's source.
		// A leading-dot bare path is unambiguously a relative-import
		// reference; route to Dynamic, mirroring the precedent for
		// `scope:component:import:local:` (isHeuristicScopeStub) which
		// the cross-language imports extractor emits for the same shape.
		regexp.MustCompile(`^\.+[\w.]*$`),
		// Bare-identifier forms: per-language extractors emit only the
		// leaf callee identifier (e.g. ToID="getattr") for `getattr(...)`
		// call sites. Without bare-name anchors none of the parens-
		// requiring patterns below ever match real stubs (issue #90).
		regexp.MustCompile(`^getattr$`),
		regexp.MustCompile(`^setattr$`),
		regexp.MustCompile(`^hasattr$`),
		regexp.MustCompile(`^delattr$`),
		// Wave-4 (Python) — `super().<method>(...)` chains. The Python
		// extractor strips the `super()` receiver to a literal `super`
		// segment, yielding stubs like `super.render`, `super.__init__`,
		// `super.to_info_dict`. The dispatch target is the MRO-resolved
		// parent method which depends on the (often-external) base
		// class — overwhelmingly Dynamic-by-design, not an extractor bug.
		regexp.MustCompile(`^super\.[A-Za-z_][A-Za-z0-9_]*`),
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

		// Flask app-factory + decorator DSL (issue #420). Flask exposes
		// its routing / lifecycle / CLI surface as decorator and method
		// calls on `app = Flask(__name__)`, blueprints (`bp = Blueprint(...)`),
		// and `bp.cli` AppGroup instances. The Python extractor strips
		// the receiver and the resolver sees only the bare leaf
		// identifier (e.g. `@app.route("/")` → ToID="route"). Without a
		// per-language anchor those land in bug-extractor and drove
		// flask + flask-realworld bug-rate to 43.93% / 43.63%. The
		// per-language gate (Python only) keeps these names from
		// shadowing user methods in other ecosystems — `route`, `errorhandler`,
		// `before_request` etc. would collide trivially in Go / JS / Ruby.
		//
		// Mirrors the Rails ActionController DSL approach from #107.
		regexp.MustCompile(`^route$`),                   // @app.route(...) / @bp.route(...)
		regexp.MustCompile(`^add_url_rule$`),            // app.add_url_rule(...)
		regexp.MustCompile(`^register_blueprint$`),      // app.register_blueprint(bp)
		regexp.MustCompile(`^before_request$`),          // @app.before_request
		regexp.MustCompile(`^before_first_request$`),    // @app.before_first_request (legacy)
		regexp.MustCompile(`^after_request$`),           // @app.after_request
		regexp.MustCompile(`^teardown_request$`),        // @app.teardown_request
		regexp.MustCompile(`^teardown_appcontext$`),     // @app.teardown_appcontext
		regexp.MustCompile(`^errorhandler$`),            // @app.errorhandler(404)
		regexp.MustCompile(`^register_error_handler$`),  // app.register_error_handler(...)
		regexp.MustCompile(`^shell_context_processor$`), // @app.shell_context_processor
		regexp.MustCompile(`^context_processor$`),       // @app.context_processor
		regexp.MustCompile(`^template_filter$`),         // @app.template_filter(...)
		regexp.MustCompile(`^template_test$`),           // @app.template_test(...)
		regexp.MustCompile(`^template_global$`),         // @app.template_global(...)
		regexp.MustCompile(`^url_value_preprocessor$`),  // @app.url_value_preprocessor
		regexp.MustCompile(`^url_defaults$`),            // @app.url_defaults
		regexp.MustCompile(`^before_app_request$`),      // blueprint scoped variants
		regexp.MustCompile(`^before_app_first_request$`),
		regexp.MustCompile(`^after_app_request$`),
		regexp.MustCompile(`^teardown_app_request$`),
		regexp.MustCompile(`^app_errorhandler$`),
		regexp.MustCompile(`^app_context_processor$`),
		regexp.MustCompile(`^app_template_filter$`),
		regexp.MustCompile(`^app_template_test$`),
		regexp.MustCompile(`^app_template_global$`),
		regexp.MustCompile(`^app_url_value_preprocessor$`),
		regexp.MustCompile(`^app_url_defaults$`),
		regexp.MustCompile(`^record$`),      // @bp.record (blueprint setup-state hook)
		regexp.MustCompile(`^record_once$`), // @bp.record_once
		// Flask CLI / click AppGroup decorator: `@bp.cli.command(...)` and
		// `@app.cli.command(...)`. Extractor leaf is "command".
		regexp.MustCompile(`^command$`),

		// click decorator + helper DSL (issue #423). click is the
		// dominant Python CLI framework and its decorators (`@click.command()`,
		// `@click.group()`, `@click.option('--foo')`, `@click.argument('name')`,
		// `@click.pass_context`) plus helper functions (`click.echo`,
		// `click.prompt`, `click.confirm`, `click.style`, ...) arrive at
		// the resolver as bare leaf identifiers after the Python extractor
		// strips the `click.` receiver. Without anchors here every
		// decorator/helper call site inflates bug-extractor (python/click
		// 32.60% pre-fix). Names follow the Rails (#107) and Flask (#420)
		// precedent: collision with user methods is accepted because the
		// per-language gate (Python only) keeps them safely scoped, and
		// Dynamic is the appropriate "we know it's framework dispatch we
		// can't statically resolve" bucket. click constants (`STRING`,
		// `INT`, `FLOAT`, `BOOL`, `UUID`) and class types (`Path`,
		// `Choice`, `IntRange`, `FloatRange`, `Tuple`, `File`,
		// `make_pass_decorator`) are deliberately EXCLUDED here — they
		// arrive as `ext:click.<name>` stubs and the external allowlist
		// (click is allowlisted) classifies them ExternalKnown without
		// help from the dynamic catalog.
		// NOTE: ^command$ already declared above by the Flask block (#420).
		regexp.MustCompile(`^group$`),
		regexp.MustCompile(`^option$`),
		regexp.MustCompile(`^argument$`),
		regexp.MustCompile(`^pass_context$`),
		regexp.MustCompile(`^pass_obj$`),
		regexp.MustCompile(`^pass_meta_key$`),
		regexp.MustCompile(`^echo$`),
		regexp.MustCompile(`^secho$`),
		regexp.MustCompile(`^prompt$`),
		regexp.MustCompile(`^confirm$`),
		regexp.MustCompile(`^progressbar$`),
		regexp.MustCompile(`^getchar$`),
		regexp.MustCompile(`^pause$`),
		regexp.MustCompile(`^clear$`),
		regexp.MustCompile(`^style$`),
		regexp.MustCompile(`^unstyle$`),
		regexp.MustCompile(`^format_filename$`),
		regexp.MustCompile(`^get_terminal_size$`),
		regexp.MustCompile(`^launch$`),
		regexp.MustCompile(`^edit$`),
		regexp.MustCompile(`^get_app_dir$`),

		// Flask extensions + Marshmallow + Flask-SQLAlchemy DSL (issue
		// #446). Residual after #420 was Flask-SQLAlchemy column / type
		// / relationship constructors on `db = SQLAlchemy()`, Flask-Login
		// proxies (`current_user`, `@login_required`), Flask-WTF form
		// methods (`form.validate_on_submit()`), Marshmallow schema field
		// constructors (`fields.Str`, `fields.Nested`) and (de)serialization
		// hooks (`@pre_load`, `Schema.dump`, `Schema.load`), and Flask's
		// common response helpers (`jsonify`, `abort`, `send_file`). The
		// Python extractor strips receivers like `db.`, `fields.`,
		// `Schema.`, `form.`, `app.` and only the bare leaf identifier
		// arrives at the resolver. Pre-fix this drove flask 41.32% and
		// flask-realworld 43.47%. Per-language gate (Python only) keeps
		// generic leaves (`add`, `delete`, `commit`, `session`, `query`,
		// `dump`, `load`, `fields`, `String`, `Integer`) from shadowing
		// user methods/types in other ecosystems. Within Python the
		// collision trade is accepted: same precedent as Rails
		// `render`/`session`/`params` (#107) and Flask `route`/`command`
		// (#420) — Dynamic is the appropriate bucket for framework
		// dispatch the resolver can't statically bind.
		// Flask-SQLAlchemy
		regexp.MustCompile(`^Column$`),
		regexp.MustCompile(`^ForeignKey$`),
		regexp.MustCompile(`^relationship$`),
		regexp.MustCompile(`^backref$`),
		regexp.MustCompile(`^Integer$`),
		regexp.MustCompile(`^String$`),
		regexp.MustCompile(`^Text$`),
		regexp.MustCompile(`^Boolean$`),
		regexp.MustCompile(`^DateTime$`),
		regexp.MustCompile(`^Date$`),
		regexp.MustCompile(`^Float$`),
		regexp.MustCompile(`^Numeric$`),
		regexp.MustCompile(`^init_app$`),
		regexp.MustCompile(`^query$`),
		regexp.MustCompile(`^query_property$`),
		regexp.MustCompile(`^create_all$`),
		regexp.MustCompile(`^drop_all$`),
		regexp.MustCompile(`^session$`),
		regexp.MustCompile(`^commit$`),
		regexp.MustCompile(`^rollback$`),
		regexp.MustCompile(`^flush$`),
		regexp.MustCompile(`^add$`),
		regexp.MustCompile(`^delete$`),
		regexp.MustCompile(`^merge$`),
		regexp.MustCompile(`^refresh$`),
		// Flask-Login
		regexp.MustCompile(`^current_user$`),
		regexp.MustCompile(`^login_required$`),
		regexp.MustCompile(`^login_user$`),
		regexp.MustCompile(`^logout_user$`),
		regexp.MustCompile(`^confirm_login$`),
		// Flask-WTF
		regexp.MustCompile(`^validate_on_submit$`),
		regexp.MustCompile(`^populate_obj$`),
		regexp.MustCompile(`^render_kw$`),
		// Marshmallow — `Boolean` and `DateTime` overlap with the
		// SQLAlchemy types above and are already covered.
		regexp.MustCompile(`^fields$`),
		regexp.MustCompile(`^Schema$`),
		regexp.MustCompile(`^Str$`),
		regexp.MustCompile(`^Int$`),
		regexp.MustCompile(`^List$`),
		regexp.MustCompile(`^Nested$`),
		regexp.MustCompile(`^Method$`),
		regexp.MustCompile(`^Function$`),
		regexp.MustCompile(`^pre_load$`),
		regexp.MustCompile(`^post_load$`),
		regexp.MustCompile(`^pre_dump$`),
		regexp.MustCompile(`^post_dump$`),
		regexp.MustCompile(`^validates$`),
		regexp.MustCompile(`^validates_schema$`),
		regexp.MustCompile(`^dump$`),
		regexp.MustCompile(`^load$`),
		regexp.MustCompile(`^dumps$`),
		regexp.MustCompile(`^loads$`),
		// Flask common response helpers
		regexp.MustCompile(`^jsonify$`),
		regexp.MustCompile(`^make_response$`),
		regexp.MustCompile(`^abort$`),
		regexp.MustCompile(`^send_file$`),
		regexp.MustCompile(`^send_from_directory$`),
		regexp.MustCompile(`^stream_with_context$`),
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
		// Wave-4 (TS framework) — relative-import paths. The JS/TS
		// extractor emits IMPORTS edges with the literal module string
		// as ToID (e.g. `./cart-context`, `../fragments/cart`, `.`,
		// `..`, `../..`). Every importing file produces its own
		// SCOPE.Component import entity for the same module string, so
		// bare-name lookup is ambiguous and the edge drives to
		// bug-resolver despite the placeholder being bookkeeping rather
		// than the imported symbol's source. Mirrors the Python relative-
		// import pattern above (#432) and the
		// `scope:component:import:local:` heuristic-scope-stub branch
		// (which the cross-language imports extractor emits for the same
		// shape). Pattern matches `.`, `..`, and any leading-`./`/`../`
		// path; anchored to avoid biting bare identifiers.
		regexp.MustCompile(`^\.{1,3}$`),
		regexp.MustCompile(`^\.{1,2}/`),
		// TS path-mapped local imports — tsconfig.json `baseUrl` /
		// `paths` lets imports look like `components/grid` or
		// `lib/shopify/types`. These are intra-repo (no leading dot, no
		// leading `@scope`, no `node:` prefix, no package dot-domain
		// like `next.js`) and the extractor emits one SCOPE.Component
		// per importing file, producing the same ambig-bare-no-hint
		// disposition as relative paths. Restrict to multi-segment
		// paths whose first segment is a common TS-monorepo source root
		// (`src`, `app`, `lib`, `components`, `pages`, `hooks`,
		// `utils`, `helpers`, `services`, `store`, `styles`, `types`,
		// `config`, `constants`, `features`, `modules`, `domain`,
		// `data`, `api`, `server`, `client`, `shared`, `core`,
		// `common`, `models`, `views`, `controllers`, `middleware`,
		// `tests`, `test`, `__tests__`, `__mocks__`, `routes`). The
		// per-language gate (js/ts only) keeps these names from
		// shadowing real go/python/etc. modules with the same prefix.
		regexp.MustCompile(`^(src|app|lib|components|pages|hooks|utils|helpers|services|store|styles|types|config|constants|features|modules|domain|data|api|server|client|shared|core|common|models|views|controllers|middleware|tests|test|__tests__|__mocks__|routes)/`),
		// JS reflective `Function.prototype.{bind,apply,call}` is real, but
		// the bare `.bind(` / `.apply(` / `.call(` patterns collide with too
		// many domain methods (DB driver `bind`, `discount.apply(order)`,
		// `controller.call(...)`). Keep them out of the JS catalog; the
		// extractors tag truly reflective uses (e.g. `Reflect.apply`) which
		// the explicit `Reflect\.` pattern above already covers.

		// Wave-7 (TS/JS React frontend, #519) — React useState setter
		// destructure pattern. The JS extractor strips the receiver from
		// destructured tuples (`const [v, setV] = useState(...)`), leaving
		// bare `setV` callee names that the resolver cannot bind because
		// the symbol is component-local and the extractor doesn't know its
		// origin. The `set[A-Z]...` convention is universal in the React
		// community (RFC + React docs) and the per-language gate
		// (js/ts only) prevents collision with `setHeader` / `setCookie`
		// style helpers in non-JS code. Names are bare leaf identifiers.
		regexp.MustCompile(`^set[A-Z][A-Za-z0-9_]*$`),
		// Promise chain methods — `then`, `catch`, `finally` — bare-name
		// callees on the result of `await`-able / Promise-returning
		// functions. The extractor emits the chained method as a bare
		// identifier with the receiver stripped, and the receiver is a
		// Promise value the resolver cannot model. `then` alone is the
		// dominant residual in client-fixture-b. JS-only gate keeps these
		// out of Ruby (`then` is a real method) / Go / Python collisions.
		regexp.MustCompile(`^then$`),
		regexp.MustCompile(`^catch$`),
		regexp.MustCompile(`^finally$`),
		// Wave-11 (TS/JS React frontend, ship-gate) — React handler-prop
		// convention `onClose`, `onClick`, `onChange`, `onSubmit`,
		// `onCancel`, `onConfirm`, `onSuccess`, `onError`, `onValueChange`,
		// `onSelect`, `onFocus`, `onBlur`, etc. These are callable props
		// passed from a parent component; the actual handler implementation
		// lives in the parent and is bound at component-invocation time, so
		// the call site (inside the child component) cannot statically bind
		// it. Standard React convention (React docs + RFC). The per-language
		// gate (js/ts only) keeps it from biting Python/Go/etc. where the
		// `onX` callable-prop convention does not exist. client-fixture-b
		// top bug-extractor sample after wave-10 was `onClearAll` /
		// `onClose` confirming this is the dominant residual shape.
		regexp.MustCompile(`^on[A-Z][A-Za-z0-9]*$`),
		// Wave-13 (TS/JS React frontend, real-residue) — `handle*` and
		// `after*` callable-prop / lifecycle-hook conventions. Mirrors
		// the wave-11 `^on[A-Z]...` rule. React/JSX components routinely
		// destructure callable props named `handleClientSelection`,
		// `handleReloadData`, `handleSaveOnCell`, `afterSaveNote`,
		// `afterCreateSuccess` from parent components or pass them as
		// `useCallback` returns. The actual implementation lives in the
		// parent (or in a higher-order hook) and is bound at component-
		// invocation time, so the call site cannot statically bind it.
		// `handleX` is universal React tutorial style (React docs +
		// every state-of-the-art repo); `afterX` is form/lifecycle hook
		// convention (antd Form `afterClose`, react-hook-form `afterSubmit`).
		// Both names dominate cfb wave-12-FINAL bug-extractor + bug-resolver
		// residues (`handleClientSelection`, `handleReloadData`,
		// `afterSaveNote`, `afterSaveSuccess`, `afterCreateSuccess`).
		// Same-file preference resolver (wave-9 Chain-fix A) fires BEFORE
		// the dynamic pattern check via the hex-ID branch in
		// classifyDispositionLang, so a same-file lifted handler entity
		// still wins. Per-language gate (js/ts only) keeps these from
		// shadowing `HandleXxx` Go method conventions / Python `handle_*`
		// snake_case (covered by other patterns). Conservative scope:
		// `get*` / `set*` / `load*` / `save*` / `create*` / `update*` /
		// `delete*` / `fetch*` / `use*` / `submit*` / `cancel*` / `select*`
		// / `reset*` / `toggle*` are deliberately EXCLUDED — those verbs
		// shadow real user-defined entities (services, repos, mutators)
		// far more aggressively than `handle*`/`after*`.
		regexp.MustCompile(`^handle[A-Z][A-Za-z0-9]*$`),
		regexp.MustCompile(`^after[A-Z][A-Za-z0-9]*$`),
		// PLT #537 RN/Expo wave — React ref + responsive-design + RN
		// notification idioms that the JS extractor strips to bare leaf
		// identifiers. `current` is the universal React useRef property
		// (`ref.current`); `isTablet` / `isMobile` / `isLandscape` /
		// `isPortrait` are responsive-design flags returned by
		// react-native-device-info / useResponsive hooks and consumed as
		// bare destructured locals; `enqueue` is the RN notification
		// queue method on the NotificationContext / Toast / Snackbar
		// receivers (also antd's notification.enqueue). These names are
		// indistinguishable from real user methods to the resolver
		// after the receiver is stripped — same shape as the wave-11
		// `^on[A-Z]` rule. JS-only gate keeps them out of Go / Python /
		// Java where `current` is a real first-class symbol name.
		regexp.MustCompile(`^current$`),
		regexp.MustCompile(`^isTablet$`),
		regexp.MustCompile(`^isMobile$`),
		regexp.MustCompile(`^isLandscape$`),
		regexp.MustCompile(`^isPortrait$`),
		regexp.MustCompile(`^isDesktop$`),
		regexp.MustCompile(`^enqueue$`),
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

		// Rails ActionPack / ActionDispatch / ActiveSupport internals
		// (issue #448). Rails framework DSL exposed to controllers,
		// routes and initializers — method_missing-generated or
		// class-macro driven. The Ruby extractor strips the receiver
		// and the resolver sees only the bare leaf identifier
		// (`Rails.application.routes.draw { resources :users }` →
		// `resources`/`draw`, `class_attribute :foo` → `class_attribute`,
		// `Rails.application.config.middleware.insert_before(...)` →
		// `insert_before`). Classify as Dynamic so they don't pollute
		// bug-extractor (rails-actionpack 20.02% pre-fix).
		//
		// Conservative selection (lessons from #94 / #107):
		// generic English verbs/accessors that exist as user-method
		// names on any class in any language (`to`, `as`, `via`,
		// `format`, `defaults`, `action`, `match`, `member`,
		// `collection`, `nested`, `included`, `extended`, `inherited`,
		// `concern` outside the routing-DSL position, `swap`) are
		// deliberately EXCLUDED — the per-language Ruby gate alone is
		// not strong enough to keep them safe. The names below are
		// Rails-idiomatic enough that the Ruby gate is sufficient
		// protection against shadowing user methods in other
		// ecosystems (Go HTTP `get`/`post`, JS `get`/`post`, etc.).
		//
		// Categories:
		//   - Routing DSL (ActionDispatch::Routing::Mapper).
		//   - ActionController DSL macros (helper / layout / CSRF).
		//   - ActiveSupport class macros and callbacks.
		//   - ActionDispatch middleware-stack DSL.
		// Already covered above by the issue #107 batch: `scope`,
		// `helper_method`, `params`, `session`, `flash`, `cookies`,
		// `request`, `response`, `respond_to`, `render`, `redirect_to`,
		// `before_action`/`after_action`/`around_action`/
		// `skip_before_action`.
		// Routing DSL.
		regexp.MustCompile(`^resources$`),
		regexp.MustCompile(`^resource$`),
		regexp.MustCompile(`^namespace$`),
		regexp.MustCompile(`^constraints$`),
		regexp.MustCompile(`^concern$`),
		regexp.MustCompile(`^concerns$`),
		regexp.MustCompile(`^mount$`),
		regexp.MustCompile(`^get$`),
		regexp.MustCompile(`^post$`),
		regexp.MustCompile(`^put$`),
		regexp.MustCompile(`^patch$`),
		regexp.MustCompile(`^delete$`),
		regexp.MustCompile(`^root$`),
		regexp.MustCompile(`^direct$`),
		regexp.MustCompile(`^resolve$`),
		regexp.MustCompile(`^controller$`),
		// ActionController DSL macros.
		regexp.MustCompile(`^helper$`),
		regexp.MustCompile(`^layout$`),
		regexp.MustCompile(`^protect_from_forgery$`),
		regexp.MustCompile(`^skip_authorization_check$`),
		regexp.MustCompile(`^verify_authenticity_token$`),
		regexp.MustCompile(`^respond_with$`),
		regexp.MustCompile(`^headers$`),
		// ActiveSupport class macros and callbacks.
		regexp.MustCompile(`^prepended$`),
		regexp.MustCompile(`^class_attribute$`),
		regexp.MustCompile(`^mattr_accessor$`),
		regexp.MustCompile(`^mattr_reader$`),
		regexp.MustCompile(`^mattr_writer$`),
		regexp.MustCompile(`^cattr_accessor$`),
		regexp.MustCompile(`^define_callbacks$`),
		regexp.MustCompile(`^set_callback$`),
		regexp.MustCompile(`^skip_callback$`),
		// ActionDispatch middleware-stack DSL.
		regexp.MustCompile(`^add_middleware$`),
		regexp.MustCompile(`^delete_middleware$`),
		regexp.MustCompile(`^insert_before$`),
		regexp.MustCompile(`^insert_after$`),
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

	// HCL / Terraform dynamic-pattern catalog (issue #44). Terraform
	// interpolations bind at apply time through provider / module / variable
	// indirection that no static resolver can fully follow. The HCL
	// extractor already emits same-file structural-refs for `var.*`,
	// `local.*`, `module.*`, `data.*.*`, `output.*`, and resource refs;
	// what lands here are the residual cross-file refs inside a
	// multi-file module (root module's `variables.tf` referenced from
	// `main.tf`, etc.), provider/meta-arg dispatches, and Terraform
	// built-in function leaves the extractor surfaces as bare callees.
	// Per-language gate (lang == "hcl" or "terraform") keeps these
	// patterns from poisoning resolution in other ecosystems where
	// `local`, `module`, `data`, `var`, `count`, `for_each`, `merge`,
	// `lookup`, etc. are common identifiers.
	hclDynamicPatterns = []*regexp.Regexp{
		// Terraform built-in reference prefixes. Same-file structural-refs
		// resolve via byLocation; cross-file leftovers are framework
		// dispatch the resolver cannot statically bind.
		regexp.MustCompile(`^var\.[A-Za-z_]`),      // var.<name>
		regexp.MustCompile(`^local\.[A-Za-z_]`),    // local.<name>
		regexp.MustCompile(`^module\.[A-Za-z_]`),   // module.<name>(.attr)
		regexp.MustCompile(`^data\.[A-Za-z_]`),     // data.<type>.<name>(.attr)
		regexp.MustCompile(`^output\.[A-Za-z_]`),   // output.<name>
		regexp.MustCompile(`^provider\.[A-Za-z_]`), // provider.<name>
		regexp.MustCompile(`^path\.(module|root|cwd)`),
		regexp.MustCompile(`^terraform\.(workspace|env)`),
		regexp.MustCompile(`^self\.`), // self.<attr> inside provisioner blocks
		regexp.MustCompile(`^each\.(key|value)`),
		regexp.MustCompile(`^count\.index`),
		// `dynamic "<label>" { ... }` blocks introduce an iteration symbol
		// equal to the label, so `dynamic "statement" { ... }` produces
		// `statement.value` / `statement.key` references. The label names
		// are arbitrary user identifiers; the suffix is what marks the
		// reference as Terraform iteration dispatch.
		regexp.MustCompile(`^[a-z_][a-z0-9_]*\.(value|key)$`),
		// `for x in <expr> : ...` and `[for x in <expr> : x.<attr>]`
		// introduce a single-letter (or short) iteration variable used as
		// `<iter>.<attr>`. Anchored to a single lowercase letter followed
		// by a dot so user identifiers like `aws_instance.x` are not
		// swept up by accident.
		regexp.MustCompile(`^[a-z]\.[a-z_]`),
		// Terraform meta-arguments arriving as bare leaves.
		regexp.MustCompile(`^count$`),
		regexp.MustCompile(`^for_each$`),
		regexp.MustCompile(`^depends_on$`),
		regexp.MustCompile(`^lifecycle$`),
		regexp.MustCompile(`^dynamic$`),
		regexp.MustCompile(`^provisioner$`),
		regexp.MustCompile(`^connection$`),
		// Terraform built-in function names (full catalog).
		// Numeric.
		regexp.MustCompile(`^abs$`),
		regexp.MustCompile(`^ceil$`),
		regexp.MustCompile(`^floor$`),
		regexp.MustCompile(`^log$`),
		regexp.MustCompile(`^max$`),
		regexp.MustCompile(`^min$`),
		regexp.MustCompile(`^parseint$`),
		regexp.MustCompile(`^pow$`),
		regexp.MustCompile(`^signum$`),
		// String.
		regexp.MustCompile(`^chomp$`),
		regexp.MustCompile(`^format$`),
		regexp.MustCompile(`^formatlist$`),
		regexp.MustCompile(`^indent$`),
		regexp.MustCompile(`^join$`),
		regexp.MustCompile(`^lower$`),
		regexp.MustCompile(`^regex$`),
		regexp.MustCompile(`^regexall$`),
		regexp.MustCompile(`^replace$`),
		regexp.MustCompile(`^split$`),
		regexp.MustCompile(`^strrev$`),
		regexp.MustCompile(`^substr$`),
		regexp.MustCompile(`^title$`),
		regexp.MustCompile(`^trim$`),
		regexp.MustCompile(`^trimprefix$`),
		regexp.MustCompile(`^trimsuffix$`),
		regexp.MustCompile(`^trimspace$`),
		regexp.MustCompile(`^upper$`),
		regexp.MustCompile(`^startswith$`),
		regexp.MustCompile(`^endswith$`),
		// Collection.
		regexp.MustCompile(`^alltrue$`),
		regexp.MustCompile(`^anytrue$`),
		regexp.MustCompile(`^chunklist$`),
		regexp.MustCompile(`^coalesce$`),
		regexp.MustCompile(`^coalescelist$`),
		regexp.MustCompile(`^compact$`),
		regexp.MustCompile(`^concat$`),
		regexp.MustCompile(`^contains$`),
		regexp.MustCompile(`^distinct$`),
		regexp.MustCompile(`^element$`),
		regexp.MustCompile(`^flatten$`),
		regexp.MustCompile(`^index$`),
		regexp.MustCompile(`^keys$`),
		regexp.MustCompile(`^length$`),
		regexp.MustCompile(`^list$`),
		regexp.MustCompile(`^lookup$`),
		regexp.MustCompile(`^map$`),
		regexp.MustCompile(`^matchkeys$`),
		regexp.MustCompile(`^merge$`),
		regexp.MustCompile(`^one$`),
		regexp.MustCompile(`^range$`),
		regexp.MustCompile(`^reverse$`),
		regexp.MustCompile(`^setintersection$`),
		regexp.MustCompile(`^setproduct$`),
		regexp.MustCompile(`^setsubtract$`),
		regexp.MustCompile(`^setunion$`),
		regexp.MustCompile(`^slice$`),
		regexp.MustCompile(`^sort$`),
		regexp.MustCompile(`^sum$`),
		regexp.MustCompile(`^transpose$`),
		regexp.MustCompile(`^try$`),
		regexp.MustCompile(`^values$`),
		regexp.MustCompile(`^zipmap$`),
		regexp.MustCompile(`^nonsensitive$`),
		regexp.MustCompile(`^sensitive$`),
		// Encoding.
		regexp.MustCompile(`^base64decode$`),
		regexp.MustCompile(`^base64encode$`),
		regexp.MustCompile(`^base64gzip$`),
		regexp.MustCompile(`^csvdecode$`),
		regexp.MustCompile(`^jsondecode$`),
		regexp.MustCompile(`^jsonencode$`),
		regexp.MustCompile(`^textdecodebase64$`),
		regexp.MustCompile(`^textencodebase64$`),
		regexp.MustCompile(`^urlencode$`),
		regexp.MustCompile(`^yamldecode$`),
		regexp.MustCompile(`^yamlencode$`),
		// Filesystem / template.
		regexp.MustCompile(`^abspath$`),
		regexp.MustCompile(`^basename$`),
		regexp.MustCompile(`^dirname$`),
		regexp.MustCompile(`^pathexpand$`),
		regexp.MustCompile(`^file$`),
		regexp.MustCompile(`^fileexists$`),
		regexp.MustCompile(`^fileset$`),
		regexp.MustCompile(`^filebase64$`),
		regexp.MustCompile(`^templatefile$`),
		regexp.MustCompile(`^templatestring$`),
		// Date/time.
		regexp.MustCompile(`^formatdate$`),
		regexp.MustCompile(`^plantimestamp$`),
		regexp.MustCompile(`^timeadd$`),
		regexp.MustCompile(`^timecmp$`),
		regexp.MustCompile(`^timestamp$`),
		// Hash/crypto.
		regexp.MustCompile(`^base64sha256$`),
		regexp.MustCompile(`^base64sha512$`),
		regexp.MustCompile(`^bcrypt$`),
		regexp.MustCompile(`^filebase64sha256$`),
		regexp.MustCompile(`^filebase64sha512$`),
		regexp.MustCompile(`^filemd5$`),
		regexp.MustCompile(`^filesha1$`),
		regexp.MustCompile(`^filesha256$`),
		regexp.MustCompile(`^filesha512$`),
		regexp.MustCompile(`^md5$`),
		regexp.MustCompile(`^rsadecrypt$`),
		regexp.MustCompile(`^sha1$`),
		regexp.MustCompile(`^sha256$`),
		regexp.MustCompile(`^sha512$`),
		regexp.MustCompile(`^uuid$`),
		regexp.MustCompile(`^uuidv5$`),
		// IP/network.
		regexp.MustCompile(`^cidrhost$`),
		regexp.MustCompile(`^cidrnetmask$`),
		regexp.MustCompile(`^cidrsubnet$`),
		regexp.MustCompile(`^cidrsubnets$`),
		// Type conversion.
		regexp.MustCompile(`^can$`),
		regexp.MustCompile(`^tobool$`),
		regexp.MustCompile(`^tolist$`),
		regexp.MustCompile(`^tomap$`),
		regexp.MustCompile(`^tonumber$`),
		regexp.MustCompile(`^toset$`),
		regexp.MustCompile(`^tostring$`),
		regexp.MustCompile(`^type$`),
		// Variable-shaped leaves that arrive bare from the extractor when
		// an interpolation is just `var.foo` (no further .attr) — already
		// covered by `^var\.` etc above; the bare `var`, `local`, `module`
		// leaves (rare, e.g. via dynamic blocks) round it out.
		regexp.MustCompile(`^var$`),
		regexp.MustCompile(`^local$`),
		regexp.MustCompile(`^module$`),
		regexp.MustCompile(`^data$`),
		regexp.MustCompile(`^output$`),
		// Provider-prefixed resource / data refs that miss the same-file
		// structural-ref bind (declared in a sibling file of the same
		// module). The major Terraform provider prefixes — any new
		// provider follows the same `<prefix>_<resource_type>.<name>`
		// shape, so anchoring on the prefix keeps the catalog stable
		// without enumerating every provider.
		regexp.MustCompile(`^aws_[a-z0-9_]+\.`),
		regexp.MustCompile(`^azurerm_[a-z0-9_]+\.`),
		regexp.MustCompile(`^azuread_[a-z0-9_]+\.`),
		regexp.MustCompile(`^google_[a-z0-9_]+\.`),
		regexp.MustCompile(`^kubernetes_[a-z0-9_]+\.`),
		regexp.MustCompile(`^helm_[a-z0-9_]+\.`),
		regexp.MustCompile(`^oci_[a-z0-9_]+\.`),
		regexp.MustCompile(`^null_[a-z0-9_]+\.`),
		regexp.MustCompile(`^random_[a-z0-9_]+\.`),
		regexp.MustCompile(`^tls_[a-z0-9_]+\.`),
		regexp.MustCompile(`^template_[a-z0-9_]+\.`),
		regexp.MustCompile(`^archive_[a-z0-9_]+\.`),
		regexp.MustCompile(`^external_[a-z0-9_]+\.`),
		regexp.MustCompile(`^http_[a-z0-9_]+\.`),
		regexp.MustCompile(`^vault_[a-z0-9_]+\.`),
		regexp.MustCompile(`^datadog_[a-z0-9_]+\.`),
		regexp.MustCompile(`^cloudflare_[a-z0-9_]+\.`),
		regexp.MustCompile(`^github_[a-z0-9_]+\.`),
		regexp.MustCompile(`^gitlab_[a-z0-9_]+\.`),
		regexp.MustCompile(`^digitalocean_[a-z0-9_]+\.`),
		regexp.MustCompile(`^linode_[a-z0-9_]+\.`),
		regexp.MustCompile(`^alicloud_[a-z0-9_]+\.`),
		regexp.MustCompile(`^tencentcloud_[a-z0-9_]+\.`),
		regexp.MustCompile(`^hcp_[a-z0-9_]+\.`),
		regexp.MustCompile(`^consul_[a-z0-9_]+\.`),
		regexp.MustCompile(`^nomad_[a-z0-9_]+\.`),
		regexp.MustCompile(`^docker_[a-z0-9_]+\.`),
		// Bare provider names — emitted as IMPORTS ToID by the HCL
		// extractor for `provider "<name>"` blocks. Same rationale as the
		// resource-prefix patterns above: the provider plugin lives outside
		// the static graph and the dynamic bucket is the right disposition.
		regexp.MustCompile(`^aws$`),
		regexp.MustCompile(`^azurerm$`),
		regexp.MustCompile(`^azuread$`),
		regexp.MustCompile(`^google$`),
		regexp.MustCompile(`^kubernetes$`),
		regexp.MustCompile(`^helm$`),
		regexp.MustCompile(`^oci$`),
		regexp.MustCompile(`^null$`),
		regexp.MustCompile(`^random$`),
		regexp.MustCompile(`^tls$`),
		regexp.MustCompile(`^template$`),
		regexp.MustCompile(`^archive$`),
		regexp.MustCompile(`^external$`),
		regexp.MustCompile(`^http$`),
		regexp.MustCompile(`^vault$`),
		regexp.MustCompile(`^datadog$`),
		regexp.MustCompile(`^cloudflare$`),
		regexp.MustCompile(`^github$`),
		regexp.MustCompile(`^gitlab$`),
		regexp.MustCompile(`^digitalocean$`),
		regexp.MustCompile(`^linode$`),
		regexp.MustCompile(`^alicloud$`),
		regexp.MustCompile(`^tencentcloud$`),
		regexp.MustCompile(`^hcp$`),
		regexp.MustCompile(`^consul$`),
		regexp.MustCompile(`^nomad$`),
		regexp.MustCompile(`^docker$`),
		// Relative-path module sources (`source = "../../"`,
		// `source = "./modules/foo"`). The HCL extractor emits these as
		// raw IMPORTS ToIDs for module blocks. Local module paths could
		// be resolved to a sibling-directory entity in principle but the
		// pattern of `..`/`./` prefixes is unambiguously a path; tagging
		// Dynamic is consistent with how Python's relative-import paths
		// (`^\.+`) land in pythonDynamicPatterns.
		regexp.MustCompile(`^\.\.?/`),
		regexp.MustCompile(`^\.\.?$`),
		// Terraform registry module sources
		// (`registry.terraform.io/hashicorp/aws/version`). External
		// package not in the graph. The 3-segment registry short form
		// (`hashicorp/aws/version`) is deliberately NOT pattern-matched
		// here — it collides with markdown IMPORTS shaped the same way
		// (`docs/modules/vpc-endpoints`); the long form (with the
		// `registry.terraform.io/` host) is unambiguous.
		regexp.MustCompile(`^registry\.terraform\.io/`),
		regexp.MustCompile(`^git::`),
		regexp.MustCompile(`^github\.com/`),
		regexp.MustCompile(`^bitbucket\.org/`),
	}

	// Cross-language patterns that are safe to evaluate when language is
	// unknown. Template-built names (`${x}` interpolation) are reflection-
	// shaped in every language that has them.
	crossLangDynamicPatterns = []*regexp.Regexp{
		regexp.MustCompile(`.*\$\{.*\}.*`), // template-built strings ${x}
	}

	// C# / .NET dynamic-pattern catalog (issue #441). Routes
	// project-internal `using Namespace.SubNamespace;` IMPORTS whose
	// target dotted namespace has no entity in the graph (because we
	// index file-level entities, not namespace entities) into Dynamic
	// instead of bug-extractor. Pattern: PascalCase root segment, at
	// least one dot, no leading `Microsoft.`/`System.` (those resolve
	// via the external allowlist) and no leading lowercase (which would
	// be a method-on-receiver shape, handled elsewhere).
	//
	// Also covers a small set of receiver-stripped reflection /
	// concurrency primitives the C# extractor emits as bare or dotted
	// callees (`Interlocked.Increment`, `MethodBase.GetCurrentMethod`,
	// `PeriodicTimer.WaitForNextTickAsync`, `ConcurrentDictionary.
	// TryRemove`). These are not language-builtins in the strict sense
	// but they're framework-dispatch entry points that no static binder
	// can reach without full assembly-level type resolution.
	csharpDynamicPatterns = []*regexp.Regexp{
		// PascalCase project-internal namespace import.
		// Anchored: starts with uppercase, contains at least one dot, every
		// segment is an identifier (no whitespace / brackets), and there is
		// no generic `<...>` suffix (those are call sites, not imports).
		// Negative-lookahead would be cleaner but Go regexp's RE2 dialect
		// doesn't support lookaround — we filter out the well-known .NET
		// / Microsoft / EF Core ecosystem roots at the caller (handled in
		// isDynamicPatternLang via a startsWith check before regex eval).
		regexp.MustCompile(`^[A-Z][A-Za-z0-9_]*(?:\.[A-Z][A-Za-z0-9_]*)+$`),
	}

	// csharpExternalNamespaceRoots lists the dotted namespace roots that
	// must NOT be classified as Dynamic by csharpDynamicPatterns — they
	// are real external imports (Microsoft.AspNetCore, System.Linq, EF
	// Core, etc.) that the external synthesiser routes to ext:microsoft
	// / ext:system. Without this exclusion the dynamic-pattern check
	// (which runs BEFORE the external-prefix check, Refs #95) would
	// promote every Microsoft.* import to Dynamic and the corpus would
	// lose its ExternalKnown classification.
	csharpExternalNamespaceRoots = []string{
		"Microsoft.",
		"System.",
		"EntityFrameworkCore",
		"Newtonsoft.",
		"Serilog",
		"NLog",
		"Autofac",
		"Castle.",
		"AutoMapper",
		"MediatR",
		"FluentValidation",
		"FluentAssertions",
		"NUnit",
		"Xunit",
		"Moq",
		"Polly",
		"Dapper",
		"RestSharp",
		"Hangfire",
		"Quartz",
		"IdentityServer",
		"MassTransit",
		"NServiceBus",
		"RabbitMQ.",
		"StackExchange.",
		"Swashbuckle.",
		"GraphQL.",
		"HotChocolate",
		"AspNetCore",
		"Org.BouncyCastle",
		"Mvc.",
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
		"hcl":        hclDynamicPatterns,
		"terraform":  hclDynamicPatterns,
		"csharp":     csharpDynamicPatterns,
		"razor":      csharpDynamicPatterns,
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
	// For structural-ref stubs (`scope:<kind>:<subtype>:<lang>:<file>:<tail>`)
	// also evaluate patterns against the trailing name segment. Per-language
	// catalogs anchor with `^` to match leaf identifiers (e.g. `^var\.`,
	// `^local\.`), which never match the full structural-ref stub but DO
	// match the tail. This mirrors how classifyDispositionLang already
	// pulls the tail out for name-existence checks.
	candidates := []string{stub}
	if strings.HasPrefix(stub, stubPrefixScope) {
		parts := strings.SplitN(stub, stubDelim, stubScopeSegments)
		if len(parts) == stubScopeSegments {
			tail := parts[stubScopeTailIndex]
			if hash := strings.IndexByte(tail, stubMemberDelim); hash >= 0 {
				tail = tail[hash+1:]
			}
			if tail != "" {
				candidates = append(candidates, tail)
			}
		}
	}
	normLang := normalizeLang(lang)
	if patterns, ok := dynamicPatternsByLang[normLang]; ok {
		// C# / Razor — exclude well-known external-namespace roots from
		// the dynamic-pattern dispatch (issue #441). The PascalCase
		// project-internal pattern matches `Microsoft.AspNetCore.X`
		// shape too; the external synthesiser routes those to
		// ext:microsoft / ext:system, so promoting them to Dynamic
		// would lose ExternalKnown classification.
		if normLang == "csharp" || normLang == "razor" {
			for _, root := range csharpExternalNamespaceRoots {
				if strings.HasPrefix(stub, root) {
					return false
				}
			}
		}
		for _, re := range patterns {
			for _, cand := range candidates {
				if re.MatchString(cand) {
					return true
				}
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

	// nameKindsReal[name][kind] = entity_id, indexed under the entity's
	// ORIGINAL kind only (no SCOPE.* dual-indexing). Used by
	// lookupByKindHint's tier-1 pass to prefer real entities over
	// SCOPE.* placeholders when EXTENDS / IMPLEMENTS / CALLS edges
	// resolve a bare name that lives under both tiers (#525). Blank
	// string sentinel marks (name, kind) collisions; identical
	// semantics to nameKinds but without the cross-tier ambiguity that
	// dual-indexing introduces.
	nameKindsReal map[string]map[string]string

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

	// byLocationKindReal mirrors byLocationKind but indexes ONLY under
	// the entity's original kind (no SCOPE.* dual-indexing). Used by
	// lookupLocationKind's tier-1 pass so structural-ref EXTENDS /
	// IMPLEMENTS edges that target a same-file collision between a
	// real Component and a SCOPE.Component placeholder bind to the
	// real entity (#525). Without this, the dual-indexing in
	// byLocationKind blanks the "Component" key when a SCOPE.Component
	// of the same (file, name) is registered, forcing the resolver
	// into ambig-bare-hint-fail.
	byLocationKindReal LocationKindIndex

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

	// byPackageOperation[pkg_dir][name] = entity_id. Used by the
	// Refs #44 Go bare-call structural-ref path: the extractor rewrites
	// identifier-form CALLS edges (e.g. `helper()` from `main`) to
	// `scope:operation:method:go:<file>:<name>` so the resolver binds the
	// callee via byLocation when the callee lives in the SAME file. The
	// dominant Go pattern, however, is cross-file same-package: `Greet` in
	// `b.go` calling `Hello` defined in `a.go`. byLocation[b.go][Hello]
	// misses but byPackageOperation[pkgDirOf(b.go)][Hello] hits. Indexed
	// only when an entity has no dot in its Name (top-level function /
	// non-method operation) AND a non-empty SourceFile. A blank-string
	// sentinel marks (pkg, name) collisions so the resolver leaves the
	// stub alone instead of silently binding to the wrong overload.
	byPackageOperation map[string]map[string]string

	// byPackageComponent[pkg_dir][name] = entity_id. Used by the Refs #44
	// Go DEPENDS_ON / bare-receiver-type path: the Go extractor emits
	// DEPENDS_ON edges from each method to its receiver type with
	// ToID set to the bare type name (e.g. "Server"), and CALLS / field-
	// type edges similarly carry bare struct names that the global byName
	// lookup can't disambiguate when the same struct name (`Server`,
	// `Client`, `Config`) appears in multiple packages of the same repo
	// (grpc-go-examples is the canonical offender — 144 residual edges
	// after #480 / #148, all DEPENDS_ON to bare struct names spread across
	// sibling files inside one package). Mirror of byPackageOperation but
	// for SCOPE.Component entities (struct / interface / view / model).
	// Indexed only when an entity has a non-empty SourceFile AND a non-
	// dotted Name AND a Component-family Kind. A blank-string sentinel
	// marks (pkg, name) collisions so the resolver leaves the stub alone
	// instead of binding to an arbitrary same-named component in the same
	// package (extremely rare in practice).
	byPackageComponent map[string]map[string]string
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
		byKind:             make(map[string]map[string]string),
		ambigKind:          make(map[string]map[string]bool),
		byName:             make(map[string]string),
		ambigName:          make(map[string]bool),
		nameKinds:          make(map[string]map[string]string),
		nameKindsReal:      make(map[string]map[string]string),
		byLocation:         make(LocationIndex),
		ambigLocation:      make(map[string]map[string]bool),
		byLocationKind:     make(LocationKindIndex),
		byLocationKindReal: make(LocationKindIndex),
		byMember:           make(map[string]map[string]map[string]string),
		byPackageMember:    make(map[string]map[string]map[string]string),
		byPackageOperation: make(map[string]map[string]string),
		byPackageComponent: make(map[string]map[string]string),
		byQualifiedName:    make(map[string]string),
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
		// Flask-realworld wave — index Properties["ref"] under the same
		// byQualifiedName bucket, scoped to refs that look like
		// `scope:endpoint:<file>#<method>:<path>`. The cross-language
		// endpoint extractor stamps the entity's structural identifier
		// into the `ref` property but doesn't populate QualifiedName, so
		// SERVES / HANDLES edges that carry the structural stub as
		// FromID fall through every other index path and land in
		// bug-extractor. By indexing the endpoint ref under the qname
		// bucket, the resolver picks these edges up without requiring
		// an extractor change. SCOPED to the `scope:endpoint:` prefix
		// so import / file structural refs (which legitimately repeat
		// across consumer files and would be blanked under the existing
		// QualifiedName collision policy) are not pulled into the
		// qname-resolution path. First-writer-wins, NO blanking on
		// duplicates — endpoint refs are unique per (file, method,
		// path) tuple by construction so collisions only occur when
		// the same endpoint entity is re-emitted, and choosing either
		// instance is safe.
		if refProp := e.Properties["ref"]; refProp != "" && refProp != e.QualifiedName &&
			strings.HasPrefix(refProp, "scope:endpoint:") {
			if _, ok := idx.byQualifiedName[refProp]; !ok {
				idx.byQualifiedName[refProp] = e.ID
			}
		}
		// Rust wave-2 (S20+) — same first-writer-wins policy for
		// hierarchy-extractor interface stubs. The cross/hierarchy
		// extractor emits one trait entity per `impl Trait for Foo`
		// site, so the same trait (e.g. `Handler<server::Message>`,
		// `actix::Message`, `fmt::Display`) gets re-emitted from
		// every implementor file. Under the default-blanking policy
		// above, every collision blanks the qname, so the
		// IMPLEMENTS edge's ToID never resolves and the trait
		// stub lands in bug-extractor.
		//
		// scope:component:interface: refs are unique per (lang,
		// trait-name) tuple BY DESIGN — they're a global naming
		// for the language's trait surface; choosing the first-seen
		// entity is safe (the entity itself is a synthesised anchor,
		// not a definition site, so all instances are equivalent).
		// Scoped strictly to the interface prefix; class refs still
		// carry file paths and don't need this branch.
		if refProp := e.Properties["ref"]; refProp != "" && refProp != e.QualifiedName &&
			strings.HasPrefix(refProp, "scope:component:interface:rust:") {
			if _, ok := idx.byQualifiedName[refProp]; !ok {
				idx.byQualifiedName[refProp] = e.ID
			}
		}

		// Issue #612 — Java / TypeScript / JS / C# / Kotlin / Scala / Dart /
		// PHP hierarchy-extractor interface stubs. The cross/hierarchy
		// extractor now emits an interface entity + EXTENDS edges for
		// `interface Foo extends Bar` declarations (Spring Data repositories,
		// generic interface chains, multi-interface extension). The edge's
		// FromID is `scope:component:interface:<lang>:<name>`, so it must be
		// indexable under that ref. First-writer-wins, matching the Rust
		// policy above — interface refs are globally unique per (lang, name)
		// by construction.
		if refProp := e.Properties["ref"]; refProp != "" && refProp != e.QualifiedName &&
			(strings.HasPrefix(refProp, "scope:component:interface:java:") ||
				strings.HasPrefix(refProp, "scope:component:interface:typescript:") ||
				strings.HasPrefix(refProp, "scope:component:interface:javascript:") ||
				strings.HasPrefix(refProp, "scope:component:interface:csharp:") ||
				strings.HasPrefix(refProp, "scope:component:interface:kotlin:") ||
				strings.HasPrefix(refProp, "scope:component:interface:scala:") ||
				strings.HasPrefix(refProp, "scope:component:interface:dart:") ||
				strings.HasPrefix(refProp, "scope:component:interface:php:")) {
			if _, ok := idx.byQualifiedName[refProp]; !ok {
				idx.byQualifiedName[refProp] = e.ID
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

		// nameKindsReal — single-pass under the entity's original kind
		// only. Used by lookupByKindHint's tier-1 real-entity pass
		// (#525) so that SCOPE.Component dual-indexing under
		// "Component" doesn't poison the hint when a same-named real
		// Component coexists. Blank sentinel marks collisions within
		// the same original kind.
		if e.Kind != "" {
			realBucket := idx.nameKindsReal[e.Name]
			if realBucket == nil {
				realBucket = make(map[string]string)
				idx.nameKindsReal[e.Name] = realBucket
			}
			if existing, ok := realBucket[e.Kind]; ok && existing != e.ID {
				realBucket[e.Kind] = ""
			} else {
				realBucket[e.Kind] = e.ID
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

			// byLocationKindReal — single-pass under the entity's
			// original kind only. Powers the real-tier preference in
			// lookupLocationKind so structural-ref EXTENDS targets
			// like scope:component:class:py:models.py:TimestampedModel
			// resolve to a real Component even when a SCOPE.Component
			// placeholder shares the same (file, name) (#525).
			if e.Kind != "" {
				realFileBucket := idx.byLocationKindReal[sourceFile]
				if realFileBucket == nil {
					realFileBucket = make(map[string]map[string]string)
					idx.byLocationKindReal[sourceFile] = realFileBucket
				}
				realNameBucket := realFileBucket[e.Name]
				if realNameBucket == nil {
					realNameBucket = make(map[string]string)
					realFileBucket[e.Name] = realNameBucket
				}
				if existing, ok := realNameBucket[e.Kind]; ok && existing != e.ID {
					realNameBucket[e.Kind] = ""
				} else {
					realNameBucket[e.Kind] = e.ID
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

		// Package-scoped top-level-operation index (Refs #44). Mirrors
		// byPackageMember but for operations whose Name has no dot — i.e.
		// non-method functions. The Go extractor rewrites identifier-form
		// CALLS edges to `scope:operation:method:go:<file>:<name>` so a
		// same-file callee binds via byLocation. Cross-file same-package
		// callees (the dominant Go pattern) fall back to this index in
		// lookupStructural. Indexed only when SourceFile is non-empty
		// and Name carries no dot (top-level Operation).
		if sourceFile != "" && strings.IndexByte(e.Name, dottedNameSep) < 0 &&
			isOperationKind(e.Kind) {
			pkgDir := pkgDirOf(sourceFile)
			if pkgDir != "" {
				pkgBucket := idx.byPackageOperation[pkgDir]
				if pkgBucket == nil {
					pkgBucket = make(map[string]string)
					idx.byPackageOperation[pkgDir] = pkgBucket
				}
				if existing, ok := pkgBucket[e.Name]; ok && existing != e.ID {
					pkgBucket[e.Name] = "" // blank sentinel → ambiguous
				} else if _, taken := pkgBucket[e.Name]; !taken {
					pkgBucket[e.Name] = e.ID
				}
			}
		}

		// Package-scoped component index (Refs #44, sibling of #148/#480
		// for component-shaped entities). The Go extractor emits a
		// DEPENDS_ON edge from each method to its receiver type with
		// ToID set to the bare type name; cross-file same-package binds
		// fail under byName when the same struct name (`Server`,
		// `Client`, `Config`, …) appears in multiple packages. The
		// resolver's ToID fast-path in ReferencesEmbeddedWithAllowlist
		// probes this index using the caller's package directory before
		// falling through to the global bare-name lookup, mirroring how
		// byPackageOperation handles bare CALLS targets. Indexed only
		// when SourceFile is non-empty and Name carries no dot
		// (top-level component declaration). A blank-string sentinel
		// marks (pkg, name) collisions so the resolver leaves the stub
		// alone instead of binding to an arbitrary same-named
		// component.
		if sourceFile != "" && strings.IndexByte(e.Name, dottedNameSep) < 0 &&
			isComponentKind(e.Kind) {
			pkgDir := pkgDirOf(sourceFile)
			if pkgDir != "" {
				pkgBucket := idx.byPackageComponent[pkgDir]
				if pkgBucket == nil {
					pkgBucket = make(map[string]string)
					idx.byPackageComponent[pkgDir] = pkgBucket
				}
				if existing, ok := pkgBucket[e.Name]; ok && existing != e.ID {
					pkgBucket[e.Name] = "" // blank sentinel → ambiguous
				} else if _, taken := pkgBucket[e.Name]; !taken {
					pkgBucket[e.Name] = e.ID
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

// isOperationKind reports whether the kind string is one of the Operation
// family kinds (SCOPE.Operation, etc.) that should be indexed in
// byPackageOperation.
func isOperationKind(k string) bool {
	return k == "SCOPE.Operation" || k == "Operation"
}

// isComponentKind reports whether the kind string is one of the Component
// family kinds (SCOPE.Component, etc.) that should be indexed in
// byPackageComponent. Mirrors componentKindFamily but kept as a fast
// switch for the BuildIndex hot path.
func isComponentKind(k string) bool {
	switch k {
	case "SCOPE.Component", "Component", "Class", "View", "Model",
		"SCOPE.View", "SCOPE.Model":
		return true
	}
	return false
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

// componentKindFamily / operationKindFamily / schemaKindFamily are the
// entity-kind families the hint resolver biases toward for type-shaped vs
// call-shaped vs field-shaped edges. Centralising the slices keeps
// hintKinds and structuralKindFamilies in agreement (issue #49).
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
	// schemaKindFamily covers field / schema / property entities (issue #778).
	// Used by structuralKindFamilies to disambiguate scope:schema:field:*
	// structural refs when byLocation finds both a SCOPE.Schema and a
	// SCOPE.Operation entity with the same (file, name) — a common pattern
	// when a Java class declares a field and a getter/setter with the same
	// leaf name (e.g. Cell.borderTop field + Cell.borderTop setter).
	schemaKindFamily = []string{
		"Schema", "Field", "Property",
		scopeKindPrefix + "Schema",
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
// (id, true) only when the hinted family yields exactly one entity for
// this name; otherwise ("", false).
//
// Tiered preference (issue #525): family members are partitioned into
// "real" entity kinds (Component, Class, View, Model, Operation,
// Function, Method) and SCOPE.* heuristic placeholders that the
// extractor emits when a structural target could not be pinned down.
// When the same bare name appears under both tiers — the classic
// `class Article(TimestampedModel):` shape where TimestampedModel is
// both an imported `Component` AND a same-file `SCOPE.Component`
// placeholder — the real entity is preferred over the placeholder. A
// real-tier hit short-circuits before the placeholder tier is even
// consulted, so EXTENDS / IMPLEMENTS / CALLS edges that would
// otherwise tag `ambig-bare-hint-fail` bind to the actual component.
func (idx Index) lookupByKindHint(name, relKind string) (string, bool) {
	families := hintKinds(relKind)
	if len(families) == 0 {
		return "", false
	}
	// Tier 1: real entity kinds only, consulted via nameKindsReal so
	// SCOPE.* dual-indexing in nameKinds doesn't blank a real entity's
	// kind bucket (#525). When a real Component / Class / View / Model
	// (or Operation / Function / Method) uniquely matches, return it
	// without consulting the SCOPE.* placeholder tier at all.
	if realBucket := idx.nameKindsReal[name]; len(realBucket) > 0 {
		if id, ok := uniqueMatchInFamily(realBucket, families, false); ok {
			return id, true
		}
	}
	bucket := idx.nameKinds[name]
	if len(bucket) == 0 {
		return "", false
	}
	// Tier 2: full family including SCOPE.* placeholders.
	return uniqueMatchInFamily(bucket, families, true)
}

// uniqueMatchInFamily walks the supplied family slice and returns the
// single entity ID present in bucket whose kind is a family member.
// When includePlaceholders is false, kinds prefixed with scopeKindPrefix
// are skipped — used by the tier-1 pass of lookupByKindHint to prefer
// real Component over SCOPE.Component (#525).
func uniqueMatchInFamily(bucket map[string]string, families []string, includePlaceholders bool) (string, bool) {
	var match string
	for _, k := range families {
		if !includePlaceholders && strings.HasPrefix(k, scopeKindPrefix) {
			continue
		}
		id := bucket[k]
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
	// Issue #432 — testmap "?" form: scope:operation:?#<qname>. The
	// cross-language test→production extractor emits this 3-segment shape
	// when the production file cannot be inferred (high/medium confidence
	// calls inside a test body). The standard 6-segment structural-ref
	// path can't match it. Try a QualifiedName lookup first; failing that
	// hand off to isHeuristicScopeStub (Dynamic) via the parts-length
	// guard below. The minority of these stubs whose qname is a unique
	// graph entity (test bodies that reference symbols by full path —
	// e.g. `requests.get` when only one entity has that qname) earn a
	// resolution credit instead of being silently dropped to Dynamic.
	if strings.HasPrefix(stub, "scope:operation:?#") {
		qname := stub[len("scope:operation:?#"):]
		if qname != "" {
			if qid, ok := idx.byQualifiedName[qname]; ok {
				if qid == "" {
					// QualifiedName collision — fall through to Dynamic.
					return "", statusUnmatched, true
				}
				return qid, statusRewritten, true
			}
		}
		return "", statusUnmatched, true
	}
	// PLT #537 — react_props short-form: scope:schema:<file>#<name>.
	// internal/extractors/cross/react_props/extractor.go's propsSchemaRef
	// emits this 3-segment shape on USES_PROPS edge ToIDs targeting the
	// component's Props interface / type-alias. The base JS/TS extractor
	// indexes interfaces / type aliases under byLocation[file][name]; this
	// short-form bypasses the 6-segment Format A path and binds directly.
	// Without it every USES_PROPS edge on tsx components lands in
	// bug-extractor (cfc 2.09% pre-fix — `AdditionalInfoFieldsProps` etc.).
	if strings.HasPrefix(stub, "scope:schema:") && strings.IndexByte(stub, stubMemberDelim) > 0 {
		rest := stub[len("scope:schema:"):]
		if hash := strings.IndexByte(rest, stubMemberDelim); hash > 0 {
			filePath := normalizePath(rest[:hash])
			member := rest[hash+1:]
			if filePath != "" && member != "" && !strings.Contains(filePath, ":") {
				if bucket, ok := idx.byLocation[filePath]; ok {
					if id, ok := bucket[member]; ok && id != "" {
						return id, statusRewritten, true
					}
				}
			}
		}
	}
	// Issue #432 — testmap short-form: scope:operation:<file>#<name>.
	// testFunctionRef + productionFunctionRef in
	// internal/extractors/cross/testmap/extractor.go emit this 3-segment
	// shape when the production file IS known (the extractor doesn't fill
	// the language / subtype slots — they only matter for the 6-segment
	// Format B). Probe the file+member index directly and fall back to a
	// file-scoped name lookup; this resolves the FromID side of every
	// TESTS edge (test functions live at known paths) and recovers the
	// minority of high-confidence ToIDs whose prod file IS inferred.
	if strings.HasPrefix(stub, "scope:operation:") && strings.IndexByte(stub, stubMemberDelim) > 0 {
		rest := stub[len("scope:operation:"):]
		if hash := strings.IndexByte(rest, stubMemberDelim); hash > 0 {
			filePath := normalizePath(rest[:hash])
			member := rest[hash+1:]
			if filePath != "" && filePath != "?" && member != "" &&
				!strings.Contains(filePath, ":") {
				// First try (file, name) — test functions defined at the
				// top level of a file (`def test_foo():`) appear in the
				// byLocation index keyed by their bare name.
				if id, ok := idx.lookupLocationKind(filePath, member, operationKindFamily); ok {
					return id, statusRewritten, true
				}
				if bucket, ok := idx.byLocation[filePath]; ok {
					if id, ok := bucket[member]; ok && id != "" {
						return id, statusRewritten, true
					}
				}
				// Walk byMember[file] looking for any scope that contains
				// this member name. testmap emits the bare method name
				// (`test_list`) while the Python / JVM / Ruby extractors
				// store class methods as `<class>.<member>` so the
				// member-bucket key matches without us needing to know
				// the enclosing class.
				if fileBucket, ok := idx.byMember[filePath]; ok {
					var match string
					ambig := false
					for _, scopeBucket := range fileBucket {
						id, ok := scopeBucket[member]
						if !ok || id == "" {
							continue
						}
						if match != "" && match != id {
							ambig = true
							break
						}
						match = id
					}
					if ambig {
						return "", statusAmbiguous, true
					}
					if match != "" {
						return match, statusRewritten, true
					}
				}
			}
		}
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
	// Refs #44 — Go cross-file same-package fallback. The Go extractor
	// rewrites bare CALLS targets to scope:operation:method:go:<file>:<name>
	// using the CALLER's file path; same-file callees bind via byLocation
	// above. Cross-file same-package callees (`Greet` in b.go calling
	// `Hello` defined in a.go) hit here: probe byPackageOperation under
	// pkgDirOf(filePath). A blank-sentinel hit means ambiguous within the
	// package — leave the stub alone rather than picking an arbitrary
	// overload, matching the byPackageMember (issue #148) policy. Only
	// fires for the "operation" scope-kind so other Format A scopes
	// (component, schema) aren't affected.
	if strings.EqualFold(scopeKind, "operation") {
		if pkgDir := pkgDirOf(filePath); pkgDir != "" {
			if pkgBucket, ok := idx.byPackageOperation[pkgDir]; ok {
				if id, ok := pkgBucket[tail]; ok {
					if id == "" {
						return "", statusAmbiguous, true
					}
					return id, statusRewritten, true
				}
			}
		}
	}
	// Flask-realworld wave — Python cross-file mixin/base class
	// fallback. The hierarchy extractor synthesises EXTENDS targets as
	// `scope:component:class:python:<consumer_file>:<ParentName>` using
	// the CONSUMER's file path (where the `class Foo(ParentName):`
	// declaration lives), even when `ParentName` is imported from a
	// sibling module. Same-file lookup above fails; pkgDir fallback
	// only fires for "operation" scope. For `component` scope with
	// lang=="python" probe the global byName index: if exactly one
	// real (non-SCOPE.*) Component-family entity exists for this name
	// across the whole graph, bind to it. Resolves the
	// `class Article(SurrogatePK):` shape where SurrogatePK is defined
	// in `conduit/database.py` but used in `conduit/articles/models.py`.
	// Conservative — only fires when the global lookup is unambiguous
	// (single real entity), so cross-file collisions are left
	// unresolved rather than guessed.
	if strings.EqualFold(scopeKind, "component") {
		// Restrict to python (other languages have their own
		// package/file-keyed lookup paths and shouldn't be widened).
		if lang := strings.ToLower(parts[stubScopeLangIndex]); lang == "python" {
			if id, ok := idx.lookupUniqueRealComponentByName(tail); ok {
				return id, statusRewritten, true
			}
		}
	}
	return "", statusUnmatched, true
}

// lookupUniqueRealComponentByName returns (id, true) when exactly one
// Component-family entity is registered globally under the supplied
// bare name. Tries the real-entity tier first (Component / Class /
// View / Model; SCOPE.* placeholders excluded). When that misses
// (Python emits user classes only as SCOPE.Component since #525), it
// falls back to scanning every (file, kind) bucket for SCOPE.Component
// entries with this name: if a single file owns the only non-placeholder
// SCOPE.Component (i.e. one consumer file has the class definition
// while the others may have placeholder imports under different fully-
// qualified names), bind to it. Used by lookupStructural's Python
// cross-file fallback for `scope:component:class:python:<file>:<Name>`
// stubs emitted by the hierarchy extractor with the consumer's file
// path when the parent class lives in a sibling module. Returns
// ("", false) when zero or >=2 candidates match — the resolver leaves
// the stub alone rather than guessing.
func (idx Index) lookupUniqueRealComponentByName(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	if realBucket := idx.nameKindsReal[name]; len(realBucket) > 0 {
		if id, ok := uniqueMatchInFamily(realBucket, componentKindFamily, false); ok {
			return id, true
		}
	}
	// Python class fallback: scan byLocationKindReal for any file that
	// owns a SCOPE.Component entity with this exact name. SCOPE.Component
	// is the Python extractor's class-entity kind (#525). When exactly
	// one file owns a SCOPE.Component for this name, bind to it; ambiguous
	// otherwise.
	scopeKind := scopeKindPrefix + "Component"
	var match string
	for _, fileBucket := range idx.byLocationKind {
		nameBucket := fileBucket[name]
		if nameBucket == nil {
			continue
		}
		id := nameBucket[scopeKind]
		if id == "" {
			continue
		}
		if match != "" && match != id {
			return "", false
		}
		match = id
	}
	if match != "" {
		return match, true
	}
	return "", false
}

// structuralKindFamilies maps a scope-kind segment from a structural ref
// (e.g. "component", "operation", "schema") to the entity-kind families it
// might be indexed under. Returns nil for unknown segments.
//
// Issue #778 — add "schema" so scope:schema:field:java:* stubs resolve via
// lookupLocationKind using schemaKindFamily instead of falling through to
// the kind-agnostic byLocation fallback, which hits ambiguity when a Java
// class declares both a field and a getter/setter sharing the same
// qualified name (e.g. Cell.borderTop as SCOPE.Schema + SCOPE.Operation).
func structuralKindFamilies(scopeKind string) []string {
	switch strings.ToLower(scopeKind) {
	case "component":
		return componentKindFamily
	case "operation":
		return operationKindFamily
	case "schema":
		return schemaKindFamily
	}
	return nil
}

// lookupLocationKind picks an entity by (file, name) constrained to the
// supplied kind families. Returns (id, true) only when exactly one family
// resolves to a non-blank entity ID for this (file, name).
//
// Tiered preference (#525): consults byLocationKindReal first, scanning
// only the non-SCOPE.* members of the family. When that yields a unique
// real entity, return it without consulting the dual-indexed bucket —
// this is what makes `class Article(TimestampedModel):` bind to the
// imported real Component even when a SCOPE.Component placeholder for
// the same name lives in the same file. The fallback tier preserves
// the historic behaviour for SCOPE.*-only and mixed-kind shapes.
func (idx Index) lookupLocationKind(filePath, name string, families []string) (string, bool) {
	if len(families) == 0 {
		return "", false
	}
	if realFileBucket := idx.byLocationKindReal[filePath]; realFileBucket != nil {
		if realNameBucket := realFileBucket[name]; len(realNameBucket) > 0 {
			if id, ok := uniqueMatchInFamily(realNameBucket, families, false); ok {
				return id, true
			}
		}
	}
	fileBucket := idx.byLocationKind[filePath]
	if fileBucket == nil {
		return "", false
	}
	nameBucket := fileBucket[name]
	if len(nameBucket) == 0 {
		return "", false
	}
	return uniqueMatchInFamily(nameBucket, families, true)
}

// looksLikeSourceFilePath reports whether s has the shape of a source
// code file path — a path (possibly basename-only) ending in one of the
// well-known per-language extensions. Used by classifyDispositionLang
// to route IMPORTS-edge FromIDs (which every language extractor sets
// to the importing file's path) into DispositionDynamic rather than
// DispositionBugExtractor.
//
// Conservative checks: must NOT contain ':' (would be a structural-ref),
// must NOT start with '/' (absolute system paths are not extractor-emitted,
// and disqualifying them keeps us from accepting accidental Unix paths
// that escaped a higher layer), and must end with one of the catalogued
// source extensions. Basename-only paths are accepted so root-level
// files (e.g. Package.swift, root main.go, root index.ts) do not get
// misclassified as bug-extractor noise — issue #491.
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
	// Compare against the small allowlist of source-file extensions
	// the IMPORTS-emitting extractors actually use. Adding new
	// languages here is a one-line change in lockstep with the
	// extractor that introduces the new extension. Basename-only
	// inputs (no '/') are accepted — root-level files are real source
	// files and must not be classified as bug-extractor output.
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
	// HCL / Terraform — issue #44. The HCL extractor emits file-level
	// CONTAINS / IMPORTS edges with FromID set to the .tf file path.
	".tf", ".tfvars", ".hcl",
	// HTML — issue #506. The HTML extractor emits IMPORTS edges with
	// FromID set to the .html file path (e.g. index.html → /src/main.jsx).
	// Without this entry the .html path itself lands in bug-extractor even
	// when the target resolved successfully.
	".html", ".htm",
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
	// testmap unknown-prod-file marker (issue #432). The cross-language
	// test→production extractor uses the literal "?" placeholder when it
	// cannot infer the production file for a call inside a test body
	// (resolver.go:213 — only convention-fallback ("low") confidence calls
	// receive a prod-file guess). Without this branch every TESTS edge
	// targeting a high/medium-confidence call lands in bug-extractor —
	// the dominant residual on python/requests (96.13% of bug-extractor).
	// See lookupStructural for the qname-rewrite branch that resolves the
	// minority of these stubs whose qname matches a unique entity.
	case strings.HasPrefix(s, "scope:operation:?#"):
		return true
	// Issue #44 / GraphQL-fix — `scope:operation:<file>#it_*` testmap
	// stubs that the resolver could NOT bind to a concrete entity.
	// In docs-heavy / monorepo JS-TS projects (apollo-server) the
	// testmap extractor emits many Pattern entities whose
	// qualified_name is unset, so rewriteOne fails to find them.
	// We reach this branch only AFTER rewriteOne returned without
	// rewriting, so accepting all file-keyed `scope:operation:` stubs
	// here only affects unresolved ones — concrete resolutions still
	// short-circuit at the hex-ID check above.
	case strings.HasPrefix(s, "scope:operation:"):
		return true
	}
	return false
}

// dataAccessSQLOrms is the set of SQL driver / ORM tags the
// internal/extractors/cross/dbmap extractor emits in the
// scope:dataaccess:<file>#<orm>:<op>:<table> stub form. Used by
// isDataAccessSQLStub to route unresolved references to ExternalKnown
// (issue #507) instead of letting them inflate the bug-extractor bucket.
//
// Keep in sync with internal/extractors/cross/dbmap/orms.go. A short list
// of names is enough — the stub-format prefix check makes this an
// unambiguous classification gate.
var dataAccessSQLOrms = map[string]struct{}{
	"psycopg2":        {},
	"sqlalchemy":      {},
	"asyncpg":         {},
	"aiopg":           {},
	"mysql-connector": {},
	"pymysql":         {},
	"pymongo":         {},
	"mongoengine":     {},
	"gorm":            {},
	"sqlx":            {},
	"database/sql":    {},
	"sequelize":       {},
	"typeorm":         {},
	"prisma":          {},
	"knex":            {},
	"activerecord":    {},
	"hibernate":       {},
	"jdbc":            {},
	"jdbi":            {},
	"mybatis":         {},
}

// isDataAccessSQLStub reports whether s is a SCOPE.DataAccess structural
// ref emitted by the cross-language dbmap extractor in the form
//
//	scope:dataaccess:<file>#<orm>:<op>:<table>
//
// (where <orm> is one of dataAccessSQLOrms). These refs are intentional
// — they identify SQL surface area — and should resolve to a real
// SCOPE.DataAccess entity when one exists (extractor sets QualifiedName).
// When resolution fails the classifier routes them to ExternalKnown via
// classifyDispositionLang (issue #507).
func isDataAccessSQLStub(s string) bool {
	const prefix = "scope:dataaccess:"
	if !strings.HasPrefix(s, prefix) {
		return false
	}
	hash := strings.IndexByte(s, stubMemberDelim)
	if hash < 0 || hash >= len(s)-1 {
		return false
	}
	rest := s[hash+1:]
	colon := strings.IndexByte(rest, ':')
	if colon <= 0 {
		return false
	}
	orm := rest[:colon]
	_, ok := dataAccessSQLOrms[orm]
	return ok
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

// lookupMemberByLeafName scans byMember[filePath] for any scope whose
// member bucket contains memberName. Returns (id, true) only when exactly
// one scope inside the file has a member by that name and the match is
// unambiguous (non-blank sentinel). Returns ("", false) when the name is
// missing from all scopes, or ("", false) when two or more scopes share
// a member of the same leaf name (caller must not guess).
//
// Issue #778 — Java CALLS resolution. The Java extractor emits method
// entities as "ClassName.method" (qualified name). A bare-name CALLS stub
// "method" never reaches byPackageOperation (which only indexes non-dotted
// names). Scanning byMember for the leaf name is the correct fallback:
// a caller inside InventoryService calling `merge()` should bind to
// byMember[callerFile]["InventoryService"]["merge"].
func (idx Index) lookupMemberByLeafName(filePath, memberName string) (string, bool) {
	if filePath == "" || memberName == "" {
		return "", false
	}
	fileBucket := idx.byMember[filePath]
	if fileBucket == nil {
		return "", false
	}
	var match string
	ambig := false
	for _, scopeBucket := range fileBucket {
		id, ok := scopeBucket[memberName]
		if !ok {
			continue
		}
		if id == "" {
			// Blank sentinel within this scope = ambiguous member for this scope.
			// Two different overloads in the same class — can't resolve.
			ambig = true
			break
		}
		if match != "" && match != id {
			// Two different scopes each have a member named memberName —
			// ambiguous across scopes; do not pick one.
			ambig = true
			break
		}
		match = id
	}
	if ambig || match == "" {
		return "", false
	}
	return match, true
}

// lookupPackageMemberByLeafName scans byPackageMember[pkgDir] for any
// scope whose member bucket contains memberName. Returns (id, true) only
// when exactly one scope in the package has an unambiguous member by that
// name. Same semantics as lookupMemberByLeafName but package-scoped (for
// cross-file same-package CALLS in Java/Kotlin).
//
// Issue #778 — package-level fallback after the same-file scan misses
// (e.g. when the callee is defined in a sibling file of the same package).
func (idx Index) lookupPackageMemberByLeafName(pkgDir, memberName string) (string, bool) {
	if pkgDir == "" || memberName == "" {
		return "", false
	}
	pkgBucket := idx.byPackageMember[pkgDir]
	if pkgBucket == nil {
		return "", false
	}
	var match string
	ambig := false
	for _, scopeBucket := range pkgBucket {
		id, ok := scopeBucket[memberName]
		if !ok {
			continue
		}
		if id == "" {
			ambig = true
			break
		}
		if match != "" && match != id {
			ambig = true
			break
		}
		match = id
	}
	if ambig || match == "" {
		return "", false
	}
	return match, true
}

// isComponentTargetKind reports whether the relationship-kind's natural
// ToID shape is a Component. Used to gate the byPackageComponent fast-
// path so call-shaped edges (CALLS) don't accidentally bind to a same-
// named component when they actually want an operation (a struct named
// `Process` shouldn't catch a call to `Process(x)`).
func isComponentTargetKind(relKind string) bool {
	switch strings.ToUpper(relKind) {
	case "DEPENDS_ON", "EXTENDS", "IMPLEMENTS":
		return true
	}
	return false
}

// lookupPackageComponent probes the byPackageComponent index. When
// pkgDir + name resolves to a single entity ID, returns (id, true).
// Returns ("", false) for missing entries; returns ("", true) for the
// blank-sentinel ambiguous case so the caller can leave the stub alone
// instead of falling back to global bare-name lookup (which would risk
// binding to a foreign-package component of the same name). Mirrors
// lookupPackageMember (issue #148) for the component-family.
func (idx Index) lookupPackageComponent(pkgDir, name string) (string, bool) {
	if pkgDir == "" || name == "" {
		return "", false
	}
	pkgBucket := idx.byPackageComponent[pkgDir]
	if pkgBucket == nil {
		return "", false
	}
	id, ok := pkgBucket[name]
	if !ok {
		return "", false
	}
	if id == "" {
		return "", true
	}
	return id, true
}

// rewriteOne resolves a single endpoint reference. It returns the (possibly
// rewritten) ID string and the status code from LookupStatusHint. Hex IDs
// and empty strings short-circuit with a zero status, signalling "skip".
func (idx Index) rewriteOne(ref, relKind string) (string, int) {
	return idx.rewriteOneWithCaller(ref, relKind, "", "")
}

// rewriteOneWithCaller is rewriteOne with the caller's (sourceFile, pkgDir)
// context. When the global lookup is ambiguous on a bare name, the caller's
// file/package is consulted as a tie-breaker — the dominant intent across
// languages is "the local same-file definition wins", with a same-package
// fallback for languages whose compilation unit is the directory. This is
// the wave-9 cross-file same-named-const disambiguation (chain-fix A): JS/TS
// codebases routinely declare `handleDelete`, `isValid`, `useStyle` in
// dozens of files; without locality the resolver tags `ambig-bare-hint-fail`
// and the edge ends up as bug-resolver.
//
// Empty callerFile / callerPkgDir disables the locality preference and
// behaves identically to rewriteOne — preserving the existing test contract
// for call sites that don't supply caller context.
func (idx Index) rewriteOneWithCaller(ref, relKind, callerFile, callerPkgDir string) (string, int) {
	if ref == "" || isHexID(ref) {
		return ref, 0
	}
	id, st := idx.LookupStatusHint(ref, relKind)
	if st == statusRewritten {
		return id, st
	}
	if st == statusAmbiguous && (callerFile != "" || callerPkgDir != "") {
		if localID, ok := idx.lookupBareWithLocality(ref, relKind, callerFile, callerPkgDir); ok {
			return localID, statusRewritten
		}
	}
	// Issue #778 — Java bare CALLS to qualified-name operations.
	// The global bare-name lookup returns statusUnmatched for bare stubs
	// like "merge" or "findRawById" because the entity is named
	// "InventoryService.merge" (qualified). lookupBareWithLocality is
	// only called on statusAmbiguous, so it never fires. Extend to also
	// probe the byMember leaf-name index when the stub is unmatched and
	// the relationship kind is CALLS with caller context available.
	// We do NOT extend this to other relationship kinds (EXTENDS,
	// IMPLEMENTS) because those shapes use Component-family targets which
	// do not carry qualified names in this way.
	if st == statusUnmatched && (callerFile != "" || callerPkgDir != "") &&
		strings.ToUpper(relKind) == "CALLS" && !strings.ContainsAny(ref, ":.#") {
		if localID, ok := idx.lookupBareWithLocality(ref, relKind, callerFile, callerPkgDir); ok {
			return localID, statusRewritten
		}
	}
	return ref, st
}

// lookupBareWithLocality is the wave-9 same-file / same-package tie-breaker
// for ambiguous bare-name lookups. Consulted only after LookupStatusHint
// returned statusAmbiguous — i.e. the global bare-name index has multiple
// candidates and the relKind hint did not narrow to one. We try, in order:
//
//  1. byLocation[callerFile][name]  — local same-file definition
//  2. byLocationKind[callerFile][name][kind] for every kind in the relKind
//     family — same-file but kind-disambiguated, in case byLocation flagged
//     the (file, name) ambiguous because of a SCOPE.* placeholder collision
//  3. byPackageOperation[callerPkgDir][name] for CALLS-shaped relKinds
//  4. byPackageComponent[callerPkgDir][name] for EXTENDS/IMPLEMENTS-shaped
//     relKinds
//
// Returns (id, true) only on an unambiguous hit. Blank-string sentinel
// (ambiguous within file/pkg) returns ("", false) so the caller leaves the
// stub alone — we never silently pick one of multiple same-locality
// definitions.
//
// Same-file preference is language-agnostic — every language has cross-file
// naming collisions on common identifier names (`isValid`, `handler`,
// `helper`). The same-package fallback (steps 3-4) duplicates the
// pre-existing Go-only paths but unconditionally on language because the
// global byName already failed; we cannot bind to a wrong overload
// because the package buckets only carry entities whose SourceFile rolls
// up to the same directory as the caller.
func (idx Index) lookupBareWithLocality(stub, relKind, callerFile, callerPkgDir string) (string, bool) {
	_, name := splitStub(stub)
	if name == "" {
		name = stub
	}
	if name == "" {
		return "", false
	}
	families := hintKinds(relKind)
	if callerFile != "" {
		// Prefer the kind-disambiguated real-entity bucket (no SCOPE.*
		// placeholders) so a same-file SCOPE.Component placeholder never
		// shadows a framework parent that would otherwise classify as
		// ExternalKnown via the python/ts allowlists. Mirrors the tier-1
		// preference in lookupByKindHint (#525).
		if len(families) > 0 {
			if fileBucket := idx.byLocationKindReal[callerFile]; fileBucket != nil {
				if nameBucket := fileBucket[name]; nameBucket != nil {
					var match string
					ambig := false
					for _, k := range families {
						if strings.HasPrefix(k, scopeKindPrefix) {
							continue
						}
						id := nameBucket[k]
						if id == "" {
							continue
						}
						if match != "" && match != id {
							ambig = true
							break
						}
						match = id
					}
					if !ambig && match != "" {
						return match, true
					}
				}
			}
		}
	}
	if callerPkgDir != "" {
		switch strings.ToUpper(relKind) {
		case "CALLS":
			if bucket, ok := idx.byPackageOperation[callerPkgDir]; ok {
				if id, ok := bucket[name]; ok && id != "" {
					return id, true
				}
			}
			// Issue #778 — Java bare CALLS to qualified-name methods.
			// The Java extractor emits method entities with qualified names
			// ("InventoryService.merge") so they land in byMember[file][scope][name]
			// rather than byPackageOperation (which only indexes non-dotted names).
			// When a bare CALLS stub like "merge" reaches here, scan byMember
			// for the callerFile first and then byPackageMember for the callerPkgDir.
			// Return the match only when exactly ONE scope declares a member
			// by this bare name — if two different scopes both declare "find",
			// we cannot pick without additional type information and leave the
			// stub unresolved (correct: ambiguous, not a false bind).
			if callerFile != "" {
				if id, ok := idx.lookupMemberByLeafName(callerFile, name); ok {
					return id, true
				}
			}
			if callerPkgDir != "" {
				if id, ok := idx.lookupPackageMemberByLeafName(callerPkgDir, name); ok {
					return id, true
				}
			}
		case "EXTENDS", "IMPLEMENTS":
			if bucket, ok := idx.byPackageComponent[callerPkgDir]; ok {
				if id, ok := bucket[name]; ok && id != "" {
					return id, true
				}
			}
		}
	}
	// Wave-10 chain-fix C — SCOPE.Component fallback for same-file
	// CALLS targets (#579 client-fixture-b residual analysis). When
	// the real-entity tier above misses and the rel hint is CALLS,
	// a same-file SCOPE.Component placeholder is the correct binding
	// for `const navigate = useNavigate()` / `const isValid = ...` /
	// other value-bound consts that get called like functions inside
	// React components. The wave-9 tier-1 deliberately excludes
	// SCOPE.* placeholders to avoid shadowing imported framework
	// parents (#525); this tier runs only after that path fails and
	// only for CALLS so EXTENDS/IMPLEMENTS continue to require a
	// real Component / Class. Strictly same-file so cross-file
	// collisions remain ambig.
	if callerFile != "" && strings.ToUpper(relKind) == "CALLS" {
		if fileBucket := idx.byLocationKind[callerFile]; fileBucket != nil {
			if nameBucket := fileBucket[name]; nameBucket != nil {
				if id := nameBucket[scopeKindPrefix+"Operation"]; id != "" {
					return id, true
				}
				if id := nameBucket[scopeKindPrefix+"Component"]; id != "" {
					return id, true
				}
			}
		}
	}
	return "", false
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
	// Issue #507 — Python SQL-driver dataaccess refs of the form
	//   scope:dataaccess:<file>#<orm>:<op>:<table>
	// are emitted by internal/extractors/cross/dbmap for psycopg2 /
	// sqlalchemy / asyncpg / aiopg / mysql-connector etc. The matching
	// SCOPE.DataAccess entity normally resolves via byQualifiedName
	// (extractor populates QualifiedName=entityID, issue #507). Anything
	// that slips past that — UNKNOWN-table fallbacks, off-by-one extractor
	// edge cases, dedup misses across re-emitted edges — represents an
	// external SQL surface area (the table is a real schema object, just
	// not modelled as a graph entity yet). Routing to ExternalKnown stops
	// these from polluting bug-extractor on Django/Flask/FastAPI repos
	// (client-fixture-a, Django backend pre-fix). The new external-sql disposition
	// bucket is tracked as a chain-fix.
	if isDataAccessSQLStub(originalStub) {
		return DispositionExternalKnown
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
	// Issue #44 / GraphQL-fix — markdown extractor emits one REFERENCES
	// edge per backtick-quoted literal in a heading (e.g. `theme`,
	// `headers`, `defaultMaxAge` in apollo-server's MDX docs) and one
	// IMPORTS edge per cross-doc `[text](path)` link. These are
	// inherently documentation pointers, NOT extractor bugs: the slug
	// (`theme`) is a reference to an option name, prop, or section that
	// may or may not have a matching code entity, and the link target
	// (`docs/source/data/errors`) is a sibling doc-only path. In a
	// docs-heavy repo like apollo-server they dominate the bug-extractor
	// bucket and obscure real extractor regressions. Tag them
	// DispositionDynamic so they stay visible in graph.json but don't
	// inflate the bug-rate metric.
	if lang == "markdown" {
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
		} else if len(parts) == stubScopeSegments-1 {
			// Issue kafka-chase-578 — the hierarchy extractor's
			// ifaceRef builder emits a 5-segment shape
			// `scope:component:interface:<lang>:<name>` with no
			// `<file>` (interfaces are inferred from `implements`
			// clauses and don't carry a definition file). The
			// trailing segment is the bare interface simple name —
			// extract it for the per-language allowlist checks
			// below so Java framework interfaces (Deserializer,
			// Serializer, Serde, ...) fold to ExternalKnown
			// instead of bug-extractor.
			name = parts[len(parts)-1]
		}
	}
	if name == "" {
		return DispositionUnclassified
	}
	// Issue #44 / GraphQL-fix — TypeScript built-in utility types
	// (`Required`, `Partial`, `Readonly`, etc.) and stdlib globals
	// (`Promise`, `Map`, `Set`, `Array`, etc.) routinely appear as the
	// trailing segment of structural-ref IMPLEMENTS / EXTENDS stubs
	// (`scope:component:interface:typescript:Required`). They are
	// language-level builtins, not extractor bugs.
	if (lang == "typescript" || lang == "javascript") && isTSBuiltinType(name) {
		return DispositionExternalKnown
	}
	// Wave-4 (Python) — Django / DRF / Flask / SQLAlchemy framework
	// base classes routinely appear as the trailing segment of
	// structural-ref EXTENDS / IMPLEMENTS stubs (`scope:component:class:
	// python:foo.py:Model`, `:APIView`, `:RetrieveAPIView`, `:AppConfig`,
	// `:JSONRenderer`, `:BaseUserManager`, `:AbstractBaseUser`,
	// `:SQLAlchemyModelFactory`, etc.) because the parent class is
	// imported from a third-party package and has no in-tree entity.
	// They are framework parent types, not extractor bugs. Gated to
	// lang=="python" so a same-named user class in another language is
	// not shadowed (#94 safer-bias rule).
	if lang == "python" && isPythonExternalBaseType(name) {
		return DispositionExternalKnown
	}
	// Issue kafka-chase-578 — Java EXTENDS / IMPLEMENTS structural-ref
	// stubs (`scope:component:interface:java:Deserializer`,
	// `:Serializer`, `:Serde`, `:Processor`, `:KeyValue`, ...) whose
	// trailing segment is a well-known Apache Kafka Streams / Connect /
	// Common framework interface, an Apache Commons CLI type, a JDK
	// interface (Iterable, Runnable, Serializable, Void), or a
	// regex-leak generic-parameter fragment (`K`, `V`, `V>`, `Long>>`,
	// `X>>`). The hierarchy extractor synthesises these refs from
	// `class Foo implements Bar<X, Y>` declarations where Bar is
	// imported from an external package; without this gate they
	// accumulate in bug-extractor (kafka-streams-examples 12.68%
	// post-#577). Lang-gated to java per safer-bias rule (#94) so a
	// same-named user class in another language is not shadowed.
	if lang == "java" && isJavaExternalBaseType(name) {
		return DispositionExternalKnown
	}
	// Issue java-spring-petclinic-wave — Spring MVC ROUTES_TO edges
	// emit `Route:<path>` -> `Controller:<methodName>` stubs from
	// both the AST-driven composed-route extractor (spring_routes.go)
	// and the YAML regex-based extractor (spring_mvc.yaml). The
	// Java method extractor emits the handler under SCOPE.Operation
	// (not `Controller:` kind), so the kind-bucket lookup misses.
	// Spring's HandlerMapping dispatches by method-name lookup at
	// runtime — the binding is framework-mediated, not an extractor
	// bug. Route to Dynamic. Lang-gated to java (lang on the edge
	// originates from the spring_routes.go Language="java" tag).
	// Also accept lang="" because YAML-emitted edges may not
	// propagate the source language onto the edge properties.
	if (lang == "java" || lang == "") &&
		(strings.HasPrefix(originalStub, "Controller:") ||
			strings.HasPrefix(originalStub, "Route:")) {
		return DispositionDynamic
	}
	// Wave-4 (PHP) — Symfony / Doctrine / PSR / PHPUnit framework
	// interfaces and abstract base classes routinely appear as the
	// trailing segment of structural-ref IMPLEMENTS / EXTENDS stubs
	// (`scope:component:interface:php:UserInterface`,
	// `:EventSubscriberInterface`, `:DataTransformerInterface`,
	// `:PasswordAuthenticatedUserInterface`, `:Constraint`, `:Voter`,
	// `:AbstractType`, `:AbstractController`, `:Command`, etc.) because
	// the parent type is imported from a third-party package and has no
	// in-tree entity. They are framework parent types, not extractor
	// bugs. Gated to lang=="php" so a same-named user class in another
	// language is not shadowed (#94 safer-bias rule).
	if lang == "php" && isPHPExternalBaseType(name) {
		return DispositionExternalKnown
	}
	// Rust wave (S19+) — Tokio / Actix / Serde / Diesel / Rust std
	// traits routinely appear as the trailing segment of
	// structural-ref IMPLEMENTS / EXTENDS stubs
	// (`scope:component:interface:rust:Future`,
	// `:AsyncRead`, `:AsyncWrite`, `:Stream`, `:Serialize`,
	// `:Deserialize`, `:Actor`, `:Handler<Foo>`, `:From<Bar>`,
	// `:Drop`, `:Iterator`, ...) because the trait is imported from
	// std or a third-party crate and has no in-tree entity. They are
	// framework / stdlib trait names, not extractor bugs. The helper
	// strips generic-argument tails (`Handler<Foo>` → `Handler`,
	// `From<cqueue::Entry>` → `From`) before lookup so the entire
	// `Trait<Generic>` family folds in one check. Gated to
	// lang=="rust" so a same-named user trait/struct in another
	// language is not shadowed (#94 safer-bias rule).
	if lang == "rust" && isRustExternalBaseType(name) {
		return DispositionExternalKnown
	}
	// Wave-7 — Python CALLS where the stub leaf is `<Class>.<method>`
	// and the method is a well-known framework-inherited method (DRF
	// GenericAPIView / GenericViewSet pagination + serializer + lookup
	// methods). These show up when the Python extractor records the
	// call site as `self.get_paginated_response(...)` and the resolver
	// retains the enclosing-class qualifier (`AocHarvestViewSet.get_
	// paginated_response`). The method is provided by the third-party
	// parent (`rest_framework.generics.GenericAPIView`), not by user
	// code in the subclass body, so route to ExternalKnown instead of
	// BugExtractor. Gated to lang=="python" to preserve safer-bias
	// rule (#94) for other languages.
	if lang == "python" {
		if dot := strings.LastIndexByte(name, '.'); dot > 0 && dot < len(name)-1 {
			method := name[dot+1:]
			if isPythonExternalInheritedMethod(method) {
				return DispositionExternalKnown
			}
		}
	}
	// Wave-4 (Python) — same allowlist also fires when language is
	// unknown but the stub carries a `Kind:Name` prefix (bare-kind-
	// prefixed category in the diagnostic dump). These edges originate
	// from the cross-language IMPORTS / EXTENDS / DEPENDS_ON synthesiser
	// which doesn't always propagate the source-file language onto the
	// edge properties; nevertheless framework class names like
	// `RetrieveUpdateAPIView`, `AppConfig`, `JSONRenderer`, `Blueprint`
	// are unambiguously framework parents and the lookup is exact-match
	// against a curated allowlist, so the safer-bias rule (#94) is
	// preserved.
	if lang == "" && strings.Contains(originalStub, stubDelim) &&
		!strings.HasPrefix(originalStub, stubPrefixScope) &&
		!strings.HasPrefix(originalStub, stubPrefixExternal) &&
		isPythonExternalBaseType(name) {
		return DispositionExternalKnown
	}
	// Wave-9 (Python) — module-level constant references emitted by the
	// Python framework-extractor as `Model:<SCREAMING_SNAKE>` (e.g.
	// `Model:MA_JURISDICTION_NAME`, `Model:DEFAULT_VIEWSET_ACTIONS`,
	// `Model:PERMISSION_PAGES`). These are module-scope CONSTANT
	// assignments imported from a sibling `settings.py` /
	// `constants.py` module; the base Python extractor only emits
	// SCOPE.Schema/field entities for class-body assignments
	// (#526), so module-scope constants have no in-tree entity and
	// drop to bug-extractor under the `Model:` kind prefix. The leaf
	// name is unambiguously a constant (^[A-Z][A-Z0-9_]+$ with at
	// least one underscore-or-multichar token), so route to Dynamic
	// — the reference is real and resolved at runtime, not an
	// extractor bug. Gated to lang=="python" or lang=="" with a
	// kind-prefixed stub (cross-language synthesiser strips the
	// language tag); safer-bias rule (#94) preserved by the strict
	// SCREAMING_SNAKE shape which never collides with user method or
	// class identifiers in JS/Go/Ruby/etc.
	if (lang == "python" || (lang == "" && strings.Contains(originalStub, stubDelim) &&
		!strings.HasPrefix(originalStub, stubPrefixScope) &&
		!strings.HasPrefix(originalStub, stubPrefixExternal))) &&
		isPythonModuleConstantName(name) {
		return DispositionDynamic
	}
	// Wave-9 (Python) — Celery task dispatch refs (`Task:<name>`)
	// emitted by the Python framework-extractor when a function is
	// decorated with `@app.task` / `@celery.task` / `@shared_task`.
	// The decorator wraps the function so the call site appears as
	// `mytask.delay(...)` or `mytask.apply_async(...)`; the
	// extractor records the wrapped target under the `Task:` kind
	// prefix but the function entity is emitted under SCOPE.Operation
	// without the prefix, so the kind-bucket lookup misses. These
	// dispatches are real but resolved at runtime by the Celery
	// broker — Dynamic is the honest classification. Sample:
	// `Task:changelog_task`, `Task:my_task`, `Task:task_chain`,
	// `Task:app`. Gated to lang=="python" / lang=="" with a
	// kind-prefixed stub.
	if (lang == "python" || (lang == "" && strings.Contains(originalStub, stubDelim) &&
		!strings.HasPrefix(originalStub, stubPrefixScope) &&
		!strings.HasPrefix(originalStub, stubPrefixExternal))) &&
		isPythonCeleryTaskStub(originalStub) {
		return DispositionDynamic
	}
	// Wave-9 (Python) — DRF `@action`-decorated viewset method
	// dispatch refs (`Action:<name>`). Same shape as `Task:` above:
	// the `@action(detail=True)` decorator wraps a viewset method,
	// the extractor records the wrapped target under the `Action:`
	// kind prefix, but the method entity is emitted as
	// SCOPE.Operation without the prefix. The dispatch is real but
	// resolved at runtime by DRF's router. Defensive — no instances
	// observed on client-fixture-a but added for symmetry with
	// Track B and to cover django-rest-framework heavy corpora.
	if (lang == "python" || (lang == "" && strings.Contains(originalStub, stubDelim) &&
		!strings.HasPrefix(originalStub, stubPrefixScope) &&
		!strings.HasPrefix(originalStub, stubPrefixExternal))) &&
		isPythonDRFActionStub(originalStub) {
		return DispositionDynamic
	}
	// Wave-9 (Python) — dotted-lower-head references to a
	// module-level constant: `<pkg>.<module>.<SCREAMING_SNAKE>`
	// (e.g. `app_config.settings.CONST_NAME`,
	// `app_config.settings.OTHER_CONST`). The head
	// segments are a module path (lower_snake) and the trailing
	// segment is a constant. The Python framework-extractor doesn't
	// emit SCOPE entities for module-scope constants (see Track A
	// rationale above) so the dotted lookup misses both the module
	// and the bare leaf. Route to Dynamic since the reference is a
	// real runtime constant import, not an extractor bug. Gated to
	// lang=="python" (the leaf shape — uppercase with underscore —
	// is rare in non-python idioms; safer-bias rule preserved).
	if lang == "python" && isPythonDottedModuleConstant(name) {
		return DispositionDynamic
	}
	// Wave-9 (Python) — receiver-qualified builtin-type method calls
	// (`str.strip`, `str.lower`, `dict.items`, `list.append`,
	// `tuple.count`, etc.). The Python extractor preserves the
	// receiver type name on the call site when the receiver is a
	// bare builtin-type reference rather than an instance variable,
	// producing dotted stubs whose head is one of Python's
	// well-known builtin types and whose leaf is a builtin-method
	// identifier. Route to ExternalKnown — these are stdlib data-
	// model methods, not extractor bugs and not user code. Sample:
	// `str.strip` (26), `str.lower` (12), `str.split` (10),
	// `str.replace` (4), `str.isdigit` (4), `dict.items` (1).
	// Gated to lang=="python" so a same-shaped reference in
	// another language (e.g. Go's `str.Title` receiver pattern) is
	// not shadowed; safer-bias rule (#94) preserved by the strict
	// builtin-type head allowlist.
	if lang == "python" && isPythonBuiltinTypeMethod(name) {
		return DispositionExternalKnown
	}
	// Flask-realworld wave — Python local-module dotted re-export
	// references (`conduit.database.Column`, `conduit.database.Model`,
	// `myapp.utils.helper`). The Python framework-extractor emits
	// SCOPE.Component placeholders at every consumer file for each
	// `from <pkg>.<module> import <Symbol>` so the same dotted name
	// has N placeholders (one per consumer) and nameExists returns
	// true → bug-resolver. The reference itself is real but resolves
	// at runtime to whatever the source module re-exports (commonly
	// an external like `db.Column` aliased as `Column`). Route to
	// Dynamic — the edge stays visible in graph.json but doesn't
	// inflate bug-rate. Gated to lang=="python" so other languages
	// with dotted identifiers are not shadowed; the strict shape
	// (all-lower head segments + identifier leaf with at least 2
	// dots) keeps the safer-bias rule (#94) intact.
	if lang == "python" && isPythonLocalDottedReexport(name) {
		return DispositionDynamic
	}
	// Flask-realworld wave — Python SQLAlchemy `relationship("ClassName")`
	// string references emitted by the framework-extractor as
	// `Model:<Name>` (e.g. `Model:User`, `Model:UserProfile`,
	// `Model:Blueprint`). These are runtime-resolved string class
	// references — SQLAlchemy looks up the class by name when the
	// mapper configures relationships. The `Model` kind bucket only
	// holds entities whose extractor-emitted kind is literally `Model`
	// (a small minority in flask-realworld — Tags + CRUDMixin only);
	// the actual target class is emitted as SCOPE.Component. Bare-name
	// fallback hits ambig because the same class name may also exist
	// as a Relationship entity from a sibling SQLAlchemy backref.
	// Route to Dynamic — the string-keyed lookup is intrinsically
	// runtime-resolved and not an extractor bug. Gated to lang=="python"
	// with a `Model:` prefix and a simple python identifier tail.
	if (lang == "python" || lang == "") && isPythonSQLAlchemyModelStub(originalStub) {
		return DispositionDynamic
	}
	if idx.nameExists(name) {
		return DispositionBugResolver
	}
	return DispositionBugExtractor
}

// isPythonLocalDottedReexport reports whether s is a dotted reference
// of the form `<lower_seg>.<lower_seg>...<Identifier>` with at least
// two dots — i.e. a local-package qualified symbol import like
// `conduit.database.Column`. The head segments must be lower_snake
// (canonical Python module path) and the leaf must be a valid Python
// identifier (lower_snake OR PascalCase/CamelCase; SCREAMING_SNAKE is
// already routed by isPythonDottedModuleConstant so this catches the
// non-constant cases). At least two head segments are required to
// avoid binding `foo.bar` shapes that may be plain attribute access on
// a local variable; `pkg.mod.Symbol` is the canonical "import from
// local package" shape. Flask-realworld wave (post-pandas residual
// 6.58% → ≤3%). Gated upstream to lang=="python".
func isPythonLocalDottedReexport(s string) bool {
	// Need at least two dots → at least three segments.
	first := strings.IndexByte(s, '.')
	if first <= 0 {
		return false
	}
	last := strings.LastIndexByte(s, '.')
	if last == first {
		return false
	}
	if last >= len(s)-1 {
		return false
	}
	head := s[:last]
	leaf := s[last+1:]
	// Leaf must be a simple Python identifier (any case).
	if !isSimplePythonIdentifier(leaf) {
		return false
	}
	// Each head segment must be a lower_snake module-path segment.
	for _, seg := range strings.Split(head, ".") {
		if seg == "" {
			return false
		}
		for i, c := range seg {
			switch {
			case c >= 'a' && c <= 'z':
				// ok
			case c >= '0' && c <= '9':
				if i == 0 {
					return false
				}
			case c == '_':
				// ok
			default:
				return false
			}
		}
	}
	return true
}

// isPythonSQLAlchemyModelStub reports whether stub is of the form
// `Model:<Name>` where <Name> is a simple Python identifier (any
// case). Used to route SQLAlchemy `relationship("Class")` string
// references — emitted as `Model:<Name>` by the python framework-
// extractor — to Dynamic. Flask-realworld wave addition. The leading
// `Model:` prefix and the strict identifier shape keep the
// safer-bias rule intact (no plausible non-python use of this exact
// shape).
func isPythonSQLAlchemyModelStub(stub string) bool {
	const prefix = "Model:"
	if !strings.HasPrefix(stub, prefix) {
		return false
	}
	name := stub[len(prefix):]
	return isSimplePythonIdentifier(name)
}

// isSimplePythonIdentifier reports whether s is a valid Python
// identifier of any case (snake_case, PascalCase, CamelCase,
// SCREAMING_SNAKE). Must start with a letter or underscore and
// contain only letters, digits, and underscores. Empty rejected.
func isSimplePythonIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
			// ok
		case c >= 'A' && c <= 'Z':
			// ok
		case c == '_':
			// ok
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// isPythonModuleConstantName reports whether s is a SCREAMING_SNAKE_CASE
// identifier of the form ^[A-Z][A-Z0-9_]*$ AND contains either an
// underscore or is at least 3 characters long — restricting to names
// that are unambiguously module-level CONSTANT declarations in Python
// idiom. Single uppercase letters (`F`, `T`) and short two-letter
// names (`PI`, `OK`) would collide with type variables and HTTP-status
// constants and are excluded. Wave-9 client-fixture-a addition for
// `Model:MA_JURISDICTION_NAME`-style stubs where the base Python
// extractor doesn't emit entities for module-scope assignments.
func isPythonModuleConstantName(s string) bool {
	if len(s) < 3 {
		return false
	}
	hasUnderscore := false
	for i, c := range s {
		switch {
		case c >= 'A' && c <= 'Z':
			// ok
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		case c == '_':
			if i == 0 || i == len(s)-1 {
				return false
			}
			hasUnderscore = true
		default:
			return false
		}
	}
	// Require an underscore so two-letter all-caps (`OK`, `PI`) and
	// HTTP-status short names don't bind here.
	return hasUnderscore
}

// isPythonCeleryTaskStub reports whether stub is of the form
// `Task:<name>` where <name> is a lower_snake_case or PascalCase
// identifier (i.e. not itself a module path). Used to route
// `@app.task` / `@celery.task` / `@shared_task` dispatch references
// (`mytask.delay(...)`, `mytask.apply_async(...)`) to Dynamic when
// the wrapped function entity is emitted without the `Task:` kind
// prefix and the kind-bucket lookup misses. Wave-9 client-fixture-a
// addition. Gated upstream to lang=="python" or lang="" with a
// kind-prefixed stub.
func isPythonCeleryTaskStub(stub string) bool {
	const prefix = "Task:"
	if !strings.HasPrefix(stub, prefix) {
		return false
	}
	name := stub[len(prefix):]
	return isSimpleIdentifier(name)
}

// isPythonDRFActionStub reports whether stub is of the form
// `Action:<name>` where <name> is a simple identifier. DRF
// `@action(detail=True)` viewset-method decorator wraps a method;
// the dispatch reference arrives under the `Action:` kind prefix
// without a matching kind bucket. Wave-9 client-fixture-a defensive
// addition (no instances observed on client-fixture-a; added for
// symmetry with isPythonCeleryTaskStub and to cover DRF-heavy
// corpora). Gated upstream same as Task.
func isPythonDRFActionStub(stub string) bool {
	const prefix = "Action:"
	if !strings.HasPrefix(stub, prefix) {
		return false
	}
	name := stub[len(prefix):]
	return isSimpleIdentifier(name)
}

// isPythonDottedModuleConstant reports whether s is a dotted-path
// reference of the form `<lower_seg>.<lower_seg>...<SCREAMING_SNAKE>`
// with at least one dot and the trailing segment being a
// module-level CONSTANT (per isPythonModuleConstantName). Used to
// route refs like `app_config.settings.CONST_NAME` to Dynamic
// when the Python framework-extractor doesn't emit SCOPE entities
// for module-scope constants. Wave-9 client-fixture-a addition.
// Gated upstream to lang=="python" (the trailing SCREAMING_SNAKE
// leaf is rare in non-python idioms; safer-bias rule preserved).
func isPythonDottedModuleConstant(s string) bool {
	dot := strings.LastIndexByte(s, '.')
	if dot <= 0 || dot >= len(s)-1 {
		return false
	}
	head := s[:dot]
	leaf := s[dot+1:]
	if !isPythonModuleConstantName(leaf) {
		return false
	}
	// Head must be a sequence of lower-case dotted segments — a
	// canonical Python module path. Empty segments, leading dots,
	// or uppercase characters disqualify (avoids binding
	// `ClassName.METHOD` shapes which are class-method refs, not
	// module-constant refs).
	if head == "" {
		return false
	}
	for _, seg := range strings.Split(head, ".") {
		if seg == "" {
			return false
		}
		for i, c := range seg {
			switch {
			case c >= 'a' && c <= 'z':
				// ok
			case c >= '0' && c <= '9':
				if i == 0 {
					return false
				}
			case c == '_':
				// ok
			default:
				return false
			}
		}
	}
	return true
}

// isPythonBuiltinTypeMethod reports whether s is a dotted reference
// `<builtin_type>.<method>` where <builtin_type> is one of Python's
// well-known builtin types (str, dict, list, tuple, set, frozenset,
// bytes, bytearray, int, float, bool). Used by classifyDispositionLang
// to route receiver-qualified builtin-type method calls (e.g.
// `str.strip`, `dict.items`) to ExternalKnown when the resolver
// can't bind them as in-tree entities. Wave-9 client-fixture-a
// addition; gated upstream to lang=="python".
func isPythonBuiltinTypeMethod(s string) bool {
	dot := strings.IndexByte(s, '.')
	if dot <= 0 || dot >= len(s)-1 {
		return false
	}
	head := s[:dot]
	switch head {
	case "str", "dict", "list", "tuple", "set", "frozenset",
		"bytes", "bytearray", "int", "float", "bool":
		// fall through — head is a builtin type
	default:
		return false
	}
	// Reject further dotted segments — only a single dotted leaf
	// is a builtin method (avoid binding `str.foo.bar`).
	rest := s[dot+1:]
	if strings.ContainsRune(rest, '.') {
		return false
	}
	return isSimpleIdentifier(rest)
}

// isSimpleIdentifier reports whether s is a non-empty Python
// identifier consisting only of letters, digits, and underscores
// with a non-digit leading character. Used by Celery / DRF action
// stub classifiers to filter out dotted module paths and other
// non-identifier shapes that should not bind to a wrapped function
// entity. Wave-9 client-fixture-a helper.
func isSimpleIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
			// ok
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// isPythonExternalBaseType reports whether s is a well-known Django /
// Django REST Framework / Flask / SQLAlchemy framework base class name
// commonly used as a parent in `class Foo(Model)` / `class Bar(APIView)`-
// style declarations. Used by classifyDispositionLang to route
// EXTENDS / IMPLEMENTS structural-ref stubs whose trailing segment is a
// framework parent into ExternalKnown rather than BugExtractor. Curated
// from real django-realworld / flask-realworld bug-extractor samples;
// the lang=="python" gate at the call site keeps the safer-bias rule
// (#94) intact for other languages.
func isPythonExternalBaseType(s string) bool {
	_, ok := pythonExternalBaseTypes[s]
	return ok
}

// isJavaExternalBaseType reports whether s is a well-known Apache
// Kafka Streams / Connect / Common framework interface, an Apache
// Commons CLI type, a JDK interface routinely used as EXTENDS /
// IMPLEMENTS parent, or a generic-parameter regex-leak fragment.
// Used by classifyDispositionLang to route Java structural-ref
// EXTENDS / IMPLEMENTS stubs (`scope:component:interface:java:Foo`)
// into ExternalKnown rather than BugExtractor. Curated from real
// kafka-streams-examples bug-extractor samples (issue kafka-chase-578
// post-#577 file-entity unhiding); the lang=="java" gate at the call
// site keeps the safer-bias rule (#94) intact for other languages.
func isJavaExternalBaseType(s string) bool {
	if _, ok := javaExternalBaseTypes[s]; ok {
		return true
	}
	// Regex-leak: the hierarchy extractor's `genericRE` (`<[^>]*>`)
	// only strips one level of generics and splits the `implements`
	// list on `,` even inside nested `<...>`, producing trailing
	// fragments like `Deserializer<Pair<Double`, `Long>>`, `V>`,
	// `X>>`, `Pair<X`, `OrderValidation>`. The angle-bracket suffix
	// or the embedded `<` is itself a structural marker: a
	// well-formed Java identifier never contains `<` or `>` so any
	// trailing segment carrying one is a parser-residue stub, not a
	// real local symbol the resolver could ever bind. Folding to
	// ExternalKnown is correct (the underlying type IS external when
	// observed as a parameterised parent) and prevents the noise
	// from inflating bug-rate.
	if strings.ContainsAny(s, "<>") {
		return true
	}
	return false
}

// isPHPExternalBaseType reports whether s is a well-known Symfony /
// Doctrine / PSR / PHPUnit framework interface or abstract base class
// commonly used as the EXTENDS / IMPLEMENTS parent in a Symfony /
// Laravel codebase. Used by classifyDispositionLang to route PHP
// structural-ref IMPLEMENTS / EXTENDS stubs into ExternalKnown rather
// than BugExtractor (kind-mismatch). Curated from real symfony-demo
// bug-resolver samples (wave-4 PHP); the lang=="php" gate at the call
// site keeps the safer-bias rule (#94) intact for other languages.
func isPHPExternalBaseType(s string) bool {
	_, ok := phpExternalBaseTypes[s]
	return ok
}

var phpExternalBaseTypes = map[string]struct{}{
	// Symfony Security — user / authenticator / voter interfaces.
	"UserInterface":                            {},
	"PasswordAuthenticatedUserInterface":       {},
	"LegacyPasswordAuthenticatedUserInterface": {},
	"EquatableUserInterface":                   {},
	"UserProviderInterface":                    {},
	"PasswordUpgraderInterface":                {},
	"AuthenticatorInterface":                   {},
	"AuthenticationEntryPointInterface":        {},
	"VoterInterface":                           {},
	"Voter":                                    {},
	"AbstractAuthenticator":                    {},
	"AbstractLoginFormAuthenticator":           {},
	"AccessDecisionStrategyInterface":          {},

	// Symfony EventDispatcher.
	"EventSubscriberInterface": {},
	"EventDispatcherInterface": {},
	"Event":                    {},
	"StoppableEventInterface":  {},

	// Symfony Form / Validator / OptionsResolver.
	"DataTransformerInterface":     {},
	"FormTypeInterface":            {},
	"FormTypeExtensionInterface":   {},
	"FormBuilderInterface":         {},
	"FormInterface":                {},
	"FormFactoryInterface":         {},
	"FormViewInterface":            {},
	"AbstractType":                 {},
	"AbstractTypeExtension":        {},
	"Constraint":                   {},
	"ConstraintValidatorInterface": {},
	"ConstraintValidator":          {},
	"ValidatorInterface":           {},
	"OptionsResolverInterface":     {},

	// Symfony HttpKernel / HttpFoundation.
	"KernelInterface":             {},
	"HttpKernelInterface":         {},
	"TerminableInterface":         {},
	"ControllerResolverInterface": {},
	"ArgumentResolverInterface":   {},
	"Kernel":                      {},
	"BaseKernel":                  {},
	"AbstractController":          {},
	"AbstractBundle":              {},
	"Bundle":                      {},
	"BundleInterface":             {},
	"ExtensionInterface":          {},
	"Extension":                   {},
	"ConfigurableExtension":       {},
	"CompilerPassInterface":       {},

	// Symfony Console.
	"Command":                {},
	"ContainerAwareCommand":  {},
	"LockableTrait":          {},
	"OutputInterface":        {},
	"InputInterface":         {},
	"InputDefinition":        {},
	"CommandLoaderInterface": {},

	// Symfony DI / Config.
	"ContainerAwareInterface": {},
	"ContainerInterface":      {},
	"ConfigurationInterface":  {},
	"TreeBuilder":             {},

	// Symfony Routing.
	"RouterInterface":              {},
	"UrlGeneratorInterface":        {},
	"UrlMatcherInterface":          {},
	"RequestContextAwareInterface": {},
	"LoaderInterface":              {},

	// Symfony Messenger / Serializer / Mailer / Notifier.
	"MessageHandlerInterface": {},
	"NormalizerInterface":     {},
	"DenormalizerInterface":   {},
	"EncoderInterface":        {},
	"DecoderInterface":        {},
	"MailerInterface":         {},
	"NotifierInterface":       {},

	// Doctrine ORM / DBAL / Persistence / Common.
	"EntityRepository":          {},
	"ServiceEntityRepository":   {},
	"ObjectRepository":          {},
	"AbstractMigration":         {},
	"AbstractRepository":        {},
	"ObjectManager":             {},
	"EntityManagerInterface":    {},
	"ManagerRegistry":           {},
	"AbstractIdGenerator":       {},
	"FixtureInterface":          {},
	"OrderedFixtureInterface":   {},
	"DependentFixtureInterface": {},
	"AbstractFixture":           {},
	"Fixture":                   {},
	"AbstractEnumType":          {},

	// Twig. ExtensionInterface is shared with Symfony DI — both are
	// external framework types, so a single entry covers both.
	"AbstractExtension":         {},
	"RuntimeExtensionInterface": {},
	"GlobalsInterface":          {},

	// PSR / PHPUnit / monolog common base interfaces. PSR-11
	// ContainerInterface shares the simple name with Symfony DI
	// ContainerInterface — both are external framework types.
	"LoggerInterface":           {},
	"LoggerAwareInterface":      {},
	"HttpClientInterface":       {},
	"ResponseInterface":         {},
	"RequestInterface":          {},
	"ServerRequestInterface":    {},
	"StreamInterface":           {},
	"UriInterface":              {},
	"MessageInterface":          {},
	"TestCase":                  {},
	"KernelTestCase":            {},
	"WebTestCase":               {},
	"BrowserKitAssertionsTrait": {},
	"ProcessorInterface":        {},
	"FormatterInterface":        {},
	"HandlerInterface":          {},
}

// isPythonExternalInheritedMethod reports whether s is the leaf of a
// `<UserClass>.<method>` stub where the method is provided by a
// well-known framework parent (DRF GenericAPIView / GenericViewSet,
// django.test.TestCase, channels.consumer.AsyncConsumer). Used by
// classifyDispositionLang (Python-gated) so an extractor stub like
// `AocHarvestViewSet.get_paginated_response` — where the user
// subclass body does NOT define `get_paginated_response` because the
// method comes from `GenericAPIView` — routes to ExternalKnown
// instead of BugExtractor. Wave-7 client-fixture-a addition.
func isPythonExternalInheritedMethod(s string) bool {
	_, ok := pythonExternalInheritedMethods[s]
	return ok
}

var pythonExternalInheritedMethods = map[string]struct{}{
	// DRF GenericAPIView / GenericViewSet pagination + lookup +
	// serializer hooks. Each is provided by the parent class and
	// commonly invoked via `self.<method>(...)` from user view-set
	// bodies — but never re-implemented locally.
	"get_paginated_response":   {},
	"paginate_queryset":        {},
	"get_serializer":           {},
	"get_serializer_class":     {},
	"get_serializer_context":   {},
	"get_object":               {},
	"get_queryset":             {},
	"get_paginator":            {},
	"perform_create":           {},
	"perform_update":           {},
	"perform_destroy":          {},
	"check_permissions":        {},
	"check_object_permissions": {},
	"get_permissions":          {},
	"get_authenticators":       {},
	"get_throttles":            {},
	"get_renderers":            {},
	"get_parsers":              {},
	"get_content_negotiator":   {},
	"get_exception_handler":    {},
	"get_view_name":            {},
	"get_view_description":     {},
	"initial":                  {},
	"initialize_request":       {},
	"finalize_response":        {},
	"handle_exception":         {},
	"permission_denied":        {},
	// channels AsyncConsumer dispatch lifecycle.
	"channel_receive": {},
	"send":            {},
	"close":           {},
	"accept":          {},
	"group_add":       {},
	"group_discard":   {},
	"group_send":      {},
	// Django management BaseCommand lifecycle.
	"execute":       {},
	"create_parser": {},
	"print_help":    {},
	// Wave-8 — django.test / unittest.TestCase assert + lifecycle
	// methods. Surface as `<MyTest>.assertEqual` etc. when extractor
	// preserves the enclosing-class qualifier on `self.assertX(...)`
	// calls. The method is provided by unittest.TestCase /
	// django.test.TestCase / rest_framework.test.APITestCase (already
	// in pythonExternalBaseTypes since wave-7) so route to ExternalKnown.
	"assertEqual":              {},
	"assertNotEqual":           {},
	"assertTrue":               {},
	"assertFalse":              {},
	"assertIn":                 {},
	"assertNotIn":              {},
	"assertIs":                 {},
	"assertIsNot":              {},
	"assertIsNone":             {},
	"assertIsNotNone":          {},
	"assertIsInstance":         {},
	"assertNotIsInstance":      {},
	"assertRaises":             {},
	"assertRaisesRegex":        {},
	"assertRaisesRegexp":       {},
	"assertWarns":              {},
	"assertWarnsRegex":         {},
	"assertLogs":               {},
	"assertNoLogs":             {},
	"assertGreater":            {},
	"assertGreaterEqual":       {},
	"assertLess":               {},
	"assertLessEqual":          {},
	"assertAlmostEqual":        {},
	"assertNotAlmostEqual":     {},
	"assertDictEqual":          {},
	"assertListEqual":          {},
	"assertSetEqual":           {},
	"assertTupleEqual":         {},
	"assertCountEqual":         {},
	"assertSequenceEqual":      {},
	"assertMultiLineEqual":     {},
	"assertRegex":              {},
	"assertNotRegex":           {},
	"assertRegexpMatches":      {},
	"assertNotRegexpMatches":   {},
	"assertDictContainsSubset": {},
	"assertItemsEqual":         {},
	"assertNumQueries":         {},
	"assertTemplateUsed":       {},
	"assertTemplateNotUsed":    {},
	"assertRedirects":          {},
	"assertContains":           {},
	"assertNotContains":        {},
	"assertFormError":          {},
	"assertFormsetError":       {},
	"assertFieldOutput":        {},
	"assertHTMLEqual":          {},
	"assertHTMLNotEqual":       {},
	"assertJSONEqual":          {},
	"assertJSONNotEqual":       {},
	"assertXMLEqual":           {},
	"assertXMLNotEqual":        {},
	"assertQuerysetEqual":      {},
	"assertQuerySetEqual":      {},
	"assertInHTML":             {},
	"fail":                     {},
	"setUp":                    {},
	"tearDown":                 {},
	"setUpClass":               {},
	"tearDownClass":            {},
	"setUpTestData":            {},
	"addCleanup":               {},
	"doCleanups":               {},
	"skipTest":                 {},
	"subTest":                  {},
	"shortDescription":         {},
	"countTestCases":           {},
	"defaultTestResult":        {},
	"id":                       {},
	"_pre_setup":               {},
	"_post_teardown":           {},
	// Wave-8 — DRF GenericViewSet / generic view inherited methods
	// beyond wave-7's pagination/serializer subset. Provided by
	// rest_framework.viewsets.GenericViewSet, mixins.{List,Create,
	// Retrieve,Update,Destroy}ModelMixin, and views.APIView.dispatch.
	"filter_queryset":          {},
	"get_success_headers":      {},
	"list":                     {},
	"retrieve":                 {},
	"create":                   {},
	"update":                   {},
	"partial_update":           {},
	"destroy":                  {},
	"dispatch":                 {},
	"http_method_not_allowed":  {},
	"options":                  {},
	"perform_authentication":   {},
	"raise_uncaught_exception": {},
	"reverse_action":           {},
	"get_extra_actions":        {},
	// Wave-8 — django.db.models.Manager / QuerySet inherited methods.
	// Show up as `<X>Manager.<method>` (e.g. `UserManager.get`,
	// `UserManager.model`, `UserManager.normalize_email`) when the
	// user manager subclass body doesn't re-define them.
	"normalize_email":      {},
	"make_random_password": {},
	"get_by_natural_key":   {},
	"contribute_to_class":  {},
	// Wave-8 pass-2 — pymongo Collection.find + Django Manager.get
	// receiver-stripped variants. These show up across client-fixture-a
	// as `_collection.find`, `_get_collection.find`, `UserManager.get`,
	// `UserManager.model` where the receiver is a Mongo collection or
	// Django manager instance.
	"find":   {}, // pymongo Collection.find / Django Manager queryset.find
	"model":  {}, // Django Manager.model (back-ref to the bound model class)
	"select": {},
	// Wave-8 pass-3 — Django middleware `get_response` callable
	// injected by Django on every middleware class via __init__
	// (`def __init__(self, get_response): self.get_response = ...`).
	// Calls like `self.get_response(request)` surface as
	// `<MyMiddleware>.get_response` against the user middleware class.
	"get_response": {},
	// Wave-8 — pymongo Collection / Database inherited methods. Show
	// up as `_collection.find_one`, `_get_collection.find`,
	// `self._collection.aggregate`, etc. when a Mongo-typed attr is
	// the receiver and the extractor keeps the receiver name.
	"find_one":                 {},
	"find_one_and_update":      {},
	"find_one_and_replace":     {},
	"find_one_and_delete":      {},
	"insert_one":               {},
	"insert_many":              {},
	"update_one":               {},
	"update_many":              {},
	"replace_one":              {},
	"delete_one":               {},
	"delete_many":              {},
	"aggregate":                {},
	"count_documents":          {},
	"estimated_document_count": {},
	"distinct":                 {},
	"bulk_write":               {},
	"watch":                    {},
	"with_options":             {},
	"rename":                   {},
	"list_collection_names":    {},
	"list_database_names":      {},
	"create_index":             {},
	"create_indexes":           {},
	"drop_index":               {},
	"drop_indexes":             {},
	"list_indexes":             {},
	"index_information":        {},
	// Wave-8 — Celery task chain operations. Used as chained dotted
	// methods on signatures/groups/chords like `chord(...).apply_async()`,
	// `mytask.s(...).set(...)`. Bare names already in pythonBareNames;
	// these handle the `<receiver>.method` chained form.
	"apply":       {},
	"apply_async": {},
	"delay":       {},
	"retry":       {},
	"on_error":    {},
	"link":        {},
	"link_error":  {},
}

var pythonExternalBaseTypes = map[string]struct{}{
	// Django auth / contrib base classes.
	"AbstractBaseUser": {},
	"AbstractUser":     {},
	"BaseUserManager":  {},
	"PermissionsMixin": {},
	"AppConfig":        {},
	// Django REST Framework view + viewset + renderer + permission base
	// classes (when used as a parent — `class Foo(APIView)`).
	"APIView":                      {},
	"GenericAPIView":               {},
	"ListAPIView":                  {},
	"RetrieveAPIView":              {},
	"CreateAPIView":                {},
	"UpdateAPIView":                {},
	"DestroyAPIView":               {},
	"ListCreateAPIView":            {},
	"RetrieveUpdateAPIView":        {},
	"RetrieveDestroyAPIView":       {},
	"RetrieveUpdateDestroyAPIView": {},
	"ViewSet":                      {},
	"GenericViewSet":               {},
	"ModelViewSet":                 {},
	"ReadOnlyModelViewSet":         {},
	"Serializer":                   {},
	"ModelSerializer":              {},
	"HyperlinkedModelSerializer":   {},
	"JSONRenderer":                 {},
	"BrowsableAPIRenderer":         {},
	"APIException":                 {},
	"AuthenticationFailed":         {},
	"NotAuthenticated":             {},
	"PermissionDenied":             {},
	"NotFound":                     {},
	"ValidationError":              {},
	"DefaultRouter":                {},
	"SimpleRouter":                 {},
	"BaseAuthentication":           {},
	"BasePermission":               {},
	"BasePagination":               {},
	"PageNumberPagination":         {},
	"LimitOffsetPagination":        {},
	"CursorPagination":             {},
	// Django ORM / Forms / Admin base classes.
	"Model":         {},
	"ModelForm":     {},
	"ModelAdmin":    {},
	"TabularInline": {},
	"StackedInline": {},
	"Manager":       {},
	"QuerySet":      {},
	// Flask / Werkzeug / Flask-RESTful / Flask-SQLAlchemy base classes.
	"Flask":            {},
	"Blueprint":        {},
	"Resource":         {},
	"Api":              {},
	"MethodView":       {},
	"Schema":           {},
	"Cache":            {},
	"Migrate":          {},
	"JWTManager":       {},
	"LoginManager":     {},
	"SQLAlchemy":       {},
	"IntegrityError":   {},
	"MethodNotAllowed": {},
	// Marshmallow / factory_boy / pytest-factoryboy base classes.
	"SQLAlchemyModelFactory":   {},
	"DjangoModelFactory":       {},
	"Factory":                  {},
	"PostGenerationMethodCall": {},
	"SubFactory":               {},
	"LazyAttribute":            {},
	// SQLAlchemy column / mapper primitives.
	"Column":   {},
	"Table":    {},
	"Index":    {},
	"Sequence": {},
	// `object` surfaces as bare-kind-prefixed (`Model:object`) from
	// Python `class Config(object):` declarations.
	"object": {},
	// Wave-7 — Django test framework base classes. Used as parents in
	// `class Foo(TestCase):` / `class Bar(APITestCase):` declarations
	// across django.test, rest_framework.test, and channels.testing.
	"TestCase":                   {},
	"LiveServerTestCase":         {},
	"TransactionTestCase":        {},
	"SimpleTestCase":             {},
	"ChannelsLiveServerTestCase": {},
	"APITestCase":                {},
	"APISimpleTestCase":          {},
	"APITransactionTestCase":     {},
	"APILiveServerTestCase":      {},
	"Client":                     {},
	"RequestFactory":             {},
	// Wave-7 — Django management command base class (subclassed by
	// every `core/management/commands/*.py` module's `Command` class).
	"BaseCommand": {},
	"Command":     {},
	// Wave-7 — DRF Simple JWT view base classes (subclassed in
	// `urls.py` / `viewsets.py` to customise serializers).
	"TokenObtainPairView": {},
	"TokenRefreshView":    {},
	"TokenBlacklistView":  {},
	"TokenVerifyView":     {},
	// Wave-7 — Django Channels consumer base classes.
	"AsyncConsumer":              {},
	"SyncConsumer":               {},
	"WebsocketConsumer":          {},
	"AsyncWebsocketConsumer":     {},
	"JsonWebsocketConsumer":      {},
	"AsyncJsonWebsocketConsumer": {},
	// Wave-7 pass-2 — Django utils + DRF mixin + parser parents.
	// Pulled from client-fixture-a residual after pass-1.
	"MiddlewareMixin":  {}, // django.utils.deprecation.MiddlewareMixin
	"FormParser":       {}, // rest_framework.parsers.FormParser
	"MultiPartParser":  {},
	"JSONParser":       {},
	"FileUploadParser": {},
	// DRF generic view mixins (subclassed in custom view-set hierarchies).
	"ListModelMixin":     {},
	"CreateModelMixin":   {},
	"RetrieveModelMixin": {},
	"UpdateModelMixin":   {},
	"DestroyModelMixin":  {},
	// Wave-8 — django.db.models F-expressions / Func / aggregations.
	// These appear as `Model:F`, `Model:Lower`, `Model:Count` etc. when
	// imported from django.db.models and used inside annotate()/filter().
	"F":                      {},
	"Q":                      {},
	"Value":                  {},
	"Case":                   {},
	"When":                   {},
	"Exists":                 {},
	"OuterRef":               {},
	"Subquery":               {},
	"Prefetch":               {},
	"ExpressionWrapper":      {},
	"Func":                   {},
	"Count":                  {},
	"Sum":                    {},
	"Avg":                    {},
	"Min":                    {},
	"Max":                    {},
	"StdDev":                 {},
	"Variance":               {},
	"Coalesce":               {},
	"Concat":                 {},
	"Lower":                  {},
	"Upper":                  {},
	"Length":                 {},
	"Substr":                 {},
	"Trim":                   {},
	"LTrim":                  {},
	"RTrim":                  {},
	"Cast":                   {},
	"Greatest":               {},
	"Least":                  {},
	"Now":                    {},
	"TruncDate":              {},
	"TruncDay":               {},
	"TruncMonth":             {},
	"TruncYear":              {},
	"TruncWeek":              {},
	"TruncHour":              {},
	"TruncMinute":            {},
	"TruncSecond":            {},
	"ExtractYear":            {},
	"ExtractMonth":           {},
	"ExtractDay":             {},
	"ExtractWeekDay":         {},
	"ExtractHour":            {},
	"SearchQuery":            {},
	"SearchVector":           {},
	"SearchRank":             {},
	"ArrayField":             {},
	"JSONField":              {},
	"HStoreField":            {},
	"DateField":              {},
	"DateTimeField":          {},
	"CharField":              {},
	"TextField":              {},
	"IntegerField":           {},
	"BooleanField":           {},
	"DecimalField":           {},
	"FloatField":             {},
	"ForeignKey":             {},
	"OneToOneField":          {},
	"ManyToManyField":        {},
	"GenericForeignKey":      {},
	"ModelField":             {},
	"FileExtensionValidator": {},
	"EmailValidator":         {},
	"MinValueValidator":      {},
	"MaxValueValidator":      {},
	"RegexValidator":         {},
	"URLValidator":           {},
	"ContentType":            {},
	// Django HTTP / responses / exceptions.
	"HttpRequest":            {},
	"HttpResponse":           {},
	"HttpResponseBadRequest": {},
	"HttpResponseNotFound":   {},
	"HttpResponseRedirect":   {},
	"HttpResponseForbidden":  {},
	"JsonResponse":           {},
	"FileResponse":           {},
	"StreamingHttpResponse":  {},
	"DisallowedHost":         {},
	"CommandError":           {},
	"ImageDownloadError":     {},
	"ImportError":            {},
	// Django channels routing helpers.
	"AuthMiddlewareStack":         {},
	"URLRouter":                   {},
	"ProtocolTypeRouter":          {},
	"AllowedHostsOriginValidator": {},
	// Django mail.
	"EmailMultiAlternatives": {},
	"EmailMessage":           {},
	// DRF permissions / auth / pagination extras.
	"AllowAny":                  {},
	"IsAuthenticated":           {},
	"IsAdminUser":               {},
	"IsAuthenticatedOrReadOnly": {},
	"DjangoModelPermissions":    {},
	"DjangoFilterBackend":       {},
	"TokenAuthentication":       {},
	"SessionAuthentication":     {},
	"BasicAuthentication":       {},
	"AnonymousUser":             {},
	"APIClient":                 {},
	"InvalidToken":              {},
	// pymongo primitives.
	"MongoClient":  {},
	"Collection":   {},
	"InsertOne":    {},
	"UpdateOne":    {},
	"DeleteOne":    {},
	"UpdateMany":   {},
	"DeleteMany":   {},
	"ReplaceOne":   {},
	"PyMongoError": {},
	"InvalidId":    {},
	"Decimal128":   {},
	"ASCENDING":    {},
	"DESCENDING":   {},
	// Celery primitives.
	"Celery":    {},
	"Task":      {},
	"Signature": {},
	"chord":     {},
	"chain":     {},
	"group":     {},
	// typing module (Python type-annotation aliases that show up as
	// EXTENDS targets when used in `class Foo(List[X]):` style).
	"Any":            {},
	"List":           {},
	"Dict":           {},
	"Tuple":          {},
	"Set":            {},
	"FrozenSet":      {},
	"Optional":       {},
	"Union":          {},
	"Callable":       {},
	"Iterable":       {},
	"Iterator":       {},
	"Generator":      {},
	"Mapping":        {},
	"MutableMapping": {},
	"Type":           {},
	"TypeVar":        {},
	"Generic":        {},
	"Protocol":       {},
	"Literal":        {},
	"Final":          {},
	"ClassVar":       {},
	// Common Python stdlib classes that surface as Model:<X> when
	// imported and used as parents or in type annotations.
	"Decimal":             {},
	"BytesIO":             {},
	"StringIO":            {},
	"ContextVar":          {},
	"NamedTemporaryFile":  {},
	"ThreadPoolExecutor":  {},
	"ProcessPoolExecutor": {},
	"SequenceMatcher":     {},
	"DataFrame":           {},
	"DictReader":          {},
	"DictWriter":          {},
	"MagicMock":           {},
	"Mock":                {},
	"PropertyMock":        {},
	"AsyncMock":           {},
	// boto3 / botocore exception types.
	"ClientError":   {},
	"BotoCoreError": {},
	// PIL / Pillow imaging.
	"Image":        {},
	"ImageEnhance": {},
	"ImageDraw":    {},
	"ImageFont":    {},
	// python-docx / openpyxl primitives.
	"Document":    {},
	"Font":        {},
	"Alignment":   {},
	"PatternFill": {},
	"RGBColor":    {},
	"OxmlElement": {},
	"Workbook":    {},
	"Inches":      {},
	"Pt":          {},
	"Cm":          {},
	"Emu":         {},
	"Matrix":      {},
	// jwt / cryptography helpers.
	"InvalidTokenError": {},
	"CryptoExtension":   {},
	// BeautifulSoup / lxml.
	"BeautifulSoup": {},
	// channels Message.
	"Message": {},
	// Wave-8 pass-3 — additional Django / DRF / Celery / stdlib types
	// surfaced as `Model:<X>` cross-language EXTENDS targets in the
	// pass-2 client-fixture-a residual.
	"NoCredentialsError":       {}, // botocore.exceptions
	"ObjectDoesNotExist":       {}, // django.core.exceptions
	"ObjectId":                 {}, // bson.ObjectId
	"OperationalError":         {}, // django.db.OperationalError / psycopg2
	"OrderedDict":              {}, // collections.OrderedDict
	"Path":                     {}, // pathlib.Path
	"PeriodicTask":             {}, // django_celery_beat.models.PeriodicTask
	"QueryDict":                {}, // django.http.QueryDict
	"Queue":                    {}, // queue.Queue / multiprocessing.Queue
	"RefreshToken":             {}, // rest_framework_simplejwt.tokens.RefreshToken
	"Request":                  {}, // rest_framework.request.Request
	"Response":                 {}, // rest_framework.response.Response
	"ReturnDocument":           {}, // pymongo.ReturnDocument
	"SAFE_METHODS":             {}, // rest_framework.permissions.SAFE_METHODS
	"Signal":                   {}, // django.dispatch.Signal
	"SoftTimeLimitExceeded":    {}, // celery.exceptions
	"Token":                    {}, // rest_framework.authtoken.models.Token
	"TokenError":               {}, // rest_framework_simplejwt.exceptions
	"TypedMultipleChoiceField": {}, // django.forms.fields
	"UUID":                     {}, // uuid.UUID
	"WSGIRequest":              {}, // django.core.handlers.wsgi.WSGIRequest
	"model_to_dict":            {}, // django.forms.models.model_to_dict
	// python-docx WD_* enum constants.
	"WD_ALIGN_PARAGRAPH":     {},
	"WD_ALIGN_VERTICAL":      {},
	"WD_BREAK":               {},
	"WD_ROW_HEIGHT_RULE":     {},
	"WD_STYLE_TYPE":          {},
	"WD_PARAGRAPH_ALIGNMENT": {},
	"WD_TABLE_ALIGNMENT":     {},
	"WD_LINE_SPACING":        {},

	// pandas wave (post-wave-4 residual at 9.80% → ship-gate <=5%).
	// Structural-ref EXTENDS targets — stdlib + typing base classes
	// frequently used as Python class parents that previously hit
	// bug-extractor because the resolver could not find a definition.
	// Each is conservative: well-known stdlib/typing class names that
	// can be a parent (`class Foo(NamedTuple)`, `class C(TypedDict)`,
	// `class M(ChainMap)`, `class S(Enum)`, `class L(list)`).
	"NamedTuple": {}, // typing.NamedTuple
	"TypedDict":  {}, // typing.TypedDict
	"Enum":       {}, // enum.Enum
	"IntEnum":    {}, // enum.IntEnum
	"StrEnum":    {}, // enum.StrEnum
	"Flag":       {}, // enum.Flag
	"ChainMap":   {}, // collections.ChainMap
	"list":       {}, // builtin list (used as parent for FrozenList etc.)
	"dict":       {}, // builtin dict
	"tuple":      {}, // builtin tuple
	"set":        {}, // builtin set
	"type":       {}, // builtin type (metaclass-as-base usage)
	// Generic / Protocol already in map above.

	// pandas-internal mixin/ABC base classes — these are pandas's own
	// public-internal hierarchy roots that subclasses across the
	// codebase extend. They land in bug-resolver (ambig-kind) because
	// the resolver sees multi-kind matches; allowlisting them as
	// external-known base types routes EXTENDS edges to ExternalKnown
	// cleanly. Distinctive pandas names — no plausible Django/Flask
	// collision.
	"PandasObject":                {},
	"OpsMixin":                    {},
	"SelectionMixin":              {},
	"IndexOpsMixin":               {},
	"GroupByIndexingMixin":        {},
	"NoNewAttributesMixin":        {},
	"PandasDelegate":              {},
	"ExtensionArray":              {},
	"ExtensionDtype":              {},
	"BaseStringArray":             {},
	"BaseMaskedArray":             {},
	"BaseMaskedDtype":             {},
	"NumericArray":                {},
	"NDArrayBackedExtensionArray": {},
	"NDArrayBackedExtensionIndex": {},
	"NDArrayBacked":               {},
	"NDFrame":                     {},
	"NDFrameIndexerBase":          {},
	"NDFrameDescriberAbstract":    {},
	"IntervalMixin":               {},
	"DatetimeTimedeltaMixin":      {},
	"DatetimeIndexOpsMixin":       {},
	"DatetimeLikeArrayMixin":      {},
	"DataFrameXchg":               {},
	"PandasDataFrameXchg":         {},
	"ArrowExtensionArray":         {},
	"ArrowStringArrayMixin":       {},
	"ObjectStringArrayMixin":      {},
	"StorageExtensionDtype":       {},
	"ExtensionArrayNaResult":      {},
	"PeriodDtypeBase":             {},
	"_GroupByMixin":               {},
	"GroupBy":                     {},
	"BaseGroupBy":                 {},
	"BaseWindow":                  {},
	"BaseWindowGroupby":           {},
	"RollingAndExpandingMixin":    {},
	// pandas interchange protocol abstract bases (in
	// pandas/core/interchange/dataframe_protocol.py).
	"ABC":     {}, // abc.ABC (used as `class Foo(ABC)`)
	"ABCMeta": {}, // abc.ABCMeta — also surfaces via metaclass kwarg parser leak

	// pandas wave pass-2 — more pandas-internal mixin/base classes
	// surfaced in post-pass-1 bug-resolver residual.
	"NumericDtype":        {},
	"Buffer":              {},
	"NumpyExtensionArray": {},
	"Grouper":             {},
	"ExtensionIndex":      {},
	"DirNamesMixin":       {},
	"DatetimeLikeBlock":   {},
	"BaseExprVisitor":     {},

	// Flask-realworld wave (post-pandas residual at 6.58%, target ≤3%).
	// Python builtin exception types — used as parents in `class Foo(Exception):`
	// across virtually every Python codebase that defines custom errors
	// (flask-realworld's `InvalidUsage(Exception)`, django/drf custom error
	// classes, library-internal hierarchy roots). All builtin — no plausible
	// user-defined class collision (the names are reserved in Python idiom).
	"Exception":                 {},
	"BaseException":             {},
	"ValueError":                {},
	"TypeError":                 {},
	"KeyError":                  {},
	"AttributeError":            {},
	"RuntimeError":              {},
	"NotImplementedError":       {},
	"StopIteration":             {},
	"StopAsyncIteration":        {},
	"GeneratorExit":             {},
	"KeyboardInterrupt":         {},
	"SystemExit":                {},
	"OSError":                   {},
	"IOError":                   {},
	"FileNotFoundError":         {},
	"FileExistsError":           {},
	"PermissionError":           {},
	"IsADirectoryError":         {},
	"NotADirectoryError":        {},
	"TimeoutError":              {},
	"ConnectionError":           {},
	"ConnectionRefusedError":    {},
	"ConnectionResetError":      {},
	"ConnectionAbortedError":    {},
	"BrokenPipeError":           {},
	"InterruptedError":          {},
	"ChildProcessError":         {},
	"ProcessLookupError":        {},
	"BlockingIOError":           {},
	"LookupError":               {},
	"IndexError":                {},
	"ArithmeticError":           {},
	"ZeroDivisionError":         {},
	"OverflowError":             {},
	"FloatingPointError":        {},
	"AssertionError":            {},
	"NameError":                 {},
	"UnboundLocalError":         {},
	"ModuleNotFoundError":       {},
	"SyntaxError":               {},
	"IndentationError":          {},
	"TabError":                  {},
	"SystemError":               {},
	"MemoryError":               {},
	"ReferenceError":            {},
	"RecursionError":            {},
	"BufferError":               {},
	"EOFError":                  {},
	"UnicodeError":              {},
	"UnicodeDecodeError":        {},
	"UnicodeEncodeError":        {},
	"UnicodeTranslateError":     {},
	"Warning":                   {},
	"UserWarning":               {},
	"DeprecationWarning":        {},
	"PendingDeprecationWarning": {},
	"SyntaxWarning":             {},
	"RuntimeWarning":            {},
	"FutureWarning":             {},
	"ImportWarning":             {},
	"UnicodeWarning":            {},
	"BytesWarning":              {},
	"ResourceWarning":           {},

	// Flask-realworld wave — Flask-Login / Flask-JWT-Extended /
	// Flask-RESTful / Flask-CORS / Flask-SocketIO / Flask-Marshmallow
	// base classes routinely used as `class Foo(LoginManager):` /
	// `class Bar(UserMixin):` / `class Baz(AnonymousUserMixin):`.
	"UserMixin":            {}, // flask_login.UserMixin
	"AnonymousUserMixin":   {}, // flask_login.AnonymousUserMixin
	"MixinMeta":            {}, // flask_login meta
	"SocketIO":             {}, // flask_socketio.SocketIO
	"Namespace":            {}, // flask_socketio.Namespace / argparse.Namespace
	"CORS":                 {}, // flask_cors.CORS
	"SQLAlchemyAutoSchema": {}, // flask_marshmallow.sqla.SQLAlchemyAutoSchema
	"SQLAlchemySchema":     {}, // flask_marshmallow.sqla.SQLAlchemySchema
	// Marshmallow validators / decorators used as parents (rare but
	// observed in custom Validator subclasses).
	"Validator":      {}, // marshmallow.validate.Validator
	"Range":          {}, // marshmallow.validate.Range
	"OneOf":          {}, // marshmallow.validate.OneOf
	"Email":          {}, // marshmallow.validate.Email
	"Regexp":         {}, // marshmallow.validate.Regexp / wtforms
	"NoneOf":         {}, // marshmallow.validate.NoneOf
	"ContainsOnly":   {}, // marshmallow.validate.ContainsOnly
	"Equal":          {}, // marshmallow.validate.Equal
	"ContainsNoneOf": {}, // marshmallow.validate.ContainsNoneOf
	"URL":            {}, // marshmallow.validate.URL
	// Flask-WTF / WTForms base classes.
	"FlaskForm":           {},
	"Form":                {}, // wtforms.Form (collides with django Form? Django Form already implicit via Model)
	"FieldList":           {},
	"FormField":           {},
	"StringField":         {},
	"PasswordField":       {},
	"SubmitField":         {},
	"HiddenField":         {},
	"SelectField":         {},
	"SelectMultipleField": {},
	"RadioField":          {},
	"DateTimeLocalField":  {},
	"TimeField":           {},
	// Flask-Migrate.
	"AlembicCommand": {},
	// Flask-Caching / Flask-Session base classes.
	"SessionInterface": {},

	// Flask-realworld wave — SQLAlchemy ORM + Core base classes
	// commonly used as parents.
	"BaseQuery":            {}, // flask_sqlalchemy.BaseQuery
	"Pagination":           {}, // flask_sqlalchemy.Pagination
	"AbstractConcreteBase": {}, // sqlalchemy.ext.declarative.AbstractConcreteBase
	"ConcreteBase":         {},
	"DeferredReflection":   {},
	"DeclarativeMeta":      {},
	"MetaData":             {},
	"ForeignKeyConstraint": {},
	"PrimaryKeyConstraint": {},
	"UniqueConstraint":     {},
	"CheckConstraint":      {},
	"BigInteger":           {},
	"SmallInteger":         {},
	"Numeric":              {},
	"Float":                {},
	"Boolean":              {},
	"DateTime":             {},
	"Date":                 {},
	"Time":                 {},
	"Interval":             {},
	"LargeBinary":          {},
	"JSON":                 {},
	// SQLAlchemy session / engine. (Session is in pythonBareNames as the
	// botocore Session bare-name; pythonExternalBaseTypes is a separate
	// map for structural-ref EXTENDS targets.)
	"Engine":     {},
	"Connection": {},
}

// javaExternalBaseTypes is the Java-language-gated allowlist of
// EXTENDS / IMPLEMENTS parent simple names that originate in
// third-party packages (Apache Kafka Streams / Connect / Common,
// Apache Commons CLI, JUnit) or in the JDK itself. The hierarchy
// extractor synthesises `scope:component:interface:java:<Name>` stubs
// from `class Foo implements Bar` declarations; when Bar is imported
// from `org.apache.kafka.common.serialization.Deserializer` no
// in-tree entity matches and the stub lands in bug-extractor.
// Curated from kafka-streams-examples bug-extractor samples
// post-#577 file-entity unhiding (kafka-chase-578).
//
// Conservative selection rule (#94 safer-bias): only Pascal-case
// names extremely unlikely to collide with user-defined class simple
// names in Java sources, plus single-letter type-parameter
// conventions (K, V, T, E, R) which are never user-defined classes.
var javaExternalBaseTypes = map[string]struct{}{
	// Apache Kafka Streams DSL / Processor API interfaces
	"Deserializer":                    {},
	"Serializer":                      {},
	"Serde":                           {},
	"Processor":                       {},
	"ProcessorSupplier":               {},
	"ProcessorContext":                {},
	"Transformer":                     {},
	"TransformerSupplier":             {},
	"ValueTransformer":                {},
	"ValueTransformerSupplier":        {},
	"ValueTransformerWithKey":         {},
	"ValueTransformerWithKeySupplier": {},
	"RocksDBConfigSetter":             {},
	"StateStore":                      {},
	"StateRestoreCallback":            {},
	"StateStoreSupplier":              {},
	"StoreBuilder":                    {},
	"KeyValueStore":                   {},
	"WindowStore":                     {},
	"SessionStore":                    {},
	"ReadOnlyKeyValueStore":           {},
	"ReadOnlyWindowStore":             {},
	"ReadOnlySessionStore":            {},
	"Topology":                        {},
	"TopologyTestDriver":              {},
	"KafkaStreams":                    {},
	"StreamsBuilder":                  {},
	"KStream":                         {},
	"KTable":                          {},
	"KGroupedStream":                  {},
	"KGroupedTable":                   {},
	"GlobalKTable":                    {},
	"Materialized":                    {},
	"Consumed":                        {},
	"Produced":                        {},
	"Grouped":                         {},
	"Joined":                          {},
	"StreamJoined":                    {},
	"Windowed":                        {},
	"TimeWindows":                     {},
	"SessionWindows":                  {},
	"SlidingWindows":                  {},
	"Suppressed":                      {},
	"KeyValue":                        {}, // org.apache.kafka.streams.KeyValue (also generic container)
	"Pair":                            {}, // commonly a tuple in Kafka examples / commons-lang3
	"Schemas":                         {}, // kafka-streams-examples helper class
	"JSerdes":                         {}, // kafka-streams-examples helper class
	// Kafka Connect / Common framework
	"Converter":        {},
	"HeaderConverter":  {},
	"SinkConnector":    {},
	"SinkTask":         {},
	"SourceConnector":  {},
	"SourceTask":       {},
	"ConnectorContext": {},
	"SourceRecord":     {},
	"SinkRecord":       {},
	"ConfigDef":        {},
	// Kafka clients (common ones referenced as IMPLEMENTS parents
	// or via generic-leaked fragments)
	"ConsumerInterceptor": {},
	"ProducerInterceptor": {},
	"PartitionAssignor":   {},
	"Partitioner":         {},
	// JDK functional / marker interfaces routinely used as
	// IMPLEMENTS parents in Java codebases
	"Iterable":      {},
	"Iterator":      {},
	"Serializable":  {},
	"Cloneable":     {},
	"Comparable":    {},
	"Comparator":    {},
	"Runnable":      {},
	"Callable":      {},
	"AutoCloseable": {},
	"Closeable":     {},
	"Void":          {}, // java.lang.Void as a generic type parameter
	"Number":        {},
	"Throwable":     {},
	"CharSequence":  {},
	// JDK primitive wrappers when they leak as generic parameters
	// of an `implements` clause (Deserializer<Integer> split on `,`
	// leaves `Integer>` which the `<>` filter catches; this row
	// covers the no-bracket simple-name case).
	"String":    {},
	"Integer":   {},
	"Long":      {},
	"Double":    {},
	"Float":     {},
	"Boolean":   {},
	"Short":     {},
	"Byte":      {},
	"Character": {},
	"Object":    {},
	// Apache Commons CLI (kafka-streams-examples interactive-queries
	// REST CLI parses command-line arguments via commons-cli).
	"Option":            {},
	"Options":           {},
	"CommandLine":       {},
	"CommandLineParser": {},
	"HelpFormatter":     {},
	"DefaultParser":     {},
	"ParseException":    {},
	// Test frameworks (JUnit + utility)
	"TestCondition": {}, // kafka org.apache.kafka.test.TestCondition
	"TestCase":      {},
	// Generic type-parameter conventions (K, V, T, E, R, U, S, N).
	// These are never user-defined class simple names in idiomatic
	// Java; they leak into the hierarchy extractor's IMPLEMENTS
	// list when the regex captures the inner generics list of a
	// `<T extends Foo, K, V>` parameterised parent. Folding to
	// ExternalKnown matches reality (the parent type IS external)
	// and is safer than letting them inflate bug-rate.
	"K": {},
	"V": {},
	"T": {},
	"E": {},
	"R": {},
	"U": {},
	"S": {},
	"N": {},
	"X": {},
	"Y": {},
	// kafka-streams-examples sample-specific interface (microservices
	// orchestration example uses `OrderValidation` as a Serde
	// type parameter and `Emailer` / `Service` as marker
	// interfaces in tutorial app code that pull from a
	// streams-examples library jar).
	"OrderValidation": {},
	"Emailer":         {},
	"Service":         {},

	// Issue java-spring-petclinic-wave — Spring Framework / Spring Boot
	// / Spring Data / Spring MVC / Spring Security framework
	// interfaces and abstract base classes routinely appear as the
	// trailing segment of structural-ref EXTENDS / IMPLEMENTS stubs
	// (`scope:component:interface:java:WebMvcConfigurer`,
	// `:ApplicationListener`, `:RuntimeHintsRegistrar`, `:Formatter`,
	// `:Validator`, ...) because the parent type is imported from a
	// Spring jar and has no in-tree entity. Curated from
	// spring-petclinic bug-resolver samples post-#593 cpp-spdlog wave.
	// Lang-gated to java per #94 safer-bias rule.
	//
	// Spring Core / Beans / Context.
	"ApplicationListener":            {},
	"ApplicationContextInitializer":  {},
	"ApplicationContextAware":        {},
	"ApplicationEventPublisherAware": {},
	"BeanPostProcessor":              {},
	"BeanFactoryPostProcessor":       {},
	"FactoryBean":                    {},
	"InitializingBean":               {},
	"DisposableBean":                 {},
	"EnvironmentAware":               {},
	"EnvironmentPostProcessor":       {},
	"ResourceLoaderAware":            {},
	"SmartLifecycle":                 {},
	"Lifecycle":                      {},
	"Ordered":                        {},
	"PriorityOrdered":                {},
	"ImportSelector":                 {},
	"ImportBeanDefinitionRegistrar":  {},
	"Condition":                      {},
	// Spring Boot.
	"SpringBootServletInitializer": {},
	"CommandLineRunner":            {},
	"ApplicationRunner":            {},
	"WebServerFactoryCustomizer":   {},
	"ErrorViewResolver":            {},
	"FailureAnalyzer":              {},
	// Spring AOT (RuntimeHints).
	"RuntimeHintsRegistrar": {},
	// Spring MVC / Web.
	"WebMvcConfigurer":                {},
	"WebMvcConfigurationSupport":      {},
	"HandlerInterceptor":              {},
	"HandlerMethodArgumentResolver":   {},
	"HandlerMethodReturnValueHandler": {},
	"HandlerExceptionResolver":        {},
	"HttpMessageConverter":            {},
	"Filter":                          {},
	"OncePerRequestFilter":            {},
	"WebFilter":                       {},
	"WebMvcRegistrations":             {},
	// Spring WebFlux.
	"WebFluxConfigurer": {},
	"WebFilterChain":    {},
	// Spring Data / JPA / Repositories.
	"JpaRepository":              {},
	"CrudRepository":             {},
	"PagingAndSortingRepository": {},
	"Repository":                 {},
	"ReactiveCrudRepository":     {},
	"ReactiveMongoRepository":    {},
	"MongoRepository":            {},
	"AttributeConverter":         {},
	"Specification":              {},
	// Spring Validation / Conversion.
	"Validator":           {},
	"ConstraintValidator": {},
	"Formatter":           {},
	"GenericConverter":    {},
	"ConverterFactory":    {},
	// Spring Security.
	"WebSecurityConfigurerAdapter":         {},
	"AbstractWebSecurityConfigurerAdapter": {},
	"UserDetailsService":                   {},
	"UserDetails":                          {},
	"AuthenticationProvider":               {},
	"AuthenticationManager":                {},
	"GrantedAuthority":                     {},
	"PasswordEncoder":                      {},
	"SecurityFilterChain":                  {},
	// Spring Boot Test slices (commonly extended in tests).
	"SpringBootTest": {},
	"WebMvcTest":     {},
	"DataJpaTest":    {},
	"JsonTest":       {},
	"RestClientTest": {},
	// jakarta.persistence / JPA standard interfaces.
	"EntityManager":        {},
	"EntityManagerFactory": {},
	"AttributeOverride":    {},
	// jakarta.servlet.
	"HttpServletRequest":        {},
	"HttpServletResponse":       {},
	"ServletContextListener":    {},
	"ServletContextInitializer": {},
	// SLF4J / logging.
	"Logger":        {},
	"LoggerFactory": {},
	// AspectJ.
	"MethodInterceptor": {},
}

// isRustExternalBaseType reports whether s is a well-known Rust
// stdlib / Tokio / Actix / Serde / Diesel / common-ecosystem trait
// commonly used as the parent in `impl Trait for Foo` or
// `trait Bar: Trait` declarations. Used by classifyDispositionLang to
// route Rust structural-ref IMPLEMENTS / EXTENDS stubs
// (`scope:component:interface:rust:Future`,
// `scope:component:interface:rust:From<Foo>`, ...) into ExternalKnown
// rather than BugExtractor. The leading generic-argument tail is
// stripped before lookup so the entire `Trait<...>` family folds in a
// single check (`Handler<Foo>`, `Handler<server::Message>`, and
// `Handler<Vec<Bar>>` all collapse to `Handler`). Curated from real
// tokio / actix-examples / mini-redis bug-resolver samples; the
// lang=="rust" gate at the call site keeps the safer-bias rule (#94)
// intact for other languages.
func isRustExternalBaseType(s string) bool {
	base := s
	// Strip the generic-argument tail (`Handler<Foo>` → `Handler`).
	if lt := strings.IndexByte(base, '<'); lt > 0 {
		base = base[:lt]
	}
	// Strip the leading module path (`std::error::Error` → `Error`,
	// `ops::Sub` → `Sub`, `task::Schedule` → `Schedule`). The
	// hierarchy extractor preserves the fully-qualified path for
	// trait references that are imported via a `use` statement with
	// a path prefix, and the structural-ref's trailing segment is the
	// only part we need to match against the curated allowlist.
	if cc := strings.LastIndex(base, "::"); cc >= 0 {
		base = base[cc+2:]
	}
	_, ok := rustExternalBaseTypes[base]
	return ok
}

// rustExternalBaseTypes is the Rust-language-gated allowlist of
// stdlib + popular-crate trait names that legitimately appear as the
// trailing segment of structural-ref IMPLEMENTS / EXTENDS stubs.
// Lookups strip the generic-argument tail in isRustExternalBaseType,
// so keys here are bare trait names without `<...>`. Curated from
// real tokio / actix-examples / mini-redis bug-resolver samples.
var rustExternalBaseTypes = map[string]struct{}{
	// std::marker / core derive + auto traits.
	"Send":          {},
	"Sync":          {},
	"Sized":         {},
	"Unpin":         {},
	"Copy":          {},
	"Clone":         {},
	"Debug":         {},
	"Default":       {},
	"Display":       {},
	"Drop":          {},
	"Hash":          {},
	"PartialEq":     {},
	"Eq":            {},
	"PartialOrd":    {},
	"Ord":           {},
	"Error":         {},
	"RefUnwindSafe": {},
	"UnwindSafe":    {},
	// std::convert + std::borrow.
	"From":      {},
	"Into":      {},
	"TryFrom":   {},
	"TryInto":   {},
	"AsRef":     {},
	"AsMut":     {},
	"Borrow":    {},
	"BorrowMut": {},
	"ToOwned":   {},
	// std::ops + std::iter + std::convert traits commonly implemented.
	"Deref":               {},
	"DerefMut":            {},
	"Iterator":            {},
	"IntoIterator":        {},
	"DoubleEndedIterator": {},
	"ExactSizeIterator":   {},
	"FusedIterator":       {},
	"FromIterator":        {},
	"Extend":              {},
	"Add":                 {},
	"Sub":                 {},
	"Mul":                 {},
	"Div":                 {},
	"Rem":                 {},
	"Neg":                 {},
	"Not":                 {},
	"BitAnd":              {},
	"BitOr":               {},
	"BitXor":              {},
	"Shl":                 {},
	"Shr":                 {},
	"Index":               {},
	"IndexMut":            {},
	"Fn":                  {},
	"FnMut":               {},
	"FnOnce":              {},
	// std::io.
	"Read":       {},
	"Write":      {},
	"Seek":       {},
	"BufRead":    {},
	"IsTerminal": {},
	// std::fmt.
	"Binary":   {},
	"Octal":    {},
	"LowerHex": {},
	"UpperHex": {},
	"LowerExp": {},
	"UpperExp": {},
	"Pointer":  {},
	// std::os::unix / windows raw-fd / raw-handle traits.
	"AsFd":          {},
	"AsHandle":      {},
	"AsRawFd":       {},
	"AsRawHandle":   {},
	"AsRawSocket":   {},
	"FromRawFd":     {},
	"FromRawHandle": {},
	"FromRawSocket": {},
	"IntoRawFd":     {},
	"IntoRawHandle": {},
	"IntoRawSocket": {},
	"OwnedFd":       {},
	"OwnedHandle":   {},
	// std::net.
	"ToSocketAddrs": {},
	// std::thread / std::process killers.
	"Termination": {},
	// Tokio / async traits.
	"Future":          {},
	"Stream":          {},
	"Sink":            {},
	"AsyncRead":       {},
	"AsyncWrite":      {},
	"AsyncBufRead":    {},
	"AsyncSeek":       {},
	"AsyncReadExt":    {},
	"AsyncWriteExt":   {},
	"AsyncBufReadExt": {},
	"AsyncSeekExt":    {},
	"StreamExt":       {},
	"SinkExt":         {},
	"TryStreamExt":    {},
	"FutureExt":       {},
	"TryFutureExt":    {},
	// Tokio-specific runtime traits seen in the real corpora.
	"Wake":               {},
	"WakerRef":           {},
	"Wait":               {},
	"UringFd":            {},
	"Source":             {},
	"Schedule":           {},
	"Storage":            {},
	"Semaphore":          {},
	"Kill":               {},
	"OrphanQueue":        {},
	"InstrumentedFuture": {},
	"InternalStream":     {},
	"ReadBuffer":         {},
	"Completable":        {},
	"Cancellable":        {},
	"RotatorSelect":      {},
	"AssertSync":         {},
	"Overflow":           {},
	// Serde.
	"Serialize":        {},
	"Deserialize":      {},
	"Serializer":       {},
	"Deserializer":     {},
	"DeserializeOwned": {},
	"SerializeStruct":  {},
	"SerializeSeq":     {},
	"SerializeMap":     {},
	"Visitor":          {},
	// Actix-web / actix actor framework parent traits / response types.
	"Actor":           {},
	"Handler":         {},
	"StreamHandler":   {},
	"WriteHandler":    {},
	"MessageResponse": {},
	"Recipient":       {},
	"Supervised":      {},
	"SystemService":   {},
	"ArbiterService":  {},
	"ActorContext":    {},
	"AsyncContext":    {},
	"FromRequest":     {},
	"Responder":       {},
	"ResponseError":   {},
	"MessageBody":     {},
	"Service":         {},
	"ServiceFactory":  {},
	"Transform":       {},
	"Decoder":         {},
	"Encoder":         {},
	"ImplNetwork":     {},
	// Diesel / SQLx / common ORM traits seen in actix-examples.
	"Queryable":    {},
	"Insertable":   {},
	"Identifiable": {},
	"AsChangeset":  {},
	"FromRow":      {},
	// rand / distributions.
	"Distribution": {},
	// std::process / clone helpers.
	"ToOwn": {},
	// ---------------------------------------------------------------------
	// Rust wave-2 (S20+) — trait names curated from real diagnostic
	// samples on actix-examples / tokio. Each is a stdlib or popular-crate
	// trait that appears as the parent in `impl Trait for Foo` and is
	// imported via a `use` statement with a path prefix — the structural-
	// ref's trailing segment is what we match against. The lang=="rust"
	// gate at the call site preserves the safer-bias rule (#94).
	// ---------------------------------------------------------------------
	// actix-web / actix actor framework — additional parent traits seen
	// on actix-examples impls.
	"Message":       {},
	"Configuration": {},
	"Context":       {},
	// std::sealed / std::net private traits (the hierarchy extractor
	// preserves the path; lookup strips the path and gets the bare name).
	"ToSocketAddrsPriv": {},
	// std::ops / iter additions (Sub<Duration>, Add<Duration> base).
	"AddAssign":   {},
	"SubAssign":   {},
	"BitOrAssign": {},
	// tokio runtime additional traits.
	"AssertSend": {},
}

// isTSBuiltinType reports whether s is a TypeScript / JavaScript
// language built-in type or utility-type name. Used by
// classifyDispositionLang to route IMPLEMENTS / EXTENDS edges whose
// target is a language builtin into ExternalKnown rather than
// BugExtractor (issue #44).
func isTSBuiltinType(s string) bool {
	_, ok := tsBuiltinTypes[s]
	return ok
}

var tsBuiltinTypes = map[string]struct{}{
	// TypeScript utility types (https://www.typescriptlang.org/docs/handbook/utility-types.html)
	"Partial": {}, "Required": {}, "Readonly": {}, "Pick": {}, "Omit": {},
	"Record": {}, "Exclude": {}, "Extract": {}, "NonNullable": {},
	"Parameters": {}, "ConstructorParameters": {}, "ReturnType": {},
	"InstanceType": {}, "ThisParameterType": {}, "OmitThisParameter": {},
	"ThisType": {}, "Uppercase": {}, "Lowercase": {}, "Capitalize": {},
	"Uncapitalize": {}, "Awaited": {},
	// JS / TS global types
	"Promise": {}, "Map": {}, "Set": {}, "WeakMap": {}, "WeakSet": {},
	"Array": {}, "ReadonlyArray": {}, "Object": {}, "Function": {},
	"Date": {}, "RegExp": {}, "Error": {}, "TypeError": {}, "RangeError": {},
	"SyntaxError": {}, "ReferenceError": {}, "EvalError": {}, "URIError": {},
	"Number": {}, "String": {}, "Boolean": {}, "BigInt": {}, "Symbol": {},
	"Iterable": {}, "Iterator": {}, "IterableIterator": {}, "Generator": {},
	"AsyncIterable": {}, "AsyncIterator": {}, "AsyncIterableIterator": {},
	"AsyncGenerator": {}, "Proxy": {}, "Reflect": {}, "JSON": {}, "Math": {},
	"ArrayBuffer": {}, "SharedArrayBuffer": {}, "DataView": {},
	"Int8Array": {}, "Uint8Array": {}, "Uint8ClampedArray": {},
	"Int16Array": {}, "Uint16Array": {}, "Int32Array": {}, "Uint32Array": {},
	"Float32Array": {}, "Float64Array": {}, "BigInt64Array": {}, "BigUint64Array": {},
	// DOM / browser globals frequently appearing in TS code
	"Element": {}, "HTMLElement": {}, "Node": {}, "Document": {}, "Window": {},
	"Event": {}, "EventTarget": {}, "Headers": {}, "Request": {}, "Response": {},
	"URL": {}, "URLSearchParams": {}, "FormData": {}, "Blob": {}, "File": {},
	"AbortController": {}, "AbortSignal": {},
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

	// Issue #614 — Go interface-field dispatch. Build a name-keyed index
	// over every IMPLEMENTS edge in the embedded relationships so a CALLS
	// edge stamped with Properties["interface_dispatch_type"] can fan out
	// to the implementing struct's method. The IMPLEMENTS edge is emitted
	// by the Go extractor as `<implementerStruct> -IMPLEMENTS-> <interface>`
	// with bare names; resolution happens here. Same-name collisions are
	// accumulated so the inner lookup can pick the unique
	// (pkgDir, implementerName, member) entry from byPackageMember.
	type implCandidate struct {
		name       string
		sourceFile string
	}
	implementersByInterfaceName := map[string][]implCandidate{}
	for k := range records {
		recName := records[k].Name
		recFile := records[k].SourceFile
		if recName == "" {
			continue
		}
		for _, r := range records[k].Relationships {
			if strings.ToUpper(r.Kind) != "IMPLEMENTS" {
				continue
			}
			ifaceName := r.ToID
			if ifaceName == "" || isHexID(ifaceName) {
				continue
			}
			// Skip structural-ref ToIDs — issue #614 only fires when the
			// edge carries a bare interface name (Go extractor today).
			if strings.Contains(ifaceName, ":") {
				continue
			}
			implementersByInterfaceName[ifaceName] = append(implementersByInterfaceName[ifaceName], implCandidate{
				name:       recName,
				sourceFile: recFile,
			})
		}
	}

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
		parentSourceFile := normalizePath(records[k].SourceFile)
		parentPkgDir := pkgDirOf(parentSourceFile)
		for j := range rels {
			r := &rels[j]
			lang := relLanguage(r)
			if lang == "" {
				lang = parentLang
			}
			if r.FromID != "" && !isHexID(r.FromID) {
				orig := r.FromID
				newID, st := idx.rewriteOneWithCaller(r.FromID, r.Kind, parentSourceFile, parentPkgDir)
				r.FromID = newID
				applyEndpointStats(&stats, st, true)
				d := idx.classifyDispositionLang(r.FromID, orig, lang, allow)
				stats.recordDisposition(d, orig)
			} else if isHexID(r.FromID) {
				stats.recordDisposition(DispositionResolved, r.FromID)
			}
			if r.ToID != "" && !isHexID(r.ToID) {
				orig := r.ToID
				// Issue #614 — Go cross-package interface-field dispatch.
				// When the Go extractor stamps
				// Properties["interface_dispatch_type"] on a CALLS edge
				// (a method calling `<recv>.<Field>.<method>()` where
				// Field is a struct field of an interface type), look up
				// every implementer of that interface (harvested above
				// from IMPLEMENTS edges across the corpus) and probe
				// byPackageMember[implPkgDir][implName][member]. If
				// EXACTLY ONE candidate resolves, rewrite the ToID.
				// Multiple distinct hits fall through to the existing
				// resolution paths (and ultimately Dynamic / Unmatched)
				// — we never bind to an arbitrary implementer to avoid
				// false-positive cross-package edges.
				//
				// Approximation rationale: full Go interface-satisfaction
				// analysis needs a real type checker. The IMPLEMENTS
				// edges we already emit (intra-file method-set superset
				// in attachImplementsRelationships) are the conservative
				// signal — a struct only reaches the candidate list if
				// the extractor proved its method set is a superset of
				// the interface's. Combined with the "exactly one match"
				// gate, the surfaced CALLS edge is precise.
				if ifaceType := r.Properties["interface_dispatch_type"]; ifaceType != "" {
					ifaceName := ifaceType
					// Strip a leading `*` and any package qualifier
					// (`store.Store` → `Store`) to align with the
					// bare-name key used in implementersByInterfaceName.
					ifaceName = strings.TrimPrefix(ifaceName, "*")
					if dot := strings.LastIndexByte(ifaceName, '.'); dot >= 0 && dot < len(ifaceName)-1 {
						ifaceName = ifaceName[dot+1:]
					}
					if candidates := implementersByInterfaceName[ifaceName]; len(candidates) > 0 {
						var hit string
						hits := 0
						for _, c := range candidates {
							implPkgDir := pkgDirOf(normalizePath(c.sourceFile))
							if implPkgDir == "" {
								continue
							}
							id, ok := idx.lookupPackageMember(implPkgDir, c.name, r.ToID)
							if !ok || id == "" {
								continue
							}
							if hit == "" {
								hit = id
							} else if hit != id {
								hits = 2
								break
							}
							hits++
						}
						if hits == 1 && hit != "" {
							r.ToID = hit
							applyEndpointStats(&stats, statusRewritten, false)
							d := idx.classifyDispositionLang(r.ToID, orig, lang, allow)
							stats.recordDisposition(d, orig)
							continue
						}
					}
				}
				// Issue #148 — Go same-package method dispatch. When the
				// extractor stamped Properties["receiver_type"] on a CALLS
				// edge (a method calling another method on its own receiver),
				// probe the package-scoped member index FIRST so a bare-name
				// target like "handle" binds to the local "<pkg>/Mux.handle"
				// rather than colliding with same-named methods elsewhere.
				if recvType := r.Properties["receiver_type"]; recvType != "" && parentPkgDir != "" {
					// Issue #148 baseline: try the stamped type as-is.
					// Issue #364 follow-up: when the stamp is package-
					// qualified (e.g. `chi.Mux` from `chi.NewRouter()` /
					// `*chi.Mux` parameter), strip the package segment and
					// retry — entities are emitted under their bare receiver
					// name (`Mux.handle`), so the qualified form would never
					// match a same-package member. We try the as-is form
					// FIRST so an unambiguous user package named e.g.
					// `chi.Mux` (struct of type Mux in pkg chi, or any
					// package whose dir name happens to match) still wins.
					resolved := false
					tryTypes := []string{recvType}
					if dot := strings.LastIndexByte(recvType, '.'); dot >= 0 && dot < len(recvType)-1 {
						tryTypes = append(tryTypes, recvType[dot+1:])
					}
					for _, t := range tryTypes {
						if id, ok := idx.lookupPackageMember(parentPkgDir, t, r.ToID); ok {
							if id != "" {
								r.ToID = id
								applyEndpointStats(&stats, statusRewritten, false)
								d := idx.classifyDispositionLang(r.ToID, orig, lang, allow)
								stats.recordDisposition(d, orig)
								resolved = true
								break
							}
							// Ambiguous within (pkg, recv, member) — fall through
							// to record as unmatched (preserve the stub).
							break
						}
					}
					if resolved {
						continue
					}
				}
				// Refs #44 — Go cross-file same-package bare-component
				// fallback. The Go extractor emits DEPENDS_ON edges from
				// each method to its receiver type with ToID set to the
				// bare type name (e.g. "Server"). Sibling files in the
				// same package directory frequently host these structs;
				// the global byName lookup either misses (multi-file
				// package) or flags ambiguous (same struct name in
				// multiple packages — the dominant grpc-go-examples
				// residual). Probe byPackageComponent[parentPkgDir]
				// before falling through to rewriteOne, gated to Go to
				// avoid disturbing the resolution of other languages
				// whose bare-name conventions differ. We restrict to
				// edge kinds where a Component ToID is the natural
				// shape — DEPENDS_ON (method → receiver type, struct
				// field type) and EXTENDS / IMPLEMENTS (interface
				// embedding) — matching the hintKinds() bias.
				if lang == "go" && parentPkgDir != "" && isComponentTargetKind(r.Kind) {
					if id, ok := idx.lookupPackageComponent(parentPkgDir, r.ToID); ok {
						if id != "" {
							r.ToID = id
							applyEndpointStats(&stats, statusRewritten, false)
							d := idx.classifyDispositionLang(r.ToID, orig, lang, allow)
							stats.recordDisposition(d, orig)
							continue
						}
						// Ambiguous within (pkg, name) — fall through to
						// rewriteOne which will record as unmatched /
						// ambiguous against the global byName index.
					}
				}
				newID, st := idx.rewriteOneWithCaller(r.ToID, r.Kind, parentSourceFile, parentPkgDir)
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
