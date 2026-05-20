import { useState, useCallback } from 'react'
import { Highlight } from 'prism-react-renderer'
import { Check, Copy } from 'lucide-react'
import { cn } from '@/lib/utils'

// prism-react-renderer ships these built-in:
// javascript, jsx, typescript, tsx, python, go, sql, bash/shell, yaml, json, css, markup
// No additional language imports needed for our use cases.

interface CodeBlockProps {
  code: string
  language?: string
  showLineNumbers?: boolean
  filename?: string
}

/**
 * Prism-powered code block.
 * Features: syntax highlighting, copy button, language label, optional line numbers.
 * Both the copy button and the block are keyboard-accessible.
 */
export function CodeBlock({ code, language = 'text', showLineNumbers = false, filename }: CodeBlockProps) {
  const [copied, setCopied] = useState(false)

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(code)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // clipboard not available
    }
  }, [code])

  // Map common language aliases
  const prismLanguage = LANGUAGE_ALIASES[language.toLowerCase()] ?? language.toLowerCase()

  return (
    <figure
      className="my-4 rounded-lg overflow-hidden border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-950"
      aria-label={`Code block: ${language}`}
    >
      {/* Header */}
      <div className="flex items-center justify-between gap-2 px-4 py-2 bg-slate-100 dark:bg-slate-900 border-b border-slate-200 dark:border-slate-800">
        <div className="flex items-center gap-2 min-w-0">
          {filename && (
            <span className="text-xs font-mono text-slate-400 dark:text-slate-400 truncate">{filename}</span>
          )}
          {!filename && language && language !== 'text' && (
            <span className="text-xs font-mono text-slate-400 dark:text-slate-500">{language}</span>
          )}
        </div>
        <button
          type="button"
          aria-label={copied ? 'Copied to clipboard' : 'Copy code to clipboard'}
          className={cn(
            'flex items-center gap-1.5 px-2 py-1 rounded text-xs transition-colors',
            copied
              ? 'text-emerald-400 bg-emerald-950/50'
              : 'text-slate-400 dark:text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 hover:bg-slate-200 dark:hover:bg-slate-800',
          )}
          onClick={handleCopy}
        >
          {copied ? (
            <>
              <Check className="w-3 h-3" aria-hidden />
              Copied
            </>
          ) : (
            <>
              <Copy className="w-3 h-3" aria-hidden />
              Copy
            </>
          )}
        </button>
      </div>

      {/* Code content */}
      <Highlight code={code.trimEnd()} language={prismLanguage as never}>
        {({ className, style, tokens, getLineProps, getTokenProps }) => (
          <pre
            className={cn(
              className,
              'overflow-x-auto p-4 text-sm leading-relaxed',
              showLineNumbers && 'pl-0',
            )}
            style={{ ...style, background: 'transparent', margin: 0 }}
            tabIndex={0}
          >
            {tokens.map((line, i) => {
              const lineProps = getLineProps({ line })
              return (
                <div
                  key={i}
                  {...lineProps}
                  className={cn(lineProps.className, 'table-row')}
                >
                  {showLineNumbers && (
                    <span
                      className="table-cell pr-4 text-right select-none text-slate-500 dark:text-slate-600 text-xs"
                      aria-hidden
                    >
                      {i + 1}
                    </span>
                  )}
                  <span className="table-cell">
                    {line.map((token, key) => (
                      <span key={key} {...getTokenProps({ token })} />
                    ))}
                  </span>
                </div>
              )
            })}
          </pre>
        )}
      </Highlight>
    </figure>
  )
}

const LANGUAGE_ALIASES: Record<string, string> = {
  js: 'javascript',
  ts: 'typescript',
  py: 'python',
  sh: 'bash',
  shell: 'bash',
  yml: 'yaml',
}
