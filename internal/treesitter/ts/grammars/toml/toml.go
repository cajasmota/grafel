// Package toml provides the TOML grammar via the official tree-sitter binding,
// wrapped as a ts.Language for the official adapter. Sibling of the Phase-0
// golang/ package (B2 cutover Part B, ADR 0023, #5418): migrating a language is
// a package like this one plus a migratedLanguages line in adapters_official.go.
//
// Source swap. Unlike the version-only batches, toml moves off its bundled source
// (ikatyang/tree-sitter-toml, 2021, no Go binding) onto the maintained
// tree-sitter-grammars/tree-sitter-toml, whose bindings/go depends on the official
// runtime — both a freshness win and the one-runtime requirement (cutover §5).
//
// ABI pin. The grammar is pinned to tree-sitter-toml v0.7.0 against runtime
// v0.24.0 — its generated src/parser.c carries LANGUAGE_VERSION 14, inside the
// runtime's 13–14 window. v0.7.0 is the latest tag and already ABI 14. Do not
// bump past ABI 14 without the smoke-parse + benchmark gate.
package toml

import (
	tstoml "github.com/tree-sitter-grammars/tree-sitter-toml/bindings/go"
	tsofficial "github.com/tree-sitter/go-tree-sitter"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// Language returns the TOML grammar as a ts.Language bound to the official adapter.
func Language() ts.Language {
	return official.WrapLanguage(tsofficial.NewLanguage(tstoml.Language()))
}
