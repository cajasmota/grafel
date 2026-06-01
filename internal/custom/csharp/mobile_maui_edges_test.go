package csharp_test

// ---------------------------------------------------------------------------
// .NET MAUI edge extraction (#3579):
//   - Shell-routing NAVIGATES_TO (RegisterRoute / ShellContent / GoToAsync)
//   - MVVM view↔viewmodel USES (BindingContext / DI ctor / CommunityToolkit)
//   - DI registration BINDS / REGISTERS (AddSingleton<IFoo,Foo> / AddTransient<X>)
//
// These assert SPECIFIC edges (ToID + Kind + owning props), not len>0.
// extractFull / fi are shared helpers from the csharp_test package.
// ---------------------------------------------------------------------------

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// mauiEdgeOwner returns the first entity carrying an edge {toID, kind}, or nil.
func mauiEdgeOwner(ents []types.EntityRecord, toID, kind string) *types.EntityRecord {
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.ToID == toID && r.Kind == kind {
				return &ents[i]
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Shell routing → NAVIGATES_TO
// ---------------------------------------------------------------------------

func TestMauiEdges_RegisterRoute_NavigatesTo(t *testing.T) {
	src := `
public partial class AppShell : Shell
{
    public AppShell()
    {
        Routing.RegisterRoute("details", typeof(DetailsPage));
    }
}
`
	ents := extractFull(t, "custom_csharp_mobile_platform", fi("AppShell.xaml.cs", "csharp", src))
	owner := mauiEdgeOwner(ents, "route:details", "NAVIGATES_TO")
	if owner == nil {
		t.Fatalf("expected NAVIGATES_TO route:details from RegisterRoute")
	}
	if owner.Subtype != "navigation_extraction" {
		t.Errorf("expected subtype navigation_extraction, got %q", owner.Subtype)
	}
	if owner.Properties["target_page"] != "DetailsPage" {
		t.Errorf("expected target_page=DetailsPage, got %q", owner.Properties["target_page"])
	}
}

func TestMauiEdges_ShellContentXaml_NavigatesTo(t *testing.T) {
	src := `
<ShellContent Route="home" ContentTemplate="{DataTemplate local:HomePage}" />
`
	ents := extractFull(t, "custom_csharp_mobile_platform", fi("AppShell.xaml", "csharp", src))
	owner := mauiEdgeOwner(ents, "route:home", "NAVIGATES_TO")
	if owner == nil {
		t.Fatalf("expected NAVIGATES_TO route:home from <ShellContent>")
	}
	if owner.Properties["target_page"] != "HomePage" {
		t.Errorf("expected target_page=HomePage from ContentTemplate, got %q", owner.Properties["target_page"])
	}
}

func TestMauiEdges_GoToAsyncString_NavigatesTo(t *testing.T) {
	src := `
public async Task Open()
{
    await Shell.Current.GoToAsync("//details");
}
`
	ents := extractFull(t, "custom_csharp_mobile_platform", fi("MainPage.xaml.cs", "csharp", src))
	// "//details" must normalize to route:details (leading slashes stripped).
	if mauiEdgeOwner(ents, "route:details", "NAVIGATES_TO") == nil {
		t.Fatalf("expected normalized NAVIGATES_TO route:details from GoToAsync(\"//details\")")
	}
}

func TestMauiEdges_GoToAsyncNameof_NavigatesTo(t *testing.T) {
	src := `
public async Task Open()
{
    await Shell.Current.GoToAsync(nameof(DetailsPage));
}
`
	ents := extractFull(t, "custom_csharp_mobile_platform", fi("MainPage.xaml.cs", "csharp", src))
	owner := mauiEdgeOwner(ents, "route:DetailsPage", "NAVIGATES_TO")
	if owner == nil {
		t.Fatalf("expected NAVIGATES_TO route:DetailsPage from GoToAsync(nameof(DetailsPage))")
	}
	if owner.Properties["target_page"] != "DetailsPage" {
		t.Errorf("expected target_page=DetailsPage, got %q", owner.Properties["target_page"])
	}
}

// ---------------------------------------------------------------------------
// MVVM view↔viewmodel → USES
// ---------------------------------------------------------------------------

func TestMauiEdges_BindingContextNew_Uses(t *testing.T) {
	src := `
public partial class MainPage : ContentPage
{
    public MainPage()
    {
        InitializeComponent();
        BindingContext = new MainViewModel();
    }
}
`
	ents := extractFull(t, "custom_csharp_mobile_platform", fi("MainPage.xaml.cs", "csharp", src))
	owner := mauiEdgeOwner(ents, "viewmodel:MainViewModel", "USES")
	if owner == nil {
		t.Fatalf("expected MainPage USES viewmodel:MainViewModel")
	}
	if owner.Properties["view"] != "MainPage" {
		t.Errorf("expected view=MainPage on USES edge, got %q", owner.Properties["view"])
	}
}

func TestMauiEdges_CtorInjectedViewModel_Uses(t *testing.T) {
	src := `
public partial class ProductPage : ContentPage
{
    public ProductPage(ProductViewModel viewModel)
    {
        InitializeComponent();
        BindingContext = viewModel;
    }
}
`
	ents := extractFull(t, "custom_csharp_mobile_platform", fi("ProductPage.xaml.cs", "csharp", src))
	owner := mauiEdgeOwner(ents, "viewmodel:ProductViewModel", "USES")
	if owner == nil {
		t.Fatalf("expected ProductPage USES viewmodel:ProductViewModel via ctor injection")
	}
	if owner.Properties["view"] != "ProductPage" {
		t.Errorf("expected view=ProductPage, got %q", owner.Properties["view"])
	}
}

func TestMauiEdges_CommunityToolkitViewModel_And_RelayCommand(t *testing.T) {
	src := `
using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;

public partial class CounterViewModel : ObservableObject
{
    [ObservableProperty]
    private int count;

    [RelayCommand]
    private void Increment() => Count++;
}
`
	ents := extractFull(t, "custom_csharp_mobile_platform", fi("CounterViewModel.cs", "csharp", src))
	foundVM := false
	foundCmd := false
	for i := range ents {
		e := ents[i]
		if e.Kind == "SCOPE.Component" && e.Subtype == "state_management" && e.Name == "viewmodel:CounterViewModel" {
			foundVM = true
		}
		if e.Name == "command:Increment" && e.Subtype == "state_management" {
			foundCmd = true
		}
	}
	if !foundVM {
		t.Errorf("expected viewmodel:CounterViewModel marker entity")
	}
	if !foundCmd {
		t.Errorf("expected command:Increment entity from [RelayCommand]")
	}
}

// ---------------------------------------------------------------------------
// DI registration → BINDS / REGISTERS
// ---------------------------------------------------------------------------

func TestMauiEdges_AddSingletonTwoArg_Binds(t *testing.T) {
	src := `
public static class MauiProgram
{
    public static MauiApp CreateMauiApp()
    {
        var builder = MauiApp.CreateBuilder();
        builder.Services.AddSingleton<IDataService, DataService>();
        return builder.Build();
    }
}
`
	ents := extractFull(t, "custom_csharp_mobile_platform", fi("MauiProgram.cs", "csharp", src))
	owner := mauiEdgeOwner(ents, "impl:DataService", "BINDS")
	if owner == nil {
		t.Fatalf("expected BINDS impl:DataService from AddSingleton<IDataService,DataService>")
	}
	if owner.Properties["interface"] != "IDataService" {
		t.Errorf("expected interface=IDataService, got %q", owner.Properties["interface"])
	}
	if owner.Properties["lifetime"] != "Singleton" {
		t.Errorf("expected lifetime=Singleton, got %q", owner.Properties["lifetime"])
	}
}

func TestMauiEdges_AddTransientOneArg_Registers(t *testing.T) {
	src := `
public static class MauiProgram
{
    public static MauiApp CreateMauiApp()
    {
        var builder = MauiApp.CreateBuilder();
        builder.Services.AddTransient<MainViewModel>();
        return builder.Build();
    }
}
`
	ents := extractFull(t, "custom_csharp_mobile_platform", fi("MauiProgram.cs", "csharp", src))
	owner := mauiEdgeOwner(ents, "impl:MainViewModel", "REGISTERS")
	if owner == nil {
		t.Fatalf("expected REGISTERS impl:MainViewModel from AddTransient<MainViewModel>")
	}
	if owner.Properties["lifetime"] != "Transient" {
		t.Errorf("expected lifetime=Transient, got %q", owner.Properties["lifetime"])
	}
}

// Two-arg form must NOT also emit a one-arg self-registration edge.
func TestMauiEdges_NoDoubleEmit_TwoArgVsOneArg(t *testing.T) {
	src := `
builder.Services.AddScoped<IRepo, Repo>();
`
	ents := extractFull(t, "custom_csharp_mobile_platform", fi("MauiProgram.cs", "csharp", src))
	if mauiEdgeOwner(ents, "impl:IRepo", "REGISTERS") != nil {
		t.Errorf("two-arg AddScoped must not emit a self REGISTERS edge for the interface")
	}
	if mauiEdgeOwner(ents, "impl:Repo", "BINDS") == nil {
		t.Errorf("expected BINDS impl:Repo from AddScoped<IRepo,Repo>")
	}
}
