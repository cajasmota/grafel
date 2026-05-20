import { repoTailwindColor as repoColor } from "@/lib/colors"

interface RepoChipProps {
  repo: string
  className?: string
}

export function RepoChip({ repo, className = '' }: RepoChipProps) {
  const colors = repoColor(repo)
  return (
    <span
      className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-xs font-mono ${colors.bg} ${colors.text} ${className}`}
      title={repo}
    >
      <span className={`w-1.5 h-1.5 rounded-full ${colors.dot}`} aria-hidden />
      {repo}
    </span>
  )
}
