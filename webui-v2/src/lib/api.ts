/* ============================================================
   lib/api.ts — typed archigraph daemon client.

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
  DocsTreeNode,
  DocsEntityDetail,
  GraphPayloadWire,
  EntityDetailWire,
  SettingsGroup,
  SettingsFeatures,
  DoctorCheck,
  TopologyResponse,
  TopologyChannelDetail,
  OrphanPublisherEntry,
  OrphanSubscriberEntry,
  V2CandidatesResponse,
  FlowsListResponse,
  FlowDetailResponse,
  FlowDeadEndsResponse,
  PathsListResponse,
  PathDetail,
  OrphansResponse,
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
  UpdateApplyReply,
  ScanInspectReply,
  WizardRepo,
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
  /** v2 — daemon bootstrap: version, supported surfaces, group slugs. */
  getMeta: () => requestV2<Meta>("/meta"),
  /** v2 — rich group list for the Landing screen. */
  listGroups: () => requestV2<Group[]>("/groups"),
  /** v2 — create an empty group from a name (Landing wizard). */
  createGroup: (name: string) =>
    requestV2<Group>("/groups", { method: "POST", body: JSON.stringify({ name }) }),

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
   * POST /api/v2/groups/from-scan — create a group, register the scanned repos,
   * and enqueue an async index job. Returns a 202 JobAck the wizard streams.
   */
  createGroupFromScan: (name: string, repos: WizardRepo[]) =>
    requestV2<JobAck>("/groups/from-scan", {
      method: "POST",
      body: JSON.stringify({ name, repos }),
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

  // --- v2 Docs entity browser (#1438) ---
  getDocsTree: (groupId: string) =>
    requestV2<DocsTreeNode[]>(`/groups/${groupId}/docs/tree`),
  getDocsEntity: (groupId: string, entityId: string) =>
    requestV2<DocsEntityDetail>(`/groups/${groupId}/docs/entities/${encodeURIComponent(entityId)}`),
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
   * POST /api/v2/update/apply — run `archigraph update` (#1512).
   * Subprocess-based, so the daemon is not replaced mid-request. The version
   * check stays on GET /api/updates/check (checkForUpdates above).
   */
  applyUpdate: () =>
    requestV2<UpdateApplyReply>(`/update/apply`, { method: "POST" }),

  /** GET /api/quality/orphans/{group} — orphan audit for a group. */
  getOrphanAudit: (groupId: string) =>
    request<OrphanAuditReply>(`/quality/orphans/${encodeURIComponent(groupId)}`),

  /** GET /api/quality/fixtures — list golden fixture names. */
  listQualityFixtures: () => request<FixturesReply>("/quality/fixtures"),

  /** POST /api/quality/recall — recall measurement against a fixture. */
  runRecall: (fixture: string, groupId?: string) =>
    request<RecallReply>("/quality/recall", {
      method: "POST",
      body: JSON.stringify({ fixture, group: groupId }),
    }),
};

export type Api = typeof api;
