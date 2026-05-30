package httproutes

import "testing"

// TestCanonicalize_Django covers Django path-converter forms and the
// trailing-slash-stripping convention.
func TestCanonicalize_Django(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"users/<int:id>/", "/users/{id}"},
		{"users/<slug:slug>/comments/<int:comment_id>", "/users/{slug}/comments/{comment_id}"},
		{"posts/<uuid:pk>/", "/posts/{pk}"},
		{"<name>/", "/{name}"},
		{"static-page", "/static-page"},
		{"", "/"},
		{"/", "/"},
	}
	for _, tc := range cases {
		got := Canonicalize(FrameworkDjango, tc.in)
		if got != tc.want {
			t.Errorf("Canonicalize(django, %q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestCanonicalize_Flask covers Flask converters and bare angle params.
func TestCanonicalize_Flask(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/users/<int:id>", "/users/{id}"},
		{"/files/<path:rest>", "/files/{rest}"},
		{"/<id>", "/{id}"},
		{"/users/<int:id>/posts/<int:post_id>", "/users/{id}/posts/{post_id}"},
	}
	for _, tc := range cases {
		got := Canonicalize(FrameworkFlask, tc.in)
		if got != tc.want {
			t.Errorf("Canonicalize(flask, %q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestCanonicalize_CurlyBrace covers FastAPI / Spring / JAX-RS forms,
// including Spring's `{id:[0-9]+}` regex-constraint variant.
func TestCanonicalize_CurlyBrace(t *testing.T) {
	cases := []struct {
		framework, in, want string
	}{
		{FrameworkFastAPI, "/users/{id}", "/users/{id}"},
		{FrameworkFastAPI, "/users/{user_id}/posts/{post_id}", "/users/{user_id}/posts/{post_id}"},
		{FrameworkSpring, "/users/{id:[0-9]+}", "/users/{id}"},
		{FrameworkJAXRS, "/users/{id}", "/users/{id}"},
		{FrameworkSpring, "/api/v1/things/{id:\\d+}/", "/api/v1/things/{id}"},
	}
	for _, tc := range cases {
		got := Canonicalize(tc.framework, tc.in)
		if got != tc.want {
			t.Errorf("Canonicalize(%s, %q) = %q, want %q", tc.framework, tc.in, got, tc.want)
		}
	}
}

// TestCanonicalize_Express covers Express colon-prefixed params + optional.
func TestCanonicalize_Express(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/users/:id", "/users/{id}"},
		{"/users/:id/posts/:postId", "/users/{id}/posts/{postId}"},
		{"/files/:filename?", "/files/{filename}"},
		{"/api/v1/things", "/api/v1/things"},
	}
	for _, tc := range cases {
		got := Canonicalize(FrameworkExpress, tc.in)
		if got != tc.want {
			t.Errorf("Canonicalize(express, %q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestCanonicalize_Lua covers Lapis + OpenResty colon-prefixed params and
// literal nginx location paths (#3484).
func TestCanonicalize_Lua(t *testing.T) {
	cases := []struct {
		framework, in, want string
	}{
		{FrameworkLapis, "/users/:id", "/users/{id}"},
		{FrameworkLapis, "/users/:user_id/posts/:post_id", "/users/{user_id}/posts/{post_id}"},
		{FrameworkLapis, "/about", "/about"},
		{FrameworkOpenResty, "/api/users", "/api/users"},
		{FrameworkOpenResty, "/users/:id", "/users/{id}"},
		{FrameworkOpenResty, "/health/", "/health"},
	}
	for _, tc := range cases {
		got := Canonicalize(tc.framework, tc.in)
		if got != tc.want {
			t.Errorf("Canonicalize(%s, %q) = %q, want %q", tc.framework, tc.in, got, tc.want)
		}
	}
}

// TestCanonicalize_SlashNormalisation verifies the leading-slash + no-
// trailing-slash + collapse-duplicate-slash conventions hold across edge
// cases.
func TestCanonicalize_SlashNormalisation(t *testing.T) {
	cases := []struct {
		framework, in, want string
	}{
		{FrameworkFastAPI, "/api//users//", "/api/users"},
		{FrameworkSpring, "api/users/", "/api/users"},
		{FrameworkExpress, "", "/"},
		{FrameworkJAXRS, "/", "/"},
	}
	for _, tc := range cases {
		got := Canonicalize(tc.framework, tc.in)
		if got != tc.want {
			t.Errorf("Canonicalize(%s, %q) = %q, want %q", tc.framework, tc.in, got, tc.want)
		}
	}
}

// TestCanonicalize_PythonNamedGroups covers Python regex named-group syntax
// `(?P<name>charclass)` that appears in Django `re_path` and DRF
// `@action(url_path=…)` strings. Fixes #2669: without this rewrite the
// embedded `(?P<…>…)` survived into the canonical path and broke byPath
// bucketing across producer and consumer sides.
func TestCanonicalize_PythonNamedGroups(t *testing.T) {
	cases := []struct {
		framework, in, want string
	}{
		// The triggering upvate case: DRF @action(url_path=…) on NoteViewSet.
		{
			FrameworkDjango,
			`group/(?P<group_id>[^/.]+)/entity/(?P<entity>[^/.]+)/(?P<entity_id>[^/.]+)`,
			"/group/{group_id}/entity/{entity}/{entity_id}",
		},
		{
			FrameworkDjango,
			`entity/(?P<entity>[^/.]+)/(?P<entity_id>[^/.]+)`,
			"/entity/{entity}/{entity_id}",
		},
		{
			FrameworkDjango,
			`catalogs/entity-types/(?P<entity>[^/.]+)`,
			"/catalogs/entity-types/{entity}",
		},
		// Numeric charclass.
		{FrameworkDjango, `(?P<id>\d+)`, "/{id}"},
		// Mixed with normal angle-bracket converters.
		{
			FrameworkDjango,
			`users/<int:user_id>/notes/(?P<note_id>[^/.]+)`,
			"/users/{user_id}/notes/{note_id}",
		},
		// Nested group inside the body — the outer `(?P<…>…)` must still
		// strip cleanly even when its body contains balanced inner parens.
		{FrameworkDjango, `(?P<a>(?:\d+))/x`, "/{a}/x"},
		// Flask `re_path` analogue (Flask doesn't ship one but the
		// canonicaliser shares the angle-bracket walker, so cover it).
		{FrameworkFlask, `things/(?P<thing_id>[^/.]+)`, "/things/{thing_id}"},
		// Pass-through: paths without any `(?P<` must remain identical
		// to the pre-#2669 output.
		{FrameworkDjango, "users/<int:id>/posts/<int:post_id>", "/users/{id}/posts/{post_id}"},
	}
	for _, tc := range cases {
		got := Canonicalize(tc.framework, tc.in)
		if got != tc.want {
			t.Errorf("Canonicalize(%s, %q) = %q, want %q", tc.framework, tc.in, got, tc.want)
		}
	}
}

// TestSyntheticID verifies the http:<METHOD>:<path> format and method
// upper-casing.
func TestSyntheticID(t *testing.T) {
	if got, want := SyntheticID("get", "/users/{id}"), "http:GET:/users/{id}"; got != want {
		t.Errorf("SyntheticID(get) = %q, want %q", got, want)
	}
	if got, want := SyntheticID("POST", "/users"), "http:POST:/users"; got != want {
		t.Errorf("SyntheticID(POST) = %q, want %q", got, want)
	}
	if got, want := SyntheticID("Any", "/"), "http:ANY:/"; got != want {
		t.Errorf("SyntheticID(Any) = %q, want %q", got, want)
	}
}
