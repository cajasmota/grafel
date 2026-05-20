package resolve

import "testing"

// TestDynamicPatterns_Catalog_Lua verifies that Lua dynamic-dispatch
// patterns classify correctly (Refs #44 Lua slice).
func TestDynamicPatterns_Catalog_Lua(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		lang string
		stub string
		want bool
	}{
		// ---- Lua stdlib / global built-in dynamic stubs ----------------
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
