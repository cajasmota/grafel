package csharp_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"

	_ "github.com/cajasmota/archigraph/internal/custom/csharp"
)

func fi(path, lang, src string) extreg.FileInput {
	return extreg.FileInput{Path: path, Language: lang, Content: []byte(src)}
}

func extract(t *testing.T, name string, file extreg.FileInput) []entitySummary {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	var out []entitySummary
	for _, ent := range ents {
		out = append(out, entitySummary{Kind: ent.Kind, Subtype: ent.Subtype, Name: ent.Name})
	}
	return out
}

type entitySummary struct{ Kind, Subtype, Name string }

func containsEntity(ents []entitySummary, kind, name string) bool {
	for _, e := range ents {
		if e.Kind == kind && e.Name == name {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// ASP.NET Core
// ---------------------------------------------------------------------------

func TestAspNetCoreAttributeRoute(t *testing.T) {
	src := `
[ApiController]
[Route("api/[controller]")]
public class UsersController : ControllerBase
{
    [HttpGet]
    public IActionResult List() => Ok();

    [HttpPost]
    public IActionResult Create([FromBody] CreateUserDto dto) => Created();

    [HttpDelete("{id}")]
    public IActionResult Delete(int id) => NoContent();
}
`
	ents := extract(t, "custom_csharp_aspnet_core", fi("UsersController.cs", "csharp", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET api/[controller]") {
		t.Error("expected GET route from [HttpGet]")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST api/[controller]") {
		t.Error("expected POST route from [HttpPost]")
	}
}

func TestAspNetCoreMinimalApi(t *testing.T) {
	src := `
app.MapGet("/users", () => Results.Ok());
app.MapPost("/users", (CreateUserDto dto) => Results.Created());
`
	ents := extract(t, "custom_csharp_aspnet_core", fi("Program.cs", "csharp", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users") {
		t.Error("expected GET /users minimal API route")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /users") {
		t.Error("expected POST /users minimal API route")
	}
}

func TestAspNetCoreDI(t *testing.T) {
	// Regex matches lowercase "services." prefix (typical Startup.cs pattern)
	src := `
services.AddSingleton<IPaymentService, StripeService>();
services.AddScoped<IOrderRepository, OrderRepository>();
`
	ents := extract(t, "custom_csharp_aspnet_core", fi("Startup.cs", "csharp", src))
	// DI entity name = "di:" + lifetime + ":" + serviceType
	if !containsEntity(ents, "SCOPE.Pattern", "di:Singleton:IPaymentService, StripeService") {
		t.Error("expected Singleton DI registration pattern")
	}
}

func TestAspNetCoreController(t *testing.T) {
	src := `public class OrdersController : ControllerBase {}`
	ents := extract(t, "custom_csharp_aspnet_core", fi("OrdersController.cs", "csharp", src))
	if !containsEntity(ents, "SCOPE.Component", "OrdersController") {
		t.Error("expected OrdersController component")
	}
}

func TestAspNetCoreNoMatch(t *testing.T) {
	src := `using System; namespace Foo {}`
	ents := extract(t, "custom_csharp_aspnet_core", fi("empty.cs", "csharp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// ASP.NET Request/Response
// ---------------------------------------------------------------------------

func TestAspNetReqRespFromBody(t *testing.T) {
	src := `
public IActionResult Create([FromBody] CreateOrderDto dto) => Ok();
public IActionResult Update([FromBody] UpdateProductRequest req) => Ok();
`
	ents := extract(t, "custom_csharp_aspnet_reqresp", fi("Controller.cs", "csharp", src))
	if !containsEntity(ents, "SCOPE.Component", "CreateOrderDto") {
		t.Error("expected CreateOrderDto component")
	}
	if !containsEntity(ents, "SCOPE.Component", "UpdateProductRequest") {
		t.Error("expected UpdateProductRequest component")
	}
}

func TestAspNetReqRespPrimitivesSkipped(t *testing.T) {
	src := `public IActionResult Delete([FromBody] int id) => Ok();`
	ents := extract(t, "custom_csharp_aspnet_reqresp", fi("Controller.cs", "csharp", src))
	if containsEntity(ents, "SCOPE.Schema", "int") {
		t.Error("primitive int should be skipped")
	}
}

func TestAspNetReqRespNoMatch(t *testing.T) {
	src := `public void Helper() {}`
	ents := extract(t, "custom_csharp_aspnet_reqresp", fi("Helper.cs", "csharp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Blazor
// ---------------------------------------------------------------------------

func TestBlazorPage(t *testing.T) {
	src := `
@page "/counter"
@page "/counter/{id:int}"
`
	ents := extract(t, "custom_csharp_blazor", fi("Counter.razor", "csharp", src))
	if !containsEntity(ents, "SCOPE.Operation", "/counter") {
		t.Error("expected /counter page route")
	}
}

func TestBlazorInject(t *testing.T) {
	src := `@inject NavigationManager Nav
@inject IAuthService AuthService`
	ents := extract(t, "custom_csharp_blazor", fi("Page.razor", "csharp", src))
	if !containsEntity(ents, "SCOPE.Component", "NavigationManager") {
		t.Error("expected NavigationManager inject component")
	}
}

func TestBlazorComponent(t *testing.T) {
	src := `<MyCustomCard title="Hello" />`
	ents := extract(t, "custom_csharp_blazor", fi("Layout.razor", "csharp", src))
	if !containsEntity(ents, "SCOPE.UIComponent", "MyCustomCard") {
		t.Error("expected MyCustomCard UIComponent")
	}
}

func TestBlazorNoMatch(t *testing.T) {
	src := `<div>Hello</div>`
	ents := extract(t, "custom_csharp_blazor", fi("plain.razor", "csharp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// EF Core
// ---------------------------------------------------------------------------

func TestEFCoreDbContext(t *testing.T) {
	src := `
public class AppDbContext : DbContext
{
    public DbSet<User> Users { get; set; }
    public DbSet<Order> Orders { get; set; }
}
`
	ents := extract(t, "custom_csharp_ef_core", fi("AppDbContext.cs", "csharp", src))
	if !containsEntity(ents, "SCOPE.Service", "AppDbContext") {
		t.Error("expected AppDbContext SCOPE.Service")
	}
	if !containsEntity(ents, "SCOPE.Component", "User") {
		t.Error("expected User DbSet component")
	}
}

func TestEFCoreFluentApi(t *testing.T) {
	src := `
modelBuilder.Entity<Order>()
    .HasMany(o => o.Items)
    .WithOne(i => i.Order);
`
	ents := extract(t, "custom_csharp_ef_core", fi("Config.cs", "csharp", src))
	if !containsEntity(ents, "SCOPE.Component", "Order") {
		t.Error("expected Order entity component from fluent API")
	}
}

func TestEFCoreMigration(t *testing.T) {
	src := `
public partial class AddOrdersTable : Migration
{
    protected override void Up(MigrationBuilder migrationBuilder) {}
}
`
	ents := extract(t, "custom_csharp_ef_core", fi("Migration.cs", "csharp", src))
	// Migration entity name = class name (no prefix)
	if !containsEntity(ents, "SCOPE.Component", "AddOrdersTable") {
		t.Error("expected AddOrdersTable migration component entity")
	}
}

func TestEFCoreNoMatch(t *testing.T) {
	src := `namespace MyApp { class Helper {} }`
	ents := extract(t, "custom_csharp_ef_core", fi("Helper.cs", "csharp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
