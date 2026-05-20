import { useState } from 'react'
import { ChevronDown, ChevronRight, Network } from 'lucide-react'
import type { PathTreeNode } from '@/types/api'
import { PathTreeSkeleton } from '@/components/shared/LoadingState'

interface PathTreeSidebarProps {
  tree: PathTreeNode[]
  isLoading: boolean
  activePrefix?: string
  onPrefixSelect: (prefix: string | undefined) => void
}

export function PathTreeSidebar({
  tree,
  isLoading,
  activePrefix,
  onPrefixSelect,
}: PathTreeSidebarProps) {
  if (isLoading) return <PathTreeSkeleton />

  return (
    <nav
      className="h-full overflow-y-auto py-2"
      aria-label="API path prefixes"
    >
      <div className="px-3 pb-2">
        <button
          type="button"
          className={[
            'w-full flex items-center gap-2 px-2 py-1.5 rounded text-xs text-left',
            'transition-colors',
            !activePrefix
              ? 'bg-sky-900/40 text-sky-300'
              : 'text-slate-400 dark:text-slate-400 hover:bg-slate-200 dark:hover:bg-slate-800 hover:text-slate-700 dark:hover:text-slate-300',
          ].join(' ')}
          onClick={() => onPrefixSelect(undefined)}
          aria-current={!activePrefix ? 'page' : undefined}
        >
          <Network className="w-3.5 h-3.5 flex-shrink-0" aria-hidden />
          <span className="font-mono">All paths</span>
        </button>
      </div>
      <ul role="tree" className="space-y-0.5 px-2">
        {tree.map((node) => (
          <TreeNode
            key={node.prefix}
            node={node}
            depth={0}
            activePrefix={activePrefix}
            onPrefixSelect={onPrefixSelect}
          />
        ))}
      </ul>
    </nav>
  )
}

interface TreeNodeProps {
  node: PathTreeNode
  depth: number
  activePrefix?: string
  onPrefixSelect: (prefix: string | undefined) => void
}

function TreeNode({ node, depth, activePrefix, onPrefixSelect }: TreeNodeProps) {
  const [expanded, setExpanded] = useState(depth === 0)
  const hasChildren = node.children.length > 0
  const isActive = activePrefix === node.prefix

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      onPrefixSelect(isActive ? undefined : node.prefix)
    }
    if (e.key === 'ArrowRight' && hasChildren && !expanded) {
      setExpanded(true)
    }
    if (e.key === 'ArrowLeft' && expanded) {
      setExpanded(false)
    }
  }

  return (
    <li role="treeitem" aria-expanded={hasChildren ? expanded : undefined}>
      <button
        type="button"
        className={[
          'w-full flex items-center gap-1.5 px-2 py-1 rounded text-xs text-left',
          'transition-colors group',
          isActive
            ? 'bg-sky-900/40 text-sky-300'
            : 'text-slate-400 dark:text-slate-400 hover:bg-slate-200 dark:hover:bg-slate-800 hover:text-slate-700 dark:hover:text-slate-300',
        ].join(' ')}
        style={{ paddingLeft: `${0.5 + depth * 0.75}rem` }}
        onClick={() => {
          onPrefixSelect(isActive ? undefined : node.prefix)
          if (hasChildren) setExpanded((e) => !e)
        }}
        onKeyDown={handleKeyDown}
        aria-current={isActive ? 'page' : undefined}
      >
        {hasChildren ? (
          expanded ? (
            <ChevronDown className="w-3 h-3 flex-shrink-0" aria-hidden />
          ) : (
            <ChevronRight className="w-3 h-3 flex-shrink-0" aria-hidden />
          )
        ) : (
          <span className="w-3 h-3 flex-shrink-0" aria-hidden />
        )}
        <span className="font-mono truncate flex-1">{node.label}/</span>
        <span className="text-slate-500 dark:text-slate-600 tabular-nums ml-1">{node.count}</span>
      </button>

      {hasChildren && expanded && (
        <ul role="group" className="mt-0.5 space-y-0.5">
          {node.children.map((child) => (
            <TreeNode
              key={child.prefix}
              node={child}
              depth={depth + 1}
              activePrefix={activePrefix}
              onPrefixSelect={onPrefixSelect}
            />
          ))}
        </ul>
      )}
    </li>
  )
}
