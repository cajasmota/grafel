import { useEffect, useRef, useState } from 'react'
import mermaid from 'mermaid'
import { AlertCircle } from 'lucide-react'

let initialized = false
let renderCount = 0

function ensureInit(isDark: boolean) {
  if (initialized) return
  mermaid.initialize({
    startOnLoad: false,
    theme: isDark ? 'dark' : 'default',
    fontFamily: 'Inter, ui-sans-serif, system-ui, sans-serif',
    themeVariables: isDark
      ? {
          background: '#0f172a',      // slate-900
          primaryColor: '#1e3a5f',    // deep sky
          primaryTextColor: '#e2e8f0', // slate-200
          lineColor: '#475569',        // slate-600
          edgeLabelBackground: '#1e293b',
          clusterBkg: '#1e293b',
        }
      : {
          background: '#f8fafc',
          primaryColor: '#bae6fd',
          primaryTextColor: '#0f172a',
          lineColor: '#94a3b8',
        },
  })
  initialized = true
}

interface MermaidBlockProps {
  code: string
  /** Force light/dark; defaults to reading .dark on <html> */
  theme?: 'dark' | 'light'
}

/**
 * Lazy-loaded Mermaid diagram renderer.
 * This component is the dynamic import target — the entire `mermaid` library
 * is only loaded when this component is actually used.
 *
 * Theme-aware: re-renders the diagram when the theme changes.
 */
export function MermaidBlock({ code, theme: themeProp }: MermaidBlockProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [error, setError] = useState<string | null>(null)
  const [svg, setSvg] = useState<string | null>(null)

  const isDark =
    themeProp !== undefined
      ? themeProp === 'dark'
      : document.documentElement.classList.contains('dark')

  useEffect(() => {
    let cancelled = false
    setError(null)
    setSvg(null)

    // Re-init if theme changed
    initialized = false
    ensureInit(isDark)

    const id = `mermaid-${++renderCount}`

    ;(async () => {
      try {
        const { svg: rendered } = await mermaid.render(id, code)
        if (!cancelled) setSvg(rendered)
      } catch (err) {
        if (!cancelled)
          setError(err instanceof Error ? err.message : 'Mermaid render error')
      }
    })()

    return () => {
      cancelled = true
    }
  }, [code, isDark])

  if (error) {
    return (
      <div className="flex items-start gap-2 p-4 rounded-lg bg-rose-950/30 border border-rose-800/50 text-sm text-rose-400">
        <AlertCircle className="w-4 h-4 flex-shrink-0 mt-0.5" aria-hidden />
        <div>
          <div className="font-medium mb-1">Diagram render error</div>
          <pre className="text-xs font-mono text-rose-500 whitespace-pre-wrap">{error}</pre>
        </div>
      </div>
    )
  }

  if (!svg) {
    return (
      <div
        className="animate-pulse rounded bg-slate-200 dark:bg-slate-800 h-40"
        role="status"
        aria-label="Loading diagram"
      />
    )
  }

  return (
    <div
      ref={containerRef}
      className="mermaid-output flex justify-center overflow-x-auto"
      aria-label="Mermaid diagram"
      // biome-ignore lint/security/noDangerouslySetInnerHtml: mermaid produces sanitised SVG
      dangerouslySetInnerHTML={{ __html: svg }}
    />
  )
}
