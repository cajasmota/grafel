// Package javascript — issue #2856 Angular Navigation + Lifecycle proving
// tests.
//
// Flips two Angular cells (missing→full):
//   - Navigation / router_pattern        : RouterModule.forRoot route table,
//     this.router.navigate([...]) / navigateByUrl, routerLink template directive
//     → NAVIGATES_TO edges.
//   - Lifecycle / state_setter_emission  : signal().set/.update + ngrx dispatch
//     → state_setter operations + WRITES_TO edges to the mutated state.
package javascript_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func TestIssue2856_AngularNavigation(t *testing.T) {
	src := []byte("import { Component } from '@angular/core';\n" +
		"import { Router, RouterModule } from '@angular/router';\n" +
		"\n" +
		"@Component({\n" +
		"  selector: 'app-nav',\n" +
		"  template: `<a routerLink=\"/home\">Home</a><a [routerLink]=\"['/users', id]\">User</a>`\n" +
		"})\n" +
		"export class NavComponent {\n" +
		"  constructor(private router: Router) {}\n" +
		"\n" +
		"  goHome() {\n" +
		"    this.router.navigate(['/dashboard']);\n" +
		"    this.router.navigateByUrl('/settings');\n" +
		"  }\n" +
		"}\n" +
		"\n" +
		"@Component({ selector: 'app-routing', template: '' })\n" +
		"export class AppRoutingModule {\n" +
		"  routes = RouterModule.forRoot([\n" +
		"    { path: 'home', component: NavComponent },\n" +
		"    { path: 'users/:id', component: NavComponent },\n" +
		"  ]);\n" +
		"}\n")

	ents := extractReact(t, "nav.component.ts", src)

	comp := findByName(ents, "NavComponent")
	if comp == nil {
		t.Fatalf("NavComponent not extracted; %s", dumpKinds(ents))
	}

	routes := map[string]bool{}
	vias := map[string]bool{}
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindNavigatesTo) {
				routes[r.Properties["route"]] = true
				vias[r.Properties["via"]] = true
			}
		}
	}

	// Imperative navigation.
	if !routes["/dashboard"] {
		t.Errorf("missing NAVIGATES_TO /dashboard (router.navigate); routes=%v vias=%v", routes, vias)
	}
	if !routes["/settings"] {
		t.Errorf("missing NAVIGATES_TO /settings (navigateByUrl); routes=%v", routes)
	}
	// routerLink directives.
	if !routes["/home"] {
		t.Errorf("missing NAVIGATES_TO /home (routerLink string form); routes=%v", routes)
	}
	if !routes["/users/{*}"] {
		t.Errorf("missing NAVIGATES_TO /users/{*} (routerLink array binding); routes=%v", routes)
	}
	// Route-table declarations.
	routingComp := findByName(ents, "AppRoutingModule")
	if routingComp == nil {
		t.Fatalf("AppRoutingModule not extracted")
	}
	if !routes["home"] || !routes["users/:id"] {
		t.Errorf("missing route-table NAVIGATES_TO (home, users/:id); routes=%v", routes)
	}
	if !vias["angular_router"] || !vias["router_link"] || !vias["route_table"] {
		t.Errorf("expected all three navigation vias present; got %v", vias)
	}
}

func TestIssue2856_AngularStateSetter(t *testing.T) {
	src := []byte("import { Component, signal } from '@angular/core';\n" +
		"import { Store } from '@ngrx/store';\n" +
		"import { loadUser } from './actions';\n" +
		"\n" +
		"@Component({ selector: 'app-counter', template: '' })\n" +
		"export class CounterComponent {\n" +
		"  count = signal(0);\n" +
		"  name = signal<string>('');\n" +
		"\n" +
		"  constructor(private store: Store) {}\n" +
		"\n" +
		"  inc() {\n" +
		"    this.count.set(1);\n" +
		"    this.count.update(c => c + 1);\n" +
		"    this.name.set('x');\n" +
		"    this.store.dispatch(loadUser());\n" +
		"  }\n" +
		"}\n")

	ents := extractReact(t, "counter.component.ts", src)

	// state_setter operations.
	setters := map[string]string{} // name → state
	for _, e := range ents {
		if e.Subtype == "state_setter" {
			setters[e.Name] = e.Properties["state"]
		}
	}
	for name, wantState := range map[string]string{
		"count.set":        "count",
		"count.update":     "count",
		"name.set":         "name",
		"dispatch:loadUser": "loadUser",
	} {
		if got, ok := setters[name]; !ok {
			t.Errorf("missing state_setter %q; setters=%v; %s", name, setters, dumpKinds(ents))
		} else if got != wantState {
			t.Errorf("state_setter %q: state=%q, want %q", name, got, wantState)
		}
	}

	// WRITES_TO edges from each setter to its state.
	writes := map[string]bool{} // "state" target
	for _, e := range ents {
		if e.Subtype != "state_setter" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindWritesTo) {
				writes[r.ToID] = true
			}
		}
	}
	for _, target := range []string{"state:count", "state:name", "state:loadUser"} {
		if !writes[target] {
			t.Errorf("missing WRITES_TO %s; writes=%v", target, writes)
		}
	}
}

// TestIssue2856_AngularNoFalsePositiveSetters guards that a non-signal .set call
// (e.g. a Set.add / Map.set on an unrelated field) does not produce a
// state_setter when the receiver is not a known signal binding.
func TestIssue2856_AngularNoFalsePositiveSetters(t *testing.T) {
	src := []byte("import { Component } from '@angular/core';\n" +
		"@Component({ selector: 'app-x', template: '' })\n" +
		"export class XComponent {\n" +
		"  cache = new Map();\n" +
		"  go() { this.cache.set('k', 1); }\n" +
		"}\n")
	ents := extractReact(t, "x.component.ts", src)
	for _, e := range ents {
		if e.Subtype == "state_setter" {
			t.Errorf("unexpected state_setter for non-signal receiver: %q", e.Name)
		}
	}
}
