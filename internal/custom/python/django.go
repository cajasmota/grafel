package python

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/lifecycle"
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
	djangoReceiverOnlyRe  = regexp.MustCompile(`(?m)@receiver\s*\(\s*([\w.]+)(?:\s*,\s*sender\s*=\s*(\w+))?[^)]*\)`)
	djangoAdminRegRe      = regexp.MustCompile(`(?m)admin\.site\.register\s*\(\s*(\w+)(?:\s*,\s*(\w+))?\s*\)`)
	djangoAdminDecorRe    = regexp.MustCompile(`(?m)@admin\.register\s*\(\s*(\w+)\s*\)\s*\n\s*class\s+(\w+)\s*\(`)
	djangoDRFSerializerRe = regexp.MustCompile(
		`(?m)^class\s+([A-Z][A-Za-z0-9_]*)\s*\([^)]*(?:serializers\.)?(?:ModelSerializer|HyperlinkedModelSerializer|Serializer|ListSerializer)[^)]*\)\s*:`)
	djangoDRFViewsetRe = regexp.MustCompile(
		`(?m)^class\s+([A-Z][A-Za-z0-9_]*)\s*\([^)]*(?:viewsets\.)?(?:ModelViewSet|ReadOnlyModelViewSet|ViewSet|GenericViewSet|ViewSetMixin)[^)]*\)\s*:`)
	djangoRouterRegRe = regexp.MustCompile(
		`(?m)router\.register\s*\(\s*(?:r)?["']([^"']*)["']\s*,\s*(\w+)`)
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
	// Django model class: class Foo(models.Model) / a SafeDeleteModel subclass.
	// Group 1 = class name, group 2 = base-class list.
	djangoModelClassRe = regexp.MustCompile(
		`(?m)^class\s+([A-Z][A-Za-z0-9_]*)\s*\(([^)]*(?:models\.Model|SafeDeleteModel|SafeDeleteMPTTModel)[^)]*)\)\s*:`)

	djangoLoginRequiredRe = regexp.MustCompile(`(?m)@login_required\s*(?:\([^)]*\))?\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	djangoPermRequiredRe  = regexp.MustCompile(`(?m)@permission_required\s*\(\s*["']?([^)"']+)["']?[^)]*\)\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)

	// Form / ModelForm field type introspection (issue #3346).
	// Matches class Foo(forms.Form) / forms.ModelForm etc.
	djangoFormClassBodyRe = regexp.MustCompile(
		`(?m)^class\s+([A-Z][A-Za-z0-9_]*)\s*\([^)]*(?:forms\.)?(?:ModelForm|Form|BaseForm)[^)]*\)\s*:`)
	// field = CharField(...) / IntegerField(...) / etc. — 4-space indented body line.
	djangoFormFieldRe = regexp.MustCompile(
		`(?m)^\s{4,}(\w+)\s*=\s*([\w.]*(?:Char|Integer|Float|Decimal|Boolean|Date|DateTime|Time|Email|URL|Slug|UUID|IP|File|Image|Choice|Multiple|Typed|Null|Model|Combo|Hidden|Multi|Split|Regex|GenericIP|NullBoolean|Duration|Json|Float)Field)\s*\(`)

	// MIDDLEWARE settings-list parser (issue #3346).
	// Matches: MIDDLEWARE = [ "...", "..." ] across multiple lines.
	djangoMiddlewareSettingRe = regexp.MustCompile(
		`(?m)^MIDDLEWARE\s*(?:\+?=)\s*\[`)
	djangoMiddlewareItemRe = regexp.MustCompile(`["']([A-Za-z][\w.]+)["']`)

	// DRF SerializerMethodField return-type inference (issue #3346).
	// class FooSerializer → field = SerializerMethodField() → def get_field(self) -> ReturnType
	djangoDRFSMFRe = regexp.MustCompile(
		`(?m)^\s{4,}(\w+)\s*=\s*(?:serializers\.)?SerializerMethodField\s*\(`)
	djangoDRFSMFGetterRe = regexp.MustCompile(
		`(?m)^\s{4,}def\s+(get_\w+)\s*\([^)]*\)\s*(?:->\s*([\w\[\], |]+?))?\s*:`)

	// DRF DEFAULT_AUTHENTICATION_CLASSES / DEFAULT_THROTTLE_CLASSES (issue #3346).
	// Matches REST_FRAMEWORK = { "DEFAULT_AUTHENTICATION_CLASSES": [...] }
	djangoDRFRestFrameworkRe = regexp.MustCompile(
		`(?ms)REST_FRAMEWORK\s*=\s*\{(.+?)\}`)
	djangoDRFAuthClassesRe     = regexp.MustCompile(`["']DEFAULT_AUTHENTICATION_CLASSES["']\s*:\s*\[([^\]]*)\]`)
	djangoDRFThrottleClassesRe = regexp.MustCompile(`["']DEFAULT_THROTTLE_CLASSES["']\s*:\s*\[([^\]]*)\]`)
	djangoDRFClassItemRe       = regexp.MustCompile(`["']([A-Za-z][\w.]+)["']`)
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

	// #1411: Pre-collect DRF ViewSet class names so section 2 (CBV) can skip
	// them. A ViewSet is emitted as SCOPE.Component/viewset by section 5b; if
	// the CBV regex also matched it we'd emit a duplicate SCOPE.Operation/endpoint.
	drfViewsetNames := map[string]bool{}
	for _, idx := range allMatchesIndex(djangoDRFViewsetRe, source) {
		drfViewsetNames[source[idx[2]:idx[3]]] = true
	}

	// 2. CBV classes and their HTTP method handlers
	for _, idx := range allMatchesIndex(djangoCBVClassRe, source) {
		className := source[idx[2]:idx[3]]
		bases := strings.TrimSpace(source[idx[4]:idx[5]])
		classLine := lineOf(source, idx[0])

		// #1411: Skip the CBV class entity if section 5b will emit this class
		// as a DRF ViewSet (SCOPE.Component/viewset). HTTP method children are
		// still emitted for both ViewSet and non-ViewSet CBVs.
		if !drfViewsetNames[className] {
			out = append(out, entity(className, "SCOPE.Operation", "endpoint", file.Path, classLine,
				map[string]string{"framework": "django", "pattern_type": "cbv", "base_classes": bases}))
		}

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
	//
	// #2599: Walk upward from each `def` to collect ALL stacked @receiver decorators,
	// emitting one HANDLES_SIGNAL edge per @receiver (one per sender).

	// Find all @receiver decorators in the file (without requiring a def to follow).
	receiverMatches := allMatchesIndex(djangoReceiverOnlyRe, source)

	// Find all function definitions in the file.
	defRe := regexp.MustCompile(`(?m)^\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	defMatches := allMatchesIndex(defRe, source)

	// Track handler functions we've already emitted (to avoid duplicates).
	handledFuncs := map[string]bool{}

	// For each function, collect its associated @receiver decorators.
	for _, defIdx := range defMatches {
		defStart := defIdx[0]
		handlerName := source[defIdx[2]:defIdx[3]]

		// Skip if we've already emitted this handler.
		if handledFuncs[handlerName] {
			continue
		}

		// Collect all @receiver decorators that immediately precede this def.
		var decorators []struct {
			signalType  string
			senderModel string
			line        int
		}

		// Walk backward through decorators to find those immediately before def.
		nextCheckPos := defStart
		for i := len(receiverMatches) - 1; i >= 0; i-- {
			rIdx := receiverMatches[i]
			rStart := rIdx[0]
			rEnd := rIdx[1]

			// Only consider receivers that come before the next check position.
			if rStart >= nextCheckPos {
				continue
			}

			// Check if there's only whitespace/comments between @receiver and next pos.
			between := source[rEnd:nextCheckPos]
			if !isOnlyWhitespaceAndComments(between) {
				break // Reached a non-contiguous decorator; stop walking backward.
			}

			signalType := source[rIdx[2]:rIdx[3]]
			var senderModel string
			if rIdx[4] != -1 {
				senderModel = source[rIdx[4]:rIdx[5]]
			}
			rLine := lineOf(source, rStart)

			decorators = append(decorators, struct {
				signalType  string
				senderModel string
				line        int
			}{signalType, senderModel, rLine})

			// Update position to check next decorator is immediately before current one.
			nextCheckPos = rStart
		}

		// If no receivers found, skip this function.
		if len(decorators) == 0 {
			continue
		}

		// Reverse the decorators since we collected them backward.
		for i, j := 0, len(decorators)-1; i < j; i, j = i+1, j-1 {
			decorators[i], decorators[j] = decorators[j], decorators[i]
		}

		handledFuncs[handlerName] = true

		// Use the first (highest) decorator's line for the entity.
		entityLine := decorators[0].line

		// Build properties: include signal_type and sender from first receiver (for backward compatibility).
		props := map[string]string{"framework": "django", "pattern_type": "signal"}
		if len(decorators) > 0 {
			props["signal_type"] = decorators[0].signalType
			if decorators[0].senderModel != "" {
				props["sender"] = decorators[0].senderModel
			}
		}

		// Emit one entity per distinct handler function.
		// We'll aggregate all HANDLES_SIGNAL edges onto it.
		ent := entity(handlerName, "SCOPE.Operation", "function", file.Path, entityLine, props)

		// Add one HANDLES_SIGNAL edge per @receiver decorator.
		for _, decorator := range decorators {
			if decorator.senderModel != "" {
				ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
					ToID:       djangoModelRef(decorator.senderModel),
					Kind:       string(types.RelationshipKindHandlesSignal),
					Properties: map[string]string{"signal_type": decorator.signalType, "framework": "django"},
				})
			}
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

	// NOTE: Celery tasks (@shared_task / @app.task) and apply_async/delay call
	// sites are extracted by the dedicated python_celery extractor
	// (internal/custom/python/celery.go) which emits canonical SCOPE.Service/task
	// nodes with richer metadata (queue, bind, max_retries) and by
	// scheduled_jobs_edges.go which emits SCOPE.ScheduledJob + TRIGGERS edges.
	// Duplicating that extraction here (#1411) caused 2–3× node inflation for
	// every Celery task and fragmented find_callers queries. Sections 6 + 6b
	// have been removed. The Celery pub/sub topology (TRIGGERS/PUBLISHES_TO) is
	// unaffected — it is owned by scheduled_jobs_edges.go, not this extractor.

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

	// 10b. Model classes with data-lifecycle traits (#3628 child).
	//      Each `class X(models.Model)` (or django-safedelete subclass) is
	//      emitted as a SCOPE.Schema model node stamped with soft-delete /
	//      timestamps / audit-column traits resolved from its class body, so the
	//      graph can answer "which Django models soft-delete / track timestamps?".
	//      Detection is convention-driven (deleted_at/is_deleted field, a
	//      safedelete base, auto_now/auto_now_add or created_at+updated_at) — a
	//      plain `deleted` boolean is NOT reported as soft-delete.
	for _, idx := range allMatchesIndex(djangoModelClassRe, source) {
		className := source[idx[2]:idx[3]]
		bases := source[idx[4]:idx[5]]
		classLine := lineOf(source, idx[0])
		body := extractClassBody(source, idx[0])
		props := map[string]string{
			"framework":    "django",
			"pattern_type": "model",
			"provenance":   "INFERRED_FROM_DJANGO_MODEL",
		}
		lifecycle.Django(bases, body).Stamp(func(kv ...string) {
			for i := 0; i+1 < len(kv); i += 2 {
				props[kv[i]] = kv[i+1]
			}
		})
		out = append(out, entity(className, "SCOPE.Schema", "model", file.Path, classLine, props))
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
			map[string]string{
				"framework":    "django",
				"pattern_type": "view_decorator",
				"decorator":    "permission_required",
				"permission":   permission,
				// #authz — the specific required permission on the cross-language
				// flat contract so archigraph_auth_coverage answers
				// "what permission does this route require?" uniformly.
				"auth_permissions": permission,
				"auth_method":      "decorator",
				"auth_confidence":  "high",
				"auth_required":    "true",
			}))
	}

	// 12. Form / ModelForm per-field type introspection (issue #3346).
	for _, idx := range allMatchesIndex(djangoFormClassBodyRe, source) {
		className := source[idx[2]:idx[3]]
		classLine := lineOf(source, idx[0])
		body := extractClassBody(source, idx[0])
		var fieldNames, fieldTypes []string
		for _, fIdx := range allMatchesIndex(djangoFormFieldRe, body) {
			fieldName := body[fIdx[2]:fIdx[3]]
			fieldType := body[fIdx[4]:fIdx[5]]
			// strip module prefix (forms.CharField → CharField)
			if dotPos := strings.LastIndex(fieldType, "."); dotPos >= 0 {
				fieldType = fieldType[dotPos+1:]
			}
			fieldNames = append(fieldNames, fieldName)
			fieldTypes = append(fieldTypes, fieldType)
			fieldLine := classLine + strings.Count(body[:fIdx[0]], "\n")
			out = append(out, entity(className+"."+fieldName, "SCOPE.Schema", "form_field", file.Path, fieldLine,
				map[string]string{
					"framework":    "django",
					"pattern_type": "form_field",
					"form_class":   className,
					"field_name":   fieldName,
					"field_type":   fieldType,
				}))
		}
		if len(fieldNames) > 0 {
			out = append(out, entity(className, "SCOPE.Schema", "form_class", file.Path, classLine,
				map[string]string{
					"framework":    "django",
					"pattern_type": "form_class",
					"field_names":  strings.Join(fieldNames, ","),
					"field_types":  strings.Join(fieldTypes, ","),
				}))
		}
	}

	// 13. MIDDLEWARE settings-list parser (issue #3346).
	// Only fires when the file contains a top-level MIDDLEWARE = [...] assignment
	// (typically settings.py or its per-environment override).
	if loc := djangoMiddlewareSettingRe.FindStringIndex(source); loc != nil {
		// Extract from the opening bracket to the first matching close bracket.
		openBracket := loc[1] - 1 // position of '['
		listBody := extractBalancedBrackets(source, openBracket)
		line := lineOf(source, loc[0])
		items := djangoMiddlewareItemRe.FindAllStringSubmatch(listBody, -1)
		var paths []string
		for _, m := range items {
			paths = append(paths, m[1])
		}
		if len(paths) > 0 {
			out = append(out, entity("MIDDLEWARE", "SCOPE.Config", "middleware_list", file.Path, line,
				map[string]string{
					"framework":       "django",
					"pattern_type":    "middleware_settings",
					"middleware_list": strings.Join(paths, ","),
				}))
		}
	}

	// 14. DRF SerializerMethodField return-type inference (issue #3346).
	for _, idx := range allMatchesIndex(djangoDRFSerializerRe, source) {
		className := source[idx[2]:idx[3]]
		classLine := lineOf(source, idx[0])
		body := extractClassBody(source, idx[0])
		// Build map: field_name → getter_name for SMF fields.
		smfFields := map[string]bool{}
		for _, fIdx := range allMatchesIndex(djangoDRFSMFRe, body) {
			smfFields[body[fIdx[2]:fIdx[3]]] = true
		}
		if len(smfFields) == 0 {
			continue
		}
		// Match getter methods and infer return type from annotation.
		for _, gIdx := range allMatchesIndex(djangoDRFSMFGetterRe, body) {
			getterName := body[gIdx[2]:gIdx[3]] // e.g. "get_full_name"
			// Field name is getterName without the "get_" prefix.
			fieldName := strings.TrimPrefix(getterName, "get_")
			if !smfFields[fieldName] {
				continue
			}
			returnType := ""
			if gIdx[4] != -1 {
				returnType = strings.TrimSpace(body[gIdx[4]:gIdx[5]])
			}
			methodLine := classLine + strings.Count(body[:gIdx[0]], "\n")
			props := map[string]string{
				"framework":    "drf",
				"pattern_type": "serializer_method_field",
				"serializer":   className,
				"field_name":   fieldName,
				"getter":       getterName,
			}
			if returnType != "" {
				props["return_type"] = returnType
			}
			out = append(out, entity(className+"."+fieldName, "SCOPE.Schema", "serializer_field", file.Path, methodLine, props))
		}
	}

	// 15. DRF DEFAULT_AUTHENTICATION_CLASSES / DEFAULT_THROTTLE_CLASSES (issue #3346).
	// Parses REST_FRAMEWORK = { ... } in Django settings files.
	if rfIdx := djangoDRFRestFrameworkRe.FindStringSubmatchIndex(source); rfIdx != nil {
		rfBlock := source[rfIdx[2]:rfIdx[3]]
		line := lineOf(source, rfIdx[0])
		if am := djangoDRFAuthClassesRe.FindStringSubmatch(rfBlock); am != nil {
			items := djangoDRFClassItemRe.FindAllStringSubmatch(am[1], -1)
			var classes []string
			for _, m := range items {
				classes = append(classes, m[1])
			}
			if len(classes) > 0 {
				out = append(out, entity("DEFAULT_AUTHENTICATION_CLASSES", "SCOPE.Config", "drf_setting", file.Path, line,
					map[string]string{
						"framework":    "drf",
						"pattern_type": "drf_setting",
						"setting_key":  "DEFAULT_AUTHENTICATION_CLASSES",
						"classes":      strings.Join(classes, ","),
					}))
			}
		}
		if tm := djangoDRFThrottleClassesRe.FindStringSubmatch(rfBlock); tm != nil {
			items := djangoDRFClassItemRe.FindAllStringSubmatch(tm[1], -1)
			var classes []string
			for _, m := range items {
				classes = append(classes, m[1])
			}
			if len(classes) > 0 {
				out = append(out, entity("DEFAULT_THROTTLE_CLASSES", "SCOPE.Config", "drf_setting", file.Path, line,
					map[string]string{
						"framework":    "drf",
						"pattern_type": "drf_setting",
						"setting_key":  "DEFAULT_THROTTLE_CLASSES",
						"classes":      strings.Join(classes, ","),
					}))
			}
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// extractBalancedBrackets returns the content of a [...] list starting at openPos.
func extractBalancedBrackets(source string, openPos int) string {
	depth := 0
	for i := openPos; i < len(source); i++ {
		switch source[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return source[openPos+1 : i]
			}
		}
	}
	return ""
}

// isOnlyWhitespaceAndComments returns true if text contains only whitespace and
// Python comments (lines starting with #). Used to detect if @receiver decorators
// immediately precede a def.
func isOnlyWhitespaceAndComments(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			return false
		}
	}
	return true
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
