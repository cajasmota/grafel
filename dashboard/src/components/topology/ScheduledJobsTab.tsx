import { Calendar } from 'lucide-react'
import type { QueueNode } from '@/types/api'

// ── Framework badge ───────────────────────────────────────────────────────────

const FRAMEWORK_COLOR: Record<string, string> = {
  celery: 'bg-green-100 dark:bg-green-900/40 text-green-700 dark:text-green-300 border-green-200 dark:border-green-700',
  celery_beat: 'bg-green-100 dark:bg-green-900/40 text-green-700 dark:text-green-300 border-green-200 dark:border-green-700',
  apscheduler: 'bg-blue-100 dark:bg-blue-900/40 text-blue-700 dark:text-blue-300 border-blue-200 dark:border-blue-700',
  'node-cron': 'bg-yellow-100 dark:bg-yellow-900/40 text-yellow-700 dark:text-yellow-300 border-yellow-200 dark:border-yellow-700',
  sidekiq: 'bg-red-100 dark:bg-red-900/40 text-red-700 dark:text-red-300 border-red-200 dark:border-red-700',
  dramatiq: 'bg-violet-100 dark:bg-violet-900/40 text-violet-700 dark:text-violet-300 border-violet-200 dark:border-violet-700',
  rq: 'bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-400 border-slate-200 dark:border-slate-700',
  bull: 'bg-orange-100 dark:bg-orange-900/40 text-orange-700 dark:text-orange-300 border-orange-200 dark:border-orange-700',
}

function FrameworkBadge({ framework }: { framework: string }) {
  const colors = FRAMEWORK_COLOR[framework.toLowerCase()] ??
    'bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-400 border-slate-200 dark:border-slate-700'
  return (
    <span
      className={[
        'inline-flex items-center px-1.5 py-0.5 text-[10px] font-mono rounded border',
        colors,
      ].join(' ')}
    >
      {framework}
    </span>
  )
}

// ── Schedule expression chip ──────────────────────────────────────────────────

function ScheduleChip({ schedule }: { schedule: string }) {
  return (
    <span className="inline-flex items-center px-1.5 py-0.5 text-[10px] font-mono rounded border border-sky-200 dark:border-sky-700/50 bg-sky-50 dark:bg-sky-900/20 text-sky-700 dark:text-sky-300">
      {schedule}
    </span>
  )
}

// ── Row ───────────────────────────────────────────────────────────────────────

function ScheduledJobRow({ job }: { job: QueueNode }) {
  return (
    <div
      role="row"
      className="flex items-center gap-3 px-4 py-2.5 border-b border-slate-100 dark:border-slate-800"
    >
      {/* Label */}
      <span className="flex-1 min-w-0 font-mono text-xs text-slate-700 dark:text-slate-200 truncate" title={job.label}>
        {job.label}
      </span>

      {/* Framework badge */}
      {job.framework && <FrameworkBadge framework={job.framework} />}

      {/* Schedule expression */}
      {job.schedule ? (
        <ScheduleChip schedule={job.schedule} />
      ) : (
        <span className="text-[10px] text-slate-400 dark:text-slate-600 italic">no schedule</span>
      )}

      {/* Repo */}
      <span className="text-xs text-slate-400 dark:text-slate-500 flex-shrink-0 max-w-[140px] truncate text-right" title={job.repo}>
        {job.repo}
      </span>

      {/* Last run placeholder */}
      <span className="text-[10px] text-slate-300 dark:text-slate-600 flex-shrink-0 italic w-16 text-right">
        —
      </span>
    </div>
  )
}

// ── Empty state ───────────────────────────────────────────────────────────────

function EmptyScheduledJobs() {
  return (
    <div className="flex flex-col items-center justify-center py-16 gap-3 text-center px-6">
      <Calendar className="w-8 h-8 text-slate-300 dark:text-slate-600" aria-hidden />
      <p className="text-sm text-slate-500 dark:text-slate-400 font-medium">
        No scheduled jobs found
      </p>
      <p className="text-xs text-slate-400 dark:text-slate-500 max-w-xs">
        Scheduled jobs (celery_beat, APScheduler, node-cron, etc.) appear here once indexed.
      </p>
    </div>
  )
}

// ── Main export ───────────────────────────────────────────────────────────────

interface ScheduledJobsTabProps {
  jobs: QueueNode[]
}

export function ScheduledJobsTab({ jobs }: ScheduledJobsTabProps) {
  if (jobs.length === 0) {
    return <EmptyScheduledJobs />
  }

  return (
    <div className="flex flex-col flex-1 overflow-hidden">
      {/* Column header */}
      <div className="flex items-center gap-3 px-4 py-1.5 border-b border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-900/50 text-[10px] text-slate-400 dark:text-slate-500 uppercase tracking-wide flex-shrink-0">
        <span className="flex-1">Job</span>
        <span>Framework</span>
        <span>Schedule</span>
        <span className="max-w-[140px] text-right">Repo</span>
        <span className="w-16 text-right">Last run</span>
      </div>

      <div
        role="grid"
        aria-label="Scheduled jobs list"
        className="flex-1 overflow-y-auto"
      >
        {jobs.map((job) => (
          <ScheduledJobRow key={job.id} job={job} />
        ))}
      </div>
    </div>
  )
}
