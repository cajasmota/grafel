package python

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("python_django", &DjangoExtractor{})
}

// DjangoExtractor extracts Django framework patterns: URL patterns, CBVs,
// signal receivers, admin registrations, DRF serializers/viewsets, Celery
// tasks, middleware, template tags, management commands, and model managers.
type DjangoExtractor struct{}

func (e *DjangoExtractor) Language() string { return "python_django" }

// Regex patterns.
var (
	djangoPathCallRe = regexp.MustCompile(
		`(?:re_)?path\s*\(\s*(?:r)?["']([^"']*)["']\s*,\s*([\w.]+)`)
	djangoPathNameRe      = regexp.MustCompile(`name\s*=\s*["']([^"']+)["']`)
	djangoIncludeRe       = regexp.MustCompile(`include\s*\(\s*["']([^"']+)["']`)
	djangoCBVClassRe      = regexp.MustCompile(`(?m)^class\s+([A-Z][A-Za-z0-9_]*)\s*\(([^)]*(?:View|Mixin|APIView|ViewSet)[^)]*)\)\s*:`)
	djangoCBVMethodRe     = regexp.MustCompile(`(?m)^\s{4,}def\s+(get|post|put|patch|delete|head|options|trace)\s*\(\s*self`)
	djangoReceiverRe      = regexp.MustCompile(`(?m)@receiver\s*\(\s*([\w.]+)(?:\s*,\s*sender\s*=\s*(\w+))?[^)]*\)\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	djangoAdminRegRe      = regexp.MustCompile(`(?m)admin\.site\.register\s*\(\s*(\w+)(?:\s*,\s*(\w+))?\s*\)`)
	djangoAdminDecorRe    = regexp.MustCompile(`(?m)@admin\.register\s*\(\s*(\w+)\s*\)\s*\n\s*class\s+(\w+)\s*\(`)
	djangoDRFSerializerRe = regexp.MustCompile(
		`(?m)^class\s+([A-Z][A-Za-z0-9_]*)\s*\([^)]*(?:serializers\.)?(?:ModelSerializer|HyperlinkedModelSerializer|Serializer|ListSerializer)[^)]*\)\s*:`)
	djangoDRFViewsetRe = regexp.MustCompile(
		`(?m)^class\s+([A-Z][A-Za-z0-9_]*)\s*\([^)]*(?:viewsets\.)?(?:ModelViewSet|ReadOnlyModelViewSet|ViewSet|GenericViewSet|ViewSetMixin)[^)]*\)\s*:`)
	djangoRouterRegRe = regexp.MustCompile(
		`(?m)router\.register\s*\(\s*(?:r)?["']([^"']*)["']\s*,\s*(\w+)`)
	djangoCeleryTaskRe = regexp.MustCompile(
		`(?m)@(?:shared_task|(?:\w+\.)?task)\s*(?:\([^)]*\))?\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	djangoCeleryQueueRe      = regexp.MustCompile(`queue\s*=\s*["']([^"']+)["']`)
	djangoCeleryApplyAsyncRe = regexp.MustCompile(`(?m)(\w+)\.(apply_async|delay)\s*\(`)
	djangoMiddlewareClassRe  = regexp.MustCompile(`(?m)^class\s+([A-Z][A-Za-z0-9_]*Middleware)\s*(?:\([^)]*\))?\s*:`)
	djangoMiddlewareMethodRe = regexp.MustCompile(
		`(?m)^\s{4,}def\s+(process_(?:request|response|view|exception|template_response))\s*\(`)
	djangoTemplateFilterRe    = regexp.MustCompile(`(?m)@register\.filter\s*(?:\([^)]*\))?\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	djangoTemplateTagRe       = regexp.MustCompile(`(?m)@register\.tag\s*(?:\([^)]*\))?\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	djangoTemplateInclusionRe = regexp.MustCompile(`(?m)@register\.inclusion_tag\s*\([^)]*\)\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	djangoTemplateSimpleTagRe = regexp.MustCompile(`(?m)@register\.simple_tag\s*(?:\([^)]*\))?\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	djangoMgmtCommandRe       = regexp.MustCompile(`(?m)^class\s+Command\s*\([^)]*BaseCommand[^)]*\)\s*:`)
	djangoMgmtHandleRe        = regexp.MustCompile(`(?m)^\s{4,}def\s+handle\s*\(\s*self`)
	djangoManagerClassRe      = regexp.MustCompile(
		`(?m)^class\s+([A-Z][A-Za-z0-9_]*)\s*\([^)]*(?:(?:models\.)?(?:Manager|QuerySet)|BaseManager)[^)]*\)\s*:`)
	djangoLoginRequiredRe = regexp.MustCompile(`(?m)@login_required\s*(?:\([^)]*\))?\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	djangoPermRequiredRe  = regexp.MustCompile(`(?m)@permission_required\s*\(\s*["']?([^)"']+)["']?[^)]*\)\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)
)

func (e *DjangoExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_django")
	_, span := tracer.Start(ctx, "custom.python_django")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}

	source := string(file.Content)
	var out []types.EntityRecord

	// 1. URL patterns
	for _, idx := range allMatchesIndex(djangoPathCallRe, source) {
		routePattern := source[idx[2]:idx[3]]
		viewName := strings.TrimSpace(source[idx[4]:idx[5]])
		// Look for name= kwarg in vicinity
		ctx300 := source[idx[0]:min(idx[0]+300, len(source))]
		nameMatch := djangoPathNameRe.FindStringSubmatch(ctx300)
		urlName := routePattern
		if nameMatch != nil {
			urlName = nameMatch[1]
		}
		line := lineOf(source, idx[0])
		out = append(out, entity(urlName, "SCOPE.Operation", "endpoint", file.Path, line,
			map[string]string{"framework": "django", "pattern_type": "url_pattern", "route_pattern": routePattern, "view": viewName}))
	}

	// 1b. include() calls
	for _, idx := range allMatchesIndex(djangoIncludeRe, source) {
		modulePath := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		out = append(out, entity(modulePath, "SCOPE.Operation", "endpoint", file.Path, line,
			map[string]string{"framework": "django", "pattern_type": "url_include", "included_module": modulePath}))
	}

	// 2. CBV classes and their HTTP method handlers
	for _, idx := range allMatchesIndex(djangoCBVClassRe, source) {
		className := source[idx[2]:idx[3]]
		bases := strings.TrimSpace(source[idx[4]:idx[5]])
		classLine := lineOf(source, idx[0])
		out = append(out, entity(className, "SCOPE.Operation", "endpoint", file.Path, classLine,
			map[string]string{"framework": "django", "pattern_type": "cbv", "base_classes": bases}))

		// Extract class body and find HTTP methods
		body := extractClassBody(source, idx[0])
		for _, mIdx := range allMatchesIndex(djangoCBVMethodRe, body) {
			httpMethod := body[mIdx[2]:mIdx[3]]
			methodName := className + "." + httpMethod
			methodLine := classLine + strings.Count(body[:mIdx[0]], "\n")
			out = append(out, entity(methodName, "SCOPE.Operation", "method", file.Path, methodLine,
				map[string]string{"framework": "django", "pattern_type": "cbv_method", "http_method": httpMethod, "view_class": className}))
		}
	}

	// 3. Signal receivers
	// The handler function is the entity; the sender model becomes the TARGET
	// of a HANDLES_SIGNAL edge — not a new entity.  This avoids the
	// Service:<ModelName> phantom orphans introduced by the old YAML pattern
	// (issue #1374 item 3).
	for _, idx := range allMatchesIndex(djangoReceiverRe, source) {
		signalType := source[idx[2]:idx[3]]
		handlerName := source[idx[6]:idx[7]]
		line := lineOf(source, idx[0])
		props := map[string]string{"framework": "django", "pattern_type": "signal", "signal_type": signalType}
		var senderModel string
		if idx[4] != -1 {
			senderModel = source[idx[4]:idx[5]]
			props["sender"] = senderModel
		}
		ent := entity(handlerName, "SCOPE.Operation", "function", file.Path, line, props)
		// Emit HANDLES_SIGNAL → sender model so the handler is connected to
		// the existing model entity instead of leaving it as an orphan.
		if senderModel != "" {
			ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
				ToID:       djangoModelRef(senderModel),
				Kind:       string(types.RelationshipKindHandlesSignal),
				Properties: map[string]string{"signal_type": signalType, "framework": "django"},
			})
		}
		out = append(out, ent)
	}

	// 4. Admin registrations
	// Each registration emits an admin-class entity plus a REGISTERS edge to
	// the existing model entity.  The old YAML pattern emitted a phantom
	// Controller:<ModelName> entity with no edges; that has been removed
	// (issue #1374 item 3).
	for _, idx := range allMatchesIndex(djangoAdminRegRe, source) {
		modelName := source[idx[2]:idx[3]]
		adminClass := modelName + "Admin"
		if idx[4] != -1 {
			adminClass = source[idx[4]:idx[5]]
		}
		line := lineOf(source, idx[0])
		ent := entity(adminClass, "SCOPE.Component", "admin_class", file.Path, line,
			map[string]string{"framework": "django", "pattern_type": "admin_register", "model": modelName})
		ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
			ToID:       djangoModelRef(modelName),
			Kind:       string(types.RelationshipKindRegisters),
			Properties: map[string]string{"framework": "django", "model": modelName},
		})
		out = append(out, ent)
	}
	for _, idx := range allMatchesIndex(djangoAdminDecorRe, source) {
		modelName := source[idx[2]:idx[3]]
		adminClass := source[idx[4]:idx[5]]
		line := lineOf(source, idx[0])
		ent := entity(adminClass, "SCOPE.Component", "admin_class", file.Path, line,
			map[string]string{"framework": "django", "pattern_type": "admin_decorator", "model": modelName})
		ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
			ToID:       djangoModelRef(modelName),
			Kind:       string(types.RelationshipKindRegisters),
			Properties: map[string]string{"framework": "django", "model": modelName},
		})
		out = append(out, ent)
	}

	// 5. DRF serializers
	for _, idx := range allMatchesIndex(djangoDRFSerializerRe, source) {
		className := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		out = append(out, entity(className, "SCOPE.Component", "", file.Path, line,
			map[string]string{"framework": "drf", "pattern_type": "serializer", "component_kind": "serializer"}))
	}

	// 5b. DRF viewsets
	for _, idx := range allMatchesIndex(djangoDRFViewsetRe, source) {
		className := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		out = append(out, entity(className, "SCOPE.Component", "", file.Path, line,
			map[string]string{"framework": "drf", "pattern_type": "viewset", "component_kind": "viewset"}))
	}

	// 5c. DRF router.register()
	for _, idx := range allMatchesIndex(djangoRouterRegRe, source) {
		prefix := source[idx[2]:idx[3]]
		viewsetName := source[idx[4]:idx[5]]
		line := lineOf(source, idx[0])
		out = append(out, entity("router:"+prefix, "SCOPE.Component", "", file.Path, line,
			map[string]string{"framework": "drf", "pattern_type": "router_entry", "prefix": prefix, "viewset": viewsetName}))
	}

	// 6. Celery tasks (within Django context)
	for _, idx := range allMatchesIndex(djangoCeleryTaskRe, source) {
		taskName := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		props := map[string]string{"framework": "django", "pattern_type": "celery_task"}
		decoratorText := source[idx[0]:min(idx[0]+200, len(source))]
		if qm := djangoCeleryQueueRe.FindStringSubmatch(decoratorText); qm != nil {
			props["queue"] = qm[1]
		}
		out = append(out, entity(taskName, "SCOPE.Operation", "function", file.Path, line, props))
	}

	// 6b. apply_async / delay call sites
	for _, idx := range allMatchesIndex(djangoCeleryApplyAsyncRe, source) {
		taskRefName := source[idx[2]:idx[3]]
		callMethod := source[idx[4]:idx[5]]
		line := lineOf(source, idx[0])
		out = append(out, entity(taskRefName+"."+callMethod, "SCOPE.Operation", "function", file.Path, line,
			map[string]string{"framework": "django", "pattern_type": "celery_apply_async", "task": taskRefName, "call_method": callMethod}))
	}

	// 7. Middleware classes
	for _, idx := range allMatchesIndex(djangoMiddlewareClassRe, source) {
		className := source[idx[2]:idx[3]]
		classLine := lineOf(source, idx[0])
		body := extractClassBody(source, idx[0])
		methods := djangoMiddlewareMethodRe.FindAllStringSubmatch(body, -1)
		if len(methods) == 0 {
			continue
		}
		hooks := make([]string, 0, len(methods))
		for _, m := range methods {
			hooks = append(hooks, m[1])
		}
		out = append(out, entity(className, "SCOPE.Pattern", "", file.Path, classLine,
			map[string]string{"framework": "django", "pattern_type": "middleware", "hooks": strings.Join(hooks, ",")}))

		for _, mIdx := range allMatchesIndex(djangoMiddlewareMethodRe, body) {
			hookName := body[mIdx[2]:mIdx[3]]
			methodLine := classLine + strings.Count(body[:mIdx[0]], "\n")
			out = append(out, entity(className+"."+hookName, "SCOPE.Operation", "function", file.Path, methodLine,
				map[string]string{"framework": "django", "pattern_type": "middleware_hook", "hook": hookName, "middleware_class": className}))
		}
	}

	// 8. Template tags/filters
	tagPatterns := []struct {
		re   *regexp.Regexp
		kind string
	}{
		{djangoTemplateFilterRe, "filter"},
		{djangoTemplateTagRe, "tag"},
		{djangoTemplateInclusionRe, "inclusion_tag"},
		{djangoTemplateSimpleTagRe, "simple_tag"},
	}
	for _, tp := range tagPatterns {
		for _, idx := range allMatchesIndex(tp.re, source) {
			funcName := source[idx[2]:idx[3]]
			line := lineOf(source, idx[0])
			out = append(out, entity(funcName, "SCOPE.Component", "", file.Path, line,
				map[string]string{"framework": "django", "pattern_type": "template_tag", "tag_kind": tp.kind}))
		}
	}

	// 9. Management commands
	for _, idx := range allMatchesIndex(djangoMgmtCommandRe, source) {
		classLine := lineOf(source, idx[0])
		out = append(out, entity("Command", "SCOPE.Operation", "function", file.Path, classLine,
			map[string]string{"framework": "django", "pattern_type": "management_command"}))

		body := extractClassBody(source, idx[0])
		for _, mIdx := range allMatchesIndex(djangoMgmtHandleRe, body) {
			handleLine := classLine + strings.Count(body[:mIdx[0]], "\n")
			out = append(out, entity("Command.handle", "SCOPE.Operation", "function", file.Path, handleLine,
				map[string]string{"framework": "django", "pattern_type": "management_command_handle"}))
		}
	}

	// 10. Model managers / querysets
	for _, idx := range allMatchesIndex(djangoManagerClassRe, source) {
		className := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		out = append(out, entity(className, "SCOPE.Schema", "", file.Path, line,
			map[string]string{"framework": "django", "pattern_type": "model_manager"}))
	}

	// 11. View decorators
	for _, idx := range allMatchesIndex(djangoLoginRequiredRe, source) {
		funcName := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		out = append(out, entity(funcName, "SCOPE.Operation", "function", file.Path, line,
			map[string]string{"framework": "django", "pattern_type": "view_decorator", "decorator": "login_required", "auth_required": "true"}))
	}
	for _, idx := range allMatchesIndex(djangoPermRequiredRe, source) {
		permission := strings.TrimSpace(source[idx[2]:idx[3]])
		funcName := source[idx[4]:idx[5]]
		line := lineOf(source, idx[0])
		out = append(out, entity(funcName, "SCOPE.Operation", "function", file.Path, line,
			map[string]string{"framework": "django", "pattern_type": "view_decorator", "decorator": "permission_required", "permission": permission, "auth_required": "true"}))
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// djangoModelRef returns the structural reference ID used as the ToID for
// REGISTERS and HANDLES_SIGNAL edges targeting a Django model class.
//
// The "Class:<Name>" format matches the intra-repo resolver's byName lookup
// against SCOPE.Component/class and SCOPE.Schema entities emitted by the
// Python extractor for `class <Name>(models.Model)`.  This is identical to
// the convention used by applyORMQueries (engine/orm_queries.go) so both
// passes connect to the same canonical model node.
func djangoModelRef(modelName string) string {
	return fmt.Sprintf("Class:%s", modelName)
}

// extractClassBody returns the class body text from class_start to the next
// top-level definition or EOF.
func extractClassBody(source string, classStart int) string {
	lines := strings.Split(source[classStart:], "\n")
	if len(lines) == 0 {
		return ""
	}
	headerLine := lines[0]
	classIndent := len(headerLine) - len(strings.TrimLeft(headerLine, " \t"))

	var bodyLines []string
	for i, line := range lines {
		if i == 0 {
			bodyLines = append(bodyLines, line)
			continue
		}
		stripped := strings.TrimSpace(line)
		if stripped == "" {
			bodyLines = append(bodyLines, line)
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if indent <= classIndent && stripped != "" {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return strings.Join(bodyLines, "\n")
}
