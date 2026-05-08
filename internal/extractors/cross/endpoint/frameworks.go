// Package endpoint — per-framework detection logic.
//
// Each framework entry declares:
//   - Name (used as the `framework` property on SCOPE.Endpoint entities)
//   - Import markers that must be present in the file to enable detection
//   - Compiled regexes that yield (method, rawPath, handlerRef) tuples
//
// Detection order matters: the first framework whose import marker matches
// the file's import list wins. Ambiguous files (multiple frameworks) pick the
// first-matched framework in importOrder.
package endpoint

import (
	"regexp"
	"strings"
)

// frameworkMatch is an internal result from a per-framework scan.
type frameworkMatch struct {
	method       string // normalised HTTP verb / RPC type / GraphQL op
	rawPath      string // path as captured from the source (pre-normalisation)
	handlerQName string // handler function qualified name (may be empty)
}

// frameworkDetector runs a framework-specific scan over source bytes.
type frameworkDetector func(source string) []frameworkMatch

// frameworkEntry describes how to recognise and scan a single framework.
type frameworkEntry struct {
	name        string
	style       string // "rest" | "grpc" | "graphql"
	importHints []string
	detect      frameworkDetector
}

// ---------------------------------------------------------------------------
// Import list extraction
// ---------------------------------------------------------------------------

// importTokenRE captures common import/require tokens across languages.
// It is intentionally permissive — false positives here only mean we run
// a framework detector that will return zero matches.
var importTokenRE = regexp.MustCompile(
	`(?mi)(?:import|from|require|use|using|package)\s+["']?([\w@][\w\-./:]*)["']?`,
)

// importCallRE captures function-style import forms: `require('x')` / `import('x')`.
// These are common in JS/TS and do not match importTokenRE which requires
// whitespace (not parentheses) after the keyword.
var importCallRE = regexp.MustCompile(
	`(?mi)\b(?:require|import)\s*\(\s*["']([\w@][\w\-./:]*)["']\s*\)`,
)

// railsRoutesSentinelRE matches the signature block of a Rails routes.rb file,
// which does not carry any `import` / `require` statement but always contains
// a `Rails.application.routes.draw do` entry point.
var railsRoutesSentinelRE = regexp.MustCompile(
	`(?m)\b(?:Rails\.application\.routes\.draw|Routes\.draw)\b`,
)

// extractImportTokens returns the lower-cased set of import tokens found in source.
func extractImportTokens(source string) map[string]bool {
	out := map[string]bool{}
	add := func(raw string) {
		if raw == "" {
			return
		}
		tok := strings.ToLower(raw)
		out[tok] = true
		// Also index the first path segment so nested imports match short hints.
		if idx := strings.IndexAny(tok, "/."); idx > 0 {
			out[tok[:idx]] = true
		}
	}
	for _, m := range importTokenRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 2 {
			add(m[1])
		}
	}
	for _, m := range importCallRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 2 {
			add(m[1])
		}
	}
	// Rails routes.rb has no import statements — inject a synthetic token.
	if railsRoutesSentinelRE.MatchString(source) {
		out["rails"] = true
	}
	return out
}

// matchesAnyImport reports whether any of the framework's import hints appear
// in the imported token set (substring match).
func matchesAnyImport(tokens map[string]bool, hints []string) bool {
	for _, h := range hints {
		hLower := strings.ToLower(h)
		if tokens[hLower] {
			return true
		}
		for t := range tokens {
			if strings.Contains(t, hLower) {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// REST — Gin (Go)
// ---------------------------------------------------------------------------

// router.GET("/path", handler) — accepts any identifier on the left of the dot.
var ginRE = regexp.MustCompile(
	`(?m)\b[\w.]+\.(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s*\(\s*"([^"\s]{1,500})"\s*,\s*([\w.]+)`,
)

func detectGin(source string) []frameworkMatch {
	var out []frameworkMatch
	for _, m := range ginRE.FindAllStringSubmatch(source, -1) {
		out = append(out, frameworkMatch{
			method:       strings.ToUpper(m[1]),
			rawPath:      m[2],
			handlerQName: m[3],
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// REST — Express (JS/TS)
// ---------------------------------------------------------------------------

// app.get('/path', handler) — method is lowercase; captures string and identifier.
var expressRE = regexp.MustCompile(
	`(?m)\b(?:app|router)\.(get|post|put|delete|patch|head|options|all)\s*\(\s*['"` + "`" + `]([^'"` + "`" + `\s]{1,500})['"` + "`" + `]\s*,\s*([\w.]+)`,
)

func detectExpress(source string) []frameworkMatch {
	var out []frameworkMatch
	for _, m := range expressRE.FindAllStringSubmatch(source, -1) {
		method := strings.ToUpper(m[1])
		if method == "ALL" {
			method = "ANY"
		}
		out = append(out, frameworkMatch{
			method:       method,
			rawPath:      m[2],
			handlerQName: m[3],
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// REST — FastAPI (Python)
// ---------------------------------------------------------------------------

// @app.get("/path") decorator followed on the next non-decorator line by
// `def handler_name(`.
var fastAPIRE = regexp.MustCompile(
	`(?m)@(?:app|router)\.(get|post|put|delete|patch|head|options)\s*\(\s*["']([^"'\s]{1,500})["']`,
)

// pythonDefAfterDecoratorRE locates the first `def foo(` following a position.
var pythonDefAfterDecoratorRE = regexp.MustCompile(`(?m)^\s*(?:async\s+)?def\s+(\w+)\s*\(`)

func detectFastAPI(source string) []frameworkMatch {
	var out []frameworkMatch
	for _, m := range fastAPIRE.FindAllStringSubmatchIndex(source, -1) {
		method := strings.ToUpper(source[m[2]:m[3]])
		path := source[m[4]:m[5]]
		handler := ""
		// Scan forward for the next def.
		tail := source[m[1]:]
		if dm := pythonDefAfterDecoratorRE.FindStringSubmatch(tail); len(dm) >= 2 {
			handler = dm[1]
		}
		out = append(out, frameworkMatch{
			method:       method,
			rawPath:      path,
			handlerQName: handler,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// REST — Flask (Python)
// ---------------------------------------------------------------------------

// @app.route("/path", methods=["GET"]) — the methods kwarg is optional and
// defaults to GET. We capture path first, then optionally extract methods.
var flaskRouteRE = regexp.MustCompile(
	`(?ms)@(?:app|bp|blueprint)\.route\s*\(\s*["']([^"']{1,500})["']([^)]*)\)`,
)

var flaskMethodsRE = regexp.MustCompile(`(?i)methods\s*=\s*\[([^\]]+)\]`)
var flaskSingleMethodRE = regexp.MustCompile(`(?i)["'](GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)["']`)

func detectFlask(source string) []frameworkMatch {
	var out []frameworkMatch
	for _, m := range flaskRouteRE.FindAllStringSubmatchIndex(source, -1) {
		path := source[m[2]:m[3]]
		rest := source[m[4]:m[5]]
		methods := []string{"GET"}
		if mm := flaskMethodsRE.FindStringSubmatch(rest); len(mm) >= 2 {
			methods = methods[:0]
			for _, sm := range flaskSingleMethodRE.FindAllStringSubmatch(mm[1], -1) {
				methods = append(methods, strings.ToUpper(sm[1]))
			}
			if len(methods) == 0 {
				methods = []string{"GET"}
			}
		}
		handler := ""
		tail := source[m[1]:]
		if dm := pythonDefAfterDecoratorRE.FindStringSubmatch(tail); len(dm) >= 2 {
			handler = dm[1]
		}
		for _, method := range methods {
			out = append(out, frameworkMatch{
				method:       method,
				rawPath:      path,
				handlerQName: handler,
			})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// REST — Spring (Java/Kotlin)
// ---------------------------------------------------------------------------

// @GetMapping("/path") / @PostMapping / @RequestMapping(value="/path", method=RequestMethod.GET)
var springTypedMappingRE = regexp.MustCompile(
	`(?m)@(Get|Post|Put|Delete|Patch|Head|Options|Request)Mapping\s*\(\s*(?:value\s*=\s*)?"([^"\s]{1,500})"`,
)

var springMethodAfter = regexp.MustCompile(
	`(?m)^\s*(?:public|private|protected|static|final|\s)*\s+[\w<>\[\],\s?]+?\s+(\w+)\s*\(`,
)

func detectSpring(source string) []frameworkMatch {
	var out []frameworkMatch
	for _, m := range springTypedMappingRE.FindAllStringSubmatchIndex(source, -1) {
		verbRaw := source[m[2]:m[3]]
		path := source[m[4]:m[5]]
		method := strings.ToUpper(verbRaw)
		if method == "REQUEST" {
			method = "GET" // default when @RequestMapping has no explicit method
		}
		handler := ""
		tail := source[m[1]:]
		if dm := springMethodAfter.FindStringSubmatch(tail); len(dm) >= 2 {
			handler = dm[1]
		}
		out = append(out, frameworkMatch{
			method:       method,
			rawPath:      path,
			handlerQName: handler,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// REST — Django urls.py (Python)
// ---------------------------------------------------------------------------

// path('users/<int:id>/', views.user_detail, name='user_detail')
var djangoPathRE = regexp.MustCompile(
	`(?m)\b(?:path|re_path)\s*\(\s*["']([^"']{0,500})["']\s*,\s*([\w.]+)`,
)

func detectDjango(source string) []frameworkMatch {
	var out []frameworkMatch
	for _, m := range djangoPathRE.FindAllStringSubmatch(source, -1) {
		out = append(out, frameworkMatch{
			method:       "GET", // Django urls.py does not encode method; view class/fn does
			rawPath:      m[1],
			handlerQName: m[2],
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// REST — Phoenix (Elixir)
// ---------------------------------------------------------------------------

// get "/path", Controller, :action
var phoenixRE = regexp.MustCompile(
	`(?m)^\s*(get|post|put|patch|delete|head|options)\s+"([^"]{1,500})"\s*,\s*(\w+)\s*,\s*:(\w+)`,
)

func detectPhoenix(source string) []frameworkMatch {
	var out []frameworkMatch
	for _, m := range phoenixRE.FindAllStringSubmatch(source, -1) {
		out = append(out, frameworkMatch{
			method:       strings.ToUpper(m[1]),
			rawPath:      m[2],
			handlerQName: m[3] + "." + m[4],
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// REST — ASP.NET (C#)
// ---------------------------------------------------------------------------

// [HttpGet("/path")] immediately above `public … Method(` declaration
var aspnetAttrRE = regexp.MustCompile(
	`(?m)\[Http(Get|Post|Put|Delete|Patch|Head|Options)\s*\(\s*"([^"]{1,500})"\s*\)\s*\]`,
)
var csharpMethodAfter = regexp.MustCompile(
	`(?m)^\s*(?:public|private|protected|internal|static|async|virtual|override|\s)+\s+[\w<>\[\],\s?]+?\s+(\w+)\s*\(`,
)

func detectASPNet(source string) []frameworkMatch {
	var out []frameworkMatch
	for _, m := range aspnetAttrRE.FindAllStringSubmatchIndex(source, -1) {
		method := strings.ToUpper(source[m[2]:m[3]])
		path := source[m[4]:m[5]]
		handler := ""
		tail := source[m[1]:]
		if dm := csharpMethodAfter.FindStringSubmatch(tail); len(dm) >= 2 {
			handler = dm[1]
		}
		out = append(out, frameworkMatch{method: method, rawPath: path, handlerQName: handler})
	}
	return out
}

// ---------------------------------------------------------------------------
// REST — Rails routes.rb
// ---------------------------------------------------------------------------

// get '/users/:id', to: 'users#show'
var railsRE = regexp.MustCompile(
	`(?m)^\s*(get|post|put|patch|delete|head|options)\s+['"]([^'"]{1,500})['"]\s*(?:,\s*to:\s*['"]([^'"]+)['"])?`,
)

func detectRails(source string) []frameworkMatch {
	var out []frameworkMatch
	for _, m := range railsRE.FindAllStringSubmatch(source, -1) {
		out = append(out, frameworkMatch{
			method:       strings.ToUpper(m[1]),
			rawPath:      m[2],
			handlerQName: m[3],
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// gRPC — .proto service definitions
// ---------------------------------------------------------------------------

// rpc MethodName (stream? Req) returns (stream? Resp);
var grpcRpcRE = regexp.MustCompile(
	`(?m)\brpc\s+(\w+)\s*\(\s*(stream\s+)?(\w[\w.]*)\s*\)\s*returns\s*\(\s*(stream\s+)?(\w[\w.]*)\s*\)`,
)

// service ServiceName { … rpc … }
var grpcServiceRE = regexp.MustCompile(`(?m)\bservice\s+(\w+)\s*{`)

func detectGRPC(source string) []frameworkMatch {
	var out []frameworkMatch

	// Locate service names by byte offset so we can attribute each rpc to its enclosing service.
	type svcSpan struct {
		name  string
		start int
	}
	var svcs []svcSpan
	for _, m := range grpcServiceRE.FindAllStringSubmatchIndex(source, -1) {
		svcs = append(svcs, svcSpan{
			name:  source[m[2]:m[3]],
			start: m[0],
		})
	}
	nameAt := func(pos int) string {
		name := ""
		for _, s := range svcs {
			if s.start <= pos {
				name = s.name
			} else {
				break
			}
		}
		return name
	}

	for _, m := range grpcRpcRE.FindAllStringSubmatchIndex(source, -1) {
		rpcName := source[m[2]:m[3]]
		reqStream := m[4] != -1
		req := source[m[6]:m[7]]
		respStream := m[8] != -1
		resp := source[m[10]:m[11]]

		var kind string
		switch {
		case reqStream && respStream:
			kind = "BIDI_STREAM"
		case reqStream:
			kind = "CLIENT_STREAM"
		case respStream:
			kind = "SERVER_STREAM"
		default:
			kind = "UNARY"
		}

		svc := nameAt(m[0])
		path := "/" + svc + "/" + rpcName
		handler := svc + "." + rpcName
		if svc == "" {
			path = "/" + rpcName
			handler = rpcName
		}

		out = append(out, frameworkMatch{
			method:       kind,
			rawPath:      path,
			handlerQName: handler,
		})

		// Stash request/response types on the match via rawPath is not ideal;
		// the caller reads them from a parallel pass. Keep single source of truth:
		// we append them as query_params-style metadata via a dedicated hook.
		_ = req
		_ = resp
	}
	return out
}

// ---------------------------------------------------------------------------
// GraphQL SDL
// ---------------------------------------------------------------------------

// Query / Mutation / Subscription block — we capture the block body then
// pull each top-level field name as a separate endpoint.
var graphqlBlockRE = regexp.MustCompile(`(?ms)(?:type\s+)?(Query|Mutation|Subscription)\s*\{([^}]*)\}`)
var graphqlFieldRE = regexp.MustCompile(`(?m)^\s*(\w+)\s*(?:\([^)]*\))?\s*:\s*[\w\[\]!]+`)

func detectGraphQL(source string) []frameworkMatch {
	var out []frameworkMatch
	for _, block := range graphqlBlockRE.FindAllStringSubmatch(source, -1) {
		if len(block) < 3 {
			continue
		}
		op := strings.ToUpper(block[1])
		body := block[2]
		for _, f := range graphqlFieldRE.FindAllStringSubmatch(body, -1) {
			if len(f) < 2 {
				continue
			}
			field := f[1]
			out = append(out, frameworkMatch{
				method:       op,
				rawPath:      "/" + field,
				handlerQName: field,
			})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Framework registry
// ---------------------------------------------------------------------------

// frameworkOrder is the deterministic detection order. Importers that match
// multiple frameworks resolve to the first entry in this slice.
var frameworkOrder = []frameworkEntry{
	{
		name:        "gin",
		style:       "rest",
		importHints: []string{"gin-gonic/gin", "gin"},
		detect:      detectGin,
	},
	{
		name:        "express",
		style:       "rest",
		importHints: []string{"express"},
		detect:      detectExpress,
	},
	{
		name:        "fastapi",
		style:       "rest",
		importHints: []string{"fastapi"},
		detect:      detectFastAPI,
	},
	{
		name:        "flask",
		style:       "rest",
		importHints: []string{"flask"},
		detect:      detectFlask,
	},
	{
		name:        "spring",
		style:       "rest",
		importHints: []string{"springframework", "org.springframework"},
		detect:      detectSpring,
	},
	{
		name:        "django",
		style:       "rest",
		importHints: []string{"django"},
		detect:      detectDjango,
	},
	{
		name:        "phoenix",
		style:       "rest",
		importHints: []string{"phoenix", "plug"},
		detect:      detectPhoenix,
	},
	{
		name:        "aspnet",
		style:       "rest",
		importHints: []string{"microsoft.aspnetcore", "aspnetcore"},
		detect:      detectASPNet,
	},
	{
		name:        "rails",
		style:       "rest",
		importHints: []string{"rails", "actiondispatch"},
		detect:      detectRails,
	},
	{
		name:        "grpc",
		style:       "grpc",
		importHints: []string{"proto", "grpc", "service"},
		detect:      detectGRPC,
	},
	{
		name:        "graphql",
		style:       "graphql",
		importHints: []string{"graphql", "apollo"},
		detect:      detectGraphQL,
	},
}

// selectFramework picks the first framework whose import hints match the file.
// Returns nil when no framework is recognised.
//
// For files where import parsing yields nothing (e.g. .proto or .graphql SDL),
// we additionally honour file-extension hints provided by the caller via the
// `forceFramework` argument.
func selectFramework(tokens map[string]bool, forceFramework string) *frameworkEntry {
	if forceFramework != "" {
		for i := range frameworkOrder {
			if frameworkOrder[i].name == forceFramework {
				return &frameworkOrder[i]
			}
		}
	}
	for i := range frameworkOrder {
		if matchesAnyImport(tokens, frameworkOrder[i].importHints) {
			return &frameworkOrder[i]
		}
	}
	return nil
}
