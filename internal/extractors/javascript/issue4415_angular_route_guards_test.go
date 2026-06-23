package javascript_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors/javascript"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// Issue #4415 LIVE-REPRO — Angular route-config guard/resolver wiring.
//
// Angular binds guard and resolver CLASSES to a route declaratively in the
// route-config array ({ path, canActivate:[Guard], resolve:{x:Res}, canMatch:
// [...] }). #4378 covered NgModule / standalone-bootstrap providers but
// deferred route guards because route extraction lives on a separate path.
//
// PRE-FIX: the route-config guard/resolver classes had NO inbound edge from the
// route declaration — only their IMPLEMENTS edge to the guard interface — so a
// "what does this route guard with?" query returned nothing and the classes
// looked disconnected from their use site.
//
// POST-FIX: each route emits a route → guard/resolver CLASS USES edge
// (di_role=guard|resolver, via=angular_route_config). Array form, resolve
// object-map and best-effort functional guards (inject(Service)) are all
// covered. Every target resolves to the real class entity through
// resolve.BuildIndex.
//
// The test runs the REAL JSExtractor.Extract pipeline (AST walk + the new
// program-level pass) and the REAL resolver over faithful Angular fixtures.

func extractTS4415(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	tree := parseTS(t, []byte(src))
	e := javascript.New()
	ents, err := e.Extract(context.Background(), extreg.FileInput{
		Path:     path,
		Language: "typescript",
		Content:  []byte(src),
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("extract %s: %v", path, err)
	}
	return ents
}

// routeUsesRole returns the di_role of the first USES edge to target, or "".
func routeUsesRole(ents []types.EntityRecord, target string) string {
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindUses) && r.ToID == target &&
				r.Properties["via"] == "angular_route_config" {
				return r.Properties["di_role"]
			}
		}
	}
	return ""
}

const ngRouteGuardsFixture = `
import { NgModule } from '@angular/core';
import { RouterModule, Routes } from '@angular/router';

const routes: Routes = [
  {
    path: 'admin',
    component: AdminComponent,
    canActivate: [AuthGuard, RoleGuard],
    canActivateChild: [ChildGuard],
    canDeactivate: [UnsavedGuard],
    canMatch: [FeatureGuard],
    resolve: { user: UserResolver, prefs: PrefsResolver },
  },
  {
    path: 'public',
    component: PublicComponent,
  },
];

@Injectable() export class AuthGuard {}
@Injectable() export class RoleGuard {}
@Injectable() export class ChildGuard {}
@Injectable() export class UnsavedGuard {}
@Injectable() export class FeatureGuard {}
@Injectable() export class UserResolver {}
@Injectable() export class PrefsResolver {}

@NgModule({ imports: [RouterModule.forRoot(routes)] })
export class AppRoutingModule {}
`

// TestIssue4415_RouteGuardsAndResolvers proves the core fix: a route with
// canActivate + resolve emits USES edges to each guard (di_role=guard) and each
// resolver (di_role=resolver), and every target resolves to its real class.
func TestIssue4415_RouteGuardsAndResolvers(t *testing.T) {
	ents := extractTS4415(t, "src/app/app-routing.module.ts", ngRouteGuardsFixture)

	wantGuards := []string{"AuthGuard", "RoleGuard", "ChildGuard", "UnsavedGuard", "FeatureGuard"}
	for _, g := range wantGuards {
		if role := routeUsesRole(ents, g); role != "guard" {
			t.Errorf("guard %s: want route USES di_role=guard, got %q", g, role)
		}
	}
	for _, r := range []string{"UserResolver", "PrefsResolver"} {
		if role := routeUsesRole(ents, r); role != "resolver" {
			t.Errorf("resolver %s: want route USES di_role=resolver, got %q", r, role)
		}
	}

	// Every guard/resolver target resolves to its real declaring class entity.
	idx := resolve.BuildIndex(ents)
	for _, name := range append(append([]string{}, wantGuards...), "UserResolver", "PrefsResolver") {
		if _, ok := idx.Lookup(name); !ok {
			t.Errorf("route guard/resolver target %s failed to resolve", name)
		}
	}
}

// TestIssue4415_MultipleGuardsArray asserts each class in a canActivate array
// gets its own edge (not just the first).
func TestIssue4415_MultipleGuardsArray(t *testing.T) {
	ents := extractTS4415(t, "src/app/app-routing.module.ts", ngRouteGuardsFixture)
	count := 0
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindUses) &&
				r.Properties["via"] == "angular_route_config" &&
				r.Properties["route_key"] == "canActivate" {
				count++
			}
		}
	}
	if count != 2 {
		t.Errorf("canActivate:[AuthGuard, RoleGuard] should yield 2 USES edges, got %d", count)
	}
}

// TestIssue4415_NoGuardsNoSpuriousEdge is the regression invariant: a route with
// no guard/resolver keys ({path, component}) emits no route-config USES edge.
func TestIssue4415_NoGuardsNoSpuriousEdge(t *testing.T) {
	const fixture = `
import { Routes } from '@angular/router';
const routes: Routes = [
  { path: 'home', component: HomeComponent },
  { path: 'about', component: AboutComponent },
];
`
	ents := extractTS4415(t, "src/app/routes.ts", fixture)
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindUses) &&
				r.Properties["via"] == "angular_route_config" {
				t.Fatalf("guard-free route emitted a spurious USES edge to %s", r.ToID)
			}
		}
	}
}

// TestIssue4415_FunctionalGuard covers the modern functional-guard form: an
// inline `() => inject(AuthService).isLoggedIn()` best-effort links to the
// injected service (di_role=guard, functional). A functional guard with no
// statically recoverable inject() target is skipped honestly.
func TestIssue4415_FunctionalGuard(t *testing.T) {
	const fixture = `
import { Routes } from '@angular/router';
import { inject } from '@angular/core';

const routes: Routes = [
  {
    path: 'secure',
    canActivate: [() => inject(AuthService).isLoggedIn()],
    canMatch: [() => true],
  },
];

@Injectable() export class AuthService { isLoggedIn() { return true; } }
`
	ents := extractTS4415(t, "src/app/secure.routes.ts", fixture)

	if role := routeUsesRole(ents, "AuthService"); role != "guard" {
		t.Errorf("functional guard should link to injected AuthService (di_role=guard), got %q", role)
	}
	// The `() => true` functional guard has no inject() target → no edge, and no
	// crash. Confirm AuthService resolves (it is declared in-file).
	idx := resolve.BuildIndex(ents)
	if _, ok := idx.Lookup("AuthService"); !ok {
		t.Errorf("functional-guard injected service AuthService failed to resolve")
	}
}
