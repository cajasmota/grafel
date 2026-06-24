package engine

import "testing"

// TestDream_BasicRoutes covers the common Dream verb shapes inside a
// Dream.router list: Dream.get "/" / Dream.post "/users".
func TestDream_BasicRoutes(t *testing.T) {
	src := `
let () =
  Dream.run
  @@ Dream.logger
  @@ Dream.router [
       Dream.get "/" (fun _ -> Dream.html "home");
       Dream.get "/users" list_users;
       Dream.post "/users" create_user;
     ]
`
	ids, _ := runDetect(t, "ocaml", "lib/app.ml", src)
	requireContains(t, ids, []string{
		"http:GET:/",
		"http:GET:/users",
		"http:POST:/users",
	}, "dream-basic-routes")
}

// TestDream_PathParam covers the Sinatra-style `:param` dynamic segment:
// Dream.get "/users/:id" → GET /users/{id}.
func TestDream_PathParam(t *testing.T) {
	src := `
let routes = Dream.router [
  Dream.get "/users/:id" show_user;
  Dream.delete "/users/:id" delete_user;
  Dream.put "/users/:id" update_user;
]
`
	ids, _ := runDetect(t, "ocaml", "lib/routes.ml", src)
	requireContains(t, ids, []string{
		"http:GET:/users/{id}",
		"http:DELETE:/users/{id}",
		"http:PUT:/users/{id}",
	}, "dream-path-param")
}

// TestDream_NonWebFileIgnored is the negative guard: an OCaml file with no
// Dream marker that happens to call a function named `get` must not synthesize
// an endpoint.
func TestDream_NonWebFileIgnored(t *testing.T) {
	src := `
module Cache = struct
  let lookup store key = get store key
end
`
	ids, _ := runDetect(t, "ocaml", "lib/cache.ml", src)
	for _, id := range ids {
		if id == "http:GET:/store" || id == "http:GET:/key" {
			t.Fatalf("non-web file must not synthesize an endpoint; got %v", ids)
		}
	}
}

// TestDream_VariablePathExcluded confirms the honest exclusion: a non-literal
// (variable) path argument is NOT synthesized.
func TestDream_VariablePathExcluded(t *testing.T) {
	src := `
let mount prefix handler =
  Dream.router [ Dream.get prefix handler ]
let real = Dream.router [ Dream.get "/health" health ]
`
	ids, _ := runDetect(t, "ocaml", "lib/dyn.ml", src)
	// The literal route IS emitted; the variable-path one is not.
	requireContains(t, ids, []string{"http:GET:/health"}, "dream-literal-route")
	for _, id := range ids {
		if id == "http:GET:prefix" || id == "http:GET:/prefix" {
			t.Fatalf("variable-path route must not be synthesized; got %v", ids)
		}
	}
}
