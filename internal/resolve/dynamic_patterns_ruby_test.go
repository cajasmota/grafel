package resolve

import "testing"

// TestDynamicPatterns_Catalog_Ruby verifies that Ruby dynamic-dispatch
// patterns classify correctly (Refs #44).
func TestDynamicPatterns_Catalog_Ruby(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		lang string
		stub string
		want bool
	}{
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
		// Bare-identifier forms (issue #90).
		{"rb_bare_send_id", "ruby", `send`, true},
		{"rb_bare_public_send_id", "ruby", `public_send`, true},
		{"rb_bare_dunder_send_id", "ruby", `__send__`, true},
		{"rb_bare_define_method_id", "ruby", `define_method`, true},
		{"rb_bare_instance_eval_id", "ruby", `instance_eval`, true},
		{"rb_bare_class_eval_id", "ruby", `class_eval`, true},
		// Rails ActionPack / ActionDispatch / ActiveSupport internals (issue #448).
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
		{"rb_rails_helper", "ruby", `helper`, true},
		{"rb_rails_layout", "ruby", `layout`, true},
		{"rb_rails_protect_from_forgery", "ruby", `protect_from_forgery`, true},
		{"rb_rails_skip_authorization_check", "ruby", `skip_authorization_check`, true},
		{"rb_rails_verify_authenticity_token", "ruby", `verify_authenticity_token`, true},
		{"rb_rails_respond_with", "ruby", `respond_with`, true},
		{"rb_rails_headers", "ruby", `headers`, true},
		{"rb_rails_prepended", "ruby", `prepended`, true},
		{"rb_rails_class_attribute", "ruby", `class_attribute`, true},
		{"rb_rails_mattr_accessor", "ruby", `mattr_accessor`, true},
		{"rb_rails_mattr_reader", "ruby", `mattr_reader`, true},
		{"rb_rails_mattr_writer", "ruby", `mattr_writer`, true},
		{"rb_rails_cattr_accessor", "ruby", `cattr_accessor`, true},
		{"rb_rails_define_callbacks", "ruby", `define_callbacks`, true},
		{"rb_rails_set_callback", "ruby", `set_callback`, true},
		{"rb_rails_skip_callback", "ruby", `skip_callback`, true},
		{"rb_rails_add_middleware", "ruby", `add_middleware`, true},
		{"rb_rails_delete_middleware", "ruby", `delete_middleware`, true},
		{"rb_rails_insert_before", "ruby", `insert_before`, true},
		{"rb_rails_insert_after", "ruby", `insert_after`, true},
		// Per-language gate: Rails internals from non-Ruby files MUST NOT classify as dynamic.
		{"rb_rails_get_js_negative", "javascript", `resources`, false},
		{"rb_rails_namespace_go_negative", "go", `namespace`, false},
		{"rb_rails_mount_python_negative", "python", `mount`, false},
		{"rb_rails_controller_java_negative", "java", `controller`, false},
		{"rb_rails_headers_kotlin_negative", "kotlin", `headers`, false},
		{"rb_rails_layout_swift_negative", "swift", `layout`, false},
		{"rb_rails_set_callback_rust_negative", "rust", `set_callback`, false},
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
