package scala_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"

	_ "github.com/cajasmota/archigraph/internal/custom/scala"
)

func fi(path, lang, src string) extreg.FileInput {
	return extreg.FileInput{Path: path, Language: lang, Content: []byte(src)}
}

func extract(t *testing.T, name string, file extreg.FileInput) []entitySummary {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	var out []entitySummary
	for _, ent := range ents {
		out = append(out, entitySummary{
			Kind: ent.Kind, Subtype: ent.Subtype, Name: ent.Name, Props: ent.Properties,
		})
	}
	return out
}

type entitySummary struct {
	Kind, Subtype, Name string
	Props               map[string]string
}

// findBySubtype returns the first entity matching subtype+name (and its props),
// or (entitySummary{}, false) when none match. Used by value-asserting tests.
func findBySubtype(ents []entitySummary, subtype, name string) (entitySummary, bool) {
	for _, e := range ents {
		if e.Subtype == subtype && e.Name == name {
			return e, true
		}
	}
	return entitySummary{}, false
}

func containsEntity(ents []entitySummary, kind, name string) bool {
	for _, e := range ents {
		if e.Kind == kind && e.Name == name {
			return true
		}
	}
	return false
}

// findEntity returns the first entity matching kind+name, or nil.
func findEntity(ents []entitySummary, kind, name string) *entitySummary {
	for i := range ents {
		if ents[i].Kind == kind && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Akka
// ---------------------------------------------------------------------------

func TestAkkaClassicActor(t *testing.T) {
	src := `
class UserActor extends Actor {
  def receive: Receive = {
    case GetUser(id) => sender() ! findUser(id)
    case CreateUser(data) => sender() ! createUser(data)
  }
}
`
	ents := extract(t, "custom_scala_akka", fi("UserActor.scala", "scala", src))
	if !containsEntity(ents, "SCOPE.Service", "UserActor") {
		t.Error("expected UserActor SCOPE.Service actor")
	}
}

func TestAkkaTypedActor(t *testing.T) {
	src := `
class OrderProcessor extends AbstractBehavior[OrderCommand](context) {
  override def onMessage(msg: OrderCommand): Behavior[OrderCommand] = ???
}
`
	ents := extract(t, "custom_scala_akka", fi("OrderProcessor.scala", "scala", src))
	if !containsEntity(ents, "SCOPE.Service", "OrderProcessor") {
		t.Error("expected OrderProcessor typed actor service")
	}
}

func TestAkkaHttpRoute(t *testing.T) {
	src := `
val route =
  pathPrefix("api") {
    path("users") {
      get { complete(users) } ~
      post { entity(as[User]) { u => complete(u) } }
    }
  }
`
	ents := extract(t, "custom_scala_akka", fi("Routes.scala", "scala", src))
	// pathPrefix entity name = "prefix:" + path
	if !containsEntity(ents, "SCOPE.Pattern", "prefix:api") {
		t.Error("expected prefix:api pattern")
	}
	// path entity name = the path string directly
	if !containsEntity(ents, "SCOPE.Operation", "users") {
		t.Error("expected users path operation")
	}
}

func TestAkkaSpawn(t *testing.T) {
	src := `
val worker = context.spawn(WorkerActor(), "worker-1")
`
	ents := extract(t, "custom_scala_akka", fi("Main.scala", "scala", src))
	// spawn entity = "spawn:" + captured actor class
	if !containsEntity(ents, "SCOPE.Component", "spawn:WorkerActor") {
		t.Error("expected spawn:WorkerActor component")
	}
}

func TestAkkaNoMatch(t *testing.T) {
	src := `object Main extends App { println("hello") }`
	ents := extract(t, "custom_scala_akka", fi("Main.scala", "scala", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
