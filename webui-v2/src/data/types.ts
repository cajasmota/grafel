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
