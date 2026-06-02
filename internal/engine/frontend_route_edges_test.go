// Tests for the frontend route -> component graph pass (epic #3628).
//
// Value-asserting: every positive test asserts a SPECIFIC SCOPE.Route node
// (id `feroute:<file>:<path>`, synthesis="frontend_routing", scope="client")
// AND a SPECIFIC ROUTES_TO edge from it to the named component — never len>0.
//
//   - React Router JSX   <Route path="/users" element={<Users/>} />
//     → feroute:App.tsx:/users  ROUTES_TO  Users
//   - React Router v5    <Route path="/about" component={About} />
//     → feroute:App.tsx:/about  ROUTES_TO  About
//   - Vue Router         { path: '/u/:id', component: UserDetail }
//     → feroute:router.ts:/u/:id  ROUTES_TO  UserDetail
//   - Angular            { path: 'users', component: UsersComponent }
//     → feroute:app.routes.ts:users  ROUTES_TO  UsersComponent
//   - createBrowserRouter { path: '/dash', element: <Dashboard/> }
//     → feroute:routes.tsx:/dash  ROUTES_TO  Dashboard
//
// Negatives: a dynamic path `{ path: pathVar }` mints no node; a <Route> with
// no resolvable element/component mints no edge; a non-JS/TS file no-ops.
package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func runFERouteDetect(t *testing.T, lang, path, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	res := applyFrontendRouteEdges(DetectorPassArgs{Lang: lang, Path: path, Content: []byte(src)})
	return res.Entities, res.Relationships
}

// feRouteNode returns the SCOPE.Route entity with the given id, or nil.
func feRouteNode(ents []types.EntityRecord, id string) *types.EntityRecord {
	for i := range ents {
		if ents[i].ID == id && ents[i].Kind == feRouteKind {
			return &ents[i]
		}
	}
	return nil
}

// feRoutesToEdge returns the ROUTES_TO edge fromID->toComponent, or nil.
func feRoutesToEdge(rels []types.RelationshipRecord, fromID, toComponent string) *types.RelationshipRecord {
	for i := range rels {
		if rels[i].Kind == feRouteRoutesTo && rels[i].FromID == fromID && rels[i].ToID == toComponent {
			return &rels[i]
		}
	}
	return nil
}

func assertFERoute(t *testing.T, ents []types.EntityRecord, rels []types.RelationshipRecord, routeID, component, framework string) {
	t.Helper()
	node := feRouteNode(ents, routeID)
	if node == nil {
		t.Fatalf("expected SCOPE.Route node %q, got entities: %+v", routeID, ents)
	}
	if got := node.Properties["synthesis"]; got != "frontend_routing" {
		t.Errorf("node %s synthesis=%q, want frontend_routing", routeID, got)
	}
	if got := node.Properties["scope"]; got != "client" {
		t.Errorf("node %s scope=%q, want client", routeID, got)
	}
	if framework != "" {
		if got := node.Properties["framework"]; got != framework {
			t.Errorf("node %s framework=%q, want %q", routeID, got, framework)
		}
	}
	edge := feRoutesToEdge(rels, routeID, component)
	if edge == nil {
		t.Fatalf("expected ROUTES_TO %s -> %s, got rels: %+v", routeID, component, rels)
	}
	if got := edge.Properties["synthesis"]; got != "frontend_routing" {
		t.Errorf("edge %s->%s synthesis=%q, want frontend_routing", routeID, component, got)
	}
}

func TestFERoute_ReactRouter_JSXElement(t *testing.T) {
	src := `
import { Routes, Route } from "react-router-dom";
import Users from "./Users";
export function App() {
  return (
    <Routes>
      <Route path="/users" element={<Users/>} />
    </Routes>
  );
}`
	ents, rels := runFERouteDetect(t, "typescript", "App.tsx", src)
	assertFERoute(t, ents, rels, "feroute:App.tsx:/users", "Users", "react_router")
}

func TestFERoute_ReactRouter_V5ComponentProp(t *testing.T) {
	src := `
import { Route } from "react-router-dom";
import About from "./About";
const r = <Route path="/about" component={About} />;`
	ents, rels := runFERouteDetect(t, "typescript", "App.tsx", src)
	assertFERoute(t, ents, rels, "feroute:App.tsx:/about", "About", "react_router")
}

func TestFERoute_VueRouter_ObjectTable(t *testing.T) {
	src := `
import { createRouter, createWebHistory } from "vue-router";
import UserDetail from "./UserDetail.vue";
const routes = [
  { path: '/u/:id', component: UserDetail },
];
export default createRouter({ history: createWebHistory(), routes });`
	ents, rels := runFERouteDetect(t, "typescript", "router.ts", src)
	assertFERoute(t, ents, rels, "feroute:router.ts:/u/:id", "UserDetail", "vue_router")
}

func TestFERoute_Angular_RouterModule(t *testing.T) {
	src := `
import { RouterModule } from "@angular/router";
import { UsersComponent } from "./users.component";
const routes = [{ path: 'users', component: UsersComponent }];
@NgModule({ imports: [RouterModule.forRoot(routes)] })
export class AppRoutingModule {}`
	ents, rels := runFERouteDetect(t, "typescript", "app.routes.ts", src)
	assertFERoute(t, ents, rels, "feroute:app.routes.ts:users", "UsersComponent", "angular")
}

func TestFERoute_CreateBrowserRouter_ElementObject(t *testing.T) {
	src := `
import { createBrowserRouter } from "react-router-dom";
import Dashboard from "./Dashboard";
const router = createBrowserRouter([
  { path: '/dash', element: <Dashboard/> },
]);`
	ents, rels := runFERouteDetect(t, "typescript", "routes.tsx", src)
	assertFERoute(t, ents, rels, "feroute:routes.tsx:/dash", "Dashboard", "react_router")
}

// Distinct-from-backend: the frontend route node id never collides with a
// backend/api-gateway SCOPE.Route id, even on the same path string.
func TestFERoute_DistinctFromBackendRouteID(t *testing.T) {
	src := `
import { Route } from "react-router-dom";
import Users from "./Users";
const r = <Route path="/users" element={<Users/>} />;`
	ents, _ := runFERouteDetect(t, "typescript", "App.tsx", src)
	node := feRouteNode(ents, "feroute:App.tsx:/users")
	if node == nil {
		t.Fatal("expected frontend route node")
	}
	// Backend api-gateway nodes are keyed "route:<tool>:…"; assert no overlap.
	if got := node.ID; got == "route:/users" || got == "route:react_router:App.tsx:/users" {
		t.Errorf("frontend route id %q collides with a backend route key shape", got)
	}
	if node.Properties["synthesis"] == "api_gateway_routing" {
		t.Error("frontend route incorrectly tagged as api_gateway_routing")
	}
}

// NEGATIVE: a dynamic (template-literal interpolated) path mints no node.
func TestFERoute_DynamicPath_NoNode(t *testing.T) {
	src := "import { Route } from \"react-router-dom\";\n" +
		"const p = base + '/x';\n" +
		"const r = <Route path={`${base}/users`} element={<Users/>} />;"
	ents, rels := runFERouteDetect(t, "typescript", "App.tsx", src)
	for _, e := range ents {
		if e.Kind == feRouteKind {
			t.Errorf("dynamic path should mint no SCOPE.Route node, got %q", e.ID)
		}
	}
	for _, r := range rels {
		if r.Kind == feRouteRoutesTo {
			t.Errorf("dynamic path should mint no ROUTES_TO edge, got %s->%s", r.FromID, r.ToID)
		}
	}
}

// NEGATIVE: a <Route> with no resolvable element/component mints no edge
// (a layout/index route with only children).
func TestFERoute_NoComponent_NoEdge(t *testing.T) {
	src := `
import { Route } from "react-router-dom";
const r = <Route path="/layout"></Route>;`
	ents, rels := runFERouteDetect(t, "typescript", "App.tsx", src)
	if n := feRouteNode(ents, "feroute:App.tsx:/layout"); n != nil {
		t.Errorf("route with no component should mint no node, got %q", n.ID)
	}
	for _, r := range rels {
		if r.Kind == feRouteRoutesTo {
			t.Errorf("route with no component should mint no edge, got %s->%s", r.FromID, r.ToID)
		}
	}
}

// NEGATIVE: a non-JS/TS file is a fast no-op even if it mentions "routes".
func TestFERoute_NonJSFile_NoOp(t *testing.T) {
	src := `routes:
  - path: /users
    component: Users`
	ents, rels := runFERouteDetect(t, "yaml", "routes.yaml", src)
	for _, e := range ents {
		if e.Kind == feRouteKind {
			t.Errorf("non-JS file should no-op, got node %q", e.ID)
		}
	}
	if len(rels) != 0 {
		t.Errorf("non-JS file should emit no edges, got %d", len(rels))
	}
}

// Append-only: the pass returns pre-existing entities/edges untouched.
func TestFERoute_AppendOnly(t *testing.T) {
	pre := []types.EntityRecord{{ID: "pre:1", Kind: "SCOPE.Function"}}
	preRel := []types.RelationshipRecord{{FromID: "a", ToID: "b", Kind: "CALLS"}}
	res := applyFrontendRouteEdges(DetectorPassArgs{
		Lang: "typescript", Path: "App.tsx",
		Content:       []byte(`<Route path="/x" element={<X/>} />`),
		Entities:      pre,
		Relationships: preRel,
	})
	if len(res.Entities) < 1 || res.Entities[0].ID != "pre:1" {
		t.Error("pre-existing entity was modified or dropped")
	}
	if len(res.Relationships) < 1 || res.Relationships[0].Kind != "CALLS" {
		t.Error("pre-existing relationship was modified or dropped")
	}
}
