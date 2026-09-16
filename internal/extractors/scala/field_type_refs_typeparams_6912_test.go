package scala_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
)

// field_type_refs_typeparams_6912_test.go — the TYPE-PARAMETER SCOPE of the
// Scala arm of #6912, graded as a form space rather than as a sample.
//
// WHY THIS FILE EXISTS. The arm shipped one type-parameter test that varied the
// parameter's NAME (`[T]` vs `[U]`) and the ANCHOR (class parameter vs template
// body) while holding the parameter's DECLARATION FORM constant at
// bare-invariant, and one sentence of prose claiming the shadow was "refused
// instead of documented". Both variance forms — `[+A]` and `[-A]`, which is how
// the collection-shaped generics Scala is actually written in are spelled
// (`List[+A]`, `Function1[-T, +R]`) — went through a switch arm naming two node
// types THIS GRAMMAR DOES NOT HAVE, so the shadow set was empty and
// `class A; class Box[+A](val a: A)` emitted `Box.a -> A`: a wrong edge that
// BINDS (bound=1 dangling=0), the #7056 signature that no instrument we own can
// see. Enumerating one axis thoroughly is what made its sibling look covered.
//
// So both axes are enumerated here FROM THE GRAMMAR, by CST dump, not from
// memory. Every direct child type `type_parameters` can hold:
//
//	identifier                       [A]                  the invariant name
//	covariant_type_parameter         [+A]                 `+`, identifier, bounds
//	contravariant_type_parameter     [-A]                 `-`, identifier, bounds
//	upper_bound                      [A <: Repo]          holds a REAL type
//	lower_bound                      [A >: Repo]          holds a REAL type
//	view_bound                       [A <% Repo]          holds a REAL type
//	context_bound                    [A : Ord]            holds a REAL type
//	type_parameters   (nested)       [F[_]] / [F[X]]      F's OWN params
//	annotation                       [@specialized A]     holds a REAL type
//	wildcard                         [_ <: Repo]          no name at all
//	[ ] , :                          anonymous
//
// A `type_identifier` NEVER occurs as a direct child: every one of them sits
// inside a bound, an annotation or a nested parameter list. That is why the
// name-collecting arm matches `identifier` only — a direct `type_identifier`,
// if the grammar ever minted one, would be a bound's right-hand side, which is
// a REAL type and must stay BINDABLE.
//
// BOTH DIRECTIONS ARE GRADED IN EVERY ROW. Each fixture carries a positive
// control — a field genuinely typed by a same-file declaration — so a row that
// refuses the shadow because the whole capture broke is distinguishable from a
// row that refuses the shadow because the guard fired. The bounded and
// context-bound rows carry the stronger control: the BOUND'S OWN right-hand
// side is a same-file type and must still bind, which is the over-refusal
// direction arm I got wrong in both directions at once (#7057) and which a
// deleted-correct-edge has no symptom for.

// TestScalaFieldTypeRefs_TypeParameterFormSpace is the enumeration. Every row's
// source is legal Scala (variance positions included: a contravariant parameter
// is placed in a function-type argument, never directly on a `val`), except the
// two noted rows, and every row pairs the refused shadow with a control that
// must still emit.
func TestScalaFieldTypeRefs_TypeParameterFormSpace(t *testing.T) {
	const decls = "class A\nclass Repo\nclass Ord\nclass Real\n"

	for _, tc := range []struct {
		form string
		src  string
		want []string
	}{
		{
			// The shipped control: bare invariant. This row passed before the
			// fix and is kept so a regression in the common form is visible.
			form: "invariant [A]",
			src:  "class Box[A](val a: A, val r: Real)\n",
			want: []string{"Box.r -> Real"},
		},
		{
			// THE DOMINANT SCALA GENERIC FORM (`List[+A]`, `Option[+A]`).
			// Emitted `Box.a -> A` before the fix.
			form: "covariant [+A]",
			src:  "class Box[+A](val a: A, val r: Real)\n",
			want: []string{"Box.r -> Real"},
		},
		{
			// `Function1[-T, +R]`'s form. A contravariant parameter may not sit
			// on a `val` directly, so it sits where the language puts it: the
			// argument of a function type.
			form: "contravariant [-A]",
			src:  "class Sink[-A](val f: A => Real, val r: Real)\n",
			want: []string{"Sink.f -> Real", "Sink.r -> Real"},
		},
		{
			// Upper bound. `Repo` is the bound's RHS AND a real same-file
			// class: refusing it would be the over-refusal direction.
			form: "upper-bounded [A <: Repo]",
			src:  "class Box[A <: Repo](val a: A, val r: Repo)\n",
			want: []string{"Box.r -> Repo"},
		},
		{
			form: "lower-bounded [A >: Repo]",
			src:  "class Box[A >: Repo](val a: A, val r: Repo)\n",
			want: []string{"Box.r -> Repo"},
		},
		{
			// View bounds are Scala 2 only; the grammar parses them and the
			// node type (`view_bound`) is distinct, so the row is kept.
			form: "view-bounded [A <% Repo]",
			src:  "class Box[A <% Repo](val a: A, val r: Repo)\n",
			want: []string{"Box.r -> Repo"},
		},
		{
			form: "context-bounded [A : Ord]",
			src:  "class Box[A : Ord](val a: A, val o: Ord)\n",
			want: []string{"Box.o -> Ord"},
		},
		{
			form: "two context bounds [A : Ord : Repo]",
			src:  "class Box[A : Ord : Repo](val a: A, val o: Ord)\n",
			want: []string{"Box.o -> Ord"},
		},
		{
			// Higher-kinded. `F` is the parameter; `F[Real]` names both, and
			// only `Real` may bind.
			form: "higher-kinded [F[_]]",
			src:  "class Box[F[_]](val f: F[Real], val r: Real)\n",
			want: []string{"Box.f -> Real", "Box.r -> Real"},
		},
		{
			// The HK parameter's OWN parameter `X` is scoped to `F`, not to the
			// class, so a field typed `X` refers to a real same-file `class X`
			// and MUST bind. Refusing it would be over-refusal through a
			// nested `type_parameters` node.
			form: "higher-kinded with a named inner param [F[X]]",
			src:  "class X\nclass Box[F[X]](val x: X, val r: Real)\n",
			want: []string{"Box.r -> Real", "Box.x -> X"},
		},
		{
			form: "variance PLUS upper bound [+A <: Repo]",
			src:  "class Box[+A <: Repo](val a: A, val r: Repo)\n",
			want: []string{"Box.r -> Repo"},
		},
		{
			form: "variance PLUS context bound [+A : Ord]",
			src:  "class Box[+A : Ord](val a: A, val o: Ord)\n",
			want: []string{"Box.o -> Ord"},
		},
		{
			form: "variance PLUS both bounds [+A >: Repo <: Real]",
			src:  "class Box[+A >: Repo <: Real](val a: A, val r: Repo)\n",
			want: []string{"Box.r -> Repo"},
		},
		{
			// An annotated parameter. `@specialized` is a direct `annotation`
			// child of `type_parameters` holding a `type_identifier`. It is an
			// annotation CLASS, so a same-file `class Real` named by an
			// annotation must not join the shadow set.
			form: "annotated [@Real A]",
			src:  "class Box[@Real A](val a: A, val r: Real)\n",
			want: []string{"Box.r -> Real"},
		},
		{
			// Multiple parameters MIXING forms, which is how `Fn[-T, +R]` and
			// `Map[K, +V]` are written.
			form: "multiple mixed [-T, +R]",
			src:  "class T\nclass R\nclass Fn[-T, +R](val f: T => R, val r: Real)\n",
			want: []string{"Fn.r -> Real"},
		},
		{
			form: "multiple mixed, invariant + bounded covariant [A, +B <: Repo]",
			src:  "class B\nclass Box[A, +B <: Repo](val a: A, val b: B, val r: Repo)\n",
			want: []string{"Box.r -> Repo"},
		},
		{
			// A wildcard parameter names nothing, so nothing is refused and the
			// control still binds.
			form: "wildcard [_ <: Repo]",
			src:  "class Box[_ <: Repo](val r: Repo)\n",
			want: []string{"Box.r -> Repo"},
		},
	} {
		t.Run(tc.form, func(t *testing.T) {
			scWantEdges(t, decls+tc.src, tc.want...)
		})
	}
}

// TestScalaFieldTypeRefs_TypeParameterFormSpaceOnTheTemplateBodyAnchor repeats
// the variance rows on the OTHER anchor. Scala has two field producers —
// emitScalaCaseClassFields (class parameters) and buildScalaField (template-body
// val/var) — and they reach the parameter set by different arguments, so a fix
// that lands on one and not the other is invisible to the table above.
func TestScalaFieldTypeRefs_TypeParameterFormSpaceOnTheTemplateBodyAnchor(t *testing.T) {
	const decls = "class A\nclass Repo\nclass Real\n"

	// Covariant, template body.
	scWantEdges(t, decls+"class Box[+A](a0: A) {\n  val a: A = a0\n  val r: Real = null\n}\n",
		"Box.r -> Real")
	// Contravariant, template body. `f0` is a plain (non-val) constructor
	// parameter and emitScalaCaseClassFields mints a field for it too, which is
	// pre-existing and not this arm's; it is listed rather than hidden.
	scWantEdges(t, decls+"class Sink[-A](f0: A => Real) {\n  val f: A => Real = f0\n  val r: Real = null\n}\n",
		"Sink.f -> Real", "Sink.f0 -> Real", "Sink.r -> Real")
	// Bounded, template body — the bound's RHS must still bind.
	scWantEdges(t, decls+"class Box[+A <: Repo](a0: A) {\n  val a: A = a0\n  val r: Repo = null\n}\n",
		"Box.r -> Repo")
	// Trait anchor, covariant.
	scWantEdges(t, decls+"trait Tr[+A] {\n  val a: A = null\n  val r: Real = null\n}\n",
		"Tr.r -> Real")
	// The control that says the refusals above are the GUARD, not a broken
	// capture: the same declaration with the parameter renamed emits.
	scWantEdges(t, decls+"class Box[+U](a0: U) {\n  val a: A = null\n  val r: Real = null\n}\n",
		"Box.a -> A", "Box.r -> Real")
	scWantEdges(t, decls+"class Box[+U](val a: A, val r: Real)\n",
		"Box.a -> A", "Box.r -> Real")
}

// ---------------------------------------------------------------------------
// NESTING. Scala's scoping is NOT Kotlin's, and the rule here is the OPPOSITE
// of arm J's.
//
// Kotlin has a `static`-by-default nesting: a nested class does NOT see the
// enclosing class's type parameters unless it is declared `inner`, so arm J's
// guard must NOT inherit. Scala has no `inner` keyword and no static nesting at
// all — every member class, trait and object of a template is an INNER member of
// that template, and §5.1 evaluates the template in a scope that already
// contains the declaration's type parameters. `object` is not an exception: an
// object nested inside a class is a per-instance member (only a TOP-LEVEL object
// is static-like), so it sees `T` as well, which is precisely where Kotlin's
// `companion object` blocks.
//
// So the derived Scala rule is ACCUMULATE: the type parameters in scope for a
// declaration are its own UNION every lexically enclosing declaration's.
//
// This was filed in the PR under "Recall floors, pinned as tests" and it was
// NEITHER: `class T; class Outer[T] { class Inner(val x: T) }` GAINED a wrong
// edge `Inner.x -> T` that BOUND, rather than losing a right one, and no nesting
// test existed in any of the three new files. A recall floor is safe; this was
// the #7056 shape one nesting level down.
//
// Every row below carries a positive control, and the two OVER-REFUSAL rows are
// the ones that matter most: nesting ALONE must never refuse, because a deleted
// correct edge has no symptom.
// ---------------------------------------------------------------------------

func TestScalaFieldTypeRefs_NestedDeclarationInheritsTheOuterTypeParameters(t *testing.T) {
	const decls = "class T\nclass Real\n"

	for _, tc := range []struct {
		name string
		src  string
		want []string
	}{
		{
			// The defect, at one level.
			name: "class nested in a generic class",
			src:  "class Outer[T] {\n  class Inner(val x: T, val r: Real)\n}\n",
			want: []string{"Inner.r -> Real"},
		},
		{
			name: "class nested in a generic trait",
			src:  "trait Outer[T] {\n  class Inner(val x: T, val r: Real)\n}\n",
			want: []string{"Inner.r -> Real"},
		},
		{
			// An `object` nested in a class is a per-instance member in Scala,
			// so it does NOT block the outer's parameters — where Kotlin's
			// `companion object` would.
			name: "class nested in an object nested in a generic class",
			src:  "class Outer[T] {\n  object Mid {\n    class Inner(val x: T, val r: Real)\n  }\n}\n",
			want: []string{"Inner.r -> Real"},
		},
		{
			// The object's OWN val, same reason.
			name: "val of an object nested in a generic class",
			src:  "class Outer[T] {\n  object Mid {\n    val x: T = null\n    val r: Real = null\n  }\n}\n",
			want: []string{"Mid.r -> Real"},
		},
		{
			// Two levels down, through a NON-generic intermediate: the scope
			// must survive a declaration that introduces nothing of its own.
			name: "class nested two levels under a generic class",
			src:  "class A1[T] {\n  class B1 {\n    class C1(val x: T, val r: Real)\n  }\n}\n",
			want: []string{"C1.r -> Real"},
		},
		{
			// The nested declaration re-declares the name: still refused, now
			// by its OWN parameter. Held here so the accumulate rule is not
			// what this row is measuring.
			name: "nested class with its own shadowing parameter",
			src:  "class Outer[T] {\n  class Inner[T](val x: T, val r: Real)\n}\n",
			want: []string{"Inner.r -> Real"},
		},
		{
			// Variance, one level down: the two fixes compose.
			name: "class nested in a COVARIANT generic class",
			src:  "class Outer[+T] {\n  class Inner(val x: T, val r: Real)\n}\n",
			want: []string{"Inner.r -> Real"},
		},
		{
			// A template-body val of the nested class, the other anchor.
			name: "template-body val of a class nested in a generic class",
			src:  "class Outer[T] {\n  class Inner {\n    val x: T = null\n    val r: Real = null\n  }\n}\n",
			want: []string{"Inner.r -> Real"},
		},

		// ------------------------------------------------------------------
		// OVER-REFUSAL. A deleted correct edge has no symptom; these rows are
		// what stops the fix from becoming arm I's two-directional mistake.
		// ------------------------------------------------------------------
		{
			// NESTING ALONE MUST NOT REFUSE. The outer declares no parameters,
			// so `T` inside `Inner` is the top-level `class T` and the edge is
			// correct. This row differs from row 1 in exactly one character.
			name: "class nested in a NON-generic class still binds",
			src:  "class Outer {\n  class Inner(val x: T, val r: Real)\n}\n",
			want: []string{"Inner.r -> Real", "Inner.x -> T"},
		},
		{
			// The outer IS generic but its parameter is named something else.
			name: "outer parameter with a different name still binds",
			src:  "class Outer[U] {\n  class Inner(val x: T, val r: Real)\n}\n",
			want: []string{"Inner.r -> Real", "Inner.x -> T"},
		},
		{
			// The scope must not leak SIDEWAYS. `Other` is a sibling of
			// `Outer`, not a member, so `Outer`'s `T` is not in its scope.
			name: "sibling of a generic class still binds",
			src:  "class Outer[T] {\n  val q: Real = null\n}\nclass Other(val x: T, val r: Real)\n",
			want: []string{"Other.r -> Real", "Other.x -> T", "Outer.q -> Real"},
		},
		{
			// Nor must it leak FORWARD out of a nested body into a later
			// sibling of the OUTER declaration.
			name: "sibling declared after a nested generic class still binds",
			src:  "class Outer {\n  class Mid[T] {\n    val q: Real = null\n  }\n  class Later(val x: T, val r: Real)\n}\n",
			want: []string{"Later.r -> Real", "Later.x -> T", "Mid.q -> Real"},
		},
		{
			// The same leak WITH A GENERIC OUTER, which is the only shape that
			// distinguishes a merge-into-a-copy from a merge-into-the-parent's
			// own map: the row above cannot see it, because an outer with no
			// parameters hands its children an EMPTY set and the merge short-
			// circuits. `Mid`'s `T` must not survive into its sibling `Later`.
			name: "sibling of a nested generic class under a GENERIC outer still binds",
			src: "class U\nclass Outer[U] {\n  class Mid[T] {\n    val q: Real = null\n  }\n" +
				"  class Later(val x: T, val u: U, val r: Real)\n}\n",
			want: []string{"Later.r -> Real", "Later.x -> T", "Mid.q -> Real"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scWantEdges(t, decls+tc.src, tc.want...)
		})
	}
}

// TestScalaFieldTypeRefs_PremiseAClassInsideADefMintsNoField is the check the
// nesting table deliberately does NOT have a row for. walkNode's
// function_definition arm builds the SCOPE.Operation and RETURNS without
// recursing, so a class declared inside a generic `def` mints no component and
// no field — there is no anchor for the outer `def`'s type parameters to shadow.
// Asserted rather than assumed, so no fixture is manufactured for a form that
// mints nothing: if that arm ever starts recursing, this test fails and the
// nesting table gains a row it can actually grade.
func TestScalaFieldTypeRefs_PremiseAClassInsideADefMintsNoField(t *testing.T) {
	src := "class T\nclass Real\n" +
		"object O {\n  def f[T](): Int = {\n    class Local(val x: T, val r: Real)\n    1\n  }\n}\n"
	ents := runScala(t, src)
	if rec := scRecord(ents, "Local", "SCOPE.Component"); rec != nil {
		t.Fatal("premise moved: a class inside a def now mints a SCOPE.Component, " +
			"so a def's type parameters are now a reachable shadow and need a row")
	}
	for i := range ents {
		if ents[i].Kind == "SCOPE.Schema" && ents[i].Subtype == "field" {
			t.Fatalf("premise moved: a class inside a def now mints a field (%q)",
				ents[i].Name)
		}
	}
	scWantEdges(t, src)
}

// TestScalaFieldTypeRefs_NestedScopeSurvivesTheTwoDEFENSIVERecursionArms grades
// the two walkNode arms that carry the scope but are NOT on the ordinary
// container path, because a mutant that drops the scope on either of them was
// ALIVE until these rows existed.
//
// walkNode reaches a nested declaration by three routes, not one:
//
//  1. emitContainerWithMembers' template-body loop   — the ordinary route
//  2. walkNode's DEFAULT RECURSION                   — for a container under a
//     template-body child that is not itself a container (an `if`/`try` block)
//  3. emitContainerWithMembers' MALFORMED arm        — when buildComponent
//     fails because the declaration has no name
//
// Routes 2 and 3 were both probed for reachability before a row was written for
// them: each of the four sources below really does mint the field the row names,
// so none of these refusals can pass vacuously.
//
// THE LAST TWO SOURCES ARE DELIBERATELY NOT LEGAL SCALA, and that is said out
// loud rather than implied: a nameless `class { … }` is a parse error. It is
// still a real input — a file mid-edit is exactly what the malformed arm exists
// for — and it is the ONLY shape that reaches route 3, so the row is kept and
// labelled instead of manufactured into something legal it could not test.
func TestScalaFieldTypeRefs_NestedScopeSurvivesTheTwoDEFENSIVERecursionArms(t *testing.T) {
	const decls = "class T\nclass Real\n"

	for _, tc := range []struct {
		name string
		src  string
		want []string
	}{
		{
			// Route 2: `if_expression` is not a container, so the class under
			// it arrives through walkNode's default recursion.
			name: "class under an if-block in a generic template body",
			src:  "class Outer[T] {\n  if (true) {\n    class Inner(val x: T, val r: Real)\n  }\n}\n",
			want: []string{"Inner.r -> Real"},
		},
		{
			// Route 2 again, through a different non-container node.
			name: "class under a try-block in a generic template body",
			src: "class Outer[T] {\n  try {\n    class Inner(val x: T, val r: Real)\n  }" +
				" catch { case e: Exception => () }\n}\n",
			want: []string{"Inner.r -> Real"},
		},
		{
			// Route 3, INHERITED: the nameless declaration is between the
			// generic outer and the field, so the scope must survive it.
			// NOT LEGAL SCALA — a nameless `class` is a parse error.
			name: "class under a NAMELESS declaration in a generic outer (not legal Scala)",
			src:  "class Outer[T] {\n  class {\n    class Inner(val x: T, val r: Real)\n  }\n}\n",
			want: []string{"Inner.r -> Real"},
		},
		{
			// Route 3, OWN: the nameless declaration is itself the generic one,
			// so its own parameters must be collected even though buildComponent
			// refused it. NOT LEGAL SCALA.
			name: "class under a NAMELESS GENERIC declaration (not legal Scala)",
			src:  "class [T] {\n  class Inner(val x: T, val r: Real)\n}\n",
			want: []string{"Inner.r -> Real"},
		},

		// The over-refusal controls for the same two routes: neither an
		// if-block nor a nameless declaration may refuse on its own.
		{
			name: "class under an if-block in a NON-generic template body still binds",
			src:  "class Outer {\n  if (true) {\n    class Inner(val x: T, val r: Real)\n  }\n}\n",
			want: []string{"Inner.r -> Real", "Inner.x -> T"},
		},
		{
			name: "class under a NAMELESS declaration with no generic outer still binds (not legal Scala)",
			src:  "class Outer {\n  class {\n    class Inner(val x: T, val r: Real)\n  }\n}\n",
			want: []string{"Inner.r -> Real", "Inner.x -> T"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ents := scWantEdges(t, decls+tc.src, tc.want...)
			// Premise: the row is not vacuous — the field it names EXISTS.
			if scRecord(ents, "Inner.x", "SCOPE.Schema") == nil {
				t.Fatal("premise gone: `Inner.x` mints no field record here, so " +
					"this row refuses nothing and grades nothing")
			}
		})
	}
}

// TestScalaFieldTypeRefs_PremiseTypeParametersChildShapes observes the two
// grammar facts that scalaTypeParameterNames rests on, across the WHOLE
// enumerated form space, so the two mutants they make equivalent are classified
// by a measurement and not by an argument.
//
// FACT 1 — no direct child of `type_parameters` is a `type_identifier`. Every
// type_identifier under a parameter list sits inside a bound, an annotation or a
// nested parameter list, i.e. it is always a REAL type that must stay bindable.
// That is why the name-collecting arm matches `identifier` alone; a mutant that
// re-adds `type_identifier` there is EQUIVALENT under this fact, and if the
// grammar ever changes, this test fails before that mutant becomes live.
//
// FACT 2 — a variance wrapper has exactly ONE direct `identifier` child. That is
// why taking the first and breaking is the same as taking the last; a mutant
// that deletes the `break` is EQUIVALENT under this fact.
//
// Both facts are checked on every form the enumeration knows, and the counters
// below refuse to pass on an empty walk.
func TestScalaFieldTypeRefs_PremiseTypeParametersChildShapes(t *testing.T) {
	forms := []string{
		"class Box[A](val a: A)",
		"class Box[+A](val a: A)",
		"class Box[-A](val a: A)",
		"class Box[A <: Repo](val a: A)",
		"class Box[A >: Repo](val a: A)",
		"class Box[A <% Repo](val a: A)",
		"class Box[A : Ord](val a: A)",
		"class Box[A : Ord : Show](val a: A)",
		"class Box[A <: Repo with Named](val a: A)",
		"class Box[F[_]](val a: Int)",
		"class Box[F[X] <: Repo](val a: Int)",
		"class Box[+A <: Repo](val a: A)",
		"class Box[-A <: Repo](val a: A)",
		"class Box[+A : Ord](val a: A)",
		"class Box[+A >: Nothing <: Repo](val a: A)",
		"class Box[@specialized A](val a: A)",
		"class Box[_ <: Repo](val a: Int)",
		"class Fn[-T, +R](val f: T => R)",
		"class Box[A, +B <: Repo](val a: A)",
		"trait Tr[+A] { val a: A = null }",
		"object O[A] { val a: A = null }",
	}

	lists, variance := 0, 0
	seen := map[string]bool{}
	for _, src := range forms {
		tree := parseForTest(t, src)
		var walk func(n ts.Node)
		walk = func(n ts.Node) {
			if n == nil {
				return
			}
			if n.Type() == "type_parameters" {
				lists++
				for i := 0; i < int(n.ChildCount()); i++ {
					ch := n.Child(i)
					seen[ch.Type()] = true
					if ch.Type() == "type_identifier" {
						t.Fatalf("FACT 1 GONE: %q — `type_parameters` now has a "+
							"direct `type_identifier` child (%q). It would be a "+
							"bound's RHS, i.e. a REAL type; re-adding "+
							"`type_identifier` to the name arm is no longer "+
							"equivalent and is now an OVER-REFUSAL",
							src, string([]byte(src)[ch.StartByte():ch.EndByte()]))
					}
					if ch.Type() == "covariant_type_parameter" ||
						ch.Type() == "contravariant_type_parameter" {
						variance++
						ids := 0
						for j := 0; j < int(ch.ChildCount()); j++ {
							if ch.Child(j).Type() == "identifier" {
								ids++
							}
						}
						if ids != 1 {
							t.Fatalf("FACT 2 GONE: %q — a variance wrapper has %d "+
								"direct `identifier` children, not 1; dropping the "+
								"`break` is no longer equivalent", src, ids)
						}
					}
				}
			}
			for i := 0; i < int(n.ChildCount()); i++ {
				walk(n.Child(i))
			}
		}
		walk(tree.RootNode())
	}

	if lists < len(forms) {
		t.Fatalf("vacuous walk: %d `type_parameters` nodes visited across %d "+
			"forms — the enumeration is not reaching the grammar", lists, len(forms))
	}
	if variance < 8 {
		t.Fatalf("vacuous walk: only %d variance wrappers visited, so FACT 2 is "+
			"barely checked", variance)
	}
	// And the child space itself is pinned, so a NEW child node type that could
	// carry a name is a visible failure rather than a silent gap.
	known := map[string]bool{
		"[": true, "]": true, ",": true, ":": true,
		"identifier": true, "wildcard": true, "annotation": true,
		"covariant_type_parameter": true, "contravariant_type_parameter": true,
		"upper_bound": true, "lower_bound": true, "view_bound": true,
		"context_bound": true, "type_parameters": true,
	}
	for k := range seen {
		if !known[k] {
			t.Fatalf("NEW `type_parameters` child node type %q — the enumeration "+
				"in scalaTypeParameterNames is stale; decide whether it names a "+
				"PARAMETER (refuse it) or a REAL TYPE (keep it bindable)", k)
		}
	}
}
