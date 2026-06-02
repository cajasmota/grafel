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
	EntityKindOperation   EntityKind = "SCOPE.Operation"
	EntityKindComponent   EntityKind = "SCOPE.Component"
	EntityKindClass       EntityKind = "SCOPE.Class"
	EntityKindFunction    EntityKind = "SCOPE.Function"
	EntityKindSchema      EntityKind = "SCOPE.Schema"
	EntityKindVariable    EntityKind = "SCOPE.Variable"
	EntityKindReference   EntityKind = "SCOPE.Reference"
	EntityKindPattern     EntityKind = "SCOPE.Pattern"
	EntityKindEvolution   EntityKind = "SCOPE.Evolution"
	EntityKindEndpoint    EntityKind = "SCOPE.Endpoint"
	EntityKindRoute       EntityKind = "SCOPE.Route"
	EntityKindService     EntityKind = "SCOPE.Service"
	EntityKindView        EntityKind = "SCOPE.View"
	EntityKindUIComponent EntityKind = "SCOPE.UIComponent"
	EntityKindJSX         EntityKind = "SCOPE.JSX"
	EntityKindStylesheet  EntityKind = "SCOPE.Stylesheet"
	EntityKindQueue       EntityKind = "SCOPE.Queue"
	EntityKindEvent       EntityKind = "SCOPE.Event"
	EntityKindDatastore   EntityKind = "SCOPE.Datastore"
	EntityKindDataAccess  EntityKind = "SCOPE.DataAccess"
	// #3628 [schema]: synthetic per-(repo,table) convergence node. The
	// migration-schema-ops engine pass creates one SCOPE.Table per logical
	// table and points both migration MODIFIES_TABLE edges and (existing)
	// query ACCESSES_TABLE accessors at it, so "what touches table X" unifies
	// schema evolution (migrations) with data access (queries) on one node.
	EntityKindTable         EntityKind = "SCOPE.Table"
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

	// CLI command entry-point detection (epic #3628). A SCOPE.Command is a
	// statically-declared command-line command — the CLI sibling of an HTTP
	// endpoint. Covers click/argparse/typer (Python), commander/yargs/oclif
	// (Node), cobra (Go), picocli/Spring Shell (Java), and Thor/Rake (Ruby).
	// One entity per statically-named command (or subcommand path). The
	// command's handler function is joined via a HANDLES_COMMAND edge
	// (Command → handler fn), modelling the CLI entry-point → handler flow.
	// Dynamic command names / handler refs are skipped (honest-partial).
	EntityKindCommand EntityKind = "SCOPE.Command"

	// #3704 (epic #3628, area #20): finite-state-machine (FSM) topology.
	// A single declared state in an application-level state machine — XState
	// (JS/TS), Ruby AASM, Spring StateMachine (Java), or the Python
	// `transitions` library. One entity per statically-named state. Distinct
	// from EntityKindStateMachine ("SCOPE.StateMachine"), which models an AWS
	// Step Functions *whole-machine*; this models the individual nodes of the
	// state graph, connected by RelationshipKindTransitionsTo. Synthetic ID
	// shape: `state:<lib>:<machine>:<stateName>`.
	EntityKindState EntityKind = "SCOPE.State"

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

	// #3624 (epic #3607): GraphQL DataLoader N+1 batch-loader topology.
	// A DataLoader instance batches per-key fetches to avoid the N+1 query
	// problem in GraphQL field resolvers. One entity per statically-named
	// loader: a `new DataLoader(batchFn)` construction in JS/TS (the
	// `dataloader` npm package) or a `DataLoader(load_fn=batch_fn)`
	// construction in Python (the `aiodataloader` package used by
	// Strawberry/Ariadne). The loader is named by the const/field/variable it
	// is assigned to. Two edges connect it to the rest of the graph:
	//   - RelationshipKindBatches : loader → batch function (the wrapped
	//     per-key fetch function).
	//   - RelationshipKindUses    : resolver → loader, emitted at each
	//     `loader.load(id)` / `loader.loadMany(ids)` call site. This surfaces
	//     which field resolver avoids N+1 via which batch loader.
	EntityKindDataLoader EntityKind = "SCOPE.DataLoader"

	// ORM model lifecycle-hook / signal events (child of #3628 area).
	// A SCOPE.ModelEvent node represents a single persistence lifecycle event
	// on a specific model/entity — e.g. `User.post_save` (Django signal),
	// `User.after_create` (ActiveRecord callback), `Order.afterInsert`
	// (TypeORM), `User.after_insert` (SQLAlchemy event), `User.afterCreate`
	// (Sequelize hook), `User.save` (Mongoose post/pre middleware). The
	// node makes both the model and the event queryable ("what runs after a
	// User is saved?"); the handler is joined via a TRIGGERS edge
	// (ModelEvent → handler fn). The name segment is "<Model>.<event>".
	EntityKindModelEvent EntityKind = "SCOPE.ModelEvent"
	// EntityKindExceptionType is a synthetic, file-agnostic node representing a
	// single exception / error type by its (normalized, unqualified) name —
	// e.g. "ValidationError", "NotFound", "IOException", "ErrNotFound". It is
	// the convergence point for the error-flow capability (epic #3628): a type
	// raised in one function and caught in another resolve to the SAME node, so
	// the graph answers "what can this function raise?" (outbound THROWS) and
	// "where is X handled?" (inbound CATCHES). Like SCOPE.Config/config_key it
	// carries a constant synthetic SourceFile (ExceptionTypeSourceFile) so
	// EntityRecord.ComputeID(SourceFile+Kind+Name) collapses identical type
	// names across files/languages into one node. See
	// internal/extractor/exception_flow.go.
	EntityKindExceptionType EntityKind = "SCOPE.ExceptionType"

	// EntityKindExternalService is a synthetic, file-agnostic node representing a
	// single well-known third-party service a codebase integrates with via its
	// official SDK — e.g. "stripe", "twilio", "sendgrid", "aws-s3", "aws-ses",
	// "openai", "slack", "sentry", "firebase", "algolia". It is the convergence
	// point for the third-party-integration capability (epic #3628): every
	// function that calls a recognised SDK entry-point gets a DEPENDS_ON_SERVICE
	// edge to the SAME service node, so the graph answers "what third-party
	// services does this codebase integrate with, and where?" (a service's
	// inbound DEPENDS_ON_SERVICE edges are its call sites). Distinct from raw
	// HTTP-client CONSUMES_API (path-level): this is SDK-level, NAMED services.
	// Like SCOPE.ExceptionType/SCOPE.Config it carries a constant synthetic
	// SourceFile (ExternalServiceSourceFile) so EntityRecord.ComputeID
	// (SourceFile+Kind+Name) collapses the same service across files/languages
	// into one node. See internal/extractor/external_service.go.
	EntityKindExternalService EntityKind = "SCOPE.ExternalService"
	// EntityKindTemplate is a synthetic, file-agnostic node representing a
	// server-side view template by its normalized logical name/path — e.g.
	// "users/list.html", "dashboard", "welcome". It is the convergence point
	// for the view-layer capability (epic #3628): a request handler / controller
	// method renders a template, and two handlers that render the SAME template
	// resolve to ONE node, so the graph answers "what renders users/list.html?"
	// (inbound RENDERS) and "what does this handler render?" (outbound RENDERS).
	// Like SCOPE.ExceptionType it carries a constant synthetic SourceFile
	// (TemplateSourceFile) so EntityRecord.ComputeID(SourceFile+Kind+Name)
	// collapses identical template names across files/languages/frameworks
	// (Flask render_template, Django render/TemplateView, Express res.render,
	// Rails render, Spring MVC view names, Laravel view()) into one node. See
	// internal/extractor/template_render.go.
	EntityKindTemplate EntityKind = "SCOPE.Template"
	// [realtime] WS room/channel grouping (child of #3628). A SCOPE.Channel
	// node represents a real-time room / channel / group that participants
	// JOIN and that messages are BROADCAST to — the grouping layer above the
	// per-event WS endpoints (#3739). Named "channel:<name>" so a join and a
	// broadcast on the same room (e.g. socket.io `socket.join('lobby')` and
	// `io.to('lobby').emit(...)`, ActionCable `stream_from 'chat_1'` and
	// `broadcast('chat_1', ...)`, Django Channels `group_add('chat')` and
	// `group_send('chat', ...)`, Phoenix `broadcast(socket, "ev", ...)` on a
	// topic) converge on a single node. Answers "who joins / broadcasts to
	// room X?". Emitted by internal/engine/ws_channel_grouping.go.
	EntityKindChannel EntityKind = "SCOPE.Channel"
	// #3628 area (data-model): enumerated-type value-set node. A SCOPE.Enum
	// entity represents ONE enumerated type and carries its full member /
	// value-set as queryable Properties — so the graph answers "what values can
	// field X take?" for rewrite enum-parity (e.g. a Django Python Enum that a
	// NestJS rewrite must reproduce value-for-value). One entity per declared
	// enum, keyed on the declaring file + enum name (distinct same-named enums
	// in different files stay distinct — unlike the file-agnostic ExceptionType).
	// Covers Python Enum/IntEnum/StrEnum/TextChoices/IntegerChoices, TypeScript
	// `enum` + string-literal unions, Java enum, Go iota const groups, Ruby
	// ActiveRecord `enum`, and C# enum. Properties:
	//   "enum_name"    : the bare enum type name.
	//   "members"      : comma-joined member names, declaration order.
	//   "values"       : comma-joined "Name=Literal" pairs for members whose
	//                    literal value is statically known (omitted for members
	//                    with no explicit value). e.g. "RED=1, GREEN=2".
	//   "member_count" : number of members.
	//   "kind_hint"    : the source construct ("python_enum", "ts_enum",
	//                    "ts_literal_union", "java_enum", "go_iota",
	//                    "rails_enum", "csharp_enum").
	// See internal/extractor/enum_valueset.go.
	EntityKindEnum EntityKind = "SCOPE.Enum"
	// [sbom] EntityKindPackage is a synthetic, file-agnostic node representing a
	// single declared EXTERNAL package (dependency) by its ecosystem-qualified
	// name — e.g. "npm:react", "go_modules:github.com/gin-gonic/gin",
	// "pip:requests", "maven:org.springframework:spring-core", "cargo:serde".
	// It is the convergence point for the software-bill-of-materials (SBOM)
	// capability (child of epic #3628): every manifest that declares the SAME
	// package — across files, modules, AND repos in a group — resolves to ONE
	// node, so the graph answers "which repos depend on package X, and at what
	// versions?" (a package's inbound DEPENDS_ON_PACKAGE edges are its full
	// usage footprint) and conversely "what is this repo's full dependency
	// set?". Like SCOPE.ExternalService it carries a constant synthetic
	// SourceFile (PackageSourceFile) so EntityRecord.ComputeID
	// (SourceFile+Kind+Name) collapses the same ecosystem:name across every
	// manifest into a single node — distinct from the per-manifest
	// SCOPE.Component(subtype=external_dependency) record (which stays
	// file-scoped for license/version provenance). The version is NOT part of
	// the identity (two repos pinning different versions of react converge on
	// one node); per-edge `version` + `dev` scope live on the DEPENDS_ON_PACKAGE
	// edge. Emitted by internal/extractors/cross/manifest/extractor.go.
	EntityKindPackage EntityKind = "SCOPE.Package"
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
		// #3628 [schema] migration↔query table convergence node:
		EntityKindTable,
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
		// CLI command entry-points (epic #3628):
		EntityKindCommand,
		// #3704 FSM topology:
		EntityKindState,
		// #1217:
		EntityKindHTTPEndpointDefinition,
		EntityKindHTTPEndpointCall,
		HTTPEndpointKindLegacy,
		// #3100:
		EntityKindCustomValidator,
		// #3628 area #17:
		EntityKindFeatureFlag,
		EntityKindPlugin,
		// #3624 GraphQL DataLoader:
		EntityKindDataLoader,
		// ORM model lifecycle-hook / signal events (#3628 area):
		EntityKindModelEvent,
		// #3628 error-flow: synthetic exception-type convergence node.
		EntityKindExceptionType,
		EntityKindExternalService,
		// #3628 view-layer: synthetic template convergence node.
		EntityKindTemplate,
		// [realtime] WS room/channel grouping convergence node (child of #3628).
		EntityKindChannel,
		// #3628 data-model: enum / value-set node.
		EntityKindEnum,
		// [sbom] synthetic package convergence node (child of #3628).
		EntityKindPackage,
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
	// #3629: endpoint→request-DTO edge. Emitted by the request/response
	// extractors (Spring already emits ACCEPTS_INPUT/RETURNS as string
	// literals; FastAPI, Flask and ASP.NET now mirror it) from a handler/
	// endpoint operation entity → the request body DTO/schema type. Partner
	// to RETURNS so `expand`/`traces`/`payload_drift` can traverse
	// endpoint→DTO in both directions.
	RelationshipKindAcceptsInput RelationshipKind = "ACCEPTS_INPUT"
	// Issue #86: surfaced by the producer-boundary validator scan. Emitted
	// by the OpenAPI pattern extractor to associate operations with their
	// tag entities.
	RelationshipKindTaggedAs RelationshipKind = "TAGGED_AS"

	// #3639 (epic #3625): DB-migration apply-order edge. Emitted by the
	// migration-sequence engine pass (Pass 8.9) from a parent migration entity
	// to the child that must run AFTER it. Currently derived from Alembic's
	// down_revision → revision DAG (read from the migration file body); the
	// FromID migration PRECEDES the ToID migration. Lets expand/traces walk the
	// migration chain in apply order rather than re-deriving it from filenames.
	RelationshipKindPrecedes RelationshipKind = "PRECEDES"

	// #3628 [schema]: per-migration schema-operation edge. Emitted by the
	// migration-schema-ops engine pass from a migration schema-change entity
	// (Alembic op.create_table / Rails create_table / Django CreateModel /
	// TypeORM-knex-Sequelize schema ops / Flyway-Liquibase DDL) to the table
	// it mutates. The edge carries `op` (create_table|add_column|drop_column|
	// create_index|drop_table|alter_column|…), `table`, and (when known)
	// `column`. The edge's `table` property uses the SAME normalised table
	// key that ACCESSES_TABLE uses, so the shared-DB coupling pass and
	// "what touches table X" queries converge query→table access with
	// migration→table evolution on one logical table. Complements PRECEDES
	// (apply-order) with the actual operations a migration performs.
	RelationshipKindModifiesTable RelationshipKind = "MODIFIES_TABLE"

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

	// #3426: MongoDB aggregation-pipeline cross-collection join edge.
	// Emitted from the aggregating collection/model entity → the `from`
	// collection named in a `$lookup` / `$graphLookup` stage. This is the
	// implicit (application-side) join the migration needs for data-flow
	// reasoning, since MongoDB has no schema-level foreign keys. Properties
	// on the edge: local_field, foreign_field, as, stage (lookup|graphLookup),
	// pattern_type.
	RelationshipKindJoinsCollection RelationshipKind = "JOINS_COLLECTION"

	// #3611 (epic #3606): Neo4j graph-schema domain-topology edge. Emitted from
	// an @Node-annotated owner node-label entity → the target @Node entity named
	// by an @Relationship(type=…, direction=…) field. Mirrors JOINS_COLLECTION
	// for graph databases: encodes the application's domain graph schema
	// (e.g. (:Person)-[:ACTED_IN]->(:Movie)) as a traversable subgraph rather
	// than as opaque string props on a relationship component. Properties on the
	// edge: rel_type (the Neo4j relationship type), direction (OUTGOING/INCOMING),
	// field_name, framework, provenance.
	RelationshipKindGraphRelates RelationshipKind = "GRAPH_RELATES"

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

	// #3624 (epic #3607): GraphQL DataLoader N+1 batch wiring.
	//   BATCHES : SCOPE.DataLoader → batch function (the wrapped per-key
	//             fetch function that the loader coalesces calls into).
	// The complementary resolver→loader edge reuses RelationshipKindUses
	// ("USES"), emitted at each `loader.load(id)` call site.
	RelationshipKindBatches RelationshipKind = "BATCHES"

	// #728: Scheduled-job and webhook edges.
	//   TRIGGERS : SCOPE.ScheduledJob → handler function/method
	//              (scheduler fires the handler on the declared schedule)
	//
	// ORM model lifecycle-hook reuse (#3628 area):
	//   TRIGGERS : SCOPE.ModelEvent:<Model>.<event> → handler function/method
	//              A model persistence lifecycle event (Django post_save,
	//              ActiveRecord after_create, TypeORM @AfterInsert, SQLAlchemy
	//              after_insert event, Sequelize afterCreate hook, Mongoose
	//              post('save') middleware) fires the handler. Same edge kind as
	//              the scheduler→handler case: "this event runs that function".
	RelationshipKindTriggers RelationshipKind = "TRIGGERS"

	// #3700 (child of #3628 area #14): background-job enqueue topology.
	//   ENQUEUES : enclosing function (the caller) → SCOPE.ScheduledJob job
	//              entity. Emitted at a dispatch call site that pushes work
	//              onto a background-job queue for asynchronous execution —
	//              e.g. Sidekiq `Worker.perform_async/perform_in/perform_at`,
	//              Resque/Que enqueue calls. This is the queue-system
	//              counterpart of TRIGGERS (scheduler→handler): TRIGGERS says
	//              "fired on a schedule", ENQUEUES says "a caller pushed this
	//              job onto the queue". Distinct from PUBLISHES_TO, which
	//              carries broker pub/sub fan-out semantics.
	RelationshipKindEnqueues RelationshipKind = "ENQUEUES"

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

	// CLI command entry-point edges (epic #3628). HANDLES_COMMAND is the CLI
	// sibling of an HTTP endpoint's HANDLES edge: it joins a SCOPE.Command
	// entity to the function that runs when that command is invoked
	// (Command → handler fn). Emitted by applyCLICommandEdges for
	// click/argparse/typer (Python), commander/yargs/oclif (Node), cobra (Go),
	// picocli/Spring Shell (Java), and Thor/Rake (Ruby).
	RelationshipKindHandlesCommand RelationshipKind = "HANDLES_COMMAND"

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

	// #3217 — Go build-system fusion (Cluster 6).
	//   MAGE_DEPENDS_ON : mage_target → mage_target
	//     Emitted by internal/extractors/mage for every target named in a
	//     mg.Deps / mg.SerialDeps / mg.CtxDeps call within a Mage target's
	//     body. The ToID resolves to a sibling target entity when known, or a
	//     stable synthetic ID for an unrecognised dependency.
	//   TASK_DEPENDS_ON : task → task
	//     Emitted by internal/extractors/task for every deps: prerequisite and
	//     every { task: <name> } command reference in a Taskfile task. The
	//     ToID resolves within the same Taskfile, or a synthetic ID for a
	//     namespaced/cross-file dependency.
	RelationshipKindMageDependsOn RelationshipKind = "MAGE_DEPENDS_ON"
	RelationshipKindTaskDependsOn RelationshipKind = "TASK_DEPENDS_ON"

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

	// #3628 area #22: SCOPED request-input → sink dataflow edge. Emitted by
	// the dataflow pass (internal/links/dataflow_pass.go) for a value that
	// travels from an HTTP request input (req.body.X, request.data['x'], …)
	// to a recognised sink (DB write / outbound HTTP call / response body)
	// within a single function body, or across exactly one local-call hop.
	// The FROM endpoint is the request-handler entity that reads the input;
	// the TO endpoint is the sink entity/call. Properties carry:
	//   "field"     : the source field name when statically known
	//   "sink_kind" : "db_write" | "http_call" | "response"
	//   "sink"      : the sink callee/expression as written
	//   "hop_via"   : the local function name for a one-hop flow (else absent)
	// SCOPED & honest-partial: this is NOT full taint analysis. Flows that
	// cannot be soundly followed (reassignment, branch merges, collection
	// mutation, >1 hop, cross-file) are dropped, never fabricated.
	RelationshipKindDataFlowsTo RelationshipKind = "DATA_FLOWS_TO"

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

	// #3100: Bean Validation custom ConstraintValidator<A,T> implementations.
	// CustomValidator represents a class that implements ConstraintValidator<A,T>
	// and provides the validation logic for a custom constraint annotation.
	// Subtype "constraint_validator" is used to distinguish from generic component types.
	EntityKindCustomValidator EntityKind = "SCOPE.CustomValidator"

	// #3628 area #17: Feature-flag entities. FeatureFlag represents a single
	// feature-flag key checked at runtime via a flag-management SDK
	// (LaunchDarkly, Unleash, OpenFeature, Flipper, Flagsmith) or a generic
	// config flag. Cross-repo / cross-file identity = the flag key string,
	// encoded in the synthetic ID `feature:<flag-key>`. One entity per
	// distinct key per repo; the enclosing function that checks the flag
	// links to it via GATED_BY so the graph can answer "what code is gated
	// by flag X" (flag blast-radius). Subtype = the SDK that detected it
	// ("launchdarkly", "unleash", "openfeature", "flipper", "flagsmith").
	EntityKindFeatureFlag EntityKind = "SCOPE.FeatureFlag"

	// #3628 area #25: Plugin / extension-system entities. Plugin represents a
	// single plugin / extension that a build tool, application, or framework
	// registers — e.g. a Webpack/Vite/Rollup plugin in a bundler config, a
	// Babel/ESLint plugin, a pytest plugin or setuptools entry-point plugin, or
	// a Maven/Gradle build plugin. Cross-file identity = the plugin's name
	// string within its ecosystem, encoded in the synthetic ID
	// `plugin:<ecosystem>:<name>`. The config / build file that declares the
	// plugin links to it via REGISTERS_PLUGIN so the graph can answer "which
	// plugins does this build/app register". Subtype = the registering system
	// ("webpack", "vite", "rollup", "babel", "eslint", "pytest",
	// "setuptools", "maven", "gradle").
	EntityKindPlugin EntityKind = "SCOPE.Plugin"

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

	// #3520: Kustomize overlay-patch edge. Emitted by the YAML extractor's
	// Kustomize flavor for every entry in `patches:` / `patchesStrategicMerge:`
	// / `patchesJson6902:`. The edge goes from the kustomization entity →
	// a synthetic patch-target stub ("kustomize_target:<Kind>/<name>" when the
	// patch declares a target, or "kustomize_patch_file:<path>" for a bare
	// strategic-merge file reference). Properties:
	//   "patch_style" : "strategic_merge" | "json6902" | "inline"
	//   "target_kind" : the K8s Kind named by the patch target (when present)
	//   "target_name" : the metadata.name named by the patch target (when present)
	// Lets the overlay graph show which base resources an overlay mutates.
	// Append-only — never modifies existing entities or edges.
	RelationshipKindPatches RelationshipKind = "PATCHES"

	// #3526: Helm chart edges. Emitted by the YAML extractor's Helm flavors.
	//
	//   BINDS    : a templates/*.yaml entity → a values.yaml "values_key"
	//              entity, for every `{{ .Values.<dotted.path> }}` reference in
	//              the template. ToID is the synthetic stub
	//              "helm_values:<dotted.path>" which matches the QualifiedName of
	//              the values_key entity emitted from values.yaml, so the edge
	//              resolves cross-file within the chart. Properties:
	//                "binding_kind" : "helm_values_ref"
	//                "values_path"  : the dotted path under .Values
	//
	//   INCLUDES : a template / helper → a named template ("helm_template:<name>")
	//              for every `{{ include "name" . }}` or `{{ template "name" . }}`
	//              reference. ToID matches the QualifiedName of the
	//              named_template entity emitted from _helpers.tpl. Properties:
	//                "include_kind"  : "helm_include"
	//                "template_name" : the named-template name
	//
	// Both are append-only — they never modify existing entities or edges.
	RelationshipKindBinds    RelationshipKind = "BINDS"
	RelationshipKindIncludes RelationshipKind = "INCLUDES"

	// #3552: Helm parent-chart values override edge. Emitted by the YAML
	// extractor's Helm values flavor when a parent chart's values.yaml carries a
	// top-level block whose key matches a declared subchart (name or alias) in the
	// sibling Chart.yaml. Each nested key under that block overrides the
	// subchart's own values key — the cross-chart values data-flow. The edge goes
	// from the parent values.yaml entity → a synthetic subchart-values stub
	// "helm_subchart_values:<subchart>:<dotted.path>". Properties:
	//   "override_kind" : "helm_subchart_value"
	//   "subchart"      : the subchart name (or alias) the block targets
	//   "values_path"   : the dotted path under the subchart's values root
	//   "parent_path"   : the dotted path in the parent values tree
	// Append-only — never modifies existing entities or edges.
	RelationshipKindOverrides RelationshipKind = "OVERRIDES"

	// #3632 (epic #3625): API-consumption edge. Emitted by the
	// internal/extractors/cross/consumes_api Pass-3 cross extractor for every
	// HTTP client call site whose (verb, path) matches a server endpoint
	// declared in the SAME source file (co-located client + server — BFFs, API
	// gateways, Django views that call their own API, integration-test
	// harnesses). The edge goes from the consuming caller stub
	// ("scope:component:http_caller:<file>") → the canonical endpoint entity ID
	// ("scope:endpoint:<file>#<VERB>:<canonical-path>") emitted by the
	// _cross_endpoint extractor. Properties:
	//   "method"        : the HTTP verb of the client call
	//   "matched_url"   : the raw client URL/pattern that produced the match
	//   "matched_path"  : the path component used for the join
	//   "endpoint_path" : the server endpoint's canonical path
	//   "via"           : "same_file_http_consumption" (capability tag)
	//   "provenance"    : "INFERRED_FROM_HTTP_CLIENT_CALL"
	// This is the *consumption* semantic — it is complementary to, not a
	// duplicate of, the CALLS→ExternalAPI edge emitted by _cross_httpclient and
	// the cross-repo MethodHTTP links emitted by internal/links/http_pass.go
	// (which only fires for groups of ≥2 repos via synthetic http_endpoint
	// entities). Cross-org / cross-repo consumption stays owned by the links
	// layer; this extractor never crosses a file boundary, so its org guard is
	// trivially satisfied. Append-only — never modifies existing entities/edges.
	RelationshipKindConsumesAPI RelationshipKind = "CONSUMES_API"

	// #3689 (epic #3628, area #11): OpenTelemetry tracing-span instrumentation
	// edge. Emitted by the per-language tracing-span passes (Python, Go, JS/TS,
	// Java) from the enclosing function/method operation entity → a synthetic
	// span stub ("span:<name>" for a string-literal span name, "span:<fn>" for
	// a dynamic/variable span name). The edge records WHERE a distributed-tracing
	// span is created and WHICH operation it instruments. Mirrors the
	// DISCRIMINATES_ON / BRANCHES_ON / NAVIGATES_TO precedent (enclosing op →
	// synthetic stub) so no new entity Kind is introduced. Properties:
	//   "span_name" : the static span name (omitted when dynamic)
	//   "library"   : "opentelemetry"
	//   "api"       : the idiom observed (e.g. "start_as_current_span",
	//                 "tracer.Start", "startActiveSpan", "spanBuilder",
	//                 "WithSpan")
	//   "line"      : 1-indexed source line of the span-creation site
	//   "traced"    : "true" (always; lets honest-partial dynamic-name spans
	//                 carry the flag without a fabricated name)
	//   "dynamic"   : "true" when the span name is a variable/expression rather
	//                 than a string literal (omitted otherwise)
	// Append-only — never modifies existing entities or edges.
	RelationshipKindInstruments RelationshipKind = "INSTRUMENTS"

	// #3692 (epic #3628, area #18): caching-topology edges. Emitted by the
	// per-framework caching passes (Spring @Cacheable/@CachePut/@CacheEvict,
	// Python @lru_cache/Flask-Caching/cachetools, NestJS @CacheKey/cache-manager,
	// Rails Rails.cache.fetch/delete) from the enclosing function/method
	// operation entity → a synthetic cache-region/key target entity
	// ("cache:<framework>:<region-or-key>", Kind SCOPE.Datastore, subtype
	// "cache_region"). Two directions:
	//   CACHES      : read-through / write cache population
	//                 (@Cacheable, @CachePut, lru_cache, cache.fetch{}, cache.wrap).
	//                 The function's result is stored/served under the region/key.
	//   INVALIDATES : cache eviction (@CacheEvict, Rails.cache.delete,
	//                 cacheManager.del). The function clears the region/key.
	// Multiple call-sites that touch the same region converge on one target node,
	// so CACHES and INVALIDATES edges for the same region are traversable together
	// (a producer ↔ invalidator subgraph). Properties on the edge:
	//   "framework" : spring | flask_caching | cachetools | lru_cache | nestjs |
	//                 cache_manager | rails
	//   "region"    : the cache region/name (Spring value=, Flask key_prefix,
	//                 NestJS CacheKey) when statically resolvable
	//   "key"       : the static cache key when resolvable (Rails fetch literal,
	//                 cache.wrap literal)
	//   "mode"      : read_through | write | evict | in_process
	//   "dynamic"   : "true" when the region/key is a runtime expression
	//                 (honest-partial: edge + mode recorded, target labelled
	//                 "<dynamic>")
	// Append-only — never modifies existing entities or edges.
	RelationshipKindCaches      RelationshipKind = "CACHES"
	RelationshipKindInvalidates RelationshipKind = "INVALIDATES"
	// #3628 area #17: Feature-flag gating edge. Emitted by the engine
	// feature_flag_edges pass for every flag-check call site detected via a
	// flag-management SDK (LaunchDarkly, Unleash, OpenFeature, Flipper,
	// Flagsmith) or a generic config flag.
	//   GATED_BY : enclosing function/method → SCOPE.FeatureFlag entity
	//              (synthetic ID `feature:<flag-key>`). Reads "this code path
	//              is gated by feature flag X", enabling flag blast-radius.
	// Properties:
	//   "flag"    : the flag key string
	//   "sdk"     : the SDK that detected the check ("launchdarkly",
	//               "unleash", "openfeature", "flipper", "flagsmith")
	//   "method"  : the SDK call/decorator observed (e.g. "variation",
	//               "isEnabled", "getBooleanValue", "Flipper.enabled?")
	//   "line"    : 1-indexed source line of the flag-check call site
	// Dynamic flag keys (non-literal first argument) are NOT emitted — no
	// fabricated flag entity. Append-only; never modifies existing edges.
	RelationshipKindGatedBy RelationshipKind = "GATED_BY"
	// #3628 area #25: plugin / extension-system registration edge.
	//   REGISTERS_PLUGIN : config / build file (or app entry) → SCOPE.Plugin
	//                      entity (synthetic ID `plugin:<ecosystem>:<name>`).
	//                      Reads "this build/app registers plugin X", enabling
	//                      "which plugins does this build register" queries.
	// Emitted by the plugin-system pass (plugin_system_edges.go) for Webpack /
	// Vite / Rollup bundler configs, Babel / ESLint configs, pytest +
	// setuptools entry-point plugins, and Maven / Gradle build plugins.
	// Properties:
	//   "plugin"    : the plugin name string
	//   "system"    : the registering system ("webpack", "vite", "rollup",
	//                 "babel", "eslint", "pytest", "setuptools", "maven",
	//                 "gradle")
	//   "group"     : (setuptools only) the entry-point group the plugin
	//                 registers under (e.g. "flake8.extension")
	//   "line"      : 1-indexed source line of the plugin declaration
	// Dynamically-computed plugin names (non-literal) are NOT emitted — no
	// fabricated plugin entity. Append-only; never modifies existing edges.
	RelationshipKindRegistersPlugin RelationshipKind = "REGISTERS_PLUGIN"
	// #3704 (epic #3628, area #20): finite-state-machine transition edge.
	// Emitted by the FSM-topology pass (state_machine_edges.go) for XState
	// (JS/TS), Ruby AASM, Spring StateMachine (Java), and the Python
	// `transitions` library. Direction follows the transition: source state →
	// target state. Both endpoints are SCOPE.State entities. Properties:
	//   "event"   : the triggering event / trigger name (e.g. "FETCH",
	//               "activate", "START", "go"); omitted for event-less
	//               (e.g. always/eventless) transitions.
	//   "library" : the FSM library ("xstate" | "aasm" | "spring-statemachine"
	//               | "python-transitions").
	//   "machine" : the owning machine / class name.
	// Append-only — never modifies existing entities or edges.
	RelationshipKindTransitionsTo RelationshipKind = "TRANSITIONS_TO"
	// #3628 area #13: shared-database cross-service coupling edge. Emitted by
	// the project-scope shared-db-coupling pass (engine.ApplySharedDataCoupling)
	// between two synthetic Module entities that BOTH access the same table or
	// collection entity (via ACCESSES_TABLE / JOINS_COLLECTION / SCOPE.DataAccess
	// attribution). It is the cross-service data-ownership / boundary-violation
	// signal: when ≥2 distinct modules touch one table, those modules are
	// data-coupled even with no direct call/import edge between them. Properties
	// on the edge: coupling=shared_data, shared_tables (comma-joined sorted list
	// of the co-accessed table/collection names), shared_count (how many tables
	// the pair co-accesses), provenance=SHARED_DB_COUPLING. The edge is
	// undirected in meaning but emitted once per unordered module pair (the
	// lexicographically smaller module ID is FromID) so it is deterministic and
	// not double-counted.
	RelationshipKindSharesData RelationshipKind = "SHARES_DATA"

	// #3623 (epic #3607): Apollo Federation cross-subgraph entity edge.
	//   FEDERATES : the extending subgraph's `extend type Foo @key(fields:"id")`
	//               SCOPE.Component stub → the owning entity type `Foo`. The edge
	//               records that this subgraph contributes fields to an entity
	//               whose canonical definition (the @key-bearing `type Foo`) lives
	//               in another subgraph. Properties on the edge:
	//                 federation=apollo, key_fields (the @key selection set),
	//                 external_fields / requires_fields / provides_fields
	//                 (comma-joined field names carrying @external / @requires /
	//                 @provides), and import_kind=federation_extend. Emitted by
	//                 internal/extractors/graphql/graphql.go. It is the
	//                 cross-subgraph entity-ownership signal Federation gateways
	//                 use to plan query fan-out.
	RelationshipKindFederates RelationshipKind = "FEDERATES"

	// #3628 error-flow: exception / error-contract edges. Both point a
	// callable (function / method) at a synthetic SCOPE.ExceptionType node
	// (Name "exception:<Type>") so a type raised in one place and caught in
	// another converge on a single node — the join that makes the graph
	// answer "what can this function raise?" and "where is X handled?".
	//
	//   THROWS  : function/method → SCOPE.ExceptionType it can raise.
	//             Emitted only for an IDENTIFIABLE type:
	//               JS/TS   `throw new ValidationError(...)` / bare `throw new Error()`
	//               Python  `raise NotFound(...)`
	//               Java    `throw new IllegalArgumentException()` + method `throws IOException`
	//               Go      `return ErrNotFound` (named error var/type only)
	//   CATCHES : handler function/method → SCOPE.ExceptionType it catches.
	//             Emitted only when the caught type is identifiable:
	//               JS/TS   typed `catch` via `e instanceof AuthError`
	//               Python  `except (ValueError, KeyError):`
	//               Java    `catch (IOException e)`
	//               Go      `errors.Is(err, ErrNotFound)`
	//
	// Precision-first / honest-partial: dynamic-or-computed raise types, bare
	// `except:` / untyped `catch(e){}`, and anonymous inline errors
	// (`errors.New("...")`, bare `fmt.Errorf("...")`) emit NO edge — a wrong
	// THROWS/CATCHES edge would mislead error-contract analysis. See
	// internal/extractor/exception_flow.go.
	RelationshipKindThrows  RelationshipKind = "THROWS"
	RelationshipKindCatches RelationshipKind = "CATCHES"

	// #3628 third-party integration: a function/method calls a recognised
	// external-service SDK entry-point.
	//
	//   DEPENDS_ON_SERVICE : function/method → SCOPE.ExternalService node
	//                        (Name "service:<name>") it integrates with. The
	//                        target service is identified from the SDK
	//                        import/symbol context, NOT a bare method name:
	//                          Python  `stripe.Charge.create(...)`         → stripe
	//                                  `boto3.client("s3").put_object(...)` → aws-s3
	//                          JS/TS   `new Stripe(key); stripe.charges.create()` → stripe
	//                                  `sgMail.send(...)`                    → sendgrid
	//                                  `new S3Client(...).send(cmd)`         → aws-s3
	//                        Optional edge property `operation` carries the SDK
	//                        call (e.g. "charges.create", "put_object").
	//
	// Precision-first / honest-partial: a dynamic boto3 service string
	// (`boto3.client(svc_var)`) resolves to aws-generic; an unrecognised SDK or
	// a bare `.create()`/`.send()` on a non-SDK object emits NO edge — a wrong
	// integration edge would mislead "what services do we depend on?". See
	// internal/extractor/external_service.go.
	RelationshipKindDependsOnService RelationshipKind = "DEPENDS_ON_SERVICE"
	// [realtime] WS room/channel grouping (child of #3628). Both edges point a
	// callable (function / method) at a synthetic SCOPE.Channel node
	// (Name "channel:<room>") so a join and a broadcast on the SAME room
	// converge on one node — the join that makes the graph answer
	// "who joins / broadcasts to room X?".
	//
	//   JOINS_CHANNEL : function/method → SCOPE.Channel it subscribes a
	//                   participant to. Emitted for an IDENTIFIABLE literal room:
	//                     socket.io  `socket.join('room1')`
	//                     ActionCable `stream_from 'chat_1'` / `stream_for @x`(skip dynamic)
	//                     Django Channels `self.channel_layer.group_add('chat', ...)`
	//   BROADCASTS_TO : function/method → SCOPE.Channel it publishes a message to.
	//                     socket.io  `io.to('room1').emit('ev')` /
	//                                `socket.broadcast.to('room1').emit()` /
	//                                `io.in('room1').emit()`
	//                     ActionCable `ActionCable.server.broadcast('chat_1', ...)` /
	//                                 `ChatChannel.broadcast_to(room, ...)`(skip dynamic)
	//                     Django Channels `self.channel_layer.group_send('chat', ...)`
	//                     Phoenix     `broadcast(socket, "ev", payload)` (topic) /
	//                                 `MyApp.Endpoint.broadcast("room:1", ...)`
	//
	// Precision-first / honest-partial: dynamic room names (a bare variable, a
	// template literal) emit NO edge, and a non-socket `.join` (array join) is
	// rejected by requiring a socket/cable/channels context. See
	// internal/engine/ws_channel_grouping.go.
	RelationshipKindJoinsChannel RelationshipKind = "JOINS_CHANNEL"
	RelationshipKindBroadcastsTo RelationshipKind = "BROADCASTS_TO"
	// #3628 data-model: field/param → enum value-set edge. Emitted from a
	// model field, struct field, or parameter whose declared type is a
	// SCOPE.Enum value-set node, to that node. Lets the graph answer "which
	// fields are constrained to enum X's values?" — the inbound side of the
	// enum-parity contract. ToID is the SCOPE.Enum entity's QualifiedName so
	// the resolver binds it via the byQualifiedName exact-match tier. Properties:
	//   "enum"  : the bare enum type name.
	//   "field" : the field/param name carrying the type (when known).
	// Honest-partial: only statically-resolvable enum-typed declarations emit
	// this edge; dynamic / computed types do not. See
	// internal/extractor/enum_valueset.go.
	RelationshipKindTypedAs RelationshipKind = "TYPED_AS"
	// [sbom] DEPENDS_ON_PACKAGE points a manifest's project-anchor entity at a
	// synthetic SCOPE.Package node (Name "package:<ecosystem>:<name>") it
	// declares as an external dependency. Distinct from the file-scoped
	// DEPENDS_ON(kind=external_dependency) edge: this edge targets the
	// CONVERGED, file/repo-agnostic package node so the SAME package declared in
	// many repos shares one inbound set — the graph's software bill-of-materials
	// (SBOM). ToID is the package entity's QualifiedName so the resolver binds
	// it via the byQualifiedName exact-match tier. Edge properties:
	//   "package_manager" : ecosystem (npm, pip, go_modules, maven, gradle,
	//                       cargo, bundler, composer, ...).
	//   "version"         : the declared version range / pin (verbatim; "" when
	//                       the manifest omits it — honest-partial).
	//   "dev"             : "true" when the package is a dev/test-only dependency.
	//   "dependency_kind" : runtime|dev|peer|locked|indirect (from the manifest).
	// Emitted by internal/extractors/cross/manifest/extractor.go.
	RelationshipKindDependsOnPackage RelationshipKind = "DEPENDS_ON_PACKAGE"
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
		// #3629:
		RelationshipKindAcceptsInput,
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
		RelationshipKindJoinsCollection,
		// #3611 (epic #3606):
		RelationshipKindGraphRelates,
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
		// #3624 GraphQL DataLoader:
		RelationshipKindBatches,
		// #728 scheduled jobs + webhooks:
		RelationshipKindTriggers,
		// #725 gRPC:
		RelationshipKindGRPCImplements,
		RelationshipKindGRPCHandles,
		// #925 serverless:
		RelationshipKindHandles,
		// CLI command entry-points (epic #3628):
		RelationshipKindHandlesCommand,
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
		// #3217 Go build-system fusion (mage / task):
		RelationshipKindMageDependsOn,
		RelationshipKindTaskDependsOn,
		// #2666 discriminator comparison edges:
		RelationshipKindDiscriminatesOn,
		// #2885 general branch-condition edges:
		RelationshipKindBranchesOn,
		// #2761 substrate Phase 0:
		RelationshipKindResolvesTo,
		// #3628 area #22 scoped request-input → sink dataflow:
		RelationshipKindDataFlowsTo,
		// #2904 request-validation / DTO-extraction linkage:
		RelationshipKindValidates,
		// #3520 Kustomize overlay-patch edge:
		RelationshipKindPatches,
		// #3526 Helm chart edges:
		RelationshipKindBinds,
		RelationshipKindIncludes,
		// #3552 Helm parent-chart values override edge:
		RelationshipKindOverrides,
		// #3632 API-consumption edge (same-file client→endpoint):
		RelationshipKindConsumesAPI,
		// #3689 OpenTelemetry tracing-span instrumentation edge:
		RelationshipKindInstruments,
		// #3692 (epic #3628, area #18) caching-topology edges:
		RelationshipKindCaches,
		RelationshipKindInvalidates,
		// #3628 area #17 feature-flag gating edge:
		RelationshipKindGatedBy,
		// #3628 area #25 plugin / extension-system registration:
		RelationshipKindRegistersPlugin,
		// #3704 FSM state-transition edge:
		RelationshipKindTransitionsTo,
		// #3628 area #13 shared-database cross-service coupling edge:
		RelationshipKindSharesData,
		// #3623 (epic #3607) Apollo Federation cross-subgraph entity edge:
		RelationshipKindFederates,
		// #3639 (epic #3625) DB-migration apply-order edge (Alembic chain):
		RelationshipKindPrecedes,
		// #3628 [schema] per-migration schema-operation edge (migration→table):
		RelationshipKindModifiesTable,
		// #3628 error-flow: THROWS / CATCHES exception-type edges:
		RelationshipKindThrows,
		RelationshipKindCatches,
		RelationshipKindDependsOnService,
		// [sbom] manifest project-anchor → synthetic package convergence node.
		RelationshipKindDependsOnPackage,
		// [realtime] WS room/channel grouping: JOINS_CHANNEL / BROADCASTS_TO.
		RelationshipKindJoinsChannel,
		RelationshipKindBroadcastsTo,
		// #3628 data-model: field/param → enum value-set edge.
		RelationshipKindTypedAs,
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
