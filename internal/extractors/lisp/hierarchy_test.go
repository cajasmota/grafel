package lisp

import (
	"strconv"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// extendsEdges runs the real commonlisp extraction path and returns every
// EXTENDS edge as "Owner->Target" pairs, plus a lookup of the owning record.
//
// It deliberately drives extractLisp — the CALL SITE — rather than
// superclassEdges directly, so a mutant that breaks the wiring (wrong offset,
// wrong owner, edge attached to the wrong record, edge emitted for defstruct)
// is graded here and not only by the golden fixture. Tests that need the
// helper's internals in isolation say so in their own names.
func extendsEdges(t *testing.T, src string) []string {
	t.Helper()
	ents := extractLisp(src, "a.lisp", "commonlisp")
	var out []string
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "EXTENDS" {
				out = append(out, e.Name+"->"+r.ToID)
			}
		}
	}
	return out
}

func edgesFor(t *testing.T, src, owner string) []types.RelationshipRecord {
	t.Helper()
	ents := extractLisp(src, "a.lisp", "commonlisp")
	for _, e := range ents {
		if e.Name == owner && e.Kind == "SCOPE.Component" {
			return e.Relationships
		}
	}
	return nil
}

func wantEdges(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("EXTENDS = %v, want %v", got, want)
	}
}

// TestLispHierarchy_SingleSuperclassOnOneLine varies nothing but the simplest
// shape: one superclass, whole form on one line. It holds line layout, member
// count and dialect constant, and is the positive control every negative
// control below is measured against.
func TestLispHierarchy_SingleSuperclassOnOneLine(t *testing.T) {
	wantEdges(t, extendsEdges(t, "(defclass dog (animal) ())\n"), "dog->animal")
}

// TestLispHierarchy_MultipleSuperclassesAllEmitted varies ONLY the NUMBER of
// names inside the superclass list (three), holding the single-line layout and
// the empty slot list constant. It is the only test that fails for a producer
// that takes just the first name of the list.
func TestLispHierarchy_MultipleSuperclassesAllEmitted(t *testing.T) {
	wantEdges(t, extendsEdges(t, "(defclass hybrid (animal machine named) ())\n"),
		"hybrid->animal", "hybrid->machine", "hybrid->named")
}

// TestLispHierarchy_EmptySuperclassListEmitsNothing is the required negative
// control for the mandatory-but-empty list. It varies ONLY the list contents
// (empty) against the positive control above; the class entity itself must
// still exist, which the second assertion holds.
func TestLispHierarchy_EmptySuperclassListEmitsNothing(t *testing.T) {
	src := "(defclass root () ((name :initarg :name)))\n"
	if got := extendsEdges(t, src); got != nil {
		t.Fatalf("EXTENDS = %v, want none for an empty superclass list", got)
	}
	if edgesFor(t, src, "root") == nil && len(extractLisp(src, "a.lisp", "commonlisp")) < 2 {
		t.Fatalf("the class entity itself disappeared; the negative control is measuring the wrong thing")
	}
}

// TestLispHierarchy_MultiLineFormWithSlotsFollowing is the shape the whole
// design turns on: the superclass list on its OWN line and a slot list after
// it. It varies ONLY the line layout against the single-line positive control,
// and it is the row that fails for a producer whose whitespace skip cannot
// cross a newline.
func TestLispHierarchy_MultiLineFormWithSlotsFollowing(t *testing.T) {
	src := "(defclass poodle\n    (dog pet)\n  ((size :initarg :size)\n   (age :initform 0)))\n"
	wantEdges(t, extendsEdges(t, src), "poodle->dog", "poodle->pet")
}

// TestLispHierarchy_SlotListIsNeverReadAsSuperclasses is the required negative
// control for the slot/superclass confusion. The source omits the superclass
// list entirely (malformed CLOS, but the exact input that a "grab the next
// parenthesised group" producer mis-reads), so the FIRST parenthesised group
// after the name is a slot list. It varies ONLY the presence of the superclass
// list; the nesting depth inside the group is what the guard reads.
func TestLispHierarchy_SlotListIsNeverReadAsSuperclasses(t *testing.T) {
	src := "(defclass lonely ((name :initarg :name) (age :initform 0)))\n"
	if got := extendsEdges(t, src); got != nil {
		t.Fatalf("EXTENDS = %v, want none — the first group is a SLOT list, not a superclass list", got)
	}
}

// TestLispHierarchy_DefstructAndDefgenericDoNotFire is the required negative
// control for neighbouring def-forms. It varies ONLY the defining form,
// holding the "(name (parenthesised-list) …)" shape constant so that a
// producer keyed on shape rather than on the defclass call site fails here.
// A defclass in the same file is the positive control that the file is being
// read at all.
func TestLispHierarchy_DefstructAndDefgenericDoNotFire(t *testing.T) {
	src := "(defstruct point (x y))\n(defgeneric speak (thing other))\n(defclass real (base) ())\n"
	wantEdges(t, extendsEdges(t, src), "real->base")
}

// TestLispHierarchy_CommentedDeclarationDoesNotFire is the required
// comment negative control. It varies ONLY whether a `;` precedes the form.
// Note what this grades and what it does not: the exclusion is
// stripLispStringsAndComments blanking the line, which is PRE-EXISTING and
// also removes the entity — so this test would pass even with no hierarchy
// code at all. It is kept as a guard against a future producer that reads raw
// source, and it says so rather than claiming to grade this change.
func TestLispHierarchy_CommentedDeclarationDoesNotFire(t *testing.T) {
	src := "; (defclass ghost (phantom) ())\n(defclass real (base) ())\n"
	wantEdges(t, extendsEdges(t, src), "real->base")
	for _, e := range extractLisp(src, "a.lisp", "commonlisp") {
		if e.Name == "ghost" {
			t.Fatalf("commented-out defclass produced an entity")
		}
	}
}

// TestLispHierarchy_DeclarationInsideStringDoesNotFire is the required string
// negative control, and carries the same caveat as the comment one: the
// scrubber is what excludes it, and it removes the entity too.
func TestLispHierarchy_DeclarationInsideStringDoesNotFire(t *testing.T) {
	src := "(defparameter *doc*\n\"(defclass spectre (phantom) ())\")\n(defclass real (base) ())\n"
	wantEdges(t, extendsEdges(t, src), "real->base")
	for _, e := range extractLisp(src, "a.lisp", "commonlisp") {
		if e.Name == "spectre" {
			t.Fatalf("defclass inside a string literal produced an entity")
		}
	}
}

// TestLispHierarchy_CommentBetweenNameAndSuperclassList holds the superclass
// list constant and varies ONLY what sits between the name and the list: a
// trailing `;` comment. It is a POSITIVE control for the interaction between
// the scrubber (which blanks the comment to spaces) and the anchor (which
// skips whitespace) — the two are independent and this is the only row where
// their composition is observed.
func TestLispHierarchy_CommentBetweenNameAndSuperclassList(t *testing.T) {
	src := "(defclass dog ; the superclass list follows\n    (animal) ())\n"
	wantEdges(t, extendsEdges(t, src), "dog->animal")
}

// TestLispHierarchy_SelfNameAndDuplicatesCollapse varies ONLY the CONTENTS of
// one superclass list — a self-reference and a repeat — holding owner, layout
// and list position constant.
func TestLispHierarchy_SelfNameAndDuplicatesCollapse(t *testing.T) {
	wantEdges(t, extendsEdges(t, "(defclass node (node tree tree) ())\n"), "node->tree")
}

// TestLispHierarchy_PackageQualifierErasedIntoBaseProperty asserts BOTH halves
// of the qualifier rule: ToID is the bare name, and the written form survives
// in the `base` property. Reading `base` back through Props.Get is deliberate —
// Props is key-sorted and Get binary-searches (#6802), so a literal in the
// wrong key order reads a PRESENT key as absent and only a Get catches it.
func TestLispHierarchy_PackageQualifierErasedIntoBaseProperty(t *testing.T) {
	src := "(defclass window (sb-mop:standard-object) ())\n"
	wantEdges(t, extendsEdges(t, src), "window->standard-object")
	rels := edgesFor(t, src, "window")
	if len(rels) != 1 {
		t.Fatalf("relationships = %d, want 1", len(rels))
	}
	if got := rels[0].Properties.Get("base"); got != "sb-mop:standard-object" {
		t.Fatalf("base = %q, want the written form", got)
	}
	if got := rels[0].Properties.Get("provenance"); got != "lisp_defclass_superclass_list" {
		t.Fatalf("provenance = %q", got)
	}
	if got := rels[0].Properties.Get("line"); got != "1" {
		t.Fatalf("line = %q, want 1", got)
	}
}

// TestLispHierarchy_UnqualifiedTargetHasNoBaseProperty is the counterpart to
// the row above: it varies ONLY the presence of a qualifier and asserts the
// property is ABSENT, so a producer that always writes `base` fails one of the
// two.
func TestLispHierarchy_UnqualifiedTargetHasNoBaseProperty(t *testing.T) {
	rels := edgesFor(t, "(defclass dog (animal) ())\n", "dog")
	if len(rels) != 1 {
		t.Fatalf("relationships = %d, want 1", len(rels))
	}
	if got, ok := rels[0].Properties.Lookup("base"); ok {
		t.Fatalf("base = %q, want absent for an unqualified superclass", got)
	}
}

// TestLispHierarchy_EdgeIsAnchoredOnTheTypeNotTheFile pins the #6295/#6298
// contract: FromID stays EMPTY so assembly stamps the owning record's id. A
// non-empty non-hex FromID would be rewritten onto the FILE entity, merging
// every class in a multi-class file onto one node.
func TestLispHierarchy_EdgeIsAnchoredOnTheTypeNotTheFile(t *testing.T) {
	for _, r := range edgesFor(t, "(defclass dog (animal) ())\n", "dog") {
		if r.FromID != "" {
			t.Fatalf("FromID = %q, want empty", r.FromID)
		}
	}
}

// TestLispHierarchy_TwoClassesInOneFileKeepSeparateEdges varies ONLY the
// number of declarations in the file, holding each declaration's shape
// constant. It is what fails for a producer that accumulates edges onto one
// record, and for one whose anchor drifts from one form into the next.
func TestLispHierarchy_TwoClassesInOneFileKeepSeparateEdges(t *testing.T) {
	src := "(defclass dog (animal) ())\n(defclass cat (animal) ())\n"
	wantEdges(t, extendsEdges(t, src), "dog->animal", "cat->animal")
}

// TestLispHierarchy_NoDuplicateComponentPerClass guards the reason these edges
// are emitted HERE rather than by registering lisp in cross/hierarchy: that
// pass mints its own SCOPE.Component per type and per named parent. Exactly
// one component named `dog` must exist, and NONE named `animal` — the target
// is a name reference, not a synthesised node.
func TestLispHierarchy_NoDuplicateComponentPerClass(t *testing.T) {
	n, parents := 0, 0
	for _, e := range extractLisp("(defclass dog (animal) ())\n", "a.lisp", "commonlisp") {
		if e.Kind != "SCOPE.Component" {
			continue
		}
		switch e.Name {
		case "dog":
			n++
		case "animal":
			parents++
		}
	}
	if n != 1 || parents != 0 {
		t.Fatalf("components: dog=%d animal=%d, want 1 and 0", n, parents)
	}
}

// TestLispHierarchy_SchemeAndRacketEmitNothing_DialectGate pins the deliberate
// dialect decision. `defclass` is Common Lisp; the same TEXT parsed as scheme
// or racket must produce no EXTENDS, because the defclass loop is inside the
// commonlisp arm. It varies ONLY the dialect, holding the source constant.
func TestLispHierarchy_SchemeAndRacketEmitNothing_DialectGate(t *testing.T) {
	src := "(defclass dog (animal) ())\n"
	for _, d := range []string{"scheme", "racket"} {
		for _, e := range extractLisp(src, "a."+d, d) {
			for _, r := range e.Relationships {
				if r.Kind == "EXTENDS" {
					t.Fatalf("dialect %s emitted %s->%s", d, e.Name, r.ToID)
				}
			}
		}
	}
}

// TestLispHierarchy_SchemeDefineClassEmitsNothing_KnownDivergence pins a real
// gap rather than merely reporting it: GOOPS/Guile `(define-class <foo>
// (<bar>) …)` has the SAME shape as CLOS defclass and is extracted as a class
// entity today, but gets no EXTENDS. Whoever extends this to Scheme gets a
// failing test naming the file to change. Mutating TOWARD the fix — calling
// superclassEdges from the define-class loop — turns this red, which is what
// makes it a tripwire rather than a comment.
func TestLispHierarchy_SchemeDefineClassEmitsNothing_KnownDivergence(t *testing.T) {
	src := "(define-class <window> (<widget>)\n  (title))\n"
	found := false
	for _, e := range extractLisp(src, "a.scm", "scheme") {
		if e.Name == "<window>" && e.Kind == "SCOPE.Component" {
			found = true
			for _, r := range e.Relationships {
				if r.Kind == "EXTENDS" {
					t.Fatalf("define-class now emits %s — good; update this known-divergence pin", r.ToID)
				}
			}
		}
	}
	if !found {
		t.Fatalf("positive control failed: define-class no longer yields an entity, so the divergence pin is measuring nothing")
	}
}

// TestLispHierarchy_DefstructIncludeEmitsNothing_KnownDivergence pins the
// other in-language inheritance construct this change deliberately leaves out:
// CL `(defstruct (point3d (:include point)) …)`, whose `:include` option names
// a base structure. The pin records the full truth measured rather than the
// half that was assumed: this form yields NO entity either, because defstructRE
// requires a bare symbol after the keyword and the options form starts with
// `(`. So `:include` inheritance is invisible at BOTH levels, and whoever adds
// it must widen defstructRE first. A plain `(defstruct point x y)` is the
// positive control that the file is being read at all.
func TestLispHierarchy_DefstructIncludeEmitsNothing_KnownDivergence(t *testing.T) {
	src := "(defstruct (point3d (:include point)) z)\n(defstruct point x y)\n"
	names := map[string]bool{}
	for _, e := range extractLisp(src, "a.lisp", "commonlisp") {
		names[e.Name] = true
		for _, r := range e.Relationships {
			if r.Kind == "EXTENDS" {
				t.Fatalf(":include now emits %s->%s — good; update this pin", e.Name, r.ToID)
			}
		}
	}
	if !names["point"] {
		t.Fatalf("positive control failed: plain defstruct yields no entity, so this pin measures nothing")
	}
	if names["point3d"] {
		t.Fatalf("the options-form defstruct now yields an entity — good; update this pin, then decide about :include")
	}
}

// TestLispHierarchy_UnterminatedSuperclassListEmitsNothing varies ONLY the
// closing paren. An unterminated group must not run to end of file and
// swallow every later form's names.
func TestLispHierarchy_UnterminatedSuperclassListEmitsNothing(t *testing.T) {
	if got := extendsEdges(t, "(defclass broken (animal\n"); got != nil {
		t.Fatalf("EXTENDS = %v, want none", got)
	}
}

// TestLispHierarchy_NonListAfterNameEmitsNothing varies ONLY the character
// that follows the name: a symbol rather than `(`. This is what stops the
// anchor from scanning forward to some LATER form's parenthesised group.
func TestLispHierarchy_NonListAfterNameEmitsNothing(t *testing.T) {
	if got := extendsEdges(t, "(defclass odd)\n\n(defclass dog (animal) ())\n"); len(got) != 1 || got[0] != "dog->animal" {
		t.Fatalf("EXTENDS = %v, want only dog->animal", got)
	}
}

// TestLispHierarchy_CRLFSourceStillEmits varies ONLY the line terminator
// against TestLispHierarchy_MultiLineFormWithSlotsFollowing.
func TestLispHierarchy_CRLFSourceStillEmits(t *testing.T) {
	src := "(defclass poodle\r\n    (dog pet)\r\n  ((size :initarg :size)))\r\n"
	wantEdges(t, extendsEdges(t, src), "poodle->dog", "poodle->pet")
}

// TestLispHierarchy_LineNumberFollowsTheEntitysOwnStartLine asserts the
// property this change controls: the edge's `line` is the OWNER ENTITY's
// StartLine, whatever that is — not 1, and not the superclass list's line. The
// two-declaration source varies the vertical position while holding the form
// shape constant, so a producer that hard-codes a line fails.
func TestLispHierarchy_LineNumberFollowsTheEntitysOwnStartLine(t *testing.T) {
	src := "(defclass dog (animal) ())\n(defclass cat\n   (animal) ())\n"
	for _, e := range extractLisp(src, "a.lisp", "commonlisp") {
		for _, r := range e.Relationships {
			want := strconv.Itoa(e.StartLine)
			if got := r.Properties.Get("line"); got != want {
				t.Fatalf("%s: line = %q, want the entity's own StartLine %q", e.Name, got, want)
			}
		}
		if e.Name == "cat" && e.StartLine != 2 {
			t.Fatalf("positive control failed: cat StartLine = %d, want 2 — the line assertion above is vacuous if every entity is on line 1", e.StartLine)
		}
	}
}

// TestLispHierarchy_BlankLineBeforeDeclarationShiftsTheLine_KnownDivergence
// pins a PRE-EXISTING defect this change inherits rather than causes.
// defclassRE is `(?m)^\s*\(defclass`, and `\s*` crosses newlines, so a
// defclass preceded by a blank line matches from the START of that blank run:
// both the entity's StartLine and the edge's `line` report the blank line, not
// the declaration. Fixing it means changing defclassRE (and its six siblings),
// which is not this change's scope. Mutating TOWARD the fix — `^[ \t]*` —
// turns this red. The first assertion is the positive control that the edge
// exists at all, so the pin cannot decay into a test of nothing.
func TestLispHierarchy_BlankLineBeforeDeclarationShiftsTheLine_KnownDivergence(t *testing.T) {
	src := "(defun f () 1)\n\n(defclass dog (animal) ())\n"
	rels := edgesFor(t, src, "dog")
	if len(rels) != 1 {
		t.Fatalf("relationships = %d, want 1", len(rels))
	}
	if got := rels[0].Properties.Get("line"); got != "2" {
		t.Fatalf("line = %q, want 2 — today's WRONG answer; the declaration is on line 3. If this now says 3, defclassRE was fixed: delete this pin", got)
	}
}

// TestLispHierarchy_NestedFormInsideTheGroupEmitsNothing grades the nesting
// guard on the ONLY input where it is the thing that decides. It varies the
// group's CONTENTS — one plain symbol plus one sub-list — holding position and
// layout constant.
//
// It exists because the guard turned out to be ungraded by every other test
// here, and that was found by SCORING, not by reading: removing the guard left
// the whole suite and the golden gate green. The reason is that
// lispSuperclassNameRE independently rejects a token containing a paren, so on
// an ordinary slot list (`((name :initarg :name))`) the two guards mask each
// other. Only a group that mixes a valid symbol with a sub-list separates
// them: without the guard this emits `weird EXTENDS base`, with it, nothing.
func TestLispHierarchy_NestedFormInsideTheGroupEmitsNothing(t *testing.T) {
	if got := extendsEdges(t, "(defclass weird (base (extra)) ())\n"); got != nil {
		t.Fatalf("EXTENDS = %v, want none — a group containing a sub-list is not a superclass list", got)
	}
}

// TestLispHierarchy_AnchorDoesNotSearchForwardIntoALaterForm grades the ANCHOR
// itself — that superclassEdges reads the group AT the name and never searches
// forward for one. It varies ONLY what follows a superclass-list-less defclass:
// a flat top-level form.
//
// Like the test above, this was added after scoring showed the anchor was
// ungraded: replacing it with "find the next `(` anywhere ahead" left every
// other test and the golden gate green, because in every other input the next
// `(` ahead HAPPENS to be the right one. Here it is not, and the searching
// producer emits `detached EXTENDS list`, `-> a`, `-> b`.
func TestLispHierarchy_AnchorDoesNotSearchForwardIntoALaterForm(t *testing.T) {
	src := "(defclass detached)\n\n(list a b)\n\n(defclass dog (animal) ())\n"
	wantEdges(t, extendsEdges(t, src), "dog->animal")
}

// TestLispHierarchy_EmptyListFollowedByFlatSlotsEmitsNothing varies ONE axis
// against TestLispHierarchy_EmptySuperclassListEmitsNothing: the slot list is
// FLAT (`(name legs)`, slots with no options) rather than a list of lists.
// That difference is the whole point — with a nested slot list the token
// `(name` is rejected by lispSuperclassNameRE anyway, so a producer that
// skipped the empty `()` and read the next group would still emit nothing and
// the defect would hide. With flat slots it emits `root EXTENDS name` and
// `-> legs`.
//
// This was added after mutant scoring, not before: the skip-the-empty-group
// mutant was ALIVE against the whole unit suite and only the golden fixture
// caught it. Both graders now do.
func TestLispHierarchy_EmptyListFollowedByFlatSlotsEmitsNothing(t *testing.T) {
	if got := extendsEdges(t, "(defclass root ()\n  (name legs))\n"); got != nil {
		t.Fatalf("EXTENDS = %v, want none — `()` is a root class, and the flat group after it is slots", got)
	}
}

// TestLispHierarchy_QuoteReaderMacroIsNotHalfMatched grades the LEADING `^` of
// lispSuperclassNameRE on the one input that separates it from the trailing
// `$`. It varies ONLY the leading character of the single token in the group.
//
// It exists because scoring showed the two anchors are a MASKING PAIR of their
// own, exactly like the nesting guard and the regex one level further in:
// dropping `^` alone left the whole unit suite and the golden gate green,
// because every other token in every other input either matches whole or is
// rejected by the trailing anchor as well. Here only the leading anchor
// decides — with `^` gone the regex simply starts matching at `q` and emits
// `x EXTENDS quoted`, inventing a superclass out of a reader macro's operand.
func TestLispHierarchy_QuoteReaderMacroIsNotHalfMatched(t *testing.T) {
	if got := extendsEdges(t, "(defclass x ('quoted) ())\n"); got != nil {
		t.Fatalf("EXTENDS = %v, want none — `'quoted` is not a plain symbol", got)
	}
}

// TestLispHierarchy_TokenWithAnInteriorSeparatorIsRejectedWhole grades BOTH
// anchors of lispSuperclassNameRE, and — the point of the row — grades each
// one SEPARATELY: `foo,bar` half-matches in a DIFFERENT direction for each
// deletion, so neither anchor can hide behind the other here. With `^` dropped
// the regex matches the tail and emits `x EXTENDS bar`; with `$` dropped it
// matches the head and emits `x EXTENDS foo`; at HEAD it matches neither and
// emits nothing.
//
// The `,` is doing nothing special: it stands for any byte that is in no part
// of the symbol character class, which is the whole family of tokens the
// package header claims are rejected whole rather than half-matched.
func TestLispHierarchy_TokenWithAnInteriorSeparatorIsRejectedWhole(t *testing.T) {
	if got := extendsEdges(t, "(defclass x (foo,bar) ())\n"); got != nil {
		t.Fatalf("EXTENDS = %v, want none — `foo,bar` is one token and not a symbol", got)
	}
}

// TestLispHierarchy_WhitespaceWithinTheGroupIsAnySequence varies ONE axis no
// other row here varies: the whitespace INSIDE the superclass list. Every
// other test (TestLispHierarchy_MultiLineFormWithSlotsFollowing included)
// keeps the list itself on one line separated by single spaces, and varies
// layout only BETWEEN the name and the list — so the token splitter was
// entirely ungraded on tabs and on a line-broken list.
//
// It is the row that fails for a producer splitting on the single byte " ":
// with a tab it emits nothing for `alpha\tbeta`, and with the line break it
// keeps `gamma` only.
func TestLispHierarchy_WhitespaceWithinTheGroupIsAnySequence(t *testing.T) {
	src := "(defclass tabbed (alpha\tbeta\n    gamma) ())\n"
	wantEdges(t, extendsEdges(t, src), "tabbed->alpha", "tabbed->beta", "tabbed->gamma")
}

// TestLispHierarchy_OmittedSuperclassListWithFlatSlotsEmitsWrongEdges_KnownDivergence
// pins the ONE shape where the anchor's positional rule gives the wrong
// answer, so the header does not have to overclaim.
//
// `(defclass flat (name legs))` omits the mandatory superclass list and writes
// FLAT slots. The first group after the name is therefore a slot list, but it
// is textually indistinguishable from a two-parent superclass list: both are a
// flat group of plain symbols in the position CLOS reserves for parents.
// Neither guard can decide it and neither should guess, so today this emits
// `flat EXTENDS name` and `flat EXTENDS legs`.
//
// The behaviour is LEFT AS IS deliberately — the alternative is a heuristic on
// malformed input that would cost real edges on well-formed input. This pin
// makes the cost visible and turns red if anyone changes it. The second
// assertion is the positive control: a well-formed neighbour must still emit,
// so the pin cannot decay into a test of nothing.
//
// Note that TestLispHierarchy_SlotListIsNeverReadAsSuperclasses does NOT
// contradict this: its slot list is nested, which the nesting guard catches.
func TestLispHierarchy_OmittedSuperclassListWithFlatSlotsEmitsWrongEdges_KnownDivergence(t *testing.T) {
	src := "(defclass flat (name legs))\n(defclass dog (animal) ())\n"
	wantEdges(t, extendsEdges(t, src),
		"flat->name", "flat->legs", "dog->animal")
}

// TestLispHierarchy_KeywordInTheSuperclassListIsDropped grades the clause the
// package header has always asserted and nothing observed: that a KEYWORD-only
// token is rejected by lispSuperclassNameRE on its own.
//
// The token must sit DIRECTLY in the superclass list. `:initarg` appears six
// other times in this file and every one of them is inside a NESTED slot list,
// where lispBalancedGroup rejects the whole group before the leaf regex is
// consulted — so the nesting guard masked this clause exactly as it previously
// masked the anchor (see TestLispHierarchy_NestedFormInsideTheGroupEmitsNothing).
// Adding `:` to the LEADING character class of the bare-symbol group, leaving
// both anchors intact, was ALIVE against the whole unit suite and the golden
// gate before this row existed, and emits `x EXTENDS :initarg` — an edge to a
// keyword, which names no class.
func TestLispHierarchy_KeywordInTheSuperclassListIsDropped(t *testing.T) {
	if got := extendsEdges(t, "(defclass x (:initarg) ())\n"); got != nil {
		t.Fatalf("EXTENDS = %v, want none — a keyword is not a class name", got)
	}
}

// TestLispHierarchy_KeywordBesideARealNameDropsOnlyTheKeyword varies ONE axis
// against the row above: a valid symbol shares the list with the keyword. It
// asserts the rejection is PER TOKEN rather than per group — the good name
// still yields its edge — so a producer that discards the whole list on one bad
// token fails here while passing the pure-keyword row.
func TestLispHierarchy_KeywordBesideARealNameDropsOnlyTheKeyword(t *testing.T) {
	wantEdges(t, extendsEdges(t, "(defclass x (animal :initarg) ())\n"), "x->animal")
}

// TestLispHierarchy_DoubleColonQualifierIsErasedToo grades the `?` in the
// `::?` alternation, which the regex's own doc names (`sb-mop:` or `sb-mop::`)
// and which TestLispHierarchy_PackageQualifierErasedIntoBaseProperty does not
// reach: that row uses a SINGLE colon, so narrowing `::?` to `:` leaves it
// green. It varies ONLY the colon count.
//
// A CL internal-symbol reference `pkg::sym` is the documented second half of
// the qualifier claim; with `::?` narrowed to `:` the token matches nothing at
// all and the edge is LOST, not mis-targeted. Both halves are asserted here —
// ToID erased to the bare symbol, `base` carrying the written form — for the
// same #6802 reason the single-colon row gives.
func TestLispHierarchy_DoubleColonQualifierIsErasedToo(t *testing.T) {
	src := "(defclass x (sb-mop::internal-thing) ())\n"
	wantEdges(t, extendsEdges(t, src), "x->internal-thing")
	rels := edgesFor(t, src, "x")
	if len(rels) != 1 {
		t.Fatalf("relationships = %d, want 1", len(rels))
	}
	if got := rels[0].Properties.Get("base"); got != "sb-mop::internal-thing" {
		t.Fatalf("base = %q, want the written form", got)
	}
}

// TestLispHierarchy_InteriorApostropheIsKeptInTheSymbol_KnownDivergence grades
// the one remaining ungraded clause of lispSuperclassNameRE: `'` is absent from
// the leading character class (which is what
// TestLispHierarchy_QuoteReaderMacroIsNotHalfMatched grades) but PRESENT in the
// trailing one, so `foo'bar` is accepted as a single symbol.
//
// This pins today's answer while recording that it is probably the WRONG one:
// in Common Lisp `'` is a terminating macro character, so the reader sees `foo`
// followed by `(quote bar)`, and the honest edge would be `x EXTENDS foo`.
// Changing it is a behaviour change outside this issue's scope and would need
// its own scoring, so the clause is graded rather than quietly widened or
// quietly removed. Whoever narrows the trailing class gets a failure here
// naming the decision.
func TestLispHierarchy_InteriorApostropheIsKeptInTheSymbol_KnownDivergence(t *testing.T) {
	wantEdges(t, extendsEdges(t, "(defclass x (foo'bar) ())\n"), "x->foo'bar")
}
