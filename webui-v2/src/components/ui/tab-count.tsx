import { cn } from "@/lib/utils";

export interface TabCountProps {
  /** The numeric count to display. */
  value: number;
  /** Visual tone of the badge. */
  tone?: "neutral" | "warning" | "accent";
  /** Whether the owning tab is active (brightens the badge). */
  active?: boolean;
  /**
   * Required plain-language description of *what* this count measures,
   * surfaced as the badge's hover tooltip (e.g. "uncovered endpoints").
   */
  label: string;
  /** When true, render nothing if `value === 0`. Defaults to false. */
  hideOnZero?: boolean;
  /**
   * Optional unit glyph appended directly after the value (e.g. "%"), so a
   * badge can render "18%" rather than a bare "18". Included in the
   * aria-label for screen readers.
   */
  suffix?: string;
}

/**
 * A standardized count-badge primitive for tab strips and headers
 * (#4572 / #4573). Generalizes the ad-hoc TabCount/TabBadge that each
 * dashboard page reinvented.
 *
 * Renders as a plain <span> (never a nested <button>) so it never disturbs
 * a tab trigger's baseline or active underline. The `label` doubles as the
 * native title/tooltip so the number is always self-explanatory.
 */
export function TabCount({
  value,
  tone = "neutral",
  active = false,
  label,
  hideOnZero = false,
  suffix,
}: TabCountProps) {
  if (hideOnZero && value === 0) return null;

  const toneClass =
    tone === "warning"
      ? "bg-warning-soft text-warning"
      : tone === "accent"
        ? "bg-accent-soft text-accent-strong"
        : "bg-surface-2 text-text-3";

  return (
    <span
      title={label}
      aria-label={`${value}${suffix ?? ""} ${label}`}
      className={cn(
        "ml-1.5 inline-flex items-center justify-center min-w-[18px] h-[18px] px-1.5",
        "rounded-full text-[11px] font-medium tabular-nums leading-none transition-colors",
        active && tone === "neutral"
          ? "bg-accent-soft text-accent-strong"
          : toneClass,
      )}
    >
      {value}
      {suffix}
    </span>
  );
}
