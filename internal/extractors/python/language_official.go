//go:build ts_official

package python

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tspython "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/python"
	tsofficial "github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// ts_official build: the Python extractor's inline-parse fallback uses the
// OFFICIAL tree-sitter/go-tree-sitter binding (B2 cutover Part A, ADR 0023,
// #5418), ABI-pinned to tree-sitter-python v0.23.6 (ABI 14) against runtime
// v0.24.0. The extractor traverses the ts façade unchanged; only the grammar
// provider differs from the default build (see language_smacker.go).

func pythonGrammar() ts.Language { return tspython.Language() }

var pythonAdapter = tsofficial.New()
