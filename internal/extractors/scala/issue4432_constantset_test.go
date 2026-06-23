package scala_test

// issue4432_constantset_test.go — value-asserting, IN-PIPELINE tests for Scala
// CONSTANT-COLLECTION / ENUMERATION value-sets (#4432, extends #4429 / #4420 /
// epic #4419, ref #4334).
//
// #4429 indexed const collections in other languages as SCOPE.Enum value-sets.
// #4432 generalises that to the Scala source-of-truth shapes that were
// invisible to search_entities and could not be diffed by a downstream
// cross-graph parity-audit:
//
//   - object const GROUP        (object Pages { val CoreAdmin = "core-admin" })
//   - const Map literal         (val Routes = Map("home" -> "/"))
//   - Scala 3 enum              (enum Color { case Red, Green, Blue })
//   - sealed-trait enumeration  (sealed trait Status; case object Active ...)
//
// The first test runs the REAL Scala extract pipeline on a byte-copy fixture
// (testdata/issue4432/constants.scala) holding all four shapes, asserting each
// value-set is emitted, name-searchable, and that members_json enumerates the
// real {key,value} pairs with source lines. RED before the fix (none surfaced);
// GREEN after.

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/scala"
	"github.com/cajasmota/grafel/internal/types"
)

type cs4432Member struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Line  int    `json:"line"`
}

func cs4432Members(t *testing.T, e *types.EntityRecord) []cs4432Member {
	t.Helper()
	raw := e.Properties["members_json"]
	if raw == "" {
		t.Fatalf("entity %q has no members_json property; props=%v", e.Name, e.Properties)
	}
	var got []cs4432Member
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("members_json not valid JSON: %v (raw=%s)", err, raw)
	}
	return got
}

func cs4432Value(members []cs4432Member, key string) (string, bool) {
	for _, m := range members {
		if m.Key == key {
			return m.Value, true
		}
	}
	return "", false
}

// searchableScalaEnum mirrors how search_entities locates a value-set: by Kind
// + name. nil means the node would not be findable.
func searchableScalaEnum(recs []types.EntityRecord, name string) *types.EntityRecord {
	for i := range recs {
		if recs[i].Kind == "SCOPE.Enum" && recs[i].Name == name {
			return &recs[i]
		}
	}
	return nil
}

func extractScala(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("scala")
	if !ok {
		t.Fatal("scala extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "scala",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return recs
}

// TestIssue4432_RealFixture_InPipeline runs the live-shaped fixture through the
// REAL Scala extractor and asserts all four value-set shapes surface, each
// name-searchable with members_json carrying real {key,value,line}.
func TestIssue4432_RealFixture_InPipeline(t *testing.T) {
	src, err := os.ReadFile("testdata/issue4432/constants.scala")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	recs := extractScala(t, "config/constants.scala", string(src))

	// (2) object const group.
	pages := searchableScalaEnum(recs, "Pages")
	if pages == nil {
		t.Fatal("SCOPE.Enum:Pages value-set not found (RED before #4432)")
	}
	if got := pages.Properties["kind_hint"]; got != "scala_const_object" {
		t.Fatalf("Pages kind_hint = %q, want scala_const_object", got)
	}
	pm := cs4432Members(t, pages)
	if v, ok := cs4432Value(pm, "CoreAdmin"); !ok || v != "core-admin" {
		t.Fatalf("Pages.CoreAdmin = %q ok=%v, want core-admin", v, ok)
	}
	if v, ok := cs4432Value(pm, "Billing"); !ok || v != "billing" {
		t.Fatalf("Pages.Billing = %q ok=%v, want billing (type ascription RHS)", v, ok)
	}
	for _, m := range pm {
		if m.Line == 0 {
			t.Fatalf("Pages member %q has zero source line", m.Key)
		}
	}

	// (1) const Map literal.
	routes := searchableScalaEnum(recs, "Routes")
	if routes == nil {
		t.Fatal("SCOPE.Enum:Routes value-set not found (RED before #4432)")
	}
	if got := routes.Properties["kind_hint"]; got != "scala_const_map" {
		t.Fatalf("Routes kind_hint = %q, want scala_const_map", got)
	}
	rm := cs4432Members(t, routes)
	if v, ok := cs4432Value(rm, "home"); !ok || v != "/" {
		t.Fatalf("Routes.home = %q ok=%v, want /", v, ok)
	}
	if v, ok := cs4432Value(rm, "admin"); !ok || v != "/admin" {
		t.Fatalf("Routes.admin = %q ok=%v, want /admin", v, ok)
	}
	// Honest-partial: the non-literal value records its expression text.
	if v, ok := cs4432Value(rm, "fallback"); !ok || v != "defaultRoute()" {
		t.Fatalf("Routes.fallback = %q ok=%v, want defaultRoute() (expr text)", v, ok)
	}

	// (3) Scala 3 enum.
	color := searchableScalaEnum(recs, "Color")
	if color == nil {
		t.Fatal("SCOPE.Enum:Color value-set not found (RED before #4432)")
	}
	if got := color.Properties["kind_hint"]; got != "scala_enum" {
		t.Fatalf("Color kind_hint = %q, want scala_enum", got)
	}
	cm := cs4432Members(t, color)
	for _, want := range []string{"Red", "Green", "Blue"} {
		if _, ok := cs4432Value(cm, want); !ok {
			t.Fatalf("Color missing case %q; members=%v", want, cm)
		}
	}

	// (4) sealed-trait enumeration.
	status := searchableScalaEnum(recs, "Status")
	if status == nil {
		t.Fatal("SCOPE.Enum:Status value-set not found (RED before #4432)")
	}
	if got := status.Properties["kind_hint"]; got != "scala_sealed_enum" {
		t.Fatalf("Status kind_hint = %q, want scala_sealed_enum", got)
	}
	sm := cs4432Members(t, status)
	for _, want := range []string{"Active", "Inactive", "Pending"} {
		if _, ok := cs4432Value(sm, want); !ok {
			t.Fatalf("Status missing case object %q; members=%v", want, sm)
		}
	}
	if status.Properties["member_count"] != "3" {
		t.Fatalf("Status member_count = %q, want 3", status.Properties["member_count"])
	}
}

// TestIssue4432_EnumWithPayload asserts an extended enum arm records its
// constructor-argument payload as the member value.
func TestIssue4432_EnumWithPayload(t *testing.T) {
	src := `enum Planet(mass: Double) {
  case Mercury extends Planet(3.303e+23)
  case Venus extends Planet(4.869e+24)
}`
	recs := extractScala(t, "planet.scala", src)
	en := searchableScalaEnum(recs, "Planet")
	if en == nil {
		t.Fatal("SCOPE.Enum:Planet not found")
	}
	m := cs4432Members(t, en)
	if v, ok := cs4432Value(m, "Mercury"); !ok || v != "3.303e+23" {
		t.Fatalf("Planet.Mercury value = %q ok=%v, want 3.303e+23", v, ok)
	}
}

// TestIssue4432_SealedCaseObjectsInCompanion asserts case objects nested in a
// companion object body still aggregate into the sealed-parent value-set.
func TestIssue4432_SealedCaseObjectsInCompanion(t *testing.T) {
	src := `sealed trait Direction
object Direction {
  case object North extends Direction
  case object South extends Direction
}`
	recs := extractScala(t, "direction.scala", src)
	en := searchableScalaEnum(recs, "Direction")
	if en == nil {
		t.Fatal("SCOPE.Enum:Direction not found")
	}
	m := cs4432Members(t, en)
	if len(m) != 2 {
		t.Fatalf("Direction members = %d, want 2; %v", len(m), m)
	}
}

// TestIssue4432_HonestPartial_NoSpuriousNodes asserts a bare scalar val and an
// empty object emit no value-set (precision-first).
func TestIssue4432_HonestPartial_NoSpuriousNodes(t *testing.T) {
	src := `val MaxRetries = 5
object Empty {
}`
	recs := extractScala(t, "misc.scala", src)
	if en := searchableScalaEnum(recs, "MaxRetries"); en != nil {
		t.Fatalf("bare scalar val MaxRetries should not emit a value-set")
	}
	if en := searchableScalaEnum(recs, "Empty"); en != nil {
		t.Fatalf("empty object Empty should not emit a value-set")
	}
}

// TestIssue4432_SeqValueSet asserts a const Seq/List literal surfaces as a
// value-set with each element as a member.
func TestIssue4432_SeqValueSet(t *testing.T) {
	src := `val Roles = Seq("admin", "editor", "viewer")`
	recs := extractScala(t, "roles.scala", src)
	en := searchableScalaEnum(recs, "Roles")
	if en == nil {
		t.Fatal("SCOPE.Enum:Roles not found")
	}
	if got := en.Properties["kind_hint"]; got != "scala_const_seq" {
		t.Fatalf("Roles kind_hint = %q, want scala_const_seq", got)
	}
	m := cs4432Members(t, en)
	for _, want := range []string{"admin", "editor", "viewer"} {
		if _, ok := cs4432Value(m, want); !ok {
			t.Fatalf("Roles missing element %q; %v", want, m)
		}
	}
}
