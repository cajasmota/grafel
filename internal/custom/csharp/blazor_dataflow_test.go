package csharp_test

// ---------------------------------------------------------------------------
// Blazor / MAUI / Xamarin Data Flow
// ---------------------------------------------------------------------------

import "testing"

func TestBlazorDataflowPropExtraction(t *testing.T) {
	src := `
@code {
    [Parameter]
    public string Title { get; set; }

    [Parameter]
    public int Count { get; set; }
}
`
	ents := extract(t, "custom_csharp_blazor_dataflow", fi("Component.razor", "csharp", src))

	found := false
	for _, e := range ents {
		if e.Subtype == "prop_extraction" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected prop_extraction entity from [Parameter] declaration")
	}
}

func TestBlazorDataflowCascadingParam(t *testing.T) {
	src := `
@code {
    [CascadingParameter]
    public AppState AppState { get; set; }
}
`
	ents := extract(t, "custom_csharp_blazor_dataflow", fi("Child.razor", "csharp", src))

	foundProp := false
	foundState := false
	for _, e := range ents {
		if e.Subtype == "prop_extraction" {
			foundProp = true
		}
		if e.Subtype == "state_management" {
			foundState = true
		}
	}
	if !foundProp {
		t.Error("expected prop_extraction from [CascadingParameter]")
	}
	if !foundState {
		t.Error("expected state_management from [CascadingParameter]")
	}
}

func TestBlazorDataflowStateHasChanged(t *testing.T) {
	src := `
@code {
    private async Task Refresh()
    {
        await LoadData();
        StateHasChanged();
    }
}
`
	ents := extract(t, "custom_csharp_blazor_dataflow", fi("Page.razor", "csharp", src))

	found := false
	for _, e := range ents {
		if e.Subtype == "state_management" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected state_management from StateHasChanged() call")
	}
}

func TestBlazorDataflowDataFetching(t *testing.T) {
	src := `
@inject HttpClient Http

@code {
    private WeatherForecast[] forecasts;

    protected override async Task OnInitializedAsync()
    {
        forecasts = await Http.GetFromJsonAsync<WeatherForecast[]>("weatherforecast");
    }
}
`
	ents := extract(t, "custom_csharp_blazor_dataflow", fi("FetchData.razor", "csharp", src))

	foundFetch := false
	for _, e := range ents {
		if e.Subtype == "data_fetching" {
			foundFetch = true
			break
		}
	}
	if !foundFetch {
		t.Error("expected data_fetching entity from @inject HttpClient + GetFromJsonAsync")
	}
}

func TestBlazorDataflowBranchConditions(t *testing.T) {
	src := `
@if (forecasts == null)
{
    <p>Loading...</p>
}
else
{
    @foreach (var f in forecasts)
    {
        <p>@f.Summary</p>
    }
}

@switch (status)
{
    case "ok":
        <p>All good</p>
        break;
}
`
	ents := extract(t, "custom_csharp_blazor_dataflow", fi("Weather.razor", "csharp", src))

	foundBranch := false
	for _, e := range ents {
		if e.Subtype == "branch_conditions" {
			foundBranch = true
			break
		}
	}
	if !foundBranch {
		t.Error("expected branch_conditions from @if / @switch in Razor markup")
	}
}

func TestBlazorDataflowCodeBranch(t *testing.T) {
	src := `
@code {
    private void ProcessResult(string result)
    {
        if (result == null) {
            return;
        }
    }
}
`
	ents := extract(t, "custom_csharp_blazor_dataflow", fi("Logic.razor", "csharp", src))

	found := false
	for _, e := range ents {
		if e.Subtype == "branch_conditions" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected branch_conditions from if statement in @code block")
	}
}

func TestMAUIBindableProperty(t *testing.T) {
	src := `
using Microsoft.Maui.Controls;

public class MyControl : ContentView
{
    public static readonly BindableProperty TitleProperty =
        BindableProperty.Create(nameof(Title), typeof(string), typeof(MyControl));

    public string Title
    {
        get => (string)GetValue(TitleProperty);
        set => SetValue(TitleProperty, value);
    }
}
`
	ents := extract(t, "custom_csharp_blazor_dataflow", fi("MyControl.cs", "csharp", src))

	foundProp := false
	foundState := false
	for _, e := range ents {
		if e.Subtype == "prop_extraction" {
			foundProp = true
		}
		if e.Subtype == "state_management" {
			foundState = true
		}
	}
	if !foundProp {
		t.Error("expected prop_extraction from BindableProperty.Create")
	}
	if !foundState {
		t.Error("expected state_management from SetValue(TitleProperty, value)")
	}
}

func TestMAUIPlatformBranch(t *testing.T) {
	src := `
using Microsoft.Maui.Devices;

public class PlatformService
{
    public void Configure()
    {
        if (DeviceInfo.Platform == DevicePlatform.Android)
        {
            // Android-specific setup
        }
        if (Device.RuntimePlatform == "iOS")
        {
            // iOS-specific setup
        }
    }
}
`
	ents := extract(t, "custom_csharp_blazor_dataflow", fi("PlatformService.cs", "csharp", src))

	foundBranch := false
	for _, e := range ents {
		if e.Subtype == "branch_conditions" {
			foundBranch = true
			break
		}
	}
	if !foundBranch {
		t.Error("expected branch_conditions from Device.RuntimePlatform / DeviceInfo.Platform comparison")
	}
}

func TestBlazorDataflowNoMatch(t *testing.T) {
	src := `namespace App { class Helper { } }`
	ents := extract(t, "custom_csharp_blazor_dataflow", fi("Helper.cs", "csharp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
