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
export type GroupHealth = "healthy" | "warning" | "unindexed";

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
}

// ── Docs screen ─────────────────────────────────────────────────────────────

export type DocsEntityKind =
  | "function"
  | "component"
  | "hook"
  | "class"
  | "method"
  | "http_endpoint"
  | "module"
  | "folder"
  | "repo";

export interface DocsTreeNode {
  type: DocsEntityKind;
  name: string;
  id?: string;           // leaf only
  children?: DocsTreeNode[];
}

export interface DocsParam {
  name: string;
  type: string;
  desc: string;
}

export interface DocsEntityDetail {
  name: string;
  type: DocsEntityKind;
  repo: string;
  file: string;
  line: number;
  signature: string;
  description: string;
  aiGenerated: boolean;
  params: DocsParam[];
  returns: { type: string; desc?: string } | null;
  inbound: number;
  outbound: number;
  callers: string[];
  callees: string[];
  responseShapes?: { status: number; shape: string }[];
  stub?: boolean;
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

/** Detailed channel view — GET /api/topology/:group/topic/:topicId. */
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

/** Orphan publisher entry — GET /api/topology/:group/orphan-publishers. */
export interface OrphanPublisherEntry {
  id: string;
  label: string;
  broker_canonical: BrokerCanonical;
  repo: string;
  producers: string[];
  reason?: string;
}

/** Orphan subscriber entry — GET /api/topology/:group/orphan-subscribers. */
export interface OrphanSubscriberEntry {
  id: string;
  label: string;
  broker_canonical: BrokerCanonical;
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
  severity: Severity;
  issueType: RepairIssueType;
  entity: EntityRef;
  description: string;
  /** 0..1 */
  confidence: number;
  /** Unix ms. */
  detectedAt: number;
}

export interface EnrichmentCandidate {
  id: string;
  enrichmentType: EnrichmentType;
  entity: EntityRef;
  description: string;
  confidence: number;
  detectedAt: number;
}

export type Candidate = RepairCandidate | EnrichmentCandidate;

/** Hints stored per-candidate-id in local state and persisted via PUT hint. */
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
  name: string;
  kind: string;
  step_index: number;
  source_file: string;
  start_line?: number;
  repo: string;
  edge_kind: FlowRelationshipKind | null;
  step_kind?: StepKind;
  side_effects?: string[];
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
}

export interface FlowDeadEnd {
  process_id: string;
  process_name: string;
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
}

/** Response from GET /api/v2/groups/:id/paths. */
export interface PathsListResponse {
  backends: PathBackend[];
  totals: PathTotals;
}

/** One parameter on a path handler. */
export interface PathParameter {
  name: string;
  in: "path" | "query" | "body" | "header";
  type: string;
  required: boolean;
  desc: string;
  /** Verbs this param applies to (for verb filter). */
  verbs?: HttpVerb[];
}

/** Response shape per verb in the detail pane. */
export interface ResponseShape {
  verb: HttpVerb;
  status_codes: number[];
  keys: string[];
  dynamic?: boolean;
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
  description: {
    has_docs: boolean;
    summary: string;
    docs_path?: string;
    ai_generated?: boolean;
  };
  parameters: PathParameter[];
  response_shapes: ResponseShape[];
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
