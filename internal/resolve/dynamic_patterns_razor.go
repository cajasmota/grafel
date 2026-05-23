package resolve

import "regexp"

// razorDynamicPatterns are per-language patterns for Blazor / ASP.NET Core
// Razor (.razor) files. Registered via init() into dynamicPatternsByLang.
//
// Razor files use the same C# syntax inside @code blocks, so this catalog
// starts with all csharpDynamicPatterns and adds Blazor-specific additions.
// init() order is guaranteed: _csharp.go registers "csharp" first (alphabetic
// file-init order within a package); _razor.go then installs the extended
// catalog under the "razor" key, overriding the placeholder left by csharp.
//
// # Classification context
//
// The razor extractor emits CALLS edges only from event-handler and lifecycle
// method bodies inside @code blocks. Three groups of callee shapes show up as
// unresolved stubs:
//
//  1. Blazor ComponentBase lifecycle methods (OnInitializedAsync, OnParametersSet,
//     OnAfterRender, etc.) — handled upstream by external/synth.go (razorBareNames)
//     which folds them to ext:microsoft; never reach the dynamic-pattern check.
//
//  2. Injected-service async method calls. When a component declares
//     `@inject IWeatherService WeatherSvc`, calls like
//     `await WeatherSvc.GetForecastAsync(...)` are stripped to the bare leaf
//     `GetForecastAsync` by the extractor's reCallHead pattern. Without full
//     .NET DI type-resolution the resolver cannot bind these leaf names to any
//     graph entity — they are interface dispatch at runtime. The naming
//     convention (PascalCase verb + `Async` suffix) is unique enough to the
//     .NET ecosystem that the razor gate is safe.
//
//  3. PascalCase single-segment helper calls in @code bodies
//     (`LoadInitialData`, `FetchData`, `BuildViewModel`) that are private
//     methods defined in the same component — these SHOULD resolve (the
//     resolver finds them in the same file entity set) and must NOT be
//     classified Dynamic. They are intentionally excluded here.
//
// # SQL slice
//
// The sql extractor emits READS_FROM / WRITES_TO edges targeting table names.
// Cross-file table targets that have no matching entity are routed to
// DispositionExternalSQL (issue #507); no dynamic-pattern slice is needed for
// SQL. See PR body for the full audit.
var razorSpecificPatterns = []*regexp.Regexp{
	// Injected-service async calls (issue #44 / Blazor slice).
	//
	// Pattern: PascalCase verb phrase ending in `Async` — the .NET
	// Task-returning async naming convention. The extractor strips the
	// receiver (`WeatherSvc.GetForecastAsync` → `GetForecastAsync`), leaving
	// a bare leaf the static resolver cannot bind without knowing the
	// runtime DI type.
	//
	// Conservative scope:
	//   - Requires uppercase first letter (PascalCase) so generic bare names
	//     like `get`, `fetch`, `load` are excluded.
	//   - Requires the `Async` suffix (minimum 6 chars) so short names like
	//     `DoAsync` are captured but a hypothetical `HasAsync` (which could
	//     legitimately be a user method in the graph) is still long enough to
	//     be a match. The resolver runs BEFORE this gate in the resolution
	//     waterfall, so in-graph methods named FooAsync are still resolved
	//     normally — this gate only fires for stubs that reach the
	//     classifyDispositionLang path (unmatched stubs).
	//   - NOT applied retroactively to "csharp" language (.cs files) — the
	//     razor gate alone prevents cross-ecosystem collisions (Go/Java/Python
	//     async patterns use different conventions). The csharp gate does not
	//     inherit this because .cs files have richer extractor coverage and
	//     their PascalCaseAsync calls are more likely to resolve.
	regexp.MustCompile(`^[A-Z][A-Za-z0-9_]+Async$`),

	// HttpClient / IHttpClientFactory extension methods on injected HTTP
	// clients. The razor extractor emits the bare method leaf after stripping
	// `Http.` or `client.` receivers. These are `Microsoft.Net.Http.Json`
	// extension methods — unresolvable without the exact generic type arg.
	regexp.MustCompile(`^GetFromJsonAsync$`),
	regexp.MustCompile(`^PostAsJsonAsync$`),
	regexp.MustCompile(`^PutAsJsonAsync$`),
	regexp.MustCompile(`^DeleteAsync$`),
	regexp.MustCompile(`^SendAsync$`),
	regexp.MustCompile(`^ReadFromJsonAsync$`),

	// EventCallback invocations — `OnChanged.InvokeAsync(value)` emits
	// `InvokeAsync` (already in razorBareNames → ExternalKnown) but also
	// `HasDelegate` and receiver-stripped event-callback helpers. Guard the
	// two shapes the extractor produces that aren't in razorBareNames.
	regexp.MustCompile(`^OnChanged$`),     // bare EventCallback field invocation
	regexp.MustCompile(`^OnClicked$`),     // common Blazor EventCallback parameter name
	regexp.MustCompile(`^OnSubmit$`),      // form submit EventCallback
	regexp.MustCompile(`^OnValidSubmit$`), // EditForm OnValidSubmit callback
	regexp.MustCompile(`^OnInvalidSubmit$`),

	// JSInterop helpers. `JS.InvokeVoidAsync` / `JS.InvokeAsync<T>` are
	// already in razorBareNames; the generic forms with `<T>` suffix survive
	// after the extractor strips the receiver but retain the generic bracket.
	// Match the generic variant as Dynamic.
	regexp.MustCompile(`^InvokeAsync<`),
	regexp.MustCompile(`^InvokeVoidAsync<`),
}

// razorDynamicPatterns is the full catalog for the "razor" language key:
// C#-shared patterns extended with Blazor-specific additions (issue #44).
var razorDynamicPatterns = append(
	append([]*regexp.Regexp{}, csharpDynamicPatterns...),
	razorSpecificPatterns...,
)

func init() {
	dynamicPatternsByLang["razor"] = razorDynamicPatterns
}
