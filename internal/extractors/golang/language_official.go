//go:build ts_official

package golang

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tsgolang "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/golang"
	tsofficial "github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// ts_official build: the Go extractor's inline-parse fallback uses the OFFICIAL
// tree-sitter/go-tree-sitter binding (B2 Phase 0, ADR 0023, #5418), ABI-pinned
// to tree-sitter-go v0.23.4 against runtime v0.24.0. The extractor traverses the
// ts façade unchanged; only the grammar provider differs from the default build.
//
// Build with: go build -tags ts_official ./...  (links the official runtime;
// see language_smacker.go for why this is tag-gated — the co-link blocker).

func goGrammar() ts.Language { return tsgolang.Language() }

var goAdapter = tsofficial.New()
