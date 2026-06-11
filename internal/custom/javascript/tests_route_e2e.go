// tests_route_e2e.go — Playwright / Cypress browser-e2e API-call → endpoint
// route-hit linkage.
//
// #4399 (the TS browser-e2e slice of the coverage-linkage tail epic #4615/#4334;
// a sibling of the NestJS/supertest #4351, Python-client #4369, Spring #4370 and
// Go-httptest / Rails-request-spec #4371 slices). Emits ONE test_suite entity
// per Playwright/Cypress spec file that drives an HTTP endpoint by ROUTE STRING
// via the browser-e2e API-call idioms, stamping the captured `VERB route` pairs
// onto the suite's `e2e_route_calls` property (one "VERB route" per line). The
// shared resolve pass (engine.linkE2ERouteTestsToEndpoints, #4351) then matches
// each pair against the cross-file http_endpoint_definition index and emits a
// TESTS edge to the exact endpoint exercised — exactly as every other slice does
// for its stack. The engine pass is language-agnostic (it fires on any
// test_suite carrying e2e_route_calls), so the only work here is the
// Playwright/Cypress route capture.
//
// Why a SEPARATE extractor (not the jest.go supertest path): Playwright and
// Cypress specs do NOT use supertest's `request(app).get('/x')` form (jest.go
// #4351 already owns that). They drive a live server through a fixture-injected
// request context (`request`, `page.request`, an `apiContext`) or Cypress's
// `cy.request` / `cy.intercept`. jest.go also only mints its suite when a
// describe/it/test is present and reuses Nest-spec subject resolution; the
// browser-e2e capture is an orthogonal route-string surface that warrants its
// own one-per-file suite (keyed off the spec path, namespaced to avoid by-name
// re-orphaning, the #4366/#4343 lesson).
//
// Playwright idioms captured (the request-context fixture has many aliases —
// `request`, `page.request`, a destructured `apiContext`, `ctx`, `api`, … — so
// we match the VERB METHOD on ANY receiver, gated on the route being a
// leading-slash literal):
//   - request.post('/api/users', { data })            → POST   /api/users
//   - page.request.get('/api/users')                  → GET    /api/users
//   - apiContext.fetch('/api/users', { method:'PUT' }) → PUT    /api/users
//   - request.fetch('/api/x', { method: 'DELETE' })   → DELETE /api/x
//
// Cypress idioms captured:
//   - cy.request('POST', '/api/users', body)          → POST   /api/users
//   - cy.request('GET', '/api/users')                 → GET    /api/users
//   - cy.request({ method: 'PUT', url: '/api/x' })    → PUT    /api/x
//   - cy.request('/api/health')                       → GET    /api/health   (default verb)
//   - cy.intercept('GET', '/api/users')               → GET    /api/users
//   - cy.intercept({ method:'POST', url:'/api/x' })   → POST   /api/x
//
// Honest exclusions (no fabricated edges):
//   - A built / interpolated URL with no static leading-slash literal
//     (`request.get(`${base}/users`)`, `cy.request(url)`, `cy.request(apiUrl(id))`)
//     is dropped — only a single-quote / double-quote leading-slash literal is
//     captured. (A leading-slash template literal with NO interpolation, e.g.
//     `request.get(`/api/users`)`, IS captured; a template containing `${…}`
//     before the path is not.)
//   - Non-spec files emit no suite.
//   - The `.fetch(...)` form requires a `method:` in its options object to know
//     the verb; a bare `.fetch('/x')` with no method is conservatively skipped
//     (the WHATWG default is GET, but a fetch-without-method on an API context is
//     ambiguous enough — and rare enough in e2e specs — that we do not guess).
//
// Registration key: "custom_js_tests_route_e2e" (the canonical
// `custom_<lang>_tests_route_e2e` convention shared by every tail slice; the JS
// dispatch prefix is `custom_js_`, so CustomExtractorsFor("javascript"/"
// typescript") picks it up — the #4769 dispatch-prefix lesson).
package javascript

import (
	"context"
	"regexp"
	"strings"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extreg.Register("custom_js_tests_route_e2e", &jsTestRouteE2EExtractor{})
}

type jsTestRouteE2EExtractor struct{}

func (e *jsTestRouteE2EExtractor) Language() string { return "custom_js_tests_route_e2e" }

var (
	// reBrowserE2EVerbCall matches a Playwright request-context verb call on ANY
	// receiver whose FIRST argument is a leading-slash string/template literal:
	// `request.post('/api/users', …)`, `page.request.get('/api/users')`,
	// `apiContext.delete('/api/x')`. Group 1 = verb, group 2 = the route literal.
	// The route is captured only when it is a leading-slash literal (single,
	// double, or non-interpolated backtick) — a `${…}`-built URL never matches
	// because the opening quote must be immediately followed by `/`.
	reBrowserE2EVerbCall = regexp.MustCompile(
		`\.(get|post|put|delete|patch|head|options)\s*\(\s*['"` + "`" + `](/[^'"` + "`" + `$\n\r]*)['"` + "`" + `]`)

	// reBrowserE2EFetch matches a Playwright apiContext `.fetch('/route', { method:
	// 'VERB' })` call. Group 1 = the route literal, group 2 = the method value.
	// The route must be a leading-slash literal; the method must appear in the
	// options object within a bounded window after the route. A `.fetch` with no
	// `method:` is skipped (see honest exclusions).
	reBrowserE2EFetch = regexp.MustCompile(
		`\.fetch\s*\(\s*['"` + "`" + `](/[^'"` + "`" + `$\n\r]*)['"` + "`" + `]\s*,[^)]{0,200}?\bmethod\s*:\s*['"` + "`" + `]([A-Za-z]+)['"` + "`" + `]`)

	// reCyRequestPositional matches the Cypress `cy.request('VERB', '/route', …)`
	// two-positional form. Group 1 = verb, group 2 = route literal.
	reCyRequestPositional = regexp.MustCompile(
		`\bcy\s*\.\s*request\s*\(\s*['"` + "`" + `]([A-Za-z]+)['"` + "`" + `]\s*,\s*['"` + "`" + `](/[^'"` + "`" + `$\n\r]*)['"` + "`" + `]`)

	// reCyRequestSingle matches the Cypress `cy.request('/route')` single-string
	// form (default verb GET). Group 1 = route literal. Anchored so it does NOT
	// also fire on the two-positional / options-object forms (the first arg must
	// be a leading-slash literal, and the next non-space char must close the call
	// or be a trailing-arg comma — i.e. not another quoted positional, which the
	// positional regex already owns).
	reCyRequestSingle = regexp.MustCompile(
		`\bcy\s*\.\s*request\s*\(\s*['"` + "`" + `](/[^'"` + "`" + `$\n\r]*)['"` + "`" + `]\s*[),]`)

	// reCyIntercept matches the Cypress `cy.intercept('VERB', '/route', …)`
	// positional form. Group 1 = verb, group 2 = route literal.
	reCyIntercept = regexp.MustCompile(
		`\bcy\s*\.\s*intercept\s*\(\s*['"` + "`" + `]([A-Za-z]+)['"` + "`" + `]\s*,\s*['"` + "`" + `](/[^'"` + "`" + `$\n\r]*)['"` + "`" + `]`)

	// reCyObjVerbUrl matches the Cypress object-arg form used by BOTH
	// `cy.request({ method, url })` and `cy.intercept({ method, url })`:
	// a `method: 'VERB'` and a `url: '/route'` within the same options object.
	// Order-tolerant (method-before-url and url-before-method) via two patterns.
	reCyObjMethodFirst = regexp.MustCompile(
		`\bcy\s*\.\s*(?:request|intercept)\s*\(\s*\{[^}]{0,400}?\bmethod\s*:\s*['"` + "`" + `]([A-Za-z]+)['"` + "`" + `][^}]{0,400}?\burl\s*:\s*['"` + "`" + `](/[^'"` + "`" + `$\n\r]*)['"` + "`" + `]`)
	reCyObjURLFirst = regexp.MustCompile(
		`\bcy\s*\.\s*(?:request|intercept)\s*\(\s*\{[^}]{0,400}?\burl\s*:\s*['"` + "`" + `](/[^'"` + "`" + `$\n\r]*)['"` + "`" + `][^}]{0,400}?\bmethod\s*:\s*['"` + "`" + `]([A-Za-z]+)['"` + "`" + `]`)
)

func (e *jsTestRouteE2EExtractor) Extract(
	ctx context.Context,
	file extreg.FileInput,
) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	// TS/JS reuse the same custom prefix; gate to those source languages.
	switch file.Language {
	case "javascript", "typescript":
	default:
		return nil, nil
	}
	if !isBrowserE2ESpecFile(file.Path) {
		return nil, nil
	}
	src := string(file.Content)
	framework := detectBrowserE2EFramework(file.Path, src)
	if framework == "" {
		return nil, nil
	}
	routeCalls := collectBrowserE2ERouteCalls(src, framework)
	if len(routeCalls) == 0 {
		return nil, nil
	}
	rec := types.EntityRecord{
		Name:       "e2e_route_suite:" + browserE2ESpecBaseName(file.Path),
		Kind:       "SCOPE.Operation",
		Subtype:    "test_suite",
		SourceFile: file.Path,
		Language:   file.Language,
		StartLine:  1,
		EndLine:    1,
		Properties: map[string]string{
			"framework":       framework,
			"test_framework":  framework,
			"provenance":      "INFERRED_FROM_JS_BROWSER_E2E_ROUTE",
			"e2e_route_calls": strings.Join(routeCalls, "\n"),
		},
	}
	rec.ID = rec.ComputeID()
	return []types.EntityRecord{rec}, nil
}

// collectBrowserE2ERouteCalls returns the de-duplicated "VERB route" pairs a
// Playwright/Cypress spec drives by route string. Both framework idiom families
// are scanned unconditionally (a file rarely mixes them, and matching the other
// family's patterns simply finds nothing) so a misclassified framework hint
// never drops a real route hit.
func collectBrowserE2ERouteCalls(src, _ string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(verb, rawRoute string) {
		route := normaliseBrowserE2ERoute(rawRoute)
		if route == "" {
			return
		}
		v := strings.ToUpper(strings.TrimSpace(verb))
		if !isBrowserE2EVerb(v) {
			return
		}
		line := v + " " + route
		if seen[line] {
			return
		}
		seen[line] = true
		out = append(out, line)
	}

	// Playwright: request-context verb methods + apiContext.fetch({method}).
	for _, m := range reBrowserE2EVerbCall.FindAllStringSubmatch(src, -1) {
		add(m[1], m[2])
	}
	for _, m := range reBrowserE2EFetch.FindAllStringSubmatch(src, -1) {
		add(m[2], m[1]) // group 2 = method, group 1 = route
	}

	// Cypress: cy.request / cy.intercept positional + single + object forms.
	for _, m := range reCyRequestPositional.FindAllStringSubmatch(src, -1) {
		add(m[1], m[2])
	}
	for _, m := range reCyRequestSingle.FindAllStringSubmatch(src, -1) {
		add("GET", m[1]) // single-string cy.request defaults to GET
	}
	for _, m := range reCyIntercept.FindAllStringSubmatch(src, -1) {
		add(m[1], m[2])
	}
	for _, m := range reCyObjMethodFirst.FindAllStringSubmatch(src, -1) {
		add(m[1], m[2])
	}
	for _, m := range reCyObjURLFirst.FindAllStringSubmatch(src, -1) {
		add(m[2], m[1]) // group 1 = url, group 2 = method
	}
	return out
}

// browserE2EVerbs is the set of HTTP verbs the resolve pass understands.
var browserE2EVerbs = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "DELETE": true,
	"PATCH": true, "HEAD": true, "OPTIONS": true,
}

func isBrowserE2EVerb(v string) bool { return browserE2EVerbs[v] }

// normaliseBrowserE2ERoute reduces a raw route literal to a leading-slash path:
// strips a scheme+authority prefix (https://app/api/x → /api/x), drops a
// query/fragment tail, collapses repeated slashes. A literal with no static
// `/segment` (only scheme/host) is dropped. Path-param placeholders (`/:id`,
// `/{id}`) and casing are preserved (the resolver wildcards templates and
// compares case-insensitively).
func normaliseBrowserE2ERoute(raw string) string {
	p := strings.TrimSpace(raw)
	if p == "" {
		return ""
	}
	if i := strings.Index(p, "://"); i >= 0 {
		rest := p[i+3:]
		if slash := strings.IndexByte(rest, '/'); slash >= 0 {
			p = rest[slash:]
		} else {
			return ""
		}
	}
	if q := strings.IndexAny(p, "?#"); q >= 0 {
		p = p[:q]
	}
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		return ""
	}
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	if p == "/" {
		return ""
	}
	return strings.TrimRight(p, "/")
}

// detectBrowserE2EFramework returns "playwright", "cypress", or "" for a spec
// file. The path is the strongest signal (`*.cy.ts` / a `/cypress/` or
// `/e2e/`-with-playwright tree), with a content fallback on the framework's
// signature imports / globals. Returning "" means "not a recognised browser-e2e
// spec" and the extractor emits nothing.
func detectBrowserE2EFramework(path, src string) string {
	lp := strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
	switch {
	case strings.Contains(lp, ".cy."), strings.Contains(lp, "/cypress/"):
		return "cypress"
	}
	// Content signals — Cypress's `cy.` global / its config, Playwright's test
	// import / request fixture.
	if strings.Contains(src, "cy.request(") || strings.Contains(src, "cy.intercept(") ||
		strings.Contains(src, "from 'cypress'") || strings.Contains(src, "from \"cypress\"") {
		return "cypress"
	}
	if strings.Contains(src, "@playwright/test") ||
		strings.Contains(src, "playwright-core") ||
		strings.Contains(src, "page.request.") ||
		regexp.MustCompile(`\brequest\.(get|post|put|delete|patch|fetch)\s*\(`).MatchString(src) {
		return "playwright"
	}
	return ""
}

// isBrowserE2ESpecFile reports whether path looks like a Playwright/Cypress spec
// — a `*.spec.{ts,js,…}`, `*.test.{ts,js,…}`, `*.cy.{ts,js,…}`, `*.e2e.{ts,js,…}`
// `*.e2e-spec.ts`, or any file under a `/cypress/`, `/e2e/`, or `/tests/` tree.
// Keeps the route-hit suite off production code that merely calls an HTTP API.
func isBrowserE2ESpecFile(path string) bool {
	lp := strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
	if !hasJSTSExt(lp) {
		return false
	}
	base := lp
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	for _, marker := range []string{
		".cy.", ".spec.", ".test.", ".e2e.", ".e2e-spec.",
	} {
		if strings.Contains(base, marker) {
			return true
		}
	}
	return strings.Contains(lp, "/cypress/") ||
		strings.Contains(lp, "/e2e/") ||
		strings.Contains(lp, "/tests/") ||
		strings.Contains(lp, "/__tests__/")
}

// hasJSTSExt reports whether a lowercased path has a JS/TS source extension.
func hasJSTSExt(lp string) bool {
	for _, ext := range []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"} {
		if strings.HasSuffix(lp, ext) {
			return true
		}
	}
	return false
}

// browserE2ESpecBaseName derives a stable suite label from the spec path
// (`e2e/users.spec.ts` → `users.spec`).
func browserE2ESpecBaseName(path string) string {
	p := strings.ReplaceAll(path, "\\", "/")
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		p = p[i+1:]
	}
	for _, ext := range []string{".tsx", ".ts", ".jsx", ".js", ".mjs", ".cjs"} {
		if strings.HasSuffix(p, ext) {
			return strings.TrimSuffix(p, ext)
		}
	}
	return p
}
