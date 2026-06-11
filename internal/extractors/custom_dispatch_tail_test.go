package extractors

import "testing"

// TestTailCoverageLinkageExtractorsDispatch is a table-driven guard against a
// class of latent bug surfaced by the #4749 coverage-linkage tail: a new
// custom_<lang>_* extractor (tests_route_e2e / integration_e2e / phpunit_pest)
// may register under a key prefix that the language's dispatch entry does not
// point to, so CustomExtractorsFor(<lang>) silently never selects it in
// PRODUCTION even though its direct-registration unit test passes.
//
// Concrete instance this test locks down: Lua. Its framework extractors use the
// bare `lua_` prefix, but the tail extractor registered as
// `custom_lua_tests_route_e2e` — which does NOT share the `lua_` prefix. The fix
// adds `custom_lua_` to extraCustomPrefixesForLanguage["lua"]. This test fails
// if that (or any analogous) dispatch wiring regresses.
//
// For each language, it asserts that the REAL dispatch path
// (CustomExtractorsFor → prefix selection over the live registry populated by
// custom_registry.go) returns an extractor whose registration key is the
// expected coverage-linkage key.
func TestTailCoverageLinkageExtractorsDispatch(t *testing.T) {
	cases := []struct {
		language    string
		expectedKey string
	}{
		{"scala", "custom_scala_tests_route_e2e"},
		{"rust", "custom_rust_tests_route_e2e"},
		{"swift", "custom_swift_tests_route_e2e"},
		{"crystal", "custom_crystal_tests_route_e2e"},
		{"clojure", "custom_clojure_tests_route_e2e"},
		{"fsharp", "custom_fsharp_tests_route_e2e"},
		{"groovy", "custom_groovy_tests_route_e2e"},
		{"erlang", "custom_erlang_tests_route_e2e"},
		{"elixir", "custom_elixir_tests_route_e2e"},
		{"lua", "custom_lua_tests_route_e2e"}, // the confirmed-broken case (#4749 tail)
		{"nim", "custom_nim_tests_route_e2e"},
		{"kotlin", "custom_kotlin_tests_route_e2e"}, // #4723
		{"csharp", "custom_csharp_integration_e2e"}, // #4720
		{"php", "custom_php_phpunit_pest"},          // #4721
	}

	for _, tc := range cases {
		t.Run(tc.language, func(t *testing.T) {
			exts := CustomExtractorsFor(tc.language)
			found := false
			keys := make([]string, 0, len(exts))
			for _, e := range exts {
				k := e.Language()
				keys = append(keys, k)
				if k == tc.expectedKey {
					found = true
				}
			}
			if !found {
				t.Errorf("CustomExtractorsFor(%q) did not return %q via production dispatch; "+
					"the extractor registers but its key prefix is not wired into the dispatch map. "+
					"Selected keys: %v", tc.language, tc.expectedKey, keys)
			}
		})
	}
}
