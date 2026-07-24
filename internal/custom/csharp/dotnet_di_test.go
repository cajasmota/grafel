package csharp_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"

	_ "github.com/cajasmota/grafel/internal/custom/csharp"
)

// dotnet_di_test.go — value-asserting tests for the .NET DI GRAPH extractor
// (#3699). Assertions check the SEMANTIC edge (interface→impl, lifetime,
// provider→consumer), never len>0.

func extractRecords(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_csharp_dotnet_di")
	if !ok {
		t.Fatal("custom_csharp_dotnet_di not registered")
	}
	recs, err := e.Extract(context.Background(),
		extreg.FileInput{Path: "Program.cs", Language: "csharp", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return recs
}

// findRel returns the first relationship of kind on any entity whose Name ==
// fromName and whose ToID == toID, or nil.
func findRel(recs []types.EntityRecord, kind, fromName, toID string) *types.RelationshipRecord {
	for i := range recs {
		if recs[i].Name != fromName {
			continue
		}
		for j := range recs[i].Relationships {
			r := &recs[i].Relationships[j]
			if r.Kind == kind && r.ToID == toID {
				return r
			}
		}
	}
	return nil
}

func TestDotnetDI_AddScoped_TwoArg_Binds(t *testing.T) {
	src := `
public class Startup {
    public void ConfigureServices(IServiceCollection services) {
        services.AddScoped<IRepo, Repo>();
        services.AddSingleton<IClock, SystemClock>();
        services.AddTransient<IMailer, SmtpMailer>();
    }
}
`
	recs := extractRecords(t, src)

	// IRepo BINDS Repo, lifetime=Scoped.
	edge := findRel(recs, "BINDS", "di:IRepo->Repo", "impl:Repo")
	if edge == nil {
		t.Fatalf("expected IRepo BINDS Repo")
	}
	if edge.Properties.Get("lifetime") != "Scoped" {
		t.Errorf("lifetime = %q, want Scoped", edge.Properties.Get("lifetime"))
	}
	if edge.Properties.Get("interface") != "IRepo" || edge.Properties.Get("implementation") != "Repo" {
		t.Errorf("binding props = %v, want IRepo->Repo", edge.Properties)
	}
	// Singleton + Transient lifetimes recorded distinctly.
	if e := findRel(recs, "BINDS", "di:IClock->SystemClock", "impl:SystemClock"); e == nil || e.Properties.Get("lifetime") != "Singleton" {
		t.Errorf("expected IClock BINDS SystemClock lifetime=Singleton; got %v", e)
	}
	if e := findRel(recs, "BINDS", "di:IMailer->SmtpMailer", "impl:SmtpMailer"); e == nil || e.Properties.Get("lifetime") != "Transient" {
		t.Errorf("expected IMailer BINDS SmtpMailer lifetime=Transient; got %v", e)
	}
}

func TestDotnetDI_SelfRegistration_Binds(t *testing.T) {
	src := `
services.AddSingleton<MetricsCollector>();
`
	recs := extractRecords(t, src)
	edge := findRel(recs, "BINDS", "di:MetricsCollector->MetricsCollector", "impl:MetricsCollector")
	if edge == nil {
		t.Fatalf("expected MetricsCollector self-BINDS")
	}
	if edge.Properties.Get("binding_kind") != "self" {
		t.Errorf("binding_kind = %q, want self", edge.Properties.Get("binding_kind"))
	}
	if edge.Properties.Get("lifetime") != "Singleton" {
		t.Errorf("lifetime = %q, want Singleton", edge.Properties.Get("lifetime"))
	}
}

func TestDotnetDI_TypeofForm_Binds(t *testing.T) {
	src := `
services.AddScoped(typeof(IRepository), typeof(SqlRepository));
`
	recs := extractRecords(t, src)
	edge := findRel(recs, "BINDS", "di:IRepository->SqlRepository", "impl:SqlRepository")
	if edge == nil {
		t.Fatalf("expected IRepository BINDS SqlRepository (typeof form)")
	}
	if edge.Properties.Get("lifetime") != "Scoped" {
		t.Errorf("lifetime = %q, want Scoped", edge.Properties.Get("lifetime"))
	}
}

func TestDotnetDI_ConstructorInjection_InjectedInto(t *testing.T) {
	src := `
public class OrderController : ControllerBase {
    private readonly IOrderService _svc;
    public OrderController(IOrderService svc, ILogger<OrderController> logger, string region) {
        _svc = svc;
    }
}
`
	recs := extractRecords(t, src)

	edge := findRel(recs, "INJECTED_INTO", "IOrderService", "consumer:OrderController")
	if edge == nil {
		t.Fatalf("expected IOrderService INJECTED_INTO OrderController")
	}
	if edge.Properties.Get("via") != "dotnet_constructor" {
		t.Errorf("via = %q, want dotnet_constructor", edge.Properties.Get("via"))
	}
	// Negative: ILogger<> infrastructure generic must NOT inject.
	if findRel(recs, "INJECTED_INTO", "ILogger", "consumer:OrderController") != nil {
		t.Error("ILogger<> produced a spurious INJECTED_INTO edge")
	}
	// Negative: primitive string region must NOT inject.
	if findRel(recs, "INJECTED_INTO", "string", "consumer:OrderController") != nil {
		t.Error("string ctor param produced a spurious INJECTED_INTO edge")
	}
}

func TestDotnetDI_NonCsharp_NoOutput(t *testing.T) {
	recs := extractRecords(t, `services.AddScoped<IRepo, Repo>();`)
	if len(recs) == 0 {
		t.Skip("sanity") // csharp path asserted elsewhere
	}
	// Language gate: a java file yields nothing.
	e, _ := extreg.Get("custom_csharp_dotnet_di")
	out, _ := e.Extract(context.Background(),
		extreg.FileInput{Path: "X.java", Language: "java", Content: []byte(`services.AddScoped<IRepo, Repo>();`)})
	if len(out) != 0 {
		t.Errorf("java file produced %d records, want 0", len(out))
	}
}
