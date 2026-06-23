//go:build ts_official

package treesitter

import (
	"fmt"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tsbash "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/bash"
	tsc "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/c"
	tscpp "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/cpp"
	tscsharp "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/csharp"
	tscss "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/css"
	tselixir "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/elixir"
	tsgolang "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/golang"
	tshtml "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/html"
	tsjava "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/java"
	tsjavascript "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/javascript"
	tslua "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/lua"
	tsocaml "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/ocaml"
	tsphp "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/php"
	tspython "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/python"
	tsruby "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/ruby"
	tsrust "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/rust"
	tsscala "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/scala"
	tsswift "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/swift"
	tstoml "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/toml"
	tstypescript "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/typescript"
	tsyaml "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/yaml"
	tsofficial "github.com/cajasmota/grafel/internal/treesitter/ts/official"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
)

// ts_official build: Go (B2 Phase 0) plus the high-value batch — python, java,
// csharp, typescript (+tsx), javascript, rust (B2 cutover Part A, #5418) — are
// migrated onto the official tree-sitter/go-tree-sitter binding (ADR 0023).
// Every other language stays on smacker for now.
// NOTE: this build links BOTH runtimes and currently fails at link time on
// macOS (the co-link blocker — see adapters_default.go). It exists to exercise
// and CI-test the official adapter + migrated grammars + ABI guard in isolation
// and on platforms/toolchains where multiple-definition linking is permitted;
// resolving the co-link is the eventual default-flip (cutover §7).

// officialAdapter is the official binding adapter (only compiled under the tag).
var officialAdapter = tsofficial.New()

// migratedLanguages maps each migrated language to its official ts.Language.
// Phase 0 migrated Go; B2 cutover A1 (#5418) adds the high-value batch. The
// registry key "tsx" routes .tsx/.jsx files to the TSX grammar (the JSX-enabled
// superset) from the same tree-sitter-typescript module.
var migratedLanguages = map[string]ts.Language{
	"go":         tsgolang.Language(),
	"python":     tspython.Language(),
	"java":       tsjava.Language(),
	"csharp":     tscsharp.Language(),
	"typescript": tstypescript.Language(),
	"tsx":        tstypescript.LanguageTSX(),
	"javascript": tsjavascript.Language(),
	"rust":       tsrust.Language(),
	// B2 cutover Part B (#5418): additive provider batch — all ABI 14.
	"bash": tsbash.Language(),
	"c":    tsc.Language(),
	"cpp":  tscpp.Language(),
	"css":  tscss.Language(),
	"html": tshtml.Language(),
	"ruby": tsruby.Language(),
	// B2 cutover Part B batch 2 (#5418): official-binding providers — all ABI ≤14.
	// kotlin and sql are deferred: their go bindings #include a generated
	// src/parser.c that is unreachable from a module download (kotlin's lives
	// outside the nested bindings/go module boundary; DerekStride/tree-sitter-sql
	// .gitignores src/parser.c, so it is never committed). Both need the
	// vendored-C track (cutover plan §3/§4), not this official-binding pattern.
	"elixir": tselixir.Language(),
	"ocaml":  tsocaml.Language(),
	"php":    tsphp.Language(),
	"scala":  tsscala.Language(),
	"swift":  tsswift.Language(),
	// B2 cutover Part B batch 3 (#5418): source-swap providers — off the dead,
	// binding-less bundled repos onto the maintained tree-sitter-grammars org
	// (a freshness win + the one-runtime requirement), all ABI 14. hcl is
	// deferred to the vendored-C track: the tree-sitter-grammars/tree-sitter-hcl
	// bindings/go only exists from v1.2.0 (ABI 15, SIGSEGVs at RootNode), and the
	// ABI-14 tags (v1.1.0/v1.1.1) ship no Go binding — so it cannot use this
	// go-get-a-binding pattern (cutover plan §3/§4).
	"lua":  tslua.Language(),
	"toml": tstoml.Language(),
	"yaml": tsyaml.Language(),
}

// abiProbeSource is trivial, valid source per migrated language for the ABI guard.
var abiProbeSource = map[string][]byte{
	"go":         []byte("package p\nfunc F() int { return 1 }\n"),
	"python":     []byte("def f():\n    return 1\n"),
	"java":       []byte("class C { int f() { return 1; } }\n"),
	"csharp":     []byte("class C { int F() { return 1; } }\n"),
	"typescript": []byte("function f(x: number): number { return x; }\n"),
	"tsx":        []byte("const e = <div className=\"x\">hi</div>;\n"),
	"javascript": []byte("function f() { return 1; }\n"),
	"rust":       []byte("fn f() -> i32 { 1 }\n"),
	"bash":       []byte("f() { echo hi; }\n"),
	"c":          []byte("int f(void) { return 1; }\n"),
	"cpp":        []byte("struct S { int f() { return 1; } };\n"),
	"css":        []byte(".a { color: red; }\n"),
	"html":       []byte("<div><p>hi</p></div>\n"),
	"ruby":       []byte("def f\n  1\nend\n"),
	"elixir":     []byte("defmodule M do\n  def f, do: 1\nend\n"),
	"ocaml":      []byte("let f x = x + 1\n"),
	"php":        []byte("<?php function f() { return 1; }\n"),
	"scala":      []byte("object M { def f: Int = 1 }\n"),
	"swift":      []byte("func f() -> Int { return 1 }\n"),
	"lua":        []byte("local function f() return 1 end\n"),
	"toml":       []byte("[table]\nkey = \"value\"\n"),
	"yaml":       []byte("key: value\n"),
}

// tsLanguageFor resolves a language to the official adapter (if migrated) or the
// smacker adapter (everything else).
func tsLanguageFor(language string) (ts.Language, ts.Adapter, bool) {
	if l, migrated := migratedLanguages[language]; migrated {
		return l, officialAdapter, true
	}
	sl, present := languageRegistry[language]
	if !present {
		return nil, nil, false
	}
	return tssmacker.WrapLanguage(sl), smackerAdapter, true
}

// abiGuard parses trivial source for a migrated grammar and asserts a sane,
// non-error root. An ABI-incompatible grammar bump compiles but SIGSEGVs at
// RootNode (ADR 0023 §6); this catches a detectable mismatch before any real
// file is parsed.
func abiGuard(language string) error {
	l, migrated := migratedLanguages[language]
	if !migrated {
		return nil
	}
	p, err := officialAdapter.NewParser(l)
	if err != nil {
		return fmt.Errorf("treesitter: ABI guard: parser init failed for %s: %w", language, err)
	}
	defer p.Close()
	tree, err := p.Parse(abiProbeSource[language])
	if err != nil {
		return fmt.Errorf("treesitter: ABI guard: parse failed for %s: %w", language, err)
	}
	if tree == nil {
		return fmt.Errorf("treesitter: ABI guard: nil tree for %s", language)
	}
	defer tree.Close()
	root := tree.RootNode()
	if root == nil {
		return fmt.Errorf("treesitter: ABI guard: nil root for %s (ABI mismatch)", language)
	}
	if root.IsError() {
		return fmt.Errorf("treesitter: ABI guard: probe parsed to ERROR root for %s", language)
	}
	return nil
}
