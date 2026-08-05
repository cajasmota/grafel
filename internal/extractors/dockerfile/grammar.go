package dockerfile

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tsdockerfile "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/dockerfile"
	tsofficial "github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// Dockerfile grammar provider for the extractor's inline-parse fallback (B2
// cutover, #5418, ADR 0023). The extractor traverses the binding-agnostic ts
// façade; this is the single place that names a concrete binding.

func dockerfileGrammar() ts.Language { return tsdockerfile.Language() }

// Declared as the ts.Adapter INTERFACE rather than the concrete type so a test
// can substitute a binding stub. #6154 made the extractor's self-parse fallback
// reachable, and ts.Parser.Parse is documented to return "nil if the binding
// produced no tree" (ts.go:136) — official.Parse does exactly that, with a nil
// error, whenever the parse watchdog is disabled (official.go:155). That nil
// tree is a real input to the fallback and the only way to exercise its guard is
// to inject a parser that produces one.
var dockerfileAdapter ts.Adapter = tsofficial.New()
