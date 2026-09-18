// Issue #7161 — the declarator CONCATENATION axis of the field signature.
//
// # What this file grades, and why #7118's rows could not
//
// buildFieldSignature assembles its second part as
// `name + javaDeclaratorDimensions(node, src)` — a bare concatenation with no
// separator. That is sound only while the dimensions text starts at `[`. JLS
// 10.2 spells the suffix `{Annotation} [ ]`, so a TYPE_USE annotation is part
// of the dimensions node and the text can begin at `@`. The source's
// separating space then sits BETWEEN the two operands, where nothing puts it
// back: `int withAnno @NN [];` emitted `int withAnno@NN []`.
//
// This is a CONCATENATION defect, not a COLLAPSE defect. #7118 (53ede5716)
// shipped the intra-line-whitespace family across six collapse sites and its
// `spacedDims`/`tabbedDims` rows drive the very same helper — but they only
// ever reach it with text starting at `[`, so the missing separator is invisible
// to them and collapsing harder or softer cannot restore it. Verified rather
// than assumed: with the fix reverted the whole 7118 file stays GREEN and only
// the rows below fail.
//
// # Axes
//
// VARIED:
//
//   - the LEADING CONTENT of the dimensions text — a TYPE_USE annotation
//     (`@NN []`, the defect) vs the bracket itself (`[]`, which must NOT gain a
//     space). Both directions are graded, so a fix that inserts the separator
//     unconditionally fails on `plainDims`.
//   - NON-WHITESPACE CONTENT INSIDE the brackets — `[/* a comment */]`. The
//     claim this defect falsifies is that whitespace is the only content the
//     dimensions text can hold; a comment falsifies it just as an annotation
//     does, and a fix that special-cased annotations would leave the comment
//     case free to regress. Its expected value is unchanged by this PR — it is
//     a REGRESSION PIN, and it is asserted on exact equality so that it is.
//   - the DIMENSION COUNT — one annotated dimension vs two (`@NN [] @NN []`),
//     because a fix that patched only the head of the text would still be right
//     on one dimension.
//   - the SITE — the field builder (defective) against the two other places a
//     name can be followed by an annotated dimension: a method PARAMETER
//     (buildMethodSignature) and a RECORD COMPONENT (walk's own replay). Both
//     replay a raw source span instead of concatenating, so both are already
//     correct; they are pinned here so the pair audit has a standing witness
//     rather than a one-off measurement.
//
// HELD CONSTANT (deliberately, and named so the next reader can see what is
// still open): the element type (`int` everywhere), the annotation identity
// (`@NN`, no arguments), the line count (every declaration is single-line, and
// asserted so — a wrap would move the row onto #7114's newline axis), the file,
// the package, and the absence of any initializer. NOT varied and NOT claimed:
// an annotation WITH arguments on a dimension, and whitespace or a comment
// between the name and a `[` that carries no annotation (the dimensions node
// starts at `[` there, so that text is outside the span this file grades).
//
// The fixture was compiled before being asserted on: javac 25.0.3,
// `javac -Xlint:all`, written out as com/example/dims/DimHost.java, exit 0,
// ZERO diagnostics. `@NN` is declared `@Target(ElementType.TYPE_USE)`, which is
// what makes every annotated dimension below legal Java rather than a
// derived-legal construct.

package java_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

const java7161DimsSrc = `package com.example.dims;

import java.lang.annotation.ElementType;
import java.lang.annotation.Retention;
import java.lang.annotation.RetentionPolicy;
import java.lang.annotation.Target;

@Target(ElementType.TYPE_USE)
@Retention(RetentionPolicy.RUNTIME)
@interface NN { }

class DimHost {

    int withAnno @NN [];

    int twoAnnoDims @NN [] @NN [];

    int withComment[/* a comment */];

    int plainDims[];

    int viaParam(int paramAnno @NN []) { return 0; }
}

record DimRec(int recComp @NN []) { }
`

// TestJava7161_AnnotatedDimensionKeepsItsSeparator — #7161. Each row asserts
// the EXACT Signature string: a substring or Contains assertion survives
// deleting the very thing it checks, which has been measured in this repo.
func TestJava7161_AnnotatedDimensionKeepsItsSeparator(t *testing.T) {
	recs := extractJava7073(t, "com/example/dims/DimHost.java", java7161DimsSrc)
	for _, want := range []struct{ kind, name, sig string }{
		// The defect. The space before `@NN` comes from the source and was
		// dropped by the `name + dimensions` concatenation.
		{"SCOPE.Schema", "DimHost.withAnno", "int withAnno @NN []"},

		// Two annotated dimensions — a head-only fix is still wrong here if it
		// ever stops being a separator and starts being a rewrite.
		{"SCOPE.Schema", "DimHost.twoAnnoDims", "int twoAnnoDims @NN [] @NN []"},

		// Regression pin, unchanged by this PR: non-whitespace content INSIDE
		// the brackets, emitted verbatim, with NO separator introduced.
		{"SCOPE.Schema", "DimHost.withComment", "int withComment[/* a comment */]"},

		// The negative direction: text that already starts at `[` must keep
		// touching the name. Kills an unconditional separator.
		{"SCOPE.Schema", "DimHost.plainDims", "int plainDims[]"},

		// Pair-audit witnesses — the other two sites where a name is followed
		// by an annotated dimension. Both replay a raw span, so neither ever
		// had the defect; these rows keep that true.
		{"SCOPE.Operation", "DimHost.viaParam", "int viaParam(int paramAnno @NN [])"},
		{"SCOPE.Schema", "DimRec.recComp", "int recComp @NN []"},
	} {
		got := java7161One(t, recs, want.kind, want.name)
		if got.Signature != want.sig {
			t.Errorf("%s.Signature = %q, want %q", want.name, got.Signature, want.sig)
		}
	}
}

// java7161One returns the single entity of the given kind and name and asserts
// its span is single-line. A wrapped declaration would put a newline inside the
// collapsed span and move the row onto the #7114 axis, where it would no longer
// grade the concatenation.
func java7161One(t *testing.T, recs []types.EntityRecord, kind, name string) types.EntityRecord {
	t.Helper()
	got := findJava7073(recs, kind, name)
	if len(got) != 1 {
		t.Fatalf("want exactly 1 %s named %q, got %d. All entities:\n  %s",
			kind, name, len(got), describeJava7073(recs))
	}
	if got[0].StartLine != got[0].EndLine {
		t.Errorf("%s span = %d-%d, want single-line: a wrapped declaration puts a "+
			"newline inside the collapsed span, so this row would grade #7114's "+
			"axis rather than #7161's concatenation",
			name, got[0].StartLine, got[0].EndLine)
	}
	return got[0]
}
