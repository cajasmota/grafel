package resolve

import "regexp"

// csharpDynamicPatterns is the per-language dynamic-dispatch pattern catalog
// for C# and Razor. Both language tags share this slice.
// Matches here tag a stub as DispositionDynamic.
//
// C# / .NET dynamic-pattern catalog (issue #441). Routes
// project-internal `using Namespace.SubNamespace;` IMPORTS whose
// target dotted namespace has no entity in the graph (because we
// index file-level entities, not namespace entities) into Dynamic
// instead of bug-extractor. Pattern: PascalCase root segment, at
// least one dot, no leading `Microsoft.`/`System.` (those resolve
// via the external allowlist) and no leading lowercase (which would
// be a method-on-receiver shape, handled elsewhere).
//
// Also covers a small set of receiver-stripped reflection /
// concurrency primitives the C# extractor emits as bare or dotted
// callees (`Interlocked.Increment`, `MethodBase.GetCurrentMethod`,
// `PeriodicTimer.WaitForNextTickAsync`, `ConcurrentDictionary.
// TryRemove`). These are not language-builtins in the strict sense
// but they're framework-dispatch entry points that no static binder
// can reach without full assembly-level type resolution.
var csharpDynamicPatterns = []*regexp.Regexp{
	// PascalCase project-internal namespace import.
	// Anchored: starts with uppercase, contains at least one dot, every
	// segment is an identifier (no whitespace / brackets), and there is
	// no generic `<...>` suffix (those are call sites, not imports).
	// Negative-lookahead would be cleaner but Go regexp's RE2 dialect
	// doesn't support lookaround — we filter out the well-known .NET
	// / Microsoft / EF Core ecosystem roots at the caller (handled in
	// isDynamicPatternLang via a startsWith check before regex eval).
	regexp.MustCompile(`^[A-Z][A-Za-z0-9_]*(?:\.[A-Z][A-Za-z0-9_]*)+$`),

	// Issue #44 — Quartz.NET / Hangfire generic static-factory calls.
	// The C# extractor emits `JobBuilder.Create<ReportJob>` and
	// `BackgroundJob.Enqueue<IEmailService>` as bare CALLS stubs when the
	// receiver type carries a generic type argument. The existing PascalCase-
	// dotted pattern above only matches stubs without a `<` suffix; these
	// generic forms fall through to BugExtractor. Pattern: PascalCase
	// dotted identifier immediately followed by `<` (the opening of the
	// type-argument list). C#-language gate keeps this from firing on
	// non-.NET codebases (Go generics, TypeScript generics, etc.).
	regexp.MustCompile(`^[A-Z][A-Za-z0-9_]*\.[A-Z][A-Za-z0-9_]*<`),

	// Issue #44 — Quartz.NET fluent builder bare-name leaf methods.
	// The Quartz.NET JobBuilder / TriggerBuilder API uses a fluent chain:
	//   JobBuilder.Create<T>().WithIdentity("name").Build()
	//   TriggerBuilder.Create().WithIdentity("t").StartNow().Build()
	// After the extractor strips the receiver the bare method leaves
	// `WithIdentity` and `StartNow` land in BugExtractor because they
	// are not in any allowlist. Without full type inference the resolver
	// cannot bind these to the Quartz.NET builder — Dynamic is the
	// correct disposition. C#-language gate (#94 safer-bias rule) keeps
	// these from polluting non-.NET graphs.
	regexp.MustCompile(`^WithIdentity$`), // JobBuilder/TriggerBuilder.WithIdentity(...)
	regexp.MustCompile(`^StartNow$`),     // TriggerBuilder.StartNow()
}

// csharpExternalNamespaceRoots lists the dotted namespace roots that
// must NOT be classified as Dynamic by csharpDynamicPatterns — they
// are real external imports (Microsoft.AspNetCore, System.Linq, EF
// Core, etc.) that the external synthesiser routes to ext:microsoft
// / ext:system. Without this exclusion the dynamic-pattern check
// (which runs BEFORE the external-prefix check, Refs #95) would
// promote every Microsoft.* import to Dynamic and the corpus would
// lose its ExternalKnown classification.
var csharpExternalNamespaceRoots = []string{
	"Microsoft.",
	"System.",
	"EntityFrameworkCore",
	"Newtonsoft.",
	"Serilog",
	"NLog",
	"Autofac",
	"Castle.",
	"AutoMapper",
	"MediatR",
	"FluentValidation",
	"FluentAssertions",
	"NUnit",
	"Xunit",
	"Moq",
	"Polly",
	"Dapper",
	"RestSharp",
	"Hangfire",
	"Quartz",
	"IdentityServer",
	"MassTransit",
	"NServiceBus",
	"RabbitMQ.",
	"StackExchange.",
	"Swashbuckle.",
	"GraphQL.",
	"HotChocolate",
	"AspNetCore",
	"Org.BouncyCastle",
	"Mvc.",
}

func init() {
	dynamicPatternsByLang["csharp"] = csharpDynamicPatterns
	dynamicPatternsByLang["razor"] = csharpDynamicPatterns
}
