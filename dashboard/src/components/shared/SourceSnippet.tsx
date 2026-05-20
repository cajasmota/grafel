/**
 * Renders a code snippet for an entity's source location.
 * In mock mode renders a placeholder; in real mode would call useEntitySource.
 */

interface SourceSnippetProps {
  sourceFile: string
  startLine: number
  endLine?: number
  language?: string
  /** If provided, shows the raw code content */
  code?: string
}

export function SourceSnippet({ sourceFile, startLine, endLine, language, code }: SourceSnippetProps) {
  return (
    <div className="rounded-lg overflow-hidden border border-slate-200 dark:border-slate-800 text-xs">
      {/* Header */}
      <div className="flex items-center justify-between px-3 py-1.5 bg-slate-100 dark:bg-slate-900 border-b border-slate-200 dark:border-slate-800">
        <span className="font-mono text-slate-400 dark:text-slate-400">
          {sourceFile}
          <span className="text-slate-500 dark:text-slate-600">:{startLine}</span>
          {endLine && endLine !== startLine && (
            <span className="text-slate-500 dark:text-slate-600">–{endLine}</span>
          )}
        </span>
        {language && (
          <span className="text-slate-500 dark:text-slate-600 uppercase tracking-wider">{language}</span>
        )}
      </div>

      {/* Code body */}
      <div className="bg-white dark:bg-slate-950 p-3 overflow-x-auto">
        {code ? (
          <pre className="font-mono text-slate-700 dark:text-slate-300 whitespace-pre leading-relaxed">
            <code>{code}</code>
          </pre>
        ) : (
          <p className="text-slate-500 dark:text-slate-600 italic">
            Source loading requires live Go server connection.
          </p>
        )}
      </div>
    </div>
  )
}
