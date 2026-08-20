package engine

import (
	"testing"
)

// TestSynth_Strawberry_Query covers @strawberry.type Query with plain resolver
// methods. #3066.
func TestSynth_Strawberry_Query(t *testing.T) {
	src := `import strawberry
import typing

@strawberry.type
class Query:
    def users(self) -> typing.List[str]:
        return []

    def user(self, id: int) -> str:
        return ""

    def health(self) -> str:
        return "ok"

schema = strawberry.Schema(query=Query)
`
	got, res := runDetect(t, "python", "schema.py", src)
	want := []string{
		"http:GRAPHQL:/graphql/Query/users",
		"http:GRAPHQL:/graphql/Query/user",
		"http:GRAPHQL:/graphql/Query/health",
	}
	requireContains(t, got, want, "Strawberry Query")

	// Verify framework label and handler attribution.
	e := findSynthDef(res, "http:GRAPHQL:/graphql/Query/users")
	if e == nil {
		t.Fatalf("Strawberry Query: missing http:GRAPHQL:/graphql/Query/users")
	}
	if e.Properties["framework"] != "strawberry-graphql" {
		t.Errorf("Strawberry Query: framework = %q, want strawberry-graphql", e.Properties["framework"])
	}
	if e.Properties["source_handler"] != "SCOPE.Operation:Query.users" {
		t.Errorf("Strawberry Query: source_handler = %q, want SCOPE.Operation:Query.users", e.Properties["source_handler"])
	}
	if e.StartLine == 0 {
		t.Errorf("Strawberry Query: StartLine not stamped on users resolver")
	}
}

// TestSynth_Strawberry_Mutation covers @strawberry.type Mutation with
// @strawberry.mutation decorated methods. #3066.
func TestSynth_Strawberry_Mutation(t *testing.T) {
	src := `import strawberry

@strawberry.type
class Mutation:
    @strawberry.mutation
    def create_user(self, name: str) -> str:
        return name

    @strawberry.mutation
    def delete_user(self, id: int) -> bool:
        return True

schema = strawberry.Schema(mutation=Mutation)
`
	got, res := runDetect(t, "python", "schema.py", src)
	want := []string{
		"http:GRAPHQL:/graphql/Mutation/create_user",
		"http:GRAPHQL:/graphql/Mutation/delete_user",
	}
	requireContains(t, got, want, "Strawberry Mutation")

	e := findSynthDef(res, "http:GRAPHQL:/graphql/Mutation/create_user")
	if e == nil {
		t.Fatalf("Strawberry Mutation: missing http:GRAPHQL:/graphql/Mutation/create_user")
	}
	if e.Properties["framework"] != "strawberry-graphql" {
		t.Errorf("Strawberry Mutation: framework = %q, want strawberry-graphql", e.Properties["framework"])
	}
	if e.Properties["source_handler"] != "SCOPE.Operation:Mutation.create_user" {
		t.Errorf("Strawberry Mutation: source_handler = %q, want SCOPE.Operation:Mutation.create_user", e.Properties["source_handler"])
	}
	if e.StartLine == 0 {
		t.Errorf("Strawberry Mutation: StartLine not stamped on create_user resolver")
	}
}

// TestSynth_Strawberry_Subscription covers @strawberry.type Subscription with
// async generator methods. #3066.
func TestSynth_Strawberry_Subscription(t *testing.T) {
	src := `import strawberry
import typing
import asyncio

@strawberry.type
class Subscription:
    @strawberry.subscription
    async def user_added(self) -> typing.AsyncGenerator[str, None]:
        while True:
            await asyncio.sleep(1)
            yield "user"

    @strawberry.subscription
    async def message_sent(self, channel: str) -> typing.AsyncGenerator[str, None]:
        while True:
            await asyncio.sleep(1)
            yield ""

schema = strawberry.Schema(subscription=Subscription)
`
	got, res := runDetect(t, "python", "schema.py", src)
	want := []string{
		"http:GRAPHQL:/graphql/Subscription/user_added",
		"http:GRAPHQL:/graphql/Subscription/message_sent",
	}
	requireContains(t, got, want, "Strawberry Subscription")

	e := findSynthDef(res, "http:GRAPHQL:/graphql/Subscription/user_added")
	if e == nil {
		t.Fatalf("Strawberry Subscription: missing http:GRAPHQL:/graphql/Subscription/user_added")
	}
	if e.Properties["framework"] != "strawberry-graphql" {
		t.Errorf("Strawberry Subscription: framework = %q, want strawberry-graphql", e.Properties["framework"])
	}
	if e.Properties["source_handler"] != "SCOPE.Operation:Subscription.user_added" {
		t.Errorf("Strawberry Subscription: source_handler = %q, want SCOPE.Operation:Subscription.user_added", e.Properties["source_handler"])
	}
}

// TestSynth_Strawberry_CombinedSchema covers a schema file that defines all
// three root types. #3066.
func TestSynth_Strawberry_CombinedSchema(t *testing.T) {
	src := `import strawberry
import typing

@strawberry.type
class Query:
    def books(self) -> typing.List[str]:
        return []

    def book(self, id: strawberry.ID) -> str:
        return ""

@strawberry.type
class Mutation:
    @strawberry.mutation
    def add_book(self, title: str) -> str:
        return title

@strawberry.type
class Subscription:
    @strawberry.subscription
    async def book_added(self) -> typing.AsyncGenerator[str, None]:
        yield ""

schema = strawberry.Schema(query=Query, mutation=Mutation, subscription=Subscription)
`
	got, _ := runDetect(t, "python", "schema.py", src)
	want := []string{
		"http:GRAPHQL:/graphql/Query/books",
		"http:GRAPHQL:/graphql/Query/book",
		"http:GRAPHQL:/graphql/Mutation/add_book",
		"http:GRAPHQL:/graphql/Subscription/book_added",
	}
	requireContains(t, got, want, "Strawberry combined schema")
}

// TestSynth_Strawberry_DunderSkip asserts that __init__, __str__ and other
// dunder methods on root types are not emitted as GraphQL operations. #3066.
func TestSynth_Strawberry_DunderSkip(t *testing.T) {
	src := `import strawberry

@strawberry.type
class Query:
    def __init__(self):
        pass

    def users(self) -> list:
        return []
`
	got, _ := runDetect(t, "python", "schema.py", src)
	for _, id := range got {
		if id == "http:GRAPHQL:/graphql/Query/__init__" {
			t.Errorf("Strawberry: dunder __init__ should not be emitted as an operation")
		}
	}
	want := []string{"http:GRAPHQL:/graphql/Query/users"}
	requireContains(t, got, want, "Strawberry dunder skip")
}

// TestSynth_Strawberry_NoOpOnFlask asserts the Strawberry synthesizer does not
// fire on a plain Flask file. #3066.
func TestSynth_Strawberry_NoOpOnFlask(t *testing.T) {
	src := `from flask import Flask

app = Flask(__name__)

@app.route("/ping")
def ping():
    return "pong"
`
	_, res := runDetect(t, "python", "flask_app.py", src)
	e := findSynthDef(res, "http:GET:/ping")
	if e == nil {
		t.Fatalf("expected http:GET:/ping endpoint from Flask")
	}
	if e.Properties["framework"] != "flask" {
		t.Errorf("framework = %q, want flask (Strawberry synthesizer must no-op on Flask files)", e.Properties["framework"])
	}
	// No GRAPHQL synthetics should exist.
	for _, ent := range res.Entities {
		if ent.Kind == httpEndpointDefinitionKind && ent.Properties["framework"] == "strawberry-graphql" {
			t.Errorf("Strawberry synthesizer fired on Flask file, emitted: %s", ent.ID)
		}
	}
}

// TestSynth_Strawberry_FastAPIIntegration covers a typical pattern where
// Strawberry is mounted on a FastAPI app. The Strawberry synthesizer should
// emit GRAPHQL synthetics while FastAPI emits the mount route. #3066.
func TestSynth_Strawberry_FastAPIIntegration(t *testing.T) {
	src := `import strawberry
from strawberry.fastapi import GraphQLRouter
from fastapi import FastAPI

@strawberry.type
class Query:
    def hello(self) -> str:
        return "world"

    def products(self) -> list:
        return []

schema = strawberry.Schema(query=Query)
graphql_app = GraphQLRouter(schema)

app = FastAPI()
# NOTE: prefix="/graphql" below is realism only -- nothing reads it. No
# synthesizer parses include_router(prefix=) today (see #6385). The "/graphql"
# in the assertions below comes from a hardcoded literal in the Strawberry
# synthesizer, not from this argument. Changing this prefix will NOT change
# the asserted paths.
app.include_router(graphql_app, prefix="/graphql")
`
	got, _ := runDetect(t, "python", "main.py", src)
	// Real subject of this test: Strawberry emits one GRAPHQL endpoint per
	// root-type method. The path prefix is synthesizer-hardcoded, not folded
	// in from the FastAPI mount above.
	want := []string{
		"http:GRAPHQL:/graphql/Query/hello",
		"http:GRAPHQL:/graphql/Query/products",
	}
	requireContains(t, got, want, "Strawberry+FastAPI integration")
}
