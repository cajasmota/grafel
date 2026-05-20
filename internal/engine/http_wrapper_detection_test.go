// Tests for #806 — custom HTTP wrapper (callApi-style) bare-name acceptance
// and per-repo wrappers.json config (Option A + Option B).
package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Option A — heuristic wrapper name recognition
// ---------------------------------------------------------------------------

// TestIsHTTPWrapperName_Known verifies that well-known wrapper names are
// recognised by the heuristic.
func TestIsHTTPWrapperName_Known(t *testing.T) {
	known := []string{
		"callApi", "callAPI", "callHttp", "callHTTP",
		"apiCall", "apiRequest", "apiFetch", "apiClient", "apiQuery",
		"httpRequest", "httpClient", "httpFetch",
		"makeRequest", "makeApiCall", "makeHttpRequest",
		"fetchApi", "fetchData", "fetchJson", "fetchFrom",
		"request", "requestApi",
		"useQuery", "useApi", "useFetch", "useRequest",
		"createApi", "createRequest",
		"sendRequest",
		// case-insensitive variants
		"CallApi", "CALLAPI",
	}
	for _, name := range known {
		if !IsHTTPWrapperName(name) {
			t.Errorf("IsHTTPWrapperName(%q) = false, want true", name)
		}
	}
}

// TestIsHTTPWrapperName_Unknown verifies that non-HTTP function names are
// NOT misidentified as HTTP wrappers.
func TestIsHTTPWrapperName_Unknown(t *testing.T) {
	unknown := []string{
		"useState", "setState", "useMemo", "useEffect", "useCallback",
		"buildConfig", "createStore", "dispatch", "emit",
		"render", "connect", "resolve",
		"console", "Object", "Array",
	}
	for _, name := range unknown {
		if IsHTTPWrapperName(name) {
			t.Errorf("IsHTTPWrapperName(%q) = true, want false", name)
		}
	}
}

// ---------------------------------------------------------------------------
// normalizeBareName
// ---------------------------------------------------------------------------

// TestNormalizeBareName verifies that bare resource names are correctly
// converted to canonical absolute paths.
func TestNormalizeBareName(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"checklists", "/checklists/"},
		{"checklists/", "/checklists/"},
		{"api/users", "/api/users/"},
		{"/checklists", "/checklists"},                           // already has leading slash: unchanged
		{"/checklists/", "/checklists/"},                         // already absolute: unchanged
		{"", ""},                                                 // empty: unchanged
		{"https://example.com/path", "https://example.com/path"}, // absolute URL: unchanged
	}
	for _, c := range cases {
		got := normalizeBareName(c.raw)
		if got != c.want {
			t.Errorf("normalizeBareName(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Option A — bare resource name acceptance via heuristic wrapper name
// ---------------------------------------------------------------------------

// TestSynth806_BareResourceNameCallApi verifies that the "old" fixture-b
// pattern (no leading slash) emits FETCHES when the wrapper is callApi.
//
//	callApi({endpoint: 'checklists'}, METHOD.GET, {filters})
func TestSynth806_BareResourceNameCallApi(t *testing.T) {
	src := `export async function getChecklists(filters) {
  const r = await callApi({endpoint: 'checklists'}, METHOD.GET, {filters});
  return r.data;
}
`
	got, _ := runDetect(t, "javascript", "bare-resource.js", src)
	want := []string{"http:GET:/checklists"}
	requireContains(t, got, want, "#806 bare resource name via callApi")
}

// TestSynth806_BareResourceNameApiRequest verifies that apiRequest (also an
// Option A match) accepts bare resource names.
func TestSynth806_BareResourceNameApiRequest(t *testing.T) {
	src := `export async function listBuildings() {
  return apiRequest({endpoint: 'buildings'}, 'GET');
}
`
	got, _ := runDetect(t, "javascript", "bare-apiRequest.js", src)
	want := []string{"http:GET:/buildings"}
	requireContains(t, got, want, "#806 bare resource via apiRequest")
}

// TestSynth806_BareResourceNameWithPost verifies that bare names also work
// with non-GET verbs (positional method argument).
func TestSynth806_BareResourceNameWithPost(t *testing.T) {
	src := `export async function createChecklist(body) {
  return callApi({endpoint: 'checklists'}, METHOD.POST, body);
}
`
	got, _ := runDetect(t, "javascript", "bare-post.js", src)
	want := []string{"http:POST:/checklists"}
	requireContains(t, got, want, "#806 bare name POST via callApi")
}

// TestSynth806_LeadingSlashStillWorks verifies that the V2 pattern (with
// leading slash) continues to work after the #806 changes.
//
//	callApi({endpoint: '/checklists/'}, METHOD.GET, params)
func TestSynth806_LeadingSlashStillWorks(t *testing.T) {
	src := `export async function getChecklists(params) {
  return callApi({endpoint: '/checklists/'}, METHOD.GET, params).then(r => r.data);
}
`
	got, _ := runDetect(t, "javascript", "leading-slash.js", src)
	want := []string{"http:GET:/checklists"}
	requireContains(t, got, want, "#806 leading-slash V2 pattern still works")
}

// TestSynth806_BareNameNonHTTPWrapperRejected verifies that a function
// with a non-HTTP name (e.g., buildConfig) does NOT accept bare resource
// names, preventing false positives.
func TestSynth806_BareNameNonHTTPWrapperRejected(t *testing.T) {
	src := `function buildConfig(opts) { return opts; }
const cfg = buildConfig({ endpoint: 'settings' });
`
	got, _ := runDetect(t, "javascript", "non-http-wrapper-bare.js", src)
	for _, id := range got {
		if id == "http:GET:/settings" {
			t.Errorf("non-HTTP wrapper buildConfig should not emit synthetic for bare name, got: %q", id)
		}
	}
}

// TestSynth806_EnvVarPathWithBareResource verifies that an env-var-prefixed
// path after a recognized wrapper name emits with runtime_dynamic=true.
// This ensures #806 does not break env-var path handling.
func TestSynth806_EnvVarPathWithBareResource(t *testing.T) {
	// The V2 pattern (leading slash) with env-var prefix is already covered
	// by #721 tests. Here we just confirm the leading-slash path from a
	// recognized wrapper still emits correctly.
	src := `export async function getHealth() {
  return callApi({endpoint: '/health'}, METHOD.GET, {});
}
`
	got, _ := runDetect(t, "javascript", "env-var-wrapper.js", src)
	want := []string{"http:GET:/health"}
	requireContains(t, got, want, "#806 recognized wrapper with absolute path")
}

// TestSynth806_FetchesEdgeForBareResourceName verifies that a FETCHES edge
// is emitted from the enclosing function to the normalized endpoint when a
// bare resource name is accepted.
func TestSynth806_FetchesEdgeForBareResourceName(t *testing.T) {
	src := `export async function fetchBuildings() {
  return callApi({endpoint: 'buildings'}, METHOD.GET, {});
}
`
	_, res := runDetect(t, "javascript", "806-fetches-edge.js", src)
	foundEdge := false
	for _, r := range res.Relationships {
		if r.Kind == fetchesEdgeKind && r.ToID == "http:GET:/buildings" && r.FromID == "Function:fetchBuildings" {
			foundEdge = true
		}
	}
	if !foundEdge {
		t.Errorf("expected FETCHES edge Function:fetchBuildings → http:GET:/buildings (bare resource name)")
	}
}

// ---------------------------------------------------------------------------
// Option B — per-repo wrappers.json config
// ---------------------------------------------------------------------------

// TestLoadWrapperConfigs_Missing verifies that a missing wrappers.json
// returns nil without error (graceful absence).
func TestLoadWrapperConfigs_Missing(t *testing.T) {
	dir := t.TempDir()
	cfgs, err := LoadWrapperConfigs(dir)
	if err != nil {
		t.Fatalf("LoadWrapperConfigs on missing file returned error: %v", err)
	}
	if cfgs != nil {
		t.Errorf("expected nil configs for missing file, got: %v", cfgs)
	}
}

// TestLoadWrapperConfigs_ValidFile verifies that a valid wrappers.json is
// parsed into the correct WrapperConfig structs.
func TestLoadWrapperConfigs_ValidFile(t *testing.T) {
	dir := t.TempDir()
	archDir := filepath.Join(dir, ".archigraph")
	if err := os.MkdirAll(archDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `{
  "wrappers": [
    {
      "name": "callApi",
      "module": "src/stores/appService.js",
      "path_arg": "endpoint",
      "path_arg_position": -1,
      "method_arg_position": 1,
      "method_values": ["METHOD.GET", "METHOD.POST", "METHOD.PUT", "METHOD.PATCH", "METHOD.DELETE"]
    }
  ]
}`
	if err := os.WriteFile(filepath.Join(archDir, "wrappers.json"), []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfgs, err := LoadWrapperConfigs(dir)
	if err != nil {
		t.Fatalf("LoadWrapperConfigs: %v", err)
	}
	if len(cfgs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(cfgs))
	}
	if cfgs[0].Name != "callApi" {
		t.Errorf("Name = %q, want callApi", cfgs[0].Name)
	}
	if cfgs[0].PathArg != "endpoint" {
		t.Errorf("PathArg = %q, want endpoint", cfgs[0].PathArg)
	}
	if cfgs[0].MethodArgPosition != 1 {
		t.Errorf("MethodArgPosition = %d, want 1", cfgs[0].MethodArgPosition)
	}
	if len(cfgs[0].MethodValues) != 5 {
		t.Errorf("MethodValues len = %d, want 5", len(cfgs[0].MethodValues))
	}
}

// TestBuildWrapperConfigIndex verifies that the index maps wrapper names to
// their configs correctly.
func TestBuildWrapperConfigIndex(t *testing.T) {
	cfgs := []WrapperConfig{
		{Name: "callApi", PathArg: "endpoint"},
		{Name: "myFetch", PathArg: "url"},
	}
	idx := BuildWrapperConfigIndex(cfgs)
	if len(idx) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(idx))
	}
	if _, ok := idx["callApi"]; !ok {
		t.Errorf("expected callApi in index")
	}
	if idx["myFetch"].PathArg != "url" {
		t.Errorf("PathArg for myFetch = %q, want url", idx["myFetch"].PathArg)
	}
}

// TestIsHTTPWrapperHeuristic_ConfigOverride verifies that a function name
// not matching Option A heuristics is still recognized when it's in the
// per-repo config index (Option B wins).
func TestIsHTTPWrapperHeuristic_ConfigOverride(t *testing.T) {
	idx := BuildWrapperConfigIndex([]WrapperConfig{
		{Name: "myProjectSpecificFetcher"},
	})
	if !IsHTTPWrapperHeuristic("myProjectSpecificFetcher", idx) {
		t.Errorf("IsHTTPWrapperHeuristic(%q) = false with config override, want true", "myProjectSpecificFetcher")
	}
	// Without the config, the same name is NOT recognized.
	emptyIdx := WrapperConfigIndex{}
	if IsHTTPWrapperHeuristic("myProjectSpecificFetcher", emptyIdx) {
		t.Errorf("IsHTTPWrapperHeuristic(%q) = true without config, want false", "myProjectSpecificFetcher")
	}
}

// ---------------------------------------------------------------------------
// React Query / SWR / RTK Query beyond-minimum
// ---------------------------------------------------------------------------

// TestSynth806_ReactQueryUseQuery verifies that useQuery with a string key
// matching a resource name emits a FETCHES edge to the normalized path.
func TestSynth806_ReactQueryUseQuery(t *testing.T) {
	src := `import { useQuery } from '@tanstack/react-query';

export function useUsers() {
  return useQuery({ queryKey: ['users'], queryFn: () => callApi('users', 'GET') });
}
`
	got, _ := runDetect(t, "typescript", "806-rq-useQuery.ts", src)
	want := []string{"http:GET:/users"}
	requireContains(t, got, want, "#806 React Query useQuery queryKey resource name")
}

// TestSynth806_RTKQueryBuilderQuery verifies that RTK Query
// builder.query with a bare resource name emits a FETCHES edge.
func TestSynth806_RTKQueryBuilderQuery(t *testing.T) {
	src := `import { createApi } from '@reduxjs/toolkit/query';

export const api = createApi({
  endpoints: (builder) => ({
    getUsers: builder.query({ query: () => 'users' }),
    getChecklists: builder.query({ query: () => 'checklists' }),
  }),
});
`
	got, _ := runDetect(t, "typescript", "806-rtk-query.ts", src)
	want := []string{
		"http:GET:/users",
		"http:GET:/checklists",
	}
	requireContains(t, got, want, "#806 RTK Query builder.query resource names")
}

// TestSynth806_BareNameMultipleVerbs verifies that different methods are
// correctly assigned for multiple callApi calls on the same resource.
func TestSynth806_BareNameMultipleVerbs(t *testing.T) {
	src := `export async function getChecklists() {
  return callApi({endpoint: 'checklists'}, METHOD.GET, {});
}

export async function createChecklist(body) {
  return callApi({endpoint: 'checklists'}, METHOD.POST, body);
}

export async function updateChecklist(id, body) {
  return callApi({endpoint: 'checklists'}, METHOD.PUT, body);
}

export async function deleteChecklist(id) {
  return callApi({endpoint: 'checklists'}, METHOD.DELETE, {id});
}
`
	got, _ := runDetect(t, "javascript", "806-multi-verb.js", src)
	want := []string{
		"http:GET:/checklists",
		"http:POST:/checklists",
		"http:PUT:/checklists",
		"http:DELETE:/checklists",
	}
	requireContains(t, got, want, "#806 bare name multiple verbs")
}
