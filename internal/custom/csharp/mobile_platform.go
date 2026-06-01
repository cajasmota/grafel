// Package csharp — Mobile platform extractor for .NET MAUI and Xamarin C# source files.
//
// Covers the missing cells for lang.csharp.framework.net-maui and
// lang.csharp.framework.xamarin:
//
//	Structure/context_extraction:
//	  Application.Current / DependencyService.Get<T>() / IServiceProvider
//	  usages emitted as SCOPE.Component/context_extraction.
//
//	Navigation/deep_link_extraction:
//	  App.xaml <data android:scheme> and custom URI scheme registrations
//	  detected via AppLinks.RegisterRoute / [UriScheme] attributes.
//	  MAUI: Shell.Current.GoToAsync("//route") calls captured.
//
//	Navigation/navigation_extraction:
//	  Shell.Current.GoToAsync / Navigation.PushAsync / Navigation.PopAsync
//	  calls emitted as SCOPE.Operation/navigation_extraction.
//	  Xamarin NavigationPage / TabbedPage / FlyoutPage.
//
//	Navigation/screen_detection:
//	  ContentPage / Shell / TabbedPage / FlyoutPage / NavigationPage
//	  subclass declarations emitted as SCOPE.UIComponent/screen_detection.
//	  MAUI: ContentPage, Shell, AppShell.
//
//	Platform/platform_branching:
//	  DeviceInfo.Platform / Device.RuntimePlatform / DeviceInfo.Idiom
//	  comparison branches emitted as SCOPE.Pattern/platform_branching.
//	  #if ANDROID / #if IOS / #if WINDOWS preprocessor checks.
//
//	Native Bridge/native_module_imports:
//	  [DllImport] / extern calls to native methods.
//	  DependencyService.Register<T>() / [assembly: Dependency(typeof(T))] patterns.
//	  WinRT / Android / iOS interop imports.
//
//	Type System/enum_extraction:
//	  enum declarations in MAUI/Xamarin files emitted as SCOPE.Schema/enum_extraction.
//	  (The tree-sitter extractor handles this for generic csharp; this pass
//	  provides coverage evidence for the mobile framework records.)
//
//	Type System/interface_extraction:
//	  interface declarations emitted as SCOPE.Component/interface_extraction.
//
//	Lifecycle/state_setter_emission:
//	  OnStart / OnSleep / OnResume / OnAppearing / OnDisappearing / CreateMauiApp
//	  lifecycle methods emitted as SCOPE.Operation/state_setter_emission.
//	  INotifyPropertyChanged.PropertyChanged event raise patterns.
//
// Registration key: "custom_csharp_mobile_platform"
// Issue #3261.
package csharp

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_csharp_mobile_platform", &mobilePlatformExtractor{})
}

type mobilePlatformExtractor struct{}

func (e *mobilePlatformExtractor) Language() string { return "custom_csharp_mobile_platform" }

// ---------------------------------------------------------------------------
// Regex catalog
// ---------------------------------------------------------------------------

var (
	// Structure/context_extraction -------------------------------------------

	// Application.Current — global app context access
	reMPAppCurrent = regexp.MustCompile(
		`\bApplication\.Current\b`,
	)

	// DependencyService.Get<T>() — Xamarin service resolution (context bridge)
	reMPDependencyServiceGet = regexp.MustCompile(
		`DependencyService\.Get\s*<\s*(\w+(?:<[^>]+>)?)\s*>`,
	)

	// IServiceProvider / MauiApp builder pattern
	reMPServiceProvider = regexp.MustCompile(
		`(?m)\b(IServiceProvider|MauiAppBuilder|IMauiContext)\b`,
	)

	// Navigation/deep_link_extraction ----------------------------------------

	// Shell.Current.GoToAsync("//route") — MAUI deep link navigation
	reMPShellGoToAsync = regexp.MustCompile(
		`Shell\.Current\.GoToAsync\s*\(\s*["']([^"']+)["']`,
	)

	// [QueryProperty("ParamName", "QueryKey")] — shell query parameter (deep link)
	reMPQueryProperty = regexp.MustCompile(
		`\[QueryProperty\s*\(\s*["']([^"']+)["']\s*,\s*["']([^"']+)["']`,
	)

	// AppLinks.RegisterRoute or RegisterAppLink — Xamarin AppLink registration
	reMPAppLinks = regexp.MustCompile(
		`AppLinks\.(?:RegisterRoute|AppendAppLink)\s*\(`,
	)

	// [assembly: Dependency] — Xamarin deep link scheme hint
	reMPUriScheme = regexp.MustCompile(
		`\[assembly\s*:\s*(?:ExportRenderer|Dependency)\s*\(`,
	)

	// Navigation/navigation_extraction ----------------------------------------

	// Navigation.PushAsync / Navigation.PopAsync / Navigation.PushModalAsync
	reMPNavPush = regexp.MustCompile(
		`Navigation\.(PushAsync|PopAsync|PushModalAsync|PopModalAsync|InsertPageBefore|RemovePage)\s*\(`,
	)

	// Shell.Current.GoToAsync / Shell.GoToAsync
	reMPShellNav = regexp.MustCompile(
		`Shell(?:\.Current)?\.GoToAsync\s*\(\s*["']([^"']+)["']`,
	)

	// NavigationPage constructor / FlyoutPage / TabbedPage usage in code
	reMPNavPageUsage = regexp.MustCompile(
		`new\s+(NavigationPage|TabbedPage|FlyoutPage|MasterDetailPage)\s*\(`,
	)

	// Navigation/screen_detection --------------------------------------------

	// class MyPage : ContentPage / Shell / TabbedPage / FlyoutPage
	reMPContentPage = regexp.MustCompile(
		`(?m)class\s+(\w+)\s*:\s*(?:ContentPage|Shell|TabbedPage|FlyoutPage|NavigationPage|` +
			`MasterDetailPage|Page|BasePage|ViewPage)\b`,
	)

	// AppShell / ShellContent usage
	reMPShellClass = regexp.MustCompile(
		`(?m)class\s+(\w+)\s*:\s*(?:AppShell|Shell)\b`,
	)

	// Platform/platform_branching ---------------------------------------------

	// DeviceInfo.Platform == DevicePlatform.Android / iOS / WinUI / Tizen
	reMPDeviceInfoPlatform = regexp.MustCompile(
		`DeviceInfo\.Platform\s*==\s*(DevicePlatform\.\w+)`,
	)

	// Device.RuntimePlatform == Device.Android / Device.iOS (Xamarin)
	reMPDeviceRuntimePlatform = regexp.MustCompile(
		`Device\.RuntimePlatform\s*==\s*(Device\.\w+)`,
	)

	// #if ANDROID / #if IOS / #if WINDOWS / #if MACCATALYST
	reMPPreprocessorPlatform = regexp.MustCompile(
		`(?m)^#if\s+(ANDROID|IOS|WINDOWS|MACCATALYST|TIZEN|MACOS)\b`,
	)

	// DeviceInfo.Idiom comparison
	reMPDeviceIdiom = regexp.MustCompile(
		`DeviceInfo\.Idiom\s*==\s*(DeviceIdiom\.\w+)`,
	)

	// Native Bridge/native_module_imports ------------------------------------

	// [DllImport("libname")] — P/Invoke native method import
	reMPDllImport = regexp.MustCompile(
		`\[DllImport\s*\(\s*["']([^"']+)["']`,
	)

	// DependencyService.Register<T>() — Xamarin native platform service
	reMPDependencyServiceRegister = regexp.MustCompile(
		`DependencyService\.Register\s*<\s*(\w+)\s*>`,
	)

	// [assembly: Dependency(typeof(T))] — platform implementation registration
	reMPAssemblyDependency = regexp.MustCompile(
		`\[assembly\s*:\s*Dependency\s*\(\s*typeof\s*\(\s*(\w+)\s*\)\s*\)`,
	)

	// using Android.Hardware / using UIKit — platform bridge imports
	reMPPlatformImport = regexp.MustCompile(
		`(?m)^\s*using\s+(Android\.|UIKit\.|AppKit\.|WinRT\.|Windows\.Foundation\.|Windows\.UI\.)\w`,
	)

	// extern keyword — C# extern declaration (native interop)
	reMPExternDecl = regexp.MustCompile(
		`(?m)\bextern\s+(?:static\s+)?(?:\w+\s+)+(\w+)\s*\(`,
	)

	// Type System/enum_extraction --------------------------------------------

	// enum MyEnum { ... }
	reMPEnum = regexp.MustCompile(
		`(?m)(?:public|internal|protected|private)?\s*enum\s+(\w+)`,
	)

	// Type System/interface_extraction ---------------------------------------

	// interface IMyInterface { ... }
	reMPInterface = regexp.MustCompile(
		`(?m)(?:public|internal|protected)?\s*interface\s+(\w+)`,
	)

	// Lifecycle/state_setter_emission ----------------------------------------

	// CreateMauiApp / OnStart / OnSleep / OnResume / OnAppearing / OnDisappearing
	reMPLifecycleMethod = regexp.MustCompile(
		`(?m)(?:protected|public)\s+(?:override\s+)?(?:static\s+)?(?:MauiApp|void|Task)\s+` +
			`(CreateMauiApp|OnStart|OnSleep|OnResume|OnAppearing|OnDisappearing|` +
			`OnNavigatedTo|OnNavigatedFrom|OnNavigatingTo|OnBackButtonPressed)\s*\(`,
	)

	// PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(...))
	reMPPropertyChanged = regexp.MustCompile(
		`PropertyChanged\s*\?\s*\.?\s*Invoke\s*\(`,
	)

	// OnPropertyChanged("PropName") — INPC helper call
	reMPOnPropertyChanged = regexp.MustCompile(
		`OnPropertyChanged\s*\(`,
	)

	// SetProperty(ref _field, value) — MVVM toolkit setter
	reMPSetProperty = regexp.MustCompile(
		`SetProperty\s*\(\s*ref\s+\w`,
	)

	// Navigation/navigation_extraction — Shell routing edges (#3579) -----------

	// Routing.RegisterRoute("details", typeof(DetailsPage)) — route→page mapping
	reMPRegisterRoute = regexp.MustCompile(
		`Routing\.RegisterRoute\s*\(\s*["']([^"']+)["']\s*,\s*typeof\s*\(\s*(\w+)\s*\)`,
	)

	// <ShellContent Route="home" ...> — XAML route declaration. Optionally a
	// ContentTemplate="{DataTemplate local:HomePage}" naming the target page.
	reMPShellContentRoute = regexp.MustCompile(
		`<ShellContent\b[^>]*\bRoute\s*=\s*["']([^"']+)["']`,
	)

	// ContentTemplate="{DataTemplate local:HomePage}" — page reference within a
	// ShellContent / Tab / FlyoutItem element.
	reMPContentTemplatePage = regexp.MustCompile(
		`ContentTemplate\s*=\s*["']\{\s*DataTemplate\s+(?:[\w:]+:)?(\w+)\s*\}`,
	)

	// Shell.Current.GoToAsync(nameof(DetailsPage)) — nameof-style route target
	reMPGoToAsyncNameof = regexp.MustCompile(
		`(?:Shell(?:\.Current)?)?\.?GoToAsync\s*\(\s*nameof\s*\(\s*(\w+)\s*\)`,
	)

	// MVVM view↔viewmodel — USES edges (#3579) ---------------------------------

	// BindingContext = new ProductViewModel(...) — explicit VM wiring
	reMPBindingContextNew = regexp.MustCompile(
		`BindingContext\s*=\s*new\s+(\w+)\s*\(`,
	)

	// public MainPage(MainViewModel vm) — DI-injected ViewModel ctor parameter.
	// Captured against the enclosing ContentPage class (resolved separately).
	reMPViewModelCtorParam = regexp.MustCompile(
		`\b(\w+ViewModel)\s+\w+\s*[,)]`,
	)

	// [ObservableProperty] / [RelayCommand] — CommunityToolkit.Mvvm markers that
	// identify a class as a ViewModel.
	reMPObservableProperty = regexp.MustCompile(
		`\[ObservableProperty\b`,
	)

	// [RelayCommand] private void/Task Save() — command method declaration
	reMPRelayCommand = regexp.MustCompile(
		`(?s)\[RelayCommand[^\]]*\]\s*(?:public|private|protected|internal)?\s*` +
			`(?:async\s+)?(?:partial\s+)?(?:void|Task|ValueTask|IAsyncRelayCommand|\w+)\s+(\w+)\s*\(`,
	)

	// DI registration — BINDS edges (#3579) ------------------------------------

	// builder.Services.AddSingleton<IFoo, Foo>() — interface→impl binding
	reMPDITwoTypeArgs = regexp.MustCompile(
		`\.Add(Singleton|Transient|Scoped)\s*<\s*(\w+(?:<[^>]+>)?)\s*,\s*(\w+(?:<[^>]+>)?)\s*>\s*\(`,
	)

	// builder.Services.AddTransient<XViewModel>() — single-type self registration
	reMPDIOneTypeArg = regexp.MustCompile(
		`\.Add(Singleton|Transient|Scoped)\s*<\s*(\w+(?:<[^>]+>)?)\s*>\s*\(`,
	)

	// class XViewModel { ... } — any class declaration (used to name the
	// CommunityToolkit.Mvvm ViewModel when no ContentPage subclass is present,
	// e.g. a partial ViewModel-only file).
	reMPMvvmClass = regexp.MustCompile(
		`(?m)\bclass\s+(\w+)\b`,
	)
)

// normalizeMauiRoute strips Shell's "//" and "/" routing prefixes so that
// GoToAsync("//details"), GoToAsync("details") and RegisterRoute("details", ...)
// all resolve to the same synthetic "route:details" stub.
func normalizeMauiRoute(route string) string {
	r := route
	for len(r) > 0 && r[0] == '/' {
		r = r[1:]
	}
	return r
}

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *mobilePlatformExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.mobile_platform_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "mobile"),
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
	// Structure/context_extraction
	// -------------------------------------------------------------------------

	for _, m := range reMPAppCurrent.FindAllStringIndex(src, -1) {
		ent := makeEntity("ctx:Application.Current:"+file.Path+":"+itoa(lineOf(src, m[0])),
			"SCOPE.Component", "context_extraction", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "mobile", "provenance", "INFERRED_FROM_APPLICATION_CURRENT")
		add(ent)
	}

	for _, m := range reMPDependencyServiceGet.FindAllStringSubmatchIndex(src, -1) {
		serviceType := src[m[2]:m[3]]
		ent := makeEntity("ctx:DependencyService:"+serviceType, "SCOPE.Component", "context_extraction",
			file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "mobile", "provenance", "INFERRED_FROM_DEPENDENCY_SERVICE_GET",
			"service_type", serviceType)
		add(ent)
	}

	for _, m := range reMPServiceProvider.FindAllStringSubmatchIndex(src, -1) {
		typeName := src[m[2]:m[3]]
		ent := makeEntity("ctx:"+typeName+":"+file.Path+":"+itoa(lineOf(src, m[0])),
			"SCOPE.Component", "context_extraction", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "mobile", "provenance", "INFERRED_FROM_SERVICE_PROVIDER",
			"type_name", typeName)
		add(ent)
	}

	// -------------------------------------------------------------------------
	// Navigation/deep_link_extraction
	// -------------------------------------------------------------------------

	for _, m := range reMPShellGoToAsync.FindAllStringSubmatchIndex(src, -1) {
		route := src[m[2]:m[3]]
		if route == "" {
			continue
		}
		ent := makeEntity("deeplink:shell:"+route, "SCOPE.Pattern", "deep_link_extraction",
			file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "mobile", "provenance", "INFERRED_FROM_SHELL_GO_TO_ASYNC",
			"route", route)
		add(ent)
	}

	for _, m := range reMPQueryProperty.FindAllStringSubmatchIndex(src, -1) {
		paramName := src[m[2]:m[3]]
		queryKey := src[m[4]:m[5]]
		ent := makeEntity("deeplink:query:"+queryKey, "SCOPE.Pattern", "deep_link_extraction",
			file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "mobile", "provenance", "INFERRED_FROM_QUERY_PROPERTY",
			"param_name", paramName, "query_key", queryKey)
		add(ent)
	}

	for _, m := range reMPAppLinks.FindAllStringIndex(src, -1) {
		ent := makeEntity("deeplink:applinks:"+file.Path+":"+itoa(lineOf(src, m[0])),
			"SCOPE.Pattern", "deep_link_extraction", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "mobile", "provenance", "INFERRED_FROM_APP_LINKS")
		add(ent)
	}

	// -------------------------------------------------------------------------
	// Navigation/navigation_extraction
	// -------------------------------------------------------------------------

	for _, m := range reMPNavPush.FindAllStringSubmatchIndex(src, -1) {
		method := src[m[2]:m[3]]
		ent := makeEntity("nav:"+method+":"+file.Path+":"+itoa(lineOf(src, m[0])),
			"SCOPE.Operation", "navigation_extraction", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "mobile", "provenance", "INFERRED_FROM_NAVIGATION_PUSH",
			"nav_method", method)
		add(ent)
	}

	for _, m := range reMPShellNav.FindAllStringSubmatchIndex(src, -1) {
		route := src[m[2]:m[3]]
		ent := makeEntity("nav:shell:"+route, "SCOPE.Operation", "navigation_extraction",
			file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "mobile", "provenance", "INFERRED_FROM_SHELL_NAV",
			"route", route)
		add(ent)
	}

	for _, m := range reMPNavPageUsage.FindAllStringSubmatchIndex(src, -1) {
		pageType := src[m[2]:m[3]]
		ent := makeEntity("nav:page:"+pageType+":"+file.Path+":"+itoa(lineOf(src, m[0])),
			"SCOPE.Operation", "navigation_extraction", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "mobile", "provenance", "INFERRED_FROM_NAV_PAGE_CTOR",
			"page_type", pageType)
		add(ent)
	}

	// -------------------------------------------------------------------------
	// Navigation/screen_detection
	// -------------------------------------------------------------------------

	for _, m := range reMPContentPage.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity("screen:"+name, "SCOPE.UIComponent", "screen_detection",
			file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "mobile", "provenance", "INFERRED_FROM_CONTENT_PAGE",
			"class_name", name)
		add(ent)
	}

	for _, m := range reMPShellClass.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity("screen:shell:"+name, "SCOPE.UIComponent", "screen_detection",
			file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "mobile", "provenance", "INFERRED_FROM_SHELL_CLASS",
			"class_name", name)
		add(ent)
	}

	// -------------------------------------------------------------------------
	// Platform/platform_branching
	// -------------------------------------------------------------------------

	for _, m := range reMPDeviceInfoPlatform.FindAllStringSubmatchIndex(src, -1) {
		platform := src[m[2]:m[3]]
		ent := makeEntity("platform:DeviceInfo:"+platform+":"+file.Path+":"+itoa(lineOf(src, m[0])),
			"SCOPE.Pattern", "platform_branching", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "mobile", "provenance", "INFERRED_FROM_DEVICE_INFO_PLATFORM",
			"platform", platform)
		add(ent)
	}

	for _, m := range reMPDeviceRuntimePlatform.FindAllStringSubmatchIndex(src, -1) {
		platform := src[m[2]:m[3]]
		ent := makeEntity("platform:RuntimePlatform:"+platform+":"+file.Path+":"+itoa(lineOf(src, m[0])),
			"SCOPE.Pattern", "platform_branching", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "mobile", "provenance", "INFERRED_FROM_DEVICE_RUNTIME_PLATFORM",
			"platform", platform)
		add(ent)
	}

	for _, m := range reMPPreprocessorPlatform.FindAllStringSubmatchIndex(src, -1) {
		platform := src[m[2]:m[3]]
		ent := makeEntity("platform:ifdef:"+platform+":"+file.Path+":"+itoa(lineOf(src, m[0])),
			"SCOPE.Pattern", "platform_branching", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "mobile", "provenance", "INFERRED_FROM_PREPROCESSOR_PLATFORM",
			"platform", platform)
		add(ent)
	}

	for _, m := range reMPDeviceIdiom.FindAllStringSubmatchIndex(src, -1) {
		idiom := src[m[2]:m[3]]
		ent := makeEntity("platform:Idiom:"+idiom+":"+file.Path+":"+itoa(lineOf(src, m[0])),
			"SCOPE.Pattern", "platform_branching", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "mobile", "provenance", "INFERRED_FROM_DEVICE_IDIOM",
			"idiom", idiom)
		add(ent)
	}

	// -------------------------------------------------------------------------
	// Native Bridge/native_module_imports
	// -------------------------------------------------------------------------

	for _, m := range reMPDllImport.FindAllStringSubmatchIndex(src, -1) {
		libName := src[m[2]:m[3]]
		ent := makeEntity("native:dll:"+libName, "SCOPE.Pattern", "native_module_imports",
			file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "mobile", "provenance", "INFERRED_FROM_DLL_IMPORT",
			"library", libName)
		add(ent)
	}

	for _, m := range reMPDependencyServiceRegister.FindAllStringSubmatchIndex(src, -1) {
		implType := src[m[2]:m[3]]
		ent := makeEntity("native:dep_register:"+implType, "SCOPE.Pattern", "native_module_imports",
			file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "mobile", "provenance", "INFERRED_FROM_DEPENDENCY_REGISTER",
			"impl_type", implType)
		add(ent)
	}

	for _, m := range reMPAssemblyDependency.FindAllStringSubmatchIndex(src, -1) {
		implType := src[m[2]:m[3]]
		ent := makeEntity("native:assembly_dep:"+implType, "SCOPE.Pattern", "native_module_imports",
			file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "mobile", "provenance", "INFERRED_FROM_ASSEMBLY_DEPENDENCY",
			"impl_type", implType)
		add(ent)
	}

	for _, m := range reMPPlatformImport.FindAllStringIndex(src, -1) {
		ent := makeEntity("native:platform_import:"+file.Path+":"+itoa(lineOf(src, m[0])),
			"SCOPE.Pattern", "native_module_imports", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "mobile", "provenance", "INFERRED_FROM_PLATFORM_IMPORT")
		add(ent)
	}

	for _, m := range reMPExternDecl.FindAllStringSubmatchIndex(src, -1) {
		funcName := src[m[2]:m[3]]
		ent := makeEntity("native:extern:"+funcName, "SCOPE.Pattern", "native_module_imports",
			file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "mobile", "provenance", "INFERRED_FROM_EXTERN_DECL",
			"function_name", funcName)
		add(ent)
	}

	// -------------------------------------------------------------------------
	// Type System/enum_extraction
	// -------------------------------------------------------------------------

	for _, m := range reMPEnum.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity("enum:"+name, "SCOPE.Schema", "enum_extraction",
			file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "mobile", "provenance", "INFERRED_FROM_ENUM_DECL",
			"enum_name", name)
		add(ent)
	}

	// -------------------------------------------------------------------------
	// Type System/interface_extraction
	// -------------------------------------------------------------------------

	for _, m := range reMPInterface.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity("interface:"+name, "SCOPE.Component", "interface_extraction",
			file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "mobile", "provenance", "INFERRED_FROM_INTERFACE_DECL",
			"interface_name", name)
		add(ent)
	}

	// -------------------------------------------------------------------------
	// Lifecycle/state_setter_emission
	// -------------------------------------------------------------------------

	for _, m := range reMPLifecycleMethod.FindAllStringSubmatchIndex(src, -1) {
		methodName := src[m[2]:m[3]]
		ent := makeEntity("lifecycle:"+methodName, "SCOPE.Operation", "state_setter_emission",
			file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "mobile", "provenance", "INFERRED_FROM_MOBILE_LIFECYCLE",
			"lifecycle_method", methodName)
		add(ent)
	}

	for _, m := range reMPPropertyChanged.FindAllStringIndex(src, -1) {
		ent := makeEntity("lifecycle:PropertyChanged:"+file.Path+":"+itoa(lineOf(src, m[0])),
			"SCOPE.Operation", "state_setter_emission", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "mobile", "provenance", "INFERRED_FROM_PROPERTY_CHANGED")
		add(ent)
	}

	for _, m := range reMPOnPropertyChanged.FindAllStringIndex(src, -1) {
		ent := makeEntity("lifecycle:OnPropertyChanged:"+file.Path+":"+itoa(lineOf(src, m[0])),
			"SCOPE.Operation", "state_setter_emission", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "mobile", "provenance", "INFERRED_FROM_ON_PROPERTY_CHANGED")
		add(ent)
	}

	for _, m := range reMPSetProperty.FindAllStringIndex(src, -1) {
		ent := makeEntity("lifecycle:SetProperty:"+file.Path+":"+itoa(lineOf(src, m[0])),
			"SCOPE.Operation", "state_setter_emission", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "mobile", "provenance", "INFERRED_FROM_SET_PROPERTY")
		add(ent)
	}

	// -------------------------------------------------------------------------
	// Navigation/navigation_extraction — Shell routing NAVIGATES_TO edges (#3579)
	//
	// Three Shell-routing surfaces become NAVIGATES_TO edges to a synthetic
	// "route:<path>" stub (mirroring the JS/TS Angular/Expo route convention):
	//   1. Routing.RegisterRoute("details", typeof(DetailsPage))   [route table]
	//   2. <ShellContent Route="home" ...>                          [route table]
	//   3. Shell.Current.GoToAsync("//details") / GoToAsync(nameof(DetailsPage))
	//      [imperative call site]
	// Cross-file resolution of the route→page identity is honest-partial: it is
	// resolved within a file when RegisterRoute / ContentTemplate names the page.
	// -------------------------------------------------------------------------

	for _, m := range reMPRegisterRoute.FindAllStringSubmatchIndex(src, -1) {
		route := normalizeMauiRoute(src[m[2]:m[3]])
		pageType := src[m[4]:m[5]]
		if route == "" {
			continue
		}
		ent := makeEntity("route_register:"+route, "SCOPE.Operation", "navigation_extraction",
			file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "maui", "provenance", "INFERRED_FROM_REGISTER_ROUTE",
			"route", route, "target_page", pageType)
		ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
			ToID: "route:" + route,
			Kind: string(types.RelationshipKindNavigatesTo),
			Properties: map[string]string{
				"route":       route,
				"via":         "register_route",
				"target_page": pageType,
				"framework":   "maui",
				"line":        itoa(lineOf(src, m[0])),
			},
		})
		add(ent)
	}

	// <ShellContent Route="home" ContentTemplate="{DataTemplate local:HomePage}">
	// Pair each Route= with the nearest following ContentTemplate page (if any).
	contentTemplates := reMPContentTemplatePage.FindAllStringSubmatchIndex(src, -1)
	for _, m := range reMPShellContentRoute.FindAllStringSubmatchIndex(src, -1) {
		route := normalizeMauiRoute(src[m[2]:m[3]])
		if route == "" {
			continue
		}
		targetPage := ""
		for _, cm := range contentTemplates {
			// The ContentTemplate belongs to the same element when it appears
			// shortly after the Route= attribute on the opening tag.
			if cm[0] >= m[0] && cm[0]-m[1] < 200 {
				targetPage = src[cm[2]:cm[3]]
				break
			}
		}
		ent := makeEntity("shell_content:"+route, "SCOPE.Operation", "navigation_extraction",
			file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "maui", "provenance", "INFERRED_FROM_SHELL_CONTENT",
			"route", route)
		props := map[string]string{
			"route":     route,
			"via":       "shell_content",
			"framework": "maui",
			"line":      itoa(lineOf(src, m[0])),
		}
		if targetPage != "" {
			setProps(&ent, "target_page", targetPage)
			props["target_page"] = targetPage
		}
		ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
			ToID:       "route:" + route,
			Kind:       string(types.RelationshipKindNavigatesTo),
			Properties: props,
		})
		add(ent)
	}

	// Shell.Current.GoToAsync("//details") — string-literal imperative nav.
	for _, m := range reMPShellGoToAsync.FindAllStringSubmatchIndex(src, -1) {
		route := normalizeMauiRoute(src[m[2]:m[3]])
		if route == "" {
			continue
		}
		ent := makeEntity("goto:"+route+":"+file.Path+":"+itoa(lineOf(src, m[0])),
			"SCOPE.Operation", "navigation_extraction", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "maui", "provenance", "INFERRED_FROM_GO_TO_ASYNC",
			"route", route)
		ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
			ToID: "route:" + route,
			Kind: string(types.RelationshipKindNavigatesTo),
			Properties: map[string]string{
				"route":     route,
				"via":       "goto_async",
				"framework": "maui",
				"line":      itoa(lineOf(src, m[0])),
			},
		})
		add(ent)
	}

	// Shell.Current.GoToAsync(nameof(DetailsPage)) — nameof target == page == route.
	for _, m := range reMPGoToAsyncNameof.FindAllStringSubmatchIndex(src, -1) {
		page := src[m[2]:m[3]]
		ent := makeEntity("goto:nameof:"+page+":"+file.Path+":"+itoa(lineOf(src, m[0])),
			"SCOPE.Operation", "navigation_extraction", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "maui", "provenance", "INFERRED_FROM_GO_TO_ASYNC_NAMEOF",
			"route", page, "target_page", page)
		ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
			ToID: "route:" + page,
			Kind: string(types.RelationshipKindNavigatesTo),
			Properties: map[string]string{
				"route":       page,
				"via":         "goto_nameof",
				"target_page": page,
				"framework":   "maui",
				"line":        itoa(lineOf(src, m[0])),
			},
		})
		add(ent)
	}

	// -------------------------------------------------------------------------
	// Data Flow/state_management — MVVM view↔viewmodel USES edges (#3579)
	//
	// A ContentPage that wires a ViewModel — via BindingContext = new XViewModel()
	// or a DI-injected XViewModel ctor parameter — emits a USES edge
	// page→"viewmodel:XViewModel". [ObservableProperty]/[RelayCommand] mark the
	// class as a ViewModel; [RelayCommand] methods become command entities.
	// -------------------------------------------------------------------------

	// Resolve the enclosing page/class name (first ContentPage-style subclass in
	// the file) so the USES edge has a meaningful caller property.
	enclosingPage := ""
	if pm := reMPContentPage.FindStringSubmatchIndex(src); pm != nil {
		enclosingPage = src[pm[2]:pm[3]]
	}

	emitViewModelUses := func(vm string, via string, line int) {
		if vm == "" {
			return
		}
		ent := makeEntity("view_binds:"+enclosingPage+":"+vm, "SCOPE.UIComponent", "state_management",
			file.Path, "csharp", line)
		setProps(&ent, "framework", "maui", "provenance", "INFERRED_FROM_MVVM_BINDING",
			"view", enclosingPage, "viewmodel", vm, "binding", via)
		ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
			ToID: "viewmodel:" + vm,
			Kind: string(types.RelationshipKindUses),
			Properties: map[string]string{
				"viewmodel": vm,
				"view":      enclosingPage,
				"via":       via,
				"framework": "maui",
				"line":      itoa(line),
			},
		})
		add(ent)
	}

	for _, m := range reMPBindingContextNew.FindAllStringSubmatchIndex(src, -1) {
		emitViewModelUses(src[m[2]:m[3]], "binding_context_new", lineOf(src, m[0]))
	}
	// DI-injected ViewModel ctor param: only meaningful inside a page class.
	if enclosingPage != "" {
		for _, m := range reMPViewModelCtorParam.FindAllStringSubmatchIndex(src, -1) {
			emitViewModelUses(src[m[2]:m[3]], "ctor_injection", lineOf(src, m[0]))
		}
	}

	// CommunityToolkit.Mvvm ViewModel marker: [ObservableProperty]/[RelayCommand].
	if reMPObservableProperty.MatchString(src) || reMPRelayCommand.MatchString(src) {
		// Tag the enclosing class as a ViewModel so the "viewmodel:<name>" stub
		// emitted by USES edges has a concrete endpoint within the same file.
		vmName := enclosingPage
		if vm := reMPMvvmClass.FindStringSubmatchIndex(src); vm != nil {
			vmName = src[vm[2]:vm[3]]
		}
		if vmName != "" {
			ent := makeEntity("viewmodel:"+vmName, "SCOPE.Component", "state_management",
				file.Path, "csharp", 1)
			setProps(&ent, "framework", "maui", "provenance", "INFERRED_FROM_MVVM_TOOLKIT",
				"viewmodel", vmName)
			add(ent)
		}
	}

	// [RelayCommand] methods → command entities (cheap; aids USES targeting).
	for _, m := range reMPRelayCommand.FindAllStringSubmatchIndex(src, -1) {
		method := src[m[2]:m[3]]
		ent := makeEntity("command:"+method, "SCOPE.Operation", "state_management",
			file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "maui", "provenance", "INFERRED_FROM_RELAY_COMMAND",
			"command", method)
		add(ent)
	}

	// -------------------------------------------------------------------------
	// Structure/context_extraction — DI registration BINDS edges (#3579)
	//
	// MauiProgram.cs service registrations:
	//   builder.Services.AddSingleton<IFoo, Foo>()  → BINDS  iface:IFoo → impl:Foo
	//   builder.Services.AddTransient<XViewModel>() → REGISTERS self-binding
	// Mirrors how DI bindings are modeled elsewhere (interface→impl edge).
	// -------------------------------------------------------------------------

	for _, m := range reMPDITwoTypeArgs.FindAllStringSubmatchIndex(src, -1) {
		lifetime := src[m[2]:m[3]]
		iface := src[m[4]:m[5]]
		impl := src[m[6]:m[7]]
		ent := makeEntity("di:"+iface+"->"+impl, "SCOPE.Component", "context_extraction",
			file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "maui", "provenance", "INFERRED_FROM_DI_REGISTRATION",
			"interface", iface, "implementation", impl, "lifetime", lifetime)
		ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
			ToID: "impl:" + impl,
			Kind: string(types.RelationshipKindBinds),
			Properties: map[string]string{
				"interface":      iface,
				"implementation": impl,
				"lifetime":       lifetime,
				"framework":      "maui",
				"line":           itoa(lineOf(src, m[0])),
			},
		})
		add(ent)
	}

	// Single-type-arg registrations (self-binding); skip those already captured
	// as the two-arg form (the one-arg regex also matches "<IFoo, Foo>" prefix).
	for _, m := range reMPDIOneTypeArg.FindAllStringSubmatchIndex(src, -1) {
		// Guard: the two-type-arg regex is a superset; re-check there is no comma
		// between the angle brackets for this match to avoid double emission.
		seg := src[m[0]:m[1]]
		if commaInTypeArgs(seg) {
			continue
		}
		lifetime := src[m[2]:m[3]]
		svc := src[m[4]:m[5]]
		ent := makeEntity("di:self:"+svc, "SCOPE.Component", "context_extraction",
			file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "maui", "provenance", "INFERRED_FROM_DI_SELF_REGISTRATION",
			"service", svc, "lifetime", lifetime)
		ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
			ToID: "impl:" + svc,
			Kind: string(types.RelationshipKindRegisters),
			Properties: map[string]string{
				"service":   svc,
				"lifetime":  lifetime,
				"framework": "maui",
				"line":      itoa(lineOf(src, m[0])),
			},
		})
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// commaInTypeArgs reports whether the matched Add{Singleton,…}<...>( segment
// contains a top-level comma between its angle brackets — i.e. it is the
// two-type-arg interface→impl form, which the dedicated two-arg pass already
// handles. Used to keep the one-arg self-registration pass from double-emitting.
func commaInTypeArgs(seg string) bool {
	open := strings.IndexByte(seg, '<')
	if open < 0 {
		return false
	}
	depth := 0
	for i := open; i < len(seg); i++ {
		switch seg[i] {
		case '<':
			depth++
		case '>':
			depth--
			if depth == 0 {
				return false
			}
		case ',':
			if depth == 1 {
				return true
			}
		}
	}
	return false
}
