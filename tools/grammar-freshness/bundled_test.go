package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile is a fixture helper: creates parent dirs and writes body.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const fixtureGoMod = `module github.com/cajasmota/grafel

go 1.26

require github.com/alex-pinkus/tree-sitter-swift v0.0.0-20260601004120-31d17fe7e818

require (
	github.com/tree-sitter/go-tree-sitter v0.24.0
	github.com/tree-sitter/tree-sitter-c v0.23.6
	github.com/tree-sitter/tree-sitter-elixir v0.3.4
	github.com/tree-sitter-grammars/tree-sitter-yaml v0.7.2 // indirect
	github.com/stretchr/testify v1.9.0
)

replace github.com/tree-sitter/tree-sitter-elixir v0.3.4 => github.com/elixir-lang/tree-sitter-elixir v0.3.4
`

func TestParseGoModPins_DerivesAPerGrammarVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "go.mod")
	writeFile(t, path, fixtureGoMod)

	pins, err := parseGoModPins(path)
	if err != nil {
		t.Fatalf("parseGoModPins: %v", err)
	}

	// A plain release pin keeps its tag and carries no invented date.
	c, ok := pins["tree-sitter/tree-sitter-c"]
	if !ok {
		t.Fatalf("tree-sitter-c not pinned; got %v", sortedKeys(pins))
	}
	if c.Release != "v0.23.6" {
		t.Errorf("c release = %q, want v0.23.6", c.Release)
	}
	if c.Date != "" {
		t.Errorf("c should carry no date (a release is not a date), got %q", c.Date)
	}

	// A pseudo-version yields a commit date and no release.
	sw, ok := pins["alex-pinkus/tree-sitter-swift"]
	if !ok {
		t.Fatalf("swift not pinned; got %v", sortedKeys(pins))
	}
	if sw.Date != "2026-06-01" {
		t.Errorf("swift date = %q, want 2026-06-01", sw.Date)
	}
	if sw.Release != "" {
		t.Errorf("swift release = %q, want empty (pseudo-version is not a release)", sw.Release)
	}

	// A replace directive re-homes the pin onto the replacement's slug.
	if _, ok := pins["tree-sitter/tree-sitter-elixir"]; ok {
		t.Error("replaced module must not be reported under its original path")
	}
	if el, ok := pins["elixir-lang/tree-sitter-elixir"]; !ok || el.Release != "v0.3.4" {
		t.Errorf("elixir pin = %+v, want elixir-lang @ v0.3.4", el)
	}

	// The runtime binding and unrelated deps are not grammars.
	for _, unwanted := range []string{"tree-sitter/go-tree-sitter", "stretchr/testify"} {
		if _, ok := pins[unwanted]; ok {
			t.Errorf("%s must not be treated as a grammar pin", unwanted)
		}
	}

	// Indirect requires still pin a grammar.
	if _, ok := pins["tree-sitter-grammars/tree-sitter-yaml"]; !ok {
		t.Error("indirect grammar require should still be a pin")
	}
}

const fixtureKotlinGo = `// Package kotlin ...
//
// Vendored source — license/attribution (license-audit gate):
//
//	source: github.com/fwcd/tree-sitter-kotlin
//	ref:    e1a2d5ad1f61f5740677183cd4125bb071cd2f30 (0.3.8, 2024-08-03)
//	license: MIT
package kotlin
`

const fixtureProtoGo = `// Package proto ...
//	source: github.com/mitchellh/tree-sitter-proto
//	ref:    42d82fa18f8afe59b5fc0b16c207ee4f84cb185f (master, 2021-06-12)
package proto
`

func TestParseVendoredPins_ReadsTheRefHeader(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "kotlin", "kotlin.go"), fixtureKotlinGo)
	writeFile(t, filepath.Join(root, "proto", "proto.go"), fixtureProtoGo)

	pins, err := parseVendoredPins(root)
	if err != nil {
		t.Fatalf("parseVendoredPins: %v", err)
	}

	k, ok := pins["fwcd/tree-sitter-kotlin"]
	if !ok {
		t.Fatalf("kotlin not found; got %v", sortedKeys(pins))
	}
	if k.Release != "0.3.8" || k.Date != "2024-08-03" {
		t.Errorf("kotlin pin = %+v, want release 0.3.8 date 2024-08-03", k)
	}
	if !strings.Contains(k.Origin, "kotlin.go") {
		t.Errorf("kotlin Origin = %q, want the vendored file path", k.Origin)
	}

	// "master" is a branch, not a release: it must NOT be recorded as one.
	p := pins["mitchellh/tree-sitter-proto"]
	if p.Release != "" {
		t.Errorf("proto release = %q, want empty (master is not a release)", p.Release)
	}
	if p.Date != "2021-06-12" {
		t.Errorf("proto date = %q, want 2021-06-12", p.Date)
	}
}

// TestLoadPins_RealRepo is the anti-vacuity anchor: the pins below are read from
// the real go.mod and the real vendored headers, not from fixtures. It does NOT
// claim every lock language resolves — groovy does not, because its header
// records only "(HEAD, 2024)"; that case is pinned by
// TestParseVendoredPins_UnreadableRefIsQuotedNotDenied.
func TestLoadPins_RealRepo(t *testing.T) {
	pins, err := loadPins("../../go.mod", "../../internal/treesitter/ts/grammars")
	if err != nil {
		t.Fatalf("loadPins: %v", err)
	}
	// smacker is gone from this repo; nothing may resolve to it.
	for _, slug := range pins.Sources() {
		if strings.Contains(slug, "smacker") {
			t.Errorf("resolved a smacker pin %q — that module is not in go.mod", slug)
		}
	}
	// kotlin is vendored at 0.3.8 (fwcd), the release the cron calls "24 mo behind".
	k, ok := pins.Get("fwcd/tree-sitter-kotlin")
	if !ok {
		t.Fatal("kotlin pin not resolved from the real repo")
	}
	if k.Release != "0.3.8" {
		t.Errorf("kotlin release = %q, want 0.3.8", k.Release)
	}
	// swift is a 2026-06-01 pseudo-version in go.mod, not a 2024 snapshot.
	sw, ok := pins.Get("alex-pinkus/tree-sitter-swift")
	if !ok {
		t.Fatal("swift pin not resolved from the real repo")
	}
	if sw.Date != "2026-06-01" {
		t.Errorf("swift date = %q, want 2026-06-01", sw.Date)
	}
}

// TestParseVendoredPins_DateShapedTagIsNotARelease is the guard for the worst
// under-reporting path: a header whose version token is a dotted date. Read as
// a release it becomes [2024,5,9], which beats every real tree-sitter tag, so
// the grammar reports CURRENT no matter how far behind it is.
func TestParseVendoredPins_DateShapedTagIsNotARelease(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "datey", "datey.go"), `// Package datey ...
//	source: github.com/example/tree-sitter-datey
//	ref:    abc123 (2024.05.09)
package datey
`)
	pins, err := parseVendoredPins(root)
	if err != nil {
		t.Fatalf("parseVendoredPins: %v", err)
	}
	p := pins["example/tree-sitter-datey"]
	if p.Release != "" {
		t.Errorf("a dotted date was accepted as release %q — it outranks every real tag", p.Release)
	}
	if p.Date != "2024-05-09" {
		t.Errorf("date = %q, want it normalised to 2024-05-09", p.Date)
	}

	// End to end: such a pin must never read CURRENT against a newer upstream.
	lock := &Lock{Grammars: []GrammarSpec{{Language: "datey", Source: "example/tree-sitter-datey"}}}
	rep := check(context.Background(), lock, Pins{bySource: pins}, fakeSource{data: map[string]Upstream{
		"example/tree-sitter-datey": {Release: "v0.24.2", CommitDate: "2026-04-22", Kind: "release"},
	}})
	if !rep.Grammars[0].Stale {
		t.Errorf("a 2024 pin against a 2026 upstream must be STALE, got %+v", rep.Grammars[0])
	}
}

// TestParseVendoredPins_AcceptsRealHeaderVariants covers header shapes that are
// well-formed but not byte-identical to the commonest one. Dropping them is bad;
// dropping them and then reporting "records no release or commit date" is worse.
func TestParseVendoredPins_AcceptsRealHeaderVariants(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "forky", "forky.go"), `// Package forky ...
//	source: github.com/example/tree-sitter-forky (fork)
//	ref:    abc123 (v0.2.0, 2024-05-09)
package forky
`)
	writeFile(t, filepath.Join(root, "labelled", "labelled.go"), `// Package labelled ...
//	source: github.com/example/tree-sitter-labelled
//	ref:    abc123 (tag v0.2.0, date 2024-05-09)
package labelled
`)
	writeFile(t, filepath.Join(root, "spaced", "spaced.go"), `// Package spaced ...
//	source: github.com/example/tree-sitter-spaced
//	ref:    abc123 (v0.2.0 2024-05-09)
package spaced
`)

	pins, err := parseVendoredPins(root)
	if err != nil {
		t.Fatalf("parseVendoredPins: %v", err)
	}
	for _, slug := range []string{
		"example/tree-sitter-forky",
		"example/tree-sitter-labelled",
		"example/tree-sitter-spaced",
	} {
		p, ok := pins[slug]
		if !ok {
			t.Errorf("%s was dropped entirely; got %v", slug, sortedKeys(pins))
			continue
		}
		if p.Release != "v0.2.0" {
			t.Errorf("%s release = %q, want v0.2.0", slug, p.Release)
		}
		if p.Date != "2024-05-09" {
			t.Errorf("%s date = %q, want 2024-05-09", slug, p.Date)
		}
	}
}

// TestParseVendoredPins_UnreadableRefIsQuotedNotDenied: when a ref genuinely
// carries no version, the diagnostic must quote it rather than claim the file
// records nothing.
func TestParseVendoredPins_UnreadableRefIsQuotedNotDenied(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "branchy", "branchy.go"), `// Package branchy ...
//	source: github.com/example/tree-sitter-branchy
//	ref:    abc123 (HEAD, 2024)
package branchy
`)
	pins, err := parseVendoredPins(root)
	if err != nil {
		t.Fatalf("parseVendoredPins: %v", err)
	}
	p := pins["example/tree-sitter-branchy"]
	if p.Release != "" || p.Date != "" {
		t.Errorf("a branch name and a bare year are neither release nor date, got %+v", p)
	}
	if p.RawRef != "HEAD, 2024" {
		t.Errorf("RawRef = %q, want the header text verbatim", p.RawRef)
	}

	lock := &Lock{Grammars: []GrammarSpec{{Language: "branchy", Source: "example/tree-sitter-branchy"}}}
	rep := check(context.Background(), lock, Pins{bySource: pins}, fakeSource{})
	r := rep.Grammars[0]
	if !r.Unknown {
		t.Fatalf("want UNKNOWN, got %+v", r)
	}
	if !strings.Contains(r.Reason, "HEAD, 2024") {
		t.Errorf("reason must quote the recorded ref, got %q", r.Reason)
	}
	if strings.Contains(r.Reason, "records no release or commit date") {
		t.Errorf("reason asserts the file records nothing, but it records %q: %q", p.RawRef, r.Reason)
	}
}

func TestCompareRelease(t *testing.T) {
	cases := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"v0.23.6", "v0.24.2", -1, true},
		{"0.3.8", "0.3.8", 0, true},
		{"v0.3.8", "0.3.8", 0, true}, // the "v" prefix is cosmetic
		{"v1.2.0", "v1.1.1", 1, true},
		{"v0.25.0", "v0.9.0", 1, true},  // numeric, not lexical: 25 > 9
		{"initial", "v0.1.0", 0, false}, // not a release identifier
		{"v0.23.6", "HEAD", 0, false},
		// A pre-release compares EQUAL to its release: under-reports, never over.
		{"v0.25.0-rc1", "v0.25.0", 0, true},
		{"v0.25.0", "v0.25.0-rc1", 0, true},
		{"v0.24.0-rc1", "v0.25.0", -1, true},
		// Dates are NOT releases, in either separator, on either side. A dotted
		// date parses as [2024,5,9] and would outrank every real tag, silently
		// turning a behind grammar into CURRENT (#6749's direction).
		{"2024.05.09", "v0.24.2", 0, false},
		{"2024-05-09", "v0.1.0", 0, false},
		{"v0.24.2", "2026.04.22", 0, false},
		{"2024-08-27", "2026-09-01", 0, false},
	}
	for _, c := range cases {
		got, ok := compareRelease(c.a, c.b)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("compareRelease(%q,%q) = %d,%v want %d,%v", c.a, c.b, got, ok, c.want, c.ok)
		}
	}
}
