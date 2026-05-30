package csharp_test

// ---------------------------------------------------------------------------
// Deep Blazor extraction tests — value-asserting (exact prop types/names,
// exact routes, exact lifecycle method names).  Closes #3381.
// ---------------------------------------------------------------------------

import (
	"testing"
)

// containsSubtype returns true when any entity has the given subtype.
func containsSubtype(ents []entitySummary, subtype string) bool {
	for _, e := range ents {
		if e.Subtype == subtype {
			return true
		}
	}
	return false
}

// containsNamePrefix returns true when any entity name starts with prefix.
func containsNamePrefix(ents []entitySummary, prefix string) bool {
	for _, e := range ents {
		if len(e.Name) >= len(prefix) && e.Name[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Structure/component_extraction
// ---------------------------------------------------------------------------

// TestBlazorDeepComponentFromFilename verifies that a .razor file basename is
// emitted as a component_extraction entity named after the file.
func TestBlazorDeepComponentFromFilename(t *testing.T) {
	src := `@page "/counter"
@code {
    private int count = 0;
}`
	ents := extract(t, "custom_csharp_blazor_deep", fi("Counter.razor", "csharp", src))
	if !containsEntity(ents, "SCOPE.UIComponent", "Counter") {
		t.Error("expected component_extraction entity named 'Counter' from Counter.razor filename")
	}
}

// TestBlazorDeepComponentFromCodeBehindFilename verifies .razor.cs basename.
func TestBlazorDeepComponentFromCodeBehindFilename(t *testing.T) {
	src := `public partial class ProductDetail : ComponentBase { }`
	ents := extract(t, "custom_csharp_blazor_deep", fi("ProductDetail.razor.cs", "csharp", src))
	if !containsEntity(ents, "SCOPE.UIComponent", "ProductDetail") {
		t.Error("expected component_extraction entity named 'ProductDetail' from ProductDetail.razor.cs filename")
	}
}

// TestBlazorDeepAttributeRouteComponent verifies @attribute [Route("...")] emits
// a component_extraction entity.
func TestBlazorDeepAttributeRouteComponent(t *testing.T) {
	src := `@attribute [Route("/admin/settings")]

@code {
    // settings page
}`
	ents := extract(t, "custom_csharp_blazor_deep", fi("Settings.razor", "csharp", src))
	if !containsNamePrefix(ents, "component:attr-route:") {
		t.Error("expected component_extraction entity from @attribute [Route(...)]")
	}
}

// TestBlazorDeepRenderMode verifies @rendermode emits a component_extraction entity.
func TestBlazorDeepRenderMode(t *testing.T) {
	src := `@page "/dashboard"
@rendermode InteractiveServer

<h1>Dashboard</h1>`
	ents := extract(t, "custom_csharp_blazor_deep", fi("Dashboard.razor", "csharp", src))
	if !containsEntity(ents, "SCOPE.UIComponent", "rendermode:InteractiveServer") {
		t.Error("expected component_extraction entity 'rendermode:InteractiveServer' from @rendermode directive")
	}
}

// ---------------------------------------------------------------------------
// Structure/context_extraction
// ---------------------------------------------------------------------------

// TestBlazorDeepConstructorDI verifies constructor DI in .razor.cs code-behind
// emits context_extraction entities with correct service type and variable name.
func TestBlazorDeepConstructorDI(t *testing.T) {
	src := `public partial class OrderList : ComponentBase
{
    private readonly IOrderService _orders;
    private readonly ILogger<OrderList> _log;

    public OrderList(IOrderService orders, ILogger<OrderList> log)
    {
        _orders = orders;
        _log = log;
    }
}`
	ents := extract(t, "custom_csharp_blazor_deep", fi("OrderList.razor.cs", "csharp", src))

	if !containsEntity(ents, "SCOPE.Component", "ctx:ctor:IOrderService:orders") {
		t.Error("expected context_extraction entity 'ctx:ctor:IOrderService:orders' from constructor DI")
	}
}

// ---------------------------------------------------------------------------
// Data Flow/prop_extraction — typed parameters
// ---------------------------------------------------------------------------

// TestBlazorDeepTypedParameter verifies that [Parameter] emits a typed
// prop_extraction entity with both type and name, e.g. "param:string:Title".
func TestBlazorDeepTypedParameter(t *testing.T) {
	src := `@code {
    [Parameter]
    public string Title { get; set; }

    [Parameter]
    public int MaxItems { get; set; }

    [Parameter]
    public List<string> Tags { get; set; }
}`
	ents := extract(t, "custom_csharp_blazor_deep", fi("Card.razor", "csharp", src))

	if !containsEntity(ents, "SCOPE.Pattern", "param:string:Title") {
		t.Error("expected prop_extraction 'param:string:Title' from [Parameter] public string Title")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "param:int:MaxItems") {
		t.Error("expected prop_extraction 'param:int:MaxItems' from [Parameter] public int MaxItems")
	}
}

// TestBlazorDeepEventCallbackParameter verifies EventCallback<T> emits a
// prop_extraction entity of form "callback:T:Name".
func TestBlazorDeepEventCallbackParameter(t *testing.T) {
	src := `@code {
    [Parameter]
    public EventCallback<string> OnSearch { get; set; }

    [Parameter]
    public EventCallback OnClose { get; set; }
}`
	ents := extract(t, "custom_csharp_blazor_deep", fi("SearchBox.razor", "csharp", src))

	if !containsEntity(ents, "SCOPE.Pattern", "callback:string:OnSearch") {
		t.Error("expected prop_extraction 'callback:string:OnSearch' from EventCallback<string>")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "callback::OnClose") {
		t.Error("expected prop_extraction 'callback::OnClose' from unparameterized EventCallback")
	}
}

// TestBlazorDeepEditorRequiredParameter verifies [EditorRequired] [Parameter]
// is captured.
func TestBlazorDeepEditorRequiredParameter(t *testing.T) {
	src := `@code {
    [Parameter]
    [EditorRequired]
    public string Label { get; set; } = default!;
}`
	ents := extract(t, "custom_csharp_blazor_deep", fi("Field.razor", "csharp", src))
	if !containsSubtype(ents, "prop_extraction") {
		t.Error("expected prop_extraction entity from [EditorRequired] [Parameter]")
	}
}

// ---------------------------------------------------------------------------
// Data Flow/state_management — @bind and CascadingValue
// ---------------------------------------------------------------------------

// TestBlazorDeepBindTwoWay verifies @bind="FieldName" emits a state_management
// entity "bind:FieldName".
func TestBlazorDeepBindTwoWay(t *testing.T) {
	src := `<input @bind="searchText" />
<InputText @bind-Value="currentUser.Name" />

@code {
    private string searchText = "";
}`
	ents := extract(t, "custom_csharp_blazor_deep", fi("Search.razor", "csharp", src))

	if !containsEntity(ents, "SCOPE.Pattern", "bind:searchText") {
		t.Error("expected state_management entity 'bind:searchText' from @bind=\"searchText\"")
	}
}

// TestBlazorDeepCascadeValueProvider verifies <CascadingValue Value="...">
// emits a state_management entity.
func TestBlazorDeepCascadeValueProvider(t *testing.T) {
	src := `<CascadingValue Value="appState">
    @Body
</CascadingValue>`
	ents := extract(t, "custom_csharp_blazor_deep", fi("App.razor", "csharp", src))

	if !containsEntity(ents, "SCOPE.Pattern", "cascade:provider:appState") {
		t.Error("expected state_management entity 'cascade:provider:appState' from CascadingValue")
	}
}

// ---------------------------------------------------------------------------
// Data Flow/data_fetching — IHttpClientFactory
// ---------------------------------------------------------------------------

// TestBlazorDeepHttpClientFactory verifies IHttpClientFactory.CreateClient()
// emits a data_fetching entity.
func TestBlazorDeepHttpClientFactory(t *testing.T) {
	src := `@inject IHttpClientFactory HttpFactory

@code {
    protected override async Task OnInitializedAsync()
    {
        var client = HttpFactory.CreateClient("orders");
        var data = await client.GetFromJsonAsync<List<Order>>("/api/orders");
    }
}`
	ents := extract(t, "custom_csharp_blazor_deep", fi("OrderList.razor", "csharp", src))

	if !containsNamePrefix(ents, "http:factory:orders:") {
		t.Error("expected data_fetching entity 'http:factory:orders:...' from CreateClient(\"orders\")")
	}
}

// TestBlazorDeepHttpClientFactoryDefault verifies CreateClient() without name
// emits "http:factory:default:...".
func TestBlazorDeepHttpClientFactoryDefault(t *testing.T) {
	src := `@code {
    protected override async Task OnInitializedAsync()
    {
        var client = _factory.CreateClient();
        var result = await client.GetStringAsync("/api/ping");
    }
}`
	ents := extract(t, "custom_csharp_blazor_deep", fi("Ping.razor", "csharp", src))

	if !containsNamePrefix(ents, "http:factory:default:") {
		t.Error("expected data_fetching entity 'http:factory:default:...' from CreateClient()")
	}
}

// ---------------------------------------------------------------------------
// Data Flow/branch_conditions — @foreach
// ---------------------------------------------------------------------------

// TestBlazorDeepForeachBranch verifies @foreach in Razor markup emits a
// branch_conditions entity.
func TestBlazorDeepForeachBranch(t *testing.T) {
	src := `@foreach (var item in items)
{
    <li>@item.Name</li>
}

@if (items == null)
{
    <p>Loading...</p>
}`
	ents := extract(t, "custom_csharp_blazor_deep", fi("List.razor", "csharp", src))

	if !containsNamePrefix(ents, "branch:razorforeach:") {
		t.Error("expected branch_conditions entity from @foreach in Razor markup")
	}
}

// ---------------------------------------------------------------------------
// Navigation/router_pattern — @attribute [Route] + route params
// ---------------------------------------------------------------------------

// TestBlazorDeepAttributeRoutePattern verifies @attribute [Route("...")] emits
// a router_pattern operation.
func TestBlazorDeepAttributeRoutePattern(t *testing.T) {
	src := `@attribute [Route("/products/{id:int}")]

@code { }`
	ents := extract(t, "custom_csharp_blazor_deep", fi("ProductPage.razor", "csharp", src))

	if !containsEntity(ents, "SCOPE.Operation", "route:attr:/products/{id:int}") {
		t.Error("expected router_pattern operation 'route:attr:/products/{id:int}' from @attribute [Route]")
	}
}

// TestBlazorDeepRouteParamExtraction verifies route parameter names are
// extracted from @page templates and emitted as router_pattern patterns.
func TestBlazorDeepRouteParamExtraction(t *testing.T) {
	src := `@page "/orders/{orderId}"
@page "/orders/{orderId}/items/{itemId:int}"`
	ents := extract(t, "custom_csharp_blazor_deep", fi("OrderDetail.razor", "csharp", src))

	if !containsNamePrefix(ents, "route:param:/orders/{orderId}:orderId") {
		t.Error("expected router_pattern entity 'route:param:...:orderId' from @page route param")
	}
}

// ---------------------------------------------------------------------------
// Lifecycle/state_setter_emission — extended methods
// ---------------------------------------------------------------------------

// TestBlazorDeepSetParametersAsync verifies SetParametersAsync override is
// emitted as a state_setter_emission lifecycle entity.
func TestBlazorDeepSetParametersAsync(t *testing.T) {
	src := `@code {
    public override async Task SetParametersAsync(ParameterView parameters)
    {
        await base.SetParametersAsync(parameters);
    }
}`
	ents := extract(t, "custom_csharp_blazor_deep", fi("Page.razor", "csharp", src))

	if !containsEntity(ents, "SCOPE.Operation", "lifecycle:SetParametersAsync") {
		t.Error("expected state_setter_emission 'lifecycle:SetParametersAsync' from SetParametersAsync override")
	}
}

// TestBlazorDeepDispose verifies IDisposable.Dispose() is emitted as lifecycle.
func TestBlazorDeepDispose(t *testing.T) {
	src := `@implements IDisposable

@code {
    public void Dispose()
    {
        timer?.Dispose();
    }
}`
	ents := extract(t, "custom_csharp_blazor_deep", fi("Timer.razor", "csharp", src))

	if !containsEntity(ents, "SCOPE.Operation", "lifecycle:Dispose") {
		t.Error("expected state_setter_emission 'lifecycle:Dispose' from IDisposable.Dispose()")
	}
}

// TestBlazorDeepShouldRender verifies ShouldRender override is emitted.
func TestBlazorDeepShouldRender(t *testing.T) {
	src := `@code {
    private bool _shouldUpdate = true;

    protected override bool ShouldRender()
    {
        return _shouldUpdate;
    }
}`
	ents := extract(t, "custom_csharp_blazor_deep", fi("OptimizedPage.razor", "csharp", src))

	if !containsEntity(ents, "SCOPE.Operation", "lifecycle:ShouldRender") {
		t.Error("expected state_setter_emission 'lifecycle:ShouldRender' from ShouldRender override")
	}
}

// ---------------------------------------------------------------------------
// No-match guard
// ---------------------------------------------------------------------------

func TestBlazorDeepNoMatch(t *testing.T) {
	src := `namespace MyApp { class Helper { public static string Format(string s) => s; } }`
	ents := extract(t, "custom_csharp_blazor_deep", fi("Helper.cs", "csharp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities from plain non-Blazor class, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Integration — realistic Blazor component covers multiple caps in one file
// ---------------------------------------------------------------------------

func TestBlazorDeepIntegration(t *testing.T) {
	src := `@page "/users/{userId}"
@rendermode InteractiveServer
@inject IUserService UserService
@inject NavigationManager Nav

<h1>@user?.Name</h1>

@if (user == null)
{
    <p>Loading...</p>
}
else
{
    @foreach (var role in user.Roles)
    {
        <span>@role</span>
    }
}

<input @bind="searchFilter" />

@code {
    [Parameter]
    public string UserId { get; set; }

    [Parameter]
    public EventCallback<string> OnUserSelected { get; set; }

    private string searchFilter = "";
    private UserDto user;

    protected override async Task OnInitializedAsync()
    {
        user = await UserService.GetByIdAsync(UserId);
    }

    protected override void OnParametersSet()
    {
        StateHasChanged();
    }

    public void Dispose()
    {
        UserService?.Dispose();
    }
}`
	ents := extract(t, "custom_csharp_blazor_deep", fi("UserPage.razor", "csharp", src))

	checks := []struct {
		desc string
		kind string
		name string
	}{
		{"razor filename component", "SCOPE.UIComponent", "UserPage"},
		{"rendermode component", "SCOPE.UIComponent", "rendermode:InteractiveServer"},
		{"typed string parameter", "SCOPE.Pattern", "param:string:UserId"},
		{"EventCallback parameter", "SCOPE.Pattern", "callback:string:OnUserSelected"},
		{"@bind two-way binding", "SCOPE.Pattern", "bind:searchFilter"},
		{"Dispose lifecycle", "SCOPE.Operation", "lifecycle:Dispose"},
	}

	for _, c := range checks {
		if !containsEntity(ents, c.kind, c.name) {
			t.Errorf("integration: expected %s entity %q", c.desc, c.name)
		}
	}

	// Check route param extracted
	if !containsNamePrefix(ents, "route:param:/users/{userId}:userId") {
		t.Error("integration: expected route:param entity for {userId}")
	}

	// Check @foreach and @if branch conditions
	foundForeach := false
	for _, e := range ents {
		if e.Subtype == "branch_conditions" {
			foundForeach = true
			break
		}
	}
	if !foundForeach {
		t.Error("integration: expected branch_conditions entity")
	}
}
