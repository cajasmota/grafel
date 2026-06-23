// Package typescript provides the TypeScript and TSX grammars via the official
// tree-sitter binding, wrapped as ts.Language values for the official adapter.
// Sibling of the Phase-0 golang/ package (B2 cutover Part A, ADR 0023, #5418).
//
// One module, two grammars. tree-sitter-typescript's bindings/go ships both
// LanguageTypescript() (for .ts) and LanguageTSX() (for .tsx/.jsx, the
// JSX-enabled superset) from a single Go package, mirroring the smacker side's
// typescript/typescript and typescript/tsx split. Callers route .tsx/.jsx files
// to TSX() by path extension (PLT #537); the entity Language tag stays
// "typescript".
//
// ABI pin. The grammars are pinned to tree-sitter-typescript v0.23.2 against
// runtime v0.24.0 — both generated src/parser.c files carry LANGUAGE_VERSION 14,
// inside the runtime's 13–14 window. v0.23.2 is already the upstream-latest tag
// (no pin-back). Do not bump past ABI 14 without the smoke-parse + benchmark gate.
package typescript

import (
	tsofficial "github.com/tree-sitter/go-tree-sitter"
	tstypescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// Language returns the TypeScript grammar as a ts.Language bound to the official adapter.
func Language() ts.Language {
	return official.WrapLanguage(tsofficial.NewLanguage(tstypescript.LanguageTypescript()))
}

// LanguageTSX returns the TSX grammar (JSX-enabled superset) as a ts.Language
// bound to the official adapter, for .tsx/.jsx files.
func LanguageTSX() ts.Language {
	return official.WrapLanguage(tsofficial.NewLanguage(tstypescript.LanguageTSX()))
}
