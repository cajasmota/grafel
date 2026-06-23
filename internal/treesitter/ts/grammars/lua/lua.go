// Package lua provides the Lua grammar via the official tree-sitter binding,
// wrapped as a ts.Language for the official adapter. Sibling of the Phase-0
// golang/ package (B2 cutover Part B, ADR 0023, #5418): migrating a language is
// a package like this one plus a migratedLanguages line in adapters_official.go.
//
// Source swap. Unlike the version-only batches, lua moves off its bundled source
// (Azganoth/tree-sitter-lua, which ships no Go binding) onto the maintained
// tree-sitter-grammars/tree-sitter-lua, whose bindings/go depends on the official
// runtime — both a freshness win and the one-runtime requirement (cutover §5).
//
// ABI pin. The grammar is pinned to tree-sitter-lua v0.3.0 against runtime
// v0.24.0 — its generated src/parser.c carries LANGUAGE_VERSION 14, inside the
// runtime's 13–14 window. v0.4.0+ are ABI 15: they compile but SIGSEGV at
// RootNode (ADR 0023 §6). v0.3.0 is the freshest ABI-14 tag. Do not bump past
// ABI 14 without the smoke-parse + benchmark gate.
package lua

import (
	tslua "github.com/tree-sitter-grammars/tree-sitter-lua/bindings/go"
	tsofficial "github.com/tree-sitter/go-tree-sitter"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// Language returns the Lua grammar as a ts.Language bound to the official adapter.
func Language() ts.Language {
	return official.WrapLanguage(tsofficial.NewLanguage(tslua.Language()))
}
