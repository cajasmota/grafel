// Package golang provides the Go grammar via the official tree-sitter binding,
// wrapped as a ts.Language for the official adapter. This is the single place
// that imports the per-language grammar module (B2 Phase 0, ADR 0023, #5418):
// migrating another language is a sibling package like this one, plus a registry
// line in parser.go.
//
// ABI pin. The grammar is pinned to tree-sitter-go v0.23.4 against runtime
// v0.24.0 — the ADR-0023 §6 verified-compatible pair. A newer grammar
// (e.g. v0.25.0) compiles but SIGSEGVs at RootNode; do not bump without the
// smoke-parse + benchmark gate.
package golang

import (
	tsofficial "github.com/tree-sitter/go-tree-sitter"
	tsgo "github.com/tree-sitter/tree-sitter-go/bindings/go"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// Language returns the Go grammar as a ts.Language bound to the official adapter.
func Language() ts.Language {
	return official.WrapLanguage(tsofficial.NewLanguage(tsgo.Language()))
}
