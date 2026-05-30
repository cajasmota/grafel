// Package php provides regex-based custom extractors for PHP source files.
// This file covers multi-framework routing/auth/middleware/validation/
// observability extraction for CakePHP, CodeIgniter, Drupal, Laminas,
// Lumen, Magento, Phalcon, Slim, WordPress, Yii, plus a cross-PHP
// type-system pass (PHP 8.1 enums via regex, interface stamps, type
// annotations) and a cross-PHP observability pass (log/metric/trace).
//
// Every extractor registers itself via init() and is keyed so the engine
// can activate it per-framework.
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
// Shared cross-PHP regexes
// ---------------------------------------------------------------------------

var (
	// Type System
	rePhpEnum = regexp.MustCompile(
		`(?m)^(?:(?:readonly|abstract|final)\s+)*enum\s+(\w+)`,
	)
	rePhpInterface = regexp.MustCompile(
		`(?m)^(?:(?:abstract|final)\s+)*interface\s+(\w+)`,
	)
	rePhpClass = regexp.MustCompile(
		`(?m)^(?:(?:readonly|abstract|final)\s+)*class\s+(\w+)`,
	)
	rePhpTypeAlias = regexp.MustCompile(
		`(?m)@(?:phpstan-type|psalm-type)\s+(\w+)(?:\s*=|\s+\w)`,
	)

	// Observability
	rePhpLog = regexp.MustCompile(
		`(?m)\b(?:Log::|(?:\$\w+\s*->)|error_log\s*\()(?:(?:debug|info|notice|warning|error|critical|alert|emergency)\s*\(|error_log\s*\()`,
	)
	rePhpLogSimple = regexp.MustCompile(
		`(?m)\b(?:error_log|syslog)\s*\(`,
	)
	rePhpMetric = regexp.MustCompile(
		`(?m)\b(?:StatsD|Prometheus|Datadog|increment|gauge|timing|histogram|counter)\s*[:(]`,
	)
	rePhpTrace = regexp.MustCompile(
		`(?m)\b(?:OpenTelemetry|OTel|Tracer|Span|startSpan|startActiveSpan|jaeger|zipkin|DDTrace)\b`,
	)

	// Auth patterns
	rePhpAuth = regexp.MustCompile(
		`(?m)\b(?:Auth::|auth\(\)|Authenticate|AuthMiddleware|IsAuthenticated|IsAuthorized|can\s*\(|Gate::|@auth\b|auth()->)\b`,
	)

	// Testing linkage
	rePhpTestClass = regexp.MustCompile(
		`(?m)class\s+\w+\s+extends\s+(?:TestCase|PHPUnit\\Framework\\TestCase|WebTestCase|KernelTestCase|Codeception\\Test\\Unit)\b`,
	)
	rePhpTestMethod = regexp.MustCompile(
		`(?m)(?:public\s+function\s+(test\w+)\s*\(|@test\s*\*/)`,
	)
)

// ---------------------------------------------------------------------------
// Cross-PHP type-system extractor (enum_extraction, interface_extraction,
// type_extraction, type_alias_extraction for all PHP frameworks)
// ---------------------------------------------------------------------------

func init() {
	extractor.Register("custom_php_typesystem", &phpTypeSystemExtractor{})
}

type phpTypeSystemExtractor struct{}

func (e *phpTypeSystemExtractor) Language() string { return "custom_php_typesystem" }

func (e *phpTypeSystemExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "indexer.php_typesystem_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
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
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// enum_extraction: PHP 8.1+ backed and pure enums
	for _, m := range rePhpEnum.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Schema", "enum", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "pattern_type", "enum", "provenance", "INFERRED_FROM_PHP_ENUM")
		add(ent)
	}

	// interface_extraction: PHP interface declarations
	for _, m := range rePhpInterface.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "interface", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "pattern_type", "interface", "provenance", "INFERRED_FROM_PHP_INTERFACE")
		add(ent)
	}

	// type_extraction: PHP class declarations (stamped with pattern_type)
	for _, m := range rePhpClass.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "class", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "pattern_type", "class", "provenance", "INFERRED_FROM_PHP_CLASS")
		add(ent)
	}

	// type_alias_extraction: @phpstan-type / @psalm-type docblock aliases
	for _, m := range rePhpTypeAlias.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Schema", "type_alias", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "pattern_type", "type_alias", "provenance", "INFERRED_FROM_PHP_TYPE_ALIAS")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// Cross-PHP observability extractor (log_extraction, metric_extraction,
// trace_extraction for all PHP frameworks)
// ---------------------------------------------------------------------------

func init() {
	extractor.Register("custom_php_observability", &phpObservabilityExtractor{})
}

type phpObservabilityExtractor struct{}

func (e *phpObservabilityExtractor) Language() string { return "custom_php_observability" }

func (e *phpObservabilityExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "indexer.php_observability_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
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
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	if rePhpLog.MatchString(src) || rePhpLogSimple.MatchString(src) {
		ent := makeEntity("php:logging", "SCOPE.Config", "observability", file.Path, file.Language, 1)
		setProps(&ent, "signal", "log", "provenance", "INFERRED_FROM_PHP_LOG_CALL")
		add(ent)
	}
	if rePhpMetric.MatchString(src) {
		ent := makeEntity("php:metrics", "SCOPE.Config", "observability", file.Path, file.Language, 1)
		setProps(&ent, "signal", "metric", "provenance", "INFERRED_FROM_PHP_METRIC_CALL")
		add(ent)
	}
	if rePhpTrace.MatchString(src) {
		ent := makeEntity("php:tracing", "SCOPE.Config", "observability", file.Path, file.Language, 1)
		setProps(&ent, "signal", "trace", "provenance", "INFERRED_FROM_PHP_TRACE_CALL")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// CakePHP extractor
// ---------------------------------------------------------------------------

func init() {
	extractor.Register("custom_php_cakephp", &cakephpExtractor{})
}

type cakephpExtractor struct{}

func (e *cakephpExtractor) Language() string { return "custom_php_cakephp" }

var (
	reCakePHPRoute = regexp.MustCompile(
		`(?m)\$routes->(?:connect|get|post|put|patch|delete|options)\s*\(\s*['"]([^'"]+)['"]`,
	)
	reCakePHPResourcesRoute = regexp.MustCompile(
		`(?m)\$routes->resources\s*\(\s*['"]([^'"]+)['"]`,
	)
	reCakePHPController = regexp.MustCompile(
		`(?m)class\s+(\w+Controller)\s+extends\s+(?:AppController|Controller)\b`,
	)
	reCakePHPMiddleware = regexp.MustCompile(
		`(?m)\$middlewareQueue->add\s*\(\s*new\s+(\w+)`,
	)
	reCakePHPAuth = regexp.MustCompile(
		`(?m)(?:\b(?:AuthenticationService|AuthorizationService|isAuthenticated|isAuthorized)\b|\$this->Authentication|\$this->Authorization)`,
	)
	reCakePHPValidation = regexp.MustCompile(
		`(?m)\$validator->(?:notEmptyString|requirePresence|add|allowEmpty|notEmpty)\s*\(\s*['"]([^'"]+)['"]`,
	)
	reCakePHPComponent = regexp.MustCompile(
		`(?m)public\s+\$components\s*=\s*\[([^\]]+)\]`,
	)
)

func (e *cakephpExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "indexer.cakephp_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "cakephp"),
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
		key := ent.Kind + ":" + ent.Name
		if !seen[key] {
			seen[key] = true
			entities = append(entities, ent)
		}
	}

	for _, m := range reCakePHPRoute.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		ent := makeEntity(path, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "cakephp", "provenance", "INFERRED_FROM_CAKEPHP_ROUTE", "route_path", path)
		add(ent)
	}
	for _, m := range reCakePHPResourcesRoute.FindAllStringSubmatchIndex(src, -1) {
		res := src[m[2]:m[3]]
		ent := makeEntity("resource:"+res, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "cakephp", "provenance", "INFERRED_FROM_CAKEPHP_RESOURCE", "resource", res)
		add(ent)
	}
	for _, m := range reCakePHPController.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "class", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "cakephp", "provenance", "INFERRED_FROM_CAKEPHP_CONTROLLER")
		add(ent)
	}
	for _, m := range reCakePHPMiddleware.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity("middleware:"+name, "SCOPE.Pattern", "middleware", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "cakephp", "provenance", "INFERRED_FROM_CAKEPHP_MIDDLEWARE")
		add(ent)
	}
	if reCakePHPAuth.MatchString(src) {
		ent := makeEntity("cakephp:auth", "SCOPE.Pattern", "auth", file.Path, file.Language, 1)
		setProps(&ent, "framework", "cakephp", "provenance", "INFERRED_FROM_CAKEPHP_AUTH")
		add(ent)
	}
	for _, m := range reCakePHPValidation.FindAllStringSubmatchIndex(src, -1) {
		field := src[m[2]:m[3]]
		ent := makeEntity("validate:"+field, "SCOPE.Component", "validator", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "cakephp", "provenance", "INFERRED_FROM_CAKEPHP_VALIDATION")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// CodeIgniter extractor
// ---------------------------------------------------------------------------

func init() {
	extractor.Register("custom_php_codeigniter", &codeigniterExtractor{})
}

type codeigniterExtractor struct{}

func (e *codeigniterExtractor) Language() string { return "custom_php_codeigniter" }

var (
	reCIRoute = regexp.MustCompile(
		`(?m)\$routes->(?:get|post|put|patch|delete|options|add|cli|match|resource|presenter)\s*\(\s*['"]([^'"]+)['"]`,
	)
	reCIController = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+extends\s+(?:BaseController|Controller|ResourceController|ResourcePresenter)\b`,
	)
	reCIFilter = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+implements\s+FilterInterface\b`,
	)
	reCIValidation = regexp.MustCompile(
		`(?m)\$this->validator->(?:setRules?|withRequest)\s*\(`,
	)
	reCISession = regexp.MustCompile(
		`(?m)\$this->session->(?:set_userdata|setFlashdata|has)\s*\(`,
	)
)

func (e *codeigniterExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "indexer.codeigniter_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "codeigniter"),
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
		key := ent.Kind + ":" + ent.Name
		if !seen[key] {
			seen[key] = true
			entities = append(entities, ent)
		}
	}

	for _, m := range reCIRoute.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		ent := makeEntity(path, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "codeigniter", "provenance", "INFERRED_FROM_CI_ROUTE", "route_path", path)
		add(ent)
	}
	for _, m := range reCIController.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "class", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "codeigniter", "provenance", "INFERRED_FROM_CI_CONTROLLER")
		add(ent)
	}
	for _, m := range reCIFilter.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity("filter:"+name, "SCOPE.Pattern", "middleware", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "codeigniter", "provenance", "INFERRED_FROM_CI_FILTER")
		add(ent)
	}
	if reCIValidation.MatchString(src) {
		ent := makeEntity("ci:validation", "SCOPE.Pattern", "validator", file.Path, file.Language, 1)
		setProps(&ent, "framework", "codeigniter", "provenance", "INFERRED_FROM_CI_VALIDATION")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// Drupal extractor
// ---------------------------------------------------------------------------

func init() {
	extractor.Register("custom_php_drupal", &drupalExtractor{})
}

type drupalExtractor struct{}

func (e *drupalExtractor) Language() string { return "custom_php_drupal" }

var (
	reDrupalRoute = regexp.MustCompile(
		`(?m)^(\w[\w.]+):\s*\n(?:\s+\w[^:\n]*:\s*[^\n]*\n)*\s+path:\s*['"]?([^'"\n]+)['"]?`,
	)
	reDrupalController = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+extends\s+ControllerBase\b`,
	)
	reDrupalAccess = regexp.MustCompile(
		`(?m)_permission:\s*['"]([^'"]+)['"]`,
	)
	reDrupalHook = regexp.MustCompile(
		`(?m)function\s+(\w+_(?:access|permission|form_alter|menu|init|boot|request))\s*\(`,
	)
	reDrupalPlugin = regexp.MustCompile(
		`(?m)#\[(?:Route|EventSubscriber|Plugin)\b`,
	)
)

func (e *drupalExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "indexer.drupal_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "drupal"),
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
		key := ent.Kind + ":" + ent.Name
		if !seen[key] {
			seen[key] = true
			entities = append(entities, ent)
		}
	}

	for _, m := range reDrupalController.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "class", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "drupal", "provenance", "INFERRED_FROM_DRUPAL_CONTROLLER")
		add(ent)
	}
	for _, m := range reDrupalAccess.FindAllStringSubmatchIndex(src, -1) {
		perm := src[m[2]:m[3]]
		ent := makeEntity("permission:"+perm, "SCOPE.Pattern", "auth", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "drupal", "provenance", "INFERRED_FROM_DRUPAL_PERMISSION")
		add(ent)
	}
	for _, m := range reDrupalHook.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity("hook:"+name, "SCOPE.Pattern", "hook", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "drupal", "provenance", "INFERRED_FROM_DRUPAL_HOOK")
		add(ent)
	}
	if reDrupalPlugin.MatchString(src) {
		ent := makeEntity("drupal:plugin", "SCOPE.Pattern", "plugin", file.Path, file.Language, 1)
		setProps(&ent, "framework", "drupal", "provenance", "INFERRED_FROM_DRUPAL_PLUGIN")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// Laminas extractor
// ---------------------------------------------------------------------------

func init() {
	extractor.Register("custom_php_laminas", &laminasExtractor{})
}

type laminasExtractor struct{}

func (e *laminasExtractor) Language() string { return "custom_php_laminas" }

var (
	reLaminasRoute = regexp.MustCompile(
		`(?m)(?:'route'|"route")\s*=>\s*['"]([^'"]+)['"]`,
	)
	reLaminasController = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+(?:extends\s+\w+\s+)?implements\s+DispatchableInterface\b`,
	)
	reLaminasMiddleware = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+implements\s+MiddlewareInterface\b`,
	)
	reLaminasInputFilter = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+extends\s+InputFilter\b`,
	)
	reLaminasACL = regexp.MustCompile(
		`(?m)\$acl->(?:addRole|allow|deny)\s*\(`,
	)
)

func (e *laminasExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "indexer.laminas_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "laminas"),
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
		key := ent.Kind + ":" + ent.Name
		if !seen[key] {
			seen[key] = true
			entities = append(entities, ent)
		}
	}

	for _, m := range reLaminasRoute.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		ent := makeEntity(path, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laminas", "provenance", "INFERRED_FROM_LAMINAS_ROUTE", "route_path", path)
		add(ent)
	}
	for _, m := range reLaminasController.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "class", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laminas", "provenance", "INFERRED_FROM_LAMINAS_CONTROLLER")
		add(ent)
	}
	for _, m := range reLaminasMiddleware.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity("middleware:"+name, "SCOPE.Pattern", "middleware", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laminas", "provenance", "INFERRED_FROM_LAMINAS_MIDDLEWARE")
		add(ent)
	}
	for _, m := range reLaminasInputFilter.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity("inputfilter:"+name, "SCOPE.Component", "validator", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laminas", "provenance", "INFERRED_FROM_LAMINAS_INPUT_FILTER")
		add(ent)
	}
	if reLaminasACL.MatchString(src) {
		ent := makeEntity("laminas:acl", "SCOPE.Pattern", "auth", file.Path, file.Language, 1)
		setProps(&ent, "framework", "laminas", "provenance", "INFERRED_FROM_LAMINAS_ACL")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// Lumen extractor (Laravel micro-framework)
// ---------------------------------------------------------------------------

func init() {
	extractor.Register("custom_php_lumen", &lumenExtractor{})
}

type lumenExtractor struct{}

func (e *lumenExtractor) Language() string { return "custom_php_lumen" }

var (
	reLumenRoute = regexp.MustCompile(
		`(?m)\$app->(?:get|post|put|patch|delete|options|addRoute)\s*\(\s*['"]([^'"]+)['"]`,
	)
	reLumenRouter = regexp.MustCompile(
		`(?m)\$router->(?:get|post|put|patch|delete|options|group)\s*\(\s*['"]([^'"]+)['"]`,
	)
	reLumenGroup = regexp.MustCompile(
		`(?m)\$(?:app|router)->group\s*\(\s*\[`,
	)
	reLumenMiddleware = regexp.MustCompile(
		`(?m)\$app->middleware\s*\(\s*\[|->middleware\s*\(\s*['"](\w+)['"]`,
	)
	reLumenAuth = regexp.MustCompile(
		`(?m)\b(?:Auth::|auth\(\)|Illuminate\\Auth|HasApiTokens)\b`,
	)
	reLumenValidation = regexp.MustCompile(
		`(?m)\$this->validate\s*\(\s*\$request\s*,\s*\[`,
	)
)

func (e *lumenExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "indexer.lumen_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "lumen"),
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
		key := ent.Kind + ":" + ent.Name
		if !seen[key] {
			seen[key] = true
			entities = append(entities, ent)
		}
	}

	for _, m := range reLumenRoute.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		ent := makeEntity(path, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "lumen", "provenance", "INFERRED_FROM_LUMEN_ROUTE", "route_path", path)
		add(ent)
	}
	for _, m := range reLumenRouter.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		ent := makeEntity(path, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "lumen", "provenance", "INFERRED_FROM_LUMEN_ROUTER_ROUTE", "route_path", path)
		add(ent)
	}
	if reLumenGroup.MatchString(src) {
		ent := makeEntity("lumen:route_group", "SCOPE.Pattern", "", file.Path, file.Language, 1)
		setProps(&ent, "framework", "lumen", "provenance", "INFERRED_FROM_LUMEN_GROUP")
		add(ent)
	}
	if reLumenMiddleware.MatchString(src) {
		ent := makeEntity("lumen:middleware", "SCOPE.Pattern", "middleware", file.Path, file.Language, 1)
		setProps(&ent, "framework", "lumen", "provenance", "INFERRED_FROM_LUMEN_MIDDLEWARE")
		add(ent)
	}
	if reLumenAuth.MatchString(src) {
		ent := makeEntity("lumen:auth", "SCOPE.Pattern", "auth", file.Path, file.Language, 1)
		setProps(&ent, "framework", "lumen", "provenance", "INFERRED_FROM_LUMEN_AUTH")
		add(ent)
	}
	if reLumenValidation.MatchString(src) {
		ent := makeEntity("lumen:validation", "SCOPE.Pattern", "validator", file.Path, file.Language, 1)
		setProps(&ent, "framework", "lumen", "provenance", "INFERRED_FROM_LUMEN_VALIDATION")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// Magento extractor
// ---------------------------------------------------------------------------

func init() {
	extractor.Register("custom_php_magento", &magentoExtractor{})
}

type magentoExtractor struct{}

func (e *magentoExtractor) Language() string { return "custom_php_magento" }

var (
	reMagentoRouteXml = regexp.MustCompile(
		`(?m)<route\s+id=['"]([^'"]+)['"]\s+frontName=['"]([^'"]+)['"]`,
	)
	reMagentoControllerPath = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+extends\s+(?:Action|AbstractController|Magento\\Framework\\App\\Action\\Action)\b`,
	)
	reMagentoPlugin = regexp.MustCompile(
		`(?m)<type\s+name=['"]([^'"]+)['"]>[\s\S]*?<plugin\s+name=['"]([^'"]+)['"]`,
	)
	reMagentoObserver = regexp.MustCompile(
		`(?m)<event\s+name=['"]([^'"]+)['"]>[\s\S]*?<observer\s+name=['"]([^'"]+)['"]`,
	)
	reMagentoPreference = regexp.MustCompile(
		`(?m)<preference\s+for=['"]([^'"]+)['"]\s+type=['"]([^'"]+)['"]`,
	)
	reMagentoACL = regexp.MustCompile(
		`(?m)<resource\s+id=['"]([^'"]+)['"]`,
	)
)

func (e *magentoExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "indexer.magento_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "magento"),
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
		key := ent.Kind + ":" + ent.Name
		if !seen[key] {
			seen[key] = true
			entities = append(entities, ent)
		}
	}

	for _, m := range reMagentoRouteXml.FindAllStringSubmatchIndex(src, -1) {
		frontName := src[m[4]:m[5]]
		ent := makeEntity("/"+frontName, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "magento", "provenance", "INFERRED_FROM_MAGENTO_ROUTE", "route_path", "/"+frontName)
		add(ent)
	}
	for _, m := range reMagentoControllerPath.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "class", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "magento", "provenance", "INFERRED_FROM_MAGENTO_CONTROLLER")
		add(ent)
	}
	for _, m := range reMagentoPlugin.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[4]:m[5]]
		ent := makeEntity("plugin:"+name, "SCOPE.Pattern", "middleware", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "magento", "provenance", "INFERRED_FROM_MAGENTO_PLUGIN")
		add(ent)
	}
	for _, m := range reMagentoACL.FindAllStringSubmatchIndex(src, -1) {
		resource := src[m[2]:m[3]]
		ent := makeEntity("acl:"+resource, "SCOPE.Pattern", "auth", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "magento", "provenance", "INFERRED_FROM_MAGENTO_ACL")
		add(ent)
	}
	for _, m := range reMagentoObserver.FindAllStringSubmatchIndex(src, -1) {
		event := src[m[2]:m[3]]
		ent := makeEntity("event:"+event, "SCOPE.Pattern", "hook", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "magento", "provenance", "INFERRED_FROM_MAGENTO_OBSERVER")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// Phalcon extractor
// ---------------------------------------------------------------------------

func init() {
	extractor.Register("custom_php_phalcon", &phalconExtractor{})
}

type phalconExtractor struct{}

func (e *phalconExtractor) Language() string { return "custom_php_phalcon" }

var (
	rePhalconRoute = regexp.MustCompile(
		`(?m)\$(?:app|router)->(?:get|post|put|patch|delete|options|add|mount)\s*\(\s*['"]([^'"]+)['"]`,
	)
	rePhalconGroup = regexp.MustCompile(
		`(?m)new\s+Group\s*\(\s*['"]([^'"]+)['"]`,
	)
	rePhalconController = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+extends\s+(?:AbstractController|Controller|ControllerBase)\b`,
	)
	rePhalconMiddleware = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+implements\s+(?:MiddlewareInterface|BeforeMiddleware|AfterMiddleware)\b`,
	)
	rePhalconAuth = regexp.MustCompile(
		`(?m)(?:\b(?:Auth|Acl|Phalcon\\Acl|Phalcon\\Security)\b|\$this->auth)`,
	)
	rePhalconValidation = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+extends\s+Validation\b`,
	)
)

func (e *phalconExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "indexer.phalcon_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "phalcon"),
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
		key := ent.Kind + ":" + ent.Name
		if !seen[key] {
			seen[key] = true
			entities = append(entities, ent)
		}
	}

	for _, m := range rePhalconRoute.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		ent := makeEntity(path, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "phalcon", "provenance", "INFERRED_FROM_PHALCON_ROUTE", "route_path", path)
		add(ent)
	}
	for _, m := range rePhalconGroup.FindAllStringSubmatchIndex(src, -1) {
		prefix := src[m[2]:m[3]]
		ent := makeEntity("group:"+prefix, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "phalcon", "provenance", "INFERRED_FROM_PHALCON_GROUP", "prefix", prefix)
		add(ent)
	}
	for _, m := range rePhalconController.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "class", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "phalcon", "provenance", "INFERRED_FROM_PHALCON_CONTROLLER")
		add(ent)
	}
	for _, m := range rePhalconMiddleware.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity("middleware:"+name, "SCOPE.Pattern", "middleware", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "phalcon", "provenance", "INFERRED_FROM_PHALCON_MIDDLEWARE")
		add(ent)
	}
	if rePhalconAuth.MatchString(src) {
		ent := makeEntity("phalcon:auth", "SCOPE.Pattern", "auth", file.Path, file.Language, 1)
		setProps(&ent, "framework", "phalcon", "provenance", "INFERRED_FROM_PHALCON_AUTH")
		add(ent)
	}
	for _, m := range rePhalconValidation.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity("validation:"+name, "SCOPE.Component", "validator", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "phalcon", "provenance", "INFERRED_FROM_PHALCON_VALIDATION")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// Slim Framework extractor
// ---------------------------------------------------------------------------

func init() {
	extractor.Register("custom_php_slim", &slimExtractor{})
}

type slimExtractor struct{}

func (e *slimExtractor) Language() string { return "custom_php_slim" }

var (
	reSlimRoute = regexp.MustCompile(
		`(?m)\$app->(?:get|post|put|patch|delete|options|any|map|group)\s*\(\s*['"]([^'"]+)['"]`,
	)
	reSlimGroup = regexp.MustCompile(
		`(?m)\$app->group\s*\(\s*['"]([^'"]+)['"]`,
	)
	reSlimMiddleware = regexp.MustCompile(
		`(?m)\$app->add\s*\(\s*(?:new\s+)?(\w+)`,
	)
	reSlimAuth = regexp.MustCompile(
		`(?m)\b(?:JwtAuthentication|BasicAuthentication|BearerAuthentication|OAuth|Authenticate)\b`,
	)
	reSlimValidation = regexp.MustCompile(
		`(?m)\b(?:respect\\Validation|RespectValidation|Validator|validateInput)\b`,
	)
)

func (e *slimExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "indexer.slim_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "slim"),
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
		key := ent.Kind + ":" + ent.Name
		if !seen[key] {
			seen[key] = true
			entities = append(entities, ent)
		}
	}

	for _, m := range reSlimRoute.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		ent := makeEntity(path, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "slim", "provenance", "INFERRED_FROM_SLIM_ROUTE", "route_path", path)
		add(ent)
	}
	for _, m := range reSlimGroup.FindAllStringSubmatchIndex(src, -1) {
		prefix := src[m[2]:m[3]]
		ent := makeEntity("group:"+prefix, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "slim", "provenance", "INFERRED_FROM_SLIM_GROUP", "prefix", prefix)
		add(ent)
	}
	for _, m := range reSlimMiddleware.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity("middleware:"+name, "SCOPE.Pattern", "middleware", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "slim", "provenance", "INFERRED_FROM_SLIM_MIDDLEWARE")
		add(ent)
	}
	if reSlimAuth.MatchString(src) {
		ent := makeEntity("slim:auth", "SCOPE.Pattern", "auth", file.Path, file.Language, 1)
		setProps(&ent, "framework", "slim", "provenance", "INFERRED_FROM_SLIM_AUTH")
		add(ent)
	}
	if reSlimValidation.MatchString(src) {
		ent := makeEntity("slim:validation", "SCOPE.Pattern", "validator", file.Path, file.Language, 1)
		setProps(&ent, "framework", "slim", "provenance", "INFERRED_FROM_SLIM_VALIDATION")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// WordPress extractor
// ---------------------------------------------------------------------------

func init() {
	extractor.Register("custom_php_wordpress", &wordpressExtractor{})
}

type wordpressExtractor struct{}

func (e *wordpressExtractor) Language() string { return "custom_php_wordpress" }

var (
	reWPRestRoute = regexp.MustCompile(
		`(?m)register_rest_route\s*\(\s*['"]([^'"]+)['"]\s*,\s*['"]([^'"]+)['"]`,
	)
	reWPAddAction = regexp.MustCompile(
		`(?m)add_action\s*\(\s*['"]([^'"]+)['"]`,
	)
	reWPAddFilter = regexp.MustCompile(
		`(?m)add_filter\s*\(\s*['"]([^'"]+)['"]`,
	)
	reWPCurrentUserCan = regexp.MustCompile(
		`(?m)current_user_can\s*\(\s*['"]([^'"]+)['"]`,
	)
	reWPNonce = regexp.MustCompile(
		`(?m)\b(?:wp_nonce_field|wp_verify_nonce|check_admin_referer|wp_create_nonce)\s*\(`,
	)
	reWPShortcode = regexp.MustCompile(
		`(?m)add_shortcode\s*\(\s*['"]([^'"]+)['"]`,
	)
	reWPCustomPostType = regexp.MustCompile(
		`(?m)register_post_type\s*\(\s*['"]([^'"]+)['"]`,
	)
	reWPBlock = regexp.MustCompile(
		`(?m)register_block_type\s*\(\s*['"]([^'"]+)['"]`,
	)
)

func (e *wordpressExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "indexer.wordpress_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "wordpress"),
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
		key := ent.Kind + ":" + ent.Name
		if !seen[key] {
			seen[key] = true
			entities = append(entities, ent)
		}
	}

	for _, m := range reWPRestRoute.FindAllStringSubmatchIndex(src, -1) {
		namespace := src[m[2]:m[3]]
		path := src[m[4]:m[5]]
		fullPath := "/" + strings.Trim(namespace, "/") + "/" + strings.Trim(path, "/")
		ent := makeEntity(fullPath, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "wordpress", "provenance", "INFERRED_FROM_WP_REST_ROUTE",
			"route_path", fullPath, "namespace", namespace)
		add(ent)
	}
	for _, m := range reWPAddAction.FindAllStringSubmatchIndex(src, -1) {
		hook := src[m[2]:m[3]]
		ent := makeEntity("action:"+hook, "SCOPE.Pattern", "hook", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "wordpress", "provenance", "INFERRED_FROM_WP_ACTION")
		add(ent)
	}
	for _, m := range reWPAddFilter.FindAllStringSubmatchIndex(src, -1) {
		hook := src[m[2]:m[3]]
		ent := makeEntity("filter:"+hook, "SCOPE.Pattern", "hook", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "wordpress", "provenance", "INFERRED_FROM_WP_FILTER")
		add(ent)
	}
	for _, m := range reWPCurrentUserCan.FindAllStringSubmatchIndex(src, -1) {
		cap := src[m[2]:m[3]]
		ent := makeEntity("capability:"+cap, "SCOPE.Pattern", "auth", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "wordpress", "provenance", "INFERRED_FROM_WP_CAPABILITY")
		add(ent)
	}
	if reWPNonce.MatchString(src) {
		ent := makeEntity("wp:nonce", "SCOPE.Pattern", "auth", file.Path, file.Language, 1)
		setProps(&ent, "framework", "wordpress", "provenance", "INFERRED_FROM_WP_NONCE")
		add(ent)
	}
	for _, m := range reWPShortcode.FindAllStringSubmatchIndex(src, -1) {
		tag := src[m[2]:m[3]]
		ent := makeEntity("shortcode:"+tag, "SCOPE.Pattern", "hook", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "wordpress", "provenance", "INFERRED_FROM_WP_SHORTCODE")
		add(ent)
	}
	for _, m := range reWPCustomPostType.FindAllStringSubmatchIndex(src, -1) {
		pt := src[m[2]:m[3]]
		ent := makeEntity("post_type:"+pt, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "wordpress", "provenance", "INFERRED_FROM_WP_POST_TYPE")
		add(ent)
	}
	for _, m := range reWPBlock.FindAllStringSubmatchIndex(src, -1) {
		block := src[m[2]:m[3]]
		ent := makeEntity("block:"+block, "SCOPE.UIComponent", "component", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "wordpress", "provenance", "INFERRED_FROM_WP_BLOCK")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// Yii Framework extractor
// ---------------------------------------------------------------------------

func init() {
	extractor.Register("custom_php_yii", &yiiExtractor{})
}

type yiiExtractor struct{}

func (e *yiiExtractor) Language() string { return "custom_php_yii" }

var (
	reYiiRoute = regexp.MustCompile(
		`(?m)['"]([a-zA-Z0-9_/-]+/[a-zA-Z0-9_/-]+)['"]\s*=>\s*['"]([a-zA-Z0-9_/-]+)['"]`,
	)
	reYiiUrlRule = regexp.MustCompile(
		`(?m)['"]pattern['"]\s*=>\s*['"]([^'"]+)['"]`,
	)
	reYiiController = regexp.MustCompile(
		`(?m)class\s+(\w+Controller)\s+extends\s+(?:Controller|ActiveController|RestController)\b`,
	)
	reYiiBehavior = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+extends\s+Behavior\b`,
	)
	reYiiFilter = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+extends\s+ActionFilter\b`,
	)
	reYiiAuth = regexp.MustCompile(
		`(?m)\b(?:Yii::app\(\)->user|Yii::\$app->user|checkAccess|can\s*\(|AccessControl|HttpBearerAuth|QueryParamAuth)\b`,
	)
	reYiiValidator = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+extends\s+Validator\b`,
	)
	reYiiModel = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+extends\s+(?:Model|ActiveRecord|FormModel)\b`,
	)
)

func (e *yiiExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "indexer.yii_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "yii"),
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
		key := ent.Kind + ":" + ent.Name
		if !seen[key] {
			seen[key] = true
			entities = append(entities, ent)
		}
	}

	for _, m := range reYiiUrlRule.FindAllStringSubmatchIndex(src, -1) {
		pattern := src[m[2]:m[3]]
		ent := makeEntity(pattern, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "yii", "provenance", "INFERRED_FROM_YII_URL_RULE", "route_path", pattern)
		add(ent)
	}
	for _, m := range reYiiController.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "class", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "yii", "provenance", "INFERRED_FROM_YII_CONTROLLER")
		add(ent)
	}
	for _, m := range reYiiFilter.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity("filter:"+name, "SCOPE.Pattern", "middleware", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "yii", "provenance", "INFERRED_FROM_YII_FILTER")
		add(ent)
	}
	if reYiiAuth.MatchString(src) {
		ent := makeEntity("yii:auth", "SCOPE.Pattern", "auth", file.Path, file.Language, 1)
		setProps(&ent, "framework", "yii", "provenance", "INFERRED_FROM_YII_AUTH")
		add(ent)
	}
	for _, m := range reYiiValidator.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity("validator:"+name, "SCOPE.Component", "validator", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "yii", "provenance", "INFERRED_FROM_YII_VALIDATOR")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// Unused import guard: strings is used in WordPress extractor.
// ---------------------------------------------------------------------------
var _ = strings.Trim
