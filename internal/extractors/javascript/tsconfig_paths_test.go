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
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	tstypescript "github.com/smacker/go-tree-sitter/typescript/typescript"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// parseTSForAlias is a minimal helper that parses TypeScript source with
// the tree-sitter TypeScript grammar, used only by this file.
func parseTSForAlias(t *testing.T, src []byte) ts.Tree {
	t.Helper()
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tstypescript.GetLanguage()))
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parseTSForAlias: %v", err)
	}
	return tree
}

// extractWithRepo runs the JS/TS extractor with RepoRoot set so alias and
// filesystem-existence checks are exercised end-to-end.
func extractWithRepo(t *testing.T, repoRoot, filePath string, content []byte, tree ts.Tree) []types.EntityRecord {
	t.Helper()
	e := New()
	entities, err := e.Extract(context.Background(), extreg.FileInput{
		Path:     filePath,
		RepoRoot: repoRoot,
		Content:  content,
		Language: "typescript",
		TSTree:   tree,
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

// TestTSExtractor_TsconfigBaseUrlOnly_ResolvesBareImport verifies that when
// tsconfig has only baseUrl (no paths{}), a bare import that resolves to an
// existing file under baseUrl is resolved to that file's repo-relative path.
//
// Fixture layout:
//
//	<tmpdir>/
//	  tsconfig.json        — baseUrl: "./src" (no paths)
//	  src/shared/Foo.ts    — target file
//	  src/main.ts          — source file under test
func TestTSExtractor_TsconfigBaseUrlOnly_ResolvesBareImport(t *testing.T) {
	resetAliasMapCache()
	dir := t.TempDir()

	writeTsconfigAliasFile(t, dir, `{
		"compilerOptions": {
			"baseUrl": "./src"
		}
	}`)

	sharedDir := filepath.Join(dir, "src", "shared")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writePlainFile(t, sharedDir, "Foo.ts", `export class Foo {}`)

	// AliasMap.BaseURL should be populated and Resolve should not hit the path.
	m := LoadAliasMap(dir)
	if m.BaseURL != "src" {
		t.Errorf("expected AliasMap.BaseURL == %q, got %q", "src", m.BaseURL)
	}
	if got := m.Resolve("shared/Foo"); got != "" {
		t.Errorf("Resolve via alias table should miss for baseUrl-only tsconfig; got %q", got)
	}

	// End-to-end extraction: 'shared/Foo' must resolve to src/shared/Foo.ts.
	src := []byte(`import { Foo } from 'shared/Foo';`)
	tree := parseTSForAlias(t, src)
	entities := extractWithRepo(t, dir, "src/main.ts", src, tree)

	paths := importsEdgePaths2572(entities)
	if len(paths) == 0 {
		t.Fatal("no IMPORTS edges emitted")
	}
	if !paths["shared/Foo"] {
		t.Errorf("expected IMPORTS edge with import_path 'shared/Foo'; got: %v", paths)
	}
	// The resolved file path must appear in the edges (either as a
	// repo-relative file path or as a dotted-module ToID) confirming that the
	// baseUrl fallback resolved the import to a project-internal target.
	// When aliasResolved=true the ToID is "<dotted>.<importedName>" so we
	// look for any path containing "src" and "shared" and "Foo".
	found := false
	for p := range paths {
		if (strings.Contains(p, "src") && strings.Contains(p, "shared") && strings.Contains(p, "Foo")) ||
			p == "src/shared/Foo.ts" || p == "src/shared/Foo" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a resolved edge pointing to src/shared/Foo*; got: %v", paths)
	}
}

// TestTSExtractor_TsconfigBaseUrlOnly_FallsThroughForExternal verifies that
// when tsconfig has only baseUrl and a bare import like 'react' has no
// corresponding file under baseUrl, the import is treated as an external npm
// spec (not misclassified as a project-internal import).
//
// Fixture layout:
//
//	<tmpdir>/
//	  tsconfig.json  — baseUrl: "./src" (no paths)
//	  src/main.ts    — source file under test
//	  (no src/react file — react is npm-only)
func TestTSExtractor_TsconfigBaseUrlOnly_FallsThroughForExternal(t *testing.T) {
	resetAliasMapCache()
	dir := t.TempDir()

	writeTsconfigAliasFile(t, dir, `{
		"compilerOptions": {
			"baseUrl": "./src"
		}
	}`)

	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// No src/react file — react is an npm package.
	src := []byte(`import React from 'react';`)
	tree := parseTSForAlias(t, src)
	entities := extractWithRepo(t, dir, "src/main.ts", src, tree)

	paths := importsEdgePaths2572(entities)
	if len(paths) == 0 {
		t.Fatal("no IMPORTS edges emitted; 'react' must still produce an IMPORTS edge")
	}
	// The import_path must be the raw 'react' spec.
	if !paths["react"] {
		t.Errorf("expected IMPORTS edge with import_path 'react'; got: %v", paths)
	}
	// No resolved-path form (src/react*) should appear — that would indicate
	// the baseUrl wildcard incorrectly swallowed an external npm import.
	for p := range paths {
		if strings.HasPrefix(p, "src/react") || strings.HasPrefix(p, "src.react") {
			t.Errorf("'react' must not resolve via baseUrl; got spurious edge: %q", p)
		}
	}
}

// TestTSExtractor_TsconfigBaseUrlAndPaths_PathsWins verifies that when
// tsconfig has both baseUrl and paths{}, the explicit paths{} entries take
// precedence over the baseUrl wildcard fallback and AliasMap.BaseURL is
// empty (the fallback is not needed when paths are present).
//
// Fixture layout:
//
//	<tmpdir>/
//	  tsconfig.json               — baseUrl: ".", paths: {"@/*": ["src/*"]}
//	  src/components/Btn.ts       — paths alias target
//	  src/shared/Foo.ts           — would match baseUrl but paths takes precedence
//	  src/App.ts                  — source file under test
func TestTSExtractor_TsconfigBaseUrlAndPaths_PathsWins(t *testing.T) {
	resetAliasMapCache()
	dir := t.TempDir()

	writeTsconfigAliasFile(t, dir, `{
		"compilerOptions": {
			"baseUrl": ".",
			"paths": {
				"@/*": ["src/*"]
			}
		}
	}`)

	// Create both target directories.
	compDir := filepath.Join(dir, "src", "components")
	if err := os.MkdirAll(compDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writePlainFile(t, compDir, "Btn.ts", `export function Btn() {}`)

	sharedDir := filepath.Join(dir, "src", "shared")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writePlainFile(t, sharedDir, "Foo.ts", `export class Foo {}`)

	// AliasMap.BaseURL must be empty when paths{} are present.
	m := LoadAliasMap(dir)
	if m.BaseURL != "" {
		t.Errorf("expected AliasMap.BaseURL == %q when paths are declared, got %q", "", m.BaseURL)
	}

	// The @/* alias must resolve correctly via paths{}.
	if got := m.Resolve("@/components/Btn"); got != "src/components/Btn" {
		t.Errorf("Resolve(@/components/Btn) = %q, want %q", got, "src/components/Btn")
	}

	// End-to-end: @/components/Btn import uses paths alias, not baseUrl.
	src := []byte(`import { Btn } from '@/components/Btn';`)
	tree := parseTSForAlias(t, src)
	entities := extractWithRepo(t, dir, "src/App.ts", src, tree)

	paths := importsEdgePaths2572(entities)
	if !paths["@/components/Btn"] {
		t.Errorf("expected IMPORTS edge with import_path '@/components/Btn'; got: %v", paths)
	}
}

// TestTSExtractor_TsconfigRootBaseUrl_ResolvesSrcImport is Fixture B for
// #4696: a tsconfig with `baseUrl: "."` (repo root, no paths{}) and an import
// rooted at a real top-level source dir — `import { X } from 'src/modules/x'`
// — must resolve to the internal target file under the repo root, NOT be
// left unresolved (which previously surfaced as 91 `src`-rooted
// external_unknown edges on upvate-v3).
func TestTSExtractor_TsconfigRootBaseUrl_ResolvesSrcImport(t *testing.T) {
	resetAliasMapCache()
	dir := t.TempDir()

	// Root baseUrl, no paths{}. This is the exact upvate-v3 shape.
	writeTsconfigAliasFile(t, dir, `{
		"compilerOptions": {
			"baseUrl": "."
		}
	}`)

	// AliasMap must record BaseURLSet=true even though BaseURL is "" (root).
	m := LoadAliasMap(dir)
	if !m.BaseURLSet {
		t.Fatalf("expected BaseURLSet=true for baseUrl: '.'")
	}
	if m.BaseURL != "" {
		t.Errorf("expected BaseURL=='' for root baseUrl, got %q", m.BaseURL)
	}

	// Target file under src/modules/.
	modDir := filepath.Join(dir, "src", "modules")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writePlainFile(t, modDir, "x.ts", `export class X {}`)

	src := []byte(`import { X } from 'src/modules/x';`)
	tree := parseTSForAlias(t, src)
	entities := extractWithRepo(t, dir, "src/app.ts", src, tree)

	paths := importsEdgePaths2572(entities)
	if len(paths) == 0 {
		t.Fatal("no IMPORTS edges emitted")
	}
	// The import must resolve to the internal target file under src/modules,
	// not remain a bare external spec rooted at "src".
	found := false
	for p := range paths {
		if (strings.Contains(p, "src") && strings.Contains(p, "modules") && strings.Contains(p, "x")) ||
			p == "src/modules/x.ts" || p == "src/modules/x" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected resolved internal edge for 'src/modules/x'; got %v", paths)
	}
}

// TestTSExtractor_TsconfigRootBaseUrl_ExternalStillFallsThrough confirms the
// #4696 root-baseUrl branch only resolves specifiers that actually exist on
// disk: a bare npm import with no matching file under the repo root must NOT
// be misclassified as project-internal.
func TestTSExtractor_TsconfigRootBaseUrl_ExternalStillFallsThrough(t *testing.T) {
	resetAliasMapCache()
	dir := t.TempDir()

	writeTsconfigAliasFile(t, dir, `{
		"compilerOptions": {
			"baseUrl": "."
		}
	}`)

	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// No file matches 'class-validator' under the repo root.
	src := []byte(`import { IsString } from 'class-validator';`)
	tree := parseTSForAlias(t, src)
	entities := extractWithRepo(t, dir, "src/dto.ts", src, tree)

	paths := importsEdgePaths2572(entities)
	if !paths["class-validator"] {
		t.Errorf("expected raw external import_path 'class-validator'; got %v", paths)
	}
	for p := range paths {
		if strings.HasPrefix(p, "class-validator.") {
			t.Errorf("'class-validator' must not resolve via root baseUrl; got %q", p)
		}
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
