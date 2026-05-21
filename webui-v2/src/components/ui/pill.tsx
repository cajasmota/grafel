import { forwardRef } from "react";
import { cn } from "@/lib/utils";

export interface PillProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  active?: boolean;
  /** Optional trailing count badge. */
  count?: number;
}

/** Toolbar pill button (`.ag-pill`). Toggleable scope/filter control. */
export const Pill = forwardRef<HTMLButtonElement, PillProps>(
  ({ className, active, count, children, ...props }, ref) => (
    <button
      ref={ref}
      aria-pressed={active}
      className={cn(
        "inline-flex items-center gap-1.5 h-7 px-2.5 rounded-full text-sm font-medium",
        "border transition-colors duration-[120ms] ease-[var(--ease-out)]",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
        active
          ? "bg-accent-soft text-accent-strong border-transparent"
          : "bg-surface text-text-2 border-border hover:bg-surface-2",
        className,
      )}
      {...props}
    >
      {children}
      {typeof count === "number" && count > 0 ? (
        <span className="ml-0.5 inline-flex items-center justify-center min-w-[16px] h-4 px-1 rounded-full bg-accent text-accent-text text-[10px] tabular-nums">
          {count}
        </span>
      ) : null}
    </button>
  ),
);
Pill.displayName = "Pill";
