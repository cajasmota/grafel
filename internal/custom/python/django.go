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
	// #4789 — imperative signal wiring registered in AppConfig.ready() (or
	// anywhere): `post_save.connect(my_receiver, sender=Foo)`, the dotted
	// `signals.post_delete.connect(handler)` form, and the bare
	// `<signal>.connect(<receiver>)` (no sender). Group 1 = the signal
	// expression (possibly dotted, e.g. `signals.post_save`), group 2 = the
	// receiver callable, group 3 = the optional `sender=` model. The receiver
	// and sender may be dotted; the leaf is used for entity / model resolution.
	djangoSignalConnectRe = regexp.MustCompile(
		`([\w.]+)\.connect\s*\(\s*([\w.]+)\s*(?:,\s*sender\s*=\s*([\w.]+))?[^)]*\)`)
	djangoAdminRegRe      = regexp.MustCompile(`(?m)admin\.site\.register\s*\(\s*(\w+)(?:\s*,\s*(\w+))?\s*\)`)
	djangoAdminDecorRe    = regexp.MustCompile(`(?m)@admin\.register\s*\(\s*(\w+)\s*\)\s*\n\s*class\s+(\w+)\s*\(`)
	djangoDRFSerializerRe = regexp.MustCompile(
		`(?m)^class\s+([A-Z][A-Za-z0-9_]*)\s*\([^)]*(?:serializers\.)?(?:ModelSerializer|HyperlinkedModelSerializer|Serializer|ListSerializer)[^)]*\)\s*:`)
	djangoDRFViewsetRe = regexp.MustCompile(
		`(?m)^class\s+([A-Z][A-Za-z0-9_]*)\s*\([^)]*(?:viewsets\.)?(?:ModelViewSet|ReadOnlyModelViewSet|ViewSet|GenericViewSet|ViewSetMixin)[^)]*\)\s*:`)
	djangoRouterRegRe = regexp.MustCompile(
		`(?m)router\.register\s*\(\s*(?:r)?["']([^"']*)["']\s*,\s*(\w+)`)

	// #4474 — DRF view↔serializer DTO linkage. A GenericAPIView/ViewSet resolves
	// its request/response DTO via `serializer_class = FooSerializer` (class
	// attribute) or per-action `get_serializer_class()`/inline overrides. We
	// resolve the serializer class name and emit ACCEPTS_INPUT (request) /
	// RETURNS (response) edges from the view to the serializer entity.
	//
	// (a) class-level `serializer_class = FooSerializer` (4-space body indent).
	djangoSerializerClassAttrRe = regexp.MustCompile(
		`(?m)^\s{4,}serializer_class\s*=\s*([A-Z][A-Za-z0-9_]*)\b`)
	// (b) inline serializer construction inside an action:
	//     `FooSerializer(data=request.data)` (request — ACCEPTS_INPUT)
	//     `FooSerializer(obj)` / `FooSerializer(qs, many=True)` (response — RETURNS)
	// Group 1 = serializer class name, group 2 = the call arg head.
	djangoSerializerCallRe = regexp.MustCompile(
		`([A-Z][A-Za-z0-9_]*Serializer)\s*\(\s*(data\s*=)?`)
	// (c) drf-yasg `@swagger_auto_schema(request_body=FooSerializer, responses={...: BarSerializer})`.
	djangoSwaggerRequestBodyRe = regexp.MustCompile(
		`request_body\s*=\s*([A-Z][A-Za-z0-9_]*)\b`)
	djangoSwaggerResponsesSerRe = regexp.MustCompile(
		`([A-Z][A-Za-z0-9_]*Serializer)\b`)
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

	// Anchor for the global-wiring pass (issue #4379, extended #4403): fires
	// when the file is a settings module declaring any of the supported
	// cross-cutting lists. The matched offset positions the synthetic
	// django_settings entity on the first such assignment.
	djangoSettingsAnchorRe = regexp.MustCompile(
		`(?m)^(?:MIDDLEWARE|AUTHENTICATION_BACKENDS|REST_FRAMEWORK|TEMPLATES|INSTALLED_APPS)\s*(?:\+?=)\s*[\[({]`)

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

	// Issue #4366 — model field-membership. A 4+-space-indented body
	// assignment `<attr> = models.SomethingField(...)` (or a bare
	// `SomethingField(...)` / a relational `ForeignKey(...)`). Group 1 is the
	// attribute name. We deliberately match only field-constructor RHS shapes
	// (CharField / ForeignKey / ...Field / ...) so class-level constants like
	// `STATUS_CHOICES = [...]` and method defs are NOT treated as fields.
	djangoModelFieldRe = regexp.MustCompile(
		`(?m)^\s{4,}(\w+)\s*=\s*([\w.]*(?:Field|ForeignKey|OneToOneField|ManyToManyField|GenericForeignKey|GenericRelation))\s*\(`)
	// Any class-body attribute assignment `<attr> = ...` at the immediate
	// (4-space) body indent — used to restore full CONTAINS membership parity
	// with the base class node this custom model node replaces (issue #4366).
	// Captures class constants (STATUS_CHOICES = [...]) and Manager attachments
	// (objects = Manager()) in addition to model fields. Excludes dunder /
	// type-annotated-only declarations and `==` comparisons.
	djangoModelAttrRe = regexp.MustCompile(
		`(?m)^\s{4}(\w+)\s*(?::[^=\n]+)?=\s*[^=]`)
	// Relational field RHS whose first positional/`to=` argument names the
	// target model — string form ('app.Model' / 'self') or symbol form (Model).
	// Group 1 = relation kind, group 2 = quoted target (if string form),
	// group 3 = identifier target (if symbol form).
	djangoModelRelTargetRe = regexp.MustCompile(
		`(?:ForeignKey|OneToOneField|ManyToManyField)\s*\(\s*(?:to\s*=\s*)?(?:["']([^"']+)["']|([A-Z][A-Za-z0-9_]*))`)

	// Issue #4366 — DRF serializer field declaration. A 4+-space-indented body
	// assignment whose RHS is either a `serializers.XField(...)` / `XField(...)`
	// call OR a nested serializer constructor `SomeSerializer(...)`. Group 1 is
	// the attribute name, group 2 the RHS callee (dotted path allowed).
	djangoSerializerFieldRe = regexp.MustCompile(
		`(?m)^\s{4,}(\w+)\s*=\s*([\w.]*(?:Field|Serializer))\s*\(`)
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
		// Extract class body once — used for HTTP-method children and (for DRF
		// APIView/GenericAPIView CBVs) the #4474 view↔serializer DTO edges.
		body := extractClassBody(source, idx[0])

		if !drfViewsetNames[className] {
			cbvEnt := entity(className, "SCOPE.Operation", "endpoint", file.Path, classLine,
				map[string]string{"framework": "django", "pattern_type": "cbv", "base_classes": bases})
			// #4474 — a DRF APIView/GenericAPIView CBV resolves its request/
			// response DTO via serializer_class / inline serializer calls, exactly
			// like a ViewSet. Attach the edges in-place (no duplicate view node).
			if strings.Contains(bases, "APIView") || strings.Contains(bases, "GenericAPIView") ||
				strings.Contains(body, "serializer_class") || djangoSerializerCallRe.MatchString(body) {
				appendDRFSerializerEdges(&cbvEnt, body)
			}
			out = append(out, cbvEnt)
		}

		// Find HTTP methods
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

	// 3b. Imperative signal wiring (#4789).
	// `post_save.connect(my_receiver, sender=Foo)` registered in an AppConfig
	// `ready()` method (or anywhere in the module) wires the receiver→signal the
	// same way the `@receiver` decorator does, but through a runtime call rather
	// than a decorator — so the decorator pass above never sees it. Emit the same
	// HANDLES_SIGNAL edge shape: a handler entity (the receiver function) with a
	// HANDLES_SIGNAL edge to the sender model, carrying signal_type. Marked
	// di_role=signal_handler so the binding is distinguishable from the
	// decorator form. The receiver's leaf name resolves structurally to the real
	// function entity via the QualifiedName/name index. A `connect()` whose first
	// arg is not a plausible callable name (e.g. a lambda) is skipped.
	for _, m := range djangoSignalConnectRe.FindAllStringSubmatchIndex(source, -1) {
		signalExpr := source[m[2]:m[3]]
		receiver := source[m[4]:m[5]]
		var senderModel string
		if m[6] != -1 {
			senderModel = source[m[6]:m[7]]
		}
		// The `.connect(` method also appears on non-signal objects (sockets,
		// DB connections). Gate on the signal expression's leaf being a known
		// Django signal name so we don't wire unrelated `.connect()` calls.
		signalType := djangoLeafName(signalExpr)
		if !djangoKnownSignals[signalType] {
			continue
		}
		handlerName := djangoLeafName(receiver)
		if handlerName == "" {
			continue
		}
		// Skip an already-emitted decorator-wired handler so the same function
		// isn't double-listed (the decorator path is authoritative for it).
		if handledFuncs[handlerName] {
			continue
		}
		props := map[string]string{
			"framework":    "django",
			"language":     "python",
			"pattern_type": "signal",
			"signal_type":  signalType,
			"di_role":      "signal_handler",
			"provenance":   "INFERRED_FROM_DJANGO_SIGNAL_CONNECT",
		}
		if senderModel != "" {
			props["sender"] = djangoLeafName(senderModel)
		}
		ent := entity(handlerName, "SCOPE.Operation", "function", file.Path, lineOf(source, m[0]), props)
		// Emit the HANDLES_SIGNAL edge. With a sender, target the model; without
		// one, target the signal itself so the wiring is still represented.
		edgeProps := map[string]string{"signal_type": signalType, "framework": "django", "di_role": "signal_handler"}
		if senderModel != "" {
			ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
				ToID:       djangoModelRef(djangoLeafName(senderModel)),
				Kind:       string(types.RelationshipKindHandlesSignal),
				Properties: edgeProps,
			})
		} else {
			ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
				ToID:       signalType,
				Kind:       string(types.RelationshipKindHandlesSignal),
				Properties: edgeProps,
			})
		}
		handledFuncs[handlerName] = true
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
		serEnt := entity(className, "SCOPE.Component", "", file.Path, line,
			map[string]string{"framework": "drf", "pattern_type": "serializer", "component_kind": "serializer"})

		// Issue #4366 — serializer field membership. This serializer node
		// replaces the base tree-sitter class node (which carried the #526
		// class→field CONTAINS edges), so re-emit membership for each declared
		// serializer field. Nested-serializer fields (`items = ItemSerializer(
		// many=True)`) additionally REFERENCES the target serializer class.
		body := extractClassBody(source, idx[0])
		seenSerMember := map[string]bool{}
		// (a) CONTAINS for every serializer class-body attribute (declared
		// fields + constants) — restores parity with the replaced base node.
		for _, aIdx := range allMatchesIndex(djangoModelAttrRe, body) {
			attr := body[aIdx[2]:aIdx[3]]
			if attr == "" || seenSerMember[attr] || strings.HasPrefix(attr, "__") {
				continue
			}
			seenSerMember[attr] = true
			serEnt.Relationships = append(serEnt.Relationships,
				containsFieldEdge(className, className+"."+attr, attr, "drf"))
		}
		// (b) REFERENCES for nested-serializer fields (`items = ItemSerializer(
		// many=True)`) → the target serializer/model class.
		for _, fIdx := range allMatchesIndex(djangoSerializerFieldRe, body) {
			attr := body[fIdx[2]:fIdx[3]]
			callee := body[fIdx[4]:fIdx[5]]
			if attr == "" {
				continue
			}
			if strings.HasSuffix(callee, "Serializer") && !strings.Contains(callee, ".") &&
				callee != "Serializer" && callee != "ModelSerializer" {
				serEnt.Relationships = append(serEnt.Relationships,
					referencesClassEdge(className+"."+attr, callee, "drf", attr))
			}
		}

		// (c) Issue #4613 — FIELD-as-member sub-entities. Mirror the JS/TS DTO
		// field-membership model (#4635): each explicitly-declared serializer
		// field becomes a `SCOPE.Schema`/field child carrying name/type/optional/
		// validators, so request/response DRF FIELD-level diffs are no longer
		// limited. The CONTAINS edges from (a) already bind these names to the
		// owner; here we emit the child entities those edges point at.
		explicitFields := extractDRFSerializerFields(body)
		out = append(out, emitPyDTOFieldMembers(
			className, explicitFields, "drf", file.Path, line, nil)...)

		// (d) ModelSerializer Meta.fields. `fields = [...]` → emit the enumerated
		// names as field members (type unknown — model-derived). `fields =
		// "__all__"` → mark the serializer as model-derived/unenumerated with a
		// flag rather than silently emitting nothing.
		meta := extractDRFMetaFields(body)
		switch {
		case meta.isAll:
			serEnt.Properties["fields_source"] = "model_all"
			serEnt.Properties["fields_unenumerated"] = "true"
		case len(meta.names) > 0:
			serEnt.Properties["fields_source"] = "meta_list"
			var metaFields []pyDTOField
			for _, fn := range meta.names {
				if seenSerMember[fn] {
					continue // already emitted as an explicit field
				}
				metaFields = append(metaFields, pyDTOField{name: fn, typ: "unknown"})
				serEnt.Relationships = append(serEnt.Relationships,
					containsFieldEdge(className, className+"."+fn, fn, "drf"))
			}
			out = append(out, emitPyDTOFieldMembers(
				className, metaFields, "drf", file.Path, line, nil)...)
		}
		out = append(out, serEnt)
	}

	// 5b. DRF viewsets — the ViewSet entity (carrying the #4474 view↔serializer
	// edges) is emitted by section 5d below so the edges hang off a single
	// canonical viewset node. (Pre-collected as drfViewsetNames above so the CBV
	// pass already skips re-emitting these as endpoints.)

	// 5c. DRF router.register()
	for _, idx := range allMatchesIndex(djangoRouterRegRe, source) {
		prefix := source[idx[2]:idx[3]]
		viewsetName := source[idx[4]:idx[5]]
		line := lineOf(source, idx[0])
		out = append(out, entity("router:"+prefix, "SCOPE.Component", "", file.Path, line,
			map[string]string{"framework": "drf", "pattern_type": "router_entry", "prefix": prefix, "viewset": viewsetName}))
	}

	// 5d. #4474 — DRF view↔serializer DTO linkage. For each DRF view class
	// (GenericAPIView/APIView CBV or ViewSet), resolve its request/response
	// serializer and emit ACCEPTS_INPUT (request) / RETURNS (response) edges
	// from the view to the serializer entity, mirroring the NestJS handler→DTO
	// edge kind/shape so cross-framework request/response-shape diffs join
	// uniformly. The serializer entities already exist (section 5); these edges
	// connect the floating view to them. Conservative: only when the serializer
	// resolves to a `XSerializer`-shaped class name (Class:<name> resolver stub,
	// bound merge-stably post-merge — no duplicate DTO nodes created here).
	// Emit the ViewSet entities here (carrying the view↔serializer edges) so the
	// edges hang off a single canonical viewset node (section 5b no longer emits
	// them). APIView/GenericAPIView CBVs get the same edges attached in-place in
	// section 2, so no duplicate view node is created.
	for _, idx := range allMatchesIndex(djangoDRFViewsetRe, source) {
		className := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		ent := entity(className, "SCOPE.Component", "", file.Path, line,
			map[string]string{"framework": "drf", "pattern_type": "viewset", "component_kind": "viewset"})
		appendDRFSerializerEdges(&ent, extractClassBody(source, idx[0]))
		out = append(out, ent)
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
		modelEnt := entity(className, "SCOPE.Schema", "model", file.Path, classLine, props)

		// Issue #4366 — field membership. MergeWithCustom replaces the base
		// tree-sitter class node (which carried the #526 class→field CONTAINS
		// edges) with this custom model node, so without re-emitting membership
		// here every model field is left an orphan. Walk the class body and hang
		// a CONTAINS edge off the model node for each `<attr> = ...Field(...)`
		// declaration, plus a REFERENCES edge to the target model for relational
		// fields (ForeignKey / OneToOneField / ManyToManyField, string- or
		// symbol-target, including 'self'). The field entity itself is emitted by
		// the base Python extractor as `<Class>.<attr>` (SCOPE.Schema/field); the
		// CONTAINS ToID names it by qualified Name so the resolver binds it.
		seenMember := map[string]bool{}
		// (a) CONTAINS membership for every class-body attribute (fields,
		// choices constants, manager attachments) — restores parity with the
		// base class node that MergeWithCustom replaces.
		for _, aIdx := range allMatchesIndex(djangoModelAttrRe, body) {
			attr := body[aIdx[2]:aIdx[3]]
			if attr == "" || seenMember[attr] || strings.HasPrefix(attr, "__") {
				continue
			}
			seenMember[attr] = true
			modelEnt.Relationships = append(modelEnt.Relationships,
				containsFieldEdge(className, className+"."+attr, attr, "django"))
		}
		// (b) REFERENCES to the target model for relational fields
		// (ForeignKey / OneToOneField / ManyToManyField), string- or
		// symbol-target including 'self'.
		for _, fIdx := range allMatchesIndex(djangoModelFieldRe, body) {
			attr := body[fIdx[2]:fIdx[3]]
			rhs := body[fIdx[4]:fIdx[5]]
			if attr == "" || !isDjangoRelationalField(rhs) {
				continue
			}
			fullRHS := body[fIdx[0]:min(fIdx[0]+400, len(body))]
			if target := djangoRelTarget(fullRHS, className); target != "" {
				modelEnt.Relationships = append(modelEnt.Relationships,
					referencesClassEdge(className+"."+attr, target, "django", attr))
			}
		}
		out = append(out, modelEnt)
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
		var memberEdges []types.RelationshipRecord
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
			// Issue #4366 — form-field membership. The form_class node below
			// replaces the base class node; carry CONTAINS so the field is not
			// orphaned.
			memberEdges = append(memberEdges,
				containsFieldEdge(className, className+"."+fieldName, fieldName, "django"))
		}
		if len(fieldNames) > 0 {
			formEnt := entity(className, "SCOPE.Schema", "form_class", file.Path, classLine,
				map[string]string{
					"framework":    "django",
					"pattern_type": "form_class",
					"field_names":  strings.Join(fieldNames, ","),
					"field_types":  strings.Join(fieldTypes, ","),
				})
			formEnt.Relationships = append(formEnt.Relationships, memberEdges...)
			out = append(out, formEnt)
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

	// 16. Global cross-cutting wiring (issue #4379).
	// MIDDLEWARE / AUTHENTICATION_BACKENDS / REST_FRAMEWORK DEFAULT_*_CLASSES
	// register middleware/auth/permission/renderer/backend classes app-wide by
	// dotted string path. Emit a synthetic `django_settings` entity that owns
	// one global USES edge per bound class so the otherwise-orphan classes are
	// connected and the global scope is queryable (mirrors NestJS #4329).
	if loc := djangoSettingsAnchorRe.FindStringIndex(source); loc != nil {
		settingsLine := lineOf(source, loc[0])
		if ent := extractDjangoGlobalWiring(source, file.Path, settingsLine); ent != nil {
			out = append(out, *ent)
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// isDjangoRelationalField reports whether a field constructor RHS (the matched
// `models.XField` head) is a relational field carrying a target-model argument.
func isDjangoRelationalField(rhs string) bool {
	return strings.HasSuffix(rhs, "ForeignKey") ||
		strings.HasSuffix(rhs, "OneToOneField") ||
		strings.HasSuffix(rhs, "ManyToManyField")
}

// djangoRelTarget extracts the bare target-model class name from a relational
// field declaration's argument blob. Handles the string form
// (`ForeignKey('app.Model', ...)` / `ForeignKey('self', ...)`) and the symbol
// form (`ForeignKey(Model, ...)` / `ForeignKey(to=Model, ...)`). For 'self' it
// returns the enclosing model class so the edge self-references the owner.
// Returns "" when no recognizable target is present (e.g. lazy callables).
func djangoRelTarget(rhs, ownerClass string) string {
	m := djangoModelRelTargetRe.FindStringSubmatch(rhs)
	if m == nil {
		return ""
	}
	if m[1] != "" { // string form
		raw := m[1]
		if raw == "self" {
			return ownerClass
		}
		if dot := strings.LastIndexByte(raw, '.'); dot >= 0 {
			return raw[dot+1:] // strip app_label
		}
		return raw
	}
	return m[2] // symbol form
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

// djangoLeafName returns the trailing dotted segment of an expression
// ("signals.post_save" → "post_save", "post_save" → "post_save"). Used to
// normalise both the signal and the receiver/sender names in the imperative
// signal-connect form (#4789).
func djangoLeafName(expr string) string {
	expr = strings.TrimSpace(expr)
	if i := strings.LastIndex(expr, "."); i >= 0 && i+1 < len(expr) {
		return expr[i+1:]
	}
	return expr
}

// djangoKnownSignals is the set of Django signal names whose `.connect(...)`
// call is a signal registration (#4789). Gating on this set keeps the
// `<x>.connect(...)` matcher from wiring unrelated `.connect()` calls (DB
// connections, sockets, custom non-signal objects). Covers the built-in
// model/request/migration/auth signals plus the generic `Signal()` convention
// names; custom signals declared as `Signal()` are commonly suffixed
// `_signal`/`_done`/`_started` but cannot be enumerated, so only the built-ins
// (the overwhelmingly common case) are recognised — a conservative gate.
var djangoKnownSignals = map[string]bool{
	"pre_init":              true,
	"post_init":             true,
	"pre_save":              true,
	"post_save":             true,
	"pre_delete":            true,
	"post_delete":           true,
	"m2m_changed":           true,
	"pre_migrate":           true,
	"post_migrate":          true,
	"request_started":       true,
	"request_finished":      true,
	"got_request_exception": true,
	"setting_changed":       true,
	"template_rendered":     true,
	"connection_created":    true,
	"user_logged_in":        true,
	"user_logged_out":       true,
	"user_login_failed":     true,
}

// appendDRFSerializerEdges resolves a DRF view's request/response serializer
// from its class body and hangs ACCEPTS_INPUT (request) / RETURNS (response)
// edges off the view entity (#4474). It mirrors the NestJS handler→DTO edge
// kind/shape (RelationshipKindAcceptsInput / RelationshipKindReturns, ToID
// `Class:<serializer>`) so cross-framework request/response-shape diffs join
// uniformly. The serializer entity already exists (django section 5); this only
// adds the EDGE — no duplicate DTO node. Conservative: only serializer names
// that resolve to a real `XSerializer`-shaped class.
func appendDRFSerializerEdges(ent *types.EntityRecord, body string) {
	seen := map[string]bool{} // dedup (kind+serializer)
	addEdge := func(serializer, kind, matchSource string) {
		if serializer == "" {
			return
		}
		key := kind + ":" + serializer
		if seen[key] {
			return
		}
		seen[key] = true
		ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
			ToID: pyClassRef(serializer),
			Kind: kind,
			Properties: map[string]string{
				"framework":    "drf",
				"language":     "python",
				"dto_type":     serializer,
				"match_source": matchSource,
				"provenance":   "INFERRED_FROM_DRF_VIEW_SERIALIZER",
			},
		})
	}

	// (a) class-level `serializer_class = FooSerializer` — the canonical DRF
	// request+response DTO. It shapes both the deserialized request body and the
	// serialized response, so emit BOTH edges (NestJS treats a @Body DTO and a
	// Promise<DTO> return symmetrically; DRF's serializer_class is both).
	for _, m := range djangoSerializerClassAttrRe.FindAllStringSubmatch(body, -1) {
		ser := m[1]
		addEdge(ser, string(types.RelationshipKindAcceptsInput), "serializer_class_attr")
		addEdge(ser, string(types.RelationshipKindReturns), "serializer_class_attr")
	}

	// (b) inline serializer construction inside an action method:
	//   FooSerializer(data=request.data)    → request  (ACCEPTS_INPUT)
	//   FooSerializer(obj) / (qs, many=True) → response (RETURNS)
	for _, m := range djangoSerializerCallRe.FindAllStringSubmatch(body, -1) {
		ser := m[1]
		if m[2] != "" { // had `data=` → request deserialization
			addEdge(ser, string(types.RelationshipKindAcceptsInput), "serializer_data_call")
		} else { // positional instance/queryset → response serialization
			addEdge(ser, string(types.RelationshipKindReturns), "serializer_response_call")
		}
	}

	// (c) drf-yasg `@swagger_auto_schema(request_body=FooSerializer, responses=...)`.
	if rb := djangoSwaggerRequestBodyRe.FindStringSubmatch(body); rb != nil {
		if strings.HasSuffix(rb[1], "Serializer") {
			addEdge(rb[1], string(types.RelationshipKindAcceptsInput), "swagger_request_body")
		}
	}
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
