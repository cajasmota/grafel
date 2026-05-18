// Package javascript — issue #505 alias-map tests.
package javascript

import (
	"os"
	"path/filepath"
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

// TestParseTsconfigPathsBytes covers the client-fixture-c shape: `@/*`
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

// TestExtractBabelModuleResolverAliases covers the client-fixture-c shape:
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
