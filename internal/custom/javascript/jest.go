package javascript

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extreg.Register("custom_js_jest", &jestExtractor{})
}

type jestExtractor struct{}

func (e *jestExtractor) Language() string { return "custom_js_jest" }

var (
	// describe / describe.only / describe.skip / describe.each / xdescribe / fdescribe
	reJestDescribe = regexp.MustCompile(
		`(?:^|[;\n])[ \t]*(?:x|f)?describe(?:\.(?:only|skip|concurrent|each\([^)]+\)))?\s*\(\s*(['` + "`" + `"][^'` + "`" + `"]+['` + "`" + `"])`,
	)
	// it / it.only / it.skip / it.todo / it.concurrent / test / test.only / etc.
	reJestTest = regexp.MustCompile(
		`(?:^|[;\n])[ \t]*(?:x|f)?(?:it|test)(?:\.(?:only|skip|todo|concurrent|each\([^)]+\)))?\s*\(\s*(['` + "`" + `"][^'` + "`" + `"]+['` + "`" + `"])`,
	)
	// beforeAll / afterAll / beforeEach / afterEach
	reJestHook = regexp.MustCompile(
		`(?:^|[;\n])[ \t]*(beforeAll|afterAll|beforeEach|afterEach)\s*\(`,
	)
	// jest.mock("module") / jest.spyOn(obj, "method")
	reJestMock = regexp.MustCompile(
		`jest\s*\.\s*(mock|spyOn|fn|useFakeTimers|useRealTimers|resetAllMocks|clearAllMocks)\s*\(`,
	)

	// ── TESTS-target resolution signals (issue #4343) ───────────────────────
	// Named imports: `import { A, B as C } from './x'`. We record the local
	// name (A, and the alias C) so a describe('A') / new A() inside the spec
	// can be linked to the imported production class.
	reTSNamedImport = regexp.MustCompile(
		`import\s+(?:type\s+)?\{([^}]*)\}\s+from\s+['"][^'"]+['"]`,
	)
	// Default import: `import Foo from './foo'`.
	reTSDefaultImport = regexp.MustCompile(
		`import\s+([A-Z][A-Za-z0-9_]*)\s+from\s+['"][^'"]+['"]`,
	)
	// `Test.createTestingModule({ controllers: [A], providers: [B] })` and
	// `TestBed.configureTestingModule({ ... })` — the symbols inside the
	// controllers/providers arrays are the units under test.
	reTestingModule = regexp.MustCompile(
		`(?:Test\.createTestingModule|TestBed\.configureTestingModule)\s*\(`,
	)
	reModuleArray = regexp.MustCompile(
		`(?:controllers|providers|declarations|components)\s*:\s*\[([^\]]*)\]`,
	)
	// `new Subject(` — direct instantiation of the unit under test.
	reNewInstance = regexp.MustCompile(`\bnew\s+([A-Z][A-Za-z0-9_]*)\s*\(`)
	// `app.get<Subject>(` / `module.get(Subject)` — DI resolution in a spec.
	reGetGeneric = regexp.MustCompile(`\.get<\s*([A-Z][A-Za-z0-9_]*)\s*>`)
	reGetArg     = regexp.MustCompile(`\.get\(\s*([A-Z][A-Za-z0-9_]*)\s*[),]`)
	// A bare TitleCase identifier (for matching describe labels against imports).
	reIdentToken = regexp.MustCompile(`^[A-Z][A-Za-z0-9_]*$`)

	// ── supertest e2e route-call resolution (issue #4351) ───────────────────
	// NestJS/Express e2e specs exercise the app through HTTP via supertest:
	//   request(app.getHttpServer()).post('/inspections/123/items').send(dto)
	//   request(httpServer).get(`${ROUTE}/${id}`)...
	// We capture the HTTP verb + the route argument so the resolve pass can
	// match (verb, normalized-route) against the synthesized
	// http_endpoint_definition entities and emit a TESTS edge from the e2e
	// suite to the endpoint(s) it covers. Without this, e2e suites link (at
	// best) to a class subject (#4343) and the endpoints they cover look
	// untested.
	//
	// reSupertestCall matches `request(<anything>).<verb>(<route-arg>` — the
	// route-arg group captures a single-quoted, double-quoted, or back-tick
	// (template-literal) string. We do NOT require the `request(` and the
	// verb call to be adjacent on the same line because specs frequently chain
	// across lines; instead we first find each `request(` opener, then scan a
	// bounded window for the first verb call. (See extractSupertestRouteCalls.)
	// The route argument is either a quoted/back-tick string OR a bare
	// identifier (e.g. `.post(ROUTE)`) that resolves via a local route const.
	reSupertestVerbCall = regexp.MustCompile(
		`\.(get|post|put|patch|delete|head|options)\s*\(\s*(` +
			"`" + `[^` + "`" + `]*` + "`" + `|'[^']*'|"[^"]*"|[A-Za-z_$][\w$]*)`,
	)
	// reRequestOpener locates a supertest invocation start. Both bare
	// `request(` and a `supertest(`-aliased form are recognised.
	reRequestOpener = regexp.MustCompile(`\b(?:request|supertest)\s*\(`)
	// reRouteConstDecl folds simple `const ROUTE = '/literal'` /
	// `const ROUTE = "/literal"` declarations so a `${ROUTE}/${id}` template in
	// a supertest call resolves to a concrete-ish path. Only literal string
	// initialisers are captured (no concatenation / function calls) to stay
	// conservative.
	reRouteConstDecl = regexp.MustCompile(
		`(?:^|[;\n])\s*(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*('[^']*'|"[^"]*")`,
	)
)

// Extract emits ONE linked test_suite entity per recognised unit-under-test in
// a Jest/Vitest spec file, carrying a TESTS edge to the production symbol, and
// folds the per-spec test_case / hook / mock counts into properties on that
// suite. It deliberately does NOT emit nested describes, individual it/test
// cases, hooks, or mock-setup calls as standalone entities: on a real NestJS
// codebase those dominate the orphan ring (thousands of edge-less nodes) while
// adding no graph value. See issue #4343.
//
// When no production target can be resolved (e.g. a pure integration spec that
// imports nothing under test), a single unlinked test_suite is still emitted
// per file so the spec remains discoverable — but the noise nodes are dropped
// either way, so the orphan blast radius collapses from O(describe+it+hook) to
// at most one node per spec.
func (e *jestExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.jest_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "jest"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	lang := strings.ToLower(file.Language)
	if lang != "typescript" && lang != "javascript" {
		return nil, nil
	}

	// No describe/it/test at all → not a Jest spec; emit nothing.
	describes := reJestDescribe.FindAllStringSubmatchIndex(src, -1)
	tests := reJestTest.FindAllStringSubmatchIndex(src, -1)
	hooks := reJestHook.FindAllStringSubmatchIndex(src, -1)
	mocks := reJestMock.FindAllStringSubmatchIndex(src, -1)
	if len(describes) == 0 && len(tests) == 0 {
		return nil, nil
	}

	// ── collect production-symbol candidates referenced in this spec ─────────
	imported := collectImportedSymbols(src)
	subjects := resolveTestSubjects(src, imported)

	// ── collect supertest e2e route calls (issue #4351) ──────────────────────
	routeCalls := extractSupertestRouteCalls(src)

	// Per-spec aggregate counts folded onto the suite(s).
	caseCount := len(tests)
	hookCount := len(hooks)
	mockCount := len(mocks)
	describeCount := len(describes)

	stripQuotes := func(s string) string {
		s = strings.TrimSpace(s)
		if len(s) >= 2 {
			s = s[1 : len(s)-1]
		}
		return s
	}

	// Suite display label: the first (top-level) describe label, falling back
	// to the file's basename. NOTE: the suite ENTITY name is namespaced (see
	// suiteEntityName) so it never collides with the production symbol of the
	// same name in the resolver's by-name index — otherwise both would blank
	// each other out and the TESTS stub would fail to resolve (#4343).
	suiteLabel := ""
	suiteLine := 1
	if len(describes) > 0 {
		suiteLabel = stripQuotes(src[describes[0][2]:describes[0][3]])
		suiteLine = lineOf(src, describes[0][0])
	} else {
		suiteLabel = baseSpecName(file.Path)
	}

	// One linked suite per spec file. Multiple resolved subjects (e.g. a spec
	// that exercises a controller and its service) all attach as TESTS edges on
	// the single suite node, so we never re-introduce per-subject orphans.
	ent := makeEntity(
		suiteEntityName(file.Path, suiteLabel),
		"SCOPE.Operation", "test_suite", file.Path, file.Language, suiteLine,
	)
	setProps(&ent, "framework", "jest",
		"provenance", "INFERRED_FROM_JEST_DESCRIBE",
		"suite_label", suiteLabel,
		"test_case_count", strconv.Itoa(caseCount),
		"nested_suite_count", strconv.Itoa(maxInt(describeCount-1, 0)),
		"hook_count", strconv.Itoa(hookCount),
		"mock_count", strconv.Itoa(mockCount),
	)
	// #4351 — stamp the supertest route calls so the resolve pass
	// (ResolveHTTPEndpointHandlers) can match (verb, normalized-route) against
	// the synthesized http_endpoint_definition entities and emit TESTS edges
	// from this e2e suite to the endpoints it covers. We attach the raw
	// "VERB route" pairs (one per line) as a single property rather than
	// emitting per-call entities, keeping the one-node-per-spec invariant from
	// #4343. The actual endpoint resolution is deferred to resolve-time because
	// only there is the cross-file http_endpoint_definition index available —
	// the controller defining the route usually lives in a different file than
	// the spec, and resolve-time resolution is merge-stable (it runs over the
	// fully-merged entity table, the same place the call→definition linkage
	// resolves).
	if len(routeCalls) > 0 {
		setProps(&ent, "e2e_route_calls", strings.Join(routeCalls, "\n"))
	}
	if len(subjects) > 0 {
		setProps(&ent, "tests_target", strings.Join(subjects, ","))
		for _, subj := range subjects {
			ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
				ToID: "Class:" + subj,
				Kind: string(types.RelationshipKindTests),
				Properties: map[string]string{
					"framework":    "jest",
					"match_source": "spec_subject_resolution",
					"target_type":  subj,
				},
				Confidence: 0.9,
			})
		}
	}

	span.SetAttributes(attribute.Int("entity_count", 1))
	return []types.EntityRecord{ent}, nil
}

// collectImportedSymbols returns the set of locally-bound names introduced by
// `import` statements in the spec (named, aliased, and default imports). These
// are the candidate production symbols a describe label / instantiation can
// resolve against.
func collectImportedSymbols(src string) map[string]bool {
	out := make(map[string]bool)
	for _, m := range reTSNamedImport.FindAllStringSubmatch(src, -1) {
		for _, part := range strings.Split(m[1], ",") {
			name := strings.TrimSpace(part)
			if name == "" {
				continue
			}
			// `A as B` → bind the local alias B (what the spec body references).
			if idx := strings.Index(name, " as "); idx >= 0 {
				name = strings.TrimSpace(name[idx+4:])
			}
			if reIdentToken.MatchString(name) {
				out[name] = true
			}
		}
	}
	for _, m := range reTSDefaultImport.FindAllStringSubmatch(src, -1) {
		if reIdentToken.MatchString(m[1]) {
			out[m[1]] = true
		}
	}
	return out
}

// resolveTestSubjects determines which imported production symbols are the
// unit(s) under test for this spec, de-duplicated and in priority order.
//
// HIGH-CONFIDENCE signals (these name the subject directly and are used first):
//
//  1. A top-level describe('Subject') whose label is an imported symbol.
//  2. Symbols listed in Test.createTestingModule({ controllers/providers: [...] }).
//
// LOW-CONFIDENCE fallback (used ONLY when 1+2 found nothing, to avoid binding
// the suite to helper-factory classes like `new User()` that specs construct
// for fixtures rather than test):
//
//  3. `new Subject(` instantiation of an imported symbol.
//  4. `.get<Subject>()` / `.get(Subject)` DI resolution of an imported symbol.
//
// Only symbols that were actually imported are returned, which keeps the TESTS
// edge pointed at an in-repo production entity (resolved by the symbol table as
// `Class:<Subject>`) rather than a framework/util name.
func resolveTestSubjects(src string, imported map[string]bool) []string {
	var ordered []string
	seen := make(map[string]bool)
	add := func(name string) bool {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] || !imported[name] {
			return false
		}
		seen[name] = true
		ordered = append(ordered, name)
		return true
	}

	// 1. describe('Subject') where Subject is imported.
	for _, m := range reJestDescribe.FindAllStringSubmatch(src, -1) {
		label := strings.TrimSpace(m[1])
		if len(label) >= 2 {
			label = label[1 : len(label)-1]
		}
		if reIdentToken.MatchString(label) {
			add(label)
		}
	}

	// 2. Test.createTestingModule({ controllers: [...], providers: [...] }).
	for _, loc := range reTestingModule.FindAllStringIndex(src, -1) {
		end := loc[1] + 600
		if end > len(src) {
			end = len(src)
		}
		window := src[loc[1]:end]
		for _, am := range reModuleArray.FindAllStringSubmatch(window, -1) {
			for _, tok := range strings.Split(am[1], ",") {
				tok = strings.TrimSpace(tok)
				if reIdentToken.MatchString(tok) {
					add(tok)
				}
			}
		}
	}

	// High-confidence signals won — do not widen to instantiation heuristics,
	// which would pull in fixture/helper classes.
	if len(ordered) > 0 {
		return ordered
	}

	// 3/4. Fallback: a single subject from instantiation / DI resolution. Take
	// only the FIRST such imported symbol to stay conservative.
	for _, m := range reNewInstance.FindAllStringSubmatch(src, -1) {
		if add(m[1]) {
			return ordered
		}
	}
	for _, m := range reGetGeneric.FindAllStringSubmatch(src, -1) {
		if add(m[1]) {
			return ordered
		}
	}
	for _, m := range reGetArg.FindAllStringSubmatch(src, -1) {
		if add(m[1]) {
			return ordered
		}
	}

	return ordered
}

// extractSupertestRouteCalls returns the de-duplicated set of "VERB route"
// pairs invoked through supertest in a spec, e.g. ["POST /api/v1/items",
// "GET /probe/buildings"]. The route is the raw argument string with quotes
// stripped and simple `${CONST}` template substitutions folded from local
// `const NAME = '/literal'` declarations; remaining `${expr}` params (path
// IDs) are left intact for the resolver's structural normalizer to wildcard.
//
// Matching strategy: for each `request(` / `supertest(` opener we scan a
// bounded window (the supertest chain rarely spans more than a few hundred
// bytes) for the FIRST verb call and capture its route argument. This keeps a
// `.set('Authorization', ...)` or `.send(dto)` in the chain from being
// mistaken for the route call, and tolerates the chain breaking across lines.
//
// Only the route ARGUMENT is interpreted — no edges are emitted here; the
// resolve pass owns endpoint matching so the linkage is merge-stable and
// reuses the same path-normalization the call→definition resolver uses.
func extractSupertestRouteCalls(src string) []string {
	consts := collectRouteConsts(src)

	seen := make(map[string]bool)
	var out []string
	openers := reRequestOpener.FindAllStringIndex(src, -1)
	for _, op := range openers {
		// Bounded forward window from the opener to find the verb call. 400
		// bytes comfortably covers a multi-line supertest chain without
		// bleeding into the next statement/test.
		end := op[1] + 400
		if end > len(src) {
			end = len(src)
		}
		window := src[op[0]:end]
		m := reSupertestVerbCall.FindStringSubmatch(window)
		if m == nil {
			continue
		}
		verb := strings.ToUpper(m[1])
		route := normalizeRouteArg(m[2], consts)
		if route == "" {
			continue
		}
		key := verb + " " + route
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out
}

// collectRouteConsts folds `const NAME = '/literal'` declarations into a
// name→value map so `${NAME}` template substitutions in supertest route
// arguments can be expanded. Only string-literal initialisers are recorded.
func collectRouteConsts(src string) map[string]string {
	out := map[string]string{}
	for _, m := range reRouteConstDecl.FindAllStringSubmatch(src, -1) {
		name := m[1]
		val := stripQuote(m[2])
		if name != "" && val != "" {
			out[name] = val
		}
	}
	return out
}

// normalizeRouteArg turns a captured supertest route argument (a quoted or
// back-tick string, including surrounding quotes) into a route path. Simple
// `${NAME}` substitutions resolve from the const map; unresolved `${...}`
// expressions are kept verbatim so the resolver's structural normalizer can
// wildcard them. Returns "" when the argument is not a usable route (e.g. an
// absolute URL to a third-party host, or an empty/relative-less string).
func normalizeRouteArg(arg string, consts map[string]string) string {
	arg = strings.TrimSpace(arg)
	// Bare identifier argument (e.g. `.post(ROUTE)`): resolve via the local
	// route-const map; if unknown it is too ambiguous to use — skip.
	if len(arg) > 0 && arg[0] != '\'' && arg[0] != '"' && arg[0] != '`' {
		if v, ok := consts[arg]; ok {
			return normalizeRouteArg("'"+v+"'", consts)
		}
		return ""
	}
	s := stripQuote(arg)
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Fold ${NAME} → const value when NAME is a known route const.
	if strings.Contains(s, "${") {
		s = reTemplateExpr.ReplaceAllStringFunc(s, func(match string) string {
			inner := reTemplateExpr.FindStringSubmatch(match)
			if len(inner) < 2 {
				return match
			}
			name := strings.TrimSpace(inner[1])
			if v, ok := consts[name]; ok {
				return v
			}
			return match
		})
	}
	// Collapse accidental duplicate slashes introduced by `${ROUTE}/...`
	// folding where ROUTE already ended in `/`.
	for strings.Contains(s, "//") && !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		s = strings.ReplaceAll(s, "//", "/")
	}
	// Only path-shaped routes are interpretable; an absolute URL to another
	// host is not an in-repo endpoint and must be skipped.
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return ""
	}
	if !strings.HasPrefix(s, "/") {
		// Supertest routes are server-relative and start with '/'. Anything
		// else (a bare path fragment, a variable that didn't fold) is too
		// ambiguous to resolve — skip conservatively.
		return ""
	}
	return s
}

// reTemplateExpr matches a `${expr}` substitution in a template literal.
var reTemplateExpr = regexp.MustCompile(`\$\{([^}]*)\}`)

// stripQuote removes a single layer of surrounding ', ", or ` from s.
func stripQuote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		q := s[0]
		if (q == '\'' || q == '"' || q == '`') && s[len(s)-1] == q {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// suiteEntityName namespaces a test_suite's entity name so it never collides
// with the production symbol of the same name (the describe label is usually
// the class under test). Without this, the resolver's by-name index would see
// two entities named e.g. "CreateNotificationService" — the spec suite and the
// real service — blank the slot as ambiguous, and the `Class:<Subject>` TESTS
// stub would fail to resolve, re-orphaning the test node (#4343). The
// human-readable label is preserved in Properties["suite_label"].
func suiteEntityName(path, label string) string {
	return "spec:" + baseSpecName(path) + ":" + label
}

// baseSpecName derives a human label from a spec file path, e.g.
// `src/foo/bar.service.spec.ts` → `bar.service`.
func baseSpecName(path string) string {
	p := path
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		p = p[i+1:]
	}
	for _, suf := range []string{".spec.ts", ".spec.tsx", ".spec.js", ".spec.jsx", ".test.ts", ".test.tsx", ".test.js", ".test.jsx"} {
		if strings.HasSuffix(p, suf) {
			return strings.TrimSuffix(p, suf)
		}
	}
	if i := strings.LastIndexByte(p, '.'); i >= 0 {
		return p[:i]
	}
	return p
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
