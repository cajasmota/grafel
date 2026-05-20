import { useRef, useCallback } from 'react'
import { Search, X } from 'lucide-react'
import { debounce } from '@/lib/pathUtils'

interface PathSearchInputProps {
  value: string
  onChange: (value: string) => void
  placeholder?: string
}

export function PathSearchInput({
  value,
  onChange,
  placeholder = 'Search paths… (press / to focus)',
}: PathSearchInputProps) {
  const inputRef = useRef<HTMLInputElement>(null)

  // Debounce the onChange call so we don't fire a query on every keypress
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const debouncedChange = useCallback(
    debounce((v: unknown) => onChange(v as string), 250),
    [onChange],
  )

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    debouncedChange(e.target.value)
  }

  const handleClear = () => {
    onChange('')
    if (inputRef.current) {
      inputRef.current.value = ''
      inputRef.current.focus()
    }
  }

  return (
    <div className="relative flex-1">
      <Search
        className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-400 dark:text-slate-500 pointer-events-none"
        aria-hidden
      />
      <input
        ref={inputRef}
        type="search"
        role="searchbox"
        aria-label="Search API paths"
        defaultValue={value}
        onChange={handleChange}
        placeholder={placeholder}
        className={[
          'w-full bg-slate-100 dark:bg-slate-900 border border-slate-300 dark:border-slate-700 rounded-lg',
          'pl-9 pr-8 py-2 text-sm text-slate-800 dark:text-slate-200 placeholder:text-slate-500',
          'focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500',
          'transition-colors',
        ].join(' ')}
      />
      {value && (
        <button
          type="button"
          aria-label="Clear search"
          className="absolute right-2 top-1/2 -translate-y-1/2 text-slate-400 dark:text-slate-500 hover:text-slate-700 dark:hover:text-slate-300"
          onClick={handleClear}
        >
          <X className="w-4 h-4" />
        </button>
      )}
    </div>
  )
}
