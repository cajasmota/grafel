package testsupport_test

// blockingopenscan_test.go — the detector's own table, including its MISSES.
//
// The misses are pinned deliberately, and modelled on homescan_test.go, which
// does the same for the HOME detector. A guard's blind spots are the part
// everybody forgets first, and an undocumented miss is how a reader concludes
// the sweep proves more than it does — which is precisely the "class is closed"
// claim #6468 made four times.

import (
	"go/parser"
	"go/token"
	"testing"

	"github.com/cajasmota/grafel/internal/testsupport"
)

func scan(t *testing.T, src string) []testsupport.NameChosenOpen {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return testsupport.FindNameChosenOpens(fset, f, "x.go")
}

func TestFindNameChosenOpens(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantN   int
		wantLit string
		why     string
	}{
		{
			name: "HIT direct literal argument",
			src: `package p
import "os"
func f() { os.ReadFile("CLAUDE.md") }`,
			wantN: 1, wantLit: "CLAUDE.md",
		},
		{
			name: "HIT literal joined onto a directory",
			src: `package p
import ("os"; "path/filepath")
func f(d string) { os.ReadFile(filepath.Join(d, ".gitignore")) }`,
			wantN: 1, wantLit: ".gitignore",
		},
		{
			name: "HIT one assignment back",
			src: `package p
import ("os"; "path/filepath")
func f(d string) { p := filepath.Join(d, ".grafel", "group.json"); os.ReadFile(p) }`,
			wantN: 1, wantLit: ".grafel",
			why: "the report names the FIRST filename-shaped literal in source order, and \".grafel\" " +
				"is one (leading dot). Which literal is named is diagnostic detail, not the verdict",
		},
		{
			name: "HIT through a package-level const, two hops",
			src: `package p
import "os"
const Name = "fitness.yaml"
func f(d string) { p := d + "/" + Name; os.ReadFile(p) }`,
			wantN: 1, wantLit: "fitness.yaml",
			why: "internal/quality/fitness/rule.go's exact shape; invisible at depth one",
		},
		{
			name: "HIT os.Open",
			src: `package p
import "os"
func f(d string) { os.Open(d + "/AGENTS.md") }`,
			wantN: 1, wantLit: "/AGENTS.md",
			why: "the literal is reported verbatim, leading separator and all",
		},
		{
			name: "HIT os.OpenFile read-only",
			src: `package p
import "os"
func f() { os.OpenFile("CLAUDE.md", os.O_RDONLY, 0) }`,
			wantN: 1, wantLit: "CLAUDE.md",
		},
		{
			name: "HIT self-referential path expression terminates",
			src: `package p
import "os"
func f(d string) { p := d; p = p + "/x.json"; os.ReadFile(p) }`,
			wantN: 1, wantLit: "/x.json",
			why: "`p = p + …` is ordinary Go; a naive resolver recurses forever",
		},
		{
			name: "SKIP routed through safeio",
			src: `package p
import "github.com/cajasmota/grafel/internal/safeio"
func f(d string) { safeio.ReadFileReported(d+"/CLAUDE.md", safeio.FollowSymlinks, 0) }`,
			wantN: 0,
			why:   "routing is what silences the guard — this is the whole feedback loop",
		},
		{
			name: "SKIP a write",
			src: `package p
import "os"
func f() { os.OpenFile("CLAUDE.md", os.O_CREATE|os.O_WRONLY, 0o644) }`,
			wantN: 0,
			why:   "a write to a FIFO does not have the unbounded-read shape",
		},
		{
			name: "SKIP bare directory name",
			src: `package p
import ("os"; "path/filepath")
func f(d string) { os.ReadFile(filepath.Join(d, "config")) }`,
			wantN: 0,
			why:   "no extension and no leading dot — a bare word carries no signal that a FILE is named",
		},
		{
			name: "SKIP a method on something that is not the os package",
			src: `package p
type fs struct{}
func (fs) ReadFile(string) ([]byte, error) { return nil, nil }
func f(os fs) { os.ReadFile("CLAUDE.md") }`,
			wantN: 1,
			why: "FALSE POSITIVE, pinned rather than hidden: the scan matches the IDENTIFIER `os`, " +
				"not a resolved package, so a local variable named os shadows into a report. " +
				"Closing it needs a type checker; no such shadowing exists in this tree.",
			wantLit: "CLAUDE.md",
		},
		{
			name: "MISS path arrives as a bare parameter",
			src: `package p
import "os"
func f(path string) { os.ReadFile(path) }`,
			wantN: 0,
			why: "internal/agents.upsertFile's shape. The scan resolves identifiers, not calls — " +
				"pinned by internal/agents/fifo_6478_test.go instead",
		},
		{
			name: "MISS extensionless hook name from a package-level slice",
			src: `package p
import ("os"; "path/filepath")
var HookNames = []string{"post-commit", "pre-push"}
func f(d string) { for _, n := range HookNames { os.ReadFile(filepath.Join(d, n)) } }`,
			wantN: 0,
			why: "internal/install/hooks's shape. \"pre-push\" is not filename-shaped by any rule " +
				"that does not also match half the identifiers in the tree — pinned by " +
				"internal/install/hooks/fifo_6478_test.go instead",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scan(t, tc.src)
			if len(got) != tc.wantN {
				t.Fatalf("got %d reports, want %d (%s): %v", len(got), tc.wantN, tc.why, got)
			}
			if tc.wantLit != "" && got[0].Literal != tc.wantLit {
				t.Fatalf("literal = %q, want %q", got[0].Literal, tc.wantLit)
			}
		})
	}
}

func TestFilenameShaped(t *testing.T) {
	yes := []string{
		".git", ".gitignore", ".grafelignore", "CLAUDE.md", "group.json",
		"fitness.yaml", "src/main/resources/application.properties", "a.b.c",
	}
	no := []string{
		"", "config", "hooks", "pre-push", ".", "..", "/",
		"https://example.com/a.json", "some file.md", "%s.json", "*.md",
	}
	for _, s := range yes {
		if !testsupport.FilenameShaped(s) {
			t.Errorf("FilenameShaped(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if testsupport.FilenameShaped(s) {
			t.Errorf("FilenameShaped(%q) = true, want false", s)
		}
	}
}
