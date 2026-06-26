package treesitter

// ABI-15 full-rollout invariant (#5473 Phase 2). The mixed-ABI parse loop the
// daemon hit came from running grammars at ABI 14 against a runtime expecting a
// newer ABI. The fix is atomic: EVERY registered grammar must report ABI 15 —
// the 12 grammars bumped to their ABI-15 upstream releases AND the 9 laggards
// regenerated locally with `tree-sitter generate --abi 15`. This test fails if
// any single grammar is left behind at ABI 14 (the loop), which is the one state
// this whole effort exists to prevent.

import (
	"sort"
	"testing"
)

// abiVersioner is the minimal surface needed to read a grammar's ABI. The
// official adapter's Language implements it; asserting through this interface
// keeps the registry's ts.Language values opaque.
type abiVersioner interface {
	AbiVersion() int
}

func TestABI15_EveryRegisteredGrammarIsABI15(t *testing.T) {
	const wantABI = 15

	if len(migratedLanguages) == 0 {
		t.Fatal("no registered grammars — registry is empty")
	}

	names := make([]string, 0, len(migratedLanguages))
	for name := range migratedLanguages {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		lang := migratedLanguages[name]
		av, ok := lang.(abiVersioner)
		if !ok {
			t.Errorf("%s: registered Language does not expose AbiVersion()", name)
			continue
		}
		if got := av.AbiVersion(); got != wantABI {
			t.Errorf("%s: AbiVersion() = %d, want %d (grammar left at the mixed-ABI loop)", name, got, wantABI)
		} else {
			t.Logf("%s: ABI %d ✓", name, got)
		}
	}
}
