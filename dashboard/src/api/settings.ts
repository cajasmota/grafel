/**
 * Settings surface API — GET/PUT /api/settings, POST /api/settings/reset
 *
 * All preferences are owned by ~/.archigraph/settings.json on the daemon side.
 * The frontend reads them once on mount, auto-saves on change (debounced),
 * and exposes a reset-to-defaults action.
 */

// ── Wire types ────────────────────────────────────────────────────────────────

export interface AppSettings {
  // General
  theme: 'light' | 'dark' | 'auto'
  default_group: string

  // Updates
  auto_check_updates: boolean
  update_channel: 'stable' | 'dev'
  refresh_schedule: string

  // Telemetry
  telemetry_enabled: boolean

  // Performance (restart required on change)
  daemon_rss_budget_mb: number   // 100–2000
  watcher_debounce_secs: number  // 1–60
  indexer_parallelism: number    // 1–32

  // Logs
  log_level: 'debug' | 'info' | 'warn' | 'error'
}

export interface SettingsReply {
  settings: AppSettings
  defaults: AppSettings
  restart_required?: string[]
}

// ── MCP config snippets ───────────────────────────────────────────────────────

/** Returns a ready-to-paste MCP config block for the given tool. */
export function mcpConfigBlock(
  tool: 'claude-code' | 'cursor' | 'windsurf',
  port: number,
): string {
  const server = {
    'archigraph': {
      command: 'archigraph',
      args: ['mcp', '--port', String(port)],
    },
  }

  if (tool === 'claude-code') {
    return JSON.stringify({ mcpServers: server }, null, 2)
  }
  if (tool === 'cursor') {
    return JSON.stringify(
      { mcp: { servers: server } },
      null,
      2,
    )
  }
  // windsurf
  return JSON.stringify(
    {
      windsurf: {
        mcp: { servers: server },
      },
    },
    null,
    2,
  )
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

export async function fetchSettings(): Promise<SettingsReply> {
  return apiFetch<SettingsReply>('/api/settings')
}

export async function putSettings(patch: Partial<AppSettings>): Promise<SettingsReply> {
  return apiFetch<SettingsReply>('/api/settings', {
    method: 'PUT',
    body: JSON.stringify(patch),
  })
}

export async function resetSettings(): Promise<SettingsReply> {
  return apiFetch<SettingsReply>('/api/settings/reset', { method: 'POST' })
}
