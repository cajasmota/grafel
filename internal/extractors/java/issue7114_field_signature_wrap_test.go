// issue7114_field_signature_wrap_test.go — buildFieldSignature is the FIFTH
// signature builder in java.go, and the only one that never collapses interior
// whitespace (issue #7114). #7091/#7112 graded the four sites that DO call
// `strings.Join(strings.Fields(raw), " ")`; a grep for that expression cannot
// see a site that has the defect by OMISSION.
//
// # Two consequences, and the second is worse than the first
//
//  1. A wrapped field keeps embedded newlines and source indentation in
//     `Signature`, which is persisted and is what a reader sees in
//     grafel_inspect and grafel_get_source.
//  2. The visibility strip below it is a list of patterns that each require a
//     TRAILING SPACE ("public ", "static ", "final "). On a declaration whose
//     modifiers wrap, "public " does not match "public\n", so the modifier
//     SURVIVES into the signature. The missing collapse silently defeats the
//     step that runs after it.
//
// The two are asserted by SEPARATE tests against SEPARATE fields, because they
// fail independently under an ordering mistake: putting the collapse AFTER the
// modifier strip fixes (1) and leaves (2) broken, and two assertions that only
// fail together would grade neither.
//
// # Both directions
//
// Every assertion is the EXACT emitted string. The wrapped rows die when the
// collapse is DELETED; the single-line controls die when the collapse is made
// too AGGRESSIVE (joining on "" instead of " "), which a "contains no newline"
// assertion would survive.
//
// Spans are asserted alongside the signatures so a fixture that LOOKS wrapped
// in this file but whose parsed field span is single-line — this defect in
// disguise — cannot masquerade as a multi-line case.
//
// Axis varied: the declaration's LINE EXTENT (wrapped vs single-line). In the
// modifier test the modifier SET also varies (CACHE is `public static final`,
// LIMIT is `private static final`) — the strip removes all of those patterns, so
// the emitted signature does not depend on which of them is present. Held
// constant: the file, the package, the construct kind (field), and the
// enclosing class.
//
// The fixture below was compiled before being asserted on: javac 25.0.3,
// written out as com/example/wrap/FieldWrap.java, zero errors.
//
// # UPDATE (#7117/#7116): the ORDER this file pinned no longer has a subject
//
// buildFieldSignature no longer manipulates the raw span at all — it reads
// `field_declaration`'s `type` and `variable_declarator` children and skips the
// `modifiers` child except for the annotations inside it. There is therefore no
// modifier strip left, and consequence (2) below can no longer be reached by
// getting an ORDER wrong: modifiers are never in scope to survive.
//
// Both tests are KEPT anyway, unchanged, because their ASSERTIONS are still
// exactly true and still load-bearing however the signature is computed:
// a wrapped type must not carry newlines or source indentation (still graded
// by the `index` row, which dies if the type node's text is emitted raw), and
// wrapped modifiers must not appear in the signature (still graded by the
// `CACHE` row, which dies if the modifiers child is emitted). What is gone is
// only the ORDER mutant's meaning: moving a collapse below a strip is no
// longer an expressible mutation here. The signatures this file expects are
// byte-identical before and after the structural rewrite — none of these four
// fields carries an annotation, which is the one part of the rendering that
// changed (see issue7117_7116_field_signature_structural_test.go).

package java_test

import "testing"

// java7114FieldSrc — four fields of one class.
//
//	index  wraps its declared TYPE onto a second line (consequence 1)
//	count  is the single-line control for index
//	CACHE  wraps its MODIFIERS onto separate lines (consequence 2)
//	LIMIT  is the single-line control for CACHE (`private static final`, i.e.
//	       a DIFFERENT set from CACHE's `public static final`; every one of
//	       those patterns is stripped, so the expected signature is unaffected)
const java7114FieldSrc = `package com.example.wrap;

import java.util.Map;

public class FieldWrap {

    private java.util.Map<String, java.util.List<String>>
            index = new java.util.HashMap<>();

    private int count = 0;

    public
    static
    final Map<String, String> CACHE = Map.of();

    private static final int LIMIT = 10;
}
`

// TestJava7114_WrappedFieldTypeSignatureIsCollapsed — consequence 1. A field
// whose declared type and name sit on different lines must not carry the
// newline and the source indentation into the persisted signature.
func TestJava7114_WrappedFieldTypeSignatureIsCollapsed(t *testing.T) {
	recs := extractJava7073(t, "com/example/wrap/FieldWrap.java", java7114FieldSrc)
	for _, want := range []struct {
		name       string
		start, end int
		sig        string
	}{
		{"FieldWrap.index", 7, 8, "java.util.Map<String, java.util.List<String>> index"},
		{"FieldWrap.count", 10, 10, "int count"},
	} {
		got := findJava7073(recs, "SCOPE.Schema", want.name)
		if len(got) != 1 {
			t.Errorf("want exactly 1 SCOPE.Schema named %q, got %d. All entities:\n  %s",
				want.name, len(got), describeJava7073(recs))
			continue
		}
		if got[0].StartLine != want.start || got[0].EndLine != want.end {
			t.Errorf("%s span = %d-%d, want %d-%d (a single-line span here would make the "+
				"signature assertion vacuous)", want.name, got[0].StartLine, got[0].EndLine, want.start, want.end)
		}
		if got[0].Signature != want.sig {
			t.Errorf("%s.Signature = %q, want %q", want.name, got[0].Signature, want.sig)
		}
	}
}

// TestJava7114_WrappedModifiersAreStrippedFromFieldSignature — consequence 2.
// Every pattern in the visibility strip carries a trailing space, so it only
// fires on text that has already been collapsed. This test is what pins the
// ORDER: it stays red if the collapse is added after the strip.
func TestJava7114_WrappedModifiersAreStrippedFromFieldSignature(t *testing.T) {
	recs := extractJava7073(t, "com/example/wrap/FieldWrap.java", java7114FieldSrc)
	for _, want := range []struct {
		name       string
		start, end int
		sig        string
	}{
		{"FieldWrap.CACHE", 12, 14, "Map<String, String> CACHE"},
		{"FieldWrap.LIMIT", 16, 16, "int LIMIT"},
	} {
		got := findJava7073(recs, "SCOPE.Schema", want.name)
		if len(got) != 1 {
			t.Errorf("want exactly 1 SCOPE.Schema named %q, got %d. All entities:\n  %s",
				want.name, len(got), describeJava7073(recs))
			continue
		}
		if got[0].StartLine != want.start || got[0].EndLine != want.end {
			t.Errorf("%s span = %d-%d, want %d-%d (a single-line span here would make the "+
				"signature assertion vacuous)", want.name, got[0].StartLine, got[0].EndLine, want.start, want.end)
		}
		if got[0].Signature != want.sig {
			t.Errorf("%s.Signature = %q, want %q", want.name, got[0].Signature, want.sig)
		}
	}
}
