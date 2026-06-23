//go:build ts_official

package treesitter

import (
	"fmt"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tsgolang "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/golang"
	tsofficial "github.com/cajasmota/grafel/internal/treesitter/ts/official"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
)

// ts_official build: Go is migrated onto the official tree-sitter/go-tree-sitter
// binding (B2 Phase 0, ADR 0023, #5418). Every other language stays on smacker.
// NOTE: this build links BOTH runtimes and currently fails at link time on
// macOS (the co-link blocker — see adapters_default.go). It exists to exercise
// and CI-test the official adapter + Go grammar + ABI guard in isolation and on
// platforms/toolchains where multiple-definition linking is permitted; resolving
// the co-link is Phase 1.

// officialAdapter is the official binding adapter (only compiled under the tag).
var officialAdapter = tsofficial.New()

// migratedLanguages maps each migrated language to its official ts.Language.
// Phase 0 migrates Go only.
var migratedLanguages = map[string]ts.Language{
	"go": tsgolang.Language(),
}

// abiProbeSource is trivial, valid source per migrated language for the ABI guard.
var abiProbeSource = map[string][]byte{
	"go": []byte("package p\nfunc F() int { return 1 }\n"),
}

// tsLanguageFor resolves a language to the official adapter (if migrated) or the
// smacker adapter (everything else).
func tsLanguageFor(language string) (ts.Language, ts.Adapter, bool) {
	if l, migrated := migratedLanguages[language]; migrated {
		return l, officialAdapter, true
	}
	sl, present := languageRegistry[language]
	if !present {
		return nil, nil, false
	}
	return tssmacker.WrapLanguage(sl), smackerAdapter, true
}

// abiGuard parses trivial source for a migrated grammar and asserts a sane,
// non-error root. An ABI-incompatible grammar bump compiles but SIGSEGVs at
// RootNode (ADR 0023 §6); this catches a detectable mismatch before any real
// file is parsed.
func abiGuard(language string) error {
	l, migrated := migratedLanguages[language]
	if !migrated {
		return nil
	}
	p, err := officialAdapter.NewParser(l)
	if err != nil {
		return fmt.Errorf("treesitter: ABI guard: parser init failed for %s: %w", language, err)
	}
	defer p.Close()
	tree, err := p.Parse(abiProbeSource[language])
	if err != nil {
		return fmt.Errorf("treesitter: ABI guard: parse failed for %s: %w", language, err)
	}
	if tree == nil {
		return fmt.Errorf("treesitter: ABI guard: nil tree for %s", language)
	}
	defer tree.Close()
	root := tree.RootNode()
	if root == nil {
		return fmt.Errorf("treesitter: ABI guard: nil root for %s (ABI mismatch)", language)
	}
	if root.IsError() {
		return fmt.Errorf("treesitter: ABI guard: probe parsed to ERROR root for %s", language)
	}
	return nil
}
