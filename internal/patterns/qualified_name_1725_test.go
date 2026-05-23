// qualified_name_1725_test.go — verifies #1725 fix: SCOPE.Pattern entities
// emitted via makeEntity must have a non-empty, stable, file-scoped
// qualified_name.
package patterns

import (
	"strings"
	"testing"
)

func TestMakeEntity_QualifiedNameNonEmpty_1725(t *testing.T) {
	cases := []struct {
		name     string
		filePath string
		entName  string
		wantQN   string
	}{
		{
			name:     "python_try_catch",
			filePath: "core/serializers/deficiency_serializer.py",
			entName:  "error_handling:try_catch:179",
			wantQN:   "core.serializers.deficiency_serializer.error_handling:try_catch:179",
		},
		{
			name:     "js_try_catch",
			filePath: "src/handlers/orders.js",
			entName:  "error_handling:try_catch:42",
			wantQN:   "src.handlers.orders.error_handling:try_catch:42",
		},
		{
			name:     "go_err_nil",
			filePath: "internal/engine/detector.go",
			entName:  "error_handling:go_error_return:99",
			wantQN:   "internal.engine.detector.error_handling:go_error_return:99",
		},
		{
			name:     "leading_slash_trimmed",
			filePath: "/abs/path/file.py",
			entName:  "decorator:foo:10",
			wantQN:   "abs.path.file.decorator:foo:10",
		},
		{
			name:     "no_extension",
			filePath: "Dockerfile",
			entName:  "code_marker:TODO:1",
			wantQN:   "Dockerfile.code_marker:TODO:1",
		},
		{
			name:     "empty_path_fallback",
			filePath: "",
			entName:  "singleton:x:1",
			wantQN:   "singleton:x:1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := makeEntity(tc.filePath, tc.entName, "SCOPE.Pattern", "error_handling", "python", 1, nil)
			if e.QualifiedName == "" {
				t.Fatalf("QualifiedName empty for %s — #1725 regression", tc.name)
			}
			if e.QualifiedName != tc.wantQN {
				t.Errorf("QualifiedName mismatch: got %q, want %q", e.QualifiedName, tc.wantQN)
			}
		})
	}
}

func TestMakeEntity_QualifiedNameStable_1725(t *testing.T) {
	// Determinism: the same inputs must produce the same QN across calls.
	a := makeEntity("foo/bar.py", "pat:x:10", "SCOPE.Pattern", "sub", "python", 10, nil)
	b := makeEntity("foo/bar.py", "pat:x:10", "SCOPE.Pattern", "sub", "python", 10, nil)
	if a.QualifiedName != b.QualifiedName {
		t.Fatalf("non-deterministic QN: %q vs %q", a.QualifiedName, b.QualifiedName)
	}
	if !strings.HasPrefix(a.QualifiedName, "foo.bar.") {
		t.Errorf("expected module-prefixed QN, got %q", a.QualifiedName)
	}
}

func TestPathToModule_1725(t *testing.T) {
	cases := map[string]string{
		"":                  "",
		"a.py":              "a",
		"a/b/c.go":          "a.b.c",
		"./a/b.js":          "a.b",
		"/leading/slash.py": "leading.slash",
		"win\\sep\\file.ts": "win.sep.file",
		"no_ext":            "no_ext",
		"foo/bar.test.tsx":  "foo.bar.test",
	}
	for in, want := range cases {
		if got := pathToModule(in); got != want {
			t.Errorf("pathToModule(%q) = %q, want %q", in, got, want)
		}
	}
}
