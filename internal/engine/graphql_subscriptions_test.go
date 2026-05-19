package engine

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// GraphQL — SDL: type Subscription { ... }
// ---------------------------------------------------------------------------

func TestGraphQL_SDLSchema_EmitsSubscriptionFields(t *testing.T) {
	src := `type Query {
  hello: String
}

type Subscription {
  messageAdded(channelId: ID!): Message
  userJoined(channelId: ID!, userId: ID!): User
}
`
	res := runDetectWS(t, "javascript", "schema.graphql", src)
	subs := filterEntities(res.Entities, graphqlSubscriptionKind)
	if len(subs) < 2 {
		t.Fatalf("expected ≥2 Subscription entities; got %v", subs)
	}
	seen := map[string]string{}
	for _, s := range subs {
		seen[s.ID] = s.Properties["args"]
	}
	if _, ok := seen["graphql_sub:messageAdded"]; !ok {
		t.Errorf("missing graphql_sub:messageAdded; got %v", seen)
	}
	if args := seen["graphql_sub:userJoined"]; !strings.Contains(args, "channelId") || !strings.Contains(args, "userId") {
		t.Errorf("userJoined args = %q, want channelId,userId", args)
	}
	pubs := filterRels(res.Relationships, "GRAPHQL_PUBLISHES")
	if len(pubs) < 2 {
		t.Errorf("expected ≥2 GRAPHQL_PUBLISHES edges; got %d", len(pubs))
	}
}

// ---------------------------------------------------------------------------
// GraphQL — Apollo Server resolver
// ---------------------------------------------------------------------------

func TestGraphQL_ApolloResolver_EmitsPublishes(t *testing.T) {
	src := `const resolvers = {
  Query: {},
  Subscription: {
    messageAdded: {
      subscribe: () => pubsub.asyncIterator(['MSG_ADDED'])
    },
    notify: {
      subscribe: withFilter(() => pubsub.asyncIterator('NOTIFY'), (p, v) => true)
    }
  }
};
`
	res := runDetectWS(t, "javascript", "resolvers.js", src)
	pubs := filterRels(res.Relationships, "GRAPHQL_PUBLISHES")
	if len(pubs) < 2 {
		t.Fatalf("expected ≥2 GRAPHQL_PUBLISHES edges; got %v", pubs)
	}
	foundFiltered := false
	for _, p := range pubs {
		if p.Properties["filtered"] == "true" {
			foundFiltered = true
		}
	}
	if !foundFiltered {
		t.Errorf("expected filtered=true on the withFilter-wrapped resolver")
	}
}

// ---------------------------------------------------------------------------
// GraphQL — Client subscription
// ---------------------------------------------------------------------------

func TestGraphQL_ClientUseSubscription_EmitsSubscribes(t *testing.T) {
	src := "import { useSubscription, gql } from '@apollo/client';\n" +
		"const SUB = gql`\n" +
		"  subscription OnMessage($cid: ID!) {\n" +
		"    messageAdded(channelId: $cid) { id text }\n" +
		"  }\n" +
		"`;\n" +
		"function Chat() {\n" +
		"  const { data } = useSubscription(SUB);\n" +
		"  return null;\n" +
		"}\n"
	res := runDetectWS(t, "typescript", "Chat.tsx", src)
	subs := filterEntities(res.Entities, graphqlSubscriptionKind)
	if len(subs) == 0 {
		t.Fatalf("expected Subscription entity from client; got %v", res.Entities)
	}
	if subs[0].ID != "graphql_sub:messageAdded" {
		t.Errorf("got %q want graphql_sub:messageAdded", subs[0].ID)
	}
	if subs[0].Properties["framework"] != "apollo_client" {
		t.Errorf("framework = %q, want apollo_client", subs[0].Properties["framework"])
	}
	sb := filterRels(res.Relationships, "GRAPHQL_SUBSCRIBES")
	if len(sb) == 0 {
		t.Fatalf("expected GRAPHQL_SUBSCRIBES edge")
	}
	if !strings.Contains(sb[0].Properties["args"], "channelId") {
		t.Errorf("expected args=channelId on subscription edge; got %q", sb[0].Properties["args"])
	}
}

// Cross-stack: SDL field + client subscription share identity.
func TestGraphQL_CrossStack_MatchByFieldName(t *testing.T) {
	server := `type Subscription {
  ticketUpdated(orgId: ID!): Ticket
}
`
	client := "function App() {\n" +
		"  useSubscription(gql`subscription { ticketUpdated(orgId: $oid) { id } }`);\n" +
		"}\n"
	srvRes := runDetectWS(t, "javascript", "schema.graphql", server)
	clRes := runDetectWS(t, "typescript", "App.tsx", client)

	srvSubs := filterEntities(srvRes.Entities, graphqlSubscriptionKind)
	clSubs := filterEntities(clRes.Entities, graphqlSubscriptionKind)
	if len(srvSubs) == 0 || len(clSubs) == 0 {
		t.Fatalf("missing subs: server=%v client=%v", srvSubs, clSubs)
	}
	if srvSubs[0].ID != clSubs[0].ID {
		t.Errorf("identity mismatch: server=%q client=%q", srvSubs[0].ID, clSubs[0].ID)
	}
}

// ---------------------------------------------------------------------------
// GraphQL — non-GraphQL file produces NO Subscription entities (parity)
// ---------------------------------------------------------------------------

func TestGraphQL_NonGraphQLFile_NoSubscriptions(t *testing.T) {
	src := `// regular code, no graphql
function add(a, b) { return a + b; }
`
	res := runDetectWS(t, "javascript", "math.js", src)
	if ss := filterEntities(res.Entities, graphqlSubscriptionKind); len(ss) > 0 {
		t.Errorf("non-GraphQL file produced Subscription entities: %v", ss)
	}
}
