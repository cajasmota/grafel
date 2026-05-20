import { useState } from 'react'
import { useParams } from 'react-router-dom'
import {
  Webhook, Lock, LockOpen, ChevronDown, ChevronRight,
  ArrowDownLeft, ArrowUpRight, Database, Zap, TestTube2,
  FileCode2, Info, ExternalLink,
} from 'lucide-react'
import { usePathDetail } from '@/hooks/paths/usePathDetail'
import { VerbChip } from '@/components/shared/VerbChip'
import { KindBadge } from '@/components/shared/KindBadge'
import { RepoChip } from '@/components/shared/RepoChip'
import { EmptyState } from '@/components/shared/EmptyState'
import { CardSkeleton } from '@/components/shared/LoadingState'
import { MultiImplBanner } from './MultiImplBanner'
import { splitPathParts } from '@/lib/pathUtils'
import type { Entity, HandlerDetail, HttpVerb, ResponseShape } from '@/types/api'

interface PathDetailPageProps {
  group: string
}

// ─── Collapsible section wrapper ─────────────────────────────────────────────

interface SectionProps {
  id: string
  title: string
  icon: React.ReactNode
  count?: number
  defaultOpen?: boolean
  children: React.ReactNode
}

function Section({ id, title, icon, count, defaultOpen = true, children }: SectionProps) {
  const [open, setOpen] = useState(defaultOpen)
  return (
    <section aria-labelledby={`section-${id}`} className="border-b border-slate-200 dark:border-slate-800 last:border-b-0">
      <button
        type="button"
        id={`section-${id}`}
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className="w-full flex items-center gap-2 px-6 py-3 text-sm font-semibold text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-900/50 transition-colors focus:outline-none focus-visible:ring-1 focus-visible:ring-sky-500"
      >
        <span className="text-slate-400 dark:text-slate-500 flex-shrink-0">{icon}</span>
        <span className="flex-1 text-left">{title}</span>
        {count !== undefined && count > 0 && (
          <span className="inline-flex items-center justify-center min-w-[20px] px-1.5 py-0.5 text-[10px] rounded-full bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400">
            {count}
          </span>
        )}
        {open
          ? <ChevronDown className="w-4 h-4 text-slate-400 flex-shrink-0" aria-hidden />
          : <ChevronRight className="w-4 h-4 text-slate-400 flex-shrink-0" aria-hidden />
        }
      </button>
      {open && <div className="px-6 pb-5 pt-1">{children}</div>}
    </section>
  )
}

// ─── AI Summary section ───────────────────────────────────────────────────────

function AISummarySection({ handlers }: { handlers: HandlerDetail[] }) {
  // Use the first handler that has docs; otherwise show the hint.
  const withDocs = handlers.find((h) => h.has_docs && h.docs_summary)

  return (
    <Section id="description" title="Description" icon={<Info className="w-4 h-4" />} defaultOpen>
      {withDocs ? (
        <p className="text-sm text-slate-700 dark:text-slate-300 leading-relaxed">
          {withDocs.docs_summary}
        </p>
      ) : (
        <div className="flex items-start gap-2 px-3 py-2.5 rounded-lg bg-slate-100 dark:bg-slate-900 border border-slate-200 dark:border-slate-800">
          <Info className="w-4 h-4 text-slate-400 flex-shrink-0 mt-0.5" aria-hidden />
          <p className="text-sm text-slate-400 dark:text-slate-500 italic">
            No documentation generated yet. Run{' '}
            <code className="font-mono text-xs text-sky-500 dark:text-sky-400">/generate-docs</code>{' '}
            in Claude Code to enable AI summaries.
          </p>
        </div>
      )}
    </Section>
  )
}

// ─── Parameters table ─────────────────────────────────────────────────────────

interface Param {
  name: string
  location: 'path' | 'query' | 'body'
  type: string
}

function parseParams(handlers: HandlerDetail[]): Param[] {
  const seen = new Set<string>()
  const params: Param[] = []
  for (const h of handlers) {
    const raw = (h.entity.properties?.parameters as string | undefined) ?? ''
    if (!raw) continue
    for (const segment of raw.split(',')) {
      const parts = segment.trim().split(':')
      if (parts.length < 2) continue
      const [name, location, type = 'string'] = parts
      const key = `${name}:${location}`
      if (!seen.has(key)) {
        seen.add(key)
        params.push({ name, location: location as Param['location'], type })
      }
    }
  }
  return params
}

const LOCATION_BADGE: Record<Param['location'], string> = {
  path:  'bg-violet-900/30 text-violet-300 border-violet-700',
  query: 'bg-sky-900/30 text-sky-300 border-sky-700',
  body:  'bg-teal-900/30 text-teal-300 border-teal-700',
}

function ParametersSection({ handlers }: { handlers: HandlerDetail[] }) {
  const params = parseParams(handlers)
  return (
    <Section id="parameters" title="Parameters" icon={<FileCode2 className="w-4 h-4" />} count={params.length} defaultOpen>
      {params.length === 0 ? (
        <p className="text-xs text-slate-400 dark:text-slate-500 italic">No parameters extracted.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm border-collapse">
            <thead>
              <tr className="text-left text-xs text-slate-400 dark:text-slate-500 border-b border-slate-200 dark:border-slate-800">
                <th className="py-1.5 pr-4 font-medium">Name</th>
                <th className="py-1.5 pr-4 font-medium">In</th>
                <th className="py-1.5 font-medium">Type</th>
              </tr>
            </thead>
            <tbody>
              {params.map((p) => (
                <tr
                  key={`${p.name}-${p.location}`}
                  className="border-b border-slate-100 dark:border-slate-900 last:border-b-0"
                >
                  <td className="py-2 pr-4 font-mono text-slate-800 dark:text-slate-200 text-xs">{p.name}</td>
                  <td className="py-2 pr-4">
                    <span className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-mono font-semibold border ${LOCATION_BADGE[p.location]}`}>
                      {p.location}
                    </span>
                  </td>
                  <td className="py-2 text-xs text-slate-500 dark:text-slate-400 font-mono">{p.type}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Section>
  )
}

// ─── Response shapes section ──────────────────────────────────────────────────

function statusCodeClass(code: number): string {
  if (code < 300) return 'bg-emerald-900/40 text-emerald-300 border-emerald-700'
  if (code < 400) return 'bg-yellow-900/40 text-yellow-300 border-yellow-700'
  return 'bg-red-900/40 text-red-300 border-red-700'
}

function ResponseShapeCard({ shape }: { shape: ResponseShape }) {
  const [expanded, setExpanded] = useState(false)
  const hasKeys = shape.keys.length > 0

  return (
    <div className="rounded-lg border border-slate-200 dark:border-slate-800 overflow-hidden">
      <button
        type="button"
        aria-expanded={expanded}
        onClick={() => setExpanded((v) => !v)}
        className="w-full flex items-center gap-2 px-3 py-2.5 bg-slate-50 dark:bg-slate-900/60 hover:bg-slate-100 dark:hover:bg-slate-900 transition-colors focus:outline-none focus-visible:ring-1 focus-visible:ring-sky-500"
      >
        <VerbChip verb={shape.verb} />
        <div className="flex gap-1 flex-wrap">
          {shape.status_codes.map((code) => (
            <span key={code} className={`px-1.5 py-0.5 rounded text-xs font-mono font-bold border ${statusCodeClass(code)}`}>
              {code}
            </span>
          ))}
        </div>
        {shape.dynamic && (
          <span className="ml-auto text-xs text-amber-400 italic">dynamic</span>
        )}
        {!shape.dynamic && (
          <span className="ml-auto text-xs text-slate-400">{shape.keys.length} fields</span>
        )}
        {expanded
          ? <ChevronDown className="w-3.5 h-3.5 text-slate-400 flex-shrink-0" aria-hidden />
          : <ChevronRight className="w-3.5 h-3.5 text-slate-400 flex-shrink-0" aria-hidden />
        }
      </button>
      {expanded && (
        <div className="px-3 py-3 border-t border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-950">
          {hasKeys ? (
            <div className="flex flex-wrap gap-1.5">
              {shape.keys.map((key) => (
                <code
                  key={key}
                  className="px-2 py-0.5 rounded bg-slate-100 dark:bg-slate-800 text-slate-700 dark:text-slate-300 text-xs font-mono border border-slate-300 dark:border-slate-700"
                >
                  {key}
                </code>
              ))}
            </div>
          ) : (
            <p className="text-xs text-slate-400 dark:text-slate-500 italic">
              {shape.dynamic ? 'Response shape determined at runtime.' : 'Empty response body.'}
            </p>
          )}
        </div>
      )}
    </div>
  )
}

function ResponseShapesSection({ shapes }: { shapes: ResponseShape[] }) {
  return (
    <Section id="responses" title="Response shapes" icon={<ArrowUpRight className="w-4 h-4" />} count={shapes.length} defaultOpen>
      {shapes.length === 0 ? (
        <p className="text-xs text-slate-400 dark:text-slate-500 italic">Response shapes could not be statically determined.</p>
      ) : (
        <div className="space-y-2">
          {shapes.map((shape, i) => <ResponseShapeCard key={i} shape={shape} />)}
        </div>
      )}
    </Section>
  )
}

// ─── Called-by section ────────────────────────────────────────────────────────

function CalledBySection({ entities }: { entities: Entity[] }) {
  return (
    <Section
      id="called-by"
      title="Called by"
      icon={<ArrowDownLeft className="w-4 h-4" />}
      count={entities.length}
      defaultOpen={entities.length > 0}
    >
      {entities.length === 0 ? (
        <p className="text-xs text-slate-400 dark:text-slate-500 italic">No inbound callers detected.</p>
      ) : (
        <ul className="space-y-1.5" role="list">
          {entities.map((e) => (
            <li
              key={e.id}
              className="flex items-center gap-2.5 px-3 py-2 rounded-lg bg-slate-50 dark:bg-slate-900 border border-slate-200 dark:border-slate-800"
            >
              <KindBadge kind={e.kind} />
              <span className="font-mono text-xs text-slate-700 dark:text-slate-300 flex-1 min-w-0 truncate" title={e.label}>
                {e.label}
              </span>
              <span className="font-mono text-[10px] text-slate-400 dark:text-slate-500 flex-shrink-0 hidden sm:block">
                {e.source_file}:{e.start_line}
              </span>
              <RepoChip repo={e.repo} />
              <a
                href={`#entity-${e.id}`}
                title={`Inspect ${e.label}`}
                className="flex-shrink-0 text-sky-400 hover:text-sky-300 transition-colors"
                aria-label={`Inspect ${e.label}`}
              >
                <ExternalLink className="w-3.5 h-3.5" />
              </a>
            </li>
          ))}
        </ul>
      )}
    </Section>
  )
}

// ─── Downstream section ───────────────────────────────────────────────────────

type DownstreamKind = 'db' | 'event' | 'queue' | 'http' | 'grpc' | 'other'

function classifyDownstream(e: Entity): DownstreamKind {
  const k = e.kind
  if (k === 'DataAccess' || k === 'Datastore') return 'db'
  if (k === 'Event' || k === 'MessageTopic') return 'event'
  if (k === 'Queue') return 'queue'
  if (k === 'ExternalAPI') return 'http'
  if (k === 'Service') return 'grpc'
  return 'other'
}

const DOWNSTREAM_ICONS: Record<DownstreamKind, React.ReactNode> = {
  db:    <Database className="w-3.5 h-3.5 text-amber-400" aria-hidden />,
  event: <Zap className="w-3.5 h-3.5 text-pink-400" aria-hidden />,
  queue: <ArrowUpRight className="w-3.5 h-3.5 text-rose-400" aria-hidden />,
  http:  <ExternalLink className="w-3.5 h-3.5 text-sky-400" aria-hidden />,
  grpc:  <ArrowUpRight className="w-3.5 h-3.5 text-indigo-400" aria-hidden />,
  other: <ArrowUpRight className="w-3.5 h-3.5 text-slate-400" aria-hidden />,
}

const DOWNSTREAM_LABELS: Record<DownstreamKind, string> = {
  db:    'DB',
  event: 'Event',
  queue: 'Queue',
  http:  'HTTP',
  grpc:  'gRPC',
  other: '',
}

function DownstreamSection({ entities }: { entities: Entity[] }) {
  // Group by kind
  const groups = entities.reduce<Record<DownstreamKind, Entity[]>>((acc, e) => {
    const dk = classifyDownstream(e)
    if (!acc[dk]) acc[dk] = []
    acc[dk].push(e)
    return acc
  }, {} as Record<DownstreamKind, Entity[]>)

  const order: DownstreamKind[] = ['db', 'queue', 'event', 'http', 'grpc', 'other']

  return (
    <Section
      id="downstream"
      title="Downstream"
      icon={<ArrowUpRight className="w-4 h-4" />}
      count={entities.length}
      defaultOpen={entities.length > 0}
    >
      {entities.length === 0 ? (
        <p className="text-xs text-slate-400 dark:text-slate-500 italic">No downstream dependencies detected.</p>
      ) : (
        <div className="space-y-3">
          {order.filter((dk) => (groups[dk] ?? []).length > 0).map((dk) => (
            <div key={dk}>
              {DOWNSTREAM_LABELS[dk] && (
                <div className="flex items-center gap-1.5 mb-1.5 text-xs font-semibold text-slate-400 dark:text-slate-500 uppercase tracking-wide">
                  {DOWNSTREAM_ICONS[dk]}
                  {DOWNSTREAM_LABELS[dk]}
                </div>
              )}
              <ul className="space-y-1" role="list">
                {(groups[dk] ?? []).map((e) => (
                  <li
                    key={e.id}
                    className="flex items-center gap-2.5 px-3 py-2 rounded-lg bg-slate-50 dark:bg-slate-900 border border-slate-200 dark:border-slate-800"
                  >
                    {DOWNSTREAM_ICONS[dk]}
                    <span className="font-mono text-xs text-slate-700 dark:text-slate-300 flex-1 min-w-0 truncate" title={e.label}>
                      {e.label}
                    </span>
                    <span className="font-mono text-[10px] text-slate-400 dark:text-slate-500 flex-shrink-0 hidden sm:block">
                      {e.source_file}:{e.start_line}
                    </span>
                    <RepoChip repo={e.repo} />
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
      )}
    </Section>
  )
}

// ─── Side-effects section (Events emitted) ────────────────────────────────────

function SideEffectsSection({ entities }: { entities: Entity[] }) {
  const events = entities.filter((e) => e.kind === 'Event' || e.kind === 'MessageTopic')
  return (
    <Section
      id="side-effects"
      title="Side effects"
      icon={<Zap className="w-4 h-4" />}
      count={events.length}
      defaultOpen={false}
    >
      {events.length === 0 ? (
        <p className="text-xs text-slate-400 dark:text-slate-500 italic">No events emitted.</p>
      ) : (
        <ul className="space-y-1.5" role="list">
          {events.map((e) => (
            <li
              key={e.id}
              className="flex items-center gap-2.5 px-3 py-2 rounded-lg bg-slate-50 dark:bg-slate-900 border border-slate-200 dark:border-slate-800"
            >
              <Zap className="w-3.5 h-3.5 text-pink-400 flex-shrink-0" aria-hidden />
              <span className="font-mono text-xs text-slate-700 dark:text-slate-300 flex-1 min-w-0 truncate" title={e.label}>
                {e.label}
              </span>
              <RepoChip repo={e.repo} />
            </li>
          ))}
        </ul>
      )}
    </Section>
  )
}

// ─── Tests section ────────────────────────────────────────────────────────────

function TestsSection({ entities }: { entities: Entity[] }) {
  const tests = entities.filter((e) => {
    const lbl = e.label.toLowerCase()
    const file = e.source_file.toLowerCase()
    return lbl.startsWith('test') || file.includes('test') || file.includes('spec') || e.kind === 'Function' && (lbl.includes('_test') || lbl.includes('test_'))
  })

  return (
    <Section
      id="tests"
      title="Tests"
      icon={<TestTube2 className="w-4 h-4" />}
      count={tests.length}
      defaultOpen={false}
    >
      {tests.length === 0 ? (
        <p className="text-xs text-slate-400 dark:text-slate-500 italic">No test coverage detected via TESTS edges.</p>
      ) : (
        <ul className="space-y-1.5" role="list">
          {tests.map((e) => (
            <li
              key={e.id}
              className="flex items-center gap-2.5 px-3 py-2 rounded-lg bg-slate-50 dark:bg-slate-900 border border-slate-200 dark:border-slate-800"
            >
              <TestTube2 className="w-3.5 h-3.5 text-emerald-400 flex-shrink-0" aria-hidden />
              <span className="font-mono text-xs text-slate-700 dark:text-slate-300 flex-1 min-w-0 truncate" title={e.label}>
                {e.label}
              </span>
              <span className="font-mono text-[10px] text-slate-400 dark:text-slate-500 hidden sm:block">
                {e.source_file}:{e.start_line}
              </span>
              <RepoChip repo={e.repo} />
            </li>
          ))}
        </ul>
      )}
    </Section>
  )
}

// ─── Defined-in section (backend handler definitions only) ───────────────────

function DefinedInSection({ handlers }: { handlers: HandlerDetail[] }) {
  return (
    <Section id="defined-in" title="Defined in" icon={<FileCode2 className="w-4 h-4" />} count={handlers.length} defaultOpen>
      {handlers.length === 0 ? (
        <div className="flex items-start gap-2 px-3 py-2.5 rounded-lg bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800">
          <Info className="w-4 h-4 text-amber-500 flex-shrink-0 mt-0.5" aria-hidden />
          <p className="text-sm text-amber-700 dark:text-amber-300">
            No backend handler found — this endpoint is an orphan call.
          </p>
        </div>
      ) : (
        <ul className="space-y-2" role="list">
          {handlers.map((h, i) => (
            <li
              key={i}
              className="flex items-center gap-2.5 px-3 py-2 rounded-lg bg-slate-50 dark:bg-slate-900 border border-slate-200 dark:border-slate-800"
            >
              <VerbChip verb={h.verb} />
              <span
                className="font-mono text-xs text-slate-700 dark:text-slate-300 flex-1 min-w-0 truncate"
                title={`${h.source_file}:${h.start_line}`}
              >
                {h.source_file}
                <span className="text-slate-400 dark:text-slate-500">:{h.start_line}</span>
              </span>
              {h.framework && (
                <span className="text-[10px] text-slate-400 dark:text-slate-500 font-mono hidden sm:block">{h.framework}</span>
              )}
              <RepoChip repo={h.entity.repo} />
            </li>
          ))}
        </ul>
      )}
    </Section>
  )
}

// ─── Auth badge helpers ───────────────────────────────────────────────────────

function extractAuth(handlers: HandlerDetail[]): string | null {
  for (const h of handlers) {
    const auth = h.entity.properties?.auth as string | undefined
    if (auth) return auth
  }
  return null
}

// ─── Main component ───────────────────────────────────────────────────────────

export function PathDetailPage({ group }: PathDetailPageProps) {
  const { pathHash } = useParams<{ pathHash: string }>()
  const { data, isLoading, error } = usePathDetail(group, pathHash ?? null)

  if (isLoading) return <div className="p-6 space-y-4"><CardSkeleton /><CardSkeleton /></div>

  if (error || !data) {
    return (
      <EmptyState
        title="Path not found"
        message={error?.message ?? 'Could not load path detail.'}
      />
    )
  }

  const pathParts = splitPathParts(data.path)
  const authInfo = extractAuth(data.handlers)

  // Split outbound_queries into downstream + events + tests for display
  const downstreamEntities = data.outbound_queries.filter(
    (e) => e.kind !== 'Event' && e.kind !== 'MessageTopic' && !isTestEntity(e),
  )
  const sideEffectEntities = data.outbound_queries.filter(
    (e) => e.kind === 'Event' || e.kind === 'MessageTopic',
  )
  const testEntities = data.outbound_queries.filter(isTestEntity)

  return (
    <div className="flex flex-col h-full overflow-hidden">

      {/* ── Header ────────────────────────────────────────────────────────── */}
      <div className="px-6 py-4 border-b border-slate-200 dark:border-slate-800 bg-slate-100/80 dark:bg-slate-900/80 flex-shrink-0">
        <div className="flex items-start gap-3 flex-wrap">
          {/* Verb chips */}
          <div className="flex items-center gap-1.5 flex-shrink-0">
            {(data.verbs as HttpVerb[]).map((v) => <VerbChip key={v} verb={v} />)}
          </div>

          {/* Path */}
          <h1 className="font-mono text-sm font-semibold text-slate-800 dark:text-slate-200 flex-1 min-w-0 mt-0.5">
            {pathParts.map((part, i) => (
              <span key={i} className={part.isDynamic ? 'text-amber-400' : ''}>
                {part.text}
              </span>
            ))}
          </h1>

          {/* Badges: webhook, auth */}
          <div className="flex items-center gap-1.5 flex-shrink-0">
            {data.is_webhook && (
              <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs bg-violet-900/40 text-violet-300 border border-violet-700">
                <Webhook className="w-3 h-3" aria-hidden />
                {data.webhook_provider ?? 'webhook'}
              </span>
            )}
            {authInfo ? (
              <span
                title={`Auth: ${authInfo}`}
                className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs bg-emerald-900/30 text-emerald-300 border border-emerald-800"
                aria-label={`Auth required: ${authInfo}`}
              >
                <Lock className="w-3 h-3" aria-hidden />
                Auth
              </span>
            ) : (
              <span
                className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs bg-slate-800/40 text-slate-500 border border-slate-700"
                aria-label="No auth decorator detected"
              >
                <LockOpen className="w-3 h-3" aria-hidden />
                No auth
              </span>
            )}
          </div>
        </div>

        {/* Repo pills */}
        <div className="mt-2 flex items-center gap-1.5 flex-wrap">
          {Array.from(new Set(data.handlers.map((h) => h.entity.repo))).map((repo) => (
            <RepoChip key={repo} repo={repo} />
          ))}
          <span className="text-[10px] font-mono text-slate-400 dark:text-slate-600 ml-1 hidden sm:block">
            {data.path_hash}
          </span>
        </div>
      </div>

      {/* Multi-impl banner */}
      <MultiImplBanner count={data.handlers.length} path={data.path} />

      {/* ── Scrollable body ───────────────────────────────────────────────── */}
      <div className="flex-1 overflow-y-auto divide-y-0">

        {/* Description / AI Summary */}
        <AISummarySection handlers={data.handlers} />

        {/* Parameters */}
        <ParametersSection handlers={data.handlers} />

        {/* Response shapes */}
        <ResponseShapesSection shapes={data.response_shapes} />

        {/* Defined in — backend handler definitions only */}
        <DefinedInSection handlers={data.handlers} />

        {/* Called by — frontend / service call sites that FETCHES this path */}
        <CalledBySection entities={data.inbound_fetches} />

        {/* Downstream */}
        <DownstreamSection entities={downstreamEntities} />

        {/* Side effects */}
        <SideEffectsSection entities={sideEffectEntities} />

        {/* Tests */}
        <TestsSection entities={testEntities} />

      </div>
    </div>
  )
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

function isTestEntity(e: Entity): boolean {
  const lbl = e.label.toLowerCase()
  const file = e.source_file.toLowerCase()
  return (
    lbl.startsWith('test') ||
    lbl.includes('_test') ||
    lbl.includes('test_') ||
    file.includes('/test') ||
    file.includes('/spec')
  )
}
