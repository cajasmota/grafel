//go:build !ts_official

package python

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	tspython "github.com/smacker/go-tree-sitter/python"
)

// Default build (no ts_official tag): the Python extractor's inline-parse
// fallback uses the smacker binding. The extractor traverses the binding-agnostic
// ts façade; this is the single place that names a concrete binding. See
// language_official.go for the migrated path.
//
// Why a build tag: the smacker and official tree-sitter runtimes both statically
// vendor the SAME C runtime under identical symbol names, so a single binary
// linking BOTH fails with ~247 duplicate C symbols on macOS. Until the default
// flip (B2 cutover §7), the official path is opt-in behind `-tags ts_official`;
// default builds link only smacker and keep every grammar working.

func pythonGrammar() ts.Language { return tssmacker.WrapLanguage(tspython.GetLanguage()) }

var pythonAdapter = tssmacker.New()
