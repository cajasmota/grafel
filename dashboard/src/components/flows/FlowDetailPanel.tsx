/**
 * Full-chain detail panel, opened when a flow row is clicked (#1150, #1152).
 *
 * Additions over original:
 *  - React Flow DAG tab (lazy-loaded @xyflow/react chunk)
 *  - Step-kind badges in Chain tab
 *  - Side-effects summary bar
 *  - Cross-repo step distinction
 *  - AI Summary + preconditions / expected outcome (collapsible)
 *  - Side-effects sidebar (writes_db_table, publishes_to, external_calls)
 *  - Data lineage section (read_source → write_sink)
 *  - Linked endpoint (entry_kind=http_handler) → Paths cross-link
 *  - Linked topic (entry_kind=message_consumer) → Topology cross-link
 *  - Complexity score badge
 *  - Print-friendly button
 *  - AI-generated enrichment insights (#1152)
 *
 * Jarvis hook reserve: useGraphHighlight from #1153 applied here for MCP-driven
 * step highlight in a future story (#1157). The prop is wired but no-op for now.
 */
import { X, ArrowLeftRight, Link, BookOpen, Printer, ChevronDown, ChevronUp, Globe, Database, Sparkles, RefreshCw } from 'lucide-react'
import * as Tabs from '@radix-ui/react-tabs'
import { RepoChip } from '@/components/shared/RepoChip'
import { CrossStackBadge } from './CrossStackBadge'
import { SwimLanePanel } from './SwimLanePanel'
import { ChainStepDetail } from './ChainStepDetail'
import { EmptyState } from '@/components/shared/EmptyState'
import { CardSkeleton } from '@/components/shared/LoadingState'
import { useFlowDetail } from '@/hooks/flows/useFlowDetail'
import { useSwimLaneLayout } from '@/hooks/flows/useSwimLaneLayout'
import { useChainNavigation } from '@/hooks/flows/useChainNavigation'
import { FlowDagLazy } from './FlowDagLazy'
import { stepKindSpec, entryKindLabel } from '@/lib/flowKinds'
import type { ProcessStep, FlowEnrichment } from '@/types/api'
import { useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'

interface FlowDetailPanelProps {
  group: string
  processId: string
  onClose?: () => void
}

// ────────────────────────────────────────────────────────────────────────────
// Complexity badge
// ────────────────────────────────────────────────────────────────────────────

function ComplexityBadge({ score }: { score: number }) {
  const cls =
    score >= 15
      ? 'bg-red-900/40 text-red-300'
      : score >= 8
        ? 'bg-amber-900/40 text-amber-300'
        : 'bg-emerald-900/40 text-emerald-300'
  return (
    <span className={`px-1.5 py-0.5 rounded text-[10px] font-mono ${cls}`}>
      cx:{score}
    </span>
  )
}

// ────────────────────────────────────────────────────────────────────────────
// Step-kind pill
// ────────────────────────────────────────────────────────────────────────────

function StepKindPill({ kind }: { kind: string | undefined }) {
  const spec = stepKindSpec(kind)
  return (
    <span
      className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-mono border ${spec.bg} ${spec.text} ${spec.border}`}
    >
      {spec.label}
    </span>
  )
}

// ────────────────────────────────────────────────────────────────────────────
// Side-effects bar
// ────────────────────────────────────────────────────────────────────────────

const SIDE_EFFECT_ICONS: Record<string, React.ReactNode> = {
  db_write:         <Database className="w-3 h-3" />,
  db_query:         <Database className="w-3 h-3" />,
  http_fetch:       <Globe className="w-3 h-3" />,
  message_publish:  <span className="text-[10px]">↑</span>,
  message_consume:  <span className="text-[10px]">↓</span>,
}

function SideEffectsBar({ effects }: { effects: string[] }) {
  if (effects.length === 0) return null
  const unique = [...new Set(effects)]
  return (
    <div
      className="flex items-center gap-2 px-4 py-2 border-b border-slate-200 dark:border-slate-800 bg-orange-950/30 flex-shrink-0 flex-wrap"
      aria-label="Observed side effects"
      data-testid="side-effects-bar"
    >
      <span className="text-[10px] text-slate-500 font-mono mr-1 flex-shrink-0">side effects:</span>
      {unique.map((fx) => {
        const spec = stepKindSpec(fx)
        return (
          <span
            key={fx}
            className={`inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-mono ${spec.bg} ${spec.text}`}
          >
            {SIDE_EFFECT_ICONS[fx] ?? null}
            {spec.label}
          </span>
        )
      })}
    </div>
  )
}

// ────────────────────────────────────────────────────────────────────────────
// AI Summary section (collapsible)
// ────────────────────────────────────────────────────────────────────────────

function AiSummarySection({
  summary,
  preconditions,
  expectedOutcome,
}: {
  summary?: string
  preconditions?: string[]
  expectedOutcome?: string
}) {
  const [open, setOpen] = useState(false)
  if (!summary && !preconditions?.length && !expectedOutcome) return null

  return (
    <div className="border border-slate-800 rounded-lg overflow-hidden" data-testid="ai-summary">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-2 w-full px-3 py-2 text-left bg-slate-900/60 hover:bg-slate-800/60 transition-colors"
        aria-expanded={open}
      >
        <BookOpen className="w-3.5 h-3.5 text-sky-400 flex-shrink-0" aria-hidden />
        <span className="text-xs text-slate-300 font-semibold flex-1">AI Insights</span>
        {open ? (
          <ChevronUp className="w-3.5 h-3.5 text-slate-500" aria-hidden />
        ) : (
          <ChevronDown className="w-3.5 h-3.5 text-slate-500" aria-hidden />
        )}
      </button>
      {open && (
        <div className="p-3 space-y-3 bg-slate-950/60">
          {summary && (
            <p className="text-xs text-slate-400 leading-relaxed">{summary}</p>
          )}
          {preconditions && preconditions.length > 0 && (
            <div>
              <p className="text-[10px] text-slate-500 font-mono mb-1">Preconditions</p>
              <ul className="space-y-0.5">
                {preconditions.map((pc, i) => (
                  <li key={i} className="text-xs text-slate-400 flex gap-1.5">
                    <span className="text-slate-600 mt-0.5">·</span>
                    {pc}
                  </li>
                ))}
              </ul>
            </div>
          )}
          {expectedOutcome && (
            <div>
              <p className="text-[10px] text-slate-500 font-mono mb-1">Expected Outcome</p>
              <p className="text-xs text-slate-400">{expectedOutcome}</p>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

// ────────────────────────────────────────────────────────────────────────────
// Side-effects sidebar section
// ────────────────────────────────────────────────────────────────────────────

function SideEffectsSidebar({
  writesDb,
  publishesTo,
  externalCalls,
}: {
  writesDb?: string[]
  publishesTo?: string[]
  externalCalls?: string[]
}) {
  const hasAny =
    (writesDb && writesDb.length > 0) ||
    (publishesTo && publishesTo.length > 0) ||
    (externalCalls && externalCalls.length > 0)
  if (!hasAny) return null

  return (
    <div className="space-y-2" data-testid="side-effects-sidebar">
      <p className="text-[10px] text-slate-500 font-mono uppercase tracking-wider">Side Effects</p>
      {writesDb && writesDb.length > 0 && (
        <div>
          <p className="text-[10px] text-amber-400 font-mono mb-1">DB Writes</p>
          {writesDb.map((t) => (
            <p key={t} className="text-xs text-slate-400 font-mono truncate">· {t}</p>
          ))}
        </div>
      )}
      {publishesTo && publishesTo.length > 0 && (
        <div>
          <p className="text-[10px] text-purple-400 font-mono mb-1">Publishes To</p>
          {publishesTo.map((t) => (
            <p key={t} className="text-xs text-slate-400 font-mono truncate">· {t}</p>
          ))}
        </div>
      )}
      {externalCalls && externalCalls.length > 0 && (
        <div>
          <p className="text-[10px] text-blue-400 font-mono mb-1">External Calls</p>
          {externalCalls.map((t) => (
            <p key={t} className="text-xs text-slate-400 font-mono truncate">· {t}</p>
          ))}
        </div>
      )}
    </div>
  )
}

// ────────────────────────────────────────────────────────────────────────────
// Data lineage section
// ────────────────────────────────────────────────────────────────────────────

function DataLineageSection({
  readSources,
  writeSinks,
}: {
  readSources?: string[]
  writeSinks?: string[]
}) {
  if ((!readSources || readSources.length === 0) && (!writeSinks || writeSinks.length === 0))
    return null

  return (
    <div className="space-y-2" data-testid="data-lineage">
      <p className="text-[10px] text-slate-500 font-mono uppercase tracking-wider">Data Lineage</p>
      {readSources && readSources.length > 0 && (
        <div className="flex items-start gap-2">
          <span className="text-[10px] text-teal-400 font-mono w-10 flex-shrink-0 mt-0.5">reads</span>
          <div className="space-y-0.5">
            {readSources.map((s) => (
              <p key={s} className="text-xs text-slate-400 font-mono">{s}</p>
            ))}
          </div>
        </div>
      )}
      {writeSinks && writeSinks.length > 0 && (
        <div className="flex items-start gap-2">
          <span className="text-[10px] text-amber-400 font-mono w-10 flex-shrink-0 mt-0.5">writes</span>
          <div className="space-y-0.5">
            {writeSinks.map((s) => (
              <p key={s} className="text-xs text-slate-400 font-mono">{s}</p>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

// ────────────────────────────────────────────────────────────────────────────
// Tab trigger
// ────────────────────────────────────────────────────────────────────────────

function FlowTabTrigger({ value, children }: { value: string; children: React.ReactNode }) {
  return (
    <Tabs.Trigger
      value={value}
      className={[
        'flex items-center gap-1.5 px-4 py-2.5 text-sm border-b-2 border-transparent transition-colors',
        'text-slate-400 dark:text-slate-500 hover:text-slate-700 dark:hover:text-slate-300',
        'data-[state=active]:border-sky-500 data-[state=active]:text-slate-800 dark:data-[state=active]:text-slate-200',
        'focus:outline-none focus-visible:ring-1 focus-visible:ring-sky-500',
      ].join(' ')}
    >
      {children}
    </Tabs.Trigger>
  )
}

// ────────────────────────────────────────────────────────────────────────────
// Main component
// ────────────────────────────────────────────────────────────────────────────

export function FlowDetailPanel({ group, processId, onClose }: FlowDetailPanelProps) {
  const { data, isLoading, error, refetch } = useFlowDetail(group, processId)
  const [selectedStep, setSelectedStep] = useState<ProcessStep | null>(null)
  const navigate = useNavigate()
  const params = useParams<{ group: string }>()

  const lanes = useSwimLaneLayout(data?.process.steps)

  const { focusedIndex, handleKeyDown, focusStep } = useChainNavigation(
    data?.process.steps,
    (step) => setSelectedStep(step),
  )

  if (isLoading) {
    return (
      <div className="h-full flex flex-col p-4 gap-4">
        <CardSkeleton />
        <CardSkeleton />
      </div>
    )
  }

  if (error || !data) {
    return (
      <EmptyState
        title="Could not load flow"
        message={error?.message ?? 'Flow detail unavailable.'}
        action={
          <button
            type="button"
            onClick={() => refetch()}
            className="px-3 py-1.5 rounded text-sm bg-slate-200 dark:bg-slate-800 text-slate-700 dark:text-slate-300 hover:bg-slate-300 dark:hover:bg-slate-700 transition-colors"
          >
            Retry
          </button>
        }
      />
    )
  }

  const { process } = data
  const steps = process.steps ?? []
  const enrichment = process.enrichment
  const sideEffects = process.flow_side_effects ?? []
  const currentGroup = params.group ?? group

  // Cross-link navigation helpers
  const linkedEndpointId = enrichment?.linked_endpoint_id
  const linkedTopicId = enrichment?.linked_topic_id
  const isHttpHandler =
    process.entity_kind === 'http' ||
    (process as { entry_kind?: string }).entry_kind === 'http_handler' ||
    (process as { entry_kind?: string }).entry_kind === 'http'
  const isMessageConsumer =
    process.entity_kind === 'kafka_consumer' ||
    (process as { entry_kind?: string }).entry_kind === 'kafka_consumer' ||
    (process as { entry_kind?: string }).entry_kind === 'message_consumer'

  function handlePrint() {
    window.print()
  }

  return (
    <div
      className="flex flex-col h-full overflow-hidden print:overflow-visible"
      onKeyDown={handleKeyDown}
      tabIndex={-1}
      aria-label={`Flow detail: ${process.label}`}
    >
      {/* ── Header ──────────────────────────────────────────────────────────── */}
      <div className="flex items-start gap-3 px-4 py-3 border-b border-slate-200 dark:border-slate-800 bg-slate-100/80 dark:bg-slate-900/80 flex-shrink-0 print:border-b-2">
        <div className="flex-1 min-w-0">
          <h2 className="font-mono text-sm font-semibold text-slate-800 dark:text-slate-200 truncate">
            {process.label}
          </h2>
          <div className="flex items-center gap-2 mt-1 flex-wrap">
            <RepoChip repo={process.repo} />
            {process.cross_stack && <CrossStackBadge />}
            {/* Entry kind badge */}
            {((process as { entry_kind?: string }).entry_kind || process.entity_kind) && (
              <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-sky-900/30 text-sky-300 border border-sky-800">
                {entryKindLabel(
                  (process as { entry_kind?: string }).entry_kind ?? process.entity_kind ?? '',
                )}
              </span>
            )}
            {/* Complexity score */}
            {process.complexity_score != null && (
              <ComplexityBadge score={process.complexity_score} />
            )}
            {/* Cross-repo chip */}
            {process.is_cross_repo && (
              <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-violet-900/30 text-violet-300 border border-violet-800">
                cross-repo
              </span>
            )}
            <span className="text-xs text-slate-400 dark:text-slate-500 font-mono">
              {process.step_count} steps
            </span>
          </div>

          {/* ── Cross-link: linked endpoint ────────────────────────────────── */}
          {isHttpHandler && (
            <button
              type="button"
              onClick={() =>
                navigate(
                  linkedEndpointId
                    ? `/paths/${currentGroup}/${linkedEndpointId}`
                    : `/paths/${currentGroup}`,
                )
              }
              className="mt-1.5 flex items-center gap-1 text-[10px] text-sky-400 hover:text-sky-300 font-mono transition-colors"
              data-testid="linked-endpoint"
            >
              <Link className="w-3 h-3" aria-hidden />
              View endpoint in Paths
            </button>
          )}

          {/* ── Cross-link: linked topic ───────────────────────────────────── */}
          {isMessageConsumer && (
            <button
              type="button"
              onClick={() =>
                navigate(
                  linkedTopicId
                    ? `/topology/${currentGroup}?topic=${linkedTopicId}`
                    : `/topology/${currentGroup}`,
                )
              }
              className="mt-1.5 flex items-center gap-1 text-[10px] text-purple-400 hover:text-purple-300 font-mono transition-colors"
              data-testid="linked-topic"
            >
              <Link className="w-3 h-3" aria-hidden />
              View topic in Topology
            </button>
          )}
        </div>

        {/* Print button */}
        <button
          type="button"
          onClick={handlePrint}
          className="p-1.5 rounded text-slate-400 dark:text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 hover:bg-slate-200 dark:hover:bg-slate-800 transition-colors flex-shrink-0 print:hidden"
          aria-label="Print flow detail"
          title="Print"
        >
          <Printer className="w-4 h-4" />
        </button>

        {onClose && (
          <button
            type="button"
            onClick={onClose}
            className="p-1.5 rounded text-slate-400 dark:text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 hover:bg-slate-200 dark:hover:bg-slate-800 transition-colors flex-shrink-0 print:hidden"
            aria-label="Close flow detail"
          >
            <X className="w-4 h-4" />
          </button>
        )}
      </div>

      {/* ── Side-effects bar ─────────────────────────────────────────────────── */}
      <SideEffectsBar effects={sideEffects} />

      {/* ── Keyboard nav hint ────────────────────────────────────────────────── */}
      {steps.length > 0 && (
        <div className="px-4 py-1.5 border-b border-slate-200 dark:border-slate-800 bg-slate-100/60 dark:bg-slate-950/60 flex-shrink-0 print:hidden">
          <p className="text-[10px] text-slate-500 dark:text-slate-600">
            ↑ ↓ navigate steps · Enter expand source
          </p>
        </div>
      )}

      {/* ── Tabs ─────────────────────────────────────────────────────────────── */}
      <Tabs.Root defaultValue="dag" className="flex flex-col flex-1 overflow-hidden">
        <Tabs.List
          className="flex border-b border-slate-200 dark:border-slate-800 px-4 bg-slate-100/60 dark:bg-slate-900/60 flex-shrink-0 print:hidden"
          aria-label="Flow detail views"
        >
          <FlowTabTrigger value="dag">DAG</FlowTabTrigger>
          <FlowTabTrigger value="lanes">
            <ArrowLeftRight className="w-3.5 h-3.5" aria-hidden />
            Swim Lanes
          </FlowTabTrigger>
          <FlowTabTrigger value="chain">Chain ({steps.length})</FlowTabTrigger>
          <FlowTabTrigger value="info">Info</FlowTabTrigger>
          <FlowTabTrigger value="insights">
            <Sparkles className="w-3.5 h-3.5" aria-hidden />
            AI Insights
            {process.docgen_status === 'stale' && (
              <span className="ml-1 text-[9px] bg-amber-500/20 text-amber-600 dark:text-amber-400 px-1 rounded">stale</span>
            )}
          </FlowTabTrigger>
        </Tabs.List>

        <div className="flex-1 overflow-y-auto">

          {/* ── DAG tab ─────────────────────────────────────────────────────── */}
          <Tabs.Content value="dag" className="p-4" data-testid="dag-tab">
            {/* AI summary */}
            {enrichment && (
              <div className="mb-4">
                <AiSummarySection
                  summary={enrichment.ai_summary}
                  preconditions={enrichment.preconditions}
                  expectedOutcome={enrichment.expected_outcome}
                />
              </div>
            )}

            {/* React Flow DAG */}
            <FlowDagLazy
              steps={steps}
              primaryRepo={process.repo}
              onStepClick={(step) => {
                setSelectedStep(step)
                focusStep(step.step_index)
              }}
              /* Jarvis hook reserve: highlightedEntityIds — wired but no-op until #1157 */
              highlightedEntityIds={undefined}
            />

            {/* Metadata sections */}
            {(enrichment?.writes_db_table?.length ||
              enrichment?.publishes_to?.length ||
              enrichment?.external_calls?.length) && (
              <div className="mt-4">
                <SideEffectsSidebar
                  writesDb={enrichment.writes_db_table}
                  publishesTo={enrichment.publishes_to}
                  externalCalls={enrichment.external_calls}
                />
              </div>
            )}

            {(enrichment?.read_sources?.length || enrichment?.write_sinks?.length) && (
              <div className="mt-4">
                <DataLineageSection
                  readSources={enrichment.read_sources}
                  writeSinks={enrichment.write_sinks}
                />
              </div>
            )}
          </Tabs.Content>

          {/* ── Swim lane tab ────────────────────────────────────────────────── */}
          <Tabs.Content value="lanes" className="p-0">
            {lanes.length === 0 ? (
              <EmptyState
                title="No chain steps"
                message="Detailed steps are loaded on demand. Switch to Chain view or select a flow with steps populated."
              />
            ) : (
              <SwimLanePanel
                lanes={lanes}
                allSteps={steps}
                focusedStepIndex={focusedIndex}
                onStepClick={(step) => {
                  setSelectedStep(step)
                  focusStep(step.step_index)
                }}
                onStepFocus={(idx) => focusStep(idx)}
              />
            )}
          </Tabs.Content>

          {/* ── Chain tab ────────────────────────────────────────────────────── */}
          <Tabs.Content value="chain" className="p-4 space-y-2">
            {steps.length === 0 ? (
              <EmptyState title="No steps" message="This process has no expanded chain steps." />
            ) : (
              steps.map((step, idx) => {
                const isCrossRepo = step.repo !== process.repo
                return (
                  <div
                    key={step.entity_id}
                    className={[
                      'rounded-lg border transition-colors cursor-pointer',
                      focusedIndex === idx
                        ? 'border-sky-700 bg-sky-900/20'
                        : 'border-slate-200 dark:border-slate-800 hover:border-slate-300 dark:hover:border-slate-700 hover:bg-slate-200/60 dark:hover:bg-slate-900/60',
                    ].join(' ')}
                    onClick={() => { setSelectedStep(step); focusStep(idx) }}
                    role="button"
                    tabIndex={0}
                    aria-label={`Step ${idx + 1}: ${step.label}`}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault()
                        setSelectedStep(step)
                        focusStep(idx)
                      }
                    }}
                  >
                    <div className="px-3 py-2 flex items-center gap-2 flex-wrap">
                      <span className="text-[10px] text-slate-500 dark:text-slate-600 font-mono">{idx + 1}.</span>
                      <span className="font-mono text-xs text-slate-700 dark:text-slate-300 flex-1 min-w-0 truncate">
                        {step.label}
                      </span>
                      {/* Step-kind badge */}
                      <StepKindPill kind={step.step_kind} />
                      {/* Cross-repo badge */}
                      {isCrossRepo && <RepoChip repo={step.repo} className="ml-1" />}
                    </div>
                  </div>
                )
              })
            )}
          </Tabs.Content>

          {/* ── Info tab ────────────────────────────────────────────────────── */}
          <Tabs.Content value="info" className="p-4 space-y-4" data-testid="info-tab">
            {enrichment ? (
              <>
                <AiSummarySection
                  summary={enrichment.ai_summary}
                  preconditions={enrichment.preconditions}
                  expectedOutcome={enrichment.expected_outcome}
                />
                <SideEffectsSidebar
                  writesDb={enrichment.writes_db_table}
                  publishesTo={enrichment.publishes_to}
                  externalCalls={enrichment.external_calls}
                />
                <DataLineageSection
                  readSources={enrichment.read_sources}
                  writeSinks={enrichment.write_sinks}
                />
              </>
            ) : (
              <EmptyState
                title="No enrichment data"
                message="Run doc generation to populate AI insights, side effects, and data lineage for this flow."
              />
            )}
          </Tabs.Content>

          {/* AI Insights view (#1152) */}
          <Tabs.Content value="insights" className="p-4">
            <AIInsightsSection
              group={group}
              processId={processId}
              enrichment={process.enrichment}
              docgenStatus={process.docgen_status}
            />
          </Tabs.Content>
        </div>
      </Tabs.Root>

      {/* ── Selected step detail slide-in ─────────────────────────────────────── */}
      {selectedStep && (
        <div className="border-t border-slate-200 dark:border-slate-800 p-4 bg-slate-100/60 dark:bg-slate-900/60 flex-shrink-0 max-h-64 overflow-y-auto print:hidden">
          <ChainStepDetail
            step={selectedStep}
            sourceCode={data.source_snippets?.[selectedStep.entity_id]}
            onClose={() => setSelectedStep(null)}
          />
        </div>
      )}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// AI Insights section (#1152) — renders YAML frontmatter fields from /generate-docs
// ─────────────────────────────────────────────────────────────────────────────

interface AIInsightsSectionProps {
  group: string
  processId: string
  enrichment?: FlowEnrichment
  docgenStatus?: 'enriched' | 'pending' | 'stale'
}

function AIInsightsSection({ group, processId, enrichment, docgenStatus }: AIInsightsSectionProps) {
  const [triggerState, setTriggerState] = useState<'idle' | 'queuing' | 'queued'>('idle')

  async function handleTrigger() {
    setTriggerState('queuing')
    try {
      await fetch(`/api/flows/${group}/${encodeURIComponent(processId)}/trigger-enrichment`, {
        method: 'POST',
      })
      setTriggerState('queued')
    } catch {
      setTriggerState('idle')
    }
  }

  // No enrichment at all.
  if (!enrichment || docgenStatus === 'pending') {
    return (
      <div className="flex flex-col items-center gap-3 py-8 text-center">
        <Sparkles className="w-8 h-8 text-slate-300 dark:text-slate-700" />
        <p className="text-sm text-slate-500 dark:text-slate-400 max-w-xs">
          No AI documentation found for this flow. Run{' '}
          <code className="font-mono text-xs bg-slate-100 dark:bg-slate-800 px-1 rounded">/generate-docs</code>{' '}
          to generate insights.
        </p>
        <TriggerButton state={triggerState} onClick={handleTrigger} />
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {/* Stale warning */}
      {docgenStatus === 'stale' && (
        <div className="flex items-start gap-2 p-3 rounded-lg bg-amber-50 dark:bg-amber-950/30 border border-amber-200 dark:border-amber-800">
          <RefreshCw className="w-4 h-4 text-amber-500 mt-0.5 flex-shrink-0" />
          <div className="flex-1 min-w-0">
            <p className="text-xs text-amber-700 dark:text-amber-400">
              AI documentation may be out of date. Re-run{' '}
              <code className="font-mono text-[10px] bg-amber-100 dark:bg-amber-900/40 px-1 rounded">/generate-docs</code>{' '}
              to refresh.
            </p>
          </div>
          <TriggerButton state={triggerState} onClick={handleTrigger} compact />
        </div>
      )}

      {/* Summary */}
      {enrichment.summary && (
        <InsightRow label="Summary">
          <p className="text-sm text-slate-700 dark:text-slate-300">{enrichment.summary}</p>
        </InsightRow>
      )}

      {/* Preconditions */}
      {enrichment.preconditions && (
        <InsightRow label="Preconditions">
          <p className="text-sm text-slate-700 dark:text-slate-300">{enrichment.preconditions}</p>
        </InsightRow>
      )}

      {/* Expected outcome */}
      {enrichment.expected_outcome && (
        <InsightRow label="Expected outcome">
          <p className="text-sm text-slate-700 dark:text-slate-300">{enrichment.expected_outcome}</p>
        </InsightRow>
      )}

      {/* AI-documented steps */}
      {enrichment.steps && enrichment.steps.length > 0 && (
        <InsightRow label="AI-documented steps">
          <ol className="space-y-1 list-none">
            {enrichment.steps.map((step, i) => (
              <li key={i} className="flex items-start gap-2 text-sm text-slate-700 dark:text-slate-300">
                <span className="text-[10px] font-mono text-slate-400 dark:text-slate-600 mt-0.5 w-4 flex-shrink-0">
                  {i + 1}.
                </span>
                {step}
              </li>
            ))}
          </ol>
        </InsightRow>
      )}

      {/* Gaps */}
      {enrichment.gaps && enrichment.gaps.length > 0 && (
        <InsightRow label="Known gaps">
          <ul className="space-y-1">
            {enrichment.gaps.map((gap, i) => (
              <li key={i} className="text-sm text-amber-700 dark:text-amber-400 flex items-start gap-1.5">
                <span className="mt-1 w-1.5 h-1.5 rounded-full bg-amber-400 dark:bg-amber-600 flex-shrink-0" />
                {gap}
              </li>
            ))}
          </ul>
        </InsightRow>
      )}

      {/* Trigger enrichment (enriched state — refresh option) */}
      {docgenStatus === 'enriched' && (
        <div className="pt-2 flex justify-end">
          <TriggerButton state={triggerState} onClick={handleTrigger} compact />
        </div>
      )}
    </div>
  )
}

function InsightRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="rounded-lg border border-slate-200 dark:border-slate-800 overflow-hidden">
      <div className="px-3 py-1.5 bg-slate-100/80 dark:bg-slate-900/80 border-b border-slate-200 dark:border-slate-800">
        <span className="text-[10px] font-semibold tracking-wide uppercase text-slate-500 dark:text-slate-500">
          {label}
        </span>
      </div>
      <div className="px-3 py-2">{children}</div>
    </div>
  )
}

function TriggerButton({
  state,
  onClick,
  compact = false,
}: {
  state: 'idle' | 'queuing' | 'queued'
  onClick: () => void
  compact?: boolean
}) {
  if (state === 'queued') {
    return (
      <span className={`text-xs text-emerald-600 dark:text-emerald-400 ${compact ? '' : 'px-3 py-1.5'}`}>
        Queued — run /generate-docs to apply
      </span>
    )
  }
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={state === 'queuing'}
      className={[
        'flex items-center gap-1.5 text-xs rounded transition-colors',
        compact
          ? 'px-2 py-1 bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-400 hover:bg-slate-200 dark:hover:bg-slate-700'
          : 'px-3 py-1.5 bg-sky-600 text-white hover:bg-sky-700',
        state === 'queuing' ? 'opacity-60 cursor-not-allowed' : '',
      ].join(' ')}
    >
      <RefreshCw className={`w-3 h-3 ${state === 'queuing' ? 'animate-spin' : ''}`} />
      {state === 'queuing' ? 'Queuing…' : 'Trigger enrichment'}
    </button>
  )
}
