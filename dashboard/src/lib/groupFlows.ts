/**
 * Grouping logic for the Flows entry-kind view (#1151).
 *
 * Render order: high priority first → medium → low, then alphabetical within
 * each tier. Within a group, processes are sorted by complexity_score desc,
 * then by step_count desc, then by label asc.
 */

import type { Process, FlowEntryKind, FlowEntryKindGroup, FlowPriorityHint } from '@/types/api'

// ── Metadata ─────────────────────────────────────────────────────────────────

export const FLOW_KIND_LABELS: Record<FlowEntryKind, string> = {
  http_handler:      'HTTP Handlers',
  message_consumer:  'Message Consumers',
  scheduled_task:    'Scheduled Tasks',
  component_render:  'UI Components',
  test:              'Tests',
  cli_command:       'CLI Commands',
  function:          'Functions',
  internal:          'Internal',
}

/** Priority hint for each kind — controls group render order. */
export const FLOW_KIND_PRIORITY: Record<FlowEntryKind, FlowPriorityHint> = {
  http_handler:      'high',
  message_consumer:  'medium',
  scheduled_task:    'medium',
  component_render:  'low',
  test:              'low',
  cli_command:       'low',
  function:          'low',
  internal:          'low',
}

/** Groups that are open by default (before the user has set a localStorage preference). */
const DEFAULT_OPEN: Set<FlowEntryKind> = new Set([
  'http_handler',
  'message_consumer',
  'scheduled_task',
])

export function isDefaultOpen(kind: FlowEntryKind): boolean {
  return DEFAULT_OPEN.has(kind)
}

const PRIORITY_ORDER: FlowPriorityHint[] = ['high', 'medium', 'low']

// ── Types ─────────────────────────────────────────────────────────────────────

export interface FlowGroup {
  kind: FlowEntryKind
  label: string
  priority: FlowPriorityHint
  count: number
  processes: Process[]
}

// ── Core grouping ─────────────────────────────────────────────────────────────

/** Resolve a process to its display group kind. Falls back via entity_kind mapping. */
function resolveKind(process: Process): FlowEntryKind {
  if (process.flow_entry_kind) return process.flow_entry_kind

  // Backward-compat: map old entity_kind values to new FlowEntryKind
  const fallback: Record<string, FlowEntryKind> = {
    http:           'http_handler',
    kafka_consumer: 'message_consumer',
    scheduled:      'scheduled_task',
    ws_handler:     'http_handler',
  }
  return fallback[process.entity_kind ?? ''] ?? 'internal'
}

/** Sort processes within a group: complexity desc → step_count desc → label asc */
function sortProcesses(rows: Process[]): Process[] {
  return [...rows].sort((a, b) => {
    const ca = a.complexity_score ?? 0
    const cb = b.complexity_score ?? 0
    if (cb !== ca) return cb - ca
    if (b.step_count !== a.step_count) return b.step_count - a.step_count
    return a.label.localeCompare(b.label)
  })
}

/**
 * Build FlowGroup array from a flat list of processes.
 *
 * When the backend supplies `entry_kind_groups`, that determines the set of
 * visible groups (preserving backend sort which is count-descending). If
 * absent, groups are derived from the processes themselves.
 *
 * Final order: priority tier (high → medium → low), then alphabetical by
 * label within the same tier.
 */
export function groupFlows(
  processes: Process[],
  entryKindGroups?: FlowEntryKindGroup[],
): FlowGroup[] {
  // Build a map of kind → Process[]
  const map = new Map<FlowEntryKind, Process[]>()

  for (const p of processes) {
    const kind = resolveKind(p)
    const bucket = map.get(kind) ?? []
    bucket.push(p)
    map.set(kind, bucket)
  }

  // Determine the ordered set of kinds to display
  let orderedKinds: FlowEntryKind[]

  if (entryKindGroups && entryKindGroups.length > 0) {
    // Use backend order (count-descending) as the canonical set,
    // but we still re-sort below by priority tier for the final render
    orderedKinds = entryKindGroups.map((g) => g.kind)
  } else {
    orderedKinds = Array.from(map.keys())
  }

  // Build groups
  const groups: FlowGroup[] = orderedKinds
    .filter((kind) => map.has(kind))
    .map((kind) => ({
      kind,
      label:    FLOW_KIND_LABELS[kind] ?? kind,
      priority: FLOW_KIND_PRIORITY[kind] ?? 'low',
      count:    map.get(kind)!.length,
      processes: sortProcesses(map.get(kind)!),
    }))

  // Sort: priority tier first, then alphabetical by label within same tier
  groups.sort((a, b) => {
    const pa = PRIORITY_ORDER.indexOf(a.priority)
    const pb = PRIORITY_ORDER.indexOf(b.priority)
    if (pa !== pb) return pa - pb
    return a.label.localeCompare(b.label)
  })

  return groups
}
