package engine

import (
	"testing"
)

// TestSynth_Graphene_Query covers a graphene.ObjectType Query with class-attribute
// fields backed by resolve_<field> methods. Asserts exact endpoint-shape parity
// with Strawberry (http:GRAPHQL:/graphql/<Root>/<field>) and handler attribution
// to the resolve_<field> method. #3620.
func TestSynth_Graphene_Query(t *testing.T) {
	src := `import graphene

class User(graphene.ObjectType):
    name = graphene.String()

class Query(graphene.ObjectType):
    users = graphene.List(User)
    me = graphene.Field(User)

    def resolve_users(self, info):
        return []

    def resolve_me(self, info):
        return None

schema = graphene.Schema(query=Query)
`
	got, res := runDetect(t, "python", "schema.py", src)
	want := []string{
		"http:GRAPHQL:/graphql/Query/users",
		"http:GRAPHQL:/graphql/Query/me",
	}
	requireContains(t, got, want, "Graphene Query")

	e := findSynthDef(res, "http:GRAPHQL:/graphql/Query/users")
	if e == nil {
		t.Fatalf("Graphene Query: missing http:GRAPHQL:/graphql/Query/users")
	}
	if e.Properties["framework"] != "graphene" {
		t.Errorf("Graphene Query: framework = %q, want graphene", e.Properties["framework"])
	}
	if e.Properties["source_handler"] != "SCOPE.Operation:Query.resolve_users" {
		t.Errorf("Graphene Query: source_handler = %q, want SCOPE.Operation:Query.resolve_users", e.Properties["source_handler"])
	}
	if e.StartLine == 0 {
		t.Errorf("Graphene Query: StartLine not stamped on resolve_users")
	}
}

// TestSynth_Graphene_Mutation covers a graphene.ObjectType Mutation with
// resolve_<field> methods. #3620.
func TestSynth_Graphene_Mutation(t *testing.T) {
	src := `import graphene

class Mutation(graphene.ObjectType):
    create_user = graphene.Field(graphene.String, name=graphene.String())

    def resolve_create_user(self, info, name):
        return name

    def resolve_delete_user(self, info, id):
        return True

schema = graphene.Schema(mutation=Mutation)
`
	got, res := runDetect(t, "python", "schema.py", src)
	want := []string{
		"http:GRAPHQL:/graphql/Mutation/create_user",
		"http:GRAPHQL:/graphql/Mutation/delete_user",
	}
	requireContains(t, got, want, "Graphene Mutation")

	e := findSynthDef(res, "http:GRAPHQL:/graphql/Mutation/create_user")
	if e == nil {
		t.Fatalf("Graphene Mutation: missing http:GRAPHQL:/graphql/Mutation/create_user")
	}
	if e.Properties["framework"] != "graphene" {
		t.Errorf("Graphene Mutation: framework = %q, want graphene", e.Properties["framework"])
	}
	if e.Properties["source_handler"] != "SCOPE.Operation:Mutation.resolve_create_user" {
		t.Errorf("Graphene Mutation: source_handler = %q, want SCOPE.Operation:Mutation.resolve_create_user", e.Properties["source_handler"])
	}
}

// TestSynth_Graphene_Subscription covers a graphene.ObjectType Subscription. #3620.
func TestSynth_Graphene_Subscription(t *testing.T) {
	src := `import graphene

class Subscription(graphene.ObjectType):
    count = graphene.Int()

    async def resolve_count(self, info):
        yield 1

schema = graphene.Schema(query=Query, subscription=Subscription)
`
	got, res := runDetect(t, "python", "schema.py", src)
	want := []string{
		"http:GRAPHQL:/graphql/Subscription/count",
	}
	requireContains(t, got, want, "Graphene Subscription")

	e := findSynthDef(res, "http:GRAPHQL:/graphql/Subscription/count")
	if e == nil {
		t.Fatalf("Graphene Subscription: missing http:GRAPHQL:/graphql/Subscription/count")
	}
	if e.Properties["source_handler"] != "SCOPE.Operation:Subscription.resolve_count" {
		t.Errorf("Graphene Subscription: source_handler = %q, want SCOPE.Operation:Subscription.resolve_count", e.Properties["source_handler"])
	}
}

// TestSynth_Graphene_DefaultResolverField asserts that a class-attribute field
// WITHOUT an explicit resolve_<field> method (relying on Graphene's default
// resolver) is still emitted (honest-partial). #3620.
func TestSynth_Graphene_DefaultResolverField(t *testing.T) {
	src := `import graphene

class Query(graphene.ObjectType):
    hello = graphene.String()
    explicit = graphene.String()

    def resolve_explicit(self, info):
        return "x"
`
	got, _ := runDetect(t, "python", "schema.py", src)
	want := []string{
		"http:GRAPHQL:/graphql/Query/hello",
		"http:GRAPHQL:/graphql/Query/explicit",
	}
	requireContains(t, got, want, "Graphene default-resolver field")
}

// TestSynth_Graphene_NoOpOnFlask asserts the Graphene synthesizer does not fire
// on a plain Flask file. #3620.
func TestSynth_Graphene_NoOpOnFlask(t *testing.T) {
	src := `from flask import Flask

app = Flask(__name__)

@app.route("/ping")
def ping():
    return "pong"
`
	_, res := runDetect(t, "python", "flask_app.py", src)
	for _, ent := range res.Entities {
		if ent.Kind == httpEndpointDefinitionKind && ent.Properties["framework"] == "graphene" {
			t.Errorf("Graphene synthesizer fired on Flask file, emitted: %s", ent.ID)
		}
	}
}

// TestSynth_Ariadne_Query covers schema-first Ariadne with a QueryType() binder
// and @query.field("<name>") decorator resolvers. Asserts exact endpoint-shape
// parity with Strawberry and handler attribution to the decorated resolver. #3620.
func TestSynth_Ariadne_Query(t *testing.T) {
	src := `from ariadne import QueryType, make_executable_schema, gql

type_defs = gql("""
    type Query {
        me: User
        users: [User]
    }
""")

query = QueryType()

@query.field("me")
def resolve_me(_, info):
    return {"name": "ada"}

@query.field("users")
def resolve_users(_, info):
    return []

schema = make_executable_schema(type_defs, query)
`
	got, res := runDetect(t, "python", "schema.py", src)
	want := []string{
		"http:GRAPHQL:/graphql/Query/me",
		"http:GRAPHQL:/graphql/Query/users",
	}
	requireContains(t, got, want, "Ariadne Query")

	e := findSynthDef(res, "http:GRAPHQL:/graphql/Query/me")
	if e == nil {
		t.Fatalf("Ariadne Query: missing http:GRAPHQL:/graphql/Query/me")
	}
	if e.Properties["framework"] != "ariadne" {
		t.Errorf("Ariadne Query: framework = %q, want ariadne", e.Properties["framework"])
	}
	if e.Properties["source_handler"] != "SCOPE.Operation:resolve_me" {
		t.Errorf("Ariadne Query: source_handler = %q, want SCOPE.Operation:resolve_me", e.Properties["source_handler"])
	}
	if e.StartLine == 0 {
		t.Errorf("Ariadne Query: StartLine not stamped on resolve_me decorator")
	}
}

// TestSynth_Ariadne_Mutation covers a MutationType() binder. #3620.
func TestSynth_Ariadne_Mutation(t *testing.T) {
	src := `from ariadne import MutationType

mutation = MutationType()

@mutation.field("create_user")
def resolve_create_user(_, info, name):
    return {"name": name}
`
	got, res := runDetect(t, "python", "schema.py", src)
	want := []string{
		"http:GRAPHQL:/graphql/Mutation/create_user",
	}
	requireContains(t, got, want, "Ariadne Mutation")

	e := findSynthDef(res, "http:GRAPHQL:/graphql/Mutation/create_user")
	if e == nil {
		t.Fatalf("Ariadne Mutation: missing http:GRAPHQL:/graphql/Mutation/create_user")
	}
	if e.Properties["source_handler"] != "SCOPE.Operation:resolve_create_user" {
		t.Errorf("Ariadne Mutation: source_handler = %q, want SCOPE.Operation:resolve_create_user", e.Properties["source_handler"])
	}
}

// TestSynth_Ariadne_ObjectTypeNamedBinder covers ObjectType("Query") style
// binders that name the root type as a constructor argument. #3620.
func TestSynth_Ariadne_ObjectTypeNamedBinder(t *testing.T) {
	src := `from ariadne import ObjectType

query = ObjectType("Query")

@query.field("health")
def resolve_health(_, info):
    return "ok"
`
	got, res := runDetect(t, "python", "schema.py", src)
	want := []string{
		"http:GRAPHQL:/graphql/Query/health",
	}
	requireContains(t, got, want, "Ariadne ObjectType named binder")

	e := findSynthDef(res, "http:GRAPHQL:/graphql/Query/health")
	if e == nil {
		t.Fatalf("Ariadne ObjectType: missing http:GRAPHQL:/graphql/Query/health")
	}
	if e.Properties["framework"] != "ariadne" {
		t.Errorf("Ariadne ObjectType: framework = %q, want ariadne", e.Properties["framework"])
	}
}

// TestSynth_Ariadne_NoOpOnFlask asserts the Ariadne synthesizer does not fire on
// a plain Flask file. #3620.
func TestSynth_Ariadne_NoOpOnFlask(t *testing.T) {
	src := `from flask import Flask

app = Flask(__name__)

@app.route("/ping")
def ping():
    return "pong"
`
	_, res := runDetect(t, "python", "flask_app.py", src)
	for _, ent := range res.Entities {
		if ent.Kind == httpEndpointDefinitionKind && ent.Properties["framework"] == "ariadne" {
			t.Errorf("Ariadne synthesizer fired on Flask file, emitted: %s", ent.ID)
		}
	}
}

// TestSynth_GrapheneAriadne_ShapeParityWithStrawberry asserts that the operation
// endpoint ids emitted for Graphene and Ariadne are byte-for-byte identical in
// shape to the canonical Strawberry shape — same /graphql/<Root>/<field> path,
// same http:GRAPHQL: prefix. #3620.
func TestSynth_GrapheneAriadne_ShapeParityWithStrawberry(t *testing.T) {
	strawberrySrc := `import strawberry

@strawberry.type
class Query:
    def me(self) -> str:
        return ""
`
	grapheneSrc := `import graphene

class Query(graphene.ObjectType):
    me = graphene.String()

    def resolve_me(self, info):
        return ""
`
	ariadneSrc := `from ariadne import QueryType

query = QueryType()

@query.field("me")
def resolve_me(_, info):
    return ""
`
	const wantID = "http:GRAPHQL:/graphql/Query/me"

	sb, _ := runDetect(t, "python", "sb.py", strawberrySrc)
	gr, _ := runDetect(t, "python", "gr.py", grapheneSrc)
	ar, _ := runDetect(t, "python", "ar.py", ariadneSrc)

	requireContains(t, sb, []string{wantID}, "Strawberry baseline")
	requireContains(t, gr, []string{wantID}, "Graphene parity")
	requireContains(t, ar, []string{wantID}, "Ariadne parity")
}
