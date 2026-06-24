/* ============================================================
   data/types.ts — the typed domain model.

   These shapes mirror the grafel daemon's responses and the
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
  | "IMPORTS"
  // Semantic edge kinds (#4252). These are emitted by the daemon and reach the
  // /api/v2/graph payload unfiltered; they default OFF in the graph filters
  // because they can be high-volume / noisy, but are toggleable.
  | "INJECTED_INTO"
  | "THROWS"
  | "CATCHES"
  | "JOINS_COLLECTION"
  | "HTTP_ENDPOINT_CALL";

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

// ── AI-tool selection settings (#5257, epic #5252) ───────────────────────────

/** One AI coding tool's install status for a group. */
export interface ToolStatus {
  id: string;
  displayName: string;
  /** Whether grafel's artifacts are written for this tool in the group. */
  enabled: boolean;
  /** Best-effort detection that the tool is present on this machine. */
  detected: boolean;
}

/** GET /api/v2/groups/:id/tools response. */
export interface ToolsStatus {
  tools: ToolStatus[];
  /** True when the group has an explicit selection (vs the all-tools default). */
  explicit: boolean;
}

/** Per-tool outcome of a PUT /tools apply. */
export interface ToolApplyResult {
  id: string;
  action: "written" | "removed" | "unchanged" | "error";
  detail?: string;
}

/** PUT /api/v2/groups/:id/tools response. */
export interface ToolsApplyReply {
  tools: string[];
  summary: ToolApplyResult[];
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
  | "inngest"
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
  /** Messaging framework of the entity (e.g. "inngest") — lets the detail panel
   *  badge an Inngest function/event distinctly (#5485). */
  framework?: string;
  /** Durable step structure of an Inngest function (#5484 inngest_step children).
   *  Present only for Inngest functions; powers the step list in the topology
   *  detail panel (#5485). */
  inngest_steps?: InngestStep[];
}

/** One durable step of an Inngest function (#5484 inngest_step Operation). */
export interface InngestStep {
  step_id: string;
  /** run | sleep | sleepUntil | waitForEvent | invoke */
  step_kind: string;
  source_file?: string;
  start_line?: number;
  /** Awaited event name for a waitForEvent step. */
  wait_event?: string;
  /** Invoked-function reference for an invoke step. */
  invoke_target?: string;
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
   *
   * @deprecated Superseded by the server-built `flow_dag` payload (#4363). The
   * detail endpoint now emits the rendered DAG directly; this raw tree remains
   * for backward-compat / the "View DAG JSON" affordance only.
   */
  branches_dag?: string;
  /**
   * Server-built FlowDag payload (#4363) — the same DownstreamDAGResponse shape
   * the v2 paths /downstream-dag endpoint produces, rooted at this flow's entry
   * with "flow-step-<index>" node ids. Emitted by the flows DETAIL endpoint so
   * the Flows view renders it directly (no client-side reshape). Null when the
   * flow carries no steps.
   */
  flow_dag?: DownstreamDAGResponse | null;
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
  /**
   * Repo-relative path of THIS route's own defining file (#4608). The module
   * left-tree groups each route by the `src/modules/<MODULE>/…` segment of this
   * field, so a route is never bucketed under a sibling's module just because
   * they share a controller group.
   */
  source_file?: string;
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
  /**
   * Per-field validation constraint chips (#4858), parsed from the field's
   * validation decorators — e.g. for a NestJS DTO field
   * `@IsString() @MaxLength(120) @IsOptional() name?: string` this is
   * ["IsString","MaxLength:120","IsOptional"]. Absent for fields with none.
   */
  validations?: string[];
  nullable?: boolean;
  type_entity_id?: string;
  /**
   * Definition location of the field's TYPE (#4869) — lets the UI open the
   * type's source in the peek modal when the user clicks the type-name link,
   * independent of the row-body expand/collapse click. Present only when the
   * type resolves to an in-group entity.
   */
  type_source_file?: string;
  type_source_line?: number;
  type_repo?: string;
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
  /**
   * #4489 — the endpoint's EFFECTIVE side-effect kinds, aggregated by the
   * backend (#4524) over the handler's downstream CALLS (transitively, capped).
   * A thin controller that delegates the DB write to a downstream service has
   * no DIRECT side-effect edge — its `side_effects` (the entity list above) is
   * empty — but `effective_effects` still surfaces `{kind:"db_write",
   * source:"downstream"}` so the panel shows "DB write (via downstream)" rather
   * than "(0)". `source` distinguishes a sink on the handler itself ("direct")
   * from one reached only via a delegated callee ("downstream"). Omitted /
   * empty when the endpoint genuinely has no effects.
   */
  effective_effects?: { kind: string; source: "direct" | "downstream" }[];
  tests: PathEntity[];
}

/* ------------------------------------------------------------------
   Endpoint posture + effective contract (#4254, epic #4249)

   Returned by GET /api/v2/groups/:id/paths/:hash/posture — the lazy
   sibling route the detail pane fetches when a path is opened. Mirrors
   the Go internal/mcp.PosturePayload / EffectiveContractResult shapes
   the grafel_endpoint_posture / grafel_effective_contract tools
   emit (same code path, no drift). All facets are honest-empty: a row
   may carry no posture at all (has_posture=false), and `contract` is
   null for non-ViewSet endpoints.
   ------------------------------------------------------------------ */

/** THROWS / CATCHES exception-flow facet of an endpoint's posture. */
export interface PostureErrorFlow {
  throws?: string[];
  catches?: string[];
}

/** Per-endpoint posture row (one per matched endpoint entity). */
export interface PosturePayload {
  entity_id: string;
  name?: string;
  kind?: string;
  repo: string;
  source_file?: string;
  start_line?: number;
  method?: string;
  path?: string;
  error_flow?: PostureErrorFlow;
  /** rate_limited / rate_limit / rate_limit_scope / rate_limit_source props. */
  rate_limit?: Record<string, string>;
  /** deprecated / deprecated_since / api_version / ... props. */
  deprecation?: Record<string, string>;
  /** Feature-flag keys this endpoint is gated by (GATED_BY edges). */
  feature_gates?: string[];
  /** Resolved auth/interceptor props (HTTP guard + gRPC/tRPC). */
  auth?: Record<string, string>;
  /** True when at least one facet above is non-empty. */
  has_posture: boolean;
}

/** One verb's effective contract within a ViewSet group. */
export interface EffectiveContract {
  verb: string;
  path?: string;
  handler?: string;
  /** "explicit" | "inherited" | "action" — "inherited" marks an MRO handler. */
  kind?: string;
  source_class?: string;
  default_status?: number;
  error_statuses?: number[];
  serializer?: string;
  pagination?: boolean;
  permissions?: string[];
  behaviour?: string;
  auth_required?: boolean;
}

/** One owning ViewSet's grouped per-verb contracts. */
export interface EffectiveContractGroup {
  class: string;
  framework?: string;
  repo?: string;
  handlers: EffectiveContract[];
}

/** Grouped effective-contract result; note explains an empty groups list. */
export interface EffectiveContractResult {
  target: string;
  groups: EffectiveContractGroup[] | null;
  note?: string;
}

/** Payload for GET /api/v2/groups/:id/paths/:hash/posture. */
export interface PathPostureResponse {
  path_hash: string;
  path: string;
  endpoints: PosturePayload[];
  /** null when no DRF/pack-known ViewSet backs this path. */
  contract: EffectiveContractResult | null;
  /**
   * #4486 — whether the (DRF/Django-only) effective-contract feature is
   * meaningful for this path. False for NestJS / Express / Go / GraphQL
   * endpoints, where the UI hides the section entirely rather than rendering
   * DRF-specific empty-state wording. Optional for backward-compat with older
   * payloads (treated as "unknown" → fall back to framework/contract shape).
   */
  contract_applicable?: boolean;
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
  /**
   * Absolute paths of the OTHER git repos alongside absPath in its parent —
   * populated only when absPath is itself a git repo. Used by the action-first
   * "group" flow to offer "this repo + its siblings" (#5336).
   */
  siblingGitRepos: string[];
  /** Whether absPath itself is a git repo. */
  isGitRepo: boolean;
  /**
   * Recommended wizard action derived from the shared detect.ClassifyPath
   * classifier ("single" | "group" | "monorepo" | ""), so the dashboard's
   * action-first step matches the CLI wizard (#5336).
   */
  suggestedAction: "single" | "group" | "monorepo" | "";
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

/**
 * One detected MCP-capable AI tool offered in the wizard's "Configure MCP for
 * which tools?" step (#5344). Mirrors v2MCPToolStatus in internal/dashboard.
 */
export interface MCPToolStatus {
  id: string;
  displayName: string;
  /** The tool's config already contains a grafel entry (shown as "configured"). */
  hasGrafel: boolean;
  /** The smart (recently-used / previously-configured / remembered) default. */
  defaultSelected: boolean;
}

/** Reply from GET /api/v2/mcp-tools/detect (#5344). */
export interface MCPToolsDetectReply {
  tools: MCPToolStatus[];
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
  // #5334 — granular graph-assembly phases (split from the coarse
  // running_algorithms / materializing). The coarse two are retained above as
  // fallbacks for older events.
  | "building_communities"
  | "computing_centrality"
  | "detecting_links"
  | "computing_flows"
  | "writing_graph"
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
 * import/reference edge grafel extracted, how many it linked to a real
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
  /** grafel indexer version that produced the last index. */
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

// ---------------------------------------------------------------------------
// Coverage / Quality screen (#4251, epic #4249)
//
// Wire shapes for the capability routes the backend already serves but no
// screen previously rendered. All RAW JSON (not the v2 { ok, data } envelope):
//   GET /api/quality/coverage/{group}        (handlers_coverage.go)
//   GET /api/dependencies/{group}            (handlers_dependencies.go)
//   GET /api/quality/anti-patterns/{group}   (handlers_nplus1.go)
//   GET /api/groups/{group}/god-nodes        (handlers_graph.go)
//   GET /api/quality/trends/{group}          (handlers_quality_trends.go)
// ---------------------------------------------------------------------------

/** One production entity with no inbound TESTS edge. */
export interface UncoveredEntity {
  entity_id: string;
  name: string;
  kind: string;
  source_file: string;
  start_line: number;
  language: string;
  module?: string;
  /**
   * Owning repository slug, stamped by the group-level coverage aggregator
   * (#4551). Used to resolve the entity's source through the correct repo
   * root in a multi-repo group. May be absent on older backends — callers
   * should fall back to the group-level repo only as a last resort.
   */
  repo?: string;
  /** "high" | "medium" | "low". */
  severity: string;
  /**
   * Coverage state (#4662). For entities in the (not-reach-covered) uncovered
   * list this is either:
   *   "contract-only" — an offline contract spec asserts this endpoint's shape,
   *                     but no test calls it. NOT dangerously untested.
   *   "uncovered"     — neither reach-covered nor contract-covered.
   * Reach-covered entities never appear here. Absent on older backends.
   */
  state?: "reach" | "contract-only" | "uncovered";
}

/** Per-directory coverage statistics. */
export interface DirCoverage {
  dir: string;
  total: number;
  covered: number;
  /** Entities that are contract-covered-only (shape-asserted, not executed) (#4662). */
  contract_only?: number;
  coverage_pct: number;
}

/** Per-file coverage statistics (deepest grouping under a directory). */
export interface FileCoverage {
  file: string;
  dir: string;
  total: number;
  covered: number;
  /** Entities that are contract-covered-only (shape-asserted, not executed) (#4662). */
  contract_only?: number;
  coverage_pct: number;
}

/**
 * Per-file slice of uncovered entities (#4636), nested under their owning file
 * so the coverage tree can render them as leaf children: directory → file →
 * entity. Capped per file by the backend with `more` carrying the overflow.
 */
export interface FileUncovered {
  /** This file's uncovered entities, severity-sorted (high first), capped. */
  entities: UncoveredEntity[];
  /** Count of additional uncovered entities beyond the cap ("+N more"). */
  more?: number;
}

/** Per-module coverage statistics. */
export interface ModuleCoverage {
  module: string;
  total: number;
  covered: number;
  coverage_pct: number;
}

/** GET /api/quality/coverage/{group}. */
export interface GroupCoverageReport {
  group: string;
  total_production: number;
  covered_production: number;
  coverage_pct: number;
  /**
   * Secondary contract-covered band (#4662). `contract_covered_only` counts
   * endpoints whose shape an offline contract spec asserts but which no test
   * executes; `contract_covered_pct` is the union (reach + contract-only) band.
   * Neither is folded into `coverage_pct`, which stays pure reach-coverage.
   * Absent on older backends.
   */
  contract_covered_only?: number;
  contract_covered_pct?: number;
  total_tests: number;
  total_tests_edges: number;
  repos: number;
  uncovered_entities: UncoveredEntity[];
  by_directory: DirCoverage[];
  by_file: FileCoverage[];
  by_module: ModuleCoverage[];
  /**
   * Uncovered entities nested under their owning file path (#4636), keyed by
   * the forward-slash source path (matching FileCoverage.file). Powers the
   * directory → file → entity leaves of the coverage tree. Absent on older
   * backends — the UI falls back to the flat `uncovered_entities` list.
   */
  by_file_uncovered?: Record<string, FileUncovered>;
  /**
   * Real ingested LINE coverage (#5036), rolled up across the group from the
   * entity props stamped at index time (#5061). Present ONLY when a coverage
   * report (LCOV/Cobertura/JaCoCo) was actually ingested — its presence is the
   * authoritative "report ingestion ran" signal the provenance banner keys
   * off. Absent (the common case) ⇒ no report ingested; the banner degrades to
   * static reachability / capability coverage. Distinct from `coverage_pct`,
   * which is graph-derived reach coverage, not a measured line %.
   */
  line_coverage?: LineCoverageSummary;
  /**
   * Static test-reachability roll-up (#5037/#5062): which HTTP endpoints have
   * at least one test path reaching their handler vs none (the "orphan"
   * surface). Graph-derived, no execution — distinct from `coverage_pct` and
   * `line_coverage`. Absent on older backends; when `computed` is false the
   * reachability pass never ran for this group (pre-#5061 index) and the UI
   * must show "not computed — reindex" rather than implying everything is
   * untested.
   */
  reachability?: ReachabilitySummary;
}

/**
 * Endpoint-level static test-reachability summary (#5037/#5062). Surfaces the
 * orphan endpoints (no test path reaching their handler) as the actionable
 * untested surface for a parity rewrite.
 */
export interface ReachabilitySummary {
  /**
   * Whether the reachability pass ran for this group. When false, the counts
   * are meaningless (pre-#5061 index) and the UI shows a "not computed" notice.
   */
  computed: boolean;
  total_endpoints: number;
  tested_endpoints: number;
  orphan_endpoints: number;
  /** 100 * tested / total (0 when total === 0). */
  reachable_pct: number;
  /** Capped, sorted list of untested endpoints. */
  orphans: ReachOrphanEndpoint[];
  /** Count of orphan endpoints elided beyond the cap. */
  orphans_more?: number;
}

/** One untested endpoint row (#5062). */
export interface ReachOrphanEndpoint {
  id: string;
  name: string;
  kind: string;
  source_file?: string;
  start_line?: number;
  repo?: string;
}

/**
 * Group-level roll-up of ingested line coverage (#5066). The authoritative,
 * executed line % — never the same as `coverage_pct` (reach coverage).
 */
export interface LineCoverageSummary {
  /** `coverage_source` prop, e.g. "lcov". */
  source: string;
  covered_lines: number;
  total_lines: number;
  /** 100 * covered_lines / total_lines (0 when total_lines === 0). */
  coverage_pct: number;
  /** `coverage_measured_at` (RFC3339), latest across entities. May be absent. */
  measured_at?: string;
  /** How many entities carried a stamped `coverage_source` prop. */
  entities: number;
}

/** One declared / used / unused / phantom external dependency. */
export interface PackageEntry {
  name: string;
  package_manager: string;
  version?: string;
  dependency_kind: string;
  /** "used" | "unused" | "phantom". */
  status: "used" | "unused" | "phantom";
  source_file?: string;
  importers?: string[];
}

/** Group-level declared/used/unused/phantom totals. */
export interface DependencyGroupSummary {
  declared: number;
  used: number;
  unused: number;
  phantom: number;
}

/** Per-repo dependency breakdown. */
export interface RepoDepSummary {
  package_manager: string;
  declared: number;
  used: number;
  unused: number;
  phantom: number;
  packages: PackageEntry[];
}

/** GET /api/dependencies/{group}. */
export interface DependenciesReply {
  group: string;
  summary: DependencyGroupSummary;
  by_repo: Record<string, RepoDepSummary>;
}

/** One detected N+1 query anti-pattern site. */
export interface NPlusOneFinding {
  caller_entity_id: string;
  caller_name: string;
  caller_file: string;
  caller_start_line: number;
  query_entity_id: string;
  query_name: string;
  query_file: string;
  query_line: number;
  orm: string;
  language: string;
  loop_entity_id?: string;
  loop_subtype?: string;
  suggestion: string;
}

/** GET /api/quality/anti-patterns/{group}. */
export interface GroupNPlusOneReport {
  group: string;
  total_findings: number;
  entities_scanned: number;
  rels_scanned: number;
  by_orm: Record<string, number>;
  by_language: Record<string, number>;
  findings: NPlusOneFinding[];
}

/** One high-degree hotspot (god-node). */
export interface GodNode {
  id: string;
  label: string;
  kind: string;
  repo: string;
  pagerank: number;
}

/** GET /api/groups/{group}/god-nodes. */
export interface GodNodesReply {
  god_nodes: GodNode[];
}

/** A single data point in a quality metric series. */
export interface TrendPoint {
  /** ISO-8601 rebuild timestamp. */
  ts: string;
  v: number;
}

/** Time series for one quality metric. */
export interface MetricTrend {
  label: string;
  /** "%" | "count". */
  unit: string;
  lower_is_better: boolean;
  goal?: number;
  points: TrendPoint[];
  latest?: number;
  delta_7d?: number;
  delta_30d?: number;
}

/** GET /api/quality/trends/{group}. */
export interface QualityTrendsReply {
  group: string;
  days: number;
  metrics: MetricTrend[];
}

// ---------------------------------------------------------------------------
// Cross-repo links map (#4253, epic #4249)
//
// Wire shape for GET /api/groups/{group}/links (handlers_graph.go →
// handleGroupLinks). RAW JSON (not the v2 envelope): { links: [...] }.
// Each CrossRepoLink mirrors internal/dashboard/graphstate.go CrossRepoLink.
// `source`/`target` are canonical prefixed entity IDs of the form
// "<repo>::<localId>" (normalizeLinkEndpoints rewrites them at load time),
// so the repo of each side is the segment before "::".
// ---------------------------------------------------------------------------

/** One resolved cross-repo link (e.g. a frontend fetch → a backend endpoint). */
export interface CrossRepoLink {
  /** Source entity id, "<repo>::<localId>". */
  source: string;
  /** Target entity id, "<repo>::<localId>". */
  target: string;
  /** Relationship kind, e.g. "HTTP_FETCH", "PUBLISHES", "GRPC_CALL". */
  kind: string;
  /** Resolution confidence 0..1 (omitted ⇒ treat as unknown). */
  confidence?: number;
  /** Transport channel for message links (topic / queue name), when present. */
  channel?: string;
  /** HTTP method for HTTP links, when present. */
  method?: string;

  // Endpoint enrichment resolved from the source/target entities at serve time
  // (#4596). NOT persisted to the on-disk links file — populated by
  // enrichLinkEndpoints just before the /links payload is written. They let the
  // detail panel render readable names + open a real source-peek instead of
  // only a graph deep-link fallback (#4648). All optional: older daemons or
  // entities that no longer resolve simply omit them.
  /** Resolved display name of the source entity. */
  source_name?: string;
  /** Fully-qualified name of the source entity, when distinct from source_name. */
  source_qualified_name?: string;
  /** Repo-relative source file of the source entity. */
  source_file?: string;
  /** 1-based start line of the source entity in source_file. */
  source_line?: number;
  /** Resolved display name of the target (sink) entity. */
  target_name?: string;
  /** Fully-qualified name of the target entity, when distinct from target_name. */
  target_qualified_name?: string;
  /** Repo-relative source file of the target (sink) entity. */
  target_file?: string;
  /** 1-based start line of the target entity in target_file. */
  target_line?: number;
}

/** GET /api/groups/{group}/links — cross-repo link records. */
export interface GroupLinksReply {
  links: CrossRepoLink[];
}

// ---------------------------------------------------------------------------
// GraphQL resolver-effects surface (#4255, epic #4249)
//
// Wire shape for GET /api/graphql/{group} (handlers_graphql.go →
// handleGraphQL). RAW JSON (not the v2 envelope). Mirrors the Go structs in
// internal/dashboard/handlers_graphql.go. Surfaces the GraphQL extraction the
// graph already has: each framework emits a `verb=GRAPHQL` resolver endpoint
// carrying graphql_operation / graphql_root / graphql_field / framework, with
// effects stamped by the link effect-propagation pass and (when modeled) the
// flat auth contract.
// ---------------------------------------------------------------------------

/** One classified effect on a resolver (db_read/db_write/http_out/mutation/…). */
export interface GraphQLEffect {
  name: string;
  /** 0..1 confidence; omitted when unknown. */
  confidence?: number;
}

/** One resolved GraphQL field resolver. */
export interface GraphQLResolver {
  entity_id: string;
  repo: string;
  /** The GraphQL field this resolver answers (e.g. "users"). */
  field: string;
  /** Owning SDL object type / resolver class (graphql_root). */
  parent_type: string;
  /** "query" | "mutation" | "subscription". */
  operation: string;
  framework?: string;
  /** Underlying handler method (resolver_method). */
  method?: string;
  source_file?: string;
  start_line?: number;
  /** Effects from the link effect-propagation pass; [] when none/not run. */
  effects: GraphQLEffect[];
  /** "endpoint" | "direct" | "transitive" | "pure". */
  effect_source?: string;
  /** Auth — present only when resolver-level auth is statically modeled. */
  auth_required?: boolean;
  auth_roles?: string[];
  auth_method?: string;
}

/** Resolvers grouped under one parent SDL type. */
export interface GraphQLTypeGroup {
  parent_type: string;
  resolvers: GraphQLResolver[];
}

/** One SDL type definition surfaced as schema context. */
export interface GraphQLSchemaType {
  name: string;
  /** "type"|"interface"|"enum"|"union"|"input"|"scalar". */
  kind: string;
  repo: string;
  source_file?: string;
  start_line?: number;
  federated?: boolean;
}

/** GET /api/graphql/{group} — GraphQL resolver-effects report. */
export interface GraphQLReport {
  group: string;
  total_resolvers: number;
  total_types: number;
  query_count: number;
  mutation_count: number;
  subscription_count: number;
  with_effects_count: number;
  with_auth_count: number;
  resolvers_with_db_ops: number;
  frameworks: string[];
  groups: GraphQLTypeGroup[];
  schema_types: GraphQLSchemaType[];
}

// ---------------------------------------------------------------------------
// IaC / Infrastructure surface (#4256, epic #4249)
//
// Wire shape for GET /api/iac/{group} (handlers_iac.go → handleIaC). RAW JSON
// (not the v2 envelope). Mirrors the Go structs in
// internal/dashboard/handlers_iac.go. Surfaces the IaC extraction the graph
// has: every tool (aws-cdk / pulumi / bicep / cloudformation / sam /
// serverless-framework / terraform) emits resource entities carrying a tool
// label, a cross-tool resource_category, tool-native resource type, and a
// curated set of typed scalar config properties (instance_type / memory_size /
// timeout / runtime / region / …). Resource-to-resource edges model IAM grants,
// event-source wiring, plain dependencies, and stack/app/module topology.
//
// HONESTY: properties + relations render only when genuinely extracted. Many
// caps are partial (some props only on some tools; topology only where a stack/
// app/module is modeled); absent facets are omitted, never fabricated.
// ---------------------------------------------------------------------------

/** One stamped typed config property on a resource. */
export interface IaCProperty {
  key: string;
  value: string;
}

/**
 * One relationship facet on a resource.
 * facet: "grant" | "event_source" | "dependency" | "topology" | "trigger" | "output".
 */
export interface IaCRelation {
  facet: string;
  /** Raw graph edge kind (DEPENDS_ON / USES / IMPORTS / CONTAINS / JOINS / …). */
  kind: string;
  /** "out" (this → target) or "in" (source → this). */
  direction: string;
  /** Human-readable name (logical id) of the other endpoint. */
  target: string;
  /**
   * True when `target` is a graph-resolved entity name; false when it is a
   * fallback derived from the raw entity id (endpoint not found among indexed
   * entities). Drives a friendlier label + id tooltip in the UI (#4495).
   */
  target_resolved: boolean;
  /** Raw graph entity id of the other endpoint; shown on hover (#4495). */
  target_id: string;
  /**
   * Slug-prefixed entity id (`<repo>/<rawId>`) of the other endpoint WHEN that
   * endpoint is itself a rendered IaC resource node; empty otherwise. The
   * architecture-diagram view (#4526) joins this against IaCResource.entity_id
   * to draw an edge between two rendered nodes. (target_id alone is unprefixed
   * and not joinable against entity_id.)
   */
  target_entity_id?: string;
  /** Grant method or other edge qualifier, when set. */
  detail?: string;
}

/** One extracted IaC resource. */
export interface IaCResource {
  entity_id: string;
  repo: string;
  /** Logical id / resource name. */
  name: string;
  /** aws-cdk / pulumi / bicep / cloudformation / sam / serverless-framework / terraform. */
  tool: string;
  /** Tool-native type string (construct_type / resource_type). */
  resource_type?: string;
  /** Cross-tool resource_category join key. */
  category?: string;
  /** Logical id when distinct from name (CDK/CFN). */
  logical_id?: string;
  source_file?: string;
  start_line?: number;
  /**
   * Module / construct / stack the resource belongs to (#4526), derived from
   * the source-file directory (e.g. `infra/terraform/modules/network`). The
   * architecture diagram clusters resources sharing a module into a container.
   * Empty when no source path is known.
   */
  module?: string;
  /**
   * Environment this resource belongs to (#4657). For a module INSTANCE it is
   * derived from the instance file's path (envs/<env>/…); for a definition
   * resource it is propagated from the instance(s) that instantiate the
   * definition. A definition shared by several envs carries a comma-joined
   * list. Drives the env tabs. Empty for env-less resources.
   */
  env?: string;
  /**
   * For a module INSTANCE only (#4657): the repo-relative directory of the
   * module DEFINITION this instance instantiates (e.g. `modules/worker-service`).
   * Equals the `module` of the definition's resources, so the diagram joins on
   * it to project the definition's resources into the env.
   */
  definition_dir?: string;
  /** For a module INSTANCE only (#4657): the raw `source` value. */
  module_source?: string;
  /**
   * Slug-prefixed entity id of the module INSTANCE that instantiates this
   * resource (#4862). In Module group-by mode the diagram nests this resource
   * INSIDE that module's container box (ownership-as-containment) instead of
   * drawing an `instantiates` fan-out edge to it. Empty for resources not
   * instantiated by any module instance.
   */
  parent_id?: string;
  /** Curated typed config props; [] when none stamped. */
  properties: IaCProperty[];
  /** Grants / event-sources / dependencies / topology / triggers; [] when none. */
  relations: IaCRelation[];
}

/** Resources grouped under one iac_tool. */
export interface IaCToolGroup {
  tool: string;
  count: number;
  resources: IaCResource[];
}

/** GET /api/iac/{group} — IaC / Infrastructure report. */
export interface IaCReport {
  group: string;
  total_resources: number;
  total_grants: number;
  total_event_sources: number;
  total_dependencies: number;
  total_outputs: number;
  with_props_count: number;
  tools: string[];
  /**
   * Environments observed across module instances (#4657), sorted. Drives the
   * env tabs in the architecture view; empty when no env-scoped stacks exist.
   */
  envs: string[];
  /** resource_category → count across all tools. */
  counts_by_category: Record<string, number>;
  groups: IaCToolGroup[];
}

// ---------------------------------------------------------------------------
// Data-flow & Taint surface (#4265, epic #4249)
//
// Wire shape for GET /api/dataflow/{group} (handlers_dataflow.go →
// handleDataflow). RAW JSON (not the v2 envelope). Mirrors the Go structs in
// internal/dashboard/handlers_dataflow.go, which reads two sidecars:
//
//   - the taint sidecar → ranked SecurityFinding records (source→sink paths,
//     confidence, vulnerability category) produced by internal/links/taint_flow.go;
//   - the data-flow sidecar → request-input → sink DATA_FLOWS_TO edges (the
//     tainted field, sink_kind, inter-procedural hop_path) produced by
//     internal/links/dataflow_pass.go.
//
// HONESTY: only genuinely-extracted flows/findings are surfaced. The taint pass
// drops anything below confidence_floor and never fabricates a flow it could not
// soundly follow; when both sidecars are absent the screen shows a clean empty
// state. Source/sink names resolve to entity refs where the entity is known;
// otherwise the raw endpoint tail is shown — never invented.
// ---------------------------------------------------------------------------

/** One end (source or sink) of a flow / finding, resolved to an entity ref. */
export interface DataflowEndpoint {
  /** "<repo>/<id>" when resolved, else the raw sidecar endpoint key. */
  entity_id: string;
  repo?: string;
  /** Entity name, or the raw key tail when unresolved. */
  name: string;
  kind?: string;
  source_file?: string;
  line?: number;
  /** Taint-rule label that fired (source_primitive / sink_primitive); findings only. */
  primitive?: string;
}

/** One node on a finding's source→sink path. */
export interface DataflowPathStep {
  entity_id: string;
  name: string;
  repo?: string;
}

/** One ranked taint source→sink security finding. */
export interface SecurityFindingView {
  fingerprint: string;
  /** sql_injection / command_injection / path_traversal / xss / ssrf / … */
  category: string;
  confidence: number;
  source: DataflowEndpoint;
  sink: DataflowEndpoint;
  /** Ordered source→sink entity chain (inclusive). */
  path: DataflowPathStep[];
  /** len(path)-1 call hops, clamped at 0. */
  hops: number;
  explanation: string;
}

/** One request-input → sink DATA_FLOWS_TO edge. */
export interface TaintFlow {
  id: string;
  source: DataflowEndpoint;
  sink: DataflowEndpoint;
  relation?: string;
  confidence: number;
  /** Tainted request field that flows to the sink. */
  field?: string;
  /** Sink classification (sql / command / fs / http / template / …). */
  sink_kind?: string;
  /** Inter-procedural chain ("a -> b -> c") when present. */
  hop_path?: string;
  hop_count?: number;
}

/** GET /api/dataflow/{group} — Data-flow & Taint report. */
export interface DataflowReport {
  group: string;
  /** Ranked source→sink findings (confidence desc). */
  findings: SecurityFindingView[];
  /** Request-input → sink DATA_FLOWS_TO edges. */
  flows: TaintFlow[];
  total_findings: number;
  total_flows: number;
  /** Vulnerability category → finding count. */
  findings_by_category: Record<string, number>;
  /** sink_kind → flow count. */
  flows_by_sink_kind: Record<string, number>;
  /** Taint pass drop threshold (provenance). */
  confidence_floor: number;
  taint_method?: string;
  flow_method?: string;
}

// ---------------------------------------------------------------------------
// Dependency-Injection surface (#4266, epic #4249)
//
// Wire shape for GET /api/di/{group} (handlers_di.go → handleDI). RAW JSON
// (not the v2 envelope). Mirrors the Go structs in internal/dashboard/
// handlers_di.go, which walks the graph for INJECTED_INTO edges (provider
// INJECTED_INTO consumer) and groups providers by DI framework.
// ---------------------------------------------------------------------------

/** One consumer a provider is INJECTED_INTO (the ToID side of the edge). */
export interface DIConsumer {
  entity_id: string;
  name: string;
  /** Consumer entity Kind when resolved (SCOPE.Controller / SCOPE.Service / …). */
  kind?: string;
  repo?: string;
  source_file?: string;
  start_line?: number;
  /** Injection mechanism when recorded (constructor / field / param). */
  via?: string;
  /** DI qualifier / token disambiguator when present. */
  qualifier?: string;
}

/** One injectable provider with every consumer it injects into. */
export interface DIProvider {
  entity_id: string;
  name: string;
  kind?: string;
  repo?: string;
  source_file?: string;
  start_line?: number;
  framework?: string;
  consumers: DIConsumer[];
}

/** Providers observed under one DI framework. */
export interface DIFrameworkGroup {
  framework: string;
  count: number;
  providers: DIProvider[];
}

/** GET /api/di/{group} — DI providers grouped by framework. */
export interface DIReport {
  group: string;
  total_providers: number;
  total_consumers: number;
  total_injections: number;
  frameworks: string[];
  groups: DIFrameworkGroup[];
}

// ---------------------------------------------------------------------------
// Error-flow surface (#4267, epic #4249). GET /api/errorflow/{group} →
// handlers_errorflow.go, which rolls up SCOPE.ExceptionType nodes across the
// group: each exception type lists the callables that THROW it (throwers) and
// the handlers that CATCH it (catchers), with an honest uncaught flag.
// ---------------------------------------------------------------------------

/** One callable that throws or catches an exception type (THROWS/CATCHES FromID). */
export interface ErrorFlowSite {
  entity_id: string;
  name: string;
  /** Callable entity Kind when resolved (SCOPE.Function / SCOPE.Method / …). */
  kind?: string;
  repo?: string;
  source_file?: string;
  start_line?: number;
  /** Detector label the edge recorded (throw_new / raise / instanceof / …). */
  pattern?: string;
}

/** One exception type with everything that throws and catches it in the group. */
export interface ErrorFlowException {
  /** Bare exception type name, no "exception:" prefix (e.g. "ValidationError"). */
  type: string;
  throwers: ErrorFlowSite[];
  catchers: ErrorFlowSite[];
  /**
   * True when thrown ≥1× but no CATCHES edge anywhere in the indexed group.
   * Honestly this means "no typed catcher in graph" (see uncaught_reason) — it
   * may be a genuine leak OR caught by an untyped / out-of-scope handler.
   */
  uncaught: boolean;
  /** Qualifies `uncaught`: "no_catcher_in_graph" (the only value emitted today). */
  uncaught_reason?: string;
  throw_count: number;
  catch_count: number;
}

/** GET /api/errorflow/{group} — exception types rolled up with throw/catch sites. */
export interface ErrorFlowReport {
  group: string;
  total_exceptions: number;
  total_uncaught: number;
  total_throws: number;
  total_catches: number;
  exceptions: ErrorFlowException[];
}

/* ============================================================
   Endpoint downstream-DAG (#4349 backend / #4350 modal)

   GET /api/v2/groups/:id/paths/:hash/downstream-dag
     ?mode=spine|full &depth= &semantic= &verb=

   Wire shape mirrors internal/dashboard/v2_paths_downstream_dag.go.
   The endpoint's DOWNSTREAM rendered as a branching DAG rooted at the
   endpoint: endpoint → handler → service → repository → pipeline, plus
   JOINS_COLLECTION / THROWS / VALIDATES side-branches. Nodes are emitted
   ONCE; a convergence node accumulates >1 in-edge (the payload is already
   deduped — the renderer keys on node id and draws every incoming edge).
   ============================================================ */

/** Semantic kind of one directed DAG in-edge. */
export type DownstreamDAGEdgeKind =
  | "CALLS"
  | "HANDLER_CONTINUATION"
  | "JOINS_COLLECTION"
  | "THROWS"
  | "VALIDATES";

/** A node's place on the spine — drives layout + styling without re-deriving
 *  from kind. The endpoint root is always "endpoint". */
export type DownstreamDAGRole = "endpoint" | "handler" | "node" | "collection";

/** One low-level builder/predicate call collapsed INTO a spine node in spine
 *  mode (eq/gte/in/$lookup helpers, …). Expanding is inline — no refetch. */
export interface DownstreamDAGCollapsedChild {
  id: string;
  name: string;
  kind: string;
  file?: string;
  line?: number;
  /** Relationship via which the parent reached this collapsed child (usually CALLS). */
  edge_kind: string;
}

/** One node in the downstream DAG. IDs are repo-prefixed ("<slug>::<entityID>"). */
export interface DownstreamDAGNode {
  id: string;
  name: string;
  kind: string;
  file?: string;
  line?: number;
  repo: string;
  role?: DownstreamDAGRole;
  /** A deliberate leaf the walk stops at (e.g. a joined collection sink). */
  terminal?: boolean;
  // --- per-node enrichment (#4482/#4484 flow cards) ----------------------
  // Read at query-time from the resolved graph entity; each is omitted when
  // the underlying data is absent, so a card shows what it can and nothing more.
  /** Function/method signature for Operation/Handler nodes (monospace, truncated). */
  signature?: string;
  /** Finer kind/subtype when the entity carries one more specific than `kind`. */
  subtype?: string;
  /** SHORT one-line summary (≤140 chars) from the entity's docstring/description. */
  doc?: string;
  /** Effect kinds (db_read/db_write/http_out/fs/…) — rendered as small badges. */
  effects?: string[];
  /** Collection/table name for a collection-terminal node (role=collection). */
  collection?: string;
  /**
   * True when this node is defined OUTSIDE the indexed source — an external
   * library / unresolved symbol (#4558/#4564). When set, the renderer shows a
   * muted 'external' card and skips the (broken) source-peek. Optional: when
   * absent the renderer falls back to file/kind/name heuristics.
   */
  external?: boolean;
  /**
   * Package / library name an external node belongs to, when the backend can
   * recover it (e.g. "stripe", "django.db") — surfaced on the external card's
   * peek copy (#4564). Optional.
   */
  package?: string;
  /** Builder/predicate noise folded into this node in spine mode (empty in full). */
  collapsed_children?: DownstreamDAGCollapsedChild[];
}

/** One directed in-edge of the DAG. A convergence node has >1 edge with the same `to`. */
export interface DownstreamDAGEdge {
  from: string;
  to: string;
  kind: DownstreamDAGEdgeKind;
}

/** What (if anything) the caps dropped. All-false means complete within depth. */
export interface DownstreamDAGTruncation {
  depth_truncated: boolean;
  fanout_truncated: boolean;
  node_truncated: boolean;
}

/** GET /api/v2/groups/:id/paths/:hash/downstream-dag → the endpoint-flow modal payload. */
export interface DownstreamDAGResponse {
  root_id: string;
  path: string;
  verb: string;
  mode: "spine" | "full";
  depth: number;
  nodes: DownstreamDAGNode[];
  edges: DownstreamDAGEdge[];
  truncation: DownstreamDAGTruncation;
  /** Count of internal fan-out points (kept nodes with out-degree > 1). */
  branch_count: number;
}

// ---------------------------------------------------------------------------
// Control-flow / flowchart (#4819, epic #4820) —
// GET /api/v2/groups/:id/paths/:hash/control-flow
//
// The Flowchart view of the Downstream-flow modal: an on-demand control-flow
// graph (CFG) of the endpoint's HANDLER function, rendered with classic
// flowchart glyphs on the shared ELK canvas. Built server-side from the same
// internal/substrate CFG builder the grafel_control_flow MCP tool uses.
// ---------------------------------------------------------------------------

/** Detail level for the flowchart — maps 1:1 to the backend `detail` param and
 *  the modal's Detail slider. Each level is a superset of the previous:
 *   outline    → shapes + lines + complexity only
 *   decisions  → + condition text on decision/loop nodes (default)
 *   data       → + effect annotations on process nodes
 *   full       → + node labels (the trimmed source line) */
export type ControlFlowDetail = "outline" | "decisions" | "data" | "full";

/** Flowchart node glyph. start/end are rounded terminals, decision is a
 *  diamond, loop is a (back-edge target) diamond, process is a rectangle,
 *  return/throw are exit terminals. */
export type ControlFlowShape =
  | "start"
  | "end"
  | "return"
  | "throw"
  | "decision"
  | "loop"
  | "process";

/** Control-flow edge kind — drives edge labelling/routing on the flowchart:
 *  the true/false branches off a decision, the loop back-edge, the early
 *  return/throw exits, and plain sequential fall-through. */
export type ControlFlowEdgeKind =
  | "seq"
  | "branch_true"
  | "branch_false"
  | "loop_back"
  | "exit";

/** A terse effect annotation on a process node (data detail+). */
export interface ControlFlowEffect {
  effect: string;
  sink?: string;
}

/** One basic block / decision / terminal in the handler's CFG. */
export interface ControlFlowNode {
  id: string;
  shape: ControlFlowShape;
  line?: number;
  /** Trimmed source line — full detail only. */
  label?: string;
  /** Predicate text on decision/loop nodes — decisions detail and up. */
  condition?: string;
  /** Side-effect annotations on process nodes — data detail and up. */
  effects?: ControlFlowEffect[];
  /** Inlined-function frame this node belongs to (#4883), keyed by frame id
   *  ("f0" = the handler, "f1"…= inlined callees). Drives the per-hop boundary
   *  box / divider. Absent for a depth-1 (handler-only) graph. */
  func?: string;
  /** A call node whose in-repo callee couldn't be resolved/recursed into — an
   *  external/library/cross-repo call rendered as a leaf terminal (#4883). */
  external?: boolean;
}

/** One inlined-function frame in the interprocedural CFG (#4883) — lets the
 *  renderer delineate each inlined hop's boundary. f0 is always the handler. */
export interface ControlFlowFunction {
  func: string;
  name: string;
  kind?: string;
  file?: string;
  line?: number;
  depth: number;
  cyclomatic_complexity?: number;
}

/** One directed control-flow edge between two node ids. */
export interface ControlFlowEdge {
  from: string;
  to: string;
  kind: ControlFlowEdgeKind;
}

/** The resolved handler function the CFG was built from. */
export interface ControlFlowHandler {
  id: string;
  name: string;
  kind: string;
  file?: string;
  line?: number;
  repo: string;
}

/** GET /api/v2/groups/:id/paths/:hash/control-flow → the Flowchart-view payload. */
export interface ControlFlowResponse {
  path: string;
  verb: string;
  detail: ControlFlowDetail;
  /** Resolved handler language slug ("python","jsts",…). */
  language: string;
  /** False when no block detector exists for the language (degenerate graph),
   *  no handler resolved, or the source was unreadable — the modal then shows a
   *  graceful "flowchart not available" state. */
  supported: boolean;
  /** Human explanation when supported is false. */
  note?: string;
  /** The handler function the CFG was built from (absent when unresolved). */
  handler?: ControlFlowHandler;
  cyclomatic_complexity: number;
  branch_count: number;
  nodes: ControlFlowNode[];
  edges: ControlFlowEdge[];
  /** Resolved inline depth (#4883): 1 = handler CFG only, >=2 = callee CFGs
   *  inlined at in-repo call sites to that hop count. */
  depth?: number;
  /** Inlined-function frames in inline order (#4883); f0 is the handler. Present
   *  when depth >= 2 (or always at depth 1 as a single frame). */
  functions?: ControlFlowFunction[];
}

// ---------------------------------------------------------------------------
// Source peek (#4499) — GET /api/v2/groups/:id/source
// ---------------------------------------------------------------------------

/** One source line with its absolute 1-based number. */
export interface SourceLine {
  number: number;
  text: string;
}

/**
 * GET /api/v2/groups/:id/source — a window of source for a file:line ref,
 * read from the indexed repo working tree. Powers the shared <SourcePeek>
 * modal (the get_source equivalent for the UI).
 */
export interface SourceReply {
  /** Repo-relative path that was read. */
  file: string;
  /** Slug of the repo the file was found in. */
  repo: string;
  /** Highlighter language hint derived from the extension (e.g. "typescript"). */
  language: string;
  /** Target line the caller asked to center on (echoed back). */
  line: number;
  /** 1-based number of the first returned line. */
  start_line: number;
  /** 1-based number of the last returned line. */
  end_line: number;
  /** Full line count of the file (for "x of N" affordances). */
  total_lines: number;
  /** True when the returned window is a slice of a larger file. */
  truncated: boolean;
  /** The returned window. */
  lines: SourceLine[];
}

/* ============================================================
   Compound topology (architecture-diagram view) — Model 1, #4810/#4811.
   GET /api/v2/topology/:group/compound?group_by=infra|modules|tier
   ============================================================ */

export type CompoundTier =
  | "client"
  | "edge"
  | "auth"
  | "compute"
  | "data"
  | "messaging"
  | "external";

export type CompoundEdgeType =
  | "reads"
  | "writes"
  | "invokes"
  | "consumes"
  | "routes"
  | "depends";

export type CompoundGroupBy = "infra" | "modules" | "tier";

export interface CompoundNode {
  id: string;
  label: string;
  kind: string;
  tier: CompoundTier;
  repo: string;
  /** Containment chain outermost→innermost; empty in tier mode. */
  zone_path: string[];
}

export interface CompoundZone {
  id: string;
  label: string;
  parent_id?: string;
  /** "repo" | "module" | "cloud" | "network" | "service". */
  kind: string;
  /** Direct + transitive member node count. */
  node_count: number;
}

export interface CompoundEdge {
  source: string;
  target: string;
  type: CompoundEdgeType;
  label: string;
  /** Stable per-edge aggregation key (type + source + target). */
  agg_key: string;
}

export interface CompoundTopologyResponse {
  group_by: CompoundGroupBy;
  zones: CompoundZone[];
  nodes: CompoundNode[];
  edges: CompoundEdge[];
  /** Canonical left→right lane order. */
  tiers: CompoundTier[];
}
