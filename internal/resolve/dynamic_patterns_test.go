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
