/**
 * GroupOpsMenu — per-group Operations kebab menu (#1200)
 *
 * Renders a MoreVertical button on each landing card that opens a dropdown
 * with maintenance actions:
 *   • Rebuild group (idempotent)
 *   • Reset group   (destructive — confirm + type group name)
 *   • Cleanup       (orphaned registry entries — dry-run preview first)
 *
 * All actions are async. After a successful dispatch the component shows a
 * brief "in progress" chip; the user can follow detailed progress via the
 * SSE stream (index-progress surface).
 */

import { useState, useRef, useEffect, useCallback } from 'react'
import { MoreVertical, RefreshCw, Trash2, Sparkles, X, AlertTriangle, CheckCircle } from 'lucide-react'
import {
  postGroupRebuild,
  postGroupReset,
  fetchCleanupPreview,
  postCleanup,
  type CleanupOrphan,
} from '@/api/client'

// ─────────────────────────────────────────────────────────────────────────────
// Types
// ─────────────────────────────────────────────────────────────────────────────

export interface GroupOpsMenuProps {
  /** Group slug (id), e.g. "acme-web" */
  group: string
  /** Display name shown in confirm modals, e.g. "Acme Web" */
  displayName: string
}

type ModalKind = 'rebuild' | 'reset' | 'cleanup-preview' | 'cleanup-confirm' | null

interface ToastState {
  kind: 'success' | 'error'
  message: string
}

// ─────────────────────────────────────────────────────────────────────────────
// Confirm modal base
// ─────────────────────────────────────────────────────────────────────────────

interface ConfirmModalProps {
  title: string
  children: React.ReactNode
  confirmLabel: string
  confirmClass?: string
  onConfirm: () => void
  onCancel: () => void
  disabled?: boolean
  busy?: boolean
}

function ConfirmModal({
  title,
  children,
  confirmLabel,
  confirmClass = 'bg-sky-600 hover:bg-sky-500 text-white',
  onConfirm,
  onCancel,
  disabled = false,
  busy = false,
}: ConfirmModalProps) {
  // Trap focus on mount
  const cancelRef = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    cancelRef.current?.focus()
  }, [])

  // Close on Escape
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onCancel()
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [onCancel])

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="ops-modal-title"
      className="fixed inset-0 z-50 flex items-center justify-center p-4"
    >
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-black/60 backdrop-blur-sm"
        aria-hidden
        onClick={onCancel}
      />

      {/* Panel */}
      <div className="relative z-10 w-full max-w-md rounded-xl border border-slate-700 bg-slate-900 shadow-2xl">
        {/* Header */}
        <div className="flex items-start justify-between gap-3 px-6 pt-6 pb-4">
          <h2 id="ops-modal-title" className="text-lg font-semibold text-slate-100">
            {title}
          </h2>
          <button
            type="button"
            aria-label="Close"
            onClick={onCancel}
            className="shrink-0 rounded text-slate-400 hover:text-slate-200 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sky-500"
          >
            <X className="w-5 h-5" aria-hidden />
          </button>
        </div>

        {/* Body */}
        <div className="px-6 pb-4 text-sm text-slate-300">{children}</div>

        {/* Footer */}
        <div className="flex justify-end gap-3 px-6 pb-6 pt-2">
          <button
            ref={cancelRef}
            type="button"
            onClick={onCancel}
            className="rounded-lg border border-slate-700 px-4 py-2 text-sm text-slate-300 hover:bg-slate-800 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sky-500"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={onConfirm}
            disabled={disabled || busy}
            className={[
              'rounded-lg px-4 py-2 text-sm font-medium transition-colors',
              'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sky-500',
              'disabled:opacity-50 disabled:cursor-not-allowed',
              confirmClass,
            ].join(' ')}
          >
            {busy ? 'Working…' : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Rebuild confirm modal
// ─────────────────────────────────────────────────────────────────────────────

interface RebuildModalProps {
  group: string
  onConfirm: () => void
  onCancel: () => void
  busy: boolean
}

function RebuildModal({ group, onConfirm, onCancel, busy }: RebuildModalProps) {
  return (
    <ConfirmModal
      title="Rebuild group"
      confirmLabel="Rebuild"
      confirmClass="bg-sky-600 hover:bg-sky-500 text-white"
      onConfirm={onConfirm}
      onCancel={onCancel}
      busy={busy}
    >
      <p className="mb-3">
        Force a fresh AST re-index for all repos in{' '}
        <span className="font-mono text-sky-400">{group}</span>. This is safe and idempotent —
        no cached data is deleted. Progress will stream in the index-progress surface.
      </p>
      <p className="text-slate-400 text-xs">
        Estimated time: a few seconds per repo for a warm build.
      </p>
    </ConfirmModal>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Reset confirm modal — requires typing group name
// ─────────────────────────────────────────────────────────────────────────────

interface ResetModalProps {
  group: string
  onConfirm: () => void
  onCancel: () => void
  busy: boolean
}

function ResetModal({ group, onConfirm, onCancel, busy }: ResetModalProps) {
  const [typed, setTyped] = useState('')
  const confirmed = typed === group

  return (
    <ConfirmModal
      title="Reset group"
      confirmLabel="Reset and rebuild"
      confirmClass="bg-red-600 hover:bg-red-500 text-white"
      onConfirm={onConfirm}
      onCancel={onCancel}
      disabled={!confirmed}
      busy={busy}
    >
      <div className="flex items-start gap-3 mb-4 rounded-lg bg-red-950/40 border border-red-900/50 p-3">
        <AlertTriangle className="w-5 h-5 text-red-400 shrink-0 mt-0.5" aria-hidden />
        <div>
          <p className="text-red-300 font-medium text-sm mb-1">Destructive operation</p>
          <p className="text-red-400/80 text-xs">
            This will wipe all <code className="font-mono">.archigraph/</code> cache directories
            inside <span className="font-mono">{group}</span> repos and then trigger a full rebuild.
            Existing graph data will be lost until the rebuild completes.
          </p>
        </div>
      </div>

      <p className="mb-3 text-slate-300">
        Type <span className="font-mono bg-slate-800 px-1.5 py-0.5 rounded text-slate-200">{group}</span>{' '}
        to confirm:
      </p>
      <input
        type="text"
        aria-label={`Type ${group} to confirm reset`}
        value={typed}
        onChange={(e) => setTyped(e.target.value)}
        autoComplete="off"
        spellCheck={false}
        className={[
          'w-full rounded-lg border px-3 py-2 text-sm font-mono bg-slate-800 text-slate-100',
          'focus:outline-none focus:ring-2 focus:ring-red-500',
          confirmed
            ? 'border-red-600'
            : 'border-slate-700',
        ].join(' ')}
        placeholder={group}
      />
    </ConfirmModal>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Cleanup preview modal
// ─────────────────────────────────────────────────────────────────────────────

interface CleanupPreviewModalProps {
  orphaned: CleanupOrphan[]
  onConfirm: () => void
  onCancel: () => void
  busy: boolean
}

function CleanupPreviewModal({ orphaned, onConfirm, onCancel, busy }: CleanupPreviewModalProps) {
  const hasOrphans = orphaned.length > 0

  return (
    <ConfirmModal
      title="Cleanup — orphaned registry entries"
      confirmLabel={hasOrphans ? `Remove ${orphaned.length} orphaned ${orphaned.length === 1 ? 'entry' : 'entries'}` : 'Nothing to clean'}
      confirmClass={hasOrphans ? 'bg-amber-600 hover:bg-amber-500 text-white' : 'bg-slate-700 text-slate-400 cursor-not-allowed'}
      onConfirm={onConfirm}
      onCancel={onCancel}
      disabled={!hasOrphans}
      busy={busy}
    >
      {hasOrphans ? (
        <>
          <p className="mb-3">
            The following {orphaned.length === 1 ? 'entry' : 'entries'} reference config files that
            no longer exist on disk. Removing them keeps the registry tidy.
          </p>
          <ul className="space-y-1 mb-3 max-h-48 overflow-y-auto">
            {orphaned.map((o) => (
              <li key={o.name} className="flex flex-col gap-0.5 rounded bg-slate-800 px-3 py-2">
                <span className="font-mono text-slate-200 text-sm">{o.name}</span>
                <span className="font-mono text-slate-500 text-xs truncate" title={o.config_path}>
                  {o.config_path}
                </span>
              </li>
            ))}
          </ul>
        </>
      ) : (
        <div className="flex items-center gap-2 text-emerald-400">
          <CheckCircle className="w-5 h-5 shrink-0" aria-hidden />
          <span>No orphaned registry entries found. The registry is clean.</span>
        </div>
      )}
    </ConfirmModal>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Dropdown menu
// ─────────────────────────────────────────────────────────────────────────────

interface MenuItemProps {
  icon: React.ReactNode
  label: string
  danger?: boolean
  onClick: () => void
}

function MenuItem({ icon, label, danger = false, onClick }: MenuItemProps) {
  return (
    <button
      type="button"
      role="menuitem"
      onClick={onClick}
      className={[
        'flex items-center gap-2.5 w-full px-3 py-2 text-sm rounded-md',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sky-500',
        danger
          ? 'text-red-400 hover:bg-red-950/40 hover:text-red-300'
          : 'text-slate-300 hover:bg-slate-800 hover:text-slate-100',
      ].join(' ')}
    >
      <span aria-hidden className="shrink-0">{icon}</span>
      {label}
    </button>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// GroupOpsMenu (public)
// ─────────────────────────────────────────────────────────────────────────────

export function GroupOpsMenu({ group, displayName }: GroupOpsMenuProps) {
  const [menuOpen, setMenuOpen] = useState(false)
  const [modal, setModal] = useState<ModalKind>(null)
  const [busy, setBusy] = useState(false)
  const [toast, setToast] = useState<ToastState | null>(null)
  const [cleanupOrphans, setCleanupOrphans] = useState<CleanupOrphan[]>([])
  const menuRef = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)

  // Close menu when clicking outside
  useEffect(() => {
    if (!menuOpen) return
    const handler = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setMenuOpen(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [menuOpen])

  // Close on Escape when menu is open
  useEffect(() => {
    if (!menuOpen) return
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setMenuOpen(false)
        triggerRef.current?.focus()
      }
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [menuOpen])

  // Auto-dismiss toast
  useEffect(() => {
    if (!toast) return
    const id = setTimeout(() => setToast(null), 4000)
    return () => clearTimeout(id)
  }, [toast])

  const openModal = useCallback((kind: ModalKind) => {
    setMenuOpen(false)
    setModal(kind)
  }, [])

  const closeModal = useCallback(() => {
    setModal(null)
    setBusy(false)
  }, [])

  // ── Actions ──

  const handleRebuild = useCallback(async () => {
    setBusy(true)
    try {
      await postGroupRebuild(group)
      setToast({ kind: 'success', message: `Rebuilding ${displayName}… check progress stream.` })
      closeModal()
    } catch {
      setToast({ kind: 'error', message: 'Rebuild failed — is the daemon running?' })
      setBusy(false)
    }
  }, [group, displayName, closeModal])

  const handleReset = useCallback(async () => {
    setBusy(true)
    try {
      await postGroupReset(group)
      setToast({ kind: 'success', message: `Reset triggered for ${displayName}. Rebuilding from scratch…` })
      closeModal()
    } catch {
      setToast({ kind: 'error', message: 'Reset failed — is the daemon running?' })
      setBusy(false)
    }
  }, [group, displayName, closeModal])

  const handleOpenCleanupPreview = useCallback(async () => {
    setMenuOpen(false)
    setBusy(true)
    try {
      const preview = await fetchCleanupPreview()
      setCleanupOrphans(preview.orphaned ?? [])
      setModal('cleanup-preview')
    } catch {
      setToast({ kind: 'error', message: 'Could not fetch cleanup preview.' })
    } finally {
      setBusy(false)
    }
  }, [])

  const handleCleanupConfirm = useCallback(async () => {
    setBusy(true)
    try {
      const result = await postCleanup(false)
      setToast({ kind: 'success', message: result.message })
      closeModal()
    } catch {
      setToast({ kind: 'error', message: 'Cleanup failed.' })
      setBusy(false)
    }
  }, [closeModal])

  return (
    <>
      {/* Trigger */}
      <div className="relative" ref={menuRef}>
        <button
          ref={triggerRef}
          type="button"
          aria-label={`Operations menu for ${displayName}`}
          aria-haspopup="menu"
          aria-expanded={menuOpen}
          data-ops-trigger={group}
          onClick={(e) => {
            e.stopPropagation()
            setMenuOpen((o) => !o)
          }}
          className={[
            'rounded-md p-1 transition-colors',
            'text-slate-400 hover:text-slate-200 hover:bg-slate-700/60',
            'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sky-500',
          ].join(' ')}
        >
          <MoreVertical className="w-4 h-4" aria-hidden />
        </button>

        {/* Dropdown */}
        {menuOpen && (
          <div
            role="menu"
            aria-label={`Operations for ${displayName}`}
            data-ops-menu={group}
            className={[
              'absolute right-0 top-full mt-1 z-30 min-w-[180px]',
              'rounded-xl border border-slate-700 bg-slate-900 shadow-2xl p-1',
            ].join(' ')}
          >
            <MenuItem
              icon={<RefreshCw className="w-4 h-4" />}
              label="Rebuild group"
              onClick={() => openModal('rebuild')}
            />
            <MenuItem
              icon={<Trash2 className="w-4 h-4" />}
              label="Reset group"
              danger
              onClick={() => openModal('reset')}
            />
            <div className="my-1 border-t border-slate-800" role="separator" />
            <MenuItem
              icon={<Sparkles className="w-4 h-4" />}
              label="Cleanup registry"
              onClick={handleOpenCleanupPreview}
            />
          </div>
        )}
      </div>

      {/* Toast */}
      {toast && (
        <div
          role="status"
          aria-live="polite"
          className={[
            'fixed bottom-5 left-1/2 -translate-x-1/2 z-50',
            'flex items-center gap-2 rounded-lg px-4 py-2.5 text-sm shadow-xl',
            toast.kind === 'success'
              ? 'bg-emerald-900/80 border border-emerald-700 text-emerald-200'
              : 'bg-red-900/80 border border-red-700 text-red-200',
          ].join(' ')}
        >
          {toast.kind === 'success'
            ? <CheckCircle className="w-4 h-4 shrink-0" aria-hidden />
            : <AlertTriangle className="w-4 h-4 shrink-0" aria-hidden />}
          {toast.message}
          <button
            type="button"
            aria-label="Dismiss"
            onClick={() => setToast(null)}
            className="ml-1 opacity-60 hover:opacity-100"
          >
            <X className="w-3.5 h-3.5" aria-hidden />
          </button>
        </div>
      )}

      {/* Modals */}
      {modal === 'rebuild' && (
        <RebuildModal
          group={displayName}
          onConfirm={handleRebuild}
          onCancel={closeModal}
          busy={busy}
        />
      )}
      {modal === 'reset' && (
        <ResetModal
          group={group}
          onConfirm={handleReset}
          onCancel={closeModal}
          busy={busy}
        />
      )}
      {(modal === 'cleanup-preview' || modal === 'cleanup-confirm') && (
        <CleanupPreviewModal
          orphaned={cleanupOrphans}
          onConfirm={handleCleanupConfirm}
          onCancel={closeModal}
          busy={busy}
        />
      )}
    </>
  )
}
