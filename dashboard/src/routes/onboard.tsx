/**
 * OnboardRoute — /onboard
 *
 * Multi-step wizard for creating a new group from the web UI (#1239).
 *
 * Steps:
 *   0  Welcome     — daemon status chip + "Get started"
 *   1  Group name  — editable, pre-filled from path detection
 *   2  Add repos   — text-input path list with live validation
 *   3  Monorepo    — per-repo: show detected packages with checkboxes
 *   4  Confirm     — summary + "Start indexing" → IndexingProgressModal
 */

import { useState, useCallback, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  GitBranch, ChevronRight, ChevronLeft, Plus, Trash2,
  CheckCircle2, AlertCircle, Loader2, FolderOpen,
  Package, Layers, ArrowRight, Terminal, Activity,
} from 'lucide-react'
import {
  onboardCheckPath,
  onboardDetectMonorepo,
  onboardCreateGroup,
  type OnboardCheckPathReply,
  type OnboardDetectMonorepoReply,
} from '@/api/client'
import { IndexingProgressModal } from '@/components/indexing/IndexingProgressModal'
import { useRegistry } from '@/hooks/shared/useRegistry'
import { useQueryClient } from '@tanstack/react-query'

// ─────────────────────────────────────────────────────────────────────────────
// Types
// ─────────────────────────────────────────────────────────────────────────────

interface RepoEntry {
  id: string
  rawPath: string
  checkResult: OnboardCheckPathReply | null
  checking: boolean
  monorepo: OnboardDetectMonorepoReply | null
  selectedModules: string[]
}

type WizardStep = 0 | 1 | 2 | 3 | 4

const STEP_LABELS: Record<WizardStep, string> = {
  0: 'Welcome',
  1: 'Group name',
  2: 'Add repos',
  3: 'Monorepo modules',
  4: 'Confirm',
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

function uid() {
  return Math.random().toString(36).slice(2, 9)
}

function stackBadge(stack: string) {
  const map: Record<string, string> = {
    go: 'bg-sky-500/15 text-sky-400',
    node: 'bg-emerald-500/15 text-emerald-400',
    next: 'bg-slate-500/15 text-slate-300',
    python: 'bg-yellow-500/15 text-yellow-400',
    rust: 'bg-orange-500/15 text-orange-400',
    jvm: 'bg-red-500/15 text-red-400',
    ruby: 'bg-red-500/15 text-red-300',
  }
  return map[stack] ?? 'bg-slate-700/40 text-slate-400'
}

// ─────────────────────────────────────────────────────────────────────────────
// Step 0 — Welcome
// ─────────────────────────────────────────────────────────────────────────────

function WelcomeStep({ onNext }: { onNext: () => void }) {
  const navigate = useNavigate()
  return (
    <div
      className="flex flex-col items-center gap-8 py-12 px-6 text-center"
      data-testid="onboard-welcome"
    >
      <div className="rounded-full bg-indigo-500/10 p-5 ring-1 ring-indigo-500/20">
        <GitBranch className="w-12 h-12 text-indigo-400" />
      </div>

      <div className="space-y-3 max-w-md">
        <h1 className="text-2xl font-semibold text-slate-100">
          Set up your first group
        </h1>
        <p className="text-sm text-slate-400 leading-relaxed">
          A group is a named collection of repos indexed together.
          This wizard guides you through adding repos, detecting monorepo
          workspaces, and kicking off indexing — all without leaving the browser.
        </p>
      </div>

      <div className="flex flex-col sm:flex-row gap-3">
        <button
          onClick={onNext}
          className="inline-flex items-center gap-2 rounded-lg bg-indigo-600 hover:bg-indigo-500 px-6 py-2.5 text-sm font-medium text-white transition-colors"
          data-testid="onboard-get-started"
        >
          Get started <ArrowRight className="w-4 h-4" />
        </button>
        <button
          onClick={() => navigate('/')}
          className="inline-flex items-center gap-2 rounded-lg border border-slate-700 hover:border-slate-600 px-6 py-2.5 text-sm font-medium text-slate-400 hover:text-slate-300 transition-colors"
        >
          I already have a group
        </button>
      </div>

      <div className="w-full max-w-sm rounded-xl border border-slate-800 bg-slate-900/60 p-4 text-left space-y-2">
        <p className="text-xs font-mono uppercase tracking-wider text-slate-500">
          <Terminal className="w-3 h-3 inline mr-1" />
          Prefer the terminal?
        </p>
        <pre className="text-xs text-slate-400 font-mono">
          <code>archigraph wizard</code>
        </pre>
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Step 1 — Group name
// ─────────────────────────────────────────────────────────────────────────────

function GroupNameStep({
  groupName,
  onChange,
  existingGroups,
}: {
  groupName: string
  onChange: (v: string) => void
  existingGroups: string[]
}) {
  const conflict = existingGroups.includes(groupName.trim())
  const empty = groupName.trim() === ''

  return (
    <div className="space-y-6" data-testid="onboard-group-name">
      <div>
        <h2 className="text-lg font-semibold text-slate-100 mb-1">Name your group</h2>
        <p className="text-sm text-slate-400">
          Used as the registry key. Must be unique. Letters, numbers, hyphens.
        </p>
      </div>

      <div className="space-y-2">
        <label htmlFor="group-name" className="block text-sm font-medium text-slate-300">
          Group name
        </label>
        <input
          id="group-name"
          type="text"
          value={groupName}
          onChange={e => onChange(e.target.value.replace(/[^a-zA-Z0-9-]/g, '-').replace(/^-+|-+$/g, ''))}
          placeholder="my-service"
          className={[
            'w-full rounded-lg border px-3 py-2 text-sm bg-slate-900 text-slate-100 placeholder-slate-600',
            'focus:outline-none focus:ring-2 focus:ring-indigo-500/50 transition-colors',
            conflict ? 'border-red-500/70' : empty ? 'border-slate-700' : 'border-emerald-600/60',
          ].join(' ')}
          data-testid="group-name-input"
          autoFocus
        />
        {conflict && (
          <p className="text-xs text-red-400 flex items-center gap-1">
            <AlertCircle className="w-3 h-3" />
            A group with this name already exists.
          </p>
        )}
        {!conflict && !empty && (
          <p className="text-xs text-emerald-400 flex items-center gap-1">
            <CheckCircle2 className="w-3 h-3" />
            Available
          </p>
        )}
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Repo row — path input + live validation chip
// ─────────────────────────────────────────────────────────────────────────────

function RepoRow({
  entry,
  onChange,
  onRemove,
  canRemove,
}: {
  entry: RepoEntry
  onChange: (raw: string) => void
  onRemove: () => void
  canRemove: boolean
}) {
  const { rawPath, checkResult, checking } = entry

  return (
    <div className="rounded-xl border border-slate-800 bg-slate-900/60 p-4 space-y-3">
      <div className="flex gap-2">
        <div className="relative flex-1">
          <FolderOpen className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-500" />
          <input
            type="text"
            value={rawPath}
            onChange={e => onChange(e.target.value)}
            placeholder="/path/to/my-repo  or  ~/projects/api"
            className={[
              'w-full rounded-lg border pl-9 pr-3 py-2 text-sm bg-slate-950 text-slate-100 placeholder-slate-600',
              'focus:outline-none focus:ring-2 focus:ring-indigo-500/50 transition-colors font-mono',
              checkResult && !checkResult.valid ? 'border-red-500/60'
                : checkResult?.valid ? 'border-emerald-600/50'
                  : 'border-slate-700',
            ].join(' ')}
          />
        </div>
        {canRemove && (
          <button
            onClick={onRemove}
            className="rounded-lg border border-slate-700 hover:border-red-500/50 p-2 text-slate-500 hover:text-red-400 transition-colors"
            aria-label="Remove repo"
          >
            <Trash2 className="w-4 h-4" />
          </button>
        )}
      </div>

      {checking && (
        <div className="flex items-center gap-2 text-xs text-slate-500">
          <Loader2 className="w-3 h-3 animate-spin" /> Validating…
        </div>
      )}

      {checkResult && checkResult.valid && (
        <div className="flex flex-wrap gap-2 text-xs">
          <span className={`rounded-full px-2 py-0.5 font-medium ${stackBadge(checkResult.stack)}`}>
            {checkResult.stack || 'unknown'}
          </span>
          {checkResult.is_monorepo && (
            <span className="rounded-full px-2 py-0.5 bg-violet-500/15 text-violet-400 font-medium">
              <Package className="w-3 h-3 inline mr-0.5" />monorepo
            </span>
          )}
          {checkResult.has_agents_md && (
            <span className="rounded-full px-2 py-0.5 bg-amber-500/15 text-amber-400 font-medium">
              AGENTS.md
            </span>
          )}
          {checkResult.has_archigraph_config && (
            <span className="rounded-full px-2 py-0.5 bg-teal-500/15 text-teal-400 font-medium">
              .archigraph config
            </span>
          )}
          <span className="text-slate-500 truncate max-w-[300px]" title={checkResult.abs_path}>
            {checkResult.abs_path}
          </span>
        </div>
      )}

      {checkResult && !checkResult.valid && (
        <p className="text-xs text-red-400 flex items-center gap-1">
          <AlertCircle className="w-3 h-3" />
          {checkResult.error ?? 'Invalid path'}
        </p>
      )}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Step 2 — Add repos
// ─────────────────────────────────────────────────────────────────────────────

function AddReposStep({
  repos,
  onPathChange,
  onAdd,
  onRemove,
}: {
  repos: RepoEntry[]
  onPathChange: (id: string, raw: string) => void
  onAdd: () => void
  onRemove: (id: string) => void
}) {
  return (
    <div className="space-y-6" data-testid="onboard-add-repos">
      <div>
        <h2 className="text-lg font-semibold text-slate-100 mb-1">Add repositories</h2>
        <p className="text-sm text-slate-400">
          Paste absolute paths (or ~ paths) to the repos you want to index.
          Monorepo workspaces will be detected automatically.
        </p>
      </div>

      <div className="space-y-3">
        {repos.map(r => (
          <RepoRow
            key={r.id}
            entry={r}
            onChange={raw => onPathChange(r.id, raw)}
            onRemove={() => onRemove(r.id)}
            canRemove={repos.length > 1}
          />
        ))}
      </div>

      <button
        onClick={onAdd}
        className="inline-flex items-center gap-2 rounded-lg border border-dashed border-slate-700 hover:border-indigo-500/50 px-4 py-2 text-sm text-slate-400 hover:text-indigo-400 transition-colors w-full justify-center"
      >
        <Plus className="w-4 h-4" /> Add another repo
      </button>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Step 3 — Monorepo module selection
// ─────────────────────────────────────────────────────────────────────────────

function MonorepoStep({
  repos,
  onToggleModule,
}: {
  repos: RepoEntry[]
  onToggleModule: (repoId: string, pkg: string) => void
}) {
  const monorepos = repos.filter(r => r.checkResult?.is_monorepo && r.monorepo)

  if (monorepos.length === 0) {
    return (
      <div className="space-y-4 py-8 text-center" data-testid="onboard-monorepo">
        <CheckCircle2 className="w-10 h-10 text-emerald-400 mx-auto" />
        <p className="text-sm text-slate-400">No monorepo workspaces detected. All repos will be indexed as-is.</p>
      </div>
    )
  }

  return (
    <div className="space-y-6" data-testid="onboard-monorepo">
      <div>
        <h2 className="text-lg font-semibold text-slate-100 mb-1">Select workspace packages</h2>
        <p className="text-sm text-slate-400">
          We detected monorepo workspaces. Choose which packages to index.
          Uncheck packages you want to skip (e.g. styleguide, e2e).
        </p>
      </div>

      {monorepos.map(repo => (
        <div key={repo.id} className="rounded-xl border border-slate-800 bg-slate-900/60 p-4 space-y-3">
          <div className="flex items-center gap-2 text-sm font-medium text-slate-200">
            <Layers className="w-4 h-4 text-violet-400" />
            {repo.checkResult?.abs_path ?? repo.rawPath}
          </div>
          <div className="space-y-1 pl-2">
            {(repo.monorepo?.packages ?? []).map(pkg => {
              const checked = repo.selectedModules.includes(pkg)
              return (
                <label
                  key={pkg}
                  className="flex items-center gap-3 py-1.5 cursor-pointer hover:bg-slate-800/40 rounded px-2 -mx-2 transition-colors"
                >
                  <input
                    type="checkbox"
                    checked={checked}
                    onChange={() => onToggleModule(repo.id, pkg)}
                    className="rounded border-slate-600 bg-slate-800 text-indigo-500 focus:ring-indigo-500/50"
                  />
                  <span className="text-sm font-mono text-slate-300">{pkg}</span>
                </label>
              )
            })}
          </div>
        </div>
      ))}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Step 4 — Confirm
// ─────────────────────────────────────────────────────────────────────────────

function ConfirmStep({
  groupName,
  repos,
  onConfirm,
  busy,
  error,
}: {
  groupName: string
  repos: RepoEntry[]
  onConfirm: () => void
  busy: boolean
  error: string | null
}) {
  const validRepos = repos.filter(r => r.checkResult?.valid)

  return (
    <div className="space-y-6" data-testid="onboard-confirm">
      <div>
        <h2 className="text-lg font-semibold text-slate-100 mb-1">Ready to index</h2>
        <p className="text-sm text-slate-400">
          Review the configuration before starting.
        </p>
      </div>

      <div className="rounded-xl border border-slate-800 bg-slate-900/60 p-4 space-y-3">
        <div className="flex items-center gap-2">
          <GitBranch className="w-4 h-4 text-indigo-400" />
          <span className="text-sm font-medium text-slate-200">Group:</span>
          <span className="text-sm font-mono text-indigo-300">{groupName}</span>
        </div>
        <hr className="border-slate-800" />
        {validRepos.map(r => (
          <div key={r.id} className="space-y-1">
            <div className="flex items-center gap-2 text-sm text-slate-300">
              <FolderOpen className="w-4 h-4 text-slate-500" />
              <span className="font-mono text-xs truncate">{r.checkResult?.abs_path}</span>
              <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${stackBadge(r.checkResult?.stack ?? '')}`}>
                {r.checkResult?.stack}
              </span>
            </div>
            {r.selectedModules.length > 0 && (
              <div className="pl-6 text-xs text-slate-500 space-y-0.5">
                {r.selectedModules.map(m => (
                  <div key={m} className="flex items-center gap-1">
                    <Package className="w-3 h-3" /> {m}
                  </div>
                ))}
              </div>
            )}
          </div>
        ))}
      </div>

      {error && (
        <div className="rounded-lg bg-red-500/10 border border-red-500/30 px-4 py-3 text-sm text-red-400 flex items-start gap-2">
          <AlertCircle className="w-4 h-4 mt-0.5 shrink-0" />
          {error}
        </div>
      )}

      <button
        onClick={onConfirm}
        disabled={busy}
        className="inline-flex items-center gap-2 rounded-lg bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 disabled:cursor-not-allowed px-6 py-2.5 text-sm font-medium text-white transition-colors w-full justify-center"
        data-testid="onboard-start-indexing"
      >
        {busy ? <Loader2 className="w-4 h-4 animate-spin" /> : <Activity className="w-4 h-4" />}
        {busy ? 'Creating group…' : 'Start indexing'}
      </button>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Step indicator
// ─────────────────────────────────────────────────────────────────────────────

function StepIndicator({ current, total }: { current: WizardStep; total: number }) {
  return (
    <div className="flex items-center justify-between mb-8">
      {Array.from({ length: total }, (_, i) => (
        <div key={i} className="flex items-center gap-1 flex-1">
          <div
            className={[
              'w-6 h-6 rounded-full flex items-center justify-center text-xs font-medium transition-colors shrink-0',
              i < current ? 'bg-indigo-600 text-white'
                : i === current ? 'bg-indigo-500 text-white ring-2 ring-indigo-400/30'
                  : 'bg-slate-800 text-slate-500',
            ].join(' ')}
          >
            {i < current ? <CheckCircle2 className="w-3 h-3" /> : i + 1}
          </div>
          {i < total - 1 && (
            <div className={`h-px flex-1 mx-1 transition-colors ${i < current ? 'bg-indigo-600' : 'bg-slate-800'}`} />
          )}
        </div>
      ))}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// OnboardRoute (root)
// ─────────────────────────────────────────────────────────────────────────────

export function OnboardRoute() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { data: registry } = useRegistry()

  const [step, setStep] = useState<WizardStep>(0)
  const [groupName, setGroupName] = useState('')
  const [repos, setRepos] = useState<RepoEntry[]>([
    { id: uid(), rawPath: '', checkResult: null, checking: false, monorepo: null, selectedModules: [] },
  ])
  const [submitBusy, setSubmitBusy] = useState(false)
  const [submitError, setSubmitError] = useState<string | null>(null)
  const [progressGroup, setProgressGroup] = useState<string | null>(null)
  const [showProgress, setShowProgress] = useState(false)

  const debounceTimers = useRef<Record<string, ReturnType<typeof setTimeout>>>({})

  const existingGroups = (registry?.groups ?? []).map((g: { id: string }) => g.id)

  // ── path validation (debounced 600 ms) ─────────────────────────────────────
  const validatePath = useCallback((id: string, raw: string) => {
    clearTimeout(debounceTimers.current[id])
    if (!raw.trim()) {
      setRepos(prev => prev.map(r => r.id === id ? { ...r, checkResult: null, checking: false } : r))
      return
    }
    setRepos(prev => prev.map(r => r.id === id ? { ...r, checking: true } : r))
    debounceTimers.current[id] = setTimeout(async () => {
      try {
        const result = await onboardCheckPath(raw.trim())
        setRepos(prev => prev.map(r => {
          if (r.id !== id) return r
          // Auto-fill group name on first valid repo if still blank.
          if (result.valid && r === prev[0]) {
            setGroupName(gn => gn || result.suggested_group_name)
          }
          return { ...r, checkResult: result, checking: false }
        }))
        // Kick monorepo scan in parallel if needed.
        if (result.valid && result.is_monorepo) {
          const mono = await onboardDetectMonorepo(result.abs_path)
          setRepos(prev => prev.map(r => r.id === id
            ? { ...r, monorepo: mono, selectedModules: mono.packages }
            : r,
          ))
        }
      } catch {
        setRepos(prev => prev.map(r => r.id === id
          ? { ...r, checkResult: { valid: false, abs_path: '', suggested_group_name: '', suggested_slug: '', stack: '', is_monorepo: false, has_agents_md: false, has_archigraph_config: false, error: 'Validation failed — is the daemon running?' }, checking: false }
          : r,
        ))
      }
    }, 600)
  }, [])

  const handlePathChange = useCallback((id: string, raw: string) => {
    setRepos(prev => prev.map(r => r.id === id ? { ...r, rawPath: raw } : r))
    validatePath(id, raw)
  }, [validatePath])

  const handleAddRepo = useCallback(() => {
    setRepos(prev => [...prev, { id: uid(), rawPath: '', checkResult: null, checking: false, monorepo: null, selectedModules: [] }])
  }, [])

  const handleRemoveRepo = useCallback((id: string) => {
    setRepos(prev => prev.filter(r => r.id !== id))
  }, [])

  const handleToggleModule = useCallback((repoId: string, pkg: string) => {
    setRepos(prev => prev.map(r => {
      if (r.id !== repoId) return r
      const has = r.selectedModules.includes(pkg)
      return { ...r, selectedModules: has ? r.selectedModules.filter(m => m !== pkg) : [...r.selectedModules, pkg] }
    }))
  }, [])

  // ── step navigation guard ───────────────────────────────────────────────────
  const validRepos = repos.filter(r => r.checkResult?.valid)
  const hasMonorepos = validRepos.some(r => r.checkResult?.is_monorepo)

  const canAdvance = ((): boolean => {
    if (step === 1) return groupName.trim().length > 0 && !existingGroups.includes(groupName.trim())
    if (step === 2) return validRepos.length > 0
    return true
  })()

  const handleNext = () => {
    if (step === 2 && !hasMonorepos) {
      // Skip monorepo step if no monorepos detected.
      setStep(4)
      return
    }
    setStep(s => Math.min(s + 1, 4) as WizardStep)
  }
  const handleBack = () => {
    if (step === 4 && !hasMonorepos) {
      setStep(2)
      return
    }
    setStep(s => Math.max(s - 1, 0) as WizardStep)
  }

  // ── submit ──────────────────────────────────────────────────────────────────
  const handleSubmit = useCallback(async () => {
    setSubmitBusy(true)
    setSubmitError(null)
    try {
      const repoSpecs = validRepos.map(r => ({
        path: r.checkResult!.abs_path,
        slug: r.checkResult!.suggested_slug,
        modules: r.selectedModules.length > 0 ? r.selectedModules : undefined,
      }))
      const reply = await onboardCreateGroup(groupName.trim(), repoSpecs)
      // Invalidate registry cache so the landing page picks up the new group.
      await queryClient.invalidateQueries({ queryKey: ['registry'] })
      setProgressGroup(reply.group)
      setShowProgress(true)
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err)
      setSubmitError(msg)
    } finally {
      setSubmitBusy(false)
    }
  }, [groupName, validRepos, queryClient])

  // ── render ──────────────────────────────────────────────────────────────────
  const totalSteps = 5 // 0–4

  return (
    <div className="min-h-screen bg-slate-950 flex items-center justify-center p-4">
      <div className="w-full max-w-xl">
        {/* Card */}
        <div className="rounded-2xl border border-slate-800 bg-slate-950/80 shadow-xl backdrop-blur-sm overflow-hidden">
          {/* Progress bar at top */}
          <div className="h-1 bg-slate-800">
            <div
              className="h-full bg-indigo-500 transition-all duration-500"
              style={{ width: `${(step / (totalSteps - 1)) * 100}%` }}
            />
          </div>

          <div className="p-6 sm:p-8">
            <StepIndicator current={step} total={totalSteps} />

            {/* Step content */}
            <div className="min-h-[320px]">
              {step === 0 && <WelcomeStep onNext={() => setStep(1)} />}
              {step === 1 && (
                <GroupNameStep
                  groupName={groupName}
                  onChange={setGroupName}
                  existingGroups={existingGroups}
                />
              )}
              {step === 2 && (
                <AddReposStep
                  repos={repos}
                  onPathChange={handlePathChange}
                  onAdd={handleAddRepo}
                  onRemove={handleRemoveRepo}
                />
              )}
              {step === 3 && (
                <MonorepoStep
                  repos={repos}
                  onToggleModule={handleToggleModule}
                />
              )}
              {step === 4 && (
                <ConfirmStep
                  groupName={groupName}
                  repos={repos}
                  onConfirm={handleSubmit}
                  busy={submitBusy}
                  error={submitError}
                />
              )}
            </div>

            {/* Navigation buttons (hidden on welcome step — it has its own CTA) */}
            {step > 0 && (
              <div className="flex items-center justify-between mt-8 pt-4 border-t border-slate-800">
                <button
                  onClick={handleBack}
                  className="inline-flex items-center gap-1.5 text-sm text-slate-400 hover:text-slate-200 transition-colors"
                >
                  <ChevronLeft className="w-4 h-4" /> Back
                </button>

                {step < 4 && (
                  <button
                    onClick={handleNext}
                    disabled={!canAdvance}
                    className="inline-flex items-center gap-1.5 rounded-lg bg-indigo-600 hover:bg-indigo-500 disabled:opacity-40 disabled:cursor-not-allowed px-5 py-2 text-sm font-medium text-white transition-colors"
                  >
                    {STEP_LABELS[(step + 1) as WizardStep]} <ChevronRight className="w-4 h-4" />
                  </button>
                )}
              </div>
            )}
          </div>
        </div>

        {/* Skip link */}
        {step === 0 && (
          <p className="text-center mt-4 text-xs text-slate-600">
            Already indexed? <button onClick={() => navigate('/')} className="underline hover:text-slate-400">Go to landing</button>
          </p>
        )}
      </div>

      {/* Indexing progress modal — opens after create-group succeeds */}
      {progressGroup && (
        <IndexingProgressModal
          isOpen={showProgress}
          groupSlug={progressGroup}
          onClose={() => {
            setShowProgress(false)
            navigate('/')
          }}
        />
      )}
    </div>
  )
}
