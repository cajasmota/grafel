package extractor

import (
	"path/filepath"
	"testing"
)

func TestBuildOperationStructuralRef(t *testing.T) {
	tests := []struct {
		name     string
		lang     string
		filePath string
		ident    string
		want     string
	}{
		{
			name:     "posix path",
			lang:     "python",
			filePath: "src/foo/bar.py",
			ident:    "do_thing",
			want:     "scope:operation:method:python:src/foo/bar.py:do_thing",
		},
		{
			name:     "windows path is normalized to forward slashes",
			lang:     "java",
			filePath: filepath.FromSlash("src/main/java/Foo.java"),
			ident:    "run",
			want:     "scope:operation:method:java:src/main/java/Foo.java:run",
		},
		{
			name:     "empty name",
			lang:     "ruby",
			filePath: "app/controllers/foo.rb",
			ident:    "",
			want:     "scope:operation:method:ruby:app/controllers/foo.rb:",
		},
		{
			name:     "dotted name",
			lang:     "javascript",
			filePath: "src/index.js",
			ident:    "Foo.bar",
			want:     "scope:operation:method:javascript:src/index.js:Foo.bar",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildOperationStructuralRef(tc.lang, tc.filePath, tc.ident)
			if got != tc.want {
				t.Fatalf("BuildOperationStructuralRef(%q, %q, %q) = %q, want %q",
					tc.lang, tc.filePath, tc.ident, got, tc.want)
			}
		})
	}
}
