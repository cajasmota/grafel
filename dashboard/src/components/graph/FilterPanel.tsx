/**
 * FilterPanel — multi-criteria graph filter sidebar section (#1367).
 *
 * Collapsible section with:
 *   - Kind: multi-select of EntityKind values
 *   - Community: multi-select (community IDs)
 *   - Min degree: number input
 *   - File path glob: text input
 *   - Has property: dropdown
 *   - Logic toggle: AND / OR
 *   - Invert filter toggle
 *   - 'Clear all' button
 *   - Match count badge (live)
 *
 * All state is managed externally via the useGraphFilter hook.
 * The match count is passed in as `matchCount` so the parent controls
 * when / how the filter is applied to the Cosmograph canvas.
 */

import { useState, useId } from 'react'
import { ChevronDown, ChevronRight, X, Filter } from 'lucide-react'
import type { EntityKind } from '@/types/api'
import type { GraphFilterState } from '@/hooks/graph/useGraphFilter'
import type { Community } from '@/types/api'

// ────────────────────────────────────────────────────────────────────────────
// Stable list of filterable entity kinds (ordered by frequency of use)
// ────────────────────────────────────────────────────────────────────────────

const ALL_KINDS: EntityKind[] = [
  'Function', 'Class', 'Component', 'Operation', 'Endpoint', 'Route',
  'Schema', 'Model', 'DataAccess', 'Datastore', 'Queue', 'Event',
  'Service', 'View', 'UIComponent', 'JSX', 'Stylesheet',
  'ExternalAPI', 'InfraResource', 'Config', 'Process',
  'AgentPattern', 'MessageTopic', 'Document', 'CodeBlock',
  'Variable', 'Reference', 'Pattern', 'Project', 'External', 'ScopeUnknown',
]

const HAS_PROPERTY_OPTIONS = [
  { value: '',            label: '— any —' },
  { value: 'description', label: 'description' },
  { value: 'domain',      label: 'domain' },
  { value: 'summary',     label: 'summary' },
  { value: 'deprecated',  label: 'deprecated' },
  { value: 'version',     label: 'version' },
  { value: 'owner',       label: 'owner' },
  { value: 'tags',        label: 'tags' },
]

// ────────────────────────────────────────────────────────────────────────────
// Props
// ────────────────────────────────────────────────────────────────────────────

export interface FilterPanelProps {
  filter:           GraphFilterState
  /** Number of nodes that currently match the active filter (-1 = not computed) */
  matchCount:       number
  /** All communities available for multi-select */
  communities:      Community[]
  onSetKinds:       (kinds: Set<EntityKind>) => void
  onSetCommunities: (ids: Set<number>) => void
  onSetMinDegree:   (v: number) => void
  onSetFileGlob:    (v: string) => void
  onSetHasProperty: (v: string) => void
  onSetInvert:      (v: boolean) => void
  onSetLogic:       (v: 'and' | 'or') => void
  onClearAll:       () => void
}

// ────────────────────────────────────────────────────────────────────────────
// Component
// ────────────────────────────────────────────────────────────────────────────

export function FilterPanel({
  filter,
  matchCount,
  communities,
  onSetKinds,
  onSetCommunities,
  onSetMinDegree,
  onSetFileGlob,
  onSetHasProperty,
  onSetInvert,
  onSetLogic,
  onClearAll,
}: FilterPanelProps) {
  const [open, setOpen] = useState(false)
  const [kindSearchQuery, setKindSearchQuery] = useState('')
  const id = useId()

  const activeCount =
    filter.kinds.size +
    filter.communityIds.size +
    (filter.minDegree > 0 ? 1 : 0) +
    (filter.fileGlob ? 1 : 0) +
    (filter.hasProperty ? 1 : 0)

  const filteredKinds = kindSearchQuery
    ? ALL_KINDS.filter((k) => k.toLowerCase().includes(kindSearchQuery.toLowerCase()))
    : ALL_KINDS

  function toggleKind(kind: EntityKind) {
    const next = new Set(filter.kinds)
    if (next.has(kind)) next.delete(kind)
    else next.add(kind)
    onSetKinds(next)
  }

  function toggleCommunity(id: number) {
    const next = new Set(filter.communityIds)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    onSetCommunities(next)
  }

  const sortedCommunities = [...communities].sort((a, b) => b.size - a.size).slice(0, 40)

  return (
    <div data-testid="filter-panel">
      {/* ── Section header ── */}
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        aria-controls={`filter-panel-body-${id}`}
        className={[
          'flex items-center justify-between w-full px-2 py-1 rounded',
          'text-[10px] uppercase tracking-wider text-slate-500 dark:text-slate-600 font-semibold',
          'hover:bg-slate-200/40 dark:hover:bg-slate-800/40 transition-colors',
          'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
        ].join(' ')}
        data-testid="filter-panel-toggle"
      >
        <span className="flex items-center gap-1.5">
          <Filter className="w-3 h-3" aria-hidden />
          Filter
          {activeCount > 0 && (
            <span
              className="inline-flex items-center justify-center w-4 h-4 rounded-full text-[9px] font-bold bg-sky-500 text-white"
              aria-label={`${activeCount} active filters`}
              data-testid="filter-active-badge"
            >
              {activeCount}
            </span>
          )}
        </span>
        <span className="flex items-center gap-1">
          {matchCount >= 0 && (
            <span
              className="text-[9px] font-mono text-slate-400 tabular-nums"
              aria-live="polite"
              aria-label={`${matchCount.toLocaleString()} matching nodes`}
              data-testid="filter-match-count"
            >
              {matchCount.toLocaleString()}
            </span>
          )}
          {open
            ? <ChevronDown className="w-3 h-3 text-slate-400" aria-hidden />
            : <ChevronRight className="w-3 h-3 text-slate-400" aria-hidden />
          }
        </span>
      </button>

      {open && (
        <div
          id={`filter-panel-body-${id}`}
          className="flex flex-col gap-3 mt-1 px-1"
          data-testid="filter-panel-body"
        >

          {/* ── Logic toggle (AND / OR) ── */}
          <div>
            <p className="text-[9px] uppercase tracking-wider text-slate-500/70 dark:text-slate-600/70 font-semibold px-1 pb-1">
              Match logic
            </p>
            <div className="flex gap-1">
              {(['and', 'or'] as const).map((v) => (
                <button
                  key={v}
                  type="button"
                  onClick={() => onSetLogic(v)}
                  aria-pressed={filter.filterLogic === v}
                  className={[
                    'flex-1 py-0.5 rounded text-[10px] font-medium transition-colors border',
                    'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
                    filter.filterLogic === v
                      ? 'bg-sky-900/40 text-sky-300 border-sky-700'
                      : 'bg-transparent text-slate-500 border-slate-600 hover:border-slate-500 hover:text-slate-300',
                  ].join(' ')}
                  data-testid={`filter-logic-${v}`}
                >
                  {v.toUpperCase()}
                </button>
              ))}
            </div>
          </div>

          {/* ── Kind multi-select ── */}
          <div>
            <p className="text-[9px] uppercase tracking-wider text-slate-500/70 dark:text-slate-600/70 font-semibold px-1 pb-1">
              Kind
              {filter.kinds.size > 0 && (
                <span className="ml-1 text-sky-400">({filter.kinds.size})</span>
              )}
            </p>
            <input
              type="search"
              placeholder="Search kinds…"
              value={kindSearchQuery}
              onChange={(e) => setKindSearchQuery(e.target.value)}
              className={[
                'w-full px-2 py-0.5 mb-1 rounded text-[10px]',
                'bg-slate-200 dark:bg-slate-800 text-slate-700 dark:text-slate-300',
                'border border-slate-300 dark:border-slate-700 focus:border-sky-600 focus:outline-none',
                'placeholder-slate-500',
              ].join(' ')}
              aria-label="Search entity kinds"
              data-testid="filter-kind-search"
            />
            <div
              className="flex flex-col gap-0.5 max-h-[140px] overflow-y-auto scrollbar-thin"
              role="group"
              aria-label="Entity kind filter"
            >
              {filteredKinds.map((kind) => {
                const checked = filter.kinds.has(kind)
                return (
                  <button
                    key={kind}
                    type="button"
                    role="checkbox"
                    aria-checked={checked}
                    onClick={() => toggleKind(kind)}
                    className={[
                      'flex items-center gap-1.5 px-2 py-0.5 rounded text-left text-[10px] w-full transition-colors',
                      'hover:bg-slate-200/60 dark:hover:bg-slate-800/60',
                      'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
                      checked ? 'text-sky-300 bg-sky-900/20' : 'text-slate-600 dark:text-slate-400',
                    ].join(' ')}
                    data-testid={`filter-kind-${kind}`}
                  >
                    <span
                      className={[
                        'w-3 h-3 rounded border shrink-0 flex items-center justify-center',
                        checked
                          ? 'bg-sky-500 border-sky-500 text-white'
                          : 'border-slate-500 bg-transparent',
                      ].join(' ')}
                      aria-hidden
                    >
                      {checked && (
                        <svg width="8" height="8" viewBox="0 0 8 8" fill="none">
                          <path d="M1.5 4L3.5 6L6.5 2" stroke="white" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
                        </svg>
                      )}
                    </span>
                    {kind}
                  </button>
                )
              })}
              {filteredKinds.length === 0 && (
                <p className="px-2 py-1 text-[10px] text-slate-500">No matching kinds</p>
              )}
            </div>
          </div>

          {/* ── Community multi-select ── */}
          {sortedCommunities.length > 0 && (
            <div>
              <p className="text-[9px] uppercase tracking-wider text-slate-500/70 dark:text-slate-600/70 font-semibold px-1 pb-1">
                Community
                {filter.communityIds.size > 0 && (
                  <span className="ml-1 text-sky-400">({filter.communityIds.size})</span>
                )}
              </p>
              <div
                className="flex flex-col gap-0.5 max-h-[100px] overflow-y-auto scrollbar-thin"
                role="group"
                aria-label="Community filter"
              >
                {sortedCommunities.map((c) => {
                  const checked = filter.communityIds.has(c.id)
                  const name = c.agent_name ?? c.auto_name ?? `Community ${c.id}`
                  return (
                    <button
                      key={`${c.id}-${c.repo}`}
                      type="button"
                      role="checkbox"
                      aria-checked={checked}
                      onClick={() => toggleCommunity(c.id)}
                      className={[
                        'flex items-center gap-1.5 px-2 py-0.5 rounded text-left text-[10px] w-full transition-colors',
                        'hover:bg-slate-200/60 dark:hover:bg-slate-800/60',
                        'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
                        checked ? 'text-sky-300 bg-sky-900/20' : 'text-slate-600 dark:text-slate-400',
                      ].join(' ')}
                      data-testid={`filter-community-${c.id}`}
                    >
                      <span
                        className={[
                          'w-3 h-3 rounded border shrink-0 flex items-center justify-center',
                          checked
                            ? 'bg-sky-500 border-sky-500 text-white'
                            : 'border-slate-500 bg-transparent',
                        ].join(' ')}
                        aria-hidden
                      >
                        {checked && (
                          <svg width="8" height="8" viewBox="0 0 8 8" fill="none">
                            <path d="M1.5 4L3.5 6L6.5 2" stroke="white" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
                          </svg>
                        )}
                      </span>
                      <span className="flex-1 truncate">{name}</span>
                      <span className="text-slate-500 tabular-nums">{c.size}</span>
                    </button>
                  )
                })}
              </div>
            </div>
          )}

          {/* ── Min degree ── */}
          <div>
            <label
              htmlFor={`filter-min-degree-${id}`}
              className="block text-[9px] uppercase tracking-wider text-slate-500/70 dark:text-slate-600/70 font-semibold px-1 pb-1"
            >
              Min degree
            </label>
            <input
              id={`filter-min-degree-${id}`}
              type="number"
              min={0}
              max={9999}
              step={1}
              value={filter.minDegree}
              onChange={(e) => {
                const v = parseInt(e.target.value, 10)
                onSetMinDegree(isNaN(v) || v < 0 ? 0 : v)
              }}
              className={[
                'w-full px-2 py-0.5 rounded text-[10px]',
                'bg-slate-200 dark:bg-slate-800 text-slate-700 dark:text-slate-300',
                'border border-slate-300 dark:border-slate-700 focus:border-sky-600 focus:outline-none',
              ].join(' ')}
              aria-label="Minimum node degree"
              data-testid="filter-min-degree"
            />
          </div>

          {/* ── File path glob ── */}
          <div>
            <label
              htmlFor={`filter-file-glob-${id}`}
              className="block text-[9px] uppercase tracking-wider text-slate-500/70 dark:text-slate-600/70 font-semibold px-1 pb-1"
            >
              File path glob
            </label>
            <input
              id={`filter-file-glob-${id}`}
              type="text"
              value={filter.fileGlob}
              onChange={(e) => onSetFileGlob(e.target.value)}
              placeholder="e.g. **/api/*.py"
              className={[
                'w-full px-2 py-0.5 rounded text-[10px]',
                'bg-slate-200 dark:bg-slate-800 text-slate-700 dark:text-slate-300',
                'border border-slate-300 dark:border-slate-700 focus:border-sky-600 focus:outline-none',
                'placeholder-slate-500',
              ].join(' ')}
              aria-label="File path glob filter"
              data-testid="filter-file-glob"
            />
          </div>

          {/* ── Has property ── */}
          <div>
            <label
              htmlFor={`filter-has-property-${id}`}
              className="block text-[9px] uppercase tracking-wider text-slate-500/70 dark:text-slate-600/70 font-semibold px-1 pb-1"
            >
              Has property
            </label>
            <select
              id={`filter-has-property-${id}`}
              value={filter.hasProperty}
              onChange={(e) => onSetHasProperty(e.target.value)}
              className={[
                'w-full px-2 py-0.5 rounded text-[10px]',
                'bg-slate-200 dark:bg-slate-800 text-slate-700 dark:text-slate-300',
                'border border-slate-300 dark:border-slate-700 focus:border-sky-600 focus:outline-none',
              ].join(' ')}
              aria-label="Filter by presence of property"
              data-testid="filter-has-property"
            >
              {HAS_PROPERTY_OPTIONS.map((opt) => (
                <option key={opt.value} value={opt.value}>{opt.label}</option>
              ))}
            </select>
          </div>

          {/* ── Invert filter toggle ── */}
          <div className="flex items-center justify-between px-1">
            <label
              htmlFor={`filter-invert-${id}`}
              className="text-[10px] text-slate-500 dark:text-slate-500 cursor-pointer"
            >
              Invert filter
            </label>
            <button
              id={`filter-invert-${id}`}
              type="button"
              role="switch"
              aria-checked={filter.invertFilter}
              onClick={() => onSetInvert(!filter.invertFilter)}
              className={[
                'relative inline-flex h-4 w-7 items-center rounded-full transition-colors',
                'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
                filter.invertFilter ? 'bg-sky-500' : 'bg-slate-600',
              ].join(' ')}
              aria-label="Invert filter — show non-matching nodes instead"
              data-testid="filter-invert"
            >
              <span
                className={[
                  'inline-block h-3 w-3 rounded-full bg-white transition-transform',
                  filter.invertFilter ? 'translate-x-3.5' : 'translate-x-0.5',
                ].join(' ')}
                aria-hidden
              />
            </button>
          </div>

          {/* ── Clear all + match count row ── */}
          <div className="flex items-center justify-between gap-2 pt-1 border-t border-slate-200 dark:border-slate-700">
            {matchCount >= 0 ? (
              <span
                className="text-[10px] text-slate-400 tabular-nums"
                aria-live="polite"
                aria-label={`${matchCount.toLocaleString()} nodes match current filters`}
              >
                {matchCount.toLocaleString()} match{matchCount !== 1 ? 'es' : ''}
              </span>
            ) : (
              <span />
            )}
            <button
              type="button"
              onClick={onClearAll}
              disabled={activeCount === 0 && !filter.invertFilter}
              className={[
                'flex items-center gap-1 px-2 py-0.5 rounded text-[10px] font-medium transition-colors border',
                'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
                activeCount > 0 || filter.invertFilter
                  ? 'border-rose-700 text-rose-400 hover:bg-rose-900/20'
                  : 'border-slate-700 text-slate-600 opacity-50 cursor-not-allowed',
              ].join(' ')}
              aria-label="Clear all filters"
              data-testid="filter-clear-all"
            >
              <X className="w-3 h-3" aria-hidden />
              Clear all
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
