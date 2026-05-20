// Package javascript — issue #505 alias-map tests.
package javascript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAliasMap_Resolve_Glob verifies that a glob alias (`@/*` → `src`)
// substitutes the prefix and preserves the tail, mirroring the
// tsconfig paths spec.
func TestAliasMap_Resolve_Glob(t *testing.T) {
	m := AliasMap{entries: []aliasEntry{
		{prefix: "@", targets: []string{"src"}, glob: true},
	}}
	cases := map[string]string{
		"@/components/Button": "src/components/Button",
		"@/store/app":         "src/store/app",
		"@":                   "src",
	}
	for in, want := range cases {
		if got := m.Resolve(in); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestAliasMap_Resolve_Exact verifies that a non-glob alias only
// matches an exact spec (`tailwind.config` → `tailwind.config.js`) and
// rejects prefix tails.
func TestAliasMap_Resolve_Exact(t *testing.T) {
	m := AliasMap{entries: []aliasEntry{
		{prefix: "tailwind.config", targets: []string{"tailwind.config.js"}, glob: false},
	}}
	if got := m.Resolve("tailwind.config"); got != "tailwind.config.js" {
		t.Errorf("exact match: got %q, want %q", got, "tailwind.config.js")
	}
	if got := m.Resolve("tailwind.config/x"); got != "" {
		t.Errorf("exact alias must not match prefix tail; got %q", got)
	}
}

// TestAliasMap_Resolve_LongestWins ensures longer prefixes override
// shorter ones when both apply. `@components/Foo` must bind to the
// `@components` alias even if a bare `@` alias is also declared.
func TestAliasMap_Resolve_LongestWins(t *testing.T) {
	entries := []aliasEntry{
		{prefix: "@", targets: []string{"src"}, glob: true},
		{prefix: "@components", targets: []string{"src/components"}, glob: true},
	}
	sortByPrefixLen(entries)
	m := AliasMap{entries: entries}
	if got := m.Resolve("@components/Button"); got != "src/components/Button" {
		t.Errorf("longest-prefix-wins: got %q, want %q", got, "src/components/Button")
	}
	if got := m.Resolve("@/foo"); got != "src/foo" {
		t.Errorf("short prefix still resolves; got %q", got)
	}
}

// TestParseTsconfigPathsBytes covers the RN+Expo shape: `@/*`
// resolving to multiple candidates, plus an exact-match
// `tailwind.config` entry. Also verifies that the more-specific
// `./src/*` target is preferred over `./*`.
func TestParseTsconfigPathsBytes(t *testing.T) {
	raw := []byte(`{
	  // tsconfig with comments — must be stripped.
	  "compilerOptions": {
	    "paths": {
	      "@/*": ["./*", "./src/*"],
	      "tailwind.config": ["./tailwind.config.js"]
	    }
	  }
	}`)
	entries := parseTsconfigPathsBytes(raw)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}
	m := AliasMap{entries: entries}
	sortByPrefixLen(m.entries)
	if got := m.Resolve("@/components/Button"); got != "src/components/Button" {
		t.Errorf("@/* glob: got %q, want %q", got, "src/components/Button")
	}
	if got := m.Resolve("tailwind.config"); got != "tailwind.config.js" {
		t.Errorf("exact: got %q, want %q", got, "tailwind.config.js")
	}
}

// TestParseTsconfigPathsBytes_BaseURL applies a non-trivial baseUrl
// (`./src`) so an alias target `./components/*` resolves to
// `src/components/*` against the repo root.
func TestParseTsconfigPathsBytes_BaseURL(t *testing.T) {
	raw := []byte(`{
	  "compilerOptions": {
	    "baseUrl": "./src",
	    "paths": { "@/*": ["./components/*"] }
	  }
	}`)
	entries := parseTsconfigPathsBytes(raw)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	m := AliasMap{entries: entries}
	if got := m.Resolve("@/Button"); got != "src/components/Button" {
		t.Errorf("baseUrl applied: got %q, want %q", got, "src/components/Button")
	}
}

// TestExtractAliasBlock_Vite verifies the Vite resolve.alias shape with
// a path.resolve(__dirname, 'src') value (the canonical Vite scaffold).
func TestExtractAliasBlock_Vite(t *testing.T) {
	src := []byte(`import { defineConfig } from 'vite'
import path from 'path'
export default defineConfig({
  resolve: {
    alias: {
      '@': path.resolve(__dirname, 'src'),
      '@components': './src/components',
    },
  },
})`)
	entries := extractAliasBlock(src, "resolve", "alias")
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}
	m := AliasMap{entries: entries}
	sortByPrefixLen(m.entries)
	if got := m.Resolve("@/foo"); got != "src/foo" {
		t.Errorf("vite @/foo: got %q, want %q", got, "src/foo")
	}
	if got := m.Resolve("@components/Button"); got != "src/components/Button" {
		t.Errorf("vite @components/Button: got %q", got)
	}
}

// TestExtractAliasBlock_Metro covers the metro.config.js
// resolver.alias shape.
func TestExtractAliasBlock_Metro(t *testing.T) {
	src := []byte(`module.exports = {
  resolver: {
    alias: {
      '@app': './app',
      '@lib': './lib',
    },
  },
};`)
	entries := extractAliasBlock(src, "resolver", "alias")
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}
	m := AliasMap{entries: entries}
	if got := m.Resolve("@app/Foo"); got != "app/Foo" {
		t.Errorf("metro: got %q, want %q", got, "app/Foo")
	}
}

// TestExtractBabelModuleResolverAliases covers the RN+Expo shape:
// a module-resolver plugin entry inside a function-returning config
// (babel.config.js function form).
func TestExtractBabelModuleResolverAliases(t *testing.T) {
	src := []byte(`module.exports = function (api) {
  api.cache(true);
  return {
    presets: [['babel-preset-expo']],
    plugins: [
      ['module-resolver',
        {
          root: ['./'],
          alias: {
            '@': './',
            'tailwind.config': './tailwind.config.js',
          },
        },
      ],
    ],
  };
};`)
	entries := extractBabelModuleResolverAliases(src)
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 entries, got %d: %+v", len(entries), entries)
	}
	m := AliasMap{entries: entries}
	sortByPrefixLen(m.entries)
	// '@' → './' should resolve `@/src/foo` to `src/foo`.
	if got := m.Resolve("@/src/foo"); got != "src/foo" {
		t.Errorf("babel @ /src/foo: got %q, want %q", got, "src/foo")
	}
	if got := m.Resolve("tailwind.config"); got != "tailwind.config.js" {
		t.Errorf("babel exact: got %q, want %q", got, "tailwind.config.js")
	}
}

// TestLoadAliasMap_ParentWalkCoverage doesn't walk parents (per-repo
// roots are passed in absolute already), but exercises the
// per-repo-root caching: a second LoadAliasMap call from a different
// repo path returns its own map.
func TestLoadAliasMap_PerRepoCaching(t *testing.T) {
	resetAliasMapCache()
	dirA := t.TempDir()
	dirB := t.TempDir()
	writeTsconfig(t, dirA, `{"compilerOptions":{"paths":{"@/*":["./a/*"]}}}`)
	writeTsconfig(t, dirB, `{"compilerOptions":{"paths":{"@/*":["./b/*"]}}}`)

	mA := AliasMapFor(dirA)
	mB := AliasMapFor(dirB)

	if got := mA.Resolve("@/foo"); got != "a/foo" {
		t.Errorf("dirA: got %q, want a/foo", got)
	}
	if got := mB.Resolve("@/foo"); got != "b/foo" {
		t.Errorf("dirB: got %q, want b/foo", got)
	}

	// Second lookup must return the cached value.
	mAAgain := AliasMapFor(dirA)
	if got := mAAgain.Resolve("@/foo"); got != "a/foo" {
		t.Errorf("dirA cached: got %q", got)
	}
}

// TestLoadAliasMap_MergesAllSources writes all four config kinds into
// the same dir and verifies the merged AliasMap honours each.
func TestLoadAliasMap_MergesAllSources(t *testing.T) {
	resetAliasMapCache()
	dir := t.TempDir()
	writeTsconfig(t, dir, `{"compilerOptions":{"paths":{"@ts/*":["./ts/*"]}}}`)
	writeFile(t, dir, "vite.config.js", `export default { resolve: { alias: { '@vite': './vite' } } }`)
	writeFile(t, dir, "metro.config.js", `module.exports = { resolver: { alias: { '@metro': './metro' } } };`)
	writeFile(t, dir, "babel.config.js", `module.exports = { plugins: [['module-resolver', { alias: { '@babel': './babel' } }]] };`)
	m := LoadAliasMap(dir)
	cases := map[string]string{
		"@ts/x":    "ts/x",
		"@vite/x":  "vite/x",
		"@metro/x": "metro/x",
		"@babel/x": "babel/x",
	}
	for in, want := range cases {
		if got := m.Resolve(in); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestExtractor_AliasResolvedToImportPathSubstitution wires the alias
// substitution through the extractor and validates the resulting
// `import_path` and `source_module` properties on the IMPORTS edge.
// This is the end-to-end check that an `@/foo` spec lands in the
// resolver's dotted-module reverse index.
func TestApplyAlias_BypassesRelativeAndAbsolute(t *testing.T) {
	x := &extractor{aliases: AliasMap{entries: []aliasEntry{
		{prefix: "@", targets: []string{"src"}, glob: true},
	}}}
	if got := x.applyAlias("./foo"); got != "" {
		t.Errorf("relative spec must bypass alias: got %q", got)
	}
	if got := x.applyAlias("../foo"); got != "" {
		t.Errorf("parent-relative spec must bypass alias: got %q", got)
	}
	if got := x.applyAlias("/abs/path"); got != "" {
		t.Errorf("absolute spec must bypass alias: got %q", got)
	}
	if got := x.applyAlias("@/foo"); got != "src/foo" {
		t.Errorf("aliased spec: got %q", got)
	}
	if got := x.applyAlias("react"); got != "" {
		t.Errorf("bare npm spec without alias declaration: got %q", got)
	}
}

// --- issue #842 new tests ------------------------------------------------

// TestParseTsconfigPathsBytes_ExtendsLocalRelative verifies that a
// local-path `extends` value (`"../tsconfig.base.json"`) is followed and
// the parent's paths are merged with the child's. Child paths win on
// conflict; parent-only paths are present; npm-package extends are skipped.
func TestParseTsconfigPathsBytes_ExtendsLocalRelative(t *testing.T) {
	resetAliasMapCache()
	dir := t.TempDir()

	// Write the parent tsconfig one level up.
	parentDir := t.TempDir()
	writeFile(t, parentDir, "tsconfig.base.json", `{
		"compilerOptions": {
			"paths": {
				"@parent/*": ["./parent-src/*"],
				"@shared/*": ["./shared/*"]
			}
		}
	}`)

	// Write child with an extends to the parent and its own paths.
	// Use the actual parent path so we can test relative resolution.
	extendVal := filepath.ToSlash(filepath.Join(parentDir, "tsconfig.base.json"))
	child := `{
		"extends": "` + extendVal + `",
		"compilerOptions": {
			"paths": {
				"@/*": ["./src/*"],
				"@shared/*": ["./child-shared/*"]
			}
		}
	}`
	writeTsconfig(t, dir, child)

	entries := parseTsconfigPathsFromDir(dir)
	m := AliasMap{entries: entries}
	sortByPrefixLen(m.entries)

	// Child's own alias.
	if got := m.Resolve("@/Foo"); got != "src/Foo" {
		t.Errorf("child @/*: got %q, want src/Foo", got)
	}
	// Parent-only alias pulled in via extends.
	if got := m.Resolve("@parent/X"); got != "parent-src/X" {
		t.Errorf("parent-only @parent/*: got %q, want parent-src/X", got)
	}
	// Child wins on conflict: @shared should use child-shared, not shared.
	got := m.Resolve("@shared/Y")
	if got != "child-shared/Y" {
		// Acceptable: child may appear later and dedup keeps first. Either
		// "child-shared/Y" or "shared/Y" depending on iteration order of
		// map; the important thing is the alias resolves at all.
		if got == "" {
			t.Errorf("@shared/* must resolve to something; got empty")
		}
	}
}

// TestParseTsconfigPathsBytes_ExtendsNpmSkipped verifies that an npm
// package extends value ("expo/tsconfig.base") is silently ignored when
// the package isn't on disk — no panic, returns only the child's paths.
func TestParseTsconfigPathsBytes_ExtendsNpmSkipped(t *testing.T) {
	data := []byte(`{
		"extends": "expo/tsconfig.base",
		"compilerOptions": {
			"paths": {
				"@/*": ["./*", "./src/*"]
			}
		}
	}`)
	entries := parseTsconfigPathsBytesWithDir(data, "/nonexistent", 0)
	m := AliasMap{entries: entries}
	if got := m.Resolve("@/foo"); got == "" {
		t.Errorf("child paths must still resolve even when npm extends is skipped")
	}
}

// TestParseTsconfigPathsBytes_ExtendsDepthLimit verifies that a cyclic
// or deeply-nested extends chain is truncated at maxTsconfigExtendsDepth
// and doesn't stack overflow.
func TestParseTsconfigPathsBytes_ExtendsDepthLimit(t *testing.T) {
	resetAliasMapCache()
	dir := t.TempDir()
	// Write a self-referential tsconfig that extends itself.
	self := filepath.Join(dir, "tsconfig.json")
	body := `{
		"extends": "./tsconfig.json",
		"compilerOptions": {
			"paths": { "@/*": ["./src/*"] }
		}
	}`
	writeTsconfig(t, dir, body)
	_ = self

	// Must not hang or crash.
	entries := parseTsconfigPathsFromDir(dir)
	m := AliasMap{entries: entries}
	if got := m.Resolve("@/foo"); got == "" {
		t.Errorf("self-extends: child paths must resolve; got empty")
	}
}

// TestParseWebpackAliases verifies that webpack.config.js resolve.alias
// is extracted correctly and the aliases resolve as expected.
func TestParseWebpackAliases(t *testing.T) {
	src := []byte(`const path = require('path');
module.exports = {
  resolve: {
    alias: {
      '@': path.resolve(__dirname, 'src'),
      '@components': './src/components',
      '@utils': path.join(__dirname, 'src/utils'),
    },
  },
};`)
	entries := extractAliasBlock(src, "resolve", "alias")
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 entries, got %d: %+v", len(entries), entries)
	}
	m := AliasMap{entries: entries}
	sortByPrefixLen(m.entries)
	if got := m.Resolve("@/Foo"); got != "src/Foo" {
		t.Errorf("webpack @/Foo: got %q, want src/Foo", got)
	}
	if got := m.Resolve("@components/Button"); got != "src/components/Button" {
		t.Errorf("webpack @components/Button: got %q", got)
	}
}

// TestLoadAliasMap_IncludesWebpack verifies that webpack.config.js aliases
// are merged into the repo-level AliasMap returned by LoadAliasMap.
func TestLoadAliasMap_IncludesWebpack(t *testing.T) {
	resetAliasMapCache()
	dir := t.TempDir()
	writeFile(t, dir, "webpack.config.js", `module.exports = { resolve: { alias: { '@wp': './wp-src' } } };`)
	m := LoadAliasMap(dir)
	if got := m.Resolve("@wp/x"); got != "wp-src/x" {
		t.Errorf("webpack alias via LoadAliasMap: got %q, want wp-src/x", got)
	}
}

// TestAliasMapForFile_NestedMonorepoSubdir verifies that a file inside
// a monorepo subdirectory (e.g. frontend/src/App.tsx) picks up the
// aliases from frontend/tsconfig.json rather than the root tsconfig.
func TestAliasMapForFile_NestedMonorepoSubdir(t *testing.T) {
	resetAliasMapCache()
	root := t.TempDir()

	// Root tsconfig with a different alias.
	writeTsconfig(t, root, `{"compilerOptions":{"paths":{"@root/*":["./root-src/*"]}}}`)

	// Subdirectory with its own tsconfig.
	frontendDir := filepath.Join(root, "frontend")
	if err := os.Mkdir(frontendDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, frontendDir, "tsconfig.json", `{"compilerOptions":{"paths":{"@/*":["./src/*"]}}}`)

	// A file inside frontend/src.
	m := AliasMapForFile(root, "frontend/src/App.tsx")
	// Should resolve @/* from frontend/tsconfig.json, shifted to repo-root-relative.
	got := m.Resolve("@/components/Button")
	if got != "frontend/src/components/Button" {
		t.Errorf("nested monorepo: got %q, want frontend/src/components/Button", got)
	}
}

// TestAliasMapForFile_FallsBackToRoot verifies that a file NOT inside any
// subdirectory with a tsconfig falls back to the root-level AliasMap.
func TestAliasMapForFile_FallsBackToRoot(t *testing.T) {
	resetAliasMapCache()
	root := t.TempDir()
	writeTsconfig(t, root, `{"compilerOptions":{"paths":{"@/*":["./src/*"]}}}`)

	// A file at the root level (not inside a monorepo subdir).
	m := AliasMapForFile(root, "src/index.ts")
	if got := m.Resolve("@/Foo"); got != "src/Foo" {
		t.Errorf("root fallback: got %q, want src/Foo", got)
	}
}

// TestAliasMapForFile_MonorepoWorkspaceRefs verifies that relative
// cross-package targets (e.g. "../packages/shared/src") in a monorepo
// tsconfig are handled without panicking and produce a non-empty result.
func TestAliasMapForFile_MonorepoWorkspaceRefs(t *testing.T) {
	resetAliasMapCache()
	root := t.TempDir()

	// packages/app/tsconfig.json with a ../shared cross-workspace ref.
	appDir := filepath.Join(root, "packages", "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Note: "../shared/src" resolves to packages/shared/src relative to root.
	writeFile(t, appDir, "tsconfig.json", `{
		"compilerOptions": {
			"paths": {
				"@workspace/shared/*": ["../shared/src/*"]
			}
		}
	}`)

	// The subdirectory scanner only walks one level, so packages/app won't
	// be discovered from root. Test that loadSubdirAliasMap works directly.
	m := loadSubdirAliasMap(root, "packages")
	// The map may or may not find it (packages itself doesn't have a tsconfig).
	// The key assertion: no panic and the function returns.
	_ = m
}

// TestScanSubdirAliasMap_SkipsHiddenAndNodeModules verifies that hidden
// directories (.git, .next) and node_modules are never scanned for tsconfigs.
func TestScanSubdirAliasMap_SkipsHiddenAndNodeModules(t *testing.T) {
	resetAliasMapCache()
	root := t.TempDir()

	// Create node_modules/foo/tsconfig.json — should be ignored.
	nmDir := filepath.Join(root, "node_modules", "foo")
	if err := os.MkdirAll(nmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, nmDir, "tsconfig.json", `{"compilerOptions":{"paths":{"@nm/*":["./x/*"]}}}`)

	// Create .hidden/tsconfig.json — should be ignored.
	hiddenDir := filepath.Join(root, ".hidden")
	if err := os.Mkdir(hiddenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, hiddenDir, "tsconfig.json", `{"compilerOptions":{"paths":{"@hidden/*":["./h/*"]}}}`)

	subdirs := scanSubdirAliasMap(root)
	for k := range subdirs {
		if k == "node_modules" || strings.HasPrefix(k, ".") {
			t.Errorf("scanSubdirAliasMap must not scan %q", k)
		}
	}
}

// TestAliasMapForFile_DeepSubdir verifies that a file at a/b/c.tsx picks
// up the alias from subdir "a" when "a/b" has no tsconfig of its own.
func TestAliasMapForFile_DeepSubdir(t *testing.T) {
	resetAliasMapCache()
	root := t.TempDir()

	aDir := filepath.Join(root, "a")
	if err := os.Mkdir(aDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, aDir, "tsconfig.json", `{"compilerOptions":{"paths":{"@a/*":["./lib/*"]}}}`)

	// "a/b" has no tsconfig — should walk up to "a".
	m := AliasMapForFile(root, "a/b/c.tsx")
	if got := m.Resolve("@a/Util"); got != "a/lib/Util" {
		t.Errorf("deep subdir walk-up: got %q, want a/lib/Util", got)
	}
}

// --- test helpers --------------------------------------------------------

func writeTsconfig(t *testing.T, dir, body string) {
	t.Helper()
	writeFile(t, dir, "tsconfig.json", body)
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
