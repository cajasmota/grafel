/**
 * Settings surface API — GET/PUT /api/settings, POST /api/settings/reset
 *
 * All preferences are owned by ~/.archigraph/settings.json on the daemon side.
 * The frontend reads them once on mount, auto-saves on change (debounced),
 * and exposes a reset-to-defaults action.
 */

// ── Wire types ────────────────────────────────────────────────────────────────

/** Built-in colour preset names. 'default' means the standard Tailwind slate palette. */
export type ThemePreset =
  | 'default'
  | 'solarized-dark'
  | 'nord'
  | 'catppuccin-mocha'
  | 'high-contrast'
  | 'custom'

/** Per-component colour overrides used by the 'custom' preset. */
export interface CustomPalette {
  bg: string
  bg_card: string
  bg_input: string
  fg: string
  fg_muted: string
  border: string
  accent: string
  accent_fg: string
}

export const DEFAULT_CUSTOM_PALETTE: CustomPalette = {
  bg:       '#0f172a',
  bg_card:  '#1e293b',
  bg_input: '#1e293b',
  fg:       '#e2e8f0',
  fg_muted: '#64748b',
  border:   '#334155',
  accent:   '#0ea5e9',
  accent_fg:'#ffffff',
}

export interface AppSettings {
  // General
  theme: 'light' | 'dark' | 'auto'
  theme_preset: ThemePreset
  theme_custom: CustomPalette
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
