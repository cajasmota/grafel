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
} from '@/types/api'

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
