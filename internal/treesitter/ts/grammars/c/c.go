// Package c provides the C grammar via the official tree-sitter binding,
// wrapped as a ts.Language for the official adapter. Sibling of the Phase-0
// golang/ package (B2 cutover Part B, ADR 0023, #5418): migrating a language is
// a package like this one plus a migratedLanguages line in adapters_official.go.
//
// ABI pin. The grammar is pinned to tree-sitter-c v0.23.6 against runtime
// v0.24.0 — its generated src/parser.c carries LANGUAGE_VERSION 14, inside the
// runtime's 13–14 window. The latest upstream (v0.24.2) is ABI 15: it compiles
// but SIGSEGVs at RootNode (ADR 0023 §6). v0.23.6 is the freshest ABI-14 tag.
// Do not bump past ABI 14 without the smoke-parse + benchmark gate.
package c

import (
	tsofficial "github.com/tree-sitter/go-tree-sitter"
	tsc "github.com/tree-sitter/tree-sitter-c/bindings/go"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// Language returns the C grammar as a ts.Language bound to the official adapter.
func Language() ts.Language {
	return official.WrapLanguage(tsofficial.NewLanguage(tsc.Language()))
}
