package engine

import (
	"testing"
)

// TestShelf_BasicRoute covers the common shelf_router cascade shape:
// Router()..get('/todos', h)..post('/todos', h).
func TestShelf_BasicRoute(t *testing.T) {
	src := `
import 'package:shelf_router/shelf_router.dart';

final router = Router()
  ..get('/todos', listTodos)
  ..post('/todos', createTodo);
`
	ids, _ := runDetect(t, "dart", "lib/routes.dart", src)
	requireContains(t, ids, []string{
		"http:GET:/todos",
		"http:POST:/todos",
	}, "shelf-basic-route")
}

// TestShelf_PathParam covers shelf_router `<id>` and `<id|regex>` angle-bracket
// dynamic segments → /todos/{id}.
func TestShelf_PathParam(t *testing.T) {
	src := `
import 'package:shelf_router/shelf_router.dart';

final router = Router();
router.get('/todos/<id>', getTodo);
router.delete('/todos/<id|[0-9]+>', deleteTodo);
`
	ids, _ := runDetect(t, "dart", "lib/todos.dart", src)
	requireContains(t, ids, []string{
		"http:GET:/todos/{id}",
		"http:DELETE:/todos/{id}",
	}, "shelf-path-param")
}

// TestDartFrog_FileRoute covers dart_frog file-based routing: a
// routes/users/[id]/index.dart with onRequest → ANY /users/{id}, and a static
// verb switch yields the precise verb.
func TestDartFrog_FileRoute(t *testing.T) {
	src := `
import 'package:dart_frog/dart_frog.dart';

Response onRequest(RequestContext context) {
  return Response(body: 'user');
}
`
	ids, _ := runDetect(t, "dart", "routes/users/[id]/index.dart", src)
	requireContains(t, ids, []string{
		"http:ANY:/users/{id}",
	}, "dart-frog-file-route")
}

// TestDartFrog_VerbSwitch covers a dart_frog handler whose verb is statically
// dispatched via HttpMethod → the precise verb is emitted.
func TestDartFrog_VerbSwitch(t *testing.T) {
	src := `
import 'package:dart_frog/dart_frog.dart';

Response onRequest(RequestContext context) {
  if (context.request.method == HttpMethod.post) {
    return Response(body: 'created');
  }
  return Response(body: 'ok');
}
`
	ids, _ := runDetect(t, "dart", "routes/items/index.dart", src)
	requireContains(t, ids, []string{
		"http:POST:/items",
	}, "dart-frog-verb-switch")
}

// TestConduit_Route covers Conduit `router.route("/users/[:id]")` → ANY
// /users/{id} (optional `[:id]` param).
func TestConduit_Route(t *testing.T) {
	src := `
import 'package:conduit/conduit.dart';

void setupRouter(Router router) {
  router.route("/users/[:id]").link(() => UserController());
}
`
	ids, _ := runDetect(t, "dart", "lib/channel.dart", src)
	requireContains(t, ids, []string{
		"http:ANY:/users/{id}",
	}, "conduit-route")
}

// TestShelf_InterpolatedRouteDropped asserts a non-static (interpolated) shelf
// path is NOT synthesized — no fabricated endpoint.
func TestShelf_InterpolatedRouteDropped(t *testing.T) {
	src := `
import 'package:shelf_router/shelf_router.dart';

final router = Router();
router.get('/api/$version/x', handler);
`
	ids, _ := runDetect(t, "dart", "lib/dyn.dart", src)
	for _, id := range ids {
		if id == "http:GET:/api/x" || id == "http:GET:/api/$version/x" {
			t.Fatalf("interpolated route should not synthesize an endpoint; got %q", id)
		}
	}
}

// TestShelf_NonWebFileIgnored asserts a plain Dart file with a `.get(` call but
// no shelf/dart_frog/conduit web marker produces no endpoints.
func TestShelf_NonWebFileIgnored(t *testing.T) {
	src := `
void handle() {
  final x = cache.get('key');
  print(x);
}
`
	ids, _ := runDetect(t, "dart", "lib/util.dart", src)
	for _, id := range ids {
		if len(id) >= 5 && id[:5] == "http:" {
			t.Fatalf("non-web Dart file should synthesize no endpoint; got %q", id)
		}
	}
}
