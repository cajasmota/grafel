package fsharp

import "testing"

// #6369 round 2 — buildImportEntities must mint AT MOST ONE placeholder per
// distinct module path.
//
// Its `seen` map cannot be reached through Extract: the single call site feeds
// it collectOpenStatements' output, which already de-duplicates on the same
// key. Mutation-measured, dropping the dedup therefore survived every
// package-level suite. It is still a real contract of the function — a
// duplicate placeholder means two records with the SAME graph.EntityID (which
// hashes repo/kind/name/sourceFile), i.e. a self-collision in every index the
// resolver builds — so it is pinned here, at the only altitude that can see it.
func TestBuildImportEntitiesDedupesModulePaths_6369(t *testing.T) {
	out := buildImportEntities("src/App/Use.fs", []string{
		"Acme.Animal",
		"System.Collections.Generic",
		"Acme.Animal",
	})
	if len(out) != 2 {
		names := make([]string, len(out))
		for i, e := range out {
			names[i] = e.Name
		}
		t.Fatalf("got %d placeholders %v, want 2 (one per distinct module path)", len(out), names)
	}
	seen := map[string]bool{}
	for _, e := range out {
		if seen[e.Name] {
			t.Errorf("duplicate placeholder for %q", e.Name)
		}
		seen[e.Name] = true
	}
}
