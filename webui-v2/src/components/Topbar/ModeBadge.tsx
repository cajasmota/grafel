/* ============================================================
   Topbar/ModeBadge.tsx — always-visible chip in the TopBar
   showing the current daemon operational mode (S7a of #2149).

   - Color-coded: background=blue, workstation=green, readonly=amber
   - Tooltip on hover: one-line description + "click to switch"
   - Click opens the ModeMenu popover
   ============================================================ */

import { useState } from "react";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { ModeMenu } from "@/components/ModeMenu/ModeMenu";
import { useDaemonMode } from "@/hooks/use-daemon-mode";

/* ---- color tokens per mode -------------------------------- */
const MODE_COLORS: Record<string, { bg: string; text: string; dot: string }> = {
  background:  { bg: "bg-blue-500/10",   text: "text-blue-600 dark:text-blue-400",   dot: "bg-blue-500" },
  workstation: { bg: "bg-green-500/10",  text: "text-green-700 dark:text-green-400", dot: "bg-green-500" },
  readonly:    { bg: "bg-amber-500/10",  text: "text-amber-700 dark:text-amber-400", dot: "bg-amber-500" },
};

const FALLBACK_COLORS = { bg: "bg-surface-2", text: "text-text-3", dot: "bg-text-4" };

function modeColors(mode: string) {
  return MODE_COLORS[mode] ?? FALLBACK_COLORS;
}

/* ---- component ------------------------------------------- */

export function ModeBadge() {
  const [menuOpen, setMenuOpen] = useState(false);
  const { data } = useDaemonMode();

  const effectiveMode = data?.effective_mode ?? "background";
  const description   = data?.description ?? "";
  const colors        = modeColors(effectiveMode);

  return (
    <ModeMenu open={menuOpen} onOpenChange={setMenuOpen}>
      <Tooltip>
        <TooltipTrigger asChild>
          {/* The ModeMenu passes a trigger ref through here */}
          <button
            aria-label={`Daemon mode: ${effectiveMode}. Click to switch.`}
            onClick={() => setMenuOpen(true)}
            className={[
              "inline-flex items-center gap-1.5 h-7 px-2.5 rounded-full",
              "text-xs font-medium select-none cursor-pointer transition-opacity",
              "border border-current/20",
              colors.bg,
              colors.text,
              "hover:opacity-80",
            ].join(" ")}
          >
            <span
              className={`size-1.5 rounded-full shrink-0 ${colors.dot}`}
              aria-hidden="true"
            />
            {effectiveMode}
          </button>
        </TooltipTrigger>
        <TooltipContent side="bottom" className="max-w-[220px]">
          <p className="text-xs leading-snug">{description || `Daemon running in ${effectiveMode} mode.`}</p>
          <p className="text-xs text-text-3 mt-0.5">Click to switch mode</p>
        </TooltipContent>
      </Tooltip>
    </ModeMenu>
  );
}
