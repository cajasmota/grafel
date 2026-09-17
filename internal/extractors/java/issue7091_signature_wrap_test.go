// issue7091_signature_wrap_test.go — a WRAPPED declaration is the only shape
// that can observe `raw = strings.Join(strings.Fields(raw), " ")` (issue #7091).
//
// That expression appears at FOUR sites in java.go. Collapsing whitespace on a
// span that already fits on one line is a NO-OP, and every signature assertion
// the package had was written against a single-line declaration — so three of
// the four sites were ungraded, measured by deleting each call individually on
// 614a43558 (`go vet` 0 each time):
//
//	buildAnnotationElementSignature  1 --- FAIL   graded by #7089
//	walk, record-component signature 0 --- FAIL   ungraded
//	buildMethodSignature             0 --- FAIL   ungraded
//	buildClassSignature              0 --- FAIL   ungraded
//
// This is ONE missing fixture SHAPE, not three oversights: a declaration that
// wraps. `Signature` is persisted and is what a reader sees in grafel_inspect
// and grafel_get_source, so an uncollapsed one is a visibly broken string
// carrying embedded newlines and source indentation.
//
// # Grading
//
// Each site gets its OWN test with its OWN source, because three additions
// that only fail together would grade none of them. Cross-checked by deleting
// each collapse in turn: each deletion reddens exactly one of the three tests
// below. That is why, e.g., the record fixture never asserts the RECORD's own
// signature (that string is built by buildClassSignature) and the class and
// method fixtures contain no records.
//
// # Both directions
//
// Every test asserts the EXACT emitted string, and every fixture pairs the
// wrapped declaration with a SINGLE-LINE sibling of the same construct. The
// wrapped row dies when the collapse is DELETED; the single-line row dies when
// the collapse is made too AGGRESSIVE (joining on "" instead of " "), which a
// "some whitespace changed" assertion would survive.
//
// Axis varied: the declaration's LINE EXTENT (wrapped vs single-line). Held
// constant within each fixture: the file, the package, the construct kind, the
// modifiers, and the enclosing scope.
//
// Every fixture below was compiled before being asserted on: javac 25.0.3,
// each source written out under the file name its declaration requires
// (OrderServiceImpl.java / Repo.java / Money.java), zero errors. A wrapped
// declaration that does not compile is a fixture claiming a shape Java cannot
// express.

package java_test

import "testing"

// java7091ClassSrc — buildClassSignature. A class whose `extends` and
// `implements` clauses sit on their own lines is ordinary Java, and is exactly
// the kind of declaration a reader inspects first.
const java7091ClassSrc = `package com.example.wrap;

public class OrderServiceImpl
        extends AbstractService
        implements OrderService, Auditable {
}

class Plain extends AbstractService {
}

class AbstractService {}
interface OrderService {}
interface Auditable {}
`

func TestJava7091_WrappedClassSignatureIsCollapsed(t *testing.T) {
	recs := extractJava7073(t, "com/example/wrap/OrderServiceImpl.java", java7091ClassSrc)
	for _, want := range []struct{ name, sig string }{
		// The wrapped row: its raw span is
		// "class OrderServiceImpl\n        extends AbstractService\n        implements ...".
		// Deleting the collapse leaves the newlines and the indentation in the
		// persisted string.
		{"OrderServiceImpl", "class OrderServiceImpl extends AbstractService implements OrderService, Auditable"},
		// The single-line control: unreachable by the deletion (a no-op there)
		// and reddened by an over-aggressive join.
		{"Plain", "class Plain extends AbstractService"},
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

// java7091MethodSrc — buildMethodSignature. A long parameter list broken over
// lines with the `throws` clause on its own line.
const java7091MethodSrc = `package com.example.wrap;

class Repo {
    public java.util.List<Order> findAll(
            String tenant,
            int limit)
            throws java.io.IOException {
        return null;
    }

    public void ping(String tenant) {
    }
}

class Order {}
`

func TestJava7091_WrappedMethodSignatureIsCollapsed(t *testing.T) {
	recs := extractJava7073(t, "com/example/wrap/Repo.java", java7091MethodSrc)
	for _, want := range []struct{ name, sig string }{
		// The interior single space after "findAll(" is the emitted artefact,
		// not an aspiration: the builder collapses runs of whitespace, it does
		// not reflow punctuation. Asserting what it actually produces is the
		// point — a tidied-up expectation would fail on the unmutated code.
		{"Repo.findAll", "java.util.List<Order> findAll( String tenant, int limit) throws java.io.IOException"},
		{"Repo.ping", "void ping(String tenant)"},
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

// java7091RecordSrc — the record-component signature built inline in walk.
// `amount` wraps its declared type onto a second line; `currency` does not.
//
// The RECORD's own signature is deliberately NOT asserted here: that string is
// built by buildClassSignature, and asserting it would make this test red when
// the CLASS site is mutated, grading two sites with one fixture and therefore
// grading neither.
const java7091RecordSrc = `package com.example.wrap;

record Money(
        java.math.BigDecimal
                amount,
        String currency) {
}
`

func TestJava7091_WrappedRecordComponentSignatureIsCollapsed(t *testing.T) {
	recs := extractJava7073(t, "com/example/wrap/Money.java", java7091RecordSrc)
	for _, want := range []struct {
		name       string
		start, end int
		sig        string
	}{
		// Span asserted alongside the signature so a fixture that LOOKS wrapped
		// but whose component span is single-line — the failure mode this whole
		// issue is about — cannot masquerade as a multi-line case.
		{"Money.amount", 4, 5, "java.math.BigDecimal amount"},
		{"Money.currency", 6, 6, "String currency"},
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
