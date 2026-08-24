// Tests for the protected-path gate on canonicalizePath (#6548).
//
// Root cause: canonicalizePath is a top-down decomposition that os.ReadDir's
// EVERY ancestor of every repo path to recover each segment's on-disk casing.
// It had no protected-path check at all, so on a Mac with iCloud "Desktop &
// Documents" sync on, canonicalizing a path under ~/Documents read a
// file-provider-managed directory and the OS asked the user to approve
// «"grafel" wants to access files managed by "iCloud Drive"» — for a location
// the user never pointed grafel at.
//
// The fix: consult the single protected-path authority (internal/protectedpath)
// before each segment read and, for a protected directory, SKIP the casing
// recovery instead of reading it. The returned path is then the input path with
// the input's own casing preserved from that segment onward — the same
// deterministic degrade already used for a read error or a ReadDir timeout, and
// stable across calls because canonicalizePath is keyed and cached by the input
// string.
//
// No test here reads a real ~/Documents: the home root is injected and the
// fixture lives under t.TempDir().
package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCanonicalizePath_SkipsProtectedDir is the #6548 regression: a path under
// ~/Documents must be canonicalized WITHOUT ever reading ~/Documents.
func TestCanonicalizePath_SkipsProtectedDir(t *testing.T) {
	home := t.TempDir()
	docs := filepath.Join(home, "Documents")
	if err := os.MkdirAll(filepath.Join(docs, "proj"), 0o755); err != nil {
		t.Fatal(err)
	}

	fakeHomeOnDarwin(t, home)

	var read []string
	origRead := readDirFunc
	readDirFunc = func(dir string) ([]os.DirEntry, error) {
		read = append(read, dir)
		return os.ReadDir(dir)
	}
	t.Cleanup(func() { readDirFunc = origRead })

	// Deliberately mis-cased leaf so we can observe whether casing recovery
	// ran (it must NOT: recovering it requires reading ~/Documents).
	input := filepath.Join(docs, "PROJ")
	got := canonicalizePath(input)

	for _, dir := range read {
		if dir == docs || strings.HasPrefix(dir, docs+string(filepath.Separator)) {
			t.Errorf("canonicalizePath read protected directory %q (#6548); reads = %v", dir, read)
		}
	}
	// Under a protected segment the degrade is case-FOLDED, not verbatim
	// (see the #2086 note in canonicalizePath): "PROJ" comes back "proj".
	want := filepath.Join(docs, "proj")
	if got != want {
		t.Errorf("canonicalizePath(%q) = %q, want %q", input, got, want)
	}
}

// fakeHomeOnDarwin points the protected-path gate at a fixture home and forces
// the darwin code path, so no test ever reads a real ~/Documents. It injects
// the HOME, deliberately not the predicate — see the note on traversalHome.
func fakeHomeOnDarwin(t *testing.T, home string) {
	t.Helper()
	origHome, origGOOS := traversalHome, traversalGOOS
	traversalHome = func() string { return home }
	traversalGOOS = "darwin"
	t.Cleanup(func() { traversalHome, traversalGOOS = origHome, origGOOS })
}

// The gate must consult the TRAVERSAL predicate (the full union), not the WALK
// predicate (media classes only, which omits Desktop/Documents/Downloads).
// That distinction IS the #6548 fix, and it lives in one identifier; this test
// asserts the shipped package-level function rather than a test closure, so
// swapping that identifier back cannot pass.
func TestTraversalGateUsesTheUnionPredicate(t *testing.T) {
	home := t.TempDir()
	fakeHomeOnDarwin(t, home)

	// TCC class: in the union, absent from the walk list. These are the #6548
	// folders — under the walk predicate every one of them returns false.
	for _, name := range []string{"Desktop", "Documents", "Downloads", "Public"} {
		dir := filepath.Join(home, name)
		if protected, _ := traversalProtected(dir); !protected {
			t.Errorf("traversalProtected(%q) = false; canonicalizePath would ReadDir it and re-open #6548 "+
				"(gate must use IsTraversalProtected, not IsWalkProtected)", dir)
		}
	}
	// Media class: in both lists — held so the union is not narrowed either.
	for _, name := range []string{"Library", "Movies", "Music", "Pictures"} {
		if protected, _ := traversalProtected(filepath.Join(home, name)); !protected {
			t.Errorf("traversalProtected(~/%s) = false, want true", name)
		}
	}
	// Permissive direction: an ordinary folder must stay readable, or casing
	// recovery stops for every repo on the machine.
	for _, name := range []string{"Projects", "Documentation", "src"} {
		if protected, _ := traversalProtected(filepath.Join(home, name)); protected {
			t.Errorf("traversalProtected(~/%s) = true, want false", name)
		}
	}
}

// #2086: repoStateHash is sha256(canonicalizePath(p)) SO THAT case variants of
// one repo collapse to one store root. canonicalizePath normally earns that by
// reading the true on-disk casing; under a protected segment it may not read,
// so it case-folds instead. Preserving the input casing there would give one
// on-disk repo two roots depending on how the user typed the path.
func TestCanonicalizePath_ProtectedDegradeCollapsesCaseVariants(t *testing.T) {
	home := t.TempDir()
	docs := filepath.Join(home, "Documents")
	if err := os.MkdirAll(filepath.Join(docs, "Acme"), 0o755); err != nil {
		t.Fatal(err)
	}
	fakeHomeOnDarwin(t, home)

	lower := canonicalizePath(filepath.Join(docs, "acme"))
	upper := canonicalizePath(filepath.Join(docs, "ACME"))
	mixed := canonicalizePath(filepath.Join(docs, "Acme"))

	if lower != upper || lower != mixed {
		t.Errorf("case variants under a protected folder produced different canonical paths "+
			"(#2086: one repo, one store root): %q / %q / %q", lower, upper, mixed)
	}
	if h1, h2 := repoStateHash(filepath.Join(docs, "acme")), repoStateHash(filepath.Join(docs, "ACME")); h1 != h2 {
		t.Errorf("repoStateHash differs by typed casing under a protected folder: %s vs %s", h1, h2)
	}

	// The protected segment itself was recovered by a legitimate read of its
	// parent, so its real casing must survive the fold.
	if !strings.Contains(lower, string(filepath.Separator)+"Documents"+string(filepath.Separator)) {
		t.Errorf("canonicalizePath folded the protected segment itself: %q", lower)
	}
}

// On a case-SENSITIVE filesystem "Acme" and "acme" are different directories,
// so folding would both collide two repos and produce a path that does not
// exist. The fold is gated on GOOS.
func TestCanonicalizePath_ProtectedDegradeDoesNotFoldOnCaseSensitiveFS(t *testing.T) {
	if caseInsensitiveFS("linux") {
		t.Fatal("caseInsensitiveFS(\"linux\") = true; folding a Linux path would collide distinct repos")
	}
	if !caseInsensitiveFS("darwin") || !caseInsensitiveFS("windows") {
		t.Fatal("caseInsensitiveFS must hold for darwin and windows")
	}
}

// A legitimate repo under a NON-protected home subdirectory must still get
// full casing recovery — the permissive direction would silently stop
// canonicalizing (and therefore de-duplicating) real repos.
func TestCanonicalizePath_UnprotectedPathStillCanonicalized(t *testing.T) {
	home := t.TempDir()
	// "Documentation" shares a prefix with "Documents" but is not protected.
	real := filepath.Join(home, "Projects", "Documentation", "grafel")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}

	fakeHomeOnDarwin(t, home)

	input := filepath.Join(home, "Projects", "DOCUMENTATION", "GRAFEL")
	got := canonicalizePath(input)
	if got != real {
		t.Errorf("canonicalizePath(%q) = %q, want %q — a legitimate path must still be canonicalized", input, got, real)
	}
}
