// Package csharp — deep ASP.NET Core routing extractor.
//
// Covers three routing styles for ASP.NET Core and ASP.NET MVC:
//
//  1. Attribute routing — [HttpGet("/products/{id}")], controller-level
//     [Route("api/[controller]")] + action [HttpGet("{id}")] with full
//     [controller] / [action] token substitution and multi-controller-per-file.
//
//  2. Conventional routing — app.MapControllerRoute / MapDefaultControllerRoute /
//     MapAreaControllerRoute / MapRoute template capture.
//
//  3. Minimal APIs — app.MapGet / MapPost / MapPut / MapDelete / MapPatch /
//     MapMethods, including route groups via app.MapGroup("/prefix").
//
// Each detected route is emitted as SCOPE.Operation with subtype "endpoint"
// so the Routing group cells (endpoint_synthesis, handler_attribution,
// route_extraction) light up.
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
	extractor.Register("custom_csharp_aspnet_core", &aspnetCoreExtractor{})
}

type aspnetCoreExtractor struct{}

func (e *aspnetCoreExtractor) Language() string { return "custom_csharp_aspnet_core" }

// ---------------------------------------------------------------------------
// Compiled regexes
// ---------------------------------------------------------------------------

var (
	// csaspnetClassBlock matches the opening of a controller class and captures:
	//   group 1 = class-level [Route(...)] template (may be empty if no [Route])
	//   group 2 = class name
	//
	// Strategy: scan for [Route("...")] ... class Foo.  We allow an arbitrary
	// stack of other attributes and access modifiers between the [Route] line
	// and the class keyword (same as the engine synthesizer).
	csaspnetClassRouteRe = regexp.MustCompile(
		`\[\s*Route\s*\(\s*"([^"\r\n]*)"\s*\)\s*\]` +
			`(?:\s*[\r\n]+(?:\s*\[[^\]\r\n]+\]\s*[\r\n]+)*)?\s*` +
			`(?:public|internal|sealed|abstract|partial|static|\s)*` +
			`class\s+([A-Za-z_]\w*)`,
	)

	// csaspnetControllerClassRe finds any *Controller class (with or without
	// a [Route] prefix) so we can pick up the class name for token expansion.
	csaspnetControllerClassRe = regexp.MustCompile(
		`(?m)^\s*(?:public|internal|sealed|abstract|partial|static|\s)*` +
			`class\s+([A-Za-z_]\w*Controller)\b`,
	)

	// csaspnetVerbAttrRe captures a method-level HTTP verb attribute + the
	// immediately following method name (same as the engine version, kept in
	// sync).  Groups: 1=verb, 2=optional route arg, 3=method name.
	csaspnetVerbAttrRe = regexp.MustCompile(
		`\[\s*Http(Get|Post|Put|Patch|Delete|Head|Options)\s*` +
			`(?:\(\s*(?:"([^"\r\n]*)")?[^)]*\))?\s*\]` +
			`\s*(?:[\r\n]+(?:\s*\[[^\]\r\n]+\]\s*[\r\n]+)*)?\s*` +
			`(?:public|protected|private|internal|static|virtual|override|sealed|async|\s)+` +
			`[\w<>\[\],.\s?]+?\s+([A-Za-z_]\w*)\s*\(`,
	)

	// csaspnetMinimalAPIRe matches app.MapGet/MapPost/MapPut/MapDelete/MapPatch/MapMethods.
	// Group 1 = verb (or empty for MapMethods), group 2 = path.
	csaspnetMinimalAPIRe = regexp.MustCompile(
		`(?m)(?:\w+)\.Map(Get|Post|Put|Delete|Patch|Head|Options)\s*\(\s*"([^"]+)"` +
			`|(?:\w+)\.MapMethods\s*\(\s*"([^"]+)"`,
	)

	// csaspnetMapMethodsVerbsRe extracts the HTTP methods array from MapMethods(path, new[]{"GET","POST"}).
	csaspnetMapMethodsVerbsRe = regexp.MustCompile(
		`MapMethods\s*\(\s*"[^"]+"\s*,\s*new\s*(?:\[\])?\s*\{([^}]+)\}`,
	)

	// csaspnetRouteGroupRe matches var group = app.MapGroup("/prefix") or
	// endpoints.MapGroup("/prefix").  Group 1 = variable name, group 2 = path prefix.
	csaspnetRouteGroupRe = regexp.MustCompile(
		`(?m)(?:var\s+(\w+)\s*=\s*)?(?:\w+)\.MapGroup\s*\(\s*"([^"]+)"`,
	)

	// csaspnetGroupEndpointRe matches groupVar.MapGet/... where groupVar is a
	// known route-group variable.  Group 1 = variable name, group 2 = verb,
	// group 3 = sub-path.
	csaspnetGroupEndpointRe = regexp.MustCompile(
		`(?m)(\w+)\.Map(Get|Post|Put|Delete|Patch|Head|Options)\s*\(\s*"([^"]+)"`,
	)

	// csaspnetConvRoutesRe captures MapControllerRoute / MapDefaultControllerRoute /
	// MapAreaControllerRoute / MapRoute template strings.
	//
	// Handles two call styles:
	//   1. Named arg:     app.MapControllerRoute(name: "default", pattern: "{controller}/{action}")
	//   2. Positional:    app.MapControllerRoute("default", "{controller}/{action}")
	//
	// Groups: 1 = pattern from named-arg form, 2 = pattern from positional form.
	csaspnetConvRoutesRe = regexp.MustCompile(
		`(?i)(?:\w+)\.Map(?:Controller|Default(?:Controller)?|Area(?:Controller)?)?Route\s*\([\s\S]{0,200}?` +
			`pattern\s*:\s*"([^"]+)"` +
			`|(?i)(?:\w+)\.Map(?:Controller|Default(?:Controller)?|Area(?:Controller)?)?Route\s*\(` +
			`\s*"[^"]*"\s*,\s*"([^"]+)"`,
	)

	// csaspnetDIRegister is unchanged — services.AddXxx<T>()
	reAspNetDIRegister = regexp.MustCompile(
		`services\.Add(Scoped|Transient|Singleton|HostedService)\s*(?:<([^>]+)>)?`,
	)

	// csaspnetMiddleware is unchanged.
	reAspNetMiddleware = regexp.MustCompile(
		`app\.Use(?:Middleware<([^>]+)>|When\s*\(|)\s*\(`,
	)

	reAspNetSignalRHub = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+:\s+Hub\b`,
	)
	reAspNetMapHub = regexp.MustCompile(
		`app\.MapHub\s*<\s*(\w+)\s*>`,
	)
	reAspNetGRPC = regexp.MustCompile(
		`app\.MapGrpcService\s*<\s*(\w+)\s*>`,
	)
	reAspNetHealthCheck = regexp.MustCompile(
		`app\.MapHealthChecks\s*\(\s*"([^"]+)"`,
	)
	reAspNetBackground = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+:\s+(?:IHostedService|BackgroundService)\b`,
	)
	reAspNetController = regexp.MustCompile(
		`(?m)class\s+(\w+(?:Controller))\s+:\s+(?:Controller|ControllerBase|ApiController)\b`,
	)
)

// ---------------------------------------------------------------------------
// Token helpers
// ---------------------------------------------------------------------------

// csaspnetControllerToken strips "Controller" suffix and lowercases — the
// canonical expansion of the [controller] route token.
func csaspnetControllerToken(className string) string {
	name := strings.TrimSuffix(className, "Controller")
	return strings.ToLower(name)
}

// csaspnetRouteConstraintRe strips ASP.NET Core route constraints from
// path parameters.  Examples stripped:
//
//	{id:int}     → {id}
//	{name:alpha} → {name}
//	{id?}        → {id}        (optional marker)
//	{slug:regex(…)} → {slug}
var csaspnetRouteConstraintRe = regexp.MustCompile(`\{(\w+)[?:][^}]*\}`)

// csaspnetStripConstraints removes type constraints and optional markers
// from ASP.NET Core route parameter syntax.
func csaspnetStripConstraints(path string) string {
	return csaspnetRouteConstraintRe.ReplaceAllString(path, "{$1}")
}

// csaspnetSubstituteTokens expands [controller] and [action] in a route template.
func csaspnetSubstituteTokens(template, controllerClass, method string) string {
	out := template
	if controllerClass != "" {
		out = strings.ReplaceAll(out, "[controller]", csaspnetControllerToken(controllerClass))
	}
	if method != "" {
		out = strings.ReplaceAll(out, "[action]", strings.ToLower(method))
	}
	return out
}

// csaspnetJoinPath joins a prefix and a suffix, normalising separators.
// An empty prefix yields the suffix unchanged.  A suffix that starts with
// "/" is returned as-is (absolute override, matching ASP.NET Core semantics).
func csaspnetJoinPath(prefix, suffix string) string {
	if suffix == "" {
		return prefix
	}
	if strings.HasPrefix(suffix, "/") {
		return suffix // absolute override
	}
	if prefix == "" {
		return suffix
	}
	p := strings.TrimRight(prefix, "/")
	s := strings.TrimLeft(suffix, "/")
	return p + "/" + s
}

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *aspnetCoreExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.aspnet_core_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "aspnet_core"),
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
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// -------------------------------------------------------------------------
	// 1. Attribute routing — multi-controller aware
	// -------------------------------------------------------------------------
	// Build a map of class name → class-level [Route] prefix by scanning all
	// [Route("...")] ... class Foo occurrences.
	classPrefixMap := make(map[string]string)
	for _, m := range csaspnetClassRouteRe.FindAllStringSubmatch(src, -1) {
		if len(m) >= 3 {
			classPrefixMap[m[2]] = m[1]
		}
	}
	// Also register any *Controller class that has no [Route] with empty prefix.
	for _, m := range csaspnetControllerClassRe.FindAllStringSubmatch(src, -1) {
		if len(m) >= 2 {
			if _, exists := classPrefixMap[m[1]]; !exists {
				classPrefixMap[m[1]] = ""
			}
		}
	}

	// Determine the "current" controller class at each verb-attribute match
	// position by scanning the text in order.  We keep a running variable
	// updated whenever we encounter a class declaration.
	//
	// Implementation: collect class-declaration positions from the same regexes,
	// then for each verb match pick the last class that starts before that match.

	type classAnchor struct {
		pos       int
		className string
		prefix    string
	}
	var anchors []classAnchor

	// Class with [Route]
	for _, loc := range csaspnetClassRouteRe.FindAllStringSubmatchIndex(src, -1) {
		if len(loc) >= 6 && loc[4] >= 0 {
			className := src[loc[4]:loc[5]]
			prefix := ""
			if loc[2] >= 0 {
				prefix = src[loc[2]:loc[3]]
			}
			anchors = append(anchors, classAnchor{pos: loc[0], className: className, prefix: prefix})
		}
	}
	// Controller class without [Route]
	for _, loc := range csaspnetControllerClassRe.FindAllStringSubmatchIndex(src, -1) {
		if len(loc) >= 4 {
			className := src[loc[2]:loc[3]]
			if _, hasRoute := classPrefixMap[className]; !hasRoute {
				anchors = append(anchors, classAnchor{pos: loc[0], className: className, prefix: ""})
			}
		}
	}
	// Sort anchors by position (simple insertion-sort — typically very few controllers per file).
	for i := 1; i < len(anchors); i++ {
		for j := i; j > 0 && anchors[j].pos < anchors[j-1].pos; j-- {
			anchors[j], anchors[j-1] = anchors[j-1], anchors[j]
		}
	}

	// Helper: find the controller anchor active at position pos.
	activeAnchor := func(pos int) classAnchor {
		result := classAnchor{}
		for _, a := range anchors {
			if a.pos <= pos {
				result = a
			}
		}
		return result
	}

	// Walk all verb-attribute matches and resolve the full path.
	for _, loc := range csaspnetVerbAttrRe.FindAllStringSubmatchIndex(src, -1) {
		if len(loc) < 8 {
			continue
		}
		verb := strings.ToUpper(src[loc[2]:loc[3]])
		methodPath := ""
		if loc[4] >= 0 {
			methodPath = src[loc[4]:loc[5]]
		}
		methodName := src[loc[6]:loc[7]]

		anchor := activeAnchor(loc[0])
		classPrefix := anchor.prefix
		className := anchor.className

		var raw string
		switch {
		case methodPath == "":
			raw = classPrefix
		case strings.HasPrefix(methodPath, "/"):
			raw = methodPath
		default:
			raw = csaspnetJoinPath(classPrefix, methodPath)
		}
		raw = csaspnetSubstituteTokens(raw, className, methodName)
		raw = csaspnetStripConstraints(raw)
		if raw == "" {
			raw = "/"
		}

		name := verb + " " + raw
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, loc[0]))
		handler := methodName
		if className != "" {
			handler = className + "." + methodName
		}
		setProps(&ent, "framework", "aspnet_core", "provenance", "INFERRED_FROM_ASPNET_ROUTE",
			"http_method", verb, "route_path", raw, "handler", handler)
		add(ent)
	}

	// -------------------------------------------------------------------------
	// 2. Minimal API — simple app.MapXxx (NOT inside a route group)
	// -------------------------------------------------------------------------
	// We need to know which variable names are route-group vars so we can skip
	// them in the simple pass and handle them in the group pass below.
	groupVars := make(map[string]string) // varName → prefix
	for _, m := range csaspnetRouteGroupRe.FindAllStringSubmatch(src, -1) {
		// m[1] = variable name (may be empty for chained calls), m[2] = prefix
		if len(m) >= 3 && m[1] != "" {
			groupVars[m[1]] = m[2]
			// Emit the group prefix as a Route entity for documentation.
			ent := makeEntity("mapgroup:"+m[2], "SCOPE.Pattern", "route_extraction", file.Path, file.Language, 0)
			setProps(&ent, "framework", "aspnet_core", "provenance", "INFERRED_FROM_ASPNET_MAP_GROUP",
				"route_prefix", m[2])
			add(ent)
		}
	}

	// Simple minimal API: must NOT be called on a known group var.
	for _, loc := range csaspnetGroupEndpointRe.FindAllStringSubmatchIndex(src, -1) {
		if len(loc) < 8 {
			continue
		}
		receiverVar := src[loc[2]:loc[3]]
		verb := strings.ToUpper(src[loc[4]:loc[5]])
		path := src[loc[6]:loc[7]]

		if prefix, isGroup := groupVars[receiverVar]; isGroup {
			// Route-group endpoint — always compose prefix + sub-path.
			// Unlike method-level absolute overrides in attribute routing, a
			// MapGroup sub-path is ALWAYS relative to the group prefix.
			p := strings.TrimRight(prefix, "/")
			s := strings.TrimLeft(path, "/")
			fullPath := p + "/" + s
			name := verb + " " + fullPath
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, loc[0]))
			setProps(&ent, "framework", "aspnet_core", "provenance", "INFERRED_FROM_ASPNET_MAP_GROUP_ENDPOINT",
				"http_method", verb, "route_path", fullPath, "route_group_prefix", prefix)
			add(ent)
		} else if receiverVar == "app" || strings.HasSuffix(receiverVar, "app") || receiverVar == "endpoints" || receiverVar == "routes" || receiverVar == "group" {
			// Standard minimal API call.
			name := verb + " " + path
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, loc[0]))
			setProps(&ent, "framework", "aspnet_core", "provenance", "INFERRED_FROM_ASPNET_MINIMAL_API",
				"http_method", verb, "route_path", path)
			add(ent)
		}
	}

	// Also pick up MapMethods(...) — arbitrary HTTP methods.
	for _, loc := range csaspnetMinimalAPIRe.FindAllStringSubmatchIndex(src, -1) {
		// The regex has two alternatives. First alt: groups 2=verb, 4=path.
		// Second alt (MapMethods): groups 6=path (verb extracted separately).
		if len(loc) < 8 {
			continue
		}
		if loc[2] >= 0 {
			// Already handled by csaspnetGroupEndpointRe above for MapGet etc.
			// This catches the standalone app.Map* pattern for any receiver var
			// that wasn't caught already.
			continue
		}
		if loc[6] >= 0 {
			// MapMethods path
			path := src[loc[6]:loc[7]]
			// Extract verbs from the second argument.
			// Find the full MapMethods(...) call starting at loc[0].
			tail := src[loc[0]:]
			verbs := []string{"ANY"}
			if vm := csaspnetMapMethodsVerbsRe.FindStringSubmatch(tail); vm != nil {
				verbs = nil
				for _, v := range strings.Split(vm[1], ",") {
					v = strings.Trim(v, ` "')`)
					if v != "" {
						verbs = append(verbs, strings.ToUpper(v))
					}
				}
			}
			for _, verb := range verbs {
				name := verb + " " + path
				ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, loc[0]))
				setProps(&ent, "framework", "aspnet_core", "provenance", "INFERRED_FROM_ASPNET_MAP_METHODS",
					"http_method", verb, "route_path", path)
				add(ent)
			}
		}
	}

	// -------------------------------------------------------------------------
	// 3. Conventional routing — MapControllerRoute / MapDefaultControllerRoute
	// -------------------------------------------------------------------------
	for _, m := range csaspnetConvRoutesRe.FindAllStringSubmatch(src, -1) {
		tmpl := ""
		if len(m) >= 2 && m[1] != "" {
			tmpl = m[1]
		} else if len(m) >= 3 && m[2] != "" {
			tmpl = m[2]
		}
		if tmpl == "" {
			continue
		}
		ent := makeEntity("conventional:"+tmpl, "SCOPE.Pattern", "route_extraction", file.Path, file.Language, 0)
		setProps(&ent, "framework", "aspnet_core", "provenance", "INFERRED_FROM_ASPNET_CONVENTIONAL_ROUTE",
			"route_template", tmpl)
		add(ent)
	}

	// -------------------------------------------------------------------------
	// 4. Controller declarations -> SCOPE.Component
	// -------------------------------------------------------------------------
	for _, m := range reAspNetController.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "aspnet_core", "provenance", "INFERRED_FROM_ASPNET_CONTROLLER")
		add(ent)
	}

	// -------------------------------------------------------------------------
	// 5. DI registrations -> SCOPE.Pattern
	// -------------------------------------------------------------------------
	for _, m := range reAspNetDIRegister.FindAllStringSubmatchIndex(src, -1) {
		lifetime := src[m[2]:m[3]]
		serviceType := ""
		if m[4] >= 0 {
			serviceType = src[m[4]:m[5]]
		}
		name := "di:" + lifetime
		if serviceType != "" {
			name += ":" + serviceType
		}
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "aspnet_core", "provenance", "INFERRED_FROM_ASPNET_DI",
			"lifetime", lifetime, "service_type", serviceType)
		add(ent)
	}

	// -------------------------------------------------------------------------
	// 6. Middleware -> SCOPE.Pattern
	// -------------------------------------------------------------------------
	for _, m := range reAspNetMiddleware.FindAllStringSubmatchIndex(src, -1) {
		mwType := ""
		if m[2] >= 0 {
			mwType = src[m[2]:m[3]]
		}
		name := "middleware:" + mwType
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "aspnet_core", "provenance", "INFERRED_FROM_ASPNET_MIDDLEWARE",
			"middleware_type", mwType)
		add(ent)
	}

	// -------------------------------------------------------------------------
	// 7. SignalR Hub -> SCOPE.Operation/endpoint
	// -------------------------------------------------------------------------
	for _, m := range reAspNetSignalRHub.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "aspnet_core", "provenance", "INFERRED_FROM_ASPNET_SIGNALR",
			"hub_class", name)
		add(ent)
	}
	for _, m := range reAspNetMapHub.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity("hub:"+name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "aspnet_core", "provenance", "INFERRED_FROM_ASPNET_SIGNALR",
			"hub_class", name)
		add(ent)
	}

	// -------------------------------------------------------------------------
	// 8. gRPC services -> SCOPE.Service
	// -------------------------------------------------------------------------
	for _, m := range reAspNetGRPC.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "aspnet_core", "provenance", "INFERRED_FROM_ASPNET_GRPC")
		add(ent)
	}

	// -------------------------------------------------------------------------
	// 9. Health check endpoints -> SCOPE.Operation/endpoint
	// -------------------------------------------------------------------------
	for _, m := range reAspNetHealthCheck.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		ent := makeEntity("GET "+path, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "aspnet_core", "provenance", "INFERRED_FROM_ASPNET_HEALTH_CHECK",
			"route_path", path)
		add(ent)
	}

	// -------------------------------------------------------------------------
	// 10. Background services -> SCOPE.Service
	// -------------------------------------------------------------------------
	for _, m := range reAspNetBackground.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "aspnet_core", "provenance", "INFERRED_FROM_ASPNET_BACKGROUND_SERVICE")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
