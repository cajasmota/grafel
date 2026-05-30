// Package csharp — Data Flow extractor for Blazor (Server / WASM),
// .NET MAUI, and Xamarin C# source files.
//
// Covers the "Data Flow" capability group:
//
//	prop_extraction:
//	  [Parameter] / [CascadingParameter] property declarations emitted as
//	  prop_extraction entities (complements blazor.go which emits SCOPE.Pattern;
//	  this extractor uses subtype "prop_extraction" explicitly).
//	  MAUI/Xamarin: BindableProperty declarations.
//
//	state_management:
//	  [CascadingParameter] — state threading via cascade.
//	  StateHasChanged() calls — explicit re-render trigger.
//	  IStateService / redux-like pattern: InjectState<T>() / Subscribe<T>().
//	  MAUI/Xamarin: ObservableProperty / INotifyPropertyChanged.
//
//	data_fetching:
//	  @inject HttpClient / IHttpClientFactory usage together with
//	  awaited GetAsync / PostAsync / GetFromJsonAsync calls.
//	  MAUI/Xamarin: HttpClient usage in code-behind files.
//
//	branch_conditions:
//	  @if / @switch directives in .razor files (markup branching).
//	  if/else/switch inside @code blocks (code branching).
//	  MAUI/Xamarin: Device.RuntimePlatform == ... conditional branches.
//
// All four subtypes share the same registration key
// "custom_csharp_blazor_dataflow"; the subtype field on each entity
// discriminates which capability is being recorded.
package csharp

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_csharp_blazor_dataflow", &blazorDataflowExtractor{})
}

type blazorDataflowExtractor struct{}

func (e *blazorDataflowExtractor) Language() string { return "custom_csharp_blazor_dataflow" }

// ---------------------------------------------------------------------------
// Regexes — prop_extraction
// ---------------------------------------------------------------------------

var (
	// [Parameter] public <Type> <Name> { get; set; }
	reBDFParameter = regexp.MustCompile(
		`\[Parameter\]\s*(?:public\s+)?(?:\w+(?:<[^>]+>)?)\s+(\w+)`,
	)
	// [CascadingParameter] public <Type> <Name> { ... }
	reBDFCascadingParameter = regexp.MustCompile(
		`\[CascadingParameter\]\s*(?:public\s+)?(?:\w+(?:<[^>]+>)?)\s+(\w+)`,
	)
	// MAUI/Xamarin BindableProperty.Create(...)
	reBDFBindableProperty = regexp.MustCompile(
		`BindableProperty\.Create\s*\(\s*(?:nameof\s*\()?(\w+)`,
	)
	// Xamarin.Forms ObservableProperty (CommunityToolkit)
	reBDFObservableProperty = regexp.MustCompile(
		`\[ObservableProperty\]\s*(?:private\s+)?(?:\w+(?:<[^>]+>)?)\s+(\w+)`,
	)
)

// ---------------------------------------------------------------------------
// Regexes — state_management
// ---------------------------------------------------------------------------

var (
	// StateHasChanged() — Blazor explicit re-render.
	reBDFStateHasChanged = regexp.MustCompile(
		`\bStateHasChanged\s*\(\s*\)`,
	)
	// InvokeAsync(StateHasChanged) — thread-safe re-render.
	reBDFInvokeState = regexp.MustCompile(
		`\bInvokeAsync\s*\(\s*StateHasChanged\b`,
	)
	// INotifyPropertyChanged.OnPropertyChanged / RaisePropertyChanged — MAUI/Xamarin.
	reBDFPropertyChanged = regexp.MustCompile(
		`\b(?:OnPropertyChanged|RaisePropertyChanged|NotifyPropertyChanged)\s*\(`,
	)
	// SetValue(BindableProperty, ...) — MAUI/Xamarin bindable state set.
	reBDFSetValue = regexp.MustCompile(
		`\bSetValue\s*\(\s*\w+Property\b`,
	)
)

// ---------------------------------------------------------------------------
// Regexes — data_fetching
// ---------------------------------------------------------------------------

var (
	// @inject HttpClient / @inject IHttpClientFactory
	reBDFInjectHTTP = regexp.MustCompile(
		`@inject\s+(?:HttpClient|IHttpClientFactory)\s+(\w+)`,
	)
	// await http.GetAsync / PostAsync / GetFromJsonAsync etc.
	reBDFHTTPCall = regexp.MustCompile(
		`\bawait\s+\w+\s*\.\s*(?:GetAsync|PostAsync|PutAsync|DeleteAsync|GetFromJsonAsync|PostAsJsonAsync|PutAsJsonAsync|SendAsync|GetStringAsync|GetStreamAsync)\s*\(`,
	)
	// HttpClient field injection (ctor injection in code-behind).
	reBDFHTTPClientField = regexp.MustCompile(
		`\bHttpClient\s+(\w+)\s*(?:;|=|\{)`,
	)
	// await _http.GetFromJsonAsync<T> / PostAsJsonAsync<T>
	reBDFHTTPJson = regexp.MustCompile(
		`\b(?:GetFromJsonAsync|PostAsJsonAsync|PutAsJsonAsync|ReadFromJsonAsync)\s*<\s*(\w+)\s*>`,
	)
)

// ---------------------------------------------------------------------------
// Regexes — branch_conditions
// ---------------------------------------------------------------------------

var (
	// @if (...) in Razor markup
	reBDFRazorIf = regexp.MustCompile(
		`(?m)^\s*@if\s*\(`,
	)
	// @switch (...) in Razor markup
	reBDFRazorSwitch = regexp.MustCompile(
		`(?m)^\s*@switch\s*\(`,
	)
	// if (...) inside @code block or .cs code-behind
	reBDFCodeIf = regexp.MustCompile(
		`(?m)\bif\s*\([^)]+\)\s*\{`,
	)
	// Device.RuntimePlatform == "iOS" / "Android" — MAUI/Xamarin platform branching
	reBDFPlatformBranch = regexp.MustCompile(
		`\bDevice\.(?:RuntimePlatform|Idiom)\s*==\s*["']?\w+["']?`,
	)
	// DeviceInfo.Platform == DevicePlatform.Android — .NET MAUI style
	reBDFDeviceInfo = regexp.MustCompile(
		`\bDeviceInfo\.Platform\s*==\s*DevicePlatform\.\w+`,
	)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *blazorDataflowExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.csharp_blazor_dataflow_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "csharp" {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// -------------------------------------------------------------------------
	// prop_extraction
	// -------------------------------------------------------------------------

	for _, m := range reBDFParameter.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity("param:"+name, "SCOPE.Pattern", "prop_extraction", file.Path, "csharp", line)
		setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_BLAZOR_PARAMETER")
		add(ent)
	}

	for _, m := range reBDFCascadingParameter.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity("cascading:"+name, "SCOPE.Pattern", "prop_extraction", file.Path, "csharp", line)
		setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_BLAZOR_CASCADING_PARAMETER")
		add(ent)
	}

	for _, m := range reBDFBindableProperty.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity("bindable:"+name, "SCOPE.Pattern", "prop_extraction", file.Path, "csharp", line)
		setProps(&ent, "framework", "maui", "provenance", "INFERRED_FROM_BINDABLE_PROPERTY")
		add(ent)
	}

	for _, m := range reBDFObservableProperty.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity("observable:"+name, "SCOPE.Pattern", "prop_extraction", file.Path, "csharp", line)
		setProps(&ent, "framework", "xamarin", "provenance", "INFERRED_FROM_OBSERVABLE_PROPERTY")
		add(ent)
	}

	// -------------------------------------------------------------------------
	// state_management
	// -------------------------------------------------------------------------

	for _, m := range reBDFCascadingParameter.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity("state:cascade:"+name, "SCOPE.Pattern", "state_management", file.Path, "csharp", line)
		setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_CASCADING_PARAMETER")
		add(ent)
	}

	for _, m := range reBDFStateHasChanged.FindAllStringIndex(src, -1) {
		line := lineOf(src, m[0])
		name := "state:StateHasChanged:" + file.Path + ":" + itoa(line)
		ent := makeEntity(name, "SCOPE.Pattern", "state_management", file.Path, "csharp", line)
		setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_STATE_HAS_CHANGED")
		add(ent)
	}

	for _, m := range reBDFInvokeState.FindAllStringIndex(src, -1) {
		line := lineOf(src, m[0])
		name := "state:InvokeAsyncState:" + file.Path + ":" + itoa(line)
		ent := makeEntity(name, "SCOPE.Pattern", "state_management", file.Path, "csharp", line)
		setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_INVOKE_ASYNC_STATE")
		add(ent)
	}

	for _, m := range reBDFPropertyChanged.FindAllStringIndex(src, -1) {
		line := lineOf(src, m[0])
		name := "state:PropertyChanged:" + file.Path + ":" + itoa(line)
		ent := makeEntity(name, "SCOPE.Pattern", "state_management", file.Path, "csharp", line)
		setProps(&ent, "framework", "maui", "provenance", "INFERRED_FROM_PROPERTY_CHANGED")
		add(ent)
	}

	for _, m := range reBDFSetValue.FindAllStringIndex(src, -1) {
		line := lineOf(src, m[0])
		name := "state:SetValue:" + file.Path + ":" + itoa(line)
		ent := makeEntity(name, "SCOPE.Pattern", "state_management", file.Path, "csharp", line)
		setProps(&ent, "framework", "maui", "provenance", "INFERRED_FROM_BINDABLE_SET_VALUE")
		add(ent)
	}

	// -------------------------------------------------------------------------
	// data_fetching
	// -------------------------------------------------------------------------

	for _, m := range reBDFInjectHTTP.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity("http:inject:"+name, "SCOPE.Pattern", "data_fetching", file.Path, "csharp", line)
		setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_INJECT_HTTP_CLIENT")
		add(ent)
	}

	for _, m := range reBDFHTTPClientField.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity("http:field:"+name, "SCOPE.Pattern", "data_fetching", file.Path, "csharp", line)
		setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_HTTP_CLIENT_FIELD")
		add(ent)
	}

	for _, m := range reBDFHTTPCall.FindAllStringIndex(src, -1) {
		line := lineOf(src, m[0])
		name := "http:call:" + file.Path + ":" + itoa(line)
		ent := makeEntity(name, "SCOPE.Pattern", "data_fetching", file.Path, "csharp", line)
		setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_HTTP_AWAIT_CALL")
		add(ent)
	}

	for _, m := range reBDFHTTPJson.FindAllStringSubmatchIndex(src, -1) {
		entityType := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		name := "http:json:" + entityType + ":" + file.Path + ":" + itoa(line)
		ent := makeEntity(name, "SCOPE.Pattern", "data_fetching", file.Path, "csharp", line)
		setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_HTTP_JSON_CALL",
			"entity_type", entityType)
		add(ent)
	}

	// -------------------------------------------------------------------------
	// branch_conditions
	// -------------------------------------------------------------------------

	for _, m := range reBDFRazorIf.FindAllStringIndex(src, -1) {
		line := lineOf(src, m[0])
		name := "branch:razorif:" + file.Path + ":" + itoa(line)
		ent := makeEntity(name, "SCOPE.Pattern", "branch_conditions", file.Path, "csharp", line)
		setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_RAZOR_IF",
			"kind", "razor_conditional")
		add(ent)
	}

	for _, m := range reBDFRazorSwitch.FindAllStringIndex(src, -1) {
		line := lineOf(src, m[0])
		name := "branch:razorswitch:" + file.Path + ":" + itoa(line)
		ent := makeEntity(name, "SCOPE.Pattern", "branch_conditions", file.Path, "csharp", line)
		setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_RAZOR_SWITCH",
			"kind", "razor_switch")
		add(ent)
	}

	for _, m := range reBDFCodeIf.FindAllStringIndex(src, -1) {
		line := lineOf(src, m[0])
		name := "branch:codeif:" + file.Path + ":" + itoa(line)
		ent := makeEntity(name, "SCOPE.Pattern", "branch_conditions", file.Path, "csharp", line)
		setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_CODE_IF",
			"kind", "code_conditional")
		add(ent)
	}

	for _, m := range reBDFPlatformBranch.FindAllStringIndex(src, -1) {
		line := lineOf(src, m[0])
		name := "branch:platform:" + file.Path + ":" + itoa(line)
		ent := makeEntity(name, "SCOPE.Pattern", "branch_conditions", file.Path, "csharp", line)
		setProps(&ent, "framework", "maui", "provenance", "INFERRED_FROM_DEVICE_PLATFORM",
			"kind", "platform_conditional")
		add(ent)
	}

	for _, m := range reBDFDeviceInfo.FindAllStringIndex(src, -1) {
		line := lineOf(src, m[0])
		name := "branch:deviceinfo:" + file.Path + ":" + itoa(line)
		ent := makeEntity(name, "SCOPE.Pattern", "branch_conditions", file.Path, "csharp", line)
		setProps(&ent, "framework", "maui", "provenance", "INFERRED_FROM_DEVICE_INFO_PLATFORM",
			"kind", "platform_conditional")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
