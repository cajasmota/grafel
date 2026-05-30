package cpp

// helpers_routes_test.go — white-box table test for cppNormalizeRoutePath.
// In-package (package cpp) so it can call the unexported normaliser directly.

import "testing"

func TestCppNormalizeRoutePath(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"colon single", "/users/:id", "/users/{id}"},
		{"colon multi", "/orders/:oid/items/:iid", "/orders/{oid}/items/{iid}"},
		{"angle typed unnamed", "/users/<int>", "/users/{int}"},
		{"angle typed named", "/users/<int:id>", "/users/{id}"},
		{"angle string and path", "/files/<string>/<path>", "/files/{string}/{path}"},
		{"brace already canonical", "/users/{id}", "/users/{id}"},
		{"brace with whitespace", "/users/{ id }", "/users/{id}"},
		{"brace glob marker", "/files/{*rest}", "/files/{rest}"},
		{"static no params", "/api/health", "/api/health"},
		{"empty", "", ""},
		{"glob star preserved", "*", "*"},
		{"sentinel listener preserved", "<listener>", "<listener>"},
		{"sentinel resource preserved", "<resource>", "<resource>"},
		{"full url with param", "http://localhost:8080/api/users/:id", "http://localhost:8080/api/users/{id}"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := cppNormalizeRoutePath(c.in)
			if got != c.want {
				t.Errorf("cppNormalizeRoutePath(%q) = %q, want %q", c.in, got, c.want)
			}
			// Idempotency: normalising the output again must be a no-op.
			if again := cppNormalizeRoutePath(got); again != got {
				t.Errorf("not idempotent: cppNormalizeRoutePath(%q) = %q", got, again)
			}
		})
	}
}
