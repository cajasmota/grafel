// Backend-HTTP routing synthesizers for the seven JS/TS frameworks that had
// no endpoint extraction before #2851:
//
//   - AdonisJS — `Route.get('/users/:id', 'UsersController.show')` plus the
//     resourceful `Route.resource('users', 'UsersController')` form.
//   - Hapi     — `server.route({ method, path, handler })` (method may be a
//     string or an array of verbs).
//   - Feathers — service-based: `app.use('/messages', new MessageService())`
//     registers a REST service at the mount path; each service exposes the
//     standard find/get/create/update/patch/remove verb set.
//   - Marble.js — `r.pipe(r.matchPath('/users/:id'), r.matchType('GET'))`
//     EffectRoute composition.
//   - Polka    — Express-compatible micro-router: `app.get('/users/:id', h)`.
//   - Restify  — `server.get('/users/:id', h)` (Express-shaped, distinct
//     receiver convention).
//   - Sails    — declarative `config/routes.js`: `'GET /users/:id':
//     'UsersController.find'`.
//
// Each synthesizer re-uses the shared `emit` closure from
// applyHTTPEndpointSynthesis, so the existing http_endpoint_resolve.go
// handler→file/line rewrite applies uniformly. The handler reference is
// forwarded as `Controller:<name>` (or `SCOPE.Operation:<name>` for inline
// effects) so the resolve pass attributes each endpoint to its handler.
//
// All synthesizers are import-guarded by a cheap substring fast-path so they
// no-op on files that don't use the framework.
//
// Refs #2851.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// feathersServiceVerbs is the standard REST verb set a Feathers service
// exposes at its mount path. `find` (list) and `create` map to the collection
// path; `get`/`update`/`patch`/`remove` map to the `{id}` member path.
//
// We model the collection/member split with the canonical `{id}` placeholder
// so the byPath cross-repo linker matches consumer calls regardless of the
// concrete param name.
var feathersServiceVerbs = []struct {
	verb   string
	member bool   // true → endpoint targets the `/{id}` member path
	method string // the service method backing this verb (handler symbol)
}{
	{"GET", false, "find"},     // service.find()
	{"POST", false, "create"},  // service.create()
	{"GET", true, "get"},       // service.get(id)
	{"PUT", true, "update"},    // service.update(id)
	{"PATCH", true, "patch"},   // service.patch(id)
	{"DELETE", true, "remove"}, // service.remove(id)
}

// ---------------------------------------------------------------------------
// AdonisJS — #2851
// ---------------------------------------------------------------------------

// adonisVerbRe captures `Route.get('/path', 'Ctrl.method')` and the inline
// handler / closure forms. The receiver is `Route` (the global router facade)
// in AdonisJS 5/6. Groups: 1=verb, 2=path, 3=handler ('Ctrl.method' or fn).
var adonisVerbRe = regexp.MustCompile(
	`\bRoute\.(get|post|put|patch|delete|any)\s*\(` +
		`\s*['"` + "`" + `]([^'"` + "`" + `\n\r]+)['"` + "`" + `]` +
		`\s*(?:,\s*['"` + "`" + `]?([\w$.]+)['"` + "`" + `]?)?`,
)

// adonisResourceRe captures `Route.resource('users', 'UsersController')`,
// which AdonisJS expands into the seven RESTful routes. Groups: 1=resource
// name, 2=controller.
var adonisResourceRe = regexp.MustCompile(
	`\bRoute\.(?:resource|apiResource|shallowResource)\s*\(` +
		`\s*['"` + "`" + `]([^'"` + "`" + `\n\r]+)['"` + "`" + `]` +
		`\s*,\s*['"` + "`" + `]?([\w$.]+)['"` + "`" + `]?`,
)

// adonisResourceRoutes is the standard RESTful expansion of Route.resource.
// `apiResource` is the same minus the create/edit form routes, but emitting
// the superset is harmless for cross-repo matching (the extra GET form routes
// simply never match a consumer call).
var adonisResourceRoutes = []struct {
	verb, suffix, action string
}{
	{"GET", "", "index"},
	{"POST", "", "store"},
	{"GET", "/{id}", "show"},
	{"PUT", "/{id}", "update"},
	{"DELETE", "/{id}", "destroy"},
}

func synthesizeAdonis(content string, emit emitFileFn) {
	if !strings.Contains(content, "Route.") {
		return
	}
	for _, m := range adonisVerbRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 3 {
			continue
		}
		verb := strings.ToUpper(m[1])
		raw := m[2]
		handler := ""
		if len(m) >= 4 {
			handler = m[3]
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkAdonis, raw)
		if canonical == "" {
			continue
		}
		method, fileHint := splitControllerActionRef(handler)
		emit(verb, canonical, "adonisjs", "Controller", method, fileHint, 0)
	}
	for _, m := range adonisResourceRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 3 {
			continue
		}
		name := strings.Trim(m[1], "/")
		controller := m[2]
		base := "/" + name
		for _, rr := range adonisResourceRoutes {
			canonical := httproutes.Canonicalize(httproutes.FrameworkAdonis, base+rr.suffix)
			emit(rr.verb, canonical, "adonisjs", "Controller", rr.action, controller, 0)
		}
	}
}

// splitControllerActionRef splits a `Controller.method` handler reference into
// the bare method name (used as the source_handler symbol) and the controller
// basename (used as the cross-file handler_file hint). A bare reference with
// no dot is returned as-is with an empty file hint.
func splitControllerActionRef(ref string) (method, fileHint string) {
	if i := strings.LastIndexByte(ref, '.'); i >= 0 {
		return ref[i+1:], ref[:i]
	}
	return ref, ""
}

// ---------------------------------------------------------------------------
// Hapi — #2851
// ---------------------------------------------------------------------------

// hapiRouteRe captures a `server.route({ ... })` object. The method and path
// kwargs may appear in either order, so we capture the whole object body and
// scan it for `method:` and `path:` separately. Group 1 = object body.
var hapiRouteRe = regexp.MustCompile(`\b(?:server|hapi|srv)\.route\s*\(\s*(\{)`)

// hapiMethodKwargRe captures `method: 'GET'` or `method: ['GET','POST']`.
// Group 1 = single verb, group 2 = array body (mutually exclusive).
var hapiMethodKwargRe = regexp.MustCompile(
	`method\s*:\s*(?:\[([^\]]+)\]|['"` + "`" + `]([A-Za-z*]+)['"` + "`" + `])`,
)

// hapiPathKwargRe captures `path: '/users/{id}'`.
var hapiPathKwargRe = regexp.MustCompile(
	`path\s*:\s*['"` + "`" + `]([^'"` + "`" + `\n\r]+)['"` + "`" + `]`,
)

// hapiHandlerKwargRe captures `handler: namedHandler` (a bare identifier
// reference). Inline arrow / function handlers leave it empty.
var hapiHandlerKwargRe = regexp.MustCompile(
	`handler\s*:\s*([A-Za-z_$][\w$.]*)\s*[,}\r\n]`,
)

func synthesizeHapi(content string, emit emitFn) {
	if !strings.Contains(content, ".route(") {
		return
	}
	for _, idx := range hapiRouteRe.FindAllStringSubmatchIndex(content, -1) {
		// idx[2]:idx[3] spans the opening brace; find its match.
		braceOpen := idx[2]
		braceClose := findMatchingBrace(content, braceOpen)
		if braceClose < 0 {
			continue
		}
		body := content[braceOpen : braceClose+1]

		pm := hapiPathKwargRe.FindStringSubmatch(body)
		if len(pm) < 2 {
			continue
		}
		raw := stripHapiPathModifiers(pm[1])
		canonical := httproutes.Canonicalize(httproutes.FrameworkHapi, raw)
		if canonical == "" {
			continue
		}

		handler := ""
		if hm := hapiHandlerKwargRe.FindStringSubmatch(body); len(hm) >= 2 {
			handler = hm[1]
		}

		verbs := parseHapiMethods(body)
		if len(verbs) == 0 {
			continue
		}
		for _, verb := range verbs {
			emit(verb, canonical, "hapi", "Controller", handler)
		}
	}
}

// parseHapiMethods extracts the verb(s) from a route object body. Hapi accepts
// a single string, an array of strings, or the wildcard "*" (all verbs → ANY).
func parseHapiMethods(body string) []string {
	mm := hapiMethodKwargRe.FindStringSubmatch(body)
	if len(mm) < 3 {
		return nil
	}
	var raw []string
	if mm[1] != "" {
		for _, tok := range strings.Split(mm[1], ",") {
			tok = strings.TrimSpace(strings.Trim(strings.TrimSpace(tok), `"'`+"`"))
			if tok != "" {
				raw = append(raw, tok)
			}
		}
	} else if mm[2] != "" {
		raw = append(raw, mm[2])
	}
	var out []string
	for _, v := range raw {
		if v == "*" {
			out = append(out, "ANY")
			continue
		}
		out = append(out, strings.ToUpper(v))
	}
	return out
}

// stripHapiPathModifiers removes Hapi's optional `?` and catch-all `*` / `*n`
// param modifiers so `{id?}` / `{path*}` / `{seg*2}` canonicalise to `{id}` /
// `{path}` / `{seg}`. The curly-brace canonicaliser only strips a `:regex`
// suffix, not these Hapi-specific markers, so we normalise them here first.
func stripHapiPathModifiers(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	i := 0
	for i < len(raw) {
		c := raw[i]
		if c != '{' {
			b.WriteByte(c)
			i++
			continue
		}
		end := strings.IndexByte(raw[i+1:], '}')
		if end < 0 {
			b.WriteByte(c)
			i++
			continue
		}
		inner := raw[i+1 : i+1+end]
		// Drop a trailing `?` (optional) or `*`/`*N` (catch-all) marker.
		inner = strings.TrimRight(inner, "?")
		if star := strings.IndexByte(inner, '*'); star >= 0 {
			inner = inner[:star]
		}
		b.WriteByte('{')
		b.WriteString(strings.TrimSpace(inner))
		b.WriteByte('}')
		i += 1 + end + 1
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Feathers — #2851
// ---------------------------------------------------------------------------

// feathersUseRe captures `app.use('/messages', service)` registrations. The
// second argument is the service (a class instance, function, or identifier).
// We capture it as the handler reference. Group 1 = mount path, 2 = service.
var feathersUseRe = regexp.MustCompile(
	`\b(?:app|feathers)\.use\s*\(` +
		`\s*['"` + "`" + `](/[^'"` + "`" + `\n\r]*)['"` + "`" + `]` +
		`\s*,\s*(?:new\s+)?([A-Za-z_$][\w$.]*)`,
)

func synthesizeFeathers(content string, emit emitFileFn) {
	if !strings.Contains(content, ".use(") {
		return
	}
	// Feathers signal: must look like a Feathers app, not a generic Express
	// `app.use(middleware)`. Require the service-mount shape (path + service
	// argument) AND a Feathers import/setup marker.
	if !strings.Contains(content, "feathers") && !strings.Contains(content, "@feathersjs") &&
		!strings.Contains(content, "Service") {
		return
	}
	for _, m := range feathersUseRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 3 {
			continue
		}
		mount := strings.TrimRight(m[1], "/")
		if mount == "" {
			mount = "/"
		}
		service := m[2]
		// Skip the express-middleware idiom `app.use('/static', express.static(...))`
		// — express.static is not a Feathers service.
		if strings.HasPrefix(service, "express.") {
			continue
		}
		for _, sv := range feathersServiceVerbs {
			path := mount
			if sv.member {
				path = mount + "/{id}"
			}
			canonical := httproutes.Canonicalize(httproutes.FrameworkFeathers, path)
			// Attribute each verb to the service METHOD that backs it
			// (find/get/create/...). The service class basename is the
			// cross-file handler_file hint so the resolver lands the
			// method even when the service lives in another module.
			emit(sv.verb, canonical, "feathers", "Controller", sv.method, service, 0)
		}
	}
}

// ---------------------------------------------------------------------------
// Marble.js — #2851
// ---------------------------------------------------------------------------
//
// Marble.js declares routes as Effects piped through `r.pipe(...)`:
//
//	const getUser$ = r.pipe(
//	  r.matchPath('/users/:id'),
//	  r.matchType('GET'),
//	  r.useEffect(req$ => req$.pipe(...))
//	);
//
// We extract one endpoint per r.pipe block, reading the path from
// `r.matchPath(...)` and the verb from `r.matchType(...)`. The enclosing
// `const <name>$ = r.pipe(` binds the Effect name, which we forward as the
// handler reference.

// marbleEffectRe captures the `const <name> = r.pipe(` opener that introduces
// a routing Effect. Group 1 = effect name.
var marbleEffectRe = regexp.MustCompile(
	`(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*r\.pipe\s*\(`,
)

// marbleMatchPathRe captures `r.matchPath('/users/:id')`. Group 1 = path.
var marbleMatchPathRe = regexp.MustCompile(
	`r\.matchPath\s*\(\s*['"` + "`" + `]([^'"` + "`" + `\n\r]+)['"` + "`" + `]`,
)

// marbleMatchTypeRe captures `r.matchType('GET')`. Group 1 = verb.
var marbleMatchTypeRe = regexp.MustCompile(
	`r\.matchType\s*\(\s*['"` + "`" + `]([A-Za-z]+)['"` + "`" + `]`,
)

func synthesizeMarble(content string, emit emitFn) {
	if !strings.Contains(content, "r.pipe") || !strings.Contains(content, "matchPath") {
		return
	}
	for _, idx := range marbleEffectRe.FindAllStringSubmatchIndex(content, -1) {
		effectName := content[idx[2]:idx[3]]
		// The r.pipe( opening paren is the last byte the regex consumed.
		parenOpen := idx[1] - 1
		parenClose := findMatchingParenFrom(content, parenOpen)
		if parenClose < 0 {
			continue
		}
		body := content[parenOpen : parenClose+1]

		pm := marbleMatchPathRe.FindStringSubmatch(body)
		if len(pm) < 2 {
			continue
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkMarble, pm[1])
		if canonical == "" {
			continue
		}
		verb := "ANY"
		if tm := marbleMatchTypeRe.FindStringSubmatch(body); len(tm) >= 2 {
			verb = strings.ToUpper(tm[1])
		}
		emit(verb, canonical, "marblejs", "SCOPE.Operation", effectName)
	}
}

// findMatchingParenFrom returns the index of the `)` that closes the `(` at
// openIdx, or -1 if not found within a bounded scan. Mirrors findMatchingBrace
// for parens (the existing findMatchingParenAfter assumes depth is already 1
// at the start byte; this variant takes the open-paren position directly).
func findMatchingParenFrom(content string, openIdx int) int {
	depth := 0
	limit := openIdx + 8192
	if limit > len(content) {
		limit = len(content)
	}
	for i := openIdx; i < limit; i++ {
		switch content[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// Polka & Restify — #2851 (Express-shaped)
// ---------------------------------------------------------------------------

// polkaRestifyVerbRe captures `<recv>.<verb>('/path', handler)` for the
// Polka / Restify receivers. We use a dedicated receiver allowlist that
// includes `polka` and `restify`-conventional names so these endpoints are
// attributed to the correct framework rather than being absorbed (and
// mislabelled "express") by the generic Express synthesizer.
//
// Groups: 1=receiver, 2=verb, 3=path, 4=handler.
var polkaRestifyVerbRe = regexp.MustCompile(
	`([$\w][\w$]*)\.(get|post|put|patch|del|delete|head|opts|options)\s*\(` +
		`\s*['"` + "`" + `]([^'"` + "`" + `\n\r]+)['"` + "`" + `]` +
		`\s*(?:,[^,)]*)*?,\s*([\w$.]+)\s*[\),]`,
)

// polkaRestifyVerbPathOnlyRe is the inline-handler variant. Groups:
// 1=receiver, 2=verb, 3=path.
var polkaRestifyVerbPathOnlyRe = regexp.MustCompile(
	`([$\w][\w$]*)\.(get|post|put|patch|del|delete|head|opts|options)\s*\(` +
		`\s*['"` + "`" + `]([^'"` + "`" + `\n\r]+)['"` + "`" + `]`,
)

// restifyMethodNorm maps Restify's short verb aliases to canonical verbs.
func restifyMethodNorm(v string) string {
	switch strings.ToLower(v) {
	case "del":
		return "DELETE"
	case "opts":
		return "OPTIONS"
	default:
		return strings.ToUpper(v)
	}
}

// synthesizePolkaRestify covers both Polka and Restify, distinguishing the two
// by file signal so each endpoint is tagged with the right framework. Polka
// apps are created with `polka()`; Restify servers with
// `restify.createServer()`.
func synthesizePolkaRestify(content string, emit emitFn) {
	isPolka := strings.Contains(content, "polka(") || strings.Contains(content, "'polka'") ||
		strings.Contains(content, `"polka"`)
	isRestify := strings.Contains(content, "restify") || strings.Contains(content, "createServer(")
	if !isPolka && !isRestify {
		return
	}
	framework := "restify"
	canonFW := httproutes.FrameworkRestify
	if isPolka && !isRestify {
		framework = "polka"
		canonFW = httproutes.FrameworkPolka
	}

	withHandler := map[string]bool{}
	for _, m := range polkaRestifyVerbRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 5 {
			continue
		}
		receiver := m[1]
		if !isExpressReceiver(receiver, nil) {
			continue
		}
		raw := m[3]
		if !looksLikeExpressPath(raw) {
			continue
		}
		verb := restifyMethodNorm(m[2])
		handler := m[4]
		if isInlineExpressHandler(m[0], raw) {
			handler = ""
		}
		canonical := httproutes.Canonicalize(canonFW, raw)
		key := verb + ":" + canonical
		withHandler[key] = true
		emit(verb, canonical, framework, "Controller", handler)
	}
	for _, m := range polkaRestifyVerbPathOnlyRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 4 {
			continue
		}
		receiver := m[1]
		if !isExpressReceiver(receiver, nil) {
			continue
		}
		raw := m[3]
		if !looksLikeExpressPath(raw) {
			continue
		}
		verb := restifyMethodNorm(m[2])
		canonical := httproutes.Canonicalize(canonFW, raw)
		key := verb + ":" + canonical
		if withHandler[key] {
			continue
		}
		emit(verb, canonical, framework, "Controller", "")
	}
}

// ---------------------------------------------------------------------------
// Sails — #2851
// ---------------------------------------------------------------------------
//
// Sails declares explicit routes in `config/routes.js` as a map literal whose
// keys are `'<VERB> /path'` (or just `'/path'` → ANY) and whose values are a
// controller-action string `'UserController.find'`, an action address
// `'user/find'`, or an object `{ controller, action }`.

// sailsRouteRe captures `'GET /users/:id': 'UserController.find'` and the
// verb-less `'/path': 'Ctrl.action'` form. Groups: 1=verb (optional),
// 2=path, 3=target string.
var sailsRouteRe = regexp.MustCompile(
	`['"` + "`" + `](?:(GET|POST|PUT|PATCH|DELETE|OPTIONS|HEAD|ALL)\s+)?` +
		`(/[^'"` + "`" + `\n\r]*)['"` + "`" + `]` +
		`\s*:\s*['"` + "`" + `]([\w$./]+)['"` + "`" + `]`,
)

// synthesizeSails extracts routes from a Sails config/routes.js map. It is
// path-gated on the routes-config filename to avoid firing on arbitrary object
// literals elsewhere in a project.
func synthesizeSails(filePath, content string, emit emitFileFn) {
	if !sailsRoutesFile(filePath) {
		return
	}
	for _, m := range sailsRouteRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 4 {
			continue
		}
		verb := strings.ToUpper(m[1])
		if verb == "" || verb == "ALL" {
			verb = "ANY"
		}
		raw := m[2]
		target := m[3]
		canonical := httproutes.Canonicalize(httproutes.FrameworkSails, raw)
		if canonical == "" {
			continue
		}
		method, fileHint := splitControllerActionRef(target)
		emit(verb, canonical, "sails", "Controller", method, fileHint, 0)
	}
}

// sailsRoutesFile reports whether filePath is a Sails routes-config file
// (`config/routes.js` / `config/routes.ts`).
func sailsRoutesFile(filePath string) bool {
	p := strings.ReplaceAll(filePath, "\\", "/")
	return strings.HasSuffix(p, "config/routes.js") ||
		strings.HasSuffix(p, "config/routes.ts")
}
