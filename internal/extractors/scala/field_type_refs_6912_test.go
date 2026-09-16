package scala_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// field_type_refs_6912_test.go — behavioural grading for the Scala arm of
// #6912. Every claim the production header makes is asserted here against a
// REAL parse; nothing is graded against a hand-built record set except the three
// same-file conjuncts, which Extract cannot reach (see
// field_type_refs_units_6912_test.go).

// scFieldTypeEdges returns "<field> -> <target_type>" for every
// field→declared-type edge in a record set, sorted.
func scFieldTypeEdges(ents []types.EntityRecord) []string {
	var out []string
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Kind != "REFERENCES" || r.Properties.Get("ref_kind") != "field_target_type" {
				continue
			}
			out = append(out, ents[i].Name+" -> "+r.Properties.Get("target_type"))
		}
	}
	sort.Strings(out)
	return out
}

// scEdgeToIDs returns the raw ToIDs of the field→declared-type edges.
func scEdgeToIDs(ents []types.EntityRecord) []string {
	var out []string
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Kind == "REFERENCES" && r.Properties.Get("ref_kind") == "field_target_type" {
				out = append(out, r.ToID)
			}
		}
	}
	sort.Strings(out)
	return out
}

func scWantEdges(t *testing.T, src string, want ...string) []types.EntityRecord {
	t.Helper()
	ents := runScala(t, src)
	got := scFieldTypeEdges(ents)
	sort.Strings(want)
	if want == nil {
		want = []string{}
	}
	if len(got) != len(want) {
		t.Fatalf("edges = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("edges = %v, want %v", got, want)
		}
	}
	return ents
}

// scRecord finds a record by (name, kind); nil when absent.
func scRecord(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

// scCountRecords counts records matching (name, kind).
func scCountRecords(ents []types.EntityRecord, name, kind string) int {
	n := 0
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// The premise. #6912 asserted "java retains NO type string" and it was FALSE,
// so Scala's absence is re-established here rather than inherited — and pinned,
// so a future producer that starts writing a `field_type` property has to come
// past this test and decide whether this arm should then read it.
// ---------------------------------------------------------------------------

// TestScalaFieldTypeRefs_PremiseFieldRecordsCarryNoTypeDatum pins the gap this
// arm closes: a Scala field record has no Properties, no Signature, and after
// the attach pass no Metadata either. The edge is the ONLY thing that changed.
func TestScalaFieldTypeRefs_PremiseFieldRecordsCarryNoTypeDatum(t *testing.T) {
	src := `class Repo
class Svc(val repo: Repo) {
  val a: Repo = null
  val b: Int = 0
}
`
	ents := runScala(t, src)
	fields := 0
	for i := range ents {
		e := &ents[i]
		if e.Kind != "SCOPE.Schema" || e.Subtype != "field" {
			continue
		}
		fields++
		if e.Signature != "" {
			t.Errorf("%s: Signature = %q, want empty — the premise moved", e.Name, e.Signature)
		}
		if len(e.Properties) != 0 {
			t.Errorf("%s: Properties = %v, want none — the premise moved", e.Name, e.Properties)
		}
		if e.Metadata != nil {
			t.Errorf("%s: Metadata = %v, want nil — the capture leaked", e.Name, e.Metadata)
		}
	}
	if fields != 3 {
		t.Fatalf("got %d field records, want 3 — the premise is measured over nothing", fields)
	}
}

// TestScalaFieldTypeRefs_CaptureNeverLeaksIntoTheGraph asserts the scratch key
// itself is gone from EVERY record, not only from the ones that produced an
// edge. A capture that shipped alone would be one more unhashed property that
// nothing reads — the exact state #6912 exists to fix.
func TestScalaFieldTypeRefs_CaptureNeverLeaksIntoTheGraph(t *testing.T) {
	src := `class Repo
class Svc {
  val a: Repo = null
  val b: Int = 0
  val c = new Repo()
}
`
	ents := runScala(t, src)
	for i := range ents {
		for k := range ents[i].Metadata {
			if strings.HasPrefix(k, "field_type_refs") {
				t.Fatalf("%s: metadata key %q survived the attach pass", ents[i].Name, k)
			}
		}
	}
	// Non-vacuity: an edge WAS produced, so the pass ran.
	if got := scFieldTypeEdges(ents); len(got) != 1 {
		t.Fatalf("edges = %v, want exactly 1 — the leak check graded nothing", got)
	}
}

// ---------------------------------------------------------------------------
// The type-expression shape space, ENUMERATED. Arm I's template-parameter
// refusal was wrong in BOTH directions and only enumeration found it; three
// rounds of hand-picked attacks on earlier arms each missed something.
//
// Every row declares `class Repo` and `trait Named` in the same file, so any
// candidate the scanner yields for those names becomes a visible edge and any
// candidate it drops is a visible absence. Both directions are graded by the
// same table.
// ---------------------------------------------------------------------------

func TestScalaFieldTypeRefs_TypeExpressionShapeSpace(t *testing.T) {
	rows := []struct {
		label string
		decl  string // the text after `val x`
		want  []string
	}{
		{"bare type_identifier", ": Repo = null", []string{"Repo"}},
		{"generic_type", ": Option[Repo] = null", []string{"Repo"}},
		{"generic_type, both args declared", ": Map[Repo, Named] = null", []string{"Repo", "Named"}},
		{"nested generic_type", ": List[List[Repo]] = null", []string{"Repo"}},
		{"wildcard type argument", ": List[_] = null", nil},
		{"compound_type (with)", ": Repo with Named = null", []string{"Repo", "Named"}},
		{"infix_type (Scala 3 &)", ": Repo & Named = null", []string{"Repo", "Named"}},
		{"tuple_type", ": (Repo, Named) = null", []string{"Repo", "Named"}},
		{"function_type", ": Repo => Named = null", []string{"Repo", "Named"}},
		{"annotated_type keeps only the TYPE", ": Repo @unchecked = null", []string{"Repo"}},
		{"dotted stable_type_identifier", ": com.acme.Repo = null", nil},
		{"_root_-prefixed dotted", ": _root_.com.acme.Repo = null", nil},
		{"type projection off a same-file object", ": Registry.Repo = null", nil},
		{"singleton_type", ": Registry.type = Registry", nil},
		{"primitive", ": Int = 0", nil},
		{"undeclared type", ": Elsewhere = null", nil},
		{"INFERRED — no colon at all", " = new Repo()", nil},
	}
	for _, row := range rows {
		t.Run(row.label, func(t *testing.T) {
			src := "class Repo\ntrait Named\nobject Registry\n" +
				"class Holder {\n  val x" + row.decl + "\n}\n"
			ents := runScala(t, src)
			var want []string
			for _, w := range row.want {
				want = append(want, "Holder.x -> "+w)
			}
			got := scFieldTypeEdges(ents)
			sort.Strings(want)
			if len(got) != len(want) {
				t.Fatalf("edges = %v, want %v", got, want)
			}
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("edges = %v, want %v", got, want)
				}
			}
		})
	}
}

// TestScalaFieldTypeRefs_ShapeSpaceHasAPositiveControlPerRefusal is the
// over-refusal guard. A refusal row in the table above is indistinguishable
// from a row the GRAMMAR never produced — the parse could have degraded and the
// zero would look like a correct refusal. Each refusal that names a same-file
// type is therefore paired with a row that differs ONLY in the refused
// dimension and DOES emit.
func TestScalaFieldTypeRefs_ShapeSpaceHasAPositiveControlPerRefusal(t *testing.T) {
	pairs := []struct {
		label    string
		refused  string
		admitted string
	}{
		{"qualifier", ": com.acme.Repo = null", ": Repo = null"},
		{"projection", ": Registry.Repo = null", ": Repo = null"},
		{"singleton", ": Registry.type = Registry", ": Repo = null"},
		{"inference", " = new Repo()", ": Repo = new Repo()"},
	}
	for _, p := range pairs {
		t.Run(p.label, func(t *testing.T) {
			pre := "class Repo\nobject Registry\nclass Holder {\n  val x"
			if got := scFieldTypeEdges(runScala(t, pre+p.refused+"\n}\n")); len(got) != 0 {
				t.Fatalf("refused row emitted %v", got)
			}
			got := scFieldTypeEdges(runScala(t, pre+p.admitted+"\n}\n"))
			if len(got) != 1 || got[0] != "Holder.x -> Repo" {
				t.Fatalf("control row emitted %v, want [Holder.x -> Repo] — the "+
					"refusal above graded a parse failure, not a refusal", got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The lossy/adjacent-capture traps (#7047). Each is excluded STRUCTURALLY, and
// each is graded with a control that differs only in the excluded position.
// ---------------------------------------------------------------------------

// TestScalaFieldTypeRefs_InitializerIsNeverTheDeclaredType is the Scala
// instance of the shape that cost arm G half its edges and arm I 16 of 39: the
// initializer lives INSIDE the val_definition and holds a type_identifier that
// names a real same-file class.
func TestScalaFieldTypeRefs_InitializerIsNeverTheDeclaredType(t *testing.T) {
	// Premise first: `new Repo()` really does put a type_identifier in this
	// node — otherwise the refusal is vacuous. The control row proves the
	// scanner reaches the node at all.
	scWantEdges(t, "class Repo\nclass Other\nclass Holder {\n  val x = new Repo()\n}\n")
	scWantEdges(t, "class Repo\nclass Other\nclass Holder {\n  val x: Other = new Repo()\n}\n",
		"Holder.x -> Other")
}

// TestScalaFieldTypeRefs_ClassParameterDefaultIsNeverTheDeclaredType grades the
// other half of the window: everything after "=" is a default VALUE.
func TestScalaFieldTypeRefs_ClassParameterDefaultIsNeverTheDeclaredType(t *testing.T) {
	src := `class Repo
class Other
case class Holder(a: Other = new Repo())
`
	scWantEdges(t, src, "Holder.a -> Other")
}

// TestScalaFieldTypeRefs_AnnotationNameIsNeverATarget is Scala's OWN trap and
// the over-collection direction: an annotation's name is a `type_identifier`
// sitting inside the declared type's own subtree, and a Scala annotation IS a
// class — so the wrong edge would bind perfectly.
func TestScalaFieldTypeRefs_AnnotationNameIsNeverATarget(t *testing.T) {
	src := `class Repo
class unchecked
class Holder {
  val x: Repo @unchecked = null
}
`
	// `class unchecked` is declared in the file precisely so a leaked
	// annotation name would become a VISIBLE edge rather than a silent drop.
	ents := scWantEdges(t, src, "Holder.x -> Repo")
	if scRecord(ents, "unchecked", "SCOPE.Component") == nil {
		t.Fatal("premise gone: no same-file `class unchecked`, so the refusal " +
			"could not have been observed")
	}
}

// TestScalaFieldTypeRefs_ModifierAnnotationIsNeverATarget grades the OTHER
// annotation position — before the colon, which the window excludes rather than
// the annotation skip.
func TestScalaFieldTypeRefs_ModifierAnnotationIsNeverATarget(t *testing.T) {
	src := `class Repo
class Inject
class Holder {
  @Inject val x: Repo = null
}
`
	ents := scWantEdges(t, src, "Holder.x -> Repo")
	if scRecord(ents, "Inject", "SCOPE.Component") == nil {
		t.Fatal("premise gone: no same-file `class Inject`")
	}
}

// TestScalaFieldTypeRefs_TypeParameterNeverShadowsASameFileClass grades the
// over-fire arm F had to pin as KNOWN-WRONG for Java. Scala's grammar has the
// same collision and the declaration's type_parameters list is in hand, so it
// is refused instead of documented. The control row differs ONLY in the type
// parameter's name.
//
// THIS TEST IS NOT THE GRADING — it varies the parameter's NAME and the ANCHOR
// and holds the parameter's DECLARATION FORM constant at bare-invariant, and
// holding that axis is what let `[+A]` / `[-A]` ship as a binding wrong edge.
// The declaration-form space and the NESTING scope are enumerated from the
// grammar and graded in both directions in
// field_type_refs_typeparams_6912_test.go; this file keeps the two axes it does
// vary, and nothing more is claimed for it.
func TestScalaFieldTypeRefs_TypeParameterNeverShadowsASameFileClass(t *testing.T) {
	scWantEdges(t, "class T\ncase class Holder[T](item: T)\n")
	scWantEdges(t, "class T\ncase class Holder[U](item: T)\n", "Holder.item -> T")
	// Also on the template-body anchor, which reaches the parameter set by a
	// different argument.
	scWantEdges(t, "class T\nclass Holder[T] {\n  val x: T = null\n}\n")
	scWantEdges(t, "class T\nclass Holder[U] {\n  val x: T = null\n}\n", "Holder.x -> T")
}

// ---------------------------------------------------------------------------
// The NOT-A-DECLARATION AUDIT. A 100% bind rate is not evidence: #7056
// established that no instrument we own can see an edge that binds to a
// non-declaration. Every same-file, same-name carrier Scala can mint is
// enumerated, and each refusal asserts the carrier EXISTS so it cannot pass
// vacuously.
// ---------------------------------------------------------------------------

// TestScalaFieldTypeRefs_ImportPlaceholderIsNeverATarget is the load-bearing
// one, and Scala reaches it more easily than any prior arm: buildImports names
// the placeholder after the FIRST dotted segment, so `import Order.Status`
// mints a bare-named SCOPE.Component called `Order` in this file.
func TestScalaFieldTypeRefs_ImportPlaceholderIsNeverATarget(t *testing.T) {
	src := `import Order.Status

class Svc {
  val o: Order = null
}
`
	ents := scWantEdges(t, src)
	ph := scRecord(ents, "Order", "SCOPE.Component")
	if ph == nil {
		t.Fatal("premise gone: buildImports no longer mints a bare-named " +
			"placeholder, so this test refuses nothing")
	}
	if ph.Subtype != "" {
		t.Fatalf("premise moved: import placeholder subtype = %q, want empty — "+
			"the allow-list may no longer be what refuses it", ph.Subtype)
	}
}

// TestScalaFieldTypeRefs_ImportPlaceholderBesideARealClassStillBinds is the
// mirror. The refusal must not cost the edge when a REAL declaration of that
// name is also in the file — the over-refusal direction, which has no symptom.
func TestScalaFieldTypeRefs_ImportPlaceholderBesideARealClassStillBinds(t *testing.T) {
	src := `import Order.Status

class Order
class Svc {
  val o: Order = null
}
`
	ents := scWantEdges(t, src, "Svc.o -> Order")
	if n := scCountRecords(ents, "Order", "SCOPE.Component"); n != 2 {
		t.Fatalf("premise gone: %d SCOPE.Component records named Order, want 2 "+
			"(the import placeholder and the class)", n)
	}
}

// TestScalaFieldTypeRefs_ObjectIsNeverATarget grades the refusal of `object`,
// which declares a TERM rather than a type.
//
// THE FIXTURE IS DELIBERATELY NOT LEGAL SCALA, and that is said out loud rather
// than quietly asserted: `val r: Registry` does not compile when `Registry` is
// only an object — `Registry.type` is the type, and singleton_type is refused by
// the candidate scanner. There is no legal input that distinguishes this
// refusal, which is exactly why it must be graded at the only place it IS
// distinguishable. The legal, reachable shape is the companion below, where the
// refusal costs nothing because the class supplies the identical ToID.
func TestScalaFieldTypeRefs_ObjectIsNeverATarget(t *testing.T) {
	src := `object Registry

class Svc {
  val r: Registry = null
}
`
	ents := scWantEdges(t, src)
	obj := scRecord(ents, "Registry", "SCOPE.Component")
	if obj == nil || obj.Subtype != "object" {
		t.Fatalf("premise gone: no SCOPE.Component/object named Registry (%v)", obj)
	}
}

// TestScalaFieldTypeRefs_FileCarrierIsNeverATarget pins the #577 file entity and
// the #501 twirl entity out of the target set. Both are SCOPE.Component and both
// are named after the FILE, so neither can be reached by a type name — the claim
// is made at that strength and no stronger, and the carrier's existence and
// subtype are asserted so a future rename cannot silently make it reachable.
func TestScalaFieldTypeRefs_FileCarrierIsNeverATarget(t *testing.T) {
	ents := runScala(t, "class Repo\n")
	fe := scRecord(ents, "Test.scala", "SCOPE.Component")
	if fe == nil || fe.Subtype != "file" {
		t.Fatalf("premise gone: no SCOPE.Component/file carrier (%v)", fe)
	}
	if strings.Contains(strings.Join(scEdgeToIDs(ents), " "), "Test.scala:Test.scala") {
		t.Fatal("the file carrier became a target")
	}
}

// TestScalaFieldTypeRefs_Scala3EnumMintsNoComponentSoItIsNoTarget records a
// RECALL FLOOR as a fact rather than as prose: walkNode has no
// `enum_definition` case, so a Scala 3 enum has no SCOPE.Component at all and a
// field typed by it gets no edge. Stated here so the floor is discovered by a
// failing test if someone adds that case, not by a corpus surprise.
func TestScalaFieldTypeRefs_Scala3EnumMintsNoComponentSoItIsNoTarget(t *testing.T) {
	src := `enum Color { case Red, Green }

class Svc {
  val c: Color = null
}
`
	ents := scWantEdges(t, src)
	if scRecord(ents, "Color", "SCOPE.Enum") == nil {
		t.Fatal("premise gone: no SCOPE.Enum value-set for the enum")
	}
	if c := scRecord(ents, "Color", "SCOPE.Component"); c != nil {
		t.Fatalf("the floor moved: a SCOPE.Component named Color now exists (%v) "+
			"— this arm should admit it", c)
	}
}

// TestScalaFieldTypeRefs_AbstractValMintsNoFieldSoItIsNoAnchor records the other
// recall floor on the SOURCE endpoint: `val abs: Repo` in a trait parses as
// `val_declaration`, which emitContainerWithMembers does not intercept, so no
// field entity exists to anchor an edge on. Arm I found 68 of 254 "fields" were
// actually member functions; the Scala audit of the source endpoint is this.
func TestScalaFieldTypeRefs_AbstractValMintsNoFieldSoItIsNoAnchor(t *testing.T) {
	src := `class Repo
trait Abstract {
  val abs: Repo
  def d: Repo
}
class Concrete {
  val c: Repo = null
}
`
	ents := scWantEdges(t, src, "Concrete.c -> Repo")
	if f := scRecord(ents, "Abstract.abs", "SCOPE.Schema"); f != nil {
		t.Fatalf("the floor moved: an abstract val now mints a field (%v) — this "+
			"arm should capture its type too", f)
	}
	// The `def d: Repo` row is the source-endpoint audit: a Scala method is a
	// SCOPE.Operation and can never be a field anchor, so a return type can
	// never be captured as a declared type.
	if op := scRecord(ents, "d", "SCOPE.Operation"); op == nil {
		t.Fatal("premise gone: `def d: Repo` no longer mints an Operation")
	}
}

// TestScalaFieldTypeRefs_SecondParameterListMintsNoField pins the last recall
// floor: emitScalaCaseClassFields breaks after the FIRST class_parameters block,
// so an implicit/using parameter list produces no field.
func TestScalaFieldTypeRefs_SecondParameterListMintsNoField(t *testing.T) {
	src := `class Repo
trait Named
case class Multi(a: Repo)(implicit b: Named)
`
	scWantEdges(t, src, "Multi.a -> Repo")
}

// ---------------------------------------------------------------------------
// The ambiguity rule: count the KINDS the tier that resolves THIS address
// weighs. Scala has exactly one admissible target kind, so that tier is
// lookupLocationKind under componentKindFamily and the count is over family
// kinds. Each rival Scala can mint is graded on its own, with its existence
// asserted first.
// ---------------------------------------------------------------------------

// TestScalaFieldTypeRefs_CompanionObjectIsOneNodeNotAnAmbiguity is the Scala
// mirror of arm I's constructor, on the single most idiomatic shape in the
// language. `case class Order` + `object Order` are TWO records — and ONE graph
// node, because graph.EntityID excludes Subtype. Arm C's record count (#7038)
// would delete this edge.
func TestScalaFieldTypeRefs_CompanionObjectIsOneNodeNotAnAmbiguity(t *testing.T) {
	src := `case class Order(id: Int)
object Order { def zero = 0 }

class Svc {
  val o: Order = null
}
`
	ents := scWantEdges(t, src, "Svc.o -> Order")
	if n := scCountRecords(ents, "Order", "SCOPE.Component"); n != 2 {
		t.Fatalf("premise gone: %d SCOPE.Component records named Order, want 2 "+
			"— the record-vs-kind distinction is graded over nothing", n)
	}
}

// TestScalaFieldTypeRefs_SealedTraitEnumTwinDoesNotBlockTheTrait grades the
// rival arm D's all-kinds rule would refuse. constantset.go mints a SCOPE.Enum
// value-set named after the sealed trait, in the same file.
func TestScalaFieldTypeRefs_SealedTraitEnumTwinDoesNotBlockTheTrait(t *testing.T) {
	src := `sealed trait Shape
case object Circle extends Shape
case object Square extends Shape

class Svc {
  val s: Shape = null
}
`
	ents := scWantEdges(t, src, "Svc.s -> Shape")
	if scRecord(ents, "Shape", "SCOPE.Enum") == nil {
		t.Fatal("premise gone: no SCOPE.Enum value-set named Shape — arm D's " +
			"counterfactual is graded over nothing here")
	}
}

// TestScalaFieldTypeRefs_ObjectConstGroupTwinDoesNotBlockTheTrait grades the
// SECOND out-of-family rival: a const-group value-set named after the OBJECT,
// beside a companion trait of the same name.
func TestScalaFieldTypeRefs_ObjectConstGroupTwinDoesNotBlockTheTrait(t *testing.T) {
	src := `trait Status
object Status {
  val Open = "open"
  val Closed = "closed"
  val Held = "held"
}

class Svc {
  val s: Status = null
}
`
	ents := scWantEdges(t, src, "Svc.s -> Status")
	if scRecord(ents, "Status", "SCOPE.Enum") == nil {
		t.Fatal("premise gone: no const-group SCOPE.Enum named Status")
	}
}

// TestScalaFieldTypeRefs_SameNamedOperationDoesNotBlockTheClass grades the third
// out-of-family rival, and it is the one arm D's rule gets wrong for a LANGUAGE
// reason: a Scala factory `def Order(...)` beside `class Order` is legal,
// idiomatic and not ambiguous at all.
func TestScalaFieldTypeRefs_SameNamedOperationDoesNotBlockTheClass(t *testing.T) {
	src := `class Order
object F {
  def Order(): Int = 1
}

class Svc {
  val o: Order = null
}
`
	ents := scWantEdges(t, src, "Svc.o -> Order")
	if scRecord(ents, "Order", "SCOPE.Operation") == nil {
		t.Fatal("premise gone: no SCOPE.Operation named Order")
	}
}

// ---------------------------------------------------------------------------
// Emission shape.
// ---------------------------------------------------------------------------

// TestScalaFieldTypeRefs_EdgeShapeAndProperties pins the address, the kind and
// all three properties against INDEPENDENT literals — not against the package's
// own constants, which is what makes arm D's CZ-2 finding hold (four
// independent literals enforce the cross-arm vocabulary transitively).
func TestScalaFieldTypeRefs_EdgeShapeAndProperties(t *testing.T) {
	src := `class Repo
class Svc {
  val myField: Repo = null
}
`
	ents := runScala(t, src)
	f := scRecord(ents, "Svc.myField", "SCOPE.Schema")
	if f == nil {
		t.Fatal("no field record")
	}
	var edges []types.RelationshipRecord
	for _, r := range f.Relationships {
		if r.Kind == "REFERENCES" {
			edges = append(edges, r)
		}
	}
	if len(edges) != 1 {
		t.Fatalf("got %d REFERENCES edges, want 1", len(edges))
	}
	e := edges[0]
	if e.ToID != "scope:component:class:scala:Test.scala:Repo" {
		t.Errorf("ToID = %q", e.ToID)
	}
	if e.FromID != "" {
		t.Errorf("FromID = %q, want empty so assembly anchors on the field", e.FromID)
	}
	if got := e.Properties.Get("ref_kind"); got != "field_target_type" {
		t.Errorf("ref_kind = %q, want field_target_type", got)
	}
	if got := e.Properties.Get("field_name"); got != "myField" {
		t.Errorf("field_name = %q, want the LEAF name myField", got)
	}
	if got := e.Properties.Get("target_type"); got != "Repo" {
		t.Errorf("target_type = %q", got)
	}
	// The attach pass runs BEFORE extractor.TagRelationshipsLanguage, so the
	// edge carries the language tag every other Scala edge carries. That is the
	// one direction in which the call site's PLACEMENT is load-bearing, and it
	// is asserted rather than left to the ordering comment.
	if got := e.Properties.Get("language"); got != "scala" {
		t.Errorf("language = %q, want scala — the attach pass moved after "+
			"TagRelationshipsLanguage", got)
	}
}

// TestScalaFieldTypeRefs_TargetTypeIsNotAlwaysTheDeclaredTypeText is the
// independent pin arm B's review demanded: on a wrapped type, `target_type` is
// the BOUND name, which differs from the declared type text.
func TestScalaFieldTypeRefs_TargetTypeIsNotAlwaysTheDeclaredTypeText(t *testing.T) {
	src := `class Repo
class Svc {
  val wrapped: Option[Repo] = None
}
`
	ents := runScala(t, src)
	f := scRecord(ents, "Svc.wrapped", "SCOPE.Schema")
	if f == nil || len(f.Relationships) != 1 {
		t.Fatalf("want exactly one edge, got %v", f)
	}
	if got := f.Relationships[0].Properties.Get("target_type"); got != "Repo" {
		t.Fatalf("target_type = %q, want Repo (NOT Option[Repo])", got)
	}
}

// TestScalaFieldTypeRefs_DuplicateCandidatesEmitOneEdge pins the dedup, which
// is per-ToID rather than per-name.
func TestScalaFieldTypeRefs_DuplicateCandidatesEmitOneEdge(t *testing.T) {
	scWantEdges(t, "class Repo\nclass Svc {\n  val p: (Repo, Repo) = null\n}\n",
		"Svc.p -> Repo")
}

// TestScalaFieldTypeRefs_SelfReferenceIsEmitted states the decision rather than
// leaving it implicit: Scala ships no container-anchored declared-type edge, so
// there is nothing for a self edge to restate.
func TestScalaFieldTypeRefs_SelfReferenceIsEmitted(t *testing.T) {
	scWantEdges(t, "case class Node(next: Option[Node])", "Node.next -> Node")
}

// TestScalaFieldTypeRefs_BothAnchorsProduceEdges grades the two emit sites
// together — a template-body val and a case-class parameter — since each was
// wired separately and one could regress alone.
func TestScalaFieldTypeRefs_BothAnchorsProduceEdges(t *testing.T) {
	src := `class Repo
trait Named
case class Holder(p: Repo) {
  val v: Named = null
}
`
	scWantEdges(t, src, "Holder.p -> Repo", "Holder.v -> Named")
}

// TestScalaFieldTypeRefs_RelationshipsAreAppendedNotAssigned pins the append.
// A Scala field record carries no outbound edge today, so the only way to grade
// this through Extract is to give the field one FIRST — which nothing does — so
// it is graded at the call site in the units file instead. What IS gradable
// here is that the container's pre-existing CONTAINS edges survive the pass.
func TestScalaFieldTypeRefs_ContainerEdgesSurviveThePass(t *testing.T) {
	src := `class Repo
class Svc {
  val a: Repo = null
  def m(): Int = 1
}
`
	ents := runScala(t, src)
	svc := scRecord(ents, "Svc", "SCOPE.Component")
	if svc == nil {
		t.Fatal("no Svc")
	}
	contains := 0
	for _, r := range svc.Relationships {
		if r.Kind == "CONTAINS" {
			contains++
		}
	}
	if contains != 2 {
		t.Fatalf("CONTAINS = %d, want 2 (the field and the method)", contains)
	}
}

// TestScalaFieldTypeRefs_ForwardDeclarationOrderDoesNotMatter pins the reason
// the capture is deferred to an attach pass rather than emitted inline.
func TestScalaFieldTypeRefs_ForwardDeclarationOrderDoesNotMatter(t *testing.T) {
	scWantEdges(t, "class Svc {\n  val r: Repo = null\n}\nclass Repo\n", "Svc.r -> Repo")
}
