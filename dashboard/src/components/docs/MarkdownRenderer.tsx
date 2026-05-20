import { Suspense, lazy, useMemo } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { Components } from 'react-markdown'
import type { TocHeading, EntityCard } from '@/types/docs'
import { CodeBlock } from './CodeBlock'
import { PatternCallout } from './PatternCallout'
import { FlowDiagram } from './FlowDiagram'
import { EntityLink } from './EntityLink'

// Mermaid is heavy (~200KB); lazy-load only when a mermaid code block is present.
const MermaidBlock = lazy(() =>
  import('./MermaidBlock').then((m) => ({ default: m.MermaidBlock })),
)

interface MarkdownRendererProps {
  markdown: string
  /** Pre-resolved entity hovercards (from ?include=hovercards) */
  hovercards?: Record<string, EntityCard>
  /** Callback to register headings for TOC + scroll-spy */
  onHeadingsFound?: (headings: TocHeading[]) => void
}

/** Slugify a heading string into a valid HTML id */
function slugify(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^\w\s-]/g, '')
    .trim()
    .replace(/[\s_-]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

/**
 * Parses {{pattern:id}} and {{flow:id}} directives from paragraph text.
 * Returns null if not a directive, or the component to render.
 */
function parseDirective(text: string): React.ReactNode | null {
  const patternMatch = text.trim().match(/^\{\{pattern:([^}]+)\}\}$/)
  if (patternMatch) return <PatternCallout patternId={patternMatch[1]} />

  const flowMatch = text.trim().match(/^\{\{flow:([^}]+)\}\}$/)
  if (flowMatch) return <FlowDiagram entryEntityId={flowMatch[1]} />

  return null
}

/**
 * Configured react-markdown instance with:
 * - remark-gfm (tables, strikethrough, tasklists, autolinks)
 * - Custom code renderer: mermaid → <MermaidBlock>, others → <CodeBlock>
 * - Custom inline-code renderer: symbol → <EntityLink>
 * - Custom heading renderer: adds slug id + registers with TOC callback
 * - Custom paragraph renderer: intercepts {{pattern:}} and {{flow:}} directives
 */
export function MarkdownRenderer({ markdown, hovercards = {}, onHeadingsFound }: MarkdownRendererProps) {
  const headingRegistry = useMemo(() => new Map<string, TocHeading>(), [markdown])

  // Flush registered headings upward after render
  // We use a small side-effect in the heading component
  const registerHeading = (id: string, text: string, depth: 2 | 3) => {
    if (!headingRegistry.has(id)) {
      headingRegistry.set(id, { id, text, depth })
      if (onHeadingsFound) {
        onHeadingsFound(Array.from(headingRegistry.values()))
      }
    }
  }

  const components: Components = {
    // ── Code blocks ──────────────────────────────────────────────────────────
    code({ className, children }) {
      const match = /language-(\w+)/.exec(className ?? '')
      const lang = match?.[1] ?? ''
      const code = String(children).replace(/\n$/, '')

      if (lang === 'mermaid') {
        return (
          <Suspense fallback={<div className="h-40 animate-pulse rounded bg-slate-200 dark:bg-slate-800" />}>
            <MermaidBlock code={code} />
          </Suspense>
        )
      }

      // For code blocks (has language class) use full CodeBlock
      if (match) {
        return <CodeBlock code={code} language={lang} />
      }

      // Inline code — check if symbol matches a known entity
      const card = hovercards[code]
      if (card || Object.keys(hovercards).length > 0) {
        return (
          <EntityLink
            symbol={code}
            entityId={card?.id}
            prefetchedCard={card}
          />
        )
      }

      return (
        <code className="px-1 py-0.5 rounded bg-slate-200 dark:bg-slate-800 text-slate-700 dark:text-slate-300 font-mono text-[0.875em] border border-slate-300 dark:border-slate-700">
          {children}
        </code>
      )
    },

    // ── Headings ─────────────────────────────────────────────────────────────
    h1({ children }) {
      return <h1 className="text-3xl font-bold text-slate-900 dark:text-slate-100 mt-0 mb-6 leading-tight">{children}</h1>
    },
    h2({ children }) {
      const text = String(children)
      const id = slugify(text)
      registerHeading(id, text, 2)
      return (
        <h2
          id={id}
          className="text-xl font-semibold text-slate-900 dark:text-slate-100 mt-10 mb-4 pb-2 border-b border-slate-200 dark:border-slate-800 scroll-mt-20"
        >
          <a href={`#${id}`} className="no-underline hover:text-sky-400 transition-colors">{children}</a>
        </h2>
      )
    },
    h3({ children }) {
      const text = String(children)
      const id = slugify(text)
      registerHeading(id, text, 3)
      return (
        <h3
          id={id}
          className="text-lg font-semibold text-slate-800 dark:text-slate-200 mt-8 mb-3 scroll-mt-20"
        >
          <a href={`#${id}`} className="no-underline hover:text-sky-400 transition-colors">{children}</a>
        </h3>
      )
    },

    // ── Block elements ────────────────────────────────────────────────────────
    p({ children }) {
      // Check for a single text child that is a directive
      if (typeof children === 'string') {
        const directive = parseDirective(children)
        if (directive) return <>{directive}</>
      }
      // Array of children — check if first is a directive string
      if (Array.isArray(children) && children.length === 1 && typeof children[0] === 'string') {
        const directive = parseDirective(children[0])
        if (directive) return <>{directive}</>
      }
      return <p className="text-slate-400 dark:text-slate-400 leading-7 my-4">{children}</p>
    },

    blockquote({ children }) {
      return (
        <blockquote className="my-4 pl-4 border-l-4 border-sky-700 text-slate-400 dark:text-slate-400 italic">
          {children}
        </blockquote>
      )
    },

    ul({ children }) {
      return <ul className="my-4 ml-6 space-y-1.5 list-disc text-slate-400 dark:text-slate-400">{children}</ul>
    },
    ol({ children }) {
      return <ol className="my-4 ml-6 space-y-1.5 list-decimal text-slate-400 dark:text-slate-400">{children}</ol>
    },
    li({ children }) {
      return <li className="leading-7 pl-1">{children}</li>
    },

    // ── Tables ────────────────────────────────────────────────────────────────
    table({ children }) {
      return (
        <div className="my-6 overflow-x-auto rounded-lg border border-slate-200 dark:border-slate-800">
          <table className="min-w-full text-sm">{children}</table>
        </div>
      )
    },
    thead({ children }) {
      return <thead className="bg-slate-100 dark:bg-slate-900">{children}</thead>
    },
    th({ children }) {
      return (
        <th className="px-4 py-2.5 text-left text-xs font-semibold text-slate-400 dark:text-slate-400 uppercase tracking-wider border-b border-slate-200 dark:border-slate-800">
          {children}
        </th>
      )
    },
    td({ children }) {
      return <td className="px-4 py-2.5 text-slate-400 dark:text-slate-400 border-b border-slate-200 dark:border-slate-800/50">{children}</td>
    },

    // ── Links ─────────────────────────────────────────────────────────────────
    a({ href, children }) {
      const isExternal = href?.startsWith('http')
      return (
        <a
          href={href}
          className="text-sky-400 hover:text-sky-300 underline decoration-sky-700 hover:decoration-sky-400 transition-colors"
          {...(isExternal ? { target: '_blank', rel: 'noopener noreferrer' } : {})}
        >
          {children}
        </a>
      )
    },

    // ── Horizontal rule ───────────────────────────────────────────────────────
    hr() {
      return <hr className="my-8 border-slate-200 dark:border-slate-800" />
    },

    // ── Strong / em ───────────────────────────────────────────────────────────
    strong({ children }) {
      return <strong className="font-semibold text-slate-800 dark:text-slate-200">{children}</strong>
    },
    em({ children }) {
      return <em className="italic text-slate-700 dark:text-slate-300">{children}</em>
    },
    del({ children }) {
      return <del className="line-through text-slate-400 dark:text-slate-500">{children}</del>
    },
  }

  return (
    <div className="docs-prose">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {markdown}
      </ReactMarkdown>
    </div>
  )
}
