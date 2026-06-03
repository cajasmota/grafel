package engine

import "testing"

// TestDartGraphQLClient_QueryConstRef covers the canonical graphql_flutter
// idiom: a gql doc bound to a const, referenced by a Query widget's
// QueryOptions(document:). Asserts the SPECIFIC operation root field
// (`user`) mapped to the server endpoint shape
// http:GRAPHQL:/graphql/Query/user (value-asserting), plus the FETCHES edge.
func TestDartGraphQLClient_QueryConstRef(t *testing.T) {
	src := `
import 'package:graphql_flutter/graphql_flutter.dart';

final GET_USER = gql(r'''
  query GetUser {
    user { id name }
  }
''');

class UserScreen extends StatelessWidget {
  Widget build(BuildContext context) {
    return Query(
      options: QueryOptions(document: GET_USER),
      builder: (result, {fetchMore, refetch}) => Text("x"),
    );
  }
}
`
	ids, rels := runDetectWithRels(t, "dart", "lib/user_screen.dart", src)
	requireContains(t, ids, []string{
		"http:GRAPHQL:/graphql/Query/user",
	}, "dart-gql-query-const")
	requireFetches(t, rels, "http:GRAPHQL:/graphql/Query/user", "dart-gql-query-const")
}

// TestDartGraphQLClient_InlineMutation covers an inline gql mutation passed
// straight into MutationOptions(document: gql(r'...')), asserting the
// Mutation root field `createUser` → http:GRAPHQL:/graphql/Mutation/createUser.
func TestDartGraphQLClient_InlineMutation(t *testing.T) {
	src := `
import 'package:graphql_flutter/graphql_flutter.dart';

Future<void> submit(GraphQLClient client) async {
  await client.mutate(MutationOptions(
    document: gql(r'''
      mutation AddUser {
        createUser(input: {name: "x"}) { id }
      }
    '''),
  ));
}
`
	ids, rels := runDetectWithRels(t, "dart", "lib/add_user.dart", src)
	requireContains(t, ids, []string{
		"http:GRAPHQL:/graphql/Mutation/createUser",
	}, "dart-gql-inline-mutation")
	requireFetches(t, rels, "http:GRAPHQL:/graphql/Mutation/createUser", "dart-gql-inline-mutation")
}

// TestDartGraphQLClient_ClientQueryInline covers
// client.query(QueryOptions(document: gql('...'))) with a single-quoted gql
// doc and MULTIPLE root fields, asserting both server endpoints.
func TestDartGraphQLClient_ClientQueryInline(t *testing.T) {
	src := `
import 'package:graphql/client.dart';

Future<QueryResult> load(GraphQLClient client) async {
  return client.query(QueryOptions(
    document: gql('query Dashboard { stats { count } alerts { id } }'),
  ));
}
`
	ids, _ := runDetectWithRels(t, "dart", "lib/dashboard.dart", src)
	requireContains(t, ids, []string{
		"http:GRAPHQL:/graphql/Query/stats",
		"http:GRAPHQL:/graphql/Query/alerts",
	}, "dart-gql-client-query")
}

// TestDartGraphQLClient_Subscription covers a subscription gql doc, asserting
// the Subscription root type mapping.
func TestDartGraphQLClient_Subscription(t *testing.T) {
	src := `
import 'package:graphql_flutter/graphql_flutter.dart';

final SUB = gql(r'''
  subscription OnMessage {
    messageAdded { id body }
  }
''');

Widget build(BuildContext ctx) {
  return Subscription(options: SubscriptionOptions(document: SUB), builder: (r) => Text("x"));
}
`
	ids, _ := runDetectWithRels(t, "dart", "lib/chat.dart", src)
	requireContains(t, ids, []string{
		"http:GRAPHQL:/graphql/Subscription/messageAdded",
	}, "dart-gql-subscription")
}

// TestDartGraphQLClient_NegativeRest asserts a Dart file with only a REST
// dio.get call (no gql) produces NO GraphQL operation entity.
func TestDartGraphQLClient_NegativeRest(t *testing.T) {
	src := `
import 'package:dio/dio.dart';

Future<void> load() async {
  await dio.get("/users");
}
`
	ids, _ := runDetectWithRels(t, "dart", "lib/rest.dart", src)
	for _, id := range ids {
		if len(id) >= 14 && id[:14] == "http:GRAPHQL:/" {
			t.Errorf("dart-gql-neg-rest: unexpected GraphQL op %q from a REST-only file", id)
		}
	}
}

// TestDartGraphQLClient_NegativePlainString asserts a non-GraphQL raw string
// that merely looks like braces does not yield a GraphQL operation.
func TestDartGraphQLClient_NegativePlainString(t *testing.T) {
	src := `
class Config {
  final String template = r'{ just a plain string }';
}
`
	ids, _ := runDetectWithRels(t, "dart", "lib/config.dart", src)
	for _, id := range ids {
		if len(id) >= 14 && id[:14] == "http:GRAPHQL:/" {
			t.Errorf("dart-gql-neg-plain: unexpected GraphQL op %q from a plain string", id)
		}
	}
}
