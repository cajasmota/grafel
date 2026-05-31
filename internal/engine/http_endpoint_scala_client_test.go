package engine

import "testing"

// TestScalaClient_SttpBasicRequestLiteral covers the canonical sttp form
// `basicRequest.get(uri"https://api.example.com/v1/users")` and a POST, and
// asserts the SPECIFIC canonical paths + FETCHES edges (value-asserting; the
// host api.example.com is stripped to leave the producer route /v1/users).
func TestScalaClient_SttpBasicRequestLiteral(t *testing.T) {
	src := `
import sttp.client3._

def fetchUsers() = {
  basicRequest.get(uri"https://api.example.com/v1/users")
}

def createUser(payload: String) = {
  basicRequest
    .body(payload)
    .post(uri"https://api.example.com/v1/users")
}
`
	ids, rels := runDetectWithRels(t, "scala", "UsersClient.scala", src)
	requireContains(t, ids, []string{
		"http:GET:/v1/users",
		"http:POST:/v1/users",
	}, "sttp-basic-literal")
	requireFetches(t, rels, "http:GET:/v1/users", "sttp-basic-literal")
	requireFetches(t, rels, "http:POST:/v1/users", "sttp-basic-literal")
}

// TestScalaClient_SttpQuickRequestInterpolated covers `quickRequest.get(...)`
// with a `$id` path interpolation → {id} placeholder, asserting the specific
// path /products/{id} on host catalog:8080 (host stripped).
func TestScalaClient_SttpQuickRequestInterpolated(t *testing.T) {
	src := `
import sttp.client3.quick._

def getProduct(id: String) = {
  quickRequest.get(uri"http://catalog:8080/products/$id")
}
`
	ids, _ := runDetectWithRels(t, "scala", "ProductClient.scala", src)
	requireContains(t, ids, []string{"http:GET:/products/{id}"}, "sttp-quick-interp")
}

// TestScalaClient_SttpVerbAfterBuilderChain covers a verb combinator that
// follows intermediate builder combinators (.response(asJson[T])) and a PUT
// with `${...}` interpolation → {param}. Asserts the exact PUT path.
func TestScalaClient_SttpVerbAfterBuilderChain(t *testing.T) {
	src := `
import sttp.client3._

def updateUser(user: User) = {
  basicRequest
    .response(asJson[User])
    .put(uri"https://api.example.com/v1/users/${user.id}")
}
`
	ids, rels := runDetectWithRels(t, "scala", "UpdateClient.scala", src)
	requireContains(t, ids, []string{"http:PUT:/v1/users/{param}"}, "sttp-chain-put")
	requireFetches(t, rels, "http:PUT:/v1/users/{param}", "sttp-chain-put")
}

// TestScalaClient_SttpNoMatch asserts a non-sttp Scala file produces no
// outbound endpoint synthetic.
func TestScalaClient_SttpNoMatch(t *testing.T) {
	src := `
object Main extends App {
  println("no http client here")
}
`
	ids, _ := runDetectWithRels(t, "scala", "Main.scala", src)
	for _, id := range ids {
		if len(id) >= 5 && id[:5] == "http:" {
			t.Errorf("sttp-no-match: unexpected http synthetic %q", id)
		}
	}
}
