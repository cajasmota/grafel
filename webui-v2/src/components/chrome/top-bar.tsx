/* ============================================================
   TopBar — per-surface breadcrumb bar. 56px tall.
   (prototype `.ag-topbar`)

   Left: archigraph › <group> › <surface>.
   Right: Quick jump · ⌘K placeholder for the command palette.
   NAVIGATION ONLY — no numeric scope counts here (handoff rule).
   ============================================================ */

import { ChevronRight } from "lucide-react";
import { Kbd } from "@/components/ui";
import { useAppStore } from "@/store/use-app-store";

export interface TopBarProps {
  group: string;
  surfaceLabel: string;
}

export function TopBar({ group, surfaceLabel }: TopBarProps) {
  const setCommandOpen = useAppStore((s) => s.setCommandOpen);

  return (
    <header className="flex items-center justify-between h-14 shrink-0 px-4 border-b border-border bg-bg">
      <nav aria-label="Breadcrumb" className="flex items-center gap-1.5 text-md">
        <span className="text-text-3">archigraph</span>
        <ChevronRight size={12} className="text-text-4" />
        <span className="font-mono text-text-2">{group}</span>
        <ChevronRight size={12} className="text-text-4" />
        <span className="font-medium text-text">{surfaceLabel}</span>
      </nav>

      <button
        onClick={() => setCommandOpen(true)}
        className="inline-flex items-center gap-2 h-8 px-3 rounded-md border border-border bg-surface text-text-3 text-md hover:bg-surface-2 transition-colors"
      >
        Quick jump
        <span className="text-text-4">·</span>
        <Kbd>⌘K</Kbd>
      </button>
    </header>
  );
}
