// Package swift provides the Swift grammar via the official tree-sitter
// binding, wrapped as a ts.Language for the official adapter. Sibling of the
// Phase-0 golang/ package (B2 cutover Part B, ADR 0023, #5418): migrating a
// language is a package like this one plus a migratedLanguages line in
// adapters_official.go.
//
// ABI pin. The grammar is pinned to alex-pinkus/tree-sitter-swift at the
// 0.7.3-with-generated-files tag against runtime v0.24.0 — that tag is ABI 14
// (inside the runtime's 13–14 window). The generated src/parser.c is checked in
// only on the "-with-generated-files" tags, so the binding is consumed via the
// pseudo-version of that tag's commit. Do not bump past ABI 14 without the
// smoke-parse + benchmark gate.
package swift

import (
	tsswift "github.com/alex-pinkus/tree-sitter-swift/bindings/go"
	tsofficial "github.com/tree-sitter/go-tree-sitter"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// Language returns the Swift grammar as a ts.Language bound to the official adapter.
func Language() ts.Language {
	return official.WrapLanguage(tsofficial.NewLanguage(tsswift.Language()))
}
