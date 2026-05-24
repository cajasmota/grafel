/* ============================================================
   ModeMenu/ModeMenu.tsx — popover opened by clicking the
   ModeBadge in the TopBar (S7a of #2149, #2169).

   Renders three mode cards. Clicking "Switch" on a non-active
   card opens a confirmation dialog that warns the user the
   daemon will restart and ongoing queries will be interrupted.

   After confirmation the component:
   1. POSTs /api/v2/daemon/mode via useSetDaemonMode
   2. Shows a "restarting…" indicator while polling /api/v2/meta
      (implemented via a simple 3-second refetch of useDaemonMode)
   ============================================================ */

import { useState } from "react";
import * as PopoverPrimitive from "@radix-ui/react-popover";
import { CheckCircle2, Loader2, RotateCcw } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogClose,
} from "@/components/ui/dialog";
import { useDaemonMode, useSetDaemonMode } from "@/hooks/use-daemon-mode";
import type { DaemonModeInfo } from "@/data/types";

/* ---- mode metadata --------------------------------------- */
const MODE_COLORS: Record<string, { accent: string; badge: string }> = {
  background:  { accent: "border-blue-400/40",  badge: "bg-blue-500/10 text-blue-600 dark:text-blue-400" },
  workstation: { accent: "border-green-400/40", badge: "bg-green-500/10 text-green-700 dark:text-green-400" },
  readonly:    { accent: "border-amber-400/40", badge: "bg-amber-500/10 text-amber-700 dark:text-amber-400" },
};

const FALLBACK_COLORS = { accent: "border-border", badge: "bg-surface-2 text-text-3" };

function modeColors(name: string) {
  return MODE_COLORS[name] ?? FALLBACK_COLORS;
}

/* ---- ModeCard -------------------------------------------- */

interface ModeCardProps {
  info: DaemonModeInfo;
  isActive: boolean;
  onSwitch: (name: string) => void;
}

function ModeCard({ info, isActive, onSwitch }: ModeCardProps) {
  const colors = modeColors(info.name);
  return (
    <div
      className={[
        "rounded-lg border p-3 flex flex-col gap-2",
        isActive ? `${colors.accent} bg-surface` : "border-border bg-bg",
      ].join(" ")}
    >
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <span
            className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${colors.badge}`}
          >
            {info.name}
          </span>
          {isActive && (
            <CheckCircle2 size={13} className="text-green-500 shrink-0" aria-label="Active" />
          )}
        </div>
        {!isActive && (
          <Button
            size="sm"
            variant="secondary"
            className="h-6 text-xs px-2"
            onClick={() => onSwitch(info.name)}
          >
            Switch
          </Button>
        )}
        {isActive && (
          <span className="text-xs text-text-3">active</span>
        )}
      </div>
      <p className="text-xs text-text-2 leading-relaxed">{info.description}</p>
    </div>
  );
}

/* ---- ConfirmDialog --------------------------------------- */

interface ConfirmDialogProps {
  targetMode: string;
  open: boolean;
  onConfirm: () => void;
  onCancel: () => void;
  isPending: boolean;
}

function ConfirmDialog({ targetMode, open, onConfirm, onCancel, isPending }: ConfirmDialogProps) {
  return (
    <Dialog open={open} onOpenChange={(v) => !v && onCancel()}>
      <DialogContent hideClose>
        <div className="flex flex-col gap-4">
          <div>
            <h2 className="text-base font-semibold text-text">Switch to {targetMode} mode?</h2>
            <p className="mt-1.5 text-sm text-text-2 leading-relaxed">
              The daemon will restart. Any ongoing indexing queries will be interrupted and will
              resume automatically after the daemon comes back up.
            </p>
          </div>
          <div className="flex justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary" size="sm" onClick={onCancel} disabled={isPending}>
                Cancel
              </Button>
            </DialogClose>
            <Button
              size="sm"
              onClick={onConfirm}
              disabled={isPending}
              className="gap-1.5"
            >
              {isPending ? (
                <>
                  <Loader2 size={13} className="animate-spin" />
                  Switching…
                </>
              ) : (
                <>
                  <RotateCcw size={13} />
                  Confirm restart
                </>
              )}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

/* ---- ModeMenu -------------------------------------------- */

export interface ModeMenuProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  children: React.ReactNode; // trigger element (ModeBadge button)
}

export function ModeMenu({ open, onOpenChange, children }: ModeMenuProps) {
  const { data } = useDaemonMode();
  const setMode  = useSetDaemonMode();

  const [confirmTarget, setConfirmTarget] = useState<string | null>(null);

  const effectiveMode = data?.effective_mode ?? "background";
  const allModes: DaemonModeInfo[] = data?.all_modes ?? [];

  function handleSwitchRequest(name: string) {
    setConfirmTarget(name);
  }

  function handleConfirm() {
    if (!confirmTarget) return;
    setMode.mutate(confirmTarget, {
      onSuccess: () => {
        setConfirmTarget(null);
        onOpenChange(false);
      },
      onError: () => {
        // Leave dialog open so user can retry or cancel.
      },
    });
  }

  function handleCancel() {
    setConfirmTarget(null);
  }

  return (
    <>
      <PopoverPrimitive.Root open={open} onOpenChange={onOpenChange}>
        <PopoverPrimitive.Trigger asChild>
          {children}
        </PopoverPrimitive.Trigger>
        <PopoverPrimitive.Portal>
          <PopoverPrimitive.Content
            side="bottom"
            align="end"
            sideOffset={8}
            className={[
              "z-50 w-[320px] rounded-xl border border-border bg-surface",
              "p-3 shadow-[var(--shadow-4)]",
              "data-[state=open]:animate-in data-[state=closed]:animate-out",
              "data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0",
              "data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95",
            ].join(" ")}
          >
            <div className="mb-2.5 px-0.5">
              <p className="text-xs font-semibold text-text uppercase tracking-wider">Daemon Mode</p>
              <p className="text-xs text-text-3 mt-0.5">
                Controls memory usage, background activity, and feature activation.
              </p>
            </div>
            <div className="flex flex-col gap-2">
              {allModes.map((m) => (
                <ModeCard
                  key={m.name}
                  info={m}
                  isActive={m.name === effectiveMode}
                  onSwitch={handleSwitchRequest}
                />
              ))}
            </div>
          </PopoverPrimitive.Content>
        </PopoverPrimitive.Portal>
      </PopoverPrimitive.Root>

      {/* Confirmation dialog — rendered outside popover so z-index stacks correctly */}
      <ConfirmDialog
        targetMode={confirmTarget ?? ""}
        open={confirmTarget !== null}
        onConfirm={handleConfirm}
        onCancel={handleCancel}
        isPending={setMode.isPending}
      />
    </>
  );
}
