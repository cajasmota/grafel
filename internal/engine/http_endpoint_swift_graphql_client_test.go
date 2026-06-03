package engine

import (
	"strings"
	"testing"
)

// gqlOpIDsFrom returns the GraphQL operation synthetic IDs from a detect run.
func gqlOpIDsFrom(ids []string) []string {
	var out []string
	for _, id := range ids {
		if strings.HasPrefix(id, "http:GRAPHQL:/") {
			out = append(out, id)
		}
	}
	return out
}

// TestSwiftGraphQLClient_FetchQuery covers the canonical Apollo-iOS idiom
// `apollo.fetch(query: GetUserQuery())`. Apollo-iOS code-generates the
// operation type from a `.graphql` file, so the Swift call site carries ONLY
// the operation NAME (GetUser), not the selected root field — the field lives
// in the generated/.graphql file. Honest-partial: we emit an operation
// REFERENCE keyed on the operation name + Query kind
// (http:GRAPHQL:/graphql/Query/GetUser) so the operation is discoverable, plus
// the FETCHES edge from the enclosing func. (This does NOT guarantee a
// cross-repo field-level link; see the file header.)
func TestSwiftGraphQLClient_FetchQuery(t *testing.T) {
	src := `
import Apollo

class UserService {
  let apollo: ApolloClient

  func loadUser() {
    apollo.fetch(query: GetUserQuery()) { result in
      print(result)
    }
  }
}
`
	ids, rels := runDetectWithRels(t, "swift", "UserService.swift", src)
	requireContains(t, ids, []string{
		"http:GRAPHQL:/graphql/Query/GetUser",
	}, "swift-apollo-fetch-query")
	requireFetches(t, rels, "http:GRAPHQL:/graphql/Query/GetUser", "swift-apollo-fetch-query")
}

// TestSwiftGraphQLClient_PerformMutation covers
// `apollo.perform(mutation: AddUserMutation(...))`, asserting the Mutation kind
// and the operation name AddUser.
func TestSwiftGraphQLClient_PerformMutation(t *testing.T) {
	src := `
import Apollo

func addUser(_ apollo: ApolloClient, name: String) {
  apollo.perform(mutation: AddUserMutation(name: name)) { result in
  }
}
`
	ids, _ := runDetectWithRels(t, "swift", "AddUserService.swift", src)
	requireContains(t, ids, []string{
		"http:GRAPHQL:/graphql/Mutation/AddUser",
	}, "swift-apollo-perform-mutation")
}

// TestSwiftGraphQLClient_Subscription covers
// `apollo.subscribe(subscription: OnMessageSubscription())`.
func TestSwiftGraphQLClient_Subscription(t *testing.T) {
	src := `
import Apollo

func listen(_ apollo: ApolloClient) {
  apollo.subscribe(subscription: OnMessageSubscription()) { _ in }
}
`
	ids, _ := runDetectWithRels(t, "swift", "Chat.swift", src)
	requireContains(t, ids, []string{
		"http:GRAPHQL:/graphql/Subscription/OnMessage",
	}, "swift-apollo-subscription")
}

// TestSwiftGraphQLClient_NegativeRest asserts an Alamofire REST call yields no
// GraphQL operation entity.
func TestSwiftGraphQLClient_NegativeRest(t *testing.T) {
	src := `
import Alamofire

func load() {
  AF.request("/users", method: .get)
}
`
	ids, _ := runDetectWithRels(t, "swift", "Rest.swift", src)
	if ops := gqlOpIDsFrom(ids); len(ops) != 0 {
		t.Errorf("swift-gql-neg-rest: unexpected GraphQL ops %v from a REST-only file", ops)
	}
}

// TestSwiftGraphQLClient_NegativeNonApollo asserts a Swift type whose name
// merely ends in Query but is NOT passed to an apollo.fetch/perform/subscribe
// call yields nothing (avoids attributing arbitrary `*Query()` constructors).
func TestSwiftGraphQLClient_NegativeNonApollo(t *testing.T) {
	src := `
struct SearchQuery {
  let term: String
}

func build() -> SearchQuery {
  return SearchQuery(term: "x")
}
`
	ids, _ := runDetectWithRels(t, "swift", "Search.swift", src)
	if ops := gqlOpIDsFrom(ids); len(ops) != 0 {
		t.Errorf("swift-gql-neg-nonapollo: unexpected GraphQL ops %v from a non-Apollo Query type", ops)
	}
}
