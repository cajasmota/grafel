package treesitter

import (
	"fmt"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tsbash "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/bash"
	tsc "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/c"
	tscpp "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/cpp"
	tscsharp "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/csharp"
	tscss "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/css"
	tsdockerfile "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/dockerfile"
	tselixir "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/elixir"
	tsgolang "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/golang"
	tsgroovy "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/groovy"
	tshcl "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/hcl"
	tshtml "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/html"
	tsjava "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/java"
	tsjavascript "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/javascript"
	tskotlin "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/kotlin"
	tslua "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/lua"
	tsocaml "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/ocaml"
	tsphp "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/php"
	tsproto "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/proto"
	tspython "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/python"
	tsruby "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/ruby"
	tsrust "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/rust"
	tsscala "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/scala"
	tssql "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/sql"
	tsswift "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/swift"
	tstoml "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/toml"
	tstypescript "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/typescript"
	tsyaml "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/yaml"
	tsofficial "github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// Binding (B2 cutover, ADR 0023, #5418). Every grammar is now bound to the
// official tree-sitter/go-tree-sitter runtime via its per-language provider in
// internal/treesitter/ts/grammars/<lang>. The legacy smacker binding has been
// removed entirely, so this is the sole, un-tagged binding configuration — the
// co-link blocker that previously forced an opt-in `-tags ts_official` build is
// gone now that nothing links the second runtime. ParseResult.TSTree is the
// binding-agnostic tree every extractor consumes.

// officialAdapter is the official binding adapter (the only adapter).
var officialAdapter = tsofficial.New()

// migratedLanguages maps each supported language to its official ts.Language.
// The registry key "tsx" routes .tsx/.jsx files to the TSX grammar (the
// JSX-enabled superset) from the same tree-sitter-typescript module.
var migratedLanguages = map[string]ts.Language{
	"go":         tsgolang.Language(),
	"shell":      tsbash.Language(), // alias: shell files use the bash grammar
	"terraform":  tshcl.Language(),  // alias: terraform files use the HCL grammar
	"python":     tspython.Language(),
	"java":       tsjava.Language(),
	"csharp":     tscsharp.Language(),
	"typescript": tstypescript.Language(),
	"tsx":        tstypescript.LanguageTSX(),
	"javascript": tsjavascript.Language(),
	"rust":       tsrust.Language(),
	"bash":       tsbash.Language(),
	"c":          tsc.Language(),
	"cpp":        tscpp.Language(),
	"css":        tscss.Language(),
	"html":       tshtml.Language(),
	"ruby":       tsruby.Language(),
	"elixir":     tselixir.Language(),
	"ocaml":      tsocaml.Language(),
	"php":        tsphp.Language(),
	"scala":      tsscala.Language(),
	"swift":      tsswift.Language(),
	"lua":        tslua.Language(),
	"toml":       tstoml.Language(),
	"yaml":       tsyaml.Language(),
	"proto":      tsproto.Language(),
	// alias: the classifier names .proto files "protobuf" and the extractor
	// registers under that token (#6356). subproc.go/incremental.go call
	// Parse with the classifier language verbatim, so without this entry the
	// parse returns ErrUnsupportedLanguage, FileInput.TSTree stays nil and the
	// proto extractor returns no entities even once the registry lookup hits.
	"protobuf":   tsproto.Language(),
	"dockerfile": tsdockerfile.Language(),
	"kotlin":     tskotlin.Language(),
	"sql":        tssql.Language(),
	"hcl":        tshcl.Language(),
	"groovy":     tsgroovy.Language(),
}

// abiProbeSource is trivial, valid source per language for the ABI guard.
var abiProbeSource = map[string][]byte{
	"go":         []byte("package p\nfunc F() int { return 1 }\n"),
	"shell":      []byte("f() { echo hi; }\n"),
	"terraform":  []byte("resource \"x\" \"y\" {\n  name = \"v\"\n}\n"),
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
	"proto":      []byte("syntax = \"proto3\";\nmessage M { int32 id = 1; }\n"),
	"protobuf":   []byte("syntax = \"proto3\";\nmessage M { int32 id = 1; }\n"),
	"dockerfile": []byte("FROM alpine:3\nRUN echo hi\n"),
	"kotlin":     []byte("fun f(): Int { return 1 }\n"),
	"sql":        []byte("SELECT id FROM t WHERE id = 1;\n"),
	"hcl":        []byte("resource \"x\" \"y\" {\n  name = \"v\"\n}\n"),
	"groovy":     []byte("def f() { return 1 }\n"),
}

// tsLanguageFor resolves a language to the official adapter and its grammar
// handle. Returns false when the language is not supported.
func tsLanguageFor(language string) (ts.Language, ts.Adapter, bool) {
	if l, ok := migratedLanguages[language]; ok {
		return l, officialAdapter, true
	}
	return nil, nil, false
}

// abiGuard parses trivial source for a grammar and asserts a sane, non-error
// root. An ABI-incompatible grammar bump compiles but SIGSEGVs at RootNode
// (ADR 0023 §6); this catches a detectable mismatch before any real file is
// parsed.
func abiGuard(language string) error {
	l, ok := migratedLanguages[language]
	if !ok {
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
