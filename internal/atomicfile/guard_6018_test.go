package atomicfile_test

// guard_6018_test.go — the guard that stops the #6018 class re-entering.
//
// It scans every non-test .go file in the repository for a temp path derived
// from its destination. Such a path is shared by every writer aiming at that
// destination, which is the whole bug (see the package doc). The shape was
// diagnosed and point-fixed three separate times before anyone generalised it;
// without a guard the fourth arrives with the next writer someone adds.
//
// The scan is AST-based, not grep, so a ".tmp" in a comment, a map key
// (internal/daemon/watch/skip.go), or a strings.HasSuffix argument does not
// trip it.
//
// # What it catches
//
//	<expr> + ".tmp"                 // concatenated string literal
//	<expr> + tmpSuffix              // concatenated CONSTANT whose value ends .tmp
//	fmt.Sprintf("%s.tmp", <expr>)   // format string ending .tmp
//
// # What it does NOT catch — read this before trusting it
//
// This guard ends ONE FAMILY of spellings, not the class. A scan of the tree at
// the time of writing found zero live instances of any of the following, so
// none of them is exploited today, but all of them are the same bug:
//
//   - A DIFFERENT SUFFIX: `path + ".partial"`, `path + ".new"`, `path + ".swp"`.
//     Identical hazard; the guard keys on ".tmp" only. Widening the suffix set
//     is easy but produces false positives on non-temp uses, so it is left out
//     deliberately rather than by oversight.
//   - A FIXED NAME IN A SHARED DIRECTORY:
//     `filepath.Join(filepath.Dir(path), "state.tmp")`. No concatenation with
//     the destination at all, yet every writer into that directory collides.
//     This is the most likely way the class re-enters, and the AST cannot
//     distinguish it from a legitimate fixed filename without knowing the
//     writer's intent.
//   - Any temp path assembled across statements, through a helper, or from a
//     non-constant variable.
//
// New code should use atomicfile.WriteFile. A green run of this test means no
// NEW instance of the three caught spellings, not that the tree is free of
// deterministic temp names.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// notYetConverted lists the files that STILL build a deterministic temp name,
// as of #6018's first conversion pass.
//
// This list is a debt ledger, not a policy. It must only ever SHRINK: every
// follow-up that converts a file deletes its entry, and when the last entry
// goes the map should be deleted along with the allowlist logic. Nothing may be
// added to it — a new offender means new code took the unsafe shape, which is
// exactly what this test exists to stop.
//
// Why these were not converted in the first pass. The ordering rule is
// EXPOSURE, not convenience: anything written by more than one process, or read
// cross-process to locate something else, goes first regardless of how awkward
// the conversion is.
//
//   - internal/graph/graph.go — only WriteAtomic remains. It encodes the WHOLE
//     graph Document straight into the open file with json.NewEncoder.
//     Converting it means json.Marshal into a []byte first, which doubles peak
//     memory on the single largest object in the process. That is a memory
//     argument, not a "it streams" one, and it is why this needs a
//     writer-callback variant of the helper rather than a drop-in.
//     (writeJSONAtomic in the same file WAS converted — it encoded one small
//     in-memory sidecar value, so the streaming framing there was incidental.)
//   - internal/graph/fbwriter/* genuinely stream: they build the flatbuffer
//     incrementally against an open handle and never hold the payload as one
//     []byte. Same follow-up as WriteAtomic.
//   - internal/install/*, internal/agentpatterns/* are install-time /
//     interactive single-writer paths.
//     (internal/install/mcpreg/{mcpreg,toml}.go came OFF this list in #6240 —
//     not by converting to atomicfile.WriteFile, which reproduces that bug
//     exactly, but by growing a local writeThrough on a unique CreateTemp name.
//     Converting a file is not the only way to leave the ledger; ceasing to
//     build a deterministic temp name is. That local helper is now
//     atomicfile.WriteThrough (#6246), which resolves the destination first and
//     then calls plain WriteFile against it.
//     internal/install/agenthooks/claudecode.go and
//     internal/dashboard/handlers_mcp_setup.go came off in #6246, onto the
//     atomicfile.WriteThrough that generalises #6240's local helper. Note what
//     the "single-writer" deferral above did NOT cover: it is a CONCURRENCY
//     argument, and these files were also destroying symlinks and widening 0600
//     to 0644 on user-owned dotfiles the whole time. Converting the remaining
//     internal/install/* entries to atomicfile.WriteFile would close their
//     ledger lines while PRESERVING both of those defects, because WriteFile
//     documents both as deliberate. Check which of the two helpers a
//     destination wants before converting it.
//     internal/install/rulesfiles/rulesfiles.go and internal/cli/register.go
//     came off in #6246 slice 2, for destinations that are TRACKED FILES IN THE
//     USER'S GIT REPO — CLAUDE.md, AGENTS.md, .cursorrules. Same two defects,
//     larger blast radius: a detached symlink there means the user's next commit
//     silently does not contain what they think it does. Both resolve first and
//     then call WriteFile against the resolved path, and both PRESERVE an
//     existing mode while creating at 0644 — the project-file answer, not the
//     dashboard's 0600 one.)
//   - internal/enrichment/*, internal/dashboard/*, internal/embed,
//     internal/indexer/diff, internal/agents and internal/graph/manifest are
//     plausible follow-ups but were left out to keep the first commit
//     reviewable. None is written by more than one process today.
var notYetConverted = map[string]bool{
	"internal/agentpatterns/config.go":                    true,
	"internal/agentpatterns/patterns.go":                  true,
	"internal/agentpatterns/sync.go":                      true,
	"internal/agents/inject_map.go":                       true,
	"internal/cli/pendingtools.go":                        true,
	"internal/dashboard/handlers_enrichment_writeback.go": true,
	"internal/dashboard/handlers_settings.go":             true,
	"internal/embed/store.go":                             true,
	"internal/enrichment/candidates.go":                   true,
	"internal/enrichment/docgen_repair.go":                true,
	"internal/graph/fbwriter/segmented.go":                true,
	"internal/graph/fbwriter/streaming.go":                true,
	"internal/graph/fbwriter/writer.go":                   true,
	"internal/graph/graph.go":                             true,
	"internal/graph/manifest.go":                          true,
	"internal/indexer/diff/diff.go":                       true,
	"internal/install/mcptools/mcptools.go":               true,
	"internal/install/state.go":                           true,
}

func TestNoDeterministicTempNames(t *testing.T) {
	root := repoRoot(t)
	offenders := scanDeterministicTempNames(t, root)

	// 1. Nothing outside the ledger may use the shape.
	var unexpected []string
	seen := map[string]bool{}
	for _, o := range offenders {
		seen[o.file] = true
		if !notYetConverted[o.file] {
			unexpected = append(unexpected, o.String())
		}
	}
	sort.Strings(unexpected)
	if len(unexpected) > 0 {
		t.Errorf("deterministic temp names (#6018) in non-allowlisted files:\n  %s\n\n"+
			"A temp path built as <dest>+\".tmp\" is shared by every writer aiming at that\n"+
			"destination: concurrent writers O_TRUNC and interleave into one temp inode and\n"+
			"each renames a torn file into place. Use atomicfile.WriteFile instead.\n"+
			"Do NOT add the file to notYetConverted — that list only shrinks.",
			strings.Join(unexpected, "\n  "))
	}

	// 2. The ledger must not rot: an entry that no longer offends is a
	//    conversion someone forgot to record, and leaving it in would keep the
	//    guard permanently blind to that file.
	var stale []string
	for f := range notYetConverted {
		if !seen[f] {
			stale = append(stale, f)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("notYetConverted lists files that no longer build a deterministic temp name:\n  %s\n\n"+
			"Delete these entries — the guard is blind to any file left on the list.",
			strings.Join(stale, "\n  "))
	}
}

// TestGuardDetectsTheShape is the guard's own guard: it runs the SAME detector
// used above over a synthetic source file containing the offending shape, and
// over one that uses atomicfile. If the detector ever stops binding — a
// refactor that silently matches nothing is the failure mode this codebase
// keeps hitting — this fails even though the scan above would be vacuously
// green.
func TestGuardDetectsTheShape(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			name: "direct concat",
			src: `package p
import "os"
func f(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil { return err }
	return os.Rename(tmp, path)
}`,
			want: 1,
		},
		{
			name: "concat inside a call",
			src: `package p
import ("os"; "path/filepath")
func f(dir, name string) { _ = os.Remove(filepath.Join(dir, name+".tmp")) }`,
			want: 1,
		},
		{
			name: "two offenders in one file",
			src: `package p
func f(a, b string) (string, string) { return a + ".tmp", b + ".tmp" }`,
			want: 2,
		},
		{
			name: "map key is not a concat",
			src: `package p
var skip = map[string]struct{}{".tmp": {}}`,
			want: 0,
		},
		{
			name: "comment mentioning the shape",
			src: `package p
// NOT a fixed path+".tmp": see #6018.
func f() {}`,
			want: 0,
		},
		{
			name: "suffix comparison is not a concat",
			src: `package p
import "strings"
func f(n string) bool { return strings.HasSuffix(n, ".tmp") }`,
			want: 0,
		},
		{
			name: "atomicfile call is clean",
			src: `package p
import "github.com/cajasmota/grafel/internal/atomicfile"
func f(path string, b []byte) error { return atomicfile.WriteFile(path, b, 0o644) }`,
			want: 0,
		},
		{
			name: "unique CreateTemp pattern is clean",
			src: `package p
import ("os"; "path/filepath")
func f(path string) { _, _ = os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*") }`,
			want: 0,
		},
		{
			name: "fmt.Sprintf format ending .tmp",
			src: `package p
import "fmt"
func f(path string) string { return fmt.Sprintf("%s.tmp", path) }`,
			want: 1,
		},
		{
			name: "Sprintf without a trailing .tmp is clean",
			src: `package p
import "fmt"
func f(path string) string { return fmt.Sprintf("%s.tmp-%d", path, 7) }`,
			want: 0,
		},
		{
			name: "Sprintf on another package is not fmt",
			src: `package p
import "log"
func f(path string) { log.Sprintf("%s.tmp", path) }`,
			want: 0,
		},
		{
			name: "file-local const suffix",
			src: `package p
const tmpSuffix = ".tmp"
func f(path string) string { return path + tmpSuffix }`,
			want: 1,
		},
		{
			name: "const not ending in .tmp is clean",
			src: `package p
const suffix = ".json"
func f(path string) string { return path + suffix }`,
			want: 0,
		},

		// --- KNOWN MISSES -------------------------------------------------
		// These are the same bug and the detector does NOT catch them. They are
		// asserted at want:0 so the file states its real reach rather than
		// implying total coverage. If a future change starts catching one of
		// these, flip its want to 1 and delete the corresponding paragraph from
		// the file doc — do not delete the row.
		{
			name: "MISS: a different suffix",
			src: `package p
func f(path string) string { return path + ".partial" }`,
			want: 0,
		},
		{
			name: "MISS: fixed name in a shared directory",
			src: `package p
import "path/filepath"
func f(path string) string { return filepath.Join(filepath.Dir(path), "state.tmp") }`,
			want: 0,
		},
		{
			name: "MISS: suffix const declared in another file of the package",
			src: `package p
func f(path string) string { return path + suffixFromElsewhere }`,
			want: 0,
		},
		{
			name: "MISS: suffix reaching the concat through a variable",
			src: `package p
func f(path string) string { s := ".tmp"; return path + s }`,
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "x.go", tc.src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got := len(findDeterministicTempNames(fset, f, "x.go"))
			if got != tc.want {
				t.Fatalf("detector found %d offenders, want %d", got, tc.want)
			}
		})
	}
}

type tempNameOffence struct {
	file string // repo-relative
	line int
}

func (o tempNameOffence) String() string { return o.file + ":" + strconv.Itoa(o.line) }

// findDeterministicTempNames reports the three caught spellings in f — see the
// file doc for the full catch/miss list.
//
// Matching AST shapes rather than text is what keeps comments, map keys and
// strings.HasSuffix arguments out of the results.
//
// Constant resolution is FILE-LOCAL: a `const tmpSuffix = ".tmp"` declared in
// another file of the same package is not resolved, because the scan parses one
// file at a time and does not run go/types. That is a known hole in a spelling
// that is itself already unusual.
func findDeterministicTempNames(fset *token.FileSet, f *ast.File, rel string) []tempNameOffence {
	consts := stringConsts(f)
	var out []tempNameOffence
	report := func(n ast.Node) {
		out = append(out, tempNameOffence{file: rel, line: fset.Position(n.Pos()).Line})
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.BinaryExpr:
			// <expr> + ".tmp"   and   <expr> + tmpSuffix
			if e.Op != token.ADD {
				return true
			}
			if s, ok := constStringValue(e.Y, consts); ok && strings.HasSuffix(s, ".tmp") {
				report(e)
			}
		case *ast.CallExpr:
			// fmt.Sprintf("%s.tmp", <expr>)
			sel, ok := e.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Sprintf" {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "fmt" || len(e.Args) < 2 {
				return true
			}
			if s, ok := constStringValue(e.Args[0], consts); ok && strings.HasSuffix(s, ".tmp") {
				report(e)
			}
		}
		return true
	})
	return out
}

// stringConsts collects file-local `const name = "literal"` bindings.
func stringConsts(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				if s, err := strconv.Unquote(lit.Value); err == nil {
					out[name.Name] = s
				}
			}
		}
	}
	return out
}

// constStringValue resolves e to a string when it is a string literal or an
// identifier bound to a file-local string constant.
func constStringValue(e ast.Expr, consts map[string]string) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	case *ast.Ident:
		s, ok := consts[v.Name]
		return s, ok
	}
	return "", false
}

func scanDeterministicTempNames(t *testing.T, root string) []tempNameOffence {
	t.Helper()
	var out []tempNameOffence
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "testdata", "dist", "build":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			// Unparseable non-test source would fail the build anyway; do not
			// let it silently shrink the scan.
			t.Fatalf("parse %s: %v", rel, perr)
		}
		out = append(out, findDeterministicTempNames(fset, f, rel)...)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	// A scan that finds nothing at all means the walk stopped binding the tree.
	if len(out) == 0 && len(notYetConverted) > 0 {
		t.Fatalf("scan of %s found no Go source using the shape at all — the walk is not "+
			"binding the repository (expected at least the notYetConverted entries)", root)
	}
	return out
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// here = <root>/internal/atomicfile/guard_6018_test.go
	return filepath.Clean(filepath.Join(filepath.Dir(here), "..", ".."))
}
