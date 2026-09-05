package clojure_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// hierarchyPairs returns every EXTENDS/IMPLEMENTS edge in the file as
// "Owner-[KIND]->Target", sorted.
//
// It reports the OWNER as well as the target, and it reports EVERY entity's
// edges rather than one named owner's. Both are deliberate: a helper scoped to
// one owner passes while an edge lands on its neighbour, and a helper that
// returns only targets passes with every edge reversed — which is exactly the
// defect extend-protocol invites.
//
// Reading through the registered extractor rather than calling the scanner
// directly makes every assertion below a claim about the EXTRACTOR, so a
// mutant that fixes a helper and skips the call site is not hidden.
func hierarchyPairs(ents []types.EntityRecord) []string {
	var out []string
	for i := range ents {
		e := &ents[i]
		for _, r := range e.Relationships {
			if r.Kind == "IMPLEMENTS" || r.Kind == "EXTENDS" {
				out = append(out, e.Name+"-["+r.Kind+"]->"+r.ToID)
			}
		}
	}
	sort.Strings(out)
	return out
}

func wantPairs(t *testing.T, ents []types.EntityRecord, want ...string) {
	t.Helper()
	got := hierarchyPairs(ents)
	sort.Strings(want)
	if len(want) == 0 {
		want = nil
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("hierarchy edges\n got: %v\nwant: %v", got, want)
	}
}

// TestClojureHierarchy_DefrecordDepth1SymbolsAreSupertypes varies ONE axis —
// which depth-1 tokens the record's tail contains — and holds the declaration
// keyword constant at `defrecord`. It asserts that the two bare symbols become
// IMPLEMENTS targets and that the field vector, which sits between the name and
// the first symbol, contributes nothing. It says nothing about `deftype` (see
// the next test) or about method-body contents (see the depth fence test).
func TestClojureHierarchy_DefrecordDepth1SymbolsAreSupertypes(t *testing.T) {
	src := `(ns app.core)
(defprotocol Shape)
(defrecord Circle [radius center]
  Shape
  (area [_] 3)
  java.lang.Runnable
  (run [_] nil))
`
	ents := extract(t, "src/core.clj", src)
	wantPairs(t, ents,
		"Circle-[IMPLEMENTS]->Shape",
		"Circle-[IMPLEMENTS]->java.lang.Runnable")
}

// TestClojureHierarchy_DeftypeUsesTheSameShape varies ONE axis — the
// declaration keyword, `defrecord` vs `deftype` — over an otherwise identical
// body. It asserts only that the keyword does not decide whether the tail is
// read.
func TestClojureHierarchy_DeftypeUsesTheSameShape(t *testing.T) {
	src := `(deftype Box [v]
  Shape
  (area [_] 1))
`
	ents := extract(t, "src/core.clj", src)
	wantPairs(t, ents, "Box-[IMPLEMENTS]->Shape")
}

// TestClojureHierarchy_MethodBodySymbolsAreNotSupertypes is the depth fence.
// Every bare symbol below sits at paren-depth >= 2: two inside a method body,
// two more inside a `let` binding vector and its body. The record's only
// depth-1 symbol is Shape, so the assertion is that exactly one edge exists —
// stated as the full edge SET, so a body symbol that leaked in is visible by
// name rather than only as a count.
func TestClojureHierarchy_MethodBodySymbolsAreNotSupertypes(t *testing.T) {
	src := `(defrecord Circle [radius]
  Shape
  (area [_]
    (let [helper compute-area]
      (println radius)
      (helper radius))))
`
	ents := extract(t, "src/core.clj", src)
	wantPairs(t, ents, "Circle-[IMPLEMENTS]->Shape")
}

// TestClojureHierarchy_ExtendTypeSubjectImplementsTheProtocols pins
// extend-type's direction on its own: the HEAD symbol is the implementer and
// the tail symbols are the protocols. The subject is declared in the same file
// so that this test grades direction only, not the anchoring limit.
func TestClojureHierarchy_ExtendTypeSubjectImplementsTheProtocols(t *testing.T) {
	src := `(defrecord MyType [])
(extend-type MyType
  Shape
  (area [_] 1)
  Sized
  (size [_] 2))
`
	ents := extract(t, "src/core.clj", src)
	wantPairs(t, ents,
		"MyType-[IMPLEMENTS]->Shape",
		"MyType-[IMPLEMENTS]->Sized")
}

// TestClojureHierarchy_ExtendProtocolIsInverted is the direction pin.
//
// `extend-protocol`'s head symbol is the PROTOCOL and its tail symbols are the
// IMPLEMENTERS — the opposite of `extend-type`, which this file's other tests
// pin the normal way round. The two forms are otherwise shape-identical, so a
// single code path handling both emits every edge here BACKWARDS while
// producing exactly the same NUMBER of edges.
//
// This test therefore asserts owner->target PAIRS. Under the swap it would see
// Shape->TypeA and Shape->TypeB instead of TypeA->Shape and TypeB->Shape: same
// row count, different rows. A count assertion passes; this one fails.
func TestClojureHierarchy_ExtendProtocolIsInverted(t *testing.T) {
	src := `(defprotocol Shape)
(defrecord TypeA [])
(defrecord TypeB [])
(extend-protocol Shape
  TypeA
  (area [_] 1)
  TypeB
  (area [_] 2))
`
	ents := extract(t, "src/core.clj", src)
	wantPairs(t, ents,
		"TypeA-[IMPLEMENTS]->Shape",
		"TypeB-[IMPLEMENTS]->Shape")
}

// TestClojureHierarchy_ExtendTypeAndExtendProtocolCoexistWithoutSwapping puts
// both keywords in ONE file with names chosen so that a swapped code path
// cannot coincidentally produce the right set: Alpha extends via extend-type,
// Beta via extend-protocol, and the protocols differ. Swapping either arm
// moves a row onto a protocol component, which the whole-file helper sees.
func TestClojureHierarchy_ExtendTypeAndExtendProtocolCoexistWithoutSwapping(t *testing.T) {
	src := `(defprotocol Drawable)
(defprotocol Printable)
(defrecord Alpha [])
(defrecord Beta [])
(extend-type Alpha
  Drawable
  (draw [_] 1))
(extend-protocol Printable
  Beta
  (print-it [_] 2))
`
	ents := extract(t, "src/core.clj", src)
	wantPairs(t, ents,
		"Alpha-[IMPLEMENTS]->Drawable",
		"Beta-[IMPLEMENTS]->Printable")
}

// TestClojureHierarchy_CommentFormEmitsNoEdge is fence 2.
//
// A `(comment ...)` rich block is idiomatic Clojure scratch and is not
// evaluated. deftypeRE's `^\s*\(` allows leading whitespace, so the indented
// declaration below IS matched and DOES produce an entity today — that
// pre-existing over-fire is asserted here too, so the test does not silently
// start claiming more than it earns if entity extraction changes. What this
// test pins is that no EDGE joins it.
func TestClojureHierarchy_CommentFormEmitsNoEdge(t *testing.T) {
	src := `(defrecord Real [] Shape (area [_] 1))
(comment
  (defrecord Scratch [x]
    Ghost
    (area [_] 0)))
`
	ents := extract(t, "src/core.clj", src)
	wantPairs(t, ents, "Real-[IMPLEMENTS]->Shape")

	if findComp(ents, "Scratch") == nil {
		t.Error("Scratch component vanished: the pre-existing entity over-fire this test documents is gone, so its wording is now wrong")
	}
}

// TestClojureHierarchy_StringContentEmitsNoEdge is fence 3.
//
// deftypeRE runs on the RAW source, so a declaration written inside a
// multi-line string or docstring is matched and produces an entity today. The
// hierarchy scan runs on a string-blanked copy, so no edge joins it. As above,
// the pre-existing entity is asserted rather than assumed.
func TestClojureHierarchy_StringContentEmitsNoEdge(t *testing.T) {
	src := `(defrecord Real [] Shape (area [_] 1))
(def doc "
(defrecord Fake [y]
  Phantom
  (area [_] 0))
")
`
	ents := extract(t, "src/core.clj", src)
	wantPairs(t, ents, "Real-[IMPLEMENTS]->Shape")

	if findComp(ents, "Fake") == nil {
		t.Error("Fake component vanished: the pre-existing entity over-fire this test documents is gone, so its wording is now wrong")
	}
}

// TestClojureHierarchy_LineCommentedDeclarationEmitsNoEdge covers the `;`
// half of the sanitiser. A `;`-commented declaration is NOT matched by
// deftypeRE either (the `;` precedes the paren on the line), so unlike the two
// tests above there is no entity here to assert — the claim is only that the
// edge is absent, which is what the body observes.
func TestClojureHierarchy_LineCommentedDeclarationEmitsNoEdge(t *testing.T) {
	src := `(defrecord Real [] Shape (area [_] 1))
;; (defrecord Commented [] Ghost (area [_] 0))
`
	ents := extract(t, "src/core.clj", src)
	wantPairs(t, ents, "Real-[IMPLEMENTS]->Shape")
}

// TestClojureHierarchy_GenClassSelectsBothLadderArms is the only place in the
// language that names a real superclass. It varies the KEYWORD inside one
// `(:gen-class)` directive and asserts that `:extends` selects EXTENDS while
// `:implements` selects IMPLEMENTS for every member of its vector — i.e. that
// the kind is decided by the keyword, not by the target's spelling.
func TestClojureHierarchy_GenClassSelectsBothLadderArms(t *testing.T) {
	src := `(ns app.core
  (:gen-class
    :name app.Core
    :extends javax.servlet.http.HttpServlet
    :implements [java.lang.Runnable java.io.Closeable]))
`
	ents := extract(t, "src/core.clj", src)
	wantPairs(t, ents,
		"app.core-[EXTENDS]->javax.servlet.http.HttpServlet",
		"app.core-[IMPLEMENTS]->java.lang.Runnable",
		"app.core-[IMPLEMENTS]->java.io.Closeable")
}

// TestClojureHierarchy_ExtendsKeywordOutsideGenClassIsNotRead holds the
// keyword constant and varies only WHERE it sits. The file HAS a (:gen-class)
// directive, and that directive has no `:extends` of its own; a second
// `:extends` sits in a map elsewhere in the file. Reading only inside the
// located directive yields the one IMPLEMENTS row and no EXTENDS; a file-wide
// keyword search adds `app.core -[EXTENDS]-> some.Base`.
//
// The gen-class directive is deliberately present rather than absent: with no
// directive at all genClassEdges returns before it reads anything, so the test
// would pass without observing the scoping it is named for.
func TestClojureHierarchy_ExtendsKeywordOutsideGenClassIsNotRead(t *testing.T) {
	src := `(ns app.core
  (:require [other.ns :refer [thing]])
  (:gen-class
    :name app.Core
    :implements [java.lang.Runnable]))
(def opts {:extends some.Base})
`
	ents := extract(t, "src/core.clj", src)
	wantPairs(t, ents, "app.core-[IMPLEMENTS]->java.lang.Runnable")
}

// TestClojureHierarchy_ExtendProtocolHeadKeepsItsQualifier varies ONE axis —
// the qualifier on the extend-protocol HEAD symbol — and holds the tail
// constant. The head is the edge's TARGET for this keyword, so a head charset
// that stops at the first `.` does not merely truncate a name, it points the
// edge at `other` instead of `other.ns/Proto`. That is what the assertion
// observes; it says nothing about targets in a defrecord tail, which is
// TestClojureHierarchy_QualifiedTargetsKeepTheirWrittenForm.
func TestClojureHierarchy_ExtendProtocolHeadKeepsItsQualifier(t *testing.T) {
	src := `(defrecord TypeA [])
(extend-protocol other.ns/Proto
  TypeA
  (m [_] 1))
`
	ents := extract(t, "src/core.clj", src)
	wantPairs(t, ents, "TypeA-[IMPLEMENTS]->other.ns/Proto")
}

// TestClojureHierarchy_FormMerelyStartingWithCommentIsNotBlanked pins that the
// `(comment ...)` blanking matches the head symbol WHOLE. `(comments ...)` is
// a different form — a user macro, here — and its body is real code; blanking
// it would silently drop the declarations inside it.
func TestClojureHierarchy_FormMerelyStartingWithCommentIsNotBlanked(t *testing.T) {
	src := `(comments
  (defrecord Real [] Shape (area [_] 1)))
`
	ents := extract(t, "src/core.clj", src)
	wantPairs(t, ents, "Real-[IMPLEMENTS]->Shape")
}

// TestClojureHierarchy_DefprotocolAndDefinterfaceAreNotScanned pins the
// call-site exclusion. `:extend-via-metadata true` puts a bare symbol —
// `true` — at depth 1 of a defprotocol tail; definterface's tail is lists
// only. Neither may produce an edge. The test observes the absence of ANY
// edge in the file, so a defprotocol arm that emitted `Shape IMPLEMENTS true`
// fails it by name.
func TestClojureHierarchy_DefprotocolAndDefinterfaceAreNotScanned(t *testing.T) {
	src := `(defprotocol Shape
  :extend-via-metadata true
  "A shape."
  (area [this] "the area"))
(definterface Sized
  (^long size []))
`
	ents := extract(t, "src/core.clj", src)
	wantPairs(t, ents)
}

// TestClojureHierarchy_DefmultiDispatchSymbolIsNotASupertype pins the other
// call-site exclusion: `(defmulti area class)` has a bare depth-1 symbol that
// is a DISPATCH FUNCTION. defmulti is absent from the keyword list, so the
// tail is never read.
func TestClojureHierarchy_DefmultiDispatchSymbolIsNotASupertype(t *testing.T) {
	src := `(defmulti area class)
`
	ents := extract(t, "src/core.clj", src)
	wantPairs(t, ents)
}

// TestClojureHierarchy_QualifiedTargetsKeepTheirWrittenForm varies ONE axis —
// the qualifier style of the target — over one record: a dotted Java class, a
// namespace-qualified protocol with `/`, and a bare name. A target charset
// missing `.` or `/` truncates the first two mid-token and yields `java` and
// `other.ns`, which this test names explicitly rather than counting.
func TestClojureHierarchy_QualifiedTargetsKeepTheirWrittenForm(t *testing.T) {
	src := `(defrecord Rec [a]
  java.lang.Runnable
  (run [_] nil)
  other.ns/Proto
  (m [_] nil)
  Bare
  (b [_] nil))
`
	ents := extract(t, "src/core.clj", src)
	wantPairs(t, ents,
		"Rec-[IMPLEMENTS]->java.lang.Runnable",
		"Rec-[IMPLEMENTS]->other.ns/Proto",
		"Rec-[IMPLEMENTS]->Bare")
}

// TestClojureHierarchy_NonSymbolDepth1TokensAreRejectedWhole varies the SHAPE
// of the depth-1 tokens around one real supertype: a keyword, a metadata-
// prefixed symbol, a quoted symbol, a discard, a number and a map. Each is
// rejected as a whole token, so none contributes a truncated name — the test
// asserts the full edge set, so `private`, `Quoted` or `discarded` leaking in
// would be visible by name.
func TestClojureHierarchy_NonSymbolDepth1TokensAreRejectedWhole(t *testing.T) {
	src := `(defrecord Rec [a]
  :some-keyword
  ^:private
  'Quoted
  #_discarded
  42
  {:k 1}
  Shape
  (area [_] 1))
`
	ents := extract(t, "src/core.clj", src)
	wantPairs(t, ents, "Rec-[IMPLEMENTS]->Shape")
}

// TestClojureHierarchy_SelfConformanceIsDropped asserts that a record naming
// ITSELF yields no edge. A self-edge is never information and is the signature
// of a mis-attributed owner (#6369). The record's other supertype is present so
// that the assertion distinguishes "the self-edge was dropped" from "the whole
// tail was dropped".
func TestClojureHierarchy_SelfConformanceIsDropped(t *testing.T) {
	src := `(defrecord Rec [a]
  Rec
  (self [_] nil)
  Shape
  (area [_] 1))
`
	ents := extract(t, "src/core.clj", src)
	wantPairs(t, ents, "Rec-[IMPLEMENTS]->Shape")
}

// TestClojureHierarchy_TargetRepeatedWithinOneFormYieldsOneEdge covers the
// dedup INSIDE a single form's tail: naming a protocol twice in one defrecord
// states one fact. This is a different code path from the cross-form dedup
// below, and it is the only test that reaches it.
func TestClojureHierarchy_TargetRepeatedWithinOneFormYieldsOneEdge(t *testing.T) {
	src := `(defrecord Rec [a]
  Shape
  (area [_] 1)
  Shape
  (perimeter [_] 2))
`
	ents := extract(t, "src/core.clj", src)
	wantPairs(t, ents, "Rec-[IMPLEMENTS]->Shape")
}

// TestClojureHierarchy_TargetRepeatedAcrossFormsYieldsOneEdge covers the OTHER
// dedup: the same target reached once inline and once through extend-type,
// which are two different producers writing onto one component.
func TestClojureHierarchy_TargetRepeatedAcrossFormsYieldsOneEdge(t *testing.T) {
	src := `(defrecord Rec [a]
  Shape
  (area [_] 1))
(extend-type Rec
  Shape
  (area [_] 2))
`
	ents := extract(t, "src/core.clj", src)
	wantPairs(t, ents, "Rec-[IMPLEMENTS]->Shape")
}

// TestClojureHierarchy_EdgeIsAnchoredOnTheComponentNotTheFile asserts that
// every hierarchy edge leaves FromID EMPTY, which is what makes assembly stamp
// the owning component's id. A non-empty non-hex FromID would be rewritten
// onto the FILE entity, merging every type in the file onto one node — the
// shape internal/extractors/file_anchored_rels_guard_test.go forbids.
func TestClojureHierarchy_EdgeIsAnchoredOnTheComponentNotTheFile(t *testing.T) {
	src := `(ns app.core (:gen-class :extends a.B))
(defrecord Circle [] Shape (area [_] 1))
(defrecord Square [] Sized (size [_] 2))
`
	ents := extract(t, "src/core.clj", src)
	n := 0
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Kind != "IMPLEMENTS" && r.Kind != "EXTENDS" {
				continue
			}
			n++
			if r.FromID != "" {
				t.Errorf("%s -[%s]-> %s has FromID %q, want empty", ents[i].Name, r.Kind, r.ToID, r.FromID)
			}
		}
	}
	if n != 3 {
		t.Fatalf("checked %d hierarchy edges, want 3 — the assertion above is vacuous otherwise", n)
	}
}

// TestClojureHierarchy_NoDuplicateComponents guards the half of the
// "emit from the language extractor" decision that is about NODES rather than
// edges: naming a protocol as a target must not synthesise a component for it,
// and the declared types must each appear exactly once.
func TestClojureHierarchy_NoDuplicateComponents(t *testing.T) {
	src := `(defprotocol Shape)
(defrecord A [] Shape (area [_] 1) java.lang.Runnable (run [_] nil))
(extend-type A Sized (size [_] 2))
(extend-protocol Shape B (area [_] 3))
`
	ents := extract(t, "src/core.clj", src)
	count := map[string]int{}
	for i := range ents {
		if ents[i].Kind == "SCOPE.Component" {
			count[ents[i].Name]++
		}
	}
	for _, n := range []string{"Shape", "A"} {
		if count[n] != 1 {
			t.Errorf("component %s appears %d times, want 1", n, count[n])
		}
	}
	for _, n := range []string{"Sized", "java.lang.Runnable", "B"} {
		if count[n] != 0 {
			t.Errorf("%s was synthesised as a component %d times, want 0", n, count[n])
		}
	}
}

// TestClojureHierarchy_ExtendTypeOnAForeignTypeIsDropped_KnownDivergence
// records a real RECALL LIMIT rather than a guard: `extend-type` names an
// implementer this file does not declare, there is therefore no component to
// anchor the edge on, and the edge is dropped. The alternatives are to
// synthesise a component (which duplicates the node whenever the type is
// declared in another file) or to anchor on the file (forbidden). The test
// exists so the zero is a recorded decision, not a silent miss.
func TestClojureHierarchy_ExtendTypeOnAForeignTypeIsDropped_KnownDivergence(t *testing.T) {
	src := `(extend-type java.lang.String
  Shape
  (area [_] 1))
`
	ents := extract(t, "src/core.clj", src)
	wantPairs(t, ents)
}

// findComp returns the first SCOPE.Component with the given name, or nil.
func findComp(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == "SCOPE.Component" && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}
