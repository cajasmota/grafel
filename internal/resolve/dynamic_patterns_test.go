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

		// ---- Rust stdlib / tokio dynamic-dispatch stubs (issue #44 slice-7) ---
		// Bare channel-constructor names emitted when the Rust extractor strips
		// the module path from a scoped call like `mpsc::channel::<String>(8)`.
		{"rust_channel_bare", "rust", `channel`, true},
		// Generic-receiver method stubs: the extractor emits `Type<T>.method`
		// when it resolves the receiver's concrete type from the function's
		// parameter list. No in-tree entity can satisfy `Receiver<String>.recv`
		// because the resolver's bare-name lookup discards the generic suffix.
		{"rust_receiver_string_recv", "rust", `Receiver<String>.recv`, true},
		{"rust_sender_string_send", "rust", `Sender<String>.send`, true},
		{"rust_receiver_u64_next", "rust", `Receiver<u64>.next`, true},
		{"rust_vec_u8_push", "rust", `Vec<u8>.push`, true},
		{"rust_option_string_unwrap", "rust", `Option<String>.unwrap`, true},
		{"rust_arc_mutex_lock", "rust", `Arc<Mutex<State>>.lock`, true},
		// Cross-language gate: Rust channel/generic-receiver stubs MUST NOT
		// fire for other languages (safer-bias rule #94).
		{"rust_channel_go_neg", "go", `channel`, false},
		{"rust_channel_python_neg", "python", `channel`, false},
		{"rust_channel_java_neg", "java", `channel`, false},
		{"rust_channel_kotlin_neg", "kotlin", `channel`, false},
		{"rust_receiver_recv_go_neg", "go", `Receiver<String>.recv`, false},
		{"rust_receiver_recv_python_neg", "python", `Receiver<String>.recv`, false},
		{"rust_receiver_recv_java_neg", "java", `Receiver<String>.recv`, false},
		// Additional negative: patterns that look similar but should NOT be dynamic
		// in Rust itself (lowercase receiver, no generics, etc.).
		{"rust_store_get_no_generic_neg", "rust", `Store.get`, false},
		{"rust_lowercase_recv_neg", "rust", `foo.recv`, false},

		// ---- Swift — Combine publisher operator leaf names (issue #44) ---
		// The Swift extractor emits bare CALLS edges for navigation-chain
		// method calls. When the receiver is an external Combine Publisher
		// or Foundation type, the leaf name is statically unresolvable.
		// Per-language gate (lang=="swift") prevents these from firing in
		// Go / Python / Ruby / etc. where same-named domain methods exist.
		{"swift_combine_sink", "swift", `sink`, true},
		{"swift_combine_store", "swift", `store`, true},
		{"swift_combine_cancel", "swift", `cancel`, true},
		{"swift_combine_eraseToAnyPublisher", "swift", `eraseToAnyPublisher`, true},
		{"swift_combine_receive", "swift", `receive`, true},
		{"swift_combine_subscribe", "swift", `subscribe`, true},
		{"swift_combine_mapError", "swift", `mapError`, true},
		{"swift_combine_flatMap", "swift", `flatMap`, true},
		{"swift_combine_compactMap", "swift", `compactMap`, true},
		{"swift_combine_tryMap", "swift", `tryMap`, true},
		{"swift_combine_decode", "swift", `decode`, true},
		{"swift_combine_removeDuplicates", "swift", `removeDuplicates`, true},
		{"swift_combine_debounce", "swift", `debounce`, true},
		{"swift_combine_throttle", "swift", `throttle`, true},
		{"swift_combine_timeout", "swift", `timeout`, true},
		{"swift_combine_retry", "swift", `retry`, true},
		{"swift_combine_assign", "swift", `assign`, true},
		{"swift_combine_share", "swift", `share`, true},
		{"swift_combine_combineLatest", "swift", `combineLatest`, true},
		{"swift_combine_zip", "swift", `zip`, true},
		{"swift_combine_send", "swift", `send`, true},
		// Swift Foundation/URLSession bare leaf names.
		{"swift_foundation_dataTaskPublisher", "swift", `dataTaskPublisher`, true},
		{"swift_foundation_dataTask", "swift", `dataTask`, true},
		{"swift_foundation_appendingPathComponent", "swift", `appendingPathComponent`, true},
		{"swift_foundation_appendingPathExtension", "swift", `appendingPathExtension`, true},
		{"swift_foundation_setValue", "swift", `setValue`, true},
		{"swift_foundation_addValue", "swift", `addValue`, true},
		// SwiftUI view modifier leaf names.
		{"swift_swiftui_padding", "swift", `padding`, true},
		{"swift_swiftui_frame", "swift", `frame`, true},
		{"swift_swiftui_background", "swift", `background`, true},
		{"swift_swiftui_foregroundColor", "swift", `foregroundColor`, true},
		{"swift_swiftui_foregroundStyle", "swift", `foregroundStyle`, true},
		{"swift_swiftui_font", "swift", `font`, true},
		{"swift_swiftui_cornerRadius", "swift", `cornerRadius`, true},
		{"swift_swiftui_overlay", "swift", `overlay`, true},
		{"swift_swiftui_shadow", "swift", `shadow`, true},
		{"swift_swiftui_opacity", "swift", `opacity`, true},
		{"swift_swiftui_scaleEffect", "swift", `scaleEffect`, true},
		{"swift_swiftui_rotationEffect", "swift", `rotationEffect`, true},
		{"swift_swiftui_onAppear", "swift", `onAppear`, true},
		{"swift_swiftui_onDisappear", "swift", `onDisappear`, true},
		{"swift_swiftui_onTapGesture", "swift", `onTapGesture`, true},
		{"swift_swiftui_onChange", "swift", `onChange`, true},
		{"swift_swiftui_sheet", "swift", `sheet`, true},
		{"swift_swiftui_alert", "swift", `alert`, true},
		{"swift_swiftui_navigationTitle", "swift", `navigationTitle`, true},
		{"swift_swiftui_toolbar", "swift", `toolbar`, true},
		{"swift_swiftui_listStyle", "swift", `listStyle`, true},
		{"swift_swiftui_searchable", "swift", `searchable`, true},
		{"swift_swiftui_disabled", "swift", `disabled`, true},
		{"swift_swiftui_hidden", "swift", `hidden`, true},
		{"swift_swiftui_environmentObject", "swift", `environmentObject`, true},
		{"swift_swiftui_environment", "swift", `environment`, true},
		{"swift_swiftui_task", "swift", `task`, true},
		{"swift_swiftui_refreshable", "swift", `refreshable`, true},
		{"swift_swiftui_swipeActions", "swift", `swipeActions`, true},
		{"swift_swiftui_contextMenu", "swift", `contextMenu`, true},
		{"swift_swiftui_ignoresSafeArea", "swift", `ignoresSafeArea`, true},
		{"swift_swiftui_clipShape", "swift", `clipShape`, true},
		{"swift_swiftui_resizable", "swift", `resizable`, true},
		{"swift_swiftui_navigationDestination", "swift", `navigationDestination`, true},
		// UIKit bare leaf names.
		{"swift_uikit_addTarget", "swift", `addTarget`, true},
		{"swift_uikit_addSubview", "swift", `addSubview`, true},
		{"swift_uikit_removeFromSuperview", "swift", `removeFromSuperview`, true},
		{"swift_uikit_present", "swift", `present`, true},
		{"swift_uikit_dismiss", "swift", `dismiss`, true},
		{"swift_uikit_reloadData", "swift", `reloadData`, true},
		{"swift_uikit_dequeueReusableCell", "swift", `dequeueReusableCell`, true},
		{"swift_uikit_becomeFirstResponder", "swift", `becomeFirstResponder`, true},
		{"swift_uikit_resignFirstResponder", "swift", `resignFirstResponder`, true},
		{"swift_uikit_pushViewController", "swift", `pushViewController`, true},
		// Cross-language gate: Swift framework names MUST NOT fire for
		// non-Swift languages. Generic names like `sink`, `store`, `frame`,
		// `padding`, `font`, `send`, `zip` are common in other ecosystems.
		{"swift_sink_go_neg", "go", `sink`, false},
		{"swift_sink_python_neg", "python", `sink`, false},
		{"swift_sink_ruby_neg", "ruby", `sink`, false},
		{"swift_sink_js_neg", "javascript", `sink`, false},
		{"swift_store_go_neg", "go", `store`, false},
		{"swift_store_python_neg", "python", `store`, false},
		{"swift_store_java_neg", "java", `store`, false},
		{"swift_frame_python_neg", "python", `frame`, false},
		{"swift_frame_ruby_neg", "ruby", `frame`, false},
		{"swift_padding_go_neg", "go", `padding`, false},
		{"swift_padding_js_neg", "javascript", `padding`, false},
		{"swift_font_python_neg", "python", `font`, false},
		{"swift_send_java_neg", "java", `send`, false},
		{"swift_send_go_neg", "go", `send`, false},
		{"swift_zip_python_neg", "python", `zip`, false},
		{"swift_zip_ruby_neg", "ruby", `zip`, false},
		{"swift_present_python_neg", "python", `present`, false},
		{"swift_dismiss_js_neg", "javascript", `dismiss`, false},
		{"swift_alert_go_neg", "go", `alert`, false},
		{"swift_assign_python_neg", "python", `assign`, false},
		{"swift_assign_js_neg", "javascript", `assign`, false},
		{"swift_environment_python_neg", "python", `environment`, false},
		{"swift_environment_ruby_neg", "ruby", `environment`, false},

		// ---- Lua stdlib / global built-in dynamic stubs (issue #44 Lua slice) ---
		//
		// The Lua extractor's luaCallTarget returns only the trailing identifier
		// of a dotted call: `table.insert(t, v)` → "insert", `math.floor(n)` →
		// "floor", etc.  Lua global built-ins arrive as bare names too (ipairs,
		// pcall, setmetatable, …).  None of these names has a matching entity in
		// the graph, so without these patterns they all land in BugExtractor.
		//
		// Tier-1: Lua-unique global identifiers — virtually never user-defined.
		{"lua_ipairs", "lua", `ipairs`, true},
		{"lua_pairs", "lua", `pairs`, true},
		{"lua_pcall", "lua", `pcall`, true},
		{"lua_xpcall", "lua", `xpcall`, true},
		{"lua_rawget", "lua", `rawget`, true},
		{"lua_rawset", "lua", `rawset`, true},
		{"lua_rawequal", "lua", `rawequal`, true},
		{"lua_rawlen", "lua", `rawlen`, true},
		{"lua_setmetatable", "lua", `setmetatable`, true},
		{"lua_getmetatable", "lua", `getmetatable`, true},
		{"lua_tostring", "lua", `tostring`, true},
		{"lua_tonumber", "lua", `tonumber`, true},
		{"lua_unpack", "lua", `unpack`, true},
		{"lua_select", "lua", `select`, true},
		{"lua_next", "lua", `next`, true},
		{"lua_collectgarbage", "lua", `collectgarbage`, true},
		{"lua_dofile", "lua", `dofile`, true},
		{"lua_loadfile", "lua", `loadfile`, true},
		{"lua_loadstring", "lua", `loadstring`, true},
		// Tier-2: table.* leaf names.
		{"lua_table_insert", "lua", `insert`, true},
		{"lua_table_remove", "lua", `remove`, true},
		{"lua_table_sort", "lua", `sort`, true},
		{"lua_table_concat", "lua", `concat`, true},
		{"lua_table_move", "lua", `move`, true},
		// Tier-2: string.* leaf names.
		{"lua_string_gmatch", "lua", `gmatch`, true},
		{"lua_string_gsub", "lua", `gsub`, true},
		{"lua_string_byte", "lua", `byte`, true},
		{"lua_string_char", "lua", `char`, true},
		{"lua_string_rep", "lua", `rep`, true},
		{"lua_string_dump", "lua", `dump`, true},
		// Tier-2: math.* leaf names.
		{"lua_math_floor", "lua", `floor`, true},
		{"lua_math_ceil", "lua", `ceil`, true},
		{"lua_math_sqrt", "lua", `sqrt`, true},
		{"lua_math_fmod", "lua", `fmod`, true},
		{"lua_math_modf", "lua", `modf`, true},
		{"lua_math_random", "lua", `random`, true},
		{"lua_math_randomseed", "lua", `randomseed`, true},
		{"lua_math_tointeger", "lua", `tointeger`, true},
		// Tier-2: os.* leaf names.
		{"lua_os_tmpname", "lua", `tmpname`, true},
		{"lua_os_difftime", "lua", `difftime`, true},
		// Tier-2: coroutine.* leaf names.
		{"lua_coroutine_resume", "lua", `resume`, true},
		{"lua_coroutine_yield", "lua", `yield`, true},
		{"lua_coroutine_isyieldable", "lua", `isyieldable`, true},
		{"lua_coroutine_running", "lua", `running`, true},
		// Cross-language gate: Lua built-in bare names MUST NOT fire for
		// other languages (safer-bias rule #94).
		{"lua_ipairs_python_neg", "python", `ipairs`, false},
		{"lua_pairs_go_neg", "go", `pairs`, false},
		{"lua_tostring_java_neg", "java", `tostring`, false},
		{"lua_pcall_ruby_neg", "ruby", `pcall`, false},
		{"lua_insert_python_neg", "python", `insert`, false},
		{"lua_insert_go_neg", "go", `insert`, false},
		{"lua_insert_java_neg", "java", `insert`, false},
		{"lua_remove_js_neg", "javascript", `remove`, false},
		{"lua_sort_python_neg", "python", `sort`, false},
		{"lua_sort_go_neg", "go", `sort`, false},
		{"lua_concat_js_neg", "javascript", `concat`, false},
		{"lua_floor_python_neg", "python", `floor`, false},
		{"lua_floor_js_neg", "javascript", `floor`, false},
		{"lua_random_python_neg", "python", `random`, false},
		{"lua_resume_go_neg", "go", `resume`, false},
		{"lua_resume_java_neg", "java", `resume`, false},
		{"lua_yield_python_neg", "python", `yield`, false},
		{"lua_setmetatable_go_neg", "go", `setmetatable`, false},

		// ---- Dart / Flutter dynamic patterns (issue #44 slice 9) -------
		// 1. Package-URI import ToIDs — unresolvable without pub-resolution.
		{"dart_pkg_flutter_material", "dart", `package:flutter/material.dart`, true},
		{"dart_pkg_provider", "dart", `package:provider/provider.dart`, true},
		{"dart_pkg_http", "dart", `package:http/http.dart`, true},
		{"dart_pkg_riverpod", "dart", `package:flutter_riverpod/flutter_riverpod.dart`, true},
		{"dart_dart_convert", "dart", `dart:convert`, true},
		{"dart_dart_async", "dart", `dart:async`, true},
		{"dart_dart_core", "dart", `dart:core`, true},
		{"dart_relative_import_dotslash", "dart", `./widgets/product_card.dart`, true},
		{"dart_relative_import_dotdot", "dart", `../providers/cart_provider.dart`, true},
		{"dart_lib_prefix", "dart", `lib/src/screens/home_screen.dart`, true},
		{"dart_src_prefix", "dart", `src/screens/login_screen.dart`, true},
		// 2. Flutter/Material widget constructors.
		{"dart_widget_Text", "dart", `Text`, true},
		{"dart_widget_Column", "dart", `Column`, true},
		{"dart_widget_Row", "dart", `Row`, true},
		{"dart_widget_Scaffold", "dart", `Scaffold`, true},
		{"dart_widget_AppBar", "dart", `AppBar`, true},
		{"dart_widget_Container", "dart", `Container`, true},
		{"dart_widget_SizedBox", "dart", `SizedBox`, true},
		{"dart_widget_Padding", "dart", `Padding`, true},
		{"dart_widget_Expanded", "dart", `Expanded`, true},
		{"dart_widget_Center", "dart", `Center`, true},
		{"dart_widget_Stack", "dart", `Stack`, true},
		{"dart_widget_Positioned", "dart", `Positioned`, true},
		{"dart_widget_ListView", "dart", `ListView`, true},
		{"dart_widget_GridView", "dart", `GridView`, true},
		{"dart_widget_MaterialApp", "dart", `MaterialApp`, true},
		{"dart_widget_SnackBar", "dart", `SnackBar`, true},
		{"dart_widget_IconButton", "dart", `IconButton`, true},
		{"dart_widget_ElevatedButton", "dart", `ElevatedButton`, true},
		{"dart_widget_TextFormField", "dart", `TextFormField`, true},
		{"dart_widget_CircularProgressIndicator", "dart", `CircularProgressIndicator`, true},
		{"dart_widget_BoxDecoration", "dart", `BoxDecoration`, true},
		{"dart_widget_InputDecoration", "dart", `InputDecoration`, true},
		{"dart_widget_TextStyle", "dart", `TextStyle`, true},
		{"dart_widget_ThemeData", "dart", `ThemeData`, true},
		{"dart_widget_SafeArea", "dart", `SafeArea`, true},
		{"dart_widget_RefreshIndicator", "dart", `RefreshIndicator`, true},
		{"dart_widget_Card", "dart", `Card`, true},
		{"dart_widget_Icon", "dart", `Icon`, true},
		{"dart_widget_MultiProvider", "dart", `MultiProvider`, true},
		{"dart_widget_ChangeNotifierProvider", "dart", `ChangeNotifierProvider`, true},
		{"dart_widget_SliverGridDelegate", "dart", `SliverGridDelegateWithFixedCrossAxisCount`, true},
		{"dart_widget_SliverGridDelegateMax", "dart", `SliverGridDelegateWithMaxCrossAxisCount`, true},
		{"dart_widget_FutureBuilder", "dart", `FutureBuilder`, true},
		{"dart_widget_StreamBuilder", "dart", `StreamBuilder`, true},
		{"dart_widget_AlertDialog", "dart", `AlertDialog`, true},
		{"dart_widget_GestureDetector", "dart", `GestureDetector`, true},
		{"dart_widget_InkWell", "dart", `InkWell`, true},
		// 3. ChangeNotifier / StatefulWidget lifecycle.
		{"dart_lifecycle_notifyListeners", "dart", `notifyListeners`, true},
		{"dart_lifecycle_setState", "dart", `setState`, true},
		{"dart_lifecycle_addListener", "dart", `addListener`, true},
		{"dart_lifecycle_removeListener", "dart", `removeListener`, true},
		{"dart_lifecycle_initState", "dart", `initState`, true},
		{"dart_lifecycle_didUpdateWidget", "dart", `didUpdateWidget`, true},
		{"dart_lifecycle_didChangeDependencies", "dart", `didChangeDependencies`, true},
		// 4. Dart async/Future/Stream chain methods.
		{"dart_async_catchError", "dart", `catchError`, true},
		{"dart_async_whenComplete", "dart", `whenComplete`, true},
		{"dart_async_listen", "dart", `listen`, true},
		// 5. Flutter static factories and lifecycle helpers.
		{"dart_nav_pushNamed", "dart", `pushNamed`, true},
		{"dart_nav_pushReplacementNamed", "dart", `pushReplacementNamed`, true},
		{"dart_nav_pop", "dart", `pop`, true},
		{"dart_nav_push", "dart", `push`, true},
		{"dart_static_of", "dart", `of`, true},
		{"dart_static_maybeOf", "dart", `maybeOf`, true},
		{"dart_overlay_showSnackBar", "dart", `showSnackBar`, true},
		{"dart_overlay_showDialog", "dart", `showDialog`, true},
		{"dart_overlay_showModalBottomSheet", "dart", `showModalBottomSheet`, true},
		// 6. Dart core type constructors and methods.
		{"dart_core_Exception", "dart", `Exception`, true},
		{"dart_core_FormatException", "dart", `FormatException`, true},
		{"dart_core_toString", "dart", `toString`, true},
		{"dart_core_parse", "dart", `parse`, true},
		{"dart_core_tryParse", "dart", `tryParse`, true},
		{"dart_core_decode", "dart", `decode`, true},
		{"dart_core_encode", "dart", `encode`, true},
		{"dart_core_now", "dart", `now`, true},
		{"dart_core_fromSeed", "dart", `fromSeed`, true},
		{"dart_core_circular", "dart", `circular`, true},
		{"dart_core_network", "dart", `network`, true},
		{"dart_core_runApp", "dart", `runApp`, true},
		{"dart_core_dispose", "dart", `dispose`, true},
		// 7. dart:core collection methods.
		{"dart_map_containsKey", "dart", `containsKey`, true},
		{"dart_map_putIfAbsent", "dart", `putIfAbsent`, true},
		{"dart_map_remove", "dart", `remove`, true},
		{"dart_map_update", "dart", `update`, true},
		{"dart_list_toList", "dart", `toList`, true},
		{"dart_list_where", "dart", `where`, true},
		{"dart_list_firstWhere", "dart", `firstWhere`, true},
		{"dart_list_map", "dart", `map`, true},
		// 8. dart:core String methods.
		{"dart_str_trim", "dart", `trim`, true},
		{"dart_str_toStringAsFixed", "dart", `toStringAsFixed`, true},
		// Cross-language gate: Dart patterns MUST NOT fire for other languages.
		{"dart_of_python_neg", "python", `of`, false},
		{"dart_of_go_neg", "go", `of`, false},
		{"dart_of_ruby_neg", "ruby", `of`, false},
		{"dart_of_java_neg", "java", `of`, false},
		{"dart_notifyListeners_python_neg", "python", `notifyListeners`, false},
		{"dart_notifyListeners_go_neg", "go", `notifyListeners`, false},
		{"dart_notifyListeners_java_neg", "java", `notifyListeners`, false},
		{"dart_setState_python_neg", "python", `setState`, false},
		// Note: `setState` for JavaScript is already dynamic via the `^set[A-Z]...` React
		// useState-setter pattern (jsDynamicPatterns wave-7), so we do NOT test it as a
		// Dart-only negative for JS. Use a language that has no matching JS pattern instead.
		{"dart_setState_ruby_neg", "ruby", `setState`, false},
		{"dart_catchError_python_neg", "python", `catchError`, false},
		{"dart_catchError_ruby_neg", "ruby", `catchError`, false},
		{"dart_parse_python_neg", "python", `parse`, false},
		{"dart_parse_go_neg", "go", `parse`, false},
		{"dart_parse_java_neg", "java", `parse`, false},
		{"dart_toString_go_neg", "go", `toString`, false},
		{"dart_toString_python_neg", "python", `toString`, false},
		{"dart_toString_java_neg", "java", `toString`, false},
		{"dart_containsKey_python_neg", "python", `containsKey`, false},
		{"dart_containsKey_go_neg", "go", `containsKey`, false},
		{"dart_containsKey_java_neg", "java", `containsKey`, false},
		{"dart_remove_python_neg", "python", `remove`, false},
		{"dart_remove_go_neg", "go", `remove`, false},
		{"dart_remove_ruby_neg", "ruby", `remove`, false},
		{"dart_trim_python_neg", "python", `trim`, false},
		{"dart_trim_go_neg", "go", `trim`, false},
		{"dart_dispose_python_neg", "python", `dispose`, false},
		{"dart_dispose_go_neg", "go", `dispose`, false},
		{"dart_dispose_ruby_neg", "ruby", `dispose`, false},
		// Note: `Text` and `Column` are also in pythonDynamicPatterns (Flask-SQLAlchemy /
		// Marshmallow), so those names correctly fire as Dynamic for Python.
		// Use widget names that are unique to the Dart catalog for the cross-language gate.
		{"dart_Scaffold_python_neg", "python", `Scaffold`, false},
		{"dart_Scaffold_go_neg", "go", `Scaffold`, false},
		{"dart_Scaffold_java_neg", "java", `Scaffold`, false},
		{"dart_setState_kotlin_neg", "kotlin", `setState`, false},
		{"dart_pushNamed_python_neg", "python", `pushNamed`, false},
		{"dart_pushNamed_go_neg", "go", `pushNamed`, false},
		{"dart_pushNamed_java_neg", "java", `pushNamed`, false},

		// ---- Elixir — Ecto.Repo query + mutation methods (issue #44 slice-9) ---
		// Ecto.Repo calls arrive as bare leaf names after receiver stripping.
		// Ecto is an external Hex dependency never indexed in-tree.
		{"elixir_ecto_all", "elixir", `all`, true},
		{"elixir_ecto_one", "elixir", `one`, true},
		{"elixir_ecto_get", "elixir", `get`, true},
		{"elixir_ecto_get_bang", "elixir", `get!`, true},
		{"elixir_ecto_get_by", "elixir", `get_by`, true},
		{"elixir_ecto_get_by_bang", "elixir", `get_by!`, true},
		{"elixir_ecto_preload", "elixir", `preload`, true},
		{"elixir_ecto_insert", "elixir", `insert`, true},
		{"elixir_ecto_insert_bang", "elixir", `insert!`, true},
		{"elixir_ecto_update", "elixir", `update`, true},
		{"elixir_ecto_update_bang", "elixir", `update!`, true},
		{"elixir_ecto_delete", "elixir", `delete`, true},
		{"elixir_ecto_delete_bang", "elixir", `delete!`, true},
		{"elixir_ecto_transaction", "elixir", `transaction`, true},
		{"elixir_ecto_insert_all", "elixir", `insert_all`, true},
		{"elixir_ecto_update_all", "elixir", `update_all`, true},
		{"elixir_ecto_delete_all", "elixir", `delete_all`, true},

		// ---- Elixir — Phoenix.Conn pipeline helpers (issue #44 slice-9) ---
		// Phoenix controller helpers are macro-injected by `use Phoenix.Controller`.
		{"elixir_phoenix_render", "elixir", `render`, true},
		{"elixir_phoenix_json", "elixir", `json`, true},
		{"elixir_phoenix_text", "elixir", `text`, true},
		{"elixir_phoenix_html", "elixir", `html`, true},
		{"elixir_phoenix_send_resp", "elixir", `send_resp`, true},
		{"elixir_phoenix_put_flash", "elixir", `put_flash`, true},
		{"elixir_phoenix_redirect", "elixir", `redirect`, true},
		{"elixir_phoenix_halt", "elixir", `halt`, true},
		{"elixir_phoenix_assign", "elixir", `assign`, true},
		{"elixir_phoenix_put_session", "elixir", `put_session`, true},
		{"elixir_phoenix_get_session", "elixir", `get_session`, true},
		{"elixir_phoenix_put_resp_content_type", "elixir", `put_resp_content_type`, true},
		{"elixir_phoenix_put_resp_header", "elixir", `put_resp_header`, true},
		{"elixir_phoenix_fetch_session", "elixir", `fetch_session`, true},

		// ---- Elixir — GenServer / OTP behaviour callbacks (issue #44 slice-9) ---
		// OTP callbacks are invoked by the runtime, not by static call sites.
		{"elixir_otp_init", "elixir", `init`, true},
		{"elixir_otp_handle_call", "elixir", `handle_call`, true},
		{"elixir_otp_handle_cast", "elixir", `handle_cast`, true},
		{"elixir_otp_handle_info", "elixir", `handle_info`, true},
		{"elixir_otp_handle_continue", "elixir", `handle_continue`, true},
		{"elixir_otp_terminate", "elixir", `terminate`, true},
		{"elixir_otp_code_change", "elixir", `code_change`, true},
		{"elixir_otp_start_link", "elixir", `start_link`, true},
		{"elixir_otp_child_spec", "elixir", `child_spec`, true},

		// ---- Elixir — Ecto.Changeset builder methods (issue #44 slice-9) ---
		// Changeset pipeline functions are imported by `use Ecto.Schema` / `import Ecto.Changeset`.
		{"elixir_cs_cast", "elixir", `cast`, true},
		{"elixir_cs_cast_assoc", "elixir", `cast_assoc`, true},
		{"elixir_cs_validate_required", "elixir", `validate_required`, true},
		{"elixir_cs_validate_length", "elixir", `validate_length`, true},
		{"elixir_cs_validate_format", "elixir", `validate_format`, true},
		{"elixir_cs_validate_inclusion", "elixir", `validate_inclusion`, true},
		{"elixir_cs_put_assoc", "elixir", `put_assoc`, true},
		{"elixir_cs_put_change", "elixir", `put_change`, true},
		{"elixir_cs_change", "elixir", `change`, true},
		{"elixir_cs_changeset", "elixir", `changeset`, true},
		{"elixir_cs_add_error", "elixir", `add_error`, true},
		{"elixir_cs_apply_action", "elixir", `apply_action`, true},
		{"elixir_cs_get_field", "elixir", `get_field`, true},
		{"elixir_cs_get_change", "elixir", `get_change`, true},

		// ---- Elixir — Ecto.Query DSL (issue #44 slice-9) ---
		// `from`, `where`, `select`, `order_by` etc. are injected by `import Ecto.Query`.
		{"elixir_query_from", "elixir", `from`, true},
		{"elixir_query_where", "elixir", `where`, true},
		{"elixir_query_select", "elixir", `select`, true},
		{"elixir_query_order_by", "elixir", `order_by`, true},
		{"elixir_query_group_by", "elixir", `group_by`, true},
		{"elixir_query_join", "elixir", `join`, true},
		{"elixir_query_limit", "elixir", `limit`, true},
		{"elixir_query_offset", "elixir", `offset`, true},
		{"elixir_query_distinct", "elixir", `distinct`, true},
		{"elixir_query_fragment", "elixir", `fragment`, true},

		// ---- Elixir — Logger calls (issue #44 slice-9) ---
		{"elixir_logger_debug", "elixir", `debug`, true},
		{"elixir_logger_info", "elixir", `info`, true},
		{"elixir_logger_warning", "elixir", `warning`, true},
		{"elixir_logger_error", "elixir", `error`, true},

		// ---- Cross-language gate: Elixir patterns MUST NOT fire for other langs ---
		// Names like `all`, `get`, `render`, `insert`, `cast`, `init`, `change`,
		// `from`, `where`, `join`, `limit` are common domain method names in
		// other ecosystems and must not be classified Dynamic outside Elixir.
		{"elixir_all_go_neg", "go", `all`, false},
		{"elixir_all_python_neg", "python", `all`, false},
		{"elixir_all_ruby_neg", "ruby", `all`, false},
		{"elixir_all_js_neg", "javascript", `all`, false},
		{"elixir_get_go_neg", "go", `get`, false},
		{"elixir_get_python_neg", "python", `get`, false},
		{"elixir_get_java_neg", "java", `get`, false},
		{"elixir_insert_go_neg", "go", `insert`, false},
		{"elixir_insert_python_neg", "python", `insert`, false},
		{"elixir_insert_ruby_neg", "ruby", `insert`, false},
		{"elixir_render_go_neg", "go", `render`, false},
		{"elixir_render_python_neg", "python", `render`, false},
		{"elixir_render_ts_neg", "typescript", `render`, false},
		{"elixir_cast_go_neg", "go", `cast`, false},
		{"elixir_cast_python_neg", "python", `cast`, false},
		{"elixir_cast_java_neg", "java", `cast`, false},
		{"elixir_init_go_neg", "go", `init`, false},
		{"elixir_init_python_neg", "python", `init`, false},
		{"elixir_init_java_neg", "java", `init`, false},
		{"elixir_change_go_neg", "go", `change`, false},
		{"elixir_change_python_neg", "python", `change`, false},
		{"elixir_from_go_neg", "go", `from`, false},
		{"elixir_from_python_neg", "python", `from`, false},
		{"elixir_where_go_neg", "go", `where`, false},
		{"elixir_where_ts_neg", "typescript", `where`, false},
		{"elixir_join_go_neg", "go", `join`, false},
		{"elixir_join_python_neg", "python", `join`, false},
		{"elixir_limit_go_neg", "go", `limit`, false},
		{"elixir_limit_python_neg", "python", `limit`, false},
		{"elixir_json_go_neg", "go", `json`, false},
		{"elixir_json_python_neg", "python", `json`, false},
		{"elixir_debug_go_neg", "go", `debug`, false},
		{"elixir_info_go_neg", "go", `info`, false},
		{"elixir_error_go_neg", "go", `error`, false},
		{"elixir_update_go_neg", "go", `update`, false},
		{"elixir_update_python_neg", "python", `update`, false},
		{"elixir_delete_go_neg", "go", `delete`, false},
		// NOTE: `delete` is also in Python/SQLAlchemy patterns, so we skip the python gate test here.
		{"elixir_halt_go_neg", "go", `halt`, false},
		{"elixir_redirect_go_neg", "go", `redirect`, false},
		{"elixir_transaction_go_neg", "go", `transaction`, false},
		// Zig dynamic patterns (issue #44 — Zig resolver slice).
		// All Zig std lib namespaces (std.mem, std.fmt, std.ascii, std.json,
		// std.meta, std.ArrayList) have names unavailable as in-tree entities.
		//
		// Tier 1: Zig-unique std.mem split/tokenize family.
		{"zig_splitScalar", "zig", `splitScalar`, true},
		{"zig_splitSequence", "zig", `splitSequence`, true},
		{"zig_splitBackwardsScalar", "zig", `splitBackwardsScalar`, true},
		{"zig_tokenizeScalar", "zig", `tokenizeScalar`, true},
		{"zig_tokenizeSequence", "zig", `tokenizeSequence`, true},
		{"zig_tokenizeAny", "zig", `tokenizeAny`, true},
		// Tier 1: Zig-unique std.mem search functions.
		{"zig_indexOfScalar", "zig", `indexOfScalar`, true},
		{"zig_lastIndexOfScalar", "zig", `lastIndexOfScalar`, true},
		{"zig_indexOfPosLinear", "zig", `indexOfPosLinear`, true},
		{"zig_containsAtLeast", "zig", `containsAtLeast`, true},
		// Tier 1: Zig-unique std.ascii functions.
		{"zig_eqlIgnoreCase", "zig", `eqlIgnoreCase`, true},
		{"zig_isPrint", "zig", `isPrint`, true},
		{"zig_isAlpha", "zig", `isAlpha`, true},
		{"zig_isDigit", "zig", `isDigit`, true},
		{"zig_toUpper", "zig", `toUpper`, true},
		{"zig_toLower", "zig", `toLower`, true},
		{"zig_lowerString", "zig", `lowerString`, true},
		// Tier 1: Zig-unique std.meta / std.fmt / std.json.
		{"zig_stringToEnum", "zig", `stringToEnum`, true},
		{"zig_toOwnedSlice", "zig", `toOwnedSlice`, true},
		{"zig_parseFromSlice", "zig", `parseFromSlice`, true},
		{"zig_allocPrint", "zig", `allocPrint`, true},
		{"zig_bufPrint", "zig", `bufPrint`, true},
		{"zig_parseInt", "zig", `parseInt`, true},
		// Tier 2: Zig std lib leaf names gated to lang=="zig".
		{"zig_stringify", "zig", `stringify`, true},
		{"zig_writeAll", "zig", `writeAll`, true},
		{"zig_startsWith", "zig", `startsWith`, true},
		{"zig_endsWith", "zig", `endsWith`, true},
		{"zig_trim", "zig", `trim`, true},
		{"zig_trimLeft", "zig", `trimLeft`, true},
		{"zig_trimRight", "zig", `trimRight`, true},
		{"zig_eql", "zig", `eql`, true},
		{"zig_writeVecAll", "zig", `writeVecAll`, true},
		{"zig_print", "zig", `print`, true},
		{"zig_assert", "zig", `assert`, true},
		{"zig_clamp", "zig", `clamp`, true},
		{"zig_parseInt_zig_only", "zig", `parseFloat`, true},
		// Cross-language gate: Zig patterns MUST NOT fire for other languages.
		// splitScalar/tokenizeScalar are entirely Zig-specific names.
		{"zig_splitScalar_go_neg", "go", `splitScalar`, false},
		{"zig_splitScalar_python_neg", "python", `splitScalar`, false},
		{"zig_splitSequence_js_neg", "javascript", `splitSequence`, false},
		{"zig_tokenizeScalar_ruby_neg", "ruby", `tokenizeScalar`, false},
		{"zig_indexOfScalar_go_neg", "go", `indexOfScalar`, false},
		{"zig_indexOfScalar_python_neg", "python", `indexOfScalar`, false},
		{"zig_eqlIgnoreCase_go_neg", "go", `eqlIgnoreCase`, false},
		{"zig_eqlIgnoreCase_java_neg", "java", `eqlIgnoreCase`, false},
		{"zig_stringToEnum_go_neg", "go", `stringToEnum`, false},
		{"zig_stringToEnum_ruby_neg", "ruby", `stringToEnum`, false},
		{"zig_toOwnedSlice_go_neg", "go", `toOwnedSlice`, false},
		{"zig_toOwnedSlice_python_neg", "python", `toOwnedSlice`, false},
		{"zig_parseFromSlice_go_neg", "go", `parseFromSlice`, false},
		{"zig_writeAll_go_neg", "go", `writeAll`, false},
		{"zig_writeAll_java_neg", "java", `writeAll`, false},
		{"zig_trim_go_neg", "go", `trim`, false},
		{"zig_trim_python_neg", "python", `trim`, false},
		{"zig_startsWith_go_neg", "go", `startsWith`, false},
		{"zig_startsWith_js_neg", "javascript", `startsWith`, false},
		{"zig_stringify_go_neg", "go", `stringify`, false},
		{"zig_stringify_js_neg", "javascript", `stringify`, false},
		{"zig_print_go_neg", "go", `print`, false},
		{"zig_print_python_neg", "python", `print`, false},
		{"zig_assert_go_neg", "go", `assert`, false},
		{"zig_assert_python_neg", "python", `assert`, false},
		{"zig_clamp_go_neg", "go", `clamp`, false},
		{"zig_clamp_python_neg", "python", `clamp`, false},
		{"zig_parseInt_go_neg", "go", `parseInt`, false},
		{"zig_eql_go_neg", "go", `eql`, false},
		{"zig_eql_python_neg", "python", `eql`, false},
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
