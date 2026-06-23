package cpp

import (
	tsc "github.com/smacker/go-tree-sitter/c"
	tscpp "github.com/smacker/go-tree-sitter/cpp"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
)

// C/C++ grammar providers for the extractor's inline-parse fallback (B2 Phase 1,
// #5418, ADR 0023). The extractor traverses the binding-agnostic ts façade; this
// is the single place that names a concrete binding. Smacker-backed in both build
// configurations (no official C/C++ grammar module is wired yet), so the file is
// untagged: `go build` and `go build -tags ts_official` both compile it unchanged.

// cGrammar returns the tree-sitter grammar for C.
func cGrammar() ts.Language { return tssmacker.WrapLanguage(tsc.GetLanguage()) }

// cppGrammar returns the tree-sitter grammar for C++.
func cppGrammar() ts.Language { return tssmacker.WrapLanguage(tscpp.GetLanguage()) }

// cppAdapter is the binding adapter used to construct parsers in the fallback.
var cppAdapter = tssmacker.New()
