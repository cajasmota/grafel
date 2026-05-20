import { useParams } from 'react-router-dom'
import * as Tabs from '@radix-ui/react-tabs'
import { ExternalLink, Webhook, Database, ArrowDownLeft, ArrowUpRight } from 'lucide-react'
import { usePathDetail } from '@/hooks/paths/usePathDetail'
import { VerbChip } from '@/components/shared/VerbChip'
import { KindBadge } from '@/components/shared/KindBadge'
import { RepoChip } from '@/components/shared/RepoChip'
import { EmptyState } from '@/components/shared/EmptyState'
import { CardSkeleton } from '@/components/shared/LoadingState'
import { MultiImplBanner } from './MultiImplBanner'
import { splitPathParts } from '@/lib/pathUtils'
import type { HandlerDetail, ResponseShape } from '@/types/api'

interface PathDetailPageProps {
  group: string
}

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

  return (
    <div className="flex flex-col h-full overflow-hidden">
      {/* Header */}
      <div className="px-6 py-4 border-b border-slate-200 dark:border-slate-800 bg-slate-100/80 dark:bg-slate-900/80">
        <div className="flex items-start gap-3 flex-wrap">
          <h1 className="font-mono text-base font-semibold text-slate-800 dark:text-slate-200 flex-1 min-w-0">
            {pathParts.map((part, i) => (
              <span key={i} className={part.isDynamic ? 'text-amber-400' : ''}>
                {part.text}
              </span>
            ))}
          </h1>
          <div className="flex items-center gap-2 flex-shrink-0">
            {data.verbs.map((v) => <VerbChip key={v} verb={v} />)}
            {data.is_webhook && (
              <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs bg-violet-900/40 text-violet-300 border border-violet-700">
                <Webhook className="w-3 h-3" />
                {data.webhook_provider ?? 'webhook'}
              </span>
            )}
          </div>
        </div>
        <div className="mt-1 text-xs text-slate-400 dark:text-slate-500 font-mono">{data.path_hash}</div>
      </div>

      {/* Multi-impl banner */}
      <MultiImplBanner count={data.handlers.length} path={data.path} />

      {/* Tabs */}
      <Tabs.Root defaultValue="handlers" className="flex flex-col flex-1 overflow-hidden">
        <Tabs.List
          className="flex border-b border-slate-200 dark:border-slate-800 px-6 bg-slate-100/60 dark:bg-slate-900/60"
          aria-label="Path detail sections"
        >
          <TabTrigger value="handlers">
            Handlers ({data.handlers.length})
          </TabTrigger>
          <TabTrigger value="responses">
            Response shapes ({data.response_shapes.length})
          </TabTrigger>
          <TabTrigger value="inbound">
            Inbound ({data.inbound_fetches.length})
          </TabTrigger>
          <TabTrigger value="outbound">
            Outbound ({data.outbound_queries.length})
          </TabTrigger>
        </Tabs.List>

        <div className="flex-1 overflow-y-auto">
          {/* Handlers tab */}
          <Tabs.Content value="handlers" className="p-6 space-y-4">
            {data.handlers.length === 0 ? (
              <EmptyState title="No handlers found" />
            ) : (
              data.handlers.map((h, i) => <HandlerCard key={i} handler={h} />)
            )}
          </Tabs.Content>

          {/* Response shapes tab */}
          <Tabs.Content value="responses" className="p-6 space-y-4">
            {data.response_shapes.length === 0 ? (
              <EmptyState title="No response shapes extracted" message="Response shapes could not be statically determined." />
            ) : (
              data.response_shapes.map((shape, i) => <ResponseShapeCard key={i} shape={shape} />)
            )}
          </Tabs.Content>

          {/* Inbound fetches tab */}
          <Tabs.Content value="inbound" className="p-6">
            {data.inbound_fetches.length === 0 ? (
              <EmptyState
                icon={ArrowDownLeft}
                title="No inbound fetches"
                message="No other services or functions were detected calling this path."
              />
            ) : (
              <ul className="space-y-2">
                {data.inbound_fetches.map((e) => (
                  <li key={e.id} className="flex items-center gap-3 px-3 py-2 rounded-lg bg-slate-100 dark:bg-slate-900 border border-slate-200 dark:border-slate-800">
                    <KindBadge kind={e.kind} />
                    <span className="font-mono text-sm text-slate-700 dark:text-slate-300 flex-1 truncate">{e.label}</span>
                    <RepoChip repo={e.repo} />
                    <a
                      href={`#entity-${e.id}`}
                      className="text-xs text-sky-400 hover:underline flex items-center gap-0.5"
                    >
                      <ExternalLink className="w-3 h-3" />
                      Inspect
                    </a>
                  </li>
                ))}
              </ul>
            )}
          </Tabs.Content>

          {/* Outbound queries tab */}
          <Tabs.Content value="outbound" className="p-6">
            {data.outbound_queries.length === 0 ? (
              <EmptyState
                icon={ArrowUpRight}
                title="No outbound queries"
                message="No database queries were detected from this handler."
              />
            ) : (
              <ul className="space-y-2">
                {data.outbound_queries.map((e) => (
                  <li key={e.id} className="flex items-center gap-3 px-3 py-2 rounded-lg bg-slate-100 dark:bg-slate-900 border border-slate-200 dark:border-slate-800">
                    <Database className="w-4 h-4 text-amber-400" aria-hidden />
                    <span className="font-mono text-sm text-slate-700 dark:text-slate-300 flex-1 truncate">{e.label}</span>
                    <RepoChip repo={e.repo} />
                  </li>
                ))}
              </ul>
            )}
          </Tabs.Content>
        </div>
      </Tabs.Root>
    </div>
  )
}

function TabTrigger({ value, children }: { value: string; children: React.ReactNode }) {
  return (
    <Tabs.Trigger
      value={value}
      className={[
        'px-4 py-2.5 text-sm border-b-2 border-transparent transition-colors',
        'text-slate-400 dark:text-slate-500 hover:text-slate-700 dark:hover:text-slate-300',
        'data-[state=active]:border-sky-500 data-[state=active]:text-slate-800 dark:data-[state=active]:text-slate-200',
        'focus:outline-none focus-visible:ring-1 focus-visible:ring-sky-500',
      ].join(' ')}
    >
      {children}
    </Tabs.Trigger>
  )
}

function HandlerCard({ handler }: { handler: HandlerDetail }) {
  return (
    <div className="rounded-lg border border-slate-200 dark:border-slate-800 bg-slate-100 dark:bg-slate-900 overflow-hidden">
      <div className="flex items-center gap-3 px-4 py-3 border-b border-slate-200 dark:border-slate-800">
        <VerbChip verb={handler.verb} />
        <KindBadge kind={handler.entity.kind} />
        <span className="font-mono text-sm text-slate-700 dark:text-slate-300 flex-1 truncate">
          {handler.entity.label}
        </span>
        <RepoChip repo={handler.entity.repo} />
      </div>
      <div className="px-4 py-2 text-xs text-slate-400 dark:text-slate-500 font-mono">
        {handler.source_file}:{handler.start_line}
      </div>
    </div>
  )
}

function ResponseShapeCard({ shape }: { shape: ResponseShape }) {
  return (
    <div className="rounded-lg border border-slate-200 dark:border-slate-800 bg-slate-100 dark:bg-slate-900 p-4">
      <div className="flex items-center gap-2 mb-3">
        <VerbChip verb={shape.verb} />
        <div className="flex gap-1">
          {shape.status_codes.map((code) => (
            <span
              key={code}
              className={[
                'px-1.5 py-0.5 rounded text-xs font-mono font-bold',
                code < 300
                  ? 'bg-emerald-900/40 text-emerald-300'
                  : code < 400
                    ? 'bg-yellow-900/40 text-yellow-300'
                    : 'bg-red-900/40 text-red-300',
              ].join(' ')}
            >
              {code}
            </span>
          ))}
        </div>
        {shape.dynamic && (
          <span className="ml-auto text-xs text-amber-400 italic">dynamic response</span>
        )}
      </div>
      {shape.keys.length > 0 ? (
        <div className="flex flex-wrap gap-1.5">
          {shape.keys.map((key) => (
            <code
              key={key}
              className="px-2 py-0.5 rounded bg-slate-200 dark:bg-slate-800 text-slate-700 dark:text-slate-300 text-xs font-mono border border-slate-300 dark:border-slate-700"
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
  )
}
