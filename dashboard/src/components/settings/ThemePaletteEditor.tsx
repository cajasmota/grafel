/**
 * ThemePaletteEditor — custom colour palette editor for the Settings surface.
 *
 * Renders per-component colour pickers + a JSON export/import panel.
 * Lives inside the Themes accordion section in /settings.
 */
import { useState, useRef } from 'react'
import { Download, Upload, RotateCcw, Check, AlertTriangle } from 'lucide-react'
import type { CustomPalette } from '@/api/settings'
import type { UseThemePresetReturn } from '@/hooks/useThemePreset'

interface ColorFieldProps {
  label: string
  note: string
  fieldKey: keyof CustomPalette
  value: string
  onChange: (v: string) => void
}

function ColorField({ label, note, fieldKey, value, onChange }: ColorFieldProps) {
  return (
    <div
      className="flex items-center gap-3 justify-between"
      data-testid={`palette-field-${fieldKey}`}
    >
      <div>
        <span className="block text-sm font-medium text-slate-700 dark:text-slate-300">{label}</span>
        <span className="block text-xs text-slate-400 dark:text-slate-500">{note}</span>
      </div>
      <div className="flex items-center gap-2 flex-shrink-0">
        {/* Native color picker */}
        <input
          type="color"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          className="w-9 h-9 rounded cursor-pointer border border-slate-300 dark:border-slate-700
            bg-transparent p-0.5"
          aria-label={`${label} colour picker`}
        />
        {/* Hex text input */}
        <input
          type="text"
          value={value}
          onChange={(e) => {
            const v = e.target.value
            if (/^#[0-9a-fA-F]{0,6}$/.test(v)) onChange(v)
          }}
          maxLength={7}
          className="w-24 rounded border border-slate-300 dark:border-slate-700
            bg-white dark:bg-slate-900 text-slate-800 dark:text-slate-200
            px-2 py-1 text-xs font-mono focus:outline-none focus:ring-2 focus:ring-sky-500"
          aria-label={`${label} hex value`}
        />
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────

interface ThemePaletteEditorProps {
  palette: CustomPalette
  onChangePalette: UseThemePresetReturn['setPalette']
  onExport: UseThemePresetReturn['exportPaletteJSON']
  onImport: UseThemePresetReturn['importPaletteJSON']
  onReset: UseThemePresetReturn['resetPalette']
}

const FIELDS: Array<{
  key: keyof CustomPalette
  label: string
  note: string
}> = [
  { key: 'bg',       label: 'Background',        note: 'Main page background' },
  { key: 'bg_card',  label: 'Card / panel',       note: 'Sidebar, modal, card surface' },
  { key: 'bg_input', label: 'Input',              note: 'Text inputs and select boxes' },
  { key: 'fg',       label: 'Foreground',         note: 'Primary text colour' },
  { key: 'fg_muted', label: 'Muted text',         note: 'Secondary / hint text' },
  { key: 'border',   label: 'Border',             note: 'Dividers and outline rings' },
  { key: 'accent',   label: 'Accent',             note: 'Buttons, links, focus rings' },
  { key: 'accent_fg',label: 'Accent foreground',  note: 'Text on accent-coloured backgrounds' },
]

export function ThemePaletteEditor({
  palette,
  onChangePalette,
  onExport,
  onImport,
  onReset,
}: ThemePaletteEditorProps) {
  const [importText, setImportText] = useState('')
  const [importError, setImportError] = useState<string | null>(null)
  const [importOk, setImportOk] = useState(false)
  const [exported, setExported] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)

  function handleExport() {
    const json = onExport()
    navigator.clipboard.writeText(json).catch(() => {/* ignore */})
    setExported(true)
    setTimeout(() => setExported(false), 2000)
  }

  function handleImportText() {
    setImportError(null)
    const result = onImport(importText)
    if (result.ok) {
      setImportOk(true)
      setImportText('')
      setTimeout(() => setImportOk(false), 2000)
    } else {
      setImportError(result.error ?? 'Invalid palette JSON')
    }
  }

  function handleFileImport(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (!file) return
    const reader = new FileReader()
    reader.onload = (ev) => {
      const text = ev.target?.result as string
      setImportText(text)
      const result = onImport(text)
      if (result.ok) {
        setImportOk(true)
        setTimeout(() => setImportOk(false), 2000)
      } else {
        setImportError(result.error ?? 'Invalid palette JSON')
      }
    }
    reader.readAsText(file)
    // Reset so same file can be re-uploaded
    e.target.value = ''
  }

  return (
    <div className="space-y-4 pt-1" data-testid="palette-editor">
      {/* Color fields */}
      <div className="space-y-3">
        {FIELDS.map((f) => (
          <ColorField
            key={f.key}
            label={f.label}
            note={f.note}
            fieldKey={f.key}
            value={palette[f.key]}
            onChange={(v) => onChangePalette({ [f.key]: v })}
          />
        ))}
      </div>

      {/* Live preview swatch row */}
      <div
        className="flex gap-2 p-3 rounded border border-slate-200 dark:border-slate-700"
        aria-label="Palette preview"
        data-testid="palette-preview"
      >
        {FIELDS.map((f) => (
          <div
            key={f.key}
            title={f.label}
            className="flex-1 h-6 rounded"
            style={{ backgroundColor: palette[f.key] }}
          />
        ))}
      </div>

      {/* Reset + Export actions */}
      <div className="flex gap-2 flex-wrap">
        <button
          type="button"
          onClick={onReset}
          className="flex items-center gap-1.5 px-3 py-1.5 rounded text-xs font-medium
            border border-slate-300 dark:border-slate-700 text-slate-600 dark:text-slate-400
            hover:bg-slate-50 dark:hover:bg-slate-800 transition-colors"
          data-testid="palette-reset-btn"
        >
          <RotateCcw className="w-3.5 h-3.5" />
          Reset to defaults
        </button>

        <button
          type="button"
          onClick={handleExport}
          className="flex items-center gap-1.5 px-3 py-1.5 rounded text-xs font-medium
            border border-slate-300 dark:border-slate-700 text-slate-600 dark:text-slate-400
            hover:bg-slate-50 dark:hover:bg-slate-800 transition-colors"
          data-testid="palette-export-btn"
        >
          {exported
            ? <Check className="w-3.5 h-3.5 text-green-500" />
            : <Download className="w-3.5 h-3.5" />}
          {exported ? 'Copied!' : 'Export JSON'}
        </button>
      </div>

      {/* Import panel */}
      <div className="space-y-2">
        <span className="text-xs font-medium text-slate-600 dark:text-slate-400">
          Import palette JSON
        </span>
        <textarea
          value={importText}
          onChange={(e) => { setImportText(e.target.value); setImportError(null) }}
          placeholder='{ "bg": "#002b36", "fg": "#839496", … }'
          rows={4}
          className="w-full rounded border border-slate-300 dark:border-slate-700
            bg-white dark:bg-slate-900 text-slate-800 dark:text-slate-200
            px-3 py-2 text-xs font-mono resize-y focus:outline-none focus:ring-2 focus:ring-sky-500"
          data-testid="palette-import-textarea"
        />
        {importError && (
          <div className="flex items-center gap-1.5 text-xs text-red-500">
            <AlertTriangle className="w-3.5 h-3.5 flex-shrink-0" />
            {importError}
          </div>
        )}
        {importOk && (
          <div className="flex items-center gap-1.5 text-xs text-green-500">
            <Check className="w-3.5 h-3.5" /> Palette applied!
          </div>
        )}
        <div className="flex gap-2">
          <button
            type="button"
            onClick={handleImportText}
            disabled={!importText.trim()}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded text-xs font-medium
              bg-sky-500 text-white hover:bg-sky-600 disabled:opacity-40 disabled:cursor-not-allowed
              transition-colors"
            data-testid="palette-import-apply-btn"
          >
            <Upload className="w-3.5 h-3.5" />
            Apply
          </button>

          {/* File upload shortcut */}
          <button
            type="button"
            onClick={() => fileInputRef.current?.click()}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded text-xs font-medium
              border border-slate-300 dark:border-slate-700 text-slate-600 dark:text-slate-400
              hover:bg-slate-50 dark:hover:bg-slate-800 transition-colors"
            data-testid="palette-import-file-btn"
          >
            Load from file
          </button>
          <input
            ref={fileInputRef}
            type="file"
            accept="application/json,.json"
            onChange={handleFileImport}
            className="sr-only"
            aria-hidden
          />
        </div>
      </div>
    </div>
  )
}
