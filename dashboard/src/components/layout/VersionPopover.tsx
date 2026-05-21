/**
 * VersionPopover — replaces the dead Settings cog in the top-nav.
 *
 * Trigger: lucide `Info` icon button.
 * Panel: anchored top-right, ~280 px wide.
 * Closes on outside-click or Escape.
 * Fetches /api/info on open; re-uses cached result for 5 s.
 */

import { useState, useEffect, useRef, useCallback } from 'react'
import { Info, ExternalLink, GitCommit, Clock, Globe, Server } from 'lucide-react'
import { Link } from 'react-router-dom'
import { fetchInfo, type DaemonInfo } from '@/api/client'

const CACHE_TTL_MS = 5_000

let cachedInfo: DaemonInfo | null = null
let cachedAt = 0

/** Format an ISO timestamp as a relative human string, e.g. "2 hours ago". */
function relativeTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  const secs = Math.floor(diff / 1000)
  if (secs < 60) return 'just now'
  const mins = Math.floor(secs / 60)
  if (mins < 60) return `${mins}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) {
    const rem = mins % 60
    return rem > 0 ? `${hours}h ${rem}m ago` : `${hours}h ago`
  }
  const days = Math.floor(hours / 24)
  return `${days}d ago`
}

/** Format a duration in seconds as "Xh Ym" or "Ym Zs". */
function fmtUptime(startedAt: string): string {
  const secs = Math.floor((Date.now() - new Date(startedAt).getTime()) / 1000)
  if (secs < 60) return `${secs}s`
  const mins = Math.floor(secs / 60)
  if (mins < 60) return `${mins}m`
  const h = Math.floor(mins / 60)
  const m = mins % 60
  return m > 0 ? `${h}h ${m}m` : `${h}h`
}

/** Shorten a commit SHA to 7 characters. */
function shortSHA(commit: string): string {
  if (!commit || commit === 'unknown') return 'unknown'
  return commit.length > 7 ? commit.slice(0, 7) : commit
}

/** Whether the app is running in debug / mock mode. */
const USE_MOCKS = import.meta.env.VITE_USE_MOCKS === 'true'
const DEBUG_PARAM =
  typeof window !== 'undefined' && new URLSearchParams(window.location.search).has('debug')
const IS_DEBUG = USE_MOCKS || DEBUG_PARAM

export function VersionPopover() {
  const [open, setOpen] = useState(false)
  const [info, setInfo] = useState<DaemonInfo | null>(null)
  const [error, setError] = useState<string | null>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)

  // Fetch info when the popover opens, respecting the 5 s cache.
  const loadInfo = useCallback(async () => {
    const now = Date.now()
    if (cachedInfo && now - cachedAt < CACHE_TTL_MS) {
      setInfo(cachedInfo)
      return
    }
    try {
      const data = await fetchInfo()
      cachedInfo = data
      cachedAt = Date.now()
      setInfo(data)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load info')
    }
  }, [])

  useEffect(() => {
    if (open) loadInfo()
  }, [open, loadInfo])

  // Close on outside-click.
  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (
        triggerRef.current?.contains(e.target as Node) ||
        panelRef.current?.contains(e.target as Node)
      )
        return
      setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  // Close on Escape.
  useEffect(() => {
    if (!open) return
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [open])

  const dashboardUrl =
    info?.dashboard_port
      ? `http://127.0.0.1:${info.dashboard_port}/`
      : 'http://127.0.0.1:47274/'

  const commitSHA = info ? shortSHA(info.commit) : null
  const commitUrl =
    commitSHA && commitSHA !== 'unknown'
      ? `https://github.com/cajasmota/archigraph/commit/${info!.commit}`
      : null

  return (
    <div className="relative">
      <button
        ref={triggerRef}
        type="button"
        aria-label="Version info"
        aria-expanded={open}
        aria-haspopup="true"
        onClick={() => setOpen((v) => !v)}
        className="p-1.5 rounded text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
        data-testid="version-info-trigger"
      >
        <Info className="w-4 h-4" />
      </button>

      {open && (
        <div
          ref={panelRef}
          role="dialog"
          aria-label="Version info panel"
          data-testid="version-info-panel"
          className="absolute right-0 top-full mt-1 z-50 w-72 rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 shadow-xl text-sm"
        >
          {/* Header */}
          <div className="px-4 py-3 border-b border-slate-200 dark:border-slate-800">
            <p className="font-semibold text-slate-900 dark:text-slate-100 tracking-tight">archigraph</p>
            {info ? (
              <p className="text-slate-600 dark:text-slate-400 mt-0.5">
                Version{' '}
                <span className="text-slate-800 dark:text-slate-200 font-mono">{info.version}</span>
              </p>
            ) : error ? (
              <p className="text-red-600 dark:text-red-400 mt-0.5">{error}</p>
            ) : (
              <p className="text-slate-500 mt-0.5">Loading…</p>
            )}
          </div>

          {/* Details */}
          {info && (
            <div className="px-4 py-3 space-y-2">
              {/* Commit */}
              <div className="flex items-center gap-2 text-slate-600 dark:text-slate-400">
                <GitCommit className="w-3.5 h-3.5 flex-shrink-0" />
                <span className="flex-1 truncate">
                  Commit:{' '}
                  {commitUrl ? (
                    <a
                      href={commitUrl}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-sky-600 dark:text-sky-400 hover:text-sky-500 dark:hover:text-sky-300 font-mono"
                    >
                      {commitSHA}
                    </a>
                  ) : (
                    <span className="text-slate-700 dark:text-slate-300 font-mono">{commitSHA}</span>
                  )}
                </span>
              </div>

              {/* Built */}
              {info.built_at && info.built_at !== 'unknown' && (
                <div className="flex items-center gap-2 text-slate-600 dark:text-slate-400">
                  <Clock className="w-3.5 h-3.5 flex-shrink-0" />
                  <span>
                    Built:{' '}
                    <span className="text-slate-700 dark:text-slate-300" title={info.built_at}>
                      {relativeTime(info.built_at)}
                    </span>
                  </span>
                </div>
              )}

              {/* Daemon uptime */}
              {info.daemon_started_at && (
                <div className="flex items-center gap-2 text-slate-600 dark:text-slate-400">
                  <Clock className="w-3.5 h-3.5 flex-shrink-0" />
                  <span>
                    Daemon uptime:{' '}
                    <span className="text-slate-700 dark:text-slate-300">{fmtUptime(info.daemon_started_at)}</span>
                  </span>
                </div>
              )}

              {/* Dashboard URL */}
              <div className="flex items-center gap-2 text-slate-600 dark:text-slate-400">
                <Globe className="w-3.5 h-3.5 flex-shrink-0" />
                <span className="truncate">
                  Dashboard:{' '}
                  <span className="text-slate-700 dark:text-slate-300 font-mono text-xs">{dashboardUrl}</span>
                </span>
              </div>
            </div>
          )}

          {/* Debug mode extras */}
          {IS_DEBUG && (
            <>
              <div className="mx-4 border-t border-slate-200 dark:border-slate-800" />
              <div className="px-4 py-2 text-xs text-amber-400">
                Build mode:{' '}
                {USE_MOCKS ? 'development (mocks)' : 'real-data (debug)'}
              </div>
            </>
          )}

          {/* Footer divider + links */}
          <div className="mx-4 border-t border-slate-200 dark:border-slate-800" />
          <div className="px-4 py-2 space-y-1.5">
            <Link
              to="/system"
              onClick={() => setOpen(false)}
              className="flex items-center gap-1.5 text-slate-600 dark:text-slate-400 hover:text-sky-400 dark:hover:text-sky-300 transition-colors text-sm"
            >
              <Server className="w-3.5 h-3.5" />
              System panel
            </Link>
            <a
              href="https://github.com/cajasmota/archigraph"
              target="_blank"
              rel="noopener noreferrer"
              className="flex items-center gap-1.5 text-sky-400 hover:text-sky-300 transition-colors"
            >
              <ExternalLink className="w-3.5 h-3.5" />
              View on GitHub
            </a>
          </div>
        </div>
      )}
    </div>
  )
}
