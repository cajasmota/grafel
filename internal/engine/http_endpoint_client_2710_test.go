// Unit tests for #2710 — query-string segment stripping in HTTP path extraction.
//
// The JS/TS extractor should strip query-string portions from canonical paths
// when they appear in template literals or static URLs. The query string itself
// is stashed in Properties["query_string_template"] for telemetry/ranking only.
//
// This ensures canonical paths on both sides of the cross-repo match (producer
// and consumer) exclude query strings, improving match quality and enabling
// exact matches for endpoints whose call sites happen to include query params.
package engine

import (
	"testing"
)

// TestSynth_QueryStringStrip_TemplateLiteral verifies that query-string segments
// are stripped from template-literal URLs in axios/fetch calls.
func TestSynth_QueryStringStrip_TemplateLiteral(t *testing.T) {
	src := `import axios from 'axios';

export async function getDevices() {
  const queryParams = new URLSearchParams({ filter: 'active' });
  // Query string with interpolation: /devices?${queryParams.toString()}
  // Expected canonical path: /devices (query string stripped)
  const response = await axios.get(` + "`" + `/devices?${queryParams.toString()}` + "`" + `);
  return response.data;
}

export async function searchUsers(term) {
  // Mixed static query params and interpolation: /users?q=${term}&limit=10
  // Expected canonical path: /users (query string stripped)
  const response = await axios.get(` + "`" + `/users?q=${term}&limit=10` + "`" + `);
  return response.data;
}

export async function fetchContracts(id, version) {
  // Path parameter + query string: /contracts/${id}?v=${version}
  // Expected canonical path: /contracts/{id} (query string stripped)
  const response = await fetch(` + "`" + `/contracts/${id}?v=${version}` + "`" + `);
  return response.json();
}
`

	_, res := runDetect(t, "javascript", "devicesService.js", src)

	// Verify that the canonical paths are extracted WITHOUT the query string.
	type pp struct{ verb, path, framework string }
	var seen []pp
	for _, e := range res.Entities {
		if e.Kind != httpEndpointCallKind {
			continue
		}
		if e.Properties == nil {
			continue
		}
		// Collect (verb, path, framework) from the synthetic.
		seen = append(seen, pp{
			verb:      e.Properties["verb"],
			path:      e.Properties["path"],
			framework: e.Properties["framework"],
		})
	}

	// Expected canonical paths (without query strings).
	want := []pp{
		{"GET", "/devices", "axios"},
		{"GET", "/users", "axios"},
		{"GET", "/contracts/{id}", "fetch"},
	}

	for _, w := range want {
		found := false
		for _, s := range seen {
			if s == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing %s %s (%s); got: %+v", w.verb, w.path, w.framework, seen)
		}
	}
}

// TestSynth_QueryStringStrip_StaticLiteral verifies that query-string stripping
// also applies to static string literals (handled by normalizeRawClientPath).
func TestSynth_QueryStringStrip_StaticLiteral(t *testing.T) {
	src := `import axios from 'axios';

export async function getUsers() {
  // Static string with query string: /users?limit=100
  // Expected canonical path: /users (query string stripped)
  const response = await axios.get('/users?limit=100');
  return response.data;
}

export async function fetchBuildings() {
  // Static fetch URL with query string: /api/v1/buildings?format=json
  // Expected canonical path: /api/v1/buildings (query string stripped)
  const response = await fetch('/api/v1/buildings?format=json');
  return response.json();
}
`

	_, res := runDetect(t, "javascript", "apiClient.js", src)

	type pp struct{ verb, path string }
	var seen []pp
	for _, e := range res.Entities {
		if e.Kind != httpEndpointCallKind {
			continue
		}
		if e.Properties == nil {
			continue
		}
		seen = append(seen, pp{
			verb: e.Properties["verb"],
			path: e.Properties["path"],
		})
	}

	want := []pp{
		{"GET", "/users"},
		{"GET", "/api/v1/buildings"},
	}

	for _, w := range want {
		found := false
		for _, s := range seen {
			if s == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing %s %s; got: %+v", w.verb, w.path, seen)
		}
	}
}

// TestSynth_QueryStringStrip_NoQueryString verifies that paths without query
// strings pass through unchanged (regression test).
func TestSynth_QueryStringStrip_NoQueryString(t *testing.T) {
	src := `import axios from 'axios';

export async function getProfile(id) {
  // Path parameter, no query string: /users/${id}/profile
  // Expected canonical path: /users/{id}/profile (unchanged)
  const response = await axios.get(` + "`" + `/users/${id}/profile` + "`" + `);
  return response.data;
}

export async function listItems() {
  // Static path, no query string: /api/items
  // Expected canonical path: /api/items (unchanged)
  const response = await fetch('/api/items');
  return response.json();
}
`

	_, res := runDetect(t, "javascript", "utils.js", src)

	type pp struct{ verb, path string }
	var seen []pp
	for _, e := range res.Entities {
		if e.Kind != httpEndpointCallKind {
			continue
		}
		if e.Properties == nil {
			continue
		}
		seen = append(seen, pp{
			verb: e.Properties["verb"],
			path: e.Properties["path"],
		})
	}

	want := []pp{
		{"GET", "/users/{id}/profile"},
		{"GET", "/api/items"},
	}

	for _, w := range want {
		found := false
		for _, s := range seen {
			if s == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing %s %s; got: %+v", w.verb, w.path, seen)
		}
	}
}

// TestSynth_QueryStringStrip_OptionalChain verifies that optional-chaining
// operators inside template params (${user?.id}) are not confused with query
// strings (which also use ?).
func TestSynth_QueryStringStrip_OptionalChain(t *testing.T) {
	src := `import axios from 'axios';

export async function getUserData(user) {
  // Template param with optional chain + query string.
  // /users/${user?.id}?expand=true
  // Expected canonical path: /users/{id} (optional chain converted, query stripped)
  const response = await axios.get(` + "`" + `/users/${user?.id}?expand=true` + "`" + `);
  return response.data;
}
`

	_, res := runDetect(t, "javascript", "userService.js", src)

	type pp struct{ verb, path string }
	var seen []pp
	for _, e := range res.Entities {
		if e.Kind != httpEndpointCallKind {
			continue
		}
		if e.Properties == nil {
			continue
		}
		seen = append(seen, pp{
			verb: e.Properties["verb"],
			path: e.Properties["path"],
		})
	}

	want := []pp{
		{"GET", "/users/{id}"},
	}

	for _, w := range want {
		found := false
		for _, s := range seen {
			if s == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing %s %s; got: %+v", w.verb, w.path, seen)
		}
	}
}
