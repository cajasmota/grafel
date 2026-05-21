/**
 * ExportDiagramButton — "Copy as diagram" control for the Entity Inspector.
 *
 * Shows a compact toolbar:
 *   [Mermaid ▼] [Copy as diagram]
 *
 * On click it calls GET /api/export/{group}/{entity_id}/{format}?depth=2
 * and writes the result to the clipboard. A "Copied!" flash confirms
 * success; errors degrade to a tooltip message.
 *
 * #1318
 */

import { useState, useCallback } from 'react'
import { Share2, Check, ChevronDown } from 'lucide-react'
import { fetchExportDSL, type ExportFormat } from '@/api/client'

// ── Format metadata ───────────────────────────────────────────────────────────

interface FormatMeta {
  label: string
  hint: string
}

const FORMATS: Record<ExportFormat, FormatMeta> = {
  mermaid: { label: 'Mermaid', hint: 'Paste into GH issues, Notion, HackMD…' },
  graphviz: { label: 'Graphviz DOT', hint: 'Render with dot / xdot / Graphviz Online' },
  plantuml: { label: 'PlantUML', hint: 'Paste into PlantUML Online / Confluence' },
  d2: { label: 'D2', hint: 'Render with the d2 CLI or Terrastruct playground' },
}

const FORMAT_ORDER: ExportFormat[] = ['mermaid', 'graphviz', 'plantuml', 'd2']

// ── Props ─────────────────────────────────────────────────────────────────────

interface ExportDiagramButtonProps {
  group: string
  entityId: string
  /** BFS depth for subgraph traversal (default 2) */
  depth?: number
}

// ── Component ─────────────────────────────────────────────────────────────────

export function ExportDiagramButton({
  group,
  entityId,
  depth = 2,
}: ExportDiagramButtonProps) {
  const [format, setFormat] = useState<ExportFormat>('mermaid')
  const [dropdownOpen, setDropdownOpen] = useState(false)
  const [state, setState] = useState<'idle' | 'loading' | 'copied' | 'error'>('idle')
  const [errorMsg, setErrorMsg] = useState('')

  const handleCopy = useCallback(async () => {
    if (state === 'loading') return
    setState('loading')
    setErrorMsg('')

    try {
      const dsl = await fetchExportDSL(group, entityId, format, { depth })
      await navigator.clipboard.writeText(dsl)
      setState('copied')
      setTimeout(() => setState('idle'), 2000)
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Copy failed'
      setErrorMsg(msg)
      setState('error')
      setTimeout(() => setState('idle'), 3000)
    }
  }, [group, entityId, format, depth, state])

  const handleSelectFormat = useCallback((f: ExportFormat) => {
    setFormat(f)
    setDropdownOpen(false)
  }, [])

  const copyLabel =
    state === 'loading' ? 'Copying…'
    : state === 'copied' ? 'Copied!'
    : state === 'error' ? 'Error'
    : 'Copy as diagram'

  return (
    <div className="flex flex-col gap-1" data-testid="export-diagram-button">
      <div className="flex items-center gap-1">
        {/* Format picker */}
        <div className="relative">
          <button
            type="button"
            onClick={() => setDropdownOpen((v) => !v)}
            aria-haspopup="listbox"
            aria-expanded={dropdownOpen}
            aria-label={`Export format: ${FORMATS[format].label}`}
            title={FORMATS[format].hint}
            className="flex items-center gap-1 px-2 py-1 rounded text-xs border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-800 transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400"
          >
            <span>{FORMATS[format].label}</span>
            <ChevronDown className="w-3 h-3 text-slate-400" />
          </button>

          {dropdownOpen && (
            <ul
              role="listbox"
              aria-label="Diagram format"
              className="absolute bottom-full mb-1 left-0 z-20 min-w-[10rem] bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded shadow-lg py-1"
            >
              {FORMAT_ORDER.map((f) => (
                <li key={f} role="option" aria-selected={f === format}>
                  <button
                    type="button"
                    onClick={() => handleSelectFormat(f)}
                    className={`w-full text-left px-3 py-1.5 text-xs transition-colors hover:bg-slate-100 dark:hover:bg-slate-800 ${
                      f === format
                        ? 'text-sky-500 font-semibold'
                        : 'text-slate-700 dark:text-slate-300'
                    }`}
                  >
                    <span className="block">{FORMATS[f].label}</span>
                    <span className="block text-[10px] text-slate-400 dark:text-slate-600 mt-0.5">
                      {FORMATS[f].hint}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>

        {/* Copy button */}
        <button
          type="button"
          onClick={handleCopy}
          disabled={state === 'loading'}
          aria-label={copyLabel}
          title={state === 'error' ? errorMsg : FORMATS[format].hint}
          data-testid="copy-diagram-btn"
          className={`flex items-center gap-1.5 px-2 py-1 rounded text-xs transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400 ${
            state === 'copied'
              ? 'text-green-600 dark:text-green-400 bg-green-50 dark:bg-green-950/30'
              : state === 'error'
                ? 'text-red-500 dark:text-red-400 bg-red-50 dark:bg-red-950/30'
                : 'text-violet-500 hover:text-violet-400 hover:bg-violet-50 dark:hover:bg-violet-950/30'
          }`}
        >
          {state === 'copied'
            ? <Check className="w-3.5 h-3.5" />
            : <Share2 className="w-3.5 h-3.5" />}
          <span>{copyLabel}</span>
        </button>
      </div>

      {/* Error message */}
      {state === 'error' && errorMsg && (
        <p className="text-[10px] text-red-500 dark:text-red-400 pl-1">
          {errorMsg}
        </p>
      )}
    </div>
  )
}
