package main

import (
	"sort"
	"testing"
)

// TestJavascriptPackageResolvesAgainstTsx pins the one hand-maintained fact in
// the package→grammar mapping. Everything else in that mapping is derived from
// the extractor.Register call sites; extraGrammarKeys is the exception, and it
// says the javascript package also receives TSX trees because
// internal/extractors/incremental.go re-points the PARSE language to "tsx" for
// a .tsx/.jsx file whose classified language is typescript or javascript.
//
// It needs its own test because deleting that entry is a mutant the five
// integration controls do NOT kill. Measured: tsx contributes exactly one node
// kind that neither the javascript nor the typescript grammar has, and it is
// the hidden internal rule _jsx_start_opening_element_repeat1, which no matcher
// would ever name. So the entry is currently INERT — it cannot change a verdict
// on today's grammars. It is kept anyway because it is true, and because a
// future divergence between the typescript and tsx grammars would make it
// load-bearing with no other signal that it was missing.
//
// This test therefore asserts what is actually observable: that the entry
// reaches the resolution set. It also logs the inertness, so the next reader
// does not have to re-derive it.
//
// VARIED: nothing — three premise assertions about one mapping, not a table.
func TestJavascriptPackageResolvesAgainstTsx(t *testing.T) {
	const dir = "internal/extractors/javascript"
	root := modRoot(t)
	g := testGrammars(t)

	surf, err := LoadSurface(root, []string{"./internal/extractors/javascript"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	keys := grammarKeysFor(dir, surf.Registrations, g)
	has := map[string]bool{}
	for _, k := range keys {
		has[k] = true
	}
	// Derived from the package's own Register calls.
	for _, want := range []string{"javascript", "typescript"} {
		if !has[want] {
			t.Errorf("%s does not resolve against %q; registrations were %v", dir, want, keys)
		}
	}
	// The hand-maintained extra route. M10 (deleting the extraGrammarKeys row)
	// dies here and nowhere else.
	if !has["tsx"] {
		t.Errorf("%s does not resolve against the tsx grammar, but incremental.go parses its .tsx/.jsx files with it — literals valid only under TSX would be reported as dead. Keys: %v", dir, keys)
	}

	var only []string
	for k := range g["tsx"].Kinds {
		if !g["javascript"].Kinds[k] && !g["typescript"].Kinds[k] {
			only = append(only, k)
		}
	}
	sort.Strings(only)
	t.Logf("tsx contributes %d node kind(s) absent from both javascript and typescript: %v", len(only), only)
}
