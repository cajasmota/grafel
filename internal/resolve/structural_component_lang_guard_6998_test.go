package resolve

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #6998 — lookupStructural's cross-file, whole-graph unique-name fallback
// (lookupUniqueRealComponentByName) fires for `component`-scope structural
// addresses with lang == "python" and for no other language. Removing that
// guard was ALIVE: the resolver package, the touched extractor packages and
// the full golden ratchet all stayed green, so the restriction of the widest
// tier in the structural lookup to one language was asserted by nothing.
//
// Today's behaviour is correct; this file is the missing assertion. The tier
// is what makes the Python address dialect NOT same-file-only, which is
// exactly what separates it from the C# (#6984) and protobuf (#6991) ports —
// both deliberately same-file because a cross-file guess is #6369's
// wrong-node hazard. Widening it would give every language a whole-graph
// unique-name fallback: the permissive direction, where a binding that should
// not happen becomes a confidently-wrong edge that reads as valid.
//
// The pin is behavioural, not a source scan for the literal `lang ==
// "python"` — a scan-and-assert guard has several independent no-op modes and
// would survive the very rewrite it exists to catch.
//
// AXES. Varied: the address's language segment (and the entity Language /
// file extension that go with it). Held constant: the entity kind and
// subtype, the declared name, the global uniqueness of that name, the
// cross-file relationship (the consumer's file declares nothing), the
// directory (so pkgDirOf is identical), and the address's scope-kind and
// subtype segments. Case `ruby-on-python-paths` additionally holds the FILE
// PATHS constant too, so the only thing separating it from the binding case
// is the language segment itself — the guard cannot be mistaken for a
// path- or extension-keyed effect.

// langGuardFixture6998 builds the one shape both halves of the pair share:
// a class `SharedBase` declared in `app/base<ext>`, globally unique, and a
// consumer file `app/child<ext>` that declares NOTHING. The structural
// address is minted with the CONSUMER's file — the Python dialect's
// convention (the hierarchy extractor addresses `class Child(SharedBase):`
// by the file the subclass declaration lives in), so the same-file
// lookupLocationKind tier necessarily misses and only the cross-file tier
// can bind.
func langGuardFixture6998(lang, ext string) (base types.EntityRecord, consumer types.EntityRecord, ref string) {
	baseFile := "app/base" + ext
	childFile := "app/child" + ext
	base = types.EntityRecord{
		ID: "c0de6998ba5e0001", Kind: "Class", Subtype: "class",
		Name: "SharedBase", QualifiedName: "SharedBase",
		SourceFile: baseFile, Language: lang,
	}
	// The consumer entity exists only so the consumer file is a real file in
	// the index with a real, DIFFERENT name in it — the same-file tier has
	// something to miss on rather than nothing at all.
	consumer = types.EntityRecord{
		ID: "c0de6998c41d0002", Kind: "Class", Subtype: "class",
		Name: "Child", QualifiedName: "Child",
		SourceFile: childFile, Language: lang,
	}
	ref = "scope:component:class:" + lang + ":" + childFile + ":SharedBase"
	return base, consumer, ref
}

func TestStructuralComponentCrossFileTier_IsPythonOnly6998(t *testing.T) {
	cases := []struct {
		name     string
		lang     string
		ext      string
		wantBind bool
	}{
		// The positive control. Without it the three negatives below could
		// all pass by the tier never firing at all, which is the commonest
		// way an absence assertion here turns out to be decoration.
		{name: "python", lang: "python", ext: ".py", wantBind: true},
		{name: "ruby", lang: "ruby", ext: ".rb", wantBind: false},
		{name: "java", lang: "java", ext: ".java", wantBind: false},
		// Language varied, file paths held identical to the python case.
		{name: "ruby-on-python-paths", lang: "ruby", ext: ".py", wantBind: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base, consumer, ref := langGuardFixture6998(tc.lang, tc.ext)
			idx := BuildIndex([]types.EntityRecord{base, consumer})

			// Premise 1 — the cross-file tier's own precondition holds in
			// EVERY case, binding and non-binding alike. `SharedBase` is
			// globally unique, so lookupUniqueRealComponentByName would
			// return the base class if it were reached. This is what makes
			// the negatives sharp: the ONLY thing withholding the binding is
			// the language guard, not an absent or ambiguous candidate.
			if id, ok := idx.lookupUniqueRealComponentByName("SharedBase"); !ok || id != base.ID {
				t.Fatalf("premise: lookupUniqueRealComponentByName(SharedBase) = (%q,%v), "+
					"want (%q,true) — the cross-file tier could not have bound in this "+
					"fixture for a reason unrelated to the language guard (#6998)",
					id, ok, base.ID)
			}

			// Premise 2 — the same-file tier misses. The consumer's file does
			// not declare `SharedBase`, so nothing but the cross-file tier
			// can produce a binding here.
			if id, ok := idx.lookupLocationKind(consumer.SourceFile, "SharedBase", componentKindFamily); ok {
				t.Fatalf("premise: lookupLocationKind(%q, SharedBase) = (%q,true); the "+
					"consumer file must not declare the name or the same-file tier, not "+
					"the cross-file one, is what this case measures (#6998)",
					consumer.SourceFile, id)
			}

			id, status, handled := idx.lookupStructural(ref)
			if !handled {
				t.Fatalf("lookupStructural did not claim %q (handled=false)", ref)
			}

			if tc.wantBind {
				if id != base.ID {
					t.Fatalf("lookupStructural(%q) = %q (status=%d), want the class "+
						"%q declared in %q — the Python cross-file unique-name tier "+
						"must still bind (#6998 positive control)",
						ref, id, status, base.ID, base.SourceFile)
				}
				if status != statusRewritten {
					t.Fatalf("lookupStructural(%q) status = %d, want statusRewritten=%d (#6998)",
						ref, status, statusRewritten)
				}
				return
			}

			if id == base.ID {
				t.Fatalf("lookupStructural(%q) bound the class %q declared in %q — a "+
					"lang=%q structural address reached the whole-graph unique-name "+
					"fallback, which is python-only. A cross-file guess in another "+
					"language's dialect is a confidently-wrong edge (#6998, #6369)",
					ref, base.ID, base.SourceFile, tc.lang)
			}
			if id != "" {
				t.Fatalf("lookupStructural(%q) = %q, want no binding (#6998)", ref, id)
			}
			if status != statusUnmatched {
				t.Fatalf("lookupStructural(%q) status = %d, want statusUnmatched=%d — the "+
					"address must dangle honestly rather than resolve or report an "+
					"ambiguity it did not find (#6998)", ref, status, statusUnmatched)
			}
		})
	}
}
