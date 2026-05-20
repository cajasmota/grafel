// http_path_normalize_test.go — unit tests for normalizePath and helpers.
// Issue #807: HTTP path normalization before entity ID computation.
package engine

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// normalizePath — composite rules
// ---------------------------------------------------------------------------

func TestNormalizePath_EmptyInput(t *testing.T) {
	r := normalizePath("")
	if r.Path != "/" {
		t.Errorf("empty input: want /, got %q", r.Path)
	}
	if len(r.Props) != 0 {
		t.Errorf("empty input: want no props, got %v", r.Props)
	}
}

func TestNormalizePath_PlainPath_NoChange(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"/buildings", "/buildings"},
		{"/api/v1/users", "/api/v1/users"},
		{"/", "/"},
		{"/buildings/{id}", "/buildings/{id}"},
	}
	for _, tc := range tests {
		r := normalizePath(tc.in)
		if r.Path != tc.want {
			t.Errorf("normalizePath(%q): path = %q, want %q", tc.in, r.Path, tc.want)
		}
		if len(r.Props) != 0 {
			t.Errorf("normalizePath(%q): unexpected props %v", tc.in, r.Props)
		}
	}
}

// ---------------------------------------------------------------------------
// Rule 1: env-var prefix stripping — JS/TS template literal
// ---------------------------------------------------------------------------

// TestNormalizePath_Rule1_JSTemplateLiteral_SimpleVar verifies that a plain
// ALL_CAPS identifier prefix like ${VITE_CORE_API} is NOT stripped by
// normalizePath (to preserve its {VITE_CORE_API} placeholder semantic per
// #706). Only explicit process.env.X / import.meta.env.X forms are stripped.
// The stripping for ALL_CAPS via canonicalizeTemplateLiteral uses
// isEnvVarStyleExpr with a tighter heuristic.
func TestNormalizePath_Rule1_JSTemplateLiteral_SimpleVar(t *testing.T) {
	// Plain ${IDENT} — NOT stripped at normalizePath level (could be a local const).
	r := normalizePath("${VITE_CORE_API}/buildings")
	// normalizePath leaves the ${...} for Rule 4 expansion: ${VITE_CORE_API} → {VITE_CORE_API}
	// The result is {VITE_CORE_API}/buildings (Rule 4 expanded, no env-var strip).
	if r.Path != "{VITE_CORE_API}/buildings" {
		t.Errorf("path = %q, want {VITE_CORE_API}/buildings", r.Path)
	}
	if r.Props["base_url_var"] != "" {
		t.Errorf("base_url_var should be empty for plain ALL_CAPS, got %q", r.Props["base_url_var"])
	}
}

func TestNormalizePath_Rule1_JSTemplateLiteral_ProcessEnv(t *testing.T) {
	r := normalizePath("${process.env.API_URL}/users")
	if r.Path != "/users" {
		t.Errorf("path = %q, want /users", r.Path)
	}
	if r.Props["base_url_var"] != "process.env.API_URL" {
		t.Errorf("base_url_var = %q, want process.env.API_URL", r.Props["base_url_var"])
	}
}

func TestNormalizePath_Rule1_JSTemplateLiteral_ImportMetaEnv(t *testing.T) {
	r := normalizePath("${import.meta.env.VITE_BASE}/api/v1/users")
	if r.Path != "/api/v1/users" {
		t.Errorf("path = %q, want /api/v1/users", r.Path)
	}
	if r.Props["base_url_var"] != "import.meta.env.VITE_BASE" {
		t.Errorf("base_url_var = %q, want import.meta.env.VITE_BASE", r.Props["base_url_var"])
	}
}

// ---------------------------------------------------------------------------
// Rule 1: env-var prefix stripping — Python
// ---------------------------------------------------------------------------

func TestNormalizePath_Rule1_Python_OsEnvironBracket(t *testing.T) {
	r := normalizePath(`os.environ['API_URL'] + '/buildings'`)
	if r.Path != "/buildings" {
		t.Errorf("path = %q, want /buildings", r.Path)
	}
	if r.Props["base_url_var"] != "API_URL" {
		t.Errorf("base_url_var = %q, want API_URL", r.Props["base_url_var"])
	}
}

func TestNormalizePath_Rule1_Python_OsEnvironDoubleQuote(t *testing.T) {
	r := normalizePath(`os.environ["API_URL"] + "/users"`)
	if r.Path != "/users" {
		t.Errorf("path = %q, want /users", r.Path)
	}
	if r.Props["base_url_var"] != "API_URL" {
		t.Errorf("base_url_var = %q, want API_URL", r.Props["base_url_var"])
	}
}

func TestNormalizePath_Rule1_Python_OsGetenv(t *testing.T) {
	r := normalizePath(`os.getenv('BASE_URL') + '/orders'`)
	if r.Path != "/orders" {
		t.Errorf("path = %q, want /orders", r.Path)
	}
	if r.Props["base_url_var"] != "BASE_URL" {
		t.Errorf("base_url_var = %q, want BASE_URL", r.Props["base_url_var"])
	}
}

func TestNormalizePath_Rule1_Python_BareGetenv(t *testing.T) {
	r := normalizePath(`getenv('API_URL') + '/health'`)
	if r.Path != "/health" {
		t.Errorf("path = %q, want /health", r.Path)
	}
	if r.Props["base_url_var"] != "API_URL" {
		t.Errorf("base_url_var = %q, want API_URL", r.Props["base_url_var"])
	}
}

// ---------------------------------------------------------------------------
// Rule 1: env-var prefix stripping — Java
// ---------------------------------------------------------------------------

func TestNormalizePath_Rule1_Java_SystemGetenv(t *testing.T) {
	r := normalizePath(`System.getenv("API_URL") + "/foo"`)
	if r.Path != "/foo" {
		t.Errorf("path = %q, want /foo", r.Path)
	}
	if r.Props["base_url_var"] != "API_URL" {
		t.Errorf("base_url_var = %q, want API_URL", r.Props["base_url_var"])
	}
}

// ---------------------------------------------------------------------------
// Rule 1: env-var prefix stripping — Go
// ---------------------------------------------------------------------------

func TestNormalizePath_Rule1_Go_OsGetenv(t *testing.T) {
	r := normalizePath(`os.Getenv("API_URL") + "/foo"`)
	if r.Path != "/foo" {
		t.Errorf("path = %q, want /foo", r.Path)
	}
	if r.Props["base_url_var"] != "API_URL" {
		t.Errorf("base_url_var = %q, want API_URL", r.Props["base_url_var"])
	}
}

// ---------------------------------------------------------------------------
// Rule 2: Query string stripping
// ---------------------------------------------------------------------------

func TestNormalizePath_Rule2_QueryStringStripped(t *testing.T) {
	r := normalizePath("/buildings?foo=1&bar=2")
	if r.Path != "/buildings" {
		t.Errorf("path = %q, want /buildings", r.Path)
	}
	if r.Props["query_template"] != "foo=1&bar=2" {
		t.Errorf("query_template = %q, want foo=1&bar=2", r.Props["query_template"])
	}
}

func TestNormalizePath_Rule2_QueryStringEmptyPath(t *testing.T) {
	// e.g. "?search=foo" with no path prefix
	r := normalizePath("?search=foo")
	// empty path before ? should become "/"
	if r.Path != "/" {
		t.Errorf("path = %q, want /", r.Path)
	}
	if r.Props["query_template"] != "search=foo" {
		t.Errorf("query_template = %q, want search=foo", r.Props["query_template"])
	}
}

func TestNormalizePath_Rule2_NoQueryString_NoProps(t *testing.T) {
	r := normalizePath("/api/v1/users")
	if _, ok := r.Props["query_template"]; ok {
		t.Errorf("unexpected query_template prop for path without query string")
	}
}

// ---------------------------------------------------------------------------
// Rule 3: Duplicate slash collapsing
// ---------------------------------------------------------------------------

func TestNormalizePath_Rule3_DuplicateSlashesCollapsed(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"/api//foo", "/api/foo"},
		{"/api///v1//users", "/api/v1/users"},
		{"//buildings", "/buildings"},
	}
	for _, tc := range tests {
		r := normalizePath(tc.in)
		if r.Path != tc.want {
			t.Errorf("normalizePath(%q).Path = %q, want %q", tc.in, r.Path, tc.want)
		}
	}
}

func TestNormalizePath_Rule3_URLSchemePreserved(t *testing.T) {
	// https:// in an absolute URL must NOT be collapsed to https:/
	r := normalizePath("https://example.com/api/health")
	if r.Path != "https://example.com/api/health" {
		t.Errorf("path = %q, want https://example.com/api/health", r.Path)
	}
	if len(r.Props) != 0 {
		t.Errorf("unexpected props: %v", r.Props)
	}
}

// ---------------------------------------------------------------------------
// Rule 4: Template-literal ${name} → {name}
// ---------------------------------------------------------------------------

func TestNormalizePath_Rule4_TemplateLiteralParam_SimpleIdent(t *testing.T) {
	r := normalizePath("/users/${id}/profile")
	if r.Path != "/users/{id}/profile" {
		t.Errorf("path = %q, want /users/{id}/profile", r.Path)
	}
}

func TestNormalizePath_Rule4_TemplateLiteralParam_DottedExpr(t *testing.T) {
	// ${user.id} → {id}
	r := normalizePath("/users/${user.id}/profile")
	if r.Path != "/users/{id}/profile" {
		t.Errorf("path = %q, want /users/{id}/profile", r.Path)
	}
}

func TestNormalizePath_Rule4_TemplateLiteralParam_OptionalChain(t *testing.T) {
	// ${user?.id} → {id}
	r := normalizePath("/users/${user?.id}")
	if r.Path != "/users/{id}" {
		t.Errorf("path = %q, want /users/{id}", r.Path)
	}
}

func TestNormalizePath_Rule4_TemplateLiteralParam_TypeScriptCast(t *testing.T) {
	// ${userId as string} → {userId}
	r := normalizePath("/users/${userId as string}/items")
	if r.Path != "/users/{userId}/items" {
		t.Errorf("path = %q, want /users/{userId}/items", r.Path)
	}
}

func TestNormalizePath_Rule4_TemplateLiteralParam_MultipleParams(t *testing.T) {
	r := normalizePath("/users/${userId}/items/${itemId}")
	if r.Path != "/users/{userId}/items/{itemId}" {
		t.Errorf("path = %q, want /users/{userId}/items/{itemId}", r.Path)
	}
}

func TestNormalizePath_Rule4_TemplateLiteralParam_ComplexExpr_Fallback(t *testing.T) {
	// Complex expressions that can't be simplified → {param}
	r := normalizePath("/items/${getItemId()}/details")
	if r.Path != "/items/{param}/details" {
		t.Errorf("path = %q, want /items/{param}/details", r.Path)
	}
}

// ---------------------------------------------------------------------------
// Combined rules
// ---------------------------------------------------------------------------

func TestNormalizePath_Combined_EnvVarPlusTemplateLiteralParam(t *testing.T) {
	// ${VITE_API}/users/${userId} — ${VITE_API} is a plain ALL_CAPS ident, not stripped
	// at normalizePath level; both ${} are expanded by Rule 4.
	r := normalizePath("${VITE_API}/users/${userId}")
	if r.Path != "{VITE_API}/users/{userId}" {
		t.Errorf("path = %q, want {VITE_API}/users/{userId}", r.Path)
	}
	// No base_url_var for plain ALL_CAPS (only process.env.* / import.meta.env.*)
	if r.Props["base_url_var"] != "" {
		t.Errorf("base_url_var should be empty, got %q", r.Props["base_url_var"])
	}
}

func TestNormalizePath_Combined_ProcessEnvPlusQueryString(t *testing.T) {
	// Explicit process.env. prefix IS stripped by Rule 1.
	r := normalizePath("${process.env.API_URL}/buildings?active=true")
	if r.Path != "/buildings" {
		t.Errorf("path = %q, want /buildings", r.Path)
	}
	if r.Props["base_url_var"] != "process.env.API_URL" {
		t.Errorf("base_url_var = %q, want process.env.API_URL", r.Props["base_url_var"])
	}
	if r.Props["query_template"] != "active=true" {
		t.Errorf("query_template = %q, want active=true", r.Props["query_template"])
	}
}

func TestNormalizePath_Combined_DuplicateSlashPlusQueryString(t *testing.T) {
	r := normalizePath("/api//v1/buildings?page=2")
	if r.Path != "/api/v1/buildings" {
		t.Errorf("path = %q, want /api/v1/buildings", r.Path)
	}
	if r.Props["query_template"] != "page=2" {
		t.Errorf("query_template = %q, want page=2", r.Props["query_template"])
	}
}

// ---------------------------------------------------------------------------
// expandTemplateLiteralParams (unit)
// ---------------------------------------------------------------------------

func TestExpandTemplateLiteralParams_NoSubst(t *testing.T) {
	p := expandTemplateLiteralParams("/buildings/{id}")
	if p != "/buildings/{id}" {
		t.Errorf("got %q", p)
	}
}

func TestExpandTemplateLiteralParams_Simple(t *testing.T) {
	p := expandTemplateLiteralParams("/buildings/${buildingId}/units")
	if p != "/buildings/{buildingId}/units" {
		t.Errorf("got %q", p)
	}
}

func TestExpandTemplateLiteralParams_AlreadyOpenAPI(t *testing.T) {
	// Pure OpenAPI-style params should not be affected by this step.
	p := expandTemplateLiteralParams("/users/{pk}/profile")
	if p != "/users/{pk}/profile" {
		t.Errorf("got %q", p)
	}
}

// ---------------------------------------------------------------------------
// extractEnvVarPrefix (unit)
// ---------------------------------------------------------------------------

func TestExtractEnvVarPrefix_NoPrefix(t *testing.T) {
	path, varName := extractEnvVarPrefix("/buildings")
	if path != "/buildings" || varName != "" {
		t.Errorf("got path=%q varName=%q", path, varName)
	}
}

func TestExtractEnvVarPrefix_JSTemplate_ImportMeta(t *testing.T) {
	path, varName := extractEnvVarPrefix("${import.meta.env.VITE_X}/buildings")
	if path != "/buildings" {
		t.Errorf("path = %q, want /buildings", path)
	}
	if varName != "import.meta.env.VITE_X" {
		t.Errorf("varName = %q, want import.meta.env.VITE_X", varName)
	}
}

func TestExtractEnvVarPrefix_Python_OsEnviron(t *testing.T) {
	path, varName := extractEnvVarPrefix(`os.environ["BASE"] + "/api"`)
	if path != "/api" {
		t.Errorf("path = %q, want /api", path)
	}
	if varName != "BASE" {
		t.Errorf("varName = %q, want BASE", varName)
	}
}

// ---------------------------------------------------------------------------
// mergeNormalizeProps (unit)
// ---------------------------------------------------------------------------

func TestMergeNormalizeProps_MergesNew(t *testing.T) {
	dst := map[string]string{"verb": "GET"}
	src := map[string]string{"base_url_var": "VITE_X", "query_template": "foo=1"}
	mergeNormalizeProps(dst, src)
	if dst["base_url_var"] != "VITE_X" {
		t.Error("base_url_var not merged")
	}
	if dst["query_template"] != "foo=1" {
		t.Error("query_template not merged")
	}
	if dst["verb"] != "GET" {
		t.Error("existing verb overwritten")
	}
}

func TestMergeNormalizeProps_DoesNotOverwrite(t *testing.T) {
	dst := map[string]string{"base_url_var": "EXISTING"}
	src := map[string]string{"base_url_var": "NEW"}
	mergeNormalizeProps(dst, src)
	if dst["base_url_var"] != "EXISTING" {
		t.Errorf("existing value overwritten: got %q", dst["base_url_var"])
	}
}

// ---------------------------------------------------------------------------
// isPlainIdentifier (unit)
// ---------------------------------------------------------------------------

func TestIsPlainIdentifier(t *testing.T) {
	trueCases := []string{"id", "userId", "_id", "$id", "buildingId", "ID", "ABC123"}
	for _, s := range trueCases {
		if !isPlainIdentifier(s) {
			t.Errorf("isPlainIdentifier(%q) = false, want true", s)
		}
	}

	falseCases := []string{"", "123abc", "user.id", "user?.id", "a-b", "a b"}
	for _, s := range falseCases {
		if isPlainIdentifier(s) {
			t.Errorf("isPlainIdentifier(%q) = true, want false", s)
		}
	}
}

// ---------------------------------------------------------------------------
// stripOuterQuotes (unit)
// ---------------------------------------------------------------------------

func TestStripOuterQuotes(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{`"/path"`, "/path"},
		{`'/path'`, "/path"},
		{`/path`, "/path"},
		{`""`, ""},
		{`"`, `"`},
		{`  "/path"  `, "/path"},
	}
	for _, tc := range tests {
		got := stripOuterQuotes(tc.in)
		if got != tc.want {
			t.Errorf("stripOuterQuotes(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Idempotence tests — normalizePath applied twice must equal once.
//
// These ensure that the normalizer's output is already in a stable canonical
// form and that running again on already-normalized output has no further
// effect. This is the "closed-form" contract from issue #819: both consumer
// and producer paths pass through normalizePath before entity-ID computation,
// so the function MUST be idempotent to guarantee they can meet at the same ID.
// ---------------------------------------------------------------------------

// TestNormalizePath_Idempotence_Rule1_ProcessEnv — env-var stripping must not
// double-strip or corrupt the path when applied to already-normalized output.
func TestNormalizePath_Idempotence_Rule1_ProcessEnv(t *testing.T) {
	// First pass: strips the env-var prefix.
	first := normalizePath("${process.env.API_URL}/users")
	// Second pass: should be a no-op on the already-normalized path.
	second := normalizePath(first.Path)
	if first.Path != second.Path {
		t.Errorf("Rule1 idempotence: first=%q second=%q — not stable", first.Path, second.Path)
	}
}

// TestNormalizePath_Idempotence_Rule1_ImportMetaEnv — same check for import.meta.env.
func TestNormalizePath_Idempotence_Rule1_ImportMetaEnv(t *testing.T) {
	first := normalizePath("${import.meta.env.VITE_BASE}/api/v1/users")
	second := normalizePath(first.Path)
	if first.Path != second.Path {
		t.Errorf("Rule1 idempotence (import.meta.env): first=%q second=%q", first.Path, second.Path)
	}
}

// TestNormalizePath_Idempotence_Rule1_Python — Python os.environ stripping.
func TestNormalizePath_Idempotence_Rule1_Python(t *testing.T) {
	first := normalizePath(`os.environ['API_URL'] + '/buildings'`)
	second := normalizePath(first.Path)
	if first.Path != second.Path {
		t.Errorf("Rule1 Python idempotence: first=%q second=%q", first.Path, second.Path)
	}
}

// TestNormalizePath_Idempotence_Rule2_QueryString — query-string stripping must
// be idempotent: the cleaned path has no '?' so second pass changes nothing.
func TestNormalizePath_Idempotence_Rule2_QueryString(t *testing.T) {
	first := normalizePath("/buildings?foo=1&bar=2")
	second := normalizePath(first.Path)
	if first.Path != second.Path {
		t.Errorf("Rule2 idempotence: first=%q second=%q", first.Path, second.Path)
	}
	// After second pass no new query_template should appear.
	if _, ok := second.Props["query_template"]; ok {
		t.Errorf("Rule2 idempotence: unexpected query_template on second pass: %v", second.Props)
	}
}

// TestNormalizePath_Idempotence_Rule3_DuplicateSlashes — collapsed slashes must
// already be in their final form after the first pass.
func TestNormalizePath_Idempotence_Rule3_DuplicateSlashes(t *testing.T) {
	inputs := []string{"/api//foo", "/api///v1//users", "//buildings"}
	for _, in := range inputs {
		first := normalizePath(in)
		second := normalizePath(first.Path)
		if first.Path != second.Path {
			t.Errorf("Rule3 idempotence (%q): first=%q second=%q", in, first.Path, second.Path)
		}
	}
}

// TestNormalizePath_Idempotence_Rule4_TemplateLiteralParams — ${name} → {name}
// must be stable: {name} has no ${ so the second pass must not alter it.
func TestNormalizePath_Idempotence_Rule4_TemplateLiteralParams(t *testing.T) {
	inputs := []string{
		"/users/${id}/profile",
		"/users/${user.id}/profile",
		"/users/${user?.id}",
		"/users/${userId as string}/items",
		"/users/${userId}/items/${itemId}",
		"/items/${getItemId()}/details",
	}
	for _, in := range inputs {
		first := normalizePath(in)
		second := normalizePath(first.Path)
		if first.Path != second.Path {
			t.Errorf("Rule4 idempotence (%q): first=%q second=%q", in, first.Path, second.Path)
		}
	}
}

// ---------------------------------------------------------------------------
// Symmetry / canonical-form contract tests — issue #819.
//
// normalizePath MUST produce the same canonical output for path strings that
// represent the same logical endpoint. This ensures that a consumer-extracted
// path and the corresponding producer-extracted path resolve to the same entity
// ID even when written with slightly different surface forms.
// ---------------------------------------------------------------------------

// TestNormalizePath_Symmetry_SlashVariants — trailing-slash variants must
// produce the same path segment before the trailing slash is later stripped by
// httproutes.Canonicalize. The Rule 3 duplicate-slash collapser must not
// introduce asymmetry between "/foo" and "/foo/".
func TestNormalizePath_Symmetry_SlashVariants(t *testing.T) {
	// Both forms are already "canonical" — normalizePath should leave the
	// trailing slash alone (Canonicalize owns that step). The key invariant
	// is that the path component is the same.
	withoutTrail := normalizePath("/buildings")
	withTrail := normalizePath("/buildings/")
	// Strip trailing slash from the "with" case for comparison; Canonicalize
	// would produce "/buildings" from both. We just verify that the non-slash
	// segment is identical.
	segWithout := withoutTrail.Path
	segWith := strings.TrimRight(withTrail.Path, "/")
	if segWithout != segWith {
		t.Errorf("Symmetry slash variants: /buildings=%q, /buildings/=%q — segments differ", segWithout, segWith)
	}
}

// TestNormalizePath_Symmetry_TemplateParamAlternateForms — different surface
// forms for the same path parameter (${id}, ${item.id}, ${item?.id}) should
// all normalize to the same {id} placeholder, ensuring producer/consumer IDs
// can still match via the byPath wildcard in the linker.
func TestNormalizePath_Symmetry_TemplateParamAlternateForms(t *testing.T) {
	cases := []struct {
		in        string
		wantParam string // the {x} placeholder we expect
	}{
		{"/users/${id}/profile", "{id}"},
		{"/users/${user.id}/profile", "{id}"},
		{"/users/${user?.id}/profile", "{id}"},
		{"/users/${userId as string}/profile", "{userId}"},
	}
	for _, tc := range cases {
		r := normalizePath(tc.in)
		// The normalized path must contain the expected placeholder.
		if !strings.Contains(r.Path, tc.wantParam) {
			t.Errorf("Symmetry param forms (%q): path=%q, want placeholder %q", tc.in, r.Path, tc.wantParam)
		}
		// And must NOT still contain the ${...} form.
		if strings.Contains(r.Path, "${") {
			t.Errorf("Symmetry param forms (%q): path=%q still contains ${...}", tc.in, r.Path)
		}
	}
}

// TestNormalizePath_Symmetry_EnvVarPrefixEquivalence — different env-var prefix
// forms for the same logical base URL (process.env.API_URL,
// import.meta.env.API_URL, os.environ['API_URL']) should all be stripped,
// leaving identical path tails.
func TestNormalizePath_Symmetry_EnvVarPrefixEquivalence(t *testing.T) {
	suffix := "/api/v1/users"
	cases := []string{
		"${process.env.API_URL}" + suffix,
		"${import.meta.env.API_URL}" + suffix,
		`os.environ['API_URL'] + '` + suffix + `'`,
		`os.getenv('API_URL') + '` + suffix + `'`,
		`System.getenv("API_URL") + "` + suffix + `"`,
		`os.Getenv("API_URL") + "` + suffix + `"`,
	}
	for _, in := range cases {
		r := normalizePath(in)
		if r.Path != suffix {
			t.Errorf("EnvVar prefix equivalence (%q): path=%q, want %q", in, r.Path, suffix)
		}
	}
}

// TestNormalizePath_Symmetry_DuplicateSlashEquivalence — paths that differ
// only in redundant slashes should produce the same normalized form, ensuring
// that a producer at "/api//v1/users" and a consumer at "/api/v1/users"
// resolve to the same entity ID.
func TestNormalizePath_Symmetry_DuplicateSlashEquivalence(t *testing.T) {
	canonical := "/api/v1/users"
	variants := []string{
		"/api//v1/users",
		"/api///v1/users",
		"/api/v1//users",
		"//api/v1/users",
	}
	for _, v := range variants {
		r := normalizePath(v)
		if r.Path != canonical {
			t.Errorf("DuplicateSlash equivalence (%q): path=%q, want %q", v, r.Path, canonical)
		}
	}
}

// TestNormalizePath_Symmetry_QueryStringEquivalence — paths that differ only
// in their query string should produce the same normalized path, because the
// entity ID is derived from the path only (not the query parameters).
func TestNormalizePath_Symmetry_QueryStringEquivalence(t *testing.T) {
	canonical := "/buildings"
	variants := []string{
		"/buildings?active=true",
		"/buildings?page=1&limit=20",
		"/buildings?",
	}
	for _, v := range variants {
		r := normalizePath(v)
		if r.Path != canonical {
			t.Errorf("QueryString equivalence (%q): path=%q, want %q", v, r.Path, canonical)
		}
	}
}
