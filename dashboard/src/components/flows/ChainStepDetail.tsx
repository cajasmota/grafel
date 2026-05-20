import { X } from 'lucide-react'
import { KindIcon } from '@/components/shared/KindIcon'
import { RepoChip } from '@/components/shared/RepoChip'
import { SourceSnippet } from '@/components/shared/SourceSnippet'
import type { ProcessStep, EntityKind } from '@/types/api'

interface ChainStepDetailProps {
  step: ProcessStep
  entityKind?: EntityKind
  /** Raw source snippet text if available */
  sourceCode?: string
  onClose?: () => void
}

export function ChainStepDetail({
  step,
  entityKind = 'Function',
  sourceCode,
  onClose,
}: ChainStepDetailProps) {
  return (
    <div
      className="flex flex-col gap-4"
      role="region"
      aria-label={`Detail for step ${step.label}`}
    >
      {/* Header */}
      <div className="flex items-start gap-3">
        <span className="text-slate-400 mt-0.5" aria-hidden>
          <KindIcon kind={entityKind} className="w-4 h-4" />
        </span>
        <div className="flex-1 min-w-0">
          <h3 className="font-mono text-sm font-semibold text-slate-200 truncate">
            {step.label}
          </h3>
          <p className="text-xs text-slate-500 font-mono mt-0.5">
            step {step.step_index + 1} · {step.edge_kind}
          </p>
        </div>
        <RepoChip repo={step.repo} />
        {onClose && (
          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded text-slate-500 hover:text-slate-300 hover:bg-slate-800 transition-colors"
            aria-label="Close step detail"
          >
            <X className="w-4 h-4" />
          </button>
        )}
      </div>

      {/* Source snippet */}
      <SourceSnippet
        sourceFile={step.source_file}
        startLine={step.start_line}
        code={sourceCode}
      />
    </div>
  )
}
