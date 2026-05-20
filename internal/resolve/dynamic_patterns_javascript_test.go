package resolve

import "testing"

// TestDynamicPatterns_Catalog_JavaScript verifies that JavaScript/TypeScript
// dynamic-dispatch patterns classify correctly (Refs #44).
func TestDynamicPatterns_Catalog_JavaScript(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		lang string
		stub string
		want bool
	}{
		// ---- TypeScript / JavaScript -------------------------------
		{"js_reflect_apply", "javascript", `Reflect.apply(fn, this, args)`, true},
		{"js_reflect_construct", "javascript", `Reflect.construct(C, args)`, true},
		{"js_function_ctor", "javascript", `Function("return 1")`, true},
		{"js_new_function", "javascript", `new Function("return 1")`, true},
		{"js_dynamic_import_var", "javascript", `import(modName)`, true},
		{"js_require_dynamic_var", "javascript", `require(modName)`, true},
		{"js_process_env", "javascript", `process.env.NODE_ENV`, true},
		{"ts_reflect_apply", "typescript", `Reflect.apply(fn, this, args)`, true},
		// Negative: literal-string require/import are statically resolvable.
		{"neg_require_literal_dquote", "javascript", `require("fs")`, false},
		{"neg_require_literal_squote", "javascript", `require('fs')`, false},
		{"neg_import_literal", "javascript", `import("./literal-mod")`, false},
		// Cross-language collisions: these MUST NOT match.
		// `res.send("hello")` in Node — must NOT match Ruby `.send`.
		{"neg_node_res_send", "javascript", `res.send("hello")`, false},
		// `discount.apply(order)` — domain method, not Function.prototype.apply.
		{"neg_discount_apply", "javascript", `discount.apply(order)`, false},
		// `controller.call(...)` — domain method, not Function.prototype.call.
		{"neg_controller_call", "javascript", `controller.call(req, res)`, false},
		// `db.bind(":id", 1)` — DB driver bind, not Function.prototype.bind.
		{"neg_db_bind", "javascript", `db.bind(":id", 1)`, false},
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
