package dockerfile

import (
	tsdockerfile "github.com/smacker/go-tree-sitter/dockerfile"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
)

// Dockerfile grammar provider for the extractor's inline-parse fallback (B2
// Phase 1, #5418, ADR 0023). The extractor traverses the binding-agnostic ts
// façade; this is the single place that names a concrete binding. Smacker-backed
// in both build configurations (no official Dockerfile grammar module is wired
// yet), so the file is untagged: `go build` and `go build -tags ts_official`
// both compile it unchanged.

func dockerfileGrammar() ts.Language { return tssmacker.WrapLanguage(tsdockerfile.GetLanguage()) }

var dockerfileAdapter = tssmacker.New()
