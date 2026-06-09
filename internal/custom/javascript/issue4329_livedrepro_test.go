package javascript_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/resolve"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/javascript"
)

// Issue #4329 LIVE-REPRO.
//
// Byte-copies of REAL core-backend-v3 files are committed under
// testdata/issue4329:
//
//   - common.module.ts  — registers APP_FILTER / APP_INTERCEPTOR (x3) /
//     APP_PIPE / APP_GUARD via the object-form provider shape
//     ({ provide: APP_*, useClass|useFactory: Impl }) and applies global
//     middleware via consumer.apply(...).forRoutes('*').
//   - main.ts           — the real bootstrap (setGlobalPrefix; NestFactory).
//
// Plus a SYNTHETIC main_globals.ts exercising the app.useGlobal*() path that
// the real main.ts does not happen to use, so the global-wiring code path is
// covered against representative source.
//
// PRE-FIX: the APP_* object providers produced only a token→impl BINDS edge
// whose FromID was the phantom magic token (APP_INTERCEPTOR has no entity), so
// the interceptor/guard/filter/pipe classes had NO edge from the real module
// entity — they looked like orphan / dead code, and the app-wide scope was
// invisible. app.useGlobal*() produced nothing at all.
//
// POST-FIX: the module emits a USES edge (module → bound class) tagged
// global=true + di_token=APP_* + di_role, and app.useGlobal*() emits a USES
// edge (app → class) tagged global=true. Both resolve to the real class entity
// through resolve.BuildIndex.

func loadNest4329(t *testing.T, base, repoPath string) (string, []byte) {
	t.Helper()
	p := filepath.Join("testdata", "issue4329", base)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return repoPath, b
}

func nestExtract4329(t *testing.T, path string, content []byte) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_js_nestjs")
	if !ok {
		t.Fatal("custom_js_nestjs not registered")
	}
	ents, err := e.Extract(context.Background(),
		extreg.FileInput{Path: path, Language: "typescript", Content: content})
	if err != nil {
		t.Fatalf("nest extract: %v", err)
	}
	return ents
}

func usesEdges(ents []types.EntityRecord) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindUses) {
				out = append(out, r)
			}
		}
	}
	return out
}

// TestIssue4329_AppGlobalProviders_ModuleUsesBoundClass runs the REAL
// common.module.ts through the actual NestJS extractor and asserts that each
// APP_* global provider yields a module→class USES edge with global=true and
// the right di_role/di_token, and that the bound class resolves in the symbol
// table (so the previously-orphan interceptor/guard/filter is now connected).
func TestIssue4329_AppGlobalProviders_ModuleUsesBoundClass(t *testing.T) {
	path, content := loadNest4329(t, "common.module.ts", "src/common/common.module.ts")
	ents := nestExtract4329(t, path, content)

	type want struct {
		class string
		role  string
		token string
	}
	wants := []want{
		{"AllExceptionsFilter", "filter", "APP_FILTER"},
		{"ErrorShapeInterceptor", "interceptor", "APP_INTERCEPTOR"},
		{"EnvelopeInterceptor", "interceptor", "APP_INTERCEPTOR"},
		{"RequestContextInterceptor", "interceptor", "APP_INTERCEPTOR"},
		{"AuthGuard", "guard", "APP_GUARD"},
	}

	for _, w := range wants {
		if !hasEdge(ents, "CommonModule", "USES", "CommonModule", w.class) {
			t.Errorf("expected CommonModule USES %s (global %s)", w.class, w.token)
			continue
		}
		if v := edgeProp(ents, "USES", "CommonModule", w.class, "global"); v != "true" {
			t.Errorf("%s: expected global=true, got %q", w.class, v)
		}
		if v := edgeProp(ents, "USES", "CommonModule", w.class, "di_role"); v != w.role {
			t.Errorf("%s: expected di_role=%s, got %q", w.class, w.role, v)
		}
		if v := edgeProp(ents, "USES", "CommonModule", w.class, "di_token"); v != w.token {
			t.Errorf("%s: expected di_token=%s, got %q", w.class, w.token, v)
		}
	}

	// useFactory APP_PIPE binds a factory, not a class — assert the factory is
	// recorded as a global pipe binding (resolves to the factory symbol).
	if !hasEdge(ents, "CommonModule", "USES", "CommonModule", "createValidationPipe") {
		t.Error("expected CommonModule USES createValidationPipe (global APP_PIPE via factory)")
	}
	if v := edgeProp(ents, "USES", "CommonModule", "createValidationPipe", "di_role"); v != "pipe" {
		t.Errorf("APP_PIPE: expected di_role=pipe, got %q", v)
	}

	// The previously-orphan interceptor class must RESOLVE against a real
	// production entity through the real resolver.
	prodInterceptor := types.EntityRecord{
		Name:       "EnvelopeInterceptor",
		Kind:       "SCOPE.Component",
		Subtype:    "interceptor",
		SourceFile: "src/common/envelope/interceptor/envelope.interceptor.ts",
		Language:   "typescript",
		Properties: map[string]string{"kind": "SCOPE.Component", "subtype": "interceptor"},
	}
	prodInterceptor.ID = prodInterceptor.ComputeID()

	idx := resolve.BuildIndex(append(ents, prodInterceptor))
	if id, ok := idx.Lookup("EnvelopeInterceptor"); !ok || id != prodInterceptor.ID {
		t.Fatalf("global interceptor stub failed to resolve (ok=%v id=%s) — would stay orphan", ok, id)
	}

	// Non-APP_* providers (CognitoTokenValidator, etc.) must NOT be tagged
	// global on a USES edge — they are ordinary providers (BINDS only).
	if hasEdge(ents, "CommonModule", "USES", "CommonModule", "CognitoTokenValidator") {
		t.Error("plain provider CognitoTokenValidator must not get a global USES edge")
	}
}

// TestIssue4329_UseGlobalInMain exercises app.useGlobal*() wiring in main.ts.
func TestIssue4329_UseGlobalInMain(t *testing.T) {
	path, content := loadNest4329(t, "main_globals.ts", "src/main_globals.ts")
	ents := nestExtract4329(t, path, content)

	// app.useGlobalGuards(new RolesGuard()) → app USES RolesGuard (global)
	checks := []struct {
		class string
		role  string
	}{
		{"RolesGuard", "guard"},
		{"LoggingInterceptor", "interceptor"},
		{"HttpExceptionFilter", "filter"},
		{"ValidationPipe", "pipe"},
	}
	for _, c := range checks {
		if !hasEdge(ents, "", "USES", "app", c.class) {
			t.Errorf("expected app USES %s (useGlobal %s)", c.class, c.role)
			continue
		}
		if v := edgeProp(ents, "USES", "app", c.class, "global"); v != "true" {
			t.Errorf("%s: expected global=true, got %q", c.class, v)
		}
		if v := edgeProp(ents, "USES", "app", c.class, "di_role"); v != c.role {
			t.Errorf("%s: expected di_role=%s, got %q", c.class, c.role, v)
		}
	}

	// Resolve one of the global classes against a real entity.
	prod := types.EntityRecord{
		Name: "RolesGuard", Kind: "SCOPE.Component", Subtype: "guard",
		SourceFile: "src/auth/roles.guard.ts", Language: "typescript",
		Properties: map[string]string{"kind": "SCOPE.Component", "subtype": "guard"},
	}
	prod.ID = prod.ComputeID()
	idx := resolve.BuildIndex(append(ents, prod))
	if id, ok := idx.Lookup("RolesGuard"); !ok || id != prod.ID {
		t.Fatalf("useGlobalGuards target RolesGuard failed to resolve (ok=%v id=%s)", ok, id)
	}
}

// TestIssue4329_RealMainNoFalseGlobals ensures the real main.ts (which uses
// setGlobalPrefix, not useGlobal*) does not fabricate global USES edges.
func TestIssue4329_RealMainNoFalseGlobals(t *testing.T) {
	path, content := loadNest4329(t, "main.ts", "src/main.ts")
	ents := nestExtract4329(t, path, content)
	for _, r := range usesEdges(ents) {
		if r.Properties["global"] == "true" {
			t.Errorf("real main.ts should not produce a global USES edge, got %+v", r)
		}
	}
}
