package resolve

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #49: file paths flowing into the resolver may arrive in OS-native
// form (backslashes on Windows) while structural-ref stubs are graph
// identifiers and must use forward slashes. The resolver normalises both
// sides so a Windows-emitted entity index can be queried by a stub
// produced on either platform.
//
// These tests exercise the path-normalisation logic deterministically on
// every platform by constructing inputs through filepath.FromSlash, which
// is a no-op on POSIX and converts to backslashes on Windows. The
// behavioural assertion (that lookup succeeds despite the mismatch) is
// the same on both platforms.

func TestNormalizePath_AlwaysForwardSlash(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"src/foo/bar.py", "src/foo/bar.py"},
		// FromSlash gives us a host-appropriate input; the round trip
		// through normalizePath must always end in slash form.
		{filepath.FromSlash("src/foo/bar.py"), "src/foo/bar.py"},
		{filepath.FromSlash("a/b/c"), "a/b/c"},
	}
	for _, tc := range cases {
		got := normalizePath(tc.in)
		if got != tc.want {
			t.Fatalf("normalizePath(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if strings.ContainsRune(got, '\\') {
			t.Fatalf("normalizePath(%q) leaked a backslash: %q", tc.in, got)
		}
	}
}

// TestBuildIndex_WindowsSourceFile_StructuralRefResolves verifies that an
// entity indexed under an OS-native path (FromSlash form) is discoverable
// via a structural-ref stub that uses forward-slash form. This is the
// concrete failure mode reported in issue #49.
func TestBuildIndex_WindowsSourceFile_StructuralRefResolves(t *testing.T) {
	winPath := filepath.FromSlash("src/foo/bar.py")
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "SCOPE.Component", "User", winPath),
	}
	idx := BuildIndex(entities)

	// Structural-ref Format A stub — always forward-slash, regardless of
	// host OS.
	stub := "scope:component:cls:python:src/foo/bar.py:User"
	rels := []types.RelationshipRecord{
		{FromID: "0000000000000000", ToID: stub, Kind: "EXTENDS"},
	}
	stats := References(rels, idx)
	if rels[0].ToID != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("ToID not rewritten: %s", rels[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %d", stats.Rewritten)
	}
}

// TestBuildIndex_StructuralRefStub_BackslashFilePath verifies the inverse:
// an entity indexed under a forward-slash path is discoverable when the
// stub itself accidentally carries an OS-native separator. The resolver
// normalises stub-side file paths defensively so the lookup still hits.
func TestBuildIndex_StructuralRefStub_BackslashFilePath(t *testing.T) {
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "SCOPE.Operation", "doWork", "src/foo/bar.py"),
	}
	idx := BuildIndex(entities)

	// Compose a stub with a host-native path segment via FromSlash. On
	// POSIX this is identical to the slash form (so the test asserts the
	// happy path is still happy); on Windows it exercises the new
	// normalisation step.
	filePart := filepath.FromSlash("src/foo/bar.py")
	stub := "scope:operation:method:python:" + filePart + ":doWork"
	rels := []types.RelationshipRecord{
		{FromID: "0000000000000000", ToID: stub, Kind: "CALLS"},
	}
	stats := References(rels, idx)

	// On non-Windows, FromSlash is a no-op and the lookup must succeed
	// as before. On Windows, the normalisation in lookupStructural makes
	// the same assertion pass.
	if rels[0].ToID != "aaaaaaaaaaaaaaaa" {
		// If we are on Windows and this fails, the normalisation regressed.
		if runtime.GOOS == "windows" {
			t.Fatalf("windows stub-path normalization regressed: ToID=%s", rels[0].ToID)
		}
		t.Fatalf("ToID not rewritten: %s", rels[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %d", stats.Rewritten)
	}
}

func TestBuildLocationIndex_WindowsSourceFile(t *testing.T) {
	winPath := filepath.FromSlash("pkg/sub/mod.go")
	entities := []types.EntityRecord{
		entAt("ccccccccccccccccc"[:16], "Function", "Hello", winPath),
	}
	loc := BuildLocationIndex(entities)
	// Stored under the slash-form key.
	bucket, ok := loc["pkg/sub/mod.go"]
	if !ok {
		t.Fatalf("BuildLocationIndex did not key under slash form; keys=%v", keysOf(loc))
	}
	if got := bucket["Hello"]; got != "cccccccccccccccc" {
		t.Fatalf("BuildLocationIndex value mismatch: got %q", got)
	}
	// And NOT under the OS-native form when that form differs.
	if winPath != "pkg/sub/mod.go" {
		if _, leaked := loc[winPath]; leaked {
			t.Fatalf("BuildLocationIndex leaked OS-native key %q", winPath)
		}
	}
}

// TestStubConstants_NoOSPathSeparator_inIdentifiers is a guard against
// future regressions: stub-grammar tokens are graph identifiers and must
// never contain an OS-native path separator.
func TestStubConstants_NoOSPathSeparator_inIdentifiers(t *testing.T) {
	for _, tok := range []string{stubPrefixScope, stubPrefixExternal, scopeKindPrefix, stubDelim} {
		if strings.ContainsRune(tok, filepath.Separator) && filepath.Separator != '/' {
			t.Fatalf("stub token %q must not contain OS path separator %q",
				tok, string(filepath.Separator))
		}
	}
}

func keysOf(m LocationIndex) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
