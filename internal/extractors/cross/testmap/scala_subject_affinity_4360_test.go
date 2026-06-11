package testmap

import "testing"

// #4360 — ScalaTest / specs2 subject-affinity tiers + scaffolding-orphan drop.
//
// detectScalaTest resolves the production system-under-test (the TESTS-edge
// target) via three honest tiers, most-trusted first:
//
//	Tier 1: spec-class stem convention (OrderServiceSpec → OrderService).
//	Tier 2: leading string-literal subject ("OrderService" should …).
//	Tier 3: a locally-constructed SUT (new OrderService(…) / mock[OrderService]).
//
// A leaf body's own direct production call always outranks the tiered fallback
// (the resolver promotes it to high confidence); the tiers supply the
// describeSubject the resolver falls back to when a leaf has no direct call.
// When no tier resolves an in-repo subject, NO spurious subject edge is emitted.

// Tier 1 — spec-class stem convention. FunSuite whose only body content is an
// assertion still links to the SUT via the OrderServiceTest → OrderService stem.
func TestScala4360_Tier1_SpecNameStem(t *testing.T) {
	src := `import org.scalatest.funsuite.AnyFunSuite
class OrderServiceTest extends AnyFunSuite {
    test("places order") {
        val sut = new OrderService(repo)
        assert(true)
    }
}`
	recs := runExtract(t, "OrderServiceTest.scala", "scala", src)
	if !hasEdgeAny(recs, "it_places_order", "OrderService") {
		t.Fatalf("Tier1: expected TESTS edge to OrderService (spec-name stem); recs=%+v", recs)
	}
}

// Tier 2 — leading string-literal subject. The spec class name (OrderBehaviours)
// has no Spec/Test suffix, and the leaf body contains only an assertion, so the
// only SUT signal is the leading literal `"OrderService" should`.
func TestScala4360_Tier2_LeadingLiteralSubject(t *testing.T) {
	src := `import org.scalatest.flatspec.AnyFlatSpec
import org.scalatest.matchers.should.Matchers
class OrderBehaviours extends AnyFlatSpec with Matchers {
    "OrderService" should "place an order" in {
        result shouldBe ok
    }
}`
	recs := runExtract(t, "OrderBehaviours.scala", "scala", src)
	if !hasEdgeAny(recs, "it_OrderService_place_an_order", "OrderService") {
		t.Fatalf("Tier2: expected TESTS edge to OrderService (leading literal); recs=%+v", recs)
	}
}

// Tier 3 — locally-constructed SUT. No Spec/Test suffix, no leading literal; the
// only SUT signal is `new PaymentGateway(...)` in the body.
func TestScala4360_Tier3_NewSubject(t *testing.T) {
	src := `import org.scalatest.funsuite.AnyFunSuite
class GatewayChecks extends AnyFunSuite {
    test("charges the card") {
        val sut = new PaymentGateway(cfg)
        assert(sut != null)
    }
}`
	recs := runExtract(t, "GatewayChecks.scala", "scala", src)
	if !hasEdgeAny(recs, "it_charges_the_card", "PaymentGateway") {
		t.Fatalf("Tier3: expected TESTS edge to PaymentGateway (new SUT); recs=%+v", recs)
	}
}

// Tier 3 mock — `mock[InventoryService]`. The mocked type is the SUT; the body
// call on the mock receiver resolves the production method.
func TestScala4360_Tier3_MockSubject(t *testing.T) {
	src := `import org.scalatest.funsuite.AnyFunSuite
class StockChecks extends AnyFunSuite {
    test("reserves stock") {
        val svc = mock[InventoryService]
        when(svc.reserve()).thenReturn(true)
        assert(true)
    }
}`
	recs := runExtract(t, "StockChecks.scala", "scala", src)
	if !hasEdgeAny(recs, "it_reserves_stock", "svc.reserve") &&
		!hasEdgeAny(recs, "it_reserves_stock", "InventoryService") {
		t.Fatalf("Tier3-mock: expected TESTS edge to the mocked SUT; recs=%+v", recs)
	}
}

// Scaffolding-orphan drop — a single FlatSpec leaf must yield exactly ONE test
// entity, not a finer FlatSpec leaf PLUS a redundant verb-only WordSpec leaf for
// the same `in {` block. Before #4360 the same test surfaced twice
// (it_Subject_verb AND it_verb).
func TestScala4360_NoDuplicateFlatSpecLeaf(t *testing.T) {
	src := `import org.scalatest.flatspec.AnyFlatSpec
class OrderBehaviours extends AnyFlatSpec {
    "OrderService" should "place an order" in {
        placeIt()
    }
}`
	recs := runExtract(t, "OrderBehaviours.scala", "scala", src)
	if len(recs) != 1 {
		var names []string
		for _, r := range recs {
			names = append(names, r.Name)
		}
		t.Fatalf("expected exactly 1 test entity (no FlatSpec/WordSpec duplicate); got %d: %v", len(recs), names)
	}
	// The verb-only shorthand leaf must NOT appear as its own entity.
	for _, r := range recs {
		if r.Properties["test_function"] == "it_place_an_order" {
			t.Errorf("redundant verb-only WordSpec leaf must be suppressed; got %q", r.Name)
		}
	}
}

// Negative — a spec with no in-repo SUT signal (no stem subject, no leading
// literal, no new/mock) must not fabricate a high/medium subject edge from the
// new tiers. (The pre-existing #4466 low-confidence naming-convention fallback
// is unrelated to the tier work and is intentionally retained.)
func TestScala4360_Negative_NoSpuriousSubject(t *testing.T) {
	src := `import org.scalatest.funsuite.AnyFunSuite
class RandomThings extends AnyFunSuite {
    test("does stuff") {
        assert(1 == 1)
    }
}`
	recs := runExtract(t, "RandomThings.scala", "scala", src)
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind != "TESTS" {
				continue
			}
			if c := rel.Properties["confidence"]; c == "high" || c == "medium" {
				t.Errorf("no high/medium subject edge expected for an unresolvable spec; got %s -> %s [%s]",
					r.Name, rel.ToID, c)
			}
		}
	}
}
