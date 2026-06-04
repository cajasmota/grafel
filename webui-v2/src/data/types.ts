/* ============================================================
   data/types.ts — the typed domain model.

   These shapes mirror the archigraph daemon's responses and the
   per-screen "Data model" sections in the design handoff docs.
   Screen tickets extend this file as they wire real endpoints.
   ============================================================ */

export type EdgeKind =
  | "CALLS"
  | "REFERENCES"
  | "RENDERS"
  | "DEPENDS_ON"
  | "EXTENDS"
  | "CONTAINS"
  | "IMPORTS";

export interface Repo {
  id: string;
  name: string;
  /** Primary language label (drives the Stack badge). */
  language: string;
}

export interface Community {
  id: string;
  label: string;
  /** 1-based index into the pastel categorical scale. */
  colorIndex: number;
  size: number;
}

export interface Entity {
  id: string;
  /** Qualified name — rendered in mono. */
  qualifiedName: string;
  kind: string;
  repoId: string;
  communityId: string | null;
  inbound: number;
  outbound: number;
}

/* ------------------------------------------------------------------ *
 * Graph screen — wire + domain shapes.
 *
 * The wire shapes (snake_case) mirror v2_graph.go's JSON exactly. The
 * data hook normalizes them into the camelCase domain shapes the screen +
 * cosmos canvas consume.
 * ------------------------------------------------------------------ */

/** Raw node as emitted by GET /api/v2/graph/{group}. */
export interface GraphNodeWire {
  id: string;
  label: string;
  kind: string;
  repo: string;
  degree: number;
  pagerank: number;
  community_id?: number;
  source_file?: string;
}

export interface GraphEdgeWire {
  source: string;
  target: string;
  kind: string;
}

export interface GraphCommunityWire {
  id: number;
  label: string;
  repo: string;
  size: number;
  color_index: number;
}

export interface GraphRepoWire {
  id: string;
  language: string;
  color_index: number;
}

export interface GraphPayloadWire {
  nodes: GraphNodeWire[];
  edges: GraphEdgeWire[];
  communities: GraphCommunityWire[];
  repos: GraphRepoWire[];
  total_node_count: number;
}

/** Normalized node consumed by the canvas + inspector. */
export interface GraphNode {
  id: string;
  label: string;
  kind: string;
  repo: string;
  degree: number;
  pageRank: number;
  communityId: number | null;
  sourceFile: string;
}

export interface GraphEdge {
  source: string;
  target: string;
  kind: string;
}

export interface GraphCommunity {
  id: number;
  label: string;
  repo: string;
  size: number;
  colorIndex: number;
}

export interface GraphRepo {
  id: string;
  language: string;
  colorIndex: number;
}

export interface GraphPayload {
  nodes: GraphNode[];
  edges: GraphEdge[];
  communities: GraphCommunity[];
  repos: GraphRepo[];
  totalNodeCount: number;
}

/** Tier-3 entity detail — GET /api/graph/{group}/entity/{id} (v1, reused). */
export interface EntityEdgeWire {
  from_id: string;
  to_id: string;
  kind: string;
  cross_repo?: boolean;
}
export interface EntityNeighborWire {
  id: string;
  label: string;
  kind: string;
  source_file: string;
  start_line: number;
  repo: string;
}
export interface EntityDetailWire {
  entity: {
    id: string;
    name: string;
    kind: string;
    source_file: string;
    start_line: number;
    repo?: string;
    pagerank?: number;
  };
  inbound_edges: EntityEdgeWire[];
  outbound_edges: EntityEdgeWire[];
  neighbors: EntityNeighborWire[];
  in_degree: number;
  out_degree: number;
  community_name?: string;
  betweenness?: number;
}

/** Derived health state for a group (computed server-side in v2_groups.go). */
export type GroupHealth = "healthy" | "warning" | "degraded" | "unindexed";

export interface Group {
  /** Slug — also the route param. */
  id: string;
  name: string;
  /** Top-level repo slugs. */
  repos: string[];
  entityCount: number;
  /**
   * Confidence the graph matches the codebase, 0–1. `null` when the group
   * has never been indexed. (Replaces the legacy "bug-rate".)
   */
  fidelity: number | null;
  /** ms epoch of the most-recent index across repos; `null` when never indexed. */
  indexedAt: number | null;
  health: GroupHealth;
  /**
   * Monorepo module map: parent-repo slug → list of module sub-paths.
   * Only present when the group has repos with declared modules (M3 #2180).
   * Absent for groups with no monorepo repos.
   */
  monorepos?: Record<string, string[]>;
}

// ----------------------------------------------------------------
// Docs screen types — GENERATED markdown documents (#1552).
//
// The Docs screen renders the markdown produced by the `generate-docs`
// SKILL (run by the user's coding agent), NOT the entity graph. The skill
// writes markdown under <repo>/docs/ (overview.md, modules/<slug>/README.md,
// reference/*.md, patterns/<cat>/*.md, plus cross-cutting guides). The tree
// groups documents per-repo then by category; leaves are renderable docs.
// ----------------------------------------------------------------

export type DocCategory =
  | "overview"
  | "guide"
  | "modules"
  | "reference"
  | "patterns";

export interface DocNode {
  type: "folder" | "doc";
  name: string;
  path?: string;           // doc leaf only — key for the page endpoint
  category?: DocCategory;  // top-level section
  isRepoDocs?: boolean;    // true = not skill-generated (raw repo markdown)
  children?: DocNode[];
}

export interface DocPage {
  path: string;
  title: string;
  markdown: string;
}

/** Wrapper returned by GET /api/v2/groups/:id/docs/tree */
export interface DocsTreeResponse {
  skillGenerated: boolean;
  nodes: DocNode[];
  /**
   * Separate, non-per-repo BUSINESS documentation set (capabilities, domain /
   * glossary, user journeys, business rules), surfaced under the Business
   * documentation view. Empty when no business docs exist. See #1622/#1623.
   */
  businessNodes?: DocNode[];
}

// ----------------------------------------------------------------
// Settings screen types (mirrors v2_group_settings.go wire shapes)
// ----------------------------------------------------------------

export interface SettingsFeatures {
  watchers: boolean;
  gitHooks: boolean;
}

export interface MonorepoPkg {
  path: string;
  stack: string;
  indexed: boolean;
  files: number;
}

export interface MonorepoInfo {
  detector: string;
  packages: MonorepoPkg[];
}

export interface SettingsRepo {
  slug: string;
  path: string;
  stack: string;
  files: number;
  entities: number;
  indexedAt: number | null;
  monorepo: MonorepoInfo | null;
}

export interface SettingsGroup {
  id: string;
  name: string;
  entities: number;
  fidelity: number;
  indexedAt: number | null;
  health: GroupHealth;
  features: SettingsFeatures;
  docsPath: string;
  repos: SettingsRepo[];
}

export interface DoctorCheck {
  id: string;
  label: string;
  status: "ok" | "warning" | "info" | "error";
  detail: string;
}

// ── Topology screen ──────────────────────────────────────────────────────────

/** Canonical broker identifiers used for color/shape mapping. */
export type BrokerCanonical =
  | "kafka"
  | "rabbitmq"
  | "sqs"
  | "pubsub"
  | "nats"
  | "websocket"
  | "sse"
  | "graphql_subscription"
  | "redis_pubsub"
  | "redis"
  | "redis-stream"
  | "celery"
  | "task-queue"
  | "serverless"
  | "unknown"
  | (string & Record<never, never>); // allow extension strings

/** Lifecycle state of a channel (producer/consumer presence). */
export type ChannelLifecycle =
  | "active"
  | "orphan_publisher"
  | "orphan_subscriber"
  | "orphan";

/**
 * Wire shape for a single channel (topic/queue/sse/ws/graphql-sub/serverless).
 * Mirrors the JSON produced by GET /api/topology/:group (non-v2).
 * Critical: no `last_message_seen`, no `usage_history` — those are always null/[].
 */
export interface TopologyChannel {
  id: string;
  label: string;
  broker: string;
  broker_canonical: BrokerCanonical;
  framework?: string;
  owning_service: string;
  producers: string[];   // entity ids
  consumers: string[];
  // Resolved producer/consumer entity refs (name + source_file:line), so the
  // list rows and detail panel can show real NAMES instead of hashed ids (#1583).
  // The LIST endpoint now emits these alongside the raw id arrays.
  producer_refs?: TopologyEntityRef[];
  consumer_refs?: TopologyEntityRef[];
  scheduled?: boolean;
  schedule?: string;
  repo: string;
  channel_type?: "websocket" | "sse" | "redis_pubsub" | "graphql_subscription";
  // optional enrichment fields (present only after /generate-docs)
  docs_summary?: string;
  docgen_status?: "enriched" | "stale" | "pending";
  // cross-repo flag (derived client-side from broker_groups)
  cross_repo?: boolean;
}

/** Serverless function entry in the topology payload. */
export interface TopologyFunction {
  id: string;
  label: string;
  repo: string;
  provider?: string;
  invokers: string[];
  handlers: string[];
}

/** Transform edge (channel → channel). */
export interface TopologyTransform {
  from_id: string;
  to_id: string;
  repo: string;
}

/** Per-service aggregated stats inside a broker group. */
export interface BrokerServiceStat {
  name: string;
  topic_count: number;
}

/** Health breakdown per broker. */
export interface BrokerHealthSummary {
  active: number;
  orphan_publisher: number;
  orphan_subscriber: number;
  orphan: number;
}

/** One element of `broker_groups` in the topology payload. */
export interface TopologyBrokerGroup {
  broker: BrokerCanonical;
  count: number;
  services: BrokerServiceStat[];
  orphan_publishers: number;
  orphan_subscribers: number;
  cross_repo_topic_count: number;
  health_summary: BrokerHealthSummary;
  last_index_timestamp?: string; // ISO-8601
}

/**
 * Full wire response from GET /api/topology/:group.
 * All array fields are guaranteed non-null by the daemon.
 */
export interface TopologyResponse {
  topics: TopologyChannel[];
  queues: TopologyChannel[];
  channels: TopologyChannel[];
  nats_subjects: TopologyChannel[];
  graphql_subscriptions: TopologyChannel[];
  functions: TopologyFunction[];
  transforms: TopologyTransform[];
  broker_groups: TopologyBrokerGroup[];
}

/**
 * One producer/consumer entry as returned by the daemon's topic-detail endpoint.
 * The real daemon emits full entity objects (not raw entity-id strings).
 */
export interface TopologyEntityRef {
  entity_id: string;
  name: string;
  kind: string;
  source_file: string;
  start_line: number;
  repo: string;
  /** Prefixed Process entity IDs of flows that contain both this entity and
   *  the channel as steps — powers the ↗ flow action (#1943). */
  flow_process_ids?: string[];
}

/** Detailed channel view — GET /api/topology/:group/topic/:topicId.
 *
 *  NOTE: the detail endpoint returns `producers` and `consumers` as rich
 *  `TopologyEntityRef` objects, not plain entity-id strings.  The base
 *  TopologyChannel.producers / consumers type says string[] (list-endpoint
 *  shape).  The mismatch is intentional — callers MUST use TopologyEntityRef
 *  helpers when consuming detail-endpoint data.  See DetailPanel in
 *  topology.tsx and the TopologyEntityRef type for the correct shape.
 */
export interface TopologyChannelDetail extends TopologyChannel {
  source_file: string;
  start_line: number;
  protocol: string;
  message_schema?: string;
  tests: string[];
  related_topics: { id: string; label: string; broker_canonical: BrokerCanonical }[];
  flow_count: number;
  cross_repo: boolean;
  lifecycle_state: ChannelLifecycle;
  enrichment_health?: {
    has_summary: boolean;
    has_schema: boolean;
    has_volume_estimate: boolean;
    has_typical_payload_size: boolean;
    has_expected_consumers: boolean;
    has_gaps: boolean;
    filled_field_count: number;
    total_field_count: number;
  };
}

/** Orphan publisher entry — GET /api/topology/:group/orphan-publishers.
 *  The real daemon returns `broker` (raw name), not `broker_canonical`.
 *  Keep both so callers can use `broker_canonical ?? broker` for display. */
export interface OrphanPublisherEntry {
  id: string;
  label: string;
  /** Raw broker name as returned by the daemon (e.g. "kafka"). */
  broker: string;
  /** Canonical broker key used for icon/color mapping.
   *  Present only when the daemon pre-normalises the field; fall back to `broker`. */
  broker_canonical?: BrokerCanonical;
  repo: string;
  producers: string[];
  reason?: string;
}

/** Orphan subscriber entry — GET /api/topology/:group/orphan-subscribers.
 *  Same broker field convention as OrphanPublisherEntry above. */
export interface OrphanSubscriberEntry {
  id: string;
  label: string;
  /** Raw broker name as returned by the daemon. */
  broker: string;
  /** Canonical broker key; fall back to `broker` when absent. */
  broker_canonical?: BrokerCanonical;
  repo: string;
  consumers: string[];
  reason?: string;
}
// =============================================================
// Pending screen types — v2_pending.go wire shapes (#1442)
// =============================================================

export type EntityKind =
  | "function"
  | "component"
  | "hook"
  | "class"
  | "method"
  | "http_endpoint";

export interface EntityRef {
  name: string;
  type: EntityKind;
  repo: string;
  /** Includes `:line` suffix. */
  file: string;
}

export type RepairIssueType =
  | "missing_docstring"
  | "dead_code"
  | "mismatched_handler"
  | "untyped_params"
  | "broken_link"
  | "stale_cache";

export type EnrichmentType =
  | "summary"
  | "param_descriptions"
  | "relationship_tag"
  | "tags";

export type Severity = "critical" | "warning" | "info";

export interface RepairCandidate {
  id: string;
  /** Stable entity identifier (SubjectID). Use this — not id — for hint keying. */
  entityId: string;
  severity: Severity;
  issueType: RepairIssueType;
  entity: EntityRef;
  description: string;
  /** 0..1 */
  confidence: number;
  /** Unix ms. */
  detectedAt: number;
  /** Team-authored hint currently stored for this entity (may be absent). */
  hint?: string;
}

export interface EnrichmentCandidate {
  id: string;
  /** Stable entity identifier (SubjectID). Use this — not id — for hint keying. */
  entityId: string;
  enrichmentType: EnrichmentType;
  entity: EntityRef;
  description: string;
  confidence: number;
  detectedAt: number;
  /** Team-authored hint currently stored for this entity (may be absent). */
  hint?: string;
}

export type Candidate = RepairCandidate | EnrichmentCandidate;

/** Hints stored per-entity-id in local state and persisted via PUT hint (#1518). */
export type HintMap = Record<string, string>;

/** Wire shape returned by GET /api/v2/groups/:id/candidates */
export interface V2CandidatesResponse {
  repairs: RepairCandidate[];
  enrichments: EnrichmentCandidate[];
}

// ─── Flows (Process Flow Explorer) ────────────────────────────────────────────

export type EntryKind =
  | "http_handler"
  | "message_consumer"
  | "kafka_consumer"
  | "scheduled_task"
  | "component_render"
  | "test"
  | "cli_command"
  | "ws_handler"
  | "function";

export type StepKind =
  | "http_fetch"
  | "db_query"
  | "db_write"
  | "message_publish"
  | "message_consume"
  | "transform"
  | "validation"
  | "side_effect"
  | "external_lib"
  | "test_assert"
  | "component_render"
  | "render"
  | "function_call"
  | "unknown";

export type FlowRelationshipKind =
  | "CALLS"
  | "FETCHES"
  | "QUERIES"
  | "PUBLISHES_TO"
  | "SUBSCRIBES_TO"
  | "RENDERS"
  | "REFERENCES";

export interface FlowEnrichment {
  ai_summary?: string;
  preconditions?: string[];
  expected_outcome?: string;
  writes_db_table?: string[];
  publishes_to?: string[];
  external_calls?: string[];
  read_sources?: string[];
  write_sinks?: string[];
  linked_endpoint_id?: string;
  linked_topic_id?: string;
  gaps?: string[];
  rank?: number;
}

export interface ProcessStep {
  entity_id: string;
  /** Qualified name, e.g. "ReceivablesService.postSale". Older endpoints emit `label` only. */
  name?: string;
  label?: string;
  kind?: string;
  step_index: number;
  source_file: string;
  start_line?: number;
  repo: string;
  edge_kind: FlowRelationshipKind | null;
  step_kind?: StepKind;
  side_effects?: string[];
}

/**
 * One node in the branches_dag JSON tree (introduced in #2028 Phase 1).
 * Mirrors ChainStep in the Go daemon.
 *
 * - `step_index` aligns with the corresponding ProcessStep in the `steps` array.
 * - `branches` is non-empty (length > 1) at fan-out points.
 * - `reason === "fanout_cap"` marks a "+N more" overflow sentinel that should
 *   be rendered as a compacted indicator rather than a real step.
 */
export interface ChainStep {
  step_index: number;
  entity_id: string;
  label: string;
  /** Present on overflow sentinels: "fanout_cap" */
  reason?: string;
  /** Child branches — length > 1 means this is a fan-out point. */
  branches: ChainStep[];
}

export interface Process {
  process_id: string;
  label: string;
  repo: string;
  entry_id: string;
  entry_name: string;
  entry_kind: EntryKind;
  entry_module?: string;
  terminal_id: string;
  step_count: number;
  cross_stack: boolean;
  is_cross_repo?: boolean;
  crosses_external_lib?: boolean;
  terminal_is_phantom?: boolean;
  chain_labels: string[];
  source_file?: string;
  priority_hint?: "high" | "medium" | "low";
  dominant_step_kind?: FlowRelationshipKind;
  complexity_score?: number;
  steps?: ProcessStep[];
  flow_side_effects?: string[];
  enrichment?: FlowEnrichment;
  docgen_status?: "enriched" | "pending" | "stale";
  source_snippets?: Record<string, string>;
  /**
   * True when this process has at least one fan-out step (introduced in #2028).
   * When true, `branches_dag` carries the full branching DAG.
   */
  is_dag?: boolean;
  /**
   * JSON-serialised ChainStep tree emitted by #2028 Phase 1.
   * Only present when `is_dag === true`. Parse with JSON.parse → ChainStep.
   */
  branches_dag?: string;
}

export interface FlowDeadEnd {
  process_id: string;
  /** May be null/absent — dead-end items have a sparser shape than full flows. */
  process_name?: string | null;
  repo: string;
  reason: "no_useful_sink" | "single_step" | "unresolved_callee" | "phantom_terminal" | "dead_end";
  step_count: number;
  dead_end_step_id?: string;
  dead_end_step_name?: string;
  cross_stack?: boolean;
}

export interface EntryKindGroup {
  kind: EntryKind;
  count: number;
}

export interface FlowsListResponse {
  processes: Process[];
  count: number;
  entry_kind_groups: EntryKindGroup[];
}

export interface FlowDetailResponse {
  process: Process;
  chain_entities: ProcessStep[];
  source_snippets: Record<string, string>;
}

export interface FlowDeadEndsResponse {
  dead_ends: FlowDeadEnd[];
  count: number;
}

// ----------------------------------------------------------------
// Event Flows (#1944 Phase 1) — multi-hop pub/sub chains seeded by
// channels (SCOPE.MessageTopic / SCOPE.EventBusEvent). Same Property
// contract as ProcessFlows so the Flows DAG renderer drives both.
// ----------------------------------------------------------------

export interface EventFlowListItem {
  event_flow_id: string;
  repo: string;
  label: string;
  seed_id: string;
  seed_name: string;
  terminal_id: string;
  step_count: number;
  channel_count: number;
  chain_labels: string[];
  source_file?: string;
  entry_kind: "channel";
}

export interface EventFlowsListResponse {
  event_flows: EventFlowListItem[];
  count: number;
}

export interface EventFlowStep {
  entity_id: string;
  label: string;
  repo: string;
  step_index: number;
  entity_kind: string;
  source_file?: string;
  start_line?: number;
  is_channel: boolean;
}

export interface EventFlowDetailResponse {
  event_flow_id: string;
  repo: string;
  label: string;
  seed_id: string;
  seed_name: string;
  terminal_id: string;
  step_count: number;
  channel_count: number;
  chain_labels: string[];
  /** JSON-serialised DAG (mirrors ProcessFlow.branches_dag).  */
  branches_dag: string;
  steps: EventFlowStep[];
  entry_kind: "channel";
}

// ----------------------------------------------------------------
// Paths screen types (mirrors v2_paths.go wire shapes)
// ----------------------------------------------------------------

export type HttpVerb = "GET" | "POST" | "PUT" | "PATCH" | "DELETE" | "GRPC" | "HEAD" | "OPTIONS" | "ANY";
export type OrphanReason = "no_handler_found" | "dynamic_baseurl" | "template_literal";

/** One path row in the grouped list (left rail). */
export interface PathRoute {
  path_hash: string;
  path: string;
  verbs: HttpVerb[];
  handlers_count: number;
  multiplicity: number;
  frameworks: string[];
  is_webhook: boolean;
  webhook_provider?: string;
  auth: boolean;
  repos: string[];
  controller: string;
}

/** One controller group inside a backend. */
export interface ControllerGroupShape {
  id: string;
  label: string;
  file: string;
  is_webhook?: boolean;
  routes: PathRoute[];
}

/** One backend service grouping in the left rail. */
export interface PathBackend {
  id: string;
  label: string;
  service_type: "REST" | "gRPC" | "GraphQL";
  framework: string;
  language: string;
  cross_backend_refs: boolean;
  any_rate: number;
  groups: ControllerGroupShape[];
}

/** Aggregate counts shown in the sub-stats bar. */
export interface PathTotals {
  routes: number;
  endpoints: number;
  controllers: number;
  backends: number;
  /** Orphan-caller count, surfaced for the tab badge (#1551). Optional for back-compat. */
  orphans?: number;
}

/** Response from GET /api/v2/groups/:id/paths. */
export interface PathsListResponse {
  backends: PathBackend[];
  totals: PathTotals;
}

/** One parameter on a path handler. */
export interface PathParameter {
  name: string;
  /** Source of the parameter. "cookie" and "form" are emitted by the Java extractor (#2113). */
  in: "path" | "query" | "body" | "header" | "cookie" | "form";
  type: string;
  required: boolean;
  desc: string;
  /** Verbs this param applies to (for verb filter). */
  verbs?: HttpVerb[];
  /**
   * Refs #1935 Phase 1 — when the parameter type resolves to a known
   * class entity in the group, type_entity_id is the prefixed entity
   * id and has_children indicates the ShapeTree expand glyph should
   * render. Both undefined for primitive / unresolved parameter types.
   */
  type_entity_id?: string;
  has_children?: boolean;
}

/**
 * #1938 Phase 1 — per-status-code response entry extracted from
 * @APIResponse / @ApiResponse annotations (Java MicroProfile / JAX-RS).
 * Populated only when the Java annotation extractor found explicit status
 * codes on the handler. The frontend renders a tab strip above the ShapeTree
 * when this array is non-empty. Default-selected tab = lowest 2xx code.
 */
export interface PerStatusResponse {
  status_code: number;
  type_name?: string;
  type_entity_id?: string;
  has_children?: boolean;
}

/** Response shape per verb in the detail pane. */
export interface ResponseShape {
  verb: HttpVerb;
  status_codes: number[];
  keys: string[];
  dynamic?: boolean;
  /**
   * Refs #1935 Phase 1 — when the handler's return type resolves to a
   * user-defined DTO, type_name is the simple-name token (rendered as
   * the shape header) and type_entity_id + has_children drive the
   * ShapeTree expansion exactly like PathParameter.
   */
  type_name?: string;
  type_entity_id?: string;
  has_children?: boolean;
}

/**
 * Refs #1935 Phase 1 — one row in a ShapeTree subtree, returned by
 * GET /api/v2/groups/:id/shape?type_entity_id=… . Each row corresponds
 * to a field of the requested class entity.
 */
export interface ShapeRow {
  name: string;
  type: string;
  annotations?: string[];
  nullable?: boolean;
  type_entity_id?: string;
  has_children: boolean;
}

/** Response from GET /api/v2/groups/:id/shape. */
export interface ShapeResponse {
  type_entity_id: string;
  type_name: string;
  subtype?: string;
  rows: ShapeRow[];
}

/** One handler implementation in the detail. */
export interface HandlerDetail {
  verb: HttpVerb;
  qualified_name: string;
  framework: string;
  repo: string;
  source_file: string;
  start_line: number;
  language: string;
  has_docs: boolean;
  docs_summary?: string;
  docs_path?: string;
  auth?: string;
}

/** An entity referenced in the detail (callers, downstream, tests). */
export interface PathEntity {
  label: string;
  qualified_name: string;
  kind: string;
  repo: string;
  source_file: string;
  start_line: number;
  edge?: string;
  protocol?: string;
}

/**
 * One evidence signal in the resolved auth_policy source chain (#1942 Phase 1).
 * Mirrors v2AuthSignal on the backend.
 */
export interface AuthSignal {
  kind: string;
  entity_id?: string;
  text: string;
  file: string;
  line: number;
}

/**
 * Structured auth posture resolved by the indexer (#1942 Phase 1).
 * Mirrors v2AuthPolicy on the backend wire shape.
 * Present only on Java endpoints (and future language phases).
 */
export interface AuthPolicy {
  required: boolean;
  method: string;
  roles?: string[];
  scopes?: string[];
  confidence: string;
  source_chain?: AuthSignal[];
}

/** Detail pane data — returned by GET /api/v2/groups/:id/paths/:hash. */
export interface PathDetail {
  path_hash: string;
  path: string;
  verbs: HttpVerb[];
  repos: string[];
  is_webhook: boolean;
  webhook_provider?: string;
  auth: boolean;
  auth_scheme?: string;
  /**
   * Structured auth posture resolved by the indexer (#1942 Phase 1).
   * When present, the UI renders an AuthSection between Description and Parameters.
   */
  auth_policy?: AuthPolicy;
  /** Pre-resolved chip label from the backend (e.g. "[Roles: ADMIN]"). */
  auth_chip?: string;
  /** Chip tone: "accent" | "warning" | "muted". */
  auth_chip_tone?: string;
  description: {
    has_docs: boolean;
    summary: string;
    docs_path?: string;
    ai_generated?: boolean;
  };
  parameters: PathParameter[];
  response_shapes: ResponseShape[];
  /** #1938 Phase 1 — per-status response tabs (Java @APIResponse only). */
  per_status_responses?: PerStatusResponse[];
  handlers: HandlerDetail[];
  inbound_fetches: PathEntity[];
  outbound: {
    db: PathEntity[];
    event: PathEntity[];
    queue: PathEntity[];
    external: PathEntity[];
    grpc: PathEntity[];
  };
  side_effects: PathEntity[];
  tests: PathEntity[];
}

/** One orphan caller row. */
export interface OrphanCaller {
  id: string;
  method: HttpVerb;
  url_pattern: string;
  caller_file: string;
  caller_line: number;
  caller_label: string;
  repo: string;
  reason: OrphanReason;
  repair_hint?: string;
}

/** Response from GET /api/v2/groups/:id/paths/orphans. */
export interface OrphansResponse {
  orphans: OrphanCaller[];
  totals: {
    no_handler_found: number;
    dynamic_baseurl: number;
    template_literal: number;
  };
}

// ----------------------------------------------------------------
// Operations screen types (mirrors handlers_system/patterns/quality/updates)
// ----------------------------------------------------------------

/** Wire shape for GET /api/system */
export interface SystemStatus {
  status: "running" | "stopped" | "unhealthy";
  uptime_seconds?: number;
  uptime_human?: string;
  pid: number;
  rss_mb: number;
  rss_budget_mb?: number;
  socket_path?: string;
  dashboard_url?: string;
  version: string;
  commit_sha: string;
  built_at: string;
  days_since_build?: number;
  stale_build: boolean;
}

/** Wire shape for POST /api/system/restart and /api/system/stop */
export interface SystemActionReply {
  ok: boolean;
  message: string;
}

/** One log line from GET /api/system/logs */
export interface LogLine {
  raw: string;
  severity: "error" | "warn" | "info" | "debug";
}

/** Wire shape for GET /api/system/logs */
export interface SystemLogsReply {
  lines: LogLine[];
  total: number;
  path: string;
}

/** Wire shape for GET /api/updates/check */
export interface UpdateCheckReply {
  current_version: string;
  current_commit: string;
  current_built_at: string;
  latest_version: string;
  latest_tag: string;
  latest_body: string;
  latest_html_url: string;
  published_at?: string;
  update_available: boolean;
  fetch_error?: string;
  checked_at: string;
}

/** One pattern row from GET /api/patterns/{group} */
export interface PatternRow {
  id: string;
  kind: string;
  category: string;
  trigger: string;
  confidence: number;
  observations: number;
  last_seen: string;
  status: "active" | "candidate" | "rejected";
  is_candidate: boolean;
  needs_attention: boolean;
  stale: boolean;
  reject_reason: string;
  approval_note: string;
  steps: string[];
  anti_patterns: unknown[];
  exemplars: unknown[];
  touches: number;
  scope: string;
  convergence_count: number;
}

/** Stats header for patterns */
export interface PatternStats {
  total: number;
  pending_review: number;
  rejected: number;
  stale: number;
  needs_attention: number;
}

/** Wire shape for GET /api/patterns/{group} */
export interface PatternsListReply {
  patterns: PatternRow[];
  count: number;
  stats: PatternStats;
}

/** Wire shape for DELETE /api/patterns/{group}/{id} */
export interface PatternDeleteReply {
  deleted: string;
}

/** Wire shape for POST /api/patterns/{group}/gc */
export interface PatternGCReply {
  dry_run: boolean;
  pruned_count: number;
  pruned: PatternRow[];
  remaining_count: number;
  candidate_decay_days: number;
}

/* ------------------------------------------------------------------ *
 * Async action jobs (#1512) — rebuild / reset return a JobAck (202) and
 * the frontend polls ActionJob via GET /api/v2/jobs/:id. See API_V2.md §6c.
 * ------------------------------------------------------------------ */

/** 202 ack returned by an async action endpoint. */
export interface JobAck {
  job_id: string;
  op: "rebuild" | "reset";
  group: string;
  repo?: string;
  status: string;
  progress_token: string;
  status_url: string;
  stream_url: string;
}

/** Live status of an async action job. */
export interface ActionJob {
  id: string;
  op: "rebuild" | "reset";
  group: string;
  repo?: string;
  status: "queued" | "running" | "done" | "failed";
  progress: number;
  message?: string;
  error?: string;
  progress_token: string;
  queued_at: number;
  started_at?: number;
  finished_at?: number;
}

/* ------------------------------------------------------------------ *
 * Create-group / add-repo scan wizard (#1517). The wizard sends a
 * server-side PATH string (the daemon resolves + indexes it); see
 * v2_wizard.go for the browser-File-handle limitation note.
 * ------------------------------------------------------------------ */

/** POST /api/v2/scan/inspect — stack + monorepo detection preview (no writes). */
export interface ScanInspectReply {
  valid: boolean;
  absPath: string;
  suggestedGroup: string;
  suggestedSlug: string;
  stack: string;
  /** "pnpm" | "npm" | "turbo" | "nx" | "lerna" | "multi" | "" */
  monorepo: string;
  packages: string[];
  /**
   * Names of immediate child directories that contain a .git entry — the
   * multi-repo-parent pattern (#1531 follow-up). Non-empty only when the
   * parent dir is NOT a git repo itself but wraps N child git repos. Takes
   * precedence over packages when both would be present.
   */
  childGitRepos: string[];
  /**
   * "git-repos" when childGitRepos is non-empty, "packages" when packages is
   * non-empty, "" otherwise. The UI uses this to label the checkbox list.
   */
  childrenKind: "git-repos" | "packages" | "";
  hasAgentsMd: boolean;
  alreadyRegistered?: string;
  error?: string;
}

/* ------------------------------------------------------------------ *
 * Server-side folder browser (#1529). The browser File System Access API
 * can't hand the daemon a real on-disk path, so the wizard navigates the
 * daemon's OWN filesystem via GET /api/v2/fs/list. Picking a folder yields
 * its absolute path — no manual paste required.
 * ------------------------------------------------------------------ */

/** One subdirectory entry returned by GET /api/v2/fs/list. */
export interface FsEntry {
  name: string;
  /** Absolute on-disk path the daemon will index. */
  path: string;
  isDir: boolean;
  hidden: boolean;
}

/** A quick-jump shortcut (home / Documents / Projects), only on the home view. */
export interface FsShortcut {
  label: string;
  path: string;
}

/** GET /api/v2/fs/list — subdirectories of an absolute path. */
export interface FsListReply {
  /** The resolved absolute path that was listed. */
  path: string;
  /** Absolute parent path, or "" at the filesystem root. */
  parent: string;
  entries: FsEntry[];
  shortcuts?: FsShortcut[];
  /** Human-readable reason when the path couldn't be listed. */
  error?: string;
}

/** One repo the wizard wants registered + indexed. */
export interface WizardRepo {
  path: string;
  slug?: string;
  modules?: string[];
}

/* ------------------------------------------------------------------ *
 * Real-time per-repo / per-MODULE indexing progress (#1527). Streamed as
 * SSE `progress` events from GET /api/index-progress/:group. Mirrors the Go
 * progress.Event shape (internal/progress/event.go). For a monorepo each
 * event carries a `module` (package-root) label so the UI renders one row
 * per module instead of one aggregate row for the whole repo.
 * ------------------------------------------------------------------ */

export type ProgressPhase =
  | "scanning"
  | "extracting_ast"
  | "resolving_refs"
  | "running_algorithms"
  | "materializing"
  | "done"
  | "error";

/** One SSE progress event off /api/index-progress/:group. */
export interface ProgressEvent {
  group_slug: string;
  repo_slug: string;
  phase: ProgressPhase;
  files_done: number;
  files_total: number;
  entities_so_far: number;
  eta_ms?: number;
  error?: string;
  ts: number;
  bytes_seen?: number;
  current_file?: string;
  phase_started_at_ms?: number;
  algorithm_name?: string;
  /** Package-root label; present only when indexing a monorepo. */
  module?: string;
}

/**
 * One UI row in the per-repo / per-module progress feed. Keyed by
 * `${repo_slug}` for single repos or `${repo_slug}/${module}` for monorepo
 * packages; the latest event per key collapses into one row.
 */
export interface ProgressRow {
  key: string;
  repoSlug: string;
  /** Package-root for monorepo modules; undefined for whole-repo rows. */
  module?: string;
  phase: ProgressPhase;
  filesDone: number;
  filesTotal: number;
  entitiesSoFar: number;
  currentFile?: string;
  etaMs?: number;
  error?: string;
  /** Wall-clock ms of the most recent event for this row. */
  ts: number;
}

/** POST /api/v2/maintenance/cleanup result. */
export interface CleanupReply {
  dry_run: boolean;
  orphaned: { name: string; config_path: string }[];
  removed: number;
  message: string;
}

/** POST /api/v2/update/apply result. */
export interface UpdateApplyReply {
  exit_code: number;
  output: string[];
  applied: boolean;
}

/** Orphan audit totals */
export interface OrphanAuditTotals {
  entities: number;
  orphans: number;
  orphan_rate: number;
}

/** Per-repo orphan stats */
export interface RepoOrphanStats {
  slug: string;
  path: string;
  entities: number;
  orphans: number;
  orphan_rate: number;
  risk_score: number;
}

/** Per-kind orphan stats */
export interface KindStat {
  kind: string;
  /** Total number of entities of this kind. */
  entities: number;
  /** Orphaned subset of this kind. */
  orphans: number;
  /** @deprecated mirrors `entities` for back-compat; read `entities`/`orphans`. */
  count: number;
  orphan_rate: number;
}

/** One plain-language bucket of unresolved references. */
export interface UnresolvedReason {
  /** Machine key: external_library | unresolved_import | extraction_gap. */
  reason: string;
  /** Human-readable name shown in the UI. */
  label: string;
  /** Plain-language explanation of what this bucket means. */
  description: string;
  /** Number of unresolved edges in this bucket. */
  count: number;
  /** Share of the TOTAL edges (0–1). */
  pct: number;
}

/**
 * Unresolved-references breakdown — the real driver of Fidelity. Of every
 * import/reference edge archigraph extracted, how many it linked to a real
 * target and, for the rest, the reason it could not.
 */
export interface UnresolvedReferences {
  /** Every import/reference edge considered. */
  total: number;
  /** Linked to a real target. */
  resolved: number;
  /** total − resolved. */
  unresolved: number;
  /** resolved/total (0–1); equals Fidelity as a ratio. */
  resolved_rate: number;
  /** The unresolved edges, bucketed by reason. */
  reasons: UnresolvedReason[];
}

/** Recommendation item from orphan audit */
export interface RecommendationItem {
  priority: number;
  issue: string;
  affected_repos: number;
  recoverable_entities_estimate: number;
}

/** Wire shape for GET/POST /api/quality/orphans/{group} */
export interface OrphanAuditReply {
  group: string;
  /** RFC3339 timestamp; empty string when never run. */
  audited_at: string;
  /** True only when a real audit has been run + persisted for this group. */
  has_run: boolean;
  total: OrphanAuditTotals;
  per_repo: RepoOrphanStats[];
  per_kind: KindStat[];
  /** Composite graph-health score (0–100): orphans + unresolved refs + recall. */
  health_score: number;
  /** Fidelity = resolved-reference share, as a 0–1 ratio (null when unknown). */
  fidelity: number | null;
  /** @deprecated internal field; the unresolved-reference rate (0–100). */
  bug_rate_pct: number;
  /** Unresolved-references breakdown — the real Fidelity story. */
  references: UnresolvedReferences;
  recommendations: RecommendationItem[];
}

/** Golden fixture entry */
export interface FixturesReply {
  fixtures: string[];
}

/** Wire shape for POST /api/quality/recall */
export interface RecallReply {
  fixture: string;
  entity_recall: number;
  relationship_recall: number;
  entity_expected: number;
  entity_found: number;
  relationship_expected: number;
  relationship_found: number;
  missing_relationships: { source_id: string; target_id: string; kind: string }[];
  errors: string[];
  elapsed_ms: number;
}

/* ============================================================
   Module overview (#1386 / #1380 / #1384)

   Wire shape for GET /api/v2/groups/:group/modules/analysis — module-
   level GDS (SCC + PageRank + betweenness on the aggregated module
   graph). One repoOut per repo in the group. `modules` is the FULL
   centrality list (one entry per module) and `edges` is the FULL set
   of directed module→module aggregated edges, both used by the webui
   to render the collapsed module-overview canvas.
   ============================================================ */
export interface ModuleCentralityWire {
  module_id: string;
  module_name: string;
  pagerank: number;
  betweenness: number;
  in_degree: number;
  out_degree: number;
  in_cycle: boolean;
}

export interface ModuleEdgeWire {
  from_module: string;
  to_module: string;
  weight: number;
  scc_internal: boolean;
  scc_id: number;
}

export interface ModuleSCCWire {
  id: number;
  size: number;
  members: string[];
  member_names: string[];
  edges: { from_module: string; to_module: string; weight: number }[];
}

export interface ModuleAnalysisRepoWire {
  repo: string;
  num_modules: number;
  num_module_edges: number;
  num_sccs: number;
  largest_scc_size: number;
  modules_in_cycle: number;
  top_pagerank: ModuleCentralityWire[];
  top_betweenness: ModuleCentralityWire[];
  sccs: ModuleSCCWire[];
  modules: ModuleCentralityWire[];
  edges: ModuleEdgeWire[];
}

export interface ModuleAnalysisResponse {
  repos: ModuleAnalysisRepoWire[];
  count: number;
}

// ----------------------------------------------------------------
// PH4 — ref selector, worktree subtree, URL persistence (#2092)
// ----------------------------------------------------------------

/**
 * Tier indicates how quickly the daemon can serve a ref's data.
 * HOT = in-memory / recently warmed; WARM = on-disk cache;
 * COLD = archived (first query triggers a warm-load ~50ms);
 * EXPIRED = data older than retention window; UNKNOWN = not yet determined.
 */
export type RefTier = "HOT" | "WARM" | "COLD" | "EXPIRED" | "UNKNOWN";

/** Source of a ref: a regular branch or an ephemeral worktree. */
export type RefSource = "branch" | "worktree";

/**
 * One ref entry returned by GET /api/v2/groups/:g/repos/:r/refs
 * (or the group-level /api/v2/groups/:g/refs aggregated endpoint).
 */
export interface RefEntry {
  /** Short or full ref name (e.g. "main", "feat/foo"). */
  name: string;
  /** Full 40-char commit SHA. */
  sha: string;
  /** 7-char short SHA prefix for display. */
  shortSha: string;
  /** Cache tier. */
  tier: RefTier;
  /** ms epoch when this ref was last indexed; null if never. */
  indexedAt: number | null;
  /** archigraph indexer version that produced the last index. */
  indexerVersion: string | null;
  /** Whether this ref originates from a branch checkout or a worktree. */
  source: RefSource;
  /** Parent repo slug (present in aggregated endpoint responses). */
  repoSlug?: string;
}

/** Response shape for GET /api/v2/groups/:g/refs */
export interface GroupRefsResponse {
  /** Key = repo slug, value = refs for that repo. */
  refs: Record<string, RefEntry[]>;
}

// ---------------------------------------------------------------------------
// PH5 (#2093) — Graph diff types
// ---------------------------------------------------------------------------

/** One entity in the diff output. */
export interface DiffEntityEntry {
  id: string;
  kind: string;
  name: string;
  source_file: string;
  /** Fields that changed (only present on modified entities). */
  modified_fields?: string[];
}

/** One relationship in the diff output. */
export interface DiffRelEntry {
  from_id: string;
  to_id: string;
  kind: string;
}

/** Aggregated change counts. */
export interface DiffSummary {
  entities_added: number;
  entities_removed: number;
  entities_modified: number;
  relationships_added: number;
  relationships_removed: number;
  files_changed: number;
}

/**
 * Full diff result returned by GET /api/v2/groups/:g/repos/:r/diff.
 * Matches internal/graph.DiffResult wire shape.
 */
export interface DiffResult {
  group: string;
  repo: string;
  ref_a: string;
  ref_b: string;
  summary: DiffSummary;
  entities: {
    added: DiffEntityEntry[];
    removed: DiffEntityEntry[];
    modified: DiffEntityEntry[];
  };
  relationships: {
    added: DiffRelEntry[];
    removed: DiffRelEntry[];
  };
}

// ---------------------------------------------------------------------------
// Daemon mode (S7a #2169)
// ---------------------------------------------------------------------------

/** One mode option as returned in AllModes by GET /api/v2/daemon/mode. */
export interface DaemonModeInfo {
  name: string;
  description: string;
  env_defaults: Record<string, string>;
}

/** Wire shape for GET /api/v2/daemon/mode. */
export interface DaemonModeReply {
  /** Mode from daemon.config.json. Empty string when no config exists. */
  mode: string;
  /** Resolved mode (defaults to "background" when mode is empty). */
  effective_mode: "background" | "workstation" | "readonly";
  /** One-line description of the effective mode. */
  description: string;
  /** Env-var defaults the effective mode applies on daemon boot. */
  env_defaults: Record<string, string>;
  /** Full catalogue of all three modes for rendering the selection UI. */
  all_modes: DaemonModeInfo[];
}

/** Wire shape for POST /api/v2/daemon/mode response. */
export interface SetDaemonModeReply {
  mode: string;
  config_path: string;
  restart_initiated: boolean;
}

// ---------------------------------------------------------------------------
// Security / Auth-Coverage screen (#4250, epic #4249)
//
// Wire shapes for the three v1 security routes served by
// internal/dashboard/handlers_security.go:
//   GET /api/security/auth-coverage/{group}
//   GET /api/security/secrets/{group}
//   GET /api/security/cycles/{group}
// All three responses are RAW JSON (not the v2 { ok, data } envelope).
// ---------------------------------------------------------------------------

/** Finding severity shared across all three security reports. */
export type SecuritySeverity = "error" | "warn" | "info";

/** One HTTP endpoint auth-coverage finding. */
export interface AuthEndpointFinding {
  entity_id: string;
  name: string;
  repo: string;
  source_file?: string;
  start_line?: number;
  method?: string;
  path?: string;
  has_auth: boolean;
  auth_evidence?: string;
  severity: SecuritySeverity;
  sensitive_op?: boolean;
  idor_risk?: boolean;
}

/** GET /api/security/auth-coverage/{group}. */
export interface GroupAuthCoverageReport {
  group: string;
  total_endpoints: number;
  covered_count: number;
  uncovered_count: number;
  coverage_pct: number;
  error_count: number;
  warn_count: number;
  info_count: number;
  findings: AuthEndpointFinding[];
}

/** One secret-related finding. */
export interface SecuritySecretFinding {
  entity_id: string;
  name: string;
  repo: string;
  source_file?: string;
  start_line?: number;
  language?: string;
  /** "hardcoded_credential" | "secrets_management". */
  category: string;
  /** Set for secrets_management findings (e.g. "vault", "aws_secrets_manager"). */
  provider?: string;
  severity: SecuritySeverity;
  remediation?: string;
}

/** GET /api/security/secrets/{group}. */
export interface GroupSecretsReport {
  group: string;
  total_findings: number;
  error_count: number;
  warn_count: number;
  info_count: number;
  by_category: Record<string, number>;
  findings: SecuritySecretFinding[];
}

/** A directed edge within an import cycle. */
export interface CycleFindingEdge {
  from_id: string;
  to_id: string;
}

/** One import-cycle finding. */
export interface CycleFinding {
  repo: string;
  members: string[];
  edges: CycleFindingEdge[];
  weakest_link_from_id?: string;
  weakest_link_to_id?: string;
  suggested_extraction_id?: string;
  size: number;
  severity: SecuritySeverity;
}

/** GET /api/security/cycles/{group}. */
export interface GroupCyclesReport {
  group: string;
  total_cycles: number;
  error_count: number;
  warn_count: number;
  info_count: number;
  findings: CycleFinding[];
}
