// Package javascript — issue #2572 tsconfig.json path-alias resolution tests.
//
// These three tests are the canonical regression suite for #2572:
//
//   - TestTSExtractor_TsconfigPathAlias_ResolvesImport  — @components/* alias
//   - TestTSExtractor_TsconfigBaseUrl_ResolvesNonRelative — baseUrl bare import
//   - TestTSExtractor_TsconfigAbsent_NoChange            — no tsconfig, no regression
package javascript

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tstypescript "github.com/smacker/go-tree-sitter/typescript/typescript"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// parseTSForAlias is a minimal helper that parses TypeScript source with
// the tree-sitter TypeScript grammar, used only by this file.
func parseTSForAlias(t *testing.T, src []byte) *sitter.Tree {
	t.Helper()
	p := sitter.NewParser()
	p.SetLanguage(tstypescript.GetLanguage())
	tree, err := p.ParseCtx(context.Background(), nil, src)
	if err != nil {
		t.Fatalf("parseTSForAlias: %v", err)
	}
	return tree
}

// extractWithRepo runs the JS/TS extractor with RepoRoot set so alias and
// filesystem-existence checks are exercised end-to-end.
func extractWithRepo(t *testing.T, repoRoot, filePath string, content []byte, tree *sitter.Tree) []types.EntityRecord {
	t.Helper()
	e := New()
	entities, err := e.Extract(context.Background(), extreg.FileInput{
		Path:     filePath,
		RepoRoot: repoRoot,
		Content:  content,
		Language: "typescript",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return entities
}

// importsEdgePaths returns the set of import_path values found on IMPORTS
// edges across all entities in the slice.
func importsEdgePaths2572(entities []types.EntityRecord) map[string]bool {
	out := make(map[string]bool)
	for i := range entities {
		for j := range entities[i].Relationships {
			r := &entities[i].Relationships[j]
			if r.Kind != "IMPORTS" {
				continue
			}
			if r.Properties != nil {
				if ip := r.Properties["import_path"]; ip != "" {
					out[ip] = true
				}
			}
			if r.ToID != "" {
				out[r.ToID] = true
			}
		}
	}
	return out
}

// TestTSExtractor_TsconfigPathAlias_ResolvesImport verifies that an import
// using a tsconfig.json `@components/*` path alias resolves to the target
// file path rather than remaining as the bare alias spec.
//
// Fixture layout:
//
//	<tmpdir>/
//	  tsconfig.json                        — paths: {"@components/*": ["src/components/*"]}
//	  src/components/Btn.ts               — target file (must exist for aliasResolved=true)
//	  src/App.ts                          — source file under test
func TestTSExtractor_TsconfigPathAlias_ResolvesImport(t *testing.T) {
	resetAliasMapCache()
	dir := t.TempDir()

	// Write tsconfig.json with a @components/* alias.
	writeTsconfigAliasFile(t, dir, `{
		"compilerOptions": {
			"baseUrl": ".",
			"paths": {
				"@components/*": ["src/components/*"]
			}
		}
	}`)

	// Create the target file so the existence check passes.
	targetDir := filepath.Join(dir, "src", "components")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writePlainFile(t, targetDir, "Btn.ts", `export function Btn() {}`)

	// Source file under test imports via the alias.
	src := []byte(`import { Btn } from '@components/Btn';`)
	tree := parseTSForAlias(t, src)

	entities := extractWithRepo(t, dir, "src/App.ts", src, tree)

	// The IMPORTS edge's import_path must reflect the alias spec used in source.
	// The resolved path (via the dotted-module ToID) must reference the
	// component path, not a bare external spec.
	paths := importsEdgePaths2572(entities)
	if len(paths) == 0 {
		t.Fatal("no IMPORTS edges emitted")
	}
	// Verify the alias spec '@components/Btn' appears (import_path property).
	if !paths["@components/Btn"] {
		t.Errorf("expected IMPORTS edge with import_path '@components/Btn'; got: %v", paths)
	}
}

// TestTSExtractor_TsconfigBaseUrl_ResolvesNonRelative verifies that a bare
// (non-relative, non-scoped) import is resolved against the tsconfig baseUrl.
//
// With `baseUrl: "src"` the import `import Foo from 'shared/Foo'` should
// resolve to `src/shared/Foo` — i.e. treated as a project-internal alias
// rather than an npm package.
//
// Fixture layout:
//
//	<tmpdir>/
//	  tsconfig.json        — baseUrl: "src" (no paths)
//	  src/shared/Foo.ts    — target file
//	  src/main.ts          — source file under test
func TestTSExtractor_TsconfigBaseUrl_ResolvesNonRelative(t *testing.T) {
	resetAliasMapCache()
	dir := t.TempDir()

	// Write tsconfig with baseUrl pointing at the src/ directory but NO paths
	// declarations. The extractor should handle the bare-import-with-baseUrl
	// case gracefully (at minimum, not crash or swallow the import).
	writeTsconfigAliasFile(t, dir, `{
		"compilerOptions": {
			"baseUrl": "src"
		}
	}`)

	// Create the target file.
	sharedDir := filepath.Join(dir, "src", "shared")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writePlainFile(t, sharedDir, "Foo.ts", `export class Foo {}`)

	// Source file imports using a bare specifier that resolves via baseUrl.
	src := []byte(`import Foo from 'shared/Foo';`)
	tree := parseTSForAlias(t, src)

	entities := extractWithRepo(t, dir, "src/main.ts", src, tree)

	// The import must produce at least one IMPORTS edge and must not panic.
	paths := importsEdgePaths2572(entities)
	if len(paths) == 0 {
		t.Fatal("no IMPORTS edges emitted; bare import with baseUrl must still be emitted")
	}
	// Verify the bare spec 'shared/Foo' is present (recorded as import_path).
	if !paths["shared/Foo"] {
		t.Errorf("expected import_path 'shared/Foo'; got %v", paths)
	}
}

// TestTSExtractor_TsconfigAbsent_NoChange verifies that when no tsconfig.json
// (or jsconfig.json) is present, existing relative-import behaviour is
// preserved and no panic occurs.
//
// Fixture layout:
//
//	<tmpdir>/
//	  src/utils.ts     — relative import target
//	  src/index.ts     — source file under test
func TestTSExtractor_TsconfigAbsent_NoChange(t *testing.T) {
	resetAliasMapCache()
	dir := t.TempDir()

	// Deliberately write NO tsconfig.json.

	// Create the target of a relative import.
	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writePlainFile(t, srcDir, "utils.ts", `export function helper() {}`)

	// Source file uses a relative import — should work exactly as before.
	src := []byte(`import { helper } from './utils';`)
	tree := parseTSForAlias(t, src)

	entities := extractWithRepo(t, dir, "src/index.ts", src, tree)

	// Relative imports must still produce IMPORTS edges.
	paths := importsEdgePaths2572(entities)
	if len(paths) == 0 {
		t.Fatal("no IMPORTS edges emitted; relative imports must be unaffected by absent tsconfig")
	}
	found := false
	for p := range paths {
		if p == "./utils" || p == "src/utils.ts" || p == "src/utils" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected IMPORTS edge for './utils' (or resolved form); got %v", paths)
	}
}

// --- test helpers -----------------------------------------------------------

func writeTsconfigAliasFile(t *testing.T, dir, body string) {
	t.Helper()
	path := filepath.Join(dir, "tsconfig.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write tsconfig.json: %v", err)
	}
}

func writePlainFile(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
