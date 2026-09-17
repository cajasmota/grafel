// issue7124_class_signature_order_test.go - buildClassSignature cut at the
// body brace BEFORE stripping annotation arguments, so a '{' inside a
// class-level annotation's string literal truncated the declaration inside the
// literal (#7124, item 1).
//
// buildMethodSignature already carries a comment naming this exact hazard and
// already runs stripAnnotationArgs first; buildClassSignature ran the two steps
// in the opposite order. Measured on 2783920c2 before the fix:
//
//	@Table(name = "{weird}") class Foo            -> "@Table"
//	@Table(name = "{a{b}c}") class NestedBrace    -> "@Table"
//	@Table(name = "{w}") class Wrapped extends... -> "@Table"
//
// The class NAME is gone - not merely a trailing clause. The fix is the
// sibling's ordering, nothing cleverer: #7124 records that making the text
// matching smarter is what produced every defect in this family.
//
// # Grading
//
// Axis varied: whether the annotation argument's string literal contains '{'.
// Held constant between the defect row (Foo) and its control (Bar): the file,
// the package, the annotation type, the argument's element name, the modifiers,
// the class body, and the enclosing scope. Foo and Bar differ in ONE character
// and nothing else.
//
// The remaining rows are non-regression controls for the reorder, each a shape
// the reorder could plausibly have disturbed: a bare class, a generic class, a
// visibility-modified class (the modifier strip now runs AFTER the annotation
// strip, so it must still remove the real "public "), and a wrapped
// extends/implements declaration (#7091's whitespace collapse is adjacent).
// These rows must NOT redden when the order is swapped back - that is what
// makes Foo's row grade the ORDER rather than merely the presence of a
// signature.
//
// Every source below was compiled before being asserted on: javac 25.0.3,
// Foo.java and Modified.java under the names their declarations require, zero
// diagnostics. "public" cannot appear on a second top-level class in one file,
// which is why the modifier row is its own compilation unit.

package java_test

import "testing"

const java7124FooSrc = `package com.example.sig;

@interface Table {
    String name();
}

@Table(name = "{weird}")
class Foo {
    int a;
}

@Table(name = "{a{b}c}")
class NestedBrace {
    int b;
}

@Table(name = "weird")
class Bar {
    int c;
}

class Plain {
}

class Box<T> {
    T t;
}

@Table(name = "kin")
class Extending
        extends Base
        implements Marker {
}

class Base {
}

interface Marker {
}
`

const java7124ModifiedSrc = `package com.example.sig;

@Table(name = "public thing")
public class Modified {
    int d;
}
`

func TestJava7124_ClassSignatureStripsAnnotationArgsBeforeBraceCut(t *testing.T) {
	recs := extractJava7073(t, "com/example/sig/Foo.java", java7124FooSrc)
	for _, want := range []struct{ name, sig string }{
		// The defect row: the "{" inside the literal used to cut here.
		{"Foo", "@Table class Foo"},
		// Braces nested inside the literal. stripAnnotationArgs balances
		// PARENTHESES, so the nesting depth of braces is irrelevant to it.
		{"NestedBrace", "@Table class NestedBrace"},
		// The control: identical to Foo but for the "{" in the literal. It
		// emitted the full signature before the fix and must still do so.
		{"Bar", "@Table class Bar"},
		// Non-regression controls.
		{"Plain", "class Plain"},
		{"Box", "class Box<T>"},
		{"Extending", "@Table class Extending extends Base implements Marker"},
	} {
		got := findJava7073(recs, "SCOPE.Component", want.name)
		if len(got) != 1 {
			t.Errorf("want exactly 1 SCOPE.Component named %q, got %d. All entities:\n  %s",
				want.name, len(got), describeJava7073(recs))
			continue
		}
		if got[0].Signature != want.sig {
			t.Errorf("%s.Signature = %q, want %q", want.name, got[0].Signature, want.sig)
		}
	}
}

// The visibility modifier is stripped AFTER the annotation arguments now. The
// real "public " must still go, and the "public " inside the annotation literal
// must not be what is acted on.
func TestJava7124_ClassSignatureStillStripsVisibilityAfterReorder(t *testing.T) {
	recs := extractJava7073(t, "com/example/sig/Modified.java", java7124ModifiedSrc)
	got := findJava7073(recs, "SCOPE.Component", "Modified")
	if len(got) != 1 {
		t.Fatalf("want exactly 1 SCOPE.Component named %q, got %d. All entities:\n  %s",
			"Modified", len(got), describeJava7073(recs))
	}
	if want := "@Table class Modified"; got[0].Signature != want {
		t.Errorf("Modified.Signature = %q, want %q", got[0].Signature, want)
	}
}

// The SIBLING site, buildMethodSignature, already ran stripAnnotationArgs
// first - and nothing observed it. Swapping ITS order back on the fixed tree
// left the whole java package green (0 "--- FAIL" lines, go vet 0), so the
// order its own comment justifies was ungraded. #7124's pair-audit discipline
// says the pair is the deliverable, so the method site gets its own fixture
// here. This is test-only: buildMethodSignature is NOT changed by this commit
// (its restructuring is #7124 item 2).
const java7124RepoSrc = `package com.example.sig;

@interface DeleteMapping {
    String value();
}

class Repo {
    @DeleteMapping("/items/{id}")
    public void remove(String id) {
    }

    @DeleteMapping("/items/all")
    public void removeAll(String id) {
    }
}
`

func TestJava7124_MethodSignatureStripsAnnotationArgsBeforeBraceCut(t *testing.T) {
	recs := extractJava7073(t, "com/example/sig/Repo.java", java7124RepoSrc)
	for _, want := range []struct{ name, sig string }{
		// The brace lives inside the annotation's string literal. With the
		// order swapped this emits "@DeleteMapping" and loses the method.
		{"Repo.remove", "@DeleteMapping void remove(String id)"},
		// The control: same annotation, same signature, no brace in the
		// literal. Green in both orders.
		{"Repo.removeAll", "@DeleteMapping void removeAll(String id)"},
	} {
		got := findJava7073(recs, "SCOPE.Operation", want.name)
		if len(got) != 1 {
			t.Errorf("want exactly 1 SCOPE.Operation named %q, got %d. All entities:\n  %s",
				want.name, len(got), describeJava7073(recs))
			continue
		}
		if got[0].Signature != want.sig {
			t.Errorf("%s.Signature = %q, want %q", want.name, got[0].Signature, want.sig)
		}
	}
}
