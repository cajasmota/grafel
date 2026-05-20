package resolve

import "regexp"

// pythonDynamicPatterns is the per-language dynamic-dispatch pattern catalog
// for Python. Matches here tag a stub as DispositionDynamic.
//
// See the per-language catalog overview comment in refs.go for the design
// rationale behind the language-gated approach (Refs #44).
var pythonDynamicPatterns = []*regexp.Regexp{
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

func init() {
	dynamicPatternsByLang["python"] = pythonDynamicPatterns
}
