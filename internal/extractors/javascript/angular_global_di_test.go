package javascript_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors/javascript"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// Issue #4378 LIVE-REPRO — Angular global DI wiring.
//
// Angular binds cross-cutting providers app-wide via the {provide, useClass/
// useExisting/useFactory, multi} provider-object shape inside an @NgModule
// providers array and inside a standalone bootstrapApplication options object —
// the same mechanism the NestJS fix (#4329) handled for APP_*.
//
// PRE-FIX: the JS/TS extractor read the @NgModule class + constructor injections
// but never the providers array, so the bound HTTP interceptor / APP_INITIALIZER
// factory / global ErrorHandler classes had NO inbound edge from the module and
// looked orphan / dead; the standalone bootstrap producers were invisible
// entirely.
//
// POST-FIX: the module emits module → bound-class USES edges (global=true +
// di_token + di_role + multi); the standalone bootstrap emits a synthetic `app`
// entity that USES each bound producer. Every target resolves to the real class
// entity through resolve.BuildIndex.
//
// The test runs the REAL JSExtractor.Extract pipeline (AST walk + the new
// program-level pass) and the REAL resolver over faithful Angular fixtures.

func extractTS4378(t *testing.T, path, src string) []types.EntityRecord {
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

// usesEdgeProp returns the property value for the first USES edge whose ToID is
// target (and, when owner != "", whose FromID is owner), or "" when absent.
func usesEdgeProp(ents []types.EntityRecord, owner, target, key string) (string, bool) {
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != string(types.RelationshipKindUses) || r.ToID != target {
				continue
			}
			if owner != "" && r.FromID != owner {
				continue
			}
			return r.Properties[key], true
		}
	}
	return "", false
}

const ngModuleProvidersFixture = `
import { NgModule, ErrorHandler, APP_INITIALIZER } from '@angular/core';
import { HTTP_INTERCEPTORS } from '@angular/common/http';

@Injectable()
export class AuthService {}

@Injectable()
export class AuthInterceptor {}

@Injectable()
export class GlobalErrorHandler implements ErrorHandler {
  handleError(e: unknown) {}
}

export function initApp() { return () => Promise.resolve(); }

@NgModule({
  declarations: [],
  imports: [],
  providers: [
    AuthService,
    { provide: HTTP_INTERCEPTORS, useClass: AuthInterceptor, multi: true },
    { provide: APP_INITIALIZER, useFactory: initApp, multi: true },
    { provide: ErrorHandler, useClass: GlobalErrorHandler },
  ],
})
export class AppModule {}
`

// TestIssue4378_NgModuleProviders asserts each @NgModule provider yields a
// module → class USES edge (global=true) with the right di_token/di_role/multi,
// and that the previously-orphan classes resolve in the symbol table.
func TestIssue4378_NgModuleProviders(t *testing.T) {
	ents := extractTS4378(t, "src/app/app.module.ts", ngModuleProvidersFixture)

	type want struct {
		target string
		token  string
		role   string
		multi  bool
	}
	wants := []want{
		{"AuthService", "AuthService", "service", false},
		{"AuthInterceptor", "HTTP_INTERCEPTORS", "interceptor", true},
		{"initApp", "APP_INITIALIZER", "initializer", true},
		{"GlobalErrorHandler", "ErrorHandler", "error_handler", false},
	}

	for _, w := range wants {
		if !hasUses(ents, "AppModule", w.target) {
			t.Errorf("expected AppModule USES %s (token %s)", w.target, w.token)
			continue
		}
		if v, _ := usesEdgeProp(ents, "AppModule", w.target, "global"); v != "true" {
			t.Errorf("%s: expected global=true, got %q", w.target, v)
		}
		if v, _ := usesEdgeProp(ents, "AppModule", w.target, "di_token"); v != w.token {
			t.Errorf("%s: expected di_token=%s, got %q", w.target, w.token, v)
		}
		if v, _ := usesEdgeProp(ents, "AppModule", w.target, "di_role"); v != w.role {
			t.Errorf("%s: expected di_role=%s, got %q", w.target, w.role, v)
		}
		wantMulti := ""
		if w.multi {
			wantMulti = "true"
		}
		if v, _ := usesEdgeProp(ents, "AppModule", w.target, "multi"); v != wantMulti {
			t.Errorf("%s: expected multi=%q, got %q", w.target, wantMulti, v)
		}
	}

	// The previously-orphan interceptor must RESOLVE to its real class entity
	// through the real resolver (it is declared in this same file as a class).
	idx := resolve.BuildIndex(ents)
	for _, name := range []string{"AuthInterceptor", "GlobalErrorHandler", "AuthService"} {
		if _, ok := idx.Lookup(name); !ok {
			t.Errorf("global provider target %s failed to resolve — would stay orphan", name)
		}
	}
}

// TestIssue4378_NgModuleProviders_RedGreen proves the linkage did not exist
// before the providers array was read: with the fix, the bound interceptor has
// an inbound USES edge from the module; without any provider parsing it would
// have none. We assert the edge count is non-zero (the green side); the red side
// is documented by the orphan-class problem in the issue.
func TestIssue4378_BoundClassesNoLongerOrphan(t *testing.T) {
	ents := extractTS4378(t, "src/app/app.module.ts", ngModuleProvidersFixture)
	for _, target := range []string{"AuthInterceptor", "initApp", "GlobalErrorHandler"} {
		if !hasUses(ents, "AppModule", target) {
			t.Fatalf("%s has no inbound module USES edge — still orphan", target)
		}
	}
}

const standaloneBootstrapFixture = `
import { bootstrapApplication } from '@angular/platform-browser';
import { provideHttpClient, withInterceptors } from '@angular/common/http';
import { provideRouter } from '@angular/router';
import { ErrorHandler } from '@angular/core';

@Injectable()
export class GlobalErrorHandler {}

export const authInterceptor = (req, next) => next(req);
export const loggingInterceptor = (req, next) => next(req);

bootstrapApplication(AppComponent, {
  providers: [
    provideHttpClient(withInterceptors([authInterceptor, loggingInterceptor])),
    provideRouter(routes),
    { provide: ErrorHandler, useClass: GlobalErrorHandler },
  ],
});
`

// TestIssue4378_StandaloneBootstrap asserts the standalone bootstrap emits a
// synthetic `app` entity USES-linked to its functional interceptors and object
// providers (global=true), and that those targets resolve.
func TestIssue4378_StandaloneBootstrap(t *testing.T) {
	ents := extractTS4378(t, "src/main.ts", standaloneBootstrapFixture)

	// Synthetic app entity must exist.
	if findByName(ents, "app") == nil {
		t.Fatalf("expected synthetic `app` entity for standalone bootstrap; names=%v", entityNames(ents))
	}

	checks := []struct {
		target string
		role   string
	}{
		{"authInterceptor", "interceptor"},
		{"loggingInterceptor", "interceptor"},
		{"GlobalErrorHandler", "error_handler"},
	}
	for _, c := range checks {
		if !hasUses(ents, "app", c.target) {
			t.Errorf("expected app USES %s (%s)", c.target, c.role)
			continue
		}
		if v, _ := usesEdgeProp(ents, "app", c.target, "global"); v != "true" {
			t.Errorf("%s: expected global=true, got %q", c.target, v)
		}
		if v, _ := usesEdgeProp(ents, "app", c.target, "di_role"); v != c.role {
			t.Errorf("%s: expected di_role=%s, got %q", c.target, c.role, v)
		}
	}

	// Functional interceptors resolve to their exported const arrow declarations.
	idx := resolve.BuildIndex(ents)
	for _, name := range []string{"authInterceptor", "loggingInterceptor", "GlobalErrorHandler"} {
		if _, ok := idx.Lookup(name); !ok {
			t.Errorf("bootstrap provider %s failed to resolve", name)
		}
	}
}

// TestIssue4378_NoFalseGlobals ensures an ordinary Angular component file with
// no providers array produces no global USES edges.
func TestIssue4378_NoFalseGlobals(t *testing.T) {
	src := `
import { Component } from '@angular/core';
@Component({ selector: 'app-root', template: '<div></div>' })
export class AppComponent {
  constructor(private http: HttpClient) {}
}
`
	ents := extractTS4378(t, "src/app/app.component.ts", src)
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindUses) && r.Properties["global"] == "true" {
				t.Errorf("plain component should not produce a global USES edge, got %+v", r)
			}
		}
	}
	if findByName(ents, "app") != nil {
		t.Error("no synthetic app entity should be emitted without a bootstrapApplication call")
	}
}

// hasUses reports whether any entity carries a USES edge owner→target.
func hasUses(ents []types.EntityRecord, owner, target string) bool {
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindUses) && r.FromID == owner && r.ToID == target {
				return true
			}
		}
	}
	return false
}
