package resolve

import "testing"

// TestDynamicPatterns_Catalog_Cross verifies cross-language patterns and
// negative cases that are not specific to any one language (Refs #44).
// These include template-interpolation stubs (which fire regardless of
// language) and structural-ref / external-package shapes that MUST NOT
// be classified as dynamic.
func TestDynamicPatterns_Catalog_Cross(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		lang string
		stub string
		want bool
	}{
		// ---- Go -----------------------------------------------------
		{"go_reflect_call", "go", `reflect.Value.Call`, true},
		{"go_reflect_valueof", "go", `reflect.ValueOf(x)`, true},
		{"go_method_by_name", "go", `v.MethodByName("Foo").Call(args)`, true},
		{"go_field_by_name", "go", `v.FieldByName("X")`, true},
		{"go_plugin_open", "go", `plugin.Open("./mod.so")`, true},
		{"go_plugin_lookup", "go", `plugin.Lookup("Sym")`, true},
		// Extra: ensure receiver-anchored Lookup is required for Go even
		// when language is missing.
		{"neg_unknown_lang_repo_lookup", "", `repo.Lookup(id)`, false},
		// `repo.Lookup(id)` in Go — must NOT match `plugin.Lookup`.
		{"neg_go_repo_lookup", "go", `repo.Lookup(id)`, false},

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
		// And that res.send under unknown language is NOT dynamic.
		{"neg_unknown_lang_res_send", "", `res.send("hello")`, false},
		// `factory.newInstance()` — domain factory, not reflective.
		{"neg_factory_newinstance", "java", `factory.newInstance()`, false},
		// `cli.invoke(...)` — user-defined invoke, not Method.invoke.
		{"neg_cli_invoke", "java", `cli.invoke(cmd, args)`, false},
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
