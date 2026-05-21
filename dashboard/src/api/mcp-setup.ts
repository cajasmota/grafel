/**
 * MCP Setup Wizard API — issue #1247
 *
 * GET  /api/mcp-setup/hosts             — detect installed hosts + config state
 * POST /api/mcp-setup/install?host=X    — idempotent install
 * POST /api/mcp-setup/uninstall?host=X  — remove archigraph entry
 * POST /api/mcp-setup/verify?host=X     — test-query the MCP server
 */

// ── Wire types ────────────────────────────────────────────────────────────────

export type MCPHostID = 'claude' | 'cursor' | 'windsurf'

export type MCPInstallState =
  | 'installed'     // entry present and well-formed
  | 'partial'       // entry present but malformed
  | 'not_installed' // no entry found
  | 'host_absent'   // config file not found (host may not be installed)

export interface MCPHostInfo {
  id: MCPHostID
  label: string
  config_path: string
  exists: boolean
  state: MCPInstallState
  current_args?: string[]
  error?: string
}

export interface MCPHostsReply {
  hosts: MCPHostInfo[]
  mcp_port: number
  server_arg: string
}

export interface MCPActionReply {
  ok: boolean
  message: string
  latency_ms?: number
}

// ── Fetch helpers ─────────────────────────────────────────────────────────────

class ApiError extends Error {
  constructor(
    public readonly status: number,
    message: string,
  ) {
    super(message)
  }
}

async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json', ...init?.headers },
    ...init,
  })
  if (!res.ok) {
    const body = await res.text()
    throw new ApiError(res.status, `API ${res.status} ${path}: ${body}`)
  }
  return res.json() as Promise<T>
}

// ── Public API ────────────────────────────────────────────────────────────────

export async function fetchMCPHosts(): Promise<MCPHostsReply> {
  return apiFetch<MCPHostsReply>('/api/mcp-setup/hosts')
}

export async function installMCPHost(host: MCPHostID): Promise<MCPActionReply> {
  return apiFetch<MCPActionReply>(`/api/mcp-setup/install?host=${host}`, {
    method: 'POST',
  })
}

export async function uninstallMCPHost(host: MCPHostID): Promise<MCPActionReply> {
  return apiFetch<MCPActionReply>(`/api/mcp-setup/uninstall?host=${host}`, {
    method: 'POST',
  })
}

export async function verifyMCPHost(host: MCPHostID): Promise<MCPActionReply> {
  return apiFetch<MCPActionReply>(`/api/mcp-setup/verify?host=${host}`, {
    method: 'POST',
  })
}
