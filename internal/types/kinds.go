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
	// AgentPattern is the kind for agent-learned Pattern entities introduced
	// in ADR-0018. Stored as "AgentPattern" (no SCOPE. prefix) to distinguish
	// from the structural SCOPE.Pattern kind used by static-analysis extractors.
	EntityKindAgentPattern EntityKind = "AgentPattern"
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
		EntityKindAgentPattern,
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
