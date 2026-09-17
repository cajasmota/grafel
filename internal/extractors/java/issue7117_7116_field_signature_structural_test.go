// issue7117_7116_field_signature_structural_test.go — buildFieldSignature used
// to slice the RAW SOURCE TEXT of a field_declaration's span. That one decision
// produced three filed defects (#7114, #7117, #7116), so this file grades the
// STRUCTURAL replacement: every part of the emitted signature is now read from
// the parse tree, so nothing inside an annotation's argument list, a string
// literal, or the initializer can reach the output.
//
// # One test per defect, because two fixes that only fail together grade neither
//
//	TestJava7117_*  the initializer used to be cut at the FIRST '=' anywhere in
//	                the span. An annotation element assignment precedes the
//	                type, so `@Deprecated(since = "1.0") private String key =
//	                "v";` emitted "@Deprecated(since" — type and name BOTH
//	                lost. Graded alongside the two ordinary fields whose
//	                initializer must STILL be dropped, which is the opposite
//	                direction: a "keep everything" fix dies on those.
//	TestJava7116_*  the modifier strip used to be an unanchored ReplaceAll, so
//	                `@SuppressWarnings("public static thing")` lost two words
//	                OUT OF A STRING LITERAL. Its control is a field carrying
//	                those same words as REAL modifiers, which separates "the
//	                modifiers are gone because the right thing removed them"
//	                from "nothing is ever removed".
//	TestJava7118_*  the #7114/#7117 fixtures vary the declaration's LINE
//	                EXTENT, so they grade newline collapse and nothing else.
//	                This grades an intra-line whitespace RUN on a SINGLE-LINE
//	                declaration — the axis #7118 records as ungraded — both
//	                inside the type argument list and between the type and the
//	                name.
//	TestJava7117_Annotated*Wrapped*  composes #7117 with #7114: two annotations,
//	                one of them with an element assignment, on a declaration
//	                that also wraps and also has an initializer.
//	TestJava7117_CStyle*  grades the `dimensions` read three ways — `[]` after
//	                the name, `[]` inside the type, and no `[]` at all — plus a
//	                declarator whose brackets wrap, which is the only grader of
//	                collapseJavaSpaces at the dimensions call site.
//
// Every assertion is the EXACT emitted Signature, never "contains no '='" —
// the pre-fix output "@Deprecated(since" contains no '=' either.
//
// Spans are asserted next to the wrapped case's signature so a fixture that
// LOOKS wrapped in this file but whose parsed field span is single-line cannot
// make the collapse assertion vacuous.
//
// # What happened to #7114's ordering test
//
// TestJava7114_WrappedModifiersAreStrippedFromFieldSignature graded that the
// whitespace collapse ran BEFORE the modifier strip. The modifier strip no
// longer exists — the `modifiers` child is never read — so that ORDER has no
// subject any more. The test is KEPT, not deleted, and its assertion is still
// exactly true and still fails if wrapped modifiers reach the signature; only
// the mechanism it pins has changed. See the note added to its header.
//
// Axes varied here: presence of an annotation, whether the annotation carries
// an element ASSIGNMENT, whether the `=` sits inside a STRING literal, the
// number of annotation elements, presence of an initializer, the declaration's
// whitespace shape (newlines vs intra-line runs vs neither), and — for an
// array — WHICH NODE carries the `[]`: the type (`String[] names`), the
// declarator (`int arr[]`), both (neither field claims it twice), or neither
// (`CACHE`), with `wrappedDims` additionally putting whitespace INSIDE the
// declarator's brackets.
// Held constant: the file, the package, the construct kind (field), the
// enclosing class.
//
// # The visible behaviour change: annotations are GONE from a field signature
//
// Before this diff an unparameterised annotation survived into the signature
// (`@SuppressWarnings("thing") String key`); a parameterised one destroyed it
// (#7117). Now neither appears — the emitted form is exactly "<Type> <name>".
// That is deliberate and it is the one thing here a reader will notice, so it
// is asserted head-on: every row below of a field that HAS an annotation
// expects a signature with no `@` in it at all. The reason is recorded on
// buildFieldSignature: a field's Signature is the only one a consumer parses
// POSITIONALLY (docgen's typeHintFromSignature, gated on SCOPE.Schema/field,
// takes Fields(sig)[0] as the declared type). The annotations a consumer acts
// on are unaffected — @Inject/@Autowired become REFERENCES edges, Bean
// Validation lands in Properties["validations"] — but this is NOT "nothing is
// lost": a MARKER annotation with no consumer (`@Deprecated`, a JPA `@Column`
// outside a NoSQL document, a third-party marker) used to appear in the
// signature and is now on the entity nowhere. See buildFieldSignature for why
// that is narrow: anything with an element was already truncated at its `=`.
//
// This also means #7116's defect is no longer merely fixed but UNREACHABLE:
// no annotation text is read, so no text inside one can be mangled. Its test
// is kept as the regression pin — a revert to the raw-span implementation
// emits `@SuppressWarnings("thing") String masked`, which is not the expected
// string.
//
// The fixture below was compiled before being asserted on: javac 25.0.3,
// written out as com/example/sig/FieldSig.java, exit 0, zero diagnostics.

package java_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// java7117FieldSigSrc — @Query and @Column are declared in-file so the fixture
// compiles standalone without a Spring/JPA classpath.
const java7117FieldSigSrc = `package com.example.sig;

import java.util.Map;

public class FieldSig {

    @Deprecated(since = "1.0") private String key = "v";

    @SuppressWarnings("public static thing") private String masked;

    public static final String realMods = "x";

    private int count = 5;

    public static final Map<String,String> CACHE = Map.of();

    @Query("a = b") private String q;

    @Column(name = "x", nullable = false) private String col;

    @Deprecated
    @Column(name = "y")
    private java.util.Map<String, java.util.List<String>>
            wrapped = new java.util.HashMap<>();

    private Map<String,   String>   spaced   =   Map.of();

    private int arr[];

    private String[] names;

    private int wrappedDims[
            ];

    @interface Query { String value(); }

    @interface Column { String name(); boolean nullable() default true; }
}
`

// java7117Sig fetches the one SCOPE.Schema record named `name` and returns it.
func java7117Sig(t *testing.T, recs []types.EntityRecord, name string) types.EntityRecord {
	t.Helper()
	got := findJava7073(recs, "SCOPE.Schema", name)
	if len(got) != 1 {
		t.Fatalf("want exactly 1 SCOPE.Schema named %q, got %d. All entities:\n  %s",
			name, len(got), describeJava7073(recs))
	}
	return got[0]
}

// TestJava7117_AnnotationElementAssignmentKeepsTypeAndName — #7117. An
// annotation that assigns an element must not destroy the signature, in any of
// its three spellings (single element, element inside a string literal,
// multiple elements) — while a field with a GENUINE initializer must still
// have it dropped.
func TestJava7117_AnnotationElementAssignmentKeepsTypeAndName(t *testing.T) {
	recs := extractJava7073(t, "com/example/sig/FieldSig.java", java7117FieldSigSrc)
	for _, want := range []struct{ name, sig string }{
		// The defect: `=` in an annotation element, before the type.
		// Pre-fix this emitted "@Deprecated(since".
		{"FieldSig.key", "String key"},
		// `=` inside the annotation's STRING literal. Pre-fix: "@Query(\"a".
		{"FieldSig.q", "String q"},
		// Two element assignments. Pre-fix: "@Column(name".
		{"FieldSig.col", "String col"},
		// Opposite direction — a real initializer must STILL be dropped, and
		// these two were already correct pre-fix, so they are the regression
		// floor a "stop dropping anything" fix falls through.
		{"FieldSig.count", "int count"},
		{"FieldSig.CACHE", "Map<String,String> CACHE"},
	} {
		if got := java7117Sig(t, recs, want.name).Signature; got != want.sig {
			t.Errorf("%s.Signature = %q, want %q", want.name, got, want.sig)
		}
	}
}

// TestJava7116_ModifierKeywordsInsideAStringLiteralAreNotEaten — #7116. The
// words must be INSIDE a string literal to matter, so `masked` is paired with
// `realMods`, which carries the same three words as actual modifiers on the
// same class. Together they separate "the modifiers are gone because the right
// thing removed them" from "nothing is ever removed": emitting the whole raw
// span leaves realMods red, and restoring the unanchored ReplaceAll over that
// span leaves masked red.
func TestJava7116_ModifierKeywordsInsideAStringLiteralAreNotEaten(t *testing.T) {
	recs := extractJava7073(t, "com/example/sig/FieldSig.java", java7117FieldSigSrc)
	for _, want := range []struct{ name, sig string }{
		// Pre-fix: "@SuppressWarnings(\"thing\") String masked" — two words
		// gone from inside the literal. Now the annotation is not read at all.
		{"FieldSig.masked", "String masked"},
		// Control: `public static final` as REAL modifiers, all removed.
		{"FieldSig.realMods", "String realMods"},
	} {
		if got := java7117Sig(t, recs, want.name).Signature; got != want.sig {
			t.Errorf("%s.Signature = %q, want %q", want.name, got, want.sig)
		}
	}
}

// TestJava7118_IntraLineWhitespaceRunsAreCollapsed — #7118. The #7114/#7117
// fixtures only vary the LINE EXTENT, so they grade newline collapse alone.
// `spaced` is SINGLE-LINE (asserted) and carries whitespace runs in two
// distinct positions: inside the type's argument list, where only collapsing
// the type node's own text fixes it, and between the type and the name, where
// only joining the parts with a single space does. Emitting the raw type text
// leaves the first red; concatenating the parts without normalising leaves the
// second red.
func TestJava7118_IntraLineWhitespaceRunsAreCollapsed(t *testing.T) {
	recs := extractJava7073(t, "com/example/sig/FieldSig.java", java7117FieldSigSrc)
	got := java7117Sig(t, recs, "FieldSig.spaced")
	if got.StartLine != 26 || got.EndLine != 26 {
		t.Errorf("FieldSig.spaced span = %d-%d, want 26-26 (a multi-line span here would "+
			"make this a newline test, which #7114 already covers)", got.StartLine, got.EndLine)
	}
	if want := "Map<String, String> spaced"; got.Signature != want {
		t.Errorf("FieldSig.spaced.Signature = %q, want %q", got.Signature, want)
	}
}

// TestJava7117_CStyleArrayDimensionsSurvive — the C-style array suffix
// (JLS 10.2, `int arr[]`) is a `dimensions` child of the variable_declarator,
// not part of the `type` node, so reading type+name alone silently drops it —
// a regression the raw-span implementation could not have had.
//
// `names` is the control in the OTHER spelling: `String[] names` parses as an
// `array_type` type node whose own text already carries the `[]`, and whose
// declarator has NO `dimensions` child (dumped: nil). It is what grades the
// dimensions read as a READ rather than as a guess — without it, appending
// "[]" whenever the type text ends in "[]" emits `String[] names[]` and
// nothing in the package notices (scored: 0 `--- FAIL` before this row, 1
// after). `CACHE` is the third leg, a non-array field: it is what the blunt
// always-append-"[]" mutant dies on.
func TestJava7117_CStyleArrayDimensionsSurvive(t *testing.T) {
	recs := extractJava7073(t, "com/example/sig/FieldSig.java", java7117FieldSigSrc)
	for _, want := range []struct{ name, sig string }{
		// `[]` after the NAME: a `dimensions` child of the declarator.
		{"FieldSig.arr", "int arr[]"},
		// `[]` inside the TYPE, declarator dimensions nil — must not be
		// doubled.
		{"FieldSig.names", "String[] names"},
		// Not an array at all: nothing may be appended.
		{"FieldSig.CACHE", "Map<String,String> CACHE"},
	} {
		if got := java7117Sig(t, recs, want.name).Signature; got != want.sig {
			t.Errorf("%s.Signature = %q, want %q", want.name, got, want.sig)
		}
	}
	// `wrappedDims` puts a NEWLINE inside the brackets — legal Java (JLS 10.2
	// places no whitespace restriction between `[` and `]`), and the only way
	// the dimensions text can contain whitespace at all, since the node starts
	// at `[`. It is what grades collapseJavaSpaces AT THE DIMENSIONS SITE,
	// which is a second, separate call from the one on the type: without the
	// collapse this emits "int wrappedDims[\n            ]" — a newline and
	// source indentation in a persisted signature, exactly #7114's defect at
	// the one place #7114's own fixture cannot reach. The span assertion is
	// what stops it degrading into a single-line case.
	dims := java7117Sig(t, recs, "FieldSig.wrappedDims")
	if dims.StartLine != 32 || dims.EndLine != 33 {
		t.Errorf("FieldSig.wrappedDims span = %d-%d, want 32-33 (a single-line span here "+
			"would leave the dimensions collapse ungraded)", dims.StartLine, dims.EndLine)
	}
	if want := "int wrappedDims[ ]"; dims.Signature != want {
		t.Errorf("FieldSig.wrappedDims.Signature = %q, want %q", dims.Signature, want)
	}
}

// TestJava7117_AnnotatedWrappedFieldWithInitializer — the composition, since
// these defects compose in real code: two annotations (one a marker, one with
// an element assignment) on a declaration that ALSO wraps its type across
// lines and ALSO has an initializer. The span assertion is what stops this
// from silently degrading into a single-line case.
func TestJava7117_AnnotatedWrappedFieldWithInitializer(t *testing.T) {
	recs := extractJava7073(t, "com/example/sig/FieldSig.java", java7117FieldSigSrc)
	got := java7117Sig(t, recs, "FieldSig.wrapped")
	if got.StartLine != 21 || got.EndLine != 24 {
		t.Errorf("FieldSig.wrapped span = %d-%d, want 21-24 (a single-line span here would "+
			"make the collapse assertion vacuous)", got.StartLine, got.EndLine)
	}
	want := "java.util.Map<String, java.util.List<String>> wrapped"
	if got.Signature != want {
		t.Errorf("FieldSig.wrapped.Signature = %q, want %q", got.Signature, want)
	}
}
