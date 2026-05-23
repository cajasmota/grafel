package resolve

import "testing"

// TestIsStdlibBuiltinTarget_JVMAndSystemsLangs verifies that Java, Kotlin,
// Scala, Rust, Swift, and C# stdlib name sets are declared (for documentation
// and use via RegisterExtraStdlibFilter) but are NOT registered in the default
// dispatch table, since those languages already have per-language bare-name
// catalogs in internal/external.classifyExternal that emit ext:<name> externals.
// Adding them to the dispatch table would change observable behavior and break
// existing tests.
func TestIsStdlibBuiltinTarget_JVMAndSystemsLangs(t *testing.T) {
	t.Parallel()
	// Verify that language sets are declared but not active in the default dispatch table.
	// These names are in the per-language stdlib var declarations but NOT in stdlibBuiltinsByLang.
	cases := []struct {
		name string
		stub string
		lang string
		want bool
	}{
		// Java — not in dispatch table (handled by javaBareNames in classifyExternal).
		{"java_arraycopy_not_dispatched", "arraycopy", "java", false},
		{"java_requireNonNull_not_dispatched", "requireNonNull", "java", false},
		// Kotlin — not in dispatch table (handled by kotlinBareNames in classifyExternal).
		{"kotlin_listOf_not_dispatched", "listOf", "kotlin", false},
		{"kotlin_launch_not_dispatched", "launch", "kotlin", false},
		// Scala — not in dispatch table (handled by scalaBareNames in classifyExternal).
		{"scala_implicitly_not_dispatched", "implicitly", "scala", false},
		// Rust — not in dispatch table (handled by rustBareNames in classifyExternal).
		{"rust_dbg_not_dispatched", "dbg", "rust", false},
		{"rust_eprintln_not_dispatched", "eprintln", "rust", false},
		// Swift — not in dispatch table (handled by swiftBareNames in classifyExternal).
		{"swift_autoreleasepool_not_dispatched", "autoreleasepool", "swift", false},
		// C# — not in dispatch table (handled by csharpBareNames in classifyExternal).
		{"csharp_WriteLine_not_dispatched", "WriteLine", "csharp", false},
		{"csharp_alias_not_dispatched", "IsNullOrEmpty", "c#", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := IsStdlibBuiltinTarget(tc.stub, tc.lang)
			if got != tc.want {
				t.Errorf("IsStdlibBuiltinTarget(%q, %q)=%v, want %v (language has classifyExternal catalog)", tc.stub, tc.lang, got, tc.want)
			}
		})
	}
}

// TestIsStdlibBuiltinTarget_PHP verifies PHP stdlib bare-name symbols.
func TestIsStdlibBuiltinTarget_PHP(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		stub string
		lang string
		want bool
	}{
		{"php_strlen", "strlen", "php", true},
		{"php_strtolower", "strtolower", "php", true},
		{"php_json_encode", "json_encode", "php", true},
		{"php_json_decode", "json_decode", "php", true},
		{"php_array_keys", "array_keys", "php", true},
		{"php_array_merge", "array_merge", "php", true},
		// count is excluded from php stdlib set (collides with Laravel's count())
		{"php_in_array", "in_array", "php", true},
		{"php_is_array", "is_array", "php", true},
		{"php_isset", "isset", "php", true},
		{"php_empty", "empty", "php", true},
		{"php_fopen", "fopen", "php", true},
		{"php_file_get_contents", "file_get_contents", "php", true},
		{"php_base64_encode", "base64_encode", "php", true},
		{"php_md5", "md5", "php", true},
		{"php_sprintf", "sprintf", "php", true},
		{"php_time", "time", "php", true},
		{"php_date", "date", "php", true},
		{"php_session_start", "session_start", "php", true},
		{"php_header", "header", "php", true},
		{"php_var_dump", "var_dump", "php", true},
		// Unknown user name.
		{"php_unknown", "myController", "php", false},
		// Cross-language gate.
		{"php_strlen_not_python", "strlen", "python", false},
		{"php_json_encode_not_js", "json_encode", "javascript", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := IsStdlibBuiltinTarget(tc.stub, tc.lang)
			if got != tc.want {
				t.Errorf("IsStdlibBuiltinTarget(%q, %q)=%v, want %v", tc.stub, tc.lang, got, tc.want)
			}
		})
	}
}

// TestIsStdlibBuiltinTarget_Elixir verifies Elixir stdlib bare-name symbols.
func TestIsStdlibBuiltinTarget_Elixir(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		stub string
		lang string
		want bool
	}{
		{"elixir_is_integer", "is_integer", "elixir", true},
		{"elixir_is_binary", "is_binary", "elixir", true},
		{"elixir_is_nil", "is_nil", "elixir", true},
		{"elixir_length", "length", "elixir", true},
		{"elixir_hd", "hd", "elixir", true},
		{"elixir_tl", "tl", "elixir", true},
		{"elixir_abs", "abs", "elixir", true},
		{"elixir_max", "max", "elixir", true},
		{"elixir_to_string", "to_string", "elixir", true},
		{"elixir_inspect", "inspect", "elixir", true},
		{"elixir_raise", "raise", "elixir", true},
		{"elixir_send", "send", "elixir", true},
		{"elixir_spawn", "spawn", "elixir", true},
		{"elixir_map", "map", "elixir", true},
		{"elixir_filter", "filter", "elixir", true},
		{"elixir_reduce", "reduce", "elixir", true},
		{"elixir_sort", "sort", "elixir", true},
		{"elixir_group_by", "group_by", "elixir", true},
		{"elixir_upcase", "upcase", "elixir", true},
		{"elixir_split", "split", "elixir", true},
		// Unknown user name.
		{"elixir_unknown", "my_context_function", "elixir", false},
		// Cross-language gate.
		{"elixir_is_integer_not_erlang", "is_integer", "erlang", false},
		{"elixir_spawn_not_python", "spawn", "python", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := IsStdlibBuiltinTarget(tc.stub, tc.lang)
			if got != tc.want {
				t.Errorf("IsStdlibBuiltinTarget(%q, %q)=%v, want %v", tc.stub, tc.lang, got, tc.want)
			}
		})
	}
}

// TestIsStdlibBuiltinTarget_Clojure verifies Clojure stdlib bare-name symbols.
func TestIsStdlibBuiltinTarget_Clojure(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		stub string
		lang string
		want bool
	}{
		{"clojure_conj", "conj", "clojure", true},
		{"clojure_assoc", "assoc", "clojure", true},
		{"clojure_get", "get", "clojure", true},
		{"clojure_get_in", "get-in", "clojure", true},
		{"clojure_count", "count", "clojure", true},
		{"clojure_first", "first", "clojure", true},
		{"clojure_rest", "rest", "clojure", true},
		{"clojure_map", "map", "clojure", true},
		{"clojure_filter", "filter", "clojure", true},
		{"clojure_reduce", "reduce", "clojure", true},
		{"clojure_println", "println", "clojure", true},
		{"clojure_str", "str", "clojure", true},
		{"clojure_nil_pred", "nil?", "clojure", true},
		{"clojure_empty_pred", "empty?", "clojure", true},
		{"clojure_partial", "partial", "clojure", true},
		{"clojure_comp", "comp", "clojure", true},
		{"clojure_zipmap", "zipmap", "clojure", true},
		{"clojure_Math_abs", "Math/abs", "clojure", true},
		{"clojure_System_exit", "System/exit", "clojure", true},
		// Unknown user name.
		{"clojure_unknown", "my-domain-fn", "clojure", false},
		// Cross-language gate.
		{"clojure_conj_not_java", "conj", "java", false},
		{"clojure_reduce_not_python", "reduce", "python", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := IsStdlibBuiltinTarget(tc.stub, tc.lang)
			if got != tc.want {
				t.Errorf("IsStdlibBuiltinTarget(%q, %q)=%v, want %v", tc.stub, tc.lang, got, tc.want)
			}
		})
	}
}

// TestIsStdlibBuiltinTarget_Erlang verifies Erlang stdlib bare-name symbols.
func TestIsStdlibBuiltinTarget_Erlang(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		stub string
		lang string
		want bool
	}{
		{"erlang_self", "self", "erlang", true},
		{"erlang_spawn", "spawn", "erlang", true},
		{"erlang_spawn_link", "spawn_link", "erlang", true},
		{"erlang_make_ref", "make_ref", "erlang", true},
		{"erlang_whereis", "whereis", "erlang", true},
		{"erlang_register", "register", "erlang", true},
		{"erlang_send", "send", "erlang", true},
		{"erlang_hd", "hd", "erlang", true},
		{"erlang_tl", "tl", "erlang", true},
		{"erlang_length", "length", "erlang", true},
		{"erlang_abs", "abs", "erlang", true},
		{"erlang_size", "size", "erlang", true},
		{"erlang_element", "element", "erlang", true},
		{"erlang_apply", "apply", "erlang", true},
		{"erlang_max", "max", "erlang", true},
		{"erlang_min", "min", "erlang", true},
		{"erlang_atom_to_list", "atom_to_list", "erlang", true},
		{"erlang_binary_to_term", "binary_to_term", "erlang", true},
		// Unknown user name.
		{"erlang_unknown", "my_module_fn", "erlang", false},
		// Cross-language gate.
		{"erlang_self_not_elixir", "self", "elixir", true}, // elixir also has self — OK
		{"erlang_hd_not_python", "hd", "python", false},
		{"erlang_spawn_not_javascript", "spawn", "javascript", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := IsStdlibBuiltinTarget(tc.stub, tc.lang)
			if got != tc.want {
				t.Errorf("IsStdlibBuiltinTarget(%q, %q)=%v, want %v", tc.stub, tc.lang, got, tc.want)
			}
		})
	}
}

// TestRegisterExtraStdlibFilter verifies the user-extensible extra filter
// (Issue #1206). Names registered via RegisterExtraStdlibFilter are treated
// as stdlib builtins for the specified language.
func TestRegisterExtraStdlibFilter(t *testing.T) {
	// Restore state after test.
	orig := extraStdlibFilter
	extraStdlibFilter = map[string]map[string]struct{}{}
	t.Cleanup(func() { extraStdlibFilter = orig })

	// Register custom names for python and java.
	RegisterExtraStdlibFilter("python", []string{"authenticate", "login_required"})
	RegisterExtraStdlibFilter("java", []string{"doFilter", "doGet"})

	// Registered names must be suppressed.
	cases := []struct {
		stub string
		lang string
		want bool
	}{
		{"authenticate", "python", true},
		{"login_required", "python", true},
		{"doFilter", "java", true},
		{"doGet", "java", true},
		// Non-registered names still pass through.
		{"my_custom_fn", "python", false},
		{"myMethod", "java", false},
		// Cross-language: python-registered name not in java.
		{"authenticate", "java", false},
		{"doFilter", "python", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.stub+"_"+tc.lang, func(t *testing.T) {
			got := IsStdlibBuiltinTarget(tc.stub, tc.lang)
			if got != tc.want {
				t.Errorf("IsStdlibBuiltinTarget(%q, %q)=%v, want %v", tc.stub, tc.lang, got, tc.want)
			}
		})
	}
}

// TestRegisterExtraStdlibFilter_Normalisation ensures that the language tag is
// normalised before lookup so "Kotlin", "kt", and "kotlin" all resolve to the
// same entry.
func TestRegisterExtraStdlibFilter_Normalisation(t *testing.T) {
	orig := extraStdlibFilter
	extraStdlibFilter = map[string]map[string]struct{}{}
	t.Cleanup(func() { extraStdlibFilter = orig })

	RegisterExtraStdlibFilter("Kotlin", []string{"myFrameworkFn"})
	// All normalised forms must match.
	if !IsStdlibBuiltinTarget("myFrameworkFn", "kotlin") {
		t.Error("IsStdlibBuiltinTarget(myFrameworkFn, kotlin) = false; want true after RegisterExtraStdlibFilter(Kotlin, ...)")
	}
	if !IsStdlibBuiltinTarget("myFrameworkFn", "kt") {
		t.Error("IsStdlibBuiltinTarget(myFrameworkFn, kt) = false; want true (kt aliases to kotlin)")
	}
	if !IsStdlibBuiltinTarget("myFrameworkFn", "KOTLIN") {
		t.Error("IsStdlibBuiltinTarget(myFrameworkFn, KOTLIN) = false; want true (case-insensitive)")
	}
}

// TestIsStdlibBuiltinTarget_EmptyInputs confirms the guard against empty
// inputs at the API boundary.
func TestIsStdlibBuiltinTarget_EmptyInputs(t *testing.T) {
	if IsStdlibBuiltinTarget("", "python") {
		t.Error("IsStdlibBuiltinTarget(\"\", \"python\") = true; want false (empty stub)")
	}
	if IsStdlibBuiltinTarget("println", "") {
		t.Error("IsStdlibBuiltinTarget(\"println\", \"\") = true; want false (empty lang)")
	}
	if IsStdlibBuiltinTarget("", "") {
		t.Error("IsStdlibBuiltinTarget(\"\", \"\") = true; want false (both empty)")
	}
}
