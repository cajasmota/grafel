package csharp_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// dotnet_di_siblings_test.go — VERIFY-FIRST probe for #3959.
//
// The .NET DI binding graph extractor (custom_csharp_dotnet_di, #3699) is
// container-driven on Microsoft.Extensions.DependencyInjection: it gates ONLY on
// file.Language=="csharp" and keys purely on services.AddScoped/AddSingleton/
// AddTransient + constructor injection — there is NO framework gating. These
// probes assert that a NON-ASP.NET-Core .NET file (Carter / FastEndpoints / etc)
// carrying the identical M.E.DI container registration + constructor injection
// produces the SAME BINDS + INJECTED_INTO edges as ASP.NET Core, so the sibling
// framework DI cells are honestly creditable.

// assertSiblingDIFires runs the shared M.E.DI registration + ctor-injection shape
// through the extractor and asserts the BINDS (iface->impl, lifetime) and
// INJECTED_INTO (service->consumer) edges fire — value-asserting, never len>0.
func assertSiblingDIFires(t *testing.T, framework, src string) {
	t.Helper()
	recs := extractRecords(t, src)

	// BINDS: IGreeter BINDS Greeter, lifetime=Scoped — identical to ASP.NET Core.
	bind := findRel(recs, "BINDS", "di:IGreeter->Greeter", "impl:Greeter")
	if bind == nil {
		t.Fatalf("%s: expected IGreeter BINDS Greeter from M.E.DI registration", framework)
	}
	if bind.Properties["lifetime"] != "Scoped" {
		t.Errorf("%s: lifetime = %q, want Scoped", framework, bind.Properties["lifetime"])
	}
	if bind.Properties["framework"] != "dotnet_di" {
		t.Errorf("%s: framework prop = %q, want dotnet_di (container-driven, framework-agnostic)", framework, bind.Properties["framework"])
	}

	// INJECTED_INTO: IGreeter -> the consumer endpoint/module class.
	inj := findRel(recs, "INJECTED_INTO", "IGreeter", "consumer:"+consumerOf(framework))
	if inj == nil {
		t.Fatalf("%s: expected IGreeter INJECTED_INTO %s", framework, consumerOf(framework))
	}
	if inj.Properties["via"] != "dotnet_constructor" {
		t.Errorf("%s: via = %q, want dotnet_constructor", framework, inj.Properties["via"])
	}
}

func consumerOf(framework string) string {
	switch framework {
	case "Carter":
		return "GreetingModule"
	case "FastEndpoints":
		return "GreetEndpoint"
	case "Blazor":
		return "Counter"
	case "grpc-net":
		return "GreeterService"
	case "aspnet-mvc":
		return "GreetController"
	}
	return ""
}

func TestDotnetDI_Sibling_Carter(t *testing.T) {
	// Carter: ICarterModule, no ASP.NET-Core controller — same M.E.DI container.
	assertSiblingDIFires(t, "Carter", `
using Carter;
public class GreetingModule : ICarterModule {
    private readonly IGreeter _greeter;
    public GreetingModule(IGreeter greeter) { _greeter = greeter; }
    public void AddRoutes(IEndpointRouteBuilder app) {
        app.MapGet("/hi", () => _greeter.Hi());
    }
}
public static class Reg {
    public static void Wire(IServiceCollection services) {
        services.AddScoped<IGreeter, Greeter>();
    }
}
`)
}

func TestDotnetDI_Sibling_FastEndpoints(t *testing.T) {
	assertSiblingDIFires(t, "FastEndpoints", `
using FastEndpoints;
public class GreetEndpoint : Endpoint<GreetReq, GreetRes> {
    private readonly IGreeter _greeter;
    public GreetEndpoint(IGreeter greeter) { _greeter = greeter; }
    public override void Configure() { Get("/greet"); }
}
public static class Reg {
    public static void Wire(IServiceCollection services) {
        services.AddScoped<IGreeter, Greeter>();
    }
}
`)
}

// NOTE on Nancy / ServiceStack: these frameworks default to a DIFFERENT IoC
// container (NancyFX → TinyIoC, ServiceStack → Funq). The M.E.DI extractor does
// NOT match their idiomatic registration syntax (asserted by the negative probes
// TestDotnetDI_TinyIoC_NotCredited / TestDotnetDI_FunqContainer_NotCredited
// below), so they are NOT credited by #3959.

func TestDotnetDI_Sibling_Blazor(t *testing.T) {
	assertSiblingDIFires(t, "Blazor", `
using Microsoft.AspNetCore.Components;
public partial class Counter : ComponentBase {
    private readonly IGreeter _greeter;
    public Counter(IGreeter greeter) { _greeter = greeter; }
}
public static class Reg {
    public static void Wire(IServiceCollection services) {
        services.AddScoped<IGreeter, Greeter>();
    }
}
`)
}

func TestDotnetDI_Sibling_GrpcNet(t *testing.T) {
	assertSiblingDIFires(t, "grpc-net", `
using Grpc.Core;
public class GreeterService : Greeter.GreeterBase {
    private readonly IGreeter _greeter;
    public GreeterService(IGreeter greeter) { _greeter = greeter; }
}
public static class Reg {
    public static void Wire(IServiceCollection services) {
        services.AddScoped<IGreeter, Greeter>();
    }
}
`)
}

func TestDotnetDI_Sibling_AspNetMvc(t *testing.T) {
	assertSiblingDIFires(t, "aspnet-mvc", `
using Microsoft.AspNetCore.Mvc;
public class GreetController : Controller {
    private readonly IGreeter _greeter;
    public GreetController(IGreeter greeter) { _greeter = greeter; }
}
public static class Reg {
    public static void Wire(IServiceCollection services) {
        services.AddScoped<IGreeter, Greeter>();
    }
}
`)
}

// TestDotnetDI_Sibling_AllShareContainer documents the verify-first finding:
// the extractor emits container records irrespective of the framework marker, so
// the only thing that matters is that the framework uses the M.E.DI container
// (services.AddXxx). Frameworks on a DIFFERENT container (e.g. Autofac-only with
// builder.RegisterType, no services.AddXxx) would NOT fire here and are NOT
// credited.
func TestDotnetDI_DifferentContainer_NotCredited(t *testing.T) {
	// Pure Autofac registration (ContainerBuilder.RegisterType) — NOT M.E.DI.
	recs := extractRecords(t, `
using Autofac;
public class Wiring {
    public void Build(ContainerBuilder builder) {
        builder.RegisterType<Greeter>().As<IGreeter>();
    }
}
`)
	if findRel(recs, "BINDS", "di:IGreeter->Greeter", "impl:Greeter") != nil {
		t.Error("Autofac RegisterType<>().As<>() must NOT be credited by the M.E.DI extractor")
	}
	_ = types.RelationshipKindBinds
}

// TestDotnetDI_FunqContainer_NotCredited proves ServiceStack's DEFAULT Funq
// container registration syntax (container.Register<IGreeter>(c => new Greeter()))
// does NOT match the M.E.DI services.AddXxx<,>() shape — so ServiceStack is NOT
// credited on its idiomatic container.
func TestDotnetDI_FunqContainer_NotCredited(t *testing.T) {
	recs := extractRecords(t, `
using Funq;
public class AppHost : AppHostBase {
    public override void Configure(Container container) {
        container.Register<IGreeter>(c => new Greeter());
        container.RegisterAs<Greeter, IGreeter>();
    }
}
`)
	if findRel(recs, "BINDS", "di:IGreeter->Greeter", "impl:Greeter") != nil {
		t.Error("Funq container.Register/RegisterAs must NOT be credited by the M.E.DI extractor")
	}
}

// TestDotnetDI_TinyIoC_NotCredited proves NancyFX's DEFAULT TinyIoC container
// registration syntax (container.Register<IGreeter, Greeter>()) — note: NOT
// services.AddXxx — does NOT match. The extractor keys on the services.Add*
// VERB, not a bare Register<,>(); Nancy is NOT credited on its idiomatic
// container.
func TestDotnetDI_TinyIoC_NotCredited(t *testing.T) {
	recs := extractRecords(t, `
using TinyIoC;
public class Bootstrapper : DefaultNancyBootstrapper {
    protected override void ConfigureApplicationContainer(TinyIoCContainer container) {
        container.Register<IGreeter, Greeter>();
    }
}
`)
	if findRel(recs, "BINDS", "di:IGreeter->Greeter", "impl:Greeter") != nil {
		t.Error("TinyIoC container.Register<,>() must NOT be credited by the M.E.DI extractor")
	}
}
