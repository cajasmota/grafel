// issue6499_qualified_names_test.go — arms 1+2 of #6499.
//
// Kotlin SCOPE.Operation entities are class-qualified (`<EnclosingType>.<leaf>`)
// when declared inside a class / object / interface body, and stay bare at file
// top level. Three producers must agree exactly on that name:
//
//	kotlin.go        buildOperation        (the entity Name)
//	exception_flow.go kotlinFunctionName   (THROWS / CATCHES FromName)
//	references.go    kotlinFrame.funcEmittedName (REFERENCES source)
//
// A disagreement between the first two is SILENT: extractor.EmitExceptionEdges
// looks FromName up in hostByName and, on a miss, leaves hostIdx at 0 — so the
// edge re-attaches to entities[0] (the file carrier) instead of erroring.
//
// Each test asserts a positive control first, so a fixture that stopped parsing
// cannot masquerade as a pass.

package kotlin_test

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"

	"github.com/cajasmota/grafel/internal/types"
)

// ktRefTo reports whether the entity named fromName carries a REFERENCES edge
// whose ToID ends with the given emitted target name.
func ktRefTo(ents []types.EntityRecord, fromName, targetName string) bool {
	for i := range ents {
		if ents[i].Name != fromName {
			continue
		}
		for _, r := range ents[i].Relationships {
			if r.Kind == "REFERENCES" && strings.HasSuffix(r.ToID, ":"+targetName) {
				return true
			}
		}
	}
	return false
}

// ktCallsSummary stringifies every CALLS edge for failure messages.
func ktCallsSummary(ents []types.EntityRecord) string {
	var b strings.Builder
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Kind == "CALLS" {
				b.WriteString(ents[i].Name + " -> " + r.ToID + "; ")
			}
		}
	}
	if b.Len() == 0 {
		return "(no CALLS)"
	}
	return b.String()
}

func ktNames(ents []types.EntityRecord, kind string) string {
	var b strings.Builder
	for i := range ents {
		if ents[i].Kind == kind {
			b.WriteString(ents[i].Name + "; ")
		}
	}
	return b.String()
}

// A method declared in a class body is emitted class-qualified.
func TestKotlin6499_NestedFunctionIsClassQualified(t *testing.T) {
	src := `package com.demo

class UserController {
    fun health(): String { return "ok" }
}
`
	ents := runKotlin(t, src)
	// Positive control — the class itself extracted.
	if ktFind(ents, "UserController", "SCOPE.Component") == nil {
		t.Fatalf("positive control failed: no SCOPE.Component UserController; got %s",
			ktNames(ents, "SCOPE.Component"))
	}
	if ktFind(ents, "UserController.health", "SCOPE.Operation") == nil {
		t.Fatalf("want SCOPE.Operation UserController.health; got %s",
			ktNames(ents, "SCOPE.Operation"))
	}
	if ktFind(ents, "health", "SCOPE.Operation") != nil {
		t.Errorf("bare `health` must not survive alongside the qualified name; got %s",
			ktNames(ents, "SCOPE.Operation"))
	}
}

// The CONTAINS structural-ref the class emits carries the QUALIFIED member
// name, so the class→method edge still points at a name an entity carries.
func TestKotlin6499_ContainsRefCarriesQualifiedName(t *testing.T) {
	src := `package com.demo

class UserController {
    fun health(): String { return "ok" }
}
`
	ents := runKotlin(t, src)
	cls := ktFind(ents, "UserController", "SCOPE.Component")
	if cls == nil {
		t.Fatalf("positive control failed: no SCOPE.Component UserController")
	}
	var contains []string
	for _, r := range cls.Relationships {
		if r.Kind == "CONTAINS" {
			contains = append(contains, r.ToID)
		}
	}
	if len(contains) == 0 {
		t.Fatalf("positive control failed: UserController emitted no CONTAINS edges")
	}
	found := false
	for _, id := range contains {
		if strings.HasSuffix(id, ":UserController.health") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a CONTAINS ref ending :UserController.health, got %v", contains)
	}
}

// An object / interface member is qualified by its enclosing type too.
func TestKotlin6499_ObjectAndInterfaceMembersAreQualified(t *testing.T) {
	src := `package com.demo

object Registry {
    fun lookup(): String { return "" }
}

interface Store {
    fun save(): Unit
}
`
	ents := runKotlin(t, src)
	if ktFind(ents, "Registry", "SCOPE.Component") == nil {
		t.Fatalf("positive control failed: no SCOPE.Component Registry; got %s",
			ktNames(ents, "SCOPE.Component"))
	}
	if ktFind(ents, "Registry.lookup", "SCOPE.Operation") == nil {
		t.Errorf("want Registry.lookup; got %s", ktNames(ents, "SCOPE.Operation"))
	}
	if ktFind(ents, "Store.save", "SCOPE.Operation") == nil {
		t.Errorf("want Store.save (Kotlin interface is a class_declaration); got %s",
			ktNames(ents, "SCOPE.Operation"))
	}
}

// Decision pinned: a companion-object member is qualified by the OUTER class,
// not by "Companion". `Outer.build()` is exactly how Kotlin source calls it.
func TestKotlin6499_CompanionMemberQualifiedByOuterClass(t *testing.T) {
	src := `package com.demo

class Outer {
    companion object {
        fun build(): String { return "" }
    }
}
`
	ents := runKotlin(t, src)
	if ktFind(ents, "Outer", "SCOPE.Component") == nil {
		t.Fatalf("positive control failed: no SCOPE.Component Outer; got %s",
			ktNames(ents, "SCOPE.Component"))
	}
	if ktFind(ents, "Outer.build", "SCOPE.Operation") == nil {
		t.Fatalf("want Outer.build (companion members take the OUTER class name); got %s",
			ktNames(ents, "SCOPE.Operation"))
	}
	if ktFind(ents, "Companion.build", "SCOPE.Operation") != nil {
		t.Errorf("companion members must NOT be qualified by `Companion`; got %s",
			ktNames(ents, "SCOPE.Operation"))
	}
}

// A top-level function has no enclosing type, so it stays bare. Qualifying it
// unconditionally would yield `.helper`, which names nothing.
func TestKotlin6499_TopLevelFunctionStaysBare(t *testing.T) {
	src := `package com.demo

fun helper(): Int { return 1 }

class Holder {
    fun member(): Int { return 2 }
}
`
	ents := runKotlin(t, src)
	// Positive control — the nested sibling IS qualified, proving the
	// qualification pass ran at all in this fixture.
	if ktFind(ents, "Holder.member", "SCOPE.Operation") == nil {
		t.Fatalf("positive control failed: want Holder.member; got %s",
			ktNames(ents, "SCOPE.Operation"))
	}
	if ktFind(ents, "helper", "SCOPE.Operation") == nil {
		t.Fatalf("top-level fun must stay bare `helper`; got %s",
			ktNames(ents, "SCOPE.Operation"))
	}
	for i := range ents {
		if strings.HasPrefix(ents[i].Name, ".") {
			t.Errorf("entity name starts with a bare dot: %q", ents[i].Name)
		}
	}
}

// The self-recursion filter in extractCallRelationships compares the callee
// text against the caller name. Feeding it the QUALIFIED name stops it matching
// a recursive `health()` call, minting a spurious self-CALLS edge. Pin that the
// LEAF name still reaches the filter.
func TestKotlin6499_RecursiveMethodEmitsNoSelfCall(t *testing.T) {
	src := `package com.demo

class UserController {
    fun health(): String {
        sideEffect()
        return health()
    }
    fun sideEffect(): Unit {}
}
`
	ents := runKotlin(t, src)
	// Positive control — a non-recursive call IS emitted, so an empty CALLS
	// set cannot pass this test.
	if !ktHasRel(ents, "UserController.health", "SCOPE.Operation", "CALLS", "sideEffect") {
		t.Fatalf("positive control failed: want CALLS health -> sideEffect; got %s",
			ktCallsSummary(ents))
	}
	for _, bad := range []string{"health", "UserController.health"} {
		if ktHasRel(ents, "UserController.health", "SCOPE.Operation", "CALLS", bad) {
			t.Errorf("recursive call must not emit a self-CALLS edge (ToID %q); got %s",
				bad, ktCallsSummary(ents))
		}
	}
}

// A top-level recursive function must not self-call either — the leaf/qualified
// split must not regress the unqualified side.
func TestKotlin6499_TopLevelRecursionEmitsNoSelfCall(t *testing.T) {
	src := `package com.demo

fun other(): Int { return 0 }

fun countdown(n: Int): Int {
    other()
    return countdown(n)
}
`
	ents := runKotlin(t, src)
	if !ktHasRel(ents, "countdown", "SCOPE.Operation", "CALLS", "other") {
		t.Fatalf("positive control failed: want CALLS countdown -> other; got %s",
			ktCallsSummary(ents))
	}
	if ktHasRel(ents, "countdown", "SCOPE.Operation", "CALLS", "countdown") {
		t.Errorf("top-level recursion must not emit a self-CALLS edge; got %s",
			ktCallsSummary(ents))
	}
}

// THROWS must land on the real host operation. If kotlinFunctionName and
// buildOperation disagree, EmitExceptionEdges misses hostByName and silently
// re-attaches the edge to entities[0] — the file carrier.
func TestKotlin6499_ExceptionEdgeLandsOnQualifiedHost(t *testing.T) {
	src := `package com.demo

class UserController {
    fun getUser(): String {
        throw NotFoundException("nope")
    }
}
`
	ents := runKotlin(t, src)
	if len(ents) == 0 {
		t.Fatalf("positive control failed: no entities")
	}
	// Positive control — the shared exception node converged exactly once.
	if n := ktExcNodeCount(ents, "NotFoundException"); n != 1 {
		t.Fatalf("positive control failed: want 1 NotFoundException node, got %d", n)
	}
	if !ktExcEdge(ents, "UserController.getUser", "THROWS", "NotFoundException") {
		t.Fatalf("THROWS must attach to the qualified host UserController.getUser; entities: %s",
			ktNames(ents, "SCOPE.Operation"))
	}
	// The silent-fallback signature: the edge landing on entities[0].
	for _, r := range ents[0].Relationships {
		if r.Kind == "THROWS" {
			t.Errorf("THROWS re-attached to entities[0] (%q) — the two name producers disagree",
				ents[0].Name)
		}
	}
}

// CATCHES moves in lockstep with THROWS.
func TestKotlin6499_CatchEdgeLandsOnQualifiedHost(t *testing.T) {
	src := `package com.demo

class Repo {
    fun load(): String {
        try {
            return "x"
        } catch (e: SqlException) {
            return ""
        }
    }
}
`
	ents := runKotlin(t, src)
	if n := ktExcNodeCount(ents, "SqlException"); n != 1 {
		t.Fatalf("positive control failed: want 1 SqlException node, got %d", n)
	}
	if !ktExcEdge(ents, "Repo.load", "CATCHES", "SqlException") {
		t.Fatalf("CATCHES must attach to Repo.load; operations: %s", ktNames(ents, "SCOPE.Operation"))
	}
	for _, r := range ents[0].Relationships {
		if r.Kind == "CATCHES" {
			t.Errorf("CATCHES re-attached to entities[0] (%q)", ents[0].Name)
		}
	}
}

// A top-level function's exception edge still uses the bare name.
func TestKotlin6499_TopLevelExceptionEdgeUsesBareName(t *testing.T) {
	src := `package com.demo

fun boom(): Unit {
    throw AuthException("no")
}
`
	ents := runKotlin(t, src)
	if n := ktExcNodeCount(ents, "AuthException"); n != 1 {
		t.Fatalf("positive control failed: want 1 AuthException node, got %d", n)
	}
	if !ktExcEdge(ents, "boom", "THROWS", "AuthException") {
		t.Fatalf("THROWS must attach to the bare top-level `boom`; operations: %s",
			ktNames(ents, "SCOPE.Operation"))
	}
}

// Same-file REFERENCES still resolve after qualification: the symbol table is
// keyed by the LEAF (identifiers in source are bare) while the edge target and
// the edge SOURCE both carry the emitted, qualified name.
func TestKotlin6499_SameFileReferenceStillResolves(t *testing.T) {
	src := `package com.demo

class Widget {
    fun render(): Int { return 1 }

    fun draw(): Any {
        val fn = this.render
        return fn
    }
}
`
	ents := runKotlin(t, src)
	if ktFind(ents, "Widget.draw", "SCOPE.Operation") == nil {
		t.Fatalf("positive control failed: want Widget.draw; got %s", ktNames(ents, "SCOPE.Operation"))
	}
	if !ktRefTo(ents, "Widget.draw", "Widget.render") {
		var got []string
		for i := range ents {
			for _, r := range ents[i].Relationships {
				if r.Kind == "REFERENCES" {
					got = append(got, ents[i].Name+" -> "+r.ToID)
				}
			}
		}
		t.Fatalf("want REFERENCES Widget.draw -> ...:Widget.render, got %v", got)
	}
}

// A same-file reference to a sibling CLASS still resolves — this pins the
// REFERENCES *source* side (funcEmittedName must match the emitted entity or
// findKotlinEntityIndex misses and the edge is dropped entirely).
func TestKotlin6499_ReferenceSourceUsesQualifiedName(t *testing.T) {
	src := `package com.demo

class Helper

class Caller {
    fun make(): Any {
        val ref: Helper = Helper()
        return ref
    }
}
`
	ents := runKotlin(t, src)
	if ktFind(ents, "Caller.make", "SCOPE.Operation") == nil {
		t.Fatalf("positive control failed: want Caller.make; got %s", ktNames(ents, "SCOPE.Operation"))
	}
	if !ktRefTo(ents, "Caller.make", "Helper") {
		t.Fatalf("want REFERENCES Caller.make -> ...:Helper; operations: %s",
			ktNames(ents, "SCOPE.Operation"))
	}
}

// The companion-object decision must hold in the EXCEPTION-FLOW producer too,
// not just in the entity minter. exception_flow.go's walk has no
// `companion_object` case, so a companion body inherits the enclosing class's
// parentType — the same fall-through kotlin.go relies on. Nothing in the suite
// covered a throw or catch inside a companion body, so that agreement was
// unpinned: a companion member's FromName could have been `Companion.build`
// while its entity is `Outer.build`, and EmitExceptionEdges would have silently
// re-attached the edge to entities[0].
func TestKotlin6499_CompanionExceptionEdgeUsesOuterClass(t *testing.T) {
	src := `package com.demo

class Outer {
    companion object {
        fun build(): String {
            throw BuildException("nope")
        }

        fun guard(): String {
            try {
                return build()
            } catch (e: BuildException) {
                return ""
            }
        }
    }
}
`
	ents := runKotlin(t, src)
	// Positive control — the companion members were minted under the OUTER
	// class, so the entities the edges must find actually exist.
	if ktFind(ents, "Outer.build", "SCOPE.Operation") == nil {
		t.Fatalf("positive control failed: want Outer.build; got %s",
			ktNames(ents, "SCOPE.Operation"))
	}
	if n := ktExcNodeCount(ents, "BuildException"); n != 1 {
		t.Fatalf("positive control failed: want 1 BuildException node, got %d", n)
	}
	if !ktExcEdge(ents, "Outer.build", "THROWS", "BuildException") {
		t.Errorf("companion THROWS must attach to Outer.build (the OUTER class); operations: %s",
			ktNames(ents, "SCOPE.Operation"))
	}
	if !ktExcEdge(ents, "Outer.guard", "CATCHES", "BuildException") {
		t.Errorf("companion CATCHES must attach to Outer.guard; operations: %s",
			ktNames(ents, "SCOPE.Operation"))
	}
	// A `Companion.`-qualified FromName would find no host, so the edge would
	// land on entities[0] — the exact silent fallback this pass guards against.
	for _, r := range ents[0].Relationships {
		if r.Kind == "THROWS" || r.Kind == "CATCHES" {
			t.Errorf("%s re-attached to entities[0] (%q) — the companion rule "+
				"disagrees between buildOperation and kotlinFunctionName", r.Kind, ents[0].Name)
		}
	}
}

// A companion member's same-file REFERENCES also key off the OUTER class, so
// all three producers agree on companion naming, not merely two.
func TestKotlin6499_CompanionReferenceUsesOuterClass(t *testing.T) {
	src := `package com.demo

class Outer {
    companion object {
        fun render(): Int { return 1 }

        fun draw(): Any {
            val fn = this.render
            return fn
        }
    }
}
`
	ents := runKotlin(t, src)
	if ktFind(ents, "Outer.draw", "SCOPE.Operation") == nil {
		t.Fatalf("positive control failed: want Outer.draw; got %s", ktNames(ents, "SCOPE.Operation"))
	}
	if !ktRefTo(ents, "Outer.draw", "Outer.render") {
		var got []string
		for i := range ents {
			for _, r := range ents[i].Relationships {
				if r.Kind == "REFERENCES" {
					got = append(got, ents[i].Name+" -> "+r.ToID)
				}
			}
		}
		t.Fatalf("want REFERENCES Outer.draw -> ...:Outer.render, got %v", got)
	}
}

// Kotlin allows a dot INSIDE a backtick-quoted identifier, so the qualified
// name of “ fun `x.y`() “ in `class Holder` is "Holder.`x.y`". Undoing that
// qualification with a naive LastIndex(".") yields "y`" — a name no call site
// spells — and keys the resolver's member index wrong. Pin the round trip on
// the emitted entity, not on the helper in isolation.
func TestKotlin6499_BacktickedNameSplitsAtTheRealQualifier(t *testing.T) {
	src := "package com.demo\n" +
		"\n" +
		"class Holder {\n" +
		"    fun `x.y`(): Int { return 1 }\n" +
		"}\n"
	ents := runKotlin(t, src)
	if ktFind(ents, "Holder", "SCOPE.Component") == nil {
		t.Fatalf("positive control failed: no SCOPE.Component Holder; got %s",
			ktNames(ents, "SCOPE.Component"))
	}
	want := "Holder.`x.y`"
	op := ktFind(ents, want, "SCOPE.Operation")
	if op == nil {
		t.Fatalf("want SCOPE.Operation %q; got %s", want, ktNames(ents, "SCOPE.Operation"))
	}
	// The split must recover the WHOLE backticked leaf, not the text after the
	// inner dot. This is what internal/resolve keys byKotlinPkgMember on.
	if got := extractor.KotlinMemberLeaf(op.Name); got != "`x.y`" {
		t.Errorf("KotlinMemberLeaf(%q) = %q, want %q — a dot inside backticks is "+
			"part of the identifier, not a qualifier", op.Name, got, "`x.y`")
	}
}

// The split is the exact inverse of the qualification, across the shapes that
// actually occur: bare, qualified, backticked leaf, backticked enclosing type.
func TestKotlin6499_MemberLeafIsTheInverseOfQualification(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"bare top-level", "helper", "helper"},
		{"qualified member", "OrderService.place", "place"},
		{"backticked leaf", "Holder.`x.y`", "`x.y`"},
		{"backticked enclosing type", "`A.B`.c", "c"},
		{"both backticked", "`A.B`.`c.d`", "`c.d`"},
		{"unbalanced backtick is not split", "`oops", "`oops"},
	}
	for _, tc := range cases {
		if got := extractor.KotlinMemberLeaf(tc.in); got != tc.want {
			t.Errorf("%s: KotlinMemberLeaf(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}
