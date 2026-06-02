package testmap

import "testing"

// Value-asserting tests_linkage fixtures for the trailing Scala frameworks
// (#3990, epic #3872, audit #3887). detectScalaTest operates purely on test
// SOURCE content (ScalaTest / specs2 / MUnit / ZIO leaf shapes + subject-from-
// spec-name resolution) — it is framework-agnostic. A spec that exercises a
// caliban resolver, an sttp client, a pekko-http route, or a tapir endpoint
// resolves the production subject identically. These prove the SPECIFIC
// test→target TESTS edge fires, justifying tests_linkage = full (matching the
// other Scala frameworks' deep-testmap citation).

// TestScalaTrailing_Caliban_TestsLinkage: a ScalaTest spec for a caliban
// resolver emits a TESTS edge to the resolver method it calls.
func TestScalaTrailing_Caliban_TestsLinkage(t *testing.T) {
	src := `import org.scalatest.funsuite.AnyFunSuite
class UserResolverSpec extends AnyFunSuite {
    test("resolves users") {
        val r = new UserResolver
        assert(r.users().nonEmpty)
    }
}`
	recs := runExtract(t, "UserResolverSpec.scala", "scala", src)
	if recs[0].Properties["test_framework"] != "scalatest" {
		t.Errorf("framework=%q, want scalatest", recs[0].Properties["test_framework"])
	}
	if !hasEdgeAny(recs, "it_resolves_users", "r.users") {
		t.Fatalf("expected TESTS edge to r.users; recs=%+v", recs)
	}
	if hasEdgeAny(recs, "it_resolves_users", "assert") {
		t.Errorf("assert() must be stop-worded")
	}
}

// TestScalaTrailing_Sttp_TestsLinkage: a MUnit spec for an sttp client emits a
// TESTS edge to the production subject. The body call is wrapped in
// assertEquals(...) (stop-worded), so the deep testmap falls back to the
// subject-from-spec-name resolution (ApiClientSpec → ApiClient) — still a
// specific, named TESTS edge.
func TestScalaTrailing_Sttp_TestsLinkage(t *testing.T) {
	src := `import munit.FunSuite
class ApiClientSpec extends FunSuite {
    test("fetches a user") {
        val client = new ApiClient
        assertEquals(client.fetchUser("1").status, 200)
    }
}`
	recs := runExtract(t, "ApiClientSpec.scala", "scala", src)
	if recs[0].Properties["test_framework"] != "munit" {
		t.Errorf("framework=%q, want munit", recs[0].Properties["test_framework"])
	}
	if !hasEdgeAny(recs, "it_fetches_a_user", "ApiClient") {
		t.Fatalf("expected TESTS edge to ApiClient (subject-from-spec-name); recs=%+v", recs)
	}
	if hasEdgeAny(recs, "it_fetches_a_user", "assertEquals") {
		t.Errorf("assertEquals must be stop-worded")
	}
}

// TestScalaTrailing_Pekko_TestsLinkage: a ScalaTest WordSpec for a pekko-http
// route handler emits a TESTS edge to the handler call. (pekko-http was
// partial citing the shallow suite-detector; this proves the DEEP testmap
// resolves a named edge, justifying full.)
func TestScalaTrailing_Pekko_TestsLinkage(t *testing.T) {
	src := `import org.scalatest.wordspec.AnyWordSpec
class UserRoutesSpec extends AnyWordSpec {
    "UserRoutes" should {
        "list users" in {
            val handler = new UserHandler
            handler.listUsers() shouldBe Seq.empty
        }
    }
}`
	recs := runExtract(t, "UserRoutesSpec.scala", "scala", src)
	if !hasEdgeAny(recs, "it_list_users", "handler.listUsers") {
		t.Fatalf("expected TESTS edge to handler.listUsers; recs=%+v", recs)
	}
	if hasEdgeAny(recs, "it_list_users", "shouldBe") {
		t.Errorf("shouldBe matcher must be stop-worded")
	}
}

// TestScalaTrailing_Tapir_TestsLinkage: a ScalaTest FlatSpec for a tapir
// endpoint logic class emits a TESTS edge to the logic call.
func TestScalaTrailing_Tapir_TestsLinkage(t *testing.T) {
	src := `import org.scalatest.flatspec.AnyFlatSpec
import org.scalatest.matchers.should.Matchers
class OrderLogicSpec extends AnyFlatSpec with Matchers {
    "OrderLogic" should "place an order" in {
        val logic = new OrderLogic
        logic.place(order) shouldBe Right(())
    }
}`
	recs := runExtract(t, "OrderLogicSpec.scala", "scala", src)
	if !hasEdgeAny(recs, "it_OrderLogic_place_an_order", "logic.place") {
		t.Fatalf("expected TESTS edge to logic.place; recs=%+v", recs)
	}
}
