// Package engine — HTTP wrapper detection helpers (issue #806).
//
// This file implements two complementary mechanisms to identify custom
// HTTP wrapper functions that use non-standard call signatures and
// non-slash-prefixed resource names:
//
// Option A — heuristic name-based recognition:
//
//	A function is treated as an HTTP wrapper when its name matches a set
//	of well-known patterns (callApi, apiCall, apiRequest, httpRequest,
//	request, api, fetch*, useQuery, etc., case-insensitive). When the
//	wrapper name is recognized, the path argument is accepted even if it
//	does not start with `/`, and the raw resource name is normalized to a
//	canonical absolute path.
//
// Option B — per-repo `.archigraph/wrappers.json` config:
//
//	Projects with unusual wrapper signatures can list their wrappers in
//	.archigraph/wrappers.json. Declared wrappers are treated as
//	HTTP-aware regardless of Option A's heuristics and can specify which
//	argument position or object-literal key holds the path.
//
// Bare-name normalization:
//
//	When a resource name like "checklists" is accepted (no leading /),
//	it is normalized to "/checklists/" so the entity ID matches the
//	slash-normalized form expected by the cross-repo linker. If #807
//	(path normalization) is landed, its normalization function is used;
//	otherwise basic leading+trailing slash insertion is applied here.
package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Option A — heuristic wrapper-name recognition
// ---------------------------------------------------------------------------

// httpWrapperNameRe matches function names that are conventionally used
// as HTTP wrapper utilities. The match is case-insensitive. When a call
// site's function name matches this pattern we relax the path-recognition
// rules to accept bare resource names (no leading /).
//
// Covered patterns (representative, not exhaustive):
//   - callApi, apiCall, callAPI, callHTTP
//   - apiRequest, httpRequest, makeRequest
//   - request, api, http (bare single-word utilities)
//   - fetch, fetchAPI, fetchJson (fetch-derived names)
//   - useQuery, useApi, useFetch (React Query / SWR key-fn form)
//   - createApi (RTK Query builder form)
//   - getResource, postResource, etc. (verb-prefixed helpers)
var httpWrapperNameRe = regexp.MustCompile(
	`(?i)^(?:` +
		`call(?:api|http|request|fetch)|` + // callApi, callHTTP, callRequest, callFetch
		`api(?:call|request|fetch|client|query)?|` + // api, apiCall, apiRequest, apiClient, apiQuery
		`http(?:request|client|fetch|call)?|` + // http, httpRequest, httpClient, httpFetch
		`make(?:request|apicall|httprequest)|` + // makeRequest, makeApiCall, makeHttpRequest
		`fetch(?:api|json|data|resource|from)?|` + // fetch, fetchApi, fetchData, fetchFrom
		`request(?:api|http|data)?|` + // request, requestApi, requestData
		`use(?:query|api|fetch|request)|` + // useQuery, useApi, useFetch, useRequest (React Query / SWR)
		`create(?:api|request)|` + // createApi (RTK Query), createRequest
		`send(?:request|http)?|` + // sendRequest, sendHttp
		`get(?:resource|data|from)|` + // getResource, getData, getFrom (verb-prefixed helpers)
		`post(?:resource|data|to)|` + // postResource, postData
		`put(?:resource|data)|` + // putResource, putData
		`patch(?:resource|data)|` + // patchResource
		`delete(?:resource|data)` + // deleteResource
		`)$`,
)

// IsHTTPWrapperName returns true when the function name looks like a
// project-specific HTTP wrapper utility (Option A heuristic).
func IsHTTPWrapperName(name string) bool {
	return httpWrapperNameRe.MatchString(name)
}

// normalizeBareName converts a bare resource name (no leading /) to a
// canonical absolute path with a leading slash.
//
//   - "checklists"  → "/checklists/"
//   - "checklists/" → "/checklists/"
//   - "/checklists" → "/checklists"    (already has leading slash: unchanged)
//   - "api/users"   → "/api/users/"
//
// The trailing slash is added so the resulting path matches the
// slash-normalised form used by the server-side synthesizer (#534).
// If #807 (path normalization) is active, its CanonicalPath function
// should be applied after this step — normalizeBareName only ensures the
// leading slash so downstream functions accept it.
func normalizeBareName(raw string) string {
	if strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	if raw == "" {
		return raw
	}
	// Add leading slash; preserve trailing slash if already present.
	if strings.HasSuffix(raw, "/") {
		return "/" + raw
	}
	return "/" + raw + "/"
}

// ---------------------------------------------------------------------------
// Option B — per-repo .archigraph/wrappers.json config
// ---------------------------------------------------------------------------

// WrapperConfig describes a single custom HTTP wrapper function declared
// in the per-repo .archigraph/wrappers.json file.
//
// JSON schema:
//
//	{
//	  "wrappers": [
//	    {
//	      "name": "callApi",
//	      "module": "src/stores/appService.js",   // optional; for disambiguation
//	      "path_arg": "endpoint",                  // object-literal key OR positional index as string
//	      "path_arg_position": -1,                 // -1 = object-arg with path_arg field; 0+ = positional
//	      "method_arg_position": 1,                // 0-based positional index for the HTTP verb
//	      "method_values": ["METHOD.GET", ...]     // optional; dotted constants for verb resolution
//	    }
//	  ]
//	}
type WrapperConfig struct {
	Name              string   `json:"name"`
	Module            string   `json:"module,omitempty"`
	PathArg           string   `json:"path_arg,omitempty"`
	PathArgPosition   int      `json:"path_arg_position,omitempty"`
	MethodArgPosition int      `json:"method_arg_position,omitempty"`
	MethodValues      []string `json:"method_values,omitempty"`
}

// wrapperConfigFile is the JSON envelope that holds the list of declared
// wrapper configs.
type wrapperConfigFile struct {
	Wrappers []WrapperConfig `json:"wrappers"`
}

// LoadWrapperConfigs reads .archigraph/wrappers.json from the given repo
// root directory and returns the list of declared wrapper configs.
// Returns nil (no error) if the file doesn't exist — a missing config is
// normal for repos that rely on heuristic detection only.
func LoadWrapperConfigs(repoRoot string) ([]WrapperConfig, error) {
	path := filepath.Join(repoRoot, ".archigraph", "wrappers.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg wrapperConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return cfg.Wrappers, nil
}

// WrapperConfigIndex is a fast-lookup map from wrapper name →
// WrapperConfig. Built once per indexing run from LoadWrapperConfigs.
type WrapperConfigIndex map[string]WrapperConfig

// BuildWrapperConfigIndex constructs an index from a list of wrapper
// configs. The last declaration wins if the same name appears twice.
func BuildWrapperConfigIndex(cfgs []WrapperConfig) WrapperConfigIndex {
	idx := make(WrapperConfigIndex, len(cfgs))
	for _, c := range cfgs {
		idx[c.Name] = c
	}
	return idx
}

// IsHTTPWrapperHeuristic returns true when the function name is recognized
// as an HTTP wrapper via either:
//  1. The per-repo config index (Option B — always wins if present)
//  2. The heuristic name pattern (Option A — fallback)
func IsHTTPWrapperHeuristic(name string, idx WrapperConfigIndex) bool {
	if _, ok := idx[name]; ok {
		return true
	}
	return IsHTTPWrapperName(name)
}

// ---------------------------------------------------------------------------
// React Query / SWR / RTK Query beyond-minimum patterns
// ---------------------------------------------------------------------------

// rtkQueryEndpointRe matches RTK Query createApi endpoint builder patterns:
//
//	createApi({ endpoints: builder => ({
//	  getUsers: builder.query({ query: () => 'users' })
//	  createUser: builder.mutation({ query: () => 'users' })
//	})})
//
// Capture groups:
//
//	1 = builder method ("query" or "mutation")
//	2 = endpoint resource name (e.g. 'users')
//
// rtkQueryEndpointRe's resource group is consumed live by
// synthesizeReactQueryCalls in http_endpoint_client_synthesis.go.
var rtkQueryEndpointRe = regexp.MustCompile(
	`\bbuilder\s*\.\s*(query|mutation)\s*\(\s*\{[^}]*query\s*:\s*\(\s*\)\s*=>\s*['"]([^'"]+)['"]`,
)
