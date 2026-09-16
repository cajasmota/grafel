package scala_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// field_type_refs_scope_6912_test.go — the resolver gate for the Scala arm.
//
// A non-binding edge is WORSE than no edge: an unresolved stub is kept verbatim,
// dangles, and is classified `bug-extractor`. Reading refs.go is not evidence —
// arms D, F, G, H and J each DROVE it, and two of them found the rule they had
// reasoned to was wrong.

// scResolveFieldTypeEdges drives the REAL resolver over an extracted record set
// and returns (bound, dangling) for field→declared-type edges, plus the entity
// ID each bound edge actually landed on.
func scResolveFieldTypeEdges(t *testing.T, recs []types.EntityRecord) (bound, dangling int, boundTo []string) {
	t.Helper()
	for i := range recs {
		if recs[i].ID == "" {
			recs[i].ID = graph.EntityID("issue6912", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
		}
	}
	idx := resolve.BuildIndex(recs)
	ids := map[string]bool{}
	for i := range recs {
		ids[recs[i].ID] = true
	}
	resolve.ReferencesEmbedded(recs, idx)
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "REFERENCES" || r.Properties.Get("ref_kind") != "field_target_type" {
				continue
			}
			if ids[r.ToID] {
				bound++
				boundTo = append(boundTo, r.ToID)
			} else {
				t.Logf("DANGLING: %s -> %s", recs[i].Name, r.ToID)
				dangling++
			}
		}
	}
	return bound, dangling, boundTo
}

// scHandEmit appends the edge the pass may have declined, so a refusal can be
// driven through the resolver instead of merely justified in a comment.
func scHandEmit(recs []types.EntityRecord, fieldName, target string) {
	for i := range recs {
		if recs[i].Name != fieldName {
			continue
		}
		recs[i].Relationships = append(recs[i].Relationships, types.RelationshipRecord{
			ToID: "scope:component:class:scala:Test.scala:" + target,
			Kind: "REFERENCES",
			Properties: types.Props{
				{K: "ref_kind", V: "field_target_type"},
			},
		})
	}
}

// scEntityID recomputes the ID the drive helper stamps, so a bound edge can be
// checked against the record it landed on.
func scEntityID(kind, name string) string {
	return graph.EntityID("issue6912", kind, name, "Test.scala")
}

// TestScalaFieldTypeRefs_ResolverBindsEveryEdge is the standing gate this whole
// defect family exists because nobody had cleared: the edge is driven through
// resolve.BuildIndex → resolve.ReferencesEmbedded and must bind.
func TestScalaFieldTypeRefs_ResolverBindsEveryEdge(t *testing.T) {
	src := `class Repo
trait Named
case class Line(qty: Int)

case class Holder(p: Repo) {
  val a: Named = null
  val b: Option[Line] = None
  val c: Map[Repo, Line] = null
}
`
	recs := runScala(t, src)
	if got := scFieldTypeEdges(recs); len(got) != 5 {
		t.Fatalf("got %d edges, want 5: %v", len(got), got)
	}
	bound, dangling, _ := scResolveFieldTypeEdges(t, recs)
	if bound != 5 || dangling != 0 {
		t.Fatalf("bound=%d dangling=%d, want 5/0", bound, dangling)
	}
}

// TestScalaFieldTypeRefs_TheOnlyTierIsTheComponentFamilyTier establishes, by
// DRIVING rather than reading, the single fact the ambiguity rule rests on:
// every edge this arm emits addresses a SCOPE.Component and is answered by
// lookupLocationKind under componentKindFamily, so an out-of-family rival cannot
// blank it. Arms G and J needed a second tier only because they had a
// SCOPE.Schema/type_alias target kind; Scala's `type Alias = X` mints no entity
// at all, which is asserted here as the premise.
func TestScalaFieldTypeRefs_TheOnlyTierIsTheComponentFamilyTier(t *testing.T) {
	src := `type Alias = Repo

class Repo
class Svc {
  val a: Alias = null
  val b: Repo = null
}
`
	recs := runScala(t, src)
	if scRecord(recs, "Alias", "SCOPE.Schema") != nil ||
		scRecord(recs, "Alias", "SCOPE.Component") != nil {
		t.Fatal("premise gone: `type Alias = Repo` now mints an entity — this arm " +
			"has a second target kind and needs a second tier")
	}
	if got := scFieldTypeEdges(recs); len(got) != 1 || got[0] != "Svc.b -> Repo" {
		t.Fatalf("edges = %v, want only [Svc.b -> Repo]", got)
	}
	bound, dangling, _ := scResolveFieldTypeEdges(t, recs)
	if bound != 1 || dangling != 0 {
		t.Fatalf("bound=%d dangling=%d, want 1/0", bound, dangling)
	}
}

// TestScalaFieldTypeRefs_OutOfFamilyRivalsDoNotDangle is the measured refutation
// of arm D's rule for Scala, and it is stated in the honest direction that arm E
// had to correct three arms on: the refusal those arms justified as "emitting
// would guarantee a dangling stub" is, for an OUT-OF-FAMILY rival, simply FALSE.
// The edge binds. Counting all kinds here would be a pure recall loss.
func TestScalaFieldTypeRefs_OutOfFamilyRivalsDoNotDangle(t *testing.T) {
	for _, tc := range []struct {
		label     string
		src       string
		field     string
		rivalKind string
		rivalName string
	}{
		{
			label: "SCOPE.Enum value-set beside a sealed trait",
			src: `sealed trait Shape
case object Circle extends Shape
case object Square extends Shape
class Svc {
  val s: Shape = null
}
`,
			field: "Svc.s", rivalKind: "SCOPE.Enum", rivalName: "Shape",
		},
		{
			label: "SCOPE.Enum const-group named after the object",
			src: `trait Status
object Status {
  val Open = "open"
  val Closed = "closed"
  val Held = "held"
}
class Svc {
  val s: Status = null
}
`,
			field: "Svc.s", rivalKind: "SCOPE.Enum", rivalName: "Status",
		},
		{
			label: "SCOPE.Operation factory named after the class",
			src: `class Order
object F { def Order(): Int = 1 }
class Svc {
  val o: Order = null
}
`,
			field: "Svc.o", rivalKind: "SCOPE.Operation", rivalName: "Order",
		},
	} {
		t.Run(tc.label, func(t *testing.T) {
			recs := runScala(t, tc.src)
			if scRecord(recs, tc.rivalName, tc.rivalKind) == nil {
				t.Fatalf("premise gone: no %s named %s — the row grades nothing",
					tc.rivalKind, tc.rivalName)
			}
			bound, dangling, _ := scResolveFieldTypeEdges(t, recs)
			if bound != 1 || dangling != 0 {
				t.Fatalf("bound=%d dangling=%d, want 1/0 — arm D's all-kinds rule "+
					"would have refused this edge for no resolver reason", bound, dangling)
			}
		})
	}
}

// TestScalaFieldTypeRefs_CompanionPairResolvesToOneNode drives the record-vs-kind
// distinction (#7038) rather than asserting it from the EntityID hash: the two
// same-named Component records collapse to ONE id, so the edge binds.
func TestScalaFieldTypeRefs_CompanionPairResolvesToOneNode(t *testing.T) {
	src := `case class Order(id: Int)
object Order { def zero = 0 }
class Svc {
  val o: Order = null
}
`
	recs := runScala(t, src)
	bound, dangling, boundTo := scResolveFieldTypeEdges(t, recs)
	if bound != 1 || dangling != 0 {
		t.Fatalf("bound=%d dangling=%d, want 1/0", bound, dangling)
	}
	if want := scEntityID("SCOPE.Component", "Order"); boundTo[0] != want {
		t.Fatalf("bound to %s, want %s", boundTo[0], want)
	}
}

// TestScalaFieldTypeRefs_RefusedImportPlaceholderEdgeWOULDBIND is the sharpest
// kill on this arm, and it is the F# shape rather than the Go shape: the edge
// the allow-list refuses does NOT dangle — it binds, confidently and wrongly, to
// an import placeholder. `bug-extractor` classifies unresolved stubs, so nothing
// we own could ever have surfaced it (#7056).
//
// Scala reaches this more easily than any prior arm because buildImports names
// the placeholder after the FIRST dotted segment, so an ordinary
// `import Order.Status` is enough — no root-package import is required.
func TestScalaFieldTypeRefs_RefusedImportPlaceholderEdgeWOULDBIND(t *testing.T) {
	src := `import Order.Status

class Svc {
  val o: Order = null
}
`
	recs := runScala(t, src)
	if got := scFieldTypeEdges(recs); len(got) != 0 {
		t.Fatalf("the pass emitted %v, want none", got)
	}
	ph := scRecord(recs, "Order", "SCOPE.Component")
	if ph == nil || ph.Subtype != "" {
		t.Fatalf("premise gone: no bare-named import placeholder (%v)", ph)
	}
	scHandEmit(recs, "Svc.o", "Order")
	bound, dangling, boundTo := scResolveFieldTypeEdges(t, recs)
	if bound != 1 || dangling != 0 {
		t.Fatalf("bound=%d dangling=%d, want 1/0 — if this ever dangles, the "+
			"allow-list's stated reason (a WRONG BINDING, not a dangle) has changed",
			bound, dangling)
	}
	if want := scEntityID("SCOPE.Component", "Order"); boundTo[0] != want {
		t.Fatalf("bound to %s, want the placeholder %s", boundTo[0], want)
	}
}

// TestScalaFieldTypeRefs_RefusedObjectEdgeWOULDBIND does the same for the
// `object` refusal: it too is a wrong binding rather than a dangle, so the
// refusal is a PRECISION decision and must be graded as one.
func TestScalaFieldTypeRefs_RefusedObjectEdgeWOULDBIND(t *testing.T) {
	src := `object Registry

class Svc {
  val r: Registry = null
}
`
	recs := runScala(t, src)
	if got := scFieldTypeEdges(recs); len(got) != 0 {
		t.Fatalf("the pass emitted %v, want none", got)
	}
	scHandEmit(recs, "Svc.r", "Registry")
	bound, dangling, _ := scResolveFieldTypeEdges(t, recs)
	if bound != 1 || dangling != 0 {
		t.Fatalf("bound=%d dangling=%d, want 1/0", bound, dangling)
	}
}

// TestScalaFieldTypeRefs_InFamilyRivalWouldActuallyDangle proves the REASON for
// the ambiguity guard instead of repeating a predecessor's justification
// comment, which arm E found to be false in general.
//
// The guard is NOT reachable from Scala source alone — every entity this
// extractor mints for a type declaration is kinded SCOPE.Component, so a second
// FAMILY kind can only arrive from another producer (internal/patterns mints
// SCOPE.Model, which IS in componentKindFamily). That is stated plainly rather
// than hidden: the rival is injected, and the row is what makes the guard
// honest rather than decorative.
func TestScalaFieldTypeRefs_InFamilyRivalWouldActuallyDangle(t *testing.T) {
	src := `class Repo

class Svc {
  val r: Repo = null
}
`
	for _, tc := range []struct {
		label     string
		rivalKind string
		wantBound int
		wantDangl int
	}{
		{"no rival — control", "", 1, 0},
		{"in-family rival (SCOPE.Model)", "SCOPE.Model", 0, 1},
		{"out-of-family rival (SCOPE.Enum)", "SCOPE.Enum", 1, 0},
	} {
		t.Run(tc.label, func(t *testing.T) {
			recs := runScala(t, src)
			if tc.rivalKind != "" {
				recs = append(recs, types.EntityRecord{
					Name:       "Repo",
					Kind:       tc.rivalKind,
					Subtype:    "injected_rival",
					SourceFile: "Test.scala",
					Language:   "scala",
				})
			}
			bound, dangling, _ := scResolveFieldTypeEdges(t, recs)
			if bound != tc.wantBound || dangling != tc.wantDangl {
				t.Fatalf("bound=%d dangling=%d, want %d/%d",
					bound, dangling, tc.wantBound, tc.wantDangl)
			}
		})
	}
}

// TestScalaFieldTypeRefs_InFamilyRivalIsRefusedByThePass is the emission half of
// the row above: the guard must actually decline, and it must decline for the
// in-family rival ONLY. Graded separately because grading one half is what makes
// the other look covered.
func TestScalaFieldTypeRefs_InFamilyRivalIsRefusedByThePass(t *testing.T) {
	base := runScala(t, "class Repo\n\nclass Svc {\n  val r: Repo = null\n}\n")
	if got := scFieldTypeEdges(base); len(got) != 1 {
		t.Fatalf("control emitted %v, want 1 edge", got)
	}
	// The emission side is reached through the attach pass's own call site, so
	// the injected rival has to be present BEFORE the pass runs. See
	// field_type_refs_units_6912_test.go, which calls attachScalaFieldTypeRefs
	// directly with the rival in the slice — this test exists to name that
	// split rather than to leave it unexplained.
}

// TestScalaFieldTypeRefs_Unit_TheAmbiguityGuardCannotFireThroughExtract states
// the STRONGER fact the header now carries, and observes it instead of arguing
// it.
//
// The PR originally said only that the in-family rival's INCIDENCE is
// unmeasured. It is more than that: attachScalaFieldTypeRefs is handed the slice
// THIS extractor alone built, and this extractor mints exactly ONE Kind in the
// component address family — SCOPE.Component. So `len(famKinds[name]) > 1` can
// never be true on any input Extract can produce, and every test that scores the
// guard (including the table above and M29) scores it against a hand-built
// record set. The guard stays as a defence against a second family-Kind producer
// landing in this package; this test is what will notice the day one does.
//
// The source is deliberately broad — every container form Scala has, plus the
// constructs that mint the out-of-family rivals (a sealed trait's value-set, a
// const-group object, a def named after a class, an import placeholder) — so a
// new family-Kind producer in this package is likely to be caught by it.
func TestScalaFieldTypeRefs_Unit_TheAmbiguityGuardCannotFireThroughExtract(t *testing.T) {
	src := `import Order.Status

sealed trait Shape
case object Circle extends Shape
case object Square extends Shape

object Status {
  val Draft = "draft"
  val Sent = "sent"
}

class Order(val status: Status, val shape: Shape)
case class Money(amount: Int)
object Money {
  def Order(): Int = 1
}
trait Repo {
  val m: Money = null
}
class Node[+A](val next: Node[A], val head: A)
`
	ents := runScala(t, src)

	// Premise: something in the component family IS minted here, so the count
	// below is not vacuously satisfied by an empty record set.
	famPerName := map[string]map[string]bool{}
	total := 0
	for i := range ents {
		k := ents[i].Kind
		if !scIsComponentFamilyKind(k) || ents[i].Name == "" {
			continue
		}
		total++
		if famPerName[ents[i].Name] == nil {
			famPerName[ents[i].Name] = map[string]bool{}
		}
		famPerName[ents[i].Name][k] = true
	}
	if total < 6 {
		t.Fatalf("premise gone: only %d family-kind records minted, so this "+
			"test observes almost nothing", total)
	}
	for name, kinds := range famPerName {
		if len(kinds) > 1 {
			t.Fatalf("PREMISE MOVED: Extract now mints %d family Kinds for %q "+
				"(%v). The ambiguity guard is now REACHABLE through Extract, so "+
				"it must be graded against a real parse and the header's "+
				"'cannot fire through Extract' claim is stale", len(kinds), name, kinds)
		}
		for k := range kinds {
			if k != "SCOPE.Component" {
				t.Fatalf("PREMISE MOVED: Extract now mints family Kind %q for "+
					"%q; the header claims SCOPE.Component is the only one", k, name)
			}
		}
	}
}

// scIsComponentFamilyKind mirrors scalaComponentAddressFamily. It is spelled out
// here rather than exported from production, so the test does not agree with the
// code by construction.
func scIsComponentFamilyKind(kind string) bool {
	switch kind {
	case "Component", "Class", "View", "Model",
		"SCOPE.Component", "SCOPE.Class", "SCOPE.View", "SCOPE.Model":
		return true
	}
	return false
}
