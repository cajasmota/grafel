// Issue #7118 — the intra-line whitespace-run axis of the signature collapse.
//
// # What this file grades, and why the existing fixtures could not
//
// The collapse work (#7091, #7112, #7114, #7115) was about WRAPPING, so every
// fixture in that family spans lines. A wrapped declaration carries both a
// NEWLINE and the indentation run beside it, so `strings.Join(strings.Fields
// (raw), " ")` is satisfied by the newline half alone: guard the whole call on
// `strings.ContainsRune(raw, '\n')` and nothing fails. Measured on
// 95a4f9769 — that guard (DM-1) was ALIVE with 0 `--- FAIL` lines at the
// record-component site, buildMethodSignature, buildClassSignature,
// buildAnnotationElementSignature, and at javaDeclaratorDimensions' call of
// collapseJavaSpaces. Five holes, one per site.
//
// Every declaration below is therefore SINGLE-LINE, and its span is asserted
// to be single-line, because that is the whole point: a single-line span can
// only be changed by the intra-line half of the collapse. If a later edit
// wraps one of these, the span assertion fails rather than the row quietly
// reverting to grading newlines.
//
// # Axes
//
// VARIED: the whitespace-run SPELLING — doubled/tripled spaces vs TABs
// (`strings.Fields` splits on any whitespace, so the tab spelling is a
// distinct input to the same call and was equally unobserved); and the SITE —
// one declaration per signature builder, since a verdict at one site says
// nothing about its four twins.
//
// HELD CONSTANT: the line count (single-line, asserted), the file, the
// package, and the absence of any newline inside a graded span. The existing
// wrapped fixtures are untouched — this is an ADDITIONAL axis, not a
// replacement, and deleting them would leave the original newline defect
// ungraded.
//
// NOT graded here: `collapseJavaSpaces`' type-node caller in
// buildFieldSignature. That one already dies — TestJava7118_IntraLine
// WhitespaceRunsAreCollapsed, via the `spaced` field — and it is the ONLY
// caller that kill covers: mutating the helper itself is killed by that one
// test, while mutating the dimensions caller alone was ALIVE. So the
// helper-level verdict does not certify its callers, and `spacedDims` /
// `tabbedDims` below grade the dimensions caller on its own.
//
// The fixture was compiled before being asserted on: javac 25.0.3, written out
// as com/example/spaces/SpaceHost.java, exit 0, zero diagnostics.

package java_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

const java7118SpacesSrc = `package com.example.spaces;

class  SpaceBase  { }

interface  SpaceIface  { }

class  SpaceHost {

    int   spacedDims[   ];

    int	tabbedDims[	];

    int   twice(int   a,  String   b) { return a; }

    int	tabs(int	a) { return a; }
}

class  SpaceLine   extends   SpaceBase   implements   SpaceIface { }

class	TabLine	extends	SpaceBase { }

record  SpaceRec(@Deprecated  int    x,  String   y) { }

record	TabRec(@Deprecated	int	z) { }

@interface  SpaceAnno { String   scope()   default   "v"; }

@interface	TabAnno { int	n()	default	1; }
`

// java7118One returns the single entity of the given kind and name, and
// asserts its span is single-line — a multi-line span here would mean the
// declaration had been wrapped, which would put a newline back inside the
// collapse span and make the signature assertion grade the newline axis
// instead of this one.
func java7118One(t *testing.T, recs []types.EntityRecord, kind, name string) types.EntityRecord {
	t.Helper()
	got := findJava7073(recs, kind, name)
	if len(got) != 1 {
		t.Fatalf("want exactly 1 %s named %q, got %d. All entities:\n  %s",
			kind, name, len(got), describeJava7073(recs))
	}
	if got[0].StartLine != got[0].EndLine {
		t.Errorf("%s span = %d-%d, want single-line: a wrapped declaration puts a "+
			"newline back inside the collapse span, so this row would no longer "+
			"grade intra-line whitespace (#7118)",
			name, got[0].StartLine, got[0].EndLine)
	}
	return got[0]
}

// TestJava7118_IntraLineWhitespaceRunsCollapseAtEverySite — #7118. One
// single-line declaration per signature builder, in both whitespace spellings.
// Each row dies under DM-1 (the collapse guarded on containing a newline) at
// exactly the site that builds it.
func TestJava7118_IntraLineWhitespaceRunsCollapseAtEverySite(t *testing.T) {
	recs := extractJava7073(t, "com/example/spaces/SpaceHost.java", java7118SpacesSrc)
	for _, want := range []struct{ kind, name, sig string }{
		// walk's record component — the raw component span is replayed
		// verbatim, so its whitespace runs are collapsed there and nowhere
		// else. `@Deprecated  int    x` carries runs on both sides of the type.
		{"SCOPE.Schema", "SpaceRec.x", "@Deprecated int x"},
		{"SCOPE.Schema", "SpaceRec.y", "String y"},
		{"SCOPE.Schema", "TabRec.z", "@Deprecated int z"},

		// buildMethodSignature — collapse runs on the header, which is
		// truncated at the body brace, so the header must be single-line even
		// though the enclosing class is not.
		{"SCOPE.Operation", "SpaceHost.twice", "int twice(int a, String b)"},
		{"SCOPE.Operation", "SpaceHost.tabs", "int tabs(int a)"},

		// buildClassSignature — the whole one-line declaration, extends and
		// implements clauses included.
		{"SCOPE.Component", "SpaceLine", "class SpaceLine extends SpaceBase implements SpaceIface"},
		{"SCOPE.Component", "TabLine", "class TabLine extends SpaceBase"},
		{"SCOPE.Component", "SpaceBase", "class SpaceBase"},
		{"SCOPE.Component", "SpaceIface", "interface SpaceIface"},
		{"SCOPE.Component", "SpaceRec", "record SpaceRec(@Deprecated int x, String y)"},
		{"SCOPE.Component", "TabRec", "record TabRec(@Deprecated int z)"},
		{"SCOPE.Component", "SpaceAnno", "@interface SpaceAnno"},
		{"SCOPE.Component", "TabAnno", "@interface TabAnno"},

		// buildAnnotationElementSignature — the `default` clause is kept, so
		// the runs around it are inside the collapsed span.
		{"SCOPE.Schema", "SpaceAnno.scope", `String scope() default "v"`},
		{"SCOPE.Schema", "TabAnno.n", "int n() default 1"},

		// javaDeclaratorDimensions' own call of collapseJavaSpaces. JLS 10.2
		// permits whitespace between the brackets, and that text is the only
		// thing the dimensions node can hold besides the brackets themselves,
		// so this is the only way to reach that caller with an intra-line run.
		{"SCOPE.Schema", "SpaceHost.spacedDims", "int spacedDims[ ]"},
		{"SCOPE.Schema", "SpaceHost.tabbedDims", "int tabbedDims[ ]"},
	} {
		got := java7118One(t, recs, want.kind, want.name)
		if got.Signature != want.sig {
			t.Errorf("%s.Signature = %q, want %q", want.name, got.Signature, want.sig)
		}
	}
}
