package python

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6990, residue 2 — the relational-target scan ran forward a raw 400
// BYTES from the matched field, unbounded by the declaration it belonged to. A
// field whose own target argument the regex cannot parse therefore captured the
// NEXT field's target. `LogEntry.user`, declared
// `models.ForeignKey(settings.AUTH_USER_MODEL, ...)`, came out targeting
// `ContentType` — a real model, a bound edge, the wrong thing.
//
// The window is now bounded by the field's OWN call parentheses
// (`djangoDeclEnd`). The regex is UNCHANGED: this ships ONE guard, so it stays
// graded (#6988's lesson — two guards that only fire together grade neither).
//
// SCOPE. Residue 1 of #6990 — `class CTForeignKey(GenericForeignKey)` passing
// `isDjangoRelationalField`'s suffix check at django.go — is NOT touched here.
// It is a policy decision (deny-list vs allow-list) that #6988/#6989 settled
// once, and it is pinned by TestIssue6988_RegexBoundaryRejected_WouldDropSubclasses.
//
// GATE (#6966): every edge below exists only with the custom Python lane ON.
// It is off by default (`WithCustomExtractors`), so default-index baselines are
// untouched. Graph-level incidence of the defect on the real django corpus is
// NOT ESTABLISHED and is not claimed anywhere in this file; what is graded here
// is extractor-level behaviour.

// ---------------------------------------------------------------------------
// The bound itself: djangoDeclEnd.
// ---------------------------------------------------------------------------

// TestIssue6990_DeclEnd_FindsTheCallsOwnCloser grades every place a `)` can
// appear without closing the call. Only the unit level can observe these: the
// target is the FIRST argument, so an EARLY close still contains it and is
// invisible end-to-end. A LATE close is the bug.
func TestIssue6990_DeclEnd_FindsTheCallsOwnCloser(t *testing.T) {
	cases := []struct {
		name string
		body string // `|` marks the expected end (one past the closing paren)
	}{
		{"flat", `f(A)|`},
		{"nested call", `f(A, on_delete=models.CASCADE)|`},
		{"double nested", `f(A, default=(1, (2, 3)))|`},
		{"paren in double-quoted string", `f(A, help_text="see (the docs)")|`},
		{"paren in single-quoted string", `f(A, help_text='see (the docs)')|`},
		{"paren in triple-quoted string", `f(A, help_text="""see (the
docs)""")|`},
		{"paren in comment", `f(A,  # a ) here
    on_delete=CASCADE)|`},
		{"hash inside string is not a comment", `f(A, default="#)")|`},
		{"apostrophe inside comment is not a string", `f(A,  # don't ) do this
    x=1)|`},
		{"escaped quote inside string", `f(A, default="a\")b")|`},
		{"multi-line with trailing comma", `f(
    "catalog.Author",
    on_delete=models.CASCADE,
)|`},
		{"stops at its OWN closer, not a later one", `f(A)| g(B)`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want := strings.IndexByte(c.body, '|')
			if want < 0 {
				t.Fatalf("fixture %q has no `|` end marker", c.body)
			}
			body := strings.Replace(c.body, "|", "", 1)
			if got := djangoDeclEnd(body, 1); got != want {
				t.Errorf("djangoDeclEnd(%q, 1) = %d, want %d (slice %q, want %q) (#6990)",
					body, got, want, body[:max(got, 0)], body[:want])
			}
		})
	}
}

// TestIssue6990_DeclEnd_RefusesToGuess pins the -1 side. The caller falls back
// to the pre-#6990 400-byte window there, which is EXACTLY the old behaviour —
// never wider. A guess (end-of-body, say) would be strictly worse than the bug
// this fixes, so "unbalanced" must be reported, not papered over.
func TestIssue6990_DeclEnd_RefusesToGuess(t *testing.T) {
	cases := []struct{ name, body string }{
		{"never closed", `f(A, on_delete=CASCADE`},
		{"unterminated string", `f(A, help_text="oops)`},
		{"unterminated triple-quoted string", `f(A, help_text="""oops)`},
		{"unterminated trailing comment", `f(A,  # oops`},
		{"depth never returns to zero", `f((A)`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := djangoDeclEnd(c.body, 1); got != -1 {
				t.Errorf("djangoDeclEnd(%q, 1) = %d, want -1 — an unbalanced source must fall back "+
					"to the byte window, not guess an end (#6990)", c.body, got)
			}
		})
	}
	// openPos must actually name a `(`.
	for _, pos := range []int{-1, 0, 99} {
		if got := djangoDeclEnd(`f(A)`, pos); got != -1 {
			t.Errorf("djangoDeclEnd(%q, %d) = %d, want -1", `f(A)`, pos, got)
		}
	}
}

// ---------------------------------------------------------------------------
// End to end. Two axes crossed: DECLARATION LAYOUT x TARGET FORM.
// ---------------------------------------------------------------------------

// declLayout6990 renders one field declaration three ways. The argument text is
// identical in all three; only the line structure varies. RE2's `\s` matches
// `\n`, so multi-line coverage in this package has been INCIDENTAL and never
// asserted — which is exactly how a narrowing walks through a pin.
type declLayout6990 struct {
	name   string
	render func(attr, ctor, args string) string
}

var declLayouts6990 = []declLayout6990{
	{"single-line", func(attr, ctor, args string) string {
		return fmt.Sprintf("    %s = %s(%s, on_delete=models.CASCADE)", attr, ctor, args)
	}},
	{"multi-line", func(attr, ctor, args string) string {
		return fmt.Sprintf("    %s = %s(\n        %s,\n        on_delete=models.CASCADE)", attr, ctor, args)
	}},
	{"multi-line-trailing-comma", func(attr, ctor, args string) string {
		return fmt.Sprintf("    %s = %s(\n        %s,\n        on_delete=models.CASCADE,\n    )", attr, ctor, args)
	}},
}

// TestIssue6990_TargetFormsSurviveEveryDeclarationLayout crosses the two axes.
//
// VARIED: declaration layout (single-line / multi-line / multi-line with a
// trailing comma) x target form (dotted lowercase `settings.AUTH_USER_MODEL`,
// bare CamelCase symbol, quoted `"app.Model"`, quoted `'self'`).
//
// HELD CONSTANT and graded elsewhere: the constructor (`models.ForeignKey`
// throughout — the constructor gate is #6988's and is pinned by
// TestIssue6988_GateKeepsRelationalConstructors); the regex's left boundary
// (unchanged by #6990, pinned by TestIssue6988_RegexBoundaryRejected_WouldDropSubclasses);
// and the `[A-Z]` symbol anchor (pinned by
// TestIssue6988_RegexRejectsLowercaseInitialSymbol).
//
// EVERY case carries a SENTINEL sibling declared immediately after it with a
// different target. That is what makes the negative rows non-vacuous: if the
// window still ran past the declaration it would capture `Sentinel`, and the
// "want no target" rows would fail LOUDLY rather than pass by producing nothing.
func TestIssue6990_TargetFormsSurviveEveryDeclarationLayout(t *testing.T) {
	targetForms := []struct {
		name string
		args string
		want string // "" = no target edge at all
	}{
		{"dotted-lowercase-settings", "settings.AUTH_USER_MODEL", ""},
		{"bare-CamelCase-symbol", "Profile", "Profile"},
		{"quoted-app-dot-Model", `"catalog.Author"`, "Author"},
		{"quoted-self", `'self'`, "Owner"},
	}
	for _, layout := range declLayouts6990 {
		for _, form := range targetForms {
			t.Run(layout.name+"/"+form.name, func(t *testing.T) {
				src := "from django.conf import settings\nfrom django.db import models\n\n\n" +
					"class Owner(models.Model):\n" +
					layout.render("subject", "models.ForeignKey", form.args) + "\n" +
					layout.render("sentinel", "models.ForeignKey", "Sentinel") + "\n"
				rels := djangoEdges6988(t, src)

				got := fieldTargetEdgesFrom6988(rels, "Owner.subject")
				if form.want == "" {
					if len(got) != 0 {
						t.Errorf("Owner.subject emitted %d field_target_type edge(s), want 0; first ToID=%q. "+
							"A target here is the scan reading past this declaration's own `)` (#6990)",
							len(got), got[0].ToID)
					}
				} else {
					if len(got) != 1 {
						t.Fatalf("Owner.subject emitted %d field_target_type edge(s), want exactly 1 "+
							"-> %s. The #6990 window bound must not cost a legitimate target (source:\n%s)",
							len(got), form.want, src)
					}
					if want := fieldTargetRef6988(form.want); got[0].ToID != want {
						t.Errorf("Owner.subject -> %q, want %q (#6990)", got[0].ToID, want)
					}
				}

				// POSITIVE CONTROL on every single row, including the negative
				// ones: the sentinel is the field whose target an over-reading
				// window would steal, and it must resolve on its own.
				sent := fieldTargetEdgesFrom6988(rels, "Owner.sentinel")
				if len(sent) != 1 || sent[0].ToID != fieldTargetRef6988("Sentinel") {
					t.Fatalf("Owner.sentinel did not resolve to Sentinel (%d edge(s)) — the control is "+
						"gone, so the assertion above proves nothing (source:\n%s)", len(sent), src)
				}
			})
		}
	}
}

// TestIssue6990_NamedResidue2_RealFieldFollowedByGFK is the issue's named
// shape, as a forbidden row: a legitimate relational field whose target the
// regex cannot parse, sitting directly above the `GenericForeignKey` whose
// `ct_field` STRING the old window captured. This is the #6988 case seen from
// the other side — the gate rejects the GFK constructor, but until #6990 it did
// not stop the regex READING the GFK's text on a neighbouring field's behalf.
func TestIssue6990_NamedResidue2_RealFieldFollowedByGFK(t *testing.T) {
	const src = `from django.conf import settings
from django.contrib.contenttypes.fields import GenericForeignKey
from django.db import models


class Note(models.Model):
    user = models.ForeignKey(settings.AUTH_USER_MODEL, on_delete=models.CASCADE)
    content_type = models.ForeignKey(ContentType, on_delete=models.CASCADE)
    object_id = models.PositiveIntegerField()
    content_object = GenericForeignKey("content_type", "object_id")
`
	rels := djangoEdges6988(t, src)

	if got := fieldTargetEdgesFrom6988(rels, "Note.user"); len(got) != 0 {
		t.Errorf("Note.user emitted %d field_target_type edge(s), want 0; first ToID=%q. Its own "+
			"declaration names settings.AUTH_USER_MODEL and nothing else (#6990 residue 2)",
			len(got), got[0].ToID)
	}
	// #6988's row, restated here because #6990 must not reopen it.
	if got := fieldTargetEdgesFrom6988(rels, "Note.content_object"); len(got) != 0 {
		t.Errorf("Note.content_object emitted %d field_target_type edge(s), want 0; first ToID=%q (#6988)",
			len(got), got[0].ToID)
	}
	// Controls: the two fields that SHOULD resolve, still do.
	if got := fieldTargetEdgesFrom6988(rels, "Note.content_type"); len(got) != 1 ||
		got[0].ToID != fieldTargetRef6988("ContentType") {
		t.Fatalf("Note.content_type did not resolve to ContentType (%d edge(s)) — the negatives above "+
			"are vacuous without it", len(got))
	}
	if !hasEdge6988(rels, pyClassRef("Note"), "Note.content_object", string(types.RelationshipKindContains)) {
		t.Error("Note CONTAINS content_object missing: #6990 bounds the target scan, it does not drop fields")
	}
}

// TestIssue6990_TheBoundIsTheSoleGuard shows that the WINDOW is what rejects the
// bad match, not the regex — so the shipped guard is graded on its own. Handed
// the OLD over-reading text, the unchanged regex still happily captures
// `ContentType`; handed the bounded text, there is nothing to capture.
func TestIssue6990_TheBoundIsTheSoleGuard(t *testing.T) {
	const twoFields = `    user = models.ForeignKey(settings.AUTH_USER_MODEL, on_delete=models.CASCADE)
    content_type = models.ForeignKey(ContentType, on_delete=models.CASCADE)`

	// The pre-#6990 window (byte-counted, spanning both declarations).
	if got := djangoRelTarget(twoFields, "LogEntry"); got != "ContentType" {
		t.Fatalf("djangoRelTarget over the OLD unbounded window = %q, want \"ContentType\": this test's "+
			"premise is that the regex is unchanged and still reads the next field (#6990)", got)
	}

	// The post-#6990 window: the first declaration, bounded at its own `)`.
	open := strings.IndexByte(twoFields, '(')
	end := djangoDeclEnd(twoFields, open)
	if end < 0 {
		t.Fatal("djangoDeclEnd failed on a balanced declaration")
	}
	if got := djangoRelTarget(twoFields[:end], "LogEntry"); got != "" {
		t.Errorf("djangoRelTarget over the BOUNDED window = %q, want \"\" — the bound alone must be "+
			"what rejects the over-read (#6990)", got)
	}
}

// TestIssue6990_UnbalancedSourceFallsBackToTheOldByteWindow grades the
// `end < 0` branch — the one path where a byte count survives #6990. When
// `djangoDeclEnd` cannot find the declaration's closer (a truncated or
// syntactically broken file), the scan reverts to the pre-#6990 400-byte
// window, which is deliberately EXACTLY the old behaviour.
//
// "Exactly the old behaviour" has TWO halves and this test asserts BOTH,
// because the fallback's whole reason to exist is that it PRESERVES prior
// behaviour rather than dropping edges:
//
//   - NOT WIDER. A target more than 400 bytes past the field must NOT be
//     captured — a fallback of `len(body)` would make an unbalanced file read
//     wider than the bug #6990 fixes.
//   - NOT NARROWER. A target WITHIN 400 bytes must still BE captured — a
//     fallback of `fIdx[0]` (an empty window) silently drops every target in
//     an unbalanced file, and until this half was asserted that build was
//     indistinguishable from this one.
//
// Both halves run on genuinely unbalanced declarations, each with its own
// `djangoDeclEnd == -1` premise control, so neither is quietly grading the
// normal path instead.
func TestIssue6990_UnbalancedSourceFallsBackToTheOldByteWindow(t *testing.T) {
	filler := strings.Repeat("    # padding to push the next declaration past 400 bytes\n", 10)
	if len(filler) <= 400 {
		t.Fatalf("filler is %d bytes, need >400 for this test to distinguish the two fallbacks", len(filler))
	}
	src := "from django.conf import settings\nfrom django.db import models\n\n\n" +
		// FAR: the target the window must NOT reach. Unterminated declaration,
		// then >400 bytes of filler, then a parseable `ForeignKey(Sentinel`.
		"class Owner(models.Model):\n" +
		"    subject = models.ForeignKey(settings.AUTH_USER_MODEL,\n" + // never closed
		filler +
		"    sentinel = models.ForeignKey(Sentinel, on_delete=models.CASCADE)\n" +
		"\n\n" +
		// NEAR: the target the window MUST still reach. Equally unterminated,
		// so it takes the same fallback branch — but its own target sits
		// immediately after its own `(`, well inside 400 bytes.
		"class NearOwner(models.Model):\n" +
		"    near = models.ForeignKey(Nearby,\n" // never closed

	// Premise controls, one per half: both declarations really do take the
	// `end < 0` fallback, so neither assertion is grading the normal path.
	for _, decl := range []string{"    subject", "    near"} {
		body := src[strings.Index(src, decl):]
		if got := djangoDeclEnd(body, strings.IndexByte(body, '(')); got != -1 {
			t.Fatalf("djangoDeclEnd for %q = %d, want -1: that half of the fixture is not "+
				"exercising the fallback branch", strings.TrimSpace(decl), got)
		}
	}

	rels := djangoEdges6988(t, src)

	// NOT WIDER.
	if got := fieldTargetEdgesFrom6988(rels, "Owner.subject"); len(got) != 0 {
		t.Errorf("Owner.subject emitted %d field_target_type edge(s), want 0; first ToID=%q. The "+
			"unbalanced-source fallback must stay at the pre-#6990 400-byte window, never WIDER",
			len(got), got[0].ToID)
	}
	// NOT NARROWER. Without this row, a fallback of `end = fIdx[0]` — an empty
	// window that finds nothing at all on any unbalanced file — passes every
	// other assertion in this package.
	if got := fieldTargetEdgesFrom6988(rels, "NearOwner.near"); len(got) != 1 ||
		got[0].ToID != fieldTargetRef6988("Nearby") {
		ids := make([]string, 0, len(got))
		for _, r := range got {
			ids = append(ids, r.ToID)
		}
		t.Errorf("NearOwner.near emitted %d field_target_type edge(s) %v, want exactly 1 -> %q. Its "+
			"own target is inside the 400-byte fallback window, so the fallback must still find it — "+
			"it exists to PRESERVE the old behaviour, not to drop every target on an unbalanced file",
			len(got), ids, fieldTargetRef6988("Nearby"))
	}
	// Control on the FAR half: the field whose target an over-reading fallback
	// would steal resolves on its own, so the negative above is not vacuous.
	if got := fieldTargetEdgesFrom6988(rels, "Owner.sentinel"); len(got) != 1 ||
		got[0].ToID != fieldTargetRef6988("Sentinel") {
		t.Fatalf("Owner.sentinel did not resolve to Sentinel (%d edge(s)) — the NOT-WIDER negative "+
			"above is vacuous", len(got))
	}
}
