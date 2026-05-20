package resolve

import "testing"

// TestDynamicPatterns_Catalog_Python verifies that Python dynamic-dispatch
// patterns classify correctly (Refs #44). Each row is a representative
// call-site stub produced by the Python extractor.
func TestDynamicPatterns_Catalog_Python(t *testing.T) {
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
		// Bare-identifier forms (issue #90).
		{"py_bare_getattr", "python", `getattr`, true},
		{"py_bare_setattr", "python", `setattr`, true},
		{"py_bare_eval", "python", `eval`, true},
		{"py_bare_exec", "python", `exec`, true},
		{"py_bare_dunder_import", "python", `__import__`, true},
		// click decorator + helper DSL (issue #423).
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
		// NOT classify as Python dynamic.
		{"click_command_js_negative", "javascript", `command`, false},
		{"click_option_ruby_negative", "ruby", `option`, false},
		{"click_echo_go_negative", "go", `echo`, false},
		{"click_group_java_negative", "java", `group`, false},
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
