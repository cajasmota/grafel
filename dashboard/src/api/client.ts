/**
 * Fetch wrapper for Archigraph REST API.
 *
 * Set VITE_USE_MOCKS=true in .env to load from src/api/mocks/*.json instead
 * of hitting the live Go server. The mock switch is compile-time (import.meta.env)
 * so the mock modules are tree-shaken in production builds.
 */

const USE_MOCKS = import.meta.env.VITE_USE_MOCKS === 'true' || import.meta.env.DEV

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
    graph: () => import('./mocks/graph.json'),
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
} from '@/types/api'
import type { DocTreeResponse, DocContentResponse, DocSearchResponse, EntityCard } from '@/types/docs'

// ── Registry ────────────────────────────────────────────────────────────────

export async function fetchRegistry(): Promise<Registry> {
  if (USE_MOCKS) return loadMock<Registry>('registry')
  return apiFetch<Registry>('/api/registry')
}

// ── Surface 4: Paths ─────────────────────────────────────────────────────────

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
  return apiFetch<PathListResponse>(`/api/paths/${group}?${params}`)
}

export async function fetchPathDetail(
  group: string,
  pathHash: string,
): Promise<PathDetailResponse> {
  if (USE_MOCKS) return loadMock<PathDetailResponse>('path-detail')
  return apiFetch<PathDetailResponse>(`/api/paths/${group}/${pathHash}`)
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

export async function fetchGraph(
  group: string,
  filters: GraphFilters = {},
): Promise<GraphResponse> {
  if (USE_MOCKS) {
    const data = await loadMock<GraphResponse>('graph')
    return applyGraphMockFilters(data, filters)
  }
  const params = buildParams({ lod: filters.lod, repo: filters.repo })
  return apiFetch<GraphResponse>(`/api/graph/${group}?${params}`)
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
  return apiFetch<EntityNeighborResponse>(`/api/graph/${group}/entity/${encodeURIComponent(entityId)}`)
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
    topics: protocols.has('kafka') ? data.topics : [],
    queues: data.queues.filter((q) => protocols.has(q.broker as TopologyProtocol)),
    channels: data.channels.filter((c) => protocols.has(c.channel_type as TopologyProtocol)),
    graphql_subscriptions: protocols.has('graphql_subscription') ? data.graphql_subscriptions : [],
    nats_subjects: protocols.has('nats') ? data.nats_subjects : [],
    transforms: data.transforms,
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

  const total = paths.length
  const page = filters.page ?? 1
  const pageSize = filters.page_size ?? 50
  const start = (page - 1) * pageSize
  const pageItems = paths.slice(start, start + pageSize)

  return {
    ...data,
    paths: pageItems,
    total,
    has_more: start + pageSize < total,
    page,
    page_size: pageSize,
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
