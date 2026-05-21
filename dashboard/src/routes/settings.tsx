import { useState, useEffect, useCallback, useRef } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  fetchSettings,
  putSettings,
  resetSettings,
  mcpConfigBlock,
  type AppSettings,
  type SettingsReply,
  type ThemePreset,
} from '@/api/settings'
import { fetchInfo } from '@/api/client'
import {
  Settings, Sun, Moon, Monitor, RefreshCw, BarChart2,
  Zap, FileText, Copy, Check, AlertTriangle, RotateCcw,
  Save, ChevronDown, ChevronRight, Palette,
} from 'lucide-react'
import { useThemeContext } from '@/context/ThemeContext'
import { ThemePaletteEditor } from '@/components/settings/ThemePaletteEditor'

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

function useDebounce<T>(value: T, delayMs: number): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const id = setTimeout(() => setDebounced(value), delayMs)
    return () => clearTimeout(id)
  }, [value, delayMs])
  return debounced
}

function cls(...parts: (string | boolean | undefined)[]) {
  return parts.filter(Boolean).join(' ')
}

// ─────────────────────────────────────────────────────────────────────────────
// Copy-to-clipboard button
// ─────────────────────────────────────────────────────────────────────────────

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)
  const onClick = async () => {
    await navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex items-center gap-1.5 px-2 py-1 rounded text-xs font-medium
        bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-400
        hover:bg-slate-200 dark:hover:bg-slate-700 transition-colors"
    >
      {copied ? <Check className="w-3.5 h-3.5 text-green-500" /> : <Copy className="w-3.5 h-3.5" />}
      {copied ? 'Copied!' : 'Copy'}
    </button>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Accordion section
// ─────────────────────────────────────────────────────────────────────────────

interface SectionProps {
  title: string
  icon: React.ReactNode
  defaultOpen?: boolean
  children: React.ReactNode
}

function Section({ title, icon, defaultOpen = false, children }: SectionProps) {
  const [open, setOpen] = useState(defaultOpen)
  return (
    <div className="border border-slate-200 dark:border-slate-800 rounded-lg overflow-hidden">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="w-full flex items-center gap-3 px-5 py-4 text-left
          bg-slate-50 dark:bg-slate-900 hover:bg-slate-100 dark:hover:bg-slate-800
          transition-colors"
      >
        <span className="text-sky-400">{icon}</span>
        <span className="flex-1 font-medium text-sm text-slate-800 dark:text-slate-200">{title}</span>
        {open
          ? <ChevronDown className="w-4 h-4 text-slate-400" />
          : <ChevronRight className="w-4 h-4 text-slate-400" />}
      </button>
      {open && (
        <div className="px-5 py-5 space-y-5 bg-white dark:bg-slate-950">
          {children}
        </div>
      )}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Form primitives
// ─────────────────────────────────────────────────────────────────────────────

function Label({ children, note }: { children: React.ReactNode; note?: string }) {
  return (
    <div>
      <span className="block text-sm font-medium text-slate-700 dark:text-slate-300">{children}</span>
      {note && <span className="block text-xs text-slate-400 dark:text-slate-500 mt-0.5">{note}</span>}
    </div>
  )
}

function Row({ children }: { children: React.ReactNode }) {
  return <div className="flex items-start gap-4 justify-between">{children}</div>
}

interface SelectProps<T extends string> {
  value: T
  options: { value: T; label: string }[]
  onChange: (v: T) => void
}

function Select<T extends string>({ value, options, onChange }: SelectProps<T>) {
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value as T)}
      className="rounded border border-slate-300 dark:border-slate-700
        bg-white dark:bg-slate-900 text-slate-800 dark:text-slate-200
        px-2 py-1 text-sm focus:outline-none focus:ring-2 focus:ring-sky-500"
    >
      {options.map((o) => (
        <option key={o.value} value={o.value}>{o.label}</option>
      ))}
    </select>
  )
}

function Toggle({ checked, onChange }: { checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      onClick={() => onChange(!checked)}
      className={cls(
        'relative inline-flex h-6 w-11 items-center rounded-full transition-colors flex-shrink-0',
        checked ? 'bg-sky-500' : 'bg-slate-300 dark:bg-slate-700',
      )}
    >
      <span
        className={cls(
          'inline-block h-4 w-4 rounded-full bg-white shadow transform transition-transform',
          checked ? 'translate-x-6' : 'translate-x-1',
        )}
      />
    </button>
  )
}

interface SliderProps {
  value: number
  min: number
  max: number
  step?: number
  unit?: string
  onChange: (v: number) => void
}

function Slider({ value, min, max, step = 1, unit, onChange }: SliderProps) {
  return (
    <div className="flex items-center gap-3">
      <input
        type="range"
        min={min}
        max={max}
        step={step}
        value={value}
        onChange={(e) => onChange(Number(e.target.value))}
        className="w-40 accent-sky-500"
      />
      <span className="text-sm text-slate-600 dark:text-slate-400 tabular-nums w-16">
        {value}{unit ?? ''}
      </span>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Restart-required badge
// ─────────────────────────────────────────────────────────────────────────────

function RestartBadge({ keys, changed }: { keys: string[]; changed: Partial<AppSettings> }) {
  const affected = keys.filter((k) => k in changed)
  if (affected.length === 0) return null
  return (
    <div className="flex items-center gap-2 text-xs text-amber-600 dark:text-amber-400
      bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800
      rounded px-3 py-2">
      <AlertTriangle className="w-3.5 h-3.5 flex-shrink-0" />
      <span>
        <strong>Restart required</strong> for: {affected.join(', ')}. Run{' '}
        <code className="font-mono">archigraph daemon restart</code>.
      </span>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// MCP config section
// ─────────────────────────────────────────────────────────────────────────────

function MCPSection({ port }: { port: number }) {
  const [active, setActive] = useState<'claude-code' | 'cursor' | 'windsurf'>('claude-code')
  const snippet = mcpConfigBlock(active, port)

  return (
    <div className="space-y-4">
      <p className="text-sm text-slate-500 dark:text-slate-400">
        Paste the config block below into your editor's MCP settings file.
      </p>

      {/* Tool picker */}
      <div className="flex gap-2">
        {(['claude-code', 'cursor', 'windsurf'] as const).map((t) => (
          <button
            key={t}
            type="button"
            onClick={() => setActive(t)}
            className={cls(
              'px-3 py-1.5 rounded text-sm font-medium border transition-colors',
              active === t
                ? 'bg-sky-500 border-sky-500 text-white'
                : 'border-slate-300 dark:border-slate-700 text-slate-600 dark:text-slate-400 hover:bg-slate-50 dark:hover:bg-slate-800',
            )}
          >
            {t === 'claude-code' ? 'Claude Code' : t.charAt(0).toUpperCase() + t.slice(1)}
          </button>
        ))}
      </div>

      {/* Code block */}
      <div className="relative">
        <pre className="rounded bg-slate-900 text-slate-100 text-xs p-4 overflow-x-auto leading-relaxed">
          {snippet}
        </pre>
        <div className="absolute top-2 right-2">
          <CopyButton text={snippet} />
        </div>
      </div>

      <p className="text-xs text-slate-400">
        Daemon port: <code className="font-mono">{port}</code>
      </p>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Main SettingsRoute
// ─────────────────────────────────────────────────────────────────────────────

const QUERY_KEY = ['settings'] as const
const DEBOUNCE_MS = 800

// ─────────────────────────────────────────────────────────────────────────────
// Preset descriptors
// ─────────────────────────────────────────────────────────────────────────────

const PRESET_OPTIONS: Array<{ value: ThemePreset; label: string; description: string }> = [
  { value: 'default',           label: 'Default',          description: 'Standard slate palette (light/dark)' },
  { value: 'solarized-dark',    label: 'Solarized Dark',   description: 'Low-contrast retro warmth' },
  { value: 'nord',              label: 'Nord',             description: 'Polar night arctic cool' },
  { value: 'catppuccin-mocha',  label: 'Catppuccin Mocha', description: 'Pastel soft dark' },
  { value: 'high-contrast',     label: 'High Contrast',    description: 'Maximum accessibility contrast' },
  { value: 'custom',            label: 'Custom',           description: 'Define your own colours' },
]

// ─────────────────────────────────────────────────────────────────────────────
// Main SettingsRoute
// ─────────────────────────────────────────────────────────────────────────────

export function SettingsRoute() {
  const qc = useQueryClient()
  const {
    theme: ctxTheme,
    toggle,
    preset,
    palette,
    setPreset,
    setPalette,
    exportPaletteJSON,
    importPaletteJSON,
    resetPalette,
  } = useThemeContext()

  const { data, isLoading, error } = useQuery<SettingsReply>({
    queryKey: QUERY_KEY,
    queryFn: fetchSettings,
    staleTime: 0,
  })

  const { data: info } = useQuery({
    queryKey: ['info'],
    queryFn: fetchInfo,
    staleTime: 60_000,
  })

  // Local edits tracked here — flushed to API on debounce or explicit Save.
  const [local, setLocal] = useState<Partial<AppSettings>>({})
  const [restartKeys, setRestartKeys] = useState<string[]>([])
  const [saveStatus, setSaveStatus] = useState<'idle' | 'saving' | 'saved' | 'error'>('idle')
  const pendingRef = useRef(false)

  // When server data loads, reset local patch.
  useEffect(() => {
    if (data) setLocal({})
  }, [data])

  const mutation = useMutation({
    mutationFn: (patch: Partial<AppSettings>) => putSettings(patch),
    onSuccess: (reply) => {
      qc.setQueryData(QUERY_KEY, reply)
      setRestartKeys(reply.restart_required ?? [])
      setSaveStatus('saved')
      pendingRef.current = false
      setTimeout(() => setSaveStatus('idle'), 2500)
    },
    onError: () => {
      setSaveStatus('error')
      pendingRef.current = false
    },
  })

  const resetMutation = useMutation({
    mutationFn: resetSettings,
    onSuccess: (reply) => {
      qc.setQueryData(QUERY_KEY, reply)
      setLocal({})
      setRestartKeys([])
    },
  })

  // Merge server + local to get the effective current value.
  const effective = { ...(data?.settings ?? ({} as AppSettings)), ...local }

  const patch = useCallback((key: keyof AppSettings, value: unknown) => {
    setLocal((prev) => ({ ...prev, [key]: value }))
    pendingRef.current = true
    setSaveStatus('idle')
  }, [])

  // Theme is special — update ThemeContext immediately for live preview.
  const patchTheme = useCallback(
    (t: 'light' | 'dark' | 'auto') => {
      patch('theme', t)
      // Live preview: apply to DOM now, matching ThemeContext's semantics.
      if (t === 'dark' && ctxTheme !== 'dark') toggle()
      if (t === 'light' && ctxTheme !== 'light') toggle()
      // 'auto' resets to system preference — simplification: treat as light.
    },
    [patch, ctxTheme, toggle],
  )

  // Auto-save on debounce.
  const debouncedLocal = useDebounce(local, DEBOUNCE_MS)
  useEffect(() => {
    if (Object.keys(debouncedLocal).length > 0 && pendingRef.current) {
      setSaveStatus('saving')
      mutation.mutate(debouncedLocal)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debouncedLocal])

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-full text-slate-400">
        Loading settings…
      </div>
    )
  }
  if (error) {
    return (
      <div className="flex items-center justify-center h-full text-red-500">
        Failed to load settings: {String(error)}
      </div>
    )
  }

  const port = info?.dashboard_port ?? 47274

  return (
    <div className="h-full overflow-y-auto">
      <div className="max-w-2xl mx-auto px-4 py-8 space-y-5">
        {/* Header */}
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <Settings className="w-6 h-6 text-sky-400" />
            <h1 className="text-xl font-semibold text-slate-800 dark:text-slate-200">Settings</h1>
          </div>

          <div className="flex items-center gap-3">
            {/* Save status indicator */}
            {saveStatus === 'saving' && (
              <span className="text-xs text-slate-400">Saving…</span>
            )}
            {saveStatus === 'saved' && (
              <span className="flex items-center gap-1 text-xs text-green-500">
                <Check className="w-3.5 h-3.5" /> Saved
              </span>
            )}
            {saveStatus === 'error' && (
              <span className="text-xs text-red-500">Save failed</span>
            )}

            {/* Manual save button */}
            <button
              type="button"
              onClick={() => {
                if (Object.keys(local).length > 0) {
                  setSaveStatus('saving')
                  mutation.mutate(local)
                }
              }}
              disabled={Object.keys(local).length === 0 || mutation.isPending}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded text-sm font-medium
                bg-sky-500 text-white hover:bg-sky-600 disabled:opacity-40 disabled:cursor-not-allowed
                transition-colors"
            >
              <Save className="w-3.5 h-3.5" />
              Save
            </button>

            {/* Reset to defaults */}
            <button
              type="button"
              onClick={() => {
                if (window.confirm('Reset all settings to factory defaults?')) {
                  resetMutation.mutate()
                }
              }}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded text-sm font-medium
                border border-slate-300 dark:border-slate-700 text-slate-600 dark:text-slate-400
                hover:bg-slate-50 dark:hover:bg-slate-800 transition-colors"
            >
              <RotateCcw className="w-3.5 h-3.5" />
              Reset
            </button>
          </div>
        </div>

        {/* Restart-required banner */}
        <RestartBadge keys={restartKeys} changed={local} />

        {/* 1. General */}
        <Section title="General" icon={<Settings className="w-4 h-4" />} defaultOpen>
          <Row>
            <Label note="Controls the colour scheme across all surfaces.">Theme</Label>
            <div className="flex gap-2">
              {(['light', 'dark', 'auto'] as const).map((t) => (
                <button
                  key={t}
                  type="button"
                  onClick={() => patchTheme(t)}
                  className={cls(
                    'flex items-center gap-1.5 px-3 py-1.5 rounded border text-sm transition-colors',
                    effective.theme === t
                      ? 'bg-sky-500 border-sky-500 text-white'
                      : 'border-slate-300 dark:border-slate-700 text-slate-600 dark:text-slate-400 hover:bg-slate-50 dark:hover:bg-slate-800',
                  )}
                >
                  {t === 'light' && <Sun className="w-3.5 h-3.5" />}
                  {t === 'dark' && <Moon className="w-3.5 h-3.5" />}
                  {t === 'auto' && <Monitor className="w-3.5 h-3.5" />}
                  {t.charAt(0).toUpperCase() + t.slice(1)}
                </button>
              ))}
            </div>
          </Row>

          <Row>
            <Label note="Group shown on the landing page before a selection is made.">Default group</Label>
            <input
              type="text"
              value={effective.default_group ?? ''}
              onChange={(e) => patch('default_group', e.target.value)}
              placeholder="e.g. fixture-a"
              className="rounded border border-slate-300 dark:border-slate-700
                bg-white dark:bg-slate-900 text-slate-800 dark:text-slate-200
                px-2 py-1 text-sm focus:outline-none focus:ring-2 focus:ring-sky-500 w-44"
            />
          </Row>
        </Section>

        {/* 2. Themes */}
        <Section
          title="Themes & Colours"
          icon={<Palette className="w-4 h-4" />}
          defaultOpen={false}
        >
          <div data-testid="themes-section">
            {/* Preset grid */}
            <div className="space-y-2 mb-5">
              <Label note="Pick a built-in colour preset or create your own.">Colour preset</Label>
              <div className="grid grid-cols-2 sm:grid-cols-3 gap-2 mt-2" data-testid="preset-grid">
                {PRESET_OPTIONS.map((opt) => (
                  <button
                    key={opt.value}
                    type="button"
                    onClick={() => setPreset(opt.value)}
                    data-testid={`preset-btn-${opt.value}`}
                    className={cls(
                      'flex flex-col items-start gap-0.5 px-3 py-2.5 rounded-lg border text-left transition-colors',
                      preset === opt.value
                        ? 'border-sky-500 bg-sky-50 dark:bg-sky-900/20 text-sky-700 dark:text-sky-300'
                        : 'border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-400 hover:bg-slate-50 dark:hover:bg-slate-800',
                    )}
                  >
                    <span className="text-sm font-medium">{opt.label}</span>
                    <span className="text-xs opacity-70">{opt.description}</span>
                  </button>
                ))}
              </div>
            </div>

            {/* Custom palette editor — only shown when custom preset is active */}
            {preset === 'custom' && (
              <div className="border-t border-slate-200 dark:border-slate-700 pt-4">
                <Label note="Adjust individual colours. Changes apply instantly.">
                  Custom palette
                </Label>
                <div className="mt-3">
                  <ThemePaletteEditor
                    palette={palette}
                    onChangePalette={setPalette}
                    onExport={exportPaletteJSON}
                    onImport={importPaletteJSON}
                    onReset={resetPalette}
                  />
                </div>
              </div>
            )}
          </div>
        </Section>

        {/* 3. Updates */}
        <Section title="Updates" icon={<RefreshCw className="w-4 h-4" />}>
          <Row>
            <Label note="Check for a new daemon version on each launch.">Auto-check for updates</Label>
            <Toggle
              checked={effective.auto_check_updates ?? true}
              onChange={(v) => patch('auto_check_updates', v)}
            />
          </Row>

          <Row>
            <Label note="stable = official releases; dev = pre-releases.">Update channel</Label>
            <Select
              value={effective.update_channel ?? 'stable'}
              options={[
                { value: 'stable', label: 'Stable' },
                { value: 'dev', label: 'Dev / pre-release' },
              ]}
              onChange={(v) => patch('update_channel', v)}
            />
          </Row>

          <Row>
            <Label note="Cron schedule for automatic graph refreshes. Leave blank for manual only.">
              Refresh schedule
            </Label>
            <input
              type="text"
              value={effective.refresh_schedule ?? ''}
              onChange={(e) => patch('refresh_schedule', e.target.value)}
              placeholder="e.g. 0 */4 * * *"
              className="rounded border border-slate-300 dark:border-slate-700
                bg-white dark:bg-slate-900 text-slate-800 dark:text-slate-200
                px-2 py-1 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-sky-500 w-44"
            />
          </Row>
        </Section>

        {/* 3. MCP */}
        <Section title="MCP Configuration" icon={<Zap className="w-4 h-4" />}>
          <MCPSection port={port} />
        </Section>

        {/* 4. Telemetry */}
        <Section title="Telemetry" icon={<BarChart2 className="w-4 h-4" />}>
          <Row>
            <Label note="Send anonymous usage data (no source code, no entity names). Off by default.">
              Anonymous telemetry
            </Label>
            <Toggle
              checked={effective.telemetry_enabled ?? false}
              onChange={(v) => patch('telemetry_enabled', v)}
            />
          </Row>
          <p className="text-xs text-slate-400 dark:text-slate-500">
            Only aggregate counts (surfaces visited, graph sizes) are collected. No code, no file
            paths, no entity labels. Source available for audit.
          </p>
        </Section>

        {/* 5. Performance */}
        <Section title="Performance" icon={<Zap className="w-4 h-4" />}>
          <Row>
            <Label note="Maximum RAM the daemon may consume. Requires restart.">
              Daemon RSS budget
            </Label>
            <Slider
              value={effective.daemon_rss_budget_mb ?? 512}
              min={100}
              max={2000}
              step={50}
              unit=" MB"
              onChange={(v) => patch('daemon_rss_budget_mb', v)}
            />
          </Row>

          <Row>
            <Label note="How long the file watcher waits before triggering a re-index.">
              Watcher debounce
            </Label>
            <Slider
              value={effective.watcher_debounce_secs ?? 2}
              min={1}
              max={60}
              unit="s"
              onChange={(v) => patch('watcher_debounce_secs', v)}
            />
          </Row>

          <Row>
            <Label note="Number of parallel goroutines used during indexing. Requires restart.">
              Indexer parallelism
            </Label>
            <Slider
              value={effective.indexer_parallelism ?? 4}
              min={1}
              max={32}
              onChange={(v) => patch('indexer_parallelism', v)}
            />
          </Row>

          {(restartKeys.includes('daemon_rss_budget_mb') ||
            restartKeys.includes('indexer_parallelism')) && (
            <p className="text-xs text-amber-500">
              One or more performance settings require a daemon restart to take effect.
            </p>
          )}
        </Section>

        {/* 6. Logs */}
        <Section title="Logs" icon={<FileText className="w-4 h-4" />}>
          <Row>
            <Label note="Verbosity of the daemon log. debug produces a lot of output.">
              Log level
            </Label>
            <Select
              value={effective.log_level ?? 'info'}
              options={[
                { value: 'debug', label: 'Debug (verbose)' },
                { value: 'info', label: 'Info (default)' },
                { value: 'warn', label: 'Warn' },
                { value: 'error', label: 'Error (quiet)' },
              ]}
              onChange={(v) => patch('log_level', v as AppSettings['log_level'])}
            />
          </Row>
          <p className="text-xs text-slate-400 dark:text-slate-500">
            Logs are written to <code className="font-mono">~/.archigraph/daemon.log</code>.
          </p>
        </Section>
      </div>
    </div>
  )
}
