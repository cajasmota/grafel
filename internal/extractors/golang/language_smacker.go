//go:build !ts_official

package golang

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	tsgo "github.com/smacker/go-tree-sitter/golang"
)

// Default build (no ts_official tag): the Go extractor's inline-parse fallback
// uses the smacker binding, exactly as before the B2 abstraction. The extractor
// itself traverses the ts façade (binding-agnostic); only the grammar provider
// differs by build tag. See language_official.go for the migrated path.
//
// Why a build tag: the smacker and official tree-sitter runtimes both statically
// vendor the SAME C runtime (lib/src/*.c) with identical symbol names, so a
// single binary linking BOTH fails with ~247 duplicate C symbols on macOS (and
// would on any platform without --allow-multiple-definition). Until that
// co-link is resolved (B2 Phase 1 — migration plan §"Co-link blocker"), the
// official path is opt-in behind `-tags ts_official`. Default builds link only
// smacker and keep every grammar working.

func goGrammar() ts.Language { return tssmacker.WrapLanguage(tsgo.GetLanguage()) }

var goAdapter = tssmacker.New()
