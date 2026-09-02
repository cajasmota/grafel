package treesitter_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// third_party/tree-sitter-swift is an inert module that exists only so
// `go mod tidy` can resolve the phantom test-only import inside the Swift
// grammar (#6732). Its package doc asserts two properties — the module has no
// requirements, and the package exports nothing — and module-hygiene.yml
// observes NEITHER: `go mod tidy` is satisfied by the package merely existing.
// A reviewer's mutant that gave the shim an exported `Language()` survived
// tidy with exit 0. These tests are what makes that mutant die.
//
// This lives here rather than in the shim module because the shim is a nested
// module: `go test ./...` from the repo root never descends into it.

func swiftShimDir(t *testing.T) string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// here = <root>/internal/treesitter/swift_shim_guard_6732_test.go
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "..", ".."))
	return filepath.Join(root, "third_party", "tree-sitter-swift")
}

// The shim must not depend on anything. A `require` would pin a second copy of
// a version that the root go.mod also pins, in a module nothing builds, and it
// would drift silently on every grammar bump.
func TestSwiftShimModuleHasNoRequirements(t *testing.T) {
	path := filepath.Join(swiftShimDir(t), "go.mod")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	for i, line := range strings.Split(string(src), "\n") {
		if f := strings.Fields(line); len(f) > 0 && f[0] == "require" {
			t.Errorf("%s:%d declares a requirement (%q); the shim must stay dependency-free — see its package doc and #6732", path, i+1, strings.TrimSpace(line))
		}
	}
}

// The shim must export nothing, so that any code which mistakes it for a real
// Swift grammar fails to compile instead of silently getting a stub.
func TestSwiftShimExportsNothing(t *testing.T) {
	dir := swiftShimDir(t)
	var files []string
	if err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".go") {
			files = append(files, p)
		}
		return nil
	}); err != nil {
		t.Fatalf("walking %s: %v", dir, err)
	}
	if len(files) == 0 {
		t.Fatalf("no .go files under %s — the shim package must exist or `go mod tidy` cannot resolve the phantom import (#6732)", dir)
	}

	fset := token.NewFileSet()
	for _, p := range files {
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", p, err)
		}
		for _, decl := range f.Decls {
			for _, name := range declaredNames(decl) {
				if name.IsExported() {
					t.Errorf("%s declares exported identifier %q; the shim is inert by design and must export nothing (#6732)", fset.Position(name.Pos()), name.Name)
				}
			}
		}
	}
}

func declaredNames(decl ast.Decl) []*ast.Ident {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		return []*ast.Ident{d.Name}
	case *ast.GenDecl:
		var out []*ast.Ident
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				out = append(out, s.Name)
			case *ast.ValueSpec:
				out = append(out, s.Names...)
			}
		}
		return out
	}
	return nil
}

// The shim is only reachable through the root `replace`. Without it the phantom
// import resolves to the archived upstream module again and `go mod tidy`
// aborts. module-hygiene.yml is the real gate for that; this is its fast local
// echo, so deleting the directive is caught by `go test` too.
func TestRootGoModReplacesSwiftGrammarPathWithShim(t *testing.T) {
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "..", ".."))
	path := filepath.Join(root, "go.mod")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	const want = "replace github.com/tree-sitter/tree-sitter-swift => ./third_party/tree-sitter-swift"
	if !strings.Contains(string(src), want) {
		t.Errorf("%s is missing %q — without it `go mod tidy` cannot complete (#6732)", path, want)
	}
}
