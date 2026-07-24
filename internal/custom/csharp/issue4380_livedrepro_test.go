package csharp_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"

	_ "github.com/cajasmota/grafel/internal/custom/csharp"
)

// Issue #4380 LIVE-REPRO (.NET side).
//
// Generalizes the NestJS global-DI fix (#4329) to ASP.NET Core. A faithful
// Program.cs registers middleware app-wide via app.UseMiddleware<T>() and binds
// services via builder.Services.AddScoped<IFoo, Foo>() / AddSingleton /
// AddTransient.
//
// PRE-FIX:
//   - app.UseMiddleware<RequestLoggingMiddleware>() produced only a standalone
//     "middleware:RequestLoggingMiddleware" entity with NO edge to the class —
//     the middleware class looked orphan / dead.
//   - AddScoped<IFoo, Foo>() already produced an IFoo BINDS Foo edge (#3699);
//     this test pins that the impl class resolves through the symbol table.
//
// POST-FIX:
//   - a synthetic `app` entity owns an app → RequestLoggingMiddleware USES edge
//     (global=true, di_role=middleware, order), and the class resolves.
//   - the AddScoped impl binds and resolves to the real impl class.

const issue4380Program = `
var builder = WebApplication.CreateBuilder(args);

builder.Services.AddScoped<IOrderService, OrderService>();
builder.Services.AddSingleton<IClock, SystemClock>();
builder.Services.AddTransient<IMailer, SmtpMailer>();

var app = builder.Build();

app.UseMiddleware<RequestLoggingMiddleware>();
app.UseMiddleware<CorrelationIdMiddleware>();
app.UseAuthentication();
app.UseAuthorization();

app.MapControllers();
app.Run();
`

func extract4380CS(t *testing.T, key, path, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get(key)
	if !ok {
		t.Fatalf("%s not registered", key)
	}
	recs, err := e.Extract(context.Background(),
		extreg.FileInput{Path: path, Language: "csharp", Content: []byte(src)})
	if err != nil {
		t.Fatalf("%s extract: %v", key, err)
	}
	return recs
}

func csEdge(ents []types.EntityRecord, kind, fromID, toID string) *types.RelationshipRecord {
	for i := range ents {
		for j := range ents[i].Relationships {
			r := &ents[i].Relationships[j]
			if r.Kind == kind && r.FromID == fromID && r.ToID == toID {
				return r
			}
		}
	}
	return nil
}

// TestIssue4380_UseMiddleware_AppUsesClass asserts the app → middleware USES
// edge for each app.UseMiddleware<T>() and that the previously-orphan class
// resolves through the real resolver.
func TestIssue4380_UseMiddleware_AppUsesClass(t *testing.T) {
	ents := extract4380CS(t, "custom_csharp_middleware_extra", "Program.cs", issue4380Program)

	for i, cls := range []string{"RequestLoggingMiddleware", "CorrelationIdMiddleware"} {
		r := csEdge(ents, "USES", "app", cls)
		if r == nil {
			t.Fatalf("expected app USES %s (UseMiddleware<%s>)", cls, cls)
		}
		if r.Properties.Get("global") != "true" {
			t.Errorf("%s: expected global=true, got %q", cls, r.Properties.Get("global"))
		}
		if r.Properties.Get("di_role") != "middleware" {
			t.Errorf("%s: expected di_role=middleware, got %q", cls, r.Properties.Get("di_role"))
		}
		if got := r.Properties.Get("order"); got != itoaTest(i) {
			t.Errorf("%s: expected order=%d, got %q", cls, i, got)
		}
	}

	// The previously-orphan middleware class must RESOLVE against a real class.
	prod := types.EntityRecord{
		Name: "RequestLoggingMiddleware", Kind: "SCOPE.Class", Subtype: "class",
		SourceFile: "Middleware/RequestLoggingMiddleware.cs", Language: "csharp",
		Properties: map[string]string{"kind": "SCOPE.Class", "subtype": "class"},
	}
	prod.ID = prod.ComputeID()
	idx := resolve.BuildIndex(append(ents, prod))
	if id, ok := idx.Lookup("RequestLoggingMiddleware"); !ok || id != prod.ID {
		t.Fatalf("UseMiddleware target failed to resolve (ok=%v id=%s) — would stay orphan", ok, id)
	}
}

// TestIssue4380_AddScoped_BindsAndResolves asserts the DI BINDS edge from the
// interface registration to the impl class and that the impl resolves.
func TestIssue4380_AddScoped_BindsAndResolves(t *testing.T) {
	ents := extract4380CS(t, "custom_csharp_dotnet_di", "Program.cs", issue4380Program)

	if csEdge(ents, "BINDS", "", "impl:OrderService") == nil {
		t.Fatalf("expected IOrderService BINDS OrderService (AddScoped<IOrderService, OrderService>)")
	}

	prod := types.EntityRecord{
		Name: "OrderService", Kind: "SCOPE.Class", Subtype: "class",
		SourceFile: "Services/OrderService.cs", Language: "csharp",
		Properties: map[string]string{"kind": "SCOPE.Class", "subtype": "class"},
	}
	prod.ID = prod.ComputeID()
	idx := resolve.BuildIndex(append(ents, prod))
	// impl:OrderService → kind-agnostic bare-name fallback resolves to OrderService.
	if id, ok := idx.Lookup("impl:OrderService"); !ok || id != prod.ID {
		t.Fatalf("AddScoped impl OrderService failed to resolve (ok=%v id=%s)", ok, id)
	}
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
