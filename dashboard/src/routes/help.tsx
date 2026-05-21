/**
 * HelpRoute — /help
 *
 * Sections:
 *   1. About — version, commit, license, links
 *   2. Tour — animated walkthrough of each surface
 *   3. Glossary — archigraph-specific terms with definitions
 *   4. Tips & Tricks — power-user shortcuts
 *   5. FAQ — common questions and answers
 *   6. Contact / Report — GitHub issues + diagnostic report link
 *
 * Keyboard shortcut: ⌘? (Cmd+Shift+/) opens this route (wired in _layout.tsx).
 */

import { useState, useEffect, useRef } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import {
  HelpCircle, Info, MapPin, BookOpen, Lightbulb,
  MessageSquare, ExternalLink, ChevronDown, ChevronRight,
  ChevronLeft, Search, GitCommit, FileText, Zap,
  ArrowRight, Play,
} from 'lucide-react'
import { fetchInfo } from '@/api/client'
import {
  TOUR_SLIDES,
  GLOSSARY_TERMS,
  TIPS,
  FAQ_ITEMS,
  type TourSlide,
  type GlossaryTerm,
  type FaqItem,
  type Tip,
} from '@/lib/helpContent'

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

function shortSHA(commit: string): string {
  if (!commit || commit === 'unknown') return 'unknown'
  return commit.length > 7 ? commit.slice(0, 7) : commit
}

function SectionCard({
  id,
  icon,
  title,
  defaultOpen = false,
  children,
}: {
  id: string
  icon: React.ReactNode
  title: string
  defaultOpen?: boolean
  children: React.ReactNode
}) {
  const [open, setOpen] = useState(defaultOpen)

  return (
    <section
      id={id}
      data-testid={`help-section-${id}`}
      className="border border-slate-200 dark:border-slate-800 rounded-xl overflow-hidden"
    >
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="w-full flex items-center gap-3 px-6 py-4 text-left
          bg-slate-50 dark:bg-slate-900 hover:bg-slate-100 dark:hover:bg-slate-800
          transition-colors"
        aria-expanded={open}
        aria-controls={`${id}-body`}
      >
        <span className="text-sky-400 flex-shrink-0">{icon}</span>
        <span className="flex-1 font-semibold text-slate-800 dark:text-slate-200">{title}</span>
        {open
          ? <ChevronDown className="w-4 h-4 text-slate-400 flex-shrink-0" />
          : <ChevronRight className="w-4 h-4 text-slate-400 flex-shrink-0" />}
      </button>
      {open && (
        <div
          id={`${id}-body`}
          className="px-6 py-6 space-y-4 bg-white dark:bg-slate-950"
        >
          {children}
        </div>
      )}
    </section>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Section 1 — About
// ─────────────────────────────────────────────────────────────────────────────

function AboutSection() {
  const { data: info } = useQuery({
    queryKey: ['info'],
    queryFn: fetchInfo,
    staleTime: 60_000,
  })

  const commitSHA = info ? shortSHA(info.commit) : null
  const commitUrl =
    commitSHA && commitSHA !== 'unknown'
      ? `https://github.com/cajasmota/archigraph/commit/${info!.commit}`
      : null

  return (
    <SectionCard id="about" icon={<Info className="w-5 h-5" />} title="About" defaultOpen>
      <div className="flex flex-col sm:flex-row gap-6">
        {/* Left: version block */}
        <div className="space-y-3 flex-1">
          <div>
            <p className="text-2xl font-bold text-sky-400 tracking-tight">archigraph</p>
            {info ? (
              <p className="text-slate-500 dark:text-slate-400 text-sm mt-0.5">
                Version{' '}
                <span className="font-mono text-slate-700 dark:text-slate-300">{info.version}</span>
              </p>
            ) : (
              <p className="text-slate-400 text-sm mt-0.5">Loading version…</p>
            )}
          </div>

          {info && (
            <dl className="space-y-1.5 text-sm">
              {commitSHA && (
                <div className="flex items-center gap-2 text-slate-600 dark:text-slate-400">
                  <GitCommit className="w-3.5 h-3.5 flex-shrink-0" />
                  <dt className="sr-only">Commit</dt>
                  <dd>
                    Commit:{' '}
                    {commitUrl ? (
                      <a
                        href={commitUrl}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="font-mono text-sky-600 dark:text-sky-400 hover:underline"
                        data-testid="help-commit-link"
                      >
                        {commitSHA}
                      </a>
                    ) : (
                      <span className="font-mono text-slate-700 dark:text-slate-300">{commitSHA}</span>
                    )}
                  </dd>
                </div>
              )}
              {info.built_at && info.built_at !== 'unknown' && (
                <div className="flex items-center gap-2 text-slate-600 dark:text-slate-400">
                  <FileText className="w-3.5 h-3.5 flex-shrink-0" />
                  <span>
                    Built:{' '}
                    <span className="text-slate-700 dark:text-slate-300 font-mono text-xs">
                      {info.built_at}
                    </span>
                  </span>
                </div>
              )}
            </dl>
          )}

          <p className="text-xs text-slate-400 dark:text-slate-500">
            Licensed under the MIT License.
          </p>
        </div>

        {/* Right: links */}
        <div className="space-y-2 sm:w-48">
          <p className="text-xs font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-500">
            Links
          </p>
          <a
            href="https://github.com/cajasmota/archigraph"
            target="_blank"
            rel="noopener noreferrer"
            className="flex items-center gap-2 text-sm text-sky-600 dark:text-sky-400 hover:text-sky-500 dark:hover:text-sky-300 transition-colors"
          >
            <ExternalLink className="w-3.5 h-3.5 flex-shrink-0" />
            GitHub repository
          </a>
          <a
            href="https://github.com/cajasmota/archigraph/issues"
            target="_blank"
            rel="noopener noreferrer"
            className="flex items-center gap-2 text-sm text-sky-600 dark:text-sky-400 hover:text-sky-500 dark:hover:text-sky-300 transition-colors"
          >
            <ExternalLink className="w-3.5 h-3.5 flex-shrink-0" />
            Issue tracker
          </a>
          <Link
            to="/settings"
            className="flex items-center gap-2 text-sm text-slate-600 dark:text-slate-400 hover:text-sky-400 dark:hover:text-sky-300 transition-colors"
          >
            <ArrowRight className="w-3.5 h-3.5 flex-shrink-0" />
            Settings
          </Link>
          <Link
            to="/system"
            className="flex items-center gap-2 text-sm text-slate-600 dark:text-slate-400 hover:text-sky-400 dark:hover:text-sky-300 transition-colors"
          >
            <ArrowRight className="w-3.5 h-3.5 flex-shrink-0" />
            System panel
          </Link>
        </div>
      </div>
    </SectionCard>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Section 2 — Tour
// ─────────────────────────────────────────────────────────────────────────────

function TourSection() {
  const navigate = useNavigate()
  const [current, setCurrent] = useState(0)
  const [animating, setAnimating] = useState(false)
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const [playing, setPlaying] = useState(false)

  const slide: TourSlide = TOUR_SLIDES[current]
  const total = TOUR_SLIDES.length

  function go(idx: number) {
    if (animating) return
    setAnimating(true)
    setCurrent((idx + total) % total)
    setTimeout(() => setAnimating(false), 300)
  }

  function prev() { go(current - 1) }
  function next() { go(current + 1) }

  useEffect(() => {
    if (playing) {
      intervalRef.current = setInterval(() => {
        setCurrent((c) => (c + 1) % total)
      }, 3500)
    } else {
      if (intervalRef.current) clearInterval(intervalRef.current)
    }
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current)
    }
  }, [playing, total])

  return (
    <SectionCard id="tour" icon={<MapPin className="w-5 h-5" />} title="Tour">
      <div data-testid="tour-container" className="space-y-4">
        {/* Slide */}
        <div
          className={[
            'rounded-lg border border-slate-200 dark:border-slate-800 overflow-hidden',
            'transition-opacity duration-300',
            animating ? 'opacity-0' : 'opacity-100',
          ].join(' ')}
        >
          {/* Slide header */}
          <div className="px-5 py-4 bg-slate-50 dark:bg-slate-900">
            <p
              className="font-semibold text-slate-800 dark:text-slate-200"
              data-testid="tour-slide-title"
            >
              {slide.title}
            </p>
            <p className="text-xs text-slate-400 dark:text-slate-500 mt-0.5">
              {current + 1} / {total}
            </p>
          </div>

          {/* Slide body */}
          <div className="px-5 py-4 bg-white dark:bg-slate-950">
            <p
              className="text-sm text-slate-600 dark:text-slate-400 leading-relaxed"
              data-testid="tour-slide-description"
            >
              {slide.description}
            </p>
          </div>
        </div>

        {/* Controls */}
        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={prev}
            disabled={animating}
            aria-label="Previous slide"
            data-testid="tour-prev"
            className="p-1.5 rounded border border-slate-200 dark:border-slate-700
              text-slate-600 dark:text-slate-400 hover:bg-slate-50 dark:hover:bg-slate-800
              disabled:opacity-40 transition-colors"
          >
            <ChevronLeft className="w-4 h-4" />
          </button>

          {/* Dot indicators */}
          <div className="flex gap-1.5 flex-1 justify-center" aria-label="Slide indicators">
            {TOUR_SLIDES.map((s, i) => (
              <button
                key={s.id}
                type="button"
                onClick={() => go(i)}
                aria-label={`Go to slide ${i + 1}: ${s.title}`}
                aria-current={i === current ? 'true' : undefined}
                className={[
                  'w-2 h-2 rounded-full transition-colors',
                  i === current
                    ? 'bg-sky-400'
                    : 'bg-slate-300 dark:bg-slate-700 hover:bg-slate-400 dark:hover:bg-slate-600',
                ].join(' ')}
              />
            ))}
          </div>

          <button
            type="button"
            onClick={next}
            disabled={animating}
            aria-label="Next slide"
            data-testid="tour-next"
            className="p-1.5 rounded border border-slate-200 dark:border-slate-700
              text-slate-600 dark:text-slate-400 hover:bg-slate-50 dark:hover:bg-slate-800
              disabled:opacity-40 transition-colors"
          >
            <ChevronRight className="w-4 h-4" />
          </button>

          <button
            type="button"
            onClick={() => setPlaying((p) => !p)}
            aria-label={playing ? 'Pause auto-advance' : 'Play auto-advance'}
            data-testid="tour-play-pause"
            className={[
              'p-1.5 rounded border text-sm transition-colors',
              playing
                ? 'border-sky-400 text-sky-400 bg-sky-50 dark:bg-sky-950/30'
                : 'border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-400 hover:bg-slate-50 dark:hover:bg-slate-800',
            ].join(' ')}
          >
            <Play className="w-4 h-4" />
          </button>

          <button
            type="button"
            onClick={() => navigate(slide.route)}
            data-testid="tour-go-to-surface"
            className="flex items-center gap-1.5 px-3 py-1.5 rounded text-sm font-medium
              bg-sky-500 text-white hover:bg-sky-600 transition-colors"
          >
            Open surface
            <ArrowRight className="w-3.5 h-3.5" />
          </button>
        </div>
      </div>
    </SectionCard>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Section 3 — Glossary
// ─────────────────────────────────────────────────────────────────────────────

function GlossaryItem({ item }: { item: GlossaryTerm }) {
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)

  return (
    <div
      className="border-b border-slate-100 dark:border-slate-800 last:border-0 py-3"
      data-testid={`glossary-term-${item.term.toLowerCase().replace(/\s+/g, '-')}`}
    >
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="w-full text-left flex items-center gap-2 group"
      >
        <span className="flex-1 font-medium text-sm text-slate-800 dark:text-slate-200 group-hover:text-sky-500 transition-colors">
          {item.term}
        </span>
        {open
          ? <ChevronDown className="w-3.5 h-3.5 text-slate-400 flex-shrink-0" />
          : <ChevronRight className="w-3.5 h-3.5 text-slate-400 flex-shrink-0" />}
      </button>

      {open && (
        <div className="mt-2 pl-0 space-y-2">
          <p className="text-sm text-slate-600 dark:text-slate-400 leading-relaxed">
            {item.definition}
          </p>
          {item.relatedRoute && (
            <button
              type="button"
              onClick={() => navigate(item.relatedRoute!)}
              className="flex items-center gap-1 text-xs text-sky-600 dark:text-sky-400 hover:text-sky-500 dark:hover:text-sky-300 transition-colors"
            >
              <ArrowRight className="w-3 h-3" />
              See in app
            </button>
          )}
        </div>
      )}
    </div>
  )
}

function GlossarySection() {
  const [query, setQuery] = useState('')

  const filtered = query.trim()
    ? GLOSSARY_TERMS.filter(
        (t) =>
          t.term.toLowerCase().includes(query.toLowerCase()) ||
          t.definition.toLowerCase().includes(query.toLowerCase()),
      )
    : GLOSSARY_TERMS

  return (
    <SectionCard id="glossary" icon={<BookOpen className="w-5 h-5" />} title="Glossary">
      {/* Search */}
      <div className="relative">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-slate-400 pointer-events-none" />
        <input
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search terms…"
          aria-label="Search glossary"
          data-testid="glossary-search"
          className="w-full pl-8 pr-3 py-2 text-sm rounded-lg border border-slate-200 dark:border-slate-700
            bg-white dark:bg-slate-900 text-slate-800 dark:text-slate-200
            placeholder-slate-400 dark:placeholder-slate-600
            focus:outline-none focus:ring-2 focus:ring-sky-500"
        />
      </div>

      {/* Terms */}
      <div data-testid="glossary-list">
        {filtered.length === 0 ? (
          <p className="text-sm text-slate-400 py-4 text-center">No terms match "{query}"</p>
        ) : (
          filtered.map((term) => <GlossaryItem key={term.term} item={term} />)
        )}
      </div>
    </SectionCard>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Section 4 — Tips & Tricks
// ─────────────────────────────────────────────────────────────────────────────

function TipCard({ tip }: { tip: Tip }) {
  return (
    <div
      data-testid={`tip-${tip.id}`}
      className="flex items-start gap-3 p-3 rounded-lg border border-slate-100 dark:border-slate-800
        bg-slate-50 dark:bg-slate-900"
    >
      <Lightbulb className="w-4 h-4 text-sky-400 flex-shrink-0 mt-0.5" />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 flex-wrap">
          <p className="font-medium text-sm text-slate-800 dark:text-slate-200">{tip.title}</p>
          {tip.shortcut && (
            <kbd className="px-1.5 py-0.5 rounded text-[10px] font-mono bg-white dark:bg-slate-800
              border border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-400">
              {tip.shortcut}
            </kbd>
          )}
        </div>
        <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5 leading-relaxed">
          {tip.description}
        </p>
      </div>
    </div>
  )
}

function TipsSection() {
  return (
    <SectionCard id="tips" icon={<Lightbulb className="w-5 h-5" />} title="Tips & Tricks">
      <div className="grid gap-2 sm:grid-cols-2" data-testid="tips-list">
        {TIPS.map((tip) => (
          <TipCard key={tip.id} tip={tip} />
        ))}
      </div>
    </SectionCard>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Section 5 — FAQ
// ─────────────────────────────────────────────────────────────────────────────

function FaqItem({ item }: { item: FaqItem }) {
  const [open, setOpen] = useState(false)

  return (
    <div
      className="border-b border-slate-100 dark:border-slate-800 last:border-0 py-3"
      data-testid={`faq-${item.id}`}
    >
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="w-full text-left flex items-start gap-2 group"
        aria-expanded={open}
      >
        <span className="flex-1 text-sm font-medium text-slate-800 dark:text-slate-200 group-hover:text-sky-500 transition-colors">
          {item.question}
        </span>
        {open
          ? <ChevronDown className="w-4 h-4 text-slate-400 flex-shrink-0 mt-0.5" />
          : <ChevronRight className="w-4 h-4 text-slate-400 flex-shrink-0 mt-0.5" />}
      </button>
      {open && (
        <p className="mt-2 text-sm text-slate-600 dark:text-slate-400 leading-relaxed">
          {item.answer}
        </p>
      )}
    </div>
  )
}

function FaqSection() {
  const [query, setQuery] = useState('')

  const filtered = query.trim()
    ? FAQ_ITEMS.filter(
        (f) =>
          f.question.toLowerCase().includes(query.toLowerCase()) ||
          f.answer.toLowerCase().includes(query.toLowerCase()),
      )
    : FAQ_ITEMS

  return (
    <SectionCard id="faq" icon={<MessageSquare className="w-5 h-5" />} title="FAQ">
      {/* Search */}
      <div className="relative">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-slate-400 pointer-events-none" />
        <input
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search questions…"
          aria-label="Search FAQ"
          data-testid="faq-search"
          className="w-full pl-8 pr-3 py-2 text-sm rounded-lg border border-slate-200 dark:border-slate-700
            bg-white dark:bg-slate-900 text-slate-800 dark:text-slate-200
            placeholder-slate-400 dark:placeholder-slate-600
            focus:outline-none focus:ring-2 focus:ring-sky-500"
        />
      </div>

      <div data-testid="faq-list">
        {filtered.length === 0 ? (
          <p className="text-sm text-slate-400 py-4 text-center">No results for "{query}"</p>
        ) : (
          filtered.map((item) => <FaqItem key={item.id} item={item} />)
        )}
      </div>
    </SectionCard>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Section 6 — Contact / Report
// ─────────────────────────────────────────────────────────────────────────────

function ContactSection() {
  const [copied, setCopied] = useState(false)

  async function copyDiagnostics() {
    try {
      const res = await fetch('/api/diagnostics/report')
      const text = await res.text()
      await navigator.clipboard.writeText(text)
    } catch {
      // Fallback: copy placeholder text
      await navigator.clipboard.writeText('archigraph diagnostic report — daemon not reachable')
    }
    setCopied(true)
    setTimeout(() => setCopied(false), 2500)
  }

  return (
    <SectionCard id="contact" icon={<Zap className="w-5 h-5" />} title="Contact / Report an Issue">
      <div className="space-y-4">
        <p className="text-sm text-slate-600 dark:text-slate-400">
          Found a bug or missing a feature? Open an issue on GitHub — include the diagnostic
          report below to speed up investigation.
        </p>

        <div className="flex flex-wrap gap-3">
          <a
            href="https://github.com/cajasmota/archigraph/issues/new"
            target="_blank"
            rel="noopener noreferrer"
            data-testid="contact-new-issue-link"
            className="flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium
              bg-sky-500 text-white hover:bg-sky-600 transition-colors"
          >
            <ExternalLink className="w-3.5 h-3.5" />
            Open GitHub issue
          </a>

          <button
            type="button"
            onClick={copyDiagnostics}
            data-testid="contact-copy-diagnostics-btn"
            className="flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium
              border border-slate-200 dark:border-slate-700
              text-slate-700 dark:text-slate-300
              hover:bg-slate-50 dark:hover:bg-slate-800 transition-colors"
          >
            {copied ? (
              <>Copied!</>
            ) : (
              <>
                <FileText className="w-3.5 h-3.5" />
                Copy diagnostic report
              </>
            )}
          </button>
        </div>

        <p className="text-xs text-slate-400 dark:text-slate-500">
          The diagnostic report contains version, commit, daemon status, and indexing metrics.
          No source code or file contents are included.
        </p>
      </div>
    </SectionCard>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Page header with section quick-links
// ─────────────────────────────────────────────────────────────────────────────

const SECTIONS = [
  { id: 'about',   label: 'About' },
  { id: 'tour',    label: 'Tour' },
  { id: 'glossary', label: 'Glossary' },
  { id: 'tips',    label: 'Tips & Tricks' },
  { id: 'faq',     label: 'FAQ' },
  { id: 'contact', label: 'Contact' },
]

function PageHeader() {
  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-3">
        <HelpCircle className="w-7 h-7 text-sky-400 flex-shrink-0" />
        <div>
          <h1 className="text-2xl font-bold text-slate-900 dark:text-slate-100">Help & About</h1>
          <p className="text-sm text-slate-500 dark:text-slate-400 mt-0.5">
            User manual, glossary, tips, and support links — all in one place.
          </p>
        </div>
      </div>

      {/* Quick-jump pills */}
      <div className="flex flex-wrap gap-2" role="navigation" aria-label="Help sections">
        {SECTIONS.map((s) => (
          <a
            key={s.id}
            href={`#${s.id}`}
            className="px-3 py-1 rounded-full text-xs font-medium
              bg-slate-100 dark:bg-slate-800
              text-slate-600 dark:text-slate-400
              hover:bg-sky-50 dark:hover:bg-sky-950/40
              hover:text-sky-600 dark:hover:text-sky-400
              border border-slate-200 dark:border-slate-700
              transition-colors"
          >
            {s.label}
          </a>
        ))}
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// HelpRoute — exported
// ─────────────────────────────────────────────────────────────────────────────

export function HelpRoute() {
  return (
    <div className="h-full overflow-y-auto" data-testid="help-page">
      <div className="max-w-3xl mx-auto px-4 py-8 space-y-5">
        <PageHeader />
        <AboutSection />
        <TourSection />
        <GlossarySection />
        <TipsSection />
        <FaqSection />
        <ContactSection />
      </div>
    </div>
  )
}
