import { useState } from 'react'
import { AlertTriangle, ChevronRight, Loader2, X } from 'lucide-react'
import type { OrphanPublisher } from '@/types/api'

// ── Broker badge ──────────────────────────────────────────────────────────────

function BrokerBadge({ broker }: { broker: string }) {
  return (
    <span className="inline-flex items-center px-1.5 py-0.5 text-[10px] font-mono rounded border border-slate-200 dark:border-slate-700 bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-400">
      {broker}
    </span>
  )
}

// ── Detail panel ──────────────────────────────────────────────────────────────

interface PublisherDetailPanelProps {
  publisher: OrphanPublisher
  onClose: () => void
}

function PublisherDetailPanel({ publisher, onClose }: PublisherDetailPanelProps) {
  return (
    <div className="w-80 flex-shrink-0 border-l border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-950 flex flex-col overflow-hidden">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-slate-200 dark:border-slate-800">
        <span className="text-sm font-semibold text-slate-800 dark:text-slate-200 truncate">
          Orphan Publisher
        </span>
        <button
          type="button"
          aria-label="Close detail panel"
          onClick={onClose}
          className="text-slate-400 hover:text-slate-700 dark:hover:text-slate-200 transition-colors focus:outline-none focus-visible:ring-1 focus-visible:ring-sky-500 rounded"
        >
          <X className="w-4 h-4" aria-hidden />
        </button>
      </div>

      {/* Body */}
      <div className="flex-1 overflow-y-auto px-4 py-4 space-y-4">
        {/* Topic */}
        <div>
          <p className="text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-500 mb-1">Topic / Queue</p>
          <p className="text-sm font-mono text-slate-800 dark:text-slate-200 break-all">{publisher.topic_label}</p>
        </div>

        {/* Broker */}
        <div>
          <p className="text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-500 mb-1">Broker</p>
          <BrokerBadge broker={publisher.broker} />
        </div>

        {/* Service */}
        <div>
          <p className="text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-500 mb-1">Service</p>
          <p className="text-sm text-slate-700 dark:text-slate-300">{publisher.service}</p>
        </div>

        {/* Location */}
        {publisher.file && (
          <div>
            <p className="text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-500 mb-1">Producer location</p>
            <p className="text-xs font-mono text-slate-600 dark:text-slate-400 break-all">
              {publisher.file}{publisher.line ? `:${publisher.line}` : ''}
            </p>
          </div>
        )}

        {/* Producer count */}
        <div>
          <p className="text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-500 mb-1">Producers</p>
          <p className="text-sm tabular-nums text-slate-700 dark:text-slate-300">{publisher.producer_count}</p>
        </div>

        {/* Suggestions */}
        <div className="rounded-lg border border-amber-200 dark:border-amber-700/50 bg-amber-50 dark:bg-amber-900/20 p-3 space-y-2">
          <p className="text-xs font-semibold text-amber-800 dark:text-amber-300">Suggested actions</p>
          <ul className="text-xs text-amber-700 dark:text-amber-400 space-y-1 list-disc list-inside">
            <li>Add a consumer that subscribes to <span className="font-mono">{publisher.topic_label}</span></li>
            <li>Remove the publish call if this topic is no longer needed</li>
          </ul>
        </div>
      </div>
    </div>
  )
}

// ── Row ───────────────────────────────────────────────────────────────────────

interface PublisherRowProps {
  publisher: OrphanPublisher
  isSelected: boolean
  onSelect: (p: OrphanPublisher) => void
}

function PublisherRow({ publisher, isSelected, onSelect }: PublisherRowProps) {
  return (
    <div
      role="row"
      tabIndex={0}
      aria-selected={isSelected}
      onClick={() => onSelect(publisher)}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          onSelect(publisher)
        }
      }}
      className={[
        'group flex items-center gap-3 px-4 py-2.5 border-b border-slate-100 dark:border-slate-800',
        'hover:bg-slate-50 dark:hover:bg-slate-900/60 cursor-pointer',
        'focus:outline-none focus-visible:ring-1 focus-visible:ring-sky-500',
        isSelected ? 'bg-sky-50 dark:bg-sky-900/20' : '',
      ].join(' ')}
    >
      {/* Topic label */}
      <span className="flex-1 min-w-0 font-mono text-xs text-slate-700 dark:text-slate-200 truncate" title={publisher.topic_label}>
        {publisher.topic_label}
      </span>

      {/* Broker */}
      <BrokerBadge broker={publisher.broker} />

      {/* Service */}
      <span className="text-xs text-slate-400 dark:text-slate-500 flex-shrink-0 max-w-[120px] truncate text-right" title={publisher.service}>
        {publisher.service}
      </span>

      {/* Producer count */}
      <span className="text-xs tabular-nums text-slate-400 dark:text-slate-500 flex-shrink-0 w-10 text-right">
        {publisher.producer_count}p
      </span>

      {/* Arrow */}
      <ChevronRight className="w-3.5 h-3.5 text-slate-300 dark:text-slate-600 group-hover:text-sky-400 flex-shrink-0 transition-colors" aria-hidden />
    </div>
  )
}

// ── Empty state ───────────────────────────────────────────────────────────────

function EmptyOrphanPublishers() {
  return (
    <div className="flex flex-col items-center justify-center py-16 gap-3 text-center px-6">
      <AlertTriangle className="w-8 h-8 text-emerald-300 dark:text-emerald-700" aria-hidden />
      <p className="text-sm text-slate-500 dark:text-slate-400 font-medium">
        No orphan publishers found
      </p>
      <p className="text-xs text-slate-400 dark:text-slate-500 max-w-xs">
        All your producers and consumers are properly paired — no orphans found.
      </p>
    </div>
  )
}

// ── Main export ───────────────────────────────────────────────────────────────

interface OrphanPublishersTabProps {
  publishers: OrphanPublisher[]
  isLoading: boolean
}

export function OrphanPublishersTab({ publishers, isLoading }: OrphanPublishersTabProps) {
  const [selected, setSelected] = useState<OrphanPublisher | null>(null)

  const handleSelect = (p: OrphanPublisher) => {
    setSelected((prev) => (prev?.id === p.id ? null : p))
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-16 text-slate-400 dark:text-slate-500 gap-2">
        <Loader2 className="w-4 h-4 animate-spin" aria-hidden />
        <span className="text-sm">Loading orphan publishers…</span>
      </div>
    )
  }

  if (publishers.length === 0) {
    return <EmptyOrphanPublishers />
  }

  return (
    <div className="flex flex-1 overflow-hidden">
      {/* List */}
      <div className="flex flex-col flex-1 overflow-hidden">
        {/* Column header */}
        <div className="flex items-center gap-3 px-4 py-1.5 border-b border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-900/50 text-[10px] text-slate-400 dark:text-slate-500 uppercase tracking-wide flex-shrink-0">
          <span className="flex-1">Topic / Queue</span>
          <span>Broker</span>
          <span className="w-[120px] text-right">Service</span>
          <span className="w-10 text-right">Prod</span>
          <span className="w-3.5" />
        </div>

        <div
          role="grid"
          aria-label="Orphan publisher list"
          className="flex-1 overflow-y-auto"
        >
          {publishers.map((pub) => (
            <PublisherRow
              key={pub.id}
              publisher={pub}
              isSelected={selected?.id === pub.id}
              onSelect={handleSelect}
            />
          ))}
        </div>
      </div>

      {/* Detail panel */}
      {selected && (
        <PublisherDetailPanel
          publisher={selected}
          onClose={() => setSelected(null)}
        />
      )}
    </div>
  )
}
