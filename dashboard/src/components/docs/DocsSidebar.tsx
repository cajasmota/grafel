import { useState, useCallback, useEffect } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { ChevronRight, FileText, FolderOpen, Folder } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { DocTreeNode, DocTreeGroup, DocTreeFile } from '@/types/docs'

interface DocsSidebarProps {
  group: string
  tree: DocTreeNode[]
  currentPath?: string
}

type CollapseState = Record<string, boolean>

function getStorageKey(group: string) {
  return `docs-sidebar-${group}`
}

function loadCollapseState(group: string): CollapseState {
  try {
    const raw = localStorage.getItem(getStorageKey(group))
    return raw ? JSON.parse(raw) : {}
  } catch {
    return {}
  }
}

function saveCollapseState(group: string, state: CollapseState) {
  try {
    localStorage.setItem(getStorageKey(group), JSON.stringify(state))
  } catch {}
}

/**
 * Recursive sidebar tree node.
 * Keyboard: arrow keys to navigate, Enter to follow links, Space to toggle groups.
 */
function SidebarGroup({
  node,
  group,
  currentPath,
  depth,
  collapsed,
  onToggle,
}: {
  node: DocTreeGroup
  group: string
  currentPath?: string
  depth: number
  collapsed: boolean
  onToggle: (path: string) => void
}) {
  const isOpen = !collapsed

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      onToggle(node.path)
    }
  }

  return (
    <li>
      <button
        type="button"
        aria-expanded={isOpen}
        className={cn(
          'flex w-full items-center gap-2 rounded px-2 py-1.5 text-sm text-slate-400 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200 hover:bg-slate-200/60 dark:hover:bg-slate-800/60 transition-colors text-left',
          depth > 0 && 'pl-4',
        )}
        style={{ paddingLeft: `${(depth + 1) * 12}px` }}
        onClick={() => onToggle(node.path)}
        onKeyDown={handleKeyDown}
      >
        <ChevronRight
          className={cn(
            'w-3.5 h-3.5 flex-shrink-0 transition-transform text-slate-500 dark:text-slate-600',
            isOpen && 'rotate-90',
          )}
          aria-hidden
        />
        {isOpen ? (
          <FolderOpen className="w-3.5 h-3.5 flex-shrink-0 text-slate-400 dark:text-slate-500" aria-hidden />
        ) : (
          <Folder className="w-3.5 h-3.5 flex-shrink-0 text-slate-400 dark:text-slate-500" aria-hidden />
        )}
        <span className="truncate">{node.label}</span>
      </button>

      {isOpen && (
        <ul role="group">
          {node.children.map((child) => (
            <SidebarNode
              key={child.path}
              node={child}
              group={group}
              currentPath={currentPath}
              depth={depth + 1}
              collapsed={false}
              onToggle={onToggle}
            />
          ))}
        </ul>
      )}
    </li>
  )
}

function SidebarFile({
  node,
  group,
  currentPath,
  depth,
}: {
  node: DocTreeFile
  group: string
  currentPath?: string
  depth: number
}) {
  const isActive = currentPath === node.path

  return (
    <li>
      <Link
        to={`/docs/${group}/${node.path}`}
        aria-current={isActive ? 'page' : undefined}
        className={cn(
          'flex items-center gap-2 rounded py-1.5 pr-2 text-sm transition-colors',
          isActive
            ? 'bg-sky-950/60 text-sky-300 font-medium border-l-2 border-sky-500'
            : 'text-slate-400 dark:text-slate-500 hover:text-slate-800 dark:hover:text-slate-200 hover:bg-slate-800/40',
        )}
        style={{ paddingLeft: `${(depth + 1) * 12 + 6}px` }}
      >
        <FileText className="w-3.5 h-3.5 flex-shrink-0" aria-hidden />
        <span className="truncate">{node.label}</span>
      </Link>
    </li>
  )
}

function SidebarNode({
  node,
  group,
  currentPath,
  depth,
  collapsed: parentCollapsed,
  onToggle,
}: {
  node: DocTreeNode
  group: string
  currentPath?: string
  depth: number
  collapsed: boolean
  onToggle: (path: string) => void
}) {
  if (node.type === 'file') {
    return (
      <SidebarFile
        node={node}
        group={group}
        currentPath={currentPath}
        depth={depth}
      />
    )
  }

  return (
    <SidebarGroup
      node={node}
      group={group}
      currentPath={currentPath}
      depth={depth}
      collapsed={parentCollapsed}
      onToggle={onToggle}
    />
  )
}

/**
 * Collapsible docs sidebar tree.
 * - Expansion state persists to localStorage keyed by group.
 * - Active page is highlighted with aria-current="page".
 * - Groups default-open if node.defaultOpen === true.
 * - Keyboard: Tab to navigate, Enter/Space to toggle groups.
 */
export function DocsSidebar({ group, tree, currentPath }: DocsSidebarProps) {
  const [collapseState, setCollapseState] = useState<CollapseState>(() =>
    loadCollapseState(group),
  )

  // Auto-open groups that contain the current page
  useEffect(() => {
    if (!currentPath) return
    const toOpen: CollapseState = {}
    function findPath(nodes: DocTreeNode[], ancestors: string[]): boolean {
      for (const node of nodes) {
        if (node.type === 'file') {
          if (node.path === currentPath) {
            ancestors.forEach((p) => { toOpen[p] = false })
            return true
          }
        } else {
          if (findPath(node.children, [...ancestors, node.path])) return true
        }
      }
      return false
    }
    findPath(tree, [])
    setCollapseState((prev) => ({ ...prev, ...toOpen }))
  }, [currentPath, tree])

  const handleToggle = useCallback((path: string) => {
    setCollapseState((prev) => {
      const next = { ...prev, [path]: !prev[path] }
      saveCollapseState(group, next)
      return next
    })
  }, [group])

  return (
    <nav
      aria-label="Documentation navigation"
      className="py-4 px-2"
    >
      <ul role="tree" className="space-y-0.5">
        {tree.map((node) => {
          // Determine initial collapsed state
          const isGroup = node.type === 'group'
          const collapsed = isGroup
            ? (collapseState[node.path] ?? !node.defaultOpen)
            : false

          return (
            <SidebarNode
              key={node.path}
              node={node}
              group={group}
              currentPath={currentPath}
              depth={0}
              collapsed={collapsed}
              onToggle={handleToggle}
            />
          )
        })}
      </ul>
    </nav>
  )
}
