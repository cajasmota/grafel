/* ============================================================
   lib/api.ts — typed grafel daemon client.

   Thin, typed fetch wrapper. Every screen's data hook calls through
   this client (never raw fetch), so auth headers, base URL, and error
   normalization live in exactly one place.

   The daemon base URL is configurable via VITE_AG_API_BASE so the new
   UI never hardcodes the live :47274 daemon during development.
   ============================================================ */

import type {
  Group,
  Entity,
  Community,
  DocPage,
  DocsTreeResponse,
  GraphPayloadWire,
  EntityDetailWire,
  SettingsGroup,
  SettingsFeatures,
  DoctorCheck,
  ToolsStatus,
  ToolsApplyReply,
  TopologyResponse,
  CompoundTopologyResponse,
  CompoundGroupBy,
  TopologyChannelDetail,
  OrphanPublisherEntry,
  OrphanSubscriberEntry,
  V2CandidatesResponse,
  FlowsListResponse,
  FlowDetailResponse,
  FlowDeadEndsResponse,
  EventFlowsListResponse,
  EventFlowDetailResponse,
  PathsListResponse,
  PathDetail,
  PathPostureResponse,
  OrphansResponse,
  ShapeResponse,
  SystemStatus,
  SystemActionReply,
  SystemLogsReply,
  UpdateCheckReply,
  PatternsListReply,
  PatternRow,
  PatternDeleteReply,
  PatternGCReply,
  OrphanAuditReply,
  FixturesReply,
  RecallReply,
  JobAck,
  ActionJob,
  CleanupReply,
  QuarantineListReply,
  QuarantineActionReply,
  UpdateApplyReply,
  ScanInspectReply,
  WizardRepo,
  MCPToolsDetectReply,
  FsListReply,
  ModuleAnalysisResponse,
  GroupRefsResponse,
  DiffResult,
  DaemonModeReply,
  SetDaemonModeReply,
  GroupAuthCoverageReport,
  GroupSecretsReport,
  GroupCyclesReport,
  GroupCoverageReport,
  DependenciesReply,
  GroupNPlusOneReport,
  GodNodesReply,
  QualityTrendsReply,
  GroupLinksReply,
  GraphQLReport,
  IaCReport,
  DataflowReport,
  DIReport,
  ErrorFlowReport,
  DownstreamDAGResponse,
  ControlFlowResponse,
  ControlFlowDetail,
  SourceReply,
} from "@/data/types";

const BASE = import.meta.env.VITE_AG_API_BASE ?? "/api";
const BASE_V2 = import.meta.env.VITE_AG_API_BASE_V2 ?? "/api/v2";

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    /** Machine-readable code from a v2 error envelope, when present. */
    public code?: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    headers: { "Content-Type": "application/json", ...init?.headers },
    ...init,
  });
  if (!res.ok) {
    throw new ApiError(res.status, `${init?.method ?? "GET"} ${path} failed: ${res.status}`);
  }
  return (await res.json()) as T;
}

/* ------------------------------------------------------------------ *
 * v2 envelope helpers — every /api/v2 response is wrapped in
 *   { ok: true, data, pagination? } | { ok: false, error: { code, message } }
 * (see internal/dashboard/API_V2.md). requestV2 unwraps `data` or throws
 * a typed ApiError carrying the canonical error code.
 * ------------------------------------------------------------------ */

interface V2Ok<T> {
  ok: true;
  data: T;
  pagination?: { limit: number; offset: number; total: number };
}
interface V2Err {
  ok: false;
  error: { code: string; message: string };
}

async function requestV2<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE_V2}${path}`, {
    headers: { "Content-Type": "application/json", ...init?.headers },
    ...init,
  });
  let body: V2Ok<T> | V2Err;
  try {
    body = (await res.json()) as V2Ok<T> | V2Err;
  } catch {
    throw new ApiError(res.status, `${init?.method ?? "GET"} ${path} failed: ${res.status}`);
  }
  if (!body.ok) {
    throw new ApiError(res.status, body.error.message, body.error.code);
  }
  return body.data;
}

/**
 * The typed surface of the daemon. Screen tickets add methods here as
 * they need them; nothing else in the app issues network calls.
 */
export interface Meta {
  version: string;
  api_versions: string[];
  groups: string[];
}

export const api = {
  /**
   * #1527 — absolute URL of the real-time per-repo / per-MODULE indexing
   * progress SSE stream for a group. The stream lives on the v1 surface
   * (/api/index-progress/:group), not /api/v2, so it derives from BASE. The
   * caller opens an EventSource against this URL.
   */
  progressStreamUrl: (group: string) =>
    `${BASE}/index-progress/${encodeURIComponent(group)}`,

  /** v2 — daemon bootstrap: version, supported surfaces, group slugs. */
  getMeta: () => requestV2<Meta>("/meta"),
  /** v2 — rich group list for the Landing screen. */
  listGroups: () => requestV2<Group[]>("/groups"),
  /** v2 — create an empty group from a name (Landing wizard). */
  createGroup: (name: string) =>
    requestV2<Group>("/groups", { method: "POST", body: JSON.stringify({ name }) }),

  /**
   * GET /api/v2/fs/list — list the subdirectories of an absolute path on the
   * daemon's own filesystem (#1529). Powers the wizard's server-side folder
   * browser: navigating a folder yields its absolute path, so picking it is
   * enough to proceed — no manual paste. Empty path defaults to the daemon's
   * home directory (with Documents/Projects shortcuts). Path errors are
   * carried in `error` (HTTP 200), not thrown.
   */
  fsList: (path?: string) =>
    requestV2<FsListReply>(`/fs/list${path ? `?path=${encodeURIComponent(path)}` : ""}`),

  // --- v2 create-group / add-repo scan wizard (#1517) ---
  /**
   * POST /api/v2/scan/inspect — resolve a server-side path and detect its
   * stack + monorepo layout. No registry writes; the "detect" wizard step.
   */
  scanInspect: (path: string) =>
    requestV2<ScanInspectReply>("/scan/inspect", {
      method: "POST",
      body: JSON.stringify({ path }),
    }),

  /**
   * GET /api/v2/mcp-tools/detect — list the detected MCP-capable AI tools with
   * the smart-default (recently-used / previously-configured) selection so the
   * wizard's "Configure MCP for which tools?" step can render checkboxes (#5344).
   */
  detectMCPTools: () => requestV2<MCPToolsDetectReply>("/mcp-tools/detect"),

  /**
   * POST /api/v2/groups/from-scan — create a group, register the scanned repos,
   * and enqueue an async index job. Returns a 202 JobAck the wizard streams.
   * `mcpTools` (#5344): when provided, registers the grafel MCP server in exactly
   * those tool IDs (empty = none); omit for back-compat (every detected tool).
   */
  createGroupFromScan: (name: string, repos: WizardRepo[], mcpTools?: string[]) =>
    requestV2<JobAck>("/groups/from-scan", {
      method: "POST",
      body: JSON.stringify(mcpTools === undefined ? { name, repos } : { name, repos, mcpTools }),
    }),

  /**
   * POST /api/v2/groups/:id/repos/scan — register scanned repos into an
   * existing group + enqueue an async index job (Settings add-repo). 202 JobAck.
   */
  scanReposIntoGroup: (groupId: string, repos: WizardRepo[]) =>
    requestV2<JobAck>(`/groups/${encodeURIComponent(groupId)}/repos/scan`, {
      method: "POST",
      body: JSON.stringify({ repos }),
    }),

  // --- v2 Docs portal — generated markdown documents (#1552) ---
  getDocsTree: (groupId: string) =>
    requestV2<DocsTreeResponse>(`/groups/${groupId}/docs/tree`),
  getDocPage: (groupId: string, path: string) =>
    requestV2<DocPage>(
      `/groups/${encodeURIComponent(groupId)}/docs/page?path=${encodeURIComponent(path)}`,
    ),
  /**
   * Docs export URL (#1624) — extensible by `format` (zip first) and `kind`
   * (all | technical | business). Returns a plain URL string suitable for use
   * as an anchor `href` or programmatic `window.location.assign` target; the
   * daemon streams the archive directly so the browser handles the download
   * lifecycle.
   */
  docsExportUrl: (
    groupId: string,
    opts?: { format?: "zip"; kind?: "all" | "technical" | "business" },
  ) => {
    const qs = new URLSearchParams();
    qs.set("format", opts?.format ?? "zip");
    qs.set("kind", opts?.kind ?? "all");
    return `/api/v2/groups/${encodeURIComponent(groupId)}/docs/export?${qs.toString()}`;
  },
  /**
   * Graph export URL (#1627) — parallel to docsExportUrl. Streams an archive
   * of the indexed store (graph.fb, enrichments, links, embeddings, fleet
   * config) for the entire group. `kind=all` also bundles the generated docs
   * so a single archive backs up the whole group surface. Returns a plain
   * URL string for use as an anchor `href` so the browser handles the
   * download lifecycle.
   */
  graphExportUrl: (
    groupId: string,
    opts?: { format?: "zip"; kind?: "graph" | "all" },
  ) => {
    const qs = new URLSearchParams();
    qs.set("format", opts?.format ?? "zip");
    qs.set("kind", opts?.kind ?? "graph");
    return `/api/v2/groups/${encodeURIComponent(groupId)}/export?${qs.toString()}`;
  },
  /**
   * POST /api/v2/groups/import (#1627) — restore a group from a zip archive
   * previously produced by graphExportUrl. The browser uploads the zip as a
   * multipart/form-data body keyed `file`. `force=true` overwrites an
   * existing group of the same name; `name` registers the archive under a
   * different group slug.
   */
  graphImport: async (
    file: File,
    opts?: { force?: boolean; name?: string },
  ): Promise<{ group: string; repos: string[]; forced: boolean }> => {
    const qs = new URLSearchParams();
    if (opts?.force) qs.set("force", "true");
    if (opts?.name) qs.set("name", opts.name);
    const fd = new FormData();
    fd.append("file", file);
    const suffix = qs.toString() ? `?${qs.toString()}` : "";
    const res = await fetch(`${BASE_V2}/groups/import${suffix}`, {
      method: "POST",
      body: fd,
    });
    let body: V2Ok<{ group: string; repos: string[]; forced: boolean }> | V2Err;
    try {
      body = (await res.json()) as
        | V2Ok<{ group: string; repos: string[]; forced: boolean }>
        | V2Err;
    } catch {
      throw new ApiError(res.status, `POST /groups/import failed: ${res.status}`);
    }
    if (!body.ok) {
      throw new ApiError(res.status, body.error.message, body.error.code);
    }
    return body.data;
  },
  /** v2 — the full graph payload (nodes/edges/communities/repos) for the Graph
   *  screen. `params` maps to the daemon's repo/kind filters. */
  getGraph: (groupId: string, params?: { repos?: string[]; filterKind?: string; lod?: string }) => {
    const qs = new URLSearchParams();
    if (params?.repos && params.repos.length > 0) qs.set("repos", params.repos.join(","));
    if (params?.filterKind) qs.set("filter_kind", params.filterKind);
    if (params?.lod) qs.set("lod", params.lod);
    const suffix = qs.toString() ? `?${qs.toString()}` : "";
    return requestV2<GraphPayloadWire>(`/graph/${encodeURIComponent(groupId)}${suffix}`);
  },

  /**
   * #5446 increment 2 — the absolute URL for the progressive graph SSE stream
   * (GET /api/v2/graph/:group/stream). Returned (not fetched) because the
   * stream is consumed via EventSource, not the fetch wrapper; centralizing it
   * here keeps the configurable v2 base URL in one place. The streamed
   * node/edge JSON is byte-for-byte the full-payload shape, so the same
   * normalize path applies.
   */
  graphStreamUrl: (groupId: string, params?: { repos?: string[]; filterKind?: string }) => {
    const qs = new URLSearchParams();
    if (params?.repos && params.repos.length > 0) qs.set("repos", params.repos.join(","));
    if (params?.filterKind) qs.set("filter_kind", params.filterKind);
    const suffix = qs.toString() ? `?${qs.toString()}` : "";
    return `${BASE_V2}/graph/${encodeURIComponent(groupId)}/stream${suffix}`;
  },

  /**
   * GET /api/v2/groups/:group/modules/analysis — module-level GDS (SCC,
   * PageRank, betweenness over the aggregated module graph). Powers the
   * "Module overview" mode on the Graph screen (#1386, closes epic #1380
   * alongside #1384 which shipped the algorithms + endpoint).
   *
   * `topN` defaults to 10 (top hubs lists); `minSccSize` to 2; the full
   * `modules` + `edges` arrays are returned regardless of `topN` so the
   * UI can lay out every module-level node.
   */
  getModuleAnalysis: (
    groupId: string,
    params?: { topN?: number; minSccSize?: number; repoFilter?: string[] },
  ) => {
    const qs = new URLSearchParams();
    if (params?.topN) qs.set("top_n", String(params.topN));
    if (params?.minSccSize) qs.set("min_scc_size", String(params.minSccSize));
    if (params?.repoFilter && params.repoFilter.length > 0)
      qs.set("repo_filter", params.repoFilter.join(","));
    const suffix = qs.toString() ? `?${qs.toString()}` : "";
    return requestV2<ModuleAnalysisResponse>(
      `/groups/${encodeURIComponent(groupId)}/modules/analysis${suffix}`,
    );
  },

  // --- Topology screen (#1440, epic #1432) ---
  // v2 endpoints wrap collectTopologyResponse + buildTopicDetail in the v2 envelope.
  // All data is static graph extraction — no runtime metrics.

  /**
   * GET /api/v2/topology/:group
   * Full topology payload in the v2 envelope (topics/queues/channels/functions/broker_groups).
   */
  getTopology: (groupId: string) =>
    requestV2<TopologyResponse>(`/topology/${encodeURIComponent(groupId)}`),

  /**
   * GET /api/v2/topology/:group/compound?group_by=infra|modules|tier
   * Compound architecture-diagram payload (Model 1, #4810/#4811): nested
   * containment zones + node tier facets + typed/aggregatable edges.
   */
  getCompoundTopology: (groupId: string, groupBy: CompoundGroupBy) =>
    requestV2<CompoundTopologyResponse>(
      `/topology/${encodeURIComponent(groupId)}/compound?group_by=${groupBy}`,
    ),

  /**
   * GET /api/v2/topology/:group/topic/:topicId
   * Detailed channel view in the v2 envelope.
   */
  getTopologyDetail: (groupId: string, topicId: string) =>
    requestV2<TopologyChannelDetail>(
      `/topology/${encodeURIComponent(groupId)}/topic/${encodeURIComponent(topicId)}`,
    ),

  /**
   * GET /api/topology/:group/orphan-publishers
   * Orphan publishers — v1 endpoint (no v2 wrapper needed for these list endpoints).
   */
  getOrphanPublishers: (groupId: string) =>
    request<OrphanPublisherEntry[]>(`/topology/${encodeURIComponent(groupId)}/orphan-publishers`),

  /**
   * GET /api/topology/:group/orphan-subscribers
   * Orphan subscribers — v1 endpoint.
   */
  getOrphanSubscribers: (groupId: string) =>
    request<OrphanSubscriberEntry[]>(`/topology/${encodeURIComponent(groupId)}/orphan-subscribers`),

  // --- v1 surfaces still used by other (placeholder) screens ---
  getGroup: (groupId: string) => request<Group>(`/groups/${groupId}`),
  /** v1 — Tier-3 entity detail for the inspector (lazy, on node click). */
  getEntityDetail: (groupId: string, entityId: string) =>
    request<EntityDetailWire>(
      `/graph/${encodeURIComponent(groupId)}/entity/${encodeURIComponent(entityId)}`,
    ),
  listCommunities: (groupId: string) => request<Community[]>(`/groups/${groupId}/communities`),
  searchEntities: (groupId: string, q: string) =>
    request<Entity[]>(`/groups/${groupId}/entities?q=${encodeURIComponent(q)}`),

  // --- v2 Settings screen ---
  /** GET /api/v2/groups/:id — full SettingsGroup for the Settings screen. */
  getSettingsGroup: (groupId: string) =>
    requestV2<SettingsGroup>(`/groups/${encodeURIComponent(groupId)}`),

  /** PATCH /api/v2/groups/:id/features — live-save feature toggles. */
  patchFeatures: (groupId: string, features: SettingsFeatures) =>
    requestV2<SettingsFeatures>(`/groups/${encodeURIComponent(groupId)}/features`, {
      method: "PATCH",
      body: JSON.stringify(features),
    }),

  /** PATCH /api/v2/groups/:id/docs — update docsPath. */
  patchDocs: (groupId: string, docsPath: string) =>
    requestV2<{ docsPath: string }>(`/groups/${encodeURIComponent(groupId)}/docs`, {
      method: "PATCH",
      body: JSON.stringify({ docsPath }),
    }),

  /**
   * POST /api/v2/groups/:id/rebuild — trigger an ASYNC group rebuild (#1512).
   * Returns 202 + a job id immediately; poll getJob / stream pollJob.
   */
  rebuildGroup: (groupId: string) =>
    requestV2<JobAck>(`/groups/${encodeURIComponent(groupId)}/rebuild`, {
      method: "POST",
    }),

  /** DELETE /api/v2/groups/:id — delete the group. */
  deleteGroup: (groupId: string) =>
    requestV2<{ deleted: string }>(`/groups/${encodeURIComponent(groupId)}`, {
      method: "DELETE",
    }),

  /** POST /api/v2/groups/:id/repos — add a repo to the group. */
  addRepo: (groupId: string, slug: string, path: string) =>
    requestV2<SettingsGroup>(`/groups/${encodeURIComponent(groupId)}/repos`, {
      method: "POST",
      body: JSON.stringify({ slug, path }),
    }),

  /** DELETE /api/v2/groups/:id/repos/:slug — remove a repo from the group. */
  removeRepo: (groupId: string, repoSlug: string, keepCache = false) =>
    requestV2<{ removed: string }>(
      `/groups/${encodeURIComponent(groupId)}/repos/${encodeURIComponent(repoSlug)}?keepCache=${keepCache}`,
      { method: "DELETE" },
    ),

  /** POST /api/v2/groups/:id/repos/:slug/rebuild — ASYNC repo rebuild → 202 + job id (#1512). */
  rebuildRepo: (groupId: string, repoSlug: string) =>
    requestV2<JobAck>(`/groups/${encodeURIComponent(groupId)}/repos/${encodeURIComponent(repoSlug)}/rebuild`, {
      method: "POST",
    }),

  /** POST /api/v2/groups/:id/repos/:slug/reset — ASYNC wipe + rebuild → 202 + job id (#1512). */
  resetRepo: (groupId: string, repoSlug: string) =>
    requestV2<JobAck>(`/groups/${encodeURIComponent(groupId)}/repos/${encodeURIComponent(repoSlug)}/reset`, {
      method: "POST",
    }),

  /** GET /api/v2/jobs/:id — poll the status/progress of an async action job (#1512). */
  getJob: (jobId: string) => requestV2<ActionJob>(`/jobs/${encodeURIComponent(jobId)}`),

  /** PATCH /api/v2/groups/:id/repos/:slug/monorepo — update package selection. */
  patchMonorepo: (groupId: string, repoSlug: string, packages: string[]) =>
    requestV2<{ saved: boolean; packages: string[]; watcher_reloaded: boolean }>(
      `/groups/${encodeURIComponent(groupId)}/repos/${encodeURIComponent(repoSlug)}/monorepo`,
      { method: "PATCH", body: JSON.stringify({ packages }) },
    ),

  /** POST /api/v2/groups/:id/doctor — run health check, returns DoctorCheck[]. */
  runDoctor: (groupId: string) =>
    requestV2<DoctorCheck[]>(`/groups/${encodeURIComponent(groupId)}/doctor`, {
      method: "POST",
    }),

  /**
   * GET /api/v2/groups/:id/tools — AI-tool selection status (#5257). Returns
   * every adapter with its enabled + detected state.
   */
  getTools: (groupId: string) =>
    requestV2<ToolsStatus>(`/groups/${encodeURIComponent(groupId)}/tools`),

  /**
   * PUT /api/v2/groups/:id/tools — set the desired enabled tool-set. The
   * delta is applied in-process (rules files + MCP entries) and a per-tool
   * summary is returned.
   */
  putTools: (groupId: string, tools: string[]) =>
    requestV2<ToolsApplyReply>(`/groups/${encodeURIComponent(groupId)}/tools`, {
      method: "PUT",
      body: JSON.stringify({ tools }),
    }),

  // --- v2 Pending screen (#1442) ---
  /** Fetch repair + enrichment candidates for a group. */
  listCandidates: (groupId: string, tab?: "repairs" | "enrichments") =>
    requestV2<V2CandidatesResponse>(
      `/groups/${encodeURIComponent(groupId)}/candidates${tab ? `?tab=${tab}` : ""}`,
    ),
  /**
   * Persist a hint for an entity.  Pass the stable `entityId` field from the
   * candidate (NOT the ephemeral `id`) so hints survive candidate-ID churn
   * across re-index sweeps (#1518).  Empty string clears the hint.
   */
  saveHint: (groupId: string, entityId: string, hint: string) =>
    requestV2<{ hint: string; entityId: string }>(
      `/groups/${encodeURIComponent(groupId)}/candidates/${encodeURIComponent(entityId)}/hint`,
      { method: "PUT", body: JSON.stringify({ hint }) },
    ),
  // --- Flows (Process Flow Explorer) ---
  listFlows: (groupId: string, params?: { tab?: string; search?: string; limit?: number }) => {
    const q = new URLSearchParams();
    if (params?.search) q.set("search", params.search);
    if (params?.limit) q.set("limit", String(params.limit));
    if (params?.tab === "crossrepo") q.set("cross_stack_only", "false");
    const qs = q.toString() ? `?${q.toString()}` : "";
    return request<FlowsListResponse>(`/flows/${groupId}${qs}`);
  },
  getFlowDetail: (groupId: string, processId: string) =>
    request<FlowDetailResponse>(`/flows/${groupId}/${encodeURIComponent(processId)}`),
  listFlowDeadEnds: (groupId: string) =>
    request<FlowDeadEndsResponse>(`/flows/${groupId}/dead-ends`),
  listFlowTruncated: (groupId: string) =>
    request<FlowsListResponse>(`/flows/${groupId}/truncated`),
  generateFlowDocs: (groupId: string, processId: string) =>
    request<{ status: string; message: string }>(
      `/flows/${groupId}/${encodeURIComponent(processId)}/trigger-enrichment`,
      { method: "POST" },
    ),

  // --- Event Flows (#1944 Phase 1) — pub/sub multi-hop chains ---
  /** GET /api/event-flows/:group — list event-flow chains. */
  listEventFlows: (groupId: string, params?: { seed?: string; minSteps?: number; limit?: number }) => {
    const q = new URLSearchParams();
    if (params?.seed) q.set("seed", params.seed);
    if (params?.minSteps != null) q.set("min_steps", String(params.minSteps));
    if (params?.limit != null) q.set("limit", String(params.limit));
    const qs = q.toString() ? `?${q.toString()}` : "";
    return request<EventFlowsListResponse>(`/event-flows/${encodeURIComponent(groupId)}${qs}`);
  },
  /** GET /api/event-flows/:group/:eventFlowId — full chain detail. */
  getEventFlowDetail: (groupId: string, eventFlowId: string) =>
    request<EventFlowDetailResponse>(
      `/event-flows/${encodeURIComponent(groupId)}/${encodeURIComponent(eventFlowId)}`,
    ),

  // --- v2 Paths screen ---
  /** GET /api/v2/groups/:id/paths — backend-grouped route list + totals. */
  listPaths: (groupId: string) =>
    requestV2<PathsListResponse>(`/groups/${encodeURIComponent(groupId)}/paths`),

  /** GET /api/v2/groups/:id/paths/:hash — full route detail (Swagger++). */
  getPathDetail: (groupId: string, pathHash: string) =>
    requestV2<PathDetail>(`/groups/${encodeURIComponent(groupId)}/paths/${encodeURIComponent(pathHash)}`),

  /** GET /api/v2/groups/:id/paths/orphans — orphan caller list. */
  listOrphans: (groupId: string) =>
    requestV2<OrphansResponse>(`/groups/${encodeURIComponent(groupId)}/paths/orphans`),

  /**
   * GET /api/v2/groups/:id/paths/:hash/downstream-dag — the endpoint's
   * DOWNSTREAM as a branching DAG rooted at the endpoint (#4349 backend,
   * #4350 modal). Drives the shared <FlowDag> component.
   *
   *   mode      — "spine" (default; collapses query-builder/predicate noise into
   *               owning nodes as collapsed_children) | "full" (every node).
   *   depth     — max hops from the endpoint (default 8, clamped server-side to
   *               [1, 24]).
   *   semantic  — include JOINS_COLLECTION/THROWS/VALIDATES side-edges (default
   *               true); false walks the CALLS spine only.
   *   verb      — disambiguate when a path hash maps to several verb endpoints.
   */
  getPathDownstreamDAG: (
    groupId: string,
    pathHash: string,
    params?: { mode?: "spine" | "full"; depth?: number; semantic?: boolean; verb?: string },
  ) => {
    const qs = new URLSearchParams();
    if (params?.mode) qs.set("mode", params.mode);
    if (params?.depth != null) qs.set("depth", String(params.depth));
    if (params?.semantic === false) qs.set("semantic", "0");
    if (params?.verb) qs.set("verb", params.verb);
    const suffix = qs.toString() ? `?${qs.toString()}` : "";
    return requestV2<DownstreamDAGResponse>(
      `/groups/${encodeURIComponent(groupId)}/paths/${encodeURIComponent(pathHash)}/downstream-dag${suffix}`,
    );
  },

  /**
   * GET /api/v2/groups/:id/paths/:hash/control-flow — the Flowchart view of the
   * Downstream-flow modal (#4819). Returns the on-demand control-flow graph
   * (CFG) of the endpoint's handler function, parameterised by `detail`
   * (outline|decisions|data|full — the Detail slider). `verb` disambiguates a
   * multi-verb path hash. Fetched lazily (only when the modal is open and the
   * Flowchart view is selected) so the main paths payload stays lean.
   */
  getPathControlFlow: (
    groupId: string,
    pathHash: string,
    params?: { detail?: ControlFlowDetail; verb?: string; depth?: number },
  ) => {
    const qs = new URLSearchParams();
    if (params?.detail) qs.set("detail", params.detail);
    if (params?.verb) qs.set("verb", params.verb);
    // #4883: inline depth (call-hops to splice). 1 = handler CFG only.
    if (params?.depth && params.depth > 1) qs.set("depth", String(params.depth));
    const suffix = qs.toString() ? `?${qs.toString()}` : "";
    return requestV2<ControlFlowResponse>(
      `/groups/${encodeURIComponent(groupId)}/paths/${encodeURIComponent(pathHash)}/control-flow${suffix}`,
    );
  },

  /**
   * GET /api/v2/groups/:id/paths/:hash/posture — lazy posture +
   * effective-contract sections for the detail pane (#4254). Reuses the
   * grafel_endpoint_posture / grafel_effective_contract computation
   * server-side. Fetched only when a path is opened so the main paths payload
   * stays lean.
   */
  getPathPosture: (groupId: string, pathHash: string) =>
    requestV2<PathPostureResponse>(
      `/groups/${encodeURIComponent(groupId)}/paths/${encodeURIComponent(pathHash)}/posture`,
    ),

  /**
   * GET /api/v2/groups/:id/shape — lazy ShapeTree subtree resolver
   * (refs #1935 Phase 1). Returns one field row per CONTAINS child of
   * the requested class entity, with type + annotations metadata and a
   * has_children flag the caller uses to decide whether to render an
   * expand glyph. Accepts either `type_entity_id` (prefixed
   * "slug:entity_id" form) or a bare `type` name to be resolved
   * group-wide.
   */
  getShape: (groupId: string, opts: { typeEntityId?: string; type?: string }) => {
    const qs = new URLSearchParams();
    if (opts.typeEntityId) qs.set("type_entity_id", opts.typeEntityId);
    else if (opts.type) qs.set("type", opts.type);
    return requestV2<ShapeResponse>(
      `/groups/${encodeURIComponent(groupId)}/shape?${qs.toString()}`,
    );
  },

  /**
   * GET /api/v2/groups/:id/source — a window of source for any file:line ref
   * (#4499), read from the indexed repo working tree. Powers the shared
   * <SourcePeek> modal: small files come back whole, large files as a window
   * centered on `line`. `repo` pins resolution when the same relative path
   * exists in more than one repo of the group; omit it to search all repos.
   */
  getSource: (
    groupId: string,
    opts: { file: string; line?: number; context?: number; repo?: string },
  ) => {
    const qs = new URLSearchParams();
    qs.set("file", opts.file);
    if (opts.line != null && opts.line > 0) qs.set("line", String(opts.line));
    if (opts.context != null) qs.set("context", String(opts.context));
    if (opts.repo) qs.set("repo", opts.repo);
    return requestV2<SourceReply>(
      `/groups/${encodeURIComponent(groupId)}/source?${qs.toString()}`,
    );
  },

  // --- Operations screen (system / patterns / quality / updates) ---
  // These call the existing v1 endpoints; no v2 wrapper needed since
  // the data shapes are stable and the Operations surface is new.

  /** GET /api/system — live daemon status snapshot. */
  getSystemStatus: () => request<SystemStatus>("/system"),

  /** POST /api/system/restart — signal daemon to restart (confirm required). */
  restartDaemon: () =>
    request<SystemActionReply>("/system/restart", { method: "POST" }),

  /** POST /api/system/stop — SIGTERM daemon (danger, confirm required). */
  stopDaemon: () =>
    request<SystemActionReply>("/system/stop", { method: "POST" }),

  /** GET /api/system/logs — tail of daemon.log. */
  getSystemLogs: (params?: { n?: number; q?: string; severity?: string }) => {
    const qs = new URLSearchParams();
    if (params?.n) qs.set("n", String(params.n));
    if (params?.q) qs.set("q", params.q);
    if (params?.severity) qs.set("severity", params.severity);
    const suffix = qs.toString() ? `?${qs.toString()}` : "";
    return request<SystemLogsReply>(`/system/logs${suffix}`);
  },

  /** GET /api/updates/check — fetch latest GitHub release info. */
  checkForUpdates: () => request<UpdateCheckReply>("/updates/check"),

  /** GET /api/patterns/{group} — list agent-learned patterns. */
  listPatterns: (groupId: string, params?: { needs_attention?: boolean; status?: string; confidence_min?: number }) => {
    const qs = new URLSearchParams();
    if (params?.needs_attention) qs.set("needs_attention", "true");
    if (params?.status) qs.set("status", params.status);
    if (params?.confidence_min != null) qs.set("confidence_min", String(params.confidence_min));
    const suffix = qs.toString() ? `?${qs.toString()}` : "";
    return request<PatternsListReply>(`/patterns/${encodeURIComponent(groupId)}${suffix}`);
  },

  /** GET /api/patterns/{group}/{id} — single pattern detail. */
  getPattern: (groupId: string, patternId: string) =>
    request<PatternRow>(`/patterns/${encodeURIComponent(groupId)}/${encodeURIComponent(patternId)}`),

  /** DELETE /api/patterns/{group}/{id} — delete a pattern. */
  deletePattern: (groupId: string, patternId: string) =>
    request<PatternDeleteReply>(`/patterns/${encodeURIComponent(groupId)}/${encodeURIComponent(patternId)}`, { method: "DELETE" }),

  /** POST /api/patterns/{group}/gc — GC dry-run or execute. */
  runPatternGC: (groupId: string, dryRun = true) =>
    request<PatternGCReply>(`/patterns/${encodeURIComponent(groupId)}/gc`, {
      method: "POST",
      body: JSON.stringify({ dry_run: dryRun }),
    }),

  /** POST /api/patterns/{group}/export — export approved patterns to CLAUDE.md. */
  exportPatterns: (groupId: string, target: { file?: string; repo?: string }) =>
    request<{ exported: number; target: string }>(`/patterns/${encodeURIComponent(groupId)}/export`, {
      method: "POST",
      body: JSON.stringify(target),
    }),

  /**
   * POST /api/v2/maintenance/cleanup — preview/execute orphaned-registry cleanup (#1512).
   * dryRun:true (default) previews; false removes.
   */
  runCleanup: (dryRun = true) =>
    requestV2<CleanupReply>(`/maintenance/cleanup`, {
      method: "POST",
      body: JSON.stringify({ dry_run: dryRun }),
    }),

  /**
   * POST /api/v2/update/apply — run `grafel update` (#1512).
   * Subprocess-based, so the daemon is not replaced mid-request. The version
   * check stays on GET /api/updates/check (checkForUpdates above).
   */
  applyUpdate: () =>
    requestV2<UpdateApplyReply>(`/update/apply`, { method: "POST" }),

  /**
   * GET /api/quality/orphans/{group} — last PERSISTED orphan audit for a group.
   * Returns has_run=false (never-run, no real numbers) until runOrphanAudit is
   * called. Does NOT trigger the expensive audit.
   */
  getOrphanAudit: (groupId: string) =>
    request<OrphanAuditReply>(`/quality/orphans/${encodeURIComponent(groupId)}`),

  /** POST /api/quality/orphans/{group} — run the audit, persist + return it. */
  runOrphanAudit: (groupId: string) =>
    request<OrphanAuditReply>(`/quality/orphans/${encodeURIComponent(groupId)}`, {
      method: "POST",
    }),

  /** GET /api/quality/fixtures — list golden fixture names. */
  listQualityFixtures: () => request<FixturesReply>("/quality/fixtures"),

  /** POST /api/quality/recall — recall measurement against a fixture. */
  runRecall: (fixture: string, groupId?: string) =>
    request<RecallReply>("/quality/recall", {
      method: "POST",
      body: JSON.stringify({ fixture, group: groupId }),
    }),

  /**
   * GET /api/v2/groups/:g/refs — all indexed refs for every repo in the
   * group, with tier + source + indexedAt metadata (PH4 of #2087 / #2092).
   *
   * Response: { refs: Record<repoSlug, RefEntry[]> }
   * Refs are sorted server-side by tier (HOT → WARM → COLD → EXPIRED) then
   * alpha; the UI re-sorts client-side as well for resilience.
   */
  getRefs: (groupId: string) =>
    requestV2<GroupRefsResponse>(`/groups/${encodeURIComponent(groupId)}/refs`),

  /**
   * GET /api/v2/groups/:g/repos/:r/diff?refA=...&refB=...
   *
   * PH5 (#2093): returns the structural diff between two indexed git refs
   * for a single repo. Both refA and refB must be indexed on disk.
   */
  getDiff: (groupId: string, repo: string, refA: string, refB: string) =>
    requestV2<DiffResult>(
      `/groups/${encodeURIComponent(groupId)}/repos/${encodeURIComponent(repo)}/diff` +
        `?refA=${encodeURIComponent(refA)}&refB=${encodeURIComponent(refB)}`,
    ),

  // --- Daemon mode (S7a #2169) ---

  /**
   * GET /api/v2/daemon/mode — returns the currently configured daemon mode,
   * the env-var defaults it applies, and the full mode catalogue for the UI.
   */
  getDaemonMode: () => requestV2<DaemonModeReply>(`/daemon/mode`),

  /**
   * POST /api/v2/daemon/mode — writes the mode to daemon.config.json and
   * triggers a daemon restart. Equivalent to `grafel mode <m>` via CLI.
   */
  setDaemonMode: (newMode: string) =>
    requestV2<SetDaemonModeReply>(`/daemon/mode`, {
      method: "POST",
      body: JSON.stringify({ mode: newMode }),
    }),

  // --- Security / Auth-Coverage screen (#4250) ---
  // These call the v1 /api/security/* routes served by
  // internal/dashboard/handlers_security.go. Responses are raw JSON (no v2
  // envelope), so they go through `request`, not `requestV2`.

  /** GET /api/security/auth-coverage/{group} — HTTP endpoint auth coverage. */
  getAuthCoverage: (
    groupId: string,
    params?: { severity?: string; file?: string; only_uncovered?: boolean },
  ) => {
    const qs = new URLSearchParams();
    if (params?.severity) qs.set("severity", params.severity);
    if (params?.file) qs.set("file", params.file);
    if (params?.only_uncovered) qs.set("only_uncovered", "true");
    const suffix = qs.toString() ? `?${qs.toString()}` : "";
    return request<GroupAuthCoverageReport>(
      `/security/auth-coverage/${encodeURIComponent(groupId)}${suffix}`,
    );
  },

  /** GET /api/security/secrets/{group} — hardcoded secrets + SM integrations. */
  getSecrets: (groupId: string, params?: { severity?: string; file?: string }) => {
    const qs = new URLSearchParams();
    if (params?.severity) qs.set("severity", params.severity);
    if (params?.file) qs.set("file", params.file);
    const suffix = qs.toString() ? `?${qs.toString()}` : "";
    return request<GroupSecretsReport>(
      `/security/secrets/${encodeURIComponent(groupId)}${suffix}`,
    );
  },

  /** GET /api/security/cycles/{group} — import-cycle findings. */
  getSecurityCycles: (
    groupId: string,
    params?: { severity?: string; min_size?: number },
  ) => {
    const qs = new URLSearchParams();
    if (params?.severity) qs.set("severity", params.severity);
    if (params?.min_size != null) qs.set("min_size", String(params.min_size));
    const suffix = qs.toString() ? `?${qs.toString()}` : "";
    return request<GroupCyclesReport>(
      `/security/cycles/${encodeURIComponent(groupId)}${suffix}`,
    );
  },

  // --- Coverage / Quality screen (#4251) ---
  // These call capability routes the backend already serves but no screen
  // previously rendered. All raw JSON (no v2 envelope) → `request`, not
  // `requestV2`. Handlers: handlers_coverage.go, handlers_dependencies.go,
  // handlers_nplus1.go, handlers_graph.go (god-nodes), handlers_quality_trends.go.

  /** GET /api/quality/coverage/{group} — test-coverage report + uncovered drill-down. */
  getQualityCoverage: (
    groupId: string,
    params?: { dir?: string; module?: string; severity?: string; limit?: number },
  ) => {
    const qs = new URLSearchParams();
    if (params?.dir) qs.set("dir", params.dir);
    if (params?.module) qs.set("module", params.module);
    if (params?.severity) qs.set("severity", params.severity);
    if (params?.limit != null) qs.set("limit", String(params.limit));
    const suffix = qs.toString() ? `?${qs.toString()}` : "";
    return request<GroupCoverageReport>(
      `/quality/coverage/${encodeURIComponent(groupId)}${suffix}`,
    );
  },

  /** GET /api/dependencies/{group} — declared/used/unused/phantom per repo. */
  getDependencies: (
    groupId: string,
    params?: { status?: string; pm?: string; kind?: string },
  ) => {
    const qs = new URLSearchParams();
    if (params?.status) qs.set("status", params.status);
    if (params?.pm) qs.set("pm", params.pm);
    if (params?.kind) qs.set("kind", params.kind);
    const suffix = qs.toString() ? `?${qs.toString()}` : "";
    return request<DependenciesReply>(
      `/dependencies/${encodeURIComponent(groupId)}${suffix}`,
    );
  },

  /** GET /api/quality/anti-patterns/{group} — N+1 query findings. */
  getAntiPatterns: (
    groupId: string,
    params?: { orm?: string; file?: string },
  ) => {
    const qs = new URLSearchParams();
    if (params?.orm) qs.set("orm", params.orm);
    if (params?.file) qs.set("file", params.file);
    const suffix = qs.toString() ? `?${qs.toString()}` : "";
    return request<GroupNPlusOneReport>(
      `/quality/anti-patterns/${encodeURIComponent(groupId)}${suffix}`,
    );
  },

  /** GET /api/groups/{group}/god-nodes — high-degree structural hotspots. */
  getGodNodes: (groupId: string, params?: { limit?: number }) => {
    const qs = new URLSearchParams();
    if (params?.limit != null) qs.set("limit", String(params.limit));
    const suffix = qs.toString() ? `?${qs.toString()}` : "";
    return request<GodNodesReply>(
      `/groups/${encodeURIComponent(groupId)}/god-nodes${suffix}`,
    );
  },

  /** GET /api/quality/trends/{group} — per-metric quality time series. */
  getQualityTrends: (groupId: string, params?: { days?: number }) => {
    const qs = new URLSearchParams();
    if (params?.days != null) qs.set("days", String(params.days));
    const suffix = qs.toString() ? `?${qs.toString()}` : "";
    return request<QualityTrendsReply>(
      `/quality/trends/${encodeURIComponent(groupId)}${suffix}`,
    );
  },

  // --- Cross-repo links map (#4253) ---
  // Raw JSON (no v2 envelope) → `request`. Handler: handlers_graph.go
  // handleGroupLinks, which returns { links: CrossRepoLink[] }.

  /** GET /api/groups/{group}/links — resolved cross-repo link records. */
  getGroupLinks: (groupId: string) =>
    request<GroupLinksReply>(
      `/groups/${encodeURIComponent(groupId)}/links`,
    ),

  // --- GraphQL resolver-effects (#4255) ---
  // Raw JSON (no v2 envelope) → `request`. Handler: handlers_graphql.go
  // handleGraphQL, which returns a GraphQLReport (resolvers grouped by type).

  /** GET /api/graphql/{group} — GraphQL resolvers, effects, auth, schema types. */
  getGraphQL: (groupId: string) =>
    request<GraphQLReport>(
      `/graphql/${encodeURIComponent(groupId)}`,
    ),

  // --- IaC / Infrastructure (#4256) ---
  // Raw JSON (no v2 envelope) → `request`. Handler: handlers_iac.go handleIaC,
  // which returns an IaCReport (resources grouped by iac_tool).

  /** GET /api/iac/{group} — IaC resources grouped by tool, with props + relations. */
  getIaC: (groupId: string) =>
    request<IaCReport>(
      `/iac/${encodeURIComponent(groupId)}`,
    ),

  // --- Data-flow & Taint (#4265) ---
  // Raw JSON (no v2 envelope) → `request`. Handler: handlers_dataflow.go
  // handleDataflow, which returns a DataflowReport (ranked taint findings +
  // request-input → sink DATA_FLOWS_TO edges).

  /** GET /api/dataflow/{group} — taint flows + ranked source→sink findings. */
  getDataflow: (groupId: string) =>
    request<DataflowReport>(
      `/dataflow/${encodeURIComponent(groupId)}`,
    ),

  // --- Dependency-Injection (#4266) ---
  // Raw JSON (no v2 envelope) → `request`. Handler: handlers_di.go handleDI,
  // which returns a DIReport (providers grouped by framework, each listing the
  // consumers it is INJECTED_INTO).

  /** GET /api/di/{group} — DI providers → consumers, grouped by framework. */
  getDI: (groupId: string) =>
    request<DIReport>(
      `/di/${encodeURIComponent(groupId)}`,
    ),

  // --- Error-flow (#4267) ---
  // Raw JSON (no v2 envelope) → `request`. Handler: handlers_errorflow.go
  // handleErrorFlow, which rolls up SCOPE.ExceptionType nodes across the group
  // with their THROWS (throwers) and CATCHES (catchers) and an honest uncaught
  // flag (thrown-but-no-typed-catcher-in-graph).

  /** GET /api/errorflow/{group} — exception types → throwers + catchers, uncaught flag. */
  getErrorFlow: (groupId: string) =>
    request<ErrorFlowReport>(
      `/errorflow/${encodeURIComponent(groupId)}`,
    ),

  // --- Index quarantine transparency (Q2, #5617) ---
  // The self-healing watcher auto-quarantines dirs that churn pathologically.
  // These surface that set and let an operator override it.

  /** GET /api/groups/{group}/quarantine — quarantined dirs across the group's repos. */
  getQuarantine: (groupId: string) =>
    request<QuarantineListReply>(
      `/groups/${encodeURIComponent(groupId)}/quarantine`,
    ),

  /** POST .../quarantine/unquarantine — manual un-quarantine of a directory. */
  unquarantine: (groupId: string, repo: string, rel: string) =>
    request<QuarantineActionReply>(
      `/groups/${encodeURIComponent(groupId)}/repos/${encodeURIComponent(repo)}/quarantine/unquarantine`,
      { method: "POST", body: JSON.stringify({ rel }) },
    ),

  /** POST .../quarantine/pin — pin (never auto-heal) or unpin a directory. */
  pinQuarantine: (groupId: string, repo: string, rel: string, pinned: boolean) =>
    request<QuarantineActionReply>(
      `/groups/${encodeURIComponent(groupId)}/repos/${encodeURIComponent(repo)}/quarantine/pin`,
      { method: "POST", body: JSON.stringify({ rel, pinned }) },
    ),
};

export type Api = typeof api;
