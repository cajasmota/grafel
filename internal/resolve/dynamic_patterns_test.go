package resolve

import "testing"

// TestDynamicPatterns_Catalog covers every pattern in the dynamic-dispatch
// catalog (refs #44). Each row asserts that a representative call-site stub
// produced by the per-language extractors is classified as
// DispositionDynamic by isDynamicPatternLang under its source language, and
// that obvious cross-language collisions (`res.send("hello")` in Node,
// `repo.Lookup(id)` in Go, `discount.apply(order)` in any language, etc.)
// are NOT classified as dynamic.
//
// New patterns MUST land here in the same commit so the catalog stays
// regression-tested. Negative rows guard against false positives — stubs
// that look reflection-adjacent but should still resolve normally.
func TestDynamicPatterns_Catalog(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		lang string
		stub string
		want bool
	}{
		// ---- Python -------------------------------------------------
		{"py_getattr_call", "python", `getattr(self, name)(arg)`, true},
		{"py_getattr_dunder_name", "python", `__getattr__`, true},
		{"py_getattr_method", "python", `obj.__getattr__("name")`, true},
		{"py_getattribute_method", "python", `self.__getattribute__("attr")`, true},
		{"py_setattr", "python", `setattr(obj, "x", 1)`, true},
		{"py_globals_subscript", "python", `globals()[name]()`, true},
		{"py_locals_subscript", "python", `locals()[name]()`, true},
		{"py_vars_subscript", "python", `vars()[name]()`, true},
		{"py_eval", "python", `eval(src)`, true},
		{"py_exec", "python", `exec(src)`, true},
		{"py_dunder_import", "python", `__import__("os")`, true},
		{"py_importlib", "python", `importlib.import_module("foo")`, true},
		{"py_functools_partial", "python", `functools.partial(fn, 1)`, true},
		{"py_functools_partialmethod", "python", `functools.partialmethod(fn)`, true},
		{"py_functools_reduce", "python", `functools.reduce(op, xs)`, true},
		{"py_methodcaller", "python", `operator.methodcaller("save")`, true},
		{"py_attrgetter", "python", `operator.attrgetter("x")`, true},
		{"py_itemgetter", "python", `operator.itemgetter(0)`, true},
		{"py_os_environ", "python", `os.environ["HOME"]`, true},
		{"py_os_getenv", "python", `os.getenv("HOME")`, true},
		{"py_dict_dispatch_str_key", "python", `handlers["save"]()`, true},
		{"py_dict_dispatch_var_key", "python", `handlers[key]()`, true},
		{"py_dotted_dispatch", "python", `self.handlers[name](x)`, true},

		// ---- Go -----------------------------------------------------
		{"go_reflect_call", "go", `reflect.Value.Call`, true},
		{"go_reflect_valueof", "go", `reflect.ValueOf(x)`, true},
		{"go_method_by_name", "go", `v.MethodByName("Foo").Call(args)`, true},
		{"go_field_by_name", "go", `v.FieldByName("X")`, true},
		{"go_plugin_open", "go", `plugin.Open("./mod.so")`, true},
		{"go_plugin_lookup", "go", `plugin.Lookup("Sym")`, true},

		// ---- TypeScript / JavaScript -------------------------------
		{"js_reflect_apply", "javascript", `Reflect.apply(fn, this, args)`, true},
		{"js_reflect_construct", "javascript", `Reflect.construct(C, args)`, true},
		{"js_function_ctor", "javascript", `Function("return 1")`, true},
		{"js_new_function", "javascript", `new Function("return 1")`, true},
		{"js_dynamic_import_var", "javascript", `import(modName)`, true},
		{"js_require_dynamic_var", "javascript", `require(modName)`, true},
		{"js_process_env", "javascript", `process.env.NODE_ENV`, true},
		{"ts_reflect_apply", "typescript", `Reflect.apply(fn, this, args)`, true},

		// ---- Ruby ---------------------------------------------------
		{"rb_send_method", "ruby", `obj.send(:name)`, true},
		{"rb_bare_send", "ruby", `send(:name)`, true},
		{"rb_public_send_method", "ruby", `obj.public_send(:name)`, true},
		{"rb_bare_public_send", "ruby", `public_send(:name)`, true},
		{"rb_dunder_send", "ruby", `obj.__send__(:name)`, true},
		{"rb_method_missing_name", "ruby", `method_missing`, true},
		{"rb_method_missing_call", "ruby", `obj.method_missing(:foo)`, true},
		{"rb_define_method", "ruby", `define_method(:foo)`, true},
		{"rb_define_method_method", "ruby", `klass.define_method(:foo)`, true},
		{"rb_instance_eval", "ruby", `obj.instance_eval(src)`, true},
		{"rb_class_eval", "ruby", `Klass.class_eval(src)`, true},

		// ---- Java / Kotlin / JVM -----------------------------------
		{"jvm_method_invoke", "java", `m.Method.invoke(target, args)`, true},
		{"jvm_method_invoke_qualified", "java", `Method.invoke(target, args)`, true},
		{"jvm_constructor_invoke", "java", `Constructor.invoke(args)`, true},
		{"jvm_class_forname", "java", `Class.forName("com.x.Y")`, true},
		{"jvm_new_instance", "java", `Class.forName(n).newInstance()`, true},
		{"jvm_class_class_newinstance", "kotlin", `MyType.class.newInstance()`, true},
		{"jvm_service_loader", "java", `ServiceLoader.load(MyService.class)`, true},
		{"jvm_system_getenv", "java", `System.getenv("HOME")`, true},

		// ---- Cross-language ----------------------------------------
		{"interpolated_template_js", "javascript", "`prefix-${name}-suffix`", true},
		{"interpolated_template_unknown", "", "`prefix-${name}-suffix`", true},

		// ---- Bare-identifier forms (issue #90) ---------------------
		// Per-language extractors emit only the leaf callee identifier
		// for call sites (ToID="getattr" for `getattr(...)`). The
		// catalog must recognize these bare-name shapes so dynamic
		// disposition is non-zero on real corpora.
		{"py_bare_getattr", "python", `getattr`, true},
		{"py_bare_setattr", "python", `setattr`, true},
		{"py_bare_eval", "python", `eval`, true},
		{"py_bare_exec", "python", `exec`, true},
		{"py_bare_dunder_import", "python", `__import__`, true},
		{"rb_bare_send_id", "ruby", `send`, true},
		{"rb_bare_public_send_id", "ruby", `public_send`, true},
		{"rb_bare_dunder_send_id", "ruby", `__send__`, true},
		{"rb_bare_define_method_id", "ruby", `define_method`, true},
		{"rb_bare_instance_eval_id", "ruby", `instance_eval`, true},
		{"rb_bare_class_eval_id", "ruby", `class_eval`, true},
		{"jvm_bare_forName", "java", `forName`, true},
		// Issue #72 — Java/Kotlin extractors emit only the leaf callee
		// identifier, so `m.invoke(target, args)` arrives as bare
		// `invoke`. The receiver-typed `Method.invoke(` regex never
		// fires on those stubs. The bare-name anchors below promote
		// the real-world reflection shape (`Method m = clazz.getMethod(name); m.invoke(...)`)
		// into Dynamic instead of bug-extractor.
		{"jvm_bare_invoke_java", "java", `invoke`, true},
		{"jvm_bare_invoke_kotlin", "kotlin", `invoke`, true},
		{"jvm_bare_newInstance", "java", `newInstance`, true},
		{"jvm_bare_getClass", "java", `getClass`, true},
		{"jvm_bare_getMethod", "java", `getMethod`, true},
		{"jvm_bare_getMethods", "java", `getMethods`, true},
		{"jvm_bare_getDeclaredMethod", "java", `getDeclaredMethod`, true},
		{"jvm_bare_getDeclaredMethods", "java", `getDeclaredMethods`, true},
		{"jvm_bare_getField", "java", `getField`, true},
		{"jvm_bare_getFields", "java", `getFields`, true},
		{"jvm_bare_getDeclaredField", "java", `getDeclaredField`, true},
		{"jvm_bare_getDeclaredFields", "java", `getDeclaredFields`, true},
		{"jvm_bare_getConstructor", "java", `getConstructor`, true},
		{"jvm_bare_getConstructors", "java", `getConstructors`, true},
		{"jvm_bare_getDeclaredConstructor", "java", `getDeclaredConstructor`, true},
		{"jvm_bare_getDeclaredConstructors", "java", `getDeclaredConstructors`, true},
		// Per-language gate: bare `invoke` from a JS file MUST NOT
		// classify as JVM dynamic — those names are JVM-only.
		// ---- click decorator + helper DSL (issue #423) -------------
		// Python click extractor strips the `click.` receiver so the
		// resolver sees bare leaf identifiers. Per-language gate keeps
		// these names from polluting other ecosystems.
		{"py_click_command", "python", `command`, true},
		{"py_click_group", "python", `group`, true},
		{"py_click_option", "python", `option`, true},
		{"py_click_argument", "python", `argument`, true},
		{"py_click_pass_context", "python", `pass_context`, true},
		{"py_click_pass_obj", "python", `pass_obj`, true},
		{"py_click_pass_meta_key", "python", `pass_meta_key`, true},
		{"py_click_echo", "python", `echo`, true},
		{"py_click_secho", "python", `secho`, true},
		{"py_click_prompt", "python", `prompt`, true},
		{"py_click_confirm", "python", `confirm`, true},
		{"py_click_progressbar", "python", `progressbar`, true},
		{"py_click_getchar", "python", `getchar`, true},
		{"py_click_pause", "python", `pause`, true},
		{"py_click_clear", "python", `clear`, true},
		{"py_click_style", "python", `style`, true},
		{"py_click_unstyle", "python", `unstyle`, true},
		{"py_click_format_filename", "python", `format_filename`, true},
		{"py_click_get_terminal_size", "python", `get_terminal_size`, true},
		{"py_click_launch", "python", `launch`, true},
		{"py_click_edit", "python", `edit`, true},
		{"py_click_get_app_dir", "python", `get_app_dir`, true},
		// Per-language gate: click DSL names from non-Python files MUST
		// NOT classify as Python dynamic — these names are Python/click
		// scoped here and would collide trivially with user methods in
		// other ecosystems (`group`, `option`, `echo`, `command`).
		{"click_command_js_negative", "javascript", `command`, false},
		{"click_option_ruby_negative", "ruby", `option`, false},
		{"click_echo_go_negative", "go", `echo`, false},
		{"click_group_java_negative", "java", `group`, false},

		{"jvm_bare_invoke_js_negative", "javascript", `invoke`, false},
		{"jvm_bare_getMethod_python_negative", "python", `getMethod`, false},
		{"jvm_bare_newInstance_go_negative", "go", `newInstance`, false},

		// ---- Rails ActionPack / ActionDispatch / ActiveSupport
		// internals (issue #448). Rails framework DSL exposed to
		// controllers/routes/initializers — method_missing-generated
		// or class-macro driven, so the Ruby extractor strips the
		// receiver and the resolver sees only the bare leaf. Per-
		// language gate (lang == "ruby") keeps generic verbs like
		// `get`/`post`/`mount`/`namespace`/`resources` from polluting
		// other ecosystems.
		// Routing DSL (ActionDispatch::Routing::Mapper).
		{"rb_rails_resources", "ruby", `resources`, true},
		{"rb_rails_resource", "ruby", `resource`, true},
		{"rb_rails_namespace", "ruby", `namespace`, true},
		{"rb_rails_constraints", "ruby", `constraints`, true},
		{"rb_rails_concern", "ruby", `concern`, true},
		{"rb_rails_concerns", "ruby", `concerns`, true},
		{"rb_rails_mount", "ruby", `mount`, true},
		{"rb_rails_get", "ruby", `get`, true},
		{"rb_rails_post", "ruby", `post`, true},
		{"rb_rails_put", "ruby", `put`, true},
		{"rb_rails_patch", "ruby", `patch`, true},
		{"rb_rails_delete", "ruby", `delete`, true},
		{"rb_rails_root", "ruby", `root`, true},
		{"rb_rails_direct", "ruby", `direct`, true},
		{"rb_rails_resolve", "ruby", `resolve`, true},
		{"rb_rails_controller", "ruby", `controller`, true},
		// ActionController DSL macros.
		{"rb_rails_helper", "ruby", `helper`, true},
		{"rb_rails_layout", "ruby", `layout`, true},
		{"rb_rails_protect_from_forgery", "ruby", `protect_from_forgery`, true},
		{"rb_rails_skip_authorization_check", "ruby", `skip_authorization_check`, true},
		{"rb_rails_verify_authenticity_token", "ruby", `verify_authenticity_token`, true},
		{"rb_rails_respond_with", "ruby", `respond_with`, true},
		{"rb_rails_headers", "ruby", `headers`, true},
		// ActiveSupport class-macros / callbacks.
		{"rb_rails_prepended", "ruby", `prepended`, true},
		{"rb_rails_class_attribute", "ruby", `class_attribute`, true},
		{"rb_rails_mattr_accessor", "ruby", `mattr_accessor`, true},
		{"rb_rails_mattr_reader", "ruby", `mattr_reader`, true},
		{"rb_rails_mattr_writer", "ruby", `mattr_writer`, true},
		{"rb_rails_cattr_accessor", "ruby", `cattr_accessor`, true},
		{"rb_rails_define_callbacks", "ruby", `define_callbacks`, true},
		{"rb_rails_set_callback", "ruby", `set_callback`, true},
		{"rb_rails_skip_callback", "ruby", `skip_callback`, true},
		// ActionDispatch middleware stack DSL.
		{"rb_rails_add_middleware", "ruby", `add_middleware`, true},
		{"rb_rails_delete_middleware", "ruby", `delete_middleware`, true},
		{"rb_rails_insert_before", "ruby", `insert_before`, true},
		{"rb_rails_insert_after", "ruby", `insert_after`, true},
		// Per-language gate: Rails internals from non-Ruby files MUST
		// NOT classify as Ruby dynamic — these names are Ruby/Rails
		// scoped here and would collide trivially with user methods
		// in other ecosystems (`get`/`post`/`mount`/`namespace`/
		// `resources`/`controller`/`headers`).
		{"rb_rails_get_js_negative", "javascript", `resources`, false},
		{"rb_rails_namespace_go_negative", "go", `namespace`, false},
		{"rb_rails_mount_python_negative", "python", `mount`, false},
		{"rb_rails_controller_java_negative", "java", `controller`, false},
		{"rb_rails_headers_kotlin_negative", "kotlin", `headers`, false},
		{"rb_rails_layout_swift_negative", "swift", `layout`, false},
		{"rb_rails_set_callback_rust_negative", "rust", `set_callback`, false},

		// ---- Negative cases (must NOT be dynamic) ------------------
		{"plain_kindname", "", `Function:Hello`, false},
		{"plain_bare_name", "", `Foo`, false},
		{"empty", "", ``, false},
		{"plain_call", "", `MyService.save()`, false},
		{"plain_attribute", "", `obj.attribute`, false},
		{"normal_function_call", "", `helper(x, y)`, false},
		{"structural_ref", "", `scope:operation:method:python:app/views.py:UserView#save`, false},
		{"ext_pkg", "", `ext:django`, false},

		// ---- Cross-language collisions (the 9 from the bug report) -
		// `res.send("hello")` in Node — must NOT match Ruby `.send`.
		{"neg_node_res_send", "javascript", `res.send("hello")`, false},
		// `discount.apply(order)` — domain method, not Function.prototype.apply.
		{"neg_discount_apply", "javascript", `discount.apply(order)`, false},
		// `controller.call(...)` — domain method, not Function.prototype.call.
		{"neg_controller_call", "javascript", `controller.call(req, res)`, false},
		// `repo.Lookup(id)` in Go — must NOT match `plugin.Lookup`.
		{"neg_go_repo_lookup", "go", `repo.Lookup(id)`, false},
		// `factory.newInstance()` — domain factory, not reflective.
		{"neg_factory_newinstance", "java", `factory.newInstance()`, false},
		// `cli.invoke(...)` — user-defined invoke, not Method.invoke.
		{"neg_cli_invoke", "java", `cli.invoke(cmd, args)`, false},
		// `db.bind(":id", 1)` — DB driver bind, not Function.prototype.bind.
		{"neg_db_bind", "javascript", `db.bind(":id", 1)`, false},
		// `require("fs")` — literal string, statically resolvable.
		{"neg_require_literal_dquote", "javascript", `require("fs")`, false},
		{"neg_require_literal_squote", "javascript", `require('fs')`, false},
		// `import("./literal-mod")` — literal string, statically resolvable.
		{"neg_import_literal", "javascript", `import("./literal-mod")`, false},
		// Extra: ensure receiver-anchored Lookup is required for Go even
		// when language is missing.
		{"neg_unknown_lang_repo_lookup", "", `repo.Lookup(id)`, false},
		// And that res.send under unknown language is NOT dynamic.
		{"neg_unknown_lang_res_send", "", `res.send("hello")`, false},

		// ---- Spring MVC ResponseEntity fluent builder methods (issue #44) ---
		// ResponseEntity.notFound().build() / .ok(body) / .noContent().build()
		// are the highest-count unresolved CALLS category in Kotlin/Java Spring
		// fixtures. The bare leaf names arrive because the Kotlin/Java extractors
		// strip the receiver; no static resolver can bind them without full type
		// inference. JVM-language gate keeps these from polluting non-JVM graphs.
		{"spring_kotlin_notFound", "kotlin", `notFound`, true},
		{"spring_kotlin_noContent", "kotlin", `noContent`, true},
		{"spring_kotlin_badRequest", "kotlin", `badRequest`, true},
		{"spring_kotlin_accepted", "kotlin", `accepted`, true},
		{"spring_kotlin_created", "kotlin", `created`, true},
		{"spring_kotlin_ok", "kotlin", `ok`, true},
		{"spring_kotlin_build", "kotlin", `build`, true},
		{"spring_kotlin_body", "kotlin", `body`, true},
		{"spring_kotlin_unprocessableEntity", "kotlin", `unprocessableEntity`, true},
		{"spring_kotlin_internalServerError", "kotlin", `internalServerError`, true},
		{"spring_java_notFound", "java", `notFound`, true},
		{"spring_java_build", "java", `build`, true},
		{"spring_java_ok", "java", `ok`, true},
		{"spring_scala_noContent", "scala", `noContent`, true},
		// Cross-language gate: Spring builder names MUST NOT fire for non-JVM.
		{"spring_python_build_neg", "python", `build`, false},
		{"spring_js_ok_neg", "javascript", `ok`, false},
		{"spring_go_body_neg", "go", `body`, false},
		{"spring_ruby_notFound_neg", "ruby", `notFound`, false},
		{"spring_ts_noContent_neg", "typescript", `noContent`, false},
		{"spring_rust_build_neg", "rust", `build`, false},

		// ---- Scala stdlib companion-object + collection methods (issue #44) ---
		// scala.concurrent.Future companion methods arrive as qualified stubs
		// "Future.successful", "Future.failed", etc. The Scala extractor emits
		// the PascalCase-qualified form when the receiver is a stdlib type;
		// the resolver cannot bind these (stdlib not indexed).
		// JVM-language gate keeps them from polluting Python / Go / JS / Ruby.
		{"scala_future_successful", "scala", `Future.successful`, true},
		{"scala_future_failed", "scala", `Future.failed`, true},
		{"scala_future_apply", "scala", `Future.apply`, true},
		{"scala_future_sequence", "scala", `Future.sequence`, true},
		// scala.util.Try / Success / Failure companion methods.
		{"scala_try_apply", "scala", `Try.apply`, true},
		{"scala_success_apply", "scala", `Success.apply`, true},
		{"scala_failure_apply", "scala", `Failure.apply`, true},
		// Scala collection qualified method stubs.
		{"scala_list_map", "scala", `List.map`, true},
		{"scala_list_flatmap", "scala", `List.flatMap`, true},
		{"scala_list_filter", "scala", `List.filter`, true},
		{"scala_list_filternot", "scala", `List.filterNot`, true},
		{"scala_list_find", "scala", `List.find`, true},
		{"scala_list_foldleft", "scala", `List.foldLeft`, true},
		{"scala_list_foreach", "scala", `List.foreach`, true},
		{"scala_list_empty", "scala", `List.empty`, true},
		{"scala_list_apply", "scala", `List.apply`, true},
		{"scala_map_get", "scala", `Map.get`, true},
		{"scala_map_contains", "scala", `Map.contains`, true},
		{"scala_map_empty", "scala", `Map.empty`, true},
		{"scala_map_map", "scala", `Map.map`, true},
		{"scala_map_filter", "scala", `Map.filter`, true},
		{"scala_seq_apply", "scala", `Seq.apply`, true},
		{"scala_seq_map", "scala", `Seq.map`, true},
		{"scala_vector_apply", "scala", `Vector.apply`, true},
		{"scala_vector_empty", "scala", `Vector.empty`, true},
		{"scala_set_apply", "scala", `Set.apply`, true},
		{"scala_option_apply", "scala", `Option.apply`, true},
		{"scala_some_apply", "scala", `Some.apply`, true},
		// These same qualified forms in Kotlin / Java also fire (JVM gate).
		{"scala_future_successful_kotlin", "kotlin", `Future.successful`, true},
		{"scala_future_successful_java", "java", `Future.successful`, true},
		{"scala_list_map_kotlin", "kotlin", `List.map`, true},
		// Cross-language gate: Scala stdlib qualified names MUST NOT fire for non-JVM.
		{"scala_future_successful_python_neg", "python", `Future.successful`, false},
		{"scala_future_successful_go_neg", "go", `Future.successful`, false},
		{"scala_future_successful_js_neg", "javascript", `Future.successful`, false},
		{"scala_future_successful_ruby_neg", "ruby", `Future.successful`, false},
		{"scala_future_successful_ts_neg", "typescript", `Future.successful`, false},
		{"scala_list_map_python_neg", "python", `List.map`, false},
		{"scala_list_map_go_neg", "go", `List.map`, false},
		{"scala_map_get_python_neg", "python", `Map.get`, false},
		{"scala_map_get_go_neg", "go", `Map.get`, false},

		// ---- Quartz.NET / Hangfire C# fluent builder + generic factory (issue #44) ---
		// Quartz.NET: JobBuilder.Create<T>() / TriggerBuilder.Create<T>() generic calls
		// land as dotted + generic stubs that the existing PascalCase-dotted pattern
		// cannot match (it rejects the `<` suffix). The new generic-factory pattern
		// promotes them to Dynamic instead of BugExtractor.
		{"quartz_jobbuilder_create_generic", "csharp", `JobBuilder.Create<ReportJob>`, true},
		{"quartz_jobbuilder_create_email", "csharp", `JobBuilder.Create<EmailJob>`, true},
		{"quartz_triggerbuilder_create_generic", "csharp", `TriggerBuilder.Create<DailyTrigger>`, true},
		// Hangfire: BackgroundJob.Enqueue<T>() / RecurringJob.AddOrUpdate<T>()
		{"hangfire_backgroundjob_enqueue_generic", "csharp", `BackgroundJob.Enqueue<IEmailService>`, true},
		{"hangfire_recurringjob_addorupdate_generic", "csharp", `RecurringJob.AddOrUpdate<IReportService>`, true},
		// Quartz.NET fluent builder bare-name leaf methods.
		{"quartz_withidentity", "csharp", `WithIdentity`, true},
		{"quartz_startnow", "csharp", `StartNow`, true},
		// Cross-language gate: Quartz.NET patterns MUST NOT fire for non-C# languages.
		{"quartz_withidentity_go_neg", "go", `WithIdentity`, false},
		{"quartz_startnow_python_neg", "python", `StartNow`, false},
		{"quartz_withidentity_java_neg", "java", `WithIdentity`, false},
		{"quartz_generic_factory_go_neg", "go", `JobBuilder.Create<ReportJob>`, false},
		{"quartz_generic_factory_python_neg", "python", `BackgroundJob.Enqueue<IEmailService>`, false},
		{"quartz_generic_factory_ts_neg", "typescript", `JobBuilder.Create<EmailJob>`, false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isDynamicPatternLang(tc.stub, tc.lang)
			if got != tc.want {
				t.Fatalf("isDynamicPatternLang(%q, lang=%q) = %v, want %v", tc.stub, tc.lang, got, tc.want)
			}
		})
	}
}

// TestInferLangFromStub_StructuralRef confirms that structural-ref stubs
// carry their language in segment 3 of `scope:<kind>:<subtype>:<lang>:...`,
// so isDynamicPattern (the no-lang wrapper) routes them to the right
// per-language catalog without the caller having to thread language down.
func TestInferLangFromStub_StructuralRef(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		stub string
		want string
	}{
		{"py_struct_ref", `scope:operation:method:python:app/views.py:UserView#save`, "python"},
		{"go_struct_ref", `scope:operation:method:go:internal/svc/handler.go:Handle`, "go"},
		{"js_struct_ref", `scope:operation:method:javascript:src/api.ts:request`, "javascript"},
		{"jvm_struct_ref", `scope:operation:method:java:src/Foo.java:bar`, "java"},
		{"non_struct", `Function:Hello`, ""},
		{"empty", ``, ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := inferLangFromStub(tc.stub); got != tc.want {
				t.Fatalf("inferLangFromStub(%q) = %q, want %q", tc.stub, got, tc.want)
			}
		})
	}
}
