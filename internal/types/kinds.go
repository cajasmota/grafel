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
	}
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
	RelationshipKindExemplar      RelationshipKind = "EXEMPLAR"       // Pattern -> Entity: real code example of this pattern in use
	RelationshipKindTouches       RelationshipKind = "TOUCHES"        // Pattern -> Entity: entity the pattern's steps read or modify
	RelationshipKindAntiExemplar  RelationshipKind = "ANTI_EXEMPLAR"  // Pattern -> Entity: real code example of the anti-pattern
	RelationshipKindSupersedes    RelationshipKind = "SUPERSEDES"     // Pattern -> Pattern: this pattern replaces an older one
	RelationshipKindConflictsWith RelationshipKind = "CONFLICTS_WITH" // Pattern -> Pattern: these two patterns cannot both apply
	RelationshipKindCoAppliesWith RelationshipKind = "CO_APPLIES_WITH" // Pattern -> Pattern: typically applied together
	RelationshipKindPrerequisite  RelationshipKind = "PREREQUISITE"   // Pattern -> Pattern: must be satisfied before this one
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
