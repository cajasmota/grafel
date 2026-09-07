package csharp

import (
	"context"
	"fmt"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
)

// #6975, review mutant MB-2 — blazorNonTypeKeywords is graded as an EXACT SET,
// key by key, not at whichever handful the corpus happens to exercise.
//
// WHY. MB-2 deleted ONE entry ("throw") and left the other 38; the package
// stayed green. That single result is not interesting on its own — `throw`'s
// reachability is genuinely thin, because the common `throw new
// ArgumentException(x)` is already suppressed by the "new" key (the pattern
// aligns on `new` as the type token and `ArgumentException` as the name), and
// what "throw" uniquely guards is `throw Foo(...)`, legal only when Foo()
// returns an exception. No live defect was claimed and none is claimed here.
//
// The finding is about the MAP. It is a 40-entry membership list (the review
// said 39 — counted here, it is 40) that was graded at some members with
// nothing saying which, so a later tidy-up, or a
// merge dropping a line, removes a guard silently. Since Go's RE2 has no
// lookahead, that map IS the mechanism — it is not a convenience list beside
// the real check. A per-key assertion is the only thing that makes the set the
// unit of grading rather than the handful of shapes a fixture happens to hit.
//
// It is an internal (package csharp) test because the map is unexported and
// the point is to iterate the real thing. Exporting a copy for the test would
// grade the copy.
//
// THE EXPECTATION IS A LITERAL LIST, NOT THE MAP ITSELF. The first revision of
// this test ranged over blazorNonTypeKeywords to build its subtests, and
// re-scoring MB-2 against it showed that was worthless: delete a key and the
// key simply produces no subtest, so the suite stays green. A test driven by
// the thing under test cannot detect a deletion. `wantKeywords` below is
// therefore an independent copy, and the set comparison runs in BOTH
// directions — a removed key fails, and a key added without a line here fails
// too, which is what stops the list from drifting back to ungraded.
//
// TWO ASSERTIONS PER KEY, because suppression alone can pass vacuously. A key
// whose line the pattern never matches in the first place would look
// "suppressed" while contributing nothing, and adding such a key would be
// invisible. So each key must ALSO be shown reachable: the raw pattern must
// align on it as the type token with `Target` as the name. A key that fails
// (1) is dead weight; a key that fails (2) is an unguarded shape.
func TestBlazorNonTypeKeywordsIsGradedAsAnExactSet6975(t *testing.T) {
	// An INDEPENDENT copy of the set, maintained by hand. Every entry is
	// asserted below; the set equality at the end is what makes a deletion or
	// an undeclared addition fail.
	wantKeywords := []string{
		// Statement keywords that can lead a line and be followed by `ident(`.
		"return", "new", "else", "throw", "await",
		"yield", "goto", "break", "continue",
		"case", "default", "do", "try", "finally",
		"checked", "unchecked", "stackalloc", "delegate",
		// Contextual/operator keywords in the same slot.
		"in", "is", "as", "out", "ref",
		"params", "typeof", "sizeof", "nameof",
		"when", "with", "and", "or", "not",
		// LINQ query clause keywords.
		"from", "select", "where", "let",
		"orderby", "group", "join", "into",
	}

	// A line-leading `<token> Target(x);` inside a method body — the shape that
	// survives the `^[ \t]*` anchor and mints #6973's collider.
	src := func(token string) string {
		return fmt.Sprintf(`
public partial class Probe : ComponentBase
{
    void Anchor()
    {
        %s Target(x);
    }
}
`, token)
	}
	mints := func(t *testing.T, token string) (target, anchor bool) {
		t.Helper()
		e := &blazorExtractor{}
		ents, err := e.Extract(context.Background(), extreg.FileInput{
			Path: "Pages/Probe.razor.cs", Language: "csharp", Content: []byte(src(token)),
		})
		if err != nil {
			t.Fatalf("extract(%q): %v", token, err)
		}
		for i := range ents {
			if ents[i].Kind != "SCOPE.Operation" {
				continue
			}
			switch ents[i].Name {
			case "Target":
				target = true
			case "Anchor":
				anchor = true
			}
		}
		return target, anchor
	}

	// CONTROL. An ordinary type token in the same slot DOES mint `Target`, so
	// every "not minted" below is the map's doing and not the fixture's.
	if target, anchor := mints(t, "Widget"); !target || !anchor {
		t.Fatalf("control: `Widget Target(x);` minted Target=%v Anchor=%v, want both true — "+
			"the fixture does not exercise the rule, so the per-key assertions cannot grade it",
			target, anchor)
	}

	for _, kw := range wantKeywords {
		t.Run(kw, func(t *testing.T) {
			// (1) REACHABLE. The pattern must actually align on this key, or
			// the key is dead weight and its presence is ungraded.
			// FindAll, not Find: the fixture's own `void Anchor()` line
			// matches first, and comparing against THAT is how the first
			// revision of this test failed every key for the wrong reason.
			var aligned bool
			for _, m := range reBlazorCodeMethod.FindAllStringSubmatch(src(kw), -1) {
				if m[1] == kw && m[2] == "Target" {
					aligned = true
					break
				}
			}
			if !aligned {
				t.Fatalf("reBlazorCodeMethod does not align on `%s Target(x);`, "+
					"so this key suppresses nothing and its removal would be invisible", kw)
			}
			// (2) SUPPRESSED. Extract must reject the match on that token.
			target, anchor := mints(t, kw)
			if !anchor {
				t.Fatal("the surrounding declaration Anchor was not extracted; the fixture is broken")
			}
			if target {
				t.Errorf("`%s Target(x);` minted a SCOPE.Operation named Target — "+
					"the %q key of blazorNonTypeKeywords is not suppressing it", kw, kw)
			}
		})
	}

	// SET EQUALITY, both directions. This is the assertion MB-2 defeats
	// without: deleting a key from the map removes its subtest above and
	// nothing else notices.
	seen := map[string]bool{}
	for _, kw := range wantKeywords {
		if seen[kw] {
			t.Errorf("wantKeywords lists %q twice", kw)
		}
		seen[kw] = true
		if !blazorNonTypeKeywords[kw] {
			t.Errorf("blazorNonTypeKeywords is MISSING %q — a guard was dropped; "+
				"if the removal is deliberate, delete it from wantKeywords too and say why", kw)
		}
	}
	for kw := range blazorNonTypeKeywords {
		if !seen[kw] {
			t.Errorf("blazorNonTypeKeywords has %q, which wantKeywords does not list — "+
				"add it there so the per-key assertions above actually grade it", kw)
		}
	}
	if len(blazorNonTypeKeywords) != len(wantKeywords) {
		t.Errorf("blazorNonTypeKeywords has %d entries, wantKeywords %d",
			len(blazorNonTypeKeywords), len(wantKeywords))
	}
}
