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
	// [controller] token must be expanded: UsersController → "users"
	if !containsEntity(ents, "SCOPE.Operation", "GET api/users") {
		t.Error("expected GET api/users from [HttpGet] (with [controller] token expanded)")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST api/users") {
		t.Error("expected POST api/users from [HttpPost] (with [controller] token expanded)")
	}
	// DELETE with sub-path: api/users/{id}
	if !containsEntity(ents, "SCOPE.Operation", "DELETE api/users/{id}") {
		t.Error("expected DELETE api/users/{id} from [HttpDelete(\"{id}\")]")
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

// ---------------------------------------------------------------------------
// Hangfire
// ---------------------------------------------------------------------------

func TestHangfireEnqueueStatic(t *testing.T) {
	src := `BackgroundJob.Enqueue(() => EmailService.Send(userId));`
	ents := extract(t, "custom_csharp_hangfire", fi("jobs/EmailJob.cs", "csharp", src))
	if !containsEntity(ents, "SCOPE.Operation", "EmailService.Send") {
		t.Error("expected Hangfire enqueue producer entity EmailService.Send")
	}
}

func TestHangfireEnqueueTyped(t *testing.T) {
	src := `BackgroundJob.Enqueue<IEmailService>(x => x.Send(userId));`
	ents := extract(t, "custom_csharp_hangfire", fi("jobs/EmailJob.cs", "csharp", src))
	if !containsEntity(ents, "SCOPE.Operation", "IEmailService.Send") {
		t.Error("expected typed Hangfire enqueue producer entity IEmailService.Send")
	}
}

func TestHangfireRecurring(t *testing.T) {
	src := `RecurringJob.AddOrUpdate("daily-report", () => ReportService.Generate(), Cron.Daily);`
	ents := extract(t, "custom_csharp_hangfire", fi("jobs/RecurringJobs.cs", "csharp", src))
	if !containsEntity(ents, "SCOPE.Pattern", "daily-report") {
		t.Error("expected Hangfire recurring job pattern entity 'daily-report'")
	}
}

func TestHangfireIJobConsumer(t *testing.T) {
	src := `
public class CleanupJob : IBackgroundJob
{
    public async Task Execute(PerformContext ctx) { }
}
`
	ents := extract(t, "custom_csharp_hangfire", fi("jobs/CleanupJob.cs", "csharp", src))
	if !containsEntity(ents, "SCOPE.Service", "CleanupJob") {
		t.Error("expected Hangfire IBackgroundJob consumer entity CleanupJob")
	}
}

func TestHangfireNoMatch(t *testing.T) {
	src := `namespace App { class Helper { } }`
	ents := extract(t, "custom_csharp_hangfire", fi("Helper.cs", "csharp", src))
	if len(ents) != 0 {
		t.Errorf("expected 0 hangfire entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Quartz.NET
// ---------------------------------------------------------------------------

func TestQuartzNetIJobConsumer(t *testing.T) {
	src := `
public class SendEmailJob : IJob
{
    public async Task Execute(IJobExecutionContext context) { }
}
`
	ents := extract(t, "custom_csharp_quartz_net", fi("jobs/SendEmailJob.cs", "csharp", src))
	if !containsEntity(ents, "SCOPE.Service", "SendEmailJob") {
		t.Error("expected Quartz.NET IJob consumer entity SendEmailJob")
	}
}

func TestQuartzNetJobBuilder(t *testing.T) {
	src := `var job = JobBuilder.Create<SendEmailJob>().WithIdentity("email-job").Build();`
	ents := extract(t, "custom_csharp_quartz_net", fi("Scheduler.cs", "csharp", src))
	if !containsEntity(ents, "SCOPE.Operation", "JobBuilder.Create<SendEmailJob>") {
		t.Error("expected Quartz.NET JobBuilder producer entity")
	}
}

func TestQuartzNetScheduleJob(t *testing.T) {
	src := `await scheduler.ScheduleJob(job, trigger);`
	ents := extract(t, "custom_csharp_quartz_net", fi("Scheduler.cs", "csharp", src))
	if !containsEntity(ents, "SCOPE.Operation", "scheduler.ScheduleJob") {
		t.Error("expected Quartz.NET scheduler.ScheduleJob producer entity")
	}
}

func TestQuartzNetNoMatch(t *testing.T) {
	src := `namespace App { class Helper { } }`
	ents := extract(t, "custom_csharp_quartz_net", fi("Helper.cs", "csharp", src))
	if len(ents) != 0 {
		t.Errorf("expected 0 Quartz.NET entities, got %d", len(ents))
	}
}
