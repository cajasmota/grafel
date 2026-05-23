/* ============================================================
   SectionHeader — collapsible section row with optional (i) tooltip
   Used by endpoint detail sections in routes/paths.tsx.
   ============================================================ */

import { Info, ChevronRight } from "lucide-react";
import { cn } from "@/lib/utils";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";

interface SectionHeaderProps {
  /** Section icon (Lucide, 14×14 recommended). */
  icon: React.ReactNode;
  /** Section title. */
  title: string;
  /** Optional item count shown in muted parens after the title. */
  count?: number;
  /** One-sentence info text shown in the (i) tooltip. When omitted the icon is hidden. */
  infoText?: string;
  /** Whether the collapsible body is open. */
  open: boolean;
  /** Toggle handler. */
  onToggle: () => void;
}

/**
 * SectionHeader renders a full-width collapsible button row with:
 *  - a leading icon slot
 *  - a title + optional count
 *  - an optional (i) info icon whose hover/focus reveals an explanatory tooltip
 *  - a trailing chevron that rotates when open
 *
 * The info icon is keyboard-focusable and tooltip is Escape-dismissable via
 * Radix TooltipPrimitive (already wired in the existing tooltip primitive).
 * It stops click-propagation so the (i) does not toggle the section.
 */
export function SectionHeader({
  icon,
  title,
  count,
  infoText,
  open,
  onToggle,
}: SectionHeaderProps) {
  return (
    <button
      type="button"
      onClick={onToggle}
      className={cn(
        "w-full flex items-center gap-2 px-4 py-2.5 text-sm font-medium text-text-2",
        "border-b border-border bg-bg-soft hover:bg-surface-2 transition-colors",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
      )}
    >
      <span className="text-text-3 shrink-0">{icon}</span>
      {title}
      {count !== undefined && (
        <span className="ml-1 text-xs text-text-4 tabular-nums">({count})</span>
      )}

      {infoText && (
        <Tooltip>
          <TooltipTrigger asChild>
            {/* Intercept click so the tooltip focus does NOT toggle the section. */}
            <span
              role="button"
              tabIndex={0}
              aria-label={`About ${title}`}
              onClick={(e) => e.stopPropagation()}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") e.stopPropagation();
              }}
              className={cn(
                "ml-1 inline-flex items-center justify-center rounded-sm shrink-0",
                "text-text-4 hover:text-accent focus-visible:text-accent",
                "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
                "transition-colors cursor-help",
              )}
            >
              <Info size={14} aria-hidden="true" />
            </span>
          </TooltipTrigger>
          <TooltipContent
            side="bottom"
            align="start"
            className="max-w-[260px] text-xs font-normal leading-snug"
          >
            {infoText}
          </TooltipContent>
        </Tooltip>
      )}

      <ChevronRight
        size={13}
        className={cn(
          "ml-auto text-text-4 transition-transform duration-150 shrink-0",
          open && "rotate-90",
        )}
      />
    </button>
  );
}
