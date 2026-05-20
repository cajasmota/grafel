package resolve

import "testing"

// TestDynamicPatterns_Clojure covers the clojureDynamicPatterns catalog.
func TestDynamicPatterns_Clojure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		lang string
		stub string
		want bool
	}{
		// ── Clojure core BIFs: map/sequence operations ───────────────
		{"clojure_core_map", "clojure", `map`, true},
		{"clojure_core_filter", "clojure", `filter`, true},
		{"clojure_core_filterv", "clojure", `filterv`, true},
		{"clojure_core_reduce", "clojure", `reduce`, true},
		{"clojure_core_reduce_kv", "clojure", `reduce-kv`, true},
		{"clojure_core_mapv", "clojure", `mapv`, true},
		{"clojure_core_keep", "clojure", `keep`, true},
		{"clojure_core_remove", "clojure", `remove`, true},
		{"clojure_core_into", "clojure", `into`, true},
		{"clojure_core_first", "clojure", `first`, true},
		{"clojure_core_rest", "clojure", `rest`, true},
		{"clojure_core_next", "clojure", `next`, true},
		{"clojure_core_count", "clojure", `count`, true},
		{"clojure_core_nth", "clojure", `nth`, true},
		{"clojure_core_sort", "clojure", `sort`, true},
		{"clojure_core_sort_by", "clojure", `sort-by`, true},
		{"clojure_core_group_by", "clojure", `group-by`, true},
		{"clojure_core_take", "clojure", `take`, true},
		{"clojure_core_drop", "clojure", `drop`, true},
		{"clojure_core_vec", "clojure", `vec`, true},
		{"clojure_core_set", "clojure", `set`, true},
		{"clojure_core_range", "clojure", `range`, true},
		{"clojure_core_conj", "clojure", `conj`, true},
		{"clojure_core_cons", "clojure", `cons`, true},
		{"clojure_core_concat", "clojure", `concat`, true},
		{"clojure_core_flatten", "clojure", `flatten`, true},
		{"clojure_core_frequencies", "clojure", `frequencies`, true},
		{"clojure_core_zipmap", "clojure", `zipmap`, true},
		{"clojure_core_interleave", "clojure", `interleave`, true},
		{"clojure_core_partition", "clojure", `partition`, true},
		{"clojure_core_distinct", "clojure", `distinct`, true},

		// ── Clojure core BIFs: map/assoc operations ─────────────────
		{"clojure_core_assoc", "clojure", `assoc`, true},
		{"clojure_core_assoc_in", "clojure", `assoc-in`, true},
		{"clojure_core_dissoc", "clojure", `dissoc`, true},
		{"clojure_core_update", "clojure", `update`, true},
		{"clojure_core_update_in", "clojure", `update-in`, true},
		{"clojure_core_merge", "clojure", `merge`, true},
		{"clojure_core_get", "clojure", `get`, true},
		{"clojure_core_get_in", "clojure", `get-in`, true},
		{"clojure_core_keys", "clojure", `keys`, true},
		{"clojure_core_vals", "clojure", `vals`, true},
		{"clojure_core_select_keys", "clojure", `select-keys`, true},
		{"clojure_core_map_q", "clojure", `map?`, true},
		{"clojure_core_contains_q", "clojure", `contains?`, true},

		// ── Clojure core BIFs: atom/state operations ─────────────────
		{"clojure_core_swap_bang", "clojure", `swap!`, true},
		{"clojure_core_reset_bang", "clojure", `reset!`, true},
		{"clojure_core_atom", "clojure", `atom`, true},
		{"clojure_core_deref", "clojure", `deref`, true},

		// ── Clojure core BIFs: type/predicate functions ──────────────
		{"clojure_core_boolean", "clojure", `boolean`, true},
		{"clojure_core_str", "clojure", `str`, true},
		{"clojure_core_int", "clojure", `int`, true},
		{"clojure_core_keyword", "clojure", `keyword`, true},
		{"clojure_core_name", "clojure", `name`, true},
		{"clojure_core_type", "clojure", `type`, true},
		{"clojure_core_nil_q", "clojure", `nil?`, true},
		{"clojure_core_some_q", "clojure", `some?`, true},
		{"clojure_core_number_q", "clojure", `number?`, true},
		{"clojure_core_string_q", "clojure", `string?`, true},
		{"clojure_core_fn_q", "clojure", `fn?`, true},
		{"clojure_core_pos_q", "clojure", `pos?`, true},
		{"clojure_core_neg_q", "clojure", `neg?`, true},
		{"clojure_core_zero_q", "clojure", `zero?`, true},
		{"clojure_core_even_q", "clojure", `even?`, true},
		{"clojure_core_odd_q", "clojure", `odd?`, true},
		{"clojure_core_empty_q", "clojure", `empty?`, true},
		{"clojure_core_re_matches", "clojure", `re-matches`, true},
		{"clojure_core_re_find", "clojure", `re-find`, true},
		{"clojure_core_format", "clojure", `format`, true},
		{"clojure_core_println", "clojure", `println`, true},
		{"clojure_core_apply", "clojure", `apply`, true},
		{"clojure_core_partial", "clojure", `partial`, true},
		{"clojure_core_comp", "clojure", `comp`, true},
		{"clojure_core_memoize", "clojure", `memoize`, true},
		{"clojure_core_identity", "clojure", `identity`, true},
		{"clojure_core_max", "clojure", `max`, true},
		{"clojure_core_min", "clojure", `min`, true},
		{"clojure_core_inc", "clojure", `inc`, true},
		{"clojure_core_dec", "clojure", `dec`, true},
		{"clojure_core_mod", "clojure", `mod`, true},
		{"clojure_core_rand_int", "clojure", `rand-int`, true},
		{"clojure_core_shuffle", "clojure", `shuffle`, true},

		// ── Ring / Pedestal HTTP helpers ─────────────────────────────
		{"clojure_ring_response", "clojure", `response`, true},
		{"clojure_ring_not_found", "clojure", `not-found`, true},
		{"clojure_ring_created", "clojure", `created`, true},
		{"clojure_ring_ok", "clojure", `ok`, true},
		{"clojure_ring_bad_request", "clojure", `bad-request`, true},
		{"clojure_ring_redirect", "clojure", `redirect`, true},
		{"clojure_ring_header", "clojure", `header`, true},
		{"clojure_ring_content_type", "clojure", `content-type`, true},
		{"clojure_ring_wrap_params", "clojure", `wrap-params`, true},
		{"clojure_ring_wrap_defaults", "clojure", `wrap-defaults`, true},
		{"clojure_ring_wrap_cors", "clojure", `wrap-cors`, true},
		{"clojure_ring_wrap_session", "clojure", `wrap-session`, true},

		// ── Namespace-qualified stdlib calls ─────────────────────────
		{"clojure_str_join", "clojure", `str/join`, true},
		{"clojure_str_split", "clojure", `str/split`, true},
		{"clojure_str_trim", "clojure", `str/trim`, true},
		{"clojure_str_upper_case", "clojure", `str/upper-case`, true},
		{"clojure_str_lower_case", "clojure", `str/lower-case`, true},
		{"clojure_str_starts_with", "clojure", `str/starts-with?`, true},
		{"clojure_str_includes", "clojure", `str/includes?`, true},
		{"clojure_str_replace", "clojure", `str/replace`, true},
		{"clojure_str_blank_q", "clojure", `str/blank?`, true},
		{"clojure_log_debug", "clojure", `log/debug`, true},
		{"clojure_log_info", "clojure", `log/info`, true},
		{"clojure_log_warn", "clojure", `log/warn`, true},
		{"clojure_log_error", "clojure", `log/error`, true},
		{"clojure_codec_form_decode_map", "clojure", `codec/form-decode-map`, true},
		{"clojure_codec_url_encode", "clojure", `codec/url-encode`, true},
		{"clojure_req_character_encoding", "clojure", `req/character-encoding`, true},
		{"clojure_req_urlencoded_form_q", "clojure", `req/urlencoded-form?`, true},
		{"clojure_json_generate_string", "clojure", `json/generate-string`, true},
		{"clojure_json_parse_string", "clojure", `json/parse-string`, true},

		// ── core.async channels ──────────────────────────────────────
		{"clojure_async_chan", "clojure", `chan`, true},
		{"clojure_async_close_bang", "clojure", `close!`, true},
		{"clojure_async_put_bang", "clojure", `put!`, true},
		{"clojure_async_take_bang", "clojure", `take!`, true},
		{"clojure_async_go_loop", "clojure", `go-loop`, true},
		{"clojure_async_pipeline", "clojure", `pipeline`, true},
		{"clojure_async_lt_lt_bang", "clojure", `<!!`, true},
		{"clojure_async_gt_gt_bang", "clojure", `>!!`, true},

		// ── Java interop ─────────────────────────────────────────────
		{"clojure_java_integer_parseint", "clojure", `Integer/parseInt`, true},
		{"clojure_java_long_parselong", "clojure", `Long/parseLong`, true},
		{"clojure_java_double_parsedouble", "clojure", `Double/parseDouble`, true},
		{"clojure_java_system_getenv", "clojure", `System/getenv`, true},
		{"clojure_java_system_currenttimemillis", "clojure", `System/currentTimeMillis`, true},
		{"clojure_java_math_floor", "clojure", `Math/floor`, true},
		{"clojure_java_math_abs", "clojure", `Math/abs`, true},
		{"clojure_java_uuid_random", "clojure", `UUID/randomUUID`, true},
		{"clojure_java_thread_sleep", "clojure", `Thread/sleep`, true},

		// ── Cross-language gate: patterns MUST NOT fire for other langs ──
		// filter is a Python builtin — the Clojure gate must prevent
		// false-positive classification on Python corpora.
		{"clojure_filter_python_neg", "python", `filter`, false},
		{"clojure_filter_go_neg", "go", `filter`, false},
		{"clojure_filter_ruby_neg", "ruby", `filter`, false},
		{"clojure_filter_js_neg", "javascript", `filter`, false},
		{"clojure_map_python_neg", "python", `map`, false},
		{"clojure_map_go_neg", "go", `map`, false},
		{"clojure_map_ruby_neg", "ruby", `map`, false},
		// count: SQL, Go len(), Ruby
		{"clojure_count_python_neg", "python", `count`, false},
		{"clojure_count_go_neg", "go", `count`, false},
		{"clojure_count_java_neg", "java", `count`, false},
		// get: common in many frameworks
		{"clojure_get_go_neg", "go", `get`, false},
		{"clojure_get_python_neg", "python", `get`, false},
		{"clojure_get_java_neg", "java", `get`, false},
		// NOTE: `get` already fires for "ruby" (Rails route helper) — skip ruby gate here.
		// {"clojure_get_ruby_neg", "ruby", `get`, false},
		// assoc: Python dict.update is "update" not "assoc", but "assoc" is
		// safe under the gate anyway.
		{"clojure_assoc_go_neg", "go", `assoc`, false},
		{"clojure_assoc_python_neg", "python", `assoc`, false},
		// response: HTTP framework helper in many languages
		{"clojure_response_go_neg", "go", `response`, false},
		{"clojure_response_python_neg", "python", `response`, false},
		// NOTE: `response` already fires for "ruby" (Rails controller helper) — skip ruby gate here.
		// {"clojure_response_ruby_neg", "ruby", `response`, false},
		{"clojure_response_ts_neg", "typescript", `response`, false},
		// status: common method name
		{"clojure_status_go_neg", "go", `status`, false},
		{"clojure_status_python_neg", "python", `status`, false},
		// boolean: Java primitive / Python builtin
		{"clojure_boolean_java_neg", "java", `boolean`, false},
		{"clojure_boolean_python_neg", "python", `boolean`, false},
		// redirect: common in web frameworks
		{"clojure_redirect_go_neg", "go", `redirect`, false},
		{"clojure_redirect_python_neg", "python", `redirect`, false},
		// format: Python str.format
		{"clojure_format_python_neg", "python", `format`, false},
		{"clojure_format_go_neg", "go", `format`, false},
		// str (Rust, Go fmt)
		{"clojure_str_go_neg", "go", `str`, false},
		{"clojure_str_python_neg", "python", `str`, false},
		// remove: JS Array.prototype.remove
		{"clojure_remove_js_neg", "javascript", `remove`, false},
		{"clojure_remove_ruby_neg", "ruby", `remove`, false},
		// type: TypeScript
		{"clojure_type_ts_neg", "typescript", `type`, false},
		// distinct: SQL
		{"clojure_distinct_go_neg", "go", `distinct`, false},
		// max/min: common everywhere
		{"clojure_max_go_neg", "go", `max`, false},
		{"clojure_max_python_neg", "python", `max`, false},
		{"clojure_min_go_neg", "go", `min`, false},
		{"clojure_min_python_neg", "python", `min`, false},
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

// TestDynamicPatterns_Clojure_CatalogSize guards against accidental catalog
// truncation. If you intentionally shrink the catalog, update this guard.
func TestDynamicPatterns_Clojure_CatalogSize(t *testing.T) {
	t.Parallel()
	const minExpected = 120
	if got := len(clojureDynamicPatterns); got < minExpected {
		t.Fatalf("clojureDynamicPatterns has %d entries; expected at least %d — accidental truncation?", got, minExpected)
	}
}

// TestDynamicPatterns_Clojure_Registration guards that the init() hook ran.
func TestDynamicPatterns_Clojure_Registration(t *testing.T) {
	t.Parallel()
	if _, ok := dynamicPatternsByLang["clojure"]; !ok {
		t.Fatal("clojureDynamicPatterns not registered under key 'clojure'")
	}
}
