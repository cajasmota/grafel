/**
 * Fetch wrapper for Archigraph REST API.
 *
 * Set VITE_USE_MOCKS=true in .env to load from src/api/mocks/*.json instead
 * of hitting the live Go server. The mock switch is compile-time (import.meta.env)
 * so the mock modules are tree-shaken in production builds.
 *
 * When VITE_USE_MOCKS is absent or set to anything other than 'true', all
 * calls are proxied to the archigraph dashboard server (see vite.config.ts
 * proxy and VITE_API_PORT).
 */

const USE_MOCKS = import.meta.env.VITE_USE_MOCKS === 'true'

// ────────────────────────────────────────────────────────────────────────────
// HTTP fetch wrapper
// ────────────────────────────────────────────────────────────────────────────

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly body: string,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const url = path.startsWith('http') ? path : path
  const res = await fetch(url, {
    headers: { 'Content-Type': 'application/json', ...init?.headers },
    ...init,
  })
  if (!res.ok) {
    const body = await res.text()
    throw new ApiError(res.status, body, `API ${res.status}: ${path}`)
  }
  return res.json() as Promise<T>
}

// ────────────────────────────────────────────────────────────────────────────
// Mock loader
// ────────────────────────────────────────────────────────────────────────────

type MockModule = Record<string, unknown>

async function loadMock<T>(name: string): Promise<T> {
  // Dynamic imports so production bundle doesn't carry mock JSON
  const mocks: Record<string, () => Promise<MockModule>> = {
    registry: () => import('./mocks/registry.json'),
    paths: () => import('./mocks/paths.json'),
    'path-detail': () => import('./mocks/path-detail.json'),
    flows: () => import('./mocks/flows.json'),
    graph: () => import('./mocks/graph-mock.json'),
    topology: () => import('./mocks/topology.json'),
    'docs-tree': () => import('./mocks/docs-tree.json'),
    'docs/acme-web/overview': () => import('./mocks/docs/acme-web-overview.json'),
    'docs/acme-web/modules/orders/everything': () => import('./mocks/docs/acme-web-orders-everything.json'),
  }
  const loader = mocks[name]
  if (!loader) throw new Error(`No mock registered for "${name}"`)
  const mod = await loader()
  // Vite adds a `default` key for JSON imports
  return (mod.default ?? mod) as T
}

// ────────────────────────────────────────────────────────────────────────────
// Typed API calls — each surface has its own section
// ────────────────────────────────────────────────────────────────────────────

import type {
  Registry,
  PathListResponse,
  PathDetailResponse,
  PathFilters,
  FlowListResponse,
  FlowDetailResponse,
  FlowFilters,
  TopologyResponse,
  TopologyFilters,
  TopologyProtocol,
  GraphResponse,
  GraphFilters,
  EntityNeighborResponse,
  PendingRepairsResponse,
  PendingEnrichmentsResponse,
  OrphanCallersResponse,
} from '@/types/api'
import type { DocTreeResponse, DocContentResponse, DocSearchResponse, EntityCard } from '@/types/docs'

// ── Registry ────────────────────────────────────────────────────────────────

/**
 * Wire-format group from GET /api/registry:
 *   { name, config_path, repos: string[], entity_count, last_indexed? }
 *
 * Frontend GroupMeta expects:
 *   { id, display_name, repos: RepoMeta[], entity_count, indexed_at? }
 *
 * normalizeRegistry maps the wire format to the frontend type so the SPA
 * group-selector and routing work without modification.
 */
type WireGroup = {
  name: string
  config_path?: string
  repos?: string[]
  entity_count?: number
  last_indexed?: string
  frameworks?: string[]
}

function normalizeRegistry(raw: { groups: Array<WireGroup> }): Registry {
  const groups = (raw.groups ?? []).map((g) => ({
    id: g.name,
    display_name: g.name,
    repos: (g.repos ?? []).map((slug) => ({
      slug,
      display_name: slug,
      language: 'unknown',
      entity_count: 0,
    })),
    entity_count: g.entity_count ?? 0,
    indexed_at: g.last_indexed,
    frameworks: g.frameworks,
  }))
  return { groups, version: '1' }
}

export async function fetchRegistry(): Promise<Registry> {
  if (USE_MOCKS) return loadMock<Registry>('registry')
  const raw = await apiFetch<{ groups: Array<WireGroup> }>('/api/registry')
  return normalizeRegistry(raw)
}

// ── Surface 4: Paths ─────────────────────────────────────────────────────────

/**
 * Wire tree node shape from the backend (handlers_paths.go buildPrefixTree).
 * Different from the frontend PathTreeNode — normalize before returning.
 */
type WireTreeNode = {
  segment?: string
  path?: string
  has_paths?: boolean
  children?: WireTreeNode[]
  // Frontend-style fields (from mock data)
  prefix?: string
  label?: string
  count?: number
}

function normalizeTreeNode(n: WireTreeNode): PathTreeNode {
  return {
    prefix: n.prefix ?? n.path ?? '',
    label: n.label ?? n.segment ?? '',
    count: n.count ?? 0,
    children: (n.children ?? []).map(normalizeTreeNode),
  }
}

function normalizePathListResponse(raw: PathListResponse): PathListResponse {
  return {
    ...raw,
    paths: (raw.paths ?? []).map((p) => ({
      ...p,
      verbs: p.verbs ?? [],
      handlers: (p as unknown as { handlers?: unknown[] }).handlers as never ?? [],
      repos: p.repos ?? [],
      frameworks: p.frameworks ?? [],
    })),
    tree: (raw.tree ?? []).map((n) => normalizeTreeNode(n as unknown as WireTreeNode)),
  }
}

export async function fetchPaths(
  group: string,
  filters: PathFilters = {},
): Promise<PathListResponse> {
  if (USE_MOCKS) {
    // Simulate server-side filtering on the mock data
    const data = await loadMock<PathListResponse>('paths')
    return applyMockFilters(data, filters)
  }
  const params = buildParams(filters)
  const raw = await apiFetch<PathListResponse>(`/api/paths/${group}?${params}`)
  return normalizePathListResponse(raw)
}

export async function fetchPathDetail(
  group: string,
  pathHash: string,
): Promise<PathDetailResponse> {
  if (USE_MOCKS) return loadMock<PathDetailResponse>('path-detail')
  return apiFetch<PathDetailResponse>(`/api/paths/${group}/${pathHash}`)
}

/**
 * GET /api/paths/{group}/orphan-callers
 *
 * Returns frontend FETCH call sites that have no matching backend handler.
 * Backend (#1091) may not be deployed yet — gracefully returns empty on 404.
 */
export async function fetchOrphanCallers(
  group: string,
): Promise<OrphanCallersResponse> {
  if (USE_MOCKS) return { callers: [], total: 0 }
  try {
    return await apiFetch<OrphanCallersResponse>(`/api/paths/${group}/orphan-callers`)
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      // Backend #1091 not yet deployed — return empty gracefully
      console.info('[paths] orphan-callers endpoint not yet available (404) — backend #1091 pending')
      return { callers: [], total: 0 }
    }
    throw err
  }
}

// ── Surface 2: Flows ─────────────────────────────────────────────────────────

export async function fetchFlows(
  group: string,
  filters: FlowFilters = {},
): Promise<FlowListResponse> {
  if (USE_MOCKS) {
    const data = await loadMock<FlowListResponse>('flows')
    return applyFlowMockFilters(data, filters)
  }
  const params = buildParams(filters as Record<string, unknown>)
  return apiFetch<FlowListResponse>(`/api/flows/${group}?${params}`)
}

export async function fetchFlowDetail(
  group: string,
  processId: string,
): Promise<FlowDetailResponse> {
  if (USE_MOCKS) {
    const data = await loadMock<FlowListResponse>('flows')
    const process = data.processes.find((p) => p.process_id === processId)
    if (!process) throw new Error(`Mock: no flow with id "${processId}"`)
    return {
      process,
      chain_entities: [],
      source_snippets: {},
    }
  }
  return apiFetch<FlowDetailResponse>(`/api/flows/${group}/${processId}`)
}

// ── Surface 1: Graph ─────────────────────────────────────────────────────────

/**
 * Wire-format edge from the Go server: { from_id, to_id, kind, cross_repo? }
 * The frontend type (GraphEdge) uses { id, source, target, kind, cross_repo? }.
 * normalizeEdge converts from the wire format to the frontend type.
 */
function normalizeEdge(raw: { from_id: string; to_id: string; kind: string; cross_repo?: boolean }): import('@/types/api').GraphEdge {
  return {
    id: `${raw.from_id}::${raw.to_id}::${raw.kind}`,
    source: raw.from_id,
    target: raw.to_id,
    kind: raw.kind as import('@/types/api').RelationshipKind,
    cross_repo: raw.cross_repo ?? false,
  }
}

/**
 * Normalise a raw /api/graph response to the GraphResponse shape the
 * frontend types expect.  The Go server uses:
 *   - edges[].from_id / to_id  →  edges[].source / target (+ synthetic id)
 *   - total_node_count (or total_nodes)  →  total_node_count
 *
 * #1023: lod field removed — server always returns dense tier.
 */
/**
 * Strip non-primitive fields from a raw node before passing it to Cosmograph.
 *
 * Cosmograph uses DuckDB-WASM for columnar storage and requires every column
 * to be a primitive (string | number | boolean | null).  The `properties`
 * field is a nested Record that causes a `TypeError: Cannot read properties
 * of undefined (reading 'constructor')` during DuckDB type inference (#1032).
 *
 * We keep only the fields declared on GraphNode and known to be primitives.
 */
function normalizeGraphNode(raw: Record<string, unknown>): import('@/types/api').GraphNode {
  return {
    id:           String(raw.id ?? ''),
    label:        String(raw.label ?? raw.id ?? ''),
    kind:         raw.kind as import('@/types/api').EntityKind,
    repo:         String(raw.repo ?? ''),
    community_id: raw.community_id as number | undefined,
    pagerank:     raw.pagerank as number | undefined,
    is_centroid:  raw.is_centroid as boolean | undefined,
    centroid_size: raw.centroid_size as number | undefined,
    source_file:  raw.source_file != null ? String(raw.source_file) : undefined,
    start_line:   raw.start_line as number | undefined,
    // degree: total in+out edge count, emitted by the server for Cosmograph's
    // pointSizeStrategy="degree" so hub nodes appear larger than leaf nodes.
    degree:       raw.degree as number | undefined,
    // `properties` intentionally omitted — nested object breaks DuckDB-WASM
    // columnar ingestion.  Entity detail is fetched separately via /api/graph/{group}/entity/{id}.
  }
}

function normalizeGraphResponse(raw: Record<string, unknown>): GraphResponse {
  type RawEdge = { from_id: string; to_id: string; kind: string; cross_repo?: boolean }
  const rawEdges = (raw.edges as RawEdge[] | undefined) ?? []
  const rawNodes = (raw.nodes as Record<string, unknown>[] | undefined) ?? []
  return {
    nodes: rawNodes.map(normalizeGraphNode),
    edges: rawEdges.map(normalizeEdge),
    communities: (raw.communities as GraphResponse['communities'] | undefined) ?? [],
    lod: 'zoom-in',  // compat: always 'zoom-in' (dense) — no LoD switching
    total_node_count: (raw.total_node_count ?? raw.total_nodes ?? 0) as number,
  }
}

export async function fetchGraph(
  group: string,
  filters: GraphFilters = {},
): Promise<GraphResponse> {
  if (USE_MOCKS) {
    const data = await loadMock<GraphResponse>('graph')
    return applyGraphMockFilters(data, filters)
  }
  const params = buildParams({
    repo: filters.repo,
    repos: (filters as { repos?: string }).repos,
    include_external: filters.include_external === true ? 'true' : undefined,
  })
  const raw = await apiFetch<Record<string, unknown>>(`/api/graph/${group}?${params}`)
  return normalizeGraphResponse(raw)
}

export async function fetchEntityNeighbors(
  group: string,
  entityId: string,
): Promise<EntityNeighborResponse> {
  if (USE_MOCKS) {
    const data = await loadMock<GraphResponse>('graph')
    const entity = data.nodes.find((n) => n.id === entityId)
    if (!entity) throw new Error(`Mock: no entity with id "${entityId}"`)
    const outbound = data.edges
      .filter((e) => e.source === entityId)
      .map((e) => ({ edge: e, node: data.nodes.find((n) => n.id === e.target)! }))
      .filter((x) => x.node != null)
    const inbound = data.edges
      .filter((e) => e.target === entityId)
      .map((e) => ({ edge: e, node: data.nodes.find((n) => n.id === e.source)! }))
      .filter((x) => x.node != null)
    return {
      entity: {
        id: entity.id,
        label: entity.label,
        qualified_name: entity.label,
        kind: entity.kind,
        source_file: entity.source_file ?? '',
        start_line: entity.start_line ?? 0,
        end_line: entity.start_line ?? 0,
        language: 'typescript',
        repo: entity.repo,
        pagerank: entity.pagerank,
        community_id: entity.community_id,
        properties: entity.properties,
      },
      outbound,
      inbound,
    }
  }
  // Server returns { entity, inbound_edges, outbound_edges, neighbors }
  // where edges are wire-format { from_id, to_id, kind }.
  // Normalise to the EntityNeighborResponse shape the frontend expects.
  type RawEdge = { from_id: string; to_id: string; kind: string; cross_repo?: boolean }
  type RawNeighbor = { id: string; label: string; kind: string; source_file: string; start_line: number; repo: string }
  const raw = await apiFetch<{
    entity: import('@/types/api').Entity
    inbound_edges: RawEdge[]
    outbound_edges: RawEdge[]
    neighbors: RawNeighbor[]
  }>(`/api/graph/${group}/entity/${encodeURIComponent(entityId)}`)

  const neighborMap = new Map((raw.neighbors ?? []).map((n) => [n.id, n]))

  const toEdgeNode = (re: RawEdge, peerId: string) => {
    const edge = normalizeEdge(re)
    const peer = neighborMap.get(peerId)
    if (!peer) return null
    const node: import('@/types/api').GraphNode = {
      id: peer.id,
      label: peer.label,
      kind: peer.kind as import('@/types/api').EntityKind,
      source_file: peer.source_file,
      start_line: peer.start_line,
      repo: peer.repo,
    }
    return { edge, node }
  }

  const outbound = (raw.outbound_edges ?? [])
    .map((re) => toEdgeNode(re, re.to_id))
    .filter((x): x is NonNullable<typeof x> => x !== null)
  const inbound = (raw.inbound_edges ?? [])
    .map((re) => toEdgeNode(re, re.from_id))
    .filter((x): x is NonNullable<typeof x> => x !== null)

  return { entity: raw.entity, outbound, inbound }
}

// ── Surface 3: Topology ───────────────────────────────────────────────────────

export async function fetchTopology(
  group: string,
  filters: TopologyFilters = {},
): Promise<TopologyResponse> {
  if (USE_MOCKS) {
    const data = await loadMock<TopologyResponse>('topology')
    return applyTopologyMockFilters(data, filters)
  }
  const params = buildParams(filters as Record<string, unknown>)
  return apiFetch<TopologyResponse>(`/api/topology/${group}?${params}`)
}

// ────────────────────────────────────────────────────────────────────────────
// Helpers
// ────────────────────────────────────────────────────────────────────────────

function applyFlowMockFilters(
  data: FlowListResponse,
  filters: FlowFilters,
): FlowListResponse {
  let processes = [...data.processes]

  if (filters.entry) {
    processes = processes.filter(
      (p) => p.entry_id === filters.entry || p.entry_name.toLowerCase().includes(filters.entry!.toLowerCase()),
    )
  }
  if (filters.cross_stack_only) {
    processes = processes.filter((p) => p.cross_stack)
  }
  if (filters.repo) {
    processes = processes.filter((p) => p.repo === filters.repo)
  }

  const limit = filters.limit ?? 50
  const total = processes.length
  const pageItems = processes.slice(0, limit)

  return {
    ...data,
    processes: pageItems,
    total,
    has_more: total > limit,
  }
}

function applyGraphMockFilters(
  data: GraphResponse,
  filters: GraphFilters,
): GraphResponse {
  let nodes = [...data.nodes]
  let edges = [...data.edges]

  if (filters.repo) {
    nodes = nodes.filter((n) => n.repo === filters.repo)
    const nodeIds = new Set(nodes.map((n) => n.id))
    edges = edges.filter((e) => nodeIds.has(e.source) && nodeIds.has(e.target))
  }

  if (filters.edge_kinds && filters.edge_kinds.length > 0) {
    const kinds = new Set(filters.edge_kinds)
    edges = edges.filter((e) => kinds.has(e.kind))
  }

  return { ...data, nodes, edges }
}

function applyTopologyMockFilters(
  data: TopologyResponse,
  filters: TopologyFilters,
): TopologyResponse {
  if (!filters.protocols || filters.protocols.length === 0) return data

  const protocols = new Set(filters.protocols)

  return {
    ...data,
    topics: protocols.has('kafka') ? (data.topics ?? []) : [],
    queues: (data.queues ?? []).filter((q) => {
      const proto = (q.id?.startsWith('stream:redis:') ? 'redis-stream'
        : q.id?.startsWith('task:') ? 'task-queue'
        : q.broker) as TopologyProtocol
      return protocols.has(proto) || protocols.has(q.broker as TopologyProtocol)
    }),
    channels: (data.channels ?? []).filter((c) => protocols.has(c.channel_type as TopologyProtocol)),
    graphql_subscriptions: protocols.has('graphql_subscription') ? (data.graphql_subscriptions ?? []) : [],
    nats_subjects: protocols.has('nats') ? (data.nats_subjects ?? []) : [],
    transforms: data.transforms ?? [],
    functions: (data.functions ?? []).filter(() => protocols.has('serverless')),
  }
}

function buildParams(obj: Record<string, unknown>): string {
  const params = new URLSearchParams()
  for (const [k, v] of Object.entries(obj)) {
    if (v !== undefined && v !== null && v !== '') {
      params.set(k, String(v))
    }
  }
  return params.toString()
}

function applyMockFilters(
  data: PathListResponse,
  filters: PathFilters,
): PathListResponse {
  let paths = [...data.paths]

  if (filters.q) {
    const q = filters.q.toLowerCase()
    paths = paths.filter((p) => p.path.toLowerCase().includes(q))
  }
  if (filters.prefix) {
    paths = paths.filter((p) => p.path.startsWith(filters.prefix!))
  }
  if (filters.repo) {
    paths = paths.filter((p) => p.repos.includes(filters.repo!))
  }
  if (filters.framework) {
    paths = paths.filter((p) => p.frameworks.includes(filters.framework!))
  }
  if (filters.is_webhook !== undefined) {
    paths = paths.filter((p) => p.is_webhook === filters.is_webhook)
  }

  return {
    ...data,
    paths,
    total: paths.length,
    has_more: false,
  }
}

// ── Surface 5: Docs ───────────────────────────────────────────────────────────

/** Fetch the navigation tree for a group's documentation */
export async function fetchDocTree(group: string): Promise<DocTreeResponse> {
  if (USE_MOCKS) return loadMock<DocTreeResponse>('docs-tree')
  return apiFetch<DocTreeResponse>(`/api/docs/${group}`)
}

/**
 * Fetch a specific doc page.
 * Passes include=hovercards so the server can pre-resolve entity symbols.
 */
export async function fetchDocContent(group: string, docPath: string): Promise<DocContentResponse> {
  if (USE_MOCKS) {
    const key = `docs/${group}/${docPath}`
    try {
      return await loadMock<DocContentResponse>(key)
    } catch {
      // Fall back to overview if the specific doc isn't mocked
      const fallback = await loadMock<DocContentResponse>(`docs/acme-web/overview`)
      return {
        ...fallback,
        path: docPath,
        title: docPath.split('/').pop() ?? 'Untitled',
      }
    }
  }
  return apiFetch<DocContentResponse>(`/api/docs/${group}/${docPath}?include=hovercards`)
}

/** Search docs within a group */
export async function fetchDocsSearch(group: string, query: string): Promise<DocSearchResponse> {
  if (USE_MOCKS) {
    // Minimal mock: filter from tree labels
    const tree = await loadMock<DocTreeResponse>('docs-tree')
    const flatten = (nodes: DocTreeResponse['tree']): Array<{ path: string; title: string }> =>
      nodes.flatMap((n) => {
        if (n.type === 'file') return [{ path: n.path, title: n.title ?? n.label }]
        return flatten(n.children)
      })
    const all = flatten(tree.tree)
    const q = query.toLowerCase()
    const results = all
      .filter((f) => f.title.toLowerCase().includes(q) || f.path.toLowerCase().includes(q))
      .map((f) => ({
        path: f.path,
        title: f.title,
        excerpt: `…${query}…`,
        score: 1.0,
      }))
    return { results, query, total: results.length }
  }
  const params = new URLSearchParams({ q: query, type: 'docs' })
  return apiFetch<DocSearchResponse>(`/api/search/${group}?${params}`)
}

// ── Build / version info ──────────────────────────────────────────────────────

/** Wire shape of GET /api/info */
export interface DaemonInfo {
  version: string
  commit: string
  built_at: string
  daemon_started_at?: string
  dashboard_port?: number
}

/** Mock info returned when VITE_USE_MOCKS=true */
const MOCK_INFO: DaemonInfo = {
  version: '0.0.0-dev',
  commit: 'abc1234567890',
  built_at: new Date().toISOString(),
  daemon_started_at: new Date(Date.now() - 83 * 60 * 1000).toISOString(), // 1h 23m ago
  dashboard_port: 47274,
}

export async function fetchInfo(): Promise<DaemonInfo> {
  if (USE_MOCKS) return MOCK_INFO
  return apiFetch<DaemonInfo>('/api/info')
}

/** Fetch minimal entity metadata for a hovercard */
export async function fetchEntityHovercard(entityId: string): Promise<EntityCard> {
  if (USE_MOCKS) {
    return {
      id: entityId,
      label: entityId.replace('entity-', '').replace(/-/g, ''),
      kind: 'Class',
      source_file: 'mock/file.py',
      start_line: 1,
      outbound_edges: [],
    }
  }
  return apiFetch<EntityCard>(`/api/inspect?id=${encodeURIComponent(entityId)}&compact=true`)
}

// ── Surface 6: Pending queue (repairs + enrichments) ─────────────────────────

/** GET /api/repairs/{group} — repair_edge and dynamic_baseurl_endpoint candidates */
export async function fetchRepairs(group: string): Promise<PendingRepairsResponse> {
  if (USE_MOCKS) {
    return { items: [], total: 0, auto_resolvable_count: 0 }
  }
  return apiFetch<PendingRepairsResponse>(`/api/repairs/${group}`)
}

/** GET /api/enrichments/{group} — all non-repair enrichment candidates */
export async function fetchEnrichments(group: string): Promise<PendingEnrichmentsResponse> {
  if (USE_MOCKS) {
    return { items: [], total: 0 }
  }
  return apiFetch<PendingEnrichmentsResponse>(`/api/enrichments/${group}`)
}

/**
 * POST a resolution (apply) or rejection for a single candidate.
 * The daemon MCP RPC handles the actual write; this just POSTs to the
 * same daemon HTTP endpoint the other surfaces use.
 *
 * action = 'apply'  → adds a resolution record
 * action = 'reject' → adds a rejection record
 */
export async function postCandidateAction(
  group: string,
  candidateId: string,
  action: 'apply' | 'reject',
  value?: string,
): Promise<void> {
  if (USE_MOCKS) return
  await apiFetch<unknown>(`/api/enrichments/${group}/action`, {
    method: 'POST',
    body: JSON.stringify({ candidate_id: candidateId, action, value }),
  })
}
