package main

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// TestSupportedLanguagesEmptyRepo confirms that pointing the helper at a
// directory with no internal/extractors/ tree returns an empty slice
// (never nil) so callers can iterate unconditionally.
func TestSupportedLanguagesEmptyRepo(t *testing.T) {
	got := SupportedLanguages(t.TempDir())
	if got == nil {
		t.Fatalf("expected non-nil slice")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %v", got)
	}
}

// TestSupportedLanguagesAliasingAndExcludes builds a synthetic
// internal/extractors/ tree containing language dirs, utility dirs,
// non-language formats, and aliased dirs (javascript+typescript collapse
// to jsts, golang→go). Verifies the returned list is sorted, unique,
// excludes utility/non-language formats, and applies aliases.
func TestSupportedLanguagesAliasingAndExcludes(t *testing.T) {
	root := t.TempDir()
	extractorsRoot := filepath.Join(root, "internal", "extractors")
	dirs := []string{
		"python",
		"ruby",
		"javascript",
		"typescript",
		"golang",
		"haskell",
		"cpp",
		"complexity",
		"config",
		"cross",
		"references",
		"sresolver",
		"yaml",
		"bazel",
		"dockerfile",
		"hcl",
		"shell",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(extractorsRoot, d), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	// A regular file inside extractors/ must be ignored.
	if err := os.WriteFile(filepath.Join(extractorsRoot, "registry.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	got := SupportedLanguages(root)
	want := []string{"c-cpp", "go", "haskell", "jsts", "python", "ruby"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SupportedLanguages mismatch:\nwant %v\n got %v", want, got)
	}
	if !sort.StringsAreSorted(got) {
		t.Fatalf("result not sorted: %v", got)
	}
}

// TestLanguageDisplayName covers the override table and the title-case
// fallback used by placeholder pages.
func TestLanguageDisplayName(t *testing.T) {
	cases := []struct {
		slug string
		want string
	}{
		{"jsts", "JS/TS"},
		{"csharp", "C#"},
		{"c-cpp", "C/C++"},
		{"fsharp", "F#"},
		{"reasonml", "ReasonML"},
		{"rescript", "ReScript"},
		{"sml", "Standard ML"},
		{"ocaml", "OCaml"},
		{"vhdl", "VHDL"},
		{"haskell", "Haskell"},
		{"zig", "Zig"},
		{"", ""},
	}
	for _, c := range cases {
		if got := languageDisplayName(c.slug); got != c.want {
			t.Errorf("languageDisplayName(%q) = %q; want %q", c.slug, got, c.want)
		}
	}
}

// TestExtractorDirForSlug pins the inverse alias map used to cite a
// concrete on-disk extractor directory on placeholder pages.
func TestExtractorDirForSlug(t *testing.T) {
	cases := map[string]string{
		"jsts":    "javascript",
		"go":      "golang",
		"c-cpp":   "cpp",
		"haskell": "haskell",
		"zig":     "zig",
	}
	for slug, want := range cases {
		if got := extractorDirForSlug(slug); got != want {
			t.Errorf("extractorDirForSlug(%q) = %q; want %q", slug, got, want)
		}
	}
}
