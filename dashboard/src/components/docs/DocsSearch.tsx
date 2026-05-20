import { useState, useRef, useEffect, useId } from 'react'
import { Link } from 'react-router-dom'
import { Search, X } from 'lucide-react'
import { useDocsSearch } from '@/hooks/docs/useDocsSearch'
import { CardSkeleton } from '@/components/shared/LoadingState'

interface DocsSearchProps {
  group: string
}

/**
 * Top-bar typeahead search.
 * Keyboard: `/` from the page focuses the input; Escape closes the dropdown.
 * Results are fetched server-side — no client-side lunr bundle.
 */
export function DocsSearch({ group }: DocsSearchProps) {
  const [query, setQuery] = useState('')
  const [open, setOpen] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  const listboxId = useId()
  const { data, isFetching } = useDocsSearch(group, query)

  // `/` shortcut — focus search
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (
        e.key === '/' &&
        document.activeElement?.tagName !== 'INPUT' &&
        document.activeElement?.tagName !== 'TEXTAREA'
      ) {
        e.preventDefault()
        inputRef.current?.focus()
      }
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [])

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      setQuery('')
      setOpen(false)
      inputRef.current?.blur()
    }
  }

  const hasResults = Boolean(data?.results.length)
  const showDropdown = open && query.trim().length >= 2

  return (
    <div className="relative">
      <label htmlFor="docs-search-input" className="sr-only">Search documentation</label>
      <div className="flex items-center gap-2 px-3 py-1.5 rounded-lg bg-slate-200 dark:bg-slate-800 border border-slate-300 dark:border-slate-700 focus-within:border-sky-500 transition-colors">
        <Search className="w-4 h-4 text-slate-400 dark:text-slate-500 flex-shrink-0" aria-hidden />
        <input
          id="docs-search-input"
          ref={inputRef}
          type="search"
          value={query}
          placeholder="Search docs…"
          aria-label="Search documentation"
          aria-expanded={showDropdown}
          aria-controls={listboxId}
          aria-autocomplete="list"
          autoComplete="off"
          className="bg-transparent text-sm text-slate-800 dark:text-slate-200 placeholder:text-slate-500 outline-none min-w-0 w-40 focus:w-56 transition-all"
          onChange={(e) => {
            setQuery(e.target.value)
            setOpen(true)
          }}
          onFocus={() => setOpen(true)}
          onKeyDown={handleKeyDown}
        />
        {query && (
          <button
            type="button"
            aria-label="Clear search"
            className="text-slate-400 dark:text-slate-500 hover:text-slate-700 dark:hover:text-slate-300"
            onClick={() => { setQuery(''); setOpen(false) }}
          >
            <X className="w-3.5 h-3.5" aria-hidden />
          </button>
        )}
        {!query && (
          <kbd className="hidden sm:flex items-center px-1.5 py-0.5 rounded text-xs bg-slate-300 dark:bg-slate-700 text-slate-400 dark:text-slate-400 border border-slate-400 dark:border-slate-600" aria-label="Press / to focus search">
            /
          </kbd>
        )}
      </div>

      {showDropdown && (
        <div
          id={listboxId}
          role="listbox"
          aria-label="Search results"
          className="absolute right-0 mt-2 w-80 max-h-80 overflow-y-auto rounded-lg bg-slate-100 dark:bg-slate-900 border border-slate-300 dark:border-slate-700 shadow-xl z-50"
        >
          {isFetching && (
            <div className="p-3">
              <div className="animate-pulse h-4 rounded bg-slate-200 dark:bg-slate-800" role="status" aria-label="Searching…" />
            </div>
          )}
          {!isFetching && !hasResults && query.length >= 2 && (
            <div className="p-4 text-sm text-slate-400 dark:text-slate-500 text-center">
              No results for "{query}"
            </div>
          )}
          {!isFetching && hasResults && (
            <ul>
              {data!.results.map((result) => (
                <li key={result.path} role="option" aria-selected={false}>
                  <Link
                    to={`/docs/${group}/${result.path}`}
                    className="block px-4 py-3 hover:bg-slate-200 dark:hover:bg-slate-800 transition-colors border-b border-slate-200 dark:border-slate-800 last:border-0"
                    onClick={() => { setQuery(''); setOpen(false) }}
                  >
                    <div className="text-sm font-medium text-slate-800 dark:text-slate-200 mb-0.5">{result.title}</div>
                    <div className="text-xs text-slate-400 dark:text-slate-500 truncate">{result.excerpt}</div>
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  )
}
