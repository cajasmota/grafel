import type { RelationshipKind } from '@/types/api'

interface EdgeBadgeProps {
  kind: RelationshipKind
  crossRepo?: boolean
  className?: string
}

// Color map for relationship kinds — grouped by category
const EDGE_KIND_COLORS: Partial<Record<RelationshipKind, string>> = {
  // Call / dependency
  CALLS:          'bg-sky-900/50 text-sky-300 border-sky-800',
  IMPORTS:        'bg-indigo-900/50 text-indigo-300 border-indigo-800',
  DEPENDS_ON:     'bg-blue-900/50 text-blue-300 border-blue-800',
  USES:           'bg-slate-200/60 dark:bg-slate-800/60 text-slate-700 dark:text-slate-300 border-slate-300 dark:border-slate-700',
  USES_HOOK:      'bg-slate-200/60 dark:bg-slate-800/60 text-slate-700 dark:text-slate-300 border-slate-300 dark:border-slate-700',
  CONTAINS:       'bg-slate-200/60 dark:bg-slate-800/60 text-slate-400 dark:text-slate-400 border-slate-300 dark:border-slate-700',
  // Data
  QUERIES:        'bg-amber-900/50 text-amber-300 border-amber-800',
  FETCHES:        'bg-yellow-900/50 text-yellow-300 border-yellow-800',
  READS_FROM:     'bg-teal-900/50 text-teal-300 border-teal-800',
  WRITES_TO:      'bg-orange-900/50 text-orange-300 border-orange-800',
  ACCESSES_TABLE: 'bg-amber-900/50 text-amber-300 border-amber-800',
  // Messaging
  PUBLISHES_TO:   'bg-rose-900/50 text-rose-300 border-rose-800',
  SUBSCRIBES_TO:  'bg-pink-900/50 text-pink-300 border-pink-800',
  TRANSFORMS:     'bg-fuchsia-900/50 text-fuchsia-300 border-fuchsia-800',
  // WebSocket
  WS_SUBSCRIBES_TO: 'bg-violet-900/50 text-violet-300 border-violet-800',
  WS_EMITS:       'bg-purple-900/50 text-purple-300 border-purple-800',
  WS_CONNECTS:    'bg-violet-900/50 text-violet-300 border-violet-800',
  // GraphQL
  GRAPHQL_SUBSCRIBES: 'bg-pink-900/50 text-pink-300 border-pink-800',
  GRAPHQL_PUBLISHES:  'bg-rose-900/50 text-rose-300 border-rose-800',
  // OOP
  EXTENDS:        'bg-cyan-900/50 text-cyan-300 border-cyan-800',
  IMPLEMENTS:     'bg-teal-900/50 text-teal-300 border-teal-800',
  RETURNS:        'bg-slate-200/60 dark:bg-slate-800/60 text-slate-400 dark:text-slate-400 border-slate-300 dark:border-slate-700',
  // HTTP
  ROUTES_TO:      'bg-emerald-900/50 text-emerald-300 border-emerald-800',
  SERVES:         'bg-green-900/50 text-green-300 border-green-800',
}

const EDGE_DEFAULT = 'bg-slate-200/50 dark:bg-slate-800/50 text-slate-400 dark:text-slate-400 border-slate-300 dark:border-slate-700'

export function EdgeBadge({ kind, crossRepo, className = '' }: EdgeBadgeProps) {
  const colorClass = EDGE_KIND_COLORS[kind] ?? EDGE_DEFAULT
  return (
    <span
      className={[
        'inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-mono font-semibold border',
        colorClass,
        className,
      ].join(' ')}
      title={crossRepo ? `${kind} (cross-repo)` : kind}
    >
      {crossRepo && <span className="w-1 h-1 rounded-full bg-current opacity-70" aria-hidden />}
      {kind}
    </span>
  )
}

/**
 * Returns the CSS hex color for a relationship kind (for canvas rendering).
 */
export function edgeKindColor(kind: RelationshipKind): string {
  const CLASS_TO_HEX: Record<string, string> = {
    CALLS: '#38bdf8', IMPORTS: '#818cf8', DEPENDS_ON: '#60a5fa',
    QUERIES: '#fbbf24', FETCHES: '#facc15', READS_FROM: '#2dd4bf',
    WRITES_TO: '#fb923c', PUBLISHES_TO: '#fb7185', SUBSCRIBES_TO: '#f472b6',
    TRANSFORMS: '#e879f9', WS_SUBSCRIBES_TO: '#a78bfa', WS_EMITS: '#c084fc',
    GRAPHQL_SUBSCRIBES: '#f472b6', GRAPHQL_PUBLISHES: '#fb7185',
    EXTENDS: '#22d3ee', IMPLEMENTS: '#2dd4bf',
    ROUTES_TO: '#34d399', SERVES: '#4ade80',
  }
  return CLASS_TO_HEX[kind] ?? '#64748b'
}
