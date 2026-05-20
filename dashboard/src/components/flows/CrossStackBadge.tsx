import { GitMerge } from 'lucide-react'

interface CrossStackBadgeProps {
  /** Number of repos the process crosses into */
  repoCount?: number
  /** Link method used at the cross-stack boundary */
  linkMethod?: 'http' | 'kafka_topic' | 'ws_channel'
  className?: string
}

const LINK_METHOD_LABELS: Record<string, string> = {
  http: 'HTTP',
  kafka_topic: 'Kafka',
  ws_channel: 'WebSocket',
}

export function CrossStackBadge({ repoCount = 2, linkMethod, className = '' }: CrossStackBadgeProps) {
  const label = linkMethod ? `via ${LINK_METHOD_LABELS[linkMethod] ?? linkMethod}` : `${repoCount} repos`

  return (
    <span
      className={[
        'inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-xs',
        'bg-violet-900/40 text-violet-300 border border-violet-700',
        className,
      ].join(' ')}
      title={`Cross-stack: crosses ${label}`}
      aria-label={`Cross-stack process crossing ${label}`}
    >
      <GitMerge className="w-3 h-3" aria-hidden />
      {label}
    </span>
  )
}
