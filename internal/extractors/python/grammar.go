package python

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	tspython "github.com/smacker/go-tree-sitter/python"
)

// Python grammar provider for the extractor's inline-parse fallback (B2 Phase 1,
// #5418, ADR 0023). The extractor traverses the binding-agnostic ts façade; this
// is the single place that names a concrete binding.
//
// Python is smacker-backed in BOTH build configurations for now: unlike Go (B2
// Phase 0), it has no official-binding grammar module yet, so there is no
// ts_official variant to tag-split against. When Python is promoted to the
// official runtime (a later B2 phase), this file becomes a build-tagged pair
// (language_smacker.go / language_official.go) exactly like the Go extractor.
//
// Keeping it untagged means `go build` and `go build -tags ts_official` both
// compile the Python extractor unchanged — the official tag only affects which
// grammars are routed off smacker (currently just Go), not Python.

func pythonGrammar() ts.Language { return tssmacker.WrapLanguage(tspython.GetLanguage()) }

var pythonAdapter = tssmacker.New()
