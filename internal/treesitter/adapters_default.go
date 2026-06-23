//go:build !ts_official

package treesitter

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
)

// Default build (no ts_official tag): NO language is migrated to the official
// binding, so every grammar parses through the smacker adapter and the binary
// links ONLY the smacker tree-sitter runtime.
//
// Co-link blocker (B2 Phase 0 finding, ADR 0023). The smacker bundle and the
// official tree-sitter/go-tree-sitter module each statically vendor the SAME C
// runtime (lib/src/*.c) under identical symbol names. A single binary that links
// BOTH fails with ~247 duplicate C symbols on macOS (a hard ld error; no
// --allow-multiple-definition equivalent). Therefore the official path is opt-in
// behind `-tags ts_official` until Phase 1 resolves the collision (symbol
// prefixing / one-runtime-for-all / out-of-process grammar host — see the
// migration plan). Default builds stay clean and fully smacker-backed.

// migratedLanguages is empty in the default build.
var migratedLanguages = map[string]ts.Language{}

// tsLanguageFor resolves a language to the smacker adapter (default build).
func tsLanguageFor(language string) (ts.Language, ts.Adapter, bool) {
	sl, present := languageRegistry[language]
	if !present {
		return nil, nil, false
	}
	return tssmacker.WrapLanguage(sl), smackerAdapter, true
}

// abiGuard is a no-op in the default build (no official grammars to guard).
func abiGuard(string) error { return nil }
