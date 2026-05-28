package types

// EntityKind enumerates the SCOPE.* entity kinds emitted by archigraph
// extractors. Producers should use these typed constants rather than
// free-form string literals so that drift between code and SCHEMA.md
// (Issue #77) stays detectable at compile time.
//
// The on-disk graph keeps the namespaced "SCOPE." form. The MCP rendering
// layer strips the prefix when surfacing kinds to the agent (ADR-0003).
type EntityKind string

const (
	EntityKindOperation     EntityKind = "SCOPE.Operation"
	EntityKindComponent     EntityKind = "SCOPE.Component"
	EntityKindClass         EntityKind = "SCOPE.Class"
	EntityKindFunction      EntityKind = "SCOPE.Function"
	EntityKindSchema        EntityKind = "SCOPE.Schema"
	EntityKindVariable      EntityKind = "SCOPE.Variable"
	EntityKindReference     EntityKind = "SCOPE.Reference"
	EntityKindPattern       EntityKind = "SCOPE.Pattern"
	EntityKindEvolution     EntityKind = "SCOPE.Evolution"
	EntityKindEndpoint      EntityKind = "SCOPE.Endpoint"
	EntityKindRoute         EntityKind = "SCOPE.Route"
	EntityKindService       EntityKind = "SCOPE.Service"
	EntityKindView          EntityKind = "SCOPE.View"
	EntityKindUIComponent   EntityKind = "SCOPE.UIComponent"
	EntityKindJSX           EntityKind = "SCOPE.JSX"
	EntityKindStylesheet    EntityKind = "SCOPE.Stylesheet"
	EntityKindQueue         EntityKind = "SCOPE.Queue"
	EntityKindEvent         EntityKind = "SCOPE.Event"
	EntityKindDatastore     EntityKind = "SCOPE.Datastore"
	EntityKindDataAccess    EntityKind = "SCOPE.DataAccess"
	EntityKindExternalAPI   EntityKind = "SCOPE.ExternalAPI"
	EntityKindInfraResource EntityKind = "SCOPE.InfraResource"
	EntityKindCodeBlock     EntityKind = "SCOPE.CodeBlock"
	EntityKindDocument      EntityKind = "SCOPE.Document"
	EntityKindHeading       EntityKind = "SCOPE.Heading"
	EntityKindScopeUnknown  EntityKind = "SCOPE.ScopeUnknown"
	// Documented-but-previously-undocumented kinds (Issue #77 reconciliation):
	EntityKindExternal EntityKind = "SCOPE.External"
	EntityKindProject  EntityKind = "SCOPE.Project"
	EntityKindConfig   EntityKind = "SCOPE.Config"
	EntityKindModel    EntityKind = "SCOPE.Model"

	// #2839: Named constant declarations (e.g. assembly equ/const directives).
	// Registered as a first-class kind so producers across all low-level
	// languages can emit constant symbols without reusing SCOPE.Schema (which
	// carries "data type / field" semantics). Subtype "equate" is used by the
	// assembly extractor; future producers may use other subtypes (e.g. "define"
	// for C preprocessor macros, "const" for Zig/Rust compile-time constants).
	EntityKindConstant EntityKind = "SCOPE.Constant"
	// AgentPattern is the kind for agent-learned Pattern entities introduced
	// in ADR-0018. Stored as "AgentPattern" (no SCOPE. prefix) to distinguish
	// from the structural SCOPE.Pattern kind used by static-analysis extractors.
	EntityKindAgentPattern EntityKind = "AgentPattern"

	// #1884: Python package-level module entities. One Module entity is emitted
	// per Python package boundary (__init__.py or namespace package). Kind="Module"
	// intentionally matches the synthetic module aggregation kind used by
	// internal/module (KindModule="Module") so that kind_filter=Module queries
	// surface both extracted and synthetic module nodes in a single pass.
	// Subtype="package" distinguishes extractor-produced Module entities from
	// the synthetic ones emitted by the aggregation layer (subtype="").
	EntityKindModule EntityKind = "Module"
	// #726 wave 1: Kafka producer/consumer cross-repo edges. MessageTopic
	// represents a message-broker topic. Cross-repo identity = the topic
	// name string (per-broker namespace via the `broker` property). Wave 1
	// is Kafka-only; wave 2 will reuse the kind for RabbitMQ, SQS, NATS,
	// and Pub/Sub.
	EntityKindMessageTopic EntityKind = "SCOPE.MessageTopic"

	// #725: gRPC service definitions + client/server cross-repo edges.
	//   GrpcService represents a gRPC service implementation (server) or stub
	//   (client). Cross-repo identity is the service name.
	//   GrpcMethod represents a single RPC method. Cross-repo identity is
	//   `grpc:<ServiceName>/<MethodName>` — identical on both client and server
	//   sides so the import-channel linker can join them without new linker code.
	EntityKindGrpcService EntityKind = "SCOPE.GrpcService"
	EntityKindGrpcMethod  EntityKind = "SCOPE.GrpcMethod"

	// #749: Django Model.Meta constraints=[UniqueConstraint/CheckConstraint]
	// emit SCOPE.Constraint entities bound to the parent Model via CONTAINS.
	// Subtypes: "unique" (UniqueConstraint) and "check" (CheckConstraint).
	EntityKindConstraint EntityKind = "SCOPE.Constraint"

	// #925: Serverless function invocation edges (AWS Lambda, GCP Cloud Functions,
	// Azure Functions). ServerlessFunction represents a deployed serverless
	// function. Cross-repo identity = provider-prefixed name
	// (`aws-lambda:<name>`, `gcp-cloudfunction:<name>`, `azure-function:<name>`).
	// Both the invoker side (CALLS) and the handler side (HANDLES) emit the
	// same entity ID so the import-channel linker joins them cross-repo.
	EntityKindServerlessFunction EntityKind = "SCOPE.ServerlessFunction"

	// #927: Managed event-bus entities. EventBusEvent is a synthetic entity
	// representing a routable event type on a managed event bus. Cross-repo
	// identity = bus-prefixed event key:
	//   `event:eventbridge:<source>:<detail-type>`
	//   `event:eventgrid:<topic>:<event-type>`
	//   `event:cloudevents:<ce-source>:<ce-type>`
	// Both producers (PUBLISHES_TO) and consumers / rules (SUBSCRIBES_TO) emit
	// the same synthetic ID so the existing import-channel linker joins them
	// without any new linker code.
	EntityKindEventBusEvent EntityKind = "SCOPE.EventBusEvent"

	// #1217 (Sub-A of #1115): Split http_endpoint into two distinct kinds.
	//
	// HTTPEndpointDefinition is emitted for backend handler registrations
	// (e.g. @app.route, APIRouter, Express app.get). It carries an
	// owning_backend property derived by walking the handler file path up
	// until a framework manifest (pyproject.toml, package.json, go.mod) or
	// framework marker is found. The synthetic ID retains the canonical
	// `http:<METHOD>:<path>` form so the cross-repo linker continues to
	// match producer↔consumer by Name without code changes.
	//
	// HTTPEndpointCall is emitted for consumer-side call sites (fetch, axios,
	// requests.get, etc.). It carries caller_file, caller_line, and url_kind
	// ("literal" | "template_literal" | "dynamic_baseurl"). One entity per
	// call site — no merging.
	//
	// Backward compatibility: all existing dashboard, link, and MCP query
	// code that compares e.Kind against "http_endpoint" uses the helper
	// IsHTTPEndpointKind() to match either new kind. The old kind string
	// is preserved as HTTPEndpointKindLegacy for aliasing only — no new
	// producers should emit it.
	EntityKindHTTPEndpointDefinition EntityKind = "http_endpoint_definition"
	EntityKindHTTPEndpointCall       EntityKind = "http_endpoint_call"
	// HTTPEndpointKindLegacy is the pre-#1217 kind string. Kept so that
	// on-disk graphs indexed before this release can still be read without
	// a migration step. No extractor emits this kind after #1217.
	HTTPEndpointKindLegacy EntityKind = "http_endpoint"
)

// AllEntityKinds returns every EntityKind that archigraph extractors are
// permitted to emit. Used by tests and tooling to validate that no
// producer leaks a free-form kind string.
func AllEntityKinds() []EntityKind {
	return []EntityKind{
		EntityKindOperation,
		EntityKindComponent,
		EntityKindClass,
		EntityKindFunction,
		EntityKindSchema,
		EntityKindVariable,
		EntityKindReference,
		EntityKindPattern,
		EntityKindEvolution,
		EntityKindEndpoint,
		EntityKindRoute,
		EntityKindService,
		EntityKindView,
		EntityKindUIComponent,
		EntityKindJSX,
		EntityKindStylesheet,
		EntityKindQueue,
		EntityKindEvent,
		EntityKindDatastore,
		EntityKindDataAccess,
		EntityKindExternalAPI,
		EntityKindInfraResource,
		EntityKindCodeBlock,
		EntityKindDocument,
		EntityKindHeading,
		EntityKindScopeUnknown,
		EntityKindExternal,
		EntityKindProject,
		EntityKindConfig,
		EntityKindModel,
		// #2839:
		EntityKindConstant,
		EntityKindAgentPattern,
		// #1884:
		EntityKindModule,
		EntityKindMessageTopic,
		// #725:
		EntityKindGrpcService,
		EntityKindGrpcMethod,
		// #749:
		EntityKindConstraint,
		// #925:
		EntityKindServerlessFunction,
		// #927:
		EntityKindEventBusEvent,
		// #1217:
		EntityKindHTTPEndpointDefinition,
		EntityKindHTTPEndpointCall,
		HTTPEndpointKindLegacy,
	}
}

// IsHTTPEndpointKind reports whether kind is any of the three HTTP endpoint
// entity kinds: the pre-#1217 legacy kind and the two new split kinds.
// Use this helper everywhere instead of a raw string comparison against
// "http_endpoint" so that existing code transparently handles graphs
// indexed before and after the #1217 split.
func IsHTTPEndpointKind(kind string) bool {
	switch kind {
	case string(HTTPEndpointKindLegacy),
		string(EntityKindHTTPEndpointDefinition),
		string(EntityKindHTTPEndpointCall):
		return true
	}
	return false
}

// IsHTTPEndpointDefinitionKind reports whether kind represents a backend
// handler definition (either the new dedicated kind or the legacy kind,
// which was exclusively producer-side before the #1217 split for graphs
// where pattern_type != "http_endpoint_client_synthesis").
func IsHTTPEndpointDefinitionKind(kind string) bool {
	return kind == string(EntityKindHTTPEndpointDefinition) || kind == string(HTTPEndpointKindLegacy)
}

// IsHTTPEndpointCallKind reports whether kind represents a consumer call
// site (either the new dedicated kind or the legacy kind with
// pattern_type == "http_endpoint_client_synthesis").
func IsHTTPEndpointCallKind(kind string) bool {
	return kind == string(EntityKindHTTPEndpointCall)
}

// IsValidEntityKind reports whether s is one of the typed EntityKind values.
func IsValidEntityKind(s string) bool {
	for _, k := range AllEntityKinds() {
		if string(k) == s {
			return true
		}
	}
	return false
}

// RelationshipKind enumerates the directed-edge kinds emitted by archigraph
// extractors and resolvers. As with EntityKind, producers should reference
// these constants rather than ad-hoc string literals.
type RelationshipKind string

const (
	RelationshipKindCalls         RelationshipKind = "CALLS"
	RelationshipKindImports       RelationshipKind = "IMPORTS"
	RelationshipKindExtends       RelationshipKind = "EXTENDS"
	RelationshipKindImplements    RelationshipKind = "IMPLEMENTS"
	RelationshipKindUses          RelationshipKind = "USES"
	RelationshipKindUsesHook      RelationshipKind = "USES_HOOK"
	RelationshipKindContains      RelationshipKind = "CONTAINS"
	RelationshipKindDependsOn     RelationshipKind = "DEPENDS_ON"
	RelationshipKindReferences    RelationshipKind = "REFERENCES"
	RelationshipKindRoutesTo      RelationshipKind = "ROUTES_TO"
	RelationshipKindServes        RelationshipKind = "SERVES"
	RelationshipKindPublishesTo   RelationshipKind = "PUBLISHES_TO"
	RelationshipKindTests         RelationshipKind = "TESTS"
	RelationshipKindHasProps      RelationshipKind = "HAS_PROPS"
	RelationshipKindAccessesTable RelationshipKind = "ACCESSES_TABLE"
	RelationshipKindInjectedInto  RelationshipKind = "INJECTED_INTO"
	RelationshipKindReadsFrom     RelationshipKind = "READS_FROM"
	RelationshipKindWritesTo      RelationshipKind = "WRITES_TO"
	// Documented-but-previously-undocumented relationship kinds (Issue #77):
	RelationshipKindRenders RelationshipKind = "RENDERS"
	RelationshipKindReturns RelationshipKind = "RETURNS"
	// Issue #86: surfaced by the producer-boundary validator scan. Emitted
	// by the OpenAPI pattern extractor to associate operations with their
	// tag entities.
	RelationshipKindTaggedAs RelationshipKind = "TAGGED_AS"

	// ADR-0018: Agent-learned pattern edge kinds (append-only additions).
	// Outgoing from Pattern entities:
	RelationshipKindExemplar      RelationshipKind = "EXEMPLAR"        // Pattern -> Entity: real code example of this pattern in use
	RelationshipKindTouches       RelationshipKind = "TOUCHES"         // Pattern -> Entity: entity the pattern's steps read or modify
	RelationshipKindAntiExemplar  RelationshipKind = "ANTI_EXEMPLAR"   // Pattern -> Entity: real code example of the anti-pattern
	RelationshipKindSupersedes    RelationshipKind = "SUPERSEDES"      // Pattern -> Pattern: this pattern replaces an older one
	RelationshipKindConflictsWith RelationshipKind = "CONFLICTS_WITH"  // Pattern -> Pattern: these two patterns cannot both apply
	RelationshipKindCoAppliesWith RelationshipKind = "CO_APPLIES_WITH" // Pattern -> Pattern: typically applied together
	RelationshipKindPrerequisite  RelationshipKind = "PREREQUISITE"    // Pattern -> Pattern: must be satisfied before this one
	// Incoming to Pattern:
	RelationshipKindCreatedBy RelationshipKind = "CREATED_BY" // Entity -> Pattern: entity produced using the linked pattern

	// #713: React Native / Expo platform-specific file variants.
	// Emitted from a platform-variant file entity (e.g. Button.ios.tsx)
	// to the canonical base file (Button.tsx). Lets the orphan-rate audit
	// count platform variants as "connected" to their canonical counterpart.
	RelationshipKindPlatformVariantOf RelationshipKind = "PLATFORM_VARIANT_OF"

	// #723: ORM query call site -> model class. Emitted by the engine-layer
	// applyORMQueries pass for every recognised ORM query call (Prisma,
	// Django ORM, SQLAlchemy, JPA, gorm, ActiveRecord, etc.). Properties
	// on the edge: operation, filter_keys, is_join, orm, pattern_type.
	// Closes the orphan class on model entities referenced only via ORM.
	RelationshipKindQueries RelationshipKind = "QUERIES"

	// #721: Consumer-side HTTP fetch edge. Emitted from a calling
	// function/method entity → the synthetic http_endpoint entity that
	// represents the URL the client invokes. Lets the process-flow BFS
	// and cross-repo HTTP matcher traverse directly from a caller to its
	// endpoint without re-running the post-hoc regex matcher.
	RelationshipKindFetches RelationshipKind = "FETCHES"

	// #726 wave 1: Kafka producer/consumer cross-repo edges.
	//   PUBLISHES_TO  : caller method → MessageTopic
	//   SUBSCRIBES_TO : consumer method → MessageTopic
	//   TRANSFORMS    : input topic → output topic (a single method is BOTH
	//                   @Incoming and @Outgoing — a stream transformer).
	// PUBLISHES_TO is distinct from the older PUBLISHES_TO above which is
	// already declared (RelationshipKindPublishesTo) — we reuse that
	// constant from kafka_edges.go rather than introducing a duplicate.
	RelationshipKindSubscribesTo RelationshipKind = "SUBSCRIBES_TO"
	RelationshipKindTransforms   RelationshipKind = "TRANSFORMS"

	// #727: Real-time event channel edges. Emitted by the engine-layer
	// synthesizers in internal/engine/{websocket_edges,sse_edges,
	// graphql_subscriptions}.go. All append-only — they never replace or
	// modify existing edges, so they cannot regress surrounding passes.
	//
	// WebSocket:
	//   WS_SUBSCRIBES_TO   handler  → ChannelEvent  (server: on/onMessage; channel/room as props)
	//   WS_EMITS           emitter  → ChannelEvent  (server: emit/send; scope=broadcast|room|user)
	//   WS_CONNECTS        client   → ChannelEvent  (browser/Node client construct on a channel)
	// Server-Sent Events:
	//   STREAMS_FROM       client   → Stream        (browser EventSource / polling SSE client)
	//   STREAMS_TO         server   → Stream        (server emits text/event-stream)
	// GraphQL subscriptions:
	//   GRAPHQL_SUBSCRIBES client   → Subscription  (Apollo/urql/graphql client subscription)
	//   GRAPHQL_PUBLISHES  server   → Subscription  (resolver / subscriptionType)
	RelationshipKindWSSubscribesTo    RelationshipKind = "WS_SUBSCRIBES_TO"
	RelationshipKindWSEmits           RelationshipKind = "WS_EMITS"
	RelationshipKindWSConnects        RelationshipKind = "WS_CONNECTS"
	RelationshipKindStreamsFrom       RelationshipKind = "STREAMS_FROM"
	RelationshipKindStreamsTo         RelationshipKind = "STREAMS_TO"
	RelationshipKindGraphQLSubscribes RelationshipKind = "GRAPHQL_SUBSCRIBES"
	RelationshipKindGraphQLPublishes  RelationshipKind = "GRAPHQL_PUBLISHES"

	// #728: Scheduled-job and webhook edges.
	//   TRIGGERS : SCOPE.ScheduledJob → handler function/method
	//              (scheduler fires the handler on the declared schedule)
	RelationshipKindTriggers RelationshipKind = "TRIGGERS"

	// #725: gRPC service definitions + client/server cross-repo edges.
	//   GRPC_IMPLEMENTS : handler method → GrpcMethod (server declares it implements this RPC).
	//   GRPC_HANDLES    : client call site → GrpcMethod (client invokes this RPC).
	// Cross-repo linking: both sides emit GrpcMethod with the same ID
	// `grpc:ServiceName/MethodName` so the existing import-channel linker joins them.
	RelationshipKindGRPCImplements RelationshipKind = "GRPC_IMPLEMENTS"
	RelationshipKindGRPCHandles    RelationshipKind = "GRPC_HANDLES"

	// #925: Serverless function invocation edges. HANDLES is the consumer-side
	// counterpart of CALLS — a handler function HANDLES a ServerlessFunction.
	// CALLS is already declared above (RelationshipKindCalls) and is reused here.
	RelationshipKindHandles RelationshipKind = "HANDLES"

	// #927: Managed event-bus edges. Both sides emit a synthetic EventBusEvent
	// entity with the same bus-prefixed key, so the import-channel linker joins
	// producer ↔ rule/consumer without new linker code.
	//   PUBLISHES_TO  : producer call site → EventBusEvent  (reuses existing constant)
	//   SUBSCRIBES_TO : rule / trigger handler → EventBusEvent  (reuses existing constant)
	// The two edge kinds above are already declared:
	//   RelationshipKindPublishesTo  ("PUBLISHES_TO")
	//   RelationshipKindSubscribesTo ("SUBSCRIBES_TO")
	// New kinds introduced for #927:
	//   EVENTBRIDGE_TRIGGERS : EventBridge rule entity → aws-lambda target
	//   EVENTGRID_TRIGGERS   : EventGrid subscription → Azure Function target
	//   CLOUDEVENT_FLOWS     : CloudEvent route → CloudEvent route (HTTP metadata match)
	RelationshipKindEventBridgeTriggers RelationshipKind = "EVENTBRIDGE_TRIGGERS"
	RelationshipKindEventGridTriggers   RelationshipKind = "EVENTGRID_TRIGGERS"
	RelationshipKindCloudEventFlows     RelationshipKind = "CLOUDEVENT_FLOWS"

	// #1217 (Sub-A of #1115): HTTP endpoint kind split.
	// UNRESOLVED_FETCH is emitted from an http_endpoint_call to an
	// http_endpoint_definition when the static linker cannot pair them
	// (e.g. dynamic base URL, template-literal path, or no matching
	// definition in the indexed group). Orphan calls that previously
	// required a post-hoc regex pass (#1099) become first-class graph
	// citizens via this edge kind.
	RelationshipKindUnresolvedFetch RelationshipKind = "UNRESOLVED_FETCH"

	// #1343: TypeScript type extraction edges.
	//   HAS_TYPE     : entity → SCOPE.Schema  (e.g. a variable or field whose declared type is the target schema)
	RelationshipKindHasType RelationshipKind = "HAS_TYPE"

	// #1344: Post-rebuild rename / move / split detection.
	// Emitted from a NEW entity to the OLD entity's ID after the indexer
	// detects that a function, method, or class was renamed between rebuilds.
	// Properties: confidence (0.0–1.0), old_name, old_id, method
	// (name_exact+same_file, name_fuzzy+neighborhood, moved, split, …).
	// The edge is append-only and never modifies existing entities or edges.
	RelationshipKindRenamedFrom RelationshipKind = "RENAMED_FROM"

	// #1374: Django signal/admin connectivity edges.
	//   HANDLES_SIGNAL : signal-handler function entity → sender model entity
	//                    (emitted for every @receiver(signal, sender=Model) handler).
	//   REGISTERS      : admin class entity → registered model entity
	//                    (emitted for admin.site.register(Model[, AdminClass]) and
	//                    @admin.register(Model) class AdminClass(…)).
	// Both edges use the model's bare name as the ToID structural-ref
	// ("Class:<ModelName>") so the intra-repo resolver matches the existing
	// SCOPE.Component/class or SCOPE.Schema model entity without new linker code.
	RelationshipKindHandlesSignal RelationshipKind = "HANDLES_SIGNAL"
	RelationshipKindRegisters     RelationshipKind = "REGISTERS"

	// #1708: Debezium / Kafka-Connect CDC connector edges.
	//   CAPTURES : cdc_connector → source table (one per element of the
	//              connector's table.include.list).
	// PUBLISHES_TO is reused (RelationshipKindPublishesTo) for the
	// connector → kafka:<topic> edge, so downstream Kafka consumers'
	// SUBSCRIBES_TO edges attach to the same MessageTopic node without
	// any cross-pass handoff.
	RelationshipKindCaptures RelationshipKind = "CAPTURES"

	// #1885: First-class config-entity edges. Emitted by the
	// internal/extractors/config discovery pass for project-level config
	// files (Dockerfile, Makefile, pyproject.toml, package.json, pom.xml,
	// build.gradle, application.properties, .env, …).
	//
	//   DEPENDS_ON_CONFIG : Module / file entity → SCOPE.Config entity
	//                       (the module is configured by the linked Config).
	//   CONFIGURES        : SCOPE.Config → consumer module (best-effort
	//                       directional inverse; emitted only when the
	//                       config's directory contains downstream modules).
	RelationshipKindDependsOnConfig RelationshipKind = "DEPENDS_ON_CONFIG"
	RelationshipKindConfigures      RelationshipKind = "CONFIGURES"

	// #2008: DRF SerializerMethodField → method link.
	//   RESOLVED_BY : SCOPE.Schema/field → SCOPE.Operation/method
	// Emitted for `<field> = serializers.SerializerMethodField(...)`
	// declarations, pointing at the sibling `get_<field>` (or
	// `method_name=` kwarg) operation that produces the field's value.
	// Generalisable to any "value resolved by another entity" shape;
	// kept narrow today to the DRF extractor producer.
	RelationshipKindResolvedBy RelationshipKind = "RESOLVED_BY"

	// #2142: DRF plain serializer field → custom field class.
	//   USES_SCHEMA : SCOPE.Schema/field → custom field class entity
	// Emitted by the DRF serializer-fields extractor when a plain
	// serializers.Serializer field references a capitalised, non-DRF-scalar
	// type (e.g. MoneyField, PhoneField) that may be a project-local custom
	// field class. Lets the resolver bind the field to its custom type entity
	// and prevents the field from becoming an orphan node.
	RelationshipKindUsesSchema RelationshipKind = "USES_SCHEMA"

	// #2279: Django ORM field-access edges. Emitted by the engine-layer
	// applyORMFieldEdges pass for every ORM call site (filter/get/exclude/
	// values/values_list/order_by/annotate/select_related/prefetch_related
	// → READS_FIELD; update/create/save/update_or_create/bulk_create/
	// bulk_update → WRITES_FIELD). Django lookup suffixes (__icontains,
	// __in, __gte, etc.) are stripped before field lookup. Only emitted
	// when the field node (SCOPE.Schema subtype=field, Name=<Model>.<field>)
	// resolves to an entity in the same file; cross-file resolution is
	// left to the linker. Append-only — cannot regress surrounding passes.
	// Closes the orphan class on ORM-only field references (bench Q08
	// User.cognito_id was 60+ grep hits, 0 graph edges before this pass).
	RelationshipKindReadsField  RelationshipKind = "READS_FIELD"
	RelationshipKindWritesField RelationshipKind = "WRITES_FIELD"

	// #2655: Client-side navigation edges. Emitted by the JS/TS extractor
	// for Expo Router / React Navigation / Next.js navigation call sites.
	//   NAVIGATES_TO : caller operation → route stub (synthetic "route:<path>")
	// Properties:
	//   "route"   : destination route/screen name or path
	//   "params"  : comma-separated param key names (omitted when empty)
	//   "line"    : 1-indexed source line of the navigation call
	//   "via"     : "navigation_call" (traceability tag)
	// Phase 2 (followup): dedicated archigraph_navigates MCP query tool.
	RelationshipKindNavigatesTo RelationshipKind = "NAVIGATES_TO"

	// #2183 — Monorepo M6: Bazel BUILD-graph fusion.
	//   BAZEL_DEPENDS_ON : bazel_target → bazel_target
	//     Emitted for every entry in a BUILD rule's deps= list that can be
	//     resolved to another target in the same workspace. External deps
	//     (@maven//:guava etc.) also emit this edge; the ToID is a stable
	//     synthetic ID for the external label.
	//   BAZEL_DEP_STATUS : bazel_target → bazel_target
	//     Emitted by the resolver overlay (internal/resolve/bazel_overlay.go)
	//     after cross-referencing declared BUILD deps against inferred CALLS /
	//     IMPORTS edges. Properties["status"] is one of:
	//       "declared+used"   — dep confirmed by runtime usage
	//       "declared_unused" — dep declared but no crossing call/import found
	//       "undeclared_used" — call/import crossing with no BUILD dep declared
	RelationshipKindBazelDependsOn RelationshipKind = "BAZEL_DEPENDS_ON"
	RelationshipKindBazelDepStatus RelationshipKind = "BAZEL_DEP_STATUS"

	// #2761: Constant-binding cross-file propagation edge. Emitted by the
	// Phase 0 substrate (internal/links/constant_propagation.go) once a
	// use-site identifier resolves to a declaration in another file (or
	// recursively via IMPORTS). The edge goes from the use-site entity →
	// the declaring entity. Properties:
	//   "resolved_value"  : the literal string value
	//   "resolved_via"    : provenance chain (comma-joined steps)
	//   "confidence"      : "1.00", "0.85", "0.60", ... (see propagation pass)
	// Append-only. Consumers (http_endpoint canonicalizer, taint flow,
	// payload-shape inference) read the resolved_value/confidence pair
	// without modifying the underlying entity kind.
	RelationshipKindResolvesTo RelationshipKind = "RESOLVES_TO"

	// #2666: Discriminator comparison edges. Emitted by the JS/TS and Python
	// extractors for every `identifier == literal` comparison detected in a
	// function/method body (the discriminator pattern from #2659).
	//   DISCRIMINATES_ON : enclosing operation → synthetic "var:<varName>" stub
	// Properties:
	//   "line"    : 1-indexed source line of the comparison
	//   "literal" : RHS literal value as a string (e.g. "2", "periodic")
	// Surfaced by archigraph_inspect (discriminators section) and mixed into
	// BM25 doc terms so literal-value queries (e.g. "checklistType 2") rank
	// the enclosing entity higher.
	RelationshipKindDiscriminatesOn RelationshipKind = "DISCRIMINATES_ON"

	// #2885: General branch-condition edges. Emitted by the JS/TS extractor for
	// every `if`/ternary/`switch` whose controlling expression is a comparison
	// in a function/method body — including member comparisons that the
	// narrower discriminator pass (DISCRIMINATES_ON, bare-identifier ===/!==
	// literal only) misses, e.g. real NativeScript view-model branches like
	// `if (this._x !== value)` or `if (this._counter <= 0)`.
	//   BRANCHES_ON : enclosing operation → synthetic "branch:<expr>" stub
	// Properties:
	//   "line"     : 1-indexed source line of the branch
	//   "operator" : the comparison operator (===, !==, <=, <, >, >=, ==, !=)
	//   "kind"     : "if", "ternary", or "switch"
	RelationshipKindBranchesOn RelationshipKind = "BRANCHES_ON"

	// #2904: Request-validation / DTO-extraction linkage edges. Emitted by
	// the JS/TS extractor when a route handler / controller method is wired
	// to a schema validator or a typed DTO, turning validators that were
	// previously seen only as imports into a route↔validator graph edge.
	//   VALIDATES : enclosing operation (route handler) → synthetic stub
	//     - "validator:<lib>"  for call-site validation (zod .parse/.safeParse,
	//       joi/yup .validate, express-validator validationResult/check/body,
	//       class-validator validate/validateOrReject)
	//     - "dto:<TypeName>"   for NestJS `@Body()/@Query()/@Param() x: Dto`
	//       parameter-decorator DTO extraction
	// Properties:
	//   "library" : the validator library ("zod", "joi", "yup",
	//               "express-validator", "class-validator", "nestjs-dto")
	//   "method"  : the validation method or decorator observed
	//   "dto"     : the DTO type name (dto_extraction edges only)
	//   "line"    : 1-indexed source line of the call / parameter
	//   "via"     : "request_validation" or "dto_extraction" (capability tag)
	// Append-only — never modifies existing entities or edges.
	RelationshipKindValidates RelationshipKind = "VALIDATES"
)

// AllRelationshipKinds returns every RelationshipKind producers may emit.
func AllRelationshipKinds() []RelationshipKind {
	return []RelationshipKind{
		RelationshipKindCalls,
		RelationshipKindImports,
		RelationshipKindExtends,
		RelationshipKindImplements,
		RelationshipKindUses,
		RelationshipKindUsesHook,
		RelationshipKindContains,
		RelationshipKindDependsOn,
		RelationshipKindReferences,
		RelationshipKindRoutesTo,
		RelationshipKindServes,
		RelationshipKindPublishesTo,
		RelationshipKindTests,
		RelationshipKindHasProps,
		RelationshipKindAccessesTable,
		RelationshipKindInjectedInto,
		RelationshipKindReadsFrom,
		RelationshipKindWritesTo,
		RelationshipKindRenders,
		RelationshipKindReturns,
		RelationshipKindTaggedAs,
		// ADR-0018 pattern edge kinds:
		RelationshipKindExemplar,
		RelationshipKindTouches,
		RelationshipKindAntiExemplar,
		RelationshipKindSupersedes,
		RelationshipKindConflictsWith,
		RelationshipKindCoAppliesWith,
		RelationshipKindPrerequisite,
		RelationshipKindCreatedBy,
		// #713:
		RelationshipKindPlatformVariantOf,
		// #723:
		RelationshipKindQueries,
		// #721:
		RelationshipKindFetches,
		// #726 wave 1:
		RelationshipKindSubscribesTo,
		RelationshipKindTransforms,
		// #727 real-time event channels:
		RelationshipKindWSSubscribesTo,
		RelationshipKindWSEmits,
		RelationshipKindWSConnects,
		RelationshipKindStreamsFrom,
		RelationshipKindStreamsTo,
		RelationshipKindGraphQLSubscribes,
		RelationshipKindGraphQLPublishes,
		// #728 scheduled jobs + webhooks:
		RelationshipKindTriggers,
		// #725 gRPC:
		RelationshipKindGRPCImplements,
		RelationshipKindGRPCHandles,
		// #925 serverless:
		RelationshipKindHandles,
		// #927 managed event buses:
		RelationshipKindEventBridgeTriggers,
		RelationshipKindEventGridTriggers,
		RelationshipKindCloudEventFlows,
		// #1217:
		RelationshipKindUnresolvedFetch,
		// #1343:
		RelationshipKindHasType,
		// #1344 rename detection:
		RelationshipKindRenamedFrom,
		// #1374 Django signal/admin connectivity:
		RelationshipKindHandlesSignal,
		RelationshipKindRegisters,
		// #1708 Debezium / Kafka-Connect CDC connector:
		RelationshipKindCaptures,
		// #1885 first-class config entities:
		RelationshipKindDependsOnConfig,
		RelationshipKindConfigures,
		// #2008 DRF SerializerMethodField → method link:
		RelationshipKindResolvedBy,
		// #2142 DRF plain serializer field → custom field class:
		RelationshipKindUsesSchema,
		// #2279 Django ORM field-access edges:
		RelationshipKindReadsField,
		RelationshipKindWritesField,
		// #2655 client-side navigation edges:
		RelationshipKindNavigatesTo,
		// #2183 Bazel BUILD-graph fusion:
		RelationshipKindBazelDependsOn,
		RelationshipKindBazelDepStatus,
		// #2666 discriminator comparison edges:
		RelationshipKindDiscriminatesOn,
		// #2885 general branch-condition edges:
		RelationshipKindBranchesOn,
		// #2761 substrate Phase 0:
		RelationshipKindResolvesTo,
		// #2904 request-validation / DTO-extraction linkage:
		RelationshipKindValidates,
	}
}

// IsValidRelationshipKind reports whether s is one of the typed RelationshipKind values.
func IsValidRelationshipKind(s string) bool {
	for _, k := range AllRelationshipKinds() {
		if string(k) == s {
			return true
		}
	}
	return false
}
