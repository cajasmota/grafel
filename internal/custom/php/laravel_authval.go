// Package php — Laravel deep extraction: Auth, Validation, Middleware.
//
// Namespaced lrAuth / lrVal / lrMw to avoid collisions with laravel.go /
// frameworks.go.  Registers three extractors:
//
//	custom_php_laravel_auth       — auth_coverage (full)
//	custom_php_laravel_validation — dto_extraction + request_validation (full)
//	custom_php_laravel_middleware — middleware_coverage (full)
package php

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

// ---------------------------------------------------------------------------
// Auth regexes  (lrAuth prefix)
// ---------------------------------------------------------------------------

var (
	// ->middleware('auth') / ->middleware('auth:sanctum') / ->middleware('auth:api')
	// Also matches Route::middleware('auth:api') (static call using ::)
	lrAuthRouteMiddleware = regexp.MustCompile(
		`(?m)(?:->|::)middleware\s*\(\s*['"]auth(?::([a-zA-Z0-9_]+))?['"]`,
	)
	// ->middleware(['auth', 'auth:sanctum', ...]) — array form
	lrAuthRouteMiddlewareArray = regexp.MustCompile(
		`(?m)(?:->|::)middleware\s*\(\s*\[[^\]]*['"]auth(?::([a-zA-Z0-9_]+))?['"][^\]]*\]`,
	)

	// Gate::define('ability', ...)
	lrAuthGateDefine = regexp.MustCompile(
		`(?m)Gate::define\s*\(\s*['"]([^'"]+)['"]`,
	)
	// Gate::authorize('ability', $model)
	lrAuthGateAuthorize = regexp.MustCompile(
		`(?m)Gate::(?:authorize|allows|denies|check|inspect|any|none)\s*\(\s*['"]([^'"]+)['"]`,
	)
	// Gate::policy(Model::class, Policy::class)
	lrAuthGatePolicy = regexp.MustCompile(
		`(?m)Gate::policy\s*\(\s*(\w+)::class\s*,\s*(\w+)::class`,
	)

	// $this->authorize('update', $post)
	lrAuthControllerAuthorize = regexp.MustCompile(
		`(?m)\$this->authorize\s*\(\s*['"]([^'"]+)['"]`,
	)

	// Policy class: class PostPolicy { ... }  (must have policy action methods)
	lrAuthPolicyClass = regexp.MustCompile(
		`(?m)class\s+(\w+Policy)\s*(?:extends\s+\w+)?\s*\{`,
	)
	// Policy action methods (beyond the ones in laravel.go which are SCOPE.Pattern;
	// here we stamp them with the guard context)
	lrAuthPolicyMethod = regexp.MustCompile(
		`(?m)public\s+function\s+(viewAny|view|create|update|delete|restore|forceDelete|before)\s*\(`,
	)

	// @can('ability') Blade directive
	lrAuthBladeCan = regexp.MustCompile(
		`(?m)@can\s*\(\s*['"]([^'"]+)['"]`,
	)
	// @cannot('ability')
	lrAuthBladeCannotRaw = regexp.MustCompile(
		`(?m)@cannot\s*\(\s*['"]([^'"]+)['"]`,
	)

	// auth()->user() / auth()->check() / auth()->guard('api')
	lrAuthHelperCall = regexp.MustCompile(
		`(?m)\bauth\(\)\s*->(?:user|check|id|guard|login|logout|attempt|viaRemember)\s*\(`,
	)
	// Auth::guard('api') / Auth::user() / Auth::check()
	lrAuthFacade = regexp.MustCompile(
		`(?m)\bAuth::\s*(?:guard|user|check|id|login|logout|attempt|viaRemember)\s*\(`,
	)

	// @auth / @guest Blade sections
	lrAuthBladeSection = regexp.MustCompile(
		`(?m)@(auth|guest)\b`,
	)

	// HasApiTokens / Sanctum trait on User model
	lrAuthSanctumTrait = regexp.MustCompile(
		`(?m)use\s+(?:Laravel\\Sanctum\\HasApiTokens|HasApiTokens)\s*;`,
	)
	// Passport: use HasApiTokens (from Passport namespace)
	lrAuthPassportTrait = regexp.MustCompile(
		`(?m)use\s+(?:Laravel\\Passport\\HasApiTokens)\s*;`,
	)
)

// ---------------------------------------------------------------------------
// Auth extractor
// ---------------------------------------------------------------------------

func init() {
	extractor.Register("custom_php_laravel_auth", &lrAuthExtractor{})
}

type lrAuthExtractor struct{}

func (e *lrAuthExtractor) Language() string { return "custom_php_laravel_auth" }

func (e *lrAuthExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "indexer.laravel_auth_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "laravel"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "php" {
		return nil, nil
	}
	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if !seen[key] {
			seen[key] = true
			entities = append(entities, ent)
		}
	}

	// 1. Route-level auth middleware: ->middleware('auth') / 'auth:sanctum' / 'auth:api'
	for _, m := range lrAuthRouteMiddleware.FindAllStringSubmatchIndex(src, -1) {
		guard := "session" // default guard when no suffix
		if m[2] >= 0 {
			guard = src[m[2]:m[3]]
		}
		name := "auth:middleware:" + guard
		ent := makeEntity(name, "SCOPE.Pattern", "auth", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laravel",
			"provenance", "INFERRED_FROM_LARAVEL_AUTH_MIDDLEWARE",
			"mechanism", "middleware",
			"guard", guard,
			"protected_scope", "route")
		add(ent)
	}
	// Array form of middleware
	for _, m := range lrAuthRouteMiddlewareArray.FindAllStringSubmatchIndex(src, -1) {
		guard := "session"
		if m[2] >= 0 {
			guard = src[m[2]:m[3]]
		}
		name := "auth:middleware:" + guard
		ent := makeEntity(name, "SCOPE.Pattern", "auth", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laravel",
			"provenance", "INFERRED_FROM_LARAVEL_AUTH_MIDDLEWARE_ARRAY",
			"mechanism", "middleware",
			"guard", guard,
			"protected_scope", "route")
		add(ent)
	}

	// 2. Gate::define
	for _, m := range lrAuthGateDefine.FindAllStringSubmatchIndex(src, -1) {
		ability := src[m[2]:m[3]]
		name := "gate:define:" + ability
		ent := makeEntity(name, "SCOPE.Pattern", "auth", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laravel",
			"provenance", "INFERRED_FROM_LARAVEL_GATE_DEFINE",
			"mechanism", "gate",
			"ability", ability)
		add(ent)
	}

	// 3. Gate::authorize / Gate::allows / Gate::denies etc.
	for _, m := range lrAuthGateAuthorize.FindAllStringSubmatchIndex(src, -1) {
		ability := src[m[2]:m[3]]
		name := "gate:authorize:" + ability
		ent := makeEntity(name, "SCOPE.Pattern", "auth", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laravel",
			"provenance", "INFERRED_FROM_LARAVEL_GATE_AUTHORIZE",
			"mechanism", "gate",
			"ability", ability)
		add(ent)
	}

	// 4. Gate::policy(Model::class, PolicyClass::class)
	for _, m := range lrAuthGatePolicy.FindAllStringSubmatchIndex(src, -1) {
		model := src[m[2]:m[3]]
		policy := src[m[4]:m[5]]
		name := "gate:policy:" + model + ":" + policy
		ent := makeEntity(name, "SCOPE.Pattern", "auth", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laravel",
			"provenance", "INFERRED_FROM_LARAVEL_GATE_POLICY",
			"mechanism", "policy",
			"model", model,
			"policy_class", policy)
		add(ent)
	}

	// 5. $this->authorize('update', $post) in controllers
	for _, m := range lrAuthControllerAuthorize.FindAllStringSubmatchIndex(src, -1) {
		ability := src[m[2]:m[3]]
		name := "authorize:" + ability
		ent := makeEntity(name, "SCOPE.Pattern", "auth", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laravel",
			"provenance", "INFERRED_FROM_LARAVEL_CONTROLLER_AUTHORIZE",
			"mechanism", "policy",
			"ability", ability,
			"protected_scope", "controller_action")
		add(ent)
	}

	// 6. Policy class declaration: class PostPolicy
	for _, m := range lrAuthPolicyClass.FindAllStringSubmatchIndex(src, -1) {
		className := src[m[2]:m[3]]
		ent := makeEntity("policy_class:"+className, "SCOPE.Component", "auth", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laravel",
			"provenance", "INFERRED_FROM_LARAVEL_POLICY_CLASS",
			"mechanism", "policy",
			"policy_class", className)
		add(ent)

		// Extract policy action methods within the same file
		for _, pm := range lrAuthPolicyMethod.FindAllStringSubmatchIndex(src, -1) {
			method := src[pm[2]:pm[3]]
			mEnt := makeEntity("policy:"+className+":"+method, "SCOPE.Pattern", "auth", file.Path, file.Language, lineOf(src, pm[0]))
			setProps(&mEnt, "framework", "laravel",
				"provenance", "INFERRED_FROM_LARAVEL_POLICY_METHOD",
				"mechanism", "policy",
				"policy_class", className,
				"policy_action", method)
			add(mEnt)
		}
	}

	// 7. @can / @cannot Blade directives
	for _, m := range lrAuthBladeCan.FindAllStringSubmatchIndex(src, -1) {
		ability := src[m[2]:m[3]]
		name := "blade:can:" + ability
		ent := makeEntity(name, "SCOPE.Pattern", "auth", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laravel",
			"provenance", "INFERRED_FROM_LARAVEL_BLADE_CAN",
			"mechanism", "blade_directive",
			"ability", ability,
			"protected_scope", "view")
		add(ent)
	}
	for _, m := range lrAuthBladeCannotRaw.FindAllStringSubmatchIndex(src, -1) {
		ability := src[m[2]:m[3]]
		name := "blade:cannot:" + ability
		ent := makeEntity(name, "SCOPE.Pattern", "auth", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laravel",
			"provenance", "INFERRED_FROM_LARAVEL_BLADE_CANNOT",
			"mechanism", "blade_directive",
			"ability", ability,
			"protected_scope", "view")
		add(ent)
	}

	// 8. @auth / @guest Blade sections
	for _, m := range lrAuthBladeSection.FindAllStringSubmatchIndex(src, -1) {
		directive := src[m[2]:m[3]]
		name := "blade:" + directive
		ent := makeEntity(name, "SCOPE.Pattern", "auth", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laravel",
			"provenance", "INFERRED_FROM_LARAVEL_BLADE_SECTION",
			"mechanism", "blade_directive",
			"directive", directive)
		add(ent)
	}

	// 9. auth() helper calls
	if lrAuthHelperCall.MatchString(src) {
		ent := makeEntity("laravel:auth_helper", "SCOPE.Pattern", "auth", file.Path, file.Language, 1)
		setProps(&ent, "framework", "laravel",
			"provenance", "INFERRED_FROM_LARAVEL_AUTH_HELPER",
			"mechanism", "auth_helper")
		add(ent)
	}

	// 10. Auth:: facade
	if lrAuthFacade.MatchString(src) {
		ent := makeEntity("laravel:auth_facade", "SCOPE.Pattern", "auth", file.Path, file.Language, 1)
		setProps(&ent, "framework", "laravel",
			"provenance", "INFERRED_FROM_LARAVEL_AUTH_FACADE",
			"mechanism", "facade")
		add(ent)
	}

	// 11. Sanctum HasApiTokens trait
	if lrAuthSanctumTrait.MatchString(src) {
		ent := makeEntity("laravel:sanctum_has_api_tokens", "SCOPE.Pattern", "auth", file.Path, file.Language, 1)
		setProps(&ent, "framework", "laravel",
			"provenance", "INFERRED_FROM_LARAVEL_SANCTUM_TRAIT",
			"mechanism", "sanctum",
			"guard", "sanctum")
		add(ent)
	}

	// 12. Passport HasApiTokens trait
	if lrAuthPassportTrait.MatchString(src) {
		ent := makeEntity("laravel:passport_has_api_tokens", "SCOPE.Pattern", "auth", file.Path, file.Language, 1)
		setProps(&ent, "framework", "laravel",
			"provenance", "INFERRED_FROM_LARAVEL_PASSPORT_TRAIT",
			"mechanism", "passport",
			"guard", "api")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// Validation regexes  (lrVal prefix)
// ---------------------------------------------------------------------------

var (
	// class StorePostRequest extends FormRequest
	lrValFormRequestClass = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+extends\s+FormRequest\b`,
	)
	// rules() method body: return ['field' => 'rule1|rule2', ...]
	// We capture individual field => rules pairs inside a rules() return block.
	lrValRulesMethod = regexp.MustCompile(
		`(?m)public\s+function\s+rules\s*\(\s*\)\s*(?::\s*array\s*)?\{`,
	)
	// Individual field => 'required|max:255' inside rules()
	lrValRuleEntry = regexp.MustCompile(
		`(?m)['"]([a-zA-Z0-9_.*]+)['"]\s*=>\s*['"]([^'"]+)['"]`,
	)
	// Array rule: 'field' => ['required', Rule::in([...]), ...]
	lrValRuleEntryArray = regexp.MustCompile(
		`(?m)['"]([a-zA-Z0-9_.*]+)['"]\s*=>\s*\[`,
	)

	// $request->validate(['field' => 'rules', ...])
	lrValRequestValidate = regexp.MustCompile(
		`(?m)\$request->validate\s*\(\s*\[`,
	)
	// Inline rule pairs inside $request->validate([...])
	// (reuse lrValRuleEntry above)

	// Validator::make($data, $rules)
	lrValMake = regexp.MustCompile(
		`(?m)Validator::make\s*\(`,
	)
	// $this->validate($request, [...])  — controller shorthand
	lrValThisValidate = regexp.MustCompile(
		`(?m)\$this->validate\s*\(\s*\$\w+\s*,\s*\[`,
	)

	// After blocks: ->after(function ($validator) { ... })
	lrValAfterHook = regexp.MustCompile(
		`(?m)->after\s*\(\s*function`,
	)
	// withValidator hook in FormRequest
	lrValWithValidator = regexp.MustCompile(
		`(?m)public\s+function\s+withValidator\s*\(`,
	)
	// prepareForValidation hook (public or protected visibility)
	lrValPrepareForValidation = regexp.MustCompile(
		`(?m)(?:public|protected)\s+function\s+prepareForValidation\s*\(`,
	)
	// authorize() method in FormRequest
	lrValAuthorizeMethod = regexp.MustCompile(
		`(?m)public\s+function\s+authorize\s*\(\s*\)`,
	)
	// messages() method in FormRequest
	lrValMessagesMethod = regexp.MustCompile(
		`(?m)public\s+function\s+messages\s*\(\s*\)`,
	)
)

// lrValExtractRulesBlock extracts field => 'rule' pairs from the rules() method body.
// It finds the opening { of rules() and scans forward collecting pairs until
// a matching } is reached.
func lrValExtractRulesBlock(src string, rulesMethodStart int) []struct{ field, rules string } {
	// find the opening { after the match offset
	bodyStart := strings.Index(src[rulesMethodStart:], "{")
	if bodyStart < 0 {
		return nil
	}
	bodyStart += rulesMethodStart + 1 // skip past {

	// scan to matching }
	depth := 1
	i := bodyStart
	for i < len(src) && depth > 0 {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
		}
		i++
	}
	if depth != 0 {
		return nil
	}
	body := src[bodyStart : i-1]

	var pairs []struct{ field, rules string }
	for _, m := range lrValRuleEntry.FindAllStringSubmatchIndex(body, -1) {
		field := body[m[2]:m[3]]
		rules := body[m[4]:m[5]]
		pairs = append(pairs, struct{ field, rules string }{field, rules})
	}
	return pairs
}

// ---------------------------------------------------------------------------
// Validation extractor
// ---------------------------------------------------------------------------

func init() {
	extractor.Register("custom_php_laravel_validation", &lrValExtractor{})
}

type lrValExtractor struct{}

func (e *lrValExtractor) Language() string { return "custom_php_laravel_validation" }

func (e *lrValExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "indexer.laravel_validation_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "laravel"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "php" {
		return nil, nil
	}
	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if !seen[key] {
			seen[key] = true
			entities = append(entities, ent)
		}
	}

	// 1. FormRequest subclasses (dto_extraction)
	for _, m := range lrValFormRequestClass.FindAllStringSubmatchIndex(src, -1) {
		className := src[m[2]:m[3]]
		ent := makeEntity(className, "SCOPE.Component", "form_request", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laravel",
			"provenance", "INFERRED_FROM_LARAVEL_FORM_REQUEST_CLASS",
			"pattern_type", "form_request")
		add(ent)
	}

	// 2. rules() method — extract per-field validation rules
	for _, m := range lrValRulesMethod.FindAllStringSubmatchIndex(src, -1) {
		// Find parent class name from this file (look backwards from match position)
		classMatch := ""
		for _, cm := range lrValFormRequestClass.FindAllStringSubmatchIndex(src, -1) {
			if cm[0] <= m[0] {
				classMatch = src[cm[2]:cm[3]]
			}
		}
		pairs := lrValExtractRulesBlock(src, m[0])
		for _, p := range pairs {
			name := "validation_rule:" + p.field
			if classMatch != "" {
				name = "validation_rule:" + classMatch + ":" + p.field
			}
			ent := makeEntity(name, "SCOPE.Schema", "validation_rule", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "laravel",
				"provenance", "INFERRED_FROM_LARAVEL_FORM_REQUEST_RULES",
				"field", p.field,
				"rules", p.rules)
			if classMatch != "" {
				setProps(&ent, "form_request", classMatch)
			}
			add(ent)
		}
	}

	// 3. $request->validate([...]) — inline controller validation (request_validation)
	for _, m := range lrValRequestValidate.FindAllStringSubmatchIndex(src, -1) {
		// Mark the validation call site
		ent := makeEntity("laravel:request_validate", "SCOPE.Pattern", "validator", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laravel",
			"provenance", "INFERRED_FROM_LARAVEL_REQUEST_VALIDATE",
			"pattern_type", "inline_validation")
		add(ent)

		// Extract field => rules pairs from the array argument
		// Find opening [ after position
		arrayStart := strings.Index(src[m[0]:], "[")
		if arrayStart < 0 {
			continue
		}
		arrayStart += m[0] + 1
		// scan to closing ]
		depth := 1
		i := arrayStart
		for i < len(src) && depth > 0 {
			switch src[i] {
			case '[':
				depth++
			case ']':
				depth--
			}
			i++
		}
		if depth != 0 {
			continue
		}
		body := src[arrayStart : i-1]
		for _, pm := range lrValRuleEntry.FindAllStringSubmatchIndex(body, -1) {
			field := body[pm[2]:pm[3]]
			rules := body[pm[4]:pm[5]]
			name := "validation_rule:" + field
			rEnt := makeEntity(name, "SCOPE.Schema", "validation_rule", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&rEnt, "framework", "laravel",
				"provenance", "INFERRED_FROM_LARAVEL_REQUEST_VALIDATE_RULE",
				"field", field,
				"rules", rules,
				"pattern_type", "inline_validation")
			add(rEnt)
		}
	}

	// 4. Validator::make($data, $rules) (request_validation)
	if lrValMake.MatchString(src) {
		ent := makeEntity("laravel:validator_make", "SCOPE.Pattern", "validator", file.Path, file.Language, 1)
		setProps(&ent, "framework", "laravel",
			"provenance", "INFERRED_FROM_LARAVEL_VALIDATOR_MAKE",
			"pattern_type", "manual_validator")
		add(ent)
	}

	// 5. $this->validate($request, [...])
	if lrValThisValidate.MatchString(src) {
		ent := makeEntity("laravel:this_validate", "SCOPE.Pattern", "validator", file.Path, file.Language, 1)
		setProps(&ent, "framework", "laravel",
			"provenance", "INFERRED_FROM_LARAVEL_THIS_VALIDATE",
			"pattern_type", "controller_validate")
		add(ent)
	}

	// 6. ->after() hook
	if lrValAfterHook.MatchString(src) {
		ent := makeEntity("laravel:validator_after_hook", "SCOPE.Pattern", "validator", file.Path, file.Language, 1)
		setProps(&ent, "framework", "laravel",
			"provenance", "INFERRED_FROM_LARAVEL_VALIDATOR_AFTER_HOOK",
			"pattern_type", "validator_hook")
		add(ent)
	}

	// 7. withValidator hook
	if lrValWithValidator.MatchString(src) {
		ent := makeEntity("laravel:with_validator", "SCOPE.Pattern", "validator", file.Path, file.Language, 1)
		setProps(&ent, "framework", "laravel",
			"provenance", "INFERRED_FROM_LARAVEL_WITH_VALIDATOR",
			"pattern_type", "validator_hook")
		add(ent)
	}

	// 8. prepareForValidation hook
	if lrValPrepareForValidation.MatchString(src) {
		ent := makeEntity("laravel:prepare_for_validation", "SCOPE.Pattern", "validator", file.Path, file.Language, 1)
		setProps(&ent, "framework", "laravel",
			"provenance", "INFERRED_FROM_LARAVEL_PREPARE_FOR_VALIDATION",
			"pattern_type", "validator_hook")
		add(ent)
	}

	// 9. authorize() method in FormRequest (auth gate check)
	if lrValAuthorizeMethod.MatchString(src) && lrValFormRequestClass.MatchString(src) {
		ent := makeEntity("laravel:form_request_authorize", "SCOPE.Pattern", "auth", file.Path, file.Language, 1)
		setProps(&ent, "framework", "laravel",
			"provenance", "INFERRED_FROM_LARAVEL_FORM_REQUEST_AUTHORIZE",
			"mechanism", "policy",
			"pattern_type", "form_request_auth")
		add(ent)
	}

	// 10. messages() override in FormRequest
	if lrValMessagesMethod.MatchString(src) && lrValFormRequestClass.MatchString(src) {
		ent := makeEntity("laravel:form_request_messages", "SCOPE.Pattern", "validator", file.Path, file.Language, 1)
		setProps(&ent, "framework", "laravel",
			"provenance", "INFERRED_FROM_LARAVEL_FORM_REQUEST_MESSAGES",
			"pattern_type", "form_request_messages")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// Middleware regexes  (lrMw prefix)
// ---------------------------------------------------------------------------

var (
	// Kernel.php: protected $middleware = [...] / $middlewareGroups / $routeMiddleware / $middlewareAliases
	lrMwKernelGlobal = regexp.MustCompile(
		`(?m)protected\s+\$middleware\s*=\s*\[`,
	)
	lrMwKernelGroups = regexp.MustCompile(
		`(?m)protected\s+\$middlewareGroups\s*=\s*\[`,
	)
	lrMwKernelRoute = regexp.MustCompile(
		`(?m)protected\s+\$(?:routeMiddleware|middlewareAliases)\s*=\s*\[`,
	)
	// Class entries inside Kernel arrays: SomeMiddleware::class
	lrMwKernelEntry = regexp.MustCompile(
		`(?m)(\w+(?:\\\w+)*)::class`,
	)
	// String keys in routeMiddleware: 'auth' => \App\Http\Middleware\Auth::class
	// Class names may be fully-qualified with leading backslash + namespace separators
	lrMwRouteAlias = regexp.MustCompile(
		`(?m)['"]([a-zA-Z0-9_.:-]+)['"]\s*=>\s*([^:,\s]+)::class`,
	)

	// Custom middleware class: class X { public function handle($request, Closure $next) }
	lrMwHandleClass = regexp.MustCompile(
		`(?m)class\s+(\w+)\s*(?:extends\s+\w+\s*)?(?:implements\s+[\w\\,\s]+)?\{`,
	)
	// Matches both:
	//   handle($request, Closure $next)              — no type-hint on $request
	//   handle(Request $request, Closure $next)      — typed Request hint (common in Laravel)
	lrMwHandleMethod = regexp.MustCompile(
		`(?m)public\s+function\s+handle\s*\([^)]*Closure\s+\$\w+`,
	)
	// terminate() method on middleware
	lrMwTerminateMethod = regexp.MustCompile(
		`(?m)public\s+function\s+terminate\s*\(\s*\$\w+\s*,\s*\$\w+`,
	)

	// Route middleware attachment: ->middleware('name') / ->middleware('throttle:60,1')
	// Includes comma to support rate-limiter syntax like throttle:60,1
	lrMwRouteAttach = regexp.MustCompile(
		`(?m)->middleware\s*\(\s*['"]([a-zA-Z0-9_.,:-]+)['"]`,
	)
	lrMwRouteAttachArray = regexp.MustCompile(
		`(?m)->middleware\s*\(\s*\[([^\]]+)\]`,
	)
	lrMwRouteWithout = regexp.MustCompile(
		`(?m)->withoutMiddleware\s*\(\s*['"]?([a-zA-Z0-9_.:\\\s]+?)['"]?\s*\)`,
	)

	// Group middleware in route groups: ->middleware([...])
	lrMwGroupMiddleware = regexp.MustCompile(
		`(?m)Route::(?:group|middleware)\s*\(\s*(?:\[[^\]]*['"]middleware['"]\s*=>\s*\[?|['"]([a-zA-Z0-9_.:-]+)['"])`,
	)
)

// lrMwExtractArrayBlock returns the string content between opening [ and matching ].
func lrMwExtractArrayBlock(src string, start int) string {
	i := start
	for i < len(src) && src[i] != '[' {
		i++
	}
	if i >= len(src) {
		return ""
	}
	i++ // skip [
	depth := 1
	begin := i
	for i < len(src) && depth > 0 {
		switch src[i] {
		case '[':
			depth++
		case ']':
			depth--
		}
		i++
	}
	if depth != 0 {
		return ""
	}
	return src[begin : i-1]
}

// ---------------------------------------------------------------------------
// Middleware extractor
// ---------------------------------------------------------------------------

func init() {
	extractor.Register("custom_php_laravel_middleware", &lrMwExtractor{})
}

type lrMwExtractor struct{}

func (e *lrMwExtractor) Language() string { return "custom_php_laravel_middleware" }

func (e *lrMwExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "indexer.laravel_middleware_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "laravel"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "php" {
		return nil, nil
	}
	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if !seen[key] {
			seen[key] = true
			entities = append(entities, ent)
		}
	}

	// Helper to stamp class entries from a Kernel array block
	extractKernelEntries := func(block, stackKind string) {
		for _, em := range lrMwKernelEntry.FindAllStringSubmatchIndex(block, -1) {
			cls := block[em[2]:em[3]]
			// Skip common non-middleware tokens
			if cls == "class" || cls == "true" || cls == "false" || cls == "null" {
				continue
			}
			name := "kernel_middleware:" + stackKind + ":" + cls
			ent := makeEntity(name, "SCOPE.Pattern", "middleware", file.Path, file.Language, 1)
			setProps(&ent, "framework", "laravel",
				"provenance", "INFERRED_FROM_LARAVEL_KERNEL_MIDDLEWARE",
				"stack", stackKind,
				"middleware_class", cls)
			add(ent)
		}
	}

	// 1. Kernel $middleware global stack
	for _, m := range lrMwKernelGlobal.FindAllStringSubmatchIndex(src, -1) {
		block := lrMwExtractArrayBlock(src, m[0])
		extractKernelEntries(block, "global")
		if block != "" {
			ent := makeEntity("laravel:kernel_global_stack", "SCOPE.Pattern", "middleware", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "laravel",
				"provenance", "INFERRED_FROM_LARAVEL_KERNEL_GLOBAL",
				"stack", "global")
			add(ent)
		}
	}

	// 2. Kernel $middlewareGroups
	for _, m := range lrMwKernelGroups.FindAllStringSubmatchIndex(src, -1) {
		block := lrMwExtractArrayBlock(src, m[0])
		extractKernelEntries(block, "group")
		if block != "" {
			ent := makeEntity("laravel:kernel_middleware_groups", "SCOPE.Pattern", "middleware", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "laravel",
				"provenance", "INFERRED_FROM_LARAVEL_KERNEL_GROUPS",
				"stack", "group")
			add(ent)
		}
	}

	// 3. Kernel $routeMiddleware / $middlewareAliases
	for _, m := range lrMwKernelRoute.FindAllStringSubmatchIndex(src, -1) {
		block := lrMwExtractArrayBlock(src, m[0])
		if block != "" {
			// Extract alias => class pairs
			for _, am := range lrMwRouteAlias.FindAllStringSubmatchIndex(block, -1) {
				alias := block[am[2]:am[3]]
				cls := block[am[4]:am[5]]
				name := "route_middleware:" + alias
				ent := makeEntity(name, "SCOPE.Pattern", "middleware", file.Path, file.Language, lineOf(src, m[0]))
				setProps(&ent, "framework", "laravel",
					"provenance", "INFERRED_FROM_LARAVEL_ROUTE_MIDDLEWARE_ALIAS",
					"alias", alias,
					"middleware_class", cls,
					"stack", "route")
				add(ent)
			}
		}
	}

	// 4. Custom middleware class (has handle(Closure $next) method)
	if lrMwHandleMethod.MatchString(src) {
		for _, m := range lrMwHandleClass.FindAllStringSubmatchIndex(src, -1) {
			// Check handle method is within range of this class
			classStart := m[0]
			className := src[m[2]:m[3]]
			// Find next class start to delimit scope
			nextClass := len(src)
			for _, nm := range lrMwHandleClass.FindAllStringSubmatchIndex(src, -1) {
				if nm[0] > classStart {
					nextClass = nm[0]
					break
				}
			}
			classBody := src[classStart:nextClass]
			if lrMwHandleMethod.MatchString(classBody) {
				ent := makeEntity("middleware_class:"+className, "SCOPE.Component", "middleware", file.Path, file.Language, lineOf(src, m[0]))
				setProps(&ent, "framework", "laravel",
					"provenance", "INFERRED_FROM_LARAVEL_MIDDLEWARE_CLASS",
					"middleware_class", className)
				add(ent)

				// Check for terminate()
				if lrMwTerminateMethod.MatchString(classBody) {
					tEnt := makeEntity("middleware_terminate:"+className, "SCOPE.Pattern", "middleware", file.Path, file.Language, lineOf(src, m[0]))
					setProps(&tEnt, "framework", "laravel",
						"provenance", "INFERRED_FROM_LARAVEL_MIDDLEWARE_TERMINATE",
						"middleware_class", className)
					add(tEnt)
				}
			}
		}
	}

	// 5. Route ->middleware('name') attachments (non-auth ones)
	for _, m := range lrMwRouteAttach.FindAllStringSubmatchIndex(src, -1) {
		alias := src[m[2]:m[3]]
		// Skip pure 'auth*' — those are covered by auth extractor
		if strings.HasPrefix(alias, "auth") {
			continue
		}
		name := "route_apply_middleware:" + alias
		ent := makeEntity(name, "SCOPE.Pattern", "middleware", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laravel",
			"provenance", "INFERRED_FROM_LARAVEL_ROUTE_ATTACH_MIDDLEWARE",
			"alias", alias)
		add(ent)
	}

	// 6. Route ->middleware([...]) array form
	for _, m := range lrMwRouteAttachArray.FindAllStringSubmatchIndex(src, -1) {
		inner := src[m[2]:m[3]]
		// extract each quoted value
		reInner := regexp.MustCompile(`['"]([a-zA-Z0-9_.:-]+)['"]`)
		for _, im := range reInner.FindAllStringSubmatchIndex(inner, -1) {
			alias := inner[im[2]:im[3]]
			if strings.HasPrefix(alias, "auth") {
				continue
			}
			name := "route_apply_middleware:" + alias
			ent := makeEntity(name, "SCOPE.Pattern", "middleware", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "laravel",
				"provenance", "INFERRED_FROM_LARAVEL_ROUTE_ATTACH_MIDDLEWARE_ARRAY",
				"alias", alias)
			add(ent)
		}
	}

	// 7. ->withoutMiddleware(...)
	for _, m := range lrMwRouteWithout.FindAllStringSubmatchIndex(src, -1) {
		alias := strings.TrimSpace(src[m[2]:m[3]])
		name := "route_exclude_middleware:" + alias
		ent := makeEntity(name, "SCOPE.Pattern", "middleware", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laravel",
			"provenance", "INFERRED_FROM_LARAVEL_WITHOUT_MIDDLEWARE",
			"alias", alias)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
