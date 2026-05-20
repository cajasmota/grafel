// Package javascript — unit tests for issue #523: Vite/Babel alias parsing
// extended to handle object-spread, computed keys, and ENV-based ternary.
package javascript

import (
	"testing"
)

// TestAliasObjectSpread — `{ ...sharedAliases, '@foo': './foo' }` must extract
// the literal entries and silently skip the spread.
func TestAliasObjectSpread(t *testing.T) {
	// Simulate the output of extractObjectLiteral on a spread object literal.
	obj := `{ ...sharedAliases, '@foo': './foo', '@bar': './bar' }`
	entries := parseAliasObjectLiteral(obj)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (spread skipped), got %d: %+v", len(entries), entries)
	}
	m := AliasMap{entries: entries}
	sortByPrefixLen(m.entries)
	if got := m.Resolve("@foo/btn"); got != "foo/btn" {
		t.Errorf("@foo glob: got %q, want %q", got, "foo/btn")
	}
	if got := m.Resolve("@bar/x"); got != "bar/x" {
		t.Errorf("@bar glob: got %q, want %q", got, "bar/x")
	}
}

// TestAliasComputedKey — `{ [\`${prefix}/foo\`]: './foo' }` must be silently
// skipped; no panic and no spurious entry.
func TestAliasComputedKey(t *testing.T) {
	obj := "{ [`${prefix}/foo`]: './foo', '@bar': './bar' }"
	entries := parseAliasObjectLiteral(obj)
	// Only the literal entry survives — computed key is skipped.
	for _, e := range entries {
		if e.prefix == "" {
			t.Errorf("empty prefix entry should not be emitted")
		}
	}
	// At least the literal entry must be present.
	found := false
	for _, e := range entries {
		if e.prefix == "@bar" {
			found = true
		}
	}
	if !found {
		t.Errorf("literal entry @bar not found in %+v", entries)
	}
}

// TestAliasENVTernary — `alias: cond ? { '@test': './test' } : { '@prod': './prod' }`
// must return entries from BOTH branches.
func TestAliasENVTernary(t *testing.T) {
	fragment := `alias: process.env.NODE_ENV === 'test' ? { '@test': './test-utils' } : { '@prod': './prod-utils' }`
	entries := extractAliasValue(fragment)
	if len(entries) == 0 {
		t.Fatal("expected entries from ternary branches, got none")
	}
	hasProd := false
	hasTest := false
	for _, e := range entries {
		if e.prefix == "@test" {
			hasTest = true
		}
		if e.prefix == "@prod" {
			hasProd = true
		}
	}
	if !hasTest {
		t.Errorf("@test entry missing from ternary consequent: %+v", entries)
	}
	if !hasProd {
		t.Errorf("@prod entry missing from ternary alternate: %+v", entries)
	}
}

// TestAliasENVTernary_ViteConfig — integration-level: parse a vite.config.js
// body containing a ternary alias declaration. Verifies extractAliasBlock
// handles it end-to-end.
func TestAliasENVTernary_ViteConfig(t *testing.T) {
	src := []byte(`
import { defineConfig } from 'vite'
import path from 'path'
export default defineConfig({
  resolve: {
    alias: process.env.VITEST
      ? { '@': path.resolve(__dirname, 'src'), '@test': path.resolve(__dirname, 'test') }
      : { '@': path.resolve(__dirname, 'src') }
  }
})
`)
	entries := extractAliasBlock(src, "resolve", "alias")
	m := AliasMap{entries: entries}
	sortByPrefixLen(m.entries)
	if got := m.Resolve("@/components/Button"); got != "src/components/Button" {
		t.Errorf("@ glob: got %q, want %q", got, "src/components/Button")
	}
	// @test should come from the consequent branch.
	if got := m.Resolve("@test/helpers"); got == "" {
		t.Errorf("@test entry from ternary consequent missing")
	}
}

// TestAliasObjectSpread_ViteConfig — vite.config.js with spread alias shape.
func TestAliasObjectSpread_ViteConfig(t *testing.T) {
	src := []byte(`
const sharedAliases = { '@shared': './shared' }
export default {
  resolve: {
    alias: { ...sharedAliases, '@app': './src' }
  }
}
`)
	entries := extractAliasBlock(src, "resolve", "alias")
	found := false
	for _, e := range entries {
		if e.prefix == "@app" {
			found = true
		}
	}
	if !found {
		t.Errorf("@app entry not found in spread alias object; entries: %+v", entries)
	}
}
