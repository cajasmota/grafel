// http_endpoint_aspnet_core.go — ASP.NET Core attribute-routing → http_endpoint_definition synthesis.
//
// ASP.NET Core uses class-level `[Route("/api/[controller]")]` (and the
// `[ApiController]` marker) on a `*Controller` class together with
// per-action verb attributes `[HttpGet("...")]` / `[HttpPost("...")]` /
// `[HttpPut/Patch/Delete/Head/Options("...")]`. The action method may
// appear on the line immediately after the verb attribute (or after a
// stack of intermediate attributes such as `[ProducesResponseType]`,
// `[Authorize]`, etc.). All artefacts live in the same `.cs` file, so
// this synthesizer is single-file and same-file is the dominant case.
//
// Token substitution:
//
//	[Route("/api/[controller]")]  on  class WidgetsController
//	      → "/api/widgets"
//
// The `[controller]` token expands to the controller class name with the
// trailing "Controller" suffix stripped and lowercased — matching the
// canonical ASP.NET Core convention. `[action]` is similarly supported
// (expanded to the lowercased method name).
//
// Handler attribution is wired through ResolveHTTPEndpointHandlers via a
// `source_handler=SCOPE.Operation:<Class>.<Method>` property. The C#
// extractor names methods inside a class as `<Class>.<Method>` (see
// internal/extractors/csharp/csharp.go buildOperation), so the resolver
// can rebind source_file / start_line to the action method without the
// cross-file globalIdx fallback needing to fire. For controllers split
// from their routing site the same rebind still applies via globalIdx.
//
// Refs #2692.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// aspnetVerbAttrRe captures a method-level HTTP verb attribute and the
// following method name. The route argument is optional (e.g. `[HttpGet]`
// on its own pairs with the class-level `[Route]` for `GET /api/widgets`).
//
// We allow intervening attributes (`[ProducesResponseType(...)]`,
// `[Authorize]`, etc.) and access / modifier keywords between the verb
// attribute and the method declaration. The method-return-type chunk
// `[\w<>\[\],.\s?]+?` matches `IActionResult`, `Task<ActionResult<Foo>>`,
// `ValueTask<IEnumerable<Bar>>`, etc.
//
// Capture groups:
//
//	1 = HTTP verb (Get / Post / Put / Patch / Delete / Head / Options)
//	2 = optional route argument (the string literal contents)
//	3 = method name
var aspnetVerbAttrRe = regexp.MustCompile(
	`\[\s*Http(Get|Post|Put|Patch|Delete|Head|Options)\s*` +
		`(?:\(\s*(?:"([^"\r\n]*)")?[^)]*\))?\s*\]` +
		`\s*(?:[\r\n]+(?:\s*\[[^\]\r\n]+\]\s*[\r\n]+)*)?\s*` +
		`(?:public|protected|private|internal|static|virtual|override|sealed|async|\s)+` +
		`[\w<>\[\],.\s?]+?\s+([A-Za-z_]\w*)\s*\(`,
)

// aspnetClassRouteRe captures the class-level `[Route("...")]` value and
// the controller class name (allowing intervening attributes between the
// `[Route]` line and the `class` declaration).
//
// Capture groups:
//
//	1 = route argument
//	2 = class name
var aspnetClassRouteRe = regexp.MustCompile(
	`\[\s*Route\s*\(\s*"([^"\r\n]*)"\s*\)\s*\]` +
		`\s*[\r\n]+(?:\s*\[[^\]\r\n]+\]\s*[\r\n]+)*` +
		`\s*(?:public|internal|sealed|abstract|partial|static|\s)*` +
		`class\s+([A-Za-z_]\w*)`,
)

// aspnetControllerClassRe matches every Controller class declaration in
// the file (with or without a class-level `[Route]`). Used to recover the
// class name when the only route prefix lives implicitly in `[Route]`.
//
// Capture group 1 = class name.
var aspnetControllerClassRe = regexp.MustCompile(
	`(?m)^\s*(?:public|internal|sealed|abstract|partial|static|\s)*` +
		`class\s+([A-Za-z_]\w*Controller)\b`,
)

// aspnetHasAttributeRouting returns true when the file shows any sign of
// ASP.NET Core attribute routing. Used as a fast pre-filter so the regex
// machinery doesn't run on every C# file in the index.
func aspnetHasAttributeRouting(content string) bool {
	if !strings.Contains(content, "[Http") && !strings.Contains(content, "[Route") {
		return false
	}
	// Cheap shape check: at least one HTTP verb attribute is present.
	return strings.Contains(content, "[HttpGet") ||
		strings.Contains(content, "[HttpPost") ||
		strings.Contains(content, "[HttpPut") ||
		strings.Contains(content, "[HttpPatch") ||
		strings.Contains(content, "[HttpDelete") ||
		strings.Contains(content, "[HttpHead") ||
		strings.Contains(content, "[HttpOptions")
}

// aspnetControllerToken converts a controller class name to the lower-case
// token that `[controller]` substitutes to. Strips the trailing "Controller"
// suffix per the canonical ASP.NET Core convention.
//
//	WidgetsController -> "widgets"
//	UsersController   -> "users"
//	HomeController    -> "home"
//	Misc              -> "misc"  (no suffix to strip)
func aspnetControllerToken(class string) string {
	name := strings.TrimSuffix(class, "Controller")
	return strings.ToLower(name)
}

// aspnetSubstituteTokens expands `[controller]` and `[action]` tokens in a
// route template using the controller class name and method name.
func aspnetSubstituteTokens(template, controllerClass, method string) string {
	out := template
	if controllerClass != "" {
		token := aspnetControllerToken(controllerClass)
		out = strings.ReplaceAll(out, "[controller]", token)
	}
	if method != "" {
		out = strings.ReplaceAll(out, "[action]", strings.ToLower(method))
	}
	return out
}

// synthesizeASPNetCore scans a C# source file for ASP.NET Core attribute
// routing patterns and calls emit for each (verb, canonical-path,
// framework, handlerKind, handlerName) tuple discovered.
//
// The class-level `[Route(...)]` prefix is applied to every per-method
// verb attribute. When a method-level attribute provides an absolute path
// (one starting with `/`), it replaces the class prefix entirely, matching
// the ASP.NET Core routing precedence rules.
func synthesizeASPNetCore(content string, emit emitFn) {
	if !aspnetHasAttributeRouting(content) {
		return
	}

	// Resolve the (single) controller class name + class-level prefix for
	// this file. ASP.NET Core overwhelmingly uses one controller per file;
	// when the file declares multiple controllers we still pick the first
	// `[Route(...)]`-anchored class as the prefix anchor and attribute all
	// actions to it (acceptable for the same reasons NestJS does).
	classPrefix := ""
	className := ""
	if m := aspnetClassRouteRe.FindStringSubmatch(content); len(m) >= 3 {
		classPrefix = m[1]
		className = m[2]
	}
	// Fallback: no class-level [Route], but a *Controller class exists —
	// the empty prefix is fine, but we still want className for
	// `[controller]` token expansion in method-level attributes.
	if className == "" {
		if m := aspnetControllerClassRe.FindStringSubmatch(content); len(m) >= 2 {
			className = m[1]
		}
	}

	for _, m := range aspnetVerbAttrRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 4 {
			continue
		}
		verb := strings.ToUpper(m[1])
		methodPath := m[2]
		methodName := m[3]

		var raw string
		switch {
		case methodPath == "":
			raw = classPrefix
		case strings.HasPrefix(methodPath, "/"):
			// Absolute method-level path overrides the class prefix.
			raw = methodPath
		default:
			raw = joinPathFragments(classPrefix, methodPath)
		}
		raw = aspnetSubstituteTokens(raw, className, methodName)

		canonical := httproutes.Canonicalize(httproutes.FrameworkASPNetCore, raw)
		if canonical == "" {
			continue
		}
		// SCOPE.Operation:<Class>.<Method> matches the C# extractor's
		// convention (buildOperation in internal/extractors/csharp/csharp.go).
		// The resolver rebinds source_file / start_line to the method
		// declaration via the #2680 mechanism.
		refName := methodName
		if className != "" {
			refName = className + "." + methodName
		}
		emit(verb, canonical, "aspnet_core", "SCOPE.Operation", refName)
	}
}
