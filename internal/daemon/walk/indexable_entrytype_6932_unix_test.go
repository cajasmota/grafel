//go:build unix

package walk

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/testsupport"
)

// IndexableEntryType is an EXPORTED cross-package contract, and the export is
// the contract — grading it only through its one consumer (the change-poller
// in internal/daemon/watch) is the wrong way round: it leaves the walk package
// free to change the exported behaviour and stay green, and it means a reader
// of this package cannot see what the function promises. #6932 review RV-3
// showed exactly that: making the Lstat error path return true was DEAD in
// `watch` and left `walk` GREEN.
//
// AXES. Varied: the entry TYPE, over the whole space the type bits can express
// plus both symlink-resolution failures. Held constant: the extension (every
// name ends .go, so the extension filter is never the reason for a verdict),
// the ignore state (no .gitignore anywhere in the fixture), and the sparse
// state (no sparse checkout). That is deliberate — those three gates are NOT
// part of this function's contract, and holding them constant is what makes a
// failure here attributable to the entry-type rule.
func TestIndexableEntryType(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "tree")
	if err := os.MkdirAll(filepath.Join(dir, "realdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(rel string) string {
		p := filepath.Join(dir, rel)
		if err := os.WriteFile(p, []byte("package p\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	link := func(target, rel string) string {
		p := filepath.Join(dir, rel)
		if err := os.Symlink(target, p); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		return p
	}

	regular := write("regular.go")
	nested := write("realdir/target.go")
	fifo := testsupport.MkfifoInTemp(t, root, "tree", "fifo.go")

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"regular file", regular, true},
		{"regular file in a subdirectory", nested, true},
		{"symlink to a regular file", link(regular, "link-to-regular.go"), true},
		{"symlink to a symlink to a regular file", link(filepath.Join(dir, "link-to-regular.go"), "link2.go"), true},
		{"symlink with a relative target", link("regular.go", "rel-link.go"), true},
		{"dangling symlink", link(filepath.Join(dir, "nope.go"), "dangling.go"), false},
		{"symlink to a directory", link(filepath.Join(dir, "realdir"), "link-to-dir.go"), false},
		{"plain directory", filepath.Join(dir, "realdir"), false},
		{"the walk root itself", dir, false},
		{"named pipe", fifo, false},
		{"symlink to a named pipe", link(fifo, "link-to-fifo.go"), false},
		{"character device", "/dev/null", false},
		{"absent path", filepath.Join(dir, "absent.go"), false},
		{"empty path", "", false},
	}
	// The symlink loop needs both halves to exist before either resolves.
	link(filepath.Join(dir, "loop-b.go"), "loop-a.go")
	link(filepath.Join(dir, "loop-a.go"), "loop-b.go")
	cases = append(cases, struct {
		name string
		path string
		want bool
	}{"symlink loop (ELOOP)", filepath.Join(dir, "loop-a.go"), false})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got bool
			// A regression that OPENED the entry would park in open(2) on the
			// FIFO cases forever; fail with attribution instead of wedging the
			// binary until the -timeout watchdog kills it with none.
			testsupport.MustReturn(t, "IndexableEntryType("+tc.path+")", func() {
				got = IndexableEntryType(tc.path)
			})
			if got != tc.want {
				t.Fatalf("IndexableEntryType(%s) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

// The contract is "the entry-type gate, and nothing else". This pins the
// "nothing else" half against the three gates the doc comment disclaims, so a
// future reader cannot mistake the function for "would WalkRepo index this?" —
// and so that a change which quietly folds one of them in is caught here.
//
// Each path below is one WalkRepo REFUSES and IndexableEntryType ACCEPTS. The
// asymmetry is the point: this predicate is a strictly weaker condition, and
// its answer is a superset of the walker's.
func TestIndexableEntryType_DoesNotReproduceTheOtherGates(t *testing.T) {
	dir := t.TempDir()
	mk := func(rel, body string) string {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	mk(".gitignore", "ignored.go\n")
	png := mk("photo.png", "not really a png")
	ignored := mk("ignored.go", "package p\n")
	kept := mk("keep.go", "package p\n")

	walked, _, err := WalkRepo(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	indexed := make(map[string]bool, len(walked))
	for _, f := range walked {
		indexed[f] = true
	}
	if !indexed["keep.go"] {
		t.Fatalf("fixture premise broken: the walker indexed nothing normal; walked=%v", walked)
	}

	for _, tc := range []struct {
		rel  string
		abs  string
		gate string
	}{
		{"photo.png", png, "the indexed-extension filter (#1629)"},
		{"ignored.go", ignored, "the file-level ignore layer (#6931/#6933)"},
	} {
		if indexed[tc.rel] {
			t.Fatalf("fixture premise broken: WalkRepo did not refuse %s via %s; walked=%v", tc.rel, tc.gate, walked)
		}
		if !IndexableEntryType(tc.abs) {
			t.Fatalf("%s: IndexableEntryType refused it, so it DOES reproduce %s — the doc comment disclaims that gate and must be corrected", tc.rel, tc.gate)
		}
	}

	// And the control: the entry-type gate IS reproduced, so the two are not
	// simply unrelated functions.
	if !IndexableEntryType(kept) {
		t.Fatal("IndexableEntryType refused an ordinary indexed file")
	}
	var refusedByWalker []string
	for _, f := range []string{"photo.png", "ignored.go"} {
		if !indexed[f] {
			refusedByWalker = append(refusedByWalker, f)
		}
	}
	sort.Strings(refusedByWalker)
	if len(refusedByWalker) != 2 {
		t.Fatalf("fixture premise broken: expected both paths refused, got %s", strings.Join(refusedByWalker, ","))
	}
}
