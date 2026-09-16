package golang_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #7041 — a Go TYPE PARAMETER is a shadowing scope, so a candidate whose name is
// bound by the enclosing declaration's type-parameter list is never the same-file
// type that happens to share its name.
//
// This file replaces TestGoFieldTypeRefs_KnownOverFire_TypeParameterShadowedByASameFileType,
// which was deleted with this change. That pin graded NOTHING: it called
// t.Skipf when the over-fire was gone and t.Logf when it was present, with no
// t.Error or t.Fatal on any path, so a fix "breaking" it — as three separate
// comments claimed it would — was never something the suite could observe.
//
// The consequence recorded on the issue, and honoured here: the deleted pin
// could not have been this fix's safety net anyway, because deleting the
// candidate handling outright would have made it skip too. EVERY assertion of an
// ABSENT edge below therefore sits in a fixture that also asserts a PRESENT one,
// from a field whose declared type is a real same-file declaration. Without that
// pairing, "type parameters are now a shadowing scope" is indistinguishable from
// "the pass stopped emitting anything".

// goFT7041Edges returns "<field> => <target_type>" for every field→declared-type
// edge in recs, sorted. Both directions of every row below are asserted against
// this ONE exact set rather than against a membership test, so an edge that
// should have disappeared and an edge that should have survived are graded by
// the same assertion.
func goFT7041Edges(recs []types.EntityRecord) []string {
	var out []string
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == "REFERENCES" && r.Properties.Get("ref_kind") == "field_target_type" {
				out = append(out, recs[i].Name+" => "+r.Properties.Get("target_type"))
			}
		}
	}
	sort.Strings(out)
	return out
}

// TestGoFieldTypeRefs_7041_FormSpace enumerates Go's type-parameter form space
// and grades each form in BOTH directions at once.
//
// It is an ENUMERATION, not a set of hand-picked attacks. cpp's first cut at the
// same refusal (#7057) was wrong in both directions and the errors were found
// only by enumerating the parameter-form space; three rounds of hand-picked
// attacks on earlier arms each missed something.
//
// ============================ AXES ============================
//
// VARIED across the rows:
//
//   - CONSTRAINT FORM: `any`, `comparable`, a same-file named type, a union of
//     two same-file types, a `~` approximation over a same-file alias, an
//     interface literal, an interface literal embedding a same-file type.
//     Every one of these but the first two puts a SAME-FILE DECLARED NAME inside
//     the parameter list, and every such row carries a sibling field of that
//     type that MUST still bind. That is the over-refusal direction — the
//     direction with no symptom, which cpp shipped and #7056 established no
//     instrument we own can see.
//   - PARAMETER-LIST ARITY: one declaration one name; two declarations; one
//     declaration binding two names (`[K, V any]`); and a list where only ONE of
//     two parameters collides with a same-file type.
//   - PARAMETER NAME SHAPE: an ordinary name, and a PREDECLARED identifier
//     (`[string any]`), which crosses this refusal with the pre-existing
//     shadowed-predeclared-identifier rule.
//   - DECLARED-TYPE WRAPPER FORM, crossed with the parameter axis: `*T`, `[]T`,
//     `[3]T`, `map[T]…`, `chan T`, `func(T) error`, `Holder[T]`. The refusal has
//     to reach every place the candidate collector reaches, and — the other
//     direction — a non-parameter candidate sitting inside the SAME wrapper must
//     survive.
//   - DECLARING FORM: generic struct, generic interface, generic function,
//     method on a generic receiver. Only the first anchors a field entity; the
//     other three are asserted to emit no field edge at all, so the axis is
//     closed by observation rather than by assumption.
//   - POSITION of the colliding declaration relative to the generic one: BEFORE
//     and AFTER. field_type_refs.go documents that the in-file declaration set is
//     complete only after the walk, which makes "before vs after" a live axis
//     rather than a formality.
//   - SCOPE REACH: whether the parameter shadows only its own declaration
//     (`Sibling` rows) or is wrongly treated as file-global.
//
// HELD CONSTANT, and why each is attacked elsewhere:
//
//   - THE ANCHOR is always a named struct field. The other two anchors a Go
//     struct body offers are graded separately and deliberately:
//     TestGoFieldTypeRefs_7041_EmbeddedFieldAndDependsOnAreUntouched covers the
//     embedded field (which produces EXTENDS, not this edge) and the
//     struct-anchored DEPENDS_ON, both of which this change leaves exactly as
//     they were.
//   - THE TARGET KIND is SCOPE.Component/struct in most rows. The alias and
//     interface target kinds are already swept by
//     TestGoFieldTypeRefs_EdgeSetIsExact, and the `Appr` row here carries a
//     type_alias target specifically so the refusal is not graded only against
//     struct targets.
//   - THE FILE is a single one per row. Cross-file behaviour is unchanged by this
//     arm and is graded by TestGoFieldTypeRefs_CrossFileTypeGetsNoEdge.
func TestGoFieldTypeRefs_7041_FormSpace(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{
			// The issue's own shape, plus the control the deleted pin lacked.
			name: "any constraint, collision declared BEFORE",
			src: `package models

type T struct{ Unrelated string }
type Ctl struct{ C string }

type Box[T any] struct {
	Item T
	Ok   Ctl
}
`,
			want: []string{"Box.Ok => Ctl"},
		},
		{
			// POSITION axis. The declaration set is only complete after the walk,
			// so "declared later in the file" is a genuinely different input.
			name: "any constraint, collision declared AFTER",
			src: `package models

type Box[T any] struct {
	Item T
	Ok   Ctl
}

type T struct{ Unrelated string }
type Ctl struct{ C string }
`,
			want: []string{"Box.Ok => Ctl"},
		},
		{
			name: "two declarations, two constraints, both collide",
			src: `package models

type K struct{ A string }
type V struct{ B string }
type Ctl struct{ C string }

type Pair[K comparable, V any] struct {
	Key K
	Val V
	Ok  Ctl
}
`,
			want: []string{"Pair.Ok => Ctl"},
		},
		{
			// ONE declaration binding TWO names. A collector that reads only the
			// first identifier of a type_parameter_declaration leaves `V` live.
			name: "one declaration binding two names",
			src: `package models

type K struct{ A string }
type V struct{ B string }
type Ctl struct{ C string }

type Multi[K, V any] struct {
	A  K
	B  V
	Ok Ctl
}
`,
			want: []string{"Multi.Ok => Ctl"},
		},
		{
			// Only ONE of the two parameters collides. The other parameter has no
			// same-file declaration, so it was already dropped; this row exists so
			// the multi-parameter rows are not all all-or-nothing.
			name: "partial collision in a two-parameter list",
			src: `package models

type K struct{ A string }
type Ctl struct{ C string }

type Pair[K comparable, V any] struct {
	Key K
	Val V
	Ok  Ctl
}
`,
			want: []string{"Pair.Ok => Ctl"},
		},
		{
			// THE OVER-REFUSAL DIRECTION. `Order` is the CONSTRAINT, not the
			// parameter name. A collector that harvests the constraint would
			// silently delete `Con.Ord => Order` — a missing edge with no symptom.
			name: "constraint is a same-file type; the constraint stays bindable",
			src: `package models

type T struct{ A string }
type Order struct{ B string }

type Con[T Order] struct {
	It  T
	Ord Order
}
`,
			want: []string{"Con.Ord => Order"},
		},
		{
			name: "union constraint over two same-file types; both stay bindable",
			src: `package models

type T struct{ A string }
type Order struct{ B string }
type Ctl struct{ C string }

type Uni[T Order | Ctl] struct {
	U   T
	Ord Order
	Ok  Ctl
}
`,
			want: []string{"Uni.Ok => Ctl", "Uni.Ord => Order"},
		},
		{
			// `~Alias` — a negated_type wrapping the name. The target here is a
			// SCOPE.Schema/type_alias rather than a struct, so the refusal is not
			// graded against one target kind only.
			name: "approximation element over a same-file alias; the alias stays bindable",
			src: `package models

type T struct{ A string }
type Alias = int

type Appr[T ~Alias] struct {
	A  T
	Al Alias
}
`,
			want: []string{"Appr.Al => Alias"},
		},
		{
			name: "interface-literal constraint",
			src: `package models

type T struct{ A string }
type Ctl struct{ C string }

type Lit[T interface{ Ship() }] struct {
	L  T
	Ok Ctl
}
`,
			want: []string{"Lit.Ok => Ctl"},
		},
		{
			// An interface literal EMBEDDING a same-file type — a named type
			// nested two levels inside the parameter list.
			name: "interface-literal constraint embedding a same-file type",
			src: `package models

type T struct{ A string }
type Order struct{ B string }

type Emb[T interface{ Order }] struct {
	E   T
	Ord Order
}
`,
			want: []string{"Emb.Ord => Order"},
		},
		{
			// The parameter name is a PREDECLARED identifier, and the file shadows
			// it at package scope too. Inside `Box` the parameter wins; in the
			// non-generic sibling the package-scope declaration wins, which is the
			// pre-existing ShadowedPredeclaredIdentifierIsATarget rule still
			// holding under this change.
			name: "parameter named after a predeclared identifier",
			src: `package models

type string struct{ V int }
type Ctl struct{ C int }

type Box[string any] struct {
	S  string
	Ok Ctl
}

type Sibling struct {
	S string
}
`,
			want: []string{"Box.Ok => Ctl", "Sibling.S => string"},
		},
		{
			// WRAPPER FORMS crossed with the parameter axis. Every wrapper the
			// candidate collector unwraps is swept, and each row that can carry a
			// second candidate carries a NON-parameter one that must survive.
			name: "wrapper forms: the parameter is refused inside every wrapper",
			src: `package models

type T struct{ A string }
type Holder[X any] struct{ V X }

type Box[T any] struct {
	Bare T
	Ptr  *T
	Sl   []T
	Arr  [3]T
	MapT map[T]T
	MapH map[T]Holder[T]
	Ch   chan T
	Cb   func(T) error
	Wrap Holder[T]
}
`,
			// `Holder` survives inside map value, generic argument and bare
			// positions; `T` survives nowhere.
			want: []string{"Box.MapH => Holder", "Box.Wrap => Holder"},
		},
		{
			// SCOPE REACH. A type parameter shadows inside ITS OWN declaration
			// only. `Sibling.Val` and `Sibling.Ptr` must still bind to the
			// package-scope `T`, or the fix has swung into file-global refusal —
			// the silent-deletion direction again, now at file scale.
			name: "scope reach: a sibling declaration still binds the same name",
			src: `package models

type T struct{ A string }

type Box[T any] struct {
	Item T
}

type Sibling struct {
	Val T
	Ptr *T
}
`,
			want: []string{"Sibling.Ptr => T", "Sibling.Val => T"},
		},
		{
			// DECLARING FORM: generic INTERFACE. No field entity is emitted for an
			// interface method, so there is no anchor to over-fire from; the
			// non-generic struct in the same file proves the file is not inert.
			name: "declaring form: generic interface emits no field edge",
			src: `package models

type T struct{ A string }
type Ctl struct{ C string }

type Iface[T any] interface {
	Get() T
	Ctl() Ctl
}

type Use struct {
	Ok  Ctl
	Val T
}
`,
			want: []string{"Use.Ok => Ctl", "Use.Val => T"},
		},
		{
			// DECLARING FORM: generic FUNCTION. Its `[T Order]` binds nothing this
			// pass anchors on, and — the direction that matters — it must not leak
			// into the file, so `Use.Val` still binds to the package-scope `T`.
			name: "declaring form: generic function does not shadow the file",
			src: `package models

type T struct{ A string }
type Order struct{ B string }

func Gen[T Order](x T) T { return x }

type Use struct {
	Val T
	Ord Order
}
`,
			want: []string{"Use.Ord => Order", "Use.Val => T"},
		},
		{
			// DECLARING FORM: method on a GENERIC RECEIVER. The receiver's `[T]` is
			// a type_arguments list, not a type_parameter_list; it must shadow
			// nothing outside `Box`.
			name: "declaring form: method on a generic receiver",
			src: `package models

type T struct{ A string }

type Box[T any] struct {
	Item T
}

func (b Box[T]) Do(v T) T { return v }

type Use struct {
	Val T
}
`,
			want: []string{"Use.Val => T"},
		},
		{
			// CONTROL: a generic declaration whose parameter collides with
			// NOTHING. Nothing about this row changes under the fix, and without
			// it the table cannot distinguish "type parameters are refused" from
			// "generic declarations emit no edges at all".
			name: "control: generic struct with no collision keeps its edges",
			src: `package models

type Ctl struct{ C string }
type Holder[X any] struct{ V X }

type Box[T any] struct {
	Item T
	Ok   Ctl
	Wrap Holder[Ctl]
}
`,
			want: []string{"Box.Ok => Ctl", "Box.Wrap => Ctl", "Box.Wrap => Holder"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recs := goFTOne(t, tc.src)
			// Premise: the fixture really produced field entities. Otherwise
			// every "absent" row is vacuous for a reason unrelated to the fix.
			fields := 0
			for i := range recs {
				if recs[i].Kind == "SCOPE.Schema" && recs[i].Subtype == "field" {
					fields++
				}
			}
			if fields == 0 {
				t.Fatal("fixture produced NO field entities; the expected set is vacuous")
			}
			got := goFT7041Edges(recs)
			want := append([]string(nil), tc.want...)
			sort.Strings(want)
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Errorf("field→declared-type edges mismatch\n got:\n  %s\nwant:\n  %s",
					strings.Join(got, "\n  "), strings.Join(want, "\n  "))
			}
		})
	}
}

// TestGoFieldTypeRefs_7041_EmbeddedFieldAndDependsOnAreUntouched closes the
// ANCHOR axis the table above holds constant, and states the boundary of this
// change as a graded fact rather than as prose.
//
// A Go struct body offers three things a type parameter could reach: a NAMED
// field (this arm's edge), an EMBEDDED field (an EXTENDS edge from the owner,
// struct_fields.go) and the struct-anchored DEPENDS_ON
// (extractStructFieldDependencies). This change touches only the first, exactly
// as arm D's header block rules for qualified names: the two sibling edges are
// resolved by a bare name against knownTypeNames, and re-pointing them is a
// separate blast radius, not a rider on this one.
//
// Embedding a type parameter is not legal Go ("embedded field type cannot be a
// type parameter"), so the embedded row here uses a REAL same-file type to show
// EXTENDS is unchanged. The DEPENDS_ON row uses the type parameter and asserts
// the KNOWN, unchanged over-fire — with a t.Error if it ever changes, so this
// test is a real pin and not the non-grading kind this change deleted.
func TestGoFieldTypeRefs_7041_EmbeddedFieldAndDependsOnAreUntouched(t *testing.T) {
	const src = `package models

type T struct{ Unrelated string }
type Base struct{ ID string }

type Box[T any] struct {
	Base
	Item T
}
`
	recs := goFTOne(t, src)

	// 1. The field→declared-type edge is refused (this arm).
	if got := goFT7041Edges(recs); len(got) != 0 {
		t.Errorf("field→declared-type edges = %v, want none", got)
	}

	var extends, dependsOn []string
	for i := range recs {
		if recs[i].Name != "Box" {
			continue
		}
		for _, r := range recs[i].Relationships {
			switch r.Kind {
			case "EXTENDS":
				extends = append(extends, r.ToID)
			case "DEPENDS_ON":
				dependsOn = append(dependsOn, r.ToID)
			}
		}
	}
	sort.Strings(extends)
	sort.Strings(dependsOn)

	// 2. The embedded field still produces its EXTENDS edge. Unchanged.
	if strings.Join(extends, ",") != "Base" {
		t.Errorf("Box EXTENDS = %v, want [Base] — this arm must not move the "+
			"embedded-field edge", extends)
	}

	// 3. The struct-anchored DEPENDS_ON still over-fires on the type parameter.
	//    DELIBERATELY out of scope, and asserted so the boundary is observable:
	//    if a later change narrows extractStructFieldDependencies, this row goes
	//    RED and says so rather than the narrowing landing unnoticed.
	//    `Base` is in the same list because the embedded field feeds that same
	//    pass; the row is asserted as the pass's FULL output rather than as a
	//    membership test, so a narrowing in EITHER direction shows up here.
	if strings.Join(dependsOn, ",") != "Base,T" {
		t.Errorf("Box DEPENDS_ON = %v, want [Base T]. The struct-anchored DEPENDS_ON is "+
			"OUT OF SCOPE for #7041 and still resolves a type parameter against "+
			"knownTypeNames. If you have just narrowed it, that is a real fix — "+
			"update this assertion and say so.", dependsOn)
	}
}
